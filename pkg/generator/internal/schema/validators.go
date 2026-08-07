package schema

import (
	"fmt"
	"go/ast"
	"go/token"
	"math"
	"regexp"
	"sort"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/ir"
	"github.com/signalbreak-labs/eidos/pkg/transformer"
)

type validatorNeeds struct {
	float64ExclusiveMin bool
	float64ExclusiveMax bool
	float64MultipleOf   bool
	int64ExclusiveMin   bool
	int64ExclusiveMax   bool
	int64MultipleOf     bool
	discriminator       bool
	conditional         bool
	patternProperties   bool
}

// isEmpty reports whether no custom validators are required.
func (v validatorNeeds) isEmpty() bool {
	return !v.float64ExclusiveMin && !v.float64ExclusiveMax && !v.float64MultipleOf &&
		!v.int64ExclusiveMin && !v.int64ExclusiveMax && !v.int64MultipleOf &&
		!v.discriminator && !v.conditional && !v.patternProperties
}

// GenerateValidatorsFile builds the *ast.File for internal/provider/validators.go.
// Nil is returned when the IR contains no advanced constraints that require
// custom validators.
func GenerateValidatorsFile(pir ir.ProviderIR) *ast.File {
	needs := collectValidatorNeeds(pir)
	if needs.isEmpty() {
		return nil
	}
	f := astgen.NewFile("provider")

	if needs.float64ExclusiveMin {
		generateFloat64ExclusiveMinimumValidator(f)
	}
	if needs.float64ExclusiveMax {
		generateFloat64ExclusiveMaximumValidator(f)
	}
	if needs.float64MultipleOf {
		generateFloat64MultipleOfValidator(f)
	}
	if needs.int64ExclusiveMin {
		generateInt64ExclusiveMinimumValidator(f)
	}
	if needs.int64ExclusiveMax {
		generateInt64ExclusiveMaximumValidator(f)
	}
	if needs.int64MultipleOf {
		generateInt64MultipleOfValidator(f)
	}
	if needs.discriminator {
		generateDiscriminatorValidator(f)
	}
	if needs.conditional {
		generateConditionalValidator(f)
	}
	if needs.patternProperties {
		generatePatternPropertiesValidator(f)
	}

	return f.AST()
}

func collectValidatorNeeds(pir ir.ProviderIR) validatorNeeds {
	var needs validatorNeeds

	for _, attr := range pir.ConfigSchema.Attributes {
		collectSchemaValidatorNeeds(attr.Schema, &needs)
	}
	for _, block := range pir.ConfigSchema.Blocks {
		collectObjectSchemaValidatorNeeds(block.Schema, &needs)
	}

	for _, r := range pir.Resources {
		collectObjectSchemaValidatorNeeds(r.Schema, &needs)
	}
	for _, ds := range pir.DataSources {
		collectObjectSchemaValidatorNeeds(ds.Schema, &needs)
	}
	for _, a := range pir.Actions {
		collectObjectSchemaValidatorNeeds(a.ConfigSchema, &needs)
	}
	for _, er := range pir.EphemeralResources {
		collectObjectSchemaValidatorNeeds(er.ConfigSchema, &needs)
		collectObjectSchemaValidatorNeeds(er.ResultSchema, &needs)
	}
	for _, lr := range pir.ListResources {
		collectObjectSchemaValidatorNeeds(lr.ConfigSchema, &needs)
		collectObjectSchemaValidatorNeeds(lr.IdentitySchema, &needs)
		if lr.ResourceSchema != nil {
			collectObjectSchemaValidatorNeeds(*lr.ResourceSchema, &needs)
		}
	}
	for _, fn := range pir.Functions {
		for _, param := range fn.Arguments {
			collectSchemaValidatorNeeds(param.Schema, &needs)
		}
		collectSchemaValidatorNeeds(fn.ReturnType, &needs)
	}

	return needs
}

func collectObjectSchemaValidatorNeeds(s ir.ObjectSchemaIR, needs *validatorNeeds) {
	for _, attr := range s.Attributes {
		collectSchemaValidatorNeeds(attr.Schema, needs)
	}
	for _, block := range s.Blocks {
		collectObjectSchemaValidatorNeeds(block.Schema, needs)
	}
}

func collectSchemaValidatorNeeds(s ir.SchemaIR, needs *validatorNeeds) {
	if s.Collection != nil {
		collectSchemaValidatorNeeds(s.Collection.ElementType, needs)
	}
	if s.Union != nil {
		if s.Union.Discriminator != nil {
			needs.discriminator = true
		}
		for _, v := range s.Union.Variants {
			collectSchemaValidatorNeeds(v, needs)
		}
	}
	if len(s.DependentRequired) > 0 {
		needs.conditional = true
	}
	if len(s.PatternProperties) > 0 {
		needs.patternProperties = true
	}

	switch s.Type {
	case ir.TypeInt:
		if s.ExclusiveMinimum != nil {
			needs.int64ExclusiveMin = true
		}
		if s.ExclusiveMaximum != nil {
			needs.int64ExclusiveMax = true
		}
		if s.MultipleOf != nil {
			needs.int64MultipleOf = true
		}
	case ir.TypeFloat:
		if s.ExclusiveMinimum != nil {
			needs.float64ExclusiveMin = true
		}
		if s.ExclusiveMaximum != nil {
			needs.float64ExclusiveMax = true
		}
		if s.MultipleOf != nil {
			needs.float64MultipleOf = true
		}
	}

	for _, attr := range s.Attributes {
		collectSchemaValidatorNeeds(attr.Schema, needs)
	}
	for _, block := range s.Blocks {
		collectObjectSchemaValidatorNeeds(block.Schema, needs)
	}
}

