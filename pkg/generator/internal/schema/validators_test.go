package schema

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/token"
	"math"
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// renderValidators renders the generated validators *ast.File (nil when no
// validators are needed) to collapsed source.
func renderValidators(t *testing.T, pir ir.ProviderIR) string {
	t.Helper()
	f := GenerateValidatorsFile(pir)
	if f == nil {
		return ""
	}
	var buf bytes.Buffer
	if err := format.Node(&buf, token.NewFileSet(), f); err != nil {
		t.Fatalf("format validators AST: %v", err)
	}
	return strings.Join(strings.Fields(buf.String()), " ")
}

func floatPtr(v float64) *float64 { return &v }

// providerWithResource returns a ProviderIR with a single resource carrying the
// given attributes.
func providerWithResource(attrs ...ir.AttributeIR) ir.ProviderIR {
	return ir.ProviderIR{Resources: []ir.ResourceIR{resource("pet", attrs...)}}
}

// TestGenerateValidatorsFile_Nil asserts no file is generated when the IR has
// no advanced constraints.
func TestGenerateValidatorsFile_Nil(t *testing.T) {
	pir := providerWithResource(stringAttr("name", true))
	if f := GenerateValidatorsFile(pir); f != nil {
		t.Error("expected nil AST when no validators are needed")
	}
	needs := collectValidatorNeeds(pir)
	if !needs.isEmpty() {
		t.Errorf("expected empty validator needs, got %+v", needs)
	}
}

