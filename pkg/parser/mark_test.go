package parser

import (
	"testing"
)

const petRef = "#/components/schemas/Pet"

// TestMarkCircularSchemaRefs_AllWalkers drives the markCircularSchemaRefs
// walkers across components responses/request bodies/headers/callbacks and a
// path item carrying every HTTP verb, asserting each reachable circular schema
// is marked Opaque. This covers markComponents, markResponse, markMediaType,
// markHeader, markRequestBody, markCallback, markPathItem, and markOperation.
func TestMarkCircularSchemaRefs_AllWalkers(t *testing.T) {
	mediaWithPet := func() *MediaType { return &MediaType{Schema: &Schema{Ref: petRef}} }

	spec := &Spec{
		Components: &Components{
			Responses: map[string]*Response{
				"R": {
					Content: map[string]*MediaType{"application/json": mediaWithPet()},
					Headers: map[string]*Header{"X-Rate": {Schema: &Schema{Ref: petRef}}},
				},
			},
			RequestBodies: map[string]*RequestBody{
				"RB": {Content: map[string]*MediaType{"application/json": mediaWithPet()}},
				// Media type whose Encoding carries header schemas
				// (markMediaType's encoding-headers loop) plus a nil encoding.
				"RBEnc": {
					Content: map[string]*MediaType{
						"multipart/form-data": {
							Encoding: map[string]*Encoding{
								"field": {Headers: map[string]*Header{"X-Enc": {Schema: &Schema{Ref: petRef}}}},
								"nil":   nil,
							},
						},
					},
				},
			},
			// Component headers: a real circular ref and a nil entry.
			Headers: map[string]*Header{
				"X-API": {Schema: &Schema{Ref: petRef}},
				"X-Nil": nil,
			},
			Parameters: map[string]*Parameter{
				"P": {Schema: &Schema{Ref: petRef}},
			},
			Callbacks: map[string]Callback{
				"cb": {
					"{$request.body#/url}": {
						Post: &Operation{
							Parameters: []Parameter{{Schema: &Schema{Ref: petRef}}},
							RequestBody: &RequestBody{Content: map[string]*MediaType{
								"application/json": mediaWithPet(),
							}},
							Responses: map[string]*Response{
								"200": {
									Content: map[string]*MediaType{"application/json": mediaWithPet()},
									Headers: map[string]*Header{"X-CB": {Schema: &Schema{Ref: petRef}}},
								},
							},
						},
					},
				},
			},
		},
		Paths: map[string]*PathItem{
			"/pets": {
				Parameters: []Parameter{{Schema: &Schema{Ref: petRef}}},
				Get: &Operation{
					Parameters:  []Parameter{{Schema: &Schema{Ref: petRef}}},
					RequestBody: &RequestBody{Content: map[string]*MediaType{"application/json": mediaWithPet()}},
					Responses: map[string]*Response{
						"200": {
							Content: map[string]*MediaType{"application/json": mediaWithPet()},
							Headers: map[string]*Header{"X-Get": {Schema: &Schema{Ref: petRef}}},
						},
					},
					Callbacks: map[string]Callback{
						"opcb": {"{$request.body#/url}": {Post: &Operation{RequestBody: &RequestBody{Content: map[string]*MediaType{"application/json": mediaWithPet()}}}}},
					},
				},
			},
		},
	}

	// Exercise the remaining HTTP verbs on a second path item so every
	// markOperation branch runs.
	spec.Paths["/verbs"] = &PathItem{
		Put:     &Operation{Responses: map[string]*Response{"204": {Content: map[string]*MediaType{"application/json": mediaWithPet()}}}},
		Post:    &Operation{RequestBody: &RequestBody{Content: map[string]*MediaType{"application/json": mediaWithPet()}}},
		Delete:  &Operation{Responses: map[string]*Response{"204": {Content: map[string]*MediaType{"application/json": mediaWithPet()}}}},
		Options: &Operation{Responses: map[string]*Response{"204": {Content: map[string]*MediaType{"application/json": mediaWithPet()}}}},
		Head:    &Operation{Responses: map[string]*Response{"204": {Content: map[string]*MediaType{"application/json": mediaWithPet()}}}},
		Patch:   &Operation{RequestBody: &RequestBody{Content: map[string]*MediaType{"application/json": mediaWithPet()}}},
		Trace:   &Operation{Responses: map[string]*Response{"204": {Content: map[string]*MediaType{"application/json": mediaWithPet()}}}},
	}
	spec.Webhooks = map[string]*PathItem{
		"wh": {Post: &Operation{RequestBody: &RequestBody{Content: map[string]*MediaType{"application/json": mediaWithPet()}}}},
	}

	markCircularSchemaRefs(spec, []string{petRef})

	// Collect every schema in the tree and assert all with Ref == petRef are Opaque.
	opaqueCount := 0
	visitSchemas := func(s *Schema) {
		if s == nil {
			return
		}
		if s.Ref == petRef && !s.Opaque {
			t.Errorf("expected schema %q to be marked Opaque", petRef)
		}
		if s.Opaque {
			opaqueCount++
		}
	}
	// Component-level.
	for _, r := range spec.Components.Responses {
		for _, mt := range r.Content {
			visitSchemas(mt.Schema)
		}
		for _, h := range r.Headers {
			visitSchemas(h.Schema)
		}
	}
	for _, rb := range spec.Components.RequestBodies {
		for _, mt := range rb.Content {
			visitSchemas(mt.Schema)
			for _, enc := range mt.Encoding {
				if enc == nil {
					continue
				}
				for _, hdr := range enc.Headers {
					visitSchemas(hdr.Schema)
				}
			}
		}
	}
	for _, h := range spec.Components.Headers {
		if h == nil {
			continue
		}
		visitSchemas(h.Schema)
	}
	for _, p := range spec.Components.Parameters {
		visitSchemas(p.Schema)
	}
	for _, cb := range spec.Components.Callbacks {
		for _, item := range cb {
			if item.Post != nil {
				for _, mt := range item.Post.RequestBody.Content {
					visitSchemas(mt.Schema)
				}
			}
		}
	}
	for _, item := range spec.Paths {
		for _, op := range []*Operation{item.Get, item.Put, item.Post, item.Delete, item.Options, item.Head, item.Patch, item.Trace} {
			if op == nil {
				continue
			}
			if op.RequestBody != nil {
				for _, mt := range op.RequestBody.Content {
					visitSchemas(mt.Schema)
				}
			}
		}
	}
	if opaqueCount == 0 {
		t.Error("expected at least one schema to be marked Opaque")
	}
}

