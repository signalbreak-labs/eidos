package generator

import (
	"fmt"
	"go/ast"
	"go/token"
	"sort"
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/naming"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/schema"
	"github.com/signalbreak-labs/eidos/pkg/ir"
	"github.com/signalbreak-labs/eidos/pkg/transformer"
)

// This file builds the wired Create/Read/Update/Delete bodies for managed
// resources whose CRUDMapping carries enough information to emit real calls
// against the generated API client (internal/client). Resources without a
// complete create/read/delete mapping keep their honest scaffold bodies; the
// decision is made once by planResourceWiring so partial mappings never
// produce half-wired resources.

// pathSubstitution describes one {placeholder} in a CRUD path template and the
// model field whose value replaces it.
type pathSubstitution struct {
	placeholder string
	field       string
	primitive   ir.PrimitiveType
}

// bodyKind identifies the request body encoding a wired operation emits.
type bodyKind int

const (
	// bodyJSON: application/json via modelToJSONMap + json.Marshal + bytes.NewReader.
	bodyJSON bodyKind = iota
	// bodyForm: application/x-www-form-urlencoded via url.Values + strings.NewReader.
	bodyForm
	// bodyMultipart: multipart/form-data via mime/multipart.NewWriter; the
	// Content-Type is set dynamically to writer.FormDataContentType(). Binary
	// formData params (format: binary) become file parts via CreateFormFile.
	bodyMultipart
	// bodyXML: application/xml via mapToXML (deterministic, sorted element order).
	bodyXML
)

// crudOperationPlan is the generation plan for one wired CRUD operation.
type crudOperationPlan struct {
	method       string
	template     string
	subs         []pathSubstitution
	successCodes []int
	// queryParams, headerParams, and cookieParams carry the operation's query,
	// header, and cookie parameters that are mapped to resource schema
	// attributes, so the wired body sends them on the request. Required
	// parameters with no matching attribute disable wiring for the whole
	// resource (see planOperation).
	queryParams  []paramSubstitution
	headerParams []paramSubstitution
	cookieParams []paramSubstitution
	// formDataParams carries the operation's formData parameters (OpenAPI 2.0
	// form-encoded request body) mapped to resource schema attributes, so a
	// wired create/update body sends them as application/x-www-form-urlencoded
	// instead of a JSON body. contentType is the request body media type:
	// "application/x-www-form-urlencoded" for a form body, "application/xml" for
	// an XML body, empty (defaulting to application/json) for a JSON body, and
	// empty for a multipart body (whose Content-Type is set dynamically via
	// writer.FormDataContentType()). formData is only resolved on body-bearing
	// methods (POST/PUT/PATCH); a bodiless GET/DELETE carrying formData cannot
	// send a form body and stays scaffolded (REMAINING_GAPS §2).
	formDataParams []paramSubstitution
	contentType    string
	// hasBody is true when the operation declares a request body that the wired
	// body encodes. It is distinct from bodyEncoding because bodyJSON is the zero
	// value of bodyKind, so a bodiless operation would otherwise be
	// indistinguishable from a JSON-body operation (the action wiring relies on
	// this distinction; resources always build a body on body-bearing methods).
	hasBody bool
	// bodyEncoding is the request body encoding derived from
	// OperationMappingIR.MediaType via transformer.RequestBodyKind. JSON is the
	// default (bodiless methods and empty media types). Unsupported media types
	// disable wiring (planOperation returns false) so the operation stays an
	// honest scaffold and the transformer's fail-loud Warning explains why (A2).
	bodyEncoding bodyKind
	// xmlRoot is the XML root element name for a bodyXML operation (the resource
	// name, e.g. "pet"); the request body is wrapped as <xmlRoot>...</xmlRoot> by
	// the generated mapToXML helper. Custom XML element/attribute names from the
	// schema's xml keyword are out of scope (A2).
	xmlRoot string
	// errorMappings carries non-2xx response status codes to their spec
	// descriptions, surfaced as per-code diagnostics in the error branch.
	errorMappings map[int]string
	// securitySchemes carries the named security scheme interceptors a wired
	// operation applies via client.WithSchemes(...) (per-operation AND
	// resolution, REMAINING_GAPS §1). securitySchemesSet is true when the
	// operation declared exactly one security requirement: the wired body then
	// applies only those scheme interceptors (an empty set marks the operation
	// unauthenticated and applies none). When securitySchemesSet is false the
	// operation declared no security (inherit the global default) or more than
	// one requirement (OR — ambiguous for a non-interactive provider, warned by
	// the transformer); the wired body applies no WithSchemes and NewRequest
	// applies every configured scheme interceptor. The names are sorted at
	// generation time so the emitted call is deterministic.
	securitySchemes    []string
	securitySchemesSet bool
	// responseEnvelope is the {data: ...} response envelope key the transformer
	// flattened out of the response schema. When non-empty, the wired body
	// unwraps the decoded response by this key before applying it to the model,
	// keeping the schema and the response consistent (E1).
	responseEnvelope string
}

// paramSubstitution describes one query, header, or cookie parameter and the
// model field whose value supplies it on the request.
type paramSubstitution struct {
	name      string
	in        string // "query", "header", or "cookie"
	field     string
	primitive ir.PrimitiveType
	// required is true for an OpenAPI parameter marked required. Non-required
	// parameters are gated on their model field being non-null when emitted, so
	// an unset optional parameter is omitted from the request rather than sent as
	// the zero-value empty string (which the API may reject or misinterpret).
	required bool
	// binary is true for a formData parameter whose OpenAPI format is "binary"
	// (a file upload): the multipart body builder writes it as a file part via
	// CreateFormFile instead of a text field via WriteField (A2).
	binary bool
}

// resourceWiringPlan describes how a resource's CRUD methods are wired to the
// generated API client. wired is false when the CRUD mapping lacks the
// information needed to emit real calls; the resource then keeps its honest
// scaffold bodies. update is false when the API exposes no update operation,
// in which case only the Update method keeps its scaffold body.
type resourceWiringPlan struct {
	wired  bool
	update bool

	create   crudOperationPlan
	read     crudOperationPlan
	updateOp crudOperationPlan
	delete   crudOperationPlan

	needsStrings bool
	needsStrconv bool
	// needsURL is true when at least one wired operation has a path placeholder,
	// gating the net/url import for url.PathEscape on path substitution.
	needsURL bool
	// needsJSONBody is true when at least one wired create/update body is a JSON
	// body (modelToJSONMap + json.Marshal + bytes.NewReader), gating the bytes
	// and encoding/json imports. needsFormBody is true when at least one wired
	// create/update body is form-encoded (url.Values + strings.NewReader),
	// gating the net/url import. A bodiless create/update (rare) sets neither;
	// a formData create sets needsFormBody and clears needsJSONBody for that op.
	needsJSONBody bool
	needsFormBody bool
	// needsMultipartBody is true when at least one wired create/update body is
	// multipart/form-data (mime/multipart.NewWriter + bytes.Buffer), gating the
	// mime/multipart import; a binary formData part also gates os (os.Open for
	// the file part) — needsMultipartFile tracks that. needsXMLBody is true when
	// at least one wired create/update body is application/xml (mapToXML),
	// gating the encoding/xml import (which is otherwise only in json_convert.go
	// when present).
	needsMultipartBody bool
	needsMultipartFile bool
	needsXMLBody       bool
}

// AnyResourceWired reports whether at least one resource has a complete enough
// CRUD mapping to wire its bodies to the generated API client. It gates
// emission of the JSON conversion helpers and the provider Configure client
// construction so providers with no wired resources carry no dead code.
func AnyResourceWired(resources []ir.ResourceIR) bool {
	for _, r := range resources {
		if planResourceWiring(r).wired {
			return true
		}
	}
	return false
}

