package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func TestGenerateCommand_NoDryRun(t *testing.T) {
	tmp := t.TempDir()
	spec := filepath.Join(tmp, "api.yaml")
	if err := os.WriteFile(spec, []byte("openapi: 3.0.0\ninfo:\n  title: Test API\n  version: 1.0.0\npaths: {}\n"), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	outDir := filepath.Join(tmp, "out")

	cmd, _ := newTestCommand("generate", "--spec", spec, "--output", outDir)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected generate to succeed without --dry-run, got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "go.mod")); err != nil {
		t.Errorf("expected generated go.mod in output directory: %v", err)
	}
}

func TestGenerateCommand_RegisteredFlags(t *testing.T) {
	cmd := newRootCmd()
	genCmd, _, err := cmd.Find([]string{"generate"})
	if err != nil {
		t.Fatalf("failed to find generate command: %v", err)
	}
	if genCmd == nil || genCmd.Name() != "generate" {
		t.Fatalf("generate command not registered")
	}

	flags := []string{"spec", "output", "dry-run", "config", "dry-run-output", "generate-config", "force", "provider-name", "generate-terraform-tests",
		"spec-allow-http", "spec-auth-scheme", "spec-token-env", "spec-username-env", "spec-password-env", "spec-key-env", "spec-header-name", "spec-token-url", "spec-client-id-env", "spec-client-secret-env"}
	for _, name := range flags {
		if genCmd.Flags().Lookup(name) == nil {
			t.Errorf("generate command missing --%s flag", name)
		}
	}
}

func TestGenerateCommand_RequiredSpec(t *testing.T) {
	cmd, out := newTestCommand("generate")
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --spec")
	}
	if !strings.Contains(err.Error(), "spec") {
		t.Errorf("expected error to mention required flag spec, got %q", err.Error())
	}
	if out.Len() == 0 {
		t.Error("expected error output")
	}
}

