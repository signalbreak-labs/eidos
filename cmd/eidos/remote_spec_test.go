package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/specsource"
)

func TestIsRemoteSpecURL(t *testing.T) {
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
		if got := isRemoteSpecURL(tc.arg); got != tc.want {
			t.Errorf("isRemoteSpecURL(%q) = %v, want %v", tc.arg, got, tc.want)
		}
	}
}

func TestLoadSpecBytes_LocalFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "api.yaml")
	if err := os.WriteFile(path, []byte("openapi: 3.0.0"), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	data, ct, err := loadSpecBytes(context.Background(), path, remoteSpecOptions{})
	if err != nil {
		t.Fatalf("loadSpecBytes local file: %v", err)
	}
	if string(data) != "openapi: 3.0.0" {
		t.Errorf("got data %q", data)
	}
	if ct != "" {
		t.Errorf("local file should have empty content type, got %q", ct)
	}
}

func TestLoadSpecBytes_MissingLocalFile(t *testing.T) {
	_, _, err := loadSpecBytes(context.Background(), "/nonexistent/api.yaml", remoteSpecOptions{})
	if err == nil || !strings.Contains(err.Error(), "failed to read spec") {
		t.Fatalf("expected file-read error, got %v", err)
	}
}

func TestFetchRemoteSpec_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = fmt.Fprint(w, "openapi: 3.0.0\ninfo:\n  title: Remote\n  version: 1.0.0\npaths: {}\n") //nolint:errcheck // test handler: response write error is non-actionable
	}))
	defer srv.Close()

	data, ct, err := fetchRemoteSpec(context.Background(), srv.URL, remoteSpecOptions{allowHTTP: true, skipHostCheck: true})
	if err != nil {
		t.Fatalf("fetchRemoteSpec: %v", err)
	}
	if !strings.Contains(string(data), "Remote") {
		t.Errorf("unexpected body: %q", data)
	}
	if !strings.Contains(ct, "yaml") {
		t.Errorf("expected yaml content type, got %q", ct)
	}
}

func TestFetchRemoteSpec_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	_, _, err := fetchRemoteSpec(context.Background(), srv.URL, remoteSpecOptions{allowHTTP: true, skipHostCheck: true})
	if err == nil || !strings.Contains(err.Error(), "server returned 404") {
		t.Fatalf("expected 404 error, got %v", err)
	}
}

func TestFetchRemoteSpec_HTTPSRequired(t *testing.T) {
	// http without --spec-allow-http is rejected before any connection.
	_, _, err := fetchRemoteSpec(context.Background(), "http://example.com/api.yaml", remoteSpecOptions{skipHostCheck: true})
	if err == nil || !strings.Contains(err.Error(), "scheme") {
		t.Fatalf("expected scheme error, got %v", err)
	}
}

func TestFetchRemoteSpec_UserinfoRejected(t *testing.T) {
	_, _, err := fetchRemoteSpec(context.Background(), "https://user:pass@example.com/api.yaml", remoteSpecOptions{skipHostCheck: true})
	if err == nil || !strings.Contains(err.Error(), "must not embed credentials") {
		t.Fatalf("expected userinfo error, got %v", err)
	}
}

func TestFetchRemoteSpec_SSRFGuardBlocksPrivateIP(t *testing.T) {
	// Pin the local-dev escape hatch off so a developer running with
	// EIDOS_SPEC_ALLOW_PRIVATE=1 in their shell does not bypass the guard here.
	t.Setenv("EIDOS_SPEC_ALLOW_PRIVATE", "")
	// A literal private IP is rejected before any connection is attempted.
	_, _, err := fetchRemoteSpec(context.Background(), "https://127.0.0.1/api.yaml", remoteSpecOptions{})
	if err == nil || !strings.Contains(err.Error(), "private/local IP") {
		t.Fatalf("expected SSRF guard error, got %v", err)
	}
	_, _, err = fetchRemoteSpec(context.Background(), "https://169.254.169.254/latest/meta-data", remoteSpecOptions{})
	if err == nil || !strings.Contains(err.Error(), "private/local IP") {
		t.Fatalf("expected SSRF guard error for metadata IP, got %v", err)
	}
}

