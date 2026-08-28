package api

import (
	"bytes"
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

// TestProviderServersIR covers providerServersIR with and without server
// variables: a server with variables maps them onto ServerVariableIR, a server
// without variables leaves the map nil.
func TestProviderServersIR(t *testing.T) {
	spec := &parser.Spec{
		Servers: []parser.Server{
			{
				URL:         "https://{region}.api.example.com",
				Description: "regional endpoint",
				Variables: map[string]*parser.ServerVariable{
					"region": {Default: "us-east-1", Enum: []string{"us-east-1", "eu-west-1"}, Description: "deployment region"},
				},
			},
			{URL: "https://fallback.example.com"},
		},
	}
	servers := providerServersIR(spec)
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(servers))
	}
	if servers[0].URL != "https://{region}.api.example.com" || servers[0].Description != "regional endpoint" {
		t.Errorf("server[0] = %+v", servers[0])
	}
	v, ok := servers[0].Variables["region"]
	if !ok {
		t.Fatalf("expected region variable, got %+v", servers[0].Variables)
	}
	if v.Default != "us-east-1" || len(v.Enum) != 2 || v.Description != "deployment region" {
		t.Errorf("region variable = %+v", v)
	}
	if servers[1].Variables != nil {
		t.Errorf("server without variables must leave Variables nil, got %+v", servers[1].Variables)
	}
}

// TestProviderServersIR_NoServers covers the empty-servers path.
func TestProviderServersIR_NoServers(t *testing.T) {
	if got := providerServersIR(&parser.Spec{}); len(got) != 0 {
		t.Errorf("expected no servers, got %+v", got)
	}
}

// TestDataSourceAlreadyExists covers the three match modes: same name, same
// read path+method, and no match.
func TestDataSourceAlreadyExists(t *testing.T) {
	existing := []ir.DataSourceIR{
		{Name: "widgets", ReadMapping: ir.OperationMappingIR{Method: "GET", PathTemplate: "/widgets"}},
	}
	// Same name.
	if !dataSourceAlreadyExists(existing, ir.DataSourceIR{Name: "widgets"}) {
		t.Error("same name must match")
	}
	// Same read path+method (case-insensitive method).
	if !dataSourceAlreadyExists(existing, ir.DataSourceIR{Name: "other", ReadMapping: ir.OperationMappingIR{Method: "get", PathTemplate: "/widgets"}}) {
		t.Error("same read path+method must match")
	}
	// No match.
	if dataSourceAlreadyExists(existing, ir.DataSourceIR{Name: "other", ReadMapping: ir.OperationMappingIR{Method: "GET", PathTemplate: "/other"}}) {
		t.Error("different name and path must not match")
	}
	// Empty read mapping on the candidate never matches by path.
	if dataSourceAlreadyExists(existing, ir.DataSourceIR{Name: "other"}) {
		t.Error("candidate with empty read mapping must not match")
	}
}

// TestApplyActionOverrideExtras covers the modify_plan_operation and
// validate_config_operation wiring plus the empty-string no-op path.
func TestApplyActionOverrideExtras(t *testing.T) {
	a := &ir.ActionIR{}
	ao := config.ActionOverride{
		ModifyPlanOperation:     "POST /pets/{id}/validate",
		ValidateConfigOperation: "POST /pets/validate-config",
	}
	applyActionOverrideExtras(a, ao)
	if a.ModifyPlanMapping == nil || !a.ModifyPlan {
		t.Errorf("modify_plan not wired: mapping=%+v flag=%v", a.ModifyPlanMapping, a.ModifyPlan)
	}
	if a.ModifyPlanMapping.Method != "POST" || a.ModifyPlanMapping.PathTemplate != "/pets/{id}/validate" {
		t.Errorf("modify_plan mapping = %+v", a.ModifyPlanMapping)
	}
	if a.ValidateConfigMapping == nil || a.ValidateConfigMapping.PathTemplate != "/pets/validate-config" {
		t.Errorf("validate_config mapping = %+v", a.ValidateConfigMapping)
	}

	// Empty strings leave the action untouched.
	b := &ir.ActionIR{ModifyPlan: true}
	applyActionOverrideExtras(b, config.ActionOverride{})
	if b.ModifyPlanMapping != nil || b.ValidateConfigMapping != nil {
		t.Errorf("empty override must not wire mappings: %+v", b)
	}
}

