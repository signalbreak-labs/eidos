package mcp

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// lookupSpec is a small OpenAPI doc with a $ref request/response schema and a
// path parameter, enough to exercise both lookup directions.
const lookupSpec = `openapi: 3.0.0
info:
  title: Lookup Store
  version: 1.0.0
paths:
  /pets:
    post:
      operationId: createPet
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/Pet'
      responses:
        "201":
          description: created
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Pet'
  /pets/{id}:
    get:
      operationId: getPet
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Pet'
    delete:
      operationId: deletePet
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "204":
          description: deleted
components:
  schemas:
    Pet:
      type: object
      properties:
        id:
          type: string
        name:
          type: string
`

// TestHandleLookup_ForwardOperation asserts the forward direction: looking up an
// operation by id returns its path, method, path params, and response schema.
func TestHandleLookup_ForwardOperation(t *testing.T) {
	_, out, err := HandleLookup(context.Background(), nil, LookupArgs{Spec: lookupSpec, OperationID: "getPet"})
	if err != nil {
		t.Fatalf("HandleLookup error: %v", err)
	}
	if !out.Valid {
		t.Fatalf("expected valid result, got diagnostics: %+v", out.Diagnostics)
	}
	if out.Operation == nil {
		t.Fatalf("expected operation lookup, got nil")
	}
	if out.Operation.OperationID != "getPet" {
		t.Errorf("operation_id = %q, want getPet", out.Operation.OperationID)
	}
	if out.Operation.Path != "/pets/{id}" {
		t.Errorf("path = %q, want /pets/{id}", out.Operation.Path)
	}
	if out.Operation.Method != http.MethodGet {
		t.Errorf("method = %q, want GET", out.Operation.Method)
	}
	if out.Operation.RequestBodySchema != "" {
		t.Errorf("getPet has no request body, got request_body_schema=%q", out.Operation.RequestBodySchema)
	}
	if out.Operation.ResponseSchema != "Pet" {
		t.Errorf("response_schema = %q, want Pet", out.Operation.ResponseSchema)
	}
	if out.Operation.RequestMediaType != "" {
		t.Errorf("getPet has no request media type, got %q", out.Operation.RequestMediaType)
	}
	// One path param: id (required, string).
	if len(out.Operation.PathParams) != 1 {
		t.Fatalf("expected 1 path param, got %+v", out.Operation.PathParams)
	}
	pp := out.Operation.PathParams[0]
	if pp.Name != "id" || !pp.Required {
		t.Errorf("path param = %+v, want {id required}", pp)
	}
}

// TestHandleLookup_ForwardOperationWithRequestBody asserts the forward direction
// resolves the request body schema and media type for a POST with a body.
func TestHandleLookup_ForwardOperationWithRequestBody(t *testing.T) {
	_, out, err := HandleLookup(context.Background(), nil, LookupArgs{Spec: lookupSpec, OperationID: "createPet"})
	if err != nil {
		t.Fatalf("HandleLookup error: %v", err)
	}
	if out.Operation == nil {
		t.Fatalf("expected operation lookup, got nil")
	}
	if out.Operation.RequestBodySchema != "Pet" {
		t.Errorf("request_body_schema = %q, want Pet", out.Operation.RequestBodySchema)
	}
	if out.Operation.RequestMediaType != "application/json" {
		t.Errorf("request_media_type = %q, want application/json", out.Operation.RequestMediaType)
	}
	if out.Operation.ResponseSchema != "Pet" {
		t.Errorf("response_schema = %q, want Pet", out.Operation.ResponseSchema)
	}
}

