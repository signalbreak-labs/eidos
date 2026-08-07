package provider
// ProjectModel describes the API-facing shape for this resource.
type ProjectModel struct {
	DefaultBranch *string `json:"default_branch,omitempty"`
	Description   *string `json:"description,omitempty"`
	FullName      *string `json:"full_name,omitempty"`
	HtmlUrl       *string `json:"html_url,omitempty"`
	Id            *int64  `json:"id,omitempty"`
	Name          *string `json:"name,omitempty"`
	Organization  *string `json:"organization,omitempty"`
	Private       *bool   `json:"private,omitempty"`
	Project       *string `json:"project,omitempty"`
}
