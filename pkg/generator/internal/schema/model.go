package schema

import (
	"fmt"
	"go/ast"
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/naming"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// ResourceAPIModelName returns the generated API model struct name for a
// resource.
func ResourceAPIModelName(r ir.ResourceIR) string {
	return naming.GoTypeName(r.Name) + "Model"
}

// GenerateModelFile builds the *ast.File for internal/provider/model_<name>.go.
func GenerateModelFile(r ir.ResourceIR) *ast.File {
	f := astgen.NewFile("provider")

	modelName := ResourceAPIModelName(r)
	generatePlainModelStruct(f, modelName, r.Schema)

	return f.AST()
}

// generatePlainModelStruct emits a plain Go struct for the given object schema
// and recursively emits nested struct types for object-like attributes. Nested
// type names are prefixed with parentTypeName and the (collision-resolved) field
// name to keep them unique within the generated file.
func generatePlainModelStruct(f *astgen.File, typeName string, s ir.ObjectSchemaIR) {
	f.AddCommentf("%s describes the API-facing shape for this resource.", typeName)

	scope := ResolveFieldNames(s.Attributes)
	fields := make([]*ast.Field, 0, len(s.Attributes))
	for _, attr := range s.Attributes {
		if SkipAttrForModel(attr) {
			continue
		}
		fieldType := plainFieldType(f, typeName, attr, scope)
		fields = append(fields, astgen.Field(
			resolvedFieldName(scope, attr),
			fieldType,
			fmt.Sprintf("json:%q", ModelJSONTag(attr)),
		))
	}

	f.AddDecl(astgen.TypeDecl(typeName, astgen.StructType(fields...)))
}

// plainFieldType maps an IR attribute to its plain Go model field type. When the
// field is object-like, the nested struct is emitted into f and the returned
// type references it (as a pointer for optional/computed fields). scope carries
// the collision-resolved field names for the enclosing struct so nested type
// names stay unique and consistent with value_mappers.go.
func plainFieldType(f *astgen.File, parentTypeName string, attr ir.AttributeIR, scope map[string]string) ast.Expr {
	s := attr.Schema
	field := resolvedFieldName(scope, attr)

	if s.Collection != nil {
		if t := collectionPlainFieldType(f, parentTypeName, field, attr); t != nil {
			return t
		}
	}

	if IsObjectLike(s) {
		nested := nestedTypeName(parentTypeName, field)
		generatePlainModelStruct(f, nested, ir.ObjectSchemaIR{Attributes: s.Attributes, Blocks: s.Blocks})
		if attr.Required {
			return astgen.Ident(nested)
		}
		return astgen.StarExpr(astgen.Ident(nested))
	}

	switch s.Type {
	case ir.TypeString:
		return requiredOrStar(attr, astgen.Ident("string"))
	case ir.TypeInt:
		return requiredOrStar(attr, astgen.Ident("int64"))
	case ir.TypeFloat:
		return requiredOrStar(attr, astgen.Ident("float64"))
	case ir.TypeBool:
		return requiredOrStar(attr, astgen.Ident("bool"))
	case ir.TypeDynamic, ir.TypeNull:
		// TypeNull (OpenAPI 3.1 {"type":"null"}) is represented in the model
		// as a Dynamic tftypes.Value, matching the framework attribute and
		// the mapper so a null-typed attribute round-trips instead of
		// silently degrading to *string (M-23).
		f.AddImport("github.com/hashicorp/terraform-plugin-go/tftypes", "")
		return astgen.QualExpr("tftypes", "Value")
	}

	// Fallback to string pointer to keep generated code compilable. This
	// includes nested collections (e.g. array of array), whose element is
	// itself a collection: the framework attribute path surfaces those as
	// ErrorFiles and the mapper surfaces a runtime error (M-21), so the
	// model field type only needs to compile.
	return astgen.StarExpr(astgen.Ident("string"))
}

// collectionPlainFieldType maps a collection-typed attribute to its plain Go
// model type: a slice (List/Set) or map (Map) of the element's type.
func collectionPlainFieldType(f *astgen.File, parentTypeName, field string, attr ir.AttributeIR) ast.Expr {
	elem := attr.Schema.Collection.ElementType
	switch attr.Schema.Collection.Kind {
	case ir.List, ir.Set:
		if IsPrimitiveSchema(elem) {
			if needsTftypes(elem) {
				f.AddImport("github.com/hashicorp/terraform-plugin-go/tftypes", "")
			}
			return astgen.SliceType(plainPrimitiveType(elem.Type))
		}
		if IsObjectLike(elem) {
			nested := nestedTypeName(parentTypeName, field) + "Elem"
			generatePlainModelStruct(f, nested, ir.ObjectSchemaIR{Attributes: elem.Attributes, Blocks: elem.Blocks})
			return astgen.SliceType(astgen.Ident(nested))
		}
	case ir.Map:
		if IsPrimitiveSchema(elem) {
			if needsTftypes(elem) {
				f.AddImport("github.com/hashicorp/terraform-plugin-go/tftypes", "")
			}
			return astgen.MapType(astgen.Ident("string"), plainPrimitiveType(elem.Type))
		}
		if IsObjectLike(elem) {
			nested := nestedTypeName(parentTypeName, field) + "MapElem"
			generatePlainModelStruct(f, nested, ir.ObjectSchemaIR{Attributes: elem.Attributes, Blocks: elem.Blocks})
			return astgen.MapType(astgen.Ident("string"), astgen.Ident(nested))
		}
	}
	return nil
}

// requiredOrStar returns t for a Required attribute, or a pointer to it for an
// optional/computed one.
func requiredOrStar(attr ir.AttributeIR, t ast.Expr) ast.Expr {
	if attr.Required {
		return t
	}
	return astgen.StarExpr(t)
}

// needsTftypes reports whether a schema maps to a tftypes.Value in the plain
// model (dynamic / null-typed values) and therefore requires the tftypes
// import to be present on the generated file.
func needsTftypes(s ir.SchemaIR) bool {
	return s.Type == ir.TypeDynamic || s.Type == ir.TypeNull
}

// plainPrimitiveType maps an IR primitive type to its plain Go type.
func plainPrimitiveType(t ir.PrimitiveType) ast.Expr {
	switch t {
	case ir.TypeString:
		return astgen.Ident("string")
	case ir.TypeInt:
		return astgen.Ident("int64")
	case ir.TypeFloat:
		return astgen.Ident("float64")
	case ir.TypeBool:
		return astgen.Ident("bool")
	case ir.TypeDynamic, ir.TypeNull:
		return astgen.QualExpr("tftypes", "Value")
	}
	return astgen.Ident("string")
}

// ModelJSONTag returns the JSON tag for an attribute. Required fields are emitted
// without omitempty; optional/computed fields use omitempty so they are omitted
// from API request bodies when nil.
//
// attr.Name is sanitized via SanitizeJSONTagKey because encoding/json splits the
// struct tag on the first comma and treats everything after it as options: a
// property name containing a comma (legal in JSON Schema, e.g. "a,b") would
// otherwise be silently parsed as key "a" with option "b" (L-49). Names without
// special characters pass through unchanged, so common specs are unaffected. A
// name with a comma cannot be faithfully round-tripped through a Go struct tag
// and would require a custom MarshalJSON/UnmarshalJSON; sanitizing keeps the
// emitted tag structurally valid and the behavior deterministic.
func ModelJSONTag(attr ir.AttributeIR) string {
	key := SanitizeJSONTagKey(attr.Name)
	if attr.Required {
		return key
	}
	return key + ",omitempty"
}

// SanitizeJSONTagKey returns name with characters that are special in a Go JSON
// struct tag replaced by underscores. encoding/json uses the first comma as the
// separator between the key and options, and double-quote/backslash would break
// the surrounding tag literal, so those are replaced. The name is returned
// unchanged when it contains none of these characters.
func SanitizeJSONTagKey(name string) string {
	if !strings.ContainsAny(name, `,"\`) {
		return name
	}
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch r {
		case ',', '"', '\\':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// nestedTypeName returns a nested type name by combining the parent model type
// name with the (collision-resolved) field name. The field argument is the
// already-resolved Go field name from ResolveFieldNames, so callers must pass
// the resolved name rather than the raw attribute name to keep nested type
// names unique and consistent across model_<name>.go and value_mappers.go.
func nestedTypeName(parentTypeName, fieldName string) string {
	return parentTypeName + fieldName
}
