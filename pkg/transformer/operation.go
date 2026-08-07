package transformer

// OpenAPIOperation is a version-agnostic OpenAPI operation used during normalization
// before it is transformed into the Terraform-oriented IR.
type OpenAPIOperation struct {
	OpenAPIParameters []OpenAPIParameter
	Security          []SecurityRequirement
}

// PathItem is a version-agnostic OpenAPI path item used during normalization.
// It holds parameters and security settings that apply to all operations under
// the path unless overridden by an individual operation.
type PathItem struct {
	OpenAPIParameters []OpenAPIParameter
	Security          []SecurityRequirement
}

// SecurityRequirement maps a security scheme name to the list of OAuth scopes
// (or an empty list for non-OAuth schemes) required to invoke an operation.
type SecurityRequirement map[string][]string
