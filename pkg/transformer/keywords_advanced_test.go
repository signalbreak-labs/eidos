package transformer

import (
	"reflect"
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

func TestApplyAdvancedKeywords_ConstString(t *testing.T) {
	s := &Schema{Type: SchemaTypeString, Const: "fixed"}
	target := &ir.SchemaIR{Type: ir.TypeString}

	if err := ApplyAdvancedKeywords(s, target, nil); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	if target.Const == nil || *target.Const != "fixed" {
		t.Errorf("Const = %v, want pointer to \"fixed\"", target.Const)
	}

	want := []ir.ValidatorIR{{Type: "stringvalidator.OneOf", Args: []string{"fixed"}}}
	if !reflect.DeepEqual(target.Validators, want) {
		t.Errorf("Validators = %v, want %v", target.Validators, want)
	}
}

func TestApplyAdvancedKeywords_ConstInt(t *testing.T) {
	s := &Schema{Type: SchemaTypeInteger, Const: float64(42)}
	target := &ir.SchemaIR{Type: ir.TypeInt}

	if err := ApplyAdvancedKeywords(s, target, nil); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	if target.Const == nil || (*target.Const).(float64) != 42 {
		t.Errorf("Const = %v, want pointer to 42", target.Const)
	}

	want := []ir.ValidatorIR{{Type: "int64validator.OneOf", Args: []string{"42"}}}
	if !reflect.DeepEqual(target.Validators, want) {
		t.Errorf("Validators = %v, want %v", target.Validators, want)
	}
}

func TestApplyAdvancedKeywords_ConstFloat(t *testing.T) {
	s := &Schema{Type: SchemaTypeNumber, Const: 3.14}
	target := &ir.SchemaIR{Type: ir.TypeFloat}

	if err := ApplyAdvancedKeywords(s, target, nil); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	if target.Const == nil || (*target.Const).(float64) != 3.14 {
		t.Errorf("Const = %v, want pointer to 3.14", target.Const)
	}

	want := []ir.ValidatorIR{{Type: "float64validator.OneOf", Args: []string{"3.14"}}}
	if !reflect.DeepEqual(target.Validators, want) {
		t.Errorf("Validators = %v, want %v", target.Validators, want)
	}
}

func TestApplyAdvancedKeywords_ConstBool(t *testing.T) {
	s := &Schema{Type: SchemaTypeBoolean, Const: true}
	target := &ir.SchemaIR{Type: ir.TypeBool}

	if err := ApplyAdvancedKeywords(s, target, nil); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	if target.Const == nil || *target.Const != true {
		t.Errorf("Const = %v, want pointer to true", target.Const)
	}

	if len(target.Validators) != 0 {
		t.Errorf("Validators = %v, want empty (no bool OneOf validator)", target.Validators)
	}
}

func TestApplyAdvancedKeywords_ConstMismatchedType(t *testing.T) {
	// A string const on an integer schema should still record Const but not
	// emit a mismatched validator.
	s := &Schema{Type: SchemaTypeInteger, Const: "oops"}
	target := &ir.SchemaIR{Type: ir.TypeInt}

	if err := ApplyAdvancedKeywords(s, target, nil); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	if target.Const == nil || *target.Const != "oops" {
		t.Errorf("Const = %v, want pointer to \"oops\"", target.Const)
	}

	if len(target.Validators) != 0 {
		t.Errorf("Validators = %v, want empty", target.Validators)
	}
}

func TestApplyAdvancedKeywords_ConstNoType(t *testing.T) {
	// Without a known primitive type, the const is preserved but no validator
	// is inferred.
	s := &Schema{Const: "unknown"}
	target := &ir.SchemaIR{}

	if err := ApplyAdvancedKeywords(s, target, nil); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	if target.Const == nil || *target.Const != "unknown" {
		t.Errorf("Const = %v, want pointer to \"unknown\"", target.Const)
	}

	if len(target.Validators) != 0 {
		t.Errorf("Validators = %v, want empty", target.Validators)
	}
}

func TestApplyAdvancedKeywords_NotEnum(t *testing.T) {
	s := &Schema{
		Type: SchemaTypeString,
		Not:  &Schema{Enum: []interface{}{"forbidden", "blocked"}},
	}
	target := &ir.SchemaIR{Type: ir.TypeString}

	if err := ApplyAdvancedKeywords(s, target, nil); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	if target.Not == nil {
		t.Fatalf("Not is nil, want non-nil")
	}
	if len(target.Not.EnumValues) != 2 {
		t.Errorf("Not.EnumValues = %v, want 2 values", target.Not.EnumValues)
	}

	want := []ir.ValidatorIR{{Type: "stringvalidator.NoneOf", Args: []string{"forbidden", "blocked"}}}
	if !reflect.DeepEqual(target.Validators, want) {
		t.Errorf("Validators = %v, want %v", target.Validators, want)
	}
}

func TestApplyAdvancedKeywords_NotWithoutEnum(t *testing.T) {
	s := &Schema{
		Type: SchemaTypeString,
		Not:  &Schema{Type: SchemaTypeInteger},
	}
	target := &ir.SchemaIR{Type: ir.TypeString}

	if err := ApplyAdvancedKeywords(s, target, nil); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	if target.Not == nil || target.Not.Type != ir.TypeInt {
		t.Fatalf("Not = %v, want integer IR", target.Not)
	}

	if len(target.Validators) != 0 {
		t.Errorf("Validators = %v, want empty (not does not contain enum)", target.Validators)
	}
}

func TestApplyAdvancedKeywords_IfThenElse(t *testing.T) {
	s := &Schema{
		Type: SchemaTypeObject,
		If:   &Schema{Type: SchemaTypeObject, Properties: map[string]*Schema{"premium": {Type: SchemaTypeBoolean}}},
		Then: &Schema{Type: SchemaTypeObject, Required: []string{"payment_method"}},
		Else: &Schema{Type: SchemaTypeObject, Required: []string{"basic_plan"}},
	}
	target := &ir.SchemaIR{Type: ir.TypeString}

	if err := ApplyAdvancedKeywords(s, target, nil); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	if target.IfSchema == nil {
		t.Error("IfSchema is nil")
	}
	if target.ThenSchema == nil {
		t.Error("ThenSchema is nil")
	}
	if target.ElseSchema == nil {
		t.Error("ElseSchema is nil")
	}
}

func TestApplyAdvancedKeywords_DependentRequired(t *testing.T) {
	s := &Schema{
		Type: SchemaTypeObject,
		DependentRequired: map[string][]string{
			"billing_address": {"payment_method", "country"},
		},
	}
	target := &ir.SchemaIR{}

	if err := ApplyAdvancedKeywords(s, target, nil); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	want := map[string][]string{
		"billing_address": {"payment_method", "country"},
	}
	if !reflect.DeepEqual(target.DependentRequired, want) {
		t.Errorf("DependentRequired = %v, want %v", target.DependentRequired, want)
	}
}

func TestApplyAdvancedKeywords_DependentSchemas(t *testing.T) {
	s := &Schema{
		Type: SchemaTypeObject,
		DependentSchemas: map[string]*Schema{
			"credit_card": {
				Type:       SchemaTypeObject,
				Properties: map[string]*Schema{"cvv": {Type: SchemaTypeString}},
				Required:   []string{"cvv"},
			},
		},
	}
	target := &ir.SchemaIR{}

	if err := ApplyAdvancedKeywords(s, target, nil); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	if len(target.DependentSchemas) != 1 {
		t.Fatalf("DependentSchemas length = %d, want 1", len(target.DependentSchemas))
	}
	if target.DependentSchemas["credit_card"] == nil {
		t.Fatalf("DependentSchemas[\"credit_card\"] is nil")
	}
	cvvAttrs := target.DependentSchemas["credit_card"].Attributes
	if len(cvvAttrs) != 1 || cvvAttrs[0].Name != "cvv" || !cvvAttrs[0].Required {
		t.Errorf("DependentSchemas[\"credit_card\"] cvv attribute = %v, want one required \"cvv\" attribute", cvvAttrs)
	}
}

func TestApplyAdvancedKeywords_DependentSchemasNestedAdvanced(t *testing.T) {
	s := &Schema{
		Type: SchemaTypeObject,
		DependentSchemas: map[string]*Schema{
			"premium": {
				Type: SchemaTypeObject,
				Properties: map[string]*Schema{
					"tier":  {Type: SchemaTypeString, Const: "gold"},
					"score": {Type: SchemaTypeInteger, Not: &Schema{Enum: []interface{}{float64(1), float64(2)}}},
				},
				Required: []string{"tier", "score"},
			},
		},
	}
	target := &ir.SchemaIR{}

	if err := ApplyAdvancedKeywords(s, target, nil); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	if len(target.DependentSchemas) != 1 {
		t.Fatalf("DependentSchemas length = %d, want 1", len(target.DependentSchemas))
	}
	premium, ok := target.DependentSchemas["premium"]
	if !ok {
		t.Fatalf("DependentSchemas[\"premium\"] missing")
	}
	if len(premium.Attributes) != 2 {
		t.Fatalf("premium.Attributes length = %d, want 2", len(premium.Attributes))
	}

	attrs := make(map[string]ir.AttributeIR, len(premium.Attributes))
	for _, a := range premium.Attributes {
		attrs[a.Name] = a
	}

	tier, ok := attrs["tier"]
	if !ok {
		t.Fatalf("tier attribute missing")
	}
	if tier.Schema.Const == nil || *tier.Schema.Const != "gold" {
		t.Errorf("tier.Const = %v, want gold", tier.Schema.Const)
	}
	wantTierValidators := []ir.ValidatorIR{{Type: "stringvalidator.OneOf", Args: []string{"gold"}}}
	if !reflect.DeepEqual(tier.Schema.Validators, wantTierValidators) {
		t.Errorf("tier.Validators = %v, want %v", tier.Schema.Validators, wantTierValidators)
	}

	score, ok := attrs["score"]
	if !ok {
		t.Fatalf("score attribute missing")
	}
	wantScoreValidators := []ir.ValidatorIR{{Type: "int64validator.NoneOf", Args: []string{"1", "2"}}}
	if !reflect.DeepEqual(score.Schema.Validators, wantScoreValidators) {
		t.Errorf("score.Validators = %v, want %v", score.Schema.Validators, wantScoreValidators)
	}
}

func TestApplyAdvancedKeywords_NotEnumMixedTypes(t *testing.T) {
	s := &Schema{
		Type: SchemaTypeString,
		Not:  &Schema{Enum: []interface{}{float64(1), "forbidden", true}},
	}
	target := &ir.SchemaIR{Type: ir.TypeString}

	if err := ApplyAdvancedKeywords(s, target, nil); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	if target.Not == nil {
		t.Fatalf("Not is nil, want non-nil")
	}
	if len(target.Not.EnumValues) != 3 {
		t.Errorf("Not.EnumValues length = %d, want 3", len(target.Not.EnumValues))
	}

	want := []ir.ValidatorIR{{Type: "stringvalidator.NoneOf", Args: []string{"forbidden"}}}
	if !reflect.DeepEqual(target.Validators, want) {
		t.Errorf("Validators = %v, want %v", target.Validators, want)
	}
}

func TestApplyAdvancedKeywords_NotEnumWithAllOf(t *testing.T) {
	s := &Schema{
		Type: SchemaTypeString,
		Not: &Schema{
			Enum: []interface{}{"forbidden", "blocked"},
			AllOf: []*Schema{
				{
					Type:       SchemaTypeObject,
					Properties: map[string]*Schema{"x": {Type: SchemaTypeString}},
				},
			},
		},
	}
	target := &ir.SchemaIR{Type: ir.TypeString}

	if err := ApplyAdvancedKeywords(s, target, nil); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	if target.Not == nil {
		t.Fatalf("Not is nil, want non-nil")
	}
	if target.Not.EnumValues != nil {
		t.Errorf("Not.EnumValues = %v, want nil (enum metadata is not preserved on converted Not schema)", target.Not.EnumValues)
	}

	want := []ir.ValidatorIR{{Type: "stringvalidator.NoneOf", Args: []string{"forbidden", "blocked"}}}
	if !reflect.DeepEqual(target.Validators, want) {
		t.Errorf("Validators = %v, want %v (validator args should come from raw enum)", target.Validators, want)
	}
}

func TestApplyAdvancedKeywords_NestedIfThenElse(t *testing.T) {
	s := &Schema{
		Type: SchemaTypeObject,
		If: &Schema{
			Type:       SchemaTypeObject,
			Properties: map[string]*Schema{"premium": {Type: SchemaTypeBoolean}},
		},
		Then: &Schema{
			Type: SchemaTypeObject,
			If: &Schema{
				Type:       SchemaTypeObject,
				Properties: map[string]*Schema{"enterprise": {Type: SchemaTypeBoolean}},
			},
			Then: &Schema{Type: SchemaTypeObject, Required: []string{"dedicated_support"}},
			Else: &Schema{Type: SchemaTypeObject, Required: []string{"standard_support"}},
		},
		Else: &Schema{Type: SchemaTypeObject, Required: []string{"basic_plan"}},
	}
	target := &ir.SchemaIR{}

	if err := ApplyAdvancedKeywords(s, target, nil); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	if target.IfSchema == nil {
		t.Fatal("IfSchema is nil")
	}
	if target.ThenSchema == nil {
		t.Fatal("ThenSchema is nil")
	}
	if target.ElseSchema == nil {
		t.Fatal("ElseSchema is nil")
	}
	if target.ThenSchema.IfSchema == nil {
		t.Fatal("nested ThenSchema.IfSchema is nil")
	}
	if target.ThenSchema.ThenSchema == nil {
		t.Fatal("nested ThenSchema.ThenSchema is nil")
	}
	if target.ThenSchema.ElseSchema == nil {
		t.Fatal("nested ThenSchema.ElseSchema is nil")
	}
}

func TestApplyAdvancedKeywords_NilInputs(t *testing.T) {
	var target *ir.SchemaIR
	if err := ApplyAdvancedKeywords(nil, target, nil); err != nil {
		t.Fatalf("nil inputs should not error, got %v", err)
	}

	s := &Schema{Type: SchemaTypeString, Const: "x"}
	if err := ApplyAdvancedKeywords(s, nil, nil); err != nil {
		t.Fatalf("nil target should not error, got %v", err)
	}
}

func TestApplyAdvancedKeywords_Pattern(t *testing.T) {
	s := &Schema{Type: SchemaTypeString, Pattern: "^[a-z]+$"}
	target := &ir.SchemaIR{Type: ir.TypeString}

	if err := ApplyAdvancedKeywords(s, target, nil); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	if target.Pattern != "^[a-z]+$" {
		t.Errorf("Pattern = %q, want %q", target.Pattern, "^[a-z]+$")
	}
	want := []ir.ValidatorIR{{Type: "stringvalidator.RegexMatches", Args: []string{"^[a-z]+$"}}}
	if !reflect.DeepEqual(target.Validators, want) {
		t.Errorf("Validators = %v, want %v", target.Validators, want)
	}
}

func TestApplyAdvancedKeywords_PatternNoValidatorForUntyped(t *testing.T) {
	s := &Schema{Pattern: "^[a-z]+$"}
	target := &ir.SchemaIR{}

	if err := ApplyAdvancedKeywords(s, target, nil); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	if target.Pattern != "^[a-z]+$" {
		t.Errorf("Pattern = %q, want %q", target.Pattern, "^[a-z]+$")
	}
	if len(target.Validators) != 0 {
		t.Errorf("Validators = %v, want empty", target.Validators)
	}
}

func TestApplyAdvancedKeywords_StringLength(t *testing.T) {
	cases := []struct {
		name string
		s    *Schema
		want []ir.ValidatorIR
	}{
		{
			name: "both",
			s:    &Schema{Type: SchemaTypeString, MinLength: intPtr(2), MaxLength: intPtr(10)},
			want: []ir.ValidatorIR{{Type: "stringvalidator.LengthBetween", Args: []string{"2", "10"}}},
		},
		{
			name: "min only",
			s:    &Schema{Type: SchemaTypeString, MinLength: intPtr(1)},
			want: []ir.ValidatorIR{{Type: "stringvalidator.LengthAtLeast", Args: []string{"1"}}},
		},
		{
			name: "max only",
			s:    &Schema{Type: SchemaTypeString, MaxLength: intPtr(20)},
			want: []ir.ValidatorIR{{Type: "stringvalidator.LengthAtMost", Args: []string{"20"}}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := &ir.SchemaIR{Type: ir.TypeString}
			if err := ApplyAdvancedKeywords(tc.s, target, nil); err != nil {
				t.Fatalf("ApplyAdvancedKeywords error: %v", err)
			}
			if target.MinLength != tc.s.MinLength {
				t.Errorf("MinLength = %v, want %v", target.MinLength, tc.s.MinLength)
			}
			if target.MaxLength != tc.s.MaxLength {
				t.Errorf("MaxLength = %v, want %v", target.MaxLength, tc.s.MaxLength)
			}
			if !reflect.DeepEqual(target.Validators, tc.want) {
				t.Errorf("Validators = %v, want %v", target.Validators, tc.want)
			}
		})
	}
}

func TestApplyAdvancedKeywords_NumericBoundsInt(t *testing.T) {
	cases := []struct {
		name string
		s    *Schema
		want []ir.ValidatorIR
	}{
		{
			name: "between",
			s:    &Schema{Type: SchemaTypeInteger, Minimum: floatPtr(0), Maximum: floatPtr(100)},
			want: []ir.ValidatorIR{{Type: "int64validator.Between", Args: []string{"0", "100"}}},
		},
		{
			name: "min only",
			s:    &Schema{Type: SchemaTypeInteger, Minimum: floatPtr(5)},
			want: []ir.ValidatorIR{{Type: "int64validator.AtLeast", Args: []string{"5"}}},
		},
		{
			name: "max only",
			s:    &Schema{Type: SchemaTypeInteger, Maximum: floatPtr(50)},
			want: []ir.ValidatorIR{{Type: "int64validator.AtMost", Args: []string{"50"}}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := &ir.SchemaIR{Type: ir.TypeInt}
			if err := ApplyAdvancedKeywords(tc.s, target, nil); err != nil {
				t.Fatalf("ApplyAdvancedKeywords error: %v", err)
			}
			if !reflect.DeepEqual(target.Validators, tc.want) {
				t.Errorf("Validators = %v, want %v", target.Validators, tc.want)
			}
		})
	}
}

// TestApplyAdvancedKeywords_IntBoundsOutOfRange locks in the M-45 fix: a
// spec-supplied integer bound outside the int64 range (e.g. minimum=1e19) is
// dropped with a warning diagnostic rather than saturated to MinInt64, which
// would emit a silently wrong int64validator.AtLeast(-9223372036854775808).
func TestApplyAdvancedKeywords_IntBoundsOutOfRange(t *testing.T) {
	s := &Schema{Type: SchemaTypeInteger, Minimum: floatPtr(1e19)}
	target := &ir.SchemaIR{Type: ir.TypeInt}
	var diags diagnostics.Diagnostics

	if err := ApplyAdvancedKeywords(s, target, &diags); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}
	if len(target.Validators) != 0 {
		t.Fatalf("expected no int64 validator for out-of-range bound, got %v", target.Validators)
	}
	if len(diags) != 1 {
		t.Fatalf("expected 1 warning diagnostic for the dropped bound, got %d: %v", len(diags), diags)
	}
	if diags[0].Severity != diagnostics.Warning {
		t.Fatalf("expected warning severity, got %v", diags[0].Severity)
	}
}

