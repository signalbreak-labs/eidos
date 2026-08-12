package transformer

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/parser"
)

// OperationsFromSpec converts a parsed, normalized OpenAPI spec into the
// transformer-internal path-operation map used by CRUD, data source, and list
// resource inference. It populates request/response metadata, response headers
// from successful responses, and operation extensions. Diagnostics produced
// during conversion (e.g. unrepresentable boolean schemas) are discarded; use
// OperationsFromSpecWithDiagnostics to surface them.
func OperationsFromSpec(spec *parser.Spec) map[string]map[HTTPMethod]Operation {
	ops, _ := OperationsFromSpecWithDiagnostics(spec)
	return ops
}

// OperationsFromSpecWithDiagnostics behaves like OperationsFromSpec but also
// returns the diagnostics collected while converting schemas, so callers can
// surface warnings about spec constructs the Terraform schema model cannot
// represent (such as boolean `items`/`additionalProperties` schemas) instead
// of dropping them silently (L-97).
func OperationsFromSpecWithDiagnostics(spec *parser.Spec) (map[string]map[HTTPMethod]Operation, diagnostics.Diagnostics) {
	if spec == nil || len(spec.Paths) == 0 {
		return nil, nil
	}

	var diags diagnostics.Diagnostics
	out := make(map[string]map[HTTPMethod]Operation, len(spec.Paths))
	for path, pi := range spec.Paths {
		if pi == nil {
			continue
		}
		ops := make(map[HTTPMethod]Operation)
		add := func(method HTTPMethod, op *parser.Operation) {
			if op == nil {
				return
			}
			ops[method] = operationFromParser(spec, path, method, op, pi, &diags)
		}
		add(MethodGet, pi.Get)
		add(MethodPut, pi.Put)
		add(MethodPost, pi.Post)
		add(MethodDelete, pi.Delete)
		add(MethodPatch, pi.Patch)
		if len(ops) > 0 {
			out[path] = ops
		}
	}
	return out, diags
}

func operationFromParser(spec *parser.Spec, path string, method HTTPMethod, op *parser.Operation, pi *parser.PathItem, diags *diagnostics.Diagnostics) Operation {
	out := Operation{
		Method:      method,
		Path:        path,
		OperationID: op.OperationID,
		Parameters:  parametersFromParser(spec, pi, op),
		Extensions:  shallowCopyExtensions(op.Extensions),
	}
	// formData parameters (OpenAPI 2.0 form-encoded request bodies) cannot be
	// wired: the generated request body only encodes JSON. Surface them as a
	// fail-loud warning so the construct is never silently dropped; the
	// generator keeps the operation honestly scaffolded (REMAINING_GAPS §2).
	warnFormDataParameters(diags, op, pi)

	if op.RequestBody != nil {
		rb := resolveRequestBody(spec, op.RequestBody)
		out.RequestBody = rb != nil && len(rb.Content) > 0
		out.RequestMediaType = selectMediaType(rb.Content)
		if s := firstContentSchema(rb.Content); s != nil {
			out.RequestSchema = schemaSpecFromParser(spec, resolveSchemaRef(spec, s), diags)
			// resolveSchemaRef drops the $ref, so restore the referenced schema
			// name for union wrapper naming (D1).
			if out.RequestSchema != nil && out.RequestSchema.RefName == "" {
				out.RequestSchema.RefName = refBaseName(s.Ref)
			}
		}
		// Fail-loud for an unsupported request body media type (A2). The generator
		// encodes JSON, form-urlencoded, multipart, and XML request bodies; any
		// other media type (e.g. application/octet-stream) leaves the operation
		// honestly scaffolded. Surface the limitation as a Warning so the construct
		// is not silently dropped to a JSON body, and the generator's scaffold is
		// explained.
		if out.RequestMediaType != "" && RequestBodyKind(out.RequestMediaType) == "unsupported" {
			*diags = append(*diags, diagnostics.Diagnostic{
				Severity: diagnostics.Warning,
				Summary:  "Unsupported request body media type",
				Detail: fmt.Sprintf(
					"operation %s declares request body media type %q, which the generator cannot encode. "+
						"Supported media types are application/json, application/x-www-form-urlencoded, "+
						"multipart/form-data, and application/xml; other media types leave the operation "+
						"honestly scaffolded (not wired to a remote API endpoint).",
					out.OperationID, out.RequestMediaType,
				),
			})
		}
	}

	if resp := successfulResponse(spec, op.Responses); resp != nil {
		out.ResponseBody = len(resp.Content) > 0
		if s := firstContentSchema(resp.Content); s != nil {
			out.ResponseSchema = schemaSpecFromParser(spec, resolveSchemaRef(spec, s), diags)
			// resolveSchemaRef drops the $ref, so restore the referenced schema
			// name for union wrapper naming (D1).
			if out.ResponseSchema != nil && out.ResponseSchema.RefName == "" {
				out.ResponseSchema.RefName = refBaseName(s.Ref)
			}
			// Flatten a {data: <payload>} response envelope (a common API
			// convention, e.g. SpaceTraders) so the Terraform schema exposes the
			// payload attributes directly instead of nesting them under a `data`
			// attribute. The envelope key is recorded so the generator unwraps the
			// decoded response body to match (E1).
			out.ResponseSchema, out.ResponseEnvelope = UnwrapResponseEnvelope(out.ResponseSchema)
		}
		out.ResponseHeaders = headerNames(resp.Headers)
	}

	return out
}

