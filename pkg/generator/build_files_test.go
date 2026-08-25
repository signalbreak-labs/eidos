package generator

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	// gopkg.in/yaml.v3 is used here because it is a lightweight, stable
	// YAML 1.2 parser that is sufficient for verifying the generated
	// .goreleaser.yml structure without pulling in a larger Kubernetes
	// or JSON-transcoding dependency tree.
	"gopkg.in/yaml.v3"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

func TestBuildFiles(t *testing.T) {
	cfg := BuildConfig{
		ProviderName: "mycloud",
		Namespace:    "mycloud",
	}

	h := Harness{OutputDir: t.TempDir()}
	if err := h.Generate(BuildFiles(cfg)); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	wantPaths := []string{
		".github/workflows/release.yml",
		".goreleaser.yml",
		"GNUmakefile",
		"README.md",
		"go.mod",
		"main.go",
		"terraform-registry-manifest.json",
	}
	gotPaths := collectPaths(t, h.OutputDir)
	if diff := sliceDiff(wantPaths, gotPaths); diff != "" {
		t.Errorf("emitted paths mismatch:\n%s", diff)
	}
}

func TestGoMod(t *testing.T) {
	cfg := BuildConfig{
		ProviderName: "mycloud",
		Namespace:    "acme",
		ModulePath:   "github.com/acme/terraform-provider-mycloud",
		GoVersion:    "1.26",
	}

	h := Harness{OutputDir: t.TempDir()}
	if err := h.Generate([]File{GoMod(cfg)}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := readFile(t, h.OutputDir, "go.mod")
	for _, want := range []string{
		"module github.com/acme/terraform-provider-mycloud",
		"go 1.26",
		"github.com/hashicorp/terraform-plugin-framework " + TerraformPluginFrameworkVersion,
		"github.com/hashicorp/terraform-plugin-go " + TerraformPluginGoVersion,
		"github.com/hashicorp/terraform-plugin-log " + TerraformPluginLogVersion,
		"github.com/hashicorp/terraform-plugin-testing " + TerraformPluginTestingVersion,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("go.mod missing %q\ncontent:\n%s", want, content)
		}
	}
}

func TestGoMod_Defaults(t *testing.T) {
	cfg := BuildConfig{
		ProviderName: "mycloud",
		Namespace:    "acme",
	}

	h := Harness{OutputDir: t.TempDir()}
	if err := h.Generate([]File{GoMod(cfg)}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := readFile(t, h.OutputDir, "go.mod")
	wantModule := "module github.com/acme/terraform-provider-mycloud"
	if !strings.Contains(content, wantModule) {
		t.Errorf("go.mod missing default module path %q\ncontent:\n%s", wantModule, content)
	}
	if !strings.Contains(content, "go "+DefaultGoVersion) {
		t.Errorf("go.mod missing default go version %s\ncontent:\n%s", DefaultGoVersion, content)
	}
}

func TestGNUmakefile(t *testing.T) {
	cfg := BuildConfig{
		ProviderName: "mycloud",
		Namespace:    "mycloud",
	}

	h := Harness{OutputDir: t.TempDir()}
	if err := h.Generate([]File{GNUmakefile(cfg)}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := readFile(t, h.OutputDir, "GNUmakefile")
	for _, want := range []string{
		"default: help",
		"all: fmt lint install generate",
		"help:",
		"build:",
		"install: build",
		"lint:",
		"generate:",
		"fmt:",
		"test:",
		"testacc:",
		".PHONY: default all help build install fmt lint generate test testacc",
		"go generate ./...", "[ -d tools ]",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("GNUmakefile missing %q\ncontent:\n%s", want, content)
		}
	}
	// help must be the default target so a bare "make" prints help rather than
	// silently running the build chain. The file must lead with "default: help".
	if !strings.HasPrefix(content, "default: help\n") {
		t.Errorf("GNUmakefile must start with %q so make defaults to help; got:\n%s", "default: help", content)
	}
	if strings.Contains(content, "tools/ directory not found") {
		t.Errorf("GNUmakefile should not hard-fail when tools/ is absent (M-9):\n%s", content)
	}

	// Recipe lines must be tab-indented or Make will refuse to parse the file.
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(line, "\t") {
			continue // recipe line
		}
		if !strings.Contains(line, ":") {
			t.Errorf("GNUmakefile line is neither a target nor a tab-indented recipe: %q", line)
		}
	}
}