func TestApplyAdvancedKeywords_ExclusiveBoundsInt(t *testing.T) {
	s := &Schema{
		Type:             SchemaTypeInteger,
		ExclusiveMinimum: floatPtr(0),
		ExclusiveMaximum: floatPtr(100),
	}
	target := &ir.SchemaIR{Type: ir.TypeInt}

	if err := ApplyAdvancedKeywords(s, target, nil); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	if target.ExclusiveMinimum == nil || *target.ExclusiveMinimum != 0 {
		t.Errorf("ExclusiveMinimum = %v, want 0", target.ExclusiveMinimum)
	}
	if target.ExclusiveMaximum == nil || *target.ExclusiveMaximum != 100 {
		t.Errorf("ExclusiveMaximum = %v, want 100", target.ExclusiveMaximum)
	}

	want := []ir.ValidatorIR{
		{Type: "int64validator.Between", Args: []string{"1", "99"}},
	}
	if !reflect.DeepEqual(target.Validators, want) {
		t.Errorf("Validators = %v, want %v", target.Validators, want)
	}
}

func TestApplyAdvancedKeywords_MultipleOfInt(t *testing.T) {
	s := &Schema{Type: SchemaTypeInteger, MultipleOf: floatPtr(5)}
	target := &ir.SchemaIR{Type: ir.TypeInt}

	if err := ApplyAdvancedKeywords(s, target, nil); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	if target.MultipleOf == nil || *target.MultipleOf != 5 {
		t.Errorf("MultipleOf = %v, want 5", target.MultipleOf)
	}
	want := []ir.ValidatorIR{{Type: "validators.Int64MultipleOfValidator", Args: []string{"5"}}}
	if !reflect.DeepEqual(target.Validators, want) {
		t.Errorf("Validators = %v, want %v", target.Validators, want)
	}
}

