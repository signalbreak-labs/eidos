package generator

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/schema"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// TestProviderFile_Render verifies that ProviderFile emits the expected
// provider struct, config model, Metadata, Schema, Configure, and
// registration methods.
func TestProviderFile_Render(t *testing.T) {
	pir := sampleProviderIR()

	file, err := ProviderFile(pir)
	if err != nil {
		t.Fatalf("ProviderFile() error = %v", err)
	}
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"package provider",
		"type mycloudProvider struct",
		"type mycloudProviderModel struct",
		// Model struct fields (gofmt aligns the struct columns, and the
		// generator-owned log_* attributes widen them, so match the tfsdk tags
		// rather than exact spacing).
		"ApiKey",
		"`tfsdk:\"api_key\"`",
		"Timeout",
		"`tfsdk:\"timeout\"`",
		"func New()",
		"func (p *mycloudProvider) Metadata",
		"func (p *mycloudProvider) Schema",
		"func (p *mycloudProvider) Configure",
		"func (p *mycloudProvider) DataSources",
		"func (p *mycloudProvider) Resources",
		"func (p *mycloudProvider) Functions",
		"func (p *mycloudProvider) EphemeralResources",
		"func (p *mycloudProvider) ListResources",
		"ProviderWithListResources",
		"resp.TypeName = \"mycloud\"",
		"schema.StringAttribute",
		"Required:",
		"Optional:",
		"Sensitive:",
		"schema.Int64Attribute",
		"return &PetResource{}",
		"return &PetsDataSource{}",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated provider.go missing %q\ncontent:\n%s", want, got)
		}
	}

	// The endpoint attribute is Optional, so the generated schema must contain
	// an Optional field (and not be silently inverted to Required).
	endpointIdx := strings.Index(got, `"endpoint":`)
	if endpointIdx == -1 {
		t.Errorf("generated provider.go missing endpoint attribute\ncontent:\n%s", got)
	} else {
		endpointBlock := got[endpointIdx:]
		if i := strings.Index(endpointBlock, "},\n"); i != -1 {
			endpointBlock = endpointBlock[:i+2]
		} else if i := strings.Index(endpointBlock, "},"); i != -1 {
			endpointBlock = endpointBlock[:i+2]
		}
		if !strings.Contains(endpointBlock, "Optional:") {
			t.Errorf("generated provider.go endpoint attribute missing Optional marker\nendpoint block:\n%s", endpointBlock)
		}
		if strings.Contains(endpointBlock, "Required:") {
			t.Errorf("generated provider.go endpoint attribute incorrectly marked Required\nendpoint block:\n%s", endpointBlock)
		}
	}
}

