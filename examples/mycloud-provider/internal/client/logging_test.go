package client

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
	resp  *http.Response
	err   error
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
