package generator

// This file emits the generated "coverage" test files: a shared helpers file
// (testing_helpers_test.go) and one resource_<name>_remote_test.go per wired
// resource. The coverage tests exercise the extracted *Remote helper methods
// (createRemote/readRemote/updateRemote/deleteRemote) directly against an
// httptest mock, covering every reachable happy and unhappy HTTP branch without
// requiring TF_ACC acceptance mode or a tfsdk.Plan (whose Schema is built from
// an internal fwschema type generated code cannot instantiate).
//
// Generation stays deterministic: test function names derive only from the
// resource struct name and a fixed case taxonomy, and the emitted bodies carry
// no timestamps or random data.

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/naming"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// retryDisabledOpt emits the client.WithRetry option that disables retries
// (maxRetries=0). The coverage tests exercise each construct's own error
// handling against a single mock response, not the client's retry/backoff
// loop — which is covered separately by client_test.go. Without this, every
// error-status and transport-error case would sleep through the default
// 3-retry exponential backoff (~10s each), making the suite unusably slow.
// A non-nil policy is required because DoWithRetry evaluates it before the
// attempt==maxRetries short-circuit; the backoff is never invoked at zero
// retries but is passed for clarity.
func retryDisabledOpt() ast.Expr {
	return astgen.Call(astgen.QualExpr("client", "WithRetry"),
		astgen.IntLit(0),
		astgen.QualExpr("client", "DefaultRetryPolicy"),
		astgen.QualExpr("client", "DefaultBackoff"),
	)
}

// SharedTestHelpersFile returns internal/provider/testing_helpers_test.go, a
// package-level test helper file emitted once per provider. It builds httptest
// mock clients and diagnostic assertions shared by every resource coverage test
// file. It is only emitted when at least one coverage test file is produced, so
// the helpers are never left unused (staticcheck U1000).
func SharedTestHelpersFile(clientImport string) File {
	path := filepath.Join("internal", "provider", "testing_helpers_test.go")
	return GoCodeAST(path, generateSharedTestHelpersFile(clientImport))
}

