package generator

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/naming"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/schema"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// This file builds the wired Read body for data sources whose ReadMapping carries
// enough information to emit a real call against the generated API client
// (internal/client). Data sources without a resolvable read mapping, or whose
// response is not a single object, keep their honest scaffold Read body; the
// decision is made once by planDataSourceWiring so partial mappings never produce
// half-wired data sources.

// dataSourceWiringPlan describes how a data source's Read method is wired to the
// generated API client. wired is false when the read mapping cannot be resolved
// against the data source schema, in which case the Read method keeps its honest
// scaffold body. list is true for a list data source (top-level array response),
// whose Read body fetches the collection — following the configured pagination
// strategy — and exposes it as a Computed `items` List attribute (REMAINING_GAPS
// §2/§4).
type dataSourceWiringPlan struct {
	wired bool
	list  bool
	read  crudOperationPlan

	needsStrings bool
	needsStrconv bool
	needsURL     bool
}

// AnyDataSourceWired reports whether at least one data source has a resolvable
// read mapping over a single-object response. It gates emission of the JSON
// conversion helpers so providers with no wired data sources carry no dead code.
func AnyDataSourceWired(dataSources []ir.DataSourceIR) bool {
	for _, ds := range dataSources {
		if planDataSourceWiring(ds).wired {
			return true
		}
	}
	return false
}

// planDataSourceWiring resolves the generation plan for a data source's wired
// Read body. The read mapping's path placeholders and required query/header
// parameters must each resolve to a primitive schema attribute, and the schema
// must carry at least one Computed output attribute (a single-object response
// property, or — for a list data source — the Computed `items` List attribute
// built from a top-level array response). Data sources read their inputs from
// req.Config, so the resolved attributes are the practitioner-set filter
// attributes built by transformer.DataSourceSchema.
func planDataSourceWiring(ds ir.DataSourceIR) dataSourceWiringPlan {
	var plan dataSourceWiringPlan
	if !dataSourceHasComputedOutput(ds) {
		return plan
	}
	read, ok := planDataSourceRead(ds)
	if !ok {
		return plan
	}
	plan.read = read
	plan.wired = true
	plan.list = ds.IsList
	// A list data source wires its Read through client.ListAllPages, which takes
	// a url.Values of query parameters and parses link-header next URLs, so
	// net/url is required. The offset pagination `next` callback advances the
	// page parameter with strconv, so offset style forces strconv even when every
	// parameter is a string.
	if plan.list {
		plan.needsURL = true
		if style, _, _, _ := listPaginationConfig(ds); style == ir.PaginationStyleOffset {
			plan.needsStrconv = true
		}
	}
	// strings.ReplaceAll is only referenced by requestPathStmts for path
	// placeholders; query/header/cookie parameters render through url.Values /
	// http.Header / strconv, so they require strconv (when non-string) but never
	// strings. Setting needsStrings for parameters would import strings unused.
	for _, sub := range read.subs {
		plan.needsStrings = true
		if sub.primitive != ir.TypeString {
			plan.needsStrconv = true
		}
	}
	for _, params := range [][]paramSubstitution{read.queryParams, read.headerParams, read.cookieParams} {
		for _, p := range params {
			if p.primitive != ir.TypeString {
				plan.needsStrconv = true
			}
		}
	}
	return plan
}

// dataSourceHasComputedOutput reports whether the data source schema has at
// least one Computed attribute, i.e. the response contributed output attributes.
// Array responses contribute no output attributes (only filter inputs), so they
// are not wired until array response mapping is implemented.
func dataSourceHasComputedOutput(ds ir.DataSourceIR) bool {
	for _, a := range ds.Schema.Attributes {
		if a.Computed {
			return true
		}
	}
	return false
}

