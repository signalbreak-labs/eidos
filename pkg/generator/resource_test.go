package generator

import (
	"bytes"
	"go/ast"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/naming"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// TestResourceFile_Render verifies that ResourceFile emits the expected
// resource struct, model, Metadata, Schema, Create, Read, Update, Delete,
// and ImportState methods.
func TestResourceFile_Render(t *testing.T) {
	r := sampleResourceIR()

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"package provider",
		"type PetResource struct",
		"type PetResourceModel struct",
		"tfsdk:\"id\"",
		"tfsdk:\"name\"",
		"tfsdk:\"tag\"",
		"tfsdk:\"age\"",
		"tfsdk:\"tags\"",
		"tfsdk:\"owner\"",
		"func (r *PetResource) Metadata",
		"func (r *PetResource) Schema",
		"func (r *PetResource) Create",
		"func (r *PetResource) Read",
		"func (r *PetResource) Update",
		"func (r *PetResource) Delete",
		"func (r *PetResource) ImportState",
		"resp.TypeName = \"mycloud_pet\"",
		"schema.StringAttribute",
		"schema.Int64Attribute",
		"schema.ListAttribute",
		"schema.SingleNestedAttribute",
		"schema.Schema",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated resource file missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestResourceFile_NoIdentitySchemaByDefault confirms a resource with no
// paired list resource (no IdentitySchema) emits neither the IdentitySchema
// method nor the ResourceWithIdentity assertion, so the common case is
// unaffected and the identityschema import is not pulled in.
func TestResourceFile_NoIdentitySchemaByDefault(t *testing.T) {
	r := sampleResourceIR()
	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()
	for _, unwanted := range []string{
		"func (r *PetResource) IdentitySchema",
		"resource.ResourceWithIdentity",
		`"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"`,
	} {
		if strings.Contains(got, unwanted) {
			t.Errorf("resource without identity should not emit %q\ncontent:\n%s", unwanted, got)
		}
	}
}

// TestResourceFile_IdentitySchema verifies that a resource carrying an
// IdentitySchema (paired with a list resource) emits the IdentitySchema method
// returning an identityschema.Schema, the ResourceWithIdentity assertion, and
// the identityschema import. Each primitive identity attribute maps to the
// matching identityschema attribute type and is RequiredForImport.
func TestResourceFile_IdentitySchema(t *testing.T) {
	r := sampleResourceIR()
	identity := ir.ObjectSchemaIR{
		Attributes: []ir.AttributeIR{
			{Name: "ship_symbol", WireName: "symbol", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			{Name: "count", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeInt}},
			{Name: "ratio", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeFloat}},
			{Name: "active", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeBool}},
		},
	}
	r.IdentitySchema = &identity

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"func (r *PetResource) IdentitySchema",
		"resource.ResourceWithIdentity",
		`"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"`,
		`identityschema.Schema{Attributes: map[string]identityschema.Attribute{"ship_symbol": identityschema.StringAttribute{RequiredForImport: true}, "count": identityschema.Int64Attribute{RequiredForImport: true}, "ratio": identityschema.Float64Attribute{RequiredForImport: true}, "active": identityschema.BoolAttribute{RequiredForImport: true}}}`,
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated resource file missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestResourceFile_IdentitySchema_Compiles generates a full provider module
// whose managed resource carries an identity schema and compiles it, proving
// the IdentitySchema method, the ResourceWithIdentity assertion, and the
// identityschema import are syntactically valid against the plugin framework.
func TestResourceFile_IdentitySchema_Compiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}
	p := sampleProviderWithResourceIR()
	identity := ir.ObjectSchemaIR{
		Attributes: []ir.AttributeIR{
			{Name: "ship_symbol", WireName: "symbol", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			{Name: "count", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeInt}},
		},
	}
	p.Resources[0].IdentitySchema = &identity

	tmp := generateResourceModule(t, p)

	ctx, cancel := contextWithTimeout(t, 5*time.Minute)
	defer cancel()

	tidyCmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidyCmd.Dir = tmp
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\n%s", err, out)
	}
	buildCmd := exec.CommandContext(ctx, "go", "build", "./...")
	buildCmd.Dir = tmp
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./... failed for identity-enabled resource: %v\n%s", err, out)
	}
}

