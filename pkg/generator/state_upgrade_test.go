package generator

import (
	"bytes"
	"context"
	"go/ast"
	"go/format"
	"go/token"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// TestResourceFile_StateUpgrade_Render verifies that a resource with state
// upgrades emits the interface assertion, schema version, versioned prior
// model, and UpgradeState method.
func TestResourceFile_StateUpgrade_Render(t *testing.T) {
	r := sampleUpgradableResourceIR()

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"_ resource.ResourceWithUpgradeState = (*WidgetResource)(nil)",
		"type WidgetResourceModelV0 struct",
		"OldName",
		"tfsdk:\"old_name\"",
		"func (r *WidgetResource) UpgradeState",
		"map[int64]resource.StateUpgrader",
		"int64(0):",
		"PriorSchema: &schema.Schema",
		"\"old_name\": schema.StringAttribute",
		"StateUpgrader: func(ctx context.Context, req resource.UpgradeStateRequest, resp *resource.UpgradeStateResponse)",
		"var prior WidgetResourceModelV0",
		"req.State.Get(ctx, &prior)",
		"var upgraded WidgetResourceModel",
		"upgraded.Name = prior.OldName",
		"resp.State.Set(ctx, &upgraded)",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated resource file missing %q\ncontent:\n%s", want, got)
		}
	}
	versionRe := regexp.MustCompile(`Version:\s+int64\(1\)`)
	if !versionRe.MatchString(got) {
		t.Errorf("generated resource file missing Version: int64(1)\ncontent:\n%s", got)
	}
}

// TestResourceFile_NoStateUpgrade_Render verifies that a resource without
// state upgrades does not emit upgrade-related code.
func TestResourceFile_NoStateUpgrade_Render(t *testing.T) {
	r := sampleResourceIR()

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantAbsent := []string{
		"ResourceWithUpgradeState",
		"UpgradeState",
		"StateUpgrader",
		"Version:",
		"ResourceModelV",
	}
	for _, want := range wantAbsent {
		if strings.Contains(got, want) {
			t.Errorf("generated resource file unexpectedly contains %q\ncontent:\n%s", want, got)
		}
	}
}