func parametersFromParser(spec *parser.Spec, pi *parser.PathItem, op *parser.Operation) []Parameter {
	type key struct{ name, in string }
	seen := make(map[key]bool)
	var params []Parameter
	add := func(p parser.Parameter) {
		resolved := resolveParameter(spec, p)
		k := key{resolved.Name, resolved.In}
		if seen[k] {
			return
		}
		seen[k] = true
		paramType := ""
		if resolved.Schema != nil {
			paramType = schemaTypeString(resolved.Schema.Type)
		}
		params = append(params, Parameter{
			Name:     resolved.Name,
			In:       resolved.In,
			Required: resolved.Required,
			Type:     paramType,
		})
	}
	// Operation-level parameters take precedence over path-item parameters per
	// OpenAPI 3.x. Add operation params first so a path-level duplicate is skipped.
	for _, p := range op.Parameters {
		add(p)
	}
	if pi != nil {
		for _, p := range pi.Parameters {
			add(p)
		}
	}
	return params
}

func successfulResponse(spec *parser.Spec, responses map[string]*parser.Response) *parser.Response {
	if len(responses) == 0 {
		return nil
	}

	// Collect success response keys: explicit 2xx codes plus OpenAPI 3 range
	// wildcards such as "2XX". A specific code is preferred over a wildcard at
	// the same hundred, so (sortKey, wildcard) sorts "200" before "2XX" (L-96).
	type successKey struct {
		sortKey  int
		wildcard bool
		key      string
	}
	keys := make([]successKey, 0, len(responses))
	for code := range responses {
		if code == "default" {
			continue
		}
		if n, err := strconv.Atoi(code); err == nil {
			if n >= 200 && n < 300 {
				keys = append(keys, successKey{sortKey: n, key: code})
			}
			continue
		}
		if isRangeWildcard(code) {
			n := int(code[0]-'0') * 100
			if n >= 200 && n < 300 {
				keys = append(keys, successKey{sortKey: n, wildcard: true, key: code})
			}
		}
	}
	sort.SliceStable(keys, func(i, j int) bool {
		if keys[i].sortKey != keys[j].sortKey {
			return keys[i].sortKey < keys[j].sortKey
		}
		// Prefer the specific code over the wildcard at the same hundred.
		return !keys[i].wildcard && keys[j].wildcard
	})
	for _, k := range keys {
		if r := resolveResponse(spec, responses[k.key]); r != nil {
			return r
		}
	}
	return resolveResponse(spec, responses["default"])
}

// isRangeWildcard reports whether code is an OpenAPI 3 range response key of
// the form 1XX, 2XX, 3XX, 4XX, or 5XX (case-insensitive on the X digits).
func isRangeWildcard(code string) bool {
	if len(code) != 3 {
		return false
	}
	c := code[0]
	if c < '1' || c > '5' {
		return false
	}
	return (code[1] == 'X' || code[1] == 'x') && (code[2] == 'X' || code[2] == 'x')
}

func resolveResponse(spec *parser.Spec, r *parser.Response) *parser.Response {
	if r == nil || r.Ref == "" || spec == nil || spec.Components == nil {
		return r
	}
	name := refName(r.Ref)
	if resolved, ok := spec.Components.Responses[name]; ok && resolved != nil {
		return resolved
	}
	return r
}

func resolveRequestBody(spec *parser.Spec, rb *parser.RequestBody) *parser.RequestBody {
	if rb == nil || rb.Ref == "" || spec == nil || spec.Components == nil {
		return rb
	}
	name := refName(rb.Ref)
	if resolved, ok := spec.Components.RequestBodies[name]; ok && resolved != nil {
		return resolved
	}
	return rb
}

