package generator

import (
	"fmt"
	"go/ast"
	"go/token"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/naming"
	"github.com/signalbreak-labs/eidos/pkg/ir"
	"github.com/signalbreak-labs/eidos/pkg/transformer"
)

// This file builds the auth-wiring half of the generated provider Configure
// method. The generated internal/client package ships ready-made request
// interceptors (APIKeyAuth, BasicAuth, BearerAuth, OAuth2ClientCredentials) and
// the transformer maps each security scheme to the provider-config attributes a
// practitioner sets to authenticate (transformer.MapSecuritySchemeToProviderConfig).
// authConfigureStmts connects the two: it emits the AST that reads the decoded
// auth config fields and appends the matching client.WithSchemeInterceptor(...)
// option (keyed by the OpenAPI security scheme name) to the client options
// slice (opts) built by Configure.
//
// The config attribute names are not duplicated here: each scheme is run through
// transformer.MapSecuritySchemeToProviderConfig (the single source of truth) and
// the returned attribute names are translated to model field names via
// goFieldName, so a future change to the attribute-naming mapping propagates
// without a second edit site.
//
// Only scheme types with a generated interceptor are wired (apiKey, HTTP basic,
// HTTP bearer, OAuth2 client_credentials/password/authorization_code, OpenID
// Connect). The OAuth2 implicit flow has no generated interceptor — it requires
// an interactive browser redirect and is deprecated in OAuth 2.1 — and still
// surfaces its config attributes via the transformer mapping but contributes no
// interceptor here, staying fail-loud at runtime. The authorization_code flow
// is wired through its non-interactive refresh path only: the practitioner
// supplies a refresh_token obtained out-of-band and the provider refreshes it
// via the token URL.

// authConfigureStmts builds the AST statements that construct request
// interceptors from the decoded provider config and append them to the opts
// slice. It is emitted inside the Configure method's client-construction
// branch. Each interceptor is guarded on its primary credential being non-null
// and non-unknown so an Optional auth attribute that the practitioner left
// unset does not send an empty credential. Each interceptor is registered as a
// scheme interceptor keyed by its OpenAPI security scheme name, so a
// per-operation client.WithSchemes(...) request option can apply exactly that
// scheme (per-operation AND resolution, REMAINING_GAPS §1). Returns nil when no
// scheme yields a supported interceptor, so providers without authenticatable
// schemes emit no dead auth code.
func authConfigureStmts(schemes []ir.SecuritySchemeIR) ([]ast.Stmt, error) {
	var stmts []ast.Stmt
	for _, scheme := range schemes {
		schemeStmts, err := authSchemeStmts(scheme)
		if err != nil {
			return nil, err
		}
		if len(schemeStmts) == 0 {
			// The scheme exposes provider config attributes (emitted by the
			// transformer) but has no generated interceptor — the OAuth2 implicit
			// flow (interactive redirect, deprecated in OAuth 2.1) or the
			// degenerate no-flows OAuth2 surface. Surface a runtime warning so a
			// practitioner who configures the scheme's attributes is not silently
			// left unauthenticated (fail-loud, per the generation principles),
			// rather than emitting nothing and quietly sending unauthenticated
			// requests.
			stmts = append(stmts, unsupportedAuthSchemeWarning(scheme))
			continue
		}
		stmts = append(stmts, schemeStmts...)
	}
	return stmts, nil
}

// unsupportedAuthSchemeWarning returns the runtime Configure warning emitted
// for a security scheme that contributes no interceptor. The detail is baked in
// as a string literal at generation time (no runtime fmt dependency).
func unsupportedAuthSchemeWarning(scheme ir.SecuritySchemeIR) ast.Stmt {
	detail := fmt.Sprintf(
		"Security scheme %q (type %s) is declared in the OpenAPI spec and exposes provider config attributes, "+
			"but eidos has no generated interceptor for it, so requests will not be authenticated by this scheme. "+
			"Supported schemes: API key, HTTP basic, HTTP bearer, OAuth2 (client_credentials, password, "+
			"authorization_code via a practitioner-supplied refresh_token), and OpenID Connect. "+
			"The OAuth2 implicit flow is intentionally unsupported: it requires an interactive browser "+
			"redirect and is deprecated in OAuth 2.1.",
		scheme.Name, scheme.Type,
	)
	return addWarningStmt("Unsupported authentication scheme", astgen.Lit(detail))
}

