package transformer

import (
	"fmt"
	"strings"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/config"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// ApplyOverrides applies user-supplied generator.yaml overrides to an inferred
// ProviderIR. It mutates the supplied ProviderIR in place.
//
// Applied overrides include naming conventions, per-resource settings (names,
// id_attribute, import_format, timeouts, computed/sensitive/write_only/force_new
// attributes, skip), per-datasource/action/ephemeral/list/function overrides,
// global timeouts, and polymorphism strategy configuration.
//
// Override precedence: per-entity overrides are applied in the order they appear
// in generator.yaml. For resources, a matching override with skip: true removes
// the resource and stops processing later overrides for that resource, but any
// earlier matching overrides have already been applied.
func ApplyOverrides(provider *ir.ProviderIR, cfg *config.Config) error {
	if provider == nil || cfg == nil {
		return nil
	}

	// Apply naming conventions first so that prefix/suffix transforms are
	// applied to inferred names; explicit per-entity overrides may then rename
	// them further.
	applyNamingOverrides(provider, cfg.Naming)

	// Per-entity overrides.
	if err := applyResourceOverrides(provider, cfg.ResourceOverrides); err != nil {
		return err
	}
	applyDatasourceOverrides(provider, cfg.DatasourceOverrides)
	applyActionOverrides(provider, cfg.ActionOverrides)
	applyEphemeralOverrides(provider, cfg.EphemeralOverrides)
	applyListResourceOverrides(provider, cfg.ListResourceOverrides)
	applyFunctionOverrides(provider, cfg.FunctionOverrides)

	// Global fallback overrides.
	applyGlobalTimeouts(provider, cfg.GlobalTimeouts)

	// Polymorphism overrides.
	applyPolymorphismOverrides(provider, cfg.Polymorphism)

	return nil
}

// applyNamingOverrides applies global naming prefix/suffix settings to the
// names stored in the IR. The naming transform setting is intentionally left as
// a no-op here because inferred Terraform names are already normalized to
// snake_case during inference; applying a different convention would require a
// full re-normalization pass that is out of scope for override application.
func applyNamingOverrides(provider *ir.ProviderIR, naming *config.NamingConfig) {
	if naming == nil {
		return
	}

	for i := range provider.Resources {
		r := &provider.Resources[i]
		r.Name = withPrefixSuffix(r.Name, naming.ResourcePrefix, naming.ResourceSuffix)
		if r.TypeName != "" {
			r.TypeName = withPrefixSuffix(r.TypeName, naming.ResourcePrefix, naming.ResourceSuffix)
		}
		if r.FullName != "" {
			r.FullName = withPrefixSuffix(r.FullName, naming.ResourcePrefix, naming.ResourceSuffix)
		}
	}

	for i := range provider.DataSources {
		ds := &provider.DataSources[i]
		// NOTE: data source naming currently only supports a prefix. Adding a
		// DatasourceSuffix field to NamingConfig would require updating this loop.
		ds.Name = withPrefixSuffix(ds.Name, naming.DatasourcePrefix, "")
		if ds.TypeName != "" {
			ds.TypeName = withPrefixSuffix(ds.TypeName, naming.DatasourcePrefix, "")
		}
		if ds.FullName != "" {
			ds.FullName = withPrefixSuffix(ds.FullName, naming.DatasourcePrefix, "")
		}
	}
}

func withPrefixSuffix(name, prefix, suffix string) string {
	return prefix + name + suffix
}

// applyResourceOverrides applies resource_overrides entries to matching
// resources.
//
// Matching overrides are processed in order. If a resource matches multiple
// overrides, each matching override is applied sequentially. A skip override
// removes the resource regardless of any prior mutations applied by earlier
// matching overrides, and subsequent overrides for that resource are ignored.
func applyResourceOverrides(provider *ir.ProviderIR, overrides []config.ResourceOverride) error {
	if len(overrides) == 0 {
		return nil
	}

	var kept []ir.ResourceIR
	for i := range provider.Resources {
		r := &provider.Resources[i]
		skip := false

		for _, override := range overrides {
			if !resourceMatchesOverride(*r, override) {
				continue
			}

			if override.Skip != nil && *override.Skip {
				skip = true
				break
			}

			applyResourceNameOverride(r, override)
			applyResourceIDOverride(r, override)
			applyResourceImportFormatOverride(r, override)
			applyResourceTimeoutOverride(r, override)
			applyResourceStateUpgradeOverride(r, override)
			if err := applyResourceAttributeOverrides(r, override); err != nil {
				return err
			}
		}

		if !skip {
			kept = append(kept, *r)
		}
	}

	provider.Resources = kept
	return nil
}

func resourceMatchesOverride(r ir.ResourceIR, override config.ResourceOverride) bool {
	if strings.TrimSpace(override.Operation) != "" {
		return operationMatches(r.SourceOperation, r.CRUDMapping.Create, override.Operation)
	}
	if strings.TrimSpace(override.Schema) != "" {
		return nameMatchesIdentity(override.Schema, r.Name, r.TypeName, r.FullName)
	}
	return false
}

// normalizeName trims surrounding whitespace, strips underscores, and lowercases
// a name so that minor formatting differences (e.g. snake_case vs camelCase or
// PascalCase) do not prevent a match. It is used for schema/name and operation
// identity comparisons.
func normalizeName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "_", "")
	return strings.ToLower(s)
}

