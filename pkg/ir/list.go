package ir

// ListResourceIR describes a Terraform list resource used by terraform query
// to search for and stream resource identities.
//
// Struct-typed fields (ConfigSchema, ListMapping, IdentitySchema) are
// serialized unconditionally; encoding/json treats `omitempty` as a no-op on
// non-pointer struct values, so the tag is omitted here rather than
// misleadingly implying empty values drop. ResourceSchema is a pointer and
// retains `omitempty`.
type ListResourceIR struct {
	Name            string             `json:"name"`
	FullName        string             `json:"full_name,omitempty"`
	TypeName        string             `json:"type_name,omitempty"`
	Description     string             `json:"description,omitempty"`
	ConfigSchema    ObjectSchemaIR     `json:"config_schema"`
	ListMapping     OperationMappingIR `json:"list_mapping"`
	IdentitySchema  ObjectSchemaIR     `json:"identity_schema"`
	ResourceSchema  *ObjectSchemaIR    `json:"resource_schema,omitempty"`
	PaginationStyle string             `json:"pagination_style,omitempty"`
	Tags            []string           `json:"tags,omitempty"`
	SourceOperation string             `json:"source_operation,omitempty"`
	// CollectionPath records the OpenAPI path of the collection endpoint the
	// list resource was inferred from. Metadata only — never emitted into
	// generated code; it pairs a list resource with the managed resource
	// inferred from the same CRUD group so the two can share a type name.
	CollectionPath string `json:"collection_path,omitempty"`
	// Registerable records whether the provider can register this list
	// resource. The framework requires every registered ListResource type name
	// to equal a managed resource type name, so a list resource with no paired
	// managed resource must stay unregistered; docs and examples are suppressed
	// for such lists so they are not advertised as usable.
	Registerable bool `json:"registerable,omitempty"`
}
