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

// TestDataSourceFile_Render verifies that DataSourceFile emits the expected
// data source struct, model, Metadata, Schema, and Read methods.
func TestDataSourceFile_Render(t *testing.T) {
	ds := sampleDataSourceIR()

	file := DataSourceFile(ds, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"package provider",
		"var _ datasource.DataSource = (*PetsDataSource)(nil)",
		"type PetsDataSource struct",
		"type PetsDataSourceModel struct",
		// The model field lines are matched with collapsed whitespace so they do
		// not depend on gofmt's column alignment, which would break spuriously if
		// a field-name length changes in the fixture (L-54).
		"Id types.String `tfsdk:\"id\"`",
		"Name types.String `tfsdk:\"name\"`",
		"Tags types.List `tfsdk:\"tags\"`",
		"func NewPetsDataSource()",
		"func (d *PetsDataSource) Metadata",
		"func (d *PetsDataSource) Schema",
		"func (d *PetsDataSource) Read",
		"var config PetsDataSourceModel",
		"req.Config.Get(ctx, &config)",
		"resp.State.Set(ctx, &config)",
		"resp.TypeName = \"mycloud_pets\"",
		"schema.Schema",
		"schema.StringAttribute",
		"schema.ListAttribute",
		"ElementType: types.StringType",
		"Computed: true",
		"Required: true",
	}
	// collapseSpaces renders got with runs of whitespace reduced to single
	// spaces so the alignment-sensitive model field assertions are robust to
	// gofmt column changes (L-54).
	collapsed := collapseSpaces(got)
	for _, want := range wantSubstrings {
		if !strings.Contains(collapsed, want) {
			t.Errorf("generated data source missing %q\ncontent:\n%s", want, got)
		}
	}
}

// collapseSpaces returns s with every run of whitespace characters collapsed to
// a single space. It lets assertions match generated Go source without
// depending on gofmt's column alignment.
func collapseSpaces(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if !inSpace {
				b.WriteByte(' ')
				inSpace = true
			}
			continue
		}
		inSpace = false
		b.WriteRune(r)
	}
	return b.String()
}

// TestDataSourceFile_SchemaValidation generates a minimal provider with a data
// source into a temporary Go module and runs the Terraform plugin-framework
// schema validation to confirm the generated data_source_<name>.go compiles and
// its schema is valid.
func TestDataSourceFile_SchemaValidation(t *testing.T) {
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

	ds := sampleDataSourceIR()

	tmp := generateDataSourceModule(t, providerIR, []ir.DataSourceIR{ds})
	writeDataSourceSchemaValidationTest(t, tmp, ds)

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

// TestDataSourceFile_NestedSchema verifies that nested attributes and blocks are
// rendered using the datasource/schema package.
func TestDataSourceFile_NestedSchema(t *testing.T) {
	ds := ir.DataSourceIR{
		Name:     "pet",
		TypeName: "mycloud_pet",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name: "owner",
					Schema: ir.SchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString, Computed: true}},
						},
					},
				},
				{
					Name: "aliases",
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind:        ir.List,
							ElementType: ir.SchemaIR{Attributes: []ir.AttributeIR{{Name: "value", Schema: ir.SchemaIR{Type: ir.TypeString, Computed: true}}}},
						},
					},
				},
			},
			Blocks: []ir.BlockIR{
				{
					Name:        "metadata",
					NestingMode: ir.NestingList,
					Schema: ir.ObjectSchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "key", Schema: ir.SchemaIR{Type: ir.TypeString, Computed: true}},
						},
					},
				},
			},
		},
	}

	file := DataSourceFile(ds, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"type PetDataSource struct",
		"schema.SingleNestedAttribute",
		"schema.ListNestedAttribute",
		"schema.ListNestedBlock",
		"NestedObject: schema.NestedAttributeObject",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated nested data source missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestDataSourceTypeName verifies that the Terraform type name prefers
// DataSourceIR.TypeName and falls back to the trimmed data source name.
func TestDataSourceTypeName(t *testing.T) {
	cases := []struct {
		name     string
		ds       ir.DataSourceIR
		wantName string
	}{
		{
			name:     "prefers type name",
			ds:       ir.DataSourceIR{Name: "pets", TypeName: "mycloud_pets"},
			wantName: "mycloud_pets",
		},
		{
			name:     "falls back to name",
			ds:       ir.DataSourceIR{Name: "pets"},
			wantName: "pets",
		},
		{
			name:     "trims whitespace fallback",
			ds:       ir.DataSourceIR{Name: "  pets  "},
			wantName: "pets",
		},
		{
			name:     "trims whitespace type name",
			ds:       ir.DataSourceIR{Name: "pets", TypeName: "  mycloud_pets  "},
			wantName: "mycloud_pets",
		},
		{
			name:     "snake cases fallback",
			ds:       ir.DataSourceIR{Name: "My Pets"},
			wantName: "my_pets",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := dataSourceTypeName(tc.ds); got != tc.wantName {
				t.Errorf("dataSourceTypeName() = %q, want %q", got, tc.wantName)
			}
		})
	}
}

