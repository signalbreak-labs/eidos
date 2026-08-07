package generator

import (
	"fmt"
	"go/ast"
	"go/token"
	"sort"
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// hasStateUpgrades reports whether the resource has configured state upgrades.
func hasStateUpgrades(r ir.ResourceIR) bool {
	return len(r.StateUpgrades) > 0
}

// stateUpgradeNeedsAttr reports whether the generated state-upgrade code for the
// resource references the github.com/hashicorp/terraform-plugin-framework/attr
// package (for attr.Type in object attribute type maps).
//
// attr.Type is only emitted through the defensive null-initialization branches in
// stateUpgraderFunc, which fire when a current attribute or block is absent from
// the prior schema produced by priorSchemaForUpgrade. Because priorSchemaForUpgrade
// currently copies every current attribute (renamed) and block into the prior
// schema, this returns false for every production resource and the attr import is
// not registered — fixing the "imported and not used" compile failure that arose
// for resources with state upgrades plus an object-like attribute. The gate
// evaluates the actual prior schema for each upgrade so that a future enhancement
// to priorSchemaForUpgrade that models attribute additions (omitting newer
// attributes from the prior schema) will correctly re-enable the attr import
// instead of producing "imported and not used".
func stateUpgradeNeedsAttr(r ir.ResourceIR) bool {
	for _, upgrade := range r.StateUpgrades {
		priorSchema := priorSchemaForUpgrade(r, upgrade)

		priorNames := make(map[string]struct{}, len(priorSchema.Attributes))
		for _, attr := range priorSchema.Attributes {
			priorNames[attr.Name] = struct{}{}
		}
		priorBlockNames := make(map[string]struct{}, len(priorSchema.Blocks))
		for _, block := range priorSchema.Blocks {
			priorBlockNames[block.Name] = struct{}{}
		}

		// The attribute else-branch emits nullValueForType, which references
		// attr.Type only when the attribute's schema is object-like or contains an
		// object-like collection element.
		for _, attr := range r.Schema.Attributes {
			if skipAttrForModel(attr) {
				continue
			}
			sourceName := attr.Name
			if oldName, ok := reverseRename(upgrade.Renames, attr.Name); ok {
				sourceName = oldName
			}
			if _, ok := priorNames[sourceName]; ok {
				continue
			}
			if schemaReferencesAttr(attr.Schema) {
				return true
			}
		}

		// The block else-branch emits nullValueForBlock, which always builds an
		// ObjectType with a map[string]attr.Type, so any missing block references attr.
		for _, block := range r.Schema.Blocks {
			if _, ok := priorBlockNames[block.Name]; !ok {
				return true
			}
		}
	}
	return false
}

// schemaReferencesAttr reports whether nullValueForType for the given schema
// would emit an attr.Type reference (directly or transitively). nullValueForType
// references attr.Type for object-like schemas and for collections whose element
// type transitively references attr; primitive and dynamic schemas do not.
func schemaReferencesAttr(s ir.SchemaIR) bool {
	if s.Collection != nil {
		return schemaReferencesAttr(s.Collection.ElementType)
	}
	return isObjectLike(s)
}

// resourceSchemaVersion returns the schema version to emit for the resource.
// It prefers the explicit SchemaVersion value and falls back to the maximum
// from-version plus one when state upgrades are configured without an explicit
// version.
//
// The inferred-version path is only meaningful because validateStateUpgrades
// has already enforced that the configured upgrades form a contiguous prefix
// starting at version 0. Without that guarantee, max(from)+1 would not
// necessarily describe the resource's actual current schema version.
func resourceSchemaVersion(r ir.ResourceIR) int64 {
	if r.SchemaVersion > 0 {
		return int64(r.SchemaVersion)
	}
	var maxFrom int64
	for _, u := range r.StateUpgrades {
		if v := int64(u.FromVersion); v > maxFrom {
			maxFrom = v
		}
	}
	if maxFrom > 0 || len(r.StateUpgrades) > 0 {
		return maxFrom + 1
	}
	return 0
}

// validateStateUpgrades checks that a resource's configured state upgrades can be
// safely emitted. It verifies:
//   - from versions are non-negative, unique, and form a contiguous prefix
//     starting at 0
//   - when SchemaVersion is explicit, it equals len(state_upgrades)
//   - every rename value refers to an attribute in the current schema
//   - reverse renames do not produce duplicate prior attribute names
//   - the same attribute is not renamed in two different upgrade steps unless
//     it forms part of a valid chain (keys and values are each unique across
//     all upgrades)
func validateStateUpgrades(r ir.ResourceIR) error {
	if len(r.StateUpgrades) == 0 {
		return nil
	}

	currentNames := currentAttributeNames(r)
	currentBlockNames := currentBlockNames(r)
	seenFrom := make(map[int]struct{}, len(r.StateUpgrades))
	fromVersions := make([]int, 0, len(r.StateUpgrades))
	var maxFrom int64

	allKeys := make(map[string]int)
	allValues := make(map[string]int)
	allBlockKeys := make(map[string]int)
	allBlockValues := make(map[string]int)

	for i, upgrade := range r.StateUpgrades {
		if err := validateStateUpgradeMeta(i, upgrade, seenFrom, &maxFrom); err != nil {
			return err
		}
		seenFrom[upgrade.FromVersion] = struct{}{}
		fromVersions = append(fromVersions, upgrade.FromVersion)

		if err := validateStateUpgradeRenames(i, upgrade, currentNames, allKeys, allValues); err != nil {
			return err
		}
		if err := validateStateUpgradeBlockChanges(i, upgrade, currentNames, currentBlockNames, allBlockKeys, allBlockValues); err != nil {
			return err
		}
		if err := validateStateUpgradeReverseNames(i, upgrade, currentNames); err != nil {
			return err
		}
		if err := validateStateUpgradePriorNames(i, upgrade, currentNames, currentBlockNames); err != nil {
			return err
		}
	}

	return validateStateUpgradeVersionChain(r, fromVersions, maxFrom)
}

func currentAttributeNames(r ir.ResourceIR) map[string]struct{} {
	currentNames := make(map[string]struct{}, len(r.Schema.Attributes))
	for _, attr := range r.Schema.Attributes {
		currentNames[attr.Name] = struct{}{}
	}
	return currentNames
}

func currentBlockNames(r ir.ResourceIR) map[string]struct{} {
	names := make(map[string]struct{}, len(r.Schema.Blocks))
	for _, block := range r.Schema.Blocks {
		names[block.Name] = struct{}{}
	}
	return names
}

func validateStateUpgradeMeta(i int, upgrade ir.StateUpgradeIR, seenFrom map[int]struct{}, maxFrom *int64) error {
	if upgrade.FromVersion < 0 {
		return fmt.Errorf("state upgrade %d: from_version must be non-negative", i)
	}
	if _, dup := seenFrom[upgrade.FromVersion]; dup {
		return fmt.Errorf("state upgrade %d: from_version %d is duplicated", i, upgrade.FromVersion)
	}
	if v := int64(upgrade.FromVersion); v > *maxFrom {
		*maxFrom = v
	}
	return nil
}

func validateStateUpgradeRenames(i int, upgrade ir.StateUpgradeIR, currentNames map[string]struct{}, allKeys, allValues map[string]int) error {
	valuesSeen := make(map[string]struct{})
	for oldName, newName := range upgrade.Renames {
		if strings.TrimSpace(oldName) == "" {
			return fmt.Errorf("state upgrade %d: rename old name must not be empty", i)
		}
		if strings.TrimSpace(newName) == "" {
			return fmt.Errorf("state upgrade %d: rename value for %q must not be empty", i, oldName)
		}
		if _, ok := currentNames[newName]; !ok {
			return fmt.Errorf("state upgrade %d: rename value %q is not a current schema attribute", i, newName)
		}
		if _, dup := valuesSeen[newName]; dup {
			return fmt.Errorf("state upgrade %d: multiple renames target current attribute %q", i, newName)
		}
		valuesSeen[newName] = struct{}{}

		if prev, ok := allKeys[oldName]; ok {
			return fmt.Errorf("state upgrade %d: rename key %q already used in state upgrade %d", i, oldName, prev)
		}
		allKeys[oldName] = i
		if prev, ok := allValues[newName]; ok {
			return fmt.Errorf("state upgrade %d: rename value %q already targeted in state upgrade %d", i, newName, prev)
		}
		allValues[newName] = i
	}
	return nil
}

func validateStateUpgradeReverseNames(i int, upgrade ir.StateUpgradeIR, currentNames map[string]struct{}) error {
	reverse := make(map[string]string, len(upgrade.Renames))
	for oldName, newName := range upgrade.Renames {
		reverse[newName] = oldName
	}

	priorNames := make(map[string]struct{}, len(currentNames))
	for name := range currentNames {
		priorName := name
		if oldName, ok := reverse[name]; ok {
			priorName = oldName
		}
		if _, dup := priorNames[priorName]; dup {
			return fmt.Errorf("state upgrade %d: reverse rename produces duplicate prior attribute name %q", i, priorName)
		}
		priorNames[priorName] = struct{}{}
	}
	return nil
}

// validateStateUpgradeBlockChanges validates block renames, added
// attributes/blocks, and removed attributes/blocks for a single upgrade step.
//
// Renames and block renames are mutually exclusive with additions: a field that
// was renamed is not "added", and a field that was removed is not a rename
// source. Removed names must not refer to current schema members (they are
// gone). Added names must refer to current schema members (they are new here).
// Prior-name uniqueness across attributes and blocks (renames + removals) is
// enforced separately by validateStateUpgradePriorNames.
func validateStateUpgradeBlockChanges(
	i int, upgrade ir.StateUpgradeIR,
	currentAttrs, currentBlocks map[string]struct{},
	allBlockKeys, allBlockValues map[string]int,
) error {
	// Block renames: values must be current blocks; keys/values unique within and
	// across steps.
	valuesSeen := make(map[string]struct{})
	for oldName, newName := range upgrade.BlockRenames {
		if strings.TrimSpace(oldName) == "" {
			return fmt.Errorf("state upgrade %d: block rename old name must not be empty", i)
		}
		if strings.TrimSpace(newName) == "" {
			return fmt.Errorf("state upgrade %d: block rename value for %q must not be empty", i, oldName)
		}
		if _, ok := currentBlocks[newName]; !ok {
			return fmt.Errorf("state upgrade %d: block rename value %q is not a current schema block", i, newName)
		}
		if _, dup := valuesSeen[newName]; dup {
			return fmt.Errorf("state upgrade %d: multiple block renames target current block %q", i, newName)
		}
		valuesSeen[newName] = struct{}{}
		if prev, ok := allBlockKeys[oldName]; ok {
			return fmt.Errorf("state upgrade %d: block rename key %q already used in state upgrade %d", i, oldName, prev)
		}
		allBlockKeys[oldName] = i
		if prev, ok := allBlockValues[newName]; ok {
			return fmt.Errorf("state upgrade %d: block rename value %q already targeted in state upgrade %d", i, newName, prev)
		}
		allBlockValues[newName] = i
	}

	// Added attributes: must be current attributes; must not also be an attribute
	// rename target (a renamed field is not "added"); no duplicates.
	if err := validateAddedRemovedNames(i, upgrade.AddedAttributes, currentAttrs, true, valuesAsSet(upgrade.Renames), "added_attributes", "attribute"); err != nil {
		return err
	}
	// Removed attributes: must NOT be current attributes (they are gone); must not
	// also be an attribute rename source; no duplicates.
	if err := validateAddedRemovedNames(i, upgrade.RemovedAttributes, currentAttrs, false, keysAsSet(upgrade.Renames), "removed_attributes", "attribute"); err != nil {
		return err
	}
	// Added blocks: must be current blocks; must not also be a block rename target.
	if err := validateAddedRemovedNames(i, upgrade.AddedBlocks, currentBlocks, true, valuesAsSet(upgrade.BlockRenames), "added_blocks", "block"); err != nil {
		return err
	}
	// Removed blocks: must NOT be current blocks; must not also be a block rename source.
	return validateAddedRemovedNames(i, upgrade.RemovedBlocks, currentBlocks, false, keysAsSet(upgrade.BlockRenames), "removed_blocks", "block")
}

// validateAddedRemovedNames checks the entries of an added- or removed-names
// list. When mustExist is true, each name must be present in current (added
// fields must be current members); when false, each name must NOT be present in
// current (removed fields are gone). Each name must not also appear in exclude
// (the rename counterpart) and must not be duplicated within the list.
func validateAddedRemovedNames(i int, names []string, current map[string]struct{}, mustExist bool, exclude map[string]struct{}, field, kind string) error {
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("state upgrade %d: %s contains empty name", i, field)
		}
		if _, dup := seen[name]; dup {
			return fmt.Errorf("state upgrade %d: %s contains duplicate %q", i, field, name)
		}
		seen[name] = struct{}{}
		if mustExist {
			if _, ok := current[name]; !ok {
				return fmt.Errorf("state upgrade %d: %s %q is not a current schema %s", i, field, name, kind)
			}
		} else {
			if _, ok := current[name]; ok {
				return fmt.Errorf("state upgrade %d: %s %q is still a current schema %s; a removed field must not be present in the current schema", i, field, name, kind)
			}
		}
		if exclude != nil {
			if _, ok := exclude[name]; ok {
				return fmt.Errorf("state upgrade %d: %s %q is also a rename %s; a field is either renamed or added/removed, not both", i, field, name, kind)
			}
		}
	}
	return nil
}

