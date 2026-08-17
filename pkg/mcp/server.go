package mcp

import (
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewServer returns an MCP server that advertises the eidos/* tools —
// generate-config, inspect, generate, validate-schemas, override-preview,
// lookup, and suggest-resources. It can be connected to a reference MCP host or
// run over stdio by the command wiring in cmd/eidos.
func NewServer(version string) *sdkmcp.Server {
	server := sdkmcp.NewServer(
		&sdkmcp.Implementation{Name: "eidos", Version: version},
		nil,
	)

	sdkmcp.AddTool(server, GenerateConfigTool(), HandleGenerateConfig)
	sdkmcp.AddTool(server, InspectTool(), HandleInspect)
	sdkmcp.AddTool(server, GenerateTool(), HandleGenerate)
	sdkmcp.AddTool(server, ValidateSchemasTool(), HandleValidateSchemas)
	sdkmcp.AddTool(server, OverridePreviewTool(), HandleOverridePreview)
	sdkmcp.AddTool(server, LookupTool(), HandleLookup)
	sdkmcp.AddTool(server, SuggestResourcesTool(), HandleSuggestResources)

	return server
}
