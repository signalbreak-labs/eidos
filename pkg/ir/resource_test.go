package ir

import (
	"testing"
	"time"
)

func TestResourceIRRoundTrip(t *testing.T) {
	assertJSONRoundTrip(t, ResourceIR{
		Name:               "pet",
		FullName:           "MyCloud Pet",
		TypeName:           "mycloud_pet",
		Description:        "A pet resource.",
		IDAttribute:        "id",
		ImportIDFormat:     "{petId}",
		Importable:         true,
		SensitiveAttrs:     []string{"api_key"},
		Tags:               []string{"pets"},
		SourceOperation:    "createPet",
		DeprecationMessage: "Use mycloud_pet_v2 instead.",
		Schema: ObjectSchemaIR{
			Attributes: []AttributeIR{
				{Name: "id", Schema: SchemaIR{Type: TypeString, Computed: true}},
				{Name: "name", Schema: SchemaIR{Type: TypeString, Required: true}},
				{Name: "tag", Schema: SchemaIR{Type: TypeString, Optional: true}},
			},
		},
		CRUDMapping: CRUDMappingIR{
			Create: OperationMappingIR{
				Method:       "POST",
				PathTemplate: "/pets",
				BodySchema:   &SchemaIR{Type: TypeString, Name: "CreatePetRequest"},
				ResponseSchema: &SchemaIR{
					Type: TypeString,
					Name: "CreatePetResponse",
				},
				SuccessCodes: []int{201},
				ErrorMappings: map[int]ErrorMappingIR{
					400: {StatusCode: 400, Description: "Invalid input"},
				},
			},
			Read: OperationMappingIR{
				Method:       "GET",
				PathTemplate: "/pets/{petId}",
				PathParams: []ParamIR{
					{Name: "petId", In: "path", Required: true, Schema: SchemaIR{Type: TypeString}},
				},
				SuccessCodes: []int{200},
			},
			Update: &OperationMappingIR{
				Method:       "PUT",
				PathTemplate: "/pets/{petId}",
				PathParams: []ParamIR{
					{Name: "petId", In: "path", Required: true, Schema: SchemaIR{Type: TypeString}},
				},
				SuccessCodes: []int{200},
			},
			Delete: OperationMappingIR{
				Method:       "DELETE",
				PathTemplate: "/pets/{petId}",
				PathParams: []ParamIR{
					{Name: "petId", In: "path", Required: true, Schema: SchemaIR{Type: TypeString}},
				},
				SuccessCodes: []int{204},
			},
		},
		Timeouts: &TimeoutConfigIR{
			Create: durationPtr(30 * time.Second),
			Read:   durationPtr(10 * time.Second),
			Update: durationPtr(30 * time.Second),
			Delete: durationPtr(10 * time.Second),
		},
	})

	assertJSONRoundTrip(t, ResourceIR{
		Name:     "minimal",
		TypeName: "mycloud_minimal",
	})

	assertJSONRoundTrip(t, ResourceIR{
		Name:     "minimal_timeouts",
		TypeName: "mycloud_minimal_timeouts",
		Timeouts: &TimeoutConfigIR{
			Create: durationPtr(2 * time.Minute),
		},
	})
}

func TestCRUDMappingIRRoundTrip(t *testing.T) {
	assertJSONRoundTrip(t, CRUDMappingIR{
		Create: OperationMappingIR{Method: "POST", PathTemplate: "/pets"},
		Read:   OperationMappingIR{Method: "GET", PathTemplate: "/pets/{id}"},
		Update: &OperationMappingIR{Method: "PUT", PathTemplate: "/pets/{id}"},
		Delete: OperationMappingIR{Method: "DELETE", PathTemplate: "/pets/{id}"},
		Import: &OperationMappingIR{Method: "GET", PathTemplate: "/pets/{id}"},
	})

	assertJSONRoundTrip(t, CRUDMappingIR{
		Create: OperationMappingIR{Method: "POST", PathTemplate: "/things"},
		Read:   OperationMappingIR{Method: "GET", PathTemplate: "/things/{id}"},
		Delete: OperationMappingIR{Method: "DELETE", PathTemplate: "/things/{id}"},
	})
}

func TestOperationMappingIRRoundTrip(t *testing.T) {
	assertJSONRoundTrip(t, OperationMappingIR{
		Method:       "POST",
		PathTemplate: "/pets/{petId}/photos",
		PathParams: []ParamIR{
			{Name: "petId", In: "path", Required: true, Schema: SchemaIR{Type: TypeString}},
		},
		QueryParams: []ParamIR{
			{Name: "limit", In: "query", Schema: SchemaIR{Type: TypeInt}},
		},
		HeaderParams: []ParamIR{
			{Name: "X-Request-ID", In: "header", Schema: SchemaIR{Type: TypeString}},
		},
		BodySchema: &SchemaIR{
			Type: TypeString,
			Name: "UploadPhotoRequest",
		},
		ResponseSchema: &SchemaIR{
			Type: TypeString,
			Name: "UploadPhotoResponse",
		},
		SuccessCodes: []int{200, 201},
		ErrorMappings: map[int]ErrorMappingIR{
			400: {
				StatusCode:  400,
				Description: "Bad request",
				Schema:      &SchemaIR{Type: TypeString, Name: "Error"},
			},
			500: {
				StatusCode:  500,
				Description: "Internal server error",
			},
		},
		ResponseIsCollection: true,
		NestedCollectionPath: "rules.*",
	})
}

func TestParamIRRoundTrip(t *testing.T) {
	assertJSONRoundTrip(t, ParamIR{
		Name:        "petId",
		In:          "path",
		Description: "The pet identifier.",
		Required:    true,
		Schema:      SchemaIR{Type: TypeString},
	})

	assertJSONRoundTrip(t, ParamIR{
		Name:       "filter",
		In:         "query",
		Schema:     SchemaIR{Type: TypeString},
		Deprecated: true,
	})
}

func TestErrorMappingIRRoundTrip(t *testing.T) {
	assertJSONRoundTrip(t, ErrorMappingIR{
		StatusCode:  404,
		Description: "Pet not found",
		Schema:      &SchemaIR{Type: TypeString, Name: "NotFoundError"},
	})

	assertJSONRoundTrip(t, ErrorMappingIR{
		StatusCode:  500,
		Description: "Internal error",
	})
}

func TestTimeoutConfigIRRoundTrip(t *testing.T) {
	assertJSONRoundTrip(t, TimeoutConfigIR{
		Create: durationPtr(30 * time.Second),
		Read:   durationPtr(10 * time.Second),
		Update: durationPtr(30 * time.Second),
		Delete: durationPtr(10 * time.Second),
	})

	assertJSONRoundTrip(t, TimeoutConfigIR{
		Create: durationPtr(0),
		Read:   durationPtr(5 * time.Minute),
	})
}

func durationPtr(d time.Duration) *time.Duration {
	return &d
}
