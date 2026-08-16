package provider

import (
	"bytes"
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
	_ datasource.DataSource              = (*ListBranchesDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*ListBranchesDataSource)(nil)
)

// ListBranchesDataSource is the generated Terraform data source implementation.
type ListBranchesDataSource struct {
	client *client.Client
}

// ListBranchesDataSourceModel describes the data source state shape.
type ListBranchesDataSourceModel struct {
	Items        types.List   `tfsdk:"items"`
	Organization types.String `tfsdk:"organization"`
	Project      types.String `tfsdk:"project"`
}

// NewListBranchesDataSource returns a new instance of the generated data source.
func NewListBranchesDataSource() datasource.DataSource {
	return &ListBranchesDataSource{}
}

// Metadata returns the data source type name.
func (d *ListBranchesDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "mycloud_list_branches"
}

// Schema returns the data source schema.
func (d *ListBranchesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "List branches", Attributes: map[string]schema.Attribute{"items": schema.ListNestedAttribute{Computed: true, NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{"branch": schema.StringAttribute{Computed: true}, "id": schema.Int64Attribute{Computed: true}, "name": schema.StringAttribute{Computed: true}, "organization": schema.StringAttribute{Computed: true}, "project": schema.StringAttribute{Computed: true}, "protected": schema.BoolAttribute{Computed: true}, "sha": schema.StringAttribute{Computed: true}}}}, "organization": schema.StringAttribute{Required: true}, "project": schema.StringAttribute{Required: true}}}
}

// Read fetches remote state into the data source model.
func (d *ListBranchesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config ListBranchesDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d.readListRemote(ctx, &config, resp)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

// readListRemote performs the paginated read HTTP exchange and decodes the response array into config. Extracted from Read so the request/response logic is unit-testable without a tfsdk.Config.
func (d *ListBranchesDataSource) readListRemote(ctx context.Context, config *ListBranchesDataSourceModel, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The API client was not set on the resource. The provider Configure method must run before resource operations; this is a bug in the generated provider.")
		return
	}
	reqPath := "/organizations/{organization}/projects/{project}/branches"
	reqPath = strings.ReplaceAll(reqPath, "{organization}", url.PathEscape(config.Organization.ValueString()))
	reqPath = strings.ReplaceAll(reqPath, "{project}", url.PathEscape(config.Project.ValueString()))
	params := url.Values{}
	var nextURL string
	fetch := func(ctx context.Context, p url.Values) (*http.Response, error) {
		httpReq, err := d.client.NewRequest(ctx, http.MethodGet, reqPath, nil)
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
		return d.client.Do(httpReq)
	}
	pages, err := client.ListAllPages(ctx, params, fetch, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error reading mycloud_list_branches", fmt.Sprintf("Could not read list response: %s", err))
		return
	}
	items := []any{}
	for _, page := range pages {
		pageItems := []any{}
		dec := json.NewDecoder(bytes.NewReader(page))
		dec.UseNumber()
		if err := dec.Decode(&pageItems); err != nil {
			resp.Diagnostics.AddError("Error reading mycloud_list_branches", fmt.Sprintf("Could not decode list page: %s", err))
			return
		}
		items = append(items, pageItems...)
	}
	if err := applyJSONToModel(&config, map[string]any{"items": items}); err != nil {
		resp.Diagnostics.AddError("Error reading mycloud_list_branches", fmt.Sprintf("Could not map response to state: %s", err))
		return
	}
}

// Configure stores the API client supplied by the provider.
func (d *ListBranchesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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
