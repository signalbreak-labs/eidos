package generator

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"
	"unicode"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/naming"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// This file builds the wired List body for list resources whose ListMapping
// resolves as a bodiless (GET) collection read with an identity schema, so
// terraform query streams real instances from the generated client instead of
// the honest scaffold's fatal diagnostic. The wiring decision mirrors the data
// source Read gating: path placeholders and required parameters must resolve
// against the list resource's config (filter) schema, and at least one
// identity attribute is required so streamed results can populate
// ListResult.Identity (F1).

// listResourceWiringPlan describes how a list resource's List method is wired
// to the generated API client. wired is false when the list mapping cannot be
// resolved (the List method then keeps its honest scaffold body).
type listResourceWiringPlan struct {
	wired bool
	read  crudOperationPlan

	// hasConfigModel is true when the config schema declares attributes, so the
	// wired body decodes req.Config into the generated model struct. Without
	// config attributes there are no parameters to resolve and the body sends a
	// static request.
	hasConfigModel bool

	needsStrings bool
	needsStrconv bool
}

// AnyListResourceWired reports whether at least one list resource has a
// resolvable list mapping. It gates the provider Configure client construction
// so providers whose only wired construct is a list resource still build the
// client.
func AnyListResourceWired(lrs []ir.ListResourceIR) bool {
	for _, lr := range lrs {
		if planListResourceWiring(lr).wired {
			return true
		}
	}
	return false
}

// planListResourceWiring resolves the generation plan for a list resource's
// wired List body. The list mapping must be a bodiless operation whose path
// placeholders and required query/header/cookie parameters resolve against the
// config (filter) schema — mirroring planDataSourceRead — and the identity
// schema must carry at least one attribute: a streamed result without identity
// attributes cannot populate ListResult.Identity, so such a list resource
// keeps its honest scaffold rather than streaming results Terraform cannot
// use.
func planListResourceWiring(lr ir.ListResourceIR) listResourceWiringPlan {
	var plan listResourceWiringPlan
	if len(lr.IdentitySchema.Attributes) == 0 {
		return plan
	}
	op := lr.ListMapping
	planned := crudOperationPlan{
		method:           strings.ToUpper(strings.TrimSpace(op.Method)),
		template:         strings.TrimSpace(op.PathTemplate),
		successCodes:     op.SuccessCodes,
		errorMappings:    errorMappingDescriptions(op.ErrorMappings),
		responseEnvelope: op.ResponseEnvelope,
	}
	if planned.method == "" || planned.template == "" {
		return plan
	}
	// A list read is bodiless by contract (GET); a body or formData parameters
	// cannot be sent and keep the list resource scaffolded.
	if op.BodySchema != nil || len(op.FormDataParams) > 0 {
		return plan
	}
	attrs := lr.ConfigSchema.Attributes
	for _, placeholder := range pathPlaceholders(planned.template) {
		sub, ok := resolveDataSourcePathSubstitution(attrs, placeholder)
		if !ok {
			return plan
		}
		planned.subs = append(planned.subs, sub)
	}
	queryParams, qok := resolveParamSubstitutions(attrs, op.QueryParams)
	if !qok {
		return plan
	}
	headerParams, hok := resolveParamSubstitutions(attrs, op.HeaderParams)
	if !hok {
		return plan
	}
	cookieParams, cok := resolveParamSubstitutions(attrs, op.CookieParams)
	if !cok {
		return plan
	}
	planned.queryParams = queryParams
	planned.headerParams = headerParams
	planned.cookieParams = cookieParams
	// Per-operation security (REMAINING_GAPS §1). See applySecurityRequirements.
	applySecurityRequirements(&planned, op.SecurityRequirements)

	plan.read = planned
	plan.wired = true
	plan.hasConfigModel = len(attrs) > 0
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
	if lr.PaginationStyle == ir.PaginationStyleOffset {
		plan.needsStrconv = true
	}
	return plan
}

