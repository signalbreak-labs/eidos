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
	datasource "github.com/hashicorp/terraform-plugin-framework/datasource"
	schema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	types "github.com/hashicorp/terraform-plugin-framework/types"
	client "github.com/mycloud/terraform-provider-mycloud/internal/client"
)
// Compile-time interface assertion.
var (
	_ datasource.DataSource              = (*ListTasksDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*ListTasksDataSource)(nil)
)
// ListTasksDataSource is the generated Terraform data source implementation.
type ListTasksDataSource struct {
	client *client.Client
}
// ListTasksDataSourceModel describes the data source state shape.
type ListTasksDataSourceModel struct {
	Items        types.List   `tfsdk:"items"`
	Organization types.String `tfsdk:"organization"`
	Project      types.String `tfsdk:"project"`
}
// NewListTasksDataSource returns a new instance of the generated data source.
func NewListTasksDataSource() datasource.DataSource {
	return &ListTasksDataSource{}
}
// Metadata returns the data source type name.
func (d *ListTasksDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "mycloud_list_tasks"
}
// Schema returns the data source schema.
func (d *ListTasksDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Attributes: map[string]schema.Attribute{"items": schema.ListNestedAttribute{Computed: true, NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{"body": schema.StringAttribute{Computed: true}, "html_url": schema.StringAttribute{Computed: true}, "id": schema.Int64Attribute{Computed: true}, "number": schema.Int64Attribute{Computed: true}, "organization": schema.StringAttribute{Computed: true}, "project": schema.StringAttribute{Computed: true}, "state": schema.StringAttribute{Computed: true}, "task_number": schema.Int64Attribute{Computed: true}, "title": schema.StringAttribute{Computed: true}}}}, "organization": schema.StringAttribute{Required: true}, "project": schema.StringAttribute{Required: true}}}
}
// Read fetches remote state into the data source model.
func (d *ListTasksDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config ListTasksDataSourceModel
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
	reqPath := "/organizations/{organization}/projects/{project}/tasks"
	reqPath = strings.ReplaceAll(reqPath, "{organization}", config.Organization.ValueString())
	reqPath = strings.ReplaceAll(reqPath, "{project}", config.Project.ValueString())
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
		resp.Diagnostics.AddError("Error reading mycloud_list_tasks", fmt.Sprintf("Could not read list response: %s", err))
		return
	}
	items := []any{}
	for _, page := range pages {
		pageItems := []any{}
		if err := json.Unmarshal(page, &pageItems); err != nil {
			resp.Diagnostics.AddError("Error reading mycloud_list_tasks", fmt.Sprintf("Could not decode list page: %s", err))
			return
		}
		items = append(items, pageItems...)
	}
	if err := applyJSONToModel(&config, map[string]any{"items": items}); err != nil {
		resp.Diagnostics.AddError("Error reading mycloud_list_tasks", fmt.Sprintf("Could not map response to state: %s", err))
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
// Configure stores the API client supplied by the provider.
func (d *ListTasksDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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
