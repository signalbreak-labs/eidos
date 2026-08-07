package parser

import (
	"encoding/json"
	"reflect"
	"testing"
)

// fullSpec returns a Spec populated with every major node type. It exercises
// info, servers, paths, operations, components, securitySchemes, webhooks,
// and source locations.
func fullSpec() *Spec {
	return &Spec{
		OpenAPI: "3.1.0",
		Info: &Info{
			Title:          "Eidos Test API",
			Description:    "A spec used for round-trip tests.",
			TermsOfService: "https://example.com/tos",
			Contact: &Contact{
				Name:  "API Support",
				URL:   "https://example.com/support",
				Email: "support@example.com",
				SourceLocation: SourceLocation{
					File: "api.yaml",
					Line: 8,
					Path: "/info/contact",
				},
			},
			License: &License{
				Name:       "Apache-2.0",
				Identifier: "Apache-2.0",
				URL:        "https://www.apache.org/licenses/LICENSE-2.0",
				SourceLocation: SourceLocation{
					File: "api.yaml",
					Line: 12,
					Path: "/info/license",
				},
			},
			Version: "1.0.0",
			SourceLocation: SourceLocation{
				File: "api.yaml",
				Line: 2,
				Path: "/info",
			},
		},
		Servers: []Server{
			{
				URL:         "https://api.example.com/{version}",
				Description: "Production server",
				Variables: map[string]*ServerVariable{
					"version": {
						Enum:    []string{"v1", "v2"},
						Default: "v1",
						SourceLocation: SourceLocation{
							File: "api.yaml",
							Line: 18,
							Path: "/servers/0/variables/version",
						},
					},
				},
				SourceLocation: SourceLocation{
					File: "api.yaml",
					Line: 15,
					Path: "/servers/0",
				},
			},
		},
		Paths: map[string]*PathItem{
			"/pets": {
				Summary:     "Pets",
				Description: "Operations on the pet collection.",
				Get: &Operation{
					OperationID: "listPets",
					Summary:     "List pets",
					Tags:        []string{"pets"},
					Parameters: []Parameter{
						{
							Name:     "limit",
							In:       "query",
							Required: false,
							Schema: &Schema{
								Type:   "integer",
								Format: "int32",
								SourceLocation: SourceLocation{
									File: "api.yaml",
									Line: 32,
									Path: "/paths/~1pets/get/parameters/0/schema",
								},
							},
							SourceLocation: SourceLocation{
								File: "api.yaml",
								Line: 28,
								Path: "/paths/~1pets/get/parameters/0",
							},
						},
					},
					Responses: map[string]*Response{
						"200": {
							Description: "A list of pets.",
							Content: map[string]*MediaType{
								"application/json": {
									Schema: &Schema{
										Type: "array",
										Items: &Schema{
											Ref: "#/components/schemas/Pet",
											SourceLocation: SourceLocation{
												File: "api.yaml",
												Line: 42,
												Path: "/paths/~1pets/get/responses/200/content/application~1json/schema/items",
											},
										},
										SourceLocation: SourceLocation{
											File: "api.yaml",
											Line: 41,
											Path: "/paths/~1pets/get/responses/200/content/application~1json/schema",
										},
									},
									SourceLocation: SourceLocation{
										File: "api.yaml",
										Line: 40,
										Path: "/paths/~1pets/get/responses/200/content/application~1json",
									},
								},
							},
							SourceLocation: SourceLocation{
								File: "api.yaml",
								Line: 38,
								Path: "/paths/~1pets/get/responses/200",
							},
						},
					},
					SourceLocation: SourceLocation{
						File: "api.yaml",
						Line: 25,
						Path: "/paths/~1pets/get",
					},
				},
				SourceLocation: SourceLocation{
					File: "api.yaml",
					Line: 22,
					Path: "/paths/~1pets",
				},
				Post: &Operation{
					OperationID: "createPet",
					Summary:     "Create a pet",
					Tags:        []string{"pets"},
					RequestBody: &RequestBody{
						Required: true,
						Content: map[string]*MediaType{
							"application/json": {
								Schema: &Schema{
									Ref: "#/components/schemas/Pet",
									SourceLocation: SourceLocation{
										File: "api.yaml",
										Line: 46,
										Path: "/paths/~1pets/post/requestBody/content/application~1json/schema",
									},
								},
								SourceLocation: SourceLocation{
									File: "api.yaml",
									Line: 45,
									Path: "/paths/~1pets/post/requestBody/content/application~1json",
								},
							},
						},
						SourceLocation: SourceLocation{
							File: "api.yaml",
							Line: 44,
							Path: "/paths/~1pets/post/requestBody",
						},
					},
					Responses: map[string]*Response{
						"201": {
							Description: "Created pet.",
							SourceLocation: SourceLocation{
								File: "api.yaml",
								Line: 48,
								Path: "/paths/~1pets/post/responses/201",
							},
						},
					},
					Callbacks: map[string]Callback{
						"petEvent": {
							"{$request.body#/callbackUrl}": {
								Post: &Operation{
									OperationID: "onPetEvent",
									Responses: map[string]*Response{
										"200": {
											Description: "event acknowledged",
										},
									},
								},
							},
						},
					},
					SourceLocation: SourceLocation{
						File: "api.yaml",
						Line: 23,
						Path: "/paths/~1pets/post",
					},
				},
			},
		},
		Webhooks: map[string]*PathItem{
			"newPet": {
				Post: &Operation{
					OperationID: "receiveNewPet",
					Summary:     "Receive a new pet event",
					RequestBody: &RequestBody{
						Required: true,
						Content: map[string]*MediaType{
							"application/json": {
								Schema: &Schema{
									Ref: "#/components/schemas/Pet",
									SourceLocation: SourceLocation{
										File: "api.yaml",
										Line: 55,
										Path: "/webhooks/newPet/post/requestBody/content/application~1json/schema",
									},
								},
								SourceLocation: SourceLocation{
									File: "api.yaml",
									Line: 54,
									Path: "/webhooks/newPet/post/requestBody/content/application~1json",
								},
							},
						},
						SourceLocation: SourceLocation{
							File: "api.yaml",
							Line: 52,
							Path: "/webhooks/newPet/post/requestBody",
						},
					},
					SourceLocation: SourceLocation{
						File: "api.yaml",
						Line: 50,
						Path: "/webhooks/newPet/post",
					},
				},
				SourceLocation: SourceLocation{
					File: "api.yaml",
					Line: 49,
					Path: "/webhooks/newPet",
				},
			},
		},
		Components: &Components{
			Schemas: map[string]*Schema{
				"Pet": {
					Type:     "object",
					Required: []string{"id", "name"},
					Properties: map[string]*Schema{
						"id": {
							Type:     "integer",
							Format:   "int64",
							ReadOnly: true,
							SourceLocation: SourceLocation{
								File: "api.yaml",
								Line: 64,
								Path: "/components/schemas/Pet/properties/id",
							},
						},
						"name": {
							Type: "string",
							SourceLocation: SourceLocation{
								File: "api.yaml",
								Line: 66,
								Path: "/components/schemas/Pet/properties/name",
							},
						},
					},
					SourceLocation: SourceLocation{
						File: "api.yaml",
						Line: 61,
						Path: "/components/schemas/Pet",
					},
				},
			},
			Parameters: map[string]*Parameter{
				"PetId": {
					Name:     "petId",
					In:       "path",
					Required: true,
					Schema: &Schema{
						Type:   "integer",
						Format: "int64",
						SourceLocation: SourceLocation{
							File: "api.yaml",
							Line: 74,
							Path: "/components/parameters/PetId/schema",
						},
					},
					SourceLocation: SourceLocation{
						File: "api.yaml",
						Line: 72,
						Path: "/components/parameters/PetId",
					},
				},
			},
			SecuritySchemes: map[string]*SecurityScheme{
				"apiKey": {
					Type:        "apiKey",
					Description: "API key authentication",
					Name:        "X-API-Key",
					In:          "header",
					SourceLocation: SourceLocation{
						File: "api.yaml",
						Line: 80,
						Path: "/components/securitySchemes/apiKey",
					},
				},
				"oauth": {
					Type: "oauth2",
					Flows: &OAuthFlows{
						AuthorizationCode: &OAuthFlow{
							AuthorizationURL: "https://example.com/oauth/authorize",
							TokenURL:         "https://example.com/oauth/token",
							Scopes: map[string]string{
								"read:pets":  "Read pets",
								"write:pets": "Write pets",
							},
							SourceLocation: SourceLocation{
								File: "api.yaml",
								Line: 86,
								Path: "/components/securitySchemes/oauth/flows/authorizationCode",
							},
						},
						SourceLocation: SourceLocation{
							File: "api.yaml",
							Line: 84,
							Path: "/components/securitySchemes/oauth/flows",
						},
					},
					SourceLocation: SourceLocation{
						File: "api.yaml",
						Line: 82,
						Path: "/components/securitySchemes/oauth",
					},
				},
			},
			SourceLocation: SourceLocation{
				File: "api.yaml",
				Line: 59,
				Path: "/components",
			},
		},
		Security: []SecurityRequirement{
			{
				Requirements: map[string][]string{"apiKey": {}},
				SourceLocation: SourceLocation{
					File: "api.yaml",
					Line: 91,
					Path: "/security/0",
				},
			},
		},
		Tags: []Tag{
			{
				Name:        "pets",
				Description: "Operations about pets",
				SourceLocation: SourceLocation{
					File: "api.yaml",
					Line: 93,
					Path: "/tags/0",
				},
			},
		},
		ExternalDocs: &ExternalDocs{
			Description: "Find more info here",
			URL:         "https://example.com/docs",
			SourceLocation: SourceLocation{
				File: "api.yaml",
				Line: 97,
				Path: "/externalDocs",
			},
		},
		SourceLocation: SourceLocation{
			File: "api.yaml",
			Line: 1,
			Path: "",
		},
	}
}