// listIdentityKeys returns the candidate JSON keys the wired List body probes
// for an identity attribute's value, in priority order: the attribute's wire
// name (the original OpenAPI property name, e.g. "symbol" when the path
// parameter was "{shipSymbol}"), then the sanitized attribute name, then "id"
// (the de-facto identifier key). The wire name leads because the identity
// attribute's Terraform name (e.g. "ship_symbol") need not match the item
// object's actual JSON key (e.g. "symbol"), and probing the sanitized name
// first would miss the value. Duplicate keys are dropped so a plain "id"
// attribute probes exactly once.
func listIdentityKeys(attrName, wireName string) []string {
	var keys []string
	add := func(k string) {
		if k == "" {
			return
		}
		for _, existing := range keys {
			if existing == k {
				return
			}
		}
		keys = append(keys, k)
	}
	add(wireName)
	add(attrName)
	add("id")
	return keys
}

// listResourcePaginationStyle resolves the pagination style for a list
// resource from its IR, defaulting to none (single page) when unset, matching
// the generated client's DefaultPagination.
func listResourcePaginationStyle(lr ir.ListResourceIR) string {
	if lr.PaginationStyle != "" {
		return lr.PaginationStyle
	}
	return ir.PaginationStyleNone
}

// listPageItemsRemoteStmts emits the statements decoding a fetched page into a
// per-page items []json.RawMessage slice the listRemote helper accumulates. It
// writes page-decode failures to the helper's returned diag.Diagnostics (via
// diags.AddError and a bare return) rather than through the framework closure's
// pushError, because the helper owns the HTTP/pagination exchange. Without an
// envelope the page is a bare JSON array decoded directly; with an envelope the
// page is a JSON object whose envelope key holds the item array (e.g.
// SpaceTraders' {data: [...], meta: ...}), decoded by unmarshaling the key's
// raw value so a malformed page surfaces an error rather than silently
// streaming nothing. The summary and "Could not decode list page" detail
// strings are preserved verbatim from the former pushError form.
func listPageItemsRemoteStmts(summary, envelope string) []ast.Stmt {
	if envelope == "" {
		return []ast.Stmt{
			astgen.AssignSingle(
				astgen.Ident("items"),
				astgen.CompositeLit(astgen.SliceType(astgen.QualExpr("json", "RawMessage"))),
			),
			&ast.IfStmt{
				Init: astgen.AssignSingle(astgen.Ident("err"), astgen.Call(
					astgen.QualExpr("json", "Unmarshal"), astgen.Ident("page"), astgen.UnaryPtr(astgen.Ident("items")),
				)),
				Cond: astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
				Body: astgen.Block(
					astgen.ExprStmt(astgen.Call(
						astgen.Selector(astgen.Ident("diags"), "AddError"),
						astgen.Lit(summary),
						astgen.Call(astgen.QualExpr("fmt", "Sprintf"), astgen.Lit("Could not decode list page: %s"), astgen.Ident("err")),
					)),
					astgen.Return(astgen.Nil(), astgen.Ident("diags")),
				),
			},
		}
	}
	return []ast.Stmt{
		astgen.AssignSingle(
			astgen.Ident("pageObj"),
			astgen.CompositeLit(astgen.MapType(astgen.Ident("string"), astgen.QualExpr("json", "RawMessage"))),
		),
		&ast.IfStmt{
			Init: astgen.AssignSingle(astgen.Ident("err"), astgen.Call(
				astgen.QualExpr("json", "Unmarshal"), astgen.Ident("page"), astgen.UnaryPtr(astgen.Ident("pageObj")),
			)),
			Cond: astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
			Body: astgen.Block(
				astgen.ExprStmt(astgen.Call(
					astgen.Selector(astgen.Ident("diags"), "AddError"),
					astgen.Lit(summary),
					astgen.Call(astgen.QualExpr("fmt", "Sprintf"), astgen.Lit("Could not decode list page: %s"), astgen.Ident("err")),
				)),
				astgen.Return(astgen.Nil(), astgen.Ident("diags")),
			),
		},
		astgen.Assign(
			[]ast.Expr{astgen.Ident("rawItems"), astgen.Ident("ok")},
			[]ast.Expr{astgen.IndexExpr(astgen.Ident("pageObj"), astgen.Lit(envelope))},
		),
		astgen.If(
			astgen.Unary(token.NOT, astgen.Ident("ok")),
			astgen.Block(
				astgen.ExprStmt(astgen.Call(
					astgen.Selector(astgen.Ident("diags"), "AddError"),
					astgen.Lit(summary),
					astgen.Call(astgen.QualExpr("fmt", "Sprintf"), astgen.Lit("Could not decode list page: missing %q array"), astgen.Lit(envelope)),
				)),
				astgen.Return(astgen.Nil(), astgen.Ident("diags")),
			),
		),
		astgen.AssignSingle(
			astgen.Ident("items"),
			astgen.CompositeLit(astgen.SliceType(astgen.QualExpr("json", "RawMessage"))),
		),
		&ast.IfStmt{
			Init: astgen.AssignSingle(astgen.Ident("err"), astgen.Call(
				astgen.QualExpr("json", "Unmarshal"), astgen.Ident("rawItems"), astgen.UnaryPtr(astgen.Ident("items")),
			)),
			Cond: astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
			Body: astgen.Block(
				astgen.ExprStmt(astgen.Call(
					astgen.Selector(astgen.Ident("diags"), "AddError"),
					astgen.Lit(summary),
					astgen.Call(astgen.QualExpr("fmt", "Sprintf"), astgen.Lit("Could not decode list page: %s"), astgen.Ident("err")),
				)),
				astgen.Return(astgen.Nil(), astgen.Ident("diags")),
			),
		},
	}
}

