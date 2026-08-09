package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDurationMethod covers the nil receiver and the value passthrough.
func TestDurationMethod(t *testing.T) {
	var d *Duration
	if got := d.Duration(); got != 0 {
		t.Errorf("nil Duration.Duration() = %v, want 0", got)
	}
	d = new(Duration)
	*d = Duration(5 * time.Second)
	if got := d.Duration(); got != 5*time.Second {
		t.Errorf("Duration.Duration() = %v, want 5s", got)
	}
}

// TestValidateStateUpgrades_BlockAndListBranches drives the block-rename and
// added/removed attribute/block emptiness checks plus the add/remove overlap
// rejection that the table-driven Validate test does not reach.
func TestValidateStateUpgrades_BlockAndListBranches(t *testing.T) {
	cases := []struct {
		name string
		su   StateUpgradeConfig
		want string
	}{
		{
			name: "empty block rename old",
			su:   StateUpgradeConfig{From: 0, BlockRenames: map[string]string{"": "block"}},
			want: "empty old block name",
		},
		{
			name: "empty block rename new",
			su:   StateUpgradeConfig{From: 0, BlockRenames: map[string]string{"block": ""}},
			want: "empty new block name",
		},
		{
			name: "empty added attribute",
			su:   StateUpgradeConfig{From: 0, AddedAttributes: []string{""}},
			want: "added_attributes contains empty name",
		},
		{
			name: "empty added block",
			su:   StateUpgradeConfig{From: 0, AddedBlocks: []string{""}},
			want: "added_blocks contains empty name",
		},
		{
			name: "empty removed attribute",
			su:   StateUpgradeConfig{From: 0, RemovedAttributes: []string{""}},
			want: "removed_attributes contains empty name",
		},
		{
			name: "empty removed block",
			su:   StateUpgradeConfig{From: 0, RemovedBlocks: []string{""}},
			want: "removed_blocks contains empty name",
		},
		{
			name: "added and removed attribute overlap",
			su: StateUpgradeConfig{From: 0,
				AddedAttributes:   []string{"name"},
				RemovedAttributes: []string{"name"},
			},
			want: "added_attributes and removed_attributes",
		},
		{
			name: "added and removed block overlap",
			su: StateUpgradeConfig{From: 0,
				AddedBlocks:   []string{"tags"},
				RemovedBlocks: []string{"tags"},
			},
			want: "added_blocks and removed_blocks",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateStateUpgrades(1, []StateUpgradeConfig{tc.su}, 3)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want substring %q", err, tc.want)
			}
		})
	}
	// Empty upgrades short-circuit to nil.
	if err := validateStateUpgrades(0, nil, 0); err != nil {
		t.Errorf("nil upgrades = %v, want nil", err)
	}
}

// TestValidateNoAddRemoveOverlap covers the attribute overlap, block overlap,
// and clean passes directly.
func TestValidateNoAddRemoveOverlap(t *testing.T) {
	if err := validateNoAddRemoveOverlap(StateUpgradeConfig{
		AddedAttributes: []string{"a"}, RemovedAttributes: []string{"a"},
	}, 1, 0); err == nil {
		t.Error("attribute overlap should error")
	}
	if err := validateNoAddRemoveOverlap(StateUpgradeConfig{
		AddedBlocks: []string{"b"}, RemovedBlocks: []string{"b"},
	}, 1, 0); err == nil {
		t.Error("block overlap should error")
	}
	if err := validateNoAddRemoveOverlap(StateUpgradeConfig{
		AddedAttributes: []string{"a"}, RemovedAttributes: []string{"b"},
		AddedBlocks: []string{"c"}, RemovedBlocks: []string{"d"},
	}, 1, 0); err != nil {
		t.Errorf("no overlap should pass, got %v", err)
	}
}

// TestWriteStarterGeneratorConfigBytes_MkdirAllError asserts a path whose
// parent is a regular file surfaces the MkdirAll failure.
func TestWriteStarterGeneratorConfigBytes_MkdirAllError(t *testing.T) {
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	err := WriteStarterGeneratorConfigBytes(filepath.Join(blocker, "out.yaml"), []byte("x"), false)
	if err == nil || !strings.Contains(err.Error(), "failed to create output directory") {
		t.Errorf("err = %v, want MkdirAll error", err)
	}
}

// TestWriteStarterGeneratorConfigBytes_CreateTempError asserts an unwritable
// output directory surfaces the temp-file creation failure.
func TestWriteStarterGeneratorConfigBytes_CreateTempError(t *testing.T) {
	tmp := t.TempDir()
	ro := filepath.Join(tmp, "ro")
	if err := os.MkdirAll(ro, 0o750); err != nil {
		t.Fatalf("mkdir ro: %v", err)
	}
	if err := os.Chmod(ro, 0o555); err != nil { //nolint:gosec // G302: read-only dir mode is intentional to force the temp-file creation error
		t.Fatalf("chmod ro: %v", err)
	}
	//nolint:errcheck // restore perms so t.TempDir cleanup can remove the dir
	defer os.Chmod(ro, 0o755) //nolint:gosec // G302: restoring the dir to a removable mode is intentional

	err := WriteStarterGeneratorConfigBytes(filepath.Join(ro, "out.yaml"), []byte("x"), true)
	if err == nil {
		t.Error("expected temp-file creation error on a read-only dir")
	}
}

// TestWriteStarterGeneratorConfigBytes_RenameError asserts a force write whose
// target is an existing directory fails and cleans up the temp file (the
// atomic-rename failure path).
func TestWriteStarterGeneratorConfigBytes_RenameError(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "existing-dir")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	err := WriteStarterGeneratorConfigBytes(dir, []byte("x"), true)
	if err == nil {
		t.Error("expected rename error onto an existing directory")
	}
}