func valuesAsSet(m map[string]string) map[string]struct{} {
	out := make(map[string]struct{}, len(m))
	for _, v := range m {
		out[v] = struct{}{}
	}
	return out
}

func keysAsSet(m map[string]string) map[string]struct{} {
	out := make(map[string]struct{}, len(m))
	for k := range m {
		out[k] = struct{}{}
	}
	return out
}

// validateStateUpgradePriorNames enforces that the prior attribute names and
// prior block names produced by priorSchemaForUpgrade are unique. Prior
// attribute names are derived from current attributes (renamed or unchanged)
// plus removed attributes plus removed blocks (removed blocks are synthesized
// as Dynamic prior attributes). Prior block names are derived from current
// blocks (block-renamed or unchanged). Duplicate prior names would produce an
// ambiguous prior schema and model struct.
func validateStateUpgradePriorNames(i int, upgrade ir.StateUpgradeIR, currentAttrs, currentBlocks map[string]struct{}) error {
	priorAttrs := make(map[string]struct{})
	for attrName := range currentAttrs {
		priorName := attrName
		if oldName, ok := reverseRename(upgrade.Renames, attrName); ok {
			priorName = oldName
		}
		// Added attributes are omitted from the prior schema.
		if containsString(upgrade.AddedAttributes, attrName) {
			continue
		}
		if _, dup := priorAttrs[priorName]; dup {
			return fmt.Errorf("state upgrade %d: prior attribute name %q is duplicated", i, priorName)
		}
		priorAttrs[priorName] = struct{}{}
	}
	// Removed attributes and removed blocks both become Dynamic prior attributes.
	for _, name := range upgrade.RemovedAttributes {
		if _, dup := priorAttrs[name]; dup {
			return fmt.Errorf("state upgrade %d: removed attribute %q collides with another prior attribute name", i, name)
		}
		priorAttrs[name] = struct{}{}
	}
	for _, name := range upgrade.RemovedBlocks {
		if _, dup := priorAttrs[name]; dup {
			return fmt.Errorf("state upgrade %d: removed block %q collides with another prior attribute name (removed blocks become prior attributes)", i, name)
		}
		priorAttrs[name] = struct{}{}
	}

	priorBlocks := make(map[string]struct{})
	for blockName := range currentBlocks {
		priorName := blockName
		if oldName, ok := reverseRename(upgrade.BlockRenames, blockName); ok {
			priorName = oldName
		}
		if containsString(upgrade.AddedBlocks, blockName) {
			continue
		}
		if _, dup := priorBlocks[priorName]; dup {
			return fmt.Errorf("state upgrade %d: prior block name %q is duplicated", i, priorName)
		}
		priorBlocks[priorName] = struct{}{}
	}
	return nil
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func validateStateUpgradeVersionChain(r ir.ResourceIR, fromVersions []int, maxFrom int64) error {
	sort.Ints(fromVersions)
	for i, v := range fromVersions {
		if v != i {
			return fmt.Errorf("state upgrades have gap or unexpected from_version %d at position %d (expected %d); upgrades must cover contiguous versions 0..N", v, i, i)
		}
	}

	if r.SchemaVersion > 0 && int64(r.SchemaVersion) != maxFrom+1 {
		return fmt.Errorf("schema_version %d does not match the inferred current version %d from state upgrades", r.SchemaVersion, maxFrom+1)
	}

	return nil
}

// upgradeStateMethod returns the UpgradeState method for a resource, or nil when
// no state upgrades are configured. The generated method satisfies
// resource.ResourceWithUpgradeState.
func upgradeStateMethod(r ir.ResourceIR) ast.Decl {
	if !hasStateUpgrades(r) {
		return nil
	}

	return astgen.MethodDecl(
		"UpgradeState", "r", astgen.StarExpr(astgen.Ident(resourceStructName(r))),
		astgen.Params(
			astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
		),
		astgen.Results(astgen.Field("", astgen.MapType(astgen.Ident("int64"), astgen.QualExpr("resource", "StateUpgrader")), "")),
		astgen.Block(astgen.Return(stateUpgraderMap(r))),
	)
}

// stateUpgraderMap builds map[int64]resource.StateUpgrader{...} for the
// configured upgrades. Upgrades are sorted and deduplicated by FromVersion so
// the generated map literal has deterministic, unique keys.
func stateUpgraderMap(r ir.ResourceIR) ast.Expr {
	upgrades := sortedUniqueUpgrades(r.StateUpgrades)
	elems := make([]ast.Expr, 0, len(upgrades))
	for _, upgrade := range upgrades {
		elems = append(elems, astgen.KeyValueExpr(
			astgen.Call(astgen.Ident("int64"), astgen.IntLit(upgrade.FromVersion)),
			stateUpgraderEntry(r, upgrade),
		))
	}
	return astgen.CompositeLit(
		astgen.MapType(astgen.Ident("int64"), astgen.QualExpr("resource", "StateUpgrader")),
		elems...,
	)
}

// sortedUniqueUpgrades returns the state upgrades sorted by FromVersion with
// duplicates removed. The config validator already rejects duplicates, but
// deduplication here keeps generated output well-formed regardless of IR source.
func sortedUniqueUpgrades(upgrades []ir.StateUpgradeIR) []ir.StateUpgradeIR {
	if len(upgrades) == 0 {
		return nil
	}
	seen := make(map[int]struct{}, len(upgrades))
	out := make([]ir.StateUpgradeIR, 0, len(upgrades))
	for _, u := range upgrades {
		if _, ok := seen[u.FromVersion]; ok {
			continue
		}
		seen[u.FromVersion] = struct{}{}
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FromVersion < out[j].FromVersion })
	return out
}

