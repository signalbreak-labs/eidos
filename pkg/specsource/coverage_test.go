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
	"testing"
)

// TestFetchURL_ParseError drives the url.Parse failure branch of FetchURL.
func TestFetchURL_ParseError(t *testing.T) {
	_, _, err := FetchURL(context.Background(), "://bad", Options{})
	if err == nil || !strings.Contains(err.Error(), "failed to parse spec URL") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

// TestFetchURL_AuthError drives the ApplyAuth failure branch of FetchURL: an
// auth scheme whose required env var is unset fails loud before the request.
func TestFetchURL_AuthError(t *testing.T) {
	t.Setenv("EIDOS_COV_BEARER", "")
	_, _, err := FetchURL(context.Background(), "https://example.com/api.yaml", Options{
		SkipHostCheck: true,
		Auth:          &Auth{Scheme: "bearer", TokenEnv: "EIDOS_COV_BEARER"},
	})
	if err == nil || !strings.Contains(err.Error(), "requires env var") {
		t.Fatalf("expected auth error, got %v", err)
	}
}

// TestFetchURL_ReadError drives the io.ReadAll failure branch of FetchURL: a
// server that advertises a Content-Length larger than the body it sends makes
// the client read fail with unexpected EOF.
func TestFetchURL_ReadError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("short")) //nolint:errcheck // deliberate truncation
	}))
	defer srv.Close()

	_, _, err := FetchURL(context.Background(), srv.URL, testFetchOptions())
	if err == nil || !strings.Contains(err.Error(), "failed to read spec URL response") {
		t.Fatalf("expected read error, got %v", err)
	}
}

// TestFetchURL_AllowPrivateHostname drives the allowPrivate escape-hatch branch
// of guardHost with a hostname (not a literal IP): the guard records the
// resolved addresses via resolveAndRecord and the fetch proceeds to dial the
// pinned IPs. localhost resolves to 127.0.0.1, which the httptest server
// listens on.
func TestFetchURL_AllowPrivateHostname(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, sampleSpec) //nolint:errcheck // test handler
	}))
	defer srv.Close()

	u := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	if u == srv.URL {
		t.Skip("httptest server not on 127.0.0.1")
	}
	data, _, err := FetchURL(context.Background(), u, Options{AllowHTTP: true, AllowPrivate: true})
	if err != nil {
		t.Fatalf("FetchURL with allowPrivate hostname: %v", err)
	}
	if !strings.Contains(string(data), "T") {
		t.Errorf("unexpected body: %q", data)
	}
}

// TestCheckHost_ResolutionError drives the lookupIP failure branch of guardHost
// via CheckHost: a hostname in the reserved .invalid TLD never resolves.
func TestCheckHost_ResolutionError(t *testing.T) {
	err := CheckHost(context.Background(), &url.URL{Host: "nonexistent.invalid"}, false)
	if err == nil || !strings.Contains(err.Error(), "failed to resolve spec URL host") {
		t.Fatalf("expected resolution error, got %v", err)
	}
}

// TestResolveAndRecord_ResolutionError drives the lookupIP failure branch of
// resolveAndRecord: a non-resolving hostname is swallowed (the fetch itself
// surfaces the failure) and nothing is recorded.
func TestResolveAndRecord_ResolutionError(t *testing.T) {
	p := &pinner{dialer: &net.Dialer{Timeout: DefaultTimeout}}
	p.resolveAndRecord(context.Background(), "nonexistent.invalid")
	p.mu.Lock()
	_, ok := p.ips["nonexistent.invalid"]
	p.mu.Unlock()
	if ok {
		t.Error("non-resolving hostname must not be recorded")
	}
}

// TestDialContext_SplitHostPortError drives the net.SplitHostPort failure branch
// of dialContext: an addr without a port is rejected before any dialing.
func TestDialContext_SplitHostPortError(t *testing.T) {
	p := &pinner{dialer: &net.Dialer{Timeout: DefaultTimeout}}
	_, err := p.dialContext(context.Background(), "tcp", "localhost")
	if err == nil {
		t.Fatal("expected SplitHostPort error for addr without port")
	}
}

