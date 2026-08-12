package schema

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/token"
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// renderFile renders the generated mapper *ast.File to formatted source.
// providerImport is kept as a parameter so the call sites read as a full
// render invocation; every test renders the same import for now.
func renderFile(t *testing.T, resources []ir.ResourceIR, providerImport string) string { //nolint:unparam // providerImport is a documented constant across all tests
	t.Helper()
	f := GenerateValueMappersFile(resources, providerImport)
	var buf bytes.Buffer
	if err := format.Node(&buf, token.NewFileSet(), f); err != nil {
		t.Fatalf("format mapper AST: %v", err)
	}
	return buf.String()
}

// stringAttr returns a primitive string attribute. required is kept as a
// parameter so call sites read as explicit attribute construction; every test
// currently builds required attributes.
func stringAttr(name string, required bool) ir.AttributeIR { //nolint:unparam // required is a documented constant across all tests
	return ir.AttributeIR{Name: name, Required: required, Schema: ir.SchemaIR{Type: ir.TypeString}}
}

// resource returns a minimal ResourceIR with the given name and attributes.
func resource(name string, attrs ...ir.AttributeIR) ir.ResourceIR {
	return ir.ResourceIR{Name: name, Schema: ir.ObjectSchemaIR{Attributes: attrs}}
}

