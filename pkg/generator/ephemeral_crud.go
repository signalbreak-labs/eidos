package generator

import (
	"fmt"
	"go/ast"
	"go/token"
	"sort"
	"strings"
	"unicode"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/naming"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// This file builds the wired Open body for ephemeral resources whose OpenMapping
// is a bodiless (no request body, no formData) operation with resolvable path
// and required parameters. Ephemeral resources with a request body, formData
// parameters, or an unresolvable mapping keep their honest scaffold Open body;
// the decision is made once by planEphemeralWiring so partial mappings never
// produce half-wired ephemeral resources. Renew and Close are wired on the same
// terms when their mappings resolve (F2): because the framework's Renew/Close
// requests carry no config, Open stashes the lifecycle parameter values in
// ephemeral private state (resp.Private.SetKey) and the wired Renew/Close
// bodies read them back (req.Private.GetKey).

// ephemeralWiringPlan describes how an ephemeral resource's Open method is wired
// to the generated API client. wired is false when the open mapping cannot be
// resolved as a bodiless request against the config schema, in which case the
// Open method keeps its honest scaffold body. renew and close are non-nil when
// the corresponding lifecycle mapping resolves on the same terms; Renew/Close
// can only be wired when Open is (the client is only stored on the resource
// when open is wired).
type ephemeralWiringPlan struct {
	wired bool
	open  crudOperationPlan
	renew *crudOperationPlan
	close *crudOperationPlan

	// privateParams are the config attributes the wired Renew/Close bodies
	// need, stashed in ephemeral private state by Open (sorted by field name
	// for deterministic output).
	privateParams []paramSubstitution

	needsStrings bool
	needsStrconv bool
}

// AnyEphemeralWired reports whether at least one ephemeral resource has a
// resolvable bodiless open mapping. It gates emission of the JSON conversion
// helpers and the provider Configure client construction so providers with no
// wired ephemeral resources carry no dead code.
func AnyEphemeralWired(ers []ir.EphemeralResourceIR) bool {
	for _, er := range ers {
		if planEphemeralWiring(er).wired {
			return true
		}
	}
	return false
}

// planEphemeralWiring resolves the generation plan for an ephemeral resource's
// wired Open body. Only bodiless opens are wired: an OpenMapping with a request
// body (BodySchema) or formData parameters cannot be sent by the generated
// client's JSON-only request path, so the ephemeral resource keeps its honest
// scaffold. The open mapping's path placeholders and required query/header
// parameters must each resolve to a primitive config (input) attribute, since
// the Open body reads its inputs from req.Config. At least one result attribute
// is required so the decoded response populates the ephemeral value.
func planEphemeralWiring(er ir.EphemeralResourceIR) ephemeralWiringPlan {
	var plan ephemeralWiringPlan
	if !ephemeralHasResultOutput(er) {
		return plan
	}
	open, ok := planEphemeralOpen(er)
	if !ok {
		return plan
	}
	plan.open = open
	plan.wired = true
	// Renew/Close wire on the same terms as Open (bodiless, resolvable
	// parameters). The required-attribute gate is not re-applied: Open already
	// guarantees every required config attribute is sent, and a lifecycle
	// operation legitimately references only the identifying subset.
	if er.HasRenew && er.RenewMapping != nil {
		if renew, ok := planEphemeralLifecycle(er.ConfigSchema.Attributes, *er.RenewMapping); ok {
			plan.renew = &renew
		}
	}
	if er.HasClose && er.CloseMapping != nil {
		if closeOp, ok := planEphemeralLifecycle(er.ConfigSchema.Attributes, *er.CloseMapping); ok {
			plan.close = &closeOp
		}
	}
	plan.privateParams = lifecyclePrivateParams(plan.renew, plan.close)
	if len(plan.privateParams) > 0 {
		// The Renew/Close bodies rebuild their paths with strings.ReplaceAll.
		plan.needsStrings = true
	}
	// strings.ReplaceAll is only referenced by requestPathStmts for path
	// placeholders; query/header/cookie parameters render through url.Values /
	// http.Header / strconv, so they require strconv (when non-string) but
	// never strings. Setting needsStrings for parameters would import strings
	// unused, mirroring the data source wiring note.
	for _, sub := range open.subs {
		plan.needsStrings = true
		if sub.primitive != ir.TypeString {
			plan.needsStrconv = true
		}
	}
	for _, params := range [][]paramSubstitution{open.queryParams, open.headerParams, open.cookieParams, plan.privateParams} {
		for _, p := range params {
			if p.primitive != ir.TypeString {
				plan.needsStrconv = true
			}
		}
	}
	return plan
}

// planEphemeralLifecycle resolves a Renew or Close mapping into a generation
// plan on the same terms as planEphemeralOpen: non-empty method/template, no
// request body or formData parameters, and path placeholders plus required
// query/header/cookie parameters that each resolve against the config (input)
// schema. The values themselves are supplied at runtime from ephemeral private
// state (stashed by Open), not from req.Config — the framework's Renew/Close
// requests carry no config.
func planEphemeralLifecycle(attrs []ir.AttributeIR, op ir.OperationMappingIR) (crudOperationPlan, bool) {
	var planned crudOperationPlan
	planned.method = strings.ToUpper(strings.TrimSpace(op.Method))
	planned.template = strings.TrimSpace(op.PathTemplate)
	planned.successCodes = op.SuccessCodes
	planned.errorMappings = errorMappingDescriptions(op.ErrorMappings)
	if planned.method == "" || planned.template == "" {
		return planned, false
	}
	if op.BodySchema != nil || len(op.FormDataParams) > 0 {
		return crudOperationPlan{}, false
	}
	for _, placeholder := range pathPlaceholders(planned.template) {
		sub, ok := resolveDataSourcePathSubstitution(attrs, placeholder)
		if !ok {
			return crudOperationPlan{}, false
		}
		planned.subs = append(planned.subs, sub)
	}
	queryParams, qok := resolveParamSubstitutions(attrs, op.QueryParams)
	if !qok {
		return crudOperationPlan{}, false
	}
	headerParams, hok := resolveParamSubstitutions(attrs, op.HeaderParams)
	if !hok {
		return crudOperationPlan{}, false
	}
	cookieParams, cok := resolveParamSubstitutions(attrs, op.CookieParams)
	if !cok {
		return crudOperationPlan{}, false
	}
	planned.queryParams = queryParams
	planned.headerParams = headerParams
	planned.cookieParams = cookieParams
	// Per-operation security (REMAINING_GAPS §1). See applySecurityRequirements.
	applySecurityRequirements(&planned, op.SecurityRequirements)
	return planned, true
}

// lifecyclePrivateParams returns the union of config fields referenced by the
// wired Renew/Close plans (path substitutions and query/header/cookie
// parameters), deduplicated by field name and sorted for deterministic output.
// Open stashes each in ephemeral private state so the lifecycle bodies can
// read them back.
func lifecyclePrivateParams(plans ...*crudOperationPlan) []paramSubstitution {
	seen := make(map[string]bool)
	var out []paramSubstitution
	collect := func(field string, primitive ir.PrimitiveType) {
		if field == "" || seen[field] {
			return
		}
		seen[field] = true
		out = append(out, paramSubstitution{field: field, primitive: primitive})
	}
	for _, plan := range plans {
		if plan == nil {
			continue
		}
		for _, sub := range plan.subs {
			collect(sub.field, sub.primitive)
		}
		for _, params := range [][]paramSubstitution{plan.queryParams, plan.headerParams, plan.cookieParams} {
			for _, p := range params {
				collect(p.field, p.primitive)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].field < out[j].field })
	return out
}

// ephemeralHasResultOutput reports whether the ephemeral resource has at least
// one result attribute, i.e. the open response contributes an output the Open
// body can populate. An ephemeral resource with no result attributes has nothing
// to decode and is not wired.
func ephemeralHasResultOutput(er ir.EphemeralResourceIR) bool {
	return len(er.ResultSchema.Attributes) > 0
}

// planEphemeralOpen resolves the ephemeral Open operation into a generation
// plan. Path placeholders and parameters resolve against the config (input)
// schema by normalized (PascalCase) field name with no ID fallback, since an
// ephemeral resource identifies its target through declared config attributes.
// A request body (BodySchema) or formData parameters disable wiring: the
// generated client only encodes JSON bodies, and an ephemeral Open is modeled
// as a bodiless read. Required query/header parameters with no matching
// attribute disable wiring; optional unmapped parameters are skipped.
func planEphemeralOpen(er ir.EphemeralResourceIR) (crudOperationPlan, bool) {
	op := er.OpenMapping
	var planned crudOperationPlan
	planned.method = strings.ToUpper(strings.TrimSpace(op.Method))
	planned.template = strings.TrimSpace(op.PathTemplate)
	planned.successCodes = op.SuccessCodes
	planned.errorMappings = errorMappingDescriptions(op.ErrorMappings)
	planned.responseEnvelope = op.ResponseEnvelope
	if planned.method == "" || planned.template == "" {
		return planned, false
	}
	// Only bodiless opens are wired (REMAINING_GAPS §4). A request body or
	// formData parameters require a body the generated JSON-only request path
	// cannot satisfy for the ephemeral Open contract, so the ephemeral resource
	// keeps its honest scaffold rather than emitting a body that would fail at
	// runtime. The transformer emits a fail-loud warning for formData.
	if op.BodySchema != nil || len(op.FormDataParams) > 0 {
		return crudOperationPlan{}, false
	}
	attrs := er.ConfigSchema.Attributes
	for _, placeholder := range pathPlaceholders(planned.template) {
		sub, ok := resolveDataSourcePathSubstitution(attrs, placeholder)
		if !ok {
			return crudOperationPlan{}, false
		}
		planned.subs = append(planned.subs, sub)
	}
	queryParams, qok := resolveParamSubstitutions(attrs, op.QueryParams)
	if !qok {
		return crudOperationPlan{}, false
	}
	headerParams, hok := resolveParamSubstitutions(attrs, op.HeaderParams)
	if !hok {
		return crudOperationPlan{}, false
	}
	cookieParams, cok := resolveParamSubstitutions(attrs, op.CookieParams)
	if !cok {
		return crudOperationPlan{}, false
	}
	planned.queryParams = queryParams
	planned.headerParams = headerParams
	planned.cookieParams = cookieParams
	// A Required config attribute that no path/query/header/cookie parameter
	// references would be silently dropped by the bodiless Open body, which only
	// sends declared parameters. That violates the honest-bodies rule, so the
	// ephemeral resource keeps its scaffold rather than wiring an Open that
	// ignores a required practitioner input. Optional unreferenced attributes
	// are acceptable (the practitioner may leave them unset).
	if !allRequiredConfigAttrsReferenced(attrs, planned) {
		return crudOperationPlan{}, false
	}
	// Per-operation security (REMAINING_GAPS §1). See applySecurityRequirements.
	applySecurityRequirements(&planned, op.SecurityRequirements)
	return planned, true
}

// allRequiredConfigAttrsReferenced reports whether every Required config
// attribute is referenced by a path substitution or a query/header/cookie
// parameter in the planned open operation, so the wired Open body sends every
// required practitioner input instead of silently dropping it.
func allRequiredConfigAttrsReferenced(attrs []ir.AttributeIR, plan crudOperationPlan) bool {
	referenced := make(map[string]struct{}, len(plan.subs)+len(plan.queryParams)+len(plan.headerParams)+len(plan.cookieParams))
	for _, s := range plan.subs {
		referenced[s.field] = struct{}{}
	}
	for _, params := range [][]paramSubstitution{plan.queryParams, plan.headerParams, plan.cookieParams} {
		for _, p := range params {
			referenced[p.field] = struct{}{}
		}
	}
	for _, attr := range attrs {
		if !attr.Required {
			continue
		}
		if _, ok := referenced[naming.GoFieldName(attr.Name)]; !ok {
			return false
		}
	}
	return true
}

// scaffoldEphemeralOpenBody returns the honest scaffold Open body used when the
// ephemeral resource's OpenMapping is not resolvable as a bodiless request: it
// decodes the config, reports that Open is not wired, and still stores the
// config as the result so the generated provider compiles into a runnable
// scaffold.
func scaffoldEphemeralOpenBody(modelName string) []ast.Stmt {
	return []ast.Stmt{
		astgen.VarDecl("data", modelName, nil),
		astgen.AssignSingle(
			astgen.Ident("diags"),
			astgen.Call(
				astgen.Selector(astgen.Selector(astgen.Ident("req"), "Config"), "Get"),
				astgen.Ident("ctx"),
				astgen.UnaryPtr(astgen.Ident("data")),
			),
		),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "Append"),
			astgen.Ellipsis(astgen.Ident("diags")),
		)),
		astgen.If(
			astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "HasError")),
			astgen.Return(),
		),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "AddError"),
			astgen.Lit("Generated provider scaffold"),
			astgen.Lit("Open is not wired to a remote API endpoint."),
		)),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "Append"),
			astgen.Ellipsis(astgen.Call(
				astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Result"), "Set"),
				astgen.Ident("ctx"),
				astgen.UnaryPtr(astgen.Ident("data")),
			)),
		)),
	}
}

