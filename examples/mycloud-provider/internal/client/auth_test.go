package client

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

func TestAPIKeyAuth(t *testing.T) {
	t.Run("header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "https://example.test/x", nil)
		if err := APIKeyAuth("secret", "header", "X-API-Key")(req); err != nil {
			t.Fatalf("interceptor: %v", err)
		}
		if got := req.Header.Get("X-API-Key"); got != "secret" {
			t.Fatalf("X-API-Key = %q", got)
		}
	})
	t.Run("query", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "https://example.test/x", nil)
		if err := APIKeyAuth("secret", "query", "api_key")(req); err != nil {
			t.Fatalf("interceptor: %v", err)
		}
		if got := req.URL.Query().Get("api_key"); got != "secret" {
			t.Fatalf("api_key = %q", got)
		}
	})
	t.Run("cookie", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "https://example.test/x", nil)
		if err := APIKeyAuth("secret", "cookie", "session")(req); err != nil {
			t.Fatalf("interceptor: %v", err)
		}
		cookies := req.Cookies()
		if len(cookies) != 1 || cookies[0].Name != "session" || cookies[0].Value != "secret" {
			t.Fatalf("cookies = %+v", cookies)
		}
	})
	t.Run("invalid", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "https://example.test/x", nil)
		if err := APIKeyAuth("secret", "body", "k")(req); err == nil {
			t.Fatal("expected error for unsupported location, got nil")
		}
	})
}

func TestBasicAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://example.test/x", nil)
	if err := BasicAuth("user", "pass")(req); err != nil {
		t.Fatalf("interceptor: %v", err)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:pass"))
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("Authorization = %q, want %q", got, want)
	}
}

func TestBearerAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://example.test/x", nil)
	if err := BearerAuth("abc123")(req); err != nil {
		t.Fatalf("interceptor: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer abc123" {
		t.Fatalf("Authorization = %q", got)
	}
}

func TestParseScopes(t *testing.T) {
	if got := ParseScopes("a b  c"); len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("ParseScopes = %+v", got)
	}
	if got := ParseScopes(""); len(got) != 0 {
		t.Fatalf("ParseScopes(\"\") = %+v, want empty", got)
	}
	if got := ParseScopes("single"); len(got) != 1 || got[0] != "single" {
		t.Fatalf("ParseScopes = %+v", got)
	}
}

// tokenRecorder captures the last token request's form and Basic-auth header.
type tokenRecorder struct {
	form  url.Values
	basic string
}

func newTokenRecorder() *tokenRecorder { return &tokenRecorder{} }

func (tr *tokenRecorder) handler(status int, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		tr.form, _ = url.ParseQuery(string(b))
		tr.basic = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

func applyInterceptor(t *testing.T, ic RequestInterceptor) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "https://example.test/resource", nil)
	if err := ic(req); err != nil {
		t.Fatalf("interceptor: %v", err)
	}
	return req
}

func TestOAuth2ClientCredentials(t *testing.T) {
	var rec tokenRecorder
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		b, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		rec.form, _ = url.ParseQuery(string(b))
		rec.basic = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{\"access_token\":\"tok\",\"token_type\":\"Bearer\",\"expires_in\":3600}"))
	}))
	t.Cleanup(srv.Close)

	ic := OAuth2ClientCredentialsWithHTTPClient(srv.URL, "cid", "sec", []string{"read", "write"}, srv.Client())

	req := applyInterceptor(t, ic)
	if got := req.Header.Get("Authorization"); got != "Bearer tok" {
		t.Fatalf("Authorization = %q, want Bearer tok", got)
	}
	if got := rec.form.Get("grant_type"); got != "client_credentials" {
		t.Fatalf("grant_type = %q", got)
	}
	if got := rec.form.Get("scope"); got != "read write" {
		t.Fatalf("scope = %q, want %q", got, "read write")
	}
	wantBasic := "Basic " + base64.StdEncoding.EncodeToString([]byte("cid:sec"))
	if rec.basic != wantBasic {
		t.Fatalf("Basic auth = %q, want %q", rec.basic, wantBasic)
	}

	// Cached: a second interceptor call within the expiry window must not hit
	// the token endpoint again.
	_ = applyInterceptor(t, ic)
	if calls.Load() != 1 {
		t.Fatalf("token requests = %d, want 1 (cached on second call)", calls.Load())
	}
}

func TestOAuth2Password(t *testing.T) {
	rec := newTokenRecorder()
	srv := httptest.NewServer(rec.handler(http.StatusOK, "{\"access_token\":\"tok\",\"expires_in\":3600}"))
	t.Cleanup(srv.Close)

	ic := OAuth2PasswordWithHTTPClient(srv.URL, "bob", "pw", "cid", "sec", nil, srv.Client())
	req := applyInterceptor(t, ic)
	if got := req.Header.Get("Authorization"); got != "Bearer tok" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := rec.form.Get("grant_type"); got != "password" {
		t.Fatalf("grant_type = %q", got)
	}
	if got := rec.form.Get("username"); got != "bob" {
		t.Fatalf("username = %q", got)
	}
	if got := rec.form.Get("password"); got != "pw" {
		t.Fatalf("password = %q", got)
	}
}

