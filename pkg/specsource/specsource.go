// Package specsource is the single source of truth for loading OpenAPI spec
// (and generator.yaml) inputs across the CLI and the MCP server.
//
// Both the CLI (cmd/eidos/remote_spec.go) and the MCP tools (pkg/mcp/specsource.go)
// previously carried ~200 lines each of the same resolution + SSRF-guard logic,
// with knobs that had already drifted apart (the CLI lacked file:// support that
// the MCP had, N-62). This package unifies them: the same reference resolution
// (local path, file:// URL, http(s):// URL, or inline content sentinel) and the
// same hardened remote fetch (https-only by default with an opt-in http scheme,
// an SSRF guard that rejects private/loopback/link-local hosts, a 30s timeout,
// and a 10 MiB response cap).
//
// The remote fetch also fixes two latent issues that lived in both copies:
//   - Cancellation (N-53): the fetch runs on the caller's context, so a Ctrl-C
//     in the CLI or a client disconnect in the MCP server aborts an in-flight
//     download instead of only being bounded by the client timeout.
//   - DNS-rebinding TOCTOU (N-55): the host guard resolves the hostname to the
//     validated IP set and the transport dials exactly those IPs via a pinned
//     DialContext, so a host whose DNS flips between public and private between
//     the check and the connect cannot reach an internal address.
package specsource

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultTimeout caps a remote spec/config fetch, including DNS resolution.
	DefaultTimeout = 30 * time.Second
	// DefaultMaxBytes caps a remote spec/config response body (10 MiB).
	DefaultMaxBytes = 10 << 20
)

// ErrNotASourceRef signals that a string is not a file path or URL, so the
// caller can treat it as inline spec/config content.
var ErrNotASourceRef = errors.New("not a source reference")

// Auth describes opt-in authentication for a remote spec fetch. Credential
// fields name environment variables; values are read at request time and never
// logged.
type Auth struct {
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

// Options controls reference resolution and remote fetches.
type Options struct {
	// AllowHTTP permits http:// URLs (opt-in; https is the default).
	AllowHTTP bool
	// AllowPrivate relaxes the SSRF guard for the INITIAL host only (never for
	// redirect targets). Set from EIDOS_SPEC_ALLOW_PRIVATE=1, an explicit
	// operator escape hatch for local development against a private mock server.
	AllowPrivate bool
	// SkipHostCheck bypasses the SSRF guard entirely. No production caller sets
	// it; it exists solely to make FetchURL testable against httptest (127.0.0.1,
	// which the guard rejects).
	SkipHostCheck bool
	// InlineFallback controls how a string that is neither a URL nor a resolvable
	// file reference is treated. When true (MCP tools), the string is returned as
	// ErrNotASourceRef so the caller treats it as inline content. When false
	// (CLI --spec), the string is read as a literal file path, so a bare
	// filename that does not exist reports a clear file-read error instead of
	// being silently re-parsed as a spec body.
	InlineFallback bool
	// Auth attaches credentials to a remote fetch (CLI-only today).
	Auth *Auth
	// Timeout caps a remote fetch; zero selects DefaultTimeout.
	Timeout time.Duration
	// MaxBytes caps a remote response body; zero selects DefaultMaxBytes.
	MaxBytes int64
}

// withDefaults fills zero-valued tuning fields so callers can pass a partial
// Options struct safely.
func (o Options) withDefaults() Options {
	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}
	if o.MaxBytes <= 0 {
		o.MaxBytes = DefaultMaxBytes
	}
	return o
}

// IsURL reports whether ref names a remote http(s) URL rather than a local file
// path. Non-http(s) schemes (e.g. file://, ftp://) are not treated as remote.
func IsURL(ref string) bool {
	u, err := url.Parse(ref)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return true
	}
	return false
}