// planDataSourceRead resolves the data source Read operation into a generation
// plan. Path placeholders match a schema attribute by normalized (PascalCase)
// field name — so a placeholder such as {userId} resolves to a snake_case
// `user_id` attribute — with no resource ID fallback, because a data source has
// no designated identifier attribute. Required query/header parameters with no
// matching attribute disable wiring; optional unmapped parameters are skipped.
func planDataSourceRead(ds ir.DataSourceIR) (crudOperationPlan, bool) {
	var planned crudOperationPlan
	planned.method = strings.ToUpper(strings.TrimSpace(ds.ReadMapping.Method))
	planned.template = strings.TrimSpace(ds.ReadMapping.PathTemplate)
	planned.successCodes = ds.ReadMapping.SuccessCodes
	planned.errorMappings = errorMappingDescriptions(ds.ReadMapping.ErrorMappings)
	if planned.method == "" || planned.template == "" {
		return planned, false
	}
	attrs := ds.Schema.Attributes
	for _, placeholder := range pathPlaceholders(planned.template) {
		sub, ok := resolveDataSourcePathSubstitution(attrs, placeholder)
		if !ok {
			return crudOperationPlan{}, false
		}
		planned.subs = append(planned.subs, sub)
	}
	// formData parameters cannot be wired (the generated request body only
	// encodes JSON), so a data source whose read carries formData stays honestly
	// scaffolded (REMAINING_GAPS §2). The transformer emits a fail-loud warning.
	if len(ds.ReadMapping.FormDataParams) > 0 {
		return crudOperationPlan{}, false
	}
	queryParams, qok := resolveParamSubstitutions(attrs, ds.ReadMapping.QueryParams)
	if !qok {
		return crudOperationPlan{}, false
	}
	headerParams, hok := resolveParamSubstitutions(attrs, ds.ReadMapping.HeaderParams)
	if !hok {
		return crudOperationPlan{}, false
	}
	cookieParams, cok := resolveParamSubstitutions(attrs, ds.ReadMapping.CookieParams)
	if !cok {
		return crudOperationPlan{}, false
	}
	planned.queryParams = queryParams
	planned.headerParams = headerParams
	planned.cookieParams = cookieParams
	// Per-operation security (REMAINING_GAPS §1). See applySecurityRequirements.
	applySecurityRequirements(&planned, ds.ReadMapping.SecurityRequirements)
	return planned, true
}

// resolveDataSourcePathSubstitution resolves a path placeholder to a primitive
// schema attribute by normalized (PascalCase) field name. Unlike the resource
// resolver it has no ID-attribute fallback: a data source identifies its target
// through a declared path parameter, so an unresolvable placeholder means the
// schema did not surface that filter and the data source is not wired.
func resolveDataSourcePathSubstitution(attrs []ir.AttributeIR, placeholder string) (pathSubstitution, bool) {
	want := naming.GoFieldName(placeholder)
	for _, attr := range attrs {
		if naming.GoFieldName(attr.Name) != want {
			continue
		}
		if !schema.IsPrimitiveSchema(attr.Schema) {
			return pathSubstitution{}, false
		}
		return pathSubstitution{placeholder: placeholder, field: naming.GoFieldName(attr.Name), primitive: attr.Schema.Type}, true
	}
	return pathSubstitution{}, false
}

// wiredDataSourceReadBody returns the Read body wired to the generated API
// client: it reads the practitioner-supplied filter attributes from the request
// config, issues the read request, and stores the API response as Terraform
// state. A 404 surfaces an error diagnostic rather than silently dropping
// state, because a data source read references an instance the practitioner
// expected to exist.
func wiredDataSourceReadBody(ds ir.DataSourceIR, plan crudOperationPlan, modelName string) []ast.Stmt {
	summary := fmt.Sprintf("Error reading %s", dataSourceTypeName(ds))
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
		clientGuardStmt("d"),
	)
	stmts = append(stmts, requestPathStmts(plan, "config")...)
	// A 404 on a data source read means the referenced instance does not exist:
	// surface an error rather than silently leaving stale state.
	notFound := []ast.Stmt{
		addErrorStmt(summary, astgen.Lit("The requested resource was not found.")),
		astgen.Return(),
	}
	stmts = append(stmts, sendRequestStmts(plan, "d", summary, "config", nil, notFound)...)
	stmts = append(stmts, decodeAndApplyStmts(summary, "config")...)
	stmts = append(stmts, stateSetStmt("config"))
	return stmts
}