// planResourceWiring resolves the generation plan for a resource's wired CRUD
// bodies. Create, read, and delete mappings must all be present and resolvable
// for the resource to be wired at all; an update mapping is optional. Any
// unresolvable path placeholder (no matching schema attribute and no usable ID
// attribute, or a non-primitive value) disables wiring for the whole resource
// so the generator never emits calls it cannot satisfy.
func planResourceWiring(r ir.ResourceIR) resourceWiringPlan {
	var plan resourceWiringPlan

	create, ok := planOperation(r, r.CRUDMapping.Create)
	if !ok {
		return plan
	}
	read, ok := planOperation(r, r.CRUDMapping.Read)
	if !ok {
		return plan
	}
	del, ok := planOperation(r, r.CRUDMapping.Delete)
	if !ok {
		return plan
	}

	plan.create = create
	plan.read = read
	plan.delete = del
	plan.wired = true

	if r.CRUDMapping.Update != nil {
		if upd, ok := planOperation(r, *r.CRUDMapping.Update); ok {
			plan.update = true
			plan.updateOp = upd
		}
	}

	for _, op := range []crudOperationPlan{plan.create, plan.read, plan.updateOp, plan.delete} {
		plan.noteOpImportNeeds(op)
	}

	// A wired create/update sends either a JSON body or a form-encoded body.
	// formData parameters make the body form-encoded (url.Values +
	// strings.NewReader, importing net/url); otherwise the body is JSON
	// (modelToJSONMap + json.Marshal + bytes.NewReader, importing bytes and
	// encoding/json). Read/Delete are bodiless and set neither. A resource with
	// a JSON create and a form update imports both sets.
	plan.needsJSONBody = opHasJSONBody(plan.create) || opHasJSONBody(plan.updateOp)
	plan.needsFormBody = opHasFormBody(plan.create) || opHasFormBody(plan.updateOp)
	plan.needsMultipartBody = opHasMultipartBody(plan.create) || opHasMultipartBody(plan.updateOp)
	plan.needsXMLBody = opHasXMLBody(plan.create) || opHasXMLBody(plan.updateOp)
	// A multipart body with at least one binary formData part reads the file
	// from the model field's path via os.Open, gating the os import.
	for _, op := range []crudOperationPlan{plan.create, plan.updateOp} {
		if opHasMultipartBody(op) {
			for _, p := range op.formDataParams {
				if p.binary {
					plan.needsMultipartFile = true
				}
			}
		}
	}

	return plan
}

// opHasJSONBody reports whether a wired operation sends a JSON request body:
// a create/update (POST/PUT/PATCH) without formData parameters builds its body
// from modelToJSONMap + json.Marshal. Read/Delete and any formData-bearing
// operation do not send JSON. The body-bearing-method guard distinguishes a
// real JSON operation from a bodiless plan (an absent update mapping leaves a
// zero-value crudOperationPlan whose bodyEncoding is bodyJSON by coincidence of
// the zero value); without it a form/multipart create with no update would
// import bytes unused.
func opHasJSONBody(op crudOperationPlan) bool {
	return methodHasBody(op.method) && op.bodyEncoding == bodyJSON
}

// opHasFormBody reports whether a wired operation sends a form-encoded request
// body: a body-bearing operation whose formData parameters resolved against
// the schema builds its body from url.Values. Read/Delete never send a body.
func opHasFormBody(op crudOperationPlan) bool {
	return methodHasBody(op.method) && op.bodyEncoding == bodyForm
}

// opHasMultipartBody reports whether a wired operation sends a multipart
// form-data body (mime/multipart.NewWriter). Read/Delete never send a body.
func opHasMultipartBody(op crudOperationPlan) bool {
	return methodHasBody(op.method) && op.bodyEncoding == bodyMultipart
}

// opHasXMLBody reports whether a wired operation sends an XML request body
// (mapToXML). Read/Delete never send a body.
func opHasXMLBody(op crudOperationPlan) bool {
	return methodHasBody(op.method) && op.bodyEncoding == bodyXML
}

// methodHasBody reports whether the HTTP method carries a request body. Only
// body-bearing methods (POST/PUT/PATCH) can send a JSON or form-encoded body;
// GET/DELETE are bodiless, so formData parameters on them cannot be sent and
// the operation stays honestly scaffolded.
func methodHasBody(method string) bool {
	switch method {
	case "POST", "PUT", "PATCH":
		return true
	}
	return false
}

// noteOpImportNeeds records that the plan needs the strings and strconv imports
// to render one operation's path substitutions, query/header/cookie parameters,
// and formData parameters as request strings. Non-string primitives require
// strconv; any substitution or parameter requires strings. A form-encoded body
// also requires strings for strings.NewReader (the form body reader), recorded
// here so the strings import is gated on the form body alongside net/url.
func (plan *resourceWiringPlan) noteOpImportNeeds(op crudOperationPlan) {
	for _, sub := range op.subs {
		plan.needsStrings = true
		plan.needsURL = true
		if sub.primitive != ir.TypeString {
			plan.needsStrconv = true
		}
	}
	for _, params := range [][]paramSubstitution{op.queryParams, op.headerParams, op.cookieParams, op.formDataParams} {
		for _, p := range params {
			plan.needsStrings = true
			if p.primitive != ir.TypeString {
				plan.needsStrconv = true
			}
		}
	}
	// A form-encoded body is read with strings.NewReader(form.Encode()).
	if opHasFormBody(op) {
		plan.needsStrings = true
	}
}

// planOperation resolves one CRUD operation mapping into a generation plan.
func planOperation(r ir.ResourceIR, op ir.OperationMappingIR) (crudOperationPlan, bool) {
	var planned crudOperationPlan
	planned.method = strings.ToUpper(strings.TrimSpace(op.Method))
	planned.template = strings.TrimSpace(op.PathTemplate)
	planned.successCodes = op.SuccessCodes
	planned.errorMappings = errorMappingDescriptions(op.ErrorMappings)
	if planned.method == "" || planned.template == "" {
		return planned, false
	}

	placeholders := pathPlaceholders(planned.template)
	// The resource-id fallback (using the single identifier attribute for a
	// placeholder whose name does not match a schema attribute) is only valid for
	// a simple-id path with one placeholder. A composite path (multiple
	// placeholders) describes distinct path parameters; falling back to the same
	// id attribute for each would substitute the same value into every slot and
	// produce a wrong URL, so each placeholder must match a same-named attribute
	// or the operation is not wired (honest scaffold, REMAINING_GAPS §3/#12).
	multiPlaceholder := len(placeholders) > 1
	for _, placeholder := range placeholders {
		sub, ok := resolvePathSubstitution(r, placeholder, multiPlaceholder)
		if !ok {
			return crudOperationPlan{}, false
		}
		planned.subs = append(planned.subs, sub)
	}

	// formData parameters (OpenAPI 2.0 form-encoded request bodies) are wired
	// as application/x-www-form-urlencoded on body-bearing methods (POST/PUT/
	// PATCH): the create/update body builds a url.Values from the resolved
	// schema attributes and sends it instead of a JSON body. formData is only
	// resolvable when every declared formData parameter matches a primitive
	// schema attribute (a non-primitive parameter such as a file upload cannot
	// be form-encoded from a typed attribute, so the operation stays honestly
	// scaffolded and the transformer emits a fail-loud warning — REMAINING_GAPS
	// §2). On a bodiless GET/DELETE, formData cannot be sent at all, so the
	// operation stays scaffolded rather than wiring a body with the wrong shape.
	if len(op.FormDataParams) > 0 {
		if !methodHasBody(planned.method) {
			return crudOperationPlan{}, false
		}
		formData, fok := resolveFormDataSubstitutions(r.Schema.Attributes, op.FormDataParams)
		if !fok {
			return crudOperationPlan{}, false
		}
		planned.formDataParams = formData
		// A formData request body's media type selects the encoding:
		// multipart/form-data (a binary param triggers this in v2) writes file
		// and text parts via mime/multipart; otherwise the body is
		// application/x-www-form-urlencoded via url.Values (A2).
		if transformer.RequestBodyKind(op.MediaType) == "multipart" {
			planned.bodyEncoding = bodyMultipart
			// contentType stays empty: the multipart body builder sets the
			// Content-Type dynamically to writer.FormDataContentType() so it
			// carries the generated boundary.
		} else {
			planned.bodyEncoding = bodyForm
			planned.contentType = "application/x-www-form-urlencoded"
		}
	} else if methodHasBody(planned.method) {
		// A non-formData body-bearing method encodes its body per the request
		// media type: JSON (the default, including JSON dialects and empty),
		// XML, or unsupported (fail-loud — the transformer warned; keep the
		// operation an honest scaffold rather than silently emitting JSON).
		switch transformer.RequestBodyKind(op.MediaType) {
		case "xml":
			planned.bodyEncoding = bodyXML
			planned.contentType = "application/xml"
			// The XML root element is the resource name (e.g. "pet"); custom xml
			// keyword names/attributes are out of scope for A2.
			planned.xmlRoot = r.Name
		case "json":
			planned.bodyEncoding = bodyJSON
			// contentType empty → defaults to application/json in sendRequestStmts.
		default: // "unsupported"
			return crudOperationPlan{}, false
		}
	}

	// Resolve query, header, and cookie parameters to the resource schema
	// attributes that supply their values. A required parameter with no matching
	// attribute cannot be sent, so the operation is not wired rather than
	// emitting a body that would fail at runtime; an optional unmapped parameter
	// is skipped.
	queryParams, qok := resolveParamSubstitutions(r.Schema.Attributes, op.QueryParams)
	if !qok {
		return crudOperationPlan{}, false
	}
	headerParams, hok := resolveParamSubstitutions(r.Schema.Attributes, op.HeaderParams)
	if !hok {
		return crudOperationPlan{}, false
	}
	cookieParams, cok := resolveParamSubstitutions(r.Schema.Attributes, op.CookieParams)
	if !cok {
		return crudOperationPlan{}, false
	}
	planned.queryParams = queryParams
	planned.headerParams = headerParams
	planned.cookieParams = cookieParams
	planned.responseEnvelope = op.ResponseEnvelope

	// Per-operation security (REMAINING_GAPS §1). See applySecurityRequirements.
	applySecurityRequirements(&planned, op.SecurityRequirements)

	return planned, true
}