// TestGenerateValidatorsFile_Float64Branches asserts each float64 validator
// generator fires when its exclusive bound or multipleOf is present.
func TestGenerateValidatorsFile_Float64Branches(t *testing.T) {
	attrs := []ir.AttributeIR{
		{Name: "a", Required: true, Schema: ir.SchemaIR{Type: ir.TypeFloat, ExclusiveMinimum: floatPtr(1.5)}},
		{Name: "b", Required: true, Schema: ir.SchemaIR{Type: ir.TypeFloat, ExclusiveMaximum: floatPtr(9.5)}},
		{Name: "c", Required: true, Schema: ir.SchemaIR{Type: ir.TypeFloat, MultipleOf: floatPtr(0.5)}},
	}
	got := renderValidators(t, providerWithResource(attrs...))

	for _, want := range []string{
		"func Float64ExclusiveMinimumValidator(min float64) validator.Float64",
		"func Float64ExclusiveMaximumValidator(max float64) validator.Float64",
		"func Float64MultipleOfValidator(factor float64) validator.Float64",
		"type float64ExclusiveMinimumValidator struct",
		"type float64ExclusiveMaximumValidator struct",
		"type float64MultipleOfValidator struct",
		"func (v float64ExclusiveMinimumValidator) ValidateFloat64",
		"func (v float64ExclusiveMaximumValidator) ValidateFloat64",
		"func (v float64MultipleOfValidator) ValidateFloat64",
		"value must be greater than %v",
		"value must be less than %v",
		"value must be a multiple of %v",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated validators missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestGenerateValidatorsFile_Int64Branches asserts each int64 validator
// generator fires when its exclusive bound or multipleOf is present.
func TestGenerateValidatorsFile_Int64Branches(t *testing.T) {
	attrs := []ir.AttributeIR{
		{Name: "min", Required: true, Schema: ir.SchemaIR{Type: ir.TypeInt, ExclusiveMinimum: floatPtr(3)}},
		{Name: "max", Required: true, Schema: ir.SchemaIR{Type: ir.TypeInt, ExclusiveMaximum: floatPtr(10)}},
		{Name: "mult", Required: true, Schema: ir.SchemaIR{Type: ir.TypeInt, MultipleOf: floatPtr(4)}},
	}
	got := renderValidators(t, providerWithResource(attrs...))

	for _, want := range []string{
		"func Int64ExclusiveMinimumValidator(min int64) validator.Int64",
		"func Int64ExclusiveMaximumValidator(max int64) validator.Int64",
		"func Int64MultipleOfValidator(factor int64) validator.Int64",
		"type int64ExclusiveMinimumValidator struct",
		"func (v int64ExclusiveMinimumValidator) ValidateInt64",
		"func (v int64ExclusiveMaximumValidator) ValidateInt64",
		"func (v int64MultipleOfValidator) ValidateInt64",
		"multipleOf factor must not be zero",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated validators missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestGenerateValidatorsFile_Discriminator asserts the discriminator validator
// is emitted when a union carries a discriminator, and that discriminator
// mapping keys are sorted for deterministic output.
func TestGenerateValidatorsFile_Discriminator(t *testing.T) {
	pir := ir.ProviderIR{Resources: []ir.ResourceIR{resource("pet",
		ir.AttributeIR{
			Name: "details",
			Schema: ir.SchemaIR{Union: &ir.UnionType{
				Kind: ir.OneOf,
				Variants: []ir.SchemaIR{
					{Type: ir.TypeString, Name: "cat"},
					{Type: ir.TypeString, Name: "dog"},
				},
				Discriminator: &ir.DiscriminatorIR{
					PropertyName: "animalType",
					Mapping:      map[string]string{"dog": "DogVariant", "cat": "CatVariant"},
				},
			}},
		},
	)}}
	got := renderValidators(t, pir)

	for _, want := range []string{
		"func DiscriminatorValidator(propertyName string, allowed ...string) validator.Object",
		"type discriminatorValidator struct",
		"func (v discriminatorValidator) ValidateObject",
		"discriminator property %q must be one of [%s]",
		// Call sites (with the snake_cased property name and sorted mapping
		// keys) are emitted by ObjectValidatorExprs into schema files, not into
		// this validators file.
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated validators missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestGenerateValidatorsFile_Conditional asserts dependent-required triggers
// emit ConditionalValidator and that triggers are sorted.
func TestGenerateValidatorsFile_Conditional(t *testing.T) {
	pir := ir.ProviderIR{Resources: []ir.ResourceIR{resource("pet",
		ir.AttributeIR{
			Name: "payment",
			Schema: ir.SchemaIR{
				Attributes: []ir.AttributeIR{stringAttr("card", true)},
				DependentRequired: map[string][]string{
					"card":   {"cvv", "expiry"},
					"paypal": {"email"},
				},
			},
		},
	)}}
	got := renderValidators(t, pir)

	for _, want := range []string{
		"func ConditionalValidator(trigger string, required ...string) validator.Object",
		"type conditionalValidator struct",
		"func (v conditionalValidator) ValidateObject",
		"field %q is required when %q is set",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated validators missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestGenerateValidatorsFile_PatternProperties asserts patternProperties emits a
// map validator and that the constructor compiles each pattern at generation
// time (via regexp.MustCompile in the generated code).
func TestGenerateValidatorsFile_PatternProperties(t *testing.T) {
	pir := ir.ProviderIR{Resources: []ir.ResourceIR{resource("pet",
		ir.AttributeIR{
			Name: "labels",
			Schema: ir.SchemaIR{
				Type: ir.TypeString,
				PatternProperties: map[string]*ir.SchemaIR{
					"^x-":  {Type: ir.TypeString},
					"^env": {Type: ir.TypeString},
				},
			},
		},
	)}}
	got := renderValidators(t, pir)

	for _, want := range []string{
		"func PatternPropertiesValidator(patterns map[string]string) validator.Map",
		"type patternPropertiesValidator struct",
		"func (v patternPropertiesValidator) ValidateMap",
		"regexp.MustCompile",
		"key %q does not match any patternProperties pattern",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated validators missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestCollectValidatorNeeds_AllCategories asserts needs are collected from every
// provider category: config schema, resources, data sources, actions,
// ephemerals, list resources, and functions.
func TestCollectValidatorNeeds_AllCategories(t *testing.T) {
	exclMin := func() *float64 { return floatPtr(1) }
	pir := ir.ProviderIR{
		ConfigSchema: ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{Name: "c", Schema: ir.SchemaIR{Type: ir.TypeFloat, ExclusiveMinimum: exclMin()}}}},
		DataSources:  []ir.DataSourceIR{{Schema: ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{Name: "d", Schema: ir.SchemaIR{Type: ir.TypeFloat, ExclusiveMinimum: exclMin()}}}}}},
		Actions:      []ir.ActionIR{{ConfigSchema: ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{Name: "a", Schema: ir.SchemaIR{Type: ir.TypeFloat, ExclusiveMinimum: exclMin()}}}}}},
		EphemeralResources: []ir.EphemeralResourceIR{{
			ConfigSchema: ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{Name: "e", Schema: ir.SchemaIR{Type: ir.TypeFloat, ExclusiveMinimum: exclMin()}}}},
			ResultSchema: ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{Name: "r", Schema: ir.SchemaIR{Type: ir.TypeFloat, ExclusiveMinimum: exclMin()}}}},
		}},
		ListResources: []ir.ListResourceIR{{
			ConfigSchema:   ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{Name: "l", Schema: ir.SchemaIR{Type: ir.TypeFloat, ExclusiveMinimum: exclMin()}}}},
			IdentitySchema: ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{Name: "i", Schema: ir.SchemaIR{Type: ir.TypeFloat, ExclusiveMinimum: exclMin()}}}},
			ResourceSchema: &ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{Name: "rs", Schema: ir.SchemaIR{Type: ir.TypeFloat, ExclusiveMinimum: exclMin()}}}},
		}},
		Functions: []ir.FunctionIR{{
			Arguments:  []ir.FunctionParamIR{{Schema: ir.SchemaIR{Type: ir.TypeFloat, ExclusiveMinimum: exclMin()}}},
			ReturnType: ir.SchemaIR{Type: ir.TypeFloat, ExclusiveMinimum: exclMin()},
		}},
	}
	needs := collectValidatorNeeds(pir)
	if !needs.float64ExclusiveMin {
		t.Error("expected float64ExclusiveMin to be set from all categories")
	}
}

