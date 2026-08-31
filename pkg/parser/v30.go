package parser

import (
	"fmt"
)

// v30Converter carries conversion state shared by the OpenAPI 3.0.x and 3.1.x
// converters. It collects non-fatal diagnostics and knows the target version so
// that version-specific warnings (e.g., webhooks in 3.0) can be emitted.
type v30Converter struct {
	diags   []Diagnostic
	version Version
	budget  *Budget
}

// warn appends a non-fatal warning diagnostic.
func (c *v30Converter) warn(loc SourceLocation, summary, detail string) {
	c.diags = append(c.diags, Diagnostic{
		Severity:       SeverityWarning,
		Summary:        summary,
		Detail:         detail,
		SourceLocation: &loc,
	})
}

// addError appends a non-fatal error diagnostic.
func (c *v30Converter) addError(loc SourceLocation, summary, detail string) {
	c.diags = append(c.diags, Diagnostic{
		Severity:       SeverityError,
		Summary:        summary,
		Detail:         detail,
		SourceLocation: &loc,
	})
}

// warnTypeMismatch emits a warning when a node is not the expected structured
// type (e.g., a string where a mapping was expected).
func (c *v30Converter) warnTypeMismatch(loc SourceLocation, field string, got Node) {
	c.warn(loc, fmt.Sprintf("%s has unexpected type", field),
		fmt.Sprintf("expected a mapping or sequence, got %T", got))
}

// warnScalarTypeMismatch records a warning when a scalar field has the wrong
// dynamic type (e.g. a number where a string was expected). nil nodes are
// ignored so callers do not emit diagnostics for missing optional fields.
func (c *v30Converter) warnScalarTypeMismatch(n Node, field, expected string) {
	if n == nil {
		return
	}
	c.warn(nodeLoc(n), fmt.Sprintf("%s has unexpected type", field),
		fmt.Sprintf("expected %s, got %T", expected, n))
}

func (c *v30Converter) scalarString(n Node, field string) string {
	s, ok := nodeString(n)
	if !ok {
		c.warnScalarTypeMismatch(n, field, "string")
	}
	return s
}

func (c *v30Converter) scalarBool(n Node, field string) bool {
	b, ok := nodeBool(n)
	if !ok {
		c.warnScalarTypeMismatch(n, field, "boolean")
	}
	return b
}

func (c *v30Converter) scalarInt(n Node, field string) int {
	i, ok := nodeInt(n)
	if !ok {
		c.warnScalarTypeMismatch(n, field, "integer")
	}
	return i
}

func (c *v30Converter) scalarFloat(n Node, field string) float64 {
	f, ok := nodeFloat(n)
	if !ok {
		c.warnScalarTypeMismatch(n, field, "number")
	}
	return f
}

// scalarFloatPtr and scalarIntPtr wrap the scalar parsers so schema
// constraints can distinguish a declared 0 bound from an absent one (G39):
// nil node → nil pointer, otherwise a pointer to the parsed value (0
// included).
func (c *v30Converter) scalarFloatPtr(n Node, field string) *float64 {
	if n == nil {
		return nil
	}
	v := c.scalarFloat(n, field)
	return &v
}

func (c *v30Converter) scalarIntPtr(n Node, field string) *int {
	if n == nil {
		return nil
	}
	v := c.scalarInt(n, field)
	return &v
}

// scalarAnySlice extracts a sequence of arbitrary values from n, appending a
// warning diagnostic when the node is present but not a sequence. Unlike the
// silent nodeNativeSlice, a non-sequence enum value (e.g. `enum: 5`) is
// surfaced rather than dropped with zero diagnostics (M-2).
func (c *v30Converter) scalarAnySlice(n Node, field string) []any {
	s, ok := n.(*SequenceNode)
	if !ok {
		c.warnScalarTypeMismatch(n, field, "sequence")
		return nil
	}
	out := make([]any, 0, len(s.Items))
	for _, item := range s.Items {
		out = append(out, nodeToNative(item))
	}
	return out
}

func (c *v30Converter) scalarStringSlice(n Node, field string) []string {
	s, ok := n.(*SequenceNode)
	if !ok {
		c.warnScalarTypeMismatch(n, field, "sequence of strings")
		return nil
	}
	out := make([]string, 0, len(s.Items))
	for _, item := range s.Items {
		if str, ok := nodeString(item); ok {
			out = append(out, str)
		} else {
			c.warnScalarTypeMismatch(item, field+" item", "string")
		}
	}
	return out
}

func (c *v30Converter) scalarStringMap(n Node, field string) map[string]string {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnScalarTypeMismatch(n, field, "mapping of strings")
		return nil
	}
	out := make(map[string]string, len(m.Entries))
	forEachEntry(m, func(key string, value Node) {
		if str, ok := nodeString(value); ok {
			out[key] = str
		} else {
			c.warnScalarTypeMismatch(value, field+"."+key, "string")
		}
	})
	return out
}

