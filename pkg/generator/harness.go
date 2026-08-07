package generator

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"text/template"
)

// ErrDuplicatePath is returned when two generated files target the same
// relative output path.
var ErrDuplicatePath = errors.New("duplicate output path")

// File describes a single generated output file inside an output directory.
type File struct {
	// Path is the relative path, using forward slashes, where the file is
	// written inside the output directory.
	Path string

	// Render writes the file content to w. It is called once per Generate call.
	Render func(w io.Writer) error
}

// Harness coordinates deterministic emission of generated files to an output
// directory. It supports both text/template and Go source generators built with
// the standard-library astgen package.
//
// The zero value is not usable: OutputDir must be set before calling Generate.
//
// Generate is not safe for concurrent use on the same OutputDir. Callers that
// run generation in parallel should use distinct output directories or provide
// external synchronization.
//
// Default permissions are 0750 for directories and 0640 for files, subject to
// the process umask.
type Harness struct {
	// OutputDir is the root directory where generated files are written.
	OutputDir string

	// RefuseOverwrite, when true, causes Generate to fail if a target file
	// already exists. Combined with O_EXCL, this makes the existence check
	// atomic with the write, eliminating a TOCTOU race.
	RefuseOverwrite bool
}

// Generate writes all files to the output directory, creating intermediate
// directories as needed. Files are processed in lexicographic order by their
// cleaned relative path so that the resulting directory layout is deterministic
// and idempotent across runs with the same input.
//
// All files are rendered into memory before any file is written, which uses
// O(total-output-size) memory. If any render fails, the output directory is left
// unchanged, giving Generate all-or-nothing semantics for the render phase.
//
// Note: the write phase is not atomic. If a write fails after some files have
// been written (for example, because of a permission error or full disk), the
// output directory may contain partial output. Callers that need all-or-nothing
// writes should render to a temporary directory and rename it on success.
func (h *Harness) Generate(files []File) error {
	if h.OutputDir == "" {
		return errors.New("generator.Harness.OutputDir is empty")
	}

	type entry struct {
		file  File
		clean string
	}

	entries := make([]entry, 0, len(files))
	for _, f := range files {
		clean, err := safeRelPath(f.Path)
		if err != nil {
			return err
		}
		entries = append(entries, entry{file: f, clean: clean})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].clean < entries[j].clean
	})

	seen := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		key := strings.ToLower(e.clean)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("%w: %q", ErrDuplicatePath, e.file.Path)
		}
		seen[key] = struct{}{}
	}

	// Render every file into memory first so a render error cannot leave a
	// partially-written output directory. A panic during Render is recovered via
	// renderFileSafely and returned as an error; a panic during File construction
	// is recovered by the per-file wrappers (e.g. ResourceFile, ProviderFile) and
	// surfaced as an ErrorFile. Either way an unexpected IR shape crashes a
	// single file rather than the whole process (L-39 clarifies the prior
	// misleading wording; see also H-5 and M-56).
	rendered := make(map[string][]byte, len(entries))
	for _, e := range entries {
		var buf bytes.Buffer
		if err := renderFileSafely(e.file, &buf); err != nil {
			return fmt.Errorf("render %q: %w", e.file.Path, err)
		}
		rendered[e.clean] = buf.Bytes()
	}

	for _, e := range entries {
		full := filepath.Join(h.OutputDir, e.clean)
		dir := filepath.Dir(full)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create directory for %q: %w", e.file.Path, err)
		}

		flags := os.O_WRONLY | os.O_CREATE
		if h.RefuseOverwrite {
			flags |= os.O_EXCL
		} else {
			flags |= os.O_TRUNC
		}
		out, err := os.OpenFile(full, flags, 0o640) //nolint:gosec // generated file permissions are intentional.
		if err != nil {
			if h.RefuseOverwrite && os.IsExist(err) {
				return fmt.Errorf("refusing to overwrite existing file %q (use --force to override)", e.file.Path)
			}
			return fmt.Errorf("write %q: %w", e.file.Path, err)
		}
		if _, err := out.Write(rendered[e.clean]); err != nil {
			//nolint:errcheck // best-effort close after write error.
			_ = out.Close()
			return fmt.Errorf("write %q: %w", e.file.Path, err)
		}
		if err := out.Close(); err != nil {
			return fmt.Errorf("close %q: %w", e.file.Path, err)
		}
	}

	return nil
}

