package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)
import (
	action "github.com/hashicorp/terraform-plugin-framework/action"
	schema "github.com/hashicorp/terraform-plugin-framework/action/schema"
	types "github.com/hashicorp/terraform-plugin-framework/types"
	client "github.com/mycloud/terraform-provider-mycloud/internal/client"
)

// Compile-time interface assertion for action.Action.
var _ action.Action = (*UpdatePullRequestAction)(nil)

// Compile-time interface assertion for action.ActionWithConfigure.
var _ action.ActionWithConfigure = (*UpdatePullRequestAction)(nil)

// UpdatePullRequestAction is the generated Terraform action implementation.
type UpdatePullRequestAction struct {
	client *client.Client
}

// UpdatePullRequestActionModel describes the action configuration shape.
type UpdatePullRequestActionModel struct {
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

// NewUpdatePullRequestAction returns a new instance of the generated action.
func NewUpdatePullRequestAction() action.Action {
	return &UpdatePullRequestAction{}
}

// Metadata returns the action type name.
func (r *UpdatePullRequestAction) Metadata(_ context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
	resp.TypeName = "mycloud_update_pull_request"
}

// Schema returns the action schema.
func (r *UpdatePullRequestAction) Schema(_ context.Context, _ action.SchemaRequest, resp *action.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Update a pull request", Attributes: map[string]schema.Attribute{"body": schema.StringAttribute{Optional: true}, "html_url": schema.StringAttribute{Optional: true}, "id": schema.Int64Attribute{Optional: true}, "merged": schema.BoolAttribute{Optional: true}, "number": schema.Int64Attribute{Optional: true}, "organization": schema.StringAttribute{Required: true}, "project": schema.StringAttribute{Required: true}, "pull_number": schema.Int64Attribute{Required: true}, "state": schema.StringAttribute{Optional: true}, "title": schema.StringAttribute{Optional: true}}}
}

// Invoke executes the action against the remote API.
func (r *UpdatePullRequestAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
	var config UpdatePullRequestActionModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The API client was not set on the resource. The provider Configure method must run before resource operations; this is a bug in the generated provider.")
		return
	}
	reqPath := "/organizations/{organization}/projects/{project}/pull_requests/{pull_number}"
	reqPath = strings.ReplaceAll(reqPath, "{organization}", url.PathEscape(config.Organization.ValueString()))
	reqPath = strings.ReplaceAll(reqPath, "{project}", url.PathEscape(config.Project.ValueString()))
	reqPath = strings.ReplaceAll(reqPath, "{pull_number}", url.PathEscape(strconv.FormatInt(config.PullNumber.ValueInt64(), 10)))
	body, err := modelToJSONMap(&config)
	if err != nil {
		resp.Diagnostics.AddError("Error invoking mycloud_update_pull_request", fmt.Sprintf("Could not build request body: %s", err))
		return
	}
	payload, err := json.Marshal(body)
	if err != nil {
		resp.Diagnostics.AddError("Error invoking mycloud_update_pull_request", fmt.Sprintf("Could not encode request body: %s", err))
		return
	}
	httpReq, err := r.client.NewRequest(ctx, http.MethodPatch, reqPath, bytes.NewReader(payload))
	if err != nil {
		resp.Diagnostics.AddError("Error invoking mycloud_update_pull_request", fmt.Sprintf("Could not build request: %s", err))
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpResp, err := r.client.Do(httpReq)
	if err != nil {
		resp.Diagnostics.AddError("Error invoking mycloud_update_pull_request", fmt.Sprintf("Could not send request: %s", err))
		return
	}
	defer httpResp.Body.Close()
	if !(httpResp.StatusCode == 200) {
		apiErr, err := client.NewAPIError(httpResp)
		if err != nil {
			resp.Diagnostics.AddError("Error invoking mycloud_update_pull_request", fmt.Sprintf("Could not read error response: %s", err))
			return
		}
		resp.Diagnostics.AddError("Error invoking mycloud_update_pull_request", apiErr.Error())
		return
	}
}

// Configure stores the API client supplied by the provider.
func (r *UpdatePullRequestAction) Configure(_ context.Context, req action.ConfigureRequest, resp *action.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Action Configure Type", fmt.Sprintf("Expected *client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData))
		return
	}
	r.client = c
}
