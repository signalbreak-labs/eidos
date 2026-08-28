package ir

// SecurityIR aggregates the security schemes and global security requirements
// declared by the OpenAPI spec.
type SecurityIR struct {
	Schemes             []SecuritySchemeIR    `json:"schemes,omitempty"`
	DefaultRequirements []map[string][]string `json:"default_requirements,omitempty"`
}

// SecuritySchemeType enumerates the OpenAPI security scheme families.
type SecuritySchemeType string

const (
	// SecuritySchemeAPIKey represents an API key in a header, query, or cookie.
	SecuritySchemeAPIKey SecuritySchemeType = "apiKey"
	// SecuritySchemeHTTP represents HTTP basic, bearer, or other HTTP schemes.
	SecuritySchemeHTTP SecuritySchemeType = "http"
	// SecuritySchemeOAuth2 represents OAuth2 flows.
	SecuritySchemeOAuth2 SecuritySchemeType = "oauth2"
	// SecuritySchemeOpenIDConnect represents OpenID Connect Discovery.
	SecuritySchemeOpenIDConnect SecuritySchemeType = "openIdConnect"
)

// SecuritySchemeIR describes a single security scheme from the source spec.
type SecuritySchemeIR struct {
	Name             string             `json:"name"`
	Type             SecuritySchemeType `json:"type,omitempty"`
	Description      string             `json:"description,omitempty"`
	In               string             `json:"in,omitempty"`
	NameField        string             `json:"name_field,omitempty"`
	Scheme           string             `json:"scheme,omitempty"`
	BearerFormat     string             `json:"bearer_format,omitempty"`
	Flows            *OAuthFlowsIR      `json:"flows,omitempty"`
	OpenIDConnectURL string             `json:"open_id_connect_url,omitempty"`

	// The following fields carry generator.yaml `auth:` overrides applied by
	// transformer.ApplyAuthOverrides. They are populated only when the user
	// configures the auth section; the generated provider reads the runtime
	// fields above (NameField, Flows, OpenIDConnectURL), while the env-var hints
	// are preserved through the config round-trip so a user-customized env var
	// is not recomputed from the provider prefix on regeneration (M-5).
	EnvVar          string `json:"env_var,omitempty"`
	ClientIDEnv     string `json:"client_id_env,omitempty"`
	ClientSecretEnv string `json:"client_secret_env,omitempty"`
	// SelectedFlow names the OAuth2 flow the auth override selects. When set,
	// the generated provider wires that flow instead of the default priority
	// order (M-5).
	SelectedFlow string `json:"selected_flow,omitempty"`
}

// OAuthFlowsIR captures the OAuth2 flow metadata for an OAuth2 security scheme.
type OAuthFlowsIR struct {
	Implicit          *OAuthFlowIR `json:"implicit,omitempty"`
	Password          *OAuthFlowIR `json:"password,omitempty"`
	ClientCredentials *OAuthFlowIR `json:"client_credentials,omitempty"`
	AuthorizationCode *OAuthFlowIR `json:"authorization_code,omitempty"`
}

// OAuthFlowIR describes a single OAuth2 flow.
type OAuthFlowIR struct {
	AuthorizationURL string            `json:"authorization_url,omitempty"`
	TokenURL         string            `json:"token_url,omitempty"`
	RefreshURL       string            `json:"refresh_url,omitempty"`
	Scopes           map[string]string `json:"scopes,omitempty"`
}
