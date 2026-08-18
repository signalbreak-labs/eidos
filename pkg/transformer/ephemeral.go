package transformer

import (
	"sort"
	"strings"
	"unicode"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// EphemeralResourceIR represents a Terraform Plugin Framework ephemeral
// resource inferred from an OpenAPI operation that returns temporary, sensitive,
// or short-lived data such as credentials or tokens.
type EphemeralResourceIR struct {
	Name            string            `json:"name"`
	FullName        string            `json:"full_name"`
	TypeName        string            `json:"type_name"`
	Description     string            `json:"description,omitempty"`
	ConfigSchema    ir.ObjectSchemaIR `json:"config_schema"`
	ResultSchema    ir.ObjectSchemaIR `json:"result_schema"`
	OpenMapping     Operation         `json:"open_mapping"`
	RenewMapping    *Operation        `json:"renew_mapping,omitempty"`
	CloseMapping    *Operation        `json:"close_mapping,omitempty"`
	HasRenew        bool              `json:"has_renew,omitempty"`
	HasClose        bool              `json:"has_close,omitempty"`
	Tags            []string          `json:"tags,omitempty"`
	SourceOperation string            `json:"source_operation,omitempty"`
}

// InferEphemeralResources identifies OpenAPI operations that should be exposed
// as Terraform ephemeral resources. Candidates are operations whose response
// contains writeOnly data, whose response uses the password format, or whose
// path/operationId strongly suggests a credential or token endpoint. The
// returned resources are sorted deterministically by name.
//
// To avoid duplicating other inferred constructs (M-38):
//   - A GET is never an ephemeral open. A GET returning data is a data source
//     (collection) or a managed-resource Read (instance), not an ephemeral
//     resource, which is opened by a mutating operation.
//   - A POST that is a managed-resource Create (the collection has a paired
//     instance subpath) is skipped, mirroring InferActions, so it is not
//     emitted as both a managed resource and an ephemeral resource.
//
// Deprecated: unreachable from production. The live pipeline classifies
// operations in pkg/api (classifyOperation) and builds ephemeral IR there;
// this function is retained only for its test coverage and must not be
// extended (M-7). See AUDIT.md.
func InferEphemeralResources(pathOps map[string]map[HTTPMethod]Operation) []EphemeralResourceIR {
	allPaths := make(map[string]struct{}, len(pathOps))
	for path := range pathOps {
		allPaths[path] = struct{}{}
	}

	var resources []EphemeralResourceIR
	for _, path := range sortedKeys(pathOps) {
		ops := pathOps[path]
		for _, op := range ops {
			if op.Method == MethodGet {
				continue
			}
			if op.Method == MethodPost && isCRUDCreate(path, allPaths) {
				continue
			}
			if isInstancePath(path) {
				// Mutating operations on an instance path (PUT/PATCH/DELETE) are a
				// managed-resource Update/Delete, not an ephemeral open. The open is
				// a collection/singleton-level operation (M-38).
				continue
			}
			if !isEphemeralCandidate(op, path) {
				continue
			}
			resources = append(resources, inferEphemeralResource(op, path, pathOps))
		}
	}

	sort.SliceStable(resources, func(i, j int) bool {
		return resources[i].Name < resources[j].Name
	})
	resources = dedupByName(resources, func(e EphemeralResourceIR) string { return e.Name }, nil)
	return resources
}

var lifecycleSuffixes = []string{"renew", "close", "revoke", "refresh", "rotate"}

func isEphemeralCandidate(op Operation, path string) bool {
	if isLifecycleSubpath(path) {
		return false
	}

	if op.ResponseBody && op.ResponseSchema != nil {
		if schemaContainsWriteOnly(op.ResponseSchema) {
			return true
		}
		if schemaHasFormat(op.ResponseSchema, "password") {
			return true
		}
	}

	cues := []string{"token", "credential", "credentials", "session", "password", "secret"}
	for _, cue := range cues {
		if cueMatches(path, cue) || cueMatches(op.OperationID, cue) {
			return true
		}
	}
	return false
}

// cueMatches reports whether any identifier token in s matches the cue. Tokens
// are split on non-alphanumeric boundaries and camelCase (lower→upper)
// transitions, and a simple plural (cue+"s") is also accepted, so "token"
// matches "createToken" and "/tokens" but not "/tokenizers", and "password"
// matches "/passwords" but not "/passwordless-policy" (L-94).
func cueMatches(s, cue string) bool {
	for _, tok := range identTokens(s) {
		if tok == cue || tok == cue+"s" {
			return true
		}
	}
	return false
}

// identTokens splits s into lowercased identifier tokens on non-alphanumeric
// boundaries and camelCase (lower→upper) transitions: "createToken" yields
// ["create", "token"], "passwordless-policy" yields ["passwordless", "policy"].
func identTokens(s string) []string {
	var tokens []string
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			if b.Len() > 0 {
				tokens = append(tokens, strings.ToLower(b.String()))
				b.Reset()
			}
			continue
		}
		if i > 0 && unicode.IsLower(runes[i-1]) && unicode.IsUpper(r) {
			if b.Len() > 0 {
				tokens = append(tokens, strings.ToLower(b.String()))
				b.Reset()
			}
		}
		b.WriteRune(r)
	}
	if b.Len() > 0 {
		tokens = append(tokens, strings.ToLower(b.String()))
	}
	return tokens
}

// IsLifecycleSubpath reports whether path ends in an ephemeral lifecycle
// segment (renew/close/revoke/refresh/rotate). It exposes isLifecycleSubpath so
// the API preview classification (pkg/api) does not emit a sibling lifecycle
// operation as its own ephemeral resource.
func IsLifecycleSubpath(path string) bool {
	return isLifecycleSubpath(path)
}

