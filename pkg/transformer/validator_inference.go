package transformer

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// InferValidators derives Terraform Plugin Framework validator IR entries from
// the OpenAPI/JSON Schema constraints present on schema. It covers the
// validators listed in PROJECT_DESIGN.md Section 8.3 for the task scope:
// enum, minLength/maxLength, minimum/maximum, pattern, format, not, const,
// dependentRequired, patternProperties, and propertyNames.
func InferValidators(schema *ir.SchemaIR) []ir.ValidatorIR {
	if schema == nil {
		return nil
	}

	var validators []ir.ValidatorIR

	// Format validators are only meaningful for string-typed schemas.
	if schema.Type == ir.TypeString {
		validators = append(validators, inferFormatValidators(schema.Format)...)
	}

	// Enum / const constraints.
	if len(schema.EnumValues) > 0 {
		validators = append(validators, inferEnumValidators(schema)...)
	}
	if schema.Const != nil {
		if v := inferConstValidator(schema); v.Type != "" {
			validators = append(validators, v)
		}
	}

	// String length constraints. minLength/maxLength are string constraints;
	// emitting stringvalidator.LengthBetween on an Int64/Bool attribute would
	// produce a non-compiling provider, so gate on the string type (M-48).
	if schema.Type == ir.TypeString && (schema.MinLength != nil || schema.MaxLength != nil) {
		validators = append(validators, inferLengthValidators(schema)...)
	}

	// Numeric range constraints.
	if schema.Minimum != nil || schema.Maximum != nil {
		validators = append(validators, inferRangeValidators(schema)...)
	}

	// Regular expression pattern. pattern is a string constraint; emitting
	// stringvalidator.RegexMatches on a non-string attribute would not compile,
	// so gate on the string type (M-48).
	if schema.Type == ir.TypeString && schema.Pattern != "" {
		validators = append(validators, inferPatternValidator(schema))
	}

	// JSON Schema negation.
	if schema.Not != nil {
		validators = append(validators, inferNotValidators(schema.Not)...)
	}

	// Map-specific validators (propertyNames, patternProperties).
	if schema.Collection != nil && schema.Collection.Kind == ir.Map {
		validators = append(validators, inferMapValidators(schema)...)
	}

	return validators
}

// ApplyValidators populates schema.Validators with the entries returned by
// InferValidators. Existing validators are preserved.
func ApplyValidators(schema *ir.SchemaIR) {
	if schema == nil {
		return
	}
	schema.Validators = append(schema.Validators, InferValidators(schema)...)
}

// InferValidatorsForAttribute derives validators for an attribute inside a
// parent object schema. In addition to the schema-level constraints handled by
// InferValidators, it emits AlsoRequires validators when the attribute is the
// trigger key for a dependentRequired constraint on the parent object.
func InferValidatorsForAttribute(attr *ir.AttributeIR, parent *ir.ObjectSchemaIR) []ir.ValidatorIR {
	if attr == nil {
		return nil
	}

	validators := InferValidators(&attr.Schema)

	if parent != nil && attr.Name != "" {
		for trigger, required := range parent.DependentRequired {
			if trigger != attr.Name {
				continue
			}
			// L-110: choose the AlsoRequires constructor matching the trigger
			// attribute's type rather than always emitting stringvalidator.
			// AlsoRequires is generic over path expressions but lives in each
			// typed validator package; using the wrong one produces a non-compiling
			// provider.
			validatorType := alsoRequiresValidatorForAttr(attr)
			if validatorType == "" {
				continue
			}
			for _, other := range required {
				validators = append(validators, ir.ValidatorIR{
					Type: validatorType,
					Args: []string{fmt.Sprintf("path.MatchRoot(%q)", other)},
				})
			}
		}
	}

	return validators
}

// alsoRequiresValidatorForAttr returns the typed AlsoRequires validator
// constructor name for the attribute's schema type, or "" if no typed
// AlsoRequires constructor applies (L-110).
func alsoRequiresValidatorForAttr(attr *ir.AttributeIR) string {
	if attr == nil {
		return ""
	}
	// Collections carry a typed AlsoRequires constructor keyed by the collection
	// kind; check Collection before Type since a collection attribute's Type is
	// empty and the kind alone determines the validator package.
	if attr.Schema.Collection != nil {
		switch attr.Schema.Collection.Kind {
		case ir.List:
			return "listvalidator.AlsoRequires"
		case ir.Set:
			return "setvalidator.AlsoRequires"
		case ir.Map:
			return "mapvalidator.AlsoRequires"
		}
	}
	switch attr.Schema.Type {
	case ir.TypeString, ir.TypeDynamic:
		return "stringvalidator.AlsoRequires"
	case ir.TypeInt:
		return "int64validator.AlsoRequires"
	case ir.TypeFloat:
		return "float64validator.AlsoRequires"
	case ir.TypeBool:
		return "boolvalidator.AlsoRequires"
	default:
		return ""
	}
}