// stateUpgraderEntry builds a single resource.StateUpgrader value.
func stateUpgraderEntry(r ir.ResourceIR, upgrade ir.StateUpgradeIR) ast.Expr {
	priorSchema := priorSchemaForUpgrade(r, upgrade)
	attrElems := make([]ast.Expr, 0, len(priorSchema.Attributes))
	for _, attr := range priorSchema.Attributes {
		attrElems = append(attrElems, astgen.KeyValueExpr(
			astgen.Lit(attr.Name),
			resourceAttributeExpr(attr),
		))
	}
	priorSchemaElems := []ast.Expr{
		astgen.KeyValue("Attributes", astgen.CompositeLit(
			astgen.MapType(astgen.Ident("string"), astgen.QualExpr("schema", "Attribute")),
			attrElems...,
		)),
	}
	if len(priorSchema.Blocks) > 0 {
		blockElems := make([]ast.Expr, 0, len(priorSchema.Blocks))
		for _, block := range priorSchema.Blocks {
			blockElems = append(blockElems, astgen.KeyValueExpr(
				astgen.Lit(block.Name),
				resourceBlockExpr(block, ""),
			))
		}
		priorSchemaElems = append(priorSchemaElems, astgen.KeyValue("Blocks", astgen.CompositeLit(
			astgen.MapType(astgen.Ident("string"), astgen.QualExpr("schema", "Block")),
			blockElems...,
		)))
	}
	return astgen.CompositeLit(
		astgen.QualExpr("resource", "StateUpgrader"),
		astgen.KeyValue("PriorSchema", astgen.UnaryPtr(astgen.CompositeLit(
			astgen.QualExpr("schema", "Schema"),
			priorSchemaElems...,
		))),
		astgen.KeyValue("StateUpgrader", stateUpgraderFunc(r, upgrade, priorSchema)),
	)
}

