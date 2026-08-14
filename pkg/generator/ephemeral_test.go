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

// TestEphemeralFile_Render verifies that EphemeralFile emits the expected
// ephemeral resource struct, model, Metadata, Schema, Open, Renew, and Close
// methods.
func TestEphemeralFile_Render(t *testing.T) {
	er := sampleEphemeralResourceIR()

	file := EphemeralFile(er, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"package provider",
		"var _ ephemeral.EphemeralResource = (*TemporaryCredentialEphemeralResource)(nil)",
		"var _ ephemeral.EphemeralResourceWithRenew = (*TemporaryCredentialEphemeralResource)(nil)",
		"var _ ephemeral.EphemeralResourceWithClose = (*TemporaryCredentialEphemeralResource)(nil)",
		"type TemporaryCredentialEphemeralResource struct",
		"type TemporaryCredentialEphemeralResourceModel struct",
		"Duration  types.Int64  `tfsdk:\"duration\"`",
		"Token     types.String `tfsdk:\"token\"`",
		"func NewTemporaryCredentialEphemeralResource()",
		"func (e *TemporaryCredentialEphemeralResource) Metadata",
		"func (e *TemporaryCredentialEphemeralResource) Schema",
		"func (e *TemporaryCredentialEphemeralResource) Open",
		"func (e *TemporaryCredentialEphemeralResource) Renew",
		"func (e *TemporaryCredentialEphemeralResource) Close",
		"resp.TypeName = \"mycloud_temporary_credential\"",
		"ephemeralschema.Schema",
		"ephemeralschema.Int64Attribute",
		"ephemeralschema.StringAttribute",
		"Required: true",
		"Computed: true",
		"req.Config.Get(ctx, &data)",
		"resp.Result.Set(ctx, &data)",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated ephemeral resource missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestEphemeralFile_RenderNoRenewClose verifies that an ephemeral resource
// without renew or close mappings does not emit the optional interface
// assertions or methods.
func TestEphemeralFile_RenderNoRenewClose(t *testing.T) {
	er := ir.EphemeralResourceIR{
		Name:     "temporary_credential",
		TypeName: "mycloud_temporary_credential",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "duration", Required: true, Schema: ir.SchemaIR{Type: ir.TypeInt}},
			},
		},
		ResultSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "token", Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
	}

	file := EphemeralFile(er, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, unwanted := range []string{
		"EphemeralResourceWithRenew",
		"EphemeralResourceWithClose",
		"func (e *TemporaryCredentialEphemeralResource) Renew",
		"func (e *TemporaryCredentialEphemeralResource) Close",
	} {
		if strings.Contains(got, unwanted) {
			t.Errorf("generated ephemeral resource should not contain %q\ncontent:\n%s", unwanted, got)
		}
	}
}

