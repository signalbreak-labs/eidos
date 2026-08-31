package provider

import (
	"context"
	"testing"
)

// TestWorkspaceListResource_List_Happy exercises WorkspaceListResource.listRemote against an httptest mock: happy path returns the success status with a JSON array body and no errors.
func TestWorkspaceListResource_List_Happy(t *testing.T) {
	r := &WorkspaceListResource{client: newMockClientStatus(t, 200, "[]")}
	_, diags := r.listRemote(context.Background())
	requireNoErrors(t, diags)
}

// TestWorkspaceListResource_List_NilClient exercises WorkspaceListResource.listRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestWorkspaceListResource_List_NilClient(t *testing.T) {
	r := &WorkspaceListResource{}
	_, diags := r.listRemote(context.Background())
	hasErrorContaining(t, diags, "Client Not Configured")
}

// TestWorkspaceListResource_List_BuildError exercises WorkspaceListResource.listRemote against an httptest mock: malformed base URL surfaces Could not read list response.
func TestWorkspaceListResource_List_BuildError(t *testing.T) {
	r := &WorkspaceListResource{client: newMalformedBaseURLClient(t)}
	_, diags := r.listRemote(context.Background())
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestWorkspaceListResource_List_SendError exercises WorkspaceListResource.listRemote against an httptest mock: transport error surfaces Could not read list response.
func TestWorkspaceListResource_List_SendError(t *testing.T) {
	r := &WorkspaceListResource{client: newTransportErrorClient(t)}
	_, diags := r.listRemote(context.Background())
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestWorkspaceListResource_List_InvalidJSON exercises WorkspaceListResource.listRemote against an httptest mock: success status with a non-array body surfaces Could not decode list page.
func TestWorkspaceListResource_List_InvalidJSON(t *testing.T) {
	r := &WorkspaceListResource{client: newMockClientStatus(t, 200, "{{")}
	_, diags := r.listRemote(context.Background())
	hasErrorContaining(t, diags, "Could not decode list page")
}
