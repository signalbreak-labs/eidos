package provider

import "context"
import (
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
	schema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
)
// Compile-time interface assertions.
var _ resource.Resource = (*CreateTaskResource)(nil)
// CreateTaskResource is the generated Terraform managed resource implementation.
type CreateTaskResource struct {
}
// CreateTaskResourceModel describes the Terraform state and plan shape for CreateTaskResource.
type CreateTaskResourceModel struct {
}
// Metadata returns the resource type name.
func (r *CreateTaskResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "mycloud_create_task"
}
// Schema returns the Terraform schema for this resource.
func (r *CreateTaskResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{}
}
// Create provisions the remote resource and stores the resulting state.
func (r *CreateTaskResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan CreateTaskResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.AddError("Generated provider scaffold", "Create is not wired to a remote API endpoint.")
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}
// Read refreshes the Terraform state with the latest remote values.
func (r *CreateTaskResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state CreateTaskResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.AddError("Generated provider scaffold", "Read is not wired to a remote API endpoint.")
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
// Update modifies the remote resource to match the desired plan.
func (r *CreateTaskResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan CreateTaskResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	var state CreateTaskResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.AddError("Generated provider scaffold", "Update is not wired to a remote API endpoint.")
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}
// Delete destroys the remote resource.
func (r *CreateTaskResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state CreateTaskResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.AddError("Generated provider scaffold", "Delete is not wired to a remote API endpoint.")
}