// TestMatchingEphemeralIndex covers match by source operation and no-match.
func TestMatchingEphemeralIndex(t *testing.T) {
	ephemerals := []ir.EphemeralResourceIR{
		{Name: "session", SourceOperation: "open-session"},
	}
	if idx := matchingEphemeralIndex(ephemerals, config.EphemeralOverride{Operation: "open-session"}); idx != 0 {
		t.Errorf("expected match at index 0, got %d", idx)
	}
	if idx := matchingEphemeralIndex(ephemerals, config.EphemeralOverride{Operation: "other"}); idx != -1 {
		t.Errorf("expected no match, got %d", idx)
	}
	if idx := matchingEphemeralIndex(nil, config.EphemeralOverride{Operation: "open-session"}); idx != -1 {
		t.Errorf("expected no match on empty list, got %d", idx)
	}
}

// TestMatchingListResourceIndex covers match by source operation and no-match.
func TestMatchingListResourceIndex(t *testing.T) {
	lists := []ir.ListResourceIR{
		{Name: "widgets", SourceOperation: "listWidgets"},
	}
	if idx := matchingListResourceIndex(lists, config.ListResourceOverride{Operation: "listWidgets"}); idx != 0 {
		t.Errorf("expected match at index 0, got %d", idx)
	}
	if idx := matchingListResourceIndex(lists, config.ListResourceOverride{Operation: "other"}); idx != -1 {
		t.Errorf("expected no match, got %d", idx)
	}
}

// TestEphemeralFromOverride covers the name-from-override and name-from-operation
// paths plus the Open/Renew/Close mapping wiring.
func TestEphemeralFromOverride(t *testing.T) {
	eo := config.EphemeralOverride{
		Name:         "session",
		Operation:    "open-session",
		Description:  "a session",
		OpenMapping:  "POST /sessions",
		RenewMapping: "POST /sessions/{id}/renew",
		CloseMapping: "DELETE /sessions/{id}",
	}
	er := ephemeralFromOverride(eo, "acme")
	if er.Name != "session" || er.FullName != "acme_session" || er.TypeName != "acme_session" {
		t.Errorf("identity = %+v", er)
	}
	if er.SourceOperation != "open-session" || er.Description != "a session" {
		t.Errorf("source/description = %+v", er)
	}
	if er.OpenMapping.Method != "POST" || er.OpenMapping.PathTemplate != "/sessions" {
		t.Errorf("open mapping = %+v", er.OpenMapping)
	}
	if er.RenewMapping == nil || !er.HasRenew {
		t.Errorf("renew not wired: %+v", er.RenewMapping)
	}
	if er.CloseMapping == nil || !er.HasClose {
		t.Errorf("close not wired: %+v", er.CloseMapping)
	}

	// No name: derived from the operation id.
	er2 := ephemeralFromOverride(config.EphemeralOverride{Operation: "open-session"}, "acme")
	if er2.Name != "open_session" {
		t.Errorf("derived name = %q, want open_session", er2.Name)
	}
	// No mappings: lifecycle flags stay false.
	er3 := ephemeralFromOverride(config.EphemeralOverride{Operation: "open-session"}, "acme")
	if er3.HasRenew || er3.HasClose {
		t.Errorf("no mappings must not set lifecycle flags: %+v", er3)
	}
}

// TestResourceNameFromOverride covers the explicit-name, path-derived, and
// fallback branches of resourceNameFromOverride.
func TestResourceNameFromOverride(t *testing.T) {
	if got := resourceNameFromOverride(config.ResourceOverride{ResourceName: "  widget  "}, "/pets"); got != "widget" {
		t.Errorf("explicit name = %q, want widget", got)
	}
	if got := resourceNameFromOverride(config.ResourceOverride{}, "/api/v1/widgets"); got != "widgets" {
		t.Errorf("path-derived name = %q, want widgets", got)
	}
	if got := resourceNameFromOverride(config.ResourceOverride{}, "/{id}"); got != "resource" {
		t.Errorf("all-placeholder path = %q, want resource", got)
	}
	// An empty path has a single empty segment, which is not a placeholder, so
	// it derives an empty name.
	if got := resourceNameFromOverride(config.ResourceOverride{}, ""); got != "" {
		t.Errorf("empty path = %q, want empty", got)
	}
}

