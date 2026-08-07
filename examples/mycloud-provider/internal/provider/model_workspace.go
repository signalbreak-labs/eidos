package provider
// WorkspaceModel describes the API-facing shape for this resource.
// WorkspaceModelStatus describes the API-facing shape for this resource.
type WorkspaceModelStatus struct {
	Phase *string `json:"phase,omitempty"`
}
type WorkspaceModel struct {
	ApiVersion *string               `json:"api_version,omitempty"`
	Kind       *string               `json:"kind,omitempty"`
	Labels     map[string]string     `json:"labels,omitempty"`
	Name       string                `json:"name"`
	Status     *WorkspaceModelStatus `json:"status,omitempty"`
}
