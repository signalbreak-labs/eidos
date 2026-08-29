package generator

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// TestTerraformTestFile_Render verifies that TerraformTestFile emits a valid
// .tftest.hcl orchestration file for a resource.
func TestTerraformTestFile_Render(t *testing.T) {
	pir := sampleProviderWithResourceIR()
	r := pir.Resources[0]
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}

	file := TerraformTestFile(pir, r, cfg)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		`run "create_pet" {`,
		"command = apply",
		`module {`,
		`source = "./tests/modules/pet"`,
		"variables {",
		`name = "example"`,
		"assert {",
		`condition     = output.id == "generated"`,
		`error_message = "unexpected id"`,
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated .tftest.hcl missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestTerraformTestFile_Path verifies the output path uses snake_case naming.
func TestTerraformTestFile_Path(t *testing.T) {
	pir := sampleProviderWithResourceIR()
	r := ir.ResourceIR{Name: "my_pet", TypeName: "mycloud_pet"}
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}

	file := TerraformTestFile(pir, r, cfg)
	if file.Path != "tests/my_pet.tftest.hcl" {
		t.Errorf("file.Path = %q, want %q", file.Path, "tests/my_pet.tftest.hcl")
	}
}

// TestTerraformTestModuleFile_Render verifies that TerraformTestModuleFile
// emits a valid supporting module main.tf file.
func TestTerraformTestModuleFile_Render(t *testing.T) {
	pir := sampleProviderWithResourceIR()
	r := pir.Resources[0]
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}

	file := TerraformTestModuleFile(pir, r, cfg)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"terraform {",
		"required_providers {",
		"mycloud = {",
		`source = "mycloud/mycloud"`,
		`provider "mycloud" {`,
		`api_key = "example"`,
		`variable "name" {`,
		"type = string",
		`resource "mycloud_pet" "example" {`,
		"name = var.name",
		`output "id" {`,
		"value = mycloud_pet.example.id",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated test module missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestTerraformTestModuleFile_NestedRequiredPrimitivesNotVarReferenced locks in
// the M-55 fix: required primitives nested inside object attributes, blocks,
// or collection elements are rendered with inline placeholder values, never as
// var.<name> references. Only direct top-level required primitives get a variable
// declaration, so a nested var.<name> would be an undeclared-variable error in
// Terraform.
func TestTerraformTestModuleFile_NestedRequiredPrimitivesNotVarReferenced(t *testing.T) {
	pir := ir.ProviderIR{
		Name:     "mycloud",
		TypeName: "mycloud",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "api_key", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
		Resources: []ir.ResourceIR{{
			Name:     "widget",
			TypeName: "mycloud_widget",
			Schema: ir.ObjectSchemaIR{
				Attributes: []ir.AttributeIR{
					// Top-level required primitive: declared as a variable.
					{Name: "name", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
					// Nested required primitive inside an object attribute: must
					// use an inline value, not var.inner.
					{
						Name:     "config",
						Required: true,
						Schema: ir.SchemaIR{Collection: nil, Attributes: []ir.AttributeIR{
							{Name: "inner", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						}},
					},
				},
				Blocks: []ir.BlockIR{{
					Name: "settings",
					Schema: ir.ObjectSchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "mode", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				}},
			},
		}},
	}
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}

	file := TerraformTestModuleFile(pir, pir.Resources[0], cfg)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	// Top-level primitive is wired to a variable.
	if !strings.Contains(got, "name = var.name") {
		t.Errorf("expected top-level name = var.name, missing from:\n%s", got)
	}
	if !strings.Contains(got, `variable "name" {`) {
		t.Errorf("expected a variable declaration for the top-level name, missing from:\n%s", got)
	}
	// Nested primitives must NOT be var.-referenced (M-55).
	for _, bad := range []string{"inner = var.inner", "mode = var.mode"} {
		if strings.Contains(got, bad) {
			t.Errorf("nested primitive rendered as %q (undeclared variable); should use an inline value. content:\n%s", bad, got)
		}
	}
	// Only one variable block (for the top-level name) should be declared.
	if n := strings.Count(got, `variable "`); n != 1 {
		t.Errorf("expected exactly 1 variable declaration, got %d. content:\n%s", n, got)
	}
}

// TestTerraformTestModuleFile_Path verifies the module output path.
func TestTerraformTestModuleFile_Path(t *testing.T) {
	pir := sampleProviderWithResourceIR()
	r := ir.ResourceIR{Name: "my_pet", TypeName: "mycloud_pet"}
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}

	file := TerraformTestModuleFile(pir, r, cfg)
	want := filepath.Join("tests", "modules", "my_pet", "main.tf")
	if file.Path != want {
		t.Errorf("file.Path = %q, want %q", file.Path, want)
	}
}

