package generator

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// wiredListResourceIR returns a list resource whose GET /pets list mapping
// resolves: a `status` query filter against the config schema, an `id`
// identity attribute, and a resource schema for the full item. It exercises
// the F1 wiring: the List body fetches pages through the generated client and
// streams one ListResult per item.
func wiredListResourceIR() ir.ListResourceIR {
	rs := ir.ObjectSchemaIR{
		Attributes: []ir.AttributeIR{
			{Name: "id", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			{Name: "name", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		},
	}
	return ir.ListResourceIR{
		Name:     "pets",
		TypeName: "mycloud_pet",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "status", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
		IdentitySchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "id", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
		ResourceSchema: &rs,
		ListMapping: ir.OperationMappingIR{
			Method:       "GET",
			PathTemplate: "/pets",
			SuccessCodes: []int{200},
			QueryParams: []ir.ParamIR{
				{Name: "status", In: "query", Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
	}
}

// TestPlanListResourceWiring verifies the wiring decision: a bodiless list
// mapping with an identity schema and resolvable parameters wires; a list
// resource with no identity attributes or with a request body stays
// scaffolded.
func TestPlanListResourceWiring(t *testing.T) {
	t.Run("resolvable mapping wires", func(t *testing.T) {
		plan := planListResourceWiring(wiredListResourceIR())
		if !plan.wired {
			t.Fatal("plan.wired = false, want true")
		}
		if !plan.hasConfigModel {
			t.Fatal("plan.hasConfigModel = false, want true (status filter attribute)")
		}
	})

	t.Run("no identity attributes stays scaffolded", func(t *testing.T) {
		lr := wiredListResourceIR()
		lr.IdentitySchema = ir.ObjectSchemaIR{}
		if plan := planListResourceWiring(lr); plan.wired {
			t.Fatal("plan.wired = true, want false (no identity attributes)")
		}
	})

	t.Run("request body stays scaffolded", func(t *testing.T) {
		lr := wiredListResourceIR()
		lr.ListMapping.BodySchema = &ir.SchemaIR{Type: ir.TypeString}
		if plan := planListResourceWiring(lr); plan.wired {
			t.Fatal("plan.wired = true, want false (request body cannot be sent)")
		}
	})

	t.Run("unresolvable query param stays scaffolded", func(t *testing.T) {
		lr := wiredListResourceIR()
		lr.ListMapping.QueryParams = append(lr.ListMapping.QueryParams,
			ir.ParamIR{Name: "missing", In: "query", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}})
		if plan := planListResourceWiring(lr); plan.wired {
			t.Fatal("plan.wired = true, want false (required query param with no matching attribute)")
		}
	})
}

// TestListIdentityKeys locks in the identity-key probing order: the wire name
// leads (it is the item object's actual JSON key), then the sanitized attribute
// name, then "id". The wire name leads because the Terraform attribute name
// (e.g. "ship_symbol") need not match the item JSON key (e.g. "symbol").
func TestListIdentityKeys(t *testing.T) {
	t.Run("wire name leads", func(t *testing.T) {
		keys := listIdentityKeys("ship_symbol", "symbol")
		want := []string{"symbol", "ship_symbol", "id"}
		if !equalStrings(keys, want) {
			t.Errorf("listIdentityKeys(ship_symbol, symbol) = %v, want %v", keys, want)
		}
	})

	t.Run("empty wire name falls back to attribute then id", func(t *testing.T) {
		keys := listIdentityKeys("id", "")
		want := []string{"id"}
		if !equalStrings(keys, want) {
			t.Errorf("listIdentityKeys(id, \"\") = %v, want %v", keys, want)
		}
	})

	t.Run("duplicates dropped", func(t *testing.T) {
		keys := listIdentityKeys("id", "id")
		want := []string{"id"}
		if !equalStrings(keys, want) {
			t.Errorf("listIdentityKeys(id, id) = %v, want %v", keys, want)
		}
	})
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestWiredListResource_Render asserts the wired List body: the stream.Results
// closure decodes the filter config, fetches pages via client.ListAllPages,
// probes the identity value from each item, converts it via
// tftypes.ValueFromJSON, populates the resource only under IncludeResource,
// and carries no scaffold marker.
func TestWiredListResource_Render(t *testing.T) {
	lr := wiredListResourceIR()

	file := ListResourceFile(lr, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		// Interface assertions and the client-carrying struct.
		`list.ListResourceWithConfigure`,
		`client *client.Client`,
		// Config model and decode inside the iterator.
		`PetsListResourceModel struct`,
		`req.Config.Get(ctx, &config)`,
		// Client guard through the pushError closure.
		`l.client == nil`,
		// Page fetching.
		// Optional query parameters are gated on a non-null value so an unset
		// parameter is omitted rather than sent as the empty string.
		`if !config.Status.IsNull() {`,
		`params.Set("status", config.Status.ValueString())`,
		`client.ListAllPages(ctx, params, fetch, nil)`,
		`l.client.Do(httpReq)`,
		// Per-item identity probe and conversion.
		`identity := map[string]json.RawMessage{}`,
		`identity["id"] = idValue`,
		`tftypes.ValueFromJSON(idJSON, req.ResourceIdentitySchema.Type().TerraformType(ctx))`,
		`result.Identity.Raw = idVal`,
		// Full resource only when requested, warning on decode failure.
		`if req.IncludeResource {`,
		`AddWarning(`,
		`result.Resource.Raw = resVal`,
		`if !push(result) {`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated wired List body missing %q\n--- body ---\n%s", want, got)
		}
	}
	if strings.Contains(got, "List is not wired to a remote API endpoint") {
		t.Errorf("wired list resource must not carry scaffold marker\n--- body ---\n%s", got)
	}
}

// TestWiredListResource_Pagination asserts the offset pagination strategy is
// threaded through the wired List body: the page parameter starts at 1 and the
// next callback advances it.
func TestWiredListResource_Pagination(t *testing.T) {
	lr := wiredListResourceIR()
	lr.PaginationStyle = ir.PaginationStyleOffset

	file := ListResourceFile(lr, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		`params.Set("page", "1")`,
		`client.ListAllPages(ctx, params, fetch, next)`,
		`next := func(resp *http.Response, body []byte, p url.Values) bool {`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated wired List body (offset pagination) missing %q\n--- body ---\n%s", want, got)
		}
	}
}

// TestWiredListResource_Scaffolded_Render asserts the counterpart: a list
// resource with no identity schema keeps the honest scaffold List body and
// carries no client wiring.
func TestWiredListResource_Scaffolded_Render(t *testing.T) {
	lr := wiredListResourceIR()
	lr.IdentitySchema = ir.ObjectSchemaIR{}

	file := ListResourceFile(lr, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, "List is not wired to a remote API endpoint") {
		t.Errorf("unresolvable list resource must keep the scaffold marker\n--- body ---\n%s", got)
	}
	if strings.Contains(got, "l.client.NewRequest") {
		t.Errorf("unresolvable list resource must not wire the client\n--- body ---\n%s", got)
	}
}

// TestWiredListResource_Compiles generates a full provider module with a wired
// list resource and compiles it, proving the streaming iterator, identity
// conversion, Configure method, and imports are syntactically valid.
func TestWiredListResource_Compiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}
	pir := ir.ProviderIR{
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
		ListResources: []ir.ListResourceIR{wiredListResourceIR()},
	}

	tmp := t.TempDir()
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}
	clientImport := cfg.modulePath() + "/internal/client"
	pf, err := ProviderFileWithClient(pir, clientImport)
	if err != nil {
		t.Fatalf("ProviderFileWithClient() error = %v", err)
	}
	files := append(BuildFiles(cfg), pf)
	files = append(files, ListResourceFiles(pir.ListResources, clientImport)...)
	files = append(files, ClientFiles(pir)...)
	h := Harness{OutputDir: tmp}
	if err := h.Generate(files); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

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
		t.Fatalf("go build ./... failed for wired list resource: %v\n%s", err, out)
	}
}
