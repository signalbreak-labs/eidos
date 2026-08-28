package ir

import (
	"strings"
	"testing"
)

// TestValidateProviderIR_ValidSchema verifies that a well-formed provider IR
// (the shape the transformer produces) validates clean — the wiring added for
// N-48 must not false-positive on legitimate IR.
func TestValidateProviderIR_ValidSchema(t *testing.T) {
	provider := &ProviderIR{
		Name: "mycloud",
		ConfigSchema: ObjectSchemaIR{
			Attributes: []AttributeIR{
				{
					Name:     "api_key",
					Optional: true,
					Schema:   SchemaIR{Type: TypeString},
				},
			},
		},
		Resources: []ResourceIR{
			{
				Name: "pet",
				Schema: ObjectSchemaIR{
					Attributes: []AttributeIR{
						{Name: "id", Computed: true, Schema: SchemaIR{Type: TypeString}},
						{
							Name:     "tags",
							Optional: true,
							Schema: SchemaIR{
								Collection: &CollectionType{Kind: Set, ElementType: SchemaIR{Type: TypeString}},
							},
						},
						{
							Name:     "config",
							Optional: true,
							Schema: SchemaIR{
								Attributes: []AttributeIR{
									{Name: "mode", Optional: true, Schema: SchemaIR{Type: TypeString}},
								},
							},
						},
					},
				},
			},
		},
	}

	if errs := ValidateProviderIR(provider); len(errs) != 0 {
		t.Fatalf("expected no validation errors for well-formed IR, got: %v", errs)
	}
}

// TestValidateProviderIR_ReportsTypeAndCollection verifies that the invariant
// "Type and Collection are mutually exclusive" is enforced on nested nodes.
func TestValidateProviderIR_ReportsTypeAndCollection(t *testing.T) {
	provider := &ProviderIR{
		Name: "mycloud",
		DataSources: []DataSourceIR{
			{
				Name: "pets",
				Schema: ObjectSchemaIR{
					Attributes: []AttributeIR{
						{
							Name: "bad",
							Schema: SchemaIR{
								Type:       TypeString,
								Collection: &CollectionType{Kind: List, ElementType: SchemaIR{Type: TypeString}},
							},
						},
					},
				},
			},
		},
	}

	errs := ValidateProviderIR(provider)
	if len(errs) == 0 {
		t.Fatal("expected a validation error for Type+Collection on one node")
	}
	if !strings.Contains(errs[0].Error(), "data_sources[0].schema.attributes[0].schema") {
		t.Errorf("error path %q does not identify the offending node", errs[0])
	}
	if !strings.Contains(errs[0].Error(), "both Type") {
		t.Errorf("error %q does not describe the Type+Collection conflict", errs[0])
	}
}

// TestValidateProviderIR_RequiredAndOptional verifies the Required && Optional
// invariant is enforced on a standalone schema node (e.g. a function argument).
func TestValidateProviderIR_RequiredAndOptional(t *testing.T) {
	provider := &ProviderIR{
		Name: "mycloud",
		Functions: []FunctionIR{
			{
				Name: "echo",
				Arguments: []FunctionParamIR{
					{
						Name:   "input",
						Schema: SchemaIR{Type: TypeString, Required: true, Optional: true},
					},
				},
			},
		},
	}

	errs := ValidateProviderIR(provider)
	if len(errs) == 0 {
		t.Fatal("expected a validation error for Required && Optional")
	}
	if !strings.Contains(errs[0].Error(), "functions[0].arguments[0].schema") {
		t.Errorf("error path %q does not identify the offending node", errs[0])
	}
}

// TestValidateProviderIR_NilProvider verifies the nil-provider guard returns
// no errors instead of panicking.
func TestValidateProviderIR_NilProvider(t *testing.T) {
	if errs := ValidateProviderIR(nil); len(errs) != 0 {
		t.Fatalf("expected no errors for nil provider, got: %v", errs)
	}
}

