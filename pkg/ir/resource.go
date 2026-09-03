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
	Name        string         `json:"name"`
	FullName    string         `json:"full_name,omitempty"`
	TypeName    string         `json:"type_name,omitempty"`
	Description string         `json:"description,omitempty"`
	Schema      ObjectSchemaIR `json:"schema"`
	CRUDMapping CRUDMappingIR  `json:"crud_mapping"`
	IDAttribute string         `json:"id_attribute,omitempty"`
	// IdentitySchema is the managed resource's identity schema, shared with
	// the paired list resource of the same type name. terraform query (Terraform
	// 1.14+) requires the managed resource to implement ResourceWithIdentity so
	// the framework can type the identities a list resource streams; the schemas
	// must match. Empty for resources with no identity (no paired list resource),
	// in which case the generator emits no IdentitySchema method. Carried as a
	// pointer so the absence of identity is distinguishable from an empty
	// (zero-attribute) schema and so omitempty drops it from serialization.
	IdentitySchema     *ObjectSchemaIR  `json:"identity_schema,omitempty"`
	ImportIDFormat     string           `json:"import_id_format,omitempty"`
	Importable         bool             `json:"importable,omitempty"`
	SensitiveAttrs     []string         `json:"sensitive_attrs,omitempty"`
	Timeouts           *TimeoutConfigIR `json:"timeouts,omitempty"`
	Tags               []string         `json:"tags,omitempty"`
	DeprecationMessage string           `json:"deprecation_message,omitempty"`
	SourceOperation    string           `json:"source_operation,omitempty"`
	// OverrideCreated marks a managed resource that was promoted from a
	// resource_override with generate_resource/create_operation/... rather than
	// inferred by the CRUD grouping pass. Such resources are not reproducible
	// from inference alone, so the config generator re-emits generate_resource
	// plus the CRUD operation ids for them so a normalized generator.yaml
	// round-trips (G8). False for inferred resources.
	OverrideCreated bool             `json:"override_created,omitempty"`
	SchemaVersion   int              `json:"schema_version,omitempty"`
	StateUpgrades   []StateUpgradeIR `json:"state_upgrades,omitempty"`
	// CollectionPath records the OpenAPI collection path of the CRUD group the
	// resource was inferred from. Metadata only — never emitted into generated
	// code; it pairs promoted list resources with the managed resource inferred
	// from the same group so the two can share a type name. Empty for
	// override-created resources, which have no inferred group.
	CollectionPath string `json:"collection_path,omitempty"`
	// IncludeCreateResponseAttributes lists create-response-only properties the
	// override asked to keep as Computed attributes (config
	// ResourceOverride.IncludeCreateResponseAttributes). Carried so the config
	// generator can re-emit the override on round-trip; the attributes
	// themselves are added to Schema at build time.
	IncludeCreateResponseAttributes []string `json:"include_create_response_attributes,omitempty"`
	// PathParamOverrides maps a CRUD operation (create/read/update/delete) to a
	// path-placeholder → schema-attribute-name mapping supplied by the
	// override. The generator consults it before the name-match and
	// id-attribute fallbacks when filling path placeholders, wiring paths whose
	// placeholder does not name-match any attribute and whose value is not the
	// resource id.
	PathParamOverrides map[string]map[string]string `json:"path_param_overrides,omitempty"`
	// ExcludedAttributes lists attributes the override asked to remove from the
	// schema (config ResourceOverride.ExcludeAttributes). Carried so the config
	// generator can re-emit the override on round-trip; the attributes
	// themselves are dropped from Schema at build time.
	ExcludedAttributes []string `json:"excluded_attributes,omitempty"`
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
	Method string `json:"method,omitempty"`
	// OperationID is the OpenAPI operationId of the operation this mapping was
	// built from. It is carried so the config generator can re-emit the
	// create/read/update/delete operation ids for override-created resources,
	// letting a normalized generator.yaml round-trip resources that CRUD
	// inference would not itself reproduce (G8). Empty for mappings built without
	// a parser operation or whose operation lacks an operationId.
	OperationID    string    `json:"operation_id,omitempty"`
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
	MediaType      string    `json:"media_type,omitempty"`
	BodySchema     *SchemaIR `json:"body_schema,omitempty"`
	ResponseSchema *SchemaIR `json:"response_schema,omitempty"`
	// ResponseEnvelope is the property name of a {data: ...} response envelope
	// the transformer flattened out of the response schema (e.g. "data"). The
	// generator unwraps the decoded response body by this key before applying it
	// to the model, so the schema and the response stay consistent (E1). Empty
	// when the response is not enveloped.
	ResponseEnvelope string `json:"response_envelope,omitempty"`
	// ResponseInnerPath is the property name to navigate into AFTER the response
	// envelope is unwrapped, before applying the body to the model. It handles
	// create/update responses that nest the created/updated resource under a
	// named property alongside side-effect objects (e.g. SpaceTraders
	// purchase-ship returns {data:{ship:{...},transaction:{...},agent:{...}}};
	// after unwrapping "data" the ship is still nested under "ship"). The
	// transformer sets it by matching a property of the unwrapped create/update
	// response whose $ref equals the resource's read response $ref. Empty when
	// the response applies directly after the envelope unwrap.
	ResponseInnerPath string `json:"response_inner_path,omitempty"`
	// ResponseIsCollection records that this (Read) operation's response, after
	// the envelope unwrap, is an ARRAY of instances — the read targets a
	// collection endpoint rather than an instance (a placeholder-free GET). The
	// generated readRemote then selects the array element whose identifier
	// matches the resource's identifier attribute instead of blindly taking
	// the first element, and reports the resource removed when no element
	// matches (G39). False for instance reads whose response wraps a single
	// item in an array (a get-one wrapper, issue #35).
	ResponseIsCollection bool `json:"response_is_collection,omitempty"`
	// NestedCollectionPath is a dot-separated path into the read response
	// (after the envelope unwrap) that locates the collection array(s) for a
	// child resource whose read is a parent GET (e.g. a port filter rule read
	// via GET /portFilters/{portId}, whose response unwraps to
	// {port, rules: {passRules, dropRules}} with the rules nested at
	// "rules.passRules"). The last segment may be "*" to search every array
	// value at that level, which sidesteps a collection split across sibling
	// arrays (passRules/dropRules) since the child's identifier is unique
	// across them. When set, the generated readRemote selects the element
	// whose identifier matches state (G39) instead of applying the whole
	// parent object. Empty for instance reads and direct collection reads
	// whose envelope is the array.
	NestedCollectionPath string                 `json:"nested_collection_path,omitempty"`
	SuccessCodes         []int                  `json:"success_codes,omitempty"`
	ErrorMappings        map[int]ErrorMappingIR `json:"error_mappings,omitempty"`
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
