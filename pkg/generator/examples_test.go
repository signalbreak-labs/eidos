package generator

import (
	"bytes"
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// TestResourceExampleFile_Render verifies that ResourceExampleFile emits a valid
// HCL example containing only configurable attributes.
func TestResourceExampleFile_Render(t *testing.T) {
	r := sampleResourceIR()

	file := ResourceExampleFile(r)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		`resource "mycloud_pet" "example" {`,
		"name = \"example\"",
		"tag  = \"example\"",
		"age  = 0",
		"tags = [ \"example\" ]",
		"owner = {",
		"email = \"example\"",
		"}",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated resource example missing %q\ncontent:\n%s", want, got)
		}
	}

	// The computed-only "id" attribute should not appear in the example.
	if strings.Contains(got, "id =") {
		t.Errorf("computed id attribute should not be in example\ncontent:\n%s", got)
	}
}

// TestResourceExampleFile_Path verifies the output path uses snake_case naming.
func TestResourceExampleFile_Path(t *testing.T) {
	r := ir.ResourceIR{Name: "my_pet", TypeName: "mycloud_pet"}
	file := ResourceExampleFile(r)
	if file.Path != "examples/resources/my_pet/resource.tf" {
		t.Errorf("file.Path = %q, want %q", file.Path, "examples/resources/my_pet/resource.tf")
	}
}

// TestResourceExampleFiles_Multiple verifies that ResourceExampleFiles emits one
// example file per resource with deterministic paths.
func TestResourceExampleFiles_Multiple(t *testing.T) {
	resources := []ir.ResourceIR{
		{Name: "pet", TypeName: "mycloud_pet"},
		{Name: "owner", TypeName: "mycloud_owner"},
	}

	files := ResourceExampleFiles(resources)
	if len(files) != len(resources) {
		t.Fatalf("ResourceExampleFiles() returned %d files, want %d", len(files), len(resources))
	}

	if files[0].Path != "examples/resources/pet/resource.tf" {
		t.Errorf("file[0].Path = %q, want %q", files[0].Path, "examples/resources/pet/resource.tf")
	}
	if files[1].Path != "examples/resources/owner/resource.tf" {
		t.Errorf("file[1].Path = %q, want %q", files[1].Path, "examples/resources/owner/resource.tf")
	}
}

// TestDataSourceExampleFile_Render verifies that DataSourceExampleFile emits a
// valid HCL example and omits computed-only attributes.
func TestDataSourceExampleFile_Render(t *testing.T) {
	ds := sampleDataSourceIR()

	file := DataSourceExampleFile(ds)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		`data "mycloud_pets" "example" {`,
		"id = \"example\"",
		"}",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated data source example missing %q\ncontent:\n%s", want, got)
		}
	}

	// Computed attributes should not appear in the example.
	if strings.Contains(got, "name =") {
		t.Errorf("computed name attribute should not be in example\ncontent:\n%s", got)
	}
	if strings.Contains(got, "tags =") {
		t.Errorf("computed tags attribute should not be in example\ncontent:\n%s", got)
	}
}

// TestDataSourceExampleFiles_Multiple verifies deterministic output paths.
func TestDataSourceExampleFiles_Multiple(t *testing.T) {
	dataSources := []ir.DataSourceIR{
		{Name: "pets", TypeName: "mycloud_pets"},
		{Name: "owners", TypeName: "mycloud_owners"},
	}

	files := DataSourceExampleFiles(dataSources)
	if len(files) != len(dataSources) {
		t.Fatalf("DataSourceExampleFiles() returned %d files, want %d", len(files), len(dataSources))
	}

	if files[0].Path != "examples/data-sources/pets/data-source.tf" {
		t.Errorf("file[0].Path = %q, want %q", files[0].Path, "examples/data-sources/pets/data-source.tf")
	}
	if files[1].Path != "examples/data-sources/owners/data-source.tf" {
		t.Errorf("file[1].Path = %q, want %q", files[1].Path, "examples/data-sources/owners/data-source.tf")
	}
}

