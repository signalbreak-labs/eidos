package provider

import (
	"context"
	"testing"
)
import "github.com/hashicorp/terraform-plugin-framework/resource"

// TestProjectResource_Create_Happy exercises ProjectResource.createRemote against an httptest mock: happy path returns the success status and an identifier in the body.
func TestProjectResource_Create_Happy(t *testing.T) {
	r := &ProjectResource{client: newMockClientStatus(t, 201, "{\"id\":1}")}
	m := ProjectResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestProjectResource_Create_NilClient exercises ProjectResource.createRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestProjectResource_Create_NilClient(t *testing.T) {
	r := &ProjectResource{}
	m := ProjectResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestProjectResource_Create_BuildError exercises ProjectResource.createRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestProjectResource_Create_BuildError(t *testing.T) {
	r := &ProjectResource{client: newMalformedBaseURLClient(t)}
	m := ProjectResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestProjectResource_Create_SendError exercises ProjectResource.createRemote against an httptest mock: transport error surfaces Could not send request.
func TestProjectResource_Create_SendError(t *testing.T) {
	r := &ProjectResource{client: newTransportErrorClient(t)}
	m := ProjectResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestProjectResource_Create_APIError exercises ProjectResource.createRemote against an httptest mock: non-success status surfaces the API error summary.
func TestProjectResource_Create_APIError(t *testing.T) {
	r := &ProjectResource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := ProjectResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error creating mycloud_project")
}

// TestProjectResource_Create_APIErrorReadBody exercises ProjectResource.createRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestProjectResource_Create_APIErrorReadBody(t *testing.T) {
	r := &ProjectResource{client: newMockClientReadErrorBody(t, 500)}
	m := ProjectResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}

// TestProjectResource_Create_InvalidJSON exercises ProjectResource.createRemote against an httptest mock: success status with a malformed body surfaces Could not decode response body.
func TestProjectResource_Create_InvalidJSON(t *testing.T) {
	r := &ProjectResource{client: newMockClientStatus(t, 201, "{{")}
	m := ProjectResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode response body")
}

// TestProjectResource_Create_MapError exercises ProjectResource.createRemote against an httptest mock: success status with a wrong-typed identifier surfaces Could not map response to state.
func TestProjectResource_Create_MapError(t *testing.T) {
	r := &ProjectResource{client: newMockClientStatus(t, 201, "{\"id\":\"not-valid\"}")}
	m := ProjectResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not map response to state")
}

// TestProjectResource_Create_MissingID exercises ProjectResource.createRemote against an httptest mock: success status with no identifier surfaces the missing-identifier diagnostic.
func TestProjectResource_Create_MissingID(t *testing.T) {
	r := &ProjectResource{client: newMockClientStatus(t, 201, "{}")}
	m := ProjectResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "did not contain an identifier")
}

// TestProjectResource_Read_Happy exercises ProjectResource.readRemote against an httptest mock: happy path returns the success status and reports removed=false with no errors.
func TestProjectResource_Read_Happy(t *testing.T) {
	r := &ProjectResource{client: newMockClientStatus(t, 200, "{}")}
	m := ProjectResourceModel{}
	resp := &resource.ReadResponse{}
	removed := r.readRemote(context.Background(), &m, resp)
	if removed {
		t.Fatalf("expected removed=false on happy path")
	}
	requireNoErrors(t, resp.Diagnostics)
}

// TestProjectResource_Read_NilClient exercises ProjectResource.readRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestProjectResource_Read_NilClient(t *testing.T) {
	r := &ProjectResource{}
	m := ProjectResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestProjectResource_Read_BuildError exercises ProjectResource.readRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestProjectResource_Read_BuildError(t *testing.T) {
	r := &ProjectResource{client: newMalformedBaseURLClient(t)}
	m := ProjectResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestProjectResource_Read_SendError exercises ProjectResource.readRemote against an httptest mock: transport error surfaces Could not send request.
func TestProjectResource_Read_SendError(t *testing.T) {
	r := &ProjectResource{client: newTransportErrorClient(t)}
	m := ProjectResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestProjectResource_Read_NotFound exercises ProjectResource.readRemote against an httptest mock: 404 reports removed=true with no error so the framework drops the resource from state.
func TestProjectResource_Read_NotFound(t *testing.T) {
	r := &ProjectResource{client: newMockClientStatus(t, 404, "")}
	m := ProjectResourceModel{}
	resp := &resource.ReadResponse{}
	removed := r.readRemote(context.Background(), &m, resp)
	if !removed {
		t.Fatalf("expected removed=true on 404")
	}
	requireNoErrors(t, resp.Diagnostics)
}

// TestProjectResource_Read_APIError exercises ProjectResource.readRemote against an httptest mock: non-success status surfaces the API error summary.
func TestProjectResource_Read_APIError(t *testing.T) {
	r := &ProjectResource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := ProjectResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error reading mycloud_project")
}

// TestProjectResource_Read_APIErrorReadBody exercises ProjectResource.readRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestProjectResource_Read_APIErrorReadBody(t *testing.T) {
	r := &ProjectResource{client: newMockClientReadErrorBody(t, 500)}
	m := ProjectResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}

// TestProjectResource_Read_InvalidJSON exercises ProjectResource.readRemote against an httptest mock: success status with a malformed body surfaces Could not decode response body.
func TestProjectResource_Read_InvalidJSON(t *testing.T) {
	r := &ProjectResource{client: newMockClientStatus(t, 200, "{{")}
	m := ProjectResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode response body")
}

// TestProjectResource_Read_MapError exercises ProjectResource.readRemote against an httptest mock: success status with a wrong-typed identifier surfaces Could not map response to state.
func TestProjectResource_Read_MapError(t *testing.T) {
	r := &ProjectResource{client: newMockClientStatus(t, 200, "{\"id\":\"not-valid\"}")}
	m := ProjectResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not map response to state")
}

// TestProjectResource_Update_Happy exercises ProjectResource.updateRemote against an httptest mock: happy path returns the success status with no errors.
func TestProjectResource_Update_Happy(t *testing.T) {
	r := &ProjectResource{client: newMockClientStatus(t, 200, "{}")}
	m := ProjectResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestProjectResource_Update_NilClient exercises ProjectResource.updateRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestProjectResource_Update_NilClient(t *testing.T) {
	r := &ProjectResource{}
	m := ProjectResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestProjectResource_Update_BuildError exercises ProjectResource.updateRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestProjectResource_Update_BuildError(t *testing.T) {
	r := &ProjectResource{client: newMalformedBaseURLClient(t)}
	m := ProjectResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestProjectResource_Update_SendError exercises ProjectResource.updateRemote against an httptest mock: transport error surfaces Could not send request.
func TestProjectResource_Update_SendError(t *testing.T) {
	r := &ProjectResource{client: newTransportErrorClient(t)}
	m := ProjectResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestProjectResource_Update_APIError exercises ProjectResource.updateRemote against an httptest mock: non-success status surfaces the API error summary.
func TestProjectResource_Update_APIError(t *testing.T) {
	r := &ProjectResource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := ProjectResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error updating mycloud_project")
}

// TestProjectResource_Update_APIErrorReadBody exercises ProjectResource.updateRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestProjectResource_Update_APIErrorReadBody(t *testing.T) {
	r := &ProjectResource{client: newMockClientReadErrorBody(t, 500)}
	m := ProjectResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}

// TestProjectResource_Update_InvalidJSON exercises ProjectResource.updateRemote against an httptest mock: success status with a malformed body surfaces Could not decode response body.
func TestProjectResource_Update_InvalidJSON(t *testing.T) {
	r := &ProjectResource{client: newMockClientStatus(t, 200, "{{")}
	m := ProjectResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode response body")
}

// TestProjectResource_Update_MapError exercises ProjectResource.updateRemote against an httptest mock: success status with a wrong-typed identifier surfaces Could not map response to state.
func TestProjectResource_Update_MapError(t *testing.T) {
	r := &ProjectResource{client: newMockClientStatus(t, 200, "{\"id\":\"not-valid\"}")}
	m := ProjectResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not map response to state")
}

// TestProjectResource_Delete_Happy exercises ProjectResource.deleteRemote against an httptest mock: happy path returns the success status with no errors.
func TestProjectResource_Delete_Happy(t *testing.T) {
	r := &ProjectResource{client: newMockClientStatus(t, 204, "")}
	m := ProjectResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestProjectResource_Delete_NilClient exercises ProjectResource.deleteRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestProjectResource_Delete_NilClient(t *testing.T) {
	r := &ProjectResource{}
	m := ProjectResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestProjectResource_Delete_BuildError exercises ProjectResource.deleteRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestProjectResource_Delete_BuildError(t *testing.T) {
	r := &ProjectResource{client: newMalformedBaseURLClient(t)}
	m := ProjectResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestProjectResource_Delete_SendError exercises ProjectResource.deleteRemote against an httptest mock: transport error surfaces Could not send request.
func TestProjectResource_Delete_SendError(t *testing.T) {
	r := &ProjectResource{client: newTransportErrorClient(t)}
	m := ProjectResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestProjectResource_Delete_NotFoundSuccess exercises ProjectResource.deleteRemote against an httptest mock: 404 is treated as already deleted and surfaces no error.
func TestProjectResource_Delete_NotFoundSuccess(t *testing.T) {
	r := &ProjectResource{client: newMockClientStatus(t, 404, "")}
	m := ProjectResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestProjectResource_Delete_APIError exercises ProjectResource.deleteRemote against an httptest mock: non-success status surfaces the API error summary.
func TestProjectResource_Delete_APIError(t *testing.T) {
	r := &ProjectResource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := ProjectResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error deleting mycloud_project")
}

// TestProjectResource_Delete_APIErrorReadBody exercises ProjectResource.deleteRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestProjectResource_Delete_APIErrorReadBody(t *testing.T) {
	r := &ProjectResource{client: newMockClientReadErrorBody(t, 500)}
	m := ProjectResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}
