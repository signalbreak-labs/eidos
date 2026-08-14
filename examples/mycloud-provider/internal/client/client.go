package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// RequestInterceptor can inspect or modify an outgoing request before it is sent.
type RequestInterceptor func(*http.Request) error

// Client wraps net/http with a configured base URL, user agent, request interceptors,
// optional retry policy, and optional trace logging.
//
// Interceptors come in two flavors. Flat interceptors added via WithInterceptors
// apply to every request unconditionally. Scheme interceptors added via
// WithSchemeInterceptor are keyed by OpenAPI security scheme name and apply
// selectively: by default every configured scheme interceptor applies
// (inheriting the global security requirement), but a per-operation
// client.WithSchemes(...) request option (REMAINING_GAPS §1) restricts the
// applied set to the named schemes so an operation declaring a single
// security requirement authenticates with exactly that requirement's schemes
// (AND resolution). schemeOrder preserves registration order so interceptor
// application is deterministic.
type Client struct {
	httpClient         *http.Client
	baseURL            string
	userAgent          string
	interceptors       []RequestInterceptor
	schemeInterceptors map[string]RequestInterceptor
	schemeOrder        []string
	retryPolicy        RetryPolicy
	maxRetries         int
	backoff            BackoffFunc
	logging            LoggingConfig
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithHTTPClient sets the underlying *http.Client.
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) { c.httpClient = httpClient }
}

// WithBaseURL sets the base URL used for all relative request paths.
func WithBaseURL(baseURL string) ClientOption {
	return func(c *Client) { c.baseURL = baseURL }
}

// WithUserAgent sets the User-Agent header.
func WithUserAgent(userAgent string) ClientOption {
	return func(c *Client) { c.userAgent = userAgent }
}

// WithInterceptors appends flat request interceptors applied to every request.
func WithInterceptors(interceptors ...RequestInterceptor) ClientOption {
	return func(c *Client) { c.interceptors = append(c.interceptors, interceptors...) }
}

// WithSchemeInterceptor registers a request interceptor keyed by OpenAPI
// security scheme name. The scheme name is the key the generated CRUD body
// passes to WithSchemes for per-operation security (AND resolution): an
// operation declaring a single security requirement passes WithSchemes with
// that requirement's scheme names, so only those interceptors apply. Re-registering
// a name replaces its interceptor without duplicating the registration-order
// entry, keeping application deterministic.
func WithSchemeInterceptor(name string, interceptor RequestInterceptor) ClientOption {
	return func(c *Client) {
		if c.schemeInterceptors == nil {
			c.schemeInterceptors = make(map[string]RequestInterceptor)
		}
		if _, exists := c.schemeInterceptors[name]; !exists {
			c.schemeOrder = append(c.schemeOrder, name)
		}
		c.schemeInterceptors[name] = interceptor
	}
}

// RequestOption configures a single request built by NewRequest.
type RequestOption func(*requestOptions)

// requestOptions carries per-request options resolved by NewRequest.
type requestOptions struct {
	// schemes is the named set of security scheme interceptors to apply. When
	// schemesSet is false every configured scheme interceptor applies
	// (inheriting the global security requirement). When schemesSet is true
	// only the named scheme interceptors apply; an empty set marks the
	// operation as unauthenticated and applies no scheme interceptors.
	schemes    []string
	schemesSet bool
}

// WithSchemes restricts the request to the named security scheme interceptors
// (per-operation AND resolution, REMAINING_GAPS §1). Pass no names to mark the
// operation unauthenticated. When no WithSchemes option is passed the request
// inherits the global default and applies every configured scheme interceptor.
func WithSchemes(names ...string) RequestOption {
	return func(ro *requestOptions) {
		ro.schemes = names
		ro.schemesSet = true
	}
}

// WithRetry sets the retry policy and backoff for idempotent requests.
func WithRetry(maxRetries int, policy RetryPolicy, backoff BackoffFunc) ClientOption {
	return func(c *Client) {
		c.maxRetries = maxRetries
		c.retryPolicy = policy
		c.backoff = backoff
	}
}

// WithLogging configures request/response trace logging.
func WithLogging(cfg LoggingConfig) ClientOption {
	return func(c *Client) { c.logging = cfg }
}

