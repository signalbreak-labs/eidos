// Package api implements the Eidos feature validation HTTP API.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/signalbreak-labs/eidos/pkg/config"
	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/generator"
	"github.com/signalbreak-labs/eidos/pkg/ir"
	"github.com/signalbreak-labs/eidos/pkg/parser"
	"github.com/signalbreak-labs/eidos/pkg/transformer"
)

// maxRequestBodySize limits how much data the validate endpoint will read from
// a single request. It is large enough for typical OpenAPI specs while guarding
// against accidental or malicious huge payloads.
const maxRequestBodySize = 10 * 1024 * 1024 // 10 MiB

// DiagnosticJSON is a serializable view of a diagnostics.Diagnostic.
type DiagnosticJSON struct {
	Severity       string                      `json:"severity"`
	Summary        string                      `json:"summary"`
	Detail         string                      `json:"detail,omitempty"`
	Hint           string                      `json:"hint,omitempty"`
	SourceLocation *diagnostics.SourceLocation `json:"source_location,omitempty"`
}

// DetectedSummary captures the high-level findings of the validation pipeline.
//
// Resources, DataSources, Actions, EphemeralResources, ListResources, and
// Functions are operation-derived construct counts produced by the same
// classifyOperation heuristic used to build the IR preview, so the `detected`
// and `ir_preview` fields of a ValidateResponse are consistent for a given
// spec. ImportableResources and StateUpgraders reflect generator.yaml
// overrides (import_format and state_upgrades entries across resource_overrides).
type DetectedSummary struct {
	Version                string `json:"version"`
	Title                  string `json:"title,omitempty"`
	InfoVersion            string `json:"info_version,omitempty"`
	Paths                  int    `json:"paths"`
	Schemas                int    `json:"schemas"`
	Operations             int    `json:"operations"`
	Resources              int    `json:"resources"`
	DataSources            int    `json:"data_sources"`
	Actions                int    `json:"actions,omitempty"`
	EphemeralResources     int    `json:"ephemeral_resources,omitempty"`
	ListResources          int    `json:"list_resources,omitempty"`
	Functions              int    `json:"functions,omitempty"`
	SecuritySchemes        int    `json:"security_schemes"`
	SchemasWithOneOf       int    `json:"schemas_with_oneOf"`
	SchemasWithAllOf       int    `json:"schemas_with_allOf"`
	SchemasWithAnyOf       int    `json:"schemas_with_anyOf"`
	WriteOnlyAttributes    int    `json:"write_only_attributes"`
	ReadOnlyAttributes     int    `json:"read_only_attributes"`
	NullableAttributes     int    `json:"nullable_attributes"`
	PaginationStyle        string `json:"pagination_style,omitempty"`
	ImportableResources    int    `json:"importable_resources"`
	StateUpgraders         int    `json:"state_upgraders"`
	GenerateTerraformTests bool   `json:"generate_terraform_tests"`
	LoggingEnabled         bool   `json:"logging_enabled"`
	PolymorphismStrategy   string `json:"polymorphism_strategy,omitempty"`
}

// ValidateResponse is the structured JSON returned by POST /api/v1/validate.
type ValidateResponse struct {
	Valid           bool             `json:"valid"`
	Diagnostics     []DiagnosticJSON `json:"diagnostics"`
	Detected        DetectedSummary  `json:"detected"`
	IRPreview       *ir.ProviderIR   `json:"ir_preview,omitempty"`
	SuggestedConfig string           `json:"suggested_config,omitempty"`
}

// errorBody is the stable JSON shape returned for handler-level errors.
type errorBody struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

// NewValidateHandler returns a net/http HandlerFunc for POST /api/v1/validate.
func NewValidateHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			// Advertise the single allowed method so clients can self-correct
			// (L-14). The production mux also emits this via its own 405, but
			// NewValidateHandler may be mounted directly.
			w.Header().Set("Allow", "POST")
			if err := WriteJSONError(w, http.StatusMethodNotAllowed, "method not allowed"); err != nil {
				slog.Default().Error("api: failed to write method not allowed error", slog.String("error", err.Error()))
			}
			return
		}

		limited := http.MaxBytesReader(w, r.Body, maxRequestBodySize)
		body, err := io.ReadAll(limited)
		if err != nil {
			// A body exceeding maxRequestBodySize surfaces as *http.MaxBytesError;
			// return 413 Request Entity Too Large instead of a 400 that leaks the
			// MaxBytesReader error string to clients (L-14). Other read errors
			// remain 400 with a generic message.
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				if writeErr := WriteJSONError(w, http.StatusRequestEntityTooLarge, "request body too large"); writeErr != nil {
					slog.Default().Error("api: failed to write 413 error", slog.String("error", writeErr.Error()))
				}
				return
			}
			if writeErr := WriteJSONError(w, http.StatusBadRequest, "failed to read request body"); writeErr != nil {
				slog.Default().Error("api: failed to write bad request error", slog.String("error", writeErr.Error()))
			}
			return
		}

		resp := ValidateContextWithContentType(r.Context(), body, r.Header.Get("Content-Type"))

		data, err := json.Marshal(resp)
		if err != nil {
			if writeErr := WriteJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to marshal response: %v", err)); writeErr != nil {
				slog.Default().Error("api: failed to write marshal error", slog.String("error", writeErr.Error()))
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, writeErr := w.Write(data); writeErr != nil {
			slog.Default().Error("api: failed to write response body", slog.String("error", writeErr.Error()))
		}
	}
}

// WriteJSONError writes a JSON error object with the given HTTP status and
// returns any error encountered while serializing or writing the response body.
// It is used for all handler-level errors so clients always receive
// application/json responses. It is exported so the CLI server middleware can
// reuse the same error shape when recovering from panics.
func WriteJSONError(w http.ResponseWriter, status int, message string) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body, err := json.Marshal(errorBody{Error: message, Code: http.StatusText(status)})
	if err != nil {
		return fmt.Errorf("marshal error body: %w", err)
	}
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("write error body: %w", err)
	}
	return nil
}

// Validate runs the parse and normalize pipeline against the supplied
// request body and returns a structured report. The body may be JSON or YAML
// and may include an optional top-level "config" string. All failures are
// communicated through Diagnostics in the returned ValidateResponse.
func Validate(body []byte) ValidateResponse {
	return ValidateContext(context.Background(), body)
}

// ValidateWithContentType runs the same pipeline as Validate but hints the
// JSON/YAML parser using the supplied Content-Type header. This prevents
// flow-style YAML documents that start with '{' or '[' from being routed to
// the JSON parser when the caller declares an application/yaml media type.
func ValidateWithContentType(body []byte, contentType string) ValidateResponse {
	return validateContext(context.Background(), body, contentType)
}

// ValidateContextWithContentType is like ValidateWithContentType but honors
// cancellation on the supplied context. The HTTP handler passes the request
// context so a client disconnect or the server's WriteTimeout aborts the
// pipeline (the body read is already context-aware via MaxBytesReader).
func ValidateContextWithContentType(ctx context.Context, body []byte, contentType string) ValidateResponse {
	return validateContext(ctx, body, contentType)
}

// ValidateContext runs the same pipeline as Validate but honors cancellation on
// the supplied context. Long-running pipeline stages should accept ctx and
// return early when it is canceled; the current short pipeline at least
// surfaces cancellation before doing work.
func ValidateContext(ctx context.Context, body []byte) ValidateResponse {
	return validateContext(ctx, body, "")
}

func validateContext(ctx context.Context, body []byte, contentType string) ValidateResponse {
	var resp ValidateResponse

	if err := ctx.Err(); err != nil {
		resp.Diagnostics = append(resp.Diagnostics, DiagnosticJSON{
			Severity: diagnostics.Error.String(),
			Summary:  "Request canceled",
			Detail:   err.Error(),
		})
		resp.Valid = false
		return resp
	}

	root, err := loadRequestBodyWithContentType(body, contentType)
	if err != nil {
		resp.Diagnostics = append(resp.Diagnostics, DiagnosticJSON{
			Severity: diagnostics.Error.String(),
			Summary:  "Failed to parse request body",
			Detail:   err.Error(),
		})
		resp.Valid = false
		return resp
	}

	cfgStr, root, configDiags := extractConfig(root)
	resp.Diagnostics = append(resp.Diagnostics, configDiags...)

	version, versionDiags := parser.DetectVersion(root)
	resp.Diagnostics = append(resp.Diagnostics, toDiagnosticJSON(versionDiags)...)

	spec, convertDiags, err := convertForVersion(root, version)
	if err != nil {
		resp.Diagnostics = append(resp.Diagnostics, DiagnosticJSON{
			Severity: diagnostics.Error.String(),
			Summary:  "Failed to convert OpenAPI document",
			Detail:   err.Error(),
		})
	} else {
		resp.Diagnostics = append(resp.Diagnostics, toDiagnosticJSON(convertDiags)...)
		validationDiags := parser.Validate(root, spec, version)
		resp.Diagnostics = append(resp.Diagnostics, toDiagnosticJSON(validationDiags)...)
	}

	var cfg *config.Config
	if cfgStr != "" {
		parsedCfg, err := config.LoadBytes([]byte(cfgStr))
		if err != nil {
			resp.Diagnostics = append(resp.Diagnostics, DiagnosticJSON{
				Severity: diagnostics.Error.String(),
				Summary:  "Invalid generator.yaml config",
				Detail:   err.Error(),
			})
		} else {
			cfg = parsedCfg
		}
	}

	resp.Valid = !hasErrors(resp.Diagnostics)

	if spec != nil {
		preview, previewDiags := buildIRPreview(spec, version, cfg)
		resp.IRPreview = preview
		resp.Diagnostics = append(resp.Diagnostics, toDiagnosticJSON(previewDiags)...)
		resp.Valid = resp.Valid && !hasErrors(toDiagnosticJSON(previewDiags))
		// Detected construct counts are derived from the IR preview (after
		// overrides and classification) so `detected` and `ir_preview` report
		// consistent numbers for the same spec (M-5).
		resp.Detected = buildDetectedSummary(spec, cfg, preview)
		suggested, err := buildSuggestedConfig(spec, cfg)
		if err != nil {
			resp.Diagnostics = append(resp.Diagnostics, DiagnosticJSON{
				Severity: parser.SeverityError.String(),
				Summary:  "Failed to marshal suggested config",
				Detail:   err.Error(),
			})
			// The error diagnostic above was appended after resp.Valid was
			// finalized, so the API could otherwise return valid:true with an
			// error-severity diagnostic present (L-11).
			resp.Valid = false
		} else {
			resp.SuggestedConfig = suggested
		}
	}

	return resp
}

// loadRequestBodyWithContentType parses a spec body using the fixed display
// name "request.yaml"/"request.json" — appropriate for the HTTP validate
// endpoint, where the request body has no real filename.
func loadRequestBodyWithContentType(body []byte, contentType string) (parser.Node, error) {
	return loadRequestBody(body, contentType, "request.yaml", "request.json")
}

// loadRequestBodyWithName parses a spec body, attributing parse errors to name
// (the caller's real spec path or URL) so diagnostics point at the actual file
// rather than the generic "request.yaml". The name is used verbatim for both
// formats: the file's own extension is authoritative.
func loadRequestBodyWithName(body []byte, contentType, name string) (parser.Node, error) {
	return loadRequestBody(body, contentType, name, name)
}

func loadRequestBody(body []byte, contentType, yamlName, jsonName string) (parser.Node, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty request body")
	}

	ct := strings.ToLower(strings.TrimSpace(contentType))
	// Route explicitly by Content-Type / first byte (not by the display name's
	// extension) so a spec body is always parsed with the right parser and errors
	// are attributed to the caller's real file name.
	if strings.Contains(ct, "yaml") || strings.Contains(ct, "yml") {
		return parser.LoadFileAsYAML(yamlName, body)
	}
	if strings.Contains(ct, "json") || trimmed[0] == '{' || trimmed[0] == '[' {
		return parser.LoadFileAsJSON(jsonName, body)
	}
	return parser.LoadFileAsYAML(yamlName, body)
}

// extractConfig removes an optional top-level "config" string from the parsed
// OpenAPI AST and returns it along with any diagnostics. Removing the key
// prevents the validator from warning about an unsupported OpenAPI keyword and
// keeps source locations for the real document intact. If the "config" key is
// present but its value is not a string scalar, an Error diagnostic is
// emitted so the user knows their generator settings were ignored.
func extractConfig(root parser.Node) (string, parser.Node, []DiagnosticJSON) {
	var diags []DiagnosticJSON
	m, ok := root.(*parser.MapNode)
	if !ok {
		return "", root, diags
	}
	kept := make([]parser.MapEntry, 0, len(m.Entries))
	var cfg string
	for _, e := range m.Entries {
		if e.Key == nil {
			continue
		}
		key, ok := e.Key.Value.(string)
		if !ok {
			continue
		}
		if key == "config" {
			if s, ok := e.Value.(*parser.ScalarNode); ok {
				if v, ok2 := s.Value.(string); ok2 {
					cfg = v
					continue
				}
			}
			var got string
			switch e.Value.(type) {
			case *parser.MapNode:
				got = "mapping/object"
			case *parser.SequenceNode:
				got = "array"
			case *parser.ScalarNode:
				got = "non-string scalar"
			default:
				got = fmt.Sprintf("%T", e.Value)
			}
			sloc := e.Value.GetSourceLocation()
			diags = append(diags, DiagnosticJSON{
				Severity:       diagnostics.Error.String(),
				Summary:        "Invalid config field type",
				Detail:         fmt.Sprintf("top-level \"config\" must be a YAML/JSON string scalar; got %s", got),
				Hint:           "Provide the generator.yaml configuration as a single string scalar under the top-level \"config\" key.",
				SourceLocation: &sloc,
			})
			continue
		}
		kept = append(kept, e)
	}
	if len(kept) == len(m.Entries) {
		return "", root, diags
	}
	return cfg, &parser.MapNode{Entries: kept, SourceLocation: m.SourceLocation}, diags
}

func convertForVersion(root parser.Node, version parser.Version) (*parser.Spec, diagnostics.Diagnostics, error) {
	switch version {
	case parser.Version2_0:
		return parser.ConvertV2(root)
	case parser.Version3_0:
		return parser.ConvertV30(root)
	case parser.Version3_1:
		return parser.ConvertV31(root)
	default:
		return nil, nil, fmt.Errorf("unsupported or unknown OpenAPI version %q", version)
	}
}

func toDiagnosticJSON(ds diagnostics.Diagnostics) []DiagnosticJSON {
	out := make([]DiagnosticJSON, 0, len(ds))
	for _, d := range ds {
		out = append(out, DiagnosticJSON{
			Severity:       d.Severity.String(),
			Summary:        d.Summary,
			Detail:         d.Detail,
			SourceLocation: d.SourceLocation,
		})
	}
	return out
}

func hasErrors(diags []DiagnosticJSON) bool {
	for _, d := range diags {
		if d.Severity == diagnostics.Error.String() {
			return true
		}
	}
	return false
}

