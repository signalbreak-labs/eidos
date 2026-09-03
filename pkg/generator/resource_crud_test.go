package generator

import (
	"bytes"
	"go/format"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// mappingDepthResourceIR returns a wired resource whose Read operation carries
// a declared success code, per-code error mappings, a query parameter, and a
// header parameter — exercising the §2 request/response mapping depth wiring.
// The query and header parameters map to same-normalized schema attributes
// (limit → limit, X-Trace-Id → x_trace_id) so the wired body can supply them.
func mappingDepthResourceIR() ir.ResourceIR {
	r := sampleResourceIR()
	r.Schema.Attributes = append(r.Schema.Attributes,
		ir.AttributeIR{Name: "limit", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeInt}},
		ir.AttributeIR{Name: "x_trace_id", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
	)
	r.CRUDMapping.Read = ir.OperationMappingIR{
		Method:       "GET",
		PathTemplate: "/pets/{id}",
		SuccessCodes: []int{200},
		ErrorMappings: map[int]ir.ErrorMappingIR{
			401: {StatusCode: 401, Description: "Unauthorized"},
			403: {StatusCode: 403, Description: "Forbidden"},
		},
		QueryParams: []ir.ParamIR{
			{Name: "limit", In: "query", Schema: ir.SchemaIR{Type: ir.TypeInt}},
		},
		HeaderParams: []ir.ParamIR{
			{Name: "X-Trace-Id", In: "header", Schema: ir.SchemaIR{Type: ir.TypeString}},
		},
	}
	return r
}

// TestWiredBody_MappingDepth_Render asserts the generated Read body honors the
// operation's SuccessCodes, surfaces per-code diagnostics from ErrorMappings,
// and sends mapped query and header parameters on the request.
func TestWiredBody_MappingDepth_Render(t *testing.T) {
	r := mappingDepthResourceIR()

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		// SuccessCodes: the wired body accepts exactly the declared code.
		`httpResp.StatusCode == 200`,
		// ErrorMappings surface as a per-code switch with the spec descriptions.
		`switch httpResp.StatusCode {`,
		`case 401:`,
		`"Unauthorized"`,
		`case 403:`,
		`"Forbidden"`,
		// The default arm falls through to the generic client error path.
		`client.NewAPIError(httpResp)`,
		// Query parameter is encoded onto the request URL from state.
		`query := httpReq.URL.Query()`,
		`query.Set("limit", strconv.FormatInt(state.Limit.ValueInt64(), 10))`,
		`httpReq.URL.RawQuery = query.Encode()`,
		// Header parameter is set on the request from state, keeping the spec
		// header name even though the attribute is snake_case.
		`httpReq.Header.Set("X-Trace-Id", state.XTraceId.ValueString())`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated body missing %q\n--- body ---\n%s", want, got)
		}
	}
}

// TestWiredBody_MappingDepth_Compiles generates a full provider module with the
// mapping-depth resource and compiles it, proving the status-code switch and
// query/header emission are syntactically valid together.
func TestWiredBody_MappingDepth_Compiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}

	p := sampleProviderWithResourceIR()
	p.Resources = []ir.ResourceIR{mappingDepthResourceIR()}

	tmp := generateResourceModule(t, p)

	ctx, cancel := contextWithTimeout(t, 5*time.Minute)
	defer cancel()

	tidyCmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidyCmd.Dir = tmp
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\n%s", err, out)
	}

	buildCmd := exec.CommandContext(ctx, "go", "build", "./...")
	buildCmd.Dir = tmp
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./... failed for mapping-depth resource: %v\n%s", err, out)
	}
}

