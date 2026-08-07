package ir

// ValidatorIR captures metadata about a Terraform-plugin-framework validator
// that should be attached to an attribute. The Type field names the validator
// constructor (e.g., "stringvalidator.OneOf") and Args holds literal argument
// values to pass to that constructor when code is generated.
type ValidatorIR struct {
	Type string   `json:"type,omitempty"`
	Args []string `json:"args,omitempty"`
}