// renderFileSafely invokes file.Render while recovering from panics so that
// generator bugs on unexpected IR shapes are surfaced as render errors instead
// of crashing the caller.
func renderFileSafely(file File, w io.Writer) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("renderer panic: %v", rec)
		}
	}()
	return file.Render(w)
}

// templateKey identifies a cached parsed template.
type templateKey struct {
	path string
	text string
}

// parsedTemplateCache stores *template.Template values keyed by the path and
// template text. It avoids re-parsing the same template string on repeated
// Generate calls. The cache is unbounded and intended for bounded template
// sets; use ClearTemplateCache to reclaim memory when a batch of generation is
// complete.
var parsedTemplateCache sync.Map

// ClearTemplateCache removes all entries from the parsed template cache. It is
// safe for concurrent use.
func ClearTemplateCache() {
	// sync.Map.Clear is available starting with Go 1.23 and clears the map in
	// O(1) time (L-42 removed the stale historical note about the prior O(n)
	// Range+Delete implementation).
	parsedTemplateCache.Clear()
}

// Template returns a File that is rendered using Go's text/template package.
// The template string is parsed once per unique (path, text) pair and cached,
// so repeated Generate calls with the same template only pay the parse cost
// once. The supplied data is evaluated fresh on each render.
//
// The template cache is package-level and unbounded; use ClearTemplateCache to
// reclaim memory after a batch of generation. Concurrent calls with the same
// (path, text) may parse the template more than once, but the result is still
// deterministic.
func Template(path, text string, data any) File {
	return File{
		Path: path,
		Render: func(w io.Writer) error {
			key := templateKey{path: path, text: text}
			tmpl, ok := parsedTemplateCache.Load(key)
			if !ok {
				parsed, err := template.New(path).Parse(text)
				if err != nil {
					return err
				}
				tmpl, _ = parsedTemplateCache.LoadOrStore(key, parsed)
			}
			t, ok := tmpl.(*template.Template)
			if !ok {
				return errors.New("internal: template cache value has wrong type")
			}
			return t.Execute(w, data)
		},
	}
}

// ErrorFile returns a File whose Render always returns err. It is useful when
// an earlier validation step detects a problem and the caller still needs to
// return a File (for example, to preserve an existing API contract).
func ErrorFile(path string, err error) File {
	return File{
		Path: path,
		Render: func(_ io.Writer) error {
			return err
		},
	}
}

// GoCodeAST returns a File that is rendered from a standard-library go/ast.File.
func GoCodeAST(path string, file *ast.File) File {
	return File{
		Path: path,
		Render: func(w io.Writer) error {
			if file == nil {
				return errors.New("GoCodeAST: nil ast.File")
			}
			var buf bytes.Buffer
			if err := format.Node(&buf, token.NewFileSet(), file); err != nil {
				return fmt.Errorf("format ast: %w", err)
			}
			_, err := w.Write(buf.Bytes())
			return err
		},
	}
}

// safeRelPath validates that path is a relative path that does not escape the
// output directory via ".." segments or an absolute prefix. It does NOT defend
// against a pre-existing symlink inside the output directory that points
// outside it: os.OpenFile follows symlinks, so such a file would be written
// through. Exploiting this requires prior write access to the output directory,
// so it is a low-risk caveat rather than a hard guarantee (L-40).
func safeRelPath(path string) (string, error) {
	rel := filepath.FromSlash(path)
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute output path %q is not allowed", path)
	}

	clean := filepath.Clean(rel)
	if clean == "" || clean == "." {
		return "", fmt.Errorf("empty output path %q is not allowed", path)
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("output path %q escapes output directory", path)
	}

	return clean, nil
}
