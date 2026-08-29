package ir

// ObjectSchemaIR describes the shape of an object-level schema, such as the
// schema of a resource, data source, action, or nested block. It is intentionally
// narrower than SchemaIR because it only represents object-like aggregates.
type ObjectSchemaIR struct {
	Attributes []AttributeIR `json:"attributes,omitempty"`
	Blocks     []BlockIR     `json:"blocks,omitempty"`

	// DependentRequired records JSON Schema conditional required fields at the
	// object level. It is duplicated on ObjectSchemaIR so that validators can be
	// inferred per trigger attribute when transforming attributes.
	DependentRequired map[string][]string `json:"dependent_required,omitempty"`
}

// AttributeIR is a single Terraform-plugin-framework-style attribute inside an
// object schema. It carries the attribute's own schema plus framework metadata
// like optionality, computedness, deprecation, validators, and plan modifiers.
type AttributeIR struct {
	Name string `json:"name"`
	// WireName is the original OpenAPI property/parameter name when it differs
	// from the Terraform attribute Name (which is snake_case and reserved-name
	// sanitized). Request bodies, response mapping, and query/header parameters
	// use WireName so the wire format matches the API (G14/G18).
	WireName            string           `json:"wire_name,omitempty"`
	Schema              SchemaIR         `json:"schema"`
	Description         string           `json:"description,omitempty"`
	MarkdownDescription string           `json:"markdown_description,omitempty"`
	Required            bool             `json:"required,omitempty"`
	Optional            bool             `json:"optional,omitempty"`
	Computed            bool             `json:"computed,omitempty"`
	Sensitive           bool             `json:"sensitive,omitempty"`
	WriteOnly           bool             `json:"write_only,omitempty"` // writeOnly: true → not stored in state (Terraform 1.10+)
	ForceNew            bool             `json:"force_new,omitempty"`  // x-terraform-force-new / forceNew marker
	// RequestInput marks an attribute whose value the generated CRUD body
	// sends to the API: a create/update request-body property, a formData
	// field, or a path/query/header parameter fed from state. It guards the
	// computed_attributes override: making such an attribute Computed-only
	// would leave the request sending a value the practitioner can never
	// supply (e.g. a required clusterId query param), breaking create and
	// import (G39).
	RequestInput        bool             `json:"request_input,omitempty"`
	Deprecated          bool             `json:"deprecated,omitempty"`
	DeprecationMessage  string           `json:"deprecation_message,omitempty"`
	Default             *any             `json:"default,omitempty"`
	PlanModifiers       []PlanModifierIR `json:"plan_modifiers,omitempty"`
	Validators          []ValidatorIR    `json:"validators,omitempty"`
}

// ComputedOnly reports whether the attribute is server-populated and not
// practitioner-settable: Computed with neither Required nor Optional. An
// import ID must never target such an attribute — the practitioner cannot
// know its value before the first read (G39).
func (a AttributeIR) ComputedOnly() bool {
	return a.Computed && !a.Required && !a.Optional
}

// BlockNestingMode describes how many block instances a Terraform block allows.
type BlockNestingMode string

const (
	// NestingSingle indicates exactly one block instance is allowed.
	NestingSingle BlockNestingMode = "single"
	// NestingList indicates zero or more ordered block instances.
	NestingList BlockNestingMode = "list"
	// NestingSet indicates zero or more unordered, unique block instances.
	NestingSet BlockNestingMode = "set"
)

// BlockIR is a Terraform-plugin-framework-style nested block. Blocks are used
// for object-like collections that require their own lifecycle or repeated
// groups of attributes, as distinct from plain attributes.
type BlockIR struct {
	Name               string           `json:"name"`
	Schema             ObjectSchemaIR   `json:"schema"`
	NestingMode        BlockNestingMode `json:"nesting_mode,omitempty"`
	MinItems           *int64           `json:"min_items,omitempty"`
	MaxItems           *int64           `json:"max_items,omitempty"`
	Description        string           `json:"description,omitempty"`
	Deprecated         bool             `json:"deprecated,omitempty"`
	DeprecationMessage string           `json:"deprecation_message,omitempty"`
}

