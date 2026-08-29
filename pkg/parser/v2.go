package parser

import (
	"fmt"
	"strings"
)

// ConvertV2 converts a Swagger / OpenAPI 2.0 raw AST into the version-agnostic
// Spec model. It follows a best-effort policy: it collects non-fatal diagnostics
// while still producing the most complete Spec possible. A returned error means
// the document is structurally invalid and cannot be converted.
//
// Note: $ref values (e.g. "#/definitions/Pet") are preserved verbatim from the
// source document. Rewriting them to OpenAPI 3.0 component paths such as
// "#/components/schemas/Pet" is the responsibility of the downstream normalizer.
func ConvertV2(root Node, opts ...ConvertOption) (*Spec, []Diagnostic, error) {
	var diags []Diagnostic
	if root == nil {
		return nil, diags, fmt.Errorf("empty document")
	}
	m, ok := root.(*MapNode)
	if !ok {
		return nil, diags, fmt.Errorf("document root must be a JSON object or YAML mapping")
	}

	cfg := defaultConvertConfig()
	for _, opt := range opts {
		opt(cfg)
	}
	budget := NewBudget(cfg.limits)

	// Note: unlike ConvertV30/ConvertV31, the v2 conversion does not enter a
	// budget frame per nested schema, so Limits.MaxDepth is not enforced here.
	// Recursion is bounded by the lexer's structural nesting cap
	// (maxNestingDepth) instead. See Limits.MaxDepth for the contract (M-29).
	if err := budget.Account(estimateNodeMemory(root)); err != nil {
		diags = append(diags, budgetExceededDiag(err, nodeLoc(root)))
		// Return a non-nil empty spec (matching ConvertV30/ConvertV31) so
		// callers that skip the error and dereference the spec do not panic
		// (C-5).
		return &Spec{SourceLocation: nodeLoc(root)}, diags, nil
	}

	spec := &Spec{
		Swagger:        swaggerVersion(findEntryValue(m, "swagger"), "swagger", &diags),
		SourceLocation: nodeLoc(root),
	}

	if infoNode := findEntryValue(m, "info"); infoNode != nil {
		info, d := parseInfo(infoNode)
		diags = append(diags, d...)
		spec.Info = info
	}

	servers, serverDiags := buildServers(m)
	diags = append(diags, serverDiags...)
	spec.Servers = servers

	globalProduces := v2StringSlice(findEntryValue(m, "produces"), "produces", &diags)
	globalConsumes := v2StringSlice(findEntryValue(m, "consumes"), "consumes", &diags)
	globalSchemes := v2StringSlice(findEntryValue(m, "schemes"), "schemes", &diags)

	if pathsNode := findEntryValue(m, "paths"); pathsNode != nil {
		paths, d := parsePaths(pathsNode, globalProduces, globalConsumes, globalSchemes)
		diags = append(diags, d...)
		spec.Paths = paths
	}

	var components *Components
	var componentsNode Node
	if defsNode := findEntryValue(m, "definitions"); defsNode != nil {
		components = &Components{SourceLocation: nodeLoc(defsNode)}
		componentsNode = defsNode
		schemas, d := parseSchemaMap(defsNode)
		components.Schemas = schemas
		diags = append(diags, d...)
	}
	if paramsNode := findEntryValue(m, "parameters"); paramsNode != nil {
		if components == nil {
			components = &Components{SourceLocation: nodeLoc(paramsNode)}
			componentsNode = paramsNode
		}
		params, d := parseParameterMap(paramsNode)
		components.Parameters = params
		diags = append(diags, d...)
	}
	if responsesNode := findEntryValue(m, "responses"); responsesNode != nil {
		if components == nil {
			components = &Components{SourceLocation: nodeLoc(responsesNode)}
			componentsNode = responsesNode
		}
		responses, d := parseResponseMap(responsesNode, globalProduces)
		components.Responses = responses
		diags = append(diags, d...)
	}
	if secDefsNode := findEntryValue(m, "securityDefinitions"); secDefsNode != nil {
		if components == nil {
			components = &Components{SourceLocation: nodeLoc(secDefsNode)}
			componentsNode = secDefsNode
		}
		schemes, d := parseSecurityDefinitions(secDefsNode)
		components.SecuritySchemes = schemes
		diags = append(diags, d...)
	}
	if components != nil {
		components.Extensions = nodeExtensions(componentsNode)
		spec.Components = components
	}

	if secNode := findEntryValue(m, "security"); secNode != nil {
		security, d := parseSecurityRequirements(secNode)
		diags = append(diags, d...)
		spec.Security = security
	}

	if tagsNode := findEntryValue(m, "tags"); tagsNode != nil {
		tags, d := parseTags(tagsNode)
		diags = append(diags, d...)
		spec.Tags = tags
	}

	if extNode := findEntryValue(m, "externalDocs"); extNode != nil {
		spec.ExternalDocs = parseExternalDocs(extNode, &diags)
	}

	circularRefs, circularDiags := DetectCircularSchemaRefs(root)
	diags = append(diags, circularDiags...)
	markCircularSchemaRefs(spec, circularRefs)

	spec.Extensions = nodeExtensions(m)
	return spec, diags, nil
}

// buildServers converts Swagger host/basePath/schemes into OpenAPI 3 servers.
func buildServers(root *MapNode) ([]Server, []Diagnostic) {
	var diags []Diagnostic
	host := v2ScalarString(findEntryValue(root, "host"), "host", &diags)
	basePath := v2ScalarString(findEntryValue(root, "basePath"), "basePath", &diags)
	schemes := v2StringSlice(findEntryValue(root, "schemes"), "schemes", &diags)

	if host == "" && basePath == "" && len(schemes) == 0 {
		return nil, diags
	}

	rootLoc := nodeLoc(root)
	loc := rootLoc
	loc.Path = "/servers"

	if host == "" && len(schemes) > 0 {
		// A Swagger 2.0 document may declare schemes without a host (the MyCloud
		// reference spec does). Instead of a hard error, degrade gracefully:
		// the generated server URL is the relative basePath and a warning
		// tells practitioners to set the provider `endpoint` override. composeServerURL
		// returns basePath when host is empty, so the fall-through below is safe.
		diags = append(diags, Diagnostic{
			Severity:       SeverityWarning,
			Summary:        "Missing host in Swagger 2.0 server configuration",
			Detail:         "schemes are declared without a host, so the generated base URL is relative (the basePath only). Set the generated provider's `endpoint` attribute to the API base URL.",
			SourceLocation: &loc,
		})
	}

	if len(schemes) == 0 {
		url := composeServerURL("", host, basePath)
		return []Server{{URL: url, SourceLocation: loc}}, diags
	}

	servers := make([]Server, 0, len(schemes))
	for _, scheme := range schemes {
		servers = append(servers, Server{
			URL:            composeServerURL(scheme, host, basePath),
			SourceLocation: loc,
		})
	}
	return servers, diags
}

// sameStringSet reports whether a and b contain the same strings regardless of
// order. Used to compare scheme lists, where ordering is not semantically
// meaningful.
func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]bool, len(a))
	for _, s := range a {
		set[s] = true
	}
	for _, s := range b {
		if !set[s] {
			return false
		}
	}
	return true
}

func composeServerURL(scheme, host, basePath string) string {
	if basePath == "" {
		basePath = "/"
	}
	if host == "" {
		return basePath
	}
	if scheme == "" {
		// Protocol-relative URL keeps the document host-agnostic.
		return "//" + host + basePath
	}
	return scheme + "://" + host + basePath
}

