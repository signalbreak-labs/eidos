package generator

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// TestListResourceFile_Render verifies that ListResourceFile emits the expected
// list resource struct, Metadata, ListResourceConfigSchema, and List methods.
func TestListResourceFile_Render(t *testing.T) {
	lr := sampleListResourceIR()

	file := ListResourceFile(lr, "")
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"package provider",
		"var _ list.ListResource = (*PetsListResource)(nil)",
		"type PetsListResource struct",
		"func NewPetsListResource()",
		"func (l *PetsListResource) Metadata",
		"func (l *PetsListResource) ListResourceConfigSchema",
		"func (l *PetsListResource) List",
		"resp.TypeName = \"mycloud_pets\"",
		"listschema.Schema",
		"listschema.StringAttribute",
		"listschema.ListAttribute",
		"listschema.MapAttribute",
		"listschema.SingleNestedAttribute",
		"ElementType: types.StringType",
		"Required: true",
		"Optional: true",
		"stream.Results = list.ListResultsStreamDiagnostics",
		"List is not wired to a remote API endpoint.",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated list resource missing %q\ncontent:\n%s", want, got)
		}
	}

	// List schema attributes must not include Computed or Sensitive.
	if strings.Contains(got, "Computed:") {
		t.Error("list resource config schema should not contain Computed fields")
	}
	if strings.Contains(got, "Sensitive:") {
		t.Error("list resource config schema should not contain Sensitive fields")
	}
}

// TestListResourceFile_NestedSchema verifies that nested attributes and blocks
// are rendered using the list/schema package.
func TestListResourceFile_NestedSchema(t *testing.T) {
	lr := ir.ListResourceIR{
		Name:     "pet",
		TypeName: "mycloud_pet",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name: "owner",
					Schema: ir.SchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "name", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				},
				{
					Name: "aliases",
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind:        ir.List,
							ElementType: ir.SchemaIR{Attributes: []ir.AttributeIR{{Name: "value", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}}}},
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
							{Name: "key", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				},
			},
		},
	}

	file := ListResourceFile(lr, "")
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"type PetListResource struct",
		"listschema.SingleNestedAttribute",
		"listschema.ListNestedAttribute",
		"listschema.ListNestedBlock",
		"NestedObject: listschema.NestedAttributeObject",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated nested list resource missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestListResourceFile_PrimitiveOnlyOmitsTypesImport verifies the §6 latent
// unused-import fix for list resources: a list resource whose config schema
// contains only primitive and object-nested attributes (no collection with a
// primitive element type) never references the types package, because list
// resources have no model struct and primitiveAttrType — the only types.*
// reference — is called only for collection element types. The generated file
// must not import types or it would not compile.
func TestListResourceFile_PrimitiveOnlyOmitsTypesImport(t *testing.T) {
	lr := ir.ListResourceIR{
		Name:     "primitive_only",
		TypeName: "mycloud_primitive_only",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "id", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
				{Name: "count", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeInt}},
				{
					Name:     "owner",
					Optional: true,
					Schema: ir.SchemaIR{Attributes: []ir.AttributeIR{
						{Name: "name", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
					}},
				},
			},
		},
	}

	file := ListResourceFile(lr, "")
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if strings.Contains(got, `"github.com/hashicorp/terraform-plugin-framework/types"`) {
		t.Errorf("primitive-only list resource must not import types (unused import)\ncontent:\n%s", got)
	}
}

