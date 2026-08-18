package specsource

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// FetchURL downloads a remote spec URL through a hardened HTTP client: an
// https-only scheme allowlist (http is opt-in via Options.AllowHTTP), an SSRF
// guard that rejects private/loopback/link-local hosts, a timeout, and a
// response size cap. Credentials are never accepted via the URL (userinfo is
// rejected); use Options.Auth.
//
// The request runs on ctx so cancellation propagates (N-53), and the transport
// dials the exact IP addresses the guard validated rather than re-resolving the
// hostname at connect time, closing the DNS-rebinding TOCTOU (N-55).
func FetchURL(ctx context.Context, rawURL string, opts Options) ([]byte, string, error) {
	opts = opts.withDefaults()
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse spec URL: %w", err)
	}
	if u.User != nil {
		return nil, "", fmt.Errorf("spec URL must not embed credentials; pass inline spec content or a local file path instead")
	}
	scheme := strings.ToLower(u.Scheme)
	if allowed := scheme == "https" || (scheme == "http" && opts.AllowHTTP); !allowed {
		return nil, "", fmt.Errorf("spec URL scheme %q is not allowed: https is required for remote specs (set EIDOS_SPEC_ALLOW_HTTP=1 to permit http, or download the spec and pass a local path or inline content)", u.Scheme)
	}

	pin := &pinner{dialer: &net.Dialer{Timeout: opts.Timeout}}
	if !opts.SkipHostCheck {
		if err := guardHost(ctx, u, opts.AllowPrivate, pin); err != nil {
			return nil, "", err
		}
	}

	client := &http.Client{
		Timeout: opts.Timeout,
		Transport: &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			DialContext:         pin.dialContext,
			TLSHandshakeTimeout: opts.Timeout,
			MaxIdleConns:        1,
			DisableKeepAlives:   true, // one-shot fetch; no connection reuse
		},
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			return pin.checkRedirect(req, opts)
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, "", fmt.Errorf("failed to build spec request: %w", err)
	}
	if err := ApplyAuth(req, opts.Auth); err != nil {
		return nil, "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to fetch spec URL: %w (download the spec and pass a local path or inline content)", err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // best-effort response body close

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("failed to fetch spec URL: server returned %s; download the spec and pass a local path or inline content", resp.Status)
	}

	// Read one byte past the limit so truncation is reported, not silently used.
	data, err := io.ReadAll(io.LimitReader(resp.Body, opts.MaxBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("failed to read spec URL response: %w", err)
	}
	if int64(len(data)) > opts.MaxBytes {
		return nil, "", fmt.Errorf("spec URL response exceeds the %d-byte maximum; download the spec and pass a local path or inline content", opts.MaxBytes)
	}
	return data, resp.Header.Get("Content-Type"), nil
}

// CheckHost validates u's host against the SSRF policy: a literal private/
// loopback/link-local IP is rejected, and a hostname that resolves to any such
// address is rejected. allowPrivate relaxes the guard (operator escape hatch
// for local mock servers). It is the standalone form of the guard used by
// FetchURL, exported so tests can exercise the policy without a fetch. The
// connection-pinning variant is internal to FetchURL.
func CheckHost(ctx context.Context, u *url.URL, allowPrivate bool) error {
	return guardHost(ctx, u, allowPrivate, nil)
}

// guardHost enforces the scheme-independent SSRF policy on u. When pin is
// non-nil, it records the validated IP addresses for the hostname so the
// transport can dial exactly those (N-55). Redirect targets are never exempted
// (allowPrivate=false) — a 302 is the classic SSRF vector.
func guardHost(ctx context.Context, u *url.URL, allowPrivate bool, pin *pinner) error {
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("spec URL has no host")
	}
	if allowPrivate {
		// Escape hatch: no rejection, but still record the resolved IPs so the
		// connection goes to exactly the addresses seen here (defense in depth).
		if pin != nil {
			pin.resolveAndRecord(ctx, host)
		}
		return nil
	}
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateOrLocalIP(ip) {
			return fmt.Errorf("spec URL host %q is a private/local IP %s, which is blocked (SSRF guard); set EIDOS_SPEC_ALLOW_PRIVATE=1 for a local mock server, or download the spec and pass a local path", host, ip)
		}
		return nil
	}
	ips, err := lookupIP(ctx, host)
	if err != nil {
		return fmt.Errorf("failed to resolve spec URL host %q: %w", host, err)
	}
	for _, ip := range ips {
		if isPrivateOrLocalIP(ip.IP) {
			return fmt.Errorf("spec URL host %q resolves to private/local IP %s, which is blocked (SSRF guard); set EIDOS_SPEC_ALLOW_PRIVATE=1 for a local mock server, or download the spec and pass a local path", host, ip.IP)
		}
	}
	if pin != nil {
		pin.record(host, ips)
	}
	return nil
}

// lookupIP resolves host, honoring the caller's cancellation context.
func lookupIP(ctx context.Context, host string) ([]net.IPAddr, error) {
	return net.DefaultResolver.LookupIPAddr(ctx, host)
}

// pinner validates a hostname once and pins the connection to exactly the IPs
// the guard saw, so a DNS rebind after validation cannot redirect the dial to an
// internal address (N-55). The pin map is keyed by lowercased hostname; literal
// IPs need no pinning and are dialed directly.
type pinner struct {
	mu     sync.Mutex
	ips    map[string][]net.IP // validated hostname → IP set
	dialer *net.Dialer
}

// resolveAndRecord resolves hostname and records the result regardless of
// private/loopback status — used by the allowPrivate escape hatch, which skips
// rejection but still pins the resolved addresses.
func (p *pinner) resolveAndRecord(ctx context.Context, host string) {
	if net.ParseIP(host) != nil {
		return // literal IP: nothing to pin
	}
	ips, err := lookupIP(ctx, host)
	if err != nil {
		return // resolution failure is surfaced by the fetch itself
	}
	p.record(host, ips)
}

func (p *pinner) record(host string, ips []net.IPAddr) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ips == nil {
		p.ips = make(map[string][]net.IP)
	}
	key := strings.ToLower(host)
	// Record a fresh slice rather than appending into a previously-exposed
	// backing array: dialContext ranges the recorded slice outside the lock, so
	// the underlying array must never be mutated after it is published.
	rec := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		rec = append(rec, ip.IP)
	}
	p.ips[key] = rec
}

// dialContext dials addr, substituting a pinned validated IP for a hostname so
// no fresh DNS resolution happens at connect time. A hostname that was never
// validated is refused outright rather than dialed.
func (p *pinner) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if net.ParseIP(host) != nil {
		// Literal IP — no DNS involved, no TOCTOU to close.
		return p.dialer.DialContext(ctx, network, addr)
	}
	p.mu.Lock()
	ips, ok := p.ips[strings.ToLower(host)]
	p.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("refusing to dial unvalidated spec host %q (SSRF guard)", host)
	}
	var lastErr error
	for _, ip := range ips {
		conn, err := p.dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// checkRedirect enforces the scheme allowlist and SSRF policy on HTTP redirect
// targets. Redirect targets are never exempted from the private-IP guard, and
// the redirect host is pinned the same way the initial host is.
func (p *pinner) checkRedirect(req *http.Request, opts Options) error {
	scheme := strings.ToLower(req.URL.Scheme)
	if allowed := scheme == "https" || (scheme == "http" && opts.AllowHTTP); !allowed {
		return fmt.Errorf("redirect to unsupported scheme %q (https required)", req.URL.Scheme)
	}
	return guardHost(req.Context(), req.URL, false, p)
}