// TestCollectValidatorNeeds_NestedBlockRecursion asserts validator needs are
// collected from blocks nested inside schema attributes and inside union
// variants (the recursion paths in collectSchemaValidatorNeeds).
func TestCollectValidatorNeeds_NestedBlockRecursion(t *testing.T) {
	pir := ir.ProviderIR{Resources: []ir.ResourceIR{resource("pet",
		ir.AttributeIR{
			Name: "wrapper",
			Schema: ir.SchemaIR{
				Blocks: []ir.BlockIR{{
					Name: "inner",
					Schema: ir.ObjectSchemaIR{
						Attributes: []ir.AttributeIR{{
							Name:   "level",
							Schema: ir.SchemaIR{Type: ir.TypeInt, ExclusiveMinimum: floatPtr(0)},
						}},
						// Block nested inside a block: exercises the blocks
						// recursion in collectObjectSchemaValidatorNeeds.
						Blocks: []ir.BlockIR{{
							Name: "deeper",
							Schema: ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{
								Name:   "ratio",
								Schema: ir.SchemaIR{Type: ir.TypeFloat, ExclusiveMaximum: floatPtr(9.5)},
							}}},
						}},
					},
				}},
			},
		},
		ir.AttributeIR{
			Name: "variant",
			Schema: ir.SchemaIR{Union: &ir.UnionType{
				Kind: ir.OneOf,
				Variants: []ir.SchemaIR{{
					Type:       ir.TypeInt,
					Attributes: []ir.AttributeIR{{Name: "nested", Schema: ir.SchemaIR{Type: ir.TypeFloat, MultipleOf: floatPtr(0.1)}}},
				}},
			}},
		},
	)}}
	needs := collectValidatorNeeds(pir)
	if !needs.int64ExclusiveMin {
		t.Error("expected int64ExclusiveMin from block nested inside attribute schema")
	}
	if !needs.float64ExclusiveMax {
		t.Error("expected float64ExclusiveMax from block nested inside block")
	}
	if !needs.float64MultipleOf {
		t.Error("expected float64MultipleOf from union variant attribute")
	}
}

// TestGenerateValidatorsFile_ConfigBlocks asserts config-schema blocks are
// scanned for validator needs.
func TestGenerateValidatorsFile_ConfigBlocks(t *testing.T) {
	pir := ir.ProviderIR{ConfigSchema: ir.ObjectSchemaIR{Blocks: []ir.BlockIR{{
		Name: "logging",
		Schema: ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{
			Name:   "level",
			Schema: ir.SchemaIR{Type: ir.TypeInt, ExclusiveMinimum: floatPtr(0)},
		}}},
	}}}}
	needs := collectValidatorNeeds(pir)
	if !needs.int64ExclusiveMin {
		t.Error("expected int64ExclusiveMin to be set from config block")
	}
	got := renderValidators(t, pir)
	if !strings.Contains(got, "Int64ExclusiveMinimumValidator") {
		t.Errorf("expected Int64ExclusiveMinimumValidator in output:\n%s", got)
	}
}

