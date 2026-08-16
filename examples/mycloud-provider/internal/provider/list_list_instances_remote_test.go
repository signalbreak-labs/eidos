package provider

import (
	"context"
	"testing"
)

// TestListInstancesListResource_List_Happy exercises ListInstancesListResource.listRemote against an httptest mock: happy path returns the success status with a JSON array body and no errors.
func TestListInstancesListResource_List_Happy(t *testing.T) {
	r := &ListInstancesListResource{client: newMockClientStatus(t, 200, "[]")}
	m := ListInstancesListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	requireNoErrors(t, diags)
}

// TestListInstancesListResource_List_NilClient exercises ListInstancesListResource.listRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestListInstancesListResource_List_NilClient(t *testing.T) {
	r := &ListInstancesListResource{}
	m := ListInstancesListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Client Not Configured")
}

// TestListInstancesListResource_List_BuildError exercises ListInstancesListResource.listRemote against an httptest mock: malformed base URL surfaces Could not read list response.
func TestListInstancesListResource_List_BuildError(t *testing.T) {
	r := &ListInstancesListResource{client: newMalformedBaseURLClient(t)}
	m := ListInstancesListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestListInstancesListResource_List_SendError exercises ListInstancesListResource.listRemote against an httptest mock: transport error surfaces Could not read list response.
func TestListInstancesListResource_List_SendError(t *testing.T) {
	r := &ListInstancesListResource{client: newTransportErrorClient(t)}
	m := ListInstancesListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestListInstancesListResource_List_InvalidJSON exercises ListInstancesListResource.listRemote against an httptest mock: success status with a non-array body surfaces Could not decode list page.
func TestListInstancesListResource_List_InvalidJSON(t *testing.T) {
	r := &ListInstancesListResource{client: newMockClientStatus(t, 200, "{{")}
	m := ListInstancesListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not decode list page")
}
