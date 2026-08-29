package provider

import "context"
import (
	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/list"
	tfframeworkprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	types "github.com/hashicorp/terraform-plugin-framework/types"
	client "github.com/mycloud/terraform-provider-mycloud/internal/client"
)

var _ tfframeworkprovider.Provider = (*mycloudProvider)(nil)
var _ tfframeworkprovider.ProviderWithFunctions = (*mycloudProvider)(nil)
var _ tfframeworkprovider.ProviderWithEphemeralResources = (*mycloudProvider)(nil)
var _ tfframeworkprovider.ProviderWithListResources = (*mycloudProvider)(nil)
var _ tfframeworkprovider.ProviderWithActions = (*mycloudProvider)(nil)

// mycloudProvider is the generated Terraform provider implementation.
type mycloudProvider struct {
	configured bool
}

// mycloudProviderModel describes the provider-level configuration shape.
type mycloudProviderModel struct {
	Endpoint                  types.String `tfsdk:"endpoint"`
	BearerToken               types.String `tfsdk:"bearer_token"`
	LogFile                   types.String `tfsdk:"log_file"`
	LogCaptureRequestHeaders  types.Bool   `tfsdk:"log_capture_request_headers"`
	LogCaptureRequestBody     types.Bool   `tfsdk:"log_capture_request_body"`
	LogCaptureResponseHeaders types.Bool   `tfsdk:"log_capture_response_headers"`
	LogCaptureResponseBody    types.Bool   `tfsdk:"log_capture_response_body"`
	LogMaxBodyBytes           types.Int64  `tfsdk:"log_max_body_bytes"`
}

// New returns a new instance of the generated provider.
func New() tfframeworkprovider.Provider {
	return &mycloudProvider{}
}

// Metadata returns the provider type name.
func (p *mycloudProvider) Metadata(_ context.Context, _ tfframeworkprovider.MetadataRequest, resp *tfframeworkprovider.MetadataResponse) {
	resp.TypeName = "mycloud"
}

// Schema returns the provider configuration schema.
func (p *mycloudProvider) Schema(_ context.Context, _ tfframeworkprovider.SchemaRequest, resp *tfframeworkprovider.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Trimmed MyCloud API reference spec for golden file regression tests. The compute schemas keep path parameters (name, workspace) at the top level as a stable baseline; focused transformer and generated-lifecycle tests cover one-level nested identity promotion. The compute family (workspaces, instances, networks, stacks, configs, secrets) exercises workspace-scoped CRUD and list resources; the projects family (organizations, projects, tasks, pull requests, members, branches, commits) exercises organization-scoped data sources and list resources.", Attributes: map[string]schema.Attribute{"endpoint": schema.StringAttribute{MarkdownDescription: "Overrides the default API base URL derived from the OpenAPI servers. Useful for directing the provider at a test or mock server.", Optional: true}, "bearer_token": schema.StringAttribute{MarkdownDescription: "Bearer token used for HTTP bearer authentication.", Optional: true, Sensitive: true}, "log_file": schema.StringAttribute{MarkdownDescription: "Path to a file that receives HTTP request/response trace logs. When unset, trace logging is disabled.", Optional: true}, "log_capture_request_headers": schema.BoolAttribute{MarkdownDescription: "Capture request headers in the trace log. Sensitive headers are redacted.", Optional: true}, "log_capture_request_body": schema.BoolAttribute{MarkdownDescription: "Capture request bodies in the trace log. Disabled by default to avoid writing sensitive payloads to disk.", Optional: true}, "log_capture_response_headers": schema.BoolAttribute{MarkdownDescription: "Capture response headers in the trace log. Sensitive headers are redacted.", Optional: true}, "log_capture_response_body": schema.BoolAttribute{MarkdownDescription: "Capture response bodies in the trace log. Disabled by default to avoid writing sensitive payloads to disk.", Optional: true}, "log_max_body_bytes": schema.Int64Attribute{MarkdownDescription: "Maximum number of body bytes captured per log entry before truncation. Defaults to 4096.", Optional: true}}}
}