func TestApplyAdvancedKeywords_NumericBoundsFloat(t *testing.T) {
	s := &Schema{Type: SchemaTypeNumber, Minimum: floatPtr(0.5), Maximum: floatPtr(5.5)}
	target := &ir.SchemaIR{Type: ir.TypeFloat}

	if err := ApplyAdvancedKeywords(s, target, nil); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	if target.Minimum == nil || *target.Minimum != 0.5 {
		t.Errorf("Minimum = %v, want 0.5", target.Minimum)
	}
	if target.Maximum == nil || *target.Maximum != 5.5 {
		t.Errorf("Maximum = %v, want 5.5", target.Maximum)
	}
	want := []ir.ValidatorIR{{Type: "float64validator.Between", Args: []string{"0.5", "5.5"}}}
	if !reflect.DeepEqual(target.Validators, want) {
		t.Errorf("Validators = %v, want %v", target.Validators, want)
	}
}

func TestApplyAdvancedKeywords_ExclusiveBoundsFloat(t *testing.T) {
	s := &Schema{Type: SchemaTypeNumber, ExclusiveMinimum: floatPtr(0), ExclusiveMaximum: floatPtr(100)}
	target := &ir.SchemaIR{Type: ir.TypeFloat}

	if err := ApplyAdvancedKeywords(s, target, nil); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	want := []ir.ValidatorIR{
		{Type: "validators.ExclusiveMinimumValidator", Args: []string{"0"}},
		{Type: "validators.ExclusiveMaximumValidator", Args: []string{"100"}},
	}
	if !reflect.DeepEqual(target.Validators, want) {
		t.Errorf("Validators = %v, want %v", target.Validators, want)
	}
}