// authSchemeStmts returns the Configure statements for a single security
// scheme. It returns nil (no error) when the scheme has no generated
// interceptor or lacks the config attributes needed to build one, so such
// schemes are silently skipped rather than producing partial wiring.
func authSchemeStmts(scheme ir.SecuritySchemeIR) ([]ast.Stmt, error) {
	attrs, err := transformer.MapSecuritySchemeToProviderConfig(scheme)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]ir.AttributeIR, len(attrs))
	for _, a := range attrs {
		byName[a.Name] = a
	}

	switch scheme.Type {
	case ir.SecuritySchemeAPIKey:
		return apiKeyStmts(scheme, byName), nil
	case ir.SecuritySchemeHTTP:
		return httpAuthStmts(scheme, byName), nil
	case ir.SecuritySchemeOAuth2:
		return oauth2Stmts(scheme, byName), nil
	case ir.SecuritySchemeOpenIDConnect:
		return openIDConnectStmts(scheme, byName), nil
	default:
		return nil, nil
	}
}

// apiKeyStmts emits the guarded append of a client.APIKeyAuth interceptor.
// The key location and header/query/cookie name default to header / X-API-Key
// when the spec omits them, matching transformer.mapAPIKey.
func apiKeyStmts(scheme ir.SecuritySchemeIR, byName map[string]ir.AttributeIR) []ast.Stmt {
	field, ok := authFieldName(byName, "api_key")
	if !ok {
		return nil
	}
	in := scheme.In
	if in == "" {
		in = "header"
	}
	name := scheme.NameField
	if name == "" {
		name = "X-API-Key"
	}
	interceptor := astgen.Call(
		astgen.QualExpr("client", "APIKeyAuth"),
		configFieldValueString(field),
		astgen.Lit(in),
		astgen.Lit(name),
	)
	return []ast.Stmt{astgen.If(configFieldNonNull(field), appendInterceptorOpt(scheme.Name, interceptor))}
}

// httpAuthStmts emits the guarded append of a BasicAuth or BearerAuth
// interceptor based on the HTTP scheme.
func httpAuthStmts(scheme ir.SecuritySchemeIR, byName map[string]ir.AttributeIR) []ast.Stmt {
	switch scheme.Scheme {
	case "basic":
		userField, okU := authFieldName(byName, "username")
		passField, okP := authFieldName(byName, "password")
		if !okU || !okP {
			return nil
		}
		interceptor := astgen.Call(
			astgen.QualExpr("client", "BasicAuth"),
			configFieldValueString(userField),
			configFieldValueString(passField),
		)
		return []ast.Stmt{astgen.If(configFieldNonNull(userField), appendInterceptorOpt(scheme.Name, interceptor))}
	case "bearer":
		field, ok := authFieldName(byName, "bearer_token")
		if !ok {
			return nil
		}
		interceptor := astgen.Call(
			astgen.QualExpr("client", "BearerAuth"),
			configFieldValueString(field),
		)
		return []ast.Stmt{astgen.If(configFieldNonNull(field), appendInterceptorOpt(scheme.Name, interceptor))}
	default:
		return nil
	}
}

// oauth2Stmts emits the guarded append of the OAuth2 interceptor matching the
// scheme's declared flow. When a scheme declares multiple flows the first in
// this priority order wins — client_credentials, password, authorization_code —
// so exactly one interceptor is registered per scheme (a second
// WithSchemeInterceptor for the same name would silently replace the first).
// The implicit flow has no generated interceptor: it requires an interactive
// browser redirect and is deprecated in OAuth 2.1, so it contributes nothing
// here and stays fail-loud via unsupportedAuthSchemeWarning.
func oauth2Stmts(scheme ir.SecuritySchemeIR, byName map[string]ir.AttributeIR) []ast.Stmt {
	if scheme.Flows == nil {
		return nil
	}
	switch {
	case scheme.Flows.ClientCredentials != nil:
		return oauth2ClientCredentialsStmts(scheme, byName)
	case scheme.Flows.Password != nil:
		return oauth2PasswordStmts(scheme, byName)
	case scheme.Flows.AuthorizationCode != nil:
		return oauth2AuthorizationCodeRefreshStmts(scheme, byName)
	default:
		return nil
	}
}

// oauth2ClientCredentialsStmts emits the guarded append of an
// OAuth2ClientCredentials interceptor.
func oauth2ClientCredentialsStmts(scheme ir.SecuritySchemeIR, byName map[string]ir.AttributeIR) []ast.Stmt {
	idField, okID := authFieldName(byName, "client_id")
	secretField, okS := authFieldName(byName, "client_secret")
	if !okID || !okS {
		return nil
	}

	tokenURLExpr, tokenURLStmts := oauth2TokenURLExpr(scheme.Flows.ClientCredentials.TokenURL, byName)
	interceptor := astgen.Call(
		astgen.QualExpr("client", "OAuth2ClientCredentials"),
		tokenURLExpr,
		configFieldValueString(idField),
		configFieldValueString(secretField),
		oauth2ScopesExpr(byName),
	)

	block := make([]ast.Stmt, 0, len(tokenURLStmts)+1)
	block = append(block, tokenURLStmts...)
	block = append(block, appendInterceptorOpt(scheme.Name, interceptor))
	return []ast.Stmt{astgen.If(configFieldNonNull(idField), block...)}
}