// inferFormatValidators maps known OpenAPI string formats to Terraform validator
// IR. The framework-validators stringvalidator package does not provide
// IsEmailAddress/IsUUID/IsURLWithScheme, and the custom validators.IsRFC3339 /
// validators.IsDate constructors are never generated, so each format is mapped to
// the real stringvalidator.RegexMatches constructor with an appropriate pattern
// (M-49). The args mirror inferPatternValidator so a renderer emits
// `stringvalidator.RegexMatches(regexp.MustCompile(<regex>), <description>)`.
func inferFormatValidators(format string) []ir.ValidatorIR {
	format = strings.ToLower(strings.TrimSpace(format))
	var pattern, desc string
	switch format {
	case "date-time":
		pattern = `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$`
		desc = "must be an RFC 3339 date-time"
	case "date":
		pattern = `^\d{4}-\d{2}-\d{2}$`
		desc = "must be an ISO 8601 date"
	case "email":
		pattern = `^[^@\s]+@[^@\s]+\.[^@\s]+$`
		desc = "must be a valid email address"
	case "uuid":
		pattern = `^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`
		desc = "must be a valid UUID"
	case "uri":
		pattern = `^[a-zA-Z][a-zA-Z0-9+.-]*://[^\s]+$`
		desc = "must be a valid URI"
	default:
		return nil
	}
	return []ir.ValidatorIR{{
		Type: "stringvalidator.RegexMatches",
		Args: []string{
			fmt.Sprintf("regexp.MustCompile(%q)", pattern),
			desc,
		},
	}}
}

// inferEnumValidators converts an enum list into a type-appropriate OneOf
// validator. Non-string enums are rendered with their default formatting. For
// primitive types with no typed OneOf (e.g. dynamic), no validator is emitted
// rather than referencing the never-generated validators.OneOf constructor
// (M-49).
func inferEnumValidators(schema *ir.SchemaIR) []ir.ValidatorIR {
	switch schema.Type {
	case ir.TypeString:
		args := make([]string, 0, len(schema.EnumValues))
		for _, v := range schema.EnumValues {
			if s, ok := v.(string); ok {
				args = append(args, strconv.Quote(s))
			} else {
				args = append(args, fmt.Sprintf("%v", v))
			}
		}
		return []ir.ValidatorIR{{Type: "stringvalidator.OneOf", Args: args}}
	case ir.TypeInt:
		args := make([]string, 0, len(schema.EnumValues))
		for _, v := range schema.EnumValues {
			// L-111: render int enum values as precise int64 literals. JSON
			// numbers unmarshal to float64, so %v would emit "2.5" (non-compiling
			// for int64validator.OneOf) and lose precision for large int64s.
			rendered, ok := renderIntValue(v)
			if !ok {
				continue
			}
			args = append(args, rendered)
		}
		if len(args) == 0 {
			return nil
		}
		return []ir.ValidatorIR{{Type: "int64validator.OneOf", Args: args}}
	case ir.TypeFloat:
		args := make([]string, 0, len(schema.EnumValues))
		for _, v := range schema.EnumValues {
			args = append(args, renderFloatValue(v))
		}
		return []ir.ValidatorIR{{Type: "float64validator.OneOf", Args: args}}
	case ir.TypeBool:
		args := make([]string, 0, len(schema.EnumValues))
		for _, v := range schema.EnumValues {
			args = append(args, fmt.Sprintf("%t", v == true))
		}
		return []ir.ValidatorIR{{Type: "boolvalidator.OneOf", Args: args}}
	}
	return nil
}

