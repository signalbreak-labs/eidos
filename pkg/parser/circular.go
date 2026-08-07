package parser

import (
	"fmt"
	"strings"
)

// DetectCircularSchemaRefs scans the raw AST for local schema $ref cycles.
// It returns the set of $ref strings that participate in a cycle and any
// warning diagnostics produced. Only local JSON Pointer references to
// #/components/schemas/* (OpenAPI 3.x) and #/definitions/* (Swagger 2.0)
// are considered. Non-schema component refs (parameters, responses, etc.) are
// intentionally out of scope here; broader cycle detection should be added
// alongside the relevant component walkers if required.
type circularDetector struct {
	root     Node
	circular map[string]struct{}
	diags    []Diagnostic
	seen     map[Node]bool
}

func (d *circularDetector) detect(n Node) {
	d.detectWithKey(n, "")
}

// detectWithKey walks the raw AST looking for local schema $ref cycles.
// parentKey is the mapping key (or sequence's enclosing mapping key) that
// holds the current node; it decides whether a bare scalar whose text is a
// local schema pointer is a genuine $ref or literal data (M-24).
//
// A $ref is normally a mapping entry whose key is "$ref". Some legacy/Swagger
// specs instead use a bare schema-pointer string where a schema object is
// expected (e.g. `additionalProperties: '#/definitions/Node'`), so a bare
// scalar is treated as a ref only when its parent key is a schema-bearing
// position. Literal-data fields (example, description, enum, etc.) hold
// arbitrary strings that may textually equal a pointer without being a ref;
// those are not treated as refs, which removes the spurious "Circular schema
// reference" warnings the former blanket-scalar branch produced.
func (d *circularDetector) detectWithKey(n Node, parentKey string) {
	if n == nil || d.seen[n] {
		return
	}
	d.seen[n] = true

	switch v := n.(type) {
	case *MapNode:
		for _, e := range v.Entries {
			if e.Key == nil {
				continue
			}
			key, _ := asString(e.Key)
			if key == "$ref" {
				d.checkRef(e.Value, v)
				continue
			}
			if scalar, ok := e.Value.(*ScalarNode); ok && d.isBareRef(scalar, key) {
				d.checkRef(e.Value, v)
				continue
			}
			d.detectWithKey(e.Value, key)
		}
	case *SequenceNode:
		for _, item := range v.Items {
			if scalar, ok := item.(*ScalarNode); ok && d.isBareRef(scalar, parentKey) {
				d.checkRef(item, v)
				continue
			}
			d.detectWithKey(item, parentKey)
		}
	}
}

// isBareRef reports whether scalar holds a local schema pointer that should be
// treated as a $ref given its enclosing key. parentKey is the mapping key that
// directly holds the scalar, or the sequence's enclosing mapping key for
// sequence items.
func (d *circularDetector) isBareRef(scalar *ScalarNode, parentKey string) bool {
	ref, ok := asString(scalar)
	if !ok || !isLocalSchemaRef(ref) {
		return false
	}
	return !literalDataKey(parentKey)
}

// literalSchemaScalarKeys are mapping keys whose scalar values are literal
// data rather than schema $refs. A string under one of these keys that
// textually equals a local schema pointer (e.g. an example or description
// value) must not be reported as a circular reference (M-24).
var literalSchemaScalarKeys = map[string]bool{
	"example":          true, // Schema.example: literal example value
	"default":          true, // Schema.default: literal default value
	"enum":             true, // Schema.enum: literal value set
	"const":            true, // Schema.const: literal value
	"description":      true,
	"title":            true,
	"$comment":         true,
	"pattern":          true, // regex literal
	"format":           true,
	"contentEncoding":  true,
	"contentMediaType": true,
	"value":            true, // Example.value: literal value
	"required":         true, // array of property names, not refs
	"tags":             true, // array of tag names, not refs
	"deprecated":       true,
	"readOnly":         true,
	"writeOnly":        true,
}

func literalDataKey(key string) bool {
	return literalSchemaScalarKeys[key]
}

func (d *circularDetector) checkRef(value, goal Node) {
	ref, ok := asString(value)
	if !ok || !isLocalSchemaRef(ref) {
		return
	}
	// A ref already known to be circular does not need to be re-walked: once a
	// ref string is in the circular set, every occurrence of it is circular.
	// Performing this check before the O(M) canReach walk (rather than after,
	// as it previously did) avoids the quadratic N×M cost on specs that repeat
	// the same circular ref many times (M-25).
	if _, found := d.circular[ref]; found {
		return
	}
	target, _ := ResolveLocalRef(d.root, ref, nodeLoc(value))
	if target == nil {
		return
	}
	reaches, reachDiags := canReach(d.root, target, goal)
	d.diags = append(d.diags, reachDiags...)
	if !reaches {
		return
	}
	d.circular[ref] = struct{}{}
	loc := nodeLoc(goal)
	d.diags = append(d.diags, Diagnostic{
		Severity:       SeverityWarning,
		Summary:        "Circular schema reference",
		Detail:         fmt.Sprintf("Schema $ref %q resolves back to itself or an ancestor schema, forming a cycle.", ref),
		SourceLocation: &loc,
	})
}

