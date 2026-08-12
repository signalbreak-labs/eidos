package generator

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"runtime"
	"strings"
	"text/template"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// DefaultGoVersion is the Go directive written to generated go.mod files when
// BuildConfig.GoVersion is empty. It is derived from the running Go toolchain's
// version so generated providers use the same language floor as the generator.
var DefaultGoVersion = defaultGoVersionFromRuntime()

func defaultGoVersionFromRuntime() string {
	ver := runtime.Version()
	if !strings.HasPrefix(ver, "go") {
		return "1.26"
	}
	parts := strings.Split(strings.TrimPrefix(ver, "go"), ".")
	if len(parts) < 2 {
		return "1.26"
	}
	return parts[0] + "." + parts[1]
}

// Pinned framework dependency versions emitted in generated provider go.mod
// files. Pinning protects generated providers from unexpected upstream
// breaking changes. Callers can override these per-build via
// BuildConfig.BuildVersions.
const (
	// TerraformPluginFrameworkVersion is pinned to v1.19.0 to include the list
	// resource packages used by generated providers and other recent framework
	// features while remaining a stable release.
	TerraformPluginFrameworkVersion = "v1.19.0"
	TerraformPluginGoVersion        = "v0.31.0"
	TerraformPluginLogVersion       = "v0.10.0"
	// TerraformPluginTestingVersion is pinned to v1.16.0 because the previous
	// pinned value v0.11.0 does not exist as a real release (terraform-plugin-testing
	// jumped from v0.x to v1.x). v1.16.0 is compatible with the framework and
	// plugin-go versions pinned above.
	TerraformPluginTestingVersion = "v1.16.0"
)

// BuildVersions holds optional overrides for the pinned Terraform plugin
// dependency versions. A zero value or empty fields fall back to the package
// constants so that callers only need to specify the versions they want to
// change (for example, to apply a security patch).
type BuildVersions struct {
	FrameworkVersion string
	PluginGoVersion  string
	PluginLogVersion string
	TestingVersion   string
}

// NewBuildVersions returns a BuildVersions populated with the package defaults.
// Callers can then override only the fields they care about.
func NewBuildVersions() BuildVersions {
	return BuildVersions{
		FrameworkVersion: TerraformPluginFrameworkVersion,
		PluginGoVersion:  TerraformPluginGoVersion,
		PluginLogVersion: TerraformPluginLogVersion,
		TestingVersion:   TerraformPluginTestingVersion,
	}
}

// BuildConfig describes the metadata needed to generate provider build,
// release, and documentation files.
type BuildConfig struct {
	// ProviderName is the provider type, e.g. "mycloud".
	ProviderName string

	// Namespace is the Terraform Registry namespace, e.g. "mycloud".
	Namespace string

	// ModulePath is the generated provider's Go module path. If empty, it
	// defaults to github.com/<Namespace>/terraform-provider-<ProviderName>.
	ModulePath string

	// GoVersion is the Go directive written to go.mod. If empty, it defaults
	// to DefaultGoVersion.
	GoVersion string

	// ProtocolVersions lists the Terraform protocol versions supported by
	// the generated provider. If empty, it defaults to ["6.0"].
	ProtocolVersions []string

	// BuildVersions optionally overrides the pinned Terraform plugin
	// dependency versions emitted in go.mod. Empty fields fall back to the
	// package constants.
	BuildVersions *BuildVersions

	// SignRelease enables GPG signing of the checksums file in the
	// generated .goreleaser.yml and the GitHub Actions release workflow.
	// When false (the default), unsigned releases are produced and no GPG
	// secrets are required. Operators can enable signed releases after
	// configuring GPG_PRIVATE_KEY and GPG_PASSPHRASE repository secrets.
	SignRelease bool
}

