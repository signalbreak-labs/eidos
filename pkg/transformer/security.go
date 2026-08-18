package transformer

import (
	"fmt"
	"sort"
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// MapSecuritySchemeToProviderConfig maps a normalized OpenAPI security scheme to
// the provider-level configuration attributes a practitioner must set to
// authenticate. The mapping follows PROJECT_DESIGN.md Section 11.
//
// All generated attributes are Optional by default; required credentials are
// enforced at runtime by the generated client based on the configured scheme.
//
// allSchemes is the full normalized scheme list the provider exposes, so a
// bearer scheme can qualify its config attribute name when the spec declares
// several bearer schemes (see BearerTokenAttributeName) instead of collapsing
// them all onto "bearer_token".
//
// L-108: this function uses the canonical ir package types
// (ir.SecuritySchemeIR, ir.AttributeIR, ir.SchemaIR) rather than package-local
// shadow types, consistent with the rest of the transformer package.
func MapSecuritySchemeToProviderConfig(scheme ir.SecuritySchemeIR, allSchemes []ir.SecuritySchemeIR) ([]ir.AttributeIR, error) {
	switch scheme.Type {
	case ir.SecuritySchemeAPIKey:
		return mapAPIKey(scheme, allSchemes), nil
	case ir.SecuritySchemeHTTP:
		return mapHTTP(scheme, allSchemes)
	case ir.SecuritySchemeOAuth2:
		return mapOAuth2(scheme)
	case ir.SecuritySchemeOpenIDConnect:
		return mapOpenIDConnect(scheme), nil
	default:
		return nil, fmt.Errorf("unsupported security scheme type %q", scheme.Type)
	}
}

func mapAPIKey(scheme ir.SecuritySchemeIR, allSchemes []ir.SecuritySchemeIR) []ir.AttributeIR {
	loc := scheme.In
	if loc == "" {
		loc = "header"
	}
	name := scheme.NameField
	if name == "" {
		name = "X-API-Key"
	}

	desc := fmt.Sprintf("API key used for authentication. Sent in the %s as %q.", loc, name)
	if scheme.Description != "" {
		desc = fmt.Sprintf("%s Sent in the %s as %q.", scheme.Description, loc, name)
	}

	return []ir.AttributeIR{
		{
			Name:        APIKeyAttributeName(scheme, allSchemes),
			Description: desc,
			// N-49: AttributeIR is the single source of truth for the Optional/
			// Sensitive flags; the embedded SchemaIR carries only the type.
			Schema:    ir.SchemaIR{Type: ir.TypeString},
			Optional:  true,
			Sensitive: true,
		},
	}
}

// APIKeyAttributeName returns the provider-config attribute name for an API key
// security scheme. A spec that declares a single apiKey scheme maps to the
// canonical "api_key"; a spec that declares several apiKey schemes (e.g. one in
// a header and one in a query) qualifies each attribute with the scheme name so
// practitioners can set distinct keys and per-operation client.WithSchemes
// selection stays meaningful instead of collapsing every apiKey scheme onto one
// attribute (N-13).
func APIKeyAttributeName(scheme ir.SecuritySchemeIR, allSchemes []ir.SecuritySchemeIR) string {
	if apiKeySchemeCount(allSchemes) <= 1 {
		return "api_key"
	}
	name := strings.TrimSpace(scheme.Name)
	if name == "" {
		return "api_key"
	}
	return SanitizeAttributeName(name)
}

// apiKeySchemeCount counts the API key schemes among allSchemes.
func apiKeySchemeCount(schemes []ir.SecuritySchemeIR) int {
	n := 0
	for _, s := range schemes {
		if s.Type == ir.SecuritySchemeAPIKey {
			n++
		}
	}
	return n
}

func mapHTTP(scheme ir.SecuritySchemeIR, allSchemes []ir.SecuritySchemeIR) ([]ir.AttributeIR, error) {
	switch strings.ToLower(scheme.Scheme) {
	case "basic":
		return []ir.AttributeIR{
			stringAttr("username", "Username for HTTP basic authentication."),
			sensitiveStringAttr("password", "Password for HTTP basic authentication."),
		}, nil
	case "bearer":
		desc := "Bearer token used for HTTP bearer authentication."
		if scheme.BearerFormat != "" {
			desc = fmt.Sprintf("Bearer token used for HTTP bearer authentication. Expected format: %s.", scheme.BearerFormat)
		}
		return []ir.AttributeIR{sensitiveStringAttr(BearerTokenAttributeName(scheme, allSchemes), desc)}, nil
	default:
		return nil, fmt.Errorf("unsupported HTTP security scheme %q", scheme.Scheme)
	}
}

// BearerTokenAttributeName returns the provider-config attribute name for an
// HTTP bearer security scheme. A spec that declares a single bearer scheme maps
// to the canonical "bearer_token"; a spec that declares several bearer schemes
// (e.g. SpaceTraders' AccountToken and AgentToken) qualifies each attribute
// with the scheme name (account_token, agent_token) so practitioners can set
// distinct tokens and per-operation client.WithSchemes selection stays
// meaningful instead of collapsing every bearer scheme onto one attribute.
func BearerTokenAttributeName(scheme ir.SecuritySchemeIR, allSchemes []ir.SecuritySchemeIR) string {
	if bearerSchemeCount(allSchemes) <= 1 {
		return "bearer_token"
	}
	name := strings.TrimSpace(scheme.Name)
	if name == "" {
		return "bearer_token"
	}
	return SanitizeAttributeName(name)
}

// bearerSchemeCount counts the HTTP bearer schemes among allSchemes.
func bearerSchemeCount(schemes []ir.SecuritySchemeIR) int {
	n := 0
	for _, s := range schemes {
		if s.Type == ir.SecuritySchemeHTTP && strings.EqualFold(s.Scheme, "bearer") {
			n++
		}
	}
	return n
}

func mapOAuth2(scheme ir.SecuritySchemeIR) ([]ir.AttributeIR, error) {
	attrs := make(map[string]ir.AttributeIR)

	if scheme.Flows == nil {
		// No flows defined: emit the common client credentials set as a
		// reasonable default so the provider still has a configurable auth
		// surface.
		attrs["client_id"] = stringAttr("client_id", "Client ID for OAuth2 authentication.")
		attrs["client_secret"] = sensitiveStringAttr("client_secret", "Client secret for OAuth2 authentication.")
		return sortedAttrs(attrs), nil
	}

	// L-107: Flows present but every flow nil means the spec declares an OAuth2
	// scheme with no usable grant — surface an error instead of silently
	// returning an empty attribute list (no auth surface, no diagnostic).
	flows := scheme.Flows
	if flows.Implicit == nil && flows.Password == nil &&
		flows.ClientCredentials == nil && flows.AuthorizationCode == nil {
		return nil, fmt.Errorf("OAuth2 scheme %q declares flows but no grant flow is defined", scheme.Name)
	}

	if flows.ClientCredentials != nil {
		attrs["client_id"] = stringAttr("client_id", "Client ID for OAuth2 client credentials authentication.")
		attrs["client_secret"] = sensitiveStringAttr("client_secret", "Client secret for OAuth2 client credentials authentication.")
		attrs["token_url"] = stringAttr("token_url", "Token URL for OAuth2 client credentials authentication.")
		if len(flows.ClientCredentials.Scopes) > 0 {
			attrs["scopes"] = stringAttr("scopes", fmt.Sprintf("Space-separated OAuth2 scopes. Available scopes: %s.", scopeNames(flows.ClientCredentials.Scopes)))
		}
	}

	if flows.AuthorizationCode != nil {
		attrs["client_id"] = stringAttr("client_id", "Client ID for OAuth2 authorization code authentication.")
		attrs["client_secret"] = sensitiveStringAttr("client_secret", "Client secret for OAuth2 authorization code authentication.")
		attrs["auth_url"] = stringAttr("auth_url", "Authorization URL for OAuth2 authorization code authentication.")
		attrs["token_url"] = stringAttr("token_url", "Token URL for OAuth2 authorization code authentication.")
		attrs["refresh_token"] = sensitiveStringAttr("refresh_token", "Refresh token for OAuth2 authorization code authentication. The initial authorization-code exchange requires an interactive browser redirect and must happen out-of-band; the provider refreshes this token via the token URL.")
		if len(flows.AuthorizationCode.Scopes) > 0 {
			attrs["scopes"] = stringAttr("scopes", fmt.Sprintf("Space-separated OAuth2 scopes. Available scopes: %s.", scopeNames(flows.AuthorizationCode.Scopes)))
		}
	}

	if flows.Implicit != nil {
		attrs["auth_url"] = stringAttr("auth_url", "Authorization URL for OAuth2 implicit authentication.")
		if len(flows.Implicit.Scopes) > 0 {
			attrs["scopes"] = stringAttr("scopes", fmt.Sprintf("Space-separated OAuth2 scopes. Available scopes: %s.", scopeNames(flows.Implicit.Scopes)))
		}
	}

	if flows.Password != nil {
		attrs["username"] = stringAttr("username", "Username for OAuth2 password grant authentication.")
		attrs["password"] = sensitiveStringAttr("password", "Password for OAuth2 password grant authentication.")
		attrs["client_id"] = stringAttr("client_id", "Client ID for OAuth2 password grant authentication.")
		attrs["client_secret"] = sensitiveStringAttr("client_secret", "Client secret for OAuth2 password grant authentication.")
		attrs["token_url"] = stringAttr("token_url", "Token URL for OAuth2 password grant authentication.")
		if len(flows.Password.Scopes) > 0 {
			attrs["scopes"] = stringAttr("scopes", fmt.Sprintf("Space-separated OAuth2 scopes. Available scopes: %s.", scopeNames(flows.Password.Scopes)))
		}
	}

	return sortedAttrs(attrs), nil
}

func mapOpenIDConnect(scheme ir.SecuritySchemeIR) []ir.AttributeIR {
	// L-106: surface the spec's OpenID Connect discovery URL so practitioners do
	// not have to manually configure oidc_token_url when the spec already
	// provides it. The URL is carried in the attribute description (the provider
	// config surface does not support computed defaults here).
	desc := "OpenID Connect discovery/token URL."
	if scheme.OpenIDConnectURL != "" {
		desc = fmt.Sprintf("OpenID Connect discovery/token URL. Spec declares %q.", scheme.OpenIDConnectURL)
	}

	attrs := map[string]ir.AttributeIR{
		"oidc_token_url": stringAttr("oidc_token_url", desc),
		"client_id":      stringAttr("client_id", "Client ID for OpenID Connect authentication."),
		"client_secret":  sensitiveStringAttr("client_secret", "Client secret for OpenID Connect authentication."),
	}
	return sortedAttrs(attrs)
}

func stringAttr(name, description string) ir.AttributeIR {
	return ir.AttributeIR{
		Name:        name,
		Description: description,
		// N-49: AttributeIR carries the Optional flag; the embedded SchemaIR
		// carries only the type.
		Schema:   ir.SchemaIR{Type: ir.TypeString},
		Optional: true,
	}
}

func sensitiveStringAttr(name, description string) ir.AttributeIR {
	return ir.AttributeIR{
		Name:        name,
		Description: description,
		// N-49: AttributeIR carries the Optional/Sensitive flags; the embedded
		// SchemaIR carries only the type.
		Schema:    ir.SchemaIR{Type: ir.TypeString},
		Optional:  true,
		Sensitive: true,
	}
}

func scopeNames(scopes map[string]string) string {
	names := make([]string, 0, len(scopes))
	for name := range scopes {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func sortedAttrs(attrs map[string]ir.AttributeIR) []ir.AttributeIR {
	names := make([]string, 0, len(attrs))
	for name := range attrs {
		names = append(names, name)
	}
	sort.Strings(names)

	result := make([]ir.AttributeIR, 0, len(names))
	for _, name := range names {
		result = append(result, attrs[name])
	}
	return result
}
