package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/signalbreak-labs/eidos/pkg/config"
)

// Remote `--spec` fetching (PROJECT_DESIGN §23). `--spec` accepts a local file path
// (the historical behavior) or an http(s) URL that is fetched with the same
// hardening the deleted pkg/parser/ref_external.go applied to remote $refs:
// an https-only scheme allowlist (http is an explicit opt-in), an SSRF guard
// that rejects private/loopback/link-local hosts (re-applied on every redirect
// target), a 30s timeout, and a 10 MiB response cap.
//
// Credentials (Phase 2) are passed only via environment variables, resolved at
// fetch time and never logged: bearer/basic/apiKey headers and an OAuth2
// client-credentials token. A URL that embeds userinfo is rejected outright.

const (
	// remoteSpecTimeout caps how long a remote spec fetch may take.
	remoteSpecTimeout = 30 * time.Second
	// remoteSpecMaxBytes caps how large a remote spec response body may be.
	remoteSpecMaxBytes = 10 << 20 // 10 MiB
	// remoteSpecTokenTimeout caps an OAuth2 client-credentials token request.
	remoteSpecTokenTimeout = 15 * time.Second
)

// specAuth describes opt-in authentication for a remote spec fetch. Credential
// fields name environment variables; values are read at request time.
type specAuth struct {
	Scheme          string // bearer | basic | apiKey | oauth2-client-credentials
	HeaderName      string
	TokenEnv        string
	UsernameEnv     string
	PasswordEnv     string
	KeyEnv          string
	TokenURL        string
	ClientIDEnv     string
	ClientSecretEnv string
}

// remoteSpecOptions controls a remote spec fetch.
type remoteSpecOptions struct {
	// allowHTTP permits http:// URLs (opt-in; https is the default).
	allowHTTP bool
	// auth is nil for an unauthenticated fetch (Phase 1 MVP).
	auth *specAuth
	// skipHostCheck bypasses the SSRF host guard for unit tests that serve a
	// spec from httptest (127.0.0.1, which the guard rejects). The CLI never
	// sets it; it exists solely to make fetchRemoteSpec testable.
	skipHostCheck bool
}

// remoteSpecFlags carries the opt-in remote-spec options shared by the generate
// and generate-config commands. Credential fields are environment-variable
// names, never values; the loader reads them at fetch time.
type remoteSpecFlags struct {
	allowHTTP       bool
	authScheme      string
	tokenEnv        string
	usernameEnv     string
	passwordEnv     string
	keyEnv          string
	headerName      string
	tokenURL        string
	clientIDEnv     string
	clientSecretEnv string
}

// registerRemoteSpecFlags binds the --spec-* flags on a command.
func (f *remoteSpecFlags) register(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.allowHTTP, "spec-allow-http", false, "Permit http:// spec URLs (https is the default for remote specs)")
	cmd.Flags().StringVar(&f.authScheme, "spec-auth-scheme", "", "Authenticate a remote spec fetch: bearer, basic, apiKey, or oauth2-client-credentials (credential values come from environment variables, never flags)")
	cmd.Flags().StringVar(&f.tokenEnv, "spec-token-env", "", "Environment variable holding the bearer token for --spec-auth-scheme bearer")
	cmd.Flags().StringVar(&f.usernameEnv, "spec-username-env", "", "Environment variable holding the username for --spec-auth-scheme basic")
	cmd.Flags().StringVar(&f.passwordEnv, "spec-password-env", "", "Environment variable holding the password for --spec-auth-scheme basic")
	cmd.Flags().StringVar(&f.keyEnv, "spec-key-env", "", "Environment variable holding the API key for --spec-auth-scheme apiKey")
	cmd.Flags().StringVar(&f.headerName, "spec-header-name", "", "Header name the apiKey scheme sends the key in")
	cmd.Flags().StringVar(&f.tokenURL, "spec-token-url", "", "OAuth2 token endpoint for --spec-auth-scheme oauth2-client-credentials")
	cmd.Flags().StringVar(&f.clientIDEnv, "spec-client-id-env", "", "Environment variable holding the OAuth2 client ID")
	cmd.Flags().StringVar(&f.clientSecretEnv, "spec-client-secret-env", "", "Environment variable holding the OAuth2 client secret")
}

