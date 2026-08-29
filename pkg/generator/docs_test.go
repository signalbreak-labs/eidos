package generator

import (
	"bytes"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// TestProviderDocsIndex_Render verifies that ProviderDocsIndex emits the
// expected frontmatter, provider name, and resource/data source links.
func TestProviderDocsIndex_Render(t *testing.T) {
	pir := sampleProviderIR()

	file := ProviderDocsIndex(pir)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"---",
		`page_title: "mycloud Provider"`,
		`subcategory: ""`,
		"description: |-",
		"Generated provider for MyCloud.",
		"# mycloud Provider",
		"## Resources",
		"- [mycloud_pet](resources/pet.md)",
		"## Data Sources",
		"- [mycloud_pets](data-sources/pets.md)",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated index.md missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestResourceDocsFile_Render verifies that ResourceDocsFile emits correct
// frontmatter, schema sections, and an import block for an importable resource.
func TestResourceDocsFile_Render(t *testing.T) {
	r := sampleResourceIR()

	file := ResourceDocsFile(r)
	if file.Path != "docs/resources/pet.md" {
		t.Errorf("file.Path = %q, want %q", file.Path, "docs/resources/pet.md")
	}

	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"---",
		`page_title: "mycloud_pet Resource - mycloud"`,
		`subcategory: ""`,
		"description: |-",
		"A pet resource.",
		"# mycloud_pet Resource",
		"## Example Usage",
		`resource "mycloud_pet" "example" {`,
		"  name = \"example\"",
		"  tag  = \"example\"",
		"  age  = 0",
		"  tags = [ \"example\" ]",
		"  owner = {",
		"    email = \"example\"",
		"  }",
		"## Schema",
		"### Arguments",
		"* `name` (String, required)",
		"* `tag` (String, optional)",
		"* `age` (Number, optional)",
		"* `tags` (List of String, optional)",
		"* `owner` (Attributes, optional) (see [below for nested schema](#nestedatt--owner))",
		"### Attributes",
		"* `id` (String, computed)",
		"### Nested Schema for `owner`",
		"* `email` (String)",
		"## Import",
		"terraform import mycloud_pet.example <id>",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated resource docs missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestDataSourceDocsFile_Render verifies that DataSourceDocsFile emits correct
// frontmatter and schema sections for a data source.
func TestDataSourceDocsFile_Render(t *testing.T) {
	ds := sampleDataSourceIR()

	file := DataSourceDocsFile(ds)
	if file.Path != "docs/data-sources/pets.md" {
		t.Errorf("file.Path = %q, want %q", file.Path, "docs/data-sources/pets.md")
	}

	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"---",
		`page_title: "mycloud_pets Data Source - mycloud"`,
		`subcategory: ""`,
		"description: |-",
		"Fetches a list of pets.",
		"# mycloud_pets Data Source",
		"## Example Usage",
		`data "mycloud_pets" "example" {`,
		"## Schema",
		"### Arguments",
		"* `id` (String, required)",
		"### Attributes",
		"* `name` (String, computed)",
		"* `tags` (List of String, computed)",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated data source docs missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestProviderDocsFiles_Multiple verifies that ProviderDocsFiles emits the
// expected set of paths for a provider with resources and data sources.
func TestProviderDocsFiles_Multiple(t *testing.T) {
	pir := ir.ProviderIR{
		Name:        "mycloud",
		TypeName:    "mycloud",
		Description: "Generated provider for MyCloud.",
		Resources: []ir.ResourceIR{
			{Name: "pet", TypeName: "mycloud_pet"},
			{Name: "owner", TypeName: "mycloud_owner"},
		},
		DataSources: []ir.DataSourceIR{
			{Name: "pets", TypeName: "mycloud_pets"},
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
		"docs/resources/pet.md",
		"docs/resources/owner.md",
		"docs/data-sources/pets.md",
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

// TestResourceDocsFile_ImportFormat verifies that a configured ImportIDFormat is
// rendered in the import section.
func TestResourceDocsFile_ImportFormat(t *testing.T) {
	r := ir.ResourceIR{
		Name:           "pet",
		TypeName:       "mycloud_pet",
		Description:    "A pet resource.",
		Importable:     true,
		ImportIDFormat: "{project_id}/{pet_id}",
		// An import is only emitted for a wired resource (a scaffolded Read
		// always fails, so the import could never succeed) (G39).
		CRUDMapping: ir.CRUDMappingIR{
			Create: ir.OperationMappingIR{Method: "POST", PathTemplate: "/pets"},
			Read:   ir.OperationMappingIR{Method: "GET", PathTemplate: "/pets/{pet_id}"},
			Delete: ir.OperationMappingIR{Method: "DELETE", PathTemplate: "/pets/{pet_id}"},
		},
		Schema: ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{
			{Name: "project_id", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			{Name: "pet_id", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		}},
	}

	file := ResourceDocsFile(r)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, "terraform import mycloud_pet.example {project_id}/{pet_id}") {
		t.Errorf("generated resource docs missing custom import format\ncontent:\n%s", got)
	}
}

// TestResourceDocsFile_NoImport verifies that non-importable resources do not
// include an Import section.
func TestResourceDocsFile_NoImport(t *testing.T) {
	r := ir.ResourceIR{
		Name:        "pet",
		TypeName:    "mycloud_pet",
		Description: "A pet resource.",
		Importable:  false,
	}

	file := ResourceDocsFile(r)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if strings.Contains(got, "## Import") {
		t.Errorf("non-importable resource should not contain Import section\ncontent:\n%s", got)
	}
}

// TestDataSourceDocsFile_NestedAttributes verifies that nested object
// attributes are rendered inside data source documentation.
func TestDataSourceDocsFile_NestedAttributes(t *testing.T) {
	ds := ir.DataSourceIR{
		Name:        "pet",
		TypeName:    "mycloud_pet",
		Description: "A pet data source.",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "owner",
					Computed: true,
					Schema: ir.SchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "name", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				},
			},
		},
	}

	file := DataSourceDocsFile(ds)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, "* `owner` (Attributes, computed) (see [below for nested schema](#nestedatt--owner))") {
		t.Errorf("generated data source docs missing nested owner attribute\ncontent:\n%s", got)
	}
	if !strings.Contains(got, "### Nested Schema for `owner`") || !strings.Contains(got, "* `name` (String)") {
		t.Errorf("generated data source docs missing nested name row\ncontent:\n%s", got)
	}
}

// TestRenderExampleArguments_CollectionsAndObjects verifies that collection and
// object attributes are emitted as populated literals in the example usage
// section instead of null/empty placeholders or comment stubs.
func TestRenderExampleArguments_CollectionsAndObjects(t *testing.T) {
	attrs := []ir.AttributeIR{
		{
			Name:     "name",
			Required: true,
			Schema:   ir.SchemaIR{Type: ir.TypeString},
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
			Name:     "metadata",
			Optional: true,
			Schema: ir.SchemaIR{
				Collection: &ir.CollectionType{
					Kind:        ir.Map,
					ElementType: ir.SchemaIR{Type: ir.TypeString},
				},
			},
		},
		{
			Name:     "owner",
			Optional: true,
			Schema: ir.SchemaIR{
				Attributes: []ir.AttributeIR{
					{Name: "email", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
				},
			},
		},
	}

	got := renderExampleArguments(attrs)
	want := []string{
		"  name = \"example\"",
		"  tags = [ \"example\" ]",
		"  metadata = {",
		`    "metadata" = "example"`,
		"  }",
		"  owner = {",
		"    email = \"example\"",
		"  }",
	}
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("renderExampleArguments() missing %q\ngot:\n%s", w, got)
		}
	}
	if strings.Contains(got, "# tags") || strings.Contains(got, "# owner") || strings.Contains(got, "# metadata") {
		t.Errorf("renderExampleArguments() should not emit comment stubs for collections/objects\ngot:\n%s", got)
	}
}

// TestSchemaTypeName_DynamicFallback verifies that an unrecognized primitive
// type (e.g. TypeNull, or an empty type that the schema renderer degrades to a
// DynamicAttribute) is surfaced as "Dynamic" — matching the attribute the
// generator actually emits — rather than silently defaulting to "String" or the
// opaque "Unknown".
func TestSchemaTypeName_DynamicFallback(t *testing.T) {
	got := schemaTypeName(ir.SchemaIR{Type: ir.TypeNull})
	if got != "Dynamic" {
		t.Errorf("schemaTypeName(TypeNull) = %q, want %q", got, "Dynamic")
	}
	// An empty type with no collection/union/object shape also renders as a
	// DynamicAttribute, so its docs label must be "Dynamic" too.
	if got := schemaTypeName(ir.SchemaIR{}); got != "Dynamic" {
		t.Errorf("schemaTypeName(empty) = %q, want %q", got, "Dynamic")
	}
}

// TestEscapeDescription_MarkdownSpecialChars verifies that markdown and YAML
// sensitive characters are escaped so they do not break frontmatter or bullet
// rendering.
func TestEscapeDescription_MarkdownSpecialChars(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Note: something", "Note: something"},
		{"Has # heading", "Has \\# heading"},
		{"Use *bold*", "Use \\*bold\\*"},
		{"Use _italic_", "Use \\_italic\\_"},
		{"See [link]", "See \\[link\\]"},
		{"Run `cmd`", "Run \\`cmd\\`"},
		{"A | B", "A \\| B"},
		{"Multi\nline", "Multi line"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := escapeDescription(tc.in)
			if got != tc.want {
				t.Errorf("escapeDescription(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestEscapeDescription_ReEscapesPrecededSpecialChars verifies that
// escapeDescription does not recognize or preserve existing backslash escapes:
// it re-escapes every special character regardless of a preceding backslash.
// So an input like `\*` (already escaped) becomes `\\*` (the original backslash
// plus the freshly escaped `*`). The prior name "PreservesExistingEscapes" was
// misleading because the function does the opposite (L-55).
func TestEscapeDescription_ReEscapesPrecededSpecialChars(t *testing.T) {
	got := escapeDescription(`already \*escaped\*`)
	want := `already \\*escaped\\*`
	if got != want {
		t.Errorf("escapeDescription() = %q, want %q", got, want)
	}
}

func TestCapitalize(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"pet", "Pet"},
		{"pets", "Pets"},
		{"already Upper", "Already Upper"},
	}
	for _, tc := range cases {
		if got := capitalize(tc.in); got != tc.want {
			t.Errorf("capitalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBlockNestingLabel(t *testing.T) {
	cases := []struct {
		mode ir.BlockNestingMode
		want string
	}{
		{ir.NestingList, "List"},
		{ir.NestingSet, "Set"},
		{ir.NestingSingle, "Single"},
	}
	for _, tc := range cases {
		block := ir.BlockIR{NestingMode: tc.mode}
		if got := blockNestingLabel(block); got != tc.want {
			t.Errorf("blockNestingLabel(%v) = %q, want %q", tc.mode, got, tc.want)
		}
	}
}

func TestBlockQualifier(t *testing.T) {
	minOne := int64(1)
	zero := int64(0)
	cases := []struct {
		block ir.BlockIR
		want  string
	}{
		{ir.BlockIR{MinItems: &minOne}, "required"},
		{ir.BlockIR{MinItems: &zero}, "optional"},
		{ir.BlockIR{}, "optional"},
	}
	for _, tc := range cases {
		if got := blockQualifier(tc.block); got != tc.want {
			t.Errorf("blockQualifier(%+v) = %q, want %q", tc.block, got, tc.want)
		}
	}
}

// TestResourceDocsFile_NestedSchemaGroupHeadersNotGlued locks in the nested-
// schema markdown fix: a Required:/Optional:/Read-Only: group header (and every
// <a id> anchor) must start its own block, separated from the previous group's
// last bullet by a blank line. Without it, CommonMark parses the header as a
// lazy continuation of the bullet and the Registry renders it glued to the
// bullet text (e.g. "type (String) - Alert clear type Optional:") — the
// alert_policy report (G39).
func TestResourceDocsFile_NestedSchemaGroupHeadersNotGlued(t *testing.T) {
	r := ir.ResourceIR{
		Name:        "alert_policy",
		TypeName:    "mycloud_alert_policy",
		Description: "An alert policy resource.",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "clear_condition",
					Required: true,
					Schema: ir.SchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "type", Required: true, Description: "Alert clear type", Schema: ir.SchemaIR{Type: ir.TypeString}},
							{Name: "threshold", Optional: true, Description: "Alert clear threshold definition", Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				},
			},
		},
	}

	file := ResourceDocsFile(r)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, glued := range []string{
		"Alert clear type\nOptional:",
		"Alert clear type Optional:",
		"threshold definition\n<a id=",
	} {
		if strings.Contains(got, glued) {
			t.Errorf("nested schema group header glued to the previous bullet (%q)\ncontent:\n%s", glued, got)
		}
	}
	if !strings.Contains(got, "Alert clear type\n\nOptional:") {
		t.Errorf("Optional: group header must be separated by a blank line\ncontent:\n%s", got)
	}
}

// TestRenderDocsSections_OptionalComputedNotDuplicated locks in the G39 fix:
// an Optional+Computed attribute is an argument the server also echoes, so it
// is listed under Arguments (as optional) only — never duplicated under
// Attributes. The Attributes section is reserved for Computed-only attributes.
func TestRenderDocsSections_OptionalComputedNotDuplicated(t *testing.T) {
	attrs := []ir.AttributeIR{
		{Name: "name", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		{Name: "color", Optional: true, Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		{Name: "id", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
	}

	arguments, attributes, _, _ := renderDocsSections(attrs, attrs, nil)

	if !strings.Contains(arguments, "* `color` (String, optional)") {
		t.Errorf("Optional+Computed attribute must be listed under Arguments\narguments:\n%s", arguments)
	}
	if strings.Contains(attributes, "`color`") {
		t.Errorf("Optional+Computed attribute must not be duplicated under Attributes\nattributes:\n%s", attributes)
	}
	if !strings.Contains(attributes, "* `id` (String, computed)") {
		t.Errorf("Computed-only attribute must stay under Attributes\nattributes:\n%s", attributes)
	}
	if !strings.Contains(arguments, "* `name` (String, required)") {
		t.Errorf("Required attribute missing from Arguments\narguments:\n%s", arguments)
	}
}

// TestResourceDocsFile_TimeoutsSection locks in the G39 fix: a resource with
// configured CRUD timeouts documents its `timeouts` block — a Blocks-section
// row linking to a "### Nested Schema for `timeouts`" section listing each
// configured operation as an optional string — instead of the block being
// silently absent from the docs.
func TestResourceDocsFile_TimeoutsSection(t *testing.T) {
	r := sampleResourceIR()
	create := 20 * time.Minute
	r.Timeouts = &ir.TimeoutConfigIR{Create: &create, Delete: &create}

	file := ResourceDocsFile(r)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		"* `timeouts` (Block Single) (see [below for nested schema](#nestedatt--timeouts))",
		"### Nested Schema for `timeouts`",
		"* `create` (String) - A create timeout for this operation",
		"* `delete` (String) - A delete timeout for this operation",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("timeouts documentation missing %q\ncontent:\n%s", want, got)
		}
	}
	// Read was not configured: it must not be documented.
	if strings.Contains(got, "* `read` (String)") {
		t.Errorf("unconfigured read timeout must not be documented\ncontent:\n%s", got)
	}

	// A resource without configured timeouts must not grow a timeouts section.
	plain := ResourceDocsFile(sampleResourceIR())
	buf.Reset()
	if err := plain.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if strings.Contains(buf.String(), "timeouts") {
		t.Errorf("resource without configured timeouts must not document a timeouts block\ncontent:\n%s", buf.String())
	}
}
