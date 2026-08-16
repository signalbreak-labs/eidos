package mcp

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The MCP tools accept the same `spec` reference shapes the CLI's --spec flag
// accepts (cmd/eidos/remote_spec.go): inline JSON/YAML content, a local file
// path, a file:// URL, or an http(s):// URL. normalizeSpec first tries to load
// the string as a source reference; if it is not one, it falls back to treating
// the string as inline content so existing callers that pass the spec body keep
// working.
//
// Remote http(s) fetches are hardened to mirror the CLI: https-only by default
// (http is opt-in via EIDOS_SPEC_ALLOW_HTTP=1), an SSRF guard that rejects
// private/loopback/link-local hosts (relaxed for the initial host only via
// EIDOS_SPEC_ALLOW_PRIVATE=1, never on redirect targets), a 30s timeout, and a
// 10 MiB response cap. Credentials are never accepted via the spec reference;
// pass inline content or a local file instead.

const (
	mcpSpecTimeout  = 30 * time.Second
	mcpSpecMaxBytes = 10 << 20 // 10 MiB; matches maxSpecSize
)

// errNotASourceRef signals that a string is not a file path or URL and should be
// treated as inline spec content by the caller.
var errNotASourceRef = fmt.Errorf("not a source reference")

// loadSpecRef loads spec bytes from a local file path, a file:// URL, or an
// http(s):// URL. It returns errNotASourceRef when the string is not a source
// reference so the caller can fall back to inline-content handling.
func loadSpecRef(ref string) ([]byte, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, errNotASourceRef
	}

	if u, err := url.Parse(ref); err == nil {
		switch strings.ToLower(u.Scheme) {
		case "http", "https":
			return fetchSpecURL(ref)
		case "file":
			return readSpecFile(fileURLPath(u))
		}
		// Other schemes (ftp://, etc.) are not supported; fall through to the
		// path check, then inline content.
	}

	// Absolute or explicitly relative path references.
	if strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "./") ||
		strings.HasPrefix(ref, "../") || strings.HasPrefix(ref, "~/") {
		return readSpecFile(expandHome(ref))
	}

	// A bare relative filename that resolves to an existing file (e.g.
	// "api.yaml") is treated as a file reference. A string that is not an
	// existing file falls through to inline-content handling, so a malformed
	// inline spec is reported as an OpenAPI parse error rather than a missing
	// file.
	if info, err := os.Stat(ref); err == nil && !info.IsDir() {
		return readSpecFile(ref)
	}

	return nil, errNotASourceRef
}

// readSpecFile reads a local spec file, expanding a leading ~ to the user's
// home directory.
func readSpecFile(path string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("failed to read spec file %q: %w", path, err)
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

// fetchSpecURL downloads a remote spec URL through a hardened HTTP client.
func fetchSpecURL(rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse spec URL: %w", err)
	}
	if u.User != nil {
		return nil, fmt.Errorf("spec URL must not embed credentials; pass inline spec content or a local file path instead")
	}
	allowHTTP := os.Getenv("EIDOS_SPEC_ALLOW_HTTP") == "1"
	scheme := strings.ToLower(u.Scheme)
	if allowed := scheme == "https" || (scheme == "http" && allowHTTP); !allowed {
		return nil, fmt.Errorf("spec URL scheme %q is not allowed: https is required for remote specs (set EIDOS_SPEC_ALLOW_HTTP=1 to permit http, or download the spec and pass a local path or inline content)", u.Scheme)
	}
	if err := checkSpecHost(u, os.Getenv("EIDOS_SPEC_ALLOW_PRIVATE") == "1"); err != nil {
		return nil, err
	}

	client := &http.Client{
		Timeout: mcpSpecTimeout,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			return checkSpecRedirect(req.URL, allowHTTP)
		},
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to build spec request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch spec URL: %w (download the spec and pass a local path or inline content)", err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // best-effort response body close

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("failed to fetch spec URL: server returned %s; download the spec and pass a local path or inline content", resp.Status)
	}

	// Read one byte past the limit so truncation is reported, not silently used.
	data, err := io.ReadAll(io.LimitReader(resp.Body, mcpSpecMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read spec URL response: %w", err)
	}
	if int64(len(data)) > mcpSpecMaxBytes {
		return nil, fmt.Errorf("spec URL response exceeds the %d-byte maximum; download the spec and pass a local path or inline content", mcpSpecMaxBytes)
	}
	return data, nil
}

// checkSpecHost rejects a spec URL whose host is a literal private/loopback/
// link-local IP or resolves to one. allowPrivate relaxes the guard for the
// initial host only (an operator escape hatch for local mock servers); redirect
// targets are never exempted.
func checkSpecHost(u *url.URL, allowPrivate bool) error {
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("spec URL has no host")
	}
	if allowPrivate {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateOrLocalIP(ip) {
			return fmt.Errorf("spec URL host %q is a private/local IP %s, which is blocked (SSRF guard); set EIDOS_SPEC_ALLOW_PRIVATE=1 for a local mock server, or download the spec and pass a local path", host, ip)
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), mcpSpecTimeout)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("failed to resolve spec URL host %q: %w", host, err)
	}
	for _, ip := range ips {
		if isPrivateOrLocalIP(ip.IP) {
			return fmt.Errorf("spec URL host %q resolves to private/local IP %s, which is blocked (SSRF guard); set EIDOS_SPEC_ALLOW_PRIVATE=1 for a local mock server, or download the spec and pass a local path", host, ip.IP)
		}
	}
	return nil
}

// checkSpecRedirect enforces the scheme and private-IP policy on redirect
// targets. Redirect targets are never exempted from the private-IP guard.
func checkSpecRedirect(u *url.URL, allowHTTP bool) error {
	scheme := strings.ToLower(u.Scheme)
	if allowed := scheme == "https" || (scheme == "http" && allowHTTP); !allowed {
		return fmt.Errorf("redirect to unsupported scheme %q (https required)", u.Scheme)
	}
	return checkSpecHost(u, false)
}

// isPrivateOrLocalIP reports whether ip is private, loopback, or link-local —
// the families most commonly targeted by SSRF attacks.
func isPrivateOrLocalIP(ip net.IP) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}
