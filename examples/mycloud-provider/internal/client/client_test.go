package client

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
func (f *failingReadCloser) Close() error          { f.closed = true; return nil }

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
	if c.BaseURL() != "https://api.mycloud.example/v1" {
		t.Fatalf("BaseURL() = %q, want %q", c.BaseURL(), "https://api.mycloud.example/v1")
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
	if got := req.Header.Get("User-Agent"); got != "eidos-generated-client" {
		t.Fatalf("User-Agent = %q, want %q", got, "eidos-generated-client")
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
	if p.Style != "none" {
		t.Fatalf("Style = %q, want %q", p.Style, "none")
	}
	if p.PageParam != "page" {
		t.Fatalf("PageParam = %q, want %q", p.PageParam, "page")
	}
	if p.PerPageParam != "per_page" {
		t.Fatalf("PerPageParam = %q, want %q", p.PerPageParam, "per_page")
	}
	if p.NextLinkRel != "next" {
		t.Fatalf("NextLinkRel = %q, want %q", p.NextLinkRel, "next")
	}
	if p.CursorField != "cursor" {
		t.Fatalf("CursorField = %q, want %q", p.CursorField, "cursor")
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
	minWait := 1 * time.Second
	maxWait := 30 * time.Second
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
