package generator

import (
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/config"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// TestWriteHCLAcceptanceBlock asserts a nested block renders with indentation,
// and its body recurses through attributes and nested blocks.
func TestWriteHCLAcceptanceBlock(t *testing.T) {
	var h hclBuilder
	block := ir.BlockIR{
		Name: "endpoint",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{{Name: "url", Required: true, Schema: schemaType(ir.TypeString)}},
			Blocks: []ir.BlockIR{{
				Name:        "auth",
				Schema:      ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{Name: "token", Required: true, Schema: schemaType(ir.TypeString)}}},
				NestingMode: ir.NestingList,
			}},
		},
	}
	writeHCLAcceptanceBlock(&h, block)
	got := h.b.String()
	for _, want := range []string{
		"endpoint {",
		`  url = "example"`,
		"  auth {",
		`    token = "example"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered block missing %q\n%s", want, got)
		}
	}
}

// TestAcceptanceTestFiles asserts the top-level aggregator renders one
// acceptance-test file per resource.
func TestAcceptanceTestFiles(t *testing.T) {
	pir := sampleProviderWithResourceIR()
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}
	files := AcceptanceTestFiles(pir, cfg)
	if len(files) != len(pir.Resources) {
		t.Fatalf("AcceptanceTestFiles() = %d files, want %d", len(files), len(pir.Resources))
	}
	var buf strings.Builder
	if err := files[0].Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if !strings.Contains(buf.String(), "func TestAcc") {
		t.Errorf("rendered file missing acceptance test function\n%s", buf.String())
	}
}

// TestWriteTerraformTestProviderBodyAndBlock asserts provider config renders
// required attributes and nested blocks.
func TestWriteTerraformTestProviderBodyAndBlock(t *testing.T) {
	var h hclBuilder
	obj := ir.ObjectSchemaIR{
		Attributes: []ir.AttributeIR{{Name: "region", Required: true, Schema: schemaType(ir.TypeString)}},
		Blocks: []ir.BlockIR{{
			Name: "endpoint",
			Schema: ir.ObjectSchemaIR{
				Attributes: []ir.AttributeIR{{Name: "url", Required: true, Schema: schemaType(ir.TypeString)}},
			},
			NestingMode: ir.NestingSingle,
		}},
	}
	writeTerraformTestProviderBody(&h, obj)
	got := h.b.String()
	for _, want := range []string{
		`region = "example"`,
		"endpoint {",
		`  url = "example"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("provider body missing %q\n%s", want, got)
		}
	}
}