func TestGenerateCommand_DryRunNoWrite(t *testing.T) {
	tmp := t.TempDir()
	spec := filepath.Join(tmp, "api.yaml")
	if err := os.WriteFile(spec, []byte("openapi: 3.0.0\ninfo:\n  title: Test API\n  version: 1.0.0\npaths: {}\n"), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	outDir := filepath.Join(tmp, "out")

	cmd, out := newTestCommand("generate", "--spec", spec, "--output", outDir, "--dry-run")
	if err := cmd.Execute(); err != nil {
		t.Fatalf("dry-run failed: %v\noutput:\n%s", err, out.String())
	}
	output := out.String()
	if !strings.Contains(output, "Eidos dry-run summary") {
		t.Errorf("expected dry-run summary in output, got:\n%s", output)
	}
	if !strings.Contains(output, spec) {
		t.Errorf("expected output to mention spec path, got:\n%s", output)
	}
	if !strings.Contains(output, "Files that would be written") {
		t.Errorf("expected output to list files, got:\n%s", output)
	}
	if _, err := os.Stat(outDir); !os.IsNotExist(err) {
		t.Errorf("output directory %q should not have been created", outDir)
	}
}

func TestGenerateCommand_DryRunJSONOutput(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	spec := "api.yaml"
	if err := os.WriteFile(spec, []byte("openapi: 3.0.0\ninfo:\n  title: Test API\n  version: 1.0.0\npaths: {}\n"), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	cmd, out := newTestCommand("generate", "--spec", spec, "--output", "out", "--dry-run", "--dry-run-output", "summary.json")
	if err := cmd.Execute(); err != nil {
		t.Fatalf("dry-run with output failed: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected no stdout when --dry-run-output is set, got:\n%s", out.String())
	}
	if _, err := os.Stat("out"); !os.IsNotExist(err) {
		t.Errorf("output directory %q should not have been created", "out")
	}
	data, err := os.ReadFile("summary.json")
	if err != nil {
		t.Fatalf("reading summary file: %v", err)
	}
	if !bytes.Contains(data, []byte(`"spec"`)) {
		t.Errorf("expected JSON summary to contain spec field, got:\n%s", string(data))
	}

	t.Run("case-insensitive JSON extension", func(t *testing.T) {
		cmd, out := newTestCommand("generate", "--spec", spec, "--output", "out2", "--dry-run", "--dry-run-output", "summary.JSON")
		if err := cmd.Execute(); err != nil {
			t.Fatalf("dry-run with .JSON output failed: %v", err)
		}
		if out.Len() != 0 {
			t.Errorf("expected no stdout when --dry-run-output is set, got:\n%s", out.String())
		}
		if _, err := os.Stat("out2"); !os.IsNotExist(err) {
			t.Errorf("output directory %q should not have been created", "out2")
		}
		data, err := os.ReadFile("summary.JSON")
		if err != nil {
			t.Fatalf("reading summary file: %v", err)
		}
		if !bytes.Contains(data, []byte(`"provider_name"`)) {
			t.Errorf("expected JSON summary to contain provider_name field, got:\n%s", string(data))
		}
	})
}

func TestGenerateCommand_DryRunTextOutput_SuppressesStdout(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	spec := "api.yaml"
	if err := os.WriteFile(spec, []byte("openapi: 3.0.0\ninfo:\n  title: Test API\n  version: 1.0.0\npaths: {}\n"), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	cmd, out := newTestCommand("generate", "--spec", spec, "--output", "out", "--dry-run", "--dry-run-output", "summary.txt")
	if err := cmd.Execute(); err != nil {
		t.Fatalf("dry-run with text output failed: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected no stdout when --dry-run-output is set, got:\n%s", out.String())
	}
	if _, err := os.Stat("out"); !os.IsNotExist(err) {
		t.Errorf("output directory %q should not have been created", "out")
	}
	data, err := os.ReadFile("summary.txt")
	if err != nil {
		t.Fatalf("reading summary file: %v", err)
	}
	if !bytes.Contains(data, []byte("Eidos dry-run summary")) {
		t.Errorf("expected text summary to contain heading, got:\n%s", string(data))
	}
}

func TestGenerateCommand_DryRunOutput_FilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not meaningful on Windows")
	}

	oldMask := syscall.Umask(0)
	t.Cleanup(func() { syscall.Umask(oldMask) })

	tmp := t.TempDir()
	t.Chdir(tmp)

	spec := "api.yaml"
	if err := os.WriteFile(spec, []byte("openapi: 3.0.0\ninfo:\n  title: Test API\n  version: 1.0.0\npaths: {}\n"), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	cases := []string{"summary.json", "summary.txt"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			cmd, _ := newTestCommand("generate", "--spec", spec, "--output", "out", "--dry-run", "--dry-run-output", name)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("dry-run with output failed: %v", err)
			}
			info, err := os.Stat(name)
			if err != nil {
				t.Fatalf("stat dry-run output: %v", err)
			}
			if got := info.Mode().Perm(); got != 0o600 {
				t.Errorf("expected dry-run output permission 0o600, got 0o%03o", got)
			}
		})
	}
}

func TestGenerateCommand_DryRunOutput_RequiresLocalPath(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	spec := "api.yaml"
	if err := os.WriteFile(spec, []byte("openapi: 3.0.0\ninfo:\n  title: Test API\n  version: 1.0.0\npaths: {}\n"), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	cases := []struct {
		name      string
		path      string
		wantError bool
	}{
		{"absolute path", filepath.Join(tmp, "summary.json"), true},
		{"escapes cwd", "../summary.json", true},
		{"nested relative path", "nested/summary.json", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, _ := newTestCommand("generate", "--spec", spec, "--output", "out", "--dry-run", "--dry-run-output", tc.path)
			err := cmd.Execute()
			if tc.wantError {
				if err == nil {
					t.Fatal("expected error for non-local dry-run output path")
				}
				if !strings.Contains(err.Error(), "relative path inside the current working directory") {
					t.Errorf("expected error about relative path, got: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected success for local dry-run output path, got: %v", err)
			}
			if _, err := os.Stat(tc.path); err != nil {
				t.Fatalf("expected dry-run output file %q to exist: %v", tc.path, err)
			}
		})
	}
}

func TestGenerateCommand_GenerateConfig_WritesToOutputDir(t *testing.T) {
	tmp := t.TempDir()
	spec := filepath.Join(tmp, "api.yaml")
	if err := os.WriteFile(spec, []byte("openapi: 3.0.0\ninfo:\n  title: Test API\n  version: 1.0.0\npaths: {}\n"), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	outDir := filepath.Join(tmp, "out")

	cmd, out := newTestCommand("generate", "--spec", spec, "--output", outDir, "--generate-config")
	if err := cmd.Execute(); err != nil {
		t.Fatalf("generate with --generate-config failed: %v\noutput:\n%s", err, out.String())
	}

	configPath := filepath.Join(outDir, "generator.yaml")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected generator config %q to exist: %v", configPath, err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading generator config: %v", err)
	}
	if !strings.Contains(string(data), "provider:") {
		t.Errorf("expected generated config to contain provider section, got:\n%s", string(data))
	}

	stdout := out.String()
	if !strings.Contains(stdout, configPath) {
		t.Errorf("expected stdout to mention config path, got:\n%s", stdout)
	}
}