// ConvertV30 transforms the raw AST of an OpenAPI 3.0.x document into the
// version-agnostic Spec model. It preserves source locations where possible.
// A returned error means the document could not be converted at all.
//
// Scalar field type mismatches (for example "description: 42") are reported as
// warning diagnostics so that best-effort conversion can continue. Structural
// type mismatches (e.g., the info field being a string instead of a mapping)
// produce warning diagnostics. Additional warning diagnostics (for example for
// circular schema references) are collected during conversion and returned
// alongside the Spec.
func ConvertV30(root Node, opts ...ConvertOption) (*Spec, []Diagnostic, error) {
	if root == nil {
		return nil, []Diagnostic{{
			Severity: SeverityError,
			Summary:  "Missing OpenAPI document",
			Detail:   "ConvertV30 received a nil document root.",
		}}, fmt.Errorf("nil document root")
	}

	m, ok := root.(*MapNode)
	if !ok {
		rootLoc := nodeLoc(root)
		return nil, []Diagnostic{{
			Severity:       SeverityError,
			Summary:        "Invalid OpenAPI 3.0 document",
			Detail:         "Document root must be a JSON object or YAML mapping.",
			SourceLocation: &rootLoc,
		}}, fmt.Errorf("document root is %T, expected *MapNode", root)
	}

	cfg := defaultConvertConfig()
	for _, opt := range opts {
		opt(cfg)
	}
	budget := NewBudget(cfg.limits)

	c := &v30Converter{version: Version3_0, budget: budget}
	spec := &Spec{SourceLocation: m.SourceLocation}

	if err := budget.Account(estimateNodeMemory(root)); err != nil {
		c.diags = append(c.diags, budgetExceededDiag(err, m.SourceLocation))
		return spec, c.diags, nil
	}

	forEachEntry(m, func(key string, value Node) {
		switch key {
		case "openapi":
			spec.OpenAPI = c.scalarString(value, "openapi")
		case "info":
			spec.Info = c.convertInfo(value)
		case "servers":
			spec.Servers = c.convertServers(value)
		case "paths":
			spec.Paths = c.convertPathItems(value)
		case "webhooks":
			if c.version == Version3_0 {
				c.warn(m.SourceLocation, "webhooks field in OpenAPI 3.0.x document",
					"The 'webhooks' top-level field is not defined in OpenAPI 3.0.3; it will be treated as path items for forward compatibility.")
			}
			spec.Webhooks = c.convertPathItems(value)
		case "components":
			spec.Components = c.convertComponents(value)
		case "security":
			spec.Security = c.convertSecurityRequirements(value)
		case "tags":
			spec.Tags = c.convertTags(value)
		case "externalDocs":
			spec.ExternalDocs = c.convertExternalDocs(value)
		}
	})

	circularRefs, circularDiags := DetectCircularSchemaRefs(root)
	c.diags = append(c.diags, circularDiags...)
	markCircularSchemaRefs(spec, circularRefs)

	spec.Extensions = nodeExtensions(root)
	return spec, c.diags, nil
}

func (c *v30Converter) convertInfo(n Node) *Info {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "info", n)
		return nil
	}
	info := &Info{SourceLocation: m.SourceLocation}
	forEachEntry(m, func(key string, value Node) {
		switch key {
		case "title":
			info.Title = c.scalarString(value, "title")
		case "summary":
			// info.summary is an OpenAPI 3.1 field; read it here so it is not
			// silently dropped (M-32). It is harmless for 3.0 specs that do not
			// set it.
			info.Summary = c.scalarString(value, "summary")
		case "description":
			info.Description = c.scalarString(value, "description")
		case "termsOfService":
			info.TermsOfService = c.scalarString(value, "termsOfService")
		case "version":
			info.Version = c.scalarString(value, "version")
		case "contact":
			info.Contact = c.convertContact(value)
		case "license":
			info.License = c.convertLicense(value)
		}
	})
	info.Extensions = nodeExtensions(n)
	return info
}

func (c *v30Converter) convertContact(n Node) *Contact {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "contact", n)
		return nil
	}
	contact := &Contact{SourceLocation: m.SourceLocation}
	forEachEntry(m, func(key string, value Node) {
		switch key {
		case "name":
			contact.Name = c.scalarString(value, "name")
		case "url":
			contact.URL = c.scalarString(value, "url")
		case "email":
			contact.Email = c.scalarString(value, "email")
		}
	})
	contact.Extensions = nodeExtensions(n)
	return contact
}

func (c *v30Converter) convertLicense(n Node) *License {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "license", n)
		return nil
	}
	l := &License{SourceLocation: m.SourceLocation}
	forEachEntry(m, func(key string, value Node) {
		switch key {
		case "name":
			l.Name = c.scalarString(value, "name")
		case "url":
			l.URL = c.scalarString(value, "url")
		case "identifier":
			l.Identifier = c.scalarString(value, "identifier")
		}
	})
	l.Extensions = nodeExtensions(n)
	return l
}

func (c *v30Converter) convertServers(n Node) []Server {
	s, ok := n.(*SequenceNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "servers", n)
		return nil
	}
	out := make([]Server, 0, len(s.Items))
	for _, item := range s.Items {
		if srv := c.convertServer(item); srv != nil {
			out = append(out, *srv)
		}
	}
	return out
}

func (c *v30Converter) convertServer(n Node) *Server {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "server", n)
		return nil
	}
	srv := &Server{SourceLocation: m.SourceLocation}
	forEachEntry(m, func(key string, value Node) {
		switch key {
		case "url":
			srv.URL = c.scalarString(value, "url")
		case "description":
			srv.Description = c.scalarString(value, "description")
		case "variables":
			srv.Variables = c.convertServerVariables(value)
		}
	})
	srv.Extensions = nodeExtensions(n)
	return srv
}

