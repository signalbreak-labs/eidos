package transformer

import (
	"sort"
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// schemaIRFromSpecRecursive converts a normalized SchemaSpec into an ir.SchemaIR
// node, recursing into array items, object properties, and additionalProperties
// so that a managed-resource schema built from a response body carries the full
// shape rather than only the shallow primitive mapping schemaIRFromSpec produces.
//
// Primitives map to the corresponding ir primitive type. Arrays map to a List
// (or Set when uniqueItems is set) whose ElementType recurses. Objects with
// properties map to an object-like SchemaIR (Attributes) whose nested attributes
// are read-only (Computed); objects with additionalProperties map to a Map
// collection. Anything unrecognized falls back to Dynamic so the generator still
// emits a valid attribute instead of failing (fail-loud is the job of the
// parser/transformer diagnostics, not the schema renderer).
func schemaIRFromSpecRecursive(spec SchemaSpec) ir.SchemaIR {
	// Union composition (oneOf/anyOf) wins over the concrete type: the variants
	// carry the alternative shapes (D1). Variant names come from the $ref the
	// variant resolved to (RefName), so split-resources and the discriminator
	// mapping can address them.
	if len(spec.OneOf) > 0 || len(spec.AnyOf) > 0 {
		union := &ir.UnionType{Kind: ir.OneOf}
		members := spec.OneOf
		if len(members) == 0 {
			union.Kind = ir.AnyOf
			members = spec.AnyOf
		}
		for _, m := range members {
			variant := schemaIRFromSpecRecursive(m)
			variant.Name = m.RefName
			union.Variants = append(union.Variants, variant)
		}
		if spec.Discriminator != nil {
			union.Discriminator = &ir.DiscriminatorIR{
				PropertyName: spec.Discriminator.PropertyName,
				Mapping:      spec.Discriminator.Mapping,
			}
		}
		return ir.SchemaIR{Union: union}
	}
	switch strings.ToLower(strings.TrimSpace(spec.Type)) {
	case "string":
		return ir.SchemaIR{Type: ir.TypeString, Format: spec.Format}
	case "integer":
		return ir.SchemaIR{Type: ir.TypeInt, Format: spec.Format}
	case "number":
		return ir.SchemaIR{Type: ir.TypeFloat, Format: spec.Format}
	case "boolean":
		return ir.SchemaIR{Type: ir.TypeBool}
	case "array":
		if spec.Items == nil {
			return ir.SchemaIR{Type: ir.TypeDynamic}
		}
		elem := schemaIRFromSpecRecursive(*spec.Items)
		kind := ir.List
		if spec.UniqueItems {
			kind = ir.Set
		}
		return ir.SchemaIR{Collection: &ir.CollectionType{Kind: kind, ElementType: elem}}
	case "object", "":
		if len(spec.Properties) > 0 {
			return ir.SchemaIR{Attributes: nestedAttributesFromSpec(spec)}
		}
		if spec.AdditionalProperties != nil {
			elem := schemaIRFromSpecRecursive(*spec.AdditionalProperties)
			return ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.Map, ElementType: elem}}
		}
		return ir.SchemaIR{Type: ir.TypeDynamic}
	}
	return ir.SchemaIR{Type: ir.TypeDynamic}
}

// nestedAttributesFromSpec builds the nested attribute list for an object-typed
// SchemaSpec. Nested attributes are read-only (Computed) by default; a managed
// resource's writable nested surface is reconciled against the request body
// after the fact by reconcileNestedRequestFlags, which promotes request-required
// nested fields to Required/Optional so the practitioner can supply them on
// create. Deeper nesting that the request does not carry stays Computed
// (server-managed).
func nestedAttributesFromSpec(spec SchemaSpec) []ir.AttributeIR {
	names := make([]string, 0, len(spec.Properties))
	for name := range spec.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	attrs := make([]ir.AttributeIR, 0, len(names))
	for _, name := range names {
		prop := spec.Properties[name]
		attrs = append(attrs, ir.AttributeIR{
			Name:     SanitizeAttributeName(name),
			WireName: name,
			Schema:   schemaIRFromSpecRecursive(prop),
			Computed: true,
		})
	}
	return attrs
}

