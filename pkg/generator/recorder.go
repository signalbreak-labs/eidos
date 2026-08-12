// Package generator emits Terraform provider source code, documentation, tests,
// and release artifacts.
//
// The recorder types in this file implement the "record mode" used by dry-run
// and other preview paths. In record mode the generator plans every file it
// would write and returns a deterministic list of {path, reason} entries
// without touching the filesystem.
package generator

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

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
}

// Run executes the generator for the supplied ProviderIR. In ModeRecord it
// returns the deterministic list of {path, reason} entries that would be
// written without creating any files. In ModeWrite it emits the files to disk
// and returns the same planned file list on success.
func Run(provider *ir.ProviderIR, opts Options) ([]FileEntry, error) {
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
		h := Harness{OutputDir: opts.OutputDir, RefuseOverwrite: !opts.Force}
		if err := h.Generate(files); err != nil {
			return nil, fmt.Errorf("write generated files: %w", err)
		}
		return CollectFromProviderIR(provider, opts.CollectOptions), nil
	default:
		return nil, fmt.Errorf("unknown generator mode %d", opts.Mode)
	}
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

// Recordf is a convenience method that formats a reason string and records the
// entry. It is provided for future generation passes that build reasons
// dynamically and currently has no non-test callers.
func (r *Recorder) Recordf(filePath, reasonFormat string, args ...any) {
	r.Record(filePath, fmt.Sprintf(reasonFormat, args...))
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
}

// DefaultCollectOptions returns the recommended collection options for a full
// generation run: tests, docs, examples, and config are enabled; native
// Terraform test files are disabled by default.
func DefaultCollectOptions() CollectOptions {
	return CollectOptions{
		IncludeTests:          true,
		IncludeTerraformTests: false,
		IncludeDocs:           true,
		IncludeExamples:       true,
		IncludeConfig:         true,
	}
}

// CollectFromProviderIR walks a ProviderIR and records every file the generator
// would create for the provider. The returned entries are sorted by path and
// do not depend on the filesystem.
func CollectFromProviderIR(provider *ir.ProviderIR, opts CollectOptions) []FileEntry {
	rec := NewRecorder()
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
		// test against a mock server could never pass. This mirrors
		// ResourceAcceptanceTestFiles so record and write modes stay in lockstep.
		if planResourceWiring(res).wired {
			rec.Record(fmt.Sprintf("internal/provider/resource_%s_acceptance_test.go", name), fmt.Sprintf("acceptance tests for resource %s", res.Name))
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
	if opts.IncludeDocs {
		rec.Record(fmt.Sprintf("docs/list-resources/%s.md", name), fmt.Sprintf("documentation for list resource %s", lr.Name))
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
// and logging.go unconditionally, plus auth.go only when the provider declares
// security schemes. Previously record mode invented auth.go for auth-less APIs
// and omitted errors.go, pagination.go, and logging.go (H-13).
func collectClientFiles(rec *Recorder, provider *ir.ProviderIR, _ CollectOptions) {
	rec.Record("internal/client/client.go", "generated HTTP client")
	rec.Record("internal/client/models.go", "request/response model structs")
	rec.Record("internal/client/errors.go", "typed API error helpers")
	rec.Record("internal/client/retry.go", "retry logic with exponential backoff")
	rec.Record("internal/client/pagination.go", "pagination helpers")
	rec.Record("internal/client/logging.go", "request/response trace logging")
	if provider != nil && len(provider.SecurityIR.Schemes) > 0 {
		rec.Record("internal/client/auth.go", "authentication middleware")
	}
}

func collectRootFiles(rec *Recorder, provider *ir.ProviderIR, opts CollectOptions) {
	rec.Record("go.mod", "Go module definition")
	rec.Record("GNUmakefile", "build and test automation")
	rec.Record("README.md", "provider overview and usage guide")
	rec.Record(".goreleaser.yml", "GoReleaser release configuration")
	rec.Record(".github/workflows/release.yml", "GitHub Actions release workflow")
	rec.Record("terraform-registry-manifest.json", "Terraform Registry manifest")
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