func TestGenerateCommand_GenerateConfig_WithDryRun(t *testing.T) {
	tmp := t.TempDir()
	spec := filepath.Join(tmp, "api.yaml")
	if err := os.WriteFile(spec, []byte("openapi: 3.0.0\ninfo:\n  title: Test API\n  version: 1.0.0\npaths: {}\n"), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	outDir := filepath.Join(tmp, "out")

	cmd, out := newTestCommand("generate", "--spec", spec, "--output", outDir, "--dry-run", "--generate-config")
	if err := cmd.Execute(); err != nil {
		t.Fatalf("generate with --dry-run --generate-config failed: %v\noutput:\n%s", err, out.String())
	}

	configPath := filepath.Join(outDir, "generator.yaml")
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not write generator config, but %q exists", configPath)
	}
	if !strings.Contains(out.String(), "Would write starter generator config") {
		t.Errorf("expected dry-run hint in output, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Eidos dry-run summary") {
		t.Errorf("expected dry-run summary in output, got:\n%s", out.String())
	}
}

func TestGenerateCommand_GenerateConfig_DefaultOutputDir(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd after chdir: %v", err)
	}
	if cwd != tmp {
		t.Fatalf("t.Chdir did not switch to tmp dir: got %s, want %s", cwd, tmp)
	}

	spec := filepath.Join(tmp, "api.yaml")
	if err := os.WriteFile(spec, []byte("openapi: 3.0.0\ninfo:\n  title: Test API\n  version: 1.0.0\npaths: {}\n"), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	cmd, out := newTestCommand("generate", "--spec", spec, "--generate-config")
	if err := cmd.Execute(); err != nil {
		t.Fatalf("generate with default output dir failed: %v\noutput:\n%s", err, out.String())
	}

	configPath := filepath.Join(tmp, "generator.yaml")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected generator config %q to exist: %v", configPath, err)
	}
	if !strings.Contains(out.String(), configPath) {
		t.Errorf("expected stdout to mention config path, got:\n%s", out.String())
	}
}

func TestGenerateCommand_GenerateConfig_RefusesOverwriteWithoutForce(t *testing.T) {
	tmp := t.TempDir()
	spec := filepath.Join(tmp, "api.yaml")
	if err := os.WriteFile(spec, []byte("openapi: 3.0.0\ninfo:\n  title: Test API\n  version: 1.0.0\npaths: {}\n"), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	outDir := filepath.Join(tmp, "out")
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		t.Fatalf("create output dir: %v", err)
	}
	configPath := filepath.Join(outDir, "generator.yaml")
	if err := os.WriteFile(configPath, []byte("existing"), 0o600); err != nil {
		t.Fatalf("write existing config: %v", err)
	}

	cmd, _ := newTestCommand("generate", "--spec", spec, "--output", outDir, "--generate-config")
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when overwriting without --force")
	}
	if !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Errorf("expected overwrite refusal, got %q", err.Error())
	}
}

