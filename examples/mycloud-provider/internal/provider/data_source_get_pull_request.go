package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
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
	_ datasource.DataSource              = (*GetPullRequestDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*GetPullRequestDataSource)(nil)
)
// GetPullRequestDataSource is the generated Terraform data source implementation.
type GetPullRequestDataSource struct {
	client *client.Client
}
// GetPullRequestDataSourceModel describes the data source state shape.
type GetPullRequestDataSourceModel struct {
	Body         types.String `tfsdk:"body"`
	HtmlUrl      types.String `tfsdk:"html_url"`
	Id           types.Int64  `tfsdk:"id"`
	Merged       types.Bool   `tfsdk:"merged"`
	Number       types.Int64  `tfsdk:"number"`
	Organization types.String `tfsdk:"organization"`
	Project      types.String `tfsdk:"project"`
	PullNumber   types.Int64  `tfsdk:"pull_number"`
	State        types.String `tfsdk:"state"`
	Title        types.String `tfsdk:"title"`
}
// NewGetPullRequestDataSource returns a new instance of the generated data source.
func NewGetPullRequestDataSource() datasource.DataSource {
	return &GetPullRequestDataSource{}
}
// Metadata returns the data source type name.
func (d *GetPullRequestDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "mycloud_get_pull_request"
}
// Schema returns the data source schema.
func (d *GetPullRequestDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Attributes: map[string]schema.Attribute{"body": schema.StringAttribute{Computed: true}, "html_url": schema.StringAttribute{Computed: true}, "id": schema.Int64Attribute{Computed: true}, "merged": schema.BoolAttribute{Computed: true}, "number": schema.Int64Attribute{Computed: true}, "organization": schema.StringAttribute{Required: true}, "project": schema.StringAttribute{Required: true}, "pull_number": schema.Int64Attribute{Required: true}, "state": schema.StringAttribute{Computed: true}, "title": schema.StringAttribute{Computed: true}}}
}
// Read fetches remote state into the data source model.
func (d *GetPullRequestDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config GetPullRequestDataSourceModel
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
	reqPath := "/organizations/{organization}/projects/{project}/pull_requests/{pull_number}"
	reqPath = strings.ReplaceAll(reqPath, "{organization}", config.Organization.ValueString())
	reqPath = strings.ReplaceAll(reqPath, "{project}", config.Project.ValueString())
	reqPath = strings.ReplaceAll(reqPath, "{pull_number}", strconv.FormatInt(config.PullNumber.ValueInt64(), 10))
	httpReq, err := d.client.NewRequest(ctx, http.MethodGet, reqPath, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error reading mycloud_get_pull_request", fmt.Sprintf("Could not build request: %s", err))
		return
	}
	httpResp, err := d.client.Do(httpReq)
	if err != nil {
		resp.Diagnostics.AddError("Error reading mycloud_get_pull_request", fmt.Sprintf("Could not send request: %s", err))
		return
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode == http.StatusNotFound {
		resp.Diagnostics.AddError("Error reading mycloud_get_pull_request", "The requested resource was not found.")
		return
	}
	if !(httpResp.StatusCode == 200) {
		apiErr, err := client.NewAPIError(httpResp)
		if err != nil {
			resp.Diagnostics.AddError("Error reading mycloud_get_pull_request", fmt.Sprintf("Could not read error response: %s", err))
			return
		}
		resp.Diagnostics.AddError("Error reading mycloud_get_pull_request", apiErr.Error())
		return
	}
	var data map[string]any
	decoder := json.NewDecoder(httpResp.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&data); err != nil && err != io.EOF {
		resp.Diagnostics.AddError("Error reading mycloud_get_pull_request", fmt.Sprintf("Could not decode response body: %s", err))
		return
	}
	err = applyJSONToModel(&config, data)
	if err != nil {
		resp.Diagnostics.AddError("Error reading mycloud_get_pull_request", fmt.Sprintf("Could not map response to state: %s", err))
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
// Configure stores the API client supplied by the provider.
func (d *GetPullRequestDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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