// applySecurityRequirements records the per-operation security selection on a
// wired operation plan. An operation declaring exactly one security requirement
// applies only that requirement's scheme interceptors: the scheme names are
// baked into the wired body as a client.WithSchemes(...) request option (AND
// resolution). An operation declaring no security (nil) inherits the global
// default — securitySchemesSet stays false, no WithSchemes is emitted, and
// NewRequest applies every configured scheme interceptor. An operation
// declaring more than one requirement (OR) is ambiguous for a non-interactive
// provider; the transformer warns (warnPerOpORSecurity) and the body inherits
// the global default (securitySchemesSet false). A single empty requirement
// ({}) marks the operation unauthenticated: securitySchemesSet true with no
// names, so WithSchemes() applies no scheme interceptors. Names are sorted so
// the emitted call is deterministic for a spec that lists schemes in map order.
func applySecurityRequirements(planned *crudOperationPlan, reqs []map[string][]string) {
	switch len(reqs) {
	case 1:
		planned.securitySchemesSet = true
		for name := range reqs[0] {
			planned.securitySchemes = append(planned.securitySchemes, name)
		}
		sort.Strings(planned.securitySchemes)
	default:
		// 0 (nil) or >1 (OR): inherit the global default; no WithSchemes.
	}
}

// errorMappingDescriptions flattens an ErrorMappings map into a code→description
// map suitable for the generation plan, dropping entries without a description
// so the generated per-code switch only carries meaningful diagnostics.
func errorMappingDescriptions(m map[int]ir.ErrorMappingIR) map[int]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[int]string, len(m))
	for code, em := range m {
		if strings.TrimSpace(em.Description) == "" {
			continue
		}
		out[code] = em.Description
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// resolveParamSubstitutions maps operation parameters to the schema attributes
// that supply their values. A parameter maps to a same-named primitive schema
// attribute. It returns ok=false when a required parameter has no matching
// attribute, so the caller disables wiring rather than emitting a body that
// omits a required parameter.
func resolveParamSubstitutions(attrs []ir.AttributeIR, params []ir.ParamIR) ([]paramSubstitution, bool) {
	var subs []paramSubstitution
	for _, p := range params {
		field, prim, ok := matchParamAttribute(attrs, p)
		if !ok {
			if p.Required {
				return nil, false
			}
			continue
		}
		subs = append(subs, paramSubstitution{name: p.Name, in: p.In, field: field, primitive: prim, required: p.Required})
	}
	return subs, true
}

// resolveFormDataSubstitutions resolves formData parameters to the resource
// schema attributes that supply their values. Unlike query/header/cookie
// parameters, a formData parameter is part of the request body, so every
// declared formData parameter must resolve to a strict primitive (string,
// integer, number, boolean) schema attribute: an unmapped parameter, a
// non-primitive (e.g. file upload), or a Dynamic attribute cannot be
// form-encoded from a typed attribute (a Dynamic field has no ValueString
// accessor), and silently omitting it from the body would drop a declared
// input, so the whole operation stays honestly scaffolded rather than wiring a
// partial form body (REMAINING_GAPS §2).
func resolveFormDataSubstitutions(attrs []ir.AttributeIR, params []ir.ParamIR) ([]paramSubstitution, bool) {
	subs := make([]paramSubstitution, 0, len(params))
	for _, p := range params {
		field, prim, ok := matchParamAttribute(attrs, p)
		if !ok || !isFormEncodablePrimitive(prim) {
			return nil, false
		}
		subs = append(subs, paramSubstitution{
			name:      p.Name,
			in:        p.In,
			field:     field,
			primitive: prim,
			required:  p.Required,
			// format: binary marks a file upload: the multipart body builder
			// writes it as a file part (A2).
			binary: strings.EqualFold(p.Schema.Format, "binary"),
		})
	}
	return subs, true
}

// isFormEncodablePrimitive reports whether a primitive type can be rendered as
// a string for a form-encoded body. Dynamic is excluded: a Dynamic model field
// has no ValueString accessor, so it cannot be form-encoded.
func isFormEncodablePrimitive(t ir.PrimitiveType) bool {
	return t == ir.TypeString || t == ir.TypeInt || t == ir.TypeFloat || t == ir.TypeBool
}

// matchParamAttribute finds a primitive schema attribute supplying the
// parameter's value. The match is on the normalized (PascalCase) field name so a
// hyphenated HTTP header such as "X-Trace-Id" maps to a snake_case Terraform
// attribute such as "x_trace_id", which is the only valid attribute shape. It
// returns the Go field name and primitive type.
func matchParamAttribute(attrs []ir.AttributeIR, p ir.ParamIR) (string, ir.PrimitiveType, bool) {
	want := naming.GoFieldName(p.Name)
	for _, attr := range attrs {
		// Match by the sanitized attribute name or its original wire name
		// (G14): a param like SAMLRequest maps to the saml_request attribute.
		if naming.GoFieldName(attr.Name) != want && (attr.WireName == "" || naming.GoFieldName(attr.WireName) != want) {
			continue
		}
		if !schema.IsPrimitiveSchema(attr.Schema) {
			return "", "", false
		}
		return naming.GoFieldName(attr.Name), attr.Schema.Type, true
	}
	return "", "", false
}

// pathPlaceholders returns the {name} placeholders in a path template in
// left-to-right order.
func pathPlaceholders(template string) []string {
	var names []string
	rest := template
	for {
		start := strings.Index(rest, "{")
		if start < 0 {
			return names
		}
		end := strings.Index(rest[start:], "}")
		if end < 0 {
			return names
		}
		if name := rest[start+1 : start+end]; name != "" {
			names = append(names, name)
		}
		rest = rest[start+end+1:]
	}
}

// resolvePathSubstitution maps a path placeholder to the model field that
// supplies its value. A schema attribute with the placeholder's name wins;
// otherwise, for a simple-id path, the resource ID attribute is used, which is
// the common case for read/update/delete paths such as "/pets/{petId}" where the
// placeholder name does not match the Terraform attribute name. When
// noIDFallback is true (a composite path with multiple placeholders) the
// id-attribute fallback is suppressed: each placeholder must match a same-named
// attribute, since substituting the single id into every slot would build a
// wrong URL (REMAINING_GAPS §3/#12).
func resolvePathSubstitution(r ir.ResourceIR, placeholder string, noIDFallback bool) (pathSubstitution, bool) {
	for _, attr := range r.Schema.Attributes {
		if attr.Name != placeholder {
			continue
		}
		if !schema.IsPrimitiveSchema(attr.Schema) {
			return pathSubstitution{}, false
		}
		return pathSubstitution{placeholder: placeholder, field: naming.GoFieldName(attr.Name), primitive: attr.Schema.Type}, true
	}
	// The placeholder names a UID-shaped identifier (e.g. "folder_uid",
	// "library_element_uid") but no attribute carries that name. Prefer the
	// resource's `uid` attribute over the numeric id fallback so requests like
	// GET /folders/{folder_uid} are filled with the UID, not the numeric id
	// (G19). The MyCloud folder instance path is the motivating case.
	if uidPlaceholder(placeholder) {
		if attr, ok := uidAttribute(r); ok {
			return pathSubstitution{placeholder: placeholder, field: naming.GoFieldName(attr.Name), primitive: attr.Schema.Type}, true
		}
	}
	if noIDFallback {
		return pathSubstitution{}, false
	}
	info := resourceIDFieldInfo(r)
	if !info.found {
		return pathSubstitution{}, false
	}
	switch info.primitive {
	case ir.TypeString, ir.TypeInt, ir.TypeFloat, ir.TypeBool:
		return pathSubstitution{placeholder: placeholder, field: info.field, primitive: info.primitive}, true
	}
	return pathSubstitution{}, false
}

// uidPlaceholder reports whether a path placeholder names a UID-shaped
// identifier: "uid", "UID", "folder_uid", "library_element_uid", ...
func uidPlaceholder(placeholder string) bool {
	lower := strings.ToLower(placeholder)
	return lower == "uid" || strings.HasSuffix(lower, "_uid")
}

// uidAttribute returns the resource's `uid` attribute when it is a primitive
// string type. Used to fill UID-shaped path placeholders that no attribute
// matches by name (G19).
func uidAttribute(r ir.ResourceIR) (ir.AttributeIR, bool) {
	for _, attr := range r.Schema.Attributes {
		if attr.Name == "uid" && attr.Schema.Type == ir.TypeString && schema.IsPrimitiveSchema(attr.Schema) {
			return attr, true
		}
	}
	return ir.AttributeIR{}, false
}

// httpMethodExpr returns the net/http method constant for a recognized HTTP
// method, falling back to a string literal for uncommon verbs.
func httpMethodExpr(method string) ast.Expr {
	switch method {
	case "GET":
		return astgen.QualExpr("http", "MethodGet")
	case "POST":
		return astgen.QualExpr("http", "MethodPost")
	case "PUT":
		return astgen.QualExpr("http", "MethodPut")
	case "PATCH":
		return astgen.QualExpr("http", "MethodPatch")
	case "DELETE":
		return astgen.QualExpr("http", "MethodDelete")
	case "HEAD":
		return astgen.QualExpr("http", "MethodHead")
	}
	return astgen.Lit(method)
}

// addErrorStmt emits resp.Diagnostics.AddError(summary, detail).
func addErrorStmt(summary string, detail ast.Expr) ast.Stmt {
	return astgen.ExprStmt(astgen.Call(
		astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "AddError"),
		astgen.Lit(summary),
		detail,
	))
}

