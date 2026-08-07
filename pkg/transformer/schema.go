package transformer

// SchemaType enumerates the primitive and container types used in the
// version-agnostic OpenAPI schema model consumed by the normalizer.
type SchemaType string

// Schema type constants.
const (
	// SchemaTypeObject is the object schema type.
	SchemaTypeObject SchemaType = "object"
	// SchemaTypeArray is the array schema type.
	SchemaTypeArray   SchemaType = "array"
	SchemaTypeString  SchemaType = "string"
	SchemaTypeInteger SchemaType = "integer"
	SchemaTypeNumber  SchemaType = "number"
	SchemaTypeBoolean SchemaType = "boolean"
	SchemaTypeNull    SchemaType = "null"
)

// Discriminator captures OpenAPI/JSON Schema discriminator metadata used to
// resolve polymorphic unions.
type Discriminator struct {
	PropertyName string            `json:"propertyName,omitempty" yaml:"propertyName,omitempty"`
	Mapping      map[string]string `json:"mapping,omitempty" yaml:"mapping,omitempty"` // discriminator value → schema name
}

// Schema is a thin, version-agnostic representation of an OpenAPI/JSON Schema
// node used before it is transformed into the Terraform-oriented IR. It keeps
// just enough information for normalizer passes such as allOf flattening and
// polymorphism normalization. Field names follow JSON Schema 2020-12 spelling
// (camelCase tags); the corresponding IR fields use Go's snake_case
// convention.
type Schema struct {
	Type                  SchemaType          `json:"type,omitempty" yaml:"type,omitempty"`
	Properties            map[string]*Schema  `json:"properties,omitempty" yaml:"properties,omitempty"`
	Required              []string            `json:"required,omitempty" yaml:"required,omitempty"`
	Items                 *Schema             `json:"items,omitempty" yaml:"items,omitempty"`
	AllOf                 []*Schema           `json:"allOf,omitempty" yaml:"allOf,omitempty"`
	OneOf                 []*Schema           `json:"oneOf,omitempty" yaml:"oneOf,omitempty"`
	AnyOf                 []*Schema           `json:"anyOf,omitempty" yaml:"anyOf,omitempty"`
	Not                   *Schema             `json:"not,omitempty" yaml:"not,omitempty"`
	Const                 interface{}         `json:"const,omitempty" yaml:"const,omitempty"`
	If                    *Schema             `json:"if,omitempty" yaml:"if,omitempty"`
	Then                  *Schema             `json:"then,omitempty" yaml:"then,omitempty"`
	Else                  *Schema             `json:"else,omitempty" yaml:"else,omitempty"`
	DependentRequired     map[string][]string `json:"dependentRequired,omitempty" yaml:"dependentRequired,omitempty"`
	DependentSchemas      map[string]*Schema  `json:"dependentSchemas,omitempty" yaml:"dependentSchemas,omitempty"`
	Discriminator         *Discriminator      `json:"discriminator,omitempty" yaml:"discriminator,omitempty"`
	Nullable              bool                `json:"nullable,omitempty" yaml:"nullable,omitempty"`
	Description           string              `json:"description,omitempty" yaml:"description,omitempty"`
	Format                string              `json:"format,omitempty" yaml:"format,omitempty"`
	Pattern               string              `json:"pattern,omitempty" yaml:"pattern,omitempty"`
	MinLength             *int                `json:"minLength,omitempty" yaml:"minLength,omitempty"`
	MaxLength             *int                `json:"maxLength,omitempty" yaml:"maxLength,omitempty"`
	Minimum               *float64            `json:"minimum,omitempty" yaml:"minimum,omitempty"`
	Maximum               *float64            `json:"maximum,omitempty" yaml:"maximum,omitempty"`
	ExclusiveMinimum      *float64            `json:"exclusiveMinimum,omitempty" yaml:"exclusiveMinimum,omitempty"`
	ExclusiveMaximum      *float64            `json:"exclusiveMaximum,omitempty" yaml:"exclusiveMaximum,omitempty"`
	MultipleOf            *float64            `json:"multipleOf,omitempty" yaml:"multipleOf,omitempty"`
	MinItems              *int                `json:"minItems,omitempty" yaml:"minItems,omitempty"`
	MaxItems              *int                `json:"maxItems,omitempty" yaml:"maxItems,omitempty"`
	MinProperties         *int                `json:"minProperties,omitempty" yaml:"minProperties,omitempty"`
	MaxProperties         *int                `json:"maxProperties,omitempty" yaml:"maxProperties,omitempty"`
	PatternProperties     map[string]*Schema  `json:"patternProperties,omitempty" yaml:"patternProperties,omitempty"`
	PropertyNames         *Schema             `json:"propertyNames,omitempty" yaml:"propertyNames,omitempty"`
	UnevaluatedProperties *Schema             `json:"unevaluatedProperties,omitempty" yaml:"unevaluatedProperties,omitempty"`
	Enum                  []interface{}       `json:"enum,omitempty" yaml:"enum,omitempty"`
}