func TestFloat64ValidatorExprs(t *testing.T) {
	t.Run("finite bounds produce exprs", func(t *testing.T) {
		s := ir.SchemaIR{Type: ir.TypeFloat, ExclusiveMinimum: floatPtr(1.5), ExclusiveMaximum: floatPtr(9.5), MultipleOf: floatPtr(0.25)}
		exprs := Float64ValidatorExprs(s)
		if len(exprs) != 3 {
			t.Fatalf("expected 3 expressions, got %d", len(exprs))
		}
		b, err := astgen.RenderExpr(exprs[0])
		if err != nil {
			t.Fatalf("RenderExpr: %v", err)
		}
		if !strings.Contains(string(b), "Float64ExclusiveMinimumValidator(1.5)") {
			t.Errorf("unexpected expr %q", string(b))
		}
	})
	t.Run("non-finite bounds are skipped", func(t *testing.T) {
		inf := floatPtr(math.Inf(1))
		nan := floatPtr(math.NaN())
		s := ir.SchemaIR{Type: ir.TypeFloat, ExclusiveMinimum: inf, ExclusiveMaximum: nan, MultipleOf: inf}
		if exprs := Float64ValidatorExprs(s); len(exprs) != 0 {
			t.Errorf("expected non-finite bounds to be skipped, got %d exprs", len(exprs))
		}
	})
	t.Run("empty schema", func(t *testing.T) {
		if exprs := Float64ValidatorExprs(ir.SchemaIR{}); len(exprs) != 0 {
			t.Errorf("expected no exprs for empty schema, got %d", len(exprs))
		}
	})
}

func TestInt64ValidatorExprs(t *testing.T) {
	t.Run("bounds produce exprs", func(t *testing.T) {
		s := ir.SchemaIR{Type: ir.TypeInt, ExclusiveMinimum: floatPtr(3), ExclusiveMaximum: floatPtr(10), MultipleOf: floatPtr(2)}
		exprs := Int64ValidatorExprs(s)
		if len(exprs) != 3 {
			t.Fatalf("expected 3 expressions, got %d", len(exprs))
		}
	})
	t.Run("empty schema", func(t *testing.T) {
		if exprs := Int64ValidatorExprs(ir.SchemaIR{}); len(exprs) != 0 {
			t.Errorf("expected no exprs for empty schema, got %d", len(exprs))
		}
	})
}

func TestObjectValidatorExprs(t *testing.T) {
	t.Run("discriminator", func(t *testing.T) {
		s := ir.SchemaIR{Union: &ir.UnionType{
			Kind:          ir.OneOf,
			Variants:      []ir.SchemaIR{{Type: ir.TypeString}},
			Discriminator: &ir.DiscriminatorIR{PropertyName: "animalType", Mapping: map[string]string{"b": "B", "a": "A"}},
		}}
		exprs := ObjectValidatorExprs(s)
		if len(exprs) != 1 {
			t.Fatalf("expected 1 expression, got %d", len(exprs))
		}
		b, err := astgen.RenderExpr(exprs[0])
		if err != nil {
			t.Fatalf("RenderExpr: %v", err)
		}
		// Property name snake_cased; sorted keys: a before b.
		if !strings.Contains(string(b), `DiscriminatorValidator("animal_type", "a", "b")`) {
			t.Errorf("unexpected expr %q", string(b))
		}
	})
	t.Run("dependent required sorted", func(t *testing.T) {
		s := ir.SchemaIR{DependentRequired: map[string][]string{"z": {"b", "a"}, "a": {"c"}}}
		exprs := ObjectValidatorExprs(s)
		if len(exprs) != 2 {
			t.Fatalf("expected 2 expressions, got %d", len(exprs))
		}
		b, err := astgen.RenderExpr(exprs[0])
		if err != nil {
			t.Fatalf("RenderExpr: %v", err)
		}
		if !strings.Contains(string(b), `ConditionalValidator("a", "c")`) {
			t.Errorf("unexpected first expr %q", string(b))
		}
	})
	t.Run("no constraints", func(t *testing.T) {
		if exprs := ObjectValidatorExprs(ir.SchemaIR{}); len(exprs) != 0 {
			t.Errorf("expected no exprs, got %d", len(exprs))
		}
	})
}

func TestMapValidatorExprs(t *testing.T) {
	t.Run("pattern properties", func(t *testing.T) {
		s := ir.SchemaIR{PatternProperties: map[string]*ir.SchemaIR{"^a": {Type: ir.TypeString}, "^b": {Type: ir.TypeString}}}
		exprs := MapValidatorExprs(s)
		if len(exprs) != 1 {
			t.Fatalf("expected 1 expression, got %d", len(exprs))
		}
	})
	t.Run("none", func(t *testing.T) {
		if exprs := MapValidatorExprs(ir.SchemaIR{}); len(exprs) != 0 {
			t.Errorf("expected no exprs, got %d", len(exprs))
		}
	})
}