func nameMatchesIdentity(pattern string, names ...string) bool {
	pattern = normalizeName(pattern)
	if pattern == "" {
		return false
	}
	for _, n := range names {
		if normalizeName(n) == pattern {
			return true
		}
	}
	return false
}

// sourceOperationMatches reports whether an override operation identifier
// matches an entity's source operation. Comparisons are case-insensitive, trim
// surrounding whitespace, and ignore underscores so that OpenAPI operation IDs
// and generator.yaml overrides tolerate formatting differences. Both arguments
// are normalized symmetrically before comparison.
func sourceOperationMatches(sourceOp, overrideOp string) bool {
	normalizedOverride := normalizeName(overrideOp)
	if normalizedOverride == "" {
		return false
	}
	return normalizeName(sourceOp) == normalizedOverride
}

// operationMatches reports whether an override operation identifier matches an
// entity's source operation. The override may be either an OpenAPI operationId
// (matched case-insensitively, ignoring underscores) or a "METHOD /path" form
// (matched against the entity's method and path template). The method+path form
// disambiguates operations that share an operationId — a duplicate operationId
// in the spec — which the operationId form cannot, because it matches every
// operation carrying that operationId.
func operationMatches(sourceOp string, m ir.OperationMappingIR, overrideOp string) bool {
	if sourceOperationMatches(sourceOp, overrideOp) {
		return true
	}
	if m.Method == "" || m.PathTemplate == "" {
		return false
	}
	return sourceOperationMatches(m.Method+" "+m.PathTemplate, overrideOp)
}

// entityOperationMatches reports whether an override operation identifier matches
// an entity. It first compares the entity's source operation (case-insensitive,
// with trimmed whitespace) or its method+path (see operationMatches). If that
// does not match, it falls back to matching the override operation against one
// or more identity strings supplied by the caller (e.g., name, type name, full
// name).
func entityOperationMatches(sourceOp string, m ir.OperationMappingIR, overrideOp string, names ...string) bool {
	if operationMatches(sourceOp, m, overrideOp) {
		return true
	}
	return nameMatchesIdentity(overrideOp, names...)
}

// OverrideMatchesEntity reports whether an override operation identifier matches
// an entity by source operation, method+path, or name identity. It mirrors the
// matching used by ApplyOverrides and is exported so the API layer can decide
// whether an override declares a new construct or modifies an existing one
// (avoiding a duplicate when both would apply).
func OverrideMatchesEntity(sourceOp string, m ir.OperationMappingIR, overrideOp string, names ...string) bool {
	return entityOperationMatches(sourceOp, m, overrideOp, names...)
}