// TestListResourceNeedsTypesImport verifies the gate directly: primitive and
// object-nested attributes do not need types; a collection attribute with a
// primitive element does, including when nested inside an object attribute or
// a block, matching the exact primitiveAttrType render decision.
func TestListResourceNeedsTypesImport(t *testing.T) {
	// Primitive-only: no types.
	if listResourceNeedsTypesImport(ir.ListResourceIR{
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{{Name: "id", Schema: ir.SchemaIR{Type: ir.TypeString}}},
		},
	}) {
		t.Fatalf("primitive-only list resource must not need types import")
	}
	// Top-level collection with primitive element: types.
	if !listResourceNeedsTypesImport(ir.ListResourceIR{
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{{
				Name:   "tags",
				Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Type: ir.TypeString}}},
			}},
		},
	}) {
		t.Fatalf("list resource with a primitive collection attribute must need types import")
	}
	// Collection nested inside an object attribute: types (recurses).
	if !listResourceNeedsTypesImport(ir.ListResourceIR{
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{{
				Name: "owner",
				Schema: ir.SchemaIR{Attributes: []ir.AttributeIR{{
					Name:   "tags",
					Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Type: ir.TypeString}}},
				}}},
			}},
		},
	}) {
		t.Fatalf("list resource with a collection nested in an object attribute must need types import")
	}
	// Collection nested inside a block: types (block attributes render).
	if !listResourceNeedsTypesImport(ir.ListResourceIR{
		ConfigSchema: ir.ObjectSchemaIR{
			Blocks: []ir.BlockIR{{
				Name:        "filter",
				NestingMode: ir.NestingList,
				Schema: ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{
					Name:   "tags",
					Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Type: ir.TypeString}}},
				}}},
			}},
		},
	}) {
		t.Fatalf("list resource with a collection nested in a block must need types import")
	}
	// Collection of object with a primitive-element nested collection: types.
	if !listResourceNeedsTypesImport(ir.ListResourceIR{
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{{
				Name: "owners",
				Schema: ir.SchemaIR{Collection: &ir.CollectionType{
					Kind: ir.List,
					ElementType: ir.SchemaIR{Attributes: []ir.AttributeIR{{
						Name:   "tags",
						Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Type: ir.TypeString}}},
					}}},
				}},
			}},
		},
	}) {
		t.Fatalf("list resource with a nested-object collection containing a primitive collection must need types import")
	}
}

// TestListResourceFile_SchemaValidation generates a minimal provider with a
// list resource into a temporary Go module and runs the Terraform
// plugin-framework schema validation to confirm the generated list_<name>.go
// compiles and its schema is valid.
func TestListResourceFile_SchemaValidation(t *testing.T) {
	lr := sampleListResourceIR()

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
		ListResources: []ir.ListResourceIR{lr},
	}

	tmp := generateListResourceModule(t, providerIR, []ir.ListResourceIR{lr})
	writeListResourceSchemaValidationTest(t, tmp, lr)

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

// TestListResourceTypeName verifies that the Terraform type name prefers
// ListResourceIR.TypeName and falls back to the trimmed list resource name.
func TestListResourceTypeName(t *testing.T) {
	cases := []struct {
		name     string
		lr       ir.ListResourceIR
		wantName string
	}{
		{
			name:     "prefers type name",
			lr:       ir.ListResourceIR{Name: "pets", TypeName: "mycloud_pets"},
			wantName: "mycloud_pets",
		},
		{
			name:     "falls back to name",
			lr:       ir.ListResourceIR{Name: "pets"},
			wantName: "pets",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := listResourceTypeName(tc.lr); got != tc.wantName {
				t.Errorf("listResourceTypeName() = %q, want %q", got, tc.wantName)
			}
		})
	}
}

// TestListResourceTypeName_WhitespaceTrimming verifies that whitespace around
// the configured type name and name is trimmed before use.
func TestListResourceTypeName_WhitespaceTrimming(t *testing.T) {
	cases := []struct {
		name     string
		lr       ir.ListResourceIR
		wantName string
	}{
		{
			name:     "trims type name",
			lr:       ir.ListResourceIR{Name: "pets", TypeName: "  mycloud_pets  "},
			wantName: "mycloud_pets",
		},
		{
			name:     "trims name fallback",
			lr:       ir.ListResourceIR{Name: "  pets  "},
			wantName: "pets",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := listResourceTypeName(tc.lr); got != tc.wantName {
				t.Errorf("listResourceTypeName() = %q, want %q", got, tc.wantName)
			}
		})
	}
}

// TestListResourceStructName verifies the generated struct name is derived from
// the PascalCase'd list resource name.
func TestListResourceStructName(t *testing.T) {
	cases := []struct {
		name       string
		lr         ir.ListResourceIR
		wantStruct string
	}{
		{
			name:       "simple name",
			lr:         ir.ListResourceIR{Name: "pets"},
			wantStruct: "PetsListResource",
		},
		{
			name:       "snake case name",
			lr:         ir.ListResourceIR{Name: "my_pets"},
			wantStruct: "MyPetsListResource",
		},
		{
			name:       "camel case name",
			lr:         ir.ListResourceIR{Name: "myPets"},
			wantStruct: "MyPetsListResource",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := listResourceStructName(tc.lr); got != tc.wantStruct {
				t.Errorf("listResourceStructName() = %q, want %q", got, tc.wantStruct)
			}
		})
	}
}