// reconcileNestedRequestFlags walks the nested attributes within a managed
// resource attribute's schema and re-derives their Required/Optional/Computed
// flags from the matching request-body property, mirroring the top-level
// reconciliation in ManagedResourceSchema (G18). nestedAttributesFromSpec marks
// every nested attribute Computed, which is correct for response-only shapes but
// wrong for objects the practitioner must supply on create: a request-required
// nested field becomes Required, a request-optional field becomes Optional, and
// only request-absent fields stay Computed. The type structure is unchanged; only
// the flags are corrected.
//
// responseEchoes reports whether the response also returns this attribute (and
// thus its nested children). When true (the attribute is in the state shape), an
// optional request field is Optional+Computed: the practitioner may set it and the
// provider may repopulate it after apply (G18). When false (a request-only
// attribute the response does not echo), an optional request field is Optional
// only — the server never returns it, so Computed would leave the framework
// expecting a value the provider cannot supply, causing inconsistency after
// apply. Required fields are Required either way; request-absent fields are
// Computed only when the response echoes the parent (otherwise they cannot
// occur, since a request-only attribute's children all come from the request).
//
// requestProp is the request body's version of the attribute; a requestProp with
// no Properties/Items means the request does not carry nested shape for this
// attribute, so its nested children keep their existing flags.
func reconcileNestedRequestFlags(s *ir.SchemaIR, requestProp SchemaSpec, responseEchoes bool) {
	if s == nil {
		return
	}
	// Descend through collections to the element schema, matching the request
	// property's items (array) or additionalProperties (map). A collection
	// element inherits the parent's response-echo status: the response returns
	// the element only if it returns the enclosing collection.
	if s.Collection != nil {
		if s.Collection.Kind == ir.Map {
			if requestProp.AdditionalProperties != nil {
				reconcileNestedRequestFlags(&s.Collection.ElementType, *requestProp.AdditionalProperties, responseEchoes)
			}
			return
		}
		if requestProp.Items != nil {
			reconcileNestedRequestFlags(&s.Collection.ElementType, *requestProp.Items, responseEchoes)
		}
		return
	}
	if len(s.Attributes) == 0 || len(requestProp.Properties) == 0 {
		return
	}
	reqProps := make(map[string]struct{}, len(requestProp.Properties))
	for n := range requestProp.Properties {
		reqProps[n] = struct{}{}
	}
	reqRequired := make(map[string]bool, len(requestProp.Required))
	for _, n := range requestProp.Required {
		reqRequired[n] = true
	}
	for i := range s.Attributes {
		a := &s.Attributes[i]
		_, inReq := reqProps[a.WireName]
		switch {
		case reqRequired[a.WireName]:
			a.Required = true
			a.Optional = false
			a.Computed = false
		case inReq:
			a.Optional = true
			a.Required = false
			// When the response also returns this field, the provider may
			// repopulate it after apply (G18). A request-only attribute's child
			// is never server-populated, so it stays Optional only.
			a.Computed = responseEchoes
		default:
			// A request-absent nested field is server-managed only when the
			// response returns the parent; a request-only attribute has no such
			// children (they all come from the request), so this arm is unreachable
			// there but kept defensive.
			a.Computed = responseEchoes
			a.Optional = false
			a.Required = false
		}
		// Recurse into the child's nested schema against the request's child,
		// propagating the response-echo status.
		if child, ok := requestProp.Properties[a.WireName]; ok {
			reconcileNestedRequestFlags(&a.Schema, child, responseEchoes)
		}
	}
}

// applyManagedAttributeFlags sets the Required/Optional/Computed flags on a
// managed-resource attribute derived from a response property, reconciling it
// against the create request body so writable inputs stay writable and
// server-assigned identifiers stay Computed. inRequest reports whether the
// property appears in the create request body; requestRequired is the request
// body's required-property set; requestSpec is the create request body schema
// (nil when there is no request body), used to reconcile nested children. It
// returns true when the attribute is the resource identifier ("id"), so the
// caller can track whether the state shape already carries an id.
func applyManagedAttributeFlags(
	attr *ir.AttributeIR,
	name, snake string,
	inRequest bool,
	requestRequired map[string]bool,
	requestSpec *SchemaSpec,
) bool {
	// State-shape attributes are always echoed by the response, so an optional
	// input the response also returns is Optional+Computed: the practitioner may
	// set it, and the provider may populate it from the server (a create/read
	// response that carries the field must not be "inconsistent after apply" just
	// because the practitioner left it unset) (G18).
	switch {
	case requestRequired[name]:
		attr.Required = true
	case inRequest:
		attr.Optional = true
		attr.Computed = true
	default:
		attr.Computed = true
	}
	// A server-assigned identifier is Computed even when the request body
	// happens to list it; the provider must not require the practitioner to
	// supply the id on create. A practitioner-set identifier — one the create
	// request body declares — keeps its Required/Optional semantics so
	// resources whose name is set on create are distinguished from
	// server-assigned ids (REMAINING_GAPS §3/#11).
	if (snake == "id" || name == "id") && !inRequest && !requestRequired[name] {
		attr.Required = false
		attr.Optional = false
		attr.Computed = true
	}
	// Reconcile nested children against the request body so writable nested
	// fields (required/optional in the request body) are Required/Optional
	// rather than unconditionally Computed. Only attributes the request body
	// carries can expose writable nested fields; response-only attributes keep
	// their Computed nested children (server-managed).
	if requestSpec != nil {
		if reqProp, ok := requestSpec.Properties[name]; ok {
			reconcileNestedRequestFlags(&attr.Schema, reqProp, true)
		}
	}
	return snake == "id" || name == "id"
}

