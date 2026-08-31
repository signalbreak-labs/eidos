// Package generator emits Terraform provider source code, documentation, tests,
// and release artifacts.
//
// The recorder types in this file implement the "record mode" used by dry-run
// and other preview paths. In record mode the generator plans every file it
// would write and returns a deterministic list of {path, reason} entries
// without touching the filesystem.
package generator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/config"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/naming"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// FileEntry describes a single file the generator intends to emit.
type FileEntry struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Mode controls whether the generator writes files to disk or only records the
// files it would create.
type Mode int

const (
	// ModeRecord collects the planned file list without writing anything to disk.
	// This is the mode used by dry-run and other preview paths.
	ModeRecord Mode = iota
	// ModeWrite emits files to disk. It requires a non-empty OutputDir and
	// refuses to overwrite existing files unless Force is set.
	ModeWrite
)

// Options configures a single generation run.
type Options struct {
	// Mode selects record-only or write-to-disk behavior.
	Mode Mode
	// OutputDir is the target directory for emitted files in write mode.
	OutputDir string
	// Force allows write mode to overwrite existing files. When false (the
	// default), write mode refuses to overwrite any path that already exists.
	Force bool
	// CollectOptions controls which optional files are included in the plan.
	CollectOptions
	// Limits carries the generator.yaml limits: section (schema/docs/description
	// caps). Nil uses the built-in Terraform platform defaults: generation
	// refuses a provider whose estimated serialized schema exceeds the cap, and
	// write mode refuses docs/ markdown files over the Registry's per-document
	// limit (G39).
	Limits *config.LimitsConfig
}

// Run executes the generator for the supplied ProviderIR. In ModeRecord it
// returns the deterministic list of {path, reason} entries that would be
// written without creating any files. In ModeWrite it emits the files to disk
// and returns the list of entries actually written, built from the files
// passed to Harness.Generate rather than re-running the record-mode collector
// (N-30): the returned plan matches disk by construction, so a future optional
// file added to FilesForProviderIR but not the collector is surfaced as a
// written file (with an empty reason) instead of being silently missing.
//
// In ModeWrite Run also maintains an internal bookkeeping manifest
// (.eidos-generated.json) at the output root and, when opts.Force is set,
// deletes the files a previous write-mode run recorded that the current run no
// longer produces (N-70). The manifest is deliberately excluded from the
// returned plan and from record-mode collection: it is generator state, not a
// provider deliverable.
func Run(provider *ir.ProviderIR, opts Options) ([]FileEntry, error) {
	// G39: refuse to generate a provider whose estimated serialized schema
	// cannot pass `terraform init` (the CLI caps GetProviderSchema responses at
	// 64 MiB). Both record and write modes enforce the cap so a dry run fails
	// the same way a write would. Description truncation (the lever) is applied
	// before the check so a configured limit can bring a provider back under
	// the cap within a single run; it is an explicit author choice recorded in
	// generator.yaml and needs no separate diagnostic.
	ApplyDescriptionLimit(provider, EffectiveDescriptionLimit(opts.Limits))
	if sizeDiags := CheckProviderSchemaSize(provider, opts.Limits); sizeDiags.HasErrors() {
		return nil, fmt.Errorf("%s", sizeDiags)
	}
	switch opts.Mode {
	case ModeRecord:
		return CollectFromProviderIR(provider, opts.CollectOptions), nil
	case ModeWrite:
		// Validate the cheap precondition before building the full file set:
		// FilesForProviderIR renders every generated file, which is wasted work
		// when OutputDir is empty and we would reject immediately anyway (L-51).
		if opts.OutputDir == "" {
			return nil, fmt.Errorf("write mode requires a non-empty OutputDir")
		}
		cfg := BuildConfigFromIR(provider)
		files, err := FilesForProviderIR(provider, cfg, opts.CollectOptions)
		if err != nil {
			return nil, fmt.Errorf("prepare files for write mode: %w", err)
		}
		h := Harness{
			OutputDir:        opts.OutputDir,
			RefuseOverwrite:  !opts.Force,
			MaxDocsFileBytes: EffectiveDocsFileLimit(opts.Limits),
		}
		if err := h.Generate(files); err != nil {
			return nil, fmt.Errorf("write generated files: %w", err)
		}
		// N-70: with --force, delete the files a previous write-mode run generated
		// that this run no longer produces, then refresh the internal bookkeeping
		// manifest. The manifest records only paths eidos itself wrote, so stale
		// cleanup can never remove a hand-written file, and it is scoped per run
		// kind (full vs --only-build) so an --only-build refresh cannot delete the
		// provider code a full run produced (and vice versa). The manifest is
		// write-mode bookkeeping, not a provider deliverable, so it is deliberately
		// absent from both the returned plan and record-mode collection.
		currentMode := generationModeFull
		if opts.OnlyBuild {
			currentMode = generationModeOnlyBuild
		}
		planned := make([]string, 0, len(files))
		for _, f := range files {
			clean, err := safeRelPath(f.Path)
			if err != nil {
				return nil, err
			}
			planned = append(planned, clean)
		}
		sort.Strings(planned)
		prev, err := readGenerationManifest(opts.OutputDir)
		if err != nil {
			return nil, err
		}
		if _, err := removeStaleGeneratedFiles(opts.OutputDir, prev, planned, currentMode, opts.Force, opts.IncludeConfig); err != nil {
			return nil, err
		}
		// Merge the current run's bookkeeping into the manifest rather than
		// overwriting it: the other run kind's file list must survive so its
		// next run can still clean up stale files (M-3).
		if err := writeGenerationManifest(opts.OutputDir, prev.withRun(currentMode, planned)); err != nil {
			return nil, fmt.Errorf("write %s: %w", manifestName, err)
		}
		// Build the returned plan from the files actually written. Reasons come
		// from the record-mode plan so they stay identical to dry-run output
		// when record and write modes are in lockstep; a file that FilesForProviderIR
		// emits but the collector does not name is still returned (with an empty
		// reason), making any future drift visible instead of silently dropping
		// the file from the write-mode plan (N-30).
		return entriesFromFiles(files, CollectFromProviderIR(provider, opts.CollectOptions)), nil
	default:
		return nil, fmt.Errorf("unknown generator mode %d", opts.Mode)
	}
}

