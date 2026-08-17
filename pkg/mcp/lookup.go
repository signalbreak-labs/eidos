package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/signalbreak-labs/eidos/pkg/api"
	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/transformer"
)

// ---------------------------------------------------------------------------
// eidos/lookup — operation<->schema two-directional lookup over the raw spec
// ---------------------------------------------------------------------------

// LookupArgs is the input to eidos/lookup. At least one of operation_id or
// schema must be set; both may be set to answer the two directions in one call.
type LookupArgs struct {
	Spec        string `json:"spec"`
	OperationID string `json:"operation_id,omitempty"`
	Schema      string `json:"schema,omitempty"`
}

// ParamSummary describes one path parameter of a looked-up operation.
type ParamSummary struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	Type     string `json:"type,omitempty"`
}

// OperationLookup is the forward-direction answer for a looked-up operation.
type OperationLookup struct {
	OperationID       string         `json:"operation_id"`
	Path              string         `json:"path"`
	Method            string         `json:"method"`
	PathParams        []ParamSummary `json:"path_params"`
	RequestBodySchema string         `json:"request_body_schema,omitempty"`
	RequestMediaType  string         `json:"request_media_type,omitempty"`
	ResponseSchema    string         `json:"response_schema,omitempty"`
	ResponseEnvelope  string         `json:"response_envelope,omitempty"`
}

// SchemaUsage is one reverse-direction use of a looked-up schema: the operation
// that accepts (request) or returns (response) it.
type SchemaUsage struct {
	OperationID string `json:"operation_id"`
	Path        string `json:"path"`
	Method      string `json:"method"`
	Role        string `json:"role"` // "request" | "response"
}

// LookupResult is the JSON shape returned by eidos/lookup.
type LookupResult struct {
	Valid       bool                 `json:"valid"`
	Diagnostics []api.DiagnosticJSON `json:"diagnostics"`
	Operation   *OperationLookup     `json:"operation"`
	SchemaUsage []SchemaUsage        `json:"schema_usage"`
}

// LookupTool returns the eidos/lookup MCP tool definition.
func LookupTool() *sdkmcp.Tool {
	return &sdkmcp.Tool{
		Name:        "eidos/lookup",
		Description: "Look up an OpenAPI operation by operationId (forward: path, method, path params, request/response schema names) and/or a schema by name (reverse: every operation that accepts it as a request body or returns it as a response). Reads the raw spec; generator.yaml overrides do not change operations or schemas, so config is not accepted.",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"spec":         {Type: "string", Description: "OpenAPI spec as inline JSON/YAML content, a local file path, a file:// URL, or an http(s):// URL (https-only; http requires EIDOS_SPEC_ALLOW_HTTP=1)"},
				"operation_id": {Type: "string", Description: "Operation ID to look up (forward direction). Optional, but at least one of operation_id/schema is required."},
				"schema":       {Type: "string", Description: "Schema name ($ref final segment) to look up (reverse direction). Optional, but at least one of operation_id/schema is required."},
			},
			Required: []string{"spec"},
		},
		OutputSchema: &jsonschema.Schema{
			Type:        "object",
			Description: "Result of the eidos/lookup tool call",
			Required:    []string{"valid", "diagnostics", "schema_usage"},
			Properties: map[string]*jsonschema.Schema{
				"valid":        {Type: "boolean"},
				"diagnostics":  {Type: "array"},
				"operation":    {Types: []string{"object", "null"}},
				"schema_usage": {Type: "array"},
			},
		},
	}
}

// HandleLookup implements eidos/lookup.
func HandleLookup(_ context.Context, _ *sdkmcp.CallToolRequest, args LookupArgs) (res *sdkmcp.CallToolResult, out LookupResult, err error) {
	defer recoverHandler("eidos/lookup", lookupErrorResult, &res, &out)
	result := LookupResult{
		Diagnostics: []api.DiagnosticJSON{},
		SchemaUsage: []SchemaUsage{},
	}

	if strings.TrimSpace(args.OperationID) == "" && strings.TrimSpace(args.Schema) == "" {
		result.Valid = false
		result.Diagnostics = append(result.Diagnostics, api.DiagnosticJSON{
			Severity: "error", Summary: "at least one of operation_id or schema is required",
		})
		out = result
		res, err = marshalToolResult(result)
		return res, out, err
	}

	specBytes, err := normalizeSpec(args.Spec)
	if err != nil {
		out = lookupErrorResult(err)
		res, err = marshalToolResult(out)
		return res, out, err
	}

	spec, parseDiags, err := api.ParseSpec(specBytes, "spec")
	if err != nil {
		out = lookupErrorResult(err)
		res, err = marshalToolResult(out)
		return res, out, err
	}
	result.Diagnostics = append(result.Diagnostics, apiDiags(parseDiags)...)
	pathOps, opDiags := transformer.OperationsFromSpecWithDiagnostics(spec)
	result.Diagnostics = append(result.Diagnostics, apiDiags(opDiags)...)
	result.Valid = !hasErrorDiags(result.Diagnostics)

	if opID := strings.TrimSpace(args.OperationID); opID != "" {
		result.Operation = lookupOperation(pathOps, opID)
		if result.Operation == nil {
			result.Diagnostics = append(result.Diagnostics, api.DiagnosticJSON{
				Severity: "warning", Summary: fmt.Sprintf("operation %q not found in spec", opID),
			})
		}
	}
	if schema := strings.TrimSpace(args.Schema); schema != "" {
		result.SchemaUsage = lookupSchemaUsage(pathOps, schema)
	}

	out = result
	res, err = marshalToolResult(result)
	return res, out, err
}