func buildDetectedSummary(spec *parser.Spec, cfg *config.Config, preview *ir.ProviderIR) DetectedSummary {
	ds := DetectedSummary{}

	if spec == nil {
		return ds
	}

	ds.Version = spec.OpenAPI
	if spec.Swagger != "" {
		ds.Version = spec.Swagger
	}
	if spec.Info != nil {
		ds.Title = spec.Info.Title
		ds.InfoVersion = spec.Info.Version
	}

	ds.Paths = len(spec.Paths)
	if spec.Components != nil {
		ds.Schemas = len(spec.Components.Schemas)
		ds.SecuritySchemes = len(spec.Components.SecuritySchemes)
	}

	ds.Operations = countOperations(spec)
	if preview != nil {
		ds.Resources = len(preview.Resources)
		ds.DataSources = len(preview.DataSources)
		ds.Actions = len(preview.Actions)
		ds.EphemeralResources = len(preview.EphemeralResources)
		ds.ListResources = len(preview.ListResources)
		ds.Functions = len(preview.Functions)
	}

	schemaStats := analyzeSchemas(spec)
	ds.SchemasWithOneOf = schemaStats.oneOf
	ds.SchemasWithAllOf = schemaStats.allOf
	ds.SchemasWithAnyOf = schemaStats.anyOf
	ds.WriteOnlyAttributes = schemaStats.writeOnly
	ds.ReadOnlyAttributes = schemaStats.readOnly
	ds.NullableAttributes = schemaStats.nullable

	if cfg != nil && cfg.Polymorphism != nil && cfg.Polymorphism.Strategy != "" {
		ds.PolymorphismStrategy = cfg.Polymorphism.Strategy
	} else if schemaStats.oneOf > 0 {
		// The generator currently defaults to dynamic_union for polymorphic
		// unions, so detection reports the same default unless the caller
		// explicitly overrides the strategy.
		ds.PolymorphismStrategy = "dynamic_union"
	}

	// Provider settings (not operation-inferred) are driven by generator.yaml.
	if cfg != nil {
		for _, ro := range cfg.ResourceOverrides {
			if strings.TrimSpace(ro.ImportFormat) != "" {
				ds.ImportableResources++
			}
			ds.StateUpgraders += len(ro.StateUpgrades)
		}

		if cfg.Pagination != nil {
			ds.PaginationStyle = cfg.Pagination.Style
		}
		if cfg.GenerateTerraformTests != nil && *cfg.GenerateTerraformTests {
			ds.GenerateTerraformTests = true
		}
		if cfg.Logging != nil && cfg.Logging.Enabled {
			ds.LoggingEnabled = true
		}
	}

	return ds
}

// countOperations returns the total number of HTTP operations declared by the
// spec (across paths and webhooks). Construct counts are derived from the IR
// preview in buildDetectedSummary so that `detected` and `ir_preview` stay
// consistent; this routine supplies only the raw operation total.
func countOperations(spec *parser.Spec) int {
	var total int
	processPathItem := func(pi *parser.PathItem) {
		if pi == nil {
			return
		}
		countOp := func(op *parser.Operation) {
			if op != nil {
				total++
			}
		}
		countOp(pi.Get)
		countOp(pi.Post)
		countOp(pi.Put)
		countOp(pi.Patch)
		countOp(pi.Delete)
	}
	for _, pi := range spec.Paths {
		processPathItem(pi)
	}
	for _, pi := range spec.Webhooks {
		processPathItem(pi)
	}
	return total
}

// isItemPath reports whether path addresses a single item by a path parameter,
// e.g. "/pets/{id}". Operations on item paths are treated as resource reads,
// updates, or deletes; operations on collection paths are treated as creates or
// custom actions.
func isItemPath(path string) bool {
	return strings.Contains(path, "{")
}

type schemaStats struct {
	oneOf     int
	allOf     int
	anyOf     int
	writeOnly int
	readOnly  int
	nullable  int
}

// analyzeSchemas counts schema features across the top-level schemas declared in
// components/schemas plus their direct inline children (Properties, Items,
// AllOf, OneOf, AnyOf, and Not). Schemas that are only reachable through a $ref
// and are not themselves declared top-level are not visited, which matches the
// parser's behavior of preserving refs rather than inlining them.
func analyzeSchemas(spec *parser.Spec) schemaStats {
	var stats schemaStats
	seen := make(map[*parser.Schema]struct{})
	if spec.Components != nil {
		for _, s := range spec.Components.Schemas {
			analyzeSchema(s, seen, &stats)
		}
	}
	return stats
}

func analyzeSchema(s *parser.Schema, seen map[*parser.Schema]struct{}, stats *schemaStats) {
	if s == nil {
		return
	}
	if _, ok := seen[s]; ok {
		return
	}
	seen[s] = struct{}{}

	if s.WriteOnly {
		stats.writeOnly++
	}
	if s.ReadOnly {
		stats.readOnly++
	}
	if s.Nullable {
		stats.nullable++
	}
	if len(s.OneOf) > 0 {
		stats.oneOf++
	}
	if len(s.AllOf) > 0 {
		stats.allOf++
	}
	if len(s.AnyOf) > 0 {
		stats.anyOf++
	}
	for _, child := range s.Properties {
		analyzeSchema(child, seen, stats)
	}
	if items, ok := s.Items.(*parser.Schema); ok {
		analyzeSchema(items, seen, stats)
	}
	for _, child := range s.AllOf {
		analyzeSchema(child, seen, stats)
	}
	for _, child := range s.OneOf {
		analyzeSchema(child, seen, stats)
	}
	for _, child := range s.AnyOf {
		analyzeSchema(child, seen, stats)
	}
	if s.Not != nil {
		analyzeSchema(s.Not, seen, stats)
	}
}

func buildIRPreview(spec *parser.Spec, version parser.Version, cfg *config.Config) (*ir.ProviderIR, diagnostics.Diagnostics) {
	name := providerName(spec, cfg)
	providerVersion := config.DefaultProviderVersion
	if cfg != nil && strings.TrimSpace(cfg.Provider.Version) != "" {
		providerVersion = cfg.Provider.Version
	}

	var description string
	if spec.Info != nil {
		description = spec.Info.Description
	}

	preview := &ir.ProviderIR{
		Name:              name,
		FullName:          "terraform-provider-" + name,
		TypeName:          name,
		Version:           providerVersion,
		Description:       description,
		SourceSpecVersion: string(version),
	}
	preview.Servers = providerServersIR(spec)

	// Thread the generator.yaml pagination config onto the provider client IR
	// before data sources are built so list data sources can carry it into their
	// wired Read body (REMAINING_GAPS §2). clientConfigFromIR guards each field,
	// so a partial ClientIR (only Pagination) leaves the other client defaults
	// intact.
	if cfg != nil && cfg.Pagination != nil {
		preview.ClientIR.Pagination = &ir.PaginationIR{
			Style:            cfg.Pagination.Style,
			PageParam:        cfg.Pagination.PageParam,
			PerPageParam:     cfg.Pagination.PerPageParam,
			TotalCountHeader: cfg.Pagination.TotalCountHeader,
			NextLinkHeader:   cfg.Pagination.NextLinkHeader,
			CursorField:      cfg.Pagination.CursorField,
		}
	}
	// Thread the generator.yaml logging config onto the provider client IR so
	// the generator can bake it into the Configure-time client.LoggingConfig.
	preview.ClientIR.Logging = loggingIRFromConfig(cfg)
	// Honor generator.yaml skip_operations / include_operations (G1): drop the
	// matching operations from the spec so CRUD grouping and the per-operation
	// pass both exclude them. Both the CLI generate path and the MCP server
	// funnel through this function, so the filter applies everywhere.
	filterDiags := applyOperationFilters(spec, cfg)
	pathOps, opsDiags := transformer.OperationsFromSpecWithDiagnostics(spec)
	// Surface schema-conversion diagnostics (e.g. unrepresentable allOf/oneOf/
	// anyOf composition in nested properties) instead of dropping them silently
	// (L-97 / fail-loud). Warnings do not break Valid; Errors do.
	previewDiags := make(diagnostics.Diagnostics, 0, len(opsDiags)+len(filterDiags))
	previewDiags = append(previewDiags, filterDiags...)
	previewDiags = append(previewDiags, opsDiags...)
	// Group operations into managed resources first (REMAINING_GAPS §3). A
	// complete CRUD group (Create + Read + Delete on a collection/instance path
	// pair) becomes a single wired resource instead of one partial resource per
	// operation. Operations consumed by a grouped resource are skipped in the
	// per-operation pass below so they are not double-emitted as data sources or
	// partial resources. Incomplete groups fall through to the per-operation
	// classification unchanged. The transformer path-operation map is computed
	// once and shared with the per-operation pass so data sources can build their
	// schemas from the same resolved request/response shapes (REMAINING_GAPS §4).
	groupedResources, consumed := buildGroupedResources(spec, name, pathOps)
	preview.Resources = append(preview.Resources, groupedResources...)

	// resource_overrides with generate_resource or explicit CRUD operations
	// promote an action to a managed resource (G8). Run before the per-operation
	// pass so the consumed operations are not double-emitted as actions.
	if cfg != nil {
		applyResourceCreationOverrides(preview, spec, name, pathOps, cfg.ResourceOverrides, consumed)
	}

	// Collection GETs paired with an instance Read are promoted to list
	// resources (additively: the data source is kept) by addPathOperations. The
	// instance path's template parameters name the promoted list resource's
	// identity attributes.
	listPaths := make(map[string][]string)
	for _, l := range transformer.InferListResources(transformer.InferResourceCRUD(pathOps)) {
		listPaths[l.CollectionPath] = instancePathParams(l.InstancePath)
	}

	addSpecPathOperations(preview, spec, name, consumed, pathOps, listPaths, &previewDiags)

	// Providers with managed resources expose an optional endpoint attribute so
	// practitioners can override the API base URL derived from the spec's
	// servers — most notably to point the provider at a mock server, which is
	// how the generated acceptance tests exercise wired CRUD bodies.
	if len(preview.Resources) > 0 {
		preview.ConfigSchema.Attributes = append(preview.ConfigSchema.Attributes, ir.AttributeIR{
			Name:        "endpoint",
			Description: "Overrides the default API base URL derived from the OpenAPI servers. Useful for directing the provider at a test or mock server.",
			Optional:    true,
			Schema:      ir.SchemaIR{Type: ir.TypeString},
		})
	}

	preview.SecurityIR = buildSecurityIR(spec, cfg, &previewDiags)
	warnPerOpORSecurity(spec, &previewDiags)
	// Map each declared security scheme to the provider-config attributes a
	// practitioner sets to authenticate, and merge them into the config schema.
	// This is the IR-assembly half of the auth wiring: the generated Configure
	// reads these attributes back and constructs the matching client interceptors
	// (pkg/generator/provider_auth.go). Without this, the provider schema has no
	// auth surface and Configure has nothing to read even though the generated
	// client ships ready-made interceptors.
	applySecurityConfigAttributes(preview, &previewDiags)
	applyConfigOverrides(preview, cfg, name, pathOps, consumed, &previewDiags)

	// Two operations that normalize to the same construct name (e.g. duplicate
	// operationIds) would make the generator emit two files at one path. Fail
	// loud here with a diagnostic naming both source operations instead of
	// surfacing a confusing "duplicate output path" error from the generator.
	previewDiags = append(previewDiags, checkDuplicateConstructNames(preview)...)

	return preview, previewDiags
}

// providerServersIR maps the spec's servers (with their variables) to IR.
func providerServersIR(spec *parser.Spec) []ir.ServerIR {
	servers := make([]ir.ServerIR, 0, len(spec.Servers))
	for _, s := range spec.Servers {
		srv := ir.ServerIR{
			URL:         s.URL,
			Description: s.Description,
		}
		if len(s.Variables) > 0 {
			srv.Variables = make(map[string]ir.ServerVariableIR, len(s.Variables))
			for n, v := range s.Variables {
				srv.Variables[n] = ir.ServerVariableIR{
					Default:     v.Default,
					Enum:        v.Enum,
					Description: v.Description,
				}
			}
		}
		servers = append(servers, srv)
	}
	return servers
}

// applyOperationFilters drops operations excluded by generator.yaml
// skip_operations/include_operations and returns the drop-count diagnostic (G1).
func applyOperationFilters(spec *parser.Spec, cfg *config.Config) diagnostics.Diagnostics {
	if cfg == nil {
		return nil
	}
	if dropped := transformer.FilterSpecOperations(spec, cfg.SkipOperations, cfg.IncludeOperations); dropped > 0 {
		return diagnostics.Diagnostics{{
			Severity: diagnostics.Info,
			Summary:  "Operations excluded by generator.yaml filters",
			Detail: fmt.Sprintf(
				"skip_operations/include_operations dropped %d operation(s) before inference; "+
					"the excluded operations do not appear in the generated provider.", dropped),
		}}
	}
	return nil
}

// addSpecPathOperations runs the per-operation classification over the spec's
// paths and webhooks maps.
func addSpecPathOperations(preview *ir.ProviderIR, spec *parser.Spec, providerName string, consumed map[string]map[string]bool, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation, listPaths map[string][]string, diags *diagnostics.Diagnostics) {
	if spec.Paths != nil {
		for _, path := range sortedKeys(spec.Paths) {
			addPathOperations(preview, spec, path, spec.Paths[path], providerName, consumed, pathOps, listPaths, diags)
		}
	}
	if spec.Webhooks != nil {
		for _, path := range sortedKeys(spec.Webhooks) {
			addPathOperations(preview, spec, path, spec.Webhooks[path], providerName, consumed, pathOps, listPaths, diags)
		}
	}
}

