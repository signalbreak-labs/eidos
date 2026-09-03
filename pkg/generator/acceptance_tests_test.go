package generator

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// TestResourceAcceptanceTestFile_Render verifies that ResourceAcceptanceTestFile
// emits the expected acceptance test scaffolding.
func TestResourceAcceptanceTestFile_Render(t *testing.T) {
	pir := sampleProviderWithResourceIR()
	r := pir.Resources[0]
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}

	file := ResourceAcceptanceTestFile(pir, r, cfg)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"package provider",
		"func TestAccPetResourceLifecycle",
		"net/http/httptest",
		"func newPetResourceMockServer",
		"func testAccPetResourceConfig",
		"resource.Test",
		"ProtoV6ProviderFactories",
		"ImportState",
		"TF_ACC",
		"mycloud_pet",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated acceptance test missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestResourceAcceptanceTestFile_DynamicAttributeScalarPlaceholder verifies that
// the acceptance test config emits a SCALAR placeholder for a DynamicAttribute,
// never a collection literal. A collection whose element is dynamic degrades to a
// DynamicAttribute; configuring it with a list literal ([ null ]) parses as a
// Tuple whose element types the response mapping (dynamicValueFromRaw) cannot
// reliably reproduce, causing "wrong final value type: tuple required" at apply
// (G18, seen on GitLab protected_branch.allowed_to_merge). A Required Dynamic
// (e.g. GitLab application.scopes / Grafana alert_rule.data) needs a non-null
// scalar ("example"); an Optional Dynamic uses null (G-22).
func TestResourceAcceptanceTestFile_DynamicAttributeScalarPlaceholder(t *testing.T) {
	r := sampleResourceIR()
	r.Schema.Attributes = append(r.Schema.Attributes,
		// Optional collection-of-dynamic: degrades to DynamicAttribute.
		ir.AttributeIR{Name: "allowed", Optional: true, Schema: ir.SchemaIR{
			Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Type: ir.TypeDynamic}},
		}},
		// Required primitive dynamic.
		ir.AttributeIR{Name: "payload", Required: true, Schema: ir.SchemaIR{Type: ir.TypeDynamic}},
	)
	pir := sampleProviderWithResourceIR()
	pir.Resources = []ir.ResourceIR{r}
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}

	file := ResourceAcceptanceTestFile(pir, r, cfg)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	// The config is emitted inside a Go fmt.Sprintf string literal, so HCL
	// double-quotes are escaped as \" in the rendered source.
	if !strings.Contains(got, `payload = \"example\"`) {
		t.Errorf("Required Dynamic should render a string scalar placeholder; got:\n%s", got)
	}
	if !strings.Contains(got, "allowed = null") {
		t.Errorf("Optional collection-degraded Dynamic should render null; got:\n%s", got)
	}
	if strings.Contains(got, "allowed = [") {
		t.Errorf("Optional Dynamic must NOT render a collection literal (Tuple mismatch at apply); got:\n%s", got)
	}
}

// TestResourceAcceptanceTestFile_PractitionerSuppliedIdentity verifies that the
// generated mock does NOT overwrite a Required, practitioner-supplied identity
// attribute with the mock placeholder, and keys state by the identity value so
// create and update share one slot (letting the single-entry read fallback serve
// import). Previously the mock unconditionally set body[idAttr] = "example-id",
// so the echoed response disagreed with the plan ("was \"example\", but now
// \"example-id\"") on GitLab variable.key / Grafana mute_timing.name, and import
// 404'd because create and update landed in separate state slots (G-22).
func TestResourceAcceptanceTestFile_PractitionerSuppliedIdentity(t *testing.T) {
	r := sampleResourceIR()
	// "name" is a Required string attribute; making it the identity models a
	// practitioner-supplied identity (like GitLab variable.key).
	r.IDAttribute = "name"
	pir := sampleProviderWithResourceIR()
	pir.Resources = []ir.ResourceIR{r}
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}

	file := ResourceAcceptanceTestFile(pir, r, cfg)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	// The create handler must synthesize the identity only when absent.
	if !strings.Contains(got, `if _, ok := body["name"]; !ok`) {
		t.Errorf("create should conditionally synthesize the identity (practitioner may supply it); got:\n%s", got)
	}
	// State must be keyed by the identity value, not the bare path-tail
	// placeholder, so create and update share a slot and import's read fallback
	// can serve the created resource.
	if !strings.Contains(got, `id = fmt.Sprintf("%v", body["name"])`) {
		t.Errorf("create should key state by the identity value body[idAttr]; got:\n%s", got)
	}
}

// TestResourceAcceptanceTestFile_WireNameIDLookup verifies the mock inspects,
// synthesizes, and keys state by the identity's wire name when it differs from
// the tfsdk attribute name (camelCase policyName vs snake_case policy_name).
// Generated request bodies are keyed by the wire name, so a presence check on
// the tfsdk name always misses, the mock injects a placeholder over a real
// user-supplied identity, and the test observes the placeholder instead of the
// configured value.
func TestResourceAcceptanceTestFile_WireNameIDLookup(t *testing.T) {
	r := sampleResourceIR()
	// Model the GigaVUE-FM alert_policy shape: the identity is exposed to
	// Terraform as policy_name but the API carries it as policyName.
	r.IDAttribute = "policy_name"
	r.Schema.Attributes = append(r.Schema.Attributes,
		ir.AttributeIR{
			Name:     "policy_name",
			Required: true,
			Schema:   ir.SchemaIR{Type: ir.TypeString},
			WireName: "policyName",
		},
	)
	pir := sampleProviderWithResourceIR()
	pir.Resources = []ir.ResourceIR{r}
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}

	file := ResourceAcceptanceTestFile(pir, r, cfg)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	// The create handler must check and synthesize the identity under the wire
	// name, never the tfsdk name — a request carrying policyName must be
	// recognized as present so its real value is echoed back.
	if !strings.Contains(got, `if _, ok := body["policyName"]; !ok`) {
		t.Errorf("create should key the presence check on the wire name body[%q]; got:\n%s", "policyName", got)
	}
	if strings.Contains(got, `body["policy_name"]`) {
		t.Errorf("mock must not access the body under the tfsdk name policy_name; got:\n%s", got)
	}
	// State must be keyed by the identity value read from the wire key.
	if !strings.Contains(got, `id = fmt.Sprintf("%v", body["policyName"])`) {
		t.Errorf("create should key state by body[%q]; got:\n%s", "policyName", got)
	}
	// Terraform state assertions must stay on the tfsdk attribute name.
	if !strings.Contains(got, `TestCheckResourceAttrSet("mycloud_pet.example", "policy_name")`) {
		t.Errorf("state checks should use the tfsdk attribute name policy_name; got:\n%s", got)
	}
}