func TestGenerateCommand_GenerateConfig_ForceOverwrite(t *testing.T) {
	tmp := t.TempDir()
	spec := filepath.Join(tmp, "api.yaml")
	if err := os.WriteFile(spec, []byte("openapi: 3.0.0\ninfo:\n  title: Test API\n  version: 1.0.0\npaths: {}\n"), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	outDir := filepath.Join(tmp, "out")
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		t.Fatalf("create output dir: %v", err)
	}
	configPath := filepath.Join(outDir, "generator.yaml")
	if err := os.WriteFile(configPath, []byte("existing"), 0o600); err != nil {
		t.Fatalf("write existing config: %v", err)
	}

	cmd, out := newTestCommand("generate", "--spec", spec, "--output", outDir, "--generate-config", "--force")
	if err := cmd.Execute(); err != nil {
		t.Fatalf("generate with --generate-config --force failed: %v\noutput:\n%s", err, out.String())
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading overwritten config: %v", err)
	}
	if !strings.Contains(string(data), "provider:") {
		t.Errorf("expected config to be overwritten with generated content, got:\n%s", string(data))
	}
}

func TestGenerateCommand_GenerateConfig_ProviderNameFlag(t *testing.T) {
	tmp := t.TempDir()
	spec := filepath.Join(tmp, "api.yaml")
	if err := os.WriteFile(spec, []byte("openapi: 3.0.0\ninfo:\n  title: Test API\n  version: 1.0.0\npaths: {}\n"), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	outDir := filepath.Join(tmp, "out")

	cmd, out := newTestCommand("generate", "--spec", spec, "--output", outDir, "--generate-config", "--provider-name", "mycloud")
	if err := cmd.Execute(); err != nil {
		t.Fatalf("generate with --provider-name failed: %v\noutput:\n%s", err, out.String())
	}

	configPath := filepath.Join(outDir, "generator.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading generated config: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "name: mycloud") {
		t.Errorf("expected provider name mycloud, got:\n%s", content)
	}
	if !strings.Contains(out.String(), "Next: edit") {
		t.Errorf("expected stdout to contain next-step hint, got:\n%s", out.String())
	}
}

func TestGenerateCommand_GenerateConfig_OutputIsFileError(t *testing.T) {
	tmp := t.TempDir()
	spec := filepath.Join(tmp, "api.yaml")
	if err := os.WriteFile(spec, []byte("openapi: 3.0.0\ninfo:\n  title: Test API\n  version: 1.0.0\npaths: {}\n"), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	existingFile := filepath.Join(tmp, "existing")
	if err := os.WriteFile(existingFile, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write existing file: %v", err)
	}

	cmd, _ := newTestCommand("generate", "--spec", spec, "--output", existingFile, "--generate-config")
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when output path is an existing file")
	}
	if !strings.Contains(err.Error(), "is not a directory") {
		t.Errorf("expected error to mention output is not a directory, got %q", err.Error())
	}
}

func TestGenerateCommand_GenerateConfig_MalformedSpec(t *testing.T) {
	tmp := t.TempDir()
	spec := filepath.Join(tmp, "api.yaml")
	if err := os.WriteFile(spec, []byte("not: a: valid: openapi: spec\n\n\n"), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	cmd, _ := newTestCommand("generate", "--spec", spec, "--generate-config")
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for malformed spec")
	}
	if !strings.Contains(err.Error(), "failed to generate starter config") && !strings.Contains(err.Error(), "failed to load spec") && !strings.Contains(err.Error(), "failed to convert") && !strings.Contains(err.Error(), "failed to build provider IR") {
		t.Errorf("expected error to mention starter config generation failure, got %q", err.Error())
	}
}

func TestGenerateCommand_GenerateConfig_ReadOnlyOutputDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not meaningful on Windows")
	}

	tmp := t.TempDir()
	spec := filepath.Join(tmp, "api.yaml")
	if err := os.WriteFile(spec, []byte("openapi: 3.0.0\ninfo:\n  title: Test API\n  version: 1.0.0\npaths: {}\n"), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	outDir := filepath.Join(tmp, "readonly")
	if err := os.MkdirAll(outDir, 0o500); err != nil {
		t.Fatalf("create read-only dir: %v", err)
	}

	cmd, _ := newTestCommand("generate", "--spec", spec, "--output", outDir, "--generate-config")
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when writing to read-only directory")
	}
}