// addWarningStmt emits resp.Diagnostics.AddWarning(summary, detail). It is
// used for honest, non-fatal signals — e.g. a declared security scheme that
// exposes provider config attributes but has no generated interceptor, so a
// practitioner who configures it is not silently left unauthenticated.
func addWarningStmt(summary string, detail ast.Expr) ast.Stmt {
	return astgen.ExprStmt(astgen.Call(
		astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "AddWarning"),
		astgen.Lit(summary),
		detail,
	))
}

// addErrorfStmt emits resp.Diagnostics.AddError(summary, fmt.Sprintf(format, err)).
func addErrorfStmt(summary, format string, arg ast.Expr) ast.Stmt {
	return addErrorStmt(summary, astgen.Call(
		astgen.QualExpr("fmt", "Sprintf"),
		astgen.Lit(format),
		arg,
	))
}

// errCheckStmt emits if err != nil { AddError(summary, fmt.Sprintf(format, err)); return }.
func errCheckStmt(summary, format string) ast.Stmt {
	return astgen.If(
		astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
		addErrorfStmt(summary, format, astgen.Ident("err")),
		astgen.Return(),
	)
}

// clientGuardStmt emits the fail-loud guard for a resource or data source used
// before the provider Configure method stored the API client on it. receiver is
// the generated method receiver identifier ("r" for resources, "d" for data
// sources), whose `client` field holds the configured API client.
func clientGuardStmt(receiver string) ast.Stmt {
	return astgen.If(
		astgen.Equal(astgen.Selector(astgen.Ident(receiver), "client"), astgen.Nil()),
		astgen.Block(
			addErrorStmt(
				"Client Not Configured",
				astgen.Lit("The API client was not set on the resource. The provider Configure method must run before resource operations; this is a bug in the generated provider."),
			),
			astgen.Return(),
		),
	)
}

// requestPathStmts emits the statements building reqPath from a path template,
// substituting each {placeholder} with the URL-path-escaped value of the planned
// model field. PathEscape ensures a value containing reserved characters
// (e.g. a name with a space or "/") is encoded as a single path segment rather
// than producing a malformed URL or spurious extra segments.
func requestPathStmts(op crudOperationPlan, modelVar string) []ast.Stmt {
	stmts := make([]ast.Stmt, 0, 1+len(op.subs))
	stmts = append(stmts, astgen.AssignSingle(astgen.Ident("reqPath"), astgen.Lit(op.template)))
	for _, sub := range op.subs {
		stmts = append(stmts, astgen.AssignStmt(
			[]ast.Expr{astgen.Ident("reqPath")},
			[]ast.Expr{astgen.Call(
				astgen.QualExpr("strings", "ReplaceAll"),
				astgen.Ident("reqPath"),
				astgen.Lit("{"+sub.placeholder+"}"),
				astgen.Call(astgen.QualExpr("url", "PathEscape"), pathValueExpr(modelVar, sub)),
			)},
			token.ASSIGN,
		))
	}
	return stmts
}

// requestQueryStmts emits the statements that encode the operation's query
// parameters onto the request URL from the model variable. It returns nil when
// the operation has no query parameters, so bodiless/path-only requests emit no
// url.Values code and the net/url import stays unused.
func requestQueryStmts(op crudOperationPlan, modelVar string) []ast.Stmt {
	if len(op.queryParams) == 0 {
		return nil
	}
	stmts := make([]ast.Stmt, 0, 2+len(op.queryParams))
	stmts = append(stmts, astgen.AssignSingle(
		astgen.Ident("query"),
		astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("httpReq"), "URL"), "Query")),
	))
	for _, p := range op.queryParams {
		setCall := astgen.Call(
			astgen.Selector(astgen.Ident("query"), "Set"),
			astgen.Lit(p.name),
			paramValueExpr(modelVar, p),
		)
		stmts = append(stmts, gateParamStmts(p, modelVar, astgen.ExprStmt(setCall))...)
	}
	stmts = append(stmts, astgen.AssignStmt(
		[]ast.Expr{astgen.Selector(astgen.Selector(astgen.Ident("httpReq"), "URL"), "RawQuery")},
		[]ast.Expr{astgen.Call(astgen.Selector(astgen.Ident("query"), "Encode"))},
		token.ASSIGN,
	))
	return stmts
}

// requestHeaderStmts emits the statements that set the operation's header
// parameters on the request from the model variable. It returns nil when the
// operation has no header parameters.
func requestHeaderStmts(op crudOperationPlan, modelVar string) []ast.Stmt {
	if len(op.headerParams) == 0 {
		return nil
	}
	stmts := make([]ast.Stmt, 0, len(op.headerParams))
	for _, p := range op.headerParams {
		setCall := astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("httpReq"), "Header"), "Set"),
			astgen.Lit(p.name),
			paramValueExpr(modelVar, p),
		)
		stmts = append(stmts, gateParamStmts(p, modelVar, astgen.ExprStmt(setCall))...)
	}
	return stmts
}

// requestCookieStmts emits the statements that add the operation's cookie
// parameters to the request from the model variable. Each cookie is added via
// httpReq.AddCookie, which serializes it onto the Cookie header as
// "name=value". It returns nil when the operation has no cookie parameters.
func requestCookieStmts(op crudOperationPlan, modelVar string) []ast.Stmt {
	if len(op.cookieParams) == 0 {
		return nil
	}
	stmts := make([]ast.Stmt, 0, len(op.cookieParams))
	for _, p := range op.cookieParams {
		addCall := astgen.Call(
			astgen.Selector(astgen.Ident("httpReq"), "AddCookie"),
			astgen.UnaryPtr(astgen.CompositeLit(
				astgen.QualExpr("http", "Cookie"),
				astgen.KeyValue("Name", astgen.Lit(p.name)),
				astgen.KeyValue("Value", paramValueExpr(modelVar, p)),
			)),
		)
		stmts = append(stmts, gateParamStmts(p, modelVar, astgen.ExprStmt(addCall))...)
	}
	return stmts
}

// pathValueExpr returns the expression rendering a path substitution's model
// field as a string.
func pathValueExpr(modelVar string, sub pathSubstitution) ast.Expr {
	return modelFieldStringExpr(modelVar, sub.field, sub.primitive)
}

// paramValueExpr returns the expression rendering a query/header parameter's
// model field as a string for the request.
func paramValueExpr(modelVar string, p paramSubstitution) ast.Expr {
	return modelFieldStringExpr(modelVar, p.field, p.primitive)
}

// gateParamStmts returns stmts as-is when p is required, or wrapped in a single
// "if !<modelVar>.<field>.IsNull() { ... }" when p is optional. An unset
// optional parameter is omitted from the request entirely rather than sent as
// the zero-value empty string, which the API may reject or misinterpret. All
// typed framework values implement attr.Value, whose IsNull method reports an
// explicitly unset (null) value; user-supplied optional parameters are either
// set (known) or null at apply time, so IsNull is the correct gate.
func gateParamStmts(p paramSubstitution, modelVar string, stmts ...ast.Stmt) []ast.Stmt {
	if p.required {
		return stmts
	}
	sel := astgen.Selector(astgen.Ident(modelVar), p.field)
	notNull := astgen.Unary(token.NOT, astgen.Call(astgen.Selector(sel, "IsNull")))
	return []ast.Stmt{astgen.If(notNull, stmts...)}
}