// TestResourceAcceptanceTestFile_UpdateKeysStateByIdentity verifies the update
// branch re-derives the state key from the echoed body's identity value
// (body[idKey]) rather than the URL path tail. The path-derived id is the tail
// the client substituted for the update request, which can differ from the
// identity value create stored under (a synthesized placeholder for a Computed
// identity, or the practitioner-supplied value for a Required one). Storing
// update under the same slot as create lets the post-update refresh's direct
// path-tail lookup serve the updated body instead of the stale create body,
// keeping the refresh plan empty (issue #35).
func TestResourceAcceptanceTestFile_UpdateKeysStateByIdentity(t *testing.T) {
	r := sampleResourceIR()
	pir := sampleProviderWithResourceIR()
	pir.Resources = []ir.ResourceIR{r}
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}

	file := ResourceAcceptanceTestFile(pir, r, cfg)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	// sampleResourceIR has a Computed "id" identity, a POST collection create, and
	// a PUT instance update. Both the create and update branches must re-key
	// state by the identity value read from the body; before the fix the update
	// branch keyed state[id] by the path tail only, leaving the stale create body
	// in the create slot for the post-update refresh to serve.
	if n := strings.Count(got, `id = fmt.Sprintf("%v", body["id"])`); n != 2 {
		t.Errorf("expected 2 `id = fmt.Sprintf(\"%%v\", body[\"id\"])` assignments (create + update), got %d\ncontent:\n%s", n, got)
	}
}

// TestAcceptanceParamAttribute_RequiredDynamicSkipped verifies a Required
// Dynamic attribute is never selected as the create/update mutation parameter
// (issue #35): acceptanceExampleValue/updatedValue emit the null literal for a
// Dynamic, and Terraform rejects null for a Required attribute at plan time
// ("Missing Configuration for Required Attribute"). writeHCLAcceptanceAttribute
// instead hardcodes a scalar "example" for a Required Dynamic so it round-trips
// without needing a parameter placeholder, so the param selection must skip it
// and pick a different configurable scalar.
func TestAcceptanceParamAttribute_RequiredDynamicSkipped(t *testing.T) {
	reqDynamic := ir.AttributeIR{
		Name:     "metadata",
		Required: true,
		Schema:   ir.SchemaIR{Type: ir.TypeDynamic},
	}
	strAttr := func(name string, required bool) ir.AttributeIR {
		return ir.AttributeIR{Name: name, Required: required, Optional: !required, Schema: ir.SchemaIR{Type: ir.TypeString}}
	}
	cases := []struct {
		name     string
		attrs    []ir.AttributeIR
		wantName string
		wantOK   bool
	}{
		{
			name: "required dynamic alongside required string",
			attrs: []ir.AttributeIR{
				strAttr("id", true),
				reqDynamic,
				strAttr("name", true),
			},
			wantName: "name",
			wantOK:   true,
		},
		{
			name: "required dynamic alongside optional string",
			attrs: []ir.AttributeIR{
				strAttr("id", true),
				reqDynamic,
				strAttr("tag", false),
			},
			wantName: "tag",
			wantOK:   true,
		},
		{
			name: "only required dynamic is not a candidate",
			attrs: []ir.AttributeIR{
				strAttr("id", true),
				reqDynamic,
			},
			wantName: "",
			wantOK:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := ir.ResourceIR{Schema: ir.ObjectSchemaIR{Attributes: tc.attrs}}
			gotName, gotCreate, gotUpdated, gotOK := acceptanceParamAttribute(r)
			if gotName != tc.wantName || gotOK != tc.wantOK {
				t.Errorf("acceptanceParamAttribute() = (%q, %q, %q, %v), want name %q, ok %v",
					gotName, gotCreate, gotUpdated, gotOK, tc.wantName, tc.wantOK)
			}
			if gotOK && gotCreate == gotUpdated {
				t.Errorf("acceptanceParamAttribute() create %q == updated %q; the update step must vary the value",
					gotCreate, gotUpdated)
			}
		})
	}
}

// TestResourceAcceptanceTestFile_CompositeIdentityImport verifies the mock
// resolves import for a composite-identity resource (e.g. GitLab
// /groups/{group}/labels/{name}). Such a resource is created on a collection
// path but read/updated/deleted on a nested instance path; both share the same
// route prefix (/groups), but the path-tail id differs between create
// ("1/labels") and read ("1/labels/foo"), so a direct state0[id] lookup misses
// and the prior single-entry (len==1) range fallback could not bridge create
// and update once they landed in separate slots. The fix tracks the most recent
// create/update storage key in lastKey and the read falls back to state0[lastKey]
// (G-22).
func TestResourceAcceptanceTestFile_CompositeIdentityImport(t *testing.T) {
	r := sampleResourceIR()
	// Model the GitLab label shape: collection create, nested instance CRUD.
	r.IDAttribute = "name"
	r.Importable = true
	r.ImportIDFormat = "{group}:{name}"
	// Add the composite identity attributes so parseImportIDFormat resolves.
	r.Schema.Attributes = append(r.Schema.Attributes,
		ir.AttributeIR{Name: "group", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
	)
	r.CRUDMapping = ir.CRUDMappingIR{
		Create: ir.OperationMappingIR{Method: "POST", PathTemplate: "/groups/{group}/labels"},
		Read:   ir.OperationMappingIR{Method: "GET", PathTemplate: "/groups/{group}/labels/{name}"},
		Update: &ir.OperationMappingIR{Method: "PUT", PathTemplate: "/groups/{group}/labels/{name}"},
		Delete: ir.OperationMappingIR{Method: "DELETE", PathTemplate: "/groups/{group}/labels/{name}"},
	}
	pir := sampleProviderWithResourceIR()
	pir.Resources = []ir.ResourceIR{r}
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}

	file := ResourceAcceptanceTestFile(pir, r, cfg)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	// lastKey must be declared and initialized to "".
	if !strings.Contains(got, `lastKey0 := ""`) {
		t.Errorf("mock should declare lastKey0 := \"\"; got:\n%s", got)
	}
	// The read handler must fall back to state0[lastKey0] when the direct
	// lookup misses, gated on lastKey0 != "".
	if !strings.Contains(got, `!ok && lastKey0 != ""`) {
		t.Errorf("read fallback should use lastKey0 != \"\"; got:\n%s", got)
	}
	if !strings.Contains(got, `body = state0[lastKey0]`) {
		t.Errorf("read fallback should read state0[lastKey0]; got:\n%s", got)
	}
	// The obsolete single-entry range fallback must be gone.
	if strings.Contains(got, `len(state0) == 1`) {
		t.Errorf("read fallback must not use the obsolete len(state0)==1 range; got:\n%s", got)
	}
	// Both create and update must record lastKey0 = id after storing state.
	// There should be exactly two `lastKey0 = id` assignments (create + update).
	if n := strings.Count(got, `lastKey0 = id`); n != 2 {
		t.Errorf("expected 2 `lastKey0 = id` assignments (create + update), got %d; got:\n%s", n, got)
	}
}

// TestResourceAcceptanceTestFile_MockDeleteClearsState verifies that the generated
// mock server tracks resources by ID, deletes the entry on DELETE, and returns a
// 404 on a subsequent GET for the same ID.
func TestResourceAcceptanceTestFile_MockDeleteClearsState(t *testing.T) {
	pir := sampleProviderWithResourceIR()
	r := pir.Resources[0]
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}

	file := ResourceAcceptanceTestFile(pir, r, cfg)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"state0 := make(map[string]map[string]interface{})",
		"id := strings.Trim(strings.TrimPrefix(r.URL.Path, \"/pets\"), \"/\")",
		"body, ok := state0[id]",
		"http.NotFound(w, r)",
		"delete(state0, id)",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated mock handler missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestResourceAcceptanceTestFile_MockTypedIDAndStatusCodes verifies the mock
