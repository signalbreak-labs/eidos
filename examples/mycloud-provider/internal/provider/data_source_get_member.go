package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	_ datasource.DataSource              = (*GetMemberDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*GetMemberDataSource)(nil)
)

// GetMemberDataSource is the generated Terraform data source implementation.
type GetMemberDataSource struct {
	client *client.Client
}

// GetMemberDataSourceModel describes the data source state shape.
type GetMemberDataSourceModel struct {
	AvatarUrl types.String `tfsdk:"avatar_url"`
	Handle    types.String `tfsdk:"handle"`
	HtmlUrl   types.String `tfsdk:"html_url"`
	Id        types.Int64  `tfsdk:"id"`
	Member    types.String `tfsdk:"member"`
	Name      types.String `tfsdk:"name"`
}

// NewGetMemberDataSource returns a new instance of the generated data source.
func NewGetMemberDataSource() datasource.DataSource {
	return &GetMemberDataSource{}
}

// Metadata returns the data source type name.
func (d *GetMemberDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "mycloud_get_member"
}

// Schema returns the data source schema.
func (d *GetMemberDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Get a member", Attributes: map[string]schema.Attribute{"avatar_url": schema.StringAttribute{Computed: true}, "handle": schema.StringAttribute{Computed: true}, "html_url": schema.StringAttribute{Computed: true}, "id": schema.Int64Attribute{Computed: true}, "member": schema.StringAttribute{Required: true}, "name": schema.StringAttribute{Computed: true}}}
}

// Read fetches remote state into the data source model.
func (d *GetMemberDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config GetMemberDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d.readRemote(ctx, &config, resp)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

// readRemote performs the read HTTP exchange and decodes the response into config. Extracted from Read so the request/response logic is unit-testable without a tfsdk.Config.
func (d *GetMemberDataSource) readRemote(ctx context.Context, config *GetMemberDataSourceModel, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The API client was not set on the resource. The provider Configure method must run before resource operations; this is a bug in the generated provider.")
		return
	}
	reqPath := "/members/{member}"
	reqPath = strings.ReplaceAll(reqPath, "{member}", url.PathEscape(config.Member.ValueString()))
	httpReq, err := d.client.NewRequest(ctx, http.MethodGet, reqPath, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error reading mycloud_get_member", fmt.Sprintf("Could not build request: %s", err))
		return
	}
	httpResp, err := d.client.Do(httpReq)
	if err != nil {
		resp.Diagnostics.AddError("Error reading mycloud_get_member", fmt.Sprintf("Could not send request: %s", err))
		return
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode == http.StatusNotFound {
		resp.Diagnostics.AddError("Error reading mycloud_get_member", "The requested resource was not found.")
		return
	}
	if !(httpResp.StatusCode == 200) {
		apiErr, err := client.NewAPIError(httpResp)
		if err != nil {
			resp.Diagnostics.AddError("Error reading mycloud_get_member", fmt.Sprintf("Could not read error response: %s", err))
			return
		}
		resp.Diagnostics.AddError("Error reading mycloud_get_member", apiErr.Error())
		return
	}
	var data map[string]any
	decoder := json.NewDecoder(httpResp.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&data); err != nil && err != io.EOF {
		resp.Diagnostics.AddError("Error reading mycloud_get_member", fmt.Sprintf("Could not decode response body: %s", err))
		return
	}
	err = applyJSONToModel(&config, data)
	if err != nil {
		resp.Diagnostics.AddError("Error reading mycloud_get_member", fmt.Sprintf("Could not map response to state: %s", err))
		return
	}
}

// Configure stores the API client supplied by the provider.
func (d *GetMemberDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Data Source Configure Type", fmt.Sprintf("Expected *client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData))
		return
	}
	d.client = c
}