// wiredListBody returns the framework List body wired to the generated API
// client, emitted as the stream.Results iterator closure. It decodes the filter
// config (when the config schema declares attributes), delegates the paginated
// HTTP exchange to listRemote (which returns the decoded items and any
// diagnostics), pushes a single error result and stops when listRemote
// reports a failure, and otherwise pushes one ListResult per decoded item:
// identity values are probed from the item JSON (top-level keys, then a nested
// "metadata" object, which nested-metadata APIs use) and converted via
// tftypes.ValueFromJSON against the request's identity schema type; the full
// resource is populated the same way when the practitioner asked for it
// (IncludeResource), with a per-item warning instead of a fatal error when the
// item does not match the resource schema. Per-item failures push an error
// result and continue. The HTTP/pagination/decode logic lives in listRemote so
// it is unit-testable without constructing a list.ListRequest, whose identity
// schema type is built from an internal fwschema type generated code cannot
// instantiate.
func wiredListBody(lr ir.ListResourceIR, plan listResourceWiringPlan, modelName string) []ast.Stmt {
	summary := fmt.Sprintf("Error listing %s", listResourceTypeName(lr))

	body := []ast.Stmt{}

	if plan.hasConfigModel {
		body = append(body,
			astgen.VarDecl("config", modelName, nil),
			astgen.AssignSingle(
				astgen.Ident("diags"),
				astgen.Call(
					astgen.Selector(astgen.Selector(astgen.Ident("req"), "Config"), "Get"),
					astgen.Ident("ctx"),
					astgen.UnaryPtr(astgen.Ident("config")),
				),
			),
			astgen.If(
				astgen.Call(astgen.Selector(astgen.Ident("diags"), "HasError")),
				listPushDiagsStmts("diags")...,
			),
		)
	}

	// Delegate the HTTP exchange + page decode to listRemote; on failure push
	// one error result carrying the helper's diagnostics and stop. On success
	// iterate the decoded items and push one ListResult per item.
	remoteArgs := []ast.Expr{astgen.Ident("ctx")}
	if plan.hasConfigModel {
		remoteArgs = append(remoteArgs, astgen.UnaryPtr(astgen.Ident("config")))
	}
	body = append(body,
		astgen.Assign(
			[]ast.Expr{astgen.Ident("items"), astgen.Ident("diags")},
			[]ast.Expr{astgen.Call(astgen.Selector(astgen.Ident("l"), "listRemote"), remoteArgs...)},
		),
		astgen.If(
			astgen.Call(astgen.Selector(astgen.Ident("diags"), "HasError")),
			astgen.Block(listPushDiagsStmts("diags")...),
		),
		astgen.RangeStmt(
			astgen.Ident("_"), astgen.Ident("item"), token.DEFINE, astgen.Ident("items"),
			astgen.Block(listItemResultStmts(lr, summary)...),
		),
	)

	return []ast.Stmt{
		astgen.AssignStmt(
			[]ast.Expr{astgen.Selector(astgen.Ident("stream"), "Results")},
			[]ast.Expr{astgen.FuncLit(
				astgen.FuncType(
					astgen.Params(astgen.Field("push", astgen.FuncType(
						astgen.Params(astgen.Field("", astgen.QualExpr("list", "ListResult"), "")),
						astgen.Results(astgen.Field("", astgen.Ident("bool"), "")),
					), "")),
					nil,
				),
				astgen.Block(body...),
			)},
			token.ASSIGN,
		),
	}
}

