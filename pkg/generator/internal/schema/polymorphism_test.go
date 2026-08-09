package schema

import (
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// discriminatedUnionSchema returns a discriminated oneOf with a shared
// discriminator property and two variants carrying distinct attributes.
func discriminatedUnionSchema() ir.SchemaIR {
	return ir.SchemaIR{
		Name: "pet",
		Union: &ir.UnionType{
			Kind: ir.OneOf,
			Variants: []ir.SchemaIR{
				{
					Name: "cat",
					Attributes: []ir.AttributeIR{
						{Name: "animalType", Schema: ir.SchemaIR{Type: ir.TypeString}},
						{Name: "meow_volume", Schema: ir.SchemaIR{Type: ir.TypeInt}},
					},
				},
				{
					Name: "dog",
					Attributes: []ir.AttributeIR{
						{Name: "animalType", Schema: ir.SchemaIR{Type: ir.TypeString}},
						{Name: "bark_volume", Schema: ir.SchemaIR{Type: ir.TypeInt}},
					},
				},
			},
			Discriminator: &ir.DiscriminatorIR{
				PropertyName: "animalType",
				Mapping:      map[string]string{"cat": "CatVariant", "dog": "DogVariant"},
			},
		},
	}
}

func TestMergedDiscriminatedUnion(t *testing.T) {
	t.Run("non-union returns nil", func(t *testing.T) {
		if got := MergedDiscriminatedUnion(ir.SchemaIR{Type: ir.TypeString}); got != nil {
			t.Errorf("expected nil for non-union, got %+v", got)
		}
	})
	t.Run("union without discriminator returns nil", func(t *testing.T) {
		s := ir.SchemaIR{Union: &ir.UnionType{Kind: ir.OneOf, Variants: []ir.SchemaIR{{Type: ir.TypeString}}}}
		if got := MergedDiscriminatedUnion(s); got != nil {
			t.Errorf("expected nil for union without discriminator, got %+v", got)
		}
	})
	t.Run("merged object carries snake_cased attributes and discriminator first", func(t *testing.T) {
		merged := MergedDiscriminatedUnion(discriminatedUnionSchema())
		if merged == nil {
			t.Fatal("expected merged schema, got nil")
		}
		if len(merged.Attributes) != 3 {
			t.Fatalf("expected 3 merged attributes, got %d", len(merged.Attributes))
		}
		// Discriminator is prepended, remaining variant attributes snake_cased
		// and made optional.
		if merged.Attributes[0].Name != "animal_type" || !merged.Attributes[0].Required {
			t.Errorf("first attribute should be required discriminator animal_type, got %+v", merged.Attributes[0])
		}
		for _, a := range merged.Attributes[1:] {
			if a.Required {
				t.Errorf("variant attribute %q should not be required after merge", a.Name)
			}
			if !a.Optional {
				t.Errorf("variant attribute %q should be optional after merge", a.Name)
			}
		}
		names := map[string]bool{}
		for _, a := range merged.Attributes {
			names[a.Name] = true
		}
		for _, want := range []string{"animal_type", "meow_volume", "bark_volume"} {
			if !names[want] {
				t.Errorf("merged schema missing attribute %q (got %v)", want, names)
			}
		}
	})
	t.Run("conflicting attribute across variants returns nil", func(t *testing.T) {
		s := ir.SchemaIR{Union: &ir.UnionType{
			Kind: ir.OneOf,
			Variants: []ir.SchemaIR{
				{Attributes: []ir.AttributeIR{{Name: "animalType", Schema: ir.SchemaIR{Type: ir.TypeString}}, {Name: "shared", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
				{Attributes: []ir.AttributeIR{{Name: "animalType", Schema: ir.SchemaIR{Type: ir.TypeString}}, {Name: "shared", Schema: ir.SchemaIR{Type: ir.TypeInt}}}},
			},
			Discriminator: &ir.DiscriminatorIR{PropertyName: "animalType", Mapping: map[string]string{"a": "A"}},
		}}
		if got := MergedDiscriminatedUnion(s); got != nil {
			t.Errorf("expected nil for conflicting attributes, got %+v", got)
		}
	})
	t.Run("empty discriminator property name returns nil", func(t *testing.T) {
		s := ir.SchemaIR{Name: "pet", Union: &ir.UnionType{
			Kind:          ir.OneOf,
			Variants:      []ir.SchemaIR{{Attributes: []ir.AttributeIR{{Name: "x", Schema: ir.SchemaIR{Type: ir.TypeString}}}}},
			Discriminator: &ir.DiscriminatorIR{PropertyName: "  ", Mapping: map[string]string{"a": "A"}},
		}}
		if got := MergedDiscriminatedUnion(s); got != nil {
			t.Errorf("expected nil for empty discriminator property name, got %+v", got)
		}
	})
	t.Run("computed source marks merged attributes computed", func(t *testing.T) {
		s := discriminatedUnionSchema()
		s.Computed = true
		merged := MergedDiscriminatedUnion(s)
		if merged == nil {
			t.Fatal("expected merged schema, got nil")
		}
		for _, a := range merged.Attributes {
			if !a.Computed {
				t.Errorf("attribute %q should be Computed when source is Computed", a.Name)
			}
			if a.Required || a.Optional {
				t.Errorf("attribute %q should have Required/Optional cleared when Computed", a.Name)
			}
		}
	})
}

func TestDiscriminatedUnionValidators(t *testing.T) {
	s := discriminatedUnionSchema()
	expr := DiscriminatedUnionValidators(s)
	b, err := astgen.RenderExpr(expr)
	if err != nil {
		t.Fatalf("RenderExpr: %v", err)
	}
	for _, want := range []string{
		"Validators:",
		"[]validator.Object{",
		// Property name snake_cased and mapping keys sorted.
		`DiscriminatorValidator("animal_type", "cat", "dog")`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("rendered expr missing %q\ncontent:\n%s", want, string(b))
		}
	}
}

func TestDynamicUnionElement(t *testing.T) {
	union := ir.SchemaIR{Union: &ir.UnionType{Kind: ir.OneOf, Variants: []ir.SchemaIR{{Type: ir.TypeString}}}}
	if got := DynamicUnionElement(union); got.Type != ir.TypeDynamic {
		t.Errorf("union element should normalize to Dynamic, got type %q", got.Type)
	}
	plain := ir.SchemaIR{Type: ir.TypeString}
	if got := DynamicUnionElement(plain); got.Type != ir.TypeString {
		t.Errorf("non-union element should pass through, got type %q", got.Type)
	}
}

func TestObjectSchemaHasDiscriminatedUnion(t *testing.T) {
	t.Run("attribute union", func(t *testing.T) {
		s := ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{Name: "details", Schema: discriminatedUnionSchema()}}}
		if !ObjectSchemaHasDiscriminatedUnion(s) {
			t.Error("expected discriminated union detected on attribute")
		}
	})
	t.Run("union inside collection element", func(t *testing.T) {
		s := ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{Name: "pets", Schema: ir.SchemaIR{
			Collection: &ir.CollectionType{Kind: ir.List, ElementType: discriminatedUnionSchema()},
		}}}}
		if !schemaHasDiscriminatedUnion(s.Attributes[0].Schema) {
			t.Error("expected discriminated union detected inside collection element")
		}
	})
	t.Run("union nested inside attribute recursion", func(t *testing.T) {
		// A non-union outer schema whose attribute is itself a non-union object
		// containing a discriminated union attribute — exercises the
		// schemaHasDiscriminatedUnion attribute loop.
		s := ir.SchemaIR{Attributes: []ir.AttributeIR{{
			Name: "wrapper",
			Schema: ir.SchemaIR{Attributes: []ir.AttributeIR{
				{Name: "details", Schema: discriminatedUnionSchema()},
			}},
		}}}
		if !schemaHasDiscriminatedUnion(s) {
			t.Error("expected discriminated union detected through nested attribute")
		}
		if schemaHasDiscriminatedUnion(ir.SchemaIR{Type: ir.TypeString}) {
			t.Error("primitive schema must not be detected as discriminated")
		}
	})
	t.Run("block recursion", func(t *testing.T) {
		s := ir.ObjectSchemaIR{Blocks: []ir.BlockIR{{
			Name:   "details",
			Schema: ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{Name: "pet", Schema: discriminatedUnionSchema()}}},
		}}}
		if !ObjectSchemaHasDiscriminatedUnion(s) {
			t.Error("expected discriminated union detected inside block")
		}
	})
	t.Run("no union", func(t *testing.T) {
		s := ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{stringAttr("name", true)}}
		if ObjectSchemaHasDiscriminatedUnion(s) {
			t.Error("expected no discriminated union in plain schema")
		}
	})
	t.Run("non-discriminated union is not detected", func(t *testing.T) {
		s := ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{Name: "v", Schema: ir.SchemaIR{
			Union: &ir.UnionType{Kind: ir.OneOf, Variants: []ir.SchemaIR{{Type: ir.TypeString}}},
		}}}}
		if ObjectSchemaHasDiscriminatedUnion(s) {
			t.Error("union without discriminator must not be detected as discriminated")
		}
	})
}
