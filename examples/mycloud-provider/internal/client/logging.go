package client

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// LoggingConfig configures request/response trace logging.
type LoggingConfig struct {
	LogFile                string
	CaptureRequestHeaders  bool
	CaptureRequestBody     bool
	CaptureResponseHeaders bool
	CaptureResponseBody    bool
	MaxBodyBytes           int
	RedactHeaders          []string
}

// DefaultRedactHeaders lists header names that are redacted by default.
// In addition to common authorization/credential headers, WWW-Authenticate is
// redacted because it can contain negotiation tokens and sensitive parameters.
var DefaultRedactHeaders = []string{
	"Authorization",
	"Cookie",
	"Proxy-Authorization",
	"Set-Cookie",
	"WWW-Authenticate",
	"X-API-Key",
}

// logEntry is a single structured line written to the trace log file.
type logEntry struct {
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

// LoggingRoundTripper wraps an http.RoundTripper and appends request/response
// traces to a log file. It is safe for concurrent use.
//
// When the round-tripper is composed with retry logic, each retry attempt is
// logged as a separate request/response pair because the transport sees each
// attempt individually. Correlating related attempts must be done by the caller
// (for example, by supplying a request ID header and including it in the logs).
type LoggingRoundTripper struct {
	base    http.RoundTripper
	cfg     LoggingConfig
	file    *os.File
	once    sync.Once
	openErr error
	mu      sync.Mutex
}

// NewLoggingRoundTripper returns a round-tripper that logs traffic to cfg.LogFile.
// Callers should invoke Close when the round-tripper is no longer needed to avoid
// leaking the log file descriptor.
func NewLoggingRoundTripper(base http.RoundTripper, cfg LoggingConfig) *LoggingRoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = 4096
	}
	if len(cfg.RedactHeaders) == 0 {
		cfg.RedactHeaders = DefaultRedactHeaders
	}
	return &LoggingRoundTripper{
		base: base,
		cfg:  cfg,
	}
}

func (rt *LoggingRoundTripper) ensureOpen() error {
	rt.once.Do(func() {
		if rt.cfg.LogFile == "" {
			return
		}
		rt.file, rt.openErr = os.OpenFile(rt.cfg.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
	})
	return rt.openErr
}

// RoundTrip implements http.RoundTripper.
//
// If capturing the response body fails, the response is still returned with an
// empty, non-nil Body so callers can safely close it.
func (rt *LoggingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := rt.ensureOpen(); err != nil {
		return nil, err
	}

	// When logging is disabled (LogFile == ""), the log file is never opened and
	// writeLog is a no-op, so capturing and sanitizing request/response bodies
	// and headers on every request is wasted work. Short-circuit to the base
	// transport and skip the capture entirely (L-47).
	if rt.cfg.LogFile == "" {
		return rt.base.RoundTrip(req)
	}

	start := time.Now()

	var reqBody []byte
	var reqBodyTruncated bool
	if req.Body != nil && rt.cfg.CaptureRequestBody {
		var err error
		reqBody, req.Body, reqBodyTruncated, err = captureBody(req.Body, rt.cfg.MaxBodyBytes)
		if err != nil {
			return nil, err
		}
	}

	var reqHeaders map[string]string
	if rt.cfg.CaptureRequestHeaders {
		reqHeaders = sanitizeHeaders(req.Header, rt.cfg.RedactHeaders)
	}

	resp, err := rt.base.RoundTrip(req)

	var respBody []byte
	var respBodyTruncated bool
	var statusCode int
	var respHeaders map[string]string
	if resp != nil {
		statusCode = resp.StatusCode
		if resp.Body != nil && rt.cfg.CaptureResponseBody {
			var bodyErr error
			respBody, resp.Body, respBodyTruncated, bodyErr = captureBody(resp.Body, rt.cfg.MaxBodyBytes)
			if bodyErr != nil {
				resp.Body = io.NopCloser(strings.NewReader(""))
				return resp, bodyErr
			}
		}
		if rt.cfg.CaptureResponseHeaders {
			respHeaders = sanitizeHeaders(resp.Header, rt.cfg.RedactHeaders)
		}
	}

	rt.writeLog(start, req, reqHeaders, reqBody, reqBodyTruncated, respHeaders, respBody, respBodyTruncated, statusCode, err)

	return resp, err
}

// Close closes the underlying log file if one was opened. It is safe to call
// Close multiple times and from multiple goroutines. After Close, the round-
// tripper must not be used for further requests.
func (rt *LoggingRoundTripper) Close() error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.file != nil {
		err := rt.file.Close()
		rt.file = nil
		return err
	}
	return nil
}

