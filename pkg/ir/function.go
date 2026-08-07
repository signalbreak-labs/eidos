package ir

// FunctionParamIR is the type used to describe a single argument of a provider-
// defined function. It is intentionally an alias of AttributeIR because both
// concepts share the same core shape: a name, a schema, and optionality. Using
// a dedicated alias makes the function-argument intent explicit without
// duplicating the underlying structure.
type FunctionParamIR = AttributeIR

// FunctionIR describes a provider-defined function that can be called from
// Terraform configuration expressions.
//
// ReturnType is a non-pointer struct, so encoding/json treats `omitempty` as a
// no-op on it; the tag is omitted here rather than misleadingly implying an
// empty return type drops (a zero ReturnType serializes as `"return_type":{}`).
type FunctionIR struct {
	Name                string            `json:"name"`
	FullName            string            `json:"full_name,omitempty"`
	TypeName            string            `json:"type_name,omitempty"`
	Description         string            `json:"description,omitempty"`
	MarkdownDescription string            `json:"markdown_description,omitempty"`
	Arguments           []FunctionParamIR `json:"arguments,omitempty"`
	ReturnType          SchemaIR          `json:"return_type"`
	Variadic            bool              `json:"variadic,omitempty"`
	Tags                []string          `json:"tags,omitempty"`
	SourceOperation     string            `json:"source_operation,omitempty"`
}