func TestApplyAdvancedKeywords_MultipleOfFloat(t *testing.T) {
	s := &Schema{Type: SchemaTypeNumber, MultipleOf: floatPtr(0.5)}
	target := &ir.SchemaIR{Type: ir.TypeFloat}

	if err := ApplyAdvancedKeywords(s, target, nil); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	want := []ir.ValidatorIR{{Type: "validators.Float64MultipleOfValidator", Args: []string{"0.5"}}}
	if !reflect.DeepEqual(target.Validators, want) {
		t.Errorf("Validators = %v, want %v", target.Validators, want)
	}
}

func TestApplyAdvancedKeywords_ArrayItems(t *testing.T) {
	s := &Schema{
		Type:     SchemaTypeArray,
		Items:    &Schema{Type: SchemaTypeString},
		MinItems: intPtr(1),
		MaxItems: intPtr(10),
	}
	target := &ir.SchemaIR{
		Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Type: ir.TypeString}},
	}

	if err := ApplyAdvancedKeywords(s, target, nil); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	if target.MinItems == nil || *target.MinItems != 1 {
		t.Errorf("MinItems = %v, want 1", target.MinItems)
	}
	if target.MaxItems == nil || *target.MaxItems != 10 {
		t.Errorf("MaxItems = %v, want 10", target.MaxItems)
	}
	want := []ir.ValidatorIR{{Type: "listvalidator.SizeBetween", Args: []string{"1", "10"}}}
	if !reflect.DeepEqual(target.Validators, want) {
		t.Errorf("Validators = %v, want %v", target.Validators, want)
	}
}

