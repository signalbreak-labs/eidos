// Package mcp implements the eidos/generate-config Model Context Protocol tool.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"

	"github.com/signalbreak-labs/eidos/pkg/api"
	"github.com/signalbreak-labs/eidos/pkg/config"
	"github.com/signalbreak-labs/eidos/pkg/generator"
)

// GenerateConfigArgs is the argument shape accepted by eidos/generate-config.
type GenerateConfigArgs struct {
	// Spec is the OpenAPI document as a JSON/YAML string or a parsed object.
	Spec any `json:"spec"`
	// Format selects the output serialization for the returned config. Valid
	// values are "yaml" (default) and "json".
	Format string `json:"format,omitempty"`
	// IncludeComments requests explanatory comments in generated YAML output.
	IncludeComments bool `json:"include_comments,omitempty"`
	// SkipOperations lists operation IDs or name patterns to omit from generated
	// resources and data sources.
	SkipOperations []string `json:"skip_operations,omitempty"`
	// IncludeOperations lists operation IDs or name patterns that must be present
	// for a resource or data source to be generated. When empty, all operations
	// are candidates.
	IncludeOperations []string `json:"include_operations,omitempty"`
}

// GenerateConfigResult is the JSON shape returned by eidos/generate-config.
type GenerateConfigResult struct {
	Config      string               `json:"config"`
	Diagnostics []api.DiagnosticJSON `json:"diagnostics"`
	// Valid reports whether the spec validated without error-severity
	// diagnostics. It mirrors api.ValidateResponse.Valid and lets an MCP client
	// distinguish a starter config produced from a clean spec from one produced
	// (or, when false, withheld) from a spec with known errors (M-54). When
	// Valid is false, Config is empty even if an IR preview was built.
	Valid bool `json:"valid"`
}

// GenerateConfigTool returns the eidos/generate-config MCP tool definition.
func GenerateConfigTool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "eidos/generate-config",
		Description: "Generate a starter Eidos generator.yaml configuration from an OpenAPI specification. Format may be yaml (default) or json; include_comments adds generator metadata (YAML section comments, JSON _generator field).",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"spec": specInputSchema(),
				"format": {
					Type:        "string",
					Description: "Output format for the generated config: yaml (default) or json",
					Enum:        []any{"yaml", "json"},
				},
				"include_comments": {
					Type:        "boolean",
					Description: "Add generator metadata to the output (YAML section comments, JSON _generator field)",
				},
				"skip_operations": {
					Type:        "array",
					Description: "Operation IDs or name patterns to skip during generation",
					Items:       &jsonschema.Schema{Type: "string"},
				},
				"include_operations": {
					Type:        "array",
					Description: "Operation IDs or name patterns that must match for an operation to be included",
					Items:       &jsonschema.Schema{Type: "string"},
				},
			},
			Required: []string{"spec"},
		},
		OutputSchema: &jsonschema.Schema{
			Type:        "object",
			Description: "Result of the eidos/generate-config tool call",
			Required:    []string{"config", "diagnostics", "valid"},
			Properties: map[string]*jsonschema.Schema{
				"config": {
					Type:        "string",
					Description: "Generated Eidos generator.yaml configuration, serialized according to the requested format. Empty when the spec did not validate (valid=false).",
				},
				"valid": {
					Type:        "boolean",
					Description: "Whether the spec validated without error-severity diagnostics. When false, config is empty even if an IR preview was built.",
				},
				"diagnostics": {
					Type:        "array",
					Description: "Diagnostics produced while validating the spec and generating the config",
					Items: &jsonschema.Schema{
						Type: "object",
						Properties: map[string]*jsonschema.Schema{
							"severity": {
								Type:        "string",
								Description: "Diagnostic severity",
								Enum:        []interface{}{"error", "warning", "info"},
							},
							"summary": {
								Type:        "string",
								Description: "Short human-readable summary of the diagnostic",
							},
							"detail": {
								Type:        "string",
								Description: "Additional detail for the diagnostic",
							},
							"source_location": {
								Type:        "object",
								Description: "Optional location in the source spec that triggered the diagnostic (may be null or absent)",
								Properties: map[string]*jsonschema.Schema{
									"file": {
										Type:        "string",
										Description: "File or source reference containing the issue",
									},
									"line": {
										Type:        "integer",
										Description: "One-based line number in the source",
									},
									"column": {
										Type:        "integer",
										Description: "One-based column number in the source",
									},
									"path": {
										Type:        "string",
										Description: "JSON pointer or path to the location within the source",
									},
								},
							},
						},
						Required: []string{"severity", "summary"},
					},
				},
			},
		},
	}
}