// BuildFiles returns the complete set of build/release files for a generated
// provider. The returned files are intended to be passed to Harness.Generate.
func BuildFiles(cfg BuildConfig) []File {
	return []File{
		GoMod(cfg),
		Readme(cfg),
		GNUmakefile(cfg),
		Goreleaser(cfg),
		ReleaseWorkflow(cfg),
		RegistryManifest(cfg),
		MainGoFile(cfg),
	}
}

func (cfg BuildConfig) providerName() string {
	return strings.TrimSpace(cfg.ProviderName)
}

func (cfg BuildConfig) namespace() string {
	return strings.TrimSpace(cfg.Namespace)
}

func (cfg BuildConfig) modulePath() string {
	if strings.TrimSpace(cfg.ModulePath) != "" {
		return strings.TrimSpace(cfg.ModulePath)
	}
	return fmt.Sprintf("github.com/%s/terraform-provider-%s", cfg.namespace(), cfg.providerName())
}

func (cfg BuildConfig) goVersion() string {
	if strings.TrimSpace(cfg.GoVersion) != "" {
		return strings.TrimSpace(cfg.GoVersion)
	}
	return DefaultGoVersion
}

func (cfg BuildConfig) sourceAddress() string {
	return fmt.Sprintf("registry.terraform.io/%s/%s", cfg.namespace(), cfg.providerName())
}

func (cfg BuildConfig) protocolVersions() []string {
	if len(cfg.ProtocolVersions) > 0 {
		return cfg.ProtocolVersions
	}
	return []string{"6.0"}
}

// versions returns the effective Terraform plugin dependency versions for the
// build. Fields set on BuildVersions override the package constants; empty or
// unset fields fall back to the pinned defaults.
func (cfg BuildConfig) versions() BuildVersions {
	defaults := BuildVersions{
		FrameworkVersion: TerraformPluginFrameworkVersion,
		PluginGoVersion:  TerraformPluginGoVersion,
		PluginLogVersion: TerraformPluginLogVersion,
		TestingVersion:   TerraformPluginTestingVersion,
	}
	if cfg.BuildVersions == nil {
		return defaults
	}
	v := *cfg.BuildVersions
	if strings.TrimSpace(v.FrameworkVersion) == "" {
		v.FrameworkVersion = defaults.FrameworkVersion
	}
	if strings.TrimSpace(v.PluginGoVersion) == "" {
		v.PluginGoVersion = defaults.PluginGoVersion
	}
	if strings.TrimSpace(v.PluginLogVersion) == "" {
		v.PluginLogVersion = defaults.PluginLogVersion
	}
	if strings.TrimSpace(v.TestingVersion) == "" {
		v.TestingVersion = defaults.TestingVersion
	}
	return v
}

// Validate reports missing, whitespace-only, or malformed required fields.
// Callers should validate BuildConfig before generating files to avoid
// malformed output such as an invalid Go module path, goreleaser project name,
// or Terraform registry source address. ProviderName and Namespace flow
// unsanitized into `module <path>`, `project_name:`, and
// `registry.terraform.io/<ns>/<name>`, so they must be safe path/identifier
// segments: no whitespace, colons, or slashes (M-10).
func (cfg BuildConfig) Validate() error {
	var msgs []string
	if cfg.providerName() == "" {
		msgs = append(msgs, "ProviderName is required")
	}
	if cfg.namespace() == "" {
		msgs = append(msgs, "Namespace is required")
	}
	if cfg.providerName() != "" && !isValidRegistrySegment(cfg.providerName()) {
		msgs = append(msgs, fmt.Sprintf("ProviderName %q must match %q (no spaces, colons, or slashes)", cfg.providerName(), registrySegmentPattern))
	}
	if cfg.namespace() != "" && !isValidRegistrySegment(cfg.namespace()) {
		msgs = append(msgs, fmt.Sprintf("Namespace %q must match %q (no spaces, colons, or slashes)", cfg.namespace(), registrySegmentPattern))
	}
	if cfg.ModulePath != "" && strings.TrimSpace(cfg.ModulePath) == "" {
		msgs = append(msgs, "ModulePath cannot be whitespace-only")
	}
	if cfg.GoVersion != "" && strings.TrimSpace(cfg.GoVersion) == "" {
		msgs = append(msgs, "GoVersion cannot be whitespace-only")
	}
	for i, v := range cfg.ProtocolVersions {
		if strings.TrimSpace(v) == "" {
			msgs = append(msgs, fmt.Sprintf("ProtocolVersions[%d] cannot be whitespace-only", i))
		}
	}
	if len(msgs) == 0 {
		return nil
	}
	return errors.New(strings.Join(msgs, "; "))
}

