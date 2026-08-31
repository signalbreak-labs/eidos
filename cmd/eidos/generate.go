package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"

	"github.com/signalbreak-labs/eidos/pkg/api"
	"github.com/signalbreak-labs/eidos/pkg/config"
	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/generator"
	"github.com/signalbreak-labs/eidos/pkg/ir"
	"github.com/signalbreak-labs/eidos/pkg/specsource"
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

	// NoUsePutAsCreate records the PUT-as-create kill-switch in a starter
	// generator.yaml emitted by --generate-config. Default-on otherwise.
	noUsePutAsCreate bool

	// SkipBuild drops the build/CI/release scaffolding files (GNUmakefile,
	// .goreleaser.yml, .github/workflows/release.yml,
	// terraform-registry-manifest.json) from the generation output. Mirrors the
	// generation.skip_build config key; the flag wins when both are set.
	skipBuild bool

	// OnlyBuild inverts the selection: emit only the build/CI/release
	// scaffolding files and nothing else. Useful for checking in the release
	// scaffolding once while the provider code is regenerated dynamically in
	// CI. Mutually exclusive with --skip-build. Requires --output unless
	// --dry-run, like the normal write path.
	onlyBuild bool

	// DynamicRelease opts into also generating a
	// .github/workflows/regenerate-and-release.yml that regenerates the provider
	// from its spec and publishes a release using the eidos CI image. Mirrors
	// generation.dynamic_release.enabled; the flag wins when both are set.
	dynamicRelease bool

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
			return recoverCommand(cmd, func() error { return runGenerate(cmd, flags) })
		},
	}

	cmd.Flags().StringVar(&flags.spec, "spec", "", "Path to OpenAPI spec file (JSON or YAML), or an http(s) URL to fetch. Optional when --config points to a generator.yaml with a spec.path; the CLI flag takes precedence")
	cmd.Flags().StringVar(&flags.output, "output", "", "Output directory for generated provider")
	cmd.Flags().StringVar(&flags.config, "config", "", "Path to generator.yaml overrides file")
	cmd.Flags().BoolVar(&flags.dryRun, "dry-run", false, "Run the full pipeline without writing files; print a summary")
	cmd.Flags().StringVar(&flags.dryRunOutput, "dry-run-output", "", "Path to write the dry-run summary (JSON or text)")
	cmd.Flags().BoolVar(&flags.generateConfig, "generate-config", false, "Write a starter generator.yaml into the output directory")
	cmd.Flags().BoolVar(&flags.force, "force", false, "Overwrite an existing generator.yaml when used with --generate-config, or overwrite generated provider files in write mode")
	cmd.Flags().StringVar(&flags.providerName, "provider-name", "", "Provider name for the starter config when used with --generate-config (defaults to the spec title)")
	cmd.Flags().BoolVar(&flags.generateTerraformTests, "generate-terraform-tests", false, "Generate native Terraform .tftest.hcl files")
	cmd.Flags().BoolVar(&flags.noUsePutAsCreate, "no-use-put-as-create", false,
		"With --generate-config, emit use_put_as_create: false (kill-switch). By default the starter config records use_put_as_create: true, so an instance-path PUT with no collection POST is used as the Create (upsert).")
	cmd.Flags().BoolVar(&flags.skipBuild, "skip-build", false,
		"Skip the build/CI/release files (GNUmakefile, .goreleaser.yml, .github/workflows/release.yml, terraform-registry-manifest.json). Mirrors generation.skip_build.")
	cmd.Flags().BoolVar(&flags.onlyBuild, "only-build", false,
		"Generate only the build/CI/release files (GNUmakefile, .goreleaser.yml, .github/workflows/release.yml, terraform-registry-manifest.json) and nothing else. --dynamic-release additionally adds .github/workflows/regenerate-and-release.yml. Mutually exclusive with --skip-build.")
	cmd.Flags().BoolVar(&flags.dynamicRelease, "dynamic-release", false,
		"Also generate .github/workflows/regenerate-and-release.yml: a workflow that regenerates the provider from its spec and publishes a release using the eidos CI image. Mirrors generation.dynamic_release.enabled.")
	flags.remote.register(cmd)

	// --spec is NOT marked required: when --config supplies a generator.yaml
	// with a spec.path, the spec is read from the config. runGenerate fails
	// loud when neither --spec nor a config spec.path is present.
	return cmd
}

