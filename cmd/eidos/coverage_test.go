package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/signalbreak-labs/eidos/pkg/config"
	"github.com/signalbreak-labs/eidos/pkg/specsource"
)

// TestMustMarkFlagRequired covers the registered-flag happy path and the
// unregistered-flag panic.
func TestMustMarkFlagRequired(t *testing.T) {
	cmd := &cobra.Command{Use: "demo"}
	cmd.Flags().String("name", "", "the name")
	mustMarkFlagRequired(cmd, "name") // must not panic

	func() {
		defer func() {
			if recover() == nil {
				t.Error("mustMarkFlagRequired on an unregistered flag should panic")
			}
		}()
		mustMarkFlagRequired(cmd, "missing")
	}()
}

// TestRemoteSpecFlags_Options covers the nil config, no-scheme, config
// inheritance, and flag-overrides-config branches of the flag/config merge.
func TestRemoteSpecFlags_Options(t *testing.T) {
	// No config: flags only, auth populated from the flags.
	f := remoteSpecFlags{allowHTTP: true, authScheme: "bearer", tokenEnv: "T"}
	opts := f.options(nil)
	if !opts.allowHTTP || opts.auth == nil || opts.auth.Scheme != "bearer" || opts.auth.TokenEnv != "T" {
		t.Errorf("nil-config options = %+v", opts)
	}

	// A config without an auth section and no flags yields no auth.
	empty := remoteSpecFlags{}
	if opts := empty.options(&config.Config{}); opts.auth != nil {
		t.Errorf("scheme-less options should leave auth nil, got %+v", opts.auth)
	}

	// Empty flags inherit every field from the config's auth section.
	cfg := &config.Config{Spec: config.SpecConfig{Auth: &config.SpecAuthConfig{
		Scheme:      "basic",
		UsernameEnv: "U",
		PasswordEnv: "P",
		TokenURL:    "https://auth/token",
	}}}
	opts = empty.options(cfg)
	if opts.auth == nil || opts.auth.Scheme != "basic" || opts.auth.UsernameEnv != "U" ||
		opts.auth.PasswordEnv != "P" || opts.auth.TokenURL != "https://auth/token" {
		t.Errorf("inherited options = %+v", opts.auth)
	}

	// Non-empty flags win over the config.
	f = remoteSpecFlags{authScheme: "bearer", tokenEnv: "T"}
	cfg.Spec.Auth.Scheme = "basic"
	opts = f.options(cfg)
	if opts.auth == nil || opts.auth.Scheme != "bearer" || opts.auth.TokenEnv != "T" {
		t.Errorf("flag-override options = %+v", opts.auth)
	}
}

// TestCheckRemoteSpecHost covers the no-host, allowPrivate bypass, literal
// private/public IP, and hostname-resolution branches of the SSRF guard.
func TestCheckRemoteSpecHost(t *testing.T) {
	if err := specsource.CheckHost(context.Background(), &url.URL{}, false); err == nil || !strings.Contains(err.Error(), "no host") {
		t.Errorf("no-host err = %v, want no host error", err)
	}
	if err := specsource.CheckHost(context.Background(), &url.URL{Host: "127.0.0.1"}, true); err != nil {
		t.Errorf("allowPrivate = %v, want nil", err)
	}
	if err := specsource.CheckHost(context.Background(), &url.URL{Host: "10.0.0.1"}, false); err == nil || !strings.Contains(err.Error(), "private/local IP") {
		t.Errorf("literal private = %v, want private IP error", err)
	}
	if err := specsource.CheckHost(context.Background(), &url.URL{Host: "8.8.8.8"}, false); err != nil {
		t.Errorf("literal public = %v, want nil", err)
	}
	// localhost resolves to a loopback address; the guard must reject it.
	if err := specsource.CheckHost(context.Background(), &url.URL{Host: "localhost"}, false); err == nil || !strings.Contains(err.Error(), "private/local IP") {
		t.Errorf("localhost = %v, want private IP error", err)
	}
}