// lookupOperation scans pathOps deterministically for the first operation with
// the given operationId and returns its lookup summary, or nil if not found.
func lookupOperation(pathOps map[string]map[transformer.HTTPMethod]transformer.Operation, opID string) *OperationLookup {
	paths := sortedPathKeys(pathOps)
	for _, path := range paths {
		ops := pathOps[path]
		methods := sortedMethodKeys(ops)
		for _, method := range methods {
			op := ops[method]
			if op.OperationID == opID {
				return &OperationLookup{
					OperationID:       op.OperationID,
					Path:              op.Path,
					Method:            string(op.Method),
					PathParams:        pathParamSummaries(op),
					RequestBodySchema: schemaRefName(op.RequestSchema),
					RequestMediaType:  op.RequestMediaType,
					ResponseSchema:    schemaRefName(op.ResponseSchema),
					ResponseEnvelope:  op.ResponseEnvelope,
				}
			}
		}
	}
	return nil
}

// lookupSchemaUsage scans pathOps deterministically and returns every operation
// whose request body or response schema resolves to the given schema name.
func lookupSchemaUsage(pathOps map[string]map[transformer.HTTPMethod]transformer.Operation, schema string) []SchemaUsage {
	want := strings.TrimSpace(schema)
	usage := []SchemaUsage{}
	paths := sortedPathKeys(pathOps)
	for _, path := range paths {
		ops := pathOps[path]
		methods := sortedMethodKeys(ops)
		for _, method := range methods {
			op := ops[method]
			if op.RequestSchema != nil && op.RequestSchema.RefName == want {
				usage = append(usage, SchemaUsage{OperationID: op.OperationID, Path: op.Path, Method: string(op.Method), Role: "request"})
			}
			if op.ResponseSchema != nil && op.ResponseSchema.RefName == want {
				usage = append(usage, SchemaUsage{OperationID: op.OperationID, Path: op.Path, Method: string(op.Method), Role: "response"})
			}
		}
	}
	return usage
}

func pathParamSummaries(op transformer.Operation) []ParamSummary {
	out := []ParamSummary{}
	for _, p := range op.Parameters {
		if p.In != "path" {
			continue
		}
		out = append(out, ParamSummary{Name: p.Name, Required: p.Required, Type: p.Type})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func schemaRefName(s *transformer.SchemaSpec) string {
	if s == nil {
		return ""
	}
	return s.RefName
}

func sortedPathKeys(m map[string]map[transformer.HTTPMethod]transformer.Operation) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedMethodKeys(m map[transformer.HTTPMethod]transformer.Operation) []transformer.HTTPMethod {
	keys := make([]transformer.HTTPMethod, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return string(keys[i]) < string(keys[j]) })
	return keys
}

// apiDiags converts diagnostics.Diagnostics to the JSON diagnostic shape used by
// the MCP tools.
func apiDiags(ds diagnostics.Diagnostics) []api.DiagnosticJSON {
	out := make([]api.DiagnosticJSON, 0, len(ds))
	for _, d := range ds {
		out = append(out, api.DiagnosticJSON{
			Severity:       d.Severity.String(),
			Summary:        d.Summary,
			Detail:         d.Detail,
			SourceLocation: d.SourceLocation,
		})
	}
	return out
}

// hasErrorDiags reports whether a JSON diagnostic slice contains an error.
func hasErrorDiags(ds []api.DiagnosticJSON) bool {
	for _, d := range ds {
		if d.Severity == diagnostics.Error.String() {
			return true
		}
	}
	return false
}

// lookupErrorResult builds an eidos/lookup error/panic result with non-nil
// required arrays.
func lookupErrorResult(err error) LookupResult {
	return LookupResult{
		Valid:       false,
		Diagnostics: errorDiags(err),
		SchemaUsage: []SchemaUsage{},
	}
}