// entriesFromFiles builds the sorted FileEntry plan for the files actually
// written, looking up each file's reason from the record-mode plan. A written
// file with no matching plan entry keeps its path with an empty reason rather
// than being dropped, so a drift between FilesForProviderIR and the collector
// is visible in the returned plan instead of silently producing a plan that
// does not match disk (N-30).
func entriesFromFiles(files []File, plan []FileEntry) []FileEntry {
	reasonByPath := make(map[string]string, len(plan))
	for _, e := range plan {
		reasonByPath[e.Path] = e.Reason
	}
	entries := make([]FileEntry, 0, len(files))
	for _, f := range files {
		entries = append(entries, FileEntry{Path: f.Path, Reason: reasonByPath[f.Path]})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})
	return entries
}

// Recorder collects planned file entries without writing them to disk. It is
// used when the generator runs in record mode (e.g. eidos generate --dry-run).
type Recorder struct {
	entries []FileEntry
	seen    map[string]struct{}
}

// NewRecorder returns an empty Recorder ready to collect file entries.
func NewRecorder() *Recorder {
	return &Recorder{seen: make(map[string]struct{})}
}

// Record adds a file entry to the recorder. Duplicate paths are ignored so the
// resulting list is deterministic and contains one entry per output path.
func (r *Recorder) Record(path, reason string) {
	if r == nil {
		return
	}
	if path == "" {
		return
	}
	if _, ok := r.seen[path]; ok {
		return
	}
	r.seen[path] = struct{}{}
	r.entries = append(r.entries, FileEntry{Path: path, Reason: reason})
}

// Entries returns the collected file entries in the order they were recorded.
// The returned slice is a copy; callers may modify it without affecting the
// recorder.
func (r *Recorder) Entries() []FileEntry {
	if r == nil {
		return nil
	}
	out := make([]FileEntry, len(r.entries))
	copy(out, r.entries)
	return out
}

// Len returns the number of distinct file entries recorded.
func (r *Recorder) Len() int {
	if r == nil {
		return 0
	}
	return len(r.entries)
}

// Sort orders the collected entries by path for stable output.
func (r *Recorder) Sort() {
	if r == nil {
		return
	}
	sort.SliceStable(r.entries, func(i, j int) bool {
		return r.entries[i].Path < r.entries[j].Path
	})
}