// TestEphemeralFile_EmptySchemaOmitsTypesImport verifies the §6 latent
// unused-import fix for ephemeral resources: an ephemeral resource whose
// config and result schemas are both empty produces an empty model and must
// not import the types package, or the import is unused and the generated
// provider does not compile. The model field type is the only types.*
// reference in the generated ephemeral file.
func TestEphemeralFile_EmptySchemaOmitsTypesImport(t *testing.T) {
	er := ir.EphemeralResourceIR{
		Name:     "empty",
		TypeName: "mycloud_empty",
	}

	file := EphemeralFile(er, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if strings.Contains(got, `"github.com/hashicorp/terraform-plugin-framework/types"`) {
		t.Errorf("empty-schema ephemeral resource must not import types (unused import)\ncontent:\n%s", got)
	}
}

// TestEphemeralFile_SchemaValidation generates a minimal provider with an
// ephemeral resource into a temporary Go module and runs the Terraform
// plugin-framework schema validation to confirm the generated
// ephemeral_<name>.go compiles and its schema is valid.
func TestEphemeralFile_SchemaValidation(t *testing.T) {
	er := sampleEphemeralResourceIR()

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
		EphemeralResources: []ir.EphemeralResourceIR{er},
	}

	tmp := generateEphemeralResourceModule(t, providerIR, []ir.EphemeralResourceIR{er})
	writeEphemeralResourceSchemaValidationTest(t, tmp)

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

// TestEphemeralFile_PlainBlockCompiles is a regression test for H-3: an
// ephemeral resource with a block that has no MinItems/MaxItems must not
// register the schema/validator import, because ephemeralBlockExpr only
// references validator.List/validator.Set when a size constraint is present.
// A plain block previously produced an "imported and not used" compile failure.
// This test compiles the generated module to guard against that regression.
func TestEphemeralFile_PlainBlockCompiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}
	skipIfNetworkRestricted(t)

	er := ir.EphemeralResourceIR{
		Name:     "temporary_credential",
		TypeName: "mycloud_temporary_credential",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "scope", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
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
				{
					Name:        "tags",
					NestingMode: ir.NestingSet,
					Schema: ir.ObjectSchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "value", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				},
			},
		},
		ResultSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "token", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
	}

	providerIR := ir.ProviderIR{
		Name:               "mycloud",
		TypeName:           "mycloud",
		EphemeralResources: []ir.EphemeralResourceIR{er},
	}

	tmp := generateEphemeralResourceModule(t, providerIR, []ir.EphemeralResourceIR{er})

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
		t.Fatalf("go build failed for ephemeral resource with plain block (H-3 regression): %v\n%s", err, out)
	}
}

// TestEphemeralFile_NestedSchema verifies that nested attributes and blocks
// are rendered using the ephemeral/schema package.
func TestEphemeralFile_NestedSchema(t *testing.T) {
	er := ir.EphemeralResourceIR{
		Name:     "temporary_credential",
		TypeName: "mycloud_temporary_credential",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "requester",
					Required: true,
					Schema: ir.SchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}},
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
							{Name: "key", Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				},
			},
		},
		ResultSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "token", Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
	}

	file := EphemeralFile(er, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"type TemporaryCredentialEphemeralResource struct",
		"ephemeralschema.SingleNestedAttribute",
		"ephemeralschema.ListNestedAttribute",
		"ephemeralschema.ListNestedBlock",
		"NestedObject: ephemeralschema.NestedAttributeObject",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated nested ephemeral resource missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestEphemeralResourceTypeName verifies that the Terraform type name prefers
// EphemeralResourceIR.TypeName and falls back to the trimmed ephemeral resource
// name.
func TestEphemeralResourceTypeName(t *testing.T) {
	cases := []struct {
		name     string
		er       ir.EphemeralResourceIR
		wantName string
	}{
		{
			name:     "prefers type name",
			er:       ir.EphemeralResourceIR{Name: "temporary_credential", TypeName: "mycloud_temporary_credential"},
			wantName: "mycloud_temporary_credential",
		},
		{
			name:     "falls back to name",
			er:       ir.EphemeralResourceIR{Name: "temporary_credential"},
			wantName: "temporary_credential",
		},
		{
			name:     "trims whitespace fallback",
			er:       ir.EphemeralResourceIR{Name: "  temporary_credential  "},
			wantName: "temporary_credential",
		},
		{
			name:     "trims whitespace type name",
			er:       ir.EphemeralResourceIR{Name: "temporary_credential", TypeName: "  mycloud_temporary_credential  "},
			wantName: "mycloud_temporary_credential",
		},
		{
			name:     "snake cases fallback",
			er:       ir.EphemeralResourceIR{Name: "My Credential"},
			wantName: "my_credential",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ephemeralResourceTypeName(tc.er); got != tc.wantName {
				t.Errorf("ephemeralResourceTypeName() = %q, want %q", got, tc.wantName)
			}
		})
	}
}