// wiredListDataSourceReadBody returns the Read body for a list data source (a read
// whose response is a top-level JSON array). It fetches the collection via the
// generated client's ListAllPages helper, following the configured pagination
// strategy (offset/cursor/link_header) or fetching a single page when pagination
// is disabled (none). Each page body is a JSON array decoded into []any and the
// accumulated elements are exposed as the Computed `items` List attribute by
// applying them to the model through applyJSONToModel with an {"items": ...}
// wrapper, reusing the same JSON-to-attr conversion the single-object Read body
// uses (REMAINING_GAPS §2/§4).
func wiredListDataSourceReadBody(ds ir.DataSourceIR, plan crudOperationPlan, modelName string) []ast.Stmt {
	summary := fmt.Sprintf("Error reading %s", dataSourceTypeName(ds))
	style, pageParam, cursorField, nextLinkRel := listPaginationConfig(ds)

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
		clientGuardStmt("d"),
	)
	stmts = append(stmts, requestPathStmts(plan, "config")...)
	// The query parameters travel in a url.Values that ListAllPages clones and
	// passes to both the fetch closure (which encodes them onto the request) and
	// the next callback (which mutates them to advance the page).
	stmts = append(stmts, astgen.AssignSingle(
		astgen.Ident("params"),
		astgen.CompositeLit(astgen.QualExpr("url", "Values")),
	))
	for _, p := range plan.queryParams {
		stmts = append(stmts, astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Ident("params"), "Set"),
			astgen.Lit(p.name),
			paramValueExpr("config", p),
		)))
	}
	if style == ir.PaginationStyleOffset {
		// Offset pagination starts at page 1; the next callback increments it
		// and stops when a page returns no items.
		stmts = append(stmts, astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Ident("params"), "Set"),
			astgen.Lit(pageParam),
			astgen.Lit("1"),
		)))
	}
	// nextURL carries the link_header next-page URL from the next callback (which
	// extracts it from the response Link header) to the fetch closure (which
	// applies it). It stays empty for the other styles, which drive pagination
	// through params rather than a response-embedded URL.
	stmts = append(stmts,
		astgen.VarDecl("nextURL", "string", nil),
		listFetchAssign("d", plan, "config"),
	)
	if style != ir.PaginationStyleNone {
		stmts = append(stmts, listNextAssign(style, pageParam, cursorField, nextLinkRel))
	}
	// Fetch the pages through the generated client's ListAllPages helper, passing
	// the next callback (or nil when pagination is disabled).
	listArgs := []ast.Expr{astgen.Ident("ctx"), astgen.Ident("params"), astgen.Ident("fetch")}
	if style == ir.PaginationStyleNone {
		listArgs = append(listArgs, astgen.Nil())
	} else {
		listArgs = append(listArgs, astgen.Ident("next"))
	}
	stmts = append(stmts,
		astgen.Assign(
			[]ast.Expr{astgen.Ident("pages"), astgen.Ident("err")},
			[]ast.Expr{astgen.Call(astgen.QualExpr("client", "ListAllPages"), listArgs...)},
		),
		astgen.If(
			astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
			addErrorfStmt(summary, "Could not read list response: %s", astgen.Ident("err")),
			astgen.Return(),
		),
	)
	stmts = append(stmts, listAccumulateStmts(summary)...)
	stmts = append(stmts, stateSetStmt("config"))
	return stmts
}

// listPaginationConfig resolves the pagination strategy for a list data source
// from its IR pagination config, applying the same defaults as the generated
// client's DefaultPagination (style=none, page=page, cursor=cursor, next=next)
// so a list data source with no explicit config fetches a single page. It returns
// the style plus the page parameter name, cursor field, and link-header rel used
// by the next callback. NextLinkHeader is treated as the rel (matching the
// generated client's NextLinkRel config field); the Link header name is fixed.
func listPaginationConfig(ds ir.DataSourceIR) (style, pageParam, cursorField, nextLinkRel string) {
	style = ir.PaginationStyleNone
	pageParam = "page"
	cursorField = "cursor"
	nextLinkRel = "next"
	if ds.Pagination == nil {
		return style, pageParam, cursorField, nextLinkRel
	}
	if ds.Pagination.Style != "" {
		style = ds.Pagination.Style
	}
	if ds.Pagination.PageParam != "" {
		pageParam = ds.Pagination.PageParam
	}
	if ds.Pagination.CursorField != "" {
		cursorField = ds.Pagination.CursorField
	}
	if ds.Pagination.NextLinkHeader != "" {
		nextLinkRel = ds.Pagination.NextLinkHeader
	}
	return style, pageParam, cursorField, nextLinkRel
}