func TestApplyAdvancedKeywords_PatternProperties(t *testing.T) {
	s := &Schema{
		Type: SchemaTypeObject,
		PatternProperties: map[string]*Schema{
			"^x-": {Type: SchemaTypeString},
		},
	}
	target := &ir.SchemaIR{}

	if err := ApplyAdvancedKeywords(s, target, nil); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	if len(target.PatternProperties) != 1 {
		t.Fatalf("PatternProperties length = %d, want 1", len(target.PatternProperties))
	}
	if target.PatternProperties["^x-"] == nil || target.PatternProperties["^x-"].Type != ir.TypeString {
		t.Errorf("PatternProperties[\"^x-\"] = %v, want string IR", target.PatternProperties["^x-"])
	}
}

func TestApplyAdvancedKeywords_PropertyNames(t *testing.T) {
	s := &Schema{
		Type:          SchemaTypeObject,
		PropertyNames: &Schema{Type: SchemaTypeString, Pattern: "^[a-z]+$"},
	}
	target := &ir.SchemaIR{}

	if err := ApplyAdvancedKeywords(s, target, nil); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	if target.PropertyNames == nil || target.PropertyNames.Type != ir.TypeString {
		t.Fatalf("PropertyNames = %v, want string IR", target.PropertyNames)
	}
	if target.PropertyNames.Pattern != "^[a-z]+$" {
		t.Errorf("PropertyNames.Pattern = %q, want %q", target.PropertyNames.Pattern, "^[a-z]+$")
	}

	want := []ir.ValidatorIR{{Type: "mapvalidator.KeysAre", Args: []string{"stringvalidator.RegexMatches", "^[a-z]+$"}}}
	if !reflect.DeepEqual(target.Validators, want) {
		t.Errorf("Validators = %v, want %v", target.Validators, want)
	}
}