// generateSharedTestHelpersFile builds the *ast.File for the shared helpers.
func generateSharedTestHelpersFile(clientImport string) *ast.File {
	f := astgen.NewFile("provider")
	f.AddImport("errors", "")
	f.AddImport("net/http", "")
	f.AddImport("net/http/httptest", "")
	f.AddImport("strings", "")
	f.AddImport("testing", "")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/diag", "")
	f.AddImport(clientImport, "client")

	// newMockClient returns a *client.Client backed by an httptest server. The
	// server is closed via t.Cleanup so each test gets an isolated endpoint.
	f.AddComment("newMockClient returns a *client.Client backed by an httptest server using the supplied handler.")
	f.AddDecl(astgen.FuncDeclFull("newMockClient",
		astgen.Params(
			astgen.Field("t", astgen.StarExpr(astgen.QualExpr("testing", "T")), ""),
			astgen.Field("handler", astgen.QualExpr("http", "HandlerFunc"), ""),
		),
		astgen.Results(astgen.Field("", astgen.StarExpr(astgen.QualExpr("client", "Client")), "")),
		astgen.Block(
			astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("t"), "Helper"))),
			astgen.AssignSingle(astgen.Ident("ts"), astgen.Call(astgen.QualExpr("httptest", "NewServer"), astgen.Ident("handler"))),
			astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("t"), "Cleanup"), astgen.Selector(astgen.Ident("ts"), "Close"))),
			astgen.Return(astgen.Call(astgen.QualExpr("client", "New"),
				astgen.Call(astgen.QualExpr("client", "WithBaseURL"), astgen.Selector(astgen.Ident("ts"), "URL")),
				astgen.Call(astgen.QualExpr("client", "WithHTTPClient"), astgen.Call(astgen.Selector(astgen.Ident("ts"), "Client"))),
				retryDisabledOpt(),
			)),
		),
	))

	// newMockClientStatus returns a client whose server responds with a fixed
	// status code and body for every request, ignoring method/path/body.
	f.AddComment("newMockClientStatus returns a *client.Client whose server responds with the given status code and body for every request.")
	f.AddDecl(astgen.FuncDeclFull("newMockClientStatus",
		astgen.Params(
			astgen.Field("t", astgen.StarExpr(astgen.QualExpr("testing", "T")), ""),
			astgen.Field("status", astgen.Ident("int"), ""),
			astgen.Field("body", astgen.Ident("string"), ""),
		),
		astgen.Results(astgen.Field("", astgen.StarExpr(astgen.QualExpr("client", "Client")), "")),
		astgen.Block(
			astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("t"), "Helper"))),
			astgen.Return(astgen.Call(astgen.Ident("newMockClient"), astgen.Ident("t"),
				mockStatusHandlerLit("status", "body", ""),
			)),
		),
	))

	// newMockClientWithLocation is like newMockClientStatus but also sets a
	// Location response header, exercising the create identifier fallback for
	// string-identifier resources whose create response carries no body id.
	f.AddComment("newMockClientWithLocation returns a *client.Client whose server responds with the given status, Location header, and body.")
	f.AddDecl(astgen.FuncDeclFull("newMockClientWithLocation",
		astgen.Params(
			astgen.Field("t", astgen.StarExpr(astgen.QualExpr("testing", "T")), ""),
			astgen.Field("status", astgen.Ident("int"), ""),
			astgen.Field("location", astgen.Ident("string"), ""),
			astgen.Field("body", astgen.Ident("string"), ""),
		),
		astgen.Results(astgen.Field("", astgen.StarExpr(astgen.QualExpr("client", "Client")), "")),
		astgen.Block(
			astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("t"), "Helper"))),
			astgen.Return(astgen.Call(astgen.Ident("newMockClient"), astgen.Ident("t"),
				mockStatusHandlerLit("status", "body", "location"),
			)),
		),
	))

	// newTransportErrorClient returns a client whose server is already closed,
	// so every request fails with a transport error (connection refused),
	// exercising the "Could not send request" branch.
	f.AddComment("newTransportErrorClient returns a *client.Client whose backing server is closed, so every request fails with a transport error.")
	f.AddDecl(astgen.FuncDeclFull("newTransportErrorClient",
		astgen.Params(astgen.Field("t", astgen.StarExpr(astgen.QualExpr("testing", "T")), "")),
		astgen.Results(astgen.Field("", astgen.StarExpr(astgen.QualExpr("client", "Client")), "")),
		astgen.Block(
			astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("t"), "Helper"))),
			astgen.AssignSingle(astgen.Ident("ts"), astgen.Call(astgen.QualExpr("httptest", "NewServer"),
				astgen.Call(astgen.QualExpr("http", "HandlerFunc"),
					astgen.FuncLit(
						astgen.FuncType(
							astgen.Params(
								astgen.Field("w", astgen.QualExpr("http", "ResponseWriter"), ""),
								astgen.Field("r", astgen.StarExpr(astgen.QualExpr("http", "Request")), ""),
							),
							astgen.Results(),
						),
						astgen.Block(),
					),
				),
			)),
			astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("ts"), "Close"))),
			astgen.Return(astgen.Call(astgen.QualExpr("client", "New"),
				astgen.Call(astgen.QualExpr("client", "WithBaseURL"), astgen.Selector(astgen.Ident("ts"), "URL")),
				astgen.Call(astgen.QualExpr("client", "WithHTTPClient"), astgen.Call(astgen.Selector(astgen.Ident("ts"), "Client"))),
				retryDisabledOpt(),
			)),
		),
	))

	// newMalformedBaseURLClient returns a client with an unparseable base URL,
	// so NewRequest fails (url.JoinPath error), exercising the "Could not build
	// request" branch without a network round-trip.
	f.AddComment("newMalformedBaseURLClient returns a *client.Client whose base URL is unparseable, so NewRequest always fails.")
	f.AddDecl(astgen.FuncDeclFull("newMalformedBaseURLClient",
		astgen.Params(astgen.Field("t", astgen.StarExpr(astgen.QualExpr("testing", "T")), "")),
		astgen.Results(astgen.Field("", astgen.StarExpr(astgen.QualExpr("client", "Client")), "")),
		astgen.Block(
			astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("t"), "Helper"))),
			astgen.Return(astgen.Call(astgen.QualExpr("client", "New"),
				astgen.Call(astgen.QualExpr("client", "WithBaseURL"), astgen.Lit(":")),
				retryDisabledOpt(),
			)),
		),
	))

	// newMockClientReadErrorBody returns a client whose every response carries a
	// body that fails on Read, so a non-success status surfaces "Could not read
	// error response" (client.NewAPIError's io.ReadAll returns an error). A
	// custom RoundTripper supplies the response directly, bypassing the network,
	// because an httptest server always serves a fully-readable body.
	f.AddComment("newMockClientReadErrorBody returns a *client.Client whose responses carry a body that errors on Read, exercising the Could not read error response branch.")
	f.AddDecl(astgen.FuncDeclFull("newMockClientReadErrorBody",
		astgen.Params(
			astgen.Field("t", astgen.StarExpr(astgen.QualExpr("testing", "T")), ""),
			astgen.Field("status", astgen.Ident("int"), ""),
		),
		astgen.Results(astgen.Field("", astgen.StarExpr(astgen.QualExpr("client", "Client")), "")),
		astgen.Block(
			astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("t"), "Helper"))),
			astgen.Return(astgen.Call(astgen.QualExpr("client", "New"),
				astgen.Call(astgen.QualExpr("client", "WithBaseURL"), astgen.Lit("http://read-error.test")),
				astgen.Call(astgen.QualExpr("client", "WithHTTPClient"),
					astgen.UnaryPtr(astgen.CompositeLit(astgen.QualExpr("http", "Client"),
						astgen.KeyValue("Transport", astgen.CompositeLit(astgen.Ident("readErrorTransport"),
							astgen.KeyValue("status", astgen.Ident("status")),
						)),
					)),
				),
				retryDisabledOpt(),
			)),
		),
	))

	// requireNoErrors fails the test if diags contains any error.
	f.AddComment("requireNoErrors fails the test if diags contains any error-level diagnostic.")
	f.AddDecl(astgen.FuncDeclFull("requireNoErrors",
		astgen.Params(
			astgen.Field("t", astgen.StarExpr(astgen.QualExpr("testing", "T")), ""),
			astgen.Field("diags", astgen.QualExpr("diag", "Diagnostics"), ""),
		),
		astgen.Results(),
		astgen.Block(
			astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("t"), "Helper"))),
			astgen.If(
				astgen.Call(astgen.Selector(astgen.Ident("diags"), "HasError")),
				astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("t"), "Fatalf"),
					astgen.Lit("expected no diagnostics errors, got: %s"),
					astgen.Ident("diags"),
				)),
			),
		),
	))

	// hasErrorContaining passes when some diagnostic's Summary or Detail
	// contains substr; otherwise it fails. Asserting on substrings keeps the
	// tests robust to fmt.Sprintf formatting and apiErr.Error() variance.
	f.AddComment("hasErrorContaining fails the test unless some diagnostic's Summary or Detail contains substr.")
	f.AddDecl(astgen.FuncDeclFull("hasErrorContaining",
		astgen.Params(
			astgen.Field("t", astgen.StarExpr(astgen.QualExpr("testing", "T")), ""),
			astgen.Field("diags", astgen.QualExpr("diag", "Diagnostics"), ""),
			astgen.Field("substr", astgen.Ident("string"), ""),
		),
		astgen.Results(),
		astgen.Block(
			astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("t"), "Helper"))),
			astgen.RangeStmt(astgen.Ident("_"), astgen.Ident("d"), token.DEFINE, astgen.Ident("diags"),
				astgen.Block(
					astgen.If(
						astgen.Binary(
							astgen.Call(astgen.QualExpr("strings", "Contains"), astgen.Call(astgen.Selector(astgen.Ident("d"), "Summary")), astgen.Ident("substr")),
							token.LOR,
							astgen.Call(astgen.QualExpr("strings", "Contains"), astgen.Call(astgen.Selector(astgen.Ident("d"), "Detail")), astgen.Ident("substr")),
						),
						astgen.Return(),
					),
				),
			),
			astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("t"), "Fatalf"),
				astgen.Lit("expected a diagnostic containing %q, got: %s"),
				astgen.Ident("substr"),
				astgen.Ident("diags"),
			)),
		),
	))

	// readErrorTransport is an http.RoundTripper that returns a response whose
	// body fails on Read, so client.NewAPIError surfaces a read error instead of
	// parsing a body. failingReadBody is the erroring io.ReadCloser it serves.
	f.AddComment("readErrorTransport is an http.RoundTripper that returns a response with a failing body for every request.")
	f.AddDecl(astgen.TypeDecl("readErrorTransport", astgen.StructType(
		astgen.Field("status", astgen.Ident("int"), ""),
	)))
	f.AddDecl(astgen.MethodDecl("RoundTrip", "r", astgen.Ident("readErrorTransport"),
		astgen.Params(astgen.Field("_", astgen.StarExpr(astgen.QualExpr("http", "Request")), "")),
		astgen.Results(
			astgen.Field("", astgen.StarExpr(astgen.QualExpr("http", "Response")), ""),
			astgen.Field("", astgen.Ident("error"), ""),
		),
		astgen.Block(
			astgen.Return(
				astgen.UnaryPtr(astgen.CompositeLit(astgen.QualExpr("http", "Response"),
					astgen.KeyValue("StatusCode", astgen.Selector(astgen.Ident("r"), "status")),
					astgen.KeyValue("Header", astgen.CompositeLit(astgen.QualExpr("http", "Header"))),
					astgen.KeyValue("Body", astgen.CompositeLit(astgen.Ident("failingReadBody"))),
				)),
				astgen.Nil(),
			),
		),
	))
	f.AddComment("failingReadBody is an io.ReadCloser whose Read always errors, so io.ReadAll fails.")
	f.AddDecl(astgen.TypeDecl("failingReadBody", astgen.StructType()))
	f.AddDecl(astgen.MethodDecl("Read", "_", astgen.Ident("failingReadBody"),
		astgen.Params(astgen.Field("_", astgen.SliceType(astgen.Ident("byte")), "")),
		astgen.Results(
			astgen.Field("", astgen.Ident("int"), ""),
			astgen.Field("", astgen.Ident("error"), ""),
		),
		astgen.Block(
			astgen.Return(astgen.IntLit(0), astgen.Call(astgen.QualExpr("errors", "New"), astgen.Lit("read boom"))),
		),
	))
	f.AddDecl(astgen.MethodDecl("Close", "_", astgen.Ident("failingReadBody"),
		astgen.Params(),
		astgen.Results(astgen.Field("", astgen.Ident("error"), "")),
		astgen.Block(
			astgen.Return(astgen.Nil()),
		),
	))

	return f.AST()
}

// mockStatusHandlerLit returns a func(http.ResponseWriter, *http.Request) that
// writes the optional Location header, the status code, and the body. statusVar
// and bodyVar name the enclosing identifiers to reference; locationVar is empty
// when no Location header should be set.
func mockStatusHandlerLit(statusVar, bodyVar, locationVar string) *ast.FuncLit {
	body := make([]ast.Stmt, 0, 3)
	if locationVar != "" {
		body = append(body,
			astgen.If(
				astgen.NotEqual(astgen.Ident(locationVar), astgen.Lit("")),
				astgen.Block(astgen.ExprStmt(astgen.Call(
					astgen.Selector(astgen.Call(astgen.Selector(astgen.Ident("w"), "Header")), "Set"),
					astgen.Lit("Location"),
					astgen.Ident(locationVar),
				))),
			),
		)
	}
	body = append(body,
		astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("w"), "WriteHeader"), astgen.Ident(statusVar))),
		astgen.AssignStmt(
			[]ast.Expr{astgen.Ident("_"), astgen.Ident("_")},
			[]ast.Expr{astgen.Call(astgen.Selector(astgen.Ident("w"), "Write"), byteSliceCall(astgen.Ident(bodyVar)))},
			token.ASSIGN,
		),
	)
	return astgen.FuncLit(
		astgen.FuncType(
			astgen.Params(
				astgen.Field("w", astgen.QualExpr("http", "ResponseWriter"), ""),
				astgen.Field("r", astgen.StarExpr(astgen.QualExpr("http", "Request")), ""),
			),
			astgen.Results(),
		),
		astgen.Block(body...),
	)
}