func (c *v30Converter) convertServerVariables(n Node) map[string]*ServerVariable {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "server variables", n)
		return nil
	}
	out := make(map[string]*ServerVariable, len(m.Entries))
	forEachEntry(m, func(name string, value Node) {
		out[name] = c.convertServerVariable(value)
	})
	return out
}

func (c *v30Converter) convertServerVariable(n Node) *ServerVariable {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "server variable", n)
		return nil
	}
	sv := &ServerVariable{SourceLocation: m.SourceLocation}
	forEachEntry(m, func(key string, value Node) {
		switch key {
		case "default":
			sv.Default = c.scalarString(value, "default")
		case "description":
			sv.Description = c.scalarString(value, "description")
		case "enum":
			sv.Enum = c.scalarStringSlice(value, "enum")
		}
	})
	sv.Extensions = nodeExtensions(n)
	return sv
}

func (c *v30Converter) convertPathItems(n Node) map[string]*PathItem {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "paths", n)
		return nil
	}
	out := make(map[string]*PathItem, len(m.Entries))
	forEachEntry(m, func(path string, value Node) {
		out[path] = c.convertPathItem(value)
	})
	return out
}

func (c *v30Converter) convertPathItem(n Node) *PathItem {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "path item", n)
		return nil
	}
	pi := &PathItem{SourceLocation: m.SourceLocation}
	forEachEntry(m, func(key string, value Node) {
		switch key {
		case "$ref":
			pi.Ref = c.scalarString(value, "$ref")
		case "summary":
			pi.Summary = c.scalarString(value, "summary")
		case "description":
			pi.Description = c.scalarString(value, "description")
		case "get":
			pi.Get = c.convertOperation(value)
		case "put":
			pi.Put = c.convertOperation(value)
		case "post":
			pi.Post = c.convertOperation(value)
		case "delete":
			pi.Delete = c.convertOperation(value)
		case "options":
			pi.Options = c.convertOperation(value)
		case "head":
			pi.Head = c.convertOperation(value)
		case "patch":
			pi.Patch = c.convertOperation(value)
		case "trace":
			pi.Trace = c.convertOperation(value)
		case "servers":
			pi.Servers = c.convertServers(value)
		case "parameters":
			pi.Parameters = c.convertParameters(value)
		}
	})
	pi.Extensions = nodeExtensions(n)
	return pi
}

func (c *v30Converter) convertOperation(n Node) *Operation {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "operation", n)
		return nil
	}
	op := &Operation{SourceLocation: m.SourceLocation}
	forEachEntry(m, func(key string, value Node) {
		switch key {
		case "tags":
			op.Tags = c.scalarStringSlice(value, "tags")
		case "summary":
			op.Summary = c.scalarString(value, "summary")
		case "description":
			op.Description = c.scalarString(value, "description")
		case "operationId":
			op.OperationID = c.scalarString(value, "operationId")
		case "externalDocs":
			op.ExternalDocs = c.convertExternalDocs(value)
		case "parameters":
			op.Parameters = c.convertParameters(value)
		case "requestBody":
			op.RequestBody = c.convertRequestBody(value)
		case "responses":
			op.Responses = c.convertResponses(value)
		case "callbacks":
			op.Callbacks = c.convertCallbacks(value)
		case "deprecated":
			op.Deprecated = c.scalarBool(value, "deprecated")
		case "security":
			op.Security = c.convertSecurityRequirements(value)
		case "servers":
			op.Servers = c.convertServers(value)
		}
	})
	op.Extensions = nodeExtensions(n)
	return op
}

func (c *v30Converter) convertParameters(n Node) []Parameter {
	s, ok := n.(*SequenceNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "parameters", n)
		return nil
	}
	out := make([]Parameter, 0, len(s.Items))
	for _, item := range s.Items {
		if p := c.convertParameter(item); p != nil {
			out = append(out, *p)
		}
	}
	return out
}

func (c *v30Converter) convertParameter(n Node) *Parameter {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "parameter", n)
		return nil
	}
	p := &Parameter{SourceLocation: m.SourceLocation}
	forEachEntry(m, func(key string, value Node) {
		switch key {
		case "$ref":
			p.Ref = c.scalarString(value, "$ref")
		case "name":
			p.Name = c.scalarString(value, "name")
		case "in":
			p.In = c.scalarString(value, "in")
		case "description":
			p.Description = c.scalarString(value, "description")
		case "required":
			p.Required = c.scalarBool(value, "required")
		case "deprecated":
			p.Deprecated = c.scalarBool(value, "deprecated")
		case "allowEmptyValue":
			p.AllowEmptyValue = c.scalarBool(value, "allowEmptyValue")
		case "style":
			p.Style = c.scalarString(value, "style")
		case "explode":
			p.Explode = c.scalarBool(value, "explode")
		case "allowReserved":
			p.AllowReserved = c.scalarBool(value, "allowReserved")
		case "schema":
			p.Schema = c.convertSchema(value)
		case "content":
			p.Content = c.convertContent(value)
		case "example":
			p.Example = nodeToNative(value)
		case "examples":
			p.Examples = c.convertExamples(value)
		}
	})
	// A parameter may declare its description either on the parameter object or
	// on its schema; fall back to the schema's when the object's is absent so
	// the prose is not silently dropped. Specs that describe every parameter on
	// the schema (e.g. Gigamon's FM bundle) otherwise lose all parameter
	// descriptions before they reach attribute construction.
	if p.Description == "" && p.Schema != nil {
		p.Description = p.Schema.Description
	}
	p.Extensions = nodeExtensions(n)
	return p
}