// TestValidateStateUpgrades verifies generator-side validation of state upgrade
// configuration against the resource's current schema.
func TestValidateStateUpgrades(t *testing.T) {
	base := sampleUpgradableResourceIR()

	cases := []struct {
		name    string
		r       ir.ResourceIR
		wantErr string
	}{
		{
			name: "valid single rename",
			r:    base,
		},
		{
			name: "rename value missing from current schema",
			r: func() ir.ResourceIR {
				r := base
				r.StateUpgrades = []ir.StateUpgradeIR{
					{FromVersion: 0, Renames: map[string]string{"old_name": "missing"}},
				}
				return r
			}(),
			wantErr: `rename value "missing" is not a current schema attribute`,
		},
		{
			name: "rename key collides with current attribute",
			r: func() ir.ResourceIR {
				r := base
				r.StateUpgrades = []ir.StateUpgradeIR{
					{FromVersion: 0, Renames: map[string]string{"tag": "name"}},
				}
				return r
			}(),
			wantErr: "duplicate prior attribute name",
		},
		{
			name: "duplicate rename values in same upgrade",
			r: func() ir.ResourceIR {
				r := base
				r.StateUpgrades = []ir.StateUpgradeIR{
					{FromVersion: 0, Renames: map[string]string{"a": "name", "b": "name"}},
				}
				return r
			}(),
			wantErr: "multiple renames target current attribute",
		},
		{
			name: "cross-version value conflict",
			r: func() ir.ResourceIR {
				r := base
				r.SchemaVersion = 2
				r.StateUpgrades = []ir.StateUpgradeIR{
					{FromVersion: 0, Renames: map[string]string{"old_name": "name"}},
					{FromVersion: 1, Renames: map[string]string{"legacy_tag": "name"}},
				}
				return r
			}(),
			wantErr: "rename value \"name\" already targeted",
		},
		{
			name: "cross-version key conflict",
			r: func() ir.ResourceIR {
				r := base
				r.SchemaVersion = 2
				r.StateUpgrades = []ir.StateUpgradeIR{
					{FromVersion: 0, Renames: map[string]string{"old_name": "name"}},
					{FromVersion: 1, Renames: map[string]string{"old_name": "tag"}},
				}
				return r
			}(),
			wantErr: "rename key \"old_name\" already used",
		},
		{
			name: "schema version mismatch",
			r: func() ir.ResourceIR {
				r := base
				r.SchemaVersion = 5
				return r
			}(),
			wantErr: "schema_version 5 does not match",
		},
		{
			name: "gap in from versions",
			r: func() ir.ResourceIR {
				r := base
				r.StateUpgrades = []ir.StateUpgradeIR{
					{FromVersion: 1, Renames: map[string]string{"old_name": "name"}},
				}
				return r
			}(),
			wantErr: "gap or unexpected from_version 1 at position 0",
		},
		{
			name: "valid block rename and added attribute",
			r: func() ir.ResourceIR {
				r := sampleUpgradableResourceWithBlocksIR()
				r.StateUpgrades = []ir.StateUpgradeIR{{
					FromVersion:     0,
					BlockRenames:    map[string]string{"old_config": "config"},
					AddedAttributes: []string{"category"},
				}}
				return r
			}(),
		},
		{
			name: "block rename value not a current block",
			r: func() ir.ResourceIR {
				r := sampleUpgradableResourceWithBlocksIR()
				r.StateUpgrades = []ir.StateUpgradeIR{{
					FromVersion:  0,
					BlockRenames: map[string]string{"old_config": "missing_block"},
				}}
				return r
			}(),
			wantErr: `block rename value "missing_block" is not a current schema block`,
		},
		{
			name: "added attribute not in current schema",
			r: func() ir.ResourceIR {
				r := base
				r.StateUpgrades = []ir.StateUpgradeIR{{
					FromVersion:     0,
					AddedAttributes: []string{"nonexistent"},
				}}
				return r
			}(),
			wantErr: `added_attributes "nonexistent" is not a current schema attribute`,
		},
		{
			name: "added attribute also a rename target",
			r: func() ir.ResourceIR {
				r := base
				r.StateUpgrades = []ir.StateUpgradeIR{{
					FromVersion:     0,
					Renames:         map[string]string{"old_name": "name"},
					AddedAttributes: []string{"name"},
				}}
				return r
			}(),
			wantErr: `added_attributes "name" is also a rename`,
		},
		{
			name: "removed attribute still in current schema",
			r: func() ir.ResourceIR {
				r := base
				r.StateUpgrades = []ir.StateUpgradeIR{{
					FromVersion:       0,
					RemovedAttributes: []string{"tag"},
				}}
				return r
			}(),
			wantErr: `removed_attributes "tag" is still a current schema attribute`,
		},
		{
			name: "removed block collides with prior attribute name",
			r: func() ir.ResourceIR {
				r := base
				// "name" is a current attribute; declaring it as a removed block
				// synthesizes a Dynamic prior attribute named "name", colliding with
				// the prior attribute "name".
				r.StateUpgrades = []ir.StateUpgradeIR{{
					FromVersion:   0,
					RemovedBlocks: []string{"name"},
				}}
				return r
			}(),
			wantErr: `removed block "name" collides with another prior attribute name`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateStateUpgrades(tc.r)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("validateStateUpgrades() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateStateUpgrades() want error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("validateStateUpgrades() error = %q, want containing %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// TestResourceFile_StateUpgrade_SchemaValidation generates a temporary provider
// module with a resource that has a state upgrade and validates that the
// generated code compiles and the schema is valid. This test requires network
// access to the public Go module proxy for go mod tidy; it is skipped
// automatically when the proxy is unreachable.
func TestResourceFile_StateUpgrade_SchemaValidation(t *testing.T) {
	skipIfNoNetwork(t)

	p := sampleProviderWithUpgradableResourceIR()

	tmp := generateUpgradableResourceModule(t, p)
	writeUpgradableResourceSchemaValidationTest(t, tmp)

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

// TestResourceFile_StateUpgrade_ObjectAttrNoAttrImport is a regression test for
// H-10: a resource with state upgrades plus an object-like attribute must NOT
// import the terraform-plugin-framework/attr package. stateUpgradeNeedsAttr
// previously returned true whenever any attribute was object-like, registering
// the attr import, but the only attr.-referencing paths (the defensive
// null-initialization branches in stateUpgraderFunc) never fire in production
// because priorSchemaForUpgrade copies every current attribute and block into
// the prior schema. The unused import then failed compilation. The gate now
// evaluates the actual prior schema, so it returns false and the import is
// omitted.
func TestResourceFile_StateUpgrade_ObjectAttrNoAttrImport(t *testing.T) {
	r := sampleUpgradableResourceIR()
	// Add an object-like (nested) attribute so the old heuristic would have
	// registered the attr import.
	r.Schema.Attributes = append(r.Schema.Attributes, ir.AttributeIR{
		Name:     "metadata",
		Optional: true,
		Schema: ir.SchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "owner", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
	})

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if strings.Contains(got, `terraform-plugin-framework/attr"`) {
		t.Errorf("generated resource file imports attr package for state-upgrade resource with object attribute (H-10 regression); stateUpgradeNeedsAttr should be false because priorSchemaForUpgrade includes every current attribute\ncontent:\n%s", got)
	}
	// The state-upgrade machinery is still emitted.
	for _, want := range []string{
		"ResourceWithUpgradeState",
		"UpgradeState",
		`"metadata":`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated resource file missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestResourceFile_StateUpgrade_ObjectAttrCompiles is a compile-time regression
// test for H-10: a resource with state upgrades plus an object-like attribute
// must compile. Previously the unused attr import caused a build failure.
func TestResourceFile_StateUpgrade_ObjectAttrCompiles(t *testing.T) {
	skipIfNoNetwork(t)

	r := sampleUpgradableResourceIR()
	r.Schema.Attributes = append(r.Schema.Attributes, ir.AttributeIR{
		Name:     "metadata",
		Optional: true,
		Schema: ir.SchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "owner", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
	})

	p := sampleProviderWithUpgradableResourceIR()
	p.Resources = []ir.ResourceIR{r}

	tmp := generateUpgradableResourceModule(t, p)
	writeUpgradableResourceSchemaValidationTest(t, tmp)

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
		t.Fatalf("go test failed for state-upgrade resource with object attribute (H-10 regression): %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "PASS") {
		t.Fatalf("expected test to pass, got:\n%s", out)
	}
}

// TestResourceSchemaVersion verifies the schema version helper.
func TestResourceSchemaVersion(t *testing.T) {
	cases := []struct {
		name string
		r    ir.ResourceIR
		want int64
	}{
		{
			name: "explicit version",
			r:    ir.ResourceIR{SchemaVersion: 3},
			want: 3,
		},
		{
			name: "inferred from upgrades",
			r: ir.ResourceIR{
				StateUpgrades: []ir.StateUpgradeIR{{FromVersion: 0}, {FromVersion: 1}},
			},
			want: 2,
		},
		{
			name: "explicit overrides inference",
			r: ir.ResourceIR{
				SchemaVersion: 1,
				StateUpgrades: []ir.StateUpgradeIR{{FromVersion: 5}},
			},
			want: 1,
		},
		{
			name: "no version no upgrades",
			r:    ir.ResourceIR{},
			want: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resourceSchemaVersion(tc.r); got != tc.want {
				t.Errorf("resourceSchemaVersion() = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestPriorSchemaForUpgrade verifies that the prior schema is computed by
// applying reverse renames to the current schema.
func TestPriorSchemaForUpgrade(t *testing.T) {
	r := sampleUpgradableResourceIR()
	prior := priorSchemaForUpgrade(r, r.StateUpgrades[0])

	if len(prior.Attributes) != len(r.Schema.Attributes) {
		t.Fatalf("prior attribute count = %d, want %d", len(prior.Attributes), len(r.Schema.Attributes))
	}

	wants := []string{"id", "old_name", "tag"}
	for _, want := range wants {
		found := false
		for _, attr := range prior.Attributes {
			if attr.Name == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("prior schema missing attribute %q", want)
		}
	}
}

// TestPriorSchemaForUpgrade_BlockChanges verifies that block renames, added
// fields, and removed fields are modeled in the prior schema: renamed blocks
// appear under their old name, added attributes/blocks are omitted, and removed
// attributes/blocks are synthesized as Dynamic attributes.
func TestPriorSchemaForUpgrade_BlockChanges(t *testing.T) {
	r := sampleUpgradableResourceWithBlocksIR()
	// Add an extra block to rename and an extra attribute to remove.
	r.Schema.Blocks = append(r.Schema.Blocks, ir.BlockIR{
		Name:        "metadata",
		NestingMode: ir.NestingSingle,
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{{Name: "owner", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}}},
		},
	})
	upgrade := ir.StateUpgradeIR{
		FromVersion:       0,
		BlockRenames:      map[string]string{"old_config": "config"},
		AddedAttributes:   []string{"category"},
		AddedBlocks:       []string{"metadata"},
		RemovedAttributes: []string{"legacy_id"},
		RemovedBlocks:     []string{"obsolete_block"},
	}
	prior := priorSchemaForUpgrade(r, upgrade)

	priorAttrNames := map[string]struct{}{}
	for _, a := range prior.Attributes {
		priorAttrNames[a.Name] = struct{}{}
	}
	priorBlockNames := map[string]struct{}{}
	for _, b := range prior.Blocks {
		priorBlockNames[b.Name] = struct{}{}
	}

	// Renamed block appears under its old name; not under the new name.
	if _, ok := priorBlockNames["old_config"]; !ok {
		t.Errorf("prior schema missing renamed block under old name %q", "old_config")
	}
	if _, ok := priorBlockNames["config"]; ok {
		t.Errorf("prior schema should not contain block under new name %q (it was renamed)", "config")
	}

	// Added attribute is omitted from the prior schema.
	if _, ok := priorAttrNames["category"]; ok {
		t.Errorf("prior schema should omit added attribute %q", "category")
	}
	// Added block is omitted from the prior schema blocks.
	if _, ok := priorBlockNames["metadata"]; ok {
		t.Errorf("prior schema should omit added block %q", "metadata")
	}

	// Removed attribute and removed block are synthesized as Dynamic prior
	// attributes (carrying the old name) so historical state decodes.
	for _, name := range []string{"legacy_id", "obsolete_block"} {
		found := false
		for _, a := range prior.Attributes {
			if a.Name == name {
				found = true
				if a.Schema.Type != ir.TypeDynamic {
					t.Errorf("prior attribute %q should be Dynamic, got %v", name, a.Schema.Type)
				}
			}
		}
		if !found {
			t.Errorf("prior schema missing synthesized Dynamic attribute for removed field %q", name)
		}
	}
}

// TestResourceFile_StateUpgrade_BlockRename_Render verifies that a block rename
// emits the shape-invariance warning and copies the block from the prior
// (old-named) field to the upgraded (current-named) field.
func TestResourceFile_StateUpgrade_BlockRename_Render(t *testing.T) {
	r := sampleUpgradableResourceWithBlocksIR()
	r.StateUpgrades = []ir.StateUpgradeIR{{
		FromVersion:  0,
		BlockRenames: map[string]string{"old_config": "config"},
	}}

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		"Block rename assumes shape invariance",
		"tfsdk:\"old_config\"", // prior model field uses the old block name
		"upgraded.Config = prior.OldConfig",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated resource file missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestResourceFile_StateUpgrade_AddedAttribute_Render verifies that an added
// attribute is omitted from the prior schema and null-initialized in the
// upgrader (the defensive else branch now fires for real).
func TestResourceFile_StateUpgrade_AddedAttribute_Render(t *testing.T) {
	r := sampleUpgradableResourceIR()
	r.StateUpgrades = []ir.StateUpgradeIR{{
		FromVersion:     0,
		AddedAttributes: []string{"category"},
	}}

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	// The added attribute must NOT appear in the prior model struct (it did not
	// exist in prior state) but must be null-initialized in the upgrader.
	if !strings.Contains(got, "upgraded.Category =") {
		t.Errorf("generated upgrader missing null-init for added attribute category\ncontent:\n%s", got)
	}
	if !strings.Contains(got, "types.StringNull()") {
		t.Errorf("generated upgrader missing types.StringNull() for added attribute\ncontent:\n%s", got)
	}
}

// TestReverseRename verifies reverseRename.
func TestReverseRename(t *testing.T) {
	renames := map[string]string{"old_name": "name", "legacy_id": "id"}
	cases := []struct {
		current string
		want    string
		wantOk  bool
	}{
		{"name", "old_name", true},
		{"id", "legacy_id", true},
		{"tag", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.current, func(t *testing.T) {
			got, ok := reverseRename(renames, tc.current)
			if ok != tc.wantOk {
				t.Errorf("reverseRename(%q) ok = %v, want %v", tc.current, ok, tc.wantOk)
			}
			if got != tc.want {
				t.Errorf("reverseRename(%q) = %q, want %q", tc.current, got, tc.want)
			}
		})
	}
}

// TestHasStateUpgrades verifies hasStateUpgrades.
func TestHasStateUpgrades(t *testing.T) {
	if hasStateUpgrades(ir.ResourceIR{}) {
		t.Errorf("hasStateUpgrades({}) = true, want false")
	}
	if !hasStateUpgrades(ir.ResourceIR{StateUpgrades: []ir.StateUpgradeIR{{FromVersion: 0}}}) {
		t.Errorf("hasStateUpgrades({StateUpgrades: [...]}) = false, want true")
	}
}

// TestSortedUniqueUpgrades verifies that upgrades are sorted and deduplicated.
func TestSortedUniqueUpgrades(t *testing.T) {
	upgrades := []ir.StateUpgradeIR{
		{FromVersion: 2},
		{FromVersion: 0},
		{FromVersion: 2},
		{FromVersion: 1},
	}
	got := sortedUniqueUpgrades(upgrades)
	want := []ir.StateUpgradeIR{{FromVersion: 0}, {FromVersion: 1}, {FromVersion: 2}}
	if len(got) != len(want) {
		t.Fatalf("sortedUniqueUpgrades() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i].FromVersion != want[i].FromVersion {
			t.Fatalf("sortedUniqueUpgrades()[%d] = %d, want %d", i, got[i].FromVersion, want[i].FromVersion)
		}
	}
}

// TestResourceFile_StateUpgrade_Chain_Render verifies that a resource with
// multiple state upgrades emits a map with both version keys.
func TestResourceFile_StateUpgrade_Chain_Render(t *testing.T) {
	r := sampleUpgradableResourceIR()
	r.SchemaVersion = 2
	r.StateUpgrades = []ir.StateUpgradeIR{
		{FromVersion: 0, Renames: map[string]string{"old_name": "name"}},
		{FromVersion: 1, Renames: map[string]string{"legacy_tag": "tag"}},
	}

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"type WidgetResourceModelV0 struct",
		"type WidgetResourceModelV1 struct",
		"int64(0):",
		"int64(1):",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated resource file missing %q\ncontent:\n%s", want, got)
		}
	}
	versionRe := regexp.MustCompile(`Version:\s+int64\(2\)`)
	if !versionRe.MatchString(got) {
		t.Errorf("generated resource file missing Version: int64(2)\ncontent:\n%s", got)
	}

	idx0 := strings.Index(got, "int64(0):")
	idx1 := strings.Index(got, "int64(1):")
	if idx0 == -1 || idx1 == -1 {
		t.Fatalf("missing map keys in generated resource file\ncontent:\n%s", got)
	}
	if idx0 > idx1 {
		t.Errorf("generated UpgradeState map keys are not in ascending order: int64(0): at %d, int64(1): at %d", idx0, idx1)
	}
}

// TestResourceFile_StateUpgrade_WithBlocks_Render verifies that a resource with
// nested blocks emits block fields in the versioned prior model and copies them
// through the state upgrade.
func TestResourceFile_StateUpgrade_WithBlocks_Render(t *testing.T) {
	r := sampleUpgradableResourceWithBlocksIR()

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"type WidgetResourceModelV0 struct",
		"Id",
		"OldName",
		"Config",
		"Alternatives",
		"Tags",
		"tfsdk:\"old_name\"",
		"tfsdk:\"config\"",
		"tfsdk:\"alternatives\"",
		"tfsdk:\"tags\"",
		"var prior WidgetResourceModelV0",
		"var upgraded WidgetResourceModel",
		"upgraded.Id = prior.Id",
		"upgraded.Name = prior.OldName",
		"upgraded.Config = prior.Config",
		"upgraded.Alternatives = prior.Alternatives",
		"upgraded.Tags = prior.Tags",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated resource file missing %q\ncontent:\n%s", want, got)
		}
	}
	// No block-level changes are declared in this upgrade (only an attribute
	// rename), so the block-rename shape-invariance warning must NOT be emitted
	// and blocks are copied unchanged.
	for _, unwanted := range []string{
		"Block rename assumes shape invariance",
		"Block-level state upgrade not modeled",
	} {
		if strings.Contains(got, unwanted) {
			t.Errorf("generated resource file unexpectedly contains %q (no block changes declared)\ncontent:\n%s", unwanted, got)
		}
	}
}

// TestResourceFile_StateUpgrade_MissingBlock_NullInit_Render verifies that when a
// block exists in the current schema but is absent from the prior schema, the
// generated upgrader initializes it with a framework null constructor rather
// than copying from prior state. This exercises the defensive else branch in
// stateUpgraderFunc that depends on nullValueForBlock.
func TestResourceFile_StateUpgrade_MissingBlock_NullInit_Render(t *testing.T) {
	r := sampleUpgradableResourceWithBlocksIR()
	upgrade := r.StateUpgrades[0]
	priorSchema := priorSchemaForUpgrade(r, upgrade)
	// Simulate a migration that added the "tags" set-nested block after the prior
	// schema version by removing it from the computed prior schema.
	filtered := priorSchema.Blocks[:0]
	var missingBlock *ir.BlockIR
	for i := range priorSchema.Blocks {
		if priorSchema.Blocks[i].Name == "tags" {
			b := priorSchema.Blocks[i]
			missingBlock = &b
			continue
		}
		filtered = append(filtered, priorSchema.Blocks[i])
	}
	priorSchema.Blocks = filtered
	if missingBlock == nil {
		t.Fatal("expected to find tags block in prior schema")
	}

	got := renderFuncDecl(t, stateUpgraderFunc(r, upgrade, priorSchema))

	// Blocks that remain in the prior schema are copied directly.
	for _, want := range []string{
		"upgraded.Config = prior.Config",
		"upgraded.Alternatives = prior.Alternatives",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated upgrader missing copy statement %q\ncontent:\n%s", want, got)
		}
	}

	// The missing block should be null-initialized with the correct constructor.
	wantSubstrings := []string{
		"upgraded.Tags =",
		"types.SetNull(",
		"types.ObjectType{",
		"AttrTypes:",
		`"key":`,
		"types.StringType",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated upgrader missing null initializer fragment %q for missing block\ncontent:\n%s", want, got)
		}
	}
}

// TestNullValueForBlock verifies that nullValueForBlock emits valid Terraform
// Plugin Framework null constructors for single, list, and set nested blocks.
func TestNullValueForBlock(t *testing.T) {
	cases := []struct {
		name  string
		block ir.BlockIR
		want  []string
	}{
		{
			name: "single",
			block: ir.BlockIR{
				Name:        "config",
				NestingMode: ir.NestingSingle,
				Schema:      ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{Name: "enabled", Schema: ir.SchemaIR{Type: ir.TypeBool}}}},
			},
			want: []string{"types.ObjectNull(", "types.ObjectType{", "AttrTypes:", `"enabled":`, "types.BoolType"},
		},
		{
			name: "list",
			block: ir.BlockIR{
				Name:        "alternatives",
				NestingMode: ir.NestingList,
				Schema:      ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
			},
			want: []string{"types.ListNull(", "types.ObjectType{", "AttrTypes:", `"name":`, "types.StringType"},
		},
		{
			name: "set",
			block: ir.BlockIR{
				Name:        "tags",
				NestingMode: ir.NestingSet,
				Schema:      ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{Name: "key", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
			},
			want: []string{"types.SetNull(", "types.ObjectType{", "AttrTypes:", `"key":`, "types.StringType"},
		},
		{
			name: "single_with_nested_sub_block",
			block: ir.BlockIR{
				Name:        "outer",
				NestingMode: ir.NestingSingle,
				Schema: ir.ObjectSchemaIR{
					Attributes: []ir.AttributeIR{{Name: "enabled", Schema: ir.SchemaIR{Type: ir.TypeBool}}},
					Blocks: []ir.BlockIR{
						{
							Name:        "inner",
							NestingMode: ir.NestingList,
							Schema:      ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{Name: "value", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
						},
					},
				},
			},
			want: []string{
				"types.ObjectNull(",
				"types.ObjectType{",
				"AttrTypes:",
				`"enabled":`,
				"types.BoolType",
				`"inner":`,
				"types.ListType{",
				"ElemType:",
				`"value":`,
				"types.StringType",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderExpr(t, nullValueForBlock(tc.block))
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("nullValueForBlock() = %q, missing %q", got, want)
				}
			}
		})
	}
}

// TestNullValueForType verifies that nullValueForType emits valid Terraform
// Plugin Framework null constructors for primitive and collection types. It
// asserts on characteristic substrings so the test is stable across go/format
// formatting changes.
func TestNullValueForType(t *testing.T) {
	cases := []struct {
		name string
		s    ir.SchemaIR
		want []string
	}{
		{"string", ir.SchemaIR{Type: ir.TypeString}, []string{"types.StringNull()"}},
		{"int", ir.SchemaIR{Type: ir.TypeInt}, []string{"types.Int64Null()"}},
		{"float", ir.SchemaIR{Type: ir.TypeFloat}, []string{"types.Float64Null()"}},
		{"bool", ir.SchemaIR{Type: ir.TypeBool}, []string{"types.BoolNull()"}},
		{"dynamic", ir.SchemaIR{Type: ir.TypeDynamic}, []string{"types.DynamicNull()"}},
		{"list", ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Type: ir.TypeString}}}, []string{"types.ListNull(", "types.StringType", ")"}},
		{"set", ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.Set, ElementType: ir.SchemaIR{Type: ir.TypeInt}}}, []string{"types.SetNull(", "types.Int64Type", ")"}},
		{"map", ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.Map, ElementType: ir.SchemaIR{Type: ir.TypeBool}}}, []string{"types.MapNull(", "types.BoolType", ")"}},
		{"object", ir.SchemaIR{Attributes: []ir.AttributeIR{{Name: "x", Schema: ir.SchemaIR{Type: ir.TypeString}}}}, []string{"types.ObjectNull(", "map[string]attr.Type{", `"x":`, "types.StringType", "}"}},
		{"object_with_single_block", ir.SchemaIR{
			Attributes: []ir.AttributeIR{{Name: "x", Schema: ir.SchemaIR{Type: ir.TypeString}}},
			Blocks:     []ir.BlockIR{{Name: "meta", NestingMode: ir.NestingSingle, Schema: ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{Name: "k", Schema: ir.SchemaIR{Type: ir.TypeString}}}}}},
		}, []string{"types.ObjectNull(", `"x":`, "types.StringType", `"meta":`, "types.ObjectType{", "AttrTypes:", `"k":`, "types.StringType"},
		},
		{"object_with_list_block", ir.SchemaIR{
			Attributes: []ir.AttributeIR{{Name: "x", Schema: ir.SchemaIR{Type: ir.TypeString}}},
			Blocks:     []ir.BlockIR{{Name: "tags", NestingMode: ir.NestingList, Schema: ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{Name: "k", Schema: ir.SchemaIR{Type: ir.TypeString}}}}}},
		}, []string{"types.ObjectNull(", `"x":`, "types.StringType", `"tags":`, "types.ListType{", "ElemType:", "types.ObjectType{", `"k":`, "types.StringType"},
		},
		{"object_with_set_block", ir.SchemaIR{
			Attributes: []ir.AttributeIR{{Name: "x", Schema: ir.SchemaIR{Type: ir.TypeString}}},
			Blocks:     []ir.BlockIR{{Name: "labels", NestingMode: ir.NestingSet, Schema: ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{Name: "k", Schema: ir.SchemaIR{Type: ir.TypeString}}}}}},
		}, []string{"types.ObjectNull(", `"x":`, "types.StringType", `"labels":`, "types.SetType{", "ElemType:", "types.ObjectType{", `"k":`, "types.StringType"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderExpr(t, nullValueForType(tc.s))
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("nullValueForType() = %q, missing %q", got, want)
				}
			}
		})
	}
}