// TestDialContext_Success drives the successful-dial branch of dialContext: a
// hostname pinned to a live listener's IP connects through the pin map.
func TestDialContext_Success(t *testing.T) {
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close() //nolint:errcheck // test listener
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		_ = conn.Close() //nolint:errcheck // test listener
	}()

	p := &pinner{dialer: &net.Dialer{Timeout: DefaultTimeout}}
	p.record("localhost", []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}})
	conn, err := p.dialContext(context.Background(), "tcp", "localhost:"+portOf(ln))
	if err != nil {
		t.Fatalf("pinned dial: %v", err)
	}
	_ = conn.Close() //nolint:errcheck // test conn
}

func portOf(ln net.Listener) string {
	return strings.TrimPrefix(ln.Addr().String(), "127.0.0.1:")
}

// TestFetchURL_RedirectUnsupportedScheme drives the checkRedirect scheme-reject
// branch: a redirect to a non-http(s) scheme fails loud.
func TestFetchURL_RedirectUnsupportedScheme(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "ftp://example.com/spec.yaml", http.StatusFound)
	}))
	defer srv.Close()

	_, _, err := FetchURL(context.Background(), srv.URL, testFetchOptions())
	if err == nil || !strings.Contains(err.Error(), "redirect to unsupported scheme") {
		t.Fatalf("expected redirect scheme error, got %v", err)
	}
}

// TestFetchClientCredentialsToken_RequestError drives the NewRequestWithContext
// failure branch of FetchClientCredentialsToken: a malformed token URL that
// passes the non-empty check but fails URL parsing.
func TestFetchClientCredentialsToken_RequestError(t *testing.T) {
	_, err := FetchClientCredentialsToken(context.Background(), "://bad", "id", "sec", 0, 0)
	if err == nil || !strings.Contains(err.Error(), "failed to build token request") {
		t.Fatalf("expected request error, got %v", err)
	}
}

// TestFetchClientCredentialsToken_DoError drives the client.Do failure branch:
// a token URL pointing at a closed port fails to connect.
func TestFetchClientCredentialsToken_DoError(t *testing.T) {
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close() //nolint:errcheck // guarantee connection refused

	_, err = FetchClientCredentialsToken(context.Background(), "http://"+addr+"/token", "id", "sec", 0, 0)
	if err == nil || !strings.Contains(err.Error(), "token request failed") {
		t.Fatalf("expected do error, got %v", err)
	}
}

// TestFetchClientCredentialsToken_Non2xx drives the non-2xx status branch.
func TestFetchClientCredentialsToken_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "denied", http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := FetchClientCredentialsToken(context.Background(), srv.URL, "id", "sec", 0, 0)
	if err == nil || !strings.Contains(err.Error(), "token endpoint returned 401") {
		t.Fatalf("expected 401 error, got %v", err)
	}
}

// TestFetchClientCredentialsToken_ReadError drives the io.ReadAll failure
// branch: a Content-Length larger than the body makes the read fail.
func TestFetchClientCredentialsToken_ReadError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"access_token":"x"}`)) //nolint:errcheck // deliberate truncation
	}))
	defer srv.Close()

	_, err := FetchClientCredentialsToken(context.Background(), srv.URL, "id", "sec", 0, 0)
	if err == nil || !strings.Contains(err.Error(), "failed to read token response") {
		t.Fatalf("expected read error, got %v", err)
	}
}

// TestFetchClientCredentialsToken_SizeCap drives the token size-cap branch.
func TestFetchClientCredentialsToken_SizeCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"access_token":"`+strings.Repeat("x", 100)+`"}`) //nolint:errcheck // test handler
	}))
	defer srv.Close()

	_, err := FetchClientCredentialsToken(context.Background(), srv.URL, "id", "sec", 0, 50)
	if err == nil || !strings.Contains(err.Error(), "exceeds the 50-byte maximum") {
		t.Fatalf("expected size cap error, got %v", err)
	}
}

