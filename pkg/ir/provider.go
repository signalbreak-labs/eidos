package ir

// ProviderIR describes the top-level intermediate representation of a
// generated Terraform provider. It aggregates all resources, data sources,
// actions, ephemeral resources, list resources, functions, and supporting
// configuration metadata produced from an OpenAPI specification.
//
// Struct-typed fields (ConfigSchema, ClientIR, SecurityIR) are serialized
// unconditionally; encoding/json treats `omitempty` as a no-op on non-pointer
// struct values, so the tag is omitted here rather than misleadingly implying
// empty values drop.
type ProviderIR struct {
	Name                   string                `json:"name"`
	FullName               string                `json:"full_name,omitempty"`
	TypeName               string                `json:"type_name,omitempty"`
	Version                string                `json:"version,omitempty"`
	Description            string                `json:"description,omitempty"`
	SourceSpec             string                `json:"source_spec,omitempty"`
	SourceSpecVersion      string                `json:"source_spec_version,omitempty"`
	GenerateTerraformTests bool                  `json:"generate_terraform_tests,omitempty"`
	ConfigSchema           ObjectSchemaIR        `json:"config_schema"`
	Resources              []ResourceIR          `json:"resources,omitempty"`
	DataSources            []DataSourceIR        `json:"data_sources,omitempty"`
	Actions                []ActionIR            `json:"actions,omitempty"`
	EphemeralResources     []EphemeralResourceIR `json:"ephemeral_resources,omitempty"`
	ListResources          []ListResourceIR      `json:"list_resources,omitempty"`
	Functions              []FunctionIR          `json:"functions,omitempty"`
	ClientIR               ClientIR              `json:"client"`
	SecurityIR             SecurityIR            `json:"security"`
	Servers                []ServerIR            `json:"servers,omitempty"`
}
