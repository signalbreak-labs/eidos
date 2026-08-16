package transformer

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// ActionIR represents a Terraform Plugin Framework invoke action inferred from
// an OpenAPI operation that does not fit a managed-resource CRUD lifecycle.
type ActionIR struct {
	Name             string            `json:"name"`
	FullName         string            `json:"full_name"`
	TypeName         string            `json:"type_name"`
	Description      string            `json:"description,omitempty"`
	ConfigSchema     ir.ObjectSchemaIR `json:"config_schema"`
	InvokeMapping    Operation         `json:"invoke_mapping"`
	ModifyPlan       bool              `json:"modify_plan,omitempty"`
	ProgressMessages bool              `json:"progress_messages,omitempty"`
	Tags             []string          `json:"tags,omitempty"`
	SourceOperation  string            `json:"source_operation,omitempty"`
}

// InferActions identifies OpenAPI operations that should be exposed as Terraform
// invoke actions. A POST operation becomes an action unless it is the Create
// operation of a managed resource, which is detected by the presence of an
// instance subpath (e.g., POST /pets is not an action because /pets/{petId}
// exists). The returned actions are sorted deterministically by name.
func InferActions(pathOps map[string]map[HTTPMethod]Operation) []ActionIR {
	allPaths := make(map[string]struct{}, len(pathOps))
	for path := range pathOps {
		allPaths[path] = struct{}{}
	}

	var actions []ActionIR
	for _, path := range sortedKeys(pathOps) {
		ops := pathOps[path]
		for _, op := range ops {
			if op.Method != MethodPost {
				continue
			}
			if isCRUDCreate(path, allPaths) {
				continue
			}
			actions = append(actions, inferAction(op))
		}
	}

	sort.SliceStable(actions, func(i, j int) bool {
		return actions[i].Name < actions[j].Name
	})
	actions = dedupByName(actions, func(a ActionIR) string { return a.Name })
	return actions
}

// IsCRUDCreatePath reports whether a POST on path is a managed-resource Create
// operation: another path in pathOps extends it with a templated (instance)
// segment, e.g., /pets/{petId} for /pets. It exposes isCRUDCreate so the API
// preview classification (pkg/api) classifies POST operations consistently
// with InferActions instead of running a parallel heuristic.
func IsCRUDCreatePath(path string, pathOps map[string]map[HTTPMethod]Operation) bool {
	allPaths := make(map[string]struct{}, len(pathOps))
	for p := range pathOps {
		allPaths[p] = struct{}{}
	}
	return isCRUDCreate(path, allPaths)
}

// isCRUDCreate reports whether a POST on path is a managed-resource Create
// operation. A path is considered a collection create if there is another path
// that extends it with a templated (instance) segment, e.g., /pets/{petId} for
// /pets.
func isCRUDCreate(path string, allPaths map[string]struct{}) bool {
	pSegs := parsePath(path)
	for q := range allPaths {
		if q == path {
			continue
		}
		qSegs := parsePath(q)
		if len(qSegs) <= len(pSegs) {
			continue
		}
		prefixMatch := true
		for i, s := range pSegs {
			if qSegs[i] != s {
				prefixMatch = false
				break
			}
		}
		if !prefixMatch {
			continue
		}
		if qSegs[len(pSegs)].IsParam {
			return true
		}
	}
	return false
}

func inferAction(op Operation) ActionIR {
	name := actionName(op)
	return ActionIR{
		Name:            name,
		FullName:        toHumanName(name),
		TypeName:        name,
		Description:     op.OperationID,
		ConfigSchema:    ObjectSchemaFromOperation(op),
		InvokeMapping:   op,
		SourceOperation: op.OperationID,
	}
}

func actionName(op Operation) string {
	if op.OperationID != "" {
		return ToSnakeCase(op.OperationID)
	}
	segs := parsePath(op.Path)
	for i := len(segs) - 1; i >= 0; i-- {
		if !segs[i].IsParam {
			return segs[i].Value
		}
	}
	return "action"
}