// server adapts to the resource ID attribute's primitive type and to the
// spec's declared success codes (G18/G20 hardening). A resource with an
// int64 ID and create success code 200 must yield a mock whose create handler
// writes body["id"] = 1 (an integer literal, decodable by jsonToAttrValue into
// types.Int64) and returns the spec's 200 — not the string "example-id" nor
// the conventional 201, both of which would make the generated client's
// success check or response decode fail.
func TestResourceAcceptanceTestFile_MockTypedIDAndStatusCodes(t *testing.T) {
	pir := sampleProviderWithResourceIR()
	r := pir.Resources[0]
	// Switch the ID to an integer and declare non-conventional success codes so
	// the mock must emit the spec's codes rather than its defaults.
	for i := range r.Schema.Attributes {
		if r.Schema.Attributes[i].Name == "id" {
			r.Schema.Attributes[i].Schema.Type = ir.TypeInt
		}
	}
	r.CRUDMapping.Create.SuccessCodes = []int{200}
	r.CRUDMapping.Read.SuccessCodes = []int{200}
	r.CRUDMapping.Update.SuccessCodes = []int{200}
	r.CRUDMapping.Delete.SuccessCodes = []int{200}
	pir.Resources[0] = r
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}

	file := ResourceAcceptanceTestFile(pir, r, cfg)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		`body["id"] = 1`,
		`id = "1"`,           // state-key default matching the integer ID's string form
		`w.WriteHeader(200)`, // spec-declared create code, not the 201 default
		`ImportStateId: "1"`, // import ID an int64 id attribute can parse back
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated mock missing %q\ncontent:\n%s", want, got)
		}
	}
	for _, absent := range []string{
		`body["id"] = "example-id"`,
		`w.WriteHeader(201)`,
		`w.WriteHeader(204)`,
	} {
		if strings.Contains(got, absent) {
			t.Errorf("generated mock must not contain %q\ncontent:\n%s", absent, got)
		}
	}
}

// TestResourceAcceptanceTestFile_MockRejectsMalformedJSON verifies that the
// generated POST/PUT/PATCH handlers reject malformed request bodies with a 400
// BadRequest instead of silently discarding the decode error.
func TestResourceAcceptanceTestFile_MockRejectsMalformedJSON(t *testing.T) {
	pir := sampleProviderWithResourceIR()
	r := pir.Resources[0]
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}

	file := ResourceAcceptanceTestFile(pir, r, cfg)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"err := json.NewDecoder(r.Body).Decode(&body)",
		"err != nil && err != io.EOF",
		"http.Error(w, err.Error(), http.StatusBadRequest)",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated mock handler missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestResourceAcceptanceTestFile_Lifecycle generates a minimal provider module
// with a managed resource and its acceptance test, then runs the generated test
// to confirm it compiles and passes.
func TestResourceAcceptanceTestFile_Lifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping compile-time acceptance test validation in short mode")
	}
	if os.Getenv("EIDOS_RUN_NETWORK_TESTS") != "1" {
		t.Skip("skipping network-backed acceptance test validation; set EIDOS_RUN_NETWORK_TESTS=1 to run")
	}

	pir := sampleProviderWithResourceIR()
	// The acceptance test passes the mock server URL through the provider's
	// endpoint attribute; the fixture must declare it like the real pipeline
	// (buildIRPreview) does for providers with managed resources.
	pir.ConfigSchema.Attributes = append(pir.ConfigSchema.Attributes, ir.AttributeIR{
		Name:     "endpoint",
		Optional: true,
		Schema:   ir.SchemaIR{Type: ir.TypeString},
	})
	tmp := generateResourceAcceptanceTestModule(t, pir)

	ctx, cancel := contextWithTimeout(t, 5*time.Minute)
	defer cancel()

	tidyCmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidyCmd.Dir = tmp
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\n%s", err, out)
	}

	cmd := exec.CommandContext(ctx, "go", "test", "./internal/provider", "-v", "-run", "TestAccPetResourceLifecycle")
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}

	if !strings.Contains(string(out), "PASS") {
		t.Fatalf("expected acceptance test to pass, got:\n%s", out)
	}
}

// TestResourceAcceptanceTestFile_MalformedImportFormatFailsLoud is the L-26
// regression guard: an importable resource whose ImportIDFormat cannot be
// parsed must surface a generation error from Render rather than silently
// dropping the import test step. Before L-26, acceptanceTestSteps swallowed the
// parse error and emitted a test with no import step, invisibly losing import
// coverage.
func TestResourceAcceptanceTestFile_MalformedImportFormatFailsLoud(t *testing.T) {
	pir := sampleProviderWithResourceIR()
	r := pir.Resources[0]
	r.Importable = true
	// Inconsistent composite delimiters are rejected by parseImportIDFormat.
	r.ImportIDFormat = "{project_id}:{resource_id}/{other_id}"
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}

	file := ResourceAcceptanceTestFile(pir, r, cfg)
	var buf bytes.Buffer
	err := file.Render(&buf)
	if err == nil {
		t.Fatalf("Render() error = nil, want a generation error for malformed ImportIDFormat; output:\n%s", buf.String())
	}
	if !strings.Contains(err.Error(), "acceptance import step") {
		t.Errorf("error %q does not mention the acceptance import step", err)
	}
	if buf.Len() != 0 {
		t.Errorf("ErrorFile should render no bytes, got %d bytes", buf.Len())
	}
}

// TestResourceAcceptanceTestFiles_Multiple verifies that ResourceAcceptanceTestFiles
// emits one acceptance test file per wired resource with deterministic paths.
func TestResourceAcceptanceTestFiles_Multiple(t *testing.T) {
	pir := ir.ProviderIR{
		Name:     "mycloud",
		TypeName: "mycloud",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "api_key", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
				{Name: "endpoint", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
		Resources: []ir.ResourceIR{
			sampleResourceIR(),
			func() ir.ResourceIR {
				r := sampleResourceIR()
				r.Name = "owner"
				r.TypeName = "mycloud_owner"
				return r
			}(),
		},
	}
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}

	files := ResourceAcceptanceTestFiles(pir, cfg)
	if len(files) != len(pir.Resources) {
		t.Fatalf("ResourceAcceptanceTestFiles() returned %d files, want %d", len(files), len(pir.Resources))
	}

	if files[0].Path != "internal/provider/resource_pet_acceptance_test.go" {
		t.Errorf("file[0].Path = %q, want %q", files[0].Path, "internal/provider/resource_pet_acceptance_test.go")
	}
	if files[1].Path != "internal/provider/resource_owner_acceptance_test.go" {
		t.Errorf("file[1].Path = %q, want %q", files[1].Path, "internal/provider/resource_owner_acceptance_test.go")
	}
}

