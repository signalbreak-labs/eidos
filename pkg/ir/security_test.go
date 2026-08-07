package ir

import "testing"

func TestSecurityIRRoundTrip(t *testing.T) {
	assertJSONRoundTrip(t, SecurityIR{
		Schemes: []SecuritySchemeIR{
			{
				Name:      "api_key",
				Type:      SecuritySchemeAPIKey,
				In:        "header",
				NameField: "X-API-Key",
			},
			{
				Name:         "bearer",
				Type:         SecuritySchemeHTTP,
				Scheme:       "bearer",
				BearerFormat: "JWT",
			},
			{
				Name: "oauth2",
				Type: SecuritySchemeOAuth2,
				Flows: &OAuthFlowsIR{
					ClientCredentials: &OAuthFlowIR{
						TokenURL: "https://auth.example.com/token",
						Scopes: map[string]string{
							"read":  "Read access",
							"write": "Write access",
						},
					},
				},
			},
			{
				Name:             "oidc",
				Type:             SecuritySchemeOpenIDConnect,
				OpenIDConnectURL: "https://auth.example.com/.well-known/openid-configuration",
			},
		},
		DefaultRequirements: []map[string][]string{
			{"api_key": {}},
			{"api_key": {}, "oauth2": {"read"}},
		},
	})
}
