package generator

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// wiredEphemeralIR returns a wired ephemeral resource whose Open is a bodiless
// POST /token with a `scope` query input and string/int result attributes. It
// exercises the §4 ephemeral Open wiring: the generator reads the config,
// issues the bodiless request through the generated client, decodes the JSON
// response, and stores the result.
func wiredEphemeralIR() ir.EphemeralResourceIR {
	return ir.EphemeralResourceIR{
		Name:     "token",
		TypeName: "mycloud_token",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "scope", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
		ResultSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "access_token", Computed: true, Sensitive: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
				{Name: "expires_in", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeInt}},
			},
		},
		OpenMapping: ir.OperationMappingIR{
			Method:       "POST",
			PathTemplate: "/token",
			SuccessCodes: []int{200},
			QueryParams: []ir.ParamIR{
				{Name: "scope", In: "query", Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
	}
}

// wiredEphemeralIRWithPath returns a wired ephemeral resource whose Open path
// carries a {name} placeholder resolved to a config attribute, exercising the
// strings.ReplaceAll path-substitution path and the needsStrings import.
func wiredEphemeralIRWithPath() ir.EphemeralResourceIR {
	return ir.EphemeralResourceIR{
		Name:     "secret",
		TypeName: "mycloud_secret",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "name", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
		ResultSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "value", Computed: true, Sensitive: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
		OpenMapping: ir.OperationMappingIR{
			Method:       "GET",
			PathTemplate: "/secrets/{name}",
			SuccessCodes: []int{200},
		},
	}
}

// sampleProviderWithEphemeralIR returns a minimal provider that configures an
// API client onto its ephemeral resources, so a wired ephemeral resource
// compiles inside a generated module.
func sampleProviderWithEphemeralIR(er ir.EphemeralResourceIR) ir.ProviderIR {
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
		EphemeralResources: []ir.EphemeralResourceIR{er},
	}
}

// ephemeralModuleFiles assembles the generated files for a compilable module
// containing the provider, its ephemeral resources, the generated client
// package, and (when an ephemeral resource is wired) the JSON conversion
// helpers the wired Open bodies call.
func ephemeralModuleFiles(t *testing.T, p ir.ProviderIR, cfg BuildConfig) []File {
	t.Helper()
	clientImport := cfg.modulePath() + "/internal/client"
	pf, err := ProviderFileWithClient(p, clientImport)
	if err != nil {
		t.Fatalf("ProviderFileWithClient() error = %v", err)
	}
	files := append(BuildFiles(cfg), pf)
	files = append(files, EphemeralFiles(p.EphemeralResources, clientImport)...)
	files = append(files, ClientFiles(p)...)
	if AnyEphemeralWired(p.EphemeralResources) {
		files = append(files, JSONConvertFile(&p))
	}
	return files
}

// generateWiredEphemeralModule writes the generated module for a provider with
// wired ephemeral resources into a temporary directory and returns the module
// root.
func generateWiredEphemeralModule(t *testing.T, p ir.ProviderIR) string {
	t.Helper()
	tmp := t.TempDir()
	cfg := BuildConfig{ProviderName: p.Name, Namespace: p.Name}
	h := Harness{OutputDir: tmp}
	if err := h.Generate(ephemeralModuleFiles(t, p, cfg)); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	return tmp
}

// TestWiredEphemeralOpen_Render asserts the generated Open body reads the
// config, issues the bodiless request through the generated client, encodes
// the query parameter, decodes the JSON response, and stores the result — and
// carries no scaffold marker.
func TestWiredEphemeralOpen_Render(t *testing.T) {
	er := wiredEphemeralIR()

	file := EphemeralFile(er, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		// Read the config into the merged model (local var named `config` to
		// avoid colliding with the `data` map decodeAndApplyStmts declares).
		`req.Config.Get`,
		// Client guard before the request.
		`e.client == nil`,
		// Bodiless request through the generated client.
		`e.client.NewRequest(ctx, http.MethodPost, reqPath, nil)`,
		// Query parameter encoded onto the request URL.
		`query.Set("scope",`,
		`query.Encode()`,
		`e.client.Do(httpReq)`,
		// Decode the response and apply it to the model.
		`applyJSONToModel(&config, data)`,
		// Store the result.
		`resp.Result.Set(ctx, &config)`,
		// EphemeralResourceWithConfigure is asserted and the Configure method
		// stores the client.
		`EphemeralResourceWithConfigure`,
		`e.client = c`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated wired Open body missing %q\n--- body ---\n%s", want, got)
		}
	}
	if strings.Contains(got, "Open is not wired to a remote API endpoint") {
		t.Errorf("wired ephemeral Open must not carry scaffold marker\n--- body ---\n%s", got)
	}
}