func (c *v30Converter) convertRequestBody(n Node) *RequestBody {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "request body", n)
		return nil
	}
	rb := &RequestBody{SourceLocation: m.SourceLocation}
	forEachEntry(m, func(key string, value Node) {
		switch key {
		case "$ref":
			rb.Ref = c.scalarString(value, "$ref")
		case "description":
			rb.Description = c.scalarString(value, "description")
		case "required":
			rb.Required = c.scalarBool(value, "required")
		case "content":
			rb.Content = c.convertContent(value)
		}
	})
	rb.Extensions = nodeExtensions(n)
	return rb
}

func (c *v30Converter) convertResponses(n Node) map[string]*Response {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "responses", n)
		return nil
	}
	out := make(map[string]*Response, len(m.Entries))
	forEachEntry(m, func(code string, value Node) {
		out[code] = c.convertResponse(value)
	})
	return out
}

func (c *v30Converter) convertResponse(n Node) *Response {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "response", n)
		return nil
	}
	r := &Response{SourceLocation: m.SourceLocation}
	forEachEntry(m, func(key string, value Node) {
		switch key {
		case "$ref":
			r.Ref = c.scalarString(value, "$ref")
		case "description":
			r.Description = c.scalarString(value, "description")
		case "headers":
			r.Headers = c.convertHeaders(value)
		case "content":
			r.Content = c.convertContent(value)
		case "links":
			r.Links = c.convertLinks(value)
		}
	})
	r.Extensions = nodeExtensions(n)
	return r
}

func (c *v30Converter) convertHeaders(n Node) map[string]*Header {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "headers", n)
		return nil
	}
	out := make(map[string]*Header, len(m.Entries))
	forEachEntry(m, func(name string, value Node) {
		out[name] = c.convertHeader(value)
	})
	return out
}

func (c *v30Converter) convertHeader(n Node) *Header {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "header", n)
		return nil
	}
	h := &Header{SourceLocation: m.SourceLocation}
	forEachEntry(m, func(key string, value Node) {
		switch key {
		case "$ref":
			h.Ref = c.scalarString(value, "$ref")
		case "description":
			h.Description = c.scalarString(value, "description")
		case "required":
			h.Required = c.scalarBool(value, "required")
		case "deprecated":
			h.Deprecated = c.scalarBool(value, "deprecated")
		case "allowEmptyValue":
			h.AllowEmptyValue = c.scalarBool(value, "allowEmptyValue")
		case "style":
			h.Style = c.scalarString(value, "style")
		case "explode":
			h.Explode = c.scalarBool(value, "explode")
		case "allowReserved":
			h.AllowReserved = c.scalarBool(value, "allowReserved")
		case "schema":
			h.Schema = c.convertSchema(value)
		case "content":
			h.Content = c.convertContent(value)
		case "example":
			h.Example = nodeToNative(value)
		case "examples":
			h.Examples = c.convertExamples(value)
		}
	})
	h.Extensions = nodeExtensions(n)
	return h
}

func (c *v30Converter) convertContent(n Node) map[string]*MediaType {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "content", n)
		return nil
	}
	out := make(map[string]*MediaType, len(m.Entries))
	forEachEntry(m, func(name string, value Node) {
		out[name] = c.convertMediaType(value)
	})
	return out
}

func (c *v30Converter) convertMediaType(n Node) *MediaType {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "media type", n)
		return nil
	}
	mt := &MediaType{SourceLocation: m.SourceLocation}
	forEachEntry(m, func(key string, value Node) {
		switch key {
		case "schema":
			mt.Schema = c.convertSchema(value)
		case "example":
			mt.Example = nodeToNative(value)
		case "examples":
			mt.Examples = c.convertExamples(value)
		case "encoding":
			mt.Encoding = c.convertEncodingMap(value)
		}
	})
	mt.Extensions = nodeExtensions(n)
	return mt
}

func (c *v30Converter) convertEncodingMap(n Node) map[string]*Encoding {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "encoding map", n)
		return nil
	}
	out := make(map[string]*Encoding, len(m.Entries))
	forEachEntry(m, func(name string, value Node) {
		out[name] = c.convertEncoding(value)
	})
	return out
}

func (c *v30Converter) convertEncoding(n Node) *Encoding {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "encoding", n)
		return nil
	}
	en := &Encoding{SourceLocation: m.SourceLocation}
	forEachEntry(m, func(key string, value Node) {
		switch key {
		case "contentType":
			en.ContentType = c.scalarString(value, "contentType")
		case "headers":
			en.Headers = c.convertHeaders(value)
		case "style":
			en.Style = c.scalarString(value, "style")
		case "explode":
			en.Explode = c.scalarBool(value, "explode")
		case "allowReserved":
			en.AllowReserved = c.scalarBool(value, "allowReserved")
		}
	})
	en.Extensions = nodeExtensions(n)
	return en
}

