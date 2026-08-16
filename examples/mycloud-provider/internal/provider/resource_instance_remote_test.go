package provider

import (
	"context"
	"testing"
)
import "github.com/hashicorp/terraform-plugin-framework/resource"

// TestInstanceResource_Create_Happy exercises InstanceResource.createRemote against an httptest mock: happy path returns the success status and an identifier in the body.
func TestInstanceResource_Create_Happy(t *testing.T) {
	r := &InstanceResource{client: newMockClientStatus(t, 201, "{\"id\":\"example-id\"}")}
	m := InstanceResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestInstanceResource_Create_NilClient exercises InstanceResource.createRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestInstanceResource_Create_NilClient(t *testing.T) {
	r := &InstanceResource{}
	m := InstanceResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestInstanceResource_Create_BuildError exercises InstanceResource.createRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestInstanceResource_Create_BuildError(t *testing.T) {
	r := &InstanceResource{client: newMalformedBaseURLClient(t)}
	m := InstanceResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestInstanceResource_Create_SendError exercises InstanceResource.createRemote against an httptest mock: transport error surfaces Could not send request.
func TestInstanceResource_Create_SendError(t *testing.T) {
	r := &InstanceResource{client: newTransportErrorClient(t)}
	m := InstanceResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestInstanceResource_Create_APIError exercises InstanceResource.createRemote against an httptest mock: non-success status surfaces the API error summary.
func TestInstanceResource_Create_APIError(t *testing.T) {
	r := &InstanceResource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := InstanceResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error creating mycloud_instance")
}

// TestInstanceResource_Create_APIErrorReadBody exercises InstanceResource.createRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestInstanceResource_Create_APIErrorReadBody(t *testing.T) {
	r := &InstanceResource{client: newMockClientReadErrorBody(t, 500)}
	m := InstanceResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}

// TestInstanceResource_Create_InvalidJSON exercises InstanceResource.createRemote against an httptest mock: success status with a malformed body surfaces Could not decode response body.
func TestInstanceResource_Create_InvalidJSON(t *testing.T) {
	r := &InstanceResource{client: newMockClientStatus(t, 201, "{{")}
	m := InstanceResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode response body")
}

// TestInstanceResource_Create_MapError exercises InstanceResource.createRemote against an httptest mock: success status with a wrong-typed identifier surfaces Could not map response to state.
func TestInstanceResource_Create_MapError(t *testing.T) {
	r := &InstanceResource{client: newMockClientStatus(t, 201, "{\"id\":12345}")}
	m := InstanceResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not map response to state")
}

// TestInstanceResource_Create_MissingID exercises InstanceResource.createRemote against an httptest mock: success status with no identifier surfaces the missing-identifier diagnostic.
func TestInstanceResource_Create_MissingID(t *testing.T) {
	r := &InstanceResource{client: newMockClientStatus(t, 201, "{}")}
	m := InstanceResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "did not contain an identifier")
}

// TestInstanceResource_Create_LocationFallback exercises InstanceResource.createRemote against an httptest mock: success status with no body id but a Location header sets the string identifier from the header.
func TestInstanceResource_Create_LocationFallback(t *testing.T) {
	r := &InstanceResource{client: newMockClientWithLocation(t, 201, "http://example.test/folders/example-id", "{}")}
	m := InstanceResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
	if m.Id.ValueString() != "http://example.test/folders/example-id" {
		t.Fatalf("identifier = %q, want %q", m.Id.ValueString(), "http://example.test/folders/example-id")
	}
}

// TestInstanceResource_Read_Happy exercises InstanceResource.readRemote against an httptest mock: happy path returns the success status and reports removed=false with no errors.
func TestInstanceResource_Read_Happy(t *testing.T) {
	r := &InstanceResource{client: newMockClientStatus(t, 200, "{}")}
	m := InstanceResourceModel{}
	resp := &resource.ReadResponse{}
	removed := r.readRemote(context.Background(), &m, resp)
	if removed {
		t.Fatalf("expected removed=false on happy path")
	}
	requireNoErrors(t, resp.Diagnostics)
}

// TestInstanceResource_Read_NilClient exercises InstanceResource.readRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestInstanceResource_Read_NilClient(t *testing.T) {
	r := &InstanceResource{}
	m := InstanceResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestInstanceResource_Read_BuildError exercises InstanceResource.readRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestInstanceResource_Read_BuildError(t *testing.T) {
	r := &InstanceResource{client: newMalformedBaseURLClient(t)}
	m := InstanceResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestInstanceResource_Read_SendError exercises InstanceResource.readRemote against an httptest mock: transport error surfaces Could not send request.
func TestInstanceResource_Read_SendError(t *testing.T) {
	r := &InstanceResource{client: newTransportErrorClient(t)}
	m := InstanceResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestInstanceResource_Read_NotFound exercises InstanceResource.readRemote against an httptest mock: 404 reports removed=true with no error so the framework drops the resource from state.
func TestInstanceResource_Read_NotFound(t *testing.T) {
	r := &InstanceResource{client: newMockClientStatus(t, 404, "")}
	m := InstanceResourceModel{}
	resp := &resource.ReadResponse{}
	removed := r.readRemote(context.Background(), &m, resp)
	if !removed {
		t.Fatalf("expected removed=true on 404")
	}
	requireNoErrors(t, resp.Diagnostics)
}

// TestInstanceResource_Read_APIError exercises InstanceResource.readRemote against an httptest mock: non-success status surfaces the API error summary.
func TestInstanceResource_Read_APIError(t *testing.T) {
	r := &InstanceResource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := InstanceResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error reading mycloud_instance")
}

// TestInstanceResource_Read_APIErrorReadBody exercises InstanceResource.readRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestInstanceResource_Read_APIErrorReadBody(t *testing.T) {
	r := &InstanceResource{client: newMockClientReadErrorBody(t, 500)}
	m := InstanceResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}

// TestInstanceResource_Read_InvalidJSON exercises InstanceResource.readRemote against an httptest mock: success status with a malformed body surfaces Could not decode response body.
func TestInstanceResource_Read_InvalidJSON(t *testing.T) {
	r := &InstanceResource{client: newMockClientStatus(t, 200, "{{")}
	m := InstanceResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode response body")
}

// TestInstanceResource_Read_MapError exercises InstanceResource.readRemote against an httptest mock: success status with a wrong-typed identifier surfaces Could not map response to state.
func TestInstanceResource_Read_MapError(t *testing.T) {
	r := &InstanceResource{client: newMockClientStatus(t, 200, "{\"id\":12345}")}
	m := InstanceResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not map response to state")
}

// TestInstanceResource_Update_Happy exercises InstanceResource.updateRemote against an httptest mock: happy path returns the success status with no errors.
func TestInstanceResource_Update_Happy(t *testing.T) {
	r := &InstanceResource{client: newMockClientStatus(t, 200, "{}")}
	m := InstanceResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestInstanceResource_Update_NilClient exercises InstanceResource.updateRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestInstanceResource_Update_NilClient(t *testing.T) {
	r := &InstanceResource{}
	m := InstanceResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestInstanceResource_Update_BuildError exercises InstanceResource.updateRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestInstanceResource_Update_BuildError(t *testing.T) {
	r := &InstanceResource{client: newMalformedBaseURLClient(t)}
	m := InstanceResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestInstanceResource_Update_SendError exercises InstanceResource.updateRemote against an httptest mock: transport error surfaces Could not send request.
func TestInstanceResource_Update_SendError(t *testing.T) {
	r := &InstanceResource{client: newTransportErrorClient(t)}
	m := InstanceResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestInstanceResource_Update_APIError exercises InstanceResource.updateRemote against an httptest mock: non-success status surfaces the API error summary.
func TestInstanceResource_Update_APIError(t *testing.T) {
	r := &InstanceResource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := InstanceResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error updating mycloud_instance")
}

// TestInstanceResource_Update_APIErrorReadBody exercises InstanceResource.updateRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestInstanceResource_Update_APIErrorReadBody(t *testing.T) {
	r := &InstanceResource{client: newMockClientReadErrorBody(t, 500)}
	m := InstanceResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}

// TestInstanceResource_Update_InvalidJSON exercises InstanceResource.updateRemote against an httptest mock: success status with a malformed body surfaces Could not decode response body.
func TestInstanceResource_Update_InvalidJSON(t *testing.T) {
	r := &InstanceResource{client: newMockClientStatus(t, 200, "{{")}
	m := InstanceResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode response body")
}

// TestInstanceResource_Update_MapError exercises InstanceResource.updateRemote against an httptest mock: success status with a wrong-typed identifier surfaces Could not map response to state.
func TestInstanceResource_Update_MapError(t *testing.T) {
	r := &InstanceResource{client: newMockClientStatus(t, 200, "{\"id\":12345}")}
	m := InstanceResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not map response to state")
}

// TestInstanceResource_Delete_Happy exercises InstanceResource.deleteRemote against an httptest mock: happy path returns the success status with no errors.
func TestInstanceResource_Delete_Happy(t *testing.T) {
	r := &InstanceResource{client: newMockClientStatus(t, 200, "")}
	m := InstanceResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestInstanceResource_Delete_NilClient exercises InstanceResource.deleteRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestInstanceResource_Delete_NilClient(t *testing.T) {
	r := &InstanceResource{}
	m := InstanceResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestInstanceResource_Delete_BuildError exercises InstanceResource.deleteRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestInstanceResource_Delete_BuildError(t *testing.T) {
	r := &InstanceResource{client: newMalformedBaseURLClient(t)}
	m := InstanceResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestInstanceResource_Delete_SendError exercises InstanceResource.deleteRemote against an httptest mock: transport error surfaces Could not send request.
func TestInstanceResource_Delete_SendError(t *testing.T) {
	r := &InstanceResource{client: newTransportErrorClient(t)}
	m := InstanceResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestInstanceResource_Delete_NotFoundSuccess exercises InstanceResource.deleteRemote against an httptest mock: 404 is treated as already deleted and surfaces no error.
func TestInstanceResource_Delete_NotFoundSuccess(t *testing.T) {
	r := &InstanceResource{client: newMockClientStatus(t, 404, "")}
	m := InstanceResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestInstanceResource_Delete_APIError exercises InstanceResource.deleteRemote against an httptest mock: non-success status surfaces the API error summary.
func TestInstanceResource_Delete_APIError(t *testing.T) {
	r := &InstanceResource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := InstanceResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error deleting mycloud_instance")
}

// TestInstanceResource_Delete_APIErrorReadBody exercises InstanceResource.deleteRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestInstanceResource_Delete_APIErrorReadBody(t *testing.T) {
	r := &InstanceResource{client: newMockClientReadErrorBody(t, 500)}
	m := InstanceResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}