// modelFieldStringExpr renders a typed model field as a string, formatting
// non-string primitives via strconv so they can be substituted into a path or
// serialized into a query string or header.
func modelFieldStringExpr(modelVar, field string, primitive ir.PrimitiveType) ast.Expr {
	sel := astgen.Selector(astgen.Ident(modelVar), field)
	switch primitive {
	case ir.TypeInt:
		return astgen.Call(astgen.QualExpr("strconv", "FormatInt"), astgen.Call(astgen.Selector(sel, "ValueInt64")), astgen.IntLit(10))
	case ir.TypeFloat:
		return astgen.Call(astgen.QualExpr("strconv", "FormatFloat"), astgen.Call(astgen.Selector(sel, "ValueFloat64")), astgen.BasicLit(token.CHAR, "'f'"), astgen.IntLit(-1), astgen.IntLit(64))
	case ir.TypeBool:
		return astgen.Call(astgen.QualExpr("strconv", "FormatBool"), astgen.Call(astgen.Selector(sel, "ValueBool")))
	default:
		return astgen.Call(astgen.Selector(sel, "ValueString"))
	}
}

// sendRequestStmts emits the NewRequest + Do + status-check sequence for a
// wired operation. receiver is the generated method receiver identifier ("r"
// for resources, "d" for data sources) whose `client` field holds the API client.
// body is the request body expression (nil for bodiless requests). modelVar is
// the name of the model variable in scope (plan, state, or config) from which
// query and header parameter values are read. notFound, when non-nil, is
// emitted as the handler for an HTTP 404 response before the non-success check.
func sendRequestStmts(op crudOperationPlan, receiver, summary, modelVar string, body ast.Expr, notFound []ast.Stmt) []ast.Stmt {
	bodyArg := body
	if bodyArg == nil {
		bodyArg = astgen.Nil()
	}
	stmts := []ast.Stmt{
		astgen.Assign(
			[]ast.Expr{astgen.Ident("httpReq"), astgen.Ident("err")},
			[]ast.Expr{newRequestCall(receiver, op, bodyArg)},
		),
		errCheckStmt(summary, "Could not build request: %s"),
	}
	stmts = append(stmts, requestQueryStmts(op, modelVar)...)
	stmts = append(stmts, requestHeaderStmts(op, modelVar)...)
	stmts = append(stmts, requestCookieStmts(op, modelVar)...)
	if body != nil {
		// The request body media type. Gated on body != nil so bodiless requests
		// (Read/Delete) set no Content-Type. A multipart body sets the
		// Content-Type dynamically to formWriter.FormDataContentType() — the
		// boundary is generated at runtime and must be carried in the header.
		// The static path covers form (application/x-www-form-urlencoded), XML
		// (application/xml), and JSON (application/json, the default when
		// contentType is empty).
		if op.bodyEncoding == bodyMultipart {
			stmts = append(stmts, astgen.ExprStmt(astgen.Call(
				astgen.Selector(astgen.Selector(astgen.Ident("httpReq"), "Header"), "Set"),
				astgen.Lit("Content-Type"),
				astgen.Call(astgen.Selector(astgen.Ident("formWriter"), "FormDataContentType")),
			)))
		} else {
			contentType := "application/json"
			if op.contentType != "" {
				contentType = op.contentType
			}
			stmts = append(stmts, astgen.ExprStmt(astgen.Call(
				astgen.Selector(astgen.Selector(astgen.Ident("httpReq"), "Header"), "Set"),
				astgen.Lit("Content-Type"),
				astgen.Lit(contentType),
			)))
		}
	}
	stmts = append(stmts,
		astgen.Assign(
			[]ast.Expr{astgen.Ident("httpResp"), astgen.Ident("err")},
			[]ast.Expr{astgen.Call(
				astgen.Selector(astgen.Selector(astgen.Ident(receiver), "client"), "Do"),
				astgen.Ident("httpReq"),
			)},
		),
		errCheckStmt(summary, "Could not send request: %s"),
		astgen.Defer(astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("httpResp"), "Body"), "Close"))),
	)
	if len(notFound) > 0 {
		stmts = append(stmts, astgen.If(
			astgen.Equal(
				astgen.Selector(astgen.Ident("httpResp"), "StatusCode"),
				astgen.QualExpr("http", "StatusNotFound"),
			),
			notFound...,
		))
	}
	stmts = append(stmts, astgen.If(
		astgen.Unary(token.NOT, successCondition(op)),
		nonSuccessBlock(op, summary)...,
	))
	return stmts
}

// newRequestCall returns the <receiver>.client.NewRequest(...) call expression
// for a wired operation, passing client.WithSchemes(...) as a variadic request
// option when the operation declares a single security requirement (per-
// operation AND resolution, REMAINING_GAPS §1). When securitySchemesSet is
// false (no security → inherit the global default, or OR → ambiguous) no
// WithSchemes is passed and NewRequest applies every configured scheme
// interceptor. The scheme names are baked as string literals, sorted at
// generation time so the emitted call is deterministic.
func newRequestCall(receiver string, op crudOperationPlan, bodyArg ast.Expr) *ast.CallExpr {
	args := []ast.Expr{
		astgen.Ident("ctx"),
		httpMethodExpr(op.method),
		astgen.Ident("reqPath"),
		bodyArg,
	}
	if op.securitySchemesSet {
		schemeArgs := make([]ast.Expr, len(op.securitySchemes))
		for i, name := range op.securitySchemes {
			schemeArgs[i] = astgen.Lit(name)
		}
		args = append(args, astgen.Call(astgen.QualExpr("client", "WithSchemes"), schemeArgs...))
	}
	return astgen.Call(
		astgen.Selector(astgen.Selector(astgen.Ident(receiver), "client"), "NewRequest"),
		args...,
	)
}

// successCondition returns the expression that is true when the response status
// is one of the operation's declared success codes. When the operation declares
// no success codes it falls back to the generic 2xx range, preserving the
// pre-§2 behavior for operations whose spec did not surface response codes.
func successCondition(op crudOperationPlan) ast.Expr {
	sc := op.successCodes
	if len(sc) == 0 {
		return astgen.Binary(
			astgen.Binary(astgen.Selector(astgen.Ident("httpResp"), "StatusCode"), token.GEQ, astgen.IntLit(200)),
			token.LAND,
			astgen.Binary(astgen.Selector(astgen.Ident("httpResp"), "StatusCode"), token.LSS, astgen.IntLit(300)),
		)
	}
	expr := astgen.Equal(astgen.Selector(astgen.Ident("httpResp"), "StatusCode"), astgen.IntLit(sc[0]))
	for _, code := range sc[1:] {
		expr = astgen.Binary(expr, token.LOR, astgen.Equal(astgen.Selector(astgen.Ident("httpResp"), "StatusCode"), astgen.IntLit(code)))
	}
	return expr
}

// nonSuccessBlock returns the statements emitted in the non-success branch: a
// per-status-code switch surfaced from the operation's ErrorMappings (so a 401
// reports "Unauthorized" rather than a generic client error), with a default
// arm that falls through to the generic client.NewAPIError path. When no error
// mappings are declared the switch is omitted and the generic path runs
// directly. Codes are emitted in ascending order so generation stays
// deterministic.
func nonSuccessBlock(op crudOperationPlan, summary string) []ast.Stmt {
	generic := []ast.Stmt{
		astgen.Assign(
			[]ast.Expr{astgen.Ident("apiErr"), astgen.Ident("err")},
			[]ast.Expr{astgen.Call(astgen.QualExpr("client", "NewAPIError"), astgen.Ident("httpResp"))},
		),
		errCheckStmt(summary, "Could not read error response: %s"),
		addErrorStmt(summary, astgen.Call(astgen.Selector(astgen.Ident("apiErr"), "Error"))),
		astgen.Return(),
	}
	codes := sortedErrorCodes(op.errorMappings)
	if len(codes) == 0 {
		return generic
	}
	clauses := make([]ast.Stmt, 0, len(codes)+1)
	for _, code := range codes {
		cc := astgen.CaseClause(astgen.IntLit(code))
		cc.Body = []ast.Stmt{
			addErrorStmt(summary, astgen.Lit(op.errorMappings[code])),
			astgen.Return(),
		}
		clauses = append(clauses, cc)
	}
	defaultClause := &ast.CaseClause{List: nil, Body: generic}
	clauses = append(clauses, defaultClause)
	return []ast.Stmt{astgen.SwitchStmt(
		astgen.Selector(astgen.Ident("httpResp"), "StatusCode"),
		astgen.Block(clauses...),
	)}
}