// TestEphemeralResourceExampleFile_Render verifies that
// EphemeralResourceExampleFile emits a valid HCL example for an ephemeral resource.
func TestEphemeralResourceExampleFile_Render(t *testing.T) {
	er := ir.EphemeralResourceIR{
		Name:     "token",
		TypeName: "mycloud_token",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "user_id", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
				{Name: "ttl", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeInt}},
			},
		},
	}

	file := EphemeralResourceExampleFile(er)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		`ephemeral "mycloud_token" "example" {`,
		"user_id = \"example\"",
		"ttl     = 0",
		"}",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated ephemeral resource example missing %q\ncontent:\n%s", want, got)
		}
	}

	if file.Path != "examples/ephemeral-resources/token/ephemeral-resource.tf" {
		t.Errorf("file.Path = %q, want %q", file.Path, "examples/ephemeral-resources/token/ephemeral-resource.tf")
	}
}

// TestActionExampleFile_Render verifies that ActionExampleFile emits a valid HCL
// example for an action.
func TestActionExampleFile_Render(t *testing.T) {
	a := sampleActionIR()

	file := ActionExampleFile(a)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		`action "mycloud_reboot_server" "example" {`,
		// Terraform 1.14+ wraps action arguments in a nested config block;
		// emitting them at the action block's top level is rejected as
		// "Unsupported argument" at validate/plan time.
		`config {`,
		"server_id = \"example\"",
		"force     = true",
		"}",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated action example missing %q\ncontent:\n%s", want, got)
		}
	}

	if file.Path != "examples/actions/reboot_server/action.tf" {
		t.Errorf("file.Path = %q, want %q", file.Path, "examples/actions/reboot_server/action.tf")
	}
}

// TestActionExampleFile_NoTypeName verifies that an empty ActionIR.TypeName falls
// back to a snake_cased action name in the generated example.
func TestActionExampleFile_NoTypeName(t *testing.T) {
	a := ir.ActionIR{
		Name: "Restart Service",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "service_id", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
	}

	file := ActionExampleFile(a)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, `action "restart_service" "example" {`) {
		t.Errorf("generated action example missing fallback type name\ncontent:\n%s", got)
	}
}

// TestExampleFiles_Paths verifies that ExampleFiles returns the expected set of
// example file paths for a populated provider.
func TestExampleFiles_Paths(t *testing.T) {
	p := ir.ProviderIR{
		Name:        "mycloud",
		TypeName:    "mycloud",
		Resources:   []ir.ResourceIR{{Name: "pet", TypeName: "mycloud_pet"}},
		DataSources: []ir.DataSourceIR{{Name: "pets", TypeName: "mycloud_pets"}},
		EphemeralResources: []ir.EphemeralResourceIR{
			{Name: "token", TypeName: "mycloud_token"},
		},
		Actions: []ir.ActionIR{
			{Name: "reboot", TypeName: "mycloud_reboot"},
		},
	}

	files := ExampleFiles(&p)
	wantPaths := []string{
		"examples/resources/pet/resource.tf",
		"examples/data-sources/pets/data-source.tf",
		"examples/ephemeral-resources/token/ephemeral-resource.tf",
		"examples/actions/reboot/action.tf",
	}
	if len(files) != len(wantPaths) {
		t.Fatalf("ExampleFiles() returned %d files, want %d", len(files), len(wantPaths))
	}

	for i, want := range wantPaths {
		if files[i].Path != want {
			t.Errorf("file[%d].Path = %q, want %q", i, files[i].Path, want)
		}
	}
}

// TestExampleFiles_NilProvider verifies that ExampleFiles returns nil for a nil
// provider argument.
func TestExampleFiles_NilProvider(t *testing.T) {
	files := ExampleFiles(nil)
	if files != nil {
		t.Errorf("ExampleFiles(nil) = %v, want nil", files)
	}
}

// TestExampleFiles_EmptyProvider verifies that a non-nil provider with no
// example-producing entities returns nil, matching the nil-provider zero state.
func TestExampleFiles_EmptyProvider(t *testing.T) {
	files := ExampleFiles(&ir.ProviderIR{Name: "mycloud", TypeName: "mycloud"})
	if files != nil {
		t.Errorf("ExampleFiles(empty provider) = %v, want nil", files)
	}
}