func TestGoreleaser(t *testing.T) {
	cfg := BuildConfig{
		ProviderName: "mycloud",
		Namespace:    "mycloud",
	}

	h := Harness{OutputDir: t.TempDir()}
	if err := h.Generate([]File{Goreleaser(cfg)}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := readFile(t, h.OutputDir, ".goreleaser.yml")
	for _, want := range []string{
		"version: 2",
		"project_name: terraform-provider-mycloud",
		"terraform-provider-mycloud_v{{ .Version }}",
		"CGO_ENABLED=0",
		"-trimpath",
		"-buildvcs=false",
		"-s -w -X main.version={{ .Version }} -X main.commit={{ .Commit }} -X main.date={{ .CommitDate }}",
		"darwin",
		"linux",
		"windows",
		"freebsd",
		"openbsd",
		"solaris",
		"amd64",
		"arm64",
		"arm",
		"\"386\"",
		"formats:",
		"- zip",
		"files:",
		"- src: terraform-registry-manifest.json",
		"  dst: terraform-registry-manifest.json",
		"name_template: \"{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}\"",
		"name_template: \"{{ .ProjectName }}_{{ .Version }}_SHA256SUMS\"",
		"algorithm: sha256",
	} {
		if !strings.Contains(content, want) {
			t.Errorf(".goreleaser.yml missing %q\ncontent:\n%s", want, content)
		}
	}

	for _, unwanted := range []string{
		"signs:",
		"GPG_FINGERPRINT",
		"--detach-sign",
	} {
		if strings.Contains(content, unwanted) {
			t.Errorf("unsigned .goreleaser.yml unexpectedly contains %q\ncontent:\n%s", unwanted, content)
		}
	}

	var gr goreleaserConfig
	if err := yaml.Unmarshal([]byte(content), &gr); err != nil {
		t.Fatalf("yaml.Unmarshal(.goreleaser.yml): %v", err)
	}
	if gr.Version != 2 {
		t.Errorf("goreleaser version = %d, want 2", gr.Version)
	}
	if len(gr.Builds) == 0 {
		t.Fatalf("goreleaser builds missing")
	}
	if !sliceContains(gr.Builds[0].Env, "CGO_ENABLED=0") {
		t.Errorf("goreleaser builds[0].env missing CGO_ENABLED=0: %v", gr.Builds[0].Env)
	}
	if len(gr.Archives) == 0 || len(gr.Archives[0].Formats) == 0 || gr.Archives[0].Formats[0] != "zip" {
		t.Errorf("goreleaser archives[0].formats = %v, want [zip]", gr.Archives[0].Formats)
	}
	if len(gr.Archives[0].Files) == 0 || gr.Archives[0].Files[0].Src != "terraform-registry-manifest.json" {
		t.Errorf("goreleaser archives[0].files = %+v, want embedded terraform-registry-manifest.json", gr.Archives[0].Files)
	}
	if len(gr.Signs) != 0 {
		t.Errorf("unsigned .goreleaser.yml signs = %+v, want empty", gr.Signs)
	}
}

func TestGoreleaser_Signed(t *testing.T) {
	cfg := BuildConfig{
		ProviderName: "mycloud",
		Namespace:    "mycloud",
		SignRelease:  true,
	}

	h := Harness{OutputDir: t.TempDir()}
	if err := h.Generate([]File{Goreleaser(cfg)}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := readFile(t, h.OutputDir, ".goreleaser.yml")
	for _, want := range []string{
		"signs:",
		"artifacts: checksum",
		"GPG_FINGERPRINT",
		"--detach-sign",
		"--pinentry-mode",
		"loopback",
		"GPG_PASSPHRASE",
		"GPG_TTY=/dev/null",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("signed .goreleaser.yml missing %q\ncontent:\n%s", want, content)
		}
	}

	var gr goreleaserConfig
	if err := yaml.Unmarshal([]byte(content), &gr); err != nil {
		t.Fatalf("yaml.Unmarshal(.goreleaser.yml): %v", err)
	}
	foundSign := false
	for _, sign := range gr.Signs {
		if sign.Artifacts == "checksum" && sliceContains(sign.Args, "--detach-sign") && sliceContains(sign.Args, "{{ .Env.GPG_FINGERPRINT }}") {
			foundSign = true
			break
		}
	}
	if !foundSign {
		t.Errorf("signed .goreleaser.yml missing checksum sign with GPG_FINGERPRINT: %+v", gr.Signs)
	}
}

type goreleaserConfig struct {
	Version int `yaml:"version"`
	Before  struct {
		Hooks []string `yaml:"hooks"`
	} `yaml:"before"`
	Builds []struct {
		Env    []string `yaml:"env"`
		Goos   []string `yaml:"goos"`
		Goarch []string `yaml:"goarch"`
	} `yaml:"builds"`
	Archives []struct {
		Formats []string `yaml:"formats"`
		Files   []struct {
			Src string `yaml:"src"`
			Dst string `yaml:"dst"`
		} `yaml:"files"`
	} `yaml:"archives"`
	Checksum struct {
		Algorithm string `yaml:"algorithm"`
	} `yaml:"checksum"`
	Signs []struct {
		Artifacts string   `yaml:"artifacts"`
		Args      []string `yaml:"args"`
	} `yaml:"signs"`
}

func TestReleaseWorkflow(t *testing.T) {
	cfg := BuildConfig{
		ProviderName: "mycloud",
		Namespace:    "mycloud",
	}

	h := Harness{OutputDir: t.TempDir()}
	if err := h.Generate([]File{ReleaseWorkflow(cfg)}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := readFile(t, h.OutputDir, ".github/workflows/release.yml")
	for _, want := range []string{
		"name: Release",
		"on:",
		"tags:",
		"jobs:",
		"actions/checkout@v4",
		"actions/setup-go@v5",
		"goreleaser/goreleaser-action@v6",
		"GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}",
		"Configure GPG_PRIVATE_KEY",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("release workflow missing %q\ncontent:\n%s", want, content)
		}
	}

	for _, unwanted := range []string{
		"Import GPG key",
		"crazy-max/ghaction-import-gpg@v6",
		"GPG_FINGERPRINT: ${{ steps.import_gpg.outputs.fingerprint }}",
	} {
		if strings.Contains(content, unwanted) {
			t.Errorf("unsigned release workflow unexpectedly contains %q\ncontent:\n%s", unwanted, content)
		}
	}

	var parsed map[string]interface{}
	if err := yaml.Unmarshal([]byte(content), &parsed); err != nil {
		t.Fatalf("yaml.Unmarshal(release.yml): %v", err)
	}
}

func TestReleaseWorkflow_Signed(t *testing.T) {
	cfg := BuildConfig{
		ProviderName: "mycloud",
		Namespace:    "mycloud",
		SignRelease:  true,
	}

	h := Harness{OutputDir: t.TempDir()}
	if err := h.Generate([]File{ReleaseWorkflow(cfg)}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := readFile(t, h.OutputDir, ".github/workflows/release.yml")
	for _, want := range []string{
		"Import GPG key",
		"crazy-max/ghaction-import-gpg@v6",
		"GPG_PRIVATE_KEY",
		"GPG_PASSPHRASE: ${{ secrets.GPG_PASSPHRASE }}",
		"GPG_FINGERPRINT: ${{ steps.import_gpg.outputs.fingerprint }}",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("signed release workflow missing %q\ncontent:\n%s", want, content)
		}
	}

	var parsed map[string]interface{}
	if err := yaml.Unmarshal([]byte(content), &parsed); err != nil {
		t.Fatalf("yaml.Unmarshal(release.yml): %v", err)
	}
}

// minimalSignProvider returns a ProviderIR just substantial enough to drive
// FilesForProviderIR without error (it needs a name and at least the core
// provider file generation to succeed).
func minimalSignProvider() *ir.ProviderIR {
	return &ir.ProviderIR{
		Name:    "mycloud",
		Version: "1.0.0",
	}
}

// TestBuildConfigFromIR_DefaultsSignRelease asserts signed releases are
// default-on: BuildConfigFromIR sets SignRelease true so a bare `eidos generate`
// with no generator.yaml produces signed .goreleaser.yml and release workflows.
func TestBuildConfigFromIR_DefaultsSignRelease(t *testing.T) {
	cfg := BuildConfigFromIR(minimalSignProvider())
	if !cfg.SignRelease {
		t.Errorf("BuildConfigFromIR SignRelease = false, want true (signed-by-default)")
	}
}

// TestFilesForProviderIR_SignReleaseOverride asserts the sign_release
// generator.yaml field (threaded as CollectOptions.SignRelease *bool) overrides
// the default-on signing: nil keeps signed, explicit false opts out.
func TestFilesForProviderIR_SignReleaseOverride(t *testing.T) {
	provider := minimalSignProvider()

	// nil override → default-on (signed).
	files, err := FilesForProviderIR(provider, BuildConfigFromIR(provider), CollectOptions{OnlyBuild: true, SignRelease: nil})
	if err != nil {
		t.Fatalf("FilesForProviderIR: %v", err)
	}
	h := Harness{OutputDir: t.TempDir()}
	if err := h.Generate(files); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got := readFile(t, h.OutputDir, ".goreleaser.yml"); !strings.Contains(got, "signs:") {
		t.Errorf("nil SignRelease override should keep signed-by-default; .goreleaser.yml missing signs:\n%s", got)
	}

	// explicit false → opt out (unsigned).
	falseVal := false
	files, err = FilesForProviderIR(provider, BuildConfigFromIR(provider), CollectOptions{OnlyBuild: true, SignRelease: &falseVal})
	if err != nil {
		t.Fatalf("FilesForProviderIR: %v", err)
	}
	h2 := Harness{OutputDir: t.TempDir()}
	if err := h2.Generate(files); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got := readFile(t, h2.OutputDir, ".goreleaser.yml"); strings.Contains(got, "signs:") {
		t.Errorf("SignRelease=false should opt out of signing; .goreleaser.yml unexpectedly contains signs:\n%s", got)
	}
}

func TestGoMod_BuildVersionsOverride(t *testing.T) {
	cases := []struct {
		name        string
		versions    BuildVersions
		wantPresent []string
		wantAbsent  []string
	}{
		{
			name: "full override",
			versions: BuildVersions{
				FrameworkVersion: "v9.9.9",
				PluginGoVersion:  "v8.8.8",
				PluginLogVersion: "v7.7.7",
				TestingVersion:   "v6.6.6",
			},
			wantPresent: []string{
				"github.com/hashicorp/terraform-plugin-framework v9.9.9",
				"github.com/hashicorp/terraform-plugin-go v8.8.8",
				"github.com/hashicorp/terraform-plugin-log v7.7.7",
				"github.com/hashicorp/terraform-plugin-testing v6.6.6",
			},
		},
		{
			name: "partial override falls back to defaults",
			versions: BuildVersions{
				FrameworkVersion: "v9.9.9",
			},
			wantPresent: []string{
				"github.com/hashicorp/terraform-plugin-framework v9.9.9",
				"github.com/hashicorp/terraform-plugin-go " + TerraformPluginGoVersion,
				"github.com/hashicorp/terraform-plugin-log " + TerraformPluginLogVersion,
				"github.com/hashicorp/terraform-plugin-testing " + TerraformPluginTestingVersion,
			},
			wantAbsent: []string{
				"github.com/hashicorp/terraform-plugin-framework " + TerraformPluginFrameworkVersion,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := BuildConfig{
				ProviderName:  "mycloud",
				Namespace:     "acme",
				BuildVersions: &tc.versions,
			}

			h := Harness{OutputDir: t.TempDir()}
			if err := h.Generate([]File{GoMod(cfg)}); err != nil {
				t.Fatalf("Generate() error = %v", err)
			}

			content := readFile(t, h.OutputDir, "go.mod")
			for _, want := range tc.wantPresent {
				if !strings.Contains(content, want) {
					t.Errorf("go.mod missing %q\ncontent:\n%s", want, content)
				}
			}
			for _, want := range tc.wantAbsent {
				if strings.Contains(content, want) {
					t.Errorf("go.mod unexpectedly contains %q\ncontent:\n%s", want, content)
				}
			}
		})
	}
}

func TestBuildConfig_Validate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     BuildConfig
		wantErr string
	}{
		{
			name: "valid",
			cfg: BuildConfig{
				ProviderName: "mycloud",
				Namespace:    "acme",
			},
		},
		{
			name: "missing provider name",
			cfg: BuildConfig{
				Namespace: "acme",
			},
			wantErr: "ProviderName is required",
		},
		{
			name: "missing namespace",
			cfg: BuildConfig{
				ProviderName: "mycloud",
			},
			wantErr: "Namespace is required",
		},
		{
			name: "whitespace provider name and namespace",
			cfg: BuildConfig{
				ProviderName: "   ",
				Namespace:    "\t",
			},
			wantErr: "ProviderName is required",
		},
		{
			name: "whitespace module path",
			cfg: BuildConfig{
				ProviderName: "mycloud",
				Namespace:    "acme",
				ModulePath:   "   ",
			},
			wantErr: "ModulePath cannot be whitespace-only",
		},
		{
			name: "whitespace go version",
			cfg: BuildConfig{
				ProviderName: "mycloud",
				Namespace:    "acme",
				GoVersion:    "   ",
			},
			wantErr: "GoVersion cannot be whitespace-only",
		},
		{
			name: "whitespace protocol version",
			cfg: BuildConfig{
				ProviderName:     "mycloud",
				Namespace:        "acme",
				ProtocolVersions: []string{"6.0", "  "},
			},
			wantErr: "ProtocolVersions[1] cannot be whitespace-only",
		},
		{
			name: "empty protocol version entry",
			cfg: BuildConfig{
				ProviderName:     "mycloud",
				Namespace:        "acme",
				ProtocolVersions: []string{""},
			},
			wantErr: "ProtocolVersions[0] cannot be whitespace-only",
		},
		{
			name: "provider name with space (M-10)",
			cfg: BuildConfig{
				ProviderName: "my cloud",
				Namespace:    "acme",
			},
			wantErr: `ProviderName "my cloud" must match`,
		},
		{
			name: "provider name with colon (M-10)",
			cfg: BuildConfig{
				ProviderName: "my:cloud",
				Namespace:    "acme",
			},
			wantErr: `ProviderName "my:cloud" must match`,
		},
		{
			name: "namespace with slash (M-10)",
			cfg: BuildConfig{
				ProviderName: "mycloud",
				Namespace:    "ac/me",
			},
			wantErr: `Namespace "ac/me" must match`,
		},
		{
			name: "namespace with hyphen accepted (M-10)",
			cfg: BuildConfig{
				ProviderName: "mycloud",
				Namespace:    "ac-me-org",
			},
		},
		{
			// Terraform v1.14.7 rejects dots in the provider namespace
			// ("Invalid provider namespace"), matching registrySegmentPattern.
			name: "namespace with dot rejected (M-10)",
			cfg: BuildConfig{
				ProviderName: "mycloud",
				Namespace:    "ac.me-org",
			},
			wantErr: `Namespace "ac.me-org" must match`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() want error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() error = %q, want containing %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestBuildConfig_TrimSpace(t *testing.T) {
	cfg := BuildConfig{
		ProviderName: " mycloud ",
		Namespace:    " acme ",
		ModulePath:   " github.com/acme/terraform-provider-mycloud ",
		GoVersion:    " 1.26 ",
	}

	if got, want := cfg.providerName(), "mycloud"; got != want {
		t.Errorf("providerName() = %q, want %q", got, want)
	}
	if got, want := cfg.namespace(), "acme"; got != want {
		t.Errorf("namespace() = %q, want %q", got, want)
	}
	if got, want := cfg.modulePath(), "github.com/acme/terraform-provider-mycloud"; got != want {
		t.Errorf("modulePath() = %q, want %q", got, want)
	}
	if got, want := cfg.goVersion(), "1.26"; got != want {
		t.Errorf("goVersion() = %q, want %q", got, want)
	}
	if got, want := cfg.sourceAddress(), "registry.terraform.io/acme/mycloud"; got != want {
		t.Errorf("sourceAddress() = %q, want %q", got, want)
	}
}

func readFile(t *testing.T, dir, path string) string {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(path))
	b, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	return string(b)
}

func sliceContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func TestDefaultGoVersionMatchesRuntime(t *testing.T) {
	// runtime.Version returns strings like "go1.26.5" or "devel ...".
	// For released toolchains, the major.minor prefix must match DefaultGoVersion
	// so generated providers do not drift from the generator's own Go version.
	ver := runtime.Version()
	if !strings.HasPrefix(ver, "go") {
		t.Skipf("non-release Go runtime %q; cannot verify version drift", ver)
	}
	parts := strings.Split(strings.TrimPrefix(ver, "go"), ".")
	if len(parts) < 2 {
		t.Fatalf("unexpected runtime version %q", ver)
	}
	want := parts[0] + "." + parts[1]
	if DefaultGoVersion != want {
		t.Errorf("DefaultGoVersion = %q, want %q (from runtime %s); update DefaultGoVersion or the CI toolchain", DefaultGoVersion, want, ver)
	}
}

func TestDynamicReleaseWorkflow_Defaults(t *testing.T) {
	h := Harness{OutputDir: t.TempDir()}
	if err := h.Generate([]File{DynamicReleaseWorkflow("", "", true)}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	content := readFile(t, h.OutputDir, ".github/workflows/regenerate-and-release.yml")
	for _, want := range []string{
		"name: Regenerate and Release",
		"workflow_dispatch:",
		"inputs:",
		"version:",
		"generate-and-tag:",
		"release:",
		"needs: generate-and-tag",
		"container:",
		"image: " + defaultDynamicReleaseImage,
		// Regeneration is config-driven (keeps generator.yaml overrides) and is
		// not passed a --spec unless the user overrides it.
		"eidos generate --config generator.yaml --skip-build --output . --force",
		"Install git",
		"apt-get install -y git",
		"safe.directory",
		"go mod tidy",
		"go build -buildvcs=false ./...",
		"go test -buildvcs=false ./...",
		// Only the tag is pushed; the generated provider code is force-added
		// because .gitignore marks it as ignored on the default branch.
		"git add -f go.mod go.sum main.go internal/ docs/ examples/ README.md .eidos-generated.json",
		"git push origin \"${VERSION}\"",
		"setup-go@v5",
		"go-version-file: 'go.mod'",
		"goreleaser/goreleaser-action@v6",
		"GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}",
		// Signed-by-default: the release job imports a GPG key and forwards the
		// fingerprint and passphrase to GoReleaser.
		"Import GPG key",
		"crazy-max/ghaction-import-gpg@v6",
		"GPG_FINGERPRINT: ${{ steps.import_gpg.outputs.fingerprint }}",
		"GPG_PASSPHRASE: ${{ secrets.GPG_PASSPHRASE }}",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("dynamic release workflow missing %q\ncontent:\n%s", want, content)
		}
	}
	// With no spec_path override, the workflow's generate command must not
	// append a --spec flag (the header comment legitimately mentions --spec).
	if strings.Contains(content, "--force --spec") {
		t.Errorf("dynamic release workflow should not emit --spec without an override\ncontent:\n%s", content)
	}
	// Generated code is committed to a tag, not a release branch.
	if strings.Contains(content, "release/${VERSION}") {
		t.Errorf("dynamic release workflow must not push a release branch\ncontent:\n%s", content)
	}
	// Non-overlapping trigger: must not use tag-push (that belongs to release.yml).
	if strings.Contains(content, "tags:") {
		t.Errorf("dynamic release workflow must not be tag-triggered\ncontent:\n%s", content)
	}
	var parsed map[string]interface{}
	if err := yaml.Unmarshal([]byte(content), &parsed); err != nil {
		t.Fatalf("yaml.Unmarshal(regenerate-and-release.yml): %v\ncontent:\n%s", err, content)
	}
}

func TestDynamicReleaseWorkflow_Unsigned(t *testing.T) {
	h := Harness{OutputDir: t.TempDir()}
	if err := h.Generate([]File{DynamicReleaseWorkflow("", "", false)}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	content := readFile(t, h.OutputDir, ".github/workflows/regenerate-and-release.yml")
	for _, unwanted := range []string{
		"Import GPG key",
		"crazy-max/ghaction-import-gpg@v6",
		"GPG_FINGERPRINT:",
		"GPG_PASSPHRASE: ${{ secrets.GPG_PASSPHRASE }}",
	} {
		if strings.Contains(content, unwanted) {
			t.Errorf("unsigned dynamic workflow unexpectedly contains %q\ncontent:\n%s", unwanted, content)
		}
	}
}

func TestDynamicReleaseWorkflow_Overrides(t *testing.T) {
	h := Harness{OutputDir: t.TempDir()}
	if err := h.Generate([]File{DynamicReleaseWorkflow("ghcr.io/example/eidos:v0.4.2", "openapi.yaml", true)}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	content := readFile(t, h.OutputDir, ".github/workflows/regenerate-and-release.yml")
	if !strings.Contains(content, "image: ghcr.io/example/eidos:v0.4.2") {
		t.Errorf("expected overridden image, got:\n%s", content)
	}
	// An explicit spec_path is appended as a --spec override on top of --config.
	if !strings.Contains(content, "eidos generate --config generator.yaml --skip-build --output . --force --spec openapi.yaml") {
		t.Errorf("expected overridden spec path, got:\n%s", content)
	}
}
