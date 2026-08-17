package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/signalbreak-labs/eidos/pkg/api"
	"github.com/signalbreak-labs/eidos/pkg/config"
	"github.com/signalbreak-labs/eidos/pkg/ir"
	"github.com/signalbreak-labs/eidos/pkg/transformer"
)

// ---------------------------------------------------------------------------
// eidos/suggest-resources — propose non-inferred CRUD groupings as overrides
// ---------------------------------------------------------------------------

// SuggestResourcesArgs is the input to eidos/suggest-resources.
type SuggestResourcesArgs struct {
	Spec   string `json:"spec"`
	Config string `json:"config,omitempty"`
}

// Suggestion describes one CRUD grouping that CRUD inference dropped (typically
// because the instance path has no DELETE-method delete) and a ready-to-paste
// resource_overrides entry that would promote it to a wired managed resource.
type Suggestion struct {
	ResourceName    string `json:"resource_name"`
	CollectionPath  string `json:"collection_path"`
	InstancePath    string `json:"instance_path"`
	CreateOperation string `json:"create_operation"`
	ReadOperation   string `json:"read_operation"`
	UpdateOperation string `json:"update_operation,omitempty"`
	DeleteOperation string `json:"delete_operation,omitempty"`
	DeleteViaAction bool   `json:"delete_via_action"`
	Completeness    string `json:"completeness"`
	Reason          string `json:"reason"`
	OverrideYAML    string `json:"override_yaml"`
}

// SuggestResourcesResult is the JSON shape returned by eidos/suggest-resources.
type SuggestResourcesResult struct {
	Valid       bool                 `json:"valid"`
	Diagnostics []api.DiagnosticJSON `json:"diagnostics"`
	Suggestions []Suggestion         `json:"suggestions"`
}

// SuggestResourcesTool returns the eidos/suggest-resources MCP tool definition.
func SuggestResourcesTool() *sdkmcp.Tool {
	return &sdkmcp.Tool{
		Name:        "eidos/suggest-resources",
		Description: "Propose OpenAPI CRUD groupings that Terraform resource inference dropped (a collection POST + instance GET with no DELETE-method delete on the instance) as ready-to-paste resource_overrides entries. Scans for a near-miss delete — a non-DELETE verb operation on a sub-path of the instance (e.g. POST /my/ships/{id}/scrap, operationId scrap-ship) — and wires it as delete_operation with delete_via_action=true. A config declaring the resource suppresses its suggestion. Output is deterministic (sorted by resource name).",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"spec":   {Type: "string", Description: "OpenAPI spec as inline JSON/YAML content, a local file path, a file:// URL, or an http(s):// URL (https-only; http requires EIDOS_SPEC_ALLOW_HTTP=1)"},
				"config": {Type: "string", Description: "Optional generator.yaml content. Resources already declared here (inferred or via resource_overrides) are excluded from suggestions, and use_put_as_create is honored."},
			},
			Required: []string{"spec"},
		},
		OutputSchema: &jsonschema.Schema{
			Type:        "object",
			Description: "Result of the eidos/suggest-resources tool call",
			Required:    []string{"valid", "diagnostics", "suggestions"},
			Properties: map[string]*jsonschema.Schema{
				"valid":       {Type: "boolean"},
				"diagnostics": {Type: "array"},
				"suggestions": {Type: "array"},
			},
		},
	}
}

