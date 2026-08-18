package generator_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"hash/fnv"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/api"
	"github.com/signalbreak-labs/eidos/pkg/generator"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// goldenCases maps a snapshot name to a reference OpenAPI spec under
// test/specs. Adding a new spec here automatically creates a regression test
// and, when EIDOS_UPDATE_GOLDEN is set, a checked-in golden snapshot.
var goldenCases = []struct {
	name string
	spec string
}{
	{"mycloud", "../../test/specs/mycloud.yaml"},
	{"mycloud-pets", "../../test/specs/mycloud-pets.yaml"},
	{"mycloud-data", "../../test/specs/mycloud-data.yaml"},
	{"complex-polymorphism", "../../test/specs/complex-polymorphism.yaml"},
	{"callback-example", "../../test/specs/callback-example.yaml"},
	{"link-example", "../../test/specs/link-example.yaml"},
	{"oauth2-security", "../../test/specs/oauth2-security.yaml"},
	{"all-of-nesting", "../../test/specs/all-of-nesting.yaml"},
	{"circular-references", "../../test/specs/circular-references.yaml"},
	{"parameter-types", "../../test/specs/parameter-types.yaml"},
	{"webhooks", "../../test/specs/webhooks.yaml"},
	{"ephemeral-resources", "../../test/specs/ephemeral-resources.yaml"},
	{"provider-functions", "../../test/specs/provider-functions.yaml"},
	{"swagger-formdata", "../../test/specs/swagger-formdata.yaml"},
	{"put-as-create", "../../test/specs/put-as-create.yaml"},
	{"put-as-create-composite", "../../test/specs/put-as-create-composite.yaml"},
}

// scaffoldMarkers are the honest, unconditional "not wired" messages that
// generated CRUD/action/ephemeral/list/data-source/function bodies emit while
// they remain scaffolds. Eidos generates provider scaffolds whose method
// bodies honestly report that they are not yet wired to a remote API endpoint
// so operators know they must be implemented; these markers are the contract
// for that honesty. Resource bodies with a complete CRUD mapping are wired to
// the generated API client instead and no longer carry the marker (see
// wiredMarker). assertHonestScaffolds verifies each construct file carries at
// least one of them, so a regression that silently drops the scaffold (or
// replaces it with a stale TODO/panic) is caught.
var scaffoldMarkers = []string{
	"is not wired to a remote API endpoint.",
}

// forbiddenMarkers are stale or unsafe scaffold patterns that must never
// appear in any generated source file: bare TODO comments, panic("not
// implemented") stubs, and the old "not yet wired" / "not implemented" wording
// that the previous markers listed but the generator no longer emits. The
// previous assertion checked these and passed vacuously because none matched
// the actual emitted text (M-59); this list keeps the negative check but
// gates it on patterns that genuinely should not return.
var forbiddenMarkers = []string{
	"TODO: call the API",
	"TODO: perform",
	"TODO: implement",
	`panic("not implemented")`,
	`panic("TODO")`,
	"Generated provider not yet implemented",
	"not yet wired to a remote API endpoint",
}

// stubMarkerContains reports whether body contains marker, case-insensitively.
func stubMarkerContains(body, marker string) bool {
	return strings.Contains(strings.ToLower(body), strings.ToLower(marker))
}

// goldenEntry is the subset of generator.FileEntry that we snapshot. The
// recorder sorts entries by path, so the resulting JSON is deterministic.
//
// BodyHash is an FNV-1a 64-bit hash of the rendered file body. Recording it
// makes body regressions and body nondeterminism visible in the snapshot
// itself: a generated file whose content silently changes — without a change
// to its path or reason — now fails TestGoldenFiles, and a generation run that
// is nondeterministic across two invocations produces a different hash that the
// determinism check in CI (regenerate + git diff) catches immediately (N-29).
type goldenEntry struct {
	Path     string `json:"path"`
	Reason   string `json:"reason"`
	BodyHash string `json:"body_hash,omitempty"`
}

// bodyHash returns an FNV-1a 64-bit hash of b, formatted as 16 lowercase hex
// digits. It is collision-resistant enough to detect any body regression or
// run-to-run nondeterminism in the golden corpus.
func bodyHash(b []byte) string {
	h := fnv.New64a()
	_, _ = h.Write(b)
	return fmt.Sprintf("%016x", h.Sum64())
}

