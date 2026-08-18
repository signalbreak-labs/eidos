package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"
	"time"

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
	Counts      InspectCounts        `json:"counts"`
}

// InspectCounts surfaces reliable, explicit construct counts derived from the
// config-aware IR preview so a caller does not have to infer them from array
// lengths. WiredResources/ScaffoldedResources split the managed-resource count
// by whether the full Create+Read+Delete mapping is wired.
type InspectCounts struct {
	Resources           int `json:"resources"`
	DataSources         int `json:"data_sources"`
	Actions             int `json:"actions"`
	EphemeralResources  int `json:"ephemeral_resources"`
	ListResources       int `json:"list_resources"`
	Functions           int `json:"functions"`
	WiredResources      int `json:"wired_resources"`
	ScaffoldedResources int `json:"scaffolded_resources"`
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
				"config": {Type: "string", Description: "Optional generator.yaml as inline YAML/JSON content, a local file path, or a file:// URL. When set, overrides (e.g. generate_resource) shape the IR preview."},
			},
			Required: []string{"spec"},
		},
		OutputSchema: &jsonschema.Schema{
			Type:        "object",
			Description: "Result of the eidos/inspect tool call",
			Required:    []string{"valid", "diagnostics", "resources", "data_sources", "actions", "counts"},
			Properties: map[string]*jsonschema.Schema{
				"valid":        {Type: "boolean"},
				"diagnostics":  {Type: "array"},
				"resources":    {Type: "array"},
				"data_sources": {Type: "array"},
				"actions":      {Type: "array"},
				"counts":       {Type: "object"},
			},
		},
	}
}

// HandleInspect implements eidos/inspect.
func HandleInspect(ctx context.Context, _ *sdkmcp.CallToolRequest, args InspectArgs) (res *sdkmcp.CallToolResult, out InspectResult, err error) {
	defer recoverHandler("eidos/inspect", inspectErrorResult, &res, &out)
	specBytes, err := normalizeSpec(ctx, args.Spec)
	if err != nil {
		out = inspectErrorResult(err)
		res, err = marshalToolResult(out)
		return res, out, err
	}
	// Thread the optional generator.yaml through the pipeline so overrides
	// (e.g. generate_resource) shape the IR preview. Without this, inspect
	// reports a spec-only view and silently ignores the declared `config`
	// input (M-73). The config may be inline content, a local file path, or a
	// file:// URL (M-76).
	configYAML, err := normalizeConfig(ctx, args.Config)
	if err != nil {
		out = inspectErrorResult(err)
		res, err = marshalToolResult(out)
		return res, out, err
	}
	specBytes, err = mergeConfigIntoSpec(specBytes, configYAML)
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
	result.Counts = countConstructs(result)
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
	// DryRun, when true, collects and returns the planned file list without
	// writing anything to disk. When output is also set, the result additionally
	// reports which planned files would overwrite an existing file and which
	// files already in output would be left stale (not regenerated).
	DryRun bool `json:"dry_run,omitempty"`
	// Verify, when true and output is set (non-dry-run), runs `go mod tidy` +
	// `go build ./...` in the output directory after writing and reports whether
	// the generated provider compiles. Ignored in dry-run mode.
	Verify bool `json:"verify,omitempty"`
	// Force, when true and output is set (non-dry-run), overwrites existing files
	// in the output directory. Defaults to false so a write never silently
	// clobbers a hand-edited provider directory — mirroring the CLI's --force
	// (N-52). Without it, a write that would overwrite existing files fails loud
	// with the same refusal as the CLI.
	Force bool `json:"force,omitempty"`
}