// generateFloat64ExclusiveMinimumValidator emits a custom float64 validator
// that enforces value > min.
func generateFloat64ExclusiveMinimumValidator(f *astgen.File) {
	f.AddImport("context", "")
	f.AddImport("fmt", "")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/schema/validator", "validator")

	f.AddComment("float64ExclusiveMinimumValidator validates that a float64 value is strictly greater than min.")
	f.AddDecl(astgen.TypeDecl("float64ExclusiveMinimumValidator", astgen.StructType(
		astgen.Field("min", astgen.Ident("float64"), ""),
	)))

	f.AddDecl(astgen.FuncDeclFull("Float64ExclusiveMinimumValidator",
		astgen.Params(astgen.Field("min", astgen.Ident("float64"), "")),
		astgen.Params(astgen.Field("", astgen.QualExpr("validator", "Float64"), "")),
		astgen.Block(astgen.Return(astgen.CompositeLit(astgen.Ident("float64ExclusiveMinimumValidator"),
			astgen.KeyValue("min", astgen.Ident("min")),
		))),
	))

	f.AddDecl(astgen.MethodDecl("Description", "v", astgen.Ident("float64ExclusiveMinimumValidator"),
		astgen.Params(astgen.Field("_", astgen.QualExpr("context", "Context"), "")),
		astgen.Params(astgen.Field("", astgen.Ident("string"), "")),
		astgen.Block(astgen.Return(astgen.Call(
			astgen.QualExpr("fmt", "Sprintf"),
			astgen.Lit("value must be greater than %v"),
			astgen.Selector(astgen.Ident("v"), "min"),
		))),
	))

	f.AddDecl(astgen.MethodDecl("MarkdownDescription", "v", astgen.Ident("float64ExclusiveMinimumValidator"),
		astgen.Params(astgen.Field("ctx", astgen.QualExpr("context", "Context"), "")),
		astgen.Params(astgen.Field("", astgen.Ident("string"), "")),
		astgen.Block(astgen.Return(astgen.Call(
			astgen.Selector(astgen.Ident("v"), "Description"),
			astgen.Ident("ctx"),
		))),
	))

	f.AddDecl(astgen.MethodDecl("ValidateFloat64", "v", astgen.Ident("float64ExclusiveMinimumValidator"),
		astgen.Params(
			astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("req", astgen.QualExpr("validator", "Float64Request"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("validator", "Float64Response")), ""),
		),
		astgen.Params(),
		astgen.Block(
			astgen.If(
				astgen.Binary(
					astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ConfigValue"), "IsNull")),
					token.LOR,
					astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ConfigValue"), "IsUnknown")),
				),
				astgen.Return(),
			),
			astgen.If(
				astgen.Binary(
					astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ConfigValue"), "ValueFloat64")),
					token.LEQ,
					astgen.Selector(astgen.Ident("v"), "min"),
				),
				astgen.ExprStmt(astgen.Call(
					astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "AddAttributeError"),
					astgen.Selector(astgen.Ident("req"), "Path"),
					astgen.Lit("Invalid Value"),
					astgen.Call(
						astgen.QualExpr("fmt", "Sprintf"),
						astgen.Lit("value must be greater than %v, got %v"),
						astgen.Selector(astgen.Ident("v"), "min"),
						astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ConfigValue"), "ValueFloat64")),
					),
				)),
			),
		),
	))
}

// generateFloat64ExclusiveMaximumValidator emits a custom float64 validator
// that enforces value < max.
func generateFloat64ExclusiveMaximumValidator(f *astgen.File) {
	f.AddImport("context", "")
	f.AddImport("fmt", "")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/schema/validator", "validator")

	f.AddComment("float64ExclusiveMaximumValidator validates that a float64 value is strictly less than max.")
	f.AddDecl(astgen.TypeDecl("float64ExclusiveMaximumValidator", astgen.StructType(
		astgen.Field("max", astgen.Ident("float64"), ""),
	)))

	f.AddDecl(astgen.FuncDeclFull("Float64ExclusiveMaximumValidator",
		astgen.Params(astgen.Field("max", astgen.Ident("float64"), "")),
		astgen.Params(astgen.Field("", astgen.QualExpr("validator", "Float64"), "")),
		astgen.Block(astgen.Return(astgen.CompositeLit(astgen.Ident("float64ExclusiveMaximumValidator"),
			astgen.KeyValue("max", astgen.Ident("max")),
		))),
	))

	f.AddDecl(astgen.MethodDecl("Description", "v", astgen.Ident("float64ExclusiveMaximumValidator"),
		astgen.Params(astgen.Field("_", astgen.QualExpr("context", "Context"), "")),
		astgen.Params(astgen.Field("", astgen.Ident("string"), "")),
		astgen.Block(astgen.Return(astgen.Call(
			astgen.QualExpr("fmt", "Sprintf"),
			astgen.Lit("value must be less than %v"),
			astgen.Selector(astgen.Ident("v"), "max"),
		))),
	))

	f.AddDecl(astgen.MethodDecl("MarkdownDescription", "v", astgen.Ident("float64ExclusiveMaximumValidator"),
		astgen.Params(astgen.Field("ctx", astgen.QualExpr("context", "Context"), "")),
		astgen.Params(astgen.Field("", astgen.Ident("string"), "")),
		astgen.Block(astgen.Return(astgen.Call(
			astgen.Selector(astgen.Ident("v"), "Description"),
			astgen.Ident("ctx"),
		))),
	))

	f.AddDecl(astgen.MethodDecl("ValidateFloat64", "v", astgen.Ident("float64ExclusiveMaximumValidator"),
		astgen.Params(
			astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("req", astgen.QualExpr("validator", "Float64Request"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("validator", "Float64Response")), ""),
		),
		astgen.Params(),
		astgen.Block(
			astgen.If(
				astgen.Binary(
					astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ConfigValue"), "IsNull")),
					token.LOR,
					astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ConfigValue"), "IsUnknown")),
				),
				astgen.Return(),
			),
			astgen.If(
				astgen.Binary(
					astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ConfigValue"), "ValueFloat64")),
					token.GEQ,
					astgen.Selector(astgen.Ident("v"), "max"),
				),
				astgen.ExprStmt(astgen.Call(
					astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "AddAttributeError"),
					astgen.Selector(astgen.Ident("req"), "Path"),
					astgen.Lit("Invalid Value"),
					astgen.Call(
						astgen.QualExpr("fmt", "Sprintf"),
						astgen.Lit("value must be less than %v, got %v"),
						astgen.Selector(astgen.Ident("v"), "max"),
						astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ConfigValue"), "ValueFloat64")),
					),
				)),
			),
		),
	))
}

