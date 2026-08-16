package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"

	"github.com/signalbreak-labs/eidos/pkg/api"
	"github.com/signalbreak-labs/eidos/pkg/config"
	"github.com/signalbreak-labs/eidos/pkg/generator"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// These tools let an LLM drive the whole eidos workflow through the MCP server
// without codebase access: inspect what a spec yields (with CRUD completeness),
// run the generator, check generated schemas for framework-validity, and preview
// the effect of generator.yaml overrides.

// ---------------------------------------------------------------------------
// eidos/inspect — IR preview with per-resource CRUD completeness + wired status
// ---------------------------------------------------------------------------

// InspectArgs is the input to eidos/inspect.
type InspectArgs struct {
	Spec   string `json:"spec"`
	Config string `json:"config,omitempty"`
}

// InspectResult is the JSON shape returned by eidos/inspect.
type InspectResult struct {
	Valid       bool                 `json:"valid"`
	Diagnostics []api.DiagnosticJSON `json:"diagnostics"`
	Resources   []ResourceSummary    `json:"resources"`
	DataSources []EntitySummary      `json:"data_sources"`
	Actions     []EntitySummary      `json:"actions"`
	Ephemerals  []EntitySummary      `json:"ephemeral_resources"`
	Lists       []EntitySummary      `json:"list_resources"`
	Functions   []EntitySummary      `json:"functions"`
}

// ResourceSummary describes one managed resource and its CRUD wiring.
type ResourceSummary struct {
	Name     string `json:"name"`
	TypeName string `json:"type_name"`
	Wired    bool   `json:"wired"`
	Create   string `json:"create,omitempty"`
	Read     string `json:"read,omitempty"`
	Update   string `json:"update,omitempty"`
	Delete   string `json:"delete,omitempty"`
}

// EntitySummary describes a data source, action, ephemeral, list, or function.
type EntitySummary struct {
	Name     string `json:"name"`
	TypeName string `json:"type_name"`
	Wired    bool   `json:"wired"`
}

// InspectTool returns the eidos/inspect MCP tool definition.
func InspectTool() *sdkmcp.Tool {
	return &sdkmcp.Tool{
		Name:        "eidos/inspect",
		Description: "Parse an OpenAPI spec and report what eidos would generate: every resource with its CRUD mapping completeness and wired-vs-scaffolded status, plus data sources, actions, ephemeral resources, list resources, and functions. Use this to decide what is provisionable before generating.",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"spec":   {Type: "string", Description: "OpenAPI spec as inline JSON/YAML content, a local file path, a file:// URL, or an http(s):// URL (https-only; http requires EIDOS_SPEC_ALLOW_HTTP=1)"},
				"config": {Type: "string", Description: "Optional generator.yaml contents"},
			},
			Required: []string{"spec"},
		},
		OutputSchema: &jsonschema.Schema{
			Type:        "object",
			Description: "Result of the eidos/inspect tool call",
			Required:    []string{"valid", "diagnostics", "resources", "data_sources", "actions"},
			Properties: map[string]*jsonschema.Schema{
				"valid":        {Type: "boolean"},
				"diagnostics":  {Type: "array"},
				"resources":    {Type: "array"},
				"data_sources": {Type: "array"},
				"actions":      {Type: "array"},
			},
		},
	}
}