// TestPlanOperation_RequiredQueryParamDisablesWiring verifies that a required
// query parameter with no matching schema attribute disables wiring for the
// resource rather than emitting a body that would omit it at runtime.
func TestPlanOperation_RequiredQueryParamDisablesWiring(t *testing.T) {
	r := sampleResourceIR()
	r.CRUDMapping.Read = ir.OperationMappingIR{
		Method:       "GET",
		PathTemplate: "/pets/{id}",
		QueryParams: []ir.ParamIR{
			{Name: "filter", In: "query", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		},
	}
	if plan := planResourceWiring(r); plan.wired {
		t.Errorf("expected wiring disabled when a required query param is unmapped, got wired=true")
	}
}

// TestPlanResourceWiring_QueryParamsNoPathNoStrings asserts the M-13 fix:
// query/header/cookie parameters render through url.Values / http.Header /
// strconv — never strings.ReplaceAll, which only path substitution uses. A
// wired resource whose operations carry parameters but no path placeholders
// must not set needsStrings (or the generated provider imports strings unused
// and fails to compile). The resource uses a non-string identifier so the only
// potential strings usage would come from the query params: a string ID alone
// sets needsStrings via the M-8 Location-header fallback (strings.TrimRight /
// strings.LastIndex), which is orthogonal to this assertion.
func TestPlanResourceWiring_QueryParamsNoPathNoStrings(t *testing.T) {
	r := sampleResourceIR()
	// Override the string id with an int id so the M-8 Location fallback does
	// not contribute a strings dependency.
	for i := range r.Schema.Attributes {
		if r.Schema.Attributes[i].Name == "id" {
			r.Schema.Attributes[i].Schema = ir.SchemaIR{Type: ir.TypeInt}
		}
	}
	query := []ir.ParamIR{
		{Name: "tag", In: "query", Required: false, Schema: ir.SchemaIR{Type: ir.TypeString}},
	}
	// All four operations are placeholder-free (a singleton-style resource); the
	// identifier would be carried out-of-band. Every operation still resolves.
	r.CRUDMapping = ir.CRUDMappingIR{
		Create: ir.OperationMappingIR{Method: "POST", PathTemplate: "/pets", QueryParams: query, SuccessCodes: []int{201}},
		Read:   ir.OperationMappingIR{Method: "GET", PathTemplate: "/pet", QueryParams: query, SuccessCodes: []int{200}},
		Update: &ir.OperationMappingIR{Method: "PUT", PathTemplate: "/pet", QueryParams: query, SuccessCodes: []int{200}},
		Delete: ir.OperationMappingIR{Method: "DELETE", PathTemplate: "/pet", QueryParams: query, SuccessCodes: []int{204}},
	}
	plan := planResourceWiring(r)
	if !plan.wired {
		t.Fatalf("plan.wired = false, want true (placeholder-free templates resolve)")
	}
	if plan.needsStrings {
		t.Fatalf("plan.needsStrings = true, want false (query params never reference strings)")
	}
	if len(plan.create.queryParams) == 0 || len(plan.read.queryParams) == 0 {
		t.Fatalf("expected query params to resolve against the schema, create=%d read=%d", len(plan.create.queryParams), len(plan.read.queryParams))
	}
}

// cookieParamResourceIR returns a wired resource whose Read operation carries a
// cookie parameter mapped to a same-named schema attribute, exercising the §2
// cookie wiring (Cookie header via httpReq.AddCookie).
func cookieParamResourceIR() ir.ResourceIR {
	r := sampleResourceIR()
	r.Schema.Attributes = append(r.Schema.Attributes,
		ir.AttributeIR{Name: "session", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
	)
	r.CRUDMapping.Read = ir.OperationMappingIR{
		Method:       "GET",
		PathTemplate: "/pets/{id}",
		SuccessCodes: []int{200},
		CookieParams: []ir.ParamIR{
			{Name: "session", In: "cookie", Schema: ir.SchemaIR{Type: ir.TypeString}},
		},
	}
	return r
}

// TestWiredBody_CookieParam_Render asserts the wired Read body sends a cookie
// parameter via httpReq.AddCookie, serializing it onto the Cookie header from
// state (REMAINING_GAPS §2).
func TestWiredBody_CookieParam_Render(t *testing.T) {
	r := cookieParamResourceIR()

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	want := `httpReq.AddCookie(&http.Cookie{Name: "session", Value: state.Session.ValueString()})`
	if !strings.Contains(got, want) {
		t.Errorf("generated body missing cookie emission %q\n--- body ---\n%s", want, got)
	}
}

// gatedQueryParamResourceIR returns a wired resource whose Read operation carries
// one optional query parameter (filter) and one required query parameter
// (region), each mapped to a same-named schema attribute. It exercises the
// optional-parameter gating: an unset optional parameter is omitted from the
// request rather than sent as the zero-value empty string.
func gatedQueryParamResourceIR() ir.ResourceIR {
	r := sampleResourceIR()
	r.Schema.Attributes = append(r.Schema.Attributes,
		ir.AttributeIR{Name: "filter", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		ir.AttributeIR{Name: "region", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
	)
	r.CRUDMapping.Read = ir.OperationMappingIR{
		Method:       "GET",
		PathTemplate: "/pets/{id}",
		SuccessCodes: []int{200},
		QueryParams: []ir.ParamIR{
			{Name: "filter", In: "query", Schema: ir.SchemaIR{Type: ir.TypeString}},
			{Name: "region", In: "query", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		},
	}
	return r
}

// TestWiredBody_OptionalQueryParamsAreGated asserts that an optional query
// parameter is emitted inside an "if !state.<field>.IsNull() { ... }" guard so
// an unset optional parameter is omitted from the request, while a required
// query parameter is emitted unguarded (it is always set at apply time).
func TestWiredBody_OptionalQueryParamsAreGated(t *testing.T) {
	r := gatedQueryParamResourceIR()

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	// Optional parameter is guarded against a null model field.
	if !strings.Contains(got, "if !state.Filter.IsNull() {") {
		t.Errorf("optional query param should be gated on !state.Filter.IsNull(), missing in body:\n%s", got)
	}
	if !strings.Contains(got, `query.Set("filter", state.Filter.ValueString())`) {
		t.Errorf("optional query param emission missing in body:\n%s", got)
	}
	// Required parameter is emitted unguarded.
	if !strings.Contains(got, `query.Set("region", state.Region.ValueString())`) {
		t.Errorf("required query param emission missing in body:\n%s", got)
	}
	if strings.Contains(got, "if !state.Region.IsNull() {") {
		t.Errorf("required query param must NOT be gated on IsNull, but a guard was emitted:\n%s", got)
	}
}

// TestPlanOperation_RequiredCookieParamDisablesWiring verifies that a required
// cookie parameter with no matching schema attribute disables wiring, mirroring
// the query/header rule, so the body never omits a required cookie at runtime.
func TestPlanOperation_RequiredCookieParamDisablesWiring(t *testing.T) {
	r := sampleResourceIR()
	r.CRUDMapping.Read = ir.OperationMappingIR{
		Method:       "GET",
		PathTemplate: "/pets/{id}",
		CookieParams: []ir.ParamIR{
			{Name: "session", In: "cookie", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		},
	}
	if plan := planResourceWiring(r); plan.wired {
		t.Errorf("expected wiring disabled when a required cookie param is unmapped, got wired=true")
	}
}

// TestPlanOperation_FormDataParamsDisableWiring verifies that an operation
// carrying formData parameters is not wired: the generated request body only
// encodes JSON, so a form-encoded operation stays honestly scaffolded rather
// than emitting a body with the wrong encoding (REMAINING_GAPS §2).
func TestPlanOperation_FormDataParamsDisableWiring(t *testing.T) {
	r := sampleResourceIR()
	r.CRUDMapping.Read = ir.OperationMappingIR{
		Method:       "GET",
		PathTemplate: "/pets/{id}",
		FormDataParams: []ir.ParamIR{
			{Name: "upload", In: "formData", Schema: ir.SchemaIR{Type: ir.TypeString}},
		},
	}
	if plan := planResourceWiring(r); plan.wired {
		t.Errorf("expected wiring disabled when formData params are present, got wired=true")
	}
}

// TestPlanOperation_CompositeIDNoIDFallback locks in the §3/#12 fix: a composite
// (multi-placeholder) path must resolve each placeholder to a same-named schema
// attribute. The resource-id fallback is suppressed for composite paths, so a
// resource whose response exposes only an "id" attribute (and not the distinct
// path parameters namespace/name) stays honestly scaffolded rather than wiring
// with both placeholders substituted from the same id value.
func TestPlanOperation_CompositeIDNoIDFallback(t *testing.T) {
	r := sampleResourceIR() // schema has id, name, ... but no "namespace" attribute
	r.CRUDMapping.Create = ir.OperationMappingIR{Method: "POST", PathTemplate: "/api/v1/namespaces/{namespace}/pods"}
	r.CRUDMapping.Read = ir.OperationMappingIR{Method: "GET", PathTemplate: "/api/v1/namespaces/{namespace}/pods/{name}"}
	r.CRUDMapping.Delete = ir.OperationMappingIR{Method: "DELETE", PathTemplate: "/api/v1/namespaces/{namespace}/pods/{name}"}
	if plan := planResourceWiring(r); plan.wired {
		t.Errorf("expected composite-id resource with no namespace/name attributes to stay scaffolded, got wired=true")
	}

	// A composite resource whose path parameters ARE top-level attributes wires.
	r2 := sampleResourceIR()
	r2.Schema.Attributes = append(r2.Schema.Attributes,
		ir.AttributeIR{Name: "namespace", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
	)
	// "name" already exists as a Required string attribute on sampleResourceIR.
	r2.CRUDMapping.Create = ir.OperationMappingIR{Method: "POST", PathTemplate: "/api/v1/namespaces/{namespace}/pods"}
	r2.CRUDMapping.Read = ir.OperationMappingIR{Method: "GET", PathTemplate: "/api/v1/namespaces/{namespace}/pods/{name}"}
	r2.CRUDMapping.Delete = ir.OperationMappingIR{Method: "DELETE", PathTemplate: "/api/v1/namespaces/{namespace}/pods/{name}"}
	if plan := planResourceWiring(r2); !plan.wired {
		t.Errorf("expected composite-id resource with namespace+name attributes to wire, got wired=false")
	}
}

// TestPlanOperation_StaticSegmentPlusIDFallback wires a resource whose instance
// path has a shared static versioning segment ({apiVersion}, enum ["v4beta"])
// plus an instance-id placeholder ({channelId}) with no same-named schema
// attribute. Because {apiVersion} resolves to a static literal, only {channelId}
// is dynamic, so the resource-id fallback remains valid and the resource wires
// (the Linode alert_channel case). Without the static-segment exclusion the
// two-placeholder path would be treated as composite and left scaffolded.
func TestPlanOperation_StaticSegmentPlusIDFallback(t *testing.T) {
	versionParam := []ir.ParamIR{
		{Name: "apiVersion", In: "path", Schema: ir.SchemaIR{Type: ir.TypeString, EnumValues: []any{"v4beta"}}},
	}
	r := sampleResourceIR() // has an "id" attribute but no "channel_id" attribute
	r.CRUDMapping.Create = ir.OperationMappingIR{Method: "POST", PathTemplate: "/{apiVersion}/monitor/alert-channels", PathParams: versionParam}
	r.CRUDMapping.Read = ir.OperationMappingIR{Method: "GET", PathTemplate: "/{apiVersion}/monitor/alert-channels/{channelId}", PathParams: append(versionParam, ir.ParamIR{Name: "channelId", In: "path", Schema: ir.SchemaIR{Type: ir.TypeInt}})}
	r.CRUDMapping.Delete = ir.OperationMappingIR{Method: "DELETE", PathTemplate: "/{apiVersion}/monitor/alert-channels/{channelId}", PathParams: append(versionParam, ir.ParamIR{Name: "channelId", In: "path", Schema: ir.SchemaIR{Type: ir.TypeInt}})}
	plan := planResourceWiring(r)
	if !plan.wired {
		t.Fatalf("expected static-segment + id-fallback resource to wire, got wired=false")
	}
	// {channelId} resolves to the resource id field; {apiVersion} to a literal.
	var sawLiteral, sawIDField bool
	for _, sub := range plan.read.subs {
		if sub.placeholder == "apiVersion" && sub.literal == "v4beta" {
			sawLiteral = true
		}
		if sub.placeholder == "channelId" && sub.field == "Id" {
			sawIDField = true
		}
	}
	if !sawLiteral {
		t.Errorf("expected {apiVersion} to resolve to literal %q, subs=%+v", "v4beta", plan.read.subs)
	}
	if !sawIDField {
		t.Errorf("expected {channelId} to resolve to the id attribute, subs=%+v", plan.read.subs)
	}
}

// TestWiredCreateBody_LocationIDFallback asserts the wired Create body falls
// back to the Location header when the response body leaves the string
// identifier unset, and surfaces a clear error when neither is present. The
// fallback extracts the trailing path segment from the Location value (M-8):
// per RFC 7231 the header is an absolute URL or absolute path, not a bare ID.
func TestWiredCreateBody_LocationIDFallback(t *testing.T) {
	r := sampleResourceIR() // id is a string Computed attribute

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		`if plan.Id.IsNull() || plan.Id.IsUnknown() {`,
		`loc := httpResp.Header.Get("Location")`,
		`if loc != "" {`,
		`loc = strings.TrimRight(loc, "/")`,
		`i := strings.LastIndex(loc, "/")`,
		`if i >= 0 {`,
		`loc = loc[i+1:]`,
		`plan.Id = types.StringValue(loc)`,
		`The create response did not contain an identifier and no Location header was returned`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated create body missing %q\n--- body ---\n%s", want, got)
		}
	}
}

// TestWiredCreateBody_LocationIDExtraction_CompilesAndRuns is the M-8
// value-semantics check: it generates a wired resource module, writes a test
// that runs the generated createRemote against a mock HTTP server returning a
// Location header, and asserts the plan ID is the trailing path segment — not
// the raw header value. Before the fix the ID was the entire Location URL.
func TestWiredCreateBody_LocationIDExtraction_CompilesAndRuns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}

	p := sampleProviderWithResourceIR()
	tmp := generateResourceModule(t, p)

	testPath := filepath.Join(tmp, "internal", "provider", "location_id_test.go")
	if err := os.MkdirAll(filepath.Dir(testPath), 0o750); err != nil {
		t.Fatalf("create provider test dir: %v", err)
	}
	content := `package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/mycloud/terraform-provider-mycloud/internal/client"
)

func TestLocationIDExtraction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://api.example.com/pets/123")
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := client.New(client.WithBaseURL(srv.URL))
	r := &PetResource{client: c}
	plan := &PetResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), plan, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	if got := plan.Id.ValueString(); got != "123" {
		t.Fatalf("Id = %q, want %q (trailing path segment, not the raw Location header)", got, "123")
	}
}
`
	if err := os.WriteFile(testPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write location id test: %v", err)
	}

	ctx, cancel := contextWithTimeout(t, 5*time.Minute)
	defer cancel()

	tidyCmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidyCmd.Dir = tmp
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\n%s", err, out)
	}

	testCmd := exec.CommandContext(ctx, "go", "test", "./internal/provider/", "-run", "TestLocationIDExtraction")
	testCmd.Dir = tmp
	if out, err := testCmd.CombinedOutput(); err != nil {
		t.Fatalf("go test ./internal/provider/ failed: %v\n%s", err, out)
	}
}

func TestNestedIdentityCRUD_CompilesAndRuns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}

	bodySchema := &ir.SchemaIR{Attributes: []ir.AttributeIR{
		{Name: "workspace", Required: true, WirePath: "metadata", Schema: ir.SchemaIR{Type: ir.TypeString}},
	}}
	r := ir.ResourceIR{
		Name:        "dashboard",
		TypeName:    "mycloud_dashboard",
		IDAttribute: "uid",
		Schema: ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{
			{Name: "uid", Computed: true, WirePath: "dashboard", Schema: ir.SchemaIR{Type: ir.TypeString}},
			{Name: "workspace", Required: true, WirePath: "metadata", Schema: ir.SchemaIR{Type: ir.TypeString}},
		}},
		CRUDMapping: ir.CRUDMappingIR{
			Create: ir.OperationMappingIR{Method: "POST", PathTemplate: "/workspaces/{workspace}/dashboards", BodySchema: bodySchema, SuccessCodes: []int{201}},
			Read:   ir.OperationMappingIR{Method: "GET", PathTemplate: "/workspaces/{workspace}/dashboards/{uid}", SuccessCodes: []int{200}},
			Delete: ir.OperationMappingIR{Method: "DELETE", PathTemplate: "/workspaces/{workspace}/dashboards/{uid}", SuccessCodes: []int{204}},
		},
	}
	p := sampleProviderWithResourceIR()
	p.Resources = []ir.ResourceIR{r}
	tmp := generateResourceModule(t, p)

	testPath := filepath.Join(tmp, "internal", "provider", "nested_identity_test.go")
	if err := os.MkdirAll(filepath.Dir(testPath), 0o750); err != nil {
		t.Fatalf("create provider test dir: %v", err)
	}
	content := `package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/mycloud/terraform-provider-mycloud/internal/client"
)

func TestNestedIdentityLifecycle(t *testing.T) {
	paths := make(chan string, 3)
	bodies := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		paths <- req.URL.Path
		switch req.Method {
		case http.MethodPost:
			body, _ := io.ReadAll(req.Body)
			bodies <- string(body)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(` + "`" + `{"metadata":{"workspace":"acme"},"dashboard":{"uid":"dash-123"}}` + "`" + `))
		case http.MethodGet:
			_, _ = w.Write([]byte(` + "`" + `{"metadata":{"workspace":"acme"},"dashboard":{"uid":"dash-123"}}` + "`" + `))
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()

	c := client.New(client.WithBaseURL(srv.URL))
	r := &DashboardResource{client: c}
	state := &DashboardResourceModel{Workspace: types.StringValue("acme")}
	createResp := &resource.CreateResponse{}
	r.createRemote(context.Background(), state, createResp)
	if createResp.Diagnostics.HasError() {
		t.Fatalf("create diagnostics: %v", createResp.Diagnostics)
	}
	if got := state.Uid.ValueString(); got != "dash-123" {
		t.Fatalf("Uid = %q, want dash-123", got)
	}
	if got := <-paths; got != "/workspaces/acme/dashboards" {
		t.Fatalf("create path = %q", got)
	}
	if got := <-bodies; got != ` + "`" + `{"metadata":{"workspace":"acme"}}` + "`" + ` {
		t.Fatalf("create body = %s", got)
	}

	readResp := &resource.ReadResponse{}
	if removed := r.readRemote(context.Background(), state, readResp); removed || readResp.Diagnostics.HasError() {
		t.Fatalf("read removed=%v diagnostics=%v", removed, readResp.Diagnostics)
	}
	if got := <-paths; got != "/workspaces/acme/dashboards/dash-123" {
		t.Fatalf("read path = %q", got)
	}

	deleteResp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), state, deleteResp)
	if deleteResp.Diagnostics.HasError() {
		t.Fatalf("delete diagnostics: %v", deleteResp.Diagnostics)
	}
	if got := <-paths; got != "/workspaces/acme/dashboards/dash-123" {
		t.Fatalf("delete path = %q", got)
	}
}
`
	if err := os.WriteFile(testPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write nested identity test: %v", err)
	}

	ctx, cancel := contextWithTimeout(t, 5*time.Minute)
	defer cancel()
	tidyCmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidyCmd.Dir = tmp
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\n%s", err, out)
	}
	testCmd := exec.CommandContext(ctx, "go", "test", "./internal/provider/", "-run", "TestNestedIdentityLifecycle")
	testCmd.Dir = tmp
	if out, err := testCmd.CombinedOutput(); err != nil {
		t.Fatalf("go test ./internal/provider/ failed: %v\n%s", err, out)
	}
}

