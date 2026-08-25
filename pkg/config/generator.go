package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// WriteStarterGeneratorConfig emits a starter generator.yaml that references
// the supplied OpenAPI spec path and provider name. It is the scaffold
// implementation used by the eidos generate-config CLI command.
//
// The helper will create any missing parent directories for outputPath using
// os.MkdirAll. Callers that want to constrain writes to the working
// directory should validate outputPath before calling this helper.
func WriteStarterGeneratorConfig(specPath, outputPath, providerName string, force bool) error {
	if specPath == "" {
		return fmt.Errorf("spec path must not be empty")
	}
	if providerName == "" {
		return fmt.Errorf("provider name must not be empty")
	}

	cfg := fmt.Sprintf(starterGeneratorTemplate, providerName, DefaultProviderVersion, specPath)
	return WriteStarterGeneratorConfigBytes(outputPath, []byte(cfg), force)
}

// WriteStarterGeneratorConfigBytes writes serialized generator.yaml bytes to
// outputPath atomically. It creates parent directories as needed. When force is
// false and outputPath already exists, it refuses to overwrite. On any write or
// close failure the temporary file is removed so outputPath is not left in a
// partially-written state.
func WriteStarterGeneratorConfigBytes(outputPath string, data []byte, force bool) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
		return fmt.Errorf("failed to create output directory for config: %w", err)
	}

	if !force {
		if _, err := os.Stat(outputPath); err == nil {
			return fmt.Errorf("refusing to overwrite %s; pass --force to overwrite", outputPath)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("failed to check output file: %w", err)
		}
	}

	// Atomic write via a sibling temp file + rename so a failed Write or Close
	// never leaves a truncated output file on disk. The temp file is created
	// with O_EXCL and a random name (os.CreateTemp) so that a pre-existing .tmp
	// is not silently clobbered and a pre-placed symlink is not followed (L-19).
	tmp, err := os.CreateTemp(filepath.Dir(outputPath), ".eidos-generator-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to write generator config: %w", err)
	}
	tmpPath := tmp.Name()
	if _, writeErr := tmp.Write(data); writeErr != nil {
		//nolint:errcheck // best-effort cleanup after failed write
		_ = tmp.Close()
		//nolint:errcheck // best-effort cleanup of temporary file
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write generator config: %w", writeErr)
	}
	if closeErr := tmp.Close(); closeErr != nil {
		//nolint:errcheck // best-effort cleanup of temporary file
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to close generator config: %w", closeErr)
	}
	if renameErr := os.Rename(tmpPath, outputPath); renameErr != nil {
		//nolint:errcheck // best-effort cleanup of temporary file
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write generator config: %w", renameErr)
	}
	return nil
}

const starterGeneratorTemplate = `# Starter generator.yaml produced by eidos generate-config
provider:
  name: %q
  version: %q
spec:
  path: %q
  format: openapi3
# sign_release opts out of GPG-signed checksums. Signed releases are default-on;
# configure GPG_PRIVATE_KEY and GPG_PASSPHRASE repository secrets, or uncomment
# to disable signing:
# sign_release: false
generation:
  resources:
    include: []
    exclude: []
    package: ""
    packages: []
  datasources:
    include: []
    exclude: []
    package: ""
    packages: []
  actions:
    include: []
    exclude: []
    package: ""
    packages: []
  ephemeral_resources:
    include: []
    exclude: []
    package: ""
    packages: []
  list_resources:
    include: []
    exclude: []
    package: ""
    packages: []
  functions:
    include: []
    exclude: []
    package: ""
    packages: []
  skip_tests: false
  skip_docs: false
  skip_build: false
  # dynamic_release opts into also generating
  # .github/workflows/regenerate-and-release.yml: a manually-dispatched
  # workflow that regenerates this provider from generator.yaml (which carries
  # the spec reference and all overrides) and publishes a release using the
  # eidos CI image. Off by default. spec_path is optional: leave it unset to
  # regenerate the spec referenced by generator.yaml, or set it to override.
  # dynamic_release:
  #   enabled: true
  #   image: ghcr.io/signalbreak-labs/eidos:latest
  #   spec_path: spec.yaml
`