// applyConfigOverrides appends generator.yaml-declared actions, ephemeral
// resources, list resources, and functions to the preview, applies the
// remaining overrides, and surfaces override failures.
//
// An override that matches an already-inferred construct modifies that
// construct (via ApplyOverrides) rather than declaring a new one: appending a
// fresh construct from the override would emit two constructs with the same
// name — the override-created one and the renamed existing one. The override
// fields that ApplyOverrides does not handle (preflight endpoints, lifecycle
// mappings, function arguments) are applied to the matched construct here so
// they are not silently dropped.
//
// pathOps and consumed thread the resolved operation map and the set of
// operations already claimed by resources so an action override that targets a
// resource-claimed operation fails loud instead of silently appending an empty
// scaffold action (the operation was consumed by the resource, so no action was
// inferred, and the bare actionFromOverride carries no ConfigSchema or
// InvokeMapping).
func applyConfigOverrides(preview *ir.ProviderIR, cfg *config.Config, providerName string, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation, consumed map[string]map[string]bool, diags *diagnostics.Diagnostics) {
	if cfg == nil {
		return
	}
	for _, ao := range cfg.ActionOverrides {
		if idx := matchingActionIndex(preview.Actions, ao); idx >= 0 {
			applyActionOverrideExtras(&preview.Actions[idx], ao)
			continue
		}
		if path, method, ok := actionOverrideDoubleClaimed(ao, pathOps, consumed); ok {
			*diags = append(*diags, diagnostics.Diagnostic{
				Severity: diagnostics.Warning,
				Summary:  "Action override references an operation already claimed by a resource",
				Detail: fmt.Sprintf(
					"Action override %q targets %s %s, which a resource already consumes. The operation "+
						"can be claimed by exactly one construct, so the action is skipped. Remove the "+
						"operation from action_overrides or from the resource's create/read/update/delete "+
						"operations so the claim is unambiguous.",
					ao.Operation, strings.ToUpper(method), path),
			})
			continue
		}
		preview.Actions = append(preview.Actions, actionFromOverride(ao, providerName))
	}
	for _, eo := range cfg.EphemeralOverrides {
		if idx := matchingEphemeralIndex(preview.EphemeralResources, eo); idx >= 0 {
			applyEphemeralOverrideExtras(&preview.EphemeralResources[idx], eo)
		} else {
			preview.EphemeralResources = append(preview.EphemeralResources, ephemeralFromOverride(eo, providerName))
		}
	}
	for _, lo := range cfg.ListResourceOverrides {
		if matchingListResourceIndex(preview.ListResources, lo) < 0 {
			preview.ListResources = append(preview.ListResources, listResourceFromOverride(lo, providerName))
		}
	}
	for _, fo := range cfg.FunctionOverrides {
		if idx := matchingFunctionIndex(preview.Functions, fo); idx >= 0 {
			applyFunctionOverrideExtras(&preview.Functions[idx], fo)
		} else {
			preview.Functions = append(preview.Functions, functionFromOverride(fo, providerName))
		}
	}

	if err := transformer.ApplyOverrides(preview, cfg); err != nil {
		*diags = append(*diags, diagnostics.Diagnostic{
			Severity: diagnostics.Error,
			Summary:  "Failed to apply generator.yaml overrides",
			Detail:   err.Error(),
		})
	}
	applyWriteOnlyAttributesToProvider(preview, diags)
}

// actionOverrideDoubleClaimed reports whether an action override's operation is
// already consumed by a resource (a grouped resource or a resource creation
// override). The operation resolves in the spec but was claimed before
// applyConfigOverrides ran, so appending an actionFromOverride would emit an
// empty scaffold (no ConfigSchema, no InvokeMapping) for an operation the
// resource already owns. Overrides that name a method+path or an operation with
// no operationId leave ok=false — those legitimately declare a fresh construct.
func actionOverrideDoubleClaimed(ao config.ActionOverride, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation, consumed map[string]map[string]bool) (string, string, bool) {
	path, method, op := resolveOperationByID(pathOps, ao.Operation)
	if op == nil || !isConsumed(consumed, path, method) {
		return "", "", false
	}
	return path, method, true
}

// matchingActionIndex returns the index of the first action an override matches
// (by source operation, method+path, or name identity), or -1. When an override
// matches an already-inferred action, it modifies that action rather than
// declaring a new one.
func matchingActionIndex(actions []ir.ActionIR, ao config.ActionOverride) int {
	for i := range actions {
		if transformer.OverrideMatchesEntity(actions[i].SourceOperation, actions[i].InvokeMapping, ao.Operation, actions[i].Name, actions[i].TypeName, actions[i].FullName) {
			return i
		}
	}
	return -1
}

// applyActionOverrideExtras applies the action-override fields that
// transformer.ApplyOverrides does not handle — the explicit preflight and
// server-side validation endpoints — to an existing action matched by an
// override. Without this, an override that matches an existing action would
// silently drop its declared modify_plan_operation / validate_config_operation.
func applyActionOverrideExtras(a *ir.ActionIR, ao config.ActionOverride) {
	if strings.TrimSpace(ao.ModifyPlanOperation) != "" {
		m := operationMappingFromString(ao.ModifyPlanOperation)
		a.ModifyPlanMapping = &m
		a.ModifyPlan = true
	}
	if strings.TrimSpace(ao.ValidateConfigOperation) != "" {
		m := operationMappingFromString(ao.ValidateConfigOperation)
		a.ValidateConfigMapping = &m
	}
}

func matchingEphemeralIndex(ephemerals []ir.EphemeralResourceIR, eo config.EphemeralOverride) int {
	for i := range ephemerals {
		if transformer.OverrideMatchesEntity(ephemerals[i].SourceOperation, ephemerals[i].OpenMapping, eo.Operation, ephemerals[i].Name, ephemerals[i].TypeName, ephemerals[i].FullName) {
			return i
		}
	}
	return -1
}

// applyEphemeralOverrideExtras applies the ephemeral-override fields that
// transformer.ApplyOverrides does not handle — the Open/Renew/Close lifecycle
// mappings — to an existing ephemeral resource matched by an override.
func applyEphemeralOverrideExtras(e *ir.EphemeralResourceIR, eo config.EphemeralOverride) {
	if strings.TrimSpace(eo.OpenMapping) != "" {
		e.OpenMapping = operationMappingFromString(eo.OpenMapping)
	}
	if strings.TrimSpace(eo.RenewMapping) != "" {
		rm := operationMappingFromString(eo.RenewMapping)
		e.RenewMapping = &rm
		e.HasRenew = true
	}
	if strings.TrimSpace(eo.CloseMapping) != "" {
		cm := operationMappingFromString(eo.CloseMapping)
		e.CloseMapping = &cm
		e.HasClose = true
	}
}

func matchingListResourceIndex(listResources []ir.ListResourceIR, lo config.ListResourceOverride) int {
	for i := range listResources {
		if transformer.OverrideMatchesEntity(listResources[i].SourceOperation, listResources[i].ListMapping, lo.Operation, listResources[i].Name, listResources[i].TypeName, listResources[i].FullName) {
			return i
		}
	}
	return -1
}

func matchingFunctionIndex(functions []ir.FunctionIR, fo config.FunctionOverride) int {
	for i := range functions {
		if transformer.OverrideMatchesEntity(functions[i].SourceOperation, ir.OperationMappingIR{}, fo.Operation, functions[i].Name, functions[i].TypeName, functions[i].FullName) {
			return i
		}
	}
	return -1
}

// applyFunctionOverrideExtras applies the function-override fields that
// transformer.ApplyOverrides does not handle — the declared arguments — to an
// existing function matched by an override.
func applyFunctionOverrideExtras(f *ir.FunctionIR, fo config.FunctionOverride) {
	for _, arg := range fo.Arguments {
		f.Arguments = append(f.Arguments, ir.FunctionParamIR{
			Name:   arg.Name,
			Schema: ir.SchemaIR{Type: primitiveTypeFromString(arg.Type)},
		})
	}
}

// sortedKeys returns the keys of m in lexicographic order. It is used to make
// map iteration deterministic so identical requests produce identical IR previews
// and pagination hints (L-10).
func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// envVarName derives a valid environment-variable name from an OpenAPI security
// scheme key. Scheme keys may contain '-' or '.' (legal in OpenAPI), which
// produce invalid env var names like "MY-KEY_API_KEY". Non-alphanumeric
// characters (except underscore) are replaced with '_' before upper-casing
// (L-12).
func envVarName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(name) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

// applyWriteOnlyAttributesToProvider runs ApplyWriteOnlyAttributes over every
// ObjectSchemaIR reachable in the provider IR. This ensures that any inferred or
// override-provided schemas receive the _wo/_version transformation. Naming-
// convention conflicts (L-112) are appended to diags as warnings.
func applyWriteOnlyAttributesToProvider(provider *ir.ProviderIR, diags *diagnostics.Diagnostics) {
	if provider == nil {
		return
	}
	for i := range provider.Resources {
		transformer.ApplyWriteOnlyAttributesWithDiagnostics(&provider.Resources[i].Schema, diags)
	}
	for i := range provider.DataSources {
		transformer.ApplyWriteOnlyAttributesWithDiagnostics(&provider.DataSources[i].Schema, diags)
	}
	for i := range provider.Actions {
		transformer.ApplyWriteOnlyAttributesWithDiagnostics(&provider.Actions[i].ConfigSchema, diags)
	}
	for i := range provider.EphemeralResources {
		transformer.ApplyWriteOnlyAttributesWithDiagnostics(&provider.EphemeralResources[i].ConfigSchema, diags)
		// ResultSchema is also a reachable ObjectSchemaIR; visiting it keeps the
		// "every ObjectSchemaIR reachable" claim in the doc comment true (L-15).
		transformer.ApplyWriteOnlyAttributesWithDiagnostics(&provider.EphemeralResources[i].ResultSchema, diags)
	}
	for i := range provider.ListResources {
		transformer.ApplyWriteOnlyAttributesWithDiagnostics(&provider.ListResources[i].ConfigSchema, diags)
		transformer.ApplyWriteOnlyAttributesWithDiagnostics(&provider.ListResources[i].IdentitySchema, diags)
		if provider.ListResources[i].ResourceSchema != nil {
			transformer.ApplyWriteOnlyAttributesWithDiagnostics(provider.ListResources[i].ResourceSchema, diags)
		}
	}
}

// loggingIRFromConfig translates the generator.yaml logging config onto the
// provider client IR. config.LoggingConfig is the user-facing generator.yaml
// shape; LoggingIR uses the generated client's field names (FilePath→LogFile).
// Enabled is dropped: logging is enabled iff LogFile is non-empty, matching
// the generated client's New guard. Returns nil when no logging config is set.
func loggingIRFromConfig(cfg *config.Config) *ir.LoggingIR {
	if cfg == nil || cfg.Logging == nil {
		return nil
	}
	logging := &ir.LoggingIR{
		CaptureRequestHeaders:  cfg.Logging.CaptureRequestHeaders,
		CaptureRequestBody:     cfg.Logging.CaptureRequestBody,
		CaptureResponseHeaders: cfg.Logging.CaptureResponseHeaders,
		CaptureResponseBody:    cfg.Logging.CaptureResponseBody,
		MaxBodyBytes:           cfg.Logging.MaxBodyBytes,
		RedactHeaders:          cfg.Logging.RedactHeaders,
	}
	if cfg.Logging.Enabled {
		logging.LogFile = cfg.Logging.FilePath
	}
	return logging
}

// buildSecurityIR assembles the security IR from the spec's declared schemes
// and global security requirements. The schemes populate SecurityIR.Schemes
// (each becomes a provider-config attribute + generated client interceptor); the
// global security requirements populate SecurityIR.DefaultRequirements so they
// are no longer silently dropped.
//
// OpenAPI security semantics: the `security` field is a list of requirement
// objects; multiple objects in the list mean OR (any one suffices), while
// multiple schemes in a single object mean AND (all required). eidos does not
// model either at request time: it applies every declared scheme (AND of all
// schemes), which matches the single-requirement AND case but is stricter than
// the OR case (it demands every alternative be satisfied rather than one). When
// the spec declares more than one global requirement (OR semantics), this emits
// a Warning diagnostic so the gap is surfaced rather than silently
// mis-resolving — the fail-loud principle applied where exact resolution is out
// of scope.
func buildSecurityIR(spec *parser.Spec, cfg *config.Config, diags *diagnostics.Diagnostics) ir.SecurityIR {
	var security ir.SecurityIR
	selectedScheme := ""
	if cfg != nil && cfg.Security != nil {
		selectedScheme = strings.TrimSpace(cfg.Security.Scheme)
	}

	// Carry the global security requirements into the IR so they are not
	// silently dropped. Each parser.SecurityRequirement wraps a
	// map[schemeName][]scopes; copy it into a fresh map to avoid aliasing the
	// parser's storage.
	for _, req := range spec.Security {
		if req.Requirements == nil {
			// An empty requirement object {} marks the API as allowing
			// unauthenticated access; preserve it as an empty map.
			security.DefaultRequirements = append(security.DefaultRequirements, map[string][]string{})
			continue
		}
		reqCopy := make(map[string][]string, len(req.Requirements))
		for schemeName, scopes := range req.Requirements {
			reqCopy[schemeName] = scopes
		}
		security.DefaultRequirements = append(security.DefaultRequirements, reqCopy)
	}

	if len(spec.Security) > 1 && selectedScheme == "" {
		// OR semantics: more than one global security requirement means any one
		// suffices. eidos applies all declared schemes (AND of all), which is
		// stricter — a practitioner would have to set every alternative's
		// credentials. Surface this as a Warning rather than silently
		// mis-resolving. Setting generator.yaml `security.scheme` selects one
		// alternative and suppresses the warning (G6).
		*diags = append(*diags, diagnostics.Diagnostic{
			Severity: diagnostics.Warning,
			Summary:  "OR security-requirement resolution not modeled",
			Detail: fmt.Sprintf(
				"The spec declares %d global security requirements, which OpenAPI "+
					"interprets as OR (any one suffices). eidos applies every declared "+
					"security scheme as AND (all required), so the generated provider "+
					"will require credentials for every alternative rather than one. "+
					"Set generator.yaml `security.scheme` to a declared scheme to "+
					"select one alternative, or review the generated provider's auth "+
					"behavior before relying on it.",
				len(spec.Security),
			),
		})
	}

	if spec.Components == nil {
		return security
	}
	for _, name := range sortedKeys(spec.Components.SecuritySchemes) {
		if selectedScheme != "" && name != selectedScheme {
			continue
		}
		scheme := spec.Components.SecuritySchemes[name]
		irScheme := ir.SecuritySchemeIR{
			Name:             name,
			Type:             ir.SecuritySchemeType(scheme.Type),
			Description:      scheme.Description,
			In:               scheme.In,
			NameField:        scheme.Name,
			Scheme:           scheme.Scheme,
			BearerFormat:     scheme.BearerFormat,
			OpenIDConnectURL: scheme.OpenIDConnectURL,
		}
		if scheme.Flows != nil {
			irScheme.Flows = &ir.OAuthFlowsIR{}
			if scheme.Flows.Implicit != nil {
				irScheme.Flows.Implicit = oauthFlowToIR(scheme.Flows.Implicit)
			}
			if scheme.Flows.Password != nil {
				irScheme.Flows.Password = oauthFlowToIR(scheme.Flows.Password)
			}
			if scheme.Flows.ClientCredentials != nil {
				irScheme.Flows.ClientCredentials = oauthFlowToIR(scheme.Flows.ClientCredentials)
			}
			if scheme.Flows.AuthorizationCode != nil {
				irScheme.Flows.AuthorizationCode = oauthFlowToIR(scheme.Flows.AuthorizationCode)
			}
		}
		security.Schemes = append(security.Schemes, irScheme)
	}
	sort.Slice(security.Schemes, func(i, j int) bool {
		return security.Schemes[i].Name < security.Schemes[j].Name
	})
	return security
}