func parseInfo(node Node) (*Info, []Diagnostic) {
	m, ok := node.(*MapNode)
	if !ok {
		return nil, []Diagnostic{diagInvalidType("info", "object", node)}
	}
	diags := make([]Diagnostic, 0, 4)
	contact, d := parseContact(findEntryValue(m, "contact"))
	diags = append(diags, d...)
	license, d := parseLicense(findEntryValue(m, "license"))
	diags = append(diags, d...)
	info := &Info{
		Title:          v2ScalarString(findEntryValue(m, "title"), "info.title", &diags),
		Summary:        v2ScalarString(findEntryValue(m, "summary"), "info.summary", &diags),
		Description:    v2ScalarString(findEntryValue(m, "description"), "info.description", &diags),
		TermsOfService: v2ScalarString(findEntryValue(m, "termsOfService"), "info.termsOfService", &diags),
		Contact:        contact,
		License:        license,
		Version:        v2ScalarString(findEntryValue(m, "version"), "info.version", &diags),
		SourceLocation: nodeLoc(node),
	}
	info.Extensions = nodeExtensions(m)
	return info, diags
}

func parseContact(node Node) (*Contact, []Diagnostic) {
	m, ok := node.(*MapNode)
	if !ok {
		return nil, nil
	}
	var diags []Diagnostic
	c := &Contact{
		Name:           v2ScalarString(findEntryValue(m, "name"), "contact.name", &diags),
		URL:            v2ScalarString(findEntryValue(m, "url"), "contact.url", &diags),
		Email:          v2ScalarString(findEntryValue(m, "email"), "contact.email", &diags),
		SourceLocation: nodeLoc(node),
	}
	c.Extensions = nodeExtensions(m)
	return c, diags
}

func parseLicense(node Node) (*License, []Diagnostic) {
	m, ok := node.(*MapNode)
	if !ok {
		return nil, nil
	}
	var diags []Diagnostic
	l := &License{
		Name:           v2ScalarString(findEntryValue(m, "name"), "license.name", &diags),
		URL:            v2ScalarString(findEntryValue(m, "url"), "license.url", &diags),
		SourceLocation: nodeLoc(node),
	}
	l.Extensions = nodeExtensions(m)
	return l, diags
}

func parsePaths(node Node, produces, consumes, schemes []string) (map[string]*PathItem, []Diagnostic) {
	m, ok := node.(*MapNode)
	if !ok {
		return nil, []Diagnostic{diagInvalidType("paths", "object", node)}
	}

	paths := make(map[string]*PathItem, len(m.Entries))
	var diags []Diagnostic
	for _, entry := range m.Entries {
		key := keyString(entry.Key)
		if key == "" {
			continue
		}
		pathMap, ok := entry.Value.(*MapNode)
		if !ok {
			diags = append(diags, diagInvalidType("paths."+key, "object", entry.Value))
			continue
		}
		item, d := parsePathItem(pathMap, produces, consumes, schemes)
		diags = append(diags, d...)
		paths[key] = item
	}
	return paths, diags
}

func parsePathItem(node *MapNode, produces, consumes, schemes []string) (*PathItem, []Diagnostic) {
	item := &PathItem{SourceLocation: nodeLoc(node)}
	var diags []Diagnostic

	// A path item may be a Reference Object ({"$ref": "..."}) in Swagger 2.0.
	// Preserve the ref so it is not silently lost; downstream resolution is the
	// transformer's concern (N-8).
	item.Ref = v2ScalarString(findEntryValue(node, "$ref"), "path.$ref", &diags)
	item.Summary = v2ScalarString(findEntryValue(node, "summary"), "path.summary", &diags)
	item.Description = v2ScalarString(findEntryValue(node, "description"), "path.description", &diags)

	if paramsNode := findEntryValue(node, "parameters"); paramsNode != nil {
		params, d := parseParameters(paramsNode)
		diags = append(diags, d...)
		item.Parameters = params
	}

	for _, method := range []string{"get", "put", "post", "delete", "options", "head", "patch"} {
		opNode := findEntryValue(node, method)
		if opNode == nil {
			continue
		}
		op, d := parseOperation(opNode, produces, consumes, schemes)
		diags = append(diags, d...)
		switch method {
		case "get":
			item.Get = op
		case "put":
			item.Put = op
		case "post":
			item.Post = op
		case "delete":
			item.Delete = op
		case "options":
			item.Options = op
		case "head":
			item.Head = op
		case "patch":
			item.Patch = op
		}
	}

	item.Extensions = nodeExtensions(node)
	return item, diags
}

func parseOperation(node Node, produces, consumes, schemes []string) (*Operation, []Diagnostic) {
	m, ok := node.(*MapNode)
	if !ok {
		return nil, []Diagnostic{diagInvalidType("operation", "object", node)}
	}

	var diags []Diagnostic
	op := &Operation{
		Tags:           v2StringSlice(findEntryValue(m, "tags"), "operation.tags", &diags),
		Summary:        v2ScalarString(findEntryValue(m, "summary"), "operation.summary", &diags),
		Description:    v2ScalarString(findEntryValue(m, "description"), "operation.description", &diags),
		OperationID:    v2ScalarString(findEntryValue(m, "operationId"), "operation.operationId", &diags),
		Deprecated:     v2ScalarBool(findEntryValue(m, "deprecated"), "operation.deprecated", &diags),
		ExternalDocs:   parseExternalDocs(findEntryValue(m, "externalDocs"), &diags),
		SourceLocation: nodeLoc(node),
	}

	// Only fall back to global produces/consumes when the operation does not
	// declare its own key. An explicitly empty array (e.g. `produces: []`)
	// means "no content types" in Swagger 2.0 and must not inherit globals.
	opProducesNode := findEntryValue(m, "produces")
	opProduces := v2StringSlice(opProducesNode, "operation.produces", &diags)
	if opProducesNode == nil && len(opProduces) == 0 {
		opProduces = produces
	}
	opConsumesNode := findEntryValue(m, "consumes")
	opConsumes := v2StringSlice(opConsumesNode, "operation.consumes", &diags)
	if opConsumesNode == nil && len(opConsumes) == 0 {
		opConsumes = consumes
	}

	// Swagger 2.0 lets an operation override the document-level transport
	// schemes. The pipeline does not honor per-operation scheme overrides (the
	// generated client uses the document-level server URL), so a differing
	// override is surfaced as a warning rather than silently dropped (N-9).
	opSchemesNode := findEntryValue(m, "schemes")
	opSchemes := v2StringSlice(opSchemesNode, "operation.schemes", &diags)
	if opSchemesNode != nil && !sameStringSet(opSchemes, schemes) {
		loc := nodeLoc(opSchemesNode)
		diags = append(diags, Diagnostic{
			Severity:       SeverityWarning,
			Summary:        "Operation-level schemes not honored",
			Detail:         fmt.Sprintf("Operation %q declares schemes %v, which differ from the document-level schemes %v. The generated client uses the document-level server URL; per-operation scheme overrides are not supported.", op.OperationID, opSchemes, schemes),
			SourceLocation: &loc,
		})
	}
	op.Schemes = opSchemes

	if paramsNode := findEntryValue(m, "parameters"); paramsNode != nil {
		params, d := parseParameters(paramsNode)
		diags = append(diags, d...)
		// Body/formData parameters are normalized into a requestBody; the rest
		// remain as regular parameters.
		var bodyOrFormData []Parameter
		for i := range params {
			p := &params[i]
			if p.In == "body" || p.In == "formData" {
				bodyOrFormData = append(bodyOrFormData, *p)
			} else {
				op.Parameters = append(op.Parameters, *p)
			}
		}
		if len(bodyOrFormData) > 0 {
			rb, rbDiags := buildRequestBody(bodyOrFormData, opConsumes)
			diags = append(diags, rbDiags...)
			op.RequestBody = rb
		}
	}

	if responsesNode := findEntryValue(m, "responses"); responsesNode != nil {
		responses, d := parseResponses(responsesNode, opProduces)
		diags = append(diags, d...)
		op.Responses = responses
	}

	if secNode := findEntryValue(m, "security"); secNode != nil {
		security, d := parseSecurityRequirements(secNode)
		diags = append(diags, d...)
		op.Security = security
	}

	op.Extensions = nodeExtensions(m)
	return op, diags
}

