package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

func TestRecorderCollectsEntries(t *testing.T) {
	rec := NewRecorder()
	rec.Record("main.go", "entrypoint")
	rec.Record("provider.go", "provider schema")

	if rec.Len() != 2 {
		t.Fatalf("expected 2 entries, got %d", rec.Len())
	}

	entries := rec.Entries()
	if entries[0].Path != "main.go" || entries[0].Reason != "entrypoint" {
		t.Fatalf("unexpected first entry: %+v", entries[0])
	}
	if entries[1].Path != "provider.go" || entries[1].Reason != "provider schema" {
		t.Fatalf("unexpected second entry: %+v", entries[1])
	}
}

func TestRecorderDeduplicatesByPath(t *testing.T) {
	rec := NewRecorder()
	rec.Record("main.go", "first reason")
	rec.Record("main.go", "second reason")

	if rec.Len() != 1 {
		t.Fatalf("expected duplicate path to be ignored, got %d entries", rec.Len())
	}
	if rec.Entries()[0].Reason != "first reason" {
		t.Fatalf("expected first reason to be retained, got %q", rec.Entries()[0].Reason)
	}
}

func TestRecorderIgnoresEmptyPath(t *testing.T) {
	rec := NewRecorder()
	rec.Record("", "no path")
	if rec.Len() != 0 {
		t.Fatalf("expected empty path to be ignored, got %d entries", rec.Len())
	}
}

func TestRecorderNilSafe(t *testing.T) {
	var rec *Recorder
	rec.Record("main.go", "should not panic")
	if rec.Len() != 0 {
		t.Fatalf("nil recorder should report 0 entries")
	}
	if rec.Entries() != nil {
		t.Fatalf("nil recorder should return nil entries")
	}
	rec.Sort()
}

func TestRecorderSort(t *testing.T) {
	rec := NewRecorder()
	rec.Record("z.go", "last")
	rec.Record("a.go", "first")
	rec.Sort()

	entries := rec.Entries()
	if entries[0].Path != "a.go" {
		t.Fatalf("expected a.go first, got %s", entries[0].Path)
	}
	if entries[1].Path != "z.go" {
		t.Fatalf("expected z.go last, got %s", entries[1].Path)
	}
}

func TestRecorderEntriesCopy(t *testing.T) {
	rec := NewRecorder()
	rec.Record("main.go", "entrypoint")

	entries := rec.Entries()
	entries[0].Path = "changed.go"

	if rec.Entries()[0].Path != "main.go" {
		t.Fatalf("Entries() must return a copy")
	}
}

func TestCollectFromProviderIR_Nil(t *testing.T) {
	opts := DefaultCollectOptions()
	entries := CollectFromProviderIR(nil, opts)

	paths := pathsFrom(entries)
	want := []string{
		"internal/client/client.go",
		"internal/client/models.go",
		"internal/client/errors.go",
		"internal/client/retry.go",
		"internal/client/pagination.go",
		"internal/client/logging.go",
		"internal/provider/provider.go",
		"internal/provider/provider_test.go",
		"main.go",
		"GNUmakefile",
		"README.md",
		".goreleaser.yml",
		".github/workflows/release.yml",
		"terraform-registry-manifest.json",
		"docs/index.md",
		"generator.yaml",
	}
	assertContainsPaths(t, paths, want)
	// A nil provider has no security schemes, so auth.go must not be recorded
	// (H-13: record mode previously invented auth.go for auth-less APIs).
	assertNotContains(t, paths, "internal/client/auth.go")
	assertNotContains(t, paths, "Makefile")
}

