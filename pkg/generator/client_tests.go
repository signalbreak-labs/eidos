package generator

// client_tests.go emits internal/client/client_test.go: an httptest-based unit
// test suite exercising the generated API client's public surface (New,
// NewRequest, Do, NewAPIError, IsNotFound, IsRetryable, ExtractLinkHeader,
// ListAllPages, DoWithRetry, DefaultRetryPolicy, DefaultBackoff,
// DefaultPagination). It is a pure addition alongside the other template-emitted
// client files and requires no changes to generated client code, lifting
// internal/client coverage from ~0%.
//
// The tests are deterministic (fixed inputs, no randomness on the test side;
// DefaultBackoff's internal jitter is bounded by robust range assertions) and
// lint-clean under .golangci.yml v2: every *http.Response body from Do is
// closed (bodyclose), every call's error is checked (errcheck), and all HTTP
// requests carry a context (noctx). Retry tests use a zero backoff and
// maxRetries tuned per case so the suite is fast.

// clientTestGoFile emits the in-package client_test.go.
func clientTestGoFile(cfg clientConfig) File {
	return Template("internal/client/client_test.go", clientTestTemplate, cfg)
}

// authTestGoFile emits internal/client/auth_test.go, exercising the security-
// scheme interceptor factories (API key, basic, bearer, OAuth2 client
// credentials / password / refresh, OpenID Connect discovery). Emitted only
// when the provider declares security schemes (alongside auth.go).
func authTestGoFile(cfg clientConfig) File {
	return Template("internal/client/auth_test.go", authTestTemplate, cfg)
}

// loggingTestGoFile emits internal/client/logging_test.go, exercising the trace
// logging round-tripper (header/URL redaction, body capture + replay,
// truncation, close safety, disabled short-circuit). Always emitted alongside
// logging.go.
func loggingTestGoFile(cfg clientConfig) File {
	return Template("internal/client/logging_test.go", loggingTestTemplate, cfg)
}

