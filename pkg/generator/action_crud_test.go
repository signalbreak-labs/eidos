package generator

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// wiredActionIR returns a wired action whose Invoke is a bodiless POST against
// /servers/{server_id}/reboot with a `server_id` path input (Required, so it is
// always referenced) and a `reason` query input (Optional). It exercises the
// §4 action wiring: the generator reads the config, substitutes the path
// placeholder, encodes the query parameter, and issues the bodiless request
// through the generated client. An action has no result surface, so the wired
// Invoke neither decodes a response nor sets a result.
func wiredActionIR() ir.ActionIR {
	return ir.ActionIR{
		Name:     "reboot_server",
		TypeName: "mycloud_reboot_server",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "server_id", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
				{Name: "reason", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
		InvokeMapping: ir.OperationMappingIR{
			Method:       "POST",
			PathTemplate: "/servers/{server_id}/reboot",
			SuccessCodes: []int{200, 202},
			QueryParams: []ir.ParamIR{
				{Name: "reason", In: "query", Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
	}
}

// sampleProviderWithWiredActionIR returns a minimal provider that configures
// an API client onto its actions, so a wired action compiles inside a
// generated module.
func sampleProviderWithWiredActionIR(a ir.ActionIR) ir.ProviderIR {
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
		Actions: []ir.ActionIR{a},
	}
}

// actionModuleFiles assembles the generated files for a compilable module
// containing the provider, its actions, the generated client package, and the
// JSON conversion helpers when a wired construct references them. Wired
// actions are bodiless and reference neither modelToJSONMap nor
// applyJSONToModel, so the json_convert.go helper is omitted unless another
// wired construct is present.
func actionModuleFiles(t *testing.T, p ir.ProviderIR, cfg BuildConfig) []File {
	t.Helper()
	clientImport := cfg.modulePath() + "/internal/client"
	pf, err := ProviderFileWithClient(p, clientImport)
	if err != nil {
		t.Fatalf("ProviderFileWithClient() error = %v", err)
	}
	files := append(BuildFiles(cfg), pf)
	files = append(files, ActionFiles(p.Actions, clientImport)...)
	files = append(files, ClientFiles(p)...)
	return files
}

// generateWiredActionModule writes the generated module for a provider with a
// wired action into a temporary directory and returns the module root.
func generateWiredActionModule(t *testing.T, p ir.ProviderIR) string {
	t.Helper()
	tmp := t.TempDir()
	cfg := BuildConfig{ProviderName: p.Name, Namespace: p.Name}
	h := Harness{OutputDir: tmp}
	if err := h.Generate(actionModuleFiles(t, p, cfg)); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	return tmp
}

// TestWiredActionInvoke_Render asserts the generated wired Invoke body reads
// the config, substitutes the path placeholder from the config, encodes the
// query parameter, issues the bodiless request through the generated client,
// and carries no scaffold marker — and that ActionWithConfigure is asserted
// with a Configure method storing the client.
func TestWiredActionInvoke_Render(t *testing.T) {
	a := wiredActionIR()

	file := ActionFile(a, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		// Read the config into the model.
		`req.Config.Get`,
		// Client guard before the request.
		`r.client == nil`,
		// Path template and substitution from the config.
		`reqPath := "/servers/{server_id}/reboot"`,
		`strings.ReplaceAll(reqPath, "{server_id}", url.PathEscape(`,
		// Bodiless request through the generated client (no body argument).
		`r.client.NewRequest(ctx, http.MethodPost, reqPath, nil)`,
		// Query parameter encoded onto the request URL.
		`query.Set("reason"`,
		`query.Encode()`,
		`r.client.Do(httpReq)`,
		// ActionWithConfigure is asserted and the Configure method stores the
		// client.
		`ActionWithConfigure`,
		`r.client = c`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated wired Invoke body missing %q\n--- body ---\n%s", want, got)
		}
	}
	if strings.Contains(got, "Invoke is not wired to a remote API endpoint") {
		t.Errorf("wired action Invoke must not carry scaffold marker\n--- body ---\n%s", got)
	}
}

// TestWiredActionInvoke_ProgressMessages asserts that an action with
// progress_messages: true emits a resp.SendProgress call before the request,
// and that an action without it does not.
func TestWiredActionInvoke_ProgressMessages(t *testing.T) {
	a := wiredActionIR()
	a.ProgressMessages = true

	file := ActionFile(a, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, `resp.SendProgress(action.InvokeProgressEvent{Message: "Invoking mycloud_reboot_server"})`) {
		t.Errorf("generated wired Invoke body missing SendProgress call\n--- body ---\n%s", got)
	}

	// Without progress_messages, no SendProgress call is emitted.
	a.ProgressMessages = false
	file = ActionFile(a, testClientImport)
	buf.Reset()
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if strings.Contains(buf.String(), "SendProgress") {
		t.Errorf("action without progress_messages must not emit SendProgress\n--- body ---\n%s", buf.String())
	}
}

// TestWiredActionInvoke_Compiles generates a full provider module with a wired
// action and compiles it, proving the wired Invoke body, Configure method, and
// client wiring are syntactically valid.
func TestWiredActionInvoke_Compiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}
	p := sampleProviderWithWiredActionIR(wiredActionIR())
	tmp := generateWiredActionModule(t, p)

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
		t.Fatalf("go build ./... failed for wired action: %v\n%s", err, out)
	}
}