func TestCollectFromProviderIR_FullProvider(t *testing.T) {
	provider := &ir.ProviderIR{
		Name:    "mycloud",
		Version: "0.1.0",
		Resources: []ir.ResourceIR{
			{Name: "pet", FullName: "Pet"},
		},
		DataSources: []ir.DataSourceIR{
			{Name: "pet", FullName: "Pet Data Source"},
		},
		Actions: []ir.ActionIR{
			{Name: "rebootServer", FullName: "Reboot Server"},
		},
		EphemeralResources: []ir.EphemeralResourceIR{
			{Name: "temporaryCredential", FullName: "Temporary Credential"},
		},
		ListResources: []ir.ListResourceIR{
			{Name: "pet", FullName: "Pet List"},
		},
		Functions: []ir.FunctionIR{
			{Name: "ipLookup", FullName: "IP Lookup"},
		},
	}

	opts := DefaultCollectOptions()
	entries := CollectFromProviderIR(provider, opts)
	paths := pathsFrom(entries)

	want := []string{
		"internal/provider/provider.go",
		"internal/provider/resource_pet.go",
		"internal/provider/data_source_pet.go",
		"internal/provider/action_rebootserver.go",
		"internal/provider/ephemeral_temporarycredential.go",
		"internal/provider/list_pet.go",
		"internal/provider/function_iplookup.go",
		"internal/provider/validators.go",
		"docs/resources/pet.md",
		"docs/data-sources/pet.md",
		"docs/actions/rebootserver.md",
		"docs/ephemeral-resources/temporarycredential.md",
		"docs/list-resources/pet.md",
		"docs/functions/iplookup.md",
		"examples/resources/pet/resource.tf",
		"examples/data-sources/pet/data-source.tf",
		"examples/actions/rebootserver/action.tf",
		"examples/ephemeral-resources/temporarycredential/ephemeral-resource.tf",
		"GNUmakefile",
	}
	assertContainsPaths(t, paths, want)
	assertContains(t, paths, "docs/index.md")
	assertContains(t, paths, "generator.yaml")
	// This provider declares no security schemes, so auth.go is not emitted
	// (H-13) and the writer's full client set must be present without it.
	assertNotContains(t, paths, "internal/client/auth.go")
	assertContainsPaths(t, paths, []string{
		"internal/client/client.go",
		"internal/client/models.go",
		"internal/client/errors.go",
		"internal/client/retry.go",
		"internal/client/pagination.go",
		"internal/client/logging.go",
	})
	assertNotContains(t, paths, "Makefile")

	reasons := reasonsByPath(entries)
	if got := reasons["internal/provider/resource_pet.go"]; got != "resource Pet" {
		t.Fatalf("unexpected resource reason: %q", got)
	}
	if got := reasons["internal/provider/action_rebootserver.go"]; got != "action Reboot Server" {
		t.Fatalf("unexpected action reason: %q", got)
	}
}

func TestCollectFromProviderIR_Options(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{{Name: "pet"}},
	}

	// Minimal options: only source files, no tests/docs/examples/config.
	opts := CollectOptions{}
	entries := CollectFromProviderIR(provider, opts)
	paths := pathsFrom(entries)

	assertContains(t, paths, "internal/provider/resource_pet.go")
	assertNotContains(t, paths, "internal/provider/provider_test.go")
	assertNotContains(t, paths, "docs/resources/pet.md")
	assertNotContains(t, paths, "examples/resources/pet/resource.tf")
	assertNotContains(t, paths, "generator.yaml")

	// Terraform tests are opt-in.
	opts = CollectOptions{IncludeTerraformTests: true}
	entries = CollectFromProviderIR(provider, opts)
	paths = pathsFrom(entries)
	assertContains(t, paths, "tests/pet.tftest.hcl")
}

func TestCollectFromProviderIR_RecordModeDoesNotWriteFiles(t *testing.T) {
	tmp := t.TempDir()
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{{Name: "pet"}},
	}

	// CollectFromProviderIR takes no output directory; this test verifies the
	// recorder does not write to the filesystem regardless of any temp directory.
	entries := CollectFromProviderIR(provider, DefaultCollectOptions())
	if len(entries) == 0 {
		t.Fatal("expected non-empty file list in record mode")
	}

	// Confirm that no files were created under tmp.
	found, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatalf("failed to read temp dir: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("record mode wrote files to temp dir: %v", found)
	}

	// Also confirm the recorder itself never creates paths on disk.
	rec := NewRecorder()
	rec.Record(filepath.Join(tmp, "should_not_exist.go"), "reason")
	if _, err := os.Stat(filepath.Join(tmp, "should_not_exist.go")); !os.IsNotExist(err) {
		t.Fatalf("recorder must not write files, got err=%v", err)
	}
}

