package ir

import (
	"testing"
)

func TestActionIRRoundTrip(t *testing.T) {
	assertJSONRoundTrip(t, ActionIR{
		Name:             "reboot_server",
		FullName:         "Reboot Server",
		TypeName:         "mycloud_reboot_server",
		Description:      "Reboots a server.",
		ModifyPlan:       true,
		ProgressMessages: true,
		Tags:             []string{"server", "lifecycle"},
		SourceOperation:  "rebootServer",
		ConfigSchema: ObjectSchemaIR{
			Attributes: []AttributeIR{
				{Name: "server_id", Schema: SchemaIR{Type: TypeString, Required: true}},
				{Name: "force", Schema: SchemaIR{Type: TypeBool, Optional: true}},
			},
		},
		InvokeMapping: OperationMappingIR{
			Method:       "POST",
			PathTemplate: "/servers/{server_id}/reboot",
			PathParams: []ParamIR{
				{Name: "server_id", In: "path", Required: true, Schema: SchemaIR{Type: TypeString}},
			},
		},
	})
}