// warnPerOpORSecurity emits a Warning for each operation that declares more than
// one security requirement (OR semantics: any one suffices). eidos applies every
// configured scheme on such an operation (AND of all), which is stricter than OR
// and may require credentials for every alternative; the warning surfaces this
// so the mis-resolution is not silent. Global OR is warned separately by
// buildSecurityIR; this covers operation-level OR (REMAINING_GAPS §1).
func warnPerOpORSecurity(spec *parser.Spec, diags *diagnostics.Diagnostics) {
	if diags == nil || spec == nil || spec.Paths == nil {
		return
	}
	for _, pathItem := range spec.Paths {
		if pathItem == nil {
			continue
		}
		for _, op := range []*parser.Operation{pathItem.Get, pathItem.Post, pathItem.Put, pathItem.Patch, pathItem.Delete} {
			if op == nil || len(op.Security) <= 1 {
				continue
			}
			*diags = append(*diags, diagnostics.Diagnostic{
				Severity: diagnostics.Warning,
				Summary:  "OR security-requirement resolution not modeled",
				Detail: fmt.Sprintf(
					"Operation %q declares %d security requirements, which OpenAPI "+
						"interprets as OR (any one suffices). eidos applies every configured "+
						"security scheme as AND (all required) on this operation, so the "+
						"generated provider will require credentials for every alternative "+
						"rather than one. Restrict the operation's `security` to a single "+
						"requirement, or review the generated provider's auth behavior before "+
						"relying on it.",
					operationLabel(op), len(op.Security),
				),
			})
		}
	}
}

// operationLabel returns a human-readable label for an operation for use in
// diagnostics, preferring OperationID and falling back to the summary.
func operationLabel(op *parser.Operation) string {
	if op == nil {
		return "<unknown>"
	}
	if op.OperationID != "" {
		return op.OperationID
	}
	return op.Summary
}

// applySecurityConfigAttributes maps each declared security scheme to the
// provider-config attributes a practitioner must set to authenticate (via
// transformer.MapSecuritySchemeToProviderConfig) and merges them into the
// provider config schema. All generated auth attributes are Optional; required
// credentials are enforced at runtime by the generated client. Duplicate
// attribute names across schemes (for example two OAuth2 schemes both
// contributing client_id) are collapsed to the first declaration so the config
// schema stays valid. Scheme-mapping errors surface as diagnostics rather than
// being dropped silently.
func applySecurityConfigAttributes(preview *ir.ProviderIR, diags *diagnostics.Diagnostics) {
	if preview == nil || len(preview.SecurityIR.Schemes) == 0 {
		return
	}
	seen := make(map[string]struct{}, len(preview.ConfigSchema.Attributes))
	for _, a := range preview.ConfigSchema.Attributes {
		seen[a.Name] = struct{}{}
	}
	for _, scheme := range preview.SecurityIR.Schemes {
		attrs, err := transformer.MapSecuritySchemeToProviderConfig(scheme, preview.SecurityIR.Schemes)
		if err != nil {
			*diags = append(*diags, diagnostics.Diagnostic{
				Severity: diagnostics.Error,
				Summary:  "Failed to map security scheme to provider config",
				Detail:   fmt.Sprintf("scheme %q: %s", scheme.Name, err.Error()),
			})
			continue
		}
		for _, a := range attrs {
			if _, dup := seen[a.Name]; dup {
				continue
			}
			seen[a.Name] = struct{}{}
			preview.ConfigSchema.Attributes = append(preview.ConfigSchema.Attributes, a)
		}
	}
}

func oauthFlowToIR(flow *parser.OAuthFlow) *ir.OAuthFlowIR {
	if flow == nil {
		return nil
	}
	return &ir.OAuthFlowIR{
		AuthorizationURL: flow.AuthorizationURL,
		TokenURL:         flow.TokenURL,
		RefreshURL:       flow.RefreshURL,
		Scopes:           flow.Scopes,
	}
}

func actionFromOverride(ao config.ActionOverride, providerName string) ir.ActionIR {
	name := ao.Name
	if strings.TrimSpace(name) == "" {
		name = transformer.ToSnakeCase(ao.Operation)
	}
	a := ir.ActionIR{
		Name:             name,
		FullName:         providerName + "_" + name,
		TypeName:         providerName + "_" + name,
		Description:      ao.Description,
		SourceOperation:  ao.Operation,
		ModifyPlan:       ao.ModifyPlan,
		ProgressMessages: ao.ProgressMessages,
	}
	// Explicit preflight / server-side validation endpoints (F3). A declared
	// modify_plan_operation implies the ModifyPlan method.
	if strings.TrimSpace(ao.ModifyPlanOperation) != "" {
		m := operationMappingFromString(ao.ModifyPlanOperation)
		a.ModifyPlanMapping = &m
		a.ModifyPlan = true
	}
	if strings.TrimSpace(ao.ValidateConfigOperation) != "" {
		m := operationMappingFromString(ao.ValidateConfigOperation)
		a.ValidateConfigMapping = &m
	}
	return a
}

func ephemeralFromOverride(eo config.EphemeralOverride, providerName string) ir.EphemeralResourceIR {
	name := eo.Name
	if strings.TrimSpace(name) == "" {
		name = transformer.ToSnakeCase(eo.Operation)
	}
	er := ir.EphemeralResourceIR{
		Name:            name,
		FullName:        providerName + "_" + name,
		TypeName:        providerName + "_" + name,
		Description:     eo.Description,
		SourceOperation: eo.Operation,
	}
	// Open, Renew, and Close mappings are parsed from the override strings
	// using operationMappingFromString. HasRenew and HasClose are inferred from
	// the presence of a non-empty mapping: a missing mapping is treated as the
	// ephemeral resource not exposing that lifecycle operation.
	if strings.TrimSpace(eo.OpenMapping) != "" {
		er.OpenMapping = operationMappingFromString(eo.OpenMapping)
	}
	if strings.TrimSpace(eo.RenewMapping) != "" {
		rm := operationMappingFromString(eo.RenewMapping)
		er.RenewMapping = &rm
		er.HasRenew = true
	}
	if strings.TrimSpace(eo.CloseMapping) != "" {
		cm := operationMappingFromString(eo.CloseMapping)
		er.CloseMapping = &cm
		er.HasClose = true
	}
	return er
}

func listResourceFromOverride(lo config.ListResourceOverride, providerName string) ir.ListResourceIR {
	name := transformer.ToSnakeCase(lo.Resource)
	lr := ir.ListResourceIR{
		Name:            name,
		FullName:        providerName + "_" + name,
		TypeName:        providerName + "_" + name,
		SourceOperation: lo.Operation,
	}
	if lo.Pagination != nil {
		lr.PaginationStyle = lo.Pagination.Style
	}
	return lr
}

func functionFromOverride(fo config.FunctionOverride, providerName string) ir.FunctionIR {
	name := fo.Name
	if strings.TrimSpace(name) == "" {
		name = transformer.ToSnakeCase(fo.Operation)
	}
	fn := ir.FunctionIR{
		Name:            name,
		FullName:        providerName + "_" + name,
		TypeName:        providerName + "_" + name,
		SourceOperation: fo.Operation,
	}
	for _, arg := range fo.Arguments {
		fn.Arguments = append(fn.Arguments, ir.FunctionParamIR{
			Name:   arg.Name,
			Schema: ir.SchemaIR{Type: primitiveTypeFromString(arg.Type)},
		})
	}
	return fn
}

// primitiveTypeFromString maps a generator.yaml primitive type name to the
// corresponding IR primitive type. Unrecognized or empty values resolve to
// TypeDynamic so callers can see that the type was declared but not understood.
func primitiveTypeFromString(s string) ir.PrimitiveType {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "string":
		return ir.TypeString
	case "integer", "int":
		return ir.TypeInt
	case "number", "float":
		return ir.TypeFloat
	case "boolean", "bool":
		return ir.TypeBool
	case "null":
		return ir.TypeNull
	case "":
		return ir.TypeDynamic
	default:
		return ir.TypeDynamic
	}
}

// operationMappingFromString parses a preview operation mapping of the form
// "METHOD /path" (e.g. "GET /pets/{id}"). The method is matched case-insensitively
// and normalized to upper-case, and separated from the path by whitespace. If
// the input does not match this format the entire string is used as the path
// template with an empty method and no default success codes, which is a
// preview-only fallback. An empty input produces an all-empty OperationMappingIR;
// that is the canonical "unset" signal used by callers to detect a missing
// mapping (L-16: the prior doc claimed the method "must be upper-cased", but the
// code and tests accept lower-case input and normalize it).
func operationMappingFromString(s string) ir.OperationMappingIR {
	parts := strings.Fields(s)
	if len(parts) == 2 {
		return ir.OperationMappingIR{
			Method:       strings.ToUpper(parts[0]),
			PathTemplate: parts[1],
			SuccessCodes: defaultSuccessCodes(strings.ToUpper(parts[0])),
		}
	}
	return ir.OperationMappingIR{PathTemplate: s}
}

func providerName(spec *parser.Spec, cfg *config.Config) string {
	if cfg != nil && strings.TrimSpace(cfg.Provider.Name) != "" {
		return cfg.Provider.Name
	}
	if spec.Info != nil && strings.TrimSpace(spec.Info.Title) != "" {
		// Terraform provider type names allow only letters, digits, and hyphens
		// (no underscores or dots), so derive the default from the title as
		// kebab-case rather than snake_case (e.g. "Example Cloud API" ->
		// "example-cloud-api", not the invalid "example_cloud_api").
		return transformer.ToProviderTypeName(spec.Info.Title)
	}
	return "generated"
}

func addPathOperations(preview *ir.ProviderIR, spec *parser.Spec, path string, pi *parser.PathItem, providerName string, consumed map[string]map[string]bool, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation, listPaths map[string][]string, diags *diagnostics.Diagnostics) {
	if pi == nil {
		return
	}
	ops := map[string]*parser.Operation{
		"GET":    pi.Get,
		"POST":   pi.Post,
		"PUT":    pi.Put,
		"PATCH":  pi.Patch,
		"DELETE": pi.Delete,
	}
	for method, op := range ops {
		if op == nil {
			continue
		}
		if isConsumed(consumed, path, method) {
			continue
		}
		op = mergePathParams(op, pi.Parameters)
		switch classifyOperation(path, method, op, pathOps, true) {
		case kindResource:
			preview.Resources = append(preview.Resources, resourceFromOperation(op, providerName, path, method, pathOps))
		case kindDataSource:
			preview.DataSources = append(preview.DataSources, dataSourceFromOperation(op, providerName, path, method, pathOps, preview.ClientIR.Pagination))
			// A collection GET paired with an instance Read is also a list
			// resource (InferListResources). The promotion is additive — the
			// data source above is kept so existing wiring is not broken —
			// and generator.yaml overrides remain the authoritative escape
			// hatch. x-terraform-list operations take the kindListResource
			// branch instead and are not double-emitted.
			if method == "GET" && listPaths[path] != nil {
				preview.ListResources = append(preview.ListResources, listResourceFromOperation(op, providerName, path, method, pathOps, listPaths[path]))
				warnListUniqueItems(diags, pathOps, path, method)
			}
		case kindAction:
			preview.Actions = append(preview.Actions, actionFromOperation(op, providerName, path, method, pathOps))
		case kindEphemeral:
			preview.EphemeralResources = append(preview.EphemeralResources, ephemeralFromOperation(spec, op, providerName, path, method, pathOps))
			// The sibling lifecycle operations the ephemeral claims as its
			// Renew/Close mappings are consumed so they do not also classify as
			// their own constructs (renew/revoke/rotate are action verbs, so a
			// lifecycle subpath would otherwise double-emit as a spurious
			// action; PROJECT_DESIGN §23). Paths are iterated in sorted order, so the
			// ephemeral's own path is visited before its "/renew"/"/close"/
			// "/revoke" siblings and the consumption is visible to them.
			consumeEphemeralLifecycleOps(consumed, path)
		case kindListResource:
			preview.ListResources = append(preview.ListResources, listResourceFromOperation(op, providerName, path, method, pathOps, nil))
			warnListUniqueItems(diags, pathOps, path, method)
		case kindFunction:
			preview.Functions = append(preview.Functions, functionFromOperation(op, providerName, path, method, pathOps))
		}
	}
}

// warnListUniqueItems surfaces a fail-loud warning when a list-classified
// operation declares uniqueItems on its response array. The experimental
// list/schema package has no Set types, so the generator downgrades a Set
// element to a List (list.go:372). The warning is emitted here — the only
// place the resolved response schema is reachable for a list-classified
// operation — instead of leaving the downgrade silent (A1).
func warnListUniqueItems(diags *diagnostics.Diagnostics, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation, path, method string) {
	if top := lookupTransformerOp(pathOps, path, method); top != nil && top.ResponseSchema != nil &&
		strings.EqualFold(top.ResponseSchema.Type, "array") && top.ResponseSchema.UniqueItems {
		*diags = append(*diags, diagnostics.Diagnostic{
			Severity: diagnostics.Warning,
			Summary:  "uniqueItems on a list resource is not supported by the list/schema API; falling back to List",
			Detail: fmt.Sprintf(
				"The list endpoint %s %s declares a response array with uniqueItems: true, "+
					"but the Terraform Plugin Framework list/schema package has no Set types, so the "+
					"generated list resource models the collection as a List (duplicates allowed).",
				strings.ToUpper(method), path),
		})
	}
}

