package main

import (
	"context"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/signalbreak-labs/eidos/pkg/config"
	"github.com/signalbreak-labs/eidos/pkg/specsource"
)

// Remote `--spec` fetching (PROJECT_DESIGN §23). `--spec` accepts a local file path
// (the historical behavior) or an http(s) URL that is fetched with the same
// hardening the deleted pkg/parser/ref_external.go applied to remote $refs:
// an https-only scheme allowlist (http is an explicit opt-in), an SSRF guard
// that rejects private/loopback/link-local hosts (re-applied on every redirect
// target, with the validated IPs pinned so a DNS rebind cannot redirect the
// dial, N-55), a 30s timeout, and a 10 MiB response cap. The fetch runs on the
// command's context so a Ctrl-C aborts an in-flight download (N-53).
//
// The actual loading and hardening live in pkg/specsource, shared with the MCP
// server (N-62), so the two entry points cannot drift again; this file keeps
// only the CLI-facing surface: flag registration, the flag/config merge, and
// thin wrappers. Credentials (Phase 2) are passed only via environment
// variables, resolved at fetch time and never logged: bearer/basic/apiKey
// headers and an OAuth2 client-credentials token. A URL that embeds userinfo is
// rejected outright.

// specAuth describes opt-in authentication for a remote spec fetch. It aliases
// the shared type so the CLI's flag/config merge feeds directly into the
// hardened fetch without a second struct to keep in sync.
type specAuth = specsource.Auth

// remoteSpecOptions controls a remote spec fetch. Fields mirror the shared
// specsource.Options; the CLI carries them separately so the flag/config merge
// can build them without importing the shared type into every test.
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

// shared converts the CLI option carrier into the shared fetch options. The
// private-IP escape hatch has no flag; like the MCP it is read from
// EIDOS_SPEC_ALLOW_PRIVATE=1 at fetch time.
func (o remoteSpecOptions) shared() specsource.Options {
	return specsource.Options{
		AllowHTTP:     o.allowHTTP,
		AllowPrivate:  os.Getenv("EIDOS_SPEC_ALLOW_PRIVATE") == "1",
		SkipHostCheck: o.skipHostCheck,
		Auth:          o.auth,
	}
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
	return specsource.IsURL(specArg)
}

// loadSpecBytes loads the spec named by specArg: a local file via the shared
// resolver (the historical path) or a remote URL via a hardened HTTP fetch. It
// returns the raw bytes and the HTTP Content-Type (empty for local files) so
// callers can route JSON vs YAML parsing. The fetch runs on ctx so a Ctrl-C
// aborts it (N-53). Credentials never appear in returned errors.
func loadSpecBytes(ctx context.Context, specArg string, opts remoteSpecOptions) ([]byte, string, error) {
	return specsource.LoadSpec(ctx, specArg, opts.shared())
}

// fetchRemoteSpec downloads a remote spec URL through a freshly hardened
// client. It returns the response body and the HTTP Content-Type header.
func fetchRemoteSpec(ctx context.Context, rawURL string, opts remoteSpecOptions) ([]byte, string, error) {
	return specsource.FetchURL(ctx, rawURL, opts.shared())
}