// wiredEphemeralOpenBody returns the Open body wired to the generated API
// client: it reads the practitioner-supplied config attributes, issues the
// bodiless open request, decodes the response into the merged config+result
// model, stores the result, and — when Renew/Close are wired — stashes the
// lifecycle parameter values in ephemeral private state so the Renew/Close
// bodies (whose requests carry no config) can read them back. The stash runs
// after the response decode so server-assigned identifiers are what Renew/
// Close use. The local model variable is named `config` to avoid colliding
// with the `data` map declared by decodeAndApplyStmts.
func wiredEphemeralOpenBody(er ir.EphemeralResourceIR, wiring ephemeralWiringPlan, modelName string) []ast.Stmt {
	plan := wiring.open
	summary := fmt.Sprintf("Error opening ephemeral resource %s", ephemeralResourceTypeName(er))
	stmts := make([]ast.Stmt, 0, 20)
	stmts = append(stmts,
		astgen.VarDecl("config", modelName, nil),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "Append"),
			astgen.Ellipsis(astgen.Call(
				astgen.Selector(astgen.Selector(astgen.Ident("req"), "Config"), "Get"),
				astgen.Ident("ctx"),
				astgen.UnaryPtr(astgen.Ident("config")),
			)),
		)),
		astgen.If(
			astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "HasError")),
			astgen.Return(),
		),
		clientGuardStmt("e"),
	)
	stmts = append(stmts, requestPathStmts(plan, "config")...)
	// An ephemeral Open has no state to drop on a 404; a non-success status is
	// surfaced as an error by the generic non-success branch.
	stmts = append(stmts, sendRequestStmts(plan, "e", summary, "config", nil, nil)...)
	stmts = append(stmts, decodeAndApplyStmts(summary, "config", plan.responseEnvelope)...)
	stmts = append(stmts, privateParamStashStmts(wiring.privateParams)...)
	stmts = append(stmts, resultSetStmt("config"))
	return stmts
}