// TestWriteHCLCollectionAttribute_SetObjectAssignment verifies that a set
// collection whose element schema has attributes renders as a list-of-objects
// assignment literal (SetNestedAttribute), matching list-of-objects rendering.
func TestWriteHCLCollectionAttribute_SetObjectAssignment(t *testing.T) {
	r := ir.ResourceIR{
		Name:     "pet",
		TypeName: "mycloud_pet",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "owners",
					Optional: true,
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind: ir.Set,
							ElementType: ir.SchemaIR{
								Attributes: []ir.AttributeIR{
									{Name: "email", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
								},
							},
						},
					},
				},
			},
		},
	}

	got := generateResourceExampleHCL(r)
	if !strings.Contains(got, "owners = [{") {
		t.Errorf("set of objects should render as a list-of-objects assignment (SetNestedAttribute)\n%s", got)
	}
	if !strings.Contains(got, "}]") {
		t.Errorf("set of objects assignment literal should close with }]\n%s", got)
	}
	if !strings.Contains(got, "email = \"example\"") {
		t.Errorf("set object element body should be rendered\n%s", got)
	}
}

// TestWriteHCLCollectionAttribute_ListObjectAssignment verifies that a list
// collection whose element schema has attributes renders as a list-of-objects
// assignment literal (ListNestedAttribute), not repeated block syntax. This is
// the H-4 regression.
func TestWriteHCLCollectionAttribute_ListObjectAssignment(t *testing.T) {
	r := ir.ResourceIR{
		Name:     "pet",
		TypeName: "mycloud_pet",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "owners",
					Optional: true,
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind: ir.List,
							ElementType: ir.SchemaIR{
								Attributes: []ir.AttributeIR{
									{Name: "email", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
								},
							},
						},
					},
				},
			},
		},
	}

	got := generateResourceExampleHCL(r)
	if !strings.Contains(got, "owners = [{") {
		t.Errorf("list of objects should render as a list-of-objects assignment (ListNestedAttribute)\n%s", got)
	}
	if !strings.Contains(got, "}]") {
		t.Errorf("list of objects assignment literal should close with }]\n%s", got)
	}
	if strings.Contains(got, "owners {") {
		t.Errorf("list of objects must not use block syntax (H-4)\n%s", got)
	}
}

// TestWriteHCLCollectionAttribute_MapKeyFromName verifies that map collection
// examples use a placeholder key derived from the attribute name.
func TestWriteHCLCollectionAttribute_MapKeyFromName(t *testing.T) {
	r := ir.ResourceIR{
		Name:     "pet",
		TypeName: "mycloud_pet",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "tag_labels",
					Optional: true,
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind:        ir.Map,
							ElementType: ir.SchemaIR{Type: ir.TypeString},
						},
					},
				},
			},
		},
	}

	got := generateResourceExampleHCL(r)
	if !strings.Contains(got, "tag_labels = {") {
		t.Errorf("map attribute should be rendered\n%s", got)
	}
	if !strings.Contains(got, "\"tag_labels\" = \"example\"") {
		t.Errorf("map key should be derived from attribute name\n%s", got)
	}
}