// TestSchemaTypeExpr verifies that schemaTypeExpr emits the correct attr.Type
// expression for primitive and collection types.
func TestSchemaTypeExpr(t *testing.T) {
	cases := []struct {
		name string
		s    ir.SchemaIR
		want string
	}{
		{"string", ir.SchemaIR{Type: ir.TypeString}, "types.StringType"},
		{"list", ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Type: ir.TypeString}}}, "types.ListType{ElemType: types.StringType}"},
		{"object", ir.SchemaIR{Attributes: []ir.AttributeIR{{Name: "x", Schema: ir.SchemaIR{Type: ir.TypeInt}}}}, "types.ObjectType{AttrTypes: map[string]attr.Type{\"x\": types.Int64Type}}"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderExpr(t, schemaTypeExpr(tc.s))
			if got != tc.want {
				t.Errorf("schemaTypeExpr() = %q, want %q", got, tc.want)
			}
		})
	}
}

// skipIfNoNetwork skips the calling test when the public Go module proxy is
// unreachable. go mod tidy in schema-validation tests needs network access.
func skipIfNoNetwork(t *testing.T) {
	t.Helper()
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(context.Background(), "tcp", "proxy.golang.org:443")
	if err != nil {
		t.Skip("skipping network-dependent test: no connectivity to module proxy")
	}
	conn.Close() //nolint:errcheck // probe cleanup: close error is non-actionable
}