func TestApplyAdvancedKeywords_UnevaluatedProperties(t *testing.T) {
	s := &Schema{
		Type:                  SchemaTypeObject,
		UnevaluatedProperties: &Schema{Type: SchemaTypeBoolean},
	}
	target := &ir.SchemaIR{}

	if err := ApplyAdvancedKeywords(s, target, nil); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	if target.UnevaluatedProperties == nil || target.UnevaluatedProperties.Type != ir.TypeBool {
		t.Errorf("UnevaluatedProperties = %v, want bool IR", target.UnevaluatedProperties)
	}
}

func TestApplyAdvancedKeywords_ObjectSize(t *testing.T) {
	s := &Schema{
		Type:          SchemaTypeObject,
		MinProperties: intPtr(1),
		MaxProperties: intPtr(5),
	}
	target := &ir.SchemaIR{}

	if err := ApplyAdvancedKeywords(s, target, nil); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	if target.MinProperties == nil || *target.MinProperties != 1 {
		t.Errorf("MinProperties = %v, want 1", target.MinProperties)
	}
	if target.MaxProperties == nil || *target.MaxProperties != 5 {
		t.Errorf("MaxProperties = %v, want 5", target.MaxProperties)
	}
	want := []ir.ValidatorIR{{Type: "mapvalidator.SizeBetween", Args: []string{"1", "5"}}}
	if !reflect.DeepEqual(target.Validators, want) {
		t.Errorf("Validators = %v, want %v", target.Validators, want)
	}
}

