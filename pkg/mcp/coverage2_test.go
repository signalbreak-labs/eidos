package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/signalbreak-labs/eidos/pkg/api"
	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/ir"
	"github.com/signalbreak-labs/eidos/pkg/transformer"
)

// ---------------------------------------------------------------------------
// tools.go — HandleInspect / HandleGenerate error and edge paths
// ---------------------------------------------------------------------------

// TestHandleInspect_ConfigErrorPaths drives the normalizeConfig and
// mergeConfigIntoSpec failure branches of HandleInspect, each of which must
// produce a valid=false result with diagnostics rather than a protocol error.
func TestHandleInspect_ConfigErrorPaths(t *testing.T) {
	// normalizeConfig fails when the config looks like a file ref that cannot load.
	_, out, err := HandleInspect(context.Background(), nil, InspectArgs{Spec: petStoreSpec, Config: "file:///nonexistent-eidos-config.yaml"})
	if err != nil {
		t.Fatalf("HandleInspect error: %v", err)
	}
	if out.Valid || len(out.Diagnostics) == 0 {
		t.Errorf("expected invalid result for unresolvable config, got %+v", out)
	}

	// mergeConfigIntoSpec fails when the spec is not JSON/YAML and a config is set.
	_, out, err = HandleInspect(context.Background(), nil, InspectArgs{Spec: "not: [valid", Config: "provider:\n  name: x\n"})
	if err != nil {
		t.Fatalf("HandleInspect error: %v", err)
	}
	if out.Valid || len(out.Diagnostics) == 0 {
		t.Errorf("expected invalid result for unmergeable spec, got %+v", out)
	}
}

// TestGenerateArgs_UnmarshalJSONError drives the json.Unmarshal failure branch
// of GenerateArgs.UnmarshalJSON: malformed JSON is rejected before any field is
// read. (json.Unmarshal validates the input before dispatching to the
// Unmarshaler, so the method must be called directly to reach its error path.)
func TestGenerateArgs_UnmarshalJSONError(t *testing.T) {
	var a GenerateArgs
	if err := a.UnmarshalJSON([]byte("not json")); err == nil {
		t.Fatal("expected an error for malformed JSON")
	}
}

// TestHandleGenerate_ConfigErrorPaths drives the normalizeSpec, normalizeConfig,
// and mergeConfigIntoSpec failure branches of HandleGenerate.
func TestHandleGenerate_ConfigErrorPaths(t *testing.T) {
	// normalizeSpec rejects an unsupported spec type.
	_, out, err := HandleGenerate(context.Background(), nil, GenerateArgs{Spec: 42})
	if err != nil {
		t.Fatalf("HandleGenerate error: %v", err)
	}
	if out.Valid || len(out.Diagnostics) == 0 {
		t.Errorf("expected invalid result for unsupported spec type, got %+v", out)
	}

	// normalizeConfig fails when the config looks like a file ref that cannot load.
	_, out, err = HandleGenerate(context.Background(), nil, GenerateArgs{Spec: petStoreSpec, Config: "file:///nonexistent-eidos-config.yaml"})
	if err != nil {
		t.Fatalf("HandleGenerate error: %v", err)
	}
	if out.Valid || len(out.Diagnostics) == 0 {
		t.Errorf("expected invalid result for unresolvable config, got %+v", out)
	}

	// mergeConfigIntoSpec fails when the spec is not JSON/YAML and a config is set.
	_, out, err = HandleGenerate(context.Background(), nil, GenerateArgs{Spec: "not: [valid", Config: "provider:\n  name: x\n"})
	if err != nil {
		t.Fatalf("HandleGenerate error: %v", err)
	}
	if out.Valid || len(out.Diagnostics) == 0 {
		t.Errorf("expected invalid result for unmergeable spec, got %+v", out)
	}
}

