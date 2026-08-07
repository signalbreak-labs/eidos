package ir

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func intPtr(v int) *int           { return &v }
func int64Ptr(v int64) *int64     { return &v }
func floatPtr(v float64) *float64 { return &v }
func anyPtr(v any) *any           { return &v }

func assertJSONRoundTrip[V any](t *testing.T, value V) {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal(%T) error: %v", value, err)
	}

	var decoded V
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(%T) error: %v", value, err)
	}

	if !reflect.DeepEqual(value, decoded) {
		t.Fatalf("round-trip mismatch for %T:\noriginal: %#v\ndecoded:  %#v\njson: %s", value, value, decoded, string(data))
	}
}

func TestPrimitiveTypeRoundTrip(t *testing.T) {
	for _, pt := range []PrimitiveType{TypeString, TypeInt, TypeFloat, TypeBool, TypeNull, TypeDynamic} {
		assertJSONRoundTrip(t, pt)
	}
}

func TestCollectionTypeRoundTrip(t *testing.T) {
	assertJSONRoundTrip(t, CollectionType{
		Kind: List,
		ElementType: SchemaIR{
			Type:        TypeString,
			Description: "list element",
		},
	})
	assertJSONRoundTrip(t, CollectionType{
		Kind: Set,
		ElementType: SchemaIR{
			Type:    TypeInt,
			Minimum: floatPtr(0),
			Maximum: floatPtr(100),
		},
	})
	assertJSONRoundTrip(t, CollectionType{
		Kind: Map,
		ElementType: SchemaIR{
			Type: TypeBool,
		},
	})
}

func TestCollectionKindRoundTrip(t *testing.T) {
	assertJSONRoundTrip(t, List)
	assertJSONRoundTrip(t, Set)
	assertJSONRoundTrip(t, Map)
}

func TestDiscriminatorIRRoundTrip(t *testing.T) {
	assertJSONRoundTrip(t, DiscriminatorIR{
		PropertyName: "petType",
		Mapping: map[string]string{
			"cat": "Cat",
			"dog": "Dog",
		},
	})
}

func TestUnionTypeRoundTrip(t *testing.T) {
	assertJSONRoundTrip(t, UnionType{
		Variants: []SchemaIR{
			{Type: TypeString, Name: "text"},
			{Type: TypeInt, Name: "count"},
		},
	})
	assertJSONRoundTrip(t, UnionType{
		Variants: []SchemaIR{
			{Name: "Cat", Type: TypeString},
			{Name: "Dog", Type: TypeString},
		},
		Discriminator: &DiscriminatorIR{
			PropertyName: "kind",
			Mapping: map[string]string{
				"feline": "Cat",
				"canine": "Dog",
			},
		},
	})
}