// byteSliceCall returns the conversion expression []byte(arg).
func byteSliceCall(arg ast.Expr) *ast.CallExpr {
	return &ast.CallExpr{Fun: astgen.SliceType(astgen.Ident("byte")), Args: []ast.Expr{arg}}
}

// ResourceCoverageTestFile returns internal/provider/resource_<name>_remote_test.go
// for a single wired resource, or nil if the resource is not coverage-eligible
// (unwired, or a binary file-upload create whose body the mock cannot round-trip).
func ResourceCoverageTestFile(r ir.ResourceIR, clientImport string) File {
	name := naming.SnakeCase(r.Name)
	path := filepath.Join("internal", "provider", fmt.Sprintf("resource_%s_remote_test.go", name))
	file, err := generateResourceCoverageTestFile(r, clientImport)
	if err != nil {
		return ErrorFile(path, err)
	}
	return GoCodeAST(path, file)
}

// ResourceCoverageTestFiles returns the coverage test files for every
// coverage-eligible (wired, non-binary-upload) resource, in the order supplied.
func ResourceCoverageTestFiles(resources []ir.ResourceIR, clientImport string) []File {
	files := make([]File, 0, len(resources))
	for _, r := range resources {
		if !planResourceWiring(r).wired || resourceCreateHasBinaryUpload(r) {
			continue
		}
		files = append(files, ResourceCoverageTestFile(r, clientImport))
	}
	return files
}

// generateResourceCoverageTestFile builds the *ast.File for a single resource's
// coverage tests. It returns an error when the resource is not coverage-eligible
// (unwired, or a binary file-upload create); ResourceCoverageTestFiles filters
// those out so the public file emitter is never invoked on them in normal flows,
// and a direct call fails loudly rather than emitting a stub.
func generateResourceCoverageTestFile(r ir.ResourceIR, _ string) (*ast.File, error) {
	plan := planResourceWiring(r)
	if !plan.wired {
		return nil, fmt.Errorf("resource %q is not wired to a remote API endpoint", r.Name)
	}
	if resourceCreateHasBinaryUpload(r) {
		return nil, fmt.Errorf("resource %q create is a binary file upload and cannot be exercised by the mock coverage tests", r.Name)
	}

	f := astgen.NewFile("provider")
	f.AddImport("context", "")
	f.AddImport("testing", "")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/resource", "")

	structName := resourceStructName(r)
	modelName := resourceModelName(r)
	info := resourceIDFieldInfo(r)

	cases := buildResourceCoverageCases(r, plan, info)
	for _, c := range cases {
		f.AddCommentf("%s exercises %s.%s against an httptest mock: %s.", c.funcName(structName), structName, c.method, c.intent)
		f.AddDecl(coverageTestDecl(structName, modelName, "resource", c))
	}

	return f.AST(), nil
}

// coverageCase describes one generated TestXxx coverage function.
type coverageCase struct {
	suffix        string // e.g. "Create_Happy"
	method        string // receiver method, e.g. "createRemote"
	resp          string // resource response type, e.g. "CreateResponse"
	returns       bool   // true when the method returns a bool (readRemote)
	captureReturn bool   // true when the bool return is captured into "removed" for assertion
	intent        string // human description for the doc comment
	client        ast.Expr
	asserts       []ast.Stmt
}

func (c coverageCase) funcName(structName string) string {
	return "Test" + structName + "_" + c.suffix
}

