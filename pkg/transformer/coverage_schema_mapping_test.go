package transformer

import (
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// TestSchemaIRFromSpec_ObjectAndMap covers the object/map branches added to the
// shallow action mapper: an object body/property maps to nested attributes, an
// additionalProperties map maps to a Map collection, and a degenerate object
// (no properties, no additionalProperties) still degrades to Dynamic.
func TestSchemaIRFromSpec_ObjectAndMap(t *testing.T) {
	t.Run("object with properties maps to nested attributes", func(t *testing.T) {
		spec := SchemaSpec{
			Type:     "object",
			Required: []string{"name"},
			Properties: map[string]SchemaSpec{
				"name": {Type: "string"},
				"age":  {Type: "integer"},
			},
		}
		got := schemaIRFromSpec(spec)
		if len(got.Attributes) != 2 {
			t.Fatalf("expected 2 nested attributes, got %+v", got.Attributes)
		}
		// Sorted by name: age, name.
		if got.Attributes[0].Name != "age" || got.Attributes[1].Name != "name" {
			t.Errorf("nested attributes not sorted: %+v", got.Attributes)
		}
		if !got.Attributes[1].Required || got.Attributes[1].Optional {
			t.Errorf("required property must be Required: %+v", got.Attributes[1])
		}
		if got.Attributes[0].Required || !got.Attributes[0].Optional {
			t.Errorf("optional property must be Optional: %+v", got.Attributes[0])
		}
		if got.Attributes[1].WireName != "name" {
			t.Errorf("WireName = %q, want the original property name", got.Attributes[1].WireName)
		}
	})

	t.Run("empty type with properties maps to nested attributes", func(t *testing.T) {
		spec := SchemaSpec{
			Properties: map[string]SchemaSpec{"label": {Type: "string"}},
		}
		got := schemaIRFromSpec(spec)
		if len(got.Attributes) != 1 || got.Attributes[0].Name != "label" {
			t.Errorf("type-less object with properties = %+v, want one label attribute", got.Attributes)
		}
	})

	t.Run("additionalProperties maps to a Map collection", func(t *testing.T) {
		spec := SchemaSpec{
			Type:                 "object",
			AdditionalProperties: &SchemaSpec{Type: "string"},
		}
		got := schemaIRFromSpec(spec)
		if got.Collection == nil || got.Collection.Kind != ir.Map {
			t.Fatalf("additionalProperties object = %+v, want Map collection", got)
		}
		if got.Collection.ElementType.Type != ir.TypeString {
			t.Errorf("map element type = %q, want string", got.Collection.ElementType.Type)
		}
	})

	t.Run("degenerate object degrades to Dynamic", func(t *testing.T) {
		spec := SchemaSpec{Type: "object"}
		got := schemaIRFromSpec(spec)
		if got.Type != ir.TypeDynamic {
			t.Errorf("degenerate object = %+v, want Dynamic", got)
		}
	})
}

// TestRequestNestedAttributesFromSpec covers the nested-attribute builder for
// object-typed request-body properties: Required/Optional per the nested
// schema's required list, WireName preserved for the request-body key, and
// deterministic sort order.
func TestRequestNestedAttributesFromSpec(t *testing.T) {
	spec := SchemaSpec{
		Required: []string{"waypointSymbol"},
		Properties: map[string]SchemaSpec{
			"waypointSymbol": {Type: "string"},
			"units":          {Type: "integer"},
			"nested": {
				Type:     "object",
				Required: []string{"inner"},
				Properties: map[string]SchemaSpec{
					"inner": {Type: "string"},
					"outer": {Type: "boolean"},
				},
			},
		},
	}
	attrs := requestNestedAttributesFromSpec(spec)
	if len(attrs) != 3 {
		t.Fatalf("expected 3 nested attributes, got %+v", attrs)
	}
	// Sorted: nested, units, waypoint_symbol.
	if attrs[0].Name != "nested" || attrs[1].Name != "units" || attrs[2].Name != "waypoint_symbol" {
		t.Errorf("attributes not sorted: %+v", attrs)
	}
	if !attrs[2].Required || attrs[2].Optional {
		t.Errorf("waypoint_symbol must be Required: %+v", attrs[2])
	}
	if attrs[2].WireName != "waypointSymbol" {
		t.Errorf("WireName = %q, want waypointSymbol", attrs[2].WireName)
	}
	if attrs[1].Required || !attrs[1].Optional {
		t.Errorf("units must be Optional: %+v", attrs[1])
	}
	// Nested object recursion: inner Required, outer Optional.
	if len(attrs[0].Schema.Attributes) != 2 {
		t.Fatalf("nested object attributes = %+v, want 2", attrs[0].Schema.Attributes)
	}
	if !attrs[0].Schema.Attributes[0].Required {
		t.Errorf("nested inner must be Required: %+v", attrs[0].Schema.Attributes[0])
	}
}

// TestMarkResultSchemaComputed covers the recursive Computed flip for ephemeral
// result schemas: nested attributes and collection elements lose Required/
// Optional and become Computed so a Computed parent never carries Required
// children.
func TestMarkResultSchemaComputed(t *testing.T) {
	schema := &ir.SchemaIR{
		Attributes: []ir.AttributeIR{
			{
				Name:     "outer",
				Required: true,
				Schema: ir.SchemaIR{
					Attributes: []ir.AttributeIR{
						{Name: "inner", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
					},
				},
			},
			{
				Name:     "items",
				Optional: true,
				Schema: ir.SchemaIR{
					Collection: &ir.CollectionType{
						Kind: ir.List,
						ElementType: ir.SchemaIR{
							Attributes: []ir.AttributeIR{
								{Name: "elem", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
							},
						},
					},
				},
			},
		},
	}
	markResultSchemaComputed(schema)
	for _, a := range schema.Attributes {
		if !a.Computed || a.Required || a.Optional {
			t.Errorf("top-level attribute %q must be Computed only: %+v", a.Name, a)
		}
		for _, inner := range a.Schema.Attributes {
			if !inner.Computed || inner.Required || inner.Optional {
				t.Errorf("nested attribute %q must be Computed only: %+v", inner.Name, inner)
			}
		}
		if a.Schema.Collection != nil {
			for _, elem := range a.Schema.Collection.ElementType.Attributes {
				if !elem.Computed || elem.Required || elem.Optional {
					t.Errorf("collection element %q must be Computed only: %+v", elem.Name, elem)
				}
			}
		}
	}
}
