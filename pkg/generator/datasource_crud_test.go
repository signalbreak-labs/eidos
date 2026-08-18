package generator

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// listDataSourceIR returns a wired list data source whose Read response is a
// top-level JSON array of pets, with a `limit` query input and the supplied
// pagination strategy. It exercises the §2/§4 list data source wiring: the
// generator fetches pages via client.ListAllPages and exposes the accumulated
// array as the Computed `items` List attribute.
func listDataSourceIR(pagination *ir.PaginationIR) ir.DataSourceIR {
	return ir.DataSourceIR{
		Name:     "pets",
		TypeName: "mycloud_pets",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "limit",
					Optional: true,
					Schema:   ir.SchemaIR{Type: ir.TypeInt},
				},
				{
					Name:     "items",
					Computed: true,
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind:        ir.List,
							ElementType: ir.SchemaIR{Type: ir.TypeString},
						},
					},
				},
			},
		},
		ReadMapping: ir.OperationMappingIR{
			Method:       "GET",
			PathTemplate: "/pets",
			SuccessCodes: []int{200},
			QueryParams: []ir.ParamIR{
				{Name: "limit", In: "query", Schema: ir.SchemaIR{Type: ir.TypeInt}},
			},
		},
		IsList:     true,
		Pagination: pagination,
	}
}

// sampleProviderWithDataSourceIR returns a minimal provider that configures an
// API client onto its data sources, so a wired list data source compiles inside
// a generated module.
func sampleProviderWithDataSourceIR(ds ir.DataSourceIR) ir.ProviderIR {
	return ir.ProviderIR{
		Name:     "mycloud",
		TypeName: "mycloud",
		ClientIR: ir.ClientIR{
			BaseURLTemplate: "https://api.mycloud.example.com/v1",
		},
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "api_key", Required: true, Sensitive: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
		DataSources: []ir.DataSourceIR{ds},
	}
}

// dataSourceModuleFiles assembles the generated files for a compilable module
// containing the provider, its data sources, the generated client package, and
// (when a data source is wired) the JSON conversion helpers the wired Read
// bodies call.
func dataSourceModuleFiles(t *testing.T, p ir.ProviderIR, cfg BuildConfig) []File {
	t.Helper()
	clientImport := cfg.modulePath() + "/internal/client"
	pf, err := ProviderFileWithClient(p, clientImport)
	if err != nil {
		t.Fatalf("ProviderFileWithClient() error = %v", err)
	}
	files := append(BuildFiles(cfg), pf)
	files = append(files, DataSourceFiles(p.DataSources, clientImport)...)
	files = append(files, ClientFiles(p)...)
	if AnyDataSourceWired(p.DataSources) {
		files = append(files, JSONConvertFile(&p))
	}
	return files
}

// generateWiredDataSourceModule writes the generated module for a provider with
// wired data sources into a temporary directory and returns the module root.
func generateWiredDataSourceModule(t *testing.T, p ir.ProviderIR) string {
	t.Helper()
	tmp := t.TempDir()
	cfg := BuildConfig{ProviderName: p.Name, Namespace: p.Name}
	h := Harness{OutputDir: tmp}
	if err := h.Generate(dataSourceModuleFiles(t, p, cfg)); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	return tmp
}

// TestWiredListDataSource_None_Render asserts the generated list Read body
// fetches a single page (pagination disabled) via client.ListAllPages with a nil
// next callback, encodes the query parameter into url.Values, decodes each page
// into []any, and applies the accumulated items to the model.
func TestWiredListDataSource_None_Render(t *testing.T) {
	ds := listDataSourceIR(nil) // no pagination → single page

	file := DataSourceFile(ds, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		`params := url.Values{}`,
		// Optional query parameters are gated on a non-null value so an unset
		// parameter is omitted rather than sent as the zero value.
		`if !config.Limit.IsNull() {`,
		`params.Set("limit", strconv.FormatInt(config.Limit.ValueInt64(), 10))`,
		// Single-page fetch passes a nil next callback.
		`client.ListAllPages(ctx, params, fetch, nil)`,
		// The fetch closure builds the request through the generated client.
		`d.client.NewRequest(ctx, http.MethodGet, reqPath, nil)`,
		`httpReq.URL.RawQuery = p.Encode()`,
		`d.client.Do(httpReq)`,
		// Accumulation wraps the array as {"items": items} for the model.
		`items := []any{}`,
		`pageItems := []any{}`,
		// Pages decode with a json.Decoder + UseNumber so numeric item fields
		// arrive as json.Number (not float64); applyJSONToModel rejects float64
		// for Int64/Number attributes, which would break list reads against any
		// API whose items carry a number. Regression guard for the list-decode
		// UseNumber fix.
		`dec := json.NewDecoder(bytes.NewReader(page))`,
		`dec.UseNumber()`,
		`dec.Decode(&pageItems)`,
		`items = append(items, pageItems...)`,
		`applyJSONToModel(&config, map[string]any{"items": items})`,
		`resp.State.Set(ctx, &config)`,
		// No scaffold marker: the data source is wired.
		`is not wired to a remote API endpoint`,
	} {
		if want == `is not wired to a remote API endpoint` {
			if strings.Contains(got, want) {
				t.Errorf("wired list data source must not carry scaffold marker, but contains %q\n--- body ---\n%s", want, got)
			}
			continue
		}
		if !strings.Contains(got, want) {
			t.Errorf("generated list body missing %q\n--- body ---\n%s", want, got)
		}
	}
	// Regression guard: the list path must not decode pages with json.Unmarshal,
	// which turns every JSON number into float64 and breaks numeric attributes.
	if strings.Contains(got, "json.Unmarshal(page") {
		t.Errorf("generated list body must decode pages with UseNumber, but contains json.Unmarshal(page)\n--- body ---\n%s", got)
	}
}