// applyResourceNameOverride overwrites the resource's Name, TypeName, and FullName
// with the override's ResourceName whenever ResourceName is non-empty. This
// unconditionally replaces any name produced by inference or prior overrides.
//
// The Terraform type name is reconstructed as <provider>_<ResourceName> from the
// provider prefix the entity's TypeName already carries (inferred TypeNames are
// always prefixed), so a rename never strips the provider prefix that Terraform
// requires to resolve the resource type (e.g. renaming "purchase_ship" to
// "buy_ship" under provider "space-traders-api" yields "space-traders-api_buy_ship",
// not the unresolvable "buy_ship").
func applyResourceNameOverride(r *ir.ResourceIR, override config.ResourceOverride) {
	if strings.TrimSpace(override.ResourceName) == "" {
		return
	}
	prefix := strings.TrimSuffix(r.TypeName, r.Name)
	r.Name = override.ResourceName
	r.TypeName = prefix + override.ResourceName
	r.FullName = toHumanName(override.ResourceName)
}

func applyResourceIDOverride(r *ir.ResourceIR, override config.ResourceOverride) {
	if strings.TrimSpace(override.IDAttribute) != "" {
		r.IDAttribute = override.IDAttribute
	}
}

// applyResourceImportFormatOverride stores the configured import format on the
// resource whenever ImportFormat is non-empty. Importable is gated by the
// presence of a Read operation, but ImportIDFormat is always recorded so the
// configured value is preserved even when the resource cannot be imported.
func applyResourceImportFormatOverride(r *ir.ResourceIR, override config.ResourceOverride) {
	if strings.TrimSpace(override.ImportFormat) != "" {
		r.ImportIDFormat = override.ImportFormat
		// Only mark the resource as importable when a Read operation is present;
		// otherwise there is no GET-by-ID path to support import.
		if r.CRUDMapping.Read.Method != "" || r.CRUDMapping.Read.PathTemplate != "" {
			r.Importable = true
		}
	}
}

func applyResourceTimeoutOverride(r *ir.ResourceIR, override config.ResourceOverride) {
	if override.Timeouts != nil {
		r.Timeouts = timeoutConfigFromOverride(override.Timeouts)
	}
}

// applyResourceStateUpgradeOverride propagates the configured schema_version and
// state_upgrades from a resource override onto the resource IR. Without this
// propagation, hasStateUpgrades (pkg/generator/state_upgrade.go) is always false
// in production and no UpgradeState method is ever emitted — the state-upgrade
// generator is exercised only by direct-IR tests. The schema_version is copied
// unconditionally (an explicit 0 is a no-op), and each StateUpgradeConfig is
// converted to a StateUpgradeIR preserving FromVersion and attribute Renames.
func applyResourceStateUpgradeOverride(r *ir.ResourceIR, override config.ResourceOverride) {
	if override.SchemaVersion > 0 {
		r.SchemaVersion = override.SchemaVersion
	}
	if len(override.StateUpgrades) == 0 {
		return
	}
	upgrades := make([]ir.StateUpgradeIR, 0, len(override.StateUpgrades))
	for _, su := range override.StateUpgrades {
		var renames map[string]string
		if len(su.Renames) > 0 {
			renames = make(map[string]string, len(su.Renames))
			for oldName, newName := range su.Renames {
				renames[oldName] = newName
			}
		}
		var blockRenames map[string]string
		if len(su.BlockRenames) > 0 {
			blockRenames = make(map[string]string, len(su.BlockRenames))
			for oldName, newName := range su.BlockRenames {
				blockRenames[oldName] = newName
			}
		}
		upgrades = append(upgrades, ir.StateUpgradeIR{
			FromVersion:       su.From,
			Renames:           renames,
			BlockRenames:      blockRenames,
			AddedAttributes:   append([]string(nil), su.AddedAttributes...),
			AddedBlocks:       append([]string(nil), su.AddedBlocks...),
			RemovedAttributes: append([]string(nil), su.RemovedAttributes...),
			RemovedBlocks:     append([]string(nil), su.RemovedBlocks...),
		})
	}
	r.StateUpgrades = upgrades
}