func toHumanName(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		runes := []rune(p)
		runes[0] = unicode.ToUpper(runes[0])
		parts[i] = string(runes)
	}
	return strings.Join(parts, " ")
}

// ObjectSchemaFromOperation builds the input (config) schema for an action or
// ephemeral resource from an operation's parameters and request-body schema:
// each parameter becomes a Required (path or required param) or Optional
// attribute, and request-body properties become Optional write inputs.
// Duplicate normalized names (e.g. "fooBar" and "foo_bar") are deduplicated
// first-wins, parameters taking precedence. Diagnostics are discarded; callers
// that need fail-loud collision warnings (the generate pipeline) use
// ObjectSchemaFromOperationWithDiagnostics instead.
func ObjectSchemaFromOperation(op Operation) ir.ObjectSchemaIR {
	return ObjectSchemaFromOperationWithDiagnostics(op, nil)
}

// ObjectSchemaFromOperationWithDiagnostics is ObjectSchemaFromOperation that
// appends fail-loud diagnostics to diags (a nil diags is allowed and simply
// suppresses emission). The only diagnostic it emits today is the path/body
// name collision described below.
//
// A request-body property whose sanitized Terraform name collides with a
// path-parameter's sanitized name represents a DISTINCT API field, not a
// duplicate: the path parameter identifies the resource acted on while the
// body property is a separate request input. The canonical case is
// SpaceTraders transfer-cargo, whose path /my/ships/{shipSymbol}/transfer names
// the source ship while the request body's required "shipSymbol" names the
// target ship. First-wins dedup would silently drop the body property, making
// the action unusable (the API rejects the missing required field) and
// violating fail-loud. Instead, the colliding body attribute is disambiguated
// with a "body_" prefix so both remain configurable; its WireName keeps the
// original property name so the request body key is correct. A Warning is
// emitted so the disambiguation is never silent. Path parameters deliberately
// carry no WireName here: they are substituted into the URL path, and emitting
// them into the request body under their wire name would collide with (and, by
// map-key overwrite, clobber) the body's same-named field.
func ObjectSchemaFromOperationWithDiagnostics(op Operation, diags *diagnostics.Diagnostics) ir.ObjectSchemaIR {
	if len(op.Parameters) == 0 && op.RequestSchema == nil {
		return ir.ObjectSchemaIR{}
	}

	// Deduplicate by normalized attribute name so parameters and request-body
	// properties whose names collide after ToSnakeCase (e.g. "fooBar" and
	// "foo_bar") do not produce duplicate attributes. Parameters are appended
	// first and win on collision; the surviving set is then sorted by name
	// (L-100).
	var attrs []ir.AttributeIR
	seen := make(map[string]struct{})
	pathNames := make(map[string]struct{})
	add := func(a ir.AttributeIR) {
		if _, dup := seen[a.Name]; dup {
			return
		}
		seen[a.Name] = struct{}{}
		attrs = append(attrs, a)
	}
	for _, p := range op.Parameters {
		name := SanitizeAttributeName(p.Name)
		pathNames[name] = struct{}{}
		schema := ir.SchemaIR{Type: mapParamType(p.Type)}
		add(ir.AttributeIR{
			Name:     name,
			Schema:   schema,
			Required: p.Required,
			Optional: !p.Required,
		})
	}

	if op.RequestSchema != nil {
		for _, a := range requestBodyAttributes(*op.RequestSchema) {
			if _, isPath := pathNames[a.Name]; isPath {
				// Path/body name collision: disambiguate the body attribute so it
				// is not dropped, preserving its WireName for the request body
				// key. See the function doc comment for the rationale.
				orig := a.Name
				a.Name = "body_" + orig
				for i := 2; ; i++ {
					if _, dup := seen[a.Name]; !dup {
						break
					}
					a.Name = fmt.Sprintf("body_%s_%d", orig, i)
				}
				if diags != nil {
					*diags = append(*diags, diagnostics.Diagnostic{
						Severity: diagnostics.Warning,
						Summary:  "request-body property collides with a path parameter name",
						Detail: fmt.Sprintf(
							"operation %q has a path parameter and a request-body property that both normalize to %q. "+
								"They are distinct API fields, so the body attribute is disambiguated as %q "+
								"(wire name %q preserved for the request body). Set both in the action config: "+
								"the bare attribute for the path parameter and the body_-prefixed attribute for the request body.",
							op.OperationID, orig, a.Name, a.WireName),
					})
				}
			}
			add(a)
		}
	}

	sort.Slice(attrs, func(i, j int) bool {
		return attrs[i].Name < attrs[j].Name
	})
	return ir.ObjectSchemaIR{Attributes: attrs}
}

