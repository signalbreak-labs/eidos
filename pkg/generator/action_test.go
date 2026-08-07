package generator

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// TestActionFile_Render verifies that ActionFile emits the expected action
// struct, model, Metadata, Schema, Invoke, and optional ModifyPlan and
// ValidateConfig methods.
func TestActionFile_Render(t *testing.T) {
	a := sampleActionIR()

	file := ActionFile(a, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"package provider",
		"var _ action.Action = (*RebootServerAction)(nil)",
		"var _ action.ActionWithModifyPlan = (*RebootServerAction)(nil)",
		"var _ action.ActionWithValidateConfig = (*RebootServerAction)(nil)",
		"type RebootServerAction struct",
		"type RebootServerActionModel struct",
		"ServerId types.String `tfsdk:\"server_id\"`",
		"Force    types.Bool   `tfsdk:\"force\"`",
		"func NewRebootServerAction()",
		"func (r *RebootServerAction) Metadata",
		"func (r *RebootServerAction) Schema",
		"func (r *RebootServerAction) Invoke",
		"func (r *RebootServerAction) ModifyPlan",
		"func (r *RebootServerAction) ValidateConfig",
		"resp.TypeName = \"mycloud_reboot_server\"",
		"schema.Schema",
		"schema.StringAttribute",
		"schema.BoolAttribute",
		"Required:",
		"Optional:",
		"MarkdownDescription:",
		"Description: \"Reboots a server.\"",
		"// The generated Invoke method is intentionally stubbed; the remote API is not wired.",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated action file missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestActionFile_Render_NoOptionalMethods verifies that an ActionIR with
// ModifyPlan false and no config schema skips ModifyPlan and ValidateConfig.
func TestActionFile_Render_NoOptionalMethods(t *testing.T) {
	a := ir.ActionIR{
		Name:     "ping",
		TypeName: "mycloud_ping",
	}

	file := ActionFile(a, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantAbsent := []string{
		"ActionWithModifyPlan",
		"ActionWithValidateConfig",
		"func (r *PingAction) ModifyPlan",
		"func (r *PingAction) ValidateConfig",
	}
	for _, want := range wantAbsent {
		if strings.Contains(got, want) {
			t.Errorf("generated action file unexpectedly contains %q\ncontent:\n%s", want, got)
		}
	}
	// §6 latent unused-import fix: an action with an empty config schema has
	// no model fields and no blocks, so it never references the types package
	// and must not import it or the generated provider would not compile.
	if strings.Contains(got, `"github.com/hashicorp/terraform-plugin-framework/types"`) {
		t.Errorf("empty-config action must not import types (unused import)\ncontent:\n%s", got)
	}
}

// TestActionNeedsTypesImport verifies the §6 types-import gate for actions:
// the model references types via modelFieldType for every config attribute, so
// any config attribute needs types; a block references types only via
// primitiveAttrType when a nested collection attribute has a primitive element
// (blocks are not model fields). An empty-config action with only primitive
// block attributes must not import types.
func TestActionNeedsTypesImport(t *testing.T) {
	// No config schema: no types.
	if actionNeedsTypesImport(ir.ActionIR{}) {
		t.Fatalf("empty action must not need types import")
	}
	// A primitive config attribute: model references types.
	if !actionNeedsTypesImport(ir.ActionIR{
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{{Name: "id", Schema: ir.SchemaIR{Type: ir.TypeString}}},
		},
	}) {
		t.Fatalf("action with a config attribute must need types import (model field)")
	}
	// A block with only primitive nested attributes: no types (blocks are not
	// model fields and primitive attributes do not call primitiveAttrType).
	if actionNeedsTypesImport(ir.ActionIR{
		ConfigSchema: ir.ObjectSchemaIR{
			Blocks: []ir.BlockIR{{
				Name:        "filter",
				NestingMode: ir.NestingList,
				Schema: ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{
					{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}},
				}},
			}},
		},
	}) {
		t.Fatalf("action with a primitive-only block must not need types import")
	}
	// A block with a nested primitive collection: types via primitiveAttrType.
	if !actionNeedsTypesImport(ir.ActionIR{
		ConfigSchema: ir.ObjectSchemaIR{
			Blocks: []ir.BlockIR{{
				Name:        "filter",
				NestingMode: ir.NestingList,
				Schema: ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{
					{Name: "tags", Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Type: ir.TypeString}}}},
				}},
			}},
		},
	}) {
		t.Fatalf("action with a block containing a primitive collection must need types import")
	}
}

