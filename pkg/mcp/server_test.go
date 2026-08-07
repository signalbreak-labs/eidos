package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const mycloudSpec = `openapi: 3.0.0
info:
  title: Mycloud
  version: 1.0.0
paths:
  /pets:
    get:
      operationId: listPets
      responses:
        "200":
          description: ok
`

const emptyMycloudSpec = `openapi: 3.0.0
info:
  title: Mycloud
  version: 1.0.0
paths: {}
`

// connectTestClient stands up an in-memory MCP server+client pair and returns a
// connected client session. It collapses the boilerplate server/connect/client
// setup that was duplicated verbatim across each server-level test (L-73). The
// session and its server counterpart are closed automatically on test cleanup.
func connectTestClient(t *testing.T) *sdkmcp.ClientSession {
	t.Helper()
	ctx := context.Background()

	server := NewServer("0.0.0-test")
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("failed to connect server session: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() }) //nolint:errcheck // test cleanup: session close error is non-actionable

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("failed to connect client session: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() }) //nolint:errcheck // test cleanup: session close error is non-actionable

	return clientSession
}

func TestNewServer_AdvertisesGenerateConfig(t *testing.T) {
	ctx := context.Background()
	clientSession := connectTestClient(t)

	listResult, err := clientSession.ListTools(ctx, &sdkmcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("failed to list tools: %v", err)
	}

	var found *sdkmcp.Tool
	for _, tool := range listResult.Tools {
		if tool.Name == "eidos/generate-config" {
			found = tool
			break
		}
	}
	if found == nil {
		t.Fatalf("expected eidos/generate-config tool to be advertised, got: %+v", listResult.Tools)
	}

	schema, ok := found.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("expected InputSchema to be map[string]any, got %T", found.InputSchema)
	}
	if schema["type"] != "object" {
		t.Errorf("expected input schema type 'object', got %v", schema["type"])
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties map, got %T", schema["properties"])
	}
	for _, name := range []string{"spec", "format", "include_comments"} {
		if _, present := properties[name]; !present {
			t.Errorf("expected property %q in input schema", name)
		}
	}
	required, ok := schema["required"].([]any)
	if !ok || len(required) != 1 || required[0] != "spec" {
		t.Errorf("expected required field [spec], got %v", required)
	}

	outputSchema, ok := found.OutputSchema.(map[string]any)
	if !ok {
		t.Fatalf("expected OutputSchema to be map[string]any, got %T", found.OutputSchema)
	}
	if outputSchema["type"] != "object" {
		t.Errorf("expected output schema type 'object', got %v", outputSchema["type"])
	}
	outputProperties, ok := outputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected output properties map, got %T", outputSchema["properties"])
	}
	for _, name := range []string{"config", "diagnostics", "valid"} {
		if _, present := outputProperties[name]; !present {
			t.Errorf("expected output property %q", name)
		}
	}
	outputRequired, ok := outputSchema["required"].([]any)
	if !ok || len(outputRequired) != 3 {
		t.Errorf("expected output required fields [config, diagnostics, valid], got %v", outputRequired)
	}
	for i, name := range []string{"config", "diagnostics", "valid"} {
		if outputRequired[i] != name {
			t.Errorf("expected output required field %q at index %d, got %v", name, i, outputRequired[i])
		}
	}

	// Ensure the advertised output schema is a well-formed JSON Schema that
	// accepts a realistic result payload.
	schemaBytes, err := json.Marshal(outputSchema)
	if err != nil {
		t.Fatalf("failed to marshal output schema: %v", err)
	}
	var parsedSchema jsonschema.Schema
	if err := json.Unmarshal(schemaBytes, &parsedSchema); err != nil {
		t.Fatalf("output schema is not well-formed JSON Schema: %v", err)
	}
	resolved, err := parsedSchema.Resolve(nil)
	if err != nil {
		t.Fatalf("failed to resolve output schema: %v", err)
	}
	if err := resolved.Validate(map[string]any{
		"config":      "provider:\n  name: test\n",
		"diagnostics": []any{},
		"valid":       true,
	}); err != nil {
		t.Errorf("output schema rejected a valid result payload: %v", err)
	}
}

func TestNewServer_CallsGenerateConfig(t *testing.T) {
	ctx := context.Background()
	clientSession := connectTestClient(t)

	callResult, err := clientSession.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "eidos/generate-config",
		Arguments: map[string]any{
			"spec":   mycloudSpec,
			"format": "yaml",
		},
	})
	if err != nil {
		t.Fatalf("failed to call eidos/generate-config: %v", err)
	}

	result := unmarshalToolResult(t, callResult)
	if strings.TrimSpace(result.Config) == "" {
		t.Fatal("expected generated config")
	}
	if !strings.Contains(result.Config, "provider:") {
		t.Errorf("expected config to contain provider section, got:\n%s", result.Config)
	}
	if !strings.Contains(result.Config, "name: mycloud") {
		t.Errorf("expected provider name derived from spec title, got:\n%s", result.Config)
	}
	for _, d := range result.Diagnostics {
		if d.Severity == "error" {
			t.Errorf("unexpected error diagnostic: %+v", d)
		}
	}
}

