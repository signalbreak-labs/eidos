package transformer

import (
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/parser"
)

func TestOperationsFromSpec_ResponseHeadersAndExtensions(t *testing.T) {
	spec := &parser.Spec{
		Paths: map[string]*parser.PathItem{
			"/pets": {
				Get: &parser.Operation{
					OperationID: "listPets",
					Responses: map[string]*parser.Response{
						"200": {
							Headers: map[string]*parser.Header{
								"X-Total-Count": {},
								"Link":          {},
							},
							Content: map[string]*parser.MediaType{
								"application/json": {Schema: &parser.Schema{Type: "array"}},
							},
						},
					},
					Extensions: map[string]any{
						"x-pagination": "offset",
					},
				},
			},
		},
	}

	ops := OperationsFromSpec(spec)
	if ops == nil {
		t.Fatalf("OperationsFromSpec(spec) = nil, want non-nil")
	}
	got := ops["/pets"][MethodGet]

	wantHeaders := []string{"Link", "X-Total-Count"}
	if !reflect.DeepEqual(sortedStrings(got.ResponseHeaders), wantHeaders) {
		t.Errorf("ResponseHeaders = %v, want %v", got.ResponseHeaders, wantHeaders)
	}
	if got.ResponseBody != true {
		t.Errorf("ResponseBody = %v, want true", got.ResponseBody)
	}
	if got.ResponseSchema == nil || got.ResponseSchema.Type != "array" {
		t.Errorf("ResponseSchema.Type = %v, want array", got.ResponseSchema)
	}
	if style := DetectPaginationStyle(got); style != PaginationOffset {
		t.Errorf("DetectPaginationStyle = %v, want %v", style, PaginationOffset)
	}
}

func TestOperationsFromSpec_RequestAndParameters(t *testing.T) {
	spec := &parser.Spec{
		Paths: map[string]*parser.PathItem{
			"/pets/{petId}": {
				Parameters: []parser.Parameter{
					{
						Ref: "#/components/parameters/petId",
					},
				},
				Patch: &parser.Operation{
					OperationID: "updatePet",
					Parameters: []parser.Parameter{
						{
							Name:     "verbose",
							In:       "query",
							Required: false,
							Schema:   &parser.Schema{Type: "boolean"},
						},
					},
					RequestBody: &parser.RequestBody{
						Content: map[string]*parser.MediaType{
							"application/json": {Schema: &parser.Schema{Type: "object"}},
						},
					},
					Responses: map[string]*parser.Response{
						"200": {
							Content: map[string]*parser.MediaType{
								"application/json": {Schema: &parser.Schema{Type: "object"}},
							},
						},
					},
				},
			},
		},
		Components: &parser.Components{
			Parameters: map[string]*parser.Parameter{
				"petId": {
					Name:     "petId",
					In:       "path",
					Required: true,
					Schema:   &parser.Schema{Type: "string"},
				},
			},
		},
	}

	ops := OperationsFromSpec(spec)
	got := ops["/pets/{petId}"][MethodPatch]

	if got.OperationID != "updatePet" {
		t.Errorf("OperationID = %q, want updatePet", got.OperationID)
	}
	if !got.RequestBody {
		t.Errorf("RequestBody = %v, want true", got.RequestBody)
	}
	if got.RequestSchema == nil || got.RequestSchema.Type != "object" {
		t.Errorf("RequestSchema.Type = %v, want object", got.RequestSchema)
	}

	wantParams := []Parameter{
		{Name: "verbose", In: "query", Required: false, Type: "boolean"},
		{Name: "petId", In: "path", Required: true, Type: "string"},
	}
	if len(got.Parameters) != len(wantParams) {
		t.Fatalf("Parameters = %+v, want %+v", got.Parameters, wantParams)
	}
	for i, p := range got.Parameters {
		if !reflect.DeepEqual(p, wantParams[i]) {
			t.Errorf("Parameter[%d] = %+v, want %+v", i, p, wantParams[i])
		}
	}
}

