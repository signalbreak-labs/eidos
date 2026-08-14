package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)
import (
	list "github.com/hashicorp/terraform-plugin-framework/list"
	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
	tftypes "github.com/hashicorp/terraform-plugin-go/tftypes"
	client "github.com/mycloud/terraform-provider-mycloud/internal/client"
)

// Compile-time interface assertion.
var _ list.ListResource = (*ListMembersListResource)(nil)
var _ list.ListResourceWithConfigure = (*ListMembersListResource)(nil)

// ListMembersListResource is the generated Terraform list resource implementation.
type ListMembersListResource struct {
	client *client.Client
}

// NewListMembersListResource returns a new instance of the generated list resource.
func NewListMembersListResource() list.ListResource {
	return &ListMembersListResource{}
}

// Metadata returns the list resource type name.
func (l *ListMembersListResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "mycloud_list_members"
}

// ListResourceConfigSchema returns the list resource config schema.
func (l *ListMembersListResource) ListResourceConfigSchema(_ context.Context, _ list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{MarkdownDescription: "List members"}
}

// List streams matching resource instances for terraform query.
func (l *ListMembersListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	stream.Results = func(push func(list.ListResult) bool) {
		pushError := func(summary string, detail string) {
			result := req.NewListResult(ctx)
			result.Diagnostics.AddError(summary, detail)
			push(result)
		}
		if l.client == nil {
			pushError("Client Not Configured", "The API client was not set on the list resource. The provider Configure method must run before list operations; this is a bug in the generated provider.")
			return
		}
		reqPath := "/members"
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
			pushError("Error listing mycloud_list_members", fmt.Sprintf("Could not read list response: %s", err))
			return
		}
		for _, page := range pages {
			items := []json.RawMessage{}
			if err := json.Unmarshal(page, &items); err != nil {
				pushError("Error listing mycloud_list_members", fmt.Sprintf("Could not decode list page: %s", err))
				return
			}
			for _, item := range items {
				result := req.NewListResult(ctx)
				itemMap := map[string]json.RawMessage{}
				if err := json.Unmarshal(item, &itemMap); err != nil {
					result.Diagnostics.AddError("Error listing mycloud_list_members", fmt.Sprintf("Could not decode list item: %s", err))
					if !push(result) {
						return
					}
					continue
				}
				identity := map[string]json.RawMessage{}
				memberValue, ok := itemMap["member"]
				if !ok {
					if itemMap["metadata"] != nil {
						metaMap := map[string]json.RawMessage{}
						if json.Unmarshal(itemMap["metadata"], &metaMap) == nil {
							memberValue, ok = metaMap["member"]
						}
					}
				}
				if !ok {
					memberValue, ok = itemMap["id"]
				}
				if !ok {
					result.Diagnostics.AddError("Error listing mycloud_list_members", "List item is missing identity attribute \"member\".")
					if !push(result) {
						return
					}
					continue
				}
				identity["member"] = memberValue
				idJSON, err := json.Marshal(identity)
				if err != nil {
					result.Diagnostics.AddError("Error listing mycloud_list_members", fmt.Sprintf("Could not encode list item identity: %s", err))
					if !push(result) {
						return
					}
					continue
				}
				idVal, err := tftypes.ValueFromJSON(idJSON, req.ResourceIdentitySchema.Type().TerraformType(ctx))
				if err != nil {
					result.Diagnostics.AddError("Error listing mycloud_list_members", fmt.Sprintf("Could not decode list item identity: %s", err))
					if !push(result) {
						return
					}
					continue
				}
				result.Identity.Raw = idVal
				if req.IncludeResource {
					resVal, err := tftypes.ValueFromJSON(item, req.ResourceSchema.Type().TerraformType(ctx))
					if err != nil {
						result.Diagnostics.AddWarning("Error listing mycloud_list_members", fmt.Sprintf("Could not decode list item into the resource schema: %s", err))
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
func (l *ListMembersListResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
