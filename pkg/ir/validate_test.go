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