// TestDataSourceStructName verifies generated data source struct naming.
func TestDataSourceStructName(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"pets", "PetsDataSource"},
		{"my_cloud", "MyCloudDataSource"},
		{"", "DataSource"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ds := ir.DataSourceIR{Name: tc.name}
			if got := dataSourceStructName(ds); got != tc.want {
				t.Errorf("dataSourceStructName(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// TestDataSourceFiles verifies that DataSourceFiles emits one file per data source
// in order.
func TestDataSourceFiles(t *testing.T) {
	dataSources := []ir.DataSourceIR{
		{Name: "pets"},
		{Name: "owners"},
	}
	files := DataSourceFiles(dataSources, testClientImport)
	if len(files) != len(dataSources) {
		t.Fatalf("DataSourceFiles returned %d files, want %d", len(files), len(dataSources))
	}
	wantPaths := []string{
		"internal/provider/data_source_pets.go",
		"internal/provider/data_source_owners.go",
	}
	for i, want := range wantPaths {
		if files[i].Path != want {
			t.Errorf("file[%d].Path = %q, want %q", i, files[i].Path, want)
		}
	}
}

// TestDataSourceFile_PrimitiveTypes verifies that Float64, Bool, and Dynamic
// primitive types are emitted using the correct datasource/schema attribute types.
func TestDataSourceFile_PrimitiveTypes(t *testing.T) {
	ds := ir.DataSourceIR{
		Name: "primitives",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "ratio", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeFloat}},
				{Name: "enabled", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeBool}},
				{Name: "anything", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeDynamic}},
			},
		},
	}

	file := DataSourceFile(ds, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"schema.Float64Attribute",
		"schema.BoolAttribute",
		"schema.DynamicAttribute",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated primitive data source missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestDataSourceFile_UnionDynamicAttribute verifies that a Union schema is emitted
// as a DynamicAttribute.
func TestDataSourceFile_UnionDynamicAttribute(t *testing.T) {
	ds := ir.DataSourceIR{
		Name: "union",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "anything", Computed: true, Schema: ir.SchemaIR{Union: &ir.UnionType{}}},
			},
		},
	}

	file := DataSourceFile(ds, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, "schema.DynamicAttribute") {
		t.Errorf("generated data source missing schema.DynamicAttribute for union\ncontent:\n%s", got)
	}
}

