package client

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
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
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
