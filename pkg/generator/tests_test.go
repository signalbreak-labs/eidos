package generator

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// TestProviderTestFile_Render verifies that ProviderTestFile emits the expected
// provider test functions.
func TestProviderTestFile_Render(t *testing.T) {
	pir := sampleProviderIR()

	file := ProviderTestFile(pir)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"package provider",
		"func TestProviderSchemaValidation",
		"func TestProviderMetadata",
		"tfframeworkprovider.SchemaResponse",
		"tfframeworkprovider.MetadataResponse",
		"p.Schema(context.Background()",
		"p.Metadata(context.Background()",
		"ValidateImplementation",
		"resp.TypeName != \"mycloud\"",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated provider_test.go missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestProviderTestFile_SchemaValidation generates a minimal provider module
// including the generated provider_test.go and runs it to confirm the unit
// tests pass.
func TestProviderTestFile_SchemaValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping compile-time test validation in short mode")
	}

	pir := sampleProviderIR()
	tmp := generateProviderTestModule(t, pir)

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

// TestResourceTestFile_Render verifies that ResourceTestFile emits the
// expected resource test functions.
func TestResourceTestFile_Render(t *testing.T) {
	r := sampleResourceIR()

	file := ResourceTestFile(r)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"package provider",
		"func TestPetResourceSchemaValidation",
		"func TestPetResourceMetadata",
		"tfframeworkresource.SchemaResponse",
		"tfframeworkresource.MetadataResponse",
		"r.Schema(context.Background()",
		"r.Metadata(context.Background()",
		"ValidateImplementation",
		"resp.TypeName != \"mycloud_pet\"",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated resource_test.go missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestResourceTestFile_SchemaValidation generates a minimal provider module with
// a managed resource and its generated test file, then runs the tests.
func TestResourceTestFile_SchemaValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping compile-time test validation in short mode")
	}

	p := sampleProviderWithResourceIR()
	tmp := generateResourceTestModule(t, p)

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

// TestResourceTestFiles_Multiple verifies that ResourceTestFiles emits one test
// file per resource with deterministic paths.
func TestResourceTestFiles_Multiple(t *testing.T) {
	resources := []ir.ResourceIR{
		{Name: "pet", TypeName: "mycloud_pet"},
		{Name: "owner", TypeName: "mycloud_owner"},
	}

	files := ResourceTestFiles(resources)
	if len(files) != len(resources) {
		t.Fatalf("ResourceTestFiles() returned %d files, want %d", len(files), len(resources))
	}

	if files[0].Path != "internal/provider/resource_pet_test.go" {
		t.Errorf("file[0].Path = %q, want %q", files[0].Path, "internal/provider/resource_pet_test.go")
	}
	if files[1].Path != "internal/provider/resource_owner_test.go" {
		t.Errorf("file[1].Path = %q, want %q", files[1].Path, "internal/provider/resource_owner_test.go")
	}
}

// TestDataSourceTestFile_Render verifies that DataSourceTestFile emits the
// expected data source test functions.
func TestDataSourceTestFile_Render(t *testing.T) {
	ds := sampleDataSourceIR()

	file := DataSourceTestFile(ds)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"package provider",
		"func TestPetsDataSourceSchemaValidation",
		"func TestPetsDataSourceMetadata",
		"tfframeworkdatasource.SchemaResponse",
		"tfframeworkdatasource.MetadataResponse",
		"d.Schema(context.Background()",
		"d.Metadata(context.Background()",
		"ValidateImplementation",
		"resp.TypeName != \"mycloud_pets\"",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated data_source_test.go missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestDataSourceTestFile_SchemaValidation generates a minimal provider module
// with a data source and its generated test file, then runs the tests.
func TestDataSourceTestFile_SchemaValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping compile-time test validation in short mode")
	}

	providerIR := ir.ProviderIR{
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
			},
		},
		DataSources: []ir.DataSourceIR{sampleDataSourceIR()},
	}

	tmp := generateDataSourceTestModule(t, providerIR, []ir.DataSourceIR{sampleDataSourceIR()})

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

