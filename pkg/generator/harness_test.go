package generator

import (
	"bytes"
	"errors"
	"go/ast"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
)

func sampleGoFile() *astgen.File {
	f := astgen.NewFile("b")
	f.AddComment("Greet returns a greeting.")
	f.AddDecl(astgen.FuncDeclFull(
		"Greet",
		astgen.Params(),
		astgen.Results(astgen.Field("", astgen.Ident("string"), "")),
		astgen.Block(astgen.Return(astgen.Lit("hello"))),
	))
	return f
}

func TestHarness_Generate(t *testing.T) {
	tmp := t.TempDir()
	h := Harness{OutputDir: tmp}

	files := []File{
		Template("zebra.txt", "Z", nil),
		Template("a/hello.txt", "Hello {{.Name}}", map[string]string{"Name": "World"}),
		GoCodeAST("a/b/c.go", sampleGoFile().AST()),
		Template("top.md", "# {{.Title}}", map[string]string{"Title": "Docs"}),
	}

	if err := h.Generate(files); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	cases := []struct {
		path    string
		want    string
		present bool
	}{
		{"a/hello.txt", "Hello World", true},
		{"top.md", "# Docs", true},
		{"zebra.txt", "Z", true},
		{"a/b/c.go", "package b", true},
		{"missing.txt", "", false},
	}

	for _, c := range cases {
		full := filepath.Join(tmp, filepath.FromSlash(c.path))

		got, err := os.ReadFile(full)
		if c.present {
			if err != nil {
				t.Errorf("expected %q to exist: %v", c.path, err)
				continue
			}
			if !strings.Contains(string(got), c.want) {
				t.Errorf("%q = %q, want substring %q", c.path, got, c.want)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("expected %q to be absent, got: %v", c.path, err)
		}
	}

	// Verify all emitted paths are present in sorted order.
	gotPaths := collectPaths(t, tmp)
	wantPaths := []string{"a/b/c.go", "a/hello.txt", "top.md", "zebra.txt"}
	if diff := sliceDiff(wantPaths, gotPaths); diff != "" {
		t.Errorf("emitted paths mismatch:\n%s", diff)
	}
}

func TestHarness_Generate_Idempotent(t *testing.T) {
	files := []File{
		Template("zebra.txt", "Z", nil),
		Template("a/hello.txt", "Hello {{.Name}}", map[string]string{"Name": "World"}),
		GoCodeAST("a/b/c.go", sampleGoFile().AST()),
	}

	run1, run2 := t.TempDir(), t.TempDir()
	for _, dir := range []string{run1, run2} {
		if err := (&Harness{OutputDir: dir}).Generate(files); err != nil {
			t.Fatalf("Generate() error = %v", err)
		}
	}

	if err := dirsEqual(run1, run2); err != nil {
		t.Errorf("idempotency failed: %v", err)
	}
}

func TestHarness_Generate_DeterministicOrder(t *testing.T) {
	// Files are deliberately supplied out of order.
	files := []File{
		Template("z/last.txt", "last", nil),
		Template("a/first.txt", "first", nil),
		Template("m/middle.txt", "middle", nil),
	}

	first, second := t.TempDir(), t.TempDir()
	if err := (&Harness{OutputDir: first}).Generate(files); err != nil {
		t.Fatalf("Generate() first error = %v", err)
	}

	// Reverse the supplied order and generate again.
	reversed := []File{files[2], files[1], files[0]}
	if err := (&Harness{OutputDir: second}).Generate(reversed); err != nil {
		t.Fatalf("Generate() second error = %v", err)
	}

	if err := dirsEqual(first, second); err != nil {
		t.Errorf("reordered generation produced different output: %v", err)
	}
}

func TestHarness_Generate_DuplicatePath(t *testing.T) {
	h := Harness{OutputDir: t.TempDir()}
	files := []File{
		Template("dup.txt", "one", nil),
		Template("dup.txt", "two", nil),
	}

	err := h.Generate(files)
	if !errors.Is(err, ErrDuplicatePath) {
		t.Fatalf("expected ErrDuplicatePath, got: %v", err)
	}
}

func TestHarness_Generate_DuplicatePathCaseInsensitive(t *testing.T) {
	h := Harness{OutputDir: t.TempDir()}
	files := []File{
		Template("Foo.txt", "one", nil),
		Template("foo.txt", "two", nil),
	}

	err := h.Generate(files)
	if !errors.Is(err, ErrDuplicatePath) {
		t.Fatalf("expected ErrDuplicatePath for case-insensitive duplicate, got: %v", err)
	}
}

func TestHarness_Generate_DuplicatePathCaseInsensitiveNonAdjacent(t *testing.T) {
	h := Harness{OutputDir: t.TempDir()}
	files := []File{
		Template("a.txt", "one", nil),
		Template("B.txt", "two", nil),
		Template("A.txt", "three", nil),
	}

	err := h.Generate(files)
	if !errors.Is(err, ErrDuplicatePath) {
		t.Fatalf("expected ErrDuplicatePath for non-adjacent case-insensitive duplicate, got: %v", err)
	}
}

func TestHarness_Generate_RenderError(t *testing.T) {
	h := Harness{OutputDir: t.TempDir()}
	files := []File{
		{
			Path: "bad.txt",
			Render: func(w io.Writer) error {
				return errors.New("render failed")
			},
		},
	}

	err := h.Generate(files)
	if err == nil {
		t.Fatal("expected render error, got nil")
	}
}

func TestHarness_Generate_TemplateExecutionError(t *testing.T) {
	h := Harness{OutputDir: t.TempDir()}
	// Missing map keys do not produce an execution error in text/template,
	// so exercise the Execute error path with a function that returns an error.
	files := []File{
		Template("bad.txt", "{{call .Bad}}", map[string]any{
			"Bad": func() (string, error) { return "", errors.New("boom") },
		}),
	}

	err := h.Generate(files)
	if err == nil {
		t.Fatal("expected template execution error, got nil")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("expected error to wrap template failure, got: %v", err)
	}

	if _, statErr := os.Stat(filepath.Join(h.OutputDir, "bad.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("expected no file written after execution error, got: %v", statErr)
	}
}

func TestHarness_Generate_TemplateParseError(t *testing.T) {
	h := Harness{OutputDir: t.TempDir()}
	files := []File{
		Template("broken.txt", "{{.NoClosing", nil),
	}

	err := h.Generate(files)
	if err == nil {
		t.Fatal("expected template parse error, got nil")
	}

	if _, statErr := os.Stat(filepath.Join(h.OutputDir, "broken.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("expected no file written after parse error, got: %v", statErr)
	}
}

func TestHarness_Generate_GoCodeASTNil(t *testing.T) {
	h := Harness{OutputDir: t.TempDir()}
	files := []File{
		GoCodeAST("nil.go", nil),
	}

	err := h.Generate(files)
	if err == nil {
		t.Fatal("expected error for nil ast.File, got nil")
	}

	if _, statErr := os.Stat(filepath.Join(h.OutputDir, "nil.go")); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("expected no file written for nil ast.File, got: %v", statErr)
	}
}

func TestHarness_Generate_AtomicRender(t *testing.T) {
	h := Harness{OutputDir: t.TempDir()}
	files := []File{
		{
			Path: "a.txt",
			Render: func(w io.Writer) error {
				_, err := io.WriteString(w, "a")
				return err
			},
		},
		{
			Path: "b.txt",
			Render: func(w io.Writer) error {
				return errors.New("render boom")
			},
		},
	}

	err := h.Generate(files)
	if err == nil {
		t.Fatal("expected render error, got nil")
	}

	if _, statErr := os.Stat(filepath.Join(h.OutputDir, "a.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("expected no partial file after render error, got: %v", statErr)
	}
}

func TestHarness_Generate_WritePhasePartialOutput(t *testing.T) {
	tmp := t.TempDir()
	h := Harness{OutputDir: tmp}

	files := []File{
		{
			Path: "a.txt",
			Render: func(w io.Writer) error {
				_, err := io.WriteString(w, "a")
				return err
			},
		},
		{
			Path: "b.txt",
			Render: func(w io.Writer) error {
				_, err := io.WriteString(w, "b")
				return err
			},
		},
	}

	// Pre-create b.txt's target as a directory so the write phase fails.
	if err := os.MkdirAll(filepath.Join(tmp, "b.txt"), 0o750); err != nil {
		t.Fatalf("mkdir b.txt: %v", err)
	}

	err := h.Generate(files)
	if err == nil {
		t.Fatal("expected write error, got nil")
	}

	// The implementation is not atomic across the write phase: a.txt is written
	// before b.txt fails. This test documents the current behavior; callers
	// that need all-or-nothing writes should render to a temp directory and
	// rename on success.
	if _, statErr := os.Stat(filepath.Join(tmp, "a.txt")); errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("expected a.txt to be written before the write error, but it was absent: %v", statErr)
	}
}

func TestHarness_Generate_EmptyOutputDir(t *testing.T) {
	h := Harness{}
	if err := h.Generate([]File{{Path: "x.txt", Render: func(w io.Writer) error { return nil }}}); err == nil {
		t.Fatal("expected error for empty OutputDir, got nil")
	}
}

func TestHarness_Generate_PathTraversal(t *testing.T) {
	parent := t.TempDir()
	out := filepath.Join(parent, "output")
	if err := os.MkdirAll(out, 0o750); err != nil {
		t.Fatalf("mkdir output: %v", err)
	}
	h := Harness{OutputDir: out}

	before := dirEntries(t, parent)

	reject := []string{"../escape.txt", "/abs.txt", "..", "a/../../escape.txt", "a/b/../../../escape.txt", ".", ""}
	for _, path := range reject {
		files := []File{{Path: path, Render: func(w io.Writer) error {
			_, err := io.WriteString(w, "escaped")
			return err
		}}}
		if err := h.Generate(files); err == nil {
			t.Errorf("path %q should be rejected", path)
		}
	}

	after := dirEntries(t, parent)
	for name := range after {
		if _, ok := before[name]; !ok {
			t.Errorf("reject case created unexpected entry in parent directory: %q", name)
		}
	}

	// Harmless normalizations that stay inside the output directory are allowed.
	allow := map[string]string{
		"a/../b":     "b",
		"a/b/../c":   "a/c",
		"./file.txt": "file.txt",
	}
	for path, wantFile := range allow {
		dir := t.TempDir()
		if err := (&Harness{OutputDir: dir}).Generate([]File{{Path: path, Render: func(w io.Writer) error {
			_, err := io.WriteString(w, "ok")
			return err
		}}}); err != nil {
			t.Errorf("path %q should be allowed: %v", path, err)
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, wantFile)); err != nil {
			t.Errorf("expected file %q for path %q: %v", wantFile, path, err)
		}
	}
}

func collectPaths(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk output dir: %v", err)
	}
	sort.Strings(paths)
	return paths
}

