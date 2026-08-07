package provider
// StackModel describes the API-facing shape for this resource.
// StackModelSpec describes the API-facing shape for this resource.
type StackModelSpec struct {
	Replicas *int64            `json:"replicas,omitempty"`
	Selector map[string]string `json:"selector,omitempty"`
}
// StackModelStatus describes the API-facing shape for this resource.
type StackModelStatus struct {
	ReadyReplicas *int64 `json:"ready_replicas,omitempty"`
}
type StackModel struct {
	ApiVersion *string           `json:"api_version,omitempty"`
	Id         *string           `json:"id,omitempty"`
	Kind       *string           `json:"kind,omitempty"`
	Name       string            `json:"name"`
	Spec       *StackModelSpec   `json:"spec,omitempty"`
	Status     *StackModelStatus `json:"status,omitempty"`
	Workspace  string            `json:"workspace"`
}
