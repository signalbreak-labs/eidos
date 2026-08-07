package ir

import (
	"testing"
)

func TestFunctionIRRoundTrip(t *testing.T) {
	assertJSONRoundTrip(t, FunctionIR{
		Name:            "format_arn",
		FullName:        "Format ARN",
		TypeName:        "format_arn",
		Description:     "Formats an ARN from parts.",
		Variadic:        false,
		Tags:            []string{"utility"},
		SourceOperation: "formatArn",
		Arguments: []AttributeIR{
			{Name: "service", Schema: SchemaIR{Type: TypeString}},
			{Name: "resource", Schema: SchemaIR{Type: TypeString}},
		},
		ReturnType: SchemaIR{Type: TypeString},
	})
}