// TestDataSourceFile_MapAndSetCollections verifies that Map and Set collection
// types are emitted using the correct datasource/schema attribute types.
func TestDataSourceFile_MapAndSetCollections(t *testing.T) {
	ds := ir.DataSourceIR{
		Name: "collections",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "tags",
					Computed: true,
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind:        ir.Map,
							ElementType: ir.SchemaIR{Type: ir.TypeString},
						},
					},
				},
				{
					Name:     "owners",
					Computed: true,
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind: ir.Map,
							ElementType: ir.SchemaIR{
								Attributes: []ir.AttributeIR{
									{Name: "name", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
								},
							},
						},
					},
				},
				{
					Name:     "ids",
					Computed: true,
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind:        ir.Set,
							ElementType: ir.SchemaIR{Type: ir.TypeInt},
						},
					},
				},
				{
					Name:     "groups",
					Computed: true,
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind: ir.Set,
							ElementType: ir.SchemaIR{
								Attributes: []ir.AttributeIR{
									{Name: "name", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
								},
							},
						},
					},
				},
			},
		},
	}

	file := DataSourceFile(ds, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"schema.MapAttribute",
		"schema.MapNestedAttribute",
		"schema.SetAttribute",
		"schema.SetNestedAttribute",
		"ElementType: types.StringType",
		"ElementType: types.Int64Type",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated Map/Set data source missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestDataSourceFile_AdvancedAttributes verifies that Sensitive,
// DeprecationMessage, Deprecated shorthand, MarkdownDescription, and default
// Computed behavior are surfaced in data source schema attributes.
func TestDataSourceFile_AdvancedAttributes(t *testing.T) {
	ds := ir.DataSourceIR{
		Name:        "advanced",
		Description: "Advanced data source.",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:                "secret",
					Computed:            true,
					Sensitive:           true,
					MarkdownDescription: "A sensitive value.",
					Schema:              ir.SchemaIR{Type: ir.TypeString},
				},
				{
					Name:               "old_msg",
					Computed:           true,
					DeprecationMessage: "Use new_msg instead.",
					Schema:             ir.SchemaIR{Type: ir.TypeString},
				},
				{
					Name:       "old_flag",
					Computed:   true,
					Deprecated: true,
					Schema:     ir.SchemaIR{Type: ir.TypeString},
				},
				{
					Name:   "default_computed",
					Schema: ir.SchemaIR{Type: ir.TypeString},
				},
			},
		},
	}

	file := DataSourceFile(ds, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, "Sensitive:") {
		t.Errorf("generated advanced data source missing Sensitive marker\ncontent:\n%s", got)
	}
	if !strings.Contains(got, "\"Use new_msg instead.\"") {
		t.Errorf("generated advanced data source missing explicit DeprecationMessage\ncontent:\n%s", got)
	}
	if !strings.Contains(got, "\"Deprecated\"") {
		t.Errorf("generated advanced data source missing shorthand DeprecationMessage\ncontent:\n%s", got)
	}
	if !strings.Contains(got, "\"Advanced data source.\"") {
		t.Errorf("generated advanced data source missing top-level MarkdownDescription\ncontent:\n%s", got)
	}
	if !strings.Contains(got, "\"A sensitive value.\"") {
		t.Errorf("generated advanced data source missing attribute MarkdownDescription\ncontent:\n%s", got)
	}
	if !strings.Contains(got, "Computed: true") {
		t.Errorf("generated advanced data source missing Computed marker\ncontent:\n%s", got)
	}
}

// TestDataSourceFile_DefaultComputed verifies that an attribute with neither
// Required nor Optional nor Computed set defaults to Computed.
func TestDataSourceFile_DefaultComputed(t *testing.T) {
	ds := ir.DataSourceIR{
		Name: "default_computed",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "value", Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
	}

	file := DataSourceFile(ds, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, "Computed: true") {
		t.Errorf("attribute with no optionality flag should default to Computed\ncontent:\n%s", got)
	}
	if strings.Contains(got, "Required: true") {
		t.Error("attribute with no optionality flag should not be Required")
	}
	if strings.Contains(got, "Optional: true") {
		t.Error("attribute with no optionality flag should not be Optional")
	}
}

