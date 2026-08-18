package generator

import (
	"strings"
	"testing"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// TestNewBuildVersions asserts NewBuildVersions populates every field with the
// pinned package default.
func TestNewBuildVersions(t *testing.T) {
	v := NewBuildVersions()
	want := BuildVersions{
		FrameworkVersion: TerraformPluginFrameworkVersion,
		PluginGoVersion:  TerraformPluginGoVersion,
		PluginLogVersion: TerraformPluginLogVersion,
		TestingVersion:   TerraformPluginTestingVersion,
	}
	if v != want {
		t.Errorf("NewBuildVersions() = %+v, want %+v", v, want)
	}
}

// TestQuoteEach asserts quoteEach double-quotes every element without mutating
// the input.
func TestQuoteEach(t *testing.T) {
	in := []string{"a", "b c"}
	got := quoteEach(in)
	if len(got) != 2 || got[0] != `"a"` || got[1] != `"b c"` {
		t.Errorf("quoteEach(%v) = %v", in, got)
	}
	if in[0] != "a" || in[1] != "b c" {
		t.Errorf("quoteEach mutated its input: %v", in)
	}
	if len(quoteEach(nil)) != 0 {
		t.Error("quoteEach(nil) should return an empty slice")
	}
}

// TestGoDurationExpr covers every branch of goDurationExpr from zero through
// the sub-microsecond fallback.
func TestGoDurationExpr(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0"},
		{2 * time.Hour, "2 * time.Hour"},
		{90 * time.Minute, "90 * time.Minute"}, // not a whole number of hours
		{45 * time.Second, "45 * time.Second"},
		{500 * time.Millisecond, "500 * time.Millisecond"},
		{1500 * time.Microsecond, "1500 * time.Microsecond"},
		{time.Duration(1), "1 * time.Nanosecond"}, // 1ns falls through to nanos
	}
	for _, tc := range cases {
		if got := goDurationExpr(tc.d); got != tc.want {
			t.Errorf("goDurationExpr(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

// TestCollectionKindLabel covers the three recognized kinds plus the
// capitalize fallback.
func TestCollectionKindLabel(t *testing.T) {
	cases := []struct {
		k    ir.CollectionKind
		want string
	}{
		{ir.List, "List"},
		{ir.Set, "Set"},
		{ir.Map, "Map"},
		{ir.CollectionKind("tuple"), "Tuple"},
	}
	for _, tc := range cases {
		if got := collectionKindLabel(tc.k); got != tc.want {
			t.Errorf("collectionKindLabel(%q) = %q, want %q", tc.k, got, tc.want)
		}
	}
}

// TestValidateProviderPrimitiveType covers the accepted primitive types, the
// null/empty/unrecognized error paths.
func TestValidateProviderPrimitiveType(t *testing.T) {
	for _, typ := range []ir.PrimitiveType{ir.TypeString, ir.TypeInt, ir.TypeFloat, ir.TypeBool, ir.TypeDynamic} {
		if err := validateProviderPrimitiveType(ir.SchemaIR{Type: typ}, "test"); err != nil {
			t.Errorf("validateProviderPrimitiveType(%q) = %v, want nil", typ, err)
		}
	}
	bad := []struct {
		typ  ir.PrimitiveType
		want string
	}{
		{ir.TypeNull, "unsupported primitive type"},
		{"", "no recognizable type"},
		{"custom", "unsupported primitive type"},
	}
	for _, tc := range bad {
		err := validateProviderPrimitiveType(ir.SchemaIR{Type: tc.typ}, "test")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("validateProviderPrimitiveType(%q) = %v, want error containing %q", tc.typ, err, tc.want)
		}
	}
}

// TestTerraformTestVariableValue covers the collection and primitive branches
// of terraformTestVariableValue.
func TestTerraformTestVariableValue(t *testing.T) {
	strAttr := func(c *ir.CollectionType) ir.AttributeIR {
		return ir.AttributeIR{Schema: ir.SchemaIR{Collection: c}}
	}
	listStr := &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Type: ir.TypeString}}
	mapStr := &ir.CollectionType{Kind: ir.Map, ElementType: ir.SchemaIR{Type: ir.TypeString}}

	cases := []struct {
		name string
		attr ir.AttributeIR
		want string
	}{
		{"list-of-string", strAttr(listStr), `[ "example" ]`},
		{"set-of-string", strAttr(&ir.CollectionType{Kind: ir.Set, ElementType: ir.SchemaIR{Type: ir.TypeString}}), `[ "example" ]`},
		{"map-of-string", strAttr(mapStr), `{ key = "example" }`},
		// List/set with a non-primitive element falls through to the empty list.
		{"list-of-object", strAttr(&ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Attributes: []ir.AttributeIR{{Name: "x"}}}}), "[]"},
		// Map with a non-primitive element also falls through to [].
		{"map-of-object", strAttr(&ir.CollectionType{Kind: ir.Map, ElementType: ir.SchemaIR{Attributes: []ir.AttributeIR{{Name: "x"}}}}), "[]"},
		{"primitive-string", ir.AttributeIR{Schema: ir.SchemaIR{Type: ir.TypeString}}, `"example"`},
		{"primitive-int", ir.AttributeIR{Schema: ir.SchemaIR{Type: ir.TypeInt}}, "1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := terraformTestVariableValue(tc.attr); got != tc.want {
				t.Errorf("terraformTestVariableValue() = %q, want %q", got, tc.want)
			}
		})
	}
}