// TestResolvePathSubstitution_UIDShapedPlaceholder asserts a UID-shaped path
// placeholder ({folder_uid}) is filled with the resource's uid attribute, not
// the numeric id fallback (G19).
func TestResolvePathSubstitution_UIDShapedPlaceholder(t *testing.T) {
	r := ir.ResourceIR{
		Name: "folder",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "id", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeInt}},
				{Name: "uid", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
				{Name: "title", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
	}

	sub, ok := resolvePathSubstitution(r, "folder_uid", false, nil, nil)
	if !ok {
		t.Fatalf("expected a substitution for {folder_uid}")
	}
	if sub.field != "Uid" {
		t.Fatalf("expected UID-shaped placeholder to map to the uid attribute, got field %q", sub.field)
	}

	// An exact-name match still wins (uid placeholder -> uid attribute).
	sub, ok = resolvePathSubstitution(r, "uid", false, nil, nil)
	if !ok || sub.field != "Uid" {
		t.Fatalf("expected {uid} to map to uid attribute, got ok=%v field=%q", ok, sub.field)
	}

	// A non-UID placeholder falls back to the numeric id.
	sub, ok = resolvePathSubstitution(r, "folderId", false, nil, nil)
	if !ok || sub.field != "Id" {
		t.Fatalf("expected non-UID placeholder to fall back to id, got ok=%v field=%q", ok, sub.field)
	}
}

