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

// DefaultTerraformVersion is the minimum Terraform CLI version stated in a
// generated README when BuildConfig.TerraformVersion is empty. Protocol 6.0
// providers work on Terraform 1.0; constructs with newer CLI requirements
// (ephemeral resources, actions, list resources) raise the stated minimum via
// BuildConfigFromIR.
const DefaultTerraformVersion = "1.0"

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
	// TerraformPluginFrameworkTimeoutsVersion is pinned to v0.7.0, the version
	// whose resource/timeouts.Block and timeouts.Value API the generated
	// timeouts block and CRUD wiring target (M-14). It requires framework
	// >= v1.16.1, satisfied by the pinned framework version above.
	TerraformPluginFrameworkTimeoutsVersion = "v0.7.0"
	// TerraformPluginFrameworkValidatorsVersion is pinned to v0.19.0, the
	// release compatible with the framework version pinned above (it requires
	// framework >= v1.16.1). It supplies the standard validators
	// (stringvalidator.OneOf, int64validator.Between, …) emitted for
	// spec-declared enum/range/length/pattern constraints (G39).
	TerraformPluginFrameworkValidatorsVersion = "v0.19.0"
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
	// TimeoutsVersion pins the terraform-plugin-framework-timeouts module used
	// by generated resources with configured timeouts (M-14).
	TimeoutsVersion string
	// ValidatorsVersion pins the terraform-plugin-framework-validators module
	// supplying the standard constraint validators.
	ValidatorsVersion string
}