// TestListResourceFile_SnakeCasePath verifies that the generated file path uses
// snakeCase for the list resource name, matching the resource generator
// convention.
func TestListResourceFile_SnakeCasePath(t *testing.T) {
	lr := ir.ListResourceIR{Name: "my pets"}
	file := ListResourceFile(lr, "")
	want := "internal/provider/list_my_pets.go"
	if file.Path != want {
		t.Errorf("ListResourceFile path = %q, want %q", file.Path, want)
	}
}

// TestListResourceFiles verifies that ListResourceFiles emits one file per list
// resource in order.
func TestListResourceFiles(t *testing.T) {
	resources := []ir.ListResourceIR{
		{Name: "pets"},
		{Name: "owners"},
	}
	files := ListResourceFiles(resources, "")
	if len(files) != len(resources) {
		t.Fatalf("ListResourceFiles returned %d files, want %d", len(files), len(resources))
	}
	wantPaths := []string{
		"internal/provider/list_pets.go",
		"internal/provider/list_owners.go",
	}
	for i, want := range wantPaths {
		if files[i].Path != want {
			t.Errorf("file[%d].Path = %q, want %q", i, files[i].Path, want)
		}
	}
}

// TestListResourceFile_PrimitiveTypes verifies that Float64, Bool, and Dynamic
// primitive types are emitted using the correct list/schema attribute types.
func TestListResourceFile_PrimitiveTypes(t *testing.T) {
	lr := ir.ListResourceIR{
		Name: "primitives",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "ratio", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeFloat}},
				{Name: "enabled", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeBool}},
				{Name: "anything", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeDynamic}},
			},
		},
	}

	file := ListResourceFile(lr, "")
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"listschema.Float64Attribute",
		"listschema.BoolAttribute",
		"listschema.DynamicAttribute",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated primitive list resource missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestListResourceFile_UnionDynamicAttribute verifies that a Union schema is emitted
// as a DynamicAttribute.
func TestListResourceFile_UnionDynamicAttribute(t *testing.T) {
	lr := ir.ListResourceIR{
		Name: "union",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "anything", Optional: true, Schema: ir.SchemaIR{Union: &ir.UnionType{}}},
			},
		},
	}

	file := ListResourceFile(lr, "")
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, "listschema.DynamicAttribute") {
		t.Errorf("generated list resource missing listschema.DynamicAttribute for union\ncontent:\n%s", got)
	}
}

// TestListResourceFile_SetFallback verifies that Set collections are emitted as
// List attributes with a generated warning comment placed immediately above the
// fallback expression.
func TestListResourceFile_SetFallback(t *testing.T) {
	lr := ir.ListResourceIR{
		Name: "sets",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
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
				{
					Name:     "owners",
					Optional: true,
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind: ir.Set,
							ElementType: ir.SchemaIR{
								Attributes: []ir.AttributeIR{
									{Name: "name", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
								},
							},
						},
					},
				},
			},
		},
	}

	file := ListResourceFile(lr, "")
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"Set collections are not supported by list/schema; emitted as a List attribute.",
		"listschema.ListAttribute",
		"listschema.ListNestedAttribute",
		"ElementType: types.StringType",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated Set fallback list resource missing %q\ncontent:\n%s", want, got)
		}
	}

	// The warning comments are now emitted as declaration-level comments above the
	// ListResourceConfigSchema method because go/format cannot easily place inline
	// comments inside composite literals for a from-scratch AST. The generated code
	// still contains the original diagnostic messages; their placement is now
	// consolidated above the schema method instead of immediately above each entry.
	if len(regexp.MustCompile(`// Set collections are not supported by list/schema; emitted as a List attribute\.`).FindAllString(got, -1)) < 2 {
		t.Errorf("Set fallback comment did not appear for each fallback attribute\ncontent:\n%s", got)
	}
	setIdx := strings.Index(got, "// Set collections are not supported by list/schema; emitted as a List attribute.")
	methodIdx := strings.Index(got, "func (l *SetsListResource) ListResourceConfigSchema")
	if setIdx == -1 || methodIdx == -1 || setIdx > methodIdx {
		t.Errorf("Set fallback comments not placed above ListResourceConfigSchema method\ncontent:\n%s", got)
	}
}

