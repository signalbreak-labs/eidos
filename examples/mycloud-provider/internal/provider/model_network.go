package provider

import "github.com/hashicorp/terraform-plugin-go/tftypes"
// NetworkModel describes the API-facing shape for this resource.
// NetworkModelSpec describes the API-facing shape for this resource.
// NetworkModelSpecPortsElem describes the API-facing shape for this resource.
type NetworkModelSpecPortsElem struct {
	Name     *string `json:"name,omitempty"`
	Port     *int64  `json:"port,omitempty"`
	Protocol *string `json:"protocol,omitempty"`
}
type NetworkModelSpec struct {
	IpAddress *string                     `json:"ip_address,omitempty"`
	Ports     []NetworkModelSpecPortsElem `json:"ports,omitempty"`
	Selector  map[string]string           `json:"selector,omitempty"`
}
// NetworkModelStatus describes the API-facing shape for this resource.
type NetworkModelStatus struct {
	LoadBalancer tftypes.Value `json:"load_balancer,omitempty"`
}
type NetworkModel struct {
	ApiVersion *string             `json:"api_version,omitempty"`
	Id         *string             `json:"id,omitempty"`
	Kind       *string             `json:"kind,omitempty"`
	Name       string              `json:"name"`
	Spec       *NetworkModelSpec   `json:"spec,omitempty"`
	Status     *NetworkModelStatus `json:"status,omitempty"`
	Workspace  string              `json:"workspace"`
}
