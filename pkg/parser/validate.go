package parser

import (
	"fmt"
	"strings"
)

// ptrNodeLoc returns a pointer to the source location of n.
func ptrNodeLoc(n Node) *SourceLocation {
	loc := nodeLoc(n)
	return &loc
}

// schemaTypeString extracts a type name from a schema "type" scalar. It treats
// the YAML/JSON null scalar as the string "null" because OpenAPI 3.1 allows
// "null" in a type array, while ordinary string coercion would discard it.
func schemaTypeString(n Node) (string, bool) {
	if s, ok := n.(*ScalarNode); ok && s.Value == nil && strings.EqualFold(s.Raw, "null") {
		return "null", true
	}
	return asString(n)
}

// Validate performs structural validation and semantic checks against an
// already-converted Spec and its original raw AST. It returns diagnostics for
// missing required fields, invalid local $ref values, unsupported keywords, and
// type mismatches. Every diagnostic carries a source location when possible.
func Validate(root Node, spec *Spec, version Version) []Diagnostic {
	if spec == nil {
		return nil
	}
	var diags []Diagnostic

	diags = append(diags, validateRequired(spec, version)...)
	diags = append(diags, validateNestedRequired(spec, version)...)
	if root != nil {
		diags = append(diags, validateRefs(root, spec)...)
		diags = append(diags, validateUnsupportedKeywords(root, version)...)
		diags = append(diags, validateTypes(root, version)...)
		diags = append(diags, validateDuplicateKeys(root)...)
	}

	return diags
}

// validateNestedRequired checks the spec-mandated required fields that live
// below the document root: every operation must declare a non-empty responses
// object, every parameter must declare name and in, path parameters must set
// required: true, and (in 2.0/3.0) every response must declare a description.
// These were previously unchecked despite the validateRequired docstring
// claiming "nested requirements" (M-1). $ref parameters and responses are
// skipped here; their definitions are validated at their own site.
// nestedRequiredValidator accumulates diagnostics for spec-mandated required
// fields below the document root. It is a struct (rather than closures) so each
// per-site check stays below the gocognit threshold.
type nestedRequiredValidator struct {
	diags   []Diagnostic
	version Version
}

func (v *nestedRequiredValidator) checkParameter(p *Parameter, where string) {
	if p == nil || p.Ref != "" {
		return
	}
	if p.Name == "" {
		v.diags = append(v.diags, Diagnostic{
			Severity:       SeverityError,
			Summary:        "Missing required field",
			Detail:         fmt.Sprintf("Parameter in %s is missing the required 'name' field.", where),
			SourceLocation: &p.SourceLocation,
		})
	}
	if p.In == "" {
		v.diags = append(v.diags, Diagnostic{
			Severity:       SeverityError,
			Summary:        "Missing required field",
			Detail:         fmt.Sprintf("Parameter %q in %s is missing the required 'in' field.", p.Name, where),
			SourceLocation: &p.SourceLocation,
		})
	}
	if p.In == "path" && !p.Required {
		v.diags = append(v.diags, Diagnostic{
			Severity:       SeverityError,
			Summary:        "Missing required field",
			Detail:         fmt.Sprintf("Path parameter %q in %s must set required: true.", p.Name, where),
			SourceLocation: &p.SourceLocation,
		})
	}
}

func (v *nestedRequiredValidator) checkResponse(r *Response, where string) {
	if r == nil || r.Ref != "" {
		return
	}
	// OpenAPI 3.1 (JSON Schema 2020-12) made response description optional;
	// 2.0 and 3.0 require it.
	if v.version != Version3_1 && r.Description == "" {
		v.diags = append(v.diags, Diagnostic{
			Severity:       SeverityError,
			Summary:        "Missing required field",
			Detail:         fmt.Sprintf("Response in %s is missing the required 'description' field.", where),
			SourceLocation: &r.SourceLocation,
		})
	}
}

func (v *nestedRequiredValidator) checkOperation(op *Operation, where string) {
	if op == nil {
		return
	}
	if len(op.Responses) == 0 {
		v.diags = append(v.diags, Diagnostic{
			Severity:       SeverityError,
			Summary:        "Missing required field",
			Detail:         fmt.Sprintf("Operation %s is missing the required 'responses' object.", where),
			SourceLocation: &op.SourceLocation,
		})
	}
	for i := range op.Parameters {
		v.checkParameter(&op.Parameters[i], where)
	}
	for _, r := range op.Responses {
		v.checkResponse(r, where)
	}
}

func (v *nestedRequiredValidator) checkPathItem(pi *PathItem, where string) {
	if pi == nil {
		return
	}
	for i := range pi.Parameters {
		v.checkParameter(&pi.Parameters[i], where)
	}
	for _, m := range []struct {
		method string
		op     *Operation
	}{
		{"GET", pi.Get},
		{"PUT", pi.Put},
		{"POST", pi.Post},
		{"DELETE", pi.Delete},
		{"OPTIONS", pi.Options},
		{"HEAD", pi.Head},
		{"PATCH", pi.Patch},
		{"TRACE", pi.Trace},
	} {
		v.checkOperation(m.op, fmt.Sprintf("%s %s", m.method, where))
	}
}

func validateNestedRequired(spec *Spec, version Version) []Diagnostic {
	v := &nestedRequiredValidator{version: version}
	for path, pi := range spec.Paths {
		v.checkPathItem(pi, path)
	}
	for name, pi := range spec.Webhooks {
		v.checkPathItem(pi, "webhook "+name)
	}
	if spec.Components != nil {
		for name, p := range spec.Components.Parameters {
			v.checkParameter(p, "components.parameters."+name)
		}
		for name, r := range spec.Components.Responses {
			v.checkResponse(r, "components.responses."+name)
		}
	}
	return v.diags
}

