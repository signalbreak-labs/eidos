package ir

// DataSourceIR describes a Terraform data source produced from an OpenAPI
// read operation. It carries the data source schema plus the read operation
// mapping needed to fetch remote state.
//
// Struct-typed fields (Schema, ReadMapping) are serialized unconditionally;
// encoding/json treats `omitempty` as a no-op on non-pointer struct values, so
// the tag is omitted here rather than misleadingly implying empty values drop.
type DataSourceIR struct {
	Name               string             `json:"name"`
	FullName           string             `json:"full_name,omitempty"`
	TypeName           string             `json:"type_name,omitempty"`
	Description        string             `json:"description,omitempty"`
	Schema             ObjectSchemaIR     `json:"schema"`
	ReadMapping        OperationMappingIR `json:"read_mapping"`
	Tags               []string           `json:"tags,omitempty"`
	DeprecationMessage string             `json:"deprecation_message,omitempty"`
	SourceOperation    string             `json:"source_operation,omitempty"`
	// IsList marks a list data source whose Read response is a top-level JSON
	// array. The generator wires its Read to fetch (and paginate) the pages and
	// expose the collection as a Computed `items` List attribute (REMAINING_GAPS
	// §2/§4). A non-list data source keeps its single-object Read body.
	IsList bool `json:"is_list,omitempty"`
	// Pagination carries the provider's pagination strategy for a list data
	// source, so the wired Read body can follow pages (offset/cursor/link_header)
	// or fetch a single page (none). Nil defaults to none (single page).
	Pagination *PaginationIR `json:"pagination,omitempty"`
}
