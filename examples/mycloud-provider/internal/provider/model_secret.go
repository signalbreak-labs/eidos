package provider
// SecretModel describes the API-facing shape for this resource.
type SecretModel struct {
	ApiVersion *string           `json:"api_version,omitempty"`
	Data       map[string]string `json:"data,omitempty"`
	Id         *string           `json:"id,omitempty"`
	Kind       *string           `json:"kind,omitempty"`
	Name       string            `json:"name"`
	Type       *string           `json:"type,omitempty"`
	Workspace  string            `json:"workspace"`
}
