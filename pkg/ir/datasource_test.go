package ir

import (
	"testing"
)

func TestDataSourceIRRoundTrip(t *testing.T) {
	assertJSONRoundTrip(t, DataSourceIR{
		Name:               "pet",
		FullName:           "MyCloud Pet",
		TypeName:           "mycloud_pet",
		Description:        "Fetches a single pet by ID.",
		Tags:               []string{"pets"},
		DeprecationMessage: "Use mycloud_pet_v2 instead.",
		Schema: ObjectSchemaIR{
			Attributes: []AttributeIR{
				{Name: "id", Schema: SchemaIR{Type: TypeString, Required: true}},
				{Name: "name", Schema: SchemaIR{Type: TypeString, Computed: true}},
				{Name: "tag", Schema: SchemaIR{Type: TypeString, Computed: true}},
			},
		},
		ReadMapping: OperationMappingIR{
			Method:       "GET",
			PathTemplate: "/pets/{petId}",
			PathParams: []ParamIR{
				{Name: "petId", In: "path", Required: true, Schema: SchemaIR{Type: TypeString}},
			},
			ResponseSchema: &SchemaIR{Type: TypeString, Name: "Pet"},
			SuccessCodes:   []int{200},
			ErrorMappings: map[int]ErrorMappingIR{
				404: {StatusCode: 404, Description: "Pet not found"},
			},
		},
	})

	assertJSONRoundTrip(t, DataSourceIR{
		Name:     "minimal",
		TypeName: "mycloud_minimal",
	})
}
