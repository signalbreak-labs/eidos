package generator

import (
	"math"
	"regexp"
	"strconv"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// This file derives placeholder values for generated configuration text (docs
// examples, .tftest.hcl modules, acceptance-test configs) from the same JSON
// Schema constraints the validator emitters read (internal/schema
// standard_validators.go and validators.go), so a generated config can never
// violate a validator generated from the same schema. Before this existed the
// placeholders were type-only ("example", 0, …), so a spec declaring an enum
// produced a provider whose own acceptance test failed at plan time: the
// config's ship_type = "example" was rejected by the generated
// stringvalidator.OneOf(["SHIP_PROBE", …]) — surfaced as a CI failure in the
// generated spacetraders provider. Every value here is derived
// deterministically (const, then first enum member, then the
// constraint-adjusted default), so generation stays byte-identical across runs.

// schemaExampleLiteral returns the HCL literal used as a placeholder for a
// primitive schema in generated configuration text, honoring the constraints
// the validator emitters derive from it: const/enum (OneOf/Equals),
// minimum/maximum and exclusive bounds (AtLeast/AtMost/Between),
// minLength/maxLength (LengthAtLeast/LengthAtMost/LengthBetween), and pattern
// (RegexMatches) where one of the deterministic pattern candidates matches. An
// unconstrained schema gets the same placeholder primitiveExampleValue returns,
// so specs without constraints generate byte-identical output to before. A
// pattern whose candidates all fail to match keeps the default placeholder —
// synthesizing an arbitrary regex match is out of scope (docs register of
// accepted limitations).
func schemaExampleLiteral(s ir.SchemaIR) string {
	switch s.Type {
	case ir.TypeString:
		return strconv.Quote(stringConstraintExample(s, "example"))
	case ir.TypeInt:
		if v, ok := intConstraintExample(s, 0); ok {
			return strconv.FormatInt(v, 10)
		}
	case ir.TypeFloat:
		if v, ok := floatConstraintExample(s, 1.0); ok {
			return formatFloatLiteral(v)
		}
	case ir.TypeBool:
		return strconv.FormatBool(boolConstraintExample(s, true))
	}
	return primitiveExampleValue(s.Type)
}

// stringConstraintExample returns a raw string value satisfying the schema's
// string constraints. Const wins (it is the most specific statement the spec
// can make); otherwise the first string enum member — stringvalidator.OneOf
// lists exactly the string enum members in spec order (enumOneOfArgs), so a
// string member always validates. Otherwise the default placeholder is grown
// or truncated into the length window, and swapped for a pattern candidate when
// the pattern rejects it.
func stringConstraintExample(s ir.SchemaIR, def string) string {
	if s.Const != nil {
		if str, ok := (*s.Const).(string); ok {
			return str
		}
	}
	if members := stringEnumMembers(s); len(members) > 0 {
		return members[0]
	}
	v := adjustStringLength(def, s.MinLength, s.MaxLength)
	if s.Pattern != "" && !patternMatches(s.Pattern, v) {
		if alt := patternExampleCandidate(s, ""); alt != "" {
			return alt
		}
	}
	return v
}

// intConstraintExample returns an int placeholder satisfying the schema's
// numeric constraints. Const and the first integral enum member win
// (int64validator.OneOf lists exactly the integral members); otherwise the
// default is clamped into the resolved [lo, hi] range and snapped up to a
// multiple of the multipleOf divisor. ok=false when no value can be derived
// (a non-integral const on an integer schema); the caller falls back to the
// unconstrained placeholder.
func intConstraintExample(s ir.SchemaIR, def int64) (int64, bool) {
	if s.Const != nil {
		return integralValue(*s.Const)
	}
	for _, v := range s.EnumValues {
		if n, ok := integralValue(v); ok {
			return n, true
		}
	}
	lo, hi, hasLo, hasHi := intBounds(s)
	val := def
	if hasLo && val < lo {
		val = lo
	}
	if hasHi && val > hi {
		val = hi
	}
	if s.MultipleOf != nil {
		if m, ok := integralValue(*s.MultipleOf); ok && m > 0 {
			val = snapUpToMultiple(val, m)
		}
	}
	return val, true
}

// floatConstraintExample returns a float placeholder satisfying the schema's
// numeric constraints: const or the first float enum member
// (float64validator.OneOf), otherwise the default clamped into the bound
// window. A strict (exclusive) bound that the default violates is nudged past
// by one whole unit; a window too tight for the nudge reports ok=false and the
// caller falls back to the unconstrained placeholder rather than emitting a
// value the validators are guaranteed to reject. Float multipleOf produces no
// validator (the transformer only maps whole-number divisors on integer
// schemas, warning otherwise), so it is not handled here.
func floatConstraintExample(s ir.SchemaIR, def float64) (float64, bool) {
	if s.Const != nil {
		if f, ok := jsonNumber(*s.Const); ok {
			return f, true
		}
		return 0, false
	}
	if members := floatEnumMembers(s); len(members) > 0 {
		return members[0], true
	}
	val := def
	if s.Minimum != nil && val < *s.Minimum {
		val = *s.Minimum
	}
	if s.ExclusiveMinimum != nil && val <= *s.ExclusiveMinimum {
		val = *s.ExclusiveMinimum + 1
	}
	if s.Maximum != nil && val > *s.Maximum {
		val = *s.Maximum
	}
	if s.ExclusiveMaximum != nil && val >= *s.ExclusiveMaximum {
		val = *s.ExclusiveMaximum - 1
	}
	if s.Minimum != nil && val < *s.Minimum {
		return 0, false
	}
	if s.Maximum != nil && val > *s.Maximum {
		return 0, false
	}
	return val, true
}

// boolConstraintExample returns a bool placeholder satisfying the schema's
// constraints: const, then the first bool enum member (boolvalidator.Equals
// pins single-member bool enums), otherwise the default.
func boolConstraintExample(s ir.SchemaIR, def bool) bool {
	if s.Const != nil {
		if b, ok := (*s.Const).(bool); ok {
			return b
		}
	}
	if members := boolEnumMembers(s); len(members) > 0 {
		return members[0]
	}
	return def
}

// acceptanceParamPair returns the raw (unquoted) create and updated values for
// the acceptance test's parameterized attribute — the attribute the update step
// varies to exercise a real change. Both values must satisfy the schema's
// validators and differ from each other, so an attribute whose constraints pin
// a single value (const, a one-member enum, a degenerate numeric range) or
// whose pattern admits no second deterministic candidate reports ok=false:
// such an attribute is skipped as a mutation target and the lifecycle test
// keeps its create/import/destroy steps.
func acceptanceParamPair(s ir.SchemaIR) (create, updated string, ok bool) {
	switch s.Type {
	case ir.TypeString:
		return stringParamPair(s)
	case ir.TypeInt:
		return intParamPair(s)
	case ir.TypeFloat:
		return floatParamPair(s)
	case ir.TypeBool:
		return boolParamPair(s)
	}
	return "", "", false
}

// stringParamPair varies a string attribute between two constraint-valid
// values: the first two string enum members when an enum constrains it, or the
// create placeholder plus "updated" (length-adjusted, pattern-checked)
// otherwise. A pattern that admits no distinct second candidate rejects the
// attribute rather than emitting an update step that fails at plan time.
func stringParamPair(s ir.SchemaIR) (string, string, bool) {
	if s.Const != nil {
		return "", "", false
	}
	if members := stringEnumMembers(s); len(members) > 0 {
		if len(members) < 2 {
			return "", "", false
		}
		return members[0], members[1], true
	}
	create := stringConstraintExample(s, "example")
	updated := adjustStringLength("updated", s.MinLength, s.MaxLength)
	if s.Pattern != "" {
		if !patternMatches(s.Pattern, create) {
			alt := patternExampleCandidate(s, "")
			if alt == "" {
				return "", "", false
			}
			create = alt
		}
		if !patternMatches(s.Pattern, updated) || updated == create {
			alt := patternExampleCandidate(s, create)
			if alt == "" {
				return "", "", false
			}
			updated = alt
		}
	}
	if updated == create {
		return "", "", false
	}
	return create, updated, true
}

// intParamPair varies an integer attribute between two in-range values. The
// create value keeps the historical "1" default when the bounds allow it; the
// updated value is the adjacent integer inside the range. Enum-constrained
// attributes use the first two integral members. A multipleOf divisor is not
// snapped for two distinct values — the degenerate-spec risk is not worth it,
// so such an attribute simply stops being the mutation target.
func intParamPair(s ir.SchemaIR) (string, string, bool) {
	if s.Const != nil {
		return "", "", false
	}
	if members := integralEnumMembers(s); len(members) > 0 {
		if len(members) < 2 {
			return "", "", false
		}
		return strconv.FormatInt(members[0], 10), strconv.FormatInt(members[1], 10), true
	}
	if s.MultipleOf != nil {
		return "", "", false
	}
	lo, hi, hasLo, hasHi := intBounds(s)
	create := int64(1)
	if hasLo && create < lo {
		create = lo
	}
	if hasHi && create > hi {
		create = hi
	}
	updated := create + 1
	if hasHi && updated > hi {
		updated = create - 1
	}
	if updated == create || (hasLo && updated < lo) {
		return "", "", false
	}
	return strconv.FormatInt(create, 10), strconv.FormatInt(updated, 10), true
}

// floatParamPair varies a float attribute between "1.0" and "2.0" clamped into
// the bound window. A window that collapses both to the same value (e.g.
// [0, 1]) reports ok=false rather than emitting an update step that does not
// change anything.
func floatParamPair(s ir.SchemaIR) (string, string, bool) {
	if s.Const != nil {
		return "", "", false
	}
	if members := floatEnumMembers(s); len(members) > 0 {
		if len(members) < 2 {
			return "", "", false
		}
		return formatFloatLiteral(members[0]), formatFloatLiteral(members[1]), true
	}
	create, okCreate := floatConstraintExample(s, 1.0)
	updated, okUpdated := floatConstraintExample(s, 2.0)
	if !okCreate || !okUpdated || create == updated {
		return "", "", false
	}
	return formatFloatLiteral(create), formatFloatLiteral(updated), true
}

// boolParamPair varies a bool attribute between true and false. A single-member
// bool enum is pinned by boolvalidator.Equals and cannot vary; a two-member
// enum emits no validator and admits both values.
func boolParamPair(s ir.SchemaIR) (string, string, bool) {
	if s.Const != nil {
		return "", "", false
	}
	if members := boolEnumMembers(s); len(members) > 0 {
		if len(members) < 2 {
			return "", "", false
		}
		return strconv.FormatBool(members[0]), strconv.FormatBool(members[1]), true
	}
	return "true", "false", true
}

// patternCandidates are the deterministic strings tried (in order) when a
// pattern constraint rejects the default placeholder. They cover the common
// shapes seen in specs — lowercase/uppercase/mixed, digit and separator
// suffixes — without attempting general regex synthesis. The first match wins
// so output stays byte-identical across runs.
var patternCandidates = []string{
	"example", "Example", "EXAMPLE", "example1", "Example1", "EXAMPLE1",
	"example-1", "example_1", "abc", "Abc", "ABC", "abc1", "Abc1", "ABC1",
	"abc-1", "ABC-1", "a1", "A1", "0", "1", "123", "1234",
}

// patternExampleCandidate returns the first pattern candidate that matches the
// schema's pattern and length constraints, or "" when none does. exclude (the
// value already used for the create step) is skipped so update steps can find a
// distinct value.
func patternExampleCandidate(s ir.SchemaIR, exclude string) string {
	for _, cand := range patternCandidates {
		if cand == exclude {
			continue
		}
		if !patternMatches(s.Pattern, cand) {
			continue
		}
		if !stringLengthSatisfied(cand, s.MinLength, s.MaxLength) {
			continue
		}
		return cand
	}
	return ""
}

// patternMatches reports whether v satisfies the ECMA regex pattern, treating
// an invalid pattern as a non-match so example selection defers to the other
// constraints (the validator emitter panics loudly on an invalid pattern at
// generation time, so an invalid pattern never reaches a generated provider).
func patternMatches(pattern, v string) bool {
	matched, err := regexp.MatchString(pattern, v)
	return err == nil && matched
}

// adjustStringLength grows or truncates v (rune-safe) into the minLength/
// maxLength window so the LengthAtLeast/LengthAtMost/LengthBetween validators
// accept it. Growth cycles the original value; a window that admits no length
// (minLength > maxLength, an invalid spec) is left to the validators to
// surface.
func adjustStringLength(v string, minLen, maxLen *int) string {
	if maxLen != nil && *maxLen >= 0 {
		if r := []rune(v); len(r) > *maxLen {
			v = string(r[:*maxLen])
		}
	}
	if minLen != nil && *minLen > 0 {
		r := []rune(v)
		if len(r) > 0 && len(r) < *minLen {
			base := r
			for len(r) < *minLen {
				r = append(r, base...)
			}
			v = string(r[:*minLen])
		}
	}
	return v
}

// stringLengthSatisfied reports whether v already fits the length window
// (used to filter pattern candidates, which are not adjusted).
func stringLengthSatisfied(v string, minLen, maxLen *int) bool {
	n := len([]rune(v))
	if minLen != nil && n < *minLen {
		return false
	}
	if maxLen != nil && n > *maxLen {
		return false
	}
	return true
}

// intBounds resolves the schema's numeric constraints into the closed integral
// range [lo, hi] an int64 attribute may take. Inclusive bounds round inward;
// exclusive bounds (JSON Schema 2020-12 strict > / <) exclude the endpoint.
// When both an inclusive and an exclusive bound of the same side are declared
// (a degenerate spec), the inclusive one wins, mirroring the standard
// validator emitter which reads only the inclusive fields.
func intBounds(s ir.SchemaIR) (lo, hi int64, hasLo, hasHi bool) {
	if s.Minimum != nil {
		lo = int64(math.Ceil(*s.Minimum))
		hasLo = true
	} else if s.ExclusiveMinimum != nil {
		lo = int64(math.Floor(*s.ExclusiveMinimum)) + 1
		hasLo = true
	}
	if s.Maximum != nil {
		hi = int64(math.Floor(*s.Maximum))
		hasHi = true
	} else if s.ExclusiveMaximum != nil {
		hi = int64(math.Ceil(*s.ExclusiveMaximum)) - 1
		hasHi = true
	}
	return lo, hi, hasLo, hasHi
}

// snapUpToMultiple returns the smallest multiple of m that is greater than or
// equal to v, for a positive divisor m, matching how an
// Int64MultipleOfValidator rejects any non-multiple regardless of sign. Go's
// integer division truncates toward zero, which already rounds a negative v up
// to the next multiple (−6/5 → −1, −1·5 = −5 ≥ −6).
func snapUpToMultiple(v, m int64) int64 {
	if m <= 0 || v%m == 0 {
		return v
	}
	q := v / m
	if v < 0 {
		return q * m
	}
	return (q + 1) * m
}

// formatFloatLiteral renders a float placeholder the way primitiveExampleValue
// does ("1.0"): a whole value keeps one decimal place, anything else uses the
// shortest round-trippable form. An unconstrained schema therefore keeps its
// existing literal byte-for-byte.
func formatFloatLiteral(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatFloat(v, 'f', 1, 64)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// jsonNumber coerces a constraint value to float64. The parsers decode JSON/
// YAML numbers as float64, but a constraint set programmatically (tests,
// generator.yaml overrides) may carry any Go integer type, so those are
// accepted too; anything else reports false.
func jsonNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// integralValue coerces a constraint value to an int64, reporting false for
// non-numeric values, non-whole numbers, and values outside the int64 range
// (int64validator.OneOf skips those members too, so the placeholder never
// picks a value the validator cannot represent).
func integralValue(v any) (int64, bool) {
	f, ok := jsonNumber(v)
	if !ok || f != math.Trunc(f) || math.Abs(f) >= 9.2e18 {
		return 0, false
	}
	return int64(f), true
}

// stringEnumMembers returns the schema's enum members that are strings, in
// spec order — the exact member set stringvalidator.OneOf validates against.
func stringEnumMembers(s ir.SchemaIR) []string {
	out := make([]string, 0, len(s.EnumValues))
	for _, v := range s.EnumValues {
		if str, ok := v.(string); ok {
			out = append(out, str)
		}
	}
	return out
}

// integralEnumMembers returns the schema's enum members that coerce to whole
// int64 values, in spec order — the member set int64validator.OneOf uses.
func integralEnumMembers(s ir.SchemaIR) []int64 {
	out := make([]int64, 0, len(s.EnumValues))
	for _, v := range s.EnumValues {
		if n, ok := integralValue(v); ok {
			out = append(out, n)
		}
	}
	return out
}

// floatEnumMembers returns the schema's enum members that are numbers, in spec
// order — the member set float64validator.OneOf uses.
func floatEnumMembers(s ir.SchemaIR) []float64 {
	out := make([]float64, 0, len(s.EnumValues))
	for _, v := range s.EnumValues {
		if f, ok := jsonNumber(v); ok {
			out = append(out, f)
		}
	}
	return out
}

// boolEnumMembers returns the schema's enum members that are booleans, in
// spec order.
func boolEnumMembers(s ir.SchemaIR) []bool {
	out := make([]bool, 0, len(s.EnumValues))
	for _, v := range s.EnumValues {
		if b, ok := v.(bool); ok {
			out = append(out, b)
		}
	}
	return out
}