// generateFloat64MultipleOfValidator emits a custom float64 validator that
// enforces value is a multiple of factor within floating-point tolerance.
func generateFloat64MultipleOfValidator(f *astgen.File) {
	f.AddImport("context", "")
	f.AddImport("fmt", "")
	f.AddImport("math", "")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/schema/validator", "validator")

	f.AddComment("float64MultipleOfValidator validates that a float64 value is a multiple of factor.")
	f.AddDecl(astgen.TypeDecl("float64MultipleOfValidator", astgen.StructType(
		astgen.Field("factor", astgen.Ident("float64"), ""),
	)))

	f.AddDecl(astgen.FuncDeclFull("Float64MultipleOfValidator",
		astgen.Params(astgen.Field("factor", astgen.Ident("float64"), "")),
		astgen.Params(astgen.Field("", astgen.QualExpr("validator", "Float64"), "")),
		astgen.Block(astgen.Return(astgen.CompositeLit(astgen.Ident("float64MultipleOfValidator"),
			astgen.KeyValue("factor", astgen.Ident("factor")),
		))),
	))

	f.AddDecl(astgen.MethodDecl("Description", "v", astgen.Ident("float64MultipleOfValidator"),
		astgen.Params(astgen.Field("_", astgen.QualExpr("context", "Context"), "")),
		astgen.Params(astgen.Field("", astgen.Ident("string"), "")),
		astgen.Block(astgen.Return(astgen.Call(
			astgen.QualExpr("fmt", "Sprintf"),
			astgen.Lit("value must be a multiple of %v"),
			astgen.Selector(astgen.Ident("v"), "factor"),
		))),
	))

	f.AddDecl(astgen.MethodDecl("MarkdownDescription", "v", astgen.Ident("float64MultipleOfValidator"),
		astgen.Params(astgen.Field("ctx", astgen.QualExpr("context", "Context"), "")),
		astgen.Params(astgen.Field("", astgen.Ident("string"), "")),
		astgen.Block(astgen.Return(astgen.Call(
			astgen.Selector(astgen.Ident("v"), "Description"),
			astgen.Ident("ctx"),
		))),
	))

	f.AddDecl(astgen.MethodDecl("ValidateFloat64", "v", astgen.Ident("float64MultipleOfValidator"),
		astgen.Params(
			astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("req", astgen.QualExpr("validator", "Float64Request"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("validator", "Float64Response")), ""),
		),
		astgen.Params(),
		astgen.Block(
			astgen.If(
				astgen.Binary(
					astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ConfigValue"), "IsNull")),
					token.LOR,
					astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ConfigValue"), "IsUnknown")),
				),
				astgen.Return(),
			),
			astgen.AssignSingle(
				astgen.Ident("value"),
				astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ConfigValue"), "ValueFloat64")),
			),
			astgen.If(
				astgen.Binary(
					astgen.Selector(astgen.Ident("v"), "factor"),
					token.EQL,
					astgen.IntLit(0),
				),
				astgen.Block(
					astgen.ExprStmt(astgen.Call(
						astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "AddAttributeError"),
						astgen.Selector(astgen.Ident("req"), "Path"),
						astgen.Lit("Invalid Validator Configuration"),
						astgen.Lit("multipleOf factor must not be zero"),
					)),
					astgen.Return(),
				),
			),
			astgen.AssignSingle(
				astgen.Ident("remainder"),
				astgen.Call(
					astgen.QualExpr("math", "Mod"),
					astgen.Ident("value"),
					astgen.Selector(astgen.Ident("v"), "factor"),
				),
			),
			astgen.If(
				astgen.Binary(
					astgen.Binary(
						astgen.Call(astgen.QualExpr("math", "Abs"), astgen.Ident("remainder")),
						token.GTR,
						astgen.FloatLit(1e-9),
					),
					token.LAND,
					astgen.Binary(
						astgen.Call(
							astgen.QualExpr("math", "Abs"),
							astgen.Binary(astgen.Ident("remainder"), token.SUB, astgen.Selector(astgen.Ident("v"), "factor")),
						),
						token.GTR,
						astgen.FloatLit(1e-9),
					),
				),
				astgen.ExprStmt(astgen.Call(
					astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "AddAttributeError"),
					astgen.Selector(astgen.Ident("req"), "Path"),
					astgen.Lit("Invalid Value"),
					astgen.Call(
						astgen.QualExpr("fmt", "Sprintf"),
						astgen.Lit("value must be a multiple of %v, got %v"),
						astgen.Selector(astgen.Ident("v"), "factor"),
						astgen.Ident("value"),
					),
				)),
			),
		),
	))
}

// generateInt64ExclusiveMinimumValidator emits a custom int64 validator that
// enforces value > min.
func generateInt64ExclusiveMinimumValidator(f *astgen.File) {
	f.AddImport("context", "")
	f.AddImport("fmt", "")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/schema/validator", "validator")

	f.AddComment("int64ExclusiveMinimumValidator validates that an int64 value is strictly greater than min.")
	f.AddDecl(astgen.TypeDecl("int64ExclusiveMinimumValidator", astgen.StructType(
		astgen.Field("min", astgen.Ident("int64"), ""),
	)))

	f.AddDecl(astgen.FuncDeclFull("Int64ExclusiveMinimumValidator",
		astgen.Params(astgen.Field("min", astgen.Ident("int64"), "")),
		astgen.Params(astgen.Field("", astgen.QualExpr("validator", "Int64"), "")),
		astgen.Block(astgen.Return(astgen.CompositeLit(astgen.Ident("int64ExclusiveMinimumValidator"),
			astgen.KeyValue("min", astgen.Ident("min")),
		))),
	))

	f.AddDecl(astgen.MethodDecl("Description", "v", astgen.Ident("int64ExclusiveMinimumValidator"),
		astgen.Params(astgen.Field("_", astgen.QualExpr("context", "Context"), "")),
		astgen.Params(astgen.Field("", astgen.Ident("string"), "")),
		astgen.Block(astgen.Return(astgen.Call(
			astgen.QualExpr("fmt", "Sprintf"),
			astgen.Lit("value must be greater than %v"),
			astgen.Selector(astgen.Ident("v"), "min"),
		))),
	))

	f.AddDecl(astgen.MethodDecl("MarkdownDescription", "v", astgen.Ident("int64ExclusiveMinimumValidator"),
		astgen.Params(astgen.Field("ctx", astgen.QualExpr("context", "Context"), "")),
		astgen.Params(astgen.Field("", astgen.Ident("string"), "")),
		astgen.Block(astgen.Return(astgen.Call(
			astgen.Selector(astgen.Ident("v"), "Description"),
			astgen.Ident("ctx"),
		))),
	))

	f.AddDecl(astgen.MethodDecl("ValidateInt64", "v", astgen.Ident("int64ExclusiveMinimumValidator"),
		astgen.Params(
			astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("req", astgen.QualExpr("validator", "Int64Request"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("validator", "Int64Response")), ""),
		),
		astgen.Params(),
		astgen.Block(
			astgen.If(
				astgen.Binary(
					astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ConfigValue"), "IsNull")),
					token.LOR,
					astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ConfigValue"), "IsUnknown")),
				),
				astgen.Return(),
			),
			astgen.If(
				astgen.Binary(
					astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ConfigValue"), "ValueInt64")),
					token.LEQ,
					astgen.Selector(astgen.Ident("v"), "min"),
				),
				astgen.ExprStmt(astgen.Call(
					astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "AddAttributeError"),
					astgen.Selector(astgen.Ident("req"), "Path"),
					astgen.Lit("Invalid Value"),
					astgen.Call(
						astgen.QualExpr("fmt", "Sprintf"),
						astgen.Lit("value must be greater than %v, got %v"),
						astgen.Selector(astgen.Ident("v"), "min"),
						astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ConfigValue"), "ValueInt64")),
					),
				)),
			),
		),
	))
}