func buildRequestBody(params []Parameter, consumes []string) (*RequestBody, []Diagnostic) {
	var bodyParams, formDataParams []Parameter
	for _, p := range params {
		switch p.In {
		case "body":
			bodyParams = append(bodyParams, p)
		case "formData":
			formDataParams = append(formDataParams, p)
		}
	}

	if len(bodyParams) == 0 && len(formDataParams) == 0 {
		return nil, nil
	}

	var diags []Diagnostic
	content := make(map[string]*MediaType)
	rb := &RequestBody{SourceLocation: params[0].SourceLocation}

	// Swagger 2.0 forbids mixing body and formData in the same operation. We
	// emit a diagnostic for the invalid combination but still populate both
	// content types as a best-effort conversion. The requestBody description and
	// required flag come from the first body parameter, while the final
	// sourceLocation is taken from the first formData parameter.
	if len(bodyParams) > 0 && len(formDataParams) > 0 {
		diags = append(diags, Diagnostic{
			Severity:       SeverityError,
			Summary:        "Invalid parameter combination",
			Detail:         "Swagger 2.0 operations cannot mix body and formData parameters",
			SourceLocation: &params[0].SourceLocation,
		})
	}

	if len(bodyParams) > 0 {
		bodyContent, desc, req, loc := buildBodyContent(bodyParams, consumes)
		for k, v := range bodyContent {
			content[k] = v
		}
		rb.Description = desc
		rb.Required = req
		rb.SourceLocation = loc
	}

	if len(formDataParams) > 0 {
		formContent, loc := buildFormDataContent(formDataParams, consumes)
		for k, v := range formContent {
			content[k] = v
		}
		rb.SourceLocation = loc
	}

	rb.Content = content
	return rb, diags
}

func buildBodyContent(bodyParams []Parameter, consumes []string) (map[string]*MediaType, string, bool, SourceLocation) {
	content := make(map[string]*MediaType)
	if len(consumes) == 0 {
		consumes = []string{"application/json"}
	}
	for _, ct := range consumes {
		content[ct] = &MediaType{}
	}
	var description string
	var required bool
	// Preserve the body parameter's location even when it has no schema so the
	// resulting RequestBody points at real source rather than a zero value.
	loc := bodyParams[0].SourceLocation
	// Swagger 2.0 allows only one body parameter; if present, attach its schema.
	for _, p := range bodyParams {
		if p.Schema == nil {
			continue
		}
		for _, mt := range content {
			mt.Schema = p.Schema
		}
		description = p.Description
		required = p.Required
		loc = p.SourceLocation
		break
	}
	return content, description, required, loc
}

func buildFormDataContent(formDataParams []Parameter, consumes []string) (map[string]*MediaType, SourceLocation) {
	formSchema := &Schema{
		Type:           "object",
		Properties:     make(map[string]*Schema),
		Required:       []string{},
		SourceLocation: formDataParams[0].SourceLocation,
	}
	hasFile := false
	hasExplicitMultipart := false
	for _, ct := range consumes {
		if ct == "multipart/form-data" {
			hasExplicitMultipart = true
			break
		}
	}
	for _, p := range formDataParams {
		schema := p.Schema
		if schema == nil {
			continue
		}
		if schema.Type == "string" && schema.Format == "binary" {
			hasFile = true
		}
		formSchema.Properties[p.Name] = schema
		if p.Required {
			formSchema.Required = append(formSchema.Required, p.Name)
		}
	}
	if len(formSchema.Required) == 0 {
		formSchema.Required = nil
	}

	mediaType := "application/x-www-form-urlencoded"
	if hasFile || hasExplicitMultipart {
		mediaType = "multipart/form-data"
	}

	mt := &MediaType{Schema: formSchema}
	if mediaType == "multipart/form-data" {
		mt.Encoding = buildMultipartEncoding(formSchema, formDataParams)
	}
	return map[string]*MediaType{mediaType: mt}, formDataParams[0].SourceLocation
}

func buildMultipartEncoding(formSchema *Schema, formDataParams []Parameter) map[string]*Encoding {
	encoding := make(map[string]*Encoding)
	for name, schema := range formSchema.Properties {
		enc := &Encoding{SourceLocation: schema.SourceLocation}
		if schema.Format == "binary" {
			enc.ContentType = "application/octet-stream"
		}
		// Propagate any collectionFormat-derived style/explode from the
		// original formData parameter into the encoding.
		for _, p := range formDataParams {
			if p.Name == name {
				enc.Style = p.Style
				enc.Explode = p.Explode
				break
			}
		}
		// Include the encoding if any of ContentType, Style, or Explode is
		// set. Explode is normally accompanied by Style from
		// applyCollectionFormat, but check it explicitly to avoid silently
		// dropping an encoding that only carries Explode.
		if enc.ContentType != "" || enc.Style != "" || enc.Explode {
			encoding[name] = enc
		}
	}
	return encoding
}

func parseResponses(node Node, produces []string) (map[string]*Response, []Diagnostic) {
	m, ok := node.(*MapNode)
	if !ok {
		return nil, []Diagnostic{diagInvalidType("responses", "object", node)}
	}
	responses := make(map[string]*Response, len(m.Entries))
	var diags []Diagnostic
	for _, entry := range m.Entries {
		key := keyString(entry.Key)
		if key == "" {
			continue
		}
		resp, d := parseResponse(entry.Value, produces)
		diags = append(diags, d...)
		responses[key] = resp
	}
	return responses, diags
}

func parseResponse(node Node, produces []string) (*Response, []Diagnostic) {
	if ref := stringValue(node); ref != "" && strings.HasPrefix(ref, "#") {
		return &Response{Ref: ref, SourceLocation: nodeLoc(node)}, nil
	}
	m, ok := node.(*MapNode)
	if !ok {
		return nil, []Diagnostic{diagInvalidType("response", "object", node)}
	}

	var diags []Diagnostic
	resp := &Response{
		Ref:            v2ScalarString(findEntryValue(m, "$ref"), "response.$ref", &diags),
		Description:    v2ScalarString(findEntryValue(m, "description"), "response.description", &diags),
		SourceLocation: nodeLoc(node),
	}

	resp.Extensions = nodeExtensions(m)

	if headersNode := findEntryValue(m, "headers"); headersNode != nil {
		headers, d := parseHeaderMap(headersNode)
		resp.Headers = headers
		diags = append(diags, d...)
	}

	if schemaNode := findEntryValue(m, "schema"); schemaNode != nil {
		schema, d := parseSchema(schemaNode)
		diags = append(diags, d...)
		if len(produces) == 0 {
			produces = []string{"application/json"}
		}
		resp.Content = make(map[string]*MediaType, len(produces))
		for _, ct := range produces {
			resp.Content[ct] = &MediaType{Schema: schema}
		}
	}

	// Swagger 2.0 response-level examples are a per-mimetype map of raw
	// example values (not Example objects). Attach each to the matching
	// MediaType.Example, creating an entry when the schema/produces pair did
	// not already populate one.
	if examplesNode := findEntryValue(m, "examples"); examplesNode != nil {
		exMap, ok := examplesNode.(*MapNode)
		if !ok {
			diags = append(diags, diagInvalidType("examples", "object", examplesNode))
		} else {
			if resp.Content == nil {
				resp.Content = make(map[string]*MediaType)
			}
			forEachEntry(exMap, func(mimetype string, value Node) {
				mt := resp.Content[mimetype]
				if mt == nil {
					mt = &MediaType{SourceLocation: nodeLoc(value)}
					resp.Content[mimetype] = mt
				}
				if mt.Example == nil {
					mt.Example = nodeToNative(value)
				}
			})
		}
	}
	return resp, diags
}