// buildGroupedResources runs CRUD inference over the parsed spec and returns one
// wired managed resource per complete CRUD group (Create + Read + Delete), plus
// the set of (path, method) pairs those resources consume so the per-operation
// pass does not double-emit them. Incomplete groups are left to the per-operation
// classification. This is the §3 fix: real specs with a collection POST plus an
// instance GET/DELETE now yield a single wired resource instead of separate
// partial resources.
func buildGroupedResources(spec *parser.Spec, providerName string, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation) ([]ir.ResourceIR, map[string]map[string]bool) {
	if len(pathOps) == 0 {
		return nil, nil
	}
	groups := transformer.InferResourceCRUD(pathOps)

	var resources []ir.ResourceIR
	consumed := make(map[string]map[string]bool)
	for _, g := range groups {
		if g.Create == nil || g.Read == nil || g.Delete == nil {
			continue
		}
		// Explicit x-terraform-* extensions and ephemeral/function path keywords
		// take precedence over CRUD grouping: an operation marked as an action,
		// ephemeral, function, or list is not a resource lifecycle step. The
		// method-based resource/data-source distinction is intentionally overridden
		// by grouping (a GET on an instance path becomes the resource Read).
		if !groupIsResource(spec, g, pathOps) {
			continue
		}

		res := ir.ResourceIR{
			Name:            g.Name,
			FullName:        providerName + "_" + g.Name,
			TypeName:        providerName + "_" + g.Name,
			SourceOperation: groupSourceOperation(g),
		}
		res.CRUDMapping.Create = operationMapping("POST", g.CollectionPath, parserOp(spec, g.CollectionPath, "POST"), envelopeOf(g.Create))
		res.CRUDMapping.Create.MediaType = mediaTypeOf(g.Create)
		res.CRUDMapping.Read = operationMapping("GET", g.InstancePath, parserOp(spec, g.InstancePath, "GET"), envelopeOf(g.Read))
		res.CRUDMapping.Read.MediaType = mediaTypeOf(g.Read)
		if g.Update != nil {
			updMethod := "PUT"
			if g.FullUpdate == nil && g.PartialUpdate != nil {
				updMethod = "PATCH"
			}
			upd := operationMapping(updMethod, g.InstancePath, parserOp(spec, g.InstancePath, updMethod), envelopeOf(g.Update))
			upd.MediaType = mediaTypeOf(g.Update)
			res.CRUDMapping.Update = &upd
		}
		res.CRUDMapping.Delete = operationMapping("DELETE", g.InstancePath, parserOp(spec, g.InstancePath, "DELETE"), envelopeOf(g.Delete))
		res.CRUDMapping.Delete.MediaType = mediaTypeOf(g.Delete)

		schema, idAttr := transformer.ManagedResourceSchema(g)
		res.Schema = schema
		res.IDAttribute = idAttr
		// Import wiring for grouped resources (REMAINING_GAPS §3/#10). A grouped
		// resource is importable when its identifier attribute(s) are real schema
		// attributes the import can populate. InferResourceCRUD expresses import
		// ids as a printf format ("%s:%s"); the generator expects brace-enclosed
		// attribute references ("{a}:{b}"), so the format is rebuilt here from the
		// CRUD group's path parameters in snake_case. A simple id references the
		// single identifier attribute; a composite id references every path
		// parameter and is only emitted when all of them are present as top-level
		// schema attributes (otherwise the import would target attributes that do
		// not exist, so the resource stays non-importable — honest, not silent).
		if importFmt, ok := groupedImportFormat(g, schema, idAttr); ok {
			res.ImportIDFormat = importFmt
			res.Importable = true
		}

		resources = append(resources, res)
		markConsumed(consumed, g.CollectionPath, "POST")
		markConsumed(consumed, g.InstancePath, "GET")
		if g.FullUpdate != nil {
			markConsumed(consumed, g.InstancePath, "PUT")
		}
		if g.PartialUpdate != nil {
			markConsumed(consumed, g.InstancePath, "PATCH")
		}
		markConsumed(consumed, g.InstancePath, "DELETE")
	}

	sort.Slice(resources, func(i, j int) bool { return resources[i].Name < resources[j].Name })
	return resources, consumed
}

// resolveOperationByID returns the path, method, and transformer Operation for
// an operationId, or nil when no operation matches.
func resolveOperationByID(pathOps map[string]map[transformer.HTTPMethod]transformer.Operation, operationID string) (string, string, *transformer.Operation) {
	if operationID == "" {
		return "", "", nil
	}
	for path, ops := range pathOps {
		for method, op := range ops {
			if op.OperationID == operationID {
				cp := op
				return path, string(method), &cp
			}
		}
	}
	return "", "", nil
}

// applyResourceCreationOverrides promotes operations to managed resources when
// a resource_override targets an operation that inference classified as an
// action (G8). The override may specify explicit read/update/delete operations
// for entities whose create path differs from their read/delete path (e.g.
// MyCloud dashboards: POST /dashboards/db vs GET|DELETE /dashboards/uid/{uid}).
// The generated resource is wired to the resolved operations and its schema is
// reconciled from the create request body and the read response. Operations the
// override consumes are marked so the per-operation pass does not double-emit
// them as actions.
func applyResourceCreationOverrides(preview *ir.ProviderIR, spec *parser.Spec, providerName string, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation, overrides []config.ResourceOverride, consumed map[string]map[string]bool) {
	for _, ro := range overrides {
		gen := ro.GenerateResource != nil && *ro.GenerateResource
		explicit := ro.CreateOperation != "" || ro.ReadOperation != "" || ro.UpdateOperation != "" || ro.DeleteOperation != ""
		if !gen && !explicit {
			continue
		}
		seed := ro.Operation
		if seed == "" {
			seed = ro.CreateOperation
		}
		if seed == "" {
			continue
		}
		createPath, createMethod, createOp := resolveOperationByID(pathOps, seed)
		if createOp == nil {
			continue
		}
		// Skip if the seed operation is already consumed by an inferred resource
		// (the existing applyResourceOverrides mutates those).
		if isConsumed(consumed, createPath, createMethod) {
			continue
		}
		readPath, _, readOp := resolveOperationByID(pathOps, ro.ReadOperation)
		updatePath, updateMethod, updateOp := resolveOperationByID(pathOps, ro.UpdateOperation)
		deletePath, deleteMethod, deleteOp := resolveOperationByID(pathOps, ro.DeleteOperation)

		g := transformer.ResourceCRUD{
			Name:           resourceNameFromOverride(ro, createPath),
			CollectionPath: createPath,
			InstancePath:   readPath,
			Create:         createOp,
			Read:           readOp,
			Update:         updateOp,
			Delete:         deleteOp,
		}
		res := resourceFromOverrideCRUD(spec, providerName, g)
		if res == nil {
			continue
		}
		if strings.TrimSpace(ro.IDAttribute) != "" {
			res.IDAttribute = ro.IDAttribute
		}
		preview.Resources = append(preview.Resources, *res)
		markConsumed(consumed, createPath, createMethod)
		if readPath != "" {
			markConsumed(consumed, readPath, "GET")
		}
		if updatePath != "" {
			markConsumed(consumed, updatePath, updateMethod)
		}
		if deletePath != "" {
			// The delete operation may be a non-DELETE method (e.g. SpaceTraders
			// scraps a ship via POST /my/ships/{shipSymbol}/scrap); consume the
			// actual method so the per-operation pass does not double-emit it as an
			// action.
			markConsumed(consumed, deletePath, deleteMethod)
		}
	}
}

// resourceNameFromOverride returns the resource name for an override-created
// resource: the override's resource_name, else the last non-param segment of
// the create path.
func resourceNameFromOverride(ro config.ResourceOverride, createPath string) string {
	if strings.TrimSpace(ro.ResourceName) != "" {
		return strings.TrimSpace(ro.ResourceName)
	}
	segs := strings.Split(strings.Trim(createPath, "/"), "/")
	for i := len(segs) - 1; i >= 0; i-- {
		if !strings.HasPrefix(segs[i], "{") {
			return transformer.ToSnakeCase(segs[i])
		}
	}
	return "resource"
}

// resourceFromOverrideCRUD builds a managed ResourceIR from a CRUD group whose
// operations were resolved from an override (G8). Unlike inferred groups, the
// create/read/update/delete paths may differ (e.g. dashboards).
func resourceFromOverrideCRUD(spec *parser.Spec, providerName string, g transformer.ResourceCRUD) *ir.ResourceIR {
	res := ir.ResourceIR{
		Name:            g.Name,
		FullName:        providerName + "_" + g.Name,
		TypeName:        providerName + "_" + g.Name,
		SourceOperation: groupSourceOperation(g),
	}
	if g.Create != nil {
		res.CRUDMapping.Create = operationMapping(string(g.Create.Method), g.Create.Path, parserOp(spec, g.Create.Path, string(g.Create.Method)), envelopeOf(g.Create))
		res.CRUDMapping.Create.MediaType = mediaTypeOf(g.Create)
	}
	if g.Read != nil {
		res.CRUDMapping.Read = operationMapping(string(g.Read.Method), g.Read.Path, parserOp(spec, g.Read.Path, string(g.Read.Method)), envelopeOf(g.Read))
		res.CRUDMapping.Read.MediaType = mediaTypeOf(g.Read)
	}
	if g.Update != nil {
		upd := operationMapping(string(g.Update.Method), g.Update.Path, parserOp(spec, g.Update.Path, string(g.Update.Method)), envelopeOf(g.Update))
		upd.MediaType = mediaTypeOf(g.Update)
		res.CRUDMapping.Update = &upd
	}
	if g.Delete != nil {
		res.CRUDMapping.Delete = operationMapping(string(g.Delete.Method), g.Delete.Path, parserOp(spec, g.Delete.Path, string(g.Delete.Method)), envelopeOf(g.Delete))
		res.CRUDMapping.Delete.MediaType = mediaTypeOf(g.Delete)
	}
	schema, idAttr := transformer.ManagedResourceSchema(g)
	res.Schema = schema
	res.IDAttribute = idAttr
	return &res
}

// mediaTypeOf returns the request body media type carried on a transformer
// Operation (resolved from the spec's request body content map, application/json
// preferred). It is nil-safe so a CRUD group missing one step (rare) yields the
// empty media type the generator treats as JSON. The media type drives the
// generated body encoding: JSON, form-urlencoded, multipart, or XML (A2).
func mediaTypeOf(op *transformer.Operation) string {
	if op == nil {
		return ""
	}
	return op.RequestMediaType
}

// envelopeOf returns the response-envelope key carried on a transformer
// Operation (the {data: ...} property the transformer flattened out of the
// response schema). It is nil-safe so a CRUD group missing one step yields the
// empty key the generator treats as "no envelope".
func envelopeOf(op *transformer.Operation) string {
	if op == nil {
		return ""
	}
	return op.ResponseEnvelope
}

// envelopeOfTransformerOp returns the response-envelope key for a path/method
// pair from the transformer path-operation map, or "" when the pair is absent.
func envelopeOfTransformerOp(pathOps map[string]map[transformer.HTTPMethod]transformer.Operation, path, method string) string {
	if top := lookupTransformerOp(pathOps, path, method); top != nil {
		return top.ResponseEnvelope
	}
	return ""
}

// groupedImportFormat builds the brace-enclosed import ID format for a grouped
// resource and reports whether the resource is importable. A simple-id resource
// is importable when its single identifier attribute exists in the schema; the
// format is "{<idAttr>}" (or "{id}" when the generator defaults the attribute).
// A composite-id resource is importable only when every path parameter is a
// top-level schema attribute (so each import segment targets a real attribute);
// the format joins them as "{p1}:{p2}:...". When the identifier attributes are
// not present (e.g. a nested-response resource whose path parameters are not
// top-level fields), the resource is not importable — honest, not silent.
func groupedImportFormat(g transformer.ResourceCRUD, schema ir.ObjectSchemaIR, idAttr string) (string, bool) {
	hasAttr := func(name string) bool {
		for _, a := range schema.Attributes {
			if a.Name == name {
				return true
			}
		}
		return false
	}

	switch g.ID.Kind {
	case transformer.IDComposite:
		if len(g.ID.ParameterNames) == 0 {
			return "", false
		}
		parts := make([]string, 0, len(g.ID.ParameterNames))
		for _, p := range g.ID.ParameterNames {
			snake := transformer.ToSnakeCase(p)
			if !hasAttr(snake) {
				return "", false
			}
			parts = append(parts, "{"+snake+"}")
		}
		return strings.Join(parts, ":"), true
	default: // IDSimple
		effective := idAttr
		if effective == "" {
			effective = "id"
		}
		if !hasAttr(effective) {
			return "", false
		}
		return "{" + effective + "}", true
	}
}

// groupIsResource reports whether every CRUD operation in the group classifies as
// a resource under the explicit-extension / path-keyword rules. It returns false
// when any operation is claimed as an action, ephemeral, function, or list. The
// method-based kindResource/kindDataSource distinction is deliberately not checked
// here: grouping reclassifies an instance GET as a resource Read.
func groupIsResource(spec *parser.Spec, g transformer.ResourceCRUD, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation) bool {
	type opRef struct {
		path, method string
	}
	opRefs := []opRef{
		{g.CollectionPath, "POST"},
		{g.InstancePath, "GET"},
		{g.InstancePath, "DELETE"},
	}
	if g.FullUpdate != nil {
		opRefs = append(opRefs, opRef{g.InstancePath, "PUT"})
	}
	if g.PartialUpdate != nil {
		opRefs = append(opRefs, opRef{g.InstancePath, "PATCH"})
	}
	for _, ref := range opRefs {
		op := parserOp(spec, ref.path, ref.method)
		if op == nil {
			return false
		}
		// checkFullCRUD is false here: the group's operations are already known
		// to form a complete CRUD group, so the CRUD-completeness reclassification
		// (which turns a partial-update PATCH into an action) must not veto the
		// group. Only explicit extensions and path keywords reject a group.
		switch classifyOperation(ref.path, ref.method, op, pathOps, false) {
		case kindAction, kindEphemeral, kindFunction, kindListResource:
			return false
		}
	}
	return true
}

// groupSourceOperation returns the operation id that best identifies the group,
// preferring the Create operation id.
func groupSourceOperation(g transformer.ResourceCRUD) string {
	if g.Create != nil && strings.TrimSpace(g.Create.OperationID) != "" {
		return g.Create.OperationID
	}
	if g.Read != nil && strings.TrimSpace(g.Read.OperationID) != "" {
		return g.Read.OperationID
	}
	if g.Delete != nil && strings.TrimSpace(g.Delete.OperationID) != "" {
		return g.Delete.OperationID
	}
	return ""
}

// parserOp returns the parser operation for a (path, method) pair, or nil when
// the path or method is absent. method is matched case-insensitively.
func parserOp(spec *parser.Spec, path, method string) *parser.Operation {
	if spec == nil || spec.Paths == nil {
		return nil
	}
	pi := spec.Paths[path]
	if pi == nil {
		return nil
	}
	switch strings.ToUpper(method) {
	case "GET":
		return pi.Get
	case "POST":
		return pi.Post
	case "PUT":
		return pi.Put
	case "PATCH":
		return pi.Patch
	case "DELETE":
		return pi.Delete
	}
	return nil
}

// consumeEphemeralLifecycleOps marks the sibling lifecycle operations an
// ephemeral resource claims as its Renew/Close mappings as consumed, matching
// the paths ephemeralFromOperation looks up (POST <path>/renew, DELETE
// <path>/close, DELETE <path>/revoke). Consuming is a no-op for undeclared
// paths.
func consumeEphemeralLifecycleOps(consumed map[string]map[string]bool, path string) {
	markConsumed(consumed, path+"/renew", "POST")
	markConsumed(consumed, path+"/close", "DELETE")
	markConsumed(consumed, path+"/revoke", "DELETE")
}

// markConsumed records that (path, method) is part of a grouped resource.
func markConsumed(consumed map[string]map[string]bool, path, method string) {
	methods := consumed[path]
	if methods == nil {
		methods = make(map[string]bool)
		consumed[path] = methods
	}
	methods[strings.ToUpper(method)] = true
}

// isConsumed reports whether (path, method) was consumed by a grouped resource.
func isConsumed(consumed map[string]map[string]bool, path, method string) bool {
	if consumed == nil {
		return false
	}
	return consumed[path][strings.ToUpper(method)]
}

func resourceFromOperation(op *parser.Operation, providerName, path, method string, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation) ir.ResourceIR {
	name := resourceName(op, method, path)
	res := ir.ResourceIR{
		Name:            name,
		FullName:        providerName + "_" + name,
		TypeName:        providerName + "_" + name,
		Description:     op.Description,
		SourceOperation: op.OperationID,
	}
	mapping := operationMapping(method, path, op, envelopeOfTransformerOp(pathOps, path, method))
	switch method {
	case "POST":
		res.CRUDMapping.Create = mapping
	case "PUT", "PATCH":
		// mapping is a fresh value on each call, so taking its address here is
		// safe and avoids the redundant local copy.
		res.CRUDMapping.Update = &mapping
	case "DELETE":
		res.CRUDMapping.Delete = mapping
	}
	return res
}