// privateParamKey returns the ephemeral private-state key under which Open
// stashes a lifecycle parameter value for the Renew/Close bodies.
func privateParamKey(field string) string {
	return "eidos.param." + field
}

// privateParamStashStmts emits the statements storing each lifecycle parameter
// value (rendered as a string, matching how it is substituted into a request)
// into ephemeral private state. It returns nil when no lifecycle parameters
// are needed, so an Open with no wired Renew/Close emits no private-state code.
func privateParamStashStmts(params []paramSubstitution) []ast.Stmt {
	if len(params) == 0 {
		return nil
	}
	stmts := make([]ast.Stmt, 0, len(params)+1)
	for _, p := range params {
		stmts = append(stmts, astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "Append"),
			astgen.Ellipsis(astgen.Call(
				astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Private"), "SetKey"),
				astgen.Ident("ctx"),
				astgen.Lit(privateParamKey(p.field)),
				astgen.Call(
					astgen.SliceType(astgen.Ident("byte")),
					modelFieldStringExpr("config", p.field, p.primitive),
				),
			)),
		)))
	}
	stmts = append(stmts, astgen.If(
		astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "HasError")),
		astgen.Return(),
	))
	return stmts
}

// privateParamReadStmts emits the statements reading one lifecycle parameter
// value back from ephemeral private state into a <var>Bytes variable (raw
// bytes; callers convert with string(...)). varName derives from the field
// name so multiple reads do not collide.
func privateParamReadStmts(p paramSubstitution) []ast.Stmt {
	varName := privateParamVar(p.field)
	return []ast.Stmt{
		astgen.Assign(
			[]ast.Expr{astgen.Ident(varName), astgen.Ident("diags")},
			[]ast.Expr{astgen.Call(
				astgen.Selector(astgen.Selector(astgen.Ident("req"), "Private"), "GetKey"),
				astgen.Ident("ctx"),
				astgen.Lit(privateParamKey(p.field)),
			)},
		),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "Append"),
			astgen.Ellipsis(astgen.Ident("diags")),
		)),
		astgen.If(
			astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "HasError")),
			astgen.Return(),
		),
	}
}