// TestDataSourceTestFiles_Multiple verifies that DataSourceTestFiles emits one
// test file per data source with deterministic paths.
func TestDataSourceTestFiles_Multiple(t *testing.T) {
	dataSources := []ir.DataSourceIR{
		{Name: "pets", TypeName: "mycloud_pets"},
		{Name: "owner", TypeName: "mycloud_owner"},
	}

	files := DataSourceTestFiles(dataSources)
	if len(files) != len(dataSources) {
		t.Fatalf("DataSourceTestFiles() returned %d files, want %d", len(files), len(dataSources))
	}

	if files[0].Path != "internal/provider/data_source_pets_test.go" {
		t.Errorf("file[0].Path = %q, want %q", files[0].Path, "internal/provider/data_source_pets_test.go")
	}
	if files[1].Path != "internal/provider/data_source_owner_test.go" {
		t.Errorf("file[1].Path = %q, want %q", files[1].Path, "internal/provider/data_source_owner_test.go")
	}
}

// TestMapperTestFile_Render verifies that MapperTestFile emits type and
// round-trip test functions for the generated value mappers.
func TestMapperTestFile_Render(t *testing.T) {
	r := sampleModelResourceIR()
	providerImport := "example.com/roundtrip/internal/provider"

	file := MapperTestFile([]ir.ResourceIR{r}, providerImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"package protocol",
		"provider \"example.com/roundtrip/internal/provider\"",
		"func TestPetModelType",
		"func TestPetModelRoundTrip",
		"reflect.DeepEqual",
		"PetModelToValue",
		"PetModelFromValue",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated value_mappers_test.go missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestDataSourceMapperTestFile_Render verifies that DataSourceMapperTestFile
// emits type and round-trip test functions for data source value mapper models.
func TestDataSourceMapperTestFile_Render(t *testing.T) {
	ds := sampleDataSourceIR()
	providerImport := "example.com/roundtrip/internal/provider"

	file := DataSourceMapperTestFile([]ir.DataSourceIR{ds}, providerImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"package protocol",
		"provider \"example.com/roundtrip/internal/provider\"",
		"func TestPetsDataSourceModelType",
		"func TestPetsDataSourceModelRoundTrip",
		"reflect.DeepEqual",
		"PetsDataSourceModelToValue",
		"PetsDataSourceModelFromValue",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated data_source_value_mappers_test.go missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestMapperTestFile_RoundTrip compiles the generated model, mapper, and mapper
// test files in a temporary module and runs them.
func TestMapperTestFile_RoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping compile-time mapper test validation in short mode")
	}

	r := sampleModelResourceIR()
	tmp := generateMapperTestModule(t, r)

	ctx, cancel := contextWithTimeout(t, 5*time.Minute)
	defer cancel()

	tidyCmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidyCmd.Dir = tmp
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\n%s", err, out)
	}

	cmd := exec.CommandContext(ctx, "go", "test", "./...", "-v")
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}

	if !strings.Contains(string(out), "PASS") {
		t.Fatalf("expected test to pass, got:\n%s", out)
	}
}

// generateProviderTestModule writes the generated go.mod, provider.go, and
// provider_test.go files into a temporary module directory and returns the
// module root. The provider IR is stripped of resources and data sources so the
// module can compile without generating the corresponding implementation files.
func generateProviderTestModule(t *testing.T, pir ir.ProviderIR) string {
	t.Helper()
	tmp := t.TempDir()

	cfg := BuildConfig{
		ProviderName: pir.Name,
		Namespace:    pir.Name,
	}

	bare := pir
	bare.Resources = nil
	bare.DataSources = nil
	bare.Functions = nil
	bare.EphemeralResources = nil
	bare.ListResources = nil
	bare.Actions = nil

	h := Harness{OutputDir: tmp}
	pf, err := ProviderFile(bare)
	if err != nil {
		t.Fatalf("ProviderFile() error = %v", err)
	}
	files := append(BuildFiles(cfg), pf)
	files = append(files, ProviderTestFile(bare))
	if err := h.Generate(files); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	return tmp
}

// generateResourceTestModule writes the generated go.mod, provider.go,
// resource file, and resource test file into a temporary module directory and
// returns the module root.
func generateResourceTestModule(t *testing.T, p ir.ProviderIR) string {
	t.Helper()
	tmp := t.TempDir()

	cfg := BuildConfig{
		ProviderName: p.Name,
		Namespace:    p.Name,
	}

	h := Harness{OutputDir: tmp}
	files := resourceModuleFiles(t, p, cfg)
	files = append(files, ResourceTestFiles(p.Resources)...)
	if err := h.Generate(files); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	return tmp
}

