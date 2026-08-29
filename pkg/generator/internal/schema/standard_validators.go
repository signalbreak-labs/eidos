package schema

import (
	"fmt"
	"go/ast"
	"go/token"
	"regexp"
	"strconv"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// This file emits the standard terraform-plugin-framework-validators library
// calls (stringvalidator.OneOf, int64validator.Between, …) derived from the
// JSON Schema constraints an OpenAPI spec declares on an attribute: enum,
// const, minLength/maxLength, pattern, minimum/maximum, and collection size
// bounds. Before this existed, only the custom validators in validators.go
// (exclusive bounds, multipleOf, discriminator, patternProperties) were
// emitted, so every enum and inclusive bound declared in a spec was silently
// dropped from the generated provider (G39).
//
// The custom-validator machinery stays as-is; the standard exprs are appended
// to the same Validators field, sharing the []validator.<Kind> slice the
// custom validators already emit.

// standardValidatorExprs returns the framework-validators library calls that
// apply to one attribute, derived from its schema constraints. It returns nil
// when the attribute is not user-settable (validators only run against
// practitioner configuration, so a Computed-only attribute — e.g. a data
// source result field — would carry dead weight) or carries no supported
// constraint. kind is the validator interface name the renderer chose for the
// attribute (String, Int64, Float64, Bool, List, Set, Map); element-level
// constraints are read off the collection element.
func standardValidatorExprs(attr ir.AttributeIR, kind string) []ast.Expr {
	if !attr.Required && !attr.Optional {
		return nil
	}
	s := attr.Schema
	switch kind {
	case "String":
		return stringStandardValidatorExprs(s)
	case "Int64":
		return intStandardValidatorExprs(s)
	case "Float64":
		return floatStandardValidatorExprs(s)
	case "Bool":
		return boolStandardValidatorExprs(s)
	case "List", "Set", "Map":
		return collectionStandardValidatorExprs(s, kind)
	}
	return nil
}

// HasStandardValidators reports whether standardValidatorExprs emits at least
// one validator for the attribute under the renderer's kind. It drives the
// schema/validator import gating in the emitters: standard exprs occupy the
// same Validators field the custom validators populate, so a file with only
// standard exprs still needs the schema/validator import.
func HasStandardValidators(attr ir.AttributeIR, kind string) bool {
	return len(standardValidatorExprs(attr, kind)) > 0
}

// StandardValidatorPackages returns the framework-validators subpackage
// aliases ("stringvalidator", "listvalidator", …) plus "regexp" that
// standardValidatorExprs will reference for the given attribute. It mirrors
// the expr selection exactly so emitters can gate imports on the same
// decision — an unconditionally-registered alias would leave the generated
// provider with an unused import and fail compilation.
func StandardValidatorPackages(attr ir.AttributeIR, kind string) []string {
	exprs := standardValidatorExprs(attr, kind)
	if len(exprs) == 0 {
		return nil
	}
	var pkgs []string
	add := func(alias string) {
		for _, p := range pkgs {
			if p == alias {
				return
			}
		}
		pkgs = append(pkgs, alias)
	}
	switch kind {
	case "String":
		add("stringvalidator")
		if s := attr.Schema; s.Pattern != "" {
			add("regexp")
		}
	case "Int64":
		add("int64validator")
	case "Float64":
		add("float64validator")
	case "Bool":
		add("boolvalidator")
	case "List":
		add("listvalidator")
	case "Set":
		add("setvalidator")
	case "Map":
		add("mapvalidator")
	}
	// Collection element enums wrap a stringvalidator.OneOf inside the
	// collection's ValueStringsAre, so those kinds need both packages.
	if (kind == "List" || kind == "Set" || kind == "Map") && len(attr.Schema.EnumValues) == 0 {
		elem := collectionElementSchema(attr.Schema)
		if elem != nil && len(elem.EnumValues) > 0 {
			add("stringvalidator")
		}
	}
	return pkgs
}

// stringStandardValidatorExprs emits stringvalidator.OneOf (enum/const),
// LengthBetween/LengthAtLeast/LengthAtMost (minLength/maxLength), and
// RegexMatches (pattern). The pattern is compiled at generation time so an
// invalid regular expression is surfaced as an invalid-constraint failure
// instead of panicking inside the generated provider at every plan.
func stringStandardValidatorExprs(s ir.SchemaIR) []ast.Expr {
	var exprs []ast.Expr
	if args := enumOneOfArgs(s, func(v any) (ast.Expr, bool) {
		str, ok := v.(string)
		if !ok {
			return nil, false
		}
		return astgen.Lit(str), true
	}); len(args) > 0 {
		exprs = append(exprs, astgen.Call(astgen.QualExpr("stringvalidator", "OneOf"), args...))
	}
	if s.Const != nil {
		if str, ok := (*s.Const).(string); ok {
			exprs = append(exprs, astgen.Call(astgen.QualExpr("stringvalidator", "OneOf"), astgen.Lit(str)))
		}
	}
	switch {
	case s.MinLength != nil && s.MaxLength != nil:
		exprs = append(exprs, astgen.Call(astgen.QualExpr("stringvalidator", "LengthBetween"),
			astgen.IntLit(*s.MinLength), astgen.IntLit(*s.MaxLength)))
	case s.MinLength != nil:
		exprs = append(exprs, astgen.Call(astgen.QualExpr("stringvalidator", "LengthAtLeast"), astgen.IntLit(*s.MinLength)))
	case s.MaxLength != nil:
		exprs = append(exprs, astgen.Call(astgen.QualExpr("stringvalidator", "LengthAtMost"), astgen.IntLit(*s.MaxLength)))
	}
	if s.Pattern != "" {
		if _, err := regexp.Compile(s.Pattern); err != nil {
			panic(fmt.Errorf("%w: invalid pattern %q: %w", ErrInvalidValidatorConstraint, s.Pattern, err))
		}
		exprs = append(exprs, astgen.Call(
			astgen.QualExpr("stringvalidator", "RegexMatches"),
			astgen.Call(astgen.QualExpr("regexp", "MustCompile"), astgen.Lit(s.Pattern)),
			astgen.Lit(fmt.Sprintf("value must match pattern %q", s.Pattern)),
		))
	}
	return exprs
}

// intStandardValidatorExprs emits int64validator.OneOf (enum) and Between
// (inclusive minimum/maximum). Non-integral enum members are skipped: a
// JSON number like 2.5 cannot be represented by an int64 schema, and
// emitting it would not compile.
func intStandardValidatorExprs(s ir.SchemaIR) []ast.Expr {
	var exprs []ast.Expr
	if args := enumOneOfArgs(s, func(v any) (ast.Expr, bool) {
		f, ok := toFloat64(v)
		if !ok || f != float64(int64(f)) {
			return nil, false
		}
		return astgen.BasicLit(token.INT, strconv.FormatInt(int64(f), 10)), true
	}); len(args) > 0 {
		exprs = append(exprs, astgen.Call(astgen.QualExpr("int64validator", "OneOf"), args...))
	}
	if s.Minimum != nil || s.Maximum != nil {
		minExpr, maxExpr := boundArgs(s.Minimum, s.Maximum)
		switch {
		case minExpr != nil && maxExpr != nil:
			exprs = append(exprs, astgen.Call(astgen.QualExpr("int64validator", "Between"), minExpr, maxExpr))
		case minExpr != nil:
			exprs = append(exprs, astgen.Call(astgen.QualExpr("int64validator", "AtLeast"), minExpr))
		default:
			exprs = append(exprs, astgen.Call(astgen.QualExpr("int64validator", "AtMost"), maxExpr))
		}
	}
	return exprs
}

// floatStandardValidatorExprs emits float64validator.OneOf (enum) and Between
// (inclusive minimum/maximum).
func floatStandardValidatorExprs(s ir.SchemaIR) []ast.Expr {
	var exprs []ast.Expr
	if args := enumOneOfArgs(s, func(v any) (ast.Expr, bool) {
		f, ok := toFloat64(v)
		if !ok {
			return nil, false
		}
		return astgen.FloatLit(f), true
	}); len(args) > 0 {
		exprs = append(exprs, astgen.Call(astgen.QualExpr("float64validator", "OneOf"), args...))
	}
	if s.Minimum != nil || s.Maximum != nil {
		minExpr, maxExpr := boundArgs(s.Minimum, s.Maximum)
		switch {
		case minExpr != nil && maxExpr != nil:
			exprs = append(exprs, astgen.Call(astgen.QualExpr("float64validator", "Between"), minExpr, maxExpr))
		case minExpr != nil:
			exprs = append(exprs, astgen.Call(astgen.QualExpr("float64validator", "AtLeast"), minExpr))
		default:
			exprs = append(exprs, astgen.Call(astgen.QualExpr("float64validator", "AtMost"), maxExpr))
		}
	}
	return exprs
}

// boolStandardValidatorExprs emits boolvalidator.Equals when an enum pins a
// boolean attribute to one value. boolvalidator has no OneOf, and an enum
// listing both true and false is degenerate (it accepts every boolean), so
// multi-member bool enums produce no validator.
func boolStandardValidatorExprs(s ir.SchemaIR) []ast.Expr {
	if len(s.EnumValues) != 1 {
		return nil
	}
	b, ok := s.EnumValues[0].(bool)
	if !ok {
		return nil
	}
	return []ast.Expr{astgen.Call(astgen.QualExpr("boolvalidator", "Equals"), astgen.BoolLit(b))}
}

// collectionStandardValidatorExprs emits collection size validators
// (listvalidator/setvalidator/mapvalidator SizeBetween/SizeAtLeast/SizeAtMost
// from minItems/maxItems) and, when the collection's elements are
// enum-constrained strings, ValueStringsAre(stringvalidator.OneOf(…)) so each
// element is validated. kind selects the validator package: List, Set, Map.
func collectionStandardValidatorExprs(s ir.SchemaIR, kind string) []ast.Expr {
	var exprs []ast.Expr
	pkg := map[string]string{"List": "listvalidator", "Set": "setvalidator", "Map": "mapvalidator"}[kind]
	if s.MinItems != nil || s.MaxItems != nil {
		minExpr, maxExpr := boundArgs(intPtrToFloat(s.MinItems), intPtrToFloat(s.MaxItems))
		switch {
		case minExpr != nil && maxExpr != nil:
			exprs = append(exprs, astgen.Call(astgen.QualExpr(pkg, "SizeBetween"), minExpr, maxExpr))
		case minExpr != nil:
			exprs = append(exprs, astgen.Call(astgen.QualExpr(pkg, "SizeAtLeast"), minExpr))
		default:
			exprs = append(exprs, astgen.Call(astgen.QualExpr(pkg, "SizeAtMost"), maxExpr))
		}
	}
	elem := collectionElementSchema(s)
	if elem != nil && elem.Type == ir.TypeString && len(elem.EnumValues) > 0 {
		args := make([]ast.Expr, 0, len(elem.EnumValues))
		for _, v := range elem.EnumValues {
			if str, ok := v.(string); ok {
				args = append(args, astgen.Lit(str))
			}
		}
		if len(args) > 0 {
			exprs = append(exprs, astgen.Call(
				astgen.QualExpr(pkg, "ValueStringsAre"),
				astgen.Call(astgen.QualExpr("stringvalidator", "OneOf"), args...),
			))
		}
	}
	return exprs
}

// enumOneOfArgs renders the enum values of s as OneOf arguments using the
// per-type renderer, which returns the Go literal expression or ok=false for
// a value that cannot be represented (e.g. a non-string enum member on a
// string schema). Values that cannot be rendered are skipped; an enum with
// no renderable members produces no validator rather than a degenerate
// OneOf().
func enumOneOfArgs(s ir.SchemaIR, render func(any) (ast.Expr, bool)) []ast.Expr {
	if len(s.EnumValues) == 0 {
		return nil
	}
	args := make([]ast.Expr, 0, len(s.EnumValues))
	for _, v := range s.EnumValues {
		if rendered, ok := render(v); ok {
			args = append(args, rendered)
		}
	}
	return args
}

// collectionElementSchema returns the schema of a collection's element, or
// nil when s is not a collection.
func collectionElementSchema(s ir.SchemaIR) *ir.SchemaIR {
	if s.Collection == nil {
		return nil
	}
	elem := s.Collection.ElementType
	return &elem
}

// boundArgs returns the min/max bound arguments for a Between/AtLeast/AtMost
// call, or nil for an absent bound.
func boundArgs(lower, upper *float64) (ast.Expr, ast.Expr) {
	return boundArg(lower), boundArg(upper)
}

// boundArg renders one inclusive bound. Integral bounds are rendered without
// a decimal point: Between/AtLeast/AtMost take untyped constants, so an
// integral float of 5 renders as 5 (valid for both int64 and float64 kinds)
// while 5.0 would fail for int64validator. Large int64s keep full precision
// by formatting the int64 value rather than rounding through int.
func boundArg(v *float64) ast.Expr {
	if v == nil {
		return nil
	}
	f := *v
	if f == float64(int64(f)) {
		return astgen.BasicLit(token.INT, strconv.FormatInt(int64(f), 10))
	}
	return astgen.FloatLit(f)
}

// intPtrToFloat widens an int pointer bound to the float64 the shared
// boundArgs helper works with.
func intPtrToFloat(v *int) *float64 {
	if v == nil {
		return nil
	}
	f := float64(*v)
	return &f
}

// toFloat64 converts a JSON-decoded number (always float64) to float64,
// reporting false for non-numeric values.
func toFloat64(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok
}