// TestGoldenFiles runs the parse/normalize/transform/generator pipeline for
// every reference spec and compares the planned file list to the checked-in
// golden snapshot. It also renders the generated source bodies and asserts
// that none contain TODO stubs or unconditional not-implemented errors.
// Set EIDOS_UPDATE_GOLDEN=1 to refresh snapshots.
func TestGoldenFiles(t *testing.T) {
	update := os.Getenv("EIDOS_UPDATE_GOLDEN") != ""

	for _, tc := range goldenCases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := os.ReadFile(tc.spec)
			if err != nil {
				t.Fatalf("read spec %s: %v", tc.spec, err)
			}

			resp := api.Validate(data)
			if !resp.Valid {
				var summaries []string
				for _, d := range resp.Diagnostics {
					summaries = append(summaries, fmt.Sprintf("[%s] %s: %s", d.Severity, d.Summary, d.Detail))
				}
				t.Fatalf("spec %s produced diagnostics:\n%s", tc.spec, summaries)
			}
			if resp.IRPreview == nil {
				t.Fatalf("spec %s produced no IR preview", tc.spec)
			}

			entries, err := generator.Run(resp.IRPreview, generator.Options{
				Mode:           generator.ModeRecord,
				CollectOptions: generator.DefaultCollectOptions(),
			})
			if err != nil {
				t.Fatalf("generator.Run for %s: %v", tc.spec, err)
			}

			// Attach a body hash to every planned entry so a body regression or a
			// run-to-run nondeterministic body is visible in the snapshot (N-29).
			// The hash map is rendered from the same FilesForProviderIR source of
			// truth as write mode, so the hashed bodies are exactly what write
			// mode would emit.
			hashes := renderAllFileHashes(t, resp.IRPreview)
			got := make([]goldenEntry, 0, len(entries))
			for _, e := range entries {
				got = append(got, goldenEntry{Path: e.Path, Reason: e.Reason, BodyHash: hashes[e.Path]})
			}

			rendered := renderSourceFiles(t, resp.IRPreview)
			assertHonestScaffolds(t, rendered)
			assertCorpusWiring(t, tc.name, rendered)
			// Cheap, always-on syntax guard (also under -short): every generated
			// .go body must at least parse and reformat. This catches
			// syntactically broken Go in short mode, where the full compile
			// corpus (TestGoldenFiles_Compile) is skipped (N-29).
			assertGoSyntax(t, rendered)

			goldenPath := filepath.Join("..", "..", "testfixtures", "golden", tc.name+".golden.json")

			if update {
				if err := writeGolden(goldenPath, got); err != nil {
					t.Fatalf("write golden %s: %v", goldenPath, err)
				}
				t.Logf("updated golden file %s", goldenPath)
				return
			}

			want, err := readGolden(goldenPath)
			if err != nil {
				t.Fatalf("read golden %s: %v", goldenPath, err)
			}

			if !goldenEqual(got, want) {
				gotJSON := formatJSON(got)
				wantJSON := formatJSON(want)
				t.Fatalf("golden mismatch for %s\n\ngot:\n%s\n\nwant:\n%s\n\nrun EIDOS_UPDATE_GOLDEN=1 go test ./pkg/generator to refresh snapshots", tc.name, gotJSON, wantJSON)
			}
		})
	}
}

// corpusWiringTargets asserts minimum wired-construct counts for the reference
// specs that PROJECT_DESIGN §23 identified as the corpus-wiring gap: before the
// specs were enriched, the mycloud reference spec (merging the two former
// external-shape reference specs) produced zero wired constructs, so the
// compile corpus never exercised wired bodies at scale. A regression that
// silently unwires a previously-wired construct would otherwise pass
// TestGoldenFiles, because assertHonestScaffolds only requires NON-wired files
// to carry their scaffold marker. The values are floors (a spec that grows more
// wired constructs still passes); raise them deliberately when the corpus is
// intentionally changed.
var corpusWiringTargets = map[string]map[string]int{
	"mycloud":                 {"resource": 7, "data_source": 17, "list": 12},
	"mycloud-pets":            {"resource": 1, "list": 1},
	"ephemeral-resources":     {"ephemeral": 1},
	"swagger-formdata":        {"resource": 2},
	"put-as-create":           {"resource": 1},
	"put-as-create-composite": {"resource": 1},
}

// corpusInferenceTargets asserts minimum inferred-construct counts for specs
// whose constructs stay honestly scaffolded by design (provider-defined
// functions have no remote endpoint to wire), so a regression that stops
// inferring them is still caught.
var corpusInferenceTargets = map[string]map[string]int{
	"provider-functions": {"function": 2},
}