// TestFetchClientCredentialsToken_NoAccessToken drives the jsonField failure
// branch: a token response without an access_token field.
func TestFetchClientCredentialsToken_NoAccessToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"token_type":"Bearer"}`) //nolint:errcheck // test handler
	}))
	defer srv.Close()

	_, err := FetchClientCredentialsToken(context.Background(), srv.URL, "id", "sec", 0, 0)
	if err == nil || !strings.Contains(err.Error(), "has no access_token") {
		t.Fatalf("expected missing access_token error, got %v", err)
	}
}

// TestFetchClientCredentialsToken_EmptyToken drives the empty access_token
// branch.
func TestFetchClientCredentialsToken_EmptyToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"access_token":""}`) //nolint:errcheck // test handler
	}))
	defer srv.Close()

	_, err := FetchClientCredentialsToken(context.Background(), srv.URL, "id", "sec", 0, 0)
	if err == nil || !strings.Contains(err.Error(), "access_token is empty") {
		t.Fatalf("expected empty token error, got %v", err)
	}
}

// TestJSONField_NotJSON drives the json.Unmarshal failure branch of jsonField.
func TestJSONField_NotJSON(t *testing.T) {
	if _, err := JSONField([]byte("not json"), "access_token"); err == nil {
		t.Fatal("expected error for non-JSON body")
	}
}

// TestIsURL_ParseError drives the url.Parse failure branch of IsURL.
func TestIsURL_ParseError(t *testing.T) {
	if IsURL("://bad") {
		t.Error("IsURL(://bad) = true, want false")
	}
}

// TestLoadSpec_HTTPURL drives the http(s) branch of LoadSpec: a remote URL is
// resolved through FetchURL.
func TestLoadSpec_HTTPURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, sampleSpec) //nolint:errcheck // test handler
	}))
	defer srv.Close()

	data, _, err := LoadSpec(context.Background(), srv.URL, Options{AllowHTTP: true, SkipHostCheck: true})
	if err != nil {
		t.Fatalf("LoadSpec http URL: %v", err)
	}
	if !strings.Contains(string(data), "T") {
		t.Errorf("unexpected body: %q", data)
	}
}

// TestLoadConfig_CanceledContext drives the ctx.Err() branch of LoadConfig: a
// canceled context aborts the load before the file read (N-54).
func TestLoadConfig_CanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := LoadConfig(ctx, "generator.yaml", Options{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

// TestLoadConfig_BareRelativeExistingFile drives the os.Stat-success branch of
// LoadConfig: a bare relative filename that exists is read as a config file.
func TestLoadConfig_BareRelativeExistingFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "generator.yaml"), []byte("provider:\n  name: x\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Chdir(dir)
	data, err := LoadConfig(context.Background(), "generator.yaml", Options{InlineFallback: true})
	if err != nil {
		t.Fatalf("LoadConfig bare relative: %v", err)
	}
	if !strings.Contains(string(data), "name: x") {
		t.Errorf("got config %q", data)
	}
}

// TestLoadConfig_MissingFileNoFallback drives the non-inline-fallback branch of
// LoadConfig: a bare filename that does not exist is still read as a file path
// (CLI-style) and reports the read error.
func TestLoadConfig_MissingFileNoFallback(t *testing.T) {
	_, err := LoadConfig(context.Background(), "nonexistent-generator.yaml", Options{})
	if err == nil || !strings.Contains(err.Error(), "failed to read config file") {
		t.Fatalf("expected file-read error, got %v", err)
	}
}

// TestRedactURL_ParseError drives the url.Parse failure branch of RedactURL: an
// unparseable URL is returned unchanged.
func TestRedactURL_ParseError(t *testing.T) {
	if got := RedactURL("://bad"); got != "://bad" {
		t.Errorf("RedactURL(://bad) = %q, want unchanged", got)
	}
}