const clientTestTemplate = `package client

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// zeroBackoff makes retry tests instantaneous.
func zeroBackoff(int) time.Duration { return 0 }

// closeResp closes a response body, discarding the error so callers can use it
// in a defer without tripping errcheck. Shared across the package's test files.
func closeResp(r *http.Response) { _ = r.Body.Close() }

// failingReadCloser returns an error on Read to exercise error paths.
type failingReadCloser struct{ closed bool }

func (failingReadCloser) Read([]byte) (int, error) { return 0, errors.New("read boom") }
func (f *failingReadCloser) Close() error           { f.closed = true; return nil }

// trackingCloser records whether Close was called.
type trackingCloser struct {
	body    []byte
	closed  bool
	readErr error
}

func (t *trackingCloser) Read(p []byte) (int, error) {
	if t.readErr != nil {
		return 0, t.readErr
	}
	if len(t.body) == 0 {
		return 0, io.EOF
	}
	n := copy(p, t.body)
	t.body = t.body[n:]
	return n, nil
}

func (t *trackingCloser) Close() error { t.closed = true; return nil }

func TestNew_Defaults(t *testing.T) {
	c := New()
	if c == nil {
		t.Fatal("New() returned nil")
	}
	if c.BaseURL() != {{.BaseURL}} {
		t.Fatalf("BaseURL() = %q, want %q", c.BaseURL(), {{.BaseURL}})
	}
}

func TestNew_WithOptions(t *testing.T) {
	hc := &http.Client{Timeout: 5 * time.Second}
	c := New(WithBaseURL("https://example.test/api"), WithUserAgent("test-agent"), WithHTTPClient(hc))
	if c.BaseURL() != "https://example.test/api" {
		t.Fatalf("BaseURL() = %q", c.BaseURL())
	}
	req, err := c.NewRequest(context.Background(), http.MethodGet, "/x", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if got := req.Header.Get("User-Agent"); got != "test-agent" {
		t.Fatalf("User-Agent = %q, want %q", got, "test-agent")
	}
}

func TestNewRequest_MethodPath(t *testing.T) {
	c := New(WithBaseURL("https://example.test"))
	req, err := c.NewRequest(context.Background(), http.MethodGet, "/things/1", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if req.Method != http.MethodGet {
		t.Fatalf("Method = %q", req.Method)
	}
	if req.URL.String() != "https://example.test/things/1" {
		t.Fatalf("URL = %q", req.URL.String())
	}
	if got := req.Header.Get("User-Agent"); got != {{.UserAgent}} {
		t.Fatalf("User-Agent = %q, want %q", got, {{.UserAgent}})
	}
}

func TestNewRequest_RejectsDotSegmentTraversal(t *testing.T) {
	c := New(WithBaseURL("https://example.test/api"))
	// A path-param value of ".." resolves out of the base path prefix when it
	// climbs past the base path's own segments (L-4).
	if _, err := c.NewRequest(context.Background(), http.MethodGet, "/things/../..", nil); err == nil {
		t.Fatal("NewRequest with a traversing path should fail")
	}
	if _, err := c.NewRequest(context.Background(), http.MethodGet, "/things/../../admin", nil); err == nil {
		t.Fatal("NewRequest with a multi-segment traversal should fail")
	}
}

func TestNewRequest_AcceptsPathWithinBase(t *testing.T) {
	c := New(WithBaseURL("https://example.test/api"))
	req, err := c.NewRequest(context.Background(), http.MethodGet, "/things/1", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if req.URL.String() != "https://example.test/api/things/1" {
		t.Fatalf("URL = %q", req.URL.String())
	}
	// A sibling prefix is joined under the base path, not treated as a
	// replacement, so it stays within the base path.
	req, err = c.NewRequest(context.Background(), http.MethodGet, "/apix/1", nil)
	if err != nil {
		t.Fatalf("NewRequest with a sibling prefix: %v", err)
	}
	if req.URL.String() != "https://example.test/api/apix/1" {
		t.Fatalf("URL = %q", req.URL.String())
	}
}

func TestPathWithin(t *testing.T) {
	cases := []struct {
		base, p string
		want    bool
	}{
		{"", "/things", true},
		{"/", "/things", true},
		{"/api", "/api", true},
		{"/api", "/api/things", true},
		{"/api", "/things", false},
		{"/api", "/apix", false},
		{"/api", "/", false},
	}
	for _, tc := range cases {
		if got := pathWithin(tc.base, tc.p); got != tc.want {
			t.Errorf("pathWithin(%q, %q) = %v, want %v", tc.base, tc.p, got, tc.want)
		}
	}
}

func TestNewRequest_WithBody(t *testing.T) {
	c := New(WithBaseURL("https://example.test"))
	body := bytes.NewReader([]byte("payload"))
	req, err := c.NewRequest(context.Background(), http.MethodPost, "/things", body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if req.Method != http.MethodPost {
		t.Fatalf("Method = %q", req.Method)
	}
	got, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "payload" {
		t.Fatalf("body = %q", got)
	}
}

func TestNewRequest_Interceptors(t *testing.T) {
	c := New(
		WithBaseURL("https://example.test"),
		WithInterceptors(func(r *http.Request) error {
			r.Header.Set("X-Custom", "yes")
			return nil
		}),
	)
	req, err := c.NewRequest(context.Background(), http.MethodGet, "/x", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if got := req.Header.Get("X-Custom"); got != "yes" {
		t.Fatalf("X-Custom = %q", got)
	}
}

func TestNewRequest_InterceptorsError(t *testing.T) {
	c := New(
		WithBaseURL("https://example.test"),
		WithInterceptors(func(*http.Request) error { return errors.New("nope") }),
	)
	if _, err := c.NewRequest(context.Background(), http.MethodGet, "/x", nil); err == nil {
		t.Fatal("expected interceptor error, got nil")
	}
}

func TestNewRequest_SchemeInterceptors(t *testing.T) {
	called := map[string]bool{}
	mk := func(name string) RequestInterceptor {
		return func(r *http.Request) error {
			called[name] = true
			r.Header.Set("X-Scheme", name)
			return nil
		}
	}
	c := New(
		WithBaseURL("https://example.test"),
		WithSchemeInterceptor("bearer", mk("bearer")),
		WithSchemeInterceptor("apiKey", mk("apiKey")),
	)

	// Default: every configured scheme interceptor applies (global security).
	req, err := c.NewRequest(context.Background(), http.MethodGet, "/x", nil)
	if err != nil {
		t.Fatalf("NewRequest default: %v", err)
	}
	if !called["bearer"] || !called["apiKey"] {
		t.Fatalf("expected both scheme interceptors, got %v", called)
	}
	if got := req.Header.Get("X-Scheme"); got != "apiKey" {
		t.Fatalf("last scheme header = %q, want apiKey (registration order)", got)
	}

	// WithSchemes restricts to the named scheme (per-operation AND resolution).
	called = map[string]bool{}
	if _, err := c.NewRequest(context.Background(), http.MethodGet, "/x", nil, WithSchemes("bearer")); err != nil {
		t.Fatalf("NewRequest WithSchemes: %v", err)
	}
	if !called["bearer"] || called["apiKey"] {
		t.Fatalf("expected only bearer, got %v", called)
	}

	// Empty WithSchemes marks the operation unauthenticated.
	called = map[string]bool{}
	if _, err := c.NewRequest(context.Background(), http.MethodGet, "/x", nil, WithSchemes()); err != nil {
		t.Fatalf("NewRequest unauthenticated: %v", err)
	}
	if len(called) != 0 {
		t.Fatalf("expected no scheme interceptors for unauthenticated op, got %v", called)
	}
}

func TestNewRequest_UnknownSchemeNoOp(t *testing.T) {
	c := New(
		WithBaseURL("https://example.test"),
		WithSchemeInterceptor("bearer", func(*http.Request) error { return nil }),
	)
	// A WithSchemes naming a scheme that was never registered applies nothing
	// (no error, no interceptor).
	req, err := c.NewRequest(context.Background(), http.MethodGet, "/x", nil, WithSchemes("missing"))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if req.Header.Get("X-Scheme") != "" {
		t.Fatalf("expected no scheme header, got %q", req.Header.Get("X-Scheme"))
	}
}

func TestDo_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)
	c := New(WithBaseURL(srv.URL), WithRetry(0, DefaultRetryPolicy, zeroBackoff))
	req, err := c.NewRequest(context.Background(), http.MethodGet, "/", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer closeResp(resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d", resp.StatusCode)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "ok" {
		t.Fatalf("body = %q", got)
	}
}

func TestDo_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close() // closed server -> connection refused
	c := New(WithBaseURL(srv.URL), WithRetry(0, DefaultRetryPolicy, zeroBackoff))
	req, err := c.NewRequest(context.Background(), http.MethodGet, "/", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if _, err := c.Do(req); err == nil {
		t.Fatal("expected transport error, got nil")
	}
}

func TestDo_NonSuccessReturnsResponse(t *testing.T) {
	// Do does not turn 4xx/5xx into errors; constructs own non-success handling
	// via NewAPIError. maxRetries=0 so 404 is not retried.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("nope"))
	}))
	t.Cleanup(srv.Close)
	c := New(WithBaseURL(srv.URL), WithRetry(0, DefaultRetryPolicy, zeroBackoff))
	req, err := c.NewRequest(context.Background(), http.MethodGet, "/", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do returned error on 404: %v", err)
	}
	defer closeResp(resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("StatusCode = %d, want 404", resp.StatusCode)
	}
}

func TestDo_RetryThenSuccess(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusBadGateway) // retryable 5xx
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)
	c := New(WithBaseURL(srv.URL), WithRetry(2, DefaultRetryPolicy, zeroBackoff))
	req, err := c.NewRequest(context.Background(), http.MethodGet, "/", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer closeResp(resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want 200 after retry", resp.StatusCode)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestDo_RetryExhaustedReturnsLastResponse(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError) // always retryable
	}))
	t.Cleanup(srv.Close)
	c := New(WithBaseURL(srv.URL), WithRetry(1, DefaultRetryPolicy, zeroBackoff))
	req, err := c.NewRequest(context.Background(), http.MethodGet, "/", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer closeResp(resp)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("StatusCode = %d, want 500", resp.StatusCode)
	}
	if calls != 2 { // initial + 1 retry
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestDo_NoRetryPolicyDirect(t *testing.T) {
	// A nil retry policy bypasses DoWithRetry entirely.
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	c := New(WithBaseURL(srv.URL), WithRetry(5, nil, zeroBackoff))
	req, err := c.NewRequest(context.Background(), http.MethodGet, "/", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer closeResp(resp)
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (no retry with nil policy)", calls)
	}
}

func TestDo_RetryResendsBody(t *testing.T) {
	// A retried request with a body must resend the full body on each attempt.
	var lastBody string
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		b, _ := io.ReadAll(r.Body)
		lastBody = string(b)
		if calls == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	c := New(WithBaseURL(srv.URL), WithRetry(2, DefaultRetryPolicy, zeroBackoff))
	req, err := c.NewRequest(context.Background(), http.MethodPost, "/", bytes.NewReader([]byte("payload")))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer closeResp(resp)
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if lastBody != "payload" {
		t.Fatalf("retry body = %q, want %q", lastBody, "payload")
	}
}

func TestNewAPIError_ValidJSON(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader([]byte("{\"message\":\"bad input\"}"))),
	}
	apiErr, err := NewAPIError(resp)
	if err != nil {
		t.Fatalf("NewAPIError: %v", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("StatusCode = %d", apiErr.StatusCode)
	}
	if !strings.Contains(apiErr.Error(), "bad input") {
		t.Fatalf("Error() = %q, want it to contain body", apiErr.Error())
	}
	if !strings.Contains(apiErr.Error(), "status=400") {
		t.Fatalf("Error() = %q, want status=400", apiErr.Error())
	}
}

func TestNewAPIError_ClosesBody(t *testing.T) {
	body := &trackingCloser{body: []byte("x")}
	resp := &http.Response{StatusCode: http.StatusNotFound, Body: body}
	if _, err := NewAPIError(resp); err != nil {
		t.Fatalf("NewAPIError: %v", err)
	}
	if !body.closed {
		t.Fatal("response body was not closed")
	}
}

func TestNewAPIError_ReadError(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusNotFound,
		Body:       &failingReadCloser{},
	}
	if _, err := NewAPIError(resp); err == nil {
		t.Fatal("expected read error, got nil")
	}
}

func TestNewAPIError_TruncatesDisplay(t *testing.T) {
	big := strings.Repeat("x", maxAPIErrorDisplayBytes+50)
	resp := &http.Response{StatusCode: http.StatusInternalServerError, Body: io.NopCloser(bytes.NewReader([]byte(big)))}
	apiErr, err := NewAPIError(resp)
	if err != nil {
		t.Fatalf("NewAPIError: %v", err)
	}
	if !strings.Contains(apiErr.Error(), "[truncated]") {
		t.Fatalf("Error() should truncate display, got len=%d", len(apiErr.Error()))
	}
}

func TestNewAPIError_BodyTruncatedOverLimit(t *testing.T) {
	// Bodies larger than maxAPIErrorBodyBytes are capped and tagged with a
	// truncation marker so enormous payloads are not retained in full.
	big := strings.Repeat("x", maxAPIErrorBodyBytes+100)
	resp := &http.Response{StatusCode: http.StatusInternalServerError, Body: io.NopCloser(bytes.NewReader([]byte(big)))}
	apiErr, err := NewAPIError(resp)
	if err != nil {
		t.Fatalf("NewAPIError: %v", err)
	}
	wantLen := maxAPIErrorBodyBytes + len("\n... truncated ...")
	if len(apiErr.Body) != wantLen {
		t.Fatalf("len(Body) = %d, want %d", len(apiErr.Body), wantLen)
	}
	if !strings.HasSuffix(string(apiErr.Body), "\n... truncated ...") {
		t.Fatalf("Body missing truncation marker, suffix=%q", string(apiErr.Body)[maxAPIErrorBodyBytes-10:])
	}
}

func TestNewAPIError_BodyAtLimitNotTruncated(t *testing.T) {
	// A body exactly at the cap is stored in full without a truncation marker.
	body := strings.Repeat("x", maxAPIErrorBodyBytes)
	resp := &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(bytes.NewReader([]byte(body)))}
	apiErr, err := NewAPIError(resp)
	if err != nil {
		t.Fatalf("NewAPIError: %v", err)
	}
	if len(apiErr.Body) != maxAPIErrorBodyBytes {
		t.Fatalf("len(Body) = %d, want %d", len(apiErr.Body), maxAPIErrorBodyBytes)
	}
	if strings.HasSuffix(string(apiErr.Body), "\n... truncated ...") {
		t.Fatalf("body at limit should not be truncated")
	}
}

func TestIsNotFound(t *testing.T) {
	if !IsNotFound(&APIError{StatusCode: http.StatusNotFound}) {
		t.Error("404 APIError should be not-found")
	}
	if IsNotFound(&APIError{StatusCode: http.StatusInternalServerError}) {
		t.Error("500 APIError should not be not-found")
	}
	if IsNotFound(errors.New("plain")) {
		t.Error("non-APIError should not be not-found")
	}
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"5xx", &APIError{StatusCode: http.StatusInternalServerError}, true},
		{"429", &APIError{StatusCode: http.StatusTooManyRequests}, true},
		{"404", &APIError{StatusCode: http.StatusNotFound}, false},
		{"transport", errors.New("connection refused"), true},
		{"canceled", context.Canceled, false},
		{"deadline", context.DeadlineExceeded, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsRetryable(tc.err); got != tc.want {
				t.Fatalf("IsRetryable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestExtractLinkHeader(t *testing.T) {
	header := "<https://example.test/page2>; rel=\"next\", <https://example.test/page1>; rel=\"prev\""
	if got := ExtractLinkHeader(header, "next"); got != "https://example.test/page2" {
		t.Fatalf("next = %q", got)
	}
	if got := ExtractLinkHeader(header, "prev"); got != "https://example.test/page1" {
		t.Fatalf("prev = %q", got)
	}
	if got := ExtractLinkHeader(header, "missing"); got != "" {
		t.Fatalf("missing = %q, want empty", got)
	}
	if got := ExtractLinkHeader("", "next"); got != "" {
		t.Fatalf("empty header = %q, want empty", got)
	}
}

func TestListAllPages_SinglePage(t *testing.T) {
	fetch := func(context.Context, url.Values) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte("[1]")))}, nil
	}
	pages, err := ListAllPages(context.Background(), nil, fetch, func(*http.Response, []byte, url.Values) bool {
		return false
	})
	if err != nil {
		t.Fatalf("ListAllPages: %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("pages = %d, want 1", len(pages))
	}
	if string(pages[0]) != "[1]" {
		t.Fatalf("page = %q", pages[0])
	}
}

func TestListAllPages_MultiplePages(t *testing.T) {
	page := 0
	fetch := func(_ context.Context, params url.Values) (*http.Response, error) {
		page++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte("page")))}, nil
	}
	next := func(resp *http.Response, body []byte, params url.Values) bool {
		pageStr := params.Get("page")
		if pageStr == "" {
			params.Set("page", "2")
			return true
		}
		return false // stop after the second page
	}
	pages, err := ListAllPages(context.Background(), url.Values{}, fetch, next)
	if err != nil {
		t.Fatalf("ListAllPages: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("pages = %d, want 2", len(pages))
	}
	if page != 2 {
		t.Fatalf("fetch calls = %d, want 2", page)
	}
}

func TestListAllPages_FetchError(t *testing.T) {
	fetch := func(context.Context, url.Values) (*http.Response, error) {
		return nil, errors.New("fetch boom")
	}
	if _, err := ListAllPages(context.Background(), nil, fetch, nil); err == nil {
		t.Fatal("expected fetch error, got nil")
	}
}

func TestListAllPages_BodyReadError(t *testing.T) {
	fetch := func(context.Context, url.Values) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: &failingReadCloser{}}, nil
	}
	if _, err := ListAllPages(context.Background(), nil, fetch, nil); err == nil {
		t.Fatal("expected body read error, got nil")
	}
}

// TestListAllPages_LoopBackDetection verifies that a next callback which returns
// true without advancing the pagination parameters (a server echoing the same
// cursor) stops the loop instead of issuing an identical request forever (M-9).
func TestListAllPages_LoopBackDetection(t *testing.T) {
	calls := 0
	fetch := func(_ context.Context, _ url.Values) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte("page")))}, nil
	}
	next := func(_ *http.Response, _ []byte, params url.Values) bool {
		// Echo the same cursor back: the server never advances.
		params.Set("cursor", "abc")
		return true
	}
	pages, err := ListAllPages(context.Background(), url.Values{}, fetch, next)
	if err != nil {
		t.Fatalf("ListAllPages: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("pages = %d, want 2 (first page plus one loop-back page)", len(pages))
	}
	if calls != 2 {
		t.Fatalf("fetch calls = %d, want 2", calls)
	}
}

// TestListAllPages_UnchangedParamsStillPaginates verifies that link_header-style
// pagination, which advances via a response-embedded URL rather than the
// parameters, is not stopped by the loop-back guard (M-9).
func TestListAllPages_UnchangedParamsStillPaginates(t *testing.T) {
	page := 0
	fetch := func(context.Context, url.Values) (*http.Response, error) {
		page++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte("page")))}, nil
	}
	next := func(*http.Response, []byte, url.Values) bool {
		return page < 3 // stop after three pages
	}
	pages, err := ListAllPages(context.Background(), url.Values{}, fetch, next)
	if err != nil {
		t.Fatalf("ListAllPages: %v", err)
	}
	if len(pages) != 3 {
		t.Fatalf("pages = %d, want 3", len(pages))
	}
}

// TestListAllPages_MaxPageBound verifies that a server which never stops
// returning a next page is bounded by maxPages instead of looping forever (M-9).
func TestListAllPages_MaxPageBound(t *testing.T) {
	fetch := func(context.Context, url.Values) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte("page")))}, nil
	}
	i := 0
	next := func(_ *http.Response, _ []byte, params url.Values) bool {
		// Advance the cursor each time so the loop-back guard does not fire; only
		// the max-page bound can stop this.
		i++
		params.Set("cursor", strconv.Itoa(i))
		return true
	}
	_, err := ListAllPages(context.Background(), url.Values{}, fetch, next)
	if err == nil {
		t.Fatal("expected max-page bound error, got nil")
	}
	if !strings.Contains(err.Error(), "pages") {
		t.Fatalf("error = %q, want page-bound message", err)
	}
}

func TestDefaultPagination(t *testing.T) {
	p := DefaultPagination()
	if p.Style != {{.PaginationStyle}} {
		t.Fatalf("Style = %q, want %q", p.Style, {{.PaginationStyle}})
	}
	if p.PageParam != {{.PageParam}} {
		t.Fatalf("PageParam = %q, want %q", p.PageParam, {{.PageParam}})
	}
	if p.PerPageParam != {{.PerPageParam}} {
		t.Fatalf("PerPageParam = %q, want %q", p.PerPageParam, {{.PerPageParam}})
	}
	if p.NextLinkRel != {{.NextLinkRel}} {
		t.Fatalf("NextLinkRel = %q, want %q", p.NextLinkRel, {{.NextLinkRel}})
	}
	if p.CursorField != {{.CursorField}} {
		t.Fatalf("CursorField = %q, want %q", p.CursorField, {{.CursorField}})
	}
}

func TestDefaultRetryPolicy(t *testing.T) {
	tests := []struct {
		name string
		resp *http.Response
		err  error
		want bool
	}{
		{"nil-err-200", &http.Response{StatusCode: 200}, nil, false},
		{"nil-err-404", &http.Response{StatusCode: 404}, nil, false},
		{"5xx", &http.Response{StatusCode: 500}, nil, true},
		{"429", &http.Response{StatusCode: 429}, nil, true},
		{"transport", nil, errors.New("boom"), true},
		{"canceled", nil, context.Canceled, false},
		{"deadline", nil, context.DeadlineExceeded, false},
		{"nil-nil", nil, nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := DefaultRetryPolicy(tc.resp, tc.err); got != tc.want {
				t.Fatalf("DefaultRetryPolicy = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDefaultBackoff_Bounded(t *testing.T) {
	minWait := {{.RetryWaitMin}}
	maxWait := {{.RetryWaitMax}}
	for attempt := 0; attempt <= 12; attempt++ {
		d := DefaultBackoff(attempt)
		// base is clamped to [minWait, maxWait]; d = base + jitter where jitter in [0, base).
		if d < minWait {
			t.Fatalf("attempt %d: backoff %s < min %s", attempt, d, minWait)
		}
		if d >= 2*maxWait {
			t.Fatalf("attempt %d: backoff %s >= 2*max %s", attempt, d, 2*maxWait)
		}
	}
}

func TestDoWithRetry_StopsOnSuccess(t *testing.T) {
	var calls int
	do := func() (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(nil))}, nil
	}
	resp, err := DoWithRetry(context.Background(), do, 3, DefaultRetryPolicy, zeroBackoff)
	if err != nil {
		t.Fatalf("DoWithRetry: %v", err)
	}
	defer closeResp(resp)
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestDoWithRetry_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	do := func() (*http.Response, error) {
		return &http.Response{StatusCode: 500, Body: io.NopCloser(bytes.NewReader(nil))}, nil
	}
	// A small non-zero backoff makes the select deterministic: the already-closed
	// ctx.Done() is immediately ready while time.After(10ms) is not, so
	// DoWithRetry always observes the cancellation instead of racing the
	// (zero-delay) timer and potentially exhausting maxRetries first.
	slowBackoff := func(int) time.Duration { return 10 * time.Millisecond }
	if _, err := DoWithRetry(ctx, do, 3, DefaultRetryPolicy, slowBackoff); err == nil {
		t.Fatal("expected context error, got nil")
	}
}
`

