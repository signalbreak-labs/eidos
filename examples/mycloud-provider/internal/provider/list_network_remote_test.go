package provider

import (
	"context"
	"testing"
)

// TestNetworkListResource_List_Happy exercises NetworkListResource.listRemote against an httptest mock: happy path returns the success status with a JSON array body and no errors.
func TestNetworkListResource_List_Happy(t *testing.T) {
	r := &NetworkListResource{client: newMockClientStatus(t, 200, "[]")}
	m := NetworkListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	requireNoErrors(t, diags)
}

// TestNetworkListResource_List_NilClient exercises NetworkListResource.listRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestNetworkListResource_List_NilClient(t *testing.T) {
	r := &NetworkListResource{}
	m := NetworkListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Client Not Configured")
}

// TestNetworkListResource_List_BuildError exercises NetworkListResource.listRemote against an httptest mock: malformed base URL surfaces Could not read list response.
func TestNetworkListResource_List_BuildError(t *testing.T) {
	r := &NetworkListResource{client: newMalformedBaseURLClient(t)}
	m := NetworkListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestNetworkListResource_List_SendError exercises NetworkListResource.listRemote against an httptest mock: transport error surfaces Could not read list response.
func TestNetworkListResource_List_SendError(t *testing.T) {
	r := &NetworkListResource{client: newTransportErrorClient(t)}
	m := NetworkListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestNetworkListResource_List_InvalidJSON exercises NetworkListResource.listRemote against an httptest mock: success status with a non-array body surfaces Could not decode list page.
func TestNetworkListResource_List_InvalidJSON(t *testing.T) {
	r := &NetworkListResource{client: newMockClientStatus(t, 200, "{{")}
	m := NetworkListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not decode list page")
}
