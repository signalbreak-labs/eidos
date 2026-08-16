package provider

import (
	"context"
	"testing"
)
import "github.com/hashicorp/terraform-plugin-framework/resource"

// TestWorkspaceResource_Create_Happy exercises WorkspaceResource.createRemote against an httptest mock: happy path returns the success status and an identifier in the body.
func TestWorkspaceResource_Create_Happy(t *testing.T) {
	r := &WorkspaceResource{client: newMockClientStatus(t, 201, "{\"name\":\"example-id\"}")}
	m := WorkspaceResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestWorkspaceResource_Create_NilClient exercises WorkspaceResource.createRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestWorkspaceResource_Create_NilClient(t *testing.T) {
	r := &WorkspaceResource{}
	m := WorkspaceResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestWorkspaceResource_Create_BuildError exercises WorkspaceResource.createRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestWorkspaceResource_Create_BuildError(t *testing.T) {
	r := &WorkspaceResource{client: newMalformedBaseURLClient(t)}
	m := WorkspaceResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestWorkspaceResource_Create_SendError exercises WorkspaceResource.createRemote against an httptest mock: transport error surfaces Could not send request.
func TestWorkspaceResource_Create_SendError(t *testing.T) {
	r := &WorkspaceResource{client: newTransportErrorClient(t)}
	m := WorkspaceResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestWorkspaceResource_Create_APIError exercises WorkspaceResource.createRemote against an httptest mock: non-success status surfaces the API error summary.
func TestWorkspaceResource_Create_APIError(t *testing.T) {
	r := &WorkspaceResource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := WorkspaceResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error creating mycloud_workspace")
}

// TestWorkspaceResource_Create_APIErrorReadBody exercises WorkspaceResource.createRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestWorkspaceResource_Create_APIErrorReadBody(t *testing.T) {
	r := &WorkspaceResource{client: newMockClientReadErrorBody(t, 500)}
	m := WorkspaceResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}

// TestWorkspaceResource_Create_InvalidJSON exercises WorkspaceResource.createRemote against an httptest mock: success status with a malformed body surfaces Could not decode response body.
func TestWorkspaceResource_Create_InvalidJSON(t *testing.T) {
	r := &WorkspaceResource{client: newMockClientStatus(t, 201, "{{")}
	m := WorkspaceResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode response body")
}

// TestWorkspaceResource_Create_MapError exercises WorkspaceResource.createRemote against an httptest mock: success status with a wrong-typed identifier surfaces Could not map response to state.
func TestWorkspaceResource_Create_MapError(t *testing.T) {
	r := &WorkspaceResource{client: newMockClientStatus(t, 201, "{\"name\":12345}")}
	m := WorkspaceResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not map response to state")
}

// TestWorkspaceResource_Create_MissingID exercises WorkspaceResource.createRemote against an httptest mock: success status with no identifier surfaces the missing-identifier diagnostic.
func TestWorkspaceResource_Create_MissingID(t *testing.T) {
	r := &WorkspaceResource{client: newMockClientStatus(t, 201, "{}")}
	m := WorkspaceResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "did not contain an identifier")
}

// TestWorkspaceResource_Create_LocationFallback exercises WorkspaceResource.createRemote against an httptest mock: success status with no body id but a Location header sets the string identifier from the header.
func TestWorkspaceResource_Create_LocationFallback(t *testing.T) {
	r := &WorkspaceResource{client: newMockClientWithLocation(t, 201, "http://example.test/folders/example-id", "{}")}
	m := WorkspaceResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
	if m.Name.ValueString() != "http://example.test/folders/example-id" {
		t.Fatalf("identifier = %q, want %q", m.Name.ValueString(), "http://example.test/folders/example-id")
	}
}

// TestWorkspaceResource_Read_Happy exercises WorkspaceResource.readRemote against an httptest mock: happy path returns the success status and reports removed=false with no errors.
func TestWorkspaceResource_Read_Happy(t *testing.T) {
	r := &WorkspaceResource{client: newMockClientStatus(t, 200, "{}")}
	m := WorkspaceResourceModel{}
	resp := &resource.ReadResponse{}
	removed := r.readRemote(context.Background(), &m, resp)
	if removed {
		t.Fatalf("expected removed=false on happy path")
	}
	requireNoErrors(t, resp.Diagnostics)
}

// TestWorkspaceResource_Read_NilClient exercises WorkspaceResource.readRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestWorkspaceResource_Read_NilClient(t *testing.T) {
	r := &WorkspaceResource{}
	m := WorkspaceResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestWorkspaceResource_Read_BuildError exercises WorkspaceResource.readRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestWorkspaceResource_Read_BuildError(t *testing.T) {
	r := &WorkspaceResource{client: newMalformedBaseURLClient(t)}
	m := WorkspaceResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestWorkspaceResource_Read_SendError exercises WorkspaceResource.readRemote against an httptest mock: transport error surfaces Could not send request.
func TestWorkspaceResource_Read_SendError(t *testing.T) {
	r := &WorkspaceResource{client: newTransportErrorClient(t)}
	m := WorkspaceResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestWorkspaceResource_Read_NotFound exercises WorkspaceResource.readRemote against an httptest mock: 404 reports removed=true with no error so the framework drops the resource from state.
func TestWorkspaceResource_Read_NotFound(t *testing.T) {
	r := &WorkspaceResource{client: newMockClientStatus(t, 404, "")}
	m := WorkspaceResourceModel{}
	resp := &resource.ReadResponse{}
	removed := r.readRemote(context.Background(), &m, resp)
	if !removed {
		t.Fatalf("expected removed=true on 404")
	}
	requireNoErrors(t, resp.Diagnostics)
}

// TestWorkspaceResource_Read_APIError exercises WorkspaceResource.readRemote against an httptest mock: non-success status surfaces the API error summary.
func TestWorkspaceResource_Read_APIError(t *testing.T) {
	r := &WorkspaceResource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := WorkspaceResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error reading mycloud_workspace")
}

// TestWorkspaceResource_Read_APIErrorReadBody exercises WorkspaceResource.readRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestWorkspaceResource_Read_APIErrorReadBody(t *testing.T) {
	r := &WorkspaceResource{client: newMockClientReadErrorBody(t, 500)}
	m := WorkspaceResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}

// TestWorkspaceResource_Read_InvalidJSON exercises WorkspaceResource.readRemote against an httptest mock: success status with a malformed body surfaces Could not decode response body.
func TestWorkspaceResource_Read_InvalidJSON(t *testing.T) {
	r := &WorkspaceResource{client: newMockClientStatus(t, 200, "{{")}
	m := WorkspaceResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode response body")
}

// TestWorkspaceResource_Read_MapError exercises WorkspaceResource.readRemote against an httptest mock: success status with a wrong-typed identifier surfaces Could not map response to state.
func TestWorkspaceResource_Read_MapError(t *testing.T) {
	r := &WorkspaceResource{client: newMockClientStatus(t, 200, "{\"name\":12345}")}
	m := WorkspaceResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not map response to state")
}

// TestWorkspaceResource_Update_Happy exercises WorkspaceResource.updateRemote against an httptest mock: happy path returns the success status with no errors.
func TestWorkspaceResource_Update_Happy(t *testing.T) {
	r := &WorkspaceResource{client: newMockClientStatus(t, 200, "{}")}
	m := WorkspaceResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestWorkspaceResource_Update_NilClient exercises WorkspaceResource.updateRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestWorkspaceResource_Update_NilClient(t *testing.T) {
	r := &WorkspaceResource{}
	m := WorkspaceResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestWorkspaceResource_Update_BuildError exercises WorkspaceResource.updateRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestWorkspaceResource_Update_BuildError(t *testing.T) {
	r := &WorkspaceResource{client: newMalformedBaseURLClient(t)}
	m := WorkspaceResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestWorkspaceResource_Update_SendError exercises WorkspaceResource.updateRemote against an httptest mock: transport error surfaces Could not send request.
func TestWorkspaceResource_Update_SendError(t *testing.T) {
	r := &WorkspaceResource{client: newTransportErrorClient(t)}
	m := WorkspaceResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestWorkspaceResource_Update_APIError exercises WorkspaceResource.updateRemote against an httptest mock: non-success status surfaces the API error summary.
func TestWorkspaceResource_Update_APIError(t *testing.T) {
	r := &WorkspaceResource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := WorkspaceResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error updating mycloud_workspace")
}

// TestWorkspaceResource_Update_APIErrorReadBody exercises WorkspaceResource.updateRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestWorkspaceResource_Update_APIErrorReadBody(t *testing.T) {
	r := &WorkspaceResource{client: newMockClientReadErrorBody(t, 500)}
	m := WorkspaceResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}

// TestWorkspaceResource_Update_InvalidJSON exercises WorkspaceResource.updateRemote against an httptest mock: success status with a malformed body surfaces Could not decode response body.
func TestWorkspaceResource_Update_InvalidJSON(t *testing.T) {
	r := &WorkspaceResource{client: newMockClientStatus(t, 200, "{{")}
	m := WorkspaceResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode response body")
}

// TestWorkspaceResource_Update_MapError exercises WorkspaceResource.updateRemote against an httptest mock: success status with a wrong-typed identifier surfaces Could not map response to state.
func TestWorkspaceResource_Update_MapError(t *testing.T) {
	r := &WorkspaceResource{client: newMockClientStatus(t, 200, "{\"name\":12345}")}
	m := WorkspaceResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not map response to state")
}

// TestWorkspaceResource_Delete_Happy exercises WorkspaceResource.deleteRemote against an httptest mock: happy path returns the success status with no errors.
func TestWorkspaceResource_Delete_Happy(t *testing.T) {
	r := &WorkspaceResource{client: newMockClientStatus(t, 200, "")}
	m := WorkspaceResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestWorkspaceResource_Delete_NilClient exercises WorkspaceResource.deleteRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestWorkspaceResource_Delete_NilClient(t *testing.T) {
	r := &WorkspaceResource{}
	m := WorkspaceResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestWorkspaceResource_Delete_BuildError exercises WorkspaceResource.deleteRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestWorkspaceResource_Delete_BuildError(t *testing.T) {
	r := &WorkspaceResource{client: newMalformedBaseURLClient(t)}
	m := WorkspaceResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestWorkspaceResource_Delete_SendError exercises WorkspaceResource.deleteRemote against an httptest mock: transport error surfaces Could not send request.
func TestWorkspaceResource_Delete_SendError(t *testing.T) {
	r := &WorkspaceResource{client: newTransportErrorClient(t)}
	m := WorkspaceResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestWorkspaceResource_Delete_NotFoundSuccess exercises WorkspaceResource.deleteRemote against an httptest mock: 404 is treated as already deleted and surfaces no error.
func TestWorkspaceResource_Delete_NotFoundSuccess(t *testing.T) {
	r := &WorkspaceResource{client: newMockClientStatus(t, 404, "")}
	m := WorkspaceResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestWorkspaceResource_Delete_APIError exercises WorkspaceResource.deleteRemote against an httptest mock: non-success status surfaces the API error summary.
func TestWorkspaceResource_Delete_APIError(t *testing.T) {
	r := &WorkspaceResource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := WorkspaceResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error deleting mycloud_workspace")
}

// TestWorkspaceResource_Delete_APIErrorReadBody exercises WorkspaceResource.deleteRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestWorkspaceResource_Delete_APIErrorReadBody(t *testing.T) {
	r := &WorkspaceResource{client: newMockClientReadErrorBody(t, 500)}
	m := WorkspaceResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}
