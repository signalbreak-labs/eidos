package transformer

import (
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

func TestInferPlanModifiersNil(t *testing.T) {
	if got := InferPlanModifiers(nil); got != nil {
		t.Errorf("InferPlanModifiers(nil) = %v, want nil", got)
	}
}

func TestInferPlanModifiersEmpty(t *testing.T) {
	schema := &ir.SchemaIR{Type: ir.TypeString}
	if got := InferPlanModifiers(schema); len(got) != 0 {
		t.Errorf("InferPlanModifiers(empty schema) = %v, want no plan modifiers", got)
	}
}

func TestInferPlanModifiersComputed(t *testing.T) {
	tests := []struct {
		name     string
		schema   *ir.SchemaIR
		expected []ir.PlanModifierIR
	}{
		{
			name:     "computed string",
			schema:   &ir.SchemaIR{Type: ir.TypeString, Computed: true},
			expected: []ir.PlanModifierIR{{Type: "stringplanmodifier.UseStateForUnknown"}},
		},
		{
			name:     "computed integer",
			schema:   &ir.SchemaIR{Type: ir.TypeInt, Computed: true},
			expected: []ir.PlanModifierIR{{Type: "int64planmodifier.UseStateForUnknown"}},
		},
		{
			name:     "computed number",
			schema:   &ir.SchemaIR{Type: ir.TypeFloat, Computed: true},
			expected: []ir.PlanModifierIR{{Type: "float64planmodifier.UseStateForUnknown"}},
		},
		{
			name:     "computed boolean",
			schema:   &ir.SchemaIR{Type: ir.TypeBool, Computed: true},
			expected: []ir.PlanModifierIR{{Type: "boolplanmodifier.UseStateForUnknown"}},
		},
		{
			name:     "computed null falls back to generic UseStateForUnknown",
			schema:   &ir.SchemaIR{Type: ir.TypeNull, Computed: true},
			expected: []ir.PlanModifierIR{{Type: "planmodifier.UseStateForUnknown"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferPlanModifiers(tt.schema)
			assertPlanModifiersEqual(t, got, tt.expected)
		})
	}
}

func TestInferPlanModifiersWriteOnly(t *testing.T) {
	schema := &ir.SchemaIR{
		Type:      ir.TypeString,
		WriteOnly: true,
	}
	got := InferPlanModifiers(schema)
	expected := []ir.PlanModifierIR{{Type: "stringplanmodifier.UseStateForUnknown"}}
	assertPlanModifiersEqual(t, got, expected)
}

func TestInferPlanModifiersForceNew(t *testing.T) {
	schema := &ir.SchemaIR{
		Type:     ir.TypeString,
		ForceNew: true,
	}
	got := InferPlanModifiers(schema)
	expected := []ir.PlanModifierIR{{Type: ir.PlanModifierTypeRequiresReplace}}
	assertPlanModifiersEqual(t, got, expected)
}

func TestInferPlanModifiersDefault(t *testing.T) {
	tests := []struct {
		name     string
		schema   *ir.SchemaIR
		expected []ir.PlanModifierIR
	}{
		{
			name:     "string default",
			schema:   &ir.SchemaIR{Type: ir.TypeString, Default: ir.NewDefaultString("hello")},
			expected: []ir.PlanModifierIR{{Type: "stringdefault.StaticString", Args: []string{`"hello"`}}},
		},
		{
			name:     "integer default",
			schema:   &ir.SchemaIR{Type: ir.TypeInt, Default: ir.NewDefaultInt64(42)},
			expected: []ir.PlanModifierIR{{Type: "int64default.StaticInt64", Args: []string{"42"}}},
		},
		{
			name:     "number default",
			schema:   &ir.SchemaIR{Type: ir.TypeFloat, Default: ir.NewDefaultFloat64(3.14)},
			expected: []ir.PlanModifierIR{{Type: "float64default.StaticFloat64", Args: []string{"3.14"}}},
		},
		{
			name:     "boolean default",
			schema:   &ir.SchemaIR{Type: ir.TypeBool, Default: ir.NewDefaultBool(true)},
			expected: []ir.PlanModifierIR{{Type: "booldefault.StaticBool", Args: []string{"true"}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferPlanModifiers(tt.schema)
			assertPlanModifiersEqual(t, got, tt.expected)
		})
	}
}

// TestInferPlanModifiersDefaultTypeMismatch locks in the M-47 fix: a default
// whose value does not match the schema type produces no plan modifier rather
// than a zero/invalid one (empty PlanModifierIR) or a silently coerced
// StaticInt64(0). The fallback for unknown types also emits nothing instead of
// an unquoted bare-identifier `planmodifier.Default` arg.
func TestInferPlanModifiersDefaultTypeMismatch(t *testing.T) {
	tests := []struct {
		name   string
		schema *ir.SchemaIR
	}{
		{"string default on int schema", &ir.SchemaIR{Type: ir.TypeInt, Default: ir.NewDefaultString("abc")}},
		{"int default on string schema", &ir.SchemaIR{Type: ir.TypeString, Default: ir.NewDefaultInt64(7)}},
		{"string default on bool schema", &ir.SchemaIR{Type: ir.TypeBool, Default: ir.NewDefaultString("true")}},
		{"string default on float schema", &ir.SchemaIR{Type: ir.TypeFloat, Default: ir.NewDefaultString("abc")}},
		{"string default on dynamic schema", &ir.SchemaIR{Type: ir.TypeDynamic, Default: ir.NewDefaultString("abc")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferPlanModifiers(tt.schema)
			if len(got) != 0 {
				t.Errorf("expected no plan modifier for type-mismatched default, got %v", got)
			}
		})
	}
}

func TestInferPlanModifiersCombined(t *testing.T) {
	schema := &ir.SchemaIR{
		Type:      ir.TypeString,
		Computed:  true,
		WriteOnly: true,
		ForceNew:  true,
		Default:   ir.NewDefaultString("default"),
	}
	got := InferPlanModifiers(schema)
	expectedTypes := []string{
		"stringplanmodifier.UseStateForUnknown",
		ir.PlanModifierTypeRequiresReplace,
		"stringdefault.StaticString",
	}
	if len(got) != len(expectedTypes) {
		t.Fatalf("expected %d plan modifiers, got %d: %v", len(expectedTypes), len(got), got)
	}
	for i, want := range expectedTypes {
		if got[i].Type != want {
			t.Errorf("plan modifier[%d].Type = %q, want %q", i, got[i].Type, want)
		}
	}
}

func TestApplyPlanModifiers(t *testing.T) {
	schema := &ir.SchemaIR{
		Type:          ir.TypeString,
		Computed:      true,
		PlanModifiers: []ir.PlanModifierIR{{Type: "custom"}},
	}
	ApplyPlanModifiers(schema)
	if len(schema.PlanModifiers) != 2 {
		t.Fatalf("expected 2 plan modifiers after ApplyPlanModifiers, got %v", schema.PlanModifiers)
	}
	if schema.PlanModifiers[0].Type != "custom" {
		t.Errorf("existing plan modifier was not preserved: %v", schema.PlanModifiers[0])
	}
	if schema.PlanModifiers[1].Type != "stringplanmodifier.UseStateForUnknown" {
		t.Errorf("unexpected inferred plan modifier: %v", schema.PlanModifiers[1])
	}
}

func TestInferPlanModifiersForAttributeForceNew(t *testing.T) {
	attr := &ir.AttributeIR{
		Name:     "id",
		Schema:   ir.SchemaIR{Type: ir.TypeString},
		ForceNew: true,
	}
	got := InferPlanModifiersForAttribute(attr)
	expected := []ir.PlanModifierIR{{Type: ir.PlanModifierTypeRequiresReplace}}
	assertPlanModifiersEqual(t, got, expected)
}

func TestInferPlanModifiersForAttributeNil(t *testing.T) {
	if got := InferPlanModifiersForAttribute(nil); got != nil {
		t.Errorf("InferPlanModifiersForAttribute(nil) = %v, want nil", got)
	}
}

func assertPlanModifiersEqual(t *testing.T, got, want []ir.PlanModifierIR) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("plan modifiers length mismatch: got %d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i := range got {
		if got[i].Type != want[i].Type {
			t.Errorf("plan modifier[%d].Type = %q, want %q", i, got[i].Type, want[i].Type)
		}
		if len(got[i].Args) != len(want[i].Args) {
			t.Errorf("plan modifier[%d].Args length = %d, want %d", i, len(got[i].Args), len(want[i].Args))
			continue
		}
		for j := range got[i].Args {
			if got[i].Args[j] != want[i].Args[j] {
				t.Errorf("plan modifier[%d].Args[%d] = %q, want %q", i, j, got[i].Args[j], want[i].Args[j])
			}
		}
	}
}
