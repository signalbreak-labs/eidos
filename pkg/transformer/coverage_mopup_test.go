package transformer

import (
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/ir"
	"github.com/signalbreak-labs/eidos/pkg/parser"
)

// TestSchemaIRFromSpec covers every primitive type, the array forms (nil items,
// uniqueItems → Set), and the dynamic fallback of the shallow action mapper.
func TestSchemaIRFromSpec(t *testing.T) {
	cases := []struct {
		name string
		spec SchemaSpec
		want string
	}{
		{"string", SchemaSpec{Type: "string", Format: "date-time"}, "string"},
		{"integer", SchemaSpec{Type: "integer"}, "integer"},
		{"number", SchemaSpec{Type: "number"}, "number"},
		{"boolean", SchemaSpec{Type: "boolean"}, "boolean"},
		{"array nil items", SchemaSpec{Type: "array"}, "dynamic"},
		{"unknown type", SchemaSpec{Type: "widget"}, "dynamic"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := schemaIRFromSpec(tc.spec); string(got.Type) != tc.want {
				t.Errorf("schemaIRFromSpec(%+v).Type = %q, want %q", tc.spec, got.Type, tc.want)
			}
		})
	}
	// Array of strings → List of string.
	list := schemaIRFromSpec(SchemaSpec{Type: "array", Items: &SchemaSpec{Type: "string"}})
	if list.Collection == nil || list.Collection.Kind != ir.List || list.Collection.ElementType.Type != ir.TypeString {
		t.Errorf("array of strings = %+v, want List[string]", list)
	}
	// uniqueItems → Set.
	set := schemaIRFromSpec(SchemaSpec{Type: "array", UniqueItems: true, Items: &SchemaSpec{Type: "integer"}})
	if set.Collection == nil || set.Collection.Kind != ir.Set {
		t.Errorf("uniqueItems array = %+v, want Set", set)
	}
}

// TestIsVowel covers all five vowels plus the non-vowel rejection.
func TestIsVowel(t *testing.T) {
	for _, b := range []byte{'a', 'e', 'i', 'o', 'u'} {
		if !isVowel(b) {
			t.Errorf("isVowel(%q) = false, want true", b)
		}
	}
	for _, b := range []byte{'b', 'y', 'A', 'E', '!', ' '} {
		if isVowel(b) {
			t.Errorf("isVowel(%q) = true, want false", b)
		}
	}
}

// TestWarnHelpersNoOpNil asserts the warning helpers are no-ops on a nil
// diagnostics sink.
func TestWarnHelpersNoOpNil(t *testing.T) {
	warnCompositionNotModeled(nil, "oneOf", parser.SourceLocation{})
	warnBooleanSchemaDropped(nil, "items", parser.SourceLocation{})
	warnIntRangeDropped(nil, "maximum", int64(1))
}

// TestWarnCompositionNotModeled asserts a non-nil sink receives a warning whose
// summary names the composition kind.
func TestWarnCompositionNotModeled(t *testing.T) {
	var diags diagnostics.Diagnostics
	warnCompositionNotModeled(&diags, "oneOf", parser.SourceLocation{})
	if len(diags) != 1 || diags[0].Severity != diagnostics.Warning {
		t.Fatalf("diags = %+v, want one warning", diags)
	}
	if !strings.Contains(diags[0].Summary, "oneOf") || !strings.Contains(diags[0].Summary, "not modeled") {
		t.Errorf("summary = %q, want oneOf composition not modeled", diags[0].Summary)
	}
}

// TestWarnBooleanSchemaDropped asserts a non-nil sink receives a warning naming
// the dropped field.
func TestWarnBooleanSchemaDropped(t *testing.T) {
	var diags diagnostics.Diagnostics
	warnBooleanSchemaDropped(&diags, "items", parser.SourceLocation{})
	if len(diags) != 1 || diags[0].Severity != diagnostics.Warning {
		t.Fatalf("diags = %+v, want one warning", diags)
	}
	if !strings.Contains(diags[0].Summary, "items") || !strings.Contains(diags[0].Summary, "dropped") {
		t.Errorf("summary = %q, want boolean items schema dropped", diags[0].Summary)
	}
}

// TestWarnIntRangeDropped asserts a non-nil sink receives a warning naming the
// out-of-range bound.
func TestWarnIntRangeDropped(t *testing.T) {
	var diags diagnostics.Diagnostics
	warnIntRangeDropped(&diags, "maximum", int64(9223372036854775807))
	if len(diags) != 1 || diags[0].Severity != diagnostics.Warning {
		t.Fatalf("diags = %+v, want one warning", diags)
	}
	if !strings.Contains(diags[0].Summary, "maximum") || !strings.Contains(diags[0].Summary, "dropped") {
		t.Errorf("summary = %q, want integer maximum dropped", diags[0].Summary)
	}
}

// TestAllOfIsObjectLike covers the nil, explicit-object, implicit-properties,
// and non-object cases.
func TestAllOfIsObjectLike(t *testing.T) {
	if allOfIsObjectLike(nil) {
		t.Error("nil schema should not be object-like")
	}
	if !allOfIsObjectLike(&Schema{Type: SchemaTypeObject}) {
		t.Error("explicit object type should be object-like")
	}
	if !allOfIsObjectLike(&Schema{Properties: map[string]*Schema{"a": {}}}) {
		t.Error("type-less schema with properties should be object-like")
	}
	if allOfIsObjectLike(&Schema{}) {
		t.Error("empty schema should not be object-like")
	}
	if allOfIsObjectLike(&Schema{Type: SchemaTypeArray, Properties: map[string]*Schema{"a": {}}}) {
		t.Error("array-typed schema should not be object-like")
	}
}

