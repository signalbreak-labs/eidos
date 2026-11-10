package generator

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/ir"
	"github.com/signalbreak-labs/eidos/pkg/transformer"
)

// Coverage tests for ETag conditional requests (resource_etag.go). The
// shared taxonomy in buildResourceCoverageCases is adapted to the extended
// helper signatures, and dedicated cases pin each conditional branch: the
// If-Match / If-None-Match headers actually sent, the "*" fallback, the ETag
// captured from the header or the body, and the 412 / 428 diagnostics.

// adaptETagCoverageCases passes an empty ifMatch to helpers that take one and
// keeps readRemote's removed capture compiling once it also returns the ETag.
func adaptETagCoverageCases(cases []coverageCase, plan resourceWiringPlan) []coverageCase {
	for i := range cases {
		c := &cases[i]
		var op crudOperationPlan
		switch c.method {
		case "createRemote":
			op = plan.create
		case "readRemote":
			op = plan.read
		case "updateRemote":
			op = plan.updateOp
		case "deleteRemote":
			op = plan.delete
		default:
			continue
		}
		if op.ifMatchParam && c.args == nil {
			c.args = []ast.Expr{astgen.Lit("")}
		}
		if c.method == "readRemote" && op.captureETag && c.captureReturn && c.lhs == nil {
			c.lhs = []string{"removed", "_"}
		}
	}
	return cases
}

// etagMockSpec describes an inline httptest handler: it asserts one request
// header (when checkHeader is set), optionally sets an ETag response header,
// and replies with status and body.
type etagMockSpec struct {
	checkHeader string
	wantHeader  string
	respETag    string
	status      int
	body        string
}

// etagMockClient returns newMockClient(t, func(w, req) {...}) for spec.
func etagMockClient(spec etagMockSpec) ast.Expr {
	var body []ast.Stmt
	if spec.checkHeader != "" {
		got := astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("req"), "Header"), "Get"), astgen.Lit(spec.checkHeader))
		body = append(body, &ast.IfStmt{
			Init: astgen.AssignSingle(astgen.Ident("got"), got),
			Cond: astgen.NotEqual(astgen.Ident("got"), astgen.Lit(spec.wantHeader)),
			Body: astgen.Block(astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("t"), "Errorf"),
				astgen.Lit(spec.checkHeader+" = %q, want %q"), astgen.Ident("got"), astgen.Lit(spec.wantHeader)))),
		})
	}
	if spec.respETag != "" {
		body = append(body, astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Call(astgen.Selector(astgen.Ident("w"), "Header")), "Set"),
			astgen.Lit("ETag"), astgen.Lit(spec.respETag),
		)))
	}
	body = append(body,
		astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("w"), "WriteHeader"), astgen.IntLit(spec.status))),
		astgen.AssignStmt(
			[]ast.Expr{astgen.Ident("_"), astgen.Ident("_")},
			[]ast.Expr{astgen.Call(astgen.Selector(astgen.Ident("w"), "Write"), byteSliceCall(astgen.Lit(spec.body)))},
			token.ASSIGN,
		),
	)
	handler := astgen.FuncLit(
		astgen.FuncType(
			astgen.Params(
				astgen.Field("w", astgen.QualExpr("http", "ResponseWriter"), ""),
				astgen.Field("req", astgen.StarExpr(astgen.QualExpr("http", "Request")), ""),
			),
			astgen.Results(),
		),
		astgen.Block(body...),
	)
	return astgen.Call(astgen.Ident("newMockClient"), astgen.Ident("t"), handler)
}

// etagEqualsStmt asserts that the captured etag variable equals want.
func etagEqualsStmt(want string) ast.Stmt {
	return astgen.If(
		astgen.NotEqual(astgen.Ident(etagResultName), astgen.Lit(want)),
		astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("t"), "Fatalf"),
			astgen.Lit("etag = %q, want %q"), astgen.Ident(etagResultName), astgen.Lit(want))),
	)
}

