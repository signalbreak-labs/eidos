package generator

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
	"time"

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
	// literal, when non-empty, is a static value substituted into the path
	// instead of a model field. It is derived from a path parameter's schema
	// (const → default → first enum value) for placeholders that have no
	// matching schema attribute and no usable ID fallback — typically a shared
	// path-versioning segment such as Linode's {apiVersion} (enum
	// ["v4","v4beta"]) that is not a resource attribute. The value is spec-
	// derived and deterministic, so wiring the operation with it is strictly
	// better than leaving it an honest scaffold.
	literal string
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
	// indistinguishable from a JSON-body operation. A body-bearing method
	// (POST/PUT/PATCH) with no BodySchema and no formData parameters is bodiless:
	// the wired create/update sends no body instead of serializing the entire
	// plan model as JSON to an endpoint that expects none (M-11).
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
	// responseIsCollection is true for a read whose response (after the
	// envelope unwrap) is an array of instances from a placeholder-free
	// collection GET. The generated readRemote selects the element whose
	// identifier matches the resource's identifier attribute — and reports the
	// resource removed when no element matches — instead of blindly applying
	// the first element (G39).
	responseIsCollection bool
	// nestedCollectionPath is a dot-separated path into the read response
	// (after the envelope unwrap) that locates the collection array(s) for a
	// child resource whose read is a parent GET (e.g. a port filter rule read
	// via GET /portFilters/{portId}, with the rules at "portFilter.rules.*").
	// The last segment may be "*" to search every array value at that level.
	// When non-empty, the generated readRemote navigates the path and selects
	// the element whose identifier matches state (G39) instead of applying the
	// whole parent object.
	nestedCollectionPath string
	// responseInnerPath is the property name to navigate into AFTER the response
	// envelope is unwrapped, before applying the body to the model. It handles
	// create/update responses that nest the created/updated resource under a
	// named property alongside side-effect objects (e.g. SpaceTraders
	// purchase-ship {data:{ship:{...},transaction:{...},agent:{...}}}; after
	// unwrapping "data" the ship is still nested under "ship"). Empty when the
	// response applies directly after the envelope unwrap.
	responseInnerPath string
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
	// collection is true for a query parameter modeled as a List attribute (an
	// OpenAPI array query parameter, items: <scalar>). The request builder emits
	// one url.Values.Add per element (repeated query values, form + explode: true)
	// rather than a single Set. Only query parameters can be collections:
	// matchParamAttribute scopes the collection match to query, so header/cookie/
	// formData substitutions always have collection == false.
	collection bool
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

