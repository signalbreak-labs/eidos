package schema

import (
	"bytes"
	"go/format"
	"go/token"
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// renderModel renders the generated model *ast.File to formatted source. Runs of
// whitespace are collapsed so struct-field column alignment from go/format does
// not affect substring assertions.
func renderModel(t *testing.T, r ir.ResourceIR) string {
	t.Helper()
	f := GenerateModelFile(r)
	var buf bytes.Buffer
	if err := format.Node(&buf, token.NewFileSet(), f); err != nil {
		t.Fatalf("format model AST: %v", err)
	}
	return strings.Join(strings.Fields(buf.String()), " ")
}

func TestResourceAPIModelName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "simple", in: "pet", want: "PetModel"},
		{name: "snake", in: "mycloud_pet", want: "MycloudPetModel"},
		{name: "already pascal", in: "PetStore", want: "PetStoreModel"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResourceAPIModelName(ir.ResourceIR{Name: tt.in}); got != tt.want {
				t.Errorf("ResourceAPIModelName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestGenerateModelFile_StructAndPrimitiveFields asserts the top-level struct,
// JSON tags, and required/optional primitive field types.
func TestGenerateModelFile_StructAndPrimitiveFields(t *testing.T) {
	r := resource("pet",
		ir.AttributeIR{Name: "name", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		ir.AttributeIR{Name: "age", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeInt}},
		ir.AttributeIR{Name: "weight", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeFloat}},
		ir.AttributeIR{Name: "active", Required: true, Schema: ir.SchemaIR{Type: ir.TypeBool}},
	)
	got := renderModel(t, r)

	for _, want := range []string{
		"package provider",
		"type PetModel struct {",
		// Required primitives are plain values; optional are pointers.
		"Name string `json:\"name\"`",
		"Age *int64 `json:\"age,omitempty\"`",
		"Weight *float64 `json:\"weight,omitempty\"`",
		"Active bool `json:\"active\"`",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated model missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestGenerateModelFile_NestedObjects asserts nested objects emit recursive
// struct types, with optional nested objects as pointers.
func TestGenerateModelFile_NestedObjects(t *testing.T) {
	r := resource("pet",
		ir.AttributeIR{
			Name:     "owner",
			Required: true,
			Schema:   ir.SchemaIR{Attributes: []ir.AttributeIR{stringAttr("name", true)}},
		},
		ir.AttributeIR{
			Name:     "sponsor",
			Optional: true,
			Schema:   ir.SchemaIR{Attributes: []ir.AttributeIR{stringAttr("email", true)}},
		},
	)
	got := renderModel(t, r)

	for _, want := range []string{
		"type PetModel struct {",
		"Owner PetModelOwner `json:\"owner\"`",
		"Sponsor *PetModelSponsor `json:\"sponsor,omitempty\"`",
		"type PetModelOwner struct {",
		"Name string `json:\"name\"`",
		"type PetModelSponsor struct {",
		"Email string `json:\"email\"`",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated model missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestGenerateModelFile_Collections asserts list/set/map model field types for
// primitive and object elements.
func TestGenerateModelFile_Collections(t *testing.T) {
	r := resource("pet",
		ir.AttributeIR{
			Name:     "tags",
			Required: true,
			Schema:   ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Type: ir.TypeString}}},
		},
		ir.AttributeIR{
			Name:     "labels",
			Optional: true,
			Schema:   ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.Set, ElementType: ir.SchemaIR{Type: ir.TypeBool}}},
		},
		ir.AttributeIR{
			Name:     "settings",
			Required: true,
			Schema:   ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.Map, ElementType: ir.SchemaIR{Type: ir.TypeInt}}},
		},
		ir.AttributeIR{
			Name:     "members",
			Required: true,
			Schema:   ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Attributes: []ir.AttributeIR{stringAttr("id", true)}}}},
		},
		ir.AttributeIR{
			Name:     "links",
			Required: true,
			Schema:   ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.Map, ElementType: ir.SchemaIR{Attributes: []ir.AttributeIR{stringAttr("url", true)}}}},
		},
	)
	got := renderModel(t, r)

	for _, want := range []string{
		"Tags []string `json:\"tags\"`",
		"Labels []bool `json:\"labels,omitempty\"`",
		"Settings map[string]int64 `json:\"settings\"`",
		"Members []PetModelMembersElem `json:\"members\"`",
		"type PetModelMembersElem struct {",
		"Id string `json:\"id\"`",
		"Links map[string]PetModelLinksMapElem `json:\"links\"`",
		"type PetModelLinksMapElem struct {",
		"Url string `json:\"url\"`",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated model missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestGenerateModelFile_DynamicAndNull asserts dynamic/null attributes map to a
// tftypes.Value field and pull in the tftypes import (M-23).
func TestGenerateModelFile_DynamicAndNull(t *testing.T) {
	r := resource("widget",
		ir.AttributeIR{Name: "metadata", Required: true, Schema: ir.SchemaIR{Type: ir.TypeDynamic}},
		ir.AttributeIR{Name: "payload", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeNull}},
		ir.AttributeIR{
			Name:     "blobs",
			Required: true,
			Schema:   ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Type: ir.TypeDynamic}}},
		},
	)
	got := renderModel(t, r)

	for _, want := range []string{
		"\"github.com/hashicorp/terraform-plugin-go/tftypes\"",
		"Metadata tftypes.Value `json:\"metadata\"`",
		"Payload tftypes.Value `json:\"payload,omitempty\"`",
		"Blobs []tftypes.Value `json:\"blobs\"`",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated model missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestGenerateModelFile_CollisionResolvedFieldNames asserts H-7 collision
// resolution is reflected in model struct fields (FooBar and FooBar2).
func TestGenerateModelFile_CollisionResolvedFieldNames(t *testing.T) {
	r := resource("thing",
		ir.AttributeIR{Name: "foo_bar", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		ir.AttributeIR{Name: "fooBar", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
	)
	got := renderModel(t, r)

	for _, want := range []string{
		"FooBar",
		"FooBar2",
		`json:"foo_bar,omitempty"`,
		`json:"fooBar,omitempty"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated model missing %q\ncontent:\n%s", want, got)
		}
	}
}

func TestModelJSONTag(t *testing.T) {
	tests := []struct {
		name     string
		required bool
		optional bool
		computed bool
		attrName string
		want     string
	}{
		{name: "required", required: true, attrName: "name", want: "name"},
		{name: "optional", optional: true, attrName: "name", want: "name,omitempty"},
		{name: "computed", computed: true, attrName: "count", want: "count,omitempty"},
		{name: "comma sanitized", required: true, attrName: "a,b", want: "a_b"},
		{name: "quote sanitized", optional: true, attrName: `he"llo`, want: "he_llo,omitempty"},
		{name: "backslash sanitized", optional: true, attrName: `a\b`, want: "a_b,omitempty"},
		{name: "plain passes through", optional: true, attrName: "display_name", want: "display_name,omitempty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attr := ir.AttributeIR{Name: tt.attrName, Required: tt.required, Optional: tt.optional, Computed: tt.computed}
			if got := ModelJSONTag(attr); got != tt.want {
				t.Errorf("ModelJSONTag(%+v) = %q, want %q", attr, got, tt.want)
			}
		})
	}
}

func TestSanitizeJSONTagKey(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "name", want: "name"},
		{in: "a,b", want: "a_b"},
		{in: `a"b`, want: "a_b"},
		{in: `a\b`, want: "a_b"},
		{in: `x,y"z\`, want: "x_y_z_"},
		{in: "already_underscored", want: "already_underscored"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := SanitizeJSONTagKey(tt.in); got != tt.want {
				t.Errorf("SanitizeJSONTagKey(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNestedTypeName(t *testing.T) {
	if got := nestedTypeName("PetModel", "Owner"); got != "PetModelOwner" {
		t.Errorf("nestedTypeName(PetModel, Owner) = %q, want PetModelOwner", got)
	}
}

// TestPlainPrimitiveType asserts the Go type expression per primitive kind,
// including the fallback for unrepresentable kinds.
func TestPlainPrimitiveType(t *testing.T) {
	cases := []struct {
		in   ir.PrimitiveType
		want string
	}{
		{in: ir.TypeString, want: "string"},
		{in: ir.TypeInt, want: "int64"},
		{in: ir.TypeFloat, want: "float64"},
		{in: ir.TypeBool, want: "bool"},
		{in: ir.TypeDynamic, want: "tftypes.Value"},
		{in: ir.TypeNull, want: "tftypes.Value"},
		{in: ir.PrimitiveType("bogus"), want: "string"},
	}
	for _, tt := range cases {
		b, err := astgen.RenderExpr(plainPrimitiveType(tt.in))
		if err != nil {
			t.Fatalf("RenderExpr(%q): %v", tt.in, err)
		}
		if string(b) != tt.want {
			t.Errorf("plainPrimitiveType(%q) = %q, want %q", tt.in, string(b), tt.want)
		}
	}
}

// TestPlainFieldType_Fallback asserts unknown/unsupported shapes fall back to a
// compilable *string type (M-21 nested collections).
func TestPlainFieldType_Fallback(t *testing.T) {
	f := astgen.NewFile("provider")
	scope := map[string]string{}
	attr := ir.AttributeIR{Name: "matrix", Schema: ir.SchemaIR{Collection: &ir.CollectionType{
		Kind: ir.List,
		ElementType: ir.SchemaIR{Collection: &ir.CollectionType{
			Kind:        ir.List,
			ElementType: ir.SchemaIR{Type: ir.TypeString},
		}},
	}}}
	got := plainFieldType(f, "WidgetModel", attr, scope)
	b, err := astgen.RenderExpr(got)
	if err != nil {
		t.Fatalf("RenderExpr: %v", err)
	}
	if string(b) != "*string" {
		t.Errorf("plainFieldType fallback = %q, want *string", string(b))
	}
}
