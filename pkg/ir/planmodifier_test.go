package ir

import (
	"testing"
)

func TestPlanModifierIRRoundTrip(t *testing.T) {
	assertJSONRoundTrip(t, PlanModifierIR{
		Type: "stringplanmodifier.UseStateForUnknown",
	})

	assertJSONRoundTrip(t, PlanModifierIR{
		Type: "planmodifier.RequiresReplace",
	})

	assertJSONRoundTrip(t, PlanModifierIR{
		Type: "stringdefault.StaticString",
		Args: []string{"default-value"},
	})

	assertJSONRoundTrip(t, PlanModifierIR{
		Type: "int64default.StaticInt64",
		Args: []string{"42"},
	})

	assertJSONRoundTrip(t, PlanModifierIR{
		Type: "",
		Args: nil,
	})
}
