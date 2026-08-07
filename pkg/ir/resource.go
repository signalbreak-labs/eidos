package ir

import "time"

// ResourceIR describes a Terraform managed resource produced from an OpenAPI
// operation set. It carries the resource schema plus the CRUD operation mappings
// needed to implement the resource lifecycle.
//
// Struct-typed fields (Schema, CRUDMapping) are serialized unconditionally;
// encoding/json treats `omitempty` as a no-op on non-pointer struct values, so
// the tag is omitted here rather than misleadingly implying empty values drop.
type ResourceIR struct {
	Name               string           `json:"name"`
	FullName           string           `json:"full_name,omitempty"`
	TypeName           string           `json:"type_name,omitempty"`
	Description        string           `json:"description,omitempty"`
	Schema             ObjectSchemaIR   `json:"schema"`
	CRUDMapping        CRUDMappingIR    `json:"crud_mapping"`
	IDAttribute        string           `json:"id_attribute,omitempty"`
	ImportIDFormat     string           `json:"import_id_format,omitempty"`
	Importable         bool             `json:"importable,omitempty"`
	SensitiveAttrs     []string         `json:"sensitive_attrs,omitempty"`
	Timeouts           *TimeoutConfigIR `json:"timeouts,omitempty"`
	Tags               []string         `json:"tags,omitempty"`
	DeprecationMessage string           `json:"deprecation_message,omitempty"`
	SourceOperation    string           `json:"source_operation,omitempty"`
	SchemaVersion      int              `json:"schema_version,omitempty"`
	StateUpgrades      []StateUpgradeIR `json:"state_upgrades,omitempty"`
}

// StateUpgradeIR describes a single migration from a prior schema version to the
// current resource schema. FromVersion is the stored state version being
// upgraded.
//
// Renames maps old attribute names to their current names; BlockRenames maps old
// block names to their current block names.
//
// AddedAttributes/AddedBlocks name current attributes/blocks that did not exist
// in the prior schema; they are omitted from the prior schema and null-initialized
// during upgrade.
//
// RemovedAttributes/RemovedBlocks name prior attributes/blocks absent from the
// current schema. They are synthesized into the prior schema as Dynamic-typed
// attributes so historical state decodes, then dropped during upgrade (the
// upgrader simply does not copy them into the upgraded model).
type StateUpgradeIR struct {
	FromVersion       int               `json:"from_version"`
	Renames           map[string]string `json:"renames,omitempty"`
	BlockRenames      map[string]string `json:"block_renames,omitempty"`
	AddedAttributes   []string          `json:"added_attributes,omitempty"`
	AddedBlocks       []string          `json:"added_blocks,omitempty"`
	RemovedAttributes []string          `json:"removed_attributes,omitempty"`
	RemovedBlocks     []string          `json:"removed_blocks,omitempty"`
}

// CRUDMappingIR binds the Create, Read, Update, Delete, and optional Import
// operations for a managed resource to concrete HTTP operation mappings.
//
// Create, Read, and Delete are non-pointer structs and so are serialized
// unconditionally (encoding/json treats `omitempty` as a no-op on non-pointer
// struct values); Update and Import are pointers and retain `omitempty`.
type CRUDMappingIR struct {
	Create OperationMappingIR  `json:"create"`
	Read   OperationMappingIR  `json:"read"`
	Update *OperationMappingIR `json:"update,omitempty"`
	Delete OperationMappingIR  `json:"delete"`
	Import *OperationMappingIR `json:"import,omitempty"`
}

// OperationMappingIR describes a single HTTP operation used by a resource,
// data source, action, ephemeral resource, or list resource. It captures the
// request shape, expected response shape, and parameter bindings.
type OperationMappingIR struct {
	Method         string    `json:"method,omitempty"`
	PathTemplate   string    `json:"path_template,omitempty"`
	PathParams     []ParamIR `json:"path_params,omitempty"`
	QueryParams    []ParamIR `json:"query_params,omitempty"`
	HeaderParams   []ParamIR `json:"header_params,omitempty"`
	CookieParams   []ParamIR `json:"cookie_params,omitempty"`
	FormDataParams []ParamIR `json:"form_data_params,omitempty"`
	// MediaType is the request body's selected media type (e.g.
	// "application/json", "application/x-www-form-urlencoded",
	// "multipart/form-data", "application/xml"). It is empty for bodiless
	// operations (GET/DELETE). The value is the media type whose schema was
	// carried into RequestSchema/BodySchema, selected deterministically
	// (application/json preferred, else the first schema-bearing media type
	// lexicographically) so the generator emits the matching body encoding
	// (JSON / form-urlencoded / multipart / XML) instead of always JSON (A2).
	MediaType      string                 `json:"media_type,omitempty"`
	BodySchema     *SchemaIR              `json:"body_schema,omitempty"`
	ResponseSchema *SchemaIR              `json:"response_schema,omitempty"`
	SuccessCodes   []int                  `json:"success_codes,omitempty"`
	ErrorMappings  map[int]ErrorMappingIR `json:"error_mappings,omitempty"`
	// SecurityRequirements carries the operation's declared security
	// requirements (a list of alternatives; each alternative is a map of
	// security scheme name to scopes). OpenAPI security is OR across
	// alternatives and AND within an alternative. When exactly one requirement
	// is present, the wired body applies only that requirement's scheme
	// interceptors (per-operation AND resolution, REMAINING_GAPS §1); when nil
	// (the operation declares no security) the wired body applies all configured
	// interceptors (inheriting the global default); when more than one (OR) the
	// wired body applies all configured interceptors and the transformer warns,
	// because choosing which alternative the practitioner intended is ambiguous
	// for a non-interactive provider.
	SecurityRequirements []map[string][]string `json:"security_requirements,omitempty"`
}

// ParamIR describes a single operation parameter and its binding location.
type ParamIR struct {
	Name        string   `json:"name"`
	In          string   `json:"in,omitempty"`
	Description string   `json:"description,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Schema      SchemaIR `json:"schema"`
	Deprecated  bool     `json:"deprecated,omitempty"`
}

// ErrorMappingIR describes the response shape for a single HTTP error status
// code so generated code can produce accurate diagnostics.
type ErrorMappingIR struct {
	StatusCode  int       `json:"status_code,omitempty"`
	Description string    `json:"description,omitempty"`
	Schema      *SchemaIR `json:"schema,omitempty"`
}

// TimeoutConfigIR captures optional per-operation timeouts for managed
// resources.
type TimeoutConfigIR struct {
	Create *time.Duration `json:"create,omitempty"`
	Read   *time.Duration `json:"read,omitempty"`
	Update *time.Duration `json:"update,omitempty"`
	Delete *time.Duration `json:"delete,omitempty"`
}