// TestWiredListDataSource_Offset_Render asserts the offset pagination next
// callback increments the page parameter and stops on an empty page, and that
// the first page is initialized to 1.
func TestWiredListDataSource_Offset_Render(t *testing.T) {
	ds := listDataSourceIR(&ir.PaginationIR{Style: ir.PaginationStyleOffset, PageParam: "page"})

	file := DataSourceFile(ds, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		// First page initialized to 1.
		`params.Set("page", "1")`,
		// Offset next callback: stop when the page has no items.
		`if len(pageItems) == 0 {`,
		// Advance the page parameter.
		`strconv.Atoi(p.Get("page"))`,
		`p.Set("page", strconv.Itoa(page+1))`,
		// ListAllPages receives the next closure (not nil).
		`client.ListAllPages(ctx, params, fetch, next)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated offset list body missing %q\n--- body ---\n%s", want, got)
		}
	}
}

// TestWiredListDataSource_Offset_DoesNotClobberPage asserts the N-21 fix: the
// offset pagination page param is seeded to "1" only when the practitioner did
// not already supply it. A config `page` attribute wired as a query param must
// win — otherwise every read restarts at page 1 and pages 2+ are silently
// truncated. The generated code guards the Set with a map-presence check.
func TestWiredListDataSource_Offset_DoesNotClobberPage(t *testing.T) {
	ds := listDataSourceIR(&ir.PaginationIR{Style: ir.PaginationStyleOffset, PageParam: "page"})
	// Add a config "page" attribute and wire it as the page query param, so a
	// practitioner can drive pagination from config.
	ds.Schema.Attributes = append(ds.Schema.Attributes, ir.AttributeIR{
		Name:     "page",
		Optional: true,
		Schema:   ir.SchemaIR{Type: ir.TypeInt},
	})
	ds.ReadMapping.QueryParams = append(ds.ReadMapping.QueryParams,
		ir.ParamIR{Name: "page", In: "query", Schema: ir.SchemaIR{Type: ir.TypeInt}},
	)

	file := DataSourceFile(ds, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()
	// The seed must be guarded on the key's absence so the config value wins.
	for _, want := range []string{
		`_, ok := params["page"]`,
		`if !ok {`,
		`params.Set("page", "1")`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated offset list body missing %q\n--- body ---\n%s", want, got)
		}
	}
}

// TestWiredListDataSource_Cursor_Render asserts the cursor next callback reads
// the cursor from the response field and sends it back as the query parameter.
func TestWiredListDataSource_Cursor_Render(t *testing.T) {
	ds := listDataSourceIR(&ir.PaginationIR{Style: ir.PaginationStyleCursor, CursorField: "next_cursor"})

	file := DataSourceFile(ds, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		`page := map[string]any{}`,
		`page["next_cursor"].(string)`,
		`p.Set("next_cursor", cursor)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated cursor list body missing %q\n--- body ---\n%s", want, got)
		}
	}
}

// TestWiredListDataSource_LinkHeader_Render asserts the link_header next callback
// extracts the next URL from the Link header onto the shared nextURL variable.
func TestWiredListDataSource_LinkHeader_Render(t *testing.T) {
	ds := listDataSourceIR(&ir.PaginationIR{Style: ir.PaginationStyleLinkHeader, NextLinkHeader: "next"})

	file := DataSourceFile(ds, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		`client.ExtractLinkHeader(resp.Header.Get("Link"), "next")`,
		`nextURL = `,
		// The fetch closure overrides the request URL with the parsed next URL.
		`httpReq.URL = parsed`,
		`var nextURL string`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated link_header list body missing %q\n--- body ---\n%s", want, got)
		}
	}
}

