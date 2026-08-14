package schema

import (
	"go/ast"
	"go/token"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// mapperNode describes a single model type and its nested children for mapper
// generation. FieldNames carries the collision-resolved Go field names for the
// node's own attribute scope, so field references and nested type names stay
// consistent with model_<name>.go.
type mapperNode struct {
	TypeName       string
	ProviderImport string
	GoTypeRef      ast.Expr
	Schema         ir.ObjectSchemaIR
	FieldNames     map[string]string
	Children       []*mapperNode
}

// GenerateValueMappersFile builds the *ast.File for internal/protocol/value_mappers.go.
func GenerateValueMappersFile(resources []ir.ResourceIR, providerImport string) *ast.File {
	f := astgen.NewFile("protocol")
	f.AddImport(providerImport, "provider")
	f.AddImport("github.com/hashicorp/terraform-plugin-go/tftypes", "tftypes")
	f.AddImport("fmt", "")
	f.AddImport("math", "")
	f.AddImport("math/big", "")

	generateDecodeHelpers(f)

	for _, r := range resources {
		node := buildMapperNode(ResourceAPIModelName(r), r.Schema, providerImport)
		generateMapperNode(f, node)
	}

	return f.AST()
}

// buildMapperNode recursively builds a tree of model nodes for a given object
// schema. Each object-like attribute or object-like collection element becomes
// a child node with a unique type name derived from the parent's collision-
// resolved field names.
func buildMapperNode(typeName string, s ir.ObjectSchemaIR, providerImport string) *mapperNode {
	scope := ResolveFieldNames(s.Attributes)
	node := &mapperNode{
		TypeName:       typeName,
		ProviderImport: providerImport,
		GoTypeRef:      astgen.QualExpr("provider", typeName),
		Schema:         s,
		FieldNames:     scope,
	}

	for _, attr := range s.Attributes {
		if SkipAttrForModel(attr) {
			continue
		}
		childSchema, childName := MapperChildSchema(scope, typeName, attr)
		if childSchema != nil {
			child := buildMapperNode(childName, *childSchema, providerImport)
			node.Children = append(node.Children, child)
		}
	}

	return node
}

// MapperChildSchema returns the object schema and generated type name for a
// nested object attribute or object-element collection, if any. The type name
// uses the parent's collision-resolved field name for the attribute so it stays
// unique and consistent with model_<name>.go.
func MapperChildSchema(scope map[string]string, parentTypeName string, attr ir.AttributeIR) (*ir.ObjectSchemaIR, string) {
	s := attr.Schema
	field := resolvedFieldName(scope, attr)

	if IsObjectLike(s) {
		name := nestedTypeName(parentTypeName, field)
		schema := ir.ObjectSchemaIR{Attributes: s.Attributes, Blocks: s.Blocks}
		return &schema, name
	}

	if s.Collection != nil {
		elem := s.Collection.ElementType
		if IsObjectLike(elem) {
			var name string
			switch s.Collection.Kind {
			case ir.List, ir.Set:
				name = nestedTypeName(parentTypeName, field) + "Elem"
			case ir.Map:
				name = nestedTypeName(parentTypeName, field) + "MapElem"
			}
			schema := ir.ObjectSchemaIR{Attributes: elem.Attributes, Blocks: elem.Blocks}
			return &schema, name
		}
	}

	return nil, ""
}

// generateMapperNode emits type, FromValue, and ToValue functions for node and
// its children. Children are generated first so their helper functions are
// available to parent code.
func generateMapperNode(f *astgen.File, node *mapperNode) {
	for _, child := range node.Children {
		generateMapperNode(f, child)
	}

	generateMapperTypeFunc(f, node)
	generateMapperFromValueFunc(f, node)
	generateMapperToValueFunc(f, node)
}

// generateMapperTypeFunc emits a function that returns the tftypes.Type for a
// generated model.
func generateMapperTypeFunc(f *astgen.File, node *mapperNode) {
	kvs := make([]ast.Expr, 0, len(node.Schema.Attributes))
	for _, attr := range node.Schema.Attributes {
		if SkipAttrForModel(attr) {
			continue
		}
		kvs = append(kvs, astgen.KeyValueExpr(astgen.Lit(attr.Name), mapperAttributeTypeExpr(node, attr)))
	}

	mapType := astgen.MapType(astgen.Ident("string"), astgen.QualExpr("tftypes", "Type"))
	mapLit := astgen.CompositeLit(mapType, kvs...)
	objectLit := astgen.CompositeLit(
		astgen.QualExpr("tftypes", "Object"),
		astgen.KeyValueExpr(astgen.Ident("AttributeTypes"), mapLit),
	)

	body := astgen.Block(astgen.Return(objectLit))
	f.AddDecl(astgen.FuncDeclFull(
		node.TypeName+"Type",
		astgen.Params(),
		astgen.Results(astgen.Field("", astgen.QualExpr("tftypes", "Type"), "")),
		body,
	))
}

// mapperAttributeTypeExpr returns the tftypes.Type expression for an attribute.
// TypeInt and TypeFloat both produce tftypes.Number; the generated Go field
// type distinguishes them at decode time.
func mapperAttributeTypeExpr(node *mapperNode, attr ir.AttributeIR) ast.Expr {
	s := attr.Schema
	field := resolvedFieldName(node.FieldNames, attr)

	if s.Collection != nil {
		elem := s.Collection.ElementType
		elemType := mapperElementTypeExpr(node.TypeName, field, s.Collection.Kind, elem)
		switch s.Collection.Kind {
		case ir.List:
			return astgen.CompositeLit(
				astgen.QualExpr("tftypes", "List"),
				astgen.KeyValueExpr(astgen.Ident("ElementType"), elemType),
			)
		case ir.Set:
			return astgen.CompositeLit(
				astgen.QualExpr("tftypes", "Set"),
				astgen.KeyValueExpr(astgen.Ident("ElementType"), elemType),
			)
		case ir.Map:
			return astgen.CompositeLit(
				astgen.QualExpr("tftypes", "Map"),
				astgen.KeyValueExpr(astgen.Ident("ElementType"), elemType),
			)
		}
	}

	if IsObjectLike(s) {
		childName := nestedTypeName(node.TypeName, field)
		return astgen.Call(astgen.Ident(childName + "Type"))
	}

	switch s.Type {
	case ir.TypeString:
		return astgen.QualExpr("tftypes", "String")
	case ir.TypeInt, ir.TypeFloat:
		return astgen.QualExpr("tftypes", "Number")
	case ir.TypeBool:
		return astgen.QualExpr("tftypes", "Bool")
	case ir.TypeDynamic, ir.TypeNull:
		return astgen.QualExpr("tftypes", "DynamicPseudoType")
	}

	return astgen.QualExpr("tftypes", "String")
}

// mapperElementTypeExpr returns the element tftypes.Type for a collection. The
// field argument is the parent's collision-resolved Go field name for the
// attribute, so the child type name matches model_<name>.go.
func mapperElementTypeExpr(parentTypeName, field string, kind ir.CollectionKind, elem ir.SchemaIR) ast.Expr {
	if IsPrimitiveSchema(elem) {
		return tftypesPrimitiveType(elem.Type)
	}

	if IsObjectLike(elem) {
		var childName string
		switch kind {
		case ir.List, ir.Set:
			childName = nestedTypeName(parentTypeName, field) + "Elem"
		case ir.Map:
			childName = nestedTypeName(parentTypeName, field) + "MapElem"
		}
		return astgen.Call(astgen.Ident(childName + "Type"))
	}

	return astgen.QualExpr("tftypes", "String")
}

// generateMapperFromValueFunc emits the FromValue conversion function for a model.
func generateMapperFromValueFunc(f *astgen.File, node *mapperNode) {
	funcName := node.TypeName + "FromValue"

	var fieldStmts []ast.Stmt
	for _, attr := range node.Schema.Attributes {
		if SkipAttrForModel(attr) {
			continue
		}
		fieldStmts = append(fieldStmts, fieldDecodeStmts(node, attr)...)
		// A nested-collection field emits an unconditional error return; any
		// statement after it is unreachable, so stop emitting further fields.
		if endsWithReturn(fieldStmts) {
			break
		}
	}

	body := make([]ast.Stmt, 0, 5+len(fieldStmts)+1)
	body = append(body,
		astgen.DeclStmt(astgen.VarDeclGen(astgen.VarSpec("m", node.GoTypeRef, nil))),
		astgen.If(
			astgen.Call(astgen.Selector(astgen.Ident("v"), "IsNull")),
			astgen.Return(astgen.Ident("m"), astgen.Nil()),
		),
		astgen.If(
			astgen.NotEqual(astgen.Call(astgen.Selector(astgen.Ident("v"), "IsKnown")), astgen.BoolLit(true)),
			astgen.Return(astgen.Ident("m"), astgen.Call(
				astgen.QualExpr("fmt", "Errorf"),
				astgen.Lit("cannot decode unknown %s value"),
				astgen.Lit(node.TypeName),
			)),
		),
	)
	// When a nested-collection field is the only field, its terminal error
	// return means `vals` is never read; declaring it would be an
	// unused-variable compile error.
	if needsVals(fieldStmts) {
		body = append(body,
			astgen.DeclStmt(astgen.VarDeclGen(astgen.VarSpec(
				"vals",
				astgen.MapType(astgen.Ident("string"), astgen.QualExpr("tftypes", "Value")),
				nil,
			))),
			&ast.IfStmt{
				Init: astgen.Assign(
					[]ast.Expr{astgen.Ident("err")},
					[]ast.Expr{astgen.Call(
						astgen.Selector(astgen.Ident("v"), "As"),
						astgen.UnaryPtr(astgen.Ident("vals")),
					)},
				),
				Cond: astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
				Body: astgen.Block(astgen.Return(astgen.Ident("m"), astgen.Ident("err"))),
			},
		)
	}
	body = append(body, fieldStmts...)
	if !endsWithReturn(fieldStmts) {
		body = append(body, astgen.Return(astgen.Ident("m"), astgen.Nil()))
	}

	f.AddDecl(astgen.FuncDeclFull(
		funcName,
		astgen.Params(astgen.Field("v", astgen.QualExpr("tftypes", "Value"), "")),
		astgen.Results(
			astgen.Field("", node.GoTypeRef, ""),
			astgen.Field("", astgen.Ident("error"), ""),
		),
		astgen.Block(body...),
	))
}

// endsWithReturn reports whether the last statement is an unconditional return
// (e.g. the nested-collection unsupported error). Used to stop emitting field
// statements after a terminal return so the generated function has no
// unreachable code.
func endsWithReturn(stmts []ast.Stmt) bool {
	if len(stmts) == 0 {
		return false
	}
	_, ok := stmts[len(stmts)-1].(*ast.ReturnStmt)
	return ok
}

// needsVals reports whether the generated function references the `vals` map.
// The trailing return always reads `vals`, so it is needed whenever that return
// is emitted (!endsWithReturn). When the terminal nested-collection error return
// is the last statement, `vals` is still needed if a normal field before it
// reads the map (len > 1). Only a lone terminal return leaves `vals` unused.
func needsVals(fieldStmts []ast.Stmt) bool {
	return !endsWithReturn(fieldStmts) || len(fieldStmts) > 1
}

// fieldDecodeStmts generates the statements that decode a single attribute from
// the tftypes.Value map into the model field.
func fieldDecodeStmts(node *mapperNode, attr ir.AttributeIR) []ast.Stmt {
	s := attr.Schema
	field := resolvedFieldName(node.FieldNames, attr)

	if s.Collection != nil {
		return collectionDecodeStmts(node, attr)
	}

	if IsObjectLike(s) {
		childName := nestedTypeName(node.TypeName, field)
		return objectDecodeStmts(attr, field, childName)
	}

	switch s.Type {
	case ir.TypeString:
		return primitiveDecodeStmts(attr.Name, field, "decodeString", attr.Required)
	case ir.TypeInt:
		return primitiveDecodeStmts(attr.Name, field, "decodeInt64", attr.Required)
	case ir.TypeFloat:
		return primitiveDecodeStmts(attr.Name, field, "decodeFloat64", attr.Required)
	case ir.TypeBool:
		return primitiveDecodeStmts(attr.Name, field, "decodeBool", attr.Required)
	case ir.TypeDynamic, ir.TypeNull:
		// TypeNull routes through the dynamic decode path so a null-typed
		// attribute decodes into the model's tftypes.Value field instead of
		// being silently skipped (M-23).
		return dynamicDecodeStmts(attr.Name, field, attr.Required)
	}

	return nil
}

// objectDecodeStmts decodes an object-like attribute into the model field.
// Missing required attributes are returned as Go errors rather than Terraform
// diagnostics because the generated FromValue signature returns (T, error).
func objectDecodeStmts(attr ir.AttributeIR, field, childName string) []ast.Stmt {
	if attr.Required {
		thenBody := astgen.Block(
			astgen.If(
				astgen.Call(astgen.Selector(astgen.Ident("val"), "IsNull")),
				astgen.Return(astgen.Ident("m"), astgen.Call(
					astgen.QualExpr("fmt", "Errorf"),
					astgen.Lit("required attribute %q is null"),
					astgen.Lit(attr.Name),
				)),
			),
			astgen.Assign(
				[]ast.Expr{astgen.Ident("nested"), astgen.Ident("err")},
				[]ast.Expr{astgen.Call(astgen.Ident(childName+"FromValue"), astgen.Ident("val"))},
			),
			astgen.If(
				astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
				astgen.Return(astgen.Ident("m"), astgen.Ident("err")),
			),
			astgen.AssignStmt(
				[]ast.Expr{astgen.Selector(astgen.Ident("m"), field)},
				[]ast.Expr{astgen.Ident("nested")},
				token.ASSIGN,
			),
		)
		elseBody := astgen.Block(astgen.Return(astgen.Ident("m"), astgen.Call(
			astgen.QualExpr("fmt", "Errorf"),
			astgen.Lit("missing required attribute %q"),
			astgen.Lit(attr.Name),
		)))

		return []ast.Stmt{&ast.IfStmt{
			Init: astgen.Assign(
				[]ast.Expr{astgen.Ident("val"), astgen.Ident("ok")},
				[]ast.Expr{astgen.IndexExpr(astgen.Ident("vals"), astgen.Lit(attr.Name))},
			),
			Cond: astgen.Ident("ok"),
			Body: thenBody,
			Else: elseBody,
		}}
	}

	return []ast.Stmt{&ast.IfStmt{
		Init: astgen.Assign(
			[]ast.Expr{astgen.Ident("val"), astgen.Ident("ok")},
			[]ast.Expr{astgen.IndexExpr(astgen.Ident("vals"), astgen.Lit(attr.Name))},
		),
		Cond: astgen.Binary(
			astgen.Ident("ok"),
			token.LAND,
			astgen.Unary(token.NOT, astgen.Call(astgen.Selector(astgen.Ident("val"), "IsNull"))),
		),
		Body: astgen.Block(
			astgen.Assign(
				[]ast.Expr{astgen.Ident("nested"), astgen.Ident("err")},
				[]ast.Expr{astgen.Call(astgen.Ident(childName+"FromValue"), astgen.Ident("val"))},
			),
			astgen.If(
				astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
				astgen.Return(astgen.Ident("m"), astgen.Ident("err")),
			),
			astgen.AssignStmt(
				[]ast.Expr{astgen.Selector(astgen.Ident("m"), field)},
				[]ast.Expr{astgen.UnaryPtr(astgen.Ident("nested"))},
				token.ASSIGN,
			),
		),
	}}
}

// dynamicDecodeStmts decodes a dynamic-typed attribute into a tftypes.Value
// field. The stored value round-trips through tftypes.DynamicPseudoType.
// Missing required attributes are returned as Go errors because the generated
// FromValue signature returns (T, error).
func dynamicDecodeStmts(attrName, field string, required bool) []ast.Stmt {
	init := astgen.Assign(
		[]ast.Expr{astgen.Ident("val"), astgen.Ident("ok")},
		[]ast.Expr{astgen.IndexExpr(astgen.Ident("vals"), astgen.Lit(attrName))},
	)
	if required {
		return []ast.Stmt{&ast.IfStmt{
			Init: init,
			Cond: astgen.Ident("ok"),
			Body: astgen.Block(astgen.AssignStmt(
				[]ast.Expr{astgen.Selector(astgen.Ident("m"), field)},
				[]ast.Expr{astgen.Ident("val")},
				token.ASSIGN,
			)),
			Else: astgen.Block(astgen.Return(astgen.Ident("m"), astgen.Call(
				astgen.QualExpr("fmt", "Errorf"),
				astgen.Lit("missing required attribute %q"),
				astgen.Lit(attrName),
			))),
		}}
	}

	return []ast.Stmt{&ast.IfStmt{
		Init: init,
		Cond: astgen.Ident("ok"),
		Body: astgen.Block(astgen.AssignStmt(
			[]ast.Expr{astgen.Selector(astgen.Ident("m"), field)},
			[]ast.Expr{astgen.Ident("val")},
			token.ASSIGN,
		)),
	}}
}

// primitiveDecodeStmts decodes a primitive field. For required fields, a
// missing attribute produces a Go error (not a Terraform diagnostic) because
// the generated FromValue signature returns (T, error); null values decode to
// the field's zero value without error, preserving partial-state tolerance.
func primitiveDecodeStmts(attrName, field, decoder string, required bool) []ast.Stmt {
	init := astgen.Assign(
		[]ast.Expr{astgen.Ident("val"), astgen.Ident("ok")},
		[]ast.Expr{astgen.IndexExpr(astgen.Ident("vals"), astgen.Lit(attrName))},
	)
	if required {
		thenBody := astgen.Block(&ast.IfStmt{
			Init: astgen.Assign(
				[]ast.Expr{astgen.Ident("err")},
				[]ast.Expr{astgen.Call(
					astgen.Ident(decoder),
					astgen.Ident("val"),
					astgen.UnaryPtr(astgen.Selector(astgen.Ident("m"), field)),
				)},
			),
			Cond: astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
			Body: astgen.Block(astgen.Return(astgen.Ident("m"), astgen.Ident("err"))),
		})
		elseBody := astgen.Block(astgen.Return(astgen.Ident("m"), astgen.Call(
			astgen.QualExpr("fmt", "Errorf"),
			astgen.Lit("missing required attribute %q"),
			astgen.Lit(attrName),
		)))

		return []ast.Stmt{&ast.IfStmt{
			Init: init,
			Cond: astgen.Ident("ok"),
			Body: thenBody,
			Else: elseBody,
		}}
	}

	return []ast.Stmt{&ast.IfStmt{
		Init: init,
		Cond: astgen.Ident("ok"),
		Body: astgen.Block(
			astgen.Assign(
				[]ast.Expr{astgen.Ident("v"), astgen.Ident("err")},
				[]ast.Expr{astgen.Call(astgen.Ident(decoder+"Ptr"), astgen.Ident("val"))},
			),
			astgen.If(
				astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
				astgen.Return(astgen.Ident("m"), astgen.Ident("err")),
			),
			astgen.AssignStmt(
				[]ast.Expr{astgen.Selector(astgen.Ident("m"), field)},
				[]ast.Expr{astgen.Ident("v")},
				token.ASSIGN,
			),
		),
	}}
}

// collectionDecodeStmts decodes list/set/map attributes. Missing required
// attributes are returned as Go errors because the generated FromValue
// signature returns (T, error).
func collectionDecodeStmts(node *mapperNode, attr ir.AttributeIR) []ast.Stmt {
	s := attr.Schema
	elem := s.Collection.ElementType
	kind := s.Collection.Kind
	field := resolvedFieldName(node.FieldNames, attr)

	if IsPrimitiveSchema(elem) {
		return primitiveCollectionDecodeStmts(kind, attr.Name, field, elem.Type, attr.Required)
	}

	if IsObjectLike(elem) {
		var childName string
		switch kind {
		case ir.List, ir.Set:
			childName = nestedTypeName(node.TypeName, field) + "Elem"
		case ir.Map:
			childName = nestedTypeName(node.TypeName, field) + "MapElem"
		}
		return objectCollectionDecodeStmts(node, attr.Name, field, kind, childName, attr.Required)
	}

	// Nested collections (for example an OpenAPI array of array, where the
	// element is itself a collection) are not yet supported. Returning nil
	// here would leave the field silently undecoded and produce a
	// tftypes.NewValue type/value mismatch at runtime; instead surface a
	// clear error so the limitation is visible (M-21).
	return nestedCollectionUnsupportedDecodeStmts(attr.Name)
}

// nestedCollectionUnsupportedDecodeStmts returns statements that fail decoding
// with a clear error for unsupported nested collections (M-21).
func nestedCollectionUnsupportedDecodeStmts(attrName string) []ast.Stmt {
	return []ast.Stmt{
		astgen.Return(
			astgen.Ident("m"),
			astgen.Call(
				astgen.QualExpr("fmt", "Errorf"),
				astgen.Lit("decode nested collection for %s is not yet supported"),
				astgen.Lit(attrName),
			),
		),
	}
}

// primitiveCollectionDecodeStmts decodes a collection of primitive values.
func primitiveCollectionDecodeStmts(kind ir.CollectionKind, attrName, field string, elemType ir.PrimitiveType, required bool) []ast.Stmt {
	// Dynamic / null-typed elements round-trip as raw tftypes.Value: the model
	// field is []tftypes.Value (or map[string]tftypes.Value), so the decoder
	// copies the raw elements instead of decoding them to a primitive (G4).
	if elemType == ir.TypeDynamic || elemType == ir.TypeNull {
		return dynamicCollectionDecodeStmts(kind, attrName, field, required)
	}

	var (
		collectionType ast.Expr
		mapType        *ast.MapType
		arrayType      *ast.ArrayType
		elemGoType     ast.Expr
		decoder        string
	)

	switch kind {
	case ir.List, ir.Set:
		arrayType = astgen.SliceType(nil)
		collectionType = arrayType
	case ir.Map:
		mapType = astgen.MapType(astgen.Ident("string"), nil)
		collectionType = mapType
	}

	switch elemType {
	case ir.TypeString:
		elemGoType = astgen.Ident("string")
		decoder = "decodeString"
	case ir.TypeInt:
		elemGoType = astgen.Ident("int64")
		decoder = "decodeInt64"
	case ir.TypeFloat:
		elemGoType = astgen.Ident("float64")
		decoder = "decodeFloat64"
	case ir.TypeBool:
		elemGoType = astgen.Ident("bool")
		decoder = "decodeBool"
	default:
		elemGoType = astgen.Ident("string")
		decoder = "decodeString"
	}

	switch kind {
	case ir.Map:
		mapType.Value = elemGoType
	default:
		arrayType.Elt = elemGoType
	}
	targetType := collectionType

	var decodeBody []ast.Stmt
	switch kind {
	case ir.Map:
		decodeBody = []ast.Stmt{
			astgen.DeclStmt(astgen.VarDeclGen(astgen.VarSpec(
				"elems",
				astgen.MapType(astgen.Ident("string"), astgen.QualExpr("tftypes", "Value")),
				nil,
			))),
			&ast.IfStmt{
				Init: astgen.Assign(
					[]ast.Expr{astgen.Ident("err")},
					[]ast.Expr{astgen.Call(
						astgen.Selector(astgen.Ident("val"), "As"),
						astgen.UnaryPtr(astgen.Ident("elems")),
					)},
				),
				Cond: astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
				Body: astgen.Block(astgen.Return(astgen.Ident("m"), astgen.Ident("err"))),
			},
			astgen.AssignStmt(
				[]ast.Expr{astgen.Selector(astgen.Ident("m"), field)},
				[]ast.Expr{astgen.Call(
					astgen.Ident("make"),
					targetType,
					astgen.Call(astgen.Ident("len"), astgen.Ident("elems")),
				)},
				token.ASSIGN,
			),
			astgen.RangeStmt(
				astgen.Ident("k"),
				astgen.Ident("ev"),
				token.DEFINE,
				astgen.Ident("elems"),
				astgen.Block(
					astgen.DeclStmt(astgen.VarDeclGen(astgen.VarSpec("tmp", elemGoType, nil))),
					&ast.IfStmt{
						Init: astgen.Assign(
							[]ast.Expr{astgen.Ident("err")},
							[]ast.Expr{astgen.Call(
								astgen.Ident(decoder),
								astgen.Ident("ev"),
								astgen.UnaryPtr(astgen.Ident("tmp")),
							)},
						),
						Cond: astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
						Body: astgen.Block(astgen.Return(astgen.Ident("m"), astgen.Ident("err"))),
					},
					astgen.AssignStmt(
						[]ast.Expr{astgen.IndexExpr(astgen.Selector(astgen.Ident("m"), field), astgen.Ident("k"))},
						[]ast.Expr{astgen.Ident("tmp")},
						token.ASSIGN,
					),
				),
			),
		}
	default:
		decodeBody = []ast.Stmt{
			astgen.DeclStmt(astgen.VarDeclGen(astgen.VarSpec(
				"elems",
				astgen.SliceType(astgen.QualExpr("tftypes", "Value")),
				nil,
			))),
			&ast.IfStmt{
				Init: astgen.Assign(
					[]ast.Expr{astgen.Ident("err")},
					[]ast.Expr{astgen.Call(
						astgen.Selector(astgen.Ident("val"), "As"),
						astgen.UnaryPtr(astgen.Ident("elems")),
					)},
				),
				Cond: astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
				Body: astgen.Block(astgen.Return(astgen.Ident("m"), astgen.Ident("err"))),
			},
			astgen.AssignStmt(
				[]ast.Expr{astgen.Selector(astgen.Ident("m"), field)},
				[]ast.Expr{astgen.Call(
					astgen.Ident("make"),
					targetType,
					astgen.Call(astgen.Ident("len"), astgen.Ident("elems")),
				)},
				token.ASSIGN,
			),
			astgen.RangeStmt(
				astgen.Ident("i"),
				astgen.Ident("ev"),
				token.DEFINE,
				astgen.Ident("elems"),
				astgen.Block(&ast.IfStmt{
					Init: astgen.Assign(
						[]ast.Expr{astgen.Ident("err")},
						[]ast.Expr{astgen.Call(
							astgen.Ident(decoder),
							astgen.Ident("ev"),
							astgen.UnaryPtr(astgen.IndexExpr(astgen.Selector(astgen.Ident("m"), field), astgen.Ident("i"))),
						)},
					),
					Cond: astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
					Body: astgen.Block(astgen.Return(astgen.Ident("m"), astgen.Ident("err"))),
				}),
			),
		}
	}

	return wrapCollectionDecode(attrName, required, decodeBody)
}

// dynamicCollectionDecodeStmts decodes a collection whose element type is
// dynamic/null by copying the raw tftypes.Value elements — the model field is
// []tftypes.Value (or map[string]tftypes.Value) and cannot be decoded to a
// primitive (G4).
func dynamicCollectionDecodeStmts(kind ir.CollectionKind, attrName, field string, required bool) []ast.Stmt {
	var elemsType ast.Expr
	switch kind {
	case ir.Map:
		elemsType = astgen.MapType(astgen.Ident("string"), astgen.QualExpr("tftypes", "Value"))
	default:
		elemsType = astgen.SliceType(astgen.QualExpr("tftypes", "Value"))
	}
	decodeBody := []ast.Stmt{
		astgen.DeclStmt(astgen.VarDeclGen(astgen.VarSpec("elems", elemsType, nil))),
		&ast.IfStmt{
			Init: astgen.Assign(
				[]ast.Expr{astgen.Ident("err")},
				[]ast.Expr{astgen.Call(
					astgen.Selector(astgen.Ident("val"), "As"),
					astgen.UnaryPtr(astgen.Ident("elems")),
				)},
			),
			Cond: astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
			Body: astgen.Block(astgen.Return(astgen.Ident("m"), astgen.Ident("err"))),
		},
		astgen.AssignStmt(
			[]ast.Expr{astgen.Selector(astgen.Ident("m"), field)},
			[]ast.Expr{astgen.Ident("elems")},
			token.ASSIGN,
		),
	}
	return wrapCollectionDecode(attrName, required, decodeBody)
}

// wrapCollectionDecode wraps a collection decode body with the shared val/ok
// lookup and the required/null handling used by every collection decoder.
func wrapCollectionDecode(attrName string, required bool, decodeBody []ast.Stmt) []ast.Stmt {
	init := astgen.Assign(
		[]ast.Expr{astgen.Ident("val"), astgen.Ident("ok")},
		[]ast.Expr{astgen.IndexExpr(astgen.Ident("vals"), astgen.Lit(attrName))},
	)

	if required {
		requiredBody := append([]ast.Stmt{
			astgen.If(
				astgen.Call(astgen.Selector(astgen.Ident("val"), "IsNull")),
				astgen.Return(astgen.Ident("m"), astgen.Call(
					astgen.QualExpr("fmt", "Errorf"),
					astgen.Lit("required attribute %q is null"),
					astgen.Lit(attrName),
				)),
			),
		}, decodeBody...)
		return []ast.Stmt{&ast.IfStmt{
			Init: init,
			Cond: astgen.Ident("ok"),
			Body: astgen.Block(requiredBody...),
			Else: astgen.Block(astgen.Return(astgen.Ident("m"), astgen.Call(
				astgen.QualExpr("fmt", "Errorf"),
				astgen.Lit("missing required attribute %q"),
				astgen.Lit(attrName),
			))),
		}}
	}

	return []ast.Stmt{&ast.IfStmt{
		Init: init,
		Cond: astgen.Binary(
			astgen.Ident("ok"),
			token.LAND,
			astgen.Unary(token.NOT, astgen.Call(astgen.Selector(astgen.Ident("val"), "IsNull"))),
		),
		Body: astgen.Block(decodeBody...),
	}}
}

// objectCollectionDecodeStmts decodes a collection of nested object values.
func objectCollectionDecodeStmts(_ *mapperNode, attrName, field string, kind ir.CollectionKind, childName string, required bool) []ast.Stmt {
	childTypeRef := astgen.QualExpr("provider", childName)

	var decodeBody []ast.Stmt
	switch kind {
	case ir.Map:
		decodeBody = []ast.Stmt{
			astgen.DeclStmt(astgen.VarDeclGen(astgen.VarSpec(
				"elems",
				astgen.MapType(astgen.Ident("string"), astgen.QualExpr("tftypes", "Value")),
				nil,
			))),
			&ast.IfStmt{
				Init: astgen.Assign(
					[]ast.Expr{astgen.Ident("err")},
					[]ast.Expr{astgen.Call(
						astgen.Selector(astgen.Ident("val"), "As"),
						astgen.UnaryPtr(astgen.Ident("elems")),
					)},
				),
				Cond: astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
				Body: astgen.Block(astgen.Return(astgen.Ident("m"), astgen.Ident("err"))),
			},
			astgen.AssignStmt(
				[]ast.Expr{astgen.Selector(astgen.Ident("m"), field)},
				[]ast.Expr{astgen.Call(
					astgen.Ident("make"),
					astgen.MapType(astgen.Ident("string"), childTypeRef),
					astgen.Call(astgen.Ident("len"), astgen.Ident("elems")),
				)},
				token.ASSIGN,
			),
			astgen.RangeStmt(
				astgen.Ident("k"),
				astgen.Ident("ev"),
				token.DEFINE,
				astgen.Ident("elems"),
				astgen.Block(
					astgen.Assign(
						[]ast.Expr{astgen.Ident("nested"), astgen.Ident("err")},
						[]ast.Expr{astgen.Call(astgen.Ident(childName+"FromValue"), astgen.Ident("ev"))},
					),
					astgen.If(
						astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
						astgen.Return(astgen.Ident("m"), astgen.Ident("err")),
					),
					astgen.AssignStmt(
						[]ast.Expr{astgen.IndexExpr(astgen.Selector(astgen.Ident("m"), field), astgen.Ident("k"))},
						[]ast.Expr{astgen.Ident("nested")},
						token.ASSIGN,
					),
				),
			),
		}
	default:
		decodeBody = []ast.Stmt{
			astgen.DeclStmt(astgen.VarDeclGen(astgen.VarSpec(
				"elems",
				astgen.SliceType(astgen.QualExpr("tftypes", "Value")),
				nil,
			))),
			&ast.IfStmt{
				Init: astgen.Assign(
					[]ast.Expr{astgen.Ident("err")},
					[]ast.Expr{astgen.Call(
						astgen.Selector(astgen.Ident("val"), "As"),
						astgen.UnaryPtr(astgen.Ident("elems")),
					)},
				),
				Cond: astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
				Body: astgen.Block(astgen.Return(astgen.Ident("m"), astgen.Ident("err"))),
			},
			astgen.AssignStmt(
				[]ast.Expr{astgen.Selector(astgen.Ident("m"), field)},
				[]ast.Expr{astgen.Call(
					astgen.Ident("make"),
					astgen.SliceType(childTypeRef),
					astgen.Call(astgen.Ident("len"), astgen.Ident("elems")),
				)},
				token.ASSIGN,
			),
			astgen.RangeStmt(
				astgen.Ident("i"),
				astgen.Ident("ev"),
				token.DEFINE,
				astgen.Ident("elems"),
				astgen.Block(
					astgen.Assign(
						[]ast.Expr{astgen.Ident("nested"), astgen.Ident("err")},
						[]ast.Expr{astgen.Call(astgen.Ident(childName+"FromValue"), astgen.Ident("ev"))},
					),
					astgen.If(
						astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
						astgen.Return(astgen.Ident("m"), astgen.Ident("err")),
					),
					astgen.AssignStmt(
						[]ast.Expr{astgen.IndexExpr(astgen.Selector(astgen.Ident("m"), field), astgen.Ident("i"))},
						[]ast.Expr{astgen.Ident("nested")},
						token.ASSIGN,
					),
				),
			),
		}
	}

	return wrapCollectionDecode(attrName, required, decodeBody)
}

// generateMapperToValueFunc emits the ToValue conversion function for a model.
func generateMapperToValueFunc(f *astgen.File, node *mapperNode) {
	funcName := node.TypeName + "ToValue"

	var fieldStmts []ast.Stmt
	hasObjectLike := false
	for _, attr := range node.Schema.Attributes {
		if SkipAttrForModel(attr) {
			continue
		}
		if attr.Schema.Collection == nil && IsObjectLike(attr.Schema) {
			hasObjectLike = true
		}
		fieldStmts = append(fieldStmts, fieldEncodeStmts(node, attr)...)
		// A nested-collection field emits an unconditional error return; any
		// statement after it is unreachable, so stop emitting further fields.
		if endsWithReturn(fieldStmts) {
			break
		}
	}

	body := []ast.Stmt{}
	// When a nested-collection field is the only field, its terminal error
	// return means `vals` is never read; declaring it would be an
	// unused-variable compile error.
	if needsVals(fieldStmts) {
		body = append(body,
			astgen.AssignSingle(
				astgen.Ident("vals"),
				astgen.CompositeLit(astgen.MapType(astgen.Ident("string"), astgen.QualExpr("tftypes", "Value"))),
			),
		)
	}
	// Object-like fields encode via `nested, err = ...`. Declare both once at
	// function-body scope so multiple object fields, and optional ones whose
	// assignment sits inside an if-block, all share the same variables.
	if hasObjectLike {
		body = append(body,
			astgen.VarDecl("nested", "tftypes.Value", nil),
			astgen.VarDecl("err", "error", nil),
		)
	}
	body = append(body, fieldStmts...)
	if !endsWithReturn(fieldStmts) {
		body = append(body, astgen.Return(
			astgen.Call(
				astgen.QualExpr("tftypes", "NewValue"),
				astgen.Call(astgen.Ident(node.TypeName+"Type")),
				astgen.Ident("vals"),
			),
			astgen.Nil(),
		))
	}

	f.AddDecl(astgen.FuncDeclFull(
		funcName,
		astgen.Params(astgen.Field("m", node.GoTypeRef, "")),
		astgen.Results(
			astgen.Field("", astgen.QualExpr("tftypes", "Value"), ""),
			astgen.Field("", astgen.Ident("error"), ""),
		),
		astgen.Block(body...),
	))
}

// fieldEncodeStmts generates the statements that encode a single model field
// into the tftypes.Value map. Object-like fields assign to `nested`/`err`,
// which generateMapperToValueFunc declares once at function-body scope.
func fieldEncodeStmts(node *mapperNode, attr ir.AttributeIR) []ast.Stmt {
	s := attr.Schema
	field := resolvedFieldName(node.FieldNames, attr)

	if s.Collection != nil {
		return collectionEncodeStmts(node, attr)
	}

	if IsObjectLike(s) {
		childName := nestedTypeName(node.TypeName, field)
		if attr.Required {
			return []ast.Stmt{
				astgen.AssignStmt(
					[]ast.Expr{astgen.Ident("nested"), astgen.Ident("err")},
					[]ast.Expr{astgen.Call(astgen.Ident(childName+"ToValue"), astgen.Selector(astgen.Ident("m"), field))},
					token.ASSIGN,
				),
				astgen.If(
					astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
					astgen.Return(
						astgen.CompositeLit(astgen.QualExpr("tftypes", "Value")),
						astgen.Ident("err"),
					),
				),
				astgen.AssignStmt(
					[]ast.Expr{astgen.IndexExpr(astgen.Ident("vals"), astgen.Lit(attr.Name))},
					[]ast.Expr{astgen.Ident("nested")},
					token.ASSIGN,
				),
			}
		}
		return []ast.Stmt{
			astgen.IfElse(
				astgen.NotEqual(astgen.Selector(astgen.Ident("m"), field), astgen.Nil()),
				astgen.Block(
					astgen.AssignStmt(
						[]ast.Expr{astgen.Ident("nested"), astgen.Ident("err")},
						[]ast.Expr{astgen.Call(astgen.Ident(childName+"ToValue"), astgen.StarExpr(astgen.Selector(astgen.Ident("m"), field)))},
						token.ASSIGN,
					),
					astgen.If(
						astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
						astgen.Return(
							astgen.CompositeLit(astgen.QualExpr("tftypes", "Value")),
							astgen.Ident("err"),
						),
					),
					astgen.AssignStmt(
						[]ast.Expr{astgen.IndexExpr(astgen.Ident("vals"), astgen.Lit(attr.Name))},
						[]ast.Expr{astgen.Ident("nested")},
						token.ASSIGN,
					),
				),
				astgen.Block(
					astgen.AssignStmt(
						[]ast.Expr{astgen.IndexExpr(astgen.Ident("vals"), astgen.Lit(attr.Name))},
						[]ast.Expr{astgen.Call(
							astgen.QualExpr("tftypes", "NewValue"),
							astgen.Call(astgen.Ident(childName+"Type")),
							astgen.Nil(),
						)},
						token.ASSIGN,
					),
				),
			),
		}
	}

	switch s.Type {
	case ir.TypeString:
		return primitiveEncodeStmts(attr.Name, field, attr.Required, astgen.QualExpr("tftypes", "String"))
	case ir.TypeInt:
		return primitiveEncodeStmts(attr.Name, field, attr.Required, astgen.QualExpr("tftypes", "Number"))
	case ir.TypeFloat:
		return primitiveEncodeStmts(attr.Name, field, attr.Required, astgen.QualExpr("tftypes", "Number"))
	case ir.TypeBool:
		return primitiveEncodeStmts(attr.Name, field, attr.Required, astgen.QualExpr("tftypes", "Bool"))
	case ir.TypeDynamic, ir.TypeNull:
		// TypeNull (OpenAPI 3.1 {"type":"null"}) is represented in the model
		// as a Dynamic tftypes.Value, matching plainFieldType and the
		// framework attribute (M-23); routing it through the dynamic
		// encode/decode path keeps the model and mapper consistent so a
		// null-typed attribute round-trips instead of failing at runtime.
		return []ast.Stmt{
			astgen.IfElse(
				astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("m"), field), "IsNull")),
				astgen.Block(
					astgen.AssignStmt(
						[]ast.Expr{astgen.IndexExpr(astgen.Ident("vals"), astgen.Lit(attr.Name))},
						[]ast.Expr{astgen.Call(
							astgen.QualExpr("tftypes", "NewValue"),
							astgen.QualExpr("tftypes", "DynamicPseudoType"),
							astgen.Nil(),
						)},
						token.ASSIGN,
					),
				),
				astgen.Block(
					astgen.AssignStmt(
						[]ast.Expr{astgen.IndexExpr(astgen.Ident("vals"), astgen.Lit(attr.Name))},
						[]ast.Expr{astgen.Selector(astgen.Ident("m"), field)},
						token.ASSIGN,
					),
				),
			),
		}
	}

	return nil
}

