package transformer

import (
	"fmt"
	"math"
	"strconv"

	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// ApplyAdvancedKeywords maps JSON Schema 2020-12 advanced validation keywords
// (not, const, if/then/else, dependentRequired, dependentSchemas,
// patternProperties, propertyNames, unevaluatedProperties) from the
// version-agnostic Schema into the IR schema node. It populates both the
// declarative IR fields and any attribute-level Terraform-plugin-framework
// validators that can be inferred from the keyword values.
//
// Keywords that have no native framework equivalent (patternProperties and
// unevaluatedProperties) are stored declaratively on SchemaIR so downstream
// generators can emit custom validation. patternProperties does not produce a
// framework validator; propertyNames emits mapvalidator.KeysAre when it carries
// a pattern.
//
// Callers should invoke ApplyAdvancedKeywords after the base schema type has
// been resolved so that validators can be typed correctly. diags collects
// non-fatal diagnostics; nil is accepted and ignored.
//
// Deprecated: unreachable from production. The live pipeline maps advanced
// keywords in schemaIRFromSpecRecursive (resource_schema.go) instead; this
// function and its helpers are retained only for their test coverage and must
// not be extended (M-7). See AUDIT.md.
func ApplyAdvancedKeywords(s *Schema, target *ir.SchemaIR, diags *diagnostics.Diagnostics) error {
	if s == nil || target == nil {
		return nil
	}

	applyConst(s, target)
	if err := applyNot(s, target, diags); err != nil {
		return fmt.Errorf("not: %w", err)
	}
	if err := applyIfThenElse(s, target, diags); err != nil {
		return fmt.Errorf("if/then/else: %w", err)
	}
	applyDependentRequired(s, target)
	if err := applyDependentSchemas(s, target, diags); err != nil {
		return fmt.Errorf("dependentSchemas: %w", err)
	}
	applyStringKeywords(s, target)
	applyNumericKeywords(s, target, diags)
	applyArrayKeywords(s, target)
	if err := applyObjectKeywords(s, target, diags); err != nil {
		return fmt.Errorf("object keywords: %w", err)
	}
	return nil
}

// applyConst maps the JSON Schema "const" keyword. The exact value is stored
// on SchemaIR.Const and, when the schema type is known, an equivalent
// type-specific OneOf validator is appended.
//
// Values that are unsupported by the Terraform plugin framework (bool,
// object, array) or that do not match the resolved schema type are recorded
// only in SchemaIR.Const; downstream emitters must enforce those via a custom
// validator.
func applyConst(s *Schema, target *ir.SchemaIR) {
	if s.Const == nil {
		return
	}

	v := s.Const
	target.Const = &v

	validators := constValidators(target.Type, s.Const)
	target.Validators = append(target.Validators, validators...)
}

// applyNot maps the JSON Schema "not" keyword. The negated schema is stored on
// SchemaIR.Not. When the negated schema is a simple enum-style schema, a
// type-specific NoneOf validator is appended so the common case of "not:
// {enum: [...]}" can be enforced by the framework without requiring a generated
// custom validator.
func applyNot(s *Schema, target *ir.SchemaIR, diags *diagnostics.Diagnostics) error {
	if s.Not == nil {
		return nil
	}

	negated, err := schemaToIR(s.Not, diags)
	if err != nil {
		return err
	}
	target.Not = negated

	// Use the raw enum values from the negated schema so that the validator
	// is produced even when schemaToIR does not preserve enum metadata on the
	// converted Not schema. This intentionally keeps the NoneOf validator args
	// tied to the user-supplied enum rather than the converted IR's
	// EnumValues, which may be filtered or normalized in future passes.
	if len(s.Not.Enum) > 0 {
		target.Validators = append(target.Validators, noneOfValidators(target.Type, s.Not.Enum)...)
	}
	return nil
}

// applyIfThenElse maps the JSON Schema "if", "then", and "else" keywords into
// the conditional schema fields on SchemaIR. These fields drive generation of
// resource-level ConfigValidators in downstream emitters.
func applyIfThenElse(s *Schema, target *ir.SchemaIR, diags *diagnostics.Diagnostics) error {
	if s.If != nil {
		ifSchema, err := schemaToIR(s.If, diags)
		if err != nil {
			return fmt.Errorf("if: %w", err)
		}
		target.IfSchema = ifSchema
	}
	if s.Then != nil {
		thenSchema, err := schemaToIR(s.Then, diags)
		if err != nil {
			return fmt.Errorf("then: %w", err)
		}
		target.ThenSchema = thenSchema
	}
	if s.Else != nil {
		elseSchema, err := schemaToIR(s.Else, diags)
		if err != nil {
			return fmt.Errorf("else: %w", err)
		}
		target.ElseSchema = elseSchema
	}
	return nil
}

// applyDependentRequired maps the JSON Schema "dependentRequired" keyword into
// SchemaIR.DependentRequired. The map describes, for each trigger property,
// the list of additional properties that become required when the trigger is
// present. Downstream emitters turn this into resource-level
// ConfigValidators or per-attribute AlsoRequires validators.
func applyDependentRequired(s *Schema, target *ir.SchemaIR) {
	if len(s.DependentRequired) == 0 {
		return
	}

	out := make(map[string][]string, len(s.DependentRequired))
	for trigger, required := range s.DependentRequired {
		dst := make([]string, len(required))
		copy(dst, required)
		out[trigger] = dst
	}
	target.DependentRequired = out
}

// applyDependentSchemas maps the JSON Schema "dependentSchemas" keyword into
// SchemaIR.DependentSchemas. Each trigger property names a schema that must be
// satisfied when the trigger is present. These become resource-level
// ConfigValidators during code generation.
func applyDependentSchemas(s *Schema, target *ir.SchemaIR, diags *diagnostics.Diagnostics) error {
	if len(s.DependentSchemas) == 0 {
		return nil
	}

	out := make(map[string]*ir.SchemaIR, len(s.DependentSchemas))
	for trigger, schema := range s.DependentSchemas {
		converted, err := schemaToIR(schema, diags)
		if err != nil {
			return fmt.Errorf("trigger %q: %w", trigger, err)
		}
		out[trigger] = converted
	}
	target.DependentSchemas = out
	return nil
}

// applyStringKeywords maps JSON Schema string validation keywords (pattern,
// minLength, maxLength) into the IR. Declarative fields are always copied; when
// the schema is known to be a string, equivalent Terraform-plugin-framework
// validators are appended. Pattern is stored declaratively on SchemaIR even when
// the type is not string so the constraint is preserved for downstream custom
// validators; only TypeString schemas receive a framework RegexMatches
// validator.
func applyStringKeywords(s *Schema, target *ir.SchemaIR) {
	if s.Pattern != "" {
		target.Pattern = s.Pattern
		if target.Type == ir.TypeString {
			target.Validators = append(target.Validators, ir.ValidatorIR{
				Type: "stringvalidator.RegexMatches",
				Args: []string{s.Pattern},
			})
		}
	}

	target.MinLength = s.MinLength
	target.MaxLength = s.MaxLength
	if target.Type == ir.TypeString && (s.MinLength != nil || s.MaxLength != nil) {
		target.Validators = append(target.Validators, sizeValidators("stringvalidator", "Length", s.MinLength, s.MaxLength)...)
	}
}

// sizeValidators returns the framework size/length validators for the supplied
// bounds. It unifies the three formerly-duplicated stringLengthValidators,
// collectionSizeValidators, and mapSizeValidators functions, which differed only
// in the validator-type prefix and the measure word ("Length" for strings,
// "Size" for lists and maps) (L-98). Both bounds produce a Between validator; a
// single bound uses AtLeast or AtMost.
func sizeValidators(prefix, measure string, minVal, maxVal *int) []ir.ValidatorIR {
	switch {
	case minVal != nil && maxVal != nil:
		return []ir.ValidatorIR{{
			Type: prefix + "." + measure + "Between",
			Args: []string{strconv.Itoa(*minVal), strconv.Itoa(*maxVal)},
		}}
	case minVal != nil:
		return []ir.ValidatorIR{{
			Type: prefix + "." + measure + "AtLeast",
			Args: []string{strconv.Itoa(*minVal)},
		}}
	case maxVal != nil:
		return []ir.ValidatorIR{{
			Type: prefix + "." + measure + "AtMost",
			Args: []string{strconv.Itoa(*maxVal)},
		}}
	}
	return nil
}

// applyNumericKeywords maps JSON Schema numeric validation keywords (minimum,
// maximum, exclusiveMinimum, exclusiveMaximum, multipleOf) into the IR.
// Declarative bounds are always copied; validators are inferred only when the
// schema type is known to be integer or number.
func applyNumericKeywords(s *Schema, target *ir.SchemaIR, diags *diagnostics.Diagnostics) {
	target.Minimum = s.Minimum
	target.Maximum = s.Maximum
	target.ExclusiveMinimum = s.ExclusiveMinimum
	target.ExclusiveMaximum = s.ExclusiveMaximum
	target.MultipleOf = s.MultipleOf

	switch target.Type {
	case ir.TypeInt:
		target.Validators = append(target.Validators, intValidators(s, diags)...)
	case ir.TypeFloat:
		target.Validators = append(target.Validators, floatValidators(s)...)
	}
}

// intValidators returns Terraform-plugin-framework int64 validators inferred
// from numeric keywords. Inclusive and exclusive bounds of the same direction are
// merged into a single effective bound, so overlapping minimum/exclusiveMinimum
// or maximum/exclusiveMaximum pairs do not produce redundant validators.
// Exclusive bounds on integers are expressed with the next valid integer
// (floor+1 for min, ceil-1 for max). multipleOf becomes a custom validator when
// it is a positive whole number; non-integer multipleOf values on integer types
// are dropped with a diagnostic warning.
func intValidators(s *Schema, diags *diagnostics.Diagnostics) []ir.ValidatorIR {
	var out []ir.ValidatorIR

	lower, upper := effectiveIntBounds(s, diags)

	switch {
	case lower != nil && upper != nil:
		out = append(out, ir.ValidatorIR{
			Type: "int64validator.Between",
			Args: []string{strconv.FormatInt(*lower, 10), strconv.FormatInt(*upper, 10)},
		})
	case lower != nil:
		out = append(out, ir.ValidatorIR{
			Type: "int64validator.AtLeast",
			Args: []string{strconv.FormatInt(*lower, 10)},
		})
	case upper != nil:
		out = append(out, ir.ValidatorIR{
			Type: "int64validator.AtMost",
			Args: []string{strconv.FormatInt(*upper, 10)},
		})
	}

	if s.MultipleOf != nil {
		if n, ok := numericInt(*s.MultipleOf); ok && n > 0 {
			out = append(out, ir.ValidatorIR{
				Type: "validators.Int64MultipleOfValidator",
				Args: []string{strconv.FormatInt(n, 10)},
			})
		} else if diags != nil {
			*diags = diags.Append(diagnostics.Diagnostic{
				Severity: diagnostics.Warning,
				Summary:  "multipleOf value dropped for integer schema",
				Detail:   fmt.Sprintf("multipleOf=%v is not a positive whole number; integer multipleOf validation requires a whole-number value.", *s.MultipleOf),
			})
		}
	}

	return out
}

// effectiveIntBounds computes the effective inclusive int64 lower and upper
// bounds for an integer schema, collapsing minimum/exclusiveMinimum and
// maximum/exclusiveMaximum. For integers, a non-whole inclusive minimum is
// rounded up (e.g. minimum=0.5 means x >= 1) and a non-whole inclusive maximum
// is rounded down (e.g. maximum=0.5 means x <= 0). Exclusive minimum uses
// floor+1 and exclusive maximum uses ceil-1. Bounds that cannot be represented
// as int64 are dropped with a diagnostic warning (M-45).
func effectiveIntBounds(s *Schema, diags *diagnostics.Diagnostics) (lower, upper *int64) {
	if s.Minimum != nil {
		if l, ok := safeCeilInt64(*s.Minimum); ok {
			lower = &l
		} else {
			warnIntRangeDropped(diags, "minimum", *s.Minimum)
		}
	}
	if s.ExclusiveMinimum != nil {
		if l, ok := safeFloorInt64(*s.ExclusiveMinimum); ok {
			l++
			if lower == nil || l > *lower {
				lower = &l
			}
		} else {
			warnIntRangeDropped(diags, "exclusiveMinimum", *s.ExclusiveMinimum)
		}
	}
	if s.Maximum != nil {
		if u, ok := safeFloorInt64(*s.Maximum); ok {
			upper = &u
		} else {
			warnIntRangeDropped(diags, "maximum", *s.Maximum)
		}
	}
	if s.ExclusiveMaximum != nil {
		if u, ok := safeCeilInt64(*s.ExclusiveMaximum); ok {
			u--
			if upper == nil || u < *upper {
				upper = &u
			}
		} else {
			warnIntRangeDropped(diags, "exclusiveMaximum", *s.ExclusiveMaximum)
		}
	}
	return lower, upper
}

// warnIntRangeDropped appends a warning diagnostic noting that an integer bound
// could not be represented as int64 and was skipped to avoid a silently wrong
// validator (M-45). It is a no-op when diags is nil.
func warnIntRangeDropped(diags *diagnostics.Diagnostics, label string, value any) {
	if diags == nil {
		return
	}
	*diags = diags.Append(diagnostics.Diagnostic{
		Severity: diagnostics.Warning,
		Summary:  fmt.Sprintf("integer %s dropped: out of int64 range", label),
		Detail: fmt.Sprintf(
			"%s=%v is outside the representable int64 range; the int64 validator was skipped to avoid a silently wrong bound.",
			label, value,
		),
	})
}

// floatValidators returns Terraform-plugin-framework float64 validators for
// inclusive bounds. Exclusive bounds and multipleOf have no built-in framework
// validator, so they are represented by custom validator IR that downstream
// generators can emit.
func floatValidators(s *Schema) []ir.ValidatorIR {
	var out []ir.ValidatorIR

	if s.Minimum != nil && s.Maximum != nil {
		out = append(out, ir.ValidatorIR{
			Type: "float64validator.Between",
			Args: []string{formatFloat(*s.Minimum), formatFloat(*s.Maximum)},
		})
	} else {
		if s.Minimum != nil {
			out = append(out, ir.ValidatorIR{
				Type: "float64validator.AtLeast",
				Args: []string{formatFloat(*s.Minimum)},
			})
		}
		if s.Maximum != nil {
			out = append(out, ir.ValidatorIR{
				Type: "float64validator.AtMost",
				Args: []string{formatFloat(*s.Maximum)},
			})
		}
	}

	if s.ExclusiveMinimum != nil {
		out = append(out, ir.ValidatorIR{
			Type: "validators.ExclusiveMinimumValidator",
			Args: []string{formatFloat(*s.ExclusiveMinimum)},
		})
	}
	if s.ExclusiveMaximum != nil {
		out = append(out, ir.ValidatorIR{
			Type: "validators.ExclusiveMaximumValidator",
			Args: []string{formatFloat(*s.ExclusiveMaximum)},
		})
	}

	if s.MultipleOf != nil {
		out = append(out, ir.ValidatorIR{
			Type: "validators.Float64MultipleOfValidator",
			Args: []string{formatFloat(*s.MultipleOf)},
		})
	}

	return out
}

// formatFloat returns a compact string representation of a float64 bound.
// Whole-numbered floats serialize without a decimal point (e.g. "0" and "100")
// because strconv.FormatFloat uses precision -1; non-whole bounds keep their
// decimal form (e.g. "0.5"). Downstream custom validators parse these strings
// back into float64 values, so the compact form is sufficient.
func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// applyArrayKeywords maps JSON Schema array validation keywords (minItems,
// maxItems) into the IR. List-type collections gain Terraform-plugin-framework
// listvalidator validators.
func applyArrayKeywords(s *Schema, target *ir.SchemaIR) {
	target.MinItems = s.MinItems
	target.MaxItems = s.MaxItems

	if target.Collection != nil && (s.MinItems != nil || s.MaxItems != nil) {
		target.Validators = append(target.Validators, sizeValidators("listvalidator", "Size", s.MinItems, s.MaxItems)...)
	}
}

// applyObjectKeywords maps JSON Schema object validation keywords
// (patternProperties, propertyNames, unevaluatedProperties, minProperties,
// maxProperties) into the IR. Nested schemas are recursively converted.
//
// patternProperties and unevaluatedProperties are stored declaratively on
// SchemaIR because the Terraform plugin framework has no native validator for
// these JSON Schema 2020-12 keywords. Downstream generators that need to enforce
// them must inspect SchemaIR.PatternProperties and SchemaIR.UnevaluatedProperties
// and emit custom validation logic.
//
// propertyNames with a pattern maps to mapvalidator.KeysAre. When the object is
// free-form (no explicit Attributes), minProperties/maxProperties map to
// mapvalidator.SizeBetween/AtLeast/AtMost. Objects that already have fixed
// Attributes ignore minProperties/maxProperties because the property count is
// determined by the fixed schema; a diagnostic is emitted when this happens.
func applyObjectKeywords(s *Schema, target *ir.SchemaIR, diags *diagnostics.Diagnostics) error {
	target.MinProperties = s.MinProperties
	target.MaxProperties = s.MaxProperties

	if len(s.PatternProperties) > 0 {
		out := make(map[string]*ir.SchemaIR, len(s.PatternProperties))
		for pattern, schema := range s.PatternProperties {
			converted, err := schemaToIR(schema, diags)
			if err != nil {
				return fmt.Errorf("patternProperties %q: %w", pattern, err)
			}
			out[pattern] = converted
		}
		target.PatternProperties = out
	}

	if s.PropertyNames != nil {
		converted, err := schemaToIR(s.PropertyNames, diags)
		if err != nil {
			return fmt.Errorf("propertyNames: %w", err)
		}
		target.PropertyNames = converted

		if converted.Pattern != "" {
			target.Validators = append(target.Validators, ir.ValidatorIR{
				Type: "mapvalidator.KeysAre",
				Args: []string{"stringvalidator.RegexMatches", converted.Pattern},
			})
		}
	}

	if s.UnevaluatedProperties != nil {
		converted, err := schemaToIR(s.UnevaluatedProperties, diags)
		if err != nil {
			return fmt.Errorf("unevaluatedProperties: %w", err)
		}
		target.UnevaluatedProperties = converted
	}

	if len(target.Attributes) == 0 && (s.MinProperties != nil || s.MaxProperties != nil) {
		target.Validators = append(target.Validators, sizeValidators("mapvalidator", "Size", s.MinProperties, s.MaxProperties)...)
	} else if len(target.Attributes) > 0 && (s.MinProperties != nil || s.MaxProperties != nil) && diags != nil {
		*diags = diags.Append(diagnostics.Diagnostic{
			Severity: diagnostics.Info,
			Summary:  "minProperties/maxProperties ignored for fixed-schema object",
			Detail:   "The object defines explicit properties, so minProperties/maxProperties constraints are not enforced via mapvalidator size validators.",
		})
	}
	return nil
}

// constValidators returns the Terraform-plugin-framework validator that
// enforces an exact const value for the given primitive type. OneOf with a
// single value is the framework's idiomatic way to express a const constraint.
// When the type is unknown or unsupported, an empty slice is returned so the
// constraint is still represented by SchemaIR.Const.
func constValidators(t ir.PrimitiveType, value interface{}) []ir.ValidatorIR {
	switch t {
	case ir.TypeString:
		str, ok := value.(string)
		if !ok {
			return nil
		}
		return []ir.ValidatorIR{{Type: "stringvalidator.OneOf", Args: []string{str}}}
	case ir.TypeInt:
		n, ok := numericInt(value)
		if !ok {
			return nil
		}
		return []ir.ValidatorIR{{Type: "int64validator.OneOf", Args: []string{strconv.FormatInt(n, 10)}}}
	case ir.TypeFloat:
		n, ok := numericFloat(value)
		if !ok {
			return nil
		}
		return []ir.ValidatorIR{{Type: "float64validator.OneOf", Args: []string{strconv.FormatFloat(n, 'f', -1, 64)}}}
	case ir.TypeBool:
		// The Terraform plugin framework does not provide a bool OneOf validator,
		// so the constraint is represented only by SchemaIR.Const.
		return nil
	default:
		return nil
	}
}

// noneOfValidators returns a Terraform-plugin-framework NoneOf validator when
// the negated schema enumerates forbidden values. Unsupported types fall back
// to an empty slice, leaving the constraint to be enforced by a generated
// custom NotValidator.
func noneOfValidators(t ir.PrimitiveType, values []interface{}) []ir.ValidatorIR {
	if len(values) == 0 {
		return nil
	}

	switch t {
	case ir.TypeString:
		args := make([]string, 0, len(values))
		for _, v := range values {
			if s, ok := v.(string); ok {
				args = append(args, s)
			}
		}
		if len(args) == 0 {
			return nil
		}
		return []ir.ValidatorIR{{Type: "stringvalidator.NoneOf", Args: args}}
	case ir.TypeInt:
		args := make([]string, 0, len(values))
		for _, v := range values {
			if n, ok := numericInt(v); ok {
				args = append(args, strconv.FormatInt(n, 10))
			}
		}
		if len(args) == 0 {
			return nil
		}
		return []ir.ValidatorIR{{Type: "int64validator.NoneOf", Args: args}}
	case ir.TypeFloat:
		args := make([]string, 0, len(values))
		for _, v := range values {
			if n, ok := numericFloat(v); ok {
				args = append(args, strconv.FormatFloat(n, 'f', -1, 64))
			}
		}
		if len(args) == 0 {
			return nil
		}
		return []ir.ValidatorIR{{Type: "float64validator.NoneOf", Args: args}}
	default:
		return nil
	}
}

// numericInt coerces JSON/YAML numeric values (float64) and literal ints into an
// int64 suitable for framework int64 validators. Out-of-range float64 values
// (|n| > 2^63) report ok=false rather than saturating to MinInt64/MaxInt64, which
// would emit a silently wrong validator (M-45).
func numericInt(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		// The round-trip check rejects out-of-range values: int64(n) saturates
		// (implementation-defined) for |n| > 2^63, and float64(int64(n)) != n,
		// so ok stays false instead of returning a saturated, wrong value.
		i := int64(n) // bounded by the round-trip check below
		if n == float64(i) {
			return i, true
		}
		return 0, false
	case string:
		if parsed, err := strconv.ParseInt(n, 10, 64); err == nil {
			return parsed, true
		}
		return 0, false
	default:
		return 0, false
	}
}

// safeCeilInt64 returns ceil(f) as int64, reporting ok=false when f is outside
// the representable int64 range. Out-of-range spec-supplied bounds are skipped
// (with a diagnostic at the call site) rather than saturated to MinInt64/
// MaxInt64, which would emit a silently wrong validator (M-45).
func safeCeilInt64(f float64) (int64, bool) { return floatToInt64(f, math.Ceil) }

// safeFloorInt64 returns floor(f) as int64 with the same range guard as
// safeCeilInt64 (M-45).
func safeFloorInt64(f float64) (int64, bool) { return floatToInt64(f, math.Floor) }

// floatToInt64 rounds f with round and returns the int64 result only when the
// rounded value round-trips through int64, which guarantees it is within the
// representable int64 range (M-45).
func floatToInt64(f float64, round func(float64) float64) (int64, bool) {
	r := round(f)
	i := int64(r) // bounded by the round-trip check below
	if float64(i) == r {
		return i, true
	}
	return 0, false
}

// numericFloat coerces JSON/YAML numeric values into a float64 suitable for
// framework float64 validators.
func numericFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	case string:
		if parsed, err := strconv.ParseFloat(n, 64); err == nil {
			return parsed, true
		}
		return 0, false
	default:
		return 0, false
	}
}