// TestProviderFile_ProviderSchemaInProcess exercises the generated provider
// schema without launching an external `go test`. It verifies that the
// generator emits a schema that the plugin-framework considers valid by
// compiling a tiny generated provider module and running ValidateImplementation.
//
// This is still a compile-time check, so it is gated by testing.Short() to
// keep the default `go test` path fast.
func TestProviderFile_ProviderSchemaInProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping compile-time schema validation in short mode")
	}

	pir := ir.ProviderIR{
		Name:        "mycloud",
		TypeName:    "mycloud",
		Description: "Generated provider for MyCloud.",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:        "api_key",
					Description: "API key authentication",
					Required:    true,
					Sensitive:   true,
					Schema:      ir.SchemaIR{Type: ir.TypeString},
				},
				{
					Name:     "endpoint",
					Optional: true,
					Schema:   ir.SchemaIR{Type: ir.TypeString},
				},
				{
					Name:     "timeout",
					Optional: true,
					Schema:   ir.SchemaIR{Type: ir.TypeInt},
				},
				{
					Name:     "nested",
					Optional: true,
					Schema: ir.SchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "url", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				},
			},
		},
	}

	file, err := ProviderFile(pir)
	if err != nil {
		t.Fatalf("ProviderFile() error = %v", err)
	}

	tmp := t.TempDir()
	h := Harness{OutputDir: tmp}
	if err := h.Generate([]File{file}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	cfg := BuildConfig{
		ProviderName: pir.Name,
		Namespace:    pir.Name,
	}
	buildFiles := BuildFiles(cfg)
	if err := h.Generate(buildFiles); err != nil {
		t.Fatalf("Generate build files error = %v", err)
	}

	writeSchemaValidationTest(t, tmp)

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

// TestProviderFile_EmptyConfigSchemaCompiles is a regression test for H-8: a
// provider with an empty config schema must still compile. The generated
// provider.go previously imported the types package unconditionally even
// though types.* is only referenced when config attributes or blocks exist,
// producing "imported and not used" for the common production case where
// ConfigSchema is empty.
func TestProviderFile_EmptyConfigSchemaCompiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}

	pir := ir.ProviderIR{
		Name:        "mycloud",
		TypeName:    "mycloud",
		Description: "Generated provider for MyCloud.",
	}

	file, err := ProviderFile(pir)
	if err != nil {
		t.Fatalf("ProviderFile() error = %v", err)
	}

	tmp := t.TempDir()
	h := Harness{OutputDir: tmp}
	if err := h.Generate([]File{file}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	cfg := BuildConfig{
		ProviderName: pir.Name,
		Namespace:    pir.Name,
	}
	if err := h.Generate(BuildFiles(cfg)); err != nil {
		t.Fatalf("Generate build files error = %v", err)
	}

	ctx, cancel := contextWithTimeout(t, 5*time.Minute)
	defer cancel()

	tidyCmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidyCmd.Dir = tmp
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\n%s", err, out)
	}

	cmd := exec.CommandContext(ctx, "go", "build", "./internal/provider")
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build failed for provider with empty config schema (H-8 regression): %v\n%s", err, out)
	}
}

// TestProviderFile_SchemaValidation is the original integration test. It is
// kept for backward compatibility (external callers may link against this
// test name) but is functionally identical to
// TestProviderFile_ProviderSchemaInProcess. It is skipped in short mode
// because it downloads dependencies and compiles a temporary module.
//
// Deprecated: prefer TestProviderFile_ProviderSchemaInProcess.
func TestProviderFile_SchemaValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-dependent integration test in short mode")
	}

	pir := ir.ProviderIR{
		Name:        "mycloud",
		TypeName:    "mycloud",
		Description: "Generated provider for MyCloud.",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:        "api_key",
					Description: "API key authentication",
					Required:    true,
					Sensitive:   true,
					Schema:      ir.SchemaIR{Type: ir.TypeString},
				},
				{
					Name:     "endpoint",
					Optional: true,
					Schema:   ir.SchemaIR{Type: ir.TypeString},
				},
				{
					Name:     "timeout",
					Optional: true,
					Schema:   ir.SchemaIR{Type: ir.TypeInt},
				},
				{
					Name:     "nested",
					Optional: true,
					Schema: ir.SchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "url", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				},
			},
		},
	}

	tmp := generateProviderModule(t, pir)
	writeSchemaValidationTest(t, tmp)

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

// TestProviderFile_ResourceRegistration verifies that the generated provider
// references the expected resource, data source, function, and ephemeral
// resource constructors when they are present in the IR.
func TestProviderFile_ResourceRegistration(t *testing.T) {
	pir := sampleProviderIR()

	file, err := ProviderFile(pir)
	if err != nil {
		t.Fatalf("ProviderFile() error = %v", err)
	}
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, "return &PetResource{}") {
		t.Errorf("Resources registration missing PetResource constructor")
	}
	if !strings.Contains(got, "return &PetsDataSource{}") {
		t.Errorf("DataSources registration missing PetsDataSource constructor")
	}
	if !strings.Contains(got, "return &ConcatTagsFunction{}") {
		t.Errorf("Functions registration missing ConcatTagsFunction constructor")
	}
	if !strings.Contains(got, "return &TemporaryCredentialEphemeralResource{}") {
		t.Errorf("EphemeralResources registration missing TemporaryCredentialEphemeralResource constructor")
	}
}