// renderExpr renders an ast expression as a Go source string by wrapping it in a
// throwaway variable declaration and formatting the resulting file.
func renderExpr(t *testing.T, expr ast.Expr) string {
	t.Helper()
	return renderWrapped(t, expr)
}

// renderFuncDecl renders an ast function-literal expression as a Go source
// string by wrapping it in a throwaway variable declaration inside a file. A
// bare function literal is not a valid top-level Go file fragment, so it needs a
// surrounding declaration to be formatted.
func renderFuncDecl(t *testing.T, expr ast.Expr) string {
	t.Helper()
	return renderWrapped(t, expr)
}

// renderWrapped formats expr inside a minimal file and returns just the
// expression source. Substring assertions in callers match against the rendered
// expression.
func renderWrapped(t *testing.T, expr ast.Expr) string {
	t.Helper()
	f := astgen.NewFile("test")
	f.AddDecl(astgen.VarDeclGen(astgen.VarSpec("_", nil, expr)))
	var buf bytes.Buffer
	if err := format.Node(&buf, token.NewFileSet(), f.AST()); err != nil {
		t.Fatalf("format expr: %v", err)
	}
	s := buf.String()
	const prefix = "package test\n\nvar _ = "
	if !strings.HasPrefix(s, prefix) {
		t.Fatalf("unexpected wrapped source: %q", s)
	}
	return strings.TrimSuffix(strings.TrimPrefix(s, prefix), "\n")
}

