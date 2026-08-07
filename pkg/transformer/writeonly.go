package transformer

import (
	"fmt"
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// ApplyWriteOnlyAttributes processes the attributes of an object schema, marking
// write-only request properties as WriteOnly + Sensitive and ensuring each has
// a companion _wo_version Int64 attribute.
//
// Per PROJECT_DESIGN.md Section 8.10, a write-only argument follows a versioned
// pattern:
//   - The attribute is renamed with a "_wo" suffix (e.g. "password" -> "password_wo").
//   - The attribute has WriteOnly and Sensitive set to true.
//   - A companion "<name>_wo_version" Int64 attribute tracks the version of the
//     write-only value and is used to signal Terraform to send a new value.
//
// The transformation is applied recursively to nested object schemas and blocks.
// Diagnostics emitted by the recursive walk (e.g. naming-convention conflicts)
// are discarded; use ApplyWriteOnlyAttributesWithDiagnostics to collect them.
func ApplyWriteOnlyAttributes(obj *ir.ObjectSchemaIR) {
	ApplyWriteOnlyAttributesWithDiagnostics(obj, nil)
}

// ApplyWriteOnlyAttributesWithDiagnostics is like ApplyWriteOnlyAttributes but
// appends warnings to diags when the documented naming convention cannot be
// satisfied (L-112), e.g. when a write-only "password" cannot be renamed to
// "password_wo" because that name is already in use.
func ApplyWriteOnlyAttributesWithDiagnostics(obj *ir.ObjectSchemaIR, diags *diagnostics.Diagnostics) {
	if obj == nil {
		return
	}

	seen := make(map[string]struct{}, len(obj.Attributes))
	for _, attr := range obj.Attributes {
		seen[attr.Name] = struct{}{}
	}

	var result []ir.AttributeIR
	for i := range obj.Attributes {
		attr := &obj.Attributes[i]

		if isWriteOnly(attr) {
			// Ensure both the attribute and its schema carry the write-only flags.
			attr.WriteOnly = true
			attr.Schema.WriteOnly = true
			attr.Sensitive = true
			attr.Schema.Sensitive = true

			// The framework forbids WriteOnly together with Computed (a write-only
			// argument is user-supplied, never provider-computed). Drop Computed so
			// the generated schema validates at provider startup (M-46).
			attr.Computed = false

			// Rename to the _wo suffix unless already present.
			if !strings.HasSuffix(attr.Name, "_wo") {
				woName := attr.Name + "_wo"
				if _, conflict := seen[woName]; !conflict {
					attr.Name = woName
					seen[woName] = struct{}{}
				} else if diags != nil {
					// L-112: the _wo name is already taken, so the attribute keeps
					// its original name (still WriteOnly) and its companion becomes
					// "<name>_version" instead of "<name>_wo_version". Surface a
					// warning so the naming-convention violation is not silent.
					*diags = diags.Append(diagnostics.Diagnostic{
						Severity: diagnostics.Warning,
						Summary:  "Write-only attribute cannot be renamed to _wo suffix",
						Detail: fmt.Sprintf(
							"write-only attribute %q could not be renamed to %q because the name is already in use; "+
								"the companion version attribute will be %q instead of %q",
							attr.Name, woName, attr.Name+"_version", woName+"_version"),
					})
				}
			}
		}

		result = append(result, *attr)

		if !attr.WriteOnly {
			continue
		}

		companionName := attr.Name + "_version"
		if _, exists := seen[companionName]; exists {
			continue
		}
		seen[companionName] = struct{}{}

		result = append(result, ir.AttributeIR{
			Name:        companionName,
			Description: fmt.Sprintf("Increment this value when %s changes.", attr.Name),
			Optional:    true,
			Schema:      ir.SchemaIR{Type: ir.TypeInt},
		})
	}

	obj.Attributes = result

	// Recurse into nested object schemas and blocks.
	for i := range obj.Attributes {
		applyWriteOnlyRecursive(&obj.Attributes[i].Schema, diags)
	}
	for i := range obj.Blocks {
		ApplyWriteOnlyAttributesWithDiagnostics(&obj.Blocks[i].Schema, diags)
	}
}

// isWriteOnly reports whether an attribute represents a write-only request
// property. It checks both the attribute-level flag and the embedded schema flag
// so that it works whether the flag was set on the attribute or its schema.
func isWriteOnly(attr *ir.AttributeIR) bool {
	if attr == nil {
		return false
	}
	return attr.WriteOnly || attr.Schema.WriteOnly
}

// applyWriteOnlyRecursive applies write-only processing to nested schema nodes
// reachable from schema: object schemas, collection element types, and union
// variants.
func applyWriteOnlyRecursive(schema *ir.SchemaIR, diags *diagnostics.Diagnostics) {
	if schema == nil {
		return
	}

	if len(schema.Attributes) > 0 || len(schema.Blocks) > 0 {
		obj := ir.ObjectSchemaIR{
			Attributes:        schema.Attributes,
			Blocks:            schema.Blocks,
			DependentRequired: schema.DependentRequired,
		}
		ApplyWriteOnlyAttributesWithDiagnostics(&obj, diags)
		schema.Attributes = obj.Attributes
		schema.Blocks = obj.Blocks
	}

	if schema.Collection != nil {
		applyWriteOnlyRecursive(&schema.Collection.ElementType, diags)
	}

	if schema.Union != nil {
		for i := range schema.Union.Variants {
			applyWriteOnlyRecursive(&schema.Union.Variants[i], diags)
		}
	}

	if schema.Not != nil {
		applyWriteOnlyRecursive(schema.Not, diags)
	}
	if schema.IfSchema != nil {
		applyWriteOnlyRecursive(schema.IfSchema, diags)
	}
	if schema.ThenSchema != nil {
		applyWriteOnlyRecursive(schema.ThenSchema, diags)
	}
	if schema.ElseSchema != nil {
		applyWriteOnlyRecursive(schema.ElseSchema, diags)
	}
	for _, dep := range schema.DependentSchemas {
		applyWriteOnlyRecursive(dep, diags)
	}
	for _, pp := range schema.PatternProperties {
		applyWriteOnlyRecursive(pp, diags)
	}
	if schema.PropertyNames != nil {
		applyWriteOnlyRecursive(schema.PropertyNames, diags)
	}
	if schema.UnevaluatedProperties != nil {
		applyWriteOnlyRecursive(schema.UnevaluatedProperties, diags)
	}
}
