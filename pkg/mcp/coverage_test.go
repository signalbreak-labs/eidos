package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/signalbreak-labs/eidos/pkg/generator"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

const petStoreSpec = `openapi: "3.0.0"
info:
  title: Pet Store
  version: 1.0.0
paths:
  /pets:
    post:
      operationId: createPet
      responses:
        "201": {description: created}
  /pets/{id}:
    get:
      operationId: getPet
      responses:
        "200": {description: ok}
    delete:
      operationId: deletePet
      responses:
        "200": {description: ok}
`

// TestHandleGenerate_DryRun asserts the generate tool runs the pipeline and
// reports a wired pet resource without writing any files.
func TestHandleGenerate_DryRun(t *testing.T) {
	res, out, err := HandleGenerate(context.Background(), nil, GenerateArgs{Spec: petStoreSpec})
	if err != nil {
		t.Fatalf("HandleGenerate error: %v", err)
	}
	if res == nil || !out.Valid {
		t.Fatalf("expected valid generate result, got %+v", out)
	}
	if out.FileCount != 0 || out.OutputDir != "" {
		t.Errorf("dry run should not write files, got file_count=%d output=%q", out.FileCount, out.OutputDir)
	}
	wired := false
	for _, r := range out.Resources {
		if r.Name == "pet" && r.Wired {
			wired = true
		}
	}
	if !wired {
		t.Errorf("expected wired pet resource, got %+v", out.Resources)
	}
}

// TestHandleGenerate_WriteMode asserts a non-empty output writes provider files
// and reports the file count and directory.
func TestHandleGenerate_WriteMode(t *testing.T) {
	outDir := t.TempDir()
	res, out, err := HandleGenerate(context.Background(), nil, GenerateArgs{Spec: petStoreSpec, Output: outDir})
	if err != nil {
		t.Fatalf("HandleGenerate error: %v", err)
	}
	if res == nil || !out.Valid {
		t.Fatalf("expected valid generate result, got %+v", out)
	}
	if out.FileCount == 0 {
		t.Errorf("expected provider files written, got file_count=0")
	}
	if out.OutputDir != outDir {
		t.Errorf("output dir = %q, want %q", out.OutputDir, outDir)
	}
}

// TestHandleGenerate_InvalidSpec asserts an invalid spec reports valid=false and
// never triggers a write.
func TestHandleGenerate_InvalidSpec(t *testing.T) {
	outDir := t.TempDir()
	res, out, err := HandleGenerate(context.Background(), nil, GenerateArgs{Spec: "not a spec: [", Output: outDir})
	if err != nil {
		t.Fatalf("HandleGenerate error: %v", err)
	}
	if res == nil || out.Valid {
		t.Fatalf("expected invalid result, got %+v", out)
	}
	if out.FileCount != 0 {
		t.Errorf("invalid spec must not write files, got file_count=%d", out.FileCount)
	}
}