// listFetchAssign emits the `fetch := func(ctx, p) (*http.Response, error) { ... }`
// closure passed to client.ListAllPages. It builds the request from reqPath via
// the generated client (so interceptors and user-agent still apply), then either
// encodes the url.Values query onto it (the first page, and every page for
// offset/cursor styles) or overrides the request URL with the parsed link-header
// next URL (subsequent pages for link_header). Header and cookie parameters are
// applied on every request so they are not lost across pages. receiver is the
// generated method receiver identifier ("d" for data sources, "l" for list
// resources) whose `client` field holds the API client; configVar is the model
// variable header/cookie parameter values are read from.
func listFetchAssign(receiver string, plan crudOperationPlan, configVar string) ast.Stmt {
	fetchBody := []ast.Stmt{ //nolint:prealloc // capacity depends on header/cookie parameter count
		astgen.Assign(
			[]ast.Expr{astgen.Ident("httpReq"), astgen.Ident("err")},
			[]ast.Expr{newRequestCall(receiver, plan, astgen.Nil())},
		),
		astgen.If(
			astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
			astgen.Block(astgen.Return(astgen.Nil(), astgen.Ident("err"))),
		),
		astgen.IfElse(
			astgen.NotEqual(astgen.Ident("nextURL"), astgen.Lit("")),
			astgen.Block(
				astgen.Assign(
					[]ast.Expr{astgen.Ident("parsed"), astgen.Ident("perr")},
					[]ast.Expr{astgen.Call(astgen.QualExpr("url", "Parse"), astgen.Ident("nextURL"))},
				),
				astgen.If(
					astgen.NotEqual(astgen.Ident("perr"), astgen.Nil()),
					astgen.Block(astgen.Return(astgen.Nil(), astgen.Ident("perr"))),
				),
				astgen.AssignStmt(
					[]ast.Expr{astgen.Selector(astgen.Ident("httpReq"), "URL")},
					[]ast.Expr{astgen.Ident("parsed")},
					token.ASSIGN,
				),
			),
			astgen.Block(
				astgen.AssignStmt(
					[]ast.Expr{astgen.Selector(astgen.Selector(astgen.Ident("httpReq"), "URL"), "RawQuery")},
					[]ast.Expr{astgen.Call(astgen.Selector(astgen.Ident("p"), "Encode"))},
					token.ASSIGN,
				),
			),
		),
	}
	fetchBody = append(fetchBody, requestHeaderStmts(plan, configVar)...)
	fetchBody = append(fetchBody, requestCookieStmts(plan, configVar)...)
	fetchBody = append(fetchBody, astgen.Return(
		astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident(receiver), "client"), "Do"), astgen.Ident("httpReq")),
	))
	return astgen.AssignSingle(
		astgen.Ident("fetch"),
		astgen.FuncLit(
			astgen.FuncType(
				astgen.Params(
					astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
					astgen.Field("p", astgen.QualExpr("url", "Values"), ""),
				),
				astgen.Results(
					astgen.Field("", astgen.StarExpr(astgen.QualExpr("http", "Response")), ""),
					astgen.Field("", astgen.Ident("error"), ""),
				),
			),
			astgen.Block(fetchBody...),
		),
	)
}

// listNextAssign emits the `next := func(resp, body, p) bool { ... }` closure
// passed to client.ListAllPages, specialized to the configured pagination
// style. Returning true advances to the next page; false stops iteration.
func listNextAssign(style, pageParam, cursorField, nextLinkRel string) ast.Stmt {
	var body []ast.Stmt
	switch style {
	case ir.PaginationStyleOffset:
		body = listOffsetNextBody(pageParam)
	case ir.PaginationStyleCursor:
		body = listCursorNextBody(cursorField)
	case ir.PaginationStyleLinkHeader:
		body = listLinkHeaderNextBody(nextLinkRel)
	default:
		body = []ast.Stmt{astgen.Return(astgen.BoolLit(false))}
	}
	return astgen.AssignSingle(
		astgen.Ident("next"),
		astgen.FuncLit(
			astgen.FuncType(
				astgen.Params(
					astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("http", "Response")), ""),
					astgen.Field("body", astgen.SliceType(astgen.Ident("byte")), ""),
					astgen.Field("p", astgen.QualExpr("url", "Values"), ""),
				),
				astgen.Results(astgen.Field("", astgen.Ident("bool"), "")),
			),
			astgen.Block(body...),
		),
	)
}

