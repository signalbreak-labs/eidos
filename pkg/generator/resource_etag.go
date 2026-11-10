package generator

import (
	"go/ast"
	"go/token"
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/naming"
	"github.com/signalbreak-labs/eidos/pkg/ir"
	"github.com/signalbreak-labs/eidos/pkg/transformer"
)

// ETag optimistic concurrency for wired managed resources (RFC 9110 §13.1).
//
// When a resource tracks ETags (transformer.PlanConditionalRequests), the
// generated *Remote helpers return the ETag of each Create/Read/Update
// response, the framework methods store it in the resource's private state
// (never in the practitioner-visible schema), and Update/Delete send it back
// as If-Match. A required If-Match with no stored ETag is sent as "*". A
// create declaring If-None-Match sends "If-None-Match: *". 412 and 428
// responses get dedicated, actionable diagnostics.
//
// The helpers carry the ETag as explicit parameters and results rather than
// touching resp.Private themselves, so the coverage tests can exercise every
// branch: the framework's private-state type lives in an internal package
// that generated tests cannot construct.

// Diagnostic details for conditional request failures. They are rendered as
// "<detail>: <API error>" by apiErrorDetailStmts.
const (
	etagCreateConflictDetail = "An object already exists at this location (HTTP 412 Precondition Failed for If-None-Match: *), so creating it would overwrite it. Import the existing object with terraform import instead of creating a new one"
	etagUpdateConflictDetail = "The remote object changed since Terraform last read it (HTTP 412 Precondition Failed: the If-Match ETag no longer matches), so the update was rejected instead of overwriting those changes. Refresh with terraform apply -refresh-only, review the remote changes, then apply again"
	etagDeleteConflictDetail = "The remote object changed since Terraform last read it (HTTP 412 Precondition Failed: the If-Match ETag no longer matches), so the delete was rejected (an object already deleted outside Terraform can also fail this way). Refresh with terraform apply -refresh-only, which also drops an already-deleted object from state, review the remote changes, then destroy again"
	etagRequiredDetail       = "The API requires a conditional request (HTTP 428 Precondition Required) but no ETag is known for this object. Refresh with terraform apply -refresh-only so its current ETag is recorded, then try again"
)

// ETag statement identifiers shared by the helper bodies and their callers.
const (
	etagResultName  = "etag"    // named result of create/read/updateRemote
	ifMatchParamVar = "ifMatch" // updateRemote/deleteRemote parameter
)

// bodyETagWireNames are the response-body property names treated as the
// object's ETag when the response carries no ETag header (Azure Cosmos DB's
// _etag, OData's @odata.etag, and the common plain etag).
var bodyETagWireNames = []string{"etag", "_etag", "@odata.etag"}

// applyConditionalPlan resolves the resource's conditional-request behavior
// onto the wired operation plans. It runs after the operations are planned so
// it can see whether Update is wired and whether Read targets a collection.
func applyConditionalPlan(r ir.ResourceIR, plan *resourceWiringPlan) {
	cp := transformer.PlanConditionalRequests(r)
	if !cp.Any() {
		return
	}
	updateSends := plan.update && cp.UpdateIfMatch
	// Tracking only pays off when something sends the ETag back.
	track := cp.TrackETag && (updateSends || cp.DeleteIfMatch)
	plan.trackETag = track

	plan.create.ifNoneMatch = cp.CreateIfNoneMatch
	if updateSends {
		plan.updateOp.ifMatch = true
		plan.updateOp.ifMatchParam = track
		plan.updateOp.ifMatchRequired = cp.UpdateIfMatchRequired
	}
	if cp.DeleteIfMatch {
		plan.delete.ifMatch = true
		plan.delete.ifMatchParam = track
		plan.delete.ifMatchRequired = cp.DeleteIfMatchRequired
	}
	if track {
		plan.etagBodyField, plan.etagBodyWire = bodyETagField(r)
		plan.create.captureETag = true
		plan.create.etagFromHeader = true
		plan.read.captureETag = true
		// A collection GET (or a parent GET for a child resource) returns the
		// ETag of the collection, not of this object, so only a body ETag on
		// the selected element can be trusted.
		plan.read.etagFromHeader = !plan.read.responseIsCollection && plan.read.nestedCollectionPath == ""
		if plan.update {
			plan.updateOp.captureETag = true
			plan.updateOp.etagFromHeader = true
		}
	}

	if plan.create.ifNoneMatch {
		addConditionalErrorMapping(&plan.create, 412, etagCreateConflictDetail)
	}
	if plan.updateOp.ifMatch {
		addConditionalErrorMapping(&plan.updateOp, 412, etagUpdateConflictDetail)
		addConditionalErrorMapping(&plan.updateOp, 428, etagRequiredDetail)
	}
	if plan.delete.ifMatch {
		addConditionalErrorMapping(&plan.delete, 412, etagDeleteConflictDetail)
		addConditionalErrorMapping(&plan.delete, 428, etagRequiredDetail)
	}
}