// TestEphemeralResourceFiles_Multiple verifies that EphemeralFiles emits one
// file per ephemeral resource with deterministic, unique paths.
func TestEphemeralResourceFiles_Multiple(t *testing.T) {
	ers := []ir.EphemeralResourceIR{
		{Name: "temporary_credential", TypeName: "mycloud_temporary_credential"},
		{Name: "short_lived_token", TypeName: "mycloud_short_lived_token"},
	}

	files := EphemeralFiles(ers, testClientImport)
	if len(files) != len(ers) {
		t.Fatalf("EphemeralFiles() returned %d files, want %d", len(files), len(ers))
	}

	if files[0].Path != "internal/provider/ephemeral_temporary_credential.go" {
		t.Errorf("file[0].Path = %q, want %q", files[0].Path, "internal/provider/ephemeral_temporary_credential.go")
	}
	if files[1].Path != "internal/provider/ephemeral_short_lived_token.go" {
		t.Errorf("file[1].Path = %q, want %q", files[1].Path, "internal/provider/ephemeral_short_lived_token.go")
	}

	tmp := t.TempDir()
	h := Harness{OutputDir: tmp}
	if err := h.Generate(files); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	gotPaths := collectPaths(t, tmp)
	wantPaths := []string{
		"internal/provider/ephemeral_short_lived_token.go",
		"internal/provider/ephemeral_temporary_credential.go",
	}
	if diff := sliceDiff(wantPaths, gotPaths); diff != "" {
		t.Errorf("emitted paths mismatch:\n%s", diff)
	}
}

// TestEphemeralResourceMergedAttribute verifies that an attribute present in
// both config and result schemas is emitted as optional and computed.
func TestEphemeralResourceMergedAttribute(t *testing.T) {
	er := ir.EphemeralResourceIR{
		Name:     "temporary_credential",
		TypeName: "mycloud_temporary_credential",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "duration", Required: true, Schema: ir.SchemaIR{Type: ir.TypeInt}},
			},
		},
		ResultSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "duration", Schema: ir.SchemaIR{Type: ir.TypeInt}},
			},
		},
	}

	file := EphemeralFile(er, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, "Optional: true") {
		t.Errorf("merged attribute should be Optional\ncontent:\n%s", got)
	}
	if !strings.Contains(got, "Computed: true") {
		t.Errorf("merged attribute should be Computed\ncontent:\n%s", got)
	}
	if strings.Contains(got, "Required: true") {
		t.Errorf("merged attribute should not be Required\ncontent:\n%s", got)
	}
}