// ManagedResourceSchema builds the object schema for a managed resource inferred
// from a complete CRUD group, plus the name of the attribute that identifies an
// instance (the resource's ID attribute). The state shape is taken from the Read
// response (the canonical representation of a single instance); when the Read
// response has no body it falls back to the Create response, then the Create
// request body. Each top-level attribute is reconciled against the Create request
// body so writable inputs are Required/Optional and server-assigned fields are
// Computed.
//
// The returned idAttribute is "" when the state shape has an "id" property (the
// generator defaults the ID attribute to "id"); otherwise it is the path-derived
// identifier name from the CRUD group's IDInfo, and a synthetic Computed string
// attribute of that name is added so the generator's ID-field lookup succeeds and
// wired Read/Delete path substitution resolves.
func ManagedResourceSchema(c ResourceCRUD) (ir.ObjectSchemaIR, string) {
	stateSpec := resourceStateSpec(c)

	// A top-level oneOf/anyOf response (the instance itself is a union, e.g.
	// Pet = oneOf[Cat, Dog]) cannot flatten into a concrete attribute set.
	// Surface it as a single Computed wrapper attribute carrying the union (D1)
	// — named for the schema's $ref — so the dynamic-union strategy renders it
	// as a discriminated SingleNestedAttribute and split-resources can address
	// it by schema name.
	if union := topLevelUnionSpec(stateSpec); union != nil {
		return ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{unionWrapperAttribute(*union)}}, ""
	}

	idAttribute := strings.TrimSpace(c.ID.AttributeName)
	if idAttribute == "" {
		idAttribute = "id"
	}

	if stateSpec == nil || len(stateSpec.Properties) == 0 {
		// No response or request body to derive a schema from: the resource has
		// no fields to populate, so an identifier attribute here would be a
		// synthetic placeholder that path substitution would fill with an
		// unpopulated (null) value — a dishonest wired body. Return an empty
		// schema with no identifier so the resource stays honestly scaffolded
		// rather than wiring with an unpopulated id (REMAINING_GAPS §3/#12).
		return ir.ObjectSchemaIR{}, ""
	}

	requestSpec := (*SchemaSpec)(nil)
	if c.Create != nil && c.Create.RequestSchema != nil {
		requestSpec = c.Create.RequestSchema
	}
	formData := createFormDataParams(c.Create)
	requestProps, requestRequired := requestPropertySets(requestSpec, formData)

	names := make([]string, 0, len(stateSpec.Properties))
	for name := range stateSpec.Properties {
		names = append(names, name)
	}
	sort.Strings(names)

	attrs := make([]ir.AttributeIR, 0, len(names))
	hasID := false
	for _, name := range names {
		prop := stateSpec.Properties[name]
		snake := SanitizeAttributeName(name)
		attr := ir.AttributeIR{
			Name:     snake,
			WireName: name,
			Schema:   schemaIRFromSpecRecursive(prop),
		}
		_, inRequest := requestProps[name]
		if applyManagedAttributeFlags(&attr, name, snake, inRequest, requestRequired, requestSpec) {
			hasID = true
		}
		attrs = append(attrs, attr)
	}

	// formData params not present in the response shape are write-only inputs;
	// emit them as Required/Optional attributes so the wired form-encoded create
	// body has a model field to read. They are never Computed (the server does
	// not return them). A non-primitive formData type (e.g. file upload) maps to
	// Dynamic; the generator treats a Dynamic formData attribute as unmapped and
	// keeps the operation honestly scaffolded, so the body is not wired with a
	// field it cannot form-encode (REMAINING_GAPS §2). The transformer emits a
	// fail-loud warning for non-primitive formData so it is not silently dropped.
	attrs = appendFormDataOnlyAttributes(attrs, formData, requestRequired)
	// JSON request-body inputs the response does not echo are write-only inputs;
	// emit them as Required/Optional attributes so the wired create body has a
	// model field to read (G9). Without this, a resource whose read response
	// wraps its payload (e.g. library elements: {result: {...}}) exposes no
	// writable attributes and cannot be configured.
	attrs = appendRequestOnlyAttributes(attrs, requestSpec, requestRequired, stateSpec)

	resolvedID := idAttribute
	if hasID {
		// The state shape carries an "id" property; let the generator default to
		// "id" rather than the path-parameter name (e.g. "pet_id" for {petId}),
		// which would not match the response field and would disable wiring. The
		// id attribute already in attrs carries the response field's real type.
		resolvedID = ""
	} else {
		// No "id" property in the state shape. When the path-parameter name (e.g.
		// "username" for {username}) is itself a top-level response property, that
		// real attribute already supplies the identifier — do not add a duplicate
		// synthetic. Only when no attribute with the identifier name exists do we
		// add a synthetic Computed string so path substitution can resolve against
		// an identifier populated via the create request or a Location header.
		alreadyPresent := false
		for _, a := range attrs {
			if a.Name == idAttribute {
				alreadyPresent = true
				break
			}
		}
		if !alreadyPresent {
			attrs = append(attrs, ir.AttributeIR{
				Name:     idAttribute,
				Schema:   ir.SchemaIR{Type: ir.TypeString},
				Computed: true,
			})
		}
	}

	sort.Slice(attrs, func(i, j int) bool { return attrs[i].Name < attrs[j].Name })
	return ir.ObjectSchemaIR{Attributes: attrs}, resolvedID
}

