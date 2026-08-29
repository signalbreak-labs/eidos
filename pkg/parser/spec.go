// Package parser provides a dedicated in-house OpenAPI parser for Eidos.
//
// The parser normalizes OpenAPI 2.0 (Swagger), 3.0.x, and 3.1.x documents into
// a single version-agnostic Spec model that downstream phases (normalizer,
// transformer, generator) consume. Every node carries SourceLocation metadata
// so diagnostics can pinpoint the originating file and line.
package parser

import (
	"encoding/json"
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
)

// SourceLocation tracks where a node originated in the source document.
// It is attached to every model node to enable precise diagnostics.
//
// This is an alias for diagnostics.SourceLocation so that parser-produced
// locations can be used directly with the shared diagnostics model without
// manual conversion.
type SourceLocation = diagnostics.SourceLocation

// Spec is a version-agnostic representation of an OpenAPI document.
// It intentionally mirrors the union of OpenAPI 2.0, 3.0.x, and 3.1.x
// top-level fields so that version-specific converters can populate one
// shared model.
type Spec struct {
	OpenAPI           string                `json:"openapi,omitempty"`
	Swagger           string                `json:"swagger,omitempty"`
	JSONSchemaDialect string                `json:"jsonSchemaDialect,omitempty"`
	Info              *Info                 `json:"info,omitempty"`
	Servers           []Server              `json:"servers,omitempty"`
	Paths             map[string]*PathItem  `json:"paths,omitempty"`
	Webhooks          map[string]*PathItem  `json:"webhooks,omitempty"`
	Components        *Components           `json:"components,omitempty"`
	Security          []SecurityRequirement `json:"security,omitempty"`
	Tags              []Tag                 `json:"tags,omitempty"`
	ExternalDocs      *ExternalDocs         `json:"externalDocs,omitempty"`
	SourceLocation    SourceLocation        `json:"sourceLocation,omitempty"`
	Extensions        map[string]any        `json:"-"`
	localRefs         *localRefResolver
}

// Info corresponds to the OpenAPI info object.
type Info struct {
	Title          string         `json:"title,omitempty"`
	Summary        string         `json:"summary,omitempty"`
	Description    string         `json:"description,omitempty"`
	TermsOfService string         `json:"termsOfService,omitempty"`
	Contact        *Contact       `json:"contact,omitempty"`
	License        *License       `json:"license,omitempty"`
	Version        string         `json:"version,omitempty"`
	SourceLocation SourceLocation `json:"sourceLocation,omitempty"`
	Extensions     map[string]any `json:"-"`
}

// Contact corresponds to the OpenAPI contact object.
type Contact struct {
	Name           string         `json:"name,omitempty"`
	URL            string         `json:"url,omitempty"`
	Email          string         `json:"email,omitempty"`
	SourceLocation SourceLocation `json:"sourceLocation,omitempty"`
	Extensions     map[string]any `json:"-"`
}

// License corresponds to the OpenAPI license object.
// Identifier is used by OpenAPI 3.1; URL is used by earlier versions.
type License struct {
	Name           string         `json:"name,omitempty"`
	Identifier     string         `json:"identifier,omitempty"`
	URL            string         `json:"url,omitempty"`
	SourceLocation SourceLocation `json:"sourceLocation,omitempty"`
	Extensions     map[string]any `json:"-"`
}

// Server corresponds to the OpenAPI server object.
type Server struct {
	URL            string                     `json:"url,omitempty"`
	Description    string                     `json:"description,omitempty"`
	Variables      map[string]*ServerVariable `json:"variables,omitempty"`
	SourceLocation SourceLocation             `json:"sourceLocation,omitempty"`
	Extensions     map[string]any             `json:"-"`
}

// ServerVariable corresponds to the OpenAPI server variable object.
type ServerVariable struct {
	Enum           []string       `json:"enum,omitempty"`
	Default        string         `json:"default,omitempty"`
	Description    string         `json:"description,omitempty"`
	SourceLocation SourceLocation `json:"sourceLocation,omitempty"`
	Extensions     map[string]any `json:"-"`
}

// PathItem corresponds to the OpenAPI path item object.
type PathItem struct {
	Ref            string         `json:"$ref,omitempty"`
	Summary        string         `json:"summary,omitempty"`
	Description    string         `json:"description,omitempty"`
	Get            *Operation     `json:"get,omitempty"`
	Put            *Operation     `json:"put,omitempty"`
	Post           *Operation     `json:"post,omitempty"`
	Delete         *Operation     `json:"delete,omitempty"`
	Options        *Operation     `json:"options,omitempty"`
	Head           *Operation     `json:"head,omitempty"`
	Patch          *Operation     `json:"patch,omitempty"`
	Trace          *Operation     `json:"trace,omitempty"`
	Servers        []Server       `json:"servers,omitempty"`
	Parameters     []Parameter    `json:"parameters,omitempty"`
	SourceLocation SourceLocation `json:"sourceLocation,omitempty"`
	Extensions     map[string]any `json:"-"`
}