// TestResolvePathSubstitution_StaticVersionSegment verifies that a shared
// path-versioning placeholder (e.g. Linode's {apiVersion}, enum ["v4","v4beta"],
// no default) that has no matching schema attribute is resolved to a static
// literal derived from the path parameter's schema (default, then first enum
// value, then const) rather than disabling wiring for the whole composite-path
// resource. This is what lets eidos wire Linode resources whose instance paths
// are /{apiVersion}/linode/instances/{linodeId}.
func TestResolvePathSubstitution_StaticVersionSegment(t *testing.T) {
	r := ir.ResourceIR{
		Name: "linode_instance",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "id", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeInt}},
				{Name: "label", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
	}
	pathParams := []ir.ParamIR{
		{Name: "apiVersion", In: "path", Schema: ir.SchemaIR{Type: ir.TypeString, EnumValues: []any{"v4", "v4beta"}}},
		{Name: "linodeId", In: "path", Schema: ir.SchemaIR{Type: ir.TypeInt}},
	}

	// {apiVersion} has no matching attribute; with the ID fallback suppressed
	// (composite path) it resolves to the first enum value as a literal.
	sub, ok := resolvePathSubstitution(r, "apiVersion", true, pathParams, nil)
	if !ok {
		t.Fatalf("expected a static substitution for {apiVersion}, got ok=%v", ok)
	}
	if sub.literal != "v4" {
		t.Fatalf("expected literal %q for {apiVersion} (first enum value), got %q", "v4", sub.literal)
	}
	if sub.field != "" {
		t.Fatalf("expected no model field for a static substitution, got field %q", sub.field)
	}

	// {linodeId} has no matching attribute either but its param declares no
	// const/default/enum, so static substitution does not apply and the
	// composite-path guard disables wiring (returns false).
	if _, ok := resolvePathSubstitution(r, "linodeId", true, pathParams, nil); ok {
		t.Fatalf("expected {linodeId} (no static value, composite path) to be unresolvable, got ok=true")
	}

	// A default value is preferred over the enum when both are present
	// (const > default > enum[0] is the priority order in staticPathValue).
	stableAny := any("stable")
	pathParamsDefault := []ir.ParamIR{
		{Name: "ver", In: "path", Schema: ir.SchemaIR{Type: ir.TypeString, Default: &stableAny, EnumValues: []any{"alpha", "stable"}}},
	}
	sub, ok = resolvePathSubstitution(r, "ver", true, pathParamsDefault, nil)
	if !ok || sub.literal != "stable" {
		t.Fatalf("expected default %q to win over enum, got ok=%v literal=%q", "stable", ok, sub.literal)
	}
}

// TestWiredUpdateBody_PreservesStateIntoPlan verifies G20: a wired Update body
// calls preserveStateIntoPlan(&plan, &state) so computed state values (e.g. an
// optimistic-concurrency version) are carried into the request body.
func TestWiredUpdateBody_PreservesStateIntoPlan(t *testing.T) {
	r := sampleResourceIR()
	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "preserveStateIntoPlan(&plan, &state)") {
		t.Errorf("wired Update body must call preserveStateIntoPlan(&plan, &state)\n--- body ---\n%s", got)
	}
}

// TestWiredCreateBody_ResponseInnerPath asserts the wired Create body navigates
// into the response inner path (after the envelope unwrap) before applying the
// body to the model. This handles create responses that nest the created
// resource under a named property alongside side-effect objects (e.g. SpaceTraders
// purchase-ship {data:{ship:{...},transaction:{...},agent:{...}}}).
func TestWiredCreateBody_ResponseInnerPath(t *testing.T) {
	r := sampleResourceIR()
	r.CRUDMapping.Create.ResponseEnvelope = "data"
	r.CRUDMapping.Create.ResponseInnerPath = "ship"

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		`inner, ok := data["ship"]`,
		`im, ok := inner.(map[string]any)`,
		`data = im`,
		`applyJSONToModel(&plan, data)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated create body missing %q\n--- body ---\n%s", want, got)
		}
	}
}

// TestWiredCreateBody_NoInnerPathByDefault asserts a create response with no
// inner path does not emit the inner navigation block (so the common, unnested
// case is unchanged).
func TestWiredCreateBody_NoInnerPathByDefault(t *testing.T) {
	r := sampleResourceIR()
	r.CRUDMapping.Create.ResponseEnvelope = "data"

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if strings.Contains(got, `inner, ok := data[`) {
		t.Errorf("create body must not emit inner navigation when ResponseInnerPath is empty\n--- body ---\n%s", got)
	}
}

// TestWiredCreateBody_ArrayEnvelopeUnwrap asserts the wired Create body unwraps
// a "get one" list wrapper — an envelope whose value is a single-element array
// (e.g. Gigamon {"Policies": [{...}]}) — down to the first element before
// applying the body to the model. The transformer unwraps a single-array
// response wrapper to the item schema for managed resources
// (ManagedResourceSchema), so the decoder must apply the same shape or the
// model fields resolve against the array instead of the item and the
// identifier lookup fails (issue #35).
func TestWiredCreateBody_ArrayEnvelopeUnwrap(t *testing.T) {
	r := sampleResourceIR()
	r.CRUDMapping.Create.ResponseEnvelope = "Policies"

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		`if v, ok := data["Policies"]; ok {`,
		`else if arr, ok := v.([]any); ok && len(arr) > 0 {`,
		`m, ok := arr[0].(map[string]any)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated create body missing %q\n--- body ---\n%s", want, got)
		}
	}
}

// identityResourceIR returns a wired resource paired with a list resource via a
// shared identity schema, mirroring the SpaceTraders ship resource: the
// identity attribute "ship_symbol" is sourced from the model's "symbol" field
// (matched by wire name). A bare "id" attribute exercises the name fallback.
func identityResourceIR() ir.ResourceIR {
	r := sampleResourceIR()
	// The model field the identity is sourced from. Its wire name ("symbol")
	// differs from the identity attribute name ("ship_symbol"), exercising the
	// wire-name match path.
	r.Schema.Attributes = append(r.Schema.Attributes, ir.AttributeIR{
		Name:     "symbol",
		Computed: true,
		WireName: "symbol",
		Schema:   ir.SchemaIR{Type: ir.TypeString},
	})
	r.IdentitySchema = &ir.ObjectSchemaIR{
		Attributes: []ir.AttributeIR{
			{
				Name:     "ship_symbol",
				WireName: "symbol",
				Computed: true,
				Schema:   ir.SchemaIR{Type: ir.TypeString},
			},
		},
	}
	return r
}

// TestWiredCreateBody_IdentitySet asserts a wired Create populates resp.Identity
// from the model so the framework does not reject the response with "Missing
// Resource Identity After Create". The identity attribute is sourced from the
// model field matched by wire name.
func TestWiredCreateBody_IdentitySet(t *testing.T) {
	r := identityResourceIR()

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		`resp.Identity.SetAttribute(ctx, path.Root("ship_symbol"), plan.Symbol)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated create body missing %q\n--- body ---\n%s", want, got)
		}
	}
}

// TestWiredReadBody_IdentitySet asserts a wired Read also sets the identity
// from the refreshed state model (identity is immutable, so it is re-derived
// from the same field after Read).
func TestWiredReadBody_IdentitySet(t *testing.T) {
	r := identityResourceIR()

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, `resp.Identity.SetAttribute(ctx, path.Root("ship_symbol"), state.Symbol)`) {
		t.Errorf("generated read body missing identity SetAttribute from state\n--- body ---\n%s", got)
	}
}

// TestWiredBody_NoIdentityOmitsIdentitySet asserts a resource without an
// identity schema (the common inferred-resource case) never emits identity
// SetAttribute statements, so non-paired resources are unaffected.
func TestWiredBody_NoIdentityOmitsIdentitySet(t *testing.T) {
	r := sampleResourceIR() // no IdentitySchema

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if strings.Contains(got, `resp.Identity.SetAttribute(`) {
		t.Errorf("body must not emit identity SetAttribute when resource has no identity schema\n--- body ---\n%s", got)
	}
}

// TestIdentityModelField asserts the identity-to-model-field match prefers the
// wire name and falls back to the sanitized attribute name.
func TestIdentityModelField(t *testing.T) {
	r := identityResourceIR()

	// Wire-name match: "symbol" wire name resolves to the Symbol model field.
	if got := identityModelField(r, r.IdentitySchema.Attributes[0]); got != "Symbol" {
		t.Errorf("identityModelField(wire name match) = %q, want %q", got, "Symbol")
	}

	// Name fallback: an identity attribute whose wire name has no model match
	// falls back to matching the attribute name against model attribute names.
	r.Schema.Attributes = append(r.Schema.Attributes, ir.AttributeIR{
		Name:   "ship_symbol",
		Schema: ir.SchemaIR{Type: ir.TypeString},
	})
	idAttr := ir.AttributeIR{Name: "ship_symbol", WireName: "nonexistent"}
	if got := identityModelField(r, idAttr); got != "ShipSymbol" {
		t.Errorf("identityModelField(name fallback) = %q, want %q", got, "ShipSymbol")
	}

	// No match, no ID fallback: a resource with no id attribute has nothing to
	// source the identity from, so identityModelField returns "" and
	// identitySetStmts surfaces a runtime diagnostic.
	noID := sampleResourceIR()
	noID.Schema.Attributes = noID.Schema.Attributes[1:] // drop the "id" attribute
	noID.IDAttribute = ""
	if got := identityModelField(noID, ir.AttributeIR{Name: "missing", WireName: "absent"}); got != "" {
		t.Errorf("identityModelField(no match) = %q, want %q", got, "")
	}

	// Path-placeholder fallback: an identity attribute whose wire name has no
	// model match but names the instance path's templated segment resolves
	// through the same substitution the request path uses. port_filter's
	// identity port_id (wire "portId") fills the same {portId} segment the
	// request path fills from the resource's `port` ID attribute.
	portFilter := ir.ResourceIR{
		Name: "port_filter",
		Schema: ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{
			{Name: "port", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			{Name: "cluster_id", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		}},
		CRUDMapping: ir.CRUDMappingIR{
			Create: ir.OperationMappingIR{Method: "POST", PathTemplate: "/portFilters"},
			Read:   ir.OperationMappingIR{Method: "GET", PathTemplate: "/portFilters/{portId}"},
			Delete: ir.OperationMappingIR{Method: "DELETE", PathTemplate: "/portFilters/{portId}"},
		},
		IDAttribute: "port",
	}
	if got := identityModelField(portFilter, ir.AttributeIR{Name: "port_id", WireName: "portId"}); got != "Port" {
		t.Errorf("identityModelField(path placeholder fallback) = %q, want %q", got, "Port")
	}
}

// TestResourceSchema_NestedCollectionPlanModifierKind asserts that a wired
// resource with no usable Update mapping types its nested-collection plan
// modifiers by the attribute's collection kind: a ListNestedAttribute takes
// []planmodifier.List and a MapNestedAttribute []planmodifier.Map, not the
// objectplanmodifier slice its element type would suggest. The plugin framework
// declares PlanModifiers per attribute type, so an Object-typed slice on a
// ListNestedAttribute fails compilation (the gigavuecore pcap_configs
// regression: resource_pcap_profile.go emitted
// "cannot use []planmodifier.Object{…} as []planmodifier.List value").
func TestResourceSchema_NestedCollectionPlanModifierKind(t *testing.T) {
	r := sampleResourceIR()
	// Replace the sample's attributes with a controlled set: a Required
	// primitive, a list of objects, and a map of objects. The sample's
	// SingleNested `owner` is dropped because its []planmodifier.Object output
	// is correct and would mask the negative assertion.
	r.Schema.Attributes = []ir.AttributeIR{
		{Name: "id", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		{Name: "name", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		{
			Name:     "pcap_configs",
			Optional: true,
			Schema: ir.SchemaIR{
				Collection: &ir.CollectionType{
					Kind: ir.List,
					ElementType: ir.SchemaIR{Attributes: []ir.AttributeIR{
						{Name: "alias", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
					}},
				},
			},
		},
		{
			Name:     "rules_by_name",
			Optional: true,
			Schema: ir.SchemaIR{
				Collection: &ir.CollectionType{
					Kind: ir.Map,
					ElementType: ir.SchemaIR{Attributes: []ir.AttributeIR{
						{Name: "value", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
					}},
				},
			},
		},
	}
	// No Update mapping: the unwired-update injection adds RequiresReplace to
	// every config-settable attribute, including the collection attributes.
	r.CRUDMapping.Update = nil

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		`PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()}`,
		`PlanModifiers: []planmodifier.Map{mapplanmodifier.RequiresReplace()}`,
		`listplanmodifier "github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"`,
		`mapplanmodifier "github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated resource missing %q\n--- body ---\n%s", want, got)
		}
	}
	if strings.Contains(got, "objectplanmodifier") {
		t.Errorf("nested collection attributes must not emit objectplanmodifier; the framework types their PlanModifiers by the collection kind\n--- body ---\n%s", got)
	}
}