func TestFileName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"pet", "pet"},
		{"rebootServer", "rebootserver"},
		{"IPLookup", "iplookup"},
		{"my-resource", "my_resource"},
		{"my resource", "my_resource"},
		{"My Resource", "my_resource"},
		{"  spaces  ", "spaces"},
		{"../../../etc/passwd", "etc_passwd"},
		{"a/../b", "a_b"},
		{"a\\b", "a_b"},
		{"", "unnamed"},
		{"   ", "unnamed"},
		{"...", "unnamed"},
	}
	for _, c := range cases {
		if got := fileName(c.in); got != c.want {
			t.Errorf("fileName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRun_RecordMode(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{{Name: "pet"}},
	}

	opts := Options{Mode: ModeRecord, CollectOptions: DefaultCollectOptions()}
	entries, err := Run(provider, opts)
	if err != nil {
		t.Fatalf("unexpected error in record mode: %v", err)
	}

	paths := pathsFrom(entries)
	assertContains(t, paths, "internal/provider/resource_pet.go")
}

// TestRecordMatchesWriteMode is the central regression test for H-11, H-12,
// H-13, and H-14: the file list produced by record mode (dry-run) must exactly
// match the file list produced by write mode for the same provider and options.
// Before the fixes, the recorder used a different snake_case implementation,
// recorded "Makefile" while the writer wrote "GNUmakefile", recorded the wrong
// client file set, and omitted value_mappers_test.go plus the action and
// ephemeral example files. This test builds a provider that exercises all of
// those paths (a camelCase-named action, security schemes, resources, an
// ephemeral resource) and asserts the two file sets are identical.
func TestRecordMatchesWriteMode(t *testing.T) {
	provider := &ir.ProviderIR{
		Name:    "mycloud",
		Version: "0.1.0",
		Resources: []ir.ResourceIR{
			{Name: "pet", FullName: "Pet"},
		},
		DataSources: []ir.DataSourceIR{
			{Name: "pet", FullName: "Pet Data Source"},
		},
		Actions: []ir.ActionIR{
			{Name: "rebootServer", FullName: "Reboot Server"},
		},
		EphemeralResources: []ir.EphemeralResourceIR{
			{Name: "temporaryCredential", FullName: "Temporary Credential"},
		},
		SecurityIR: ir.SecurityIR{
			Schemes: []ir.SecuritySchemeIR{{Name: "api_key", Type: ir.SecuritySchemeAPIKey}},
		},
	}

	opts := DefaultCollectOptions()

	recordEntries := CollectFromProviderIR(provider, opts)
	recordSet := make(map[string]struct{}, len(recordEntries))
	for _, e := range recordEntries {
		recordSet[e.Path] = struct{}{}
	}

	writeFiles, err := FilesForProviderIR(provider, BuildConfigFromIR(provider), opts)
	if err != nil {
		t.Fatalf("FilesForProviderIR: %v", err)
	}
	writeSet := make(map[string]struct{}, len(writeFiles))
	for _, f := range writeFiles {
		writeSet[f.Path] = struct{}{}
	}

	// Paths the recorder lists but the writer never emits (dry-run lies).
	var extraInRecord []string
	for p := range recordSet {
		if _, ok := writeSet[p]; !ok {
			extraInRecord = append(extraInRecord, p)
		}
	}
	// Paths the writer emits but the recorder omits (dry-run is incomplete).
	var missingFromRecord []string
	for p := range writeSet {
		if _, ok := recordSet[p]; !ok {
			missingFromRecord = append(missingFromRecord, p)
		}
	}
	if len(extraInRecord) != 0 || len(missingFromRecord) != 0 {
		t.Errorf("record mode and write mode file sets diverge:\n"+
			"  recorded but not written: %v\n"+
			"  written but not recorded: %v",
			extraInRecord, missingFromRecord)
	}
}

// buildFiles is the set of build/CI/release scaffolding files gated by
// IncludeBuild. Both record and write paths must gate them identically.
var buildFiles = []string{
	"GNUmakefile",
	".goreleaser.yml",
	".github/workflows/release.yml",
	"terraform-registry-manifest.json",
}

// TestIncludeBuildGatesBuildFiles verifies that IncludeBuild controls the four
// build/CI/release scaffolding files in both record and write paths, and that
// record/write parity holds with the flag off.
func TestIncludeBuildGatesBuildFiles(t *testing.T) {
	provider := &ir.ProviderIR{Name: "mycloud", Version: "0.1.0"}
	cfg := BuildConfigFromIR(provider)

	// Default: build files present in both paths.
	defaultOpts := DefaultCollectOptions()
	for _, p := range buildFiles {
		assertContains(t, pathsFrom(CollectFromProviderIR(provider, defaultOpts)), p)
	}
	wf, err := FilesForProviderIR(provider, cfg, defaultOpts)
	if err != nil {
		t.Fatalf("FilesForProviderIR default: %v", err)
	}
	for _, p := range buildFiles {
		assertContains(t, pathsFromFile(wf), p)
	}

	// IncludeBuild=false: the four files are absent from both record and write.
	skipOpts := DefaultCollectOptions()
	skipOpts.IncludeBuild = false
	recordPaths := pathsFrom(CollectFromProviderIR(provider, skipOpts))
	wfSkip, err := FilesForProviderIR(provider, cfg, skipOpts)
	if err != nil {
		t.Fatalf("FilesForProviderIR skip: %v", err)
	}
	writePaths := pathsFromFile(wfSkip)
	for _, p := range buildFiles {
		if contains(recordPaths, p) {
			t.Errorf("record path unexpectedly includes %q with IncludeBuild=false", p)
		}
		if contains(writePaths, p) {
			t.Errorf("write path unexpectedly includes %q with IncludeBuild=false", p)
		}
	}
	// Record/write parity must still hold with build files excluded.
	recordSet := setOf(recordPaths)
	writeSet := setOf(writePaths)
	for p := range recordSet {
		if _, ok := writeSet[p]; !ok {
			t.Errorf("recorded but not written with IncludeBuild=false: %s", p)
		}
	}
	for p := range writeSet {
		if _, ok := recordSet[p]; !ok {
			t.Errorf("written but not recorded with IncludeBuild=false: %s", p)
		}
	}

	// OnlyBuild: both paths emit exactly the four scaffolding files and nothing
	// else, so record/write parity holds and no core file leaks through.
	onlyRecord := pathsFrom(CollectFromProviderIR(provider, CollectOptions{OnlyBuild: true}))
	onlyWrite, err := FilesForProviderIR(provider, cfg, CollectOptions{OnlyBuild: true})
	if err != nil {
		t.Fatalf("FilesForProviderIR only-build: %v", err)
	}
	onlyWritePaths := pathsFromFile(onlyWrite)
	if len(onlyRecord) != len(buildFiles) || len(onlyWritePaths) != len(buildFiles) {
		t.Errorf("OnlyBuild should emit %d files, got record=%d write=%d",
			len(buildFiles), len(onlyRecord), len(onlyWritePaths))
	}
	for _, p := range buildFiles {
		if !contains(onlyRecord, p) {
			t.Errorf("OnlyBuild record missing %q: %v", p, onlyRecord)
		}
		if !contains(onlyWritePaths, p) {
			t.Errorf("OnlyBuild write missing %q: %v", p, onlyWritePaths)
		}
	}
	for _, p := range []string{"go.mod", "README.md", "main.go", "internal/provider/provider.go"} {
		if contains(onlyRecord, p) || contains(onlyWritePaths, p) {
			t.Errorf("OnlyBuild should not emit core file %q", p)
		}
	}
}

// dynamicReleaseWorkflowPath is the opt-in regenerate-and-release workflow
// gated by IncludeDynamicRelease. Default-off so golden snapshots stay
// byte-identical; both record and write paths must gate it identically.
const dynamicReleaseWorkflowPath = ".github/workflows/regenerate-and-release.yml"

// TestDynamicReleaseOptIn verifies that IncludeDynamicRelease gates the
// regenerate-and-release workflow in both record and write paths, that it is
// absent by default (so goldens are unchanged), and that record/write parity
// holds in both states.
func TestDynamicReleaseOptIn(t *testing.T) {
	provider := &ir.ProviderIR{Name: "mycloud", Version: "0.1.0"}
	cfg := BuildConfigFromIR(provider)

	// Default-off: the workflow is absent from both paths.
	defaultOpts := DefaultCollectOptions()
	if contains(pathsFrom(CollectFromProviderIR(provider, defaultOpts)), dynamicReleaseWorkflowPath) {
		t.Errorf("default record path unexpectedly includes dynamic release workflow")
	}
	wf, err := FilesForProviderIR(provider, cfg, defaultOpts)
	if err != nil {
		t.Fatalf("FilesForProviderIR default: %v", err)
	}
	if contains(pathsFromFile(wf), dynamicReleaseWorkflowPath) {
		t.Errorf("default write path unexpectedly includes dynamic release workflow")
	}

	// Opt-in: the workflow is present in both paths, and parity holds.
	onOpts := DefaultCollectOptions()
	onOpts.IncludeDynamicRelease = true
	recordPaths := pathsFrom(CollectFromProviderIR(provider, onOpts))
	wfOn, err := FilesForProviderIR(provider, cfg, onOpts)
	if err != nil {
		t.Fatalf("FilesForProviderIR dynamic-release: %v", err)
	}
	writePaths := pathsFromFile(wfOn)
	if !contains(recordPaths, dynamicReleaseWorkflowPath) {
		t.Errorf("record path missing dynamic release workflow with IncludeDynamicRelease=true: %v", recordPaths)
	}
	if !contains(writePaths, dynamicReleaseWorkflowPath) {
		t.Errorf("write path missing dynamic release workflow with IncludeDynamicRelease=true: %v", writePaths)
	}
	recordSet := setOf(recordPaths)
	writeSet := setOf(writePaths)
	for p := range recordSet {
		if _, ok := writeSet[p]; !ok {
			t.Errorf("recorded but not written with IncludeDynamicRelease=true: %s", p)
		}
	}
	for p := range writeSet {
		if _, ok := recordSet[p]; !ok {
			t.Errorf("written but not recorded with IncludeDynamicRelease=true: %s", p)
		}
	}
}

// TestOnlyBuildWithDynamicRelease verifies that --only-build honors
// IncludeDynamicRelease: the regenerate-and-release workflow is emitted in both
// record and write paths alongside the static scaffolding, and record/write
// parity holds. Without M-78, OnlyBuild short-circuited before the dynamic
// workflow and the flag was silently dropped (regression guard for M-78).
func TestOnlyBuildWithDynamicRelease(t *testing.T) {
	provider := &ir.ProviderIR{Name: "mycloud", Version: "0.1.0"}
	cfg := BuildConfigFromIR(provider)

	opts := CollectOptions{OnlyBuild: true, IncludeDynamicRelease: true}

	recordPaths := pathsFrom(CollectFromProviderIR(provider, opts))
	wf, err := FilesForProviderIR(provider, cfg, opts)
	if err != nil {
		t.Fatalf("FilesForProviderIR only-build+dynamic: %v", err)
	}
	writePaths := pathsFromFile(wf)

	// The five static scaffolding files are present.
	for _, p := range []string{"GNUmakefile", ".goreleaser.yml", ".github/workflows/release.yml", "terraform-registry-manifest.json", ".gitignore"} {
		if !contains(recordPaths, p) {
			t.Errorf("record path missing scaffolding %q: %v", p, recordPaths)
		}
		if !contains(writePaths, p) {
			t.Errorf("write path missing scaffolding %q: %v", p, writePaths)
		}
	}
	// The dynamic workflow is present in both paths.
	if !contains(recordPaths, dynamicReleaseWorkflowPath) {
		t.Errorf("record path missing dynamic release workflow under --only-build: %v", recordPaths)
	}
	if !contains(writePaths, dynamicReleaseWorkflowPath) {
		t.Errorf("write path missing dynamic release workflow under --only-build: %v", writePaths)
	}
	// Nothing else leaks through: only the scaffolding + the dynamic workflow.
	if len(recordPaths) != 6 {
		t.Errorf("record path should emit exactly 6 files, got %d: %v", len(recordPaths), recordPaths)
	}
	// record/write parity.
	recordSet, writeSet := setOf(recordPaths), setOf(writePaths)
	for p := range recordSet {
		if _, ok := writeSet[p]; !ok {
			t.Errorf("recorded but not written under --only-build: %s", p)
		}
	}
	for p := range writeSet {
		if _, ok := recordSet[p]; !ok {
			t.Errorf("written but not recorded under --only-build: %s", p)
		}
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func setOf(xs []string) map[string]struct{} {
	m := make(map[string]struct{}, len(xs))
	for _, x := range xs {
		m[x] = struct{}{}
	}
	return m
}

func pathsFromFile(fs []File) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Path)
	}
	return out
}