// primitiveEncodeStmts encodes a single primitive field.
func primitiveEncodeStmts(attrName, field string, required bool, tftypesType ast.Expr) []ast.Stmt {
	if required {
		return []ast.Stmt{
			astgen.AssignStmt(
				[]ast.Expr{astgen.IndexExpr(astgen.Ident("vals"), astgen.Lit(attrName))},
				[]ast.Expr{astgen.Call(
					astgen.QualExpr("tftypes", "NewValue"),
					tftypesType,
					astgen.Selector(astgen.Ident("m"), field),
				)},
				token.ASSIGN,
			),
		}
	}

	return []ast.Stmt{
		astgen.IfElse(
			astgen.NotEqual(astgen.Selector(astgen.Ident("m"), field), astgen.Nil()),
			astgen.Block(
				astgen.AssignStmt(
					[]ast.Expr{astgen.IndexExpr(astgen.Ident("vals"), astgen.Lit(attrName))},
					[]ast.Expr{astgen.Call(
						astgen.QualExpr("tftypes", "NewValue"),
						tftypesType,
						astgen.StarExpr(astgen.Selector(astgen.Ident("m"), field)),
					)},
					token.ASSIGN,
				),
			),
			astgen.Block(
				astgen.AssignStmt(
					[]ast.Expr{astgen.IndexExpr(astgen.Ident("vals"), astgen.Lit(attrName))},
					[]ast.Expr{astgen.Call(
						astgen.QualExpr("tftypes", "NewValue"),
						tftypesType,
						astgen.Nil(),
					)},
					token.ASSIGN,
				),
			),
		),
	}
}

