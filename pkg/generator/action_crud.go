package generator

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/ir"
	"github.com/signalbreak-labs/eidos/pkg/transformer"
)

// This file builds the wired Invoke body for actions whose InvokeMapping is a
// bodiless (no request body, no formData) operation with resolvable path and
// required parameters. An action has no result/output surface — the framework
// InvokeResponse carries only Diagnostics and SendProgress — so a wired
// Invoke makes the real HTTP call, surfaces any error via Diagnostics, and
// returns; there is no response to decode. Actions with a request body,
// formData parameters, or an unresolvable mapping keep their honest scaffold
// Invoke body; the decision is made once by planActionWiring so partial
// mappings never produce half-wired actions. ModifyPlan and ValidateConfig are
// wired on the same terms when the action declares an explicit
// modify_plan_operation / validate_config_operation mapping in generator.yaml
// (F3): the generated body calls the preflight/validation endpoint and
// surfaces non-success statuses as diagnostics. The spec does not encode plan
// mutations, so a successful ModifyPlan leaves the plan unchanged.

// actionWiringPlan describes how an action's methods are wired to the
// generated API client. wired is true when at least one mapping resolves as a
// request against the config schema (it gates the client-carrying struct and
// Configure method); each method body is wired individually from its own plan
// pointer, so a resolvable ModifyPlan does not force a wired Invoke and vice
// versa. sendsBody is true when the wired Invoke encodes a JSON request body
// from the config model (modelToJSONMap + json.Marshal + bytes.NewReader),
// gating the encoding/json and bytes imports and the json_convert.go helper.
type actionWiringPlan struct {
	wired          bool
	invoke         *crudOperationPlan
	modifyPlan     *crudOperationPlan
	validateConfig *crudOperationPlan

	sendsBody    bool
	needsStrings bool
	needsStrconv bool
}

// AnyActionWired reports whether at least one action has a resolvable mapping
// (invoke, modify-plan, or validate-config). It gates the provider Configure
// client construction (via ActionData) so providers with no wired actions
// carry no dead code.
func AnyActionWired(actions []ir.ActionIR) bool {
	for _, a := range actions {
		if planActionWiring(a).wired {
			return true
		}
	}
	return false
}

// AnyActionSendsBody reports whether at least one action's wired Invoke
// encodes a JSON request body from its config model. It gates the
// json_convert.go helper file: a body-bearing wired action references
// modelToJSONMap, which lives there. Bodiless wired actions reference neither
// modelToJSONMap nor applyJSONToModel.
func AnyActionSendsBody(actions []ir.ActionIR) bool {
	for _, a := range actions {
		if planActionWiring(a).sendsBody {
			return true
		}
	}
	return false
}

// planActionWiring resolves the generation plan for an action's wired bodies.
// The Invoke mapping is wired when its path placeholders and required
// query/header parameters resolve to primitive config attributes; a JSON
// request body (BodySchema with a JSON media type) is additionally wired by
// encoding the config model, so the essential body-bearing actions (register,
// navigate, purchase, sell, refuel, deliver, install) work instead of staying
// scaffolded. formData parameters and non-JSON body media types cannot be sent
// by the generated JSON-only request path, so the corresponding method keeps
// its honest scaffold. ModifyPlan/ValidateConfig preflight mappings stay
// bodiless (allowBody=false): the preflight body is not modeled. Each mapping's
// path placeholders and required query/header parameters must each resolve to a
// primitive config (input) attribute, since the bodies read their inputs from
// req.Config. An action has no result surface, so unlike an ephemeral resource
// there is no result-output requirement.
func planActionWiring(a ir.ActionIR) actionWiringPlan {
	var plan actionWiringPlan
	if invoke, ok := planActionOperation(a.ConfigSchema.Attributes, a.InvokeMapping, true); ok {
		plan.invoke = &invoke
		plan.sendsBody = invoke.hasBody
	}
	if a.ModifyPlanMapping != nil {
		if mp, ok := planActionOperation(a.ConfigSchema.Attributes, *a.ModifyPlanMapping, false); ok {
			plan.modifyPlan = &mp
		}
	}
	if a.ValidateConfigMapping != nil {
		if vc, ok := planActionOperation(a.ConfigSchema.Attributes, *a.ValidateConfigMapping, false); ok {
			plan.validateConfig = &vc
		}
	}
	plan.wired = plan.invoke != nil || plan.modifyPlan != nil || plan.validateConfig != nil
	if !plan.wired {
		return plan
	}
	// strings.ReplaceAll is only referenced by requestPathStmts for path
	// placeholders; query/header/cookie parameters render through url.Values /
	// http.Header / strconv, so they require strconv (when non-string) but
	// never strings. Setting needsStrings for parameters would import strings
	// unused, mirroring the data source / ephemeral wiring note.
	for _, planned := range []*crudOperationPlan{plan.invoke, plan.modifyPlan, plan.validateConfig} {
		if planned == nil {
			continue
		}
		for _, sub := range planned.subs {
			plan.needsStrings = true
			if sub.primitive != ir.TypeString {
				plan.needsStrconv = true
			}
		}
		for _, params := range [][]paramSubstitution{planned.queryParams, planned.headerParams, planned.cookieParams} {
			for _, p := range params {
				if p.primitive != ir.TypeString {
					plan.needsStrconv = true
				}
			}
		}
	}
	return plan
}