func TestApplyAdvancedKeywords_ObjectSizeIgnoredForFixedProperties(t *testing.T) {
	s := &Schema{
		Type: SchemaTypeObject,
		Properties: map[string]*Schema{
			"id": {Type: SchemaTypeString},
		},
		MinProperties: intPtr(1),
		MaxProperties: intPtr(5),
	}
	target := &ir.SchemaIR{
		Attributes: []ir.AttributeIR{{Name: "id", Schema: ir.SchemaIR{Type: ir.TypeString}}},
	}

	if err := ApplyAdvancedKeywords(s, target, nil); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	if len(target.Validators) != 0 {
		t.Errorf("Validators = %v, want empty for fixed-schema object", target.Validators)
	}
}

func TestSchemaToIR_AdvancedKeywords(t *testing.T) {
	s := &Schema{
		Type: SchemaTypeObject,
		Properties: map[string]*Schema{
			"kind": {
				Type:  SchemaTypeString,
				Const: "fixed-kind",
			},
			"value": {
				Type: SchemaTypeInteger,
				Not:  &Schema{Enum: []interface{}{float64(1), float64(2)}},
			},
		},
		DependentRequired: map[string][]string{
			"kind": {"value"},
		},
	}

	irSchema, err := schemaToIR(s, nil)
	if err != nil {
		t.Fatalf("schemaToIR error: %v", err)
	}

	if len(irSchema.Attributes) != 2 {
		t.Fatalf("Attributes length = %d, want 2", len(irSchema.Attributes))
	}

	attrs := make(map[string]ir.AttributeIR, len(irSchema.Attributes))
	for _, a := range irSchema.Attributes {
		attrs[a.Name] = a
	}

	kind, ok := attrs["kind"]
	if !ok {
		t.Fatalf("kind attribute missing")
	}
	if kind.Schema.Const == nil || *kind.Schema.Const != "fixed-kind" {
		t.Errorf("kind.Const = %v, want fixed-kind", kind.Schema.Const)
	}
	wantKindValidators := []ir.ValidatorIR{{Type: "stringvalidator.OneOf", Args: []string{"fixed-kind"}}}
	if !reflect.DeepEqual(kind.Schema.Validators, wantKindValidators) {
		t.Errorf("kind.Validators = %v, want %v", kind.Schema.Validators, wantKindValidators)
	}

	value, ok := attrs["value"]
	if !ok {
		t.Fatalf("value attribute missing")
	}
	wantValueValidators := []ir.ValidatorIR{{Type: "int64validator.NoneOf", Args: []string{"1", "2"}}}
	if !reflect.DeepEqual(value.Schema.Validators, wantValueValidators) {
		t.Errorf("value.Validators = %v, want %v", value.Schema.Validators, wantValueValidators)
	}

	wantDepReq := map[string][]string{"kind": {"value"}}
	if !reflect.DeepEqual(irSchema.DependentRequired, wantDepReq) {
		t.Errorf("DependentRequired = %v, want %v", irSchema.DependentRequired, wantDepReq)
	}
}

func TestApplyAdvancedKeywords_OverlappingInclusiveExclusiveInt(t *testing.T) {
	cases := []struct {
		name string
		s    *Schema
		want []ir.ValidatorIR
	}{
		{
			name: "exclusive minimum tighter than inclusive minimum",
			s:    &Schema{Type: SchemaTypeInteger, Minimum: floatPtr(0), ExclusiveMinimum: floatPtr(5)},
			want: []ir.ValidatorIR{{Type: "int64validator.AtLeast", Args: []string{"6"}}},
		},
		{
			name: "inclusive minimum tighter than exclusive minimum",
			s:    &Schema{Type: SchemaTypeInteger, Minimum: floatPtr(10), ExclusiveMinimum: floatPtr(5)},
			want: []ir.ValidatorIR{{Type: "int64validator.AtLeast", Args: []string{"10"}}},
		},
		{
			name: "exclusive maximum tighter than inclusive maximum",
			s:    &Schema{Type: SchemaTypeInteger, Maximum: floatPtr(100), ExclusiveMaximum: floatPtr(90)},
			want: []ir.ValidatorIR{{Type: "int64validator.AtMost", Args: []string{"89"}}},
		},
		{
			name: "inclusive maximum tighter than exclusive maximum",
			s:    &Schema{Type: SchemaTypeInteger, Maximum: floatPtr(80), ExclusiveMaximum: floatPtr(90)},
			want: []ir.ValidatorIR{{Type: "int64validator.AtMost", Args: []string{"80"}}},
		},
		{
			name: "both bounds merged with exclusive adjustments",
			s:    &Schema{Type: SchemaTypeInteger, Minimum: floatPtr(0), Maximum: floatPtr(100), ExclusiveMinimum: floatPtr(5), ExclusiveMaximum: floatPtr(90)},
			want: []ir.ValidatorIR{{Type: "int64validator.Between", Args: []string{"6", "89"}}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := &ir.SchemaIR{Type: ir.TypeInt}
			if err := ApplyAdvancedKeywords(tc.s, target, nil); err != nil {
				t.Fatalf("ApplyAdvancedKeywords error: %v", err)
			}
			if !reflect.DeepEqual(target.Validators, tc.want) {
				t.Errorf("Validators = %v, want %v", target.Validators, tc.want)
			}
		})
	}
}