// TestResourceFile_SchemaValidation generates a minimal provider module with
// one managed resource and runs the Terraform plugin-framework schema
// validation to confirm the generated resource.go compiles and its schema is
// valid.
func TestResourceFile_SchemaValidation(t *testing.T) {
	p := sampleProviderWithResourceIR()

	tmp := generateResourceModule(t, p)
	writeResourceSchemaValidationTest(t, tmp)

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

// TestResourceFiles_Multiple verifies that ResourceFiles emits one file per
// resource with deterministic, unique paths.
func TestResourceFiles_Multiple(t *testing.T) {
	resources := []ir.ResourceIR{
		{Name: "pet", TypeName: "mycloud_pet"},
		{Name: "owner", TypeName: "mycloud_owner"},
	}

	files := ResourceFiles(resources, "github.com/mycloud/terraform-provider-mycloud/internal/client")
	if len(files) != len(resources) {
		t.Fatalf("ResourceFiles() returned %d files, want %d", len(files), len(resources))
	}

	if files[0].Path != "internal/provider/resource_pet.go" {
		t.Errorf("file[0].Path = %q, want %q", files[0].Path, "internal/provider/resource_pet.go")
	}
	if files[1].Path != "internal/provider/resource_owner.go" {
		t.Errorf("file[1].Path = %q, want %q", files[1].Path, "internal/provider/resource_owner.go")
	}

	tmp := t.TempDir()
	h := Harness{OutputDir: tmp}
	if err := h.Generate(files); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	gotPaths := collectPaths(t, tmp)
	wantPaths := []string{"internal/provider/resource_owner.go", "internal/provider/resource_pet.go"}
	if diff := sliceDiff(wantPaths, gotPaths); diff != "" {
		t.Errorf("emitted paths mismatch:\n%s", diff)
	}
}

// sampleResourceIR returns a ResourceIR covering primitives, collections, and
// nested object attributes.
func sampleResourceIR() ir.ResourceIR {
	return ir.ResourceIR{
		Name:        "pet",
		TypeName:    "mycloud_pet",
		Description: "A pet resource.",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "id",
					Computed: true,
					Schema:   ir.SchemaIR{Type: ir.TypeString},
				},
				{
					Name:     "name",
					Required: true,
					Schema:   ir.SchemaIR{Type: ir.TypeString},
				},
				{
					Name:     "tag",
					Optional: true,
					Schema:   ir.SchemaIR{Type: ir.TypeString},
				},
				{
					Name:     "age",
					Optional: true,
					Schema:   ir.SchemaIR{Type: ir.TypeInt},
				},
				{
					Name:     "tags",
					Optional: true,
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind:        ir.List,
							ElementType: ir.SchemaIR{Type: ir.TypeString},
						},
					},
				},
				{
					Name:     "owner",
					Optional: true,
					Schema: ir.SchemaIR{
						Attributes: []ir.AttributeIR{
							{
								Name:     "email",
								Required: true,
								Schema:   ir.SchemaIR{Type: ir.TypeString},
							},
						},
					},
				},
			},
		},
		CRUDMapping: ir.CRUDMappingIR{
			Create: ir.OperationMappingIR{Method: "POST", PathTemplate: "/pets"},
			Read:   ir.OperationMappingIR{Method: "GET", PathTemplate: "/pets/{id}"},
			Update: &ir.OperationMappingIR{Method: "PUT", PathTemplate: "/pets/{id}"},
			Delete: ir.OperationMappingIR{Method: "DELETE", PathTemplate: "/pets/{id}"},
		},
		Importable: true,
	}
}

// sampleProviderWithResourceIR returns a ProviderIR that registers the sample
// resource so it can be compiled and validated.
func sampleProviderWithResourceIR() ir.ProviderIR {
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
			},
		},
		Resources: []ir.ResourceIR{sampleResourceIR()},
	}
}

// testClientImport is the generated client package import path used by tests
// that render resource files directly. It only needs to be a plausible import
// path; the rendered output is not compiled by these tests.
const testClientImport = "github.com/mycloud/terraform-provider-mycloud/internal/client"

// resourceModuleFiles assembles the generated files for a compilable module
// containing the provider and its managed resources. It includes the generated
// client package and — when at least one resource's CRUD mapping is complete
// enough to wire — the JSON conversion helpers those wired bodies call.
func resourceModuleFiles(t *testing.T, p ir.ProviderIR, cfg BuildConfig) []File {
	t.Helper()
	clientImport := cfg.modulePath() + "/internal/client"
	pf, err := ProviderFileWithClient(p, clientImport)
	if err != nil {
		t.Fatalf("ProviderFileWithClient() error = %v", err)
	}
	files := append(BuildFiles(cfg), pf)
	files = append(files, ResourceFiles(p.Resources, clientImport)...)
	files = append(files, ClientFiles(p)...)
	if AnyResourceWired(p.Resources) {
		files = append(files, JSONConvertFile(&p))
	}
	return files
}