// validateDuplicateKeys walks the raw AST and emits a warning for every mapping
// that contains a duplicate key. Duplicate keys are invalid in both JSON and
// YAML; the converters resolve them last-wins (matching encoding/json and
// yaml.v3), while $ref resolution previously saw the first occurrence — two
// inconsistent views of one document. The warning makes the collapse loud, and
// findMapEntry/findEntryValue now agree with the converters on last-wins (H-2).
func validateDuplicateKeys(root Node) []Diagnostic {
	var diags []Diagnostic
	var walk func(n Node)
	walk = func(n Node) {
		if n == nil {
			return
		}
		switch v := n.(type) {
		case *MapNode:
			seen := make(map[string]bool, len(v.Entries))
			for _, e := range v.Entries {
				if e.Key == nil {
					continue
				}
				key, ok := asString(e.Key)
				if !ok {
					key = e.Key.Raw
				}
				if key == "" {
					continue
				}
				if seen[key] {
					diags = append(diags, Diagnostic{
						Severity:       SeverityWarning,
						Summary:        "Duplicate mapping key",
						Detail:         fmt.Sprintf("Mapping key %q appears more than once; the last occurrence wins.", key),
						SourceLocation: &e.Key.SourceLocation,
					})
				}
				seen[key] = true
				walk(e.Value)
			}
		case *SequenceNode:
			for _, item := range v.Items {
				walk(item)
			}
		}
	}
	walk(root)
	return diags
}

// validateRequired checks that the spec satisfies OpenAPI/Swagger required
// top-level and nested requirements.
func validateRequired(spec *Spec, version Version) []Diagnostic {
	var diags []Diagnostic

	switch version {
	case Version2_0:
		if spec.Swagger == "" {
			diags = append(diags, Diagnostic{
				Severity:       SeverityError,
				Summary:        "Missing required field",
				Detail:         "swagger version string is required.",
				SourceLocation: &spec.SourceLocation,
			})
		}
	case Version3_0, Version3_1:
		if spec.OpenAPI == "" {
			diags = append(diags, Diagnostic{
				Severity:       SeverityError,
				Summary:        "Missing required field",
				Detail:         "openapi version string is required.",
				SourceLocation: &spec.SourceLocation,
			})
		}
	}

	if version == Version2_0 || version == Version3_0 {
		// A missing 'paths' key produces a nil map; an empty 'paths:' value in YAML
		// produces a non-nil empty map, which is valid and therefore not reported.
		if spec.Paths == nil {
			diags = append(diags, Diagnostic{
				Severity:       SeverityError,
				Summary:        "Missing required field",
				Detail:         "paths is required.",
				SourceLocation: &spec.SourceLocation,
			})
		}
	}

	if spec.Info == nil {
		diags = append(diags, Diagnostic{
			Severity:       SeverityError,
			Summary:        "Missing required field",
			Detail:         "OpenAPI document is missing the required 'info' object.",
			SourceLocation: &spec.SourceLocation,
		})
	} else {
		if spec.Info.Title == "" {
			diags = append(diags, Diagnostic{
				Severity:       SeverityError,
				Summary:        "Missing required field",
				Detail:         "info.title is required.",
				SourceLocation: &spec.Info.SourceLocation,
			})
		}
		if spec.Info.Version == "" {
			diags = append(diags, Diagnostic{
				Severity:       SeverityError,
				Summary:        "Missing required field",
				Detail:         "info.version is required.",
				SourceLocation: &spec.Info.SourceLocation,
			})
		}
	}

	return diags
}

// validateRefs scans the raw AST for $ref values and verifies that each one
// resolves. Relative files are followed only when a local entry document has
// enabled the bounded resolver; other callers remain same-document only.
type refValidator struct {
	root     Node
	resolver *localRefResolver
	seen     map[string]bool
	walked   map[Node]bool
	diags    []Diagnostic
}

func (rv *refValidator) walk(n Node, depth int) {
	if n == nil || rv.walked[n] {
		return
	}
	rv.walked[n] = true
	switch v := n.(type) {
	case *MapNode:
		for _, e := range v.Entries {
			if e.Key == nil {
				continue
			}
			key, _ := asString(e.Key)
			if key == "$ref" {
				rv.checkRef(e.Value, depth)
				continue
			}
			rv.walk(e.Value, depth)
		}
	case *SequenceNode:
		for _, item := range v.Items {
			rv.walk(item, depth)
		}
	}
}

func (rv *refValidator) checkRef(value Node, depth int) {
	ref, ok := asString(value)
	if !ok {
		// A non-string $ref (e.g. `$ref: 123` or `$ref: {…}`) is invalid and
		// must not pass silently; the converter's scalarString would only report
		// a warning, so flag it here as an error (N-10).
		loc := nodeLoc(value)
		rv.diags = append(rv.diags, Diagnostic{
			Severity:       SeverityError,
			Summary:        "Invalid $ref",
			Detail:         "$ref must be a string containing a JSON Pointer reference.",
			SourceLocation: &loc,
		})
		return
	}
	var (
		target Node
		key    = ref
		d      []Diagnostic
	)
	if rv.resolver == nil {
		target, d = ResolveLocalRef(rv.root, ref, nodeLoc(value))
	} else {
		target, key, d = rv.resolver.resolve(ref, nodeLoc(value))
	}
	if rv.seen[key] {
		return
	}
	rv.seen[key] = true
	if len(d) > 0 {
		// Preserve the source location of the $ref value, which
		// ResolveLocalRef already provides.
		rv.diags = append(rv.diags, d...)
		return
	}
	if depth >= maxReferenceDepth {
		loc := nodeLoc(value)
		rv.diags = append(rv.diags, Diagnostic{
			Severity:       SeverityError,
			Summary:        "Reference depth limit exceeded",
			Detail:         fmt.Sprintf("Local references may be nested at most %d levels.", maxReferenceDepth),
			SourceLocation: &loc,
		})
		return
	}
	rv.walk(target, depth+1)
}

func validateRefs(root Node, spec *Spec) []Diagnostic {
	rv := &refValidator{root: root, seen: make(map[string]bool), walked: make(map[Node]bool)}
	if spec != nil {
		rv.resolver = spec.localRefs
	}
	rv.walk(root, 0)
	return rv.diags
}