// HandleGenerateConfig runs the Eidos discovery pipeline against the supplied
// spec and returns a serialized generator config plus any diagnostics.
//
// The three return values are:
//   - *mcp.CallToolResult: the MCP tool result content (application errors are
//     embedded here, not returned as the third error value).
//   - GenerateConfigResult: the structured output value for clients that
//     support parsed tool results.
//   - error: reserved for protocol-level errors; it is always nil because
//     application errors are represented as diagnostics inside the result.
func HandleGenerateConfig(ctx context.Context, _ *mcp.CallToolRequest, args GenerateConfigArgs) (res *mcp.CallToolResult, out GenerateConfigResult, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			// Log the panic with a stack trace server-side so a recurring
			// generator panic leaves evidence to debug from; the client only
			// sees a generic diagnostic (L-66: the prior recovery converted the
			// panic to a diagnostic with no stack trace and no server log).
			log.Printf("panic in eidos/generate-config handler: %v\n%s", rec, debug.Stack())
			res, out = resultFromError(fmt.Errorf("panic in generate-config handler: %v", rec))
			err = nil
		}
	}()

	specBytes, err := normalizeSpec(ctx, args.Spec)
	if err != nil {
		res, out = resultFromError(err)
		return res, out, nil
	}

	resp := validateContext(ctx, specBytes)

	result := GenerateConfigResult{}
	if resp.Diagnostics != nil {
		result.Diagnostics = resp.Diagnostics
	} else {
		result.Diagnostics = []api.DiagnosticJSON{}
	}
	// Surface validation status so an MCP client can tell whether the spec
	// validated (M-54). Config generation is gated on Valid so a starter config
	// is never produced from a spec with error-severity diagnostics, even when
	// parsing succeeded enough to build an IR preview.
	result.Valid = resp.Valid
	if resp.Valid && resp.IRPreview != nil {
		cfg, genErr := generator.GenerateConfig(*resp.IRPreview)
		if genErr != nil {
			result.Diagnostics = append(result.Diagnostics, api.DiagnosticJSON{
				Severity: "error",
				Summary:  "Config generation failed",
				Detail:   genErr.Error(),
			})
		} else {
			applyOperationFilters(cfg, args.SkipOperations, args.IncludeOperations)
			out, marshalErr := marshalConfig(cfg, args.Format, args.IncludeComments)
			if marshalErr != nil {
				result.Diagnostics = append(result.Diagnostics, api.DiagnosticJSON{
					Severity: "error",
					Summary:  "Failed to serialize generated config",
					Detail:   marshalErr.Error(),
				})
			} else {
				result.Config = string(out)
			}
		}
	}

	data, err := json.Marshal(result)
	if err != nil {
		res, out = resultFromError(fmt.Errorf("failed to marshal tool result: %w", err))
		return res, out, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(data)},
		},
	}, result, nil
}

// maxSpecSize is the largest OpenAPI document the MCP tool will accept. It
// mirrors the HTTP API request body limit and bounds the memory the handler
// allocates after the MCP SDK has decoded the JSON arguments payload.
//
// Caveat: the MCP SDK fully decodes the arguments (including spec) into Go
// values before HandleGenerateConfig runs, so this limit is enforced
// post-decode and does not bound the transport-level memory the SDK allocates
// while parsing the request. It prevents the handler itself from amplifying a
// huge spec into the generator pipeline, but a client could still exhaust
// memory during the SDK's own decode (L-67: the prior comment's OOM claim held
// only after decode).
const maxSpecSize = 10 * 1024 * 1024 // 10 MiB

