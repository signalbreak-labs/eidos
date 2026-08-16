package provider

import (
	"context"
	"testing"
)

// TestListMembersListResource_List_Happy exercises ListMembersListResource.listRemote against an httptest mock: happy path returns the success status with a JSON array body and no errors.
func TestListMembersListResource_List_Happy(t *testing.T) {
	r := &ListMembersListResource{client: newMockClientStatus(t, 200, "[]")}
	_, diags := r.listRemote(context.Background())
	requireNoErrors(t, diags)
}

// TestListMembersListResource_List_NilClient exercises ListMembersListResource.listRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestListMembersListResource_List_NilClient(t *testing.T) {
	r := &ListMembersListResource{}
	_, diags := r.listRemote(context.Background())
	hasErrorContaining(t, diags, "Client Not Configured")
}

// TestListMembersListResource_List_BuildError exercises ListMembersListResource.listRemote against an httptest mock: malformed base URL surfaces Could not read list response.
func TestListMembersListResource_List_BuildError(t *testing.T) {
	r := &ListMembersListResource{client: newMalformedBaseURLClient(t)}
	_, diags := r.listRemote(context.Background())
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestListMembersListResource_List_SendError exercises ListMembersListResource.listRemote against an httptest mock: transport error surfaces Could not read list response.
func TestListMembersListResource_List_SendError(t *testing.T) {
	r := &ListMembersListResource{client: newTransportErrorClient(t)}
	_, diags := r.listRemote(context.Background())
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestListMembersListResource_List_InvalidJSON exercises ListMembersListResource.listRemote against an httptest mock: success status with a non-array body surfaces Could not decode list page.
func TestListMembersListResource_List_InvalidJSON(t *testing.T) {
	r := &ListMembersListResource{client: newMockClientStatus(t, 200, "{{")}
	_, diags := r.listRemote(context.Background())
	hasErrorContaining(t, diags, "Could not decode list page")
}
