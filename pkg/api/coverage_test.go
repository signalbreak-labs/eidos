package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/config"
	"github.com/signalbreak-labs/eidos/pkg/ir"
	"github.com/signalbreak-labs/eidos/pkg/parser"
	"github.com/signalbreak-labs/eidos/pkg/transformer"
)

// minimalSpec is a small OpenAPI 3.0 document exercising the parse → IR →
// starter-config paths that the HTTP /validate endpoint also covers. The
// POST + GET + DELETE group is a complete CRUD mapping, so the transformer
// infers a managed resource (not just an action).
const minimalSpec = `openapi: 3.0.1
info:
  title: Pet Store
  version: 1.0.0
paths:
  /pets:
    post:
      operationId: createPet
      responses:
        "201":
          description: created
  /pets/{id}:
    get:
      operationId: getPet
      responses:
        "200":
          description: ok
    delete:
      operationId: deletePet
      responses:
        "204":
          description: deleted
`

// TestParseSpec_ValidYAML verifies ParseSpec parses a YAML document and
// detects its version.
func TestParseSpec_ValidYAML(t *testing.T) {
	spec, diags, err := ParseSpec([]byte(minimalSpec), "spec.yaml")
	if err != nil {
		t.Fatalf("ParseSpec returned error: %v", err)
	}
	if spec == nil {
		t.Fatal("ParseSpec returned nil spec")
	}
	if len(diags) != 0 {
		t.Errorf("expected no diagnostics, got %d: %v", len(diags), diags)
	}
}

// TestParseSpec_ValidJSON verifies ParseSpec handles a JSON document.
func TestParseSpec_ValidJSON(t *testing.T) {
	body := []byte(`{"openapi":"3.0.1","info":{"title":"Pet Store","version":"1.0.0"},"paths":{}}`)
	spec, _, err := ParseSpec(body, "spec.json")
	if err != nil {
		t.Fatalf("ParseSpec returned error: %v", err)
	}
	if spec == nil {
		t.Fatal("ParseSpec returned nil spec")
	}
}

// TestParseSpec_InvalidBody verifies ParseSpec surfaces a parse error for a
// multi-document YAML stream (rejected rather than silently merged) and
// attributes it to the display name.
func TestParseSpec_InvalidBody(t *testing.T) {
	_, _, err := ParseSpec([]byte("---\na: 1\n---\nb: 2\n"), "spec.yaml")
	if err == nil {
		t.Fatal("expected an error for a multi-document YAML stream")
	}
	if !strings.Contains(err.Error(), "spec.yaml") {
		t.Errorf("error %q does not attribute the failure to the display name", err)
	}
}

// TestBuildProviderIRWithName verifies BuildProviderIRWithName parses a spec
// and produces a ProviderIR with the expected resource.
func TestBuildProviderIRWithName(t *testing.T) {
	provider, version, diags, err := BuildProviderIRWithName([]byte(minimalSpec), "spec.yaml", "", nil)
	if err != nil {
		t.Fatalf("BuildProviderIRWithName returned error: %v", err)
	}
	if provider == nil {
		t.Fatal("BuildProviderIRWithName returned nil provider")
	}
	if version != parser.Version3_0 {
		t.Errorf("expected Version3_0, got %v", version)
	}
	if len(provider.Resources) == 0 {
		t.Errorf("expected at least one resource, got %d", len(provider.Resources))
	}
	_ = diags
}

// TestBuildProviderIRWithName_InvalidSpec verifies BuildProviderIRWithName
// surfaces a load error for garbage input.
func TestBuildProviderIRWithName_InvalidSpec(t *testing.T) {
	_, _, _, err := BuildProviderIRWithName([]byte("garbage"), "spec.yaml", "", nil)
	if err == nil {
		t.Fatal("expected an error for invalid spec")
	}
}

