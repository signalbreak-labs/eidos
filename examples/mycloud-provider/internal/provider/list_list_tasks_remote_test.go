package provider

import (
	"context"
	"testing"
)

// TestListTasksListResource_List_Happy exercises ListTasksListResource.listRemote against an httptest mock: happy path returns the success status with a JSON array body and no errors.
func TestListTasksListResource_List_Happy(t *testing.T) {
	r := &ListTasksListResource{client: newMockClientStatus(t, 200, "[]")}
	m := ListTasksListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	requireNoErrors(t, diags)
}

// TestListTasksListResource_List_NilClient exercises ListTasksListResource.listRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestListTasksListResource_List_NilClient(t *testing.T) {
	r := &ListTasksListResource{}
	m := ListTasksListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Client Not Configured")
}

// TestListTasksListResource_List_BuildError exercises ListTasksListResource.listRemote against an httptest mock: malformed base URL surfaces Could not read list response.
func TestListTasksListResource_List_BuildError(t *testing.T) {
	r := &ListTasksListResource{client: newMalformedBaseURLClient(t)}
	m := ListTasksListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestListTasksListResource_List_SendError exercises ListTasksListResource.listRemote against an httptest mock: transport error surfaces Could not read list response.
func TestListTasksListResource_List_SendError(t *testing.T) {
	r := &ListTasksListResource{client: newTransportErrorClient(t)}
	m := ListTasksListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestListTasksListResource_List_InvalidJSON exercises ListTasksListResource.listRemote against an httptest mock: success status with a non-array body surfaces Could not decode list page.
func TestListTasksListResource_List_InvalidJSON(t *testing.T) {
	r := &ListTasksListResource{client: newMockClientStatus(t, 200, "{{")}
	m := ListTasksListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not decode list page")
}