// TestWiredCreateBody_IdentitySet_Compiles generates a full provider module with
// an identity-bearing wired resource and compiles it, proving the path import is
// registered and the SetAttribute call is syntactically valid.
func TestWiredCreateBody_IdentitySet_Compiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}

	p := sampleProviderWithResourceIR()
	p.Resources = []ir.ResourceIR{identityResourceIR()}

	tmp := generateResourceModule(t, p)

	ctx, cancel := contextWithTimeout(t, 5*time.Minute)
	defer cancel()

	tidyCmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidyCmd.Dir = tmp
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\n%s", err, out)
	}

	buildCmd := exec.CommandContext(ctx, "go", "build", "./...")
	buildCmd.Dir = tmp
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./... failed for identity resource: %v\n%s", err, out)
	}
}

// TestWiredCreateBody_PutAsCreate asserts a PUT-as-create (upsert) resource's
// wired Create body issues http.MethodPut to the instance path with the
// practitioner-supplied identifier substituted into the path placeholder. The
// identifier is Required (the mandatory schema fix that ships with the
// inference), so the path fills with a real value rather than a null id.
func TestWiredCreateBody_PutAsCreate(t *testing.T) {
	r := ir.ResourceIR{
		Name:        "alarm",
		TypeName:    "mycloud_alarm",
		Description: "An alarm resource.",
		IDAttribute: "alarm_id",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "alarm_id", Required: true, WireName: "alarmId", Schema: ir.SchemaIR{Type: ir.TypeString}},
				{Name: "severity", Required: true, WireName: "severity", Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
		CRUDMapping: ir.CRUDMappingIR{
			// PUT-as-create: the instance PUT is Create (and Update); no
			// collection POST exists.
			Create: ir.OperationMappingIR{Method: "PUT", PathTemplate: "/alarms/{alarmId}"},
			Read:   ir.OperationMappingIR{Method: "GET", PathTemplate: "/alarms/{alarmId}"},
			Update: &ir.OperationMappingIR{Method: "PUT", PathTemplate: "/alarms/{alarmId}"},
			Delete: ir.OperationMappingIR{Method: "DELETE", PathTemplate: "/alarms/{alarmId}"},
		},
	}

	// The resource must be wired (Create/Read/Delete all resolve) for the body
	// to be emitted rather than scaffolded.
	plan := planResourceWiring(r)
	if !plan.wired {
		t.Fatalf("PUT-as-create resource should be wired, plan=%+v", plan)
	}

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		`http.MethodPut`,                             // Create issues a PUT, not a POST
		`reqPath := "/alarms/{alarmId}"`,             // instance path, not a collection path
		`url.PathEscape(plan.AlarmId.ValueString())`, // Required identifier fills the path
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated PUT-as-create body missing %q\n--- body ---\n%s", want, got)
		}
	}
	// A PUT-as-create must not fall back to a POST create.
	if strings.Contains(got, `http.MethodPost`) {
		t.Errorf("PUT-as-create body must not emit http.MethodPost\n--- body ---\n%s", got)
	}
}

// TestWiredCreateBody_PutAsCreateComposite asserts a composite-identity
// PUT-as-create resource (an instance path with multiple path parameters, e.g.
// /notifMetaConfig/{notifType}/{taskId}) wires and that the Create body fills
// EVERY path placeholder with a practitioner-supplied Required identifier. The
// path-substitution resolver matches each camelCase placeholder against the
// attribute's WireName (the attr Name is snake_case), and composite paths have
// no single-id fallback, so both identifiers must resolve for the resource to
// wire at all. This mirrors the Gigamon /notifMetaConfig/{notifType}/{taskId}
// case the plan's verification step 2 expects to wire.
func TestWiredCreateBody_PutAsCreateComposite(t *testing.T) {
	r := ir.ResourceIR{
		Name:        "notif_meta_config",
		TypeName:    "upsert_api_notif_meta_config",
		Description: "A composite-id upsert resource.",
		IDAttribute: "id",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "id", Computed: true, WireName: "id", Schema: ir.SchemaIR{Type: ir.TypeString}},
				{Name: "notif_type", Required: true, WireName: "notifType", Schema: ir.SchemaIR{Type: ir.TypeString}},
				{Name: "task_id", Required: true, WireName: "taskId", Schema: ir.SchemaIR{Type: ir.TypeString}},
				{Name: "enabled", Required: true, WireName: "enabled", Schema: ir.SchemaIR{Type: ir.TypeBool}},
			},
		},
		CRUDMapping: ir.CRUDMappingIR{
			Create: ir.OperationMappingIR{Method: "PUT", PathTemplate: "/notification/event/notifMetaConfig/{notifType}/{taskId}"},
			Read:   ir.OperationMappingIR{Method: "GET", PathTemplate: "/notification/event/notifMetaConfig/{notifType}/{taskId}"},
			Update: &ir.OperationMappingIR{Method: "PUT", PathTemplate: "/notification/event/notifMetaConfig/{notifType}/{taskId}"},
			Delete: ir.OperationMappingIR{Method: "DELETE", PathTemplate: "/notification/event/notifMetaConfig/{notifType}/{taskId}"},
		},
	}

	plan := planResourceWiring(r)
	if !plan.wired {
		t.Fatalf("composite PUT-as-create resource should be wired, plan=%+v", plan)
	}

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		`http.MethodPut`,
		`reqPath := "/notification/event/notifMetaConfig/{notifType}/{taskId}"`,
		`url.PathEscape(plan.NotifType.ValueString())`, // first composite id fills its slot
		`url.PathEscape(plan.TaskId.ValueString())`,    // second composite id fills its slot
	} {
		if !strings.Contains(got, want) {
			t.Errorf("composite PUT-as-create body missing %q\n--- body ---\n%s", want, got)
		}
	}
	if strings.Contains(got, `http.MethodPost`) {
		t.Errorf("composite PUT-as-create body must not emit http.MethodPost\n--- body ---\n%s", got)
	}
	if strings.Contains(got, `is not wired to a remote API endpoint`) {
		t.Errorf("composite PUT-as-create must be wired, not scaffolded\n--- body ---\n%s", got)
	}
}