// TestProviderFile_RejectsComputedConfigAttributes verifies that ProviderFile
// returns an error when a provider config attribute is marked Computed.
func TestProviderFile_RejectsComputedConfigAttributes(t *testing.T) {
	pir := sampleProviderIR()
	pir.ConfigSchema.Attributes = append(pir.ConfigSchema.Attributes, ir.AttributeIR{
		Name:     "computed_attr",
		Computed: true,
		Schema:   ir.SchemaIR{Type: ir.TypeString},
	})

	_, err := ProviderFile(pir)
	if err == nil {
		t.Fatal("expected error for Computed provider config attribute, got nil")
	}
	if !strings.Contains(err.Error(), "cannot be Computed") {
		t.Fatalf("expected error to mention Computed, got: %v", err)
	}
}

// TestProviderFile_RejectsWriteOnlyConfigAttributes verifies that ProviderFile
// returns an error when a provider config attribute is marked WriteOnly.
func TestProviderFile_RejectsWriteOnlyConfigAttributes(t *testing.T) {
	pir := sampleProviderIR()
	pir.ConfigSchema.Attributes = append(pir.ConfigSchema.Attributes, ir.AttributeIR{
		Name:      "write_only_attr",
		WriteOnly: true,
		Schema:    ir.SchemaIR{Type: ir.TypeString},
	})

	_, err := ProviderFile(pir)
	if err == nil {
		t.Fatal("expected error for WriteOnly provider config attribute, got nil")
	}
	if !strings.Contains(err.Error(), "cannot be WriteOnly") {
		t.Fatalf("expected error to mention WriteOnly, got: %v", err)
	}
}