// generateInt64ExclusiveMaximumValidator emits a custom int64 validator that
// enforces value < max.
func generateInt64ExclusiveMaximumValidator(f *astgen.File) {
	f.AddImport("context", "")
	f.AddImport("fmt", "")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/schema/validator", "validator")

	f.AddComment("int64ExclusiveMaximumValidator validates that an int64 value is strictly less than max.")
	f.AddDecl(astgen.TypeDecl("int64ExclusiveMaximumValidator", astgen.StructType(
		astgen.Field("max", astgen.Ident("int64"), ""),
	)))

	f.AddDecl(astgen.FuncDeclFull("Int64ExclusiveMaximumValidator",
		astgen.Params(astgen.Field("max", astgen.Ident("int64"), "")),
		astgen.Params(astgen.Field("", astgen.QualExpr("validator", "Int64"), "")),
		astgen.Block(astgen.Return(astgen.CompositeLit(astgen.Ident("int64ExclusiveMaximumValidator"),
			astgen.KeyValue("max", astgen.Ident("max")),
		))),
	))

	f.AddDecl(astgen.MethodDecl("Description", "v", astgen.Ident("int64ExclusiveMaximumValidator"),
		astgen.Params(astgen.Field("_", astgen.QualExpr("context", "Context"), "")),
		astgen.Params(astgen.Field("", astgen.Ident("string"), "")),
		astgen.Block(astgen.Return(astgen.Call(
			astgen.QualExpr("fmt", "Sprintf"),
			astgen.Lit("value must be less than %v"),
			astgen.Selector(astgen.Ident("v"), "max"),
		))),
	))

	f.AddDecl(astgen.MethodDecl("MarkdownDescription", "v", astgen.Ident("int64ExclusiveMaximumValidator"),
		astgen.Params(astgen.Field("ctx", astgen.QualExpr("context", "Context"), "")),
		astgen.Params(astgen.Field("", astgen.Ident("string"), "")),
		astgen.Block(astgen.Return(astgen.Call(
			astgen.Selector(astgen.Ident("v"), "Description"),
			astgen.Ident("ctx"),
		))),
	))

	f.AddDecl(astgen.MethodDecl("ValidateInt64", "v", astgen.Ident("int64ExclusiveMaximumValidator"),
		astgen.Params(
			astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("req", astgen.QualExpr("validator", "Int64Request"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("validator", "Int64Response")), ""),
		),
		astgen.Params(),
		astgen.Block(
			astgen.If(
				astgen.Binary(
					astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ConfigValue"), "IsNull")),
					token.LOR,
					astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ConfigValue"), "IsUnknown")),
				),
				astgen.Return(),
			),
			astgen.If(
				astgen.Binary(
					astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ConfigValue"), "ValueInt64")),
					token.GEQ,
					astgen.Selector(astgen.Ident("v"), "max"),
				),
				astgen.ExprStmt(astgen.Call(
					astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "AddAttributeError"),
					astgen.Selector(astgen.Ident("req"), "Path"),
					astgen.Lit("Invalid Value"),
					astgen.Call(
						astgen.QualExpr("fmt", "Sprintf"),
						astgen.Lit("value must be less than %v, got %v"),
						astgen.Selector(astgen.Ident("v"), "max"),
						astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ConfigValue"), "ValueInt64")),
					),
				)),
			),
		),
	))
}