// HandleSuggestResources implements eidos/suggest-resources.
func HandleSuggestResources(ctx context.Context, _ *sdkmcp.CallToolRequest, args SuggestResourcesArgs) (res *sdkmcp.CallToolResult, out SuggestResourcesResult, err error) {
	defer recoverHandler("eidos/suggest-resources", suggestResourcesErrorResult, &res, &out)
	result := SuggestResourcesResult{
		Diagnostics: []api.DiagnosticJSON{},
		Suggestions: []Suggestion{},
	}

	specBytes, err := normalizeSpec(args.Spec)
	if err != nil {
		out = suggestResourcesErrorResult(err)
		res, err = marshalToolResult(out)
		return res, out, err
	}

	// Merge config into a copy for the config-aware IR preview (consumed set).
	// The raw pathOps come from the un-merged spec so overrides never change the
	// operations/schemas we group.
	mergedBytes, err := mergeConfigIntoSpec(specBytes, args.Config)
	if err != nil {
		out = suggestResourcesErrorResult(err)
		res, err = marshalToolResult(out)
		return res, out, err
	}
	resp := validateContext(ctx, mergedBytes)
	result.Diagnostics = append(result.Diagnostics, nonNilDiags(resp.Diagnostics)...)

	// usePutAsCreate mirrors the real pipeline so candidate grouping matches
	// what was (or was not) inferred.
	usePutAsCreate := true
	if strings.TrimSpace(args.Config) != "" {
		if cfg, cfgErr := config.LoadBytes([]byte(args.Config)); cfgErr == nil && cfg != nil {
			usePutAsCreate = cfg.UsePutAsCreate == nil || *cfg.UsePutAsCreate
		} else if cfgErr != nil {
			result.Diagnostics = append(result.Diagnostics, api.DiagnosticJSON{
				Severity: "warning", Summary: "could not parse config; use_put_as_create defaulted to true", Detail: cfgErr.Error(),
			})
		}
	}

	spec, parseDiags, err := api.ParseSpec(specBytes, "spec")
	if err != nil {
		out = suggestResourcesErrorResult(err)
		res, err = marshalToolResult(out)
		return res, out, err
	}
	result.Diagnostics = append(result.Diagnostics, apiDiags(parseDiags)...)
	pathOps, opDiags := transformer.OperationsFromSpecWithDiagnostics(spec)
	result.Diagnostics = append(result.Diagnostics, apiDiags(opDiags)...)

	consumed := consumedFromPreview(resp.IRPreview)
	groups := transformer.InferResourceCRUD(pathOps, usePutAsCreate)

	for _, g := range groups {
		if g.Create == nil || g.Read == nil || g.Delete != nil {
			continue // only the inference gap: create+read present, no DELETE-method delete
		}
		createKey := opKey(string(g.Create.Method), g.Create.Path)
		if consumed[createKey] {
			continue // already a resource (inferred or overridden); don't re-propose
		}
		result.Suggestions = append(result.Suggestions, buildSuggestion(g, pathOps))
	}

	// InferResourceCRUD already sorts by name, but re-sort defensively.
	sort.Slice(result.Suggestions, func(i, j int) bool {
		return result.Suggestions[i].ResourceName < result.Suggestions[j].ResourceName
	})

	result.Valid = !hasErrorDiags(result.Diagnostics)
	out = result
	res, err = marshalToolResult(result)
	return res, out, err
}

// buildSuggestion turns one dropped CRUD group into a Suggestion with a
// ready-to-paste override, searching pathOps for a near-miss delete.
func buildSuggestion(g transformer.ResourceCRUD, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation) Suggestion {
	name := transformer.ToSnakeCase(g.Name)
	createID := g.Create.OperationID
	readID := g.Read.OperationID

	parts := []string{"create", "read"}
	if g.Update != nil {
		parts = append(parts, "update")
	}

	near := findNearMissDelete(g, pathOps)
	reason := fmt.Sprintf(
		"no DELETE-method delete on %s and no verb-like delete operation detected; the resource would have a scaffolded delete",
		g.InstancePath)
	if near != nil {
		parts = append(parts, "delete")
		reason = fmt.Sprintf(
			"no DELETE-method delete on %s; %s (%s) proposed as delete_operation (delete via action)",
			g.InstancePath, near.OperationID, string(near.Method))
	}
	completeness := strings.Join(parts, "+")

	s := Suggestion{
		ResourceName:    name,
		CollectionPath:  g.CollectionPath,
		InstancePath:    g.InstancePath,
		CreateOperation: createID,
		ReadOperation:   readID,
		Completeness:    completeness,
		Reason:          reason,
	}
	if g.Update != nil {
		s.UpdateOperation = g.Update.OperationID
	}
	if near != nil {
		s.DeleteOperation = near.OperationID
		s.DeleteViaAction = string(near.Method) != "DELETE"
	}
	s.OverrideYAML = buildOverrideYAML(s)
	return s
}

// findNearMissDelete searches pathOps for an operation that can serve as the
// group's delete even though it is not a DELETE on the instance path. It matches
// either a sub-path of the instance whose trailing static segment is a delete
// verb (POST /my/ships/{id}/scrap), or any operation whose operationId leads
// with a delete verb, excluding the group's own create/read/update ops. The
// result is the first match in deterministic (path, method) order, or nil.
func findNearMissDelete(g transformer.ResourceCRUD, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation) *transformer.Operation {
	own := map[string]bool{
		g.Create.OperationID: true,
		g.Read.OperationID:   true,
	}
	if g.Update != nil {
		own[g.Update.OperationID] = true
	}

	var best *transformer.Operation
	paths := sortedPathKeys(pathOps)
	for _, path := range paths {
		ops := pathOps[path]
		methods := sortedMethodKeys(ops)
		for _, method := range methods {
			op := ops[method]
			if op.OperationID == "" || own[op.OperationID] {
				continue
			}
			if !isNearMissDelete(op, g.InstancePath) {
				continue
			}
			best = &op
			break
		}
		if best != nil {
			break
		}
	}
	return best
}