func parseParameters(node Node) ([]Parameter, []Diagnostic) {
	seq, ok := node.(*SequenceNode)
	if !ok {
		return nil, []Diagnostic{diagInvalidType("parameters", "array", node)}
	}
	params := make([]Parameter, 0, len(seq.Items))
	var diags []Diagnostic
	for _, item := range seq.Items {
		p, d := parseParameter(item)
		diags = append(diags, d...)
		params = append(params, p)
	}
	return params, diags
}

func parseParameter(node Node) (Parameter, []Diagnostic) {
	if ref := stringValue(node); ref != "" && strings.HasPrefix(ref, "#") {
		return Parameter{Ref: ref, SourceLocation: nodeLoc(node)}, nil
	}
	m, ok := node.(*MapNode)
	if !ok {
		return Parameter{}, []Diagnostic{diagInvalidType("parameter", "object", node)}
	}

	var diags []Diagnostic
	param := Parameter{
		Ref:             v2ScalarString(findEntryValue(m, "$ref"), "parameter.$ref", &diags),
		Name:            v2ScalarString(findEntryValue(m, "name"), "parameter.name", &diags),
		In:              v2ScalarString(findEntryValue(m, "in"), "parameter.in", &diags),
		Description:     v2ScalarString(findEntryValue(m, "description"), "parameter.description", &diags),
		Required:        v2ScalarBool(findEntryValue(m, "required"), "parameter.required", &diags),
		Deprecated:      v2ScalarBool(findEntryValue(m, "deprecated"), "parameter.deprecated", &diags),
		AllowEmptyValue: v2ScalarBool(findEntryValue(m, "allowEmptyValue"), "parameter.allowEmptyValue", &diags),
		SourceLocation:  nodeLoc(node),
	}

	if schemaNode := findEntryValue(m, "schema"); schemaNode != nil {
		schema, d := parseSchema(schemaNode)
		diags = append(diags, d...)
		param.Schema = schema
	} else {
		schema, d := parameterSchemaFromType(m)
		diags = append(diags, d...)
		param.Schema = schema
	}

	param.Extensions = nodeExtensions(m)
	// collectionFormat controls how array parameters serialize; translate it into
	// the equivalent OpenAPI 3.0 style/explode values.
	applyCollectionFormat(&param, m, &diags)
	return param, diags
}

// applyCollectionFormat maps Swagger 2.0 collectionFormat values to OpenAPI 3.0
// Parameter.Style and Parameter.Explode. Values without a direct equivalent are
// preserved as an x-collectionFormat extension.
func applyCollectionFormat(param *Parameter, m *MapNode, diags *[]Diagnostic) {
	cf := v2ScalarString(findEntryValue(m, "collectionFormat"), "parameter.collectionFormat", diags)
	if cf == "" {
		return
	}
	switch cf {
	case "csv":
		// For path/header parameters csv maps to the simple style; for query/cookie
		// and legacy formData it maps to the form style.
		if param.In == "path" || param.In == "header" {
			param.Style = "simple"
		} else {
			param.Style = "form"
		}
		param.Explode = false
	case "ssv":
		param.Style = "space"
		param.Explode = false
	case "tsv":
		// OpenAPI 3.0 has no direct tab-separated style.
		setExtensionIfAbsent(param, "x-collectionFormat", cf)
	case "pipes":
		param.Style = "pipe"
		param.Explode = false
	case "multi":
		param.Style = "form"
		param.Explode = true
	default:
		setExtensionIfAbsent(param, "x-collectionFormat", cf)
	}
}

func setExtensionIfAbsent(param *Parameter, key string, value any) {
	if param.Extensions == nil {
		param.Extensions = make(map[string]any)
	}
	if _, ok := param.Extensions[key]; !ok {
		param.Extensions[key] = value
	}
}

// parameterSchemaFromType builds a Schema for non-body Swagger 2.0 parameters
// that use the legacy type/format/items fields.
func parameterSchemaFromType(m *MapNode) (*Schema, []Diagnostic) {
	var diags []Diagnostic
	schema := &Schema{
		Type:           v2ScalarString(findEntryValue(m, "type"), "parameter.type", &diags),
		Format:         v2ScalarString(findEntryValue(m, "format"), "parameter.format", &diags),
		SourceLocation: nodeLoc(m),
	}
	if itemsNode := findEntryValue(m, "items"); itemsNode != nil {
		items, d := parseSchema(itemsNode)
		schema.Items = items
		diags = append(diags, d...)
	}
	if enumNode := findEntryValue(m, "enum"); enumNode != nil {
		schema.Enum = v2AnySlice(enumNode, "parameter.enum", &diags)
	}
	if def := findEntryValue(m, "default"); def != nil {
		schema.Default = nodeToNative(def)
	}
	// Swagger 2.0 "type: file" (allowed only for formData parameters) maps to
	// OpenAPI 3.0's string/binary schema.
	if schema.Type == "file" {
		schema.Type = "string"
		schema.Format = "binary"
	}
	return schema, diags
}

func parseSchemaMap(node Node) (map[string]*Schema, []Diagnostic) {
	m, ok := node.(*MapNode)
	if !ok {
		return nil, []Diagnostic{diagInvalidType("definitions", "object", node)}
	}
	schemas := make(map[string]*Schema, len(m.Entries))
	var diags []Diagnostic
	for _, entry := range m.Entries {
		key := keyString(entry.Key)
		if key == "" {
			continue
		}
		schema, d := parseSchema(entry.Value)
		diags = append(diags, d...)
		schemas[key] = schema
	}
	return schemas, diags
}