// TestSpecJSONRoundTrip marshals a fully populated Spec to JSON and back,
// asserting that the recovered value equals the original.
func TestSpecJSONRoundTrip(t *testing.T) {
	original := fullSpec()

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}

	var got Spec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}

	if !reflect.DeepEqual(&got, original) {
		t.Fatalf("round-trip mismatch:\njson: %s\n got: %+v\nwant: %+v", string(data), &got, original)
	}
}

// TestSpecJSONRoundTripFromRawJSON verifies that a JSON document encoding a
// Spec can be unmarshaled and re-marshaled without data loss.
func TestSpecJSONRoundTripFromRawJSON(t *testing.T) {
	original := fullSpec()

	first, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("first marshal: %v", err)
	}

	var intermediate Spec
	if err := json.Unmarshal(first, &intermediate); err != nil {
		t.Fatalf("first unmarshal: %v", err)
	}

	second, err := json.Marshal(&intermediate)
	if err != nil {
		t.Fatalf("second marshal: %v", err)
	}

	var got Spec
	if err := json.Unmarshal(second, &got); err != nil {
		t.Fatalf("second unmarshal: %v", err)
	}

	if !reflect.DeepEqual(&got, original) {
		t.Fatalf("double round-trip mismatch")
	}
}

// TestSpecEmptyRoundTrip ensures an empty Spec survives a JSON round-trip.
func TestSpecEmptyRoundTrip(t *testing.T) {
	original := &Spec{}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal empty spec: %v", err)
	}

	var got Spec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal empty spec: %v", err)
	}

	if !reflect.DeepEqual(&got, original) {
		t.Fatalf("empty round-trip mismatch")
	}
}