// priorSchemaForUpgrade computes the prior object schema for a single upgrade
// step by applying the reverse of the configured renames to the current
// resource schema, and modeling block-level changes:
//
//   - Attribute renames and block renames are reversed so the prior schema
//     carries the old names.
//   - AddedAttributes/AddedBlocks are omitted from the prior schema (they did
//     not exist in prior state); the upgrader null-initializes them.
//   - RemovedAttributes/RemovedBlocks are synthesized into the prior schema as
//     Dynamic-typed attributes carrying the old name. Dynamic accepts any
//     prior value, so historical state decodes regardless of the dropped
//     field's original shape, and the upgrader simply does not copy them into
//     the upgraded model (which has no such field).
//
// Removed blocks are represented as Dynamic prior attributes (not blocks)
// because the generator does not know the removed block's prior object shape;
// Dynamic is the only type that safely decodes an arbitrary prior value. The
// prior-name uniqueness that makes this well-formed is enforced by
// validateStateUpgradePriorNames.
func priorSchemaForUpgrade(r ir.ResourceIR, upgrade ir.StateUpgradeIR) ir.ObjectSchemaIR {
	addedAttrs := stringSet(upgrade.AddedAttributes)
	addedBlocks := stringSet(upgrade.AddedBlocks)

	attrs := make([]ir.AttributeIR, 0, len(r.Schema.Attributes)+len(upgrade.RemovedAttributes)+len(upgrade.RemovedBlocks))
	for _, attr := range r.Schema.Attributes {
		if _, added := addedAttrs[attr.Name]; added {
			continue // added in this version; absent from prior state
		}
		attrCopy := attr
		if oldName, ok := reverseRename(upgrade.Renames, attr.Name); ok {
			attrCopy.Name = oldName
		}
		attrs = append(attrs, attrCopy)
	}
	// Removed attributes and removed blocks both become Dynamic prior attributes
	// so historical state decodes; the upgrader drops them (no current field).
	for _, name := range upgrade.RemovedAttributes {
		attrs = append(attrs, removedFieldAttribute(name))
	}
	for _, name := range upgrade.RemovedBlocks {
		attrs = append(attrs, removedFieldAttribute(name))
	}

	blocks := make([]ir.BlockIR, 0, len(r.Schema.Blocks))
	for _, block := range r.Schema.Blocks {
		if _, added := addedBlocks[block.Name]; added {
			continue // added in this version; absent from prior state
		}
		blockCopy := block
		if oldName, ok := reverseRename(upgrade.BlockRenames, block.Name); ok {
			blockCopy.Name = oldName
		}
		blocks = append(blocks, blockCopy)
	}

	return ir.ObjectSchemaIR{
		Attributes: attrs,
		Blocks:     blocks,
	}
}