func applyResourceAttributeOverrides(r *ir.ResourceIR, override config.ResourceOverride) error {
	if len(override.ForceNew) > 0 {
		if err := setAttributeFlag(&r.Schema, override.ForceNew, "force_new"); err != nil {
			return err
		}
	}
	if len(override.ComputedAttributes) > 0 {
		if err := setAttributeFlag(&r.Schema, override.ComputedAttributes, "computed"); err != nil {
			return err
		}
	}
	if len(override.SensitiveAttributes) > 0 {
		if err := setAttributeFlag(&r.Schema, override.SensitiveAttributes, "sensitive"); err != nil {
			return err
		}
	}
	if len(override.WriteOnlyAttributes) > 0 {
		addWriteOnlyAttributes(&r.Schema, override.WriteOnlyAttributes)
	}
	return nil
}

// setAttributeFlag recursively sets a boolean flag on attributes whose name
// matches one of the supplied target names. Names are compared case-
// insensitively and ignoring underscores so that OpenAPI camelCase names and
// Terraform snake_case names both match.
func setAttributeFlag(obj *ir.ObjectSchemaIR, names []string, flag string) error {
	return setAttributeFlagAtPath(obj, names, flag, nil)
}

func setAttributeFlagAtPath(obj *ir.ObjectSchemaIR, names []string, flag string, path []string) error {
	if obj == nil {
		return nil
	}

	for i := range obj.Attributes {
		for _, n := range names {
			if attributeNameMatches(obj.Attributes[i].Name, n) {
				switch flag {
				case "computed":
					// Forcing an attribute Computed declares it server-managed.
					// The plugin framework forbids Computed together with Required
					// (a practitioner cannot be forced to supply a value the server
					// also populates), so clear Required when a computed_attributes
					// override claims a previously-Required attribute. Optional is
					// preserved: Optional+Computed is valid (the practitioner may
					// set the value and the server may also populate it).
					obj.Attributes[i].Computed = true
					obj.Attributes[i].Required = false
				case "sensitive":
					obj.Attributes[i].Sensitive = true
				case "force_new":
					obj.Attributes[i].ForceNew = true
				default:
					pathStr := strings.Join(path, ".")
					if pathStr == "" {
						pathStr = "<root>"
					}
					return fmt.Errorf("unknown attribute flag %q for attribute %q (path: %q)", flag, obj.Attributes[i].Name, pathStr)
				}
				break
			}
		}
	}

	for j := range obj.Blocks {
		if err := setAttributeFlagAtPath(&obj.Blocks[j].Schema, names, flag, append(path, obj.Blocks[j].Name)); err != nil {
			return err
		}
	}
	return nil
}

func attributeNameMatches(attrName, target string) bool {
	return strings.EqualFold(normalizeAttributeName(attrName), normalizeAttributeName(target))
}

// normalizeAttributeName strips underscores and lowercases the name so that
// e.g. "createdAt" and "created_at" compare equal. This is intentionally a
// fuzzier comparison than ToSnakeCase: it is used for matching existing
// attributes, while ToSnakeCase produces the canonical Terraform attribute name
// for newly declared write-only attributes.
func normalizeAttributeName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "_", "")
	return strings.ToLower(s)
}

