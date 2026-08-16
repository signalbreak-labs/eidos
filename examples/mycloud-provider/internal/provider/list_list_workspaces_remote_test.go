package provider

import (
	"context"
	"testing"
)

// TestListWorkspacesListResource_List_Happy exercises ListWorkspacesListResource.listRemote against an httptest mock: happy path returns the success status with a JSON array body and no errors.
func TestListWorkspacesListResource_List_Happy(t *testing.T) {
	r := &ListWorkspacesListResource{client: newMockClientStatus(t, 200, "[]")}
	_, diags := r.listRemote(context.Background())
	requireNoErrors(t, diags)
}

// TestListWorkspacesListResource_List_NilClient exercises ListWorkspacesListResource.listRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestListWorkspacesListResource_List_NilClient(t *testing.T) {
	r := &ListWorkspacesListResource{}
	_, diags := r.listRemote(context.Background())
	hasErrorContaining(t, diags, "Client Not Configured")
}

// TestListWorkspacesListResource_List_BuildError exercises ListWorkspacesListResource.listRemote against an httptest mock: malformed base URL surfaces Could not read list response.
func TestListWorkspacesListResource_List_BuildError(t *testing.T) {
	r := &ListWorkspacesListResource{client: newMalformedBaseURLClient(t)}
	_, diags := r.listRemote(context.Background())
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestListWorkspacesListResource_List_SendError exercises ListWorkspacesListResource.listRemote against an httptest mock: transport error surfaces Could not read list response.
func TestListWorkspacesListResource_List_SendError(t *testing.T) {
	r := &ListWorkspacesListResource{client: newTransportErrorClient(t)}
	_, diags := r.listRemote(context.Background())
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestListWorkspacesListResource_List_InvalidJSON exercises ListWorkspacesListResource.listRemote against an httptest mock: success status with a non-array body surfaces Could not decode list page.
func TestListWorkspacesListResource_List_InvalidJSON(t *testing.T) {
	r := &ListWorkspacesListResource{client: newMockClientStatus(t, 200, "{{")}
	_, diags := r.listRemote(context.Background())
	hasErrorContaining(t, diags, "Could not decode list page")
}
