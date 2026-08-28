package ir

import (
	"testing"
	"time"
)

func TestProviderIRRoundTrip(t *testing.T) {
	assertJSONRoundTrip(t, ProviderIR{
		Name:              "mycloud",
		FullName:          "MyCloud",
		TypeName:          "mycloud",
		Version:           "1.0.0",
		Description:       "Generated provider for MyCloud.",
		SourceSpec:        "spec.yaml",
		SourceSpecVersion: "3.1.0",
		ConfigSchema: ObjectSchemaIR{
			Attributes: []AttributeIR{
				{
					Name:     "api_key",
					Schema:   SchemaIR{Type: TypeString, Required: true, Sensitive: true},
					Required: true,
				},
			},
		},
		Resources: []ResourceIR{
			{
				Name:     "pet",
				TypeName: "mycloud_pet",
				Schema: ObjectSchemaIR{
					Attributes: []AttributeIR{
						{Name: "id", Schema: SchemaIR{Type: TypeString, Computed: true}},
						{Name: "name", Schema: SchemaIR{Type: TypeString, Required: true}},
					},
				},
			},
		},
		DataSources: []DataSourceIR{
			{
				Name:     "pets",
				TypeName: "mycloud_pets",
			},
		},
		Actions: []ActionIR{
			{
				Name:     "reboot_server",
				TypeName: "mycloud_reboot_server",
			},
		},
		EphemeralResources: []EphemeralResourceIR{
			{
				Name:     "temporary_credential",
				TypeName: "mycloud_temporary_credential",
			},
		},
		ListResources: []ListResourceIR{
			{
				Name:     "things",
				TypeName: "mycloud_thing",
			},
		},
		Functions: []FunctionIR{
			{
				Name:     "concat_tags",
				TypeName: "concat_tags",
			},
		},
		ClientIR: ClientIR{
			BaseURLTemplate: "https://api.mycloud.example.com/v1",
			UserAgent:       "terraform-provider-mycloud/1.0.0",
			RetryMax:        3,
			RetryWaitMin:    time.Second,
			RetryWaitMax:    30 * time.Second,
			Timeout:         2 * time.Minute,
			Pagination: &PaginationIR{
				Style:        "offset",
				PageParam:    "page",
				PerPageParam: "per_page",
			},
		},
		SecurityIR: SecurityIR{
			Schemes: []SecuritySchemeIR{
				{
					Name:        "api_key",
					Type:        SecuritySchemeAPIKey,
					In:          "header",
					NameField:   "X-API-Key",
					Description: "API key authentication",
				},
			},
			DefaultRequirements: []map[string][]string{
				{"api_key": {}},
			},
		},
		Servers: []ServerIR{
			{
				URL:         "https://api.mycloud.example.com/v1",
				Description: "Production server",
				Variables: map[string]ServerVariableIR{
					"region": {
						Default:     "us-east-1",
						Enum:        []string{"us-east-1", "us-west-2"},
						Description: "Deployment region",
					},
				},
			},
		},
	})

	assertJSONRoundTrip(t, ProviderIR{
		Name: "minimal",
	})
}