// generateResourceModule writes the generated go.mod, provider.go, and
// resource_<name>.go files into a temporary module directory and returns the
// module root.
func generateResourceModule(t *testing.T, p ir.ProviderIR) string {
	t.Helper()
	tmp := t.TempDir()

	cfg := BuildConfig{
		ProviderName: p.Name,
		Namespace:    p.Name,
	}

	h := Harness{OutputDir: tmp}
	if err := h.Generate(resourceModuleFiles(t, p, cfg)); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	return tmp
}

// writeResourceSchemaValidationTest writes a small test file that imports the
// generated provider, instantiates its managed resources, and validates their
// schema implementations.
func writeResourceSchemaValidationTest(t *testing.T, moduleRoot string) {
	t.Helper()
	testPath := filepath.Join(moduleRoot, "internal", "provider", "resource_schema_validate_test.go")
	if err := os.MkdirAll(filepath.Dir(testPath), 0o750); err != nil {
		t.Fatalf("create provider test dir: %v", err)
	}

	content := `package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
)

func TestResourceSchemaValidation(t *testing.T) {
	p := New()
	resources := p.Resources(context.Background())
	for _, rf := range resources {
		r := rf()
		var mdResp resource.MetadataResponse
		r.Metadata(context.Background(), resource.MetadataRequest{}, &mdResp)

		var schemaResp resource.SchemaResponse
		r.Schema(context.Background(), resource.SchemaRequest{}, &schemaResp)

		diags := schemaResp.Schema.ValidateImplementation(context.Background())
		if diags.HasError() {
			t.Fatalf("schema validation failed for %s: %s", mdResp.TypeName, diags)
		}
	}
}
`
	if err := os.WriteFile(testPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write resource schema validation test: %v", err)
	}
}

// TestResourceTypeName verifies resource type name generation.
func TestResourceTypeName(t *testing.T) {
	cases := []struct {
		name     string
		r        ir.ResourceIR
		wantName string
	}{
		{name: "prefers type name", r: ir.ResourceIR{Name: "pet", TypeName: "mycloud_pet"}, wantName: "mycloud_pet"},
		{name: "falls back to name", r: ir.ResourceIR{Name: "pet"}, wantName: "pet"},
		{name: "trims whitespace fallback", r: ir.ResourceIR{Name: "  pet  "}, wantName: "pet"},
		{name: "trims whitespace type name", r: ir.ResourceIR{Name: "pet", TypeName: "  mycloud_pet  "}, wantName: "mycloud_pet"},
		{name: "snake cases fallback", r: ir.ResourceIR{Name: "My Pet"}, wantName: "my_pet"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resourceTypeName(tc.r); got != tc.wantName {
				t.Errorf("resourceTypeName() = %q, want %q", got, tc.wantName)
			}
		})
	}
}