// CollectOptions configures the file list produced by CollectFromProviderIR.
type CollectOptions struct {
	// IncludeTests includes generated unit/acceptance test files.
	IncludeTests bool
	// IncludeTerraformTests includes generated .tftest.hcl files.
	IncludeTerraformTests bool
	// IncludeDocs includes generated Markdown documentation.
	IncludeDocs bool
	// IncludeExamples includes generated HCL example files.
	IncludeExamples bool
	// IncludeConfig emits a generator.yaml file in the output root.
	IncludeConfig bool
	// IncludeBuild emits the build/CI/release scaffolding files: GNUmakefile,
	// .goreleaser.yml, .github/workflows/release.yml, and
	// terraform-registry-manifest.json. These are templated from BuildConfig
	// (provider name, module path, release settings) and are independent of the
	// provider's constructs. When false, a full generation run omits them so a
	// regenerated provider does not touch hand-managed release scaffolding.
	IncludeBuild bool
	// OnlyBuild short-circuits collection to emit exactly the build/CI/release
	// scaffolding files and nothing else — not even the always-on core files
	// (go.mod, README.md, main.go, provider/client packages). This supports a
	// workflow where the provider code is regenerated dynamically in CI and the
	// release scaffolding is checked in once and managed separately. When true
	// it overrides every other Include* flag except IncludeDynamicRelease, which
	// is still honored so --only-build --dynamic-release emits the
	// regenerate-and-release workflow alongside the static scaffolding (M-78).
	// OnlyBuild does not imply IncludeBuild; it emits the scaffolding directly.
	OnlyBuild bool
	// IncludeDynamicRelease emits the generated
	// .github/workflows/regenerate-and-release.yml workflow that regenerates the
	// provider from its spec and publishes a release using the eidos CI image.
	// Opt-in (off by default). When true, DynamicReleaseImage and
	// DynamicReleaseSpecPath configure the workflow's image and spec path.
	IncludeDynamicRelease bool
	// DynamicReleaseImage is the eidos CI image the generated
	// regenerate-and-release workflow runs in. Empty means the emitter applies
	// its default.
	DynamicReleaseImage string
	// DynamicReleaseSpecPath is the spec path the generated workflow regenerates
	// from. Empty means the emitter applies its default.
	DynamicReleaseSpecPath string
	// SignRelease overrides the default-on GPG signing of release artifacts.
	// It mirrors the top-level sign_release generator.yaml field (a *bool): nil
	// means "unset" → keep the BuildConfigFromIR default (signed); an explicit
	// false opts out. Applied in FilesForProviderIR before releaseFiles and the
	// dynamic-release workflow are emitted.
	SignRelease *bool
}

// DefaultCollectOptions returns the recommended collection options for a full
// generation run: tests, docs, examples, config, and build/CI/release files
// are enabled; native Terraform test files are disabled by default.
func DefaultCollectOptions() CollectOptions {
	return CollectOptions{
		IncludeTests:          true,
		IncludeTerraformTests: false,
		IncludeDocs:           true,
		IncludeExamples:       true,
		IncludeConfig:         true,
		IncludeBuild:          true,
	}
}

// CollectFromProviderIR walks a ProviderIR and records every file the generator
// would create for the provider. The returned entries are sorted by path and
// do not depend on the filesystem.
func CollectFromProviderIR(provider *ir.ProviderIR, opts CollectOptions) []FileEntry {
	rec := NewRecorder()
	// OnlyBuild short-circuits to exactly the build/CI/release scaffolding,
	// skipping the always-on core files so record and write modes emit the
	// same files. An opted-in dynamic release workflow is still recorded so
	// --only-build --dynamic-release stays in lockstep with write mode (M-78).
	if opts.OnlyBuild {
		collectBuildFiles(rec)
		if opts.IncludeDynamicRelease {
			rec.Record(".gitignore", "ignore regenerated provider files on the default branch")
			rec.Record(".github/workflows/regenerate-and-release.yml", "dynamic regenerate-and-release workflow (opt-in)")
		}
		rec.Sort()
		return rec.Entries()
	}
	collectProviderCore(rec, opts)

	if provider != nil {
		for _, res := range provider.Resources {
			collectResourceFiles(rec, res, opts)
		}
		for _, ds := range provider.DataSources {
			collectDataSourceFiles(rec, ds, opts)
		}
		for _, act := range provider.Actions {
			collectActionFiles(rec, act, opts)
		}
		for _, er := range provider.EphemeralResources {
			collectEphemeralResourceFiles(rec, er, opts)
		}
		for _, lr := range provider.ListResources {
			collectListResourceFiles(rec, lr, opts)
		}
		for _, fn := range provider.Functions {
			collectFunctionFiles(rec, fn, opts)
		}

		rec.Record("internal/provider/validators.go", "custom schema validators")

		// The shared coverage-test helpers file is emitted once when at least
		// one resource produces a coverage test file (wired and non-binary
		// upload). This mirrors TestFiles so record and write modes name the
		// same set of files.
		if opts.IncludeTests && (anyResourceCoverageEligible(provider.Resources) || anyDataSourceCoverageEligible(provider.DataSources) || anyActionCoverageEligible(provider.Actions) || anyEphemeralCoverageEligible(provider.EphemeralResources) || anyListCoverageEligible(provider.ListResources)) {
			rec.Record("internal/provider/testing_helpers_test.go", "shared httptest helpers for coverage tests")
		}

		if AnyResourceWired(provider.Resources) || AnyDataSourceWired(provider.DataSources) || AnyEphemeralWired(provider.EphemeralResources) || AnyActionSendsBody(provider.Actions) {
			rec.Record("internal/provider/json_convert.go", "JSON/model conversion helpers for wired CRUD bodies")
		}
	}

	collectClientFiles(rec, provider, opts)
	collectRootFiles(rec, provider, opts)

	rec.Sort()
	return rec.Entries()
}

