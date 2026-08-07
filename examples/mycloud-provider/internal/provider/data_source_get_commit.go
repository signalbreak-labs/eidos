package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	_ datasource.DataSource              = (*GetCommitDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*GetCommitDataSource)(nil)
)
// GetCommitDataSource is the generated Terraform data source implementation.
type GetCommitDataSource struct {
	client *client.Client
}
// GetCommitDataSourceModel describes the data source state shape.
type GetCommitDataSourceModel struct {
	AuthorName   types.String `tfsdk:"author_name"`
	CommittedAt  types.String `tfsdk:"committed_at"`
	Message      types.String `tfsdk:"message"`
	Organization types.String `tfsdk:"organization"`
	Project      types.String `tfsdk:"project"`
	Ref          types.String `tfsdk:"ref"`
	Sha          types.String `tfsdk:"sha"`
}
// NewGetCommitDataSource returns a new instance of the generated data source.
func NewGetCommitDataSource() datasource.DataSource {
	return &GetCommitDataSource{}
}
// Metadata returns the data source type name.
func (d *GetCommitDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "mycloud_get_commit"
}
// Schema returns the data source schema.
func (d *GetCommitDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Attributes: map[string]schema.Attribute{"author_name": schema.StringAttribute{Computed: true}, "committed_at": schema.StringAttribute{Computed: true}, "message": schema.StringAttribute{Computed: true}, "organization": schema.StringAttribute{Required: true}, "project": schema.StringAttribute{Required: true}, "ref": schema.StringAttribute{Required: true}, "sha": schema.StringAttribute{Computed: true}}}
}
// Read fetches remote state into the data source model.
func (d *GetCommitDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config GetCommitDataSourceModel
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
	reqPath := "/organizations/{organization}/projects/{project}/commits/{ref}"
	reqPath = strings.ReplaceAll(reqPath, "{organization}", config.Organization.ValueString())
	reqPath = strings.ReplaceAll(reqPath, "{project}", config.Project.ValueString())
	reqPath = strings.ReplaceAll(reqPath, "{ref}", config.Ref.ValueString())
	httpReq, err := d.client.NewRequest(ctx, http.MethodGet, reqPath, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error reading mycloud_get_commit", fmt.Sprintf("Could not build request: %s", err))
		return
	}
	httpResp, err := d.client.Do(httpReq)
	if err != nil {
		resp.Diagnostics.AddError("Error reading mycloud_get_commit", fmt.Sprintf("Could not send request: %s", err))
		return
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode == http.StatusNotFound {
		resp.Diagnostics.AddError("Error reading mycloud_get_commit", "The requested resource was not found.")
		return
	}
	if !(httpResp.StatusCode == 200) {
		apiErr, err := client.NewAPIError(httpResp)
		if err != nil {
			resp.Diagnostics.AddError("Error reading mycloud_get_commit", fmt.Sprintf("Could not read error response: %s", err))
			return
		}
		resp.Diagnostics.AddError("Error reading mycloud_get_commit", apiErr.Error())
		return
	}
	var data map[string]any
	decoder := json.NewDecoder(httpResp.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&data); err != nil && err != io.EOF {
		resp.Diagnostics.AddError("Error reading mycloud_get_commit", fmt.Sprintf("Could not decode response body: %s", err))
		return
	}
	err = applyJSONToModel(&config, data)
	if err != nil {
		resp.Diagnostics.AddError("Error reading mycloud_get_commit", fmt.Sprintf("Could not map response to state: %s", err))
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
// Configure stores the API client supplied by the provider.
func (d *GetCommitDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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
