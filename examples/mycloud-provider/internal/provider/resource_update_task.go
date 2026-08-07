package provider

import "context"
import (
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
	schema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
)
// Compile-time interface assertions.
var _ resource.Resource = (*UpdateTaskResource)(nil)
// UpdateTaskResource is the generated Terraform managed resource implementation.
type UpdateTaskResource struct {
}
// UpdateTaskResourceModel describes the Terraform state and plan shape for UpdateTaskResource.
type UpdateTaskResourceModel struct {
}
// Metadata returns the resource type name.
func (r *UpdateTaskResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "mycloud_update_task"
}
// Schema returns the Terraform schema for this resource.
func (r *UpdateTaskResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{}
}
// Create provisions the remote resource and stores the resulting state.
func (r *UpdateTaskResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan UpdateTaskResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.AddError("Generated provider scaffold", "Create is not wired to a remote API endpoint.")
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}
// Read refreshes the Terraform state with the latest remote values.
func (r *UpdateTaskResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state UpdateTaskResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.AddError("Generated provider scaffold", "Read is not wired to a remote API endpoint.")
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
// Update modifies the remote resource to match the desired plan.
func (r *UpdateTaskResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan UpdateTaskResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	var state UpdateTaskResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.AddError("Generated provider scaffold", "Update is not wired to a remote API endpoint.")
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}
// Delete destroys the remote resource.
func (r *UpdateTaskResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state UpdateTaskResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.AddError("Generated provider scaffold", "Delete is not wired to a remote API endpoint.")
}