// TestProviderFile_RejectsUnknownModelTypes verifies that ProviderFile returns
// an error for a provider config attribute whose schema type is not supported.
func TestProviderFile_RejectsUnknownModelTypes(t *testing.T) {
	pir := sampleProviderIR()
	pir.ConfigSchema.Attributes = append(pir.ConfigSchema.Attributes, ir.AttributeIR{
		Name:     "unknown",
		Optional: true,
		Schema:   ir.SchemaIR{Type: ir.TypeNull},
	})

	_, err := ProviderFile(pir)
	if err == nil {
		t.Fatal("expected error for unsupported model type, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported primitive type") {
		t.Fatalf("expected error to mention unsupported primitive type, got: %v", err)
	}
}

// TestProviderFile_RejectsComputedInNestedCollection verifies that
// ProviderFile returns an error when a provider config attribute is a
// collection of objects containing a Computed attribute.
func TestProviderFile_RejectsComputedInNestedCollection(t *testing.T) {
	pir := sampleProviderIR()
	pir.ConfigSchema.Attributes = append(pir.ConfigSchema.Attributes, ir.AttributeIR{
		Name:     "regions",
		Optional: true,
		Schema: ir.SchemaIR{
			Collection: &ir.CollectionType{
				Kind: ir.List,
				ElementType: ir.SchemaIR{
					Attributes: []ir.AttributeIR{
						{Name: "id", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
					},
				},
			},
		},
	})

	_, err := ProviderFile(pir)
	if err == nil {
		t.Fatal("expected error for Computed attribute inside collection element, got nil")
	}
	if !strings.Contains(err.Error(), "cannot be Computed") {
		t.Fatalf("expected error to mention Computed, got: %v", err)
	}
}

// TestProviderFile_RejectsUnknownNestingMode verifies that ProviderFile returns
// an error for a provider config block with an unsupported nesting mode.
func TestProviderFile_RejectsUnknownNestingMode(t *testing.T) {
	cases := []struct {
		name        string
		nestingMode ir.BlockNestingMode
	}{
		{name: "unknown", nestingMode: "unknown"},
		{name: "group", nestingMode: "group"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pir := sampleProviderIR()
			pir.ConfigSchema.Blocks = []ir.BlockIR{
				{
					Name:        "bad_block",
					NestingMode: tc.nestingMode,
					Schema: ir.ObjectSchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "x", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				},
			}

			_, err := ProviderFile(pir)
			if err == nil {
				t.Fatal("expected error for unsupported block nesting mode, got nil")
			}
			if !strings.Contains(err.Error(), "unsupported nesting mode") {
				t.Fatalf("expected error to mention unsupported nesting mode, got: %v", err)
			}
		})
	}
}

// TestProviderFile_RecoversValidatorPanic locks in the M-56 fix: a hostile
// provider config attribute that triggers a validator-builder panic (a
// fractional exclusiveMinimum on an integer schema panics in validateIntBound)
// must surface as a generation error, not crash the process. Before the recover
// wrapper, FilesForProviderIR could terminate the generator on such IR.
//
// N-31 tightens the classification: a panic wrapping
// schema.ErrInvalidValidatorConstraint is surfaced as an "invalid validator
// constraint" (a spec problem, e.g. multipleOf: 2.5 on an integer or an
// RE2-invalid patternProperties pattern) rather than a generic "renderer panic"
// that is indistinguishable from a genuine generator bug, and errors.Is
// matches the sentinel through the render boundary.
func TestProviderFile_RecoversValidatorPanic(t *testing.T) {
	frac := 0.5
	patternProperties := map[string]*ir.SchemaIR{
		"(": {Type: ir.TypeString}, // invalid RE2: unclosed group
	}
	hostile := []struct {
		name string
		attr ir.AttributeIR
	}{
		{
			name: "fractional integer exclusiveMinimum",
			attr: ir.AttributeIR{
				Name:     "count",
				Optional: true,
				Schema: ir.SchemaIR{
					Type:             ir.TypeInt,
					ExclusiveMinimum: &frac,
				},
			},
		},
		{
			name: "RE2-invalid patternProperties pattern",
			attr: ir.AttributeIR{
				Name:     "labels",
				Optional: true,
				Schema: ir.SchemaIR{
					Collection: &ir.CollectionType{
						Kind:        ir.Map,
						ElementType: ir.SchemaIR{Type: ir.TypeString},
					},
					PatternProperties: patternProperties,
				},
			},
		},
	}

	for _, tc := range hostile {
		t.Run(tc.name, func(t *testing.T) {
			pir := sampleProviderIR()
			pir.ConfigSchema.Attributes = append(pir.ConfigSchema.Attributes, tc.attr)

			// ProviderFile must not panic; it must return an error that is
			// classified as an invalid spec constraint, not a renderer bug.
			defer func() {
				if rec := recover(); rec != nil {
					t.Fatalf("ProviderFile panicked instead of returning an error: %v", rec)
				}
			}()
			_, err := ProviderFile(pir)
			if err == nil {
				t.Fatal("expected error for hostile validator constraint, got nil")
			}
			if !strings.Contains(err.Error(), "invalid validator constraint") {
				t.Fatalf("expected error to mention invalid validator constraint, got: %v", err)
			}
			if strings.Contains(err.Error(), "renderer panic") {
				t.Fatalf("expected error NOT to be mislabeled as a renderer panic, got: %v", err)
			}
			if !errors.Is(err, schema.ErrInvalidValidatorConstraint) {
				t.Fatalf("expected %v to wrap schema.ErrInvalidValidatorConstraint (N-31)", err)
			}
		})
	}
}

// TestProviderFile_ValidatorImportRegistered is a regression test for M-8: the
// provider file must register the schema/validator import exactly when a
// provider config attribute or block emits a Validators field
// (validator.Int64ExclusiveMinimumValidator etc.). Pre-fix, the import gate
// functions existed but were never called from generateProviderFile, so an
// integral exclusiveMinimum on a config attribute rendered the validator
// reference with no import and the generated provider did not compile.
func TestProviderFile_ValidatorImportRegistered(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}
	skipIfNetworkRestricted(t)

	exclMin := float64(1)
	pir := ir.ProviderIR{
		Name:        "mycloud",
		TypeName:    "mycloud",
		Description: "Generated provider for MyCloud.",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "count",
					Optional: true,
					Schema: ir.SchemaIR{
						Type:             ir.TypeInt,
						ExclusiveMinimum: &exclMin,
					},
				},
			},
		},
	}

	pf, err := ProviderFile(pir)
	if err != nil {
		t.Fatalf("ProviderFile() error = %v", err)
	}
	var buf bytes.Buffer
	if err := pf.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, `"github.com/hashicorp/terraform-plugin-framework/schema/validator"`) {
		t.Errorf("provider file with constrained config attribute must import schema/validator (M-8):\n%s", got)
	}
	// The validator package qualifier is used in the Validators slice type
	// ([]validator.Int64), not on the constructor call itself.
	if !strings.Contains(got, "[]validator.Int64{Int64ExclusiveMinimumValidator(1)}") {
		t.Errorf("provider file with constrained config attribute must reference the validator package in a Validators field:\n%s", got)
	}

	// The generated module must build cleanly (import present and used).
	tmp := t.TempDir()
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}
	h := Harness{OutputDir: tmp}
	files := append(BuildFiles(cfg), pf)
	// The custom validators file defines the referenced validator constructor.
	files = append(files, ValidatorsFile(pir))
	if err := h.Generate(files); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	ctx, cancel := contextWithTimeout(t, 5*time.Minute)
	defer cancel()
	tidyCmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidyCmd.Dir = tmp
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\n%s", err, out)
	}
	cmd := exec.CommandContext(ctx, "go", "build", "./internal/provider")
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build failed for provider with validator import (M-8 regression): %v\n%s", err, out)
	}
}

