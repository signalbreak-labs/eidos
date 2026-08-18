package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/signalbreak-labs/eidos/pkg/config"
)

func TestGenerateConfigCommand_RegisteredFlags(t *testing.T) {
	cmd := newRootCmd()
	genCmd, _, err := cmd.Find([]string{"generate-config"})
	if err != nil {
		t.Fatalf("failed to find generate-config command: %v", err)
	}
	if genCmd == nil || genCmd.Name() != "generate-config" {
		t.Fatalf("generate-config command not registered")
	}

	flags := []string{"spec", "output", "provider-name", "force",
		"spec-allow-http", "spec-auth-scheme", "spec-token-env", "spec-username-env", "spec-password-env", "spec-key-env", "spec-header-name", "spec-token-url", "spec-client-id-env", "spec-client-secret-env"}
	for _, name := range flags {
		if genCmd.Flags().Lookup(name) == nil {
			t.Errorf("generate-config command missing --%s flag", name)
		}
	}
}

func TestGenerateConfigCommand_RequiredFlags(t *testing.T) {
	t.Run("missing spec flag", func(t *testing.T) {
		cmd, _ := newTestCommand("generate-config")
		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected error for missing required flags")
		}
		if !strings.Contains(err.Error(), "spec") {
			t.Errorf("expected error to mention required flag spec, got %q", err.Error())
		}
	})

	t.Run("missing spec with output provided", func(t *testing.T) {
		cmd, _ := newTestCommand("generate-config", "--output", "/tmp/generator.yaml")
		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected error for missing required spec flag")
		}
		if !strings.Contains(err.Error(), "spec") {
			t.Errorf("expected error to mention required flag spec, got %q", err.Error())
		}
	})
}

func TestGenerateConfigCommand_EmitsYAML(t *testing.T) {
	tmp := t.TempDir()
	spec := filepath.Join(tmp, "api.yaml")
	if err := os.WriteFile(spec, []byte(`openapi: "3.0.0"
info:
  title: Test API
  version: 1.0.0
paths:
  /pets:
    get:
      operationId: listPets
      responses:
        "200":
          description: ok
    post:
      operationId: createPet
      responses:
        "201":
          description: created
`), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	output := filepath.Join(tmp, "generator.yaml")

	cmd, out := newTestCommand("generate-config", "--spec", spec, "--output", output)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("generate-config failed: %v\noutput:\n%s", err, out.String())
	}

	if _, err := os.Stat(output); err != nil {
		t.Fatalf("expected output file %q to exist: %v", output, err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		"provider:",
		"name: generated",
		"version: 0.1.0",
		"spec:",
		"format: openapi3",
		spec,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("expected generated config to contain %q, got:\n%s", want, content)
		}
	}

	var cfg config.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("generated config is not valid YAML: %v\n%s", err, content)
	}
	if cfg.Provider.Name != "generated" {
		t.Errorf("expected provider name generated, got %q", cfg.Provider.Name)
	}
	if cfg.Provider.Version != "0.1.0" {
		t.Errorf("expected provider version 0.1.0, got %q", cfg.Provider.Version)
	}
	if cfg.Spec.Format != "openapi3" {
		t.Errorf("expected spec format openapi3, got %q", cfg.Spec.Format)
	}
	if cfg.Spec.Path != spec {
		t.Errorf("expected spec path %q, got %q", spec, cfg.Spec.Path)
	}

	stdout := out.String()
	if !strings.Contains(stdout, output) {
		t.Errorf("expected stdout to mention output path, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Next: edit") {
		t.Errorf("expected stdout to mention next step, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Specify --output") {
		t.Errorf("expected stdout to mention optional output flag, got:\n%s", stdout)
	}
}

func TestGenerateConfigCommand_DefaultOutput(t *testing.T) {
	// This test verifies the default output path, which is relative to the
	// current working directory. t.Chdir is process-global, so this test must
	// not run in parallel with other tests that depend on the working
	// directory.
	tmp := t.TempDir()
	t.Chdir(tmp)

	spec := filepath.Join(tmp, "api.yaml")
	if err := os.WriteFile(spec, []byte("openapi: 3.0.0\ninfo:\n  title: Test API\n  version: 1.0.0\npaths: {}\n"), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	cmd, out := newTestCommand("generate-config", "--spec", spec)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("generate-config with default output failed: %v\noutput:\n%s", err, out.String())
	}

	output := filepath.Join(tmp, "generator.yaml")
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("expected default output file %q to exist: %v", output, err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("reading default output file: %v", err)
	}
	if !strings.Contains(string(data), "provider:") {
		t.Errorf("expected default output to contain provider section, got:\n%s", string(data))
	}
}

func TestGenerateConfigCommand_ProviderNameFlag(t *testing.T) {
	tmp := t.TempDir()
	spec := filepath.Join(tmp, "api.yaml")
	if err := os.WriteFile(spec, []byte("openapi: 3.0.0\ninfo:\n  title: Test API\n  version: 1.0.0\npaths: {}\n"), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	output := filepath.Join(tmp, "generator.yaml")

	cmd, out := newTestCommand("generate-config", "--spec", spec, "--output", output, "--provider-name", "mycloud")
	if err := cmd.Execute(); err != nil {
		t.Fatalf("generate-config failed: %v\noutput:\n%s", err, out.String())
	}

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "name: mycloud") {
		t.Errorf("expected provider name mycloud, got:\n%s", content)
	}

	var cfg config.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("generated config is not valid YAML: %v\n%s", err, content)
	}
	if cfg.Provider.Name != "mycloud" {
		t.Errorf("expected provider name mycloud, got %q", cfg.Provider.Name)
	}

	stdout := out.String()
	if !strings.Contains(stdout, output) {
		t.Errorf("expected stdout to mention output path, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Next: edit") {
		t.Errorf("expected stdout to mention next step, got:\n%s", stdout)
	}
}

func TestGenerateConfigCommand_RefusesOverwriteWithoutForce(t *testing.T) {
	tmp := t.TempDir()
	spec := filepath.Join(tmp, "api.yaml")
	if err := os.WriteFile(spec, []byte("openapi: 3.0.0\ninfo:\n  title: Test API\n  version: 1.0.0\npaths: {}\n"), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	output := filepath.Join(tmp, "generator.yaml")
	if err := os.WriteFile(output, []byte("existing"), 0o600); err != nil {
		t.Fatalf("write existing output: %v", err)
	}

	cmd, _ := newTestCommand("generate-config", "--spec", spec, "--output", output)
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when overwriting without force")
	}
	if !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Errorf("expected overwrite refusal, got %q", err.Error())
	}
}

