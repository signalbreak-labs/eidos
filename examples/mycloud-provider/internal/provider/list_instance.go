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
var _ list.ListResource = (*InstanceListResource)(nil)
var _ list.ListResourceWithConfigure = (*InstanceListResource)(nil)

// InstanceListResource is the generated Terraform list resource implementation.
type InstanceListResource struct {
	client *client.Client
}

// InstanceListResourceModel describes the mycloud_instance list filter configuration shape.
type InstanceListResourceModel struct {
	Workspace types.String `tfsdk:"workspace"`
}

// NewInstanceListResource returns a new instance of the generated list resource.
func NewInstanceListResource() list.ListResource {
	return &InstanceListResource{}
}

// Metadata returns the list resource type name.
func (l *InstanceListResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "mycloud_instance"
}

// ListResourceConfigSchema returns the list resource config schema.
func (l *InstanceListResource) ListResourceConfigSchema(_ context.Context, _ list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{MarkdownDescription: "List Instances", Attributes: map[string]listschema.Attribute{"workspace": listschema.StringAttribute{Required: true}}}
}

// List streams matching resource instances for terraform query.
func (l *InstanceListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	stream.Results = func(push func(list.ListResult) bool) {
		var config InstanceListResourceModel
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
				result.Diagnostics.AddError("Error listing mycloud_instance", fmt.Sprintf("Could not decode list item: %s", err))
				if !push(result) {
					return
				}
				continue
			}
			identity := map[string]json.RawMessage{}
			workspaceValue, ok := itemMap["workspace"]
			if !ok {
				if itemMap["metadata"] != nil {
					metaMap := map[string]json.RawMessage{}
					if json.Unmarshal(itemMap["metadata"], &metaMap) == nil {
						workspaceValue, ok = metaMap["workspace"]
					}
				}
			}
			if !ok {
				workspaceValue, ok = itemMap["id"]
			}
			if !ok {
				result.Diagnostics.AddError("Error listing mycloud_instance", "List item is missing identity attribute \"workspace\".")
				if !push(result) {
					return
				}
				continue
			}
			identity["workspace"] = workspaceValue
			nameValue, ok := itemMap["name"]
			if !ok {
				if itemMap["metadata"] != nil {
					metaMap := map[string]json.RawMessage{}
					if json.Unmarshal(itemMap["metadata"], &metaMap) == nil {
						nameValue, ok = metaMap["name"]
					}
				}
			}
			if !ok {
				nameValue, ok = itemMap["id"]
			}
			if !ok {
				result.Diagnostics.AddError("Error listing mycloud_instance", "List item is missing identity attribute \"name\".")
				if !push(result) {
					return
				}
				continue
			}
			identity["name"] = nameValue
			idJSON, err := json.Marshal(identity)
			if err != nil {
				result.Diagnostics.AddError("Error listing mycloud_instance", fmt.Sprintf("Could not encode list item identity: %s", err))
				if !push(result) {
					return
				}
				continue
			}
			idVal, err := tftypes.ValueFromJSON(idJSON, req.ResourceIdentitySchema.Type().TerraformType(ctx))
			if err != nil {
				result.Diagnostics.AddError("Error listing mycloud_instance", fmt.Sprintf("Could not decode list item identity: %s", err))
				if !push(result) {
					return
				}
				continue
			}
			result.Identity.Raw = idVal
			if req.IncludeResource {
				resVal, err := tftypes.ValueFromJSON(item, req.ResourceSchema.Type().TerraformType(ctx))
				if err != nil {
					result.Diagnostics.AddWarning("Error listing mycloud_instance", fmt.Sprintf("Could not decode list item into the resource schema: %s", err))
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
func (l *InstanceListResource) listRemote(ctx context.Context, config *InstanceListResourceModel) ([]json.RawMessage, diag.Diagnostics) {
	var diags diag.Diagnostics
	if l.client == nil {
		diags.AddError("Client Not Configured", "The API client was not set on the list resource. The provider Configure method must run before list operations; this is a bug in the generated provider.")
		return nil, diags
	}
	reqPath := "/workspaces/{workspace}/instances"
	reqPath = strings.ReplaceAll(reqPath, "{workspace}", url.PathEscape(config.Workspace.ValueString()))
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
		diags.AddError("Error listing mycloud_instance", fmt.Sprintf("Could not read list response: %s", err))
		return nil, diags
	}
	allItems := []json.RawMessage{}
	for _, page := range pages {
		items := []json.RawMessage{}
		if err := json.Unmarshal(page, &items); err != nil {
			diags.AddError("Error listing mycloud_instance", fmt.Sprintf("Could not decode list page: %s", err))
			return nil, diags
		}
		allItems = append(allItems, items...)
	}
	return allItems, diags
}

// Configure stores the API client supplied by the provider.
func (l *InstanceListResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