// listPushDiagsStmts emits the framework-closure statements that surface a
// diag.Diagnostics value as a single error ListResult and stop iteration:
// build a fresh result, assign the diagnostics, push it, and return. diagsVar
// names the enclosing diagnostics expression ("diags").
func listPushDiagsStmts(diagsVar string) []ast.Stmt {
	return []ast.Stmt{
		astgen.AssignSingle(
			astgen.Ident("result"),
			astgen.Call(astgen.Selector(astgen.Ident("req"), "NewListResult"), astgen.Ident("ctx")),
		),
		astgen.AssignStmt(
			[]ast.Expr{astgen.Selector(astgen.Ident("result"), "Diagnostics")},
			[]ast.Expr{astgen.Ident(diagsVar)},
			token.ASSIGN,
		),
		astgen.ExprStmt(astgen.Call(astgen.Ident("push"), astgen.Ident("result"))),
		astgen.Return(),
	}
}

// wiredListHelperBody returns the body of listRemote: the client guard, request
// path, query parameters, paginated fetch via the generated client's
// ListAllPages helper, and per-page decode/accumulate. It writes diagnostics to
// a returned diag.Diagnostics value (not resp.Diagnostics, because the list
// resource has no public response struct the helper can mutate) and returns the
// accumulated items so the framework List closure can build per-item ListResult
// values from them. The client guard, request build, and ListAllPages exchange
// stay in this method (in the same file as List) so the l.client.NewRequest
// marker and the l.client == nil / client.ListAllPages / l.client.Do substrings
// remain present, preserving the honest-scaffold golden invariants.
func wiredListHelperBody(lr ir.ListResourceIR, plan listResourceWiringPlan) []ast.Stmt {
	summary := fmt.Sprintf("Error listing %s", listResourceTypeName(lr))
	style := listResourcePaginationStyle(lr)

	stmts := make([]ast.Stmt, 0, 20)
	// diags accumulates the helper's diagnostics and is returned alongside the
	// items. AddError has a pointer receiver, but a local var is addressable so
	// diags.AddError resolves without an explicit &diags. The client guard
	// follows immediately: it writes the Client Not Configured diagnostic to
	// diags and returns (nil, diags). It is built inline (not clientGuardStmt)
	// because the shared builder writes to resp.Diagnostics, which the list
	// helper does not carry.
	stmts = append(stmts,
		astgen.VarDecl("diags", "diag.Diagnostics", nil),
		astgen.If(
			astgen.Equal(astgen.Selector(astgen.Ident("l"), "client"), astgen.Nil()),
			astgen.Block(
				astgen.ExprStmt(astgen.Call(
					astgen.Selector(astgen.Ident("diags"), "AddError"),
					astgen.Lit("Client Not Configured"),
					astgen.Lit("The API client was not set on the list resource. The provider Configure method must run before list operations; this is a bug in the generated provider."),
				)),
				astgen.Return(astgen.Nil(), astgen.Ident("diags")),
			),
		),
	)

	if plan.hasConfigModel {
		stmts = append(stmts, requestPathStmts(plan.read, "config")...)
	} else {
		stmts = append(stmts, astgen.AssignSingle(astgen.Ident("reqPath"), astgen.Lit(plan.read.template)))
	}

	// The query parameters travel in a url.Values that ListAllPages clones and
	// passes to both the fetch closure and the next callback (see the list data
	// source wiring).
	stmts = append(stmts, astgen.AssignSingle(
		astgen.Ident("params"),
		astgen.CompositeLit(astgen.QualExpr("url", "Values")),
	))
	for _, p := range plan.read.queryParams {
		// A scalar query parameter emits a single params.Set; a collection (an
		// array query parameter modeled as a List) emits one params.Add per
		// element (repeated query values). Gate optional query parameters on a
		// non-null model value so an unset optional parameter is omitted from the
		// request rather than sent as the zero-value empty string/0. Required
		// parameters pass through ungated. Mirrors requestHeaderStmts and the
		// list data source wiring.
		stmts = append(stmts, paramSetStmts(p, "config", astgen.Ident("params"))...)
	}
	if style == ir.PaginationStyleOffset {
		stmts = append(stmts, astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Ident("params"), "Set"),
			astgen.Lit("page"),
			astgen.Lit("1"),
		)))
	}
	// nextURL carries the link_header next-page URL from the next callback to
	// the fetch closure. It stays empty for the other styles, which drive
	// pagination through params.
	stmts = append(stmts,
		astgen.VarDecl("nextURL", "string", nil),
		listFetchAssign("l", plan.read, "config"),
	)
	if style != ir.PaginationStyleNone {
		stmts = append(stmts, listNextAssign(style, "page", "cursor", "next"))
	}
	listArgs := []ast.Expr{astgen.Ident("ctx"), astgen.Ident("params"), astgen.Ident("fetch")}
	if style == ir.PaginationStyleNone {
		listArgs = append(listArgs, astgen.Nil())
	} else {
		listArgs = append(listArgs, astgen.Ident("next"))
	}
	// Fetch the pages through the generated client's ListAllPages helper; a
	// fetch/send error surfaces as "Could not read list response".
	stmts = append(stmts,
		astgen.Assign(
			[]ast.Expr{astgen.Ident("pages"), astgen.Ident("err")},
			[]ast.Expr{astgen.Call(astgen.QualExpr("client", "ListAllPages"), listArgs...)},
		),
		astgen.If(
			astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
			astgen.Block(
				astgen.ExprStmt(astgen.Call(
					astgen.Selector(astgen.Ident("diags"), "AddError"),
					astgen.Lit(summary),
					astgen.Call(astgen.QualExpr("fmt", "Sprintf"), astgen.Lit("Could not read list response: %s"), astgen.Ident("err")),
				)),
				astgen.Return(astgen.Nil(), astgen.Ident("diags")),
			),
		),
		// Accumulate the per-page decoded items into a single slice returned to
		// the framework closure. Per page: decode into the loop-local items
		// (envelope-aware); on failure listPageItemsRemoteStmts returns
		// (nil, diags) for us. allItems is the accumulator visible inside the
		// range body, where the per-page items shadows nothing because it is
		// named items and the accumulator is named allItems.
		astgen.AssignSingle(
			astgen.Ident("allItems"),
			astgen.CompositeLit(astgen.SliceType(astgen.QualExpr("json", "RawMessage"))),
		),
		astgen.RangeStmt(
			astgen.Ident("_"), astgen.Ident("page"), token.DEFINE, astgen.Ident("pages"),
			astgen.Block(
				append(listPageItemsRemoteStmts(summary, plan.read.responseEnvelope),
					astgen.AssignStmt(
						[]ast.Expr{astgen.Ident("allItems")},
						[]ast.Expr{astgen.Call(astgen.Ident("append"), astgen.Ident("allItems"), astgen.Ellipsis(astgen.Ident("items")))},
						token.ASSIGN,
					),
				)...,
			),
		),
		astgen.Return(astgen.Ident("allItems"), astgen.Ident("diags")),
	)
	return stmts
}