// Configure decodes practitioner configuration and marks the provider as configured.
func (p *mycloudProvider) Configure(ctx context.Context, req tfframeworkprovider.ConfigureRequest, resp *tfframeworkprovider.ConfigureResponse) {
	var config mycloudProviderModel
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	opts := []client.ClientOption{}
	if !config.Endpoint.IsNull() && !config.Endpoint.IsUnknown() {
		opts = append(opts, client.WithBaseURL(config.Endpoint.ValueString()))
	}
	if !config.BearerToken.IsNull() && !config.BearerToken.IsUnknown() {
		opts = append(opts, client.WithSchemeInterceptor("bearerAuth", client.BearerAuth(config.BearerToken.ValueString())))
	}
	loggingConfig := client.LoggingConfig{}
	if !config.LogFile.IsNull() && !config.LogFile.IsUnknown() {
		loggingConfig.LogFile = config.LogFile.ValueString()
	}
	if !config.LogCaptureRequestHeaders.IsNull() && !config.LogCaptureRequestHeaders.IsUnknown() {
		loggingConfig.CaptureRequestHeaders = config.LogCaptureRequestHeaders.ValueBool()
	}
	if !config.LogCaptureRequestBody.IsNull() && !config.LogCaptureRequestBody.IsUnknown() {
		loggingConfig.CaptureRequestBody = config.LogCaptureRequestBody.ValueBool()
	}
	if !config.LogCaptureResponseHeaders.IsNull() && !config.LogCaptureResponseHeaders.IsUnknown() {
		loggingConfig.CaptureResponseHeaders = config.LogCaptureResponseHeaders.ValueBool()
	}
	if !config.LogCaptureResponseBody.IsNull() && !config.LogCaptureResponseBody.IsUnknown() {
		loggingConfig.CaptureResponseBody = config.LogCaptureResponseBody.ValueBool()
	}
	if !config.LogMaxBodyBytes.IsNull() && !config.LogMaxBodyBytes.IsUnknown() {
		loggingConfig.MaxBodyBytes = int(config.LogMaxBodyBytes.ValueInt64())
	}
	if loggingConfig.LogFile != "" {
		opts = append(opts, client.WithLogging(loggingConfig))
	}
	c := client.New(opts...)
	resp.DataSourceData = c
	resp.ResourceData = c
	resp.EphemeralResourceData = c
	resp.ActionData = c
	resp.ListResourceData = c
	p.configured = true
}

// DataSources returns the data sources registered with this provider.
func (p *mycloudProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{func() datasource.DataSource {
		return &ListMembersDataSource{}
	}, func() datasource.DataSource {
		return &GetMemberDataSource{}
	}, func() datasource.DataSource {
		return &ListProjectsForOrganizationDataSource{}
	}, func() datasource.DataSource {
		return &ListBranchesDataSource{}
	}, func() datasource.DataSource {
		return &GetBranchDataSource{}
	}, func() datasource.DataSource {
		return &ListCommitsDataSource{}
	}, func() datasource.DataSource {
		return &GetCommitDataSource{}
	}, func() datasource.DataSource {
		return &ListPullRequestsDataSource{}
	}, func() datasource.DataSource {
		return &GetPullRequestDataSource{}
	}, func() datasource.DataSource {
		return &ListTasksDataSource{}
	}, func() datasource.DataSource {
		return &GetTaskDataSource{}
	}, func() datasource.DataSource {
		return &ListWorkspacesDataSource{}
	}, func() datasource.DataSource {
		return &ListConfigsDataSource{}
	}, func() datasource.DataSource {
		return &ListInstancesDataSource{}
	}, func() datasource.DataSource {
		return &ListNetworksDataSource{}
	}, func() datasource.DataSource {
		return &ListSecretsDataSource{}
	}, func() datasource.DataSource {
		return &ListStacksDataSource{}
	}}
}

// Resources returns the managed resources registered with this provider.
func (p *mycloudProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{func() resource.Resource {
		return &ConfigResource{}
	}, func() resource.Resource {
		return &InstanceResource{}
	}, func() resource.Resource {
		return &NetworkResource{}
	}, func() resource.Resource {
		return &ProjectResource{}
	}, func() resource.Resource {
		return &SecretResource{}
	}, func() resource.Resource {
		return &StackResource{}
	}, func() resource.Resource {
		return &WorkspaceResource{}
	}}
}

// Actions returns the actions registered with this provider.
func (p *mycloudProvider) Actions(_ context.Context) []func() action.Action {
	return []func() action.Action{func() action.Action {
		return &CreatePullRequestAction{}
	}, func() action.Action {
		return &UpdatePullRequestAction{}
	}, func() action.Action {
		return &CreateTaskAction{}
	}, func() action.Action {
		return &UpdateTaskAction{}
	}}
}

// Functions returns the provider-defined functions registered with this provider.
func (p *mycloudProvider) Functions(_ context.Context) []func() function.Function {
	return nil
}

// EphemeralResources returns the ephemeral resources registered with this provider.
func (p *mycloudProvider) EphemeralResources(_ context.Context) []func() ephemeral.EphemeralResource {
	return nil
}

// ListResources returns the list resources registered with this provider.
func (p *mycloudProvider) ListResources(_ context.Context) []func() list.ListResource {
	return nil
}