// removedFieldAttribute returns a Dynamic-typed attribute used to represent a
// removed attribute or block in the prior schema. Dynamic accepts any prior
// value, so historical state decodes regardless of the dropped field's
// original shape; the upgrader simply does not copy it into the upgraded model.
func removedFieldAttribute(name string) ir.AttributeIR {
	return ir.AttributeIR{
		Name:     name,
		Optional: true,
		Computed: true,
		Schema:   ir.SchemaIR{Type: ir.TypeDynamic},
	}
}

func stringSet(items []string) map[string]struct{} {
	out := make(map[string]struct{}, len(items))
	for _, s := range items {
		out[s] = struct{}{}
	}
	return out
}

// stateUpgraderFunc builds the StateUpgrader closure that decodes the prior
// state into a versioned model, applies renames, and writes the upgraded model
// back to the response state. The priorSchema argument is the schema against
// which the prior state is decoded; callers normally compute it with
// priorSchemaForUpgrade.
func stateUpgraderFunc(r ir.ResourceIR, upgrade ir.StateUpgradeIR, priorSchema ir.ObjectSchemaIR) ast.Expr {
	priorModel := priorModelName(r, upgrade)
	model := resourceModelName(r)

	priorNames := make(map[string]struct{}, len(priorSchema.Attributes))
	for _, attr := range priorSchema.Attributes {
		priorNames[attr.Name] = struct{}{}
	}
	priorBlockNames := make(map[string]struct{}, len(priorSchema.Blocks))
	for _, block := range priorSchema.Blocks {
		priorBlockNames[block.Name] = struct{}{}
	}

	stmts := []ast.Stmt{
		astgen.VarDecl("prior", priorModel, nil),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "Append"),
			astgen.Ellipsis(astgen.Call(
				astgen.Selector(astgen.Selector(astgen.Ident("req"), "State"), "Get"),
				astgen.Ident("ctx"),
				astgen.UnaryPtr(astgen.Ident("prior")),
			)),
		)),
		astgen.If(
			astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "HasError")),
			astgen.Return(),
		),
		astgen.VarDecl("upgraded", model, nil),
	}

	// Top-level block renames, additions, and removals are modeled by
	// priorSchemaForUpgrade. A renamed block is decoded into the prior model
	// under its old name using the current block's schema, which assumes the
	// block's internal schema did not change across the rename. Surface that
	// assumption as a runtime warning so practitioners know intra-block schema
	// changes (added/removed/renamed attributes within a block) are not modeled.
	if len(upgrade.BlockRenames) > 0 {
		stmts = append(stmts, astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "AddWarning"),
			astgen.Lit("Block rename assumes shape invariance"),
			astgen.Lit("A block is renamed by this state upgrade. The prior block is decoded against the current block schema, which assumes the block's internal attributes did not change. Nested (intra-block) schema changes are not modeled; if a block's shape changed, provide a custom StateUpgrader."),
		)))
	}

	// Defensive null-initialization: if a current attribute is absent from the
	// prior schema, initialize it to its null value so the upgraded state remains
	// valid. In production, priorSchemaForUpgrade copies every current attribute
	// (renamed) into the prior schema, so this branch does not fire and attribute
	// additions are NOT modeled — decoding old state that genuinely lacks a newer
	// attribute surfaces as framework diagnostics at upgrade time. The branch is
	// retained so custom upgraders that construct a prior schema omitting newer
	// attributes (or a future priorSchemaForUpgrade that models additions)
	// null-initialize correctly. Required new fields must be populated by a
	// subsequent apply.
	for _, attr := range r.Schema.Attributes {
		if skipAttrForModel(attr) {
			continue
		}
		currentName := attr.Name
		sourceName := currentName
		if oldName, ok := reverseRename(upgrade.Renames, currentName); ok {
			sourceName = oldName
		}

		currentField := goFieldName(currentName)
		if _, ok := priorNames[sourceName]; ok {
			stmts = append(stmts, astgen.AssignStmt(
				[]ast.Expr{astgen.Selector(astgen.Ident("upgraded"), currentField)},
				[]ast.Expr{astgen.Selector(astgen.Ident("prior"), goFieldName(sourceName))},
				token.ASSIGN,
			))
		} else {
			stmts = append(stmts, astgen.AssignStmt(
				[]ast.Expr{astgen.Selector(astgen.Ident("upgraded"), currentField)},
				[]ast.Expr{nullValueForType(attr.Schema)},
				token.ASSIGN,
			))
		}
	}

	// Copy each current block value from the prior model. A block whose current
	// name maps from a prior (old) name via BlockRenames is copied from the old
	// field; a block present in the prior schema under its current name is copied
	// directly; a block absent from the prior schema (added in this version) is
	// null-initialized. Removed blocks are not iterated here (they are not in the
	// current schema) and are dropped, having been decoded into a Dynamic prior
	// field by priorSchemaForUpgrade.
	for _, block := range r.Schema.Blocks {
		currentField := goFieldName(block.Name)
		sourceName := block.Name
		if oldName, ok := reverseRename(upgrade.BlockRenames, block.Name); ok {
			sourceName = oldName
		}
		if _, ok := priorBlockNames[sourceName]; ok {
			stmts = append(stmts, astgen.AssignStmt(
				[]ast.Expr{astgen.Selector(astgen.Ident("upgraded"), currentField)},
				[]ast.Expr{astgen.Selector(astgen.Ident("prior"), goFieldName(sourceName))},
				token.ASSIGN,
			))
		} else {
			stmts = append(stmts, astgen.AssignStmt(
				[]ast.Expr{astgen.Selector(astgen.Ident("upgraded"), currentField)},
				[]ast.Expr{nullValueForBlock(block)},
				token.ASSIGN,
			))
		}
	}

	stmts = append(stmts, astgen.ExprStmt(astgen.Call(
		astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "Append"),
		astgen.Ellipsis(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "State"), "Set"),
			astgen.Ident("ctx"),
			astgen.UnaryPtr(astgen.Ident("upgraded")),
		)),
	)))

	return astgen.FuncLit(
		astgen.FuncType(
			astgen.Params(
				astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
				astgen.Field("req", astgen.QualExpr("resource", "UpgradeStateRequest"), ""),
				astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("resource", "UpgradeStateResponse")), ""),
			),
			astgen.Results(),
		),
		astgen.Block(stmts...),
	)
}

