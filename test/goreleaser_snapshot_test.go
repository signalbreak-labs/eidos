package integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/generator"
)

// TestGoreleaserSnapshot generates a minimal provider, creates a v0.1.0 tag,
// and runs `goreleaser build --snapshot --single-target` to verify that the
// generated .goreleaser.yml produces a binary named with the
// terraform-provider-<name>_v<version> convention expected by the Terraform
// Registry installer. A full `goreleaser release --snapshot` (cross-compiled
// archive/checksum matrix) is not run here; the archive and checksum name
// templates are already covered by TestGoldenFiles, and the full matrix exceeds
// the test's per-command timeout for no additional assertion value. The test
// does not push anything to GitHub or the Terraform Registry.
func TestGoreleaserSnapshot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("git/goreleaser shell integration is not tested on Windows")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	if _, err := exec.LookPath("goreleaser"); err != nil {
		t.Skip("goreleaser not installed")
	}

	tmp := t.TempDir()
	providerName := "mycloud"
	namespace := "acme"

	cfg := generator.BuildConfig{
		ProviderName: providerName,
		Namespace:    namespace,
		ModulePath:   fmt.Sprintf("github.com/%s/terraform-provider-%s", namespace, providerName),
		GoVersion:    generator.DefaultGoVersion,
	}

	h := generator.Harness{OutputDir: tmp}
	if err := h.Generate(generator.BuildFiles(cfg)); err != nil {
		t.Fatalf("failed to generate build files: %v", err)
	}

	// Write a minimal provider implementation so the generated main.go compiles.
	providerDir := filepath.Join(tmp, "internal", "provider")
	if err := os.MkdirAll(providerDir, 0o750); err != nil {
		t.Fatalf("failed to create provider directory: %v", err)
	}
	providerGo := filepath.Join(providerDir, "provider.go")
	if err := os.WriteFile(providerGo, []byte(minimalProviderGo(providerName)), 0o600); err != nil {
		t.Fatalf("failed to write provider.go: %v", err)
	}

	// Initialize a git repository and tag the initial release.
	runCmd(t, tmp, "git", "init", "--quiet", "--initial-branch=main")
	runCmd(t, tmp, "git", "config", "user.email", "eidos@example.com")
	runCmd(t, tmp, "git", "config", "user.name", "Eidos")
	runCmd(t, tmp, "git", "add", ".")
	runCmd(t, tmp, "git", "commit", "--quiet", "-m", "Initial generated provider")
	runCmd(t, tmp, "git", "tag", "v0.1.0")

	// Warm the local module cache so the subsequent GoReleaser before-hook
	// (`go mod tidy`) does not require network access.
	runCmd(t, tmp, "go", "mod", "download")

	// Run GoReleaser in snapshot mode, building a single target. A full
	// `goreleaser release --snapshot` cross-compiles the entire OS/arch matrix
	// the generated config declares (~20 targets), which exceeds the test's
	// per-command timeout (and CI's) for no gain: the assertions below only
	// need one built binary, and the archive/checksum name templates are
	// already covered by TestGoldenFiles. `--single-target` builds exactly
	// one binary in a few seconds. Snapshot mode skips the release/publish
	// pipes, so no git remote is required (the temp repo has none).
	runCmd(t, tmp, "goreleaser", "build", "--snapshot", "--single-target")

	// goreleaser build places the binary under dist/<project>_<os>_<arch>_*/
	// named terraform-provider-<name>_v<version>, which is the convention
	// expected by the Terraform Registry installer. Walk dist/ for that file.
	project := "terraform-provider-" + providerName
	wantPrefix := project + "_v"
	var binaryFound string
	err := filepath.WalkDir(filepath.Join(tmp, "dist"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasPrefix(d.Name(), wantPrefix) {
			binaryFound = d.Name()
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk dist directory: %v", err)
	}
	if binaryFound == "" {
		t.Errorf("expected a binary named %s* under dist/, got none", wantPrefix)
	}
}

func runCmd(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %s %s\nerror: %v\noutput:\n%s", name, strings.Join(args, " "), err, out)
	}
}

func minimalProviderGo(name string) string {
	return fmt.Sprintf(`package provider

import (
	"context"

	tfdatasource "github.com/hashicorp/terraform-plugin-framework/datasource"
	tfprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	tfresource "github.com/hashicorp/terraform-plugin-framework/resource"
)

type %sProvider struct{}

func New() tfprovider.Provider {
	return &%sProvider{}
}

func (p *%sProvider) Metadata(ctx context.Context, _ tfprovider.MetadataRequest, resp *tfprovider.MetadataResponse) {
	resp.TypeName = %q
}

func (p *%sProvider) Schema(ctx context.Context, _ tfprovider.SchemaRequest, resp *tfprovider.SchemaResponse) {
}

func (p *%sProvider) Configure(ctx context.Context, _ tfprovider.ConfigureRequest, resp *tfprovider.ConfigureResponse) {
}

func (p *%sProvider) DataSources(ctx context.Context) []func() tfdatasource.DataSource {
	return nil
}

func (p *%sProvider) Resources(ctx context.Context) []func() tfresource.Resource {
	return nil
}
`, name, name, name, name, name, name, name, name)
}