// HandleInspect implements eidos/inspect.
func HandleInspect(ctx context.Context, _ *sdkmcp.CallToolRequest, args InspectArgs) (res *sdkmcp.CallToolResult, out InspectResult, err error) {
	defer recoverHandler("eidos/inspect", inspectErrorResult, &res, &out)
	specBytes, err := normalizeSpec(args.Spec)
	if err != nil {
		out = inspectErrorResult(err)
		res, err = marshalToolResult(out)
		return res, out, err
	}
	resp := validateContext(ctx, specBytes)
	result := InspectResult{
		Valid:       resp.Valid,
		Diagnostics: nonNilDiags(resp.Diagnostics),
		Resources:   []ResourceSummary{},
		DataSources: []EntitySummary{},
		Actions:     []EntitySummary{},
		Ephemerals:  []EntitySummary{},
		Lists:       []EntitySummary{},
		Functions:   []EntitySummary{},
	}
	if resp.IRPreview != nil {
		result.Resources = summarizeResources(resp.IRPreview.Resources)
		result.DataSources = summarizeDataSources(resp.IRPreview.DataSources)
		result.Actions = summarizeActions(resp.IRPreview.Actions)
		result.Ephemerals = summarizeEphemerals(resp.IRPreview.EphemeralResources)
		result.Lists = summarizeLists(resp.IRPreview.ListResources)
		result.Functions = summarizeFunctions(resp.IRPreview.Functions)
	}
	out = result
	res, err = marshalToolResult(result)
	return res, out, err
}

// ---------------------------------------------------------------------------
// eidos/generate — run the pipeline and return a manifest (optionally write)
// ---------------------------------------------------------------------------

// GenerateArgs is the input to eidos/generate.
type GenerateArgs struct {
	Spec   string `json:"spec"`
	Config string `json:"config,omitempty"`
	Output string `json:"output,omitempty"`
}

// GenerateResult is the JSON shape returned by eidos/generate.
type GenerateResult struct {
	Valid       bool                 `json:"valid"`
	Diagnostics []api.DiagnosticJSON `json:"diagnostics"`
	Resources   []ResourceSummary    `json:"resources"`
	DataSources []EntitySummary      `json:"data_sources"`
	Actions     []EntitySummary      `json:"actions"`
	FileCount   int                  `json:"file_count"`
	OutputDir   string               `json:"output_dir,omitempty"`
}

// GenerateTool returns the eidos/generate MCP tool definition.
func GenerateTool() *sdkmcp.Tool {
	return &sdkmcp.Tool{
		Name:        "eidos/generate",
		Description: "Run the eidos generation pipeline on an OpenAPI spec and return a manifest of what was generated (resources with CRUD wiring, data sources, actions, file count). When output is set, the provider files are written to that directory.",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"spec":   {Type: "string", Description: "OpenAPI spec as inline JSON/YAML content, a local file path, a file:// URL, or an http(s):// URL (https-only; http requires EIDOS_SPEC_ALLOW_HTTP=1)"},
				"config": {Type: "string", Description: "Optional generator.yaml contents"},
				"output": {Type: "string", Description: "Optional directory to write the generated provider to"},
			},
			Required: []string{"spec"},
		},
		OutputSchema: &jsonschema.Schema{
			Type:        "object",
			Description: "Result of the eidos/generate tool call",
			Required:    []string{"valid", "diagnostics", "resources", "data_sources", "actions", "file_count"},
			Properties: map[string]*jsonschema.Schema{
				"valid":        {Type: "boolean"},
				"diagnostics":  {Type: "array"},
				"resources":    {Type: "array"},
				"data_sources": {Type: "array"},
				"actions":      {Type: "array"},
				"file_count":   {Type: "integer"},
				"output_dir":   {Type: "string"},
			},
		},
	}
}

// HandleGenerate implements eidos/generate.
func HandleGenerate(ctx context.Context, _ *sdkmcp.CallToolRequest, args GenerateArgs) (res *sdkmcp.CallToolResult, out GenerateResult, err error) {
	defer recoverHandler("eidos/generate", generateErrorResult, &res, &out)
	specBytes, err := normalizeSpec(args.Spec)
	if err != nil {
		out = generateErrorResult(err)
		res, err = marshalToolResult(out)
		return res, out, err
	}
	resp := validateContext(ctx, specBytes)
	result := GenerateResult{
		Valid:       resp.Valid,
		Diagnostics: nonNilDiags(resp.Diagnostics),
		Resources:   []ResourceSummary{},
		DataSources: []EntitySummary{},
		Actions:     []EntitySummary{},
	}
	if resp.IRPreview != nil {
		result.Resources = summarizeResources(resp.IRPreview.Resources)
		result.DataSources = summarizeDataSources(resp.IRPreview.DataSources)
		result.Actions = summarizeActions(resp.IRPreview.Actions)
	}
	if strings.TrimSpace(args.Output) != "" && resp.Valid && resp.IRPreview != nil {
		entries, err := writeProvider(args.Output, resp.IRPreview)
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, api.DiagnosticJSON{
				Severity: "error", Summary: "Provider generation failed", Detail: err.Error(),
			})
		} else {
			result.FileCount = len(entries)
			result.OutputDir = args.Output
		}
	}
	out = result
	res, err = marshalToolResult(result)
	return res, out, err
}