// privateParamVar returns the local variable name holding a private-state
// value, e.g. "idBytes" for the Id field.
func privateParamVar(field string) string {
	if field == "" {
		return "paramBytes"
	}
	r := []rune(field)
	r[0] = unicode.ToLower(r[0])
	return string(r) + "Bytes"
}

// lifecycleParamString returns the expression rendering a private-state value
// as the string it was stored as.
func lifecycleParamString(field string) ast.Expr {
	return astgen.Call(astgen.Ident("string"), astgen.Ident(privateParamVar(field)))
}

// wiredEphemeralLifecycleBody returns the body for a wired Renew or Close
// method: it reads the lifecycle parameter values back from ephemeral private
// state (stashed by Open), rebuilds the request path/query/header/cookie from
// them, and issues the bodiless request. Renew surfaces no result (the
// framework's RenewResponse carries only RenewAt/Private/Diagnostics and the
// spec does not encode TTL semantics, so RenewAt is left unset); Close simply
// reports success.
func wiredEphemeralLifecycleBody(er ir.EphemeralResourceIR, plan crudOperationPlan, kind string) []ast.Stmt {
	summary := fmt.Sprintf("Error %s ephemeral resource %s", kind, ephemeralResourceTypeName(er))
	stmts := []ast.Stmt{clientGuardStmt("e")}

	// Read every referenced value back from private state, in a fixed order
	// (path substitutions, then query, header, cookie) for deterministic
	// output.
	for _, sub := range plan.subs {
		stmts = append(stmts, privateParamReadStmts(paramSubstitution{field: sub.field, primitive: sub.primitive})...)
	}
	for _, params := range [][]paramSubstitution{plan.queryParams, plan.headerParams, plan.cookieParams} {
		for _, p := range params {
			stmts = append(stmts, privateParamReadStmts(p)...)
		}
	}

	stmts = append(stmts, astgen.AssignSingle(astgen.Ident("reqPath"), astgen.Lit(plan.template)))
	for _, sub := range plan.subs {
		stmts = append(stmts, astgen.AssignStmt(
			[]ast.Expr{astgen.Ident("reqPath")},
			[]ast.Expr{astgen.Call(
				astgen.QualExpr("strings", "ReplaceAll"),
				astgen.Ident("reqPath"),
				astgen.Lit("{"+sub.placeholder+"}"),
				lifecycleParamString(sub.field),
			)},
			token.ASSIGN,
		))
	}

	stmts = append(stmts,
		astgen.Assign(
			[]ast.Expr{astgen.Ident("httpReq"), astgen.Ident("err")},
			[]ast.Expr{newRequestCall("e", plan, astgen.Nil())},
		),
		errCheckStmt(summary, "Could not build request: %s"),
	)
	if len(plan.queryParams) > 0 {
		stmts = append(stmts, astgen.AssignSingle(
			astgen.Ident("query"),
			astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("httpReq"), "URL"), "Query")),
		))
		for _, p := range plan.queryParams {
			stmts = append(stmts, astgen.ExprStmt(astgen.Call(
				astgen.Selector(astgen.Ident("query"), "Set"),
				astgen.Lit(p.name),
				lifecycleParamString(p.field),
			)))
		}
		stmts = append(stmts, astgen.AssignStmt(
			[]ast.Expr{astgen.Selector(astgen.Selector(astgen.Ident("httpReq"), "URL"), "RawQuery")},
			[]ast.Expr{astgen.Call(astgen.Selector(astgen.Ident("query"), "Encode"))},
			token.ASSIGN,
		))
	}
	for _, p := range plan.headerParams {
		stmts = append(stmts, astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("httpReq"), "Header"), "Set"),
			astgen.Lit(p.name),
			lifecycleParamString(p.field),
		)))
	}
	for _, p := range plan.cookieParams {
		stmts = append(stmts, astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Ident("httpReq"), "AddCookie"),
			astgen.UnaryPtr(astgen.CompositeLit(
				astgen.QualExpr("http", "Cookie"),
				astgen.KeyValue("Name", astgen.Lit(p.name)),
				astgen.KeyValue("Value", lifecycleParamString(p.field)),
			)),
		)))
	}
	stmts = append(stmts,
		astgen.Assign(
			[]ast.Expr{astgen.Ident("httpResp"), astgen.Ident("err")},
			[]ast.Expr{astgen.Call(
				astgen.Selector(astgen.Selector(astgen.Ident("e"), "client"), "Do"),
				astgen.Ident("httpReq"),
			)},
		),
		errCheckStmt(summary, "Could not send request: %s"),
		astgen.Defer(astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("httpResp"), "Body"), "Close"))),
		astgen.If(
			astgen.Unary(token.NOT, successCondition(plan)),
			nonSuccessBlock(plan, summary)...,
		),
	)
	return stmts
}