// TestWiredActionInvoke_ProgressMessages_Compiles generates a full provider
// module with a wired action that emits SendProgress and compiles it, proving
// the resp.SendProgress call is syntactically valid against the framework's
// action.InvokeResponse.
func TestWiredActionInvoke_ProgressMessages_Compiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}
	a := wiredActionIR()
	a.ProgressMessages = true
	p := sampleProviderWithWiredActionIR(a)
	tmp := generateWiredActionModule(t, p)

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
		t.Fatalf("go build ./... failed for wired action with progress messages: %v\n%s", err, out)
	}
}

// TestPlanActionWiring verifies the action wiring plan: a bodiless invoke with
// resolvable config attributes is wired; an invoke with a request body or
// formData stays unwired; an unmapped required config attribute stays unwired;
// and the strings import is needed only when the path carries a placeholder.
func TestPlanActionWiring(t *testing.T) {
	t.Run("bodiless path+query wired", func(t *testing.T) {
		plan := planActionWiring(wiredActionIR())
		if !plan.wired {
			t.Fatalf("plan.wired = false, want true")
		}
		if !plan.needsStrings {
			t.Fatalf("plan.needsStrings = false, want true (path placeholder)")
		}
		// Both the path and query inputs are strings, so strconv is unused.
		if plan.needsStrconv {
			t.Fatalf("plan.needsStrconv = true, want false (string-only inputs)")
		}
	})

	t.Run("path placeholder needs strings", func(t *testing.T) {
		a := wiredActionIR()
		// Drop the query parameter; the path placeholder still needs strings.
		a.InvokeMapping.QueryParams = nil
		plan := planActionWiring(a)
		if !plan.wired {
			t.Fatalf("plan.wired = false, want true")
		}
		if !plan.needsStrings {
			t.Fatalf("plan.needsStrings = false, want true (path placeholder)")
		}
		if plan.needsStrconv {
			t.Fatalf("plan.needsStrconv = true, want false (string path input)")
		}
	})

	t.Run("non-string param needs strconv", func(t *testing.T) {
		a := wiredActionIR()
		// An integer query input forces strconv (Itoa).
		a.InvokeMapping.QueryParams = []ir.ParamIR{
			{Name: "reason", In: "query", Schema: ir.SchemaIR{Type: ir.TypeInt}},
		}
		// Provide a matching integer config attribute so the param resolves.
		a.ConfigSchema.Attributes = []ir.AttributeIR{
			{Name: "server_id", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			{Name: "reason", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeInt}},
		}
		plan := planActionWiring(a)
		if !plan.wired {
			t.Fatalf("plan.wired = false, want true")
		}
		if !plan.needsStrconv {
			t.Fatalf("plan.needsStrconv = false, want true (int query input)")
		}
	})

	t.Run("JSON request body wired", func(t *testing.T) {
		a := wiredActionIR()
		a.InvokeMapping.BodySchema = &ir.SchemaIR{Type: ir.TypeString}
		a.InvokeMapping.MediaType = "application/json"
		plan := planActionWiring(a)
		if !plan.wired {
			t.Fatalf("Invoke with a JSON request body must be wired")
		}
		if !plan.sendsBody {
			t.Fatalf("plan.sendsBody = false, want true (JSON body encoded from config)")
		}
	})

	t.Run("non-JSON request body not wired", func(t *testing.T) {
		a := wiredActionIR()
		a.InvokeMapping.BodySchema = &ir.SchemaIR{Type: ir.TypeString}
		a.InvokeMapping.MediaType = "application/xml"
		if planActionWiring(a).wired {
			t.Fatalf("Invoke with a non-JSON request body must not be wired (JSON-only client)")
		}
	})

	t.Run("formData not wired", func(t *testing.T) {
		a := wiredActionIR()
		a.InvokeMapping.FormDataParams = []ir.ParamIR{{Name: "reason", In: "formData", Schema: ir.SchemaIR{Type: ir.TypeString}}}
		if planActionWiring(a).wired {
			t.Fatalf("Invoke with formData must not be wired (JSON-only client)")
		}
	})

	t.Run("unresolved path placeholder not wired", func(t *testing.T) {
		a := wiredActionIR()
		// Remove the server_id config attribute so the path placeholder has
		// nothing to resolve against.
		a.ConfigSchema.Attributes = []ir.AttributeIR{
			{Name: "reason", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		}
		if planActionWiring(a).wired {
			t.Fatalf("Invoke with an unresolved path placeholder must not be wired")
		}
	})

	t.Run("unreferenced required attr not wired", func(t *testing.T) {
		a := wiredActionIR()
		// Add a second Required config attribute that no parameter references;
		// the bodiless Invoke would silently drop it, so the action stays
		// honestly scaffolded.
		a.ConfigSchema.Attributes = append(a.ConfigSchema.Attributes, ir.AttributeIR{
			Name: "cluster", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString},
		})
		if planActionWiring(a).wired {
			t.Fatalf("Invoke with an unreferenced Required config attribute must not be wired")
		}
	})
}