// keySet describes the allowed keys for an OpenAPI object and the human-readable
// object name used in diagnostics.
type keySet struct {
	name    string
	allowed map[string]bool
}

// validateUnsupportedKeywords walks the raw AST and emits warnings for any object
// key that is not recognized for the object's type in the detected version.
// Object type is tracked as the tree is descended, and named maps (paths,
// components sub-maps, schema properties, callback runtime expressions, etc.) are
// recognized so that their arbitrary name keys do not produce false positives.
func validateUnsupportedKeywords(root Node, version Version) []Diagnostic {
	v := &unsupportedKeywordValidator{version: version}
	v.walk(root, "root", false)
	return v.diags
}

type unsupportedKeywordValidator struct {
	version Version
	diags   []Diagnostic
}

func (v *unsupportedKeywordValidator) walk(n Node, objType string, namedMap bool) {
	if n == nil {
		return
	}
	switch m := n.(type) {
	case *MapNode:
		v.checkMapEntries(m, objType, namedMap)
		for _, e := range m.Entries {
			if e.Key == nil {
				continue
			}
			key, _ := asString(e.Key)
			childType, childNamed := childState(objType, namedMap, key)
			v.walk(e.Value, childType, childNamed)
		}
	case *SequenceNode:
		itemType := sequenceItemType(objType)
		for _, item := range m.Items {
			v.walk(item, itemType, false)
		}
	}
}

func (v *unsupportedKeywordValidator) checkMapEntries(m *MapNode, objType string, namedMap bool) {
	if namedMap {
		return
	}
	set, ok := knownKeys[v.version][objType]
	if !ok {
		return
	}
	for _, e := range m.Entries {
		if e.Key == nil {
			continue
		}
		key, _ := asString(e.Key)
		if key == "" {
			continue
		}
		// OpenAPI vendor extensions (keys beginning with x- or X-) are allowed
		// in any object. When the known set is empty, arbitrary keys are
		// permitted (e.g. security requirement names).
		if len(set.allowed) == 0 || strings.HasPrefix(strings.ToLower(key), "x-") {
			continue
		}
		if set.allowed[key] {
			continue
		}
		v.diags = append(v.diags, Diagnostic{
			Severity:       SeverityWarning,
			Summary:        "Unsupported keyword",
			Detail:         fmt.Sprintf("%s does not support %q.", set.name, key),
			SourceLocation: &e.Key.SourceLocation,
		})
	}
}

// childState returns the object type and named-map flag for a child value. The
// parent may itself be a named map, in which case every key is a name and the
// child value is an object of the parent's element type.
func childState(parent string, parentNamed bool, key string) (string, bool) {
	if parentNamed {
		// A callback value is itself a map of runtime expressions to PathItems.
		if parent == "callback" {
			return "pathItem", true
		}
		return parent, false
	}
	return childDesc(parent, key)
}

// childDescTable maps "parent|key" to the child object type and whether the child
// value is a named map. It is built once from the static parent/child rules below.
var childDescTable = buildChildDescTable()

func buildChildDescTable() map[string]childDescEntry {
	t := make(map[string]childDescEntry)
	add := func(parent, key, typ string, named bool) {
		t[parent+"|"+key] = childDescEntry{typ: typ, named: named}
	}
	add("root", "info", "info", false)
	add("root", "servers", "servers", false)
	add("root", "paths", "pathItem", true)
	add("root", "webhooks", "pathItem", true)
	add("root", "components", "components", false)
	add("root", "security", "security", false)
	add("root", "tags", "tags", false)
	add("root", "externalDocs", "externalDocs", false)
	add("root", "definitions", "schema", true)
	add("root", "parameters", "parameter", true)
	add("root", "responses", "response", true)
	add("root", "securityDefinitions", "securityScheme", true)
	add("info", "contact", "contact", false)
	add("info", "license", "license", false)
	add("server", "variables", "serverVariable", true)
	add("pathItem", "parameters", "parameters", false)
	add("pathItem", "servers", "servers", false)
	add("requestBody", "content", "mediaType", true)
	add("encoding", "headers", "header", true)
	add("securityScheme", "flows", "oauthFlows", false)
	add("tag", "externalDocs", "externalDocs", false)
	add("link", "server", "server", false)
	add("discriminator", "mapping", "", true)
	for _, method := range []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"} {
		add("pathItem", method, "operation", false)
	}
	for _, key := range []string{"parameters", "tags", "security", "servers"} {
		add("operation", key, key, false)
	}
	add("operation", "requestBody", "requestBody", false)
	add("operation", "responses", "response", true)
	add("operation", "callbacks", "callback", true)
	add("operation", "externalDocs", "externalDocs", false)
	for _, parent := range []string{"parameter", "header"} {
		add(parent, "schema", "schema", false)
		add(parent, "content", "mediaType", true)
		add(parent, "examples", "example", true)
	}
	for _, key := range []string{"schema", "encoding", "examples"} {
		add("mediaType", key, key, key != "schema")
	}
	for _, key := range []string{"headers", "content", "links", "schema"} {
		named := key != "schema"
		add("response", key, key, named)
	}
	add("components", "schemas", "schema", true)
	add("components", "responses", "response", true)
	add("components", "parameters", "parameter", true)
	add("components", "examples", "example", true)
	add("components", "requestBodies", "requestBody", true)
	add("components", "headers", "header", true)
	add("components", "securitySchemes", "securityScheme", true)
	add("components", "links", "link", true)
	add("components", "callbacks", "callback", true)
	for _, key := range []string{"allOf", "oneOf", "anyOf", "prefixItems"} {
		add("schema", key, key, false)
	}
	for _, key := range []string{"items", "not", "contains", "propertyNames", "unevaluatedProperties", "additionalProperties"} {
		add("schema", key, "schema", false)
	}
	// JSON Schema 2020-12 (OpenAPI 3.1) schema-valued keywords previously
	// missing from the descent table (H-17): if/then/else, unevaluatedItems, and
	// contentSchema are each a single schema; dependentSchemas is a named map of
	// schemas. Without these entries validation never descended into the
	// subtrees, so unsupported keywords or type errors inside them went silent.
	for _, key := range []string{"if", "then", "else", "unevaluatedItems", "contentSchema"} {
		add("schema", key, "schema", false)
	}
	add("schema", "dependentSchemas", "schema", true)
	for _, key := range []string{"properties", "patternProperties"} {
		add("schema", key, "schema", true)
	}
	add("schema", "discriminator", "discriminator", false)
	add("schema", "xml", "xml", false)
	add("schema", "externalDocs", "externalDocs", false)
	for _, key := range []string{"implicit", "password", "clientCredentials", "authorizationCode"} {
		add("oauthFlows", key, "oauthFlow", false)
	}
	add("link", "parameters", "", true)
	return t
}