// TestResourceAcceptanceTestFiles_SkipsScaffolded verifies that a scaffolded
// (unwired) resource — whose CRUD bodies report "is not wired to a remote API
// endpoint" — gets no acceptance test, so the generated provider's own
// `go test ./...` does not fail on a lifecycle test that could never pass.
func TestResourceAcceptanceTestFiles_SkipsScaffolded(t *testing.T) {
	pir := ir.ProviderIR{
		Name:     "mycloud",
		TypeName: "mycloud",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "api_key", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
		Resources: []ir.ResourceIR{
			sampleResourceIR(),
			{Name: "scaffolded", TypeName: "mycloud_scaffolded"},
		},
	}
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}

	files := ResourceAcceptanceTestFiles(pir, cfg)
	if len(files) != 1 {
		t.Fatalf("ResourceAcceptanceTestFiles() returned %d files, want 1 (scaffolded resource skipped)", len(files))
	}
	if files[0].Path != "internal/provider/resource_pet_acceptance_test.go" {
		t.Errorf("file[0].Path = %q, want %q", files[0].Path, "internal/provider/resource_pet_acceptance_test.go")
	}
}

// generateResourceAcceptanceTestModule writes the generated go.mod, provider.go,
// resource file, and resource acceptance test file into a temporary module
// directory and returns the module root.
func generateResourceAcceptanceTestModule(t *testing.T, pir ir.ProviderIR) string {
	t.Helper()
	tmp := t.TempDir()

	cfg := BuildConfig{
		ProviderName: pir.Name,
		Namespace:    pir.Name,
	}

	h := Harness{OutputDir: tmp}
	files := resourceModuleFiles(t, pir, cfg)
	files = append(files, ResourceAcceptanceTestFiles(pir, cfg)...)
	if err := h.Generate(files); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	return tmp
}

// TestAcceptanceParamPair covers acceptanceParamPair across the primitive types
// and the constraint shapes that must reject an attribute as a mutation
// target: const and one-member enums pin the value, a two-member enum varies
// across its members, numeric bounds clamp the historical "1"/"2" defaults,
// and a pattern admits at most the candidates on the deterministic list.
func TestAcceptanceParamPair(t *testing.T) {
	strPtr := func(v string) *any {
		x := any(v)
		return &x
	}
	intPtr := func(v int) *int {
		return &v
	}
	floatPtr := func(v float64) *float64 {
		return &v
	}
	cases := []struct {
		name        string
		schema      ir.SchemaIR
		wantCreate  string
		wantUpdated string
		wantOK      bool
	}{
		{
			name:        "unconstrained string",
			schema:      ir.SchemaIR{Type: ir.TypeString},
			wantCreate:  "example",
			wantUpdated: "updated",
			wantOK:      true,
		},
		{
			name:        "unconstrained int",
			schema:      ir.SchemaIR{Type: ir.TypeInt},
			wantCreate:  "1",
			wantUpdated: "2",
			wantOK:      true,
		},
		{
			name:        "unconstrained float",
			schema:      ir.SchemaIR{Type: ir.TypeFloat},
			wantCreate:  "1.0",
			wantUpdated: "2.0",
			wantOK:      true,
		},
		{
			name:        "unconstrained bool",
			schema:      ir.SchemaIR{Type: ir.TypeBool},
			wantCreate:  "true",
			wantUpdated: "false",
			wantOK:      true,
		},
		{
			name:        "string enum with two members",
			schema:      ir.SchemaIR{Type: ir.TypeString, EnumValues: []any{"alpha", "beta", "gamma"}},
			wantCreate:  "alpha",
			wantUpdated: "beta",
			wantOK:      true,
		},
		{
			name:   "string enum with one member is pinned",
			schema: ir.SchemaIR{Type: ir.TypeString, EnumValues: []any{"alpha"}},
			wantOK: false,
		},
		{
			name:   "string const is pinned",
			schema: ir.SchemaIR{Type: ir.TypeString, Const: strPtr("standard")},
			wantOK: false,
		},
		{
			name:        "string length window keeps both values distinct",
			schema:      ir.SchemaIR{Type: ir.TypeString, MinLength: intPtr(3), MaxLength: intPtr(64)},
			wantCreate:  "example",
			wantUpdated: "updated",
			wantOK:      true,
		},
		{
			name:        "string pattern admitting two candidates",
			schema:      ir.SchemaIR{Type: ir.TypeString, Pattern: `^[a-z]+-[0-9]+$`},
			wantCreate:  "example-1",
			wantUpdated: "abc-1",
			wantOK:      true,
		},
		{
			name:   "string pattern admitting one candidate",
			schema: ir.SchemaIR{Type: ir.TypeString, Pattern: `^[A-Z]{3}-[0-9]+$`},
			wantOK: false,
		},
		{
			name:        "int bounds clamp the defaults",
			schema:      ir.SchemaIR{Type: ir.TypeInt, Minimum: floatPtr(2), Maximum: floatPtr(10)},
			wantCreate:  "2",
			wantUpdated: "3",
			wantOK:      true,
		},
		{
			name:        "int bounds admitting only one value",
			schema:      ir.SchemaIR{Type: ir.TypeInt, Minimum: floatPtr(1), Maximum: floatPtr(1)},
			wantCreate:  "1",
			wantUpdated: "0",
			wantOK:      false,
		},
		{
			name:   "int enum with one member is pinned",
			schema: ir.SchemaIR{Type: ir.TypeInt, EnumValues: []any{float64(3)}},
			wantOK: false,
		},
		{
			name:   "float range collapsing the defaults",
			schema: ir.SchemaIR{Type: ir.TypeFloat, Minimum: floatPtr(0), Maximum: floatPtr(1)},
			wantOK: false,
		},
		{
			name:   "bool enum with one member is pinned",
			schema: ir.SchemaIR{Type: ir.TypeBool, EnumValues: []any{true}},
			wantOK: false,
		},
		{
			name:   "null type is not a mutation candidate",
			schema: ir.SchemaIR{Type: ir.TypeNull},
			wantOK: false,
		},
		{
			name:   "dynamic type is not a mutation candidate",
			schema: ir.SchemaIR{Type: ir.TypeDynamic},
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			create, updated, ok := acceptanceParamPair(tc.schema)
			if ok != tc.wantOK {
				t.Fatalf("acceptanceParamPair() ok = %v, want %v (create %q, updated %q)", ok, tc.wantOK, create, updated)
			}
			if !ok {
				return
			}
			if create != tc.wantCreate || updated != tc.wantUpdated {
				t.Errorf("acceptanceParamPair() = (%q, %q), want (%q, %q)",
					create, updated, tc.wantCreate, tc.wantUpdated)
			}
			if create == updated {
				t.Errorf("acceptanceParamPair() create %q == updated %q; the update step must vary the value",
					create, updated)
			}
		})
	}
}

// sampleProviderWithAPIKeyAuthIR returns a provider fixture that declares an
// API-key (header) security scheme alongside the sample wired resource. The
// api_key config attribute is already present on sampleProviderWithResourceIR;
// adding the scheme makes the generated Configure wire client.APIKeyAuth, and
// the generated acceptance config sets api_key = "example" (the string example
// value), so the mock's auth assertion sees X-API-Key: example on the wire.
// The endpoint attribute lets the test point the provider at the mock server.
func sampleProviderWithAPIKeyAuthIR() ir.ProviderIR {
	pir := sampleProviderWithResourceIR()
	pir.ConfigSchema.Attributes = append(pir.ConfigSchema.Attributes, ir.AttributeIR{
		Name:     "endpoint",
		Optional: true,
		Schema:   ir.SchemaIR{Type: ir.TypeString},
	})
	pir.SecurityIR.Schemes = []ir.SecuritySchemeIR{
		{Name: "apiKey", Type: ir.SecuritySchemeAPIKey, In: "header", NameField: "X-API-Key"},
	}
	return pir
}

