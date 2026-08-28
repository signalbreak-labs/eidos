package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleSpec = "openapi: 3.0.0\ninfo:\n  title: T\n  version: 1.0.0\npaths: {}\n"

const sampleConfig = "provider:\n  name: petapi\n  version: \"0.1.0\"\n"

// TestNormalizeConfig_InlineContentNotTreatedAsPath ensures a config body (not
// a file path or URL) is returned verbatim as inline content. This is the path
// LLMs took before M-76: passing inline YAML worked, but passing a path silently
// failed to parse.
func TestNormalizeConfig_InlineContentNotTreatedAsPath(t *testing.T) {
	got, err := normalizeConfig(context.Background(), sampleConfig)
	if err != nil {
		t.Fatalf("normalizeConfig inline: %v", err)
	}
	if got != sampleConfig {
		t.Errorf("expected inline config content, got %q", got)
	}
}

// TestNormalizeConfig_EmptyIsNoop ensures an empty config resolves to empty
// (a no-op for mergeConfigIntoSpec).
func TestNormalizeConfig_EmptyIsNoop(t *testing.T) {
	got, err := normalizeConfig(context.Background(), "")
	if err != nil {
		t.Fatalf("normalizeConfig empty: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty config, got %q", got)
	}
}

// TestNormalizeConfig_AbsoluteFilePath verifies an absolute local file path is
// read, so an LLM can pass a config by path the same way it passes a spec.
func TestNormalizeConfig_AbsoluteFilePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "generator.yaml")
	if err := os.WriteFile(path, []byte(sampleConfig), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	got, err := normalizeConfig(context.Background(), path)
	if err != nil {
		t.Fatalf("normalizeConfig file path: %v", err)
	}
	if got != sampleConfig {
		t.Errorf("expected file contents, got %q", got)
	}
}

// TestNormalizeConfig_FileURL verifies a file:// URL is read from disk. This is
// the exact shape the LLM passed to override-preview that previously failed with
// a YAML unmarshal error (M-76).
func TestNormalizeConfig_FileURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "generator.yaml")
	if err := os.WriteFile(path, []byte(sampleConfig), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	got, err := normalizeConfig(context.Background(), "file://"+path)
	if err != nil {
		t.Fatalf("normalizeConfig file URL: %v", err)
	}
	if got != sampleConfig {
		t.Errorf("expected file contents, got %q", got)
	}
}

// TestNormalizeConfig_RelativeFilePath verifies a bare relative filename that
// exists is read as a config file reference.
func TestNormalizeConfig_RelativeFilePath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "generator.yaml"), []byte(sampleConfig), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Chdir(dir)
	got, err := normalizeConfig(context.Background(), "generator.yaml")
	if err != nil {
		t.Fatalf("normalizeConfig relative file: %v", err)
	}
	if got != sampleConfig {
		t.Errorf("expected file contents, got %q", got)
	}
}

// TestNormalizeConfig_MissingFilePathClearError ensures a path that cannot be
// read produces a clear "failed to read config file" error rather than being
// silently treated as inline content (which would then fail YAML parsing far
// from the cause).
func TestNormalizeConfig_MissingFilePathClearError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.yaml")
	_, err := normalizeConfig(context.Background(), missing)
	if err == nil {
		t.Fatal("expected an error for a missing config file path")
	}
	if !strings.Contains(err.Error(), "failed to read config file") {
		t.Errorf("expected a clear file-read error, got %q", err.Error())
	}
}

// TestHandleOverridePreview_ConfigFilePathResolves verifies override-preview
// resolves a file:// config reference (previously it unmarshaled the literal
// "file://..." string as YAML and failed) and reports the override (M-76/M-77).
func TestHandleOverridePreview_ConfigFilePathResolves(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "generator.yaml")
	if err := os.WriteFile(path, []byte(requiresConfigOverride), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	_, out, err := HandleOverridePreview(context.Background(), nil, OverridePreviewArgs{
		Spec:   requiresConfigSpec,
		Config: "file://" + path,
	})
	if err != nil {
		t.Fatalf("override-preview with file:// config: %v", err)
	}
	if len(out.Resources) != 1 {
		t.Errorf("expected the config to promote 1 resource, got %d: %+v", len(out.Resources), out.Resources)
	}
	if len(out.Overrides) != 1 {
		t.Errorf("expected 1 override report, got %d: %+v", len(out.Overrides), out.Overrides)
	}
	for _, d := range out.Diagnostics {
		if strings.Contains(d.Summary, "Could not parse generator.yaml") {
			t.Errorf("config file reference should resolve, not produce a parse diagnostic: %+v", d)
		}
	}
}

// TestHandleOverridePreview_MalformedConfigSurfacesDiagnostic verifies that when
// the config cannot be parsed, override-preview surfaces a warning diagnostic
// instead of silently returning an empty override report (M-77).
func TestHandleOverridePreview_MalformedConfigSurfacesDiagnostic(t *testing.T) {
	_, out, err := HandleOverridePreview(context.Background(), nil, OverridePreviewArgs{
		Spec:   requiresConfigSpec,
		Config: "this: is: not: valid: yaml: mapping: [",
	})
	if err != nil {
		t.Fatalf("override-preview with malformed config: %v", err)
	}
	var found bool
	for _, d := range out.Diagnostics {
		if strings.Contains(d.Summary, "Could not parse generator.yaml") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a 'Could not parse generator.yaml' diagnostic, got: %+v", out.Diagnostics)
	}
}

// TestNormalizeSpec_InlineContentNotTreatedAsPath ensures a string that is a
// spec body (not a file path or URL) is returned verbatim as inline content.
func TestNormalizeSpec_InlineContentNotTreatedAsPath(t *testing.T) {
	got, err := normalizeSpec(context.Background(), sampleSpec, nil)
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
	got, err := normalizeSpec(context.Background(), path, nil)
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
	got, err := normalizeSpec(context.Background(), "api.yaml", nil)
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
	got, err := normalizeSpec(context.Background(), "file://"+path, nil)
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

	got, err := normalizeSpec(context.Background(), srv.URL, nil)
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
	_, err := normalizeSpec(context.Background(), missing, nil)
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

	_, err := normalizeSpec(context.Background(), srv.URL, nil)
	if err == nil {
		t.Fatal("expected http to be rejected without EIDOS_SPEC_ALLOW_HTTP")
	}
	if !strings.Contains(err.Error(), "https is required") {
		t.Errorf("expected an https-required error, got %q", err.Error())
	}
}