// TestApplySpecAuth covers every scheme branch of the shared ApplyAuth: the nil and
// empty-scheme no-ops, bearer/basic/apiKey success and missing-env failures,
// the apiKey missing-header failure, and the unknown-scheme rejection.
func TestApplySpecAuth(t *testing.T) {
	newReq := func() *http.Request {
		r, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com/api.yaml", http.NoBody)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		return r
	}

	if err := specsource.ApplyAuth(newReq(), nil); err != nil {
		t.Errorf("nil auth = %v, want nil", err)
	}
	if err := specsource.ApplyAuth(newReq(), &specAuth{}); err != nil {
		t.Errorf("empty scheme = %v, want nil", err)
	}

	// bearer with a token set.
	t.Setenv("EIDOS_TEST_BEARER", "tok")
	r := newReq()
	if err := specsource.ApplyAuth(r, &specAuth{Scheme: "bearer", TokenEnv: "EIDOS_TEST_BEARER"}); err != nil {
		t.Errorf("bearer = %v, want nil", err)
	}
	if got := r.Header.Get("Authorization"); got != "Bearer tok" {
		t.Errorf("bearer header = %q, want Bearer tok", got)
	}
	// bearer with a missing env var.
	t.Setenv("EIDOS_TEST_BEARER_MISSING", "")
	if err := specsource.ApplyAuth(newReq(), &specAuth{Scheme: "Bearer", TokenEnv: "EIDOS_TEST_BEARER_MISSING"}); err == nil {
		t.Error("bearer missing env should error")
	}

	// basic with credentials set.
	t.Setenv("EIDOS_TEST_USER", "u")
	t.Setenv("EIDOS_TEST_PASS", "p")
	r = newReq()
	if err := specsource.ApplyAuth(r, &specAuth{Scheme: "basic", UsernameEnv: "EIDOS_TEST_USER", PasswordEnv: "EIDOS_TEST_PASS"}); err != nil {
		t.Errorf("basic = %v, want nil", err)
	}
	if u, p, ok := r.BasicAuth(); !ok || u != "u" || p != "p" {
		t.Errorf("basic auth = (%q,%q,%v), want (u,p,true)", u, p, ok)
	}
	// basic with both env vars empty.
	t.Setenv("EIDOS_TEST_USER_M", "")
	t.Setenv("EIDOS_TEST_PASS_M", "")
	if err := specsource.ApplyAuth(newReq(), &specAuth{Scheme: "basic", UsernameEnv: "EIDOS_TEST_USER_M", PasswordEnv: "EIDOS_TEST_PASS_M"}); err == nil {
		t.Error("basic missing env should error")
	}

	// apiKey with a key and header.
	t.Setenv("EIDOS_TEST_KEY", "k")
	r = newReq()
	if err := specsource.ApplyAuth(r, &specAuth{Scheme: "apikey", KeyEnv: "EIDOS_TEST_KEY", HeaderName: "X-API-Key"}); err != nil {
		t.Errorf("apikey = %v, want nil", err)
	}
	if got := r.Header.Get("X-API-Key"); got != "k" {
		t.Errorf("apikey header = %q, want k", got)
	}
	// apiKey with a missing key.
	t.Setenv("EIDOS_TEST_KEY_M", "")
	if err := specsource.ApplyAuth(newReq(), &specAuth{Scheme: "apiKey", KeyEnv: "EIDOS_TEST_KEY_M", HeaderName: "X-API-Key"}); err == nil {
		t.Error("apikey missing key should error")
	}
	// apiKey with a missing header name.
	t.Setenv("EIDOS_TEST_KEY_H", "k")
	if err := specsource.ApplyAuth(newReq(), &specAuth{Scheme: "apiKey", KeyEnv: "EIDOS_TEST_KEY_H"}); err == nil {
		t.Error("apikey missing header should error")
	}

	// Unknown scheme.
	if err := specsource.ApplyAuth(newReq(), &specAuth{Scheme: "nope"}); err == nil {
		t.Error("unknown scheme should error")
	}
}

// TestApplySpecAuth_OAuth2ClientCredentials drives the oauth2-client-credentials
// branch, including the token fetch and the missing token_url failure.
func TestApplySpecAuth_OAuth2ClientCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		if r.Form.Get("grant_type") != "client_credentials" {
			t.Errorf("grant_type = %q", r.Form.Get("grant_type"))
		}
		_, _ = fmt.Fprint(w, `{"access_token":"oauth-tok"}`) //nolint:errcheck // test handler: response write error is non-actionable
	}))
	defer srv.Close()

	t.Setenv("EIDOS_TEST_CID", "cid")
	t.Setenv("EIDOS_TEST_CSECRET", "sec")
	r, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com/api.yaml", http.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	auth := &specAuth{Scheme: "oauth2-client-credentials", TokenURL: srv.URL,
		ClientIDEnv: "EIDOS_TEST_CID", ClientSecretEnv: "EIDOS_TEST_CSECRET"}
	if err := specsource.ApplyAuth(r, auth); err != nil {
		t.Fatalf("oauth2 = %v, want nil", err)
	}
	if got := r.Header.Get("Authorization"); got != "Bearer oauth-tok" {
		t.Errorf("oauth2 header = %q, want Bearer oauth-tok", got)
	}
	if err := specsource.ApplyAuth(newReqForOAuth(t), &specAuth{Scheme: "oauth2-client-credentials"}); err == nil {
		t.Error("oauth2 without token_url should error")
	}
}

func newReqForOAuth(t *testing.T) *http.Request {
	t.Helper()
	r, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com/api.yaml", http.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	return r
}

// TestNewMCPCmd_RunE_ReturnsOnEOF drives the mcp command's RunE closure by
// feeding the stdio server an immediately-EOF stdin. The server must return
// promptly rather than block forever.
func TestNewMCPCmd_RunE_ReturnsOnEOF(t *testing.T) {
	oldStdin, oldStdout := os.Stdin, os.Stdout
	defer func() { os.Stdin, os.Stdout = oldStdin, oldStdout }()

	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	defer pr.Close() //nolint:errcheck // test cleanup: closing the pipe read end is non-actionable
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	defer devnull.Close() //nolint:errcheck // test cleanup: closing /dev/null is non-actionable

	os.Stdin = pr
	os.Stdout = devnull
	if err := pw.Close(); err != nil {
		t.Fatalf("close stdin pipe writer: %v", err)
	}

	cmd := newMCPCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("mcp RunE did not return after stdin EOF")
	}
}