// TestWiredListDataSource_None_Compiles generates a full provider module with a
// single-page list data source and compiles it.
func TestWiredListDataSource_None_Compiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}
	p := sampleProviderWithDataSourceIR(listDataSourceIR(nil))
	tmp := generateWiredDataSourceModule(t, p)

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
		t.Fatalf("go build ./... failed for list data source (none): %v\n%s", err, out)
	}
}

// TestWiredListDataSource_Offset_Compiles generates a full provider module with
// an offset-paginated list data source and compiles it, proving the offset next
// callback and strconv page advancement are syntactically valid together.
func TestWiredListDataSource_Offset_Compiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}
	p := sampleProviderWithDataSourceIR(listDataSourceIR(&ir.PaginationIR{
		Style:     ir.PaginationStyleOffset,
		PageParam: "page",
	}))
	tmp := generateWiredDataSourceModule(t, p)

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
		t.Fatalf("go build ./... failed for list data source (offset): %v\n%s", err, out)
	}
}

// TestPlanDataSourceWiring_List verifies the wiring plan for a list data source:
// it is wired, marked as a list, requires net/url, and forces strconv for the
// offset pagination style even when the only parameter is a string.
func TestPlanDataSourceWiring_List(t *testing.T) {
	t.Run("none style int param", func(t *testing.T) {
		ds := listDataSourceIR(nil)
		plan := planDataSourceWiring(ds)
		if !plan.wired {
			t.Fatalf("plan.wired = false, want true")
		}
		if !plan.list {
			t.Fatalf("plan.list = false, want true")
		}
		if !plan.needsURL {
			t.Fatalf("plan.needsURL = false, want true")
		}
		// limit is an int query parameter rendered via strconv.
		if !plan.needsStrconv {
			t.Fatalf("plan.needsStrconv = false, want true")
		}
		// No path placeholders, so strings.ReplaceAll is never referenced.
		if plan.needsStrings {
			t.Fatalf("plan.needsStrings = true, want false (no path placeholders)")
		}
	})

	t.Run("offset style string param forces strconv", func(t *testing.T) {
		ds := listDataSourceIR(&ir.PaginationIR{Style: ir.PaginationStyleOffset, PageParam: "page"})
		// Make the only parameter a string so strconv would not otherwise be needed.
		ds.Schema.Attributes[0].Schema = ir.SchemaIR{Type: ir.TypeString}
		ds.ReadMapping.QueryParams[0].Schema = ir.SchemaIR{Type: ir.TypeString}
		plan := planDataSourceWiring(ds)
		if !plan.needsStrconv {
			t.Fatalf("plan.needsStrconv = false, want true for offset pagination next callback")
		}
	})
}

// TestListPaginationConfig_Defaults verifies that a list data source with no
// pagination config defaults to the single-page (none) strategy with the
// generated client's default parameter names.
func TestListPaginationConfig_Defaults(t *testing.T) {
	style, pageParam, cursorField, nextLinkRel := listPaginationConfig(ir.DataSourceIR{})
	if style != ir.PaginationStyleNone {
		t.Fatalf("style = %q, want %q", style, ir.PaginationStyleNone)
	}
	if pageParam != "page" || cursorField != "cursor" || nextLinkRel != "next" {
		t.Fatalf("defaults = (%q, %q, %q), want (page, cursor, next)", pageParam, cursorField, nextLinkRel)
	}
}