// addWriteOnlyAttributes appends declared write-only attributes to the top-level
// resource schema. Attributes that already exist by the same normalized name are
// updated in place rather than duplicated. Existing Required, Computed,
// Validators, and PlanModifiers are preserved; only the write-only behavior,
// description, and sensitivity are overridden.
func addWriteOnlyAttributes(obj *ir.ObjectSchemaIR, additions []config.WriteOnlyAttribute) {
	if obj == nil {
		return
	}

	for _, add := range additions {
		name := ToSnakeCase(strings.TrimSpace(add.Name))
		if name == "" {
			continue
		}

		existing := findAttributeIndex(obj.Attributes, name)
		if existing >= 0 {
			obj.Attributes[existing].Description = add.Description
			obj.Attributes[existing].WriteOnly = true
			obj.Attributes[existing].Sensitive = add.Sensitive
			// The framework forbids WriteOnly together with Computed. An attribute
			// inferred as Computed (read-only from the API) cannot also be
			// user-supplied, so clear Computed when forcing write-only (M-46).
			obj.Attributes[existing].Computed = false
			// Preserve Required/Validators/PlanModifiers from inference.
			continue
		}

		obj.Attributes = append(obj.Attributes, ir.AttributeIR{
			Name:        name,
			Description: add.Description,
			Schema:      ir.SchemaIR{Type: ir.TypeString},
			Optional:    true,
			WriteOnly:   true,
			Sensitive:   add.Sensitive,
		})
	}
}

func findAttributeIndex(attrs []ir.AttributeIR, name string) int {
	for i, a := range attrs {
		if attributeNameMatches(a.Name, name) {
			return i
		}
	}
	return -1
}

func timeoutConfigFromOverride(override *config.TimeoutConfig) *ir.TimeoutConfigIR {
	if override == nil {
		return nil
	}

	cfg := &ir.TimeoutConfigIR{}
	if override.Create != nil {
		d := time.Duration(*override.Create)
		cfg.Create = &d
	}
	if override.Read != nil {
		d := time.Duration(*override.Read)
		cfg.Read = &d
	}
	if override.Update != nil {
		d := time.Duration(*override.Update)
		cfg.Update = &d
	}
	if override.Delete != nil {
		d := time.Duration(*override.Delete)
		cfg.Delete = &d
	}
	return cfg
}

// applyDatasourceOverrides applies datasource_overrides entries to matching data
// sources by source operation ID, falling back to name/type name matching.
func applyDatasourceOverrides(provider *ir.ProviderIR, overrides []config.DatasourceOverride) {
	for _, override := range overrides {
		for i := range provider.DataSources {
			ds := &provider.DataSources[i]
			if !datasourceMatchesOverride(*ds, override) {
				continue
			}
			if strings.TrimSpace(override.DatasourceName) != "" {
				prefix := strings.TrimSuffix(ds.TypeName, ds.Name)
				ds.Name = override.DatasourceName
				ds.TypeName = prefix + override.DatasourceName
				ds.FullName = toHumanName(override.DatasourceName)
			}
		}
	}
}

func datasourceMatchesOverride(ds ir.DataSourceIR, override config.DatasourceOverride) bool {
	if strings.TrimSpace(override.Operation) != "" {
		return operationMatches(ds.SourceOperation, ds.ReadMapping, override.Operation)
	}
	if strings.TrimSpace(override.Name) != "" {
		return nameMatchesIdentity(override.Name, ds.Name, ds.TypeName, ds.FullName)
	}
	return false
}

// applyActionOverrides applies action_overrides entries to matching actions by
// source operation ID, falling back to name/type name matching.
func applyActionOverrides(provider *ir.ProviderIR, overrides []config.ActionOverride) {
	for _, override := range overrides {
		for i := range provider.Actions {
			a := &provider.Actions[i]
			if !entityOperationMatches(a.SourceOperation, a.InvokeMapping, override.Operation, a.Name, a.TypeName, a.FullName) {
				continue
			}
			if strings.TrimSpace(override.Name) != "" {
				prefix := strings.TrimSuffix(a.TypeName, a.Name)
				a.Name = override.Name
				a.TypeName = prefix + override.Name
				a.FullName = toHumanName(override.Name)
			}
			if strings.TrimSpace(override.Description) != "" {
				a.Description = override.Description
			}
			if override.ProgressMessages {
				a.ProgressMessages = true
			}
			if override.ModifyPlan {
				a.ModifyPlan = true
			}
		}
	}
}

