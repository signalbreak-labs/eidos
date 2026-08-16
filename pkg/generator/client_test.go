package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// TestClientFiles_Render verifies that ClientFiles emits the expected client
// package files and that the generated source contains the expected types and
// auth middleware.
func TestClientFiles_Render(t *testing.T) {
	providerIR := sampleClientIR()

	h := Harness{OutputDir: t.TempDir()}
	if err := h.Generate(ClientFiles(providerIR)); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	wantPaths := []string{
		"internal/client/auth.go",
		"internal/client/auth_test.go",
		"internal/client/client.go",
		"internal/client/client_test.go",
		"internal/client/errors.go",
		"internal/client/logging.go",
		"internal/client/logging_test.go",
		"internal/client/models.go",
		"internal/client/pagination.go",
		"internal/client/retry.go",
	}
	gotPaths := collectPaths(t, h.OutputDir)
	if diff := sliceDiff(wantPaths, gotPaths); diff != "" {
		t.Fatalf("emitted paths mismatch:\n%s", diff)
	}

	clientSrc := readFile(t, h.OutputDir, "internal/client/client.go")
	for _, want := range []string{
		"package client",
		"type Client struct",
		"type RequestInterceptor func(*http.Request) error",
		"func New(opts ...ClientOption) *Client",
		"func (c *Client) NewRequest",
		"func (c *Client) Do",
		"func (c *Client) Close",
		"baseURL:     \"https://api.example.com\"",
		"User-Agent",
		"func WithLogging",
		"logging            LoggingConfig",
		"NewLoggingRoundTripper",
		// Per-operation security (REMAINING_GAPS §1): scheme-keyed interceptors
		// and the WithSchemes request option that selectively applies them.
		"func WithSchemeInterceptor",
		"func WithSchemes",
		"type RequestOption func(*requestOptions)",
		"schemeInterceptors map[string]RequestInterceptor",
		"resolveInterceptors",
	} {
		if !strings.Contains(clientSrc, want) {
			t.Errorf("client.go missing %q\ncontent:\n%s", want, clientSrc)
		}
	}

	authSrc := readFile(t, h.OutputDir, "internal/client/auth.go")
	for _, want := range []string{
		"func APIKeyAuth",
		"func BasicAuth",
		"func BearerAuth",
		"func OAuth2ClientCredentials",
		"func OAuth2Password",
		"func OAuth2AuthorizationCodeRefresh",
		"func OpenIDConnect",
		"oauth2TokenSource",
		"grant_type",
		"client_credentials",
	} {
		if !strings.Contains(authSrc, want) {
			t.Errorf("auth.go missing %q\ncontent:\n%s", want, authSrc)
		}
	}

	modelsSrc := readFile(t, h.OutputDir, "internal/client/models.go")
	for _, want := range []string{
		"type Envelope struct",
		"type ErrorResponse struct",
		"type Empty struct",
		"json:\"data\"",
		"json:\"message\"",
		"json:\"code\"",
	} {
		if !strings.Contains(modelsSrc, want) {
			t.Errorf("models.go missing %q\ncontent:\n%s", want, modelsSrc)
		}
	}

	errorsSrc := readFile(t, h.OutputDir, "internal/client/errors.go")
	for _, want := range []string{
		"type APIError struct",
		"func NewAPIError",
		"func IsNotFound",
		"func IsRetryable",
	} {
		if !strings.Contains(errorsSrc, want) {
			t.Errorf("errors.go missing %q\ncontent:\n%s", want, errorsSrc)
		}
	}

	retrySrc := readFile(t, h.OutputDir, "internal/client/retry.go")
	for _, want := range []string{
		"type RetryPolicy",
		"type BackoffFunc",
		"func DefaultRetryPolicy",
		"func DefaultBackoff",
		"func DoWithRetry",
	} {
		if !strings.Contains(retrySrc, want) {
			t.Errorf("retry.go missing %q\ncontent:\n%s", want, retrySrc)
		}
	}

	paginationSrc := readFile(t, h.OutputDir, "internal/client/pagination.go")
	for _, want := range []string{
		"type Pagination struct",
		"func DefaultPagination",
		"func ExtractLinkHeader",
		"func ListAllPages",
		"offset",
		"cursor",
		"link_header",
	} {
		if !strings.Contains(paginationSrc, want) {
			t.Errorf("pagination.go missing %q\ncontent:\n%s", want, paginationSrc)
		}
	}
}