func collectProviderCore(rec *Recorder, opts CollectOptions) {
	rec.Record("main.go", "provider server entrypoint")
	rec.Record("internal/provider/provider.go", "provider schema and registration")
	if opts.IncludeTests {
		rec.Record("internal/provider/provider_test.go", "provider-level unit tests")
	}
}

func collectResourceFiles(rec *Recorder, res ir.ResourceIR, opts CollectOptions) {
	name := fileName(res.Name)
	rec.Record(fmt.Sprintf("internal/provider/resource_%s.go", name), fmt.Sprintf("resource %s", displayName(res.Name, res.FullName)))
	if opts.IncludeTests {
		rec.Record(fmt.Sprintf("internal/provider/resource_%s_test.go", name), fmt.Sprintf("unit tests for resource %s", res.Name))
		// A scaffolded (unwired) resource gets no acceptance test: its CRUD
		// bodies report "is not wired to a remote API endpoint", so a lifecycle
		// test against a mock server could never pass. A resource whose wired
		// create sends a multipart/form-data binary file upload is likewise
		// skipped: the placeholder-driven mock lifecycle cannot round-trip a
		// file path. This mirrors ResourceAcceptanceTestFiles so record and
		// write modes stay in lockstep.
		if planResourceWiring(res).wired && !resourceCreateHasBinaryUpload(res) {
			rec.Record(fmt.Sprintf("internal/provider/resource_%s_acceptance_test.go", name), fmt.Sprintf("acceptance tests for resource %s", res.Name))
			// Coverage tests exercise the extracted *Remote helpers directly
			// against an httptest mock under the same eligibility gate as the
			// acceptance test, so record and write modes stay in lockstep.
			rec.Record(fmt.Sprintf("internal/provider/resource_%s_remote_test.go", name), fmt.Sprintf("coverage tests for resource %s remote helpers", res.Name))
		}
	}
	if opts.IncludeDocs {
		rec.Record(fmt.Sprintf("docs/resources/%s.md", name), fmt.Sprintf("documentation for resource %s", res.Name))
	}
	if opts.IncludeExamples {
		rec.Record(fmt.Sprintf("examples/resources/%s/resource.tf", name), fmt.Sprintf("example for resource %s", res.Name))
	}
}

func collectDataSourceFiles(rec *Recorder, ds ir.DataSourceIR, opts CollectOptions) {
	name := fileName(ds.Name)
	rec.Record(fmt.Sprintf("internal/provider/data_source_%s.go", name), fmt.Sprintf("data source %s", displayName(ds.Name, ds.FullName)))
	if opts.IncludeTests {
		rec.Record(fmt.Sprintf("internal/provider/data_source_%s_test.go", name), fmt.Sprintf("unit tests for data source %s", ds.Name))
		// Coverage tests exercise the extracted readRemote/readListRemote helpers
		// directly against an httptest mock under the same eligibility gate as
		// TestFiles (wired data sources), so record and write modes stay in
		// lockstep.
		if planDataSourceWiring(ds).wired {
			rec.Record(fmt.Sprintf("internal/provider/data_source_%s_remote_test.go", name), fmt.Sprintf("coverage tests for data source %s remote helpers", ds.Name))
		}
	}
	if opts.IncludeDocs {
		rec.Record(fmt.Sprintf("docs/data-sources/%s.md", name), fmt.Sprintf("documentation for data source %s", ds.Name))
	}
	if opts.IncludeExamples {
		rec.Record(fmt.Sprintf("examples/data-sources/%s/data-source.tf", name), fmt.Sprintf("example for data source %s", ds.Name))
	}
}

