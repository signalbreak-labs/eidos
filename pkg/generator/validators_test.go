package generator

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/generator/internal/schema"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// TestValidatorsFile_NoValidators verifies that a provider IR with no advanced
// constraints still emits a valid (if minimal) validators.go file.
func TestValidatorsFile_NoValidators(t *testing.T) {
	providerIR := sampleProviderIR()

	file := ValidatorsFile(providerIR)
	if file.Path != "internal/provider/validators.go" {
		t.Fatalf("unexpected file path: %q", file.Path)
	}

	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, "package provider") {
		t.Errorf("generated validators.go missing package declaration\n%s", got)
	}
}

// TestValidatorsFile_ExclusiveBounds verifies that numeric exclusive bounds and
// multipleOf constraints cause the corresponding float64/int64 validators to be
// generated.
func TestValidatorsFile_ExclusiveBounds(t *testing.T) {
	floatVal := 1.5
	intVal := 7.0
	providerIR := ir.ProviderIR{
		Name:     "mycloud",
		TypeName: "mycloud",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "float_attr",
					Optional: true,
					Schema: ir.SchemaIR{
						Type:             ir.TypeFloat,
						ExclusiveMinimum: &floatVal,
						ExclusiveMaximum: &floatVal,
						MultipleOf:       &floatVal,
					},
				},
				{
					Name:     "int_attr",
					Optional: true,
					Schema: ir.SchemaIR{
						Type:             ir.TypeInt,
						ExclusiveMinimum: &intVal,
						ExclusiveMaximum: &intVal,
						MultipleOf:       &intVal,
					},
				},
			},
		},
	}

	file := ValidatorsFile(providerIR)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"type float64ExclusiveMinimumValidator struct",
		"type float64ExclusiveMaximumValidator struct",
		"type float64MultipleOfValidator struct",
		"type int64ExclusiveMinimumValidator struct",
		"type int64ExclusiveMaximumValidator struct",
		"type int64MultipleOfValidator struct",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated validators.go missing %q\n%s", want, got)
		}
	}
}

// TestValidatorsFile_DiscriminatorConditionalPatternProperties verifies that
// union discriminators, conditional schemas, and patternProperties cause the
// corresponding object/map validators to be generated.
func TestValidatorsFile_DiscriminatorConditionalPatternProperties(t *testing.T) {
	providerIR := ir.ProviderIR{
		Name:     "mycloud",
		TypeName: "mycloud",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "pet",
					Optional: true,
					Schema: ir.SchemaIR{
						Union: &ir.UnionType{
							Discriminator: &ir.DiscriminatorIR{
								PropertyName: "petType",
								Mapping:      map[string]string{"cat": "Cat", "dog": "Dog"},
							},
							Variants: []ir.SchemaIR{
								{Attributes: []ir.AttributeIR{{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
							},
						},
					},
				},
				{
					Name:     "conditional",
					Optional: true,
					Schema: ir.SchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "a", Schema: ir.SchemaIR{Type: ir.TypeBool}},
							{Name: "b", Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
						IfSchema: &ir.SchemaIR{Attributes: []ir.AttributeIR{
							{Name: "a", Schema: ir.SchemaIR{Type: ir.TypeBool, Const: schemaConstBoolPtr(true)}},
						}},
						ThenSchema: &ir.SchemaIR{Attributes: []ir.AttributeIR{
							{Name: "b", Schema: ir.SchemaIR{Type: ir.TypeString, Required: true}},
						}},
						DependentRequired: map[string][]string{"a": {"b"}},
					},
				},
				{
					Name:     "pattern_props",
					Optional: true,
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind:        ir.Map,
							ElementType: ir.SchemaIR{Type: ir.TypeString},
						},
						PatternProperties: map[string]*ir.SchemaIR{
							"^prefix_": {Type: ir.TypeString},
						},
					},
				},
			},
		},
	}

	file := ValidatorsFile(providerIR)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"type discriminatorValidator struct",
		"type conditionalValidator struct",
		"type patternPropertiesValidator struct",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated validators.go missing %q\n%s", want, got)
		}
	}
}

