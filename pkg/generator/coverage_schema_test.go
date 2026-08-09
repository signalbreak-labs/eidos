package generator

import (
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// schemaType is a shorthand for building SchemaIR values.
func schemaType(t ir.PrimitiveType) ir.SchemaIR { return ir.SchemaIR{Type: t} }

// TestEphemeralSchemaTypesEqual covers every branch of the recursive type
// comparison: primitive mismatch, collection presence/kind mismatch, recursive
// element comparison, union presence/shallow-match, object presence/arity/
// attribute-name/attribute-type mismatch, and the equal case.
func TestEphemeralSchemaTypesEqual(t *testing.T) {
	obj := func(names ...string) ir.SchemaIR {
		attrs := make([]ir.AttributeIR, 0, len(names))
		for _, n := range names {
			attrs = append(attrs, ir.AttributeIR{Name: n, Schema: schemaType(ir.TypeString)})
		}
		return ir.SchemaIR{Attributes: attrs}
	}

	cases := []struct {
		name string
		a, b ir.SchemaIR
		want bool
	}{
		{"equal primitives", schemaType(ir.TypeString), schemaType(ir.TypeString), true},
		{"different primitives", schemaType(ir.TypeString), schemaType(ir.TypeInt), false},
		{"collection presence mismatch", ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List}}, schemaType(ir.TypeString), false},
		{"collection kind mismatch",
			ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List}},
			ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.Set}},
			false},
		{"collection element mismatch",
			ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: schemaType(ir.TypeString)}},
			ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: schemaType(ir.TypeInt)}},
			false},
		{"equal collections",
			ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: schemaType(ir.TypeString)}},
			ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: schemaType(ir.TypeString)}},
			true},
		{"union presence mismatch", ir.SchemaIR{Union: &ir.UnionType{}}, schemaType(ir.TypeString), false},
		{"both unions shallow match", ir.SchemaIR{Union: &ir.UnionType{Kind: ir.OneOf}}, ir.SchemaIR{Union: &ir.UnionType{Kind: ir.AnyOf}}, true},
		{"object presence mismatch", obj("a"), schemaType(ir.TypeString), false},
		{"object arity mismatch", obj("a"), obj("a", "b"), false},
		{"object attribute name mismatch", obj("a"), obj("b"), false},
		{"object attribute type mismatch", obj("a"),
			ir.SchemaIR{Attributes: []ir.AttributeIR{{Name: "a", Schema: schemaType(ir.TypeInt)}}},
			false},
		{"equal objects", obj("a", "b"), obj("b", "a"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ephemeralSchemaTypesEqual(tc.a, tc.b); got != tc.want {
				t.Errorf("ephemeralSchemaTypesEqual() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestEphemeralCollectionAttributeExpr covers the dynamic-element top-level and
// nested-nil branches plus the list/set/map element dispatch.
func TestEphemeralCollectionAttributeExpr(t *testing.T) {
	// A dynamic element at the top level degrades to DynamicAttribute.
	expr := ephemeralCollectionAttributeExpr(
		ir.AttributeIR{Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: schemaType(ir.TypeDynamic)}}},
		"dynamic", "ephemeral")
	if expr == nil || !strings.Contains(renderExpr(t, expr), "DynamicAttribute") {
		t.Errorf("top-level dynamic collection = %v", renderExpr(t, expr))
	}
	// A dynamic element nested inside a path is unrepresentable (nil).
	if expr := ephemeralCollectionAttributeExpr(
		ir.AttributeIR{Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: schemaType(ir.TypeNull)}}},
		"nested.dynamic", "ephemeral"); expr != nil {
		t.Errorf("nested null-element collection should be nil, got %v", renderExpr(t, expr))
	}

	// List of primitives → ListAttribute.
	expr = ephemeralCollectionAttributeExpr(
		ir.AttributeIR{Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: schemaType(ir.TypeString)}}},
		"tags", "ephemeral")
	if !strings.Contains(renderExpr(t, expr), "ListAttribute") {
		t.Errorf("list-of-string = %v", renderExpr(t, expr))
	}
	// Set of primitives → SetAttribute.
	expr = ephemeralCollectionAttributeExpr(
		ir.AttributeIR{Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.Set, ElementType: schemaType(ir.TypeInt)}}},
		"ids", "ephemeral")
	if !strings.Contains(renderExpr(t, expr), "SetAttribute") {
		t.Errorf("set-of-int = %v", renderExpr(t, expr))
	}
	// Map of objects → MapNestedAttribute.
	expr = ephemeralCollectionAttributeExpr(
		ir.AttributeIR{Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.Map, ElementType: ir.SchemaIR{Attributes: []ir.AttributeIR{{Name: "x", Schema: schemaType(ir.TypeString)}}}}}},
		"map", "ephemeral")
	if !strings.Contains(renderExpr(t, expr), "MapNestedAttribute") {
		t.Errorf("map-of-object = %v", renderExpr(t, expr))
	}
	// An unsupported collection kind (e.g. an empty kind) yields nil.
	if expr := ephemeralCollectionAttributeExpr(
		ir.AttributeIR{Schema: ir.SchemaIR{Collection: &ir.CollectionType{ElementType: schemaType(ir.TypeString)}}},
		"unknown", "ephemeral"); expr != nil {
		t.Errorf("unknown collection kind should be nil, got %v", renderExpr(t, expr))
	}
}