// createFormDataParams returns the create operation's formData parameters
// (OpenAPI 2.0 form-encoded request body fields). formData parameters are the
// create body when a spec declares no JSON request body; surfacing them lets
// ManagedResourceSchema emit them as writable schema attributes the wired
// create body sends as application/x-www-form-urlencoded (REMAINING_GAPS §2).
func createFormDataParams(op *Operation) []Parameter {
	if op == nil {
		return nil
	}
	var form []Parameter
	for _, p := range op.Parameters {
		if strings.EqualFold(p.In, "formData") {
			form = append(form, p)
		}
	}
	if len(form) == 0 {
		// OpenAPI 2.0 formData parameters are normalized by the v2 parser into a
		// request-body content schema (they are dropped from op.Parameters), so a
		// form/multipart request body's object schema must be decomposed back into
		// per-field parameters for the resource schema to surface them as writable
		// inputs (PROJECT_DESIGN §23).
		if kind := RequestBodyKind(op.RequestMediaType); kind == "form" || kind == "multipart" {
			form = formDataParamsFromRequestSchema(op.RequestSchema)
		}
	}
	return form
}

// formDataParamsFromRequestSchema decomposes a form/multipart request body
// object schema back into per-field parameters (In: formData), preserving the
// declared Required set. The v2 parser builds this schema from the operation's
// in: formData parameters, so this reverses the normalization for downstream
// schema construction.
func formDataParamsFromRequestSchema(spec *SchemaSpec) []Parameter {
	if spec == nil || len(spec.Properties) == 0 {
		return nil
	}
	required := make(map[string]bool, len(spec.Required))
	for _, name := range spec.Required {
		required[name] = true
	}
	names := make([]string, 0, len(spec.Properties))
	for name := range spec.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]Parameter, 0, len(names))
	for _, name := range names {
		out = append(out, Parameter{
			Name:     name,
			In:       "formData",
			Required: required[name],
			Type:     spec.Properties[name].Type,
		})
	}
	return out
}

