package schema

import (
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// renderStandardExprs renders the standard validator exprs for one attribute
// to collapsed source, failing the test when rendering errors.
func renderStandardExprs(t *testing.T, attr ir.AttributeIR, kind string) string {
	t.Helper()
	exprs := standardValidatorExprs(attr, kind)
	if len(exprs) == 0 {
		return ""
	}
	var parts []string
	for _, e := range exprs {
		b, err := astgen.RenderExpr(e)
		if err != nil {
			t.Fatalf("RenderExpr: %v", err)
		}
		parts = append(parts, string(b))
	}
	return strings.Join(parts, "; ")
}

// requiredAttr wraps a schema in a Required attribute — standard validators
// only fire on user-settable attributes.
func requiredAttr(s ir.SchemaIR) ir.AttributeIR {
	return ir.AttributeIR{Name: "x", Required: true, Schema: s}
}

func intPtr(v int) *int { return &v }

func TestStandardValidatorExprs_String(t *testing.T) {
	t.Run("enum", func(t *testing.T) {
		got := renderStandardExprs(t, requiredAttr(ir.SchemaIR{
			Type:       ir.TypeString,
			EnumValues: []any{"alpha", "beta", 3, nil},
		}), "String")
		if got != `stringvalidator.OneOf("alpha", "beta")` {
			t.Errorf("enum: got %q", got)
		}
	})
	t.Run("const", func(t *testing.T) {
		c := any("standard")
		got := renderStandardExprs(t, requiredAttr(ir.SchemaIR{Type: ir.TypeString, Const: &c}), "String")
		if got != `stringvalidator.OneOf("standard")` {
			t.Errorf("const: got %q", got)
		}
	})
	t.Run("length pair and singles", func(t *testing.T) {
		got := renderStandardExprs(t, requiredAttr(ir.SchemaIR{Type: ir.TypeString, MinLength: intPtr(3), MaxLength: intPtr(64)}), "String")
		if got != "stringvalidator.LengthBetween(3, 64)" {
			t.Errorf("length pair: got %q", got)
		}
		got = renderStandardExprs(t, requiredAttr(ir.SchemaIR{Type: ir.TypeString, MinLength: intPtr(1)}), "String")
		if got != "stringvalidator.LengthAtLeast(1)" {
			t.Errorf("minLength only: got %q", got)
		}
		got = renderStandardExprs(t, requiredAttr(ir.SchemaIR{Type: ir.TypeString, MaxLength: intPtr(8)}), "String")
		if got != "stringvalidator.LengthAtMost(8)" {
			t.Errorf("maxLength only: got %q", got)
		}
	})
	t.Run("pattern", func(t *testing.T) {
		got := renderStandardExprs(t, requiredAttr(ir.SchemaIR{Type: ir.TypeString, Pattern: "^[A-Z]+$"}), "String")
		want := `stringvalidator.RegexMatches(regexp.MustCompile("^[A-Z]+$"), "value must match pattern \"^[A-Z]+$\"")`
		if got != want {
			t.Errorf("pattern: got %q", got)
		}
	})
	t.Run("invalid pattern panics at generation time", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Error("expected panic for invalid pattern")
			}
		}()
		_ = standardValidatorExprs(requiredAttr(ir.SchemaIR{Type: ir.TypeString, Pattern: "["}), "String")
	})
	t.Run("non-string enum members skipped", func(t *testing.T) {
		got := renderStandardExprs(t, requiredAttr(ir.SchemaIR{
			Type:       ir.TypeString,
			EnumValues: []any{1, true},
		}), "String")
		if got != "" {
			t.Errorf("expected no validator, got %q", got)
		}
	})
}