const authTestTemplate = `package client

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

func TestAPIKeyAuth(t *testing.T) {
	t.Run("header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "https://example.test/x", nil)
		if err := APIKeyAuth("secret", "header", "X-API-Key")(req); err != nil {
			t.Fatalf("interceptor: %v", err)
		}
		if got := req.Header.Get("X-API-Key"); got != "secret" {
			t.Fatalf("X-API-Key = %q", got)
		}
	})
	t.Run("query", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "https://example.test/x", nil)
		if err := APIKeyAuth("secret", "query", "api_key")(req); err != nil {
			t.Fatalf("interceptor: %v", err)
		}
		if got := req.URL.Query().Get("api_key"); got != "secret" {
			t.Fatalf("api_key = %q", got)
		}
	})
	t.Run("cookie", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "https://example.test/x", nil)
		if err := APIKeyAuth("secret", "cookie", "session")(req); err != nil {
			t.Fatalf("interceptor: %v", err)
		}
		cookies := req.Cookies()
		if len(cookies) != 1 || cookies[0].Name != "session" || cookies[0].Value != "secret" {
			t.Fatalf("cookies = %+v", cookies)
		}
	})
	t.Run("invalid", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "https://example.test/x", nil)
		if err := APIKeyAuth("secret", "body", "k")(req); err == nil {
			t.Fatal("expected error for unsupported location, got nil")
		}
	})
}

func TestBasicAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://example.test/x", nil)
	if err := BasicAuth("user", "pass")(req); err != nil {
		t.Fatalf("interceptor: %v", err)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:pass"))
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("Authorization = %q, want %q", got, want)
	}
}

func TestBearerAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://example.test/x", nil)
	if err := BearerAuth("abc123")(req); err != nil {
		t.Fatalf("interceptor: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer abc123" {
		t.Fatalf("Authorization = %q", got)
	}
}

func TestParseScopes(t *testing.T) {
	if got := ParseScopes("a b  c"); len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("ParseScopes = %+v", got)
	}
	if got := ParseScopes(""); len(got) != 0 {
		t.Fatalf("ParseScopes(\"\") = %+v, want empty", got)
	}
	if got := ParseScopes("single"); len(got) != 1 || got[0] != "single" {
		t.Fatalf("ParseScopes = %+v", got)
	}
}

// tokenRecorder captures the last token request's form and Basic-auth header.
type tokenRecorder struct {
	form  url.Values
	basic string
}

func newTokenRecorder() *tokenRecorder { return &tokenRecorder{} }

func (tr *tokenRecorder) handler(status int, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		tr.form, _ = url.ParseQuery(string(b))
		tr.basic = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

func applyInterceptor(t *testing.T, ic RequestInterceptor) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "https://example.test/resource", nil)
	if err := ic(req); err != nil {
		t.Fatalf("interceptor: %v", err)
	}
	return req
}

func TestOAuth2ClientCredentials(t *testing.T) {
	var rec tokenRecorder
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		b, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		rec.form, _ = url.ParseQuery(string(b))
		rec.basic = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{\"access_token\":\"tok\",\"token_type\":\"Bearer\",\"expires_in\":3600}"))
	}))
	t.Cleanup(srv.Close)

	ic := OAuth2ClientCredentialsWithHTTPClient(srv.URL, "cid", "sec", []string{"read", "write"}, srv.Client())

	req := applyInterceptor(t, ic)
	if got := req.Header.Get("Authorization"); got != "Bearer tok" {
		t.Fatalf("Authorization = %q, want Bearer tok", got)
	}
	if got := rec.form.Get("grant_type"); got != "client_credentials" {
		t.Fatalf("grant_type = %q", got)
	}
	if got := rec.form.Get("scope"); got != "read write" {
		t.Fatalf("scope = %q, want %q", got, "read write")
	}
	wantBasic := "Basic " + base64.StdEncoding.EncodeToString([]byte("cid:sec"))
	if rec.basic != wantBasic {
		t.Fatalf("Basic auth = %q, want %q", rec.basic, wantBasic)
	}

	// Cached: a second interceptor call within the expiry window must not hit
	// the token endpoint again.
	_ = applyInterceptor(t, ic)
	if calls.Load() != 1 {
		t.Fatalf("token requests = %d, want 1 (cached on second call)", calls.Load())
	}
}

func TestOAuth2Password(t *testing.T) {
	rec := newTokenRecorder()
	srv := httptest.NewServer(rec.handler(http.StatusOK, "{\"access_token\":\"tok\",\"expires_in\":3600}"))
	t.Cleanup(srv.Close)

	ic := OAuth2PasswordWithHTTPClient(srv.URL, "bob", "pw", "cid", "sec", nil, srv.Client())
	req := applyInterceptor(t, ic)
	if got := req.Header.Get("Authorization"); got != "Bearer tok" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := rec.form.Get("grant_type"); got != "password" {
		t.Fatalf("grant_type = %q", got)
	}
	if got := rec.form.Get("username"); got != "bob" {
		t.Fatalf("username = %q", got)
	}
	if got := rec.form.Get("password"); got != "pw" {
		t.Fatalf("password = %q", got)
	}
}

func TestOAuth2AuthorizationCodeRefresh_RotatesToken(t *testing.T) {
	var rec tokenRecorder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		rec.form, _ = url.ParseQuery(string(b))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Rotate the refresh token in the response.
		_, _ = w.Write([]byte("{\"access_token\":\"tok\",\"expires_in\":3600,\"refresh_token\":\"rotated\"}"))
	}))
	t.Cleanup(srv.Close)

	ic := OAuth2AuthorizationCodeRefreshWithHTTPClient(srv.URL, "initial-rt", "cid", "sec", nil, srv.Client())
	_ = applyInterceptor(t, ic)
	if got := rec.form.Get("grant_type"); got != "refresh_token" {
		t.Fatalf("grant_type = %q", got)
	}
	if got := rec.form.Get("refresh_token"); got != "initial-rt" {
		t.Fatalf("refresh_token = %q, want initial-rt", got)
	}
}

func TestOAuth2TokenRequest_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		_ = r.Body.Close()
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	ic := OAuth2ClientCredentialsWithHTTPClient(srv.URL, "cid", "sec", nil, srv.Client())
	req := httptest.NewRequest(http.MethodGet, "https://example.test/x", nil)
	if err := ic(req); err == nil {
		t.Fatal("expected error on 500 token response, got nil")
	}
}

func TestOAuth2TokenResponse_MissingAccessToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		_ = r.Body.Close()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	t.Cleanup(srv.Close)
	ic := OAuth2ClientCredentialsWithHTTPClient(srv.URL, "cid", "sec", nil, srv.Client())
	req := httptest.NewRequest(http.MethodGet, "https://example.test/x", nil)
	if err := ic(req); err == nil {
		t.Fatal("expected error for missing access_token, got nil")
	}
}

func TestOAuth2TokenResponse_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		_ = r.Body.Close()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{not json"))
	}))
	t.Cleanup(srv.Close)
	ic := OAuth2ClientCredentialsWithHTTPClient(srv.URL, "cid", "sec", nil, srv.Client())
	req := httptest.NewRequest(http.MethodGet, "https://example.test/x", nil)
	if err := ic(req); err == nil {
		t.Fatal("expected error for bad JSON, got nil")
	}
}

func TestOpenIDConnect_TokenURLOverride(t *testing.T) {
	rec := newTokenRecorder()
	srv := httptest.NewServer(rec.handler(http.StatusOK, "{\"access_token\":\"oidc-tok\",\"expires_in\":3600}"))
	t.Cleanup(srv.Close)

	// tokenURL set -> no discovery; client_credentials against tokenURL.
	ic := OpenIDConnectWithHTTPClient("https://unused.example.test/discovery", srv.URL, "cid", "sec", nil, srv.Client())
	req := applyInterceptor(t, ic)
	if got := req.Header.Get("Authorization"); got != "Bearer oidc-tok" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := rec.form.Get("grant_type"); got != "client_credentials" {
		t.Fatalf("grant_type = %q", got)
	}
}

func TestOpenIDConnect_Discovery(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		_ = r.Body.Close()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{\"access_token\":\"disc-tok\",\"expires_in\":3600}"))
	}))
	t.Cleanup(tokenSrv.Close)

	discoveryDoc := "{\"token_endpoint\":\"" + tokenSrv.URL + "\"}"
	var discoveryCalls atomic.Int64
	discSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		discoveryCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(discoveryDoc))
	}))
	t.Cleanup(discSrv.Close)

	ic := OpenIDConnectWithHTTPClient(discSrv.URL, "", "cid", "sec", nil, discSrv.Client())
	req := applyInterceptor(t, ic)
	if got := req.Header.Get("Authorization"); got != "Bearer disc-tok" {
		t.Fatalf("Authorization = %q", got)
	}

	// Second call: discovery is cached (resolve runs once), token is cached.
	_ = applyInterceptor(t, ic)
	if discoveryCalls.Load() != 1 {
		t.Fatalf("discovery calls = %d, want 1 (cached)", discoveryCalls.Load())
	}
}

func TestOpenIDConnect_DiscoveryNon200(t *testing.T) {
	discSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(discSrv.Close)
	ic := OpenIDConnectWithHTTPClient(discSrv.URL, "", "cid", "sec", nil, discSrv.Client())
	req := httptest.NewRequest(http.MethodGet, "https://example.test/x", nil)
	if err := ic(req); err == nil {
		t.Fatal("expected discovery error, got nil")
	}
}

func TestOpenIDConnect_DiscoveryMissingEndpoint(t *testing.T) {
	discSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{\"other\":\"value\"}"))
	}))
	t.Cleanup(discSrv.Close)
	ic := OpenIDConnectWithHTTPClient(discSrv.URL, "", "cid", "sec", nil, discSrv.Client())
	req := httptest.NewRequest(http.MethodGet, "https://example.test/x", nil)
	if err := ic(req); err == nil {
		t.Fatal("expected missing token_endpoint error, got nil")
	}
}

func TestOpenIDConnect_NoDiscoveryOrTokenURL(t *testing.T) {
	ic := OpenIDConnectWithHTTPClient("", "", "cid", "sec", nil, nil)
	req := httptest.NewRequest(http.MethodGet, "https://example.test/x", nil)
	if err := ic(req); err == nil {
		t.Fatal("expected error when no discovery or token URL, got nil")
	}
}
`