// TestWriteTerraformTestCollectionAttribute covers the list/set primitive,
// map primitive, and fallback branches of the collection renderer.
func TestWriteTerraformTestCollectionAttribute(t *testing.T) {
	cases := []struct {
		name string
		attr ir.AttributeIR
		want string
	}{
		{"list-of-string",
			ir.AttributeIR{Name: "tags", Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: schemaType(ir.TypeString)}}},
			`tags = ["example"]`},
		{"set-of-int",
			ir.AttributeIR{Name: "ids", Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.Set, ElementType: schemaType(ir.TypeInt)}}},
			`ids = [0]`},
		{"map-of-bool",
			ir.AttributeIR{Name: "flags", Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.Map, ElementType: schemaType(ir.TypeBool)}}},
			`flags = {`},
		{"list-of-object",
			ir.AttributeIR{Name: "items", Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Attributes: []ir.AttributeIR{{Name: "x", Schema: schemaType(ir.TypeString)}}}}}},
			`items = [{`},
		{"map-of-object",
			ir.AttributeIR{Name: "objs", Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.Map, ElementType: ir.SchemaIR{Attributes: []ir.AttributeIR{{Name: "x", Schema: schemaType(ir.TypeString)}}}}}},
			`objs = {`},
		// Union element is neither primitive nor object-like → fallback.
		{"union-element",
			ir.AttributeIR{Name: "u", Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Union: &ir.UnionType{}}}}},
			`u = [{}]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var h hclBuilder
			writeTerraformTestCollectionAttribute(&h, tc.attr, false)
			if got := h.b.String(); !strings.Contains(got, tc.want) {
				t.Errorf("rendered %q missing %q\n%s", tc.name, tc.want, got)
			}
		})
	}
}

// TestRenderDocsNestedBlocks asserts the tfplugindocs-style nested-block
// rendering: the block row links to its nested schema, and the nested section
// groups child attributes and deeper blocks under Required/Optional/Read-Only
// subtitles with their own anchors.
func TestRenderDocsNestedBlocks(t *testing.T) {
	minItems := int64(1)
	blocks := []ir.BlockIR{{
		Name:        "endpoint",
		Description: "an endpoint config",
		NestingMode: ir.NestingList,
		MinItems:    &minItems,
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "url", Required: true, Description: "the url", Schema: schemaType(ir.TypeString)},
				{Name: "retries", Optional: true, Schema: schemaType(ir.TypeInt)},
			},
			Blocks: []ir.BlockIR{{
				Name:        "auth",
				NestingMode: ir.NestingSingle,
				Schema:      ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{Name: "token", Computed: true, Schema: schemaType(ir.TypeString)}}},
			}},
		},
	}}

	_, _, section, nested := renderDocsSections(nil, nil, blocks)
	for _, want := range []string{
		"* `endpoint` (Block List, required) - an endpoint config (see [below for nested schema](#nestedblock--endpoint))",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("block row missing %q\n%s", want, section)
		}
	}
	for _, want := range []string{
		"<a id=\"nestedblock--endpoint\"></a>",
		"### Nested Schema for `endpoint`",
		"Required:",
		"* `url` (String) - the url",
		"Optional:",
		"* `retries` (Number)",
		"<a id=\"nestedblock--endpoint--auth\"></a>",
		"### Nested Schema for `endpoint.auth`",
		"Read-Only:",
		"* `token` (String)",
	} {
		if !strings.Contains(nested, want) {
			t.Errorf("nested schema missing %q\n%s", want, nested)
		}
	}
}

// TestProviderFilterFromConfig asserts all six construct families are wired
// from a GenerationConfig, and an empty config produces empty filters.
func TestProviderFilterFromConfig(t *testing.T) {
	cfg := config.GenerationConfig{
		Resources:          config.ResourceGenerationConfig{Include: []string{"pet"}},
		DataSources:        config.ResourceGenerationConfig{Exclude: []string{"stats"}},
		Actions:            config.ResourceGenerationConfig{Include: []string{"reboot"}},
		EphemeralResources: config.ResourceGenerationConfig{Include: []string{"session"}},
		ListResources:      config.ResourceGenerationConfig{Exclude: []string{"old"}},
		Functions:          config.ResourceGenerationConfig{Include: []string{"now"}},
	}
	f := ProviderFilterFromConfig(cfg)
	if len(f.Resources.Include) != 1 || f.Resources.Include[0] != "pet" {
		t.Errorf("resources filter = %+v", f.Resources)
	}
	if len(f.DataSources.Exclude) != 1 || f.DataSources.Exclude[0] != "stats" {
		t.Errorf("data sources filter = %+v", f.DataSources)
	}
	if len(f.Actions.Include) != 1 || f.Actions.Include[0] != "reboot" {
		t.Errorf("actions filter = %+v", f.Actions)
	}
	if len(f.EphemeralResources.Include) != 1 || f.EphemeralResources.Include[0] != "session" {
		t.Errorf("ephemeral resources filter = %+v", f.EphemeralResources)
	}
	if len(f.ListResources.Exclude) != 1 || f.ListResources.Exclude[0] != "old" {
		t.Errorf("list resources filter = %+v", f.ListResources)
	}
	if len(f.Functions.Include) != 1 || f.Functions.Include[0] != "now" {
		t.Errorf("functions filter = %+v", f.Functions)
	}

	empty := ProviderFilterFromConfig(config.GenerationConfig{})
	if len(empty.Resources.Include)+len(empty.Resources.Exclude) != 0 {
		t.Errorf("empty config should produce empty resource filter, got %+v", empty.Resources)
	}
}

// TestConstructFilterMatches_Branches drives matches through include matches,
// include non-match, exclude match, and the empty-filter all-match path.
func TestConstructFilterMatches_Branches(t *testing.T) {
	if !(ConstructFilter{Include: []string{"pet*"}}).matches("pet_store") {
		t.Error("include pattern should match")
	}
	if (ConstructFilter{Include: []string{"pet*"}}).matches("owner") {
		t.Error("include non-match should be rejected")
	}
	if (ConstructFilter{Exclude: []string{"admin*"}}).matches("admin_user") {
		t.Error("exclude match should be rejected")
	}
	if !(ConstructFilter{}).matches("anything") {
		t.Error("empty filter should match everything")
	}
	// A malformed include pattern never matches, silently filtering out the
	// whole family (the defensive fallback that Validate exists to prevent).
	if (ConstructFilter{Include: []string{"["}}).matches("x") {
		t.Error("malformed include pattern should fall back to a non-match")
	}
}

// TestFilterFamilyHelpers asserts each construct-family filter retains only
// matching items.
func TestFilterFamilyHelpers(t *testing.T) {
	f := ConstructFilter{Include: []string{"keep*"}}

	actions := filterActions(
		[]ir.ActionIR{{Name: "keep_going"}, {Name: "drop"}}, f)
	if len(actions) != 1 || actions[0].Name != "keep_going" {
		t.Errorf("filterActions = %+v", actions)
	}

	ephs := filterEphemeralResources(
		[]ir.EphemeralResourceIR{{Name: "keep_session"}, {Name: "drop"}}, f)
	if len(ephs) != 1 || ephs[0].Name != "keep_session" {
		t.Errorf("filterEphemeralResources = %+v", ephs)
	}

	lists := filterListResources(
		[]ir.ListResourceIR{{Name: "keep_all"}, {Name: "drop"}}, f)
	if len(lists) != 1 || lists[0].Name != "keep_all" {
		t.Errorf("filterListResources = %+v", lists)
	}

	fns := filterFunctions(
		[]ir.FunctionIR{{Name: "keep_now"}, {Name: "drop"}}, f)
	if len(fns) != 1 || fns[0].Name != "keep_now" {
		t.Errorf("filterFunctions = %+v", fns)
	}
}