// TestValidatorsFile_SchemaValidation generates a provider that uses every
// custom validator and confirms the generated validators.go compiles and the
// provider schema validates.
func TestValidatorsFile_SchemaValidation(t *testing.T) {
	floatVal := 1.5
	intVal := 7.0
	providerIR := ir.ProviderIR{
		Name:     "mycloud",
		TypeName: "mycloud",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "api_key",
					Required: true,
					Schema:   ir.SchemaIR{Type: ir.TypeString},
				},
			},
		},
		Resources: []ir.ResourceIR{
			{
				Name:     "constrained",
				TypeName: "mycloud_constrained",
				Schema: ir.ObjectSchemaIR{
					Attributes: []ir.AttributeIR{
						{
							Name:     "float_attr",
							Optional: true,
							Schema: ir.SchemaIR{
								Type:             ir.TypeFloat,
								ExclusiveMinimum: &floatVal,
								ExclusiveMaximum: &floatVal,
								MultipleOf:       &floatVal,
							},
						},
						{
							Name:     "int_attr",
							Optional: true,
							Schema: ir.SchemaIR{
								Type:             ir.TypeInt,
								ExclusiveMinimum: &intVal,
								ExclusiveMaximum: &intVal,
								MultipleOf:       &intVal,
							},
						},
						{
							Name:     "pet",
							Optional: true,
							Schema: ir.SchemaIR{
								Union: &ir.UnionType{
									Discriminator: &ir.DiscriminatorIR{
										PropertyName: "petType",
										Mapping:      map[string]string{"cat": "Cat"},
									},
									Variants: []ir.SchemaIR{
										{Attributes: []ir.AttributeIR{{Name: "petType", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
									},
								},
							},
						},
						{
							Name:     "conditional",
							Optional: true,
							Schema: ir.SchemaIR{
								Attributes: []ir.AttributeIR{
									{Name: "a", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeBool}},
									{Name: "b", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
								},
								IfSchema:          &ir.SchemaIR{},
								DependentRequired: map[string][]string{"a": {"b"}},
							},
						},
						{
							Name:     "pattern_props",
							Optional: true,
							Schema: ir.SchemaIR{
								Collection: &ir.CollectionType{
									Kind:        ir.Map,
									ElementType: ir.SchemaIR{Type: ir.TypeString},
								},
								PatternProperties: map[string]*ir.SchemaIR{
									"^prefix_": {Type: ir.TypeString},
								},
							},
						},
					},
				},
			},
		},
	}

	tmp := generateProviderModuleWithValidators(t, providerIR)
	writeValidatorsSchemaValidationTest(t, tmp)

	ctx, cancel := contextWithTimeout(t, 5*time.Minute)
	defer cancel()

	tidyCmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidyCmd.Dir = tmp
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\n%s", err, out)
	}

	cmd := exec.CommandContext(ctx, "go", "test", "./internal/provider", "-v")
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}

	if !strings.Contains(string(out), "PASS") {
		t.Fatalf("expected test to pass, got:\n%s", out)
	}
}

// TestValidatorsFile_Float64MultipleOfTolerance verifies the generated float64
// multipleOf validator uses a tolerance-based remainder check instead of a
// direct floating-point equality.
func TestValidatorsFile_Float64MultipleOfTolerance(t *testing.T) {
	factor := 0.1
	pir := ir.ProviderIR{
		Name:     "mycloud",
		TypeName: "mycloud",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "float_attr",
					Optional: true,
					Schema: ir.SchemaIR{
						Type:       ir.TypeFloat,
						MultipleOf: &factor,
					},
				},
			},
		},
	}

	got := renderValidatorsFile(t, pir)
	if !strings.Contains(got, `remainder := math.Mod(value, v.factor)`) {
		t.Errorf("generated validators.go missing remainder declaration\n%s", got)
	}
	if !strings.Contains(got, `math.Abs(remainder) > 1e-09`) {
		t.Errorf("generated validators.go missing tolerance lower-bound check\n%s", got)
	}
	if !strings.Contains(got, `math.Abs(remainder-v.factor) > 1e-09`) {
		t.Errorf("generated validators.go missing tolerance upper-bound check\n%s", got)
	}
}

