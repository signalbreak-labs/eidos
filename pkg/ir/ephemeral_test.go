package ir

import (
	"testing"
)

func TestEphemeralResourceIRRoundTrip(t *testing.T) {
	assertJSONRoundTrip(t, EphemeralResourceIR{
		Name:            "temporary_credential",
		FullName:        "Temporary Credential",
		TypeName:        "mycloud_temporary_credential",
		HasRenew:        true,
		HasClose:        true,
		Tags:            []string{"auth"},
		SourceOperation: "createTemporaryCredential",
		OpenMapping: OperationMappingIR{
			Method:       "POST",
			PathTemplate: "/credentials/temporary",
		},
		RenewMapping: &OperationMappingIR{
			Method:       "POST",
			PathTemplate: "/credentials/temporary/{id}/renew",
		},
		CloseMapping: &OperationMappingIR{
			Method:       "DELETE",
			PathTemplate: "/credentials/temporary/{id}",
		},
	})
}