// buildResourceCoverageCases assembles the happy + unhappy taxonomy for each
// wired CRUD operation of a resource. The taxonomy maps 1:1 to the reachable
// AddError branches in the generated *Remote helpers.
func buildResourceCoverageCases(r ir.ResourceIR, plan resourceWiringPlan, info idFieldInfo) []coverageCase {
	var cases []coverageCase

	// Create.
	createHappy := idBodyJSON(info)
	cases = append(cases,
		coverageCase{suffix: "Create_Happy", method: "createRemote", resp: "CreateResponse",
			intent:  "happy path returns the success status and an identifier in the body",
			client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(happyStatus(plan.create)), astgen.Lit(createHappy)),
			asserts: []ast.Stmt{requireNoErrorsStmt()},
		},
		coverageCase{suffix: "Create_NilClient", method: "createRemote", resp: "CreateResponse",
			intent:  "nil client surfaces the Client Not Configured diagnostic",
			client:  nil,
			asserts: []ast.Stmt{hasErrorContainingStmt("Client Not Configured")},
		},
		coverageCase{suffix: "Create_BuildError", method: "createRemote", resp: "CreateResponse",
			intent:  "malformed base URL surfaces Could not build request",
			client:  astgen.Call(astgen.Ident("newMalformedBaseURLClient"), astgen.Ident("t")),
			asserts: []ast.Stmt{hasErrorContainingStmt("Could not build request")},
		},
		coverageCase{suffix: "Create_SendError", method: "createRemote", resp: "CreateResponse",
			intent:  "transport error surfaces Could not send request",
			client:  astgen.Call(astgen.Ident("newTransportErrorClient"), astgen.Ident("t")),
			asserts: []ast.Stmt{hasErrorContainingStmt("Could not send request")},
		},
		coverageCase{suffix: "Create_APIError", method: "createRemote", resp: "CreateResponse",
			intent:  "non-success status surfaces the API error summary",
			client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(coverageErrorStatus(plan.create)), astgen.Lit(`{"message":"boom"}`)),
			asserts: []ast.Stmt{hasErrorContainingStmt(fmt.Sprintf("Error creating %s", resourceTypeName(r)))},
		},
		coverageCase{suffix: "Create_APIErrorReadBody", method: "createRemote", resp: "CreateResponse",
			intent:  "non-success status whose error body cannot be read surfaces Could not read error response",
			client:  astgen.Call(astgen.Ident("newMockClientReadErrorBody"), astgen.Ident("t"), astgen.IntLit(coverageErrorStatus(plan.create))),
			asserts: []ast.Stmt{hasErrorContainingStmt("Could not read error response")},
		},
		coverageCase{suffix: "Create_InvalidJSON", method: "createRemote", resp: "CreateResponse",
			intent:  "success status with a malformed body surfaces Could not decode response body",
			client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(happyStatus(plan.create)), astgen.Lit(`{{`)),
			asserts: []ast.Stmt{hasErrorContainingStmt("Could not decode response body")},
		},
	)
	if body, ok := mapErrBody(info); ok {
		cases = append(cases, coverageCase{suffix: "Create_MapError", method: "createRemote", resp: "CreateResponse",
			intent:  "success status with a wrong-typed identifier surfaces Could not map response to state",
			client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(happyStatus(plan.create)), astgen.Lit(body)),
			asserts: []ast.Stmt{hasErrorContainingStmt("Could not map response to state")},
		})
	}
	if info.found {
		cases = append(cases, coverageCase{suffix: "Create_MissingID", method: "createRemote", resp: "CreateResponse",
			intent:  "success status with no identifier surfaces the missing-identifier diagnostic",
			client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(happyStatus(plan.create)), astgen.Lit(`{}`)),
			asserts: []ast.Stmt{hasErrorContainingStmt("did not contain an identifier")},
		})
	}
	if info.found && info.primitive == ir.TypeString {
		loc := "http://example.test/folders/example-id"
		cases = append(cases, coverageCase{suffix: "Create_LocationFallback", method: "createRemote", resp: "CreateResponse",
			intent: "success status with no body id but a Location header sets the string identifier from the header",
			client: astgen.Call(astgen.Ident("newMockClientWithLocation"), astgen.Ident("t"), astgen.IntLit(happyStatus(plan.create)), astgen.Lit(loc), astgen.Lit(`{}`)),
			asserts: []ast.Stmt{
				requireNoErrorsStmt(),
				astgen.If(
					astgen.NotEqual(
						astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("m"), info.field), "ValueString")),
						astgen.Lit(loc),
					),
					astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("t"), "Fatalf"),
						astgen.Lit("identifier = %q, want %q"),
						astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("m"), info.field), "ValueString")),
						astgen.Lit(loc),
					)),
				),
			},
		})
	}

	// Read.
	readCases := []coverageCase{
		{suffix: "Read_Happy", method: "readRemote", resp: "ReadResponse", returns: true, captureReturn: true,
			intent: "happy path returns the success status and reports removed=false with no errors",
			client: astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(happyStatus(plan.read)), astgen.Lit(`{}`)),
			asserts: []ast.Stmt{
				astgen.If(astgen.Ident("removed"), astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("t"), "Fatalf"), astgen.Lit("expected removed=false on happy path")))),
				requireNoErrorsStmt(),
			},
		},
		{suffix: "Read_NilClient", method: "readRemote", resp: "ReadResponse", returns: true,
			intent:  "nil client surfaces the Client Not Configured diagnostic",
			client:  nil,
			asserts: []ast.Stmt{hasErrorContainingStmt("Client Not Configured")},
		},
		{suffix: "Read_BuildError", method: "readRemote", resp: "ReadResponse", returns: true,
			intent:  "malformed base URL surfaces Could not build request",
			client:  astgen.Call(astgen.Ident("newMalformedBaseURLClient"), astgen.Ident("t")),
			asserts: []ast.Stmt{hasErrorContainingStmt("Could not build request")},
		},
		{suffix: "Read_SendError", method: "readRemote", resp: "ReadResponse", returns: true,
			intent:  "transport error surfaces Could not send request",
			client:  astgen.Call(astgen.Ident("newTransportErrorClient"), astgen.Ident("t")),
			asserts: []ast.Stmt{hasErrorContainingStmt("Could not send request")},
		},
		{suffix: "Read_NotFound", method: "readRemote", resp: "ReadResponse", returns: true, captureReturn: true,
			intent: "404 reports removed=true with no error so the framework drops the resource from state",
			client: astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(404), astgen.Lit(``)),
			asserts: []ast.Stmt{
				astgen.If(astgen.Unary(token.NOT, astgen.Ident("removed")), astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("t"), "Fatalf"), astgen.Lit("expected removed=true on 404")))),
				requireNoErrorsStmt(),
			},
		},
		{suffix: "Read_APIError", method: "readRemote", resp: "ReadResponse", returns: true,
			intent:  "non-success status surfaces the API error summary",
			client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(coverageErrorStatus(plan.read)), astgen.Lit(`{"message":"boom"}`)),
			asserts: []ast.Stmt{hasErrorContainingStmt(fmt.Sprintf("Error reading %s", resourceTypeName(r)))},
		},
		{suffix: "Read_APIErrorReadBody", method: "readRemote", resp: "ReadResponse", returns: true,
			intent:  "non-success status whose error body cannot be read surfaces Could not read error response",
			client:  astgen.Call(astgen.Ident("newMockClientReadErrorBody"), astgen.Ident("t"), astgen.IntLit(coverageErrorStatus(plan.read))),
			asserts: []ast.Stmt{hasErrorContainingStmt("Could not read error response")},
		},
		{suffix: "Read_InvalidJSON", method: "readRemote", resp: "ReadResponse", returns: true,
			intent:  "success status with a malformed body surfaces Could not decode response body",
			client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(happyStatus(plan.read)), astgen.Lit(`{{`)),
			asserts: []ast.Stmt{hasErrorContainingStmt("Could not decode response body")},
		},
	}
	if body, ok := mapErrBody(info); ok {
		readCases = append(readCases, coverageCase{suffix: "Read_MapError", method: "readRemote", resp: "ReadResponse", returns: true,
			intent:  "success status with a wrong-typed identifier surfaces Could not map response to state",
			client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(happyStatus(plan.read)), astgen.Lit(body)),
			asserts: []ast.Stmt{hasErrorContainingStmt("Could not map response to state")},
		})
	}
	cases = append(cases, readCases...)

	// Update (only when wired).
	if plan.update {
		updateCases := []coverageCase{
			{suffix: "Update_Happy", method: "updateRemote", resp: "UpdateResponse",
				intent:  "happy path returns the success status with no errors",
				client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(happyStatus(plan.updateOp)), astgen.Lit(`{}`)),
				asserts: []ast.Stmt{requireNoErrorsStmt()},
			},
			{suffix: "Update_NilClient", method: "updateRemote", resp: "UpdateResponse",
				intent:  "nil client surfaces the Client Not Configured diagnostic",
				client:  nil,
				asserts: []ast.Stmt{hasErrorContainingStmt("Client Not Configured")},
			},
			{suffix: "Update_BuildError", method: "updateRemote", resp: "UpdateResponse",
				intent:  "malformed base URL surfaces Could not build request",
				client:  astgen.Call(astgen.Ident("newMalformedBaseURLClient"), astgen.Ident("t")),
				asserts: []ast.Stmt{hasErrorContainingStmt("Could not build request")},
			},
			{suffix: "Update_SendError", method: "updateRemote", resp: "UpdateResponse",
				intent:  "transport error surfaces Could not send request",
				client:  astgen.Call(astgen.Ident("newTransportErrorClient"), astgen.Ident("t")),
				asserts: []ast.Stmt{hasErrorContainingStmt("Could not send request")},
			},
			{suffix: "Update_APIError", method: "updateRemote", resp: "UpdateResponse",
				intent:  "non-success status surfaces the API error summary",
				client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(coverageErrorStatus(plan.updateOp)), astgen.Lit(`{"message":"boom"}`)),
				asserts: []ast.Stmt{hasErrorContainingStmt(fmt.Sprintf("Error updating %s", resourceTypeName(r)))},
			},
			{suffix: "Update_APIErrorReadBody", method: "updateRemote", resp: "UpdateResponse",
				intent:  "non-success status whose error body cannot be read surfaces Could not read error response",
				client:  astgen.Call(astgen.Ident("newMockClientReadErrorBody"), astgen.Ident("t"), astgen.IntLit(coverageErrorStatus(plan.updateOp))),
				asserts: []ast.Stmt{hasErrorContainingStmt("Could not read error response")},
			},
			{suffix: "Update_InvalidJSON", method: "updateRemote", resp: "UpdateResponse",
				intent:  "success status with a malformed body surfaces Could not decode response body",
				client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(happyStatus(plan.updateOp)), astgen.Lit(`{{`)),
				asserts: []ast.Stmt{hasErrorContainingStmt("Could not decode response body")},
			},
		}
		if body, ok := mapErrBody(info); ok {
			updateCases = append(updateCases, coverageCase{suffix: "Update_MapError", method: "updateRemote", resp: "UpdateResponse",
				intent:  "success status with a wrong-typed identifier surfaces Could not map response to state",
				client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(happyStatus(plan.updateOp)), astgen.Lit(body)),
				asserts: []ast.Stmt{hasErrorContainingStmt("Could not map response to state")},
			})
		}
		cases = append(cases, updateCases...)
	}

	// Delete. Delete does not decode a response body, so there are no
	// InvalidJSON/MapError cases; a 404 is a silent success (already deleted).
	cases = append(cases,
		coverageCase{suffix: "Delete_Happy", method: "deleteRemote", resp: "DeleteResponse",
			intent:  "happy path returns the success status with no errors",
			client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(happyStatus(plan.delete)), astgen.Lit(``)),
			asserts: []ast.Stmt{requireNoErrorsStmt()},
		},
		coverageCase{suffix: "Delete_NilClient", method: "deleteRemote", resp: "DeleteResponse",
			intent:  "nil client surfaces the Client Not Configured diagnostic",
			client:  nil,
			asserts: []ast.Stmt{hasErrorContainingStmt("Client Not Configured")},
		},
		coverageCase{suffix: "Delete_BuildError", method: "deleteRemote", resp: "DeleteResponse",
			intent:  "malformed base URL surfaces Could not build request",
			client:  astgen.Call(astgen.Ident("newMalformedBaseURLClient"), astgen.Ident("t")),
			asserts: []ast.Stmt{hasErrorContainingStmt("Could not build request")},
		},
		coverageCase{suffix: "Delete_SendError", method: "deleteRemote", resp: "DeleteResponse",
			intent:  "transport error surfaces Could not send request",
			client:  astgen.Call(astgen.Ident("newTransportErrorClient"), astgen.Ident("t")),
			asserts: []ast.Stmt{hasErrorContainingStmt("Could not send request")},
		},
		coverageCase{suffix: "Delete_NotFoundSuccess", method: "deleteRemote", resp: "DeleteResponse",
			intent:  "404 is treated as already deleted and surfaces no error",
			client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(404), astgen.Lit(``)),
			asserts: []ast.Stmt{requireNoErrorsStmt()},
		},
		coverageCase{suffix: "Delete_APIError", method: "deleteRemote", resp: "DeleteResponse",
			intent:  "non-success status surfaces the API error summary",
			client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(coverageErrorStatus(plan.delete)), astgen.Lit(`{"message":"boom"}`)),
			asserts: []ast.Stmt{hasErrorContainingStmt(fmt.Sprintf("Error deleting %s", resourceTypeName(r)))},
		},
		coverageCase{suffix: "Delete_APIErrorReadBody", method: "deleteRemote", resp: "DeleteResponse",
			intent:  "non-success status whose error body cannot be read surfaces Could not read error response",
			client:  astgen.Call(astgen.Ident("newMockClientReadErrorBody"), astgen.Ident("t"), astgen.IntLit(coverageErrorStatus(plan.delete))),
			asserts: []ast.Stmt{hasErrorContainingStmt("Could not read error response")},
		},
	)

	return cases
}