// options merges CLI flags over generator.yaml spec.auth defaults (flags win)
// into the loader's options. Only non-empty flag values override config so an
// absent flag inherits the config's auth section.
func (f *remoteSpecFlags) options(cfg *config.Config) remoteSpecOptions {
	opts := remoteSpecOptions{allowHTTP: f.allowHTTP}
	auth := &specAuth{
		Scheme:          f.authScheme,
		HeaderName:      f.headerName,
		TokenEnv:        f.tokenEnv,
		UsernameEnv:     f.usernameEnv,
		PasswordEnv:     f.passwordEnv,
		KeyEnv:          f.keyEnv,
		TokenURL:        f.tokenURL,
		ClientIDEnv:     f.clientIDEnv,
		ClientSecretEnv: f.clientSecretEnv,
	}
	if cfg != nil && cfg.Spec.Auth != nil {
		ca := cfg.Spec.Auth
		if auth.Scheme == "" {
			auth.Scheme = ca.Scheme
		}
		if auth.HeaderName == "" {
			auth.HeaderName = ca.HeaderName
		}
		if auth.TokenEnv == "" {
			auth.TokenEnv = ca.TokenEnv
		}
		if auth.UsernameEnv == "" {
			auth.UsernameEnv = ca.UsernameEnv
		}
		if auth.PasswordEnv == "" {
			auth.PasswordEnv = ca.PasswordEnv
		}
		if auth.KeyEnv == "" {
			auth.KeyEnv = ca.KeyEnv
		}
		if auth.TokenURL == "" {
			auth.TokenURL = ca.TokenURL
		}
		if auth.ClientIDEnv == "" {
			auth.ClientIDEnv = ca.ClientIDEnv
		}
		if auth.ClientSecretEnv == "" {
			auth.ClientSecretEnv = ca.ClientSecretEnv
		}
	}
	if strings.TrimSpace(auth.Scheme) != "" {
		opts.auth = auth
	}
	return opts
}

// isRemoteSpecURL reports whether specArg names a remote http(s) URL rather
// than a local file path. Non-http(s) schemes (e.g. file://, ftp://) are not
// treated as remote and fall through to the local-file path, which reports a
// sensible file-read error.
func isRemoteSpecURL(specArg string) bool {
	u, err := url.Parse(specArg)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return true
	}
	return false
}

// loadSpecBytes loads the spec named by specArg: a local file via os.ReadFile
// (the historical path) or a remote URL via a hardened HTTP fetch. It returns
// the raw bytes and the HTTP Content-Type (empty for local files) so callers
// can route JSON vs YAML parsing. Credentials never appear in returned errors.
func loadSpecBytes(specArg string, opts remoteSpecOptions) ([]byte, string, error) {
	if !isRemoteSpecURL(specArg) {
		data, err := os.ReadFile(filepath.Clean(specArg))
		if err != nil {
			return nil, "", fmt.Errorf("failed to read spec %q: %w", specArg, err)
		}
		return data, "", nil
	}
	return fetchRemoteSpec(specArg, opts)
}