// TestSnakeCase verifies identifier-to-snake_case conversion.
func TestSnakeCase(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"pet", "pet"},
		{"Pet", "pet"},
		{"MyCloud", "mycloud"},
		{"myCloud", "mycloud"},
		{"pet_store", "pet_store"},
		{"pet store", "pet_store"},
		{"pet-store", "pet_store"},
		{"", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := naming.SnakeCase(tc.in); got != tc.want {
				t.Errorf("naming.SnakeCase(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestResourceStructName verifies generated resource struct naming. The empty
// name is hostile IR: GoTypeName sanitizes it to "X" so the emitted identifier
// stays a valid Go identifier rather than the bare "Resource" suffix (M-10).
func TestResourceStructName(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"pet", "PetResource"},
		{"my_cloud", "MyCloudResource"},
		{"", "XResource"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := ir.ResourceIR{Name: tc.name}
			if got := resourceStructName(r); got != tc.want {
				t.Errorf("resourceStructName(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// TestResourceModelName verifies generated resource model naming. The empty
// name is hostile IR: GoTypeName sanitizes it to "X" so the emitted identifier
// stays a valid Go identifier rather than the bare "ResourceModel" suffix
// (M-10).
func TestResourceModelName(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"pet", "PetResourceModel"},
		{"my_cloud", "MyCloudResourceModel"},
		{"", "XResourceModel"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := ir.ResourceIR{Name: tc.name}
			if got := resourceModelName(r); got != tc.want {
				t.Errorf("resourceModelName(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// TestResourceFile_NonImportable verifies that resources marked Importable:false do
// not assert ResourceWithImportState or generate an ImportState method.
func TestResourceFile_NonImportable(t *testing.T) {
	r := sampleResourceIR()
	r.Importable = false
	got := renderResourceString(t, r)

	if strings.Contains(got, "ResourceWithImportState") {
		t.Errorf("non-importable resource should not assert ResourceWithImportState")
	}
	if strings.Contains(got, "func (r *PetResource) ImportState") {
		t.Errorf("non-importable resource should not generate ImportState")
	}
}

// TestResourceFile_IDAttribute verifies that ImportState uses the configured
// IDAttribute when no explicit import format is provided.
func TestResourceFile_IDAttribute(t *testing.T) {
	r := sampleResourceIR()
	r.IDAttribute = "resource_id"
	r.ImportIDFormat = ""
	got := renderResourceString(t, r)

	if !strings.Contains(got, `path.Root("resource_id")`) {
		t.Errorf("generated ImportState missing custom id attribute path\n%s", got)
	}
}

// TestResourceFile_MapAttribute verifies MapAttribute generation for primitive
// element types.
func TestResourceFile_MapAttribute(t *testing.T) {
	r := sampleResourceIR()
	r.Schema.Attributes = []ir.AttributeIR{
		{
			Name:     "metadata",
			Optional: true,
			Schema: ir.SchemaIR{
				Collection: &ir.CollectionType{
					Kind:        ir.Map,
					ElementType: ir.SchemaIR{Type: ir.TypeString},
				},
			},
		},
	}
	got := renderResourceString(t, r)

	wantSubstrings := []string{
		"schema.MapAttribute",
		"types.StringType",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated resource file missing %q\n%s", want, got)
		}
	}
}

// TestResourceFile_SetAttribute verifies SetAttribute generation for primitive
// element types.
func TestResourceFile_SetAttribute(t *testing.T) {
	r := sampleResourceIR()
	r.Schema.Attributes = []ir.AttributeIR{
		{
			Name:     "tags",
			Optional: true,
			Schema: ir.SchemaIR{
				Collection: &ir.CollectionType{
					Kind:        ir.Set,
					ElementType: ir.SchemaIR{Type: ir.TypeString},
				},
			},
		},
	}
	got := renderResourceString(t, r)

	wantSubstrings := []string{
		"schema.SetAttribute",
		"types.StringType",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated resource file missing %q\n%s", want, got)
		}
	}
}

// TestResourceFile_ComputedOptional verifies that both Computed and Optional are
// emitted together.
func TestResourceFile_ComputedOptional(t *testing.T) {
	r := sampleResourceIR()
	r.Schema.Attributes = []ir.AttributeIR{
		{
			Name:     "status",
			Optional: true,
			Computed: true,
			Schema:   ir.SchemaIR{Type: ir.TypeString},
		},
	}
	got := renderResourceString(t, r)

	if !strings.Contains(got, "Optional: true") || !strings.Contains(got, "Computed: true") {
		t.Errorf("generated resource file missing Optional/Computed flags\n%s", got)
	}
}

// TestResourceFile_Sensitive verifies Sensitive attribute generation.
func TestResourceFile_Sensitive(t *testing.T) {
	r := sampleResourceIR()
	r.Schema.Attributes = []ir.AttributeIR{
		{
			Name:      "secret",
			Optional:  true,
			Sensitive: true,
			Schema:    ir.SchemaIR{Type: ir.TypeString},
		},
	}
	got := renderResourceString(t, r)

	if !strings.Contains(got, "Sensitive: true") {
		t.Errorf("generated resource file missing Sensitive flag\n%s", got)
	}
}

// TestResourceFile_WriteOnly verifies WriteOnly attribute generation.
func TestResourceFile_WriteOnly(t *testing.T) {
	r := sampleResourceIR()
	r.Schema.Attributes = []ir.AttributeIR{
		{
			Name:      "password",
			Optional:  true,
			WriteOnly: true,
			Schema:    ir.SchemaIR{Type: ir.TypeString},
		},
	}
	got := renderResourceString(t, r)

	if !strings.Contains(got, "WriteOnly: true") {
		t.Errorf("generated resource file missing WriteOnly flag\n%s", got)
	}
}

// TestResourceFile_Deprecated verifies both the generic Deprecated flag and a
// custom DeprecationMessage are emitted.
func TestResourceFile_Deprecated(t *testing.T) {
	cases := []struct {
		name       string
		attr       ir.AttributeIR
		wantSubstr string
	}{
		{
			name: "deprecated flag",
			attr: ir.AttributeIR{
				Name:       "old_field",
				Optional:   true,
				Deprecated: true,
				Schema:     ir.SchemaIR{Type: ir.TypeString},
			},
			wantSubstr: `DeprecationMessage: "Deprecated"`,
		},
		{
			name: "deprecation message",
			attr: ir.AttributeIR{
				Name:               "old_field",
				Optional:           true,
				DeprecationMessage: "Use new_field instead.",
				Schema:             ir.SchemaIR{Type: ir.TypeString},
			},
			wantSubstr: `DeprecationMessage: "Use new_field instead."`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := sampleResourceIR()
			r.Schema.Attributes = []ir.AttributeIR{tc.attr}
			got := renderResourceString(t, r)
			if !strings.Contains(got, tc.wantSubstr) {
				t.Errorf("generated resource file missing %q\n%s", tc.wantSubstr, got)
			}
		})
	}
}

// TestResourceFile_DynamicAttribute verifies DynamicAttribute generation.
func TestResourceFile_DynamicAttribute(t *testing.T) {
	r := sampleResourceIR()
	r.Schema.Attributes = []ir.AttributeIR{
		{
			Name:     "payload",
			Optional: true,
			Schema:   ir.SchemaIR{Type: ir.TypeDynamic},
		},
	}
	got := renderResourceString(t, r)

	if !strings.Contains(got, "schema.DynamicAttribute") {
		t.Errorf("generated resource file missing schema.DynamicAttribute\n%s", got)
	}
}

// TestResourceFile_UnionDynamicAttribute verifies that a Union schema is emitted
// as a DynamicAttribute.
func TestResourceFile_UnionDynamicAttribute(t *testing.T) {
	r := sampleResourceIR()
	r.Schema.Attributes = []ir.AttributeIR{
		{
			Name:     "anything",
			Optional: true,
			Schema:   ir.SchemaIR{Union: &ir.UnionType{}},
		},
	}
	got := renderResourceString(t, r)

	if !strings.Contains(got, "schema.DynamicAttribute") {
		t.Errorf("generated resource file missing schema.DynamicAttribute for union\n%s", got)
	}
}

// TestResourceFile_UnionPrimitiveTypeWins verifies that a primitive Type takes
// precedence over a Union on the same schema.
func TestResourceFile_UnionPrimitiveTypeWins(t *testing.T) {
	r := sampleResourceIR()
	r.Schema.Attributes = []ir.AttributeIR{
		{
			Name:     "value",
			Optional: true,
			Schema: ir.SchemaIR{
				Type:  ir.TypeString,
				Union: &ir.UnionType{},
			},
		},
	}
	got := renderResourceString(t, r)

	if !strings.Contains(got, "schema.StringAttribute") {
		t.Errorf("generated resource file missing schema.StringAttribute when Type and Union are set\n%s", got)
	}
	if strings.Contains(got, "schema.DynamicAttribute") {
		t.Errorf("generated resource file unexpectedly contains schema.DynamicAttribute when Type and Union are set\n%s", got)
	}
}

// TestResourceFile_ValidatorImportGating is a regression test for H-9: the
// schema/validator import must be registered exactly when a validator.<Kind>
// slice is emitted. A pure union attribute renders as DynamicAttribute with no
// Validators, so it must NOT trigger the import; an Int attribute with an
// exclusive minimum and a Map-of-string with patternProperties DO emit
// validators, so the import must be present and used. The compiled module must
// build cleanly in both cases.
func TestResourceFile_ValidatorImportGating(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}
	skipIfNetworkRestricted(t)

	// Case 1: a resource whose only validator-bearing attribute is a pure union.
	// The rendered file must not import the schema/validator package.
	unionOnly := sampleResourceIR()
	unionOnly.Name = "union_only"
	unionOnly.TypeName = "mycloud_union_only"
	unionOnly.Schema.Attributes = []ir.AttributeIR{
		{Name: "anything", Optional: true, Schema: ir.SchemaIR{Union: &ir.UnionType{}}},
	}
	got := renderResourceString(t, unionOnly)
	if strings.Contains(got, `terraform-plugin-framework/schema/validator"`) {
		t.Errorf("pure-union resource should not import schema/validator (H-9):\n%s", got)
	}

	// Case 2: a resource with an Int exclusive-minimum and a Map-of-string with
	// patternProperties, plus a pure union. The validator import is required by
	// the Int and Map attributes and must compile.
	exclMin := float64(1)
	withValidators := sampleResourceIR()
	withValidators.Name = "with_validators"
	withValidators.TypeName = "mycloud_with_validators"
	withValidators.Schema.Attributes = []ir.AttributeIR{
		{
			Name:     "count",
			Optional: true,
			Schema: ir.SchemaIR{
				Type:             ir.TypeInt,
				ExclusiveMinimum: &exclMin,
			},
		},
		{
			Name:     "anything",
			Optional: true,
			Schema:   ir.SchemaIR{Union: &ir.UnionType{}},
		},
		{
			Name:     "labels",
			Optional: true,
			Schema: ir.SchemaIR{
				Collection: &ir.CollectionType{
					Kind:        ir.Map,
					ElementType: ir.SchemaIR{Type: ir.TypeString},
				},
				PatternProperties: map[string]*ir.SchemaIR{
					"^[a-z]+$": {Type: ir.TypeString},
				},
			},
		},
	}

	p := ir.ProviderIR{
		Name:      "mycloud",
		TypeName:  "mycloud",
		Resources: []ir.ResourceIR{withValidators},
	}

	// Build a provider module that includes the custom validators file, which
	// defines Int64ExclusiveMinimumValidator and PatternPropertiesValidator
	// referenced by the generated resource.
	tmp := generateResourceModule(t, p)
	vh := Harness{OutputDir: tmp}
	if err := vh.Generate([]File{ValidatorsFile(p)}); err != nil {
		t.Fatalf("Generate validators file error = %v", err)
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
		t.Fatalf("go build failed for resource with validator gating (H-9 regression): %v\n%s", err, out)
	}
}

// TestResourceFile_NestedDynamicElementDoesNotImportValidators is a regression
// test for M-9: a collection whose element contains a nested dynamic renders as
// a DynamicAttribute with no Validators field, so the schema/validator import
// must NOT be registered even when the element also declares a constrained
// attribute. The pre-fix import gate recursed into collection elements without
// the ContainsNestedDynamic guard, registering the import unused and breaking
// the generated provider's compile ("imported and not used").
func TestResourceFile_NestedDynamicElementDoesNotImportValidators(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}
	skipIfNetworkRestricted(t)

	exclMin := float64(1)
	r := sampleResourceIR()
	r.Name = "nested_dynamic"
	r.TypeName = "mycloud_nested_dynamic"
	r.Schema.Attributes = []ir.AttributeIR{
		{
			Name:     "items",
			Optional: true,
			Schema: ir.SchemaIR{
				Collection: &ir.CollectionType{
					Kind: ir.List,
					ElementType: ir.SchemaIR{
						Attributes: []ir.AttributeIR{
							{
								Name:     "limit",
								Optional: true,
								Schema: ir.SchemaIR{
									Type:             ir.TypeInt,
									ExclusiveMinimum: &exclMin,
								},
							},
							{
								Name:     "extra",
								Optional: true,
								Schema:   ir.SchemaIR{Type: ir.TypeDynamic},
							},
						},
					},
				},
			},
		},
	}

	got := renderResourceString(t, r)
	// The collection must degrade to a DynamicAttribute (framework rejects a
	// collection whose element contains a dynamic at any depth), and that
	// DynamicAttribute carries no Validators field.
	if !strings.Contains(got, "schema.DynamicAttribute") {
		t.Fatalf("collection with nested dynamic element should render as DynamicAttribute:\n%s", got)
	}
	if strings.Contains(got, `terraform-plugin-framework/schema/validator"`) {
		t.Errorf("collection with nested dynamic element must not import schema/validator (M-9):\n%s", got)
	}

	// The generated module must build cleanly: no unused schema/validator import.
	p := ir.ProviderIR{
		Name:      "mycloud",
		TypeName:  "mycloud",
		Resources: []ir.ResourceIR{r},
	}
	tmp := generateResourceModule(t, p)
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
		t.Fatalf("go build failed for nested-dynamic collection resource (M-9 regression): %v\n%s", err, out)
	}
}

// TestResourceFile_NestingSetBlock verifies SetNestedBlock generation.
func TestResourceFile_NestingSetBlock(t *testing.T) {
	r := sampleResourceIR()
	r.Schema.Attributes = nil
	r.Schema.Blocks = []ir.BlockIR{
		{
			Name:        "alternative",
			NestingMode: ir.NestingSet,
			Schema: ir.ObjectSchemaIR{
				Attributes: []ir.AttributeIR{
					{Name: "name", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
				},
			},
		},
	}
	got := renderResourceString(t, r)

	if !strings.Contains(got, "schema.SetNestedBlock") {
		t.Errorf("generated resource file missing schema.SetNestedBlock\n%s", got)
	}
}

// TestResourceFile_SingleNestedBlock verifies SingleNestedBlock generation.
func TestResourceFile_SingleNestedBlock(t *testing.T) {
	r := sampleResourceIR()
	r.Schema.Attributes = nil
	r.Schema.Blocks = []ir.BlockIR{
		{
			Name:        "config",
			NestingMode: ir.NestingSingle,
			Schema: ir.ObjectSchemaIR{
				Attributes: []ir.AttributeIR{
					{Name: "enabled", Required: true, Schema: ir.SchemaIR{Type: ir.TypeBool}},
				},
			},
		},
	}
	got := renderResourceString(t, r)

	if !strings.Contains(got, "schema.SingleNestedBlock") {
		t.Errorf("generated resource file missing schema.SingleNestedBlock\n%s", got)
	}
}

// TestResourceFile_BlockMinMaxItems verifies MinItems/MaxItems validators are
// emitted for list-nested blocks.
func TestResourceFile_BlockMinMaxItems(t *testing.T) {
	minItems := int64(1)
	maxItems := int64(5)
	r := sampleResourceIR()
	r.Schema.Attributes = nil
	r.Schema.Blocks = []ir.BlockIR{
		{
			Name:        "items",
			NestingMode: ir.NestingList,
			MinItems:    &minItems,
			MaxItems:    &maxItems,
			Schema: ir.ObjectSchemaIR{
				Attributes: []ir.AttributeIR{
					{Name: "value", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
				},
			},
		},
	}
	got := renderResourceString(t, r)

	wantRegexes := []*regexp.Regexp{
		regexp.MustCompile(`\[\]validator\.List`),
		regexp.MustCompile(`listvalidator\.SizeAtLeast\(\s*int64\(1\)\s*\)`),
		regexp.MustCompile(`listvalidator\.SizeAtMost\(\s*int64\(5\)\s*\)`),
	}
	for _, re := range wantRegexes {
		if !re.MatchString(got) {
			t.Errorf("generated resource file missing match for %q\n%s", re.String(), got)
		}
	}
}

// TestResourceFile_UnsupportedAttributeErrors verifies that an unrecognized
// resource attribute schema surfaces a render error instead of silently emitting
// a StringAttribute.
func TestResourceFile_UnsupportedAttributeErrors(t *testing.T) {
	r := sampleResourceIR()
	r.Schema.Attributes = []ir.AttributeIR{
		{
			Name:     "bad",
			Optional: true,
			Schema:   ir.SchemaIR{},
		},
	}

	// An unrepresentable top-level attribute now renders as a DynamicAttribute
	// instead of failing generation (G2).
	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if !strings.Contains(buf.String(), `"bad": schema.DynamicAttribute`) {
		t.Errorf("expected unrepresentable attribute to render as DynamicAttribute, got:\n%s", buf.String())
	}
}

// renderResourceString renders a ResourceFile to a string for substring assertions.
func renderResourceString(t *testing.T, r ir.ResourceIR) string {
	t.Helper()
	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	return buf.String()
}

// TestCreateIDPlaceholder_Coverage verifies that createIDPlaceholder returns
// the expected HCL literal for every primitive type it recognizes.
func TestCreateIDPlaceholder_Coverage(t *testing.T) {
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
			if got := createIDPlaceholder(tc.typ); got != tc.want {
				t.Errorf("createIDPlaceholder(%q) = %q, want %q", tc.typ, got, tc.want)
			}
		})
	}
}

// TestCreateIDValue_MatchesCreateIDPlaceholder verifies that createIDValue
// emits a Go expression whose literal argument matches createIDPlaceholder.
// This keeps the generated provider's stub Create and the generated terraform
// test assertions in sync.
func TestCreateIDValue_MatchesCreateIDPlaceholder(t *testing.T) {
	cases := []struct {
		typ ir.PrimitiveType
	}{
		{ir.TypeString},
		{ir.TypeInt},
		{ir.TypeFloat},
		{ir.TypeBool},
	}
	for _, tc := range cases {
		t.Run(string(tc.typ), func(t *testing.T) {
			stmt := createIDValue(tc.typ)
			if stmt == nil {
				t.Fatalf("createIDValue(%q) returned nil", tc.typ)
			}
			want := strings.Trim(createIDPlaceholder(tc.typ), `"`)
			got := extractLiteral(t, stmt)
			if got != want {
				t.Errorf("createIDValue(%q) literal = %q, want %q", tc.typ, got, want)
			}
		})
	}

	// Unrecognized primitive types should return nil so the caller can skip id
	// assignment.
	if createIDValue(ir.TypeDynamic) != nil {
		t.Errorf("createIDValue(TypeDynamic) should return nil")
	}
}

// extractLiteral returns the single literal argument passed to the placeholder
// constructor produced by createIDValue. It is only suitable for the simple
// constructor calls produced by that helper.
func extractLiteral(t *testing.T, expr ast.Expr) string {
	t.Helper()
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		t.Fatalf("createIDValue expression is not a call: %#v", expr)
	}
	if len(call.Args) != 1 {
		t.Fatalf("createIDValue call has %d args, want 1", len(call.Args))
	}
	switch arg := call.Args[0].(type) {
	case *ast.BasicLit:
		if arg.Kind == token.STRING {
			s, err := strconv.Unquote(arg.Value)
			if err != nil {
				t.Fatalf("unquote string literal %q: %v", arg.Value, err)
			}
			return s
		}
		return arg.Value
	case *ast.Ident:
		return arg.Name
	default:
		t.Fatalf("createIDValue argument is not a literal or identifier: %#v", call.Args[0])
	}
	return ""
}

// compile-time interface checks.
var _ = ir.ResourceIR{}
var _ = time.Second

// TestResourceModel_WireNameJSONTag verifies G18: a model field whose attribute
// carries a WireName (the API's original property name) emits a json tag so the
// JSON converter uses the wire format instead of the snake_case Terraform name.
func TestResourceModel_WireNameJSONTag(t *testing.T) {
	r := sampleResourceIR()
	r.Schema.Attributes = append(r.Schema.Attributes, ir.AttributeIR{
		Name:     "can_admin",
		WireName: "canAdmin",
		Computed: true,
		Schema:   ir.SchemaIR{Type: ir.TypeBool},
	})
	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, `tfsdk:"can_admin" json:"canAdmin"`) {
		t.Errorf("model field must carry the wire-name json tag (tfsdk:\"can_admin\" json:\"canAdmin\")\n--- body ---\n%s", got)
	}
}

