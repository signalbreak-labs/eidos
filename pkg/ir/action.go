package ir

// ActionIR describes a Terraform invoke action: a side-effecting operation
// that does not participate in managed resource lifecycle.
//
// Struct-typed fields (ConfigSchema, InvokeMapping) are serialized
// unconditionally: encoding/json treats `omitempty` as a no-op on non-pointer
// struct values, so the tag is omitted here to avoid implying empty values are
// dropped. A zero ConfigSchema serializes as `"config_schema":{}`.
type ActionIR struct {
	Name                string             `json:"name"`
	FullName            string             `json:"full_name,omitempty"`
	TypeName            string             `json:"type_name,omitempty"`
	Description         string             `json:"description,omitempty"`
	MarkdownDescription string             `json:"markdown_description,omitempty"`
	ConfigSchema        ObjectSchemaIR     `json:"config_schema"`
	InvokeMapping       OperationMappingIR `json:"invoke_mapping"`
	ModifyPlan          bool               `json:"modify_plan,omitempty"`
	// ModifyPlanMapping and ValidateConfigMapping carry explicit preflight /
	// server-side validation endpoints declared via generator.yaml
	// (modify_plan_operation / validate_config_operation). They are never
	// auto-inferred: the spec does not encode preflight semantics (F3).
	ModifyPlanMapping     *OperationMappingIR `json:"modify_plan_mapping,omitempty"`
	ValidateConfigMapping *OperationMappingIR `json:"validate_config_mapping,omitempty"`
	ProgressMessages      bool                `json:"progress_messages,omitempty"`
	Tags                  []string            `json:"tags,omitempty"`
	SourceOperation       string              `json:"source_operation,omitempty"`
	// UnmarkableSensitiveAttrs records the wire names of string-typed
	// attributes whose names indicate a secret but that the action schema
	// cannot mark Sensitive (the plugin-framework action/schema package does
	// not support Sensitive). The generator renders a doc-page admonition
	// from this list so practitioners see that the values surface in plan
	// and state output (§3.6).
	UnmarkableSensitiveAttrs []string `json:"unmarkable_sensitive_attrs,omitempty"`
}