// reverseRename returns the old name that maps to the given current name.
func reverseRename(renames map[string]string, currentName string) (string, bool) {
	for oldName, newName := range renames {
		if newName == currentName {
			return oldName, true
		}
	}
	return "", false
}

// priorModelName returns the generated prior model struct name for an upgrade.
func priorModelName(r ir.ResourceIR, upgrade ir.StateUpgradeIR) string {
	return fmt.Sprintf("%sV%d", resourceModelName(r), upgrade.FromVersion)
}

// generatePriorModelStructs emits any versioned model structs needed by the
// configured state upgrades. The emitted prior model includes both attribute
// and nested block fields so that block state can be decoded and transferred
// during the upgrade closure.
func generatePriorModelStructs(f *astgen.File, r ir.ResourceIR) {
	seen := make(map[string]struct{})
	for _, upgrade := range r.StateUpgrades {
		name := priorModelName(r, upgrade)
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}

		priorSchema := priorSchemaForUpgrade(r, upgrade)
		f.AddCommentf("%s describes the prior Terraform state shape for %s schema version %d.", name, resourceStructName(r), upgrade.FromVersion)
		fields := make([]*ast.Field, 0, len(priorSchema.Attributes)+len(priorSchema.Blocks))
		for _, attr := range priorSchema.Attributes {
			if skipAttrForModel(attr) {
				continue
			}
			fields = append(fields, astgen.Field(
				goFieldName(attr.Name),
				modelFieldType(attr),
				modelFieldTags(attr),
			))
		}
		for _, block := range priorSchema.Blocks {
			fields = append(fields, astgen.Field(
				goFieldName(block.Name),
				blockModelFieldType(block),
				fmt.Sprintf("tfsdk:%q", block.Name),
			))
		}
		f.AddDecl(astgen.TypeDecl(name, astgen.StructType(fields...)))
	}
}

