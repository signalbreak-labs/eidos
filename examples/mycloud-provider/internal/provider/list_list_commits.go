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
	diag "github.com/hashicorp/terraform-plugin-framework/diag"
	list "github.com/hashicorp/terraform-plugin-framework/list"
	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
	types "github.com/hashicorp/terraform-plugin-framework/types"
	tftypes "github.com/hashicorp/terraform-plugin-go/tftypes"
	client "github.com/mycloud/terraform-provider-mycloud/internal/client"
)

// Compile-time interface assertion.
var _ list.ListResource = (*ListCommitsListResource)(nil)
var _ list.ListResourceWithConfigure = (*ListCommitsListResource)(nil)

// ListCommitsListResource is the generated Terraform list resource implementation.
type ListCommitsListResource struct {
	client *client.Client
}

// ListCommitsListResourceModel describes the mycloud_list_commits list filter configuration shape.
type ListCommitsListResourceModel struct {
	Organization types.String `tfsdk:"organization"`
	Project      types.String `tfsdk:"project"`
}

// NewListCommitsListResource returns a new instance of the generated list resource.
func NewListCommitsListResource() list.ListResource {
	return &ListCommitsListResource{}
}

// Metadata returns the list resource type name.
func (l *ListCommitsListResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "mycloud_list_commits"
}

// ListResourceConfigSchema returns the list resource config schema.
func (l *ListCommitsListResource) ListResourceConfigSchema(_ context.Context, _ list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{MarkdownDescription: "List commits", Attributes: map[string]listschema.Attribute{"organization": listschema.StringAttribute{Required: true}, "project": listschema.StringAttribute{Required: true}}}
}

// List streams matching resource instances for terraform query.
func (l *ListCommitsListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	stream.Results = func(push func(list.ListResult) bool) {
		var config ListCommitsListResourceModel
		diags := req.Config.Get(ctx, &config)
		if diags.HasError() {
			result := req.NewListResult(ctx)
			result.Diagnostics = diags
			push(result)
			return
		}
		items, diags := l.listRemote(ctx, &config)
		if diags.HasError() {
			result := req.NewListResult(ctx)
			result.Diagnostics = diags
			push(result)
			return
		}
		for _, item := range items {
			result := req.NewListResult(ctx)
			itemMap := map[string]json.RawMessage{}
			if err := json.Unmarshal(item, &itemMap); err != nil {
				result.Diagnostics.AddError("Error listing mycloud_list_commits", fmt.Sprintf("Could not decode list item: %s", err))
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
				result.Diagnostics.AddError("Error listing mycloud_list_commits", "List item is missing identity attribute \"organization\".")
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
				result.Diagnostics.AddError("Error listing mycloud_list_commits", "List item is missing identity attribute \"project\".")
				if !push(result) {
					return
				}
				continue
			}
			identity["project"] = projectValue
			refValue, ok := itemMap["ref"]
			if !ok {
				if itemMap["metadata"] != nil {
					metaMap := map[string]json.RawMessage{}
					if json.Unmarshal(itemMap["metadata"], &metaMap) == nil {
						refValue, ok = metaMap["ref"]
					}
				}
			}
			if !ok {
				refValue, ok = itemMap["id"]
			}
			if !ok {
				result.Diagnostics.AddError("Error listing mycloud_list_commits", "List item is missing identity attribute \"ref\".")
				if !push(result) {
					return
				}
				continue
			}
			identity["ref"] = refValue
			idJSON, err := json.Marshal(identity)
			if err != nil {
				result.Diagnostics.AddError("Error listing mycloud_list_commits", fmt.Sprintf("Could not encode list item identity: %s", err))
				if !push(result) {
					return
				}
				continue
			}
			idVal, err := tftypes.ValueFromJSON(idJSON, req.ResourceIdentitySchema.Type().TerraformType(ctx))
			if err != nil {
				result.Diagnostics.AddError("Error listing mycloud_list_commits", fmt.Sprintf("Could not decode list item identity: %s", err))
				if !push(result) {
					return
				}
				continue
			}
			result.Identity.Raw = idVal
			if req.IncludeResource {
				resVal, err := tftypes.ValueFromJSON(item, req.ResourceSchema.Type().TerraformType(ctx))
				if err != nil {
					result.Diagnostics.AddWarning("Error listing mycloud_list_commits", fmt.Sprintf("Could not decode list item into the resource schema: %s", err))
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

// listRemote fetches and decodes the collection pages, returning the items and any diagnostics for the List iterator to surface.
func (l *ListCommitsListResource) listRemote(ctx context.Context, config *ListCommitsListResourceModel) ([]json.RawMessage, diag.Diagnostics) {
	var diags diag.Diagnostics
	if l.client == nil {
		diags.AddError("Client Not Configured", "The API client was not set on the list resource. The provider Configure method must run before list operations; this is a bug in the generated provider.")
		return nil, diags
	}
	reqPath := "/organizations/{organization}/projects/{project}/commits"
	reqPath = strings.ReplaceAll(reqPath, "{organization}", url.PathEscape(config.Organization.ValueString()))
	reqPath = strings.ReplaceAll(reqPath, "{project}", url.PathEscape(config.Project.ValueString()))
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
		diags.AddError("Error listing mycloud_list_commits", fmt.Sprintf("Could not read list response: %s", err))
		return nil, diags
	}
	allItems := []json.RawMessage{}
	for _, page := range pages {
		items := []json.RawMessage{}
		if err := json.Unmarshal(page, &items); err != nil {
			diags.AddError("Error listing mycloud_list_commits", fmt.Sprintf("Could not decode list page: %s", err))
			return nil, diags
		}
		allItems = append(allItems, items...)
	}
	return allItems, diags
}

// Configure stores the API client supplied by the provider.
func (l *ListCommitsListResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