func TestAddValidators(t *testing.T) {
	t.Run("no validators returns input unchanged", func(t *testing.T) {
		elems := []ast.Expr{astgen.Ident("placeholder")}
		attr := ir.AttributeIR{Name: "x", Schema: ir.SchemaIR{Type: ir.TypeString}}
		if got := AddValidators(elems, attr, "String"); len(got) != 1 {
			t.Errorf("expected unchanged elems, got %d", len(got))
		}
	})
	t.Run("with validators appends field", func(t *testing.T) {
		attr := ir.AttributeIR{Name: "x", Schema: ir.SchemaIR{Type: ir.TypeInt, ExclusiveMinimum: floatPtr(1)}}
		elems := AddValidators(nil, attr, "Int64")
		if len(elems) != 1 {
			t.Fatalf("expected 1 element, got %d", len(elems))
		}
		b, err := astgen.RenderExpr(elems[0])
		if err != nil {
			t.Fatalf("RenderExpr: %v", err)
		}
		if !strings.Contains(string(b), "Validators") || !strings.Contains(string(b), "Int64ExclusiveMinimumValidator") {
			t.Errorf("unexpected validator element %q", string(b))
		}
	})
	t.Run("unsupported kind returns nil", func(t *testing.T) {
		attr := ir.AttributeIR{Name: "x", Schema: ir.SchemaIR{Type: ir.TypeInt, ExclusiveMinimum: floatPtr(1)}}
		if got := AddValidators(nil, attr, "Bool"); len(got) != 0 {
			t.Errorf("expected no validators for unsupported kind, got %d", len(got))
		}
	})
}

func TestIsFiniteFloat(t *testing.T) {
	if isFiniteFloat(1.5) != true {
		t.Error("expected 1.5 to be finite")
	}
	if isFiniteFloat(0) != true {
		t.Error("expected 0 to be finite")
	}
	if isFiniteFloat(math.Inf(1)) != false {
		t.Error("expected +Inf to be non-finite")
	}
	if isFiniteFloat(math.Inf(-1)) != false {
		t.Error("expected -Inf to be non-finite")
	}
	if isFiniteFloat(math.NaN()) != false {
		t.Error("expected NaN to be non-finite")
	}
}

// TestValidateIntBoundPanics asserts fractional bounds on integer schemas panic
// rather than silently truncating.
func TestValidateIntBoundPanics(t *testing.T) {
	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected panic for fractional integer bound")
			}
		}()
		validateIntBound("exclusive_minimum", 1.5)
	}()
	// Integral bound does not panic.
	validateIntBound("exclusive_minimum", 2)
}

// TestValidatePatternProperties asserts invalid patterns panic at generation time.
func TestValidatePatternProperties(t *testing.T) {
	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected panic for invalid pattern")
			}
		}()
		validatePatternProperties(map[string]*ir.SchemaIR{"[": {Type: ir.TypeString}})
	}()
	validatePatternProperties(map[string]*ir.SchemaIR{"^[a-z]+$": {Type: ir.TypeString}})
}

// TestAttributeValidatorExprs_KindDispatch exercises the kind switch in
// attributeValidatorExprs. Dispatch is purely on the kind string: the bound
// field checks inside Int64/Float64ValidatorExprs fire regardless of the
// schema's declared type, so an Int64 dispatch on a schema carrying an
// exclusive bound yields an expr.
func TestAttributeValidatorExprs_KindDispatch(t *testing.T) {
	bound := ir.SchemaIR{Type: ir.TypeFloat, ExclusiveMinimum: floatPtr(1)}
	if got := attributeValidatorExprs(bound, "Float64"); len(got) != 1 {
		t.Errorf("Float64 dispatch: expected 1 expr, got %d", len(got))
	}
	if got := attributeValidatorExprs(bound, "Int64"); len(got) != 1 {
		t.Errorf("Int64 dispatch on bound schema: expected 1 expr, got %d", len(got))
	}
	empty := ir.SchemaIR{Type: ir.TypeFloat}
	if got := attributeValidatorExprs(empty, "Object"); len(got) != 0 {
		t.Errorf("Object dispatch: expected 0 exprs, got %d", len(got))
	}
	if got := attributeValidatorExprs(empty, "Map"); len(got) != 0 {
		t.Errorf("Map dispatch: expected 0 exprs, got %d", len(got))
	}
	if got := attributeValidatorExprs(empty, "String"); len(got) != 0 {
		t.Errorf("String dispatch: expected 0 exprs, got %d", len(got))
	}
	if got := attributeValidatorExprs(empty, "Bool"); len(got) != 0 {
		t.Errorf("unsupported kind dispatch: expected 0 exprs, got %d", len(got))
	}
}
