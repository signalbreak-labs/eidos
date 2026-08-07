package ir

// ServerIR describes a server URL template and its variables from the OpenAPI
// servers list.
type ServerIR struct {
	URL         string                      `json:"url,omitempty"`
	Description string                      `json:"description,omitempty"`
	Variables   map[string]ServerVariableIR `json:"variables,omitempty"`
}

// ServerVariableIR describes a single server URL template variable.
type ServerVariableIR struct {
	Default     string   `json:"default,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Description string   `json:"description,omitempty"`
}