func TestRun_WriteMode(t *testing.T) {
	opts := Options{Mode: ModeWrite, OutputDir: t.TempDir()}
	entries, err := Run(&ir.ProviderIR{Name: "test"}, opts)
	if err != nil {
		t.Fatalf("unexpected error in write mode: %v", err)
	}
	assertContains(t, pathsFrom(entries), "internal/provider/provider.go")
}

// TestRun_WriteModeReturnsFilesActuallyWritten locks in the N-30 fix: the
// write-mode return value is built from the files actually written to disk,
// not re-run through the record-mode collector. Every returned entry must have
// a real file on disk, and every file written must appear in the returned
// plan, so a future divergence between FilesForProviderIR and the collector is
// visible instead of silently producing a plan that does not match disk.
func TestRun_WriteModeReturnsFilesActuallyWritten(t *testing.T) {
	out := t.TempDir()
	opts := Options{Mode: ModeWrite, OutputDir: out, CollectOptions: DefaultCollectOptions()}
	entries, err := Run(&ir.ProviderIR{Name: "test"}, opts)
	if err != nil {
		t.Fatalf("unexpected error in write mode: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("write mode returned no entries")
	}

	returned := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		returned[e.Path] = struct{}{}
		if _, statErr := os.Stat(filepath.Join(out, filepath.FromSlash(e.Path))); statErr != nil {
			t.Errorf("returned entry %q has no file on disk: %v", e.Path, statErr)
		}
	}

	onDisk := collectPaths(t, out)
	for _, p := range onDisk {
		if p == manifestName {
			// .eidos-generated.json is write-mode bookkeeping, not a provider
			// deliverable, so it is deliberately outside the returned plan
			// (N-70). Every other written file must appear in the plan.
			continue
		}
		if _, ok := returned[p]; !ok {
			t.Errorf("file %q was written to disk but is missing from the write-mode plan (N-30)", p)
		}
	}
}