// inferConstValidator converts a JSON Schema const into a OneOf validator for
// the matching primitive type. For primitive types with no typed OneOf, an
// empty ValidatorIR (empty Type) is returned so the caller can skip it rather
// than reference the never-generated validators.ConstValidator (M-49).
func inferConstValidator(schema *ir.SchemaIR) ir.ValidatorIR {
	value := *schema.Const
	switch schema.Type {
	case ir.TypeString:
		if s, ok := value.(string); ok {
			return ir.ValidatorIR{Type: "stringvalidator.OneOf", Args: []string{strconv.Quote(s)}}
		}
		return ir.ValidatorIR{Type: "stringvalidator.OneOf", Args: []string{strconv.Quote(fmt.Sprintf("%v", value))}}
	case ir.TypeInt:
		// L-111: render int const as a precise int64 literal; skip if the value
		// is non-integral (cannot be an int64 const).
		rendered, ok := renderIntValue(value)
		if !ok {
			return ir.ValidatorIR{}
		}
		return ir.ValidatorIR{Type: "int64validator.OneOf", Args: []string{rendered}}
	case ir.TypeFloat:
		return ir.ValidatorIR{Type: "float64validator.OneOf", Args: []string{renderFloatValue(value)}}
	case ir.TypeBool:
		return ir.ValidatorIR{Type: "boolvalidator.OneOf", Args: []string{fmt.Sprintf("%t", value == true)}}
	}
	return ir.ValidatorIR{}
}

// inferLengthValidators converts minLength/maxLength into LengthBetween or
// one-sided length validators.
func inferLengthValidators(schema *ir.SchemaIR) []ir.ValidatorIR {
	if schema.MinLength != nil && schema.MaxLength != nil {
		return []ir.ValidatorIR{{
			Type: "stringvalidator.LengthBetween",
			Args: []string{fmt.Sprintf("%d", *schema.MinLength), fmt.Sprintf("%d", *schema.MaxLength)},
		}}
	}
	if schema.MinLength != nil {
		return []ir.ValidatorIR{{
			Type: "stringvalidator.LengthAtLeast",
			Args: []string{fmt.Sprintf("%d", *schema.MinLength)},
		}}
	}
	if schema.MaxLength != nil {
		return []ir.ValidatorIR{{
			Type: "stringvalidator.LengthAtMost",
			Args: []string{fmt.Sprintf("%d", *schema.MaxLength)},
		}}
	}
	return nil
}

// inferRangeValidators converts minimum/maximum into Between or one-sided
// range validators for integer and number types.
func inferRangeValidators(schema *ir.SchemaIR) []ir.ValidatorIR {
	switch schema.Type {
	case ir.TypeInt:
		if schema.Minimum != nil && schema.Maximum != nil {
			return []ir.ValidatorIR{{
				Type: "int64validator.Between",
				Args: []string{fmt.Sprintf("%d", int64(*schema.Minimum)), fmt.Sprintf("%d", int64(*schema.Maximum))},
			}}
		}
		if schema.Minimum != nil {
			return []ir.ValidatorIR{{
				Type: "int64validator.AtLeast",
				Args: []string{fmt.Sprintf("%d", int64(*schema.Minimum))},
			}}
		}
		if schema.Maximum != nil {
			return []ir.ValidatorIR{{
				Type: "int64validator.AtMost",
				Args: []string{fmt.Sprintf("%d", int64(*schema.Maximum))},
			}}
		}
	case ir.TypeFloat:
		if schema.Minimum != nil && schema.Maximum != nil {
			return []ir.ValidatorIR{{
				Type: "float64validator.Between",
				Args: []string{fmt.Sprintf("%g", *schema.Minimum), fmt.Sprintf("%g", *schema.Maximum)},
			}}
		}
		if schema.Minimum != nil {
			return []ir.ValidatorIR{{
				Type: "float64validator.AtLeast",
				Args: []string{fmt.Sprintf("%g", *schema.Minimum)},
			}}
		}
		if schema.Maximum != nil {
			return []ir.ValidatorIR{{
				Type: "float64validator.AtMost",
				Args: []string{fmt.Sprintf("%g", *schema.Maximum)},
			}}
		}
	}
	return nil
}

// inferPatternValidator converts a JSON Schema pattern into a RegexMatches
// validator.
func inferPatternValidator(schema *ir.SchemaIR) ir.ValidatorIR {
	return ir.ValidatorIR{
		Type: "stringvalidator.RegexMatches",
		Args: []string{
			fmt.Sprintf("regexp.MustCompile(%q)", schema.Pattern),
			"must match pattern",
		},
	}
}

