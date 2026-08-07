package ir

import "testing"

func TestServerIRRoundTrip(t *testing.T) {
	assertJSONRoundTrip(t, ServerIR{
		URL:         "https://{region}.api.example.com",
		Description: "Regional server",
		Variables: map[string]ServerVariableIR{
			"region": {
				Default:     "us-east-1",
				Enum:        []string{"us-east-1", "us-west-2"},
				Description: "Deployment region",
			},
		},
	})
}
