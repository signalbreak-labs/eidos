// Package schema holds the pure schema-to-Go mapping cores of the generator:
// model struct emission, value-mapper emission, custom-validator emission,
// JSON/XML conversion templates, and discriminated-union handling. The
// generator package wraps these cores into File-producing entry points; the
// package docstrings and identifier names here must stay consistent with what
// the generator emits, since generated field names and nested type names are
// derived in this package.
package schema

import (
	"strconv"

	"github.com/signalbreak-labs/eidos/pkg/generator/internal/naming"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// SkipAttrForModel reports whether the attribute should be skipped in the model.
// It is a no-op hook today; attribute-level filtering is not wired, and
// all IR attributes are included in generated models.
func SkipAttrForModel(_ ir.AttributeIR) bool {
	return false
}

// IsPrimitiveSchema reports whether the schema describes a primitive value.
func IsPrimitiveSchema(s ir.SchemaIR) bool {
	return s.Type == ir.TypeString || s.Type == ir.TypeInt || s.Type == ir.TypeFloat || s.Type == ir.TypeBool || s.Type == ir.TypeDynamic
}

// IsObjectLike reports whether the schema describes an object with attributes
// or blocks.
func IsObjectLike(s ir.SchemaIR) bool {
	return len(s.Attributes) > 0 || len(s.Blocks) > 0
}

// ResolveFieldNames returns the map from IR attribute name to unique Go field
// name for the given attribute slice, disambiguating colliding names by
// appending a numeric suffix ("foo_bar", "fooBar", "foo_bar_2" → "FooBar",
// "FooBar2", "FooBar3"). The result is deterministic and identical for any
// caller that iterates the same attribute slice in the same order.
//
// This addresses H-7: properties that differ only in separator style (for
// example "foo_bar" and "fooBar") both normalize to "FooBar" and would produce
// duplicate struct fields, duplicate nested types, and duplicate
// FooBarType/FromValue/ToValue functions. ResolveFieldNames keeps them
// distinct while leaving non-colliding names unchanged. Both the generated
// plain API model and value_mappers.go call this for the same attribute scope,
// so the resolved field and nested-type names stay consistent across the two
// generated files.
func ResolveFieldNames(attrs []ir.AttributeIR) map[string]string {
	resolved := make(map[string]string, len(attrs))
	taken := make(map[string]struct{}, len(attrs))
	baseCount := make(map[string]int)

	assign := func(base string) string {
		baseCount[base]++
		name := base
		if baseCount[base] > 1 {
			name = base + strconv.Itoa(baseCount[base])
		}
		// If the chosen name was already taken (for example a real attribute
		// named "foo_bar_2" already claimed "FooBar2"), keep incrementing.
		for {
			if _, dup := taken[name]; !dup {
				break
			}
			baseCount[base]++
			name = base + strconv.Itoa(baseCount[base])
		}
		taken[name] = struct{}{}
		return name
	}

	for _, attr := range attrs {
		if SkipAttrForModel(attr) {
			continue
		}
		resolved[attr.Name] = assign(naming.GoFieldName(attr.Name))
	}
	return resolved
}

// resolvedFieldName returns the unique Go field name for attr within the given
// resolved-names scope. It falls back to the unsuffixed GoFieldName when the
// attribute is absent from the scope (for example, a block's nested schema
// resolved independently), which is safe because such lookups never share a scope
// with a colliding sibling.
func resolvedFieldName(scope map[string]string, attr ir.AttributeIR) string {
	if name, ok := scope[attr.Name]; ok {
		return name
	}
	return naming.GoFieldName(attr.Name)
}
