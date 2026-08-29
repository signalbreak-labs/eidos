package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/signalbreak-labs/eidos/pkg/api"
	"github.com/signalbreak-labs/eidos/pkg/config"
	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/parser"
	"github.com/signalbreak-labs/eidos/pkg/specsource"
)

type generateConfigFlags struct {
	spec             string
	output           string
	providerName     string
	force            bool
	noUsePutAsCreate bool
	remote           remoteSpecFlags
}

func newGenerateConfigCmd() *cobra.Command {
	flags := &generateConfigFlags{}

	cmd := &cobra.Command{
		Use:   "generate-config",
		Short: "Generate a starter generator.yaml from an OpenAPI specification",
		Long: `Produces a starter generator.yaml configuration file from the supplied
OpenAPI spec path. The emitted file is a scaffold that can be edited to add
custom overrides and rename resources before running eidos generate.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return recoverCommand(cmd, func() error { return runGenerateConfig(cmd, flags) })
		},
	}

	cmd.Flags().StringVar(&flags.spec, "spec", "", "Path to OpenAPI spec file (JSON or YAML), or an http(s) URL to fetch (required)")
	cmd.Flags().StringVar(&flags.output, "output", "generator.yaml", "Path to write the starter generator.yaml")
	cmd.Flags().StringVar(&flags.providerName, "provider-name", "generated", "Provider name to use in the generated config")
	cmd.Flags().BoolVar(&flags.force, "force", false, "Overwrite an existing output file")
	cmd.Flags().BoolVar(&flags.noUsePutAsCreate, "no-use-put-as-create", false,
		"Emit use_put_as_create: false (kill-switch). By default the generated config records use_put_as_create: true, so an instance-path PUT with no collection POST is used as the Create (upsert).")
	flags.remote.register(cmd)

	mustMarkFlagRequired(cmd, "spec")

	return cmd
}

func runGenerateConfig(cmd *cobra.Command, flags *generateConfigFlags) error {
	specBytes, _, err := loadSpecBytes(cmd.Context(), flags.spec, flags.remote.options(nil))
	if err != nil {
		return err
	}

	// The spec path written into the starter config is the absolute local path
	// for a file spec, or the URL itself for a remote spec.
	specDisplay := flags.spec
	if localPath, ok := specsource.LocalPath(flags.spec); ok {
		specDisplay = localPath
	}

	output := flags.output
	if output == "" {
		output = "generator.yaml"
	}
	absOutput, err := filepath.Abs(output)
	if err != nil {
		return fmt.Errorf("failed to resolve output path: %w", err)
	}

	// PUT-as-create is default-on; --no-use-put-as-create records the kill-switch
	// in the generated config and builds the IR with PUT-as-create disabled so the
	// emitted overrides stay consistent with the toggle.
	usePutAsCreate := !flags.noUsePutAsCreate

	convertDiags, err := writeStarterConfigFromSpec(specBytes, specDisplay, absOutput, strings.TrimSpace(flags.providerName), flags.force, usePutAsCreate)
	// Print diagnostics before the error check: when the write is refused on
	// error-severity diagnostics, the reason must be visible even though err is
	// returned (N-57).
	for _, d := range convertDiags {
		printDiagnostic(cmd.ErrOrStderr(), d)
	}
	if err != nil {
		return err
	}

	return writeStarterConfigHint(cmd.OutOrStdout(), absOutput, specDisplay)
}

func writeStarterConfigFromSpec(specData []byte, specPath, absOutput, providerName string, force, usePutAsCreate bool) (diagnostics.Diagnostics, error) {
	cfg, version, convertDiags, err := api.GenerateStarterConfigWithName(specData, specPath, providerName, usePutAsCreate)
	if err != nil {
		return convertDiags, fmt.Errorf("failed to generate starter config: %w", err)
	}
	// A starter config is never written from a spec that carries error-severity
	// diagnostics (duplicate operationIds, unsupported constructs): the emitted
	// file would be a partial-validity scaffold that misrepresents the IR. This
	// mirrors the MCP generate-config tool, which gates on resp.Valid (N-57);
	// callers print convertDiags before returning so the reason stays visible.
	if convertDiags.HasErrors() {
		return convertDiags, fmt.Errorf("refusing to write starter config: spec has error-severity diagnostics")
	}

	cfg.Spec = config.SpecConfig{
		Path:   specPath,
		Format: formatFromVersion(version),
	}

	// Surface config validation warnings (both-schema-and-operation set, env-var
	// collisions) as diagnostics. cfg.Warnings carries yaml:"-" json:"-" so the
	// generated file cannot hold them; without this they would be written by
	// config.Validate and immediately lost (M-16).
	for _, w := range cfg.Warnings {
		convertDiags = append(convertDiags, diagnostics.Diagnostic{
			Severity: diagnostics.Warning,
			Summary:  w,
		})
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return convertDiags, fmt.Errorf("failed to marshal starter config: %w", err)
	}

	if err := config.WriteStarterGeneratorConfigBytes(absOutput, data, force); err != nil {
		return convertDiags, err
	}
	return convertDiags, nil
}

func formatFromVersion(v parser.Version) string {
	switch v {
	case parser.Version2_0:
		return "openapi2"
	case parser.Version3_1:
		return "openapi31"
	default:
		return "openapi3"
	}
}