func collectActionFiles(rec *Recorder, act ir.ActionIR, opts CollectOptions) {
	name := fileName(act.Name)
	rec.Record(fmt.Sprintf("internal/provider/action_%s.go", name), fmt.Sprintf("action %s", displayName(act.Name, act.FullName)))
	if opts.IncludeTests {
		// Coverage tests exercise the extracted invokeRemote helper directly
		// against an httptest mock under the same eligibility gate as
		// TestFiles (wired actions), so record and write modes stay in
		// lockstep.
		if planActionWiring(act).wired {
			rec.Record(fmt.Sprintf("internal/provider/action_%s_remote_test.go", name), fmt.Sprintf("coverage tests for action %s remote helper", act.Name))
		}
	}
	if opts.IncludeDocs {
		rec.Record(fmt.Sprintf("docs/actions/%s.md", name), fmt.Sprintf("documentation for action %s", act.Name))
	}
	if opts.IncludeExamples {
		rec.Record(fmt.Sprintf("examples/actions/%s/action.tf", name), fmt.Sprintf("example for action %s", act.Name))
	}
}

func collectEphemeralResourceFiles(rec *Recorder, er ir.EphemeralResourceIR, opts CollectOptions) {
	name := fileName(er.Name)
	rec.Record(fmt.Sprintf("internal/provider/ephemeral_%s.go", name), fmt.Sprintf("ephemeral resource %s", displayName(er.Name, er.FullName)))
	if opts.IncludeTests {
		// Coverage tests exercise the extracted openRemote helper directly
		// against an httptest mock under the same eligibility gate as
		// TestFiles (wired ephemeral resources), so record and write modes
		// stay in lockstep.
		if planEphemeralWiring(er).wired {
			rec.Record(fmt.Sprintf("internal/provider/ephemeral_%s_remote_test.go", name), fmt.Sprintf("coverage tests for ephemeral resource %s remote helper", er.Name))
		}
	}
	if opts.IncludeDocs {
		rec.Record(fmt.Sprintf("docs/ephemeral-resources/%s.md", name), fmt.Sprintf("documentation for ephemeral resource %s", er.Name))
	}
	if opts.IncludeExamples {
		rec.Record(fmt.Sprintf("examples/ephemeral-resources/%s/ephemeral-resource.tf", name), fmt.Sprintf("example for ephemeral resource %s", er.Name))
	}
}

func collectListResourceFiles(rec *Recorder, lr ir.ListResourceIR, opts CollectOptions) {
	name := fileName(lr.Name)
	rec.Record(fmt.Sprintf("internal/provider/list_%s.go", name), fmt.Sprintf("list resource %s", displayName(lr.Name, lr.FullName)))
	if opts.IncludeTests {
		// Coverage tests exercise the extracted listRemote helper directly
		// against an httptest mock under the same eligibility gate as
		// TestFiles (wired list resources), so record and write modes stay in
		// lockstep.
		if planListResourceWiring(lr).wired {
			rec.Record(fmt.Sprintf("internal/provider/list_%s_remote_test.go", name), fmt.Sprintf("coverage tests for list resource %s remote helper", lr.Name))
		}
	}
	if opts.IncludeDocs {
		// Docs are suppressed for list resources the provider cannot register
		// (no paired managed resource): terraform query never exposes them, so
		// documenting them would advertise constructs the provider cannot
		// serve. Mirrors ListResourceDocsFiles so record and write modes stay
		// in lockstep.
		if lr.Registerable {
			rec.Record(fmt.Sprintf("docs/list-resources/%s.md", name), fmt.Sprintf("documentation for list resource %s", lr.Name))
		}
	}
}

func collectFunctionFiles(rec *Recorder, fn ir.FunctionIR, opts CollectOptions) {
	name := fileName(fn.Name)
	rec.Record(fmt.Sprintf("internal/provider/function_%s.go", name), fmt.Sprintf("provider-defined function %s", displayName(fn.Name, fn.FullName)))
	if opts.IncludeDocs {
		rec.Record(fmt.Sprintf("docs/functions/%s.md", name), fmt.Sprintf("documentation for function %s", fn.Name))
	}
}