// applyEphemeralOverrides applies ephemeral_resource_overrides entries to
// matching ephemeral resources by source operation ID, falling back to name/type
// name matching.
func applyEphemeralOverrides(provider *ir.ProviderIR, overrides []config.EphemeralOverride) {
	for _, override := range overrides {
		for i := range provider.EphemeralResources {
			e := &provider.EphemeralResources[i]
			if !entityOperationMatches(e.SourceOperation, e.OpenMapping, override.Operation, e.Name, e.TypeName, e.FullName) {
				continue
			}
			if strings.TrimSpace(override.Name) != "" {
				prefix := strings.TrimSuffix(e.TypeName, e.Name)
				e.Name = override.Name
				e.TypeName = prefix + override.Name
				e.FullName = toHumanName(override.Name)
			}
			if strings.TrimSpace(override.Description) != "" {
				e.Description = override.Description
			}
		}
	}
}

// applyListResourceOverrides applies list_resource_overrides entries to matching
// list resources. When an override specifies Operation, it matches by source
// operation first and falls back to the resource's name, type name, or full name.
// When only Resource is specified, matching is by name, type name, or full name.
func applyListResourceOverrides(provider *ir.ProviderIR, overrides []config.ListResourceOverride) {
	for _, override := range overrides {
		for i := range provider.ListResources {
			lr := &provider.ListResources[i]
			if !listResourceMatchesOverride(*lr, override) {
				continue
			}
			if override.Pagination != nil && override.Pagination.Style != "" {
				lr.PaginationStyle = override.Pagination.Style
			}
			if len(override.ConfigSchema) > 0 {
				applyListResourceConfigSchema(lr, override.ConfigSchema)
			}
		}
	}
}

func listResourceMatchesOverride(lr ir.ListResourceIR, override config.ListResourceOverride) bool {
	if strings.TrimSpace(override.Operation) != "" {
		return entityOperationMatches(lr.SourceOperation, lr.ListMapping, override.Operation, lr.Name, lr.TypeName, lr.FullName)
	}
	if strings.TrimSpace(override.Resource) != "" {
		return nameMatchesIdentity(override.Resource, lr.Name, lr.TypeName, lr.FullName)
	}
	return false
}

func applyListResourceConfigSchema(lr *ir.ListResourceIR, overrides []config.ListConfigSchema) {
	for _, override := range overrides {
		name := ToSnakeCase(strings.TrimSpace(override.Name))
		if name == "" {
			continue
		}

		schema := ir.SchemaIR{Type: primitiveTypeFromConfig(override.Type)}
		required := !override.Optional
		optional := override.Optional

		existing := findAttributeIndex(lr.ConfigSchema.Attributes, name)
		if existing >= 0 {
			lr.ConfigSchema.Attributes[existing].Schema = schema
			lr.ConfigSchema.Attributes[existing].Description = override.Description
			lr.ConfigSchema.Attributes[existing].Required = required
			lr.ConfigSchema.Attributes[existing].Optional = optional
			continue
		}

		lr.ConfigSchema.Attributes = append(lr.ConfigSchema.Attributes, ir.AttributeIR{
			Name:        name,
			Description: override.Description,
			Schema:      schema,
			Required:    required,
			Optional:    optional,
		})
	}
}

func primitiveTypeFromConfig(t string) ir.PrimitiveType {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "string":
		return ir.TypeString
	case "integer", "int":
		return ir.TypeInt
	case "number", "float":
		return ir.TypeFloat
	case "boolean", "bool":
		return ir.TypeBool
	case "null":
		return ir.TypeNull
	case "":
		return ir.TypeDynamic
	default:
		return ir.TypeDynamic
	}
}

// applyFunctionOverrides applies function_overrides entries to matching
// functions by source operation ID, falling back to name/type name matching.
func applyFunctionOverrides(provider *ir.ProviderIR, overrides []config.FunctionOverride) {
	for _, override := range overrides {
		for i := range provider.Functions {
			f := &provider.Functions[i]
			// Functions carry no method+path mapping in the IR, so the override
			// operation matches by operationId or name only.
			if !entityOperationMatches(f.SourceOperation, ir.OperationMappingIR{}, override.Operation, f.Name, f.TypeName, f.FullName) {
				continue
			}
			if strings.TrimSpace(override.Name) != "" {
				prefix := strings.TrimSuffix(f.TypeName, f.Name)
				f.Name = override.Name
				f.TypeName = prefix + override.Name
				f.FullName = toHumanName(override.Name)
			}
		}
	}
}