type childDescEntry struct {
	typ   string
	named bool
}

// childDesc maps a parent object type and child key to the child value's object
// type and whether the child value is a named map. An empty object type means
// the child is not a known object for validation purposes.
//
// This is only reached when the parent is NOT a named map (childState handles
// the named-map case, including callbacks, before delegating here). Both
// callback registrations mark the parent named, so a "callback" parent never
// reaches this function; the prior `parent == "callback"` branch was dead
// duplicated logic (L-90).
func childDesc(parent, key string) (string, bool) {
	if parent == "pathItem" && isHTTPMethod(key) {
		return "operation", false
	}
	if e, ok := childDescTable[parent+"|"+key]; ok {
		return e.typ, e.named
	}
	return "", false
}

// sequenceItemType maps a parent sequence's object type to the type of its
// items. An empty return value means the items are not validated as a known
// object type.
func sequenceItemType(parent string) string {
	switch parent {
	case "servers":
		return "server"
	case "tags":
		return "tag"
	case "parameters":
		return "parameter"
	case "security":
		return "securityRequirement"
	case "allOf", "oneOf", "anyOf", "prefixItems":
		return "schema"
	}
	return ""
}

// validateTypes checks that major OpenAPI objects have the expected structural
// type. Emits an error when a required object is a scalar, a sequence is
// expected, etc.
func validateTypes(root Node, version Version) []Diagnostic {
	var diags []Diagnostic
	if root == nil {
		return diags
	}

	m, ok := root.(*MapNode)
	if !ok {
		return diags
	}

	diags = append(diags, validateTopLevelTypes(m, version)...)
	diags = append(diags, validateInfoStringTypes(m)...)
	diags = append(diags, validateNamedMapObjectTypes(m, "paths", "paths.%s")...)
	diags = append(diags, validateSequenceObjectTypes(m, "servers", "servers[%d]")...)
	diags = append(diags, validateSequenceObjectTypes(m, "tags", "tags[%d]")...)
	if version != Version2_0 {
		diags = append(diags, validateNamedMapObjectTypes(m, "components", "components.%s")...)
	}

	// Validate schema 'type' values. The OpenAPI spec allows either a string or,
	// in 3.1, an array of strings. Unknown primitive types produce a warning.
	validateSchemaTypes(root, version, &diags)

	return diags
}

func validateTopLevelTypes(m *MapNode, version Version) []Diagnostic {
	var diags []Diagnostic
	expectObject := func(key, objName string) {
		node := findMapEntry(m, key)
		if node == nil {
			return
		}
		if _, ok := node.Value.(*MapNode); !ok {
			diags = append(diags, Diagnostic{
				Severity:       SeverityError,
				Summary:        "Invalid OpenAPI structure",
				Detail:         fmt.Sprintf("%s must be an object, got %T", objName, node.Value),
				SourceLocation: ptrNodeLoc(node.Value),
			})
		}
	}
	expectArray := func(key, objName string) {
		node := findMapEntry(m, key)
		if node == nil {
			return
		}
		if _, ok := node.Value.(*SequenceNode); !ok {
			diags = append(diags, Diagnostic{
				Severity:       SeverityError,
				Summary:        "Invalid OpenAPI structure",
				Detail:         fmt.Sprintf("%s must be an array, got %T", objName, node.Value),
				SourceLocation: ptrNodeLoc(node.Value),
			})
		}
	}

	expectObject("info", "info")
	expectObject("paths", "paths")
	expectArray("servers", "servers")
	expectArray("tags", "tags")
	if version != Version2_0 {
		expectObject("components", "components")
		expectObject("webhooks", "webhooks")
	}
	return diags
}

func validateInfoStringTypes(m *MapNode) []Diagnostic {
	var diags []Diagnostic
	infoNode := findMapEntry(m, "info")
	if infoNode == nil {
		return diags
	}
	infoMap, ok := infoNode.Value.(*MapNode)
	if !ok {
		return diags
	}
	for _, pair := range []struct{ key, objName string }{
		{"title", "info.title"},
		{"version", "info.version"},
	} {
		e := findMapEntry(infoMap, pair.key)
		if e == nil || e.Value == nil {
			continue
		}
		diags = append(diags, typeMismatchStringDiag(e.Value, pair.objName)...)
	}
	return diags
}

func typeMismatchStringDiag(val Node, objName string) []Diagnostic {
	switch v := val.(type) {
	case *ScalarNode:
		if _, ok := asString(v); ok {
			return nil
		}
		return []Diagnostic{{
			Severity:       SeverityError,
			Summary:        "Type mismatch",
			Detail:         fmt.Sprintf("%s must be a string, got non-string scalar", objName),
			SourceLocation: ptrNodeLoc(v),
		}}
	case *MapNode:
		return []Diagnostic{{
			Severity:       SeverityError,
			Summary:        "Type mismatch",
			Detail:         fmt.Sprintf("%s must be a string, got an object", objName),
			SourceLocation: ptrNodeLoc(v),
		}}
	case *SequenceNode:
		return []Diagnostic{{
			Severity:       SeverityError,
			Summary:        "Type mismatch",
			Detail:         fmt.Sprintf("%s must be a string, got an array", objName),
			SourceLocation: ptrNodeLoc(v),
		}}
	}
	return nil
}

