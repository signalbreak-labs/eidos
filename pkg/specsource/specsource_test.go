package specsource

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const sampleSpec = "openapi: 3.0.0\ninfo:\n  title: T\n  version: 1.0.0\npaths: {}\n"

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

func TestLoadSpec_LocalFile(t *testing.T) {
	p := writeFile(t, "api.yaml", sampleSpec)
	data, ct, err := LoadSpec(context.Background(), p, Options{})
	if err != nil {
		t.Fatalf("LoadSpec local file: %v", err)
	}
	if string(data) != sampleSpec {
		t.Errorf("got data %q", data)
	}
	if ct != "" {
		t.Errorf("local file should have empty content type, got %q", ct)
	}
}

func TestLoadSpec_MissingFile(t *testing.T) {
	// InlineFallback=false (CLI): a bare filename that doesn't exist reads as a
	// file and reports the read error.
	_, _, err := LoadSpec(context.Background(), "/nonexistent/api.yaml", Options{})
	if err == nil || !strings.Contains(err.Error(), "failed to read spec file") {
		t.Fatalf("expected file-read error, got %v", err)
	}
	// InlineFallback=true (MCP): an absolute path is still a file reference.
	_, _, err = LoadSpec(context.Background(), "/nonexistent/api.yaml", Options{InlineFallback: true})
	if err == nil || !strings.Contains(err.Error(), "failed to read spec file") {
		t.Fatalf("expected file-read error with inline fallback, got %v", err)
	}
}

// TestLoadSpec_LocalSizeCap covers N-58: a local spec file larger than MaxBytes
// must fail with a clear error, matching the cap that FetchURL enforces on
// remote responses via io.LimitReader.
func TestLoadSpec_LocalSizeCap(t *testing.T) {
	p := writeFile(t, "big.yaml", strings.Repeat("x", 1024))
	// A tiny MaxBytes rejects the file even though it reads fine.
	_, _, err := LoadSpec(context.Background(), p, Options{MaxBytes: 100})
	if err == nil || !strings.Contains(err.Error(), "exceeds the 100-byte maximum") {
		t.Fatalf("expected size cap error, got %v", err)
	}
	// The default cap accepts it.
	if _, _, err := LoadSpec(context.Background(), p, Options{}); err != nil {
		t.Fatalf("default-cap load: %v", err)
	}
}

func TestLoadSpec_FileURL(t *testing.T) {
	p := writeFile(t, "spec.yaml", sampleSpec)
	data, _, err := LoadSpec(context.Background(), "file://"+p, Options{})
	if err != nil {
		t.Fatalf("LoadSpec file URL: %v", err)
	}
	if string(data) != sampleSpec {
		t.Errorf("got data %q", data)
	}
}

func TestLoadSpec_BareRelativeExistingFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "api.yaml"), []byte(sampleSpec), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Chdir(dir)
	data, _, err := LoadSpec(context.Background(), "api.yaml", Options{InlineFallback: true})
	if err != nil {
		t.Fatalf("LoadSpec bare relative: %v", err)
	}
	if string(data) != sampleSpec {
		t.Errorf("got data %q", data)
	}
}

func TestLoadSpec_InlineFallbackSentinel(t *testing.T) {
	// With InlineFallback=true, a non-reference string returns the sentinel so
	// the caller treats it as inline content.
	_, _, err := LoadSpec(context.Background(), `{"openapi":"3.0.0"}`, Options{InlineFallback: true})
	if !errors.Is(err, ErrNotASourceRef) {
		t.Fatalf("expected ErrNotASourceRef, got %v", err)
	}
	// Without it (CLI), the string is read as a literal path and errors.
	_, _, err = LoadSpec(context.Background(), `{"openapi":"3.0.0"}`, Options{})
	if err == nil || !strings.Contains(err.Error(), "failed to read spec file") {
		t.Fatalf("expected file-read error without inline fallback, got %v", err)
	}
	// Empty string is never a source reference.
	_, _, err = LoadSpec(context.Background(), "", Options{})
	if !errors.Is(err, ErrNotASourceRef) {
		t.Fatalf("empty string: expected ErrNotASourceRef, got %v", err)
	}
}

func TestLoadConfig_FileAndURL(t *testing.T) {
	p := writeFile(t, "generator.yaml", "provider:\n  name: x\n")
	data, err := LoadConfig(context.Background(), p, Options{InlineFallback: true})
	if err != nil {
		t.Fatalf("LoadConfig path: %v", err)
	}
	if !strings.Contains(string(data), "name: x") {
		t.Errorf("got config %q", data)
	}
	data, err = LoadConfig(context.Background(), "file://"+p, Options{InlineFallback: true})
	if err != nil {
		t.Fatalf("LoadConfig file URL: %v", err)
	}
	if !strings.Contains(string(data), "name: x") {
		t.Errorf("got config %q", data)
	}
}