// validateContextMu guards validateContextFn. Tests swap validateContextFn to
// exercise panic-recovery paths; the mutex makes that swap safe if a future
// test runs in parallel with the handler (L-71: the prior mutable package
// global was a latent data race).
var (
	validateContextMu sync.RWMutex
	validateContextFn = api.ValidateContext
)

// validateContext calls the (possibly test-swapped) validate function under the
// seam mutex. Production reads the default api.ValidateContext.
func validateContext(ctx context.Context, spec []byte) api.ValidateResponse {
	validateContextMu.RLock()
	fn := validateContextFn
	validateContextMu.RUnlock()
	return fn(ctx, spec)
}

// setValidateContextForTest swaps the validate function used by the handler.
// It is intended for tests only and acquires the seam mutex so the swap does
// not race with in-flight handler calls.
func setValidateContextForTest(fn func(context.Context, []byte) api.ValidateResponse) {
	validateContextMu.Lock()
	validateContextFn = fn
	validateContextMu.Unlock()
}

func normalizeSpec(ctx context.Context, spec any) ([]byte, error) {
	if spec == nil {
		return nil, fmt.Errorf("spec is required")
	}
	var specBytes []byte
	switch v := spec.(type) {
	case string:
		// A string may be inline spec content OR a reference the CLI also
		// accepts: a local file path, a file:// URL, or an http(s):// URL. Try
		// to load it as a reference first; if it is not one, treat it as inline
		// content so callers that pass the spec body still work.
		if b, err := loadSpecRef(ctx, v); err == nil {
			specBytes = b
		} else if errors.Is(err, errNotASourceRef) {
			specBytes = []byte(v)
		} else {
			return nil, err
		}
	case []byte:
		specBytes = v
	case json.RawMessage:
		specBytes = []byte(v)
	case map[string]any:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal spec object: %w", err)
		}
		specBytes = data
	default:
		// Reject scalar or otherwise unsupported types early instead of
		// letting api.Validate fail with a less helpful parse error.
		return nil, fmt.Errorf("spec must be a string, []byte, json.RawMessage, or map[string]any, got %T", v)
	}
	if len(specBytes) > maxSpecSize {
		return nil, fmt.Errorf("spec exceeds maximum size of %d bytes", maxSpecSize)
	}
	return specBytes, nil
}

// normalizeConfig resolves a generator.yaml input the same way normalizeSpec
// resolves a spec: a string may be inline YAML/JSON content, a local file path,
// or a file:// URL. It tries to load the string as a file reference first; if it
// is not one, it returns the string unchanged so callers that pass the config
// body inline keep working. An empty config is returned empty (a no-op for
// mergeConfigIntoSpec). This lets an LLM pass `config` by path or file:// URL
// instead of only inline contents (M-76).
func normalizeConfig(ctx context.Context, configYAML string) (string, error) {
	if strings.TrimSpace(configYAML) == "" {
		return "", nil
	}
	if b, err := loadConfigRef(ctx, configYAML); err == nil {
		return string(b), nil
	} else if !errors.Is(err, errNotASourceRef) {
		return "", err
	}
	return configYAML, nil
}

// applyOperationFilters copies user-supplied operation filter lists into the
// generated config so they round-trip to generator.yaml. The actual filtering
// is performed by the generator pipeline via config.SkipOperations and the
// provider filter.
func applyOperationFilters(cfg *config.Config, skip, include []string) {
	if cfg == nil {
		return
	}
	if len(skip) > 0 {
		cfg.SkipOperations = append(cfg.SkipOperations, skip...)
	}
	if len(include) > 0 {
		cfg.IncludeOperations = append(cfg.IncludeOperations, include...)
	}
}