// resolveSchemaRef follows a content schema's $ref to the component schema it
// names, so a request/response body declared as {"$ref": "#/components/schemas/Pet"}
// contributes its real properties to the Operation's RequestSchema/ResponseSchema
// instead of an empty, unresolved-ref schema. It follows chained refs and stops
// on a cycle (the parser marks cyclic component schemas Opaque, which
// schemaSpecFromParser honors as an opaque boundary) or an unresolvable ref.
func resolveSchemaRef(spec *parser.Spec, s *parser.Schema) *parser.Schema {
	if s == nil || s.Ref == "" || spec == nil || spec.Components == nil {
		return s
	}
	visited := map[string]bool{}
	cur := s
	for cur.Ref != "" {
		if visited[cur.Ref] {
			return cur // cycle: stop at the boundary rather than looping forever
		}
		visited[cur.Ref] = true
		name := refName(cur.Ref)
		resolved, ok := spec.Components.Schemas[name]
		if !ok || resolved == nil {
			return cur // unresolvable: return the ref schema as-is
		}
		cur = resolved
	}
	return cur
}

func resolveParameter(spec *parser.Spec, p parser.Parameter) *parser.Parameter {
	if p.Ref == "" || spec == nil || spec.Components == nil {
		return &p
	}
	name := refName(p.Ref)
	if resolved, ok := spec.Components.Parameters[name]; ok && resolved != nil {
		return resolved
	}
	return &p
}

func firstContentSchema(content map[string]*parser.MediaType) *parser.Schema {
	name := selectMediaType(content)
	if name == "" {
		return nil
	}
	return content[name].Schema
}

// UnwrapResponseEnvelope flattens a {data: <payload>} response envelope into the
// payload schema itself, returning the flattened schema and the envelope
// property name ("data"). It returns the input schema unchanged with an empty
// key when the response is not enveloped.
//
// A response is treated as an envelope when it is an object whose `data`
// property is itself an object with properties (or an unresolved $ref to one)
// or an array, and every other property is an auxiliary envelope companion
// ("meta" or "links"). The payload-object rule excludes a `data` map
// (additionalProperties) or scalar, which is a legitimate field rather than an
// envelope; the companion rule prevents mis-flattening a spec that carries a
// meaningful top-level `data` object alongside other payload fields. The
// generator reads the returned key to unwrap the decoded response body before
// applying it to the model, keeping the schema and the response consistent (E1).
func UnwrapResponseEnvelope(spec *SchemaSpec) (*SchemaSpec, string) {
	if spec == nil || !strings.EqualFold(spec.Type, "object") {
		return spec, ""
	}
	data, ok := spec.Properties["data"]
	if !ok {
		return spec, ""
	}
	payload := data.RefName != "" ||
		(strings.EqualFold(data.Type, "object") && len(data.Properties) > 0) ||
		strings.EqualFold(data.Type, "array")
	if !payload {
		return spec, ""
	}
	for name := range spec.Properties {
		if name == "data" {
			continue
		}
		if name != "meta" && name != "links" {
			return spec, ""
		}
	}
	return &data, "data"
}

// selectMediaType returns the media-type name whose schema is carried into the
// IR, chosen deterministically: application/json is preferred when it carries a
// schema, otherwise the first schema-bearing media type in lexicographic order
// (not map iteration order, which is random per run) so RequestSchema /
// ResponseSchema are deterministic for specs that use e.g.
// application/hal+json alongside application/problem+json (M-40). It returns ""
// when no media type carries a schema, so the request body media type is empty
// (the generator defaults to application/json) for bodiless or schema-less
// operations. The name is carried onto OperationMappingIR.MediaType so the
// generator emits the matching body encoding (A2).
func selectMediaType(content map[string]*parser.MediaType) string {
	if len(content) == 0 {
		return ""
	}
	for _, name := range sortedKeys(content) {
		mt := content[name]
		if strings.EqualFold(name, "application/json") && mt != nil && mt.Schema != nil {
			return name
		}
	}
	for _, name := range sortedKeys(content) {
		mt := content[name]
		if mt != nil && mt.Schema != nil {
			return name
		}
	}
	return ""
}

// RequestBodyKind classifies a request body media type into the encoding the
// generator emits. "json" covers application/json and any JSON dialect whose
// media type ends in "+json" (e.g. application/hal+json) — those encode as JSON.
// "form", "multipart", and "xml" map to the matching body builder. Any other
// media type is "unsupported": the operation stays honestly scaffolded and the
// transformer emits a fail-loud Warning so the construct is not silently
// dropped to a JSON body (A2). The comparison is case-insensitive and ignores an
// optional ";charset=..." suffix. Shared by the transformer warning and the
// generator dispatch so the two agree on what each media type means.
func RequestBodyKind(mt string) string {
	m := normalizeMediaType(mt)
	// An empty/absent media type defaults to JSON: a synthetic IR (test helpers)
	// or an operation whose request body content was not surfaced leaves
	// MediaType empty, and wiring a JSON body preserves the pre-A2 behavior and
	// the documented "JSON is the default for empty media types" contract. The
	// transformer's fail-loud warning is guarded on RequestMediaType != "" so an
	// absent media type never falsely reports an unsupported type (A2).
	if m == "" {
		return "json"
	}
	if isJSONMediaType(m) {
		return "json"
	}
	switch m {
	case "application/x-www-form-urlencoded":
		return "form"
	case "multipart/form-data":
		return "multipart"
	case "application/xml", "text/xml":
		return "xml"
	}
	return "unsupported"
}