func TestRun_WriteModeRefusesOverwrite(t *testing.T) {
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "go.mod"), []byte("existing"), 0o600); err != nil {
		t.Fatalf("failed to create existing file: %v", err)
	}

	opts := Options{Mode: ModeWrite, OutputDir: out}
	_, err := Run(&ir.ProviderIR{Name: "test"}, opts)
	if err == nil {
		t.Fatal("expected write mode to refuse overwriting an existing file")
	}
}

func TestRun_WriteModeForceOverwrites(t *testing.T) {
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "go.mod"), []byte("existing"), 0o600); err != nil {
		t.Fatalf("failed to create existing file: %v", err)
	}

	opts := Options{Mode: ModeWrite, OutputDir: out, Force: true}
	_, err := Run(&ir.ProviderIR{Name: "test"}, opts)
	if err != nil {
		t.Fatalf("unexpected error in write mode with force: %v", err)
	}
}

// TestRun_WriteModeForceRemovesStaleFiles locks in the N-70 fix: a forced
// regeneration deletes the files a previous write-mode run generated that the
// current spec no longer produces (a renamed/removed resource), prunes the
// now-empty directories (the removed resource's example subdir), and never
// touches a hand-written file.
func TestRun_WriteModeForceRemovesStaleFiles(t *testing.T) {
	out := t.TempDir()
	opts := Options{Mode: ModeWrite, OutputDir: out, Force: true, CollectOptions: DefaultCollectOptions()}

	// First run: a provider with resource "pet".
	first := &ir.ProviderIR{Name: "test", Resources: []ir.ResourceIR{
		{Name: "pet", TypeName: "test_pet"},
	}}
	if _, err := Run(first, opts); err != nil {
		t.Fatalf("first write run: %v", err)
	}
	oldFile := "internal/provider/resource_pet.go"
	oldExampleDir := filepath.Join(out, "examples", "resources", "pet")
	if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(oldFile))); err != nil {
		t.Fatalf("first run did not write %q: %v", oldFile, err)
	}
	if _, err := os.Stat(filepath.Join(oldExampleDir, "resource.tf")); err != nil {
		t.Fatalf("first run did not write the example under %q: %v", oldExampleDir, err)
	}

	// A hand-written file at the output root must survive stale cleanup even
	// though it is in no plan.
	handwritten := "custom.go"
	if err := os.WriteFile(filepath.Join(out, handwritten), []byte("// user file\n"), 0o600); err != nil {
		t.Fatalf("create handwritten file: %v", err)
	}

	// Second run: resource renamed "pet" -> "cat" (same spec shape, new name).
	second := &ir.ProviderIR{Name: "test", Resources: []ir.ResourceIR{
		{Name: "cat", TypeName: "test_cat"},
	}}
	if _, err := Run(second, opts); err != nil {
		t.Fatalf("second write run: %v", err)
	}

	if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(oldFile))); !os.IsNotExist(err) {
		t.Errorf("stale %q was not removed by the forced regeneration (N-70)", oldFile)
	}
	if _, err := os.Stat(oldExampleDir); !os.IsNotExist(err) {
		t.Errorf("empty example directory %q was not pruned after stale removal", oldExampleDir)
	}
	if _, err := os.Stat(filepath.Join(out, handwritten)); err != nil {
		t.Errorf("hand-written file %q was deleted by stale cleanup: %v", handwritten, err)
	}

	// The manifest is refreshed to the current run's file set (and excludes the
	// manifest itself).
	prev, err := readGenerationManifest(out)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if prev.Mode != generationModeFull {
		t.Errorf("manifest mode = %q, want %q", prev.Mode, generationModeFull)
	}
	for _, want := range []string{"internal/provider/resource_cat.go", "internal/provider/provider.go"} {
		if !containsStr(prev.Generated, want) {
			t.Errorf("manifest does not list %q: %v", want, prev.Generated)
		}
	}
	if containsStr(prev.Generated, manifestName) {
		t.Errorf("manifest must not list itself: %v", prev.Generated)
	}
	if containsStr(prev.Generated, oldFile) {
		t.Errorf("manifest still lists stale %q: %v", oldFile, prev.Generated)
	}
}

