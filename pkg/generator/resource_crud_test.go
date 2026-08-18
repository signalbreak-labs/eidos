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

// TestPlanResourceWiring_QueryParamsNoPathNoStrings asserts the M-13 fix:
// query/header/cookie parameters render through url.Values / http.Header /
// strconv — never strings.ReplaceAll, which only path substitution uses. A
// wired resource whose operations carry parameters but no path placeholders
// must not set needsStrings (or the generated provider imports strings unused
// and fails to compile).
func TestPlanResourceWiring_QueryParamsNoPathNoStrings(t *testing.T) {
	r := sampleResourceIR()
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

	sub, ok := resolvePathSubstitution(r, "folder_uid", false, nil)
	if !ok {
		t.Fatalf("expected a substitution for {folder_uid}")
	}
	if sub.field != "Uid" {
		t.Fatalf("expected UID-shaped placeholder to map to the uid attribute, got field %q", sub.field)
	}

	// An exact-name match still wins (uid placeholder -> uid attribute).
	sub, ok = resolvePathSubstitution(r, "uid", false, nil)
	if !ok || sub.field != "Uid" {
		t.Fatalf("expected {uid} to map to uid attribute, got ok=%v field=%q", ok, sub.field)
	}

	// A non-UID placeholder falls back to the numeric id.
	sub, ok = resolvePathSubstitution(r, "folderId", false, nil)
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
	sub, ok := resolvePathSubstitution(r, "apiVersion", true, pathParams)
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
	if _, ok := resolvePathSubstitution(r, "linodeId", true, pathParams); ok {
		t.Fatalf("expected {linodeId} (no static value, composite path) to be unresolvable, got ok=true")
	}

	// A default value is preferred over the enum when both are present
	// (const > default > enum[0] is the priority order in staticPathValue).
	stableAny := any("stable")
	pathParamsDefault := []ir.ParamIR{
		{Name: "ver", In: "path", Schema: ir.SchemaIR{Type: ir.TypeString, Default: &stableAny, EnumValues: []any{"alpha", "stable"}}},
	}
	sub, ok = resolvePathSubstitution(r, "ver", true, pathParamsDefault)
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

	// No match returns "" so identitySetStmts surfaces a runtime diagnostic.
	if got := identityModelField(sampleResourceIR(), ir.AttributeIR{Name: "missing", WireName: "absent"}); got != "" {
		t.Errorf("identityModelField(no match) = %q, want %q", got, "")
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