// TestEphemeralResourceModelName verifies the generated ephemeral resource model
// struct name.
func TestEphemeralResourceModelName(t *testing.T) {
	cases := []struct {
		name string
		er   ir.EphemeralResourceIR
		want string
	}{
		{
			name: "snake_case name",
			er:   ir.EphemeralResourceIR{Name: "temporary_credential"},
			want: "TemporaryCredentialEphemeralResourceModel",
		},
		{
			name: "already PascalCase name",
			er:   ir.EphemeralResourceIR{Name: "TemporaryCredential"},
			want: "TemporaryCredentialEphemeralResourceModel",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ephemeralResourceModelName(tc.er); got != tc.want {
				t.Errorf("ephemeralResourceModelName() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestEphemeralResourceStructName verifies the generated ephemeral resource
// struct name.
func TestEphemeralResourceStructName(t *testing.T) {
	cases := []struct {
		name string
		er   ir.EphemeralResourceIR
		want string
	}{
		{
			name: "snake_case name",
			er:   ir.EphemeralResourceIR{Name: "temporary_credential"},
			want: "TemporaryCredentialEphemeralResource",
		},
		{
			name: "already PascalCase name",
			er:   ir.EphemeralResourceIR{Name: "TemporaryCredential"},
			want: "TemporaryCredentialEphemeralResource",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ephemeralResourceStructName(tc.er); got != tc.want {
				t.Errorf("ephemeralResourceStructName() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestEphemeralFile_MapSetDynamicTypes verifies MapAttribute, MapNestedAttribute,
// SetAttribute, and DynamicAttribute are emitted.
func TestEphemeralFile_MapSetDynamicTypes(t *testing.T) {
	er := ir.EphemeralResourceIR{
		Name:     "temporary_credential",
		TypeName: "mycloud_temporary_credential",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name: "tags",
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind:        ir.Map,
							ElementType: ir.SchemaIR{Type: ir.TypeString},
						},
					},
				},
				{
					Name: "metadata",
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind: ir.Map,
							ElementType: ir.SchemaIR{
								Attributes: []ir.AttributeIR{
									{Name: "value", Schema: ir.SchemaIR{Type: ir.TypeString, Computed: true}},
								},
							},
						},
					},
				},
				{
					Name: "allowed",
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind:        ir.Set,
							ElementType: ir.SchemaIR{Type: ir.TypeString},
						},
					},
				},
				{
					Name: "dynamic_value",
					Schema: ir.SchemaIR{
						Type: ir.TypeDynamic,
					},
				},
			},
		},
	}

	file := EphemeralFile(er, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		"ephemeralschema.MapAttribute",
		"ephemeralschema.MapNestedAttribute",
		"ephemeralschema.SetAttribute",
		"ephemeralschema.DynamicAttribute",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated ephemeral resource missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestEphemeralFile_UnionDynamicAttribute verifies that a Union schema is emitted
// as a DynamicAttribute.
func TestEphemeralFile_UnionDynamicAttribute(t *testing.T) {
	er := ir.EphemeralResourceIR{
		Name:     "union",
		TypeName: "mycloud_union",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "anything", Schema: ir.SchemaIR{Union: &ir.UnionType{}}},
			},
		},
	}

	file := EphemeralFile(er, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, "ephemeralschema.DynamicAttribute") {
		t.Errorf("generated ephemeral resource missing ephemeralschema.DynamicAttribute for union\ncontent:\n%s", got)
	}
}

// TestEphemeralFile_SensitiveDeprecated verifies that Sensitive and deprecation
// metadata are emitted for ephemeral resource attributes.
func TestEphemeralFile_SensitiveDeprecated(t *testing.T) {
	er := ir.EphemeralResourceIR{
		Name:     "temporary_credential",
		TypeName: "mycloud_temporary_credential",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:      "password",
					Sensitive: true,
					Schema:    ir.SchemaIR{Type: ir.TypeString},
				},
				{
					Name:               "old_field",
					DeprecationMessage: "Use new_field instead.",
					Schema:             ir.SchemaIR{Type: ir.TypeString},
				},
				{
					Name:       "legacy_field",
					Deprecated: true,
					Schema:     ir.SchemaIR{Type: ir.TypeString},
				},
			},
		},
	}

	file := EphemeralFile(er, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		"Sensitive: true",
		"DeprecationMessage: \"Use new_field instead.\"",
		"DeprecationMessage: \"Deprecated\"",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated ephemeral resource missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestEphemeralFile_HasRenewWithoutClose verifies that an ephemeral resource
// with only Renew emits the renew interface assertion and method but not the
// close ones.
func TestEphemeralFile_HasRenewWithoutClose(t *testing.T) {
	er := ir.EphemeralResourceIR{
		Name:     "temporary_credential",
		TypeName: "mycloud_temporary_credential",
		HasRenew: true,
		HasClose: false,
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "duration", Required: true, Schema: ir.SchemaIR{Type: ir.TypeInt}},
			},
		},
	}

	file := EphemeralFile(er, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		"var _ ephemeral.EphemeralResourceWithRenew",
		"func (e *TemporaryCredentialEphemeralResource) Renew",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated ephemeral resource missing %q\ncontent:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{
		"EphemeralResourceWithClose",
		"func (e *TemporaryCredentialEphemeralResource) Close",
	} {
		if strings.Contains(got, unwanted) {
			t.Errorf("generated ephemeral resource should not contain %q\ncontent:\n%s", unwanted, got)
		}
	}
}

