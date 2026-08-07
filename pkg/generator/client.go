package generator

import (
	"fmt"
	"strconv"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// goString returns v formatted as a quoted Go string literal (via strconv.Quote).
// It is used when interpolating spec-derived strings into generated Go source so
// that a hostile value cannot break out of the surrounding string literal and
// inject arbitrary Go code. Empty input renders as the empty Go string literal
// `""`.
func goString(v string) string { return strconv.Quote(v) }

// ClientFiles returns the generated internal/client package files for a provider.
// It emits client.go, auth.go, models.go, errors.go, retry.go, and pagination.go
// using the provider-level client and security configuration from the IR.
func ClientFiles(p ir.ProviderIR) []File {
	cfg := clientConfigFromIR(p)
	files := []File{
		clientGoFile(cfg),
		modelsGoFile(cfg),
		errorsGoFile(cfg),
		retryGoFile(cfg),
		paginationGoFile(cfg),
		loggingGoFile(cfg),
	}
	// Only generate auth middleware when the provider declares security schemes.
	if len(p.SecurityIR.Schemes) > 0 {
		files = append(files, authGoFile(cfg))
	}
	return files
}

// clientConfig carries the interpolated values used by the client templates.
type clientConfig struct {
	BaseURL         string
	UserAgent       string
	Timeout         string
	RetryMax        int
	RetryWaitMin    string
	RetryWaitMax    string
	PaginationStyle string
	PageParam       string
	PerPageParam    string
	NextLinkRel     string
	CursorField     string

	// Struct tags injected into generated model structs.
	DataTag         string
	MessageTag      string
	CodeTag         string
	AccessTokenTag  string
	TokenTypeTag    string
	ExpiresInTag    string
	RefreshTokenTag string
}

func clientConfigFromIR(p ir.ProviderIR) clientConfig {
	cfg := clientConfig{
		BaseURL:         goString(""),
		UserAgent:       goString("eidos-generated-client"),
		Timeout:         formatDuration(30 * time.Second),
		RetryMax:        3,
		RetryWaitMin:    formatDuration(1 * time.Second),
		RetryWaitMax:    formatDuration(30 * time.Second),
		PaginationStyle: goString("none"),
		PageParam:       goString("page"),
		PerPageParam:    goString("per_page"),
		NextLinkRel:     goString("next"),
		CursorField:     goString("cursor"),
		DataTag:         jsonTag("data"),
		MessageTag:      jsonTag("message"),
		CodeTag:         jsonTag("code"),
		AccessTokenTag:  jsonTag("access_token"),
		TokenTypeTag:    jsonTag("token_type"),
		ExpiresInTag:    jsonTag("expires_in"),
		RefreshTokenTag: jsonTag("refresh_token"),
	}

	if p.ClientIR.BaseURLTemplate != "" {
		cfg.BaseURL = goString(p.ClientIR.BaseURLTemplate)
	} else if len(p.Servers) > 0 && p.Servers[0].URL != "" {
		cfg.BaseURL = goString(p.Servers[0].URL)
	}

	if p.ClientIR.UserAgent != "" {
		cfg.UserAgent = goString(p.ClientIR.UserAgent)
	}
	if p.ClientIR.Timeout > 0 {
		cfg.Timeout = formatDuration(p.ClientIR.Timeout)
	}
	if p.ClientIR.RetryMax > 0 {
		cfg.RetryMax = p.ClientIR.RetryMax
	}
	if p.ClientIR.RetryWaitMin > 0 {
		cfg.RetryWaitMin = formatDuration(p.ClientIR.RetryWaitMin)
	}
	if p.ClientIR.RetryWaitMax > 0 {
		cfg.RetryWaitMax = formatDuration(p.ClientIR.RetryWaitMax)
	}

	if p.ClientIR.Pagination != nil {
		if p.ClientIR.Pagination.Style != "" {
			cfg.PaginationStyle = goString(p.ClientIR.Pagination.Style)
		}
		if p.ClientIR.Pagination.PageParam != "" {
			cfg.PageParam = goString(p.ClientIR.Pagination.PageParam)
		}
		if p.ClientIR.Pagination.PerPageParam != "" {
			cfg.PerPageParam = goString(p.ClientIR.Pagination.PerPageParam)
		}
		if p.ClientIR.Pagination.NextLinkHeader != "" {
			cfg.NextLinkRel = goString(p.ClientIR.Pagination.NextLinkHeader)
		}
		if p.ClientIR.Pagination.CursorField != "" {
			cfg.CursorField = goString(p.ClientIR.Pagination.CursorField)
		}
	}

	return cfg
}

func jsonTag(name string) string {
	return fmt.Sprintf("`json:%q`", name)
}

func formatDuration(d time.Duration) string {
	switch {
	case d == 0:
		return "0"
	case d%time.Hour == 0 && d >= time.Hour:
		return fmt.Sprintf("%d * time.Hour", d/time.Hour)
	case d%time.Minute == 0 && d >= time.Minute:
		return fmt.Sprintf("%d * time.Minute", d/time.Minute)
	case d%time.Second == 0 && d >= time.Second:
		return fmt.Sprintf("%d * time.Second", d/time.Second)
	case d%time.Millisecond == 0 && d >= time.Millisecond:
		return fmt.Sprintf("%d * time.Millisecond", d/time.Millisecond)
	case d%time.Microsecond == 0 && d >= time.Microsecond:
		return fmt.Sprintf("%d * time.Microsecond", d/time.Microsecond)
	default:
		return fmt.Sprintf("%d * time.Nanosecond", d.Nanoseconds())
	}
}

func clientGoFile(cfg clientConfig) File {
	return Template("internal/client/client.go", clientGoTemplate, cfg)
}

func authGoFile(cfg clientConfig) File {
	return Template("internal/client/auth.go", authGoTemplate, cfg)
}

func modelsGoFile(cfg clientConfig) File {
	return Template("internal/client/models.go", modelsGoTemplate, cfg)
}

func errorsGoFile(cfg clientConfig) File {
	return Template("internal/client/errors.go", errorsGoTemplate, cfg)
}

func retryGoFile(cfg clientConfig) File {
	return Template("internal/client/retry.go", retryGoTemplate, cfg)
}

func paginationGoFile(cfg clientConfig) File {
	return Template("internal/client/pagination.go", paginationGoTemplate, cfg)
}

func loggingGoFile(_ clientConfig) File {
	return LoggingFile()
}

const clientGoTemplate = `package client

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
	httpClient        *http.Client
	baseURL           string
	userAgent         string
	interceptors       []RequestInterceptor
	schemeInterceptors map[string]RequestInterceptor
	schemeOrder       []string
	retryPolicy       RetryPolicy
	maxRetries        int
	backoff           BackoffFunc
	logging           LoggingConfig
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
		httpClient:  &http.Client{Timeout: {{.Timeout}}},
		baseURL:     {{.BaseURL}},
		userAgent:   {{.UserAgent}},
		maxRetries:  {{.RetryMax}},
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
`

const authGoTemplate = `package client

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultOAuthHTTPClientTimeout = 30 * time.Second
	oauthTokenRefreshBuffer       = 30 * time.Second
)

// APIKeyAuth returns an interceptor that injects an API key as a header, query, or cookie.
func APIKeyAuth(apiKey, in, name string) RequestInterceptor {
	return func(req *http.Request) error {
		switch strings.ToLower(in) {
		case "header":
			req.Header.Set(name, apiKey)
		case "query":
			q := req.URL.Query()
			q.Set(name, apiKey)
			req.URL.RawQuery = q.Encode()
		case "cookie":
			req.AddCookie(&http.Cookie{Name: name, Value: apiKey})
		default:
			return fmt.Errorf("unsupported API key location %q", in)
		}
		return nil
	}
}

// BasicAuth returns an interceptor that injects an Authorization: Basic header.
func BasicAuth(username, password string) RequestInterceptor {
	return func(req *http.Request) error {
		creds := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
		req.Header.Set("Authorization", "Basic "+creds)
		return nil
	}
}

// BearerAuth returns an interceptor that injects an Authorization: Bearer <token> header.
func BearerAuth(token string) RequestInterceptor {
	return func(req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	}
}

// OAuth2ClientCredentials returns an interceptor that obtains and caches an access
// token using the OAuth2 client_credentials flow. It uses a default HTTP client
// with a 30 second timeout; use OAuth2ClientCredentialsWithHTTPClient to customize
// the HTTP client used for token requests.
func OAuth2ClientCredentials(tokenURL, clientID, clientSecret string, scopes []string) RequestInterceptor {
	return OAuth2ClientCredentialsWithHTTPClient(tokenURL, clientID, clientSecret, scopes, nil)
}

// OAuth2ClientCredentialsWithHTTPClient returns an interceptor like
// OAuth2ClientCredentials but uses the provided *http.Client for token requests.
// If httpClient is nil, a default client is used.
func OAuth2ClientCredentialsWithHTTPClient(tokenURL, clientID, clientSecret string, scopes []string, httpClient *http.Client) RequestInterceptor {
	return bearerFromTokenSource(&oauth2TokenSource{
		tokenURL:     tokenURL,
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient:   defaultOAuthHTTPClient(httpClient),
		form:         clientCredentialsForm(scopes),
	})
}

// OAuth2Password returns an interceptor that obtains and caches an access token
// using the OAuth2 resource owner password credentials flow. It uses a default
// HTTP client with a 30 second timeout; use OAuth2PasswordWithHTTPClient to
// customize the HTTP client used for token requests.
func OAuth2Password(tokenURL, username, password, clientID, clientSecret string, scopes []string) RequestInterceptor {
	return OAuth2PasswordWithHTTPClient(tokenURL, username, password, clientID, clientSecret, scopes, nil)
}

// OAuth2PasswordWithHTTPClient returns an interceptor like OAuth2Password but
// uses the provided *http.Client for token requests. If httpClient is nil, a
// default client is used.
func OAuth2PasswordWithHTTPClient(tokenURL, username, password, clientID, clientSecret string, scopes []string, httpClient *http.Client) RequestInterceptor {
	return bearerFromTokenSource(&oauth2TokenSource{
		tokenURL:     tokenURL,
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient:   defaultOAuthHTTPClient(httpClient),
		form: func() url.Values {
			data := url.Values{}
			data.Set("grant_type", "password")
			data.Set("username", username)
			data.Set("password", password)
			if len(scopes) > 0 {
				data.Set("scope", strings.Join(scopes, " "))
			}
			return data
		},
	})
}

// OAuth2AuthorizationCodeRefresh returns an interceptor that obtains and caches
// an access token by refreshing a practitioner-supplied refresh token via the
// OAuth2 authorization_code flow's token endpoint. The initial authorization-
// code exchange requires an interactive browser redirect and must happen
// out-of-band; this interceptor only exercises the non-interactive refresh
// path. When the token endpoint rotates refresh tokens, the new refresh token
// from each response replaces the stored one. It uses a default HTTP client
// with a 30 second timeout; use OAuth2AuthorizationCodeRefreshWithHTTPClient to
// customize the HTTP client used for token requests.
func OAuth2AuthorizationCodeRefresh(tokenURL, refreshToken, clientID, clientSecret string, scopes []string) RequestInterceptor {
	return OAuth2AuthorizationCodeRefreshWithHTTPClient(tokenURL, refreshToken, clientID, clientSecret, scopes, nil)
}

// OAuth2AuthorizationCodeRefreshWithHTTPClient returns an interceptor like
// OAuth2AuthorizationCodeRefresh but uses the provided *http.Client for token
// requests. If httpClient is nil, a default client is used.
func OAuth2AuthorizationCodeRefreshWithHTTPClient(tokenURL, refreshToken, clientID, clientSecret string, scopes []string, httpClient *http.Client) RequestInterceptor {
	ts := &oauth2TokenSource{
		tokenURL:     tokenURL,
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient:   defaultOAuthHTTPClient(httpClient),
		refreshToken: refreshToken,
	}
	// form reads ts.refreshToken under the token source's write lock, so it
	// always sends the latest refresh token after a rotation.
	ts.form = func() url.Values {
		data := url.Values{}
		data.Set("grant_type", "refresh_token")
		data.Set("refresh_token", ts.refreshToken)
		if len(scopes) > 0 {
			data.Set("scope", strings.Join(scopes, " "))
		}
		return data
	}
	return bearerFromTokenSource(ts)
}

// OpenIDConnect returns an interceptor that obtains and caches an access token
// using the OAuth2 client_credentials flow against the token endpoint of an
// OpenID Connect provider. When tokenURL is non-empty it is used directly and
// no discovery is performed; otherwise the token endpoint is discovered (once,
// then cached) from the discovery document at discoveryURL. It uses a default
// HTTP client with a 30 second timeout; use OpenIDConnectWithHTTPClient to
// customize the HTTP client used for discovery and token requests.
func OpenIDConnect(discoveryURL, tokenURL, clientID, clientSecret string, scopes []string) RequestInterceptor {
	return OpenIDConnectWithHTTPClient(discoveryURL, tokenURL, clientID, clientSecret, scopes, nil)
}

// OpenIDConnectWithHTTPClient returns an interceptor like OpenIDConnect but
// uses the provided *http.Client for discovery and token requests. If
// httpClient is nil, a default client is used.
func OpenIDConnectWithHTTPClient(discoveryURL, tokenURL, clientID, clientSecret string, scopes []string, httpClient *http.Client) RequestInterceptor {
	ts := &oidcTokenSource{
		discoveryURL: discoveryURL,
		tokenURL:     tokenURL,
		clientID:     clientID,
		clientSecret: clientSecret,
		scopes:       scopes,
		httpClient:   defaultOAuthHTTPClient(httpClient),
	}
	return func(req *http.Request) error {
		token, err := ts.Token(req.Context())
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	}
}

// defaultOAuthHTTPClient returns httpClient, or a default client with a 30
// second timeout when httpClient is nil.
func defaultOAuthHTTPClient(httpClient *http.Client) *http.Client {
	if httpClient == nil {
		return &http.Client{Timeout: defaultOAuthHTTPClientTimeout}
	}
	return httpClient
}

// clientCredentialsForm returns the grant form for the OAuth2
// client_credentials flow.
func clientCredentialsForm(scopes []string) func() url.Values {
	return func() url.Values {
		data := url.Values{}
		data.Set("grant_type", "client_credentials")
		if len(scopes) > 0 {
			data.Set("scope", strings.Join(scopes, " "))
		}
		return data
	}
}

// bearerFromTokenSource returns an interceptor that injects an
// Authorization: Bearer header carrying a token obtained from ts.
func bearerFromTokenSource(ts *oauth2TokenSource) RequestInterceptor {
	return func(req *http.Request) error {
		token, err := ts.Token(req.Context())
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	}
}

// oidcTokenSource resolves the token endpoint for an OpenID Connect provider
// (from a configured token URL override or, on first use, the discovery
// document) and delegates token acquisition to an inner client-credentials
// token source. It is safe for concurrent use.
type oidcTokenSource struct {
	discoveryURL string
	tokenURL     string
	clientID     string
	clientSecret string
	scopes       []string
	httpClient   *http.Client

	mu    sync.Mutex
	inner *oauth2TokenSource
}

func (ts *oidcTokenSource) Token(ctx context.Context) (string, error) {
	inner, err := ts.resolve(ctx)
	if err != nil {
		return "", err
	}
	return inner.Token(ctx)
}

// resolve returns the inner client-credentials token source, discovering the
// token endpoint from the OpenID Connect discovery document on first use. A
// configured token URL override skips discovery entirely.
func (ts *oidcTokenSource) resolve(ctx context.Context) (*oauth2TokenSource, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.inner != nil {
		return ts.inner, nil
	}
	tokenURL := ts.tokenURL
	if tokenURL == "" {
		endpoint, err := ts.discoverTokenEndpoint(ctx)
		if err != nil {
			return nil, err
		}
		tokenURL = endpoint
	}
	ts.inner = &oauth2TokenSource{
		tokenURL:     tokenURL,
		clientID:     ts.clientID,
		clientSecret: ts.clientSecret,
		httpClient:   ts.httpClient,
		form:         clientCredentialsForm(ts.scopes),
	}
	return ts.inner, nil
}

// discoverTokenEndpoint fetches the OpenID Connect discovery document and
// returns its token_endpoint.
func (ts *oidcTokenSource) discoverTokenEndpoint(ctx context.Context) (string, error) {
	if ts.discoveryURL == "" {
		return "", fmt.Errorf("openid connect: no discovery URL configured and no token URL override set")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.discoveryURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := ts.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("openid connect discovery returned status %d", resp.StatusCode)
	}
	// Decoded into a map rather than a struct so this template needs no struct
	// tags.
	var doc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return "", err
	}
	endpoint, _ := doc["token_endpoint"].(string)
	if endpoint == "" {
		return "", fmt.Errorf("openid connect discovery document missing token_endpoint")
	}
	return endpoint, nil
}

// oauth2TokenSource obtains and caches an access token by POSTing a
// grant-specific form to the token endpoint. It is safe for concurrent use.
// form builds the grant-specific request body and runs under the write lock,
// so it can read mutable source state (the current refresh token). When the
// token endpoint rotates refresh tokens, the new refresh token from the
// response replaces the stored one.
type oauth2TokenSource struct {
	tokenURL     string
	clientID     string
	clientSecret string
	httpClient   *http.Client
	form         func() url.Values

	mu           sync.RWMutex
	token        string
	expiry       time.Time
	tokenType    string
	refreshToken string
}

type tokenResponse struct {
	AccessToken  string {{.AccessTokenTag}}
	TokenType    string {{.TokenTypeTag}}
	ExpiresIn    int    {{.ExpiresInTag}}
	RefreshToken string {{.RefreshTokenTag}}
}

func (ts *oauth2TokenSource) Token(ctx context.Context) (string, error) {
	ts.mu.RLock()
	if ts.token != "" && time.Now().Add(oauthTokenRefreshBuffer).Before(ts.expiry) {
		tok := ts.token
		ts.mu.RUnlock()
		return tok, nil
	}
	ts.mu.RUnlock()

	// Serialize refresh attempts. Double-check the cache after acquiring the write
	// lock in case another goroutine refreshed the token while we were waiting.
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.token != "" && time.Now().Add(oauthTokenRefreshBuffer).Before(ts.expiry) {
		return ts.token, nil
	}

	data := ts.form()

	r, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if ts.clientID != "" || ts.clientSecret != "" {
		r.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(ts.clientID+":"+ts.clientSecret)))
	}

	resp, err := ts.httpClient.Do(r)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("oauth2 token request returned status %d", resp.StatusCode)
	}

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", err
	}
	if tr.AccessToken == "" {
		return "", fmt.Errorf("oauth2 token response missing access_token")
	}

	ts.token = tr.AccessToken
	ts.tokenType = tr.TokenType
	ts.expiry = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	if tr.RefreshToken != "" {
		ts.refreshToken = tr.RefreshToken
	}

	return tr.AccessToken, nil
}

// ParseScopes splits a space-separated OAuth2 scope string into a slice of
// individual scope tokens. It is used by the generated provider Configure to
// translate the practitioner-configured scopes attribute into the []string the
// OAuth2 interceptors expect. Empty input yields an empty (non-nil) slice.
func ParseScopes(s string) []string {
	return strings.Fields(s)
}
`

const modelsGoTemplate = `package client

import "encoding/json"

// Envelope is a generic response wrapper used when the API wraps objects in a predictable envelope.
type Envelope struct {
	Data json.RawMessage {{.DataTag}}
}

// ErrorResponse captures a common error payload shape returned by APIs.
type ErrorResponse struct {
	Message string {{.MessageTag}}
	Code    string {{.CodeTag}}
}

// Empty is a placeholder empty request or response body.
type Empty struct{}
`

const errorsGoTemplate = `package client

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
`

const retryGoTemplate = `package client

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
// provider's configured min/max wait windows ({{.RetryWaitMin}} ..
// {{.RetryWaitMax}}) rather than hardcoded 1s/30s constants, so the
// RetryWaitMin/RetryWaitMax values read from the IR are actually honored by the
// generated client (M-11). The prior comment called this "full jitter", which
// was inaccurate (L-29).
func DefaultBackoff(attempt int) time.Duration {
	minWait := {{.RetryWaitMin}}
	maxWait := {{.RetryWaitMax}}
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
`

const paginationGoTemplate = `package client

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// PaginationStyle enumerates supported pagination strategies.
type PaginationStyle string

const (
	// PaginationStyleOffset requests pages using offset/limit or page/per_page parameters.
	PaginationStyleOffset PaginationStyle = "offset"
	// PaginationStyleCursor requests pages using a cursor token.
	PaginationStyleCursor PaginationStyle = "cursor"
	// PaginationStyleLinkHeader follows RFC 5988 Link headers to retrieve the next page.
	PaginationStyleLinkHeader PaginationStyle = "link_header"
	// PaginationStyleNone disables pagination helpers.
	PaginationStyleNone PaginationStyle = "none"
)

// Pagination holds the configured pagination strategy.
type Pagination struct {
	Style        PaginationStyle
	PageParam    string
	PerPageParam string
	NextLinkRel  string
	CursorField  string
}

// DefaultPagination returns the provider's default pagination configuration.
func DefaultPagination() Pagination {
	return Pagination{
		Style:        {{.PaginationStyle}},
		PageParam:    {{.PageParam}},
		PerPageParam: {{.PerPageParam}},
		NextLinkRel:  {{.NextLinkRel}},
		CursorField:  {{.CursorField}},
	}
}

// ExtractLinkHeader returns the URL for the requested rel from an RFC 5988 Link header.
func ExtractLinkHeader(header string, rel string) string {
	links := parseLinkHeader(header)
	return links[rel]
}

func parseLinkHeader(header string) map[string]string {
	result := make(map[string]string)
	for _, part := range splitLinks(header) {
		url, rest := splitLink(part)
		if url == "" {
			continue
		}
		for _, p := range parseParams(rest) {
			if p.key == "rel" {
				result[p.value] = url
				break
			}
		}
	}
	return result
}

func splitLinks(header string) []string {
	var parts []string
	start := 0
	depth := 0
	for i, r := range header {
		switch r {
		case '<':
			depth++
		case '>':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, header[start:i])
				start = i + 1
			}
		}
	}
	if start < len(header) {
		parts = append(parts, header[start:])
	}
	return parts
}

func splitLink(part string) (string, string) {
	part = strings.TrimSpace(part)
	if part == "" || part[0] != '<' {
		return "", ""
	}
	end := strings.IndexByte(part, '>')
	if end < 0 {
		return "", ""
	}
	return part[1:end], part[end+1:]
}

type linkParam struct {
	key   string
	value string
}

func parseParams(rest string) []linkParam {
	var params []linkParam
	for _, p := range strings.Split(rest, ";") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		idx := strings.IndexByte(p, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(p[:idx])
		value := strings.TrimSpace(p[idx+1:])
		if len(value) > 1 && value[0] == '"' && value[len(value)-1] == '"' {
			value = value[1 : len(value)-1]
		}
		params = append(params, linkParam{key: key, value: value})
	}
	return params
}

// ListAllPages repeatedly calls fetch with pagination parameters and collects all page bodies.
// The next callback is invoked after each page; it may update params and should return false
// when there are no more pages.
func ListAllPages(ctx context.Context, params url.Values, fetch func(context.Context, url.Values) (*http.Response, error), next func(*http.Response, []byte, url.Values) bool) ([][]byte, error) {
	var pages [][]byte
	current := cloneValues(params)
	for {
		resp, err := fetch(ctx, current)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		pages = append(pages, body)
		if next == nil || !next(resp, body, current) {
			return pages, nil
		}
	}
}

func cloneValues(v url.Values) url.Values {
	if v == nil {
		return nil
	}
	out := make(url.Values, len(v))
	for key, values := range v {
		out[key] = append([]string(nil), values...)
	}
	return out
}
`