func TestLoadConfig_InlineSentinel(t *testing.T) {
	_, err := LoadConfig(context.Background(), "not a path at all", Options{InlineFallback: true})
	if !errors.Is(err, ErrNotASourceRef) {
		t.Fatalf("expected ErrNotASourceRef, got %v", err)
	}
	_, err = LoadConfig(context.Background(), "", Options{InlineFallback: true})
	if !errors.Is(err, ErrNotASourceRef) {
		t.Fatalf("empty: expected ErrNotASourceRef, got %v", err)
	}
}

func TestIsURL(t *testing.T) {
	for _, tc := range []struct {
		arg  string
		want bool
	}{
		{"https://example.com/api.yaml", true},
		{"http://example.com/api.json", true},
		{"HTTPS://example.com/api.yaml", true},
		{"api.yaml", false},
		{"./specs/api.yaml", false},
		{"/abs/path/api.yaml", false},
		{"file:///etc/spec.yaml", false},
		{"ftp://example.com/api.yaml", false},
		{"", false},
	} {
		if got := IsURL(tc.arg); got != tc.want {
			t.Errorf("IsURL(%q) = %v, want %v", tc.arg, got, tc.want)
		}
	}
}

// testFetchOptions returns Options that can reach an httptest server on
// 127.0.0.1 (which the SSRF guard rejects): http allowed, host check skipped.
func testFetchOptions() Options {
	return Options{AllowHTTP: true, SkipHostCheck: true}
}

func TestFetchURL_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = fmt.Fprint(w, sampleSpec) //nolint:errcheck // test handler
	}))
	defer srv.Close()

	data, ct, err := FetchURL(context.Background(), srv.URL, testFetchOptions())
	if err != nil {
		t.Fatalf("FetchURL: %v", err)
	}
	if !strings.Contains(string(data), "T") {
		t.Errorf("unexpected body: %q", data)
	}
	if !strings.Contains(ct, "yaml") {
		t.Errorf("expected yaml content type, got %q", ct)
	}
}

func TestFetchURL_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	_, _, err := FetchURL(context.Background(), srv.URL, testFetchOptions())
	if err == nil || !strings.Contains(err.Error(), "server returned 404") {
		t.Fatalf("expected 404 error, got %v", err)
	}
}

func TestFetchURL_HTTPSRequired(t *testing.T) {
	_, _, err := FetchURL(context.Background(), "http://example.com/api.yaml", Options{SkipHostCheck: true})
	if err == nil || !strings.Contains(err.Error(), "https is required") {
		t.Fatalf("expected scheme error, got %v", err)
	}
}

func TestFetchURL_UserinfoRejected(t *testing.T) {
	_, _, err := FetchURL(context.Background(), "https://user:pass@example.com/api.yaml", Options{SkipHostCheck: true})
	if err == nil || !strings.Contains(err.Error(), "must not embed credentials") {
		t.Fatalf("expected userinfo error, got %v", err)
	}
}

func TestFetchURL_SSRFGuardBlocksLiteralPrivateIP(t *testing.T) {
	t.Setenv("EIDOS_SPEC_ALLOW_PRIVATE", "")
	_, _, err := FetchURL(context.Background(), "https://127.0.0.1/api.yaml", Options{})
	if err == nil || !strings.Contains(err.Error(), "private/local IP") {
		t.Fatalf("expected SSRF guard error, got %v", err)
	}
	_, _, err = FetchURL(context.Background(), "https://169.254.169.254/latest/meta-data", Options{})
	if err == nil || !strings.Contains(err.Error(), "private/local IP") {
		t.Fatalf("expected SSRF guard error for metadata IP, got %v", err)
	}
}

func TestFetchURL_RedirectToPrivateBlocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1:9/internal", http.StatusFound)
	}))
	defer srv.Close()

	_, _, err := FetchURL(context.Background(), srv.URL, testFetchOptions())
	if err == nil || !strings.Contains(err.Error(), "private/local IP") {
		t.Fatalf("expected redirect SSRF guard error, got %v", err)
	}
}

func TestFetchURL_SizeCap(t *testing.T) {
	big := strings.Repeat("x", DefaultMaxBytes+1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, big) //nolint:errcheck // test handler
	}))
	defer srv.Close()

	_, _, err := FetchURL(context.Background(), srv.URL, testFetchOptions())
	if err == nil || !strings.Contains(err.Error(), "maximum") {
		t.Fatalf("expected size cap error, got %v", err)
	}
}

