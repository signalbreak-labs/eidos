package provider

import (
	"context"
	"testing"
)
import "github.com/hashicorp/terraform-plugin-framework/resource"

// TestSecretResource_Create_Happy exercises SecretResource.createRemote against an httptest mock: happy path returns the success status and an identifier in the body.
func TestSecretResource_Create_Happy(t *testing.T) {
	r := &SecretResource{client: newMockClientStatus(t, 201, "{\"id\":\"example-id\"}")}
	m := SecretResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestSecretResource_Create_NilClient exercises SecretResource.createRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestSecretResource_Create_NilClient(t *testing.T) {
	r := &SecretResource{}
	m := SecretResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestSecretResource_Create_BuildError exercises SecretResource.createRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestSecretResource_Create_BuildError(t *testing.T) {
	r := &SecretResource{client: newMalformedBaseURLClient(t)}
	m := SecretResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestSecretResource_Create_SendError exercises SecretResource.createRemote against an httptest mock: transport error surfaces Could not send request.
func TestSecretResource_Create_SendError(t *testing.T) {
	r := &SecretResource{client: newTransportErrorClient(t)}
	m := SecretResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestSecretResource_Create_APIError exercises SecretResource.createRemote against an httptest mock: non-success status surfaces the API error summary.
func TestSecretResource_Create_APIError(t *testing.T) {
	r := &SecretResource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := SecretResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error creating mycloud_secret")
}

// TestSecretResource_Create_APIErrorReadBody exercises SecretResource.createRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestSecretResource_Create_APIErrorReadBody(t *testing.T) {
	r := &SecretResource{client: newMockClientReadErrorBody(t, 500)}
	m := SecretResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}

// TestSecretResource_Create_InvalidJSON exercises SecretResource.createRemote against an httptest mock: success status with a malformed body surfaces Could not decode response body.
func TestSecretResource_Create_InvalidJSON(t *testing.T) {
	r := &SecretResource{client: newMockClientStatus(t, 201, "{{")}
	m := SecretResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode response body")
}

// TestSecretResource_Create_MapError exercises SecretResource.createRemote against an httptest mock: success status with a wrong-typed identifier surfaces Could not map response to state.
func TestSecretResource_Create_MapError(t *testing.T) {
	r := &SecretResource{client: newMockClientStatus(t, 201, "{\"id\":12345}")}
	m := SecretResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not map response to state")
}

// TestSecretResource_Create_MissingID exercises SecretResource.createRemote against an httptest mock: success status with no identifier surfaces the missing-identifier diagnostic.
func TestSecretResource_Create_MissingID(t *testing.T) {
	r := &SecretResource{client: newMockClientStatus(t, 201, "{}")}
	m := SecretResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "did not contain an identifier")
}

// TestSecretResource_Create_LocationFallback exercises SecretResource.createRemote against an httptest mock: success status with no body id but a Location header sets the string identifier from the header.
func TestSecretResource_Create_LocationFallback(t *testing.T) {
	r := &SecretResource{client: newMockClientWithLocation(t, 201, "http://example.test/folders/example-id", "{}")}
	m := SecretResourceModel{}
	resp := &resource.CreateResponse{}
	r.createRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
	if m.Id.ValueString() != "http://example.test/folders/example-id" {
		t.Fatalf("identifier = %q, want %q", m.Id.ValueString(), "http://example.test/folders/example-id")
	}
}

// TestSecretResource_Read_Happy exercises SecretResource.readRemote against an httptest mock: happy path returns the success status and reports removed=false with no errors.
func TestSecretResource_Read_Happy(t *testing.T) {
	r := &SecretResource{client: newMockClientStatus(t, 200, "{}")}
	m := SecretResourceModel{}
	resp := &resource.ReadResponse{}
	removed := r.readRemote(context.Background(), &m, resp)
	if removed {
		t.Fatalf("expected removed=false on happy path")
	}
	requireNoErrors(t, resp.Diagnostics)
}

// TestSecretResource_Read_NilClient exercises SecretResource.readRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestSecretResource_Read_NilClient(t *testing.T) {
	r := &SecretResource{}
	m := SecretResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestSecretResource_Read_BuildError exercises SecretResource.readRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestSecretResource_Read_BuildError(t *testing.T) {
	r := &SecretResource{client: newMalformedBaseURLClient(t)}
	m := SecretResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestSecretResource_Read_SendError exercises SecretResource.readRemote against an httptest mock: transport error surfaces Could not send request.
func TestSecretResource_Read_SendError(t *testing.T) {
	r := &SecretResource{client: newTransportErrorClient(t)}
	m := SecretResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestSecretResource_Read_NotFound exercises SecretResource.readRemote against an httptest mock: 404 reports removed=true with no error so the framework drops the resource from state.
func TestSecretResource_Read_NotFound(t *testing.T) {
	r := &SecretResource{client: newMockClientStatus(t, 404, "")}
	m := SecretResourceModel{}
	resp := &resource.ReadResponse{}
	removed := r.readRemote(context.Background(), &m, resp)
	if !removed {
		t.Fatalf("expected removed=true on 404")
	}
	requireNoErrors(t, resp.Diagnostics)
}

// TestSecretResource_Read_APIError exercises SecretResource.readRemote against an httptest mock: non-success status surfaces the API error summary.
func TestSecretResource_Read_APIError(t *testing.T) {
	r := &SecretResource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := SecretResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error reading mycloud_secret")
}

// TestSecretResource_Read_APIErrorReadBody exercises SecretResource.readRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestSecretResource_Read_APIErrorReadBody(t *testing.T) {
	r := &SecretResource{client: newMockClientReadErrorBody(t, 500)}
	m := SecretResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}

// TestSecretResource_Read_InvalidJSON exercises SecretResource.readRemote against an httptest mock: success status with a malformed body surfaces Could not decode response body.
func TestSecretResource_Read_InvalidJSON(t *testing.T) {
	r := &SecretResource{client: newMockClientStatus(t, 200, "{{")}
	m := SecretResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode response body")
}

// TestSecretResource_Read_MapError exercises SecretResource.readRemote against an httptest mock: success status with a wrong-typed identifier surfaces Could not map response to state.
func TestSecretResource_Read_MapError(t *testing.T) {
	r := &SecretResource{client: newMockClientStatus(t, 200, "{\"id\":12345}")}
	m := SecretResourceModel{}
	resp := &resource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not map response to state")
}

// TestSecretResource_Update_Happy exercises SecretResource.updateRemote against an httptest mock: happy path returns the success status with no errors.
func TestSecretResource_Update_Happy(t *testing.T) {
	r := &SecretResource{client: newMockClientStatus(t, 200, "{}")}
	m := SecretResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestSecretResource_Update_NilClient exercises SecretResource.updateRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestSecretResource_Update_NilClient(t *testing.T) {
	r := &SecretResource{}
	m := SecretResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestSecretResource_Update_BuildError exercises SecretResource.updateRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestSecretResource_Update_BuildError(t *testing.T) {
	r := &SecretResource{client: newMalformedBaseURLClient(t)}
	m := SecretResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestSecretResource_Update_SendError exercises SecretResource.updateRemote against an httptest mock: transport error surfaces Could not send request.
func TestSecretResource_Update_SendError(t *testing.T) {
	r := &SecretResource{client: newTransportErrorClient(t)}
	m := SecretResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestSecretResource_Update_APIError exercises SecretResource.updateRemote against an httptest mock: non-success status surfaces the API error summary.
func TestSecretResource_Update_APIError(t *testing.T) {
	r := &SecretResource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := SecretResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error updating mycloud_secret")
}

// TestSecretResource_Update_APIErrorReadBody exercises SecretResource.updateRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestSecretResource_Update_APIErrorReadBody(t *testing.T) {
	r := &SecretResource{client: newMockClientReadErrorBody(t, 500)}
	m := SecretResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}

// TestSecretResource_Update_InvalidJSON exercises SecretResource.updateRemote against an httptest mock: success status with a malformed body surfaces Could not decode response body.
func TestSecretResource_Update_InvalidJSON(t *testing.T) {
	r := &SecretResource{client: newMockClientStatus(t, 200, "{{")}
	m := SecretResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode response body")
}

// TestSecretResource_Update_MapError exercises SecretResource.updateRemote against an httptest mock: success status with a wrong-typed identifier surfaces Could not map response to state.
func TestSecretResource_Update_MapError(t *testing.T) {
	r := &SecretResource{client: newMockClientStatus(t, 200, "{\"id\":12345}")}
	m := SecretResourceModel{}
	resp := &resource.UpdateResponse{}
	r.updateRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not map response to state")
}

// TestSecretResource_Delete_Happy exercises SecretResource.deleteRemote against an httptest mock: happy path returns the success status with no errors.
func TestSecretResource_Delete_Happy(t *testing.T) {
	r := &SecretResource{client: newMockClientStatus(t, 200, "")}
	m := SecretResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestSecretResource_Delete_NilClient exercises SecretResource.deleteRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestSecretResource_Delete_NilClient(t *testing.T) {
	r := &SecretResource{}
	m := SecretResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestSecretResource_Delete_BuildError exercises SecretResource.deleteRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestSecretResource_Delete_BuildError(t *testing.T) {
	r := &SecretResource{client: newMalformedBaseURLClient(t)}
	m := SecretResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestSecretResource_Delete_SendError exercises SecretResource.deleteRemote against an httptest mock: transport error surfaces Could not send request.
func TestSecretResource_Delete_SendError(t *testing.T) {
	r := &SecretResource{client: newTransportErrorClient(t)}
	m := SecretResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestSecretResource_Delete_NotFoundSuccess exercises SecretResource.deleteRemote against an httptest mock: 404 is treated as already deleted and surfaces no error.
func TestSecretResource_Delete_NotFoundSuccess(t *testing.T) {
	r := &SecretResource{client: newMockClientStatus(t, 404, "")}
	m := SecretResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestSecretResource_Delete_APIError exercises SecretResource.deleteRemote against an httptest mock: non-success status surfaces the API error summary.
func TestSecretResource_Delete_APIError(t *testing.T) {
	r := &SecretResource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := SecretResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error deleting mycloud_secret")
}

// TestSecretResource_Delete_APIErrorReadBody exercises SecretResource.deleteRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestSecretResource_Delete_APIErrorReadBody(t *testing.T) {
	r := &SecretResource{client: newMockClientReadErrorBody(t, 500)}
	m := SecretResourceModel{}
	resp := &resource.DeleteResponse{}
	r.deleteRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}