// TestProviderFile_DigitLeadingNamesCompile is a regression test for M-10:
// digit-leading provider/resource/data source/function names must produce valid
// Go identifiers. Pre-fix, PascalCase("2fa") == "2fa" was used un-sanitized for
// struct names, so Name: "2fa" rendered "2faProvider"/"2faResource" and the
// generated provider failed to parse ("expected ')', found faProvider").
func TestProviderFile_DigitLeadingNamesCompile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}
	skipIfNetworkRestricted(t)

	pir := ir.ProviderIR{
		Name:        "2fa",
		TypeName:    "2fa",
		Description: "Generated provider for 2FA.",
		Resources: []ir.ResourceIR{
			{Name: "2fa_token", TypeName: "2fa_token"},
		},
		DataSources: []ir.DataSourceIR{
			{Name: "2fa_tokens", TypeName: "2fa_tokens"},
		},
	}

	pf, err := ProviderFile(pir)
	if err != nil {
		t.Fatalf("ProviderFile() error = %v", err)
	}
	var buf bytes.Buffer
	if err := pf.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()
	// The sanitized struct names must appear and the raw digit-leading forms must
	// not (they are invalid Go identifiers).
	for _, want := range []string{
		"X2faProvider",
		"X2faProviderModel",
		"X2faTokenResource",
		"X2faTokensDataSource",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated provider missing sanitized struct name %q (M-10):\n%s", want, got)
		}
	}
	// Word-boundary match so the sanitized X2faProvider does not false-positive:
	// the boundary between the X and the 2 is not a word boundary, so the bare
	// invalid identifier 2faProvider (not preceded by a letter/digit) must not
	// appear anywhere in the file.
	for _, bad := range []string{`\b2faProvider\b`, `\b2faTokenResource\b`, `\b2faTokensDataSource\b`} {
		if regexp.MustCompile(bad).MatchString(got) {
			t.Errorf("generated provider must not contain the invalid identifier %q (M-10):\n%s", bad, got)
		}
	}

	// The generated module must build cleanly: provider + resource files, where
	// the digit-leading names flow into struct, model, and file-path derivation.
	resourceOnly := ir.ProviderIR{
		Name:        "2fa",
		TypeName:    "2fa",
		Description: "Generated provider for 2FA.",
		Resources: []ir.ResourceIR{
			{Name: "2fa_token", TypeName: "2fa_token"},
		},
	}
	tmp := generateResourceModule(t, resourceOnly)
	ctx, cancel := contextWithTimeout(t, 5*time.Minute)
	defer cancel()
	tidyCmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidyCmd.Dir = tmp
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\n%s", err, out)
	}
	cmd := exec.CommandContext(ctx, "go", "build", "./internal/provider")
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build failed for digit-leading provider/resource names (M-10 regression): %v\n%s", err, out)
	}
}