// Operation corresponds to the OpenAPI operation object.
type Operation struct {
	Tags         []string              `json:"tags,omitempty"`
	Summary      string                `json:"summary,omitempty"`
	Description  string                `json:"description,omitempty"`
	ExternalDocs *ExternalDocs         `json:"externalDocs,omitempty"`
	OperationID  string                `json:"operationId,omitempty"`
	Parameters   []Parameter           `json:"parameters,omitempty"`
	RequestBody  *RequestBody          `json:"requestBody,omitempty"`
	Responses    map[string]*Response  `json:"responses,omitempty"`
	Callbacks    map[string]Callback   `json:"callbacks,omitempty"`
	Deprecated   bool                  `json:"deprecated,omitempty"`
	Security     []SecurityRequirement `json:"security,omitempty"`
	Servers      []Server              `json:"servers,omitempty"`
	// Schemes holds a Swagger 2.0 operation-level transport-scheme override
	// (e.g. ["https"]). OpenAPI 3.x has no operation-level schemes; the field is
	// only populated by the v2 parser. The pipeline does not honor per-operation
	// scheme overrides, so a non-empty value that differs from the document
	// schemes is surfaced as a warning at parse time (N-9).
	Schemes        []string       `json:"schemes,omitempty"`
	SourceLocation SourceLocation `json:"sourceLocation,omitempty"`
	Extensions     map[string]any `json:"-"`
}

// Parameter corresponds to the OpenAPI parameter object.
// It covers header, query, path, cookie, and formData parameters.
type Parameter struct {
	Ref             string                `json:"$ref,omitempty"`
	Name            string                `json:"name,omitempty"`
	In              string                `json:"in,omitempty"`
	Description     string                `json:"description,omitempty"`
	Required        bool                  `json:"required,omitempty"`
	Deprecated      bool                  `json:"deprecated,omitempty"`
	AllowEmptyValue bool                  `json:"allowEmptyValue,omitempty"`
	Schema          *Schema               `json:"schema,omitempty"`
	Content         map[string]*MediaType `json:"content,omitempty"`
	Style           string                `json:"style,omitempty"`
	Explode         bool                  `json:"explode,omitempty"`
	AllowReserved   bool                  `json:"allowReserved,omitempty"`
	Example         any                   `json:"example,omitempty"`
	Examples        map[string]*Example   `json:"examples,omitempty"`
	SourceLocation  SourceLocation        `json:"sourceLocation,omitempty"`
	Extensions      map[string]any        `json:"-"`
}

// RequestBody corresponds to the OpenAPI request body object.
type RequestBody struct {
	Ref            string                `json:"$ref,omitempty"`
	Description    string                `json:"description,omitempty"`
	Content        map[string]*MediaType `json:"content,omitempty"`
	Required       bool                  `json:"required,omitempty"`
	SourceLocation SourceLocation        `json:"sourceLocation,omitempty"`
	Extensions     map[string]any        `json:"-"`
}

// MediaType corresponds to the OpenAPI media type object.
type MediaType struct {
	Schema         *Schema              `json:"schema,omitempty"`
	Example        any                  `json:"example,omitempty"`
	Examples       map[string]*Example  `json:"examples,omitempty"`
	Encoding       map[string]*Encoding `json:"encoding,omitempty"`
	SourceLocation SourceLocation       `json:"sourceLocation,omitempty"`
	Extensions     map[string]any       `json:"-"`
}

// Encoding corresponds to the OpenAPI encoding object.
type Encoding struct {
	ContentType    string             `json:"contentType,omitempty"`
	Headers        map[string]*Header `json:"headers,omitempty"`
	Style          string             `json:"style,omitempty"`
	Explode        bool               `json:"explode,omitempty"`
	AllowReserved  bool               `json:"allowReserved,omitempty"`
	SourceLocation SourceLocation     `json:"sourceLocation,omitempty"`
	Extensions     map[string]any     `json:"-"`
}

// Response corresponds to the OpenAPI response object.
type Response struct {
	Ref            string                `json:"$ref,omitempty"`
	Description    string                `json:"description,omitempty"`
	Headers        map[string]*Header    `json:"headers,omitempty"`
	Content        map[string]*MediaType `json:"content,omitempty"`
	Links          map[string]*Link      `json:"links,omitempty"`
	SourceLocation SourceLocation        `json:"sourceLocation,omitempty"`
	Extensions     map[string]any        `json:"-"`
}

// Header corresponds to the OpenAPI header object.
type Header struct {
	Ref             string                `json:"$ref,omitempty"`
	Description     string                `json:"description,omitempty"`
	Required        bool                  `json:"required,omitempty"`
	Deprecated      bool                  `json:"deprecated,omitempty"`
	AllowEmptyValue bool                  `json:"allowEmptyValue,omitempty"`
	Schema          *Schema               `json:"schema,omitempty"`
	Content         map[string]*MediaType `json:"content,omitempty"`
	Style           string                `json:"style,omitempty"`
	Explode         bool                  `json:"explode,omitempty"`
	AllowReserved   bool                  `json:"allowReserved,omitempty"`
	Example         any                   `json:"example,omitempty"`
	Examples        map[string]*Example   `json:"examples,omitempty"`
	SourceLocation  SourceLocation        `json:"sourceLocation,omitempty"`
	Extensions      map[string]any        `json:"-"`
}