// coverageTestDecl emits a single TestXxx function for one coverage case. It
// constructs the receiver (with the case's client or nil), a zero-value model,
// a bare framework response, invokes the *Remote helper, and runs the case's
// assertions. respPkg is the import qualifier of the framework response type
// ("resource" for resources, "datasource" for data sources, etc.).
func coverageTestDecl(structName, modelName, respPkg string, c coverageCase) *ast.FuncDecl {
	recvLit := astgen.CompositeLit(astgen.Ident(structName))
	if c.client != nil {
		recvLit = astgen.CompositeLit(astgen.Ident(structName), astgen.KeyValue("client", c.client))
	}

	body := make([]ast.Stmt, 0, 4+len(c.asserts))
	body = append(body,
		astgen.AssignSingle(astgen.Ident("r"), astgen.UnaryPtr(recvLit)),
		astgen.AssignSingle(astgen.Ident("m"), astgen.CompositeLit(astgen.Ident(modelName))),
		astgen.AssignSingle(astgen.Ident("resp"), astgen.UnaryPtr(astgen.CompositeLit(astgen.QualExpr(respPkg, c.resp)))),
	)

	call := astgen.Call(
		astgen.Selector(astgen.Ident("r"), c.method),
		astgen.Call(astgen.QualExpr("context", "Background")),
		astgen.UnaryPtr(astgen.Ident("m")),
		astgen.Ident("resp"),
	)
	if c.returns && c.captureReturn {
		body = append(body, astgen.AssignSingle(astgen.Ident("removed"), call))
	} else {
		body = append(body, astgen.ExprStmt(call))
	}
	body = append(body, c.asserts...)

	return astgen.FuncDeclFull(c.funcName(structName),
		astgen.Params(astgen.Field("t", astgen.StarExpr(astgen.QualExpr("testing", "T")), "")),
		astgen.Results(),
		astgen.Block(body...),
	)
}

// requireNoErrorsStmt returns requireNoErrors(t, resp.Diagnostics). The
// response variable in every coverage test is named "resp".
func requireNoErrorsStmt() ast.Stmt {
	return astgen.ExprStmt(astgen.Call(astgen.Ident("requireNoErrors"), astgen.Ident("t"), astgen.Selector(astgen.Ident("resp"), "Diagnostics")))
}

// hasErrorContainingStmt returns hasErrorContaining(t, resp.Diagnostics, substr).
func hasErrorContainingStmt(substr string) ast.Stmt {
	return astgen.ExprStmt(astgen.Call(astgen.Ident("hasErrorContaining"), astgen.Ident("t"), astgen.Selector(astgen.Ident("resp"), "Diagnostics"), astgen.Lit(substr)))
}

// happyStatus returns the success status code the happy-path mock should serve:
// the operation's first declared success code, falling back to 200 when the
// spec surfaced none (matching the generated 2xx-range success condition).
func happyStatus(op crudOperationPlan) int {
	if len(op.successCodes) > 0 {
		return op.successCodes[0]
	}
	return 200
}

// coverageErrorStatus returns a 5xx status code the unhappy-path mock should
// serve: one that is not a success code, not a 404 (handled specially by
// read/delete), and not a code with a declared error mapping (which would
// bypass the generic client.NewAPIError path the test exercises). Iterating the
// 5xx range guarantees such a code exists for any realistic spec.
func coverageErrorStatus(op crudOperationPlan) int {
	banned := map[int]bool{404: true}
	for _, c := range op.successCodes {
		banned[c] = true
	}
	for c := range op.errorMappings {
		banned[c] = true
	}
	for s := 500; s < 600; s++ {
		if !banned[s] {
			return s
		}
	}
	return 599
}

// idBodyJSON returns the JSON response body that sets the resource's identifier
// on the happy-path create, keyed by the identifier's tfsdk attribute name. When
// the resource has no recognizable identifier, an empty object is returned
// (create has no identifier fallback for such resources).
func idBodyJSON(info idFieldInfo) string {
	if !info.found {
		return `{}`
	}
	switch info.primitive {
	case ir.TypeInt:
		return fmt.Sprintf("{%q:1}", info.attr)
	case ir.TypeFloat:
		return fmt.Sprintf("{%q:1.0}", info.attr)
	case ir.TypeBool:
		return fmt.Sprintf("{%q:true}", info.attr)
	default: // TypeString, TypeDynamic
		return fmt.Sprintf("{%q:%q}", info.attr, "example-id")
	}
}

// mapErrBody returns a JSON response body whose identifier value has the wrong
// JSON type for the field, so applyJSONToModel surfaces "Could not map response
// to state". It returns ok=false when no such body can be built (no identifier,
// or a dynamic identifier which accepts any JSON value and so never errors).
func mapErrBody(info idFieldInfo) (string, bool) {
	if !info.found || info.primitive == ir.TypeDynamic {
		return "", false
	}
	if info.primitive == ir.TypeString {
		return fmt.Sprintf("{%q:12345}", info.attr), true
	}
	return fmt.Sprintf("{%q:%q}", info.attr, "not-valid"), true // int/float/bool
}

// DataSourceCoverageTestFile returns internal/provider/data_source_<name>_remote_test.go
// for a single wired data source, or nil if the data source is not coverage-eligible
// (unwired). Both single-object and list data sources are supported.
func DataSourceCoverageTestFile(ds ir.DataSourceIR, clientImport string) File {
	name := naming.SnakeCase(ds.Name)
	path := filepath.Join("internal", "provider", fmt.Sprintf("data_source_%s_remote_test.go", name))
	file, err := generateDataSourceCoverageTestFile(ds, clientImport)
	if err != nil {
		return ErrorFile(path, err)
	}
	return GoCodeAST(path, file)
}

// DataSourceCoverageTestFiles returns the coverage test files for every
// coverage-eligible (wired) data source, in the order supplied.
func DataSourceCoverageTestFiles(dataSources []ir.DataSourceIR, clientImport string) []File {
	files := make([]File, 0, len(dataSources))
	for _, ds := range dataSources {
		if !planDataSourceWiring(ds).wired {
			continue
		}
		files = append(files, DataSourceCoverageTestFile(ds, clientImport))
	}
	return files
}