// sortedErrorCodes returns the keys of an error-mappings map in ascending
// order, so generated per-code switches are deterministic.
func sortedErrorCodes(m map[int]string) []int {
	if len(m) == 0 {
		return nil
	}
	codes := make([]int, 0, len(m))
	for code := range m {
		codes = append(codes, code)
	}
	sort.Ints(codes)
	return codes
}

// decodeAndApplyStmts emits the statements decoding a JSON response body and
// applying it to the named model variable.
func decodeAndApplyStmts(summary, modelVar, envelope string) []ast.Stmt {
	stmts := []ast.Stmt{
		astgen.DeclStmt(astgen.VarDeclGen(astgen.VarSpec(
			"data",
			astgen.MapType(astgen.Ident("string"), astgen.Ident("any")),
			nil,
		))),
		astgen.AssignSingle(astgen.Ident("decoder"), astgen.Call(
			astgen.QualExpr("json", "NewDecoder"),
			astgen.Selector(astgen.Ident("httpResp"), "Body"),
		)),
		astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("decoder"), "UseNumber"))),
		&ast.IfStmt{
			Init: astgen.AssignSingle(astgen.Ident("err"), astgen.Call(
				astgen.Selector(astgen.Ident("decoder"), "Decode"),
				astgen.UnaryPtr(astgen.Ident("data")),
			)),
			Cond: astgen.Binary(
				astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
				token.LAND,
				astgen.NotEqual(astgen.Ident("err"), astgen.QualExpr("io", "EOF")),
			),
			Body: astgen.Block(
				addErrorfStmt(summary, "Could not decode response body: %s", astgen.Ident("err")),
				astgen.Return(),
			),
		},
	}
	// A {data: ...} response envelope (E1): the transformer flattened the payload
	// out of the response schema, so unwrap the decoded body by the same key
	// before applying it to the model. The type assertion is fail-safe — a
	// response that does not carry the envelope object leaves data untouched and
	// applyJSONToModel simply finds no matching fields.
	if envelope != "" {
		stmts = append(stmts, &ast.IfStmt{
			Init: astgen.Assign(
				[]ast.Expr{astgen.Ident("v"), astgen.Ident("ok")},
				[]ast.Expr{astgen.IndexExpr(astgen.Ident("data"), astgen.Lit(envelope))},
			),
			Cond: astgen.Ident("ok"),
			Body: astgen.Block(
				&ast.IfStmt{
					Init: astgen.Assign(
						[]ast.Expr{astgen.Ident("m"), astgen.Ident("ok")},
						[]ast.Expr{astgen.TypeAssertExpr(astgen.Ident("v"), astgen.MapType(astgen.Ident("string"), astgen.Ident("any")))},
					),
					Cond: astgen.Ident("ok"),
					Body: astgen.Block(
						astgen.AssignStmt(
							[]ast.Expr{astgen.Ident("data")},
							[]ast.Expr{astgen.Ident("m")},
							token.ASSIGN,
						),
					),
				},
			),
		})
	}
	stmts = append(stmts,
		astgen.AssignStmt(
			[]ast.Expr{astgen.Ident("err")},
			[]ast.Expr{astgen.Call(
				astgen.Ident("applyJSONToModel"),
				astgen.UnaryPtr(astgen.Ident(modelVar)),
				astgen.Ident("data"),
			)},
			token.ASSIGN,
		),
		errCheckStmt(summary, "Could not map response to state: %s"),
	)
	return stmts
}

// stateSetStmt emits resp.Diagnostics.Append(resp.State.Set(ctx, &model)...).
func stateSetStmt(modelVar string) ast.Stmt {
	return astgen.ExprStmt(astgen.Call(
		astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "Append"),
		astgen.Ellipsis(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "State"), "Set"),
			astgen.Ident("ctx"),
			astgen.UnaryPtr(astgen.Ident(modelVar)),
		)),
	))
}

// requestBodyStmts returns the statements that build a wired create/update
// request body and the body reader expression passed to sendRequestStmts. The
// encoding is selected by planOperation from the request body media type: a
// formData body is application/x-www-form-urlencoded (url.Values) or
// multipart/form-data (mime/multipart, when a binary param is present);
// otherwise the body is JSON (modelToJSONMap + json.Marshal) or XML (mapToXML).
// The paths are mutually exclusive: an operation declares either a JSON/XML
// request body or formData parameters, never both.
func requestBodyStmts(op crudOperationPlan, summary, modelVar string) ([]ast.Stmt, ast.Expr) {
	switch op.bodyEncoding {
	case bodyForm:
		return formBodyStmts(op, modelVar)
	case bodyMultipart:
		return multipartBodyStmts(op, summary, modelVar)
	case bodyXML:
		return xmlBodyStmts(op, summary, modelVar)
	default: // bodyJSON
		return jsonBodyStmts(summary, modelVar)
	}
}

// jsonBodyStmts builds a JSON request body from the model: modelToJSONMap
// converts the typed model to a map, json.Marshal encodes it, and bytes.NewReader
// wraps the encoded payload as the request body reader.
func jsonBodyStmts(summary, modelVar string) ([]ast.Stmt, ast.Expr) {
	stmts := []ast.Stmt{
		astgen.Assign(
			[]ast.Expr{astgen.Ident("body"), astgen.Ident("err")},
			[]ast.Expr{astgen.Call(astgen.Ident("modelToJSONMap"), astgen.UnaryPtr(astgen.Ident(modelVar)))},
		),
		errCheckStmt(summary, "Could not build request body: %s"),
		astgen.Assign(
			[]ast.Expr{astgen.Ident("payload"), astgen.Ident("err")},
			[]ast.Expr{astgen.Call(astgen.QualExpr("json", "Marshal"), astgen.Ident("body"))},
		),
		errCheckStmt(summary, "Could not encode request body: %s"),
	}
	body := astgen.Call(astgen.QualExpr("bytes", "NewReader"), astgen.Ident("payload"))
	return stmts, body
}

// formBodyStmts builds an application/x-www-form-urlencoded request body from
// the operation's formData parameters: a url.Values populated with form.Set for
// each resolved schema attribute and encoded, then wrapped with
// strings.NewReader as the request body reader. Unlike the JSON path, form
// encoding cannot fail, so no error check (and no summary) follows the build.
func formBodyStmts(op crudOperationPlan, modelVar string) ([]ast.Stmt, ast.Expr) {
	stmts := make([]ast.Stmt, 0, 2+len(op.formDataParams))
	stmts = append(stmts, astgen.AssignSingle(
		astgen.Ident("form"),
		astgen.CompositeLit(astgen.QualExpr("url", "Values")),
	))
	for _, p := range op.formDataParams {
		setCall := astgen.Call(
			astgen.Selector(astgen.Ident("form"), "Set"),
			astgen.Lit(p.name),
			paramValueExpr(modelVar, p),
		)
		stmts = append(stmts, gateParamStmts(p, modelVar, astgen.ExprStmt(setCall))...)
	}
	stmts = append(stmts, astgen.AssignSingle(
		astgen.Ident("payload"),
		astgen.Call(astgen.Selector(astgen.Ident("form"), "Encode")),
	))
	body := astgen.Call(astgen.QualExpr("strings", "NewReader"), astgen.Ident("payload"))
	return stmts, body
}

