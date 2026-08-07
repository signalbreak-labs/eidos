package main

import (
	"github.com/spf13/cobra"
)

// version is set at build time via -ldflags '-X main.version=...'. Release
// pipelines (for example, GoReleaser) should inject the matching eidos CLI
// version so that --version reports the release tag.
var version = "0.1.0-dev"

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "eidos",
		Short:         "Eidos generates Terraform providers from OpenAPI specifications",
		Long:          "Eidos is a generator that turns OpenAPI specifications into Terraform provider code using the Terraform Plugin Framework.",
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	root.AddCommand(newGenerateCmd())
	root.AddCommand(newGenerateConfigCmd())
	root.AddCommand(newMCPCmd())
	root.AddCommand(newAPICmd())

	return root
}

var rootCmd = newRootCmd()