// TestAnyActionWired verifies the provider-Configure gate: a provider with at
// least one wired action wires the client, while a provider whose only action
// is scaffolded does not.
func TestAnyActionWired(t *testing.T) {
	if !AnyActionWired([]ir.ActionIR{wiredActionIR()}) {
		t.Fatalf("AnyActionWired = false for a wired action, want true")
	}
	// A fully unwired action (no invoke/modify-plan/validate-config mapping) is
	// scaffolded: AnyActionWired must not wire the provider client for it.
	scaffold := ir.ActionIR{
		Name:     "ping",
		TypeName: "mycloud_ping",
		ConfigSchema: ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{
			{Name: "target", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		}},
	}
	if AnyActionWired([]ir.ActionIR{scaffold}) {
		t.Fatalf("AnyActionWired = true for a scaffolded action, want false")
	}
	if AnyActionWired(nil) {
		t.Fatalf("AnyActionWired = true for no actions, want false")
	}
}

// wiredActionIRWithPreflight returns a wired action that also declares
// resolvable modify_plan_operation and validate_config_operation mappings
// (F3), so its ModifyPlan and ValidateConfig methods wire to the preflight /
// server-side validation endpoints.
func wiredActionIRWithPreflight() ir.ActionIR {
	a := wiredActionIR()
	a.ModifyPlan = true
	mp := ir.OperationMappingIR{
		Method:       "POST",
		PathTemplate: "/servers/{server_id}/reboot/preview",
		SuccessCodes: []int{200},
		QueryParams: []ir.ParamIR{
			{Name: "reason", In: "query", Schema: ir.SchemaIR{Type: ir.TypeString}},
		},
	}
	a.ModifyPlanMapping = &mp
	vc := ir.OperationMappingIR{
		Method:       "POST",
		PathTemplate: "/servers/{server_id}/reboot/validate",
		SuccessCodes: []int{200},
		QueryParams: []ir.ParamIR{
			{Name: "reason", In: "query", Schema: ir.SchemaIR{Type: ir.TypeString}},
		},
	}
	a.ValidateConfigMapping = &vc
	return a
}