func parseSchema(node Node) (*Schema, []Diagnostic) {
	if ref := stringValue(node); ref != "" && strings.HasPrefix(ref, "#") {
		return &Schema{Ref: ref, SourceLocation: nodeLoc(node)}, nil
	}
	m, ok := node.(*MapNode)
	if !ok {
		return nil, []Diagnostic{diagInvalidType("schema", "object", node)}
	}

	diags := make([]Diagnostic, 0, 8)
	schema := &Schema{
		Ref:              v2ScalarString(findEntryValue(m, "$ref"), "schema.$ref", &diags),
		Type:             v2ScalarString(findEntryValue(m, "type"), "schema.type", &diags),
		Format:           v2ScalarString(findEntryValue(m, "format"), "schema.format", &diags),
		Title:            v2ScalarString(findEntryValue(m, "title"), "schema.title", &diags),
		Description:      v2ScalarString(findEntryValue(m, "description"), "schema.description", &diags),
		Default:          nodeToNative(findEntryValue(m, "default")),
		MultipleOf:       v2ScalarFloatPtr(findEntryValue(m, "multipleOf"), "schema.multipleOf", &diags),
		Maximum:          v2ScalarFloatPtr(findEntryValue(m, "maximum"), "schema.maximum", &diags),
		Minimum:          v2ScalarFloatPtr(findEntryValue(m, "minimum"), "schema.minimum", &diags),
		MaxLength:        v2ScalarIntPtr(findEntryValue(m, "maxLength"), "schema.maxLength", &diags),
		MinLength:        v2ScalarIntPtr(findEntryValue(m, "minLength"), "schema.minLength", &diags),
		Pattern:          v2ScalarString(findEntryValue(m, "pattern"), "schema.pattern", &diags),
		MaxItems:         v2ScalarIntPtr(findEntryValue(m, "maxItems"), "schema.maxItems", &diags),
		MinItems:         v2ScalarIntPtr(findEntryValue(m, "minItems"), "schema.minItems", &diags),
		UniqueItems:      v2ScalarBool(findEntryValue(m, "uniqueItems"), "schema.uniqueItems", &diags),
		MaxProperties:    v2ScalarInt(findEntryValue(m, "maxProperties"), "schema.maxProperties", &diags),
		MinProperties:    v2ScalarInt(findEntryValue(m, "minProperties"), "schema.minProperties", &diags),
		Required:         v2StringSlice(findEntryValue(m, "required"), "schema.required", &diags),
		Enum:             v2AnySlice(findEntryValue(m, "enum"), "schema.enum", &diags),
		Nullable:         v2ScalarBool(findEntryValue(m, "x-nullable"), "schema.x-nullable", &diags),
		ReadOnly:         v2ScalarBool(findEntryValue(m, "readOnly"), "schema.readOnly", &diags),
		WriteOnly:        v2ScalarBool(findEntryValue(m, "writeOnly"), "schema.writeOnly", &diags),
		Deprecated:       v2ScalarBool(findEntryValue(m, "deprecated"), "schema.deprecated", &diags),
		Example:          nodeToNative(findEntryValue(m, "example")),
		ContentMediaType: v2ScalarString(findEntryValue(m, "contentMediaType"), "schema.contentMediaType", &diags),
		ContentEncoding:  v2ScalarString(findEntryValue(m, "contentEncoding"), "schema.contentEncoding", &diags),
		SourceLocation:   nodeLoc(node),
	}

	if exclusive := findEntryValue(m, "exclusiveMaximum"); exclusive != nil {
		schema.ExclusiveMaximum = nodeToNative(exclusive)
	}
	if exclusive := findEntryValue(m, "exclusiveMinimum"); exclusive != nil {
		schema.ExclusiveMinimum = nodeToNative(exclusive)
	}

	diags = append(diags, parseSchemaItems(m, schema)...)
	diags = append(diags, parseSchemaComposite(m, schema)...)
	diags = append(diags, parseSchemaObject(m, schema)...)
	diags = append(diags, parseSchemaMetadata(m, schema)...)

	schema.Extensions = nodeExtensions(m)
	return schema, diags
}

func parseSchemaItems(m *MapNode, schema *Schema) []Diagnostic {
	var diags []Diagnostic
	if itemsNode := findEntryValue(m, "items"); itemsNode != nil {
		items, d := parseSchema(itemsNode)
		schema.Items = items
		diags = append(diags, d...)
	}
	return diags
}

func parseSchemaSubList(items []Node) ([]*Schema, []Diagnostic) {
	subs := make([]*Schema, 0, len(items))
	diags := make([]Diagnostic, 0, len(items))
	for _, item := range items {
		sub, d := parseSchema(item)
		diags = append(diags, d...)
		subs = append(subs, sub)
	}
	return subs, diags
}

func parseSchemaComposite(m *MapNode, schema *Schema) []Diagnostic {
	var diags []Diagnostic
	forEachSeq := func(node Node, target *[]*Schema) {
		if seq, ok := node.(*SequenceNode); ok {
			subs, d := parseSchemaSubList(seq.Items)
			*target = append(*target, subs...)
			diags = append(diags, d...)
		}
	}
	if allOfNode := findEntryValue(m, "allOf"); allOfNode != nil {
		forEachSeq(allOfNode, &schema.AllOf)
	}
	if oneOfNode := findEntryValue(m, "oneOf"); oneOfNode != nil {
		forEachSeq(oneOfNode, &schema.OneOf)
	}
	if anyOfNode := findEntryValue(m, "anyOf"); anyOfNode != nil {
		forEachSeq(anyOfNode, &schema.AnyOf)
	}
	if prefixItemsNode := findEntryValue(m, "prefixItems"); prefixItemsNode != nil {
		forEachSeq(prefixItemsNode, &schema.PrefixItems)
	}
	parseSchemaSingle := func(node Node, target **Schema) {
		sub, d := parseSchema(node)
		*target = sub
		diags = append(diags, d...)
	}
	if notNode := findEntryValue(m, "not"); notNode != nil {
		parseSchemaSingle(notNode, &schema.Not)
	}
	if containsNode := findEntryValue(m, "contains"); containsNode != nil {
		parseSchemaSingle(containsNode, &schema.Contains)
	}
	if propNamesNode := findEntryValue(m, "propertyNames"); propNamesNode != nil {
		parseSchemaSingle(propNamesNode, &schema.PropertyNames)
	}
	if unevaluatedNode := findEntryValue(m, "unevaluatedProperties"); unevaluatedNode != nil {
		if ref := stringValue(unevaluatedNode); ref != "" && strings.HasPrefix(ref, "#") {
			schema.UnevaluatedProperties = &Schema{Ref: ref, SourceLocation: nodeLoc(unevaluatedNode)}
		} else if _, ok := unevaluatedNode.(*MapNode); ok {
			sub, d := parseSchema(unevaluatedNode)
			schema.UnevaluatedProperties = sub
			diags = append(diags, d...)
		}
	}
	return diags
}

func parseSchemaObject(m *MapNode, schema *Schema) []Diagnostic {
	var diags []Diagnostic
	if propNode := findEntryValue(m, "properties"); propNode != nil {
		props, d := parseSchemaMap(propNode)
		schema.Properties = props
		diags = append(diags, d...)
	}
	if patternPropsNode := findEntryValue(m, "patternProperties"); patternPropsNode != nil {
		props, d := parseSchemaMap(patternPropsNode)
		schema.PatternProperties = props
		diags = append(diags, d...)
	}
	if addPropsNode := findEntryValue(m, "additionalProperties"); addPropsNode != nil {
		if ref := stringValue(addPropsNode); ref != "" && strings.HasPrefix(ref, "#") {
			schema.AdditionalProperties = ref
		} else if scalar, ok := addPropsNode.(*ScalarNode); ok {
			// additionalProperties is legitimately a boolean or a schema object.
			// A boolean is stored directly; any other scalar is wrong-typed, so
			// warn (still preserving the raw value) rather than silently accept it.
			if b, isBool := nodeBool(addPropsNode); isBool {
				schema.AdditionalProperties = b
			} else {
				schema.AdditionalProperties = scalar.Value
				diags = append(diags, v2ScalarTypeMismatchDiag(addPropsNode, "schema.additionalProperties", "boolean or schema"))
			}
		} else {
			sub, d := parseSchema(addPropsNode)
			schema.AdditionalProperties = sub
			diags = append(diags, d...)
		}
	}
	return diags
}

