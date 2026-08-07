package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLoggingFile_Render verifies that ClientFiles emits internal/client/logging.go
// and that the generated source contains the expected logging types and helpers.
func TestLoggingFile_Render(t *testing.T) {
	ir := sampleClientIR()

	h := Harness{OutputDir: t.TempDir()}
	if err := h.Generate(ClientFiles(ir)); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	src := readFile(t, h.OutputDir, "internal/client/logging.go")
	for _, want := range []string{
		"type LoggingConfig struct",
		"type LoggingRoundTripper struct",
		"func NewLoggingRoundTripper",
		"func (rt *LoggingRoundTripper) RoundTrip",
		"func (rt *LoggingRoundTripper) Close",
		"func sanitizeHeaders",
		"DefaultRedactHeaders",
		"WWW-Authenticate",
		"captureBody",
		"joinReadCloser",
		"[REDACTED]",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("logging.go missing %q\ncontent:\n%s", want, src)
		}
	}
}

// TestLoggingRoundTripper_Integration generates the client package into a
// temporary Go module and runs an integration test that exercises logging,
// header redaction, and body capture limits.
func TestLoggingRoundTripper_Integration(t *testing.T) {
	skipIfNetworkRestricted(t)
	ir := sampleClientIR()
	tmp := generateClientModule(t, ir)
	writeLoggingIntegrationTest(t, tmp)

	ctx, cancel := contextWithTimeout(t, 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "test", "./internal/client", "-v", "-run", "TestLogging")
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "PASS") {
		t.Fatalf("expected test to pass, got:\n%s", out)
	}
}