const loggingTestTemplate = `package client

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRT is a configurable base round-tripper for logging tests.
type fakeRT struct {
	resp *http.Response
	err  error
	calls int
}

func (f *fakeRT) RoundTrip(*http.Request) (*http.Response, error) {
	f.calls++
	return f.resp, f.err
}

func newFakeResponse(status int, body string) *http.Response {
	r := &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"text/plain"}, "X-Trace": []string{"yes"}},
		Body:       io.NopCloser(bytes.NewReader([]byte(body))),
	}
	return r
}

func TestNewLoggingRoundTripper_Defaults(t *testing.T) {
	rt := NewLoggingRoundTripper(nil, LoggingConfig{})
	if rt.base == nil {
		t.Fatal("nil base should default to http.DefaultTransport")
	}
	if rt.cfg.MaxBodyBytes != 4096 {
		t.Fatalf("MaxBodyBytes = %d, want 4096", rt.cfg.MaxBodyBytes)
	}
	if len(rt.cfg.RedactHeaders) == 0 {
		t.Fatal("empty RedactHeaders should default to DefaultRedactHeaders")
	}
}

func TestLoggingRoundTripper_DisabledShortCircuit(t *testing.T) {
	base := &fakeRT{resp: newFakeResponse(http.StatusOK, "ok")}
	rt := NewLoggingRoundTripper(base, LoggingConfig{}) // LogFile == ""
	req := httptest.NewRequest(http.MethodGet, "https://example.test/x", nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	defer closeResp(resp)
	if base.calls != 1 {
		t.Fatalf("base calls = %d, want 1", base.calls)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestLoggingRoundTripper_LogsRequestAndResponse(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "trace.log")
	base := &fakeRT{resp: newFakeResponse(http.StatusOK, "hello")}
	rt := NewLoggingRoundTripper(base, LoggingConfig{
		LogFile:                logPath,
		CaptureRequestHeaders:  true,
		CaptureRequestBody:     true,
		CaptureResponseHeaders: true,
		CaptureResponseBody:    true,
	})
	t.Cleanup(func() { _ = rt.Close() })

	req := httptest.NewRequest(http.MethodPost, "https://example.test/x?api_key=secret", strings.NewReader("req-body"))
	req.Header.Set("Authorization", "Bearer xyz")
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	defer closeResp(resp)

	// The response body must be fully replayed for the caller.
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("response body = %q, want %q", got, "hello")
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("log lines = %d, want 2", len(lines))
	}
	var reqEntry, respEntry logEntry
	if err := json.Unmarshal([]byte(lines[0]), &reqEntry); err != nil {
		t.Fatalf("unmarshal request entry: %v", err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &respEntry); err != nil {
		t.Fatalf("unmarshal response entry: %v", err)
	}
	if reqEntry.Type != "request" || reqEntry.Method != http.MethodPost {
		t.Fatalf("request entry = %+v", reqEntry)
	}
	if v, ok := reqEntry.Headers["Authorization"]; !ok || v != "[REDACTED]" {
		t.Fatalf("Authorization header = %q, want [REDACTED]", reqEntry.Headers["Authorization"])
	}
	if !strings.Contains(reqEntry.URL, "api_key=[REDACTED]") {
		t.Fatalf("request URL = %q, want redacted api_key", reqEntry.URL)
	}
	if respEntry.Type != "response" || respEntry.StatusCode != http.StatusOK {
		t.Fatalf("response entry = %+v", respEntry)
	}
	if v, ok := respEntry.Headers["X-Trace"]; !ok || v != "yes" {
		t.Fatalf("X-Trace header = %q", respEntry.Headers["X-Trace"])
	}
}

func TestLoggingRoundTripper_BodyTruncation(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "trace.log")
	big := strings.Repeat("x", 100)
	base := &fakeRT{resp: newFakeResponse(http.StatusOK, big)}
	rt := NewLoggingRoundTripper(base, LoggingConfig{
		LogFile:             logPath,
		CaptureResponseBody: true,
		MaxBodyBytes:        10,
	})
	t.Cleanup(func() { _ = rt.Close() })

	req := httptest.NewRequest(http.MethodGet, "https://example.test/x", nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	defer closeResp(resp)
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(got) != 100 {
		t.Fatalf("replayed body len = %d, want 100", len(got))
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	var respEntry logEntry
	if err := json.Unmarshal([]byte(lines[1]), &respEntry); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !respEntry.BodyTruncated {
		t.Fatal("expected BodyTruncated=true")
	}
}

func TestLoggingRoundTripper_EnsureOpenError(t *testing.T) {
	// A log file path in a non-existent directory cannot be opened.
	rt := NewLoggingRoundTripper(&fakeRT{resp: newFakeResponse(http.StatusOK, "")}, LoggingConfig{
		LogFile: filepath.Join(t.TempDir(), "nope", "trace.log"),
	})
	req := httptest.NewRequest(http.MethodGet, "https://example.test/x", nil)
	if _, err := rt.RoundTrip(req); err == nil {
		t.Fatal("expected ensureOpen error, got nil")
	}
}

func TestLoggingRoundTripper_CloseTwice(t *testing.T) {
	dir := t.TempDir()
	rt := NewLoggingRoundTripper(&fakeRT{resp: newFakeResponse(http.StatusOK, "")}, LoggingConfig{LogFile: filepath.Join(dir, "t.log")})
	req := httptest.NewRequest(http.MethodGet, "https://example.test/x", nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	closeResp(resp)
	if err := rt.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestSanitizeHeaders(t *testing.T) {
	h := http.Header{"Authorization": []string{"secret"}, "X-Other": []string{"a", "b"}}
	got := sanitizeHeaders(h, DefaultRedactHeaders)
	if got["Authorization"] != "[REDACTED]" {
		t.Fatalf("Authorization = %q", got["Authorization"])
	}
	if got["X-Other"] != "a, b" {
		t.Fatalf("X-Other = %q", got["X-Other"])
	}
}

func TestHeaderIsRedacted(t *testing.T) {
	if !headerIsRedacted("authorization", DefaultRedactHeaders) {
		t.Error("authorization should be redacted")
	}
	if headerIsRedacted("x-custom", DefaultRedactHeaders) {
		t.Error("x-custom should not be redacted")
	}
}

func TestRedactURL(t *testing.T) {
	u, _ := url.Parse("https://example.test/path?api_key=secret&foo=bar")
	got := redactURL(u)
	if !strings.Contains(got, "api_key=[REDACTED]") || !strings.Contains(got, "foo=[REDACTED]") {
		t.Fatalf("redactURL = %q", got)
	}
	if !strings.Contains(got, "/path") {
		t.Fatalf("path lost: %q", got)
	}
	if redactURL(nil) != "" {
		t.Fatalf("redactURL(nil) = %q, want empty", redactURL(nil))
	}
}

func TestRedactQueryValues(t *testing.T) {
	got := redactQueryValues("a=1&b=2&plain")
	want := "a=[REDACTED]&b=[REDACTED]&plain"
	if got != want {
		t.Fatalf("redactQueryValues = %q, want %q", got, want)
	}
	if redactQueryValues("") != "" {
		t.Fatalf("redactQueryValues(\"\") = %q, want empty", redactQueryValues(""))
	}
}

func TestCaptureBody(t *testing.T) {
	// Body larger than max is truncated in the capture but fully replayed.
	r := io.NopCloser(bytes.NewReader([]byte("hello world")))
	captured, replay, truncated, err := captureBody(r, 5)
	if err != nil {
		t.Fatalf("captureBody: %v", err)
	}
	if !truncated {
		t.Fatal("expected truncated=true")
	}
	if string(captured) != "hello" {
		t.Fatalf("captured = %q", captured)
	}
	full, err := io.ReadAll(replay)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(full) != "hello world" {
		t.Fatalf("replayed = %q, want full body", full)
	}
	if err := replay.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestCaptureBody_Nil(t *testing.T) {
	captured, replay, truncated, err := captureBody(nil, 10)
	if err != nil {
		t.Fatalf("captureBody: %v", err)
	}
	if captured != nil || replay != nil || truncated {
		t.Fatalf("expected nils, got captured=%v replay=%v truncated=%v", captured, replay, truncated)
	}
}

func TestJoinReadCloser(t *testing.T) {
	j := &joinReadCloser{first: bytes.NewReader([]byte("ab")), rest: io.NopCloser(bytes.NewReader([]byte("cd")))}
	got, err := io.ReadAll(j)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "abcd" {
		t.Fatalf("got = %q", got)
	}
	if err := j.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
`