// TestRun_WriteModeOnlyBuildKeepsProviderCode locks in the mode scoping of
// N-70: an --only-build forced run writes only the build/CI/release scaffolding
// and must not delete the provider files a previous full run recorded — the two
// workflows write disjoint file sets on purpose.
func TestRun_WriteModeOnlyBuildKeepsProviderCode(t *testing.T) {
	out := t.TempDir()
	opts := Options{Mode: ModeWrite, OutputDir: out, Force: true, CollectOptions: DefaultCollectOptions()}
	provider := &ir.ProviderIR{Name: "test", Resources: []ir.ResourceIR{
		{Name: "pet", TypeName: "test_pet"},
	}}
	if _, err := Run(provider, opts); err != nil {
		t.Fatalf("full write run: %v", err)
	}
	providerFile := "internal/provider/resource_pet.go"
	if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(providerFile))); err != nil {
		t.Fatalf("full run did not write %q: %v", providerFile, err)
	}

	onlyBuildOpts := Options{Mode: ModeWrite, OutputDir: out, Force: true, CollectOptions: CollectOptions{OnlyBuild: true}}
	if _, err := Run(provider, onlyBuildOpts); err != nil {
		t.Fatalf("only-build write run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(providerFile))); err != nil {
		t.Errorf("only-build forced run deleted provider file %q (N-70 mode scoping)", providerFile)
	}
	if _, err := os.Stat(filepath.Join(out, "GNUmakefile")); err != nil {
		t.Errorf("only-build run did not write GNUmakefile: %v", err)
	}
}