// TestActionFile_SchemaValidation generates a minimal provider with an action
// into a temporary Go module and runs the Terraform plugin-framework schema
// validation to confirm the generated action_<name>.go compiles and its schema
// is valid.
//
// This is an integration test that downloads dependencies with go mod tidy, so
// it is skipped in short mode to avoid network/toolchain dependency failures in
// restricted CI environments.
func TestActionFile_SchemaValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-dependent integration test in short mode")
	}

	p := sampleProviderWithActionIR()

	tmp := generateActionModule(t, p)
	writeActionSchemaValidationTest(t, tmp)

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

// TestActionFile_NestedSchema verifies that nested attributes and blocks are
// rendered using the action/schema package.
func TestActionFile_NestedSchema(t *testing.T) {
	a := ir.ActionIR{
		Name:     "reboot_server",
		TypeName: "mycloud_reboot_server",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name: "server",
					Schema: ir.SchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "id", Schema: ir.SchemaIR{Type: ir.TypeString, Required: true}},
						},
					},
				},
				{
					Name: "aliases",
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind:        ir.List,
							ElementType: ir.SchemaIR{Attributes: []ir.AttributeIR{{Name: "value", Schema: ir.SchemaIR{Type: ir.TypeString, Required: true}}}},
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
							{Name: "key", Schema: ir.SchemaIR{Type: ir.TypeString, Required: true}},
						},
					},
				},
			},
		},
	}

	file := ActionFile(a, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"type RebootServerAction struct",
		"schema.SingleNestedAttribute",
		"schema.ListNestedAttribute",
		"schema.ListNestedBlock",
		"NestedObject: schema.NestedAttributeObject",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated nested action missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestActionFiles_Multiple verifies that ActionFiles emits one file per action
// with deterministic, unique paths.
func TestActionFiles_Multiple(t *testing.T) {
	actions := []ir.ActionIR{
		{Name: "reboot_server", TypeName: "mycloud_reboot_server"},
		{Name: "rotate_credentials", TypeName: "mycloud_rotate_credentials"},
	}

	files := ActionFiles(actions, testClientImport)
	if len(files) != len(actions) {
		t.Fatalf("ActionFiles() returned %d files, want %d", len(files), len(actions))
	}

	if files[0].Path != "internal/provider/action_reboot_server.go" {
		t.Errorf("file[0].Path = %q, want %q", files[0].Path, "internal/provider/action_reboot_server.go")
	}
	if files[1].Path != "internal/provider/action_rotate_credentials.go" {
		t.Errorf("file[1].Path = %q, want %q", files[1].Path, "internal/provider/action_rotate_credentials.go")
	}

	tmp := t.TempDir()
	h := Harness{OutputDir: tmp}
	if err := h.Generate(files); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	gotPaths := collectPaths(t, tmp)
	wantPaths := []string{
		"internal/provider/action_reboot_server.go",
		"internal/provider/action_rotate_credentials.go",
	}
	if diff := sliceDiff(wantPaths, gotPaths); diff != "" {
		t.Errorf("emitted paths mismatch:\n%s", diff)
	}
}

// TestActionTypeName verifies that the Terraform action type name prefers
// ActionIR.TypeName and falls back to empty so Metadata can construct it from
// the provider type name.
func TestActionTypeName(t *testing.T) {
	cases := []struct {
		name     string
		action   ir.ActionIR
		wantName string
	}{
		{
			name:     "prefers type name",
			action:   ir.ActionIR{Name: "reboot_server", TypeName: "mycloud_reboot_server"},
			wantName: "mycloud_reboot_server",
		},
		{
			name:     "falls back to empty",
			action:   ir.ActionIR{Name: "reboot_server"},
			wantName: "",
		},
		{
			name:     "trims whitespace",
			action:   ir.ActionIR{Name: "reboot_server", TypeName: "  mycloud_reboot_server  "},
			wantName: "mycloud_reboot_server",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := actionTypeName(tc.action); got != tc.wantName {
				t.Errorf("actionTypeName() = %q, want %q", got, tc.wantName)
			}
		})
	}
}