// collectionEncodeStmts encodes list/set/map attributes.
func collectionEncodeStmts(node *mapperNode, attr ir.AttributeIR) []ast.Stmt {
	s := attr.Schema
	elem := s.Collection.ElementType
	kind := s.Collection.Kind
	field := resolvedFieldName(node.FieldNames, attr)

	if IsPrimitiveSchema(elem) {
		return primitiveCollectionEncodeStmts(attr.Name, field, kind, elem.Type)
	}

	if IsObjectLike(elem) {
		var childName string
		switch kind {
		case ir.List, ir.Set:
			childName = nestedTypeName(node.TypeName, field) + "Elem"
		case ir.Map:
			childName = nestedTypeName(node.TypeName, field) + "MapElem"
		}
		return objectCollectionEncodeStmts(node, attr.Name, field, kind, childName)
	}

	// Nested collections (for example an OpenAPI array of array) are not yet
	// supported. Returning nil here would leave the field silently
	// unencoded and produce a tftypes.NewValue type/value mismatch at
	// runtime; instead surface a clear error (M-21).
	return nestedCollectionUnsupportedEncodeStmts(attr.Name)
}

// nestedCollectionUnsupportedEncodeStmts returns statements that fail encoding
// with a clear error for unsupported nested collections (M-21).
func nestedCollectionUnsupportedEncodeStmts(attrName string) []ast.Stmt {
	return []ast.Stmt{
		astgen.Return(
			astgen.CompositeLit(astgen.QualExpr("tftypes", "Value")),
			astgen.Call(
				astgen.QualExpr("fmt", "Errorf"),
				astgen.Lit("encode nested collection for %s is not yet supported"),
				astgen.Lit(attrName),
			),
		),
	}
}