// sampleProviderIR returns a ProviderIR with one of each registrable construct.
func sampleProviderIR() ir.ProviderIR {
	return ir.ProviderIR{
		Name:        "mycloud",
		TypeName:    "mycloud",
		Description: "Generated provider for MyCloud.",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:        "api_key",
					Description: "API key authentication",
					Required:    true,
					Sensitive:   true,
					Schema:      ir.SchemaIR{Type: ir.TypeString},
				},
				{
					Name:     "endpoint",
					Optional: true,
					Schema:   ir.SchemaIR{Type: ir.TypeString},
				},
				{
					Name:     "timeout",
					Optional: true,
					Schema:   ir.SchemaIR{Type: ir.TypeInt},
				},
			},
		},
		Resources: []ir.ResourceIR{
			{Name: "pet", TypeName: "mycloud_pet"},
		},
		DataSources: []ir.DataSourceIR{
			{Name: "pets", TypeName: "mycloud_pets"},
		},
		Functions: []ir.FunctionIR{
			{Name: "concat_tags", TypeName: "concat_tags"},
		},
		EphemeralResources: []ir.EphemeralResourceIR{
			{Name: "temporary_credential", TypeName: "mycloud_temporary_credential"},
		},
	}
}

// generateProviderModule writes the generated go.mod and provider.go into a
// temporary module directory and returns the module root.
func generateProviderModule(t *testing.T, pir ir.ProviderIR) string {
	t.Helper()
	tmp := t.TempDir()

	cfg := BuildConfig{
		ProviderName: pir.Name,
		Namespace:    pir.Name,
	}

	h := Harness{OutputDir: tmp}
	pf, err := ProviderFile(pir)
	if err != nil {
		t.Fatalf("ProviderFile() error = %v", err)
	}
	files := append(BuildFiles(cfg), pf)
	if err := h.Generate(files); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	return tmp
}

// writeSchemaValidationTest writes a small test file that imports the generated
// provider and validates its schema implementation.
func writeSchemaValidationTest(t *testing.T, moduleRoot string) {
	t.Helper()
	testPath := filepath.Join(moduleRoot, "internal", "provider", "schema_validate_test.go")
	if err := os.MkdirAll(filepath.Dir(testPath), 0o750); err != nil {
		t.Fatalf("create provider test dir: %v", err)
	}

	content := `package provider

import (
	"context"
	"testing"

	tfframeworkprovider "github.com/hashicorp/terraform-plugin-framework/provider"
)

func TestProviderSchemaValidation(t *testing.T) {
	p := New()
	var resp tfframeworkprovider.SchemaResponse
	p.Schema(context.Background(), tfframeworkprovider.SchemaRequest{}, &resp)

	diags := resp.Schema.ValidateImplementation(context.Background())
	if diags.HasError() {
		t.Fatalf("schema validation failed: %s", diags)
	}
}
`
	if err := os.WriteFile(testPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write schema validation test: %v", err)
	}
}

// contextWithTimeout returns a context suitable for running external go commands
// in tests. It is canceled automatically via t.Cleanup when the test ends.
func contextWithTimeout(t *testing.T, d time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), d)
}

// compile-time interface checks.
var _ = ir.ProviderIR{}
var _ = time.Second