// oauth2PasswordStmts emits the guarded append of an OAuth2Password interceptor
// for the resource owner password credentials flow. The guard is on the
// username attribute: without it no token can be obtained.
func oauth2PasswordStmts(scheme ir.SecuritySchemeIR, byName map[string]ir.AttributeIR) []ast.Stmt {
	userField, okU := authFieldName(byName, "username")
	passField, okP := authFieldName(byName, "password")
	idField, okID := authFieldName(byName, "client_id")
	secretField, okS := authFieldName(byName, "client_secret")
	if !okU || !okP || !okID || !okS {
		return nil
	}

	tokenURLExpr, tokenURLStmts := oauth2TokenURLExpr(scheme.Flows.Password.TokenURL, byName)
	interceptor := astgen.Call(
		astgen.QualExpr("client", "OAuth2Password"),
		tokenURLExpr,
		configFieldValueString(userField),
		configFieldValueString(passField),
		configFieldValueString(idField),
		configFieldValueString(secretField),
		oauth2ScopesExpr(byName),
	)

	block := make([]ast.Stmt, 0, len(tokenURLStmts)+1)
	block = append(block, tokenURLStmts...)
	block = append(block, appendInterceptorOpt(scheme.Name, interceptor))
	return []ast.Stmt{astgen.If(configFieldNonNull(userField), block...)}
}

// oauth2AuthorizationCodeRefreshStmts emits the guarded append of an
// OAuth2AuthorizationCodeRefresh interceptor: the non-interactive refresh path
// of the authorization_code flow. The initial authorization-code exchange
// requires an interactive browser redirect and must happen out-of-band; the
// practitioner supplies the resulting refresh token via the refresh_token
// config attribute and the provider refreshes it via the token URL.
func oauth2AuthorizationCodeRefreshStmts(scheme ir.SecuritySchemeIR, byName map[string]ir.AttributeIR) []ast.Stmt {
	refreshField, okR := authFieldName(byName, "refresh_token")
	idField, okID := authFieldName(byName, "client_id")
	secretField, okS := authFieldName(byName, "client_secret")
	if !okR || !okID || !okS {
		return nil
	}

	tokenURLExpr, tokenURLStmts := oauth2TokenURLExpr(scheme.Flows.AuthorizationCode.TokenURL, byName)
	interceptor := astgen.Call(
		astgen.QualExpr("client", "OAuth2AuthorizationCodeRefresh"),
		tokenURLExpr,
		configFieldValueString(refreshField),
		configFieldValueString(idField),
		configFieldValueString(secretField),
		oauth2ScopesExpr(byName),
	)

	block := make([]ast.Stmt, 0, len(tokenURLStmts)+1)
	block = append(block, tokenURLStmts...)
	block = append(block, appendInterceptorOpt(scheme.Name, interceptor))
	return []ast.Stmt{astgen.If(configFieldNonNull(refreshField), block...)}
}

// openIDConnectStmts emits the guarded append of an OpenIDConnect interceptor.
// The generated client discovers the token endpoint from the spec's
// OpenIDConnectURL (baked in), unless the practitioner overrides it with the
// oidc_token_url config attribute; the token request itself uses the
// client_credentials grant with the configured client_id/client_secret.
func openIDConnectStmts(scheme ir.SecuritySchemeIR, byName map[string]ir.AttributeIR) []ast.Stmt {
	idField, okID := authFieldName(byName, "client_id")
	secretField, okS := authFieldName(byName, "client_secret")
	if !okID || !okS {
		return nil
	}

	// An unset oidc_token_url decodes to "", which tells the generated client
	// to discover the token endpoint from the baked-in discovery URL.
	tokenURLExpr := ast.Expr(astgen.Lit(""))
	if f, ok := authFieldName(byName, "oidc_token_url"); ok {
		tokenURLExpr = configFieldValueString(f)
	}
	interceptor := astgen.Call(
		astgen.QualExpr("client", "OpenIDConnect"),
		astgen.Lit(scheme.OpenIDConnectURL),
		tokenURLExpr,
		configFieldValueString(idField),
		configFieldValueString(secretField),
		astgen.Nil(),
	)
	return []ast.Stmt{astgen.If(configFieldNonNull(idField), appendInterceptorOpt(scheme.Name, interceptor))}
}