func dataSourceFromOperation(op *parser.Operation, providerName, path, method string, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation, pagination *ir.PaginationIR) ir.DataSourceIR {
	name := resourceName(op, method, path)
	ds := ir.DataSourceIR{
		Name:            name,
		FullName:        providerName + "_" + name,
		TypeName:        providerName + "_" + name,
		Description:     op.Description,
		SourceOperation: op.OperationID,
		ReadMapping:     operationMapping(method, path, op, envelopeOfTransformerOp(pathOps, path, method)),
	}
	// Build the data source schema from the resolved read operation so the
	// generator can wire Read against real filter/output attributes instead of
	// an empty schema (REMAINING_GAPS §4). pathOps carries the operation's
	// resolved response schema and merged parameters; when the operation is not
	// present there (e.g. an unsupported method), the schema stays empty and the
	// generator keeps the honest scaffold Read body.
	if top := lookupTransformerOp(pathOps, path, method); top != nil {
		ds.Schema = transformer.DataSourceSchema(*top)
		// A top-level array response marks a list data source: the generator
		// wires its Read to fetch (and paginate) the pages and expose them as a
		// Computed `items` List attribute (REMAINING_GAPS §2/§4). Carry the
		// provider pagination strategy so the wired body can follow pages.
		if top.ResponseSchema != nil && strings.EqualFold(top.ResponseSchema.Type, "array") {
			ds.IsList = true
			ds.Pagination = pagination
		}
	}
	return ds
}

// lookupTransformerOp returns the transformer Operation for a path/method pair,
// or nil when the pair is absent from the map (e.g. an unsupported method that
// OperationsFromSpecWithDiagnostics did not normalize).
func lookupTransformerOp(pathOps map[string]map[transformer.HTTPMethod]transformer.Operation, path, method string) *transformer.Operation {
	methods, ok := pathOps[path]
	if !ok {
		return nil
	}
	hm := transformer.HTTPMethod(strings.ToUpper(method))
	op, ok := methods[hm]
	if !ok {
		return nil
	}
	return &op
}

func resourceName(op *parser.Operation, method, path string) string {
	if op != nil && strings.TrimSpace(op.OperationID) != "" {
		return transformer.ToSnakeCase(op.OperationID)
	}
	return transformer.NormalizeOperationID("", method, path)
}

func operationMapping(method, path string, op *parser.Operation, responseEnvelope string) ir.OperationMappingIR {
	query, header, cookie, formData := paramsFromOperation(op)
	m := ir.OperationMappingIR{
		Method:               method,
		PathTemplate:         path,
		SuccessCodes:         successCodesFromResponses(op, method),
		ErrorMappings:        errorMappingsFromResponses(op),
		QueryParams:          query,
		HeaderParams:         header,
		CookieParams:         cookie,
		FormDataParams:       formData,
		SecurityRequirements: securityRequirementsFromOp(op),
		ResponseEnvelope:     responseEnvelope,
	}
	// A declared request body disables bodiless wiring for actions, list
	// resources, and ephemeral resources: the generated client only encodes
	// request bodies through the resource CRUD path, so a bodiless call would
	// silently drop the practitioner's request body. Carry a non-nil BodySchema
	// so the generator's wiring gates (planActionWiring / planListWiring /
	// planEphemeralWiring) keep those constructs honestly scaffolded. The value
	// is a presence marker: no generator path reads BodySchema's contents, and
	// resource CRUD bodies get their schema from the transformer's RequestSchema.
	if op != nil && op.RequestBody != nil && len(op.RequestBody.Content) > 0 {
		m.BodySchema = &ir.SchemaIR{}
	}
	return m
}

// securityRequirementsFromOp carries the operation's declared security
// requirements (per-operation `security`) into the IR as a list of
// alternatives, each a map of security scheme name to scopes. An operation that
// declares no security returns nil, which the generator interprets as
// "inherit the global default" (apply all configured interceptors). An
// operation declaring exactly one requirement gets per-operation AND resolution
// (only that requirement's scheme interceptors apply); more than one (OR) is
// warned by warnPerOpORSecurity and applies all configured interceptors
// (REMAINING_GAPS §1).
func securityRequirementsFromOp(op *parser.Operation) []map[string][]string {
	if op == nil || len(op.Security) == 0 {
		return nil
	}
	out := make([]map[string][]string, 0, len(op.Security))
	for _, req := range op.Security {
		if req.Requirements == nil {
			// An empty requirement object {} marks the operation as allowing
			// unauthenticated access; preserve it as an empty map.
			out = append(out, map[string][]string{})
			continue
		}
		c := make(map[string][]string, len(req.Requirements))
		for schemeName, scopes := range req.Requirements {
			c[schemeName] = scopes
		}
		out = append(out, c)
	}
	return out
}

func defaultSuccessCodes(method string) []int {
	switch method {
	case "POST":
		return []int{201, 200}
	case "DELETE":
		return []int{204, 200}
	default:
		return []int{200}
	}
}

// mergePathParams returns a shallow copy of op with the path item's parameters
// merged into the operation's own. OpenAPI inherits a path item's parameters
// onto every operation; an operation-level parameter of the same name overrides
// the path-level definition. Only Parameters is rebuilt; the rest of the
// operation is shared, so this is safe for read-only downstream use.
func mergePathParams(op *parser.Operation, pathParams []parser.Parameter) *parser.Operation {
	if op == nil || len(pathParams) == 0 {
		return op
	}
	byName := make(map[string]struct{}, len(op.Parameters))
	for _, p := range op.Parameters {
		byName[p.Name] = struct{}{}
	}
	merged := make([]parser.Parameter, 0, len(op.Parameters)+len(pathParams))
	merged = append(merged, op.Parameters...)
	for _, p := range pathParams {
		if _, dup := byName[p.Name]; dup {
			continue
		}
		merged = append(merged, p)
	}
	if len(merged) == len(op.Parameters) {
		return op
	}
	out := *op
	out.Parameters = merged
	return &out
}

// paramsFromOperation splits an operation's parameters into query, header,
// cookie, and formData ParamIRs. Path parameters are not surfaced here: wired
// bodies substitute path placeholders from the resource schema directly (see
// resolvePathSubstitution), so carrying them on the mapping would be redundant.
// Cookie parameters are wired onto the request as Cookie headers (see
// requestCookieStmts). formData parameters are not wired — the generated request
// body only encodes JSON, and form-encoded bodies are a separate effort — so
// they are surfaced on the mapping (FormDataParams) for the generator to keep
// the operation honestly scaffolded, and a fail-loud warning is emitted in the
// transformer so the construct is never silently dropped (REMAINING_GAPS §2).
func paramsFromOperation(op *parser.Operation) (query, header, cookie, formData []ir.ParamIR) {
	if op == nil {
		return nil, nil, nil, nil
	}
	for _, p := range op.Parameters {
		switch strings.ToLower(p.In) {
		case "query":
			query = append(query, paramIR(p))
		case "header":
			header = append(header, paramIR(p))
		case "cookie":
			cookie = append(cookie, paramIR(p))
		case "formdata":
			formData = append(formData, paramIR(p))
		}
	}
	// OpenAPI 2.0 formData parameters are normalized by the v2 parser into a
	// request-body content schema (and dropped from op.Parameters); decompose
	// that schema back into per-field parameters so grouped v2 resources
	// populate FormDataParams and their form/multipart bodies wire
	// (PROJECT_DESIGN §23). The media type itself is already carried on
	// OperationMappingIR.MediaType.
	if len(formData) == 0 {
		formData = append(formData, v2FormDataParams(op)...)
	}
	return query, header, cookie, formData
}

// v2FormDataParams decomposes an OpenAPI 2.0 formData request body (the
// form/multipart content schema buildFormDataContent built from the operation's
// in: formData parameters) back into per-field ParamIRs. Without this
// decomposition FormDataParams stays empty for grouped v2 resources and the
// generator's form/multipart body builders (formBodyStmts, multipartBodyStmts)
// never fire, so the create/update bodies stay honestly scaffolded even though
// the IR-level machinery is implemented and tested (PROJECT_DESIGN §23).
func v2FormDataParams(op *parser.Operation) []ir.ParamIR {
	if op == nil || op.RequestBody == nil {
		return nil
	}
	for _, ct := range []string{"application/x-www-form-urlencoded", "multipart/form-data"} {
		mt := op.RequestBody.Content[ct]
		if mt == nil || mt.Schema == nil || len(mt.Schema.Properties) == 0 {
			continue
		}
		required := make(map[string]bool, len(mt.Schema.Required))
		for _, name := range mt.Schema.Required {
			required[name] = true
		}
		names := make([]string, 0, len(mt.Schema.Properties))
		for name := range mt.Schema.Properties {
			names = append(names, name)
		}
		sort.Strings(names)
		out := make([]ir.ParamIR, 0, len(names))
		for _, name := range names {
			prop := mt.Schema.Properties[name]
			out = append(out, ir.ParamIR{
				Name:     name,
				In:       "formdata",
				Required: required[name],
				Schema:   ir.SchemaIR{Type: paramPrimitiveType(prop), Format: prop.Format},
			})
		}
		return out
	}
	return nil
}

// paramIR converts a parser parameter to its IR form, carrying a best-effort
// primitive schema type so downstream consumers can distinguish scalar
// parameters from structured ones.
func paramIR(p parser.Parameter) ir.ParamIR {
	return ir.ParamIR{
		Name:        p.Name,
		In:          p.In,
		Description: p.Description,
		Required:    p.Required,
		Schema:      ir.SchemaIR{Type: paramPrimitiveType(p.Schema), Format: paramFormat(p.Schema)},
		Deprecated:  p.Deprecated,
	}
}

// paramFormat carries a parameter schema's OpenAPI format onto the IR so the
// generator can distinguish binary string formData (format: binary → multipart
// file part) from plain string formData (form text field) when the operation's
// media type is multipart/form-data (A2).
func paramFormat(schema *parser.Schema) string {
	if schema == nil {
		return ""
	}
	return schema.Format
}

// paramPrimitiveType maps a parameter's schema to an IR primitive type when it
// is a simple scalar. Non-scalar or absent schemas yield an empty type so the
// generator treats the parameter as unmapped unless a same-named schema
// attribute supplies the value.
func paramPrimitiveType(schema *parser.Schema) ir.PrimitiveType {
	if schema == nil {
		return ""
	}
	switch schemaTypeString(schema) {
	case "string":
		return ir.TypeString
	case "integer":
		return ir.TypeInt
	case "number":
		return ir.TypeFloat
	case "boolean":
		return ir.TypeBool
	}
	return ""
}

// schemaTypeString extracts the type name from a parser schema whose Type is
// either a string (OpenAPI 2.0/3.0) or a slice (3.1, where it may list several
// types). The first non-null type is used; an absent or empty type yields "".
func schemaTypeString(schema *parser.Schema) string {
	switch v := schema.Type.(type) {
	case string:
		return v
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && s != "null" {
				return s
			}
		}
	case []string:
		for _, s := range v {
			if s != "null" {
				return s
			}
		}
	}
	return ""
}

// successCodesFromResponses collects the 2xx status codes an operation declares.
// When the operation declares none it falls back to the method's default
// success codes, preserving the pre-§2 behavior for specs that omit response
// codes. Codes are sorted ascending so generation stays deterministic.
func successCodesFromResponses(op *parser.Operation, method string) []int {
	if op == nil || len(op.Responses) == 0 {
		return defaultSuccessCodes(method)
	}
	var codes []int
	for code := range op.Responses {
		n, ok := parseStatusCode(code)
		if !ok || n < 200 || n >= 300 {
			continue
		}
		codes = append(codes, n)
	}
	if len(codes) == 0 {
		return defaultSuccessCodes(method)
	}
	sort.Ints(codes)
	return codes
}

// errorMappingsFromResponses collects 4xx/5xx response codes with their spec
// descriptions, so generated wired bodies can surface per-code diagnostics
// instead of a generic client error. Returns nil when there are none.
func errorMappingsFromResponses(op *parser.Operation) map[int]ir.ErrorMappingIR {
	if op == nil || len(op.Responses) == 0 {
		return nil
	}
	m := make(map[int]ir.ErrorMappingIR)
	for code, resp := range op.Responses {
		n, ok := parseStatusCode(code)
		if !ok || n < 400 {
			continue
		}
		desc := ""
		if resp != nil {
			desc = resp.Description
		}
		m[n] = ir.ErrorMappingIR{StatusCode: n, Description: desc}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// parseStatusCode parses a numeric HTTP status code key. Non-numeric keys
// ("default"), range keys ("2xx"), and out-of-range values are rejected.
func parseStatusCode(code string) (int, bool) {
	n, err := strconv.Atoi(code)
	if err != nil || n < 100 || n > 599 {
		return 0, false
	}
	return n, true
}

// operationKind classifies an OpenAPI operation into the Terraform construct it
// most naturally represents. It is used by buildIRPreview to infer actions,
// ephemeral resources, list resources, and provider-defined functions in addition
// to standard managed resources and data sources.
type operationKind string

const (
	kindResource     operationKind = "resource"
	kindDataSource   operationKind = "data_source"
	kindAction       operationKind = "action"
	kindEphemeral    operationKind = "ephemeral"
	kindListResource operationKind = "list_resource"
	kindFunction     operationKind = "function"
	kindUnknown      operationKind = "unknown"
)

var (
	functionKeywords  = []string{"search", "compute", "calculate", "convert", "lookup", "query"}
	ephemeralKeywords = []string{"credentials", "token", "session", "lease", "ticket"}
)

// classifyOperation maps an OpenAPI path/method/operation to a Terraform
// construct kind. The heuristics intentionally mirror the mappings documented
// in PROJECT_DESIGN.md sections 8.7-8.10 and can be overridden via generator.yaml.
// pathOps is the transformer's operation map, shared so POST classification
// agrees with transformer.InferActions instead of running a parallel heuristic.
func classifyOperation(path, method string, op *parser.Operation, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation, checkFullCRUD bool) operationKind {
	method = strings.ToUpper(method)

	if k, ok := extensionKind(op); ok {
		return k
	}

	pathLower := strings.ToLower(path)
	if k, ok := keywordKind(method, pathLower); ok {
		return k
	}

	return methodKind(method, path, op, pathOps, checkFullCRUD)
}

// extensionKind returns the kind implied by explicit Terraform extension keys,
// if any are present on the operation.
func extensionKind(op *parser.Operation) (operationKind, bool) {
	if op == nil {
		return "", false
	}
	extensions := []struct {
		key  string
		kind operationKind
	}{
		{"x-terraform-action", kindAction},
		{"x-terraform-ephemeral", kindEphemeral},
		{"x-terraform-list", kindListResource},
		{"x-terraform-function", kindFunction},
	}
	for _, ext := range extensions {
		if extensionBool(op.Extensions[ext.key]) {
			return ext.kind, true
		}
	}
	return "", false
}

// extensionBool reports whether an x-terraform-* extension value is truthy. A
// literal YAML/JSON boolean true is honored, and a quoted string "true" is also
// honored (case-insensitive) so that JSON specs carrying "true" as a string are
// not silently ignored (L-13). Any other value (including "false", 1, or a
// non-bool object) is treated as not set, with no diagnostic.
func extensionBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return strings.EqualFold(x, "true")
	}
	return false
}