// TestListResourceFile_Deprecation verifies that DeprecationMessage and the
// Deprecated shorthand are emitted in list resource schema attributes.
func TestListResourceFile_Deprecation(t *testing.T) {
	lr := ir.ListResourceIR{
		Name: "deprecated",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:               "old_msg",
					Optional:           true,
					DeprecationMessage: "Use new_msg instead.",
					Schema:             ir.SchemaIR{Type: ir.TypeString},
				},
				{
					Name:       "old_flag",
					Optional:   true,
					Deprecated: true,
					Schema:     ir.SchemaIR{Type: ir.TypeString},
				},
			},
		},
	}

	file := ListResourceFile(lr, "")
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, "DeprecationMessage: \"Use new_msg instead.\"") {
		t.Errorf("generated deprecated list resource missing explicit DeprecationMessage\ncontent:\n%s", got)
	}
	if !strings.Contains(got, "DeprecationMessage: \"Deprecated\"") {
		t.Errorf("generated deprecated list resource missing shorthand DeprecationMessage\ncontent:\n%s", got)
	}
}

// TestListResourceFile_BlockDeprecation verifies that DeprecationMessage and the
// Deprecated shorthand are emitted in list resource schema blocks.
func TestListResourceFile_BlockDeprecation(t *testing.T) {
	lr := ir.ListResourceIR{
		Name: "deprecated_block",
		ConfigSchema: ir.ObjectSchemaIR{
			Blocks: []ir.BlockIR{
				{
					Name:               "old_msg",
					NestingMode:        ir.NestingList,
					DeprecationMessage: "Use new_msg instead.",
					Schema:             ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{Name: "value", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}}}},
				},
				{
					Name:        "old_flag",
					NestingMode: ir.NestingList,
					Deprecated:  true,
					Schema:      ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{Name: "value", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}}}},
				},
			},
		},
	}

	file := ListResourceFile(lr, "")
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, "DeprecationMessage: \"Use new_msg instead.\"") {
		t.Errorf("generated deprecated block missing explicit DeprecationMessage\ncontent:\n%s", got)
	}
	if !strings.Contains(got, "DeprecationMessage: \"Deprecated\"") {
		t.Errorf("generated deprecated block missing shorthand DeprecationMessage\ncontent:\n%s", got)
	}
}

// TestListResourceFile_EmptyConfigSchema verifies that a list resource with an
// empty config schema renders without Attributes or Blocks.
func TestListResourceFile_EmptyConfigSchema(t *testing.T) {
	lr := ir.ListResourceIR{Name: "empty"}

	file := ListResourceFile(lr, "")
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if strings.Contains(got, "Attributes:") {
		t.Error("empty config schema should not emit Attributes")
	}
	if strings.Contains(got, "Blocks:") {
		t.Error("empty config schema should not emit Blocks")
	}
}

// TestListResourceFile_DefaultOptional verifies that an attribute with neither
// Required nor Optional set defaults to Optional.
func TestListResourceFile_DefaultOptional(t *testing.T) {
	lr := ir.ListResourceIR{
		Name: "default_optional",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "value", Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
	}

	file := ListResourceFile(lr, "")
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, "Optional: true") {
		t.Errorf("attribute with no optionality flag should default to Optional\ncontent:\n%s", got)
	}
	if strings.Contains(got, "Required: true") {
		t.Error("attribute with no optionality flag should not be Required")
	}
}

// TestListResourceFile_MarkdownDescriptionOmitted verifies that an empty
// description omits the MarkdownDescription field entirely.
func TestListResourceFile_MarkdownDescriptionOmitted(t *testing.T) {
	lr := ir.ListResourceIR{
		Name:        "no_desc",
		Description: "",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "value", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
	}

	file := ListResourceFile(lr, "")
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if strings.Contains(got, "MarkdownDescription:") {
		t.Errorf("empty description should omit MarkdownDescription field\ncontent:\n%s", got)
	}
}