// addConditionalErrorMapping installs the actionable diagnostic for a
// conditional-request status code, keeping the spec's own description (when
// it declares one) after the provider's explanation.
func addConditionalErrorMapping(op *crudOperationPlan, code int, detail string) {
	m := make(map[int]string, len(op.errorMappings)+1)
	for k, v := range op.errorMappings {
		m[k] = v
	}
	if spec := strings.TrimSpace(m[code]); spec != "" {
		detail += " (API: " + strings.ReplaceAll(spec, "%", "%%") + ")"
	}
	m[code] = detail
	op.errorMappings = m
}

// bodyETagField returns the model field name and JSON wire name of a
// top-level string attribute that carries the object's ETag in the response
// body (wire name etag, _etag, or @odata.etag, case-insensitive), or empty
// strings when there is none.
func bodyETagField(r ir.ResourceIR) (field, wire string) {
	for _, a := range r.Schema.Attributes {
		if a.Schema.Type != ir.TypeString || a.Schema.Collection != nil || len(a.Schema.Attributes) > 0 || a.WriteOnly || a.Schema.WriteOnly || a.WirePath != "" {
			continue
		}
		wire := a.WireName
		if wire == "" {
			wire = a.Name
		}
		for _, n := range bodyETagWireNames {
			if strings.EqualFold(wire, n) {
				return naming.GoFieldName(a.Name), wire
			}
		}
	}
	return "", ""
}

// AnyResourceTracksETag reports whether any wired resource stores ETags in
// private state, gating the internal/provider/etag.go helpers.
func AnyResourceTracksETag(resources []ir.ResourceIR) bool {
	for _, r := range resources {
		if p := planResourceWiring(r); p.wired && p.trackETag {
			return true
		}
	}
	return false
}

// conditionalHeaderStmts emits the If-Match / If-None-Match request headers
// for a wired operation, after the request is built.
func conditionalHeaderStmts(op crudOperationPlan) []ast.Stmt {
	var stmts []ast.Stmt
	set := func(name string, value ast.Expr) ast.Stmt {
		return astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("httpReq"), "Header"), "Set"),
			astgen.Lit(name), value,
		))
	}
	if op.ifNoneMatch {
		// Create only when no object exists at the target.
		stmts = append(stmts, set(transformer.HeaderIfNoneMatch, astgen.Lit("*")))
	}
	if op.ifMatch {
		switch {
		case op.ifMatchParam && op.ifMatchRequired:
			// The API requires If-Match: send the stored ETag, or "*" (any
			// current version) when none is known, e.g. for state written
			// before ETags were tracked.
			stmts = append(stmts, astgen.IfElse(
				astgen.NotEqual(astgen.Ident(ifMatchParamVar), astgen.Lit("")),
				astgen.Block(set(transformer.HeaderIfMatch, astgen.Ident(ifMatchParamVar))),
				astgen.Block(set(transformer.HeaderIfMatch, astgen.Lit("*"))),
			))
		case op.ifMatchParam:
			stmts = append(stmts, astgen.If(
				astgen.NotEqual(astgen.Ident(ifMatchParamVar), astgen.Lit("")),
				set(transformer.HeaderIfMatch, astgen.Ident(ifMatchParamVar)),
			))
		case op.ifMatchRequired:
			// ETag tracking is disabled; satisfy the required header.
			stmts = append(stmts, set(transformer.HeaderIfMatch, astgen.Lit("*")))
		}
	}
	return stmts
}