// LoadSpec resolves ref to spec bytes: a local file path, a file:// URL, or an
// http(s):// URL. It returns ErrNotASourceRef when the string is not a source
// reference and InlineFallback is set, so the caller can fall back to
// inline-content handling. It returns the HTTP Content-Type (empty for local
// files) so callers can route JSON vs YAML parsing.
func LoadSpec(ctx context.Context, ref string, opts Options) ([]byte, string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, "", ErrNotASourceRef
	}
	opts = opts.withDefaults()

	if u, err := url.Parse(ref); err == nil {
		switch strings.ToLower(u.Scheme) {
		case "http", "https":
			return FetchURL(ctx, ref, opts)
		case "file":
			return readSpecFile(fileURLPath(u), opts.MaxBytes)
		}
		// Other schemes (ftp://, etc.) are not supported; fall through to the
		// path check, then inline content.
	}

	// Absolute or explicitly relative path references.
	if strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "./") ||
		strings.HasPrefix(ref, "../") || strings.HasPrefix(ref, "~/") {
		return readSpecFile(expandHome(ref), opts.MaxBytes)
	}

	// A bare relative filename that resolves to an existing file (e.g.
	// "api.yaml") is treated as a file reference. A string that is not an
	// existing file falls through to inline-content handling, so a malformed
	// inline spec is reported as an OpenAPI parse error rather than a missing
	// file.
	if info, err := os.Stat(ref); err == nil && !info.IsDir() {
		return readSpecFile(ref, opts.MaxBytes)
	}

	if !opts.InlineFallback {
		// CLI-style: a bare filename that does not exist is still a required
		// file path; report the read error instead of re-parsing it as inline.
		return readSpecFile(ref, opts.MaxBytes)
	}
	return nil, "", ErrNotASourceRef
}

// LoadConfig resolves ref to generator.yaml bytes from a local file path or a
// file:// URL. Unlike spec references, remote http(s):// config URLs are not
// resolved: a generator.yaml is small and local, and accepting remote URLs
// would widen the SSRF surface for no real benefit — pass inline content or a
// local path instead. It returns ErrNotASourceRef for strings that are not file
// references, so the caller can fall back to inline-content handling.
func LoadConfig(ctx context.Context, ref string, opts Options) ([]byte, error) {
	// Honor cancellation before the file read (a local file read is not
	// itself cancellable): a client disconnect aborts the load instead of
	// doing the I/O into a dead connection (N-54).
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, ErrNotASourceRef
	}

	if u, err := url.Parse(ref); err == nil && strings.EqualFold(u.Scheme, "file") {
		return readConfigFile(fileURLPath(u))
	}

	// Absolute or explicitly relative path references.
	if strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "./") ||
		strings.HasPrefix(ref, "../") || strings.HasPrefix(ref, "~/") {
		return readConfigFile(expandHome(ref))
	}

	// A bare relative filename that resolves to an existing file (e.g.
	// "generator.yaml") is treated as a config file reference.
	if info, err := os.Stat(ref); err == nil && !info.IsDir() {
		return readConfigFile(ref)
	}

	if !opts.InlineFallback {
		return readConfigFile(ref)
	}
	return nil, ErrNotASourceRef
}

// readSpecFile reads a local spec file, expanding a leading ~ to the user's
// home directory. maxBytes caps the file size so an accidental giant path (or a
// spec that has grown far beyond practical size) fails with a clear error
// instead of allocating unbounded memory; remote responses get the same cap via
// io.LimitReader in FetchURL, so the local and remote paths agree (N-58).
func readSpecFile(path string, maxBytes int64) ([]byte, string, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, "", fmt.Errorf("failed to read spec file %q: %w", path, err)
	}
	if int64(len(data)) > maxBytes {
		return nil, "", fmt.Errorf("spec file %q exceeds the %d-byte maximum; use a smaller spec or split it into multiple files", path, maxBytes)
	}
	return data, "", nil
}

// readConfigFile reads a local generator.yaml file, expanding a leading ~ to
// the user's home directory.
func readConfigFile(path string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %q: %w", path, err)
	}
	return data, nil
}

// expandHome replaces a leading ~/ with the user's home directory.
func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// fileURLPath returns the local filesystem path for a file:// URL.
func fileURLPath(u *url.URL) string {
	return expandHome(u.Path)
}

// RedactURL strips any userinfo from a URL for inclusion in error text (the URL
// is otherwise safe to log; query parameters are preserved for diagnosability).
func RedactURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.User = nil
	return u.String()
}

// isPrivateOrLocalIP reports whether ip is a private, loopback, or link-local
// address — the families most commonly targeted by SSRF attacks.
func isPrivateOrLocalIP(ip net.IP) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}