// SchemaIR is the core IR schema node. A SchemaIR is either a primitive type,
// a collection, a union, or an object-like aggregate described by Attributes
// and Blocks.
//
// The framework flags below (Required/Optional/Computed/Sensitive/WriteOnly/
// ForceNew/Deprecated/DeprecationMessage/Default/Validators/PlanModifiers) are
// for STANDALONE schema nodes (e.g. ParamIR.Schema, function return types).
// When a SchemaIR is embedded in an AttributeIR, the AttributeIR is the single
// source of truth for those flags and the embedded SchemaIR carries only the
// shape fields (Type/Collection/Union/Attributes/Blocks/...); producers and
// consumers never read the flag copies on an embedded SchemaIR (N-49).
type SchemaIR struct {
	Name                  string               `json:"name"`
	Description           string               `json:"description,omitempty"`
	Type                  PrimitiveType        `json:"type,omitempty"`
	Collection            *CollectionType      `json:"collection,omitempty"`
	Union                 *UnionType           `json:"union,omitempty"`
	Attributes            []AttributeIR        `json:"attributes,omitempty"`
	Blocks                []BlockIR            `json:"blocks,omitempty"`
	Required              bool                 `json:"required,omitempty"`
	Optional              bool                 `json:"optional,omitempty"`
	Computed              bool                 `json:"computed,omitempty"`
	Sensitive             bool                 `json:"sensitive,omitempty"`
	WriteOnly             bool                 `json:"write_only,omitempty"` // writeOnly: true → not stored in state (Terraform 1.10+)
	ForceNew              bool                 `json:"force_new,omitempty"`  // x-terraform-force-new / forceNew marker
	Deprecated            bool                 `json:"deprecated,omitempty"`
	DeprecationMessage    string               `json:"deprecation_message,omitempty"`
	Default               *any                 `json:"default,omitempty"`
	Validators            []ValidatorIR        `json:"validators,omitempty"`
	PlanModifiers         []PlanModifierIR     `json:"plan_modifiers,omitempty"`
	Format                string               `json:"format,omitempty"` // OpenAPI format: date-time, email, uuid, etc.
	Pattern               string               `json:"pattern,omitempty"`
	EnumValues            []any                `json:"enum_values,omitempty"`
	Const                 *any                 `json:"const,omitempty"` // JSON Schema const: exact value match
	MinLength             *int                 `json:"min_length,omitempty"`
	MaxLength             *int                 `json:"max_length,omitempty"`
	Minimum               *float64             `json:"minimum,omitempty"`
	Maximum               *float64             `json:"maximum,omitempty"`
	ExclusiveMinimum      *float64             `json:"exclusive_minimum,omitempty"` // JSON Schema 2020-12: strict > bound
	ExclusiveMaximum      *float64             `json:"exclusive_maximum,omitempty"` // JSON Schema 2020-12: strict < bound
	MultipleOf            *float64             `json:"multiple_of,omitempty"`       // JSON Schema: value must be divisible by this
	MinItems              *int                 `json:"min_items,omitempty"`
	MaxItems              *int                 `json:"max_items,omitempty"`
	MinProperties         *int                 `json:"min_properties,omitempty"`         // JSON Schema: min object property count
	MaxProperties         *int                 `json:"max_properties,omitempty"`         // JSON Schema: max object property count
	Not                   *SchemaIR            `json:"not,omitempty"`                    // JSON Schema: negation
	IfSchema              *SchemaIR            `json:"if,omitempty"`                     // JSON Schema: conditional if
	ThenSchema            *SchemaIR            `json:"then,omitempty"`                   // JSON Schema: conditional then
	ElseSchema            *SchemaIR            `json:"else,omitempty"`                   // JSON Schema: conditional else
	DependentRequired     map[string][]string  `json:"dependent_required,omitempty"`     // JSON Schema: conditional required fields
	DependentSchemas      map[string]*SchemaIR `json:"dependent_schemas,omitempty"`      // JSON Schema: conditional schema application
	PatternProperties     map[string]*SchemaIR `json:"pattern_properties,omitempty"`     // JSON Schema: regex-matched property schemas
	PropertyNames         *SchemaIR            `json:"property_names,omitempty"`         // JSON Schema: validates property names
	UnevaluatedProperties *SchemaIR            `json:"unevaluated_properties,omitempty"` // JSON Schema 2020-12: controls unevaluated properties
	OriginalRef           string               `json:"original_ref,omitempty"`
	// Source position tracking for schema nodes was removed (N-47): no producer
	// ever set SourceLocation, and its JSON tag diverged from
	// diagnostics.SourceLocation (col vs column). Source traceability for
	// diagnostics lives on the parser's Schema and the diagnostics themselves.
}

// NewDefaultInt returns a pointer to a default value suitable for
// AttributeIR.Default or SchemaIR.Default. The value is stored as int64 so
// that integer precision is preserved for in-memory consumers (the generator
// reads defaults back via int64Value). JSON marshaling still emits the exact
// integer; only an encoding/json *any* decode (which yields float64) loses
// precision, and the IR is the canonical in-memory form, not a reload source.
func NewDefaultInt(v int) *any {
	a := any(int64(v))
	return &a
}

// NewDefaultInt64 returns a pointer to a default value suitable for
// AttributeIR.Default or SchemaIR.Default. The value is stored as int64 so
// that integer precision above 2^53 is preserved for in-memory consumers.
func NewDefaultInt64(v int64) *any {
	a := any(v)
	return &a
}

// NewDefaultFloat64 returns a pointer to a float64 default value suitable for
// AttributeIR.Default or SchemaIR.Default.
func NewDefaultFloat64(v float64) *any {
	a := any(v)
	return &a
}

// NewDefaultString returns a pointer to a string default value suitable for
// AttributeIR.Default or SchemaIR.Default.
func NewDefaultString(v string) *any {
	a := any(v)
	return &a
}

// NewDefaultBool returns a pointer to a bool default value suitable for
// AttributeIR.Default or SchemaIR.Default.
func NewDefaultBool(v bool) *any {
	a := any(v)
	return &a
}