// TestTerraformTestFiles_Multiple verifies that TerraformTestFiles emits a
// test file and a module file per resource with deterministic paths.
func TestTerraformTestFiles_Multiple(t *testing.T) {
	pir := ir.ProviderIR{
		Name:     "mycloud",
		TypeName: "mycloud",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "api_key", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
		Resources: []ir.ResourceIR{
			{Name: "pet", TypeName: "mycloud_pet"},
			{Name: "owner", TypeName: "mycloud_owner"},
		},
	}
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}

	files := TerraformTestFiles(pir, cfg)
	if len(files) != len(pir.Resources)*2 {
		t.Fatalf("TerraformTestFiles() returned %d files, want %d", len(files), len(pir.Resources)*2)
	}

	wantPaths := []string{
		"tests/pet.tftest.hcl",
		filepath.Join("tests", "modules", "pet", "main.tf"),
		"tests/owner.tftest.hcl",
		filepath.Join("tests", "modules", "owner", "main.tf"),
	}
	for i, want := range wantPaths {
		if files[i].Path != want {
			t.Errorf("file[%d].Path = %q, want %q", i, files[i].Path, want)
		}
	}
}

// TestSchemaExampleLiteral_UnconstrainedMatchesPrimitiveExampleValue asserts
// that schemaExampleLiteral keeps the unconstrained placeholder
// (primitiveExampleValue) for every primitive type. Generated test modules,
// acceptance configs, and generated examples all render placeholders through
// schemaExampleLiteral, so if this invariant drifts, either those configs
// change for specs without constraints or the three call sites disagree.
func TestSchemaExampleLiteral_UnconstrainedMatchesPrimitiveExampleValue(t *testing.T) {
	cases := []ir.PrimitiveType{
		ir.TypeString,
		ir.TypeInt,
		ir.TypeFloat,
		ir.TypeBool,
		ir.TypeDynamic,
		ir.TypeNull,
		"unknown",
	}
	for _, tc := range cases {
		t.Run(string(tc), func(t *testing.T) {
			got := schemaExampleLiteral(ir.SchemaIR{Type: tc})
			if want := primitiveExampleValue(tc); got != want {
				t.Errorf("schemaExampleLiteral(%q) = %q, want %q", tc, got, want)
			}
		})
	}
}

// TestTerraformTestExpectedIDValue_MatchesCreateIDPlaceholder asserts that the
// .tftest.hcl expected id value matches the placeholder produced by the generated
// provider's stub Create implementation.
func TestTerraformTestExpectedIDValue_MatchesCreateIDPlaceholder(t *testing.T) {
	cases := []struct {
		typ  ir.PrimitiveType
		want string
	}{
		{ir.TypeString, `"generated"`},
		{ir.TypeInt, "1"},
		{ir.TypeFloat, "1.0"},
		{ir.TypeBool, "true"},
		{ir.TypeDynamic, "null"},
		{"unknown", `"generated"`},
	}
	for _, tc := range cases {
		t.Run(string(tc.typ), func(t *testing.T) {
			if got := terraformTestExpectedIDValue(tc.typ); got != tc.want {
				t.Errorf("terraformTestExpectedIDValue(%q) = %q, want %q", tc.typ, got, tc.want)
			}
			if got := terraformTestExpectedIDValue(tc.typ); got != createIDPlaceholder(tc.typ) {
				t.Errorf("terraformTestExpectedIDValue(%q) = %q, createIDPlaceholder(%q) = %q; want equal", tc.typ, got, tc.typ, createIDPlaceholder(tc.typ))
			}
		})
	}
}

// TestWriteTerraformCLIConfig verifies the generated .terraformrc content and
// ensures the provider source address and binary path are properly quoted.
func TestWriteTerraformCLIConfig(t *testing.T) {
	tmp := t.TempDir()
	cfg := BuildConfig{ProviderName: "mycloud", Namespace: "mycloud"}
	if err := writeTerraformCLIConfig(tmp, cfg); err != nil {
		t.Fatalf("writeTerraformCLIConfig() error = %v", err)
	}

	content, err := os.ReadFile(filepath.Join(tmp, ".terraformrc"))
	if err != nil {
		t.Fatalf("read .terraformrc: %v", err)
	}
	got := string(content)

	wantSubstrings := []string{
		"provider_installation {",
		"dev_overrides {",
		`"mycloud/mycloud" = "` + filepath.Join(tmp, "bin") + `"`,
		"direct {}",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated .terraformrc missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestTerraformTestFiles_Integration generates a minimal provider module with a
// managed resource and its Terraform test files, then runs terraform test
// against the generated provider. The fixture resource has a complete CRUD
// mapping, so its Create body is wired to the generated API client; with no
// servers in the fixture IR the client's base URL is empty and the apply fails
// with a clear runtime HTTP error. The test verifies that the generated HCL is
// valid and that the failure surfaces the wired operation's diagnostic.
func TestTerraformTestFiles_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping terraform test integration in short mode")
	}
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform binary not found in PATH")
	}
	requireTerraformVersion(t, 1, 6)

	pir := sampleProviderWithResourceIR()
	tmp := generateTerraformTestModule(t, pir)

	ctx, cancel := contextWithTimeout(t, 10*time.Minute)
	defer cancel()

	tidyCmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidyCmd.Dir = tmp
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\n%s", err, out)
	}

	providerName := pir.Name
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0o750); err != nil {
		t.Fatalf("create provider bin dir: %v", err)
	}
	binaryPath := filepath.Join(binDir, "terraform-provider-"+providerName)
	buildCmd := exec.CommandContext(ctx, "go", "build", "-o", binaryPath, ".")
	buildCmd.Dir = tmp
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build provider failed: %v\n%s", err, out)
	}

	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}
	if err := writeTerraformCLIConfig(tmp, cfg); err != nil {
		t.Fatalf("write .terraformrc: %v", err)
	}

	// Initialize the test modules. terraform init may emit a provider-query
	// error because the generated provider is not published, but the module
	// cache is still written and the subsequent terraform test uses the
	// dev_overrides binary. We capture the output and only ignore errors that
	// clearly come from the provider registry lookup; other init failures are
	// surfaced so the test does not proceed with a broken setup.
	initCmd := exec.CommandContext(ctx, "terraform", "init", "-test-directory=tests")
	initCmd.Dir = tmp
	initCmd.Env = append(os.Environ(), "TF_CLI_CONFIG_FILE="+filepath.Join(tmp, ".terraformrc"))
	initOut, initErr := initCmd.CombinedOutput()
	if initErr != nil && !isExpectedTerraformInitError(string(initOut)) {
		t.Fatalf("terraform init failed: %v\n%s", initErr, initOut)
	}

	testCmd := exec.CommandContext(ctx, "terraform", "test", "-test-directory=tests")
	testCmd.Dir = tmp
	testCmd.Env = append(os.Environ(), "TF_CLI_CONFIG_FILE="+filepath.Join(tmp, ".terraformrc"))
	out, err := testCmd.CombinedOutput()
	output := string(out)
	if err == nil {
		t.Fatalf("expected terraform test to fail because the fixture provider has no reachable API endpoint; got success:\n%s", output)
	}
	if !strings.Contains(output, "Error creating mycloud_pet") {
		t.Fatalf("expected terraform test output to contain the wired create diagnostic; got:\n%s", output)
	}
}