// primitiveCollectionEncodeStmts encodes a collection of primitive values.
func primitiveCollectionEncodeStmts(attrName, field string, kind ir.CollectionKind, elemType ir.PrimitiveType) []ast.Stmt {
	tftypesElem := tftypesPrimitiveType(elemType)

	var collectionType ast.Expr
	switch kind {
	case ir.List:
		collectionType = astgen.CompositeLit(
			astgen.QualExpr("tftypes", "List"),
			astgen.KeyValueExpr(astgen.Ident("ElementType"), tftypesElem),
		)
	case ir.Set:
		collectionType = astgen.CompositeLit(
			astgen.QualExpr("tftypes", "Set"),
			astgen.KeyValueExpr(astgen.Ident("ElementType"), tftypesElem),
		)
	case ir.Map:
		collectionType = astgen.CompositeLit(
			astgen.QualExpr("tftypes", "Map"),
			astgen.KeyValueExpr(astgen.Ident("ElementType"), tftypesElem),
		)
	}

	if kind == ir.Map {
		return []ast.Stmt{
			astgen.IfElse(
				astgen.NotEqual(astgen.Selector(astgen.Ident("m"), field), astgen.Nil()),
				astgen.Block(
					astgen.AssignSingle(
						astgen.Ident("elems"),
						astgen.Call(
							astgen.Ident("make"),
							astgen.MapType(astgen.Ident("string"), astgen.QualExpr("tftypes", "Value")),
							astgen.Call(astgen.Ident("len"), astgen.Selector(astgen.Ident("m"), field)),
						),
					),
					astgen.RangeStmt(
						astgen.Ident("k"),
						astgen.Ident("v"),
						token.DEFINE,
						astgen.Selector(astgen.Ident("m"), field),
						astgen.Block(
							astgen.AssignStmt(
								[]ast.Expr{astgen.IndexExpr(astgen.Ident("elems"), astgen.Ident("k"))},
								[]ast.Expr{astgen.Call(
									astgen.QualExpr("tftypes", "NewValue"),
									tftypesElem,
									astgen.Ident("v"),
								)},
								token.ASSIGN,
							),
						),
					),
					astgen.AssignStmt(
						[]ast.Expr{astgen.IndexExpr(astgen.Ident("vals"), astgen.Lit(attrName))},
						[]ast.Expr{astgen.Call(
							astgen.QualExpr("tftypes", "NewValue"),
							collectionType,
							astgen.Ident("elems"),
						)},
						token.ASSIGN,
					),
				),
				astgen.Block(
					astgen.AssignStmt(
						[]ast.Expr{astgen.IndexExpr(astgen.Ident("vals"), astgen.Lit(attrName))},
						[]ast.Expr{astgen.Call(
							astgen.QualExpr("tftypes", "NewValue"),
							collectionType,
							astgen.Nil(),
						)},
						token.ASSIGN,
					),
				),
			),
		}
	}

	return []ast.Stmt{
		astgen.IfElse(
			astgen.NotEqual(astgen.Selector(astgen.Ident("m"), field), astgen.Nil()),
			astgen.Block(
				astgen.AssignSingle(
					astgen.Ident("elems"),
					astgen.Call(
						astgen.Ident("make"),
						astgen.SliceType(astgen.QualExpr("tftypes", "Value")),
						astgen.Call(astgen.Ident("len"), astgen.Selector(astgen.Ident("m"), field)),
					),
				),
				astgen.RangeStmt(
					astgen.Ident("i"),
					astgen.Ident("v"),
					token.DEFINE,
					astgen.Selector(astgen.Ident("m"), field),
					astgen.Block(
						astgen.AssignStmt(
							[]ast.Expr{astgen.IndexExpr(astgen.Ident("elems"), astgen.Ident("i"))},
							[]ast.Expr{astgen.Call(
								astgen.QualExpr("tftypes", "NewValue"),
								tftypesElem,
								astgen.Ident("v"),
							)},
							token.ASSIGN,
						),
					),
				),
				astgen.AssignStmt(
					[]ast.Expr{astgen.IndexExpr(astgen.Ident("vals"), astgen.Lit(attrName))},
					[]ast.Expr{astgen.Call(
						astgen.QualExpr("tftypes", "NewValue"),
						collectionType,
						astgen.Ident("elems"),
					)},
					token.ASSIGN,
				),
			),
			astgen.Block(
				astgen.AssignStmt(
					[]ast.Expr{astgen.IndexExpr(astgen.Ident("vals"), astgen.Lit(attrName))},
					[]ast.Expr{astgen.Call(
						astgen.QualExpr("tftypes", "NewValue"),
						collectionType,
						astgen.Nil(),
					)},
					token.ASSIGN,
				),
			),
		),
	}
}