// generateDataSourceCoverageTestFile builds the *ast.File for a single data
// source's coverage tests. It returns an error when the data source is not
// coverage-eligible (unwired); DataSourceCoverageTestFiles filters those out so
// the public emitter is never invoked on them in normal flows, and a direct
// call fails loudly rather than emitting a stub.
func generateDataSourceCoverageTestFile(ds ir.DataSourceIR, _ string) (*ast.File, error) {
	plan := planDataSourceWiring(ds)
	if !plan.wired {
		return nil, fmt.Errorf("data source %q is not wired to a remote API endpoint", ds.Name)
	}

	f := astgen.NewFile("provider")
	f.AddImport("context", "")
	f.AddImport("testing", "")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/datasource", "")

	structName := dataSourceStructName(ds)
	modelName := dataSourceAPIModelName(ds)

	cases := buildDataSourceCoverageCases(ds, plan)
	for _, c := range cases {
		f.AddCommentf("%s exercises %s.%s against an httptest mock: %s.", c.funcName(structName), structName, c.method, c.intent)
		f.AddDecl(coverageTestDecl(structName, modelName, "datasource", c))
	}

	return f.AST(), nil
}

// buildDataSourceCoverageCases assembles the happy + unhappy taxonomy for a
// wired data source. A single-object data source exercises readRemote (404 is
// an error, not a removal); a list data source exercises readListRemote (no
// status handling — non-2xx/empty bodies surface as decode errors). The
// taxonomy maps 1:1 to the reachable AddError branches in the generated helper.
func buildDataSourceCoverageCases(ds ir.DataSourceIR, plan dataSourceWiringPlan) []coverageCase {
	summary := fmt.Sprintf("Error reading %s", dataSourceTypeName(ds))
	if plan.list {
		// A list data source decodes each page into a bare []any when the
		// response is a top-level array, or into a map[string]any and then
		// extracts the <envelope> array when the response is enveloped. The
		// happy-path body must match: a bare array, or an object wrapping the
		// array under the envelope key.
		happyBody := `[]`
		if plan.read.responseEnvelope != "" {
			happyBody = fmt.Sprintf(`{%q:[]}`, plan.read.responseEnvelope)
		}
		return []coverageCase{
			{suffix: "Read_Happy", method: "readListRemote", resp: "ReadResponse",
				intent:  "happy path returns the success status with a JSON array body and no errors",
				client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(happyStatus(plan.read)), astgen.Lit(happyBody)),
				asserts: []ast.Stmt{requireNoErrorsStmt()},
			},
			{suffix: "Read_NilClient", method: "readListRemote", resp: "ReadResponse",
				intent:  "nil client surfaces the Client Not Configured diagnostic",
				client:  nil,
				asserts: []ast.Stmt{hasErrorContainingStmt("Client Not Configured")},
			},
			{suffix: "Read_BuildError", method: "readListRemote", resp: "ReadResponse",
				intent:  "malformed base URL surfaces Could not read list response",
				client:  astgen.Call(astgen.Ident("newMalformedBaseURLClient"), astgen.Ident("t")),
				asserts: []ast.Stmt{hasErrorContainingStmt("Could not read list response")},
			},
			{suffix: "Read_SendError", method: "readListRemote", resp: "ReadResponse",
				intent:  "transport error surfaces Could not read list response",
				client:  astgen.Call(astgen.Ident("newTransportErrorClient"), astgen.Ident("t")),
				asserts: []ast.Stmt{hasErrorContainingStmt("Could not read list response")},
			},
			{suffix: "Read_InvalidJSON", method: "readListRemote", resp: "ReadResponse",
				intent:  "success status with a non-array body surfaces Could not decode list page",
				client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(happyStatus(plan.read)), astgen.Lit(`{{`)),
				asserts: []ast.Stmt{hasErrorContainingStmt("Could not decode list page")},
			},
		}
	}

	return []coverageCase{
		{suffix: "Read_Happy", method: "readRemote", resp: "ReadResponse",
			intent:  "happy path returns the success status with no errors",
			client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(happyStatus(plan.read)), astgen.Lit(`{}`)),
			asserts: []ast.Stmt{requireNoErrorsStmt()},
		},
		{suffix: "Read_NilClient", method: "readRemote", resp: "ReadResponse",
			intent:  "nil client surfaces the Client Not Configured diagnostic",
			client:  nil,
			asserts: []ast.Stmt{hasErrorContainingStmt("Client Not Configured")},
		},
		{suffix: "Read_BuildError", method: "readRemote", resp: "ReadResponse",
			intent:  "malformed base URL surfaces Could not build request",
			client:  astgen.Call(astgen.Ident("newMalformedBaseURLClient"), astgen.Ident("t")),
			asserts: []ast.Stmt{hasErrorContainingStmt("Could not build request")},
		},
		{suffix: "Read_SendError", method: "readRemote", resp: "ReadResponse",
			intent:  "transport error surfaces Could not send request",
			client:  astgen.Call(astgen.Ident("newTransportErrorClient"), astgen.Ident("t")),
			asserts: []ast.Stmt{hasErrorContainingStmt("Could not send request")},
		},
		{suffix: "Read_NotFound", method: "readRemote", resp: "ReadResponse",
			intent:  "404 surfaces the requested-resource-not-found error",
			client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(404), astgen.Lit(``)),
			asserts: []ast.Stmt{hasErrorContainingStmt("The requested resource was not found.")},
		},
		{suffix: "Read_APIError", method: "readRemote", resp: "ReadResponse",
			intent:  "non-success status surfaces the API error summary",
			client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(coverageErrorStatus(plan.read)), astgen.Lit(`{"message":"boom"}`)),
			asserts: []ast.Stmt{hasErrorContainingStmt(summary)},
		},
		{suffix: "Read_APIErrorReadBody", method: "readRemote", resp: "ReadResponse",
			intent:  "non-success status whose error body cannot be read surfaces Could not read error response",
			client:  astgen.Call(astgen.Ident("newMockClientReadErrorBody"), astgen.Ident("t"), astgen.IntLit(coverageErrorStatus(plan.read))),
			asserts: []ast.Stmt{hasErrorContainingStmt("Could not read error response")},
		},
		{suffix: "Read_InvalidJSON", method: "readRemote", resp: "ReadResponse",
			intent:  "success status with a malformed body surfaces Could not decode response body",
			client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(happyStatus(plan.read)), astgen.Lit(`{{`)),
			asserts: []ast.Stmt{hasErrorContainingStmt("Could not decode response body")},
		},
	}
}

// ActionCoverageTestFile returns internal/provider/action_<name>_remote_test.go
// for a single wired action, or nil if the action is not coverage-eligible
// (unwired). Actions carry no result surface, so invokeRemote has no decode or
// state-setting branches; the taxonomy covers the request issue and the
// non-success error surfacing.
func ActionCoverageTestFile(a ir.ActionIR, clientImport string) File {
	name := naming.SnakeCase(a.Name)
	path := filepath.Join("internal", "provider", fmt.Sprintf("action_%s_remote_test.go", name))
	file, err := generateActionCoverageTestFile(a, clientImport)
	if err != nil {
		return ErrorFile(path, err)
	}
	return GoCodeAST(path, file)
}

// ActionCoverageTestFiles returns the coverage test files for every
// coverage-eligible (wired) action, in the order supplied.
func ActionCoverageTestFiles(actions []ir.ActionIR, clientImport string) []File {
	files := make([]File, 0, len(actions))
	for _, a := range actions {
		if !planActionWiring(a).wired {
			continue
		}
		files = append(files, ActionCoverageTestFile(a, clientImport))
	}
	return files
}

// generateActionCoverageTestFile builds the *ast.File for a single action's
// coverage tests. It returns an error when the action is not coverage-eligible
// (no resolvable invoke mapping); ActionCoverageTestFiles filters those out so
// the public emitter is never invoked on them in normal flows, and a direct
// call fails loudly rather than emitting a stub.
func generateActionCoverageTestFile(a ir.ActionIR, _ string) (*ast.File, error) {
	wiring := planActionWiring(a)
	if !wiring.wired || wiring.invoke == nil {
		return nil, fmt.Errorf("action %q is not wired to a remote API endpoint", a.Name)
	}

	f := astgen.NewFile("provider")
	f.AddImport("context", "")
	f.AddImport("testing", "")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/action", "")

	structName := actionStructName(a)
	modelName := actionModelName(a)

	cases := buildActionCoverageCases(a, *wiring.invoke)
	for _, c := range cases {
		f.AddCommentf("%s exercises %s.%s against an httptest mock: %s.", c.funcName(structName), structName, c.method, c.intent)
		f.AddDecl(coverageTestDecl(structName, modelName, "action", c))
	}

	return f.AST(), nil
}