func dirsEqual(a, b string) error {
	filesA, err := readDirAll(a)
	if err != nil {
		return err
	}
	filesB, err := readDirAll(b)
	if err != nil {
		return err
	}
	if len(filesA) != len(filesB) {
		return errors.New("different file count")
	}

	for path, contentA := range filesA {
		contentB, ok := filesB[path]
		if !ok {
			return errors.New("missing file: " + path)
		}
		if !bytes.Equal(contentA, contentB) {
			return errors.New("content differs: " + path)
		}
	}
	return nil
}

func readDirAll(root string) (map[string][]byte, error) {
	out := make(map[string][]byte)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = content
		return nil
	})
	return out, err
}

func sliceDiff(want, got []string) string {
	if len(want) != len(got) {
		return formatSliceDiff(want, got)
	}
	for i := range want {
		if want[i] != got[i] {
			return formatSliceDiff(want, got)
		}
	}
	return ""
}

func formatSliceDiff(want, got []string) string {
	return "want: " + strings.Join(want, ", ") + "\ngot:  " + strings.Join(got, ", ")
}

func dirEntries(t *testing.T, dir string) map[string]struct{} {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %q: %v", dir, err)
	}
	m := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		m[e.Name()] = struct{}{}
	}
	return m
}

func TestHarness_Generate_RecoversRendererPanic(t *testing.T) {
	tmp := t.TempDir()
	h := Harness{OutputDir: tmp}

	panicFile := File{
		Path: "panic.txt",
		Render: func(io.Writer) error {
			panic("intentional renderer panic")
		},
	}

	err := h.Generate([]File{panicFile})
	if err == nil {
		t.Fatal("expected error for renderer panic, got nil")
	}
	if !strings.Contains(err.Error(), "renderer panic") {
		t.Errorf("expected error to mention renderer panic, got %v", err)
	}
}