func (c *v30Converter) convertExamples(n Node) map[string]*Example {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "examples", n)
		return nil
	}
	out := make(map[string]*Example, len(m.Entries))
	forEachEntry(m, func(name string, value Node) {
		out[name] = c.convertExample(value)
	})
	return out
}

func (c *v30Converter) convertExample(n Node) *Example {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "example", n)
		return nil
	}
	ex := &Example{SourceLocation: m.SourceLocation}
	forEachEntry(m, func(key string, value Node) {
		switch key {
		case "$ref":
			ex.Ref = c.scalarString(value, "$ref")
		case "summary":
			ex.Summary = c.scalarString(value, "summary")
		case "description":
			ex.Description = c.scalarString(value, "description")
		case "value":
			ex.Value = nodeToNative(value)
		case "externalValue":
			ex.ExternalValue = c.scalarString(value, "externalValue")
		}
	})
	ex.Extensions = nodeExtensions(n)
	return ex
}

func (c *v30Converter) convertCallbacks(n Node) map[string]Callback {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "callbacks", n)
		return nil
	}
	out := make(map[string]Callback, len(m.Entries))
	forEachEntry(m, func(name string, value Node) {
		out[name] = c.convertCallback(value)
	})
	return out
}

func (c *v30Converter) convertCallback(n Node) Callback {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "callback", n)
		return nil
	}
	cb := Callback{}
	forEachEntry(m, func(key string, value Node) {
		if key == "$ref" {
			ref := c.scalarString(value, "$ref")
			cb["$ref"] = &PathItem{Ref: ref, SourceLocation: m.SourceLocation}
			return
		}
		cb[key] = c.convertPathItem(value)
	})
	return cb
}

func (c *v30Converter) convertLinks(n Node) map[string]*Link {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "links", n)
		return nil
	}
	out := make(map[string]*Link, len(m.Entries))
	forEachEntry(m, func(name string, value Node) {
		out[name] = c.convertLink(value)
	})
	return out
}

func (c *v30Converter) convertLink(n Node) *Link {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "link", n)
		return nil
	}
	l := &Link{SourceLocation: m.SourceLocation}
	forEachEntry(m, func(key string, value Node) {
		switch key {
		case "$ref":
			l.Ref = c.scalarString(value, "$ref")
		case "operationId":
			l.OperationID = c.scalarString(value, "operationId")
		case "operationRef":
			l.OperationRef = c.scalarString(value, "operationRef")
		case "parameters":
			l.Parameters = nodeToNativeMap(value)
		case "requestBody":
			l.RequestBody = nodeToNative(value)
		case "description":
			l.Description = c.scalarString(value, "description")
		case "server":
			l.Server = c.convertServer(value)
		}
	})
	l.Extensions = nodeExtensions(m)
	return l
}

func (c *v30Converter) convertComponents(n Node) *Components {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "components", n)
		return nil
	}
	comp := &Components{SourceLocation: m.SourceLocation}
	forEachEntry(m, func(key string, value Node) {
		switch key {
		case "schemas":
			comp.Schemas = c.convertSchemaMap(value)
		case "responses":
			comp.Responses = c.convertResponseMap(value)
		case "parameters":
			comp.Parameters = c.convertParameterMap(value)
		case "examples":
			comp.Examples = c.convertExamples(value)
		case "requestBodies":
			comp.RequestBodies = c.convertRequestBodyMap(value)
		case "headers":
			comp.Headers = c.convertHeaders(value)
		case "securitySchemes":
			comp.SecuritySchemes = c.convertSecuritySchemes(value)
		case "links":
			comp.Links = c.convertLinks(value)
		case "callbacks":
			comp.Callbacks = c.convertCallbackMap(value)
		}
	})
	comp.Extensions = nodeExtensions(m)
	return comp
}

func (c *v30Converter) convertSchemaMap(n Node) map[string]*Schema {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "schemas", n)
		return nil
	}
	out := make(map[string]*Schema, len(m.Entries))
	forEachEntry(m, func(name string, value Node) {
		// convertSchema returns nil on a type mismatch or budget exhaustion.
		// Skip nil entries (matching convertParameters) so downstream consumers
		// that do not nil-check do not panic (M-31).
		if s := c.convertSchema(value); s != nil {
			out[name] = s
		}
	})
	return out
}

func (c *v30Converter) convertResponseMap(n Node) map[string]*Response {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "responses", n)
		return nil
	}
	out := make(map[string]*Response, len(m.Entries))
	forEachEntry(m, func(name string, value Node) {
		out[name] = c.convertResponse(value)
	})
	return out
}

func (c *v30Converter) convertParameterMap(n Node) map[string]*Parameter {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "parameters", n)
		return nil
	}
	out := make(map[string]*Parameter, len(m.Entries))
	forEachEntry(m, func(name string, value Node) {
		out[name] = c.convertParameter(value)
	})
	return out
}

func (c *v30Converter) convertRequestBodyMap(n Node) map[string]*RequestBody {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "request bodies", n)
		return nil
	}
	out := make(map[string]*RequestBody, len(m.Entries))
	forEachEntry(m, func(name string, value Node) {
		out[name] = c.convertRequestBody(value)
	})
	return out
}

func (c *v30Converter) convertSecuritySchemes(n Node) map[string]*SecurityScheme {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "security schemes", n)
		return nil
	}
	out := make(map[string]*SecurityScheme, len(m.Entries))
	forEachEntry(m, func(name string, value Node) {
		out[name] = c.convertSecurityScheme(value)
	})
	return out
}