// TestMarkCircularSchemaRefs_NilSpec guards the nil-spec early return.
func TestMarkCircularSchemaRefs_NilSpec(t *testing.T) {
	// Must not panic.
	markCircularSchemaRefs(nil, []string{petRef})
}

// TestMarkSchema_AllEdges exercises every recursive schema edge in markSchema:
// allOf/oneOf/anyOf/prefixItems, properties/patternProperties, items(*Schema),
// not/contains/propertyNames/unevaluatedProperties, conditional if/then/else,
// unevaluatedItems, and dependentSchemas.
func TestMarkSchema_AllEdges(t *testing.T) {
	circular := map[string]struct{}{petRef: {}}
	ref := &Schema{Ref: petRef}

	root := &Schema{
		AllOf:                 []*Schema{ref},
		OneOf:                 []*Schema{ref},
		AnyOf:                 []*Schema{ref},
		PrefixItems:           []*Schema{ref},
		Properties:            map[string]*Schema{"p": ref},
		PatternProperties:     map[string]*Schema{"^x-": ref},
		Items:                 ref,
		Not:                   ref,
		Contains:              ref,
		PropertyNames:         ref,
		UnevaluatedProperties: ref,
		UnevaluatedItems:      ref,
		If:                    ref,
		Then:                  ref,
		Else:                  ref,
		DependentSchemas:      map[string]*Schema{"dep": ref},
	}
	markSchema(root, circular)

	// Every edge schema must have been marked Opaque via its Ref.
	opaque := []*Schema{
		root.AllOf[0], root.OneOf[0], root.AnyOf[0], root.PrefixItems[0],
		root.Properties["p"], root.PatternProperties["^x-"],
		root.Items.(*Schema), root.Not, root.Contains, root.PropertyNames,
		root.UnevaluatedProperties, root.UnevaluatedItems, root.If, root.Then,
		root.Else, root.DependentSchemas["dep"],
	}
	for i, s := range opaque {
		if !s.Opaque {
			t.Errorf("edge schema %d should be Opaque", i)
		}
	}
}

// TestMarkSchemaAdditionalProperties covers the three shapes of the
// additionalProperties field: a *Schema (recursed and marked), a string ref
// present in the circular set (parent marked Opaque), and nil.
func TestMarkSchemaAdditionalProperties(t *testing.T) {
	circular := map[string]struct{}{petRef: {}}

	// *Schema form recurses into the schema and marks it.
	schemaForm := &Schema{AdditionalProperties: &Schema{Ref: petRef}}
	markSchema(schemaForm, circular)
	if ap := schemaForm.AdditionalProperties.(*Schema); !ap.Opaque {
		t.Error("additionalProperties *Schema form should be marked Opaque")
	}
	if schemaForm.Opaque {
		t.Error("parent schema must not be marked Opaque for the *Schema form")
	}

	// String-ref form present in the circular set marks the parent.
	stringForm := &Schema{AdditionalProperties: petRef}
	markSchema(stringForm, circular)
	if !stringForm.Opaque {
		t.Error("parent schema should be Opaque when additionalProperties is a circular string ref")
	}

	// String ref NOT in the circular set leaves the parent unmarked.
	notCircular := &Schema{AdditionalProperties: "#/components/schemas/Other"}
	markSchema(notCircular, map[string]struct{}{})
	if notCircular.Opaque {
		t.Error("parent schema must not be Opaque for a non-circular string ref")
	}

	// Nil additionalProperties is a no-op.
	markSchema(&Schema{}, circular)
}

// TestCanReach_Nil guards the nil start/goal early returns.
func TestCanReach_Nil(t *testing.T) {
	if reached, diags := canReach(nil, nil, nil); reached || len(diags) != 0 {
		t.Errorf("canReach(nil,nil,nil) = (%v,%v), want (false,nil)", reached, diags)
	}
	start := &ScalarNode{Value: "x"}
	if reached, _ := canReach(nil, start, nil); reached {
		t.Error("canReach with nil goal should be false")
	}
}

// TestDepthExceededDiag_KeepsFirst asserts the first depth-exceeded diagnostic
// is kept when subsequent branches also exceed the limit.
func TestDepthExceededDiag_KeepsFirst(t *testing.T) {
	first := depthExceededDiag(nil, &ScalarNode{Value: "a"})
	if first == nil {
		t.Fatal("expected a diagnostic from the fresh path")
	}
	second := depthExceededDiag(first, &ScalarNode{Value: "b"})
	if second != first {
		t.Error("expected the original diagnostic to be preserved")
	}
}
