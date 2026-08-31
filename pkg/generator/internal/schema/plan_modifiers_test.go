package schema

import (
	"go/ast"
	"reflect"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// renderPlanModifiers renders the PlanModifiers field expression(s) added to a
// nil slice, failing the test when rendering errors. Empty output means no
// field was added.
func renderPlanModifiers(t *testing.T, attr ir.AttributeIR, kind string) []string {
	t.Helper()
	exprs := AddPlanModifiers(nil, attr, kind)
	out := make([]string, 0, len(exprs))
	for _, e := range exprs {
		b, err := astgen.RenderExpr(e)
		if err != nil {
			t.Fatalf("RenderExpr: %v", err)
		}
		out = append(out, string(b))
	}
	return out
}

// planModifierAttr returns a config-settable attribute with the given entries.
func planModifierAttr(pms ...ir.PlanModifierIR) ir.AttributeIR {
	return ir.AttributeIR{Name: "x", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}, PlanModifiers: pms}
}

func TestAddPlanModifiers_Emission(t *testing.T) {
	t.Run("no modifiers leaves elems untouched", func(t *testing.T) {
		got := renderPlanModifiers(t, planModifierAttr(), "String")
		if len(got) != 0 {
			t.Errorf("expected no PlanModifiers field, got %v", got)
		}
	})
	t.Run("force_new override emits typed RequiresReplace", func(t *testing.T) {
		attr := planModifierAttr()
		attr.ForceNew = true
		got := renderPlanModifiers(t, attr, "String")
		want := []string{"PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("IR RequiresReplace resolves to the attribute kind's typed constructor", func(t *testing.T) {
		attr := planModifierAttr(ir.PlanModifierIR{Type: ir.PlanModifierTypeRequiresReplace})
		got := renderPlanModifiers(t, attr, "Int64")
		want := []string{"PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()}"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("typed UseStateForUnknown passes through", func(t *testing.T) {
		attr := planModifierAttr(ir.PlanModifierIR{Type: "stringplanmodifier.UseStateForUnknown"})
		got := renderPlanModifiers(t, attr, "String")
		want := []string{"PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("force_new leads the IR entries", func(t *testing.T) {
		attr := planModifierAttr(
			ir.PlanModifierIR{Type: "stringplanmodifier.UseStateForUnknown"},
			ir.PlanModifierIR{Type: ir.PlanModifierTypeRequiresReplace},
		)
		attr.ForceNew = true
		got := renderPlanModifiers(t, attr, "String")
		want := []string{"PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace(), stringplanmodifier.UseStateForUnknown(), stringplanmodifier.RequiresReplace()}"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}

// TestAddPlanModifiers_FailLoud covers the mismatches that would otherwise
// silently emit a modifier that never applies: the panics are recovered by
// renderFileSafely and surface as render errors.
func TestAddPlanModifiers_FailLoud(t *testing.T) {
	cases := []struct {
		name string
		attr ir.AttributeIR
		kind string
	}{
		{
			name: "unknown kind",
			attr: planModifierAttr(ir.PlanModifierIR{Type: ir.PlanModifierTypeRequiresReplace}),
			kind: "Wat",
		},
		{
			name: "typed package does not match attribute kind",
			attr: planModifierAttr(ir.PlanModifierIR{Type: "stringplanmodifier.UseStateForUnknown"}),
			kind: "Int64",
		},
		{
			name: "argument-bearing modifier has no emission path",
			attr: planModifierAttr(ir.PlanModifierIR{Type: "stringdefault.StaticString", Args: []string{`"x"`}}),
			kind: "String",
		},
		{
			name: "unknown constructor",
			attr: planModifierAttr(ir.PlanModifierIR{Type: "stringplanmodifier.RequiresReplaceUnless"}),
			kind: "String",
		},
		{
			name: "bare non-constructor name",
			attr: planModifierAttr(ir.PlanModifierIR{Type: "UseStateForUnknown"}),
			kind: "String",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("expected panic for %s", tc.name)
				}
			}()
			AddPlanModifiers(nil, tc.attr, tc.kind)
		})
	}
}

// TestUsedPlanModifierImports verifies import derivation from the rendered
// schema AST: the shared planmodifier package plus each typed package whose
// constructors actually appear, and nothing for expressions without
// modifiers (the exactness that keeps a dropped attribute from registering an
// unused import).
func TestUsedPlanModifierImports(t *testing.T) {
	t.Run("modifiers across kinds", func(t *testing.T) {
		exprs := make([]ast.Expr, 0, 4)
		exprs = append(exprs, AddPlanModifiers(nil, ir.AttributeIR{
			Name: "name", Required: true, ForceNew: true,
			Schema: ir.SchemaIR{Type: ir.TypeString},
		}, "String")...)
		exprs = append(exprs, AddPlanModifiers(nil, ir.AttributeIR{
			Name:          "tags",
			Optional:      true,
			Schema:        ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.Set, ElementType: ir.SchemaIR{Type: ir.TypeString}}},
			PlanModifiers: []ir.PlanModifierIR{{Type: ir.PlanModifierTypeRequiresReplace}},
		}, "Set")...)
		exprs = append(exprs, AddPlanModifiers(nil, ir.AttributeIR{
			Name: "count", Required: true, Schema: ir.SchemaIR{Type: ir.TypeInt}, ForceNew: true,
		}, "Int64")...)
		// A plain attribute without modifiers contributes nothing.
		exprs = append(exprs, AddPlanModifiers(nil, ir.AttributeIR{
			Name: "plain", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeBool},
		}, "Bool")...)

		got := UsedPlanModifierImports(exprs)
		want := [][2]string{
			{"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier", "planmodifier"},
			{"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier", "int64planmodifier"},
			{"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier", "setplanmodifier"},
			{"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier", "stringplanmodifier"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("no modifiers yields nil", func(t *testing.T) {
		exprs := []ast.Expr{astgen.KeyValue("Required", astgen.BoolLit(true))}
		if got := UsedPlanModifierImports(exprs); got != nil {
			t.Errorf("got %v for expressions without modifiers, want nil", got)
		}
	})
	t.Run("dropped attribute yields nil", func(t *testing.T) {
		// Simulates a force_new attribute the renderer drops (a nested
		// dynamic inside a collection): it never contributes expressions, so
		// it must not contribute imports either.
		if got := UsedPlanModifierImports(nil); got != nil {
			t.Errorf("got %v for no expressions, want nil", got)
		}
	})
}
