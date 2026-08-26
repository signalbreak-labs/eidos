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
	// JSON Schema 2020-12 boolean schemas that cause a real information loss
	// (items: false → array must be empty; additionalProperties: true → open
	// map Terraform cannot model) must not be dropped silently (L-97).
	// additionalProperties: false is deliberately NOT warned (benign: Terraform
	// objects are closed by default), so this spec uses true to exercise the
	// warning path.
	spec := &parser.Spec{
		Paths: map[string]*parser.PathItem{
			"/pets": {
				Post: &parser.Operation{
					OperationID: "createPet",
					RequestBody: &parser.RequestBody{
						Content: map[string]*parser.MediaType{
							"application/json": {Schema: &parser.Schema{
								Type:                 "object",
								AdditionalProperties: true,
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

// TestOperationsFromSpec_BooleanAdditionalPropertiesFalseNoWarn asserts that
// additionalProperties: false (a closed property set) does NOT emit a warning:
// Terraform objects are closed by default, so dropping the constraint is a
// no-op with no information loss. Real-world specs declare false on thousands
// of objects (Linode: 3147); warning on each would drown out genuine losses.
func TestOperationsFromSpec_BooleanAdditionalPropertiesFalseNoWarn(t *testing.T) {
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
								Properties: map[string]*parser.Schema{
									"name": {Type: "string"},
								},
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
	for _, d := range diags {
		if strings.Contains(d.Summary, "additionalProperties") {
			t.Fatalf("expected NO warning for additionalProperties: false (benign, closed set is Terraform default), got: %s: %s", d.Summary, d.Detail)
		}
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

// TestOperationsFromSpec_FormDataInBodyContentWarns locks in the N-11 fix: the
// v2 parser normalizes `in: formData` parameters into the request body's
// form/multipart content schema (and drops them from op.Parameters), so a
// non-primitive formData property (file upload, object, array) surfaces the
// fail-loud "formData parameter not wired" Warning by walking the request body
// content — not just the direct formData parameters.
func TestOperationsFromSpec_FormDataInBodyContentWarns(t *testing.T) {
	spec := &parser.Spec{
		Paths: map[string]*parser.PathItem{
			"/uploads": {
				Post: &parser.Operation{
					OperationID: "uploadFile",
					RequestBody: &parser.RequestBody{
						Content: map[string]*parser.MediaType{
							"multipart/form-data": {
								Schema: &parser.Schema{
									Type: "object",
									Properties: map[string]*parser.Schema{
										"file":    {Type: "file"},
										"caption": {Type: "string"},
									},
								},
							},
						},
					},
					Responses: map[string]*parser.Response{
						"200": {Description: "ok"},
					},
				},
			},
		},
	}

	_, diags := OperationsFromSpecWithDiagnostics(spec)
	var fileWarn, captionWarn bool
	for _, d := range diags {
		if d.Severity != diagnostics.Warning || !strings.Contains(d.Summary, "formData") {
			continue
		}
		if strings.Contains(d.Detail, "file") {
			fileWarn = true
		}
		if strings.Contains(d.Detail, "caption") {
			captionWarn = true
		}
	}
	if !fileWarn {
		t.Errorf("expected a formData-not-wired Warning for the file property, got %v", diags)
	}
	if captionWarn {
		t.Errorf("primitive formData property caption must not warn (it is wired), got %v", diags)
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

// TestOperationsFromSpec_DefaultResponseNotSuccess locks in N-16: the `default`
// response is the OpenAPI catch-all and frequently describes errors, so it is
// NOT treated as the success schema. An operation that declares only a default
// (plus error codes) carries no response schema and surfaces a fail-loud
// warning instead of deriving the entire resource shape from the default.
func TestOperationsFromSpec_DefaultResponseNotSuccess(t *testing.T) {
	spec := &parser.Spec{
		Paths: map[string]*parser.PathItem{
			"/pets": {
				Get: &parser.Operation{
					OperationID: "listPets",
					Responses: map[string]*parser.Response{
						"404": {
							Content: map[string]*parser.MediaType{
								"application/json": {Schema: &parser.Schema{Type: "object"}},
							},
						},
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

	ops, diags := OperationsFromSpecWithDiagnostics(spec)
	got := ops["/pets"][MethodGet]

	if got.ResponseBody {
		t.Errorf("ResponseBody = %v, want false (no 2xx success response)", got.ResponseBody)
	}
	if got.ResponseSchema != nil {
		t.Errorf("ResponseSchema = %+v, want nil (default must not be used as the success schema)", got.ResponseSchema)
	}
	if len(got.ResponseHeaders) != 0 {
		t.Errorf("ResponseHeaders = %v, want none (default headers must not be adopted)", got.ResponseHeaders)
	}
	warned := false
	for _, d := range diags {
		if d.Severity == diagnostics.Warning && strings.Contains(d.Summary, "no 2xx response") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("expected a fail-loud warning that the default response is not used as success, got diags=%v", diags)
	}
}

// TestOperationsFromSpec_ContentBearing2xxBeatsEmptyLower2xx locks in the
// other half of N-16: an empty 200 (no content) must not shadow a
// content-bearing 201 just because 200 sorts first.
func TestOperationsFromSpec_ContentBearing2xxBeatsEmptyLower2xx(t *testing.T) {
	spec := &parser.Spec{
		Paths: map[string]*parser.PathItem{
			"/pets": {
				Post: &parser.Operation{
					OperationID: "createPet",
					Responses: map[string]*parser.Response{
						"200": {}, // declared but carries no content
						"201": {
							Content: map[string]*parser.MediaType{
								"application/json": {Schema: &parser.Schema{Type: "object"}},
							},
						},
					},
				},
			},
		},
	}

	ops := OperationsFromSpec(spec)
	got := ops["/pets"][MethodPost]

	if !got.ResponseBody {
		t.Errorf("ResponseBody = %v, want true", got.ResponseBody)
	}
	if got.ResponseSchema == nil || got.ResponseSchema.Type != "object" {
		t.Errorf("ResponseSchema.Type = %v, want object (content-bearing 201 should win over empty 200)", got.ResponseSchema)
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

// TestSchemaSpecFromParserCyclicDepthBounded locks in the circular-schema fix
// (docs/PROJECT_DESIGN.md §12.4): a $ref the parser marks Opaque
// is expanded up to maxCyclicDepth levels — preserving first-entry properties so
// the generated attribute stays a concrete object instead of degrading to
// Dynamic — and then cut to an opaque boundary, so the conversion returns
// promptly instead of re-expanding the cycle on every descent (the generator
// hang). This mirrors TestSchemaSpecFromParserCrossPropertyCycleBounded but
// exercises the Opaque/cycleDepth path the parser drives on real specs.
func TestSchemaSpecFromParserCyclicDepthBounded(t *testing.T) {
	// Department: id, subDepartments (array of Department). The items ref-holder
	// is Opaque, exactly as the parser marks it (Department is circular).
	department := &parser.Schema{Type: "object"}
	department.Properties = map[string]*parser.Schema{
		"id":             {Type: "string"},
		"subDepartments": {Type: "array", Items: &parser.Schema{Ref: "#/components/schemas/Department", Opaque: true}},
	}
	spec := &parser.Spec{
		Components: &parser.Components{
			Schemas: map[string]*parser.Schema{"Department": department},
		},
	}

	// Top-level ref to Department; its ref-holder is Opaque (Department is
	// circular). Must return promptly instead of hanging on the self-cycle.
	done := make(chan struct{})
	var got *SchemaSpec
	go func() {
		got = schemaSpecFromParser(spec, &parser.Schema{Ref: "#/components/schemas/Department", Opaque: true}, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("schemaSpecFromParser hung on a self-referential Opaque cycle (cycleDepth bound regressed)")
	}
	if got == nil {
		t.Fatal("expected non-nil SchemaSpec")
	}
	// First-entry properties are preserved: Department keeps its scalar fields,
	// so the generated attribute is a concrete object rather than Dynamic.
	if got.Type != "object" {
		t.Fatalf("expected type object (first-entry properties preserved), got %q", got.Type)
	}
	if len(got.Properties) == 0 {
		t.Fatal("expected first-entry properties preserved (non-empty Properties), got none")
	}
	sub, ok := got.Properties["subDepartments"]
	if !ok {
		t.Fatal("expected subDepartments property preserved at the first entry")
	}
	if sub.Items == nil {
		t.Fatal("expected subDepartments items expanded one level (not a scalar boundary)")
	}
	// The first-level expansion descends into Department again (cycleDepth 1 -> 2);
	// at cycleDepth == maxCyclicDepth the cycle is cut, so the next re-entry carries
	// no nested shape. This is what keeps the IR finite and shallow.
	if len(sub.Items.Properties) == 0 {
		t.Fatalf("expected the expanded level to carry Department's properties, got none")
	}
	innerSub, ok := sub.Items.Properties["subDepartments"]
	if !ok {
		t.Fatal("expected inner subDepartments at the expanded level")
	}
	if innerSub.Items == nil || len(innerSub.Items.Properties) != 0 {
		t.Errorf("expected the cycle to be cut at maxCyclicDepth (no nested shape past the boundary), got Properties=%v", innerSub.Items.Properties)
	}
}

// TestSchemaSpecFromParserCyclicMemoSound locks in the memo soundness half of
// the fix: a cyclic component reached at two different cycleDepths within one
// conversion must produce two distinct forms (one expanded, one cut), not a
// single conflated cached form. The pre-fix memo was keyed only on the schema
// pointer, so whichever path converted first won and other paths got the wrong
// (over- or under-expanded) shape; keying on (schema, cycleDepth) fixes that.
func TestSchemaSpecFromParserCyclicMemoSound(t *testing.T) {
	// department: id, subDepartments (array of Department), cyclic.
	department := &parser.Schema{Type: "object"}
	department.Properties = map[string]*parser.Schema{
		"id":             {Type: "string"},
		"subDepartments": {Type: "array", Items: &parser.Schema{Ref: "#/components/schemas/Department", Opaque: true}},
	}
	// wrapper: a second cyclic schema that also references Department, so
	// Department is reached at cycleDepth 1 (via "direct") and cycleDepth 2 (via
	// "through" -> Wrapper.dept) in the same top-level conversion.
	wrapper := &parser.Schema{Type: "object"}
	wrapper.Properties = map[string]*parser.Schema{
		"dept": {Ref: "#/components/schemas/Department", Opaque: true},
	}
	spec := &parser.Spec{
		Components: &parser.Components{
			Schemas: map[string]*parser.Schema{"Department": department, "Wrapper": wrapper},
		},
	}
	root := &parser.Schema{Type: "object", Properties: map[string]*parser.Schema{
		"direct":  {Ref: "#/components/schemas/Department", Opaque: true},
		"through": {Ref: "#/components/schemas/Wrapper", Opaque: true},
	}}

	got := schemaSpecFromParser(spec, root, nil)
	if got == nil {
		t.Fatal("expected non-nil SchemaSpec")
	}
	direct, ok := got.Properties["direct"]
	if !ok {
		t.Fatal("expected direct property")
	}
	through, ok := got.Properties["through"]
	if !ok {
		t.Fatal("expected through property")
	}
	// "direct" reaches Department at cycleDepth 1, so its subDepartments items
	// expand one more level (Department at cycleDepth 2, which then cuts).
	directSub, ok := direct.Properties["subDepartments"]
	if !ok || directSub.Items == nil {
		t.Fatal("expected direct.subDepartments to be expanded (cycleDepth 1 -> 2)")
	}
	if len(directSub.Items.Properties) == 0 {
		t.Error("expected direct.subDepartments items to carry Department's properties (expanded form)")
	}
	// "through" reaches Department at cycleDepth 2 (through -> Wrapper.dept), so
	// Department's own cyclic ref (subDepartments) is cut: its items is a scalar
	// boundary carrying no nested shape.
	throughDept, ok := through.Properties["dept"]
	if !ok {
		t.Fatal("expected through.dept (Department reached via Wrapper)")
	}
	throughSub, ok := throughDept.Properties["subDepartments"]
	if !ok {
		t.Fatal("expected through.dept.subDepartments")
	}
	if throughSub.Items == nil || len(throughSub.Items.Properties) != 0 {
		t.Errorf("expected through.dept.subDepartments items cut at maxCyclicDepth (no nested shape), got Properties=%v", throughSub.Items.Properties)
	}
	// The two Department occurrences have distinct shapes (expanded vs cut);
	// a sound memo must not conflate them. If the memo were keyed on the schema
	// pointer alone, the cut form and the expanded form would collapse into one
	// and one of these assertions would fail.
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

// TestSchemaSpecFromParserAllOfConflictWarns locks in the M-5 fix: when two
// allOf members define the same property with different schemas, the first
// member's definition wins (first-wins) and a Warning naming the property is
// emitted instead of the drop being silent.
func TestSchemaSpecFromParserAllOfConflictWarns(t *testing.T) {
	spec := &parser.Spec{
		Components: &parser.Components{
			Schemas: map[string]*parser.Schema{
				"One": {Type: "object", Properties: map[string]*parser.Schema{"name": {Type: "string"}}},
				"Two": {Type: "object", Properties: map[string]*parser.Schema{"name": {Type: "integer"}}},
			},
		},
	}
	parent := &parser.Schema{
		AllOf: []*parser.Schema{
			{Ref: "#/components/schemas/One"},
			{Ref: "#/components/schemas/Two"},
		},
	}
	var diags diagnostics.Diagnostics
	got := schemaSpecFromParser(spec, parent, &diags)
	if got == nil {
		t.Fatal("expected non-nil SchemaSpec")
	}
	// First-wins: name keeps the string type from the first member.
	prop, ok := got.Properties["name"]
	if !ok {
		t.Fatal("expected name property")
	}
	if prop.Type != "string" {
		t.Errorf("name type = %q, want string (first allOf member wins)", prop.Type)
	}
	if !hasWarning(diags, "allOf property conflict") || !hasWarning(diags, "name") {
		t.Fatalf("expected an allOf conflict Warning naming the property, got diags: %+v", diags)
	}
}

// TestSchemaSpecFromParserRefNameNotLeakedAcrossRefs locks in the M-6 fix: a
// memoized SchemaSpec returned for an acyclic ref is a copy, so the RefName a
// caller stamps on one ref does not leak into later refs to the same underlying
// schema. Without the copy, both oneOf variants here would end up carrying the
// same RefName, and which one won would depend on map-iteration order — breaking
// byte-identical determinism.
func TestSchemaSpecFromParserRefNameNotLeakedAcrossRefs(t *testing.T) {
	spec := &parser.Spec{
		Components: &parser.Components{
			Schemas: map[string]*parser.Schema{
				"Pet": {
					Type: "object",
					Properties: map[string]*parser.Schema{
						"name": {Type: "string"},
					},
				},
				"PetAlias": {
					Ref: "#/components/schemas/Pet",
				},
			},
		},
	}
	parent := &parser.Schema{
		OneOf: []*parser.Schema{
			{Ref: "#/components/schemas/Pet"},
			{Ref: "#/components/schemas/PetAlias"},
		},
	}

	// Convert twice: the RefNames must be identical across runs.
	for i := 0; i < 2; i++ {
		got := schemaSpecFromParser(spec, parent, nil)
		if got == nil {
			t.Fatal("expected non-nil SchemaSpec")
		}
		if len(got.OneOf) != 2 {
			t.Fatalf("expected 2 oneOf variants, got %d: %+v", len(got.OneOf), got.OneOf)
		}
		names := map[string]bool{}
		for _, v := range got.OneOf {
			names[v.RefName] = true
		}
		if !names["Pet"] {
			t.Errorf("iteration %d: expected a variant with RefName %q, got %v", i, "Pet", got.OneOf)
		}
		if !names["PetAlias"] {
			t.Errorf("iteration %d: expected a variant with RefName %q (not leaked %q), got %v", i, "PetAlias", "Pet", got.OneOf)
		}
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

// objSpec builds a flat object SchemaSpec with the given property names mapped
// to string scalars, used as a stand-in request body or envelope payload.
func objSpec(props ...string) *SchemaSpec {
	p := make(map[string]SchemaSpec, len(props))
	for _, name := range props {
		p[name] = SchemaSpec{Type: "string"}
	}
	return &SchemaSpec{Type: "object", Properties: p}
}

func TestUnwrapResponseEnvelope(t *testing.T) {
	inner := SchemaSpec{
		Type: "object",
		Properties: map[string]SchemaSpec{
			"uuid": {Type: "string"},
			"name": {Type: "string"},
		},
	}
	wrap := func(key string, extra map[string]SchemaSpec) *SchemaSpec {
		props := map[string]SchemaSpec{key: inner}
		for k, v := range extra {
			props[k] = v
		}
		return &SchemaSpec{Type: "object", Properties: props}
	}

	tests := []struct {
		name        string
		spec        *SchemaSpec
		requestSpec *SchemaSpec
		wantKey     string
		wantUnwrap  bool
	}{
		{
			name:       "data envelope always unwrapped",
			spec:       wrap("data", map[string]SchemaSpec{"meta": {Type: "object", Properties: map[string]SchemaSpec{"total": {Type: "integer"}}}}),
			wantKey:    "data",
			wantUnwrap: true,
		},
		{
			name:        "arbitrary wrapper with flat request is unwrapped",
			spec:        wrap("agent", nil),
			requestSpec: objSpec("name", "description"),
			wantKey:     "agent",
			wantUnwrap:  true,
		},
		{
			name:        "arbitrary wrapper with no request body is unwrapped (read/data source)",
			spec:        wrap("agent", nil),
			requestSpec: nil,
			wantKey:     "agent",
			wantUnwrap:  true,
		},
		{
			name:        "same-key single-property request is NOT unwrapped (genuine wrapped resource)",
			spec:        wrap("agent", nil),
			requestSpec: &SchemaSpec{Type: "object", Properties: map[string]SchemaSpec{"agent": inner}},
			wantKey:     "",
			wantUnwrap:  false,
		},
		{
			name:        "wrapper with a non-companion peer field is NOT unwrapped",
			spec:        wrap("agent", map[string]SchemaSpec{"count": {Type: "integer"}}),
			requestSpec: objSpec("name"),
			wantKey:     "",
			wantUnwrap:  false,
		},
		{
			name:        "scalar wrapper value is NOT unwrapped",
			spec:        &SchemaSpec{Type: "object", Properties: map[string]SchemaSpec{"agent": {Type: "string"}}},
			requestSpec: objSpec("name"),
			wantKey:     "",
			wantUnwrap:  false,
		},
		{
			name:        "multi-field response is NOT unwrapped",
			spec:        objSpec("name", "description", "region"),
			requestSpec: objSpec("name", "description"),
			wantKey:     "",
			wantUnwrap:  false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, key := UnwrapResponseEnvelope(tc.spec, tc.requestSpec)
			if key != tc.wantKey {
				t.Errorf("envelope key = %q, want %q", key, tc.wantKey)
			}
			unwrap := key != "" && got != tc.spec
			if unwrap != tc.wantUnwrap {
				t.Errorf("unwrapped = %v, want %v (got=%v)", unwrap, tc.wantUnwrap, got)
			}
			if tc.wantUnwrap && got != nil && len(got.Properties) != len(inner.Properties) {
				t.Errorf("unwrapped schema should expose inner payload properties, got %d props", len(got.Properties))
			}
		})
	}
}

// TestUnwrapResponseEnvelope_CollectionContext locks in issue #40: a collection
// response shaped {<items>: [...], context: {...}} is an envelope, not a
// two-field resource. Unwrap it to the item array so list resources can wire.
// {object, context} stays wrapped — that is a real two-field resource.
func TestUnwrapResponseEnvelope_CollectionContext(t *testing.T) {
	item := &SchemaSpec{
		Type: "object",
		Properties: map[string]SchemaSpec{
			"id":   {Type: "string"},
			"name": {Type: "string"},
		},
	}
	mapsArray := SchemaSpec{Type: "array", Items: item}
	contextObj := SchemaSpec{
		Type:       "object",
		Properties: map[string]SchemaSpec{"offset": {Type: "integer"}},
	}

	t.Run("{maps, context} unwraps to the maps array", func(t *testing.T) {
		spec := &SchemaSpec{
			Type: "object",
			Properties: map[string]SchemaSpec{
				"maps":    mapsArray,
				"context": contextObj,
			},
		}
		got, key := UnwrapResponseEnvelope(spec, nil)
		if key != "maps" {
			t.Errorf("envelope key = %q, want %q", key, "maps")
		}
		if got == nil || !strings.EqualFold(got.Type, "array") {
			t.Fatalf("unwrapped type = %v, want array", got)
		}
		if got.Items == nil || len(got.Items.Properties) != 2 {
			t.Errorf("unwrapped array items should expose the map object, got %+v", got.Items)
		}
	})

	t.Run("{maps, context, meta} unwraps (meta is a companion)", func(t *testing.T) {
		spec := &SchemaSpec{
			Type: "object",
			Properties: map[string]SchemaSpec{
				"maps":    mapsArray,
				"context": contextObj,
				"meta":    {Type: "object", Properties: map[string]SchemaSpec{"total": {Type: "integer"}}},
			},
		}
		got, key := UnwrapResponseEnvelope(spec, nil)
		if key != "maps" {
			t.Errorf("envelope key = %q, want %q", key, "maps")
		}
		if got == nil || !strings.EqualFold(got.Type, "array") {
			t.Fatalf("unwrapped type = %v, want array", got)
		}
	})

	t.Run("{data, context} unwraps to the data array", func(t *testing.T) {
		spec := &SchemaSpec{
			Type: "object",
			Properties: map[string]SchemaSpec{
				"data":    mapsArray,
				"context": contextObj,
			},
		}
		got, key := UnwrapResponseEnvelope(spec, nil)
		if key != "data" {
			t.Errorf("envelope key = %q, want %q", key, "data")
		}
		if got == nil || !strings.EqualFold(got.Type, "array") {
			t.Fatalf("unwrapped type = %v, want array", got)
		}
	})

	t.Run("{agent, context} is NOT unwrapped (object payload, not a collection)", func(t *testing.T) {
		spec := &SchemaSpec{
			Type: "object",
			Properties: map[string]SchemaSpec{
				"agent":   innerObjectSpec(),
				"context": contextObj,
			},
		}
		got, key := UnwrapResponseEnvelope(spec, nil)
		if key != "" {
			t.Errorf("envelope key = %q, want empty (real two-field resource)", key)
		}
		if got != spec {
			t.Errorf("spec should be returned unchanged, got %+v", got)
		}
	})

	t.Run("{maps, clusters, context} is NOT unwrapped (two arrays)", func(t *testing.T) {
		spec := &SchemaSpec{
			Type: "object",
			Properties: map[string]SchemaSpec{
				"maps":     mapsArray,
				"clusters": mapsArray,
				"context":  contextObj,
			},
		}
		got, key := UnwrapResponseEnvelope(spec, nil)
		if key != "" {
			t.Errorf("envelope key = %q, want empty (ambiguous collection)", key)
		}
		if got != spec {
			t.Errorf("spec should be returned unchanged, got %+v", got)
		}
	})
}

func innerObjectSpec() SchemaSpec {
	return SchemaSpec{
		Type: "object",
		Properties: map[string]SchemaSpec{
			"uuid": {Type: "string"},
			"name": {Type: "string"},
		},
	}
}

// TestOperationsFromSpec_RequiredReadOnlyPropertyWarns locks in issue #40's
// spec-hygiene diagnostic: a property listed in required that is also
// readOnly is a contradiction. Fail loud so spec authors see it instead of
// silently resolving the conflict to Computed or Required.
func TestOperationsFromSpec_RequiredReadOnlyPropertyWarns(t *testing.T) {
	spec := &parser.Spec{
		Paths: map[string]*parser.PathItem{
			"/clients": {
				Post: &parser.Operation{
					OperationID: "createClient",
					RequestBody: &parser.RequestBody{
						Content: map[string]*parser.MediaType{
							"application/json": {Schema: &parser.Schema{
								Type:     "object",
								Required: []string{"clusterId", "name"},
								Properties: map[string]*parser.Schema{
									"clusterId": {Type: "string", ReadOnly: true},
									"name":      {Type: "string"},
								},
							}},
						},
					},
					Responses: map[string]*parser.Response{
						"201": {Description: "created"},
					},
				},
			},
		},
	}

	_, diags := OperationsFromSpecWithDiagnostics(spec)
	var saw bool
	for _, d := range diags {
		if d.Severity == diagnostics.Warning &&
			strings.Contains(d.Summary, "required") &&
			strings.Contains(strings.ToLower(d.Summary), "readonly") {
			saw = true
			if !strings.Contains(d.Detail, "clusterId") {
				t.Errorf("warning detail should name clusterId, got: %s", d.Detail)
			}
		}
	}
	if !saw {
		t.Errorf("expected a required+readOnly warning, got diagnostics: %+v", diags)
	}
}

// TestOperationsFromSpec_RequiredReadOnlyAbsentWhenNotBoth asserts a required
// writable property, or a readOnly property that is not required, does not
// emit the required+readOnly warning.
func TestOperationsFromSpec_RequiredReadOnlyAbsentWhenNotBoth(t *testing.T) {
	spec := &parser.Spec{
		Paths: map[string]*parser.PathItem{
			"/clients": {
				Post: &parser.Operation{
					OperationID: "createClient",
					RequestBody: &parser.RequestBody{
						Content: map[string]*parser.MediaType{
							"application/json": {Schema: &parser.Schema{
								Type:     "object",
								Required: []string{"name"},
								Properties: map[string]*parser.Schema{
									"clusterId": {Type: "string", ReadOnly: true},
									"name":      {Type: "string"},
								},
							}},
						},
					},
					Responses: map[string]*parser.Response{
						"201": {Description: "created"},
					},
				},
			},
		},
	}

	_, diags := OperationsFromSpecWithDiagnostics(spec)
	for _, d := range diags {
		if strings.Contains(strings.ToLower(d.Summary), "readonly") &&
			strings.Contains(d.Summary, "required") {
			t.Errorf("did not expect a required+readOnly warning, got: %+v", d)
		}
	}
}

// TestMergeAllOfSpecProperties_Descriptions covers the interaction between the
// allOf first-wins property merge and property descriptions: prose alone must
// not read as a structural conflict (at any depth), a documented member must
// be able to document an undocumented one, and a genuinely conflicting member's
// prose must not be pasted onto the differently-shaped survivor.
func TestMergeAllOfSpecProperties_Descriptions(t *testing.T) {
	t.Run("same shape, different wording is not a conflict", func(t *testing.T) {
		dst := &SchemaSpec{Type: "object", Properties: map[string]SchemaSpec{
			"label": {Type: "string", Description: "First wording."},
		}}
		conflicts := mergeAllOfSpecProperties(dst, SchemaSpec{Properties: map[string]SchemaSpec{
			"label": {Type: "string", Description: "Second wording."},
		}})
		if len(conflicts) != 0 {
			t.Errorf("conflicts = %v, want none", conflicts)
		}
		if got := dst.Properties["label"].Description; got != "First wording." {
			t.Errorf("description = %q, want the first member's", got)
		}
	})

	t.Run("nested wording difference is not a conflict", func(t *testing.T) {
		nested := func(desc string) SchemaSpec {
			return SchemaSpec{Type: "object", Properties: map[string]SchemaSpec{
				"owner": {Type: "string", Description: desc},
			}}
		}
		dst := &SchemaSpec{Type: "object", Properties: map[string]SchemaSpec{"meta": nested("First wording.")}}
		conflicts := mergeAllOfSpecProperties(dst, SchemaSpec{Properties: map[string]SchemaSpec{"meta": nested("Second wording.")}})
		if len(conflicts) != 0 {
			t.Errorf("conflicts = %v, want none (only nested prose differs)", conflicts)
		}
	})

	t.Run("undocumented property adopts a later member's description", func(t *testing.T) {
		dst := &SchemaSpec{Type: "object", Properties: map[string]SchemaSpec{
			"label": {Type: "string"},
		}}
		if conflicts := mergeAllOfSpecProperties(dst, SchemaSpec{Properties: map[string]SchemaSpec{
			"label": {Type: "string", Description: "Adopted."},
		}}); len(conflicts) != 0 {
			t.Errorf("conflicts = %v, want none", conflicts)
		}
		if got := dst.Properties["label"].Description; got != "Adopted." {
			t.Errorf("description = %q, want %q", got, "Adopted.")
		}
	})

	t.Run("shape conflict still warns and does not adopt the dropped prose", func(t *testing.T) {
		dst := &SchemaSpec{Type: "object", Properties: map[string]SchemaSpec{
			"count": {Type: "string", Description: "As string."},
		}}
		conflicts := mergeAllOfSpecProperties(dst, SchemaSpec{Properties: map[string]SchemaSpec{
			"count": {Type: "integer", Description: "As integer."},
		}})
		if len(conflicts) != 1 || conflicts[0] != "count" {
			t.Errorf("conflicts = %v, want [count]", conflicts)
		}
		if got := dst.Properties["count"].Description; got != "As string." {
			t.Errorf("description = %q, want the surviving member's prose", got)
		}
	})

	t.Run("a member with no properties contributes nothing", func(t *testing.T) {
		dst := &SchemaSpec{Type: "object", Properties: map[string]SchemaSpec{
			"label": {Type: "string", Description: "Kept."},
		}}
		if conflicts := mergeAllOfSpecProperties(dst, SchemaSpec{Type: "object"}); conflicts != nil {
			t.Errorf("conflicts = %v, want nil", conflicts)
		}
		if got := dst.Properties["label"].Description; got != "Kept." {
			t.Errorf("description = %q, want it untouched", got)
		}
	})

	t.Run("undocumented property with a conflicting shape stays undocumented", func(t *testing.T) {
		dst := &SchemaSpec{Type: "object", Properties: map[string]SchemaSpec{
			"count": {Type: "string"},
		}}
		mergeAllOfSpecProperties(dst, SchemaSpec{Properties: map[string]SchemaSpec{
			"count": {Type: "integer", Description: "Describes the integer shape."},
		}})
		if got := dst.Properties["count"].Description; got != "" {
			t.Errorf("description = %q, want empty (the prose describes the dropped shape)", got)
		}
	})
}

// TestSameSchemaShape_IgnoresDescriptionAtEveryDepth covers the recursive
// description stripping. A SchemaSpec nests through Properties, Items,
// AdditionalProperties, OneOf and AnyOf; wording that differs in any of those
// positions is prose, not a structural conflict.
func TestSameSchemaShape_IgnoresDescriptionAtEveryDepth(t *testing.T) {
	cases := map[string]func(desc string) SchemaSpec{
		"top level": func(d string) SchemaSpec {
			return SchemaSpec{Type: "string", Description: d}
		},
		"object property": func(d string) SchemaSpec {
			return SchemaSpec{Type: "object", Properties: map[string]SchemaSpec{"x": {Type: "string", Description: d}}}
		},
		"array items": func(d string) SchemaSpec {
			return SchemaSpec{Type: "array", Items: &SchemaSpec{Type: "string", Description: d}}
		},
		"additionalProperties": func(d string) SchemaSpec {
			return SchemaSpec{Type: "object", AdditionalProperties: &SchemaSpec{Type: "string", Description: d}}
		},
		"oneOf variant": func(d string) SchemaSpec {
			return SchemaSpec{OneOf: []SchemaSpec{{Type: "string", Description: d}}}
		},
		"anyOf variant": func(d string) SchemaSpec {
			return SchemaSpec{AnyOf: []SchemaSpec{{Type: "string", Description: d}}}
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			if !sameSchemaShape(build("First wording."), build("Second wording.")) {
				t.Error("descriptions differ but shapes match; want same shape")
			}
		})
	}

	t.Run("a real shape difference is still detected", func(t *testing.T) {
		if sameSchemaShape(SchemaSpec{Type: "array", Items: &SchemaSpec{Type: "string"}},
			SchemaSpec{Type: "array", Items: &SchemaSpec{Type: "integer"}}) {
			t.Error("array element types differ; want different shape")
		}
	})

	t.Run("an empty variant slice compares equal to none at all", func(t *testing.T) {
		if !sameSchemaShape(SchemaSpec{Type: "string", OneOf: []SchemaSpec{}}, SchemaSpec{Type: "string"}) {
			t.Error("an empty OneOf should normalize to nil")
		}
	})

	t.Run("pathological nesting hits the depth backstop without recursing forever", func(t *testing.T) {
		build := func() SchemaSpec {
			s := SchemaSpec{Type: "string"}
			for i := 0; i < maxSchemaDepth+5; i++ {
				inner := s
				s = SchemaSpec{Type: "array", Items: &inner}
			}
			return s
		}
		if !sameSchemaShape(build(), build()) {
			t.Error("identical deep schemas should compare equal")
		}
	})
}
