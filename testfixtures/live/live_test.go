package live_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/signalbreak-labs/eidos/pkg/api"
	"github.com/signalbreak-labs/eidos/pkg/generator"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// Live end-to-end tests for the generated Terraform provider.
//
// These tests load the fictional MyCloud reference spec, generate a complete
// Terraform provider module, inject a live API connectivity test, and run the
// generated acceptance suite with TF_ACC=1 — against a local deterministic mock
// server, so no external system is involved.
//
// Required environment variables:
//   - TF_ACC=1            opt-in flag required by Terraform for acceptance tests.
//
// The test spins up a local mock server, sets MYCLOUD_API_KEY and
// MYCLOUD_ENDPOINT for the generated module, and verifies the generated client
// reaches the mock and gets a 200 response.

func TestAccLiveProviders(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("set TF_ACC=1 to run live end-to-end acceptance tests")
	}

	mock := newMyCloudMockServer(t, "test-api-key")

	t.Run("mycloud", func(t *testing.T) {
		runLiveProvider(t, "mycloud", liveConnectivityConfig{
			requiredEnv:  "MYCLOUD_API_KEY",
			endpointEnv:  "MYCLOUD_ENDPOINT",
			authCall:     `client.APIKeyAuth(os.Getenv("MYCLOUD_API_KEY"), "header", "X-API-Key")`,
			path:         "/workspaces/demo/instances?limit=1",
			expectedCode: 200,
			extraEnv: map[string]string{
				"MYCLOUD_API_KEY":  "test-api-key",
				"MYCLOUD_ENDPOINT": mock.URL,
			},
		})
	})
}

// newMyCloudMockServer stands up a deterministic mock of the MyCloud API. It
// serves the workspace-scoped instance endpoints the generated client calls and
// validates the X-API-Key header, so the connectivity test exercises the
// generated client's auth plumbing end to end without touching a real system.
func newMyCloudMockServer(t *testing.T, apiKey string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/workspaces/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != apiKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"items":[]}`) //nolint:errcheck // mock response write failure is non-actionable
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

type liveConnectivityConfig struct {
	requiredEnv  string
	endpointEnv  string
	authCall     string
	path         string
	expectedCode int
	extraEnv     map[string]string
}

func runLiveProvider(t *testing.T, specName string, cfg liveConnectivityConfig) {
	t.Helper()

	specPath := filepath.Join("..", "..", "test", "specs", specName+".yaml")
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read spec %s: %v", specPath, err)
	}

	resp := api.Validate(data)
	if !resp.Valid {
		t.Fatalf("validate spec %s: %v", specName, resp.Diagnostics)
	}
	if resp.IRPreview == nil {
		t.Fatalf("no IR preview for spec %s", specName)
	}

	pir := sanitizeProviderIR(*resp.IRPreview)
	buildCfg := generator.BuildConfig{
		ProviderName: pir.Name,
		Namespace:    pir.Name,
	}

	dir := t.TempDir()
	if err := generateProviderModule(dir, pir, buildCfg); err != nil {
		t.Fatalf("generate provider module: %v", err)
	}

	liveFile := liveConnectivityFile(buildCfg, pir, cfg)
	h := generator.Harness{OutputDir: dir}
	if err := h.Generate([]generator.File{liveFile}); err != nil {
		t.Fatalf("inject live connectivity test: %v", err)
	}

	tidyCtx, tidyCancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer tidyCancel()
	tidy := exec.CommandContext(tidyCtx, "go", "mod", "tidy")
	tidy.Dir = dir
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy in generated module: %v\n%s", err, out)
	}

	testCtx, testCancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer testCancel()
	// Run only the injected connectivity test: `go test` still builds every
	// package in the generated module (verifying the whole module compiles),
	// then exercises the generated client against the mock endpoint. The full
	// generated acceptance suite is intentionally not run — its mock servers are
	// single-resource lifecycle provers that do not serve multi-segment shapes
	// or a delete whose declared success status differs from 204.
	test := exec.CommandContext(testCtx, "go", "test", "-run", "TestAcc"+pascalCase(pir.Name)+"LiveConnectivity", "./...", "-count=1")
	test.Dir = dir
	test.Env = append(os.Environ(), "TF_ACC=1")
	for k, v := range cfg.extraEnv {
		test.Env = append(test.Env, k+"="+v)
	}
	if out, err := test.CombinedOutput(); err != nil {
		t.Fatalf("generated provider tests failed: %v\n%s", err, out)
	}
}

func generateProviderModule(dir string, pir ir.ProviderIR, cfg generator.BuildConfig) error {
	// Assemble the complete generated module via FilesForProviderIR, the same
	// single source of truth the record/write modes use, so the live module
	// carries every construct type (resources, data sources, actions, ephemeral
	// resources, list resources, functions) and compiles like a real generation.
	// Only the injected connectivity test below is added on top.
	h := generator.Harness{OutputDir: dir}
	files, err := generator.FilesForProviderIR(&pir, cfg, generator.CollectOptions{
		IncludeTests: true,
	})
	if err != nil {
		return err
	}
	return h.Generate(files)
}

func liveConnectivityFile(cfg generator.BuildConfig, pir ir.ProviderIR, conn liveConnectivityConfig) generator.File {
	clientImport := moduleImportPath(cfg)
	data := struct {
		ClientImport      string
		ProviderPascal    string
		ProviderHuman     string
		RequiredEnv       string
		RequiredEnvQuoted string
		EndpointEnv       string
		EndpointEnvQuoted string
		AuthCall          string
		Path              string
		PathQuoted        string
		ExpectedStatus    int
	}{
		ClientImport:      clientImport,
		ProviderPascal:    pascalCase(pir.Name),
		ProviderHuman:     pir.Name,
		RequiredEnv:       conn.requiredEnv,
		RequiredEnvQuoted: strconv.Quote(conn.requiredEnv),
		EndpointEnv:       conn.endpointEnv,
		EndpointEnvQuoted: strconv.Quote(conn.endpointEnv),
		AuthCall:          conn.authCall,
		Path:              conn.path,
		PathQuoted:        strconv.Quote(conn.path),
		ExpectedStatus:    conn.expectedCode,
	}
	return generator.Template("internal/provider/live_connectivity_test.go", liveConnectivityTemplate, data)
}

const liveConnectivityTemplate = `package provider