// buildActionCoverageCases assembles the happy + unhappy taxonomy for a wired
// action's invokeRemote helper. An action has no response decode and no state
// to set, so the taxonomy covers only the request-issue and non-success error
// branches: nil client, malformed base URL, transport error, non-success
// status with a readable API error, and non-success status with a malformed
// error body. The taxonomy maps 1:1 to the reachable AddError branches in
// invokeRemote.
func buildActionCoverageCases(a ir.ActionIR, plan crudOperationPlan) []coverageCase {
	summary := fmt.Sprintf("Error invoking %s", actionTypeName(a))
	return []coverageCase{
		{suffix: "Invoke_Happy", method: "invokeRemote", resp: "InvokeResponse",
			intent:  "happy path returns the success status with no errors; the response body is not decoded",
			client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(happyStatus(plan)), astgen.Lit(`{}`)),
			asserts: []ast.Stmt{requireNoErrorsStmt()},
		},
		{suffix: "Invoke_NilClient", method: "invokeRemote", resp: "InvokeResponse",
			intent:  "nil client surfaces the Client Not Configured diagnostic",
			client:  nil,
			asserts: []ast.Stmt{hasErrorContainingStmt("Client Not Configured")},
		},
		{suffix: "Invoke_BuildError", method: "invokeRemote", resp: "InvokeResponse",
			intent:  "malformed base URL surfaces Could not build request",
			client:  astgen.Call(astgen.Ident("newMalformedBaseURLClient"), astgen.Ident("t")),
			asserts: []ast.Stmt{hasErrorContainingStmt("Could not build request")},
		},
		{suffix: "Invoke_SendError", method: "invokeRemote", resp: "InvokeResponse",
			intent:  "transport error surfaces Could not send request",
			client:  astgen.Call(astgen.Ident("newTransportErrorClient"), astgen.Ident("t")),
			asserts: []ast.Stmt{hasErrorContainingStmt("Could not send request")},
		},
		{suffix: "Invoke_APIError", method: "invokeRemote", resp: "InvokeResponse",
			intent:  "non-success status surfaces the API error summary",
			client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(coverageErrorStatus(plan)), astgen.Lit(`{"message":"boom"}`)),
			asserts: []ast.Stmt{hasErrorContainingStmt(summary)},
		},
		{suffix: "Invoke_APIErrorReadBody", method: "invokeRemote", resp: "InvokeResponse",
			intent:  "non-success status whose error body cannot be read surfaces Could not read error response",
			client:  astgen.Call(astgen.Ident("newMockClientReadErrorBody"), astgen.Ident("t"), astgen.IntLit(coverageErrorStatus(plan))),
			asserts: []ast.Stmt{hasErrorContainingStmt("Could not read error response")},
		},
	}
}

// EphemeralCoverageTestFile returns internal/provider/ephemeral_<name>_remote_test.go
// for a single wired ephemeral resource, or nil if the ephemeral is not
// coverage-eligible (unwired). Only the Open exchange is extracted; Renew/Close
// read ephemeral private state (req.Private), a framework surface the helper
// cannot reconstruct, so they stay framework-side and are not exercised here.
func EphemeralCoverageTestFile(er ir.EphemeralResourceIR, clientImport string) File {
	name := naming.SnakeCase(er.Name)
	path := filepath.Join("internal", "provider", fmt.Sprintf("ephemeral_%s_remote_test.go", name))
	file, err := generateEphemeralCoverageTestFile(er, clientImport)
	if err != nil {
		return ErrorFile(path, err)
	}
	return GoCodeAST(path, file)
}

// EphemeralCoverageTestFiles returns the coverage test files for every
// coverage-eligible (wired) ephemeral resource, in the order supplied.
func EphemeralCoverageTestFiles(ers []ir.EphemeralResourceIR, clientImport string) []File {
	files := make([]File, 0, len(ers))
	for _, er := range ers {
		if !planEphemeralWiring(er).wired {
			continue
		}
		files = append(files, EphemeralCoverageTestFile(er, clientImport))
	}
	return files
}

// generateEphemeralCoverageTestFile builds the *ast.File for a single ephemeral
// resource's coverage tests. It returns an error when the ephemeral is not
// coverage-eligible (no resolvable open mapping); EphemeralCoverageTestFiles
// filters those out so the public emitter is never invoked on them in normal
// flows, and a direct call fails loudly rather than emitting a stub.
func generateEphemeralCoverageTestFile(er ir.EphemeralResourceIR, _ string) (*ast.File, error) {
	wiring := planEphemeralWiring(er)
	if !wiring.wired {
		return nil, fmt.Errorf("ephemeral resource %q is not wired to a remote API endpoint", er.Name)
	}

	f := astgen.NewFile("provider")
	f.AddImport("context", "")
	f.AddImport("testing", "")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/ephemeral", "")

	structName := ephemeralResourceStructName(er)
	modelName := ephemeralResourceModelName(er)

	cases := buildEphemeralCoverageCases(er, wiring.open)
	for _, c := range cases {
		f.AddCommentf("%s exercises %s.%s against an httptest mock: %s.", c.funcName(structName), structName, c.method, c.intent)
		f.AddDecl(coverageTestDecl(structName, modelName, "ephemeral", c))
	}

	return f.AST(), nil
}

// buildEphemeralCoverageCases assembles the happy + unhappy taxonomy for a
// wired ephemeral resource's openRemote helper. The taxonomy mirrors the
// single-object data source read: a 404 is not special-cased (an ephemeral
// Open has no state to drop), so it surfaces as a generic API error. The
// taxonomy maps 1:1 to the reachable AddError branches in openRemote.
func buildEphemeralCoverageCases(er ir.EphemeralResourceIR, plan crudOperationPlan) []coverageCase {
	summary := fmt.Sprintf("Error opening ephemeral resource %s", ephemeralResourceTypeName(er))
	return []coverageCase{
		{suffix: "Open_Happy", method: "openRemote", resp: "OpenResponse",
			intent:  "happy path returns the success status and decodes the response body with no errors",
			client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(happyStatus(plan)), astgen.Lit(`{}`)),
			asserts: []ast.Stmt{requireNoErrorsStmt()},
		},
		{suffix: "Open_NilClient", method: "openRemote", resp: "OpenResponse",
			intent:  "nil client surfaces the Client Not Configured diagnostic",
			client:  nil,
			asserts: []ast.Stmt{hasErrorContainingStmt("Client Not Configured")},
		},
		{suffix: "Open_BuildError", method: "openRemote", resp: "OpenResponse",
			intent:  "malformed base URL surfaces Could not build request",
			client:  astgen.Call(astgen.Ident("newMalformedBaseURLClient"), astgen.Ident("t")),
			asserts: []ast.Stmt{hasErrorContainingStmt("Could not build request")},
		},
		{suffix: "Open_SendError", method: "openRemote", resp: "OpenResponse",
			intent:  "transport error surfaces Could not send request",
			client:  astgen.Call(astgen.Ident("newTransportErrorClient"), astgen.Ident("t")),
			asserts: []ast.Stmt{hasErrorContainingStmt("Could not send request")},
		},
		{suffix: "Open_APIError", method: "openRemote", resp: "OpenResponse",
			intent:  "non-success status surfaces the API error summary",
			client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(coverageErrorStatus(plan)), astgen.Lit(`{"message":"boom"}`)),
			asserts: []ast.Stmt{hasErrorContainingStmt(summary)},
		},
		{suffix: "Open_APIErrorReadBody", method: "openRemote", resp: "OpenResponse",
			intent:  "non-success status whose error body cannot be read surfaces Could not read error response",
			client:  astgen.Call(astgen.Ident("newMockClientReadErrorBody"), astgen.Ident("t"), astgen.IntLit(coverageErrorStatus(plan))),
			asserts: []ast.Stmt{hasErrorContainingStmt("Could not read error response")},
		},
		{suffix: "Open_InvalidJSON", method: "openRemote", resp: "OpenResponse",
			intent:  "success status with a malformed body surfaces Could not decode response body",
			client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(happyStatus(plan)), astgen.Lit(`{{`)),
			asserts: []ast.Stmt{hasErrorContainingStmt("Could not decode response body")},
		},
	}
}