// New creates a new Client with the supplied options.
func New(opts ...ClientOption) *Client {
	c := &Client{
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		baseURL:     "https://api.mycloud.example/v1",
		userAgent:   "eidos-generated-client",
		maxRetries:  3,
		retryPolicy: DefaultRetryPolicy,
		backoff:     DefaultBackoff,
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.logging.LogFile != "" {
		c.httpClient.Transport = NewLoggingRoundTripper(c.httpClient.Transport, c.logging)
	}
	return c
}

// BaseURL returns the client's configured base URL.
func (c *Client) BaseURL() string { return c.baseURL }

// NewRequest builds an HTTP request relative to the client's base URL. The
// variadic opts configure per-request behavior; the generated CRUD bodies pass
// client.WithSchemes(...) for per-operation security (AND resolution), so an
// operation declaring a single security requirement authenticates with exactly
// that requirement's scheme interceptors. With no opts every configured scheme
// interceptor applies (the global default).
func (c *Client) NewRequest(ctx context.Context, method, path string, body io.Reader, opts ...RequestOption) (*http.Request, error) {
	fullURL, err := url.JoinPath(c.baseURL, path)
	if err != nil {
		return nil, fmt.Errorf("join base URL and path: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return nil, err
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	for _, interceptor := range c.resolveInterceptors(opts) {
		if err := interceptor(req); err != nil {
			return nil, err
		}
	}
	return req, nil
}

// resolveInterceptors selects the request interceptors to apply. With no
// per-operation scheme selection (the default) every configured scheme
// interceptor applies, in registration order, followed by any flat
// interceptors added via WithInterceptors. When a caller passes WithSchemes
// (per-operation AND resolution, REMAINING_GAPS §1) only the named scheme
// interceptors apply, in registration order; an empty set marks the operation
// as unauthenticated and applies no scheme interceptors. Flat interceptors
// added via WithInterceptors always apply regardless of a per-operation scheme
// selection, so non-auth middleware is never silently dropped.
func (c *Client) resolveInterceptors(opts []RequestOption) []RequestInterceptor {
	var ro requestOptions
	for _, opt := range opts {
		opt(&ro)
	}
	scheme := c.schemeInterceptorsFor(ro)
	if len(c.interceptors) == 0 {
		return scheme
	}
	out := make([]RequestInterceptor, 0, len(scheme)+len(c.interceptors))
	out = append(out, scheme...)
	out = append(out, c.interceptors...)
	return out
}

// schemeInterceptorsFor returns the scheme interceptors selected by the
// per-request options. With no scheme selection every registered scheme
// interceptor applies in schemeOrder; with a selection only the named
// interceptors apply, iterated in schemeOrder so the result is deterministic.
func (c *Client) schemeInterceptorsFor(ro requestOptions) []RequestInterceptor {
	if !ro.schemesSet {
		out := make([]RequestInterceptor, 0, len(c.schemeOrder))
		for _, name := range c.schemeOrder {
			if ic, ok := c.schemeInterceptors[name]; ok {
				out = append(out, ic)
			}
		}
		return out
	}
	want := make(map[string]bool, len(ro.schemes))
	for _, s := range ro.schemes {
		want[s] = true
	}
	out := make([]RequestInterceptor, 0, len(ro.schemes))
	for _, name := range c.schemeOrder {
		if want[name] {
			if ic, ok := c.schemeInterceptors[name]; ok {
				out = append(out, ic)
			}
		}
	}
	return out
}

// Do sends the request. If a retry policy is configured, it is applied.
//
// Retried requests must be able to resend the request body. After the first
// attempt the request body has been drained; http.Request.GetBody is set by
// http.NewRequestWithContext for the common body types (bytes.Reader,
// strings.Reader, bytes.Buffer) but is nil for a generic io.Reader. When GetBody
// is nil and a body is present, the body is buffered here and a GetBody closure
// is installed so every retry sends a fresh, complete body rather than an
// empty one.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	if c.retryPolicy == nil {
		return c.httpClient.Do(req)
	}
	if req.Body != nil && req.GetBody == nil {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			req.Body.Close()
			return nil, fmt.Errorf("read request body for retry: %w", err)
		}
		req.Body.Close()
		buf := body // captured by GetBody; never mutated after this point
		req.Body = io.NopCloser(bytes.NewReader(buf))
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(buf)), nil
		}
	}
	return DoWithRetry(req.Context(), func() (*http.Response, error) {
		attempt := req.Clone(req.Context())
		if attempt.GetBody != nil {
			body, err := attempt.GetBody()
			if err != nil {
				return nil, err
			}
			attempt.Body = body
		}
		return c.httpClient.Do(attempt)
	}, c.maxRetries, c.retryPolicy, c.backoff)
}

// Close releases resources held by the client. When trace logging is enabled,
// this closes the underlying log file.
func (c *Client) Close() error {
	if closer, ok := c.httpClient.Transport.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}