// normalizeMediaType lower-cases the media type and strips an optional
// ";charset=..." (or other parameter) suffix so "application/json; charset=utf-8"
// matches "application/json".
func normalizeMediaType(mt string) string {
	mt = strings.ToLower(strings.TrimSpace(mt))
	if i := strings.IndexByte(mt, ';'); i >= 0 {
		mt = strings.TrimSpace(mt[:i])
	}
	return mt
}

// isJSONMediaType reports whether a normalized media type is application/json or
// a JSON dialect (suffix "+json"), all of which the generator encodes as JSON.
func isJSONMediaType(m string) bool {
	return m == "application/json" || strings.HasSuffix(m, "+json")
}

func headerNames(headers map[string]*parser.Header) []string {
	if len(headers) == 0 {
		return nil
	}
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// maxSchemaDepth caps how deeply schemaSpecFromParser will recurse into nested
// schemas. It is a backstop against unbounded recursion on a deeply-nested or
// cyclic spec that escapes the parser's own depth limits after $ref expansion
// (M-41). The parser marks $ref cycles as Opaque, which schemaSpecFromParser
// honors as an opaque boundary; this depth cap guards the remaining (non-cyclic)
// deep-nesting path.
const maxSchemaDepth = 1000

// maxCyclicDepth bounds how many cyclic $ref edges schemaSpecFromParser descends
// along a single path before cutting the cycle to an opaque (scalar-only)
// boundary. It preserves the first-entry properties of a circular schema (the
// target's direct fields are emitted) while keeping the generated IR finite and
// shallow: a cyclic ref is expanded up to maxCyclicDepth levels, then deeper
// re-entry is treated as an opaque boundary instead of recursing forever.
//
// This is the resolution to the circular-schema tension documented in
// docs/OPEN_ISSUE-circular-schema-generation.md: cutting at the ref holder
// (depth 0) regressed first-entry circular refs to DynamicAttribute, while
// unbounded expansion produced an enormous IR that hung generation. Expanding a
// fixed number of levels keeps first-entry properties and bounds output size,
// and — because cycleDepth is the only path-varying dimension for an Opaque ref
// (cyclic refs are not added to the visited set) — the conversion of a schema at
// a given cycleDepth is path-independent, so memoizing on (schema, cycleDepth)
// is sound.
const maxCyclicDepth = 2

// schemaMemo caches the SchemaSpec conversion of a (non-ref parser.Schema,
// cycleDepth) pair so a shared sub-schema in a DAG is converted once per
// top-level conversion instead of once per path (exponential on real-world
// specs). The cycleDepth dimension is required for cyclic schemas: a schema
// reached after k cyclic-ref descents has a different (bounded) shape than the
// same schema reached after k+1, and the cycleDepth key keeps those distinct
// without conflating them. The memo is scoped to a single schemaSpecFromParser
// call, so concurrent conversions (e.g. the API server handling requests in
// parallel) never share a cache.
type schemaMemoKey struct {
	schema     *parser.Schema
	cycleDepth int
}
type schemaMemo map[schemaMemoKey]*SchemaSpec

func schemaSpecFromParser(spec *parser.Spec, s *parser.Schema, diags *diagnostics.Diagnostics) *SchemaSpec {
	return schemaSpecFromParserDepth(spec, s, 0, 0, nil, make(schemaMemo), diags)
}

// schemaSpecFromParserDepth converts a parser.Schema into a SchemaSpec, recursing
// into items, object properties, and additionalProperties. A nested schema that
// is itself a $ref is resolved against spec.Components before descent (via
// resolveSchemaRef), so a body whose properties reference component schemas —
// not just a top-level request/response $ref — carries the referenced shape
// instead of falling back to Dynamic.
//
// cycleDepth counts how many cyclic (Opaque) $ref edges have been descended on
// the current path. It bounds circular-schema expansion: a cyclic ref is
// expanded up to maxCyclicDepth levels (preserving first-entry properties) and
// then cut to an opaque boundary, so the IR stays finite and shallow instead of
// re-expanding the dense component graph on every operation.
//
// Cycle safety is layered: (1) the parser marks $ref cycles as Opaque on the
// ref holder, and the Opaque branch below bounds expansion by cycleDepth; (2)
// resolveSchemaRef stops following a chained ref at a cycle; (3) the path-local
// visited set passed to each *acyclic* descent stops a $ref re-entered via
// object properties (a cross-property cycle the parser did not mark Opaque,
// e.g. a synthetic or malformed spec) from recursing to maxSchemaDepth. Cyclic
// (Opaque) refs are intentionally not added to visited, so the conversion of a
// schema at a given cycleDepth is path-independent and memoizing on
// (schema, cycleDepth) is sound. The visited set is copied only when an acyclic
// ref is resolved, so sibling properties (which do not grow the path) share it
// without interfering with each other.
func schemaSpecFromParserDepth(spec *parser.Spec, s *parser.Schema, depth, cycleDepth int, visited map[string]bool, memo schemaMemo, diags *diagnostics.Diagnostics) *SchemaSpec {
	if s == nil {
		return nil
	}
	// Resolve a nested $ref against the component schemas before processing, so
	// referenced properties/items/additionalProperties contribute their real
	// shape.
	if s.Ref != "" && spec != nil {
		refName := refBaseName(s.Ref)
		if s.Opaque {
			// A cyclic $ref. Bound expansion by cycleDepth so the circular schema
			// is expanded a fixed number of levels (preserving first-entry
			// properties) and then cut to an opaque boundary, instead of
			// re-expanding the cycle on every operation. Cyclic refs are not
			// added to the visited set: cycleDepth is the only path-varying
			// dimension, which keeps the (schema, cycleDepth) memo sound.
			if cycleDepth >= maxCyclicDepth {
				return &SchemaSpec{
					Type:     schemaTypeString(s.Type),
					Format:   s.Format,
					Nullable: s.Nullable,
					RefName:  refName,
				}
			}
			resolved := resolveSchemaRef(spec, s)
			out := schemaSpecFromParserDepth(spec, resolved, depth+1, cycleDepth+1, visited, memo, diags)
			if out != nil && out.RefName == "" {
				out.RefName = refName
			}
			return out
		}
		// An acyclic $ref. The visited set backstops cycles the parser did not
		// mark Opaque (a synthetic or malformed spec): if the same ref is already
		// on this descent path, stop — re-entering it is a cross-property cycle,
		// and descending would recurse to maxSchemaDepth and emit pathological
		// nesting. Treat the boundary as opaque (scalar fields only, no descent),
		// matching the Opaque handling below.
		if visited[s.Ref] {
			return &SchemaSpec{
				Type:     schemaTypeString(s.Type),
				Format:   s.Format,
				Nullable: s.Nullable,
				RefName:  refName,
			}
		}
		// Copy the path-local visited set and add this ref so descendants see it,
		// without mutating the set shared with sibling properties.
		pathVisited := make(map[string]bool, len(visited)+1)
		for k := range visited {
			pathVisited[k] = true
		}
		pathVisited[s.Ref] = true
		visited = pathVisited
		resolved := resolveSchemaRef(spec, s)
		out := schemaSpecFromParserDepth(spec, resolved, depth+1, cycleDepth, visited, memo, diags)
		if out != nil && out.RefName == "" {
			out.RefName = refName
		}
		return out
	}
	// A non-ref schema the parser marked Opaque (e.g. an additionalProperties
	// bare $ref that closes a cycle) is an opaque boundary too: keep its scalar
	// fields but do not descend, which would recurse forever (M-41).
	if s.Opaque {
		return &SchemaSpec{
			Type:     schemaTypeString(s.Type),
			Format:   s.Format,
			Nullable: s.Nullable,
		}
	}
	// A non-ref schema is converted once per (schema, cycleDepth) and memoized:
	// the schema graph is a DAG whose shared sub-schemas would otherwise be
	// re-converted along every path (exponential on real-world specs). With the
	// cycleDepth bound above, a schema participating in a $ref cycle is cut after
	// a fixed number of levels, so the conversion result depends only on
	// (schema, cycleDepth) and memoizing on that pair is sound. The cached result
	// is returned as a shallow copy so callers that stamp a RefName
	// (operationFromParser, unionSpecsFromParser) never mutate the cache.
	key := schemaMemoKey{schema: s, cycleDepth: cycleDepth}
	if cached, ok := memo[key]; ok {
		cp := *cached
		return &cp
	}
	out := schemaSpecFromParserDepthInner(spec, s, depth, cycleDepth, visited, memo, diags)
	memo[key] = out
	return out
}

// schemaSpecFromParserDepthInner converts a non-ref parser.Schema into a
// SchemaSpec, recursing into items, object properties, and additionalProperties.
// It is the memoized body of schemaSpecFromParserDepth; callers must go through
// the memoizing wrapper so shared sub-schemas are converted once.
func schemaSpecFromParserDepthInner(spec *parser.Spec, s *parser.Schema, depth, cycleDepth int, visited map[string]bool, memo schemaMemo, diags *diagnostics.Diagnostics) *SchemaSpec {
	out := &SchemaSpec{
		Type:        schemaTypeString(s.Type),
		Format:      s.Format,
		Nullable:    s.Nullable,
		UniqueItems: s.UniqueItems,
		WriteOnly:   s.WriteOnly,
		Required:    s.Required,
	}
	// A depth backstop keeps a pathologically deep (non-cyclic) schema from
	// stack-overflowing; the Opaque/visited/cycleDepth guards above handle
	// cycles.
	if depth >= maxSchemaDepth {
		return out
	}
	// Descents into items/properties/additionalProperties are not $ref descents,
	// so cycleDepth is propagated unchanged: only descending through a cyclic
	// $ref (handled in schemaSpecFromParserDepth) grows it.
	switch v := s.Items.(type) {
	case *parser.Schema:
		if v != nil {
			out.Items = schemaSpecFromParserDepth(spec, v, depth+1, cycleDepth, visited, memo, diags)
		}
	case bool:
		// items: false means no items are allowed; items: true is the permissive
		// default and dropping it is benign. Only the constraint (false) is both
		// unrepresentable in SchemaSpec and a real semantic loss, so warn (L-97).
		if !v {
			warnBooleanSchemaDropped(diags, "items", s.SourceLocation)
		}
	}
	if len(s.Properties) > 0 {
		out.Properties = make(map[string]SchemaSpec, len(s.Properties))
		for name, prop := range s.Properties {
			if prop == nil {
				continue
			}
			if child := schemaSpecFromParserDepth(spec, prop, depth+1, cycleDepth, visited, memo, diags); child != nil {
				out.Properties[name] = *child
			}
		}
	}
	switch v := s.AdditionalProperties.(type) {
	case *parser.Schema:
		if v != nil {
			out.AdditionalProperties = schemaSpecFromParserDepth(spec, v, depth+1, cycleDepth, visited, memo, diags)
		}
	case bool:
		// additionalProperties: false means the object has a closed property set
		// (which Terraform objects model by default, so dropping is benign); true
		// means arbitrary extras are allowed, which Terraform's closed object
		// type cannot represent. Warn on the permissive (true) case where the
		// generated schema will be stricter than the spec, and on false to make
		// the dropped constraint observable (L-97).
		warnBooleanSchemaDropped(diags, "additionalProperties", s.SourceLocation)
		_ = v
	}
	// allOf/oneOf/anyOf composition inside nested properties is handled by a
	// separate pass so the main conversion stays readable (REMAINING_GAPS §3).
	flattenCompositionInto(out, spec, s, depth, cycleDepth, visited, memo, diags)
	return out
}

// flattenCompositionInto folds allOf/oneOf/anyOf composition on s into the
// already-built out SchemaSpec. allOf is flattened (merged) so a nested
// property that composes several object schemas carries the union of their
// properties instead of falling back to Dynamic; each member is converted
// through schemaSpecFromParserDepth (which recursively flattens its own allOf
// and resolves its $refs) and merged via mergeAllOfSchemaSpec. oneOf/anyOf are
// captured onto out (OneOf/AnyOf/Discriminator, with variant RefNames) so the
// IR can represent the union (D1): a top-level composition (depth 0, the
// response/request root schema) wires through to generation as a
// discriminated SingleNestedAttribute or split resources, while a nested
// composition still renders as a Dynamic attribute and emits a fail-loud
// Warning (L-97) because the flat attribute model cannot switch on
// alternatives there.
func flattenCompositionInto(out *SchemaSpec, spec *parser.Spec, s *parser.Schema, depth, cycleDepth int, visited map[string]bool, memo schemaMemo, diags *diagnostics.Diagnostics) {
	for _, member := range s.AllOf {
		if member == nil {
			continue
		}
		merged := schemaSpecFromParserDepth(spec, member, depth+1, cycleDepth, visited, memo, diags)
		if merged == nil {
			continue
		}
		mergeAllOfSchemaSpec(out, *merged)
	}
	if len(s.OneOf) > 0 {
		out.OneOf = unionSpecsFromParser(spec, s.OneOf, depth, cycleDepth, visited, memo, diags)
		out.Discriminator = discriminatorSpecFromParser(s.Discriminator)
		if depth > 0 {
			warnCompositionNotModeled(diags, "oneOf", s.SourceLocation)
		}
	}
	if len(s.AnyOf) > 0 {
		out.AnyOf = unionSpecsFromParser(spec, s.AnyOf, depth, cycleDepth, visited, memo, diags)
		if depth > 0 {
			warnCompositionNotModeled(diags, "anyOf", s.SourceLocation)
		}
	}
}

// unionSpecsFromParser converts oneOf/anyOf variant schemas, recording each
// variant's RefName from its $ref's final segment (e.g. "Cat") so union
// variants carry the concrete schema names the split-resources strategy and
// the discriminator mapping need. Nil members are skipped.
func unionSpecsFromParser(spec *parser.Spec, members []*parser.Schema, depth, cycleDepth int, visited map[string]bool, memo schemaMemo, diags *diagnostics.Diagnostics) []SchemaSpec {
	out := make([]SchemaSpec, 0, len(members))
	for _, member := range members {
		if member == nil {
			continue
		}
		converted := schemaSpecFromParserDepth(spec, member, depth+1, cycleDepth, visited, memo, diags)
		if converted == nil {
			continue
		}
		if converted.RefName == "" {
			converted.RefName = refBaseName(member.Ref)
		}
		out = append(out, *converted)
	}
	return out
}

// discriminatorSpecFromParser converts an OpenAPI discriminator object,
// copying the mapping so later mutation cannot alias the parser's data.
func discriminatorSpecFromParser(d *parser.Discriminator) *DiscriminatorSpec {
	if d == nil {
		return nil
	}
	out := &DiscriminatorSpec{PropertyName: d.PropertyName}
	if len(d.Mapping) > 0 {
		out.Mapping = make(map[string]string, len(d.Mapping))
		for k, v := range d.Mapping {
			out.Mapping[k] = v
		}
	}
	return out
}

// refBaseName returns the final segment of a $ref (e.g. "Cat" for
// "#/components/schemas/Cat"), or "" for an empty ref.
func refBaseName(ref string) string {
	if ref == "" {
		return ""
	}
	if idx := strings.LastIndex(ref, "/"); idx >= 0 {
		return ref[idx+1:]
	}
	return ref
}

// mergeAllOfSchemaSpec merges src into dst the way an allOf member combines with
// its enclosing schema: scalar type/format fill empty dst fields, properties are
// first-wins (an existing property is not overwritten), required is unioned with
// deduplication, and items/additionalProperties fill empty dst fields. Bounds and
// other validation metadata are not carried on SchemaSpec, so this is a structural
// merge only, matching what schemaIRFromSpecRecursive consumes.
func mergeAllOfSchemaSpec(dst *SchemaSpec, src SchemaSpec) {
	if dst.Type == "" && src.Type != "" {
		dst.Type = src.Type
		dst.Format = src.Format
	}
	if dst.Items == nil && src.Items != nil {
		dst.Items = src.Items
	}
	if dst.AdditionalProperties == nil && src.AdditionalProperties != nil {
		dst.AdditionalProperties = src.AdditionalProperties
	}
	if len(src.Properties) > 0 {
		if dst.Properties == nil {
			dst.Properties = make(map[string]SchemaSpec, len(src.Properties))
		}
		for name, prop := range src.Properties {
			if _, exists := dst.Properties[name]; !exists {
				dst.Properties[name] = prop
			}
		}
	}
	if len(src.Required) > 0 {
		seen := make(map[string]bool, len(dst.Required)+len(src.Required))
		for _, r := range dst.Required {
			seen[r] = true
		}
		for _, r := range src.Required {
			if !seen[r] {
				seen[r] = true
				dst.Required = append(dst.Required, r)
			}
		}
	}
}

// warnCompositionNotModeled records a warning that a oneOf/anyOf composition in a
// nested property could not be represented as a single Terraform attribute and
// was left as-is (Dynamic when no concrete type/properties were present). A nil
// diags sink makes the helper a no-op so callers without a diagnostics channel
// are not affected (L-97).
func warnCompositionNotModeled(diags *diagnostics.Diagnostics, kind string, loc parser.SourceLocation) {
	if diags == nil {
		return
	}
	*diags = diags.Append(diagnostics.Diagnostic{
		Severity:       diagnostics.Warning,
		Summary:        fmt.Sprintf("%s composition not modeled", kind),
		Detail:         fmt.Sprintf("%s describes alternative schemas that the flat Terraform attribute model cannot represent; the attribute falls back to Dynamic. Split the schema or use a generator.yaml override if a concrete type is required.", kind),
		SourceLocation: locPtrOrNil(loc),
	})
}

// warnBooleanSchemaDropped records a warning that a JSON Schema 2020-12 boolean
// schema (items: false / additionalProperties: false|true) could not be
// represented in the Terraform schema model and was dropped. A nil diags sink
// makes the helper a no-op so callers without a diagnostics channel are not
// affected.
func warnBooleanSchemaDropped(diags *diagnostics.Diagnostics, field string, loc parser.SourceLocation) {
	if diags == nil {
		return
	}
	*diags = diags.Append(diagnostics.Diagnostic{
		Severity:       diagnostics.Warning,
		Summary:        fmt.Sprintf("boolean %s schema dropped", field),
		Detail:         fmt.Sprintf("%s is a JSON Schema 2020-12 boolean schema that the Terraform schema model cannot represent; the constraint is dropped", field),
		SourceLocation: locPtrOrNil(loc),
	})
}

// warnFormDataParameters records a fail-loud warning for each formData
// parameter that cannot be wired. Primitive formData parameters (string,
// integer, number, boolean) are now wired: the generator sends them as
// application/x-www-form-urlencoded, and ManagedResourceSchema surfaces them as
// writable schema attributes (REMAINING_GAPS §2). A non-primitive formData
// parameter (e.g. a file upload, object, or array) cannot be form-encoded from a
// typed attribute, so the generator keeps the operation honestly scaffolded
// and this warning surfaces why, so the construct is never silently dropped. A
// nil diags sink makes the helper a no-op so callers without a diagnostics
// channel are not affected.
func warnFormDataParameters(diags *diagnostics.Diagnostics, op *parser.Operation, pi *parser.PathItem) {
	if diags == nil {
		return
	}
	seen := make(map[string]bool)
	emit := func(p parser.Parameter) {
		if !strings.EqualFold(p.In, "formData") || seen[p.Name] {
			return
		}
		seen[p.Name] = true
		if isWireableFormDataPrimitive(p) {
			// Primitive formData is wired as application/x-www-form-urlencoded;
			// no warning, because the construct is not dropped.
			return
		}
		*diags = diags.Append(diagnostics.Diagnostic{
			Severity: diagnostics.Warning,
			Summary:  "formData parameter not wired",
			Detail: fmt.Sprintf(
				"parameter %q is declared in: formData with a non-primitive type. The generated request body form-encodes primitive (string/integer/number/boolean) formData parameters, but this parameter's type cannot be form-encoded from a typed attribute, so this operation is not wired to a remote API endpoint. Use a primitive formData type, a JSON request body, or a generator.yaml override.",
				p.Name,
			),
			SourceLocation: locPtrOrNil(p.SourceLocation),
		})
	}
	if op != nil {
		for _, p := range op.Parameters {
			emit(p)
		}
	}
	if pi != nil {
		for _, p := range pi.Parameters {
			emit(p)
		}
	}
}

// locPtrOrNil returns a pointer to loc when it carries a file, or nil when the
// location is empty, so diagnostics without a source do not carry a misleading
// zero-value location.
func locPtrOrNil(loc parser.SourceLocation) *diagnostics.SourceLocation {
	if loc.IsEmpty() {
		return nil
	}
	cp := loc
	return &cp
}

func schemaTypeString(t any) string {
	switch v := t.(type) {
	case string:
		return v
	case []any:
		var nonNull []string
		for _, item := range v {
			if s, ok := item.(string); ok && s != "null" {
				nonNull = append(nonNull, s)
			}
		}
		switch len(nonNull) {
		case 0:
			return ""
		case 1:
			return nonNull[0]
		default:
			// JSON Schema 3.1 multi-type unions are not directly representable in
			// Terraform Plugin Framework; fall back to Dynamic (empty type) so the
			// generator can still emit a valid schema.
			return ""
		}
	default:
		return ""
	}
}

// isWireableFormDataPrimitive reports whether a formData parameter's schema is
// a primitive the generated request body can form-encode (string, integer,
// number, or boolean). A file upload, object, array, or absent schema cannot
// be form-encoded from a typed attribute, so the operation carrying it stays
// honestly scaffolded and warnFormDataParameters surfaces why (REMAINING_GAPS
// §2).
func isWireableFormDataPrimitive(p parser.Parameter) bool {
	if p.Schema == nil {
		return false
	}
	switch schemaTypeString(p.Schema.Type) {
	case "string", "integer", "number", "boolean":
		return true
	}
	return false
}

func shallowCopyExtensions(ext map[string]any) map[string]any {
	if len(ext) == 0 {
		return nil
	}
	out := make(map[string]any, len(ext))
	for k, v := range ext {
		out[k] = v
	}
	return out
}