// generateDataSourceTestModule writes the generated go.mod, provider.go, data
// source files, and data source test files into a temporary module directory and
// returns the module root.
func generateDataSourceTestModule(t *testing.T, providerIR ir.ProviderIR, dataSources []ir.DataSourceIR) string {
	t.Helper()
	tmp := t.TempDir()

	cfg := BuildConfig{
		ProviderName: providerIR.Name,
		Namespace:    providerIR.Name,
	}

	h := Harness{OutputDir: tmp}
	pf, err := ProviderFile(providerIR)
	if err != nil {
		t.Fatalf("ProviderFile() error = %v", err)
	}
	files := append(BuildFiles(cfg), pf)
	for _, ds := range dataSources {
		files = append(files, DataSourceFile(ds, cfg.modulePath()+"/internal/client"), DataSourceTestFile(ds))
	}
	if err := h.Generate(files); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	return tmp
}

// generateMapperTestModule creates a temporary Go module containing the
// generated model file, value mapper file, and value mapper test file for a
// single resource.
func generateMapperTestModule(t *testing.T, r ir.ResourceIR) string {
	t.Helper()
	tmp := t.TempDir()

	cfg := BuildConfig{
		ProviderName: "roundtrip",
		Namespace:    "test",
		ModulePath:   "example.com/roundtrip",
	}

	h := Harness{OutputDir: tmp}
	providerImport := cfg.ModulePath + "/internal/provider"
	files := []File{
		GoMod(cfg),
		ModelFile(r),
		ValueMappersFile([]ir.ResourceIR{r}, providerImport),
		MapperTestFile([]ir.ResourceIR{r}, providerImport),
	}
	if err := h.Generate(files); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	return tmp
}

// TestTestFiles verifies that TestFiles aggregates provider, resource, data
// source, and mapper test files in a deterministic order.
func TestTestFiles(t *testing.T) {
	pir := sampleProviderIR()
	// A wired resource (full CRUD mapping) so the acceptance test file is
	// emitted; scaffolded resources are skipped by ResourceAcceptanceTestFiles.
	pir.Resources = []ir.ResourceIR{sampleResourceIR()}
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}

	files := TestFiles(pir, cfg)

	wantPaths := []string{
		"internal/provider/provider_test.go",
		"internal/provider/resource_pet_test.go",
		"internal/provider/resource_pet_acceptance_test.go",
		"internal/provider/data_source_pets_test.go",
		"internal/protocol/value_mappers_test.go",
	}
	if len(files) != len(wantPaths) {
		t.Fatalf("TestFiles() returned %d files, want %d", len(files), len(wantPaths))
	}
	for i, want := range wantPaths {
		if files[i].Path != want {
			t.Errorf("file[%d].Path = %q, want %q", i, files[i].Path, want)
		}
	}
}

// TestTestFiles_NoResources verifies that TestFiles omits the mapper test file
// when the provider defines no resources.
func TestTestFiles_NoResources(t *testing.T) {
	pir := ir.ProviderIR{
		Name:     "mycloud",
		TypeName: "mycloud",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "api_key", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
		DataSources: []ir.DataSourceIR{sampleDataSourceIR()},
	}
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}

	files := TestFiles(pir, cfg)

	for _, f := range files {
		if f.Path == "internal/protocol/value_mappers_test.go" {
			t.Fatalf("TestFiles() emitted value_mappers_test.go for a provider with no resources")
		}
	}

	wantPaths := []string{
		"internal/provider/provider_test.go",
		"internal/provider/data_source_pets_test.go",
	}
	if len(files) != len(wantPaths) {
		t.Fatalf("TestFiles() returned %d files, want %d", len(files), len(wantPaths))
	}
	for i, want := range wantPaths {
		if files[i].Path != want {
			t.Errorf("file[%d].Path = %q, want %q", i, files[i].Path, want)
		}
	}
}

// compile-time interface checks.
var _ = ir.ProviderIR{}
var _ = ir.ResourceIR{}
var _ = ir.DataSourceIR{}
var _ = os.FileMode(0)
var _ = filepath.Separator
var _ = time.Second