// nullValueForType returns the appropriate types.XNull(...) expression for an
// IR schema. It is used to initialize new attributes that did not exist in the
// prior schema.
func nullValueForType(s ir.SchemaIR) ast.Expr {
	switch {
	case s.Collection != nil:
		elemType := schemaTypeExpr(s.Collection.ElementType)
		switch s.Collection.Kind {
		case ir.List:
			return astgen.Call(astgen.QualExpr("types", "ListNull"), elemType)
		case ir.Set:
			return astgen.Call(astgen.QualExpr("types", "SetNull"), elemType)
		case ir.Map:
			return astgen.Call(astgen.QualExpr("types", "MapNull"), elemType)
		}
	case isObjectLike(s):
		return astgen.Call(astgen.QualExpr("types", "ObjectNull"), objectAttrTypesMap(ir.ObjectSchemaIR{Attributes: s.Attributes, Blocks: s.Blocks}))
	case s.Type == ir.TypeString:
		return astgen.Call(astgen.QualExpr("types", "StringNull"))
	case s.Type == ir.TypeInt:
		return astgen.Call(astgen.QualExpr("types", "Int64Null"))
	case s.Type == ir.TypeFloat:
		return astgen.Call(astgen.QualExpr("types", "Float64Null"))
	case s.Type == ir.TypeBool:
		return astgen.Call(astgen.QualExpr("types", "BoolNull"))
	case s.Type == ir.TypeDynamic:
		return astgen.Call(astgen.QualExpr("types", "DynamicNull"))
	}
	// Fallback for unrecognized schemas. This default preserves backwards
	// compatibility but may produce a type mismatch if a new IR schema variant is
	// added without a matching null constructor. Consider making this case a
	// build-time error once all schema shapes are explicitly handled.
	return astgen.Call(astgen.QualExpr("types", "StringNull"))
}