// TestHandleGenerate_DryRunStaleScanError drives the staleFilesInOutput failure
// branch of the dry-run path: an output argument that names a regular file (not
// a directory) makes the stale scan fail, which surfaces as a warning
// diagnostic while the plan itself stays valid.
func TestHandleGenerate_DryRunStaleScanError(t *testing.T) {
	outDir := t.TempDir()
	fp := filepath.Join(outDir, "afile")
	if err := os.WriteFile(fp, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, out, err := HandleGenerate(context.Background(), nil, GenerateArgs{Spec: petStoreSpec, DryRun: true, Output: fp})
	if err != nil {
		t.Fatalf("HandleGenerate error: %v", err)
	}
	if !out.Valid {
		t.Fatalf("expected valid result, got diagnostics: %+v", out.Diagnostics)
	}
	found := false
	for _, d := range out.Diagnostics {
		if d.Severity == "warning" && strings.Contains(d.Summary, "stale files") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a stale-scan warning diagnostic, got %+v", out.Diagnostics)
	}
}

// TestHandleGenerate_WriteModeStaleScanError drives the staleFilesInOutput
// failure branch of the write path: a pre-existing unreadable subdirectory in
// the output dir makes the post-write stale scan fail, surfacing as a warning
// while the written provider stays valid.
func TestHandleGenerate_WriteModeStaleScanError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; chmod 000 does not block reads")
	}
	outDir := t.TempDir()
	locked := filepath.Join(outDir, "locked")
	if err := os.Mkdir(locked, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(locked, "x.go"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(locked, 0o700) //nolint:errcheck // best-effort cleanup

	_, out, err := HandleGenerate(context.Background(), nil, GenerateArgs{Spec: petStoreSpec, Output: outDir})
	if err != nil {
		t.Fatalf("HandleGenerate error: %v", err)
	}
	if !out.Valid {
		t.Fatalf("expected valid result, got diagnostics: %+v", out.Diagnostics)
	}
	if out.FileCount == 0 {
		t.Errorf("expected provider files written, got file_count=0")
	}
	found := false
	for _, d := range out.Diagnostics {
		if d.Severity == "warning" && strings.Contains(d.Summary, "stale files") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a stale-scan warning diagnostic, got %+v", out.Diagnostics)
	}
}

// TestHandleGenerate_VerifyFailureDiagnostic drives the verify-failure branch
// of HandleGenerate: with no `go` binary on PATH, `go mod tidy` fails
// immediately, so verify_ok=false and an error diagnostic are reported even
// though the provider was written.
func TestHandleGenerate_VerifyFailureDiagnostic(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no go binary: runVerify fails fast, offline-safe
	outDir := t.TempDir()
	_, out, err := HandleGenerate(context.Background(), nil, GenerateArgs{Spec: petStoreSpec, Output: outDir, Verify: true})
	if err != nil {
		t.Fatalf("HandleGenerate error: %v", err)
	}
	if !out.Valid {
		t.Fatalf("expected valid result, got diagnostics: %+v", out.Diagnostics)
	}
	if out.VerifyOK {
		t.Errorf("expected verify_ok=false with no go on PATH, got true")
	}
	found := false
	for _, d := range out.Diagnostics {
		if d.Severity == "error" && strings.Contains(d.Summary, "verification failed") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a verification-failed error diagnostic, got %+v", out.Diagnostics)
	}
}

// ---------------------------------------------------------------------------
// tools.go — reportOverrides / decodeSpecMap / yamlToMap / scalar helpers
// ---------------------------------------------------------------------------

// TestReportOverrides_EmptyConfig drives the empty-config early return of
// reportOverrides: no reports and no diagnostics.
func TestReportOverrides_EmptyConfig(t *testing.T) {
	reports, diags := reportOverrides("", &ir.ProviderIR{})
	if len(reports) != 0 || len(diags) != 0 {
		t.Errorf("empty config: reports=%+v diags=%+v, want both empty", reports, diags)
	}
}

// TestReportOverrides_Warnings drives the cfg.Warnings loop of reportOverrides:
// a resource override with both schema and operation set produces a validation
// warning that is surfaced as a diagnostic.
func TestReportOverrides_Warnings(t *testing.T) {
	cfg := "provider:\n  name: petstore\n  version: 1.0.0\nresource_overrides:\n  - operation: createPet\n    schema: pet\n    generate_resource: true\n"
	reports, diags := reportOverrides(cfg, &ir.ProviderIR{})
	if len(reports) != 1 {
		t.Fatalf("expected 1 override report, got %+v", reports)
	}
	if len(diags) != 1 || diags[0].Severity != "warning" {
		t.Errorf("expected 1 warning diagnostic, got %+v", diags)
	}
	if !strings.Contains(diags[0].Summary, "both schema") {
		t.Errorf("warning %q does not mention the schema+operation collision", diags[0].Summary)
	}
}

// TestDecodeSpecMap_TrailingContent drives the trailing-content rejection branch
// of decodeSpecMap: a JSON document followed by a second value is rejected.
func TestDecodeSpecMap_TrailingContent(t *testing.T) {
	if _, err := decodeSpecMap([]byte("{} {}")); err == nil {
		t.Fatal("expected an error for trailing JSON content")
	}
}

// TestYAMLToMap_EmptyDoc drives the empty-document branch of yamlToMap.
func TestYAMLToMap_EmptyDoc(t *testing.T) {
	if _, err := yamlToMap([]byte("")); err == nil {
		t.Fatal("expected an error for an empty YAML document")
	}
}

// TestNodeToAny_AliasDefault drives the default branch of nodeToAny: a YAML
// alias node is not a mapping/sequence/scalar, so it converts to nil.
func TestNodeToAny_AliasDefault(t *testing.T) {
	m, err := yamlToMap([]byte("a: &x 1\nb: *x\n"))
	if err != nil {
		t.Fatalf("yamlToMap: %v", err)
	}
	if v, ok := m["b"]; !ok || v != nil {
		t.Errorf("alias value = %#v, want nil", m["b"])
	}
}

// TestScalarToAny_DecodeError drives the decode-failure branch of scalarToAny: a
// scalar whose tag does not match its value (!!int "abc") falls back to the raw
// text instead of erroring.
func TestScalarToAny_DecodeError(t *testing.T) {
	m, err := yamlToMap([]byte("a: !!int abc\n"))
	if err != nil {
		t.Fatalf("yamlToMap: %v", err)
	}
	if v, ok := m["a"]; !ok || v != "abc" {
		t.Errorf("bad-tag scalar = %#v, want raw text \"abc\"", m["a"])
	}
}

// TestGenerateCollectOptions_SkipToggles drives the skip_tests/skip_docs/
// skip_build branches of generateCollectOptions.
func TestGenerateCollectOptions_SkipToggles(t *testing.T) {
	opts := generateCollectOptions("provider:\n  name: petstore\n  version: 1.0.0\ngeneration:\n  skip_tests: true\n  skip_docs: true\n  skip_build: true\n")
	if opts.IncludeTests {
		t.Error("skip_tests: true must clear IncludeTests")
	}
	if opts.IncludeDocs {
		t.Error("skip_docs: true must clear IncludeDocs")
	}
	if opts.IncludeBuild {
		t.Error("skip_build: true must clear IncludeBuild")
	}
	if opts.IncludeConfig {
		t.Error("MCP generate must never emit the canonical generator.yaml (IncludeConfig=false)")
	}
}

// TestStaleFilesInOutput_ErrorPaths drives the os.Stat error, not-a-directory,
// and WalkDir error branches of staleFilesInOutput.
func TestStaleFilesInOutput_ErrorPaths(t *testing.T) {
	// Nonexistent output dir: empty slice, no error (nothing is stale yet).
	stale, err := staleFilesInOutput(filepath.Join(t.TempDir(), "nope"), nil, false)
	if err != nil || len(stale) != 0 {
		t.Errorf("nonexistent dir: stale=%v err=%v, want empty and nil", stale, err)
	}

	// Output path is a regular file: not-a-directory error.
	dir := t.TempDir()
	fp := filepath.Join(dir, "afile")
	if err := os.WriteFile(fp, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := staleFilesInOutput(fp, nil, false); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("file-as-dir: err=%v, want not-a-directory", err)
	}

	// A path whose parent component is a file: os.Stat fails with a non-IsNotExist
	// error (ENOTDIR), which must be returned, not swallowed.
	if _, err := staleFilesInOutput(filepath.Join(fp, "sub"), nil, false); err == nil {
		t.Error("ENOTDIR path: expected an error")
	}

	// An unreadable subdirectory makes the walk fail.
	if os.Geteuid() == 0 {
		t.Skip("running as root; chmod 000 does not block reads")
	}
	dir2 := t.TempDir()
	locked := filepath.Join(dir2, "locked")
	if err := os.Mkdir(locked, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(locked, "x.go"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(locked, 0o700) //nolint:errcheck // best-effort cleanup
	if _, err := staleFilesInOutput(dir2, nil, false); err == nil {
		t.Error("unreadable subdir: expected a walk error")
	}
}

// TestRunVerify_TidyFailure drives the `go mod tidy` failure branch of runVerify
// with an already-canceled context.
func TestRunVerify_TidyFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ok, out := runVerify(ctx, t.TempDir())
	if ok {
		t.Error("expected verify to fail with a canceled context")
	}
	if !strings.Contains(out, "go mod tidy") {
		t.Errorf("output %q does not mention go mod tidy", out)
	}
}

// TestRunVerify_BuildFailure drives the `go build ./...` failure branch of
// runVerify with a module whose source does not compile.
func TestRunVerify_BuildFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module broken\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main( {\n"), 0o600); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	ok, out := runVerify(context.Background(), dir)
	if ok {
		t.Error("expected verify to fail for a module that does not compile")
	}
	if !strings.Contains(out, "go build ./...") {
		t.Errorf("output %q does not mention go build", out)
	}
}

// TestMarshalToolResult_Error drives the json.Marshal failure branch of
// marshalToolResult with a value that cannot be serialized.
func TestMarshalToolResult_Error(t *testing.T) {
	if _, err := marshalToolResult(map[string]any{"f": func() {}}); err == nil {
		t.Fatal("expected a marshal error for a func value")
	}
}

// TestRecoverHandler_MarshalError drives the marshal-failure branch of
// recoverHandler: when the recovered error result cannot be serialized, res is
// left nil and the panic is still swallowed.
func TestRecoverHandler_MarshalError(t *testing.T) {
	type bad struct{ F func() }
	var res *sdkmcp.CallToolResult
	var out bad
	func() {
		defer recoverHandler("eidos/test", func(error) bad { return bad{F: func() {}} }, &res, &out)
		panic("kaboom")
	}()
	if res != nil {
		t.Errorf("expected res to stay nil when the recovered result cannot marshal, got %+v", res)
	}
}

// ---------------------------------------------------------------------------
// suggest.go — error paths, config warnings, update branch, near-miss branches
// ---------------------------------------------------------------------------

// updateShipStoreSpec has a dropped CRUD group with an update op (PUT on the
// instance), a near-miss delete (scrap), and a non-delete sub-path op (status)
// that findNearMissDelete must skip. A second dropped group (widgets) makes the
// final sort run over two suggestions.
const updateShipStoreSpec = `openapi: 3.0.0
info:
  title: Update Store
  version: 1.0.0
paths:
  /things:
    post:
      operationId: createThing
      responses:
        "201":
          description: created
  /things/{id}:
    get:
      operationId: getThing
      responses:
        "200":
          description: ok
    put:
      operationId: updateThing
      responses:
        "200":
          description: ok
  /things/{id}/scrap:
    post:
      operationId: scrapThing
      responses:
        "200":
          description: scrapped
  /things/{id}/status:
    get:
      operationId: getThingStatus
      responses:
        "200":
          description: ok
  /widgets:
    post:
      operationId: createWidget
      responses:
        "201":
          description: created
  /widgets/{id}:
    get:
      operationId: getWidget
      responses:
        "200":
          description: ok
`

// TestHandleSuggestResources_ConfigErrorPaths drives the normalizeConfig and
// mergeConfigIntoSpec failure branches of HandleSuggestResources.
func TestHandleSuggestResources_ConfigErrorPaths(t *testing.T) {
	_, out, err := HandleSuggestResources(context.Background(), nil, SuggestResourcesArgs{Spec: shipStoreSpec, Config: "file:///nonexistent-eidos-config.yaml"})
	if err != nil {
		t.Fatalf("HandleSuggestResources error: %v", err)
	}
	if out.Valid || len(out.Diagnostics) == 0 {
		t.Errorf("expected invalid result for unresolvable config, got %+v", out)
	}

	_, out, err = HandleSuggestResources(context.Background(), nil, SuggestResourcesArgs{Spec: "not: [valid", Config: "provider:\n  name: x\n"})
	if err != nil {
		t.Fatalf("HandleSuggestResources error: %v", err)
	}
	if out.Valid || len(out.Diagnostics) == 0 {
		t.Errorf("expected invalid result for unmergeable spec, got %+v", out)
	}
}

// TestHandleSuggestResources_ConfigWarnings drives the cfg.Warnings loop and the
// unparseable-config warning branch of HandleSuggestResources.
func TestHandleSuggestResources_ConfigWarnings(t *testing.T) {
	// A config with both schema and operation set produces a validation warning.
	_, out, err := HandleSuggestResources(context.Background(), nil, SuggestResourcesArgs{
		Spec:   shipStoreSpec,
		Config: "provider:\n  name: petstore\n  version: 1.0.0\nresource_overrides:\n  - operation: purchase-ship\n    schema: ship\n    generate_resource: true\n",
	})
	if err != nil {
		t.Fatalf("HandleSuggestResources error: %v", err)
	}
	found := false
	for _, d := range out.Diagnostics {
		if d.Severity == "warning" && strings.Contains(d.Summary, "both schema") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a schema+operation warning, got %+v", out.Diagnostics)
	}

	// A config that is not a file ref but fails to parse surfaces a warning and
	// defaults use_put_as_create to true.
	_, out, err = HandleSuggestResources(context.Background(), nil, SuggestResourcesArgs{Spec: shipStoreSpec, Config: "not: [valid"})
	if err != nil {
		t.Fatalf("HandleSuggestResources error: %v", err)
	}
	found = false
	for _, d := range out.Diagnostics {
		if d.Severity == "warning" && strings.Contains(d.Summary, "could not parse config") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a could-not-parse-config warning, got %+v", out.Diagnostics)
	}
}

// TestHandleSuggestResources_UpdateAndSort drives the update-op branches of
// buildSuggestion/findNearMissDelete/buildOverrideYAML, the non-near-miss skip
// in findNearMissDelete, and the final sort over two suggestions.
func TestHandleSuggestResources_UpdateAndSort(t *testing.T) {
	_, out, err := HandleSuggestResources(context.Background(), nil, SuggestResourcesArgs{Spec: updateShipStoreSpec})
	if err != nil {
		t.Fatalf("HandleSuggestResources error: %v", err)
	}
	if !out.Valid {
		t.Fatalf("expected valid result, got diagnostics: %+v", out.Diagnostics)
	}
	if len(out.Suggestions) != 2 {
		t.Fatalf("expected 2 suggestions, got %+v", out.Suggestions)
	}
	// Deterministic order: things before widgets.
	if out.Suggestions[0].ResourceName != "thing" || out.Suggestions[1].ResourceName != "widget" {
		t.Errorf("suggestions not sorted by name: %+v", out.Suggestions)
	}
	thing := findSuggestion(out.Suggestions, "createThing")
	if thing == nil {
		t.Fatalf("expected a suggestion for createThing, got %+v", out.Suggestions)
	}
	if thing.UpdateOperation != "updateThing" {
		t.Errorf("update_operation = %q, want updateThing", thing.UpdateOperation)
	}
	if thing.DeleteOperation != "scrapThing" || !thing.DeleteViaAction {
		t.Errorf("delete = %q via_action=%v, want scrapThing true", thing.DeleteOperation, thing.DeleteViaAction)
	}
	if thing.Completeness != "create+read+update+delete" {
		t.Errorf("completeness = %q, want create+read+update+delete", thing.Completeness)
	}
	if !strings.Contains(thing.OverrideYAML, "update_operation: updateThing") {
		t.Errorf("override_yaml missing update_operation:\n%s", thing.OverrideYAML)
	}
}

// TestIsNearMissDelete_Branches drives every branch of isNearMissDelete directly.
func TestIsNearMissDelete_Branches(t *testing.T) {
	instance := "/things/{id}"
	// Same path: only a delete-verb operationId qualifies.
	if !isNearMissDelete(transformer.Operation{Path: instance, OperationID: "retireThing"}, instance) {
		t.Error("same-path delete-verb op should qualify")
	}
	// Same path with a non-delete verb does not qualify.
	if isNearMissDelete(transformer.Operation{Path: instance, OperationID: "getThing"}, instance) {
		t.Error("same-path non-delete-verb op must not qualify")
	}
	// Unrelated path: rejected before the tail check.
	if isNearMissDelete(transformer.Operation{Path: "/other", OperationID: "getThing"}, instance) {
		t.Error("unrelated path must not qualify")
	}
	// Multi-segment tail is not a clean verb.
	if isNearMissDelete(transformer.Operation{Path: "/things/{id}/a/b", OperationID: "getThing"}, instance) {
		t.Error("multi-segment tail must not qualify")
	}
	// Parameterized tail is not a clean verb.
	if isNearMissDelete(transformer.Operation{Path: "/things/{id}/{sub}", OperationID: "getThing"}, instance) {
		t.Error("parameterized tail must not qualify")
	}
	// Trailing static verb segment qualifies.
	if !isNearMissDelete(transformer.Operation{Path: "/things/{id}/scrap", OperationID: "getThing"}, instance) {
		t.Error("trailing verb segment should qualify")
	}
}

// TestConsumedFromPreview_Nil drives the nil-preview early return of
// consumedFromPreview.
func TestConsumedFromPreview_Nil(t *testing.T) {
	if got := consumedFromPreview(nil); len(got) != 0 {
		t.Errorf("nil preview: consumed=%v, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// tool.go — normalizeSpec map-marshal and applyOperationFilters nil branches
// ---------------------------------------------------------------------------

// TestNormalizeSpec_MapMarshalError drives the json.Marshal failure branch of
// normalizeSpec's map path: a map containing a non-serializable value with no
// raw wire arguments to fall back on.
func TestNormalizeSpec_MapMarshalError(t *testing.T) {
	if _, err := normalizeSpec(context.Background(), map[string]any{"f": func() {}}, nil); err == nil {
		t.Fatal("expected a marshal error for a func value in the spec map")
	}
}

// TestApplyOperationFilters_NilConfig drives the nil-config early return of
// applyOperationFilters.
func TestApplyOperationFilters_NilConfig(t *testing.T) {
	applyOperationFilters(nil, []string{"skip"}, []string{"include"}) // must not panic
}

// ---------------------------------------------------------------------------
// lookup.go — pathParamSummaries / apiDiags / hasErrorDiags branches
// ---------------------------------------------------------------------------

// TestPathParamSummaries_FilterAndSort drives the non-path skip and the sort of
// pathParamSummaries.
func TestPathParamSummaries_FilterAndSort(t *testing.T) {
	op := transformer.Operation{Parameters: []transformer.Parameter{
		{Name: "q", In: "query", Required: true, Type: "string"},
		{Name: "id", In: "path", Required: true, Type: "string"},
		{Name: "zeta", In: "path", Required: false, Type: "string"},
	}}
	got := pathParamSummaries(op)
	if len(got) != 2 {
		t.Fatalf("pathParamSummaries = %+v, want 2 path params", got)
	}
	if got[0].Name != "id" || got[1].Name != "zeta" {
		t.Errorf("path params not sorted: %+v", got)
	}
}

// TestAPIDiags_NonEmpty drives the loop body of apiDiags with a non-empty
// diagnostic slice.
func TestAPIDiags_NonEmpty(t *testing.T) {
	ds := diagnostics.Diagnostics{
		{Severity: diagnostics.Error, Summary: "boom", Detail: "detail"},
	}
	got := apiDiags(ds)
	if len(got) != 1 {
		t.Fatalf("apiDiags = %+v, want 1", got)
	}
	if got[0].Severity != "error" || got[0].Summary != "boom" || got[0].Detail != "detail" {
		t.Errorf("apiDiags = %+v", got[0])
	}
}

// TestHasErrorDiags_ErrorSeverity drives the error-severity branch of
// hasErrorDiags.
func TestHasErrorDiags_ErrorSeverity(t *testing.T) {
	if !hasErrorDiags([]api.DiagnosticJSON{{Severity: "error", Summary: "x"}}) {
		t.Error("error-severity diagnostic must be detected")
	}
	if hasErrorDiags([]api.DiagnosticJSON{{Severity: "warning", Summary: "x"}}) {
		t.Error("warning-only diagnostics must not count as errors")
	}
}