// planActionOperation resolves an action operation (invoke, modify-plan, or
// validate-config) into a generation plan. Path placeholders and parameters
// resolve against the config (input) schema by normalized (PascalCase) field
// name with no ID fallback, since an action identifies its target through
// declared config attributes (mirroring the data source and ephemeral
// resolvers). When allowBody is true and the operation declares a JSON request
// body (BodySchema with a JSON media type), the plan carries bodyJSON so the
// wired Invoke encodes the config model as the request body — the essential
// body-bearing actions (register, navigate, purchase, sell, refuel, deliver,
// install) are wired instead of scaffolded. formData parameters and non-JSON
// body media types disable wiring: the generated client only encodes JSON
// bodies, so the method keeps its honest scaffold rather than emitting a body
// that would fail at runtime (the transformer emits a fail-loud warning for
// formData). Required query/header parameters with no matching attribute
// disable wiring; optional unmapped parameters are skipped.
func planActionOperation(attrs []ir.AttributeIR, op ir.OperationMappingIR, allowBody bool) (crudOperationPlan, bool) {
	var planned crudOperationPlan
	planned.method = strings.ToUpper(strings.TrimSpace(op.Method))
	planned.template = strings.TrimSpace(op.PathTemplate)
	planned.successCodes = op.SuccessCodes
	planned.errorMappings = errorMappingDescriptions(op.ErrorMappings)
	if planned.method == "" || planned.template == "" {
		return planned, false
	}
	// formData parameters require a body the generated JSON-only request path
	// cannot satisfy, so the method keeps its honest scaffold rather than
	// emitting a body that would fail at runtime. The transformer emits a
	// fail-loud warning for formData.
	if len(op.FormDataParams) > 0 {
		return crudOperationPlan{}, false
	}
	// A JSON request body is wired when allowBody (the Invoke mapping) and the
	// media type is JSON. XML and unsupported media types keep the method
	// scaffolded: actions have no XML root element to wrap the body in, and an
	// unsupported type cannot be encoded (the transformer already warned).
	if op.BodySchema != nil {
		if !allowBody || transformer.RequestBodyKind(op.MediaType) != "json" {
			return crudOperationPlan{}, false
		}
		planned.hasBody = true
		planned.bodyEncoding = bodyJSON
		// contentType empty → defaults to application/json in sendRequestStmts.
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
	// A Required config attribute that no path/query/header/cookie parameter
	// references would be silently dropped by a bodiless body, which only sends
	// declared parameters. That violates the honest-bodies rule, so the action
	// keeps its scaffold rather than wiring a body that ignores a required
	// practitioner input. A body-bearing action sends every config attribute
	// (modelToJSONMap encodes the whole model), so the check is skipped there.
	// Optional unreferenced attributes are acceptable (the practitioner may leave
	// them unset).
	if op.BodySchema == nil && !allRequiredConfigAttrsReferenced(attrs, planned) {
		return crudOperationPlan{}, false
	}
	// Per-operation security (REMAINING_GAPS §1). See applySecurityRequirements.
	applySecurityRequirements(&planned, op.SecurityRequirements)
	return planned, true
}

// scaffoldActionInvokeBody returns the honest scaffold Invoke body used when
// the action's InvokeMapping is not resolvable as a bodiless request: it
// decodes the config and reports that Invoke is not wired, so the generated
// provider compiles into a runnable scaffold rather than silently no-op'ing.
func scaffoldActionInvokeBody(modelName string) []ast.Stmt {
	return []ast.Stmt{
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
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "AddError"),
			astgen.Lit("Generated provider scaffold"),
			astgen.Lit("Invoke is not wired to a remote API endpoint."),
		)),
	}
}