// TestDataSourceFile_MarkdownDescriptionOmitted verifies that an empty top-level
// description omits the MarkdownDescription field entirely.
func TestDataSourceFile_MarkdownDescriptionOmitted(t *testing.T) {
	ds := ir.DataSourceIR{
		Name:        "no_desc",
		Description: "",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "value", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
	}

	file := DataSourceFile(ds, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if strings.Contains(got, "MarkdownDescription:") {
		t.Errorf("empty description should omit MarkdownDescription field\ncontent:\n%s", got)
	}
}

// TestDataSourceFile_EmptyConfigSchema verifies that a data source with an empty
// schema renders without Attributes or Blocks.
func TestDataSourceFile_EmptyConfigSchema(t *testing.T) {
	ds := ir.DataSourceIR{Name: "empty"}

	file := DataSourceFile(ds, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if strings.Contains(got, "Attributes:") {
		t.Error("empty schema should not emit Attributes")
	}
	if strings.Contains(got, "Blocks:") {
		t.Error("empty schema should not emit Blocks")
	}
}

// TestDataSourceFile_BlockCardinality verifies that MinItems and MaxItems on
// list-nested and set-nested blocks are emitted as listvalidator and setvalidator
// validators.
func TestDataSourceFile_BlockCardinality(t *testing.T) {
	minItems := int64(1)
	maxItems := int64(5)
	ds := ir.DataSourceIR{
		Name: "cardinality",
		Schema: ir.ObjectSchemaIR{
			Blocks: []ir.BlockIR{
				{
					Name:        "metadata",
					NestingMode: ir.NestingList,
					MinItems:    &minItems,
					MaxItems:    &maxItems,
					Schema: ir.ObjectSchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "key", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				},
				{
					Name:        "tags",
					NestingMode: ir.NestingSet,
					MinItems:    &minItems,
					MaxItems:    &maxItems,
					Schema: ir.ObjectSchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "value", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				},
			},
		},
	}

	file := DataSourceFile(ds, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"[]validator.List",
		"listvalidator.SizeAtLeast(int64(1))",
		"listvalidator.SizeAtMost(int64(5))",
		"[]validator.Set",
		"setvalidator.SizeAtLeast(int64(1))",
		"setvalidator.SizeAtMost(int64(5))",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated cardinality data source missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestDataSourceFile_NestingSetAndSingleNestedBlock verifies that SetNestedBlock
// and SingleNestedBlock are emitted using the datasource/schema package.
func TestDataSourceFile_NestingSetAndSingleNestedBlock(t *testing.T) {
	ds := ir.DataSourceIR{
		Name: "nested_blocks",
		Schema: ir.ObjectSchemaIR{
			Blocks: []ir.BlockIR{
				{
					Name:        "metadata",
					NestingMode: ir.NestingSet,
					Schema: ir.ObjectSchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "key", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				},
				{
					Name:        "singleton",
					NestingMode: ir.NestingSingle,
					Schema: ir.ObjectSchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "value", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				},
			},
		},
	}

	file := DataSourceFile(ds, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"schema.SetNestedBlock",
		"schema.SingleNestedBlock",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated nested block data source missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestDataSourceAttributeExpr_Panic verifies that an unsupported attribute
// schema causes datasourceAttributeExpr to panic.
func TestDataSourceAttributeExpr_Panic(t *testing.T) {
	// An unrepresentable top-level attribute now renders as a DynamicAttribute
	// instead of panicking (G2).
	attr := ir.AttributeIR{Name: "bad", Schema: ir.SchemaIR{}}
	expr := datasourceAttributeExpr(attr)
	if expr == nil {
		t.Fatal("datasourceAttributeExpr returned nil for unsupported schema")
	}
}

// sampleDataSourceIR returns a DataSourceIR used for render and validation tests.
func sampleDataSourceIR() ir.DataSourceIR {
	return ir.DataSourceIR{
		Name:        "pets",
		TypeName:    "mycloud_pets",
		Description: "Fetches a list of pets.",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "id",
					Required: true,
					Schema:   ir.SchemaIR{Type: ir.TypeString},
				},
				{
					Name:     "name",
					Computed: true,
					Schema:   ir.SchemaIR{Type: ir.TypeString},
				},
				{
					Name:     "tags",
					Computed: true,
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind:        ir.List,
							ElementType: ir.SchemaIR{Type: ir.TypeString},
						},
					},
				},
			},
		},
	}
}

// generateDataSourceModule writes the generated go.mod, provider.go, and data
// source files into a temporary module directory and returns the module root.
func generateDataSourceModule(t *testing.T, providerIR ir.ProviderIR, dataSources []ir.DataSourceIR) string {
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
		files = append(files, DataSourceFile(ds, testClientImport))
	}
	if err := h.Generate(files); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	return tmp
}

// writeDataSourceSchemaValidationTest writes a small test file that imports the
// generated data source and validates its schema implementation.
func writeDataSourceSchemaValidationTest(t *testing.T, moduleRoot string, ds ir.DataSourceIR) {
	t.Helper()
	testPath := filepath.Join(moduleRoot, "internal", "provider", "datasource_schema_validate_test.go")
	if err := os.MkdirAll(filepath.Dir(testPath), 0o750); err != nil {
		t.Fatalf("create provider test dir: %v", err)
	}

	structName := dataSourceStructName(ds)
	content := `package provider

import (
	"context"
	"testing"

	tfframeworkdatasource "github.com/hashicorp/terraform-plugin-framework/datasource"
)

func Test` + structName + `SchemaValidation(t *testing.T) {
	ds := New` + structName + `()
	var resp tfframeworkdatasource.SchemaResponse
	ds.Schema(context.Background(), tfframeworkdatasource.SchemaRequest{}, &resp)

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