func (rt *LoggingRoundTripper) writeLog(start time.Time, req *http.Request, reqHeaders map[string]string, reqBody []byte, reqBodyTruncated bool, respHeaders map[string]string, respBody []byte, respBodyTruncated bool, statusCode int, tripErr error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	if rt.file == nil {
		return
	}

	timestamp := start.UTC().Format(time.RFC3339Nano)

	reqEntry := logEntry{
		Timestamp:     timestamp,
		Type:          "request",
		Method:        req.Method,
		URL:           redactURL(req.URL),
		Headers:       reqHeaders,
		Body:          base64.RawStdEncoding.EncodeToString(reqBody),
		BodyTruncated: reqBodyTruncated,
	}
	rt.writeEntry(reqEntry)

	respEntry := logEntry{
		Timestamp:     time.Now().UTC().Format(time.RFC3339Nano),
		Type:          "response",
		Method:        req.Method,
		URL:           redactURL(req.URL),
		StatusCode:    statusCode,
		Headers:       respHeaders,
		Body:          base64.RawStdEncoding.EncodeToString(respBody),
		BodyTruncated: respBodyTruncated,
	}
	if tripErr != nil {
		respEntry.Error = tripErr.Error()
	}
	rt.writeEntry(respEntry)
}

func (rt *LoggingRoundTripper) writeEntry(entry logEntry) {
	data, marshalErr := json.Marshal(entry)
	if marshalErr != nil {
		_, _ = os.Stderr.WriteString(fmt.Sprintf("logging round-tripper: failed to marshal log entry: %v\n", marshalErr))
		return
	}
	if _, writeErr := rt.file.Write(data); writeErr != nil {
		_, _ = os.Stderr.WriteString(fmt.Sprintf("logging round-tripper: failed to write log entry: %v\n", writeErr))
		return
	}
	if _, writeErr := rt.file.Write([]byte("\n")); writeErr != nil {
		_, _ = os.Stderr.WriteString(fmt.Sprintf("logging round-tripper: failed to write log newline: %v\n", writeErr))
	}
}

// captureBody reads up to max bytes from r for logging, then returns a
// ReadCloser that replays the captured prefix followed by the remainder of r.
// Only max+1 bytes are buffered in memory, so very large bodies do not cause
// excessive memory pressure. The original ReadCloser is closed when the returned
// ReadCloser is closed.
func captureBody(r io.ReadCloser, max int) ([]byte, io.ReadCloser, bool, error) {
	if r == nil {
		return nil, nil, false, nil
	}
	limited := io.LimitReader(r, int64(max)+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		_ = r.Close()
		return nil, nil, false, err
	}
	truncated := len(data) > max
	captured := data
	if truncated {
		captured = data[:max]
	}
	// Replay every byte read from r so the caller sees the full body.
	return captured, &joinReadCloser{first: bytes.NewReader(data), rest: r}, truncated, nil
}

// joinReadCloser reads from first until EOF, then continues reading from rest
// and closes rest when closed.
type joinReadCloser struct {
	first io.Reader
	rest  io.ReadCloser
}

func (j *joinReadCloser) Read(p []byte) (int, error) {
	if j.first != nil {
		n, err := j.first.Read(p)
		if err == io.EOF {
			j.first = nil
			if n == 0 {
				return j.rest.Read(p)
			}
			return n, nil
		}
		return n, err
	}
	return j.rest.Read(p)
}

func (j *joinReadCloser) Close() error {
	return j.rest.Close()
}

// sanitizeHeaders returns a copy of the headers with redacted names replaced.
func sanitizeHeaders(h http.Header, redact []string) map[string]string {
	out := make(map[string]string, len(h))
	for name, values := range h {
		key := http.CanonicalHeaderKey(name)
		if headerIsRedacted(key, redact) {
			out[key] = "[REDACTED]"
		} else {
			out[key] = strings.Join(values, ", ")
		}
	}
	return out
}

func headerIsRedacted(name string, redact []string) bool {
	name = strings.ToLower(name)
	for _, r := range redact {
		if name == strings.ToLower(r) {
			return true
		}
	}
	return false
}

// redactURL returns the string form of u with every query parameter value
// replaced by "[REDACTED]". APIs commonly accept credentials as query
// parameters (for example ?api_key=... or ?sig=...); redacting all query
// values prevents secrets from being written to the trace log file (M-20).
// Parameter names and the URL path are preserved so the log remains useful for
// debugging. A nil URL returns the empty string.
func redactURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	if u.RawQuery == "" {
		return u.String()
	}
	redacted := *u
	redacted.RawQuery = redactQueryValues(redacted.RawQuery)
	return redacted.String()
}

// redactQueryValues replaces each "key=value" pair in rawQuery with
// "key=[REDACTED]", preserving parameter names and the original separators.
// Pairs with no "=" are left untouched (there is no value to leak). The raw
// query is rewritten at the string level rather than via url.Values.Encode so
// the literal "[REDACTED]" marker is not percent-encoded into an unreadable
// %5BREDACTED%5D form in the log.
func redactQueryValues(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	parts := strings.Split(rawQuery, "&")
	for i, p := range parts {
		if p == "" {
			continue
		}
		if idx := strings.IndexByte(p, '='); idx >= 0 {
			parts[i] = p[:idx+1] + "[REDACTED]"
		}
	}
	return strings.Join(parts, "&")
}