// TestWiredEphemeralOpen_Path_Render asserts the Open body substitutes a path
// placeholder from the config and imports strings for the substitution.
func TestWiredEphemeralOpen_Path_Render(t *testing.T) {
	er := wiredEphemeralIRWithPath()

	file := EphemeralFile(er, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		`reqPath := "/secrets/{name}"`,
		`strings.ReplaceAll(reqPath, "{name}", url.PathEscape(`,
		`e.client.NewRequest(ctx, http.MethodGet, reqPath, nil)`,
		`resp.Result.Set(ctx, &config)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated wired Open (path) body missing %q\n--- body ---\n%s", want, got)
		}
	}
	// strings is imported because of the path substitution.
	if !strings.Contains(got, `"strings"`) {
		t.Errorf("generated wired Open (path) body must import strings\n--- body ---\n%s", got)
	}
}

// TestWiredEphemeralOpen_Compiles generates a full provider module with a wired
// ephemeral resource and compiles it.
func TestWiredEphemeralOpen_Compiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}
	p := sampleProviderWithEphemeralIR(wiredEphemeralIR())
	tmp := generateWiredEphemeralModule(t, p)

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
		t.Fatalf("go build ./... failed for wired ephemeral: %v\n%s", err, out)
	}
}

// TestWiredEphemeralOpen_Path_Compiles generates a full provider module with a
// wired ephemeral resource whose Open carries a path placeholder and compiles
// it, proving the path-substitution + strings import are syntactically valid.
func TestWiredEphemeralOpen_Path_Compiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}
	p := sampleProviderWithEphemeralIR(wiredEphemeralIRWithPath())
	tmp := generateWiredEphemeralModule(t, p)

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
		t.Fatalf("go build ./... failed for wired ephemeral (path): %v\n%s", err, out)
	}
}

// TestPlanEphemeralWiring verifies the wiring plan for an ephemeral resource:
// a bodiless open with a resolvable config attribute is wired; an open with a
// request body or formData stays unwired; and the strings import is needed only
// when the path carries a placeholder.
func TestPlanEphemeralWiring(t *testing.T) {
	t.Run("bodiless query param wired", func(t *testing.T) {
		plan := planEphemeralWiring(wiredEphemeralIR())
		if !plan.wired {
			t.Fatalf("plan.wired = false, want true")
		}
		// No path placeholder and a string query parameter: neither strings nor
		// strconv is referenced.
		if plan.needsStrings {
			t.Fatalf("plan.needsStrings = true, want false (no path placeholders)")
		}
		if plan.needsStrconv {
			t.Fatalf("plan.needsStrconv = true, want false (string param)")
		}
	})

	t.Run("path placeholder needs strings", func(t *testing.T) {
		plan := planEphemeralWiring(wiredEphemeralIRWithPath())
		if !plan.wired {
			t.Fatalf("plan.wired = false, want true")
		}
		if !plan.needsStrings {
			t.Fatalf("plan.needsStrings = false, want true (path placeholder)")
		}
	})

	t.Run("request body not wired", func(t *testing.T) {
		er := wiredEphemeralIR()
		er.OpenMapping.BodySchema = &ir.SchemaIR{}
		if planEphemeralWiring(er).wired {
			t.Fatalf("Open with a request body must not be wired (bodiless only)")
		}
	})

	t.Run("formData not wired", func(t *testing.T) {
		er := wiredEphemeralIR()
		er.OpenMapping.FormDataParams = []ir.ParamIR{{Name: "grant_type", In: "formData", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}}}
		if planEphemeralWiring(er).wired {
			t.Fatalf("Open with formData parameters must not be wired")
		}
	})

	t.Run("no result output not wired", func(t *testing.T) {
		er := wiredEphemeralIR()
		er.ResultSchema = ir.ObjectSchemaIR{}
		if planEphemeralWiring(er).wired {
			t.Fatalf("Open with no result attributes must not be wired")
		}
	})
}

// TestAnyEphemeralWired verifies the gate reports true when at least one
// ephemeral resource is wired and false otherwise, so the provider Configure
// client construction and JSON helpers are only emitted when needed.
func TestAnyEphemeralWired(t *testing.T) {
	if !AnyEphemeralWired([]ir.EphemeralResourceIR{wiredEphemeralIR()}) {
		t.Fatalf("AnyEphemeralWired = false for a wired ephemeral, want true")
	}
	er := wiredEphemeralIR()
	er.OpenMapping.BodySchema = &ir.SchemaIR{}
	if AnyEphemeralWired([]ir.EphemeralResourceIR{er}) {
		t.Fatalf("AnyEphemeralWired = true for an unwired ephemeral, want false")
	}
	if AnyEphemeralWired(nil) {
		t.Fatalf("AnyEphemeralWired = true for no ephemeral resources, want false")
	}
}

// wiredEphemeralIRWithLifecycle returns a wired ephemeral resource whose Open
// path carries a {name} placeholder and whose Renew (POST
// /secrets/{name}/renew) and Close (DELETE /secrets/{name}) mappings resolve
// against the same config attribute. It exercises the F2 lifecycle wiring:
// Open stashes the parameter value in ephemeral private state, and the
// Renew/Close bodies read it back (their requests carry no config).
func wiredEphemeralIRWithLifecycle() ir.EphemeralResourceIR {
	er := wiredEphemeralIRWithPath()
	er.HasRenew = true
	er.RenewMapping = &ir.OperationMappingIR{
		Method:       "POST",
		PathTemplate: "/secrets/{name}/renew",
		SuccessCodes: []int{200},
	}
	er.HasClose = true
	er.CloseMapping = &ir.OperationMappingIR{
		Method:       "DELETE",
		PathTemplate: "/secrets/{name}",
		SuccessCodes: []int{204},
	}
	return er
}

// TestWiredEphemeralRenew_Render asserts Open stashes the lifecycle parameter
// in private state and the Renew body reads it back to call the renew
// endpoint — and carries no scaffold marker.
func TestWiredEphemeralRenew_Render(t *testing.T) {
	er := wiredEphemeralIRWithLifecycle()

	file := EphemeralFile(er, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		// Open stashes the parameter value after the response decode.
		`resp.Private.SetKey(ctx, "eidos.param.Name", []byte(config.Name.ValueString()))`,
		// Renew reads it back and rebuilds the renew path.
		`req.Private.GetKey(ctx, "eidos.param.Name")`,
		`reqPath := "/secrets/{name}/renew"`,
		`strings.ReplaceAll(reqPath, "{name}", url.PathEscape(string(nameBytes)))`,
		`e.client.NewRequest(ctx, http.MethodPost, reqPath, nil)`,
		`e.client.Do(httpReq)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated wired Renew body missing %q\n--- body ---\n%s", want, got)
		}
	}
	if strings.Contains(got, "Renew is not wired to a remote API endpoint") {
		t.Errorf("wired ephemeral Renew must not carry scaffold marker\n--- body ---\n%s", got)
	}
}