// printConfigWarnings surfaces non-fatal config validation warnings (both
// schema and operation set on an override, env-var collisions). cfg.Warnings
// carries yaml:"-" so a generator.yaml round-trip cannot hold them; without
// this they would be written by config.Validate and immediately lost (M-16).
func printConfigWarnings(cmd *cobra.Command, cfg *config.Config) {
	if cfg == nil {
		return
	}
	for _, w := range cfg.Warnings {
		printDiagnostic(cmd.ErrOrStderr(), diagnostics.Diagnostic{
			Severity: diagnostics.Warning,
			Summary:  w,
		})
	}
}

// recoverCommand converts a panic in a command's RunE body into a returned
// error, so a bug in the generation pipeline fails the CLI with a clean
// non-zero exit (Cobra prints the error) plus a logged stack trace instead of
// crashing the process mid-run. The generator already converts its own panics
// into render errors (renderFileSafely); this is the outer safety net for any
// other panic in the command path (N-65).
func recoverCommand(cmd *cobra.Command, run func() error) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "eidos: internal panic in %q:\n%v\n%s\n", cmd.Name(), rec, debug.Stack()) //nolint:errcheck // best-effort panic log to stderr; the converted error is what matters
			err = fmt.Errorf("internal error in %q: %v", cmd.Name(), rec)
		}
	}()
	return run()
}

