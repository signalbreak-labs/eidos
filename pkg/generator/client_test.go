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
		"internal/client/client.go",
		"internal/client/errors.go",
		"internal/client/logging.go",
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
		"logging           LoggingConfig",
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

// TestClientFiles_AuthMiddleware verifies that each auth middleware correctly
// mutates a request in a generated module.
func TestClientFiles_AuthMiddleware(t *testing.T) {
	providerIR := sampleClientIR()
	tmp := generateClientModule(t, providerIR)
	writeClientSmokeTest(t, tmp)

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

// writeClientSmokeTest writes a small test that exercises each generated auth
// middleware to ensure the generated code compiles and behaves as expected.
func writeClientSmokeTest(t *testing.T, moduleRoot string) {
	t.Helper()
	testPath := filepath.Join(moduleRoot, "internal", "client", "auth_smoke_test.go")
	if err := os.MkdirAll(filepath.Dir(testPath), 0o750); err != nil {
		t.Fatalf("create client test dir: %v", err)
	}

	content := `package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAPIKeyHeaderAuth(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/things", nil)
	if err := APIKeyAuth("secret", "header", "X-API-Key")(req); err != nil {
		t.Fatalf("APIKeyAuth error: %v", err)
	}
	if got := req.Header.Get("X-API-Key"); got != "secret" {
		t.Errorf("X-API-Key = %q, want %q", got, "secret")
	}
}

func TestAPIKeyQueryAuth(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/things", nil)
	if err := APIKeyAuth("secret", "query", "api_key")(req); err != nil {
		t.Fatalf("APIKeyAuth error: %v", err)
	}
	if got := req.URL.Query().Get("api_key"); got != "secret" {
		t.Errorf("api_key = %q, want %q", got, "secret")
	}
}

func TestBasicAuth(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/things", nil)
	if err := BasicAuth("user", "pass")(req); err != nil {
		t.Fatalf("BasicAuth error: %v", err)
	}
	if !strings.HasPrefix(req.Header.Get("Authorization"), "Basic ") {
		t.Errorf("Authorization header missing Basic prefix: %q", req.Header.Get("Authorization"))
	}
}

func TestBearerAuth(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/things", nil)
	if err := BearerAuth("token123")(req); err != nil {
		t.Fatalf("BearerAuth error: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer token123" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer token123")
	}
}

func TestAPIKeyCookieAuth(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/things", nil)
	if err := APIKeyAuth("secret", "cookie", "session")(req); err != nil {
		t.Fatalf("APIKeyAuth error: %v", err)
	}
	cookies := req.Cookies()
	if len(cookies) != 1 || cookies[0].Name != "session" || cookies[0].Value != "secret" {
		t.Errorf("cookies = %v, want session=secret", cookies)
	}
}

func TestAPIErrorBodyTruncation(t *testing.T) {
	body := strings.Repeat("x", 2048)
	err := &APIError{StatusCode: http.StatusInternalServerError, Body: []byte(body)}
	got := err.Error()
	want := "API error status=500 body=" + strings.Repeat("x", 1024) + "... [truncated]"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestNewAPIErrorSmallBody(t *testing.T) {
	body := strings.NewReader("small error body")
	w := httptest.NewRecorder()
	w.WriteHeader(http.StatusBadRequest)
	_, _ = io.Copy(w, body)
	resp := w.Result()

	apiErr, err := NewAPIError(resp)
	if err != nil {
		t.Fatalf("NewAPIError error: %v", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusBadRequest)
	}
	if string(apiErr.Body) != "small error body" {
		t.Errorf("Body = %q, want %q", apiErr.Body, "small error body")
	}
	if strings.HasSuffix(string(apiErr.Body), "\n... truncated ...") {
		t.Errorf("small body should not have truncation marker")
	}
}

func TestNewAPIErrorBodyAtLimitNotTruncated(t *testing.T) {
	const max = 1 << 20
	body := strings.Repeat("x", max)
	w := httptest.NewRecorder()
	w.WriteHeader(http.StatusBadRequest)
	_, _ = io.WriteString(w, body)
	resp := w.Result()

	apiErr, err := NewAPIError(resp)
	if err != nil {
		t.Fatalf("NewAPIError error: %v", err)
	}
	if len(apiErr.Body) != max {
		t.Errorf("len(Body) = %d, want %d", len(apiErr.Body), max)
	}
	if !strings.HasPrefix(string(apiErr.Body), strings.Repeat("x", 1024)) {
		t.Errorf("Body content prefix mismatch")
	}
	if strings.HasSuffix(string(apiErr.Body), "\n... truncated ...") {
		t.Errorf("body at limit should not be truncated")
	}
}

func TestNewAPIErrorBodyOverLimitTruncated(t *testing.T) {
	const max = 1 << 20
	body := strings.Repeat("x", max+1)
	w := httptest.NewRecorder()
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = io.WriteString(w, body)
	resp := w.Result()

	apiErr, err := NewAPIError(resp)
	if err != nil {
		t.Fatalf("NewAPIError error: %v", err)
	}
	wantLen := max + len("\n... truncated ...")
	if len(apiErr.Body) != wantLen {
		t.Errorf("len(Body) = %d, want %d", len(apiErr.Body), wantLen)
	}
	if !strings.HasSuffix(string(apiErr.Body), "\n... truncated ...") {
		t.Errorf("Body missing truncation marker")
	}
}

func TestNewAPIErrorViaHTTPServer(t *testing.T) {
	const max = 1 << 20
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, strings.Repeat("z", max+100))
	}))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("server request error: %v", err)
	}

	apiErr, err := NewAPIError(resp)
	if err != nil {
		t.Fatalf("NewAPIError error: %v", err)
	}
	if apiErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusInternalServerError)
	}
	wantLen := max + len("\n... truncated ...")
	if len(apiErr.Body) != wantLen {
		t.Errorf("len(Body) = %d, want %d", len(apiErr.Body), wantLen)
	}
}

type closeTracker struct {
	io.Reader
	closed bool
}

func (c *closeTracker) Close() error {
	c.closed = true
	return nil
}

func TestNewAPIErrorClosesBody(t *testing.T) {
	body := &closeTracker{Reader: strings.NewReader("error body")}
	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     make(http.Header),
		Body:       body,
	}

	if _, err := NewAPIError(resp); err != nil {
		t.Fatalf("NewAPIError error: %v", err)
	}
	if !body.closed {
		t.Errorf("NewAPIError did not close the response body")
	}
}

func TestClientNewRequest(t *testing.T) {
	c := New(WithBaseURL("https://api.example.com"), WithUserAgent("test-agent"))
	req, err := c.NewRequest(context.Background(), http.MethodGet, "/things", nil)
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}
	if !strings.HasSuffix(req.URL.Path, "/things") {
		t.Errorf("request path = %q, want suffix /things", req.URL.Path)
	}
	if got := req.Header.Get("User-Agent"); got != "test-agent" {
		t.Errorf("User-Agent = %q, want %q", got, "test-agent")
	}
}

// TestNewRequestWithSchemes verifies per-operation security (AND resolution,
// REMAINING_GAPS §1): scheme interceptors are keyed by name and applied
// selectively via WithSchemes. With no option every configured scheme applies
// (global default); with a named subset only those apply (in registration
// order); with an empty set the request is unauthenticated.
func TestNewRequestWithSchemes(t *testing.T) {
	c := New(
		WithBaseURL("https://api.example.com"),
		WithSchemeInterceptor("apiKey", APIKeyAuth("secret", "header", "X-API-Key")),
		WithSchemeInterceptor("bearer", BearerAuth("tok")),
	)

	// No WithSchemes → global default applies every scheme interceptor.
	req, err := c.NewRequest(context.Background(), http.MethodGet, "/things", nil)
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}
	if got := req.Header.Get("X-API-Key"); got != "secret" {
		t.Errorf("default: X-API-Key = %q, want %q", got, "secret")
	}
	if got := req.Header.Get("Authorization"); got != "Bearer tok" {
		t.Errorf("default: Authorization = %q, want %q", got, "Bearer tok")
	}

	// WithSchemes("apiKey") → only the apiKey scheme applies.
	req, err = c.NewRequest(context.Background(), http.MethodGet, "/things", nil, WithSchemes("apiKey"))
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}
	if got := req.Header.Get("X-API-Key"); got != "secret" {
		t.Errorf("apiKey-only: X-API-Key = %q, want %q", got, "secret")
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("apiKey-only: Authorization = %q, want empty (bearer not selected)", got)
	}

	// WithSchemes("bearer") → only the bearer scheme applies.
	req, err = c.NewRequest(context.Background(), http.MethodGet, "/things", nil, WithSchemes("bearer"))
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}
	if got := req.Header.Get("X-API-Key"); got != "" {
		t.Errorf("bearer-only: X-API-Key = %q, want empty (apiKey not selected)", got)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer tok" {
		t.Errorf("bearer-only: Authorization = %q, want %q", got, "Bearer tok")
	}

	// WithSchemes() → unauthenticated: no scheme interceptor applies.
	req, err = c.NewRequest(context.Background(), http.MethodGet, "/things", nil, WithSchemes())
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}
	if got := req.Header.Get("X-API-Key"); got != "" {
		t.Errorf("unauthenticated: X-API-Key = %q, want empty", got)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("unauthenticated: Authorization = %q, want empty", got)
	}
}

// TestNewRequestWithSchemesRegistrationOrder verifies that a per-operation
// selection applies scheme interceptors in registration order, not in the
// order the names are passed to WithSchemes, so the generated call is stable.
func TestNewRequestWithSchemesRegistrationOrder(t *testing.T) {
	var order []string
	first := func(req *http.Request) error { order = append(order, "first"); return nil }
	second := func(req *http.Request) error { order = append(order, "second"); return nil }
	c := New(
		WithBaseURL("https://api.example.com"),
		WithSchemeInterceptor("first", first),
		WithSchemeInterceptor("second", second),
	)
	if _, err := c.NewRequest(context.Background(), http.MethodGet, "/things", nil, WithSchemes("second", "first")); err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}
	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Errorf("interceptor order = %v, want [first second] (registration order)", order)
	}
}

func TestPaginationDefaults(t *testing.T) {
	p := DefaultPagination()
	if p.Style != "offset" {
		t.Errorf("style = %q, want offset", p.Style)
	}
	if p.PageParam != "page" {
		t.Errorf("page_param = %q, want page", p.PageParam)
	}
}

func TestExtractLinkHeader(t *testing.T) {
	header := "<https://api.example.com/things?page=2>; rel=next, <https://api.example.com/things?page=5>; rel=last"
	if got := ExtractLinkHeader(header, "next"); got != "https://api.example.com/things?page=2" {
		t.Errorf("next link = %q", got)
	}
}

func TestListAllPages(t *testing.T) {
	var calls int
	pages, err := ListAllPages(context.Background(), url.Values{}, func(_ context.Context, _ url.Values) (*http.Response, error) {
		calls++
		return &http.Response{Body: http.NoBody, StatusCode: 200}, nil
	}, func(_ *http.Response, _ []byte, _ url.Values) bool {
		return calls < 2
	})
	if err != nil {
		t.Fatalf("ListAllPages error: %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
	if len(pages) != 2 {
		t.Errorf("pages = %d, want 2", len(pages))
	}
}

func TestOAuth2ClientCredentials(t *testing.T) {
	var tokenCalls int
	var gotGrantType, gotScope string
	var gotBasicAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenCalls++
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		gotGrantType = r.FormValue("grant_type")
		gotScope = r.FormValue("scope")
		gotBasicAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "abc",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer ts.Close()

	interceptor := OAuth2ClientCredentials(ts.URL, "cid", "csecret", []string{"read", "write"})
	req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/things", nil)
	if err := interceptor(req); err != nil {
		t.Fatalf("OAuth2ClientCredentials error: %v", err)
	}

	if tokenCalls != 1 {
		t.Errorf("token endpoint calls = %d, want 1", tokenCalls)
	}
	if gotGrantType != "client_credentials" {
		t.Errorf("grant_type = %q, want client_credentials", gotGrantType)
	}
	if gotScope != "read write" {
		t.Errorf("scope = %q, want read write", gotScope)
	}
	if !strings.HasPrefix(gotBasicAuth, "Basic ") {
		t.Errorf("token endpoint Authorization = %q, want Basic prefix", gotBasicAuth)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer abc" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer abc")
	}

	// A second call should reuse the cached token without another token request.
	req2, _ := http.NewRequest(http.MethodGet, "https://api.example.com/things", nil)
	if err := interceptor(req2); err != nil {
		t.Fatalf("second OAuth2ClientCredentials error: %v", err)
	}
	if tokenCalls != 1 {
		t.Errorf("cached token endpoint calls = %d, want 1", tokenCalls)
	}
	if got := req2.Header.Get("Authorization"); got != "Bearer abc" {
		t.Errorf("second Authorization = %q, want %q", got, "Bearer abc")
	}
}

func TestOAuth2ClientCredentialsWithHTTPClient(t *testing.T) {
	var called bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Basic ") {
			t.Errorf("token request Authorization = %q, want Basic prefix", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "tok",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer ts.Close()

	custom := &http.Client{Timeout: 5 * time.Second}
	interceptor := OAuth2ClientCredentialsWithHTTPClient(ts.URL, "cid", "csecret", []string{"read"}, custom)
	req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/things", nil)
	if err := interceptor(req); err != nil {
		t.Fatalf("OAuth2ClientCredentialsWithHTTPClient error: %v", err)
	}
	if !called {
		t.Errorf("token endpoint was not called")
	}
	if got := req.Header.Get("Authorization"); got != "Bearer tok" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer tok")
	}
}

func TestOAuth2Password(t *testing.T) {
	var tokenCalls int
	var gotGrantType, gotUsername, gotPassword, gotScope string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenCalls++
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		gotGrantType = r.FormValue("grant_type")
		gotUsername = r.FormValue("username")
		gotPassword = r.FormValue("password")
		gotScope = r.FormValue("scope")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "pw-tok",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer ts.Close()

	interceptor := OAuth2Password(ts.URL, "alice", "s3cret", "cid", "csecret", []string{"read"})
	req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/things", nil)
	if err := interceptor(req); err != nil {
		t.Fatalf("OAuth2Password error: %v", err)
	}

	if tokenCalls != 1 {
		t.Errorf("token endpoint calls = %d, want 1", tokenCalls)
	}
	if gotGrantType != "password" {
		t.Errorf("grant_type = %q, want password", gotGrantType)
	}
	if gotUsername != "alice" || gotPassword != "s3cret" {
		t.Errorf("username/password = %q/%q, want alice/s3cret", gotUsername, gotPassword)
	}
	if gotScope != "read" {
		t.Errorf("scope = %q, want read", gotScope)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer pw-tok" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer pw-tok")
	}

	// A second call reuses the cached token without another token request.
	req2, _ := http.NewRequest(http.MethodGet, "https://api.example.com/things", nil)
	if err := interceptor(req2); err != nil {
		t.Fatalf("second OAuth2Password error: %v", err)
	}
	if tokenCalls != 1 {
		t.Errorf("cached token endpoint calls = %d, want 1", tokenCalls)
	}
}

func TestOAuth2AuthorizationCodeRefresh(t *testing.T) {
	var tokenCalls int
	var gotGrantType, gotRefreshToken string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenCalls++
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		gotGrantType = r.FormValue("grant_type")
		gotRefreshToken = r.FormValue("refresh_token")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "refreshed-tok",
			"token_type":    "Bearer",
			"expires_in":    3600,
			"refresh_token": "rotated-refresh",
		})
	}))
	defer ts.Close()

	interceptor := OAuth2AuthorizationCodeRefresh(ts.URL, "initial-refresh", "cid", "csecret", nil)
	req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/things", nil)
	if err := interceptor(req); err != nil {
		t.Fatalf("OAuth2AuthorizationCodeRefresh error: %v", err)
	}

	if tokenCalls != 1 {
		t.Errorf("token endpoint calls = %d, want 1", tokenCalls)
	}
	if gotGrantType != "refresh_token" {
		t.Errorf("grant_type = %q, want refresh_token", gotGrantType)
	}
	if gotRefreshToken != "initial-refresh" {
		t.Errorf("refresh_token = %q, want initial-refresh", gotRefreshToken)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer refreshed-tok" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer refreshed-tok")
	}
}

func TestOpenIDConnect(t *testing.T) {
	var discoveryCalls, tokenCalls int
	var gotGrantType string
	mux := http.NewServeMux()
	var ts *httptest.Server
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		discoveryCalls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":         ts.URL,
			"token_endpoint": ts.URL + "/oauth/token",
		})
	})
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		tokenCalls++
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		gotGrantType = r.FormValue("grant_type")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "oidc-tok",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	})
	ts = httptest.NewServer(mux)
	defer ts.Close()

	interceptor := OpenIDConnect(ts.URL+"/.well-known/openid-configuration", "", "cid", "csecret", nil)
	req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/things", nil)
	if err := interceptor(req); err != nil {
		t.Fatalf("OpenIDConnect error: %v", err)
	}

	if discoveryCalls != 1 {
		t.Errorf("discovery calls = %d, want 1", discoveryCalls)
	}
	if tokenCalls != 1 {
		t.Errorf("token endpoint calls = %d, want 1", tokenCalls)
	}
	if gotGrantType != "client_credentials" {
		t.Errorf("grant_type = %q, want client_credentials", gotGrantType)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer oidc-tok" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer oidc-tok")
	}

	// A second call reuses the discovered endpoint and the cached token.
	req2, _ := http.NewRequest(http.MethodGet, "https://api.example.com/things", nil)
	if err := interceptor(req2); err != nil {
		t.Fatalf("second OpenIDConnect error: %v", err)
	}
	if discoveryCalls != 1 || tokenCalls != 1 {
		t.Errorf("cached calls: discovery = %d, token = %d, want 1 and 1", discoveryCalls, tokenCalls)
	}
}

func TestOpenIDConnectTokenURLOverrideSkipsDiscovery(t *testing.T) {
	var tokenCalls int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenCalls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "override-tok",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer ts.Close()

	// A non-empty token URL override is used directly; the (unreachable)
	// discovery URL must not be fetched.
	interceptor := OpenIDConnect("http://127.0.0.1:1/unreachable", ts.URL, "cid", "csecret", nil)
	req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/things", nil)
	if err := interceptor(req); err != nil {
		t.Fatalf("OpenIDConnect with override error: %v", err)
	}
	if tokenCalls != 1 {
		t.Errorf("token endpoint calls = %d, want 1", tokenCalls)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer override-tok" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer override-tok")
	}
}
`
	if err := os.WriteFile(testPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write client smoke test: %v", err)
	}
}
