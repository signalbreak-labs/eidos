package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// These tests guard against the MCP output-schema violations an LLM hit against
// eidos 0.3.3: when a tool produced no resources / issues / overrides / etc.,
// the corresponding slice fields serialized to JSON null, but each tool's
// OutputSchema requires those fields to be arrays. The SDK validates tool
// output against the OutputSchema and rejected the null with
// "type: ... has type null, want array". The handlers now initialize every
// required array field to a non-nil empty slice (and the shared error path
// emits all required arrays), so the serialized output validates.

// toolBody extracts the TextContent payload from a CallToolResult.
func toolBody(t *testing.T, res *sdkmcp.CallToolResult) []byte {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("expected non-empty tool result content")
	}
	text, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[0])
	}
	return []byte(text.Text)
}

// assertOutputValidates resolves the tool's OutputSchema and validates body
// against it, mirroring the SDK's output validation. It is the direct
// regression guard: before the fix, a null required array failed here.
func assertOutputValidates(t *testing.T, tool *sdkmcp.Tool, body []byte) {
	t.Helper()
	schemaBytes, err := json.Marshal(tool.OutputSchema)
	if err != nil {
		t.Fatalf("marshal %s output schema: %v", tool.Name, err)
	}
	var s jsonschema.Schema
	if err := json.Unmarshal(schemaBytes, &s); err != nil {
		t.Fatalf("unmarshal %s output schema: %v", tool.Name, err)
	}
	resolved, err := s.Resolve(nil)
	if err != nil {
		t.Fatalf("resolve %s output schema: %v", tool.Name, err)
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("unmarshal %s output body: %v\n%s", tool.Name, err, body)
	}
	if err := resolved.Validate(v); err != nil {
		t.Errorf("%s output does not validate against its OutputSchema: %v\nbody: %s", tool.Name, err, body)
	}
}

// assertArrayFieldsNotNull unmarshals body and asserts each named field is a
// JSON array (not null), giving a clearer failure than schema validation alone.
func assertArrayFieldsNotNull(t *testing.T, body []byte, fields []string) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("unmarshal body: %v\n%s", err, body)
	}
	for _, f := range fields {
		v, ok := m[f]
		if !ok {
			t.Errorf("required array field %q is absent in output: %s", f, body)
			continue
		}
		if v == nil {
			t.Errorf("required array field %q serialized as null (want []):\n%s", f, body)
			continue
		}
		if _, ok := v.([]any); !ok {
			t.Errorf("required array field %q is %T, not array:\n%s", f, v, body)
		}
	}
}

// emptySpec is a valid OpenAPI doc with no paths: it builds an IR preview with
// no constructs, exercising the "valid spec, empty result" path where append to
// a nil slice previously left array fields nil.
const emptySpec = "openapi: 3.0.0\ninfo:\n  title: Empty\n  version: 1.0.0\npaths: {}\n"

// invalidSpec is a YAML document that is not an OpenAPI doc: validation fails
// and IRPreview is nil, exercising the path where array fields were never
// assigned and stayed nil.
const invalidSpec = "not: an-openapi-document\n"

func TestHandleInspect_EmptyResultValidatesAgainstOutputSchema(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec string
	}{
		{"empty valid spec", emptySpec},
		{"invalid spec", invalidSpec},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, _, err := HandleInspect(context.Background(), nil, InspectArgs{Spec: tc.spec})
			if err != nil {
				t.Fatalf("HandleInspect error: %v", err)
			}
			body := toolBody(t, res)
			assertOutputValidates(t, InspectTool(), body)
			assertArrayFieldsNotNull(t, body,
				[]string{"diagnostics", "resources", "data_sources", "actions", "ephemeral_resources", "list_resources", "functions"})
		})
	}
}

func TestHandleGenerate_EmptyResultValidatesAgainstOutputSchema(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec string
	}{
		{"empty valid spec", emptySpec},
		{"invalid spec", invalidSpec},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, _, err := HandleGenerate(context.Background(), nil, GenerateArgs{Spec: tc.spec})
			if err != nil {
				t.Fatalf("HandleGenerate error: %v", err)
			}
			body := toolBody(t, res)
			assertOutputValidates(t, GenerateTool(), body)
			assertArrayFieldsNotNull(t, body,
				[]string{"diagnostics", "resources", "data_sources", "actions"})
		})
	}
}

func TestHandleValidateSchemas_EmptyResultValidatesAgainstOutputSchema(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec string
	}{
		{"empty valid spec", emptySpec},
		{"invalid spec", invalidSpec},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, _, err := HandleValidateSchemas(context.Background(), nil, ValidateSchemasArgs{Spec: tc.spec})
			if err != nil {
				t.Fatalf("HandleValidateSchemas error: %v", err)
			}
			body := toolBody(t, res)
			assertOutputValidates(t, ValidateSchemasTool(), body)
			assertArrayFieldsNotNull(t, body, []string{"diagnostics", "issues"})
		})
	}
}

func TestHandleOverridePreview_EmptyResultValidatesAgainstOutputSchema(t *testing.T) {
	cfg := "provider:\n  name: empty\n  version: \"0.1.0\"\n"
	for _, tc := range []struct {
		name string
		spec string
	}{
		{"empty valid spec", emptySpec},
		{"invalid spec", invalidSpec},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, _, err := HandleOverridePreview(context.Background(), nil, OverridePreviewArgs{Spec: tc.spec, Config: cfg})
			if err != nil {
				t.Fatalf("HandleOverridePreview error: %v", err)
			}
			body := toolBody(t, res)
			assertOutputValidates(t, OverridePreviewTool(), body)
			assertArrayFieldsNotNull(t, body, []string{"diagnostics", "resources", "overrides"})
		})
	}
}

// TestHandleInspect_ErrorPathValidatesAgainstOutputSchema ensures the shared
// error path (errorToolResult) emits every required array field as [], so a
// spec-source error does not produce output the SDK rejects. This was the
// other half of the null-array bug: the error result omitted required fields.
func TestHandleInspect_ErrorPathValidatesAgainstOutputSchema(t *testing.T) {
	// An absolute path that does not exist yields a file-read error from
	// normalizeSpec, routing through errorToolResult.
	res, _, err := HandleInspect(context.Background(), nil, InspectArgs{
		Spec: "/definitely/not/a/real/spec/path.yaml",
	})
	if err != nil {
		t.Fatalf("HandleInspect error: %v", err)
	}
	body := toolBody(t, res)
	assertOutputValidates(t, InspectTool(), body)
	assertArrayFieldsNotNull(t, body,
		[]string{"diagnostics", "resources", "data_sources", "actions"})
	if !strings.Contains(string(body), `"valid":false`) {
		t.Errorf("expected valid:false in error output, got %s", body)
	}
}
