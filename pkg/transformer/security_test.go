package transformer

import (
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// attrSignature captures the structural properties of a mapped provider config
// attribute. Generated descriptions are intentionally ignored in tests because
// they are implementation details of the mapping; the contract is the set of
// attributes, their types, optionality, and sensitivity.
type attrSignature struct {
	Name      string
	Type      ir.PrimitiveType
	Optional  bool
	Sensitive bool
}

func signAttr(a ir.AttributeIR) attrSignature {
	return attrSignature{
		Name:      a.Name,
		Type:      a.Schema.Type,
		Optional:  a.Optional,
		Sensitive: a.Sensitive,
	}
}

func signAttrs(attrs []ir.AttributeIR) []attrSignature {
	out := make([]attrSignature, len(attrs))
	for i, a := range attrs {
		out[i] = signAttr(a)
	}
	return out
}

func TestMapSecuritySchemeToProviderConfig(t *testing.T) {
	tests := []struct {
		name    string
		scheme  ir.SecuritySchemeIR
		want    []attrSignature
		wantErr bool
	}{
		{
			name: "apiKey header",
			scheme: ir.SecuritySchemeIR{
				Name:      "api_key",
				Type:      ir.SecuritySchemeAPIKey,
				In:        "header",
				NameField: "X-API-Key",
			},
			want: []attrSignature{
				{Name: "api_key", Type: ir.TypeString, Optional: true, Sensitive: true},
			},
		},
		{
			name: "apiKey query",
			scheme: ir.SecuritySchemeIR{
				Name:      "api_key",
				Type:      ir.SecuritySchemeAPIKey,
				In:        "query",
				NameField: "api_key",
			},
			want: []attrSignature{
				{Name: "api_key", Type: ir.TypeString, Optional: true, Sensitive: true},
			},
		},
		{
			name: "apiKey cookie",
			scheme: ir.SecuritySchemeIR{
				Name:      "api_key",
				Type:      ir.SecuritySchemeAPIKey,
				In:        "cookie",
				NameField: "api_key",
			},
			want: []attrSignature{
				{Name: "api_key", Type: ir.TypeString, Optional: true, Sensitive: true},
			},
		},
		{
			name: "apiKey default header name",
			scheme: ir.SecuritySchemeIR{
				Name: "api_key",
				Type: ir.SecuritySchemeAPIKey,
				In:   "header",
			},
			want: []attrSignature{
				{Name: "api_key", Type: ir.TypeString, Optional: true, Sensitive: true},
			},
		},
		{
			name: "HTTP basic",
			scheme: ir.SecuritySchemeIR{
				Name:   "basic_auth",
				Type:   ir.SecuritySchemeHTTP,
				Scheme: "basic",
			},
			want: []attrSignature{
				{Name: "username", Type: ir.TypeString, Optional: true, Sensitive: false},
				{Name: "password", Type: ir.TypeString, Optional: true, Sensitive: true},
			},
		},
		{
			name: "HTTP bearer",
			scheme: ir.SecuritySchemeIR{
				Name:         "bearer_auth",
				Type:         ir.SecuritySchemeHTTP,
				Scheme:       "bearer",
				BearerFormat: "JWT",
			},
			want: []attrSignature{
				{Name: "bearer_token", Type: ir.TypeString, Optional: true, Sensitive: true},
			},
		},
		{
			name: "OAuth2 client credentials",
			scheme: ir.SecuritySchemeIR{
				Name: "oauth2",
				Type: ir.SecuritySchemeOAuth2,
				Flows: &ir.OAuthFlowsIR{
					ClientCredentials: &ir.OAuthFlowIR{
						TokenURL: "https://example.com/oauth/token",
						Scopes: map[string]string{
							"read":  "Read access",
							"write": "Write access",
						},
					},
				},
			},
			want: []attrSignature{
				{Name: "client_id", Type: ir.TypeString, Optional: true, Sensitive: false},
				{Name: "client_secret", Type: ir.TypeString, Optional: true, Sensitive: true},
				{Name: "scopes", Type: ir.TypeString, Optional: true, Sensitive: false},
				{Name: "token_url", Type: ir.TypeString, Optional: true, Sensitive: false},
			},
		},
		{
			name: "OAuth2 authorization code",
			scheme: ir.SecuritySchemeIR{
				Name: "oauth2",
				Type: ir.SecuritySchemeOAuth2,
				Flows: &ir.OAuthFlowsIR{
					AuthorizationCode: &ir.OAuthFlowIR{
						AuthorizationURL: "https://example.com/oauth/authorize",
						TokenURL:         "https://example.com/oauth/token",
					},
				},
			},
			want: []attrSignature{
				{Name: "auth_url", Type: ir.TypeString, Optional: true, Sensitive: false},
				{Name: "client_id", Type: ir.TypeString, Optional: true, Sensitive: false},
				{Name: "client_secret", Type: ir.TypeString, Optional: true, Sensitive: true},
				{Name: "refresh_token", Type: ir.TypeString, Optional: true, Sensitive: true},
				{Name: "token_url", Type: ir.TypeString, Optional: true, Sensitive: false},
			},
		},
		{
			name: "OAuth2 implicit",
			scheme: ir.SecuritySchemeIR{
				Name: "oauth2",
				Type: ir.SecuritySchemeOAuth2,
				Flows: &ir.OAuthFlowsIR{
					Implicit: &ir.OAuthFlowIR{
						AuthorizationURL: "https://example.com/oauth/authorize",
						Scopes: map[string]string{
							"read": "Read access",
						},
					},
				},
			},
			want: []attrSignature{
				{Name: "auth_url", Type: ir.TypeString, Optional: true, Sensitive: false},
				{Name: "scopes", Type: ir.TypeString, Optional: true, Sensitive: false},
			},
		},
		{
			name: "OAuth2 password",
			scheme: ir.SecuritySchemeIR{
				Name: "oauth2",
				Type: ir.SecuritySchemeOAuth2,
				Flows: &ir.OAuthFlowsIR{
					Password: &ir.OAuthFlowIR{
						TokenURL: "https://example.com/oauth/token",
					},
				},
			},
			want: []attrSignature{
				{Name: "client_id", Type: ir.TypeString, Optional: true, Sensitive: false},
				{Name: "client_secret", Type: ir.TypeString, Optional: true, Sensitive: true},
				{Name: "password", Type: ir.TypeString, Optional: true, Sensitive: true},
				{Name: "token_url", Type: ir.TypeString, Optional: true, Sensitive: false},
				{Name: "username", Type: ir.TypeString, Optional: true, Sensitive: false},
			},
		},
		{
			name: "OAuth2 merges flows without duplicates",
			scheme: ir.SecuritySchemeIR{
				Name: "oauth2",
				Type: ir.SecuritySchemeOAuth2,
				Flows: &ir.OAuthFlowsIR{
					ClientCredentials: &ir.OAuthFlowIR{
						TokenURL: "https://example.com/oauth/token",
						Scopes: map[string]string{
							"read": "Read access",
						},
					},
					AuthorizationCode: &ir.OAuthFlowIR{
						AuthorizationURL: "https://example.com/oauth/authorize",
						TokenURL:         "https://example.com/oauth/token",
						Scopes: map[string]string{
							"write": "Write access",
						},
					},
				},
			},
			want: []attrSignature{
				{Name: "auth_url", Type: ir.TypeString, Optional: true, Sensitive: false},
				{Name: "client_id", Type: ir.TypeString, Optional: true, Sensitive: false},
				{Name: "client_secret", Type: ir.TypeString, Optional: true, Sensitive: true},
				{Name: "refresh_token", Type: ir.TypeString, Optional: true, Sensitive: true},
				{Name: "scopes", Type: ir.TypeString, Optional: true, Sensitive: false},
				{Name: "token_url", Type: ir.TypeString, Optional: true, Sensitive: false},
			},
		},
		{
			name: "OpenID Connect",
			scheme: ir.SecuritySchemeIR{
				Name:             "oidc",
				Type:             ir.SecuritySchemeOpenIDConnect,
				OpenIDConnectURL: "https://example.com/.well-known/openid-configuration",
			},
			want: []attrSignature{
				{Name: "client_id", Type: ir.TypeString, Optional: true, Sensitive: false},
				{Name: "client_secret", Type: ir.TypeString, Optional: true, Sensitive: true},
				{Name: "oidc_token_url", Type: ir.TypeString, Optional: true, Sensitive: false},
			},
		},
		{
			name: "unsupported scheme type",
			scheme: ir.SecuritySchemeIR{
				Name: "unknown",
				Type: ir.SecuritySchemeType("unknown"),
			},
			wantErr: true,
		},
		{
			name: "unsupported HTTP scheme",
			scheme: ir.SecuritySchemeIR{
				Name:   "digest",
				Type:   ir.SecuritySchemeHTTP,
				Scheme: "digest",
			},
			wantErr: true,
		},
		// L-107: OAuth2 with flows present but every grant nil must error rather
		// than silently returning an empty auth surface.
		{
			name: "OAuth2 all-nil flows errors",
			scheme: ir.SecuritySchemeIR{
				Name:  "oauth2",
				Type:  ir.SecuritySchemeOAuth2,
				Flows: &ir.OAuthFlowsIR{},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A single-scheme spec: the allSchemes list is just this scheme, so a
			// bearer scheme keeps the canonical "bearer_token" attribute name.
			got, err := MapSecuritySchemeToProviderConfig(tt.scheme, []ir.SecuritySchemeIR{tt.scheme})
			if (err != nil) != tt.wantErr {
				t.Fatalf("MapSecuritySchemeToProviderConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !slicesEqual(signAttrs(got), tt.want) {
				t.Errorf("MapSecuritySchemeToProviderConfig() = %+v, want %+v", signAttrs(got), tt.want)
			}
		})
	}
}

// TestBearerTokenAttributeName covers the scheme-qualification rule: a single
// bearer scheme keeps the canonical "bearer_token" attribute, while a spec with
// several bearer schemes qualifies each attribute with the scheme name so
// practitioners can set distinct tokens per scheme.
func TestBearerTokenAttributeName(t *testing.T) {
	bearer := func(name string) ir.SecuritySchemeIR {
		return ir.SecuritySchemeIR{Name: name, Type: ir.SecuritySchemeHTTP, Scheme: "bearer"}
	}

	single := []ir.SecuritySchemeIR{bearer("AccountToken")}
	if got := BearerTokenAttributeName(bearer("AccountToken"), single); got != "bearer_token" {
		t.Errorf("single bearer scheme: BearerTokenAttributeName = %q, want bearer_token", got)
	}

	multi := []ir.SecuritySchemeIR{bearer("AccountToken"), bearer("AgentToken")}
	if got := BearerTokenAttributeName(bearer("AccountToken"), multi); got != "account_token" {
		t.Errorf("multi-bearer: BearerTokenAttributeName(AccountToken) = %q, want account_token", got)
	}
	if got := BearerTokenAttributeName(bearer("AgentToken"), multi); got != "agent_token" {
		t.Errorf("multi-bearer: BearerTokenAttributeName(AgentToken) = %q, want agent_token", got)
	}

	// A non-bearer scheme in the list must not change the count.
	mixed := []ir.SecuritySchemeIR{bearer("AccountToken"), bearer("AgentToken"), {Name: "api_key", Type: ir.SecuritySchemeAPIKey, In: "header"}}
	if got := BearerTokenAttributeName(bearer("AccountToken"), mixed); got != "account_token" {
		t.Errorf("mixed schemes: BearerTokenAttributeName(AccountToken) = %q, want account_token", got)
	}

	// A multi-bearer spec with a nameless scheme falls back to the canonical name.
	nameless := []ir.SecuritySchemeIR{bearer(""), bearer("AgentToken")}
	if got := BearerTokenAttributeName(bearer(""), nameless); got != "bearer_token" {
		t.Errorf("nameless scheme: BearerTokenAttributeName = %q, want bearer_token", got)
	}
}

// TestMapSecuritySchemeToProviderConfig_MultiBearerQualifies asserts that two
// bearer schemes map to distinct, scheme-qualified config attributes rather than
// collapsing onto one bearer_token (the SpaceTraders AccountToken/AgentToken
// case).
func TestMapSecuritySchemeToProviderConfig_MultiBearerQualifies(t *testing.T) {
	schemes := []ir.SecuritySchemeIR{
		{Name: "AccountToken", Type: ir.SecuritySchemeHTTP, Scheme: "bearer"},
		{Name: "AgentToken", Type: ir.SecuritySchemeHTTP, Scheme: "bearer"},
	}
	attrs, err := MapSecuritySchemeToProviderConfig(schemes[0], schemes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(attrs) != 1 {
		t.Fatalf("expected 1 attribute, got %d: %+v", len(attrs), attrs)
	}
	if attrs[0].Name != "account_token" {
		t.Errorf("attribute name = %q, want account_token", attrs[0].Name)
	}
	if !attrs[0].Sensitive {
		t.Error("bearer token attribute must be sensitive")
	}
}

// TestMapOpenIDConnectSurfacesDiscoveryURL locks in the L-106 fix: when the
// spec declares an OpenIDConnectURL, it is carried into the oidc_token_url
// attribute description so practitioners do not have to look it up manually.
func TestMapOpenIDConnectSurfacesDiscoveryURL(t *testing.T) {
	scheme := ir.SecuritySchemeIR{
		Name:             "oidc",
		Type:             ir.SecuritySchemeOpenIDConnect,
		OpenIDConnectURL: "https://example.com/.well-known/openid-configuration",
	}
	got, err := MapSecuritySchemeToProviderConfig(scheme, []ir.SecuritySchemeIR{scheme})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var found bool
	for _, a := range got {
		if a.Name == "oidc_token_url" {
			found = true
			if !strings.Contains(a.Description, scheme.OpenIDConnectURL) {
				t.Errorf("oidc_token_url description = %q, want it to contain the discovery URL %q", a.Description, scheme.OpenIDConnectURL)
			}
		}
	}
	if !found {
		t.Fatalf("expected oidc_token_url attribute, got %v", got)
	}
}

func slicesEqual(a, b []attrSignature) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
