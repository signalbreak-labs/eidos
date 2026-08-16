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
	action "github.com/hashicorp/terraform-plugin-framework/action"
	schema "github.com/hashicorp/terraform-plugin-framework/action/schema"
	types "github.com/hashicorp/terraform-plugin-framework/types"
	client "github.com/mycloud/terraform-provider-mycloud/internal/client"
)

// Compile-time interface assertion for action.Action.
var _ action.Action = (*CreateTaskAction)(nil)

// Compile-time interface assertion for action.ActionWithConfigure.
var _ action.ActionWithConfigure = (*CreateTaskAction)(nil)

// CreateTaskAction is the generated Terraform action implementation.
type CreateTaskAction struct {
	client *client.Client
}

// CreateTaskActionModel describes the action configuration shape.
type CreateTaskActionModel struct {
	Body             types.String `tfsdk:"body"`
	BodyOrganization types.String `tfsdk:"body_organization" json:"organization"`
	BodyProject      types.String `tfsdk:"body_project" json:"project"`
	HtmlUrl          types.String `tfsdk:"html_url"`
	Id               types.Int64  `tfsdk:"id"`
	Number           types.Int64  `tfsdk:"number"`
	Organization     types.String `tfsdk:"organization"`
	Project          types.String `tfsdk:"project"`
	State            types.String `tfsdk:"state"`
	TaskNumber       types.Int64  `tfsdk:"task_number"`
	Title            types.String `tfsdk:"title"`
}

// NewCreateTaskAction returns a new instance of the generated action.
func NewCreateTaskAction() action.Action {
	return &CreateTaskAction{}
}

// Metadata returns the action type name.
func (r *CreateTaskAction) Metadata(_ context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
	resp.TypeName = "mycloud_create_task"
}

// Schema returns the action schema.
func (r *CreateTaskAction) Schema(_ context.Context, _ action.SchemaRequest, resp *action.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Create an task", Attributes: map[string]schema.Attribute{"body": schema.StringAttribute{Optional: true}, "body_organization": schema.StringAttribute{Optional: true}, "body_project": schema.StringAttribute{Optional: true}, "html_url": schema.StringAttribute{Optional: true}, "id": schema.Int64Attribute{Optional: true}, "number": schema.Int64Attribute{Optional: true}, "organization": schema.StringAttribute{Required: true}, "project": schema.StringAttribute{Required: true}, "state": schema.StringAttribute{Optional: true}, "task_number": schema.Int64Attribute{Optional: true}, "title": schema.StringAttribute{Optional: true}}}
}

// Invoke executes the action against the remote API.
func (r *CreateTaskAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
	var config CreateTaskActionModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.invokeRemote(ctx, &config, resp)
}

// invokeRemote performs the invoke HTTP exchange and surfaces any error via diagnostics. Extracted from Invoke so the request/response logic is unit-testable without a tfsdk.Config.
func (r *CreateTaskAction) invokeRemote(ctx context.Context, config *CreateTaskActionModel, resp *action.InvokeResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The API client was not set on the resource. The provider Configure method must run before resource operations; this is a bug in the generated provider.")
		return
	}
	reqPath := "/organizations/{organization}/projects/{project}/tasks"
	reqPath = strings.ReplaceAll(reqPath, "{organization}", url.PathEscape(config.Organization.ValueString()))
	reqPath = strings.ReplaceAll(reqPath, "{project}", url.PathEscape(config.Project.ValueString()))
	body, err := modelToJSONMap(&config)
	if err != nil {
		resp.Diagnostics.AddError("Error invoking mycloud_create_task", fmt.Sprintf("Could not build request body: %s", err))
		return
	}
	payload, err := json.Marshal(body)
	if err != nil {
		resp.Diagnostics.AddError("Error invoking mycloud_create_task", fmt.Sprintf("Could not encode request body: %s", err))
		return
	}
	httpReq, err := r.client.NewRequest(ctx, http.MethodPost, reqPath, bytes.NewReader(payload))
	if err != nil {
		resp.Diagnostics.AddError("Error invoking mycloud_create_task", fmt.Sprintf("Could not build request: %s", err))
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpResp, err := r.client.Do(httpReq)
	if err != nil {
		resp.Diagnostics.AddError("Error invoking mycloud_create_task", fmt.Sprintf("Could not send request: %s", err))
		return
	}
	defer httpResp.Body.Close()
	if !(httpResp.StatusCode == 201) {
		apiErr, err := client.NewAPIError(httpResp)
		if err != nil {
			resp.Diagnostics.AddError("Error invoking mycloud_create_task", fmt.Sprintf("Could not read error response: %s", err))
			return
		}
		resp.Diagnostics.AddError("Error invoking mycloud_create_task", apiErr.Error())
		return
	}
}

// Configure stores the API client supplied by the provider.
func (r *CreateTaskAction) Configure(_ context.Context, req action.ConfigureRequest, resp *action.ConfigureResponse) {
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