// TestValidatorsFile_Float64MultipleOfZeroDiagnostic verifies the generated
// float64 multipleOf validator reports an invalid-configuration diagnostic when
// the factor is zero.
func TestValidatorsFile_Float64MultipleOfZeroDiagnostic(t *testing.T) {
	factor := 0.0
	pir := ir.ProviderIR{
		Name:     "mycloud",
		TypeName: "mycloud",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "float_attr",
					Optional: true,
					Schema: ir.SchemaIR{
						Type:       ir.TypeFloat,
						MultipleOf: &factor,
					},
				},
			},
		},
	}

	got := renderValidatorsFile(t, pir)
	if !strings.Contains(got, "Invalid Validator Configuration") {
		t.Errorf("generated validators.go missing invalid-configuration diagnostic\n%s", got)
	}
	if !strings.Contains(got, "multipleOf factor must not be zero") {
		t.Errorf("generated validators.go missing zero-factor message\n%s", got)
	}
}

// TestValidatorsFile_Int64MultipleOfZeroDiagnostic verifies the generated int64
// multipleOf validator reports an invalid-configuration diagnostic when the
// factor is zero.
func TestValidatorsFile_Int64MultipleOfZeroDiagnostic(t *testing.T) {
	factor := 0.0
	pir := ir.ProviderIR{
		Name:     "mycloud",
		TypeName: "mycloud",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "int_attr",
					Optional: true,
					Schema: ir.SchemaIR{
						Type:       ir.TypeInt,
						MultipleOf: &factor,
					},
				},
			},
		},
	}

	got := renderValidatorsFile(t, pir)
	if !strings.Contains(got, "Invalid Validator Configuration") {
		t.Errorf("generated validators.go missing invalid-configuration diagnostic\n%s", got)
	}
	if !strings.Contains(got, "multipleOf factor must not be zero") {
		t.Errorf("generated validators.go missing zero-factor message\n%s", got)
	}
}

// TestValidatorsFile_Int64NonIntegerBoundPanics verifies that fractional bounds
// on integer schemas are rejected at generation time instead of being
// silently truncated.
func TestValidatorsFile_Int64NonIntegerBoundPanics(t *testing.T) {
	bound := 7.5
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for non-integer int bound")
		}
		if !strings.Contains(fmt.Sprint(r), "non-integer bound") {
			t.Fatalf("unexpected panic message: %v", r)
		}
	}()
	_ = schema.Int64ValidatorExprs(ir.SchemaIR{Type: ir.TypeInt, ExclusiveMinimum: &bound})
}

// TestValidatorsFile_InvalidPatternPropertiesPanics verifies that invalid
// patternProperties regular expressions are rejected at generation time.
func TestValidatorsFile_InvalidPatternPropertiesPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for invalid patternProperties pattern")
		}
		if !strings.Contains(fmt.Sprint(r), "invalid patternProperties pattern") {
			t.Fatalf("unexpected panic message: %v", r)
		}
	}()
	_ = schema.MapValidatorExprs(ir.SchemaIR{
		Collection: &ir.CollectionType{
			Kind:        ir.Map,
			ElementType: ir.SchemaIR{Type: ir.TypeString},
		},
		PatternProperties: map[string]*ir.SchemaIR{
			"[invalid": {Type: ir.TypeString},
		},
	})
}

// TestValidatorsFile_ConditionalIfSchemaOnlyNoPlaceholder verifies that a
// schema with if/then/else but no dependentRequired does not emit a no-op
// ConditionalValidator with an empty trigger.
func TestValidatorsFile_ConditionalIfSchemaOnlyNoPlaceholder(t *testing.T) {
	pir := ir.ProviderIR{
		Name:     "mycloud",
		TypeName: "mycloud",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "conditional",
					Optional: true,
					Schema: ir.SchemaIR{
						IfSchema:   &ir.SchemaIR{},
						ThenSchema: &ir.SchemaIR{},
					},
				},
			},
		},
	}

	got := renderValidatorsFile(t, pir)
	if strings.Contains(got, `ConditionalValidator("")`) {
		t.Errorf("generated validators.go should not emit empty ConditionalValidator\n%s", got)
	}
	if strings.Contains(got, "type conditionalValidator struct") {
		t.Errorf("generated validators.go should not generate unused conditionalValidator\n%s", got)
	}
}

