package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/signalbreak-labs/eidos/pkg/api"
	"github.com/signalbreak-labs/eidos/pkg/config"
	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/generator"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

type generateFlags struct {
	// Spec is the path to the OpenAPI specification file, or an http(s) URL
	// that is fetched with the PROJECT_DESIGN §23 hardening (https-only by default,
	// SSRF guard, size/timeout caps, optional env-var-only auth). For
	// reproducible generation, pin a remote spec: commit the downloaded copy or
	// use a versioned spec URL — the remote document is not pinned across runs.
	spec string

	// Output is the target directory for the generated provider.
	output string

	// Config is the path to a generator.yaml overrides file.
	config string

	// DryRun prints a preview of the generated files without writing them.
	dryRun bool

	// DryRunOutput is the path to write the dry-run summary (JSON or text).
	dryRunOutput string

	// GenerateConfig writes a starter generator.yaml into the output directory.
	generateConfig bool

	// Force allows --generate-config to overwrite an existing generator.yaml.
	force bool

	// ProviderName is the provider name to use when writing a starter config.
	providerName string

	// GenerateTerraformTests emits native .tftest.hcl files in the output.
	generateTerraformTests bool

	// remote carries the opt-in remote --spec options (URL fetch + auth).
	remote remoteSpecFlags
}