// TestWiredEphemeralClose_Render asserts the Close body reads the stashed
// parameter back and calls the close endpoint — and carries no scaffold
// marker.
func TestWiredEphemeralClose_Render(t *testing.T) {
	er := wiredEphemeralIRWithLifecycle()

	file := EphemeralFile(er, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		`req.Private.GetKey(ctx, "eidos.param.Name")`,
		`reqPath := "/secrets/{name}"`,
		`strings.ReplaceAll(reqPath, "{name}", url.PathEscape(string(nameBytes)))`,
		`e.client.NewRequest(ctx, http.MethodDelete, reqPath, nil)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated wired Close body missing %q\n--- body ---\n%s", want, got)
		}
	}
	if strings.Contains(got, "Close is not wired to a remote API endpoint") {
		t.Errorf("wired ephemeral Close must not carry scaffold marker\n--- body ---\n%s", got)
	}
}

// TestWiredEphemeralRenewClose_Render_Scaffolded asserts the counterpart: a
// lifecycle mapping that does not resolve (request body present) keeps the
// honest scaffold Renew/Close bodies even when Open is wired, and Open stashes
// nothing.
func TestWiredEphemeralRenewClose_Render_Scaffolded(t *testing.T) {
	er := wiredEphemeralIRWithPath()
	er.HasRenew = true
	er.RenewMapping = &ir.OperationMappingIR{
		Method:       "POST",
		PathTemplate: "/secrets/{name}/renew",
		BodySchema:   &ir.SchemaIR{Type: ir.TypeString},
	}

	file := EphemeralFile(er, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, "Renew is not wired to a remote API endpoint") {
		t.Errorf("unresolvable Renew mapping must keep the scaffold marker\n--- body ---\n%s", got)
	}
	if strings.Contains(got, "resp.Private.SetKey") {
		t.Errorf("Open must not stash private params when no lifecycle mapping is wired\n--- body ---\n%s", got)
	}
}

// TestWiredEphemeralLifecycle_Compiles generates a full provider module with a
// wired ephemeral resource including Renew/Close and compiles it, proving the
// private-state stash/read-back and the lifecycle request builders are
// syntactically valid.
func TestWiredEphemeralLifecycle_Compiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}
	p := sampleProviderWithEphemeralIR(wiredEphemeralIRWithLifecycle())
	tmp := generateWiredEphemeralModule(t, p)

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
		t.Fatalf("go build ./... failed for wired ephemeral lifecycle: %v\n%s", err, out)
	}
}