func TestGenerateCommand_ConfigExcludesResource(t *testing.T) {
	tmp := t.TempDir()
	spec := writeMinimalSpec(t, tmp)
	configPath := filepath.Join(tmp, "gen.yaml")
	configContent := `provider:
  name: pet_api
  version: "0.1.0"
generation:
  resources:
    exclude:
      - pet
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd, out := newTestCommand("generate", "--spec", spec, "--output", filepath.Join(tmp, "out"), "--config", configPath, "--dry-run")
	if err := cmd.Execute(); err != nil {
		t.Fatalf("generate with --config failed: %v\noutput:\n%s", err, out.String())
	}
	output := out.String()
	if strings.Contains(output, "resource_pet.go") {
		t.Errorf("excluded resource pet should not appear in dry-run output:\n%s", output)
	}
	if !strings.Contains(output, "resource_owner.go") {
		t.Errorf("non-excluded resource owner should appear in dry-run output:\n%s", output)
	}
}

func TestGenerateCommand_WriteModeRefusesOverwriteWithoutForce(t *testing.T) {
	tmp := t.TempDir()
	spec := writeMinimalSpec(t, tmp)
	outDir := filepath.Join(tmp, "out")

	cmd1, _ := newTestCommand("generate", "--spec", spec, "--output", outDir)
	if err := cmd1.Execute(); err != nil {
		t.Fatalf("first generation failed: %v", err)
	}
	firstGoMod, err := os.ReadFile(filepath.Join(outDir, "go.mod"))
	if err != nil {
		t.Fatalf("read generated go.mod: %v", err)
	}

	cmd2, _ := newTestCommand("generate", "--spec", spec, "--output", outDir)
	err = cmd2.Execute()
	if err == nil {
		t.Fatal("expected second generation without --force to fail")
	}
	if !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Errorf("expected overwrite refusal, got %q", err.Error())
	}

	secondGoMod, err := os.ReadFile(filepath.Join(outDir, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod after failed overwrite: %v", err)
	}
	if !bytes.Equal(firstGoMod, secondGoMod) {
		t.Errorf("go.mod was modified despite overwrite refusal")
	}
}

func TestGenerateCommand_GenerateTerraformTests(t *testing.T) {
	tmp := t.TempDir()
	spec := writeMinimalSpec(t, tmp)

	cmd, out := newTestCommand("generate", "--spec", spec, "--output", filepath.Join(tmp, "out"), "--dry-run", "--generate-terraform-tests")
	if err := cmd.Execute(); err != nil {
		t.Fatalf("generate with --generate-terraform-tests failed: %v\noutput:\n%s", err, out.String())
	}
	output := out.String()
	if !strings.Contains(output, "tests/pet.tftest.hcl") {
		t.Errorf("expected .tftest.hcl file for pet in dry-run output:\n%s", output)
	}
	if !strings.Contains(output, "tests/modules/pet/main.tf") {
		t.Errorf("expected test module for pet in dry-run output:\n%s", output)
	}
}

func writeMinimalSpec(t *testing.T, dir string) string {
	t.Helper()
	spec := filepath.Join(dir, "api.yaml")
	content := `openapi: 3.0.0
info:
  title: Pet API
  version: 1.0.0
paths:
  /pets:
    post:
      operationId: createPets
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                name:
                  type: string
      responses:
        '201':
          description: created
          content:
            application/json:
              schema:
                type: object
                properties:
                  id:
                    type: string
  /pets/{id}:
    get:
      operationId: getPet
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  id:
                    type: string
                  name:
                    type: string
    delete:
      operationId: deletePet
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        '204':
          description: deleted
  /owners:
    post:
      operationId: createOwner
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                name:
                  type: string
      responses:
        '201':
          description: created
          content:
            application/json:
              schema:
                type: object
                properties:
                  id:
                    type: string
  /owners/{id}:
    get:
      operationId: getOwner
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  id:
                    type: string
                  name:
                    type: string
    delete:
      operationId: deleteOwner
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        '204':
          description: deleted
`
	if err := os.WriteFile(spec, []byte(content), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	return spec
}

func TestRunEidos_BadFlag_WritesErrorToStderr(t *testing.T) {
	cmd := newRootCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--bad-flag"})

	var stderr bytes.Buffer
	code := runEidosWith(cmd, &stderr)

	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "unknown flag: --bad-flag") {
		t.Errorf("expected stderr to contain unknown flag error, got %q", stderr.String())
	}
}
