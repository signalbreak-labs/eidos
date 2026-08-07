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
	_ datasource.DataSource              = (*ListWorkspacesDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*ListWorkspacesDataSource)(nil)
)
// ListWorkspacesDataSource is the generated Terraform data source implementation.
type ListWorkspacesDataSource struct {
	client *client.Client
}
// ListWorkspacesDataSourceModel describes the data source state shape.
type ListWorkspacesDataSourceModel struct {
	Items types.List `tfsdk:"items"`
}
// NewListWorkspacesDataSource returns a new instance of the generated data source.
func NewListWorkspacesDataSource() datasource.DataSource {
	return &ListWorkspacesDataSource{}
}
// Metadata returns the data source type name.
func (d *ListWorkspacesDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "mycloud_list_workspaces"
}
// Schema returns the data source schema.
func (d *ListWorkspacesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Attributes: map[string]schema.Attribute{"items": schema.ListNestedAttribute{Computed: true, NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{"api_version": schema.StringAttribute{Computed: true}, "kind": schema.StringAttribute{Computed: true}, "labels": schema.MapAttribute{Computed: true, ElementType: types.StringType}, "name": schema.StringAttribute{Computed: true}, "status": schema.SingleNestedAttribute{Computed: true, Attributes: map[string]schema.Attribute{"phase": schema.StringAttribute{Computed: true}}}}}}}}
}
// Read fetches remote state into the data source model.
func (d *ListWorkspacesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config ListWorkspacesDataSourceModel
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
	reqPath := "/workspaces"
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
		resp.Diagnostics.AddError("Error reading mycloud_list_workspaces", fmt.Sprintf("Could not read list response: %s", err))
		return
	}
	items := []any{}
	for _, page := range pages {
		pageItems := []any{}
		if err := json.Unmarshal(page, &pageItems); err != nil {
			resp.Diagnostics.AddError("Error reading mycloud_list_workspaces", fmt.Sprintf("Could not decode list page: %s", err))
			return
		}
		items = append(items, pageItems...)
	}
	if err := applyJSONToModel(&config, map[string]any{"items": items}); err != nil {
		resp.Diagnostics.AddError("Error reading mycloud_list_workspaces", fmt.Sprintf("Could not map response to state: %s", err))
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
// Configure stores the API client supplied by the provider.
func (d *ListWorkspacesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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
