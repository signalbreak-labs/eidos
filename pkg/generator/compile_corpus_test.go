package generator_test

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/api"
	"github.com/signalbreak-labs/eidos/pkg/generator"
)

// TestGoldenFiles_Compile is the per-spec compile corpus (REMAINING_GAPS §6): it
// generates a full provider module from every reference spec in test/specs and
// compiles it with `go build ./...`. The golden snapshot test only checks the
// planned file list and scaffold markers; this test catches generation that
// produces non-compiling Go on real-world spec shapes, and is the safety net
// for any change (such as CRUD-inference grouping) that alters which resources
// are produced and wired. It is skipped in -short mode because it runs go mod
// tidy + go build per spec.
func TestGoldenFiles_Compile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping per-spec compile corpus in -short mode")
	}

	for _, tc := range goldenCases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := os.ReadFile(tc.spec)
			if err != nil {
				t.Fatalf("read spec %s: %v", tc.spec, err)
			}

			resp := api.Validate(data)
			if !resp.Valid {
				t.Fatalf("spec %s produced invalid diagnostics", tc.spec)
			}
			if resp.IRPreview == nil {
				t.Fatalf("spec %s produced no IR preview", tc.spec)
			}

			tmp := t.TempDir()
			if _, err := generator.Run(resp.IRPreview, generator.Options{
				Mode:           generator.ModeWrite,
				OutputDir:      tmp,
				CollectOptions: generator.DefaultCollectOptions(),
			}); err != nil {
				t.Fatalf("generator.Run write for %s: %v", tc.name, err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			tidy := exec.CommandContext(ctx, "go", "mod", "tidy")
			tidy.Dir = tmp
			if out, err := tidy.CombinedOutput(); err != nil {
				t.Fatalf("go mod tidy for %s failed: %v\n%s", tc.name, err, out)
			}

			build := exec.CommandContext(ctx, "go", "build", "./...")
			build.Dir = tmp
			if out, err := build.CombinedOutput(); err != nil {
				t.Fatalf("go build ./... for %s failed: %v\n%s", tc.name, err, out)
			}
		})
	}
}

// TestGoldenFiles_CompileCount is a cheap, always-on guard that the corpus is
// non-empty so a future edit that empties goldenCases does not silently make
// the compile corpus vacuously pass.
func TestGoldenFiles_CompileCount(t *testing.T) {
	if len(goldenCases) == 0 {
		t.Fatalf("goldenCases is empty; the per-spec compile corpus would pass vacuously")
	}
	// Distinct spec paths guard against accidental duplication.
	seen := make(map[string]struct{}, len(goldenCases))
	for _, tc := range goldenCases {
		if _, dup := seen[tc.spec]; dup {
			t.Fatalf("duplicate golden spec %q", tc.spec)
		}
		seen[tc.spec] = struct{}{}
		if tc.name == "" || tc.spec == "" {
			t.Fatalf("golden case has empty name or spec: %+v", tc)
		}
	}
}
