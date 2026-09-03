package generator

import (
	"bytes"
	"sort"
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// TestActionDocsFile_Render verifies that ActionDocsFile emits correct
// frontmatter, example usage, and schema sections for an action.
func TestActionDocsFile_Render(t *testing.T) {
	a := sampleActionIR()

	file := ActionDocsFile(a)
	if file.Path != "docs/actions/reboot_server.md" {
		t.Errorf("file.Path = %q, want %q", file.Path, "docs/actions/reboot_server.md")
	}

	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"---",
		`page_title: "mycloud_reboot_server Action - mycloud"`,
		`subcategory: ""`,
		"description: |-",
		"Reboots a server.",
		"# mycloud_reboot_server Action",
		"## Example Usage",
		`action "mycloud_reboot_server" "example" {`,
		"server_id = \"example\"",
		"## Schema",
		"### Arguments",
		"* `server_id` (String, required)",
		"* `force` (Boolean, optional)",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated action docs missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestEphemeralResourceDocsFile_Render verifies that
// EphemeralResourceDocsFile emits frontmatter, config arguments, result
// attributes, and an ephemeral note.
func TestEphemeralResourceDocsFile_Render(t *testing.T) {
	er := sampleEphemeralResourceIR()

	file := EphemeralResourceDocsFile(er)
	if file.Path != "docs/ephemeral-resources/temporary_credential.md" {
		t.Errorf("file.Path = %q, want %q", file.Path, "docs/ephemeral-resources/temporary_credential.md")
	}

	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"---",
		`page_title: "mycloud_temporary_credential Ephemeral Resource - mycloud"`,
		`subcategory: ""`,
		"description: |-",
		"Generates a temporary credential.",
		"# mycloud_temporary_credential Ephemeral Resource",
		"Ephemeral resources are only available within the context of a single Terraform operation",
		"## Example Usage",
		`ephemeral "mycloud_temporary_credential" "example" {`,
		"duration = 0",
		"## Schema",
		"### Arguments",
		"* `duration` (Number, required)",
		"* `scope` (String, optional)",
		"### Attributes",
		"* `token` (String, computed)",
		"* `expires_at` (String, computed)",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated ephemeral resource docs missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestListResourceDocsFile_Render verifies that ListResourceDocsFile emits
// correct frontmatter, query example, and schema sections for a list resource.
func TestListResourceDocsFile_Render(t *testing.T) {
	lr := sampleListResourceIR()

	file := ListResourceDocsFile(lr)
	if file.Path != "docs/list-resources/pets.md" {
		t.Errorf("file.Path = %q, want %q", file.Path, "docs/list-resources/pets.md")
	}

	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"---",
		`page_title: "mycloud_pets List Resource - mycloud"`,
		`subcategory: ""`,
		"description: |-",
		"Lists pets.",
		"# mycloud_pets List Resource",
		"## Example Usage",
		`list "mycloud_pets" "example" {`,
		"provider = mycloud",
		"limit    = 100",
		"config {",
		"id   = \"example\"",
		"## Schema",
		"### Arguments",
		"* `id` (String, required)",
		"* `tags` (List of String, optional)",
		"* `labels` (Map of String, optional)",
		"* `owner` (Attributes, optional) (see [below for nested schema](#nestedatt--owner))",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated list resource docs missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestFunctionDocsFile_Render verifies that FunctionDocsFile emits correct
// frontmatter, example usage, signature, argument details, and return type.
func TestFunctionDocsFile_Render(t *testing.T) {
	fn := sampleFunctionIR()

	file := FunctionDocsFile(fn, "mycloud")
	if file.Path != "docs/functions/concat_tags.md" {
		t.Errorf("file.Path = %q, want %q", file.Path, "docs/functions/concat_tags.md")
	}

	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"---",
		`page_title: "concat_tags Function - mycloud"`,
		`subcategory: ""`,
		"description: |-",
		"Joins a list of tags with a separator.",
		"# concat_tags Function",
		"## Example Usage",
		`provider::mycloud::concat_tags("<separator>", ["<tags>"])`,
		"## Signature",
		"concat_tags(separator: String, tags: List of String) -> String",
		"## Arguments",
		"* `separator` (String) - Delimiter placed between tags.",
		"* `tags` (List of String) - Tags to join.",
		"## Return",
		"`String`",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated function docs missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestProviderDocsIndex_AdvancedSections verifies that the index stays
// provider-focused — description, example configuration, and schema — even
// when the provider defines actions, ephemeral resources, list resources, or
// functions: construct link lists live in the Registry's own navigation, not
// the index page.
func TestProviderDocsIndex_AdvancedSections(t *testing.T) {
	pir := ir.ProviderIR{
		Name:        "mycloud",
		TypeName:    "mycloud",
		Description: "Generated provider for MyCloud.",
		Actions: []ir.ActionIR{
			{Name: "reboot_server", TypeName: "mycloud_reboot_server"},
		},
		EphemeralResources: []ir.EphemeralResourceIR{
			{Name: "temporary_credential", TypeName: "mycloud_temporary_credential"},
		},
		ListResources: []ir.ListResourceIR{
			{Name: "pets", TypeName: "mycloud_pets", Registerable: true},
		},
		Functions: []ir.FunctionIR{
			{Name: "concat_tags", TypeName: "concat_tags"},
		},
	}

	file := ProviderDocsIndex(pir)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"# mycloud Provider",
		"Generated provider for MyCloud.",
		"## Example Usage",
		`provider "mycloud" {`,
		"## Schema",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated index missing %q\ncontent:\n%s", want, got)
		}
	}

	for _, gone := range []string{
		"## Actions",
		"## Ephemeral Resources",
		"## List Resources",
		"## Functions",
		"(actions/",
		"(ephemeral-resources/",
		"(list-resources/",
		"(functions/",
	} {
		if strings.Contains(got, gone) {
			t.Errorf("generated index unexpectedly contains %q\ncontent:\n%s", gone, got)
		}
	}
}

// TestProviderDocsFiles_Advanced verifies that ProviderDocsFiles emits the
// index plus the expected advanced documentation paths. The comparison uses
// sorted sets so adding resources or data sources to the test fixture does not
// break positional assertions.
func TestProviderDocsFiles_Advanced(t *testing.T) {
	pir := ir.ProviderIR{
		Name:     "mycloud",
		TypeName: "mycloud",
		Actions: []ir.ActionIR{
			{Name: "reboot_server", TypeName: "mycloud_reboot_server"},
		},
		EphemeralResources: []ir.EphemeralResourceIR{
			{Name: "temporary_credential", TypeName: "mycloud_temporary_credential"},
		},
		ListResources: []ir.ListResourceIR{
			// Registerable: the provider can register this list against the
			// mycloud_pets managed resource, so it is documented.
			{Name: "pets", TypeName: "mycloud_pets", Registerable: true},
		},
		Functions: []ir.FunctionIR{
			{Name: "concat_tags", TypeName: "concat_tags"},
		},
	}

	files := ProviderDocsFiles(pir)
	got := make([]string, len(files))
	for i, f := range files {
		got[i] = f.Path
	}
	sort.Strings(got)
	want := []string{
		"docs/index.md",
		"docs/actions/reboot_server.md",
		"docs/ephemeral-resources/temporary_credential.md",
		"docs/list-resources/pets.md",
		"docs/functions/concat_tags.md",
	}
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("ProviderDocsFiles() returned %d files, want %d", len(got), len(want))
	}
	for i, path := range want {
		if got[i] != path {
			t.Errorf("sorted files[%d].Path = %q, want %q", i, got[i], path)
		}
	}
}

// TestFunctionArgumentPlaceholder verifies that function argument placeholders
// expand collections and objects into syntactically valid HCL and use
// angle-bracket labels for string values.
func TestFunctionArgumentPlaceholder(t *testing.T) {
	stringSchema := ir.SchemaIR{Type: ir.TypeString}
	intSchema := ir.SchemaIR{Type: ir.TypeInt}
	objectSchema := ir.SchemaIR{
		Attributes: []ir.AttributeIR{
			{Name: "name", Schema: stringSchema},
			{Name: "age", Schema: intSchema},
		},
	}

	cases := []struct {
		name   string
		schema ir.SchemaIR
		label  string
		want   string
	}{
		{
			name:   "string",
			schema: stringSchema,
			label:  "separator",
			want:   `"<separator>"`,
		},
		{
			name:   "dynamic",
			schema: ir.SchemaIR{Type: ir.TypeDynamic},
			label:  "value",
			want:   `"example"`,
		},
		{
			name: "list of strings",
			schema: ir.SchemaIR{
				Collection: &ir.CollectionType{
					Kind:        ir.List,
					ElementType: stringSchema,
				},
			},
			label: "tags",
			want:  `["<tags>"]`,
		},
		{
			name: "list of objects",
			schema: ir.SchemaIR{
				Collection: &ir.CollectionType{
					Kind:        ir.List,
					ElementType: objectSchema,
				},
			},
			label: "owners",
			want:  `[{ name = "<name>", age = 0 }]`,
		},
		{
			name: "map of strings",
			schema: ir.SchemaIR{
				Collection: &ir.CollectionType{
					Kind:        ir.Map,
					ElementType: stringSchema,
				},
			},
			label: "labels",
			want:  `{ "key" = "<labels>" }`,
		},
		{
			name: "map of objects",
			schema: ir.SchemaIR{
				Collection: &ir.CollectionType{
					Kind:        ir.Map,
					ElementType: objectSchema,
				},
			},
			label: "owners",
			want:  `{ "key" = { name = "<name>", age = 0 } }`,
		},
		{
			name:   "object",
			schema: objectSchema,
			label:  "owner",
			want:   `{ name = "<name>", age = 0 }`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := functionArgumentPlaceholder(tc.schema, tc.label)
			if got != tc.want {
				t.Errorf("functionArgumentPlaceholder() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDocsTypeNameFallbacks verifies that docs type-name helpers fall back to
// snake_case when TypeName is empty, so names with spaces or casing produce
// valid Terraform identifiers.
func TestDocsTypeNameFallbacks(t *testing.T) {
	if got := actionDocsTypeName(ir.ActionIR{Name: "Reboot Server"}); got != "reboot_server" {
		t.Errorf("actionDocsTypeName() = %q, want %q", got, "reboot_server")
	}
	if got := ephemeralResourceDocsTypeName(ir.EphemeralResourceIR{Name: "Temporary Credential"}); got != "temporary_credential" {
		t.Errorf("ephemeralResourceDocsTypeName() = %q, want %q", got, "temporary_credential")
	}
	if got := listResourceDocsTypeName(ir.ListResourceIR{Name: "All Pets"}); got != "all_pets" {
		t.Errorf("listResourceDocsTypeName() = %q, want %q", got, "all_pets")
	}
	if got := functionDocsTypeName(ir.FunctionIR{Name: "concat tags"}); got != "concat_tags" {
		t.Errorf("functionDocsTypeName() = %q, want %q", got, "concat_tags")
	}
}

// TestProviderDocsPrefix verifies that providerDocsPrefix extracts the provider
// prefix from conventional <provider>_<resource> type names. When a name has
// no underscore, the full name is returned as a fallback. Names that contain an
// underscore but do not follow the convention (e.g. function names) still split
// on the first underscore, which is the documented limitation.
func TestProviderDocsPrefix(t *testing.T) {
	if got := providerDocsPrefix("mycloud_reboot_server"); got != "mycloud" {
		t.Errorf("providerDocsPrefix(mycloud_reboot_server) = %q, want %q", got, "mycloud")
	}
	if got := providerDocsPrefix("concat_tags"); got != "concat" {
		t.Errorf("providerDocsPrefix(concat_tags) = %q, want %q", got, "concat")
	}
	if got := providerDocsPrefix("mycloud"); got != "mycloud" {
		t.Errorf("providerDocsPrefix(mycloud) = %q, want %q", got, "mycloud")
	}
}