func validateNamedMapObjectTypes(m *MapNode, field, detailFormat string) []Diagnostic {
	var diags []Diagnostic
	node := findMapEntry(m, field)
	if node == nil {
		return diags
	}
	childMap, ok := node.Value.(*MapNode)
	if !ok {
		return diags
	}
	for _, e := range childMap.Entries {
		if e.Key == nil {
			continue
		}
		if _, ok := e.Value.(*MapNode); !ok {
			diags = append(diags, Diagnostic{
				Severity:       SeverityError,
				Summary:        "Invalid OpenAPI structure",
				Detail:         fmt.Sprintf(detailFormat+" must be an object, got %T", keyString(e.Key), e.Value),
				SourceLocation: ptrNodeLoc(e.Value),
			})
		}
	}
	return diags
}

func validateSequenceObjectTypes(m *MapNode, field, detailFormat string) []Diagnostic {
	var diags []Diagnostic
	node := findMapEntry(m, field)
	if node == nil {
		return diags
	}
	seq, ok := node.Value.(*SequenceNode)
	if !ok {
		return diags
	}
	for i, item := range seq.Items {
		if _, ok := item.(*MapNode); !ok {
			diags = append(diags, Diagnostic{
				Severity:       SeverityError,
				Summary:        "Invalid OpenAPI structure",
				Detail:         fmt.Sprintf(detailFormat+" must be an object, got %T", i, item),
				SourceLocation: ptrNodeLoc(item),
			})
		}
	}
	return diags
}

// validateSchemaTypes walks the raw AST and emits diagnostics for invalid schema
// type values. It only checks 'type' keys that actually belong to schema objects.
func validateSchemaTypes(root Node, version Version, diags *[]Diagnostic) {
	validTypes := map[string]bool{
		"array":   true,
		"boolean": true,
		"integer": true,
		"number":  true,
		"object":  true,
		"string":  true,
	}
	switch version {
	case Version2_0:
		validTypes["file"] = true
	case Version3_1:
		// `null` is a first-class JSON Schema primitive only in OpenAPI 3.1
		// (JSON Schema Core 2020-12). In 3.0 the mechanism is `nullable: true`,
		// and a standalone `type: null` is not valid, so the validator flags it
		// rather than silently accepting it (L-89).
		validTypes["null"] = true
	}
	v := &schemaTypeValidator{validTypes: validTypes, version: version, diags: diags}
	v.walk(root, "root", false)
}

type schemaTypeValidator struct {
	validTypes map[string]bool
	version    Version
	diags      *[]Diagnostic
}

func (stv *schemaTypeValidator) walk(n Node, objType string, namedMap bool) {
	if n == nil {
		return
	}
	switch v := n.(type) {
	case *MapNode:
		if !namedMap && objType == "schema" {
			stv.checkSchemaTypeEntries(v.Entries)
		}
		for _, e := range v.Entries {
			if e.Key == nil {
				continue
			}
			key, _ := asString(e.Key)
			childType, childNamed := childState(objType, namedMap, key)
			stv.walk(e.Value, childType, childNamed)
		}
	case *SequenceNode:
		itemType := sequenceItemType(objType)
		for _, item := range v.Items {
			stv.walk(item, itemType, false)
		}
	}
}

func (stv *schemaTypeValidator) checkSchemaTypeEntries(entries []MapEntry) {
	for _, e := range entries {
		if e.Key == nil {
			continue
		}
		key, _ := asString(e.Key)
		if key != "type" {
			continue
		}
		stv.checkTypeValue(e.Value)
	}
}

func (stv *schemaTypeValidator) checkTypeValue(val Node) {
	switch v := val.(type) {
	case *ScalarNode:
		stv.checkScalarType(v)
	case *SequenceNode:
		stv.checkSequenceType(v)
	default:
		*stv.diags = append(*stv.diags, Diagnostic{
			Severity:       SeverityError,
			Summary:        "Type mismatch",
			Detail:         "schema type must be a string or an array of strings.",
			SourceLocation: ptrNodeLoc(v),
		})
	}
}

func (stv *schemaTypeValidator) checkScalarType(val *ScalarNode) {
	if s, ok := schemaTypeString(val); ok {
		if !stv.validTypes[s] {
			*stv.diags = append(*stv.diags, Diagnostic{
				Severity:       SeverityWarning,
				Summary:        "Unsupported schema type",
				Detail:         fmt.Sprintf("schema type %q is not a recognized JSON Schema primitive type.", s),
				SourceLocation: ptrNodeLoc(val),
			})
		}
		return
	}
	*stv.diags = append(*stv.diags, Diagnostic{
		Severity:       SeverityError,
		Summary:        "Type mismatch",
		Detail:         "schema type must be a string or an array of strings.",
		SourceLocation: ptrNodeLoc(val),
	})
}

func (stv *schemaTypeValidator) checkSequenceType(val *SequenceNode) {
	if stv.version != Version3_1 {
		*stv.diags = append(*stv.diags, Diagnostic{
			Severity:       SeverityError,
			Summary:        "Type mismatch",
			Detail:         "schema type as an array is only supported in OpenAPI 3.1.",
			SourceLocation: ptrNodeLoc(val),
		})
		return
	}
	for i, item := range val.Items {
		if s, ok := schemaTypeString(item); ok {
			if !stv.validTypes[s] {
				*stv.diags = append(*stv.diags, Diagnostic{
					Severity:       SeverityWarning,
					Summary:        "Unsupported schema type",
					Detail:         fmt.Sprintf("schema type array entry %d (%q) is not a recognized JSON Schema primitive type.", i, s),
					SourceLocation: ptrNodeLoc(item),
				})
			}
		} else {
			*stv.diags = append(*stv.diags, Diagnostic{
				Severity:       SeverityError,
				Summary:        "Type mismatch",
				Detail:         fmt.Sprintf("schema type array entry %d must be a string", i),
				SourceLocation: ptrNodeLoc(item),
			})
		}
	}
}

