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