// timeoutsResourceIR returns a fully wired resource with per-operation timeouts
// configured, exercising the M-14 timeouts block, model field, and CRUD wiring.
func timeoutsResourceIR() ir.ResourceIR {
	r := sampleResourceIR()
	create := 20 * time.Minute
	read := 10 * time.Minute
	update := 20 * time.Minute
	deleteTimeout := 10 * time.Minute
	r.Timeouts = &ir.TimeoutConfigIR{
		Create: &create,
		Read:   &read,
		Update: &update,
		Delete: &deleteTimeout,
	}
	return r
}

// TestResourceTimeouts_Render asserts the generated resource file carries the
// timeouts schema block of Int64 seconds attributes, the generated timeouts
// model struct, the time import, and per-operation context timeout wiring in
// each wired CRUD method (M-14).
func TestResourceTimeouts_Render(t *testing.T) {
	r := timeoutsResourceIR()

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		// Schema: the timeouts SingleNestedBlock exposes one Int64 attribute per
		// configured operation.
		`"timeouts": schema.SingleNestedBlock{`,
		`"create": schema.Int64Attribute{`,
		`"read": schema.Int64Attribute{`,
		`"update": schema.Int64Attribute{`,
		`"delete": schema.Int64Attribute{`,
		// Model: the generated timeouts model struct round-trips the block.
		`Timeouts *PetTimeoutsModel`,
		`type PetTimeoutsModel struct {`,
		`Create types.Int64`,
		`tfsdk:"timeouts"`,
		// Imports.
		`"time"`,
		// Create wiring: generator.yaml default, practitioner seconds override,
		// HTTP exchange bounded.
		`timeout := 20 * time.Minute`,
		`if plan.Timeouts != nil && !plan.Timeouts.Create.IsNull() && !plan.Timeouts.Create.IsUnknown() {`,
		`timeout = time.Duration(plan.Timeouts.Create.ValueInt64()) * time.Second`,
		`ctx, cancel := context.WithTimeout(ctx, timeout)`,
		`defer cancel()`,
		// Read wiring.
		`timeout = time.Duration(state.Timeouts.Read.ValueInt64()) * time.Second`,
		// Update wiring.
		`timeout = time.Duration(plan.Timeouts.Update.ValueInt64()) * time.Second`,
		// Delete wiring.
		`timeout = time.Duration(state.Timeouts.Delete.ValueInt64()) * time.Second`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated resource missing %q\n--- body ---\n%s", want, got)
		}
	}

	// The framework-timeouts package and its string-duration block API are gone.
	for _, gone := range []string{
		`terraform-plugin-framework-timeouts`,
		`timeouts.Block(`,
		`timeouts.Value`,
		`Timeouts.Create(ctx,`,
	} {
		if strings.Contains(got, gone) {
			t.Errorf("generated resource unexpectedly contains %q\n--- body ---\n%s", gone, got)
		}
	}
}

// TestResourceNoTimeouts_OmitsBlock asserts a resource without configured
// timeouts emits no timeouts block, model field, or wiring, so the common case
// is unchanged (M-14).
func TestResourceNoTimeouts_OmitsBlock(t *testing.T) {
	r := sampleResourceIR()

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, forbidden := range []string{
		`"timeouts":`,
		`TimeoutsModel`,
		`terraform-plugin-framework-timeouts`,
		`context.WithTimeout`,
	} {
		if strings.Contains(got, forbidden) {
			t.Errorf("resource without timeouts must not emit %q\n--- body ---\n%s", forbidden, got)
		}
	}
}