// TestResourceAcceptanceTestFile_AuthChecks verifies the generated mock server
// asserts the provider's static-credential auth schemes (REMAINING_GAPS §1.2
// level (c) + §5 "the mock asserts nothing about auth headers"). For each
// static scheme (API key in header/query/cookie, HTTP basic) the mock emits a
// 401 guard. The HTTP bearer and OpenID Connect interceptors both fetch/inject
// a bearer token and contest the Authorization header under AND semantics, so
// neither Authorization assertion is emitted (the documented conflict skip);
// the degenerate no-flows OAuth2 surface contributes nothing. OIDC is
// token-fetching, so the mock stubs /oauth/token.
func TestResourceAcceptanceTestFile_AuthChecks(t *testing.T) {
	pir := sampleProviderWithResourceIR()
	r := pir.Resources[0]
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}
	pir.SecurityIR.Schemes = []ir.SecuritySchemeIR{
		{Name: "apiKeyCookie", Type: ir.SecuritySchemeAPIKey, In: "cookie", NameField: "session"},
		{Name: "apiKeyHeader", Type: ir.SecuritySchemeAPIKey, In: "header", NameField: "X-API-Key"},
		{Name: "apiKeyQuery", Type: ir.SecuritySchemeAPIKey, In: "query", NameField: "api_key"},
		{Name: "basicAuth", Type: ir.SecuritySchemeHTTP, Scheme: "basic"},
		{Name: "bearerAuth", Type: ir.SecuritySchemeHTTP, Scheme: "bearer"},
		{Name: "oauth2", Type: ir.SecuritySchemeOAuth2},
		{Name: "oidc", Type: ir.SecuritySchemeOpenIDConnect},
	}

	file := ResourceAcceptanceTestFile(pir, r, cfg)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		`r.Header.Get("X-API-Key") != "example"`,
		`r.URL.Query().Get("api_key") != "example"`,
		`r.Cookie("session")`,
		`r.BasicAuth()`,
		`http.StatusUnauthorized`,
		// OIDC is token-fetching: the mock stubs the token endpoint.
		`mux.HandleFunc("/oauth/token"`,
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated mock missing auth check %q\ncontent:\n%s", want, got)
		}
	}
	// The bearer and OIDC interceptors contest the Authorization header under
	// AND semantics, so neither bearer assertion is emitted.
	for _, absent := range []string{
		`r.Header.Get("Authorization") != "Bearer example"`,
		`r.Header.Get("Authorization") != "Bearer example-token"`,
	} {
		if strings.Contains(got, absent) {
			t.Errorf("contested Authorization assertion %q must not be emitted\ncontent:\n%s", absent, got)
		}
	}
	// Exactly four 401 guards: one per static-credential scheme. The degenerate
	// no-flows OAuth2 surface must not contribute a guard (no interceptor).
	if n := strings.Count(got, "http.StatusUnauthorized"); n != 4 {
		t.Errorf("StatusUnauthorized occurrences = %d, want 4 (bearer/oidc contested, no-flows oauth2 skipped)\ncontent:\n%s", n, got)
	}
}

// TestResourceAcceptanceTestFile_NoAuthOmitsChecks verifies that a provider with
// no security scheme generates a mock with no auth guards, so the handler is
// unchanged for unauthenticated providers (regression guard).
func TestResourceAcceptanceTestFile_NoAuthOmitsChecks(t *testing.T) {
	pir := sampleProviderWithResourceIR()
	r := pir.Resources[0]
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}

	file := ResourceAcceptanceTestFile(pir, r, cfg)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if strings.Contains(buf.String(), "http.StatusUnauthorized") {
		t.Errorf("unauthenticated provider mock emitted a 401 auth guard\ncontent:\n%s", buf.String())
	}
}

// TestResourceAcceptanceTestFile_MockRejectsMissingAuth generates a provider
// module whose mock server asserts an API-key header, then runs a direct test
// against the generated mock proving a request without the credential is
// rejected with 401. This is the §1.2 level (c) rejection half: it proves the
// generated mock actually enforces the credential, not just that the check
// renders. It needs no terraform binary (it hits the httptest server directly).
func TestResourceAcceptanceTestFile_MockRejectsMissingAuth(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping compile-time mock auth rejection test in short mode")
	}
	pir := sampleProviderWithAPIKeyAuthIR()
	tmp := generateResourceAcceptanceTestModule(t, pir)

	rejectTest := `package provider

import (
	"io"
	"net/http"
	"testing"
)

func TestMockRejectsMissingAuth(t *testing.T) {
	s := newPetResourceMockServer()
	defer s.Close()
	resp, err := http.Get(s.URL + "/pets")
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d (mock must reject missing X-API-Key)", resp.StatusCode, http.StatusUnauthorized)
	}
}
`
	if err := os.WriteFile(filepath.Join(tmp, "internal", "provider", "zz_mock_auth_reject_test.go"), []byte(rejectTest), 0o600); err != nil {
		t.Fatalf("write reject test: %v", err)
	}

	ctx, cancel := contextWithTimeout(t, 5*time.Minute)
	defer cancel()

	tidyCmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidyCmd.Dir = tmp
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\n%s", err, out)
	}

	cmd := exec.CommandContext(ctx, "go", "test", "./internal/provider", "-v", "-run", "TestMockRejectsMissingAuth")
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "PASS") {
		t.Fatalf("expected mock reject test to pass, got:\n%s", out)
	}
}

// TestResourceAcceptanceTestFile_AuthLifecycle generates a provider module whose
// mock server asserts an API-key header and runs the full generated acceptance
// lifecycle (create/update/import/delete) against it. Passing proves the §1.2
// level (c) acceptance half end to end: the generated Configure wires
// client.APIKeyAuth from the api_key config attribute, the interceptor attaches
// X-API-Key: example to every request, and the mock accepts the credential. It
// requires the terraform CLI binary (terraform-plugin-testing shells out to
// it), so it is gated on EIDOS_RUN_NETWORK_TESTS=1 like the plain lifecycle
// test.
func TestResourceAcceptanceTestFile_AuthLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping compile-time acceptance test validation in short mode")
	}
	if os.Getenv("EIDOS_RUN_NETWORK_TESTS") != "1" {
		t.Skip("skipping network-backed auth acceptance test; set EIDOS_RUN_NETWORK_TESTS=1 to run")
	}

	pir := sampleProviderWithAPIKeyAuthIR()
	tmp := generateResourceAcceptanceTestModule(t, pir)

	ctx, cancel := contextWithTimeout(t, 5*time.Minute)
	defer cancel()

	tidyCmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidyCmd.Dir = tmp
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\n%s", err, out)
	}

	cmd := exec.CommandContext(ctx, "go", "test", "./internal/provider", "-v", "-run", "TestAccPetResourceLifecycle")
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "PASS") {
		t.Fatalf("expected auth acceptance test to pass, got:\n%s", out)
	}
}