// generateInt64MultipleOfValidator emits a custom int64 validator that
// enforces value % factor == 0.
func generateInt64MultipleOfValidator(f *astgen.File) {
	f.AddImport("context", "")
	f.AddImport("fmt", "")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/schema/validator", "validator")

	f.AddComment("int64MultipleOfValidator validates that an int64 value is a multiple of factor.")
	f.AddDecl(astgen.TypeDecl("int64MultipleOfValidator", astgen.StructType(
		astgen.Field("factor", astgen.Ident("int64"), ""),
	)))

	f.AddDecl(astgen.FuncDeclFull("Int64MultipleOfValidator",
		astgen.Params(astgen.Field("factor", astgen.Ident("int64"), "")),
		astgen.Params(astgen.Field("", astgen.QualExpr("validator", "Int64"), "")),
		astgen.Block(astgen.Return(astgen.CompositeLit(astgen.Ident("int64MultipleOfValidator"),
			astgen.KeyValue("factor", astgen.Ident("factor")),
		))),
	))

	f.AddDecl(astgen.MethodDecl("Description", "v", astgen.Ident("int64MultipleOfValidator"),
		astgen.Params(astgen.Field("_", astgen.QualExpr("context", "Context"), "")),
		astgen.Params(astgen.Field("", astgen.Ident("string"), "")),
		astgen.Block(astgen.Return(astgen.Call(
			astgen.QualExpr("fmt", "Sprintf"),
			astgen.Lit("value must be a multiple of %v"),
			astgen.Selector(astgen.Ident("v"), "factor"),
		))),
	))

	f.AddDecl(astgen.MethodDecl("MarkdownDescription", "v", astgen.Ident("int64MultipleOfValidator"),
		astgen.Params(astgen.Field("ctx", astgen.QualExpr("context", "Context"), "")),
		astgen.Params(astgen.Field("", astgen.Ident("string"), "")),
		astgen.Block(astgen.Return(astgen.Call(
			astgen.Selector(astgen.Ident("v"), "Description"),
			astgen.Ident("ctx"),
		))),
	))

	f.AddDecl(astgen.MethodDecl("ValidateInt64", "v", astgen.Ident("int64MultipleOfValidator"),
		astgen.Params(
			astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("req", astgen.QualExpr("validator", "Int64Request"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("validator", "Int64Response")), ""),
		),
		astgen.Params(),
		astgen.Block(
			astgen.If(
				astgen.Binary(
					astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ConfigValue"), "IsNull")),
					token.LOR,
					astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ConfigValue"), "IsUnknown")),
				),
				astgen.Return(),
			),
			astgen.AssignSingle(
				astgen.Ident("value"),
				astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ConfigValue"), "ValueInt64")),
			),
			astgen.If(
				astgen.Binary(
					astgen.Selector(astgen.Ident("v"), "factor"),
					token.EQL,
					astgen.IntLit(0),
				),
				astgen.Block(
					astgen.ExprStmt(astgen.Call(
						astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "AddAttributeError"),
						astgen.Selector(astgen.Ident("req"), "Path"),
						astgen.Lit("Invalid Validator Configuration"),
						astgen.Lit("multipleOf factor must not be zero"),
					)),
					astgen.Return(),
				),
			),
			astgen.If(
				astgen.Binary(
					astgen.Binary(astgen.Ident("value"), token.REM, astgen.Selector(astgen.Ident("v"), "factor")),
					token.NEQ,
					astgen.IntLit(0),
				),
				astgen.ExprStmt(astgen.Call(
					astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "AddAttributeError"),
					astgen.Selector(astgen.Ident("req"), "Path"),
					astgen.Lit("Invalid Value"),
					astgen.Call(
						astgen.QualExpr("fmt", "Sprintf"),
						astgen.Lit("value must be a multiple of %v, got %v"),
						astgen.Selector(astgen.Ident("v"), "factor"),
						astgen.Ident("value"),
					),
				)),
			),
		),
	))
}

// generateDiscriminatorValidator emits a custom object validator that checks
// the discriminator property value against a set of allowed variant keys.
func generateDiscriminatorValidator(f *astgen.File) {
	f.AddImport("context", "")
	f.AddImport("fmt", "")
	f.AddImport("strings", "")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/schema/validator", "validator")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/types", "types")

	f.AddComment("discriminatorValidator validates that an object's discriminator property matches one of the allowed variant keys.")
	f.AddDecl(astgen.TypeDecl("discriminatorValidator", astgen.StructType(
		astgen.Field("propertyName", astgen.Ident("string"), ""),
		astgen.Field("allowed", astgen.SliceType(astgen.Ident("string")), ""),
	)))

	f.AddDecl(astgen.FuncDeclFull("DiscriminatorValidator",
		astgen.Params(
			astgen.Field("propertyName", astgen.Ident("string"), ""),
			astgen.Field("allowed", astgen.Ellipsis(astgen.Ident("string")), ""),
		),
		astgen.Params(astgen.Field("", astgen.QualExpr("validator", "Object"), "")),
		astgen.Block(astgen.Return(astgen.CompositeLit(astgen.Ident("discriminatorValidator"),
			astgen.KeyValue("propertyName", astgen.Ident("propertyName")),
			astgen.KeyValue("allowed", astgen.Ident("allowed")),
		))),
	))

	f.AddDecl(astgen.MethodDecl("Description", "v", astgen.Ident("discriminatorValidator"),
		astgen.Params(astgen.Field("_", astgen.QualExpr("context", "Context"), "")),
		astgen.Params(astgen.Field("", astgen.Ident("string"), "")),
		astgen.Block(astgen.Return(astgen.Call(
			astgen.QualExpr("fmt", "Sprintf"),
			astgen.Lit("discriminator property %q must be one of [%s]"),
			astgen.Selector(astgen.Ident("v"), "propertyName"),
			astgen.Call(
				astgen.QualExpr("strings", "Join"),
				astgen.Selector(astgen.Ident("v"), "allowed"),
				astgen.Lit(", "),
			),
		))),
	))

	f.AddDecl(astgen.MethodDecl("MarkdownDescription", "v", astgen.Ident("discriminatorValidator"),
		astgen.Params(astgen.Field("ctx", astgen.QualExpr("context", "Context"), "")),
		astgen.Params(astgen.Field("", astgen.Ident("string"), "")),
		astgen.Block(astgen.Return(astgen.Call(
			astgen.Selector(astgen.Ident("v"), "Description"),
			astgen.Ident("ctx"),
		))),
	))

	f.AddDecl(astgen.MethodDecl("ValidateObject", "v", astgen.Ident("discriminatorValidator"),
		astgen.Params(
			astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("req", astgen.QualExpr("validator", "ObjectRequest"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("validator", "ObjectResponse")), ""),
		),
		astgen.Params(),
		astgen.Block(
			astgen.If(
				astgen.Binary(
					astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ConfigValue"), "IsNull")),
					token.LOR,
					astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ConfigValue"), "IsUnknown")),
				),
				astgen.Return(),
			),
			astgen.Assign(
				[]ast.Expr{astgen.Ident("attr"), astgen.Ident("ok")},
				[]ast.Expr{astgen.IndexExpr(
					astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ConfigValue"), "Attributes")),
					astgen.Selector(astgen.Ident("v"), "propertyName"),
				)},
			),
			astgen.If(
				astgen.Unary(token.NOT, astgen.Ident("ok")),
				astgen.Block(
					astgen.ExprStmt(astgen.Call(
						astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "AddAttributeError"),
						astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "Path"), "AtName"), astgen.Selector(astgen.Ident("v"), "propertyName")),
						astgen.Lit("Missing Discriminator"),
						astgen.Call(
							astgen.QualExpr("fmt", "Sprintf"),
							astgen.Lit("discriminator property %q is required"),
							astgen.Selector(astgen.Ident("v"), "propertyName"),
						),
					)),
					astgen.Return(),
				),
			),
			astgen.If(
				astgen.Binary(
					astgen.Call(astgen.Selector(astgen.Ident("attr"), "IsNull")),
					token.LOR,
					astgen.Call(astgen.Selector(astgen.Ident("attr"), "IsUnknown")),
				),
				astgen.Return(),
			),
			astgen.Assign(
				[]ast.Expr{astgen.Ident("str"), astgen.Ident("ok")},
				[]ast.Expr{astgen.TypeAssertExpr(astgen.Ident("attr"), astgen.QualExpr("types", "String"))},
			),
			astgen.If(
				astgen.Unary(token.NOT, astgen.Ident("ok")),
				astgen.Block(
					astgen.ExprStmt(astgen.Call(
						astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "AddAttributeError"),
						astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "Path"), "AtName"), astgen.Selector(astgen.Ident("v"), "propertyName")),
						astgen.Lit("Invalid Discriminator"),
						astgen.Lit("discriminator value must be a string"),
					)),
					astgen.Return(),
				),
			),
			astgen.AssignSingle(
				astgen.Ident("value"),
				astgen.Call(astgen.Selector(astgen.Ident("str"), "ValueString")),
			),
			astgen.RangeStmt(
				astgen.Ident("_"), astgen.Ident("allowed"), token.DEFINE,
				astgen.Selector(astgen.Ident("v"), "allowed"),
				astgen.Block(
					astgen.If(
						astgen.Binary(astgen.Ident("allowed"), token.EQL, astgen.Ident("value")),
						astgen.Return(),
					),
				),
			),
			astgen.ExprStmt(astgen.Call(
				astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "AddAttributeError"),
				astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "Path"), "AtName"), astgen.Selector(astgen.Ident("v"), "propertyName")),
				astgen.Lit("Invalid Discriminator"),
				astgen.Call(
					astgen.QualExpr("fmt", "Sprintf"),
					astgen.Lit("value %q is not one of the allowed discriminators: [%s]"),
					astgen.Ident("value"),
					astgen.Call(
						astgen.QualExpr("strings", "Join"),
						astgen.Selector(astgen.Ident("v"), "allowed"),
						astgen.Lit(", "),
					),
				),
			)),
		),
	))
}