// multipartBodyStmts builds a multipart/form-data request body from the
// operation's formData parameters via mime/multipart.NewWriter. Each
// non-binary parameter is written as a text field (formWriter.WriteField); each
// binary parameter (OpenAPI format: binary) is written as a file part: the model
// field holds the upload path, which is os.Open'd, copied into a
// formWriter.CreateFormFile part, and closed. The writer is left in scope as the
// `formWriter` variable so sendRequestStmts can set the Content-Type to
// writer.FormDataContentType() (the boundary is generated at runtime). The body
// reader is &buf (*bytes.Buffer implements io.Reader). Errors at every step are
// surfaced as diagnostics and return the method (A2).
func multipartBodyStmts(op crudOperationPlan, summary, modelVar string) ([]ast.Stmt, ast.Expr) {
	stmts := make([]ast.Stmt, 0, 3+5*len(op.formDataParams))
	// Declare the multipart body buffer and a writer over it. The writer stays
	// in scope as `formWriter` so sendRequestStmts can read its
	// FormDataContentType() for the request Content-Type (the boundary is
	// generated at runtime and must be carried in the header).
	stmts = append(stmts,
		astgen.DeclStmt(astgen.VarDeclGen(astgen.VarSpec(
			"buf",
			astgen.QualExpr("bytes", "Buffer"),
			nil,
		))),
		astgen.AssignSingle(
			astgen.Ident("formWriter"),
			astgen.Call(astgen.QualExpr("multipart", "NewWriter"), astgen.UnaryPtr(astgen.Ident("buf"))),
		),
	)
	for _, p := range op.formDataParams {
		var inner []ast.Stmt
		if p.binary {
			// Binary parameter: the model field holds the upload path. Open it,
			// create a file part named after the field using the path's base name
			// as the filename (so the request does not leak the full local path),
			// copy the file into the part, then close the file. Each step is
			// error-checked and returns on failure.
			inner = append(inner,
				astgen.Assign(
					[]ast.Expr{astgen.Ident("file"), astgen.Ident("err")},
					[]ast.Expr{astgen.Call(astgen.QualExpr("os", "Open"), paramValueExpr(modelVar, p))},
				),
				errCheckStmt(summary, "Could not open upload file: %s"),
				astgen.Assign(
					[]ast.Expr{astgen.Ident("part"), astgen.Ident("err")},
					[]ast.Expr{astgen.Call(
						astgen.Selector(astgen.Ident("formWriter"), "CreateFormFile"),
						astgen.Lit(p.name),
						astgen.Call(astgen.QualExpr("filepath", "Base"),
							astgen.Call(astgen.Selector(astgen.Ident("file"), "Name")),
						),
					)},
				),
				errCheckStmt(summary, "Could not create upload field: %s"),
				&ast.IfStmt{
					Init: astgen.Assign(
						[]ast.Expr{astgen.Ident("_"), astgen.Ident("err")},
						[]ast.Expr{astgen.Call(astgen.QualExpr("io", "Copy"), astgen.Ident("part"), astgen.Ident("file"))},
					),
					Cond: astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
					Body: astgen.Block(
						addErrorfStmt(summary, "Could not write upload file: %s", astgen.Ident("err")),
						astgen.Return(),
					),
				},
				&ast.IfStmt{
					Init: astgen.AssignSingle(
						astgen.Ident("err"),
						astgen.Call(astgen.Selector(astgen.Ident("file"), "Close")),
					),
					Cond: astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
					Body: astgen.Block(
						addErrorfStmt(summary, "Could not close upload file: %s", astgen.Ident("err")),
						astgen.Return(),
					),
				},
			)
		} else {
			// Non-binary parameter: write it as a text field.
			inner = append(inner, astgen.ExprStmt(astgen.Call(
				astgen.Selector(astgen.Ident("formWriter"), "WriteField"),
				astgen.Lit(p.name),
				paramValueExpr(modelVar, p),
			)))
		}
		// An optional formData parameter is omitted from the body entirely when
		// unset (its model field is null) rather than written as an empty field.
		stmts = append(stmts, gateParamStmts(p, modelVar, inner...)...)
	}
	// Finalize the multipart body; a failure to close the writer leaves an
	// incomplete body, so it is error-checked like the other steps.
	stmts = append(stmts, &ast.IfStmt{
		Init: astgen.AssignSingle(
			astgen.Ident("err"),
			astgen.Call(astgen.Selector(astgen.Ident("formWriter"), "Close")),
		),
		Cond: astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
		Body: astgen.Block(
			addErrorfStmt(summary, "Could not finalize request body: %s", astgen.Ident("err")),
			astgen.Return(),
		),
	})
	// The buffer address is the body reader (*bytes.Buffer implements io.Reader)
	// and carries the multipart boundary.
	body := astgen.UnaryPtr(astgen.Ident("buf"))
	return stmts, body
}

// xmlBodyStmts builds an application/xml request body from the model: the typed
// model is converted to a JSON-ready map (modelToJSONMap, reused so the same
// field-name/null-omission rules apply), then mapToXML encodes it wrapped in the
// resource's XML root element with deterministic sorted child order. The
// encoded payload is wrapped with bytes.NewReader as the request body reader
// (A2).
func xmlBodyStmts(op crudOperationPlan, summary, modelVar string) ([]ast.Stmt, ast.Expr) {
	stmts := []ast.Stmt{
		astgen.Assign(
			[]ast.Expr{astgen.Ident("body"), astgen.Ident("err")},
			[]ast.Expr{astgen.Call(astgen.Ident("modelToJSONMap"), astgen.UnaryPtr(astgen.Ident(modelVar)))},
		),
		errCheckStmt(summary, "Could not build request body: %s"),
		astgen.Assign(
			[]ast.Expr{astgen.Ident("payload"), astgen.Ident("err")},
			[]ast.Expr{astgen.Call(astgen.Ident("mapToXML"), astgen.Ident("body"), astgen.Lit(op.xmlRoot))},
		),
		errCheckStmt(summary, "Could not encode request body: %s"),
	}
	body := astgen.Call(astgen.QualExpr("bytes", "NewReader"), astgen.Ident("payload"))
	return stmts, body
}

// wiredCreateBody returns the Create body wired to the generated API client:
// it posts the planned attributes to the create endpoint and stores the API
// response as Terraform state.
func wiredCreateBody(r ir.ResourceIR, plan resourceWiringPlan, modelName string) []ast.Stmt {
	summary := fmt.Sprintf("Error creating %s", resourceTypeName(r))
	stmts := make([]ast.Stmt, 0, 24)
	stmts = append(stmts,
		astgen.VarDecl("plan", modelName, nil),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "Append"),
			astgen.Ellipsis(astgen.Call(
				astgen.Selector(astgen.Selector(astgen.Ident("req"), "Plan"), "Get"),
				astgen.Ident("ctx"),
				astgen.UnaryPtr(astgen.Ident("plan")),
			)),
		)),
		astgen.If(
			astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "HasError")),
			astgen.Return(),
		),
	)
	bodyStmts, bodyExpr := requestBodyStmts(plan.create, summary, "plan")
	stmts = append(stmts, clientGuardStmt("r"))
	stmts = append(stmts, bodyStmts...)
	stmts = append(stmts, requestPathStmts(plan.create, "plan")...)
	stmts = append(stmts, sendRequestStmts(plan.create, "r", summary, "plan", bodyExpr, nil)...)
	stmts = append(stmts, decodeAndApplyStmts(summary, "plan", plan.create.responseEnvelope)...)
	stmts = append(stmts, createIDFallbackStmts(r, "plan", summary)...)
	stmts = append(stmts, stateSetStmt("plan"))
	return stmts
}

// createIDFallbackStmts emits the identifier-resolution fallback for a wired
// Create: after the response body is applied to the model, if the identifier is
// still unset it falls back to the response's Location header (for string
// identifiers), and surfaces a clear error diagnostic when neither the body nor a
// Location header supplied one. This addresses APIs that return 201 with an
// empty body or expose the new identifier only via Location. It returns nil
// when the resource has no recognizable identifier attribute.
func createIDFallbackStmts(r ir.ResourceIR, modelVar, summary string) []ast.Stmt {
	info := resourceIDFieldInfo(r)
	if !info.found {
		return nil
	}
	idSel := astgen.Selector(astgen.Ident(modelVar), info.field)
	unset := astgen.Binary(
		astgen.Call(astgen.Selector(idSel, "IsNull")),
		token.LOR,
		astgen.Call(astgen.Selector(idSel, "IsUnknown")),
	)

	if info.primitive == ir.TypeString {
		locAssign := astgen.AssignSingle(astgen.Ident("loc"), astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("httpResp"), "Header"), "Get"),
			astgen.Lit("Location"),
		))
		setFromLoc := astgen.Block(astgen.AssignStmt(
			[]ast.Expr{idSel},
			[]ast.Expr{astgen.Call(astgen.QualExpr("types", "StringValue"), astgen.Ident("loc"))},
			token.ASSIGN,
		))
		errBlock := astgen.Block(
			addErrorStmt(summary, astgen.Lit("The create response did not contain an identifier and no Location header was returned, so the resource cannot be tracked in state.")),
			astgen.Return(),
		)
		locIf := astgen.IfElse(astgen.NotEqual(astgen.Ident("loc"), astgen.Lit("")), setFromLoc, errBlock)
		return []ast.Stmt{astgen.If(unset, locAssign, locIf)}
	}

	// A non-string identifier cannot be derived from a Location header URL, so
	// surface a clear error when the response body did not supply one.
	return []ast.Stmt{astgen.If(unset,
		addErrorStmt(summary, astgen.Lit("The create response did not contain an identifier for this resource.")),
		astgen.Return(),
	)}
}