// Example corresponds to the OpenAPI example object.
type Example struct {
	Ref            string         `json:"$ref,omitempty"`
	Summary        string         `json:"summary,omitempty"`
	Description    string         `json:"description,omitempty"`
	Value          any            `json:"value,omitempty"`
	ExternalValue  string         `json:"externalValue,omitempty"`
	SourceLocation SourceLocation `json:"sourceLocation,omitempty"`
	Extensions     map[string]any `json:"-"`
}

// Link corresponds to the OpenAPI link object.
type Link struct {
	Ref            string         `json:"$ref,omitempty"`
	OperationID    string         `json:"operationId,omitempty"`
	OperationRef   string         `json:"operationRef,omitempty"`
	Parameters     map[string]any `json:"parameters,omitempty"`
	RequestBody    any            `json:"requestBody,omitempty"`
	Description    string         `json:"description,omitempty"`
	Server         *Server        `json:"server,omitempty"`
	SourceLocation SourceLocation `json:"sourceLocation,omitempty"`
	Extensions     map[string]any `json:"-"`
}

// Callback corresponds to the OpenAPI callback object.
// OpenAPI represents a callback as a map keyed by runtime expressions
// (for example `{$request.body#/callbackUrl}`), where each value is a
// PathItem, or as a Reference Object `{"$ref": "..."}` pointing to a
// reusable callback in components. To preserve the OpenAPI shape during
// JSON round-trips, Callback is modeled as a map type rather than a
// struct. A `$ref` callback is encoded with the literal key "$ref" and
// a custom MarshalJSON produces the spec-correct flat `{"$ref":"..."}`
// shape when the map contains only a reference PathItem.
//
// Use IsRef to test whether a Callback represents a reference object
// without checking the internal "$ref" key convention directly.
type Callback map[string]*PathItem

// MarshalJSON serializes a Callback. When the callback represents a
// reference object (a single "$ref" key whose PathItem carries only a Ref
// value plus optional source-location metadata), it emits the flat
// OpenAPI Reference Object shape `{"$ref": "..."}`. Otherwise it emits
// the normal runtime-expression-keyed map.
//
// Note: OpenAPI defines a callback as either a Reference Object or a
// runtime-expression-keyed map, never a mix. If a callback contains both a
// "$ref" key and runtime-expression keys, the fallback below marshals the
// full map, which is not valid OpenAPI output. Callers should avoid producing
// such mixed callbacks.
func (cb Callback) MarshalJSON() ([]byte, error) {
	if len(cb) == 1 {
		if item, ok := cb["$ref"]; ok && pathItemIsRefOnly(item) {
			return json.Marshal(map[string]string{"$ref": item.Ref})
		}
	}
	return json.Marshal(map[string]*PathItem(cb))
}

// UnmarshalJSON deserializes a Callback. If the JSON object is a flat
// Reference Object `{"$ref": "..."}`, it is represented internally as
// the map entry `{"$ref": &PathItem{Ref: "..."}}` so that the model
// round-trips without data loss. Otherwise the object is unmarshaled as
// a runtime-expression-keyed PathItem map.
// UnmarshalJSON implements json.Unmarshaler.
func (cb *Callback) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw) == 1 {
		if refRaw, ok := raw["$ref"]; ok {
			var ref string
			if err := json.Unmarshal(refRaw, &ref); err == nil && ref != "" {
				*cb = Callback{"$ref": &PathItem{Ref: ref}}
				return nil
			}
		}
	}
	var m map[string]*PathItem
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	*cb = Callback(m)
	return nil
}

// IsRef reports whether the callback represents a flat Reference Object
// `{"$ref": "..."}` and returns the reference value. Consumers should use
// this helper rather than testing for the literal "$ref" key directly.
func (cb Callback) IsRef() (string, bool) {
	if len(cb) != 1 {
		return "", false
	}
	item, ok := cb["$ref"]
	if !ok || !pathItemIsRefOnly(item) {
		return "", false
	}
	return item.Ref, true
}

// pathItemIsRefOnly reports whether pi carries only a Ref value (and
// optional source-location metadata). It is used when deciding whether a
// Callback can be serialized as the flat OpenAPI Reference Object shape.
//
// Maintenance note: This function must be updated whenever a new field is
// added to PathItem. It returns true only when every non-Ref field is empty,
// so any new field not included here could allow a non-flat $ref serialization.
// The current fields mirror the OpenAPI Path Item Object specification across
// OpenAPI 2.0, 3.0.x, and 3.1.x.
func pathItemIsRefOnly(pi *PathItem) bool {
	if pi == nil || pi.Ref == "" {
		return false
	}
	return pi.Summary == "" &&
		pi.Description == "" &&
		pi.Get == nil && pi.Put == nil && pi.Post == nil && pi.Delete == nil &&
		pi.Options == nil && pi.Head == nil && pi.Patch == nil && pi.Trace == nil &&
		len(pi.Servers) == 0 && len(pi.Parameters) == 0
}