// runWithPanicCheck executes fn inside a deferred recover so tests that expect
// a panic can share the same boilerplate. The label is used only for failure
// messages. wantErr must be non-nil when wantPanic is true; it is the sentinel
// error the panic value is expected to wrap. It returns the recovered panic
// value (if any) so callers can assert on the panic contents.
//
// Example:
//
//	rec := runWithPanicCheck(t, true, ErrEmptyActionName, "actionStructName", func() {
//		_ = actionStructName(ir.ActionIR{})
//	})
//	if err, ok := rec.(error); ok && !errors.Is(err, ErrEmptyActionName) {
//		t.Errorf("unexpected panic sentinel: %v", err)
//	}
func runWithPanicCheck(t *testing.T, wantPanic bool, wantErr error, label string, fn func()) {
	t.Helper()
	if wantPanic && wantErr == nil {
		t.Fatalf("%s: wantPanic=true requires a non-nil wantErr sentinel", label)
	}
	defer func() {
		if r := recover(); r != nil {
			if !wantPanic {
				t.Errorf("%s panicked unexpectedly: %v", label, r)
				return
			}
			err, ok := r.(error)
			if !ok {
				t.Errorf("%s panicked with non-error value (%T): %v", label, r, r)
				return
			}
			if !errors.Is(err, wantErr) {
				t.Errorf("%s panic did not wrap expected sentinel (errors.Is returned false): expected %v, got %v", label, wantErr, err)
			}
			return
		}
		if wantPanic {
			t.Errorf("%s did not panic", label)
		}
	}()
	fn()
}

// TestActionStructName verifies generated action struct naming.
func TestActionStructName(t *testing.T) {
	cases := []struct {
		name      string
		want      string
		wantPanic bool
	}{
		{"reboot_server", "RebootServerAction", false},
		{"rotate_credentials", "RotateCredentialsAction", false},
		{"", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runWithPanicCheck(t, tc.wantPanic, ErrEmptyActionName, fmt.Sprintf("actionStructName(%q)", tc.name), func() {
				a := ir.ActionIR{Name: tc.name}
				if got := actionStructName(a); got != tc.want {
					t.Errorf("actionStructName(%q) = %q, want %q", tc.name, got, tc.want)
				}
			})
		})
	}
}

// TestActionModelName verifies generated action model naming.
func TestActionModelName(t *testing.T) {
	cases := []struct {
		name      string
		want      string
		wantPanic bool
	}{
		{"reboot_server", "RebootServerActionModel", false},
		{"rotate_credentials", "RotateCredentialsActionModel", false},
		{"", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runWithPanicCheck(t, tc.wantPanic, ErrEmptyActionName, fmt.Sprintf("actionModelName(%q)", tc.name), func() {
				a := ir.ActionIR{Name: tc.name}
				if got := actionModelName(a); got != tc.want {
					t.Errorf("actionModelName(%q) = %q, want %q", tc.name, got, tc.want)
				}
			})
		})
	}
}

// sampleActionIR returns an ActionIR used for render and validation tests.
func sampleActionIR() ir.ActionIR {
	return ir.ActionIR{
		Name:        "reboot_server",
		FullName:    "Reboot Server",
		TypeName:    "mycloud_reboot_server",
		Description: "Reboots a server.",
		ModifyPlan:  true,
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:        "server_id",
					Description: "The ID of the server to reboot.",
					Required:    true,
					Schema:      ir.SchemaIR{Type: ir.TypeString},
				},
				{
					Name:        "force",
					Description: "Force a hard reboot.",
					Optional:    true,
					Schema:      ir.SchemaIR{Type: ir.TypeBool},
				},
			},
		},
	}
}

// sampleProviderWithActionIR returns a ProviderIR that registers the sample
// action so it can be compiled and validated.
func sampleProviderWithActionIR() ir.ProviderIR {
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
		Actions: []ir.ActionIR{sampleActionIR()},
	}
}

// generateActionModule writes the generated go.mod, provider.go, and action
// files into a temporary module directory and returns the module root.
func generateActionModule(t *testing.T, p ir.ProviderIR) string {
	t.Helper()
	tmp := t.TempDir()

	cfg := BuildConfig{
		ProviderName: p.Name,
		Namespace:    p.Name,
	}

	h := Harness{OutputDir: tmp}
	pf, err := ProviderFile(p)
	if err != nil {
		t.Fatalf("ProviderFile() error = %v", err)
	}
	files := append(BuildFiles(cfg), pf)
	files = append(files, ActionFiles(p.Actions, testClientImport)...)
	if err := h.Generate(files); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	return tmp
}