// state when the API reports it is gone.
func wiredReadBody(r ir.ResourceIR, plan resourceWiringPlan, modelName string) []ast.Stmt {
	summary := fmt.Sprintf("Error reading %s", resourceTypeName(r))
	stmts := make([]ast.Stmt, 0, 20)
	stmts = append(stmts,
		astgen.VarDecl("state", modelName, nil),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "Append"),
			astgen.Ellipsis(astgen.Call(
				astgen.Selector(astgen.Selector(astgen.Ident("req"), "State"), "Get"),
				astgen.Ident("ctx"),
				astgen.UnaryPtr(astgen.Ident("state")),
			)),
		)),
		astgen.If(
			astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "HasError")),
			astgen.Return(),
		),
		clientGuardStmt("r"),
	)
	stmts = append(stmts, requestPathStmts(plan.read, "state")...)
	stmts = append(stmts, sendRequestStmts(plan.read, "r", summary, "state", nil, []ast.Stmt{
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "State"), "RemoveResource"),
			astgen.Ident("ctx"),
		)),
		astgen.Return(),
	})...)
	stmts = append(stmts, decodeAndApplyStmts(summary, "state", plan.read.responseEnvelope)...)
	stmts = append(stmts, stateSetStmt("state"))
	return stmts
}

// wiredUpdateBody returns the Update body wired to the generated API client:
// it sends the planned attributes to the update endpoint and stores the API
// response as Terraform state.
func wiredUpdateBody(r ir.ResourceIR, plan resourceWiringPlan, modelName string) []ast.Stmt {
	summary := fmt.Sprintf("Error updating %s", resourceTypeName(r))
	stmts := []ast.Stmt{
		astgen.VarDecl("plan", modelName, nil),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "Append"),
			astgen.Ellipsis(astgen.Call(
				astgen.Selector(astgen.Selector(astgen.Ident("req"), "Plan"), "Get"),
				astgen.Ident("ctx"),
				astgen.UnaryPtr(astgen.Ident("plan")),
			)),
		)),
		astgen.VarDecl("state", modelName, nil),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "Append"),
			astgen.Ellipsis(astgen.Call(
				astgen.Selector(astgen.Selector(astgen.Ident("req"), "State"), "Get"),
				astgen.Ident("ctx"),
				astgen.UnaryPtr(astgen.Ident("state")),
			)),
		)),
		astgen.If(
			astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "HasError")),
			astgen.Return(),
		),
	}
	if preserve := updateIDPreservation(r); preserve != nil {
		stmts = append(stmts, preserve)
	}
	// Preserve computed state values (e.g. an optimistic-concurrency version)
	// that the plan leaves unknown so the Update request body carries them (G20).
	stmts = append(stmts, astgen.ExprStmt(astgen.Call(
		astgen.Ident("preserveStateIntoPlan"),
		astgen.UnaryPtr(astgen.Ident("plan")),
		astgen.UnaryPtr(astgen.Ident("state")),
	)))
	bodyStmts, bodyExpr := requestBodyStmts(plan.updateOp, summary, "plan")
	stmts = append(stmts, clientGuardStmt("r"))
	stmts = append(stmts, bodyStmts...)
	stmts = append(stmts, requestPathStmts(plan.updateOp, "plan")...)
	stmts = append(stmts, sendRequestStmts(plan.updateOp, "r", summary, "plan", bodyExpr, nil)...)
	stmts = append(stmts, decodeAndApplyStmts(summary, "plan", plan.updateOp.responseEnvelope)...)
	stmts = append(stmts, stateSetStmt("plan"))
	return stmts
}

// wiredDeleteBody returns the Delete body wired to the generated API client:
// it deletes the remote resource, treating an HTTP 404 as already deleted.
func wiredDeleteBody(r ir.ResourceIR, plan resourceWiringPlan, modelName string) []ast.Stmt {
	summary := fmt.Sprintf("Error deleting %s", resourceTypeName(r))
	stmts := make([]ast.Stmt, 0, 16)
	stmts = append(stmts,
		astgen.VarDecl("state", modelName, nil),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "Append"),
			astgen.Ellipsis(astgen.Call(
				astgen.Selector(astgen.Selector(astgen.Ident("req"), "State"), "Get"),
				astgen.Ident("ctx"),
				astgen.UnaryPtr(astgen.Ident("state")),
			)),
		)),
		astgen.If(
			astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "HasError")),
			astgen.Return(),
		),
		clientGuardStmt("r"),
	)
	stmts = append(stmts, requestPathStmts(plan.delete, "state")...)
	stmts = append(stmts, sendRequestStmts(plan.delete, "r", summary, "state", nil, []ast.Stmt{
		astgen.Return(),
	})...)
	return stmts
}

// resourceConfigureDecl returns the Configure method implementing
// resource.ResourceWithConfigure. It type-asserts the provider-configured data
// to the generated API client and stores it on the resource.
func resourceConfigureDecl(structName string) *ast.FuncDecl {
	return astgen.MethodDecl(
		"Configure", "r", astgen.StarExpr(astgen.Ident(structName)),
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
						"Unexpected Resource Configure Type",
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

// scaffoldCreateBody returns the honest scaffold Create body used when the
// resource's CRUD mapping is not complete enough to wire to the API client:
// it decodes the plan, reports that Create is not wired, and still stores the
// plan (with a placeholder identifier when one is recognizable) so the
// generated provider compiles into a runnable scaffold.
func scaffoldCreateBody(r ir.ResourceIR, modelName string) []ast.Stmt {
	body := []ast.Stmt{
		astgen.VarDecl("plan", modelName, nil),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "Append"),
			astgen.Ellipsis(astgen.Call(
				astgen.Selector(astgen.Selector(astgen.Ident("req"), "Plan"), "Get"),
				astgen.Ident("ctx"),
				astgen.UnaryPtr(astgen.Ident("plan")),
			)),
		)),
		astgen.If(
			astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "HasError")),
			astgen.Return(),
		),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "AddError"),
			astgen.Lit("Generated provider scaffold"),
			astgen.Lit("Create is not wired to a remote API endpoint."),
		)),
	}
	if init := createIDInitialization(r); init != nil {
		body = append(body, init)
	}
	return append(body, stateSetStmt("plan"))
}

// scaffoldReadBody returns the honest scaffold Read body used when the
// resource's CRUD mapping is not complete enough to wire to the API client.
func scaffoldReadBody(modelName string) []ast.Stmt {
	return []ast.Stmt{
		astgen.VarDecl("state", modelName, nil),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "Append"),
			astgen.Ellipsis(astgen.Call(
				astgen.Selector(astgen.Selector(astgen.Ident("req"), "State"), "Get"),
				astgen.Ident("ctx"),
				astgen.UnaryPtr(astgen.Ident("state")),
			)),
		)),
		astgen.If(
			astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "HasError")),
			astgen.Return(),
		),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "AddError"),
			astgen.Lit("Generated provider scaffold"),
			astgen.Lit("Read is not wired to a remote API endpoint."),
		)),
		stateSetStmt("state"),
	}
}

// scaffoldUpdateBody returns the honest scaffold Update body used when the API
// exposes no update operation or the resource is not wired at all. It copies a
// non-null identifier from state into the plan so Terraform can keep tracking
// the resource.
func scaffoldUpdateBody(r ir.ResourceIR, modelName string) []ast.Stmt {
	body := []ast.Stmt{
		astgen.VarDecl("plan", modelName, nil),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "Append"),
			astgen.Ellipsis(astgen.Call(
				astgen.Selector(astgen.Selector(astgen.Ident("req"), "Plan"), "Get"),
				astgen.Ident("ctx"),
				astgen.UnaryPtr(astgen.Ident("plan")),
			)),
		)),
		astgen.VarDecl("state", modelName, nil),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "Append"),
			astgen.Ellipsis(astgen.Call(
				astgen.Selector(astgen.Selector(astgen.Ident("req"), "State"), "Get"),
				astgen.Ident("ctx"),
				astgen.UnaryPtr(astgen.Ident("state")),
			)),
		)),
		astgen.If(
			astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "HasError")),
			astgen.Return(),
		),
	}
	if preserve := updateIDPreservation(r); preserve != nil {
		body = append(body, preserve)
	}
	return append(body,
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "AddError"),
			astgen.Lit("Generated provider scaffold"),
			astgen.Lit("Update is not wired to a remote API endpoint."),
		)),
		stateSetStmt("plan"),
	)
}

// scaffoldDeleteBody returns the honest scaffold Delete body used when the
// resource's CRUD mapping is not complete enough to wire to the API client.
func scaffoldDeleteBody(modelName string) []ast.Stmt {
	return []ast.Stmt{
		astgen.VarDecl("state", modelName, nil),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "Append"),
			astgen.Ellipsis(astgen.Call(
				astgen.Selector(astgen.Selector(astgen.Ident("req"), "State"), "Get"),
				astgen.Ident("ctx"),
				astgen.UnaryPtr(astgen.Ident("state")),
			)),
		)),
		astgen.If(
			astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "HasError")),
			astgen.Return(),
		),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "AddError"),
			astgen.Lit("Generated provider scaffold"),
			astgen.Lit("Delete is not wired to a remote API endpoint."),
		)),
	}
}