// TestGroupSourceOperation covers the create/read/delete priority and the
// empty fallback.
func TestGroupSourceOperation(t *testing.T) {
	g := transformer.ResourceCRUD{
		Create: &transformer.Operation{OperationID: "createWidget"},
		Read:   &transformer.Operation{OperationID: "getWidget"},
		Delete: &transformer.Operation{OperationID: "deleteWidget"},
	}
	if got := groupSourceOperation(g); got != "createWidget" {
		t.Errorf("create priority = %q, want createWidget", got)
	}
	g.Create = &transformer.Operation{}
	if got := groupSourceOperation(g); got != "getWidget" {
		t.Errorf("read fallback = %q, want getWidget", got)
	}
	g.Read = &transformer.Operation{}
	if got := groupSourceOperation(g); got != "deleteWidget" {
		t.Errorf("delete fallback = %q, want deleteWidget", got)
	}
	g.Delete = &transformer.Operation{}
	if got := groupSourceOperation(g); got != "" {
		t.Errorf("empty group = %q, want empty", got)
	}
}

// TestCrudGroupDescriptionOp covers the read-then-create priority and the nil
// fallback.
func TestCrudGroupDescriptionOp(t *testing.T) {
	spec := &parser.Spec{Paths: map[string]*parser.PathItem{
		"/widgets": {
			Post: &parser.Operation{OperationID: "createWidget", Summary: "create"},
		},
		"/widgets/{id}": {
			Get: &parser.Operation{OperationID: "getWidget", Summary: "read"},
		},
	}}
	g := transformer.ResourceCRUD{
		Create: &transformer.Operation{Path: "/widgets", Method: transformer.MethodPost},
		Read:   &transformer.Operation{Path: "/widgets/{id}", Method: transformer.MethodGet},
	}
	if op := crudGroupDescriptionOp(spec, g); op == nil || op.OperationID != "getWidget" {
		t.Errorf("read priority = %+v, want getWidget", op)
	}
	g.Read = nil
	if op := crudGroupDescriptionOp(spec, g); op == nil || op.OperationID != "createWidget" {
		t.Errorf("create fallback = %+v, want createWidget", op)
	}
	g.Create = nil
	if op := crudGroupDescriptionOp(spec, g); op != nil {
		t.Errorf("empty group = %+v, want nil", op)
	}
}

// TestParamIR covers the enum/default/const propagation and the nil-schema path.
func TestParamIR(t *testing.T) {
	p := parser.Parameter{
		Name:        "status",
		In:          "query",
		Description: "filter by status",
		Required:    true,
		Schema: &parser.Schema{
			Type:    "string",
			Enum:    []any{"active", "inactive"},
			Default: "active",
			Const:   "active",
		},
	}
	got := paramIR(p)
	if got.Name != "status" || got.In != "query" || !got.Required || got.Description != "filter by status" {
		t.Errorf("param = %+v", got)
	}
	if got.Schema.Type != ir.TypeString || len(got.Schema.EnumValues) != 2 {
		t.Errorf("schema = %+v", got.Schema)
	}
	if got.Schema.Default == nil || *got.Schema.Default != "active" {
		t.Errorf("default = %+v, want active", got.Schema.Default)
	}
	if got.Schema.Const == nil || *got.Schema.Const != "active" {
		t.Errorf("const = %+v, want active", got.Schema.Const)
	}

	// Nil schema: no enum/default/const, empty type.
	got2 := paramIR(parser.Parameter{Name: "bare", In: "path"})
	if got2.Schema.Type != "" || got2.Schema.EnumValues != nil || got2.Schema.Default != nil || got2.Schema.Const != nil {
		t.Errorf("nil-schema param = %+v", got2.Schema)
	}
}