// isNearMissDelete reports whether op can serve as a delete for a resource
// whose instance path is instancePath. It requires either a trailing static
// verb segment on a sub-path of the instance, or a delete-verb-led operationId,
// so it does not greedily claim unrelated operations.
func isNearMissDelete(op transformer.Operation, instancePath string) bool {
	verb := leadingVerb(op.OperationID)
	deleteVerb := isDeleteVerb(verb)
	if op.Path == instancePath {
		// Same path: only a delete-verb operationId qualifies (e.g. a POST
		// /my/ships/{id} whose operationId is retireShip).
		return deleteVerb
	}
	if !strings.HasPrefix(op.Path, instancePath+"/") {
		return false
	}
	remainder := op.Path[len(instancePath)+1:]
	if strings.Contains(remainder, "/") || strings.HasPrefix(remainder, "{") {
		return false // multi-segment or parameterized tail is not a clean verb
	}
	// Trailing static verb segment (e.g. "scrap") or a delete-verb operationId.
	return isDeleteVerb(remainder) || deleteVerb
}

// leadingVerb extracts the leading word of an operationId, lowercased. It
// handles camelCase (scrapShip -> scrap), snake/kebab case (scrap_ship,
// scrap-ship -> scrap), and dotted names by taking the leading run of lowercase
// letters/digits up to the first uppercase letter or separator.
func leadingVerb(operationID string) string {
	if operationID == "" {
		return ""
	}
	for i, r := range operationID {
		if !isLowerAlphaNum(r) {
			if i == 0 {
				// Leading separator or uppercase: fall back to the first
				// separator-delimited token, else the whole id lowercased.
				if j := strings.IndexAny(operationID, "-_."); j > 0 {
					return strings.ToLower(operationID[:j])
				}
				return strings.ToLower(operationID)
			}
			return operationID[:i]
		}
	}
	return operationID
}

// isLowerAlphaNum reports whether r is a lowercase ASCII letter or digit, the
// characters that make up a camelCase verb's leading run.
func isLowerAlphaNum(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
}

// isDeleteVerb reports whether v is a verb that semantically deletes/retires a
// resource, so a non-DELETE operation with this verb can stand in for a delete.
func isDeleteVerb(v string) bool {
	switch v {
	case "delete", "scrap", "remove", "destroy", "purge", "cancel", "terminate",
		"retire", "archive", "trash", "dismiss", "close", "shutdown", "deactivate",
		"revoke", "drop", "wipe", "abandon", "expire":
		return true
	}
	return false
}

// buildOverrideYAML renders a ready-to-paste resource_overrides entry for s.
// `operation` is the required seed/match operation (the create); the explicit
// read/update/delete operations wire the rest. The create is resolved from the
// seed, so create_operation is omitted to avoid a redundant ignored field.
func buildOverrideYAML(s Suggestion) string {
	var b strings.Builder
	b.WriteString("resource_overrides:\n  - operation: ")
	b.WriteString(s.CreateOperation)
	b.WriteString("\n    generate_resource: true\n    resource_name: ")
	b.WriteString(s.ResourceName)
	b.WriteString("\n    read_operation: ")
	b.WriteString(s.ReadOperation)
	if s.UpdateOperation != "" {
		b.WriteString("\n    update_operation: ")
		b.WriteString(s.UpdateOperation)
	}
	if s.DeleteOperation != "" {
		b.WriteString("\n    delete_operation: ")
		b.WriteString(s.DeleteOperation)
	}
	b.WriteString("\n")
	return b.String()
}

// consumedFromPreview builds the set of "METHOD path" pairs already claimed by
// resources in the IR preview (inferred + override-created), so suggest never
// re-proposes an existing resource. Only resource CRUD mappings are counted —
// not actions/data sources — because the create op of a dropped group becomes an
// action when the group is dropped, and counting that action would wrongly
// suppress the suggestion.
func consumedFromPreview(preview *ir.ProviderIR) map[string]bool {
	consumed := map[string]bool{}
	if preview == nil {
		return consumed
	}
	for _, r := range preview.Resources {
		addConsumed(consumed, r.CRUDMapping.Create.Method, r.CRUDMapping.Create.PathTemplate)
		addConsumed(consumed, r.CRUDMapping.Read.Method, r.CRUDMapping.Read.PathTemplate)
		if r.CRUDMapping.Update != nil {
			addConsumed(consumed, r.CRUDMapping.Update.Method, r.CRUDMapping.Update.PathTemplate)
		}
		addConsumed(consumed, r.CRUDMapping.Delete.Method, r.CRUDMapping.Delete.PathTemplate)
	}
	return consumed
}

func addConsumed(consumed map[string]bool, method, path string) {
	if method == "" || path == "" {
		return
	}
	consumed[opKey(method, path)] = true
}

func opKey(method, path string) string { return method + " " + path }

// suggestResourcesErrorResult builds an eidos/suggest-resources error/panic
// result with non-nil required arrays.
func suggestResourcesErrorResult(err error) SuggestResourcesResult {
	return SuggestResourcesResult{
		Valid:       false,
		Diagnostics: errorDiags(err),
		Suggestions: []Suggestion{},
	}
}