// registrySegmentPattern is the set of strings safe to embed in a Go module
// path segment, a goreleaser project_name, and a Terraform registry source
// address: an alphanumeric lead character followed by alphanumerics or hyphens.
// It rejects whitespace, colons, slashes, dots, and underscores. Dots and
// underscores are valid in Go module path segments but are rejected by
// Terraform for provider type names and namespaces (verified against terraform
// v1.14.7), and the generated module path is derived from the same
// namespace/name, so a dash-only name keeps both the module path and the
// registry source address valid.
const registrySegmentPattern = `^[a-zA-Z0-9][a-zA-Z0-9-]*$`

// registrySegmentRe is the compiled form of registrySegmentPattern. The pattern
// is a compile-time constant, so MustCompile is safe and avoids recompiling on
// every isValidRegistrySegment call (and the unchecked error that MatchString
// would otherwise force).
var registrySegmentRe = regexp.MustCompile(registrySegmentPattern)

// isValidRegistrySegment reports whether s is safe to embed as a module path /
// project_name / registry source segment.
func isValidRegistrySegment(s string) bool {
	return registrySegmentRe.MatchString(s)
}

// GoMod returns the generated go.mod file for the provider.
func GoMod(cfg BuildConfig) File {
	versions := cfg.versions()
	return Template("go.mod", goModTemplate, map[string]any{
		"ModulePath":       cfg.modulePath(),
		"GoVersion":        cfg.goVersion(),
		"FrameworkVersion": versions.FrameworkVersion,
		"PluginGoVersion":  versions.PluginGoVersion,
		"PluginLogVersion": versions.PluginLogVersion,
		"TestingVersion":   versions.TestingVersion,
	})
}

const goModTemplate = `module {{.ModulePath}}

go {{.GoVersion}}

require (
	github.com/hashicorp/terraform-plugin-framework {{.FrameworkVersion}}
	github.com/hashicorp/terraform-plugin-go {{.PluginGoVersion}}
	github.com/hashicorp/terraform-plugin-log {{.PluginLogVersion}}
	github.com/hashicorp/terraform-plugin-testing {{.TestingVersion}}
)
`

// GNUmakefile returns the generated GNUmakefile for the provider.
func GNUmakefile(_ BuildConfig) File {
	return staticFile("GNUmakefile", gnuMakefile)
}

const gnuMakefile = `default: fmt lint install generate

build:
	go build -v ./...

install: build
	go install -v ./...

lint:
	golangci-lint run

# Run go generate only when a tools/ directory is present. Eidos does not emit
# one, so on a fresh checkout this target is a no-op rather than a hard failure
# (M-9); users who add a tools/ directory for code generation get the usual
# behavior.
generate:
	@if [ -d tools ]; then cd tools && go generate ./...; fi

fmt:
	gofmt -s -w -e .

test:
	go test -v -cover -timeout=120s -parallel=10 ./...

testacc:
	TF_ACC=1 go test -v -cover -timeout 120m ./...

.PHONY: fmt lint test testacc build install generate
`

// Goreleaser returns the generated .goreleaser.yml file for the provider.
// The output is valid GoReleaser v2 configuration and produces
// Terraform Registry-compatible artifacts. GPG signing of checksums is
// included only when cfg.SignRelease is true.
func Goreleaser(cfg BuildConfig) File {
	return releaseTemplateFile(".goreleaser.yml", goreleaserYAML, cfg)
}