func TestSchemaIRRoundTrip(t *testing.T) {
	assertJSONRoundTrip(t, SchemaIR{
		Name:        "empty",
		Description: "A schema with no semantic content.",
	})

	assertJSONRoundTrip(t, SchemaIR{
		Name:        "string_attr",
		Description: "A required string attribute",
		Type:        TypeString,
		Required:    true,
		Optional:    false,
		Computed:    false,
		Sensitive:   true,
		Format:      "uuid",
		Pattern:     "^[0-9a-f-]+$",
		MinLength:   intPtr(1),
		MaxLength:   intPtr(36),
		SourceLocation: &SourceLocation{
			File: "spec.yaml",
			Line: 42,
		},
	})

	// A collection schema carries its element type on Collection.ElementType,
	// not on the outer schema's Type; setting both is a state Validate() rejects,
	// so the outer Type is omitted here (L-65).
	assertJSONRoundTrip(t, SchemaIR{
		Name: "tags",
		Collection: &CollectionType{
			Kind: Set,
			ElementType: SchemaIR{
				Type:        TypeString,
				Description: "unique tag",
			},
		},
	})

	assertJSONRoundTrip(t, SchemaIR{
		Name:       "rating",
		Type:       TypeFloat,
		Minimum:    floatPtr(0),
		Maximum:    floatPtr(5),
		MultipleOf: floatPtr(0.5),
		Default:    anyPtr(any(2.5)),
	})

	assertJSONRoundTrip(t, SchemaIR{
		Name:       "status",
		Type:       TypeString,
		EnumValues: []any{"pending", "running", "done"},
		Const:      anyPtr(any("running")),
	})

	assertJSONRoundTrip(t, SchemaIR{
		Name:       "nested",
		Attributes: []AttributeIR{{Name: "id"}, {Name: "name"}},
		Blocks:     []BlockIR{{Name: "metadata"}},
	})

	assertJSONRoundTrip(t, SchemaIR{
		Name: "union",
		Union: &UnionType{
			Variants: []SchemaIR{
				{Name: "A", Type: TypeString},
				{Name: "B", Type: TypeInt},
			},
		},
	})

	assertJSONRoundTrip(t, SchemaIR{
		Name:       "conditional",
		Not:        &SchemaIR{Type: TypeNull},
		IfSchema:   &SchemaIR{Type: TypeBool},
		ThenSchema: &SchemaIR{Type: TypeString},
		ElseSchema: &SchemaIR{Type: TypeInt},
		DependentRequired: map[string][]string{
			"billing_address": {"payment_method"},
		},
		DependentSchemas: map[string]*SchemaIR{
			"credit_card": {Type: TypeString},
		},
		PatternProperties: map[string]*SchemaIR{
			"^x-": {Type: TypeString},
		},
		PropertyNames:         &SchemaIR{Type: TypeString, Pattern: "^[a-z]+$"},
		UnevaluatedProperties: &SchemaIR{Type: TypeBool},
		MinProperties:         intPtr(1),
		MaxProperties:         intPtr(10),
	})

	assertJSONRoundTrip(t, SchemaIR{
		Name:               "deprecated_attr",
		Deprecated:         true,
		DeprecationMessage: "Use new_attr instead.",
		WriteOnly:          true,
		Validators:         []ValidatorIR{{}},
		PlanModifiers:      []PlanModifierIR{{}},
	})

	// Nil Default/Const should round-trip as omitted fields.
	assertJSONRoundTrip(t, SchemaIR{
		Name:    "nil_default_const",
		Type:    TypeString,
		Default: nil,
		Const:   nil,
	})

	// Mix of set and unset pointer fields to lock in null-vs-omitted behavior.
	assertJSONRoundTrip(t, SchemaIR{
		Name:      "mixed_pointers",
		Type:      TypeString,
		MinLength: intPtr(1),
		Maximum:   floatPtr(100),
		MaxLength: intPtr(0), // zero value must be preserved and distinct from nil
	})
}

func TestSchemaIRValidate(t *testing.T) {
	cases := []struct {
		name       string
		schema     SchemaIR
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:    "empty schema is valid",
			schema:  SchemaIR{Name: "empty"},
			wantErr: false,
		},
		{
			name:    "primitive only is valid",
			schema:  SchemaIR{Name: "primitive", Type: TypeString},
			wantErr: false,
		},
		{
			name:    "collection only is valid",
			schema:  SchemaIR{Name: "collection", Collection: &CollectionType{Kind: List}},
			wantErr: false,
		},
		{
			name:    "union only is valid",
			schema:  SchemaIR{Name: "union", Union: &UnionType{}},
			wantErr: false,
		},
		{
			name:    "required xor optional is valid",
			schema:  SchemaIR{Name: "required", Required: true},
			wantErr: false,
		},
		{
			name:    "optional only is valid",
			schema:  SchemaIR{Name: "optional", Optional: true},
			wantErr: false,
		},
		{
			name:       "type and collection is inconsistent",
			schema:     SchemaIR{Name: "bad", Type: TypeString, Collection: &CollectionType{Kind: List}},
			wantErr:    true,
			wantErrMsg: "has both Type=\"string\" and Collection",
		},
		{
			name:       "type and union is inconsistent",
			schema:     SchemaIR{Name: "bad", Type: TypeString, Union: &UnionType{}},
			wantErr:    true,
			wantErrMsg: "has both Type=\"string\" and Union",
		},
		{
			name:       "collection and union together is inconsistent",
			schema:     SchemaIR{Name: "bad", Collection: &CollectionType{Kind: List}, Union: &UnionType{}},
			wantErr:    true,
			wantErrMsg: "has both Collection and Union",
		},
		{
			name:       "required and optional together is inconsistent",
			schema:     SchemaIR{Name: "bad", Required: true, Optional: true},
			wantErr:    true,
			wantErrMsg: "has both Required and Optional set",
		},
		{
			name:       "multiple independent violations are reported together",
			schema:     SchemaIR{Name: "bad", Type: TypeString, Collection: &CollectionType{Kind: List}, Required: true, Optional: true},
			wantErr:    true,
			wantErrMsg: "has both Type=\"string\" and Collection",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.schema.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErrMsg != "" && !strings.Contains(err.Error(), tc.wantErrMsg) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErrMsg)
			}
			if tc.name == "multiple independent violations are reported together" && !strings.Contains(err.Error(), "has both Required and Optional set") {
				t.Fatalf("multi-error should also contain Required+Optional violation, got: %v", err)
			}
		})
	}
}