func TestOperationsFromSpec_OperationParamOverridesPathParam(t *testing.T) {
	spec := &parser.Spec{
		Paths: map[string]*parser.PathItem{
			"/pets/{id}": {
				Parameters: []parser.Parameter{
					{Name: "id", In: "path", Required: false, Schema: &parser.Schema{Type: "integer"}},
				},
				Patch: &parser.Operation{
					OperationID: "updatePet",
					Parameters: []parser.Parameter{
						{Name: "id", In: "path", Required: true, Schema: &parser.Schema{Type: "string"}},
					},
				},
			},
		},
	}

	ops := OperationsFromSpec(spec)
	got := ops["/pets/{id}"][MethodPatch]
	if len(got.Parameters) != 1 {
		t.Fatalf("expected one parameter after override, got %+v", got.Parameters)
	}
	want := Parameter{Name: "id", In: "path", Required: true, Type: "string"}
	if !reflect.DeepEqual(got.Parameters[0], want) {
		t.Errorf("Parameter = %+v, want %+v", got.Parameters[0], want)
	}
}

func TestOperationsFromSpec_ResponseRefResolution(t *testing.T) {
	spec := &parser.Spec{
		Paths: map[string]*parser.PathItem{
			"/pets": {
				Get: &parser.Operation{
					Responses: map[string]*parser.Response{
						"200": {Ref: "#/components/responses/PetList"},
					},
				},
			},
		},
		Components: &parser.Components{
			Responses: map[string]*parser.Response{
				"PetList": {
					Headers: map[string]*parser.Header{
						"Link": {},
					},
				},
			},
		},
	}

	ops := OperationsFromSpec(spec)
	got := ops["/pets"][MethodGet]
	wantHeaders := []string{"Link"}
	if !reflect.DeepEqual(sortedStrings(got.ResponseHeaders), wantHeaders) {
		t.Errorf("ResponseHeaders = %v, want %v", got.ResponseHeaders, wantHeaders)
	}
}

func TestOperationsFromSpec_NilSpec(t *testing.T) {
	if got := OperationsFromSpec(nil); got != nil {
		t.Errorf("OperationsFromSpec(nil) = %v, want nil", got)
	}
}

func TestOperationsFromSpec_ParameterDedup(t *testing.T) {
	spec := &parser.Spec{
		Paths: map[string]*parser.PathItem{
			"/pets/{petId}": {
				Parameters: []parser.Parameter{
					{
						Name:     "petId",
						In:       "path",
						Required: true,
						Schema:   &parser.Schema{Type: "string"},
					},
				},
				Get: &parser.Operation{
					OperationID: "getPet",
					Parameters: []parser.Parameter{
						{
							Name:     "petId",
							In:       "path",
							Required: true,
							Schema:   &parser.Schema{Type: "string"},
						},
						{
							Name:     "verbose",
							In:       "query",
							Required: false,
							Schema:   &parser.Schema{Type: "boolean"},
						},
					},
					Responses: map[string]*parser.Response{
						"200": {Content: map[string]*parser.MediaType{
							"application/json": {Schema: &parser.Schema{Type: "object"}},
						}},
					},
				},
			},
		},
	}

	ops := OperationsFromSpec(spec)
	got := ops["/pets/{petId}"][MethodGet]

	if len(got.Parameters) != 2 {
		t.Fatalf("Parameters = %+v, want 2 entries", got.Parameters)
	}
	want := []Parameter{
		{Name: "petId", In: "path", Required: true, Type: "string"},
		{Name: "verbose", In: "query", Required: false, Type: "boolean"},
	}
	if !reflect.DeepEqual(got.Parameters, want) {
		t.Errorf("Parameters = %+v, want %+v", got.Parameters, want)
	}
}