func TestApplyAdvancedKeywords_ExclusiveMinimumNonIntegerInt(t *testing.T) {
	s := &Schema{Type: SchemaTypeInteger, ExclusiveMinimum: floatPtr(0.5)}
	target := &ir.SchemaIR{Type: ir.TypeInt}

	if err := ApplyAdvancedKeywords(s, target, nil); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	// exclusiveMinimum=0.5 means x > 0.5. For integers the smallest valid value
	// is 1, so the effective inclusive bound is floor(0.5)+1 = 1.
	want := []ir.ValidatorIR{{Type: "int64validator.AtLeast", Args: []string{"1"}}}
	if !reflect.DeepEqual(target.Validators, want) {
		t.Errorf("Validators = %v, want %v", target.Validators, want)
	}
}

func TestApplyAdvancedKeywords_MultipleOfIntNonIntegerDiagnostic(t *testing.T) {
	s := &Schema{Type: SchemaTypeInteger, MultipleOf: floatPtr(2.5)}
	target := &ir.SchemaIR{Type: ir.TypeInt}
	var diags diagnostics.Diagnostics

	if err := ApplyAdvancedKeywords(s, target, &diags); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	if len(target.Validators) != 0 {
		t.Errorf("Validators = %v, want empty for non-integer multipleOf on integer", target.Validators)
	}
	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diags))
	}
	if diags[0].Severity != diagnostics.Warning {
		t.Errorf("diagnostic severity = %v, want Warning", diags[0].Severity)
	}
	if !strings.Contains(diags[0].Summary, "multipleOf") {
		t.Errorf("diagnostic summary = %q, want it to mention multipleOf", diags[0].Summary)
	}
}

func TestApplyAdvancedKeywords_PatternPropertiesNestedSchema(t *testing.T) {
	s := &Schema{
		Type: SchemaTypeObject,
		PatternProperties: map[string]*Schema{
			"^x-": {Type: SchemaTypeString, Pattern: "^[a-z]+$", MinLength: intPtr(2)},
		},
	}
	target := &ir.SchemaIR{}

	if err := ApplyAdvancedKeywords(s, target, nil); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	if len(target.PatternProperties) != 1 {
		t.Fatalf("PatternProperties length = %d, want 1", len(target.PatternProperties))
	}
	inner, ok := target.PatternProperties["^x-"]
	if !ok || inner == nil {
		t.Fatalf("PatternProperties[\"^x-\"] = %v, want non-nil", target.PatternProperties["^x-"])
	}
	if inner.Type != ir.TypeString {
		t.Errorf("inner.Type = %v, want SchemaTypeString", inner.Type)
	}
	if inner.Pattern != "^[a-z]+$" {
		t.Errorf("inner.Pattern = %q, want %q", inner.Pattern, "^[a-z]+$")
	}
	if inner.MinLength == nil || *inner.MinLength != 2 {
		t.Errorf("inner.MinLength = %v, want 2", inner.MinLength)
	}
}

func TestApplyAdvancedKeywords_ObjectSizeIgnoredForFixedPropertiesDiagnostic(t *testing.T) {
	s := &Schema{
		Type: SchemaTypeObject,
		Properties: map[string]*Schema{
			"id": {Type: SchemaTypeString},
		},
		MinProperties: intPtr(1),
		MaxProperties: intPtr(5),
	}
	target := &ir.SchemaIR{
		Attributes: []ir.AttributeIR{{Name: "id", Schema: ir.SchemaIR{Type: ir.TypeString}}},
	}
	var diags diagnostics.Diagnostics

	if err := ApplyAdvancedKeywords(s, target, &diags); err != nil {
		t.Fatalf("ApplyAdvancedKeywords error: %v", err)
	}

	if len(target.Validators) != 0 {
		t.Errorf("Validators = %v, want empty for fixed-schema object", target.Validators)
	}
	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diags))
	}
	if diags[0].Severity != diagnostics.Info {
		t.Errorf("diagnostic severity = %v, want Info", diags[0].Severity)
	}
	if !strings.Contains(diags[0].Summary, "minProperties") {
		t.Errorf("diagnostic summary = %q, want it to mention minProperties/maxProperties", diags[0].Summary)
	}
}