// fetchRemoteSpec downloads a remote spec URL through a freshly hardened
// client. It returns the response body and the HTTP Content-Type header.
func fetchRemoteSpec(rawURL string, opts remoteSpecOptions) ([]byte, string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse spec URL: %w", err)
	}
	// No URL-embedded credentials: userinfo in a CLI argument is visible in
	// shell history, ps, and process accounting. Require the env-var form.
	if u.User != nil {
		return nil, "", fmt.Errorf("spec URL must not embed credentials (userinfo is visible in shell history and process listings); use --spec-auth-* flags naming environment variables instead")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && (scheme != "http" || !opts.allowHTTP) {
		return nil, "", fmt.Errorf("spec URL scheme %q is not allowed: https is required for remote specs (pass --spec-allow-http to permit http)", u.Scheme)
	}
	if !opts.skipHostCheck {
		if err := checkRemoteSpecHost(u, os.Getenv("EIDOS_SPEC_ALLOW_PRIVATE") == "1"); err != nil {
			return nil, "", err
		}
	}

	client := newRemoteSpecClient(opts)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, "", fmt.Errorf("failed to build spec request: %w", err)
	}
	if err := applySpecAuth(req, opts.auth); err != nil {
		return nil, "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to fetch spec URL %s: %w (download the spec manually and pass a local path, or check the URL)", redactRemoteURL(rawURL), err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // best-effort response body close

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("failed to fetch spec URL: server returned %s for %s; download the spec manually and pass a local path", resp.Status, redactRemoteURL(rawURL))
	}

	// Cap the response body size. Read one byte past the limit so truncation is
	// reported rather than silently using a partial document.
	limited := io.LimitReader(resp.Body, remoteSpecMaxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read spec URL response: %w", err)
	}
	if int64(len(data)) > remoteSpecMaxBytes {
		return nil, "", fmt.Errorf("spec URL response exceeds the %d-byte maximum (%s); download the spec manually and pass a local path", remoteSpecMaxBytes, redactRemoteURL(rawURL))
	}

	return data, resp.Header.Get("Content-Type"), nil
}

// newRemoteSpecClient builds the hardened HTTP client used for remote spec
// fetches: a 30s timeout and a redirect policy that re-applies the same scheme
// and private-IP rules so a 302 cannot redirect to an internal address.
func newRemoteSpecClient(opts remoteSpecOptions) *http.Client {
	return &http.Client{
		Timeout: remoteSpecTimeout,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			return checkRemoteSpecRedirect(req.URL, opts)
		},
	}
}

// checkRemoteSpecHost rejects a spec URL whose host is a literal private/
// loopback/link-local IP or whose hostname resolves to one. This is the SSRF
// guard: it prevents a spec URL (possibly attacker-controlled in CI) from
// reaching cloud metadata endpoints or internal services.
//
// allowPrivate relaxes the guard for the INITIAL host only (never for redirect
// targets). It is set from EIDOS_SPEC_ALLOW_PRIVATE=1, an explicit operator
// escape hatch for local development against a mock API server; the default
// posture blocks private hosts.
func checkRemoteSpecHost(u *url.URL, allowPrivate bool) error {
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("spec URL has no host")
	}
	if allowPrivate {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateOrLocalIP(ip) {
			return fmt.Errorf("spec URL host %q resolves to private/local IP %s, which is blocked (SSRF guard); download the spec manually and pass a local path", host, ip)
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteSpecTimeout)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("failed to resolve spec URL host %q: %w", host, err)
	}
	for _, ip := range ips {
		if isPrivateOrLocalIP(ip.IP) {
			return fmt.Errorf("spec URL host %q resolves to private/local IP %s, which is blocked (SSRF guard); download the spec manually and pass a local path", host, ip.IP)
		}
	}
	return nil
}

// checkRemoteSpecRedirect enforces the scheme and private-IP policy on HTTP
// redirect targets, mirroring checkRemoteSpecHost. Redirect targets are never
// exempted from the private-IP guard (a 302 is the classic SSRF vector).
func checkRemoteSpecRedirect(u *url.URL, opts remoteSpecOptions) error {
	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && (scheme != "http" || !opts.allowHTTP) {
		return fmt.Errorf("redirect to unsupported scheme %q (https required)", u.Scheme)
	}
	return checkRemoteSpecHost(u, false)
}

// isPrivateOrLocalIP reports whether ip is a private, loopback, or link-local
// address — the families most commonly targeted by SSRF attacks.
func isPrivateOrLocalIP(ip net.IP) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}

