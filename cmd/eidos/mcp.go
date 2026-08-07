package main

import (
	"fmt"

	mcp_sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/signalbreak-labs/eidos/pkg/mcp"
)

func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Start the Eidos MCP server",
		Long: `Starts a Model Context Protocol server over stdio that advertises five
 eidos/* tools: generate-config (scaffold a starter generator.yaml from an
 OpenAPI specification), inspect (IR preview with per-resource CRUD
 completeness), generate (run the pipeline, optionally writing the provider),
 validate-schemas (framework-validity of generated schemas), and
 override-preview (IR after generator.yaml overrides with a per-entry match
 report).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			server := mcp.NewServer(version)
			if err := server.Run(cmd.Context(), &mcp_sdk.StdioTransport{}); err != nil {
				return fmt.Errorf("mcp server failed: %w", err)
			}
			return nil
		},
	}
}
