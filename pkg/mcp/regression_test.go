package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// regressionSpec is a minimal CRUD spec used by the regression tests. It has a
// collection POST (createPet) and an instance GET/DELETE (getPet/deletePet), so
// a full resource is inferred and the inspect/generate results are non-empty.
const regressionSpec = `openapi: 3.0.0
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

// callTool drives a tool call through the in-memory client session and returns
// the raw text content.
func callTool(t *testing.T, client *sdkmcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	ctx := context.Background()
	res, err := client.CallTool(ctx, &sdkmcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	if len(res.Content) == 0 {
		t.Fatalf("CallTool %s: no content", name)
	}
	text, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("CallTool %s: content not text: %T", name, res.Content[0])
	}
	return text.Text
}

// objectSpec is the regressionSpec parsed into a map, the shape an LLM client
// commonly sends for the spec argument instead of a serialized string.
func objectSpec() map[string]any {
	return map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "Pet Store", "version": "1.0.0"},
		"paths": map[string]any{
			"/pets": map[string]any{
				"post": map[string]any{"operationId": "createPet", "responses": map[string]any{"201": map[string]any{"description": "created"}}},
			},
			"/pets/{id}": map[string]any{
				"get":    map[string]any{"operationId": "getPet", "responses": map[string]any{"200": map[string]any{"description": "ok"}}},
				"delete": map[string]any{"operationId": "deletePet", "responses": map[string]any{"200": map[string]any{"description": "ok"}}},
			},
		},
	}
}

// TestInspect_ObjectSpec verifies eidos/inspect accepts a parsed object spec and
// reports the inferred resources. Previously the input schema typed spec as a
// string, so the MCP SDK rejected an object with "has type object, want string"
// and the tool returned an empty resources array (M-83).
func TestInspect_ObjectSpec(t *testing.T) {
	client := connectTestClient(t)
	raw := callTool(t, client, "eidos/inspect", map[string]any{
		"spec": objectSpec(),
	})
	t.Logf("inspect(object spec) => %s", raw)
	var out InspectResult
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Resources) == 0 {
		t.Errorf("BUG: resources array empty for object spec: %s", raw)
	}
	if !out.Valid {
		t.Errorf("BUG: object spec reported invalid: %s", raw)
	}
}

// TestGenerate_ObjectSpec verifies eidos/generate accepts a parsed object spec
// in dry-run mode (M-83).
func TestGenerate_ObjectSpec(t *testing.T) {
	client := connectTestClient(t)
	raw := callTool(t, client, "eidos/generate", map[string]any{
		"spec":   objectSpec(),
		"dryRun": true,
	})
	t.Logf("generate(object spec, dryRun) => %s", raw)
	var out GenerateResult
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Resources) == 0 {
		t.Errorf("BUG: resources empty for object spec: %s", raw)
	}
	if out.FileCount == 0 {
		t.Errorf("BUG: no files planned for object spec: %s", raw)
	}
}

// TestValidateSchemas_ObjectSpec verifies eidos/validate-schemas accepts a
// parsed object spec (M-83).
func TestValidateSchemas_ObjectSpec(t *testing.T) {
	client := connectTestClient(t)
	raw := callTool(t, client, "eidos/validate-schemas", map[string]any{
		"spec": objectSpec(),
	})
	t.Logf("validate-schemas(object spec) => %s", raw)
	var out ValidateSchemasResult
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.Valid {
		t.Errorf("BUG: object spec reported invalid: %s", raw)
	}
}

// TestOverridePreview_ObjectSpec verifies eidos/override-preview accepts a
// parsed object spec alongside a config (M-83).
func TestOverridePreview_ObjectSpec(t *testing.T) {
	client := connectTestClient(t)
	raw := callTool(t, client, "eidos/override-preview", map[string]any{
		"spec":   objectSpec(),
		"config": "provider:\n  name: petapi\n  version: \"0.1.0\"\n",
	})
	t.Logf("override-preview(object spec) => %s", raw)
	var out OverridePreviewResult
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Resources) == 0 {
		t.Errorf("BUG: resources empty for object spec: %s", raw)
	}
}

// TestGenerate_WriteModePreservesGeneratorYAML verifies that an MCP generate
// write (which deliberately does not emit generator.yaml, IncludeConfig=false)
// never deletes a generator.yaml a previous run recorded in the manifest. The
// config is the caller's source-of-truth input, not a provider deliverable, so
// stale-file cleanup must leave it alone (M-82).
func TestGenerate_WriteModePreservesGeneratorYAML(t *testing.T) {
	client := connectTestClient(t)
	dir := t.TempDir()
	// Simulate a previous CLI write-mode run that emitted generator.yaml and
	// recorded it in the bookkeeping manifest.
	cfg := "provider:\n  name: petapi\n  version: \"0.1.0\"\n"
	if err := os.WriteFile(filepath.Join(dir, "generator.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{"mode": "full", "generated": []string{"generator.yaml"}}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".eidos-generated.json"), manifestData, 0o600); err != nil {
		t.Fatal(err)
	}

	raw := callTool(t, client, "eidos/generate", map[string]any{
		"spec":   regressionSpec,
		"config": cfg,
		"output": dir,
		"force":  true,
	})
	t.Logf("generate(write, force) => %s", raw)
	var out GenerateResult
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "generator.yaml")); err != nil {
		// The file is expected to be gone (that is the bug under test); read
		// whatever remains only to enrich the failure message.
		data, readErr := os.ReadFile(filepath.Join(dir, "generator.yaml"))
		content := ""
		if readErr == nil {
			content = string(data)
		}
		t.Errorf("BUG: generator.yaml deleted by MCP write (stale cleanup): %v; remaining content: %q", err, content)
	}
}

// TestGenerate_DryRunWritesNothing verifies that eidos/generate with dry_run
// (snake_case) plans files without writing anything to the output directory.
func TestGenerate_DryRunWritesNothing(t *testing.T) {
	client := connectTestClient(t)
	dir := t.TempDir()
	raw := callTool(t, client, "eidos/generate", map[string]any{
		"spec":    regressionSpec,
		"dry_run": true,
		"output":  dir,
	})
	t.Logf("generate(dry_run) => %s", raw)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) > 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("BUG: dry-run wrote files to output dir: %v", names)
	}
}

// TestGenerate_DryRunCamelCaseWritesNothing verifies the camelCase "dryRun"
// alias (M-75) also plans without writing.
func TestGenerate_DryRunCamelCaseWritesNothing(t *testing.T) {
	client := connectTestClient(t)
	dir := t.TempDir()
	raw := callTool(t, client, "eidos/generate", map[string]any{
		"spec":   regressionSpec,
		"dryRun": true,
		"output": dir,
	})
	t.Logf("generate(dryRun camelCase) => %s", raw)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) > 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("BUG: camelCase dryRun wrote files: %v", names)
	}
}

// TestOverridePreview_FileURLConfig verifies eidos/override-preview resolves a
// file:// config URL (M-76).
func TestOverridePreview_FileURLConfig(t *testing.T) {
	client := connectTestClient(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "generator.yaml")
	cfg := "provider:\n  name: petapi\n  version: \"0.1.0\"\nresource_overrides:\n  - operation: createPet\n    resource_name: animal\n"
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	raw := callTool(t, client, "eidos/override-preview", map[string]any{
		"spec":   regressionSpec,
		"config": "file://" + cfgPath,
	})
	t.Logf("override-preview(file://) => %s", raw)
	var out OverridePreviewResult
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Resources) == 0 {
		t.Errorf("BUG: override-preview resources empty for file:// config: %s", raw)
	}
}

// TestGenerate_DynamicReleaseWorkflow verifies that an MCP generate honors
// generation.dynamic_release.enabled and threads the configured spec.path into
// the emitted regenerate-and-release workflow instead of the hardcoded
// "spec.yaml" default (M-84).
func TestGenerate_DynamicReleaseWorkflow(t *testing.T) {
	client := connectTestClient(t)
	dir := t.TempDir()
	remoteSpec := "https://api.example.com/v2/documentation/json"
	cfg := "provider:\n  name: petapi\n  version: \"0.1.0\"\nspec:\n  path: " + remoteSpec + "\ngeneration:\n  dynamic_release:\n    enabled: true\n"
	raw := callTool(t, client, "eidos/generate", map[string]any{
		"spec":   regressionSpec,
		"config": cfg,
		"output": dir,
		"force":  true,
	})
	t.Logf("generate(dynamic release) => %s", raw)
	var out GenerateResult
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	workflowPath := filepath.Join(dir, ".github", "workflows", "regenerate-and-release.yml")
	content, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("BUG: regenerate-and-release.yml not written: %v; files: %v", err, out.Files)
	}
	if !strings.Contains(string(content), "eidos generate --spec "+remoteSpec+" --skip-build --output .") {
		t.Errorf("BUG: workflow does not use configured remote spec %q:\n%s", remoteSpec, string(content))
	}
}