// writeActionSchemaValidationTest writes a small test file that imports the
// generated provider, instantiates its actions, and validates their schema
// implementations.
func writeActionSchemaValidationTest(t *testing.T, moduleRoot string) {
	t.Helper()
	testPath := filepath.Join(moduleRoot, "internal", "provider", "action_schema_validate_test.go")
	if err := os.MkdirAll(filepath.Dir(testPath), 0o750); err != nil {
		t.Fatalf("create provider test dir: %v", err)
	}

	content := `package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/action"
	tfframeworkprovider "github.com/hashicorp/terraform-plugin-framework/provider"
)

func TestActionSchemaValidation(t *testing.T) {
	p := New()
	pa, ok := p.(tfframeworkprovider.ProviderWithActions)
	if !ok {
		t.Fatalf("provider does not implement ProviderWithActions")
	}
	actions := pa.Actions(context.Background())
	for _, af := range actions {
		a := af()
		var mdResp action.MetadataResponse
		a.Metadata(context.Background(), action.MetadataRequest{ProviderTypeName: "mycloud"}, &mdResp)

		var schemaResp action.SchemaResponse
		a.Schema(context.Background(), action.SchemaRequest{}, &schemaResp)

		diags := schemaResp.Schema.ValidateImplementation(context.Background())
		if diags.HasError() {
			t.Fatalf("schema validation failed for %s: %s", mdResp.TypeName, diags)
		}
	}
}
`
	if err := os.WriteFile(testPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write action schema validation test: %v", err)
	}
}

// TestActionFile_Render_AdvancedAttributes verifies that MarkdownDescription,
// WriteOnly, Deprecated, and DeprecationMessage are surfaced in action schema
// attributes. Action schema attributes do not support Sensitive, so WriteOnly is
// used to exercise the framework-specific flag branch.
func TestActionFile_Render_AdvancedAttributes(t *testing.T) {
	a := ir.ActionIR{
		Name:                "rotate_key",
		TypeName:            "mycloud_rotate_key",
		Description:         "Rotates an API key.",
		MarkdownDescription: "Rotates an **API key**.",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:                "old_key",
					MarkdownDescription: "The existing API key to rotate.",
					Required:            true,
					WriteOnly:           true,
					Schema:              ir.SchemaIR{Type: ir.TypeString},
				},
				{
					Name:               "reason",
					Description:        "Reason for rotation.",
					Optional:           true,
					Deprecated:         true,
					DeprecationMessage: "Use rotation_reason instead.",
					Schema:             ir.SchemaIR{Type: ir.TypeString},
				},
			},
		},
	}

	file := ActionFile(a, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"func (r *RotateKeyAction) Metadata",
		"Rotates an API key.",
		"Rotates an **API key**.",
		"The existing API key to rotate.",
		"Use rotation_reason instead.",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated action file missing %q\ncontent:\n%s", want, got)
		}
	}

	// Action schema attributes cannot be Sensitive, so it must not appear.
	if strings.Contains(got, "Sensitive:") {
		t.Errorf("generated action file unexpectedly emitted Sensitive for an action attribute\ncontent:\n%s", got)
	}

	// WriteOnly is the action-schema equivalent of a sensitive input flag.
	if !strings.Contains(got, "WriteOnly:") {
		t.Errorf("generated action file missing WriteOnly marker\ncontent:\n%s", got)
	}
}