func isLifecycleSubpath(path string) bool {
	segs := parsePath(path)
	if len(segs) == 0 {
		return false
	}
	last := strings.ToLower(segs[len(segs)-1].Value)
	for _, suffix := range lifecycleSuffixes {
		if last == suffix {
			return true
		}
	}
	return false
}

// isInstancePath reports whether path ends in a templated (param) segment, e.g.
// /pets/{petId}. Such paths address a single managed-resource instance; their
// mutating operations are CRUD Update/Delete, not ephemeral opens.
func isInstancePath(path string) bool {
	segs := parsePath(path)
	if len(segs) == 0 {
		return false
	}
	return segs[len(segs)-1].IsParam
}

func schemaContainsWriteOnly(spec *SchemaSpec) bool {
	return schemaContainsWriteOnlyDepth(spec, 0)
}

func schemaContainsWriteOnlyDepth(spec *SchemaSpec, depth int) bool {
	if spec == nil {
		return false
	}
	if spec.WriteOnly {
		return true
	}
	// Depth backstop against unbounded recursion on a deeply-nested or cyclic
	// schema spec (M-41). The parser marks $ref cycles as Opaque, which
	// schemaSpecFromParser honors by stopping descent, so cycles do not reach
	// here; this cap guards the remaining deep-nesting path.
	if depth >= maxSchemaDepth {
		return false
	}
	for _, prop := range spec.Properties {
		if schemaContainsWriteOnlyDepth(&prop, depth+1) {
			return true
		}
	}
	if spec.Items != nil && schemaContainsWriteOnlyDepth(spec.Items, depth+1) {
		return true
	}
	if spec.AdditionalProperties != nil && schemaContainsWriteOnlyDepth(spec.AdditionalProperties, depth+1) {
		return true
	}
	return false
}

func schemaHasFormat(spec *SchemaSpec, format string) bool {
	return schemaHasFormatDepth(spec, format, 0)
}

func schemaHasFormatDepth(spec *SchemaSpec, format string, depth int) bool {
	if spec == nil {
		return false
	}
	if strings.EqualFold(spec.Format, format) {
		return true
	}
	if depth >= maxSchemaDepth {
		return false
	}
	for _, prop := range spec.Properties {
		if schemaHasFormatDepth(&prop, format, depth+1) {
			return true
		}
	}
	if spec.Items != nil && schemaHasFormatDepth(spec.Items, format, depth+1) {
		return true
	}
	if spec.AdditionalProperties != nil && schemaHasFormatDepth(spec.AdditionalProperties, format, depth+1) {
		return true
	}
	return false
}

func inferEphemeralResource(op Operation, path string, pathOps map[string]map[HTTPMethod]Operation) EphemeralResourceIR {
	name := ephemeralName(op, path)
	er := EphemeralResourceIR{
		Name:            name,
		FullName:        toHumanName(name),
		TypeName:        name,
		Description:     op.OperationID,
		ConfigSchema:    ObjectSchemaFromOperation(op),
		ResultSchema:    ResultSchemaFromResponse(op.ResponseSchema),
		OpenMapping:     op,
		SourceOperation: op.OperationID,
	}

	if renew, ok := pathOps[path+"/renew"][MethodPost]; ok {
		er.RenewMapping = &renew
		er.HasRenew = true
	}
	if closeOps, ok := pathOps[path+"/close"]; ok {
		if del, ok2 := closeOps[MethodDelete]; ok2 {
			er.CloseMapping = &del
			er.HasClose = true
		}
	}
	if !er.HasClose {
		if revokeOps, ok := pathOps[path+"/revoke"]; ok {
			if del, ok2 := revokeOps[MethodDelete]; ok2 {
				er.CloseMapping = &del
				er.HasClose = true
			}
		}
	}

	return er
}

func ephemeralName(op Operation, path string) string {
	if op.OperationID != "" {
		return ToSnakeCase(op.OperationID)
	}
	segs := parsePath(path)
	for i := len(segs) - 1; i >= 0; i-- {
		if !segs[i].IsParam {
			return segs[i].Value
		}
	}
	return "ephemeral"
}

// ResultSchemaFromResponse builds the computed (output) schema for an
// ephemeral resource from the Open operation's response schema. An object
// response contributes one Computed attribute per property (write-only or
// password-format properties are Sensitive); a non-object response contributes
// a single Computed `result` attribute.
func ResultSchemaFromResponse(spec *SchemaSpec) ir.ObjectSchemaIR {
	if spec == nil {
		return ir.ObjectSchemaIR{}
	}
	switch strings.ToLower(strings.TrimSpace(spec.Type)) {
	case "object":
		var attrs []ir.AttributeIR
		for name, prop := range spec.Properties {
			schema := schemaIRFromSpec(prop)
			if prop.WriteOnly || strings.EqualFold(prop.Format, "password") {
				schema.Sensitive = true
			}
			attrs = append(attrs, ir.AttributeIR{
				Name:      SanitizeAttributeName(name),
				Schema:    schema,
				Computed:  true,
				Sensitive: schema.Sensitive,
			})
		}
		sort.Slice(attrs, func(i, j int) bool {
			return attrs[i].Name < attrs[j].Name
		})
		return ir.ObjectSchemaIR{Attributes: attrs}
	default:
		return ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{{
				Name:     "result",
				Schema:   schemaIRFromSpec(*spec),
				Computed: true,
			}},
		}
	}
}
