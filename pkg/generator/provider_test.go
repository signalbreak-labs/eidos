package generator

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
// provider config attribute that triggers a renderer panic (a fractional
// exclusiveMinimum on an integer schema panics in validateIntBound) must
// surface as a generation error, not crash the process. Before the recover
// wrapper, FilesForProviderIR could terminate the generator on such IR.
func TestProviderFile_RecoversValidatorPanic(t *testing.T) {
	frac := 0.5
	pir := sampleProviderIR()
	pir.ConfigSchema.Attributes = append(pir.ConfigSchema.Attributes, ir.AttributeIR{
		Name:     "count",
		Optional: true,
		Schema: ir.SchemaIR{
			Type:             ir.TypeInt,
			ExclusiveMinimum: &frac,
		},
	})

	// ProviderFile must not panic; it must return an error mentioning the panic.
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("ProviderFile panicked instead of returning an error: %v", rec)
		}
	}()
	_, err := ProviderFile(pir)
	if err == nil {
		t.Fatal("expected error for fractional integer exclusiveMinimum, got nil")
	}
	if !strings.Contains(err.Error(), "renderer panic") {
		t.Fatalf("expected error to mention renderer panic, got: %v", err)
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