// generateTerraformTestModule writes the generated go.mod, provider.go,
// resource file, and Terraform native test files into a temporary module
// directory and returns the module root.
func generateTerraformTestModule(t *testing.T, pir ir.ProviderIR) string {
	t.Helper()
	tmp := t.TempDir()

	cfg := BuildConfig{
		ProviderName: pir.Name,
		Namespace:    pir.Name,
	}

	h := Harness{OutputDir: tmp}
	files := resourceModuleFiles(t, pir, cfg)
	files = append(files, TerraformTestFiles(pir, cfg)...)
	if err := h.Generate(files); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	return tmp
}

// writeTerraformCLIConfig writes a .terraformrc that uses provider development
// overrides to point Terraform at the freshly built provider binary. The source
// address and binary path are written with %q so special characters are escaped
// automatically.
func terraformTestProviderSource(cfg BuildConfig) string {
	return fmt.Sprintf("%s/%s", cfg.namespace(), cfg.providerName())
}

func writeTerraformCLIConfig(moduleRoot string, cfg BuildConfig) error {
	path := filepath.Join(moduleRoot, ".terraformrc")
	content := fmt.Sprintf(`provider_installation {
  dev_overrides {
    %q = %q
  }
  direct {}
}
`, terraformTestProviderSource(cfg), filepath.Join(moduleRoot, "bin"))
	return os.WriteFile(path, []byte(content), 0o600)
}

// requireTerraformVersion skips the test when the terraform binary is older than
// the given major.minor version. Terraform's native test command requires 1.6+
// for the -test-directory flag.
func requireTerraformVersion(t *testing.T, major, minor int) {
	t.Helper()
	out, err := exec.CommandContext(context.Background(), "terraform", "version").CombinedOutput()
	if err != nil {
		t.Skipf("terraform version failed: %v\n%s", err, out)
	}
	m := regexp.MustCompile(`Terraform v(\d+)\.(\d+)\.(\d+)`).FindStringSubmatch(string(out))
	if m == nil {
		t.Skipf("could not parse terraform version output:\n%s", out)
	}
	gotMajor, errA := strconv.Atoi(m[1])
	if errA != nil {
		t.Skipf("could not parse terraform major version %q: %v", m[1], errA)
	}
	gotMinor, errB := strconv.Atoi(m[2])
	if errB != nil {
		t.Skipf("could not parse terraform minor version %q: %v", m[2], errB)
	}
	if gotMajor < major || (gotMajor == major && gotMinor < minor) {
		t.Skipf("terraform version %s.%s.%s is older than required %d.%d", m[1], m[2], m[3], major, minor)
	}
}

// isExpectedTerraformInitError reports whether a failed terraform init can be
// safely ignored for dev_overrides-based tests. Init commonly fails when the
// unpublished provider cannot be found in the registry, but the module cache is
// still written and terraform test can proceed. Other failures (malformed HCL,
// missing required_providers, network issues not related to the registry, etc.)
// are not ignored.
func isExpectedTerraformInitError(output string) bool {
	lowered := strings.ToLower(output)
	expected := []string{
		"provider registry",
		"registry.terraform.io",
		"no available releases",
		"dev_overrides",
	}
	for _, substr := range expected {
		if strings.Contains(lowered, substr) {
			return true
		}
	}
	return false
}