// TestSourceLocationPreserved checks that SourceLocation fields survive
// serialization, since they are essential for diagnostics.
func TestSourceLocationPreserved(t *testing.T) {
	original := &Spec{
		OpenAPI: "3.0.3",
		Info: &Info{
			Title:   "Loc Test",
			Version: "1",
			SourceLocation: SourceLocation{
				File:   "loc.yaml",
				Line:   4,
				Column: 2,
				Path:   "/info",
			},
		},
		SourceLocation: SourceLocation{
			File:   "loc.yaml",
			Line:   1,
			Column: 1,
			Path:   "",
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Spec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !reflect.DeepEqual(&got, original) {
		t.Fatalf("source location not preserved: got %+v want %+v", &got, original)
	}
}

// TestCallbackRoundTrip verifies that Callback serializes as a map keyed by
// runtime expressions (or "$ref"), matching the OpenAPI callback shape.
func TestCallbackRoundTrip(t *testing.T) {
	original := &Spec{
		OpenAPI: "3.0.3",
		Paths: map[string]*PathItem{
			"/pets/{petId}": {
				Post: &Operation{
					OperationID: "createPet",
					Callbacks: map[string]Callback{
						"petEvent": {
							"{$request.body#/callbackUrl}": {
								Post: &Operation{
									OperationID: "onPetEvent",
									Responses: map[string]*Response{
										"200": {
											Description: "event acknowledged",
										},
									},
								},
							},
						},
						"refCallback": {
							"$ref": {
								Ref: "#/components/callbacks/Shared",
							},
						},
					},
				},
			},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal spec with callbacks: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal into raw map: %v", err)
	}

	paths, ok := raw["paths"].(map[string]any)
	if !ok {
		t.Fatalf("paths missing or not an object")
	}
	pathItem, ok := paths["/pets/{petId}"].(map[string]any)
	if !ok {
		t.Fatalf("path item missing or not an object")
	}
	op, ok := pathItem["post"].(map[string]any)
	if !ok {
		t.Fatalf("post operation missing or not an object")
	}
	callbacks, ok := op["callbacks"].(map[string]any)
	if !ok {
		t.Fatalf("callbacks missing or not an object")
	}
	petEvent, ok := callbacks["petEvent"].(map[string]any)
	if !ok {
		t.Fatalf("petEvent callback missing or not an object")
	}
	if _, ok := petEvent["{$request.body#/callbackUrl}"]; !ok {
		t.Fatalf("runtime expression key missing in callback JSON: %v", petEvent)
	}
	if _, hasPathItems := petEvent["pathItems"]; hasPathItems {
		t.Fatalf("callback serialized with struct key 'pathItems', want map shape: %v", petEvent)
	}

	refCallback, ok := callbacks["refCallback"].(map[string]any)
	if !ok {
		t.Fatalf("refCallback missing or not an object")
	}
	refValue, ok := refCallback["$ref"].(string)
	if !ok {
		t.Fatalf("$ref callback entry missing or not a string")
	}
	if refValue != "#/components/callbacks/Shared" {
		t.Fatalf("$ref callback value mismatch: %v", refValue)
	}

	var got Spec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}

	if !reflect.DeepEqual(&got, original) {
		t.Fatalf("callback round-trip mismatch:\n got: %+v\nwant: %+v", &got, original)
	}
}

// TestSecurityRequirementRoundTrip verifies that SecurityRequirement
// marshals to the flat OpenAPI shape and that both Requirements and
// SourceLocation survive an unmarshal round-trip.
func TestSecurityRequirementRoundTrip(t *testing.T) {
	original := SecurityRequirement{
		Requirements: map[string][]string{
			"apiKey": {},
			"oauth":  {"read:pets", "write:pets"},
		},
		SourceLocation: SourceLocation{
			File:   "api.yaml",
			Line:   42,
			Column: 4,
			Path:   "/security/0",
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal security requirement: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal into raw map: %v", err)
	}
	if _, ok := raw["apiKey"]; !ok {
		t.Fatalf("apiKey requirement missing in JSON: %v", raw)
	}
	if scopes, ok := raw["oauth"].([]any); !ok || len(scopes) != 2 {
		t.Fatalf("oauth scopes mismatch in JSON: %v", raw["oauth"])
	}
	if _, ok := raw["sourceLocation"]; !ok {
		t.Fatalf("sourceLocation missing in JSON: %v", raw)
	}

	var got SecurityRequirement
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal security requirement: %v", err)
	}

	if !reflect.DeepEqual(got, original) {
		t.Fatalf("security requirement round-trip mismatch:\n got: %+v\nwant: %+v", got, original)
	}
}

// TestSecurityRequirementEmptyRequirementsWithSourceLocation verifies the
// documented edge case where Requirements is empty but SourceLocation is
// non-zero. The resulting JSON contains only sourceLocation, and the
// round-trip preserves the empty requirement map and its source location.
func TestSecurityRequirementEmptyRequirementsWithSourceLocation(t *testing.T) {
	original := SecurityRequirement{
		Requirements: map[string][]string{},
		SourceLocation: SourceLocation{
			File:   "api.yaml",
			Line:   42,
			Column: 4,
			Path:   "/security/0",
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal security requirement: %v", err)
	}

	want := `{"sourceLocation":{"file":"api.yaml","line":42,"column":4,"path":"/security/0"}}`
	if string(data) != want {
		t.Fatalf("marshaled JSON mismatch:\n got: %s\nwant: %s", data, want)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal into raw map: %v", err)
	}
	if _, ok := raw["sourceLocation"]; !ok {
		t.Fatalf("sourceLocation missing in JSON: %v", raw)
	}
	for k := range raw {
		if k != "sourceLocation" {
			t.Fatalf("unexpected key in JSON: %q", k)
		}
	}

	var got SecurityRequirement
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal security requirement: %v", err)
	}

	if !reflect.DeepEqual(got, original) {
		t.Fatalf("security requirement round-trip mismatch:\n got: %+v\nwant: %+v", got, original)
	}
}

// TestSecurityRequirementInSpecRoundTrip verifies that a Spec containing a
// Security field round-trips through JSON without losing Requirements or
// SourceLocation entries.
func TestSecurityRequirementInSpecRoundTrip(t *testing.T) {
	original := &Spec{
		OpenAPI: "3.0.3",
		Security: []SecurityRequirement{
			{
				Requirements: map[string][]string{
					"bearer": {"read", "write"},
				},
				SourceLocation: SourceLocation{
					File: "api.yaml",
					Line: 10,
					Path: "/security/0",
				},
			},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}

	var got Spec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}

	if !reflect.DeepEqual(&got, original) {
		t.Fatalf("spec security round-trip mismatch:\n got: %+v\nwant: %+v", &got, original)
	}
}

// TestSchemaUnionFieldsRoundTrip exercises OpenAPI 3.1-only JSON Schema
// fields that are part of the version-agnostic union (type arrays,
// additionalProperties as schema/bool, unevaluatedProperties, contains).
// Because Schema.Type is typed as `any` to hold either a string or a
// []string, a struct DeepEqual would see a []string become []any after
// JSON unmarshal. The JSON round-trip itself is verified by comparing
// the serialized bytes of the original and the re-serialized document.
func TestSchemaUnionFieldsRoundTrip(t *testing.T) {
	original := &Spec{
		OpenAPI: "3.1.0",
		Components: &Components{
			Schemas: map[string]*Schema{
				"Union": {
					Type: []string{"string", "null"},
					Properties: map[string]*Schema{
						"tags": {
							Type:        "array",
							Items:       &Schema{Type: "string"},
							Contains:    &Schema{Type: "string"},
							MinContains: 1,
							MaxContains: 5,
						},
						"extra": {
							Type:                  "object",
							AdditionalProperties:  false,
							UnevaluatedProperties: &Schema{Type: "boolean"},
						},
					},
				},
			},
		},
	}

	first, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var intermediate Spec
	if err := json.Unmarshal(first, &intermediate); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	second, err := json.Marshal(&intermediate)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("schema union JSON round-trip mismatch:\nfirst:  %s\nsecond: %s", string(first), string(second))
	}
}

// TestExtensionsRoundTrip verifies that OpenAPI vendor extensions (`x-*`
// keys) on model structs are preserved across JSON marshal/unmarshal.
func TestExtensionsRoundTrip(t *testing.T) {
	original := &Spec{
		OpenAPI: "3.1.0",
		Info: &Info{
			Title:   "Extension Test",
			Version: "1.0.0",
			Extensions: map[string]any{
				"x-api-owner":      "platform-team",
				"x-legacy-version": float64(2),
			},
		},
		Paths: map[string]*PathItem{
			"/pets": {
				Extensions: map[string]any{
					"x-path-notes": "experimental",
				},
				Get: &Operation{
					OperationID: "listPets",
					Extensions: map[string]any{
						"x-rate-limit": float64(100),
					},
					Responses: map[string]*Response{
						"200": {
							Description: "OK",
							Extensions: map[string]any{
								"x-response-hint": "paginated",
							},
						},
					},
				},
			},
		},
		Components: &Components{
			Schemas: map[string]*Schema{
				"Pet": {
					Type: "object",
					Extensions: map[string]any{
						"x-schema-kind": "entity",
					},
				},
			},
			Extensions: map[string]any{
				"x-components-meta": "v1",
			},
		},
		Extensions: map[string]any{
			"x-document-level": true,
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to raw: %v", err)
	}
	if raw["x-document-level"] != true {
		t.Fatalf("top-level extension missing in JSON: %v", raw["x-document-level"])
	}
	info := raw["info"].(map[string]any)
	if info["x-api-owner"] != "platform-team" {
		t.Fatalf("info extension missing: %v", info["x-api-owner"])
	}

	var got Spec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !reflect.DeepEqual(&got, original) {
		t.Fatalf("extension round-trip mismatch:\n got: %+v\nwant: %+v", &got, original)
	}
}

// TestSchemaOpenAPI31FieldsRoundTrip exercises the remaining OpenAPI 3.1 JSON
// Schema fields that are part of the version-agnostic union but were not
// covered by TestSchemaUnionFieldsRoundTrip.
func TestSchemaOpenAPI31FieldsRoundTrip(t *testing.T) {
	original := &Spec{
		OpenAPI: "3.1.0",
		Components: &Components{
			Schemas: map[string]*Schema{
				"Full31": {
					Type:          "object",
					PropertyNames: &Schema{Type: "string", MinLength: 1},
					PrefixItems:   []*Schema{{Type: "string"}, {Type: "integer"}},
					If:            &Schema{Required: []string{"name"}},
					Then:          &Schema{Required: []string{"id"}},
					Else:          &Schema{Required: []string{"alias"}},
					DependentSchemas: map[string]*Schema{
						"name": {Type: "string"},
					},
					DependentRequired: map[string][]string{
						"name": {"id"},
					},
					Const:            "fixed-value",
					ContentMediaType: "application/json",
					ContentEncoding:  "base64",
					UnevaluatedItems: &Schema{Type: "boolean"},
				},
			},
		},
	}

	first, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var intermediate Spec
	if err := json.Unmarshal(first, &intermediate); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	second, err := json.Marshal(&intermediate)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("schema 3.1 JSON round-trip mismatch:\nfirst:  %s\nsecond: %s", string(first), string(second))
	}
}

// TestSchemaItemsUnmarshalFallback verifies that Schema.UnmarshalJSON converts
// an "items" object back into a *Schema after the generic JSON unmarshal has
// represented it as map[string]any. This is the happy path of the Items
// normalization fallback; if json.Marshal or json.Unmarshal of that map fails,
// the field is left as map[string]any rather than failing the whole unmarshal.
func TestSchemaItemsUnmarshalFallback(t *testing.T) {
	data := []byte(`{"type":"array","items":{"type":"string","description":"an item"}}`)

	var s Schema
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}

	itemsSchema, ok := s.Items.(*Schema)
	if !ok {
		t.Fatalf("expected Items to be normalized to *Schema, got %T: %v", s.Items, s.Items)
	}
	if itemsSchema.Type != "string" || itemsSchema.Description != "an item" {
		t.Fatalf("normalized items schema mismatch: %+v", itemsSchema)
	}

	// Boolean items are preserved as-is; the fallback only applies to objects.
	boolData := []byte(`{"type":"array","items":true}`)
	var boolSchema Schema
	if err := json.Unmarshal(boolData, &boolSchema); err != nil {
		t.Fatalf("unmarshal boolean items schema: %v", err)
	}
	if itemsTrue, ok := boolSchema.Items.(bool); !ok || !itemsTrue {
		t.Fatalf("expected boolean items true, got %v (%T)", boolSchema.Items, boolSchema.Items)
	}
}

