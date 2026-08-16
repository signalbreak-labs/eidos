package provider

import (
	"context"
	"testing"
)

// TestListStacksListResource_List_Happy exercises ListStacksListResource.listRemote against an httptest mock: happy path returns the success status with a JSON array body and no errors.
func TestListStacksListResource_List_Happy(t *testing.T) {
	r := &ListStacksListResource{client: newMockClientStatus(t, 200, "[]")}
	m := ListStacksListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	requireNoErrors(t, diags)
}

// TestListStacksListResource_List_NilClient exercises ListStacksListResource.listRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestListStacksListResource_List_NilClient(t *testing.T) {
	r := &ListStacksListResource{}
	m := ListStacksListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Client Not Configured")
}

// TestListStacksListResource_List_BuildError exercises ListStacksListResource.listRemote against an httptest mock: malformed base URL surfaces Could not read list response.
func TestListStacksListResource_List_BuildError(t *testing.T) {
	r := &ListStacksListResource{client: newMalformedBaseURLClient(t)}
	m := ListStacksListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestListStacksListResource_List_SendError exercises ListStacksListResource.listRemote against an httptest mock: transport error surfaces Could not read list response.
func TestListStacksListResource_List_SendError(t *testing.T) {
	r := &ListStacksListResource{client: newTransportErrorClient(t)}
	m := ListStacksListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestListStacksListResource_List_InvalidJSON exercises ListStacksListResource.listRemote against an httptest mock: success status with a non-array body surfaces Could not decode list page.
func TestListStacksListResource_List_InvalidJSON(t *testing.T) {
	r := &ListStacksListResource{client: newMockClientStatus(t, 200, "{{")}
	m := ListStacksListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not decode list page")
}