// appendFormDataOnlyAttributes appends write-only schema attributes for formData
// parameters that are not already present (from the response shape) in attrs.
// Each is Required or Optional per the create request's required set and never
// Computed, so the wired form-encoded create body has a model field to read
// (REMAINING_GAPS §2). It returns the appended-to slice.
func appendFormDataOnlyAttributes(attrs []ir.AttributeIR, formData []Parameter, requestRequired map[string]bool) []ir.AttributeIR {
	presentNames := make(map[string]bool, len(attrs))
	for _, a := range attrs {
		presentNames[a.Name] = true
	}
	for _, p := range formData {
		snake := ToSnakeCase(p.Name)
		if presentNames[snake] {
			continue
		}
		attr := ir.AttributeIR{
			Name:   snake,
			Schema: schemaIRFromSpecRecursive(SchemaSpec{Type: p.Type}),
		}
		if requestRequired[p.Name] {
			attr.Required = true
		} else {
			attr.Optional = true
		}
		attrs = append(attrs, attr)
		presentNames[snake] = true
	}
	return attrs
}

// appendRequestOnlyAttributes adds JSON request-body inputs that the response
// does not echo as Required/Optional attributes, so a resource whose read
// response wraps its payload still exposes its create inputs (G9). The
// attributes are never Computed (the server does not return them).
func appendRequestOnlyAttributes(attrs []ir.AttributeIR, requestSpec *SchemaSpec, requestRequired map[string]bool, stateSpec *SchemaSpec) []ir.AttributeIR {
	if requestSpec == nil {
		return attrs
	}
	presentNames := make(map[string]bool, len(attrs))
	for _, a := range attrs {
		presentNames[a.Name] = true
	}
	names := make([]string, 0, len(requestSpec.Properties))
	for name := range requestSpec.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, inState := stateSpec.Properties[name]; inState {
			continue // already reconciled from the state shape
		}
		snake := SanitizeAttributeName(name)
		if presentNames[snake] {
			continue
		}
		attr := ir.AttributeIR{
			Name:     snake,
			WireName: name,
			Schema:   schemaIRFromSpecRecursive(requestSpec.Properties[name]),
		}
		if requestRequired[name] {
			attr.Required = true
		} else {
			attr.Optional = true
		}
		// Reconcile nested children against the request body. A request-only
		// attribute is not echoed by the response, so its optional nested inputs
		// are Optional only (not Computed): the server never returns them, so
		// marking them Computed would leave the framework expecting a value the
		// provider cannot supply. Required nested inputs become Required.
		reconcileNestedRequestFlags(&attr.Schema, requestSpec.Properties[name], false)
		attrs = append(attrs, attr)
		presentNames[snake] = true
	}
	return attrs
}

// requestPropertySets builds the request-property membership and required
// sets reconciled against the response schema: the Create request body's
// properties, plus formData parameters (OpenAPI 2.0 form-encoded create
// bodies), which are practitioner inputs the wired create body sends as
// application/x-www-form-urlencoded (REMAINING_GAPS §2) — a formData param
// also present in the response keeps Required/Optional (write) semantics
// instead of Computed, and a formData-only param is emitted as a write-only
// attribute.
func requestPropertySets(requestSpec *SchemaSpec, formData []Parameter) (map[string]struct{}, map[string]bool) {
	requestProps := map[string]struct{}{}
	requestRequired := map[string]bool{}
	if requestSpec != nil {
		for name := range requestSpec.Properties {
			requestProps[name] = struct{}{}
		}
		for _, name := range requestSpec.Required {
			requestRequired[name] = true
		}
	}
	for _, p := range formData {
		requestProps[p.Name] = struct{}{}
		if p.Required {
			requestRequired[p.Name] = true
		}
	}
	return requestProps, requestRequired
}

// topLevelUnionSpec returns spec when it is a non-nil top-level union schema
// (carries oneOf or anyOf variants), or nil otherwise.
func topLevelUnionSpec(spec *SchemaSpec) *SchemaSpec {
	if spec == nil || (len(spec.OneOf) == 0 && len(spec.AnyOf) == 0) {
		return nil
	}
	return spec
}

// unionWrapperAttribute returns the Computed wrapper attribute synthesized for
// a top-level union schema: named for the schema's $ref (snake_cased, "value"
// when inline), carrying the full union (variants + discriminator) so the
// dynamic-union and split-resources strategies can render or split it.
func unionWrapperAttribute(spec SchemaSpec) ir.AttributeIR {
	name := ToSnakeCase(spec.RefName)
	if name == "" {
		name = "value"
	}
	schema := schemaIRFromSpecRecursive(spec)
	// Carry the component schema name (e.g. "Pet") so SelectStrategy can match
	// per-oneOf generator.yaml overrides and SplitResources can name the split
	// base. Computed marks the wrapper as an output shape, so the dynamic-union
	// merge renders its children Computed as well (a Computed parent cannot
	// have Required children).
	schema.Name = spec.RefName
	schema.Computed = true
	return ir.AttributeIR{
		Name:     name,
		Computed: true,
		Schema:   schema,
	}
}