// ---------------------------------------------------------------------------
// eidos/validate-schemas — framework-validity of generated schemas
// ---------------------------------------------------------------------------

// ValidateSchemasArgs is the input to eidos/validate-schemas.
type ValidateSchemasArgs struct {
	Spec   string `json:"spec"`
	Config string `json:"config,omitempty"`
}

// SchemaIssue describes one framework-invalid schema problem.
type SchemaIssue struct {
	Entity string `json:"entity"`
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	Detail string `json:"detail"`
}

// ValidateSchemasResult is the JSON shape returned by eidos/validate-schemas.
type ValidateSchemasResult struct {
	Valid       bool                 `json:"valid"`
	Diagnostics []api.DiagnosticJSON `json:"diagnostics"`
	Issues      []SchemaIssue        `json:"issues"`
}

// ValidateSchemasTool returns the eidos/validate-schemas MCP tool definition.
func ValidateSchemasTool() *sdkmcp.Tool {
	return &sdkmcp.Tool{
		Name:        "eidos/validate-schemas",
		Description: "Report which generated resource/data source schemas terraform-plugin-framework would reject (dynamic-element collections, nested DynamicAttribute, invalid attribute names, Computed+Required, reserved root names), so an LLM can fix the spec/config before generating.",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"spec":   {Type: "string", Description: "OpenAPI spec as inline JSON/YAML content, a local file path, a file:// URL, or an http(s):// URL (https-only; http requires EIDOS_SPEC_ALLOW_HTTP=1)"},
				"config": {Type: "string", Description: "Optional generator.yaml contents"},
			},
			Required: []string{"spec"},
		},
		OutputSchema: &jsonschema.Schema{
			Type:        "object",
			Description: "Result of the eidos/validate-schemas tool call",
			Required:    []string{"valid", "diagnostics", "issues"},
			Properties: map[string]*jsonschema.Schema{
				"valid":       {Type: "boolean"},
				"diagnostics": {Type: "array"},
				"issues":      {Type: "array"},
			},
		},
	}
}

// HandleValidateSchemas implements eidos/validate-schemas.
func HandleValidateSchemas(ctx context.Context, _ *sdkmcp.CallToolRequest, args ValidateSchemasArgs) (res *sdkmcp.CallToolResult, out ValidateSchemasResult, err error) {
	defer recoverHandler("eidos/validate-schemas", validateSchemasErrorResult, &res, &out)
	specBytes, err := normalizeSpec(args.Spec)
	if err != nil {
		out = validateSchemasErrorResult(err)
		res, err = marshalToolResult(out)
		return res, out, err
	}
	resp := validateContext(ctx, specBytes)
	result := ValidateSchemasResult{
		Valid:       resp.Valid,
		Diagnostics: nonNilDiags(resp.Diagnostics),
		Issues:      []SchemaIssue{},
	}
	if resp.IRPreview != nil {
		for _, r := range resp.IRPreview.Resources {
			result.Issues = append(result.Issues, schemaIssues("resource "+r.Name, r.Schema)...)
		}
		for _, ds := range resp.IRPreview.DataSources {
			result.Issues = append(result.Issues, schemaIssues("data source "+ds.Name, ds.Schema)...)
		}
	}
	out = result
	res, err = marshalToolResult(result)
	return res, out, err
}

// ---------------------------------------------------------------------------
// eidos/override-preview — IR after overrides + per-override match report
// ---------------------------------------------------------------------------

