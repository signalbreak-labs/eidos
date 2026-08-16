package provider

import (
	"context"
	"testing"
)
import "github.com/hashicorp/terraform-plugin-framework/resource"

// TestStackResource_Create_Happy exercises StackResource.createRemote against an httptest mock: happy path returns the success status and an identifier in the body.
func TestStackResource_Create_Happy(t *testing.T) {
	r := &StackResource{client: newMockClientStatus(t, 201, "{\"id\":\"example-id\"}")}
	m := StackResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestStackResource_Create_NilClient exercises StackResource.createRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestStackResource_Create_NilClient(t *testing.T) {
	r := &StackResource{}
	m := StackResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestStackResource_Create_BuildError exercises StackResource.createRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestStackResource_Create_BuildError(t *testing.T) {
	r := &StackResource{client: newMalformedBaseURLClient(t)}
	m := StackResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestStackResource_Create_SendError exercises StackResource.createRemote against an httptest mock: transport error surfaces Could not send request.
func TestStackResource_Create_SendError(t *testing.T) {
	r := &StackResource{client: newTransportErrorClient(t)}
	m := StackResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestStackResource_Create_APIError exercises StackResource.createRemote against an httptest mock: non-success status surfaces the API error summary.
func TestStackResource_Create_APIError(t *testing.T) {
	r := &StackResource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := StackResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error creating mycloud_stack")
}

// TestStackResource_Create_APIErrorReadBody exercises StackResource.createRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestStackResource_Create_APIErrorReadBody(t *testing.T) {
	r := &StackResource{client: newMockClientReadErrorBody(t, 500)}
	m := StackResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}

// TestStackResource_Create_InvalidJSON exercises StackResource.createRemote against an httptest mock: success status with a malformed body surfaces Could not decode response body.
func TestStackResource_Create_InvalidJSON(t *testing.T) {
	r := &StackResource{client: newMockClientStatus(t, 201, "{{")}
	m := StackResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode response body")
}

// TestStackResource_Create_MapError exercises StackResource.createRemote against an httptest mock: success status with a wrong-typed identifier surfaces Could not map response to state.
func TestStackResource_Create_MapError(t *testing.T) {
	r := &StackResource{client: newMockClientStatus(t, 201, "{\"id\":12345}")}
	m := StackResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not map response to state")
}

// TestStackResource_Create_MissingID exercises StackResource.createRemote against an httptest mock: success status with no identifier surfaces the missing-identifier diagnostic.
func TestStackResource_Create_MissingID(t *testing.T) {
	r := &StackResource{client: newMockClientStatus(t, 201, "{}")}
	m := StackResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "did not contain an identifier")
}

// TestStackResource_Create_LocationFallback exercises StackResource.createRemote against an httptest mock: success status with no body id but a Location header sets the string identifier from the header.
func TestStackResource_Create_LocationFallback(t *testing.T) {
	r := &StackResource{client: newMockClientWithLocation(t, 201, "http://example.test/folders/example-id", "{}")}
	m := StackResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
	if m.Id.ValueString() != "http://example.test/folders/example-id" {
		t.Fatalf("identifier = %q, want %q", m.Id.ValueString(), "http://example.test/folders/example-id")
	}
}

// TestStackResource_Read_Happy exercises StackResource.readRemote against an httptest mock: happy path returns the success status and reports removed=false with no errors.
func TestStackResource_Read_Happy(t *testing.T) {
	r := &StackResource{client: newMockClientStatus(t, 200, "{}")}
	m := StackResourceModel{}
	resp := &resource.ReadResponse{}
	removed := r.readRemote(context.Background(), &m, resp)
	if removed {
		t.Fatalf("expected removed=false on happy path")
	}
	requireNoErrors(t, resp.Diagnostics)
}

// TestStackResource_Read_NilClient exercises StackResource.readRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestStackResource_Read_NilClient(t *testing.T) {
	r := &StackResource{}
	m := StackResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestStackResource_Read_BuildError exercises StackResource.readRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestStackResource_Read_BuildError(t *testing.T) {
	r := &StackResource{client: newMalformedBaseURLClient(t)}
	m := StackResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestStackResource_Read_SendError exercises StackResource.readRemote against an httptest mock: transport error surfaces Could not send request.
func TestStackResource_Read_SendError(t *testing.T) {
	r := &StackResource{client: newTransportErrorClient(t)}
	m := StackResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestStackResource_Read_NotFound exercises StackResource.readRemote against an httptest mock: 404 reports removed=true with no error so the framework drops the resource from state.
func TestStackResource_Read_NotFound(t *testing.T) {
	r := &StackResource{client: newMockClientStatus(t, 404, "")}
	m := StackResourceModel{}
	resp := &resource.ReadResponse{}
	removed := r.readRemote(context.Background(), &m, resp)
	if !removed {
		t.Fatalf("expected removed=true on 404")
	}
	requireNoErrors(t, resp.Diagnostics)
}

// TestStackResource_Read_APIError exercises StackResource.readRemote against an httptest mock: non-success status surfaces the API error summary.
func TestStackResource_Read_APIError(t *testing.T) {
	r := &StackResource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := StackResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error reading mycloud_stack")
}

// TestStackResource_Read_APIErrorReadBody exercises StackResource.readRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestStackResource_Read_APIErrorReadBody(t *testing.T) {
	r := &StackResource{client: newMockClientReadErrorBody(t, 500)}
	m := StackResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}

// TestStackResource_Read_InvalidJSON exercises StackResource.readRemote against an httptest mock: success status with a malformed body surfaces Could not decode response body.
func TestStackResource_Read_InvalidJSON(t *testing.T) {
	r := &StackResource{client: newMockClientStatus(t, 200, "{{")}
	m := StackResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode response body")
}

// TestStackResource_Read_MapError exercises StackResource.readRemote against an httptest mock: success status with a wrong-typed identifier surfaces Could not map response to state.
func TestStackResource_Read_MapError(t *testing.T) {
	r := &StackResource{client: newMockClientStatus(t, 200, "{\"id\":12345}")}
	m := StackResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not map response to state")
}

// TestStackResource_Update_Happy exercises StackResource.updateRemote against an httptest mock: happy path returns the success status with no errors.
func TestStackResource_Update_Happy(t *testing.T) {
	r := &StackResource{client: newMockClientStatus(t, 200, "{}")}
	m := StackResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestStackResource_Update_NilClient exercises StackResource.updateRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestStackResource_Update_NilClient(t *testing.T) {
	r := &StackResource{}
	m := StackResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestStackResource_Update_BuildError exercises StackResource.updateRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestStackResource_Update_BuildError(t *testing.T) {
	r := &StackResource{client: newMalformedBaseURLClient(t)}
	m := StackResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestStackResource_Update_SendError exercises StackResource.updateRemote against an httptest mock: transport error surfaces Could not send request.
func TestStackResource_Update_SendError(t *testing.T) {
	r := &StackResource{client: newTransportErrorClient(t)}
	m := StackResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestStackResource_Update_APIError exercises StackResource.updateRemote against an httptest mock: non-success status surfaces the API error summary.
func TestStackResource_Update_APIError(t *testing.T) {
	r := &StackResource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := StackResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error updating mycloud_stack")
}

// TestStackResource_Update_APIErrorReadBody exercises StackResource.updateRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestStackResource_Update_APIErrorReadBody(t *testing.T) {
	r := &StackResource{client: newMockClientReadErrorBody(t, 500)}
	m := StackResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}

// TestStackResource_Update_InvalidJSON exercises StackResource.updateRemote against an httptest mock: success status with a malformed body surfaces Could not decode response body.
func TestStackResource_Update_InvalidJSON(t *testing.T) {
	r := &StackResource{client: newMockClientStatus(t, 200, "{{")}
	m := StackResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode response body")
}

// TestStackResource_Update_MapError exercises StackResource.updateRemote against an httptest mock: success status with a wrong-typed identifier surfaces Could not map response to state.
func TestStackResource_Update_MapError(t *testing.T) {
	r := &StackResource{client: newMockClientStatus(t, 200, "{\"id\":12345}")}
	m := StackResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not map response to state")
}

// TestStackResource_Delete_Happy exercises StackResource.deleteRemote against an httptest mock: happy path returns the success status with no errors.
func TestStackResource_Delete_Happy(t *testing.T) {
	r := &StackResource{client: newMockClientStatus(t, 200, "")}
	m := StackResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestStackResource_Delete_NilClient exercises StackResource.deleteRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestStackResource_Delete_NilClient(t *testing.T) {
	r := &StackResource{}
	m := StackResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestStackResource_Delete_BuildError exercises StackResource.deleteRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestStackResource_Delete_BuildError(t *testing.T) {
	r := &StackResource{client: newMalformedBaseURLClient(t)}
	m := StackResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestStackResource_Delete_SendError exercises StackResource.deleteRemote against an httptest mock: transport error surfaces Could not send request.
func TestStackResource_Delete_SendError(t *testing.T) {
	r := &StackResource{client: newTransportErrorClient(t)}
	m := StackResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestStackResource_Delete_NotFoundSuccess exercises StackResource.deleteRemote against an httptest mock: 404 is treated as already deleted and surfaces no error.
func TestStackResource_Delete_NotFoundSuccess(t *testing.T) {
	r := &StackResource{client: newMockClientStatus(t, 404, "")}
	m := StackResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestStackResource_Delete_APIError exercises StackResource.deleteRemote against an httptest mock: non-success status surfaces the API error summary.
func TestStackResource_Delete_APIError(t *testing.T) {
	r := &StackResource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := StackResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error deleting mycloud_stack")
}

// TestStackResource_Delete_APIErrorReadBody exercises StackResource.deleteRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestStackResource_Delete_APIErrorReadBody(t *testing.T) {
	r := &StackResource{client: newMockClientReadErrorBody(t, 500)}
	m := StackResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}
