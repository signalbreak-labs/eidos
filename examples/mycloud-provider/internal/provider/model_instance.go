package provider
// InstanceModel describes the API-facing shape for this resource.
// InstanceModelSpec describes the API-facing shape for this resource.
// InstanceModelSpecContainersElem describes the API-facing shape for this resource.
type InstanceModelSpecContainersElem struct {
	Image           *string `json:"image,omitempty"`
	ImagePullPolicy *string `json:"image_pull_policy,omitempty"`
	Name            *string `json:"name,omitempty"`
}
type InstanceModelSpec struct {
	Containers []InstanceModelSpecContainersElem `json:"containers,omitempty"`
}
// InstanceModelStatus describes the API-facing shape for this resource.
type InstanceModelStatus struct {
	Phase *string `json:"phase,omitempty"`
}
type InstanceModel struct {
	ApiVersion *string              `json:"api_version,omitempty"`
	Id         *string              `json:"id,omitempty"`
	Kind       *string              `json:"kind,omitempty"`
	Labels     map[string]string    `json:"labels,omitempty"`
	Name       string               `json:"name"`
	Spec       *InstanceModelSpec   `json:"spec,omitempty"`
	Status     *InstanceModelStatus `json:"status,omitempty"`
	Workspace  string               `json:"workspace"`
}