// generateConditionalValidator emits a custom object validator that enforces
// dependent required fields (conditional required) and records the presence
// of conditional schemas for future expansion.
func generateConditionalValidator(f *astgen.File) {
	f.AddImport("context", "")
	f.AddImport("fmt", "")
	f.AddImport("strings", "")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/schema/validator", "validator")

	f.AddComment("conditionalValidator validates conditional schema constraints such as dependent required fields.")
	f.AddDecl(astgen.TypeDecl("conditionalValidator", astgen.StructType(
		astgen.Field("trigger", astgen.Ident("string"), ""),
		astgen.Field("required", astgen.SliceType(astgen.Ident("string")), ""),
	)))

	f.AddDecl(astgen.FuncDeclFull("ConditionalValidator",
		astgen.Params(
			astgen.Field("trigger", astgen.Ident("string"), ""),
			astgen.Field("required", astgen.Ellipsis(astgen.Ident("string")), ""),
		),
		astgen.Params(astgen.Field("", astgen.QualExpr("validator", "Object"), "")),
		astgen.Block(astgen.Return(astgen.CompositeLit(astgen.Ident("conditionalValidator"),
			astgen.KeyValue("trigger", astgen.Ident("trigger")),
			astgen.KeyValue("required", astgen.Ident("required")),
		))),
	))

	f.AddDecl(astgen.MethodDecl("Description", "v", astgen.Ident("conditionalValidator"),
		astgen.Params(astgen.Field("_", astgen.QualExpr("context", "Context"), "")),
		astgen.Params(astgen.Field("", astgen.Ident("string"), "")),
		astgen.Block(astgen.Return(astgen.Call(
			astgen.QualExpr("fmt", "Sprintf"),
			astgen.Lit("when %q is set, the following fields are required: [%s]"),
			astgen.Selector(astgen.Ident("v"), "trigger"),
			astgen.Call(
				astgen.QualExpr("strings", "Join"),
				astgen.Selector(astgen.Ident("v"), "required"),
				astgen.Lit(", "),
			),
		))),
	))

	f.AddDecl(astgen.MethodDecl("MarkdownDescription", "v", astgen.Ident("conditionalValidator"),
		astgen.Params(astgen.Field("ctx", astgen.QualExpr("context", "Context"), "")),
		astgen.Params(astgen.Field("", astgen.Ident("string"), "")),
		astgen.Block(astgen.Return(astgen.Call(
			astgen.Selector(astgen.Ident("v"), "Description"),
			astgen.Ident("ctx"),
		))),
	))

	f.AddDecl(astgen.MethodDecl("ValidateObject", "v", astgen.Ident("conditionalValidator"),
		astgen.Params(
			astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("req", astgen.QualExpr("validator", "ObjectRequest"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("validator", "ObjectResponse")), ""),
		),
		astgen.Params(),
		astgen.Block(
			astgen.If(
				astgen.Binary(
					astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ConfigValue"), "IsNull")),
					token.LOR,
					astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ConfigValue"), "IsUnknown")),
				),
				astgen.Return(),
			),
			astgen.AssignSingle(
				astgen.Ident("attrs"),
				astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ConfigValue"), "Attributes")),
			),
			astgen.Assign(
				[]ast.Expr{astgen.Ident("trigger"), astgen.Ident("ok")},
				[]ast.Expr{astgen.IndexExpr(astgen.Ident("attrs"), astgen.Selector(astgen.Ident("v"), "trigger"))},
			),
			astgen.If(
				astgen.Binary(
					astgen.Binary(
						astgen.Unary(token.NOT, astgen.Ident("ok")),
						token.LOR,
						astgen.Call(astgen.Selector(astgen.Ident("trigger"), "IsNull")),
					),
					token.LOR,
					astgen.Call(astgen.Selector(astgen.Ident("trigger"), "IsUnknown")),
				),
				astgen.Return(),
			),
			astgen.RangeStmt(
				astgen.Ident("_"), astgen.Ident("name"), token.DEFINE,
				astgen.Selector(astgen.Ident("v"), "required"),
				astgen.Block(
					astgen.Assign(
						[]ast.Expr{astgen.Ident("field"), astgen.Ident("ok")},
						[]ast.Expr{astgen.IndexExpr(astgen.Ident("attrs"), astgen.Ident("name"))},
					),
					astgen.If(
						astgen.Binary(
							astgen.Unary(token.NOT, astgen.Ident("ok")),
							token.LOR,
							astgen.Call(astgen.Selector(astgen.Ident("field"), "IsNull")),
						),
						astgen.ExprStmt(astgen.Call(
							astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "AddAttributeError"),
							astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "Path"), "AtName"), astgen.Ident("name")),
							astgen.Lit("Missing Conditional Field"),
							astgen.Call(
								astgen.QualExpr("fmt", "Sprintf"),
								astgen.Lit("field %q is required when %q is set"),
								astgen.Ident("name"),
								astgen.Selector(astgen.Ident("v"), "trigger"),
							),
						)),
					),
				),
			),
		),
	))
}