// TestResolveParamRef covers the no-ref passthrough, the resolved component
// parameter, the unresolved-ref fallback, and the nil-spec guard.
func TestResolveParamRef(t *testing.T) {
	inline := parser.Parameter{Name: "inline", In: "query", Schema: &parser.Schema{Type: "string"}}
	if got := resolveParamRef(nil, inline); got.Name != "inline" {
		t.Errorf("nil spec must return the input, got %+v", got)
	}

	ref := parser.Parameter{Ref: "#/components/parameters/PageSize", Name: "pageSize", In: "query"}
	spec := &parser.Spec{Components: &parser.Components{
		Parameters: map[string]*parser.Parameter{
			"PageSize": {Name: "page_size", In: "query", Schema: &parser.Schema{Type: "integer"}},
		},
	}}
	if got := resolveParamRef(spec, ref); got == nil || got.Name != "page_size" {
		t.Errorf("resolved ref = %+v, want page_size", got)
	}

	missing := parser.Parameter{Ref: "#/components/parameters/Missing", Name: "x"}
	if got := resolveParamRef(spec, missing); got == nil || got.Name != "x" {
		t.Errorf("unresolved ref must fall back to the input, got %+v", got)
	}
}

// TestParamPrimitiveType covers the scalar mapping and the empty fallback.
func TestParamPrimitiveType(t *testing.T) {
	cases := map[string]ir.PrimitiveType{
		"string":  ir.TypeString,
		"integer": ir.TypeInt,
		"number":  ir.TypeFloat,
		"boolean": ir.TypeBool,
		"array":   "",
		"":        "",
	}
	for in, want := range cases {
		if got := paramPrimitiveType(&parser.Schema{Type: in}); got != want {
			t.Errorf("paramPrimitiveType(%q) = %q, want %q", in, got, want)
		}
	}
	if got := paramPrimitiveType(nil); got != "" {
		t.Errorf("nil schema = %q, want empty", got)
	}
}

// TestStarterConfigToggle covers the use_put_as_create toggle: true yields nil
// (default-on), false yields an explicit false pointer.
func TestStarterConfigToggle(t *testing.T) {
	if got := starterConfigToggle(true); got != nil {
		t.Errorf("use_put_as_create=true must yield nil config, got %+v", got)
	}
	got := starterConfigToggle(false)
	if got == nil || got.UsePutAsCreate == nil || *got.UsePutAsCreate {
		t.Errorf("use_put_as_create=false must yield explicit false, got %+v", got)
	}
}

// TestOAuth2FlowAndTokenURL covers the flow-priority selection and the
// unrepresentable implicit-only / nil paths.
func TestOAuth2FlowAndTokenURL(t *testing.T) {
	if flow, url := oauth2FlowAndTokenURL(nil); flow != "" || url != "" {
		t.Errorf("nil flows = (%q,%q), want empty", flow, url)
	}
	flows := &parser.OAuthFlows{
		ClientCredentials: &parser.OAuthFlow{TokenURL: "https://api.example.com/oauth/token"},
		Password:          &parser.OAuthFlow{TokenURL: "https://api.example.com/oauth/password"},
		AuthorizationCode: &parser.OAuthFlow{TokenURL: "https://api.example.com/oauth/authcode"},
	}
	if flow, url := oauth2FlowAndTokenURL(flows); flow != "client_credentials" || url != "https://api.example.com/oauth/token" {
		t.Errorf("client_credentials priority = (%q,%q)", flow, url)
	}
	flows.ClientCredentials = nil
	if flow, url := oauth2FlowAndTokenURL(flows); flow != "password" || url != "https://api.example.com/oauth/password" {
		t.Errorf("password = (%q,%q)", flow, url)
	}
	flows.Password = nil
	if flow, url := oauth2FlowAndTokenURL(flows); flow != "authorization_code" || url != "https://api.example.com/oauth/authcode" {
		t.Errorf("authorization_code = (%q,%q)", flow, url)
	}
	// Implicit-only has no token URL and cannot be represented.
	implicit := &parser.OAuthFlows{Implicit: &parser.OAuthFlow{AuthorizationURL: "https://api.example.com/oauth/authorize"}}
	if flow, url := oauth2FlowAndTokenURL(implicit); flow != "" || url != "" {
		t.Errorf("implicit-only = (%q,%q), want empty", flow, url)
	}
}