// TestHandleLookup_ReverseSchema asserts the reverse direction: looking up a
// schema by name returns every operation that accepts it (request) or returns
// it (response), in deterministic order.
func TestHandleLookup_ReverseSchema(t *testing.T) {
	_, out, err := HandleLookup(context.Background(), nil, LookupArgs{Spec: lookupSpec, Schema: "Pet"})
	if err != nil {
		t.Fatalf("HandleLookup error: %v", err)
	}
	if !out.Valid {
		t.Fatalf("expected valid result, got diagnostics: %+v", out.Diagnostics)
	}
	// Pet is used as: createPet request, createPet response, getPet response.
	// deletePet returns 204 (no content) so it is absent. Order is deterministic:
	// path-sorted (/pets before /pets/{id}), then method-sorted within a path.
	want := []struct {
		op   string
		path string
		m    string
		role string
	}{
		{"createPet", "/pets", "POST", "request"},
		{"createPet", "/pets", "POST", "response"},
		{"getPet", "/pets/{id}", "GET", "response"},
	}
	if len(out.SchemaUsage) != len(want) {
		t.Fatalf("expected %d schema uses, got %+v", len(want), out.SchemaUsage)
	}
	for i, w := range want {
		got := out.SchemaUsage[i]
		if got.OperationID != w.op || got.Path != w.path || got.Method != w.m || got.Role != w.role {
			t.Errorf("schema_usage[%d] = %+v, want %+v", i, got, w)
		}
	}
}

// TestHandleLookup_BothDirections asserts operation_id and schema can be set
// together to answer both directions in one call.
func TestHandleLookup_BothDirections(t *testing.T) {
	_, out, err := HandleLookup(context.Background(), nil, LookupArgs{Spec: lookupSpec, OperationID: "createPet", Schema: "Pet"})
	if err != nil {
		t.Fatalf("HandleLookup error: %v", err)
	}
	if out.Operation == nil || out.Operation.OperationID != "createPet" {
		t.Errorf("expected createPet operation, got %+v", out.Operation)
	}
	if len(out.SchemaUsage) != 3 {
		t.Errorf("expected 3 schema uses, got %+v", out.SchemaUsage)
	}
}

// TestHandleLookup_OperationNotFound asserts a missing operation_id yields a
// nil operation and a warning diagnostic, with valid still true.
func TestHandleLookup_OperationNotFound(t *testing.T) {
	_, out, err := HandleLookup(context.Background(), nil, LookupArgs{Spec: lookupSpec, OperationID: "nope"})
	if err != nil {
		t.Fatalf("HandleLookup error: %v", err)
	}
	if out.Operation != nil {
		t.Errorf("expected nil operation for missing id, got %+v", out.Operation)
	}
	found := false
	for _, d := range out.Diagnostics {
		if d.Severity == "warning" && strings.Contains(d.Summary, "nope") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a warning mentioning %q, got diagnostics %+v", "nope", out.Diagnostics)
	}
}

// TestHandleLookup_SchemaNotFound asserts an unknown schema name yields an
// empty (non-nil) schema_usage slice and no crash.
func TestHandleLookup_SchemaNotFound(t *testing.T) {
	_, out, err := HandleLookup(context.Background(), nil, LookupArgs{Spec: lookupSpec, Schema: "Nope"})
	if err != nil {
		t.Fatalf("HandleLookup error: %v", err)
	}
	if out.SchemaUsage == nil {
		t.Errorf("expected non-nil schema_usage, got nil")
	}
	if len(out.SchemaUsage) != 0 {
		t.Errorf("expected 0 schema uses for unknown schema, got %+v", out.SchemaUsage)
	}
}

// TestHandleLookup_NoArgsError asserts that omitting both operation_id and
// schema fails loud with an error diagnostic and valid=false.
func TestHandleLookup_NoArgsError(t *testing.T) {
	_, out, err := HandleLookup(context.Background(), nil, LookupArgs{Spec: lookupSpec})
	if err != nil {
		t.Fatalf("HandleLookup error: %v", err)
	}
	if out.Valid {
		t.Errorf("expected valid=false when neither arg is set, got valid=true")
	}
	found := false
	for _, d := range out.Diagnostics {
		if d.Severity == "error" && strings.Contains(d.Summary, "operation_id or schema") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an error requiring operation_id or schema, got %+v", out.Diagnostics)
	}
}