// TestEphemeralFile_HasCloseWithoutRenew verifies that an ephemeral resource
// with only Close emits the close interface assertion and method but not the
// renew ones.
func TestEphemeralFile_HasCloseWithoutRenew(t *testing.T) {
	er := ir.EphemeralResourceIR{
		Name:     "temporary_credential",
		TypeName: "mycloud_temporary_credential",
		HasRenew: false,
		HasClose: true,
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "duration", Required: true, Schema: ir.SchemaIR{Type: ir.TypeInt}},
			},
		},
	}

	file := EphemeralFile(er, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		"var _ ephemeral.EphemeralResourceWithClose",
		"func (e *TemporaryCredentialEphemeralResource) Close",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated ephemeral resource missing %q\ncontent:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{
		"EphemeralResourceWithRenew",
		"func (e *TemporaryCredentialEphemeralResource) Renew",
	} {
		if strings.Contains(got, unwanted) {
			t.Errorf("generated ephemeral resource should not contain %q\ncontent:\n%s", unwanted, got)
		}
	}
}

// TestEphemeralMergedBlocks verifies that config and result blocks are merged
// deterministically by name, config blocks take precedence, and block
// cardinality validators are emitted.
func TestEphemeralMergedBlocks(t *testing.T) {
	minItems := int64(1)
	maxItems := int64(5)
	er := ir.EphemeralResourceIR{
		Name:     "temporary_credential",
		TypeName: "mycloud_temporary_credential",
		ConfigSchema: ir.ObjectSchemaIR{
			Blocks: []ir.BlockIR{
				{
					Name:        "config_block",
					NestingMode: ir.NestingList,
					MinItems:    &minItems,
					MaxItems:    &maxItems,
					Schema: ir.ObjectSchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "key", Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				},
			},
		},
		ResultSchema: ir.ObjectSchemaIR{
			Blocks: []ir.BlockIR{
				{
					Name:        "result_block",
					NestingMode: ir.NestingSet,
					Schema: ir.ObjectSchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "value", Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				},
			},
		},
	}

	file := EphemeralFile(er, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		"ephemeralschema.ListNestedBlock",
		"ephemeralschema.SetNestedBlock",
		"SizeAtLeast(int64(1))",
		"SizeAtMost(int64(5))",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated ephemeral resource missing %q\ncontent:\n%s", want, got)
		}
	}

	// Verify deterministic ordering: config_block should appear before result_block.
	// Use byte positions of the stable map-key tokens rather than substring
	// indexes so the assertion survives jen formatter whitespace changes.
	raw := buf.Bytes()
	idxConfig := bytes.Index(raw, []byte(`"config_block":`))
	idxResult := bytes.Index(raw, []byte(`"result_block":`))
	switch {
	case idxConfig == -1:
		t.Errorf("generated ephemeral resource missing config_block\ncontent:\n%s", got)
	case idxResult == -1:
		t.Errorf("generated ephemeral resource missing result_block\ncontent:\n%s", got)
	case idxConfig > idxResult:
		t.Errorf("config_block should appear before result_block\ncontent:\n%s", got)
	}
}

// TestEphemeralMergedAttributes_TypeConflict verifies that merging config and
// result attributes with incompatible types degrades the merged attribute to a
// dynamic type instead of panicking (G2): the plugin-framework ephemeral schema
// has no first-class union attribute, so a conflicting config/result value can
// only be represented as dynamic. Generation stays honest rather than crashing.
func TestEphemeralMergedAttributes_TypeConflict(t *testing.T) {
	er := ir.EphemeralResourceIR{
		Name: "temporary_credential",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "duration", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeInt}},
			},
		},
		ResultSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "duration", Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
	}

	// Should not panic.
	merged := ephemeralMergedAttributes(er)

	var found bool
	for _, attr := range merged {
		if attr.Name != "duration" {
			continue
		}
		found = true
		if attr.Schema.Type != ir.TypeDynamic {
			t.Errorf("duration.Schema.Type = %q, want %q (dynamic degradation)", attr.Schema.Type, ir.TypeDynamic)
		}
		if attr.Schema.Collection != nil || attr.Schema.Union != nil || attr.Schema.Attributes != nil {
			t.Errorf("duration schema shape fields must be cleared for dynamic degradation, got collection=%v union=%v attrs=%v",
				attr.Schema.Collection, attr.Schema.Union, attr.Schema.Attributes)
		}
		if !attr.Optional || !attr.Computed {
			t.Errorf("duration must be Optional+Computed after merge, got Optional=%v Computed=%v", attr.Optional, attr.Computed)
		}
	}
	if !found {
		t.Fatalf("duration attribute missing from merged attributes: %+v", merged)
	}
}