func parseSchemaMetadata(m *MapNode, schema *Schema) []Diagnostic {
	var diags []Diagnostic
	if discNode := findEntryValue(m, "discriminator"); discNode != nil {
		disc, d := parseDiscriminator(discNode)
		schema.Discriminator = disc
		diags = append(diags, d...)
	}
	if xmlNode := findEntryValue(m, "xml"); xmlNode != nil {
		xml, d := parseXML(xmlNode)
		diags = append(diags, d...)
		schema.XML = xml
	}
	if extNode := findEntryValue(m, "externalDocs"); extNode != nil {
		schema.ExternalDocs = parseExternalDocs(extNode, &diags)
	}
	if constNode := findEntryValue(m, "const"); constNode != nil {
		schema.Const = nodeToNative(constNode)
	}
	if examplesNode := findEntryValue(m, "examples"); examplesNode != nil {
		examples, d := parseExampleMap(examplesNode)
		schema.Examples = examples
		diags = append(diags, d...)
	}
	return diags
}

func parseDiscriminator(node Node) (*Discriminator, []Diagnostic) {
	// Swagger 2.0 defines discriminator as a simple string (the property name);
	// OpenAPI 3.0+ uses an object. We support both.
	if s := stringValue(node); s != "" {
		return &Discriminator{PropertyName: s, SourceLocation: nodeLoc(node)}, nil
	}
	if scalar, ok := node.(*ScalarNode); ok {
		if v, isString := scalar.Value.(string); isString && v == "" {
			loc := nodeLoc(node)
			if loc.Path == "" {
				loc.Path = "/discriminator"
			}
			return nil, []Diagnostic{{
				Severity:       SeverityError,
				Summary:        "Invalid discriminator",
				Detail:         "discriminator propertyName must not be empty",
				SourceLocation: &loc,
			}}
		}
	}
	m, ok := node.(*MapNode)
	if !ok {
		loc := nodeLoc(node)
		if loc.Path == "" {
			loc.Path = "/discriminator"
		}
		return nil, []Diagnostic{{
			Severity:       SeverityWarning,
			Summary:        "Invalid discriminator",
			Detail:         fmt.Sprintf("discriminator must be a string or object, got %T", node),
			SourceLocation: &loc,
		}}
	}
	var diags []Diagnostic
	propertyName := v2ScalarString(findEntryValue(m, "propertyName"), "discriminator.propertyName", &diags)
	if propertyName == "" {
		loc := nodeLoc(node)
		if loc.Path == "" {
			loc.Path = "/discriminator"
		}
		return nil, append(diags, Diagnostic{
			Severity:       SeverityError,
			Summary:        "Invalid discriminator",
			Detail:         "discriminator propertyName must not be empty",
			SourceLocation: &loc,
		})
	}
	disc := &Discriminator{PropertyName: propertyName, SourceLocation: nodeLoc(node)}
	if mappingNode := findEntryValue(m, "mapping"); mappingNode != nil {
		if mm, ok := mappingNode.(*MapNode); ok {
			disc.Mapping = make(map[string]string, len(mm.Entries))
			for _, entry := range mm.Entries {
				disc.Mapping[keyString(entry.Key)] = v2ScalarString(entry.Value, "discriminator.mapping."+keyString(entry.Key), &diags)
			}
		}
	}
	disc.Extensions = nodeExtensions(m)
	return disc, diags
}

func parseXML(node Node) (*XML, []Diagnostic) {
	m, ok := node.(*MapNode)
	if !ok {
		return nil, nil
	}
	var diags []Diagnostic
	x := &XML{
		Name:           v2ScalarString(findEntryValue(m, "name"), "xml.name", &diags),
		Namespace:      v2ScalarString(findEntryValue(m, "namespace"), "xml.namespace", &diags),
		Prefix:         v2ScalarString(findEntryValue(m, "prefix"), "xml.prefix", &diags),
		Attribute:      v2ScalarBool(findEntryValue(m, "attribute"), "xml.attribute", &diags),
		Wrapped:        v2ScalarBool(findEntryValue(m, "wrapped"), "xml.wrapped", &diags),
		SourceLocation: nodeLoc(node),
	}
	x.Extensions = nodeExtensions(m)
	return x, diags
}

func parseParameterMap(node Node) (map[string]*Parameter, []Diagnostic) {
	m, ok := node.(*MapNode)
	if !ok {
		return nil, []Diagnostic{diagInvalidType("parameters", "object", node)}
	}
	params := make(map[string]*Parameter, len(m.Entries))
	var diags []Diagnostic
	for _, entry := range m.Entries {
		key := keyString(entry.Key)
		if key == "" {
			continue
		}
		param, d := parseParameter(entry.Value)
		diags = append(diags, d...)
		params[key] = &param
	}
	return params, diags
}

func parseResponseMap(node Node, produces []string) (map[string]*Response, []Diagnostic) {
	m, ok := node.(*MapNode)
	if !ok {
		return nil, []Diagnostic{diagInvalidType("responses", "object", node)}
	}
	responses := make(map[string]*Response, len(m.Entries))
	var diags []Diagnostic
	for _, entry := range m.Entries {
		key := keyString(entry.Key)
		if key == "" {
			continue
		}
		resp, d := parseResponse(entry.Value, produces)
		diags = append(diags, d...)
		responses[key] = resp
	}
	return responses, diags
}

func parseHeaderMap(node Node) (map[string]*Header, []Diagnostic) {
	m, ok := node.(*MapNode)
	if !ok {
		return nil, []Diagnostic{diagInvalidType("headers", "object", node)}
	}
	headers := make(map[string]*Header, len(m.Entries))
	var diags []Diagnostic
	for _, entry := range m.Entries {
		key := keyString(entry.Key)
		if key == "" {
			continue
		}
		header, d := parseHeader(entry.Value)
		diags = append(diags, d...)
		headers[key] = header
	}
	return headers, diags
}

func parseHeader(node Node) (*Header, []Diagnostic) {
	if ref := stringValue(node); ref != "" && strings.HasPrefix(ref, "#") {
		return &Header{Ref: ref, SourceLocation: nodeLoc(node)}, nil
	}
	m, ok := node.(*MapNode)
	if !ok {
		return nil, []Diagnostic{diagInvalidType("header", "object", node)}
	}
	var diags []Diagnostic
	header := &Header{
		Ref:             v2ScalarString(findEntryValue(m, "$ref"), "header.$ref", &diags),
		Description:     v2ScalarString(findEntryValue(m, "description"), "header.description", &diags),
		Required:        v2ScalarBool(findEntryValue(m, "required"), "header.required", &diags),
		Deprecated:      v2ScalarBool(findEntryValue(m, "deprecated"), "header.deprecated", &diags),
		AllowEmptyValue: v2ScalarBool(findEntryValue(m, "allowEmptyValue"), "header.allowEmptyValue", &diags),
		SourceLocation:  nodeLoc(node),
	}
	if schemaNode := findEntryValue(m, "schema"); schemaNode != nil {
		schema, d := parseSchema(schemaNode)
		diags = append(diags, d...)
		header.Schema = schema
	} else {
		schema, d := parameterSchemaFromType(m)
		diags = append(diags, d...)
		header.Schema = schema
	}
	header.Extensions = nodeExtensions(m)
	return header, diags
}

func parseExampleMap(node Node) (map[string]*Example, []Diagnostic) {
	m, ok := node.(*MapNode)
	if !ok {
		return nil, []Diagnostic{diagInvalidType("examples", "object", node)}
	}
	examples := make(map[string]*Example, len(m.Entries))
	var diags []Diagnostic
	for _, entry := range m.Entries {
		key := keyString(entry.Key)
		if key == "" {
			continue
		}
		example, d := parseExample(entry.Value)
		diags = append(diags, d...)
		examples[key] = example
	}
	return examples, diags
}