const goreleaserYAML = `version: 2

project_name: terraform-provider-[[ .ProviderName ]]

before:
  hooks:
    - go mod tidy

builds:
  - id: terraform-provider-[[ .ProviderName ]]
    main: ./
    binary: terraform-provider-[[ .ProviderName ]]_v{{ .Version }}
    env:
      - CGO_ENABLED=0
    flags:
      - -trimpath
    ldflags:
      - -s -w -X main.version={{ .Version }} -X main.commit={{ .Commit }} -X main.date={{ .CommitDate }}
    goos:
      - freebsd
      - windows
      - linux
      - darwin
      - openbsd
      - solaris
    goarch:
      - amd64
      - arm
      - arm64
      - "386"
    ignore:
      - goos: darwin
        goarch: "386"
      - goos: darwin
        goarch: arm
      - goos: openbsd
        goarch: arm
      - goos: openbsd
        goarch: arm64
      - goos: solaris
        goarch: "386"
      - goos: solaris
        goarch: arm
      - goos: solaris
        goarch: arm64
      - goos: windows
        goarch: arm64

archives:
  - formats:
      - tar.gz
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    format_overrides:
      - goos: windows
        formats:
          - zip

checksum:
  name_template: "{{ .ProjectName }}_{{ .Version }}_SHA256SUMS"
  algorithm: sha256
[[ if .SignRelease ]]
signs:
  - artifacts: checksum
    args:
      - --batch
      - --local-user
      - "{{ .Env.GPG_FINGERPRINT }}"
      - --output
      - "${signature}"
      - --detach-sign
      - "${artifact}"
[[- end ]]

release:
  draft: false

changelog:
  use: github
  sort: asc
  filters:
    exclude:
      - "^docs:"
      - "^test:"
      - "^chore:"
`

// ReleaseWorkflow returns the generated .github/workflows/release.yml file for
// the provider. It triggers on version tags and runs GoReleaser. GPG key import
// and signing are included only when cfg.SignRelease is true.
func ReleaseWorkflow(cfg BuildConfig) File {
	return releaseTemplateFile(".github/workflows/release.yml", releaseWorkflowYAML, cfg)
}

const releaseWorkflowYAML = `# Generated release workflow for terraform-provider-[[ .ProviderName ]].
# Configure GPG_PRIVATE_KEY and GPG_PASSPHRASE repository secrets before cutting
# a signed release. To disable signed checksums, set SignRelease to false when
# generating this workflow and also remove (or comment out) the signs: block in
# .goreleaser.yml.

name: Release

on:
  push:
    tags:
      - "v*"

permissions:
  contents: write

jobs:
  release:
    name: Release
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
          cache: true
[[ if .SignRelease ]]
      - name: Import GPG key
        id: import_gpg
        uses: crazy-max/ghaction-import-gpg@v6
        with:
          gpg_private_key: ${{ secrets.GPG_PRIVATE_KEY }}
          passphrase: ${{ secrets.GPG_PASSPHRASE }}
[[- end ]]
      - name: Run GoReleaser
        uses: goreleaser/goreleaser-action@v6
        with:
          version: "~> v2"
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}[[ if .SignRelease ]]
          GPG_FINGERPRINT: ${{ steps.import_gpg.outputs.fingerprint }}[[- end ]]
`

// BuildConfigFromIR derives a BuildConfig from a ProviderIR. It uses the
// provider name as both the provider type and the registry namespace and leaves
// the module path at the default derived value.
func BuildConfigFromIR(provider *ir.ProviderIR) BuildConfig {
	name := "generated"
	if provider != nil && strings.TrimSpace(provider.Name) != "" {
		name = provider.Name
	}
	return BuildConfig{
		ProviderName: name,
		Namespace:    name,
	}
}