// TestEphemeralAttributeExpr_Panic verifies that an unsupported attribute schema
// causes ephemeralAttributeExpr to panic and that the panic message includes the
// resource and attribute names.
func TestEphemeralAttributeExpr_Panic(t *testing.T) {
	// An unrepresentable top-level attribute now renders as a DynamicAttribute
	// instead of panicking (G2).
	attr := ir.AttributeIR{Name: "bad", Schema: ir.SchemaIR{}}
	if expr := ephemeralAttributeExpr(attr, "badres"); expr == nil {
		t.Error("ephemeralAttributeExpr returned nil for unsupported schema")
	}
}

// TestEphemeralAttributeExprWithPath_Panic verifies that an unsupported nested
// attribute schema is dropped (returns nil) so the nested map builder skips it,
// instead of panicking (G2).
func TestEphemeralAttributeExprWithPath_Panic(t *testing.T) {
	attr := ir.AttributeIR{Name: "nested_bad", Schema: ir.SchemaIR{}}
	if expr := ephemeralAttributeExprWithPath(attr, "parent", "badres"); expr != nil {
		t.Errorf("ephemeralAttributeExprWithPath should return nil for a nested unsupported schema, got %v", expr)
	}
}

// TestEphemeralFile_BlockDeprecated verifies that the block Deprecated bool
// shorthand emits a DeprecationMessage field for ephemeral resource blocks.
func TestEphemeralFile_BlockDeprecated(t *testing.T) {
	er := ir.EphemeralResourceIR{
		Name:     "temporary_credential",
		TypeName: "mycloud_temporary_credential",
		ConfigSchema: ir.ObjectSchemaIR{
			Blocks: []ir.BlockIR{
				{
					Name:       "legacy_block",
					Deprecated: true,
					Schema: ir.ObjectSchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "key", Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				},
			},
		},
	}

	file := EphemeralFile(er, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, "DeprecationMessage: \"Deprecated\"") {
		t.Errorf("generated ephemeral resource missing block DeprecationMessage\ncontent:\n%s", got)
	}
}

// TestEphemeralFile_NoRequiredOptionalComputed verifies that a config attribute
// with none of the Required/Optional/Computed flags set is emitted without any
// of those flags.
func TestEphemeralFile_NoRequiredOptionalComputed(t *testing.T) {
	er := ir.EphemeralResourceIR{
		Name:     "temporary_credential",
		TypeName: "mycloud_temporary_credential",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "unflagged", Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
	}

	file := EphemeralFile(er, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	// Locate the attribute declaration and ensure no flag follows it.
	attrIdx := strings.Index(got, `"unflagged"`)
	if attrIdx == -1 {
		t.Fatalf("generated ephemeral resource missing unflagged attribute\ncontent:\n%s", got)
	}
	attrEnd := strings.Index(got[attrIdx:], ",") + attrIdx
	segment := got[attrIdx:attrEnd]
	for _, unwanted := range []string{"Required: true", "Optional: true", "Computed: true"} {
		if strings.Contains(segment, unwanted) {
			t.Errorf("unflagged attribute segment should not contain %q\nsegment:\n%s", unwanted, segment)
		}
	}
}

// TestEphemeralFile_EmptyDescription verifies that an empty ephemeral resource
// description does not emit a MarkdownDescription field.
func TestEphemeralFile_EmptyDescription(t *testing.T) {
	er := ir.EphemeralResourceIR{
		Name:        "temporary_credential",
		TypeName:    "mycloud_temporary_credential",
		Description: "",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "duration", Required: true, Schema: ir.SchemaIR{Type: ir.TypeInt}},
			},
		},
	}

	file := EphemeralFile(er, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if strings.Contains(got, "MarkdownDescription") {
		t.Errorf("generated ephemeral resource should not contain MarkdownDescription for empty description\ncontent:\n%s", got)
	}
}