func (c *v30Converter) convertSecurityScheme(n Node) *SecurityScheme {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "security scheme", n)
		return nil
	}
	ss := &SecurityScheme{SourceLocation: m.SourceLocation}
	forEachEntry(m, func(key string, value Node) {
		switch key {
		case "$ref":
			ss.Ref = c.scalarString(value, "$ref")
		case "type":
			ss.Type = c.scalarString(value, "type")
		case "description":
			ss.Description = c.scalarString(value, "description")
		case "name":
			ss.Name = c.scalarString(value, "name")
		case "in":
			ss.In = c.scalarString(value, "in")
		case "scheme":
			ss.Scheme = c.scalarString(value, "scheme")
		case "bearerFormat":
			ss.BearerFormat = c.scalarString(value, "bearerFormat")
		case "flows":
			ss.Flows = c.convertOAuthFlows(value)
		case "openIdConnectUrl":
			ss.OpenIDConnectURL = c.scalarString(value, "openIdConnectUrl")
		}
	})
	ss.Extensions = nodeExtensions(m)
	return ss
}

func (c *v30Converter) convertOAuthFlows(n Node) *OAuthFlows {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "OAuth flows", n)
		return nil
	}
	flows := &OAuthFlows{SourceLocation: m.SourceLocation}
	forEachEntry(m, func(key string, value Node) {
		switch key {
		case "implicit":
			flows.Implicit = c.convertOAuthFlow(value)
		case "password":
			flows.Password = c.convertOAuthFlow(value)
		case "clientCredentials":
			flows.ClientCredentials = c.convertOAuthFlow(value)
		case "authorizationCode":
			flows.AuthorizationCode = c.convertOAuthFlow(value)
		}
	})
	flows.Extensions = nodeExtensions(m)
	return flows
}

func (c *v30Converter) convertOAuthFlow(n Node) *OAuthFlow {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "OAuth flow", n)
		return nil
	}
	flow := &OAuthFlow{SourceLocation: m.SourceLocation}
	forEachEntry(m, func(key string, value Node) {
		switch key {
		case "authorizationUrl":
			flow.AuthorizationURL = c.scalarString(value, "authorizationUrl")
		case "tokenUrl":
			flow.TokenURL = c.scalarString(value, "tokenUrl")
		case "refreshUrl":
			flow.RefreshURL = c.scalarString(value, "refreshUrl")
		case "scopes":
			flow.Scopes = c.scalarStringMap(value, "scopes")
		}
	})
	flow.Extensions = nodeExtensions(m)
	return flow
}

func (c *v30Converter) convertCallbackMap(n Node) map[string]Callback {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "callbacks", n)
		return nil
	}
	out := make(map[string]Callback, len(m.Entries))
	forEachEntry(m, func(name string, value Node) {
		out[name] = c.convertCallback(value)
	})
	return out
}

func (c *v30Converter) convertSecurityRequirements(n Node) []SecurityRequirement {
	s, ok := n.(*SequenceNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "security", n)
		return nil
	}
	out := make([]SecurityRequirement, 0, len(s.Items))
	for _, item := range s.Items {
		if req := c.convertSecurityRequirement(item); req != nil {
			out = append(out, *req)
		}
	}
	return out
}

func (c *v30Converter) convertSecurityRequirement(n Node) *SecurityRequirement {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "security requirement", n)
		return nil
	}
	req := SecurityRequirement{
		Requirements:   make(map[string][]string, len(m.Entries)),
		SourceLocation: m.SourceLocation,
	}
	forEachEntry(m, func(name string, value Node) {
		req.Requirements[name] = c.scalarStringSlice(value, "securityRequirement."+name)
	})
	return &req
}

func (c *v30Converter) convertTags(n Node) []Tag {
	s, ok := n.(*SequenceNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "tags", n)
		return nil
	}
	out := make([]Tag, 0, len(s.Items))
	for _, item := range s.Items {
		if t := c.convertTag(item); t != nil {
			out = append(out, *t)
		}
	}
	return out
}

func (c *v30Converter) convertTag(n Node) *Tag {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "tag", n)
		return nil
	}
	t := &Tag{SourceLocation: m.SourceLocation}
	forEachEntry(m, func(key string, value Node) {
		switch key {
		case "name":
			t.Name = c.scalarString(value, "name")
		case "description":
			t.Description = c.scalarString(value, "description")
		case "externalDocs":
			t.ExternalDocs = c.convertExternalDocs(value)
		}
	})
	t.Extensions = nodeExtensions(m)
	return t
}

func (c *v30Converter) convertExternalDocs(n Node) *ExternalDocs {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "external docs", n)
		return nil
	}
	ed := &ExternalDocs{SourceLocation: m.SourceLocation}
	forEachEntry(m, func(key string, value Node) {
		switch key {
		case "description":
			ed.Description = c.scalarString(value, "description")
		case "url":
			ed.URL = c.scalarString(value, "url")
		}
	})
	ed.Extensions = nodeExtensions(m)
	return ed
}