// TestHandleGenerate_DryRunStaleFilesNeverNil verifies that a dry-run with an
// output dir (the success path) leaves StaleFiles as a non-nil slice so it
// serializes as [] rather than null. A null stale_files is rejected by the SDK's
// structured-output validation, which made a successful dry-run return an MCP
// error (M-80).
func TestHandleGenerate_DryRunStaleFilesNeverNil(t *testing.T) {
	outDir := t.TempDir()
	_, out, err := HandleGenerate(context.Background(), nil, GenerateArgs{Spec: petStoreSpec, Output: outDir, DryRun: true})
	if err != nil {
		t.Fatalf("HandleGenerate error: %v", err)
	}
	if !out.Valid {
		t.Fatalf("expected valid result, got diagnostics: %+v", out.Diagnostics)
	}
	if out.StaleFiles == nil {
		t.Fatalf("StaleFiles must be non-nil so it serializes as [] not null (M-80)")
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if bytes.Contains(b, []byte(`"stale_files":null`)) {
		t.Errorf("stale_files serialized as null (M-80): %s", b)
	}
}

// TestHandleGenerate_EmitsDocsAndCoverageTests verifies MCP generate emits the
// same complete provider the CLI does (docs, Go coverage tests) rather than a
// bare build-only set, and that it does NOT write a canonical generator.yaml
// into the output dir (clobber safety; the MCP caller owns the config) (M-81).
func TestHandleGenerate_EmitsDocsAndCoverageTests(t *testing.T) {
	outDir := t.TempDir()
	_, out, err := HandleGenerate(context.Background(), nil, GenerateArgs{Spec: petStoreSpec, Output: outDir})
	if err != nil {
		t.Fatalf("HandleGenerate error: %v", err)
	}
	if !out.Valid {
		t.Fatalf("expected valid result, got diagnostics: %+v", out.Diagnostics)
	}
	if _, err := os.Stat(filepath.Join(outDir, "docs", "index.md")); err != nil {
		t.Errorf("MCP generate should emit docs/index.md (M-81): %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(outDir, "internal", "provider", "*_test.go"))
	if err != nil {
		t.Fatalf("glob test files: %v", err)
	}
	if len(matches) == 0 {
		t.Errorf("MCP generate should emit Go coverage tests under internal/provider (M-81)")
	}
	if _, err := os.Stat(filepath.Join(outDir, "generator.yaml")); err == nil {
		t.Errorf("MCP generate should not write a canonical generator.yaml into the output dir (M-81)")
	}
}

// TestSummarizeEntities_WiredFlags drives every summarizer through both wired
// and scaffolded (unwired) entities.
func TestSummarizeEntities_WiredFlags(t *testing.T) {
	ds := summarizeDataSources([]ir.DataSourceIR{
		{Name: "stats", TypeName: "stats", ReadMapping: ir.OperationMappingIR{Method: "GET", PathTemplate: "/stats"}},
		{Name: "stub", TypeName: "stub"},
	})
	if len(ds) != 2 || !ds[0].Wired || ds[1].Wired {
		t.Errorf("data sources = %+v", ds)
	}

	actions := summarizeActions([]ir.ActionIR{
		{Name: "reboot", TypeName: "reboot", InvokeMapping: ir.OperationMappingIR{Method: "POST", PathTemplate: "/reboot"}},
		{Name: "stub", TypeName: "stub"},
	})
	if len(actions) != 2 || !actions[0].Wired || actions[1].Wired {
		t.Errorf("actions = %+v", actions)
	}

	ephs := summarizeEphemerals([]ir.EphemeralResourceIR{
		{Name: "session", TypeName: "session", OpenMapping: ir.OperationMappingIR{Method: "POST", PathTemplate: "/sessions"}},
		{Name: "stub", TypeName: "stub"},
	})
	if len(ephs) != 2 || !ephs[0].Wired || ephs[1].Wired {
		t.Errorf("ephemerals = %+v", ephs)
	}

	lists := summarizeLists([]ir.ListResourceIR{
		{Name: "pets", TypeName: "pets", ListMapping: ir.OperationMappingIR{Method: "GET", PathTemplate: "/pets"}},
		{Name: "stub", TypeName: "stub"},
	})
	if len(lists) != 2 || !lists[0].Wired || lists[1].Wired {
		t.Errorf("lists = %+v", lists)
	}

	fns := summarizeFunctions([]ir.FunctionIR{
		{Name: "now", TypeName: "now", SourceOperation: "getNow"},
		{Name: "stub", TypeName: "stub"},
	})
	if len(fns) != 2 || !fns[0].Wired || fns[1].Wired {
		t.Errorf("functions = %+v", fns)
	}
}

// TestSchemaIssues_AttributeShapes drives every attribute-level issue branch of
// the framework-validity walk: root attribute checks, object recursion, the
// nested-dynamic collection branch, and union variant recursion.
func TestSchemaIssues_AttributeShapes(t *testing.T) {
	obj := ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{
		{Name: "bad-name", Schema: ir.SchemaIR{Type: ir.TypeString}}, // invalid identifier
		{Name: "count", Schema: ir.SchemaIR{Type: ir.TypeString}},    // reserved root name
		{Name: "both", Required: true, Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		{Name: "ok", Schema: ir.SchemaIR{Type: ir.TypeString}}, // clean
		{Name: "list", Schema: ir.SchemaIR{Collection: &ir.CollectionType{ // nested dynamic via collection
			Kind: ir.List,
			ElementType: ir.SchemaIR{Attributes: []ir.AttributeIR{
				{Name: "deep", Schema: ir.SchemaIR{Type: ir.TypeDynamic}},
			}},
		}}},
		{Name: "union", Schema: ir.SchemaIR{Union: &ir.UnionType{Variants: []ir.SchemaIR{ // union variant recursion
			{Attributes: []ir.AttributeIR{{Name: "variant-field", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
		}}}},
	}}
	issues := schemaIssues("resource pet", obj)
	kinds := map[string]bool{}
	for _, i := range issues {
		kinds[i.Kind] = true
	}
	for _, want := range []string{"invalid-attribute-name", "reserved-root-name", "computed-and-required", "nested-dynamic"} {
		if !kinds[want] {
			t.Errorf("expected issue kind %q, got %+v", want, kinds)
		}
	}
}

// TestCollectionSchemaIssues covers the dynamic-element and nested-collection
// branches plus the clean pass.
func TestCollectionSchemaIssues(t *testing.T) {
	dyn := collectionSchemaIssues("resource pet", ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Type: ir.TypeDynamic}}}, "items")
	if len(dyn) != 1 || dyn[0].Kind != "dynamic-element-collection" {
		t.Errorf("dynamic element = %+v", dyn)
	}
	nested := collectionSchemaIssues("resource pet", ir.SchemaIR{Collection: &ir.CollectionType{
		Kind: ir.List, ElementType: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.Set}},
	}}, "objs")
	if len(nested) != 1 || nested[0].Kind != "nested-collection" {
		t.Errorf("nested collection = %+v", nested)
	}
	clean := collectionSchemaIssues("resource pet", ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Type: ir.TypeString}}}, "tags")
	if len(clean) != 0 {
		t.Errorf("clean collection = %+v, want no issues", clean)
	}
}