func TestOAuth2AuthorizationCodeRefresh_RotatesToken(t *testing.T) {
	var rec tokenRecorder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		rec.form, _ = url.ParseQuery(string(b))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Rotate the refresh token in the response.
		_, _ = w.Write([]byte("{\"access_token\":\"tok\",\"expires_in\":3600,\"refresh_token\":\"rotated\"}"))
	}))
	t.Cleanup(srv.Close)

	ic := OAuth2AuthorizationCodeRefreshWithHTTPClient(srv.URL, "initial-rt", "cid", "sec", nil, srv.Client())
	_ = applyInterceptor(t, ic)
	if got := rec.form.Get("grant_type"); got != "refresh_token" {
		t.Fatalf("grant_type = %q", got)
	}
	if got := rec.form.Get("refresh_token"); got != "initial-rt" {
		t.Fatalf("refresh_token = %q, want initial-rt", got)
	}
}

func TestOAuth2TokenRequest_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		_ = r.Body.Close()
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	ic := OAuth2ClientCredentialsWithHTTPClient(srv.URL, "cid", "sec", nil, srv.Client())
	req := httptest.NewRequest(http.MethodGet, "https://example.test/x", nil)
	if err := ic(req); err == nil {
		t.Fatal("expected error on 500 token response, got nil")
	}
}

func TestOAuth2TokenResponse_MissingAccessToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		_ = r.Body.Close()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	t.Cleanup(srv.Close)
	ic := OAuth2ClientCredentialsWithHTTPClient(srv.URL, "cid", "sec", nil, srv.Client())
	req := httptest.NewRequest(http.MethodGet, "https://example.test/x", nil)
	if err := ic(req); err == nil {
		t.Fatal("expected error for missing access_token, got nil")
	}
}

func TestOAuth2TokenResponse_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		_ = r.Body.Close()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{not json"))
	}))
	t.Cleanup(srv.Close)
	ic := OAuth2ClientCredentialsWithHTTPClient(srv.URL, "cid", "sec", nil, srv.Client())
	req := httptest.NewRequest(http.MethodGet, "https://example.test/x", nil)
	if err := ic(req); err == nil {
		t.Fatal("expected error for bad JSON, got nil")
	}
}

func TestOpenIDConnect_TokenURLOverride(t *testing.T) {
	rec := newTokenRecorder()
	srv := httptest.NewServer(rec.handler(http.StatusOK, "{\"access_token\":\"oidc-tok\",\"expires_in\":3600}"))
	t.Cleanup(srv.Close)

	// tokenURL set -> no discovery; client_credentials against tokenURL.
	ic := OpenIDConnectWithHTTPClient("https://unused.example.test/discovery", srv.URL, "cid", "sec", nil, srv.Client())
	req := applyInterceptor(t, ic)
	if got := req.Header.Get("Authorization"); got != "Bearer oidc-tok" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := rec.form.Get("grant_type"); got != "client_credentials" {
		t.Fatalf("grant_type = %q", got)
	}
}

func TestOpenIDConnect_Discovery(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		_ = r.Body.Close()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{\"access_token\":\"disc-tok\",\"expires_in\":3600}"))
	}))
	t.Cleanup(tokenSrv.Close)

	discoveryDoc := "{\"token_endpoint\":\"" + tokenSrv.URL + "\"}"
	var discoveryCalls atomic.Int64
	discSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		discoveryCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(discoveryDoc))
	}))
	t.Cleanup(discSrv.Close)

	ic := OpenIDConnectWithHTTPClient(discSrv.URL, "", "cid", "sec", nil, discSrv.Client())
	req := applyInterceptor(t, ic)
	if got := req.Header.Get("Authorization"); got != "Bearer disc-tok" {
		t.Fatalf("Authorization = %q", got)
	}

	// Second call: discovery is cached (resolve runs once), token is cached.
	_ = applyInterceptor(t, ic)
	if discoveryCalls.Load() != 1 {
		t.Fatalf("discovery calls = %d, want 1 (cached)", discoveryCalls.Load())
	}
}

func TestOpenIDConnect_DiscoveryNon200(t *testing.T) {
	discSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(discSrv.Close)
	ic := OpenIDConnectWithHTTPClient(discSrv.URL, "", "cid", "sec", nil, discSrv.Client())
	req := httptest.NewRequest(http.MethodGet, "https://example.test/x", nil)
	if err := ic(req); err == nil {
		t.Fatal("expected discovery error, got nil")
	}
}

func TestOpenIDConnect_DiscoveryMissingEndpoint(t *testing.T) {
	discSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{\"other\":\"value\"}"))
	}))
	t.Cleanup(discSrv.Close)
	ic := OpenIDConnectWithHTTPClient(discSrv.URL, "", "cid", "sec", nil, discSrv.Client())
	req := httptest.NewRequest(http.MethodGet, "https://example.test/x", nil)
	if err := ic(req); err == nil {
		t.Fatal("expected missing token_endpoint error, got nil")
	}
}

func TestOpenIDConnect_NoDiscoveryOrTokenURL(t *testing.T) {
	ic := OpenIDConnectWithHTTPClient("", "", "cid", "sec", nil, nil)
	req := httptest.NewRequest(http.MethodGet, "https://example.test/x", nil)
	if err := ic(req); err == nil {
		t.Fatal("expected error when no discovery or token URL, got nil")
	}
}