func TestOperationsFromSpec_RequestBodyRefResolution(t *testing.T) {
	spec := &parser.Spec{
		Paths: map[string]*parser.PathItem{
			"/pets": {
				Post: &parser.Operation{
					OperationID: "createPet",
					RequestBody: &parser.RequestBody{Ref: "#/components/requestBodies/PetBody"},
					Responses: map[string]*parser.Response{
						"201": {Content: map[string]*parser.MediaType{
							"application/json": {Schema: &parser.Schema{Type: "object"}},
						}},
					},
				},
			},
		},
		Components: &parser.Components{
			RequestBodies: map[string]*parser.RequestBody{
				"PetBody": {
					Content: map[string]*parser.MediaType{
						"application/json": {Schema: &parser.Schema{Type: "object"}},
					},
				},
			},
		},
	}

	ops := OperationsFromSpec(spec)
	got := ops["/pets"][MethodPost]

	if !got.RequestBody {
		t.Errorf("RequestBody = %v, want true", got.RequestBody)
	}
	if got.RequestSchema == nil || got.RequestSchema.Type != "object" {
		t.Errorf("RequestSchema.Type = %v, want object", got.RequestSchema)
	}
}

func TestOperationsFromSpec_ParameterRefResolution(t *testing.T) {
	spec := &parser.Spec{
		Paths: map[string]*parser.PathItem{
			"/pets/{petId}": {
				Get: &parser.Operation{
					OperationID: "getPet",
					Parameters: []parser.Parameter{
						{Ref: "#/components/parameters/petId"},
					},
					Responses: map[string]*parser.Response{
						"200": {Content: map[string]*parser.MediaType{
							"application/json": {Schema: &parser.Schema{Type: "object"}},
						}},
					},
				},
			},
		},
		Components: &parser.Components{
			Parameters: map[string]*parser.Parameter{
				"petId": {
					Name:     "petId",
					In:       "path",
					Required: true,
					Schema:   &parser.Schema{Type: "integer"},
				},
			},
		},
	}

	ops := OperationsFromSpec(spec)
	got := ops["/pets/{petId}"][MethodGet]

	if len(got.Parameters) != 1 {
		t.Fatalf("Parameters = %+v, want 1 entry", got.Parameters)
	}
	want := Parameter{Name: "petId", In: "path", Required: true, Type: "integer"}
	if !reflect.DeepEqual(got.Parameters[0], want) {
		t.Errorf("Parameter = %+v, want %+v", got.Parameters[0], want)
	}
}

func TestOperationsFromSpec_RangeWildcardResponse(t *testing.T) {
	// OpenAPI 3 allows range response keys such as "2XX". A spec declaring only
	// "2XX" must still surface a success response schema/headers (L-96).
	spec := &parser.Spec{
		Paths: map[string]*parser.PathItem{
			"/pets": {
				Get: &parser.Operation{
					OperationID: "listPets",
					Responses: map[string]*parser.Response{
						"2XX": {
							Headers: map[string]*parser.Header{
								"Link": {},
							},
							Content: map[string]*parser.MediaType{
								"application/json": {Schema: &parser.Schema{Type: "array"}},
							},
						},
					},
				},
			},
		},
	}

	ops := OperationsFromSpec(spec)
	got := ops["/pets"][MethodGet]

	if got.ResponseSchema == nil || got.ResponseSchema.Type != "array" {
		t.Errorf("ResponseSchema.Type = %v, want array (2XX should be a success)", got.ResponseSchema)
	}
	wantHeaders := []string{"Link"}
	if !reflect.DeepEqual(sortedStrings(got.ResponseHeaders), wantHeaders) {
		t.Errorf("ResponseHeaders = %v, want %v", got.ResponseHeaders, wantHeaders)
	}

	// A specific 2xx code is preferred over the wildcard when both are present.
	spec.Paths["/pets"].Get.Responses["200"] = &parser.Response{
		Content: map[string]*parser.MediaType{
			"application/json": {Schema: &parser.Schema{Type: "object"}},
		},
	}
	got = OperationsFromSpec(spec)["/pets"][MethodGet]
	if got.ResponseSchema == nil || got.ResponseSchema.Type != "object" {
		t.Errorf("specific 200 should win over 2XX: ResponseSchema.Type = %v, want object", got.ResponseSchema)
	}
}