// wiredListHelperDecl emits the listRemote method declaration wired to the
// generated API client. Emitted only for wired list resources, alongside List.
// When the list resource declares filter attributes the helper takes a config
// model pointer; an attribute-free (static-path) list resource has no model
// type, so the helper takes only the context.
func wiredListHelperDecl(lr ir.ListResourceIR, plan listResourceWiringPlan, modelName, structName string) *ast.FuncDecl {
	params := []*ast.Field{
		astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
	}
	if plan.hasConfigModel {
		params = append(params, astgen.Field("config", astgen.StarExpr(astgen.Ident(modelName)), ""))
	}
	results := astgen.Results(
		astgen.Field("", astgen.SliceType(astgen.QualExpr("json", "RawMessage")), ""),
		astgen.Field("", astgen.QualExpr("diag", "Diagnostics"), ""),
	)
	return astgen.MethodDecl(
		"listRemote", "l", astgen.StarExpr(astgen.Ident(structName)),
		astgen.Params(params...),
		results,
		astgen.Block(wiredListHelperBody(lr, plan)...),
	)
}

// listItemResultStmts emits the per-item body of the streaming loop: decode
// the item, resolve the identity values, convert them into the identity type,
// optionally populate the full resource, and push the result (stopping the
// iteration when push returns false).
func listItemResultStmts(lr ir.ListResourceIR, summary string) []ast.Stmt {
	stmts := []ast.Stmt{
		astgen.AssignSingle(
			astgen.Ident("result"),
			astgen.Call(astgen.Selector(astgen.Ident("req"), "NewListResult"), astgen.Ident("ctx")),
		),
		astgen.AssignSingle(
			astgen.Ident("itemMap"),
			astgen.CompositeLit(astgen.MapType(astgen.Ident("string"), astgen.QualExpr("json", "RawMessage"))),
		),
		&ast.IfStmt{
			Init: astgen.AssignSingle(astgen.Ident("err"), astgen.Call(
				astgen.QualExpr("json", "Unmarshal"), astgen.Ident("item"), astgen.UnaryPtr(astgen.Ident("itemMap")),
			)),
			Cond: astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
			Body: astgen.Block(
				astgen.ExprStmt(astgen.Call(
					astgen.Selector(astgen.Selector(astgen.Ident("result"), "Diagnostics"), "AddError"),
					astgen.Lit(summary),
					astgen.Call(astgen.QualExpr("fmt", "Sprintf"), astgen.Lit("Could not decode list item: %s"), astgen.Ident("err")),
				)),
				astgen.If(
					astgen.Unary(token.NOT, astgen.Call(astgen.Ident("push"), astgen.Ident("result"))),
					astgen.Return(),
				),
				astgen.Continue(),
			),
		},
		astgen.AssignSingle(
			astgen.Ident("identity"),
			astgen.CompositeLit(astgen.MapType(astgen.Ident("string"), astgen.QualExpr("json", "RawMessage"))),
		),
	}

	// Per identity attribute: probe the candidate keys (top-level, then a
	// nested "metadata" object), push an error result and skip the item when
	// the value is missing.
	for _, attr := range lr.IdentitySchema.Attributes {
		varName := identityValueVar(attr.Name)
		keys := listIdentityKeys(attr.Name, attr.WireName)
		stmts = append(stmts,
			astgen.Assign(
				[]ast.Expr{astgen.Ident(varName), astgen.Ident("ok")},
				[]ast.Expr{astgen.IndexExpr(astgen.Ident("itemMap"), astgen.Lit(keys[0]))},
			),
			astgen.If(
				astgen.Unary(token.NOT, astgen.Ident("ok")),
				astgen.Block(
					astgen.If(
						astgen.NotEqual(
							astgen.IndexExpr(astgen.Ident("itemMap"), astgen.Lit("metadata")),
							astgen.Nil(),
						),
						astgen.Block(
							astgen.AssignSingle(
								astgen.Ident("metaMap"),
								astgen.CompositeLit(astgen.MapType(astgen.Ident("string"), astgen.QualExpr("json", "RawMessage"))),
							),
							astgen.If(
								astgen.Equal(
									astgen.Call(
										astgen.QualExpr("json", "Unmarshal"),
										astgen.IndexExpr(astgen.Ident("itemMap"), astgen.Lit("metadata")),
										astgen.UnaryPtr(astgen.Ident("metaMap")),
									),
									astgen.Nil(),
								),
								astgen.Block(astgen.AssignStmt(
									[]ast.Expr{astgen.Ident(varName), astgen.Ident("ok")},
									[]ast.Expr{astgen.IndexExpr(astgen.Ident("metaMap"), astgen.Lit(keys[0]))},
									token.ASSIGN,
								)),
							),
						),
					),
				),
			),
		)
		for _, key := range keys[1:] {
			stmts = append(stmts, astgen.If(
				astgen.Unary(token.NOT, astgen.Ident("ok")),
				astgen.Block(astgen.AssignStmt(
					[]ast.Expr{astgen.Ident(varName), astgen.Ident("ok")},
					[]ast.Expr{astgen.IndexExpr(astgen.Ident("itemMap"), astgen.Lit(key))},
					token.ASSIGN,
				)),
			))
		}
		stmts = append(stmts,
			astgen.If(
				astgen.Unary(token.NOT, astgen.Ident("ok")),
				astgen.Block(
					astgen.ExprStmt(astgen.Call(
						astgen.Selector(astgen.Selector(astgen.Ident("result"), "Diagnostics"), "AddError"),
						astgen.Lit(summary),
						astgen.Lit(fmt.Sprintf("List item is missing identity attribute %q.", attr.Name)),
					)),
					astgen.If(
						astgen.Unary(token.NOT, astgen.Call(astgen.Ident("push"), astgen.Ident("result"))),
						astgen.Return(),
					),
					astgen.Continue(),
				),
			),
			astgen.AssignStmt(
				[]ast.Expr{astgen.IndexExpr(astgen.Ident("identity"), astgen.Lit(attr.Name))},
				[]ast.Expr{astgen.Ident(varName)},
				token.ASSIGN,
			),
		)
	}

	// Convert the identity JSON object into the identity schema type and store
	// it on the result; on failure push the error result and skip the item.
	stmts = append(stmts,
		astgen.Assign(
			[]ast.Expr{astgen.Ident("idJSON"), astgen.Ident("err")},
			[]ast.Expr{astgen.Call(astgen.QualExpr("json", "Marshal"), astgen.Ident("identity"))},
		),
		astgen.If(
			astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
			astgen.Block(
				astgen.ExprStmt(astgen.Call(
					astgen.Selector(astgen.Selector(astgen.Ident("result"), "Diagnostics"), "AddError"),
					astgen.Lit(summary),
					astgen.Call(astgen.QualExpr("fmt", "Sprintf"), astgen.Lit("Could not encode list item identity: %s"), astgen.Ident("err")),
				)),
				astgen.If(
					astgen.Unary(token.NOT, astgen.Call(astgen.Ident("push"), astgen.Ident("result"))),
					astgen.Return(),
				),
				astgen.Continue(),
			),
		),
		astgen.Assign(
			[]ast.Expr{astgen.Ident("idVal"), astgen.Ident("err")},
			[]ast.Expr{astgen.Call(
				astgen.QualExpr("tftypes", "ValueFromJSON"),
				astgen.Ident("idJSON"),
				astgen.Call(
					astgen.Selector(
						astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ResourceIdentitySchema"), "Type")),
						"TerraformType",
					),
					astgen.Ident("ctx"),
				),
			)},
		),
		astgen.If(
			astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
			astgen.Block(
				astgen.ExprStmt(astgen.Call(
					astgen.Selector(astgen.Selector(astgen.Ident("result"), "Diagnostics"), "AddError"),
					astgen.Lit(summary),
					astgen.Call(astgen.QualExpr("fmt", "Sprintf"), astgen.Lit("Could not decode list item identity: %s"), astgen.Ident("err")),
				)),
				astgen.If(
					astgen.Unary(token.NOT, astgen.Call(astgen.Ident("push"), astgen.Ident("result"))),
					astgen.Return(),
				),
				astgen.Continue(),
			),
		),
		astgen.AssignStmt(
			[]ast.Expr{astgen.Selector(astgen.Selector(astgen.Ident("result"), "Identity"), "Raw")},
			[]ast.Expr{astgen.Ident("idVal")},
			token.ASSIGN,
		),
		// Populate the full resource only when requested; a decode failure is a
		// per-item warning (the identity still identifies the instance), never a
		// fatal error.
		astgen.If(
			astgen.Selector(astgen.Ident("req"), "IncludeResource"),
			astgen.Block(
				astgen.Assign(
					[]ast.Expr{astgen.Ident("resVal"), astgen.Ident("err")},
					[]ast.Expr{astgen.Call(
						astgen.QualExpr("tftypes", "ValueFromJSON"),
						astgen.Ident("item"),
						astgen.Call(
							astgen.Selector(
								astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "ResourceSchema"), "Type")),
								"TerraformType",
							),
							astgen.Ident("ctx"),
						),
					)},
				),
				astgen.IfElse(
					astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
					astgen.Block(astgen.ExprStmt(astgen.Call(
						astgen.Selector(astgen.Selector(astgen.Ident("result"), "Diagnostics"), "AddWarning"),
						astgen.Lit(summary),
						astgen.Call(astgen.QualExpr("fmt", "Sprintf"), astgen.Lit("Could not decode list item into the resource schema: %s"), astgen.Ident("err")),
					))),
					astgen.Block(astgen.AssignStmt(
						[]ast.Expr{astgen.Selector(astgen.Selector(astgen.Ident("result"), "Resource"), "Raw")},
						[]ast.Expr{astgen.Ident("resVal")},
						token.ASSIGN,
					)),
				),
			),
		),
		astgen.If(
			astgen.Unary(token.NOT, astgen.Call(astgen.Ident("push"), astgen.Ident("result"))),
			astgen.Return(),
		),
	)
	return stmts
}

// identityValueVar returns the local variable name holding an identity
// attribute's probed JSON value, e.g. "petIdValue" for the pet_id attribute.
func identityValueVar(attrName string) string {
	v := naming.GoFieldName(attrName) + "Value"
	if v == "" {
		return "identityValue"
	}
	r := []rune(v)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

// listResourceConfigureDecl returns the Configure method implementing
// list.ListResourceWithConfigure. It type-asserts the provider-configured data
// to the generated API client and stores it on the list resource. The request/
// response types are the resource package's, matching the interface.
func listResourceConfigureDecl(structName string) *ast.FuncDecl {
	return astgen.MethodDecl(
		"Configure", "l", astgen.StarExpr(astgen.Ident(structName)),
		astgen.Params(
			astgen.Field("_", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("req", astgen.QualExpr("resource", "ConfigureRequest"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("resource", "ConfigureResponse")), ""),
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
						"Unexpected List Resource Configure Type",
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
				[]ast.Expr{astgen.Selector(astgen.Ident("l"), "client")},
				[]ast.Expr{astgen.Ident("c")},
				token.ASSIGN,
			),
		),
	)
}