// TestNewValidateHandler_RequestBodyTooLarge covers the 413 branch: a body
// exceeding maxRequestBodySize surfaces as *http.MaxBytesError and returns 413
// rather than a 400 that leaks the reader error (L-14).
func TestNewValidateHandler_RequestBodyTooLarge(t *testing.T) {
	h := NewValidateHandler()
	big := bytes.Repeat([]byte("x"), maxRequestBodySize+1)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/validate", bytes.NewReader(big))
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "request body too large") {
		t.Errorf("body = %q, want the 413 message", rec.Body.String())
	}
}

// TestOperationLabel covers the nil, operationId, and summary branches.
func TestOperationLabel(t *testing.T) {
	if got := operationLabel(nil); got != "<unknown>" {
		t.Errorf("nil op = %q, want <unknown>", got)
	}
	if got := operationLabel(&parser.Operation{OperationID: "getPet"}); got != "getPet" {
		t.Errorf("operationId = %q, want getPet", got)
	}
	if got := operationLabel(&parser.Operation{Summary: "Get a pet"}); got != "Get a pet" {
		t.Errorf("summary = %q, want Get a pet", got)
	}
}

// TestMediaTypeOfAndEnvelopeOf covers the nil-safe accessors.
func TestMediaTypeOfAndEnvelopeOf(t *testing.T) {
	if got := mediaTypeOf(nil); got != "" {
		t.Errorf("mediaTypeOf(nil) = %q, want empty", got)
	}
	if got := mediaTypeOf(&transformer.Operation{RequestMediaType: "application/json"}); got != "application/json" {
		t.Errorf("mediaTypeOf = %q", got)
	}
	if got := envelopeOf(nil); got != "" {
		t.Errorf("envelopeOf(nil) = %q, want empty", got)
	}
	if got := envelopeOf(&transformer.Operation{ResponseEnvelope: "data"}); got != "data" {
		t.Errorf("envelopeOf = %q, want data", got)
	}
}

// TestEnvelopeOfTransformerOp covers the present and absent path/method pairs.
func TestEnvelopeOfTransformerOp(t *testing.T) {
	pathOps := map[string]map[transformer.HTTPMethod]transformer.Operation{
		"/pets": {transformer.MethodGet: {Method: transformer.MethodGet, Path: "/pets", ResponseEnvelope: "data"}},
	}
	if got := envelopeOfTransformerOp(pathOps, "/pets", "GET"); got != "data" {
		t.Errorf("present pair = %q, want data", got)
	}
	if got := envelopeOfTransformerOp(pathOps, "/pets", "POST"); got != "" {
		t.Errorf("absent method = %q, want empty", got)
	}
	if got := envelopeOfTransformerOp(nil, "/pets", "GET"); got != "" {
		t.Errorf("nil map = %q, want empty", got)
	}
}

// TestIsConsumed covers the nil map, present, and absent cases.
func TestIsConsumed(t *testing.T) {
	consumed := map[string]map[string]bool{"/pets": {"GET": true}}
	if !isConsumed(consumed, "/pets", "get") {
		t.Error("consumed pair must match case-insensitively")
	}
	if isConsumed(consumed, "/pets", "POST") {
		t.Error("absent method must not be consumed")
	}
	if isConsumed(consumed, "/other", "GET") {
		t.Error("absent path must not be consumed")
	}
	if isConsumed(nil, "/pets", "GET") {
		t.Error("nil map must not be consumed")
	}
}

// TestResourceName covers the operationId and fallback branches.
func TestResourceName(t *testing.T) {
	if got := resourceName(&parser.Operation{OperationID: "createPet"}, "POST", "/pets"); got != "create_pet" {
		t.Errorf("operationId name = %q, want create_pet", got)
	}
	if got := resourceName(nil, "GET", "/pets/{id}"); got == "" {
		t.Error("fallback name must be non-empty")
	}
}