// TestRenderEntitySafely_RecoversConstructionPanic locks in the N-23 contract
// for every entity file (resource, data source, action, ephemeral, list
// resource, provider, function): AST construction is routed through
// renderEntitySafely so a renderer panic is surfaced as an error the caller can
// turn into an ErrorFile, instead of crashing the whole eidos generate run.
// The string panic is the common hostile case; the error-valued panic proves
// the error (and its wrapping via %w) is preserved rather than re-stringified.
func TestRenderEntitySafely_RecoversConstructionPanic(t *testing.T) {
	t.Run("string panic becomes renderer panic error", func(t *testing.T) {
		_, err := renderEntitySafely(func() (*ast.File, error) {
			panic("intentional construction panic")
		})
		if err == nil {
			t.Fatal("expected error for construction panic, got nil")
		}
		if !strings.Contains(err.Error(), "renderer panic") {
			t.Errorf("expected error to mention renderer panic, got %v", err)
		}
		if !strings.Contains(err.Error(), "intentional construction panic") {
			t.Errorf("expected error to carry the panic value, got %v", err)
		}
	})

	t.Run("error panic wraps original error", func(t *testing.T) {
		sentinel := errors.New("hostile IR sentinel")
		_, err := renderEntitySafely(func() (*ast.File, error) {
			panic(sentinel)
		})
		if err == nil {
			t.Fatal("expected error for construction panic, got nil")
		}
		if !strings.Contains(err.Error(), "renderer panic") {
			t.Errorf("expected error to mention renderer panic, got %v", err)
		}
		if !errors.Is(err, sentinel) {
			t.Errorf("expected %v to wrap sentinel %v", err, sentinel)
		}
	})

	t.Run("no panic returns the built file", func(t *testing.T) {
		got, err := renderEntitySafely(func() (*ast.File, error) {
			return astgen.NewFile("provider").AST(), nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got == nil || got.Name == nil || got.Name.Name != "provider" {
			t.Fatalf("expected provider AST file, got %#v", got)
		}
	})
}

// TestRenderFileSafely_PreservesErrorChain locks in the N-32 fix: renderFileSafely
// wraps an error-valued panic with %w so errors.Is/errors.As work through the
// render boundary (previously %v flattened it, so the same panic that
// errors.Is-matched through the construction wrappers failed to match through
// Render).
func TestRenderFileSafely_PreservesErrorChain(t *testing.T) {
	sentinel := errors.New("hostile render sentinel")

	var buf bytes.Buffer
	err := renderFileSafely(File{
		Path: "x.go",
		Render: func(_ io.Writer) error {
			panic(sentinel)
		},
	}, &buf)
	if err == nil {
		t.Fatal("expected error for render panic, got nil")
	}
	if !strings.Contains(err.Error(), "renderer panic") {
		t.Errorf("expected error to mention renderer panic, got %v", err)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected %v to wrap sentinel %v (N-32)", err, sentinel)
	}
}

func TestClearTemplateCache(t *testing.T) {
	// Render a template to populate the cache, then clear it.
	file := Template("cached.txt", "value: {{.V}}", map[string]int{"V": 1})
	var buf strings.Builder
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if buf.String() != "value: 1" {
		t.Errorf("unexpected render output: %q", buf.String())
	}

	ClearTemplateCache()

	// Re-render after clearing; it should still succeed and use the same output.
	var buf2 strings.Builder
	if err := file.Render(&buf2); err != nil {
		t.Fatalf("Render() after clear error = %v", err)
	}
	if buf2.String() != "value: 1" {
		t.Errorf("unexpected render output after clear: %q", buf2.String())
	}
}