// oauth2TokenURLExpr returns the expression for an OAuth2 flow's token URL and
// any preceding statements needed to compute it. specTokenURL is the token URL
// the spec declares for the flow ("" when undeclared): when non-empty it is
// baked in as the default and a practitioner-configured token_url attribute
// overrides it at runtime; when the spec omits the token URL the configured
// token_url attribute is the sole source.
func oauth2TokenURLExpr(specTokenURL string, byName map[string]ir.AttributeIR) (ast.Expr, []ast.Stmt) {
	tokenURLField, hasTokenAttr := authFieldName(byName, "token_url")

	if specTokenURL != "" && hasTokenAttr {
		// Bake in the spec's token URL as the default, then let the configured
		// token_url attribute override it when set.
		stmts := []ast.Stmt{
			astgen.AssignSingle(astgen.Ident("tokenURL"), astgen.Lit(specTokenURL)),
			astgen.If(
				configFieldNonNull(tokenURLField),
				astgen.AssignStmt(
					[]ast.Expr{astgen.Ident("tokenURL")},
					[]ast.Expr{configFieldValueString(tokenURLField)},
					token.ASSIGN,
				),
			),
		}
		return astgen.Ident("tokenURL"), stmts
	}
	if specTokenURL != "" {
		return astgen.Lit(specTokenURL), nil
	}
	if hasTokenAttr {
		return configFieldValueString(tokenURLField), nil
	}
	// No token URL available from spec or config. mapOAuth2 always emits a
	// token_url attribute for the token-fetching flows, so this is unreachable
	// in practice; emit an empty literal so a token request fails loudly with a
	// clear client error rather than silently sending an unauthenticated request.
	return astgen.Lit(""), nil
}

// oauth2ScopesExpr returns the expression for the OAuth2 scopes argument. When
// the scheme exposes a scopes config attribute it is parsed from the
// space-separated practitioner input via client.ParseScopes; otherwise nil is
// passed (no scopes requested).
func oauth2ScopesExpr(byName map[string]ir.AttributeIR) ast.Expr {
	scopesField, ok := authFieldName(byName, "scopes")
	if !ok {
		return astgen.Nil()
	}
	return astgen.Call(
		astgen.QualExpr("client", "ParseScopes"),
		configFieldValueString(scopesField),
	)
}

// authFieldName resolves the Go model field name for a named auth config
// attribute produced by transformer.MapSecuritySchemeToProviderConfig. It
// returns ok=false when the attribute is absent, so callers can skip building
// an interceptor that would read a non-existent field.
func authFieldName(byName map[string]ir.AttributeIR, attr string) (string, bool) {
	a, ok := byName[attr]
	if !ok {
		return "", false
	}
	return naming.GoFieldName(a.Name), true
}

// configField returns the config.<field> selector expression.
func configField(field string) *ast.SelectorExpr {
	return astgen.Selector(astgen.Ident("config"), field)
}

// configFieldNonNull returns the guard expression !config.<field>.IsNull() &&
// !config.<field>.IsUnknown(), used to skip interceptors whose credential the
// practitioner left unset.
func configFieldNonNull(field string) ast.Expr {
	return astgen.Binary(
		astgen.Unary(token.NOT, astgen.Call(astgen.Selector(configField(field), "IsNull"))),
		token.LAND,
		astgen.Unary(token.NOT, astgen.Call(astgen.Selector(configField(field), "IsUnknown"))),
	)
}

// configFieldValueString returns the config.<field>.ValueString() call
// expression used to feed a typed config value into an interceptor.
func configFieldValueString(field string) *ast.CallExpr {
	return astgen.Call(astgen.Selector(configField(field), "ValueString"))
}

// appendInterceptorOpt returns the statement
//
//	opts = append(opts, client.WithSchemeInterceptor(<schemeName>, <interceptor>))
//
// which registers a constructed interceptor keyed by its OpenAPI security
// scheme name on the client. The scheme name lets a per-operation
// client.WithSchemes(...) request option apply exactly this scheme (per-
// operation AND resolution, REMAINING_GAPS §1).
func appendInterceptorOpt(schemeName string, interceptor ast.Expr) ast.Stmt {
	return astgen.AssignStmt(
		[]ast.Expr{astgen.Ident("opts")},
		[]ast.Expr{astgen.Call(
			astgen.Ident("append"),
			astgen.Ident("opts"),
			astgen.Call(astgen.QualExpr("client", "WithSchemeInterceptor"), astgen.Lit(schemeName), interceptor),
		)},
		token.ASSIGN,
	)
}