// TestGenerateValueMappersFile_Render covers the top-level generation path:
// package name, imports, per-resource Type/FromValue/ToValue functions, and the
// shared decode helpers.
func TestGenerateValueMappersFile_Render(t *testing.T) {
	r := resource("pet",
		stringAttr("name", true),
		ir.AttributeIR{Name: "age", Required: true, Schema: ir.SchemaIR{Type: ir.TypeInt}},
		ir.AttributeIR{Name: "weight", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeFloat}},
		ir.AttributeIR{Name: "happy", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeBool}},
	)
	got := renderFile(t, []ir.ResourceIR{r}, "example.com/t/internal/provider")

	for _, want := range []string{
		"package protocol",
		"provider \"example.com/t/internal/provider\"",
		"func PetModelType() tftypes.Type",
		"func PetModelFromValue(v tftypes.Value) (provider.PetModel, error)",
		"func PetModelToValue(m provider.PetModel) (tftypes.Value, error)",
		"func decodeString(v tftypes.Value, out *string) error",
		"func decodeStringPtr",
		"func decodeInt64(v tftypes.Value, out *int64) error",
		"func decodeInt64Ptr",
		"func decodeFloat64(v tftypes.Value, out *float64) error",
		"func decodeFloat64Ptr",
		"func decodeBool(v tftypes.Value, out *bool) error",
		"func decodeBoolPtr",
		"tftypes.Object",
		"AttributeTypes",
		"\"name\": tftypes.String",
		"\"age\": tftypes.Number",
		"\"weight\": tftypes.Number",
		"\"happy\": tftypes.Bool",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated mapper file missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestTftypesPrimitiveType asserts the tftypes type expression per primitive
// kind, including the fallback for unrepresentable kinds.
func TestTftypesPrimitiveType(t *testing.T) {
	cases := []struct {
		in   ir.PrimitiveType
		want string
	}{
		{in: ir.TypeString, want: "tftypes.String"},
		{in: ir.TypeInt, want: "tftypes.Number"},
		{in: ir.TypeFloat, want: "tftypes.Number"},
		{in: ir.TypeBool, want: "tftypes.Bool"},
		{in: ir.TypeDynamic, want: "tftypes.DynamicPseudoType"},
		// TypeNull (and unknown kinds) fall back to String.
		{in: ir.TypeNull, want: "tftypes.String"},
		{in: ir.PrimitiveType("bogus"), want: "tftypes.String"},
	}
	for _, tt := range cases {
		b, err := astgen.RenderExpr(tftypesPrimitiveType(tt.in))
		if err != nil {
			t.Fatalf("RenderExpr(%q): %v", tt.in, err)
		}
		if string(b) != tt.want {
			t.Errorf("tftypesPrimitiveType(%q) = %q, want %q", tt.in, string(b), tt.want)
		}
	}
}

// renderStmts renders a statement list inside a dummy function so substring
// assertions can inspect generated decode/encode bodies.
func renderStmts(t *testing.T, stmts []ast.Stmt) string {
	t.Helper()
	decl := &ast.FuncDecl{
		Name: ast.NewIdent("f"),
		Type: &ast.FuncType{},
		Body: &ast.BlockStmt{List: stmts},
	}
	f := &ast.File{Name: ast.NewIdent("provider"), Decls: []ast.Decl{decl}}
	var buf bytes.Buffer
	if err := format.Node(&buf, token.NewFileSet(), f); err != nil {
		t.Fatalf("format stmts: %v", err)
	}
	return buf.String()
}

// TestPrimitiveCollectionDecodeStmts_FallbackElem asserts the defensive
// fallback decoder (decodeString) is used for an unrepresentable element type.
func TestPrimitiveCollectionDecodeStmts_FallbackElem(t *testing.T) {
	stmts := primitiveCollectionDecodeStmts(ir.List, "x", "X", ir.PrimitiveType("bogus"), false)
	got := renderStmts(t, stmts)
	if !strings.Contains(got, "decodeString") {
		t.Errorf("expected decodeString fallback decoder, got:\n%s", got)
	}
}

// TestGenerateValueMappersFile_MultipleResources verifies each resource gets its
// own prefixed function set and that a resource with an empty schema still
// renders (no panic) with a top-level Type function.
func TestGenerateValueMappersFile_MultipleResources(t *testing.T) {
	r1 := resource("pet", stringAttr("name", true))
	r2 := resource("owner", stringAttr("email", true))
	got := renderFile(t, []ir.ResourceIR{r1, r2}, "example.com/t/internal/provider")

	for _, want := range []string{
		"func PetModelType()",
		"func OwnerModelType()",
		"func PetModelFromValue",
		"func OwnerModelFromValue",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated mapper file missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestGenerateValueMappersFile_RequiredPrimitive asserts required primitives
// decode via the plain decoder (no pointer) and encode with NewValue.
func TestGenerateValueMappersFile_RequiredPrimitive(t *testing.T) {
	r := resource("pet",
		stringAttr("name", true),
		ir.AttributeIR{Name: "count", Required: true, Schema: ir.SchemaIR{Type: ir.TypeInt}},
	)
	got := renderFile(t, []ir.ResourceIR{r}, "example.com/t/internal/provider")

	// Required decode uses decodeString(v, &m.Name) directly.
	if !strings.Contains(got, "decodeString(val, &m.Name)") {
		t.Errorf("required string should decode via decodeString(val, &m.Name), got:\n%s", got)
	}
	// Required encode writes the value (not a pointer) into the map.
	if !strings.Contains(got, "vals[\"name\"] = tftypes.NewValue(tftypes.String, m.Name)") {
		t.Errorf("required string should encode via NewValue(tftypes.String, m.Name), got:\n%s", got)
	}
	if !strings.Contains(got, "vals[\"count\"] = tftypes.NewValue(tftypes.Number, m.Count)") {
		t.Errorf("required int should encode via NewValue(tftypes.Number, m.Count), got:\n%s", got)
	}
}

// TestGenerateValueMappersFile_OptionalPrimitive asserts optional primitives
// decode via the Ptr helpers (nil-safe) and encode nil as tftypes.NewValue(type,
// nil).
func TestGenerateValueMappersFile_OptionalPrimitive(t *testing.T) {
	r := resource("pet",
		ir.AttributeIR{Name: "nickname", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		ir.AttributeIR{Name: "ratio", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeFloat}},
		ir.AttributeIR{Name: "active", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeBool}},
	)
	got := renderFile(t, []ir.ResourceIR{r}, "example.com/t/internal/provider")

	for _, want := range []string{
		"v, err := decodeStringPtr(val)",
		"v, err := decodeFloat64Ptr(val)",
		"v, err := decodeBoolPtr(val)",
		"m.Nickname = v",
		"m.Ratio = v",
		"m.Active = v",
		"if m.Nickname != nil",
		"vals[\"nickname\"] = tftypes.NewValue(tftypes.String, *m.Nickname)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated mapper missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestGenerateValueMappersFile_NestedObject asserts nested object attributes
// produce child Type/FromValue/ToValue functions and decode via childNameFromValue.
func TestGenerateValueMappersFile_NestedObject(t *testing.T) {
	r := resource("pet",
		ir.AttributeIR{
			Name:     "owner",
			Required: true,
			Schema: ir.SchemaIR{
				Attributes: []ir.AttributeIR{stringAttr("name", true)},
			},
		},
		ir.AttributeIR{
			Name:     "sponsor",
			Optional: true,
			Schema: ir.SchemaIR{
				Attributes: []ir.AttributeIR{stringAttr("email", true)},
			},
		},
	)
	got := renderFile(t, []ir.ResourceIR{r}, "example.com/t/internal/provider")

	for _, want := range []string{
		"func PetModelOwnerType() tftypes.Type",
		"func PetModelOwnerFromValue(v tftypes.Value) (provider.PetModelOwner, error)",
		"func PetModelOwnerToValue(m provider.PetModelOwner) (tftypes.Value, error)",
		"func PetModelSponsorType() tftypes.Type",
		"func PetModelSponsorFromValue",
		"nested, err := PetModelOwnerFromValue(val)",
		// ToValue: `nested`/`err` are declared once at function-body scope and
		// every object-like field assigns with =, so multiple object fields and
		// optional ones (whose assignment sits inside an if-block) all compile.
		"var nested tftypes.Value",
		"nested, err = PetModelOwnerToValue(m.Owner)",
		"nested, err = PetModelSponsorToValue(*m.Sponsor)",
		// The else branch reports missing required attributes for required
		// objects. The message is emitted as a %q format string with the
		// attribute name as an argument.
		"missing required attribute %q",
		"\"owner\"",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated mapper missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestGenerateValueMappersFile_Collections asserts list/set/map collection
// attribute handling for both primitive and object element types.
func TestGenerateValueMappersFile_Collections(t *testing.T) {
	r := resource("pet",
		ir.AttributeIR{
			Name:     "tags",
			Required: true,
			Schema: ir.SchemaIR{Collection: &ir.CollectionType{
				Kind:        ir.List,
				ElementType: ir.SchemaIR{Type: ir.TypeString},
			}},
		},
		ir.AttributeIR{
			Name:     "labels",
			Required: true,
			Schema: ir.SchemaIR{Collection: &ir.CollectionType{
				Kind:        ir.Set,
				ElementType: ir.SchemaIR{Type: ir.TypeString},
			}},
		},
		ir.AttributeIR{
			Name:     "settings",
			Optional: true,
			Schema: ir.SchemaIR{Collection: &ir.CollectionType{
				Kind:        ir.Map,
				ElementType: ir.SchemaIR{Type: ir.TypeInt},
			}},
		},
		ir.AttributeIR{
			Name:     "members",
			Required: true,
			Schema: ir.SchemaIR{Collection: &ir.CollectionType{
				Kind:        ir.List,
				ElementType: ir.SchemaIR{Attributes: []ir.AttributeIR{stringAttr("name", true)}},
			}},
		},
		ir.AttributeIR{
			Name:     "aliases",
			Optional: true,
			Schema: ir.SchemaIR{Collection: &ir.CollectionType{
				Kind:        ir.Map,
				ElementType: ir.SchemaIR{Attributes: []ir.AttributeIR{stringAttr("url", true)}},
			}},
		},
	)
	got := renderFile(t, []ir.ResourceIR{r}, "example.com/t/internal/provider")

	for _, want := range []string{
		// Primitive list.
		"tftypes.List{ElementType: tftypes.String}",
		"m.Tags = make([]string, len(elems))",
		// Primitive set.
		"tftypes.Set{ElementType: tftypes.String}",
		// Primitive map (int64 element).
		"tftypes.Map{ElementType: tftypes.Number}",
		"m.Settings = make(map[string]int64, len(elems))",
		"decodeInt64(ev, &tmp)",
		// Object-element list child type.
		"func PetModelMembersElemType() tftypes.Type",
		"func PetModelMembersElemFromValue",
		"m.Members = make([]provider.PetModelMembersElem, len(elems))",
		// Object-element map child type.
		"func PetModelAliasesMapElemType() tftypes.Type",
		"m.Aliases = make(map[string]provider.PetModelAliasesMapElem, len(elems))",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated mapper missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestGenerateValueMappersFile_Dynamic asserts dynamic-typed attributes route
// through the tftypes.DynamicPseudoType path on both encode and decode.
func TestGenerateValueMappersFile_Dynamic(t *testing.T) {
	r := resource("pet",
		ir.AttributeIR{Name: "metadata", Required: true, Schema: ir.SchemaIR{Type: ir.TypeDynamic}},
	)
	got := renderFile(t, []ir.ResourceIR{r}, "example.com/t/internal/provider")

	for _, want := range []string{
		"\"metadata\": tftypes.DynamicPseudoType",
		"m.Metadata = val",
		"if m.Metadata.IsNull()",
		"tftypes.NewValue(tftypes.DynamicPseudoType, nil)",
		"missing required attribute %q",
		"\"metadata\"",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated mapper missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestGenerateValueMappersFile_NullType asserts TypeNull (OpenAPI 3.1
// {"type":"null"}) is represented as a Dynamic tftypes.Value (M-23).
func TestGenerateValueMappersFile_NullType(t *testing.T) {
	r := resource("widget",
		ir.AttributeIR{Name: "payload", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeNull}},
	)
	got := renderFile(t, []ir.ResourceIR{r}, "example.com/t/internal/provider")

	for _, want := range []string{
		"\"payload\": tftypes.DynamicPseudoType",
		"m.Payload.IsNull()",
		"tftypes.NewValue(tftypes.DynamicPseudoType, nil)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated mapper missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestGenerateValueMappersFile_DynamicElementCollection asserts a List whose
// element type is dynamic decodes by copying the raw tftypes.Value elements (G4).
func TestGenerateValueMappersFile_DynamicElementCollection(t *testing.T) {
	r := resource("widget",
		ir.AttributeIR{
			Name:     "payloads",
			Optional: true,
			Schema: ir.SchemaIR{Collection: &ir.CollectionType{
				Kind:        ir.List,
				ElementType: ir.SchemaIR{Type: ir.TypeDynamic},
			}},
		},
		ir.AttributeIR{
			Name:     "sizes",
			Optional: true,
			Schema: ir.SchemaIR{Collection: &ir.CollectionType{
				Kind:        ir.Map,
				ElementType: ir.SchemaIR{Type: ir.TypeDynamic},
			}},
		},
	)
	got := renderFile(t, []ir.ResourceIR{r}, "example.com/t/internal/provider")

	for _, want := range []string{
		"m.Payloads = elems",
		"m.Sizes = elems",
		"[]tftypes.Value",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated mapper missing %q\ncontent:\n%s", want, got)
		}
	}
	if strings.Contains(got, "make([]string, len(elems))") {
		t.Errorf("dynamic-element collection must not decode to []string:\n%s", got)
	}
}

// TestGenerateValueMappersFile_NestedCollectionUnsupported asserts nested
// collections (array of array) surface a clear decode/encode error (M-21)
// rather than being silently dropped, and that a field following the nested
// collection is not emitted after the unconditional error return (which would
// be unreachable code).
func TestGenerateValueMappersFile_NestedCollectionUnsupported(t *testing.T) {
	r := resource("widget",
		ir.AttributeIR{
			Name:     "matrix",
			Optional: true,
			Schema: ir.SchemaIR{Collection: &ir.CollectionType{
				Kind: ir.List,
				ElementType: ir.SchemaIR{Collection: &ir.CollectionType{
					Kind:        ir.List,
					ElementType: ir.SchemaIR{Type: ir.TypeString},
				}},
			}},
		},
		// A field after the nested collection: the error return is terminal, so
		// this field must not be emitted after it.
		stringAttr("name", true),
	)
	got := renderFile(t, []ir.ResourceIR{r}, "example.com/t/internal/provider")

	for _, want := range []string{
		"decode nested collection for %s is not yet supported",
		"encode nested collection for %s is not yet supported",
		"\"matrix\"",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated mapper missing nested-collection diagnostic %q\ncontent:\n%s", want, got)
		}
	}
	// The trailing field must not appear after the terminal error return.
	if strings.Contains(got, `vals["name"]`) {
		t.Errorf("generated mapper emits unreachable code after the nested-collection error return\ncontent:\n%s", got)
	}
}

// TestGenerateValueMappersFile_NestedCollectionUnsupported_AfterField asserts a
// nested-collection field that follows a normal field still declares `vals`
// (the earlier field reads it) while the terminal error return leaves no
// unreachable code after it.
func TestGenerateValueMappersFile_NestedCollectionUnsupported_AfterField(t *testing.T) {
	r := resource("widget",
		stringAttr("name", true),
		ir.AttributeIR{
			Name:     "matrix",
			Optional: true,
			Schema: ir.SchemaIR{Collection: &ir.CollectionType{
				Kind: ir.List,
				ElementType: ir.SchemaIR{Collection: &ir.CollectionType{
					Kind:        ir.List,
					ElementType: ir.SchemaIR{Type: ir.TypeString},
				}},
			}},
		},
	)
	got := renderFile(t, []ir.ResourceIR{r}, "example.com/t/internal/provider")

	for _, want := range []string{
		"decode nested collection for %s is not yet supported",
		"encode nested collection for %s is not yet supported",
		// The earlier field reads `vals`, so it must still be declared.
		`vals["name"]`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated mapper missing %q\ncontent:\n%s", want, got)
		}
	}
	// The terminal error return must be the last statement of FromValue/ToValue:
	// no trailing `return m, nil` / `return tftypes.NewValue(...)` after it.
	if strings.Contains(got, "return m, nil\n}") {
		t.Errorf("generated mapper has unreachable trailing return after nested-collection error\ncontent:\n%s", got)
	}
}

// TestGenerateValueMappersFile_NameCollisions is the H-7 regression test in the
// package that derives the field names: colliding attributes (foo_bar / fooBar,
// owner_bar / ownerBar) must yield distinct type, FromValue, ToValue functions
// and distinct struct field references.
func TestGenerateValueMappersFile_NameCollisions(t *testing.T) {
	r := resource("thing",
		ir.AttributeIR{Name: "foo_bar", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		ir.AttributeIR{Name: "fooBar", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		ir.AttributeIR{Name: "owner_bar", Optional: true, Schema: ir.SchemaIR{Attributes: []ir.AttributeIR{stringAttr("x", true)}}},
		ir.AttributeIR{Name: "ownerBar", Optional: true, Schema: ir.SchemaIR{Attributes: []ir.AttributeIR{stringAttr("y", true)}}},
	)
	got := renderFile(t, []ir.ResourceIR{r}, "example.com/t/internal/provider")

	for _, want := range []string{
		"func ThingModelOwnerBarType() tftypes.Type",
		"func ThingModelOwnerBarFromValue",
		"func ThingModelOwnerBarToValue",
		"func ThingModelOwnerBar2Type() tftypes.Type",
		"func ThingModelOwnerBar2FromValue",
		"func ThingModelOwnerBar2ToValue",
		"m.FooBar",
		"m.FooBar2",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated mapper missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestBuildMapperNode_Recursion verifies buildMapperNode recursion through
// nested object attributes, object-element collections, and skipped attributes.
func TestBuildMapperNode_Recursion(t *testing.T) {
	s := ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{
		stringAttr("name", true),
		{Name: "owner", Optional: true, Schema: ir.SchemaIR{Attributes: []ir.AttributeIR{stringAttr("email", true)}}},
		{Name: "members", Required: true, Schema: ir.SchemaIR{Collection: &ir.CollectionType{
			Kind:        ir.Set,
			ElementType: ir.SchemaIR{Attributes: []ir.AttributeIR{stringAttr("id", true)}},
		}}},
	}}
	node := buildMapperNode("PetModel", s, "example.com/t/internal/provider")
	if node.TypeName != "PetModel" {
		t.Errorf("TypeName = %q, want PetModel", node.TypeName)
	}
	if node.Schema.Attributes[0].Name != "name" {
		t.Errorf("unexpected schema root attribute %q", node.Schema.Attributes[0].Name)
	}
	if len(node.Children) != 2 {
		t.Fatalf("expected 2 children (owner, members), got %d", len(node.Children))
	}
	if node.Children[0].TypeName != "PetModelOwner" {
		t.Errorf("child 0 TypeName = %q, want PetModelOwner", node.Children[0].TypeName)
	}
	if node.Children[1].TypeName != "PetModelMembersElem" {
		t.Errorf("child 1 TypeName = %q, want PetModelMembersElem", node.Children[1].TypeName)
	}
}

// TestMapperChildSchema covers the object-schema extraction and naming for
// object attributes and object-element collections.
func TestMapperChildSchema(t *testing.T) {
	scope := map[string]string{"owner": "Owner", "tags": "Tags", "settings": "Settings"}
	t.Run("object attribute", func(t *testing.T) {
		attr := ir.AttributeIR{Name: "owner", Schema: ir.SchemaIR{Attributes: []ir.AttributeIR{stringAttr("x", true)}}}
		s, name := MapperChildSchema(scope, "PetModel", attr)
		if s == nil {
			t.Fatal("expected non-nil child schema")
		}
		if name != "PetModelOwner" {
			t.Errorf("name = %q, want PetModelOwner", name)
		}
	})
	t.Run("list of objects", func(t *testing.T) {
		attr := ir.AttributeIR{Name: "tags", Schema: ir.SchemaIR{Collection: &ir.CollectionType{
			Kind:        ir.List,
			ElementType: ir.SchemaIR{Attributes: []ir.AttributeIR{stringAttr("v", true)}},
		}}}
		s, name := MapperChildSchema(scope, "PetModel", attr)
		if s == nil {
			t.Fatal("expected non-nil child schema")
		}
		if name != "PetModelTagsElem" {
			t.Errorf("name = %q, want PetModelTagsElem", name)
		}
	})
	t.Run("map of objects", func(t *testing.T) {
		attr := ir.AttributeIR{Name: "settings", Schema: ir.SchemaIR{Collection: &ir.CollectionType{
			Kind:        ir.Map,
			ElementType: ir.SchemaIR{Attributes: []ir.AttributeIR{stringAttr("v", true)}},
		}}}
		_, name := MapperChildSchema(scope, "PetModel", attr)
		if name != "PetModelSettingsMapElem" {
			t.Errorf("name = %q, want PetModelSettingsMapElem", name)
		}
	})
	t.Run("primitive is not a child", func(t *testing.T) {
		attr := ir.AttributeIR{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}}
		if s, _ := MapperChildSchema(scope, "PetModel", attr); s != nil {
			t.Errorf("primitive attribute must not produce a child schema, got %+v", s)
		}
	})
	t.Run("primitive collection is not a child", func(t *testing.T) {
		attr := ir.AttributeIR{Name: "names", Schema: ir.SchemaIR{Collection: &ir.CollectionType{
			Kind:        ir.List,
			ElementType: ir.SchemaIR{Type: ir.TypeString},
		}}}
		if s, _ := MapperChildSchema(scope, "PetModel", attr); s != nil {
			t.Errorf("primitive-element collection must not produce a child schema, got %+v", s)
		}
	})
}
