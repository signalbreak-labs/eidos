package provider

import (
	"context"
	"testing"
)

// TestListNetworksListResource_List_Happy exercises ListNetworksListResource.listRemote against an httptest mock: happy path returns the success status with a JSON array body and no errors.
func TestListNetworksListResource_List_Happy(t *testing.T) {
	r := &ListNetworksListResource{client: newMockClientStatus(t, 200, "[]")}
	m := ListNetworksListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	requireNoErrors(t, diags)
}

// TestListNetworksListResource_List_NilClient exercises ListNetworksListResource.listRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestListNetworksListResource_List_NilClient(t *testing.T) {
	r := &ListNetworksListResource{}
	m := ListNetworksListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Client Not Configured")
}

// TestListNetworksListResource_List_BuildError exercises ListNetworksListResource.listRemote against an httptest mock: malformed base URL surfaces Could not read list response.
func TestListNetworksListResource_List_BuildError(t *testing.T) {
	r := &ListNetworksListResource{client: newMalformedBaseURLClient(t)}
	m := ListNetworksListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestListNetworksListResource_List_SendError exercises ListNetworksListResource.listRemote against an httptest mock: transport error surfaces Could not read list response.
func TestListNetworksListResource_List_SendError(t *testing.T) {
	r := &ListNetworksListResource{client: newTransportErrorClient(t)}
	m := ListNetworksListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestListNetworksListResource_List_InvalidJSON exercises ListNetworksListResource.listRemote against an httptest mock: success status with a non-array body surfaces Could not decode list page.
func TestListNetworksListResource_List_InvalidJSON(t *testing.T) {
	r := &ListNetworksListResource{client: newMockClientStatus(t, 200, "{{")}
	m := ListNetworksListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not decode list page")
}