// sampleUpgradableResourceIR returns a ResourceIR with a schema version and one
// state upgrade that renames an old top-level attribute.
func sampleUpgradableResourceIR() ir.ResourceIR {
	return ir.ResourceIR{
		Name:          "widget",
		TypeName:      "mycloud_widget",
		Description:   "A widget resource.",
		SchemaVersion: 1,
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
					Name:     "category",
					Optional: true,
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
			},
		},
		CRUDMapping: ir.CRUDMappingIR{
			Create: ir.OperationMappingIR{Method: "POST", PathTemplate: "/widgets"},
			Read:   ir.OperationMappingIR{Method: "GET", PathTemplate: "/widgets/{id}"},
			Update: &ir.OperationMappingIR{Method: "PUT", PathTemplate: "/widgets/{id}"},
			Delete: ir.OperationMappingIR{Method: "DELETE", PathTemplate: "/widgets/{id}"},
		},
		Importable: true,
		StateUpgrades: []ir.StateUpgradeIR{
			{
				FromVersion: 0,
				Renames:     map[string]string{"old_name": "name"},
			},
		},
	}
}

// sampleUpgradableResourceWithBlocksIR returns a ResourceIR with a schema version,
// one state upgrade that renames an old top-level attribute, and nested blocks of
// each supported nesting mode. It is used to assert that state upgraders preserve
// nested block values across schema versions.
func sampleUpgradableResourceWithBlocksIR() ir.ResourceIR {
	r := sampleUpgradableResourceIR()
	r.Schema.Blocks = []ir.BlockIR{
		{
			Name:        "config",
			NestingMode: ir.NestingSingle,
			Schema: ir.ObjectSchemaIR{
				Attributes: []ir.AttributeIR{
					{Name: "enabled", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeBool}},
				},
			},
		},
		{
			Name:        "alternatives",
			NestingMode: ir.NestingList,
			Schema: ir.ObjectSchemaIR{
				Attributes: []ir.AttributeIR{
					{Name: "name", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
				},
			},
		},
		{
			Name:        "tags",
			NestingMode: ir.NestingSet,
			Schema: ir.ObjectSchemaIR{
				Attributes: []ir.AttributeIR{
					{Name: "key", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
				},
			},
		},
	}
	return r
}

// sampleProviderWithUpgradableResourceIR returns a ProviderIR that registers the
// sample upgradable resource.
func sampleProviderWithUpgradableResourceIR() ir.ProviderIR {
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
		Resources: []ir.ResourceIR{sampleUpgradableResourceIR()},
	}
}

