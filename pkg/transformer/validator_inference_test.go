package transformer

import (
	"reflect"
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

func intPtr(v int) *int           { return &v }
func floatPtr(v float64) *float64 { return &v }
func anyPtr(v any) *any {
	p := v
	return &p
}

func TestInferValidatorsNil(t *testing.T) {
	if got := InferValidators(nil); got != nil {
		t.Errorf("InferValidators(nil) = %v, want nil", got)
	}
}

func TestInferValidatorsEmpty(t *testing.T) {
	schema := &ir.SchemaIR{Type: ir.TypeString}
	if got := InferValidators(schema); len(got) != 0 {
		t.Errorf("InferValidators(empty schema) = %v, want no validators", got)
	}
}

func TestInferValidatorsEnum(t *testing.T) {
	tests := []struct {
		name     string
		schema   *ir.SchemaIR
		expected []ir.ValidatorIR
	}{
		{
			name: "string enum",
			schema: &ir.SchemaIR{
				Type:       ir.TypeString,
				EnumValues: []any{"active", "inactive"},
			},
			expected: []ir.ValidatorIR{{Type: "stringvalidator.OneOf", Args: []string{"\"active\"", "\"inactive\""}}},
		},
		{
			name: "integer enum",
			schema: &ir.SchemaIR{
				Type:       ir.TypeInt,
				EnumValues: []any{1, 2, 3},
			},
			expected: []ir.ValidatorIR{{Type: "int64validator.OneOf", Args: []string{"1", "2", "3"}}},
		},
		{
			name: "number enum",
			schema: &ir.SchemaIR{
				Type:       ir.TypeFloat,
				EnumValues: []any{1.5, 2.5},
			},
			expected: []ir.ValidatorIR{{Type: "float64validator.OneOf", Args: []string{"1.5", "2.5"}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferValidators(tt.schema)
			assertValidatorsEqual(t, got, tt.expected)
		})
	}
}

func TestInferValidatorsLength(t *testing.T) {
	tests := []struct {
		name     string
		schema   *ir.SchemaIR
		expected []ir.ValidatorIR
	}{
		{
			name: "min and max length",
			schema: &ir.SchemaIR{
				Type:      ir.TypeString,
				MinLength: intPtr(2),
				MaxLength: intPtr(10),
			},
			expected: []ir.ValidatorIR{{Type: "stringvalidator.LengthBetween", Args: []string{"2", "10"}}},
		},
		{
			name: "only min length",
			schema: &ir.SchemaIR{
				Type:      ir.TypeString,
				MinLength: intPtr(5),
			},
			expected: []ir.ValidatorIR{{Type: "stringvalidator.LengthAtLeast", Args: []string{"5"}}},
		},
		{
			name: "only max length",
			schema: &ir.SchemaIR{
				Type:      ir.TypeString,
				MaxLength: intPtr(8),
			},
			expected: []ir.ValidatorIR{{Type: "stringvalidator.LengthAtMost", Args: []string{"8"}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferValidators(tt.schema)
			assertValidatorsEqual(t, got, tt.expected)
		})
	}
}

func TestInferValidatorsRange(t *testing.T) {
	tests := []struct {
		name     string
		schema   *ir.SchemaIR
		expected []ir.ValidatorIR
	}{
		{
			name: "integer between",
			schema: &ir.SchemaIR{
				Type:    ir.TypeInt,
				Minimum: floatPtr(1),
				Maximum: floatPtr(100),
			},
			expected: []ir.ValidatorIR{{Type: "int64validator.Between", Args: []string{"1", "100"}}},
		},
		{
			name: "integer only minimum",
			schema: &ir.SchemaIR{
				Type:    ir.TypeInt,
				Minimum: floatPtr(0),
			},
			expected: []ir.ValidatorIR{{Type: "int64validator.AtLeast", Args: []string{"0"}}},
		},
		{
			name: "integer only maximum",
			schema: &ir.SchemaIR{
				Type:    ir.TypeInt,
				Maximum: floatPtr(10),
			},
			expected: []ir.ValidatorIR{{Type: "int64validator.AtMost", Args: []string{"10"}}},
		},
		{
			name: "number between",
			schema: &ir.SchemaIR{
				Type:    ir.TypeFloat,
				Minimum: floatPtr(0.0),
				Maximum: floatPtr(1.0),
			},
			expected: []ir.ValidatorIR{{Type: "float64validator.Between", Args: []string{"0", "1"}}},
		},
		{
			name: "number only minimum",
			schema: &ir.SchemaIR{
				Type:    ir.TypeFloat,
				Minimum: floatPtr(0.5),
			},
			expected: []ir.ValidatorIR{{Type: "float64validator.AtLeast", Args: []string{"0.5"}}},
		},
		{
			name: "number only maximum",
			schema: &ir.SchemaIR{
				Type:    ir.TypeFloat,
				Maximum: floatPtr(99.9),
			},
			expected: []ir.ValidatorIR{{Type: "float64validator.AtMost", Args: []string{"99.9"}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferValidators(tt.schema)
			assertValidatorsEqual(t, got, tt.expected)
		})
	}
}

func TestInferValidatorsPattern(t *testing.T) {
	schema := &ir.SchemaIR{
		Type:    ir.TypeString,
		Pattern: "^[a-z]+$",
	}
	got := InferValidators(schema)
	expected := []ir.ValidatorIR{{
		Type: "stringvalidator.RegexMatches",
		Args: []string{`regexp.MustCompile("^[a-z]+$")`, "must match pattern"},
	}}
	assertValidatorsEqual(t, got, expected)
}

func TestInferValidatorsFormat(t *testing.T) {
	// M-49: format validators emit the real stringvalidator.RegexMatches
	// constructor (the framework-validators package has no IsEmailAddress /
	// IsUUID / IsURLWithScheme and Eidos never generates validators.IsRFC3339 /
	// validators.IsDate). Each validator has two args — a regexp.MustCompile(...)
	// expression and a human-readable description — mirroring inferPatternValidator.
	tests := []struct {
		name     string
		format   string
		wantDesc string
	}{
		{"date-time", "date-time", "must be an RFC 3339 date-time"},
		{"date", "date", "must be an ISO 8601 date"},
		{"email", "email", "must be a valid email address"},
		{"uuid", "uuid", "must be a valid UUID"},
		{"uri", "uri", "must be a valid URI"},
		{"case insensitive", "UUID", "must be a valid UUID"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := &ir.SchemaIR{Type: ir.TypeString, Format: tt.format}
			got := InferValidators(schema)
			if len(got) != 1 {
				t.Fatalf("expected 1 validator, got %d: %v", len(got), got)
			}
			if got[0].Type != "stringvalidator.RegexMatches" {
				t.Errorf("Type = %q, want stringvalidator.RegexMatches", got[0].Type)
			}
			if len(got[0].Args) != 2 {
				t.Fatalf("expected 2 args, got %d: %v", len(got[0].Args), got[0].Args)
			}
			if !strings.HasPrefix(got[0].Args[0], "regexp.MustCompile(") {
				t.Errorf("regex arg = %q, want regexp.MustCompile(...) prefix", got[0].Args[0])
			}
			if got[0].Args[1] != tt.wantDesc {
				t.Errorf("description arg = %q, want %q", got[0].Args[1], tt.wantDesc)
			}
		})
	}

	t.Run("unknown format ignored", func(t *testing.T) {
		schema := &ir.SchemaIR{Type: ir.TypeString, Format: "custom"}
		if got := InferValidators(schema); got != nil {
			t.Errorf("expected no validator for unknown format, got %v", got)
		}
	})
}

func TestInferValidatorsNot(t *testing.T) {
	tests := []struct {
		name     string
		schema   *ir.SchemaIR
		expected []ir.ValidatorIR
	}{
		{
			name: "not string enum",
			schema: &ir.SchemaIR{
				Type: ir.TypeString,
				Not: &ir.SchemaIR{
					Type:       ir.TypeString,
					EnumValues: []any{"forbidden"},
				},
			},
			expected: []ir.ValidatorIR{{Type: "stringvalidator.NoneOf", Args: []string{"\"forbidden\""}}},
		},
		{
			name: "not integer enum",
			schema: &ir.SchemaIR{
				Type: ir.TypeInt,
				Not: &ir.SchemaIR{
					Type:       ir.TypeInt,
					EnumValues: []any{13, 42},
				},
			},
			expected: []ir.ValidatorIR{{Type: "int64validator.NoneOf", Args: []string{"13", "42"}}},
		},
		{
			// M-49: a non-enum `not` schema has no typed NoneOf target, so no
			// validator is emitted rather than referencing the never-generated
			// validators.NotValidator constructor.
			name: "not complex schema",
			schema: &ir.SchemaIR{
				Type: ir.TypeString,
				Not: &ir.SchemaIR{
					Type:      ir.TypeString,
					MinLength: intPtr(100),
				},
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferValidators(tt.schema)
			assertValidatorsEqual(t, got, tt.expected)
		})
	}
}

func TestInferValidatorsConst(t *testing.T) {
	tests := []struct {
		name     string
		schema   *ir.SchemaIR
		expected []ir.ValidatorIR
	}{
		{
			name:     "string const",
			schema:   &ir.SchemaIR{Type: ir.TypeString, Const: anyPtr("fixed")},
			expected: []ir.ValidatorIR{{Type: "stringvalidator.OneOf", Args: []string{"\"fixed\""}}},
		},
		{
			name:     "integer const",
			schema:   &ir.SchemaIR{Type: ir.TypeInt, Const: anyPtr(7)},
			expected: []ir.ValidatorIR{{Type: "int64validator.OneOf", Args: []string{"7"}}},
		},
		{
			name:     "number const",
			schema:   &ir.SchemaIR{Type: ir.TypeFloat, Const: anyPtr(3.14)},
			expected: []ir.ValidatorIR{{Type: "float64validator.OneOf", Args: []string{"3.14"}}},
		},
		{
			// M-49: a boolean const maps to boolvalidator.OneOf rather than the
			// never-generated validators.ConstValidator constructor.
			name:     "boolean const",
			schema:   &ir.SchemaIR{Type: ir.TypeBool, Const: anyPtr(true)},
			expected: []ir.ValidatorIR{{Type: "boolvalidator.OneOf", Args: []string{"true"}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferValidators(tt.schema)
			assertValidatorsEqual(t, got, tt.expected)
		})
	}
}

func TestInferValidatorsForAttributeDependentRequired(t *testing.T) {
	parent := &ir.ObjectSchemaIR{
		DependentRequired: map[string][]string{
			"country": {"state", "zip"},
		},
	}

	attr := &ir.AttributeIR{
		Name:   "country",
		Schema: ir.SchemaIR{Type: ir.TypeString},
	}

	got := InferValidatorsForAttribute(attr, parent)
	expected := []ir.ValidatorIR{
		{Type: "stringvalidator.AlsoRequires", Args: []string{`path.MatchRoot("state")`}},
		{Type: "stringvalidator.AlsoRequires", Args: []string{`path.MatchRoot("zip")`}},
	}
	assertValidatorsEqual(t, got, expected)
}

func TestInferValidatorsForAttributeNoDependentRequiredWhenNotTrigger(t *testing.T) {
	parent := &ir.ObjectSchemaIR{
		DependentRequired: map[string][]string{
			"country": {"state"},
		},
	}

	attr := &ir.AttributeIR{
		Name:   "state",
		Schema: ir.SchemaIR{Type: ir.TypeString},
	}

	got := InferValidatorsForAttribute(attr, parent)
	if len(got) != 0 {
		t.Errorf("expected no validators for non-trigger attribute, got %v", got)
	}
}

func TestInferValidatorsMapPropertyNames(t *testing.T) {
	schema := &ir.SchemaIR{
		Type: ir.TypeString,
		Collection: &ir.CollectionType{
			Kind:        ir.Map,
			ElementType: ir.SchemaIR{Type: ir.TypeString},
		},
		PropertyNames: &ir.SchemaIR{
			Type:    ir.TypeString,
			Pattern: "^[a-z]+$",
		},
	}
	got := InferValidators(schema)
	expected := []ir.ValidatorIR{{
		Type: "mapvalidator.KeysAre",
		Args: []string{`stringvalidator.RegexMatches(regexp.MustCompile("^[a-z]+$")` + ", \"key must match pattern\")"},
	}}
	assertValidatorsEqual(t, got, expected)
}

func TestInferValidatorsMapPatternProperties(t *testing.T) {
	schema := &ir.SchemaIR{
		Type: ir.TypeString,
		Collection: &ir.CollectionType{
			Kind:        ir.Map,
			ElementType: ir.SchemaIR{Type: ir.TypeString},
		},
		PatternProperties: map[string]*ir.SchemaIR{
			"^foo$": {Type: ir.TypeString},
			"^bar$": {Type: ir.TypeString},
		},
	}
	got := InferValidators(schema)
	expected := []ir.ValidatorIR{{
		Type: "validators.PatternPropertiesValidator",
	}}
	if len(got) != 1 || got[0].Type != expected[0].Type {
		t.Fatalf("expected %v, got %v", expected, got)
	}
	if len(got[0].Args) != 2 {
		t.Fatalf("expected 2 pattern args, got %v", got[0].Args)
	}
	// L-109: pattern args must be sorted deterministically (^bar$ before ^foo$).
	wantArgs := []string{"^bar$", "^foo$"}
	if !reflect.DeepEqual(got[0].Args, wantArgs) {
		t.Errorf("pattern args = %v, want %v (sorted)", got[0].Args, wantArgs)
	}
}

// TestInferValidatorsForAttributeDependentRequiredTyped locks in the L-110 fix:
// the AlsoRequires constructor is chosen by the trigger attribute's type, not
// hard-coded to stringvalidator.
func TestInferValidatorsForAttributeDependentRequiredTyped(t *testing.T) {
	tests := []struct {
		name       string
		attrType   ir.PrimitiveType
		collection *ir.CollectionType
		wantType   string
	}{
		{name: "string", attrType: ir.TypeString, wantType: "stringvalidator.AlsoRequires"},
		{name: "integer", attrType: ir.TypeInt, wantType: "int64validator.AlsoRequires"},
		{name: "number", attrType: ir.TypeFloat, wantType: "float64validator.AlsoRequires"},
		{name: "boolean", attrType: ir.TypeBool, wantType: "boolvalidator.AlsoRequires"},
		{name: "list", attrType: ir.PrimitiveType(""), collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Type: ir.TypeString}}, wantType: "listvalidator.AlsoRequires"},
		{name: "set", attrType: ir.PrimitiveType(""), collection: &ir.CollectionType{Kind: ir.Set, ElementType: ir.SchemaIR{Type: ir.TypeString}}, wantType: "setvalidator.AlsoRequires"},
		{name: "map", attrType: ir.PrimitiveType(""), collection: &ir.CollectionType{Kind: ir.Map, ElementType: ir.SchemaIR{Type: ir.TypeString}}, wantType: "mapvalidator.AlsoRequires"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := &ir.ObjectSchemaIR{
				DependentRequired: map[string][]string{"trigger": {"other"}},
			}
			attr := &ir.AttributeIR{
				Name:   "trigger",
				Schema: ir.SchemaIR{Type: tt.attrType, Collection: tt.collection},
			}
			got := InferValidatorsForAttribute(attr, parent)
			if len(got) != 1 {
				t.Fatalf("expected 1 validator, got %v", got)
			}
			if got[0].Type != tt.wantType {
				t.Errorf("AlsoRequires type = %q, want %q", got[0].Type, tt.wantType)
			}
		})
	}
}

// TestInferValidatorsEnumIntFloat64Precision locks in the L-111 fix: int enum
// values that arrive as float64 (the JSON unmarshal default) are rendered as
// precise int64 literals, non-integral float values are skipped, and large
// int64s do not lose precision.
func TestInferValidatorsEnumIntFloat64Precision(t *testing.T) {
	schema := &ir.SchemaIR{
		Type:       ir.TypeInt,
		EnumValues: []any{float64(1), float64(2), float64(2.5), int64(9223372036854775807)},
	}
	got := InferValidators(schema)
	if len(got) != 1 || got[0].Type != "int64validator.OneOf" {
		t.Fatalf("expected one int64validator.OneOf, got %v", got)
	}
	// 2.5 is non-integral and must be skipped; the rest render as int64 literals.
	wantArgs := []string{"1", "2", "9223372036854775807"}
	if !reflect.DeepEqual(got[0].Args, wantArgs) {
		t.Errorf("int enum args = %v, want %v (2.5 skipped, int64 preserved)", got[0].Args, wantArgs)
	}
}

// TestInferValidatorsConstIntNonIntegralSkipped locks in the L-111 fix: a
// non-integral const on an int schema produces no validator rather than a
// non-compiling int64validator.OneOf(2.5).
func TestInferValidatorsConstIntNonIntegralSkipped(t *testing.T) {
	nonIntegral := any(float64(2.5))
	schema := &ir.SchemaIR{
		Type:  ir.TypeInt,
		Const: &nonIntegral,
	}
	got := InferValidators(schema)
	for _, v := range got {
		if v.Type == "int64validator.OneOf" {
			t.Errorf("expected no int64validator.OneOf for non-integral int const, got %v", got)
		}
	}
}

func TestInferValidatorsCombined(t *testing.T) {
	schema := &ir.SchemaIR{
		Type:       ir.TypeString,
		Format:     "email",
		MinLength:  intPtr(5),
		MaxLength:  intPtr(100),
		Pattern:    "^[^@]+@[^@]+$",
		EnumValues: []any{"a@example.com", "b@example.com"},
	}
	got := InferValidators(schema)
	expectedTypes := []string{
		// M-49: email format now emits stringvalidator.RegexMatches instead of
		// the non-existent stringvalidator.IsEmailAddress constructor.
		"stringvalidator.RegexMatches",
		"stringvalidator.OneOf",
		"stringvalidator.LengthBetween",
		"stringvalidator.RegexMatches",
	}
	if len(got) != len(expectedTypes) {
		t.Fatalf("expected %d validators, got %d: %v", len(expectedTypes), len(got), got)
	}
	for i, want := range expectedTypes {
		if got[i].Type != want {
			t.Errorf("validator[%d].Type = %q, want %q", i, got[i].Type, want)
		}
	}
}

func TestApplyValidators(t *testing.T) {
	schema := &ir.SchemaIR{
		Type:       ir.TypeString,
		EnumValues: []any{"one"},
	}
	ApplyValidators(schema)
	if len(schema.Validators) != 1 {
		t.Fatalf("expected 1 validator after ApplyValidators, got %v", schema.Validators)
	}
	if schema.Validators[0].Type != "stringvalidator.OneOf" {
		t.Errorf("unexpected validator type: %v", schema.Validators[0].Type)
	}
}

func assertValidatorsEqual(t *testing.T, got, want []ir.ValidatorIR) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("validators length mismatch: got %d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i := range got {
		if got[i].Type != want[i].Type {
			t.Errorf("validator[%d].Type = %q, want %q", i, got[i].Type, want[i].Type)
		}
		if len(got[i].Args) != len(want[i].Args) {
			t.Errorf("validator[%d].Args length = %d, want %d", i, len(got[i].Args), len(want[i].Args))
			continue
		}
		for j := range got[i].Args {
			if got[i].Args[j] != want[i].Args[j] {
				t.Errorf("validator[%d].Args[%d] = %q, want %q", i, j, got[i].Args[j], want[i].Args[j])
			}
		}
	}
}