func TestGenerateConfigCommand_ForceOverwrite(t *testing.T) {
	tmp := t.TempDir()
	spec := filepath.Join(tmp, "api.yaml")
	if err := os.WriteFile(spec, []byte("openapi: 3.0.0\ninfo:\n  title: Test API\n  version: 1.0.0\npaths: {}\n"), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	output := filepath.Join(tmp, "generator.yaml")
	if err := os.WriteFile(output, []byte("existing"), 0o600); err != nil {
		t.Fatalf("write existing output: %v", err)
	}

	cmd, out := newTestCommand("generate-config", "--spec", spec, "--output", output, "--force")
	if err := cmd.Execute(); err != nil {
		t.Fatalf("generate-config with --force failed: %v\noutput:\n%s", err, out.String())
	}

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}
	if !strings.Contains(string(data), "provider:") {
		t.Errorf("expected output to be overwritten with generated config, got:\n%s", string(data))
	}
}

func TestGenerateConfigCommand_ErrorPaths(t *testing.T) {
	cases := []struct {
		name            string
		specContent     string
		wantErr         bool
		wantErrContains string
		wantFormat      string
	}{
		{
			name:            "empty spec file",
			specContent:     "",
			wantErr:         true,
			wantErrContains: "failed to load spec",
		},
		{
			name:            "unparseable spec",
			specContent:     `{"unclosed": "value"`,
			wantErr:         true,
			wantErrContains: "failed to load spec",
		},
		{
			name: "both openapi and swagger fields",
			specContent: `openapi: 3.0.0
swagger: "2.0"
info:
  title: Test API
  version: 1.0.0
paths: {}
`,
			wantErr:         true,
			wantErrContains: "failed to detect OpenAPI version",
		},
		{
			name: "missing version field",
			specContent: `info:
  title: Test API
  version: 1.0.0
paths: {}
`,
			wantErr:         true,
			wantErrContains: "failed to detect OpenAPI version",
		},
		{
			name: "swagger 2.0 spec",
			specContent: `swagger: "2.0"
info:
  title: Test API
  version: 1.0.0
paths:
  /pets:
    get:
      operationId: listPets
      responses:
        "200":
          description: ok
`,
			wantErr:    false,
			wantFormat: "openapi2",
		},
		{
			name: "openapi 3.1 spec",
			specContent: `openapi: 3.1.0
info:
  title: Test API
  version: 1.0.0
paths:
  /pets:
    get:
      operationId: listPets
      responses:
        "200":
          description: ok
`,
			wantErr:    false,
			wantFormat: "openapi31",
		},
		{
			// N-57: error-severity diagnostics (duplicate operationIds) must
			// withhold the starter config, mirroring the MCP generate-config tool.
			name: "duplicate operationIds refuses write",
			specContent: `openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
paths:
  /pets:
    get:
      operationId: listPets
      responses:
        "200":
          description: ok
  /pets2:
    get:
      operationId: listPets
      responses:
        "200":
          description: ok
`,
			wantErr:         true,
			wantErrContains: "refusing to write starter config",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			spec := filepath.Join(tmp, "api.yaml")
			if err := os.WriteFile(spec, []byte(tc.specContent), 0o600); err != nil {
				t.Fatalf("write spec: %v", err)
			}
			output := filepath.Join(tmp, "generator.yaml")

			cmd, _ := newTestCommand("generate-config", "--spec", spec, "--output", output)
			err := cmd.Execute()
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), tc.wantErrContains) {
					t.Errorf("expected error to contain %q, got %q", tc.wantErrContains, err.Error())
				}
				if _, statErr := os.Stat(output); !os.IsNotExist(statErr) {
					t.Errorf("expected no partial output file on failure, got %v", statErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("generate-config failed: %v", err)
			}
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatalf("reading output file: %v", err)
			}
			content := string(data)
			wantFormat := "format: " + tc.wantFormat
			if !strings.Contains(content, wantFormat) {
				t.Errorf("expected generated config to contain %q, got:\n%s", wantFormat, content)
			}
			var cfg config.Config
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				t.Fatalf("generated config is not valid YAML: %v\n%s", err, content)
			}
			if cfg.Spec.Format != tc.wantFormat {
				t.Errorf("expected cfg.Spec.Format %q, got %q", tc.wantFormat, cfg.Spec.Format)
			}
		})
	}
}
