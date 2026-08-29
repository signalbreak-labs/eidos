package transformer

// SchemaSpec is a minimal normalized OpenAPI schema used by the transformer when
// the parser/normalizer packages are not yet available. It carries enough
// information to decide the Terraform Plugin Framework attribute type.
//
// The previous TypeMapping/FrameworkType/GoModelType cluster (MapSchemaType,
// mapArrayType, mapObjectType, MapPrimitiveType, ApplyNullable, and the
// mapStringFormat/mapIntegerFormat/mapNumberFormat helpers) lived here as a
// parallel type-mapping surface that no production caller reached — only its own
// tests. The live type mapping is schemaIRFromSpecRecursive / schemaIRFromSpec
// (resource_schema.go, action.go), which already covers arrays, objects, maps,
// nullable, and uniqueItems→Set, so the parallel surface was removed as dead
// code (A1) rather than maintained as a correct-if-wired guarantee.
type SchemaSpec struct {
	Type string
	// Description is the OpenAPI `description` of the schema (or of the property
	// this schema is the value of). It is carried through conversion so
	// attribute construction can set ir.AttributeIR.Description, which the
	// generators already render as the framework MarkdownDescription and as the
	// docs attribute blurb.
	Description string
	Format      string
	Nullable    bool
	UniqueItems bool
	WriteOnly   bool
	// ReadOnly is the OpenAPI `readOnly` flag. A readOnly property may appear
	// in responses but is not a practitioner input, even when the spec also
	// lists it on the request body (issue #40).
	ReadOnly             bool
	Required             []string
	Items                *SchemaSpec
	Properties           map[string]SchemaSpec
	AdditionalProperties *SchemaSpec

	// OneOf/AnyOf carry union composition captured during conversion so the IR
	// can represent the union (SchemaIR.Union, D1) instead of dropping it with
	// a warning. Top-level occurrences (the response/request root schema) are
	// wired through to generation; nested occurrences are also captured (they
	// render as Dynamic attributes) and still emit a fail-loud warning.
	// RefName records the schema name a $ref resolved to (e.g. "Pet"), used to
	// name union variants and wrapper attributes. Discriminator carries the
	// OpenAPI discriminator declared alongside a oneOf.
	OneOf         []SchemaSpec
	AnyOf         []SchemaSpec
	RefName       string
	Discriminator *DiscriminatorSpec

	// Scalar constraints carried through from the parser's schema so
	// schemaIRFromSpec(Recursive) can populate the matching ir.SchemaIR fields
	// and the generator can emit framework validators (OneOf, Between,
	// LengthBetween, RegexMatches …). Before these fields existed the
	// parser→SchemaSpec conversion dropped every constraint, so no generated
	// attribute ever carried a spec-declared enum or bound (G39: constraints
	// must not be silently dropped). Pointer fields distinguish "absent" from
	// the zero bound; the parser's non-pointer numbers treat 0 as absent,
	// which is semantically a no-op bound anyway.
	Enum             []any
	Const            *any
	Pattern          string
	MinLength        *int
	MaxLength        *int
	Minimum          *float64
	Maximum          *float64
	ExclusiveMinimum *float64
	ExclusiveMaximum *float64
	MultipleOf       *float64
	MinItems         *int
	MaxItems         *int
}

// DiscriminatorSpec carries an OpenAPI discriminator object: the property
// whose value selects the concrete variant, plus the optional value→schema
// mapping.
type DiscriminatorSpec struct {
	PropertyName string
	Mapping      map[string]string
}