func marshalConfig(cfg *config.Config, format string, includeComments bool) ([]byte, error) {
	switch format {
	case "json":
		if includeComments {
			// Inject _generator as the first field via a wrapper struct so the
			// config's own field order is preserved. The prior implementation
			// round-tripped through map[string]any, which re-sorts all keys
			// alphabetically and produces a noisy diff against the non-comments
			// JSON output (L-70).
			return json.MarshalIndent(struct {
				Generator string `json:"_generator"`
				*config.Config
			}{Generator: "eidos/generate-config", Config: cfg}, "", "  ")
		}
		return json.MarshalIndent(cfg, "", "  ")
	case "yaml", "":
		if includeComments {
			return marshalConfigWithComments(cfg)
		}
		return yaml.Marshal(cfg)
	default:
		// The MCP SDK validates format against the tool's input schema enum
		// ("yaml"|"json") before this handler runs, so this branch is
		// unreachable through the MCP transport. It is retained for direct
		// callers (e.g. tests) that bypass the SDK, where an unsupported
		// format is a genuine caller error rather than a protocol violation
		// (L-68: previously the two divergent error shapes — the SDK's IsError
		// envelope vs. this diagnostics payload — were undocumented).
		return nil, fmt.Errorf("unsupported format %q", format)
	}
}

// sectionComment provides a short explanatory comment for each top-level
// generator.yaml section. It is used when include_comments is enabled.
var sectionComment = map[string]string{
	"provider":                     "Provider metadata (name, version, description, etc.)",
	"servers":                      "Optional server URL templates and variables",
	"resource_overrides":           "Optional per-resource customization",
	"datasource_overrides":         "Optional per-data-source customization",
	"action_overrides":             "Optional action overrides",
	"ephemeral_resource_overrides": "Optional ephemeral resource overrides",
	"list_resource_overrides":      "Optional list resource overrides",
	"function_overrides":           "Optional function overrides",
	"logging":                      "Optional HTTP trace logging configuration",
	"auth":                         "Optional provider authentication schemes",
	"naming":                       "Optional Terraform resource/data source naming rules",
	"skip_operations":              "Operation IDs or patterns to skip during generation",
	"include_operations":           "Operation IDs or patterns that must match for an operation to be included",
	"global_timeouts":              "Default CRUD timeout overrides",
	"pagination":                   "Collection pagination behavior",
	"polymorphism":                 "OneOf/anyOf generation strategy",
	"generate_terraform_tests":     "Whether to generate Terraform acceptance tests",
	"generation":                   "Controls which constructs are generated and how they are packaged",
	"spec":                         "Optional reference to the source OpenAPI spec (documentary)",
}

// marshalConfigWithComments serializes cfg to YAML and adds a banner header plus
// a short explanatory comment above each top-level section.
func marshalConfigWithComments(cfg *config.Config) ([]byte, error) {
	yamlBytes, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(yamlBytes, &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("expected YAML mapping node, got %v", doc.Content)
	}

	mapping := doc.Content[0]
	for i := 0; i < len(mapping.Content); i += 2 {
		keyNode := mapping.Content[i]
		if comment, ok := sectionComment[keyNode.Value]; ok {
			keyNode.HeadComment = comment
		}
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}

	return append([]byte("# Generated by eidos/generate-config\n"), buf.Bytes()...), nil
}

func resultFromError(err error) (*mcp.CallToolResult, GenerateConfigResult) {
	result := GenerateConfigResult{
		Diagnostics: []api.DiagnosticJSON{
			{
				Severity: "error",
				Summary:  err.Error(),
			},
		},
	}
	// GenerateConfigResult marshals only plain strings, so json.Marshal cannot
	// fail for this value in practice. The prior defensive branch returned a
	// contradictory "failed to marshal error result" message that would have
	// masked the original error the caller actually needs to see. Should a
	// future field ever make marshal fallible, surface the original error
	// verbatim rather than a misleading marshal complaint (L-72).
	data, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: err.Error()},
			},
		}, result
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(data)},
		},
	}, result
}