// TestWriteHCLCollectionAttribute_UnionElementGraceful verifies that a
// collection whose element type is a oneOf/anyOf union degrades gracefully
// instead of panicking. A union element normalizes to a dynamic element
// (DynamicUnionElement), so the collection degrades to a DynamicAttribute and
// the example writer emits a populated placeholder matching the collection shape
// (a list/set literal or a map literal with a single "example" element) rather
// than a bare null. This is the H-5 regression: previously
// writeHCLCollectionAttribute panicked for `array` with `items: {oneOf: ...}`,
// and the ExampleFiles call path had no recover, crashing the whole CLI run.
func TestWriteHCLCollectionAttribute_UnionElementGraceful(t *testing.T) {
	cases := []struct {
		name string
		kind ir.CollectionKind
		want string
	}{
		{name: "list", kind: ir.List, want: `bad = [ "example" ]`},
		{name: "set", kind: ir.Set, want: `bad = [ "example" ]`},
		{name: "map", kind: ir.Map, want: `bad = { "key" = "example" }`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := ir.ResourceIR{
				Name:     "pet",
				TypeName: "mycloud_pet",
				Schema: ir.ObjectSchemaIR{
					Attributes: []ir.AttributeIR{
						{
							Name:     "bad",
							Optional: true,
							Schema: ir.SchemaIR{
								Collection: &ir.CollectionType{
									Kind:        tc.kind,
									ElementType: ir.SchemaIR{Union: &ir.UnionType{}},
								},
							},
						},
					},
				},
			}

			defer func() {
				if rec := recover(); rec != nil {
					t.Fatalf("expected graceful fallback, got panic: %v", rec)
				}
			}()
			got := generateResourceExampleHCL(r)
			if !strings.Contains(got, tc.want) {
				t.Errorf("union-element %s collection should render %q, got:\n%s", tc.name, tc.want, got)
			}
		})
	}
}

// TestWriteHCLBlock_NestedBlock verifies that writeHCLBlock emits a nested block
// with its configurable body.
func TestWriteHCLBlock_NestedBlock(t *testing.T) {
	r := ir.ResourceIR{
		Name:     "pet",
		TypeName: "mycloud_pet",
		Schema: ir.ObjectSchemaIR{
			Blocks: []ir.BlockIR{
				{
					Name:        "owner",
					NestingMode: ir.NestingSingle,
					Schema: ir.ObjectSchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "email", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				},
			},
		},
	}

	got := generateResourceExampleHCL(r)
	if !strings.Contains(got, "owner {") {
		t.Errorf("expected nested block\n%s", got)
	}
	if !strings.Contains(got, "email = \"example\"") {
		t.Errorf("expected nested block body\n%s", got)
	}
}

// TestExampleFiles_Harness verifies that generated example files can be written
// to an output directory by Harness.Generate.
func TestExampleFiles_Harness(t *testing.T) {
	p := ir.ProviderIR{
		Name:     "mycloud",
		TypeName: "mycloud",
		Resources: []ir.ResourceIR{
			{Name: "pet", TypeName: "mycloud_pet"},
		},
		DataSources: []ir.DataSourceIR{
			{Name: "pets", TypeName: "mycloud_pets"},
		},
	}

	tmp := t.TempDir()
	h := Harness{OutputDir: tmp}
	if err := h.Generate(ExampleFiles(&p)); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	gotPaths := collectPaths(t, tmp)
	wantPaths := []string{
		"examples/data-sources/pets/data-source.tf",
		"examples/resources/pet/resource.tf",
	}
	if diff := sliceDiff(wantPaths, gotPaths); diff != "" {
		t.Errorf("emitted paths mismatch:\n%s", diff)
	}
}

// TestWriteHCLDiscriminatedUnionAligned verifies that a discriminated union
// attribute renders as an object literal whose discriminator and merged
// attribute rows have aligned `=` signs (fmt-clean), with the discriminator set
// to the first sorted mapping key.
func TestWriteHCLDiscriminatedUnionAligned(t *testing.T) {
	r := ir.ResourceIR{
		Name:     "pet",
		TypeName: "mycloud_pet",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "animal",
					Required: true,
					Schema: ir.SchemaIR{
						Union: &ir.UnionType{
							Kind: ir.OneOf,
							Variants: []ir.SchemaIR{
								{
									Name: "cat",
									Attributes: []ir.AttributeIR{
										{Name: "animal_type", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
										{Name: "lives", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeInt}},
									},
								},
								{
									Name: "dog",
									Attributes: []ir.AttributeIR{
										{Name: "animal_type", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
										{Name: "bark_volume", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeInt}},
									},
								},
							},
							Discriminator: &ir.DiscriminatorIR{
								PropertyName: "animalType",
								Mapping:      map[string]string{"cat": "CatVariant", "dog": "DogVariant"},
							},
						},
					},
				},
			},
		},
	}

	got := generateResourceExampleHCL(r)
	for _, want := range []string{
		"animal = {",
		// animal_type and bark_volume are both 11 chars, so they sit at the
		// run's max width with a single space; the shorter lives row pads to
		// align the `=` signs at column 12 (fmt-clean).
		`animal_type = "cat"`,
		"bark_volume = 0",
		"lives       = 0",
		"}",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("union example missing %q\n%s", want, got)
		}
	}
}