// etagResultLHS returns the assignment targets capturing a helper's etag
// result (readRemote's removed result is discarded).
func etagResultLHS(method string) []string {
	if method == "readRemote" {
		return []string{"_", etagResultName}
	}
	return []string{etagResultName}
}

// etagBodyJSON returns the happy-path identifier body (idBodyJSON) with the
// body ETag property added. Keys are sorted by encoding/json, keeping the
// generated test deterministic.
func etagBodyJSON(wire, etag string, info idFieldInfo) string {
	m := map[string]any{}
	if err := json.Unmarshal([]byte(idBodyJSON(info)), &m); err != nil {
		m = map[string]any{}
	}
	m[wire] = etag
	out, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(out)
}

// etagCoverageCases returns the conditional-request coverage cases for a
// resource; none for a resource without conditional requests.
func etagCoverageCases(r ir.ResourceIR, plan resourceWiringPlan, info idFieldInfo) []coverageCase {
	var cases []coverageCase
	add := func(c coverageCase) { cases = append(cases, c) }
	createBody := idBodyJSON(info)
	statusClient := func(status int) ast.Expr {
		return astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(status), astgen.Lit(`{"message":"precondition"}`))
	}
	errorSummary := func(verb string) string { return fmt.Sprintf("Error %s %s", verb, resourceTypeName(r)) }

	// Create.
	if plan.create.ifNoneMatch {
		add(coverageCase{suffix: "Create_SendsIfNoneMatch", method: "createRemote", resp: "CreateResponse", needsHTTP: true,
			intent:  `the create request carries "If-None-Match: *" so it cannot overwrite an existing object`,
			client:  etagMockClient(etagMockSpec{checkHeader: transformer.HeaderIfNoneMatch, wantHeader: "*", status: happyStatus(plan.create), body: createBody}),
			asserts: []ast.Stmt{requireNoErrorsStmt()},
		})
		add(coverageCase{suffix: "Create_PreconditionFailed", method: "createRemote", resp: "CreateResponse",
			intent:  "412 surfaces the already-exists diagnostic pointing at terraform import",
			client:  statusClient(412),
			asserts: []ast.Stmt{hasErrorContainingStmt(errorSummary("creating")), hasErrorContainingStmt("already exists at this location")},
		})
	}
	if plan.create.captureETag {
		add(coverageCase{suffix: "Create_CapturesETag", method: "createRemote", resp: "CreateResponse", needsHTTP: true,
			intent:  "the ETag response header is returned for private state",
			client:  etagMockClient(etagMockSpec{respETag: `"c1"`, status: happyStatus(plan.create), body: createBody}),
			lhs:     etagResultLHS("createRemote"),
			asserts: []ast.Stmt{requireNoErrorsStmt(), etagEqualsStmt(`"c1"`)},
		})
	}

	// Read.
	if plan.read.captureETag {
		plainRead := plan.read.etagFromHeader
		if plainRead {
			add(coverageCase{suffix: "Read_CapturesETag", method: "readRemote", resp: "ReadResponse", needsHTTP: true,
				intent:  "the ETag response header is returned for private state",
				client:  etagMockClient(etagMockSpec{respETag: `W/"r1"`, status: happyStatus(plan.read), body: `{}`}),
				lhs:     etagResultLHS("readRemote"),
				asserts: []ast.Stmt{requireNoErrorsStmt(), etagEqualsStmt(`W/"r1"`)},
			})
			add(coverageCase{suffix: "Read_NoETag", method: "readRemote", resp: "ReadResponse",
				intent:  "a response without an ETag returns an empty ETag so the stored one is cleared",
				client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(happyStatus(plan.read)), astgen.Lit(`{}`)),
				lhs:     etagResultLHS("readRemote"),
				asserts: []ast.Stmt{requireNoErrorsStmt(), etagEqualsStmt("")},
			})
		}
		if plan.etagBodyField != "" && plainRead {
			add(coverageCase{suffix: "Read_ETagFromBody", method: "readRemote", resp: "ReadResponse",
				intent:  "with no ETag header the body's ETag property is used",
				client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(happyStatus(plan.read)), astgen.Lit(etagBodyJSON(plan.etagBodyWire, `"b1"`, info))),
				lhs:     etagResultLHS("readRemote"),
				asserts: []ast.Stmt{requireNoErrorsStmt(), etagEqualsStmt(`"b1"`)},
			})
		}
	}

	// Update.
	if plan.update {
		op := plan.updateOp
		if op.ifMatchParam {
			add(coverageCase{suffix: "Update_SendsIfMatch", method: "updateRemote", resp: "UpdateResponse", needsHTTP: true,
				intent:  "the stored ETag is sent as If-Match and the new ETag is returned",
				client:  etagMockClient(etagMockSpec{checkHeader: transformer.HeaderIfMatch, wantHeader: `"u0"`, respETag: `"u1"`, status: happyStatus(op), body: `{}`}),
				args:    []ast.Expr{astgen.Lit(`"u0"`)},
				lhs:     etagResultLHS("updateRemote"),
				asserts: []ast.Stmt{requireNoErrorsStmt(), etagEqualsStmt(`"u1"`)},
			})
			if op.ifMatchRequired {
				add(coverageCase{suffix: "Update_IfMatchWildcard", method: "updateRemote", resp: "UpdateResponse", needsHTTP: true,
					intent:  `with no stored ETag the required If-Match is sent as "*"`,
					client:  etagMockClient(etagMockSpec{checkHeader: transformer.HeaderIfMatch, wantHeader: "*", status: happyStatus(op), body: `{}`}),
					args:    []ast.Expr{astgen.Lit("")},
					asserts: []ast.Stmt{requireNoErrorsStmt()},
				})
			} else {
				add(coverageCase{suffix: "Update_NoIfMatchWithoutETag", method: "updateRemote", resp: "UpdateResponse", needsHTTP: true,
					intent:  "with no stored ETag the optional If-Match is omitted",
					client:  etagMockClient(etagMockSpec{checkHeader: transformer.HeaderIfMatch, wantHeader: "", status: happyStatus(op), body: `{}`}),
					args:    []ast.Expr{astgen.Lit("")},
					asserts: []ast.Stmt{requireNoErrorsStmt()},
				})
			}
			if plan.etagBodyField != "" {
				add(coverageCase{suffix: "Update_ETagFromBody", method: "updateRemote", resp: "UpdateResponse",
					intent:  "with no ETag header a new body ETag is used",
					client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(happyStatus(op)), astgen.Lit(etagBodyJSON(plan.etagBodyWire, `"u1"`, info))),
					args:    []ast.Expr{astgen.Lit(`"u0"`)},
					lhs:     etagResultLHS("updateRemote"),
					asserts: []ast.Stmt{requireNoErrorsStmt(), etagEqualsStmt(`"u1"`)},
				})
				add(coverageCase{suffix: "Update_StaleBodyETagIgnored", method: "updateRemote", resp: "UpdateResponse",
					intent:  "a body ETag equal to the If-Match just sent is stale and is not stored",
					client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(happyStatus(op)), astgen.Lit(etagBodyJSON(plan.etagBodyWire, `"u0"`, info))),
					args:    []ast.Expr{astgen.Lit(`"u0"`)},
					lhs:     etagResultLHS("updateRemote"),
					asserts: []ast.Stmt{requireNoErrorsStmt(), etagEqualsStmt("")},
				})
			}
		} else if op.ifMatchRequired && op.ifMatch {
			add(coverageCase{suffix: "Update_IfMatchWildcard", method: "updateRemote", resp: "UpdateResponse", needsHTTP: true,
				intent:  `with ETag tracking disabled the required If-Match is sent as "*"`,
				client:  etagMockClient(etagMockSpec{checkHeader: transformer.HeaderIfMatch, wantHeader: "*", status: happyStatus(op), body: `{}`}),
				asserts: []ast.Stmt{requireNoErrorsStmt()},
			})
		}
		if op.captureETag && !op.ifMatchParam {
			add(coverageCase{suffix: "Update_CapturesETag", method: "updateRemote", resp: "UpdateResponse", needsHTTP: true,
				intent:  "the ETag response header is returned for private state",
				client:  etagMockClient(etagMockSpec{respETag: `"u1"`, status: happyStatus(op), body: `{}`}),
				lhs:     etagResultLHS("updateRemote"),
				asserts: []ast.Stmt{requireNoErrorsStmt(), etagEqualsStmt(`"u1"`)},
			})
		}
		if op.ifMatch {
			add(coverageCase{suffix: "Update_PreconditionFailed", method: "updateRemote", resp: "UpdateResponse",
				intent:  "412 surfaces the changed-outside-Terraform diagnostic",
				client:  statusClient(412),
				args:    ifMatchArgs(op, `"stale"`),
				asserts: []ast.Stmt{hasErrorContainingStmt(errorSummary("updating")), hasErrorContainingStmt("changed since Terraform last read it")},
			})
			add(coverageCase{suffix: "Update_PreconditionRequired", method: "updateRemote", resp: "UpdateResponse",
				intent:  "428 surfaces the refresh-to-record-the-ETag diagnostic",
				client:  statusClient(428),
				args:    ifMatchArgs(op, ""),
				asserts: []ast.Stmt{hasErrorContainingStmt("requires a conditional request")},
			})
		}
	}

	// Delete.
	op := plan.delete
	if op.ifMatchParam {
		add(coverageCase{suffix: "Delete_SendsIfMatch", method: "deleteRemote", resp: "DeleteResponse", needsHTTP: true,
			intent:  "the stored ETag is sent as If-Match",
			client:  etagMockClient(etagMockSpec{checkHeader: transformer.HeaderIfMatch, wantHeader: `"d0"`, status: happyStatus(op), body: ``}),
			args:    []ast.Expr{astgen.Lit(`"d0"`)},
			asserts: []ast.Stmt{requireNoErrorsStmt()},
		})
	}
	if op.ifMatch && op.ifMatchRequired {
		add(coverageCase{suffix: "Delete_IfMatchWildcard", method: "deleteRemote", resp: "DeleteResponse", needsHTTP: true,
			intent:  `with no stored ETag the required If-Match is sent as "*"`,
			client:  etagMockClient(etagMockSpec{checkHeader: transformer.HeaderIfMatch, wantHeader: "*", status: happyStatus(op), body: ``}),
			args:    ifMatchArgs(op, ""),
			asserts: []ast.Stmt{requireNoErrorsStmt()},
		})
	}
	if op.ifMatch {
		add(coverageCase{suffix: "Delete_PreconditionFailed", method: "deleteRemote", resp: "DeleteResponse",
			intent:  "412 surfaces the changed-outside-Terraform diagnostic",
			client:  statusClient(412),
			args:    ifMatchArgs(op, `"stale"`),
			asserts: []ast.Stmt{hasErrorContainingStmt(errorSummary("deleting")), hasErrorContainingStmt("changed since Terraform last read it")},
		})
		add(coverageCase{suffix: "Delete_PreconditionRequired", method: "deleteRemote", resp: "DeleteResponse",
			intent:  "428 surfaces the refresh-to-record-the-ETag diagnostic",
			client:  statusClient(428),
			args:    ifMatchArgs(op, ""),
			asserts: []ast.Stmt{hasErrorContainingStmt("requires a conditional request")},
		})
	}
	return cases
}

// ifMatchArgs returns the ifMatch argument for a helper that takes one.
func ifMatchArgs(op crudOperationPlan, value string) []ast.Expr {
	if !op.ifMatchParam {
		return nil
	}
	return []ast.Expr{astgen.Lit(value)}
}