// assertCorpusWiring checks that the enriched reference specs still wire their
// constructs to the generated API client at the expected breadth, and that
// by-design-scaffolded constructs are still inferred. Prefixes are the
// constructFilePrefixes entries (e.g. "resource", "data_source", "list").
func assertCorpusWiring(t *testing.T, specName string, files []sourceFile) {
	t.Helper()
	for _, prefix := range []string{"resource", "data_source", "action", "list", "ephemeral", "function"} {
		want, ok := corpusWiringTargets[specName][prefix]
		if !ok {
			continue
		}
		got := 0
		for _, f := range files {
			if strings.HasPrefix(f.Path, "internal/provider/"+prefix+"_") && isWiredConstruct(f.Path, f.Body) {
				got++
			}
		}
		if got < want {
			t.Errorf("spec %s: expected at least %d wired %s files, got %d (corpus wiring breadth regressed)", specName, want, prefix, got)
		}
	}
	for prefix, want := range corpusInferenceTargets[specName] {
		got := 0
		for _, f := range files {
			if strings.HasPrefix(f.Path, "internal/provider/"+prefix+"_") {
				got++
			}
		}
		if got < want {
			t.Errorf("spec %s: expected at least %d inferred %s files, got %d (construct inference regressed)", specName, want, prefix, got)
		}
	}
}

// sourceFile is a generated file with its rendered body.
type sourceFile struct {
	Path string
	Body string
}

// renderSourceFiles returns the bodies of all generated source files for the
// supplied provider IR. Only files that are part of the generated provider
// implementation are rendered; documentation, examples, tests, and build
// artifacts are intentionally omitted from the TODO-stub check.
func renderSourceFiles(t *testing.T, provider *ir.ProviderIR) []sourceFile {
	t.Helper()

	cfg := buildConfigFor(provider)
	clientImport := cfg.ModulePath + "/internal/client"

	files := sourceFilesFor(provider, clientImport)
	out := make([]sourceFile, 0, len(files))
	for _, f := range files {
		var buf bytes.Buffer
		if err := f.Render(&buf); err != nil {
			t.Fatalf("render %s: %v", f.Path, err)
		}
		out = append(out, sourceFile{Path: f.Path, Body: buf.String()})
	}
	return out
}

// renderAllFileHashes renders the complete generated file set for the provider
// — the same FilesForProviderIR source of truth that write mode emits — and
// returns a map from output path to an FNV-1a body hash (N-29). Any render
// failure is a test failure: the golden test asserts every planned file can
// actually be rendered, which the record-mode {path, reason} list alone cannot.
func renderAllFileHashes(t *testing.T, provider *ir.ProviderIR) map[string]string {
	t.Helper()
	cfg := buildConfigFor(provider)
	files, err := generator.FilesForProviderIR(provider, cfg, generator.DefaultCollectOptions())
	if err != nil {
		t.Fatalf("FilesForProviderIR: %v", err)
	}
	hashes := make(map[string]string, len(files))
	for _, f := range files {
		var buf bytes.Buffer
		if err := f.Render(&buf); err != nil {
			t.Fatalf("render %s for body hash: %v", f.Path, err)
		}
		hashes[f.Path] = bodyHash(buf.Bytes())
	}
	return hashes
}

// assertGoSyntax parses and reformats every generated .go body so syntactically
// broken Go is caught in every test run, including -short mode where the full
// compile corpus is skipped. format.Source parses the file and would error on
// malformed syntax; name resolution is intentionally out of scope (N-29).
func assertGoSyntax(t *testing.T, files []sourceFile) {
	t.Helper()
	for _, f := range files {
		if !strings.HasSuffix(f.Path, ".go") {
			continue
		}
		if _, err := format.Source([]byte(f.Body)); err != nil {
			t.Errorf("generated Go file %q does not parse/reformat: %v", f.Path, err)
		}
	}
}

// buildConfigFor returns a BuildConfig derived from a provider IR for use
// during golden-file rendering. The namespace falls back to the provider name
// because the IR does not carry a registry namespace.
func buildConfigFor(provider *ir.ProviderIR) generator.BuildConfig {
	name := ""
	if provider != nil {
		name = strings.TrimSpace(provider.Name)
	}
	if name == "" {
		name = "generated"
	}
	return generator.BuildConfig{
		ProviderName: name,
		Namespace:    name,
		ModulePath:   fmt.Sprintf("github.com/%s/terraform-provider-%s", name, name),
	}
}

// sourceFilesFor assembles the implementation files that may contain CRUD,
// action, ephemeral, list, data-source, or function method bodies. It mirrors
// the body-bearing portion of the generator's planned file list.
func sourceFilesFor(provider *ir.ProviderIR, clientImport string) []generator.File {
	if provider == nil {
		return nil
	}

	pir := *provider

	pf, err := generator.ProviderFile(pir)
	if err != nil {
		// Wrap the error in an ErrorFile so the render phase reports it cleanly.
		return []generator.File{generator.ErrorFile("internal/provider/provider.go", err)}
	}

	files := make([]generator.File, 0, 10)
	files = append(files, pf)
	files = append(files, generator.ResourceFiles(pir.Resources, clientImport)...)
	files = append(files, generator.DataSourceFiles(pir.DataSources, clientImport)...)
	files = append(files, generator.ActionFiles(pir.Actions, clientImport)...)
	files = append(files, generator.EphemeralFiles(pir.EphemeralResources, clientImport)...)
	files = append(files, generator.ListResourceFiles(pir.ListResources, clientImport)...)
	files = append(files, generator.FunctionFiles(pir.Functions)...)
	files = append(files, generator.ClientFiles(pir)...)
	files = append(files, generator.ValidatorsFile(pir))
	return files
}