// TestErrorMappingsFromResponses covers nil op, no error codes, error codes,
// and non-numeric / out-of-range codes.
func TestErrorMappingsFromResponses(t *testing.T) {
	if got := errorMappingsFromResponses(nil); got != nil {
		t.Errorf("nil op = %+v, want nil", got)
	}
	op := &parser.Operation{Responses: map[string]*parser.Response{
		"200":     {Description: "ok"},
		"404":     {Description: "not found"},
		"500":     {Description: "boom"},
		"default": {Description: "fallback"},
		"2xx":     {Description: "range"},
	}}
	m := errorMappingsFromResponses(op)
	if len(m) != 2 {
		t.Fatalf("expected 2 error mappings, got %+v", m)
	}
	if m[404].Description != "not found" || m[500].Description != "boom" {
		t.Errorf("mappings = %+v", m)
	}
	// Only success codes → nil.
	if got := errorMappingsFromResponses(&parser.Operation{Responses: map[string]*parser.Response{"200": {Description: "ok"}}}); got != nil {
		t.Errorf("success-only = %+v, want nil", got)
	}
}

// TestOAuthFlowToIR covers the nil and populated branches.
func TestOAuthFlowToIR(t *testing.T) {
	if got := oauthFlowToIR(nil); got != nil {
		t.Errorf("nil flow = %+v, want nil", got)
	}
	got := oauthFlowToIR(&parser.OAuthFlow{
		AuthorizationURL: "https://api.example.com/oauth/authorize",
		TokenURL:         "https://api.example.com/oauth/token",
		RefreshURL:       "https://api.example.com/oauth/refresh",
		Scopes:           map[string]string{"read": "Read access"},
	})
	if got == nil || got.AuthorizationURL != "https://api.example.com/oauth/authorize" || got.TokenURL != "https://api.example.com/oauth/token" || got.RefreshURL != "https://api.example.com/oauth/refresh" {
		t.Errorf("flow = %+v", got)
	}
	if got.Scopes["read"] != "Read access" {
		t.Errorf("scopes = %+v", got.Scopes)
	}
}

// TestApplySecurityConfigAttributes covers the scheme-mapping error path and
// the duplicate-attribute warning path.
func TestApplySecurityConfigAttributes(t *testing.T) {
	// Unsupported scheme type → error diagnostic, no attributes added.
	preview := &ir.ProviderIR{SecurityIR: ir.SecurityIR{Schemes: []ir.SecuritySchemeIR{
		{Name: "weird", Type: "unsupported"},
	}}}
	var diags diagnostics.Diagnostics
	applySecurityConfigAttributes(preview, &diags)
	if len(diags) != 1 || diags[0].Severity != diagnostics.Error {
		t.Errorf("expected one error diagnostic, got %+v", diags)
	}

	// Two OAuth2 schemes both map client_id → the second is dropped with a
	// warning, and only the first scheme's attributes are added.
	preview2 := &ir.ProviderIR{SecurityIR: ir.SecurityIR{Schemes: []ir.SecuritySchemeIR{
		{Name: "oauth_a", Type: ir.SecuritySchemeOAuth2, Flows: &ir.OAuthFlowsIR{ClientCredentials: &ir.OAuthFlowIR{TokenURL: "https://a/token"}}},
		{Name: "oauth_b", Type: ir.SecuritySchemeOAuth2, Flows: &ir.OAuthFlowsIR{ClientCredentials: &ir.OAuthFlowIR{TokenURL: "https://b/token"}}},
	}}}
	var diags2 diagnostics.Diagnostics
	applySecurityConfigAttributes(preview2, &diags2)
	if len(diags2) != 3 {
		t.Fatalf("expected 3 duplicate warnings (client_id, client_secret, token_url), got %+v", diags2)
	}
	for _, d := range diags2 {
		if d.Severity != diagnostics.Warning {
			t.Errorf("expected warning severity, got %+v", d)
		}
		if !strings.Contains(d.Detail, "oauth_b") {
			t.Errorf("warning must name the dropped scheme, got %q", d.Detail)
		}
	}
	// client_id appears once (the first scheme's).
	count := 0
	for _, a := range preview2.ConfigSchema.Attributes {
		if a.Name == "client_id" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected client_id once, got %d in %+v", count, preview2.ConfigSchema.Attributes)
	}

	// No schemes → no-op.
	var diags3 diagnostics.Diagnostics
	applySecurityConfigAttributes(&ir.ProviderIR{}, &diags3)
	if len(diags3) != 0 {
		t.Errorf("no schemes must be a no-op, got %+v", diags3)
	}
}

