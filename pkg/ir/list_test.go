package ir

import (
	"testing"
)

func TestListResourceIRRoundTrip(t *testing.T) {
	assertJSONRoundTrip(t, ListResourceIR{
		Name:            "things",
		FullName:        "Things",
		TypeName:        "mycloud_thing",
		PaginationStyle: "offset",
		Tags:            []string{"query"},
		SourceOperation: "listThings",
		ConfigSchema: ObjectSchemaIR{
			Attributes: []AttributeIR{
				{Name: "filter", Schema: SchemaIR{Type: TypeString, Optional: true}},
			},
		},
		ListMapping: OperationMappingIR{
			Method:       "GET",
			PathTemplate: "/things",
		},
		IdentitySchema: ObjectSchemaIR{
			Attributes: []AttributeIR{
				{Name: "id", Schema: SchemaIR{Type: TypeString, Computed: true}},
			},
		},
		ResourceSchema: &ObjectSchemaIR{
			Attributes: []AttributeIR{
				{Name: "id", Schema: SchemaIR{Type: TypeString, Computed: true}},
				{Name: "name", Schema: SchemaIR{Type: TypeString, Computed: true}},
			},
		},
	})
}