// wiredActionInvokeBody returns the Invoke body wired to the generated API
// client: it reads the practitioner-supplied config attributes, issues the
// invoke request, and surfaces any error via Diagnostics. A body-bearing
// action (plan.bodyEncoding == bodyJSON) encodes the config model as the JSON
// request body via modelToJSONMap + json.Marshal + bytes.NewReader, so the
// essential body-bearing actions (register, navigate, purchase, sell, refuel,
// deliver, install) send their payloads instead of staying scaffolded. An
// action has no result surface, so — unlike resource/data-source/ephemeral
// bodies — there is no response to decode and no state/result to set; the
// Invoke succeeds by completing the request without a non-success status. The
// local model variable is named `config`, matching the scaffold body and
// avoiding collision with any future decode helper's `data` map.
func wiredActionInvokeBody(a ir.ActionIR, plan crudOperationPlan, modelName string) []ast.Stmt {
	summary := fmt.Sprintf("Error invoking %s", actionTypeName(a))
	stmts := make([]ast.Stmt, 0, 16)
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
		clientGuardStmt("r"),
	)
	// progress_messages: true streams a progress update to the practitioner
	// before the request is issued. The framework's InvokeResponse carries
	// SendProgress, which takes an InvokeProgressEvent; the action package is
	// already imported by the generated action file, so no new import is needed.
	if a.ProgressMessages {
		stmts = append(stmts, astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Ident("resp"), "SendProgress"),
			astgen.CompositeLit(
				astgen.QualExpr("action", "InvokeProgressEvent"),
				astgen.KeyValue("Message", astgen.Lit(fmt.Sprintf("Invoking %s", actionTypeName(a)))),
			),
		)))
	}
	stmts = append(stmts, requestPathStmts(plan, "config")...)
	// A body-bearing action encodes the config model as the JSON request body;
	// a bodiless action passes nil (sendRequestStmts sets no Content-Type).
	var body ast.Expr
	if plan.hasBody {
		bodyStmts, bodyExpr := requestBodyStmts(plan, summary, "config")
		stmts = append(stmts, bodyStmts...)
		body = bodyExpr
	}
	// An action has no state to drop on a 404; a non-success status is surfaced
	// as an error by the generic non-success branch. There is no response body
	// to decode and no result surface to populate, so the Invoke completes by
	// issuing the request and returning on success.
	stmts = append(stmts, sendRequestStmts(plan, "r", summary, "config", body, nil)...)
	return stmts
}

// wiredActionPreflightBody returns the ModifyPlan or ValidateConfig body wired
// to the action's explicit preflight/validation endpoint (declared via
// generator.yaml modify_plan_operation / validate_config_operation, F3): it
// decodes the config, issues the bodiless request, and surfaces a non-success
// status as error diagnostics. A successful response carries no machine-usable
// payload the spec encodes, so ModifyPlan leaves the plan unchanged and
// ValidateConfig simply returns — the wiring exists so the endpoint performs
// its server-side check.
func wiredActionPreflightBody(plan crudOperationPlan, modelName, summary string) []ast.Stmt {
	stmts := make([]ast.Stmt, 0, 12)
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
		clientGuardStmt("r"),
	)
	stmts = append(stmts, requestPathStmts(plan, "config")...)
	stmts = append(stmts, sendRequestStmts(plan, "r", summary, "config", nil, nil)...)
	return stmts
}

// actionConfigureDecl returns the Configure method implementing
// action.ActionWithConfigure. It type-asserts the provider-configured data to
// the generated API client and stores it on the action, mirroring the
// resource, data source, and ephemeral Configure paths.
func actionConfigureDecl(structName string) *ast.FuncDecl {
	return astgen.MethodDecl(
		"Configure", "r", astgen.StarExpr(astgen.Ident(structName)),
		astgen.Params(
			astgen.Field("_", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("req", astgen.QualExpr("action", "ConfigureRequest"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("action", "ConfigureResponse")), ""),
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
						"Unexpected Action Configure Type",
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
				[]ast.Expr{astgen.Selector(astgen.Ident("r"), "client")},
				[]ast.Expr{astgen.Ident("c")},
				token.ASSIGN,
			),
		),
	)
}
