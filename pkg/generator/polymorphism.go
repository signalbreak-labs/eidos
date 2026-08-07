package generator

import (
	"go/ast"
	"sort"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/ir"
	"github.com/signalbreak-labs/eidos/pkg/transformer"
)

// This file holds the shared generation support for polymorphic (oneOf/anyOf)
// union attributes (Workstream D). A union without a discriminator renders as
// a DynamicAttribute (the framework has no first-class union attribute); a
// discriminated union renders via the dynamic-union strategy as a
// SingleNestedAttribute merging every variant's fields plus the discriminator
// attribute, with a DiscriminatorValidator attached through the "Object"
// validator path. The merged shape comes from
// transformer.ApplyDynamicUnion; when the merge fails (e.g. duplicate
// attribute names across variants) the attribute falls back to
// DynamicAttribute.

// mergedDiscriminatedUnion returns the merged object schema for a
// discriminated union via the dynamic-union strategy, or nil when the schema
// is not a discriminated union or the variants cannot merge (duplicate
// fields, empty discriminator property name). A nil result means the caller
// must fall back to DynamicAttribute. When the union schema is Computed (an
// output shape, e.g. a data-source response), every merged child is Computed
// too — the framework rejects Required children inside a Computed object.
func mergedDiscriminatedUnion(s ir.SchemaIR) *ir.SchemaIR {
	if s.Union == nil || s.Union.Discriminator == nil {
		return nil
	}
	merged, err := transformer.ApplyDynamicUnion(&s)
	if err != nil || !isObjectLike(*merged) {
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

// discriminatedUnionValidators returns the Validators field element carrying
// the DiscriminatorValidator call for a discriminated union, attached through
// the "Object" validator path (validator.Object on the rendered
// SingleNestedAttribute). The discriminator property name is snake_cased to
// match the merged attribute's name.
func discriminatedUnionValidators(s ir.SchemaIR) ast.Expr {
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

// dynamicUnionElement normalizes a collection element type that is a union:
// unions render as Dynamic elements because a typed collection element cannot
// switch on alternatives (nested unions stay Dynamic by design, D2).
func dynamicUnionElement(elem ir.SchemaIR) ir.SchemaIR {
	if elem.Union != nil {
		return ir.SchemaIR{Type: ir.TypeDynamic}
	}
	return elem
}

// objectSchemaHasDiscriminatedUnion reports whether any attribute or nested
// block attribute in the schema is a discriminated union that renders as a
// SingleNestedAttribute with a DiscriminatorValidator, so the generated file
// imports the schema/validator package exactly when it is referenced.
func objectSchemaHasDiscriminatedUnion(s ir.ObjectSchemaIR) bool {
	for _, attr := range s.Attributes {
		if schemaHasDiscriminatedUnion(attr.Schema) {
			return true
		}
	}
	for _, block := range s.Blocks {
		if objectSchemaHasDiscriminatedUnion(block.Schema) {
			return true
		}
	}
	return false
}

// schemaHasDiscriminatedUnion reports whether the schema (or any nested
// attribute it contains) is a discriminated union renderable via the
// dynamic-union strategy.
func schemaHasDiscriminatedUnion(s ir.SchemaIR) bool {
	if mergedDiscriminatedUnion(s) != nil {
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