// TestClientFiles_NoSecurityOmitsAuth verifies that auth.go is not emitted when
// the provider has no security schemes, while the remaining client package still
// builds cleanly.
func TestClientFiles_NoSecurityOmitsAuth(t *testing.T) {
	providerIR := sampleClientIR()
	providerIR.SecurityIR.Schemes = nil

	h := Harness{OutputDir: t.TempDir()}
	if err := h.Generate(ClientFiles(providerIR)); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	for _, p := range collectPaths(t, h.OutputDir) {
		if p == "internal/client/auth.go" {
			t.Fatalf("auth.go emitted unexpectedly for provider with no security schemes")
		}
	}

	tmp := generateClientModule(t, providerIR)
	ctx, cancel := contextWithTimeout(t, 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "build", "./internal/client")
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}
}

// TestClientFiles_Compile generates the client package into a temporary Go
// module and verifies that it builds cleanly.
func TestClientFiles_Compile(t *testing.T) {
	providerIR := sampleClientIR()
	tmp := generateClientModule(t, providerIR)

	ctx, cancel := contextWithTimeout(t, 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "build", "./internal/client")
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}
}

// TestClientFiles_AuthMiddleware verifies that the generated client package's
// own test suite (client_test.go, auth_test.go, logging_test.go) compiles and
// passes in a generated module. eidos emits these tests alongside the client
// code, so the hand-rolled smoke fixture that previously stood in for them is
// no longer needed: running `go test ./internal/client` on the generated module
// exercises every auth middleware (API key/basic/bearer/OAuth2/OIDC), the
// request/retry/pagination helpers, and the trace logging round-tripper.
func TestClientFiles_AuthMiddleware(t *testing.T) {
	providerIR := sampleClientIR()
	tmp := generateClientModule(t, providerIR)

	ctx, cancel := contextWithTimeout(t, 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "test", "./internal/client", "-v")
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "PASS") {
		t.Fatalf("expected test to pass, got:\n%s", out)
	}
}

// sampleClientIR returns a ProviderIR populated with client, server, and
// pagination settings suitable for exercising ClientFiles.
func sampleClientIR() ir.ProviderIR {
	return ir.ProviderIR{
		Name:     "mycloud",
		TypeName: "mycloud",
		Servers: []ir.ServerIR{
			{URL: "https://api.example.com", Description: "Production API"},
		},
		ClientIR: ir.ClientIR{
			BaseURLTemplate: "https://api.example.com",
			UserAgent:       "mycloud-terraform-provider/1.0.0",
			RetryMax:        4,
			RetryWaitMin:    500 * time.Millisecond,
			RetryWaitMax:    20 * time.Second,
			Timeout:         45 * time.Second,
			Pagination: &ir.PaginationIR{
				Style:        "offset",
				PageParam:    "page",
				PerPageParam: "per_page",
			},
		},
		SecurityIR: ir.SecurityIR{
			Schemes: []ir.SecuritySchemeIR{
				{Name: "apiKey", Type: ir.SecuritySchemeAPIKey, In: "header", NameField: "X-API-Key"},
				{Name: "basicAuth", Type: ir.SecuritySchemeHTTP, Scheme: "basic"},
				{Name: "bearerAuth", Type: ir.SecuritySchemeHTTP, Scheme: "bearer"},
				{
					Name: "oauth2ClientCreds",
					Type: ir.SecuritySchemeOAuth2,
					Flows: &ir.OAuthFlowsIR{
						ClientCredentials: &ir.OAuthFlowIR{TokenURL: "https://api.example.com/oauth2/token"},
					},
				},
			},
		},
	}
}

// generateClientModule writes the generated client files into a temporary Go
// module and returns the module root.
func generateClientModule(t *testing.T, providerIR ir.ProviderIR) string {
	t.Helper()
	tmp := t.TempDir()

	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/test\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	h := Harness{OutputDir: tmp}
	if err := h.Generate(ClientFiles(providerIR)); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	return tmp
}