func TestOperationsFromSpec_BooleanSchemaWarns(t *testing.T) {
	// JSON Schema 2020-12 boolean schemas (items: false, additionalProperties:
	// false|true) cannot be represented in SchemaSpec and must not be dropped
	// silently (L-97).
	spec := &parser.Spec{
		Paths: map[string]*parser.PathItem{
			"/pets": {
				Post: &parser.Operation{
					OperationID: "createPet",
					RequestBody: &parser.RequestBody{
						Content: map[string]*parser.MediaType{
							"application/json": {Schema: &parser.Schema{
								Type:                 "object",
								AdditionalProperties: false,
								Items:                false,
							}},
						},
					},
					Responses: map[string]*parser.Response{
						"200": {
							Content: map[string]*parser.MediaType{
								"application/json": {Schema: &parser.Schema{Type: "object"}},
							},
						},
					},
				},
			},
		},
	}

	_, diags := OperationsFromSpecWithDiagnostics(spec)
	var sawItems, sawAP bool
	for _, d := range diags {
		if !strings.Contains(d.Detail, "boolean schema") {
			continue
		}
		if strings.Contains(d.Summary, "items") {
			sawItems = true
		}
		if strings.Contains(d.Summary, "additionalProperties") {
			sawAP = true
		}
	}
	if !sawItems {
		t.Errorf("expected a warning for boolean items schema, got %v", diags)
	}
	if !sawAP {
		t.Errorf("expected a warning for boolean additionalProperties schema, got %v", diags)
	}
}

// TestOperationsFromSpec_FormDataParameterWarns verifies that a formData
// parameter (OpenAPI 2.0 form-encoded request body) is surfaced as a fail-loud
// Warning rather than silently dropped: the generated request body only encodes
// JSON, so the operation is kept honestly scaffolded (REMAINING_GAPS §2).
func TestOperationsFromSpec_FormDataParameterWarns(t *testing.T) {
	// A non-primitive formData parameter (file upload) cannot be form-encoded
	// from a typed attribute, so the operation stays honestly scaffolded and a
	// fail-loud warning surfaces why. Primitive formData (string/integer/
	// number/boolean) is now wired and does not warn (see
	// TestOperationsFromSpec_PrimitiveFormDataDoesNotWarn).
	spec := &parser.Spec{
		Paths: map[string]*parser.PathItem{
			"/uploads": {
				Post: &parser.Operation{
					OperationID: "uploadFile",
					Parameters: []parser.Parameter{
						{Name: "file", In: "formData", Schema: &parser.Schema{Type: "file"}},
					},
					Responses: map[string]*parser.Response{
						"200": {Description: "ok"},
					},
				},
			},
		},
	}

	_, diags := OperationsFromSpecWithDiagnostics(spec)
	var sawFormData bool
	for _, d := range diags {
		if d.Severity == diagnostics.Warning && strings.Contains(d.Summary, "formData") {
			sawFormData = true
		}
	}
	if !sawFormData {
		t.Errorf("expected a formData-not-wired warning, got %v", diags)
	}
}

// TestOperationsFromSpec_PrimitiveFormDataDoesNotWarn verifies that a
// primitive (string) formData parameter is wired as
// application/x-www-form-urlencoded and so does not emit the not-wired warning
// (REMAINING_GAPS §2).
func TestOperationsFromSpec_PrimitiveFormDataDoesNotWarn(t *testing.T) {
	spec := &parser.Spec{
		Paths: map[string]*parser.PathItem{
			"/uploads": {
				Post: &parser.Operation{
					OperationID: "uploadFile",
					Parameters: []parser.Parameter{
						{Name: "name", In: "formData", Schema: &parser.Schema{Type: "string"}},
					},
					Responses: map[string]*parser.Response{
						"200": {Description: "ok"},
					},
				},
			},
		},
	}

	_, diags := OperationsFromSpecWithDiagnostics(spec)
	for _, d := range diags {
		if d.Severity == diagnostics.Warning && strings.Contains(d.Summary, "formData") {
			t.Errorf("primitive formData parameter must not warn (it is wired), got: %s: %s", d.Summary, d.Detail)
		}
	}
}