// applyGlobalTimeouts fills in missing resource timeouts from the global
// timeout configuration. Individual timeout fields that are already set (e.g., from
// a per-resource override) are preserved; only unset fields are filled.
func applyGlobalTimeouts(provider *ir.ProviderIR, global *config.TimeoutConfig) {
	if global == nil {
		return
	}

	for i := range provider.Resources {
		r := &provider.Resources[i]
		if r.Timeouts == nil {
			r.Timeouts = &ir.TimeoutConfigIR{}
		}
		if r.Timeouts.Create == nil && global.Create != nil {
			d := time.Duration(*global.Create)
			r.Timeouts.Create = &d
		}
		if r.Timeouts.Read == nil && global.Read != nil {
			d := time.Duration(*global.Read)
			r.Timeouts.Read = &d
		}
		if r.Timeouts.Update == nil && global.Update != nil {
			d := time.Duration(*global.Update)
			r.Timeouts.Update = &d
		}
		if r.Timeouts.Delete == nil && global.Delete != nil {
			d := time.Duration(*global.Delete)
			r.Timeouts.Delete = &d
		}
	}
}

// applyPolymorphismOverrides converts generator.yaml polymorphism settings into
// the transformer polymorphism configuration, splits top-level polymorphic
// resources into one resource per variant when the split_resources strategy is
// selected (explicitly via per-oneOf/global strategy, or by the
// named-object-variants heuristic, D3), and applies per-schema variant name
// overrides to any matching resources and data sources.
func applyPolymorphismOverrides(provider *ir.ProviderIR, cfg *config.PolymorphismConfig) {
	if cfg == nil {
		return
	}

	polyCfg := toPolymorphismConfig(cfg)
	provider.Resources = splitPolymorphicResources(provider.Resources, polyCfg)
	for _, oneOf := range polyCfg.OneOf {
		// Variant name overrides describe the concrete resources/data sources
		// emitted by the split_resources strategy; apply them only when that
		// strategy is selected for this oneOf, so an entity that merely shares a
		// name with a variant schema is not renamed when split_resources is not
		// in effect (L-102). Per-oneOf strategy is rarely set (config.OneOfOverride
		// has no Strategy field), so fall back to the global strategy — mirroring
		// SelectStrategy's per-schema → global → heuristic precedence.
		strategy := strings.TrimSpace(oneOf.Strategy)
		if strategy == "" {
			strategy = strings.TrimSpace(polyCfg.Strategy)
		}
		if !strings.EqualFold(strategy, string(StrategySplitResources)) {
			continue
		}
		renameResourcesByVariants(provider.Resources, oneOf.Variants)
		renameDataSourcesByVariants(provider.DataSources, oneOf.Variants)
	}
}