// singlePageDataSourceIR returns a wired single-object data source (not a list
// data source) whose Read endpoint carries a `page` query parameter mapped to a
// `page` schema attribute, plus a Computed `name` output attribute. It mirrors the
// Gigamon load_all_* shape: an envelope/single-object response from a paginated
// endpoint, where the data source returns one page only.
func singlePageDataSourceIR() ir.DataSourceIR {
	return ir.DataSourceIR{
		Name:     "roles",
		TypeName: "mycloud_roles",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "page", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
				{Name: "name", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
		ReadMapping: ir.OperationMappingIR{
			Method:       "GET",
			PathTemplate: "/roles",
			SuccessCodes: []int{200},
			QueryParams: []ir.ParamIR{
				{Name: "page", In: "query", Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
	}
}

// TestWiredDataSource_SinglePageWarning_Render asserts that a single-object
// data source whose endpoint carries a pagination query parameter emits a
// non-fatal AddWarning when that argument is unset, so the single-page
// limitation is surfaced rather than silently truncating a paginated endpoint
// (REMAINING_GAPS §4).
func TestWiredDataSource_SinglePageWarning_Render(t *testing.T) {
	ds := singlePageDataSourceIR()

	file := DataSourceFile(ds, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	// The warning is gated on the page argument being null (unset) so a
	// practitioner who explicitly selects a page is not warned.
	if !strings.Contains(got, "if config.Page.IsNull() {") {
		t.Errorf("expected single-page warning gated on config.Page.IsNull(), missing in body:\n%s", got)
	}
	if !strings.Contains(got, "AddWarning") {
		t.Errorf("expected AddWarning for single-page paginated data source, missing in body:\n%s", got)
	}
	if !strings.Contains(got, "Single-page result") {
		t.Errorf("expected 'Single-page result' warning summary, missing in body:\n%s", got)
	}
	if !strings.Contains(got, `\"page\"`) {
		t.Errorf("expected warning to name the page argument, missing in body:\n%s", got)
	}
}

// TestWiredDataSource_NoPaginationParamNoWarning asserts that a single-object
// data source whose endpoint has no pagination query parameter does NOT emit the
// single-page warning (there is no pagination to truncate).
func TestWiredDataSource_NoPaginationParamNoWarning(t *testing.T) {
	ds := singlePageDataSourceIR()
	// Replace the page query param with a non-pagination filter param.
	ds.ReadMapping.QueryParams[0] = ir.ParamIR{Name: "filter", In: "query", Schema: ir.SchemaIR{Type: ir.TypeString}}
	// Keep the schema attribute named for the filter so wiring stays valid.
	ds.Schema.Attributes[0] = ir.AttributeIR{Name: "filter", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}}

	file := DataSourceFile(ds, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if strings.Contains(got, "Single-page result") {
		t.Errorf("non-paginated data source must not emit the single-page warning, but it did:\n%s", got)
	}
}

// arrayQueryDataSourceIR returns a wired single-object data source whose Read
// endpoint carries an `expand` array query parameter modeled as a List of
// strings (transformer.paramSchemaIR for an OpenAPI `type: array, items: string`
// query parameter), plus a Computed `name` output attribute. It exercises the
// collection query-parameter emission: one repeated query.Add per element.
func arrayQueryDataSourceIR() ir.DataSourceIR {
	return ir.DataSourceIR{
		Name:     "account",
		TypeName: "mycloud_account",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "expand",
					Optional: true,
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind:        ir.List,
							ElementType: ir.SchemaIR{Type: ir.TypeString},
						},
					},
				},
				{Name: "name", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
		ReadMapping: ir.OperationMappingIR{
			Method:       "GET",
			PathTemplate: "/account",
			SuccessCodes: []int{200},
			QueryParams: []ir.ParamIR{
				{Name: "expand", In: "query", Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
	}
}

// TestWiredDataSource_ArrayQueryParam_Render asserts that an array query
// parameter (modeled as a List attribute) is serialized as one repeated
// query.Add per element — `?expand=a&expand=b` (OpenAPI form style, explode:
// true) — rather than a single flattened query.Set on a string.
func TestWiredDataSource_ArrayQueryParam_Render(t *testing.T) {
	ds := arrayQueryDataSourceIR()

	file := DataSourceFile(ds, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		// The array query parameter is gated on a non-null List value, then each
		// element is added as a repeated query value via query.Add.
		`if !config.Expand.IsNull() {`,
		`for _, elem := range config.Expand.Elements() {`,
		`query.Add("expand", elem.(types.String).ValueString())`,
		`httpReq.URL.RawQuery = query.Encode()`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated body missing %q\n--- body ---\n%s", want, got)
		}
	}
	// A collection query parameter must never be flattened to a single Set on
	// the List value (types.List has no ValueString accessor and would not
	// compile / would send a single bogus value).
	if strings.Contains(got, `query.Set("expand", config.Expand.ValueString())`) {
		t.Errorf("array query parameter must use repeated query.Add, not a single Set:\n%s", got)
	}
}

// TestWiredDataSource_ArrayQueryParam_Compiles generates a full provider module
// with the array-query-parameter data source and compiles it, proving the range
// loop, the types.String type assertion, and the query.Add call are syntactically
// valid together.
func TestWiredDataSource_ArrayQueryParam_Compiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}
	p := sampleProviderWithDataSourceIR(arrayQueryDataSourceIR())
	tmp := generateWiredDataSourceModule(t, p)

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
		t.Fatalf("go build ./... failed for array-query data source: %v\n%s", err, out)
	}
}
