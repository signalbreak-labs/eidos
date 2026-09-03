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
	"os"
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
	return validateContext(context.Background(), body, contentType, "")
}

// ValidateContextWithContentType is like ValidateWithContentType but honors
// cancellation on the supplied context. The HTTP handler passes the request
// context so a client disconnect or the server's WriteTimeout aborts the
// pipeline (the body read is already context-aware via MaxBytesReader).
func ValidateContextWithContentType(ctx context.Context, body []byte, contentType string) ValidateResponse {
	return validateContext(ctx, body, contentType, "")
}

// ValidateContext runs the same pipeline as Validate but honors cancellation on
// the supplied context. Long-running pipeline stages should accept ctx and
// return early when it is canceled; the current short pipeline at least
// surfaces cancellation before doing work.
func ValidateContext(ctx context.Context, body []byte) ValidateResponse {
	return validateContext(ctx, body, "", "")
}

// ValidateContextWithName validates a spec loaded from name. When name is an
// existing local file, relative file references are resolved from its
// directory. URL and synthetic names remain filesystem-isolated.
func ValidateContextWithName(ctx context.Context, body []byte, name, contentType string) ValidateResponse {
	return validateContext(ctx, body, contentType, name)
}

func validateContext(ctx context.Context, body []byte, contentType, name string) ValidateResponse {
	var resp ValidateResponse

	// checkCtx aborts the pipeline early when the caller's context is canceled
	// (a client disconnect or the server's WriteTimeout), so an aborted request
	// stops doing wasted work instead of running every stage to completion
	// (N-54; the prior code only honored ctx at entry). It is called between
	// stages rather than wrapping them, because the parse/convert/validate and
	// IR-preview stages are the expensive ones and each is a single call.
	checkCtx := func() bool {
		if err := ctx.Err(); err != nil {
			resp.Diagnostics = append(resp.Diagnostics, DiagnosticJSON{
				Severity: diagnostics.Error.String(),
				Summary:  "Request canceled",
				Detail:   err.Error(),
			})
			resp.Valid = false
			return true
		}
		return false
	}

	if checkCtx() {
		return resp
	}

	var root parser.Node
	var err error
	if name == "" {
		root, err = loadRequestBodyWithContentType(body, contentType)
	} else {
		root, err = loadRequestBodyWithName(body, contentType, name)
	}
	if err != nil {
		resp.Diagnostics = append(resp.Diagnostics, DiagnosticJSON{
			Severity: diagnostics.Error.String(),
			Summary:  "Failed to parse request body",
			Detail:   err.Error(),
		})
		resp.Valid = false
		return resp
	}
	if checkCtx() {
		return resp
	}

	cfgStr, root, configDiags := extractConfig(root)
	resp.Diagnostics = append(resp.Diagnostics, configDiags...)
	if checkCtx() {
		return resp
	}

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
		if refErr := enableLocalReferences(spec, root, name, version); refErr != nil {
			resp.Diagnostics = append(resp.Diagnostics, DiagnosticJSON{
				Severity: diagnostics.Error.String(),
				Summary:  "Failed to initialize local $ref resolution",
				Detail:   refErr.Error(),
			})
		}
		validationDiags := parser.Validate(root, spec, version)
		resp.Diagnostics = append(resp.Diagnostics, toDiagnosticJSON(validationDiags)...)
	}
	if checkCtx() {
		return resp
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
			// Surface config validation warnings (both schema and operation set on
			// an override, env-var collisions). cfg.Warnings carries yaml:"-" so the
			// generator.yaml round-trip cannot hold them; without this they would be
			// written by config.Validate and immediately lost (M-16).
			for _, w := range parsedCfg.Warnings {
				resp.Diagnostics = append(resp.Diagnostics, DiagnosticJSON{
					Severity: diagnostics.Warning.String(),
					Summary:  w,
				})
			}
		}
	}
	if checkCtx() {
		return resp
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

// looksLikeSpecRoot reports whether node is a mapping whose top level carries an
// OpenAPI/Swagger version key. It gates the N-56 YAML fallback: the block YAML
// parser cannot parse a `{`-leading flow-style document root and instead reads
// "{openapi" as a literal key, so without this check a mis-parsed document would
// be accepted as a spec and fail confusingly downstream (version detection).
func looksLikeSpecRoot(node parser.Node) bool {
	m, ok := node.(*parser.MapNode)
	if !ok {
		return false
	}
	for _, e := range m.Entries {
		if e.Key == nil {
			continue
		}
		key, ok := e.Key.Value.(string)
		if !ok {
			continue
		}
		if key == "openapi" || key == "swagger" {
			return true
		}
	}
	return false
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
		// Content-Type / first byte only decides the *first* attempt. A YAML
		// spec sent with Content-Type: application/json (a mislabeled HTTP
		// response, or a client that always tags bodies as JSON) would otherwise
		// be rejected outright, so on a JSON parse failure we retry as YAML —
		// which is a superset of JSON and can never mis-parse a document that was
		// genuinely JSON (N-56).
		jsonNode, jsonErr := parser.LoadFileAsJSON(jsonName, body)
		if jsonErr == nil {
			return jsonNode, nil
		}
		yamlNode, yamlErr := parser.LoadFileAsYAML(yamlName, body)
		if yamlErr == nil && looksLikeSpecRoot(yamlNode) {
			return yamlNode, nil
		}
		// The YAML attempt is rejected unless it produced a recognizable OpenAPI
		// root. Without that guard a `{`-leading flow-style document that the
		// block parser mis-reads (e.g. `{openapi: 3.0.0}` -> a literal "{openapi"
		// key) would sail through as garbage; and a genuinely broken JSON body
		// should report the JSON error, which is the more precise diagnosis.
		return nil, jsonErr
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

func enableLocalReferences(spec *parser.Spec, root parser.Node, name string, version parser.Version) error {
	if name == "" {
		return nil
	}
	info, err := os.Stat(name)
	if err != nil || info.IsDir() {
		return nil
	}
	return parser.EnableLocalReferences(spec, root, name, version)
}

// ParseSpec parses raw OpenAPI bytes without authorizing filesystem references.
// displayName is used only for diagnostics; use ParseSpecWithName when the bytes
// came from a local file and relative references should resolve.
func ParseSpec(specBytes []byte, displayName string) (*parser.Spec, diagnostics.Diagnostics, error) {
	return parseSpec(specBytes, displayName, "")
}

// ParseSpecWithName parses bytes loaded from name. An existing local file name
// authorizes bounded relative-file resolution; URLs remain filesystem-isolated.
func ParseSpecWithName(specBytes []byte, name string) (*parser.Spec, diagnostics.Diagnostics, error) {
	return parseSpec(specBytes, name, name)
}

func parseSpec(specBytes []byte, displayName, localName string) (*parser.Spec, diagnostics.Diagnostics, error) {
	if displayName == "" {
		displayName = "spec"
	}
	root, err := loadRequestBodyWithName(specBytes, "", displayName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse spec: %w", err)
	}
	version, versionDiags := parser.DetectVersion(root)
	spec, convertDiags, err := convertForVersion(root, version)
	if err != nil {
		return nil, versionDiags, err
	}
	if err := enableLocalReferences(spec, root, localName, version); err != nil {
		return nil, append(versionDiags, convertDiags...), err
	}
	allDiags := append(diagnostics.Diagnostics(versionDiags), convertDiags...)
	allDiags = append(allDiags, parser.Validate(root, spec, version)...)
	return spec, allDiags, nil
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

func resolvePathItemReferences(spec *parser.Spec, diags *diagnostics.Diagnostics) {
	if spec == nil {
		return
	}
	for _, path := range sortedKeys(spec.Paths) {
		resolved, refDiags := spec.ResolvePathItemReference(spec.Paths[path])
		spec.Paths[path] = resolved
		*diags = append(*diags, refDiags...)
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
	// A generator.yaml provider.description overrides the spec's
	// info.description, mirroring provider.name/version precedence.
	if cfg != nil && strings.TrimSpace(cfg.Provider.Description) != "" {
		description = cfg.Provider.Description
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

	// Thread the generator.yaml client config onto the provider client IR so the
	// generator can bake the base URL template, user agent, timeout, and retry
	// settings into the generated client (L-5). clientConfigFromIR guards each
	// field, so a partial ClientIR leaves the other client defaults intact.
	preview.ClientIR = clientIRFromConfig(cfg)

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
	previewDiags := make(diagnostics.Diagnostics, 0, len(spec.Paths))
	resolvePathItemReferences(spec, &previewDiags)
	filterDiags := applyOperationFilters(spec, cfg)
	pathOps, opsDiags := transformer.OperationsFromSpecWithDiagnostics(spec)
	// Surface schema-conversion diagnostics (e.g. unrepresentable allOf/oneOf/
	// anyOf composition in nested properties) instead of dropping them silently
	// (L-97 / fail-loud). Warnings do not break Valid; Errors do.
	previewDiags = append(previewDiags, filterDiags...)
	previewDiags = append(previewDiags, opsDiags...)
	// Resolve the PUT-as-create toggle: default-on when no config is supplied
	// (the auto-generator's natural behavior) or when the field is unset/true;
	// use_put_as_create: false is the global kill-switch that restores the legacy
	// scaffold behavior. The *bool distinguishes "unset" (nil → on) from an
	// explicit false, which is what makes the default-on stance legible.
	usePutAsCreate := cfg == nil || cfg.UsePutAsCreate == nil || *cfg.UsePutAsCreate
	// Group operations into managed resources first (REMAINING_GAPS §3). A
	// complete CRUD group (Create + Read + Delete on a collection/instance path
	// pair) becomes a single wired resource instead of one partial resource per
	// operation. Operations consumed by a grouped resource are skipped in the
	// per-operation pass below so they are not double-emitted as data sources or
	// partial resources. Incomplete groups fall through to the per-operation
	// classification unchanged. The transformer path-operation map is computed
	// once and shared with the per-operation pass so data sources can build their
	// schemas from the same resolved request/response shapes (REMAINING_GAPS §4).
	var resourceOverrides []config.ResourceOverride
	if cfg != nil {
		resourceOverrides = cfg.ResourceOverrides
	}
	groupedResources, consumed := buildGroupedResources(spec, name, pathOps, resourceOverrides, &previewDiags, usePutAsCreate)
	preview.Resources = append(preview.Resources, groupedResources...)
	// Record which surviving grouped resources had their Create resolved from the
	// instance-path PUT (PUT-as-create inference). The Info diagnostic for this
	// inference is emitted after config overrides below so it fires only for
	// resources that survive a skip: true opt-out — emitting it inside
	// buildGroupedResources would surface a misleading "set skip: true" hint for a
	// resource the practitioner already dropped. Explicit create-operation overrides
	// (applyResourceCreationOverrides) are deliberately excluded: a practitioner who
	// declares create_operation: putX already knows the PUT is the Create.
	inferredPutCreates := make(map[string]bool)
	for _, r := range groupedResources {
		if r.CRUDMapping.Create.Method == string(transformer.MethodPut) {
			inferredPutCreates[r.CRUDMapping.Create.PathTemplate] = true
		}
	}

	// resource_overrides with generate_resource or explicit CRUD operations
	// promote an action to a managed resource (G8). Run before the per-operation
	// pass so the consumed operations are not double-emitted as actions.
	if cfg != nil {
		applyResourceCreationOverrides(preview, spec, name, pathOps, cfg.ResourceOverrides, consumed, &previewDiags)
	}

	// Collection GETs paired with an instance Read are promoted to list
	// resources (additively: the data source is kept) by addPathOperations. The
	// instance path's template parameters name the promoted list resource's
	// identity attributes.
	listPaths := make(map[string][]string)
	for _, l := range transformer.InferListResourcesWithDiagnostics(
		transformer.InferResourceCRUDWithDiagnostics(pathOps, usePutAsCreate, &previewDiags),
		&previewDiags) {
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
	// Surface PUT-as-create (upsert) inference with an Info diagnostic so the
	// load-bearing assumption is never silent (AGENTS.md "fail loud, never
	// silently"). Emitted after config overrides so a skip: true opt-out drops
	// both the resource and its Info — the hint it carries ("set skip: true")
	// would otherwise advise an action the practitioner already took. Only
	// inferred PUT-as-create groups qualify (inferredPutCreates); an explicit
	// create_operation override that happens to use a PUT is not an inference.
	emitPutAsCreateInfoDiagnostics(preview, inferredPutCreates, &previewDiags)
	// Write-only and secret (Sensitive) inference are spec-driven passes, not
	// generator.yaml preferences, so they run unconditionally (even when cfg is
	// nil) after config overrides so override-added constructs are covered too.
	applyWriteOnlyAttributesToProvider(preview, &previewDiags)
	inferSensitiveAttributesToProvider(preview, &previewDiags)

	// List resources share their identity schema with the paired managed
	// resource of the same type name: terraform query types the identities a
	// list resource streams against the managed resource's identity schema
	// (ResourceWithIdentity), so the two must match. Copy the list resource's
	// identity schema onto the managed resource so the generator emits the
	// IdentitySchema method the framework requires; without it terraform query
	// fails with "Identity schema not found for resource type".
	//
	// Registration pairing runs first: it renames promoted list resources to
	// their CRUD group's managed resource so the two share a type name (the
	// framework only registers lists that pair) and warns for lists that
	// cannot pair, which stay unregistered.
	pairListResourceRegistrations(preview, &previewDiags)
	pairListResourceIdentities(preview)

	// Two operations that normalize to the same construct name (e.g. duplicate
	// operationIds) would make the generator emit two files at one path. Fail
	// loud here with a diagnostic naming both source operations instead of
	// surfacing a confusing "duplicate output path" error from the generator.
	previewDiags = append(previewDiags, checkDuplicateConstructNames(preview)...)

	// Enforce the IR schema invariants (SchemaIR.Validate) on the fully
	// assembled provider, post-overrides (N-48). A transformer or override bug
	// that produces e.g. both Type and Collection on one node must fail loud
	// here instead of surfacing as an unrelated generated-code compile error.
	for _, verr := range ir.ValidateProviderIR(preview) {
		previewDiags = append(previewDiags, diagnostics.Diagnostic{
			Severity: diagnostics.Error,
			Summary:  "invalid IR schema (transformer/override produced a schema that violates IR invariants)",
			Detail:   verr.Error(),
		})
	}

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
// paths map. OpenAPI 3.1 webhooks are deliberately excluded: a webhook
// describes a callback the API provider makes to the client, not an endpoint
// the server exposes, so classifying it as a provider-side operation would
// generate a wired action that POSTs to a path the server does not host (M-11).
// Webhooks are parsed but not mapped to a Terraform construct, and each is
// surfaced with a fail-loud warning so the omission is never silent.
func addSpecPathOperations(preview *ir.ProviderIR, spec *parser.Spec, providerName string, consumed map[string]map[string]bool, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation, listPaths map[string][]string, diags *diagnostics.Diagnostics) {
	if spec.Paths != nil {
		for _, path := range sortedKeys(spec.Paths) {
			addPathOperations(preview, spec, path, spec.Paths[path], providerName, consumed, pathOps, listPaths, diags)
		}
	}
	if spec.Webhooks != nil {
		for _, name := range sortedKeys(spec.Webhooks) {
			*diags = append(*diags, diagnostics.Diagnostic{
				Severity: diagnostics.Warning,
				Summary:  fmt.Sprintf("OpenAPI webhook %q is parsed but not mapped to a Terraform construct", name),
				Detail: "Webhook operations describe callbacks the API provider makes to the client, " +
					"not endpoints the server exposes, so no action, resource, or data source is generated for them.",
			})
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
	// resource_overrides entries that match no resource are a silent no-op in the
	// CLI generate path: a typo'd schema: or operation: vanishes with no signal
	// (M-18). This pre-scan runs before transformer.ApplyOverrides, so
	// override-created resources (applyResourceCreationOverrides already ran) are
	// present and a skip:true entry whose resource still exists is not flagged.
	for _, ro := range cfg.ResourceOverrides {
		if resourceOverrideMatchesAny(preview.Resources, ro) {
			continue
		}
		*diags = append(*diags, diagnostics.Diagnostic{
			Severity: diagnostics.Warning,
			Summary:  "resource override did not match any generated resource",
			Detail: fmt.Sprintf(
				"resource_overrides entry %s matched no resource; it is ignored. Check the schema or operation name (the MCP override-preview tool reports per-entry match/no-match).",
				overrideSeedLabel(ro),
			),
		})
	}
	for _, ao := range cfg.ActionOverrides {
		if idx := matchingActionIndex(preview.Actions, ao); idx >= 0 {
			applyActionOverrideExtras(&preview.Actions[idx], ao)
			continue
		}
		if path, method, ok := overrideOperationDoubleClaimed(ao.Operation, pathOps, consumed); ok {
			*diags = append(*diags, doubleClaimedDiagnostic("Action", ao.Operation, path, method))
			continue
		}
		preview.Actions = append(preview.Actions, actionFromOverride(ao, providerName))
	}
	for _, eo := range cfg.EphemeralOverrides {
		if idx := matchingEphemeralIndex(preview.EphemeralResources, eo); idx >= 0 {
			applyEphemeralOverrideExtras(&preview.EphemeralResources[idx], eo)
			continue
		}
		if path, method, ok := overrideOperationDoubleClaimed(eo.Operation, pathOps, consumed); ok {
			*diags = append(*diags, doubleClaimedDiagnostic("Ephemeral resource", eo.Operation, path, method))
			continue
		}
		preview.EphemeralResources = append(preview.EphemeralResources, ephemeralFromOverride(eo, providerName))
	}
	for _, lo := range cfg.ListResourceOverrides {
		if matchingListResourceIndex(preview.ListResources, lo) >= 0 {
			continue
		}
		if path, method, ok := overrideOperationDoubleClaimed(lo.Operation, pathOps, consumed); ok {
			*diags = append(*diags, doubleClaimedDiagnostic("List resource", lo.Operation, path, method))
			continue
		}
		preview.ListResources = append(preview.ListResources, listResourceFromOverride(lo, providerName))
	}
	for _, fo := range cfg.FunctionOverrides {
		if idx := matchingFunctionIndex(preview.Functions, fo); idx >= 0 {
			applyFunctionOverrideExtras(&preview.Functions[idx], fo)
			continue
		}
		if path, method, ok := overrideOperationDoubleClaimed(fo.Operation, pathOps, consumed); ok {
			*diags = append(*diags, doubleClaimedDiagnostic("Function", fo.Operation, path, method))
			continue
		}
		preview.Functions = append(preview.Functions, functionFromOverride(fo, providerName))
	}

	if err := transformer.ApplyOverridesWithDiagnostics(preview, cfg, diags); err != nil {
		*diags = append(*diags, diagnostics.Diagnostic{
			Severity: diagnostics.Error,
			Summary:  "Failed to apply generator.yaml overrides",
			Detail:   err.Error(),
		})
	}

	// resource_overrides.generate_datasource emits a data source mirroring a
	// matched managed resource (M-17). Run after ApplyOverrides so renames and
	// skip:true opt-outs are authoritative: a skipped resource must not emit, and
	// the data source must match the resource's final name.
	applyGenerateDatasourceOverrides(preview, providerName, pathOps, cfg.ResourceOverrides, cfg, diags)
}

// applyGenerateDatasourceOverrides emits a data source mirroring each managed
// resource whose matching resource_override sets generate_datasource: true
// (M-17). The resource's read operation is consumed by its CRUD group, so no
// standalone data source is inferred for it; this re-emits the read as a
// practitioner-facing data source. The schema is the read-shaped data source
// schema (path/query params as inputs, response properties as Computed outputs)
// built from the resolved read operation, mirroring dataSourceFromOperation.
//
// The emitted data source carries the resource's SourceOperation (not the read
// operation's) so the round-trip config generator can recognize it as
// resource-derived: convertResources re-emits generate_datasource + datasource_name
// and convertDatasources skips it, so a normalized generator.yaml reproduces the
// same data source instead of silently dropping it (G8).
func applyGenerateDatasourceOverrides(preview *ir.ProviderIR, providerName string, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation, overrides []config.ResourceOverride, cfg *config.Config, diags *diagnostics.Diagnostics) {
	if preview == nil {
		return
	}
	for _, ro := range overrides {
		if ro.GenerateDatasource == nil || !*ro.GenerateDatasource {
			continue
		}
		var matched *ir.ResourceIR
		for i := range preview.Resources {
			if transformer.ResourceOverrideMatches(preview.Resources[i], ro) {
				matched = &preview.Resources[i]
				break
			}
		}
		if matched == nil {
			*diags = append(*diags, diagnostics.Diagnostic{
				Severity: diagnostics.Warning,
				Summary:  "generate_datasource: true did not match a resource",
				Detail:   fmt.Sprintf("resource_overrides entry %q matched no generated resource; no data source was emitted", overrideSeedLabel(ro)),
			})
			continue
		}
		ds := datasourceFromResource(providerName, *matched, ro, pathOps, diags)
		if ds == nil {
			*diags = append(*diags, diagnostics.Diagnostic{
				Severity: diagnostics.Warning,
				Summary:  "generate_datasource: true but the resource has no wired read operation",
				Detail:   fmt.Sprintf("resource %q has no read operation (or its read cannot be resolved), so no data source was emitted", matched.Name),
			})
			continue
		}
		// Apply the datasource naming prefix after ApplyOverrides ran, so a
		// generated data source honors datasource_prefix the way inferred ones do.
		if cfg != nil && cfg.Naming != nil && strings.TrimSpace(cfg.Naming.DatasourcePrefix) != "" {
			ds.Name = cfg.Naming.DatasourcePrefix + ds.Name
			ds.FullName = cfg.Naming.DatasourcePrefix + ds.FullName
			ds.TypeName = cfg.Naming.DatasourcePrefix + ds.TypeName
		}
		if dataSourceAlreadyExists(preview.DataSources, *ds) {
			continue
		}
		preview.DataSources = append(preview.DataSources, *ds)
	}
}

// resourceOverrideMatchesAny reports whether any provider resource matches the
// override, using the same rules applyResourceOverrides applies.
func resourceOverrideMatchesAny(resources []ir.ResourceIR, ro config.ResourceOverride) bool {
	for _, r := range resources {
		if transformer.ResourceOverrideMatches(r, ro) {
			return true
		}
	}
	return false
}

// overrideSeedLabel renders a resource override's matching key for diagnostics.
func overrideSeedLabel(ro config.ResourceOverride) string {
	if strings.TrimSpace(ro.Operation) != "" {
		return "operation=" + ro.Operation
	}
	if strings.TrimSpace(ro.Schema) != "" {
		return "schema=" + ro.Schema
	}
	return "(no schema or operation)"
}

// datasourceFromResource builds a DataSourceIR mirroring a managed resource for
// the generate_datasource opt-in. The data source schema is the read-shaped
// schema built from the resource's resolved read operation (the read is consumed
// by the resource, so pathOps still resolves it), not the resource's write-shaped
// create schema. A resource with no read mapping, or whose read operation cannot
// be resolved in pathOps, yields nil so the caller can surface a diagnostic.
func datasourceFromResource(providerName string, r ir.ResourceIR, override config.ResourceOverride, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation, diags *diagnostics.Diagnostics) *ir.DataSourceIR {
	read := r.CRUDMapping.Read
	if strings.TrimSpace(read.PathTemplate) == "" || strings.TrimSpace(read.Method) == "" {
		return nil
	}
	top := lookupTransformerOp(pathOps, read.PathTemplate, read.Method)
	if top == nil {
		return nil
	}
	name := strings.TrimSpace(override.DatasourceName)
	if name == "" {
		name = r.Name
	}
	name = transformer.ToSnakeCase(name)
	ds := ir.DataSourceIR{
		Name:            name,
		FullName:        providerName + "_" + name,
		TypeName:        providerName + "_" + name,
		Description:     fmt.Sprintf("Reads the %s data source.", humanizeConstructName(name)),
		Schema:          transformer.DataSourceSchema(*top, diags),
		ReadMapping:     read,
		SourceOperation: r.SourceOperation,
		// A data source derived from a deprecated resource inherits the
		// deprecation so the flag reaches the generated schema (M-10).
		DeprecationMessage: r.DeprecationMessage,
	}
	return &ds
}

// dataSourceAlreadyExists reports whether a data source with the same name, or
// reading from the same path/method, is already present. The path/method check
// prevents double-emission when the read operation is somehow not consumed, and
// the name check prevents a duplicate-name generator failure.
func dataSourceAlreadyExists(dataSources []ir.DataSourceIR, ds ir.DataSourceIR) bool {
	for _, existing := range dataSources {
		if existing.Name == ds.Name {
			return true
		}
		if strings.TrimSpace(existing.ReadMapping.PathTemplate) != "" &&
			existing.ReadMapping.PathTemplate == ds.ReadMapping.PathTemplate &&
			strings.EqualFold(existing.ReadMapping.Method, ds.ReadMapping.Method) {
			return true
		}
	}
	return false
}

// overrideOperationDoubleClaimed reports whether an override's operation is
// already consumed by a resource (a grouped resource or a resource creation
// override). The operation resolves in the spec but was claimed before
// applyConfigOverrides ran, so appending a *FromOverride would emit an empty
// scaffold (no ConfigSchema / mapping) for an operation the resource already
// owns. Overrides that name a method+path or an operation with no operationId
// leave ok=false — those legitimately declare a fresh construct. The caller
// labels the override family ("Action", "Ephemeral resource", ...) when building
// the diagnostic.
func overrideOperationDoubleClaimed(operationID string, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation, consumed map[string]map[string]bool) (string, string, bool) {
	path, method, op := resolveOperationByID(pathOps, operationID)
	if op == nil || !isConsumed(consumed, path, method) {
		return "", "", false
	}
	return path, method, true
}

// doubleClaimedDiagnostic builds the warning emitted when an override targets an
// operation a resource already consumes. The operation can be claimed by exactly
// one construct, so the override is skipped; the message tells the user how to
// make the claim unambiguous.
func doubleClaimedDiagnostic(kind, operationID, path, method string) diagnostics.Diagnostic {
	return diagnostics.Diagnostic{
		Severity: diagnostics.Warning,
		Summary:  kind + " override references an operation already claimed by a resource",
		Detail: fmt.Sprintf(
			kind+" override %q targets %s %s, which a resource already consumes. The operation "+
				"can be claimed by exactly one construct, so the override is skipped. Remove the "+
				"operation from the override or from the resource's create/read/update/delete "+
				"operations so the claim is unambiguous.",
			operationID, strings.ToUpper(method), path),
	}
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
//
// Declared arguments MERGE with the function's inferred arguments by name: an
// override argument whose name matches an inferred argument (case-insensitively,
// ignoring underscores) replaces that argument's type so the override can
// correct an inferred type without duplicating it; an override argument with no
// inferred counterpart is appended. Blindly appending every declared argument
// duplicates the inferred signature when an override redeclares the same
// parameters (e.g. an auto-generated config that records an inferred
// function's query-parameter signature), producing "Parameter names must be
// unique" validation errors at provider load.
func applyFunctionOverrideExtras(f *ir.FunctionIR, fo config.FunctionOverride) {
	for _, arg := range fo.Arguments {
		name := strings.TrimSpace(arg.Name)
		if name == "" {
			continue
		}
		schema := ir.SchemaIR{Type: primitiveTypeFromString(arg.Type)}
		if idx := functionArgumentIndex(f.Arguments, name); idx >= 0 {
			// Replace only the type; preserve the inferred WireName and
			// Description, which the override does not carry.
			f.Arguments[idx].Schema = schema
			continue
		}
		f.Arguments = append(f.Arguments, ir.FunctionParamIR{
			Name:   transformer.SanitizeAttributeName(name),
			Schema: schema,
		})
	}
}

// functionArgumentIndex returns the index of the first function argument whose
// name matches target. Names are compared case-insensitively and with
// underscores stripped so that an override's snake_case name (e.g. "start_time")
// matches an inferred name derived from a camelCase or dotted parameter (e.g.
// "startTime" or "fm.tags").
func functionArgumentIndex(args []ir.FunctionParamIR, target string) int {
	want := normalizeFunctionArgName(target)
	if want == "" {
		return -1
	}
	for i := range args {
		if normalizeFunctionArgName(args[i].Name) == want {
			return i
		}
	}
	return -1
}

// normalizeFunctionArgName trims surrounding whitespace, strips underscores, and
// lowercases a function-argument name for case- and formatting-insensitive
// comparison. It mirrors transformer.normalizeName (which is not exported).
func normalizeFunctionArgName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "_", "")
	return strings.ToLower(s)
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

// inferSensitiveAttributesToProvider marks string-typed attributes whose names
// indicate a secret (password/secret/token/...) as Sensitive across every
// schema kind the generator renders Sensitive for (resources, data sources,
// ephemeral resources). For schema kinds that ignore Sensitive — action config
// attributes (action.go) and list resource schema attributes (list.go) — it
// emits a Warning per secret-named attribute instead, so the limitation is
// surfaced rather than the secret leaking in state with no signal (fail-loud).
// Existing Sensitive attributes (security-scheme inference, write-only
// processing, overrides) are left untouched; this pass only ever adds.
func inferSensitiveAttributesToProvider(provider *ir.ProviderIR, diags *diagnostics.Diagnostics) {
	if provider == nil {
		return
	}
	for i := range provider.Resources {
		transformer.InferSensitiveAttributes(&provider.Resources[i].Schema)
	}
	for i := range provider.DataSources {
		transformer.InferSensitiveAttributes(&provider.DataSources[i].Schema)
	}
	for i := range provider.EphemeralResources {
		transformer.InferSensitiveAttributes(&provider.EphemeralResources[i].ConfigSchema)
		transformer.InferSensitiveAttributes(&provider.EphemeralResources[i].ResultSchema)
	}
	// Actions and list resources cannot render Sensitive (action.go/list.go);
	// surface the secret-named attributes we cannot mark so they are not
	// silent, and record them in the IR so the generated docs carry a
	// practitioner-facing admonition (§3.6).
	for i := range provider.Actions {
		provider.Actions[i].UnmarkableSensitiveAttrs = transformer.WarnUnmarkableSensitive(
			&provider.Actions[i].ConfigSchema, "action", provider.Actions[i].TypeName, diags)
	}
	for i := range provider.ListResources {
		name := provider.ListResources[i].TypeName
		var unmarkable []string
		unmarkable = append(unmarkable, transformer.WarnUnmarkableSensitive(
			&provider.ListResources[i].ConfigSchema, "list resource", name, diags)...)
		unmarkable = append(unmarkable, transformer.WarnUnmarkableSensitive(
			&provider.ListResources[i].IdentitySchema, "list resource", name, diags)...)
		if provider.ListResources[i].ResourceSchema != nil {
			unmarkable = append(unmarkable, transformer.WarnUnmarkableSensitive(
				provider.ListResources[i].ResourceSchema, "list resource", name, diags)...)
		}
		provider.ListResources[i].UnmarkableSensitiveAttrs = unmarkable
	}
}

// clientIRFromConfig translates the generator.yaml client config onto the
// provider client IR. config.ClientConfig is the user-facing generator.yaml
// shape; ClientIR uses the generated client's field names. Duration fields are
// converted from the config Duration wrapper to time.Duration. Returns a
// zero-value ClientIR when no client config is set, so the generator's
// clientConfigFromIR guards fall back to the defaults (L-5).
func clientIRFromConfig(cfg *config.Config) ir.ClientIR {
	var out ir.ClientIR
	if cfg == nil || cfg.Client == nil {
		return out
	}
	out.BaseURLTemplate = cfg.Client.BaseURLTemplate
	out.UserAgent = cfg.Client.UserAgent
	out.RetryMax = cfg.Client.RetryMax
	if cfg.Client.Timeout != nil {
		out.Timeout = cfg.Client.Timeout.Duration()
	}
	if cfg.Client.RetryWaitMin != nil {
		out.RetryWaitMin = cfg.Client.RetryWaitMin.Duration()
	}
	if cfg.Client.RetryWaitMax != nil {
		out.RetryWaitMax = cfg.Client.RetryWaitMax.Duration()
	}
	return out
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
// global security requirements populate SecurityIR.DefaultRequirements and are
// validated so a requirement naming an undeclared scheme is surfaced as a
// Warning instead of being silently dropped.
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

	security.DefaultRequirements = copySecurityRequirements(spec.Security)

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

	warnUndeclaredSecuritySchemes(spec, selectedScheme, diags)

	if spec.Components == nil {
		return security
	}
	security.Schemes = buildSecuritySchemes(spec, selectedScheme, diags)
	// Apply generator.yaml `auth:` overrides so the documented auth section is
	// actually consumed: header_name, token_url, discovery_url, flow, and the
	// env-var hints override the auto-derived scheme configuration (M-5).
	if cfg != nil && len(cfg.Auth) > 0 {
		security.Schemes = transformer.ApplyAuthOverrides(security.Schemes, cfg.Auth, diags)
	}
	return security
}

// copySecurityRequirements carries the global security requirements into the IR
// so they are not silently dropped. Each parser.SecurityRequirement wraps a
// map[schemeName][]scopes; it is copied into a fresh map to avoid aliasing the
// parser's storage. An empty requirement object {} marks the API as allowing
// unauthenticated access; the copy preserves it as an empty map.
func copySecurityRequirements(requirements []parser.SecurityRequirement) []map[string][]string {
	out := make([]map[string][]string, 0, len(requirements))
	for _, req := range requirements {
		reqCopy := make(map[string][]string, len(req.Requirements))
		for schemeName, scopes := range req.Requirements {
			reqCopy[schemeName] = scopes
		}
		out = append(out, reqCopy)
	}
	return out
}

// warnUndeclaredSecuritySchemes validates that every scheme referenced by the
// global security requirements is actually declared in
// components.securitySchemes. A requirement naming an undeclared scheme would
// otherwise be silently dropped — the generated client can only apply schemes
// it knows about — so it is surfaced as a Warning (fail-loud). When
// generator.yaml selects a single scheme, requirements naming other schemes are
// intentionally not applied and are not validated.
func warnUndeclaredSecuritySchemes(spec *parser.Spec, selectedScheme string, diags *diagnostics.Diagnostics) {
	declaredSchemes := make(map[string]struct{})
	if spec.Components != nil {
		for name := range spec.Components.SecuritySchemes {
			declaredSchemes[name] = struct{}{}
		}
	}
	for _, req := range spec.Security {
		for schemeName := range req.Requirements {
			if _, ok := declaredSchemes[schemeName]; ok {
				continue
			}
			if selectedScheme != "" && schemeName != selectedScheme {
				// The user selected one scheme; other requirements are
				// intentionally ignored, so an undeclared name there is not a
				// silent drop.
				continue
			}
			*diags = append(*diags, diagnostics.Diagnostic{
				Severity: diagnostics.Warning,
				Summary:  "Security requirement references undeclared scheme",
				Detail: fmt.Sprintf(
					"The global security requirement references scheme %q, which is "+
						"not declared in components.securitySchemes. The generated "+
						"provider cannot apply it, so this requirement is dropped. "+
						"Declare the scheme in components.securitySchemes or remove "+
						"the requirement from the spec.",
					schemeName,
				),
			})
		}
	}
}

// buildSecuritySchemes converts every declared security scheme (optionally
// filtered to the generator.yaml-selected scheme) into its IR form, sorted by
// name for deterministic output.
func buildSecuritySchemes(spec *parser.Spec, selectedScheme string, diags *diagnostics.Diagnostics) []ir.SecuritySchemeIR {
	var schemes []ir.SecuritySchemeIR
	for _, name := range sortedKeys(spec.Components.SecuritySchemes) {
		if selectedScheme != "" && name != selectedScheme {
			continue
		}
		scheme, refDiags := spec.ResolveSecuritySchemeReference(spec.Components.SecuritySchemes[name])
		*diags = append(*diags, refDiags...)
		spec.Components.SecuritySchemes[name] = scheme
		if scheme == nil {
			continue
		}
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
		schemes = append(schemes, irScheme)
	}
	sort.Slice(schemes, func(i, j int) bool {
		return schemes[i].Name < schemes[j].Name
	})
	return schemes
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
// schema stays valid; the dropped duplicate is surfaced as a warning rather
// than silently discarded (N-13). Scheme-mapping errors surface as diagnostics
// rather than being dropped silently.
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
				*diags = append(*diags, diagnostics.Diagnostic{
					Severity: diagnostics.Warning,
					Summary:  "Security scheme config attribute dropped",
					Detail: fmt.Sprintf(
						"scheme %q maps to provider config attribute %q, which another scheme already declares; the duplicate is dropped and this scheme's credential cannot be set independently. Qualify the scheme name in the spec so eidos can emit a distinct attribute.",
						scheme.Name, a.Name,
					),
				})
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
	name := listResourceNameFromOverride(lo)
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

// listResourceNameFromOverride derives a list resource's name from an override.
// The explicit `resource` key wins; when it is absent the name falls back to the
// operation identifier, mirroring functionFromOverride. The operation string may
// be either an OpenAPI operationId ("loadAllIcapProfiles") or a "METHOD /path"
// form ("GET /apps/icap/profiles"); the latter is derived via DeriveOperationID
// so both forms produce a stable snake_case name instead of an empty one.
func listResourceNameFromOverride(lo config.ListResourceOverride) string {
	if name := transformer.ToSnakeCase(lo.Resource); name != "" {
		return name
	}
	op := strings.TrimSpace(lo.Operation)
	if op == "" {
		return ""
	}
	if parts := strings.Fields(op); len(parts) == 2 {
		return transformer.DeriveOperationID(parts[0], parts[1])
	}
	return transformer.ToSnakeCase(op)
}

func functionFromOverride(fo config.FunctionOverride, providerName string) ir.FunctionIR {
	name := fo.Name
	if strings.TrimSpace(name) == "" {
		name = transformer.ToSnakeCase(fo.Operation)
	}
	fn := ir.FunctionIR{
		Name:     name,
		FullName: providerName + "_" + name,
		// The registered function name is bare — see functionFromOperation for
		// why provider-defined functions do not carry the provider prefix (§3.7).
		TypeName:        name,
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
	// Iterate methods in a fixed order rather than ranging the map: Go
	// randomizes map iteration order per process, so an unsorted range would
	// make the relative order of same-path constructs (e.g. a PATCH action and
	// a DELETE action on one path) nondeterministic across runs, violating
	// byte-identical generation for an identical spec. The fixed order also
	// keeps CRUD classification stable: GET (data source) before POST (create)
	// before PUT/PATCH (update) before DELETE (delete) matches the natural
	// CRUD lifecycle so a resource's operations are visited in lifecycle order.
	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		op := ops[method]
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
			preview.DataSources = append(preview.DataSources, dataSourceFromOperation(op, providerName, path, method, pathOps, preview.ClientIR.Pagination, diags))
			// A collection GET paired with an instance Read is also a list
			// resource (InferListResources). The promotion is additive — the
			// data source above is kept so existing wiring is not broken —
			// and generator.yaml overrides remain the authoritative escape
			// hatch. x-terraform-list operations take the kindListResource
			// branch instead and are not double-emitted.
			if method == "GET" && listPaths[path] != nil {
				preview.ListResources = append(preview.ListResources, listResourceFromOperation(op, providerName, path, method, pathOps, listPaths[path], diags))
				warnListUniqueItems(diags, pathOps, path, method)
			}
		case kindAction:
			preview.Actions = append(preview.Actions, actionFromOperation(op, providerName, path, method, pathOps, diags))
		case kindEphemeral:
			preview.EphemeralResources = append(preview.EphemeralResources, ephemeralFromOperation(spec, op, providerName, path, method, pathOps, diags))
			// The sibling lifecycle operations the ephemeral claims as its
			// Renew/Close mappings are consumed so they do not also classify as
			// their own constructs (renew/revoke/rotate are action verbs, so a
			// lifecycle subpath would otherwise double-emit as a spurious
			// action; PROJECT_DESIGN §23). Paths are iterated in sorted order, so the
			// ephemeral's own path is visited before its "/renew"/"/close"/
			// "/revoke" siblings and the consumption is visible to them.
			consumeEphemeralLifecycleOps(consumed, path)
		case kindListResource:
			preview.ListResources = append(preview.ListResources, listResourceFromOperation(op, providerName, path, method, pathOps, nil, diags))
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

// warnUnwritableManagedResource surfaces a Warning when a managed resource's
// schema has no practitioner-writable (Required or Optional) attributes. This
// is degenerate — Terraform cannot create or update a resource whose every
// attribute is Computed — and typically indicates the inferred create operation
// declares no request body (a spec defect), so every attribute is derived from
// the read response. Fail loud instead of silently emitting an unwritable
// resource.
func warnUnwritableManagedResource(diags *diagnostics.Diagnostics, res ir.ResourceIR, create *transformer.Operation) {
	if diags == nil {
		return
	}
	for _, a := range res.Schema.Attributes {
		if a.Required || a.Optional {
			return
		}
	}
	createDesc := "no request body"
	if create != nil {
		if create.OperationID != "" {
			createDesc = "operation " + create.OperationID
		}
		if !create.RequestBody {
			if createDesc != "no request body" {
				createDesc += " declares no request body"
			}
		}
	}
	*diags = append(*diags, diagnostics.Diagnostic{
		Severity: diagnostics.Warning,
		Summary:  "Managed resource has no writable attributes",
		Detail: fmt.Sprintf(
			"Resource %s has no Required or Optional attributes, so every attribute is "+
				"Computed-only and a practitioner cannot create or update it. The inferred "+
				"create (%s) does not contribute a writable request body; verify the spec's "+
				"create operation declares its inputs, or supply a generator.yaml "+
				"resource_override pinning a create operation that does.",
			res.FullName, createDesc),
	})
}

// buildGroupedResources runs CRUD inference over the parsed spec and returns one
// wired managed resource per complete CRUD group (Create + Read + Delete), plus
// the set of (path, method) pairs those resources consume so the per-operation
// pass does not double-emit them. Incomplete groups are left to the per-operation
// classification. This is the §3 fix: real specs with a collection POST plus an
// instance GET/DELETE now yield a single wired resource instead of separate
// partial resources.
func buildGroupedResources(spec *parser.Spec, providerName string, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation, overrides []config.ResourceOverride, diags *diagnostics.Diagnostics, usePutAsCreate bool) ([]ir.ResourceIR, map[string]map[string]bool) {
	if len(pathOps) == 0 {
		return nil, nil
	}
	groups := transformer.InferResourceCRUDWithDiagnostics(pathOps, usePutAsCreate, diags)

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
		if !groupIsResource(g, pathOps) {
			continue
		}

		// The CRUD group's name is derived from the last path segment (e.g.
		// "trafficPolicyGraph", "alert-policy"); normalize it to snake_case so the
		// Terraform type name is a valid, conventional HCL identifier (camelCase
		// is a convention violation; hyphens make the resource handle unreferenceable
		// in expressions, e.g. gigamon_alert-policy.x lexes as subtraction). This
		// mirrors the data-source/list/action naming convention.
		name := transformer.ToSnakeCase(g.Name)
		res := ir.ResourceIR{
			Name:            name,
			FullName:        providerName + "_" + name,
			TypeName:        providerName + "_" + name,
			Description:     operationDescription(crudGroupDescriptionOp(spec, g), fmt.Sprintf("Manages the %s resource.", humanizeConstructName(name))),
			SourceOperation: groupSourceOperation(g),
			// A CRUD group whose source operation is deprecated surfaces as a
			// deprecated resource so the flag reaches the generated schema (M-10).
			DeprecationMessage: groupDeprecationMessage(spec, g),
			// Metadata for list-resource pairing: promoted list resources whose
			// collection endpoint is this group's collection path are renamed to
			// this resource so the framework can register them (pairListResource-
			// Registrations).
			CollectionPath: g.CollectionPath,
		}
		// The Create mapping is built from the resolved Create op's method and
		// path so it is honest for both POST-create (method POST, collection path)
		// and PUT-as-create (method PUT, instance path). g.Create.Method is POST
		// and g.Create.Path is the collection path for a POST-create group, so
		// existing specs are byte-identical; a PUT-as-create group carries the
		// instance-path PUT here instead of a hard-coded POST on the collection.
		createMethod, createPath := string(g.Create.Method), g.Create.Path
		res.CRUDMapping.Create = resourceOperationMapping(spec, createMethod, createPath, parserOp(spec, createPath, createMethod), envelopeOf(g.Create))
		res.CRUDMapping.Create.MediaType = mediaTypeOf(g.Create)
		res.CRUDMapping.Create.ResponseInnerPath = detectResponseInnerPath(g.Create, g.Read)
		res.CRUDMapping.Read = resourceOperationMapping(spec, "GET", g.InstancePath, parserOp(spec, g.InstancePath, "GET"), envelopeOf(g.Read))
		res.CRUDMapping.Read.MediaType = mediaTypeOf(g.Read)
		// A placeholder-free GET returns the whole collection, not one
		// instance: record it so the generated read selects the element whose
		// identifier matches (and reports the resource removed when none does)
		// instead of blindly reading the first element (G39). A read whose path
		// carries placeholders is an instance read; an array response there is
		// a get-one wrapper (issue #35), where the first element IS the
		// instance.
		if isCollectionRead(g, g.Read.Path) {
			res.CRUDMapping.Read.ResponseIsCollection = true
		}
		if g.Update != nil {
			updMethod := "PUT"
			if g.FullUpdate == nil && g.PartialUpdate != nil {
				updMethod = "PATCH"
			}
			upd := resourceOperationMapping(spec, updMethod, g.InstancePath, parserOp(spec, g.InstancePath, updMethod), envelopeOf(g.Update))
			upd.MediaType = mediaTypeOf(g.Update)
			upd.ResponseInnerPath = detectResponseInnerPath(g.Update, g.Read)
			res.CRUDMapping.Update = &upd
		}
		res.CRUDMapping.Delete = resourceOperationMapping(spec, "DELETE", g.InstancePath, parserOp(spec, g.InstancePath, "DELETE"), envelopeOf(g.Delete))
		res.CRUDMapping.Delete.MediaType = mediaTypeOf(g.Delete)

		// An override that explicitly configures the identifier (id_attribute or
		// import_format) disables the user-settable-identifier preference in the
		// schema builder: the practitioner has chosen the ID attribute, so eidos
		// must not guess a different one and leave the override referencing an
		// attribute the schema no longer carries (e.g. archive_server's
		// id_attribute: server_alias). The gate mirrors the one
		// applyResourceCreationOverrides applies to override-created resources.
		skipUserSettableID := resourceOverrideConfiguresID(overrides, res)
		schema, idAttr := transformer.ManagedResourceSchemaWithDiagnostics(g, diags, skipUserSettableID, false)
		res.Schema = schema
		res.IDAttribute = idAttr
		// Fail loud when a managed resource ends up with no practitioner-writable
		// attributes. This happens when the inferred create operation declares no
		// request body (a spec defect — e.g. an archived/incomplete spec that
		// omits the POST body) so every attribute is derived from the read
		// response and is Computed-only. A resource with no Required/Optional
		// attributes cannot be created or updated by a practitioner, so surface it
		// as a Warning naming the resource and its create operation instead of
		// silently emitting a degenerate, unwritable resource (fail-loud).
		warnUnwritableManagedResource(diags, res, g.Create)
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
		// When the override explicitly configures the identifier (id_attribute or
		// import_format), the inference-time import gate is superseded: the
		// override's applyResourceIDOverride / applyResourceImportFormatOverride
		// re-derives the format and emits the accurate warning if the chosen
		// attribute is Computed-only. Warning here would be stale — the inferred
		// Computed-only id is exactly what the override replaces (e.g.
		// gigavuecore's activation: inferred entl_item_id → id_attribute eli_id).
		importDiags := diags
		if skipUserSettableID {
			importDiags = nil
		}
		if importFmt, ok := groupedImportFormatWithDiagnostics(g, schema, idAttr, importDiags); ok {
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
			// When an instance path has both PUT and PATCH, the PUT wins as
			// the Update mapping (chooseUpdateOps) and the PATCH is consumed
			// here — the only silent operation drop left in the pipeline. The
			// PATCH's partial-body contract (a different request schema, often
			// different required fields) is real API surface, so surface the
			// loss instead of discarding it without a trace (AGENTS.md "fail
			// loud, never silently").
			if g.FullUpdate != nil {
				patchID := g.PartialUpdate.OperationID
				if strings.TrimSpace(patchID) == "" {
					patchID = "PATCH " + g.InstancePath
				}
				*diags = append(*diags, diagnostics.Diagnostic{
					Severity: diagnostics.Warning,
					Summary:  fmt.Sprintf("PATCH %q is shadowed by the sibling PUT and not generated", patchID),
					Detail: fmt.Sprintf(
						"The instance path %s declares both PUT and PATCH; the resource %q uses the PUT as its "+
							"Update mapping and the PATCH %s is consumed without generating any construct, so its "+
							"partial-body semantics are lost. To expose the PATCH, add a generator.yaml "+
							"action_override that matches \"PATCH %s\".",
						g.InstancePath, res.Name, patchID, g.InstancePath),
				})
			}
		}
		markConsumed(consumed, g.InstancePath, "DELETE")
	}

	sort.Slice(resources, func(i, j int) bool { return resources[i].Name < resources[j].Name })
	return resources, consumed
}

// resourceOverrideConfiguresID reports whether any resource override matching the
// resource explicitly configures its identifier (id_attribute or import_format).
// The schema builder's user-settable-identifier preference must be disabled in
// that case: the practitioner has chosen the ID attribute, so eidos must not
// guess a different one and leave the override referencing an attribute the
// schema no longer carries (e.g. archive_server's id_attribute: server_alias).
// Matching mirrors applyResourceOverrides (transformer.ResourceOverrideMatches),
// so a grouped resource whose override sets the identifier is gated the same
// way an override-created resource is in applyResourceCreationOverrides.
func resourceOverrideConfiguresID(overrides []config.ResourceOverride, r ir.ResourceIR) bool {
	for _, o := range overrides {
		if !transformer.ResourceOverrideMatches(r, o) {
			continue
		}
		if strings.TrimSpace(o.IDAttribute) != "" || strings.TrimSpace(o.ImportFormat) != "" {
			return true
		}
	}
	return false
}

// emitPutAsCreateInfoDiagnostics surfaces an Info diagnostic for each surviving
// resource whose Create was inferred from the instance-path PUT (PUT-as-create
// upsert). inferredPutCreates is the set of create path templates that
// buildGroupedResources resolved via a PUT; only resources still present in the
// preview (i.e. not dropped by skip: true) and whose create path is in that set
// receive the diagnostic. The emitted list is sorted by resource name so the
// diagnostic order is deterministic for byte-identical generation.
func emitPutAsCreateInfoDiagnostics(preview *ir.ProviderIR, inferredPutCreates map[string]bool, diags *diagnostics.Diagnostics) {
	if len(inferredPutCreates) == 0 {
		return
	}
	type putCreate struct {
		name string
		path string
	}
	var puts []putCreate
	for _, r := range preview.Resources {
		if r.CRUDMapping.Create.Method != string(transformer.MethodPut) {
			continue
		}
		if !inferredPutCreates[r.CRUDMapping.Create.PathTemplate] {
			continue
		}
		puts = append(puts, putCreate{name: r.Name, path: r.CRUDMapping.Create.PathTemplate})
	}
	sort.Slice(puts, func(i, j int) bool { return puts[i].name < puts[j].name })
	for _, p := range puts {
		*diags = append(*diags, diagnostics.Diagnostic{
			Severity: diagnostics.Info,
			Summary:  fmt.Sprintf("using PUT %s as Create (upsert)", p.path),
			Detail: fmt.Sprintf(
				"resource %s: no collection POST exists; the instance-path PUT is used as the Create (upsert) mapping. "+
					"Set use_put_as_create: false to disable, or skip: true on this resource to drop it.",
				p.name),
		})
	}
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
func applyResourceCreationOverrides(preview *ir.ProviderIR, spec *parser.Spec, providerName string, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation, overrides []config.ResourceOverride, consumed map[string]map[string]bool, diags *diagnostics.Diagnostics) {
	for _, ro := range overrides {
		applyResourceCreationOverride(preview, spec, providerName, pathOps, ro, consumed, diags)
	}
}

// applyResourceCreationOverride promotes a single resource_override to a managed
// resource when it targets an operation that inference classified as an action
// (G8). The override may specify explicit read/update/delete operations for
// entities whose create path differs from their read/delete path (e.g. MyCloud
// dashboards: POST /dashboards/db vs GET|DELETE /dashboards/uid/{uid}). The
// generated resource is wired to the resolved operations and its schema is
// reconciled from the create request body and the read response. Operations the
// override consumes are marked so the per-operation pass does not double-emit
// them as actions.
func applyResourceCreationOverride(preview *ir.ProviderIR, spec *parser.Spec, providerName string, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation, ro config.ResourceOverride, consumed map[string]map[string]bool, diags *diagnostics.Diagnostics) {
	gen := ro.GenerateResource != nil && *ro.GenerateResource
	explicit := ro.CreateOperation != "" || ro.ReadOperation != "" || ro.UpdateOperation != "" || ro.DeleteOperation != ""
	if !gen && !explicit {
		return
	}
	seed := ro.Operation
	if seed == "" {
		seed = ro.CreateOperation
	}
	if seed == "" {
		return
	}
	createPath, createMethod, createOp := resolveOperationByID(pathOps, seed)
	if createOp == nil {
		return
	}
	// Skip if the seed operation is already consumed by an inferred resource
	// (the existing applyResourceOverrides mutates those).
	if isConsumed(consumed, createPath, createMethod) {
		return
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
	// The identifier comes from the instance path, mirroring how
	// buildResourceCRUD derives it from the deepest parameterized path. For an
	// override-created resource the read may be a collection GET (e.g.
	// intent_policy's GET /intent/policies filtered by a name query param)
	// with no path parameters, so the identity is taken from the first of
	// read/update/delete whose path carries parameters. Without this, g.ID
	// stays zero-valued and resourceFromOverrideCRUD cannot wire an import
	// even when the update/delete path parameter maps to a real schema
	// attribute (e.g. {name} → name) — the "many resources are missing
	// imports" gap.
	for _, p := range []string{readPath, updatePath, deletePath} {
		if p == "" {
			continue
		}
		if id := transformer.DetectIDFromPath(p); len(id.ParameterNames) > 0 {
			g.ID = id
			break
		}
	}
	// An override that explicitly configures the identifier (id_attribute or
	// import_format) disables the user-settable-identifier preference in the
	// schema builder: the practitioner has chosen the ID attribute, so eidos
	// must not guess a different one and leave the override referencing an
	// attribute the schema no longer carries (e.g. archive_server's
	// id_attribute: server_alias).
	skipUserSettableID := strings.TrimSpace(ro.IDAttribute) != "" || strings.TrimSpace(ro.ImportFormat) != ""
	res := resourceFromOverrideCRUD(spec, providerName, g, diags, skipUserSettableID, ro.IncludeCreateResponseAttributes, ro.ReadCollectionPath)
	if res == nil {
		return
	}
	// The override's id_attribute is applied later by applyResourceIDOverride
	// (via transformer.ApplyOverridesWithDiagnostics), which also drops the
	// superseded synthetic placeholder when the override names a different
	// attribute (e.g. spacetraders' ship: {shipSymbol} → the Computed "symbol"
	// property). Setting it here would make applyResourceIDOverride see
	// old == newID and skip the drop, leaving the dead placeholder in the schema.
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
//
// skipUserSettableID is true when the override explicitly configures the
// identifier (id_attribute or import_format): the schema builder then keeps the
// synthetic Computed placeholder named for the path parameter instead of
// preferring a practitioner-supplied create-body attribute, so the override's
// chosen attribute stays present in the schema.
//
// includeCreateResponse lists create-response-only properties to keep as
// Computed attributes (config ResourceOverride.IncludeCreateResponseAttributes).
func resourceFromOverrideCRUD(spec *parser.Spec, providerName string, g transformer.ResourceCRUD, diags *diagnostics.Diagnostics, skipUserSettableID bool, includeCreateResponse []string, readCollectionPath string) *ir.ResourceIR {
	// Validate the read_collection_path once, mirroring
	// transformer.applyResourceReadCollectionPath: a malformed path (empty
	// segment, wildcard mid-path) must never reach the generator, whose
	// navigation would silently resolve to an empty array and report the
	// resource removed on every read. Drop the override fail-loud instead —
	// and with it the child-resource state shape, which is only honest when
	// the nested read actually selects the element.
	nestedPath := strings.TrimSpace(readCollectionPath)
	childRead := nestedPath != ""
	if childRead && !transformer.ValidReadCollectionPath(nestedPath) {
		childRead = false
		nestedPath = ""
		if diags != nil {
			*diags = append(*diags, diagnostics.Diagnostic{
				Severity: diagnostics.Warning,
				Summary:  "read_collection_path override cannot be applied",
				Detail: fmt.Sprintf(
					"Resource %q: read_collection_path %q must be a dot-separated path with a wildcard only in the final segment (e.g. \"rules.*\").",
					transformer.ToSnakeCase(g.Name), strings.TrimSpace(readCollectionPath),
				),
			})
		}
	}
	// Normalize the CRUD group name to snake_case for the Terraform type name
	// (camelCase is a convention violation; hyphens make the resource handle
	// unreferenceable in HCL expressions). See the inferred-group counterpart
	// above; the Go struct/file names derive from Name via naming.PascalCase /
	// naming.SnakeCase, which are idempotent on an already-snake name.
	name := transformer.ToSnakeCase(g.Name)
	res := ir.ResourceIR{
		Name:            name,
		FullName:        providerName + "_" + name,
		TypeName:        providerName + "_" + name,
		Description:     operationDescription(crudGroupDescriptionOp(spec, g), fmt.Sprintf("Manages the %s resource.", humanizeConstructName(name))),
		SourceOperation: groupSourceOperation(g),
		// A CRUD group whose source operation is deprecated surfaces as a
		// deprecated resource so the flag reaches the generated schema (M-10).
		DeprecationMessage: groupDeprecationMessage(spec, g),
		// The collection path is what pairs this resource with a list resource
		// (pairListResourceRegistrations). Inferred groups set it in
		// buildGroupedResources; override-created groups must carry it too or the
		// paired list resource stays unregistered (G8).
		CollectionPath: g.CollectionPath,
	}
	if g.Create != nil {
		res.CRUDMapping.Create = resourceOperationMapping(spec, string(g.Create.Method), g.Create.Path, parserOp(spec, g.Create.Path, string(g.Create.Method)), envelopeOf(g.Create))
		res.CRUDMapping.Create.MediaType = mediaTypeOf(g.Create)
		if g.Read != nil {
			res.CRUDMapping.Create.ResponseInnerPath = detectResponseInnerPath(g.Create, g.Read)
		}
	}
	if g.Read != nil {
		res.CRUDMapping.Read = resourceOperationMapping(spec, string(g.Read.Method), g.Read.Path, parserOp(spec, g.Read.Path, string(g.Read.Method)), envelopeOf(g.Read))
		res.CRUDMapping.Read.MediaType = mediaTypeOf(g.Read)
		// Mirror the grouped path: a placeholder-free array read selects the
		// matching element by identifier (G39).
		if isCollectionRead(g, g.Read.Path) {
			res.CRUDMapping.Read.ResponseIsCollection = true
		}
		// A child resource's read is a parent GET whose response nests the
		// collection under a path (e.g. a port filter rule read via
		// GET /portFilters/{portId}, with the rules at "portFilter.rules.*").
		// The read is a collection read by construction — the generated body
		// selects the element whose identifier matches state — even though the
		// parent path carries a placeholder, which isCollectionRead would
		// reject. The override's read_collection_path supplies the nested path.
		if childRead {
			res.CRUDMapping.Read.NestedCollectionPath = nestedPath
			res.CRUDMapping.Read.ResponseIsCollection = true
		}
	}
	if g.Update != nil {
		upd := resourceOperationMapping(spec, string(g.Update.Method), g.Update.Path, parserOp(spec, g.Update.Path, string(g.Update.Method)), envelopeOf(g.Update))
		upd.MediaType = mediaTypeOf(g.Update)
		if g.Read != nil {
			upd.ResponseInnerPath = detectResponseInnerPath(g.Update, g.Read)
		}
		res.CRUDMapping.Update = &upd
	}
	if g.Delete != nil {
		res.CRUDMapping.Delete = resourceOperationMapping(spec, string(g.Delete.Method), g.Delete.Path, parserOp(spec, g.Delete.Path, string(g.Delete.Method)), envelopeOf(g.Delete))
		res.CRUDMapping.Delete.MediaType = mediaTypeOf(g.Delete)
	}
	schema, idAttr := transformer.ManagedResourceSchemaWithDiagnostics(g, diags, skipUserSettableID, childRead)
	res.Schema = schema
	res.IDAttribute = idAttr
	// Create-response-only properties the override asked to keep (e.g. an
	// activation id returned by POST but absent from the collection read) are
	// appended as Computed attributes so the resource can track and delete the
	// instance. Carried on the IR so the config generator re-emits the override.
	res.IncludeCreateResponseAttributes = includeCreateResponse
	var createResp *transformer.SchemaSpec
	if g.Create != nil {
		createResp = g.Create.ResponseSchema
	}
	transformer.AddCreateResponseAttributes(&res.Schema, createResp, includeCreateResponse, diags)
	// Import wiring for override-created resources, mirroring the grouped path
	// (buildGroupedResources). An override-created resource is importable when
	// its identifier attribute(s) are real schema attributes the import can
	// populate; groupedImportFormat returns ok=false otherwise and the resource
	// stays honestly non-importable. An explicit import_format override applied
	// later (applyResourceImportFormatOverride) supersedes this inferred format.
	// Same stale-warning suppression as the inferred-group path: an override
	// that configures the identifier supersedes the inference-time import gate,
	// and the override's own application emits the accurate warning.
	importDiags := diags
	if skipUserSettableID {
		importDiags = nil
	}
	if importFmt, ok := groupedImportFormatWithDiagnostics(g, schema, idAttr, importDiags); ok {
		res.ImportIDFormat = importFmt
		res.Importable = true
	}
	res.OverrideCreated = true
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

// detectResponseInnerPath identifies the property under which a create or
// update response nests the created/updated resource alongside side-effect
// objects. The single-key envelope unwrap (transformer.UnwrapResponseEnvelope)
// strips one {data: ...} wrapper, but some create/update responses wrap the
// resource under a named property of that wrapper (e.g. SpaceTraders
// purchase-ship returns {data:{ship:{...},transaction:{...},agent:{...}}},
// where the ship is under "ship"). After the envelope is stripped the
// response is {ship, transaction, agent}, which does not match the resource
// model directly, so the generator would find no identifier and fail to track
// the resource.
//
// This matches a property of the unwrapped create/update response whose $ref
// (RefName) equals the resource's read response $ref, returning that property
// name so the generator navigates into it before applying the body to the
// model. It returns "" when the response applies directly after the envelope
// unwrap (the create response already IS the resource), when either side has
// no resolvable $ref, or when the match is ambiguous (zero or more than one
// property references the resource schema) — in those cases the generator
// applies the body as-is and surfaces a clear runtime diagnostic if the
// identifier remains unset (fail-loud, never silent).
func detectResponseInnerPath(write, read *transformer.Operation) string {
	if write == nil || write.ResponseSchema == nil || read == nil || read.ResponseSchema == nil {
		return ""
	}
	readRef := strings.TrimSpace(read.ResponseSchema.RefName)
	if readRef == "" {
		return ""
	}
	// The response applies directly when the create/update response itself is
	// the resource (same RefName) — no inner navigation needed.
	if strings.EqualFold(strings.TrimSpace(write.ResponseSchema.RefName), readRef) {
		return ""
	}
	if !strings.EqualFold(write.ResponseSchema.Type, "object") || len(write.ResponseSchema.Properties) == 0 {
		return ""
	}
	var match string
	count := 0
	for name, p := range write.ResponseSchema.Properties {
		if !strings.EqualFold(strings.TrimSpace(p.RefName), readRef) {
			continue
		}
		count++
		if count == 1 {
			match = name
		}
	}
	if count != 1 {
		return ""
	}
	return match
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
	return groupedImportFormatWithDiagnostics(g, schema, idAttr, nil)
}

// groupedImportFormatWithDiagnostics is groupedImportFormat that appends
// fail-loud warnings to diags (a nil diags is allowed and simply suppresses
// emission). It warns when a resource stays non-importable because an import
// target attribute is Computed-only (a value the practitioner cannot know) or
// because a required read parameter has no schema attribute.
func groupedImportFormatWithDiagnostics(g transformer.ResourceCRUD, schema ir.ObjectSchemaIR, idAttr string, diags *diagnostics.Diagnostics) (string, bool) {
	findAttr := func(name string) (ir.AttributeIR, bool) {
		for _, a := range schema.Attributes {
			if a.Name == name {
				return a, true
			}
		}
		return ir.AttributeIR{}, false
	}
	// userSettable reports whether the practitioner can supply the attribute's
	// value in configuration. An import ID segment must reference a value the
	// practitioner knows before the first read — a Computed-only attribute
	// (server-assigned, e.g. a server-generated id) is not knowable, so an
	// import that targets it can never succeed (G39).
	userSettable := func(name string) bool {
		a, ok := findAttr(name)
		return ok && !a.ComputedOnly()
	}

	var parts []string
	switch g.ID.Kind {
	case transformer.IDComposite:
		p, ok := compositeImportParts(g, userSettable, diags)
		if !ok {
			return "", false
		}
		parts = p
	default: // IDSimple
		if len(g.ID.ParameterNames) == 0 && len(requiredReadParams(g.Read)) == 0 {
			// Singleton resource: the read substitutes nothing into the path
			// and carries no required query/header parameters, so any import
			// ID refreshes cleanly. The import populates the resolved ID
			// attribute with the raw req.ID even when it is Computed-only —
			// the refresh that follows ignores it and repopulates state from
			// the response (§3.13: GigaVUE-FM's copilot_config, a static
			// /copilot/config endpoint, previously stayed non-importable
			// because its only identity candidate is a computed id echo).
			p, ok := singletonImportBase(g, findAttr, idAttr, diags)
			if !ok {
				return "", false
			}
			parts = []string{"{" + p + "}"}
		} else {
			p, ok := simpleImportBase(g, findAttr, userSettable, idAttr, diags)
			if !ok {
				return "", false
			}
			parts = []string{"{" + p + "}"}
		}
	}
	parts, ok := appendRequiredReadParams(g, parts, findAttr, userSettable, diags)
	if !ok {
		return "", false
	}
	return strings.Join(parts, ":"), true
}

// compositeImportParts builds the {param} segments for a composite-identity
// resource: every path parameter, sanitized, must be user-settable or the
// resource is not importable (warnNotImportable fires via diags).
func compositeImportParts(g transformer.ResourceCRUD, userSettable func(string) bool, diags *diagnostics.Diagnostics) ([]string, bool) {
	if len(g.ID.ParameterNames) == 0 {
		return nil, false
	}
	parts := make([]string, 0, len(g.ID.ParameterNames))
	for _, p := range g.ID.ParameterNames {
		snake := transformer.ToSnakeCase(p)
		if !userSettable(snake) {
			warnNotImportable(diags, g.Name, snake, "composite path parameter")
			return nil, false
		}
		parts = append(parts, "{"+snake+"}")
	}
	return parts, true
}

// simpleImportBase resolves the single attribute an import populates for a
// simple-identity resource.
//
// The import must populate the attribute the read substitutes into the path
// placeholder. When a schema attribute carries the raw path parameter name
// (e.g. {name} → name), that attribute is what the read uses; the resolved
// ID attribute (e.g. "id" from a response echo) may be a different field the
// read does not substitute — importing by it would set the wrong attribute
// and the follow-up read would 404 (e.g. intent_policy's {name} path with an
// "id" response property). Fall back to the resolved ID attribute only when
// no attribute matches the path parameter name.
func simpleImportBase(g transformer.ResourceCRUD, findAttr func(string) (ir.AttributeIR, bool), userSettable func(string) bool, idAttr string, diags *diagnostics.Diagnostics) (string, bool) {
	base := ""
	if len(g.ID.ParameterNames) > 0 {
		if _, ok := findAttr(g.ID.ParameterNames[0]); ok {
			base = g.ID.ParameterNames[0]
		}
	}
	if base == "" {
		base = idAttr
		if base == "" {
			base = "id"
		}
	}
	if _, ok := findAttr(base); !ok {
		return "", false
	}
	if !userSettable(base) {
		warnNotImportable(diags, g.Name, base, "identifier")
		return "", false
	}
	return base, true
}

// singletonImportBase resolves the attribute a singleton resource's import
// populates. A singleton's read substitutes nothing into the path, so any
// import ID refreshes cleanly; the import stores the raw req.ID in the
// resolved ID attribute even when that attribute is Computed-only (the
// refresh that follows repopulates it from the response). ok=false when the
// resource carries no such attribute at all, so the import would target an
// attribute that does not exist — the resource stays non-importable, honest
// rather than silent. An Info diagnostic notes the placeholder semantics so
// provider authors see why import succeeded without a real identity.
func singletonImportBase(g transformer.ResourceCRUD, findAttr func(string) (ir.AttributeIR, bool), idAttr string, diags *diagnostics.Diagnostics) (string, bool) {
	base := idAttr
	if base == "" {
		base = "id"
	}
	if _, ok := findAttr(base); !ok {
		return "", false
	}
	if diags != nil {
		*diags = append(*diags, diagnostics.Diagnostic{
			Severity: diagnostics.Info,
			Summary:  "singleton resource import uses a placeholder ID",
			Detail: fmt.Sprintf(
				"Resource %q reads without any path or required query parameters, so its import accepts any identifier and stores it in attribute %q; the refresh that follows the import repopulates state from the response.",
				g.Name, base,
			),
		})
	}
	return base, true
}

// appendRequiredReadParams extends parts with the read operation's required
// query/header parameters. A required query/header parameter on the READ is
// sent from state on the refresh that follows every import, so the import must
// populate it too — otherwise the generated read goes out with an empty value
// the API rejects (e.g. GigaVUE-FM's required clusterId query parameter on
// /portConfig/gigastreams/advHash/{slotId}) (G39). Parameters whose schema
// attribute is user-settable join the composite import format; a Computed-only
// one is not knowable by the practitioner, so the resource stays honestly
// non-importable with a warning (ok reports whether the format survived).
func appendRequiredReadParams(g transformer.ResourceCRUD, parts []string, findAttr func(string) (ir.AttributeIR, bool), userSettable func(string) bool, diags *diagnostics.Diagnostics) ([]string, bool) {
	for _, p := range requiredReadParams(g.Read) {
		if _, ok := findAttr(p); !ok {
			continue // no schema attribute: the read is scaffolded anyway
		}
		if !userSettable(p) {
			warnNotImportable(diags, g.Name, p, "required read parameter")
			return parts, false
		}
		dup := false
		for _, existing := range parts {
			if existing == "{"+p+"}" {
				dup = true
				break
			}
		}
		if !dup {
			parts = append(parts, "{"+p+"}")
		}
	}
	return parts, true
}

// requiredReadParams returns the sanitized names of the read operation's
// required query and header parameters, sorted for deterministic import
// formats. Path parameters are handled by the identifier logic and are
// excluded.
func requiredReadParams(read *transformer.Operation) []string {
	if read == nil {
		return nil
	}
	var names []string
	for _, p := range read.Parameters {
		in := strings.ToLower(p.In)
		if in != "query" && in != "header" {
			continue
		}
		if !p.Required {
			continue
		}
		names = append(names, transformer.SanitizeAttributeName(p.Name))
	}
	sort.Strings(names)
	return names
}

// warnNotImportable surfaces why a resource's import was suppressed: the
// import target attribute is Computed-only, a value the practitioner cannot
// know before the first read (G39).
func warnNotImportable(diags *diagnostics.Diagnostics, resource, attr, role string) {
	if diags == nil {
		return
	}
	*diags = append(*diags, diagnostics.Diagnostic{
		Severity: diagnostics.Warning,
		Summary:  "import suppressed: import target is computed-only",
		Detail: fmt.Sprintf(
			"Resource %q stays non-importable because its %s attribute %q is Computed-only — the practitioner cannot know the value before the first read, so an import referencing it cannot succeed. Make the attribute user-settable (required or optional in the request) or choose a user-settable identifier via generator.yaml id_attribute/import_format.",
			resource, role, attr,
		),
	})
}

// groupIsResource reports whether every CRUD operation in the group classifies as
// a resource under the explicit-extension / path-keyword rules. It returns false
// when any operation is claimed as an action, ephemeral, function, or list. The
// method-based kindResource/kindDataSource distinction is deliberately not checked
// here: grouping reclassifies an instance GET as a resource Read.
// groupIsResource reports whether the CRUD group's operations survive
// reclassification as a managed resource: each of its lifecycle operations must
// still classify as a resource under per-operation rules. An explicit
// x-terraform-* extension or a path keyword (e.g. "convert", "search", "query"
// making the instance GET a provider function) rejects the group, because the
// operation is meant to be a function/action/ephemeral/list, not a resource
// lifecycle step.
//
// The parser operations are reconstructed from the group's transformer
// Operations rather than via parserOp(spec, ...) so the check needs no *parser.Spec
// and can be reused by classifyOperation's resource gate (groupEmitsFullCRUDResource)
// without threading spec through classifyOperation. transformer.Operation carries
// OperationID and Extensions — the only parser.Operation fields classifyOperation
// consults — so the reconstruction is equivalent for classification.
func groupIsResource(g transformer.ResourceCRUD, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation) bool {
	// opRef pairs the path classifyOperation expects for a lifecycle step
	// (g.CollectionPath for Create, g.InstancePath for Read/Update/Delete) with
	// the HTTP method and the transformer Operation supplying OperationID and
	// Extensions. The path comes from the group rather than the op so callers
	// building synthetic ResourceCRUDs (and production, where InferResourceCRUD
	// sets both consistently) need not populate Operation.Path.
	type opRef struct {
		path   string
		method string
		op     *transformer.Operation
	}
	var opRefs []opRef
	add := func(path, method string, top *transformer.Operation) {
		if top != nil {
			opRefs = append(opRefs, opRef{path, method, top})
		}
	}
	add(g.CollectionPath, "POST", g.Create)
	add(g.InstancePath, "GET", g.Read)
	add(g.InstancePath, "DELETE", g.Delete)
	add(g.InstancePath, "PUT", g.FullUpdate)
	add(g.InstancePath, "PATCH", g.PartialUpdate)
	for _, ref := range opRefs {
		pop := &parser.Operation{OperationID: ref.op.OperationID, Extensions: ref.op.Extensions}
		// checkFullCRUD is false here: the group's operations are already known
		// to form a complete CRUD group, so the CRUD-completeness reclassification
		// (which turns a partial-update PATCH into an action) must not veto the
		// group. Only explicit extensions and path keywords reject a group.
		switch classifyOperation(ref.path, ref.method, pop, pathOps, false) {
		case kindAction, kindEphemeral, kindFunction, kindListResource:
			return false
		}
	}
	return true
}

// groupEmitsFullCRUDResource reports whether the operation at (path, method)
// belongs to a CRUD group that buildGroupedResources would actually emit as a
// managed resource: the group must be structurally complete (Create + Read +
// Delete) AND pass groupIsResource (its lifecycle ops are not reclassified as a
// function/action/ephemeral/list by an x-terraform-* extension or a path
// keyword). This is the gate classifyOperation applies in place of the
// structural-only transformer.HasFullCRUD: without the groupIsResource overlay,
// an operation whose group is rejected (e.g. a /convert/.../rules POST whose
// sibling instance GET is a provider function via the "convert" keyword) would
// still classify as kindResource and resourceFromOperation would emit an empty,
// fully-scaffolded orphan resource — worse than the action it should become,
// matching the HasFullCRUD design intent ("a scaffolded resource with an empty
// model is worse than a wired action"). The structural check is equivalent to
// transformer.HasFullCRUD; groupIsResource is the overlay that was missing.
func groupEmitsFullCRUDResource(path, method string, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation) bool {
	hm := transformer.HTTPMethod(strings.ToUpper(method))
	// The classification gate deliberately runs CRUD inference with PUT-as-create
	// DISABLED (false). This gate decides whether an operation NOT already
	// consumed by buildGroupedResources should be a resource lifecycle step or an
	// action. A PUT-as-create group is either emitted by buildGroupedResources
	// (default-on, passing groupIsResource) — in which case its ops are consumed
	// and this gate is never reached — or it is skipped (kill-switch, or the group
	// is rejected by groupIsResource). In both skip cases the PUT should become an
	// action, not a scaffolded Update-only orphan resource ("a scaffolded resource
	// with an empty model is worse than a wired action"). Running with false makes
	// the gate see no full-CRUD group for a PUT+GET+DELETE-no-POST triple, so the
	// PUT classifies as an action — the correct outcome in every skip case. This
	// also keeps the gate's behavior byte-identical to before PUT-as-create existed.
	for _, g := range transformer.InferResourceCRUD(pathOps, false) {
		if g.Create == nil || g.Read == nil || g.Delete == nil {
			continue
		}
		if !opInGroup(g, path, hm) {
			continue
		}
		if groupIsResource(g, pathOps) {
			return true
		}
	}
	return false
}

// opInGroup reports whether the CRUD group contains the operation at (path,
// method), comparing against the group's Create/Read/Update/Delete operations.
// transformer.groupHasOperation is unexported, so this mirrors it for use in
// the handler's classification gate.
func opInGroup(g transformer.ResourceCRUD, path string, method transformer.HTTPMethod) bool {
	for _, op := range []*transformer.Operation{g.Create, g.Read, g.Update, g.Delete} {
		if op != nil && op.Path == path && op.Method == method {
			return true
		}
	}
	return false
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

// groupDeprecationMessage returns the deprecation message for a CRUD group whose
// source operation declares deprecated: true, mirroring groupSourceOperation's
// priority (create, then read, then delete) so the message tracks the same
// operation the resource is sourced from (M-10).
func groupDeprecationMessage(spec *parser.Spec, g transformer.ResourceCRUD) string {
	if g.Create != nil {
		if op := parserOp(spec, g.Create.Path, string(g.Create.Method)); op != nil && op.Deprecated {
			return deprecationMessage("resource", true)
		}
	}
	if g.Read != nil {
		if op := parserOp(spec, g.Read.Path, string(g.Read.Method)); op != nil && op.Deprecated {
			return deprecationMessage("resource", true)
		}
	}
	if g.Delete != nil {
		if op := parserOp(spec, g.Delete.Path, string(g.Delete.Method)); op != nil && op.Deprecated {
			return deprecationMessage("resource", true)
		}
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

// crudGroupDescriptionOp returns the parser operation whose summary/description
// best describes a CRUD-grouped resource, preferring the Create operation then
// the Read (GET instance) operation. A resource's doc page is primarily about
// creating and managing the resource, and the Create operation's description
// ("Create a new X") is usually more accurate than the Read's ("Get X"). It
// feeds operationDescription so a grouped resource gets the same description
// fallback chain as a single-op construct (spec description if a real sentence,
// else summary, else a generated "Manages the X resource." phrase).
func crudGroupDescriptionOp(spec *parser.Spec, g transformer.ResourceCRUD) *parser.Operation {
	if g.Create != nil {
		if op := parserOp(spec, g.Create.Path, string(g.Create.Method)); op != nil {
			return op
		}
	}
	if g.Read != nil {
		if op := parserOp(spec, g.Read.Path, string(g.Read.Method)); op != nil {
			return op
		}
	}
	return nil
}

func resourceFromOperation(op *parser.Operation, providerName, path, method string, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation) ir.ResourceIR {
	name := resourceName(op, method, path)
	res := ir.ResourceIR{
		Name:            name,
		FullName:        providerName + "_" + name,
		TypeName:        providerName + "_" + name,
		Description:     operationDescription(op, fmt.Sprintf("Manages the %s resource.", humanizeConstructName(name))),
		SourceOperation: op.OperationID,
		// An operation marked deprecated: true surfaces as a deprecated resource
		// so the flag reaches the generated schema (M-10).
		DeprecationMessage: deprecationMessage("resource", op.Deprecated),
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

func dataSourceFromOperation(op *parser.Operation, providerName, path, method string, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation, pagination *ir.PaginationIR, diags *diagnostics.Diagnostics) ir.DataSourceIR {
	name := resourceName(op, method, path)
	ds := ir.DataSourceIR{
		Name:            name,
		FullName:        providerName + "_" + name,
		TypeName:        providerName + "_" + name,
		Description:     operationDescription(op, fmt.Sprintf("Reads the %s data source.", humanizeConstructName(name))),
		SourceOperation: op.OperationID,
		ReadMapping:     operationMapping(method, path, op, envelopeOfTransformerOp(pathOps, path, method)),
		// An operation marked deprecated: true surfaces as a deprecated data
		// source so the flag reaches the generated schema (M-10).
		DeprecationMessage: deprecationMessage("data source", op.Deprecated),
	}
	// Build the data source schema from the resolved read operation so the
	// generator can wire Read against real filter/output attributes instead of
	// an empty schema (REMAINING_GAPS §4). pathOps carries the operation's
	// resolved response schema and merged parameters; when the operation is not
	// present there (e.g. an unsupported method), the schema stays empty and the
	// generator keeps the honest scaffold Read body.
	if top := lookupTransformerOp(pathOps, path, method); top != nil {
		ds.Schema = transformer.DataSourceSchema(*top, diags)
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
	if op != nil {
		m.OperationID = op.OperationID
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
// parameters from structured ones. The schema's const/default/enum are carried
// so a path parameter that is not a resource attribute (e.g. a shared
// {apiVersion} versioning segment with enum ["v4","v4beta"]) can be substituted
// with a static literal by the generator's resolvePathSubstitution, wiring the
// resource instead of leaving it an honest scaffold.
func paramIR(p parser.Parameter) ir.ParamIR {
	s := ir.SchemaIR{Type: paramPrimitiveType(p.Schema), Format: paramFormat(p.Schema)}
	if p.Schema != nil {
		if len(p.Schema.Enum) > 0 {
			s.EnumValues = p.Schema.Enum
		}
		if p.Schema.Default != nil {
			d := p.Schema.Default
			s.Default = &d
		}
		if p.Schema.Const != nil {
			c := p.Schema.Const
			s.Const = &c
		}
	}
	return ir.ParamIR{
		Name:        p.Name,
		In:          p.In,
		Description: p.Description,
		Required:    p.Required,
		Schema:      s,
		Deprecated:  p.Deprecated,
	}
}

// resolveParamRef resolves a parser parameter's $ref against the spec's
// components/parameters, returning the dereferenced parameter. A parameter with
// no $ref (declared inline) is returned as-is. This mirrors the transformer's
// resolveParameter so path-level parameters referenced by $ref expose their
// schema (and const/default/enum) to paramIR.
func resolveParamRef(spec *parser.Spec, p parser.Parameter) *parser.Parameter {
	if p.Ref == "" || spec == nil {
		return &p
	}
	resolved, _ := spec.ResolveParameterReference(&p)
	return resolved
}

// pathParamIRs returns the in:path parameters declared on an operation, merging
// path-level (path-item) parameters with operation-level parameters per OpenAPI
// semantics: an operation-level parameter overrides a path-level parameter of
// the same name. Each parameter's $ref is resolved so its schema is available.
// The result is sorted by name for deterministic IR. Only managed-resource
// CRUD mappings consume PathParams (via resolvePathSubstitution); data sources,
// actions, ephemerals, and lists resolve their paths against their own config
// schemas, so this is only populated for resource operations.
func pathParamIRs(spec *parser.Spec, path string, op *parser.Operation) []ir.ParamIR {
	var pi *parser.PathItem
	if spec != nil && spec.Paths != nil {
		pi = spec.Paths[path]
	}
	if pi == nil && op == nil {
		return nil
	}
	seen := make(map[string]bool, 4)
	var out []ir.ParamIR
	// Operation-level parameters take precedence; add them first so a same-named
	// path-level parameter is skipped.
	if op != nil {
		for _, p := range op.Parameters {
			r := resolveParamRef(spec, p)
			if !strings.EqualFold(r.In, "path") {
				continue
			}
			out = append(out, paramIR(*r))
			seen[r.Name] = true
		}
	}
	if pi != nil {
		for _, p := range pi.Parameters {
			r := resolveParamRef(spec, p)
			if !strings.EqualFold(r.In, "path") {
				continue
			}
			if seen[r.Name] {
				continue
			}
			out = append(out, paramIR(*r))
			seen[r.Name] = true
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// resourceOperationMapping builds an operation mapping for a managed-resource
// CRUD step and populates PathParams from the operation's merged path-level and
// operation-level path parameters. This lets resolvePathSubstitution substitute
// a static literal for a shared path segment that is not a resource attribute
// (e.g. {apiVersion}), wiring the resource instead of leaving it scaffolded.
func resourceOperationMapping(spec *parser.Spec, method, path string, op *parser.Operation, responseEnvelope string) ir.OperationMappingIR {
	m := operationMapping(method, path, op, responseEnvelope)
	m.PathParams = pathParamIRs(spec, path, op)
	return m
}

// isCollectionRead reports whether a CRUD group's Read fetches the whole
// collection rather than one instance: the read operation's own path carries
// no dynamic placeholder (e.g. GigaVUE-FM reads GET /apps/diameter/whitelists
// while delete is DELETE /apps/diameter/whitelists/{alias}) and the read
// response — after the transformer's envelope unwrap — is an array of
// instances. The generated readRemote selects the matching element by
// identifier for such reads (G39).
func isCollectionRead(g transformer.ResourceCRUD, readPath string) bool {
	if g.Read == nil || g.Read.ResponseSchema == nil {
		return false
	}
	if strings.Contains(readPath, "{") {
		return false
	}
	return strings.EqualFold(g.Read.ResponseSchema.Type, "array")
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
		// A POST that is a CRUD Create but whose group buildGroupedResources would
		// not emit (either structurally incomplete — no full Create+Read+Delete —
		// or rejected by groupIsResource because a sibling op is a
		// function/action/ephemeral/list) is reclassified as an action: a scaffolded
		// resource with an empty model is worse than a wired action, and a resource
		// without a Delete cannot be destroyed by Terraform. The check is skipped
		// when classifying a group's own operations (groupIsResource), which are
		// already known to form a complete CRUD group.
		if checkFullCRUD && !groupEmitsFullCRUDResource(path, method, pathOps) {
			return kindAction
		}
		return kindResource
	case "PUT", "PATCH":
		if itemPath {
			if checkFullCRUD && !groupEmitsFullCRUDResource(path, method, pathOps) {
				return kindAction
			}
			return kindResource
		}
		// PUT/PATCH on a collection is an unusual bulk update; treat as action.
		return kindAction
	case "DELETE":
		if itemPath {
			if checkFullCRUD && !groupEmitsFullCRUDResource(path, method, pathOps) {
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

func actionFromOperation(op *parser.Operation, providerName, path, method string, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation, diags *diagnostics.Diagnostics) ir.ActionIR {
	name := resourceName(op, method, path)
	// Build the config schema from the operation's parameters AND request-body
	// properties (via the transformer's ObjectSchemaFromOperation, the same
	// builder the ephemeral path uses) so a body-bearing action — even one the
	// generator keeps honestly scaffolded because the client cannot send its
	// body — still surfaces its intended inputs to the practitioner. Without
	// this, the essential register action would present an empty schema.
	configSchema := ir.ObjectSchemaIR{Attributes: actionConfigAttributes(op)}
	if top := lookupTransformerOp(pathOps, path, method); top != nil {
		configSchema = transformer.ObjectSchemaFromOperationWithDiagnostics(*top, diags)
	}
	return ir.ActionIR{
		Name:            name,
		FullName:        providerName + "_" + name,
		TypeName:        providerName + "_" + name,
		Description:     actionDescription(op),
		SourceOperation: op.OperationID,
		// Actions have no result surface: the wired Invoke neither decodes a
		// response nor sets a result, so no response envelope is carried.
		InvokeMapping: operationMapping(method, path, op, ""),
		ConfigSchema:  configSchema,
	}
}

// operationDescription picks the most useful human-readable description for a
// construct (resource, data source, or action) inferred from an OpenAPI
// operation. Operations carry both a short summary and a verbose description;
// either is acceptable, but some specs (notably the Gigamon GigaVUE-FM bundle)
// accidentally copy a referenced schema component name into the operation
// description field (e.g. description "GigaAlarmBulkAcknowledgementSpec"
// alongside summary "Acknowledge Multiple Alarms"). A bare PascalCase
// identifier with no whitespace is not a description a practitioner can use, so
// such leaked titles are skipped in favor of the summary. The fallback chain
// is:
//
//  1. op.Description, when it reads as a sentence (contains whitespace)
//  2. op.Summary, when present
//  3. fallback (a construct-specific generated phrase, or "" for actions)
//
// This keeps real verbose descriptions, fills the many constructs whose only
// human-readable field is the summary, and never surfaces a leaked schema
// title as the description. The same chain is applied to resources, data
// sources, and actions so the generated schema MarkdownDescription and the
// docs front matter stay consistent and non-empty.
func operationDescription(op *parser.Operation, fallback string) string {
	if op == nil {
		return fallback
	}
	if desc := strings.TrimSpace(op.Description); desc != "" && strings.ContainsAny(desc, " \t\n") {
		return desc
	}
	if summary := strings.TrimSpace(op.Summary); summary != "" {
		return summary
	}
	return fallback
}

// deprecationMessage returns the standard deprecation message for a construct
// whose source OpenAPI operation declares deprecated: true. OpenAPI carries no
// message with the boolean flag, so a fixed honest message naming the construct
// kind is used (M-10). An empty kind yields the empty message (not deprecated).
func deprecationMessage(kind string, deprecated bool) string {
	if !deprecated {
		return ""
	}
	return fmt.Sprintf("This %s is deprecated.", kind)
}

// humanizeConstructName turns a snake_case construct name into a space-separated
// phrase for use in a generated description fallback (e.g. "alert_policy" ->
// "alert policy").
func humanizeConstructName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "resource"
	}
	return strings.ReplaceAll(name, "_", " ")
}

// actionDescription returns the description for an auto-inferred action. Actions
// have no natural noun phrase, so the empty-description fallback is "" (the
// generator omits an empty Description cleanly) rather than a generated
// sentence.
func actionDescription(op *parser.Operation) string {
	return operationDescription(op, "")
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
				Name:               transformer.SanitizeAttributeName(p.Name),
				WireName:           p.Name,
				Description:        p.Description,
				Required:           true,
				Schema:             ir.SchemaIR{Type: paramPrimitiveType(p.Schema)},
				Deprecated:         p.Deprecated,
				DeprecationMessage: deprecationMessage("parameter", p.Deprecated),
			})
		case "query", "header", "cookie":
			attrs = append(attrs, ir.AttributeIR{
				Name:               transformer.SanitizeAttributeName(p.Name),
				WireName:           p.Name,
				Description:        p.Description,
				Required:           p.Required,
				Schema:             ir.SchemaIR{Type: paramPrimitiveType(p.Schema)},
				Deprecated:         p.Deprecated,
				DeprecationMessage: deprecationMessage("parameter", p.Deprecated),
			})
		}
	}
	return attrs
}

func ephemeralFromOperation(spec *parser.Spec, op *parser.Operation, providerName, path, method string, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation, diags *diagnostics.Diagnostics) ir.EphemeralResourceIR {
	name := resourceName(op, method, path)
	er := ir.EphemeralResourceIR{
		Name:            name,
		FullName:        providerName + "_" + name,
		TypeName:        providerName + "_" + name,
		Description:     operationDescription(op, fmt.Sprintf("Opens the %s ephemeral resource.", humanizeConstructName(name))),
		SourceOperation: op.OperationID,
		OpenMapping:     operationMapping(method, path, op, envelopeOfTransformerOp(pathOps, path, method)),
	}
	// Build the config (input) schema from the open operation's path/query/header
	// parameters and the result (output) schema from its resolved response, so an
	// inferred ephemeral resource can wire its Open/Renew/Close bodies instead of
	// keeping an empty-schema scaffold (PROJECT_DESIGN §23). The transformer exposes
	// the same builders its own inferEphemeralResource uses.
	if top := lookupTransformerOp(pathOps, path, method); top != nil {
		er.ConfigSchema = transformer.ObjectSchemaFromOperationWithDiagnostics(*top, diags)
		er.ResultSchema = transformer.ResultSchemaFromResponseWithDiagnostics(top.ResponseSchema, diags)
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
// (the generator keeps the list resource honestly scaffolded — F1). The config
// (filter) schema is always built from the collection path's parameters, even
// when the response is not a bare array, so a required query or header
// parameter is not dropped from an enveloped collection.
func listResourceFromOperation(op *parser.Operation, providerName, path, method string, pathOps map[string]map[transformer.HTTPMethod]transformer.Operation, identityParams []string, diags *diagnostics.Diagnostics) ir.ListResourceIR {
	name := resourceName(op, method, path)
	lr := ir.ListResourceIR{
		Name:            name,
		FullName:        providerName + "_" + name,
		TypeName:        providerName + "_" + name,
		Description:     operationDescription(op, fmt.Sprintf("Lists %s resources.", humanizeConstructName(name))),
		SourceOperation: op.OperationID,
		ListMapping:     operationMapping(method, path, op, envelopeOfTransformerOp(pathOps, path, method)),
		// The collection endpoint this list was inferred from; pairs the list
		// with the managed resource from the same CRUD group so it can be
		// registered (pairListResourceRegistrations).
		CollectionPath: path,
	}

	top := lookupTransformerOp(pathOps, path, method)
	if top != nil {
		// ConfigSchema is the collection's input filters. It does not depend
		// on the response being a bare array; building it first keeps required
		// query/header/path parameters from vanishing when the response is an
		// enveloped collection such as {items: [...], context: {...}}.
		lr.ConfigSchema = transformer.ListResourceConfigSchema(*top, diags)
	}
	if top == nil || top.ResponseSchema == nil || !strings.EqualFold(top.ResponseSchema.Type, "array") {
		return lr
	}
	item := top.ResponseSchema.Items
	if item == nil || len(item.Properties) == 0 {
		return lr
	}
	rs := transformer.ObjectSchemaFromSpecWithDiagnostics(item, diags)
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
			name := transformer.ToSnakeCase(p)
			wire := matchItemProperty(p, item.Properties)
			// The matching item property's description documents what the
			// identity attribute identifies; dropping it left every identity
			// attribute blank even when the spec described it.
			desc := ""
			if wire != "" {
				desc = item.Properties[wire].Description
			}
			lr.IdentitySchema.Attributes = append(lr.IdentitySchema.Attributes, ir.AttributeIR{
				Name:        name,
				WireName:    wire,
				Description: desc,
				Computed:    true,
				Schema:      ir.SchemaIR{Type: propType(wire)},
			})
		}
	} else if _, ok := item.Properties["id"]; ok {
		lr.IdentitySchema.Attributes = append(lr.IdentitySchema.Attributes, ir.AttributeIR{
			Name:        "id",
			Description: item.Properties["id"].Description,
			Computed:    true,
			Schema:      ir.SchemaIR{Type: propType("id")},
		})
	}
	return lr
}

// pairListResourceRegistrations pairs each list resource with the managed
// resource it can be registered against, renaming it when necessary. The
// framework requires every registered ListResource type name to equal a
// managed resource type name, but list names are derived from the collection
// operation while managed resources are named from their CRUD group, so the
// two only coincide by accident. A list whose collection endpoint belongs to a
// surviving managed-resource CRUD group is renamed to that resource's type
// name, which makes it registerable and lets pairListResourceIdentities share
// the identity schema. A list that cannot pair — no managed resource for its
// collection path, the resource already claimed by another list, or an empty
// identity schema — stays unregistered and gets a fail-loud Warning: without
// it the dry-run would count constructs `terraform query` can never expose
// (AGENTS.md "fail loud, never silently"). The generator suppresses docs and
// examples for unregistered lists.
//
// Lists whose type name already matches a managed resource (e.g. a
// generator.yaml list_resource_override with resource: user) keep that
// pairing; the rename is only for inferred names.
func pairListResourceRegistrations(provider *ir.ProviderIR, diags *diagnostics.Diagnostics) {
	if len(provider.ListResources) == 0 {
		return
	}
	resourcesByCollection := make(map[string]int, len(provider.Resources))
	managedTypes := make(map[string]bool, len(provider.Resources))
	for i := range provider.Resources {
		res := &provider.Resources[i]
		managedTypes[res.TypeName] = true
		if res.CollectionPath != "" {
			if _, ok := resourcesByCollection[res.CollectionPath]; !ok {
				resourcesByCollection[res.CollectionPath] = i
			}
		}
	}
	// listNames guards renames against colliding with an existing list name —
	// two lists at one type name would fail checkDuplicateConstructNames.
	listNames := make(map[string]bool, len(provider.ListResources))
	for i := range provider.ListResources {
		listNames[provider.ListResources[i].Name] = true
	}
	// claimed tracks managed type names already spoken for, so a resource is
	// never registered against two lists.
	claimed := make(map[string]bool, len(provider.ListResources))

	// First pass: lists that already match a managed resource by type name keep
	// the pairing (preserves generator.yaml list overrides and specs whose
	// names happen to align, e.g. the collection operationId and the resource
	// name both reduce to "user").
	for i := range provider.ListResources {
		lr := &provider.ListResources[i]
		if managedTypes[lr.TypeName] && !claimed[lr.TypeName] {
			lr.Registerable = true
			claimed[lr.TypeName] = true
		}
	}

	// Second pass: rename unpaired lists to the managed resource inferred from
	// the same CRUD group. Lists are visited in their slice order, which the
	// per-operation pass builds in sorted path order, so the claim is
	// deterministic for byte-identical generation.
	for i := range provider.ListResources {
		lr := &provider.ListResources[i]
		if lr.Registerable {
			continue
		}
		idx, ok := resourcesByCollection[lr.CollectionPath]
		if ok && !claimed[provider.Resources[idx].TypeName] &&
			!listNames[provider.Resources[idx].Name] &&
			len(lr.IdentitySchema.Attributes) > 0 {
			res := &provider.Resources[idx]
			*diags = append(*diags, diagnostics.Diagnostic{
				Severity: diagnostics.Info,
				Summary:  fmt.Sprintf("list resource %q is paired with managed resource %q", lr.Name, res.Name),
				Detail: fmt.Sprintf(
					"The Terraform Plugin Framework only registers a list resource whose type name equals a "+
						"managed resource type name, so the list resource inferred from %s %s is renamed from %q to "+
						"%q to match the managed resource from the same CRUD group. terraform query exposes it as "+
						"%s.", lr.ListMapping.Method, lr.ListMapping.PathTemplate, lr.Name, res.Name, res.TypeName),
			})
			delete(listNames, lr.Name)
			listNames[res.Name] = true
			claimed[res.TypeName] = true
			lr.Name = res.Name
			lr.FullName = res.TypeName
			lr.TypeName = res.TypeName
			lr.Registerable = true
			continue
		}
		var reason string
		switch {
		case !ok:
			reason = "no managed resource was inferred from its collection path's CRUD group"
		case len(lr.IdentitySchema.Attributes) == 0:
			reason = "it has no identity attributes to pair with a managed resource"
		default:
			reason = "the paired managed resource is already claimed by another list resource"
		}
		*diags = append(*diags, diagnostics.Diagnostic{
			Severity: diagnostics.Warning,
			Summary:  fmt.Sprintf("list resource %q cannot be registered; terraform query will not expose it", lr.Name),
			Detail: fmt.Sprintf(
				"The Terraform Plugin Framework only registers a list resource whose type name equals a managed "+
					"resource type name, and %s. The list resource %s is still generated, but the provider does not "+
					"register it and its documentation is suppressed. Add the missing CRUD operations to the spec, "+
					"or declare the list via a generator.yaml list_resource_overrides entry whose resource names a "+
					"managed resource.", reason, lr.Name),
		})
	}
}

// pairListResourceIdentities copies each list resource's identity schema onto
// the managed resource that shares its type name. terraform query types the
// identities a list resource streams against the managed resource's identity
// schema (ResourceWithIdentity), so the two must be identical; a list resource
// whose type name has no matching managed resource is not registered by the
// generator's listRegistrationBody (G12), so it is skipped here too.
func pairListResourceIdentities(provider *ir.ProviderIR) {
	resourcesByType := make(map[string]int, len(provider.Resources))
	for i := range provider.Resources {
		resourcesByType[provider.Resources[i].TypeName] = i
	}
	for i := range provider.ListResources {
		lr := &provider.ListResources[i]
		if len(lr.IdentitySchema.Attributes) == 0 {
			continue
		}
		idx, ok := resourcesByType[lr.TypeName]
		if !ok {
			continue
		}
		identity := lr.IdentitySchema
		provider.Resources[idx].IdentitySchema = &identity
	}
}

// instancePathParams returns the templated ({param}) segments of an instance
// path, in their original OpenAPI casing, in order. They name the identity
// attributes of a list resource promoted from a CRUD group; the original
// casing is preserved so listResourceFromOperation can match each param to the
// paired item object's identifier property (e.g. path {shipSymbol} ↔ item
// property "symbol") and carry that wire name on the identity attribute.
func instancePathParams(path string) []string {
	var out []string
	for _, seg := range strings.Split(path, "/") {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			out = append(out, strings.TrimSuffix(strings.TrimPrefix(seg, "{"), "}"))
		}
	}
	return out
}

// matchItemProperty resolves an instance path parameter name to the item
// object property that holds its value, returning that property's original
// (wire) name. The path parameter and the item identifier commonly differ in
// name — e.g. SpaceTraders GET /my/ships/{shipSymbol} returns ships whose
// identifier property is "symbol", not "shipSymbol" — so the identity
// extraction must probe the item's actual JSON key, not the path parameter
// name. Matching is: exact name first, then the last camelCase word of the
// parameter compared case-insensitively to the item properties (shipSymbol →
// "symbol"). When no property matches, the parameter name itself is returned
// (a best-effort wire name; the list extraction still falls back to "id").
// The result is deterministic: when several properties match, the
// lexicographically smallest name wins.
func matchItemProperty(param string, props map[string]transformer.SchemaSpec) string {
	if _, ok := props[param]; ok {
		return param
	}
	parts := strings.Split(transformer.ToSnakeCase(param), "_")
	last := strings.ToLower(parts[len(parts)-1])
	var match string
	for name := range props {
		if strings.EqualFold(name, last) {
			if match == "" || name < match {
				match = name
			}
		}
	}
	if match != "" {
		return match
	}
	return param
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
		Name:     name,
		FullName: providerName + "_" + name,
		// Provider-defined functions are invoked as provider::<provider>::<name>,
		// so the framework-registered name is the bare function name. Prefixing
		// it with the provider name (unlike managed resources, whose type names
		// require the prefix) doubles the prefix in the invocation:
		// provider::gigavuecore::gigavuecore_query_raw_data(...) (§3.7).
		TypeName:        name,
		Description:     operationDescription(op, fmt.Sprintf("Provider-defined function %s.", humanizeConstructName(name))),
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
				attrs = append(attrs, ir.AttributeIR{Name: name, Schema: ir.SchemaIR{Type: t}, Description: prop.Description})
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
	if err := enableLocalReferences(sp, root, name, version); err != nil {
		return nil, version, append(diagnostics.Diagnostics(versionDiags), convertDiags...), fmt.Errorf("failed to initialize local $ref resolution: %w", err)
	}

	allDiags := append(diagnostics.Diagnostics(versionDiags), convertDiags...)
	// Run the same structural and document-aware $ref validation the HTTP
	// /validate endpoint applies. Without it, generation could silently drop a
	// dangling reference and emit an empty-schema resource instead of failing.
	allDiags = append(allDiags, parser.Validate(root, sp, version)...)
	preview, previewDiags := buildIRPreview(sp, version, cfg)
	allDiags = append(allDiags, previewDiags...)
	return preview, version, allDiags, nil
}

// GenerateStarterConfig parses an OpenAPI document, builds a ProviderIR from it,
// and returns a validated generator.yaml Config. If providerName is non-empty it
// overrides the provider name derived from the spec title. usePutAsCreate threads
// the PUT-as-create toggle into the IR build (true = default-on; false = the
// kill-switch) and is recorded on the returned Config so the generated
// generator.yaml is self-documenting and round-trips through `eidos generate`.
//
// The returned diagnostics are conversion warnings produced while normalizing
// the OpenAPI document; callers should surface them to the user even when the
// overall generation succeeds. Version detection diagnostics are returned as
// part of the error instead.
func GenerateStarterConfig(spec []byte, providerName string, usePutAsCreate bool) (*config.Config, parser.Version, diagnostics.Diagnostics, error) {
	return generateStarterConfig(spec, "", providerName, usePutAsCreate)
}

// GenerateStarterConfigWithName is GenerateStarterConfig with parse errors
// attributed to name (the spec's real path or URL) rather than the generic
// "request.yaml".
func GenerateStarterConfigWithName(spec []byte, name, providerName string, usePutAsCreate bool) (*config.Config, parser.Version, diagnostics.Diagnostics, error) {
	return generateStarterConfig(spec, name, providerName, usePutAsCreate)
}

// starterConfigToggle returns the config used to build the starter-config IR so
// it reflects the PUT-as-create toggle. It is nil for the default-on case so
// the IR build is byte-identical to a no-config `eidos generate`; for the
// kill-switch it carries only UsePutAsCreate=false (every other buildIRPreview
// cfg branch is nil-safe for an otherwise-empty config, so nothing else moves).
func starterConfigToggle(usePutAsCreate bool) *config.Config {
	if usePutAsCreate {
		return nil
	}
	v := false
	return &config.Config{UsePutAsCreate: &v}
}

func generateStarterConfig(spec []byte, name, providerName string, usePutAsCreate bool) (*config.Config, parser.Version, diagnostics.Diagnostics, error) {
	var (
		preview  *ir.ProviderIR
		version  parser.Version
		allDiags diagnostics.Diagnostics
		err      error
	)
	toggle := starterConfigToggle(usePutAsCreate)
	if name == "" {
		preview, version, allDiags, err = BuildProviderIR(spec, toggle)
	} else {
		preview, version, allDiags, err = BuildProviderIRWithName(spec, name, "", toggle)
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
	// Record the toggle on the returned config so the generated generator.yaml
	// carries use_put_as_create explicitly (self-documenting default-on) and
	// round-trips: feeding it back to `eidos generate --config` honors it.
	cfg.UsePutAsCreate = boolPtr(usePutAsCreate)
	// Record sign_release explicitly so the starter config documents the
	// default-on GPG signing as a visible, flippable knob (set false to opt
	// out) and round-trips through `eidos generate --config`.
	cfg.SignRelease = boolPtr(true)
	return cfg, version, allDiags, nil
}

// boolPtr returns a pointer to b. It mirrors the helper in pkg/generator so the
// *bool UsePutAsCreate field can be set without a local variable at each call.
func boolPtr(b bool) *bool {
	return &b
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