// TestInspectErrorResult asserts the per-tool error-result constructor produces
// a body that validates against eidos/inspect's OutputSchema: valid=false with
// an error diagnostic and every required array field a non-nil empty slice. The
// SDK validates the structured return value against the OutputSchema, so a
// zero-value InspectResult{} (nil slices → JSON null) would be rejected with
// "has type null, want array"; this is the regression guard for that path.
func TestInspectErrorResult(t *testing.T) {
	out := inspectErrorResult(errors.New("boom"))
	res, err := marshalToolResult(out)
	if err != nil {
		t.Fatalf("marshalToolResult: %v", err)
	}
	body := toolBody(t, res)
	assertOutputValidates(t, InspectTool(), body)
	assertArrayFieldsNotNull(t, body,
		[]string{"diagnostics", "resources", "data_sources", "actions", "ephemeral_resources", "list_resources", "functions"})
	if !strings.Contains(string(body), "boom") || !strings.Contains(string(body), `"valid":false`) {
		t.Errorf("error result body = %q, want error detail + valid=false", body)
	}
}

// TestRecoverHandler_PanicPath asserts recoverHandler swallows a panic in a
// deferred call and sets the named returns to a valid error result, so the
// handler never propagates and the SDK receives schema-conformant output
// instead of a zero-value struct whose nil arrays it would reject.
func TestRecoverHandler_PanicPath(t *testing.T) {
	var (
		res *sdkmcp.CallToolResult
		out InspectResult
	)
	func() {
		defer recoverHandler("eidos/test", inspectErrorResult, &res, &out)
		panic("kaboom")
	}()
	if res == nil {
		t.Fatal("expected recoverHandler to set a non-nil result")
	}
	if out.Valid {
		t.Errorf("expected Valid=false after panic, got %+v", out)
	}
	body := toolBody(t, res)
	assertOutputValidates(t, InspectTool(), body)
	if !strings.Contains(string(body), "panic in eidos/test handler") {
		t.Errorf("expected panic summary in body, got %s", body)
	}
}

// TestWriteProvider_WritesFiles asserts writeProvider emits files into dir.
func TestWriteProvider_WritesFiles(t *testing.T) {
	pir := &ir.ProviderIR{
		Name:    "petstore",
		Version: "1.0.0",
		Resources: []ir.ResourceIR{{
			Name:     "pet",
			TypeName: "pet",
			Schema: ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{
				{Name: "id", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			}},
			CRUDMapping: ir.CRUDMappingIR{
				Create: ir.OperationMappingIR{Method: "POST", PathTemplate: "/pets"},
				Read:   ir.OperationMappingIR{Method: "GET", PathTemplate: "/pets/{id}"},
				Delete: ir.OperationMappingIR{Method: "DELETE", PathTemplate: "/pets/{id}"},
			},
		}},
	}
	entries, err := writeProvider(t.TempDir(), pir, generator.DefaultCollectOptions(), true, nil)
	if err != nil {
		t.Fatalf("writeProvider error: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected provider files to be planned")
	}
}