// UnmarshalJSON accepts both the schema's snake_case "dry_run" key and the
// camelCase "dryRun" key that some MCP clients (notably LLMs) send despite the
// input schema. Without this, a camelCase call leaves DryRun at its zero value,
// silently selects write mode, and creates a full provider tree on disk — a
// destructive footgun for a tool whose caller expects a no-op plan (M-75).
func (a *GenerateArgs) UnmarshalJSON(data []byte) error {
	var raw struct {
		Spec        string `json:"spec"`
		Config      string `json:"config,omitempty"`
		Output      string `json:"output,omitempty"`
		DryRun      bool   `json:"dry_run,omitempty"`
		DryRunCamel bool   `json:"dryRun,omitempty"`
		Verify      bool   `json:"verify,omitempty"`
		Force       bool   `json:"force,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	a.Spec = raw.Spec
	a.Config = raw.Config
	a.Output = raw.Output
	a.DryRun = raw.DryRun || raw.DryRunCamel
	a.Verify = raw.Verify
	a.Force = raw.Force
	return nil
}

// FileSummary describes one planned/written file.
type FileSummary struct {
	Path           string `json:"path"`
	Reason         string `json:"reason,omitempty"`
	WouldOverwrite bool   `json:"would_overwrite,omitempty"`
}

// GenerateResult is the JSON shape returned by eidos/generate.
type GenerateResult struct {
	Valid        bool                 `json:"valid"`
	Diagnostics  []api.DiagnosticJSON `json:"diagnostics"`
	Resources    []ResourceSummary    `json:"resources"`
	DataSources  []EntitySummary      `json:"data_sources"`
	Actions      []EntitySummary      `json:"actions"`
	Files        []FileSummary        `json:"files"`
	StaleFiles   []string             `json:"stale_files"`
	FileCount    int                  `json:"file_count"`
	OutputDir    string               `json:"output_dir,omitempty"`
	VerifyOK     bool                 `json:"verify_ok"`
	VerifyOutput string               `json:"verify_output,omitempty"`
}

// GenerateTool returns the eidos/generate MCP tool definition.
func GenerateTool() *sdkmcp.Tool {
	return &sdkmcp.Tool{
		Name:        "eidos/generate",
		Description: "Run the eidos generation pipeline on an OpenAPI spec and return a manifest of what was generated (resources with CRUD wiring, data sources, actions, file list). When output is set, the provider files are written to that directory. dry_run returns the planned file list without writing (plus overwrite/stale analysis when output is set). verify runs `go build ./...` in output after writing.",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"spec":    {Type: "string", Description: "OpenAPI spec as inline JSON/YAML content, a local file path, a file:// URL, or an http(s):// URL (https-only; http requires EIDOS_SPEC_ALLOW_HTTP=1)"},
				"config":  {Type: "string", Description: "Optional generator.yaml as inline YAML/JSON content, a local file path, or a file:// URL. When set, overrides shape the generated provider and summaries."},
				"output":  {Type: "string", Description: "Optional directory to write the generated provider to"},
				"dry_run": {Type: "boolean", Description: "Collect and return the planned file list without writing. When output is set, also reports would-overwrite and stale files."},
				"verify":  {Type: "boolean", Description: "After writing (non-dry-run), run `go build ./...` in output and report whether the generated provider compiles. Verify runs synchronously inside the tool call and is bounded by a 5-minute timeout; the MCP server serializes handler invocation, so no other tool call is serviced while it runs (N-61)."},
				"force":   {Type: "boolean", Description: "Overwrite existing files in output (default false). Without force, a write that would overwrite an existing file fails with a diagnostic, like the CLI's --force."},
			},
			Required: []string{"spec"},
		},
		OutputSchema: &jsonschema.Schema{
			Type:        "object",
			Description: "Result of the eidos/generate tool call",
			Required:    []string{"valid", "diagnostics", "resources", "data_sources", "actions", "file_count", "files", "stale_files", "verify_ok"},
			Properties: map[string]*jsonschema.Schema{
				"valid":         {Type: "boolean"},
				"diagnostics":   {Type: "array"},
				"resources":     {Type: "array"},
				"data_sources":  {Type: "array"},
				"actions":       {Type: "array"},
				"file_count":    {Type: "integer"},
				"files":         {Type: "array"},
				"stale_files":   {Type: "array"},
				"output_dir":    {Type: "string"},
				"verify_ok":     {Type: "boolean"},
				"verify_output": {Type: "string"},
			},
		},
	}
}

// HandleGenerate implements eidos/generate.
func HandleGenerate(ctx context.Context, _ *sdkmcp.CallToolRequest, args GenerateArgs) (res *sdkmcp.CallToolResult, out GenerateResult, err error) {
	defer recoverHandler("eidos/generate", generateErrorResult, &res, &out)
	specBytes, err := normalizeSpec(ctx, args.Spec)
	if err != nil {
		out = generateErrorResult(err)
		res, err = marshalToolResult(out)
		return res, out, err
	}
	// Honor the optional generator.yaml so resource/action summaries and the
	// written provider reflect overrides, not just spec-only inference (M-73).
	// The config may be inline content, a local file path, or a file:// URL
	// (M-76).
	configYAML, err := normalizeConfig(ctx, args.Config)
	if err != nil {
		out = generateErrorResult(err)
		res, err = marshalToolResult(out)
		return res, out, err
	}
	specBytes, err = mergeConfigIntoSpec(specBytes, configYAML)
	if err != nil {
		out = generateErrorResult(err)
		res, err = marshalToolResult(out)
		return res, out, err
	}
	// Build the same CollectOptions the CLI applies (DefaultCollectOptions +
	// generation.skip_* toggles) so an MCP generate emits the same complete
	// provider — docs, examples, Go coverage tests, build scaffolding — as the
	// CLI, instead of a bare IncludeBuild-only set that silently dropped them
	// (M-81). The canonical generator.yaml is not emitted: the MCP caller
	// already supplies the config, and writing it back into the output dir risks
	// clobbering a hand-written source-of-truth config the CLI guards against
	// (M-74).
	genOpts := generateCollectOptions(configYAML)
	resp := validateContext(ctx, specBytes)
	result := GenerateResult{
		Valid:       resp.Valid,
		Diagnostics: nonNilDiags(resp.Diagnostics),
		Resources:   []ResourceSummary{},
		DataSources: []EntitySummary{},
		Actions:     []EntitySummary{},
		Files:       []FileSummary{},
		StaleFiles:  []string{},
	}
	if resp.IRPreview != nil {
		result.Resources = summarizeResources(resp.IRPreview.Resources)
		result.DataSources = summarizeDataSources(resp.IRPreview.DataSources)
		result.Actions = summarizeActions(resp.IRPreview.Actions)
	}
	output := strings.TrimSpace(args.Output)

	// Without a valid IR preview there is nothing to plan or write.
	if !resp.Valid || resp.IRPreview == nil {
		out = result
		res, err = marshalToolResult(result)
		return res, out, err
	}

	if !args.DryRun && output == "" {
		// Fail loud when the caller forgot both flags, mirroring the CLI's
		// "--output is required for full provider generation" (N-51). A silent
		// success with file_count: 0 reads as "nothing to generate" and the
		// provider never reaches disk.
		result.Diagnostics = append(result.Diagnostics, api.DiagnosticJSON{
			Severity: "error", Summary: "Neither dry_run nor output was set",
			Detail: "eidos/generate requires either dry_run (plan-only) or output (a directory to write the provider to); pass output to generate files, or dry_run to preview them",
		})
		out = result
		res, err = marshalToolResult(result)
		return res, out, err
	}
	if args.DryRun {
		// Record-only: collect the planned file list without touching disk.
		entries, runErr := generator.Run(resp.IRPreview, generator.Options{
			Mode:           generator.ModeRecord,
			CollectOptions: genOpts,
		})
		if runErr != nil {
			result.Diagnostics = append(result.Diagnostics, api.DiagnosticJSON{
				Severity: "error", Summary: "Provider plan failed", Detail: runErr.Error(),
			})
		} else {
			result.FileCount = len(entries)
			result.Files = fileSummaries(entries, output)
			if output != "" {
				result.OutputDir = output
				stale, sErr := staleFilesInOutput(output, entries)
				if sErr != nil {
					result.Diagnostics = append(result.Diagnostics, api.DiagnosticJSON{
						Severity: "warning", Summary: "Could not scan output directory for stale files", Detail: sErr.Error(),
					})
				} else {
					result.StaleFiles = stale
				}
			}
		}
	} else if output != "" {
		entries, runErr := writeProvider(output, resp.IRPreview, genOpts, args.Force)
		if runErr != nil {
			result.Diagnostics = append(result.Diagnostics, api.DiagnosticJSON{
				Severity: "error", Summary: "Provider generation failed", Detail: runErr.Error(),
			})
		} else {
			result.FileCount = len(entries)
			result.OutputDir = output
			// WouldOverwrite is not meaningful after a forced write, so the
			// written files are listed without it.
			result.Files = fileSummaries(entries, "")
			stale, sErr := staleFilesInOutput(output, entries)
			if sErr != nil {
				result.Diagnostics = append(result.Diagnostics, api.DiagnosticJSON{
					Severity: "warning", Summary: "Could not scan output directory for stale files", Detail: sErr.Error(),
				})
			} else {
				result.StaleFiles = stale
			}
			if args.Verify {
				// runVerify blocks until `go mod tidy` + `go build ./...` finish
				// (up to the 5-minute timeout). The MCP SDK invokes handlers
				// sequentially per connection, so while this runs no other tool
				// call is serviced on the same connection — callers should treat
				// verify as an explicitly-slow operation (N-61).
				ok, vOut := runVerify(ctx, output)
				result.VerifyOK = ok
				result.VerifyOutput = truncateForJSON(vOut, maxVerifyOutput)
				if !ok {
					result.Diagnostics = append(result.Diagnostics, api.DiagnosticJSON{
						Severity: "error", Summary: "Post-generation verification failed", Detail: result.VerifyOutput,
					})
				}
			}
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
				"config": {Type: "string", Description: "Optional generator.yaml as inline YAML/JSON content, a local file path, or a file:// URL. When set, schema issues are reported against the override-shaped IR."},
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
	specBytes, err := normalizeSpec(ctx, args.Spec)
	if err != nil {
		out = validateSchemasErrorResult(err)
		res, err = marshalToolResult(out)
		return res, out, err
	}
	// Honor the optional generator.yaml so schema issues are reported against
	// the override-shaped IR (e.g. resources promoted via generate_resource),
	// not just spec-only inference (M-73). The config may be inline content, a
	// local file path, or a file:// URL (M-76).
	configYAML, err := normalizeConfig(ctx, args.Config)
	if err != nil {
		out = validateSchemasErrorResult(err)
		res, err = marshalToolResult(out)
		return res, out, err
	}
	specBytes, err = mergeConfigIntoSpec(specBytes, configYAML)
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
				"config": {Type: "string", Description: "generator.yaml as inline YAML/JSON content, a local file path, or a file:// URL (required)"},
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
	specBytes, err := normalizeSpec(ctx, args.Spec)
	if err != nil {
		out = overridePreviewErrorResult(err)
		res, err = marshalToolResult(out)
		return res, out, err
	}
	// The config may be inline content, a local file path, or a file:// URL
	// (M-76). It is required for this tool; normalizeConfig resolves a
	// reference and otherwise returns the inline body unchanged.
	configYAML, err := normalizeConfig(ctx, args.Config)
	if err != nil {
		out = overridePreviewErrorResult(err)
		res, err = marshalToolResult(out)
		return res, out, err
	}
	merged, err := mergeConfigIntoSpec(specBytes, configYAML)
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
	overrides, overrideDiags := reportOverrides(configYAML, resp.IRPreview)
	result.Overrides = overrides
	result.Diagnostics = append(result.Diagnostics, overrideDiags...)
	out = result
	res, err = marshalToolResult(result)
	return res, out, err
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// countConstructs derives the InspectCounts summary from the populated result
// slices. WiredResources/ScaffoldedResources split the managed-resource count
// by the Wired flag (Create+Read+Delete all mapped).
func countConstructs(r InspectResult) InspectCounts {
	c := InspectCounts{
		Resources:          len(r.Resources),
		DataSources:        len(r.DataSources),
		Actions:            len(r.Actions),
		EphemeralResources: len(r.Ephemerals),
		ListResources:      len(r.Lists),
		Functions:          len(r.Functions),
	}
	for _, res := range r.Resources {
		if res.Wired {
			c.WiredResources++
		} else {
			c.ScaffoldedResources++
		}
	}
	return c
}

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
// resource (by operation or schema name). When the config cannot be parsed it
// returns no reports and a warning diagnostic so a malformed/unresolvable config
// is surfaced instead of silently degrading to an empty override report (M-77).
func reportOverrides(configYAML string, preview *ir.ProviderIR) ([]OverrideReport, []api.DiagnosticJSON) {
	reports := make([]OverrideReport, 0)
	diags := make([]api.DiagnosticJSON, 0)
	if strings.TrimSpace(configYAML) == "" {
		return reports, diags
	}
	cfg, err := config.LoadBytes([]byte(configYAML))
	if err != nil {
		diags = append(diags, api.DiagnosticJSON{
			Severity: "warning",
			Summary:  "Could not parse generator.yaml for override report",
			Detail:   err.Error(),
		})
		return reports, diags
	}
	// Surface config validation warnings (both schema and operation set on an
	// override, env-var collisions). cfg.Warnings carries yaml:"-" so the
	// generator.yaml round-trip cannot hold them; without this they would be
	// written by config.Validate and immediately lost (M-16).
	for _, w := range cfg.Warnings {
		diags = append(diags, api.DiagnosticJSON{
			Severity: "warning",
			Summary:  w,
		})
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
	return reports, diags
}

// mergeConfigIntoSpec injects a generator.yaml config string into a spec body
// the way the HTTP validate handler expects (a top-level "config" field). The
// spec may be JSON or YAML; the merged body is re-serialized as JSON. An empty
// config is a no-op: the spec is returned unchanged so the no-config path
// behaves exactly as before (no re-serialization, no parse round-trip).
func mergeConfigIntoSpec(specBytes []byte, configYAML string) ([]byte, error) {
	if strings.TrimSpace(configYAML) == "" {
		return specBytes, nil
	}
	doc, err := decodeSpecMap(specBytes)
	if err != nil {
		return nil, err
	}
	doc["config"] = configYAML
	return json.Marshal(doc)
}

// decodeSpecMap decodes a spec into a generic map, preserving integer precision:
// json.Decoder with UseNumber keeps integral values as json.Number so an int64
// bound like "maximum": 9223372036854775807 survives the config-merge round-trip
// instead of silently degrading to float64 (which rounds past 2^53). The YAML
// fallback is unaffected — yaml.v3 already decodes integers as int64 (N-50).
func decodeSpecMap(data []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		var out map[string]any
		if err2 := yaml.Unmarshal(data, &out); err2 != nil {
			return nil, fmt.Errorf("spec must be JSON or YAML: %w", errors.Join(err, err2))
		}
		return out, nil
	}
	// json.Decoder decodes a single value; reject trailing content so this path
	// rejects malformed input the same way json.Unmarshal did.
	if dec.More() {
		return nil, fmt.Errorf("spec must be a single JSON or YAML document")
	}
	return doc, nil
}

// generateCollectOptions builds the CollectOptions for an MCP generate run. It
// mirrors the CLI's collectOptionsFor so an MCP generate produces the same
// complete provider (docs, examples, Go coverage tests, build scaffolding) as
// the CLI, honoring the config's generation.skip_* toggles and the
// sign_release opt-out (N-39). The canonical generator.yaml is not emitted
// (IncludeConfig=false): the MCP caller already supplies the config, and
// writing it back risks clobbering a hand-written source-of-truth config
// (M-81, cf. the CLI's M-74 collision guard).
func generateCollectOptions(configYAML string) generator.CollectOptions {
	opts := generator.DefaultCollectOptions()
	if strings.TrimSpace(configYAML) != "" {
		if cfg, err := config.LoadBytes([]byte(configYAML)); err == nil {
			if cfg.Generation.SkipTests {
				opts.IncludeTests = false
			}
			if cfg.Generation.SkipDocs {
				opts.IncludeDocs = false
			}
			if cfg.Generation.SkipBuild {
				opts.IncludeBuild = false
			}
			if cfg.SignRelease != nil {
				opts.SignRelease = cfg.SignRelease
			}
		}
	}
	opts.IncludeConfig = false
	return opts
}

// writeProvider runs the generator in write mode into dir and returns the
// planned file entries. The CollectOptions match the dry-run path so record and
// write modes emit the same set of files. force mirrors the CLI's --force: when
// false, a write that would overwrite an existing file fails loud instead of
// clobbering it (N-52). Previously this wrote with Force always true, so an MCP
// caller (or a prompt-injected request) could silently overwrite a hand-edited
// provider directory — or any writable path it pointed output at.
func writeProvider(dir string, pir *ir.ProviderIR, opts generator.CollectOptions, force bool) ([]generator.FileEntry, error) {
	return generator.Run(pir, generator.Options{
		Mode:           generator.ModeWrite,
		OutputDir:      dir,
		Force:          force,
		CollectOptions: opts,
	})
}

// fileSummaries maps planned file entries to FileSummary records. When outputDir
// is non-empty (dry-run), each entry's WouldOverwrite is set by statting the
// target path so the caller can see which existing files a write would clobber.
// After a forced write the files already exist, so outputDir is passed empty and
// WouldOverwrite stays false.
func fileSummaries(entries []generator.FileEntry, outputDir string) []FileSummary {
	out := make([]FileSummary, 0, len(entries))
	for _, e := range entries {
		fs := FileSummary{Path: e.Path, Reason: e.Reason}
		if outputDir != "" {
			fs.WouldOverwrite = pathExists(filepath.Join(outputDir, e.Path))
		}
		out = append(out, fs)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// staleFilesInOutput walks outputDir and returns the regular files present there
// that the planned generation would NOT produce, sorted deterministically. These
// are pre-existing files that a regeneration would leave behind (e.g. a resource
// file from a previous run whose resource was since removed from the spec). The
// .git directory and dot-prefixed entries are skipped to reduce noise. A missing
// outputDir yields an empty slice (nothing is stale yet).
func staleFilesInOutput(outputDir string, planned []generator.FileEntry) ([]string, error) {
	info, err := os.Stat(outputDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("output path %q is not a directory", outputDir)
	}
	plannedSet := make(map[string]bool, len(planned))
	for _, e := range planned {
		plannedSet[filepath.ToSlash(e.Path)] = true
	}
	// Initialize non-nil so an empty result serializes as [] (not null); the
	// generate output schema requires stale_files to be an array, and a null
	// value is rejected by the SDK's structured-output validation (M-80).
	stale := make([]string, 0)
	walkErr := filepath.WalkDir(outputDir, func(path string, d os.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, rErr := filepath.Rel(outputDir, path)
		if rErr != nil {
			return rErr
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		base := d.Name()
		if d.IsDir() {
			if base == ".git" || strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(base, ".") {
			return nil
		}
		if !plannedSet[rel] {
			stale = append(stale, rel)
		}
		return nil
	})
	if walkErr != nil {
		return stale, walkErr
	}
	sort.Strings(stale)
	return stale, nil
}

// maxVerifyOutput caps the captured `go build` output embedded in the result so
// a verbose build failure cannot balloon the MCP response.
const maxVerifyOutput = 4000

// runVerify runs `go mod tidy` then `go build ./...` in dir and reports whether
// the generated provider compiles. It is the first production use of os/exec in
// the repo; the build is bounded by a context timeout. A missing go toolchain
// surfaces as a non-nil error (clean diagnostic), not a panic.
func runVerify(ctx context.Context, dir string) (bool, string) {
	verifyCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	tidy := exec.CommandContext(verifyCtx, "go", "mod", "tidy")
	tidy.Dir = dir
	if out, err := tidy.CombinedOutput(); err != nil {
		return false, fmt.Sprintf("go mod tidy: %v\n%s", err, out)
	}
	build := exec.CommandContext(verifyCtx, "go", "build", "./...")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		return false, fmt.Sprintf("go build ./...: %v\n%s", err, out)
	}
	return true, ""
}

// truncateForJSON caps a string at n bytes, appending an ellipsis when truncated,
// so embedded build output stays bounded.
func truncateForJSON(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
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
		Files:       []FileSummary{},
		StaleFiles:  []string{},
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