// constructFilePrefixes identifies the generated implementation files that
// carry CRUD/action/ephemeral/list/data-source/function method bodies and are
// therefore expected to emit an honest "not wired" scaffold marker.
var constructFilePrefixes = []string{
	"internal/provider/resource_",
	"internal/provider/data_source_",
	"internal/provider/action_",
	"internal/provider/ephemeral_",
	"internal/provider/list_",
	"internal/provider/function_",
}

// isConstructFile reports whether path is a generated construct implementation
// file expected to carry an honest scaffold marker.
func isConstructFile(path string) bool {
	for _, prefix := range constructFilePrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// wiredClientCallRE matches an actual call on the generated client field —
// r.client.NewRequest(ctx, ...), d.client.Read(...), l.client.List(...), etc.
// It keys wired-vs-scaffolded detection on the call idiom rather than a bare
// method-name substring, addressing both brittleness directions of the old
// `client.NewRequest` match (N-34):
//
//   - a comment that merely mentions "NewRequest" does not match, because a
//     real call requires the `.client.<Method>(` shape with the opening paren;
//   - a method rename (e.g. NewRequest → NewRequestWithContext) still counts as
//     wired, because any method call on the client field matches;
//   - the receiver name (r/d/e/l) is unconstrained, so a receiver rename does
//     not flip wired constructs to "missing honest scaffold marker".
//
// Wired construct bodies always make HTTP calls through the client field, so
// the pattern matches exactly the bodies that legitimately drop the scaffold
// marker; scaffolded bodies never reference `.client.<Method>(`.
var wiredClientCallRE = regexp.MustCompile(`\.client\.[A-Z][A-Za-z0-9]*\(`)

// isWiredConstruct reports whether a generated construct implementation file is
// wired to the generated API client, and so legitimately drops the "not wired"
// scaffold marker. Any construct file whose body contains an actual call on the
// client field counts as wired; the path prefix restricts the check to
// construct files (functions are never wired — they have no client receiver).
func isWiredConstruct(path, body string) bool {
	for _, prefix := range []string{
		"internal/provider/resource_",
		"internal/provider/action_",
		"internal/provider/data_source_",
		"internal/provider/ephemeral_",
		"internal/provider/list_",
	} {
		if strings.HasPrefix(path, prefix) {
			return wiredClientCallRE.MatchString(body)
		}
	}
	return false
}

// assertHonestScaffolds verifies the generated construct bodies are honest
// scaffolds: every construct implementation file must carry at least one
// scaffoldMarker (so a regression that drops the "not wired" message is
// caught), and no file may carry a forbidden stale/panic marker. Resource and
// data source files wired to the generated API client are exempt from the
// scaffold-marker requirement because their bodies are real implementations; an
// unwired construct must still carry the marker. The previous assertNoTODOStubs
// checked stale markers that never matched the emitted text and so passed
// vacuously (M-59); this assertion is non-vacuous because every construct file
// is required to match.
func assertHonestScaffolds(t *testing.T, files []sourceFile) {
	t.Helper()

	constructCount := 0
	for _, f := range files {
		if isConstructFile(f.Path) {
			constructCount++
			matched := false
			for _, marker := range scaffoldMarkers {
				if stubMarkerContains(f.Body, marker) {
					matched = true
					break
				}
			}
			wired := isWiredConstruct(f.Path, f.Body)
			if !matched && !wired {
				t.Errorf("generated construct body %q does not contain an honest scaffold marker (one of %v) and is not wired to the API client; the 'not wired' message was dropped or reworded",
					f.Path, scaffoldMarkers)
			}
		}
		for _, marker := range forbiddenMarkers {
			if stubMarkerContains(f.Body, marker) {
				t.Errorf("generated body %q contains forbidden stale/panic marker %q", f.Path, marker)
			}
		}
	}
	// Guard against vacuousness: if no construct files were rendered, the
	// scaffold-presence check above could not have failed, so a regression that
	// stops emitting construct bodies would pass silently. Require at least
	// one construct file to be present for each spec.
	if constructCount == 0 {
		t.Errorf("no construct implementation files were rendered; scaffold honesty check is vacuous")
	}
}

func writeGolden(path string, entries []goldenEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func readGolden(path string) ([]goldenEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var entries []goldenEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func formatJSON(v any) string {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("<marshal error: %v>", err)
	}
	return string(data)
}

func goldenEqual(a, b []goldenEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Path != b[i].Path || a[i].Reason != b[i].Reason || a[i].BodyHash != b[i].BodyHash {
			return false
		}
	}
	return true
}