func (c *v30Converter) convertSchema(n Node) *Schema {
	if c.budget != nil {
		if err := c.budget.Enter(1); err != nil {
			c.diags = append(c.diags, budgetExceededDiag(err, nodeLoc(n)))
			return nil
		}
		defer c.budget.Leave()
	}

	m, ok := n.(*MapNode)
	if !ok {
		// Boolean schemas (true/false) are valid in OpenAPI 3.1 but cannot be
		// represented in the *Schema model. Surface a warning instead of
		// dropping them silently: `true` accepts anything and `false` accepts
		// nothing, so dropping changes semantics (C-3).
		if _, isBool := nodeBool(n); isBool {
			c.warn(nodeLoc(n), "boolean schema is not modeled",
				"A boolean schema (true/false) is valid in OpenAPI 3.1 but cannot be represented in the generated provider; it is being dropped. Use an explicit object schema instead.")
		} else {
			c.warnTypeMismatch(nodeLoc(n), "schema", n)
		}
		return nil
	}
	s := &Schema{SourceLocation: m.SourceLocation}
	forEachEntry(m, func(key string, value Node) {
		switch key {
		case "$ref":
			s.Ref = c.scalarString(value, "$ref")
		case "type":
			s.Type = c.convertSchemaType(value)
		case "format":
			s.Format = c.scalarString(value, "format")
		case "title":
			s.Title = c.scalarString(value, "title")
		case "description":
			s.Description = c.scalarString(value, "description")
		case "default":
			s.Default = nodeToNative(value)
		case "multipleOf":
			s.MultipleOf = c.scalarFloatPtr(value, "multipleOf")
		case "maximum":
			s.Maximum = c.scalarFloatPtr(value, "maximum")
		case "exclusiveMaximum":
			s.ExclusiveMaximum = nodeToNative(value)
		case "minimum":
			s.Minimum = c.scalarFloatPtr(value, "minimum")
		case "exclusiveMinimum":
			s.ExclusiveMinimum = nodeToNative(value)
		case "maxLength":
			s.MaxLength = c.scalarIntPtr(value, "maxLength")
		case "minLength":
			s.MinLength = c.scalarIntPtr(value, "minLength")
		case "pattern":
			s.Pattern = c.scalarString(value, "pattern")
		case "maxItems":
			s.MaxItems = c.scalarIntPtr(value, "maxItems")
		case "minItems":
			s.MinItems = c.scalarIntPtr(value, "minItems")
		case "uniqueItems":
			s.UniqueItems = c.scalarBool(value, "uniqueItems")
		case "maxProperties":
			s.MaxProperties = c.scalarInt(value, "maxProperties")
		case "minProperties":
			s.MinProperties = c.scalarInt(value, "minProperties")
		case "required":
			s.Required = c.scalarStringSlice(value, "required")
		case "enum":
			s.Enum = c.scalarAnySlice(value, "enum")
		case "allOf":
			s.AllOf = c.convertSchemaSlice(value)
		case "oneOf":
			s.OneOf = c.convertSchemaSlice(value)
		case "anyOf":
			s.AnyOf = c.convertSchemaSlice(value)
		case "not":
			s.Not = c.convertSchema(value)
		case "items":
			s.Items = c.convertItems(value)
		case "prefixItems":
			s.PrefixItems = c.convertSchemaSlice(value)
		case "properties":
			s.Properties = c.convertSchemaMap(value)
		case "additionalProperties":
			s.AdditionalProperties = c.convertAdditionalProperties(value)
		case "patternProperties":
			s.PatternProperties = c.convertSchemaMap(value)
		case "propertyNames":
			s.PropertyNames = c.convertSchema(value)
		case "contains":
			s.Contains = c.convertSchema(value)
		case "minContains":
			s.MinContains = c.scalarInt(value, "minContains")
		case "maxContains":
			s.MaxContains = c.scalarInt(value, "maxContains")
		case "discriminator":
			s.Discriminator = c.convertDiscriminator(value)
		case "xml":
			s.XML = c.convertXML(value)
		case "externalDocs":
			s.ExternalDocs = c.convertExternalDocs(value)
		case "example":
			s.Example = nodeToNative(value)
		case "examples":
			// The schema-level "examples" keyword has two legal forms: a map of
			// Example objects (OpenAPI 3.0) and an array of raw values (OpenAPI
			// 3.1 / JSON Schema 2020-12). Route by node type so a valid 3.1 spec
			// is preserved instead of dropped with a self-contradictory warning
			// (L-1).
			if seq, ok := value.(*SequenceNode); ok {
				s.ExamplesArray = make([]any, 0, len(seq.Items))
				for _, item := range seq.Items {
					s.ExamplesArray = append(s.ExamplesArray, nodeToNative(item))
				}
			} else {
				s.Examples = c.convertExamples(value)
			}
		case "nullable":
			s.Nullable = c.scalarBool(value, "nullable")
		case "readOnly":
			s.ReadOnly = c.scalarBool(value, "readOnly")
		case "writeOnly":
			s.WriteOnly = c.scalarBool(value, "writeOnly")
		case "deprecated":
			s.Deprecated = c.scalarBool(value, "deprecated")
		case "const":
			s.Const = nodeToNative(value)
		case "contentMediaType":
			s.ContentMediaType = c.scalarString(value, "contentMediaType")
		case "contentEncoding":
			s.ContentEncoding = c.scalarString(value, "contentEncoding")
		case "contentSchema":
			s.ContentSchema = c.convertSchema(value)
		case "unevaluatedProperties":
			s.UnevaluatedProperties = c.convertSchema(value)
		case "unevaluatedItems":
			s.UnevaluatedItems = c.convertSchema(value)
		case "dependentSchemas":
			s.DependentSchemas = c.convertSchemaMap(value)
		case "dependentRequired":
			s.DependentRequired = c.scalarStringStringSliceMap(value, "dependentRequired")
		case "if":
			s.If = c.convertSchema(value)
		case "then":
			s.Then = c.convertSchema(value)
		case "else":
			s.Else = c.convertSchema(value)
		}
	})
	s.Extensions = nodeExtensions(m)
	return s
}