// nullValueForBlock returns the appropriate types.XNull(...) expression for a
// nested block based on its nesting mode. It is used to initialize block fields
// that are present in the current schema but missing from the prior schema.
func nullValueForBlock(block ir.BlockIR) ast.Expr {
	elemType := astgen.CompositeLit(astgen.QualExpr("types", "ObjectType"), astgen.KeyValue("AttrTypes", objectAttrTypesMap(block.Schema)))
	switch block.NestingMode {
	case ir.NestingList:
		return astgen.Call(astgen.QualExpr("types", "ListNull"), elemType)
	case ir.NestingSet:
		return astgen.Call(astgen.QualExpr("types", "SetNull"), elemType)
	default:
		return astgen.Call(astgen.QualExpr("types", "ObjectNull"), elemType)
	}
}

// schemaTypeExpr returns the Terraform Plugin Framework attr.Type expression for
// an IR schema. It is used when constructing collection/object null values.
func schemaTypeExpr(s ir.SchemaIR) ast.Expr {
	if s.Collection != nil {
		elem := schemaTypeExpr(s.Collection.ElementType)
		switch s.Collection.Kind {
		case ir.List:
			return astgen.CompositeLit(astgen.QualExpr("types", "ListType"), astgen.KeyValue("ElemType", elem))
		case ir.Set:
			return astgen.CompositeLit(astgen.QualExpr("types", "SetType"), astgen.KeyValue("ElemType", elem))
		case ir.Map:
			return astgen.CompositeLit(astgen.QualExpr("types", "MapType"), astgen.KeyValue("ElemType", elem))
		}
	}
	if isObjectLike(s) {
		return astgen.CompositeLit(astgen.QualExpr("types", "ObjectType"), astgen.KeyValue("AttrTypes", objectAttrTypesMap(ir.ObjectSchemaIR{Attributes: s.Attributes, Blocks: s.Blocks})))
	}
	return primitiveAttrType(s.Type)
}

// objectAttrTypesMap returns map[string]attr.Type{...} for an object-like IR
// schema. It is used to construct types.ObjectNull values.
func objectAttrTypesMap(s ir.ObjectSchemaIR) ast.Expr {
	elems := make([]ast.Expr, 0, len(s.Attributes)+len(s.Blocks))
	for _, attr := range s.Attributes {
		elems = append(elems, astgen.KeyValueExpr(
			astgen.Lit(attr.Name),
			schemaTypeExpr(attr.Schema),
		))
	}
	for _, block := range s.Blocks {
		elems = append(elems, astgen.KeyValueExpr(
			astgen.Lit(block.Name),
			blockAttrTypeExpr(block),
		))
	}
	return astgen.CompositeLit(
		astgen.MapType(astgen.Ident("string"), astgen.QualExpr("attr", "Type")),
		elems...,
	)
}

// blockAttrTypeExpr returns the attr.Type expression for a nested block as it
// appears inside an Object attr.Type map.
func blockAttrTypeExpr(block ir.BlockIR) ast.Expr {
	attrTypes := objectAttrTypesMap(block.Schema)
	elemType := astgen.CompositeLit(astgen.QualExpr("types", "ObjectType"), astgen.KeyValue("AttrTypes", attrTypes))
	switch block.NestingMode {
	case ir.NestingList:
		return astgen.CompositeLit(astgen.QualExpr("types", "ListType"), astgen.KeyValue("ElemType", elemType))
	case ir.NestingSet:
		return astgen.CompositeLit(astgen.QualExpr("types", "SetType"), astgen.KeyValue("ElemType", elemType))
	default:
		return elemType
	}
}
