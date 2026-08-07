package generator

import (
	"bytes"
	"errors"
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
// emits one acceptance test file per resource with deterministic paths.
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
			{Name: "pet", TypeName: "mycloud_pet"},
			{Name: "owner", TypeName: "mycloud_owner"},
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

func TestAcceptanceExampleValue_Known(t *testing.T) {
	cases := []struct {
		typ  ir.PrimitiveType
		want string
	}{
		{ir.TypeString, "example"},
		{ir.TypeInt, "1"},
		{ir.TypeFloat, "1.0"},
		{ir.TypeBool, "true"},
		{ir.TypeNull, "null"},
		{ir.TypeDynamic, "null"},
	}
	for _, tc := range cases {
		t.Run(string(tc.typ), func(t *testing.T) {
			if got := acceptanceExampleValue(tc.typ); got != tc.want {
				t.Errorf("acceptanceExampleValue(%q) = %q, want %q", tc.typ, got, tc.want)
			}
		})
	}
}

func TestAcceptanceExampleValue_UnknownPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for unknown PrimitiveType, got none")
		}
		err, ok := r.(error)
		if !ok {
			t.Fatalf("expected error panic, got %T", r)
		}
		if !errors.Is(err, ErrUnsupportedPrimitiveType) {
			t.Fatalf("expected ErrUnsupportedPrimitiveType sentinel, got: %v", err)
		}
		if !strings.Contains(err.Error(), "unknown-type") {
			t.Fatalf("panic message %q does not contain type name", err.Error())
		}
	}()
	acceptanceExampleValue(ir.PrimitiveType("unknown-type"))
}

func TestUpdatedValue_Known(t *testing.T) {
	cases := []struct {
		typ  ir.PrimitiveType
		want string
	}{
		{ir.TypeString, "updated"},
		{ir.TypeInt, "2"},
		{ir.TypeFloat, "2.0"},
		{ir.TypeBool, "false"},
		{ir.TypeNull, "null"},
		{ir.TypeDynamic, "null"},
	}
	for _, tc := range cases {
		t.Run(string(tc.typ), func(t *testing.T) {
			if got := updatedValue(tc.typ); got != tc.want {
				t.Errorf("updatedValue(%q) = %q, want %q", tc.typ, got, tc.want)
			}
		})
	}
}

func TestUpdatedValue_UnknownPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for unknown PrimitiveType, got none")
		}
		err, ok := r.(error)
		if !ok {
			t.Fatalf("expected error panic, got %T", r)
		}
		if !errors.Is(err, ErrUnsupportedPrimitiveType) {
			t.Fatalf("expected ErrUnsupportedPrimitiveType sentinel, got: %v", err)
		}
		if !strings.Contains(err.Error(), "unknown-type") {
			t.Fatalf("panic message %q does not contain type name", err.Error())
		}
	}()
	updatedValue(ir.PrimitiveType("unknown-type"))
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
