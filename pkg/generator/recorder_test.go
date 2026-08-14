package generator

import (
	"os"
	"path/filepath"
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
	rec.Recordf("main.go", "formatted %s", "reason")
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

func TestRun_WriteMode(t *testing.T) {
	opts := Options{Mode: ModeWrite, OutputDir: t.TempDir()}
	entries, err := Run(&ir.ProviderIR{Name: "test"}, opts)
	if err != nil {
		t.Fatalf("unexpected error in write mode: %v", err)
	}
	assertContains(t, pathsFrom(entries), "internal/provider/provider.go")
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