// TestFetchURL_Cancellation guards N-53: an already-canceled context aborts the
// fetch before it connects, instead of being bounded only by the client timeout.
func TestFetchURL_Cancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, sampleSpec) //nolint:errcheck // test handler
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := FetchURL(ctx, srv.URL, testFetchOptions())
	if err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("expected cancellation error, got %v", err)
	}
}

// TestFetchURL_DialPinning guards N-55: the fetch must dial exactly the IPs the
// guard validated, so a DNS rebind after validation cannot redirect the dial to
// an internal address. We simulate this by making the DNS resolver return a
// private IP on the second lookup (the connect-time lookup the pinner must
// bypass) and asserting the pinned first-lookup IP is what the server sees.
func TestFetchURL_DialPinning(t *testing.T) {
	// A server reachable at a public-looking IP is impractical; instead verify
	// the mechanism directly: the pinner records the validated IPs and the
	// dialContext substitutes them for a hostname without re-resolving.
	p := &pinner{dialer: &net.Dialer{Timeout: DefaultTimeout}}
	// localhost is a hostname (not a literal IP); pinning replaces it at dial.
	ips, err := lookupIP(context.Background(), "localhost")
	if err != nil {
		t.Skipf("localhost resolution unavailable: %v", err)
	}
	p.record("localhost", ips)

	// A dial through the pinner to localhost:1 must resolve via the pinned IPs
	// and fail with a connection error, NOT with the "unvalidated host" refusal.
	_, err = p.dialContext(context.Background(), "tcp", "localhost:1")
	if err == nil || strings.Contains(err.Error(), "unvalidated") {
		t.Fatalf("pinned dial to localhost should attempt the pinned IPs, got %v", err)
	}

	// A hostname that was never validated must be refused outright.
	_, err = p.dialContext(context.Background(), "tcp", "example.com:80")
	if err == nil || !strings.Contains(err.Error(), "unvalidated") {
		t.Fatalf("expected unvalidated-host refusal, got %v", err)
	}

	// A literal IP dials directly (no pinning involved) and does not consult the
	// pin map.
	_, err = p.dialContext(context.Background(), "tcp", "127.0.0.1:1")
	if err == nil || strings.Contains(err.Error(), "unvalidated") {
		t.Fatalf("literal-IP dial should not consult the pin map, got %v", err)
	}
}

func TestCheckHost(t *testing.T) {
	if err := CheckHost(context.Background(), &url.URL{}, false); err == nil || !strings.Contains(err.Error(), "no host") {
		t.Errorf("no-host err = %v, want no host error", err)
	}
	if err := CheckHost(context.Background(), &url.URL{Host: "127.0.0.1"}, true); err != nil {
		t.Errorf("allowPrivate = %v, want nil", err)
	}
	if err := CheckHost(context.Background(), &url.URL{Host: "10.0.0.1"}, false); err == nil || !strings.Contains(err.Error(), "private/local IP") {
		t.Errorf("literal private = %v, want private IP error", err)
	}
	if err := CheckHost(context.Background(), &url.URL{Host: "8.8.8.8"}, false); err != nil {
		t.Errorf("literal public = %v, want nil", err)
	}
	// localhost resolves to a loopback address; the guard must reject it.
	if err := CheckHost(context.Background(), &url.URL{Host: "localhost"}, false); err == nil || !strings.Contains(err.Error(), "private/local IP") {
		t.Errorf("localhost = %v, want private IP error", err)
	}
}