// generatePatternPropertiesValidator emits a custom map validator that
// checks that each map key matches at least one of the configured regular
// expressions. Value-side validation inferred from the pattern schema is not
// yet implemented.
func generatePatternPropertiesValidator(f *astgen.File) {
	f.AddImport("context", "")
	f.AddImport("fmt", "")
	f.AddImport("regexp", "")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/schema/validator", "validator")

	f.AddComment("patternPropertiesValidator validates that map keys matching configured patterns satisfy the associated value constraints.")
	f.AddDecl(astgen.TypeDecl("patternPropertiesValidator", astgen.StructType(
		astgen.Field("patterns", astgen.MapType(astgen.Ident("string"), astgen.StarExpr(astgen.QualExpr("regexp", "Regexp"))), ""),
	)))

	f.AddDecl(astgen.FuncDeclFull("PatternPropertiesValidator",
		astgen.Params(astgen.Field("patterns", astgen.MapType(astgen.Ident("string"), astgen.Ident("string")), "")),
		astgen.Params(astgen.Field("", astgen.QualExpr("validator", "Map"), "")),
		astgen.Block(
			astgen.AssignSingle(
				astgen.Ident("compiled"),
				astgen.Call(
					astgen.Ident("make"),
					astgen.MapType(astgen.Ident("string"), astgen.StarExpr(astgen.QualExpr("regexp", "Regexp"))),
					astgen.Call(astgen.Ident("len"), astgen.Ident("patterns")),
				),
			),
			astgen.RangeStmt(
				astgen.Ident("name"), astgen.Ident("expr"), token.DEFINE,
				astgen.Ident("patterns"),
				astgen.Block(
					astgen.AssignStmt(
						[]ast.Expr{astgen.IndexExpr(astgen.Ident("compiled"), astgen.Ident("name"))},
						[]ast.Expr{astgen.Call(astgen.QualExpr("regexp", "MustCompile"), astgen.Ident("expr"))},
						token.ASSIGN,
					),
				),
			),
			astgen.Return(astgen.CompositeLit(astgen.Ident("patternPropertiesValidator"),
				astgen.KeyValue("patterns", astgen.Ident("compiled")),
			)),
		),
	))

	f.AddDecl(astgen.MethodDecl("Description", "v", astgen.Ident("patternPropertiesValidator"),
		astgen.Params(astgen.Field("_", astgen.QualExpr("context", "Context"), "")),
		astgen.Params(astgen.Field("", astgen.Ident("string"), "")),
		astgen.Block(astgen.Return(astgen.Lit("map keys must match one of the configured patternProperties patterns"))),
	))

	f.AddDecl(astgen.MethodDecl("MarkdownDescription", "v", astgen.Ident("patternPropertiesValidator"),
		astgen.Params(astgen.Field("ctx", astgen.QualExpr("context", "Context"), "")),
		astgen.Params(astgen.Field("", astgen.Ident("string"), "")),
		astgen.Block(astgen.Return(astgen.Call(
			astgen.Selector(astgen.Ident("v"), "Description"),
			astgen.Ident("ctx"),
		))),
	))

	f.AddDecl(astgen.MethodDecl("ValidateMap", "v", astgen.Ident("patternPropertiesValidator"),
		astgen.Params(
			astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("req", astgen.QualExpr("validator", "MapRequest"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("validator", "MapResponse")), ""),
		),
		astgen.Params(),
		astgen.Block(
			astgen.If(
				astgen.Binary(
					astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ConfigValue"), "IsNull")),
					token.LOR,
					astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ConfigValue"), "IsUnknown")),
				),
				astgen.Return(),
			),
			astgen.RangeStmt(
				astgen.Ident("key"), nil, token.DEFINE,
				astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ConfigValue"), "Elements")),
				astgen.Block(
					astgen.AssignSingle(astgen.Ident("matched"), astgen.BoolLit(false)),
					astgen.RangeStmt(
						astgen.Ident("_"), astgen.Ident("re"), token.DEFINE,
						astgen.Selector(astgen.Ident("v"), "patterns"),
						astgen.Block(
							astgen.If(
								astgen.Call(astgen.Selector(astgen.Ident("re"), "MatchString"), astgen.Ident("key")),
								astgen.Block(
									astgen.AssignStmt(
										[]ast.Expr{astgen.Ident("matched")},
										[]ast.Expr{astgen.BoolLit(true)},
										token.ASSIGN,
									),
									astgen.Break(),
								),
							),
						),
					),
					astgen.If(
						astgen.Unary(token.NOT, astgen.Ident("matched")),
						astgen.ExprStmt(astgen.Call(
							astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "AddAttributeError"),
							astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "Path"), "AtMapKey"), astgen.Ident("key")),
							astgen.Lit("Invalid Map Key"),
							astgen.Call(
								astgen.QualExpr("fmt", "Sprintf"),
								astgen.Lit("key %q does not match any patternProperties pattern"),
								astgen.Ident("key"),
							),
						)),
					),
				),
			),
		),
	))
}

// AddValidators adds a Terraform-plugin-framework Validators field to elems when
// the attribute schema requires custom validators. kind is the validator
// interface name (String, Int64, Float64, Bool, List, Set, Map, Object).
func AddValidators(elems []ast.Expr, attr ir.AttributeIR, kind string) []ast.Expr {
	exprs := attributeValidatorExprs(attr.Schema, kind)
	if len(exprs) == 0 {
		return elems
	}
	return append(elems, astgen.KeyValueExpr(
		astgen.Ident("Validators"),
		astgen.CompositeLit(
			astgen.SliceType(astgen.QualExpr("validator", kind)),
			exprs...,
		),
	))
}

