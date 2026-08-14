package schema

import (
	"go/ast"
	"sort"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/ir"
	"github.com/signalbreak-labs/eidos/pkg/transformer"
)

// MergedDiscriminatedUnion returns the merged object schema for a discriminated
// union via the dynamic-union strategy, or nil when the schema is not a
// discriminated union or the variants cannot merge (duplicate attribute names
// across variants). Merged attributes are marked Computed and stripped of
// Required/Optional when the source schema was Computed.
func MergedDiscriminatedUnion(s ir.SchemaIR) *ir.SchemaIR {
	if s.Union == nil || s.Union.Discriminator == nil {
		return nil
	}
	merged, err := transformer.ApplyDynamicUnion(&s)
	if err != nil || !IsObjectLike(*merged) {
		return nil
	}
	if s.Computed {
		for i := range merged.Attributes {
			merged.Attributes[i].Computed = true
			merged.Attributes[i].Required = false
			merged.Attributes[i].Optional = false
		}
	}
	return merged
}

// DiscriminatedUnionValidators returns the Validators field element carrying
// the DiscriminatorValidator call for a discriminated union, attached through
// the "Object" validator path (validator.Object on the rendered
// SingleNestedAttribute). The discriminator property name is snake_cased to
// match the merged attribute's name.
func DiscriminatedUnionValidators(s ir.SchemaIR) ast.Expr {
	disc := s.Union.Discriminator
	keys := make([]string, 0, len(disc.Mapping))
	for k := range disc.Mapping {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	args := make([]ast.Expr, 0, 1+len(keys))
	args = append(args, astgen.Lit(transformer.ToSnakeCase(disc.PropertyName)))
	for _, k := range keys {
		args = append(args, astgen.Lit(k))
	}
	return astgen.KeyValueExpr(
		astgen.Ident("Validators"),
		astgen.CompositeLit(
			astgen.SliceType(astgen.QualExpr("validator", "Object")),
			astgen.Call(astgen.Ident("DiscriminatorValidator"), args...),
		),
	)
}

// DynamicUnionElement normalizes a collection element type that is a union:
// unions render as Dynamic elements because a typed collection element cannot
// switch on alternatives (nested unions stay Dynamic by design, D2).
func DynamicUnionElement(elem ir.SchemaIR) ir.SchemaIR {
	if elem.Union != nil {
		return ir.SchemaIR{Type: ir.TypeDynamic}
	}
	return elem
}

// ContainsNestedDynamic reports whether the schema, when rendered as a
// terraform-plugin-framework attribute, would produce a DynamicAttribute at any
// point nested under a collection element. The framework rejects any
// collection (List/Set/Map) whose element type contains a dynamic type at any
// depth — see fwtype.ContainsCollectionWithDynamic — so a collection whose
// element contains a nested dynamic must be emitted as a DynamicAttribute as a
// whole rather than as a typed collection with a dynamic leaf.
//
// The check is recursive through object attributes and nested blocks and
// collections. A discriminated union that renders as a SingleNestedAttribute
// via the dynamic-union strategy is recursed through its merged attributes; any
// other union renders as a DynamicAttribute and is therefore treated as
// dynamic. A primitive dynamic or null type is dynamic by definition.
func ContainsNestedDynamic(s ir.SchemaIR) bool {
	if s.Type == ir.TypeDynamic || s.Type == ir.TypeNull {
		return true
	}
	if s.Union != nil {
		if merged := MergedDiscriminatedUnion(s); merged != nil {
			for _, attr := range merged.Attributes {
				if ContainsNestedDynamic(attr.Schema) {
					return true
				}
			}
			return false
		}
		return true
	}
	if s.Collection != nil {
		return ContainsNestedDynamic(s.Collection.ElementType)
	}
	for _, attr := range s.Attributes {
		if ContainsNestedDynamic(attr.Schema) {
			return true
		}
	}
	for _, block := range s.Blocks {
		if objectSchemaContainsDynamic(block.Schema) {
			return true
		}
	}
	return false
}

// IsDynamicAttribute reports whether an attribute's IR schema will be emitted
// as a schema.DynamicAttribute rather than a typed framework attribute. This is
// true for a primitive dynamic/null type, and for a collection whose element is
// dynamic/null or contains a nested dynamic at any depth: the plugin-framework
// rejects a Dynamic element inside a typed collection
// (fwtype.ContainsCollectionWithDynamic), so such a collection degrades to a
// top-level DynamicAttribute carrying arbitrary JSON. The example/acceptance
// config writer uses this to emit a scalar placeholder (null / "example")
// instead of a collection literal — a list literal configured on a
// DynamicAttribute is parsed by the framework as a Tuple, whose concrete
// element types the response mapping (dynamicValueFromRaw -> inferTFTypes)
// cannot reliably reproduce, causing "wrong final value type: tuple required"
// at apply (G18, seen on GitLab protected_branch.allowed_to_merge and Grafana
// alert_rule.data). The emission rules mirrored here live in
// resourceCollectionAttributeExpr / resourcePrimitiveAttributeExpr; keep them in
// sync when the degradation policy changes.
func IsDynamicAttribute(s ir.SchemaIR) bool {
	if s.Type == ir.TypeDynamic || s.Type == ir.TypeNull {
		return true
	}
	if s.Collection != nil {
		elem := DynamicUnionElement(s.Collection.ElementType)
		if elem.Type == ir.TypeDynamic || elem.Type == ir.TypeNull || ContainsNestedDynamic(elem) {
			return true
		}
	}
	return false
}

// objectSchemaContainsDynamic is the ObjectSchemaIR recursion companion to
// ContainsNestedDynamic, walking a nested block's attributes and sub-blocks.
func objectSchemaContainsDynamic(s ir.ObjectSchemaIR) bool {
	for _, attr := range s.Attributes {
		if ContainsNestedDynamic(attr.Schema) {
			return true
		}
	}
	for _, block := range s.Blocks {
		if objectSchemaContainsDynamic(block.Schema) {
			return true
		}
	}
	return false
}

// ObjectSchemaHasDiscriminatedUnion reports whether any attribute or nested
// block attribute in the schema is a discriminated union that renders as a
// SingleNestedAttribute with a DiscriminatorValidator, so the generated file
// imports the schema/validator package exactly when it is referenced.
func ObjectSchemaHasDiscriminatedUnion(s ir.ObjectSchemaIR) bool {
	for _, attr := range s.Attributes {
		if schemaHasDiscriminatedUnion(attr.Schema) {
			return true
		}
	}
	for _, block := range s.Blocks {
		if ObjectSchemaHasDiscriminatedUnion(block.Schema) {
			return true
		}
	}
	return false
}

// schemaHasDiscriminatedUnion reports whether the schema (or any nested
// attribute it contains) is a discriminated union renderable via the
// dynamic-union strategy.
func schemaHasDiscriminatedUnion(s ir.SchemaIR) bool {
	if MergedDiscriminatedUnion(s) != nil {
		return true
	}
	if s.Collection != nil {
		return schemaHasDiscriminatedUnion(s.Collection.ElementType)
	}
	for _, attr := range s.Attributes {
		if schemaHasDiscriminatedUnion(attr.Schema) {
			return true
		}
	}
	return false
}