// sampleProviderWithOAuth2ClientCredentialsIR returns a provider fixture that
// declares an OAuth2 client_credentials security scheme (no spec tokenUrl, so
// the token_url config attribute is the sole token source) alongside the
// sample wired resource. The client_id/client_secret/token_url config
// attributes mirror what transformer.applySecurityConfigAttributes would emit
// for such a scheme, and the endpoint attribute lets the test point the
// provider at the mock server. No HTTP bearer scheme is declared, so the mock
// asserts the OAuth2 bearer token on the resource path without the
// Authorization-header conflict that arises when both are present.
func sampleProviderWithOAuth2ClientCredentialsIR() ir.ProviderIR {
	pir := sampleProviderWithResourceIR()
	pir.ConfigSchema.Attributes = append(pir.ConfigSchema.Attributes,
		ir.AttributeIR{Name: "endpoint", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		ir.AttributeIR{Name: "client_id", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		ir.AttributeIR{Name: "client_secret", Optional: true, Sensitive: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		ir.AttributeIR{Name: "token_url", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
	)
	pir.SecurityIR.Schemes = []ir.SecuritySchemeIR{
		{
			Name: "oauth2",
			Type: ir.SecuritySchemeOAuth2,
			Flows: &ir.OAuthFlowsIR{
				ClientCredentials: &ir.OAuthFlowIR{TokenURL: ""},
			},
		},
	}
	return pir
}

// TestResourceAcceptanceTestFile_OAuth2AuthChecks verifies the generated mock
// server stubs the OAuth2 token endpoint and asserts the resulting bearer
// token on the resource path (REMAINING_GAPS §1.2 level (c) for OAuth2
// client_credentials). The mock registers /oauth/token returning a fixed
// token, and the resource handler rejects requests missing
// "Authorization: Bearer example-token" with 401. The acceptance config
// injects the mock token URL into the token_url attribute via a fmt
// placeholder, so the generated client's OAuth2ClientCredentials interceptor
// fetches the token from the mock during the lifecycle.
func TestResourceAcceptanceTestFile_OAuth2AuthChecks(t *testing.T) {
	pir := sampleProviderWithOAuth2ClientCredentialsIR()
	r := pir.Resources[0]
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}

	file := ResourceAcceptanceTestFile(pir, r, cfg)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		`mux.HandleFunc("/oauth/token"`,
		`\"access_token\":\"example-token\"`,
		`r.Header.Get("Authorization") != "Bearer example-token"`,
		`token_url = \"%s\"`,
		`server.URL+"/oauth/token"`,
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated OAuth2 mock/config missing %q\ncontent:\n%s", want, got)
		}
	}
	// Exactly one 401 guard: the OAuth2 bearer check. The api_key config
	// attribute is present (inherited from the sample fixture) but no API key
	// scheme is declared, so it contributes no guard.
	if n := strings.Count(got, "http.StatusUnauthorized"); n != 1 {
		t.Errorf("StatusUnauthorized occurrences = %d, want 1 (OAuth2 bearer only)\ncontent:\n%s", n, got)
	}
}

// TestResourceAcceptanceTestFile_OAuth2AndBearerConflictSkipsHeaderCheck
// verifies that when an OAuth2 client_credentials scheme and an HTTP bearer
// scheme are both declared, the mock emits neither Authorization assertion:
// both interceptors write the Authorization header (last writer wins under the
// provider's AND semantics), so either assertion could spuriously fail. This is
// the documented AND-semantics limitation, not a silent drop of coverage.
func TestResourceAcceptanceTestFile_OAuth2AndBearerConflictSkipsHeaderCheck(t *testing.T) {
	pir := sampleProviderWithOAuth2ClientCredentialsIR()
	pir.SecurityIR.Schemes = append(pir.SecurityIR.Schemes, ir.SecuritySchemeIR{
		Name: "bearerAuth", Type: ir.SecuritySchemeHTTP, Scheme: "bearer",
	})
	r := pir.Resources[0]
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}

	file := ResourceAcceptanceTestFile(pir, r, cfg)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()
	if strings.Contains(got, `Bearer example-token`) {
		t.Errorf("OAuth2 bearer assertion emitted alongside HTTP bearer scheme (Authorization conflict)\ncontent:\n%s", got)
	}
	if strings.Contains(got, `Bearer example"`) {
		t.Errorf("HTTP bearer assertion emitted alongside OAuth2 client_credentials scheme (Authorization conflict)\ncontent:\n%s", got)
	}
}