func isHTTPMethod(s string) bool {
	switch strings.ToLower(s) {
	case "get", "put", "post", "delete", "options", "head", "patch", "trace":
		return true
	}
	return false
}

// knownKeys defines allowed object keys per OpenAPI version. The sets include
// both common OpenAPI fields and version-specific extensions recognized by this
// parser.
var knownKeys = map[Version]map[string]keySet{
	Version2_0: {
		"root": {
			name: "Swagger 2.0 root object",
			allowed: map[string]bool{
				"swagger": true, "info": true, "host": true, "basePath": true,
				"schemes": true, "consumes": true, "produces": true, "paths": true,
				"definitions": true, "parameters": true, "responses": true,
				"securityDefinitions": true, "security": true, "tags": true,
				"externalDocs": true,
			},
		},
		"info": {
			name: "info object",
			allowed: map[string]bool{
				"title": true, "description": true, "termsOfService": true,
				"contact": true, "license": true, "version": true,
			},
		},
		"contact": {
			name:    "contact object",
			allowed: map[string]bool{"name": true, "url": true, "email": true},
		},
		"license": {
			name:    "license object",
			allowed: map[string]bool{"name": true, "url": true},
		},
		"server": {
			name:    "server object",
			allowed: map[string]bool{"url": true, "description": true, "variables": true},
		},
		"serverVariable": {
			name:    "server variable object",
			allowed: map[string]bool{"enum": true, "default": true, "description": true},
		},
		"pathItem": {
			name: "path item object",
			allowed: map[string]bool{
				"$ref": true, "get": true, "put": true, "post": true, "delete": true,
				"options": true, "head": true, "patch": true, "parameters": true,
			},
		},
		"operation": {
			name: "operation object",
			allowed: map[string]bool{
				"tags": true, "summary": true, "description": true,
				"externalDocs": true, "operationId": true, "consumes": true,
				"produces": true, "parameters": true, "responses": true,
				"schemes": true, "deprecated": true, "security": true,
			},
		},
		"parameter": {
			name: "parameter object",
			allowed: map[string]bool{
				"name": true, "in": true, "description": true, "required": true,
				"schema": true, "type": true, "format": true, "items": true,
				"collectionFormat": true, "default": true, "maximum": true,
				"exclusiveMaximum": true, "minimum": true, "exclusiveMinimum": true,
				"maxLength": true, "minLength": true, "pattern": true, "maxItems": true,
				"minItems": true, "uniqueItems": true, "enum": true, "multipleOf": true,
				"allowEmptyValue": true, "$ref": true,
			},
		},
		"requestBody": {
			name:    "request body object",
			allowed: map[string]bool{"description": true, "required": true, "content": true, "$ref": true},
		},
		"mediaType": {
			name:    "media type object",
			allowed: map[string]bool{"schema": true, "example": true, "examples": true, "encoding": true},
		},
		"encoding": {
			name: "encoding object",
			allowed: map[string]bool{
				"contentType": true, "headers": true, "style": true, "explode": true,
				"allowReserved": true,
			},
		},
		"response": {
			name: "response object",
			allowed: map[string]bool{
				"description": true, "schema": true, "headers": true, "examples": true,
				"$ref": true,
			},
		},
		"header": {
			name: "header object",
			allowed: map[string]bool{
				"description": true, "type": true, "format": true, "items": true,
				"collectionFormat": true, "default": true, "maximum": true,
				"exclusiveMaximum": true, "minimum": true, "exclusiveMinimum": true,
				"maxLength": true, "minLength": true, "pattern": true, "maxItems": true,
				"minItems": true, "uniqueItems": true, "enum": true, "multipleOf": true,
			},
		},
		"schema": {
			name: "schema object",
			allowed: map[string]bool{
				"$ref": true, "type": true, "format": true, "title": true,
				"description": true, "default": true, "multipleOf": true,
				"maximum": true, "exclusiveMaximum": true, "minimum": true,
				"exclusiveMinimum": true, "maxLength": true, "minLength": true,
				"pattern": true, "maxItems": true, "minItems": true, "uniqueItems": true,
				"maxProperties": true, "minProperties": true, "required": true,
				"enum": true, "allOf": true, "oneOf": true, "anyOf": true, "not": true,
				"items": true, "properties": true, "additionalProperties": true,
				"discriminator": true, "xml": true, "externalDocs": true, "example": true,
				"x-nullable": true, "readOnly": true, "writeOnly": true, "deprecated": true,
				"const": true, "contentMediaType": true, "contentEncoding": true,
			},
		},
		"example": {
			name:    "example object",
			allowed: map[string]bool{"summary": true, "description": true, "value": true, "externalValue": true, "$ref": true},
		},
		"link": {
			name: "link object",
			allowed: map[string]bool{
				"operationId": true, "operationRef": true, "parameters": true,
				"requestBody": true, "description": true, "server": true, "$ref": true,
			},
		},
		"securityScheme": {
			name: "security scheme object",
			allowed: map[string]bool{
				"type": true, "description": true, "name": true, "in": true,
				"flow": true, "authorizationUrl": true, "tokenUrl": true,
				"refreshUrl": true, "scopes": true,
			},
		},
		"tag": {
			name:    "tag object",
			allowed: map[string]bool{"name": true, "description": true, "externalDocs": true},
		},
		"externalDocs": {
			name:    "external documentation object",
			allowed: map[string]bool{"description": true, "url": true},
		},
		"xml": {
			name:    "XML object",
			allowed: map[string]bool{"name": true, "namespace": true, "prefix": true, "attribute": true, "wrapped": true},
		},
		"discriminator": {
			name:    "discriminator object",
			allowed: map[string]bool{"propertyName": true, "mapping": true},
		},
		"securityRequirement": {
			name:    "security requirement object",
			allowed: map[string]bool{}, // keys are security scheme names
		},
	},
	Version3_0: {
		"root": {
			name: "OpenAPI 3.0 root object",
			allowed: map[string]bool{
				"openapi": true, "info": true, "servers": true, "paths": true,
				"components": true, "security": true, "tags": true,
				"externalDocs": true,
				// webhooks is deliberately omitted: ConvertV30 warns that the
				// field is not defined in OpenAPI 3.0.x (see v30.go), so the
				// keyword validator emits a matching "Unsupported keyword"
				// warning here rather than silently accepting it. The two
				// validators now agree that webhooks is non-standard in 3.0
				// (L-89: previously Validate accepted it while ConvertV30
				// warned).
			},
		},
		"info": {
			name: "info object",
			allowed: map[string]bool{
				"title": true, "description": true, "termsOfService": true,
				"contact": true, "license": true, "version": true,
			},
		},
		"contact": {
			name:    "contact object",
			allowed: map[string]bool{"name": true, "url": true, "email": true},
		},
		"license": {
			name:    "license object",
			allowed: map[string]bool{"name": true, "url": true, "identifier": true},
		},
		"server": {
			name:    "server object",
			allowed: map[string]bool{"url": true, "description": true, "variables": true},
		},
		"serverVariable": {
			name:    "server variable object",
			allowed: map[string]bool{"enum": true, "default": true, "description": true},
		},
		"pathItem": {
			name: "path item object",
			allowed: map[string]bool{
				"$ref": true, "summary": true, "description": true, "get": true,
				"put": true, "post": true, "delete": true, "options": true,
				"head": true, "patch": true, "trace": true, "servers": true,
				"parameters": true,
			},
		},
		"operation": {
			name: "operation object",
			allowed: map[string]bool{
				"tags": true, "summary": true, "description": true, "externalDocs": true,
				"operationId": true, "parameters": true, "requestBody": true,
				"responses": true, "callbacks": true, "deprecated": true,
				"security": true, "servers": true,
			},
		},
		"parameter": {
			name: "parameter object",
			allowed: map[string]bool{
				"$ref": true, "name": true, "in": true, "description": true,
				"required": true, "deprecated": true, "allowEmptyValue": true,
				"style": true, "explode": true, "allowReserved": true, "schema": true,
				"content": true, "example": true, "examples": true,
			},
		},
		"header": {
			name: "header object",
			allowed: map[string]bool{
				"$ref": true, "description": true, "required": true, "deprecated": true,
				"allowEmptyValue": true, "style": true, "explode": true,
				"allowReserved": true, "schema": true, "content": true, "example": true,
				"examples": true,
			},
		},
		"requestBody": {
			name:    "request body object",
			allowed: map[string]bool{"$ref": true, "description": true, "content": true, "required": true},
		},
		"mediaType": {
			name:    "media type object",
			allowed: map[string]bool{"schema": true, "example": true, "examples": true, "encoding": true},
		},
		"encoding": {
			name: "encoding object",
			allowed: map[string]bool{
				"contentType": true, "headers": true, "style": true, "explode": true,
				"allowReserved": true,
			},
		},
		"response": {
			name: "response object",
			allowed: map[string]bool{
				"$ref": true, "description": true, "headers": true, "content": true,
				"links": true,
			},
		},
		"schema": {
			name: "schema object",
			allowed: map[string]bool{
				"$ref": true, "type": true, "format": true, "title": true,
				"description": true, "default": true, "multipleOf": true,
				"maximum": true, "exclusiveMaximum": true, "minimum": true,
				"exclusiveMinimum": true, "maxLength": true, "minLength": true,
				"pattern": true, "maxItems": true, "minItems": true, "uniqueItems": true,
				"maxProperties": true, "minProperties": true, "required": true,
				"enum": true, "allOf": true, "oneOf": true, "anyOf": true, "not": true,
				"items": true, "properties": true, "additionalProperties": true,
				"discriminator": true, "xml": true, "externalDocs": true, "example": true,
				"nullable": true, "readOnly": true, "writeOnly": true, "deprecated": true,
			},
		},
		"components": {
			name: "components object",
			allowed: map[string]bool{
				"schemas": true, "responses": true, "parameters": true, "examples": true,
				"requestBodies": true, "headers": true, "securitySchemes": true,
				"links": true, "callbacks": true,
			},
		},
		"example": {
			name:    "example object",
			allowed: map[string]bool{"$ref": true, "summary": true, "description": true, "value": true, "externalValue": true},
		},
		"link": {
			name: "link object",
			allowed: map[string]bool{
				"$ref": true, "operationId": true, "operationRef": true, "parameters": true,
				"requestBody": true, "description": true, "server": true,
			},
		},
		"securityScheme": {
			name: "security scheme object",
			allowed: map[string]bool{
				"$ref": true, "type": true, "description": true, "name": true, "in": true,
				"scheme": true, "bearerFormat": true, "flows": true, "openIdConnectUrl": true,
			},
		},
		"oauthFlows": {
			name:    "OAuth flows object",
			allowed: map[string]bool{"implicit": true, "password": true, "clientCredentials": true, "authorizationCode": true},
		},
		"oauthFlow": {
			name:    "OAuth flow object",
			allowed: map[string]bool{"authorizationUrl": true, "tokenUrl": true, "refreshUrl": true, "scopes": true},
		},
		"tag": {
			name:    "tag object",
			allowed: map[string]bool{"name": true, "description": true, "externalDocs": true},
		},
		"externalDocs": {
			name:    "external documentation object",
			allowed: map[string]bool{"description": true, "url": true},
		},
		"xml": {
			name:    "XML object",
			allowed: map[string]bool{"name": true, "namespace": true, "prefix": true, "attribute": true, "wrapped": true},
		},
		"discriminator": {
			name:    "discriminator object",
			allowed: map[string]bool{"propertyName": true, "mapping": true},
		},
		"securityRequirement": {
			name:    "security requirement object",
			allowed: map[string]bool{}, // keys are security scheme names
		},
	},
	Version3_1: {
		"root": {
			name: "OpenAPI 3.1 root object",
			allowed: map[string]bool{
				"openapi": true, "info": true, "servers": true, "paths": true,
				"webhooks": true, "components": true, "security": true, "tags": true,
				"externalDocs": true, "jsonSchemaDialect": true,
			},
		},
		"info": {
			name: "info object",
			allowed: map[string]bool{
				"title": true, "description": true, "termsOfService": true,
				"contact": true, "license": true, "summary": true, "version": true,
			},
		},
		"contact": {
			name:    "contact object",
			allowed: map[string]bool{"name": true, "url": true, "email": true},
		},
		"license": {
			name:    "license object",
			allowed: map[string]bool{"name": true, "identifier": true, "url": true},
		},
		"server": {
			name:    "server object",
			allowed: map[string]bool{"url": true, "description": true, "variables": true},
		},
		"serverVariable": {
			name:    "server variable object",
			allowed: map[string]bool{"enum": true, "default": true, "description": true},
		},
		"pathItem": {
			name: "path item object",
			allowed: map[string]bool{
				"$ref": true, "summary": true, "description": true, "get": true,
				"put": true, "post": true, "delete": true, "options": true,
				"head": true, "patch": true, "trace": true, "servers": true,
				"parameters": true,
			},
		},
		"operation": {
			name: "operation object",
			allowed: map[string]bool{
				"tags": true, "summary": true, "description": true, "externalDocs": true,
				"operationId": true, "parameters": true, "requestBody": true,
				"responses": true, "callbacks": true, "deprecated": true,
				"security": true, "servers": true,
			},
		},
		"parameter": {
			name: "parameter object",
			allowed: map[string]bool{
				"$ref": true, "name": true, "in": true, "description": true,
				"required": true, "deprecated": true, "allowEmptyValue": true,
				"style": true, "explode": true, "allowReserved": true, "schema": true,
				"content": true, "example": true, "examples": true,
			},
		},
		"header": {
			name: "header object",
			allowed: map[string]bool{
				"$ref": true, "description": true, "required": true, "deprecated": true,
				"allowEmptyValue": true, "style": true, "explode": true,
				"allowReserved": true, "schema": true, "content": true, "example": true,
				"examples": true,
			},
		},
		"requestBody": {
			name:    "request body object",
			allowed: map[string]bool{"$ref": true, "description": true, "content": true, "required": true},
		},
		"mediaType": {
			name:    "media type object",
			allowed: map[string]bool{"schema": true, "example": true, "examples": true, "encoding": true},
		},
		"encoding": {
			name: "encoding object",
			allowed: map[string]bool{
				"contentType": true, "headers": true, "style": true, "explode": true,
				"allowReserved": true,
			},
		},
		"response": {
			name: "response object",
			allowed: map[string]bool{
				"$ref": true, "description": true, "headers": true, "content": true,
				"links": true,
			},
		},
		"schema": {
			name: "schema object",
			allowed: map[string]bool{
				"$ref": true, "type": true, "format": true, "title": true,
				"description": true, "default": true, "multipleOf": true,
				"maximum": true, "exclusiveMaximum": true, "minimum": true,
				"exclusiveMinimum": true, "maxLength": true, "minLength": true,
				"pattern": true, "maxItems": true, "minItems": true, "uniqueItems": true,
				"maxProperties": true, "minProperties": true, "required": true,
				"enum": true, "allOf": true, "oneOf": true, "anyOf": true, "not": true,
				"items": true, "properties": true, "patternProperties": true,
				"additionalProperties": true, "propertyNames": true, "contains": true,
				"minContains": true, "maxContains": true, "discriminator": true,
				"xml": true, "externalDocs": true, "example": true, "examples": true,
				"nullable": true, "readOnly": true, "writeOnly": true, "deprecated": true,
				"const": true, "contentMediaType": true, "contentEncoding": true,
				"unevaluatedProperties": true, "prefixItems": true,
				// JSON Schema 2020-12 (OpenAPI 3.1) conditional and dependency
				// keywords previously omitted (H-17): if/then/else,
				// dependentSchemas, dependentRequired, unevaluatedItems, and the
				// contentSchema guard for contentMediaType/contentEncoding.
				"if": true, "then": true, "else": true,
				"dependentSchemas": true, "dependentRequired": true,
				"unevaluatedItems": true, "contentSchema": true,
			},
		},
		"components": {
			name: "components object",
			allowed: map[string]bool{
				"schemas": true, "responses": true, "parameters": true, "examples": true,
				"requestBodies": true, "headers": true, "securitySchemes": true,
				"links": true, "callbacks": true,
			},
		},
		"example": {
			name:    "example object",
			allowed: map[string]bool{"$ref": true, "summary": true, "description": true, "value": true, "externalValue": true},
		},
		"link": {
			name: "link object",
			allowed: map[string]bool{
				"$ref": true, "operationId": true, "operationRef": true, "parameters": true,
				"requestBody": true, "description": true, "server": true,
			},
		},
		"securityScheme": {
			name: "security scheme object",
			allowed: map[string]bool{
				"$ref": true, "type": true, "description": true, "name": true, "in": true,
				"scheme": true, "bearerFormat": true, "flows": true, "openIdConnectUrl": true,
			},
		},
		"oauthFlows": {
			name:    "OAuth flows object",
			allowed: map[string]bool{"implicit": true, "password": true, "clientCredentials": true, "authorizationCode": true},
		},
		"oauthFlow": {
			name:    "OAuth flow object",
			allowed: map[string]bool{"authorizationUrl": true, "tokenUrl": true, "refreshUrl": true, "scopes": true},
		},
		"tag": {
			name:    "tag object",
			allowed: map[string]bool{"name": true, "description": true, "externalDocs": true},
		},
		"externalDocs": {
			name:    "external documentation object",
			allowed: map[string]bool{"description": true, "url": true},
		},
		"xml": {
			name:    "XML object",
			allowed: map[string]bool{"name": true, "namespace": true, "prefix": true, "attribute": true, "wrapped": true},
		},
		"discriminator": {
			name:    "discriminator object",
			allowed: map[string]bool{"propertyName": true, "mapping": true},
		},
		"securityRequirement": {
			name:    "security requirement object",
			allowed: map[string]bool{}, // keys are security scheme names
		},
	},
}