// TestGenerateStarterConfigWithName verifies GenerateStarterConfigWithName
// produces a validated generator.yaml config from a spec.
func TestGenerateStarterConfigWithName(t *testing.T) {
	cfg, version, _, err := GenerateStarterConfigWithName([]byte(minimalSpec), "spec.yaml", "petstore", true)
	if err != nil {
		t.Fatalf("GenerateStarterConfigWithName returned error: %v", err)
	}
	if cfg == nil {
		t.Fatal("GenerateStarterConfigWithName returned nil config")
	}
	if cfg.Provider.Name != "petstore" {
		t.Errorf("expected provider name petstore, got %q", cfg.Provider.Name)
	}
	if version != parser.Version3_0 {
		t.Errorf("expected Version3_0, got %v", version)
	}
}

// TestLoadRequestBodyWithName_ErrorAttribution verifies loadRequestBodyWithName
// attributes parse errors to the caller's name rather than the generic
// "request.yaml".
func TestLoadRequestBodyWithName_ErrorAttribution(t *testing.T) {
	_, err := loadRequestBodyWithName([]byte("---\na: 1\n---\nb: 2\n"), "", "real-spec.yaml")
	if err == nil {
		t.Fatal("expected a parse error for a multi-document YAML stream")
	}
	if !strings.Contains(err.Error(), "real-spec.yaml") {
		t.Errorf("error %q does not attribute the failure to the real name", err)
	}
}

// TestApplyEphemeralOverrideExtras verifies the Open/Renew/Close lifecycle
// mappings from an ephemeral override are applied to the IR.
func TestApplyEphemeralOverrideExtras(t *testing.T) {
	e := &ir.EphemeralResourceIR{
		Name:         "session",
		ConfigSchema: ir.ObjectSchemaIR{},
		ResultSchema: ir.ObjectSchemaIR{},
	}
	eo := config.EphemeralOverride{
		Operation:    "open-session",
		OpenMapping:  "POST /sessions",
		RenewMapping: "POST /sessions/{id}/renew",
		CloseMapping: "DELETE /sessions/{id}",
	}
	applyEphemeralOverrideExtras(e, eo)

	if e.OpenMapping.Method != http.MethodPost || e.OpenMapping.PathTemplate != "/sessions" {
		t.Errorf("OpenMapping not applied: %+v", e.OpenMapping)
	}
	if e.RenewMapping == nil || !e.HasRenew {
		t.Errorf("RenewMapping not applied: renew=%v hasRenew=%v", e.RenewMapping, e.HasRenew)
	}
	if e.CloseMapping == nil || !e.HasClose {
		t.Errorf("CloseMapping not applied: close=%v hasClose=%v", e.CloseMapping, e.HasClose)
	}
}

// TestApplyEphemeralOverrideExtras_EmptyMappings verifies empty mapping
// strings leave the IR untouched.
func TestApplyEphemeralOverrideExtras_EmptyMappings(t *testing.T) {
	e := &ir.EphemeralResourceIR{
		Name:         "session",
		ConfigSchema: ir.ObjectSchemaIR{},
		ResultSchema: ir.ObjectSchemaIR{},
	}
	applyEphemeralOverrideExtras(e, config.EphemeralOverride{Operation: "open-session"})
	if e.HasRenew || e.HasClose {
		t.Errorf("empty mappings must not set lifecycle flags: renew=%v close=%v", e.HasRenew, e.HasClose)
	}
}