// DetectCircularSchemaRefs scans the raw AST for local schema $ref cycles.
func DetectCircularSchemaRefs(root Node) ([]string, []Diagnostic) {
	d := &circularDetector{
		root:     root,
		circular: make(map[string]struct{}),
		seen:     make(map[Node]bool),
	}
	d.detect(root)

	refs := make([]string, 0, len(d.circular))
	for ref := range d.circular {
		refs = append(refs, ref)
	}
	return refs, d.diags
}

// isLocalSchemaRef reports whether ref points to a local schema definition.
func isLocalSchemaRef(ref string) bool {
	return strings.HasPrefix(ref, "#") &&
		(strings.HasPrefix(ref, "#/components/schemas/") ||
			strings.HasPrefix(ref, "#/definitions/"))
}

// maxReachDepth is the recursion limit for canReach. OpenAPI specs are
// normally shallow; this bound protects against malformed or pathological
// documents that could otherwise overflow the stack.
var maxReachDepth = 1000

// canReach reports whether start can reach goal through nested AST nodes and
// local $ref edges. It is used to close a cycle: a schema $ref is circular
// when the referenced schema can find its way back to the field that holds the
// reference. If the recursion limit is exceeded on a branch, that branch is
// truncated and a warning diagnostic is recorded; exploration of sibling
// branches continues. The warning is only returned when no path to the goal is
// found.
type reachState struct {
	root      Node
	goal      Node
	seen      map[Node]bool
	depthDiag *Diagnostic
}

func (rs *reachState) walk(n Node, depth int) bool {
	if n == nil || rs.seen[n] {
		return false
	}
	if depth > maxReachDepth {
		rs.depthDiag = depthExceededDiag(rs.depthDiag, n)
		return false
	}
	if n == rs.goal {
		return true
	}
	rs.seen[n] = true
	return rs.walkChildren(n, depth)
}

func (rs *reachState) walkChildren(n Node, depth int) bool {
	switch v := n.(type) {
	case *MapNode:
		for _, e := range v.Entries {
			if e.Key == nil {
				continue
			}
			key, _ := asString(e.Key)
			if key == "$ref" {
				ref, ok := asString(e.Value)
				if !ok || !strings.HasPrefix(ref, "#") {
					continue
				}
				target, _ := ResolveLocalRef(rs.root, ref, nodeLoc(e.Value))
				if target != nil && rs.walk(target, depth+1) {
					return true
				}
				continue
			}
			if rs.walk(e.Value, depth+1) {
				return true
			}
		}
	case *SequenceNode:
		for _, item := range v.Items {
			if rs.walk(item, depth+1) {
				return true
			}
		}
	}
	return false
}

func depthExceededDiag(current *Diagnostic, n Node) *Diagnostic {
	if current != nil {
		return current
	}
	loc := nodeLoc(n)
	return &Diagnostic{
		Severity:       SeverityWarning,
		Summary:        "Schema reference search depth exceeded",
		Detail:         fmt.Sprintf("Recursion depth exceeded %d while following schema references; cycle detection may be incomplete.", maxReachDepth),
		SourceLocation: &loc,
	}
}

func canReach(root, start, goal Node) (bool, []Diagnostic) {
	if start == nil || goal == nil {
		return false, nil
	}
	rs := &reachState{root: root, goal: goal, seen: make(map[Node]bool)}
	found := rs.walk(start, 0)
	if found {
		return true, nil
	}
	if rs.depthDiag != nil {
		return false, []Diagnostic{*rs.depthDiag}
	}
	return false, nil
}

// markCircularSchemaRefs marks every Schema in spec whose $ref is part of a
// cycle as Opaque.
func markCircularSchemaRefs(spec *Spec, refs []string) {
	if spec == nil || len(refs) == 0 {
		return
	}
	circular := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		circular[ref] = struct{}{}
	}
	markComponents(spec.Components, circular)
	for _, item := range spec.Paths {
		markPathItem(item, circular)
	}
	for _, item := range spec.Webhooks {
		markPathItem(item, circular)
	}
}

func markComponents(c *Components, circular map[string]struct{}) {
	if c == nil {
		return
	}
	for _, s := range c.Schemas {
		markSchema(s, circular)
	}
	for _, r := range c.Responses {
		markResponse(r, circular)
	}
	for _, p := range c.Parameters {
		markParameter(p, circular)
	}
	for _, rb := range c.RequestBodies {
		markRequestBody(rb, circular)
	}
	for _, h := range c.Headers {
		markHeader(h, circular)
	}
	for _, cb := range c.Callbacks {
		markCallback(cb, circular)
	}
}

