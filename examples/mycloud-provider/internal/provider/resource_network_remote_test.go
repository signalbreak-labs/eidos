package provider

import (
	"context"
	"testing"
)
import "github.com/hashicorp/terraform-plugin-framework/resource"

// TestNetworkResource_Create_Happy exercises NetworkResource.createRemote against an httptest mock: happy path returns the success status and an identifier in the body.
func TestNetworkResource_Create_Happy(t *testing.T) {
	r := &NetworkResource{client: newMockClientStatus(t, 201, "{\"id\":\"example-id\"}")}
	m := NetworkResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestNetworkResource_Create_NilClient exercises NetworkResource.createRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestNetworkResource_Create_NilClient(t *testing.T) {
	r := &NetworkResource{}
	m := NetworkResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestNetworkResource_Create_BuildError exercises NetworkResource.createRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestNetworkResource_Create_BuildError(t *testing.T) {
	r := &NetworkResource{client: newMalformedBaseURLClient(t)}
	m := NetworkResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestNetworkResource_Create_SendError exercises NetworkResource.createRemote against an httptest mock: transport error surfaces Could not send request.
func TestNetworkResource_Create_SendError(t *testing.T) {
	r := &NetworkResource{client: newTransportErrorClient(t)}
	m := NetworkResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestNetworkResource_Create_APIError exercises NetworkResource.createRemote against an httptest mock: non-success status surfaces the API error summary.
func TestNetworkResource_Create_APIError(t *testing.T) {
	r := &NetworkResource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := NetworkResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error creating mycloud_network")
}

// TestNetworkResource_Create_APIErrorReadBody exercises NetworkResource.createRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestNetworkResource_Create_APIErrorReadBody(t *testing.T) {
	r := &NetworkResource{client: newMockClientReadErrorBody(t, 500)}
	m := NetworkResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}

// TestNetworkResource_Create_InvalidJSON exercises NetworkResource.createRemote against an httptest mock: success status with a malformed body surfaces Could not decode response body.
func TestNetworkResource_Create_InvalidJSON(t *testing.T) {
	r := &NetworkResource{client: newMockClientStatus(t, 201, "{{")}
	m := NetworkResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode response body")
}

// TestNetworkResource_Create_MapError exercises NetworkResource.createRemote against an httptest mock: success status with a wrong-typed identifier surfaces Could not map response to state.
func TestNetworkResource_Create_MapError(t *testing.T) {
	r := &NetworkResource{client: newMockClientStatus(t, 201, "{\"id\":12345}")}
	m := NetworkResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not map response to state")
}

// TestNetworkResource_Create_MissingID exercises NetworkResource.createRemote against an httptest mock: success status with no identifier surfaces the missing-identifier diagnostic.
func TestNetworkResource_Create_MissingID(t *testing.T) {
	r := &NetworkResource{client: newMockClientStatus(t, 201, "{}")}
	m := NetworkResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "did not contain an identifier")
}

// TestNetworkResource_Create_LocationFallback exercises NetworkResource.createRemote against an httptest mock: success status with no body id but a Location header sets the string identifier from the header's trailing path segment.
func TestNetworkResource_Create_LocationFallback(t *testing.T) {
	r := &NetworkResource{client: newMockClientWithLocation(t, 201, "http://example.test/folders/example-id", "{}")}
	m := NetworkResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
	if m.Id.ValueString() != "example-id" {
		t.Fatalf("identifier = %q, want %q", m.Id.ValueString(), "example-id")
	}
}

// TestNetworkResource_Read_Happy exercises NetworkResource.readRemote against an httptest mock: happy path returns the success status and reports removed=false with no errors.
func TestNetworkResource_Read_Happy(t *testing.T) {
	r := &NetworkResource{client: newMockClientStatus(t, 200, "{}")}
	m := NetworkResourceModel{}
	resp := &resource.ReadResponse{}
	removed := r.readRemote(context.Background(), &m, resp)
	if removed {
		t.Fatalf("expected removed=false on happy path")
	}
	requireNoErrors(t, resp.Diagnostics)
}

// TestNetworkResource_Read_NilClient exercises NetworkResource.readRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestNetworkResource_Read_NilClient(t *testing.T) {
	r := &NetworkResource{}
	m := NetworkResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestNetworkResource_Read_BuildError exercises NetworkResource.readRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestNetworkResource_Read_BuildError(t *testing.T) {
	r := &NetworkResource{client: newMalformedBaseURLClient(t)}
	m := NetworkResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestNetworkResource_Read_SendError exercises NetworkResource.readRemote against an httptest mock: transport error surfaces Could not send request.
func TestNetworkResource_Read_SendError(t *testing.T) {
	r := &NetworkResource{client: newTransportErrorClient(t)}
	m := NetworkResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestNetworkResource_Read_NotFound exercises NetworkResource.readRemote against an httptest mock: 404 reports removed=true with no error so the framework drops the resource from state.
func TestNetworkResource_Read_NotFound(t *testing.T) {
	r := &NetworkResource{client: newMockClientStatus(t, 404, "")}
	m := NetworkResourceModel{}
	resp := &resource.ReadResponse{}
	removed := r.readRemote(context.Background(), &m, resp)
	if !removed {
		t.Fatalf("expected removed=true on 404")
	}
	requireNoErrors(t, resp.Diagnostics)
}

// TestNetworkResource_Read_APIError exercises NetworkResource.readRemote against an httptest mock: non-success status surfaces the API error summary.
func TestNetworkResource_Read_APIError(t *testing.T) {
	r := &NetworkResource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := NetworkResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error reading mycloud_network")
}

// TestNetworkResource_Read_APIErrorReadBody exercises NetworkResource.readRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestNetworkResource_Read_APIErrorReadBody(t *testing.T) {
	r := &NetworkResource{client: newMockClientReadErrorBody(t, 500)}
	m := NetworkResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}

// TestNetworkResource_Read_InvalidJSON exercises NetworkResource.readRemote against an httptest mock: success status with a malformed body surfaces Could not decode response body.
func TestNetworkResource_Read_InvalidJSON(t *testing.T) {
	r := &NetworkResource{client: newMockClientStatus(t, 200, "{{")}
	m := NetworkResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode response body")
}

// TestNetworkResource_Read_MapError exercises NetworkResource.readRemote against an httptest mock: success status with a wrong-typed identifier surfaces Could not map response to state.
func TestNetworkResource_Read_MapError(t *testing.T) {
	r := &NetworkResource{client: newMockClientStatus(t, 200, "{\"id\":12345}")}
	m := NetworkResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not map response to state")
}

// TestNetworkResource_Update_Happy exercises NetworkResource.updateRemote against an httptest mock: happy path returns the success status with no errors.
func TestNetworkResource_Update_Happy(t *testing.T) {
	r := &NetworkResource{client: newMockClientStatus(t, 200, "{}")}
	m := NetworkResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestNetworkResource_Update_NilClient exercises NetworkResource.updateRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestNetworkResource_Update_NilClient(t *testing.T) {
	r := &NetworkResource{}
	m := NetworkResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestNetworkResource_Update_BuildError exercises NetworkResource.updateRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestNetworkResource_Update_BuildError(t *testing.T) {
	r := &NetworkResource{client: newMalformedBaseURLClient(t)}
	m := NetworkResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestNetworkResource_Update_SendError exercises NetworkResource.updateRemote against an httptest mock: transport error surfaces Could not send request.
func TestNetworkResource_Update_SendError(t *testing.T) {
	r := &NetworkResource{client: newTransportErrorClient(t)}
	m := NetworkResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestNetworkResource_Update_APIError exercises NetworkResource.updateRemote against an httptest mock: non-success status surfaces the API error summary.
func TestNetworkResource_Update_APIError(t *testing.T) {
	r := &NetworkResource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := NetworkResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error updating mycloud_network")
}

// TestNetworkResource_Update_APIErrorReadBody exercises NetworkResource.updateRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestNetworkResource_Update_APIErrorReadBody(t *testing.T) {
	r := &NetworkResource{client: newMockClientReadErrorBody(t, 500)}
	m := NetworkResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}

// TestNetworkResource_Update_InvalidJSON exercises NetworkResource.updateRemote against an httptest mock: success status with a malformed body surfaces Could not decode response body.
func TestNetworkResource_Update_InvalidJSON(t *testing.T) {
	r := &NetworkResource{client: newMockClientStatus(t, 200, "{{")}
	m := NetworkResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode response body")
}

// TestNetworkResource_Update_MapError exercises NetworkResource.updateRemote against an httptest mock: success status with a wrong-typed identifier surfaces Could not map response to state.
func TestNetworkResource_Update_MapError(t *testing.T) {
	r := &NetworkResource{client: newMockClientStatus(t, 200, "{\"id\":12345}")}
	m := NetworkResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not map response to state")
}

// TestNetworkResource_Delete_Happy exercises NetworkResource.deleteRemote against an httptest mock: happy path returns the success status with no errors.
func TestNetworkResource_Delete_Happy(t *testing.T) {
	r := &NetworkResource{client: newMockClientStatus(t, 200, "")}
	m := NetworkResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestNetworkResource_Delete_NilClient exercises NetworkResource.deleteRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestNetworkResource_Delete_NilClient(t *testing.T) {
	r := &NetworkResource{}
	m := NetworkResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestNetworkResource_Delete_BuildError exercises NetworkResource.deleteRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestNetworkResource_Delete_BuildError(t *testing.T) {
	r := &NetworkResource{client: newMalformedBaseURLClient(t)}
	m := NetworkResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestNetworkResource_Delete_SendError exercises NetworkResource.deleteRemote against an httptest mock: transport error surfaces Could not send request.
func TestNetworkResource_Delete_SendError(t *testing.T) {
	r := &NetworkResource{client: newTransportErrorClient(t)}
	m := NetworkResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestNetworkResource_Delete_NotFoundSuccess exercises NetworkResource.deleteRemote against an httptest mock: 404 is treated as already deleted and surfaces no error.
func TestNetworkResource_Delete_NotFoundSuccess(t *testing.T) {
	r := &NetworkResource{client: newMockClientStatus(t, 404, "")}
	m := NetworkResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestNetworkResource_Delete_APIError exercises NetworkResource.deleteRemote against an httptest mock: non-success status surfaces the API error summary.
func TestNetworkResource_Delete_APIError(t *testing.T) {
	r := &NetworkResource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := NetworkResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error deleting mycloud_network")
}

// TestNetworkResource_Delete_APIErrorReadBody exercises NetworkResource.deleteRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestNetworkResource_Delete_APIErrorReadBody(t *testing.T) {
	r := &NetworkResource{client: newMockClientReadErrorBody(t, 500)}
	m := NetworkResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}