func parseExample(node Node) (*Example, []Diagnostic) {
	if ref := stringValue(node); ref != "" && strings.HasPrefix(ref, "#") {
		return &Example{Ref: ref, SourceLocation: nodeLoc(node)}, nil
	}
	m, ok := node.(*MapNode)
	if !ok {
		return nil, []Diagnostic{diagInvalidType("example", "object", node)}
	}
	var diags []Diagnostic
	ex := &Example{
		Ref:            v2ScalarString(findEntryValue(m, "$ref"), "example.$ref", &diags),
		Summary:        v2ScalarString(findEntryValue(m, "summary"), "example.summary", &diags),
		Description:    v2ScalarString(findEntryValue(m, "description"), "example.description", &diags),
		Value:          nodeToNative(findEntryValue(m, "value")),
		ExternalValue:  v2ScalarString(findEntryValue(m, "externalValue"), "example.externalValue", &diags),
		SourceLocation: nodeLoc(node),
	}
	ex.Extensions = nodeExtensions(m)
	return ex, diags
}

func parseSecurityDefinitions(node Node) (map[string]*SecurityScheme, []Diagnostic) {
	m, ok := node.(*MapNode)
	if !ok {
		return nil, []Diagnostic{diagInvalidType("securityDefinitions", "object", node)}
	}
	schemes := make(map[string]*SecurityScheme, len(m.Entries))
	var diags []Diagnostic
	for _, entry := range m.Entries {
		key := keyString(entry.Key)
		if key == "" {
			continue
		}
		scheme, d := parseSecurityScheme(entry.Value)
		diags = append(diags, d...)
		schemes[key] = scheme
	}
	return schemes, diags
}

func parseSecurityScheme(node Node) (*SecurityScheme, []Diagnostic) {
	m, ok := node.(*MapNode)
	if !ok {
		return nil, []Diagnostic{diagInvalidType("securityDefinition", "object", node)}
	}

	var diags []Diagnostic
	secType := v2ScalarString(findEntryValue(m, "type"), "security.type", &diags)
	scheme := &SecurityScheme{
		Type:           secType,
		Description:    v2ScalarString(findEntryValue(m, "description"), "security.description", &diags),
		Name:           v2ScalarString(findEntryValue(m, "name"), "security.name", &diags),
		In:             v2ScalarString(findEntryValue(m, "in"), "security.in", &diags),
		SourceLocation: nodeLoc(node),
	}

	switch secType {
	case "basic":
		// Swagger 2.0 "basic" maps to OpenAPI 3.0 "http" with scheme "basic".
		scheme.Type = "http"
		scheme.Scheme = "basic"
	case "apiKey":
		// name and in are already copied.
	case "oauth2":
		flows, d := parseOAuthFlows(m)
		scheme.Flows = flows
		diags = append(diags, d...)
	}

	scheme.Extensions = nodeExtensions(m)
	return scheme, diags
}

func parseOAuthFlows(m *MapNode) (*OAuthFlows, []Diagnostic) {
	var diags []Diagnostic
	flow := v2ScalarString(findEntryValue(m, "flow"), "oauth2.flow", &diags)
	if flow == "" {
		loc := nodeLoc(m)
		return nil, []Diagnostic{{
			Severity:       SeverityError,
			Summary:        "Missing OAuth2 flow",
			Detail:         "oauth2 security scheme missing required flow field",
			SourceLocation: &loc,
		}}
	}

	scopes := parseScopes(findEntryValue(m, "scopes"))
	flows := &OAuthFlows{SourceLocation: nodeLoc(m)}

	switch flow {
	case "implicit":
		flows.Implicit = &OAuthFlow{
			AuthorizationURL: v2ScalarString(findEntryValue(m, "authorizationUrl"), "oauth2.authorizationUrl", &diags),
			Scopes:           scopes,
			SourceLocation:   nodeLoc(m),
		}
	case "password":
		flows.Password = &OAuthFlow{
			TokenURL:       v2ScalarString(findEntryValue(m, "tokenUrl"), "oauth2.tokenUrl", &diags),
			Scopes:         scopes,
			SourceLocation: nodeLoc(m),
		}
	case "application":
		flows.ClientCredentials = &OAuthFlow{
			TokenURL:       v2ScalarString(findEntryValue(m, "tokenUrl"), "oauth2.tokenUrl", &diags),
			Scopes:         scopes,
			SourceLocation: nodeLoc(m),
		}
	case "accessCode":
		flows.AuthorizationCode = &OAuthFlow{
			AuthorizationURL: v2ScalarString(findEntryValue(m, "authorizationUrl"), "oauth2.authorizationUrl", &diags),
			TokenURL:         v2ScalarString(findEntryValue(m, "tokenUrl"), "oauth2.tokenUrl", &diags),
			Scopes:           scopes,
			SourceLocation:   nodeLoc(m),
		}
	default:
		loc := nodeLoc(m)
		return nil, []Diagnostic{{
			Severity:       SeverityWarning,
			Summary:        "Unrecognized OAuth2 flow",
			Detail:         fmt.Sprintf("OAuth2 flow %q is not recognized", flow),
			SourceLocation: &loc,
		}}
	}

	if refreshNode := findEntryValue(m, "refreshUrl"); refreshNode != nil {
		if url := v2ScalarString(refreshNode, "oauth2.refreshUrl", &diags); url != "" {
			for _, f := range []*OAuthFlow{flows.Implicit, flows.Password, flows.ClientCredentials, flows.AuthorizationCode} {
				if f != nil {
					f.RefreshURL = url
				}
			}
		}
	}

	flows.Extensions = nodeExtensions(m)
	return flows, diags
}

func parseScopes(node Node) map[string]string {
	m, ok := node.(*MapNode)
	if !ok {
		return nil
	}
	scopes := make(map[string]string, len(m.Entries))
	for _, entry := range m.Entries {
		scopes[keyString(entry.Key)] = stringValue(entry.Value)
	}
	return scopes
}

func parseSecurityRequirements(node Node) ([]SecurityRequirement, []Diagnostic) {
	seq, ok := node.(*SequenceNode)
	if !ok {
		return nil, []Diagnostic{diagInvalidType("security", "array", node)}
	}
	requirements := make([]SecurityRequirement, 0, len(seq.Items))
	var diags []Diagnostic
	for _, item := range seq.Items {
		m, ok := item.(*MapNode)
		if !ok {
			diags = append(diags, diagInvalidType("security item", "object", item))
			continue
		}
		req := SecurityRequirement{
			Requirements:   make(map[string][]string, len(m.Entries)),
			SourceLocation: nodeLoc(item),
		}
		for _, entry := range m.Entries {
			req.Requirements[keyString(entry.Key)] = v2StringSlice(entry.Value, "securityRequirement."+keyString(entry.Key), &diags)
		}
		requirements = append(requirements, req)
	}
	return requirements, diags
}

func parseTags(node Node) ([]Tag, []Diagnostic) {
	seq, ok := node.(*SequenceNode)
	if !ok {
		return nil, []Diagnostic{diagInvalidType("tags", "array", node)}
	}
	tags := make([]Tag, 0, len(seq.Items))
	var diags []Diagnostic
	for _, item := range seq.Items {
		m, ok := item.(*MapNode)
		if !ok {
			diags = append(diags, diagInvalidType("tag", "object", item))
			continue
		}
		var d []Diagnostic
		t := Tag{
			Name:           v2ScalarString(findEntryValue(m, "name"), "tag.name", &d),
			Description:    v2ScalarString(findEntryValue(m, "description"), "tag.description", &d),
			ExternalDocs:   parseExternalDocs(findEntryValue(m, "externalDocs"), &d),
			SourceLocation: nodeLoc(item),
		}
		diags = append(diags, d...)
		t.Extensions = nodeExtensions(m)
		tags = append(tags, t)
	}
	return tags, diags
}