func TestFetchRemoteSpec_RedirectToPrivateIPBlocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Even with the initial host check bypassed, a redirect to a private
		// address must be blocked.
		http.Redirect(w, r, "http://127.0.0.1:9/internal", http.StatusFound)
	}))
	defer srv.Close()

	_, _, err := fetchRemoteSpec(context.Background(), srv.URL, remoteSpecOptions{allowHTTP: true, skipHostCheck: true})
	if err == nil || !strings.Contains(err.Error(), "private/local IP") {
		t.Fatalf("expected redirect SSRF guard error, got %v", err)
	}
}

func TestFetchRemoteSpec_SizeCap(t *testing.T) {
	big := strings.Repeat("x", specsource.DefaultMaxBytes+1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, big) //nolint:errcheck // test handler: response write error is non-actionable
	}))
	defer srv.Close()

	_, _, err := fetchRemoteSpec(context.Background(), srv.URL, remoteSpecOptions{allowHTTP: true, skipHostCheck: true})
	if err == nil || !strings.Contains(err.Error(), "maximum") {
		t.Fatalf("expected size cap error, got %v", err)
	}
}

func TestFetchRemoteSpec_BearerAuth(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = fmt.Fprint(w, "openapi: 3.0.0\npaths: {}\n") //nolint:errcheck // test handler: response write error is non-actionable
	}))
	defer srv.Close()

	t.Setenv("EIDOS_TEST_SPEC_TOKEN", "s3cret-token")
	opts := remoteSpecOptions{
		allowHTTP:     true,
		skipHostCheck: true,
		auth:          &specAuth{Scheme: "bearer", TokenEnv: "EIDOS_TEST_SPEC_TOKEN"},
	}
	if _, _, err := fetchRemoteSpec(context.Background(), srv.URL, opts); err != nil {
		t.Fatalf("fetchRemoteSpec: %v", err)
	}
	if gotAuth != "Bearer s3cret-token" {
		t.Errorf("Authorization = %q, want Bearer s3cret-token", gotAuth)
	}
}

func TestFetchRemoteSpec_BearerAuthMissingEnv(t *testing.T) {
	// Ensure the env var is absent (an empty value reads back as "" too).
	t.Setenv("EIDOS_TEST_SPEC_TOKEN_ABSENT", "")
	opts := remoteSpecOptions{allowHTTP: true, skipHostCheck: true,
		auth: &specAuth{Scheme: "bearer", TokenEnv: "EIDOS_TEST_SPEC_TOKEN_ABSENT"}}
	_, _, err := fetchRemoteSpec(context.Background(), "https://example.com/api.yaml", opts)
	if err == nil || !strings.Contains(err.Error(), "EIDOS_TEST_SPEC_TOKEN_ABSENT") {
		t.Fatalf("expected missing-env error, got %v", err)
	}
}

// TestFetchClientCredentialsToken exercises the shared OAuth2 token flow that
// the CLI's spec-auth oauth2-client-credentials scheme drives.
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
		_, _ = fmt.Fprint(w, `{"access_token":"tok-123","token_type":"Bearer","expires_in":3600}`) //nolint:errcheck // test handler: response write error is non-actionable
	}))
	defer srv.Close()

	tok, err := specsource.FetchClientCredentialsToken(context.Background(), srv.URL, "cid", "csecret", 0, 0)
	if err != nil {
		t.Fatalf("fetchClientCredentialsToken: %v", err)
	}
	if tok != "tok-123" {
		t.Errorf("token = %q, want tok-123", tok)
	}
}

// TestJsonField exercises the shared token-response field extractor.
func TestJsonField(t *testing.T) {
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
		got, err := specsource.JSONField([]byte(tc.body), tc.key)
		if tc.err {
			if err == nil {
				t.Errorf("jsonField(%q, %q): expected error, got %q", tc.body, tc.key, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("jsonField(%q, %q): unexpected error %v", tc.body, tc.key, err)
			continue
		}
		if got != tc.want {
			t.Errorf("jsonField(%q, %q) = %q, want %q", tc.body, tc.key, got, tc.want)
		}
	}
}