// TestSchemaAdditionalPropertiesUnmarshalFallback asserts that a schema-valued
// additionalProperties is normalized to *Schema on unmarshal, mirroring Items,
// so the two `any`-typed fields round-trip symmetrically (L-84).
func TestSchemaAdditionalPropertiesUnmarshalFallback(t *testing.T) {
	data := []byte(`{"type":"object","additionalProperties":{"type":"string","description":"extra"}}`)

	var s Schema
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}

	apSchema, ok := s.AdditionalProperties.(*Schema)
	if !ok {
		t.Fatalf("expected AdditionalProperties to be normalized to *Schema, got %T: %v", s.AdditionalProperties, s.AdditionalProperties)
	}
	if apSchema.Type != "string" || apSchema.Description != "extra" {
		t.Fatalf("normalized additionalProperties schema mismatch: %+v", apSchema)
	}

	// Boolean additionalProperties are preserved as-is.
	boolData := []byte(`{"type":"object","additionalProperties":false}`)
	var boolSchema Schema
	if err := json.Unmarshal(boolData, &boolSchema); err != nil {
		t.Fatalf("unmarshal boolean additionalProperties schema: %v", err)
	}
	if apFalse, ok := boolSchema.AdditionalProperties.(bool); !ok || apFalse {
		t.Fatalf("expected boolean additionalProperties false, got %v (%T)", boolSchema.AdditionalProperties, boolSchema.AdditionalProperties)
	}
}