// TestValidateProviderIR_RecursesAllNodeKinds exercises every recursion branch
// of ValidateProviderIR/validateObjectSchema/validateSchemaNode with well-formed
// IR: resource identity schemas, list-resource resource schemas, blocks,
// collection element types, union variants, JSON Schema conditional/negation
// subschemas, dependent schemas, pattern properties, property names, and
// unevaluated properties. All are valid, so the assertion is zero errors — the
// point is that each branch is reached without panicking and without
// false-positives.
func TestValidateProviderIR_RecursesAllNodeKinds(t *testing.T) {
	provider := &ProviderIR{
		Name: "mycloud",
		Resources: []ResourceIR{
			{
				Name: "pet",
				Schema: ObjectSchemaIR{
					Attributes: []AttributeIR{
						// Collection element type recursion.
						{
							Name:     "tags",
							Optional: true,
							Schema: SchemaIR{
								Collection: &CollectionType{Kind: Set, ElementType: SchemaIR{Type: TypeString}},
							},
						},
						// Union variant recursion.
						{
							Name:     "shape",
							Optional: true,
							Schema: SchemaIR{
								Union: &UnionType{
									Kind: OneOf,
									Variants: []SchemaIR{
										{Type: TypeString},
										{Type: TypeFloat},
									},
								},
							},
						},
						// Not / If / Then / Else subschema recursion.
						{
							Name:     "conditional",
							Optional: true,
							Schema: SchemaIR{
								Not:        &SchemaIR{Type: TypeString},
								IfSchema:   &SchemaIR{Type: TypeString},
								ThenSchema: &SchemaIR{Type: TypeString},
								ElseSchema: &SchemaIR{Type: TypeString},
							},
						},
						// DependentSchemas (sorted-key iteration) and
						// PatternProperties recursion.
						{
							Name:     "dependent",
							Optional: true,
							Schema: SchemaIR{
								DependentSchemas: map[string]*SchemaIR{
									"b": {Type: TypeString},
									"a": {Type: TypeString},
								},
								PatternProperties: map[string]*SchemaIR{
									"^x-": {Type: TypeString},
								},
							},
						},
						// PropertyNames and UnevaluatedProperties recursion.
						{
							Name:     "props",
							Optional: true,
							Schema: SchemaIR{
								PropertyNames:         &SchemaIR{Type: TypeString},
								UnevaluatedProperties: &SchemaIR{Type: TypeString},
							},
						},
						// Blocks directly on a schema node (validateSchemaNode
						// → Blocks, distinct from validateObjectSchema → Blocks).
						{
							Name:     "nested",
							Optional: true,
							Schema: SchemaIR{
								Blocks: []BlockIR{
									{
										Name: "inner",
										Schema: ObjectSchemaIR{
											Attributes: []AttributeIR{
												{Name: "mode", Optional: true, Schema: SchemaIR{Type: TypeString}},
											},
										},
									},
								},
							},
						},
					},
					// Block recursion (validateObjectSchema → Blocks).
					Blocks: []BlockIR{
						{
							Name: "settings",
							Schema: ObjectSchemaIR{
								Attributes: []AttributeIR{
									{Name: "mode", Optional: true, Schema: SchemaIR{Type: TypeString}},
								},
							},
						},
					},
				},
				// Resource identity schema recursion.
				IdentitySchema: &ObjectSchemaIR{
					Attributes: []AttributeIR{
						{Name: "id", Computed: true, Schema: SchemaIR{Type: TypeString}},
					},
				},
			},
		},
		// Action config-schema recursion.
		Actions: []ActionIR{
			{
				Name: "reboot",
				ConfigSchema: ObjectSchemaIR{
					Attributes: []AttributeIR{
						{Name: "force", Optional: true, Schema: SchemaIR{Type: TypeBool}},
					},
				},
			},
		},
		// Ephemeral config + result schema recursion.
		EphemeralResources: []EphemeralResourceIR{
			{
				Name:         "session",
				ConfigSchema: ObjectSchemaIR{},
				ResultSchema: ObjectSchemaIR{
					Attributes: []AttributeIR{
						{Name: "token", Computed: true, Schema: SchemaIR{Type: TypeString}},
					},
				},
			},
		},
		ListResources: []ListResourceIR{
			{
				Name: "pets",
				ConfigSchema: ObjectSchemaIR{
					Attributes: []AttributeIR{
						{Name: "limit", Optional: true, Schema: SchemaIR{Type: TypeFloat}},
					},
				},
				IdentitySchema: ObjectSchemaIR{
					Attributes: []AttributeIR{
						{Name: "id", Computed: true, Schema: SchemaIR{Type: TypeString}},
					},
				},
				// List-resource resource schema recursion.
				ResourceSchema: &ObjectSchemaIR{
					Attributes: []AttributeIR{
						{Name: "name", Optional: true, Schema: SchemaIR{Type: TypeString}},
					},
				},
			},
		},
	}

	if errs := ValidateProviderIR(provider); len(errs) != 0 {
		t.Fatalf("expected no validation errors for well-formed IR, got: %v", errs)
	}
}

// TestValidateProviderIR_ErrorPropagatesFromDeepNode verifies that an invariant
// violation buried inside a nested block is surfaced with a path that identifies
// the offending node — proving the recursion actually descends into blocks.
func TestValidateProviderIR_ErrorPropagatesFromDeepNode(t *testing.T) {
	provider := &ProviderIR{
		Name: "mycloud",
		Resources: []ResourceIR{
			{
				Name: "pet",
				Schema: ObjectSchemaIR{
					Blocks: []BlockIR{
						{
							Name: "settings",
							Schema: ObjectSchemaIR{
								Attributes: []AttributeIR{
									{
										Name: "bad",
										Schema: SchemaIR{
											Type:       TypeString,
											Collection: &CollectionType{Kind: List, ElementType: SchemaIR{Type: TypeString}},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	errs := ValidateProviderIR(provider)
	if len(errs) == 0 {
		t.Fatal("expected a validation error for Type+Collection on a nested block attribute")
	}
	if !strings.Contains(errs[0].Error(), "resources[0].schema.blocks[0].schema.attributes[0].schema") {
		t.Errorf("error path %q does not identify the deep offending node", errs[0])
	}
}
