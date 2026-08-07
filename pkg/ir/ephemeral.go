package ir

// EphemeralResourceIR describes a Terraform ephemeral resource: a temporary
// value that is never persisted to state or plan files.
//
// Struct-typed fields (ConfigSchema, ResultSchema, OpenMapping) are serialized
// unconditionally; encoding/json treats `omitempty` as a no-op on non-pointer
// struct values, so the tag is omitted here rather than misleadingly implying
// empty values drop. RenewMapping/CloseMapping are pointers and retain
// `omitempty`.
//
// HasRenew and HasClose are convenience flags that duplicate the nil-ness of
// RenewMapping/CloseMapping. The invariant is HasRenew == (RenewMapping != nil)
// and HasClose == (CloseMapping != nil): producers (transformer, api handler)
// set the pointer and the flag together, and the generator keys the
// WithRenew/WithClose interface assertion off the flag. The mapping pointer is
// the source of truth; a hand-constructed IR that sets HasRenew without a
// non-nil RenewMapping violates the contract and would emit a WithRenew
// assertion whose Renew method has no backing mapping (L-62: this redundancy is
// documented rather than mechanically enforced because the generator's Renew
// body is an honest scaffold either way; keep the two consistent when
// constructing the IR by hand).
type EphemeralResourceIR struct {
	Name            string              `json:"name"`
	FullName        string              `json:"full_name,omitempty"`
	TypeName        string              `json:"type_name,omitempty"`
	Description     string              `json:"description,omitempty"`
	ConfigSchema    ObjectSchemaIR      `json:"config_schema"`
	ResultSchema    ObjectSchemaIR      `json:"result_schema"`
	OpenMapping     OperationMappingIR  `json:"open_mapping"`
	RenewMapping    *OperationMappingIR `json:"renew_mapping,omitempty"`
	CloseMapping    *OperationMappingIR `json:"close_mapping,omitempty"`
	HasRenew        bool                `json:"has_renew,omitempty"`
	HasClose        bool                `json:"has_close,omitempty"`
	Tags            []string            `json:"tags,omitempty"`
	SourceOperation string              `json:"source_operation,omitempty"`
}
