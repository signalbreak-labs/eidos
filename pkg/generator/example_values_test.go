package generator

import (
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// TestSchemaExampleLiteral_Constraints covers schemaExampleLiteral across the
// constraint shapes the standard validator emitters derive from a schema. The
// placeholder and the validator are generated from the same SchemaIR, so the
// placeholder must always satisfy the validator: an enum placeholder is a
// listed member, a const placeholder is the const, a bounded placeholder is
// within the bounds, a length-constrained placeholder fits the window, and a
// pattern placeholder matches when the deterministic candidate list allows.
// This is the regression test for the generated spacetraders provider, whose
// acceptance test failed at plan time with `ship_type = "example"` against the
// generated stringvalidator.OneOf(["SHIP_PROBE", …]).
func TestSchemaExampleLiteral_Constraints(t *testing.T) {
	strPtr := func(v string) *any {
		x := any(v)
		return &x
	}
	boolPtr := func(v bool) *any {
		x := any(v)
		return &x
	}
	intPtr := func(v int) *int {
		return &v
	}
	floatPtr := func(v float64) *float64 {
		return &v
	}
	cases := []struct {
		name   string
		schema ir.SchemaIR
		want   string
	}{
		{
			name:   "string enum picks the first member",
			schema: ir.SchemaIR{Type: ir.TypeString, EnumValues: []any{"SHIP_PROBE", "SHIP_MINING_DRONE"}},
			want:   `"SHIP_PROBE"`,
		},
		{
			name:   "string const",
			schema: ir.SchemaIR{Type: ir.TypeString, Const: strPtr("standard")},
			want:   `"standard"`,
		},
		{
			name:   "string enum skipping non-string members",
			schema: ir.SchemaIR{Type: ir.TypeString, EnumValues: []any{float64(1), "alpha"}},
			want:   `"alpha"`,
		},
		{
			name:   "string minLength grows the placeholder to exactly the window",
			schema: ir.SchemaIR{Type: ir.TypeString, MinLength: intPtr(12)},
			want:   `"exampleexamp"`,
		},
		{
			name:   "string maxLength truncates the placeholder",
			schema: ir.SchemaIR{Type: ir.TypeString, MaxLength: intPtr(3)},
			want:   `"exa"`,
		},
		{
			name:   "string pattern picks a matching candidate",
			schema: ir.SchemaIR{Type: ir.TypeString, Pattern: `^[A-Z]{3}-[0-9]+$`},
			want:   `"ABC-1"`,
		},
		{
			name:   "string pattern with no matching candidate keeps the default",
			schema: ir.SchemaIR{Type: ir.TypeString, Pattern: `^[0-9a-f]{8}-[0-9a-f]{4}$`},
			want:   `"example"`,
		},
		{
			name:   "int enum picks the first integral member",
			schema: ir.SchemaIR{Type: ir.TypeInt, EnumValues: []any{float64(3), float64(7)}},
			want:   `3`,
		},
		{
			name:   "int minimum clamps the default up",
			schema: ir.SchemaIR{Type: ir.TypeInt, Minimum: floatPtr(1)},
			want:   `1`,
		},
		{
			name:   "int maximum clamps the default down",
			schema: ir.SchemaIR{Type: ir.TypeInt, Maximum: floatPtr(-1)},
			want:   `-1`,
		},
		{
			name:   "int exclusive minimum excludes the endpoint",
			schema: ir.SchemaIR{Type: ir.TypeInt, ExclusiveMinimum: floatPtr(1)},
			want:   `2`,
		},
		{
			name:   "int multipleOf snaps up to a multiple",
			schema: ir.SchemaIR{Type: ir.TypeInt, Minimum: floatPtr(4), MultipleOf: floatPtr(3)},
			want:   `6`,
		},
		{
			name:   "float enum picks the first member",
			schema: ir.SchemaIR{Type: ir.TypeFloat, EnumValues: []any{float64(0.5), float64(2.5)}},
			want:   `0.5`,
		},
		{
			name:   "float maximum clamps the default",
			schema: ir.SchemaIR{Type: ir.TypeFloat, Maximum: floatPtr(0.25)},
			want:   `0.25`,
		},
		{
			name:   "bool enum picks the member",
			schema: ir.SchemaIR{Type: ir.TypeBool, EnumValues: []any{false}},
			want:   `false`,
		},
		{
			name:   "bool const",
			schema: ir.SchemaIR{Type: ir.TypeBool, Const: boolPtr(false)},
			want:   `false`,
		},
		{
			name:   "collection element enum is honored",
			schema: ir.SchemaIR{Type: ir.TypeString, EnumValues: []any{"info", "warn"}},
			want:   `"info"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := schemaExampleLiteral(tc.schema); got != tc.want {
				t.Errorf("schemaExampleLiteral() = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestSchemaExampleLiteral_CollectionElementEnum exercises the call sites that
// render collection literals: an enum-constrained element must use a listed
// member so the generated ValueStringsAre(stringvalidator.OneOf(…)) validator
// accepts the config (the constraint-validators reference spec's severity
// attribute).
func TestSchemaExampleLiteral_CollectionElementEnum(t *testing.T) {
	elem := ir.SchemaIR{Type: ir.TypeString, EnumValues: []any{"info", "warn", "critical"}}
	attr := ir.AttributeIR{
		Name:     "severity",
		Required: true,
		Schema: ir.SchemaIR{
			Collection: &ir.CollectionType{Kind: ir.List, ElementType: elem},
		},
	}

	value, single := writeHCLAttributeValue(attr)
	if !single {
		t.Fatalf("writeHCLAttributeValue() single = false, want true for a primitive list")
	}
	if want := `[ "info" ]`; value != want {
		t.Errorf("writeHCLAttributeValue() = %s, want %s", value, want)
	}
}

// TestSnapUpToMultiple covers the rounding direction for negative dividends,
// where Go's truncated integer division already rounds toward the next
// multiple up.
func TestSnapUpToMultiple(t *testing.T) {
	cases := []struct {
		v, m int64
		want int64
	}{
		{7, 5, 10},
		{10, 5, 10},
		{-6, 5, -5},
		{-1, 5, 0},
		{-14, 5, -10},
		{4, 0, 4},
	}
	for _, tc := range cases {
		if got := snapUpToMultiple(tc.v, tc.m); got != tc.want {
			t.Errorf("snapUpToMultiple(%d, %d) = %d, want %d", tc.v, tc.m, got, tc.want)
		}
	}
}