// ListCoverageTestFile returns internal/provider/list_<name>_remote_test.go for
// a single wired list resource, or nil if the list resource is not
// coverage-eligible (unwired). Only the listRemote helper (HTTP fetch + page
// decode) is exercised; the per-item identity/tftypes/push logic stays in the
// framework List closure (it needs req.NewListResult/req.ResourceIdentitySchema,
// whose types are built from an internal fwschema type generated code cannot
// instantiate) and is covered by acceptance tests.
func ListCoverageTestFile(lr ir.ListResourceIR, clientImport string) File {
	name := naming.SnakeCase(lr.Name)
	path := filepath.Join("internal", "provider", fmt.Sprintf("list_%s_remote_test.go", name))
	file, err := generateListCoverageTestFile(lr, clientImport)
	if err != nil {
		return ErrorFile(path, err)
	}
	return GoCodeAST(path, file)
}

// ListCoverageTestFiles returns the coverage test files for every
// coverage-eligible (wired) list resource, in the order supplied.
func ListCoverageTestFiles(lrs []ir.ListResourceIR, clientImport string) []File {
	files := make([]File, 0, len(lrs))
	for _, lr := range lrs {
		if !planListResourceWiring(lr).wired {
			continue
		}
		files = append(files, ListCoverageTestFile(lr, clientImport))
	}
	return files
}

// generateListCoverageTestFile builds the *ast.File for a single list
// resource's coverage tests. It returns an error when the list resource is not
// coverage-eligible (no resolvable list mapping); ListCoverageTestFiles filters
// those out so the public emitter is never invoked on them in normal flows, and
// a direct call fails loudly rather than emitting a stub.
func generateListCoverageTestFile(lr ir.ListResourceIR, _ string) (*ast.File, error) {
	plan := planListResourceWiring(lr)
	if !plan.wired {
		return nil, fmt.Errorf("list resource %q is not wired to a remote API endpoint", lr.Name)
	}

	f := astgen.NewFile("provider")
	f.AddImport("context", "")
	f.AddImport("testing", "")

	structName := listResourceStructName(lr)
	modelName := structName + "Model"

	cases := buildListCoverageCases(plan)
	for _, c := range cases {
		f.AddCommentf("%s exercises %s.%s against an httptest mock: %s.", c.funcName(structName), structName, c.method, c.intent)
		f.AddDecl(listCoverageTestDecl(structName, modelName, plan.hasConfigModel, c))
	}

	return f.AST(), nil
}

// buildListCoverageCases assembles the happy + unhappy taxonomy for a wired
// list resource's listRemote helper. The taxonomy mirrors the list data source
// readListRemote: ListAllPages surfaces both request-build and transport
// failures as "Could not read list response" (the fetch closure's NewRequest/Do
// errors propagate through ListAllPages), and a malformed page surfaces "Could
// not decode list page". The taxonomy maps 1:1 to the reachable AddError
// branches in listRemote.
func buildListCoverageCases(plan listResourceWiringPlan) []coverageCase {
	// A list resource decodes each page into a bare []json.RawMessage when the
	// response is a top-level array, or into a map[string]json.RawMessage and
	// then extracts the <envelope> array when the response is enveloped. The
	// happy-path body must match: a bare array, or an object wrapping the array
	// under the envelope key.
	happyBody := `[]`
	if plan.read.responseEnvelope != "" {
		happyBody = fmt.Sprintf(`{%q:[]}`, plan.read.responseEnvelope)
	}
	return []coverageCase{
		{suffix: "List_Happy", method: "listRemote", resp: "",
			intent:  "happy path returns the success status with a JSON array body and no errors",
			client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(happyStatus(plan.read)), astgen.Lit(happyBody)),
			asserts: []ast.Stmt{requireNoErrorsDiagsStmt()},
		},
		{suffix: "List_NilClient", method: "listRemote", resp: "",
			intent:  "nil client surfaces the Client Not Configured diagnostic",
			client:  nil,
			asserts: []ast.Stmt{hasErrorContainingDiagsStmt("Client Not Configured")},
		},
		{suffix: "List_BuildError", method: "listRemote", resp: "",
			intent:  "malformed base URL surfaces Could not read list response",
			client:  astgen.Call(astgen.Ident("newMalformedBaseURLClient"), astgen.Ident("t")),
			asserts: []ast.Stmt{hasErrorContainingDiagsStmt("Could not read list response")},
		},
		{suffix: "List_SendError", method: "listRemote", resp: "",
			intent:  "transport error surfaces Could not read list response",
			client:  astgen.Call(astgen.Ident("newTransportErrorClient"), astgen.Ident("t")),
			asserts: []ast.Stmt{hasErrorContainingDiagsStmt("Could not read list response")},
		},
		{suffix: "List_InvalidJSON", method: "listRemote", resp: "",
			intent:  "success status with a non-array body surfaces Could not decode list page",
			client:  astgen.Call(astgen.Ident("newMockClientStatus"), astgen.Ident("t"), astgen.IntLit(happyStatus(plan.read)), astgen.Lit(`{{`)),
			asserts: []ast.Stmt{hasErrorContainingDiagsStmt("Could not decode list page")},
		},
	}
}

// listCoverageTestDecl emits a single TestXxx function for one list resource
// coverage case. Unlike coverageTestDecl, the listRemote helper returns
// ([]json.RawMessage, diag.Diagnostics) instead of mutating a framework
// response, so the test discards the items and asserts on the returned diags.
// When the list resource declares filter attributes the helper takes a config
// model pointer; an attribute-free list resource has no model type, so the
// helper is called with the context alone.
func listCoverageTestDecl(structName, modelName string, hasConfigModel bool, c coverageCase) *ast.FuncDecl {
	recvLit := astgen.CompositeLit(astgen.Ident(structName))
	if c.client != nil {
		recvLit = astgen.CompositeLit(astgen.Ident(structName), astgen.KeyValue("client", c.client))
	}

	body := make([]ast.Stmt, 0, 4+len(c.asserts))
	body = append(body,
		astgen.AssignSingle(astgen.Ident("r"), astgen.UnaryPtr(recvLit)),
	)
	callArgs := []ast.Expr{astgen.Call(astgen.QualExpr("context", "Background"))}
	if hasConfigModel {
		body = append(body, astgen.AssignSingle(astgen.Ident("m"), astgen.CompositeLit(astgen.Ident(modelName))))
		callArgs = append(callArgs, astgen.UnaryPtr(astgen.Ident("m")))
	}
	body = append(body,
		astgen.Assign(
			[]ast.Expr{astgen.Ident("_"), astgen.Ident("diags")},
			[]ast.Expr{astgen.Call(astgen.Selector(astgen.Ident("r"), c.method), callArgs...)},
		),
	)
	body = append(body, c.asserts...)

	return astgen.FuncDeclFull(c.funcName(structName),
		astgen.Params(astgen.Field("t", astgen.StarExpr(astgen.QualExpr("testing", "T")), "")),
		astgen.Results(),
		astgen.Block(body...),
	)
}

// requireNoErrorsDiagsStmt returns requireNoErrors(t, diags). The list coverage
// tests name the returned diagnostics "diags" (listRemote returns them rather
// than writing to a resp).
func requireNoErrorsDiagsStmt() ast.Stmt {
	return astgen.ExprStmt(astgen.Call(astgen.Ident("requireNoErrors"), astgen.Ident("t"), astgen.Ident("diags")))
}

// hasErrorContainingDiagsStmt returns hasErrorContaining(t, diags, substr). The
// list coverage tests name the returned diagnostics "diags".
func hasErrorContainingDiagsStmt(substr string) ast.Stmt {
	return astgen.ExprStmt(astgen.Call(astgen.Ident("hasErrorContaining"), astgen.Ident("t"), astgen.Ident("diags"), astgen.Lit(substr)))
}