func markPathItem(item *PathItem, circular map[string]struct{}) {
	if item == nil {
		return
	}
	for i := range item.Parameters {
		markParameter(&item.Parameters[i], circular)
	}
	markOperation(item.Get, circular)
	markOperation(item.Put, circular)
	markOperation(item.Post, circular)
	markOperation(item.Delete, circular)
	markOperation(item.Options, circular)
	markOperation(item.Head, circular)
	markOperation(item.Patch, circular)
	markOperation(item.Trace, circular)
}

func markOperation(op *Operation, circular map[string]struct{}) {
	if op == nil {
		return
	}
	for i := range op.Parameters {
		markParameter(&op.Parameters[i], circular)
	}
	markRequestBody(op.RequestBody, circular)
	for _, r := range op.Responses {
		markResponse(r, circular)
	}
	// Operation-level callbacks contain PathItems with their own request bodies
	// and responses whose schemas may participate in a cycle; without walking
	// them, circular refs reachable only through callbacks are detected but
	// never marked Opaque (M-26).
	for _, cb := range op.Callbacks {
		markCallback(cb, circular)
	}
}

func markParameter(p *Parameter, circular map[string]struct{}) {
	if p == nil {
		return
	}
	markSchema(p.Schema, circular)
}

func markRequestBody(rb *RequestBody, circular map[string]struct{}) {
	if rb == nil {
		return
	}
	for _, mt := range rb.Content {
		markMediaType(mt, circular)
	}
}

func markResponse(r *Response, circular map[string]struct{}) {
	if r == nil {
		return
	}
	for _, mt := range r.Content {
		markMediaType(mt, circular)
	}
	for _, h := range r.Headers {
		markHeader(h, circular)
	}
}

func markHeader(h *Header, circular map[string]struct{}) {
	if h == nil {
		return
	}
	markSchema(h.Schema, circular)
}

func markMediaType(mt *MediaType, circular map[string]struct{}) {
	if mt == nil {
		return
	}
	markSchema(mt.Schema, circular)
	// Encoding.Headers maps to Header objects whose Schema may participate in
	// a cycle; without walking them, circular refs reachable only through
	// encoding headers are detected but never marked Opaque (M-26).
	for _, h := range mt.Encoding {
		if h == nil {
			continue
		}
		for _, hdr := range h.Headers {
			markHeader(hdr, circular)
		}
	}
}

func markCallback(cb Callback, circular map[string]struct{}) {
	for _, item := range cb {
		markPathItem(item, circular)
	}
}

// markSchema marks schemas whose $ref participates in a cycle as Opaque. It
// walks the schema sub-tree that can contain nested schema $refs. Metadata
// fields such as examples, example values, externalDocs, and xml are not
// traversed because they do not contain schema $refs in practice.
func markSchema(s *Schema, circular map[string]struct{}) {
	if s == nil {
		return
	}
	if _, ok := circular[s.Ref]; ok {
		s.Opaque = true
	}
	markSchemaList(s.AllOf, circular)
	markSchemaList(s.OneOf, circular)
	markSchemaList(s.AnyOf, circular)
	markSchemaList(s.PrefixItems, circular)
	markSchemaMap(s.Properties, circular)
	markSchemaMap(s.PatternProperties, circular)
	if itemSchema, ok := s.Items.(*Schema); ok {
		markSchema(itemSchema, circular)
	}
	markSchema(s.Not, circular)
	markSchema(s.Contains, circular)
	markSchema(s.PropertyNames, circular)
	markSchema(s.UnevaluatedProperties, circular)
	// JSON Schema conditional and dependent-schema edges also carry schema
	// $refs that can close a cycle. Without walking If/Then/Else,
	// DependentSchemas, and UnevaluatedItems, circular refs reachable only
	// through those edges are detected but never marked Opaque, leaving
	// downstream recursion protection incomplete (M-26).
	markSchema(s.If, circular)
	markSchema(s.Then, circular)
	markSchema(s.Else, circular)
	markSchema(s.UnevaluatedItems, circular)
	markSchemaMap(s.DependentSchemas, circular)
	markSchemaAdditionalProperties(s, circular)
}

func markSchemaList(list []*Schema, circular map[string]struct{}) {
	for _, s := range list {
		markSchema(s, circular)
	}
}

func markSchemaMap(m map[string]*Schema, circular map[string]struct{}) {
	for _, s := range m {
		markSchema(s, circular)
	}
}

func markSchemaAdditionalProperties(s *Schema, circular map[string]struct{}) {
	if s.AdditionalProperties == nil {
		return
	}
	if ap, ok := s.AdditionalProperties.(*Schema); ok {
		markSchema(ap, circular)
		return
	}
	if ref, ok := s.AdditionalProperties.(string); ok {
		if _, found := circular[ref]; found {
			s.Opaque = true
		}
	}
}