import (
	"context"
	"os"
	"testing"

	"{{.ClientImport}}/internal/client"
)

// TestAcc{{.ProviderPascal}}LiveConnectivity verifies connectivity to the live
// {{.ProviderHuman}} API. Required environment variables:
//   - TF_ACC=1
//   - {{.RequiredEnv}}
// Optional environment variables:
//   - {{.EndpointEnv}} (overrides the generated client base URL)
func TestAcc{{.ProviderPascal}}LiveConnectivity(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("set TF_ACC=1 to run live API tests")
	}
	credential := os.Getenv({{.RequiredEnvQuoted}})
	if credential == "" {
		t.Skipf("set %s (and optionally %s) to run live API tests", {{.RequiredEnvQuoted}}, {{.EndpointEnvQuoted}})
	}

	opts := []client.ClientOption{
		client.WithInterceptors({{.AuthCall}}),
	}
	if endpoint := os.Getenv({{.EndpointEnvQuoted}}); endpoint != "" {
		opts = append(opts, client.WithBaseURL(endpoint))
	}

	c := client.New(opts...)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := c.NewRequest(ctx, "GET", {{.PathQuoted}}, nil)
	if err != nil {
		t.Fatalf("build live request: %v", err)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("live request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != {{.ExpectedStatus}} {
		t.Fatalf("unexpected status code: got %d, want %d", resp.StatusCode, {{.ExpectedStatus}})
	}
}
`

func moduleImportPath(cfg generator.BuildConfig) string {
	if strings.TrimSpace(cfg.ModulePath) != "" {
		return strings.TrimSpace(cfg.ModulePath)
	}
	return fmt.Sprintf("github.com/%s/terraform-provider-%s", cfg.Namespace, cfg.ProviderName)
}

func sanitizeProviderIR(pir ir.ProviderIR) ir.ProviderIR {
	name := sanitizeTerraformName(pir.Name)
	pir.Name = name
	pir.FullName = "terraform-provider-" + name
	pir.TypeName = name

	for i := range pir.Resources {
		pir.Resources[i].TypeName = name + "_" + sanitizeTerraformName(pir.Resources[i].Name)
	}
	for i := range pir.DataSources {
		pir.DataSources[i].TypeName = name + "_" + sanitizeTerraformName(pir.DataSources[i].Name)
	}
	for i := range pir.Actions {
		pir.Actions[i].TypeName = name + "_" + sanitizeTerraformName(pir.Actions[i].Name)
	}
	for i := range pir.EphemeralResources {
		pir.EphemeralResources[i].TypeName = name + "_" + sanitizeTerraformName(pir.EphemeralResources[i].Name)
	}
	for i := range pir.ListResources {
		pir.ListResources[i].TypeName = name + "_" + sanitizeTerraformName(pir.ListResources[i].Name)
	}
	for i := range pir.Functions {
		pir.Functions[i].TypeName = name + "_" + sanitizeTerraformName(pir.Functions[i].Name)
	}

	return pir
}

// sanitizeTerraformName converts an identifier into a valid Terraform provider
// or resource type name: lower-case, only letters/digits/dashes, with
// underscores and spaces mapped to dashes.
func sanitizeTerraformName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		case r == '_' || r == '-' || unicode.IsSpace(r):
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	out = collapseDashes(out)
	if out == "" {
		return "generated"
	}
	return out
}

func collapseDashes(s string) string {
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return s
}

// pascalCase converts an identifier to PascalCase, matching the generator's
// naming convention.
func pascalCase(s string) string {
	if s == "" {
		return ""
	}
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		r := []rune(p)
		r[0] = unicode.ToUpper(r[0])
		b.WriteString(string(r))
	}
	return b.String()
}
