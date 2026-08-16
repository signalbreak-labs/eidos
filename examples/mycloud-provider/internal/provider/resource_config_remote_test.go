package provider

import (
	"context"
	"testing"
)
import "github.com/hashicorp/terraform-plugin-framework/resource"

// TestConfigResource_Create_Happy exercises ConfigResource.createRemote against an httptest mock: happy path returns the success status and an identifier in the body.
func TestConfigResource_Create_Happy(t *testing.T) {
	r := &ConfigResource{client: newMockClientStatus(t, 201, "{\"id\":\"example-id\"}")}
	m := ConfigResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestConfigResource_Create_NilClient exercises ConfigResource.createRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestConfigResource_Create_NilClient(t *testing.T) {
	r := &ConfigResource{}
	m := ConfigResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestConfigResource_Create_BuildError exercises ConfigResource.createRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestConfigResource_Create_BuildError(t *testing.T) {
	r := &ConfigResource{client: newMalformedBaseURLClient(t)}
	m := ConfigResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestConfigResource_Create_SendError exercises ConfigResource.createRemote against an httptest mock: transport error surfaces Could not send request.
func TestConfigResource_Create_SendError(t *testing.T) {
	r := &ConfigResource{client: newTransportErrorClient(t)}
	m := ConfigResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestConfigResource_Create_APIError exercises ConfigResource.createRemote against an httptest mock: non-success status surfaces the API error summary.
func TestConfigResource_Create_APIError(t *testing.T) {
	r := &ConfigResource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := ConfigResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error creating mycloud_config")
}

// TestConfigResource_Create_APIErrorReadBody exercises ConfigResource.createRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestConfigResource_Create_APIErrorReadBody(t *testing.T) {
	r := &ConfigResource{client: newMockClientReadErrorBody(t, 500)}
	m := ConfigResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}

// TestConfigResource_Create_InvalidJSON exercises ConfigResource.createRemote against an httptest mock: success status with a malformed body surfaces Could not decode response body.
func TestConfigResource_Create_InvalidJSON(t *testing.T) {
	r := &ConfigResource{client: newMockClientStatus(t, 201, "{{")}
	m := ConfigResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode response body")
}

// TestConfigResource_Create_MapError exercises ConfigResource.createRemote against an httptest mock: success status with a wrong-typed identifier surfaces Could not map response to state.
func TestConfigResource_Create_MapError(t *testing.T) {
	r := &ConfigResource{client: newMockClientStatus(t, 201, "{\"id\":12345}")}
	m := ConfigResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not map response to state")
}

// TestConfigResource_Create_MissingID exercises ConfigResource.createRemote against an httptest mock: success status with no identifier surfaces the missing-identifier diagnostic.
func TestConfigResource_Create_MissingID(t *testing.T) {
	r := &ConfigResource{client: newMockClientStatus(t, 201, "{}")}
	m := ConfigResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "did not contain an identifier")
}

// TestConfigResource_Create_LocationFallback exercises ConfigResource.createRemote against an httptest mock: success status with no body id but a Location header sets the string identifier from the header.
func TestConfigResource_Create_LocationFallback(t *testing.T) {
	r := &ConfigResource{client: newMockClientWithLocation(t, 201, "http://example.test/folders/example-id", "{}")}
	m := ConfigResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
	if m.Id.ValueString() != "http://example.test/folders/example-id" {
		t.Fatalf("identifier = %q, want %q", m.Id.ValueString(), "http://example.test/folders/example-id")
	}
}

// TestConfigResource_Read_Happy exercises ConfigResource.readRemote against an httptest mock: happy path returns the success status and reports removed=false with no errors.
func TestConfigResource_Read_Happy(t *testing.T) {
	r := &ConfigResource{client: newMockClientStatus(t, 200, "{}")}
	m := ConfigResourceModel{}
	resp := &resource.ReadResponse{}
	removed := r.readRemote(context.Background(), &m, resp)
	if removed {
		t.Fatalf("expected removed=false on happy path")
	}
	requireNoErrors(t, resp.Diagnostics)
}

// TestConfigResource_Read_NilClient exercises ConfigResource.readRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestConfigResource_Read_NilClient(t *testing.T) {
	r := &ConfigResource{}
	m := ConfigResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestConfigResource_Read_BuildError exercises ConfigResource.readRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestConfigResource_Read_BuildError(t *testing.T) {
	r := &ConfigResource{client: newMalformedBaseURLClient(t)}
	m := ConfigResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestConfigResource_Read_SendError exercises ConfigResource.readRemote against an httptest mock: transport error surfaces Could not send request.
func TestConfigResource_Read_SendError(t *testing.T) {
	r := &ConfigResource{client: newTransportErrorClient(t)}
	m := ConfigResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestConfigResource_Read_NotFound exercises ConfigResource.readRemote against an httptest mock: 404 reports removed=true with no error so the framework drops the resource from state.
func TestConfigResource_Read_NotFound(t *testing.T) {
	r := &ConfigResource{client: newMockClientStatus(t, 404, "")}
	m := ConfigResourceModel{}
	resp := &resource.ReadResponse{}
	removed := r.readRemote(context.Background(), &m, resp)
	if !removed {
		t.Fatalf("expected removed=true on 404")
	}
	requireNoErrors(t, resp.Diagnostics)
}

// TestConfigResource_Read_APIError exercises ConfigResource.readRemote against an httptest mock: non-success status surfaces the API error summary.
func TestConfigResource_Read_APIError(t *testing.T) {
	r := &ConfigResource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := ConfigResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error reading mycloud_config")
}

// TestConfigResource_Read_APIErrorReadBody exercises ConfigResource.readRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestConfigResource_Read_APIErrorReadBody(t *testing.T) {
	r := &ConfigResource{client: newMockClientReadErrorBody(t, 500)}
	m := ConfigResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}

// TestConfigResource_Read_InvalidJSON exercises ConfigResource.readRemote against an httptest mock: success status with a malformed body surfaces Could not decode response body.
func TestConfigResource_Read_InvalidJSON(t *testing.T) {
	r := &ConfigResource{client: newMockClientStatus(t, 200, "{{")}
	m := ConfigResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode response body")
}

// TestConfigResource_Read_MapError exercises ConfigResource.readRemote against an httptest mock: success status with a wrong-typed identifier surfaces Could not map response to state.
func TestConfigResource_Read_MapError(t *testing.T) {
	r := &ConfigResource{client: newMockClientStatus(t, 200, "{\"id\":12345}")}
	m := ConfigResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not map response to state")
}

// TestConfigResource_Update_Happy exercises ConfigResource.updateRemote against an httptest mock: happy path returns the success status with no errors.
func TestConfigResource_Update_Happy(t *testing.T) {
	r := &ConfigResource{client: newMockClientStatus(t, 200, "{}")}
	m := ConfigResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestConfigResource_Update_NilClient exercises ConfigResource.updateRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestConfigResource_Update_NilClient(t *testing.T) {
	r := &ConfigResource{}
	m := ConfigResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestConfigResource_Update_BuildError exercises ConfigResource.updateRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestConfigResource_Update_BuildError(t *testing.T) {
	r := &ConfigResource{client: newMalformedBaseURLClient(t)}
	m := ConfigResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestConfigResource_Update_SendError exercises ConfigResource.updateRemote against an httptest mock: transport error surfaces Could not send request.
func TestConfigResource_Update_SendError(t *testing.T) {
	r := &ConfigResource{client: newTransportErrorClient(t)}
	m := ConfigResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestConfigResource_Update_APIError exercises ConfigResource.updateRemote against an httptest mock: non-success status surfaces the API error summary.
func TestConfigResource_Update_APIError(t *testing.T) {
	r := &ConfigResource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := ConfigResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error updating mycloud_config")
}

// TestConfigResource_Update_APIErrorReadBody exercises ConfigResource.updateRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestConfigResource_Update_APIErrorReadBody(t *testing.T) {
	r := &ConfigResource{client: newMockClientReadErrorBody(t, 500)}
	m := ConfigResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}

// TestConfigResource_Update_InvalidJSON exercises ConfigResource.updateRemote against an httptest mock: success status with a malformed body surfaces Could not decode response body.
func TestConfigResource_Update_InvalidJSON(t *testing.T) {
	r := &ConfigResource{client: newMockClientStatus(t, 200, "{{")}
	m := ConfigResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode response body")
}

// TestConfigResource_Update_MapError exercises ConfigResource.updateRemote against an httptest mock: success status with a wrong-typed identifier surfaces Could not map response to state.
func TestConfigResource_Update_MapError(t *testing.T) {
	r := &ConfigResource{client: newMockClientStatus(t, 200, "{\"id\":12345}")}
	m := ConfigResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not map response to state")
}

// TestConfigResource_Delete_Happy exercises ConfigResource.deleteRemote against an httptest mock: happy path returns the success status with no errors.
func TestConfigResource_Delete_Happy(t *testing.T) {
	r := &ConfigResource{client: newMockClientStatus(t, 200, "")}
	m := ConfigResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestConfigResource_Delete_NilClient exercises ConfigResource.deleteRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestConfigResource_Delete_NilClient(t *testing.T) {
	r := &ConfigResource{}
	m := ConfigResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestConfigResource_Delete_BuildError exercises ConfigResource.deleteRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestConfigResource_Delete_BuildError(t *testing.T) {
	r := &ConfigResource{client: newMalformedBaseURLClient(t)}
	m := ConfigResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestConfigResource_Delete_SendError exercises ConfigResource.deleteRemote against an httptest mock: transport error surfaces Could not send request.
func TestConfigResource_Delete_SendError(t *testing.T) {
	r := &ConfigResource{client: newTransportErrorClient(t)}
	m := ConfigResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestConfigResource_Delete_NotFoundSuccess exercises ConfigResource.deleteRemote against an httptest mock: 404 is treated as already deleted and surfaces no error.
func TestConfigResource_Delete_NotFoundSuccess(t *testing.T) {
	r := &ConfigResource{client: newMockClientStatus(t, 404, "")}
	m := ConfigResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestConfigResource_Delete_APIError exercises ConfigResource.deleteRemote against an httptest mock: non-success status surfaces the API error summary.
func TestConfigResource_Delete_APIError(t *testing.T) {
	r := &ConfigResource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := ConfigResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error deleting mycloud_config")
}

// TestConfigResource_Delete_APIErrorReadBody exercises ConfigResource.deleteRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestConfigResource_Delete_APIErrorReadBody(t *testing.T) {
	r := &ConfigResource{client: newMockClientReadErrorBody(t, 500)}
	m := ConfigResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}
