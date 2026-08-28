package generator_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/api"
	"github.com/signalbreak-labs/eidos/pkg/generator"
)

// TestGoldenFiles_Compile is the per-spec compile corpus (REMAINING_GAPS §6): it
// generates a full provider module from every reference spec in test/specs and
// compiles it with `go test -run '^$' ./...`, which builds every package's test
// binary without running any tests. The golden snapshot test only checks the
// planned file list and scaffold markers; this test catches generation that
// produces non-compiling Go on real-world spec shapes, and is the safety net
// for any change (such as CRUD-inference grouping) that alters which resources
// are produced and wired. `go build ./...` alone would not compile the emitted
// *_test.go files (coverage, client, provider, mapper, acceptance), so the
// test-compile form is used to keep those honest too (M-13). It is skipped in
// -short mode because it runs go mod tidy + go test per spec.
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

			// Guard against the test-compile step passing vacuously: the corpus
			// must actually emit *_test.go files for the M-13 check to mean
			// anything. A future change that stops emitting them fails here.
			// filepath.Glob has no ** recursion, so walk the tree.
			testFileCount := 0
			if err := filepath.WalkDir(tmp, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() && strings.HasSuffix(d.Name(), "_test.go") {
					testFileCount++
				}
				return nil
			}); err != nil {
				t.Fatalf("walk test files for %s: %v", tc.name, err)
			}
			if testFileCount == 0 {
				t.Fatalf("generated module for %s contains no *_test.go files; the test-compile corpus would pass vacuously", tc.name)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			tidy := exec.CommandContext(ctx, "go", "mod", "tidy")
			tidy.Dir = tmp
			if out, err := tidy.CombinedOutput(); err != nil {
				t.Fatalf("go mod tidy for %s failed: %v\n%s", tc.name, err, out)
			}

			// `go test -run '^$'` compiles every package's test binary (including
			// the emitted *_test.go files) and runs no tests. This is the check
			// `go build ./...` cannot provide: test files are only compiled by
			// go test / go vet (M-13).
			compile := exec.CommandContext(ctx, "go", "test", "-run", "^$", "./...")
			compile.Dir = tmp
			if out, err := compile.CombinedOutput(); err != nil {
				t.Fatalf("go test -run '^$' ./... for %s failed: %v\n%s", tc.name, err, out)
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