// TestResourceAcceptanceTestFile_OAuth2PasswordAuthChecks verifies the mock
// server and acceptance config for an OAuth2 password flow: the mock stubs
// /oauth/token (the same stub serves every grant, including password), the
// resource handler asserts "Authorization: Bearer example-token", and the
// acceptance config injects the mock token URL into the token_url attribute so
// the generated client's OAuth2Password interceptor fetches from the mock.
func TestResourceAcceptanceTestFile_OAuth2PasswordAuthChecks(t *testing.T) {
	pir := sampleProviderWithResourceIR()
	pir.ConfigSchema.Attributes = append(pir.ConfigSchema.Attributes,
		ir.AttributeIR{Name: "endpoint", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		ir.AttributeIR{Name: "username", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		ir.AttributeIR{Name: "password", Optional: true, Sensitive: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		ir.AttributeIR{Name: "client_id", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		ir.AttributeIR{Name: "client_secret", Optional: true, Sensitive: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		ir.AttributeIR{Name: "token_url", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
	)
	pir.SecurityIR.Schemes = []ir.SecuritySchemeIR{
		{
			Name: "oauth2",
			Type: ir.SecuritySchemeOAuth2,
			Flows: &ir.OAuthFlowsIR{
				Password: &ir.OAuthFlowIR{TokenURL: ""},
			},
		},
	}
	r := pir.Resources[0]
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}

	file := ResourceAcceptanceTestFile(pir, r, cfg)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		`mux.HandleFunc("/oauth/token"`,
		`\"access_token\":\"example-token\"`,
		`r.Header.Get("Authorization") != "Bearer example-token"`,
		`token_url = \"%s\"`,
		`server.URL+"/oauth/token"`,
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated OAuth2 password mock/config missing %q\ncontent:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "http.StatusUnauthorized"); n != 1 {
		t.Errorf("StatusUnauthorized occurrences = %d, want 1 (OAuth2 bearer only)\ncontent:\n%s", n, got)
	}
}

// TestResourceAcceptanceTestFile_OpenIDConnectAuthChecks verifies the mock
// server and acceptance config for an OpenID Connect scheme: the mock stubs
// /oauth/token, the resource handler asserts "Authorization: Bearer
// example-token", and the acceptance config injects the mock token URL into
// the oidc_token_url attribute. The override skips discovery (the spec's
// discovery URL is baked into the provider and unreachable in tests), so no
// discovery endpoint is stubbed.
func TestResourceAcceptanceTestFile_OpenIDConnectAuthChecks(t *testing.T) {
	pir := sampleProviderWithResourceIR()
	pir.ConfigSchema.Attributes = append(pir.ConfigSchema.Attributes,
		ir.AttributeIR{Name: "endpoint", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		ir.AttributeIR{Name: "oidc_token_url", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		ir.AttributeIR{Name: "client_id", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		ir.AttributeIR{Name: "client_secret", Optional: true, Sensitive: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
	)
	pir.SecurityIR.Schemes = []ir.SecuritySchemeIR{
		{
			Name:             "oidc",
			Type:             ir.SecuritySchemeOpenIDConnect,
			OpenIDConnectURL: "https://api.example.com/.well-known/openid-configuration",
		},
	}
	r := pir.Resources[0]
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}

	file := ResourceAcceptanceTestFile(pir, r, cfg)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		`mux.HandleFunc("/oauth/token"`,
		`r.Header.Get("Authorization") != "Bearer example-token"`,
		`oidc_token_url = \"%s\"`,
		`server.URL+"/oauth/token"`,
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated OIDC mock/config missing %q\ncontent:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "http.StatusUnauthorized"); n != 1 {
		t.Errorf("StatusUnauthorized occurrences = %d, want 1 (OIDC bearer only)\ncontent:\n%s", n, got)
	}
}

// TestResourceAcceptanceTestFile_OAuth2Lifecycle generates a provider module
// whose mock server stubs the OAuth2 token endpoint and asserts the resulting
// bearer token, then runs the full generated acceptance lifecycle
// (create/update/import/delete) against it. Passing proves the §1.2 level (c)
// OAuth2 client_credentials coverage end to end: the generated Configure wires
// client.OAuth2ClientCredentials from the client_id/client_secret/token_url
// config attributes, the interceptor fetches a token from the mock's
// /oauth/token endpoint, attaches it as "Authorization: Bearer example-token"
// to every resource request, and the mock accepts the credential. It requires
// the terraform CLI binary, so it is gated on EIDOS_RUN_NETWORK_TESTS=1 like
// the other lifecycle tests.
func TestResourceAcceptanceTestFile_OAuth2Lifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping compile-time acceptance test validation in short mode")
	}
	if os.Getenv("EIDOS_RUN_NETWORK_TESTS") != "1" {
		t.Skip("skipping network-backed OAuth2 acceptance test; set EIDOS_RUN_NETWORK_TESTS=1 to run")
	}

	pir := sampleProviderWithOAuth2ClientCredentialsIR()
	tmp := generateResourceAcceptanceTestModule(t, pir)

	ctx, cancel := contextWithTimeout(t, 5*time.Minute)
	defer cancel()

	tidyCmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidyCmd.Dir = tmp
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\n%s", err, out)
	}

	cmd := exec.CommandContext(ctx, "go", "test", "./internal/provider", "-v", "-run", "TestAccPetResourceLifecycle")
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "PASS") {
		t.Fatalf("expected OAuth2 acceptance test to pass, got:\n%s", out)
	}
}

// TestResourceAcceptanceTestFile_NonIDAttrAndPOSTDelete verifies the mock server
// adapts to a resource whose ID attribute is not named "id" and whose delete is
// a non-DELETE method on a nested path (G-21). The mock must echo the ID back
// under the actual ID attribute name (body["symbol"], not body["id"]) so the
// create response decodes into the resource's ID attribute; the create status
// must stay the spec's 201 rather than being overwritten by the nested-path
// delete's 200; and the POST handler must dispatch nested-path requests (id
// containing '/') to the delete branch. A resource with no update mapping must
// also skip the update step, since its scaffold Update reports a diagnostic and
// the step could never pass.
func TestResourceAcceptanceTestFile_NonIDAttrAndPOSTDelete(t *testing.T) {
	r := ir.ResourceIR{
		Name:        "ship",
		TypeName:    "mycloud_ship",
		Description: "A ship resource.",
		IDAttribute: "symbol",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "symbol", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
				{Name: "name", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
		CRUDMapping: ir.CRUDMappingIR{
			Create: ir.OperationMappingIR{Method: "POST", PathTemplate: "/ships", SuccessCodes: []int{201}},
			Read:   ir.OperationMappingIR{Method: "GET", PathTemplate: "/ships/{id}", SuccessCodes: []int{200}},
			// No Update mapping: the Update method stays a scaffold, so the
			// lifecycle test must not emit an update step.
			Delete: ir.OperationMappingIR{Method: "POST", PathTemplate: "/ships/{id}/scrap", SuccessCodes: []int{200}},
		},
		Importable: true,
	}
	pir := sampleProviderWithResourceIR()
	pir.Resources[0] = r
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}

	file := ResourceAcceptanceTestFile(pir, r, cfg)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		`body["symbol"] = "example-id"`, // ID echoed under the ID attribute name
		`w.WriteHeader(201)`,            // create status not overwritten by the delete
		`strings.Contains(id, "/")`,     // nested-path POST dispatched to delete
		`w.WriteHeader(200)`,            // delete status
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated mock missing %q\ncontent:\n%s", want, got)
		}
	}
	for _, absent := range []string{
		`body["id"] = "example-id"`,                                // must not hardcode the "id" key
		`Config: testAccShipResourceConfig(server.URL, "updated")`, // no update step for a scaffold Update
	} {
		if strings.Contains(got, absent) {
			t.Errorf("generated acceptance test must not contain %q\ncontent:\n%s", absent, got)
		}
	}
}

// TestResourceAcceptanceTestFile_PutAsCreateDispatch verifies the mock server
// for a PUT-as-create (upsert) resource: the instance-path PUT is the Create,
// the same PUT is the Update, and the create branch must dispatch on
// http.MethodPut rather than a hard-coded POST. Because create and update
// share the instance PUT, the update branch must drop http.MethodPut (keeping
// only http.MethodPatch) so the generated switch has no duplicate case labels.
// The acceptance update step must also mutate a non-identifier attribute: the
// id is Required for a PUT-as-create resource, but changing it would rewrite
// the instance path rather than test a real update.
func TestResourceAcceptanceTestFile_PutAsCreateDispatch(t *testing.T) {
	r := ir.ResourceIR{
		Name:        "alarm",
		TypeName:    "mycloud_alarm",
		Description: "An alarm resource.",
		IDAttribute: "alarm_id",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "alarm_id", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
				{Name: "severity", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		},
		CRUDMapping: ir.CRUDMappingIR{
			// PUT-as-create: no collection POST; the instance PUT is Create and
			// Update (the same upsert), GET is Read, DELETE is Delete.
			Create: ir.OperationMappingIR{Method: "PUT", PathTemplate: "/alarms/{alarmId}", SuccessCodes: []int{200}},
			Read:   ir.OperationMappingIR{Method: "GET", PathTemplate: "/alarms/{alarmId}", SuccessCodes: []int{200}},
			Update: &ir.OperationMappingIR{Method: "PUT", PathTemplate: "/alarms/{alarmId}", SuccessCodes: []int{200}},
			Delete: ir.OperationMappingIR{Method: "DELETE", PathTemplate: "/alarms/{alarmId}", SuccessCodes: []int{200}},
		},
		Importable: true,
	}
	pir := sampleProviderWithResourceIR()
	pir.Resources[0] = r
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}

	file := ResourceAcceptanceTestFile(pir, r, cfg)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	// The create branch must match the PUT method, not a hard-coded POST, so a
	// PUT create reaches the create handler.
	if !strings.Contains(got, `case http.MethodPut:`) {
		t.Errorf("PUT-as-create mock must dispatch create on http.MethodPut\ncontent:\n%s", got)
	}
	// The update branch must not re-list http.MethodPut (duplicate case label is
	// a compile error); only http.MethodPatch should remain for update.
	putCount := strings.Count(got, `http.MethodPut`)
	if putCount != 1 {
		t.Errorf("http.MethodPut must appear exactly once (create branch only); got %d\ncontent:\n%s", putCount, got)
	}
	if !strings.Contains(got, `case http.MethodPatch:`) {
		t.Errorf("update branch must still dispatch http.MethodPatch\ncontent:\n%s", got)
	}
	// The update step must mutate a non-identifier attribute (severity), not the
	// Required alarm_id, so the test exercises a real update rather than an
	// identity/path rewrite.
	if strings.Contains(got, `testAccAlarmResourceConfig(server.URL, "updated")`) {
		// The update step's config call is present; confirm it does not key the
		// mutation off the identifier by checking the param attribute is severity.
		if !strings.Contains(got, `"severity"`) {
			t.Errorf("update step should check the severity attribute, not the identifier\ncontent:\n%s", got)
		}
	}
	// The identifier must not be the acceptance param attribute: the config
	// function's mutated argument must not be alarm_id.
	if strings.Contains(got, `TestCheckResourceAttr("mycloud_alarm.example", "alarm_id", "updated`) {
		t.Errorf("update step must not mutate the identifier attribute alarm_id\ncontent:\n%s", got)
	}
}

