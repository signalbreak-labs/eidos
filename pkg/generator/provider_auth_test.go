package generator

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/ir"
	"github.com/signalbreak-labs/eidos/pkg/transformer"
)

// renderAuthStmts renders the Configure statements produced by
// authConfigureStmts for the supplied schemes, wrapped in a synthetic
// function so go/format can print them. The config/opts/client references are
// intentionally undeclared: go/format checks syntax, not name resolution, so
// the rendered body is sufficient for substring assertions on the generated
// interceptor wiring.
func renderAuthStmts(t *testing.T, schemes []ir.SecuritySchemeIR) string {
	t.Helper()
	stmts, err := authConfigureStmts(schemes)
	if err != nil {
		t.Fatalf("authConfigureStmts() error = %v", err)
	}
	f := astgen.NewFile("provider")
	f.AddImport("example.com/provider/internal/client", "client")
	f.AddDecl(astgen.FuncDecl("Configure", astgen.Block(stmts...)))
	b, err := f.Render()
	if err != nil {
		t.Fatalf("render auth stmts: %v", err)
	}
	return string(b)
}

func TestAuthConfigureStmts_APIKeyHeader(t *testing.T) {
	got := renderAuthStmts(t, []ir.SecuritySchemeIR{{
		Name:      "api_key",
		Type:      ir.SecuritySchemeAPIKey,
		In:        "header",
		NameField: "X-API-Key",
	}})

	for _, want := range []string{
		`!config.ApiKey.IsNull()`,
		`!config.ApiKey.IsUnknown()`,
		`client.APIKeyAuth(config.ApiKey.ValueString(), "header", "X-API-Key")`,
		`opts = append(opts, client.WithSchemeInterceptor("api_key", `,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
}

func TestAuthConfigureStmts_APIKeyQueryDefaults(t *testing.T) {
	// An apiKey scheme with no in/name defaults to header / X-API-Key, matching
	// transformer.mapAPIKey.
	got := renderAuthStmts(t, []ir.SecuritySchemeIR{{
		Name: "api_key",
		Type: ir.SecuritySchemeAPIKey,
	}})
	if !strings.Contains(got, `"header"`) || !strings.Contains(got, `"X-API-Key"`) {
		t.Errorf("expected header/X-API-Key defaults in\n%s", got)
	}
}

func TestAuthConfigureStmts_APIKeyQueryCustom(t *testing.T) {
	got := renderAuthStmts(t, []ir.SecuritySchemeIR{{
		Name:      "api_key",
		Type:      ir.SecuritySchemeAPIKey,
		In:        "query",
		NameField: "token",
	}})
	if !strings.Contains(got, `client.APIKeyAuth(config.ApiKey.ValueString(), "query", "token")`) {
		t.Errorf("expected query/token apiKey wiring in\n%s", got)
	}
}

func TestAuthConfigureStmts_HTTPBasic(t *testing.T) {
	got := renderAuthStmts(t, []ir.SecuritySchemeIR{{
		Name:   "basic",
		Type:   ir.SecuritySchemeHTTP,
		Scheme: "basic",
	}})
	for _, want := range []string{
		`!config.Username.IsNull()`,
		`client.BasicAuth(config.Username.ValueString(), config.Password.ValueString())`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
}

func TestAuthConfigureStmts_HTTPBearer(t *testing.T) {
	got := renderAuthStmts(t, []ir.SecuritySchemeIR{{
		Name:   "bearer",
		Type:   ir.SecuritySchemeHTTP,
		Scheme: "bearer",
	}})
	for _, want := range []string{
		`!config.BearerToken.IsNull()`,
		`client.BearerAuth(config.BearerToken.ValueString())`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
}

func TestAuthConfigureStmts_OAuth2ClientCredentialsWithScopes(t *testing.T) {
	got := renderAuthStmts(t, []ir.SecuritySchemeIR{{
		Name: "oauth2",
		Type: ir.SecuritySchemeOAuth2,
		Flows: &ir.OAuthFlowsIR{
			ClientCredentials: &ir.OAuthFlowIR{
				TokenURL: "https://api.example.com/oauth/token",
				Scopes:   map[string]string{"read": "Read access"},
			},
		},
	}})

	for _, want := range []string{
		`!config.ClientId.IsNull()`,
		`tokenURL := "https://api.example.com/oauth/token"`,
		`!config.TokenUrl.IsNull()`,
		`tokenURL = config.TokenUrl.ValueString()`,
		`client.OAuth2ClientCredentials(tokenURL, config.ClientId.ValueString(), config.ClientSecret.ValueString(), client.ParseScopes(config.Scopes.ValueString()))`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
}

func TestAuthConfigureStmts_OAuth2ClientCredentialsNoScopes(t *testing.T) {
	// A client_credentials flow with no scopes passes nil for the scopes arg.
	got := renderAuthStmts(t, []ir.SecuritySchemeIR{{
		Name: "oauth2",
		Type: ir.SecuritySchemeOAuth2,
		Flows: &ir.OAuthFlowsIR{
			ClientCredentials: &ir.OAuthFlowIR{
				TokenURL: "https://api.example.com/oauth/token",
			},
		},
	}})
	if !strings.Contains(got, `client.OAuth2ClientCredentials(tokenURL, config.ClientId.ValueString(), config.ClientSecret.ValueString(), nil)`) {
		t.Errorf("expected nil scopes in\n%s", got)
	}
	if strings.Contains(got, "ParseScopes") {
		t.Errorf("ParseScopes should not appear when no scopes attr exists, in\n%s", got)
	}
}

// TestAuthConfigureStmts_OAuth2PasswordWired asserts the password grant is
// wired: Configure appends a client.OAuth2Password interceptor guarded on the
// username attribute, reading the token URL (spec default overridden by the
// token_url attribute), username, password, and client credentials from config.
func TestAuthConfigureStmts_OAuth2PasswordWired(t *testing.T) {
	got := renderAuthStmts(t, []ir.SecuritySchemeIR{{
		Name: "oauth2",
		Type: ir.SecuritySchemeOAuth2,
		Flows: &ir.OAuthFlowsIR{
			Password: &ir.OAuthFlowIR{
				TokenURL: "https://api.example.com/oauth/token",
				Scopes:   map[string]string{"read": "Read access"},
			},
		},
	}})

	for _, want := range []string{
		`!config.Username.IsNull()`,
		`tokenURL := "https://api.example.com/oauth/token"`,
		`!config.TokenUrl.IsNull()`,
		`tokenURL = config.TokenUrl.ValueString()`,
		`client.OAuth2Password(tokenURL, config.Username.ValueString(), config.Password.ValueString(), config.ClientId.ValueString(), config.ClientSecret.ValueString(), client.ParseScopes(config.Scopes.ValueString()))`,
		`opts = append(opts, client.WithSchemeInterceptor("oauth2", `,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
}

// TestAuthConfigureStmts_OAuth2AuthorizationCode_RefreshOnly asserts the
// authorization_code flow is wired through its non-interactive refresh path:
// Configure appends a client.OAuth2AuthorizationCodeRefresh interceptor guarded
// on the practitioner-supplied refresh_token attribute. The initial
// authorization-code exchange is interactive and stays out-of-band.
func TestAuthConfigureStmts_OAuth2AuthorizationCode_RefreshOnly(t *testing.T) {
	got := renderAuthStmts(t, []ir.SecuritySchemeIR{{
		Name: "oauth2",
		Type: ir.SecuritySchemeOAuth2,
		Flows: &ir.OAuthFlowsIR{
			AuthorizationCode: &ir.OAuthFlowIR{
				AuthorizationURL: "https://api.example.com/oauth/authorize",
				TokenURL:         "https://api.example.com/oauth/token",
			},
		},
	}})

	for _, want := range []string{
		`!config.RefreshToken.IsNull()`,
		`!config.RefreshToken.IsUnknown()`,
		`tokenURL := "https://api.example.com/oauth/token"`,
		`tokenURL = config.TokenUrl.ValueString()`,
		`client.OAuth2AuthorizationCodeRefresh(tokenURL, config.RefreshToken.ValueString(), config.ClientId.ValueString(), config.ClientSecret.ValueString(), nil)`,
		`opts = append(opts, client.WithSchemeInterceptor("oauth2", `,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "AddWarning") {
		t.Errorf("authorization_code flow is wired via refresh; no unsupported-scheme warning expected, got\n%s", got)
	}
}

// TestAuthConfigureStmts_OAuth2ImplicitNoInterceptor asserts the implicit flow
// stays fail-loud: it needs an interactive browser redirect and was removed
// from OAuth 2.1, so Configure emits a runtime AddWarning instead of
// an interceptor.
func TestAuthConfigureStmts_OAuth2ImplicitNoInterceptor(t *testing.T) {
	got := renderAuthStmts(t, []ir.SecuritySchemeIR{{
		Name: "oauth2",
		Type: ir.SecuritySchemeOAuth2,
		Flows: &ir.OAuthFlowsIR{
			Implicit: &ir.OAuthFlowIR{
				AuthorizationURL: "https://api.example.com/oauth/authorize",
			},
		},
	}})
	if strings.Contains(got, "WithSchemeInterceptor") {
		t.Errorf("implicit flow must not emit an interceptor, got\n%s", got)
	}
	if !strings.Contains(got, "AddWarning") {
		t.Errorf("implicit flow must emit an unsupported-scheme warning, got\n%s", got)
	}
	if !strings.Contains(got, `\"oauth2\"`) {
		t.Errorf("warning must name the scheme, got\n%s", got)
	}
}

// TestAuthConfigureStmts_OpenIDConnectWired asserts OpenID Connect is wired:
// Configure appends a client.OpenIDConnect interceptor guarded on client_id,
// carrying the spec's discovery URL and the oidc_token_url override (empty
// means discover at runtime).
func TestAuthConfigureStmts_OpenIDConnectWired(t *testing.T) {
	got := renderAuthStmts(t, []ir.SecuritySchemeIR{{
		Name:             "oidc",
		Type:             ir.SecuritySchemeOpenIDConnect,
		OpenIDConnectURL: "https://api.example.com/.well-known/openid-configuration",
	}})

	for _, want := range []string{
		`!config.ClientId.IsNull()`,
		`client.OpenIDConnect("https://api.example.com/.well-known/openid-configuration", config.OidcTokenUrl.ValueString(), config.ClientId.ValueString(), config.ClientSecret.ValueString(), nil)`,
		`opts = append(opts, client.WithSchemeInterceptor("oidc", `,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "AddWarning") {
		t.Errorf("OpenID Connect is wired; no unsupported-scheme warning expected, got\n%s", got)
	}
}

func TestAuthConfigureStmts_EmptyNoStatements(t *testing.T) {
	stmts, err := authConfigureStmts(nil)
	if err != nil {
		t.Fatalf("authConfigureStmts(nil) error = %v", err)
	}
	if stmts != nil {
		t.Errorf("expected nil stmts for no schemes, got %v", stmts)
	}
}

func TestAuthConfigureStmts_MultipleSchemes(t *testing.T) {
	// A spec declaring both an apiKey and HTTP bearer scheme wires both, in
	// declaration order.
	got := renderAuthStmts(t, []ir.SecuritySchemeIR{
		{Name: "api_key", Type: ir.SecuritySchemeAPIKey, In: "header", NameField: "X-API-Key"},
		{Name: "bearer", Type: ir.SecuritySchemeHTTP, Scheme: "bearer"},
	})
	if !strings.Contains(got, "client.APIKeyAuth(") {
		t.Errorf("missing apiKey interceptor in\n%s", got)
	}
	if !strings.Contains(got, "client.BearerAuth(") {
		t.Errorf("missing bearer interceptor in\n%s", got)
	}
	apiKeyIdx := strings.Index(got, "client.APIKeyAuth(")
	bearerIdx := strings.Index(got, "client.BearerAuth(")
	if apiKeyIdx < 0 || bearerIdx < 0 || apiKeyIdx > bearerIdx {
		t.Errorf("expected apiKey interceptor before bearer, got\n%s", got)
	}
}

// TestProviderAuthWiring_Compiles is the end-to-end compile check for §1.1: it
// builds a provider with a wired resource and every supported auth scheme, adds
// the auth config attributes exactly as buildIRPreview does (via
// transformer.MapSecuritySchemeToProviderConfig), generates a full provider
// module including the internal/client package (auth.go + ParseScopes), and
// compiles it. This proves the generated Configure reads the model fields that
// the schema declares and calls interceptors that the generated client defines.
func TestProviderAuthWiring_Compiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}

	pir := sampleProviderWithResourceIR()
	schemes := []ir.SecuritySchemeIR{
		{Name: "api_key", Type: ir.SecuritySchemeAPIKey, In: "header", NameField: "X-API-Key"},
		{Name: "bearer", Type: ir.SecuritySchemeHTTP, Scheme: "bearer"},
		{Name: "basic", Type: ir.SecuritySchemeHTTP, Scheme: "basic"},
		{
			Name: "oauth2",
			Type: ir.SecuritySchemeOAuth2,
			Flows: &ir.OAuthFlowsIR{
				ClientCredentials: &ir.OAuthFlowIR{
					TokenURL: "https://api.example.com/oauth/token",
					Scopes:   map[string]string{"read": "Read access"},
				},
			},
		},
	}
	pir.SecurityIR.Schemes = schemes

	// Mirror buildIRPreview: merge the auth config attributes the transformer
	// maps each scheme to, plus the endpoint attribute providers with managed
	// resources declare, so the generated model struct has every field
	// Configure reads.
	seen := make(map[string]struct{}, len(pir.ConfigSchema.Attributes))
	for _, a := range pir.ConfigSchema.Attributes {
		seen[a.Name] = struct{}{}
	}
	for _, s := range schemes {
		attrs, err := transformer.MapSecuritySchemeToProviderConfig(s, schemes)
		if err != nil {
			t.Fatalf("MapSecuritySchemeToProviderConfig(%s): %v", s.Name, err)
		}
		for _, a := range attrs {
			if _, dup := seen[a.Name]; dup {
				continue
			}
			seen[a.Name] = struct{}{}
			pir.ConfigSchema.Attributes = append(pir.ConfigSchema.Attributes, a)
		}
	}
	pir.ConfigSchema.Attributes = append(pir.ConfigSchema.Attributes, ir.AttributeIR{
		Name:     "endpoint",
		Optional: true,
		Schema:   ir.SchemaIR{Type: ir.TypeString},
	})

	tmp := generateResourceModule(t, pir)

	ctx, cancel := contextWithTimeout(t, 5*time.Minute)
	defer cancel()

	tidyCmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidyCmd.Dir = tmp
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\n%s", err, out)
	}

	buildCmd := exec.CommandContext(ctx, "go", "build", "./...")
	buildCmd.Dir = tmp
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./... failed for auth-wired provider: %v\n%s", err, out)
	}
}