// TestNormalizeAttributeFlags_RequiredComputed locks in the N-25 emit-time guard:
// an attribute marked both Required and Computed is normalized the way the
// ephemeral merge resolves the conflict (Computed wins, Required clears to
// Optional) so the rendered schema stays framework-valid. The transformer
// enforces the invariant today, but the emit path must not produce a
// Required+Computed attribute if hostile IR ever reaches it.
func TestNormalizeAttributeFlags_RequiredComputed(t *testing.T) {
	attr := ir.AttributeIR{Required: true, Computed: true}
	got := normalizeAttributeFlags(attr)
	if got.Required {
		t.Error("normalizeAttributeFlags left Required set")
	}
	if !got.Optional {
		t.Error("normalizeAttributeFlags did not set Optional")
	}
	if !got.Computed {
		t.Error("normalizeAttributeFlags cleared Computed")
	}
}

// TestAttributeValues_RequiredComputedEmitsOptional verifies that the resource
// and datasource attribute-value emitters render a Required+Computed attribute
// as Optional+Computed (never Required), so a hostile IR flag combination
// cannot produce a schema the plugin-framework rejects at startup (N-25).
func TestAttributeValues_RequiredComputedEmitsOptional(t *testing.T) {
	attr := ir.AttributeIR{Name: "conflict", Required: true, Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}}

	resourceElems := resourceAttributeValues(attr, nil)
	resourceGot, err := astgen.RenderExpr(astgen.CompositeLit(astgen.Ident("schema.StringAttribute"), resourceElems...))
	if err != nil {
		t.Fatalf("resource RenderExpr() error = %v", err)
	}
	for _, want := range []string{"Optional: true", "Computed: true"} {
		if !strings.Contains(string(resourceGot), want) {
			t.Errorf("resource attribute missing %q\ncontent:\n%s", want, string(resourceGot))
		}
	}
	if strings.Contains(string(resourceGot), "Required: true") {
		t.Errorf("resource attribute must not emit Required for Required+Computed\ncontent:\n%s", string(resourceGot))
	}

	datasourceElems := datasourceAttributeValues(attr, nil)
	dsGot, err := astgen.RenderExpr(astgen.CompositeLit(astgen.Ident("schema.StringAttribute"), datasourceElems...))
	if err != nil {
		t.Fatalf("datasource RenderExpr() error = %v", err)
	}
	for _, want := range []string{"Optional: true", "Computed: true"} {
		if !strings.Contains(string(dsGot), want) {
			t.Errorf("datasource attribute missing %q\ncontent:\n%s", want, string(dsGot))
		}
	}
	if strings.Contains(string(dsGot), "Required: true") {
		t.Errorf("datasource attribute must not emit Required for Required+Computed\ncontent:\n%s", string(dsGot))
	}
}