// listOffsetNextBody advances offset pagination by incrementing the page
// parameter, stopping when a page returns no items (decoded from the body so
// trailing whitespace such as a trailing newline does not defeat the check).
func listOffsetNextBody(pageParam string) []ast.Stmt {
	return []ast.Stmt{
		astgen.AssignSingle(
			astgen.Ident("pageItems"),
			astgen.CompositeLit(astgen.SliceType(astgen.Ident("any"))),
		),
		&ast.IfStmt{
			Init: astgen.AssignSingle(astgen.Ident("unmarshalErr"), astgen.Call(
				astgen.QualExpr("json", "Unmarshal"), astgen.Ident("body"), astgen.UnaryPtr(astgen.Ident("pageItems")),
			)),
			Cond: astgen.NotEqual(astgen.Ident("unmarshalErr"), astgen.Nil()),
			Body: astgen.Block(astgen.Return(astgen.BoolLit(false))),
		},
		astgen.If(
			astgen.Equal(astgen.Call(astgen.Ident("len"), astgen.Ident("pageItems")), astgen.IntLit(0)),
			astgen.Block(astgen.Return(astgen.BoolLit(false))),
		),
		// page and parseErr are declared at the closure scope (not the if init
		// scope) so page remains visible to the strconv.Itoa call below.
		astgen.Assign(
			[]ast.Expr{astgen.Ident("page"), astgen.Ident("parseErr")},
			[]ast.Expr{astgen.Call(
				astgen.QualExpr("strconv", "Atoi"),
				astgen.Call(astgen.Selector(astgen.Ident("p"), "Get"), astgen.Lit(pageParam)),
			)},
		),
		astgen.If(
			astgen.NotEqual(astgen.Ident("parseErr"), astgen.Nil()),
			astgen.Block(astgen.Return(astgen.BoolLit(false))),
		),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Ident("p"), "Set"),
			astgen.Lit(pageParam),
			astgen.Call(astgen.QualExpr("strconv", "Itoa"), astgen.Binary(astgen.Ident("page"), token.ADD, astgen.IntLit(1))),
		)),
		astgen.Return(astgen.BoolLit(true)),
	}
}

// listCursorNextBody advances cursor pagination by reading the cursor token from
// the response field named by cursorField and sending it back as the query
// parameter of the same name, stopping when the response carries no cursor.
func listCursorNextBody(cursorField string) []ast.Stmt {
	return []ast.Stmt{
		astgen.AssignSingle(
			astgen.Ident("page"),
			astgen.CompositeLit(astgen.MapType(astgen.Ident("string"), astgen.Ident("any"))),
		),
		&ast.IfStmt{
			Init: astgen.AssignSingle(astgen.Ident("unmarshalErr"), astgen.Call(
				astgen.QualExpr("json", "Unmarshal"), astgen.Ident("body"), astgen.UnaryPtr(astgen.Ident("page")),
			)),
			Cond: astgen.NotEqual(astgen.Ident("unmarshalErr"), astgen.Nil()),
			Body: astgen.Block(astgen.Return(astgen.BoolLit(false))),
		},
		astgen.Assign(
			[]ast.Expr{astgen.Ident("cursor"), astgen.Ident("ok")},
			[]ast.Expr{astgen.TypeAssertExpr(
				astgen.IndexExpr(astgen.Ident("page"), astgen.Lit(cursorField)),
				astgen.Ident("string"),
			)},
		),
		astgen.If(
			astgen.Binary(
				astgen.Unary(token.NOT, astgen.Ident("ok")),
				token.LOR,
				astgen.Equal(astgen.Ident("cursor"), astgen.Lit("")),
			),
			astgen.Block(astgen.Return(astgen.BoolLit(false))),
		),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Ident("p"), "Set"),
			astgen.Lit(cursorField),
			astgen.Ident("cursor"),
		)),
		astgen.Return(astgen.BoolLit(true)),
	}
}

