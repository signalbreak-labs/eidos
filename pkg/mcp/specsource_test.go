package mcp

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleSpec = "openapi: 3.0.0\ninfo:\n  title: T\n  version: 1.0.0\npaths: {}\n"

// TestNormalizeSpec_InlineContentNotTreatedAsPath ensures a string that is a
// spec body (not a file path or URL) is returned verbatim as inline content.
func TestNormalizeSpec_InlineContentNotTreatedAsPath(t *testing.T) {
	got, err := normalizeSpec(sampleSpec)
	if err != nil {
		t.Fatalf("normalizeSpec inline: %v", err)
	}
	if !strings.Contains(string(got), "openapi") {
		t.Errorf("expected inline spec content, got %q", got)
	}
}

// TestNormalizeSpec_AbsoluteFilePath verifies an absolute local file path is
// read like the CLI's --spec accepts.
func TestNormalizeSpec_AbsoluteFilePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.yaml")
	if err := os.WriteFile(path, []byte(sampleSpec), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	got, err := normalizeSpec(path)
	if err != nil {
		t.Fatalf("normalizeSpec file path: %v", err)
	}
	if string(got) != sampleSpec {
		t.Errorf("expected file contents, got %q", got)
	}
}

// TestNormalizeSpec_RelativeFilePath verifies a bare relative filename that
// exists is read as a file reference.
func TestNormalizeSpec_RelativeFilePath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "api.yaml"), []byte(sampleSpec), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	t.Chdir(dir)
	got, err := normalizeSpec("api.yaml")
	if err != nil {
		t.Fatalf("normalizeSpec relative file: %v", err)
	}
	if string(got) != sampleSpec {
		t.Errorf("expected file contents, got %q", got)
	}
}

// TestNormalizeSpec_FileURL verifies a file:// URL is read from disk.
func TestNormalizeSpec_FileURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(path, []byte(sampleSpec), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	got, err := normalizeSpec("file://" + path)
	if err != nil {
		t.Fatalf("normalizeSpec file URL: %v", err)
	}
	if string(got) != sampleSpec {
		t.Errorf("expected file contents, got %q", got)
	}
}

// TestNormalizeSpec_HTTPURL verifies an http(s):// URL is fetched. The SSRF
// guard and https-only policy are relaxed for a localhost httptest server via
// the same env-var escape hatches the CLI exposes.
func TestNormalizeSpec_HTTPURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sampleSpec)) //nolint:errcheck // test fixture
	}))
	t.Cleanup(srv.Close)
	t.Setenv("EIDOS_SPEC_ALLOW_HTTP", "1")
	t.Setenv("EIDOS_SPEC_ALLOW_PRIVATE", "1")

	got, err := normalizeSpec(srv.URL)
	if err != nil {
		t.Fatalf("normalizeSpec http URL: %v", err)
	}
	if string(got) != sampleSpec {
		t.Errorf("expected fetched contents, got %q", got)
	}
}

// TestNormalizeSpec_MissingFilePathClearError ensures a path that cannot be
// read produces a clear "failed to read spec file" error rather than the
// misleading "unknown OpenAPI version" parse error the LLM hit when paths were
// treated as inline content.
func TestNormalizeSpec_MissingFilePathClearError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.yaml")
	_, err := normalizeSpec(missing)
	if err == nil {
		t.Fatal("expected an error for a missing file path")
	}
	if !strings.Contains(err.Error(), "failed to read spec file") {
		t.Errorf("expected a clear file-read error, got %q", err.Error())
	}
}

// TestNormalizeSpec_HTTPSSchemeRejectedByDefault ensures https is required for
// remote specs and http is rejected without the opt-in env var.
func TestNormalizeSpec_HTTPSSchemeRejectedByDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sampleSpec)) //nolint:errcheck // test fixture
	}))
	t.Cleanup(srv.Close)
	// Allow private (localhost) but NOT http, so the scheme check is the gate.
	t.Setenv("EIDOS_SPEC_ALLOW_PRIVATE", "1")
	t.Setenv("EIDOS_SPEC_ALLOW_HTTP", "")

	_, err := normalizeSpec(srv.URL)
	if err == nil {
		t.Fatal("expected http to be rejected without EIDOS_SPEC_ALLOW_HTTP")
	}
	if !strings.Contains(err.Error(), "https is required") {
		t.Errorf("expected an https-required error, got %q", err.Error())
	}
}