// NewBuildVersions returns a BuildVersions populated with the package defaults.
// Callers can then override only the fields they care about.
func NewBuildVersions() BuildVersions {
	return BuildVersions{
		FrameworkVersion:  TerraformPluginFrameworkVersion,
		PluginGoVersion:   TerraformPluginGoVersion,
		PluginLogVersion:  TerraformPluginLogVersion,
		TestingVersion:    TerraformPluginTestingVersion,
		TimeoutsVersion:   TerraformPluginFrameworkTimeoutsVersion,
		ValidatorsVersion: TerraformPluginFrameworkValidatorsVersion,
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

	// TerraformVersion is the minimum Terraform CLI version stated in the
	// generated README's Requirements section. If empty, it defaults to
	// DefaultTerraformVersion. BuildConfigFromIR raises it to the highest
	// CLI version the provider's constructs require (actions and list
	// resources need 1.14+, ephemeral resources 1.10+), so the README never
	// understates the requirement.
	TerraformVersion string

	// BuildVersions optionally overrides the pinned Terraform plugin
	// dependency versions emitted in go.mod. Empty fields fall back to the
	// package constants.
	BuildVersions *BuildVersions

	// SignRelease enables GPG signing of the checksums file in the
	// generated .goreleaser.yml and both GitHub Actions release workflows.
	// Signed releases are default-on: BuildConfigFromIR sets this true, and the
	// sign_release generator.yaml field (a *bool) opts out by setting it false.
	// When false, unsigned releases are produced and no GPG secrets are
	// required. Operators enabling signed releases must configure
	// GPG_PRIVATE_KEY and GPG_PASSPHRASE repository secrets.
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

// releaseFiles returns the build/CI/release scaffolding gated by CollectOptions
// .IncludeBuild / emitted alone by CollectOptions.OnlyBuild: the GNUmakefile,
// .goreleaser.yml, the GitHub Actions release workflow, and the Terraform
// Registry manifest. These are templated from BuildConfig and do not depend on
// the provider's constructs.
func releaseFiles(cfg BuildConfig) []File {
	return []File{
		GNUmakefile(cfg),
		Goreleaser(cfg),
		ReleaseWorkflow(cfg),
		RegistryManifest(cfg),
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

// terraformVersion returns the minimum Terraform CLI version stated in the
// generated README's Requirements section.
func (cfg BuildConfig) terraformVersion() string {
	if strings.TrimSpace(cfg.TerraformVersion) != "" {
		return strings.TrimSpace(cfg.TerraformVersion)
	}
	return DefaultTerraformVersion
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
		FrameworkVersion:  TerraformPluginFrameworkVersion,
		PluginGoVersion:   TerraformPluginGoVersion,
		PluginLogVersion:  TerraformPluginLogVersion,
		TestingVersion:    TerraformPluginTestingVersion,
		TimeoutsVersion:   TerraformPluginFrameworkTimeoutsVersion,
		ValidatorsVersion: TerraformPluginFrameworkValidatorsVersion,
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
	if strings.TrimSpace(v.TimeoutsVersion) == "" {
		v.TimeoutsVersion = defaults.TimeoutsVersion
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
		"ModulePath":        cfg.modulePath(),
		"GoVersion":         cfg.goVersion(),
		"FrameworkVersion":  versions.FrameworkVersion,
		"ValidatorsVersion": versions.ValidatorsVersion,
		"PluginGoVersion":   versions.PluginGoVersion,
		"PluginLogVersion":  versions.PluginLogVersion,
		"TestingVersion":    versions.TestingVersion,
		"TimeoutsVersion":   versions.TimeoutsVersion,
	})
}

const goModTemplate = `module {{.ModulePath}}

go {{.GoVersion}}

require (
	github.com/hashicorp/terraform-plugin-framework {{.FrameworkVersion}}
	github.com/hashicorp/terraform-plugin-framework-timeouts {{.TimeoutsVersion}}
	github.com/hashicorp/terraform-plugin-framework-validators {{.ValidatorsVersion}}
	github.com/hashicorp/terraform-plugin-go {{.PluginGoVersion}}
	github.com/hashicorp/terraform-plugin-log {{.PluginLogVersion}}
	github.com/hashicorp/terraform-plugin-testing {{.TestingVersion}}
)
`

// GNUmakefile returns the generated GNUmakefile for the provider.
func GNUmakefile(_ BuildConfig) File {
	return staticFile("GNUmakefile", gnuMakefile)
}

const gnuMakefile = `default: help

all: fmt lint install generate

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

# help is the default target so a bare "make" prints the available targets
# instead of silently running fmt+lint+install+generate.
help:
	@echo "Eidos-generated Terraform provider Makefile"
	@echo ""
	@echo "Usage: make <target>"
	@echo ""
	@echo "Targets:"
	@echo "  build      Build the provider (go build)"
	@echo "  install    Build and install the provider (go install)"
	@echo "  fmt        Format Go sources (gofmt -s)"
	@echo "  lint       Run golangci-lint"
	@echo "  generate   Run go generate (no-op without a tools/ dir)"
	@echo "  test       Run unit tests"
	@echo "  testacc    Run acceptance tests (requires TF_ACC=1)"
	@echo "  all        fmt, lint, install, and generate in one pass"

.PHONY: default all help build install fmt lint generate test testacc
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
      # -buildvcs=false so releases build cleanly in CI/containers where git is
      # absent or the checkout has no VCS metadata to stamp.
      - -buildvcs=false
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

# The Terraform Registry installs providers only from zip archives; a tar.gz
# package fails terraform init with "zip: not a valid zip file". Embed the
# generated terraform-registry-manifest.json in every archive so terraform and
# the registry can learn the provider's supported protocol versions without
# launching the binary.
archives:
  - formats:
      - zip
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    files:
      - src: terraform-registry-manifest.json
        dst: terraform-registry-manifest.json

checksum:
  name_template: "{{ .ProjectName }}_{{ .Version }}_SHA256SUMS"
  algorithm: sha256
[[ if .SignRelease ]]
signs:
  - artifacts: checksum
    args:
      - --batch
      # Headless GPG in CI has no agent/pinentry: --pinentry-mode loopback with
      # the passphrase passed via GPG_PASSPHRASE is what makes --detach-sign
      # succeed on GitHub Actions runners.
      - --pinentry-mode
      - loopback
      - --passphrase
      - "{{ .Env.GPG_PASSPHRASE }}"
      - --local-user
      - "{{ .Env.GPG_FINGERPRINT }}"
      - --output
      - "${signature}"
      - --detach-sign
      - "${artifact}"
    env:
      - GPG_TTY=/dev/null
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

// Default image for the generated dynamic-release workflow when the generator
// config does not override it.
const (
	defaultDynamicReleaseImage = "ghcr.io/signalbreak-labs/eidos:latest"
)

// DynamicReleaseWorkflow returns the generated
// .github/workflows/regenerate-and-release.yml file: a manually-dispatched
// workflow that regenerates the provider from its generator.yaml (which carries
// the spec reference and all overrides) and publishes a release. Regeneration
// runs inside the eidos CI image; GoReleaser runs on a standard Ubuntu runner
// (mirroring the workflow that both published eidos providers use in
// production). When signRelease is true, the workflow imports a GPG key and
// forwards GPG_FINGERPRINT/GPG_PASSPHRASE to GoReleaser so checksums are
// signed, mirroring release.yml.
func DynamicReleaseWorkflow(image, specPath string, signRelease bool) File {
	if strings.TrimSpace(image) == "" {
		image = defaultDynamicReleaseImage
	}
	// An empty SpecPath means "regenerate the spec referenced by the
	// generator.yaml passed via --config"; the workflow only appends an explicit
	// --spec override when the user configured generation.dynamic_release.spec_path.
	return releaseTemplateFile(
		".github/workflows/regenerate-and-release.yml",
		dynamicReleaseWorkflowYAML,
		map[string]any{
			"Image":       image,
			"SpecPath":    strings.TrimSpace(specPath),
			"SignRelease": signRelease,
		})
}

const dynamicReleaseWorkflowYAML = `# Generated by eidos: dynamic regenerate-and-release workflow.
#
# Regenerates this provider from its OpenAPI spec and publishes a release in a
# single manually-dispatched run. Regeneration happens inside the eidos CI
# image (eidos + Go + golangci-lint); the generated code is committed to a
# version tag, and GoReleaser builds the release archives on a standard Ubuntu
# runner. Trigger via Actions → "Regenerate and Release" → Run workflow,
# entering a version like v1.2.3.
#
# The generated provider code is committed to a tag, not to the default branch,
# so the default branch stays clean and keeps only the source-of-truth files:
#
#   generator.yaml
#   .github/workflows/regenerate-and-release.yml
#   .goreleaser.yml
#   terraform-registry-manifest.json
#   GNUmakefile
#   .gitignore
#
# Because the tag is created with the default GITHUB_TOKEN, it does not
# re-trigger a tag-push release workflow. Adjust the container image via
# generation.dynamic_release.image in generator.yaml. Regeneration reads the
# spec and all overrides from generator.yaml; only set
# generation.dynamic_release.spec_path to override the spec. Signed checksums
# are default-on; configure GPG_PRIVATE_KEY and GPG_PASSPHRASE repository
# secrets, or opt out with sign_release: false in generator.yaml.

name: Regenerate and Release

on:
  workflow_dispatch:
    inputs:
      version:
        description: "Release version tag, e.g. v1.2.3"
        required: true
        type: string

permissions:
  contents: write

jobs:
  generate-and-tag:
    name: Generate provider and push tag
    runs-on: ubuntu-latest
    container:
      image: [[ .Image ]]
    outputs:
      version: ${{ steps.tag.outputs.version }}
    steps:
      - name: Install git
        run: |
          apt-get update
          apt-get install -y git

      - name: Checkout
        uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Configure git safe directory
        run: git config --global --add safe.directory "$GITHUB_WORKSPACE"

      - name: Regenerate provider from spec
        # --skip-build keeps the committed build scaffolding (GNUmakefile,
        # .goreleaser.yml, terraform-registry-manifest.json); only the provider
        # code is regenerated into the working tree. --config reads the spec and
        # all overrides from generator.yaml; --force overwrites previously
        # generated files in the working tree. An explicit --spec is appended
        # only when generation.dynamic_release.spec_path overrides the config.
        run: eidos generate --config generator.yaml --skip-build --output . --force[[ if .SpecPath ]] --spec [[ .SpecPath ]][[ end ]]

      - name: Build and test
        run: |
          go mod tidy
          go build -buildvcs=false ./...
          go test -buildvcs=false ./...

      - name: Commit generated code and push tag
        id: tag
        env:
          VERSION: ${{ inputs.version }}
        run: |
          git config user.name "github-actions[bot]"
          git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
          # .gitignore ignores generated provider files on the default branch,
          # so force-add them to the release tag commit.
          git add -f go.mod go.sum main.go internal/ docs/ examples/ README.md .eidos-generated.json
          git commit -m "chore: regenerate provider from spec for ${VERSION}"
          git tag "${VERSION}"
          git push origin "${VERSION}"
          echo "version=${VERSION}" >> "$GITHUB_OUTPUT"

  release:
    name: Release with GoReleaser
    needs: generate-and-tag
    runs-on: ubuntu-latest
    steps:
      - name: Checkout tag
        uses: actions/checkout@v4
        with:
          fetch-depth: 0
          ref: ${{ needs.generate-and-tag.outputs.version }}

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version-file: 'go.mod'
[[ if .SignRelease ]]
      - name: Import GPG key
        id: import_gpg
        uses: crazy-max/ghaction-import-gpg@v6
        with:
          gpg_private_key: ${{ secrets.GPG_PRIVATE_KEY }}
          passphrase: ${{ secrets.GPG_PASSPHRASE }}
[[- end ]]
      - name: Release
        uses: goreleaser/goreleaser-action@v6
        with:
          version: latest
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}[[ if .SignRelease ]]
          GPG_FINGERPRINT: ${{ steps.import_gpg.outputs.fingerprint }}
          GPG_PASSPHRASE: ${{ secrets.GPG_PASSPHRASE }}[[- end ]]
`

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
          GPG_FINGERPRINT: ${{ steps.import_gpg.outputs.fingerprint }}
          GPG_PASSPHRASE: ${{ secrets.GPG_PASSPHRASE }}[[- end ]]
`

// BuildConfigFromIR derives a BuildConfig from a ProviderIR. It uses the
// provider name as both the provider type and the registry namespace, leaves
// the module path at the default derived value, and raises the minimum
// Terraform CLI version to the highest version the provider's constructs
// require: actions and list resources need Terraform 1.14+ (`action` blocks
// and `terraform query`), ephemeral resources 1.10+.
func BuildConfigFromIR(provider *ir.ProviderIR) BuildConfig {
	name := "generated"
	if provider != nil && strings.TrimSpace(provider.Name) != "" {
		name = provider.Name
	}
	return BuildConfig{
		ProviderName:     name,
		Namespace:        name,
		TerraformVersion: minTerraformVersionForIR(provider),
		// Signed releases are default-on. The sign_release generator.yaml field
		// (a *bool, threaded through CollectOptions.SignRelease) overrides this
		// to false to opt out.
		SignRelease: true,
	}
}

// minTerraformVersionForIR returns the minimum Terraform CLI version the
// provider's construct mix requires: 1.14 for actions or list resources,
// 1.10 for ephemeral resources, otherwise the DefaultTerraformVersion floor.
func minTerraformVersionForIR(provider *ir.ProviderIR) string {
	if provider == nil {
		return DefaultTerraformVersion
	}
	if len(provider.Actions) > 0 || len(provider.ListResources) > 0 {
		return "1.14"
	}
	if len(provider.EphemeralResources) > 0 {
		return "1.10"
	}
	return DefaultTerraformVersion
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

	// The sign_release generator.yaml field (a *bool) overrides the default-on
	// signing from BuildConfigFromIR. nil means "unset" → keep the default; an
	// explicit false opts out of GPG signing. Applied before the OnlyBuild
	// short-circuit so scaffolding-only runs honor the override too.
	if opts.SignRelease != nil {
		cfg.SignRelease = *opts.SignRelease
	}

	// OnlyBuild short-circuits to exactly the build/CI/release scaffolding,
	// skipping the always-on core files so write mode emits the same files
	// record mode lists. An opted-in dynamic release workflow is still emitted
	// so --only-build --dynamic-release produces the regenerate-and-release
	// workflow alongside the static scaffolding (M-78).
	if opts.OnlyBuild {
		files := releaseFiles(cfg)
		if opts.IncludeDynamicRelease {
			// The dynamic workflow keeps the default branch free of generated
			// provider code, so it needs a .gitignore that marks that code as
			// ignored on the default branch (it is force-added to release tags).
			files = append(files, ProviderGitignore(), DynamicReleaseWorkflow(opts.DynamicReleaseImage, opts.DynamicReleaseSpecPath, cfg.SignRelease))
		}
		return files, nil
	}

	files := make([]File, 0)
	// Core root files (go module, README, entrypoint) are always emitted; they
	// are part of any compilable provider. The build/CI/release scaffolding
	// (GNUmakefile, .goreleaser.yml, release workflow, registry manifest) is
	// gated by IncludeBuild so a full run can omit it (--skip-build).
	// BuildFiles(cfg) returns the full set and is kept for test callers that
	// assemble a buildable provider directly.
	files = append(files, GoMod(cfg), Readme(cfg), MainGoFile(cfg))
	if opts.IncludeBuild {
		files = append(files, releaseFiles(cfg)...)
	}

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
	if opts.IncludeDynamicRelease {
		// The dynamic workflow keeps the default branch free of generated
		// provider code, so it needs a .gitignore that marks that code as
		// ignored on the default branch (it is force-added to release tags).
		files = append(files, ProviderGitignore(), DynamicReleaseWorkflow(opts.DynamicReleaseImage, opts.DynamicReleaseSpecPath, cfg.SignRelease))
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