// splitPolymorphicResources replaces each resource whose schema is a top-level
// polymorphic union (a single Computed wrapper attribute synthesized by
// ManagedResourceSchema) with one resource per variant when SelectStrategy
// resolves to split_resources. Each variant resource shares the original
// CRUD mapping, import wiring, timeouts, and source operation — it is a view
// of the same endpoints, distinguished by its schema (the discriminator
// attribute is removed by SplitResources). Resources without a top-level
// union, or whose strategy resolves to dynamic_union, are kept unchanged.
func splitPolymorphicResources(resources []ir.ResourceIR, cfg PolymorphismConfig) []ir.ResourceIR {
	var out []ir.ResourceIR
	for _, r := range resources {
		unionAttr, ok := topLevelUnionAttribute(r)
		if !ok {
			out = append(out, r)
			continue
		}
		strategy, err := SelectStrategy(&unionAttr.Schema, ContextTopLevelResource, cfg)
		if err != nil || strategy != StrategySplitResources {
			out = append(out, r)
			continue
		}
		baseName := unionAttr.Schema.Name
		if baseName == "" {
			baseName = r.Name
		}
		variants, err := SplitResources(baseName, &unionAttr.Schema, cfg)
		if err != nil || len(variants) == 0 {
			out = append(out, r)
			continue
		}
		// Derive the provider/name affixes from the original resource so the
		// variants keep the same TypeName/FullName conventions; variant names
		// are snake_cased so they are valid Terraform resource names.
		prefix := strings.TrimSuffix(r.TypeName, r.Name)
		for _, v := range variants {
			v.Name = ToSnakeCase(v.Name)
			v.TypeName = prefix + v.TypeName
			v.FullName = v.TypeName
			v.CRUDMapping = r.CRUDMapping
			v.Importable = r.Importable
			v.ImportIDFormat = r.ImportIDFormat
			v.Timeouts = r.Timeouts
			v.SourceOperation = r.SourceOperation
			out = append(out, v)
		}
	}
	return out
}

// topLevelUnionAttribute returns the union wrapper attribute of a resource
// whose entire schema is a top-level polymorphic union (exactly one
// attribute, Computed, carrying a Union), as synthesized by
// ManagedResourceSchema.
func topLevelUnionAttribute(r ir.ResourceIR) (ir.AttributeIR, bool) {
	if len(r.Schema.Attributes) != 1 || len(r.Schema.Blocks) > 0 {
		return ir.AttributeIR{}, false
	}
	attr := r.Schema.Attributes[0]
	if !attr.Computed || attr.Schema.Union == nil {
		return ir.AttributeIR{}, false
	}
	return attr, true
}

func renameResourcesByVariants(resources []ir.ResourceIR, variants []PolymorphismVariantConfig) {
	for i := range resources {
		r := &resources[i]
		for _, variant := range variants {
			if !strings.EqualFold(r.Name, variant.Schema) && !strings.EqualFold(r.TypeName, variant.Schema) {
				continue
			}
			if strings.TrimSpace(variant.ResourceName) == "" {
				continue
			}
			prefix := strings.TrimSuffix(r.TypeName, r.Name)
			r.Name = variant.ResourceName
			r.TypeName = prefix + variant.ResourceName
			r.FullName = toHumanName(variant.ResourceName)
		}
	}
}

func renameDataSourcesByVariants(dataSources []ir.DataSourceIR, variants []PolymorphismVariantConfig) {
	for i := range dataSources {
		ds := &dataSources[i]
		for _, variant := range variants {
			if !strings.EqualFold(ds.Name, variant.Schema) && !strings.EqualFold(ds.TypeName, variant.Schema) {
				continue
			}
			if strings.TrimSpace(variant.DataSourceName) == "" {
				continue
			}
			prefix := strings.TrimSuffix(ds.TypeName, ds.Name)
			ds.Name = variant.DataSourceName
			ds.TypeName = prefix + variant.DataSourceName
			ds.FullName = toHumanName(variant.DataSourceName)
		}
	}
}

// toPolymorphismConfig converts the config-package polymorphism configuration
// into the transformer-package representation used by SelectStrategy and
// SplitResources.
func toPolymorphismConfig(cfg *config.PolymorphismConfig) PolymorphismConfig {
	if cfg == nil {
		return PolymorphismConfig{}
	}

	out := PolymorphismConfig{
		Strategy: cfg.Strategy,
	}
	for _, oneOf := range cfg.OneOf {
		entry := PolymorphismOneOfConfig{
			Schema: oneOf.Schema,
		}
		for _, v := range oneOf.Variants {
			entry.Variants = append(entry.Variants, PolymorphismVariantConfig{
				Schema:         v.Schema,
				ResourceName:   v.ResourceName,
				DataSourceName: v.DatasourceName,
			})
		}
		out.OneOf = append(out.OneOf, entry)
	}
	return out
}
