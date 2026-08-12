package generator

import (
	"bytes"
	"os/exec"
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

// TestWiredCreateBody_LocationIDFallback asserts the wired Create body falls
// back to the Location header when the response body leaves the string
// identifier unset, and surfaces a clear error when neither is present.
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
		`plan.Id = types.StringValue(loc)`,
		`The create response did not contain an identifier and no Location header was returned`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated create body missing %q\n--- body ---\n%s", want, got)
		}
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

	sub, ok := resolvePathSubstitution(r, "folder_uid", false)
	if !ok {
		t.Fatalf("expected a substitution for {folder_uid}")
	}
	if sub.field != "Uid" {
		t.Fatalf("expected UID-shaped placeholder to map to the uid attribute, got field %q", sub.field)
	}

	// An exact-name match still wins (uid placeholder -> uid attribute).
	sub, ok = resolvePathSubstitution(r, "uid", false)
	if !ok || sub.field != "Uid" {
		t.Fatalf("expected {uid} to map to uid attribute, got ok=%v field=%q", ok, sub.field)
	}

	// A non-UID placeholder falls back to the numeric id.
	sub, ok = resolvePathSubstitution(r, "folderId", false)
	if !ok || sub.field != "Id" {
		t.Fatalf("expected non-UID placeholder to fall back to id, got ok=%v field=%q", ok, sub.field)
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