// Components corresponds to the OpenAPI components object.
type Components struct {
	Schemas         map[string]*Schema         `json:"schemas,omitempty"`
	Responses       map[string]*Response       `json:"responses,omitempty"`
	Parameters      map[string]*Parameter      `json:"parameters,omitempty"`
	Examples        map[string]*Example        `json:"examples,omitempty"`
	RequestBodies   map[string]*RequestBody    `json:"requestBodies,omitempty"`
	Headers         map[string]*Header         `json:"headers,omitempty"`
	SecuritySchemes map[string]*SecurityScheme `json:"securitySchemes,omitempty"`
	Links           map[string]*Link           `json:"links,omitempty"`
	Callbacks       map[string]Callback        `json:"callbacks,omitempty"`
	SourceLocation  SourceLocation             `json:"sourceLocation,omitempty"`
	Extensions      map[string]any             `json:"-"`
}

// SecurityScheme corresponds to the OpenAPI security scheme object.
type SecurityScheme struct {
	Ref              string         `json:"$ref,omitempty"`
	Type             string         `json:"type,omitempty"`
	Description      string         `json:"description,omitempty"`
	Name             string         `json:"name,omitempty"`
	In               string         `json:"in,omitempty"`
	Scheme           string         `json:"scheme,omitempty"`
	BearerFormat     string         `json:"bearerFormat,omitempty"`
	Flows            *OAuthFlows    `json:"flows,omitempty"`
	OpenIDConnectURL string         `json:"openIdConnectUrl,omitempty"`
	SourceLocation   SourceLocation `json:"sourceLocation,omitempty"`
	Extensions       map[string]any `json:"-"`
}

// OAuthFlows corresponds to the OpenAPI OAuth flows object.
type OAuthFlows struct {
	Implicit          *OAuthFlow     `json:"implicit,omitempty"`
	Password          *OAuthFlow     `json:"password,omitempty"`
	ClientCredentials *OAuthFlow     `json:"clientCredentials,omitempty"`
	AuthorizationCode *OAuthFlow     `json:"authorizationCode,omitempty"`
	SourceLocation    SourceLocation `json:"sourceLocation,omitempty"`
	Extensions        map[string]any `json:"-"`
}

// OAuthFlow corresponds to a single OpenAPI OAuth flow object.
type OAuthFlow struct {
	AuthorizationURL string            `json:"authorizationUrl,omitempty"`
	TokenURL         string            `json:"tokenUrl,omitempty"`
	RefreshURL       string            `json:"refreshUrl,omitempty"`
	Scopes           map[string]string `json:"scopes,omitempty"`
	SourceLocation   SourceLocation    `json:"sourceLocation,omitempty"`
	Extensions       map[string]any    `json:"-"`
}

// SecurityRequirement is the OpenAPI security requirement object. It is
// wrapped in a struct rather than a plain map alias so that source-location
// metadata can be attached for diagnostics.
//
// Use the Requirements field to access security scheme names and their
// required scopes; the struct wrapper carries SourceLocation for diagnostics.
// SourceLocation is a model-internal field and is not part of the OpenAPI spec;
// it is preserved during JSON round-trips used by this package but should be
// ignored by standard OpenAPI consumers.
type SecurityRequirement struct {
	Requirements   map[string][]string `json:"-"`
	SourceLocation SourceLocation      `json:"sourceLocation,omitempty"`
}

// MarshalJSON flattens the security requirement map so that the JSON shape
// matches the OpenAPI spec (a plain object whose keys are security scheme
// names and whose values are string arrays). SourceLocation is emitted as an
// additional field when present.
//
// Note: when Requirements is empty but SourceLocation is non-zero, the output
// is a JSON object containing only sourceLocation. This preserves internal
// metadata but is not a shape a standard OpenAPI consumer would produce. Empty
// security requirements are unusual; callers that need strict spec output can
// omit the SourceLocation field before serialization.
func (sr SecurityRequirement) MarshalJSON() ([]byte, error) {
	type Alias SecurityRequirement
	b, err := json.Marshal((*Alias)(&sr))
	if err != nil {
		return nil, err
	}
	if len(sr.Requirements) == 0 {
		return b, nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	for k, v := range sr.Requirements {
		m[k] = v
	}
	return json.Marshal(m)
}

// UnmarshalJSON parses a security requirement object. All keys other than
// sourceLocation are treated as security scheme names and collected into
// Requirements.
// UnmarshalJSON implements json.Unmarshaler.
func (sr *SecurityRequirement) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	reqs := make(map[string][]string, len(raw))
	for k, v := range raw {
		if k == "sourceLocation" {
			continue
		}
		var scopes []string
		if err := json.Unmarshal(v, &scopes); err != nil {
			return err
		}
		reqs[k] = scopes
	}
	sr.Requirements = reqs
	if locRaw, ok := raw["sourceLocation"]; ok {
		if err := json.Unmarshal(locRaw, &sr.SourceLocation); err != nil {
			return err
		}
	}
	return nil
}