// TestActionSchemaValues_EmptyDescription verifies that an empty top-level
// action Description is omitted from the generated schema.Schema dict.
func TestActionSchemaValues_EmptyDescription(t *testing.T) {
	a := ir.ActionIR{
		Name:     "ping",
		TypeName: "mycloud_ping",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "target", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
	}

	file := ActionFile(a, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if strings.Contains(got, "Description: \"\"") {
		t.Errorf("generated action file should not contain an empty top-level Description\ncontent:\n%s", got)
	}
}

// TestFrameworkActionAttributeExpr verifies the action/schema attribute mapping
// for primitives, collections, nested objects, unions, and unsupported shapes.
func TestFrameworkActionAttributeExpr(t *testing.T) {
	cases := []struct {
		name      string
		attr      ir.AttributeIR
		want      string
		wantPanic bool
		wantErr   error
	}{
		{
			name: "string primitive",
			attr: ir.AttributeIR{Name: "s", Schema: ir.SchemaIR{Type: ir.TypeString}},
			want: "schema.StringAttribute",
		},
		{
			name: "int primitive",
			attr: ir.AttributeIR{Name: "i", Schema: ir.SchemaIR{Type: ir.TypeInt}},
			want: "schema.Int64Attribute",
		},
		{
			name: "float primitive",
			attr: ir.AttributeIR{Name: "f", Schema: ir.SchemaIR{Type: ir.TypeFloat}},
			want: "schema.Float64Attribute",
		},
		{
			name: "bool primitive",
			attr: ir.AttributeIR{Name: "b", Schema: ir.SchemaIR{Type: ir.TypeBool}},
			want: "schema.BoolAttribute",
		},
		{
			name: "dynamic primitive",
			attr: ir.AttributeIR{Name: "d", Schema: ir.SchemaIR{Type: ir.TypeDynamic}},
			want: "schema.DynamicAttribute",
		},
		{
			name: "null primitive",
			attr: ir.AttributeIR{Name: "n", Schema: ir.SchemaIR{Type: ir.TypeNull}},
			want: "schema.DynamicAttribute",
		},
		{
			name: "list of strings",
			attr: ir.AttributeIR{Name: "tags", Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Type: ir.TypeString}}}},
			want: "schema.ListAttribute",
		},
		{
			name: "set of ints",
			attr: ir.AttributeIR{Name: "ids", Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.Set, ElementType: ir.SchemaIR{Type: ir.TypeInt}}}},
			want: "schema.SetAttribute",
		},
		{
			name: "map of bools",
			attr: ir.AttributeIR{Name: "flags", Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.Map, ElementType: ir.SchemaIR{Type: ir.TypeBool}}}},
			want: "schema.MapAttribute",
		},
		{
			name: "list nested object",
			attr: ir.AttributeIR{Name: "items", Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Attributes: []ir.AttributeIR{{Name: "v", Schema: ir.SchemaIR{Type: ir.TypeString}}}}}}},
			want: "schema.ListNestedAttribute",
		},
		{
			name: "union fallback",
			attr: ir.AttributeIR{Name: "anything", Schema: ir.SchemaIR{Union: &ir.UnionType{Variants: []ir.SchemaIR{{Type: ir.TypeString}, {Type: ir.TypeInt}}}}},
			want: "schema.DynamicAttribute",
		},
		{
			// An empty schema (no Type, Collection, Attributes, or Blocks) now
			// renders as a DynamicAttribute instead of panicking (G2).
			name: "unsupported shape",
			attr: ir.AttributeIR{Name: "bad", Schema: ir.SchemaIR{}},
			want: "schema.DynamicAttribute",
		},
		{
			name:      "unknown collection kind",
			attr:      ir.AttributeIR{Name: "bad", Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: "unknown", ElementType: ir.SchemaIR{Type: ir.TypeString}}}},
			wantPanic: true,
			wantErr:   ErrUnsupportedActionCollection,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runWithPanicCheck(t, tc.wantPanic, tc.wantErr, fmt.Sprintf("actionAttributeExpr(%q)", tc.attr.Name), func() {
				stmt := actionAttributeExpr(tc.attr)
				got, err := astgen.RenderExpr(stmt)
				if err != nil {
					t.Fatalf("RenderExpr() error = %v", err)
				}
				if !strings.Contains(string(got), tc.want) {
					t.Errorf("actionAttributeExpr(%q) = %q, want substring %q", tc.attr.Name, string(got), tc.want)
				}
			})
		})
	}
}

// TestActionBlockExpr_NestingModes verifies that supported block nesting modes
// map to the correct action/schema block type and that unknown modes panic.
func TestActionBlockExpr_NestingModes(t *testing.T) {
	cases := []struct {
		name      string
		mode      ir.BlockNestingMode
		want      string
		wantPanic bool
		wantErr   error
	}{
		{name: "single", mode: ir.NestingSingle, want: "schema.SingleNestedBlock"},
		{name: "list", mode: ir.NestingList, want: "schema.ListNestedBlock"},
		{name: "set", mode: ir.NestingSet, want: "schema.SetNestedBlock"},
		{name: "unknown", mode: "unknown", wantPanic: true, wantErr: ErrUnknownActionBlockNesting},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runWithPanicCheck(t, tc.wantPanic, tc.wantErr, fmt.Sprintf("actionBlockExpr(%q)", tc.mode), func() {
				block := ir.BlockIR{Name: "meta", NestingMode: tc.mode, Schema: ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{Name: "k", Schema: ir.SchemaIR{Type: ir.TypeString}}}}}
				stmt := actionBlockExpr(block)
				got, err := astgen.RenderExpr(stmt)
				if err != nil {
					t.Fatalf("RenderExpr() error = %v", err)
				}
				if !strings.Contains(string(got), tc.want) {
					t.Errorf("actionBlockExpr(%q) = %q, want substring %q", tc.mode, string(got), tc.want)
				}
			})
		})
	}
}

// TestActionFile_EmptyNameErrors verifies that generating an action file for an
// unnamed action surfaces the naming conflict as a render error instead of
// silently colliding on "Action".
func TestActionFile_EmptyNameErrors(t *testing.T) {
	file := ActionFile(ir.ActionIR{}, testClientImport)
	var buf bytes.Buffer
	err := file.Render(&buf)
	if err == nil {
		t.Fatal("expected error for empty action name, got nil")
	}
	if !errors.Is(err, ErrEmptyActionName) {
		t.Fatalf("expected ErrEmptyActionName, got %v", err)
	}
}

// compile-time interface checks.
var _ = ir.ActionIR{}
var _ = time.Second