// TestResourceTimeoutWiringNeedsTime covers the time-import gating: a wired
// resource with any configured op needs time, a scaffolded resource never does,
// and a wired resource whose only configured op is an unwired update does not.
func TestResourceTimeoutWiringNeedsTime(t *testing.T) {
	create := time.Minute
	update := time.Minute

	cases := []struct {
		name   string
		r      ir.ResourceIR
		wiring resourceWiringPlan
		want   bool
	}{
		{"no timeouts", ir.ResourceIR{}, resourceWiringPlan{wired: true, update: true}, false},
		{"scaffolded with timeouts", ir.ResourceIR{Timeouts: &ir.TimeoutConfigIR{Create: &create}}, resourceWiringPlan{wired: false}, false},
		{"wired create", ir.ResourceIR{Timeouts: &ir.TimeoutConfigIR{Create: &create}}, resourceWiringPlan{wired: true, update: true}, true},
		{"wired update only", ir.ResourceIR{Timeouts: &ir.TimeoutConfigIR{Update: &update}}, resourceWiringPlan{wired: true, update: true}, true},
		{"unwired update only", ir.ResourceIR{Timeouts: &ir.TimeoutConfigIR{Update: &update}}, resourceWiringPlan{wired: true, update: false}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resourceTimeoutWiringNeedsTime(tc.r, tc.wiring); got != tc.want {
				t.Errorf("resourceTimeoutWiringNeedsTime() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestGoDurationAST asserts goDurationAST renders the same expressions as
// goDurationExpr, so the resource and client files cannot drift (M-14).
func TestGoDurationAST(t *testing.T) {
	for _, d := range []time.Duration{
		0,
		30 * time.Second,
		10 * time.Minute,
		2 * time.Hour,
		1500 * time.Millisecond,
		time.Duration(123456789),
	} {
		expr := goDurationAST(d)
		var buf bytes.Buffer
		if err := format.Node(&buf, token.NewFileSet(), expr); err != nil {
			t.Fatalf("format.Node(%v) error = %v", d, err)
		}
		if got, want := buf.String(), goDurationExpr(d); got != want {
			t.Errorf("goDurationAST(%v) = %q, want %q", d, got, want)
		}
	}
}

// TestResourceTimeouts_Compiles generates a full provider module with a
// timeouts-configured resource and compiles it, proving the emitted timeouts
// block, model struct, and CRUD wiring are valid against the pinned
// terraform-plugin-framework API (M-14).
func TestResourceTimeouts_Compiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}

	p := sampleProviderWithResourceIR()
	p.Resources = []ir.ResourceIR{timeoutsResourceIR()}

	tmp := generateResourceModule(t, p)

	ctx, cancel := contextWithTimeout(t, 5*time.Minute)
	defer cancel()

	tidyCmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidyCmd.Dir = tmp
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\n%s", err, out)
	}

	// go test -run '^$' compiles every package's test binary (including the
	// emitted *_test.go files) and runs no tests.
	compileCmd := exec.CommandContext(ctx, "go", "test", "-run", "^$", "./...")
	compileCmd.Dir = tmp
	if out, err := compileCmd.CombinedOutput(); err != nil {
		t.Fatalf("go test -run '^$' ./... failed for timeouts resource: %v\n%s", err, out)
	}
}

// collectionReadResourceIR returns a wired resource whose Read is a
// placeholder-free collection GET: the response envelope carries an array of
// every instance (e.g. GigaVUE-FM {"diameterWhitelists": [...]}) and the
// schema is derived from the item.
func collectionReadResourceIR() ir.ResourceIR {
	r := sampleResourceIR()
	r.CRUDMapping.Read = ir.OperationMappingIR{
		Method:               "GET",
		PathTemplate:         "/pets",
		SuccessCodes:         []int{200},
		ResponseEnvelope:     "diameterWhitelists",
		ResponseIsCollection: true,
	}
	return r
}

// TestWiredReadBody_CollectionReadSelectsByID asserts a collection read (a
// placeholder-free GET whose envelope response is an array of every instance)
// selects the element whose identifier matches the state's identifier
// attribute, and reports the resource removed (removed = true) when no
// element matches — instead of blindly applying the first element (G39). A
// null identifier falls back to the first element with a warning.
func TestWiredReadBody_CollectionReadSelectsByID(t *testing.T) {
	r := collectionReadResourceIR()

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		`if v, ok := data["diameterWhitelists"]; ok {`,
		`arr, ok := v.([]any)`,
		`state.Id.IsNull()`,
		`want := fmt.Sprint(state.Id.ValueString())`,
		`for _, item := range arr {`,
		`if idVal, ok := m["id"]; ok && fmt.Sprint(idVal) == want {`,
		`match = m`,
		`if match == nil {`,
		`removed = true`,
		`m, ok := arr[0].(map[string]any)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("collection read body missing %q\n--- body ---\n%s", want, got)
		}
	}
}

// TestWiredReadBody_InstanceReadKeepsFirstElementUnwrap asserts an instance
// read (a placeholder path) keeps the get-one array unwrap: the first element
// IS the instance there, so identifier selection must not be emitted.
func TestWiredReadBody_InstanceReadKeepsFirstElementUnwrap(t *testing.T) {
	r := collectionReadResourceIR()
	r.CRUDMapping.Read.PathTemplate = "/pets/{id}"
	r.CRUDMapping.Read.ResponseIsCollection = false

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, `m, ok := arr[0].(map[string]any)`) {
		t.Errorf("instance read must keep the first-element unwrap\n--- body ---\n%s", got)
	}
	if strings.Contains(got, "removed = true") && strings.Contains(got, "want :=") {
		t.Errorf("instance read must not emit identifier selection\n--- body ---\n%s", got)
	}
}

// TestWiredBody_CollectionRead_Compiles generates a full provider module with
// the collection-read resource and compiles it, proving the selection code is
// valid Go in the generated module.
func TestWiredBody_CollectionRead_Compiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}

	p := sampleProviderWithResourceIR()
	p.Resources = []ir.ResourceIR{collectionReadResourceIR()}

	tmp := generateResourceModule(t, p)

	ctx, cancel := contextWithTimeout(t, 5*time.Minute)
	defer cancel()

	tidyCmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidyCmd.Dir = tmp
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\n%s", err, out)
	}

	buildCmd := exec.CommandContext(ctx, "go", "build", "./...")
	buildCmd.Dir = tmp
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./... failed for collection-read resource: %v\n%s", err, out)
	}
}

// nestedCollectionReadResourceIR returns a child resource whose read is a
// parent GET: the response unwraps to {port, rules: {passRules, dropRules}},
// and the rules collection is located by the read_collection_path "rules.*"
// (the wildcard searches both sibling arrays, since ruleId is unique across
// them). The identifier is rule_id, whose wire name is the spec's ruleId.
func nestedCollectionReadResourceIR() ir.ResourceIR {
	r := sampleResourceIR()
	r.Name = "port_filter_rule"
	r.TypeName = "mycloud_port_filter_rule"
	r.IDAttribute = "rule_id"
	r.Schema.Attributes = []ir.AttributeIR{
		{Name: "rule_id", Computed: true, WireName: "ruleId", Schema: ir.SchemaIR{Type: ir.TypeString}},
		{Name: "port_id", Required: true, WireName: "portId", Schema: ir.SchemaIR{Type: ir.TypeString}},
		{Name: "rule_type", Required: true, WireName: "ruleType", Schema: ir.SchemaIR{Type: ir.TypeString}},
	}
	r.CRUDMapping.Read = ir.OperationMappingIR{
		Method:               "GET",
		PathTemplate:         "/portFilters/{portId}",
		SuccessCodes:         []int{200},
		ResponseEnvelope:     "portFilter",
		ResponseIsCollection: true,
		NestedCollectionPath: "rules.*",
	}
	return r
}

// TestWiredReadBody_NestedCollectionReadSelectsByID asserts a child-resource
// read (a parent GET whose response nests the collection under a
// read_collection_path) unwraps the envelope, navigates the path, collects the
// array(s) the wildcard names, and selects the element whose identifier
// matches the state's identifier attribute — reporting the resource removed
// when no element matches (G39).
func TestWiredReadBody_NestedCollectionReadSelectsByID(t *testing.T) {
	r := nestedCollectionReadResourceIR()

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		`if v, ok := data["portFilter"]; ok {`,
		`m, ok := v.(map[string]any)`,
		`if v, ok := data["rules"]; ok {`,
		`for _, val := range data {`,
		`a, ok := val.([]any)`,
		`arr = append(arr, a...)`,
		`state.RuleId.IsNull()`,
		`want := fmt.Sprint(state.RuleId.ValueString())`,
		`for _, item := range arr {`,
		`if idVal, ok := m["ruleId"]; ok && fmt.Sprint(idVal) == want {`,
		`match = m`,
		`if match == nil {`,
		`removed = true`,
		`m, ok := arr[0].(map[string]any)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("nested collection read body missing %q\n--- body ---\n%s", want, got)
		}
	}
}

// TestWiredBody_NestedCollectionRead_Compiles generates a full provider module
// with the nested-collection-read resource and compiles it, proving the
// navigation and selection code is valid Go in the generated module.
func TestWiredBody_NestedCollectionRead_Compiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}

	p := sampleProviderWithResourceIR()
	p.Resources = []ir.ResourceIR{nestedCollectionReadResourceIR()}

	tmp := generateResourceModule(t, p)

	ctx, cancel := contextWithTimeout(t, 5*time.Minute)
	defer cancel()

	tidyCmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidyCmd.Dir = tmp
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\n%s", err, out)
	}

	buildCmd := exec.CommandContext(ctx, "go", "build", "./...")
	buildCmd.Dir = tmp
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./... failed for nested-collection-read resource: %v\n%s", err, out)
	}
}

// TestWiredCreateBody_ChildResourcePathParams asserts a child resource's create
// fills every path placeholder from the practitioner-supplied Required attribute
// whose WireName matches the placeholder, not the resource-id fallback or a
// static enum value. The port_filter_rule shape: create POSTs to
// /portFilters/{portId}/rules/{ruleType} where port_id (wire portId) and
// rule_type (wire ruleType) are path parameters folded into the schema as
// Required attributes. Before the fix, {portId} fell back to the id attribute
// (rule_id) and {ruleType} was pinned to the first enum member ("pass"), so a
// drop rule would POST to the wrong URL.
func TestWiredCreateBody_ChildResourcePathParams(t *testing.T) {
	r := nestedCollectionReadResourceIR()
	r.CRUDMapping.Create = ir.OperationMappingIR{
		Method:       "POST",
		PathTemplate: "/portFilters/{portId}/rules/{ruleType}",
		SuccessCodes: []int{201},
		PathParams: []ir.ParamIR{
			{Name: "portId", In: "path", Schema: ir.SchemaIR{Type: ir.TypeString}},
			{Name: "ruleType", In: "path", Schema: ir.SchemaIR{Type: ir.TypeString, EnumValues: []any{"pass", "drop"}}},
		},
	}
	r.CRUDMapping.Update = &ir.OperationMappingIR{
		Method:       "PUT",
		PathTemplate: "/portFilters/{portId}/rules/{ruleType}/{ruleId}",
		SuccessCodes: []int{200},
		PathParams: []ir.ParamIR{
			{Name: "portId", In: "path", Schema: ir.SchemaIR{Type: ir.TypeString}},
			{Name: "ruleType", In: "path", Schema: ir.SchemaIR{Type: ir.TypeString, EnumValues: []any{"pass", "drop"}}},
			{Name: "ruleId", In: "path", Schema: ir.SchemaIR{Type: ir.TypeString}},
		},
	}
	r.CRUDMapping.Delete = ir.OperationMappingIR{
		Method:       "DELETE",
		PathTemplate: "/portFilters/{portId}/rules/{ruleId}",
		SuccessCodes: []int{204},
		PathParams: []ir.ParamIR{
			{Name: "portId", In: "path", Schema: ir.SchemaIR{Type: ir.TypeString}},
			{Name: "ruleId", In: "path", Schema: ir.SchemaIR{Type: ir.TypeString}},
		},
	}

	plan := planResourceWiring(r)
	if !plan.wired {
		t.Fatalf("child resource should be wired, plan=%+v", plan)
	}

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		`reqPath := "/portFilters/{portId}/rules/{ruleType}"`,
		`url.PathEscape(plan.PortId.ValueString())`,   // {portId} from port_id, not the id fallback
		`url.PathEscape(plan.RuleType.ValueString())`, // {ruleType} from rule_type, not a static enum value
		`url.PathEscape(plan.RuleId.ValueString())`,   // update {ruleId} from rule_id
	} {
		if !strings.Contains(got, want) {
			t.Errorf("child create body missing %q\n--- body ---\n%s", want, got)
		}
	}
	if strings.Contains(got, `strconv.FormatInt(plan.RuleId.ValueInt64(), 10)`) {
		t.Errorf("child create body must not fill {portId} with the id attribute\n--- body ---\n%s", got)
	}
	if strings.Contains(got, `url.PathEscape("pass")`) {
		t.Errorf("child create body must not pin {ruleType} to a static enum value\n--- body ---\n%s", got)
	}
}

// TestPlanOperation_EnumEquivalentPathParam wires a composite resource whose
// placeholder cannot name-match an attribute but whose path parameter enum
// exactly equals a Required string attribute's enum set (the gigavuecore
// notif_meta_config shape: {notifType} enum [instant, batch, trap] ↔ the
// Required `type` body attribute). Before the fix, {notifType} fell through to
// the static-value fallback and was pinned to the first enum member
// ("instant"), so creates for batch/trap notification types hit the wrong URL;
// with the binding in place the practitioner's `type` configuration supplies
// the segment and {taskId} resolves via its WireName attribute.
func TestPlanOperation_EnumEquivalentPathParam(t *testing.T) {
	enum := []any{"instant", "batch", "trap"}
	params := func(extra ...ir.ParamIR) []ir.ParamIR {
		return append([]ir.ParamIR{{Name: "notifType", In: "path", Schema: ir.SchemaIR{Type: ir.TypeString, EnumValues: enum}}}, extra...)
	}
	r := sampleResourceIR()
	// Mirror the generated notif_meta_config schema: a Required `type`
	// attribute carrying the same enum as {notifType}, and a Required
	// `task_id` attribute whose WireName is the spec's camelCase taskId.
	r.Schema.Attributes = append(r.Schema.Attributes,
		ir.AttributeIR{Name: "type", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString, EnumValues: enum}},
		ir.AttributeIR{Name: "task_id", Required: true, WireName: "taskId", Schema: ir.SchemaIR{Type: ir.TypeString}},
	)
	r.CRUDMapping.Create = ir.OperationMappingIR{Method: "POST", PathTemplate: "/notification/event/notifMetaConfig/{notifType}/{taskId}", PathParams: params(ir.ParamIR{Name: "taskId", In: "path", Schema: ir.SchemaIR{Type: ir.TypeString}})}
	r.CRUDMapping.Read = ir.OperationMappingIR{Method: "GET", PathTemplate: "/notification/event/notifMetaConfig/{notifType}/{taskId}", PathParams: params(ir.ParamIR{Name: "taskId", In: "path", Schema: ir.SchemaIR{Type: ir.TypeString}})}
	r.CRUDMapping.Delete = ir.OperationMappingIR{Method: "DELETE", PathTemplate: "/notification/event/notifMetaConfig/{notifType}/{taskId}", PathParams: params(ir.ParamIR{Name: "taskId", In: "path", Schema: ir.SchemaIR{Type: ir.TypeString}})}
	plan := planResourceWiring(r)
	if !plan.wired {
		t.Fatalf("expected enum-equivalent composite resource to wire, got wired=false")
	}
	var sawType, sawTaskID bool
	for _, sub := range plan.read.subs {
		if sub.placeholder == "notifType" && sub.field == "Type" && sub.literal == "" {
			sawType = true
		}
		if sub.placeholder == "taskId" && sub.field == "TaskId" {
			sawTaskID = true
		}
	}
	if !sawType {
		t.Errorf("expected {notifType} to resolve to the Type attribute, subs=%+v", plan.read.subs)
	}
	if !sawTaskID {
		t.Errorf("expected {taskId} to resolve to the TaskId attribute, subs=%+v", plan.read.subs)
	}

	// The binding must not fire when the enum sets differ: with a disjoint
	// attribute enum the placeholder falls back to the static first-enum
	// member, the pre-existing behavior for versioning segments.
	r2 := sampleResourceIR()
	r2.Schema.Attributes = append(r2.Schema.Attributes,
		ir.AttributeIR{Name: "type", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString, EnumValues: []any{"other"}}},
	)
	r2.CRUDMapping.Create = ir.OperationMappingIR{Method: "POST", PathTemplate: "/x/{notifType}", PathParams: params()}
	r2.CRUDMapping.Read = ir.OperationMappingIR{Method: "GET", PathTemplate: "/x/{notifType}/{id}", PathParams: params()}
	r2.CRUDMapping.Delete = ir.OperationMappingIR{Method: "DELETE", PathTemplate: "/x/{notifType}/{id}", PathParams: params()}
	plan2 := planResourceWiring(r2)
	for _, sub := range plan2.read.subs {
		if sub.placeholder == "notifType" && sub.field != "" {
			t.Errorf("enum mismatch must not bind an attribute, got field=%q", sub.field)
		}
	}
}