// TestWriteHCLAttributeValue_DynamicCollection covers the degraded-collection
// and union branches of writeHCLAttributeValue: a dynamic collection whose
// element is object-like renders as a block (single=false), a dynamic collection
// with a non-object element renders a populated literal, an unknown collection
// kind falls back to a populated literal, and a non-discriminated union renders
// a scalar placeholder.
func TestWriteHCLAttributeValue_DynamicCollection(t *testing.T) {
	cases := []struct {
		name   string
		attr   ir.AttributeIR
		want   string
		single bool
	}{
		{
			name: "dynamic list with object-like element renders as block",
			attr: ir.AttributeIR{
				Name: "bad",
				Schema: ir.SchemaIR{
					Collection: &ir.CollectionType{
						Kind: ir.List,
						ElementType: ir.SchemaIR{
							Attributes: []ir.AttributeIR{
								{Name: "x", Schema: ir.SchemaIR{Type: ir.TypeDynamic}},
							},
						},
					},
				},
			},
			want:   "",
			single: false,
		},
		{
			name: "list of nested collection renders populated literal",
			attr: ir.AttributeIR{
				Name: "bad",
				Schema: ir.SchemaIR{
					Collection: &ir.CollectionType{
						Kind: ir.List,
						ElementType: ir.SchemaIR{
							Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				},
			},
			want:   `[ "example" ]`,
			single: true,
		},
		{
			name: "map of nested collection renders populated literal",
			attr: ir.AttributeIR{
				Name: "bad",
				Schema: ir.SchemaIR{
					Collection: &ir.CollectionType{
						Kind: ir.Map,
						ElementType: ir.SchemaIR{
							Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				},
			},
			want:   `{ "key" = "example" }`,
			single: true,
		},
		{
			name: "unknown collection kind renders populated literal",
			attr: ir.AttributeIR{
				Name: "bad",
				Schema: ir.SchemaIR{
					Collection: &ir.CollectionType{
						Kind:        ir.CollectionKind("tuple"),
						ElementType: ir.SchemaIR{Type: ir.TypeString},
					},
				},
			},
			want:   `[ "example" ]`,
			single: true,
		},
		{
			name: "non-discriminated union renders scalar placeholder",
			attr: ir.AttributeIR{
				Name:   "bad",
				Schema: ir.SchemaIR{Union: &ir.UnionType{}},
			},
			want:   `"example"`,
			single: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, single := writeHCLAttributeValue(tc.attr)
			if got != tc.want || single != tc.single {
				t.Errorf("writeHCLAttributeValue(%s) = %q, %v; want %q, %v", tc.attr.Name, got, single, tc.want, tc.single)
			}
		})
	}
}

// TestDynamicCollectionExampleValue covers the populated-literal placeholder for
// a collection that degraded to a DynamicAttribute: a map literal for Map, a
// list literal for List/Set.
func TestDynamicCollectionExampleValue(t *testing.T) {
	if got := dynamicCollectionExampleValue(&ir.CollectionType{Kind: ir.Map}); got != `{ "key" = "example" }` {
		t.Errorf("map placeholder = %q, want { \"key\" = \"example\" }", got)
	}
	if got := dynamicCollectionExampleValue(&ir.CollectionType{Kind: ir.List}); got != `[ "example" ]` {
		t.Errorf("list placeholder = %q, want [ \"example\" ]", got)
	}
	if got := dynamicCollectionExampleValue(&ir.CollectionType{Kind: ir.Set}); got != `[ "example" ]` {
		t.Errorf("set placeholder = %q, want [ \"example\" ]", got)
	}
}