// TestHandleInspect_Counts asserts the explicit counts block matches the
// resource/data-source/action slices and the wired/scaffolded split. The pet
// store fixture yields one wired managed resource and nothing else.
func TestHandleInspect_Counts(t *testing.T) {
	_, out, err := HandleInspect(context.Background(), nil, InspectArgs{Spec: petStoreSpec})
	if err != nil {
		t.Fatalf("HandleInspect error: %v", err)
	}
	if !out.Valid {
		t.Fatalf("expected valid inspect result, got diagnostics: %+v", out.Diagnostics)
	}
	if out.Counts.Resources != len(out.Resources) {
		t.Errorf("counts.resources=%d != len(resources)=%d", out.Counts.Resources, len(out.Resources))
	}
	if out.Counts.DataSources != len(out.DataSources) {
		t.Errorf("counts.data_sources=%d != len(data_sources)=%d", out.Counts.DataSources, len(out.DataSources))
	}
	if out.Counts.Actions != len(out.Actions) {
		t.Errorf("counts.actions=%d != len(actions)=%d", out.Counts.Actions, len(out.Actions))
	}
	if out.Counts.WiredResources+out.Counts.ScaffoldedResources != out.Counts.Resources {
		t.Errorf("wired+scaffolded=%d != resources=%d", out.Counts.WiredResources+out.Counts.ScaffoldedResources, out.Counts.Resources)
	}
	if out.Counts.Resources != 1 || out.Counts.WiredResources != 1 {
		t.Errorf("expected 1 wired resource, got counts=%+v (resources=%+v)", out.Counts, out.Resources)
	}
}

