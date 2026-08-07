package ir

import (
	"fmt"
	"strings"
)

// PrimitiveType enumerates the scalar value kinds supported by the IR.
type PrimitiveType string

// Primitive type constants.
const (
	// TypeString is the string primitive type.
	TypeString PrimitiveType = "string"
	// TypeInt is the integer primitive type.
	TypeInt PrimitiveType = "integer"
	// TypeFloat is the number primitive type.
	TypeFloat PrimitiveType = "number"
	// TypeBool is the boolean primitive type.
	TypeBool PrimitiveType = "boolean"
	// TypeNull is the null primitive type.
	TypeNull PrimitiveType = "null"
	// TypeDynamic is the dynamic primitive type.
	TypeDynamic PrimitiveType = "dynamic"
)

// CollectionKind describes the kind of container represented by a
// CollectionType.
type CollectionKind string

// Collection kind constants.
const (
	// List is a list collection kind.
	List CollectionKind = "list"
	// Set is a set collection kind.
	Set CollectionKind = "set"
	// Map is a map collection kind.
	Map CollectionKind = "map"
)

// CollectionType represents a list, set, or map whose elements conform to a
// single SchemaIR.
type CollectionType struct {
	Kind        CollectionKind `json:"kind"`
	ElementType SchemaIR       `json:"element_type"`
}

// UnionKind distinguishes between exclusive (oneOf) and inclusive (anyOf) unions.
type UnionKind string

// Union kind constants.
const (
	// OneOf is a oneOf union kind.
	OneOf UnionKind = "oneOf"
	// AnyOf is an inclusive union kind.
	AnyOf UnionKind = "anyOf"
)

// UnionType represents a schema that can match one of several variants. When a
// discriminator is present it describes how the concrete variant is selected.
type UnionType struct {
	Kind          UnionKind        `json:"kind"`
	Variants      []SchemaIR       `json:"variants"`
	Discriminator *DiscriminatorIR `json:"discriminator,omitempty"`
}

// DiscriminatorIR captures OpenAPI/JSON Schema discriminator metadata used to
// resolve polymorphic unions.
type DiscriminatorIR struct {
	PropertyName string            `json:"property_name"`
	Mapping      map[string]string `json:"mapping"` // discriminator value → schema name
}

// SourceLocation records the position of a construct in the original OpenAPI
// spec for diagnostic traceability.
type SourceLocation struct {
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
	Col  int    `json:"col,omitempty"`
}

// Validate reports a single error describing the structural inconsistencies it
// checks. It is a partial, top-level sanity check, not an exhaustive validator:
// it verifies that the mutually exclusive shape fields (Type, Collection, Union)
// are not set together and that Required and Optional are not both set. It does
// NOT check every framework invariant — for example it does not flag
// Required && Computed (invalid for a managed-resource attribute), it does not
// flag Type set alongside Attributes/Blocks (an object shape should not also
// carry a primitive Type), and it does not recurse into Collection.ElementType,
// Union.Variants, or nested Attributes/Blocks. Callers that need deeper
// validation must do so themselves (L-64: the prior doc overpromised "all
// structural inconsistencies").
func (s SchemaIR) Validate() error {
	var errs []string
	if s.Type != "" && s.Collection != nil {
		errs = append(errs, fmt.Sprintf("schema %q has both Type=%q and Collection", s.Name, s.Type))
	}
	if s.Type != "" && s.Union != nil {
		errs = append(errs, fmt.Sprintf("schema %q has both Type=%q and Union", s.Name, s.Type))
	}
	if s.Collection != nil && s.Union != nil {
		errs = append(errs, fmt.Sprintf("schema %q has both Collection and Union", s.Name))
	}
	if s.Required && s.Optional {
		errs = append(errs, fmt.Sprintf("schema %q has both Required and Optional set", s.Name))
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(errs, "; "))
}