// etagCaptureStmts assigns the named etag result from the response: the ETag
// header (when trustworthy for this operation), falling back to the body ETag
// attribute just applied to modelVar. For an update the body fallback is only
// taken when it differs from the If-Match just sent, because a body-less
// response leaves the prior state's (now stale) value in the model.
func etagCaptureStmts(op crudOperationPlan, modelVar, bodyField string) []ast.Stmt {
	if !op.captureETag {
		return nil
	}
	var stmts []ast.Stmt
	if op.etagFromHeader {
		stmts = append(stmts, astgen.AssignStmt(
			[]ast.Expr{astgen.Ident(etagResultName)},
			[]ast.Expr{astgen.Call(astgen.Ident("responseETag"), astgen.Ident("httpResp"))},
			token.ASSIGN,
		))
	}
	isUpdate := op.method == "PUT" || op.method == "PATCH"
	if bodyField == "" || (isUpdate && !op.ifMatchParam) {
		return stmts
	}
	field := astgen.Selector(astgen.Ident(modelVar), bodyField)
	cond := ast.Expr(astgen.Binary(
		astgen.Unary(token.NOT, astgen.Call(astgen.Selector(field, "IsNull"))),
		token.LAND,
		astgen.Unary(token.NOT, astgen.Call(astgen.Selector(field, "IsUnknown"))),
	))
	if op.etagFromHeader {
		cond = astgen.Binary(astgen.Equal(astgen.Ident(etagResultName), astgen.Lit("")), token.LAND, cond)
	}
	if isUpdate {
		cond = astgen.Binary(cond, token.LAND, astgen.NotEqual(astgen.Call(astgen.Selector(field, "ValueString")), astgen.Ident(ifMatchParamVar)))
	}
	stmts = append(stmts, astgen.If(cond, astgen.AssignStmt(
		[]ast.Expr{astgen.Ident(etagResultName)},
		[]ast.Expr{astgen.Call(astgen.Selector(field, "ValueString"))},
		token.ASSIGN,
	)))
	return stmts
}

// remoteHelperResults returns the result list of a *Remote helper: the
// existing results plus the named etag result when the operation captures it.
func remoteHelperResults(op crudOperationPlan, existing ...*ast.Field) *ast.FieldList {
	fields := append([]*ast.Field(nil), existing...)
	if op.captureETag {
		fields = append(fields, astgen.Field(etagResultName, astgen.Ident("string"), ""))
	}
	return astgen.Results(fields...)
}

// remoteHelperParams returns the parameter list of a *Remote helper, adding
// the ifMatch parameter before resp when the operation sends a stored ETag.
func remoteHelperParams(op crudOperationPlan, modelParam string, modelType, respType ast.Expr) *ast.FieldList {
	fields := []*ast.Field{
		astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
		astgen.Field(modelParam, modelType, ""),
	}
	if op.ifMatchParam {
		fields = append(fields, astgen.Field(ifMatchParamVar, astgen.Ident("string"), ""))
	}
	fields = append(fields, astgen.Field("resp", respType, ""))
	return astgen.Params(fields...)
}

// remoteHelperArgs returns the call arguments for a *Remote helper.
func remoteHelperArgs(op crudOperationPlan, modelVar string) []ast.Expr {
	args := []ast.Expr{astgen.Ident("ctx"), astgen.UnaryPtr(astgen.Ident(modelVar))}
	if op.ifMatchParam {
		args = append(args, astgen.Ident(ifMatchParamVar))
	}
	return append(args, astgen.Ident("resp"))
}

// privateETagReadStmts loads the stored ETag from req.Private into ifMatch.
func privateETagReadStmts() []ast.Stmt {
	return []ast.Stmt{
		astgen.Assign(
			[]ast.Expr{astgen.Ident(ifMatchParamVar), astgen.Ident("etagDiags")},
			[]ast.Expr{astgen.Call(astgen.Ident("privateETag"), astgen.Ident("ctx"), astgen.Selector(astgen.Ident("req"), "Private"))},
		),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "Append"),
			astgen.Ellipsis(astgen.Ident("etagDiags")),
		)),
	}
}

// privateETagWriteStmts stores the captured ETag in resp.Private (removing
// the key when the response carried none, so a stale ETag is never sent).
func privateETagWriteStmts() []ast.Stmt {
	return []ast.Stmt{astgen.If(
		astgen.NotEqual(astgen.Selector(astgen.Ident("resp"), "Private"), astgen.Nil()),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "Append"),
			astgen.Ellipsis(astgen.Call(
				astgen.Ident("setPrivateETag"),
				astgen.Ident("ctx"),
				astgen.Selector(astgen.Ident("resp"), "Private"),
				astgen.Ident(etagResultName),
			)),
		)),
	)}
}

// remoteCallStmt emits the call of a *Remote helper from its framework method.
// existing names the helper's own results captured by the caller (readRemote's
// removed); the etag result, when the operation captures one, is appended.
// With no results to capture the call is a plain expression statement.
func remoteCallStmt(op crudOperationPlan, method, modelVar string, existing []string) ast.Stmt {
	call := astgen.Call(astgen.Selector(astgen.Ident("r"), method), remoteHelperArgs(op, modelVar)...)
	lhs := make([]ast.Expr, 0, len(existing)+1)
	for _, name := range existing {
		lhs = append(lhs, astgen.Ident(name))
	}
	if op.captureETag {
		lhs = append(lhs, astgen.Ident(etagResultName))
	}
	if len(lhs) == 0 {
		return astgen.ExprStmt(call)
	}
	return astgen.Assign(lhs, []ast.Expr{call})
}