// keywordKind returns the kind implied by path keywords for provider-defined
// functions and ephemeral resources, if any match.
func keywordKind(method, pathLower string) (operationKind, bool) {
	// Provider-defined functions: read-only compute/query endpoints.
	if method == "GET" && containsAny(pathLower, functionKeywords) {
		return kindFunction, true
	}
	// Ephemeral resources: temporary credentials/tokens/sessions. A lifecycle
	// subpath (renew/close/revoke/refresh/rotate) is the sibling lifecycle
	// operation of an ephemeral resource, not an ephemeral open itself — it is
	// wired as the Renew/Close mapping by ephemeralFromOperation.
	if method == "POST" && containsAny(pathLower, ephemeralKeywords) && !transformer.IsLifecycleSubpath(pathLower) {
		return kindEphemeral, true
	}
	return "", false
}

// methodKind classifies an operation based on its HTTP method and path shape.
// POST classification is unified with transformer.InferActions: a POST that is
// not a managed-resource Create (no instance subpath extends the path) is an
// action, as is a POST whose trailing path segment or operationId leading verb
// is a recognized action verb. Only a POST that is a CRUD Create stays a
// (partial) resource.
func methodKind(method, path string, op *parser.Operation, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation, checkFullCRUD bool) operationKind {
	itemPath := isItemPath(path)
	lastSegment := lastPathSegment(path)
	lastLower := strings.ToLower(lastSegment)
	isVerb := lastSegment != "" && !strings.HasPrefix(lastSegment, "{") && isVerbPathSegment(lastLower)

	switch method {
	case "GET":
		// Collection reads are surfaced as data sources to preserve the
		// legacy IR contract used by existing golden snapshots. Explicit
		// x-terraform-list extensions are still honored above, and a collection
		// GET paired with an instance Read is additionally promoted to a list
		// resource by addPathOperations (additive: the data source is kept).
		return kindDataSource
	case "POST":
		if itemPath && isVerb {
			return kindAction
		}
		if operationIDVerb(op) != "" {
			return kindAction
		}
		if !transformer.IsCRUDCreatePath(path, pathOps) {
			return kindAction
		}
		// A POST that is a CRUD Create but whose group lacks a full
		// Create+Read+Delete mapping is reclassified as an action: a scaffolded
		// resource with an empty model is worse than a wired action, and a
		// resource without a Delete cannot be destroyed by Terraform. The check
		// is skipped when classifying a group's own operations (groupIsResource),
		// which are already known to form a complete CRUD group.
		if checkFullCRUD && !transformer.HasFullCRUD(path, transformer.HTTPMethod(method), pathOps) {
			return kindAction
		}
		return kindResource
	case "PUT", "PATCH":
		if itemPath {
			if checkFullCRUD && !transformer.HasFullCRUD(path, transformer.HTTPMethod(method), pathOps) {
				return kindAction
			}
			return kindResource
		}
		// PUT/PATCH on a collection is an unusual bulk update; treat as action.
		return kindAction
	case "DELETE":
		if itemPath {
			if checkFullCRUD && !transformer.HasFullCRUD(path, transformer.HTTPMethod(method), pathOps) {
				return kindAction
			}
			return kindResource
		}
		// DELETE on a collection is a bulk/clear action.
		return kindAction
	case "OPTIONS", "HEAD":
		return kindUnknown
	}

	if isVerb {
		return kindAction
	}
	return kindUnknown
}

// containsAny reports whether s contains any of the given substrings.
func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// lastPathSegment returns the final slash-delimited segment of path, with any
// trailing slash removed. It returns "" for the root path.
func lastPathSegment(path string) string {
	path = strings.TrimSuffix(path, "/")
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return path
	}
	return path[idx+1:]
}

// verbPathSegments is a heuristic set of path segments that indicate an action
// rather than a resource lifecycle operation.
var verbPathSegments = map[string]struct{}{
	"adopt": {}, "archive": {}, "ban": {}, "cancel": {}, "check": {},
	"clone": {}, "deploy": {}, "disable": {}, "enable": {}, "execute": {},
	"export": {}, "import": {}, "lock": {}, "move": {}, "pause": {},
	"publish": {}, "reboot": {}, "refresh": {}, "renew": {}, "reset": {},
	"restart": {}, "restore": {}, "resume": {}, "revoke": {}, "rotate": {},
	"run": {}, "scale": {}, "schedule": {}, "send": {}, "start": {},
	"stop": {}, "submit": {}, "suspend": {}, "sync": {}, "trigger": {},
	"unlock": {}, "upgrade": {}, "validate": {}, "verify": {},
}

func isVerbPathSegment(segment string) bool {
	_, ok := verbPathSegments[segment]
	return ok
}

// operationIDVerb returns the leading verb token of the operation's ID when it
// is a recognized action verb — "rebootServer" and "reboot_server" both yield
// "reboot" — or "" otherwise. It lets a POST classify as an action when the
// path's trailing segment is not a verb but the operationId leads with one.
func operationIDVerb(op *parser.Operation) string {
	if op == nil || strings.TrimSpace(op.OperationID) == "" {
		return ""
	}
	verb, _, _ := strings.Cut(transformer.ToSnakeCase(op.OperationID), "_")
	if isVerbPathSegment(verb) {
		return verb
	}
	return ""
}

func actionFromOperation(op *parser.Operation, providerName, path, method string, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation) ir.ActionIR {
	name := resourceName(op, method, path)
	// Build the config schema from the operation's parameters AND request-body
	// properties (via the transformer's ObjectSchemaFromOperation, the same
	// builder the ephemeral path uses) so a body-bearing action — even one the
	// generator keeps honestly scaffolded because the client cannot send its
	// body — still surfaces its intended inputs to the practitioner. Without
	// this, the essential register action would present an empty schema.
	configSchema := ir.ObjectSchemaIR{Attributes: actionConfigAttributes(op)}
	if top := lookupTransformerOp(pathOps, path, method); top != nil {
		configSchema = transformer.ObjectSchemaFromOperation(*top)
	}
	return ir.ActionIR{
		Name:            name,
		FullName:        providerName + "_" + name,
		TypeName:        providerName + "_" + name,
		Description:     op.Description,
		SourceOperation: op.OperationID,
		// Actions have no result surface: the wired Invoke neither decodes a
		// response nor sets a result, so no response envelope is carried.
		InvokeMapping: operationMapping(method, path, op, ""),
		ConfigSchema:  configSchema,
	}
}

// actionConfigAttributes builds the config schema attributes for an
// auto-inferred action from the operation's path, query, header, and cookie
// parameters. Each parameter becomes a practitioner-set config attribute the
// wired Invoke body sends on the request: path parameters are Required (an
// OpenAPI path parameter is always required), and query/header/cookie
// parameters preserve their declared Required flag. formData parameters are
// intentionally not surfaced here because the generated client only encodes
// JSON bodies; an action with formData parameters stays honestly scaffolded
// (REMAINING_GAPS §2). The request body is not modeled for auto-inferred
// actions either: operationMapping carries a non-nil BodySchema for body-bearing
// operations, so the generator keeps such actions honestly scaffolded rather
// than wiring a bodiless Invoke that would drop the practitioner's body. Only a
// truly bodiless action is the wired shape. Non-primitive parameters yield an
// empty type, which the generator treats as unmapped and keeps the action
// honestly scaffolded rather than wiring a body that cannot send them.
func actionConfigAttributes(op *parser.Operation) []ir.AttributeIR {
	if op == nil {
		return nil
	}
	var attrs []ir.AttributeIR
	for _, p := range op.Parameters {
		switch strings.ToLower(p.In) {
		case "path":
			attrs = append(attrs, ir.AttributeIR{
				Name:        transformer.SanitizeAttributeName(p.Name),
				WireName:    p.Name,
				Description: p.Description,
				Required:    true,
				Schema:      ir.SchemaIR{Type: paramPrimitiveType(p.Schema)},
				Deprecated:  p.Deprecated,
			})
		case "query", "header", "cookie":
			attrs = append(attrs, ir.AttributeIR{
				Name:        transformer.SanitizeAttributeName(p.Name),
				WireName:    p.Name,
				Description: p.Description,
				Required:    p.Required,
				Schema:      ir.SchemaIR{Type: paramPrimitiveType(p.Schema)},
				Deprecated:  p.Deprecated,
			})
		}
	}
	return attrs
}

func ephemeralFromOperation(spec *parser.Spec, op *parser.Operation, providerName, path, method string, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation) ir.EphemeralResourceIR {
	name := resourceName(op, method, path)
	er := ir.EphemeralResourceIR{
		Name:            name,
		FullName:        providerName + "_" + name,
		TypeName:        providerName + "_" + name,
		Description:     op.Description,
		SourceOperation: op.OperationID,
		OpenMapping:     operationMapping(method, path, op, envelopeOfTransformerOp(pathOps, path, method)),
	}
	// Build the config (input) schema from the open operation's path/query/header
	// parameters and the result (output) schema from its resolved response, so an
	// inferred ephemeral resource can wire its Open/Renew/Close bodies instead of
	// keeping an empty-schema scaffold (PROJECT_DESIGN §23). The transformer exposes
	// the same builders its own inferEphemeralResource uses.
	if top := lookupTransformerOp(pathOps, path, method); top != nil {
		er.ConfigSchema = transformer.ObjectSchemaFromOperation(*top)
		er.ResultSchema = transformer.ResultSchemaFromResponse(top.ResponseSchema)
	}
	// Renew/Close are wired to the sibling lifecycle operations when the spec
	// declares them (POST <path>/renew, DELETE <path>/close, with DELETE
	// <path>/revoke as the close fallback), mirroring the transformer's
	// inferEphemeralResource. When no such operations exist the mappings stay
	// unset — never a placeholder pointing at the open operation.
	if renewOp := parserOp(spec, path+"/renew", "POST"); renewOp != nil {
		rm := operationMapping("POST", path+"/renew", renewOp, "")
		er.RenewMapping = &rm
		er.HasRenew = true
	}
	if closeOp := parserOp(spec, path+"/close", "DELETE"); closeOp != nil {
		cm := operationMapping("DELETE", path+"/close", closeOp, "")
		er.CloseMapping = &cm
		er.HasClose = true
	}
	if !er.HasClose {
		if revokeOp := parserOp(spec, path+"/revoke", "DELETE"); revokeOp != nil {
			cm := operationMapping("DELETE", path+"/revoke", revokeOp, "")
			er.CloseMapping = &cm
			er.HasClose = true
		}
	}
	return er
}

// listResourceFromOperation builds a list resource from a list-classified
// operation. identityParams carries the paired instance path's template
// parameters (snake_cased) when the list was promoted from a CRUD group; they
// become the identity schema. Without them (an x-terraform-list operation with
// no known instance path), the identity falls back to the item object's "id"
// property when present. The resource schema is the item object's property
// set. Both come from the resolved response schema carried on pathOps: an
// array-of-objects response populates them, anything else leaves them empty
// (the generator keeps the list resource honestly scaffolded — F1).
func listResourceFromOperation(op *parser.Operation, providerName, path, method string, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation, identityParams []string) ir.ListResourceIR {
	name := resourceName(op, method, path)
	lr := ir.ListResourceIR{
		Name:            name,
		FullName:        providerName + "_" + name,
		TypeName:        providerName + "_" + name,
		Description:     op.Description,
		SourceOperation: op.OperationID,
		ListMapping:     operationMapping(method, path, op, envelopeOfTransformerOp(pathOps, path, method)),
	}

	top := lookupTransformerOp(pathOps, path, method)
	if top == nil || top.ResponseSchema == nil || !strings.EqualFold(top.ResponseSchema.Type, "array") {
		return lr
	}
	// The collection path's path/query/header parameters become the config
	// (filter) schema, so a list resource whose collection path carries
	// parameters can resolve its path substitutions and wire its List body
	// (PROJECT_DESIGN §23).
	lr.ConfigSchema = transformer.ListResourceConfigSchema(*top)
	item := top.ResponseSchema.Items
	if item == nil || len(item.Properties) == 0 {
		return lr
	}
	rs := transformer.ObjectSchemaFromSpec(item)
	lr.ResourceSchema = &rs

	// Identity: the paired instance path's parameters when known, else the
	// item's "id" property. The attribute type comes from the matching item
	// property (fallback string) so the generated identity type-checks against
	// the decoded item.
	propType := func(names ...string) ir.PrimitiveType {
		for _, n := range names {
			if prop, ok := item.Properties[n]; ok {
				switch strings.ToLower(prop.Type) {
				case "integer":
					return ir.TypeInt
				case "number":
					return ir.TypeFloat
				case "boolean":
					return ir.TypeBool
				default:
					return ir.TypeString
				}
			}
		}
		return ir.TypeString
	}
	if len(identityParams) > 0 {
		for _, p := range identityParams {
			lr.IdentitySchema.Attributes = append(lr.IdentitySchema.Attributes, ir.AttributeIR{
				Name:     p,
				Computed: true,
				Schema:   ir.SchemaIR{Type: propType(p)},
			})
		}
	} else if _, ok := item.Properties["id"]; ok {
		lr.IdentitySchema.Attributes = append(lr.IdentitySchema.Attributes, ir.AttributeIR{
			Name:     "id",
			Computed: true,
			Schema:   ir.SchemaIR{Type: propType("id")},
		})
	}
	return lr
}

// instancePathParams returns the templated ({param}) segments of an instance
// path, snake_cased, in order. They name the identity attributes of a list
// resource promoted from a CRUD group.
func instancePathParams(path string) []string {
	var out []string
	for _, seg := range strings.Split(path, "/") {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			out = append(out, transformer.ToSnakeCase(strings.TrimSuffix(strings.TrimPrefix(seg, "{"), "}")))
		}
	}
	return out
}

// functionFromOperation builds a provider-defined function from an operation,
// inferring its signature: Arguments come from the operation's path, query,
// header, and cookie parameters (the same primitive mapping actions use), and
// ReturnType comes from the resolved response schema carried on pathOps.
// Primitives, arrays of primitives, and flat objects of primitives map
// directly; anything more complex falls back to a Dynamic return (honest, not
// a guessed String). The function body stays a scaffold (F4 is out of scope).
func functionFromOperation(op *parser.Operation, providerName, path, method string, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation) ir.FunctionIR {
	name := resourceName(op, method, path)
	fn := ir.FunctionIR{
		Name:            name,
		FullName:        providerName + "_" + name,
		TypeName:        providerName + "_" + name,
		Description:     op.Description,
		SourceOperation: op.OperationID,
		Arguments:       actionConfigAttributes(op),
	}
	if top := lookupTransformerOp(pathOps, path, method); top != nil && top.ResponseSchema != nil {
		fn.ReturnType = functionReturnSchema(top.ResponseSchema)
	}
	return fn
}