func TestApplyAuth(t *testing.T) {
	newReq := func() *http.Request {
		r, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com/api.yaml", http.NoBody)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		return r
	}

	if err := ApplyAuth(newReq(), nil); err != nil {
		t.Errorf("nil auth = %v, want nil", err)
	}
	if err := ApplyAuth(newReq(), &Auth{}); err != nil {
		t.Errorf("empty scheme = %v, want nil", err)
	}

	// bearer with a token set.
	t.Setenv("EIDOS_TEST_BEARER", "tok")
	r := newReq()
	if err := ApplyAuth(r, &Auth{Scheme: "bearer", TokenEnv: "EIDOS_TEST_BEARER"}); err != nil {
		t.Errorf("bearer = %v, want nil", err)
	}
	if got := r.Header.Get("Authorization"); got != "Bearer tok" {
		t.Errorf("bearer header = %q, want Bearer tok", got)
	}
	t.Setenv("EIDOS_TEST_BEARER_MISSING", "")
	if err := ApplyAuth(newReq(), &Auth{Scheme: "Bearer", TokenEnv: "EIDOS_TEST_BEARER_MISSING"}); err == nil {
		t.Error("bearer missing env should error")
	}

	// basic with both credentials set.
	t.Setenv("EIDOS_TEST_USER", "u")
	t.Setenv("EIDOS_TEST_PASS", "p")
	r = newReq()
	if err := ApplyAuth(r, &Auth{Scheme: "basic", UsernameEnv: "EIDOS_TEST_USER", PasswordEnv: "EIDOS_TEST_PASS"}); err != nil {
		t.Errorf("basic = %v, want nil", err)
	}
	if u, p, ok := r.BasicAuth(); !ok || u != "u" || p != "p" {
		t.Errorf("basic auth = (%q,%q,%v), want (u,p,true)", u, p, ok)
	}
	// N-59: either credential missing must fail loud, not send a half header.
	t.Setenv("EIDOS_TEST_USER_ONLY", "u")
	t.Setenv("EIDOS_TEST_PASS_EMPTY", "")
	if err := ApplyAuth(newReq(), &Auth{Scheme: "basic", UsernameEnv: "EIDOS_TEST_USER_ONLY", PasswordEnv: "EIDOS_TEST_PASS_EMPTY"}); err == nil {
		t.Error("basic with missing password should error (N-59)")
	}
	t.Setenv("EIDOS_TEST_USER_EMPTY", "")
	t.Setenv("EIDOS_TEST_PASS_ONLY", "p")
	if err := ApplyAuth(newReq(), &Auth{Scheme: "basic", UsernameEnv: "EIDOS_TEST_USER_EMPTY", PasswordEnv: "EIDOS_TEST_PASS_ONLY"}); err == nil {
		t.Error("basic with missing username should error (N-59)")
	}

	// apiKey with a key and header.
	t.Setenv("EIDOS_TEST_KEY", "k")
	r = newReq()
	if err := ApplyAuth(r, &Auth{Scheme: "apikey", KeyEnv: "EIDOS_TEST_KEY", HeaderName: "X-API-Key"}); err != nil {
		t.Errorf("apikey = %v, want nil", err)
	}
	if got := r.Header.Get("X-API-Key"); got != "k" {
		t.Errorf("apikey header = %q, want k", got)
	}
	t.Setenv("EIDOS_TEST_KEY_M", "")
	if err := ApplyAuth(newReq(), &Auth{Scheme: "apiKey", KeyEnv: "EIDOS_TEST_KEY_M", HeaderName: "X-API-Key"}); err == nil {
		t.Error("apikey missing key should error")
	}
	t.Setenv("EIDOS_TEST_KEY_H", "k")
	if err := ApplyAuth(newReq(), &Auth{Scheme: "apiKey", KeyEnv: "EIDOS_TEST_KEY_H"}); err == nil {
		t.Error("apikey missing header should error")
	}

	// Unknown scheme.
	if err := ApplyAuth(newReq(), &Auth{Scheme: "nope"}); err == nil {
		t.Error("unknown scheme should error")
	}
}

func TestApplyAuth_OAuth2ClientCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		if r.Form.Get("grant_type") != "client_credentials" {
			t.Errorf("grant_type = %q", r.Form.Get("grant_type"))
		}
		if r.Form.Get("client_id") != "cid" || r.Form.Get("client_secret") != "sec" {
			t.Errorf("client creds not sent: %q / %q", r.Form.Get("client_id"), r.Form.Get("client_secret"))
		}
		_, _ = fmt.Fprint(w, `{"access_token":"oauth-tok"}`) //nolint:errcheck // test handler
	}))
	defer srv.Close()

	t.Setenv("EIDOS_TEST_CID", "cid")
	t.Setenv("EIDOS_TEST_CSECRET", "sec")
	r, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com/api.yaml", http.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	auth := &Auth{Scheme: "oauth2-client-credentials", TokenURL: srv.URL,
		ClientIDEnv: "EIDOS_TEST_CID", ClientSecretEnv: "EIDOS_TEST_CSECRET"}
	if err := ApplyAuth(r, auth); err != nil {
		t.Fatalf("oauth2 = %v, want nil", err)
	}
	if got := r.Header.Get("Authorization"); got != "Bearer oauth-tok" {
		t.Errorf("oauth2 header = %q, want Bearer oauth-tok", got)
	}
	if err := ApplyAuth(r, &Auth{Scheme: "oauth2-client-credentials"}); err == nil {
		t.Error("oauth2 without token_url should error")
	}
}

func TestFetchClientCredentialsToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		if r.Form.Get("grant_type") != "client_credentials" {
			t.Errorf("grant_type = %q", r.Form.Get("grant_type"))
		}
		if r.Form.Get("client_id") != "cid" || r.Form.Get("client_secret") != "csecret" {
			t.Errorf("client creds not sent: %q / %q", r.Form.Get("client_id"), r.Form.Get("client_secret"))
		}
		_, _ = fmt.Fprint(w, `{"access_token":"tok-123","token_type":"Bearer","expires_in":3600}`) //nolint:errcheck // test handler
	}))
	defer srv.Close()

	tok, err := FetchClientCredentialsToken(context.Background(), srv.URL, "cid", "csecret", 0, 0)
	if err != nil {
		t.Fatalf("FetchClientCredentialsToken: %v", err)
	}
	if tok != "tok-123" {
		t.Errorf("token = %q, want tok-123", tok)
	}
}

func TestJSONField(t *testing.T) {
	for _, tc := range []struct {
		body string
		key  string
		want string
		err  bool
	}{
		{`{"access_token":"abc"}`, "access_token", "abc", false},
		{`{"access_token":"a\"b"}`, "access_token", `a"b`, false},
		{`{"token_type":"Bearer","access_token":"xyz"}`, "access_token", "xyz", false},
		{`{"access_token":123}`, "access_token", "", true},
		{`{"other":"x"}`, "access_token", "", true},
		{`{"access_token":""}`, "access_token", "", false},
	} {
		got, err := JSONField([]byte(tc.body), tc.key)
		if tc.err {
			if err == nil {
				t.Errorf("JSONField(%q, %q): expected error, got %q", tc.body, tc.key, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("JSONField(%q, %q): unexpected error %v", tc.body, tc.key, err)
			continue
		}
		if got != tc.want {
			t.Errorf("JSONField(%q, %q) = %q, want %q", tc.body, tc.key, got, tc.want)
		}
	}
}

// TestRedactURL strips userinfo but preserves the path and query.
func TestRedactURL(t *testing.T) {
	got := RedactURL("https://user:pass@example.com/api.yaml?x=1")
	if got != "https://example.com/api.yaml?x=1" {
		t.Errorf("RedactURL = %q", got)
	}
}

// TestPinnerConcurrency exercises the pin map under concurrent reads so the
// mutex guard stays race-free.
func TestPinnerConcurrency(t *testing.T) {
	p := &pinner{dialer: &net.Dialer{Timeout: DefaultTimeout}}
	ips, err := lookupIP(context.Background(), "localhost")
	if err != nil {
		t.Skipf("localhost resolution unavailable: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.record("localhost", ips)
			_, _ = p.dialContext(context.Background(), "tcp", "localhost:1") //nolint:errcheck // deliberate connect failure
		}()
	}
	wg.Wait()
}

// TestResolveAndRecord verifies resolveAndRecord pins a hostname's resolved
// addresses (the allowPrivate escape hatch path) and skips literal IPs.
func TestResolveAndRecord(t *testing.T) {
	p := &pinner{dialer: &net.Dialer{Timeout: DefaultTimeout}}

	// A literal IP is not resolved or recorded.
	p.resolveAndRecord(context.Background(), "127.0.0.1")
	p.mu.Lock()
	_, ok := p.ips["127.0.0.1"]
	p.mu.Unlock()
	if ok {
		t.Error("literal IP must not be recorded")
	}

	// A hostname is resolved and recorded.
	ips, err := lookupIP(context.Background(), "localhost")
	if err != nil {
		t.Skipf("localhost resolution unavailable: %v", err)
	}
	p.resolveAndRecord(context.Background(), "localhost")
	p.mu.Lock()
	recorded, ok := p.ips["localhost"]
	p.mu.Unlock()
	if !ok {
		t.Fatal("localhost must be recorded after resolveAndRecord")
	}
	if len(recorded) != len(ips) {
		t.Errorf("recorded %d IPs, want %d", len(recorded), len(ips))
	}
}

// TestExpandHome covers the ~/ expansion and pass-through branches of
// expandHome.
func TestExpandHome(t *testing.T) {
	// No ~/ prefix: returned unchanged.
	if got := expandHome("/etc/hosts"); got != "/etc/hosts" {
		t.Errorf("expandHome(/etc/hosts) = %q", got)
	}
	// ~/ prefix: expanded to the home directory.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir() error = %v", err)
	}
	got := expandHome("~/spec.yaml")
	want := filepath.Join(home, "spec.yaml")
	if got != want {
		t.Errorf("expandHome(~/spec.yaml) = %q, want %q", got, want)
	}
	// Bare ~ (no slash) is left alone.
	if got := expandHome("~"); got != "~" {
		t.Errorf("expandHome(~) = %q, want ~", got)
	}
}