// TestResourceAcceptanceTestFile_DiscriminatedUnion verifies that a
// discriminated-union attribute renders as an HCL object literal in the
// acceptance test config (the writeHCLDiscriminatedUnion path), not a scalar
// placeholder — a union attribute with empty Attributes would otherwise fall
// through to primitiveExampleValue and emit a string for an object attribute,
// failing schema validation at plan time.
func TestResourceAcceptanceTestFile_DiscriminatedUnion(t *testing.T) {
	r := sampleResourceIR()
	r.Schema.Attributes = append(r.Schema.Attributes, ir.AttributeIR{
		Name:     "animal",
		Required: true,
		Schema: ir.SchemaIR{
			Union: &ir.UnionType{
				Kind: ir.OneOf,
				Variants: []ir.SchemaIR{
					{
						Name: "cat",
						Attributes: []ir.AttributeIR{
							{Name: "animal_type", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
							{Name: "lives", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeInt}},
						},
					},
					{
						Name: "dog",
						Attributes: []ir.AttributeIR{
							{Name: "animal_type", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
							{Name: "bark_volume", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeInt}},
						},
					},
				},
				Discriminator: &ir.DiscriminatorIR{
					PropertyName: "animalType",
					Mapping:      map[string]string{"cat": "CatVariant", "dog": "DogVariant"},
				},
			},
		},
	})

	pir := sampleProviderWithResourceIR()
	pir.Resources[0] = r
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}

	file := ResourceAcceptanceTestFile(pir, r, cfg)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	// The union must render as an HCL object literal keyed by the attribute
	// name, with the discriminator and a variant field present.
	if !strings.Contains(got, "animal = {") {
		t.Errorf("union attribute must render as an object literal\ncontent:\n%s", got)
	}
	if !strings.Contains(got, "animal_type = ") {
		t.Errorf("union object literal must include the discriminator field\ncontent:\n%s", got)
	}
	if !strings.Contains(got, "lives = ") {
		t.Errorf("union object literal must include a variant field\ncontent:\n%s", got)
	}
}

// TestResourceAcceptanceTestFile_NestedCollectionRead verifies that the
// stateful mock for a child resource (read_collection_path) wraps the stored
// element in the nested read shape the generated Read navigation expects —
// {"portFilter": {"rules": {"mock_collection": [body]}}} for NestedCollectionPath
// "rules.*" with envelope "portFilter" — and that the import step is omitted:
// the mock's read path carries only the parent id, so it cannot serve an
// element keyed by an arbitrary imported identifier.
func TestResourceAcceptanceTestFile_NestedCollectionRead(t *testing.T) {
	r := ir.ResourceIR{
		Name:     "port_filter_rule",
		TypeName: "gigavuecore_port_filter_rule",
		Schema: ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{
			{Name: "id", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			{Name: "port_id", Required: true, WireName: "portId", PathParam: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			{Name: "action", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		}},
		CRUDMapping: ir.CRUDMappingIR{
			Create: ir.OperationMappingIR{Method: "POST", PathTemplate: "/ports/{portId}/filters/rules"},
			Read: ir.OperationMappingIR{
				Method: "GET", PathTemplate: "/ports/{portId}/filters",
				ResponseEnvelope: "portFilter", NestedCollectionPath: "rules.*", ResponseIsCollection: true,
			},
			Delete: ir.OperationMappingIR{Method: "DELETE", PathTemplate: "/ports/{portId}/filters/rules/{id}"},
		},
		Importable: true,
	}
	pir := sampleProviderWithResourceIR()
	pir.Resources = []ir.ResourceIR{r}
	cfg := BuildConfig{ProviderName: pir.Name, Namespace: pir.Name}

	file := ResourceAcceptanceTestFile(pir, r, cfg)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, `map[string]any{"portFilter": map[string]any{"rules": map[string]any{"mock_collection": []any{body}}}}`) {
		t.Errorf("mock GET response must wrap the stored element in the nested collection shape; content:\n%s", got)
	}
	if strings.Contains(got, "ImportState") {
		t.Errorf("nested-collection child resources must not emit an import step; content:\n%s", got)
	}
}

// TestResourceFile_PathParamOmittedFromRequestBody verifies that attributes
// folded into the schema from operation path parameters (child resources) are
// deleted from the marshaled request-body maps the generated Create and
// Update helpers build: modelToJSONMap reflects the whole model, so without
// the delete the path param would leak into the JSON body (issue #64).
func TestResourceFile_PathParamOmittedFromRequestBody(t *testing.T) {
	bodySchema := &ir.SchemaIR{Attributes: []ir.AttributeIR{
		{Name: "action", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
	}}
	r := ir.ResourceIR{
		Name:     "port_filter_rule",
		TypeName: "gigavuecore_port_filter_rule",
		Schema: ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{
			{Name: "id", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			{Name: "port_id", Required: true, WireName: "portId", PathParam: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			{Name: "action", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		}},
		CRUDMapping: ir.CRUDMappingIR{
			Create: ir.OperationMappingIR{Method: "POST", PathTemplate: "/ports/{portId}/filters/rules", BodySchema: bodySchema},
			Read:   ir.OperationMappingIR{Method: "GET", PathTemplate: "/ports/{portId}/filters/rules/{id}"},
			Update: &ir.OperationMappingIR{Method: "PUT", PathTemplate: "/ports/{portId}/filters/rules/{id}", BodySchema: bodySchema},
			Delete: ir.OperationMappingIR{Method: "DELETE", PathTemplate: "/ports/{portId}/filters/rules/{id}"},
		},
	}

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if n := strings.Count(got, `delete(body, "portId")`); n != 2 {
		t.Errorf("create AND update bodies must each omit the path param (found %d deletes); content:\n%s", n, got)
	}
	if strings.Contains(got, `delete(body, "action")`) || strings.Contains(got, `delete(body, "id")`) {
		t.Errorf("non-path-param attributes must not be deleted from the body; content:\n%s", got)
	}
}