// TestHandleLookup_InvalidSpec asserts an unparseable spec reports valid=false
// with an error diagnostic and never populates operation/schema_usage.
func TestHandleLookup_InvalidSpec(t *testing.T) {
	_, out, err := HandleLookup(context.Background(), nil, LookupArgs{Spec: "not: an openapi doc [", OperationID: "createPet"})
	if err != nil {
		t.Fatalf("HandleLookup error: %v", err)
	}
	if out.Valid {
		t.Errorf("expected valid=false for invalid spec, got valid=true")
	}
	if out.Operation != nil {
		t.Errorf("expected nil operation for invalid spec, got %+v", out.Operation)
	}
}

// TestHandleLookup_EmptyAndErrorValidateAgainstOutputSchema asserts the empty,
// invalid, and error paths all produce output that validates against the
// lookup OutputSchema — including non-nil required array fields (the
// null-array regression guard that mirrors the other tools).
func TestHandleLookup_EmptyAndErrorValidateAgainstOutputSchema(t *testing.T) {
	cases := []struct {
		name string
		args LookupArgs
	}{
		{"empty valid spec", LookupArgs{Spec: emptySpec, OperationID: "anything"}},
		{"invalid spec", LookupArgs{Spec: invalidSpec, OperationID: "anything"}},
		{"file read error", LookupArgs{Spec: "/definitely/not/a/real/spec.yaml", OperationID: "anything"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, out, err := HandleLookup(context.Background(), nil, tc.args)
			if err != nil {
				t.Fatalf("HandleLookup error: %v", err)
			}
			body := toolBody(t, res)
			assertOutputValidates(t, LookupTool(), body)
			assertArrayFieldsNotNull(t, body, []string{"diagnostics", "schema_usage"})
			assertStructuredOutputValidates(t, LookupTool(), out)
		})
	}
}

// TestLookupErrorResult asserts the error-result constructor produces output
// that validates against the lookup OutputSchema with non-nil arrays.
func TestLookupErrorResult(t *testing.T) {
	out := lookupErrorResult(errors.New("boom"))
	if out.Valid {
		t.Errorf("expected valid=false from error result")
	}
	res, err := marshalToolResult(out)
	if err != nil {
		t.Fatalf("marshalToolResult: %v", err)
	}
	body := toolBody(t, res)
	assertOutputValidates(t, LookupTool(), body)
	assertArrayFieldsNotNull(t, body, []string{"diagnostics", "schema_usage"})
	if !strings.Contains(string(body), "boom") {
		t.Errorf("expected error detail in body, got %s", body)
	}
}

// TestRecoverHandler_LookupPanicPath asserts recoverHandler turns a lookup
// handler panic into schema-conformant output (the lookup-specific instance of
// the generic guard).
func TestRecoverHandler_LookupPanicPath(t *testing.T) {
	var (
		res *sdkmcp.CallToolResult
		out LookupResult
	)
	func() {
		defer recoverHandler("eidos/lookup", lookupErrorResult, &res, &out)
		panic("kaboom")
	}()
	if res == nil {
		t.Fatal("expected recoverHandler to set a non-nil result")
	}
	if out.Valid {
		t.Errorf("expected Valid=false after panic, got %+v", out)
	}
	body := toolBody(t, res)
	assertOutputValidates(t, LookupTool(), body)
	if !strings.Contains(string(body), "panic in eidos/lookup handler") {
		t.Errorf("expected panic summary in body, got %s", body)
	}
}

// TestLookupTool_RegistrationSanity sanity-checks the tool definition fields the
// SDK relies on, so a rename or schema typo is caught without a live server.
func TestLookupTool_RegistrationSanity(t *testing.T) {
	tool := LookupTool()
	if tool.Name != "eidos/lookup" {
		t.Errorf("tool name = %q, want eidos/lookup", tool.Name)
	}
	if tool.InputSchema == nil || tool.OutputSchema == nil {
		t.Fatal("tool must declare both input and output schemas")
	}
	// OutputSchema requires schema_usage (array); operation is optional. The
	// resolved schema must validate an empty-object result (all fields optional
	// except schema_usage, which is non-nil in the error-result shape).
	res, err := marshalToolResult(lookupErrorResult(errors.New("x")))
	if err != nil {
		t.Fatalf("marshalToolResult: %v", err)
	}
	assertOutputValidates(t, tool, toolBody(t, res))
}