// TestFilePathsUseForwardSlashes locks in the M-4 contract: every File.Path the
// generator emits is a relative path spelled with forward slashes, as
// documented in harness.go. Before the fix the emitters built paths with
// filepath.Join, which produces backslashes on Windows — breaking record/write
// matching (the record plan is forward-slash-keyed), the N-30 lockstep, and
// byte-identical cross-OS output. The test exercises every emitter family
// (resource, data source, action, ephemeral, list resource, function, examples,
// tftest modules, coverage tests) and asserts the forward-slash contract plus
// record/write path parity.
func TestFilePathsUseForwardSlashes(t *testing.T) {
	provider := &ir.ProviderIR{
		Name:    "mycloud",
		Version: "0.1.0",
		Resources: []ir.ResourceIR{
			{Name: "pet", TypeName: "mycloud_pet"},
		},
		DataSources: []ir.DataSourceIR{
			{Name: "pet", TypeName: "mycloud_pet"},
		},
		Actions: []ir.ActionIR{
			{Name: "scrap", TypeName: "mycloud_scrap"},
		},
		EphemeralResources: []ir.EphemeralResourceIR{
			{Name: "token", TypeName: "mycloud_token"},
		},
		ListResources: []ir.ListResourceIR{
			{Name: "pet", TypeName: "mycloud_pet"},
		},
		Functions: []ir.FunctionIR{
			{Name: "add", TypeName: "mycloud_add"},
		},
	}
	opts := DefaultCollectOptions()
	opts.IncludeTerraformTests = true
	opts.IncludeDynamicRelease = true

	cfg := BuildConfigFromIR(provider)
	files, err := FilesForProviderIR(provider, cfg, opts)
	if err != nil {
		t.Fatalf("FilesForProviderIR: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("FilesForProviderIR returned no files")
	}

	// Every emitted path must be relative, forward-slash, and stable under a
	// round-trip through the OS path conversion the harness applies on write.
	for _, f := range files {
		if f.Path == "" {
			t.Errorf("empty File.Path")
			continue
		}
		if strings.ContainsRune(f.Path, '\\') {
			t.Errorf("File.Path %q contains a backslash; must use forward slashes (M-4)", f.Path)
		}
		if filepath.IsAbs(f.Path) || filepath.VolumeName(f.Path) != "" {
			t.Errorf("File.Path %q is absolute; must be relative (M-4)", f.Path)
		}
		if round := filepath.ToSlash(filepath.FromSlash(f.Path)); round != f.Path {
			t.Errorf("File.Path %q does not round-trip through filepath.FromSlash/ToSlash (got %q) (M-4)", f.Path, round)
		}
	}

	// Record/write lockstep: the write-mode path set must equal the record-mode
	// plan exactly, so a Windows-spelled path can never silently diverge (M-4).
	// Compared as sets because the two collectors are not required to agree on
	// ordering — only on the exact set of paths.
	record := pathsFrom(CollectFromProviderIR(provider, opts))
	write := pathsFromFile(files)
	if len(record) != len(write) {
		t.Fatalf("record/write path sets differ in size: record=%d write=%d (M-4)", len(record), len(write))
	}
	writeSet := make(map[string]struct{}, len(write))
	for _, p := range write {
		writeSet[p] = struct{}{}
	}
	for _, p := range record {
		if _, ok := writeSet[p]; !ok {
			t.Errorf("record path %q missing from write set (M-4)", p)
		}
	}
}

// TestRun_WriteModeOnlyBuildPreservesFullManifest locks in the M-3 fix: an
// --only-build refresh must not clobber the full-mode manifest, so a later full
// run can still delete the stale provider files the first full run recorded.
// Before the fix the only-build run overwrote the manifest with its four build
// files, the next full run saw a mode mismatch and silently skipped cleanup,
// and the orphaned resource file was in no manifest — so no later run ever
// deleted it.
func TestRun_WriteModeOnlyBuildPreservesFullManifest(t *testing.T) {
	out := t.TempDir()
	fullOpts := Options{Mode: ModeWrite, OutputDir: out, Force: true, CollectOptions: DefaultCollectOptions()}
	pet := &ir.ProviderIR{Name: "test", Resources: []ir.ResourceIR{
		{Name: "pet", TypeName: "test_pet"},
	}}
	if _, err := Run(pet, fullOpts); err != nil {
		t.Fatalf("first full write run: %v", err)
	}
	oldFile := "internal/provider/resource_pet.go"
	if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(oldFile))); err != nil {
		t.Fatalf("first full run did not write %q: %v", oldFile, err)
	}

	// An --only-build --force refresh in between must not delete the provider
	// file (N-70 mode scoping) and must not clobber the full-mode manifest.
	onlyBuildOpts := Options{Mode: ModeWrite, OutputDir: out, Force: true, CollectOptions: CollectOptions{OnlyBuild: true}}
	if _, err := Run(pet, onlyBuildOpts); err != nil {
		t.Fatalf("only-build write run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(oldFile))); err != nil {
		t.Fatalf("only-build run deleted provider file %q (N-70 mode scoping)", oldFile)
	}
	prev, err := readGenerationManifest(out)
	if err != nil {
		t.Fatalf("read manifest after only-build: %v", err)
	}
	if !containsStr(prev.ByMode[generationModeFull], oldFile) {
		t.Errorf("only-build run clobbered the full-mode manifest: %q missing from ByMode[full] = %v (M-3)", oldFile, prev.ByMode[generationModeFull])
	}
	if len(prev.ByMode[generationModeOnlyBuild]) == 0 {
		t.Errorf("only-build run did not record its own bookkeeping: ByMode[only-build] = %v", prev.ByMode[generationModeOnlyBuild])
	}

	// Second full run with the resource renamed pet -> cat: the stale pet file
	// from the FIRST full run must still be removed, proving the full-mode
	// bookkeeping survived the only-build refresh.
	cat := &ir.ProviderIR{Name: "test", Resources: []ir.ResourceIR{
		{Name: "cat", TypeName: "test_cat"},
	}}
	if _, err := Run(cat, fullOpts); err != nil {
		t.Fatalf("second full write run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(oldFile))); !os.IsNotExist(err) {
		t.Errorf("stale %q was not removed by the second full run (M-3: only-build clobbered the full manifest)", oldFile)
	}
	if _, err := os.Stat(filepath.Join(out, "internal", "provider", "resource_cat.go")); err != nil {
		t.Errorf("second full run did not write resource_cat.go: %v", err)
	}
}

// TestRun_WriteModeForceWithoutManifestSkipsCleanup verifies a forced run into a
// directory that was never generated before (no manifest) succeeds and leaves
// unrelated pre-existing files alone.
func TestRun_WriteModeForceWithoutManifestSkipsCleanup(t *testing.T) {
	out := t.TempDir()
	unrelated := "notes.txt"
	if err := os.WriteFile(filepath.Join(out, unrelated), []byte("keep me"), 0o600); err != nil {
		t.Fatalf("create unrelated file: %v", err)
	}
	opts := Options{Mode: ModeWrite, OutputDir: out, Force: true, CollectOptions: DefaultCollectOptions()}
	if _, err := Run(&ir.ProviderIR{Name: "test"}, opts); err != nil {
		t.Fatalf("forced write run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, unrelated)); err != nil {
		t.Errorf("unrelated file %q was deleted by stale cleanup: %v", unrelated, err)
	}
}

func containsStr(slice []string, want string) bool {
	for _, s := range slice {
		if s == want {
			return true
		}
	}
	return false
}

func pathsFrom(entries []FileEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Path
	}
	return out
}

func reasonsByPath(entries []FileEntry) map[string]string {
	m := make(map[string]string, len(entries))
	for _, e := range entries {
		m[e.Path] = e.Reason
	}
	return m
}

func assertContainsPaths(t *testing.T, paths, want []string) {
	t.Helper()
	set := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		set[p] = struct{}{}
	}
	for _, w := range want {
		if _, ok := set[w]; !ok {
			t.Fatalf("expected path %q in entries; got %v", w, paths)
		}
	}
}

func assertContains(t *testing.T, paths []string, want string) {
	t.Helper()
	for _, p := range paths {
		if p == want {
			return
		}
	}
	t.Fatalf("expected path %q in %v", want, paths)
}

func assertNotContains(t *testing.T, paths []string, unwanted string) {
	t.Helper()
	for _, p := range paths {
		if p == unwanted {
			t.Fatalf("unexpected path %q in %v", unwanted, paths)
		}
	}
}