// collectClientFiles records the internal/client package file set. It must
// match ClientFiles (client.go) exactly so dry-run output names the same files
// write mode creates: client.go, models.go, errors.go, retry.go, pagination.go,
// logging.go, client_test.go, and logging_test.go unconditionally, plus auth.go
// and auth_test.go only when the provider declares security schemes. Previously
// record mode invented auth.go for auth-less APIs and omitted errors.go,
// pagination.go, and logging.go (H-13).
func collectClientFiles(rec *Recorder, provider *ir.ProviderIR, _ CollectOptions) {
	rec.Record("internal/client/client.go", "generated HTTP client")
	rec.Record("internal/client/models.go", "request/response model structs")
	rec.Record("internal/client/errors.go", "typed API error helpers")
	rec.Record("internal/client/retry.go", "retry logic with exponential backoff")
	rec.Record("internal/client/pagination.go", "pagination helpers")
	rec.Record("internal/client/logging.go", "request/response trace logging")
	rec.Record("internal/client/client_test.go", "generated HTTP client unit tests")
	rec.Record("internal/client/logging_test.go", "trace logging unit tests")
	if provider != nil && len(provider.SecurityIR.Schemes) > 0 {
		rec.Record("internal/client/auth.go", "authentication middleware")
		rec.Record("internal/client/auth_test.go", "authentication middleware unit tests")
	}
}

// collectBuildFiles records the build/CI/release scaffolding: GNUmakefile,
// .goreleaser.yml, the GitHub Actions release workflow, and the Terraform
// Registry manifest. These files are templated from BuildConfig and are
// independent of the provider's constructs, so they can be emitted alone
// (OnlyBuild) or omitted from a full run (!IncludeBuild).
func collectBuildFiles(rec *Recorder) {
	rec.Record("GNUmakefile", "build and test automation")
	rec.Record(".goreleaser.yml", "GoReleaser release configuration")
	rec.Record(".github/workflows/release.yml", "GitHub Actions release workflow")
	rec.Record("terraform-registry-manifest.json", "Terraform Registry manifest")
}

func collectRootFiles(rec *Recorder, provider *ir.ProviderIR, opts CollectOptions) {
	rec.Record("go.mod", "Go module definition")
	rec.Record("README.md", "provider overview and usage guide")
	if opts.IncludeBuild {
		collectBuildFiles(rec)
	}
	if opts.IncludeDynamicRelease {
		rec.Record(".gitignore", "ignore regenerated provider files on the default branch")
		rec.Record(".github/workflows/regenerate-and-release.yml", "dynamic regenerate-and-release workflow (opt-in)")
	}
	if opts.IncludeDocs {
		rec.Record("docs/index.md", "provider overview and authentication guide")
	}
	if opts.IncludeTerraformTests && provider != nil {
		for _, res := range provider.Resources {
			name := fileName(res.Name)
			rec.Record(fmt.Sprintf("tests/%s.tftest.hcl", name), fmt.Sprintf("terraform test suite for resource %s", res.Name))
			rec.Record(fmt.Sprintf("tests/modules/%s/main.tf", name), fmt.Sprintf("terraform test module for resource %s", res.Name))
		}
	}
	if opts.IncludeConfig {
		rec.Record("generator.yaml", "generated generator configuration")
	}
}

// anyResourceCoverageEligible reports whether at least one resource would
// produce a coverage test file (wired and not a binary file-upload create). It
// mirrors the gate in ResourceCoverageTestFiles so the shared helpers file is
// recorded exactly when it is emitted.
func anyResourceCoverageEligible(resources []ir.ResourceIR) bool {
	for _, res := range resources {
		if planResourceWiring(res).wired && !resourceCreateHasBinaryUpload(res) {
			return true
		}
	}
	return false
}

// anyDataSourceCoverageEligible reports whether at least one data source would
// produce a coverage test file (wired). It mirrors the gate in
// DataSourceCoverageTestFiles so the shared helpers file is recorded exactly
// when it is emitted.
func anyDataSourceCoverageEligible(dataSources []ir.DataSourceIR) bool {
	for _, ds := range dataSources {
		if planDataSourceWiring(ds).wired {
			return true
		}
	}
	return false
}