// TestHandleGenerate_DryRunFlagFileListAndStale asserts dry_run returns the
// planned file list without writing, flags pre-existing planned files as
// would-overwrite, and lists unrelated files in --output as stale.
func TestHandleGenerate_DryRunFlagFileListAndStale(t *testing.T) {
	outDir := t.TempDir()

	// Empty output dir: planned files listed, none overwrite, none stale.
	_, out, err := HandleGenerate(context.Background(), nil, GenerateArgs{Spec: petStoreSpec, DryRun: true, Output: outDir})
	if err != nil {
		t.Fatalf("HandleGenerate error: %v", err)
	}
	if !out.Valid {
		t.Fatalf("expected valid result, got diagnostics: %+v", out.Diagnostics)
	}
	if out.FileCount == 0 || len(out.Files) != out.FileCount {
		t.Fatalf("expected planned files, got count=%d files=%d", out.FileCount, len(out.Files))
	}
	for _, f := range out.Files {
		if f.WouldOverwrite {
			t.Errorf("empty dir: %q should not be a would-overwrite", f.Path)
		}
	}
	if len(out.StaleFiles) != 0 {
		t.Errorf("empty dir: expected no stale files, got %v", out.StaleFiles)
	}

	// Pre-create one planned file (to trigger would-overwrite) and one unrelated
	// file (to surface as stale), then re-run the dry-run.
	plannedPath := filepath.Join(outDir, filepath.FromSlash(out.Files[0].Path))
	if err := os.MkdirAll(filepath.Dir(plannedPath), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(plannedPath, []byte("old"), 0o600); err != nil {
		t.Fatalf("write planned: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "leftover.go"), []byte("old"), 0o600); err != nil {
		t.Fatalf("write leftover: %v", err)
	}

	_, out2, err := HandleGenerate(context.Background(), nil, GenerateArgs{Spec: petStoreSpec, DryRun: true, Output: outDir})
	if err != nil {
		t.Fatalf("HandleGenerate error: %v", err)
	}
	if !out2.Files[0].WouldOverwrite {
		t.Errorf("expected %q to be flagged would-overwrite", out2.Files[0].Path)
	}
	found := false
	for _, s := range out2.StaleFiles {
		if s == "leftover.go" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected leftover.go in stale files, got %v", out2.StaleFiles)
	}
	// Dry-run must not have written anything: the pre-created planned file is
	// unchanged.
	b, err := os.ReadFile(plannedPath)
	if err != nil || string(b) != "old" {
		t.Errorf("dry-run overwrote %q (got %q)", plannedPath, string(b))
	}
}

// TestHandleGenerate_DryRunFlagNoOutput asserts dry_run without output still
// returns the planned file list (no overwrite/stale analysis).
func TestHandleGenerate_DryRunFlagNoOutput(t *testing.T) {
	_, out, err := HandleGenerate(context.Background(), nil, GenerateArgs{Spec: petStoreSpec, DryRun: true})
	if err != nil {
		t.Fatalf("HandleGenerate error: %v", err)
	}
	if !out.Valid {
		t.Fatalf("expected valid result, got diagnostics: %+v", out.Diagnostics)
	}
	if out.FileCount == 0 || len(out.Files) == 0 {
		t.Fatalf("expected planned files without output, got count=%d files=%d", out.FileCount, len(out.Files))
	}
	if out.OutputDir != "" {
		t.Errorf("dry_run without output should not set output_dir, got %q", out.OutputDir)
	}
	for _, f := range out.Files {
		if f.WouldOverwrite {
			t.Errorf("no output: %q should not be would-overwrite", f.Path)
		}
	}
}

// skipIfNetworkRestrictedMCP skips the verify test when the local Go environment
// is configured to avoid remote module fetches, since `go mod tidy` for the
// generated provider needs to resolve terraform-plugin-framework et al.
func skipIfNetworkRestrictedMCP(t *testing.T) {
	t.Helper()
	if goflags := os.Getenv("GOFLAGS"); strings.Contains(goflags, "-mod=vendor") {
		t.Skipf("GOFLAGS=%q contains -mod=vendor; skipping network-bound verify test", goflags)
	}
	if proxy := os.Getenv("GOPROXY"); strings.TrimSpace(proxy) == "off" {
		t.Skipf("GOPROXY=%q; skipping network-bound verify test", proxy)
	}
}

// TestHandleGenerate_VerifyCompiles asserts verify=true runs `go build ./...`
// in the output dir after writing and reports verify_ok=true for a spec that
// generates a compilable provider. Skipped when the Go environment is offline.
func TestHandleGenerate_VerifyCompiles(t *testing.T) {
	skipIfNetworkRestrictedMCP(t)
	outDir := t.TempDir()
	_, out, err := HandleGenerate(context.Background(), nil, GenerateArgs{Spec: petStoreSpec, Output: outDir, Verify: true})
	if err != nil {
		t.Fatalf("HandleGenerate error: %v", err)
	}
	if !out.Valid {
		t.Fatalf("expected valid result, got diagnostics: %+v", out.Diagnostics)
	}
	if !out.VerifyOK {
		t.Fatalf("expected verify_ok=true, got false; verify_output=%s diagnostics=%+v", out.VerifyOutput, out.Diagnostics)
	}
}

// TestAddConsumed covers the empty-method/path skip and the key insertion of
// addConsumed.
func TestAddConsumed(t *testing.T) {
	consumed := map[string]bool{}
	addConsumed(consumed, "", "/pets")
	addConsumed(consumed, "GET", "")
	if len(consumed) != 0 {
		t.Errorf("empty method/path must be skipped, got %+v", consumed)
	}
	addConsumed(consumed, "GET", "/pets")
	if !consumed["GET /pets"] {
		t.Errorf("expected GET /pets consumed, got %+v", consumed)
	}
}

// TestTruncateForJSON covers the pass-through and truncation branches.
func TestTruncateForJSON(t *testing.T) {
	if got := truncateForJSON("short", 10); got != "short" {
		t.Errorf("short string = %q, want unchanged", got)
	}
	if got := truncateForJSON("1234567890", 10); got != "1234567890" {
		t.Errorf("exact-length string = %q, want unchanged", got)
	}
	if got := truncateForJSON("this is a long string", 7); got != "this is..." {
		t.Errorf("truncated = %q, want %q", got, "this is...")
	}
}

// TestHandleValidateSchemas_ErrorPaths drives the normalizeSpec, normalizeConfig,
// and mergeConfigIntoSpec error branches of HandleValidateSchemas, each of which
// must produce a valid=false result with diagnostics rather than an error.
func TestHandleValidateSchemas_ErrorPaths(t *testing.T) {
	// normalizeSpec rejects an unsupported spec type.
	_, out, err := HandleValidateSchemas(context.Background(), nil, ValidateSchemasArgs{Spec: 42})
	if err != nil {
		t.Fatalf("HandleValidateSchemas error: %v", err)
	}
	if out.Valid || len(out.Diagnostics) == 0 {
		t.Errorf("expected invalid result for unsupported spec type, got %+v", out)
	}

	// normalizeConfig fails when the config looks like a file ref that cannot load.
	_, out, err = HandleValidateSchemas(context.Background(), nil, ValidateSchemasArgs{Spec: petStoreSpec, Config: "file:///nonexistent-eidos-config.yaml"})
	if err != nil {
		t.Fatalf("HandleValidateSchemas error: %v", err)
	}
	if out.Valid || len(out.Diagnostics) == 0 {
		t.Errorf("expected invalid result for unresolvable config, got %+v", out)
	}

	// mergeConfigIntoSpec fails when the spec is not JSON/YAML and a config is set.
	_, out, err = HandleValidateSchemas(context.Background(), nil, ValidateSchemasArgs{Spec: "not: [valid", Config: "provider:\n  name: x\n"})
	if err != nil {
		t.Fatalf("HandleValidateSchemas error: %v", err)
	}
	if out.Valid || len(out.Diagnostics) == 0 {
		t.Errorf("expected invalid result for unmergeable spec, got %+v", out)
	}
}

// TestHandleOverridePreview_ErrorPaths drives the same three error branches for
// eidos/override-preview.
func TestHandleOverridePreview_ErrorPaths(t *testing.T) {
	// normalizeSpec rejects an unsupported spec type.
	_, out, err := HandleOverridePreview(context.Background(), nil, OverridePreviewArgs{Spec: 42, Config: "provider:\n  name: x\n"})
	if err != nil {
		t.Fatalf("HandleOverridePreview error: %v", err)
	}
	if out.Valid || len(out.Diagnostics) == 0 {
		t.Errorf("expected invalid result for unsupported spec type, got %+v", out)
	}

	// normalizeConfig fails when the config looks like a file ref that cannot load.
	_, out, err = HandleOverridePreview(context.Background(), nil, OverridePreviewArgs{Spec: petStoreSpec, Config: "file:///nonexistent-eidos-config.yaml"})
	if err != nil {
		t.Fatalf("HandleOverridePreview error: %v", err)
	}
	if out.Valid || len(out.Diagnostics) == 0 {
		t.Errorf("expected invalid result for unresolvable config, got %+v", out)
	}

	// mergeConfigIntoSpec fails when the spec is not JSON/YAML and a config is set.
	_, out, err = HandleOverridePreview(context.Background(), nil, OverridePreviewArgs{Spec: "not: [valid", Config: "provider:\n  name: x\n"})
	if err != nil {
		t.Fatalf("HandleOverridePreview error: %v", err)
	}
	if out.Valid || len(out.Diagnostics) == 0 {
		t.Errorf("expected invalid result for unmergeable spec, got %+v", out)
	}
}

// TestSummarizeResources_UpdateAndWired covers the Update branch and the Wired
// computation in summarizeResources.
func TestSummarizeResources_UpdateAndWired(t *testing.T) {
	rs := summarizeResources([]ir.ResourceIR{
		{
			Name: "pet", TypeName: "pet",
			CRUDMapping: ir.CRUDMappingIR{
				Create: ir.OperationMappingIR{Method: "POST", PathTemplate: "/pets"},
				Read:   ir.OperationMappingIR{Method: "GET", PathTemplate: "/pets/{id}"},
				Update: &ir.OperationMappingIR{Method: "PUT", PathTemplate: "/pets/{id}"},
				Delete: ir.OperationMappingIR{Method: "DELETE", PathTemplate: "/pets/{id}"},
			},
		},
		{
			Name: "stub", TypeName: "stub",
			CRUDMapping: ir.CRUDMappingIR{Create: ir.OperationMappingIR{Method: "POST", PathTemplate: "/stubs"}},
		},
	})
	if len(rs) != 2 {
		t.Fatalf("summarizeResources = %d, want 2", len(rs))
	}
	if rs[0].Update != "PUT /pets/{id}" || !rs[0].Wired {
		t.Errorf("wired resource = %+v", rs[0])
	}
	if rs[1].Wired {
		t.Errorf("stub resource must not be wired, got %+v", rs[1])
	}
}