func TestOperationsFromSpec_DefaultResponseFallback(t *testing.T) {
	spec := &parser.Spec{
		Paths: map[string]*parser.PathItem{
			"/pets": {
				Get: &parser.Operation{
					OperationID: "listPets",
					Responses: map[string]*parser.Response{
						"default": {
							Headers: map[string]*parser.Header{
								"Link": {},
							},
							Content: map[string]*parser.MediaType{
								"application/json": {Schema: &parser.Schema{Type: "array"}},
							},
						},
					},
				},
			},
		},
	}

	ops := OperationsFromSpec(spec)
	got := ops["/pets"][MethodGet]

	if !got.ResponseBody {
		t.Errorf("ResponseBody = %v, want true", got.ResponseBody)
	}
	if got.ResponseSchema == nil || got.ResponseSchema.Type != "array" {
		t.Errorf("ResponseSchema.Type = %v, want array", got.ResponseSchema)
	}
	wantHeaders := []string{"Link"}
	if !reflect.DeepEqual(sortedStrings(got.ResponseHeaders), wantHeaders) {
		t.Errorf("ResponseHeaders = %v, want %v", got.ResponseHeaders, wantHeaders)
	}
}

func sortedStrings(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}

func TestSchemaTypeString(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{"string", "string"},
		{[]any{"string"}, "string"},
		{[]any{"string", "null"}, "string"},
		{[]any{"null", "string"}, "string"},
		// JSON Schema 3.1 multi-type unions fall back to Dynamic.
		{[]any{"string", "integer"}, ""},
		{[]any{"string", "integer", "null"}, ""},
		{[]any{"null"}, ""},
		{nil, ""},
		{42, ""},
	}
	for _, tc := range cases {
		if got := schemaTypeString(tc.in); got != tc.want {
			t.Errorf("schemaTypeString(%#v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestFirstContentSchemaDeterministicForNonJSON locks in the M-40 fix: when no
// application/json entry is present, the schema is selected from the
// schema-bearing media type that sorts first lexicographically, so the result
// is stable regardless of map iteration order.
func TestFirstContentSchemaDeterministicForNonJSON(t *testing.T) {
	hal := &parser.Schema{Type: "object", Description: "hal"}
	problem := &parser.Schema{Type: "object", Description: "problem"}
	content := map[string]*parser.MediaType{
		"application/problem+json": {Schema: problem},
		"application/hal+json":     {Schema: hal},
	}
	// "application/hal+json" < "application/problem+json" lexicographically.
	got := firstContentSchema(content)
	if got != hal {
		t.Fatalf("expected hal schema (lexicographically first), got %+v", got)
	}

	// application/json is always preferred when present, regardless of order.
	jsonSchema := &parser.Schema{Type: "object", Description: "json"}
	content["application/json"] = &parser.MediaType{Schema: jsonSchema}
	if got := firstContentSchema(content); got != jsonSchema {
		t.Fatalf("expected application/json schema to be preferred, got %+v", got)
	}
}

// TestSchemaSpecFromParserOpaqueStopsRecursion locks in the M-41 fix: an Opaque
// schema (a $ref cycle boundary set by the parser) is treated as an opaque
// boundary, so a self-referential Properties graph does not recurse forever.
func TestSchemaSpecFromParserOpaqueStopsRecursion(t *testing.T) {
	cyclic := &parser.Schema{Type: "object"}
	cyclic.Properties = map[string]*parser.Schema{"self": cyclic}
	cyclic.Opaque = true

	// This would hang/stack-overflow without the Opaque guard.
	spec := schemaSpecFromParser(nil, cyclic, nil)
	if spec == nil {
		t.Fatal("expected non-nil SchemaSpec")
	}
	if spec.Type != "object" {
		t.Fatalf("expected type object, got %q", spec.Type)
	}
	// An opaque boundary keeps its scalar fields but must not expand nested
	// schemas, so Properties is empty even though the source had one.
	if len(spec.Properties) != 0 {
		t.Fatalf("expected opaque boundary to drop nested Properties, got %d", len(spec.Properties))
	}
}

// TestSchemaSpecFromParserDepthCap locks in the M-41 depth backstop: a
// pathologically deep (non-cyclic) schema returns without stack-overflowing.
func TestSchemaSpecFromParserDepthCap(t *testing.T) {
	// Build a 5000-deep chain of object.schemas.nested.
	leaf := &parser.Schema{Type: "string"}
	curr := leaf
	for i := 0; i < 5000; i++ {
		parent := &parser.Schema{Type: "object"}
		parent.Properties = map[string]*parser.Schema{"nested": curr}
		curr = parent
	}
	// Must return, not panic with a stack overflow.
	_ = schemaSpecFromParser(nil, curr, nil)
}

// TestSchemaSpecFromParserResolvesNestedRef locks in the §3 nested-$ref fix:
// a $ref inside an object property (and inside an array's items) is resolved
// against components.schemas during SchemaSpec conversion, so the referenced
// shape contributes its real properties instead of falling back to Dynamic.
// Previously only the top-level request/response $ref was resolved; nested
// property $refs produced an empty (Dynamic) SchemaSpec.
func TestSchemaSpecFromParserResolvesNestedRef(t *testing.T) {
	category := &parser.Schema{
		Type: "object",
		Properties: map[string]*parser.Schema{
			"name": {Type: "string"},
		},
	}
	tag := &parser.Schema{
		Type: "object",
		Properties: map[string]*parser.Schema{
			"name": {Type: "string"},
		},
	}
	pet := &parser.Schema{
		Type: "object",
		Properties: map[string]*parser.Schema{
			// A nested $ref to a component schema.
			"category": {Ref: "#/components/schemas/Category"},
			// An array whose items are a nested $ref.
			"tags": {
				Type:  "array",
				Items: &parser.Schema{Ref: "#/components/schemas/Tag"},
			},
			// A plain primitive for control.
			"id": {Type: "integer"},
		},
	}
	spec := &parser.Spec{
		Components: &parser.Components{
			Schemas: map[string]*parser.Schema{
				"Pet":      pet,
				"Category": category,
				"Tag":      tag,
			},
		},
	}

	// Resolve a top-level $ref to Pet; its nested property/array $refs must
	// also resolve.
	got := schemaSpecFromParser(spec, &parser.Schema{Ref: "#/components/schemas/Pet"}, nil)
	if got == nil {
		t.Fatal("expected non-nil SchemaSpec")
	}
	if len(got.Properties) != 3 {
		t.Fatalf("expected 3 properties on Pet, got %d: %+v", len(got.Properties), got.Properties)
	}

	cat, ok := got.Properties["category"]
	if !ok {
		t.Fatal("expected category property")
	}
	if len(cat.Properties) != 1 {
		t.Errorf("expected category $ref resolved to an object with 1 property, got %+v", cat)
	}
	if _, ok := cat.Properties["name"]; !ok {
		t.Errorf("expected category.name resolved from $ref, got %+v", cat.Properties)
	}

	tags, ok := got.Properties["tags"]
	if !ok {
		t.Fatal("expected tags property")
	}
	if tags.Items == nil {
		t.Fatalf("expected tags array items resolved from $ref, got nil Items")
	}
	if len(tags.Items.Properties) != 1 {
		t.Errorf("expected tags[].name resolved from $ref (1 property), got %+v", tags.Items)
	}
}

// TestSchemaSpecFromParserCrossPropertyCycleBounded locks in the visited-set
// cycle defense: a cross-property $ref cycle (A.b -> $ref B, B.a -> $ref A)
// that is NOT marked Opaque by the parser must stop at the cycle boundary
// instead of recursing to maxSchemaDepth and emitting pathological nesting. The
// result is a shallow (bounded) schema, and the call returns promptly.
func TestSchemaSpecFromParserCrossPropertyCycleBounded(t *testing.T) {
	a := &parser.Schema{Type: "object"}
	b := &parser.Schema{Type: "object"}
	a.Properties = map[string]*parser.Schema{"b": {Ref: "#/components/schemas/B"}}
	b.Properties = map[string]*parser.Schema{"a": {Ref: "#/components/schemas/A"}}
	spec := &parser.Spec{
		Components: &parser.Components{
			Schemas: map[string]*parser.Schema{"A": a, "B": b},
		},
	}

	done := make(chan struct{})
	var got *SchemaSpec
	go func() {
		got = schemaSpecFromParser(spec, &parser.Schema{Ref: "#/components/schemas/A"}, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("schemaSpecFromParser hung on a cross-property cycle (visited-set defense regressed)")
	}
	if got == nil {
		t.Fatal("expected non-nil SchemaSpec")
	}
	bProp, ok := got.Properties["b"]
	if !ok {
		t.Fatal("expected property b on A")
	}
	aProp, ok := bProp.Properties["a"]
	if !ok {
		t.Fatal("expected property a on B (resolved once before the cycle boundary)")
	}
	// a is the cycle boundary: it must not descend further, so it carries no
	// nested Properties/Items. This is what keeps the generated nesting shallow.
	if len(aProp.Properties) != 0 || aProp.Items != nil {
		t.Errorf("expected cycle boundary to stop descent (no nested shape), got Properties=%v Items=%+v", aProp.Properties, aProp.Items)
	}
}

// names no component schema does not panic and falls back to an empty (Dynamic)
// shape rather than dropping the property — fail-loud is the renderer's job via
// diagnostics, but the SchemaSpec must remain structurally valid.
func TestSchemaSpecFromParserNestedRefUnresolvable(t *testing.T) {
	spec := &parser.Spec{
		Components: &parser.Components{
			Schemas: map[string]*parser.Schema{},
		},
	}
	parent := &parser.Schema{
		Type: "object",
		Properties: map[string]*parser.Schema{
			"missing": {Ref: "#/components/schemas/Missing"},
			"id":      {Type: "string"},
		},
	}
	got := schemaSpecFromParser(spec, parent, nil)
	if got == nil {
		t.Fatal("expected non-nil SchemaSpec")
	}
	if len(got.Properties) != 2 {
		t.Fatalf("expected 2 properties (missing + id), got %d", len(got.Properties))
	}
	// The unresolvable ref yields an empty schema (no type, no properties);
	// the sibling primitive is unaffected.
	if _, ok := got.Properties["id"]; !ok {
		t.Errorf("expected id property present alongside unresolvable ref, got %+v", got.Properties)
	}
}

// TestSchemaSpecFromParserFlattensNestedAllOf locks in allOf flattening inside
// nested properties (REMAINING_GAPS §3): a property whose schema composes two
// object schemas via allOf must carry the union of their properties (and the
// union of their required lists) instead of falling back to Dynamic.
func TestSchemaSpecFromParserFlattensNestedAllOf(t *testing.T) {
	spec := &parser.Spec{
		Components: &parser.Components{
			Schemas: map[string]*parser.Schema{
				"Base": {
					Type:     "object",
					Required: []string{"id"},
					Properties: map[string]*parser.Schema{
						"id": {Type: "string"},
					},
				},
				"Traits": {
					Type: "object",
					Properties: map[string]*parser.Schema{
						"name": {Type: "string"},
						"tags": {Type: "array", Items: &parser.Schema{Type: "string"}},
					},
					Required: []string{"name"},
				},
			},
		},
	}
	parent := &parser.Schema{
		Type: "object",
		Properties: map[string]*parser.Schema{
			"item": {
				AllOf: []*parser.Schema{
					{Ref: "#/components/schemas/Base"},
					{Ref: "#/components/schemas/Traits"},
				},
			},
		},
	}
	got := schemaSpecFromParser(spec, parent, nil)
	if got == nil {
		t.Fatal("expected non-nil SchemaSpec")
	}
	item, ok := got.Properties["item"]
	if !ok {
		t.Fatal("expected item property")
	}
	if len(item.Properties) != 3 {
		t.Fatalf("expected allOf-flattened item to have 3 properties (id,name,tags), got %d: %+v", len(item.Properties), item.Properties)
	}
	for _, want := range []string{"id", "name", "tags"} {
		if _, ok := item.Properties[want]; !ok {
			t.Errorf("expected flattened property %q, got %+v", want, item.Properties)
		}
	}
	// Required is the union of Base.Required (id) and Traits.Required (name).
	wantRequired := map[string]bool{"id": true, "name": true}
	for _, r := range item.Required {
		if !wantRequired[r] {
			t.Errorf("unexpected required entry %q in %v", r, item.Required)
		}
		delete(wantRequired, r)
	}
	if len(wantRequired) > 0 {
		t.Errorf("missing required entries %v (got %v)", wantRequired, item.Required)
	}
	// Array items from the Traits member survive the merge.
	if item.Properties["tags"].Items == nil {
		t.Errorf("expected tags array items to survive allOf merge, got %+v", item.Properties["tags"])
	}
}

// TestSchemaSpecFromParserOneOfAnyOfWarns locks in the fail-loud behavior for
// oneOf/anyOf composition in nested properties (REMAINING_GAPS §3): the
// composition is not flattened into a single object (semantically wrong for
// alternatives), and a Warning diagnostic is emitted instead of a silent drop.
func TestSchemaSpecFromParserOneOfAnyOfWarns(t *testing.T) {
	spec := &parser.Spec{
		Components: &parser.Components{
			Schemas: map[string]*parser.Schema{
				"Cat": {Type: "object", Properties: map[string]*parser.Schema{"meow": {Type: "string"}}},
				"Dog": {Type: "object", Properties: map[string]*parser.Schema{"bark": {Type: "string"}}},
			},
		},
	}
	parent := &parser.Schema{
		Type: "object",
		Properties: map[string]*parser.Schema{
			"pet":  {OneOf: []*parser.Schema{{Ref: "#/components/schemas/Cat"}, {Ref: "#/components/schemas/Dog"}}},
			"obj":  {AnyOf: []*parser.Schema{{Ref: "#/components/schemas/Cat"}, {Ref: "#/components/schemas/Dog"}}},
			"name": {Type: "string"},
		},
	}
	var diags diagnostics.Diagnostics
	got := schemaSpecFromParser(spec, parent, &diags)
	if got == nil {
		t.Fatal("expected non-nil SchemaSpec")
	}
	if len(got.Properties) != 3 {
		t.Fatalf("expected 3 properties preserved, got %d", len(got.Properties))
	}
	// oneOf/anyOf are NOT flattened into the union of variant properties.
	pet := got.Properties["pet"]
	if len(pet.Properties) != 0 {
		t.Errorf("expected oneOf NOT flattened (no variant properties), got %+v", pet.Properties)
	}
	// A warning must be emitted for each composition kind.
	var oneOfWarn, anyOfWarn bool
	for _, d := range diags {
		if d.Severity != diagnostics.Warning {
			continue
		}
		if strings.Contains(d.Summary, "oneOf") {
			oneOfWarn = true
		}
		if strings.Contains(d.Summary, "anyOf") {
			anyOfWarn = true
		}
	}
	if !oneOfWarn {
		t.Errorf("expected a oneOf composition-not-modeled warning, got diags: %v", diags)
	}
	if !anyOfWarn {
		t.Errorf("expected an anyOf composition-not-modeled warning, got diags: %v", diags)
	}
}