// OverridePreviewArgs is the input to eidos/override-preview.
type OverridePreviewArgs struct {
	Spec   string `json:"spec"`
	Config string `json:"config"`
}

// OverrideReport describes one resource_override entry and whether it matched.
type OverrideReport struct {
	Operation string `json:"operation,omitempty"`
	Schema    string `json:"schema,omitempty"`
	Matched   bool   `json:"matched"`
	Note      string `json:"note,omitempty"`
}

// OverridePreviewResult is the JSON shape returned by eidos/override-preview.
type OverridePreviewResult struct {
	Valid       bool                 `json:"valid"`
	Diagnostics []api.DiagnosticJSON `json:"diagnostics"`
	Resources   []ResourceSummary    `json:"resources"`
	Overrides   []OverrideReport     `json:"overrides"`
}

// OverridePreviewTool returns the eidos/override-preview MCP tool definition.
func OverridePreviewTool() *sdkmcp.Tool {
	return &sdkmcp.Tool{
		Name:        "eidos/override-preview",
		Description: "Given an OpenAPI spec and a generator.yaml, return the IR preview after overrides plus a per-entry report of which resource_overrides matched and which had no effect (so silent no-ops are visible).",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"spec":   {Type: "string", Description: "OpenAPI spec as inline JSON/YAML content, a local file path, a file:// URL, or an http(s):// URL (https-only; http requires EIDOS_SPEC_ALLOW_HTTP=1)"},
				"config": {Type: "string", Description: "generator.yaml contents"},
			},
			Required: []string{"spec", "config"},
		},
		OutputSchema: &jsonschema.Schema{
			Type:        "object",
			Description: "Result of the eidos/override-preview tool call",
			Required:    []string{"valid", "diagnostics", "resources", "overrides"},
			Properties: map[string]*jsonschema.Schema{
				"valid":       {Type: "boolean"},
				"diagnostics": {Type: "array"},
				"resources":   {Type: "array"},
				"overrides":   {Type: "array"},
			},
		},
	}
}