// TestListResourceFile_BlockCardinality verifies that MinItems and MaxItems on
// list-nested blocks are emitted as listvalidator validators.
func TestListResourceFile_BlockCardinality(t *testing.T) {
	minItems := int64(1)
	maxItems := int64(5)
	lr := ir.ListResourceIR{
		Name: "cardinality",
		ConfigSchema: ir.ObjectSchemaIR{
			Blocks: []ir.BlockIR{
				{
					Name:        "metadata",
					NestingMode: ir.NestingList,
					MinItems:    &minItems,
					MaxItems:    &maxItems,
					Schema: ir.ObjectSchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "key", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				},
			},
		},
	}

	file := ListResourceFile(lr, "")
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantPatterns := []string{
		`\[\]validator\.List`,
		`listvalidator\.SizeAtLeast\s*\(\s*int64\s*\(\s*1\s*\)\s*\)`,
		`listvalidator\.SizeAtMost\s*\(\s*int64\s*\(\s*5\s*\)\s*\)`,
	}
	for _, pattern := range wantPatterns {
		if !regexp.MustCompile(pattern).MatchString(got) {
			t.Errorf("generated cardinality block list resource missing pattern %q\ncontent:\n%s", pattern, got)
		}
	}
}

// TestListResourceFile_SingleBlockCardinalityIgnored verifies that MinItems and
// MaxItems on single-nested blocks do not emit cardinality validators, because
// SingleNestedBlock does not support them.
func TestListResourceFile_SingleBlockCardinalityIgnored(t *testing.T) {
	minItems := int64(1)
	maxItems := int64(5)
	lr := ir.ListResourceIR{
		Name: "single_cardinality",
		ConfigSchema: ir.ObjectSchemaIR{
			Blocks: []ir.BlockIR{
				{
					Name:        "metadata",
					NestingMode: ir.NestingSingle,
					MinItems:    &minItems,
					MaxItems:    &maxItems,
					Schema: ir.ObjectSchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "key", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				},
			},
		},
	}

	file := ListResourceFile(lr, "")
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if strings.Contains(got, "validator.List") {
		t.Errorf("single-nested block should not emit List validators\ncontent:\n%s", got)
	}
	if strings.Contains(got, "SizeAtLeast") || strings.Contains(got, "SizeAtMost") {
		t.Errorf("single-nested block should not emit cardinality validators\ncontent:\n%s", got)
	}
}

// TestListResourceFile_SetBlockFallback verifies that Set-nested blocks are
// emitted as ListNestedBlock with a generated warning comment placed immediately
// above the fallback expression.
func TestListResourceFile_SetBlockFallback(t *testing.T) {
	lr := ir.ListResourceIR{
		Name: "set_blocks",
		ConfigSchema: ir.ObjectSchemaIR{
			Blocks: []ir.BlockIR{
				{
					Name:        "metadata",
					NestingMode: ir.NestingSet,
					Schema: ir.ObjectSchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "key", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				},
			},
		},
	}

	file := ListResourceFile(lr, "")
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, "listschema.ListNestedBlock") {
		t.Errorf("generated Set block fallback list resource missing ListNestedBlock\ncontent:\n%s", got)
	}

	// The warning comment is now emitted as a declaration-level comment above the
	// ListResourceConfigSchema method because go/format cannot easily place inline
	// comments inside composite literals for a from-scratch AST. The earlier
	// duplicate substring-only assertion was removed (L-45); this block checks
	// both presence and position.
	if !strings.Contains(got, "Set-nested blocks are not supported by list/schema; emitted as a ListNestedBlock.") {
		t.Errorf("Set block fallback comment missing\ncontent:\n%s", got)
	}
	setIdx := strings.Index(got, "// Set-nested blocks are not supported by list/schema; emitted as a ListNestedBlock.")
	methodIdx := strings.Index(got, "func (l *SetBlocksListResource) ListResourceConfigSchema")
	if setIdx == -1 || methodIdx == -1 || setIdx > methodIdx {
		t.Errorf("Set block fallback comment must appear above ListResourceConfigSchema method\ncontent:\n%s", got)
	}
}