// resultSetStmt emits resp.Diagnostics.Append(resp.Result.Set(ctx, &model)...),
// the ephemeral equivalent of stateSetStmt: an ephemeral resource stores its
// value in resp.Result rather than resp.State.
func resultSetStmt(modelVar string) ast.Stmt {
	return astgen.ExprStmt(astgen.Call(
		astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "Append"),
		astgen.Ellipsis(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Result"), "Set"),
			astgen.Ident("ctx"),
			astgen.UnaryPtr(astgen.Ident(modelVar)),
		)),
	))
}

// ephemeralConfigureDecl returns the Configure method implementing
// ephemeral.EphemeralResourceWithConfigure. It type-asserts the
// provider-configured data to the generated API client and stores it on the
// ephemeral resource.
func ephemeralConfigureDecl(structName string) *ast.FuncDecl {
	return astgen.MethodDecl(
		"Configure", "e", astgen.StarExpr(astgen.Ident(structName)),
		astgen.Params(
			astgen.Field("_", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("req", astgen.QualExpr("ephemeral", "ConfigureRequest"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("ephemeral", "ConfigureResponse")), ""),
		),
		astgen.Results(),
		astgen.Block(
			astgen.If(
				astgen.Equal(astgen.Selector(astgen.Ident("req"), "ProviderData"), astgen.Nil()),
				astgen.Return(),
			),
			astgen.Assign(
				[]ast.Expr{astgen.Ident("c"), astgen.Ident("ok")},
				[]ast.Expr{astgen.TypeAssertExpr(
					astgen.Selector(astgen.Ident("req"), "ProviderData"),
					astgen.StarExpr(astgen.QualExpr("client", "Client")),
				)},
			),
			astgen.If(
				astgen.Unary(token.NOT, astgen.Ident("ok")),
				astgen.Block(
					addErrorStmt(
						"Unexpected Ephemeral Resource Configure Type",
						astgen.Call(
							astgen.QualExpr("fmt", "Sprintf"),
							astgen.Lit("Expected *client.Client, got: %T. Please report this issue to the provider developers."),
							astgen.Selector(astgen.Ident("req"), "ProviderData"),
						),
					),
					astgen.Return(),
				),
			),
			astgen.AssignStmt(
				[]ast.Expr{astgen.Selector(astgen.Ident("e"), "client")},
				[]ast.Expr{astgen.Ident("c")},
				token.ASSIGN,
			),
		),
	)
}