// TestValidatorsFile_DiscriminatorDescriptionJoins verifies that the generated
// discriminator validator uses strings.Join to format the allowed slice in
// description strings.
func TestValidatorsFile_DiscriminatorDescriptionJoins(t *testing.T) {
	pir := ir.ProviderIR{
		Name:     "mycloud",
		TypeName: "mycloud",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "pet",
					Optional: true,
					Schema: ir.SchemaIR{
						Union: &ir.UnionType{
							Discriminator: &ir.DiscriminatorIR{
								PropertyName: "petType",
								Mapping:      map[string]string{"cat": "Cat", "dog": "Dog"},
							},
							Variants: []ir.SchemaIR{
								{Attributes: []ir.AttributeIR{{Name: "petType", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
							},
						},
					},
				},
			},
		},
	}

	got := renderValidatorsFile(t, pir)
	if !strings.Contains(got, `strings.Join(v.allowed, ", ")`) {
		t.Errorf("generated validators.go should use strings.Join for allowed values\n%s", got)
	}
	if !strings.Contains(got, "must be one of [%s]") {
		t.Errorf("generated validators.go should use %%s for allowed list in description\n%s", got)
	}
}

// renderValidatorsFile renders internal/provider/validators.go for the given
// provider IR and returns its source.
func renderValidatorsFile(t *testing.T, pir ir.ProviderIR) string {
	t.Helper()
	file := ValidatorsFile(pir)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	return buf.String()
}

// generateProviderModuleWithValidators writes the generated go.mod, provider.go,
// resource files, and validators.go into a temporary module directory and
// returns the module root.
func generateProviderModuleWithValidators(t *testing.T, providerIR ir.ProviderIR) string {
	t.Helper()
	tmp := t.TempDir()

	cfg := BuildConfig{
		ProviderName: providerIR.Name,
		Namespace:    providerIR.Name,
	}

	h := Harness{OutputDir: tmp}
	files := resourceModuleFiles(t, providerIR, cfg)
	files = append(files, ValidatorsFile(providerIR))
	if err := h.Generate(files); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	return tmp
}

// writeValidatorsSchemaValidationTest writes a test that imports the generated
// provider and resource, exercising both provider and resource schema
// validation so the generated validators.go must compile.
func writeValidatorsSchemaValidationTest(t *testing.T, moduleRoot string) {
	t.Helper()
	testPath := filepath.Join(moduleRoot, "internal", "provider", "validators_validate_test.go")
	if err := os.MkdirAll(filepath.Dir(testPath), 0o750); err != nil {
		t.Fatalf("create provider test dir: %v", err)
	}

	content := `package provider

import (
	"context"
	"testing"

	tfframeworkprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

func TestValidatorsSchemaValidation(t *testing.T) {
	p := New()
	var providerResp tfframeworkprovider.SchemaResponse
	p.Schema(context.Background(), tfframeworkprovider.SchemaRequest{}, &providerResp)
	if diags := providerResp.Schema.ValidateImplementation(context.Background()); diags.HasError() {
		t.Fatalf("provider schema validation failed: %s", diags)
	}

	r := &ConstrainedResource{}
	var resourceResp resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &resourceResp)
	if diags := resourceResp.Schema.ValidateImplementation(context.Background()); diags.HasError() {
		t.Fatalf("resource schema validation failed: %s", diags)
	}
}
`
	if err := os.WriteFile(testPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write validators schema validation test: %v", err)
	}
}

// schemaConstBoolPtr returns a pointer to a bool suitable for SchemaIR.Const.
func schemaConstBoolPtr(v bool) *any {
	a := any(v)
	return &a
}