// erroringReader is an io.Reader that always fails, so the handler's 400
// read-error path is reachable without a MaxBytesError.
type erroringReader struct{}

func (erroringReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

// TestNewValidateHandler_ErrorPaths covers the handler's error branches that
// require a failing writer or reader: the method-not-allowed write error, the
// 413 write error, the 400 read/write errors, and the response-body write error.
func TestNewValidateHandler_ErrorPaths(t *testing.T) {
	h := NewValidateHandler()

	// Method not allowed with a failing writer → the inner slog error path.
	rec := newFailingResponseWriter(errors.New("write failed"))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/validate", nil)
	h(rec, req)
	if rec.status != http.StatusMethodNotAllowed {
		t.Errorf("method-not-allowed status = %d, want 405", rec.status)
	}

	// Oversized body with a failing writer → the 413 inner error path.
	rec2 := newFailingResponseWriter(errors.New("write failed"))
	big := bytes.Repeat([]byte("x"), maxRequestBodySize+1)
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/validate", bytes.NewReader(big))
	h(rec2, req2)
	if rec2.status != http.StatusRequestEntityTooLarge {
		t.Errorf("413 status = %d, want 413", rec2.status)
	}

	// Erroring body with a failing writer → the 400 read + write error paths.
	rec3 := newFailingResponseWriter(errors.New("write failed"))
	req3 := httptest.NewRequest(http.MethodPost, "/api/v1/validate", erroringReader{})
	h(rec3, req3)
	if rec3.status != http.StatusBadRequest {
		t.Errorf("400 status = %d, want 400", rec3.status)
	}

	// Valid request with a failing writer → the response-body write error path.
	rec4 := newFailingResponseWriter(errors.New("write failed"))
	req4 := httptest.NewRequest(http.MethodPost, "/api/v1/validate", bytes.NewReader([]byte(minimalSpec)))
	h(rec4, req4)
	if rec4.status != http.StatusOK {
		t.Errorf("ok status = %d, want 200", rec4.status)
	}
}

// TestPathParamIRs covers the both-nil guard, the non-path skip, the
// operation-over-path precedence, and the duplicate-name skip.
func TestPathParamIRs(t *testing.T) {
	// Both nil → nil.
	if got := pathParamIRs(nil, "/pets/{id}", nil); got != nil {
		t.Errorf("both nil = %+v, want nil", got)
	}

	spec := &parser.Spec{Paths: map[string]*parser.PathItem{
		"/pets/{id}": {
			Parameters: []parser.Parameter{
				{Name: "petId", In: "path", Required: true, Schema: &parser.Schema{Type: "string"}},
				{Name: "verbose", In: "query", Schema: &parser.Schema{Type: "boolean"}},
			},
		},
	}}
	op := &parser.Operation{
		Parameters: []parser.Parameter{
			{Name: "petId", In: "path", Required: true, Schema: &parser.Schema{Type: "string"}},
			{Name: "X-Trace", In: "header", Schema: &parser.Schema{Type: "string"}},
		},
	}
	got := pathParamIRs(spec, "/pets/{id}", op)
	if len(got) != 1 {
		t.Fatalf("expected 1 path param (operation-level petId wins, path-level duplicate skipped), got %+v", got)
	}
	if got[0].Name != "petId" || got[0].Schema.Type != ir.TypeString {
		t.Errorf("param = %+v", got[0])
	}

	// No operation: path-level params surface, non-path skipped.
	got2 := pathParamIRs(spec, "/pets/{id}", nil)
	if len(got2) != 1 || got2[0].Name != "petId" {
		t.Errorf("path-level only = %+v, want [petId]", got2)
	}
}