func requestBodyAttributes(spec SchemaSpec) []ir.AttributeIR {
	spec.Type = strings.ToLower(strings.TrimSpace(spec.Type))
	switch spec.Type {
	case "object":
		required := make(map[string]bool, len(spec.Required))
		for _, name := range spec.Required {
			required[name] = true
		}
		attrs := make([]ir.AttributeIR, 0, len(spec.Properties))
		for name, prop := range spec.Properties {
			// WireName carries the original OpenAPI property name (commonly
			// camelCase, e.g. "waypointSymbol") so the generated model field
			// gets a `json:"waypointSymbol"` tag and modelToJSONMap emits the
			// API's wire name as the request-body key. Without it the body key
			// falls back to the snake_case Terraform attribute name and
			// multi-word fields are reported "undefined"/Required by the API
			// (422). Single-word names where snake_case == wire name are
			// unaffected, which is why symbol/units/produce happened to work.
			attrs = append(attrs, ir.AttributeIR{
				Name:     SanitizeAttributeName(name),
				WireName: name,
				Schema:   schemaIRFromSpec(prop),
				Required: required[name],
				Optional: !required[name],
			})
		}
		sort.Slice(attrs, func(i, j int) bool {
			return attrs[i].Name < attrs[j].Name
		})
		return attrs
	default:
		// A non-object request body is represented as a single `body` attribute.
		// The framework requires at least one mode flag per attribute, so mark it
		// Required (a declared request body is expected to be sent) — otherwise the
		// generated schema is rejected at runtime (M-37).
		return []ir.AttributeIR{{
			Name:     "body",
			Schema:   schemaIRFromSpec(spec),
			Required: true,
			Optional: false,
		}}
	}
}

func schemaIRFromSpec(spec SchemaSpec) ir.SchemaIR {
	switch strings.ToLower(strings.TrimSpace(spec.Type)) {
	case "string":
		return ir.SchemaIR{Type: ir.TypeString, Format: spec.Format}
	case "integer":
		return ir.SchemaIR{Type: ir.TypeInt}
	case "number":
		return ir.SchemaIR{Type: ir.TypeFloat}
	case "boolean":
		return ir.SchemaIR{Type: ir.TypeBool}
	case "array":
		// Map an array body/property to a collection attribute, honoring
		// uniqueItems as a Set. Elements are mapped with the same shallow mapper
		// (not schemaIRFromSpecRecursive) so writable request-body elements do not
		// inherit the Computed flag the recursive mapper applies to nested
		// response attributes.
		if spec.Items == nil {
			return ir.SchemaIR{Type: ir.TypeDynamic}
		}
		kind := ir.List
		if spec.UniqueItems {
			kind = ir.Set
		}
		return ir.SchemaIR{Collection: &ir.CollectionType{Kind: kind, ElementType: schemaIRFromSpec(*spec.Items)}}
	default:
		return ir.SchemaIR{Type: ir.TypeDynamic}
	}
}

func mapParamType(t string) ir.PrimitiveType {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "integer":
		return ir.TypeInt
	case "number":
		return ir.TypeFloat
	case "boolean":
		return ir.TypeBool
	case "string":
		return ir.TypeString
	default:
		return ir.TypeString
	}
}
