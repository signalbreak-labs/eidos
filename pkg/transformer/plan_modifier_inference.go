// Package transformer maps normalized OpenAPI schemas to Terraform Plugin Framework
// representations used by the Eidos provider generator.
package transformer

import (
	"fmt"
	"strconv"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// InferPlanModifiers derives Terraform Plugin Framework plan modifier IR entries
// from the OpenAPI/JSON Schema metadata present on schema. It covers the plan
// modifiers listed in PROJECT_DESIGN.md Section 8.3 for the task scope:
//   - Computed / readOnly fields -> UseStateForUnknown
//   - writeOnly fields           -> UseStateForUnknown
//   - forceNew / x-terraform-force-new -> RequiresReplace
//   - default values             -> typed Static* default plan modifier
func InferPlanModifiers(schema *ir.SchemaIR) []ir.PlanModifierIR {
	if schema == nil {
		return nil
	}

	var modifiers []ir.PlanModifierIR

	// Computed values are set by the API on Read, so preserve the prior state
	// when Terraform sees an unknown value during planning.
	if schema.Computed {
		modifiers = append(modifiers, inferUseStateForUnknown(schema.Type))
	}

	// Write-only arguments are not stored in state. UseStateForUnknown keeps
	// planning stable while Terraform passes the configured value through.
	// Avoid emitting a duplicate modifier when the schema is already Computed.
	if schema.WriteOnly && !schema.Computed {
		modifiers = append(modifiers, inferUseStateForUnknown(schema.Type))
	}

	// forceNew / x-terraform-force-new marks an attribute change as requiring
	// resource replacement.
	if schema.ForceNew {
		modifiers = append(modifiers, ir.PlanModifierIR{
			Type: ir.PlanModifierTypeRequiresReplace,
		})
	}

	// A default value is surfaced as a typed static default plan modifier. A
	// default whose value does not match the schema type (e.g. a string default
	// on an integer schema) produces no modifier rather than a zero/invalid
	// one (M-47).
	if schema.Default != nil {
		if pm, ok := inferDefaultPlanModifier(schema.Type, schema.Default); ok {
			modifiers = append(modifiers, pm)
		}
	}

	return modifiers
}

// ApplyPlanModifiers populates schema.PlanModifiers with the entries returned by
// InferPlanModifiers. Existing plan modifiers are preserved.
func ApplyPlanModifiers(schema *ir.SchemaIR) {
	if schema == nil {
		return
	}
	schema.PlanModifiers = append(schema.PlanModifiers, InferPlanModifiers(schema)...)
}

// InferPlanModifiersForAttribute derives plan modifiers for an attribute inside
// an object schema. In addition to the schema-level metadata handled by
// InferPlanModifiers, it honors attribute-level ForceNew overrides.
func InferPlanModifiersForAttribute(attr *ir.AttributeIR) []ir.PlanModifierIR {
	if attr == nil {
		return nil
	}

	modifiers := InferPlanModifiers(&attr.Schema)

	if attr.ForceNew {
		modifiers = append(modifiers, ir.PlanModifierIR{
			Type: ir.PlanModifierTypeRequiresReplace,
		})
	}

	return modifiers
}

// inferUseStateForUnknown returns the typed UseStateForUnknown plan modifier
// for the given primitive type.
func inferUseStateForUnknown(t ir.PrimitiveType) ir.PlanModifierIR {
	switch t {
	case ir.TypeString:
		return ir.PlanModifierIR{Type: "stringplanmodifier.UseStateForUnknown"}
	case ir.TypeInt:
		return ir.PlanModifierIR{Type: "int64planmodifier.UseStateForUnknown"}
	case ir.TypeFloat:
		return ir.PlanModifierIR{Type: "float64planmodifier.UseStateForUnknown"}
	case ir.TypeBool:
		return ir.PlanModifierIR{Type: "boolplanmodifier.UseStateForUnknown"}
	case ir.TypeDynamic:
		return ir.PlanModifierIR{Type: "dynamicplanmodifier.UseStateForUnknown"}
	default:
		return ir.PlanModifierIR{Type: "planmodifier.UseStateForUnknown"}
	}
}

// inferDefaultPlanModifier returns the typed static-default plan modifier for
// the given primitive type and default value. The bool is false when no valid
// modifier can be produced — when the default is nil, when its value does not
// match the schema type (e.g. a string default on an integer schema), or when
// the schema type has no static-default representation — so the caller skips it
// rather than appending a zero or non-compiling modifier (M-47).
func inferDefaultPlanModifier(t ir.PrimitiveType, defaultValue *any) (ir.PlanModifierIR, bool) {
	if defaultValue == nil {
		return ir.PlanModifierIR{}, false
	}

	value := *defaultValue

	switch t {
	case ir.TypeString:
		s, ok := value.(string)
		if !ok {
			return ir.PlanModifierIR{}, false
		}
		return ir.PlanModifierIR{
			Type: "stringdefault.StaticString",
			Args: []string{strconv.Quote(s)},
		}, true
	case ir.TypeInt:
		n, ok := int64Value(value)
		if !ok {
			return ir.PlanModifierIR{}, false
		}
		return ir.PlanModifierIR{
			Type: "int64default.StaticInt64",
			Args: []string{fmt.Sprintf("%d", n)},
		}, true
	case ir.TypeFloat:
		n, ok := float64Value(value)
		if !ok {
			return ir.PlanModifierIR{}, false
		}
		return ir.PlanModifierIR{
			Type: "float64default.StaticFloat64",
			Args: []string{fmt.Sprintf("%g", n)},
		}, true
	case ir.TypeBool:
		b, ok := boolValue(value)
		if !ok {
			return ir.PlanModifierIR{}, false
		}
		return ir.PlanModifierIR{
			Type: "booldefault.StaticBool",
			Args: []string{fmt.Sprintf("%t", b)},
		}, true
	default:
		// No typed static-default plan modifier exists for dynamic/unknown
		// types. Emitting `planmodifier.Default` with an unquoted %v value would
		// render a bare identifier in generated code and fail to compile, so
		// produce no modifier instead (M-47).
		return ir.PlanModifierIR{}, false
	}
}

// int64Value coerces a numeric default value (int, int64, float64, etc.) to
// int64. The bool is false for non-numeric values so callers do not silently
// coerce `default: "abc"` to StaticInt64(0) (M-47).
func int64Value(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int8:
		return int64(n), true
	case int16:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case uint:
		//nolint:gosec // uint values are validated JSON defaults that never exceed int64 in this domain.
		return int64(n), true
	case uint8:
		return int64(n), true
	case uint16:
		return int64(n), true
	case uint32:
		return int64(n), true
	case uint64:
		//nolint:gosec // uint64 values are validated JSON defaults that never exceed int64 in this domain.
		return int64(n), true
	case float32:
		return int64(n), true
	case float64:
		return int64(n), true
	}
	return 0, false
}

// float64Value coerces a numeric default value to float64, reporting ok=false
// for non-numeric values (M-47).
func float64Value(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	case int16:
		return float64(n), true
	case int8:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint64:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint8:
		return float64(n), true
	}
	return 0, false
}

// boolValue reports a default value as a bool, reporting ok=false for non-bool
// values so a string/numeric default is not silently coerced (M-47).
func boolValue(v any) (bool, bool) {
	if b, ok := v.(bool); ok {
		return b, true
	}
	return false, false
}