// writeLoggingIntegrationTest writes a generated-module test that verifies the
// logging round-tripper behavior.
func writeLoggingIntegrationTest(t *testing.T, moduleRoot string) {
	t.Helper()
	testPath := filepath.Join(moduleRoot, "internal", "client", "logging_integration_test.go")
	if err := os.MkdirAll(filepath.Dir(testPath), 0o750); err != nil {
		t.Fatalf("create client test dir: %v", err)
	}

	content := `package client

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testLogEntry struct {
	Timestamp     string            "json:\"timestamp\""
	Type          string            "json:\"type\""
	Method        string            "json:\"method\""
	URL           string            "json:\"url\""
	StatusCode    int               "json:\"status_code,omitempty\""
	Headers       map[string]string "json:\"headers,omitempty\""
	Body          string            "json:\"body,omitempty\""
	BodyTruncated bool              "json:\"body_truncated,omitempty\""
	Error         string            "json:\"error,omitempty\""
}

func TestLoggingRoundTripperLogsRequestAndResponse(t *testing.T) {
	logDir := t.TempDir()
	logFile := filepath.Join(logDir, "trace.log")

	serverBody := strings.Repeat("response-body-", 100)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ := io.ReadAll(r.Body)
		if string(gotBody) != "request-payload" {
			t.Errorf("server received body %q, want request-payload", gotBody)
		}
		w.Header().Set("X-Custom", "visible")
		w.Header().Set("Authorization", "should-be-redacted")
		w.Header().Set("WWW-Authenticate", "Bearer realm=secret")
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte(serverBody))
	}))
	defer ts.Close()

	cfg := LoggingConfig{
		LogFile:                logFile,
		CaptureRequestHeaders:  true,
		CaptureRequestBody:     true,
		CaptureResponseHeaders: true,
		CaptureResponseBody:    true,
		MaxBodyBytes:           32,
		RedactHeaders:          []string{"Authorization", "X-API-Key", "WWW-Authenticate"},
	}

	c := New(WithBaseURL(ts.URL), WithLogging(cfg))
	req, err := c.NewRequest(context.Background(), http.MethodPost, "/things", bytes.NewReader([]byte("request-payload")))
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}
	// Attach a query string that may carry credentials so the trace log's
	// URL-redaction behavior is exercised (M-20).
	req.URL.RawQuery = "api_key=secret-value&filter=active"
	req.Header.Set("Authorization", "Bearer secret-token")
	req.Header.Set("X-API-Key", "super-secret")
	req.Header.Set("X-Custom", "visible-value")

	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do error: %v", err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(respBody) != serverBody {
		t.Errorf("response body mismatch: got length %d want %d", len(respBody), len(serverBody))
	}

	entries := readLogEntries(t, logFile)
	if len(entries) != 2 {
		t.Fatalf("expected 2 log entries, got %d: %+v", len(entries), entries)
	}

	reqEntry := entries[0]
	if reqEntry.Type != "request" {
		t.Errorf("first entry type = %q, want request", reqEntry.Type)
	}
	if reqEntry.Method != http.MethodPost {
		t.Errorf("request method = %q, want POST", reqEntry.Method)
	}
	if !strings.Contains(reqEntry.URL, "/things") {
		t.Errorf("request url = %q, want suffix /things", reqEntry.URL)
	}
	// Query parameters that may carry credentials (api_key, sig, etc.) must be
	// redacted from the trace log so secrets are not written to disk (M-20).
	if strings.Contains(reqEntry.URL, "secret-value") {
		t.Errorf("request url leaks query secret %q: %q", "secret-value", reqEntry.URL)
	}
	if !strings.Contains(reqEntry.URL, "api_key=[REDACTED]") {
		t.Errorf("request url api_key not redacted: %q", reqEntry.URL)
	}
	if !strings.Contains(reqEntry.URL, "filter=[REDACTED]") {
		t.Errorf("request url filter not redacted: %q", reqEntry.URL)
	}
	if reqEntry.Headers["Authorization"] != "[REDACTED]" {
		t.Errorf("Authorization header not redacted: %q", reqEntry.Headers["Authorization"])
	}
	if reqEntry.Headers["X-Api-Key"] != "[REDACTED]" {
		t.Errorf("X-Api-Key header not redacted: %q", reqEntry.Headers["X-Api-Key"])
	}
	if reqEntry.Headers["X-Custom"] != "visible-value" {
		t.Errorf("X-Custom header = %q, want visible-value", reqEntry.Headers["X-Custom"])
	}
	if decodeBody(t, reqEntry.Body) != "request-payload" {
		t.Errorf("request body = %q, want request-payload", decodeBody(t, reqEntry.Body))
	}
	if reqEntry.BodyTruncated {
		t.Errorf("request body should not be truncated")
	}

	respEntry := entries[1]
	if respEntry.Type != "response" {
		t.Errorf("second entry type = %q, want response", respEntry.Type)
	}
	if respEntry.StatusCode != http.StatusTeapot {
		t.Errorf("response status = %d, want %d", respEntry.StatusCode, http.StatusTeapot)
	}
	if respEntry.Headers["Authorization"] != "[REDACTED]" {
		t.Errorf("response Authorization header not redacted: %q", respEntry.Headers["Authorization"])
	}
	if respEntry.Headers["Www-Authenticate"] != "[REDACTED]" {
		t.Errorf("WWW-Authenticate header not redacted: %q", respEntry.Headers["Www-Authenticate"])
	}
	if respEntry.Headers["X-Custom"] != "visible" {
		t.Errorf("response X-Custom header = %q, want visible", respEntry.Headers["X-Custom"])
	}
	if len(decodeBody(t, respEntry.Body)) != 32 {
		t.Errorf("response logged body length = %d, want 32", len(decodeBody(t, respEntry.Body)))
	}
	if !respEntry.BodyTruncated {
		t.Errorf("response body should be truncated")
	}
}

func TestLoggingRoundTripper_Close(t *testing.T) {
	logDir := t.TempDir()
	logFile := filepath.Join(logDir, "trace.log")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	cfg := LoggingConfig{LogFile: logFile, CaptureRequestHeaders: true}
	c := New(WithBaseURL(ts.URL), WithLogging(cfg))
	req, _ := c.NewRequest(context.Background(), http.MethodGet, "/close", nil)
	if _, err := c.Do(req); err != nil {
		t.Fatalf("Do error: %v", err)
	}

	entries := readLogEntries(t, logFile)
	if len(entries) == 0 {
		t.Fatalf("expected log entries before close")
	}

	// Close should be idempotent and not return an error.
	if err := c.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close error: %v", err)
	}
}

func TestLoggingRoundTripper_Concurrent(t *testing.T) {
	logDir := t.TempDir()
	logFile := filepath.Join(logDir, "trace.log")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer ts.Close()

	cfg := LoggingConfig{LogFile: logFile, CaptureRequestHeaders: true}
	c := New(WithBaseURL(ts.URL), WithLogging(cfg))
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := c.NewRequest(context.Background(), http.MethodGet, "/ concurrent", nil)
			if _, err := c.Do(req); err != nil {
				t.Errorf("Do error: %v", err)
			}
		}()
	}
	wg.Wait()
	_ = c.Close()

	entries := readLogEntries(t, logFile)
	if len(entries) != 20 {
		t.Fatalf("expected 20 log entries, got %d", len(entries))
	}
	requests := 0
	responses := 0
	for _, e := range entries {
		switch e.Type {
		case "request":
			requests++
		case "response":
			responses++
		}
	}
	if requests != 10 || responses != 10 {
		t.Fatalf("requests=%d responses=%d, want 10 each", requests, responses)
	}
}

func TestLoggingRoundTripper_NilBody(t *testing.T) {
	logDir := t.TempDir()
	logFile := filepath.Join(logDir, "trace.log")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			_, _ = io.Copy(io.Discard, r.Body)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	cfg := LoggingConfig{
		LogFile:                logFile,
		CaptureRequestHeaders:  true,
		CaptureRequestBody:     true,
		CaptureResponseHeaders: true,
		CaptureResponseBody:    true,
	}
	c := New(WithBaseURL(ts.URL), WithLogging(cfg))
	req, _ := c.NewRequest(context.Background(), http.MethodGet, "/nil", nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do error: %v", err)
	}
	if resp.Body != nil {
		_ = resp.Body.Close()
	}

	entries := readLogEntries(t, logFile)
	if len(entries) != 2 {
		t.Fatalf("expected 2 log entries, got %d", len(entries))
	}
	if decodeBody(t, entries[0].Body) != "" {
		t.Errorf("request body should be empty, got %q", decodeBody(t, entries[0].Body))
	}
}

func TestLoggingRoundTripper_ResponseErrorNilResp(t *testing.T) {
	logDir := t.TempDir()
	logFile := filepath.Join(logDir, "trace.log")

	rt := NewLoggingRoundTripper(roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("boom")
	}), LoggingConfig{LogFile: logFile, CaptureRequestHeaders: true})
	defer rt.Close()

	req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/things", nil)
	resp, err := rt.RoundTrip(req)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("expected boom error, got resp=%v err=%v", resp, err)
	}
	if resp != nil {
		t.Fatalf("expected nil response, got %v", resp)
	}

	entries := readLogEntries(t, logFile)
	if len(entries) != 2 {
		t.Fatalf("expected 2 log entries, got %d", len(entries))
	}
	if entries[0].Type != "request" {
		t.Errorf("first entry type = %q, want request", entries[0].Type)
	}
	if entries[1].Type != "response" {
		t.Errorf("second entry type = %q, want response", entries[1].Type)
	}
	if entries[1].Error != "boom" {
		t.Errorf("response error = %q, want boom", entries[1].Error)
	}
}

func TestLoggingRoundTripper_FileOpenFailure(t *testing.T) {
	// Use the temp directory itself as the log file path; opening a directory
	// for append writes must fail.
	logDir := t.TempDir()

	rt := NewLoggingRoundTripper(roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("should not be called")
	}), LoggingConfig{LogFile: logDir})

	req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/things", nil)
	if _, err := rt.RoundTrip(req); err == nil {
		t.Fatalf("expected file open error")
	}
}

func TestLoggingRoundTripper_EmptyLogFile(t *testing.T) {
	logDir := t.TempDir()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer ts.Close()

	cfg := LoggingConfig{
		LogFile:                "",
		CaptureRequestHeaders:  true,
		CaptureRequestBody:     true,
		CaptureResponseHeaders: true,
		CaptureResponseBody:    true,
	}
	c := New(WithBaseURL(ts.URL), WithLogging(cfg))
	req, _ := c.NewRequest(context.Background(), http.MethodGet, "/empty", nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do error: %v", err)
	}
	if resp.Body != nil {
		_ = resp.Body.Close()
	}

	// No log file should be created in logDir.
	files, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("ReadDir error: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected no log files, got %d", len(files))
	}
}

func TestLoggingRoundTripper_RetryLogsEachAttempt(t *testing.T) {
	logDir := t.TempDir()
	logFile := filepath.Join(logDir, "trace.log")

	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	cfg := LoggingConfig{LogFile: logFile, CaptureRequestHeaders: true}
	retryPolicy := func(resp *http.Response, err error) bool {
		return err != nil || resp.StatusCode >= 500
	}
	c := New(
		WithBaseURL(ts.URL),
		WithLogging(cfg),
		WithRetry(1, retryPolicy, func(attempt int) time.Duration { return 0 }),
	)
	req, _ := c.NewRequest(context.Background(), http.MethodGet, "/retry", nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	entries := readLogEntries(t, logFile)
	if len(entries) != 4 {
		t.Fatalf("expected 4 log entries for two attempts, got %d", len(entries))
	}
	if entries[0].Type != "request" || entries[1].Type != "response" ||
		entries[2].Type != "request" || entries[3].Type != "response" {
		t.Fatalf("expected alternating request/response entries, got %+v", entries)
	}
}

func TestCaptureBody_NilReader(t *testing.T) {
	captured, replacement, truncated, err := captureBody(nil, 4096)
	if err != nil {
		t.Fatalf("captureBody(nil) error: %v", err)
	}
	if captured != nil || replacement != nil || truncated {
		t.Fatalf("captureBody(nil) = (%v, %v, %v, %v), want nil", captured, replacement, truncated, err)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func readLogEntries(t *testing.T, path string) []testLogEntry {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var entries []testLogEntry
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e testLogEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("unmarshal log entry %q: %v", line, err)
		}
		entries = append(entries, e)
	}
	return entries
}

func decodeBody(t *testing.T, s string) string {
	t.Helper()
	b, err := base64.RawStdEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("decode body %q: %v", s, err)
	}
	return string(b)
}
`
	if err := os.WriteFile(testPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write logging integration test: %v", err)
	}
}
