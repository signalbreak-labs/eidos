package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// maxAPIErrorBodyBytes limits how much of a response body is stored in an
// APIError, preventing enormous payloads from being retained in memory.
const maxAPIErrorBodyBytes = 1 << 20 // 1 MiB

// maxAPIErrorDisplayBytes limits how much of the stored body is rendered in
// APIError.Error(), keeping logs and error messages readable.
const maxAPIErrorDisplayBytes = 1024

// APIError represents an HTTP error response from the API.
// Callers should construct APIError values through NewAPIError rather than
// directly populating this struct, so that response bodies are capped at
// maxAPIErrorBodyBytes and the response body is always closed.
type APIError struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

func (e *APIError) Error() string {
	body := string(e.Body)
	if len(body) > maxAPIErrorDisplayBytes {
		body = body[:maxAPIErrorDisplayBytes] + "... [truncated]"
	}
	return fmt.Sprintf("API error status=%d body=%s", e.StatusCode, body)
}

// NewAPIError reads an HTTP response body into an APIError, truncating bodies
// larger than maxAPIErrorBodyBytes to protect against unbounded memory use.
func NewAPIError(resp *http.Response) (*APIError, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIErrorBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxAPIErrorBodyBytes {
		body = append(body[:maxAPIErrorBodyBytes], []byte("\n... truncated ...")...)
	}
	return &APIError{
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
		Body:       body,
	}, nil
}

// IsNotFound reports whether err is an API error with a 404 status code.
func IsNotFound(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusNotFound
	}
	return false
}

// IsRetryable reports whether err indicates a request should be retried.
// It returns true for transient network/transport errors, HTTP 5xx responses,
// and HTTP 429 Too Many Requests. Context cancellation and deadline errors are
// not retryable.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode >= http.StatusInternalServerError || apiErr.StatusCode == http.StatusTooManyRequests
	}
	return true
}