// HandleOverridePreview implements eidos/override-preview.
func HandleOverridePreview(ctx context.Context, _ *sdkmcp.CallToolRequest, args OverridePreviewArgs) (res *sdkmcp.CallToolResult, out OverridePreviewResult, err error) {
	defer recoverHandler("eidos/override-preview", overridePreviewErrorResult, &res, &out)
	specBytes, err := normalizeSpec(args.Spec)
	if err != nil {
		out = overridePreviewErrorResult(err)
		res, err = marshalToolResult(out)
		return res, out, err
	}
	merged, err := mergeConfigIntoSpec(specBytes, args.Config)
	if err != nil {
		out = overridePreviewErrorResult(err)
		res, err = marshalToolResult(out)
		return res, out, err
	}
	resp := validateContext(ctx, merged)
	result := OverridePreviewResult{
		Valid:       resp.Valid,
		Diagnostics: nonNilDiags(resp.Diagnostics),
		Resources:   []ResourceSummary{},
		Overrides:   []OverrideReport{},
	}
	if resp.IRPreview != nil {
		result.Resources = summarizeResources(resp.IRPreview.Resources)
	}
	result.Overrides = reportOverrides(args.Config, resp.IRPreview)
	out = result
	res, err = marshalToolResult(result)
	return res, out, err
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func summarizeResources(resources []ir.ResourceIR) []ResourceSummary {
	out := make([]ResourceSummary, 0, len(resources))
	for _, r := range resources {
		s := ResourceSummary{Name: r.Name, TypeName: r.TypeName}
		if r.CRUDMapping.Create.PathTemplate != "" {
			s.Create = r.CRUDMapping.Create.Method + " " + r.CRUDMapping.Create.PathTemplate
		}
		if r.CRUDMapping.Read.PathTemplate != "" {
			s.Read = r.CRUDMapping.Read.Method + " " + r.CRUDMapping.Read.PathTemplate
		}
		if r.CRUDMapping.Update != nil && r.CRUDMapping.Update.PathTemplate != "" {
			s.Update = r.CRUDMapping.Update.Method + " " + r.CRUDMapping.Update.PathTemplate
		}
		if r.CRUDMapping.Delete.PathTemplate != "" {
			s.Delete = r.CRUDMapping.Delete.Method + " " + r.CRUDMapping.Delete.PathTemplate
		}
		s.Wired = s.Create != "" && s.Read != "" && s.Delete != ""
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func summarizeDataSources(ds []ir.DataSourceIR) []EntitySummary {
	out := make([]EntitySummary, 0, len(ds))
	for _, d := range ds {
		out = append(out, EntitySummary{Name: d.Name, TypeName: d.TypeName, Wired: d.ReadMapping.PathTemplate != ""})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func summarizeActions(actions []ir.ActionIR) []EntitySummary {
	out := make([]EntitySummary, 0, len(actions))
	for _, a := range actions {
		out = append(out, EntitySummary{Name: a.Name, TypeName: a.TypeName, Wired: a.InvokeMapping.PathTemplate != ""})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func summarizeEphemerals(ers []ir.EphemeralResourceIR) []EntitySummary {
	out := make([]EntitySummary, 0, len(ers))
	for _, e := range ers {
		out = append(out, EntitySummary{Name: e.Name, TypeName: e.TypeName, Wired: e.OpenMapping.PathTemplate != ""})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func summarizeLists(lrs []ir.ListResourceIR) []EntitySummary {
	out := make([]EntitySummary, 0, len(lrs))
	for _, l := range lrs {
		out = append(out, EntitySummary{Name: l.Name, TypeName: l.TypeName, Wired: l.ListMapping.PathTemplate != ""})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func summarizeFunctions(fns []ir.FunctionIR) []EntitySummary {
	out := make([]EntitySummary, 0, len(fns))
	for _, f := range fns {
		out = append(out, EntitySummary{Name: f.Name, TypeName: f.TypeName, Wired: f.SourceOperation != ""})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

var validTFName = regexp.MustCompile(`^[a-z0-9_]+$`)

var reservedRootNames = map[string]bool{
	"provider": true, "provisioner": true, "connection": true,
	"count": true, "depends_on": true, "for_each": true, "lifecycle": true,
}

// schemaIssues walks an object schema and reports framework-invalid attribute
// shapes (G12/G14/G15). Each root attribute is checked for its own
// attribute-level violations (invalid identifier, reserved root name,
// Computed+Required) at depth 0, then its schema is walked for nested shapes.
func schemaIssues(entity string, schema ir.ObjectSchemaIR) []SchemaIssue {
	issues := make([]SchemaIssue, 0, len(schema.Attributes))
	for _, a := range schema.Attributes {
		issues = append(issues, attributeSchemaIssues(entity, a, a.Name, 0)...)
		walkSchemaIssues(&issues, entity, a.Schema, a.Name, 0)
	}
	return issues
}

// walkSchemaIssues recursively visits a schema, appending framework-invalid
// attribute shapes. depth counts how many collection/attribute levels the node
// sits below the resource root (a Dynamic attribute is only invalid nested).
func walkSchemaIssues(issues *[]SchemaIssue, entity string, s ir.SchemaIR, path string, depth int) {
	if s.Collection != nil {
		*issues = append(*issues, collectionSchemaIssues(entity, s, path)...)
		walkSchemaIssues(issues, entity, s.Collection.ElementType, path, depth+1)
	}
	if s.Union != nil {
		for _, v := range s.Union.Variants {
			walkSchemaIssues(issues, entity, v, path, depth+1)
		}
	}
	for _, a := range s.Attributes {
		ap := path + "." + a.Name
		*issues = append(*issues, attributeSchemaIssues(entity, a, ap, depth)...)
		walkSchemaIssues(issues, entity, a.Schema, ap, depth+1)
	}
}

// collectionSchemaIssues reports collection shapes terraform-plugin-framework
// rejects: a dynamic element type and a nested collection.
func collectionSchemaIssues(entity string, s ir.SchemaIR, path string) []SchemaIssue {
	elem := s.Collection.ElementType
	if elem.Type == ir.TypeDynamic || elem.Type == ir.TypeNull {
		return []SchemaIssue{{entity, "dynamic-element-collection", path,
			"collection with a dynamic element type is rejected by terraform-plugin-framework"}}
	}
	if elem.Collection != nil {
		return []SchemaIssue{{entity, "nested-collection", path,
			"nested collections are not representable in terraform-plugin-framework"}}
	}
	return nil
}

// attributeSchemaIssues reports the attribute-level framework-invalid shapes.
func attributeSchemaIssues(entity string, a ir.AttributeIR, ap string, depth int) []SchemaIssue {
	var issues []SchemaIssue
	if !validTFName.MatchString(a.Name) {
		issues = append(issues, SchemaIssue{entity, "invalid-attribute-name", ap,
			fmt.Sprintf("attribute name %q is not a valid Terraform identifier", a.Name)})
	}
	if reservedRootNames[a.Name] {
		issues = append(issues, SchemaIssue{entity, "reserved-root-name", ap,
			fmt.Sprintf("attribute name %q is a reserved Terraform root name", a.Name)})
	}
	if a.Required && a.Computed {
		issues = append(issues, SchemaIssue{entity, "computed-and-required", ap,
			"attribute cannot be both Computed and Required"})
	}
	if a.Schema.Type == ir.TypeDynamic && depth >= 1 {
		issues = append(issues, SchemaIssue{entity, "nested-dynamic", ap,
			"DynamicAttribute nested inside a collection is rejected by terraform-plugin-framework"})
	}
	return issues
}

// reportOverrides reports which resource_overrides entries matched a generated
// resource (by operation or schema name).
func reportOverrides(configYAML string, preview *ir.ProviderIR) []OverrideReport {
	reports := make([]OverrideReport, 0)
	cfg, err := config.LoadBytes([]byte(configYAML))
	if err != nil {
		return reports
	}
	matched := func(ro config.ResourceOverride) bool {
		for _, r := range preview.Resources {
			if ro.Operation != "" && strings.EqualFold(strings.ReplaceAll(r.SourceOperation, "_", ""), strings.ReplaceAll(ro.Operation, "_", "")) {
				return true
			}
			if ro.Schema != "" && strings.EqualFold(strings.ReplaceAll(r.Name, "_", ""), strings.ReplaceAll(ro.Schema, "_", "")) {
				return true
			}
		}
		return false
	}
	for _, ro := range cfg.ResourceOverrides {
		rep := OverrideReport{Operation: ro.Operation, Schema: ro.Schema, Matched: matched(ro)}
		if !rep.Matched {
			rep.Note = "no generated resource matched this override; if the operation was inferred as an action, set generate_resource with explicit create/read/update/delete operations (G8)"
		}
		reports = append(reports, rep)
	}
	return reports
}

// mergeConfigIntoSpec injects a generator.yaml config string into a spec body
// the way the HTTP validate handler expects (a top-level "config" field). The
// spec may be JSON or YAML; the merged body is re-serialized as JSON.
func mergeConfigIntoSpec(specBytes []byte, configYAML string) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(specBytes, &doc); err != nil {
		if err2 := yaml.Unmarshal(specBytes, &doc); err2 != nil {
			return nil, fmt.Errorf("spec must be JSON or YAML: %w", errors.Join(err, err2))
		}
	}
	doc["config"] = configYAML
	return json.Marshal(doc)
}

// writeProvider runs the generator in write mode into dir and returns the
// planned file entries.
func writeProvider(dir string, pir *ir.ProviderIR) ([]generator.FileEntry, error) {
	return generator.Run(pir, generator.Options{
		Mode:           generator.ModeWrite,
		OutputDir:      dir,
		Force:          true,
		CollectOptions: generator.CollectOptions{IncludeBuild: true},
	})
}

// nonNilDiags returns d if non-nil, else a non-nil empty slice, so tool outputs
// never serialize a diagnostics array as JSON null. The MCP SDK validates tool
// output against the declared OutputSchema, which requires array fields to be
// arrays (not null); a nil Go slice marshals to null and is rejected.
func nonNilDiags(d []api.DiagnosticJSON) []api.DiagnosticJSON {
	if d == nil {
		return []api.DiagnosticJSON{}
	}
	return d
}

// errorDiags wraps a single error as an error-severity diagnostic slice. It is
// the shared building block for the per-tool error-result constructors, which
// the error and panic paths return as the structured tool output.
func errorDiags(err error) []api.DiagnosticJSON {
	return []api.DiagnosticJSON{{Severity: "error", Summary: err.Error()}}
}

// inspectErrorResult builds an eidos/inspect result for an error or panic path.
// Every array field the tool's OutputSchema requires is a non-nil empty slice so
// the SDK's output-schema validation passes; a zero-value InspectResult{} would
// marshal null arrays and be rejected with "has type null, want array".
func inspectErrorResult(err error) InspectResult {
	return InspectResult{
		Valid:       false,
		Diagnostics: errorDiags(err),
		Resources:   []ResourceSummary{},
		DataSources: []EntitySummary{},
		Actions:     []EntitySummary{},
		Ephemerals:  []EntitySummary{},
		Lists:       []EntitySummary{},
		Functions:   []EntitySummary{},
	}
}

// generateErrorResult builds an eidos/generate error/panic result. See
// inspectErrorResult for why every required array field is non-nil.
func generateErrorResult(err error) GenerateResult {
	return GenerateResult{
		Valid:       false,
		Diagnostics: errorDiags(err),
		Resources:   []ResourceSummary{},
		DataSources: []EntitySummary{},
		Actions:     []EntitySummary{},
	}
}

// validateSchemasErrorResult builds an eidos/validate-schemas error/panic result.
func validateSchemasErrorResult(err error) ValidateSchemasResult {
	return ValidateSchemasResult{
		Valid:       false,
		Diagnostics: errorDiags(err),
		Issues:      []SchemaIssue{},
	}
}

// overridePreviewErrorResult builds an eidos/override-preview error/panic result.
func overridePreviewErrorResult(err error) OverridePreviewResult {
	return OverridePreviewResult{
		Valid:       false,
		Diagnostics: errorDiags(err),
		Resources:   []ResourceSummary{},
		Overrides:   []OverrideReport{},
	}
}

// marshalToolResult serializes a result into an MCP text content result.
func marshalToolResult(result any) (*sdkmcp.CallToolResult, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tool result: %w", err)
	}
	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(data)}},
	}, nil
}

// recoverHandler is deferred by each eidos/* tool handler to convert a panic
// into a valid, schema-conformant structured result. It mirrors the named-return
// recovery pattern in HandleGenerateConfig: a panic is logged with a stack
// trace server-side, and the named returns are set to an error result whose
// required array fields are non-nil empty slices — so the SDK's output-schema
// validation does not reject the recovered output as "type null, want array".
//
// emptyResult builds the typed zero-empty result for the panicking tool; res and
// out are pointers to the handler's named returns. err is intentionally not a
// parameter: a recovered panic is represented as a diagnostic inside the
// structured result, not a protocol error, so the client receives the validated
// output. If marshalToolResult fails during recovery (essentially impossible for
// these plain-data structs), res is left nil and the SDK synthesizes an empty
// CallToolResult while still sending the structured output.
func recoverHandler[T any](name string, emptyResult func(error) T, res **sdkmcp.CallToolResult, out *T) {
	if rec := recover(); rec != nil {
		log.Printf("panic in %s handler: %v\n%s", name, rec, debug.Stack())
		*out = emptyResult(fmt.Errorf("panic in %s handler: %v", name, rec))
		r, mErr := marshalToolResult(*out)
		if mErr != nil {
			log.Printf("marshal failed during %s panic recovery: %v", name, mErr)
			return
		}
		*res = r
	}
}