// inferNotValidators converts a JSON Schema not constraint into a NoneOf
// validator when the negated schema is an enum. For non-enum or unsupported
// types, no validator is emitted rather than referencing the never-generated
// validators.NotValidator constructor (M-49).
func inferNotValidators(not *ir.SchemaIR) []ir.ValidatorIR {
	if not == nil {
		return nil
	}

	if len(not.EnumValues) > 0 {
		switch not.Type {
		case ir.TypeString:
			args := make([]string, 0, len(not.EnumValues))
			for _, v := range not.EnumValues {
				if s, ok := v.(string); ok {
					args = append(args, strconv.Quote(s))
				} else {
					args = append(args, fmt.Sprintf("%v", v))
				}
			}
			return []ir.ValidatorIR{{Type: "stringvalidator.NoneOf", Args: args}}
		case ir.TypeInt:
			args := make([]string, 0, len(not.EnumValues))
			for _, v := range not.EnumValues {
				// L-111: render int not-enum values as precise int64 literals.
				rendered, ok := renderIntValue(v)
				if !ok {
					continue
				}
				args = append(args, rendered)
			}
			if len(args) == 0 {
				return nil
			}
			return []ir.ValidatorIR{{Type: "int64validator.NoneOf", Args: args}}
		case ir.TypeFloat:
			args := make([]string, 0, len(not.EnumValues))
			for _, v := range not.EnumValues {
				args = append(args, renderFloatValue(v))
			}
			return []ir.ValidatorIR{{Type: "float64validator.NoneOf", Args: args}}
		case ir.TypeBool:
			args := make([]string, 0, len(not.EnumValues))
			for _, v := range not.EnumValues {
				args = append(args, fmt.Sprintf("%t", v == true))
			}
			return []ir.ValidatorIR{{Type: "boolvalidator.NoneOf", Args: args}}
		}
	}

	return nil
}

// inferMapValidators derives validators specific to map-typed schemas:
// propertyNames (key pattern) and patternProperties.
func inferMapValidators(schema *ir.SchemaIR) []ir.ValidatorIR {
	var validators []ir.ValidatorIR

	if schema.PropertyNames != nil && schema.PropertyNames.Pattern != "" {
		validators = append(validators, ir.ValidatorIR{
			Type: "mapvalidator.KeysAre",
			Args: []string{
				fmt.Sprintf("stringvalidator.RegexMatches(regexp.MustCompile(%q), \"key must match pattern\")", schema.PropertyNames.Pattern),
			},
		})
	}

	if len(schema.PatternProperties) > 0 {
		// L-109: sort patterns so the validator args are deterministic regardless
		// of map iteration order, consistent with the rest of the package.
		patterns := make([]string, 0, len(schema.PatternProperties))
		for pattern := range schema.PatternProperties {
			patterns = append(patterns, pattern)
		}
		sort.Strings(patterns)
		validators = append(validators, ir.ValidatorIR{
			Type: "validators.PatternPropertiesValidator",
			Args: patterns,
		})
	}

	return validators
}

// renderIntValue renders an int-typed enum/const/not value as a precise int64
// literal suitable for int64validator.OneOf/NoneOf. JSON numbers unmarshal to
// float64, so a naive %v would emit "2.5" (non-compiling) and lose precision for
// large int64s. Non-integral float values return ok=false so the caller can skip
// them (L-111).
func renderIntValue(v any) (string, bool) {
	switch x := v.(type) {
	case float64:
		if x != math.Trunc(x) {
			return "", false
		}
		return strconv.FormatInt(int64(x), 10), true
	case float32:
		if float64(x) != math.Trunc(float64(x)) {
			return "", false
		}
		return strconv.FormatInt(int64(x), 10), true
	case int:
		return strconv.FormatInt(int64(x), 10), true
	case int64:
		return strconv.FormatInt(x, 10), true
	case int32:
		return strconv.FormatInt(int64(x), 10), true
	default:
		return "", false
	}
}

// renderFloatValue renders a float-typed enum/const/not value as a precise
// float64 literal using strconv.FormatFloat, avoiding %v's compact form which
// can mangle very large or very small magnitudes (L-111).
func renderFloatValue(v any) string {
	switch x := v.(type) {
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 64)
	case int:
		return strconv.FormatFloat(float64(x), 'f', -1, 64)
	case int64:
		return strconv.FormatFloat(float64(x), 'f', -1, 64)
	case int32:
		return strconv.FormatFloat(float64(x), 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", v)
	}
}