// ObjectSchemaFromSpec maps an object SchemaSpec's properties to an
// ObjectSchemaIR, one attribute per property (snake_cased name, recursively
// mapped type, Computed). It is used for shapes that are outputs by
// construction — the list resource identity/resource schemas derived from a
// collection endpoint's item type (F1). A nil spec or one with no properties
// yields an empty schema. Attributes are sorted by name for deterministic
// generation.
func ObjectSchemaFromSpec(spec *SchemaSpec) ir.ObjectSchemaIR {
	if spec == nil || len(spec.Properties) == 0 {
		return ir.ObjectSchemaIR{}
	}
	names := make([]string, 0, len(spec.Properties))
	for name := range spec.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	attrs := make([]ir.AttributeIR, 0, len(names))
	for _, name := range names {
		attrs = append(attrs, ir.AttributeIR{
			Name:     SanitizeAttributeName(name),
			Computed: true,
			Schema:   schemaIRFromSpecRecursive(spec.Properties[name]),
		})
	}
	return ir.ObjectSchemaIR{Attributes: attrs}
}

// DataSourceSchema builds the object schema for a data source inferred from a
// read operation (REMAINING_GAPS §4). Path, query, and header parameters become
// practitioner-set input attributes — Required for path parameters and required
// query/header parameters, Optional otherwise — and the response body's
// properties become Computed output attributes. When a parameter and a response
// property share a snake_case name the attribute is both an input (Required or
// Optional) and an output (Computed): the same attribute identifies the instance
// to read and carries the refreshed value, so a path such as GET /pets/{id} whose
// response also exposes `id` yields a single Required+Computed `id` attribute.
//
// The response property's type wins over the parameter's when they collide,
// because the model field is populated from the response and only stringified
// into the request URL. Attributes are sorted by name for deterministic
// generation. The schema alone does not decide wiring: the generator resolves the
// read mapping against these attributes and only wires when every path
// placeholder and required query/header parameter maps to a primitive attribute
// and at least one Computed output attribute is present (i.e. the response is a
// single object, not an array — array responses are a documented follow-up).
func DataSourceSchema(op Operation) ir.ObjectSchemaIR {
	attrs := map[string]ir.AttributeIR{}
	upsert := func(name string, mutate func(*ir.AttributeIR)) {
		a := attrs[name]
		a.Name = name
		mutate(&a)
		attrs[name] = a
	}

	// Input attributes from path/query/header parameters. Path parameters are
	// always required: the instance cannot be identified without them.
	for _, p := range op.Parameters {
		switch strings.ToLower(p.In) {
		case "path", "query", "header":
		default:
			continue
		}
		snake := ToSnakeCase(p.Name)
		upsert(snake, func(a *ir.AttributeIR) {
			a.Schema = paramSchemaIR(p.Type)
			a.WireName = p.Name
			if p.Required || strings.EqualFold(p.In, "path") {
				a.Required = true
				a.Optional = false
			} else {
				a.Optional = true
			}
		})
	}

	// Output attributes from the response body. A response property that shares
	// its name with a parameter attribute contributes the response type and the
	// Computed flag without clearing the input's Required/Optional, so the
	// attribute is both a practitioner-set filter and a refreshed output. A
	// Required input that the response also returns stays Required (the
	// practitioner supplies the value; the response echoes it) — never
	// Required+Computed, which the framework rejects (G15).
	if op.ResponseSchema != nil && len(op.ResponseSchema.Properties) > 0 {
		for propName, prop := range op.ResponseSchema.Properties {
			snake := SanitizeAttributeName(propName)
			upsert(snake, func(a *ir.AttributeIR) {
				a.Schema = schemaIRFromSpecRecursive(prop)
				a.WireName = propName
				if !a.Required {
					a.Computed = true
				}
			})
		}
	}

	// A top-level oneOf/anyOf response (the read returns a union instance, e.g.
	// Pet = oneOf[Cat, Dog]) carries no properties; surface it as a single
	// Computed wrapper attribute carrying the union (D1) so the dynamic-union
	// strategy renders it as a discriminated SingleNestedAttribute.
	if op.ResponseSchema != nil && (len(op.ResponseSchema.OneOf) > 0 || len(op.ResponseSchema.AnyOf) > 0) {
		wrapper := unionWrapperAttribute(*op.ResponseSchema)
		upsert(wrapper.Name, func(a *ir.AttributeIR) {
			a.Schema = wrapper.Schema
			a.Computed = true
		})
	}

	// An array response (a list endpoint) carries no object properties; instead
	// it contributes a single Computed `items` List attribute whose element type
	// is the array's item schema, so a list data source exposes the collection as
	// state and the generator can wire its Read to fetch (and paginate) the pages
	// (REMAINING_GAPS §2/§4). Object-wrapped arrays (e.g. {"items": [...],
	// "next": ...}) are not modeled here: their container path is ambiguous
	// without a convention, so they fall through to the object-property branch
	// above when the wrapper object's properties are present, and otherwise stay
	// honestly scaffolded.
	if op.ResponseSchema != nil && strings.EqualFold(op.ResponseSchema.Type, "array") && op.ResponseSchema.Items != nil {
		elem := schemaIRFromSpecRecursive(*op.ResponseSchema.Items)
		// uniqueItems: true on the array response yields a Set attribute; the
		// framework models unordered, de-duplicated collections as a Set.
		kind := ir.List
		if op.ResponseSchema.UniqueItems {
			kind = ir.Set
		}
		upsert("items", func(a *ir.AttributeIR) {
			a.Schema = ir.SchemaIR{
				Collection: &ir.CollectionType{
					Kind:        kind,
					ElementType: elem,
				},
			}
			a.Computed = true
		})
	}

	out := make([]ir.AttributeIR, 0, len(attrs))
	for _, a := range attrs {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return ir.ObjectSchemaIR{Attributes: out}
}

// ListResourceConfigSchema builds the config (filter) schema for a list
// resource from the collection path's path, query, and header parameters — the
// same input-attribute mapping DataSourceSchema applies to read operations. Path
// parameters are Required (a collection GET cannot stream matching instances
// without them); required query/header parameters stay Required, and optional
// parameters are Optional. Without these input attributes a list resource whose
// collection path carries parameters could never resolve its path substitutions
// against the config schema and would stay an honest scaffold even when the
// response schema is present (PROJECT_DESIGN §23).
func ListResourceConfigSchema(op Operation) ir.ObjectSchemaIR {
	attrs := make([]ir.AttributeIR, 0, len(op.Parameters))
	for _, p := range op.Parameters {
		switch strings.ToLower(p.In) {
		case "path", "query", "header":
		default:
			continue
		}
		attr := ir.AttributeIR{
			Name:   SanitizeAttributeName(p.Name),
			Schema: paramSchemaIR(p.Type),
		}
		if p.Required || strings.EqualFold(p.In, "path") {
			attr.Required = true
		} else {
			attr.Optional = true
		}
		attrs = append(attrs, attr)
	}
	sort.Slice(attrs, func(i, j int) bool { return attrs[i].Name < attrs[j].Name })
	return ir.ObjectSchemaIR{Attributes: attrs}
}

// paramSchemaIR maps a parameter's declared scalar type to an ir primitive
// schema. Parameters without a recognized scalar type default to string, which
// is how path/query/header values are serialized into the request URL regardless
// of the declared type, so the generated attribute is always usable as a filter.
func paramSchemaIR(typeStr string) ir.SchemaIR {
	switch strings.ToLower(strings.TrimSpace(typeStr)) {
	case "integer":
		return ir.SchemaIR{Type: ir.TypeInt}
	case "number":
		return ir.SchemaIR{Type: ir.TypeFloat}
	case "boolean":
		return ir.SchemaIR{Type: ir.TypeBool}
	default:
		return ir.SchemaIR{Type: ir.TypeString}
	}
}

// resourceStateSpec selects the SchemaSpec that best represents a single resource
// instance: the Read response body, then the Create response body, then the
// Create request body. Returns nil when none carry a schema.
func resourceStateSpec(c ResourceCRUD) *SchemaSpec {
	if c.Read != nil && c.Read.ResponseSchema != nil {
		return c.Read.ResponseSchema
	}
	if c.Create != nil && c.Create.ResponseSchema != nil {
		return c.Create.ResponseSchema
	}
	if c.Create != nil && c.Create.RequestSchema != nil {
		return c.Create.RequestSchema
	}
	return nil
}