// redactRemoteURL strips any userinfo from a URL for inclusion in error text
// (the URL is otherwise safe to log; query parameters are preserved for
// diagnosability).
func redactRemoteURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.User = nil
	return u.String()
}

// applySpecAuth attaches the configured authentication headers to the spec
// request, resolving credential values from environment variables immediately
// before the request. Empty required env vars fail loud rather than sending an
// unauthenticated request the vendor will reject.
func applySpecAuth(req *http.Request, auth *specAuth) error {
	if auth == nil || strings.TrimSpace(auth.Scheme) == "" {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(auth.Scheme)) {
	case "bearer":
		tok := os.Getenv(strings.TrimSpace(auth.TokenEnv))
		if tok == "" {
			return fmt.Errorf("spec auth scheme bearer requires env var %s to be set", auth.TokenEnv)
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	case "basic":
		user := os.Getenv(strings.TrimSpace(auth.UsernameEnv))
		pass := os.Getenv(strings.TrimSpace(auth.PasswordEnv))
		if user == "" && pass == "" {
			return fmt.Errorf("spec auth scheme basic requires env vars %s and %s to be set", auth.UsernameEnv, auth.PasswordEnv)
		}
		req.SetBasicAuth(user, pass)
	case "apikey":
		key := os.Getenv(strings.TrimSpace(auth.KeyEnv))
		if key == "" {
			return fmt.Errorf("spec auth scheme apiKey requires env var %s to be set", auth.KeyEnv)
		}
		header := strings.TrimSpace(auth.HeaderName)
		if header == "" {
			return fmt.Errorf("spec auth scheme apiKey requires a header_name")
		}
		req.Header.Set(header, key)
	case "oauth2-client-credentials":
		id := os.Getenv(strings.TrimSpace(auth.ClientIDEnv))
		secret := os.Getenv(strings.TrimSpace(auth.ClientSecretEnv))
		tok, err := fetchClientCredentialsToken(auth.TokenURL, id, secret)
		if err != nil {
			return fmt.Errorf("failed to obtain spec auth token: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	default:
		return fmt.Errorf("unknown spec auth scheme %q (want bearer, basic, apiKey, or oauth2-client-credentials)", auth.Scheme)
	}
	return nil
}

// jsonField extracts a top-level string field from a JSON object. It is used
// for the OAuth2 token response (access_token); the body is small and the
// structure is the standard OAuth2 JSON object.
func jsonField(body []byte, name string) (string, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return "", fmt.Errorf("token response is not a JSON object: %w", err)
	}
	raw, ok := m[name]
	if !ok {
		return "", fmt.Errorf("field %q not found", name)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("field %q is not a string", name)
	}
	return s, nil
}

// fetchClientCredentialsToken performs an OAuth2 client-credentials grant
// against tokenURL, mirroring the generated client's clientCredentialsForm.
func fetchClientCredentialsToken(tokenURL, clientID, clientSecret string) (string, error) {
	if strings.TrimSpace(tokenURL) == "" {
		return "", fmt.Errorf("oauth2-client-credentials requires a token_url")
	}
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)

	client := &http.Client{Timeout: remoteSpecTokenTimeout}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // best-effort response body close
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("token endpoint returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, remoteSpecMaxBytes+1))
	if err != nil {
		return "", fmt.Errorf("failed to read token response: %w", err)
	}
	if int64(len(body)) > remoteSpecMaxBytes {
		return "", fmt.Errorf("token response exceeds the %d-byte maximum", remoteSpecMaxBytes)
	}
	// Extract the access_token field. A tiny inline parser avoids a heavy
	// dependency for one field; the response shape is the standard OAuth2
	// JSON object.
	token, err := jsonField(body, "access_token")
	if err != nil {
		return "", fmt.Errorf("token response has no access_token: %w", err)
	}
	if strings.TrimSpace(token) == "" {
		return "", fmt.Errorf("token response access_token is empty")
	}
	return token, nil
}