// anyActionCoverageEligible reports whether at least one action would produce a
// coverage test file (wired). It mirrors the gate in ActionCoverageTestFiles so
// the shared helpers file is recorded exactly when it is emitted.
func anyActionCoverageEligible(actions []ir.ActionIR) bool {
	for _, a := range actions {
		if planActionWiring(a).wired {
			return true
		}
	}
	return false
}

// anyEphemeralCoverageEligible reports whether at least one ephemeral resource
// would produce a coverage test file (wired). It mirrors the gate in
// EphemeralCoverageTestFiles so the shared helpers file is recorded exactly
// when it is emitted.
func anyEphemeralCoverageEligible(ers []ir.EphemeralResourceIR) bool {
	for _, er := range ers {
		if planEphemeralWiring(er).wired {
			return true
		}
	}
	return false
}

// anyListCoverageEligible reports whether at least one list resource would
// produce a coverage test file (wired). It mirrors the gate in
// ListCoverageTestFiles so the shared helpers file is recorded exactly when it
// is emitted.
func anyListCoverageEligible(lrs []ir.ListResourceIR) bool {
	for _, lr := range lrs {
		if planListResourceWiring(lr).wired {
			return true
		}
	}
	return false
}

// fileName converts an IR construct name into a safe file name segment. It
// uses the same naming.SnakeCase helper the writers use (resource.go, datasource.go,
// action.go, ...) so record mode and write mode agree on every output path
// (H-11). It explicitly neutralizes path separators and ".." segments as
// defense in depth so a future write mode cannot escape the configured
// OutputDir.
func fileName(name string) string {
	name = strings.TrimSpace(name)
	name = naming.SnakeCase(name)
	// Defense in depth for future write mode: replace any path separator and
	// any ".." segment that may have survived as literal text. naming.SnakeCase
	// already splits on every non-alphanumeric rune, so these are no-ops today,
	// but they guard against a future change to naming.SnakeCase.
	name = strings.ReplaceAll(name, string(filepath.Separator), "_")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	for strings.Contains(name, "..") {
		name = strings.ReplaceAll(name, "..", "_")
	}
	if name == "" {
		return "unnamed"
	}
	return name
}

// displayName returns a human-readable construct name, falling back to the raw
// name when no full name is present.
func displayName(name, fullName string) string {
	if strings.TrimSpace(fullName) != "" {
		return fullName
	}
	if strings.TrimSpace(name) != "" {
		return name
	}
	return "unnamed"
}

// manifestName is the write-mode bookkeeping file at the output root. It lists
// which relative paths the most recent write-mode run generated so a subsequent
// forced run can delete the files that generation no longer produces (N-70)
// without ever touching a file eidos did not write.
const manifestName = ".eidos-generated.json"

// generation mode labels stored in the manifest. Stale-file cleanup is scoped
// per mode so a full run never deletes --only-build scaffolding and an
// --only-build run never deletes provider code: the two workflows write
// disjoint file sets on purpose (M-78).
const (
	generationModeFull      = "full"
	generationModeOnlyBuild = "only-build"
)

// generationManifest is the on-disk shape of .eidos-generated.json.
type generationManifest struct {
	// Mode is the mode of the most recent write-mode run. It is retained for
	// backward compatibility with manifests written before per-mode tracking
	// (M-3); new runs always populate ByMode.
	Mode      string   `json:"mode"`
	Generated []string `json:"generated"`
	// ByMode holds the file list each run kind (full vs only-build) most
	// recently generated, so a full run and an --only-build run each keep their
	// own bookkeeping without clobbering the other's (M-3). A full run can
	// therefore still delete stale provider files after an --only-build refresh
	// ran in between, and vice versa.
	ByMode map[string][]string `json:"by_mode,omitempty"`
}

// withRun returns a copy of the manifest with the current run's mode and file
// list recorded, preserving the other mode's bookkeeping. Without this, an
// --only-build refresh would overwrite the full-mode manifest, and the next
// full run would see a mode mismatch, silently skip stale cleanup, and leave
// orphaned provider files in the manifest of no run (M-3).
func (m *generationManifest) withRun(mode string, files []string) *generationManifest {
	next := &generationManifest{Mode: mode, Generated: files}
	if m != nil {
		next.ByMode = make(map[string][]string, len(m.ByMode)+1)
		for k, v := range m.ByMode {
			next.ByMode[k] = v
		}
	} else {
		next.ByMode = make(map[string][]string, 1)
	}
	next.ByMode[mode] = files
	return next
}

