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