func newGenerateCmd() *cobra.Command {
	flags := &generateFlags{}

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a Terraform provider from an OpenAPI specification",
		Long: `Runs the Eidos generation pipeline to turn an OpenAPI spec into a
Terraform provider. Use --dry-run to preview what would be generated.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runGenerate(cmd, flags)
		},
	}

	cmd.Flags().StringVar(&flags.spec, "spec", "", "Path to OpenAPI spec file (JSON or YAML), or an http(s) URL to fetch (required)")
	cmd.Flags().StringVar(&flags.output, "output", "", "Output directory for generated provider")
	cmd.Flags().StringVar(&flags.config, "config", "", "Path to generator.yaml overrides file")
	cmd.Flags().BoolVar(&flags.dryRun, "dry-run", false, "Run the full pipeline without writing files; print a summary")
	cmd.Flags().StringVar(&flags.dryRunOutput, "dry-run-output", "", "Path to write the dry-run summary (JSON or text)")
	cmd.Flags().BoolVar(&flags.generateConfig, "generate-config", false, "Write a starter generator.yaml into the output directory")
	cmd.Flags().BoolVar(&flags.force, "force", false, "Overwrite an existing generator.yaml when used with --generate-config, or overwrite generated provider files in write mode")
	cmd.Flags().StringVar(&flags.providerName, "provider-name", "", "Provider name for the starter config when used with --generate-config (defaults to the spec title)")
	cmd.Flags().BoolVar(&flags.generateTerraformTests, "generate-terraform-tests", false, "Generate native Terraform .tftest.hcl files")
	flags.remote.register(cmd)

	mustMarkFlagRequired(cmd, "spec")

	return cmd
}

func runGenerate(cmd *cobra.Command, flags *generateFlags) error {
	if err := validateDryRunOutput(flags.dryRunOutput, flags.dryRun); err != nil {
		return err
	}

	// The config is loaded before the spec so a remote --spec fetch can honor
	// the generator.yaml spec.auth section (CLI flags override it).
	cfg, err := loadGeneratorConfig(flags.config)
	if err != nil {
		return err
	}

	specBytes, contentType, err := loadSpecBytes(flags.spec, flags.remote.options(cfg))
	if err != nil {
		return err
	}

	// The display path is the absolute local path for a file spec, or the URL
	// itself for a remote spec (filepath.Abs would mangle a URL).
	specDisplay := flags.spec
	if !isRemoteSpecURL(flags.spec) {
		absSpec, aerr := filepath.Abs(flags.spec)
		if aerr != nil {
			return fmt.Errorf("failed to resolve spec path: %w", aerr)
		}
		specDisplay = absSpec
	}

	// In non-dry-run --generate-config mode, handleGenerateConfig builds its
	// own IR from the spec. Building the provider IR here too would parse the
	// spec twice (M-1), so defer the main build until after the starter-config
	// path has had a chance to short-circuit.
	if flags.generateConfig && !flags.dryRun {
		done, err := handleGenerateConfig(cmd, flags, specBytes, specDisplay)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}

	// --output is required for full (non-dry-run) generation. Validate it before
	// running the expensive IR pipeline so a missing flag fails fast instead of
	// after all the parse/transform/generate work (L-2). The dry-run and
	// --generate-config short-circuit paths above do not need --output.
	if !flags.dryRun && flags.output == "" {
		return fmt.Errorf("--output is required for full provider generation")
	}

	provider, _, genDiags, err := api.BuildProviderIRWithName(specBytes, specDisplay, contentType, cfg)
	if err != nil {
		return failBuildProviderIR(cmd, genDiags, err)
	}
	// Error-severity diagnostics (e.g. duplicate construct names from colliding
	// operationIds) mean the spec cannot be generated as-is; fail before the
	// generator runs so the user sees the actionable diagnostic rather than a
	// downstream "duplicate output path" error.
	if genDiags.HasErrors() {
		return failBuildProviderIR(cmd, genDiags, fmt.Errorf("spec cannot be generated: the provider IR contains error diagnostics (see above)"))
	}

	if cfg != nil {
		filter := generator.ProviderFilterFromConfig(cfg.Generation)
		// Surface malformed include/exclude patterns (e.g. an unmatched "[")
		// before filtering so a typo does not silently drop an entire construct
		// family (M-57).
		if err := filter.Validate(); err != nil {
			return err
		}
		provider = generator.FilterProviderIR(provider, filter)
	}

	if flags.generateConfig && flags.dryRun {
		done, err := handleGenerateConfig(cmd, flags, specBytes, specDisplay)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}

	mode := generator.ModeWrite
	if flags.dryRun {
		mode = generator.ModeRecord
	}

	collectOpts := generator.DefaultCollectOptions()
	collectOpts.IncludeTerraformTests = flags.generateTerraformTests

	files, err := generator.Run(provider, generator.Options{
		Mode:           mode,
		OutputDir:      flags.output,
		CollectOptions: collectOpts,
		Force:          flags.force,
	})
	if err != nil {
		return fmt.Errorf("generation failed: %w", err)
	}

	return writeGenerateSummary(cmd, flags, provider, files, genDiags)
}

func validateDryRunOutput(path string, dryRun bool) error {
	if path == "" {
		return nil
	}
	if !dryRun {
		return fmt.Errorf("--dry-run-output requires --dry-run")
	}
	if !filepath.IsLocal(path) {
		return fmt.Errorf("dry-run output path %q must be a relative path inside the current working directory", path)
	}
	return nil
}

func loadGeneratorConfig(configPath string) (*config.Config, error) {
	if configPath == "" {
		return nil, nil
	}
	//nolint:gosec // config file path is user-supplied and intentionally read.
	cfgBytes, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config %q: %w", configPath, err)
	}
	cfg, err := config.LoadBytes(cfgBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to load config %q: %w", configPath, err)
	}
	return cfg, nil
}

// handleGenerateConfig writes or previews the starter generator.yaml. It returns
// done=true when the command should stop after writing (non-dry-run), and
// done=false when generation should continue with the provider dry-run summary
// (dry-run mode).
func handleGenerateConfig(cmd *cobra.Command, flags *generateFlags, specBytes []byte, specDisplay string) (bool, error) {
	outputPath, err := starterConfigPath(flags.output)
	if err != nil {
		return false, err
	}
	providerName := strings.TrimSpace(flags.providerName)
	if flags.dryRun {
		return false, writeDryRunStarterConfigHint(cmd.OutOrStdout(), outputPath, specDisplay)
	}
	convertDiags, err := writeStarterConfigFromSpec(specBytes, specDisplay, outputPath, providerName, flags.force)
	if err != nil {
		return false, err
	}
	for _, d := range convertDiags {
		printDiagnostic(cmd.ErrOrStderr(), d)
	}
	return true, writeStarterConfigHint(cmd.OutOrStdout(), outputPath, specDisplay)
}

// failBuildProviderIR prints the pipeline diagnostics and wraps err so the
// caller returns a single error. It is shared by the build-failure and
// error-diagnostic paths so both surface the same actionable diagnostics.
func failBuildProviderIR(cmd *cobra.Command, genDiags diagnostics.Diagnostics, err error) error {
	for _, d := range genDiags {
		printDiagnostic(cmd.ErrOrStderr(), d)
	}
	return err
}

func printDiagnostic(w io.Writer, d diagnostics.Diagnostic) {
	msg := d.Summary
	if d.Detail != "" {
		msg += ": " + d.Detail
	}
	//nolint:errcheck // diagnostic printing; I/O errors are best-effort
	fmt.Fprintf(w, "%s: %s\n", d.Severity.String(), msg)
}

func starterConfigPath(outputDir string) (string, error) {
	if outputDir == "" {
		return filepath.Abs("generator.yaml")
	}
	info, err := os.Stat(outputDir)
	if err != nil {
		if os.IsNotExist(err) {
			return filepath.Abs(filepath.Join(outputDir, "generator.yaml"))
		}
		return "", fmt.Errorf("failed to validate output directory %q: %w", outputDir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("output path %q is not a directory", outputDir)
	}
	return filepath.Abs(filepath.Join(outputDir, "generator.yaml"))
}

// writeStarterConfigHint prints the post-generation next-step message.
func writeStarterConfigHint(w io.Writer, outputPath, absSpec string) error {
	var msg bytes.Buffer
	fmt.Fprintf(&msg, "Wrote starter generator config to %s\n", outputPath)
	fmt.Fprintf(&msg, "Next: edit %s, then run 'eidos generate --config %s --spec %q'\n", outputPath, outputPath, absSpec)
	fmt.Fprint(&msg, "Specify --output <dir> to choose a target directory for the generated provider.\n")
	_, err := w.Write(msg.Bytes())
	return err
}

// writeDryRunStarterConfigHint prints the preview message when --generate-config
// is used with --dry-run, without writing the config to disk.
func writeDryRunStarterConfigHint(w io.Writer, outputPath, absSpec string) error {
	var msg bytes.Buffer
	fmt.Fprintf(&msg, "Would write starter generator config to %s\n", outputPath)
	fmt.Fprintf(&msg, "Next: edit %s, then run 'eidos generate --config %s --spec %q'\n", outputPath, outputPath, absSpec)
	fmt.Fprint(&msg, "Specify --output <dir> to choose a target directory for the generated provider.\n")
	_, err := w.Write(msg.Bytes())
	return err
}

func writeGenerateSummary(cmd *cobra.Command, flags *generateFlags, provider *ir.ProviderIR, files []generator.FileEntry, genDiags diagnostics.Diagnostics) error {
	allDiags := genDiags
	for _, d := range allDiags {
		printDiagnostic(cmd.ErrOrStderr(), d)
	}

	summary := generator.NewSummary(provider, flags.spec, flags.config, files, allDiags)
	summary.Written = !flags.dryRun

	var output []byte
	var err error
	if flags.dryRunOutput != "" && strings.EqualFold(filepath.Ext(flags.dryRunOutput), ".json") {
		output, err = generator.FormatJSON(summary)
		if err != nil {
			return fmt.Errorf("failed to format dry-run summary: %w", err)
		}
	} else {
		output = []byte(generator.FormatText(summary))
	}

	if flags.dryRunOutput != "" {
		dir := filepath.Dir(flags.dryRunOutput)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o750); err != nil {
				return fmt.Errorf("failed to create dry-run output directory: %w", err)
			}
		}
		if err := os.WriteFile(flags.dryRunOutput, output, 0o600); err != nil {
			return fmt.Errorf("failed to write dry-run output: %w", err)
		}
		return nil
	}

	_, err = cmd.OutOrStdout().Write(output)
	return err
}
