package transformer

import (
	"sort"
	"strings"
	"unicode"

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
// first-wins, parameters taking precedence.
func ObjectSchemaFromOperation(op Operation) ir.ObjectSchemaIR {
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
	add := func(a ir.AttributeIR) {
		if _, dup := seen[a.Name]; dup {
			return
		}
		seen[a.Name] = struct{}{}
		attrs = append(attrs, a)
	}
	for _, p := range op.Parameters {
		schema := ir.SchemaIR{Type: mapParamType(p.Type)}
		add(ir.AttributeIR{
			Name:     SanitizeAttributeName(p.Name),
			Schema:   schema,
			Required: p.Required,
			Optional: !p.Required,
		})
	}

	if op.RequestSchema != nil {
		for _, a := range requestBodyAttributes(*op.RequestSchema) {
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
			attrs = append(attrs, ir.AttributeIR{
				Name:     SanitizeAttributeName(name),
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
