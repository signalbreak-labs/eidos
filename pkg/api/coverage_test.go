package api

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/config"
	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/ir"
	"github.com/signalbreak-labs/eidos/pkg/parser"
	"github.com/signalbreak-labs/eidos/pkg/transformer"
)

const coverageSpec = `openapi: "3.0.0"
info:
  title: Cover Store
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

// TestProviderServersIR covers servers with variables, without variables, and
// the empty case.
func TestProviderServersIR(t *testing.T) {
	got := providerServersIR(&parser.Spec{Servers: []parser.Server{
		{URL: "https://api.example.com", Description: "prod"},
		{URL: "https://{region}.example.com", Variables: map[string]*parser.ServerVariable{
			"region": {Default: "us-east", Enum: []string{"us-east", "us-west"}, Description: "region"},
		}},
	}})
	if len(got) != 2 {
		t.Fatalf("servers = %+v, want 2", got)
	}
	if got[0].URL != "https://api.example.com" || got[0].Description != "prod" {
		t.Errorf("servers[0] = %+v", got[0])
	}
	if got[1].Variables == nil || got[1].Variables["region"].Default != "us-east" {
		t.Errorf("servers[1] = %+v, want region variable", got[1])
	}
	if len(providerServersIR(&parser.Spec{})) != 0 {
		t.Error("empty spec should yield no servers")
	}
}

// TestAddSpecPathOperations_Webhooks drives the webhook iteration branch of
// addSpecPathOperations, which the full-pipeline tests never reach (the parser
// populates only Paths for these specs). A spec with nil Paths but a declared
// webhook also covers the nil-Paths branch.
func TestAddSpecPathOperations_Webhooks(t *testing.T) {
	preview := &ir.ProviderIR{Name: "acme"}
	spec := &parser.Spec{
		Webhooks: map[string]*parser.PathItem{
			"/pets": {Post: &parser.Operation{OperationID: "createPet"}},
		},
	}
	var diags diagnostics.Diagnostics
	addSpecPathOperations(preview, spec, "acme",
		map[string]map[string]bool{},
		map[string]map[transformer.HTTPMethod]transformer.Operation{},
		map[string][]string{}, &diags)
	// With no instance path in pathOps, the webhook POST classifies as an action.
	if len(preview.Actions) != 1 {
		t.Errorf("webhook actions = %+v, want 1", preview.Actions)
	}
	if preview.Actions[0].SourceOperation != "createPet" {
		t.Errorf("webhook action = %+v", preview.Actions[0])
	}
}

// TestOperationLabel covers the nil, OperationID, and summary-only branches.
func TestOperationLabel(t *testing.T) {
	if got := operationLabel(nil); got != "<unknown>" {
		t.Errorf("operationLabel(nil) = %q", got)
	}
	if got := operationLabel(&parser.Operation{OperationID: "getPet", Summary: "Fetch a pet"}); got != "getPet" {
		t.Errorf("operationLabel(with id) = %q", got)
	}
	if got := operationLabel(&parser.Operation{Summary: "Fetch a pet"}); got != "Fetch a pet" {
		t.Errorf("operationLabel(summary only) = %q", got)
	}
}

// TestOAuthFlowToIR covers the nil flow and the populated mapping.
func TestOAuthFlowToIR(t *testing.T) {
	if got := oauthFlowToIR(nil); got != nil {
		t.Errorf("oauthFlowToIR(nil) = %+v, want nil", got)
	}
	got := oauthFlowToIR(&parser.OAuthFlow{
		AuthorizationURL: "https://auth.example.com/authorize",
		TokenURL:         "https://auth.example.com/token",
		RefreshURL:       "https://auth.example.com/refresh",
		Scopes:           map[string]string{"read": "Read access"},
	})
	if got == nil || got.AuthorizationURL == "" || got.TokenURL == "" || got.RefreshURL == "" || got.Scopes["read"] == "" {
		t.Errorf("oauthFlowToIR = %+v", got)
	}
}

// TestEphemeralFromOverride covers name derivation, full lifecycle mappings,
// and the absent-mapping branches.
func TestEphemeralFromOverride(t *testing.T) {
	derived := ephemeralFromOverride(config.EphemeralOverride{Operation: "openSession"}, "acme")
	if derived.Name != "open_session" || derived.FullName != "acme_open_session" {
		t.Errorf("derived ephemeral = %+v", derived)
	}
	full := ephemeralFromOverride(config.EphemeralOverride{
		Operation:    "openSession",
		Name:         "session",
		Description:  "a session",
		OpenMapping:  "POST /sessions",
		RenewMapping: "POST /sessions/{id}/renew",
		CloseMapping: "DELETE /sessions/{id}/close",
	}, "acme")
	if full.Name != "session" || full.HasRenew != true || full.HasClose != true {
		t.Errorf("full ephemeral = %+v", full)
	}
	if full.RenewMapping == nil || full.RenewMapping.PathTemplate != "/sessions/{id}/renew" {
		t.Errorf("renew mapping = %+v", full.RenewMapping)
	}
	none := ephemeralFromOverride(config.EphemeralOverride{Operation: "openSession"}, "acme")
	if none.HasRenew || none.HasClose || none.OpenMapping.PathTemplate != "" {
		t.Errorf("mapping-less ephemeral = %+v", none)
	}
}

// TestResourceNameFromOverride covers the explicit name, the last-segment
// fallback, and the all-params fallback.
func TestResourceNameFromOverride(t *testing.T) {
	if got := resourceNameFromOverride(config.ResourceOverride{ResourceName: "animal"}, "/pets"); got != "animal" {
		t.Errorf("explicit name = %q", got)
	}
	if got := resourceNameFromOverride(config.ResourceOverride{}, "/dashboards/db"); got != "db" {
		t.Errorf("last segment = %q", got)
	}
	if got := resourceNameFromOverride(config.ResourceOverride{}, "/owners/{ownerId}/pets/{petId}"); got != "pets" {
		t.Errorf("last non-param segment = %q", got)
	}
	if got := resourceNameFromOverride(config.ResourceOverride{}, "/{a}/{b}"); got != "resource" {
		t.Errorf("all-params path = %q", got)
	}
}

// TestMediaTypeOf covers the nil op and the passthrough.
func TestMediaTypeOf(t *testing.T) {
	if got := mediaTypeOf(nil); got != "" {
		t.Errorf("mediaTypeOf(nil) = %q", got)
	}
	if got := mediaTypeOf(&transformer.Operation{RequestMediaType: "application/json"}); got != "application/json" {
		t.Errorf("mediaTypeOf = %q", got)
	}
}

// TestGroupSourceOperation covers create/read/delete preference and the empty
// case.
func TestGroupSourceOperation(t *testing.T) {
	g := transformer.ResourceCRUD{
		Create: &transformer.Operation{OperationID: "createPet"},
		Read:   &transformer.Operation{OperationID: "getPet"},
		Delete: &transformer.Operation{OperationID: "deletePet"},
	}
	if got := groupSourceOperation(g); got != "createPet" {
		t.Errorf("create preferred = %q", got)
	}
	g.Create = &transformer.Operation{}
	if got := groupSourceOperation(g); got != "getPet" {
		t.Errorf("read fallback = %q", got)
	}
	g.Read = &transformer.Operation{}
	if got := groupSourceOperation(g); got != "deletePet" {
		t.Errorf("delete fallback = %q", got)
	}
	if got := groupSourceOperation(transformer.ResourceCRUD{}); got != "" {
		t.Errorf("empty group = %q", got)
	}
}

// TestParserOp covers every method dispatch, the nil spec, the missing path,
// and the unknown method.
func TestParserOp(t *testing.T) {
	pi := &parser.PathItem{
		Get: &parser.Operation{OperationID: "g"}, Post: &parser.Operation{OperationID: "p"},
		Put: &parser.Operation{OperationID: "u"}, Patch: &parser.Operation{OperationID: "pa"},
		Delete: &parser.Operation{OperationID: "d"},
	}
	spec := &parser.Spec{Paths: map[string]*parser.PathItem{"/pets": pi}}
	for method, want := range map[string]string{
		"GET": "g", "POST": "p", "PUT": "u", "PATCH": "pa", "DELETE": "d",
	} {
		if got := parserOp(spec, "/pets", method); got == nil || got.OperationID != want {
			t.Errorf("parserOp(%s) = %+v, want %q", method, got, want)
		}
	}
	if got := parserOp(spec, "/pets", "get"); got == nil || got.OperationID != "g" {
		t.Errorf("parserOp should match method case-insensitively, got %+v", got)
	}
	if got := parserOp(nil, "/pets", "GET"); got != nil {
		t.Errorf("nil spec = %+v, want nil", got)
	}
	if got := parserOp(spec, "/missing", "GET"); got != nil {
		t.Errorf("missing path = %+v, want nil", got)
	}
	if got := parserOp(spec, "/pets", "OPTIONS"); got != nil {
		t.Errorf("unknown method = %+v, want nil", got)
	}
}

// TestIsConsumed covers the nil map, the consumed pair, and the absent pair.
func TestIsConsumed(t *testing.T) {
	if isConsumed(nil, "/pets", "POST") {
		t.Error("nil map should not report consumed")
	}
	consumed := map[string]map[string]bool{"/pets": {"POST": true}}
	if !isConsumed(consumed, "/pets", "POST") {
		t.Error("consumed pair should report true")
	}
	if isConsumed(consumed, "/pets", "GET") {
		t.Error("absent pair should report false")
	}
}

// TestResourceName covers the operation-id source and the path fallback.
func TestResourceName(t *testing.T) {
	if got := resourceName(&parser.Operation{OperationID: "getPet"}, "GET", "/pets/{id}"); got != "get_pet" {
		t.Errorf("resourceName(with id) = %q", got)
	}
	if got := resourceName(nil, "GET", "/pets/{id}"); got == "" {
		t.Error("resourceName(nil) should fall back to a normalized path name")
	}
}

// TestMergePathParams covers the nil op, no path params, dedup, and the append
// case.
func TestMergePathParams(t *testing.T) {
	if got := mergePathParams(nil, []parser.Parameter{{Name: "x"}}); got != nil {
		t.Errorf("nil op = %+v, want nil", got)
	}
	op := &parser.Operation{OperationID: "getPet", Parameters: []parser.Parameter{{Name: "id", In: "path"}}}
	if got := mergePathParams(op, nil); got != op {
		t.Error("no path params should return the original operation")
	}
	// A duplicate path param is skipped and the operation is returned unchanged.
	dup := mergePathParams(op, []parser.Parameter{{Name: "id", In: "path", Required: true}})
	if dup != op || len(dup.Parameters) != 1 {
		t.Errorf("duplicate merge = %+v, want unchanged", dup)
	}
	// A new path param is appended via a shallow copy.
	merged := mergePathParams(op, []parser.Parameter{{Name: "ownerId", In: "path", Required: true}})
	if merged == op || len(merged.Parameters) != 2 {
		t.Errorf("merged = %+v, want a copy with 2 params", merged)
	}
}

// TestParamFormat covers the nil schema and the passthrough.
func TestParamFormat(t *testing.T) {
	if got := paramFormat(nil); got != "" {
		t.Errorf("paramFormat(nil) = %q", got)
	}
	if got := paramFormat(&parser.Schema{Format: "binary"}); got != "binary" {
		t.Errorf("paramFormat = %q", got)
	}
}

// TestParamPrimitiveType covers every scalar type plus the nil and unknown
// branches.
func TestParamPrimitiveType(t *testing.T) {
	cases := []struct {
		typ  string
		want ir.PrimitiveType
	}{
		{"string", ir.TypeString},
		{"integer", ir.TypeInt},
		{"number", ir.TypeFloat},
		{"boolean", ir.TypeBool},
		{"object", ""},
	}
	for _, tc := range cases {
		if got := paramPrimitiveType(&parser.Schema{Type: tc.typ}); got != tc.want {
			t.Errorf("paramPrimitiveType(%q) = %q, want %q", tc.typ, got, tc.want)
		}
	}
	if got := paramPrimitiveType(nil); got != "" {
		t.Errorf("paramPrimitiveType(nil) = %q", got)
	}
}

// TestSchemaTypeString covers the string, []any, []string, and absent branches.
func TestSchemaTypeString(t *testing.T) {
	if got := schemaTypeString(&parser.Schema{Type: "integer"}); got != "integer" {
		t.Errorf("string type = %q", got)
	}
	if got := schemaTypeString(&parser.Schema{Type: []any{"null", "string"}}); got != "string" {
		t.Errorf("[]any type = %q", got)
	}
	if got := schemaTypeString(&parser.Schema{Type: []any{"null"}}); got != "" {
		t.Errorf("[]any all-null = %q", got)
	}
	if got := schemaTypeString(&parser.Schema{Type: []string{"null", "boolean"}}); got != "boolean" {
		t.Errorf("[]string type = %q", got)
	}
	if got := schemaTypeString(&parser.Schema{Type: nil}); got != "" {
		t.Errorf("absent type = %q", got)
	}
	if got := schemaTypeString(&parser.Schema{Type: 42}); got != "" {
		t.Errorf("unrecognized type = %q", got)
	}
}

// TestErrorMappingsFromResponses covers the nil op, no responses, 4xx/5xx with
// and without descriptions, and the all-2xx case.
func TestErrorMappingsFromResponses(t *testing.T) {
	if got := errorMappingsFromResponses(nil); got != nil {
		t.Errorf("nil op = %+v, want nil", got)
	}
	op := &parser.Operation{Responses: map[string]*parser.Response{
		"404": {Description: "not found"},
		"500": {},
		"200": {Description: "ok"},
		"2XX": {Description: "range"},
	}}
	m := errorMappingsFromResponses(op)
	if len(m) != 2 {
		t.Fatalf("mappings = %+v, want 2 (404, 500)", m)
	}
	if m[404].Description != "not found" || m[500].Description != "" {
		t.Errorf("mappings = %+v", m)
	}
	if got := errorMappingsFromResponses(&parser.Operation{Responses: map[string]*parser.Response{"200": {}}}); got != nil {
		t.Errorf("all-2xx = %+v, want nil", got)
	}
}

// TestOAuth2FlowAndTokenURL covers nil flows and each declared flow.
func TestOAuth2FlowAndTokenURL(t *testing.T) {
	if flow, url := oauth2FlowAndTokenURL(nil); flow != "" || url != "" {
		t.Errorf("nil flows = (%q,%q)", flow, url)
	}
	flows := &parser.OAuthFlows{
		ClientCredentials: &parser.OAuthFlow{TokenURL: "https://auth/token"},
	}
	if flow, url := oauth2FlowAndTokenURL(flows); flow != "client_credentials" || url != "https://auth/token" {
		t.Errorf("client_credentials = (%q,%q)", flow, url)
	}
	flows.Password = &parser.OAuthFlow{TokenURL: "https://auth/password"}
	if flow, _ := oauth2FlowAndTokenURL(flows); flow != "client_credentials" {
		t.Errorf("client_credentials should win, got %q", flow)
	}
	only := &parser.OAuthFlows{Password: &parser.OAuthFlow{TokenURL: "https://auth/password"}}
	if flow, _ := oauth2FlowAndTokenURL(only); flow != "password" {
		t.Errorf("password = %q", flow)
	}
	authCode := &parser.OAuthFlows{AuthorizationCode: &parser.OAuthFlow{TokenURL: "https://auth/code"}}
	if flow, _ := oauth2FlowAndTokenURL(authCode); flow != "authorization_code" {
		t.Errorf("authorization_code = %q", flow)
	}
	// The implicit flow has no token URL.
	implicit := &parser.OAuthFlows{Implicit: &parser.OAuthFlow{}}
	if flow, _ := oauth2FlowAndTokenURL(implicit); flow != "" {
		t.Errorf("implicit = %q, want empty", flow)
	}
}

// TestGroupIsResource covers the true case, the function-keyword rejection
// (the /convert/.../rules case that motivated groupEmitsFullCRUDResource), and
// the classified-as-action rejection via an x-terraform-* extension.
func TestGroupIsResource(t *testing.T) {
	mkOp := func(id string) *transformer.Operation {
		return &transformer.Operation{OperationID: id}
	}
	g := transformer.ResourceCRUD{
		CollectionPath: "/pets",
		InstancePath:   "/pets/{id}",
		Create:         mkOp("createPet"),
		Read:           mkOp("getPet"),
		Delete:         mkOp("deletePet"),
	}
	// pathOps must contain the instance path so the collection POST classifies
	// as a managed-resource Create (IsCRUDCreatePath) rather than an action.
	pathOps := map[string]map[transformer.HTTPMethod]transformer.Operation{
		"/pets": {
			transformer.MethodPost: {Method: transformer.MethodPost, Path: "/pets", OperationID: "createPet"},
		},
		"/pets/{id}": {
			transformer.MethodGet:    {Method: transformer.MethodGet, Path: "/pets/{id}", OperationID: "getPet"},
			transformer.MethodDelete: {Method: transformer.MethodDelete, Path: "/pets/{id}", OperationID: "deletePet"},
		},
	}
	if !groupIsResource(g, pathOps) {
		t.Error("clean CRUD group should classify as a resource")
	}
	// A group whose instance GET is reclassified as a provider function by a
	// path keyword (e.g. "convert") is rejected: the POST/DELETE must become
	// actions rather than an empty, fully-scaffolded managed resource (the
	// /convert/.../rules case the groupEmitsFullCRUDResource gate fixes).
	convertPathOps := map[string]map[transformer.HTTPMethod]transformer.Operation{
		"/convert/rules": {transformer.MethodPost: {Method: transformer.MethodPost, Path: "/convert/rules", OperationID: "postRules"}},
		"/convert/rules/{id}": {
			transformer.MethodGet:    {Method: transformer.MethodGet, Path: "/convert/rules/{id}", OperationID: "getRule"},
			transformer.MethodDelete: {Method: transformer.MethodDelete, Path: "/convert/rules/{id}", OperationID: "deleteRule"},
		},
	}
	convertGroup := transformer.ResourceCRUD{
		CollectionPath: "/convert/rules",
		InstancePath:   "/convert/rules/{id}",
		Create:         mkOp("postRules"),
		Read:           mkOp("getRule"),
		Delete:         mkOp("deleteRule"),
	}
	if groupIsResource(convertGroup, convertPathOps) {
		t.Error("group whose instance GET is a provider function (convert keyword) should not classify as a resource")
	}
	// An operation carrying x-terraform-action classifies as an action, so the
	// group is rejected. The extension rides on the transformer Operation that
	// groupIsResource reconstructs the parser operation from.
	actionGroup := g
	actionGroup.Create = &transformer.Operation{OperationID: "createPet", Extensions: map[string]any{"x-terraform-action": true}}
	if groupIsResource(actionGroup, pathOps) {
		t.Error("group with an action-classified operation should not classify as a resource")
	}
}

// TestNewValidateHandler_TooLarge asserts a body over the size limit returns
// 413 instead of a generic 400.
func TestNewValidateHandler_TooLarge(t *testing.T) {
	handler := NewValidateHandler()
	server := httptest.NewServer(handler)
	defer server.Close()

	big := bytes.Repeat([]byte("a"), maxRequestBodySize+1)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL+"/api/v1/validate", bytes.NewReader(big))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to reach server: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup: response body close error is non-actionable
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413, got %d", resp.StatusCode)
	}
}

// erroringReader fails reads with a non-MaxBytesError so the handler's read
// error path returns 400.
type erroringReader struct{}

func (erroringReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

// TestNewValidateHandler_ReadError asserts a body read error returns 400. The
// handler is invoked directly against a recorder (not through a real server)
// because a client-side body read failure aborts the transport before the
// response is readable.
func TestNewValidateHandler_ReadError(t *testing.T) {
	handler := NewValidateHandler()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/validate", erroringReader{})
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

// TestBuildProviderIRWithContentType covers the YAML sniff, an explicit JSON
// content type, and the malformed-spec error.
func TestBuildProviderIRWithContentType(t *testing.T) {
	preview, version, diags, err := BuildProviderIRWithContentType([]byte(coverageSpec), "", nil)
	if err != nil {
		t.Fatalf("yaml spec error: %v", err)
	}
	if preview == nil || preview.Name != "cover-store" {
		t.Errorf("preview = %+v, want name cover-store", preview)
	}
	if version != parser.Version3_0 {
		t.Errorf("version = %v, want 3.0", version)
	}
	_ = diags

	jsonSpec := `{"openapi":"3.0.0","info":{"title":"JSON Store","version":"1.0.0"},"paths":{}}`
	preview, _, _, err = BuildProviderIRWithContentType([]byte(jsonSpec), "application/json", nil)
	if err != nil || preview == nil {
		t.Fatalf("json spec error: %v", err)
	}
	if preview.Name != "json-store" {
		t.Errorf("json preview name = %q", preview.Name)
	}
	if _, _, _, err := BuildProviderIRWithContentType([]byte("{not valid"), "", nil); err == nil {
		t.Error("malformed spec should error")
	}
}

// TestGenerateStarterConfig covers the derived and overridden provider names.
func TestGenerateStarterConfig(t *testing.T) {
	cfg, _, _, err := GenerateStarterConfig([]byte(coverageSpec), "", true)
	if err != nil {
		t.Fatalf("GenerateStarterConfig error: %v", err)
	}
	if cfg.Provider.Name != "cover-store" {
		t.Errorf("derived provider name = %q, want cover-store", cfg.Provider.Name)
	}
	cfg, _, _, err = GenerateStarterConfig([]byte(coverageSpec), "custom", true)
	if err != nil {
		t.Fatalf("GenerateStarterConfig override error: %v", err)
	}
	if cfg.Provider.Name != "custom" {
		t.Errorf("overridden provider name = %q, want custom", cfg.Provider.Name)
	}
}

// TestDiagnosticDetails renders summaries and detail pairs.
func TestDiagnosticDetails(t *testing.T) {
	got := diagnosticDetails(diagnostics.Diagnostics{
		{Severity: diagnostics.Error, Summary: "boom", Detail: "detail here"},
		{Severity: diagnostics.Info, Summary: "note"},
	})
	if !strings.Contains(got, "boom: detail here") || !strings.Contains(got, "note") {
		t.Errorf("diagnosticDetails = %q", got)
	}
}