// TestFrameworkAttributeExpr_Branches exercises every branch of
// frameworkAttributeExpr directly: collection kinds (List/Set/Map) with
// primitive and object elements, dynamic-element collections, primitive
// types, discriminated and plain unions, and the empty-schema nil return.
func TestFrameworkAttributeExpr_Branches(t *testing.T) {
	render := func(attr ir.AttributeIR) string {
		expr := frameworkAttributeExpr(attr, "test")
		if expr == nil {
			return "<nil>"
		}
		b, err := astgen.RenderExpr(expr)
		if err != nil {
			t.Fatalf("RenderExpr() error = %v", err)
		}
		return string(b)
	}

	objElem := func() ir.SchemaIR {
		return ir.SchemaIR{Attributes: []ir.AttributeIR{{Name: "name", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}}}}
	}
	col := func(kind ir.CollectionKind, elem ir.SchemaIR) ir.AttributeIR {
		return ir.AttributeIR{Name: "a", Optional: true, Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: kind, ElementType: elem}}}
	}

	cases := []struct {
		name string
		attr ir.AttributeIR
		want string
	}{
		{"list-primitive", col(ir.List, ir.SchemaIR{Type: ir.TypeString}), "schema.ListAttribute"},
		{"list-object", col(ir.List, objElem()), "schema.ListNestedAttribute"},
		{"set-primitive", col(ir.Set, ir.SchemaIR{Type: ir.TypeString}), "schema.SetAttribute"},
		{"set-object", col(ir.Set, objElem()), "schema.SetNestedAttribute"},
		{"map-primitive", col(ir.Map, ir.SchemaIR{Type: ir.TypeString}), "schema.MapAttribute"},
		{"map-object", col(ir.Map, objElem()), "schema.MapNestedAttribute"},
		{"list-dynamic-element", col(ir.List, ir.SchemaIR{Type: ir.TypeDynamic}), "schema.DynamicAttribute"},
		{"float", ir.AttributeIR{Name: "a", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeFloat}}, "schema.Float64Attribute"},
		{"dynamic", ir.AttributeIR{Name: "a", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeDynamic}}, "schema.DynamicAttribute"},
		{"union-discriminated", ir.AttributeIR{Name: "a", Optional: true, Schema: ir.SchemaIR{Union: &ir.UnionType{
			Kind: ir.OneOf,
			Variants: []ir.SchemaIR{
				{Name: "Cat", Attributes: []ir.AttributeIR{{Name: "lives", Schema: ir.SchemaIR{Type: ir.TypeInt}}}},
				{Name: "Dog", Attributes: []ir.AttributeIR{{Name: "barks", Schema: ir.SchemaIR{Type: ir.TypeBool}}}},
			},
			Discriminator: &ir.DiscriminatorIR{PropertyName: "petType", Mapping: map[string]string{"cat": "#/components/schemas/Cat", "dog": "#/components/schemas/Dog"}},
		}}}, "schema.SingleNestedAttribute"},
		{"union-plain", ir.AttributeIR{Name: "a", Optional: true, Schema: ir.SchemaIR{Union: &ir.UnionType{Kind: ir.OneOf, Variants: []ir.SchemaIR{{Type: ir.TypeString}, {Type: ir.TypeInt}}}}}, "schema.DynamicAttribute"},
		{"empty-schema", ir.AttributeIR{Name: "a", Optional: true, Schema: ir.SchemaIR{}}, "<nil>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := render(tc.attr)
			if !strings.Contains(got, tc.want) {
				t.Errorf("frameworkAttributeExpr() = %q, want substring %q", got, tc.want)
			}
		})
	}
}

// TestValidateProviderObjectSchema covers the attribute-error, block-nesting,
// and block-schema-error branches of validateProviderObjectSchema.
func TestValidateProviderObjectSchema(t *testing.T) {
	// Attribute whose schema fails validation propagates the error.
	err := validateProviderObjectSchema(ir.SchemaIR{
		Attributes: []ir.AttributeIR{{Name: "bad", Schema: ir.SchemaIR{Type: ir.TypeNull}}},
	}, "test")
	if err == nil || !strings.Contains(err.Error(), "test attribute bad") {
		t.Errorf("attribute error = %v, want error naming the attribute", err)
	}

	// Supported nesting modes validate the block schema.
	for _, mode := range []ir.BlockNestingMode{ir.NestingSingle, ir.NestingList, ir.NestingSet} {
		err := validateProviderObjectSchema(ir.SchemaIR{
			Blocks: []ir.BlockIR{{Name: "b", NestingMode: mode, Schema: ir.ObjectSchemaIR{}}},
		}, "test")
		if err != nil {
			t.Errorf("block nesting %q: unexpected error %v", mode, err)
		}
	}

	// Unsupported nesting mode is rejected.
	err = validateProviderObjectSchema(ir.SchemaIR{
		Blocks: []ir.BlockIR{{Name: "b", NestingMode: "map"}},
	}, "test")
	if err == nil || !strings.Contains(err.Error(), "unsupported nesting mode") {
		t.Errorf("unsupported nesting = %v, want error", err)
	}

	// A block whose schema fails validation wraps the error with the block name.
	err = validateProviderObjectSchema(ir.SchemaIR{
		Blocks: []ir.BlockIR{{Name: "b", NestingMode: ir.NestingSingle, Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{{Name: "bad", Schema: ir.SchemaIR{Type: ir.TypeNull}}},
		}}},
	}, "test")
	if err == nil || !strings.Contains(err.Error(), "test block \"b\"") {
		t.Errorf("block schema error = %v, want error naming the block", err)
	}
}