// TestResourceFromOperation verifies resourceFromOperation builds a ResourceIR
// from a parser operation, wiring the CRUD mapping by HTTP method.
func TestResourceFromOperation(t *testing.T) {
	op := &parser.Operation{
		OperationID: "createPet",
		Summary:     "Create a pet",
		Responses: map[string]*parser.Response{
			"201": {Description: "created"},
		},
	}
	pathOps := map[string]map[transformer.HTTPMethod]transformer.Operation{
		"/pets": {
			transformer.MethodPost: {Method: transformer.MethodPost, Path: "/pets", OperationID: "createPet"},
		},
	}
	res := resourceFromOperation(op, "petstore", "/pets", "POST", pathOps)
	if res.Name != "create_pet" {
		t.Errorf("expected resource name create_pet, got %q", res.Name)
	}
	if res.FullName != "petstore_create_pet" {
		t.Errorf("expected full name petstore_create_pet, got %q", res.FullName)
	}
	if res.CRUDMapping.Create.Method != http.MethodPost || res.CRUDMapping.Create.PathTemplate != "/pets" {
		t.Errorf("Create mapping not wired: %+v", res.CRUDMapping.Create)
	}
	if res.SourceOperation != "createPet" {
		t.Errorf("expected SourceOperation createPet, got %q", res.SourceOperation)
	}
}

// TestBuildProviderIRWithContentType verifies BuildProviderIRWithContentType
// routes JSON vs YAML by the explicit Content-Type hint.
func TestBuildProviderIRWithContentType(t *testing.T) {
	provider, version, _, err := BuildProviderIRWithContentType([]byte(minimalSpec), "application/yaml", nil)
	if err != nil {
		t.Fatalf("BuildProviderIRWithContentType returned error: %v", err)
	}
	if provider == nil {
		t.Fatal("BuildProviderIRWithContentType returned nil provider")
	}
	if version != parser.Version3_0 {
		t.Errorf("expected Version3_0, got %v", version)
	}
	if len(provider.Resources) == 0 {
		t.Errorf("expected at least one resource, got %d", len(provider.Resources))
	}
}

// TestMergePathParams verifies mergePathParams appends path-level parameters
// that the operation does not already declare, and skips duplicates.
func TestMergePathParams(t *testing.T) {
	op := &parser.Operation{
		OperationID: "getPet",
		Parameters: []parser.Parameter{
			{Name: "petId", In: "path", Required: true},
		},
	}
	pathParams := []parser.Parameter{
		{Name: "petId", In: "path", Required: true}, // duplicate: skipped
		{Name: "verbose", In: "query"},
	}
	merged := mergePathParams(op, pathParams)
	if merged == op {
		t.Fatal("expected a new operation with merged parameters")
	}
	if len(merged.Parameters) != 2 {
		t.Fatalf("expected 2 parameters, got %d", len(merged.Parameters))
	}
	if merged.Parameters[1].Name != "verbose" {
		t.Errorf("expected verbose appended, got %q", merged.Parameters[1].Name)
	}
}

// TestMergePathParams_NilOrEmpty verifies mergePathParams returns the input
// unchanged for a nil operation or no path params.
func TestMergePathParams_NilOrEmpty(t *testing.T) {
	if got := mergePathParams(nil, []parser.Parameter{{Name: "x"}}); got != nil {
		t.Error("nil operation must return nil")
	}
	op := &parser.Operation{OperationID: "getPet"}
	if got := mergePathParams(op, nil); got != op {
		t.Error("no path params must return the original operation")
	}
}

// TestSchemaTypeString verifies schemaTypeString extracts a non-null type from
// a string, []any, or []string schema type.
func TestSchemaTypeString(t *testing.T) {
	if got := schemaTypeString(&parser.Schema{Type: "string"}); got != "string" {
		t.Errorf("string type: got %q", got)
	}
	if got := schemaTypeString(&parser.Schema{Type: []any{"null", "string"}}); got != "string" {
		t.Errorf("[]any type: got %q", got)
	}
	if got := schemaTypeString(&parser.Schema{Type: []string{"null", "integer"}}); got != "integer" {
		t.Errorf("[]string type: got %q", got)
	}
	if got := schemaTypeString(&parser.Schema{Type: []any{"null"}}); got != "" {
		t.Errorf("null-only type: got %q, want empty", got)
	}
	if got := schemaTypeString(&parser.Schema{Type: 42}); got != "" {
		t.Errorf("unsupported type: got %q, want empty", got)
	}
}