// TestEphemeralPrimitiveAttributeExpr covers the recognized primitives, the
// top-level dynamic attribute, and the nested-dynamic nil path.
func TestEphemeralPrimitiveAttributeExpr(t *testing.T) {
	prim := map[ir.PrimitiveType]string{
		ir.TypeString: "StringAttribute",
		ir.TypeInt:    "Int64Attribute",
		ir.TypeFloat:  "Float64Attribute",
		ir.TypeBool:   "BoolAttribute",
	}
	for typ, wantKind := range prim {
		expr := ephemeralPrimitiveAttributeExpr(ir.AttributeIR{Schema: schemaType(typ)}, "")
		if !strings.Contains(renderExpr(t, expr), wantKind) {
			t.Errorf("%s attribute = %v, want %s", typ, renderExpr(t, expr), wantKind)
		}
	}
	// Top-level dynamic → DynamicAttribute.
	if expr := ephemeralPrimitiveAttributeExpr(ir.AttributeIR{Schema: schemaType(ir.TypeDynamic)}, ""); !strings.Contains(renderExpr(t, expr), "DynamicAttribute") {
		t.Errorf("top-level dynamic = %v", renderExpr(t, expr))
	}
	// Nested dynamic is rejected by the framework → nil.
	if expr := ephemeralPrimitiveAttributeExpr(ir.AttributeIR{Schema: schemaType(ir.TypeDynamic)}, "parent.child"); expr != nil {
		t.Errorf("nested dynamic should be nil, got %v", renderExpr(t, expr))
	}
	// Unrecognized primitive → nil.
	if expr := ephemeralPrimitiveAttributeExpr(ir.AttributeIR{Schema: schemaType("custom")}, ""); expr != nil {
		t.Errorf("custom primitive should be nil, got %v", renderExpr(t, expr))
	}
}

