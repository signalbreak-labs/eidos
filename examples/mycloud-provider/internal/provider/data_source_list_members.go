package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)
import (
	datasource "github.com/hashicorp/terraform-plugin-framework/datasource"
	schema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	types "github.com/hashicorp/terraform-plugin-framework/types"
	client "github.com/mycloud/terraform-provider-mycloud/internal/client"
)
// Compile-time interface assertion.
var (
	_ datasource.DataSource              = (*ListMembersDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*ListMembersDataSource)(nil)
)
// ListMembersDataSource is the generated Terraform data source implementation.
type ListMembersDataSource struct {
	client *client.Client
}
// ListMembersDataSourceModel describes the data source state shape.
type ListMembersDataSourceModel struct {
	Items types.List `tfsdk:"items"`
}
// NewListMembersDataSource returns a new instance of the generated data source.
func NewListMembersDataSource() datasource.DataSource {
	return &ListMembersDataSource{}
}
// Metadata returns the data source type name.
func (d *ListMembersDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "mycloud_list_members"
}
// Schema returns the data source schema.
func (d *ListMembersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Attributes: map[string]schema.Attribute{"items": schema.ListNestedAttribute{Computed: true, NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{"avatar_url": schema.StringAttribute{Computed: true}, "handle": schema.StringAttribute{Computed: true}, "html_url": schema.StringAttribute{Computed: true}, "id": schema.Int64Attribute{Computed: true}, "member": schema.StringAttribute{Computed: true}, "name": schema.StringAttribute{Computed: true}}}}}}
}
// Read fetches remote state into the data source model.
func (d *ListMembersDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config ListMembersDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil {
		{
			resp.Diagnostics.AddError("Client Not Configured", "The API client was not set on the resource. The provider Configure method must run before resource operations; this is a bug in the generated provider.")
			return
		}
	}
	reqPath := "/members"
	params := url.Values{}
	var nextURL string
	fetch := func(ctx context.Context, p url.Values) (*http.Response, error) {
		httpReq, err := d.client.NewRequest(ctx, http.MethodGet, reqPath, nil)
		if err != nil {
			{
				return nil, err
			}
		}
		if nextURL != "" {
			parsed, perr := url.Parse(nextURL)
			if perr != nil {
				{
					return nil, perr
				}
			}
			httpReq.URL = parsed
		} else {
			httpReq.URL.RawQuery = p.Encode()
		}
		return d.client.Do(httpReq)
	}
	pages, err := client.ListAllPages(ctx, params, fetch, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error reading mycloud_list_members", fmt.Sprintf("Could not read list response: %s", err))
		return
	}
	items := []any{}
	for _, page := range pages {
		pageItems := []any{}
		if err := json.Unmarshal(page, &pageItems); err != nil {
			resp.Diagnostics.AddError("Error reading mycloud_list_members", fmt.Sprintf("Could not decode list page: %s", err))
			return
		}
		items = append(items, pageItems...)
	}
	if err := applyJSONToModel(&config, map[string]any{"items": items}); err != nil {
		resp.Diagnostics.AddError("Error reading mycloud_list_members", fmt.Sprintf("Could not map response to state: %s", err))
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
// Configure stores the API client supplied by the provider.
func (d *ListMembersDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		{
			resp.Diagnostics.AddError("Unexpected Data Source Configure Type", fmt.Sprintf("Expected *client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData))
			return
		}
	}
	d.client = c
}