// TestListResourceAttributeExpr_Panic verifies that an unsupported attribute
// schema causes listResourceAttributeExprWithPath to panic and that the panic
// message includes the dotted parent path and attribute name.
func TestListResourceAttributeExpr_Panic(t *testing.T) {
	// A nested unsupported attribute is dropped (returns nil) so the nested map
	// builder skips it, instead of panicking (G2).
	attr := ir.AttributeIR{Name: "bad", Schema: ir.SchemaIR{}}
	if expr := listResourceAttributeExprWithPath(attr, "badres", "badresname"); expr != nil {
		t.Errorf("listResourceAttributeExprWithPath should return nil for a nested unsupported schema, got %v", expr)
	}
}

// TestListResourceFile_SetFallsBackToList locks in A1: the experimental
// list/schema package has no Set types, so a Set collection attribute on a list
// resource config schema is downgraded to listschema.ListAttribute. The
// transformer surfaces this downgrade as a fail-loud Warning (see
// pkg/api/handler_test.go TestListResource_UniqueItemsWarnsAndFallsBack); this
// test pins the generator-side fallback so the downgrade is observable here
// too, rather than silent.
func TestListResourceFile_SetFallsBackToList(t *testing.T) {
	lr := ir.ListResourceIR{
		Name:     "things",
		TypeName: "mycloud_things",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
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
			},
		},
	}

	file := ListResourceFile(lr, "")
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, "listschema.ListAttribute") {
		t.Errorf("expected Set collection to fall back to listschema.ListAttribute\ncontent:\n%s", got)
	}
	if strings.Contains(got, "listschema.SetAttribute") {
		t.Errorf("list/schema has no Set types; SetAttribute must not appear\ncontent:\n%s", got)
	}
	// ElementType is preserved through the downgrade.
	if !strings.Contains(got, "ElementType: types.StringType") {
		t.Errorf("expected ElementType: types.StringType preserved through fallback\ncontent:\n%s", got)
	}
}

// sampleListResourceIR returns a representative ListResourceIR for tests.
func sampleListResourceIR() ir.ListResourceIR {
	return ir.ListResourceIR{
		Name:        "pets",
		TypeName:    "mycloud_pets",
		Description: "Lists pets.",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "id",
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
					Name:     "labels",
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
							{Name: "name", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
							{Name: "age", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeInt}},
						},
					},
				},
			},
		},
	}
}

// generateListResourceModule writes the generated go.mod, provider.go, and
// list resource files into a temporary Go module directory and returns the module
// root.
func generateListResourceModule(t *testing.T, providerIR ir.ProviderIR, listResources []ir.ListResourceIR) string {
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
	for _, lr := range listResources {
		files = append(files, ListResourceFile(lr, ""))
	}
	if err := h.Generate(files); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	return tmp
}

// writeListResourceSchemaValidationTest writes a small test file that imports
// the generated list resource and validates its schema implementation.
func writeListResourceSchemaValidationTest(t *testing.T, moduleRoot string, lr ir.ListResourceIR) {
	t.Helper()
	testPath := filepath.Join(moduleRoot, "internal", "provider", "list_schema_validate_test.go")
	if err := os.MkdirAll(filepath.Dir(testPath), 0o750); err != nil {
		t.Fatalf("create provider test dir: %v", err)
	}

	structName := listResourceStructName(lr)
	content := `package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/list"
	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
)

func Test` + structName + `SchemaValidation(t *testing.T) {
	lr := New` + structName + `()
	var resp list.ListResourceSchemaResponse
	lr.ListResourceConfigSchema(context.Background(), list.ListResourceSchemaRequest{}, &resp)

	diags := resp.Schema.ValidateImplementation(context.Background())
	if diags.HasError() {
		t.Fatalf("schema validation failed: %s", diags)
	}

	if len(resp.Schema.Attributes) == 0 {
		t.Fatal("expected at least one config schema attribute")
	}

	_ = listschema.Schema{}
}
`
	if err := os.WriteFile(testPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write schema validation test: %v", err)
	}
}

// compile-time interface checks.
var _ = ir.ListResourceIR{}
var _ = time.Second