// listLinkHeaderNextBody advances link_header pagination by extracting the next
// page URL from the response Link header and storing it on the shared nextURL
// variable, which the fetch closure applies on the next request. Iteration
// stops when no next link is present.
func listLinkHeaderNextBody(nextLinkRel string) []ast.Stmt {
	return []ast.Stmt{
		astgen.AssignStmt(
			[]ast.Expr{astgen.Ident("nextURL")},
			[]ast.Expr{astgen.Call(
				astgen.QualExpr("client", "ExtractLinkHeader"),
				astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Header"), "Get"), astgen.Lit("Link")),
				astgen.Lit(nextLinkRel),
			)},
			token.ASSIGN,
		),
		astgen.Return(astgen.NotEqual(astgen.Ident("nextURL"), astgen.Lit(""))),
	}
}

// listAccumulateStmts emits the loop that decodes each fetched page (a JSON
// array) into []any, appends its elements to an accumulator, and applies the
// accumulated collection to the model as the `items` List attribute by wrapping
// it in an {"items": items} object for applyJSONToModel.
func listAccumulateStmts(summary string) []ast.Stmt {
	return []ast.Stmt{
		astgen.AssignSingle(
			astgen.Ident("items"),
			astgen.CompositeLit(astgen.SliceType(astgen.Ident("any"))),
		),
		astgen.RangeStmt(
			astgen.Ident("_"), astgen.Ident("page"), token.DEFINE, astgen.Ident("pages"),
			astgen.Block(
				astgen.AssignSingle(
					astgen.Ident("pageItems"),
					astgen.CompositeLit(astgen.SliceType(astgen.Ident("any"))),
				),
				&ast.IfStmt{
					Init: astgen.AssignSingle(astgen.Ident("err"), astgen.Call(
						astgen.QualExpr("json", "Unmarshal"), astgen.Ident("page"), astgen.UnaryPtr(astgen.Ident("pageItems")),
					)),
					Cond: astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
					Body: astgen.Block(
						addErrorfStmt(summary, "Could not decode list page: %s", astgen.Ident("err")),
						astgen.Return(),
					),
				},
				astgen.AssignStmt(
					[]ast.Expr{astgen.Ident("items")},
					[]ast.Expr{astgen.Call(astgen.Ident("append"), astgen.Ident("items"), astgen.Ellipsis(astgen.Ident("pageItems")))},
					token.ASSIGN,
				),
			),
		),
		&ast.IfStmt{
			Init: astgen.AssignSingle(astgen.Ident("err"), astgen.Call(
				astgen.Ident("applyJSONToModel"),
				astgen.UnaryPtr(astgen.Ident("config")),
				astgen.CompositeLit(
					astgen.MapType(astgen.Ident("string"), astgen.Ident("any")),
					astgen.KeyValueExpr(astgen.Lit("items"), astgen.Ident("items")),
				),
			)),
			Cond: astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
			Body: astgen.Block(
				addErrorfStmt(summary, "Could not map response to state: %s", astgen.Ident("err")),
				astgen.Return(),
			),
		},
	}
}

// dataSourceConfigureDecl returns the Configure method implementing
// datasource.DataSourceWithConfigure. It type-asserts the provider-configured
// data to the generated API client and stores it on the data source, mirroring
// resourceConfigureDecl over the data-source provider-data channel
// (resp.DataSourceData in the provider's Configure).
func dataSourceConfigureDecl(structName string) *ast.FuncDecl {
	return astgen.MethodDecl(
		"Configure", "d", astgen.StarExpr(astgen.Ident(structName)),
		astgen.Params(
			astgen.Field("_", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("req", astgen.QualExpr("datasource", "ConfigureRequest"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("datasource", "ConfigureResponse")), ""),
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
						"Unexpected Data Source Configure Type",
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
				[]ast.Expr{astgen.Selector(astgen.Ident("d"), "client")},
				[]ast.Expr{astgen.Ident("c")},
				token.ASSIGN,
			),
		),
	)
}