func (c *v30Converter) scalarStringStringSliceMap(n Node, field string) map[string][]string {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnScalarTypeMismatch(n, field, "mapping of string sequences")
		return nil
	}
	out := make(map[string][]string, len(m.Entries))
	forEachEntry(m, func(key string, value Node) {
		out[key] = c.scalarStringSlice(value, field+"."+key)
	})
	return out
}

// convertSchemaType normalizes the OpenAPI schema "type" field. OpenAPI 3.0.3
// requires a string; 3.1 allows a string or an array of strings (including the
// "null" type). For 3.0.x, non-string values produce a warning and preserve the
// native value (matching 3.1's behavior rather than dropping to raw source
// text); for 3.1.x, native values are preserved to support the array-of-types
// extension and boolean/enum typing. A mapping (or any other unexpected node
// shape) emits a warning and yields nil rather than failing silently.
func (c *v30Converter) convertSchemaType(n Node) any {
	switch v := n.(type) {
	case *ScalarNode:
		str, ok := v.Value.(string)
		if !ok && c.version == Version3_0 {
			c.warn(nodeLoc(n), "schema type is not a string",
				fmt.Sprintf("expected string, got %T; preserving native value", v.Value))
			return v.Value
		}
		if !ok {
			return v.Value
		}
		return str
	case *SequenceNode:
		out := make([]any, 0, len(v.Items))
		for _, item := range v.Items {
			if c.version == Version3_0 {
				switch s := item.(type) {
				case *ScalarNode:
					if str, ok := s.Value.(string); ok {
						out = append(out, str)
						continue
					}
					c.warn(nodeLoc(item), "schema type array contains non-string item",
						fmt.Sprintf("expected string, got %T; skipping", s.Value))
				default:
					c.warn(nodeLoc(item), "schema type array contains non-scalar item",
						fmt.Sprintf("expected string scalar, got %T; skipping", item))
				}
				continue
			}
			out = append(out, nodeToNative(item))
		}
		return out
	default:
		c.warnTypeMismatch(nodeLoc(n), "schema type", n)
		return nil
	}
}

func (c *v30Converter) convertSchemaSlice(n Node) []*Schema {
	s, ok := n.(*SequenceNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "schema list", n)
		return nil
	}
	out := make([]*Schema, 0, len(s.Items))
	for _, item := range s.Items {
		// Skip nil entries (convertSchema returns nil on type mismatch or
		// budget exhaustion) so downstream consumers that do not nil-check do
		// not panic (M-31).
		if s := c.convertSchema(item); s != nil {
			out = append(out, s)
		}
	}
	return out
}

func (c *v30Converter) convertAdditionalProperties(n Node) any {
	if b, ok := nodeBool(n); ok {
		return b
	}
	return c.convertSchema(n)
}

// convertItems normalizes the JSON Schema "items" field. In OpenAPI 3.0 the
// value is always a schema, but OpenAPI 3.1 allows it to be a boolean (for
// example "items: false" to forbid additional tuple items). Boolean values
// are preserved as native bools; everything else is converted as a schema.
func (c *v30Converter) convertItems(n Node) any {
	if b, ok := nodeBool(n); ok {
		return b
	}
	return c.convertSchema(n)
}

func (c *v30Converter) convertDiscriminator(n Node) *Discriminator {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "discriminator", n)
		return nil
	}
	d := &Discriminator{SourceLocation: m.SourceLocation}
	forEachEntry(m, func(key string, value Node) {
		switch key {
		case "propertyName":
			d.PropertyName = c.scalarString(value, "propertyName")
		case "mapping":
			d.Mapping = c.scalarStringMap(value, "mapping")
		}
	})
	if d.PropertyName == "" {
		loc := nodeLoc(n)
		if loc.Path == "" {
			loc.Path = "/discriminator"
		}
		c.addError(loc, "Invalid discriminator", "discriminator propertyName must not be empty")
	}
	d.Extensions = nodeExtensions(m)
	return d
}

func (c *v30Converter) convertXML(n Node) *XML {
	m, ok := n.(*MapNode)
	if !ok {
		c.warnTypeMismatch(nodeLoc(n), "xml", n)
		return nil
	}
	x := &XML{SourceLocation: m.SourceLocation}
	forEachEntry(m, func(key string, value Node) {
		switch key {
		case "name":
			x.Name = c.scalarString(value, "name")
		case "namespace":
			x.Namespace = c.scalarString(value, "namespace")
		case "prefix":
			x.Prefix = c.scalarString(value, "prefix")
		case "attribute":
			x.Attribute = c.scalarBool(value, "attribute")
		case "wrapped":
			x.Wrapped = c.scalarBool(value, "wrapped")
		}
	})
	x.Extensions = nodeExtensions(m)
	return x
}