// sampleEphemeralResourceIR returns an EphemeralResourceIR used for render and
// validation tests.
func sampleEphemeralResourceIR() ir.EphemeralResourceIR {
	return ir.EphemeralResourceIR{
		Name:        "temporary_credential",
		TypeName:    "mycloud_temporary_credential",
		Description: "Generates a temporary credential.",
		HasRenew:    true,
		HasClose:    true,
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "duration",
					Required: true,
					Schema:   ir.SchemaIR{Type: ir.TypeInt},
				},
				{
					Name:     "scope",
					Optional: true,
					Schema:   ir.SchemaIR{Type: ir.TypeString},
				},
			},
		},
		ResultSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "token",
					Computed: true,
					Schema:   ir.SchemaIR{Type: ir.TypeString},
				},
				{
					Name:     "expires_at",
					Computed: true,
					Schema:   ir.SchemaIR{Type: ir.TypeString},
				},
			},
		},
		OpenMapping: ir.OperationMappingIR{
			Method:       "POST",
			PathTemplate: "/credentials/temporary",
		},
		RenewMapping: &ir.OperationMappingIR{
			Method:       "POST",
			PathTemplate: "/credentials/temporary/{id}/renew",
		},
		CloseMapping: &ir.OperationMappingIR{
			Method:       "DELETE",
			PathTemplate: "/credentials/temporary/{id}",
		},
	}
}

// generateEphemeralResourceModule writes the generated go.mod, provider.go, and
// ephemeral resource files into a temporary module directory and returns the
// module root.
func generateEphemeralResourceModule(t *testing.T, providerIR ir.ProviderIR, ers []ir.EphemeralResourceIR) string {
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
	for _, er := range ers {
		files = append(files, EphemeralFile(er, testClientImport))
	}
	if err := h.Generate(files); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	return tmp
}

// writeEphemeralResourceSchemaValidationTest writes a small test file that
// imports the generated ephemeral resource and validates its schema
// implementation.
func writeEphemeralResourceSchemaValidationTest(t *testing.T, moduleRoot string) {
	t.Helper()
	testPath := filepath.Join(moduleRoot, "internal", "provider", "ephemeral_schema_validate_test.go")
	if err := os.MkdirAll(filepath.Dir(testPath), 0o750); err != nil {
		t.Fatalf("create provider test dir: %v", err)
	}

	content := `package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/ephemeral"
	tfframeworkprovider "github.com/hashicorp/terraform-plugin-framework/provider"
)

func TestEphemeralResourceSchemaValidation(t *testing.T) {
	p := New()
	pwer, ok := p.(tfframeworkprovider.ProviderWithEphemeralResources)
	if !ok {
		t.Fatalf("provider does not implement ephemeral resources")
	}
	ephemerals := pwer.EphemeralResources(context.Background())
	for _, ef := range ephemerals {
		e := ef()
		var mdResp ephemeral.MetadataResponse
		e.Metadata(context.Background(), ephemeral.MetadataRequest{}, &mdResp)

		var schemaResp ephemeral.SchemaResponse
		e.Schema(context.Background(), ephemeral.SchemaRequest{}, &schemaResp)

		diags := schemaResp.Schema.ValidateImplementation(context.Background())
		if diags.HasError() {
			t.Fatalf("schema validation failed for %s: %s", mdResp.TypeName, diags)
		}
	}
}
`
	if err := os.WriteFile(testPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write ephemeral schema validation test: %v", err)
	}
}

// compile-time interface checks.
var _ = ir.EphemeralResourceIR{}
var _ = time.Second