// FilesForProviderIR assembles the complete set of generated files for a
// provider according to the supplied collection options. It is the single source
// of truth for the file list produced by both record mode and write mode.
func FilesForProviderIR(provider *ir.ProviderIR, cfg BuildConfig, opts CollectOptions) ([]File, error) {
	if provider == nil {
		return nil, errors.New("FilesForProviderIR: provider is nil")
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid BuildConfig: %w", err)
	}

	files := make([]File, 0)
	files = append(files, BuildFiles(cfg)...)

	pf, err := ProviderFileWithClient(*provider, cfg.modulePath()+"/internal/client")
	if err != nil {
		return nil, fmt.Errorf("generate provider file: %w", err)
	}
	files = append(files, pf)

	files = append(files, ResourceFiles(provider.Resources, cfg.modulePath()+"/internal/client")...)
	files = append(files, DataSourceFiles(provider.DataSources, cfg.modulePath()+"/internal/client")...)
	files = append(files, ActionFiles(provider.Actions, cfg.modulePath()+"/internal/client")...)
	files = append(files, EphemeralFiles(provider.EphemeralResources, cfg.modulePath()+"/internal/client")...)
	files = append(files, ListResourceFiles(provider.ListResources, cfg.modulePath()+"/internal/client")...)
	files = append(files, FunctionFiles(provider.Functions)...)
	files = append(files, ClientFiles(*provider)...)

	providerImport := cfg.modulePath() + "/internal/provider"
	if len(provider.Resources) > 0 {
		files = append(files, ValueMappersFile(provider.Resources, providerImport))
		files = append(files, ModelFiles(provider.Resources)...)
	}
	if AnyResourceWired(provider.Resources) || AnyDataSourceWired(provider.DataSources) || AnyEphemeralWired(provider.EphemeralResources) || AnyActionSendsBody(provider.Actions) {
		// JSON conversion helpers used by resource CRUD bodies, data source
		// Read bodies, ephemeral resource Open bodies, and body-bearing action
		// Invoke bodies wired to the generated API client.
		files = append(files, JSONConvertFile(provider))
	}
	files = append(files, ValidatorsFile(*provider))

	if opts.IncludeTests {
		files = append(files, TestFiles(*provider, cfg)...)
	}
	if opts.IncludeDocs {
		files = append(files, ProviderDocsFiles(*provider)...)
	}
	if opts.IncludeExamples {
		files = append(files, ExampleFiles(provider)...)
	}
	if opts.IncludeTerraformTests {
		files = append(files, TerraformTestFiles(*provider, cfg)...)
	}
	if opts.IncludeConfig {
		files = append(files, ConfigFile(*provider))
	}

	return files, nil
}

// staticFile returns a File that writes a fixed string to the output directory
// without running text/template on it.
//
// This helper is intentionally separate from Template because the GNUmakefile
// embeds no provider-specific values and its recipes must be emitted verbatim.
// staticFile does not validate or escape content; callers must ensure the
// provided string is safe to write verbatim to the output path.
func staticFile(path, content string) File {
	return File{
		Path: path,
		Render: func(w io.Writer) error {
			_, err := io.WriteString(w, content)
			return err
		},
	}
}

// releaseTemplateFile returns a File rendered with Go's text/template package
// using [[ ... ]] delimiters. The alternate delimiters let the generated
// .goreleaser.yml and release workflow retain GoReleaser's own {{ ... }}
// template variables (for example {{ .Version }}) while still substituting
// provider-specific values at generation time.
func releaseTemplateFile(path, text string, data any) File {
	return File{
		Path: path,
		Render: func(w io.Writer) error {
			tmpl, err := template.New(path).Delims("[[", "]]").Parse(text)
			if err != nil {
				return fmt.Errorf("failed to parse %s template: %w", path, err)
			}
			return tmpl.Execute(w, data)
		},
	}
}
