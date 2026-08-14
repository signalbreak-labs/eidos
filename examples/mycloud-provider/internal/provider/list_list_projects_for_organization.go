package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)
import (
	list "github.com/hashicorp/terraform-plugin-framework/list"
	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
	types "github.com/hashicorp/terraform-plugin-framework/types"
	tftypes "github.com/hashicorp/terraform-plugin-go/tftypes"
	client "github.com/mycloud/terraform-provider-mycloud/internal/client"
)

// Compile-time interface assertion.
var _ list.ListResource = (*ListProjectsForOrganizationListResource)(nil)
var _ list.ListResourceWithConfigure = (*ListProjectsForOrganizationListResource)(nil)

// ListProjectsForOrganizationListResource is the generated Terraform list resource implementation.
type ListProjectsForOrganizationListResource struct {
	client *client.Client
}

// ListProjectsForOrganizationListResourceModel describes the mycloud_list_projects_for_organization list filter configuration shape.
type ListProjectsForOrganizationListResourceModel struct {
	Organization types.String `tfsdk:"organization"`
}

// NewListProjectsForOrganizationListResource returns a new instance of the generated list resource.
func NewListProjectsForOrganizationListResource() list.ListResource {
	return &ListProjectsForOrganizationListResource{}
}

// Metadata returns the list resource type name.
func (l *ListProjectsForOrganizationListResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "mycloud_list_projects_for_organization"
}

// ListResourceConfigSchema returns the list resource config schema.
func (l *ListProjectsForOrganizationListResource) ListResourceConfigSchema(_ context.Context, _ list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{MarkdownDescription: "List organization projects", Attributes: map[string]listschema.Attribute{"organization": listschema.StringAttribute{Required: true}}}
}

// List streams matching resource instances for terraform query.
func (l *ListProjectsForOrganizationListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	stream.Results = func(push func(list.ListResult) bool) {
		pushError := func(summary string, detail string) {
			result := req.NewListResult(ctx)
			result.Diagnostics.AddError(summary, detail)
			push(result)
		}
		var config ListProjectsForOrganizationListResourceModel
		diags := req.Config.Get(ctx, &config)
		if diags.HasError() {
			result := req.NewListResult(ctx)
			result.Diagnostics = diags
			push(result)
			return
		}
		if l.client == nil {
			pushError("Client Not Configured", "The API client was not set on the list resource. The provider Configure method must run before list operations; this is a bug in the generated provider.")
			return
		}
		reqPath := "/organizations/{organization}/projects"
		reqPath = strings.ReplaceAll(reqPath, "{organization}", url.PathEscape(config.Organization.ValueString()))
		params := url.Values{}
		var nextURL string
		fetch := func(ctx context.Context, p url.Values) (*http.Response, error) {
			httpReq, err := l.client.NewRequest(ctx, http.MethodGet, reqPath, nil)
			if err != nil {
				return nil, err
			}
			if nextURL != "" {
				parsed, perr := url.Parse(nextURL)
				if perr != nil {
					return nil, perr
				}
				httpReq.URL = parsed
			} else {
				httpReq.URL.RawQuery = p.Encode()
			}
			return l.client.Do(httpReq)
		}
		pages, err := client.ListAllPages(ctx, params, fetch, nil)
		if err != nil {
			pushError("Error listing mycloud_list_projects_for_organization", fmt.Sprintf("Could not read list response: %s", err))
			return
		}
		for _, page := range pages {
			items := []json.RawMessage{}
			if err := json.Unmarshal(page, &items); err != nil {
				pushError("Error listing mycloud_list_projects_for_organization", fmt.Sprintf("Could not decode list page: %s", err))
				return
			}
			for _, item := range items {
				result := req.NewListResult(ctx)
				itemMap := map[string]json.RawMessage{}
				if err := json.Unmarshal(item, &itemMap); err != nil {
					result.Diagnostics.AddError("Error listing mycloud_list_projects_for_organization", fmt.Sprintf("Could not decode list item: %s", err))
					if !push(result) {
						return
					}
					continue
				}
				identity := map[string]json.RawMessage{}
				organizationValue, ok := itemMap["organization"]
				if !ok {
					if itemMap["metadata"] != nil {
						metaMap := map[string]json.RawMessage{}
						if json.Unmarshal(itemMap["metadata"], &metaMap) == nil {
							organizationValue, ok = metaMap["organization"]
						}
					}
				}
				if !ok {
					organizationValue, ok = itemMap["id"]
				}
				if !ok {
					result.Diagnostics.AddError("Error listing mycloud_list_projects_for_organization", "List item is missing identity attribute \"organization\".")
					if !push(result) {
						return
					}
					continue
				}
				identity["organization"] = organizationValue
				projectValue, ok := itemMap["project"]
				if !ok {
					if itemMap["metadata"] != nil {
						metaMap := map[string]json.RawMessage{}
						if json.Unmarshal(itemMap["metadata"], &metaMap) == nil {
							projectValue, ok = metaMap["project"]
						}
					}
				}
				if !ok {
					projectValue, ok = itemMap["id"]
				}
				if !ok {
					result.Diagnostics.AddError("Error listing mycloud_list_projects_for_organization", "List item is missing identity attribute \"project\".")
					if !push(result) {
						return
					}
					continue
				}
				identity["project"] = projectValue
				idJSON, err := json.Marshal(identity)
				if err != nil {
					result.Diagnostics.AddError("Error listing mycloud_list_projects_for_organization", fmt.Sprintf("Could not encode list item identity: %s", err))
					if !push(result) {
						return
					}
					continue
				}
				idVal, err := tftypes.ValueFromJSON(idJSON, req.ResourceIdentitySchema.Type().TerraformType(ctx))
				if err != nil {
					result.Diagnostics.AddError("Error listing mycloud_list_projects_for_organization", fmt.Sprintf("Could not decode list item identity: %s", err))
					if !push(result) {
						return
					}
					continue
				}
				result.Identity.Raw = idVal
				if req.IncludeResource {
					resVal, err := tftypes.ValueFromJSON(item, req.ResourceSchema.Type().TerraformType(ctx))
					if err != nil {
						result.Diagnostics.AddWarning("Error listing mycloud_list_projects_for_organization", fmt.Sprintf("Could not decode list item into the resource schema: %s", err))
					} else {
						result.Resource.Raw = resVal
					}
				}
				if !push(result) {
					return
				}
			}
		}
	}
}

// Configure stores the API client supplied by the provider.
func (l *ListProjectsForOrganizationListResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected List Resource Configure Type", fmt.Sprintf("Expected *client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData))
		return
	}
	l.client = c
}