func parseExternalDocs(node Node, diags *[]Diagnostic) *ExternalDocs {
	m, ok := node.(*MapNode)
	if !ok {
		return nil
	}
	ed := &ExternalDocs{
		Description:    v2ScalarString(findEntryValue(m, "description"), "externalDocs.description", diags),
		URL:            v2ScalarString(findEntryValue(m, "url"), "externalDocs.url", diags),
		SourceLocation: nodeLoc(node),
	}
	ed.Extensions = nodeExtensions(m)
	return ed
}

// findEntryValue returns the value node for the given key in a MapNode, or nil.
// A duplicated key resolves to the last occurrence, matching the converters'
// last-wins map assignment (H-2).
func findEntryValue(m *MapNode, key string) Node {
	for i := len(m.Entries) - 1; i >= 0; i-- {
		if m.Entries[i].Key != nil && m.Entries[i].Key.Value == key {
			return m.Entries[i].Value
		}
	}
	return nil
}

func keyString(key *ScalarNode) string {
	if key == nil {
		return ""
	}
	if s, ok := key.Value.(string); ok {
		return s
	}
	return key.Raw
}

func scalarValue(n Node) any {
	if n == nil {
		return nil
	}
	s, ok := n.(*ScalarNode)
	if !ok {
		return nil
	}
	return s.Value
}

func stringValue(n Node) string {
	if n == nil {
		return ""
	}
	s, ok := n.(*ScalarNode)
	if !ok {
		return ""
	}
	if v, ok := s.Value.(string); ok {
		return v
	}
	if s.Raw != "" {
		return s.Raw
	}
	return ""
}

// v2AnySlice extracts a sequence of arbitrary values from n, appending warning
// diagnostics when the node is present but not a sequence or when an item is
// not a scalar. The prior anySlice returned nil silently for a non-sequence
// and nulled non-scalar items, so `enum: 5` produced zero diagnostics (M-2).
func v2AnySlice(n Node, path string, diags *[]Diagnostic) []any {
	if n == nil {
		return nil
	}
	seq, ok := n.(*SequenceNode)
	if !ok {
		*diags = append(*diags, v2ScalarTypeMismatchDiag(n, path, "sequence"))
		return nil
	}
	out := make([]any, 0, len(seq.Items))
	for _, item := range seq.Items {
		if _, ok := item.(*ScalarNode); !ok {
			*diags = append(*diags, v2ScalarTypeMismatchDiag(item, path+" item", "scalar"))
			continue
		}
		out = append(out, scalarValue(item))
	}
	return out
}

func diagInvalidType(path, want string, got Node) Diagnostic {
	loc := nodeLoc(got)
	if loc.Path == "" && path != "" {
		loc.Path = "/" + path
	}
	return Diagnostic{
		Severity:       SeverityError,
		Summary:        "Invalid OpenAPI structure",
		Detail:         fmt.Sprintf("%s must be a %s, got %T", path, want, got),
		SourceLocation: &loc,
	}
}

// v2ScalarTypeMismatchDiag returns a warning diagnostic for a scalar field whose
// node is not the expected type. path is the JSON/YAML pointer segment used
// when the node has no explicit source location.
func v2ScalarTypeMismatchDiag(n Node, path, expected string) Diagnostic {
	loc := nodeLoc(n)
	if loc.Path == "" && path != "" {
		loc.Path = "/" + path
	}
	return Diagnostic{
		Severity:       SeverityWarning,
		Summary:        fmt.Sprintf("Invalid %s value", path),
		Detail:         fmt.Sprintf("%s must be a %s scalar, got %T", path, expected, n),
		SourceLocation: &loc,
	}
}

// v2ScalarString extracts a string scalar from n, appending a warning
// diagnostic to diags when the node is present but not a string.
func v2ScalarString(n Node, path string, diags *[]Diagnostic) string {
	if n == nil {
		return ""
	}
	v, ok := nodeString(n)
	if !ok {
		*diags = append(*diags, v2ScalarTypeMismatchDiag(n, path, "string"))
	}
	return v
}

// v2ScalarBool extracts a boolean scalar from n, appending a warning diagnostic
// to diags when the node is present but not a boolean.
func v2ScalarBool(n Node, path string, diags *[]Diagnostic) bool {
	if n == nil {
		return false
	}
	v, ok := nodeBool(n)
	if !ok {
		*diags = append(*diags, v2ScalarTypeMismatchDiag(n, path, "boolean"))
	}
	return v
}

// v2ScalarInt extracts an integer scalar from n, appending a warning diagnostic
// to diags when the node is present but not a number.
func v2ScalarInt(n Node, path string, diags *[]Diagnostic) int {
	if n == nil {
		return 0
	}
	v, ok := nodeInt(n)
	if !ok {
		*diags = append(*diags, v2ScalarTypeMismatchDiag(n, path, "integer"))
	}
	return v
}

// v2ScalarFloat extracts a numeric scalar from n, appending a warning
// diagnostic to diags when the node is present but not a number.
func v2ScalarFloat(n Node, path string, diags *[]Diagnostic) float64 {
	if n == nil {
		return 0
	}
	v, ok := nodeFloat(n)
	if !ok {
		*diags = append(*diags, v2ScalarTypeMismatchDiag(n, path, "number"))
	}
	return v
}

// v2ScalarFloatPtr and v2ScalarIntPtr wrap the scalar parsers so schema
// constraints can distinguish a declared 0 bound from an absent one (G39):
// nil node → nil pointer, otherwise a pointer to the parsed value (0
// included).
func v2ScalarFloatPtr(n Node, path string, diags *[]Diagnostic) *float64 {
	if n == nil {
		return nil
	}
	v := v2ScalarFloat(n, path, diags)
	return &v
}

func v2ScalarIntPtr(n Node, path string, diags *[]Diagnostic) *int {
	if n == nil {
		return nil
	}
	v := v2ScalarInt(n, path, diags)
	return &v
}

// swaggerVersion extracts the top-level Swagger 2.0 "swagger" version field. The
// version is conventionally the string "2.0" but is frequently left unquoted in
// YAML (e.g. `swagger: 2.0`), which the lexer represents as a numeric scalar.
// To avoid spurious diagnostics on that common form, any scalar — string, number,
// or boolean — is preserved as its raw textual form without warning; only a
// genuinely non-scalar value (a sequence or mapping) emits a warning.
func swaggerVersion(n Node, path string, diags *[]Diagnostic) string {
	if n == nil {
		return ""
	}
	if s, ok := n.(*ScalarNode); ok {
		if v, ok := s.Value.(string); ok {
			return v
		}
		return s.Raw
	}
	*diags = append(*diags, v2ScalarTypeMismatchDiag(n, path, "string"))
	return ""
}

// v2StringSlice extracts a sequence of strings from n, appending warning
// diagnostics to diags when the node is present but not a sequence or when an
// item is not a string. An explicit empty sequence returns a non-nil empty
// slice, preserving the distinction between "absent" and "explicitly empty"
// that callers rely on when deciding whether to fall back to global values.
func v2StringSlice(n Node, path string, diags *[]Diagnostic) []string {
	if n == nil {
		return nil
	}
	seq, ok := n.(*SequenceNode)
	if !ok {
		*diags = append(*diags, v2ScalarTypeMismatchDiag(n, path, "sequence of strings"))
		return nil
	}
	out := make([]string, 0, len(seq.Items))
	for _, item := range seq.Items {
		if str, ok := nodeString(item); ok {
			out = append(out, str)
		} else {
			*diags = append(*diags, v2ScalarTypeMismatchDiag(item, path+" item", "string"))
		}
	}
	return out
}