func runGenerate(cmd *cobra.Command, flags *generateFlags) error {
	if err := validateDryRunOutput(flags.dryRunOutput, flags.dryRun); err != nil {
		return err
	}
	if flags.onlyBuild && flags.skipBuild {
		return fmt.Errorf("--only-build and --skip-build are mutually exclusive")
	}

	// The config is loaded before the spec so a remote --spec fetch can honor
	// the generator.yaml spec.auth section (CLI flags override it).
	cfg, err := loadGeneratorConfig(flags.config)
	if err != nil {
		return err
	}
	printConfigWarnings(cmd, cfg)

	specBytes, contentType, specPath, specDisplay, err := resolveGenerateSpec(cmd.Context(), flags, cfg)
	if err != nil {
		return err
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

	// G39: surface Terraform platform size-limit diagnostics (warning as the
	// estimated serialized schema approaches the 64 MiB GetProviderSchema cap)
	// alongside the pipeline diagnostics. Description truncation — the lever for
	// a provider over the cap — is applied before the check so a configured
	// limit brings the provider under the cap within this run; generator.Run
	// re-applies both (truncation is idempotent) and enforces the hard cap.
	limits := limitsFromConfig(cfg)
	generator.ApplyDescriptionLimit(provider, generator.EffectiveDescriptionLimit(limits))
	genDiags = append(genDiags, generator.CheckProviderSchemaSize(provider, limits)...)
	if genDiags.HasErrors() {
		return failBuildProviderIR(cmd, genDiags, fmt.Errorf("spec cannot be generated: the provider IR contains error diagnostics (see above)"))
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

	collectOpts := collectOptionsFor(cfg, flags)
	// The emitted generator.yaml preserves the input config's input-only
	// sections (spec:, generation:, provider metadata, client/naming/security/
	// limits, operation filters, feature toggles) so regenerating from the
	// output directory is a lossless round-trip (§3.5). cfg is nil when the
	// run was started from a bare --spec.
	collectOpts.BaseConfig = cfg

	// The generator re-emits a canonical generator.yaml from the IR (round-trip
	// reproducibility). When --output is the same directory as the input --config,
	// that emitted file would clobber the user's hand-written source-of-truth
	// config. Detect the collision and skip the emitted config, warning so the
	// user knows the canonical config was withheld rather than silently dropped
	// (M-74). The non-collision case still writes the canonical config into the
	// output directory.
	if collides, err := configOutputCollidesWithInput(flags.config, flags.output); err != nil {
		return err
	} else if collides {
		collectOpts.IncludeConfig = false
		//nolint:errcheck // best-effort warning to stderr; I/O failure is not actionable here.
		fmt.Fprintf(cmd.ErrOrStderr(),
			"warning: skipping generated generator.yaml because --output would overwrite the input --config %q;"+
				" the round-trip canonical config is not written. Use a separate --output directory to retain it.\n",
			flags.config)
	}

	files, err := generator.Run(provider, generator.Options{
		Mode:           mode,
		OutputDir:      flags.output,
		CollectOptions: collectOpts,
		Force:          flags.force,
		Limits:         limitsFromConfig(cfg),
	})
	if err != nil {
		return fmt.Errorf("generation failed: %w", err)
	}

	return writeGenerateSummary(cmd, flags, specPath, provider, files, genDiags)
}

// collectOptionsFor builds the generator CollectOptions for a run from the
// default-on set, the generator.yaml generation.skip_* toggles, and the CLI
// build flags. The generation.skip_tests/skip_docs/skip_build keys existed
// before but were never applied (the CLI hardcoded the default-on options);
// wiring them here makes the documented toggles functional. --skip-build is
// the CLI equivalent of skip_build and wins when both are set. --only-build
// short-circuits to exactly the four build/CI/release files, overriding every
// other selection; the full parse→transform pipeline still runs because the
// scaffolding is templated from BuildConfig, which is derived from the provider
// name.
// limitsFromConfig returns the generator.yaml limits: section, or nil when no
// config is in play (the generator then applies its built-in Terraform
// platform defaults).
func limitsFromConfig(cfg *config.Config) *config.LimitsConfig {
	if cfg == nil {
		return nil
	}
	return cfg.Limits
}

func collectOptionsFor(cfg *config.Config, flags *generateFlags) generator.CollectOptions {
	opts := generator.DefaultCollectOptions()
	opts.IncludeTerraformTests = flags.generateTerraformTests
	if cfg != nil {
		if cfg.Generation.SkipTests {
			opts.IncludeTests = false
		}
		if cfg.Generation.SkipDocs {
			opts.IncludeDocs = false
		}
		if cfg.Generation.SkipBuild {
			opts.IncludeBuild = false
		}
	}
	if flags.skipBuild {
		opts.IncludeBuild = false
	}
	// Dynamic release is opt-in via --dynamic-release or
	// generation.dynamic_release.enabled. It applies to both full and
	// --only-build runs: the regenerate-and-release workflow is part of the
	// build/CI scaffolding set, so --only-build --dynamic-release emits it
	// alongside the four static scaffolding files instead of silently dropping
	// the flag (M-78). The image and spec_path come from the config block; the
	// emitter applies its own defaults when they are empty.
	if flags.dynamicRelease || (cfg != nil && cfg.Generation.DynamicRelease != nil && cfg.Generation.DynamicRelease.Enabled) {
		opts.IncludeDynamicRelease = true
		if cfg != nil && cfg.Generation.DynamicRelease != nil {
			opts.DynamicReleaseImage = cfg.Generation.DynamicRelease.Image
			opts.DynamicReleaseSpecPath = cfg.Generation.DynamicRelease.SpecPath
		}
	}
	// When dynamic_release.spec_path is unset, fall back to the top-level
	// spec.path so a remote spec URL configured there drives the regeneration
	// workflow instead of the hardcoded "spec.yaml" default (which would not
	// exist in the repo and would make the manual workflow fail at runtime).
	// The emitter still applies its own default only when both are empty (M-79).
	if opts.IncludeDynamicRelease && strings.TrimSpace(opts.DynamicReleaseSpecPath) == "" && cfg != nil {
		opts.DynamicReleaseSpecPath = cfg.Spec.Path
	}
	// sign_release is the opt-out for default-on GPG signing. Thread the *bool
	// through so an explicit false disables signing; nil (no config) keeps the
	// BuildConfigFromIR default of signed releases.
	if cfg != nil {
		opts.SignRelease = cfg.SignRelease
		// provider.namespace — or the repository owner it resolves from —
		// overrides the provider-name default so the README advertises the
		// real registry address (§3.9).
		opts.RegistryNamespace = cfg.Provider.RegistryNamespace()
	}
	if flags.onlyBuild {
		// OnlyBuild emits exactly the build/CI/release scaffolding and nothing
		// else, but still honors an opted-in dynamic release workflow and an
		// explicit sign_release opt-out (N-39).
		return generator.CollectOptions{
			OnlyBuild:              true,
			IncludeDynamicRelease:  opts.IncludeDynamicRelease,
			DynamicReleaseImage:    opts.DynamicReleaseImage,
			DynamicReleaseSpecPath: opts.DynamicReleaseSpecPath,
			SignRelease:            opts.SignRelease,
			RegistryNamespace:      opts.RegistryNamespace,
		}
	}
	return opts
}

// configOutputCollidesWithInput reports whether the generator-emitted
// generator.yaml (written to <outputDir>/generator.yaml) would overwrite the
// user's input --config file. The emitted config is a round-trip canonical
// re-serialization of the IR; when it lands on the input path it clobbers the
// user's hand-written source-of-truth config (M-74).
//
// Returns false when either path is empty (no input config, or no write target
// such as a dry-run without --output), so those paths keep emitting/listing the
// canonical config. Paths are compared by their cleaned absolute form so
// relative paths, ".", and trailing-slash variants all resolve consistently.
func configOutputCollidesWithInput(configPath, outputDir string) (bool, error) {
	if configPath == "" || outputDir == "" {
		return false, nil
	}
	absConfig, err := filepath.Abs(configPath)
	if err != nil {
		return false, fmt.Errorf("failed to resolve config path %q: %w", configPath, err)
	}
	absOutput, err := filepath.Abs(outputDir)
	if err != nil {
		return false, fmt.Errorf("failed to resolve output directory %q: %w", outputDir, err)
	}
	emitted := filepath.Clean(filepath.Join(absOutput, "generator.yaml"))
	return filepath.Clean(absConfig) == emitted, nil
}

// resolveGenerateSpec resolves the spec source for a generation run. --spec is
// optional when --config supplies a generator.yaml with a spec.path; the CLI
// flag takes precedence (consistent with spec.auth, which CLI flags also
// override). Fail loud when neither is set rather than silently producing
// nothing. The display path is the absolute local path for a file spec, or the
// URL itself for a remote spec (filepath.Abs would mangle a URL).
func resolveGenerateSpec(ctx context.Context, flags *generateFlags, cfg *config.Config) ([]byte, string, string, string, error) {
	specPath := flags.spec
	if specPath == "" && cfg != nil {
		specPath = strings.TrimSpace(cfg.Spec.Path)
	}
	if specPath == "" {
		return nil, "", "", "", fmt.Errorf("--spec is required (or set spec.path in the generator.yaml passed via --config)")
	}
	specBytes, contentType, err := loadSpecBytes(ctx, specPath, flags.remote.options(cfg))
	if err != nil {
		return nil, "", "", "", err
	}
	specDisplay := specPath
	if localPath, ok := specsource.LocalPath(specPath); ok {
		specDisplay = localPath
	}
	return specBytes, contentType, specPath, specDisplay, nil
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
	// PUT-as-create is default-on; --no-use-put-as-create records the kill-switch
	// in the emitted starter config.
	usePutAsCreate := !flags.noUsePutAsCreate
	convertDiags, err := writeStarterConfigFromSpec(specBytes, specDisplay, outputPath, providerName, flags.force, usePutAsCreate)
	// Print diagnostics before the error check so a refused write (error-severity
	// diagnostics, N-57) still surfaces the reasons.
	for _, d := range convertDiags {
		printDiagnostic(cmd.ErrOrStderr(), d)
	}
	if err != nil {
		return false, err
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
	fmt.Fprintf(&msg, "Next: edit %s, then run 'eidos generate --config %s' (spec.path is read from the config; pass --spec %q to override)\n", outputPath, outputPath, absSpec)
	fmt.Fprint(&msg, "Specify --output <dir> to choose a target directory for the generated provider.\n")
	_, err := w.Write(msg.Bytes())
	return err
}

// writeDryRunStarterConfigHint prints the preview message when --generate-config
// is used with --dry-run, without writing the config to disk.
func writeDryRunStarterConfigHint(w io.Writer, outputPath, absSpec string) error {
	var msg bytes.Buffer
	fmt.Fprintf(&msg, "Would write starter generator config to %s\n", outputPath)
	fmt.Fprintf(&msg, "Next: edit %s, then run 'eidos generate --config %s' (spec.path is read from the config; pass --spec %q to override)\n", outputPath, outputPath, absSpec)
	fmt.Fprint(&msg, "Specify --output <dir> to choose a target directory for the generated provider.\n")
	_, err := w.Write(msg.Bytes())
	return err
}

func writeGenerateSummary(cmd *cobra.Command, flags *generateFlags, specPath string, provider *ir.ProviderIR, files []generator.FileEntry, genDiags diagnostics.Diagnostics) error {
	allDiags := genDiags
	for _, d := range allDiags {
		printDiagnostic(cmd.ErrOrStderr(), d)
	}

	summary := generator.NewSummary(provider, specPath, flags.config, files, allDiags)
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