func TestNewServer_CallsGenerateConfig_ObjectSpec(t *testing.T) {
	ctx := context.Background()
	clientSession := connectTestClient(t)

	spec := map[string]any{
		"openapi": "3.0.0",
		"info": map[string]any{
			"title":   "Mycloud",
			"version": "1.0.0",
		},
		"paths": map[string]any{
			"/pets": map[string]any{
				"get": map[string]any{
					"operationId": "listPets",
					"responses": map[string]any{
						"200": map[string]any{"description": "ok"},
					},
				},
			},
		},
	}

	callResult, err := clientSession.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "eidos/generate-config",
		Arguments: map[string]any{
			"spec": spec,
		},
	})
	if err != nil {
		t.Fatalf("failed to call eidos/generate-config: %v", err)
	}

	result := unmarshalToolResult(t, callResult)
	if strings.TrimSpace(result.Config) == "" {
		t.Fatal("expected generated config")
	}
	if !strings.Contains(result.Config, "name: mycloud") {
		t.Errorf("expected provider name derived from spec title, got:\n%s", result.Config)
	}
}

func TestNewServer_CallsGenerateConfig_JSONFormat(t *testing.T) {
	ctx := context.Background()
	clientSession := connectTestClient(t)

	callResult, err := clientSession.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "eidos/generate-config",
		Arguments: map[string]any{
			"spec":   emptyMycloudSpec,
			"format": "json",
		},
	})
	if err != nil {
		t.Fatalf("failed to call eidos/generate-config: %v", err)
	}

	result := unmarshalToolResult(t, callResult)
	var cfg map[string]any
	if err := json.Unmarshal([]byte(result.Config), &cfg); err != nil {
		t.Fatalf("expected config to be valid JSON: %v\n%s", err, result.Config)
	}
	provider, ok := cfg["provider"].(map[string]any)
	if !ok {
		t.Fatalf("expected provider object in JSON config, got: %v", cfg)
	}
	if provider["name"] != "mycloud" {
		t.Errorf("expected provider name mycloud, got %v", provider["name"])
	}
}

func TestNewServer_CallsGenerateConfig_IncludeComments(t *testing.T) {
	ctx := context.Background()
	clientSession := connectTestClient(t)

	callResult, err := clientSession.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "eidos/generate-config",
		Arguments: map[string]any{
			"spec":             emptyMycloudSpec,
			"format":           "yaml",
			"include_comments": true,
		},
	})
	if err != nil {
		t.Fatalf("failed to call eidos/generate-config: %v", err)
	}

	result := unmarshalToolResult(t, callResult)
	if !strings.HasPrefix(result.Config, "# Generated by eidos/generate-config\n") {
		t.Errorf("expected YAML config to start with header comment, got:\n%s", result.Config)
	}
}

// TestNewServer_JSONFormatWithComments exercises the JSON + include_comments
// path over the MCP transport, asserting the _generator marker is injected as
// the first key without re-sorting the config fields (L-70/L-73: this path had
// no server-level coverage).
func TestNewServer_JSONFormatWithComments(t *testing.T) {
	ctx := context.Background()
	clientSession := connectTestClient(t)

	callResult, err := clientSession.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "eidos/generate-config",
		Arguments: map[string]any{
			"spec":             emptyMycloudSpec,
			"format":           "json",
			"include_comments": true,
		},
	})
	if err != nil {
		t.Fatalf("failed to call eidos/generate-config: %v", err)
	}

	result := unmarshalToolResult(t, callResult)
	var cfg map[string]any
	if err := json.Unmarshal([]byte(result.Config), &cfg); err != nil {
		t.Fatalf("expected config to be valid JSON: %v\n%s", err, result.Config)
	}
	if cfg["_generator"] != "eidos/generate-config" {
		t.Errorf("expected JSON _generator marker, got %v", cfg["_generator"])
	}
	// _generator must be the first serialized key so diffs against the
	// non-comments JSON output stay minimal (L-70). A map iteration cannot
	// recover key order, so assert against the raw serialized text instead.
	if !strings.HasPrefix(result.Config, "{\n  \"_generator\":") {
		t.Errorf("expected _generator to be the first JSON key, got:\n%s", result.Config)
	}
}

// unmarshalToolResult extracts the TextContent payload from a CallToolResult
// and unmarshals it into a GenerateConfigResult. It consolidates the
// parse/assert boilerplate that was duplicated inline across the
// server-level tests (L-73).
func unmarshalToolResult(t *testing.T, callResult *sdkmcp.CallToolResult) GenerateConfigResult {
	t.Helper()
	if len(callResult.Content) == 0 {
		t.Fatal("expected non-empty tool result content")
	}
	text, ok := callResult.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", callResult.Content[0])
	}
	var result GenerateConfigResult
	if err := json.Unmarshal([]byte(text.Text), &result); err != nil {
		t.Fatalf("failed to unmarshal tool result: %v\n%s", err, text.Text)
	}
	return result
}