// Tag corresponds to the OpenAPI tag object.
type Tag struct {
	Name           string         `json:"name,omitempty"`
	Description    string         `json:"description,omitempty"`
	ExternalDocs   *ExternalDocs  `json:"externalDocs,omitempty"`
	SourceLocation SourceLocation `json:"sourceLocation,omitempty"`
	Extensions     map[string]any `json:"-"`
}

// ExternalDocs corresponds to the OpenAPI external documentation object.
type ExternalDocs struct {
	Description    string         `json:"description,omitempty"`
	URL            string         `json:"url,omitempty"`
	SourceLocation SourceLocation `json:"sourceLocation,omitempty"`
	Extensions     map[string]any `json:"-"`
}

// Schema corresponds to the OpenAPI / JSON Schema object.
// It carries the union of fields used across OpenAPI 2.0, 3.0.x, and 3.1.x.
type Schema struct {
	Ref         string `json:"$ref,omitempty"`
	Type        any    `json:"type,omitempty"`
	Format      string `json:"format,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Default     any    `json:"default,omitempty"`
	// Numeric and size constraints are pointers so a declared bound of 0 is
	// distinct from an absent one: `minimum: 0` genuinely forbids negative
	// values and must survive the trip to the generated validators (G39).
	MultipleOf            *float64            `json:"multipleOf,omitempty"`
	Maximum               *float64            `json:"maximum,omitempty"`
	ExclusiveMaximum      any                 `json:"exclusiveMaximum,omitempty"`
	Minimum               *float64            `json:"minimum,omitempty"`
	ExclusiveMinimum      any                 `json:"exclusiveMinimum,omitempty"`
	MaxLength             *int                `json:"maxLength,omitempty"`
	MinLength             *int                `json:"minLength,omitempty"`
	Pattern               string              `json:"pattern,omitempty"`
	MaxItems              *int                `json:"maxItems,omitempty"`
	MinItems              *int                `json:"minItems,omitempty"`
	UniqueItems           bool                `json:"uniqueItems,omitempty"`
	MaxProperties         int                 `json:"maxProperties,omitempty"`
	MinProperties         int                 `json:"minProperties,omitempty"`
	Required              []string            `json:"required,omitempty"`
	Enum                  []any               `json:"enum,omitempty"`
	AllOf                 []*Schema           `json:"allOf,omitempty"`
	OneOf                 []*Schema           `json:"oneOf,omitempty"`
	AnyOf                 []*Schema           `json:"anyOf,omitempty"`
	Not                   *Schema             `json:"not,omitempty"`
	Discriminator         *Discriminator      `json:"discriminator,omitempty"`
	Properties            map[string]*Schema  `json:"properties,omitempty"`
	AdditionalProperties  any                 `json:"additionalProperties,omitempty"`
	PatternProperties     map[string]*Schema  `json:"patternProperties,omitempty"`
	PropertyNames         *Schema             `json:"propertyNames,omitempty"`
	Items                 any                 `json:"items,omitempty"`
	PrefixItems           []*Schema           `json:"prefixItems,omitempty"`
	Contains              *Schema             `json:"contains,omitempty"`
	MinContains           int                 `json:"minContains,omitempty"`
	MaxContains           int                 `json:"maxContains,omitempty"`
	UnevaluatedItems      *Schema             `json:"unevaluatedItems,omitempty"`
	UnevaluatedProperties *Schema             `json:"unevaluatedProperties,omitempty"`
	DependentSchemas      map[string]*Schema  `json:"dependentSchemas,omitempty"`
	DependentRequired     map[string][]string `json:"dependentRequired,omitempty"`
	If                    *Schema             `json:"if,omitempty"`
	Then                  *Schema             `json:"then,omitempty"`
	Else                  *Schema             `json:"else,omitempty"`
	Nullable              bool                `json:"nullable,omitempty"`
	ReadOnly              bool                `json:"readOnly,omitempty"`
	WriteOnly             bool                `json:"writeOnly,omitempty"`
	Deprecated            bool                `json:"deprecated,omitempty"`
	Example               any                 `json:"example,omitempty"`
	Examples              map[string]*Example `json:"examples,omitempty"`
	// ExamplesArray holds the OpenAPI 3.1 / JSON Schema 2020-12 form of the
	// schema-level "examples" keyword: an array of raw values. The 3.0 form is a
	// map of Example objects (Examples); the two forms are mutually exclusive per
	// version, and both are preserved so a valid 3.1 spec is not dropped (L-1).
	ExamplesArray    []any         `json:"examplesArray,omitempty"`
	XML              *XML          `json:"xml,omitempty"`
	ExternalDocs     *ExternalDocs `json:"externalDocs,omitempty"`
	Const            any           `json:"const,omitempty"`
	ContentMediaType string        `json:"contentMediaType,omitempty"`
	ContentEncoding  string        `json:"contentEncoding,omitempty"`
	ContentSchema    *Schema       `json:"contentSchema,omitempty"`
	// Opaque is set by the parser when this schema (or a schema referenced by
	// one of its $refs) participates in a circular reference. Downstream consumers
	// should treat Opaque schemas as opaque reference boundaries rather than
	// expanding them, preventing infinite recursion during generation.
	Opaque         bool           `json:"opaque,omitempty"`
	SourceLocation SourceLocation `json:"sourceLocation,omitempty"`
	Extensions     map[string]any `json:"-"`
}

// Discriminator corresponds to the OpenAPI discriminator object.
type Discriminator struct {
	PropertyName   string            `json:"propertyName,omitempty"`
	Mapping        map[string]string `json:"mapping,omitempty"`
	SourceLocation SourceLocation    `json:"sourceLocation,omitempty"`
	Extensions     map[string]any    `json:"-"`
}

// XML corresponds to the OpenAPI XML object.
type XML struct {
	Name           string         `json:"name,omitempty"`
	Namespace      string         `json:"namespace,omitempty"`
	Prefix         string         `json:"prefix,omitempty"`
	Attribute      bool           `json:"attribute,omitempty"`
	Wrapped        bool           `json:"wrapped,omitempty"`
	SourceLocation SourceLocation `json:"sourceLocation,omitempty"`
	Extensions     map[string]any `json:"-"`
}

// marshalWithExtensions marshals v and then flattens any extension keys on
// top of the resulting JSON object. This lets structs with an Extensions
// field preserve `x-*` vendor extensions during JSON round-trips.
func marshalWithExtensions(v any, extensions map[string]any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(extensions) == 0 {
		return b, nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	for k, v := range extensions {
		m[k] = v
	}
	return json.Marshal(m)
}

// unmarshalExtensions extracts any `x-*` keys from data into the Extensions
// map. It is intended to be called from custom UnmarshalJSON implementations
// after the known fields have already been parsed. The pointer parameter lets
// callers conditionally populate the field only when extensions exist.
//
//nolint:gocritic // pointer-to-map parameter is an intentional API used by all UnmarshalJSON helpers
func unmarshalExtensions(data []byte, extensions *map[string]any) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	ext := make(map[string]any)
	for k, v := range raw {
		if strings.HasPrefix(k, "x-") {
			var val any
			if err := json.Unmarshal(v, &val); err != nil {
				return err
			}
			ext[k] = val
		}
	}
	if len(ext) > 0 {
		*extensions = ext
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (s *Spec) MarshalJSON() ([]byte, error) {
	type Alias Spec
	return marshalWithExtensions((*Alias)(s), s.Extensions)
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *Spec) UnmarshalJSON(data []byte) error {
	type Alias Spec
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if err := unmarshalExtensions(data, &aux.Extensions); err != nil {
		return err
	}
	*s = Spec(aux)
	return nil
}

// MarshalJSON implements json.Marshaler.
func (s *Info) MarshalJSON() ([]byte, error) {
	type Alias Info
	return marshalWithExtensions((*Alias)(s), s.Extensions)
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *Info) UnmarshalJSON(data []byte) error {
	type Alias Info
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if err := unmarshalExtensions(data, &aux.Extensions); err != nil {
		return err
	}
	*s = Info(aux)
	return nil
}

// MarshalJSON implements json.Marshaler.
func (s *Contact) MarshalJSON() ([]byte, error) {
	type Alias Contact
	return marshalWithExtensions((*Alias)(s), s.Extensions)
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *Contact) UnmarshalJSON(data []byte) error {
	type Alias Contact
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if err := unmarshalExtensions(data, &aux.Extensions); err != nil {
		return err
	}
	*s = Contact(aux)
	return nil
}

// MarshalJSON implements json.Marshaler.
func (s *License) MarshalJSON() ([]byte, error) {
	type Alias License
	return marshalWithExtensions((*Alias)(s), s.Extensions)
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *License) UnmarshalJSON(data []byte) error {
	type Alias License
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if err := unmarshalExtensions(data, &aux.Extensions); err != nil {
		return err
	}
	*s = License(aux)
	return nil
}

// MarshalJSON implements json.Marshaler.
func (s *Server) MarshalJSON() ([]byte, error) {
	type Alias Server
	return marshalWithExtensions((*Alias)(s), s.Extensions)
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *Server) UnmarshalJSON(data []byte) error {
	type Alias Server
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if err := unmarshalExtensions(data, &aux.Extensions); err != nil {
		return err
	}
	*s = Server(aux)
	return nil
}

// MarshalJSON implements json.Marshaler.
func (s *ServerVariable) MarshalJSON() ([]byte, error) {
	type Alias ServerVariable
	return marshalWithExtensions((*Alias)(s), s.Extensions)
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *ServerVariable) UnmarshalJSON(data []byte) error {
	type Alias ServerVariable
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if err := unmarshalExtensions(data, &aux.Extensions); err != nil {
		return err
	}
	*s = ServerVariable(aux)
	return nil
}

// MarshalJSON implements json.Marshaler.
func (s *PathItem) MarshalJSON() ([]byte, error) {
	type Alias PathItem
	return marshalWithExtensions((*Alias)(s), s.Extensions)
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *PathItem) UnmarshalJSON(data []byte) error {
	type Alias PathItem
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if err := unmarshalExtensions(data, &aux.Extensions); err != nil {
		return err
	}
	*s = PathItem(aux)
	return nil
}

// MarshalJSON implements json.Marshaler.
func (s *Operation) MarshalJSON() ([]byte, error) {
	type Alias Operation
	return marshalWithExtensions((*Alias)(s), s.Extensions)
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *Operation) UnmarshalJSON(data []byte) error {
	type Alias Operation
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if err := unmarshalExtensions(data, &aux.Extensions); err != nil {
		return err
	}
	*s = Operation(aux)
	return nil
}

// MarshalJSON implements json.Marshaler.
func (s *Parameter) MarshalJSON() ([]byte, error) {
	type Alias Parameter
	return marshalWithExtensions((*Alias)(s), s.Extensions)
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *Parameter) UnmarshalJSON(data []byte) error {
	type Alias Parameter
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if err := unmarshalExtensions(data, &aux.Extensions); err != nil {
		return err
	}
	*s = Parameter(aux)
	return nil
}

// MarshalJSON implements json.Marshaler.
func (s *RequestBody) MarshalJSON() ([]byte, error) {
	type Alias RequestBody
	return marshalWithExtensions((*Alias)(s), s.Extensions)
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *RequestBody) UnmarshalJSON(data []byte) error {
	type Alias RequestBody
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if err := unmarshalExtensions(data, &aux.Extensions); err != nil {
		return err
	}
	*s = RequestBody(aux)
	return nil
}

// MarshalJSON implements json.Marshaler.
func (s *MediaType) MarshalJSON() ([]byte, error) {
	type Alias MediaType
	return marshalWithExtensions((*Alias)(s), s.Extensions)
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *MediaType) UnmarshalJSON(data []byte) error {
	type Alias MediaType
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if err := unmarshalExtensions(data, &aux.Extensions); err != nil {
		return err
	}
	*s = MediaType(aux)
	return nil
}

// MarshalJSON implements json.Marshaler.
func (s *Encoding) MarshalJSON() ([]byte, error) {
	type Alias Encoding
	return marshalWithExtensions((*Alias)(s), s.Extensions)
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *Encoding) UnmarshalJSON(data []byte) error {
	type Alias Encoding
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if err := unmarshalExtensions(data, &aux.Extensions); err != nil {
		return err
	}
	*s = Encoding(aux)
	return nil
}

// MarshalJSON implements json.Marshaler.
func (s *Response) MarshalJSON() ([]byte, error) {
	type Alias Response
	return marshalWithExtensions((*Alias)(s), s.Extensions)
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *Response) UnmarshalJSON(data []byte) error {
	type Alias Response
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if err := unmarshalExtensions(data, &aux.Extensions); err != nil {
		return err
	}
	*s = Response(aux)
	return nil
}

// MarshalJSON implements json.Marshaler.
func (s *Header) MarshalJSON() ([]byte, error) {
	type Alias Header
	return marshalWithExtensions((*Alias)(s), s.Extensions)
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *Header) UnmarshalJSON(data []byte) error {
	type Alias Header
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if err := unmarshalExtensions(data, &aux.Extensions); err != nil {
		return err
	}
	*s = Header(aux)
	return nil
}

// MarshalJSON implements json.Marshaler.
func (s *Example) MarshalJSON() ([]byte, error) {
	type Alias Example
	return marshalWithExtensions((*Alias)(s), s.Extensions)
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *Example) UnmarshalJSON(data []byte) error {
	type Alias Example
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if err := unmarshalExtensions(data, &aux.Extensions); err != nil {
		return err
	}
	*s = Example(aux)
	return nil
}

// MarshalJSON implements json.Marshaler.
func (s *Link) MarshalJSON() ([]byte, error) {
	type Alias Link
	return marshalWithExtensions((*Alias)(s), s.Extensions)
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *Link) UnmarshalJSON(data []byte) error {
	type Alias Link
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if err := unmarshalExtensions(data, &aux.Extensions); err != nil {
		return err
	}
	*s = Link(aux)
	return nil
}

// MarshalJSON implements json.Marshaler.
func (s *Components) MarshalJSON() ([]byte, error) {
	type Alias Components
	return marshalWithExtensions((*Alias)(s), s.Extensions)
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *Components) UnmarshalJSON(data []byte) error {
	type Alias Components
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if err := unmarshalExtensions(data, &aux.Extensions); err != nil {
		return err
	}
	*s = Components(aux)
	return nil
}

// MarshalJSON implements json.Marshaler.
func (s *SecurityScheme) MarshalJSON() ([]byte, error) {
	type Alias SecurityScheme
	return marshalWithExtensions((*Alias)(s), s.Extensions)
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *SecurityScheme) UnmarshalJSON(data []byte) error {
	type Alias SecurityScheme
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if err := unmarshalExtensions(data, &aux.Extensions); err != nil {
		return err
	}
	*s = SecurityScheme(aux)
	return nil
}

// MarshalJSON implements json.Marshaler.
func (s *OAuthFlows) MarshalJSON() ([]byte, error) {
	type Alias OAuthFlows
	return marshalWithExtensions((*Alias)(s), s.Extensions)
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *OAuthFlows) UnmarshalJSON(data []byte) error {
	type Alias OAuthFlows
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if err := unmarshalExtensions(data, &aux.Extensions); err != nil {
		return err
	}
	*s = OAuthFlows(aux)
	return nil
}

// MarshalJSON implements json.Marshaler.
func (s *OAuthFlow) MarshalJSON() ([]byte, error) {
	type Alias OAuthFlow
	return marshalWithExtensions((*Alias)(s), s.Extensions)
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *OAuthFlow) UnmarshalJSON(data []byte) error {
	type Alias OAuthFlow
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if err := unmarshalExtensions(data, &aux.Extensions); err != nil {
		return err
	}
	*s = OAuthFlow(aux)
	return nil
}

// MarshalJSON implements json.Marshaler.
func (s *Tag) MarshalJSON() ([]byte, error) {
	type Alias Tag
	return marshalWithExtensions((*Alias)(s), s.Extensions)
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *Tag) UnmarshalJSON(data []byte) error {
	type Alias Tag
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if err := unmarshalExtensions(data, &aux.Extensions); err != nil {
		return err
	}
	*s = Tag(aux)
	return nil
}

// MarshalJSON implements json.Marshaler.
func (s *ExternalDocs) MarshalJSON() ([]byte, error) {
	type Alias ExternalDocs
	return marshalWithExtensions((*Alias)(s), s.Extensions)
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *ExternalDocs) UnmarshalJSON(data []byte) error {
	type Alias ExternalDocs
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if err := unmarshalExtensions(data, &aux.Extensions); err != nil {
		return err
	}
	*s = ExternalDocs(aux)
	return nil
}

// MarshalJSON implements json.Marshaler.
func (s *Schema) MarshalJSON() ([]byte, error) {
	type Alias Schema
	return marshalWithExtensions((*Alias)(s), s.Extensions)
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *Schema) UnmarshalJSON(data []byte) error {
	type Alias Schema
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if err := unmarshalExtensions(data, &aux.Extensions); err != nil {
		return err
	}
	// Items and AdditionalProperties are typed as `any` so they can hold both
	// boolean values (e.g. OpenAPI 3.1's "items: false" or
	// "additionalProperties: false") and nested schemas. When unmarshaled into
	// an empty interface, a schema object becomes a map[string]any, which would
	// break round-trip value equality. Coerce both back to *Schema so that
	// Schema's own UnmarshalJSON (including extension handling) is applied.
	// The same coercion is applied to both fields to keep their round-trip
	// behavior symmetric (L-84: previously only Items was coerced, so a
	// schema-valued AdditionalProperties came back as map[string]any).
	aux.Items = coerceAnySchema(aux.Items)
	aux.AdditionalProperties = coerceAnySchema(aux.AdditionalProperties)
	*s = Schema(aux)
	return nil
}

// coerceAnySchema converts a schema object that was unmarshaled into a plain
// map[string]any (because the field is typed `any` to also allow boolean
// values) back into a *Schema, restoring value equality after a round-trip.
// It is a no-op for non-mapping values (booleans, nil, already-*Schema).
func coerceAnySchema(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	b, err := json.Marshal(m)
	if err != nil {
		return v
	}
	var s Schema
	if err := json.Unmarshal(b, &s); err != nil {
		return v
	}
	return &s
}

// MarshalJSON implements json.Marshaler.
func (s *Discriminator) MarshalJSON() ([]byte, error) {
	type Alias Discriminator
	return marshalWithExtensions((*Alias)(s), s.Extensions)
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *Discriminator) UnmarshalJSON(data []byte) error {
	type Alias Discriminator
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if err := unmarshalExtensions(data, &aux.Extensions); err != nil {
		return err
	}
	*s = Discriminator(aux)
	return nil
}

// MarshalJSON implements json.Marshaler.
func (s *XML) MarshalJSON() ([]byte, error) {
	type Alias XML
	return marshalWithExtensions((*Alias)(s), s.Extensions)
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *XML) UnmarshalJSON(data []byte) error {
	type Alias XML
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if err := unmarshalExtensions(data, &aux.Extensions); err != nil {
		return err
	}
	*s = XML(aux)
	return nil
}