// attributeValidatorExprs returns the custom validator constructor calls that
// apply to an attribute of the given validator kind.
func attributeValidatorExprs(s ir.SchemaIR, kind string) []ast.Expr {
	switch kind {
	case "Float64":
		return Float64ValidatorExprs(s)
	case "Int64":
		return Int64ValidatorExprs(s)
	case "Object":
		return ObjectValidatorExprs(s)
	case "Map":
		return MapValidatorExprs(s)
	}
	return nil
}

// Float64ValidatorExprs returns custom validator expressions for float64
// exclusive bounds and multipleOf constraints.
//
// Non-finite bounds (NaN, ±Inf — legal YAML floats) are skipped rather than
// emitted: a validator built from a non-finite bound has no useful semantics
// and would render invalid Go, so the constraint is dropped (M-8).
func Float64ValidatorExprs(s ir.SchemaIR) []ast.Expr {
	var exprs []ast.Expr
	if s.ExclusiveMinimum != nil && isFiniteFloat(*s.ExclusiveMinimum) {
		exprs = append(exprs, astgen.Call(astgen.Ident("Float64ExclusiveMinimumValidator"), astgen.FloatLit(*s.ExclusiveMinimum)))
	}
	if s.ExclusiveMaximum != nil && isFiniteFloat(*s.ExclusiveMaximum) {
		exprs = append(exprs, astgen.Call(astgen.Ident("Float64ExclusiveMaximumValidator"), astgen.FloatLit(*s.ExclusiveMaximum)))
	}
	if s.MultipleOf != nil && isFiniteFloat(*s.MultipleOf) {
		exprs = append(exprs, astgen.Call(astgen.Ident("Float64MultipleOfValidator"), astgen.FloatLit(*s.MultipleOf)))
	}
	return exprs
}

// isFiniteFloat reports whether v is a finite float64 (not NaN or ±Inf).
func isFiniteFloat(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

// Int64ValidatorExprs returns custom validator expressions for int64
// exclusive bounds and multipleOf constraints.
func Int64ValidatorExprs(s ir.SchemaIR) []ast.Expr {
	var exprs []ast.Expr
	if s.ExclusiveMinimum != nil {
		validateIntBound("exclusive_minimum", *s.ExclusiveMinimum)
		exprs = append(exprs, astgen.Call(astgen.Ident("Int64ExclusiveMinimumValidator"), astgen.IntLit(int(*s.ExclusiveMinimum))))
	}
	if s.ExclusiveMaximum != nil {
		validateIntBound("exclusive_maximum", *s.ExclusiveMaximum)
		exprs = append(exprs, astgen.Call(astgen.Ident("Int64ExclusiveMaximumValidator"), astgen.IntLit(int(*s.ExclusiveMaximum))))
	}
	if s.MultipleOf != nil {
		validateIntBound("multiple_of", *s.MultipleOf)
		exprs = append(exprs, astgen.Call(astgen.Ident("Int64MultipleOfValidator"), astgen.IntLit(int(*s.MultipleOf))))
	}
	return exprs
}

// ObjectValidatorExprs returns custom validator expressions for object-level
// constraints such as discriminators and conditional required fields. The
// discriminator property name is snake_cased so it matches the merged
// attribute's name (transformer.ApplyDynamicUnion snake_cases it, D2).
func ObjectValidatorExprs(s ir.SchemaIR) []ast.Expr {
	var exprs []ast.Expr
	if s.Union != nil && s.Union.Discriminator != nil {
		keys := make([]string, 0, len(s.Union.Discriminator.Mapping))
		for k := range s.Union.Discriminator.Mapping {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		args := make([]ast.Expr, 0, 1+len(keys))
		args = append(args, astgen.Lit(transformer.ToSnakeCase(s.Union.Discriminator.PropertyName)))
		for _, k := range keys {
			args = append(args, astgen.Lit(k))
		}
		exprs = append(exprs, astgen.Call(astgen.Ident("DiscriminatorValidator"), args...))
	}
	if len(s.DependentRequired) > 0 {
		triggers := make([]string, 0, len(s.DependentRequired))
		for t := range s.DependentRequired {
			triggers = append(triggers, t)
		}
		sort.Strings(triggers)
		for _, trigger := range triggers {
			required := s.DependentRequired[trigger]
			sort.Strings(required)
			args := []ast.Expr{astgen.Lit(trigger)}
			for _, r := range required {
				args = append(args, astgen.Lit(r))
			}
			exprs = append(exprs, astgen.Call(astgen.Ident("ConditionalValidator"), args...))
		}
	}
	return exprs
}

// MapValidatorExprs returns custom validator expressions for map-level
// constraints such as patternProperties.
func MapValidatorExprs(s ir.SchemaIR) []ast.Expr {
	var exprs []ast.Expr
	if len(s.PatternProperties) > 0 {
		validatePatternProperties(s.PatternProperties)
		patterns := make([]ast.Expr, 0, len(s.PatternProperties))
		keys := make([]string, 0, len(s.PatternProperties))
		for k := range s.PatternProperties {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			patterns = append(patterns, astgen.KeyValueExpr(astgen.Lit(k), astgen.Lit(k)))
		}
		exprs = append(exprs, astgen.Call(
			astgen.Ident("PatternPropertiesValidator"),
			astgen.CompositeLit(
				astgen.MapType(astgen.Ident("string"), astgen.Ident("string")),
				patterns...,
			),
		))
	}
	return exprs
}

// validateIntBound ensures a numeric bound intended for an integer validator is
// an integral float, preventing silent truncation of fractional JSON Schema
// values.
func validateIntBound(name string, v float64) {
	if v != math.Trunc(v) {
		panic(fmt.Sprintf("non-integer bound %v for %s on integer schema would be silently truncated", v, name))
	}
}

// validatePatternProperties compiles each patternProperties regular expression
// at generation time so that invalid patterns are surfaced before provider
// initialization instead of panicking at runtime.
func validatePatternProperties(patterns map[string]*ir.SchemaIR) {
	for pattern := range patterns {
		if _, err := regexp.Compile(pattern); err != nil {
			panic(fmt.Sprintf("invalid patternProperties pattern %q: %v", pattern, err))
		}
	}
}