func TestStandardValidatorExprs_Int(t *testing.T) {
	t.Run("between", func(t *testing.T) {
		got := renderStandardExprs(t, requiredAttr(ir.SchemaIR{Type: ir.TypeInt, Minimum: floatPtr(1), Maximum: floatPtr(10)}), "Int64")
		if got != "int64validator.Between(1, 10)" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("zero minimum preserved", func(t *testing.T) {
		got := renderStandardExprs(t, requiredAttr(ir.SchemaIR{Type: ir.TypeInt, Minimum: floatPtr(0), Maximum: floatPtr(3)}), "Int64")
		if got != "int64validator.Between(0, 3)" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("single bounds", func(t *testing.T) {
		got := renderStandardExprs(t, requiredAttr(ir.SchemaIR{Type: ir.TypeInt, Minimum: floatPtr(1)}), "Int64")
		if got != "int64validator.AtLeast(1)" {
			t.Errorf("min only: got %q", got)
		}
		got = renderStandardExprs(t, requiredAttr(ir.SchemaIR{Type: ir.TypeInt, Maximum: floatPtr(5)}), "Int64")
		if got != "int64validator.AtMost(5)" {
			t.Errorf("max only: got %q", got)
		}
	})
	t.Run("integral enum only", func(t *testing.T) {
		got := renderStandardExprs(t, requiredAttr(ir.SchemaIR{
			Type:       ir.TypeInt,
			EnumValues: []any{float64(1), float64(2), 2.5, "x"},
		}), "Int64")
		if got != "int64validator.OneOf(1, 2)" {
			t.Errorf("got %q", got)
		}
	})
}

func TestStandardValidatorExprs_Float(t *testing.T) {
	got := renderStandardExprs(t, requiredAttr(ir.SchemaIR{Type: ir.TypeFloat, Minimum: floatPtr(0), Maximum: floatPtr(1)}), "Float64")
	if got != "float64validator.Between(0, 1)" {
		t.Errorf("got %q", got)
	}
	got = renderStandardExprs(t, requiredAttr(ir.SchemaIR{Type: ir.TypeFloat, Minimum: floatPtr(0.5)}), "Float64")
	if got != "float64validator.AtLeast(0.5)" {
		t.Errorf("got %q", got)
	}
}

func TestStandardValidatorExprs_Bool(t *testing.T) {
	t.Run("single-value enum pins Equals", func(t *testing.T) {
		got := renderStandardExprs(t, requiredAttr(ir.SchemaIR{Type: ir.TypeBool, EnumValues: []any{true}}), "Bool")
		if got != "boolvalidator.Equals(true)" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("both-value enum is degenerate", func(t *testing.T) {
		got := renderStandardExprs(t, requiredAttr(ir.SchemaIR{Type: ir.TypeBool, EnumValues: []any{true, false}}), "Bool")
		if got != "" {
			t.Errorf("expected no validator, got %q", got)
		}
	})
}

func TestStandardValidatorExprs_Collections(t *testing.T) {
	list := ir.SchemaIR{
		Type:     ir.TypeString, // element primitive; container-ness comes from Collection
		MinItems: intPtr(1),
		MaxItems: intPtr(5),
		Collection: &ir.CollectionType{
			Kind:        ir.List,
			ElementType: ir.SchemaIR{Type: ir.TypeString, EnumValues: []any{"info", "warn"}},
		},
	}
	t.Run("list size plus element enum", func(t *testing.T) {
		got := renderStandardExprs(t, requiredAttr(list), "List")
		want := `listvalidator.SizeBetween(1, 5); listvalidator.ValueStringsAre(stringvalidator.OneOf("info", "warn"))`
		if got != want {
			t.Errorf("got %q", got)
		}
	})
	t.Run("set and map packages", func(t *testing.T) {
		set := list
		set.Collection = &ir.CollectionType{
			Kind:        ir.Set,
			ElementType: list.Collection.ElementType,
		}
		if got := renderStandardExprs(t, requiredAttr(set), "Set"); !strings.Contains(got, "setvalidator.SizeBetween") {
			t.Errorf("set: got %q", got)
		}
		mp := ir.SchemaIR{
			Type: ir.TypeString,
			Collection: &ir.CollectionType{
				Kind:        ir.Map,
				ElementType: ir.SchemaIR{Type: ir.TypeString, EnumValues: []any{"a"}},
			},
		}
		if got := renderStandardExprs(t, requiredAttr(mp), "Map"); !strings.Contains(got, "mapvalidator.ValueStringsAre") {
			t.Errorf("map: got %q", got)
		}
	})
	t.Run("non-string element enum skipped", func(t *testing.T) {
		nums := ir.SchemaIR{
			Type: ir.TypeInt,
			Collection: &ir.CollectionType{
				Kind:        ir.List,
				ElementType: ir.SchemaIR{Type: ir.TypeInt, EnumValues: []any{float64(1)}},
			},
		}
		if got := renderStandardExprs(t, requiredAttr(nums), "List"); got != "" {
			t.Errorf("expected no validator, got %q", got)
		}
	})
}

func TestStandardValidatorExprs_Gating(t *testing.T) {
	s := ir.SchemaIR{Type: ir.TypeString, EnumValues: []any{"a"}}
	t.Run("computed-only attributes are skipped", func(t *testing.T) {
		attr := ir.AttributeIR{Name: "x", Schema: s} // neither Required nor Optional
		if got := standardValidatorExprs(attr, "String"); got != nil {
			t.Errorf("expected nil for computed-only, got %v", got)
		}
		if HasStandardValidators(attr, "String") {
			t.Error("HasStandardValidators should be false for computed-only")
		}
	})
	t.Run("unknown kind yields nothing", func(t *testing.T) {
		if got := standardValidatorExprs(requiredAttr(s), "Object"); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})
}

// TestStandardValidatorPackages_MirrorExprs asserts the import packages
// exactly mirror the emitted exprs: no package is reported without a
// corresponding call and no call site is left without its package.
func TestStandardValidatorPackages_MirrorExprs(t *testing.T) {
	cases := []struct {
		name string
		attr ir.AttributeIR
		kind string
		want []string
		call string // substring expected in the rendered exprs
	}{
		{"string enum", requiredAttr(ir.SchemaIR{Type: ir.TypeString, EnumValues: []any{"a"}}), "String", []string{"stringvalidator"}, "stringvalidator.OneOf"},
		{"string pattern", requiredAttr(ir.SchemaIR{Type: ir.TypeString, Pattern: "^a$"}), "String", []string{"stringvalidator", "regexp"}, "stringvalidator.RegexMatches"},
		{"int bounds", requiredAttr(ir.SchemaIR{Type: ir.TypeInt, Minimum: floatPtr(0)}), "Int64", []string{"int64validator"}, "int64validator.AtLeast"},
		{"float bounds", requiredAttr(ir.SchemaIR{Type: ir.TypeFloat, Maximum: floatPtr(1)}), "Float64", []string{"float64validator"}, "float64validator.AtMost"},
		{"bool enum", requiredAttr(ir.SchemaIR{Type: ir.TypeBool, EnumValues: []any{false}}), "Bool", []string{"boolvalidator"}, "boolvalidator.Equals"},
		{"list element enum", requiredAttr(ir.SchemaIR{Type: ir.TypeString, Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Type: ir.TypeString, EnumValues: []any{"a"}}}}), "List", []string{"listvalidator", "stringvalidator"}, "listvalidator.ValueStringsAre"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pkgs := StandardValidatorPackages(tc.attr, tc.kind)
			if len(pkgs) != len(tc.want) {
				t.Fatalf("packages = %v, want %v", pkgs, tc.want)
			}
			for i := range pkgs {
				if pkgs[i] != tc.want[i] {
					t.Fatalf("packages = %v, want %v", pkgs, tc.want)
				}
			}
			got := renderStandardExprs(t, tc.attr, tc.kind)
			if !strings.Contains(got, tc.call) {
				t.Errorf("exprs %q missing call %q", got, tc.call)
			}
		})
	}
	t.Run("no exprs no packages", func(t *testing.T) {
		attr := requiredAttr(ir.SchemaIR{Type: ir.TypeString})
		if pkgs := StandardValidatorPackages(attr, "String"); pkgs != nil {
			t.Errorf("expected nil packages, got %v", pkgs)
		}
	})
}

func TestBoundArgZeroAndIntegral(t *testing.T) {
	// A zero bound must render as a present literal, not be dropped: the
	// parser now distinguishes `minimum: 0` from an absent minimum.
	zero := floatPtr(0)
	if got := boundArg(zero); got == nil {
		t.Fatal("zero bound must render")
	}
	b, err := astgen.RenderExpr(boundArg(zero))
	if err != nil {
		t.Fatalf("RenderExpr: %v", err)
	}
	if string(b) != "0" {
		t.Errorf("zero bound rendered as %q", string(b))
	}
	// Integral float bounds render without a decimal point so the untyped
	// constants suit both int64validator and float64validator.
	five := floatPtr(5)
	b, err = astgen.RenderExpr(boundArg(five))
	if err != nil {
		t.Fatalf("RenderExpr: %v", err)
	}
	if string(b) != "5" {
		t.Errorf("integral bound rendered as %q", string(b))
	}
	frac := floatPtr(2.5)
	b, err = astgen.RenderExpr(boundArg(frac))
	if err != nil {
		t.Fatalf("RenderExpr: %v", err)
	}
	if string(b) != "2.5" {
		t.Errorf("fractional bound rendered as %q", string(b))
	}
	if boundArg(nil) != nil {
		t.Error("nil bound must render nil")
	}
}