// TestEnumEquivalentAttribute_UniqueMatchesOnly asserts the helper rejects
// ambiguity: zero matching attributes and multiple matching attributes both
// resolve to ok=false so the remaining fallbacks decide.
func TestEnumEquivalentAttribute_UniqueMatchesOnly(t *testing.T) {
	enum := []any{"a", "b"}
	pathParams := []ir.ParamIR{{Name: "mode", In: "path", Schema: ir.SchemaIR{Type: ir.TypeString, EnumValues: enum}}}
	mk := func(names ...string) ir.ResourceIR {
		r := sampleResourceIR()
		for _, n := range names {
			r.Schema.Attributes = append(r.Schema.Attributes, ir.AttributeIR{Name: n, Required: true, Schema: ir.SchemaIR{Type: ir.TypeString, EnumValues: enum}})
		}
		return r
	}
	if _, ok := enumEquivalentAttribute(mk(), pathParams, "mode"); ok {
		t.Error("zero matching attributes must not bind")
	}
	if _, ok := enumEquivalentAttribute(mk("left", "right"), pathParams, "mode"); ok {
		t.Error("two matching attributes must not bind")
	}
	attr, ok := enumEquivalentAttribute(mk("kind"), pathParams, "mode")
	if !ok || attr.Name != "kind" {
		t.Errorf("unique match must bind, got ok=%v attr=%+v", ok, attr)
	}
	// Optional attributes and non-string attributes are not candidates.
	r := sampleResourceIR()
	r.Schema.Attributes = append(r.Schema.Attributes,
		ir.AttributeIR{Name: "opt", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString, EnumValues: enum}},
		ir.AttributeIR{Name: "num", Required: true, Schema: ir.SchemaIR{Type: ir.TypeInt, EnumValues: []any{float64(1)}}},
	)
	if _, ok := enumEquivalentAttribute(r, pathParams, "mode"); ok {
		t.Error("optional or non-string attributes must not bind")
	}
	// sameStringEnumSet is order-insensitive and rejects partial overlap.
	if !sameStringEnumSet([]any{"b", "a"}, []any{"a", "b"}) {
		t.Error("order-insensitive equality failed")
	}
	if sameStringEnumSet([]any{"a"}, []any{"a", "b"}) {
		t.Error("different lengths must not be equal")
	}
	if sameStringEnumSet([]any{"a", "a"}, []any{"a", "b"}) {
		t.Error("duplicates vs distinct values must not be equal")
	}
	if sameStringEnumSet([]any{"a", float64(1)}, []any{"a", float64(1)}) {
		t.Error("non-string enum members must not be equal")
	}
}

// TestResolvePathSubstitution_PathParamOverride asserts a path_params override
// wins over the name-match and id-attribute fallbacks: a placeholder that does
// not name-match any attribute and is not the resource id resolves to the
// mapped attribute (e.g. gigavuecore's activation read GET .../{entlItemId}
// filled from the `eli_id` create-body attribute).
func TestResolvePathSubstitution_PathParamOverride(t *testing.T) {
	r := ir.ResourceIR{
		Name: "activation",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "id", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
				{Name: "eli_id", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
				{Name: "aid", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
	}

	// The override maps {entlItemId} to eli_id, which must win over the
	// id-attribute fallback (id).
	sub, ok := resolvePathSubstitution(r, "entlItemId", false, nil, map[string]string{"entlItemId": "eli_id"})
	if !ok {
		t.Fatalf("expected a substitution for {entlItemId} via path_params override")
	}
	if sub.field != "EliId" {
		t.Fatalf("expected path_params override to map {entlItemId} to eli_id, got field %q", sub.field)
	}

	// Without the override the placeholder falls back to the id attribute.
	sub, ok = resolvePathSubstitution(r, "entlItemId", false, nil, nil)
	if !ok || sub.field != "Id" {
		t.Fatalf("expected {entlItemId} to fall back to id without the override, got ok=%v field=%q", ok, sub.field)
	}

	// A placeholder with no override entry keeps the normal resolution.
	sub, ok = resolvePathSubstitution(r, "aid", false, nil, map[string]string{"entlItemId": "eli_id"})
	if !ok || sub.field != "Aid" {
		t.Fatalf("expected {aid} to name-match the aid attribute, got ok=%v field=%q", ok, sub.field)
	}
}