// AnyResourceXMLBody reports whether any resource's wired CRUD bodies serialize
// a request body as XML. It gates the XML helper section (mapToXML and friends)
// and their supporting imports in json_convert.go so JSON-only providers carry
// no dead XML serialization code (N-37).
func AnyResourceXMLBody(resources []ir.ResourceIR) bool {
	for _, r := range resources {
		plan := planResourceWiring(r)
		if plan.wired && plan.needsXMLBody {
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

	create, ok := planOperation(r, r.CRUDMapping.Create, r.PathParamOverrides["create"])
	if !ok {
		return plan
	}
	read, ok := planOperation(r, r.CRUDMapping.Read, r.PathParamOverrides["read"])
	if !ok {
		return plan
	}
	del, ok := planOperation(r, r.CRUDMapping.Delete, r.PathParamOverrides["delete"])
	if !ok {
		return plan
	}

	plan.create = create
	plan.read = read
	plan.delete = del
	plan.wired = true

	// The create ID fallback extracts the trailing path segment from the
	// Location header with strings.TrimRight/LastIndex (M-8), so a wired
	// resource with a string ID attribute needs the strings import.
	if info := resourceIDFieldInfo(r); info.found && info.primitive == ir.TypeString {
		plan.needsStrings = true
	}

	if r.CRUDMapping.Update != nil {
		if upd, ok := planOperation(r, *r.CRUDMapping.Update, r.PathParamOverrides["update"]); ok {
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
	return op.hasBody && methodHasBody(op.method) && op.bodyEncoding == bodyJSON
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
	return op.hasBody && methodHasBody(op.method) && op.bodyEncoding == bodyXML
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
			// Query/header/cookie/formData parameters render through url.Values /
			// http.Header / strconv — never strings.ReplaceAll — so they require
			// strconv (when non-string) but never strings (M-13). Setting
			// needsStrings here would import strings unused for a resource with
			// parameters but no path placeholders.
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
func planOperation(r ir.ResourceIR, op ir.OperationMappingIR, pathOverrides map[string]string) (crudOperationPlan, bool) {
	var planned crudOperationPlan
	planned.method = strings.ToUpper(strings.TrimSpace(op.Method))
	planned.template = strings.TrimSpace(op.PathTemplate)
	planned.successCodes = op.SuccessCodes
	planned.errorMappings = errorMappingDescriptions(op.ErrorMappings)
	planned.responseIsCollection = op.ResponseIsCollection
	planned.nestedCollectionPath = strings.TrimSpace(op.NestedCollectionPath)
	if planned.method == "" || planned.template == "" {
		return planned, false
	}

	placeholders := pathPlaceholders(planned.template)
	// The resource-id fallback (using the single identifier attribute for a
	// placeholder whose name does not match a schema attribute) is only valid for
	// a simple-id path with one dynamic placeholder. A composite path (multiple
	// dynamic placeholders) describes distinct path parameters; falling back to
	// the same id attribute for each would substitute the same value into every
	// slot and produce a wrong URL, so each placeholder must match a same-named
	// attribute or the operation is not wired (honest scaffold, REMAINING_GAPS
	// §3/#12). A static path segment — a placeholder resolved to a literal from
	// its parameter schema (e.g. a shared {apiVersion} with enum ["v4","v4beta"])
	// — is NOT dynamic: it does not count toward the composite decision, so a
	// path like /{apiVersion}/things/{thingId} has one dynamic placeholder and
	// the resource-id fallback remains valid for {thingId}. An enum-equivalent
	// placeholder (bound to a Required attribute by identical enum set, e.g.
	// notif_meta_config's {notifType} ↔ `type`) IS dynamic — it is filled from
	// practitioner configuration — even though a static literal could also be
	// derived from its enum, so it counts toward the composite decision and
	// keeps the WireName matching (not the id fallback) resolving its siblings.
	dynamicPlaceholders := 0
	for _, placeholder := range placeholders {
		if _, ok := staticPathValue(op.PathParams, placeholder); ok {
			if _, ok := enumEquivalentAttribute(r, op.PathParams, placeholder); ok {
				dynamicPlaceholders++
			}
			continue
		}
		dynamicPlaceholders++
	}
	multiPlaceholder := dynamicPlaceholders > 1
	for _, placeholder := range placeholders {
		sub, ok := resolvePathSubstitution(r, placeholder, multiPlaceholder, op.PathParams, pathOverrides)
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
		planned.hasBody = true
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
	} else if methodHasBody(planned.method) && op.BodySchema != nil {
		// A non-formData body-bearing method with a declared request body encodes
		// it per the media type: JSON (the default, including JSON dialects and
		// empty), XML, or unsupported (fail-loud — the transformer warned; keep
		// the operation an honest scaffold rather than silently emitting JSON).
		// A body-bearing method with NO BodySchema is bodiless: the create/update
		// sends no request body (M-11).
		planned.hasBody = true
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
	planned.responseInnerPath = op.ResponseInnerPath

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
		field, prim, coll, ok := matchParamAttribute(attrs, p)
		if !ok {
			if p.Required {
				return nil, false
			}
			continue
		}
		subs = append(subs, paramSubstitution{name: p.Name, in: p.In, field: field, primitive: prim, required: p.Required, collection: coll})
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
		field, prim, coll, ok := matchParamAttribute(attrs, p)
		// formData carries single scalar values: a collection (array query
		// parameter) cannot be form-encoded as one field, and a non-primitive
		// or Dynamic attribute has no ValueString accessor, so the operation
		// stays honestly scaffolded rather than wiring a partial form body.
		if !ok || coll || !isFormEncodablePrimitive(prim) {
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

// matchParamAttribute finds a schema attribute supplying the parameter's value.
// The match is on the normalized (PascalCase) field name so a hyphenated HTTP
// header such as "X-Trace-Id" maps to a snake_case Terraform attribute such as
// "x_trace_id", which is the only valid attribute shape. It returns the Go field
// name, the primitive type, whether the attribute is a collection (an array
// query parameter modeled as a List of a scalar element), and whether a match
// was found.
//
// A query parameter declared as an array (OpenAPI `type: array, items: <scalar>`)
// is modeled as a List attribute of the element primitive by
// transformer.paramSchemaIR (data sources and list resources). Match it as a
// collection when the element is a strict scalar (Dynamic excluded: a Dynamic
// element has no ValueString accessor and cannot be serialized into repeated
// query values). Only query parameters are array-serialized — header/cookie/
// formData carry single scalar values — so the collection match is scoped to
// query. A collection attribute matched against a non-query parameter does not
// satisfy it (a header cannot carry a list), so the match fails.
func matchParamAttribute(attrs []ir.AttributeIR, p ir.ParamIR) (string, ir.PrimitiveType, bool, bool) {
	want := naming.GoFieldName(p.Name)
	for _, attr := range attrs {
		// Match by the sanitized attribute name or its original wire name
		// (G14): a param like SAMLRequest maps to the saml_request attribute.
		if naming.GoFieldName(attr.Name) != want && (attr.WireName == "" || naming.GoFieldName(attr.WireName) != want) {
			continue
		}
		if strings.EqualFold(p.In, "query") && attr.Schema.Collection != nil {
			elem := attr.Schema.Collection.ElementType
			if isFormEncodablePrimitive(elem.Type) {
				return naming.GoFieldName(attr.Name), elem.Type, true, true
			}
			return "", "", false, false
		}
		// Dynamic is rejected even though IsPrimitiveSchema admits it: a
		// types.Dynamic model field has no ValueString/ValueBool/ValueInt64/
		// ValueFloat64 accessor, so it cannot be serialized into a query, header,
		// cookie, or form value. Treating it as unmappable makes an optional param
		// skip wiring (rather than emitting a body that will not compile) and a
		// required param disable wiring for the whole operation. This arises when
		// a scalar query parameter shares a name with a free-form object response
		// property (e.g. GitLab's "trailers": a boolean query param and an object
		// response property on the same operation) and the merged attribute
		// resolves to Dynamic.
		if !isFormEncodablePrimitive(attr.Schema.Type) {
			return "", "", false, false
		}
		return naming.GoFieldName(attr.Name), attr.Schema.Type, false, true
	}
	return "", "", false, false
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
//
// An unmatched placeholder whose path parameter declares a string enum binds
// to the unique Required string attribute carrying the exact same enum set:
// the practitioner's chosen value fills the segment. This runs before the
// static-value fallback so a multi-valued enum parameter is not silently
// pinned to its first value (gigavuecore notif_meta_config's {notifType},
// enum [instant, batch, trap], ↔ the Required `type` body attribute).
//
// Before giving up on an unresolvable placeholder, a static value derived from
// the matching path parameter's schema is attempted (const → default → first
// enum value). This wires shared path segments that are not resource attributes
// — chiefly path-versioning parameters such as Linode's {apiVersion} (enum
// ["v4","v4beta"], no default) — instead of leaving the whole resource an
// honest scaffold. The value is spec-derived and deterministic.
//
// pathOverrides is the resource's path_params override for the operation being
// planned (nil when none): a placeholder → attribute-name mapping that wins
// over every fallback. It wires paths whose placeholder does not name-match any
// attribute and whose value is not the resource id — e.g. a read keyed by a
// create-body field (gigavuecore's activation: GET .../{entlItemId} filled from
// the `eli_id` attribute). The override is validated at transform time, so a
// mapped attribute is guaranteed present in the schema.
func resolvePathSubstitution(r ir.ResourceIR, placeholder string, noIDFallback bool, pathParams []ir.ParamIR, pathOverrides map[string]string) (pathSubstitution, bool) {
	if attrName, ok := pathOverrides[placeholder]; ok {
		for _, attr := range r.Schema.Attributes {
			if attr.Name != attrName {
				continue
			}
			if !schema.IsPrimitiveSchema(attr.Schema) {
				return pathSubstitution{}, false
			}
			return pathSubstitution{placeholder: placeholder, field: naming.GoFieldName(attr.Name), primitive: attr.Schema.Type}, true
		}
	}
	for _, attr := range r.Schema.Attributes {
		// A schema attribute whose Terraform name matches the placeholder wins.
		// Also accept a match against the attribute's WireName (the original spec
		// field name): a placeholder is camelCase (e.g. {portId}) while the
		// attribute is snake_case (port_id), so a name-only match would miss it.
		// This matters for child resources, whose path parameters are folded into
		// the schema as Required attributes carrying the spec's wire name — a
		// simple-id path like /portFilters/{portId} must fill {portId} from the
		// port_id attribute, not fall back to the resource id (rule_id). The
		// WireName match is safe on simple-id paths too: when the placeholder is
		// the id attribute's own wire name, the match resolves to the same field
		// the id fallback would have chosen.
		if attr.Name != placeholder && attr.WireName != placeholder {
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
	// The placeholder did not name-match an attribute: when its path
	// parameter declares a string enum and exactly one Required string
	// attribute carries the identical enum set, bind to that attribute so the
	// practitioner's configuration supplies the URL segment. This must run
	// before staticPathValue, which would pin a multi-valued enum parameter
	// to its first value, and before the id-attribute fallback, which would
	// fill the segment with an unrelated identifier.
	if attr, ok := enumEquivalentAttribute(r, pathParams, placeholder); ok {
		return pathSubstitution{placeholder: placeholder, field: naming.GoFieldName(attr.Name), primitive: attr.Schema.Type}, true
	}
	// No matching attribute and no UID fallback: try a static value from the
	// path parameter's schema. This runs before the noIDFallback guard so a
	// shared versioning segment in a composite path (e.g. {apiVersion}) is
	// resolved rather than disabling wiring for the whole resource, and before
	// the id-attribute fallback so a versioning segment is never filled with the
	// resource id.
	if v, ok := staticPathValue(pathParams, placeholder); ok {
		return pathSubstitution{placeholder: placeholder, literal: v, primitive: ir.TypeString}, true
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

// staticPathValue derives a deterministic static value for a path placeholder
// from its declared parameter schema, in priority order: const, then default,
// then the first enum value. It returns ok=false when the parameter is absent
// or declares none of these. The value is used to wire shared path segments
// that are not resource attributes — e.g. Linode's {apiVersion} (enum
// ["v4","v4beta"]) — choosing the stable, conventionally-first enum value.
func staticPathValue(pathParams []ir.ParamIR, placeholder string) (string, bool) {
	for _, p := range pathParams {
		if p.Name != placeholder {
			continue
		}
		s := p.Schema
		if s.Const != nil {
			if v := anyStringValue(*s.Const); v != "" {
				return v, true
			}
		}
		if s.Default != nil {
			if v := anyStringValue(*s.Default); v != "" {
				return v, true
			}
		}
		if len(s.EnumValues) > 0 {
			if v := anyStringValue(s.EnumValues[0]); v != "" {
				return v, true
			}
		}
		return "", false
	}
	return "", false
}

// enumEquivalentAttribute finds the unique Required string attribute whose
// enum values are exactly the placeholder's path-parameter enum set. The
// motivating case is gigavuecore's notif_meta_config: the instance path
// /notification/event/notifMetaConfig/{notifType}/{taskId} declares
// notifType with enum [instant, batch, trap] while the request body's
// Required `type` attribute carries the same enum — the placeholder cannot
// name-match (notifType vs type) and must not be statically pinned, so the
// practitioner's `type` configuration is the only correct source for the
// segment. Ambiguity (zero or multiple matching attributes) resolves to
// ok=false so the remaining fallbacks decide.
func enumEquivalentAttribute(r ir.ResourceIR, pathParams []ir.ParamIR, placeholder string) (ir.AttributeIR, bool) {
	var paramEnum []any
	for _, p := range pathParams {
		if p.Name == placeholder {
			paramEnum = p.Schema.EnumValues
			break
		}
	}
	if len(paramEnum) == 0 {
		return ir.AttributeIR{}, false
	}
	var match ir.AttributeIR
	matches := 0
	for _, attr := range r.Schema.Attributes {
		if !attr.Required || attr.Schema.Type != ir.TypeString || !schema.IsPrimitiveSchema(attr.Schema) {
			continue
		}
		if !sameStringEnumSet(attr.Schema.EnumValues, paramEnum) {
			continue
		}
		matches++
		if matches > 1 {
			return ir.AttributeIR{}, false
		}
		match = attr
	}
	if matches != 1 {
		return ir.AttributeIR{}, false
	}
	return match, true
}

// sameStringEnumSet reports whether two enum value slices contain exactly the
// same string members, ignoring order. Non-string members in either slice
// make the sets unequal (order-insensitive equality, not subset).
func sameStringEnumSet(a, b []any) bool {
	if len(a) == 0 || len(a) != len(b) {
		return false
	}
	set := make(map[string]struct{}, len(a))
	for _, v := range a {
		s, ok := v.(string)
		if !ok {
			return false
		}
		set[s] = struct{}{}
	}
	for _, v := range b {
		s, ok := v.(string)
		if !ok {
			return false
		}
		if _, exists := set[s]; !exists {
			return false
		}
	}
	return true
}

// anyStringValue renders a schema const/default/enum value of any primitive
// type as a string suitable for path substitution. Non-string scalars are
// formatted with %v; empty strings are ignored (return "") so a default of ""
// does not produce a degenerate path segment.
func anyStringValue(v any) string {
	switch t := v.(type) {
	case string:
		return t
	default:
		if t == nil {
			return ""
		}
		return fmt.Sprintf("%v", t)
	}
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
		stmts = append(stmts, paramSetStmts(p, modelVar, astgen.Ident("query"))...)
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

// pathValueExpr returns the expression rendering a path substitution's value
// as a string. A static literal (e.g. a path-versioning segment like "v4") is
// emitted directly; otherwise the substitution's model field is rendered.
func pathValueExpr(modelVar string, sub pathSubstitution) ast.Expr {
	if sub.literal != "" {
		return astgen.Lit(sub.literal)
	}
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

// scalarValueExpr renders an expression denoting a typed framework scalar
// value as a string, formatting non-string primitives via strconv so the result
// can be substituted into a path or serialized into a query string or header.
// It is the shared core for model fields (modelFieldStringExpr) and collection
// elements (elementValueExpr): sel is the expression denoting the value — a
// model field selector, or a range element identifier after a type assertion.
func scalarValueExpr(sel ast.Expr, primitive ir.PrimitiveType) ast.Expr {
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

// modelFieldStringExpr renders a typed model field as a string, formatting
// non-string primitives via strconv so they can be substituted into a path or
// serialized into a query string or header.
func modelFieldStringExpr(modelVar, field string, primitive ir.PrimitiveType) ast.Expr {
	return scalarValueExpr(astgen.Selector(astgen.Ident(modelVar), field), primitive)
}

// elementValueExpr renders a collection element as a string for a repeated query
// value. types.List.Elements() returns []attr.Value, whose static type has no
// ValueString/ValueInt64/... accessor, so the element is type-asserted to the
// concrete scalar framework type (matching the List's ElementType) before
// formatting via scalarValueExpr. elemVar is the range variable identifier.
func elementValueExpr(elemVar string, primitive ir.PrimitiveType) ast.Expr {
	var asserted ast.Expr
	switch primitive {
	case ir.TypeInt:
		asserted = astgen.TypeAssertExpr(astgen.Ident(elemVar), astgen.QualExpr("types", "Int64"))
	case ir.TypeFloat:
		asserted = astgen.TypeAssertExpr(astgen.Ident(elemVar), astgen.QualExpr("types", "Float64"))
	case ir.TypeBool:
		asserted = astgen.TypeAssertExpr(astgen.Ident(elemVar), astgen.QualExpr("types", "Bool"))
	default:
		asserted = astgen.TypeAssertExpr(astgen.Ident(elemVar), astgen.QualExpr("types", "String"))
	}
	return scalarValueExpr(asserted, primitive)
}

// paramSetStmts emits the statement(s) that serialize one query parameter onto a
// url.Values receiver from the model variable. A scalar parameter emits a single
// `receiver.Set(name, value)`; a collection parameter (an array query parameter
// modeled as a List attribute) emits a `for _, elem := range <modelVar>.<field>.
// Elements() { receiver.Add(name, elemValue) }` so each element is sent as a
// repeated query value (OpenAPI form style, explode: true → `?name=a&name=b`).
// An optional parameter is gated on its model field being non-null via
// gateParamStmts, so an unset parameter is omitted from the request rather than
// sent as a zero-value empty string.
func paramSetStmts(p paramSubstitution, modelVar string, receiver ast.Expr) []ast.Stmt {
	if p.collection {
		fieldSel := astgen.Selector(astgen.Ident(modelVar), p.field)
		addCall := astgen.Call(
			astgen.Selector(receiver, "Add"),
			astgen.Lit(p.name),
			elementValueExpr("elem", p.primitive),
		)
		loop := astgen.RangeStmt(
			astgen.Ident("_"),
			astgen.Ident("elem"),
			token.DEFINE,
			astgen.Call(astgen.Selector(fieldSel, "Elements")),
			astgen.Block(astgen.ExprStmt(addCall)),
		)
		return gateParamStmts(p, modelVar, loop)
	}
	setCall := astgen.Call(
		astgen.Selector(receiver, "Set"),
		astgen.Lit(p.name),
		paramValueExpr(modelVar, p),
	)
	return gateParamStmts(p, modelVar, astgen.ExprStmt(setCall))
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
func decodeAndApplyStmts(summary, modelVar, envelope, innerPath string) []ast.Stmt {
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
	// A {data: ...} response envelope (E1): the backend flattened the payload
	// out of the response schema, so unwrap the decoded body by the same key
	// before applying it to the model. The type assertions are fail-safe — a
	// response that does not carry the envelope object leaves data untouched and
	// applyJSONToModel simply finds no matching fields.
	//
	// An array-valued envelope is a "get one" list wrapper (e.g. Gigamon
	// {"Policies": [{...}]}): the payload is the array's first element, so unwrap
	// that element when it is an object. The transformer unwraps a single-array
	// response wrapper to the item schema for managed resources
	// (ManagedResourceSchema), so applying the element's fields keeps the
	// schema and the response consistent (issue #35).
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
					Else: &ast.IfStmt{
						Init: astgen.Assign(
							[]ast.Expr{astgen.Ident("arr"), astgen.Ident("ok")},
							[]ast.Expr{astgen.TypeAssertExpr(astgen.Ident("v"), astgen.ArrayType(nil, astgen.Ident("any")))},
						),
						Cond: astgen.Binary(
							astgen.Ident("ok"),
							token.LAND,
							astgen.Binary(astgen.Call(astgen.Ident("len"), astgen.Ident("arr")), token.GTR, astgen.IntLit(0)),
						),
						Body: astgen.Block(
							&ast.IfStmt{
								Init: astgen.Assign(
									[]ast.Expr{astgen.Ident("m"), astgen.Ident("ok")},
									[]ast.Expr{astgen.TypeAssertExpr(astgen.IndexExpr(astgen.Ident("arr"), astgen.IntLit(0)), astgen.MapType(astgen.Ident("string"), astgen.Ident("any")))},
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
					},
				},
			),
		})
	}
	// A create/update response may nest the resource under a named property
	// alongside side-effect objects (e.g. SpaceTraders purchase-ship
	// {data:{ship:{...},transaction:{...},agent:{...}}} unwraps "data" to
	// {ship, transaction, agent}). Navigate into the inner property before
	// applying the body to the model so the resource's fields (and identifier)
	// resolve. The assertion is fail-safe: a response that does not carry the
	// nested object leaves data untouched and applyJSONToModel finds no matching
	// fields, surfacing the same clear "did not contain an identifier" error as
	// before rather than silently tracking the wrong shape.
	if innerPath != "" {
		stmts = append(stmts, &ast.IfStmt{
			Init: astgen.Assign(
				[]ast.Expr{astgen.Ident("inner"), astgen.Ident("ok")},
				[]ast.Expr{astgen.IndexExpr(astgen.Ident("data"), astgen.Lit(innerPath))},
			),
			Cond: astgen.Ident("ok"),
			Body: astgen.Block(
				&ast.IfStmt{
					Init: astgen.Assign(
						[]ast.Expr{astgen.Ident("im"), astgen.Ident("ok")},
						[]ast.Expr{astgen.TypeAssertExpr(astgen.Ident("inner"), astgen.MapType(astgen.Ident("string"), astgen.Ident("any")))},
					),
					Cond: astgen.Ident("ok"),
					Body: astgen.Block(
						astgen.AssignStmt(
							[]ast.Expr{astgen.Ident("data")},
							[]ast.Expr{astgen.Ident("im")},
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

// collectionReadSelectable reports whether the resource's identifier can be
// compared against collection elements: it must be a present scalar attribute
// (string/int/float/bool), whose formatted value can be compared with the
// decoded element's wire value.
func collectionReadSelectable(r ir.ResourceIR) bool {
	info := resourceIDFieldInfo(r)
	if !info.found {
		return false
	}
	switch info.primitive {
	case ir.TypeString, ir.TypeInt, ir.TypeFloat, ir.TypeBool:
		return true
	}
	return false
}

// decodeAndApplyCollectionReadStmts emits the decode/apply statements for a
// collection read: a placeholder-free GET whose response (after the envelope
// unwrap) is an array of every instance. Instead of blindly applying the first
// element, the body selects the element whose identifier (compared by its
// formatted value, so json.Number and string both match) equals the state's
// identifier attribute, and reports the resource removed when no element
// matches — so a remote deletion is detected instead of silently tracking
// whichever instance happens to sort first (G39).
//
// The identifier comparison uses fmt.Sprint on both sides: the decoder runs
// with UseNumber, so a numeric wire value is a json.Number whose Sprint
// matches the formatted model accessor for the same value, and a string wire
// value prints itself. A null identifier in state (a create response that
// never populated it) cannot select an element; the body warns and falls
// back to the first element, preserving the pre-selection behavior rather
// than dropping the resource from state.
func decodeAndApplyCollectionReadStmts(r ir.ResourceIR, summary, envelope string) []ast.Stmt {
	// Decode into data exactly as decodeAndApplyStmts does. The statement
	// list grows across several appended branches below; the capacity keeps
	// the decode/error-check block together with them without reallocation.
	stmts := make([]ast.Stmt, 0, 7)
	stmts = append(stmts,
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
		// if v, ok := data[<envelope>]; ok {
		//     if arr, ok := v.([]any); ok {
		//         <collectionSelectStmts: select by identifier, fall back to first>
		//     } else if m, ok := v.(map[string]any); ok { data = m }
		// }
		&ast.IfStmt{
			Init: astgen.Assign(
				[]ast.Expr{astgen.Ident("v"), astgen.Ident("ok")},
				[]ast.Expr{astgen.IndexExpr(astgen.Ident("data"), astgen.Lit(envelope))},
			),
			Cond: astgen.Ident("ok"),
			Body: astgen.Block(
				&ast.IfStmt{
					Init: astgen.Assign(
						[]ast.Expr{astgen.Ident("arr"), astgen.Ident("ok")},
						[]ast.Expr{astgen.TypeAssertExpr(astgen.Ident("v"), astgen.ArrayType(nil, astgen.Ident("any")))},
					),
					Cond: astgen.Ident("ok"),
					Body: astgen.Block(collectionSelectStmts(r, summary, "arr")...),
					Else: &ast.IfStmt{
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
				},
			),
		},
		astgen.AssignStmt(
			[]ast.Expr{astgen.Ident("err")},
			[]ast.Expr{astgen.Call(
				astgen.Ident("applyJSONToModel"),
				astgen.UnaryPtr(astgen.Ident("state")),
				astgen.Ident("data"),
			)},
			token.ASSIGN,
		),
		errCheckStmt(summary, "Could not map response to state: %s"))
	return stmts
}

// collectionSelectStmts emits the statements that select the collection element
// whose identifier matches the state's identifier attribute from the array
// variable arrVar, and assigns it to data. Shared by the direct collection read
// (the response envelope is the array) and the nested collection read (the
// array is found by navigating a path into the parent response). The identifier
// is compared by its formatted value, so json.Number and string both match; a
// null identifier in state cannot select an element, so the body warns and
// falls back to the first element, preserving the pre-selection behavior rather
// than dropping the resource from state (G39).
func collectionSelectStmts(r ir.ResourceIR, summary, arrVar string) []ast.Stmt {
	info := resourceIDFieldInfo(r)
	// want := fmt.Sprint(state.<Field>.Value<String|Int64|Float64|Bool>())
	accessor := "ValueString"
	switch info.primitive {
	case ir.TypeInt:
		accessor = "ValueInt64"
	case ir.TypeFloat:
		accessor = "ValueFloat64"
	case ir.TypeBool:
		accessor = "ValueBool"
	}
	wantExpr := astgen.AssignSingle(
		astgen.Ident("want"),
		astgen.Call(
			astgen.QualExpr("fmt", "Sprint"),
			astgen.Call(
				astgen.Selector(astgen.Selector(astgen.Ident("state"), info.field), accessor),
			),
		),
	)
	// for _, item := range <arrVar> {
	//     m, ok := item.(map[string]any)
	//     if !ok { continue }
	//     if idVal, ok := m[<wire>]; ok && fmt.Sprint(idVal) == want {
	//         match = m
	//         break
	//     }
	// }
	// if match == nil { removed = true; return }
	loop := astgen.RangeStmt(astgen.Ident("_"), astgen.Ident("item"), token.DEFINE, astgen.Ident(arrVar), astgen.Block(
		&ast.IfStmt{
			Init: astgen.Assign(
				[]ast.Expr{astgen.Ident("m"), astgen.Ident("ok")},
				[]ast.Expr{astgen.TypeAssertExpr(astgen.Ident("item"), astgen.MapType(astgen.Ident("string"), astgen.Ident("any")))},
			),
			Cond: astgen.Ident("ok"),
			Body: astgen.Block(
				&ast.IfStmt{
					Init: astgen.Assign(
						[]ast.Expr{astgen.Ident("idVal"), astgen.Ident("ok")},
						[]ast.Expr{astgen.IndexExpr(astgen.Ident("m"), astgen.Lit(info.wire))},
					),
					Cond: astgen.Binary(
						astgen.Ident("ok"),
						token.LAND,
						astgen.Binary(
							astgen.Call(
								astgen.QualExpr("fmt", "Sprint"),
								astgen.Ident("idVal"),
							),
							token.EQL,
							astgen.Ident("want"),
						),
					),
					Body: astgen.Block(
						astgen.AssignStmt(
							[]ast.Expr{astgen.Ident("match")},
							[]ast.Expr{astgen.Ident("m")},
							token.ASSIGN,
						),
						astgen.Break(),
					),
				},
			),
			Else: astgen.Block(astgen.Continue()),
		},
	))
	return []ast.Stmt{
		astgen.DeclStmt(astgen.VarDeclGen(astgen.VarSpec(
			"match",
			astgen.MapType(astgen.Ident("string"), astgen.Ident("any")),
			nil,
		))),
		astgen.IfElse(
			astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("state"), info.field), "IsNull")),
			astgen.Block(addWarningStmt(summary, astgen.Lit(fmt.Sprintf(
				"The identifier attribute %q is null in state, so the matching collection element cannot be identified. Falling back to the first element; this may track the wrong instance when the collection has more than one.",
				info.attr,
			)))),
			astgen.Block(wantExpr, loop, astgen.If(
				astgen.Equal(astgen.Ident("match"), astgen.Nil()),
				astgen.AssignStmt(
					[]ast.Expr{astgen.Ident("removed")},
					[]ast.Expr{astgen.Ident("true")},
					token.ASSIGN,
				),
				astgen.Return(),
			)),
		),
		&ast.IfStmt{
			Cond: astgen.Binary(
				astgen.Equal(astgen.Ident("match"), astgen.Nil()),
				token.LAND,
				astgen.Binary(astgen.Call(astgen.Ident("len"), astgen.Ident(arrVar)), token.GTR, astgen.IntLit(0)),
			),
			Body: astgen.Block(
				&ast.IfStmt{
					Init: astgen.Assign(
						[]ast.Expr{astgen.Ident("m"), astgen.Ident("ok")},
						[]ast.Expr{astgen.TypeAssertExpr(astgen.IndexExpr(astgen.Ident(arrVar), astgen.IntLit(0)), astgen.MapType(astgen.Ident("string"), astgen.Ident("any")))},
					),
					Cond: astgen.Ident("ok"),
					Body: astgen.Block(
						astgen.AssignStmt(
							[]ast.Expr{astgen.Ident("match")},
							[]ast.Expr{astgen.Ident("m")},
							token.ASSIGN,
						),
					),
				},
			),
		},
		astgen.If(
			astgen.NotEqual(astgen.Ident("match"), astgen.Nil()),
			astgen.AssignStmt(
				[]ast.Expr{astgen.Ident("data")},
				[]ast.Expr{astgen.Ident("match")},
				token.ASSIGN,
			),
		),
	}
}

// decodeAndApplyNestedCollectionReadStmts emits the decode/apply statements for
// a child-resource read: a parent GET whose response (after the envelope
// unwrap) nests the collection under a dot-separated path (e.g. a port filter
// rule read via GET /portFilters/{portId}, with the rules at
// "portFilter.rules.passRules"). The body navigates the path, collects the
// array(s) it names (a final "*" segment searches every array value at that
// level, sidestepping a collection split across sibling arrays), and selects
// the element whose identifier matches state — reporting the resource removed
// when no element matches — exactly as the direct collection read does (G39).
func decodeAndApplyNestedCollectionReadStmts(r ir.ResourceIR, summary, envelope, collectionPath string) []ast.Stmt {
	// Decode into data exactly as decodeAndApplyCollectionReadStmts does.
	stmts := make([]ast.Stmt, 0, 7)
	stmts = append(stmts,
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
	)
	// Unwrap the envelope: the parent object is nested under a single property
	// (e.g. {"portFilter": {...}}). A response that does not carry the envelope
	// leaves data untouched and the navigation below finds no array, so the
	// resource is reported removed rather than tracking the wrong shape.
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
	// Navigate the collection path into data, collecting the array(s) it names
	// into arr, then select the element whose identifier matches state.
	stmts = append(stmts, collectionPathStmts(collectionPath)...)
	stmts = append(stmts, collectionSelectStmts(r, summary, "arr")...)
	stmts = append(stmts,
		astgen.AssignStmt(
			[]ast.Expr{astgen.Ident("err")},
			[]ast.Expr{astgen.Call(
				astgen.Ident("applyJSONToModel"),
				astgen.UnaryPtr(astgen.Ident("state")),
				astgen.Ident("data"),
			)},
			token.ASSIGN,
		),
		errCheckStmt(summary, "Could not map response to state: %s"))
	return stmts
}

// collectionPathStmts emits the statements that navigate a read_collection_path
// into the decoded response (data) and collect the array(s) it names into arr.
// Each non-final segment must resolve to a map; the final segment is either a
// concrete array property (arr = that array) or "*" (append every array value
// at that level, sidestepping a collection split across sibling arrays). A
// segment that does not resolve leaves arr empty, so the subsequent selection
// reports the resource removed. The path is validated at override-application
// time (validCollectionPath), so a wildcard never appears mid-path here.
func collectionPathStmts(collectionPath string) []ast.Stmt {
	segs := strings.Split(collectionPath, ".")
	// Build the navigation innermost-first: the final segment's array
	// collection wraps in the preceding segments' map assertions, so a missing
	// or non-object segment short-circuits the whole descent.
	body := finalCollectionSegmentStmts(segs[len(segs)-1])
	for i := len(segs) - 2; i >= 0; i-- {
		seg := segs[i]
		// The map-assertion block prepends the descent into this segment to the
		// statements built for the segments below it. The slice is assembled
		// explicitly because Go rejects mixing a single element with a slice
		// expansion when the variadic is the first parameter of astgen.Block.
		inner := make([]ast.Stmt, 0, 1+len(body))
		inner = append(inner, astgen.AssignStmt(
			[]ast.Expr{astgen.Ident("data")},
			[]ast.Expr{astgen.Ident("m")},
			token.ASSIGN,
		))
		inner = append(inner, body...)
		body = []ast.Stmt{&ast.IfStmt{
			Init: astgen.Assign(
				[]ast.Expr{astgen.Ident("v"), astgen.Ident("ok")},
				[]ast.Expr{astgen.IndexExpr(astgen.Ident("data"), astgen.Lit(seg))},
			),
			Cond: astgen.Ident("ok"),
			Body: astgen.Block(
				&ast.IfStmt{
					Init: astgen.Assign(
						[]ast.Expr{astgen.Ident("m"), astgen.Ident("ok")},
						[]ast.Expr{astgen.TypeAssertExpr(astgen.Ident("v"), astgen.MapType(astgen.Ident("string"), astgen.Ident("any")))},
					),
					Cond: astgen.Ident("ok"),
					Body: astgen.Block(inner...),
				},
			),
		}}
	}
	stmts := make([]ast.Stmt, 0, 1+len(body))
	stmts = append(stmts, astgen.DeclStmt(astgen.VarDeclGen(astgen.VarSpec(
		"arr",
		astgen.ArrayType(nil, astgen.Ident("any")),
		nil,
	))))
	stmts = append(stmts, body...)
	return stmts
}

// finalCollectionSegmentStmts emits the innermost navigation statements for the
// final segment of a read_collection_path. A "*" segment appends every array
// value of the current map to arr; a concrete segment assigns the named array
// property to arr. Both are fail-safe: a value that is not an array is skipped
// and arr stays empty, so the subsequent selection reports the resource removed.
func finalCollectionSegmentStmts(seg string) []ast.Stmt {
	if seg == "*" {
		return []ast.Stmt{&ast.RangeStmt{
			Key:   astgen.Ident("_"),
			Value: astgen.Ident("val"),
			Tok:   token.DEFINE,
			X:     astgen.Ident("data"),
			Body: astgen.Block(
				&ast.IfStmt{
					Init: astgen.Assign(
						[]ast.Expr{astgen.Ident("a"), astgen.Ident("ok")},
						[]ast.Expr{astgen.TypeAssertExpr(astgen.Ident("val"), astgen.ArrayType(nil, astgen.Ident("any")))},
					),
					Cond: astgen.Ident("ok"),
					Body: astgen.Block(
						astgen.AssignStmt(
							[]ast.Expr{astgen.Ident("arr")},
							[]ast.Expr{astgen.Call(astgen.Ident("append"), astgen.Ident("arr"), astgen.Ellipsis(astgen.Ident("a")))},
							token.ASSIGN,
						),
					),
				},
			),
		}}
	}
	return []ast.Stmt{&ast.IfStmt{
		Init: astgen.Assign(
			[]ast.Expr{astgen.Ident("v"), astgen.Ident("ok")},
			[]ast.Expr{astgen.IndexExpr(astgen.Ident("data"), astgen.Lit(seg))},
		),
		Cond: astgen.Ident("ok"),
		Body: astgen.Block(
			&ast.IfStmt{
				Init: astgen.Assign(
					[]ast.Expr{astgen.Ident("a"), astgen.Ident("ok")},
					[]ast.Expr{astgen.TypeAssertExpr(astgen.Ident("v"), astgen.ArrayType(nil, astgen.Ident("any")))},
				),
				Cond: astgen.Ident("ok"),
				Body: astgen.Block(
					astgen.AssignStmt(
						[]ast.Expr{astgen.Ident("arr")},
						[]ast.Expr{astgen.Ident("a")},
						token.ASSIGN,
					),
				),
			},
		),
	}}
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

// identitySetStmts emits statements that populate resp.Identity from the model
// after a wired Create/Read/Update, so a managed resource that implements
// ResourceWithIdentity (paired with a list resource for terraform query) returns
// the identity data the framework requires. Without it the framework rejects the
// response with "Missing Resource Identity After Create/Read". Each identity
// attribute is sourced from the model field whose wire name matches the identity
// attribute's wire name (falling back to the attribute name), so the identity
// reflects the resource's actual identifier as decoded from the response. The
// identity is immutable, so Read/Update re-derive it from the same model field.
// Returns nil when the resource has no identity schema, so inferred resources
// without identity are unaffected.
func identitySetStmts(r ir.ResourceIR, summary, modelVar string) []ast.Stmt {
	if !resourceHasIdentity(r) {
		return nil
	}
	var stmts []ast.Stmt
	for _, idAttr := range r.IdentitySchema.Attributes {
		field := identityModelField(r, idAttr)
		if field == "" {
			// No model field carries this identity value. Fail loud at runtime
			// rather than silently returning a null identity the framework
			// rejects with an opaque "no resource identity data" error.
			stmts = append(stmts,
				addErrorStmt(summary, astgen.Lit(fmt.Sprintf(
					"No model attribute matches identity attribute %q, so the resource identity cannot be set.", idAttr.Name))),
				astgen.Return(),
			)
			return stmts
		}
		stmts = append(stmts, astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "Append"),
			astgen.Ellipsis(astgen.Call(
				astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Identity"), "SetAttribute"),
				astgen.Ident("ctx"),
				astgen.Call(astgen.QualExpr("path", "Root"), astgen.Lit(idAttr.Name)),
				astgen.Selector(astgen.Ident(modelVar), field),
			)),
		)))
	}
	return stmts
}

// identityModelField returns the Go model field name carrying the value for an
// identity attribute, matched by wire name (the JSON property name the identity
// was derived from) and falling back to the sanitized attribute name. Returns
// "" when no model attribute matches, which identitySetStmts surfaces as a
// runtime diagnostic.
func identityModelField(r ir.ResourceIR, idAttr ir.AttributeIR) string {
	match := func(want string) string {
		if want == "" {
			return ""
		}
		for _, attr := range r.Schema.Attributes {
			if schema.SkipAttrForModel(attr) {
				continue
			}
			if attr.WireName == want || attr.Name == want {
				return naming.GoFieldName(attr.Name)
			}
		}
		return ""
	}
	if field := match(idAttr.WireName); field != "" {
		return field
	}
	if field := match(idAttr.Name); field != "" {
		return field
	}
	return identityPathParamField(r, idAttr)
}

// identityPathParamField resolves the model field that fills the instance-path
// placeholder an identity attribute names. A list resource's identity
// attributes come from the instance path's templated segments (e.g.
// {portId}), but the managed resource's schema attribute for that same value
// may carry a different name (port_filter's {portId} is the resource's `port`
// ID attribute). The request-path builders already resolve every placeholder
// to the model field that fills it, so resolving the identity attribute
// through the same substitution keeps the identity in lockstep with the
// request path — the identity value is, by construction, the value the
// provider sends in the URL. Read is consulted first (identity is re-derived
// on every wired operation, and Read is always present for an identity-bearing
// resource), then Update and Delete for resources whose Read is unwired.
// Static placeholders (sub.literal, e.g. a pinned {apiVersion}) have no model
// field and resolve to "", leaving identitySetStmts' fail-loud error in place.
func identityPathParamField(r ir.ResourceIR, idAttr ir.AttributeIR) string {
	ops := []ir.OperationMappingIR{r.CRUDMapping.Read}
	if r.CRUDMapping.Update != nil {
		ops = append(ops, *r.CRUDMapping.Update)
	}
	ops = append(ops, r.CRUDMapping.Delete)
	for _, op := range ops {
		for _, candidate := range []string{idAttr.WireName, idAttr.Name} {
			if candidate == "" {
				continue
			}
			sub, ok := resolvePathSubstitution(r, candidate, false, op.PathParams, nil)
			if ok && sub.field != "" {
				return sub.field
			}
		}
	}
	return ""
}

// requestBodyStmts returns the statements that build a wired create/update
// request body and the body reader expression passed to sendRequestStmts. The
// encoding is selected by planOperation from the request body media type: a
// formData body is application/x-www-form-urlencoded (url.Values) or
// multipart/form-data (mime/multipart, when a binary param is present);
// otherwise the body is JSON (modelToJSONMap + json.Marshal) or XML (mapToXML).
// The paths are mutually exclusive: an operation declares either a JSON/XML
// request body or formData parameters, never both.
func requestBodyStmts(op crudOperationPlan, summary, modelVar string, bodyOmitKeys []string) ([]ast.Stmt, ast.Expr) {
	switch op.bodyEncoding {
	case bodyForm:
		return formBodyStmts(op, modelVar)
	case bodyMultipart:
		return multipartBodyStmts(op, summary, modelVar)
	case bodyXML:
		return xmlBodyStmts(op, summary, modelVar, bodyOmitKeys)
	default: // bodyJSON
		return jsonBodyStmts(summary, modelVar, bodyOmitKeys)
	}
}

// jsonBodyStmts builds a JSON request body from the model: modelToJSONMap
// converts the typed model to a map, json.Marshal encodes it, and bytes.NewReader
// wraps the encoded payload as the request body reader. bodyOmitKeys names JSON
// keys deleted from the map before marshaling — attributes that live in the
// model only to fill path/query parameters (e.g. a child resource's folded path
// parameters) and must not be sent to the API as body properties.
func jsonBodyStmts(summary, modelVar string, bodyOmitKeys []string) ([]ast.Stmt, ast.Expr) {
	stmts := make([]ast.Stmt, 0, len(bodyOmitKeys)+4)
	stmts = append(stmts,
		astgen.Assign(
			[]ast.Expr{astgen.Ident("body"), astgen.Ident("err")},
			[]ast.Expr{astgen.Call(astgen.Ident("modelToJSONMap"), astgen.UnaryPtr(astgen.Ident(modelVar)))},
		),
		errCheckStmt(summary, "Could not build request body: %s"),
	)
	for _, key := range bodyOmitKeys {
		stmts = append(stmts, astgen.ExprStmt(astgen.Call(astgen.Ident("delete"), astgen.Ident("body"), astgen.Lit(key))))
	}
	stmts = append(stmts,
		astgen.Assign(
			[]ast.Expr{astgen.Ident("payload"), astgen.Ident("err")},
			[]ast.Expr{astgen.Call(astgen.QualExpr("json", "Marshal"), astgen.Ident("body"))},
		),
		errCheckStmt(summary, "Could not encode request body: %s"),
	)
	body := astgen.Call(astgen.QualExpr("bytes", "NewReader"), astgen.Ident("payload"))
	return stmts, body
}

// pathParamBodyOmitKeys returns the wire names of attributes folded into the
// schema from path parameters (child resources), sorted for deterministic
// output. modelToJSONMap encodes the whole model, so a child resource's create/
// update body would otherwise leak the URL path parameters (e.g. portId,
// ruleType) as body properties the API never declared.
func pathParamBodyOmitKeys(r ir.ResourceIR) []string {
	keys := make([]string, 0, 2)
	for _, attr := range r.Schema.Attributes {
		if !attr.PathParam {
			continue
		}
		key := attr.WireName
		if key == "" {
			key = attr.Name
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
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
func xmlBodyStmts(op crudOperationPlan, summary, modelVar string, bodyOmitKeys []string) ([]ast.Stmt, ast.Expr) {
	stmts := make([]ast.Stmt, 0, len(bodyOmitKeys)+4)
	stmts = append(stmts,
		astgen.Assign(
			[]ast.Expr{astgen.Ident("body"), astgen.Ident("err")},
			[]ast.Expr{astgen.Call(astgen.Ident("modelToJSONMap"), astgen.UnaryPtr(astgen.Ident(modelVar)))},
		),
		errCheckStmt(summary, "Could not build request body: %s"),
	)
	for _, key := range bodyOmitKeys {
		stmts = append(stmts, astgen.ExprStmt(astgen.Call(astgen.Ident("delete"), astgen.Ident("body"), astgen.Lit(key))))
	}
	stmts = append(stmts,
		astgen.Assign(
			[]ast.Expr{astgen.Ident("payload"), astgen.Ident("err")},
			[]ast.Expr{astgen.Call(astgen.Ident("mapToXML"), astgen.Ident("body"), astgen.Lit(op.xmlRoot))},
		),
		errCheckStmt(summary, "Could not encode request body: %s"),
	)
	body := astgen.Call(astgen.QualExpr("bytes", "NewReader"), astgen.Ident("payload"))
	return stmts, body
}

// goDurationAST renders a time.Duration as a Go source expression for astgen
// emission, reusing goDurationExpr (client.go) as the single source of truth
// for the rendered form so the client and resource files cannot drift. The
// expressions goDurationExpr produces are always valid Go, so ParseExpr cannot
// fail; a failure would be a programming error and is surfaced loudly (and
// caught by renderEntitySafely) rather than silently emitting a wrong duration.
func goDurationAST(d time.Duration) ast.Expr {
	expr, err := parser.ParseExpr(goDurationExpr(d))
	if err != nil {
		panic(fmt.Sprintf("goDurationExpr(%v) produced unparseable Go: %v", d, err))
	}
	return expr
}

// resourceTimeoutWiringNeedsTime reports whether any wired CRUD method will
// reference the time package through a timeout default (goDurationExpr renders
// configured durations as "N * time.Minute" etc.). The timeouts schema block
// and model field are emitted for any resource with configured timeouts, but
// the time import is only needed when a wired body actually wires one (M-14).
func resourceTimeoutWiringNeedsTime(r ir.ResourceIR, wiring resourceWiringPlan) bool {
	if !wiring.wired || r.Timeouts == nil {
		return false
	}
	if r.Timeouts.Create != nil || r.Timeouts.Read != nil || r.Timeouts.Delete != nil {
		return true
	}
	return r.Timeouts.Update != nil && wiring.update
}

// resourceTimeoutWiringStmts emits the timeout wiring for one CRUD operation:
// it reads the configured timeout in seconds from the model's timeouts block
// (falling back to the generator.yaml default) and bounds the remaining HTTP
// exchange with context.WithTimeout. Emitted only for wired operations with a
// configured timeout (M-14). modelVar is the plan or state variable whose
// Timeouts pointer carries the block value; op is the model struct field
// (Create/Read/Update/Delete).
func resourceTimeoutWiringStmts(modelVar, op string, defaultTimeout time.Duration) []ast.Stmt {
	block := astgen.Selector(astgen.Ident(modelVar), "Timeouts")
	field := astgen.Selector(block, op)
	return []ast.Stmt{
		astgen.AssignSingle(astgen.Ident("timeout"), goDurationAST(defaultTimeout)),
		astgen.If(
			astgen.Binary(
				astgen.Binary(
					astgen.NotEqual(block, astgen.Nil()),
					token.LAND,
					astgen.Unary(token.NOT, astgen.Call(astgen.Selector(field, "IsNull"))),
				),
				token.LAND,
				astgen.Unary(token.NOT, astgen.Call(astgen.Selector(field, "IsUnknown"))),
			),
			astgen.AssignStmt(
				[]ast.Expr{astgen.Ident("timeout")},
				[]ast.Expr{astgen.Binary(
					astgen.Call(astgen.QualExpr("time", "Duration"), astgen.Call(astgen.Selector(field, "ValueInt64"))),
					token.MUL,
					astgen.QualExpr("time", "Second"),
				)},
				token.ASSIGN,
			),
		),
		astgen.Assign(
			[]ast.Expr{astgen.Ident("ctx"), astgen.Ident("cancel")},
			[]ast.Expr{astgen.Call(
				astgen.QualExpr("context", "WithTimeout"),
				astgen.Ident("ctx"),
				astgen.Ident("timeout"),
			)},
		),
		astgen.Defer(astgen.Call(astgen.Ident("cancel"))),
	}
}

// wiredCreateBody returns the framework Create body: it reads the plan, then
// delegates the HTTP exchange to createRemote (which writes diagnostics to the
// same resp), and finally populates the resource identity and stores state.
// The HTTP logic lives in a separate method so it is unit-testable without
// constructing a tfsdk.Plan, whose Schema is built from an internal
// fwschema type that generated code cannot instantiate.
func wiredCreateBody(r ir.ResourceIR, modelName string) []ast.Stmt {
	summary := fmt.Sprintf("Error creating %s", resourceTypeName(r))
	stmts := make([]ast.Stmt, 0, 12)
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
	// A configured create timeout bounds the HTTP exchange (M-14).
	if r.Timeouts != nil && r.Timeouts.Create != nil {
		stmts = append(stmts, resourceTimeoutWiringStmts("plan", "Create", *r.Timeouts.Create)...)
	}
	stmts = append(stmts,
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Ident("r"), "createRemote"),
			astgen.Ident("ctx"),
			astgen.UnaryPtr(astgen.Ident("plan")),
			astgen.Ident("resp"),
		)),
		astgen.If(
			astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "HasError")),
			astgen.Return(),
		),
	)
	stmts = append(stmts, identitySetStmts(r, summary, "plan")...)
	stmts = append(stmts, stateSetStmt("plan"))
	return stmts
}

// wiredCreateHelperBody returns the body of createRemote: the client guard,
// request body, request path, HTTP exchange, response decode, and identifier
// fallback. It writes diagnostics to resp.Diagnostics and mutates *plan, so the
// framework Create method can call it and then set state/identity on success.
func wiredCreateHelperBody(r ir.ResourceIR, plan resourceWiringPlan) []ast.Stmt {
	summary := fmt.Sprintf("Error creating %s", resourceTypeName(r))
	var bodyStmts []ast.Stmt
	var bodyExpr ast.Expr
	if plan.create.hasBody {
		bodyStmts, bodyExpr = requestBodyStmts(plan.create, summary, "plan", pathParamBodyOmitKeys(r))
	}
	stmts := make([]ast.Stmt, 0, 16)
	stmts = append(stmts, clientGuardStmt("r"))
	stmts = append(stmts, bodyStmts...)
	stmts = append(stmts, requestPathStmts(plan.create, "plan")...)
	stmts = append(stmts, sendRequestStmts(plan.create, "r", summary, "plan", bodyExpr, nil)...)
	stmts = append(stmts, decodeAndApplyStmts(summary, "plan", plan.create.responseEnvelope, plan.create.responseInnerPath)...)
	stmts = append(stmts, createIDFallbackStmts(r, "plan", summary)...)
	return stmts
}

// wiredCreateHelperDecl emits the createRemote method declaration wired to the
// generated API client. Emitted only for wired resources, alongside Create.
func wiredCreateHelperDecl(r ir.ResourceIR, plan resourceWiringPlan, modelName, structName string) *ast.FuncDecl {
	return astgen.MethodDecl(
		"createRemote", "r", astgen.StarExpr(astgen.Ident(structName)),
		astgen.Params(
			astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("plan", astgen.StarExpr(astgen.Ident(modelName)), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("resource", "CreateResponse")), ""),
		),
		astgen.Results(),
		astgen.Block(wiredCreateHelperBody(r, plan)...),
	)
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
		// The Location header is an absolute URL or absolute path per RFC 7231,
		// not a bare ID; extract the trailing path segment as the identifier
		// (M-8). A bare ID with no "/" is left unchanged.
		trimLoc := astgen.AssignStmt(
			[]ast.Expr{astgen.Ident("loc")},
			[]ast.Expr{astgen.Call(astgen.QualExpr("strings", "TrimRight"), astgen.Ident("loc"), astgen.Lit("/"))},
			token.ASSIGN,
		)
		lastSlash := astgen.AssignSingle(astgen.Ident("i"), astgen.Call(
			astgen.QualExpr("strings", "LastIndex"), astgen.Ident("loc"), astgen.Lit("/"),
		))
		extractID := astgen.If(
			astgen.Binary(astgen.Ident("i"), token.GEQ, astgen.IntLit(0)),
			astgen.AssignStmt(
				[]ast.Expr{astgen.Ident("loc")},
				[]ast.Expr{astgen.SliceExpr(astgen.Ident("loc"), astgen.Binary(astgen.Ident("i"), token.ADD, astgen.IntLit(1)), nil)},
				token.ASSIGN,
			),
		)
		setFromLoc := astgen.Block(trimLoc, lastSlash, extractID, astgen.AssignStmt(
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

// wiredReadBody returns the framework Read body: it reads the state, delegates
// the HTTP exchange to readRemote (which reports whether the remote resource is
// gone so the framework can drop it from state), and on success renews the
// identity and stores state.
func wiredReadBody(r ir.ResourceIR, modelName string) []ast.Stmt {
	summary := fmt.Sprintf("Error reading %s", resourceTypeName(r))
	stmts := make([]ast.Stmt, 0, 12)
	// readRemote returns removed=true when the API reports 404, so the framework
	// removes the resource from state rather than treating "gone" as an error.
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
	)
	// A configured read timeout bounds the HTTP exchange (M-14).
	if r.Timeouts != nil && r.Timeouts.Read != nil {
		stmts = append(stmts, resourceTimeoutWiringStmts("state", "Read", *r.Timeouts.Read)...)
	}
	stmts = append(stmts,
		astgen.If(
			astgen.Call(
				astgen.Selector(astgen.Ident("r"), "readRemote"),
				astgen.Ident("ctx"),
				astgen.UnaryPtr(astgen.Ident("state")),
				astgen.Ident("resp"),
			),
			astgen.Block(
				astgen.ExprStmt(astgen.Call(
					astgen.Selector(astgen.Selector(astgen.Ident("resp"), "State"), "RemoveResource"),
					astgen.Ident("ctx"),
				)),
				astgen.Return(),
			),
		),
		astgen.If(
			astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "HasError")),
			astgen.Return(),
		),
	)
	stmts = append(stmts, identitySetStmts(r, summary, "state")...)
	stmts = append(stmts, stateSetStmt("state"))
	return stmts
}

// wiredReadHelperBody returns the body of readRemote: the client guard, request
// path, and HTTP exchange. A 404 sets the named return removed=true (signaling
// the framework to drop the resource); any other non-success writes a
// diagnostic. On success the response is decoded into *state.
func wiredReadHelperBody(r ir.ResourceIR, plan resourceWiringPlan) []ast.Stmt {
	summary := fmt.Sprintf("Error reading %s", resourceTypeName(r))
	stmts := make([]ast.Stmt, 0, 12)
	stmts = append(stmts, clientGuardStmt("r"))
	stmts = append(stmts, requestPathStmts(plan.read, "state")...)
	stmts = append(stmts, sendRequestStmts(plan.read, "r", summary, "state", nil, []ast.Stmt{
		astgen.AssignStmt(
			[]ast.Expr{astgen.Ident("removed")},
			[]ast.Expr{astgen.Ident("true")},
			token.ASSIGN,
		),
		astgen.Return(),
	})...)
	// A child-resource read targets a parent GET whose response nests the
	// collection under a dot-separated path: navigate the path, collect the
	// array(s) it names, and select the element whose identifier matches state
	// (G39). A placeholder-free collection GET returns every instance and selects
	// the same way. An instance read keeps the envelope unwrap (whose array
	// branch applies the single wrapped element). Selection needs a scalar
	// identifier attribute; anything else keeps the first-element unwrap.
	switch {
	case plan.read.nestedCollectionPath != "" && collectionReadSelectable(r):
		stmts = append(stmts, decodeAndApplyNestedCollectionReadStmts(r, summary, plan.read.responseEnvelope, plan.read.nestedCollectionPath)...)
	case plan.read.responseIsCollection && plan.read.responseEnvelope != "" && collectionReadSelectable(r):
		stmts = append(stmts, decodeAndApplyCollectionReadStmts(r, summary, plan.read.responseEnvelope)...)
	default:
		stmts = append(stmts, decodeAndApplyStmts(summary, "state", plan.read.responseEnvelope, "")...)
	}
	// Naked return yields removed=false on the happy path (the 404 branch above
	// sets removed=true and returns early). Required because readRemote declares
	// a named bool result.
	stmts = append(stmts, astgen.Return())
	return stmts
}

// wiredReadHelperDecl emits the readRemote method declaration wired to the
// generated API client. Emitted only for wired resources, alongside Read.
func wiredReadHelperDecl(r ir.ResourceIR, plan resourceWiringPlan, modelName, structName string) *ast.FuncDecl {
	return astgen.MethodDecl(
		"readRemote", "r", astgen.StarExpr(astgen.Ident(structName)),
		astgen.Params(
			astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("state", astgen.StarExpr(astgen.Ident(modelName)), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("resource", "ReadResponse")), ""),
		),
		astgen.Results(astgen.Field("removed", astgen.Ident("bool"), "")),
		astgen.Block(wiredReadHelperBody(r, plan)...),
	)
}

// wiredUpdateBody returns the framework Update body: it reads the plan and
// prior state, carries over the identifier and computed values the plan leaves
// unknown, then delegates the HTTP exchange to updateRemote, and on success
// renews the identity and stores state.
func wiredUpdateBody(r ir.ResourceIR, modelName string) []ast.Stmt {
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
	// A configured update timeout bounds the HTTP exchange (M-14).
	if r.Timeouts != nil && r.Timeouts.Update != nil {
		stmts = append(stmts, resourceTimeoutWiringStmts("plan", "Update", *r.Timeouts.Update)...)
	}
	if preserve := updateIDPreservation(r); preserve != nil {
		stmts = append(stmts, preserve)
	}
	// Preserve computed state values (e.g. an optimistic-concurrency version)
	// that the plan leaves unknown so the Update request body carries them (G20),
	// then delegate the HTTP exchange to updateRemote and on success store state.
	stmts = append(stmts,
		astgen.ExprStmt(astgen.Call(
			astgen.Ident("preserveStateIntoPlan"),
			astgen.UnaryPtr(astgen.Ident("plan")),
			astgen.UnaryPtr(astgen.Ident("state")),
		)),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Ident("r"), "updateRemote"),
			astgen.Ident("ctx"),
			astgen.UnaryPtr(astgen.Ident("plan")),
			astgen.Ident("resp"),
		)),
		astgen.If(
			astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "HasError")),
			astgen.Return(),
		),
	)
	stmts = append(stmts, identitySetStmts(r, summary, "plan")...)
	stmts = append(stmts, stateSetStmt("plan"))
	return stmts
}

// wiredUpdateHelperBody returns the body of updateRemote: the client guard,
// request body, request path, HTTP exchange, and response decode. It writes
// diagnostics to resp.Diagnostics and mutates *plan, so the framework Update
// method can call it and then set state/identity on success.
func wiredUpdateHelperBody(r ir.ResourceIR, plan resourceWiringPlan) []ast.Stmt {
	summary := fmt.Sprintf("Error updating %s", resourceTypeName(r))
	var bodyStmts []ast.Stmt
	var bodyExpr ast.Expr
	if plan.updateOp.hasBody {
		bodyStmts, bodyExpr = requestBodyStmts(plan.updateOp, summary, "plan", pathParamBodyOmitKeys(r))
	}
	stmts := make([]ast.Stmt, 0, 16)
	stmts = append(stmts, clientGuardStmt("r"))
	stmts = append(stmts, bodyStmts...)
	stmts = append(stmts, requestPathStmts(plan.updateOp, "plan")...)
	stmts = append(stmts, sendRequestStmts(plan.updateOp, "r", summary, "plan", bodyExpr, nil)...)
	stmts = append(stmts, decodeAndApplyStmts(summary, "plan", plan.updateOp.responseEnvelope, plan.updateOp.responseInnerPath)...)
	return stmts
}

// wiredUpdateHelperDecl emits the updateRemote method declaration wired to the
// generated API client. Emitted only for wired resources with an update op.
func wiredUpdateHelperDecl(r ir.ResourceIR, plan resourceWiringPlan, modelName, structName string) *ast.FuncDecl {
	return astgen.MethodDecl(
		"updateRemote", "r", astgen.StarExpr(astgen.Ident(structName)),
		astgen.Params(
			astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("plan", astgen.StarExpr(astgen.Ident(modelName)), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("resource", "UpdateResponse")), ""),
		),
		astgen.Results(),
		astgen.Block(wiredUpdateHelperBody(r, plan)...),
	)
}

// wiredDeleteBody returns the framework Delete body: it reads the state, then
// delegates the HTTP exchange to deleteRemote (which treats a 404 as already
// deleted and reports other errors via diagnostics).
func wiredDeleteBody(r ir.ResourceIR, modelName string) []ast.Stmt {
	stmts := make([]ast.Stmt, 0, 8)
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
	)
	// A configured delete timeout bounds the HTTP exchange (M-14).
	if r.Timeouts != nil && r.Timeouts.Delete != nil {
		stmts = append(stmts, resourceTimeoutWiringStmts("state", "Delete", *r.Timeouts.Delete)...)
	}
	stmts = append(stmts,
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Ident("r"), "deleteRemote"),
			astgen.Ident("ctx"),
			astgen.UnaryPtr(astgen.Ident("state")),
			astgen.Ident("resp"),
		)),
	)
	return stmts
}

// wiredDeleteHelperBody returns the body of deleteRemote: the client guard,
// request path, and HTTP exchange. A 404 returns silently (the resource is
// already gone); any other non-success writes a diagnostic.
func wiredDeleteHelperBody(r ir.ResourceIR, plan resourceWiringPlan) []ast.Stmt {
	summary := fmt.Sprintf("Error deleting %s", resourceTypeName(r))
	stmts := make([]ast.Stmt, 0, 8)
	stmts = append(stmts, clientGuardStmt("r"))
	stmts = append(stmts, requestPathStmts(plan.delete, "state")...)
	stmts = append(stmts, sendRequestStmts(plan.delete, "r", summary, "state", nil, []ast.Stmt{
		astgen.Return(),
	})...)
	return stmts
}

// wiredDeleteHelperDecl emits the deleteRemote method declaration wired to the
// generated API client. Emitted only for wired resources, alongside Delete.
func wiredDeleteHelperDecl(r ir.ResourceIR, plan resourceWiringPlan, modelName, structName string) *ast.FuncDecl {
	return astgen.MethodDecl(
		"deleteRemote", "r", astgen.StarExpr(astgen.Ident(structName)),
		astgen.Params(
			astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("state", astgen.StarExpr(astgen.Ident(modelName)), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("resource", "DeleteResponse")), ""),
		),
		astgen.Results(),
		astgen.Block(wiredDeleteHelperBody(r, plan)...),
	)
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