// functionReturnSchema maps a resolved response schema to a function return
// type. Only shapes the function generator can express are mapped — primitives,
// arrays of primitives, and flat objects of primitives; anything else (nested
// objects, unions, maps) resolves to TypeDynamic.
func functionReturnSchema(spec *transformer.SchemaSpec) ir.SchemaIR {
	primitive := func(t string) ir.PrimitiveType {
		switch strings.ToLower(t) {
		case "string":
			return ir.TypeString
		case "integer":
			return ir.TypeInt
		case "number":
			return ir.TypeFloat
		case "boolean":
			return ir.TypeBool
		}
		return ""
	}

	switch strings.ToLower(spec.Type) {
	case "array":
		if spec.Items != nil {
			if elem := primitive(spec.Items.Type); elem != "" {
				return ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Type: elem}}}
			}
		}
	case "object":
		if len(spec.Properties) > 0 {
			attrs := make([]ir.AttributeIR, 0, len(spec.Properties))
			flat := true
			for _, name := range sortedKeys(spec.Properties) {
				prop := spec.Properties[name]
				t := primitive(prop.Type)
				if t == "" {
					flat = false
					break
				}
				attrs = append(attrs, ir.AttributeIR{Name: name, Schema: ir.SchemaIR{Type: t}})
			}
			if flat {
				return ir.SchemaIR{Attributes: attrs}
			}
		}
	default:
		if t := primitive(spec.Type); t != "" {
			return ir.SchemaIR{Type: t}
		}
	}
	return ir.SchemaIR{Type: ir.TypeDynamic}
}

// BuildProviderIR parses an OpenAPI document and builds a ProviderIR from it.
// The optional cfg supplies generator.yaml overrides that influence IR
// construction (for example, actions, ephemeral resources, list resources, and
// provider-defined functions that are not inferred from the spec alone).
//
// The returned diagnostics are conversion warnings produced while normalizing
// the OpenAPI document; callers should surface them to the user even when the
// overall build succeeds. Version detection diagnostics are included in the
// returned slice and are also surfaced through the error when version detection
// fails.
func BuildProviderIR(spec []byte, cfg *config.Config) (*ir.ProviderIR, parser.Version, diagnostics.Diagnostics, error) {
	return buildProviderIR(spec, "", "", cfg)
}

// BuildProviderIRWithContentType parses an OpenAPI document with an explicit
// HTTP Content-Type hint (e.g. from a remote spec fetch, PROJECT_DESIGN §23) and
// builds a ProviderIR from it. An empty contentType falls back to the
// first-byte JSON/YAML sniff shared by the local-file path.
func BuildProviderIRWithContentType(spec []byte, contentType string, cfg *config.Config) (*ir.ProviderIR, parser.Version, diagnostics.Diagnostics, error) {
	return buildProviderIR(spec, "", contentType, cfg)
}

// BuildProviderIRWithName parses an OpenAPI document and builds a ProviderIR,
// attributing parse errors to name (the spec's real path or URL) rather than
// the generic "request.yaml" used by the HTTP validate endpoint. The
// contentType hint (e.g. from a remote fetch's Content-Type header) routes
// JSON vs YAML parsing; an empty contentType sniffs the first byte.
func BuildProviderIRWithName(spec []byte, name, contentType string, cfg *config.Config) (*ir.ProviderIR, parser.Version, diagnostics.Diagnostics, error) {
	return buildProviderIR(spec, name, contentType, cfg)
}

func buildProviderIR(spec []byte, name, contentType string, cfg *config.Config) (*ir.ProviderIR, parser.Version, diagnostics.Diagnostics, error) {
	var root parser.Node
	var err error
	if name == "" {
		root, err = loadRequestBodyWithContentType(spec, contentType)
	} else {
		root, err = loadRequestBodyWithName(spec, contentType, name)
	}
	if err != nil {
		return nil, parser.VersionUnknown, nil, fmt.Errorf("failed to load spec: %w", err)
	}

	version, versionDiags := parser.DetectVersion(root)
	if hasErrors(toDiagnosticJSON(versionDiags)) {
		return nil, version, diagnostics.Diagnostics(versionDiags), fmt.Errorf("failed to detect OpenAPI version: %s", diagnosticDetails(versionDiags))
	}

	sp, convertDiags, err := convertForVersion(root, version)
	if err != nil {
		return nil, version, append(diagnostics.Diagnostics(versionDiags), convertDiags...), fmt.Errorf("failed to convert OpenAPI spec: %w", err)
	}

	allDiags := append(diagnostics.Diagnostics(versionDiags), convertDiags...)
	// Run the same structural and $ref validation the HTTP /validate endpoint
	// applies (parser.Validate). The converter records a schema's $ref string but
	// never resolves it; only Validate rejects non-local refs and unresolvable
	// local pointers. Without it, `eidos generate` silently dropped dangling
	// external refs (e.g. a bundled spec's sibling schema files) and emitted
	// empty-schema resources instead of failing loud.
	allDiags = append(allDiags, parser.Validate(root, sp, version)...)
	preview, previewDiags := buildIRPreview(sp, version, cfg)
	allDiags = append(allDiags, previewDiags...)
	return preview, version, allDiags, nil
}

// GenerateStarterConfig parses an OpenAPI document, builds a ProviderIR from it,
// and returns a validated generator.yaml Config. If providerName is non-empty it
// overrides the provider name derived from the spec title.
//
// The returned diagnostics are conversion warnings produced while normalizing
// the OpenAPI document; callers should surface them to the user even when the
// overall generation succeeds. Version detection diagnostics are returned as
// part of the error instead.
func GenerateStarterConfig(spec []byte, providerName string) (*config.Config, parser.Version, diagnostics.Diagnostics, error) {
	return generateStarterConfig(spec, "", providerName)
}

// GenerateStarterConfigWithName is GenerateStarterConfig with parse errors
// attributed to name (the spec's real path or URL) rather than the generic
// "request.yaml".
func GenerateStarterConfigWithName(spec []byte, name, providerName string) (*config.Config, parser.Version, diagnostics.Diagnostics, error) {
	return generateStarterConfig(spec, name, providerName)
}

func generateStarterConfig(spec []byte, name, providerName string) (*config.Config, parser.Version, diagnostics.Diagnostics, error) {
	var (
		preview  *ir.ProviderIR
		version  parser.Version
		allDiags diagnostics.Diagnostics
		err      error
	)
	if name == "" {
		preview, version, allDiags, err = BuildProviderIR(spec, nil)
	} else {
		preview, version, allDiags, err = BuildProviderIRWithName(spec, name, "", nil)
	}
	if err != nil {
		return nil, version, allDiags, err
	}

	if strings.TrimSpace(providerName) != "" {
		preview.Name = providerName
		preview.FullName = "terraform-provider-" + providerName
		preview.TypeName = providerName
	}

	cfg, err := generator.GenerateConfig(*preview)
	if err != nil {
		return nil, version, allDiags, fmt.Errorf("failed to generate starter config: %w", err)
	}
	return cfg, version, allDiags, nil
}

// diagnosticDetails renders a slice of diagnostics as a semicolon-separated
// string of summary:detail pairs. It is used to surface parser-produced
// diagnostics alongside command/API errors.
func diagnosticDetails(diags diagnostics.Diagnostics) string {
	parts := make([]string, 0, len(diags))
	for _, d := range diags {
		msg := d.Summary
		if d.Detail != "" {
			msg += ": " + d.Detail
		}
		parts = append(parts, msg)
	}
	return strings.Join(parts, "; ")
}

func buildSuggestedConfig(spec *parser.Spec, cfg *config.Config) (string, error) {
	name := providerName(spec, cfg)
	version := config.DefaultProviderVersion
	description := ""
	if spec.Info != nil {
		description = spec.Info.Description
	}
	if cfg != nil && strings.TrimSpace(cfg.Provider.Version) != "" {
		version = cfg.Provider.Version
	}

	suggested := config.Config{
		Provider: config.ProviderConfig{
			Name:        name,
			Version:     version,
			Description: description,
		},
	}

	if auth := suggestAuth(spec); len(auth) > 0 {
		suggested.Auth = auth
	}
	if pagination := suggestPagination(spec); pagination != nil {
		suggested.Pagination = pagination
	}

	data, err := yaml.Marshal(suggested)
	if err != nil {
		return "", fmt.Errorf("marshal suggested config: %w", err)
	}
	return string(data), nil
}

// suggestAuth maps declared security schemes to starter auth configuration.
// It does not invent credentials; it only records the scheme, header/query
// location, and OAuth2 token URL so the operator knows what to configure.
func suggestAuth(spec *parser.Spec) []config.AuthConfig {
	if spec == nil || spec.Components == nil {
		return nil
	}
	var auth []config.AuthConfig
	for _, name := range sortedKeys(spec.Components.SecuritySchemes) {
		scheme := spec.Components.SecuritySchemes[name]
		if scheme == nil {
			continue
		}
		if ac, ok := authConfigFromScheme(name, scheme); ok {
			auth = append(auth, ac)
		}
	}
	// Drop any entry that does not satisfy eidos's own auth validation so the
	// suggested config is always consumable (M-3/M-4).
	valid := auth[:0]
	for _, ac := range auth {
		if len(config.ValidateAuth(ac)) == 0 {
			valid = append(valid, ac)
		}
	}
	auth = valid
	sort.Slice(auth, func(i, j int) bool { return auth[i].Scheme < auth[j].Scheme })
	return auth
}

// authConfigFromScheme maps a single OpenAPI security scheme to a starter
// AuthConfig. It returns ok=false for schemes that cannot be represented as a
// validating starter config (implicit OAuth2, OpenID Connect).
func authConfigFromScheme(name string, scheme *parser.SecurityScheme) (config.AuthConfig, bool) {
	env := envVarName(name)
	switch scheme.Type {
	case "apiKey":
		// Scheme must be the literal "apiKey" (a value Validate accepts), not the
		// security-scheme map key, which may be arbitrary.
		ac := config.AuthConfig{
			Scheme: "apiKey",
			EnvVar: env + "_API_KEY",
		}
		// Respect the declared location. AuthConfig has no query/cookie field,
		// so only header (and the default) populate HeaderName.
		if scheme.In == "header" || scheme.In == "" {
			ac.HeaderName = scheme.Name
		}
		return ac, true
	case "http":
		if strings.EqualFold(scheme.Scheme, "bearer") {
			return config.AuthConfig{Scheme: "bearer", EnvVar: env + "_TOKEN"}, true
		}
		return config.AuthConfig{Scheme: "basic", EnvVar: env + "_CREDENTIALS"}, true
	case "oauth2":
		flow, tokenURL := oauth2FlowAndTokenURL(scheme.Flows)
		if flow == "" || strings.TrimSpace(tokenURL) == "" {
			return config.AuthConfig{}, false
		}
		ac := config.AuthConfig{
			Scheme:   "oauth2",
			Flow:     flow,
			TokenURL: tokenURL,
			EnvVar:   env + "_TOKEN",
		}
		if flow == "client_credentials" || flow == "authorization_code" {
			ac.ClientIDEnv = env + "_CLIENT_ID"
			ac.ClientSecretEnv = env + "_CLIENT_SECRET"
		}
		return ac, true
	case "openIdConnect":
		// OpenID Connect is discovered from a discovery URL, not a static token
		// URL; AuthConfig requires token_url for oauth2, so an OIDC scheme cannot
		// be represented as a validating starter config.
		return config.AuthConfig{}, false
	}
	return config.AuthConfig{}, false
}

// oauth2FlowAndTokenURL selects the flow actually declared by the spec (rather
// than hardcoding client_credentials) and returns its name and token URL. The
// implicit flow has no token URL, so it returns ("", "") and is skipped.
func oauth2FlowAndTokenURL(flows *parser.OAuthFlows) (string, string) {
	if flows == nil {
		return "", ""
	}
	switch {
	case flows.ClientCredentials != nil:
		return "client_credentials", flows.ClientCredentials.TokenURL
	case flows.Password != nil:
		return "password", flows.Password.TokenURL
	case flows.AuthorizationCode != nil:
		return "authorization_code", flows.AuthorizationCode.TokenURL
	}
	// flows.Implicit has no token URL; AuthConfig requires one for oauth2, so it
	// cannot be represented.
	return "", ""
}

// paginationHint captures a detected pagination style and its parameters.
type paginationHint struct {
	style        string
	pageParam    string
	perPageParam string
	cursorField  string
}

// suggestPagination inspects collection operations for common pagination
// parameter names and returns a starter PaginationConfig when a clear pattern
// is detected.
func suggestPagination(spec *parser.Spec) *config.PaginationConfig {
	if spec == nil || spec.Paths == nil {
		return nil
	}

	hints := make(map[string]paginationHint)
	processPathItem := func(path string, pi *parser.PathItem) {
		if pi == nil || !isCollectionPath(path) {
			return
		}
		for _, op := range []*parser.Operation{pi.Get, pi.Post} {
			if op == nil || op.Parameters == nil {
				continue
			}
			for _, p := range op.Parameters {
				name := strings.ToLower(p.Name)
				switch name {
				case "page", "pagenumber", "page_number":
					h := hints["offset"]
					h.style = "offset"
					h.pageParam = p.Name
					hints["offset"] = h
				case "limit", "perpage", "per_page", "pagesize", "page_size":
					h := hints["offset"]
					h.style = "offset"
					h.perPageParam = p.Name
					hints["offset"] = h
				case "offset":
					h := hints["offset"]
					h.style = "offset"
					h.pageParam = p.Name
					hints["offset"] = h
				case "cursor", "after", "nextcursor", "next_cursor":
					h := hints["cursor"]
					h.style = "cursor"
					h.cursorField = p.Name
					hints["cursor"] = h
				}
			}
		}
	}
	for _, path := range sortedKeys(spec.Paths) {
		processPathItem(path, spec.Paths[path])
	}

	// Prefer offset pagination when both page and limit-like params are found.
	if h, ok := hints["offset"]; ok && h.perPageParam != "" {
		return &config.PaginationConfig{
			Style:        h.style,
			PageParam:    h.pageParam,
			PerPageParam: h.perPageParam,
		}
	}
	if h, ok := hints["cursor"]; ok {
		return &config.PaginationConfig{
			Style:       h.style,
			CursorField: h.cursorField,
		}
	}
	if h, ok := hints["offset"]; ok {
		return &config.PaginationConfig{
			Style:     h.style,
			PageParam: h.pageParam,
		}
	}
	return nil
}

// isCollectionPath reports whether path addresses a collection rather than a
// single item. Item paths contain a path parameter (e.g. /pets/{id}).
func isCollectionPath(path string) bool {
	return !isItemPath(path)
}