// objectCollectionEncodeStmts encodes a collection of nested object values.
func objectCollectionEncodeStmts(_ *mapperNode, attrName, field string, kind ir.CollectionKind, childName string) []ast.Stmt {
	var collectionType ast.Expr
	switch kind {
	case ir.List:
		collectionType = astgen.CompositeLit(
			astgen.QualExpr("tftypes", "List"),
			astgen.KeyValueExpr(astgen.Ident("ElementType"), astgen.Call(astgen.Ident(childName+"Type"))),
		)
	case ir.Set:
		collectionType = astgen.CompositeLit(
			astgen.QualExpr("tftypes", "Set"),
			astgen.KeyValueExpr(astgen.Ident("ElementType"), astgen.Call(astgen.Ident(childName+"Type"))),
		)
	case ir.Map:
		collectionType = astgen.CompositeLit(
			astgen.QualExpr("tftypes", "Map"),
			astgen.KeyValueExpr(astgen.Ident("ElementType"), astgen.Call(astgen.Ident(childName+"Type"))),
		)
	}

	if kind == ir.Map {
		return []ast.Stmt{
			astgen.IfElse(
				astgen.NotEqual(astgen.Selector(astgen.Ident("m"), field), astgen.Nil()),
				astgen.Block(
					astgen.AssignSingle(
						astgen.Ident("elems"),
						astgen.Call(
							astgen.Ident("make"),
							astgen.MapType(astgen.Ident("string"), astgen.QualExpr("tftypes", "Value")),
							astgen.Call(astgen.Ident("len"), astgen.Selector(astgen.Ident("m"), field)),
						),
					),
					astgen.RangeStmt(
						astgen.Ident("k"),
						astgen.Ident("v"),
						token.DEFINE,
						astgen.Selector(astgen.Ident("m"), field),
						astgen.Block(
							astgen.Assign(
								[]ast.Expr{astgen.Ident("ev"), astgen.Ident("err")},
								[]ast.Expr{astgen.Call(astgen.Ident(childName+"ToValue"), astgen.Ident("v"))},
							),
							astgen.If(
								astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
								astgen.Return(
									astgen.CompositeLit(astgen.QualExpr("tftypes", "Value")),
									astgen.Ident("err"),
								),
							),
							astgen.AssignStmt(
								[]ast.Expr{astgen.IndexExpr(astgen.Ident("elems"), astgen.Ident("k"))},
								[]ast.Expr{astgen.Ident("ev")},
								token.ASSIGN,
							),
						),
					),
					astgen.AssignStmt(
						[]ast.Expr{astgen.IndexExpr(astgen.Ident("vals"), astgen.Lit(attrName))},
						[]ast.Expr{astgen.Call(
							astgen.QualExpr("tftypes", "NewValue"),
							collectionType,
							astgen.Ident("elems"),
						)},
						token.ASSIGN,
					),
				),
				astgen.Block(
					astgen.AssignStmt(
						[]ast.Expr{astgen.IndexExpr(astgen.Ident("vals"), astgen.Lit(attrName))},
						[]ast.Expr{astgen.Call(
							astgen.QualExpr("tftypes", "NewValue"),
							collectionType,
							astgen.Nil(),
						)},
						token.ASSIGN,
					),
				),
			),
		}
	}

	return []ast.Stmt{
		astgen.IfElse(
			astgen.NotEqual(astgen.Selector(astgen.Ident("m"), field), astgen.Nil()),
			astgen.Block(
				astgen.AssignSingle(
					astgen.Ident("elems"),
					astgen.Call(
						astgen.Ident("make"),
						astgen.SliceType(astgen.QualExpr("tftypes", "Value")),
						astgen.Call(astgen.Ident("len"), astgen.Selector(astgen.Ident("m"), field)),
					),
				),
				astgen.RangeStmt(
					astgen.Ident("i"),
					astgen.Ident("v"),
					token.DEFINE,
					astgen.Selector(astgen.Ident("m"), field),
					astgen.Block(
						astgen.Assign(
							[]ast.Expr{astgen.Ident("ev"), astgen.Ident("err")},
							[]ast.Expr{astgen.Call(astgen.Ident(childName+"ToValue"), astgen.Ident("v"))},
						),
						astgen.If(
							astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
							astgen.Return(
								astgen.CompositeLit(astgen.QualExpr("tftypes", "Value")),
								astgen.Ident("err"),
							),
						),
						astgen.AssignStmt(
							[]ast.Expr{astgen.IndexExpr(astgen.Ident("elems"), astgen.Ident("i"))},
							[]ast.Expr{astgen.Ident("ev")},
							token.ASSIGN,
						),
					),
				),
				astgen.AssignStmt(
					[]ast.Expr{astgen.IndexExpr(astgen.Ident("vals"), astgen.Lit(attrName))},
					[]ast.Expr{astgen.Call(
						astgen.QualExpr("tftypes", "NewValue"),
						collectionType,
						astgen.Ident("elems"),
					)},
					token.ASSIGN,
				),
			),
			astgen.Block(
				astgen.AssignStmt(
					[]ast.Expr{astgen.IndexExpr(astgen.Ident("vals"), astgen.Lit(attrName))},
					[]ast.Expr{astgen.Call(
						astgen.QualExpr("tftypes", "NewValue"),
						collectionType,
						astgen.Nil(),
					)},
					token.ASSIGN,
				),
			),
		),
	}
}