// generateUpgradableResourceModule writes the generated go.mod, provider.go,
// and resource_<name>.go files for a provider with an upgradable resource into
// a temporary module directory and returns the module root.
func generateUpgradableResourceModule(t *testing.T, p ir.ProviderIR) string {
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

// writeUpgradableResourceSchemaValidationTest writes a small test file that
// imports the generated provider, instantiates its managed resources, and
// validates their schema implementations.
func writeUpgradableResourceSchemaValidationTest(t *testing.T, moduleRoot string) {
	t.Helper()
	testPath := filepath.Join(moduleRoot, "internal", "provider", "resource_upgrade_schema_validate_test.go")
	if err := os.MkdirAll(filepath.Dir(testPath), 0o750); err != nil {
		t.Fatalf("create provider test dir: %v", err)
	}

	content := `package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
)

func TestUpgradableResourceSchemaValidation(t *testing.T) {
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

		if upgrader, ok := r.(resource.ResourceWithUpgradeState); ok {
			upgraders := upgrader.UpgradeState(context.Background())
			if len(upgraders) == 0 {
				t.Fatalf("expected state upgraders for %s", mdResp.TypeName)
			}
			for version, su := range upgraders {
				if su.PriorSchema == nil {
					t.Fatalf("upgrader %d for %s has nil PriorSchema", version, mdResp.TypeName)
				}
				if su.StateUpgrader == nil {
					t.Fatalf("upgrader %d for %s has nil StateUpgrader", version, mdResp.TypeName)
				}
				priorDiags := su.PriorSchema.ValidateImplementation(context.Background())
				if priorDiags.HasError() {
					t.Fatalf("prior schema validation failed for %s version %d: %s", mdResp.TypeName, version, priorDiags)
				}
			}
		} else {
			t.Fatalf("expected resource %s to implement ResourceWithUpgradeState", mdResp.TypeName)
		}
	}
}
`
	if err := os.WriteFile(testPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write upgradable resource schema validation test: %v", err)
	}
}
