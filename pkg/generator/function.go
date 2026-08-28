package generator

import (
	"fmt"
	"go/ast"
	"go/token"
	"path"
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/naming"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// FunctionFile returns the generated internal/provider/function_<name>.go file
// for a Terraform plugin-framework provider-defined function built from the
// supplied FunctionIR. Like every other entity file, it routes AST construction
// through renderEntitySafely, converting a render panic into an ErrorFile rather
// than letting it crash the whole eidos generate run (N-23) — the documented
// contract in harness.go applies to functions too.
func FunctionFile(fn ir.FunctionIR) File {
	path := path.Join("internal", "provider", fmt.Sprintf("function_%s.go", naming.SnakeCase(fn.Name)))
	file, err := renderEntitySafely(func() (*ast.File, error) {
		return generateFunctionFile(fn), nil
	})
	if err != nil {
		return ErrorFile(path, err)
	}
	return GoCodeAST(path, file)
}

// FunctionFiles returns the generated function files for every FunctionIR in
// the provider. Files are emitted in the order the functions are supplied.
func FunctionFiles(functions []ir.FunctionIR) []File {
	files := make([]File, 0, len(functions))
	for _, fn := range functions {
		files = append(files, FunctionFile(fn))
	}
	return files
}

// generateFunctionFile builds the *ast.File for internal/provider/function_<name>.go.
func generateFunctionFile(fn ir.FunctionIR) *ast.File {
	f := astgen.NewFile("provider")
	f.AddImports(
		"context",
		"github.com/hashicorp/terraform-plugin-framework/function",
	)
	// types.* is referenced only by functionAttrType, which the parameter and
	// return renderers call for collection (List/Set/Map) and object
	// parameters/returns; plain primitive parameters/returns use
	// function.StringParameter/Int64Return/etc. directly and never touch
	// types. A function whose parameters and return are all primitive must
	// not import types, or the import is unused and the generated provider
	// does not compile (the §6 latent unused-import bug).
	if functionNeedsTypesImport(fn) {
		f.AddImport("github.com/hashicorp/terraform-plugin-framework/types", "types")
	}
	if functionNeedsAttrImport(fn) {
		f.AddImport("github.com/hashicorp/terraform-plugin-framework/attr", "")
	}

	structName := functionStructName(fn)
	funcName := functionName(fn)

	// Interface assertion.
	f.AddDecl(astgen.VarDeclGen(astgen.VarSpec(
		"_",
		astgen.QualExpr("function", "Function"),
		astgen.Call(
			astgen.Parens(astgen.StarExpr(astgen.Ident(structName))),
			astgen.Nil(),
		),
	)))

	// Function struct.
	f.AddCommentf("%s is the generated Terraform provider-defined function implementation.", structName)
	f.AddDecl(astgen.TypeDecl(structName, astgen.StructType(
		astgen.Field("SourceOperation", astgen.Ident("string"), ""),
	)))

	// Metadata method.
	f.AddComment("Metadata returns the function name.")
	f.AddDecl(astgen.MethodDecl(
		"Metadata", "f", astgen.StarExpr(astgen.Ident(structName)),
		astgen.Params(
			astgen.Field("_", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("_", astgen.QualExpr("function", "MetadataRequest"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("function", "MetadataResponse")), ""),
		),
		astgen.Results(),
		astgen.Block(astgen.AssignStmt(
			[]ast.Expr{astgen.Selector(astgen.Ident("resp"), "Name")},
			[]ast.Expr{astgen.Lit(funcName)},
			token.ASSIGN,
		)),
	))

	// Definition method.
	f.AddComment("Definition returns the function signature.")
	f.AddDecl(astgen.MethodDecl(
		"Definition", "f", astgen.StarExpr(astgen.Ident(structName)),
		astgen.Params(
			astgen.Field("_", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("_", astgen.QualExpr("function", "DefinitionRequest"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("function", "DefinitionResponse")), ""),
		),
		astgen.Results(),
		astgen.Block(astgen.AssignStmt(
			[]ast.Expr{astgen.Selector(astgen.Ident("resp"), "Definition")},
			[]ast.Expr{astgen.CompositeLit(
				astgen.QualExpr("function", "Definition"),
				functionDefinitionValues(fn)...,
			)},
			token.ASSIGN,
		)),
	))

	// Run method.
	f.AddComment("Run executes the function logic.")
	f.AddDecl(astgen.MethodDecl(
		"Run", "f", astgen.StarExpr(astgen.Ident(structName)),
		astgen.Params(
			astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("req", astgen.QualExpr("function", "RunRequest"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("function", "RunResponse")), ""),
		),
		astgen.Results(),
		astgen.Block(
			astgen.AssignStmt(
				[]ast.Expr{astgen.Selector(astgen.Ident("resp"), "Error")},
				[]ast.Expr{astgen.Call(
					astgen.QualExpr("function", "NewFuncError"),
					astgen.Lit("Run is not wired to a remote API endpoint."),
				)},
				token.ASSIGN,
			),
		),
	))

	return f.AST()
}

// functionName returns the Terraform function name. It prefers
// FunctionIR.TypeName and falls back to a snake_cased FunctionIR.Name (via
// typeNameFallback) so a camelCase operation-derived name produces a valid
// Terraform function name rather than failing framework validation (M-19).
func functionName(fn ir.FunctionIR) string {
	if strings.TrimSpace(fn.TypeName) != "" {
		return strings.TrimSpace(fn.TypeName)
	}
	return typeNameFallback(fn.Name)
}

// isFunctionObjectLike reports whether a schema describes a function object
// parameter or return based on its direct attributes. Nested blocks are
// ignored because the Terraform plugin framework does not support nested
// blocks inside function object types.
func isFunctionObjectLike(s ir.SchemaIR) bool {
	return len(s.Attributes) > 0
}

// functionNeedsAttrImport reports whether the generated function file needs the
// terraform-plugin-framework/attr package for object parameter/return types.
func functionNeedsAttrImport(fn ir.FunctionIR) bool {
	for _, arg := range fn.Arguments {
		if isFunctionObjectLike(arg.Schema) {
			return true
		}
	}
	return isFunctionObjectLike(fn.ReturnType)
}

// functionNeedsTypesImport reports whether the generated function file
// references the terraform-plugin-framework types package. types.* is
// referenced only by functionAttrType, which the parameter/return renderers
// call for collection (List/Set/Map) and object parameters/returns; plain
// primitive parameters/returns render as function.StringParameter/
// Int64Return/etc. and never touch types. The gate is therefore whether any
// argument or the return is a collection or object, matching the exact render
// decision so a primitive-only function does not import types unused (the §6
// latent unused-import bug).
func functionNeedsTypesImport(fn ir.FunctionIR) bool {
	for _, arg := range fn.Arguments {
		if functionSchemaReferencesTypes(arg.Schema) {
			return true
		}
	}
	return functionSchemaReferencesTypes(fn.ReturnType)
}

// functionSchemaReferencesTypes reports whether a function parameter/return
// schema, when rendered, emits a types.* reference via functionAttrType. A
// collection (List/Set/Map) renders functionAttrType for its element; an
// object renders functionAttributeTypesMap, which calls functionAttrType for
// each attribute. A primitive, union, or defaulted schema renders a bare
// function.*Parameter/Return and never references types.
func functionSchemaReferencesTypes(s ir.SchemaIR) bool {
	return s.Collection != nil || isFunctionObjectLike(s)
}

// functionDefinitionValues builds the []ast.Expr of KeyValueExpr for function.Definition{...}.
func functionDefinitionValues(fn ir.FunctionIR) []ast.Expr {
	elems := []ast.Expr{}

	summary := fn.FullName
	if strings.TrimSpace(summary) == "" {
		summary = functionName(fn)
	}
	elems = append(elems, astgen.KeyValue("Summary", astgen.Lit(strings.TrimSpace(summary))))

	if fn.Description != "" {
		elems = append(elems, astgen.KeyValue("Description", astgen.Lit(strings.TrimSpace(fn.Description))))
	}
	if fn.MarkdownDescription != "" {
		elems = append(elems, astgen.KeyValue("MarkdownDescription", astgen.Lit(strings.TrimSpace(fn.MarkdownDescription))))
	}

	if len(fn.Arguments) > 0 {
		params := fn.Arguments
		if fn.Variadic {
			// The variadic argument is represented separately as VariadicParameter,
			// so it must not also appear in the positional Parameters list.
			params = params[:len(params)-1]
		}
		if len(params) > 0 {
			paramElems := make([]ast.Expr, 0, len(params))
			for _, arg := range params {
				paramElems = append(paramElems, functionParameterExpr(arg))
			}
			elems = append(elems, astgen.KeyValue("Parameters", astgen.CompositeLit(
				astgen.SliceType(astgen.QualExpr("function", "Parameter")),
				paramElems...,
			)))
		}
	}

	if fn.Variadic && len(fn.Arguments) > 0 {
		last := fn.Arguments[len(fn.Arguments)-1]
		elems = append(elems, astgen.KeyValue("VariadicParameter", functionParameterExpr(last)))
	}

	elems = append(elems, astgen.KeyValue("Return", functionReturnExpr(fn.ReturnType)))

	return elems
}

// functionParameterExpr returns an ast.Expr for a function.Parameter value.
func functionParameterExpr(arg ir.FunctionParamIR) ast.Expr {
	return functionParameterForSchema(arg.Name, arg.Schema, arg.Description, arg.MarkdownDescription, arg.DeprecationMessage)
}

// functionParameterForSchema builds a function parameter expression from a
// schema and parameter metadata. The name is required for definition validation.
func functionParameterForSchema(name string, s ir.SchemaIR, description, markdownDescription, deprecationMessage string) ast.Expr {
	elems := []ast.Expr{
		astgen.KeyValue("Name", astgen.Lit(name)),
	}
	if description != "" {
		elems = append(elems, astgen.KeyValue("Description", astgen.Lit(description)))
	}
	if markdownDescription != "" {
		elems = append(elems, astgen.KeyValue("MarkdownDescription", astgen.Lit(markdownDescription)))
	}
	if deprecationMessage != "" {
		elems = append(elems, astgen.KeyValue("DeprecationMessage", astgen.Lit(deprecationMessage)))
	}

	if s.Collection != nil {
		elem := s.Collection.ElementType
		switch s.Collection.Kind {
		case ir.List:
			elems = append(elems, astgen.KeyValue("ElementType", functionAttrType(elem)))
			return astgen.CompositeLit(astgen.QualExpr("function", "ListParameter"), elems...)
		case ir.Set:
			elems = append(elems, astgen.KeyValue("ElementType", functionAttrType(elem)))
			return astgen.CompositeLit(astgen.QualExpr("function", "SetParameter"), elems...)
		case ir.Map:
			elems = append(elems, astgen.KeyValue("ElementType", functionAttrType(elem)))
			return astgen.CompositeLit(astgen.QualExpr("function", "MapParameter"), elems...)
		}
		// An unknown collection kind falls through to the dynamic default below:
		// representing the parameter as a typed String would silently mislabel
		// its shape (N-27).
	}

	if s.Type != "" {
		switch s.Type {
		case ir.TypeString:
			return astgen.CompositeLit(astgen.QualExpr("function", "StringParameter"), elems...)
		case ir.TypeInt:
			return astgen.CompositeLit(astgen.QualExpr("function", "Int64Parameter"), elems...)
		case ir.TypeFloat:
			return astgen.CompositeLit(astgen.QualExpr("function", "Float64Parameter"), elems...)
		case ir.TypeBool:
			return astgen.CompositeLit(astgen.QualExpr("function", "BoolParameter"), elems...)
		case ir.TypeDynamic:
			return astgen.CompositeLit(astgen.QualExpr("function", "DynamicParameter"), elems...)
		}
		// An unknown primitive type falls through to the dynamic default below
		// (N-27).
	}

	// Union types (oneOf/anyOf) have no first-class function parameter. Render as
	// Dynamic so the signature honestly accepts any type instead of silently
	// degrading the union to String (N-27).
	if s.Union != nil {
		return astgen.CompositeLit(astgen.QualExpr("function", "DynamicParameter"), elems...)
	}

	if isFunctionObjectLike(s) {
		elems = append(elems, astgen.KeyValue("AttributeTypes", functionAttributeTypesMap(s)))
		return astgen.CompositeLit(astgen.QualExpr("function", "ObjectParameter"), elems...)
	}

	// Default to a dynamic parameter when the IR provides no recognizable type
	// information (empty primitive type, unknown collection kind, or no object
	// attributes). DynamicParameter accepts any value, so the generated function
	// signature stays honest about the parameter shape rather than silently
	// degrading an unrecognized shape to String (N-27).
	return astgen.CompositeLit(astgen.QualExpr("function", "DynamicParameter"), elems...)
}

// functionReturnExpr returns an ast.Expr for a function.Return value.
func functionReturnExpr(s ir.SchemaIR) ast.Expr {
	if s.Collection != nil {
		elem := s.Collection.ElementType
		switch s.Collection.Kind {
		case ir.List:
			return astgen.CompositeLit(astgen.QualExpr("function", "ListReturn"), astgen.KeyValue("ElementType", functionAttrType(elem)))
		case ir.Set:
			return astgen.CompositeLit(astgen.QualExpr("function", "SetReturn"), astgen.KeyValue("ElementType", functionAttrType(elem)))
		case ir.Map:
			return astgen.CompositeLit(astgen.QualExpr("function", "MapReturn"), astgen.KeyValue("ElementType", functionAttrType(elem)))
		}
		// An unknown collection kind falls through to the dynamic default below:
		// representing the return as a typed String would silently mislabel its
		// shape (N-27).
	}

	if s.Type != "" {
		switch s.Type {
		case ir.TypeString:
			return astgen.CompositeLit(astgen.QualExpr("function", "StringReturn"))
		case ir.TypeInt:
			return astgen.CompositeLit(astgen.QualExpr("function", "Int64Return"))
		case ir.TypeFloat:
			return astgen.CompositeLit(astgen.QualExpr("function", "Float64Return"))
		case ir.TypeBool:
			return astgen.CompositeLit(astgen.QualExpr("function", "BoolReturn"))
		case ir.TypeDynamic:
			return astgen.CompositeLit(astgen.QualExpr("function", "DynamicReturn"))
		}
		// An unknown primitive type falls through to the dynamic default below
		// (N-27).
	}

	// Union types (oneOf/anyOf) have no first-class function return. Render as
	// Dynamic so the signature honestly accepts any result type instead of
	// silently degrading the union to String (N-27).
	if s.Union != nil {
		return astgen.CompositeLit(astgen.QualExpr("function", "DynamicReturn"))
	}

	if isFunctionObjectLike(s) {
		return astgen.CompositeLit(
			astgen.QualExpr("function", "ObjectReturn"),
			astgen.KeyValue("AttributeTypes", functionAttributeTypesMap(s)),
		)
	}

	// Default to a dynamic return when the IR provides no recognizable type
	// information (empty primitive type, unknown collection kind, or no object
	// attributes). DynamicReturn accepts any value, so the generated function
	// signature stays honest about the return shape rather than silently
	// degrading an unrecognized shape to String (N-27).
	return astgen.CompositeLit(astgen.QualExpr("function", "DynamicReturn"))
}

// functionAttrType maps an IR schema to its Terraform Plugin Framework attr.Type.
func functionAttrType(s ir.SchemaIR) ast.Expr {
	if s.Collection != nil {
		elem := s.Collection.ElementType
		switch s.Collection.Kind {
		case ir.List:
			return astgen.CompositeLit(
				astgen.QualExpr("types", "ListType"),
				astgen.KeyValue("ElemType", functionAttrType(elem)),
			)
		case ir.Set:
			return astgen.CompositeLit(
				astgen.QualExpr("types", "SetType"),
				astgen.KeyValue("ElemType", functionAttrType(elem)),
			)
		case ir.Map:
			return astgen.CompositeLit(
				astgen.QualExpr("types", "MapType"),
				astgen.KeyValue("ElemType", functionAttrType(elem)),
			)
		}
		// An unknown collection kind falls through to the dynamic default below
		// (N-27).
	}

	// Union types have no first-class attr.Type; DynamicType is the honest
	// representation of a value that may be any variant (N-27).
	if s.Union != nil {
		return astgen.QualExpr("types", "DynamicType")
	}

	if isFunctionObjectLike(s) {
		return astgen.CompositeLit(
			astgen.QualExpr("types", "ObjectType"),
			astgen.KeyValue("AttrTypes", functionAttributeTypesMap(s)),
		)
	}

	switch s.Type {
	case ir.TypeString:
		return astgen.QualExpr("types", "StringType")
	case ir.TypeInt:
		return astgen.QualExpr("types", "Int64Type")
	case ir.TypeFloat:
		return astgen.QualExpr("types", "Float64Type")
	case ir.TypeBool:
		return astgen.QualExpr("types", "BoolType")
	case ir.TypeDynamic:
		return astgen.QualExpr("types", "DynamicType")
	}

	// Unknown primitive type: DynamicType accepts any value, keeping the element
	// type honest instead of silently degrading to StringType (N-27).
	return astgen.QualExpr("types", "DynamicType")
}

// functionAttributeTypesMap builds map[string]attr.Type{...} from a schema's
// direct attributes. Nested blocks are intentionally omitted because function
// object parameters and returns do not support nested blocks in the Terraform
// plugin framework.
func functionAttributeTypesMap(s ir.SchemaIR) ast.Expr {
	elemExprs := make([]ast.Expr, 0, len(s.Attributes))
	for _, attr := range s.Attributes {
		elemExprs = append(elemExprs, astgen.KeyValueExpr(
			astgen.Lit(attr.Name),
			functionAttrType(attr.Schema),
		))
	}
	return astgen.CompositeLit(
		astgen.MapType(astgen.Ident("string"), astgen.QualExpr("attr", "Type")),
		elemExprs...,
	)
}