// TestResourceCollectionAttributeExpr mirrors the ephemeral collection test
// against the resource attribute builder.
func TestResourceCollectionAttributeExpr(t *testing.T) {
	// Top-level dynamic element degrades to DynamicAttribute.
	expr := resourceCollectionAttributeExpr(
		ir.AttributeIR{Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: schemaType(ir.TypeDynamic)}}},
		"dynamic")
	if expr == nil || !strings.Contains(renderExpr(t, expr), "DynamicAttribute") {
		t.Errorf("top-level dynamic collection = %v", renderExpr(t, expr))
	}
	// Nested dynamic/null element is unrepresentable (nil).
	if expr := resourceCollectionAttributeExpr(
		ir.AttributeIR{Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: schemaType(ir.TypeNull)}}},
		"nested.dynamic"); expr != nil {
		t.Errorf("nested null-element collection should be nil, got %v", renderExpr(t, expr))
	}
	// List of primitives → ListAttribute.
	expr = resourceCollectionAttributeExpr(
		ir.AttributeIR{Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: schemaType(ir.TypeString)}}},
		"tags")
	if !strings.Contains(renderExpr(t, expr), "ListAttribute") {
		t.Errorf("list-of-string = %v", renderExpr(t, expr))
	}
	// Set of objects → SetNestedAttribute.
	expr = resourceCollectionAttributeExpr(
		ir.AttributeIR{Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.Set, ElementType: ir.SchemaIR{Attributes: []ir.AttributeIR{{Name: "x", Schema: schemaType(ir.TypeString)}}}}}},
		"objs")
	if !strings.Contains(renderExpr(t, expr), "SetNestedAttribute") {
		t.Errorf("set-of-object = %v", renderExpr(t, expr))
	}
	// Map of primitives → MapAttribute.
	expr = resourceCollectionAttributeExpr(
		ir.AttributeIR{Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.Map, ElementType: schemaType(ir.TypeBool)}}},
		"flags")
	if !strings.Contains(renderExpr(t, expr), "MapAttribute") {
		t.Errorf("map-of-bool = %v", renderExpr(t, expr))
	}
	// Unknown collection kind → nil.
	if expr := resourceCollectionAttributeExpr(
		ir.AttributeIR{Schema: ir.SchemaIR{Collection: &ir.CollectionType{ElementType: schemaType(ir.TypeString)}}},
		"unknown"); expr != nil {
		t.Errorf("unknown collection kind should be nil, got %v", renderExpr(t, expr))
	}
}

// TestActionMapElementAttributeExpr covers the primitive and object element
// branches plus the nil fallthrough.
func TestActionMapElementAttributeExpr(t *testing.T) {
	attr := ir.AttributeIR{Name: "labels", Schema: ir.SchemaIR{Type: ir.TypeString}}
	prim := actionMapElementAttributeExpr(attr, schemaType(ir.TypeString))
	if prim == nil || !strings.Contains(renderExpr(t, prim), "MapAttribute") {
		t.Errorf("primitive element = %v", renderExpr(t, prim))
	}
	objElem := ir.SchemaIR{Attributes: []ir.AttributeIR{{Name: "x", Schema: schemaType(ir.TypeString)}}}
	obj := actionMapElementAttributeExpr(attr, objElem)
	if obj == nil || !strings.Contains(renderExpr(t, obj), "MapNestedAttribute") {
		t.Errorf("object element = %v", renderExpr(t, obj))
	}
	// A union element is neither primitive nor object-like → nil.
	if got := actionMapElementAttributeExpr(attr, ir.SchemaIR{Union: &ir.UnionType{}}); got != nil {
		t.Errorf("union element should be nil, got %v", renderExpr(t, got))
	}
}

// TestProviderBlockExpr covers the three nesting modes with their optional
// fields plus the unsupported-mode error.
func TestProviderBlockExpr(t *testing.T) {
	mkBlock := func(name string, mode ir.BlockNestingMode, desc, deprecation string) ir.BlockIR {
		return ir.BlockIR{
			Name:               name,
			NestingMode:        mode,
			Description:        desc,
			DeprecationMessage: deprecation,
			Schema:             ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{Name: "api_key", Schema: schemaType(ir.TypeString)}}},
		}
	}

	cases := []struct {
		name  string
		block ir.BlockIR
		want  string
	}{
		{"single", mkBlock("endpoint", ir.NestingSingle, "an endpoint", ""), "SingleNestedBlock"},
		{"list", mkBlock("endpoint", ir.NestingList, "", "use the new block"), "ListNestedBlock"},
		{"set", mkBlock("endpoint", ir.NestingSet, "", ""), "SetNestedBlock"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := providerBlockExpr(tc.block, "")
			if err != nil {
				t.Fatalf("providerBlockExpr() error = %v", err)
			}
			got := renderExpr(t, expr)
			if !strings.Contains(got, tc.want) {
				t.Errorf("providerBlockExpr() = %v, want %s", got, tc.want)
			}
		})
	}

	if _, err := providerBlockExpr(ir.BlockIR{Name: "bad", NestingMode: "exotic"}, ""); err == nil {
		t.Error("expected error for unsupported nesting mode")
	}
}