// TestApplyHelpersNil asserts the Apply* helpers are no-ops on nil schemas.
func TestApplyHelpersNil(t *testing.T) {
	ApplyPlanModifiers(nil)
	ApplyValidators(nil)
}

// TestSchemaIRSameShape covers every branch of the shallow shape comparison.
func TestSchemaIRSameShape(t *testing.T) {
	obj := func(names ...string) ir.SchemaIR {
		attrs := make([]ir.AttributeIR, 0, len(names))
		for _, n := range names {
			attrs = append(attrs, ir.AttributeIR{Name: n, Schema: ir.SchemaIR{Type: ir.TypeString}})
		}
		return ir.SchemaIR{Attributes: attrs}
	}
	if !schemaIRSameShape(ir.SchemaIR{Type: ir.TypeString}, ir.SchemaIR{Type: ir.TypeString}) {
		t.Error("equal primitives should match")
	}
	if schemaIRSameShape(ir.SchemaIR{Type: ir.TypeString}, ir.SchemaIR{Type: ir.TypeInt}) {
		t.Error("type mismatch should not match")
	}
	if schemaIRSameShape(ir.SchemaIR{Type: ir.TypeString, Format: "date-time"}, ir.SchemaIR{Type: ir.TypeString}) {
		t.Error("format mismatch should not match")
	}
	if schemaIRSameShape(
		ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List}},
		ir.SchemaIR{Type: ir.TypeString}) {
		t.Error("collection presence mismatch should not match")
	}
	if schemaIRSameShape(
		ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List}},
		ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.Set}}) {
		t.Error("collection kind mismatch should not match")
	}
	if schemaIRSameShape(
		ir.SchemaIR{Union: &ir.UnionType{}},
		ir.SchemaIR{Type: ir.TypeString}) {
		t.Error("union presence mismatch should not match")
	}
	if schemaIRSameShape(obj("a"), ir.SchemaIR{Type: ir.TypeString}) {
		t.Error("object-like mismatch should not match")
	}
	if !schemaIRSameShape(obj("a", "b"), obj("c")) {
		t.Error("two object-like schemas should match (shallow comparison)")
	}
}

// TestTopLevelUnionSpec covers nil, empty, oneOf, and anyOf inputs.
func TestTopLevelUnionSpec(t *testing.T) {
	if got := topLevelUnionSpec(nil); got != nil {
		t.Errorf("nil spec = %+v, want nil", got)
	}
	if got := topLevelUnionSpec(&SchemaSpec{Type: "object"}); got != nil {
		t.Errorf("non-union spec = %+v, want nil", got)
	}
	oneOf := topLevelUnionSpec(&SchemaSpec{OneOf: []SchemaSpec{{Type: "string"}}})
	if oneOf == nil || len(oneOf.OneOf) != 1 {
		t.Errorf("oneOf spec = %+v, want the spec back", oneOf)
	}
	anyOf := topLevelUnionSpec(&SchemaSpec{AnyOf: []SchemaSpec{{Type: "integer"}}})
	if anyOf == nil || len(anyOf.AnyOf) != 1 {
		t.Errorf("anyOf spec = %+v, want the spec back", anyOf)
	}
}

// TestIsWriteOnly covers the nil, attribute-flag, schema-flag, and neither cases.
func TestIsWriteOnly(t *testing.T) {
	if isWriteOnly(nil) {
		t.Error("nil attribute should not be write-only")
	}
	if !isWriteOnly(&ir.AttributeIR{WriteOnly: true}) {
		t.Error("attribute WriteOnly flag should count")
	}
	if !isWriteOnly(&ir.AttributeIR{Schema: ir.SchemaIR{WriteOnly: true}}) {
		t.Error("schema WriteOnly flag should count")
	}
	if isWriteOnly(&ir.AttributeIR{Schema: ir.SchemaIR{Type: ir.TypeString}}) {
		t.Error("plain attribute should not be write-only")
	}
}

// TestApplyWriteOnlyRecursive exercises the recursive walk over collection
// elements, union variants, and every conditional schema node.
func TestApplyWriteOnlyRecursive(t *testing.T) {
	leaf := func() *ir.SchemaIR {
		return &ir.SchemaIR{Attributes: []ir.AttributeIR{{Name: "secret", Schema: ir.SchemaIR{Type: ir.TypeString}, WriteOnly: true}}}
	}
	schema := &ir.SchemaIR{
		Attributes: []ir.AttributeIR{{Name: "password", Schema: ir.SchemaIR{Type: ir.TypeString}, WriteOnly: true}},
		Collection: &ir.CollectionType{ElementType: *leaf()},
		Union:      &ir.UnionType{Variants: []ir.SchemaIR{*leaf()}},
		Not:        leaf(),
		IfSchema:   leaf(),
		ThenSchema: leaf(),
		ElseSchema: leaf(),
		DependentSchemas: map[string]*ir.SchemaIR{
			"billing": leaf(),
		},
		PatternProperties: map[string]*ir.SchemaIR{
			"^x-": leaf(),
		},
		PropertyNames:         leaf(),
		UnevaluatedProperties: leaf(),
	}
	applyWriteOnlyRecursive(schema, nil)
	// The walk must have descended into the object branch and renamed the
	// top-level write-only attribute.
	if len(schema.Attributes) == 0 || schema.Attributes[0].Name != "password_wo" {
		t.Errorf("top-level attribute = %+v, want renamed password_wo", schema.Attributes)
	}
}