// tftypesPrimitiveType returns the tftypes primitive type expression for a
// primitive IR type. Both TypeInt and TypeFloat map to tftypes.Number; the
// generated Go field type (int64 vs float64) is the authoritative runtime
// discriminator used by the decode helpers.
func tftypesPrimitiveType(t ir.PrimitiveType) ast.Expr {
	switch t {
	case ir.TypeString:
		return astgen.QualExpr("tftypes", "String")
	case ir.TypeInt, ir.TypeFloat:
		return astgen.QualExpr("tftypes", "Number")
	case ir.TypeBool:
		return astgen.QualExpr("tftypes", "Bool")
	case ir.TypeDynamic:
		return astgen.QualExpr("tftypes", "DynamicPseudoType")
	}
	return astgen.QualExpr("tftypes", "String")
}

// generateDecodeHelpers emits shared primitive decoding helpers used by the
// generated FromValue functions.
func generateDecodeHelpers(f *astgen.File) {
	// decodeString decodes a non-null string value into *out. Null values leave
	// *out unchanged and return nil, preserving partial-state tolerance.
	f.AddComment("decodeString decodes a non-null string value into *out. Null values leave *out unchanged and return nil, preserving partial-state tolerance.")
	f.AddDecl(astgen.FuncDeclFull(
		"decodeString",
		astgen.Params(
			astgen.Field("v", astgen.QualExpr("tftypes", "Value"), ""),
			astgen.Field("out", astgen.StarExpr(astgen.Ident("string")), ""),
		),
		astgen.Results(astgen.Field("", astgen.Ident("error"), "")),
		astgen.Block(
			astgen.If(
				astgen.Call(astgen.Selector(astgen.Ident("v"), "IsNull")),
				astgen.Return(astgen.Nil()),
			),
			astgen.Return(astgen.Call(astgen.Selector(astgen.Ident("v"), "As"), astgen.Ident("out"))),
		),
	))

	// decodeStringPtr decodes a string value, returning nil for null values
	// without error so optional fields remain nil.
	f.AddComment("decodeStringPtr decodes a string value, returning nil for null values without error so optional fields remain nil.")
	f.AddDecl(astgen.FuncDeclFull(
		"decodeStringPtr",
		astgen.Params(astgen.Field("v", astgen.QualExpr("tftypes", "Value"), "")),
		astgen.Results(
			astgen.Field("", astgen.StarExpr(astgen.Ident("string")), ""),
			astgen.Field("", astgen.Ident("error"), ""),
		),
		astgen.Block(
			astgen.If(
				astgen.Call(astgen.Selector(astgen.Ident("v"), "IsNull")),
				astgen.Return(astgen.Nil(), astgen.Nil()),
			),
			astgen.VarDecl("s", "string", nil),
			&ast.IfStmt{
				Init: astgen.Assign(
					[]ast.Expr{astgen.Ident("err")},
					[]ast.Expr{astgen.Call(astgen.Selector(astgen.Ident("v"), "As"), astgen.UnaryPtr(astgen.Ident("s")))},
				),
				Cond: astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
				Body: astgen.Block(astgen.Return(astgen.Nil(), astgen.Ident("err"))),
			},
			astgen.Return(astgen.UnaryPtr(astgen.Ident("s")), astgen.Nil()),
		),
	))

	// decodeInt64 decodes a non-null tftypes.Number into *out as int64. Null
	// values leave *out at zero and return nil, preserving partial-state tolerance.
	// NewFloat is allocated per call because big.Float is mutable and not safe
	// for concurrent reuse.
	f.AddComment("decodeInt64 decodes a non-null tftypes.Number into *out as int64. Null values leave *out at zero and return nil, preserving partial-state tolerance. NewFloat is allocated per call because big.Float is mutable and not safe for concurrent reuse.")
	f.AddDecl(astgen.FuncDeclFull(
		"decodeInt64",
		astgen.Params(
			astgen.Field("v", astgen.QualExpr("tftypes", "Value"), ""),
			astgen.Field("out", astgen.StarExpr(astgen.Ident("int64")), ""),
		),
		astgen.Results(astgen.Field("", astgen.Ident("error"), "")),
		astgen.Block(
			astgen.If(
				astgen.Call(astgen.Selector(astgen.Ident("v"), "IsNull")),
				astgen.Return(astgen.Nil()),
			),
			astgen.AssignSingle(
				astgen.Ident("bf"),
				astgen.Call(astgen.QualExpr("big", "NewFloat"), astgen.IntLit(0)),
			),
			&ast.IfStmt{
				Init: astgen.Assign(
					[]ast.Expr{astgen.Ident("err")},
					[]ast.Expr{astgen.Call(astgen.Selector(astgen.Ident("v"), "As"), astgen.UnaryPtr(astgen.Ident("bf")))},
				),
				Cond: astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
				Body: astgen.Block(astgen.Return(astgen.Ident("err"))),
			},
			astgen.Assign(
				[]ast.Expr{astgen.Ident("i"), astgen.Ident("acc")},
				[]ast.Expr{astgen.Call(astgen.Selector(astgen.Ident("bf"), "Int64"))},
			),
			astgen.If(
				astgen.NotEqual(astgen.Ident("acc"), astgen.QualExpr("big", "Exact")),
				astgen.Return(astgen.Call(
					astgen.QualExpr("fmt", "Errorf"),
					astgen.Lit("decode int64: number %s is not an exact integer (accuracy %v)"),
					astgen.Call(astgen.Selector(astgen.Ident("bf"), "String")),
					astgen.Ident("acc"),
				)),
			),
			astgen.AssignStmt(
				[]ast.Expr{astgen.StarExpr(astgen.Ident("out"))},
				[]ast.Expr{astgen.Ident("i")},
				token.ASSIGN,
			),
			astgen.Return(astgen.Nil()),
		),
	))

	// decodeInt64Ptr decodes a number value, returning nil for null values
	// without error so optional fields remain nil.
	f.AddComment("decodeInt64Ptr decodes a number value, returning nil for null values without error so optional fields remain nil.")
	f.AddDecl(astgen.FuncDeclFull(
		"decodeInt64Ptr",
		astgen.Params(astgen.Field("v", astgen.QualExpr("tftypes", "Value"), "")),
		astgen.Results(
			astgen.Field("", astgen.StarExpr(astgen.Ident("int64")), ""),
			astgen.Field("", astgen.Ident("error"), ""),
		),
		astgen.Block(
			astgen.If(
				astgen.Call(astgen.Selector(astgen.Ident("v"), "IsNull")),
				astgen.Return(astgen.Nil(), astgen.Nil()),
			),
			astgen.AssignSingle(
				astgen.Ident("bf"),
				astgen.Call(astgen.QualExpr("big", "NewFloat"), astgen.IntLit(0)),
			),
			&ast.IfStmt{
				Init: astgen.Assign(
					[]ast.Expr{astgen.Ident("err")},
					[]ast.Expr{astgen.Call(astgen.Selector(astgen.Ident("v"), "As"), astgen.UnaryPtr(astgen.Ident("bf")))},
				),
				Cond: astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
				Body: astgen.Block(astgen.Return(astgen.Nil(), astgen.Ident("err"))),
			},
			astgen.Assign(
				[]ast.Expr{astgen.Ident("i"), astgen.Ident("acc")},
				[]ast.Expr{astgen.Call(astgen.Selector(astgen.Ident("bf"), "Int64"))},
			),
			astgen.If(
				astgen.NotEqual(astgen.Ident("acc"), astgen.QualExpr("big", "Exact")),
				astgen.Return(astgen.Nil(), astgen.Call(
					astgen.QualExpr("fmt", "Errorf"),
					astgen.Lit("decode int64: number %s is not an exact integer (accuracy %v)"),
					astgen.Call(astgen.Selector(astgen.Ident("bf"), "String")),
					astgen.Ident("acc"),
				)),
			),
			astgen.Return(astgen.UnaryPtr(astgen.Ident("i")), astgen.Nil()),
		),
	))

	// decodeFloat64 decodes a non-null tftypes.Number into *out as float64. Null
	// values leave *out at zero and return nil, preserving partial-state tolerance.
	// NewFloat is allocated per call because big.Float is mutable and not safe
	// for concurrent reuse.
	f.AddComment("decodeFloat64 decodes a non-null tftypes.Number into *out as float64. Null values leave *out at zero and return nil, preserving partial-state tolerance. NewFloat is allocated per call because big.Float is mutable and not safe for concurrent reuse.")
	f.AddDecl(astgen.FuncDeclFull(
		"decodeFloat64",
		astgen.Params(
			astgen.Field("v", astgen.QualExpr("tftypes", "Value"), ""),
			astgen.Field("out", astgen.StarExpr(astgen.Ident("float64")), ""),
		),
		astgen.Results(astgen.Field("", astgen.Ident("error"), "")),
		astgen.Block(
			astgen.If(
				astgen.Call(astgen.Selector(astgen.Ident("v"), "IsNull")),
				astgen.Return(astgen.Nil()),
			),
			astgen.AssignSingle(
				astgen.Ident("bf"),
				astgen.Call(astgen.QualExpr("big", "NewFloat"), astgen.IntLit(0)),
			),
			&ast.IfStmt{
				Init: astgen.Assign(
					[]ast.Expr{astgen.Ident("err")},
					[]ast.Expr{astgen.Call(astgen.Selector(astgen.Ident("v"), "As"), astgen.UnaryPtr(astgen.Ident("bf")))},
				),
				Cond: astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
				Body: astgen.Block(astgen.Return(astgen.Ident("err"))),
			},
			astgen.Assign(
				[]ast.Expr{astgen.Ident("f"), astgen.Ident("_")},
				[]ast.Expr{astgen.Call(astgen.Selector(astgen.Ident("bf"), "Float64"))},
			),
			astgen.If(
				astgen.Call(astgen.QualExpr("math", "IsInf"), astgen.Ident("f"), astgen.IntLit(0)),
				astgen.Return(astgen.Call(
					astgen.QualExpr("fmt", "Errorf"),
					astgen.Lit("decode float64: number %s is out of float64 range"),
					astgen.Call(astgen.Selector(astgen.Ident("bf"), "String")),
				)),
			),
			astgen.AssignStmt(
				[]ast.Expr{astgen.StarExpr(astgen.Ident("out"))},
				[]ast.Expr{astgen.Ident("f")},
				token.ASSIGN,
			),
			astgen.Return(astgen.Nil()),
		),
	))

	// decodeFloat64Ptr decodes a number value, returning nil for null values
	// without error so optional fields remain nil.
	f.AddComment("decodeFloat64Ptr decodes a number value, returning nil for null values without error so optional fields remain nil.")
	f.AddDecl(astgen.FuncDeclFull(
		"decodeFloat64Ptr",
		astgen.Params(astgen.Field("v", astgen.QualExpr("tftypes", "Value"), "")),
		astgen.Results(
			astgen.Field("", astgen.StarExpr(astgen.Ident("float64")), ""),
			astgen.Field("", astgen.Ident("error"), ""),
		),
		astgen.Block(
			astgen.If(
				astgen.Call(astgen.Selector(astgen.Ident("v"), "IsNull")),
				astgen.Return(astgen.Nil(), astgen.Nil()),
			),
			astgen.AssignSingle(
				astgen.Ident("bf"),
				astgen.Call(astgen.QualExpr("big", "NewFloat"), astgen.IntLit(0)),
			),
			&ast.IfStmt{
				Init: astgen.Assign(
					[]ast.Expr{astgen.Ident("err")},
					[]ast.Expr{astgen.Call(astgen.Selector(astgen.Ident("v"), "As"), astgen.UnaryPtr(astgen.Ident("bf")))},
				),
				Cond: astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
				Body: astgen.Block(astgen.Return(astgen.Nil(), astgen.Ident("err"))),
			},
			astgen.Assign(
				[]ast.Expr{astgen.Ident("f"), astgen.Ident("_")},
				[]ast.Expr{astgen.Call(astgen.Selector(astgen.Ident("bf"), "Float64"))},
			),
			astgen.If(
				astgen.Call(astgen.QualExpr("math", "IsInf"), astgen.Ident("f"), astgen.IntLit(0)),
				astgen.Return(astgen.Nil(), astgen.Call(
					astgen.QualExpr("fmt", "Errorf"),
					astgen.Lit("decode float64: number %s is out of float64 range"),
					astgen.Call(astgen.Selector(astgen.Ident("bf"), "String")),
				)),
			),
			astgen.Return(astgen.UnaryPtr(astgen.Ident("f")), astgen.Nil()),
		),
	))

	// decodeBool decodes a non-null bool value into *out. Null values leave
	// *out unchanged and return nil, preserving partial-state tolerance.
	f.AddComment("decodeBool decodes a non-null bool value into *out. Null values leave *out unchanged and return nil, preserving partial-state tolerance.")
	f.AddDecl(astgen.FuncDeclFull(
		"decodeBool",
		astgen.Params(
			astgen.Field("v", astgen.QualExpr("tftypes", "Value"), ""),
			astgen.Field("out", astgen.StarExpr(astgen.Ident("bool")), ""),
		),
		astgen.Results(astgen.Field("", astgen.Ident("error"), "")),
		astgen.Block(
			astgen.If(
				astgen.Call(astgen.Selector(astgen.Ident("v"), "IsNull")),
				astgen.Return(astgen.Nil()),
			),
			astgen.Return(astgen.Call(astgen.Selector(astgen.Ident("v"), "As"), astgen.Ident("out"))),
		),
	))

	// decodeBoolPtr decodes a bool value, returning nil for null values
	// without error so optional fields remain nil.
	f.AddComment("decodeBoolPtr decodes a bool value, returning nil for null values without error so optional fields remain nil.")
	f.AddDecl(astgen.FuncDeclFull(
		"decodeBoolPtr",
		astgen.Params(astgen.Field("v", astgen.QualExpr("tftypes", "Value"), "")),
		astgen.Results(
			astgen.Field("", astgen.StarExpr(astgen.Ident("bool")), ""),
			astgen.Field("", astgen.Ident("error"), ""),
		),
		astgen.Block(
			astgen.If(
				astgen.Call(astgen.Selector(astgen.Ident("v"), "IsNull")),
				astgen.Return(astgen.Nil(), astgen.Nil()),
			),
			astgen.VarDecl("b", "bool", nil),
			&ast.IfStmt{
				Init: astgen.Assign(
					[]ast.Expr{astgen.Ident("err")},
					[]ast.Expr{astgen.Call(astgen.Selector(astgen.Ident("v"), "As"), astgen.UnaryPtr(astgen.Ident("b")))},
				),
				Cond: astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
				Body: astgen.Block(astgen.Return(astgen.Nil(), astgen.Ident("err"))),
			},
			astgen.Return(astgen.UnaryPtr(astgen.Ident("b")), astgen.Nil()),
		),
	))
}
