package provider
// ConfigModel describes the API-facing shape for this resource.
type ConfigModel struct {
	ApiVersion *string           `json:"api_version,omitempty"`
	Data       map[string]string `json:"data,omitempty"`
	Id         *string           `json:"id,omitempty"`
	Kind       *string           `json:"kind,omitempty"`
	Name       string            `json:"name"`
	Workspace  string            `json:"workspace"`
}
