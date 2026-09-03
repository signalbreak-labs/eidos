package transformer

import (
	"fmt"
	"strings"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/config"
	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
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
	return ApplyOverridesWithDiagnostics(provider, cfg, nil)
}

// ApplyOverridesWithDiagnostics is ApplyOverrides that appends fail-loud
// warnings to diags (a nil diags is allowed and simply suppresses emission).
// It warns when a computed_attributes override targets an attribute the
// generated CRUD body sends with a required value (e.g. a required query
// parameter like clusterId): making it Computed-only would leave the request
// sending a value the practitioner can never supply, so the override is
// skipped for that attribute and the attribute keeps its Required semantics
// (G39).
func ApplyOverridesWithDiagnostics(provider *ir.ProviderIR, cfg *config.Config, diags *diagnostics.Diagnostics) error {
	if provider == nil || cfg == nil {
		return nil
	}

	// Apply naming conventions first so that prefix/suffix transforms are
	// applied to inferred names; explicit per-entity overrides may then rename
	// them further.
	applyNamingOverrides(provider, cfg.Naming)

	// Per-entity overrides.
	if err := applyResourceOverrides(provider, cfg.ResourceOverrides, diags); err != nil {
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
func applyResourceOverrides(provider *ir.ProviderIR, overrides []config.ResourceOverride, diags *diagnostics.Diagnostics) error {
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
			applyResourceIDOverride(r, override, diags)
			applyResourceImportFormatOverride(r, override, diags)
			applyResourceTimeoutOverride(r, override)
			applyResourceStateUpgradeOverride(r, override)
			applyResourceDescriptionOverride(r, override)
			applyResourcePathParamOverride(r, override, diags)
			applyResourceReadCollectionPath(r, override, diags)
			if err := applyResourceAttributeOverrides(r, override, diags); err != nil {
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

// ResourceOverrideMatches reports whether a resource override targets a
// resource. It mirrors the matching applyResourceOverrides uses, exported so the
// API layer can re-resolve matched resources after override application (e.g.
// for the resource_overrides.generate_datasource opt-in) without duplicating the
// matching rules.
func ResourceOverrideMatches(r ir.ResourceIR, override config.ResourceOverride) bool {
	return resourceMatchesOverride(r, override)
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

func applyResourceIDOverride(r *ir.ResourceIR, override config.ResourceOverride, diags *diagnostics.Diagnostics) {
	if strings.TrimSpace(override.IDAttribute) != "" {
		old := r.IDAttribute
		r.IDAttribute = override.IDAttribute
		warnComputedOnlyImportTarget(r, "{"+r.IDAttribute+"}", diags)
		dropSupersededIDAttribute(r, old, override.IDAttribute, diags)
		// An explicit id_attribute override wires the import to the named
		// attribute, mirroring applyResourceImportFormatOverride's "explicit
		// configuration wins" policy: the practitioner chose the identifier, so
		// the resource is importable by it even when the attribute is
		// Computed-only (the value is learned out of band, e.g. spacetraders'
		// ship symbol). Without this, an id_attribute override on a resource
		// whose inferred identifier was Computed-only leaves the resource
		// non-importable despite the explicit choice. Only when the resource is
		// not already importable — an existing inferred format is preserved —
		// and the named attribute exists in the schema (a missing attribute
		// would emit an ImportState that references a nonexistent model field).
		if !r.Importable && (r.CRUDMapping.Read.Method != "" || r.CRUDMapping.Read.PathTemplate != "") && schemaHasAttribute(r, r.IDAttribute) {
			r.ImportIDFormat = "{" + r.IDAttribute + "}"
			extendImportFormatWithRequiredReadParams(r, diags)
			r.Importable = true
		}
	}
}

// schemaHasAttribute reports whether the resource schema carries an attribute
// with the given (sanitized) name.
func schemaHasAttribute(r *ir.ResourceIR, name string) bool {
	for _, a := range r.Schema.Attributes {
		if a.Name == name {
			return true
		}
	}
	return false
}

// dropSupersededIDAttribute removes the previous identifier attribute when an
// explicit id_attribute override supersedes it with a different attribute and
// the old one is the inferred synthetic placeholder: the Computed-only
// attribute resolveIdentifierAttribute appends, named for the path parameter
// (e.g. {serverAlias} → server_alias) with no WireName because the response
// does not echo it. Left in place it renders as a dead, always-null Computed
// attribute in the schema and docs (gigavuecore's archive_server with
// id_attribute: alias; spacetraders' ship with id_attribute: symbol). The
// removal is surfaced with a Warning, never silent, and skipped when the old
// attribute is real (response-derived attributes always carry a WireName), is
// itself user-settable, is still referenced by a path template, or the new
// identifier is not present in the schema. The new identifier need not be
// user-settable: when it is Computed-only (a server-assigned id echoed by the
// response, e.g. spacetraders' ship symbol), the generator's id-attribute
// fallback still fills the path placeholder with it, so the old synthetic is
// dead weight either way.
func dropSupersededIDAttribute(r *ir.ResourceIR, old, newID string, diags *diagnostics.Diagnostics) {
	if r == nil || old == "" || old == newID {
		return
	}
	var oldAttr, newAttr *ir.AttributeIR
	for i := range r.Schema.Attributes {
		switch r.Schema.Attributes[i].Name {
		case old:
			oldAttr = &r.Schema.Attributes[i]
		case newID:
			newAttr = &r.Schema.Attributes[i]
		}
	}
	if oldAttr == nil || newAttr == nil {
		return
	}
	if oldAttr.WireName != "" || !oldAttr.ComputedOnly() {
		return // real (echoed or input) attribute, not the synthetic placeholder
	}
	templates := []string{r.CRUDMapping.Create.PathTemplate, r.CRUDMapping.Read.PathTemplate, r.CRUDMapping.Delete.PathTemplate}
	if r.CRUDMapping.Update != nil {
		templates = append(templates, r.CRUDMapping.Update.PathTemplate)
	}
	for _, tmpl := range templates {
		if strings.Contains(tmpl, "{"+old+"}") {
			return // a path still name-matches the old attribute; it stays load-bearing
		}
	}
	kept := make([]ir.AttributeIR, 0, len(r.Schema.Attributes))
	for _, a := range r.Schema.Attributes {
		if a.Name != old {
			kept = append(kept, a)
		}
	}
	r.Schema.Attributes = kept
	if diags != nil {
		*diags = append(*diags, diagnostics.Diagnostic{
			Severity: diagnostics.Warning,
			Summary:  "id_attribute override drops the inferred placeholder attribute",
			Detail: fmt.Sprintf(
				"Resource %q: id_attribute %q supersedes the inferred Computed placeholder %q (named for the path parameter and never populated by the response), so %q is dropped from the schema.",
				r.Name, newID, old, old,
			),
		})
	}
}

// applyResourceDescriptionOverride overwrites the resource's Description with
// the override's Description whenever it is non-empty. An omitted description
// does not erase the spec-supplied text, matching the action/ephemeral
// overrides and the write-only attribute description handling.
func applyResourceDescriptionOverride(r *ir.ResourceIR, override config.ResourceOverride) {
	if strings.TrimSpace(override.Description) != "" {
		r.Description = override.Description
	}
}

// applyResourcePathParamOverride records the override's path_params mapping on
// the resource and validates it fail-loud. Each operation must be part of the
// resource's CRUD mapping, each placeholder must appear in that operation's
// path template, and each mapped attribute must exist in the schema. A mapping
// entry that fails validation is dropped (never applied) so the generator does
// not wire a path with a nonexistent model field; the drop is surfaced with a
// Warning, never silent. Placeholder keys tolerate surrounding braces
// ("{entlItemId}" and "entlItemId" are equivalent).
func applyResourcePathParamOverride(r *ir.ResourceIR, override config.ResourceOverride, diags *diagnostics.Diagnostics) {
	if len(override.PathParams) == 0 {
		return
	}
	ops := map[string]ir.OperationMappingIR{
		"create": r.CRUDMapping.Create,
		"read":   r.CRUDMapping.Read,
		"delete": r.CRUDMapping.Delete,
	}
	if r.CRUDMapping.Update != nil {
		ops["update"] = *r.CRUDMapping.Update
	}
	attrs := make(map[string]bool, len(r.Schema.Attributes))
	for _, a := range r.Schema.Attributes {
		attrs[a.Name] = true
	}
	out := make(map[string]map[string]string, len(override.PathParams))
	for op, m := range override.PathParams {
		mapping, ok := ops[op]
		if !ok {
			warnPathParamOverride(r, op, "", "", "operation is not part of the resource's CRUD mapping", diags)
			continue
		}
		resolved := make(map[string]string, len(m))
		for placeholder, attr := range m {
			ph := strings.Trim(strings.TrimSpace(placeholder), "{}")
			if !strings.Contains(mapping.PathTemplate, "{"+ph+"}") {
				warnPathParamOverride(r, op, placeholder, attr, fmt.Sprintf("placeholder %q does not appear in the %s path %q", ph, op, mapping.PathTemplate), diags)
				continue
			}
			if !attrs[attr] {
				warnPathParamOverride(r, op, placeholder, attr, fmt.Sprintf("attribute %q is not in the resource schema", attr), diags)
				continue
			}
			resolved[ph] = attr
		}
		if len(resolved) > 0 {
			out[op] = resolved
		}
	}
	if len(out) > 0 {
		r.PathParamOverrides = out
	}
}

// warnPathParamOverride emits a fail-loud Warning for a path_params mapping
// entry that cannot be applied.
func warnPathParamOverride(r *ir.ResourceIR, op, placeholder, attr, detail string, diags *diagnostics.Diagnostics) {
	if diags == nil {
		return
	}
	*diags = diags.Append(diagnostics.Diagnostic{
		Severity: diagnostics.Warning,
		Summary:  "path_params override cannot be applied",
		Detail:   fmt.Sprintf("Resource %q: path_params.%s %q → %q: %s", r.Name, op, placeholder, attr, detail),
	})
}

// applyResourceReadCollectionPath records the override's read_collection_path
// on the resource's read mapping. A child resource's read is a parent GET whose
// response (after the envelope unwrap) nests the collection under a path (e.g.
// a port filter rule read via GET /portFilters/{portId}, whose response unwraps
// to {port, rules: {passRules, dropRules}} with the rules at "rules.*"); the
// generated read then selects the element whose identifier matches state (G39)
// instead of applying the whole parent. The read is a collection read by
// construction, so ResponseIsCollection is set alongside the path. A malformed
// path (empty segments, a wildcard in a non-final position) is surfaced
// fail-loud and the override is dropped so the generator never emits a
// navigation it cannot satisfy.
func applyResourceReadCollectionPath(r *ir.ResourceIR, override config.ResourceOverride, diags *diagnostics.Diagnostics) {
	path := strings.TrimSpace(override.ReadCollectionPath)
	if path == "" {
		return
	}
	if !validCollectionPath(path) {
		if diags != nil {
			*diags = diags.Append(diagnostics.Diagnostic{
				Severity: diagnostics.Warning,
				Summary:  "read_collection_path override cannot be applied",
				Detail: fmt.Sprintf(
					"Resource %q: read_collection_path %q must be a dot-separated path with a wildcard only in the final segment (e.g. \"rules.*\").",
					r.Name, path,
				),
			})
		}
		return
	}
	r.CRUDMapping.Read.NestedCollectionPath = path
	r.CRUDMapping.Read.ResponseIsCollection = true
}

// validCollectionPath reports whether a read_collection_path is well-formed: a
// non-empty dot-separated path whose segments are non-empty and whose wildcard
// ("*"), if any, appears only in the final segment.
func validCollectionPath(path string) bool {
	segs := strings.Split(path, ".")
	if len(segs) == 0 {
		return false
	}
	for i, seg := range segs {
		if seg == "" {
			return false
		}
		if seg == "*" && i != len(segs)-1 {
			return false
		}
	}
	return true
}

// applyResourceImportFormatOverride stores the configured import format on the
// resource whenever ImportFormat is non-empty. Importable is gated by the
// presence of a Read operation, but ImportIDFormat is always recorded so the
// configured value is preserved even when the resource cannot be imported.
func applyResourceImportFormatOverride(r *ir.ResourceIR, override config.ResourceOverride, diags *diagnostics.Diagnostics) {
	if strings.TrimSpace(override.ImportFormat) != "" {
		r.ImportIDFormat = override.ImportFormat
		warnComputedOnlyImportTarget(r, r.ImportIDFormat, diags)
		extendImportFormatWithRequiredReadParams(r, diags)
		// warnMissingRequiredReadParams runs inside the extension pass: after it,
		// only parameters that could not be auto-extended still warn.
		// Only mark the resource as importable when a Read operation is present;
		// otherwise there is no GET-by-ID path to support import.
		if r.CRUDMapping.Read.Method != "" || r.CRUDMapping.Read.PathTemplate != "" {
			r.Importable = true
		}
	}
}

// extendImportFormatWithRequiredReadParams appends the read operation's
// required query parameters to the import format when the format does not
// already populate them and the parameter maps to a user-settable schema
// attribute. The refresh that follows an import sends those parameters from
// state, so an import format that omits one (GigaVUE-FM's required clusterId
// query parameter, across dozens of resources) produces a read the API
// rejects with an empty value. Auto-extending keeps the import usable
// without forcing every generator.yaml entry to spell the full cluster
// addressing; the extension is surfaced with an Info diagnostic, never
// silent. Parameters with no matching user-settable attribute are left to
// warnMissingRequiredReadParams — they need a config decision, not an
// invented attribute.
func extendImportFormatWithRequiredReadParams(r *ir.ResourceIR, diags *diagnostics.Diagnostics) {
	if r == nil || !strings.Contains(r.ImportIDFormat, "{") {
		// Brace-less formats are parsed as bare attribute words; appending a
		// braced segment would turn them into mixed static text, so leave them
		// to the fail-loud warning instead.
		if r != nil {
			warnMissingRequiredReadParams(r, r.ImportIDFormat, diags)
		}
		return
	}
	covered := map[string]bool{}
	for _, attr := range importFormatAttrs(r.ImportIDFormat) {
		covered[attr] = true
	}
	original := r.ImportIDFormat
	extended := original
	var added []string
	for _, p := range r.CRUDMapping.Read.QueryParams {
		if !p.Required {
			continue
		}
		attr := SanitizeAttributeName(p.Name)
		if covered[attr] || !hasUserSettableAttribute(r, attr) {
			continue
		}
		extended += "/{" + attr + "}"
		covered[attr] = true
		added = append(added, attr)
	}
	if len(added) == 0 {
		warnMissingRequiredReadParams(r, r.ImportIDFormat, diags)
		return
	}
	r.ImportIDFormat = extended
	if diags != nil {
		*diags = append(*diags, diagnostics.Diagnostic{
			Severity: diagnostics.Info,
			Summary:  "import format extended with required read parameters",
			Detail: fmt.Sprintf(
				"The import format on resource %q was extended from %q to %q so the read after import populates its required query parameter(s) (%s) instead of sending an empty value.",
				r.Name, original, r.ImportIDFormat, strings.Join(added, ", "),
			),
		})
	}
	warnMissingRequiredReadParams(r, r.ImportIDFormat, diags)
}

// hasUserSettableAttribute reports whether the resource schema has an attribute
// with the given name that the practitioner can set in configuration.
func hasUserSettableAttribute(r *ir.ResourceIR, name string) bool {
	for _, a := range r.Schema.Attributes {
		if a.Name == name {
			return a.Required || a.Optional
		}
	}
	return false
}

// warnComputedOnlyImportTarget surfaces a fail-loud warning when an explicit
// id_attribute or import_format references a Computed-only attribute: its
// value is not part of the practitioner's configuration, so the import
// relies on the practitioner supplying an externally-assigned value. That is
// legitimate when the API assigns the identifier server-side and the
// practitioner can learn it out of band (the FM UI, a collection read), but
// when the spec also offers a user-settable identifier, that one makes the
// import frictionless — hence the warning rather than silence (G39). The
// override still applies: explicit configuration wins.
func warnComputedOnlyImportTarget(r *ir.ResourceIR, format string, diags *diagnostics.Diagnostics) {
	if diags == nil || r == nil {
		return
	}
	// A singleton resource (parameterless read) accepts any import ID — the
	// refresh repopulates the Computed-only target from the response — so a
	// Computed-only import target is not a friction signal there; the import
	// gate notes the placeholder semantics via its own Info diagnostic.
	if singletonResourceRead(r) {
		return
	}
	for _, attr := range importFormatAttrs(format) {
		for _, a := range r.Schema.Attributes {
			if a.Name == attr && a.ComputedOnly() {
				*diags = append(*diags, diagnostics.Diagnostic{
					Severity: diagnostics.Warning,
					Summary:  "import target is computed-only",
					Detail: fmt.Sprintf(
						"The import format %q on resource %q targets attribute %q, which is Computed-only, so importing requires the practitioner to supply its externally-assigned value (from the API or its UI). If the spec offers a user-settable identifier (required or optional in the request), importing by that instead avoids the out-of-band lookup.",
						format, r.Name, attr,
					),
				})
			}
		}
	}
}

// singletonResourceRead reports whether the resource's read operation takes no
// path parameters and no required query/header parameters, so the refresh
// after import succeeds regardless of the import ID's value.
func singletonResourceRead(r *ir.ResourceIR) bool {
	read := r.CRUDMapping.Read
	if read.Method == "" && read.PathTemplate == "" {
		return false
	}
	if len(read.PathParams) > 0 {
		return false
	}
	for _, p := range read.QueryParams {
		if p.Required {
			return false
		}
	}
	for _, p := range read.HeaderParams {
		if p.Required {
			return false
		}
	}
	return true
}

// warnMissingRequiredReadParams surfaces a fail-loud warning when the explicit
// import format does not populate every required query/header parameter of the
// read operation: the refresh that follows import sends those parameters from
// state, and an unpopulated one leaves the request sending an empty value the
// API rejects (e.g. GigaVUE-FM's required clusterId query parameter) (G39).
func warnMissingRequiredReadParams(r *ir.ResourceIR, format string, diags *diagnostics.Diagnostics) {
	if diags == nil || r == nil {
		return
	}
	covered := map[string]bool{}
	for _, attr := range importFormatAttrs(format) {
		covered[attr] = true
	}
	for _, p := range r.CRUDMapping.Read.QueryParams {
		if !p.Required {
			continue
		}
		if covered[SanitizeAttributeName(p.Name)] {
			continue
		}
		*diags = append(*diags, diagnostics.Diagnostic{
			Severity: diagnostics.Warning,
			Summary:  "import format omits a required read parameter",
			Detail: fmt.Sprintf(
				"The import format %q on resource %q does not populate the required read query parameter %q; the read after import will send an empty value. Extend the import format with the corresponding attribute (e.g. {%s}).",
				format, r.Name, p.Name, SanitizeAttributeName(p.Name),
			),
		})
	}
}

// importFormatAttrs extracts the brace-enclosed attribute names from an import
// format string (e.g. "{slot_id}:{cluster_id}" → ["slot_id", "cluster_id"]).
func importFormatAttrs(format string) []string {
	var attrs []string
	start := -1
	for i, c := range format {
		switch c {
		case '{':
			start = i + 1
		case '}':
			if start >= 0 && start < i {
				attrs = append(attrs, format[start:i])
			}
			start = -1
		}
	}
	return attrs
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

func applyResourceAttributeOverrides(r *ir.ResourceIR, override config.ResourceOverride, diags *diagnostics.Diagnostics) error {
	if len(override.ForceNew) > 0 {
		if err := setAttributeFlag(&r.Schema, override.ForceNew, "force_new"); err != nil {
			return err
		}
	}
	if len(override.ComputedAttributes) > 0 {
		if err := setAttributeFlagWithDiagnostics(&r.Schema, override.ComputedAttributes, "computed", r.Name, diags); err != nil {
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
	if len(override.ExcludeAttributes) > 0 {
		excludeAttributesAtPath(&r.Schema, override.ExcludeAttributes)
		// Record the exclusion so the config generator can re-emit the override
		// on round-trip; the attributes themselves are gone from Schema.
		r.ExcludedAttributes = append([]string(nil), override.ExcludeAttributes...)
	}
	return nil
}

// excludeAttributesAtPath removes every attribute whose name matches one of the
// supplied target names, at any nesting depth. Matching mirrors
// setAttributeFlagAtPath: case-insensitive and underscore-insensitive, with an
// exact-name match winning over the fuzzy match so an entry for "user_name"
// does not also claim a distinct "username" attribute (G39). Removing an
// attribute drops it from the generated model, so the create/update body omits
// it and the read ignores it — the mechanism a child resource uses to stop the
// parent from exposing a nested collection it manages separately (e.g.
// port_filter's "rules" once port_filter_rule owns the rules).
func excludeAttributesAtPath(obj *ir.ObjectSchemaIR, names []string) {
	if obj == nil {
		return
	}
	exactIndex := exactNameIndex(obj.Attributes, names)
	kept := obj.Attributes[:0]
	for i := range obj.Attributes {
		excluded := false
		for _, n := range names {
			if !attributeNameMatches(obj.Attributes[i].Name, n) {
				continue
			}
			if j, ok := exactIndex[n]; ok && j != i {
				continue // a distinct exact match claims this entry
			}
			excluded = true
			break
		}
		if excluded {
			continue
		}
		kept = append(kept, obj.Attributes[i])
	}
	obj.Attributes = kept

	for j := range obj.Blocks {
		excludeAttributesAtPath(&obj.Blocks[j].Schema, names)
	}
	for i := range obj.Attributes {
		excludeAttributesRecursiveSchema(&obj.Attributes[i].Schema, names)
	}
}

// excludeAttributesRecursiveSchema applies excludeAttributesAtPath to every
// nested schema node reachable from schema, mirroring
// setAttributeFlagRecursiveSchema's traversal so an exclusion reaches the same
// nodes a computed/sensitive/force_new override does (N-20).
func excludeAttributesRecursiveSchema(schema *ir.SchemaIR, names []string) {
	if schema == nil {
		return
	}
	if len(schema.Attributes) > 0 || len(schema.Blocks) > 0 {
		obj := ir.ObjectSchemaIR{
			Attributes:        schema.Attributes,
			Blocks:            schema.Blocks,
			DependentRequired: schema.DependentRequired,
		}
		excludeAttributesAtPath(&obj, names)
		schema.Attributes = obj.Attributes
		schema.Blocks = obj.Blocks
	}

	recurse := func(children ...*ir.SchemaIR) {
		for _, c := range children {
			if c == nil {
				continue
			}
			excludeAttributesRecursiveSchema(c, names)
		}
	}

	var children []*ir.SchemaIR
	if schema.Collection != nil {
		children = append(children, &schema.Collection.ElementType)
	}
	if schema.Union != nil {
		for i := range schema.Union.Variants {
			children = append(children, &schema.Union.Variants[i])
		}
	}
	children = append(children, schema.Not, schema.IfSchema, schema.ThenSchema, schema.ElseSchema)
	for _, dep := range schema.DependentSchemas {
		children = append(children, dep)
	}
	for _, pp := range schema.PatternProperties {
		children = append(children, pp)
	}
	children = append(children, schema.PropertyNames, schema.UnevaluatedProperties)
	recurse(children...)
}

// setAttributeFlag recursively sets a boolean flag on attributes whose name
// matches one of the supplied target names. Names are compared case-
// insensitively and ignoring underscores so that OpenAPI camelCase names and
// Terraform snake_case names both match.
func setAttributeFlag(obj *ir.ObjectSchemaIR, names []string, flag string) error {
	return setAttributeFlagAtPath(obj, names, flag, nil, nil)
}

// setAttributeFlagWithDiagnostics is setAttributeFlag that warns (via diags,
// nil-safe) when the "computed" flag is refused for an attribute the generated
// CRUD body sends with a required value (G39).
func setAttributeFlagWithDiagnostics(obj *ir.ObjectSchemaIR, names []string, flag, resourceName string, diags *diagnostics.Diagnostics) error {
	return setAttributeFlagAtPath(obj, names, flag, nil, &computedOverrideContext{resourceName: resourceName, diags: diags})
}

// computedOverrideContext carries the warning channel for a computed_attributes
// override: a request-input attribute that is Required keeps its Required
// semantics (the request needs a practitioner-supplied value), so applying the
// computed flag to it is refused and surfaced rather than silently breaking the
// generated request.
type computedOverrideContext struct {
	resourceName string
	diags        *diagnostics.Diagnostics
}

// setAttributeFlagAtPath sets the flag on matching attributes at any depth: the
// object's own attributes and blocks, plus every nested schema reachable from
// an attribute (nested object/list/set attributes, union variants, conditional
// and dependent schemas). Without the nested-attribute recursion an override
// like computed_attributes: ["nested_field"] for a field nested under an object
// attribute matched nothing and returned nil silently (N-20).
func setAttributeFlagAtPath(obj *ir.ObjectSchemaIR, names []string, flag string, path []string, computed *computedOverrideContext) error {
	if obj == nil {
		return nil
	}

	exactIndex := exactNameIndex(obj.Attributes, names)
	for i := range obj.Attributes {
		if err := applyAttributeFlag(&obj.Attributes[i], i, names, exactIndex, flag, path, computed); err != nil {
			return err
		}
	}

	for j := range obj.Blocks {
		if err := setAttributeFlagAtPath(&obj.Blocks[j].Schema, names, flag, append(path, obj.Blocks[j].Name), computed); err != nil {
			return err
		}
	}

	// Recurse into each attribute's schema so a name nested under an object
	// attribute, inside a list/set element, or under a union variant is reached.
	// This mirrors applyWriteOnlyRecursive's traversal (writeonly.go), which
	// already covers these nodes; the two walks must stay in step so every flag
	// override and the write-only pass behave identically on nested schemas.
	for i := range obj.Attributes {
		if err := setAttributeFlagRecursiveSchema(&obj.Attributes[i].Schema, names, flag, append(path, obj.Attributes[i].Name), computed); err != nil {
			return err
		}
	}
	return nil
}

// exactNameIndex maps each override entry to the index of the first attribute
// whose name is an exact (case-insensitive, underscores intact) match. An
// exact-name match wins over the underscore-insensitive fuzzy match: when
// "user_name" and "username" both exist as distinct attributes, an override
// entry for "user_name" must not also claim "username" (G39 gigavuecore
// v3_user — the computed_attributes entry for the synthetic user_name id
// attribute also demoted the distinct create-required username, making the
// resource uncreatable). Fuzzy matching stays for entries whose spelling
// differs from the attribute only by underscores.
func exactNameIndex(attrs []ir.AttributeIR, names []string) map[string]int {
	exactIndex := make(map[string]int, len(names))
	for _, n := range names {
		if _, ok := exactIndex[n]; ok {
			continue
		}
		for i := range attrs {
			if exactAttributeName(attrs[i].Name, n) {
				exactIndex[n] = i
				break
			}
		}
	}
	return exactIndex
}

// applyAttributeFlag applies the flag to attr for the first names entry it
// matches (fuzzy), unless a distinct attribute holds that entry's exact match
// (exactIndex). The matched entry wins and the remaining entries are skipped,
// mirroring the pre-refactor single-break behavior.
func applyAttributeFlag(attr *ir.AttributeIR, attrIndex int, names []string, exactIndex map[string]int, flag string, path []string, computed *computedOverrideContext) error {
	for _, n := range names {
		if !attributeNameMatches(attr.Name, n) {
			continue
		}
		if j, ok := exactIndex[n]; ok && j != attrIndex {
			continue // a distinct exact match claims this entry
		}
		if err := mutateAttributeFlag(attr, flag, path, computed); err != nil {
			return err
		}
		break
	}
	return nil
}

// mutateAttributeFlag mutates a single attribute per the override flag.
func mutateAttributeFlag(attr *ir.AttributeIR, flag string, path []string, computed *computedOverrideContext) error {
	switch flag {
	case "computed":
		// Forcing an attribute Computed declares it server-managed.
		// The plugin framework forbids Computed together with Required
		// (a practitioner cannot be forced to supply a value the server
		// also populates), so clear Required when a computed_attributes
		// override claims a previously-Required attribute. Optional is
		// preserved: Optional+Computed is valid (the practitioner may
		// set the value and the server may also populate it).
		//
		// A request-input attribute that is Required is exempt (G39):
		// the generated CRUD body sends its value (e.g. a required
		// query parameter like clusterId, or a required create-body
		// field), so making it Computed-only would leave the request
		// sending a value the practitioner can never supply and break
		// create and import. The override is refused for that
		// attribute and surfaced with a Warning instead of silently
		// breaking the request.
		if attr.RequestInput && attr.Required {
			if computed != nil && computed.diags != nil {
				pathStr := strings.Join(path, ".")
				if pathStr == "" {
					pathStr = "<root>"
				}
				*computed.diags = append(*computed.diags, diagnostics.Diagnostic{
					Severity: diagnostics.Warning,
					Summary:  "computed_attributes override refused for a required request input",
					Detail: fmt.Sprintf(
						"The attribute %q on resource %q (path %q) is sent by the generated request with a required value, so marking it computed would make the resource uncreatable and unimportable. The computed_attributes entry is ignored for this attribute; remove it from generator.yaml or make the attribute optional in the spec.",
						attr.Name, computed.resourceName, pathStr,
					),
				})
			}
			return nil
		}
		attr.Computed = true
		attr.Required = false
	case "sensitive":
		attr.Sensitive = true
	case "force_new":
		attr.ForceNew = true
	default:
		pathStr := strings.Join(path, ".")
		if pathStr == "" {
			pathStr = "<root>"
		}
		return fmt.Errorf("unknown attribute flag %q for attribute %q (path: %q)", flag, attr.Name, pathStr)
	}
	return nil
}

// setAttributeFlagRecursiveSchema applies setAttributeFlagAtPath to every nested
// schema node reachable from schema: object schemas (attributes/blocks),
// collection element types, union variants, and the conditional/dependent/
// pattern/property-name/unevaluated nodes. It mirrors applyWriteOnlyRecursive so
// computed/sensitive/force_new overrides reach the same nodes write-only
// processing does (N-20).
func setAttributeFlagRecursiveSchema(schema *ir.SchemaIR, names []string, flag string, path []string, computed *computedOverrideContext) error {
	if schema == nil {
		return nil
	}

	if len(schema.Attributes) > 0 || len(schema.Blocks) > 0 {
		obj := ir.ObjectSchemaIR{
			Attributes:        schema.Attributes,
			Blocks:            schema.Blocks,
			DependentRequired: schema.DependentRequired,
		}
		if err := setAttributeFlagAtPath(&obj, names, flag, path, computed); err != nil {
			return err
		}
		schema.Attributes = obj.Attributes
		schema.Blocks = obj.Blocks
	}

	// recurse applies setAttributeFlagRecursiveSchema to each child node in turn,
	// returning the first error. Collecting the reachable children into a slice
	// keeps the per-node nil checks flat (one loop) instead of a chain of ifs, so
	// the function stays under the cognitive-complexity budget while covering the
	// same node set as applyWriteOnlyRecursive (N-20).
	recurse := func(children ...*ir.SchemaIR) error {
		for _, c := range children {
			if c == nil {
				continue
			}
			if err := setAttributeFlagRecursiveSchema(c, names, flag, path, computed); err != nil {
				return err
			}
		}
		return nil
	}

	var children []*ir.SchemaIR
	if schema.Collection != nil {
		children = append(children, &schema.Collection.ElementType)
	}
	if schema.Union != nil {
		for i := range schema.Union.Variants {
			children = append(children, &schema.Union.Variants[i])
		}
	}
	children = append(children, schema.Not, schema.IfSchema, schema.ThenSchema, schema.ElseSchema)
	for _, dep := range schema.DependentSchemas {
		children = append(children, dep)
	}
	for _, pp := range schema.PatternProperties {
		children = append(children, pp)
	}
	children = append(children, schema.PropertyNames, schema.UnevaluatedProperties)
	return recurse(children...)
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
			// `description` is omitempty in generator.yaml: an override that only
			// sets e.g. `sensitive: true` must not erase the description the spec
			// supplied. Matches applyActionOverrides/applyEphemeralOverrides.
			if strings.TrimSpace(add.Description) != "" {
				obj.Attributes[existing].Description = add.Description
			}
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
	// An exact-name match (case-insensitive, underscores intact) wins over the
	// underscore-insensitive fuzzy match, for the same reason as in
	// setAttributeFlagAtPath: "user_name" and "username" are distinct
	// attributes, and an override entry must resolve to the one it names (G39).
	for i, a := range attrs {
		if exactAttributeName(a.Name, name) {
			return i
		}
	}
	for i, a := range attrs {
		if attributeNameMatches(a.Name, name) {
			return i
		}
	}
	return -1
}

// exactAttributeName reports whether attrName and target are the same name
// ignoring case but keeping underscores, so distinct snake_case attributes
// (user_name vs username) do not collide.
func exactAttributeName(attrName, target string) bool {
	return strings.EqualFold(strings.TrimSpace(attrName), strings.TrimSpace(target))
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

		existing := findAttributeIndex(lr.ConfigSchema.Attributes, name)
		if existing >= 0 {
			lr.ConfigSchema.Attributes[existing].Schema = schema
			// See addWriteOnlyAttributes: an omitted `description` must not erase
			// the one the spec's parameter supplied.
			if strings.TrimSpace(override.Description) != "" {
				lr.ConfigSchema.Attributes[existing].Description = override.Description
			}
			// M-7: Optional is a *bool, so an omitted `optional:` key (nil) must
			// not overwrite the existing attribute's Required/Optional — a
			// description-only override would otherwise flip a spec-optional
			// filter to Required. Only an explicit `optional:` value changes it.
			if override.Optional != nil {
				lr.ConfigSchema.Attributes[existing].Required = !*override.Optional
				lr.ConfigSchema.Attributes[existing].Optional = *override.Optional
			}
			continue
		}

		// New attribute: an omitted `optional:` keeps the historical default of
		// Required (matching the pre-M-7 behavior for brand-new entries); only an
		// explicit `optional:` value changes it.
		required := true
		optional := false
		if override.Optional != nil {
			required = !*override.Optional
			optional = *override.Optional
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