// TestWiredActionModifyPlan_Render asserts the ModifyPlan body calls the
// declared preflight endpoint through the generated client and carries no
// scaffold marker.
func TestWiredActionModifyPlan_Render(t *testing.T) {
	a := wiredActionIRWithPreflight()

	file := ActionFile(a, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		`reqPath := "/servers/{server_id}/reboot/preview"`,
		`strings.ReplaceAll(reqPath, "{server_id}", url.PathEscape(config.ServerId.ValueString()))`,
		`r.client.NewRequest(ctx, http.MethodPost, reqPath, nil)`,
		`r.client.Do(httpReq)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated wired ModifyPlan body missing %q\n--- body ---\n%s", want, got)
		}
	}
	if strings.Contains(got, "ModifyPlan is not wired to a remote API endpoint") {
		t.Errorf("wired ModifyPlan must not carry scaffold marker\n--- body ---\n%s", got)
	}
}

// TestWiredActionValidateConfig_Render asserts the ValidateConfig body calls
// the declared server-side validation endpoint through the generated client
// and carries no scaffold marker.
func TestWiredActionValidateConfig_Render(t *testing.T) {
	a := wiredActionIRWithPreflight()

	file := ActionFile(a, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		`reqPath := "/servers/{server_id}/reboot/validate"`,
		`r.client.NewRequest(ctx, http.MethodPost, reqPath, nil)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated wired ValidateConfig body missing %q\n--- body ---\n%s", want, got)
		}
	}
	if strings.Contains(got, "ValidateConfig is not wired to a remote API endpoint") {
		t.Errorf("wired ValidateConfig must not carry scaffold marker\n--- body ---\n%s", got)
	}
}

// TestWiredActionPreflight_UnresolvableMappingOmitsModifyPlan asserts the
// counterpart to the wired preflight render: a declared modify_plan_operation
// mapping that does not resolve (request body present) must not emit a
// ModifyPlan method at all. The optional ActionWithModifyPlan interface is
// implemented only when the mapping resolves — a scaffold ModifyPlan that
// hard-errors would fail every terraform plan for the action (the framework
// invokes ModifyPlan during planning).
func TestWiredActionPreflight_UnresolvableMappingOmitsModifyPlan(t *testing.T) {
	a := wiredActionIR()
	a.ModifyPlan = true
	mp := ir.OperationMappingIR{
		Method:       "POST",
		PathTemplate: "/servers/{server_id}/reboot/preview",
		BodySchema:   &ir.SchemaIR{Type: ir.TypeString},
	}
	a.ModifyPlanMapping = &mp

	file := ActionFile(a, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if strings.Contains(got, "ActionWithModifyPlan") {
		t.Errorf("unresolvable ModifyPlan mapping must not implement ActionWithModifyPlan\n--- body ---\n%s", got)
	}
	if strings.Contains(got, "func (r *RebootServerAction) ModifyPlan") {
		t.Errorf("unresolvable ModifyPlan mapping must not emit a ModifyPlan method\n--- body ---\n%s", got)
	}
	if strings.Contains(got, "ModifyPlan is not wired to a remote API endpoint") {
		t.Errorf("unresolvable ModifyPlan mapping must not emit a scaffold marker\n--- body ---\n%s", got)
	}
	// Invoke still wires: each method's wiring decision is independent.
	if !strings.Contains(got, "r.client.NewRequest") {
		t.Errorf("Invoke must stay wired when only ModifyPlan is unresolvable\n--- body ---\n%s", got)
	}
}

// TestWiredActionPreflight_Compiles generates a full provider module with a
// wired action including ModifyPlan/ValidateConfig preflight bodies and
// compiles it.
func TestWiredActionPreflight_Compiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}
	p := sampleProviderWithWiredActionIR(wiredActionIRWithPreflight())
	tmp := generateWiredActionModule(t, p)

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
		t.Fatalf("go build ./... failed for wired action preflight: %v\n%s", err, out)
	}
}
