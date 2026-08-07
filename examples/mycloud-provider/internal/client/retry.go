package client

import (
	"context"
	"errors"
	"math/rand"
	"net/http"
	"time"
)

// RetryPolicy decides whether a request should be retried.
type RetryPolicy func(resp *http.Response, err error) bool

// BackoffFunc returns the duration to wait before the next attempt.
type BackoffFunc func(attempt int) time.Duration

// DefaultRetryPolicy retries on network errors, 5xx responses, and 429 Too Many Requests.
// It does not retry when the context has been canceled or the deadline exceeded.
func DefaultRetryPolicy(resp *http.Response, err error) bool {
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false
		}
		return true
	}
	if resp == nil {
		return false
	}
	return resp.StatusCode >= http.StatusInternalServerError || resp.StatusCode == http.StatusTooManyRequests
}

// DefaultBackoff returns exponential backoff with additive jitter: the delay
// is base + rand[0, base), so the effective floor is base (the doubled floor,
// not zero as in true full jitter). The exponential base is clamped to the
// provider's configured min/max wait windows (1 * time.Second ..
// 30 * time.Second) rather than hardcoded 1s/30s constants, so the
// RetryWaitMin/RetryWaitMax values read from the IR are actually honored by the
// generated client (M-11). The prior comment called this "full jitter", which
// was inaccurate (L-29).
func DefaultBackoff(attempt int) time.Duration {
	minWait := 1 * time.Second
	maxWait := 30 * time.Second
	exp := attempt
	if exp > 10 {
		exp = 10
	}
	base := time.Duration(1<<exp) * time.Second
	if base > maxWait || base <= 0 {
		base = maxWait
	}
	if base < minWait {
		base = minWait
	}
	jitter := time.Duration(rand.Int63n(int64(base)))
	return base + jitter
}

// DoWithRetry executes do until the policy no longer requests a retry or the context is canceled.
func DoWithRetry(ctx context.Context, do func() (*http.Response, error), maxRetries int, policy RetryPolicy, backoff BackoffFunc) (*http.Response, error) {
	var resp *http.Response
	var err error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		resp, err = do()
		if !policy(resp, err) || attempt == maxRetries {
			return resp, err
		}
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff(attempt)):
		}
	}
	return resp, err
}