// readGenerationManifest loads the write-mode bookkeeping file from dir. A
// missing file yields a nil manifest (nothing to clean up); a present but
// corrupt file fails loud rather than silently skipping the cleanup the caller
// asked for with --force.
func readGenerationManifest(dir string) (*generationManifest, error) {
	// gosec: reading the bookkeeping manifest eidos itself wrote under the
	// caller-supplied output dir is the intended contract of --force cleanup.
	data, err := os.ReadFile(filepath.Join(dir, manifestName)) //nolint:gosec // manifest path is the output dir eidos manages
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var m generationManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", manifestName, err)
	}
	// Backfill per-mode tracking from the legacy single-mode shape so a
	// manifest written before M-3 still drives stale cleanup.
	if len(m.ByMode) == 0 && m.Mode != "" {
		m.ByMode = map[string][]string{m.Mode: m.Generated}
	}
	return &m, nil
}

// writeGenerationManifest persists the manifest atomically (write to a temp
// file, then rename) so an interrupted write cannot leave a truncated manifest
// that later fails readGenerationManifest.
func writeGenerationManifest(dir string, m *generationManifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := filepath.Join(dir, manifestName+".tmp")
	if err := os.WriteFile(tmp, data, 0o640); err != nil { //nolint:gosec // generated bookkeeping file permissions are intentional.
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, manifestName))
}

// removeStaleGeneratedFiles deletes the paths the previous write-mode run of
// the same kind (full vs only-build) recorded in prev that are not in planned,
// gated on force. The per-mode bookkeeping (ByMode) means a full run and an
// --only-build run each clean up only their own previous output, so an
// --only-build refresh can never delete provider code a full run produced and a
// full run can still delete stale provider files after an --only-build refresh
// ran in between (M-3). It only ever removes files eidos itself wrote (per the
// manifest), so hand-written files are untouched even when they sit at a path
// the generator would own. Empty parent directories are pruned up to (but never
// including) the output root. It returns the removed paths.
//
// includeConfig guards the generator.yaml path: when the current run is not
// configured to emit it (IncludeConfig=false, e.g. the MCP generate tool which
// deliberately omits it), a generator.yaml recorded by a previous run is never
// deleted. The config is the caller's source-of-truth input, not a provider
// deliverable, and removing it would silently destroy the very config the
// current run was invoked with (M-82).
func removeStaleGeneratedFiles(dir string, prev *generationManifest, planned []string, currentMode string, force, includeConfig bool) ([]string, error) {
	if !force || prev == nil {
		return nil, nil
	}
	prevFiles := prev.ByMode[currentMode]
	if len(prevFiles) == 0 {
		return nil, nil
	}
	plannedSet := make(map[string]struct{}, len(planned))
	for _, p := range planned {
		plannedSet[p] = struct{}{}
	}
	var removed []string
	for _, p := range prevFiles {
		if _, ok := plannedSet[p]; ok {
			continue
		}
		if !includeConfig && p == "generator.yaml" {
			continue
		}
		if err := os.Remove(filepath.Join(dir, filepath.FromSlash(p))); err != nil {
			if os.IsNotExist(err) {
				// The file is already gone (user cleanup, a partial earlier run);
				// there is nothing to remove.
				continue
			}
			return removed, fmt.Errorf("remove stale generated file %q: %w", p, err)
		}
		removed = append(removed, p)
	}
	if len(removed) > 0 {
		pruneEmptyDirs(dir, removed)
	}
	return removed, nil
}

// pruneEmptyDirs removes directories that became empty as a result of removing
// stale generated files, walking up from each removed path and stopping at the
// output root (".", which is never removed). A directory that is non-empty (a
// hand-written file, a third-party artifact, another stale file's sibling) is
// left alone, and because a non-empty directory's ancestors cannot be empty
// either, the walk-up stops at the first failure. The cleanup is best-effort:
// nothing is returned or logged on a non-empty directory, because that is the
// normal outcome (a hand-written file or third-party artifact coexisting with
// generated output).
func pruneEmptyDirs(root string, removed []string) {
	for _, p := range removed {
		d := filepath.Dir(filepath.FromSlash(p))
		for d != "." && d != string(filepath.Separator) {
			if err := os.Remove(filepath.Join(root, d)); err != nil {
				// Non-empty or otherwise not removable; stop walking up this chain.
				break
			}
			d = filepath.Dir(d)
		}
	}
}
