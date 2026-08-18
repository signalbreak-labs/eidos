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
		Long: `Starts a Model Context Protocol server over stdio that advertises
 seven eidos/* tools: generate-config (scaffold a starter generator.yaml from
 an OpenAPI specification), inspect (IR preview with per-resource CRUD
 completeness), generate (run the pipeline, optionally writing the provider),
 validate-schemas (framework-validity of generated schemas), override-preview
 (IR after generator.yaml overrides with a per-entry match report), lookup
 (forward/reverse operation and schema lookup), and suggest-resources (CRUD
 grouping gaps as ready-to-paste resource_overrides).

Each stdio connection is served concurrently, but tool calls on a single
connection are serialized by the MCP SDK; in particular verify: true on
eidos/generate blocks its connection while "go mod tidy" and "go build ./..."
run (up to a 5-minute timeout), so no other tool call is serviced on that
connection in the meantime (N-61).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			server := mcp.NewServer(version)
			if err := server.Run(cmd.Context(), &mcp_sdk.StdioTransport{}); err != nil {
				return fmt.Errorf("mcp server failed: %w", err)
			}
			return nil
		},
	}
}
