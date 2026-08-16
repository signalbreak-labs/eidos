package provider

import (
	"context"
	"testing"
)

// TestListBranchesListResource_List_Happy exercises ListBranchesListResource.listRemote against an httptest mock: happy path returns the success status with a JSON array body and no errors.
func TestListBranchesListResource_List_Happy(t *testing.T) {
	r := &ListBranchesListResource{client: newMockClientStatus(t, 200, "[]")}
	m := ListBranchesListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	requireNoErrors(t, diags)
}

// TestListBranchesListResource_List_NilClient exercises ListBranchesListResource.listRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestListBranchesListResource_List_NilClient(t *testing.T) {
	r := &ListBranchesListResource{}
	m := ListBranchesListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Client Not Configured")
}

// TestListBranchesListResource_List_BuildError exercises ListBranchesListResource.listRemote against an httptest mock: malformed base URL surfaces Could not read list response.
func TestListBranchesListResource_List_BuildError(t *testing.T) {
	r := &ListBranchesListResource{client: newMalformedBaseURLClient(t)}
	m := ListBranchesListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestListBranchesListResource_List_SendError exercises ListBranchesListResource.listRemote against an httptest mock: transport error surfaces Could not read list response.
func TestListBranchesListResource_List_SendError(t *testing.T) {
	r := &ListBranchesListResource{client: newTransportErrorClient(t)}
	m := ListBranchesListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestListBranchesListResource_List_InvalidJSON exercises ListBranchesListResource.listRemote against an httptest mock: success status with a non-array body surfaces Could not decode list page.
func TestListBranchesListResource_List_InvalidJSON(t *testing.T) {
	r := &ListBranchesListResource{client: newMockClientStatus(t, 200, "{{")}
	m := ListBranchesListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not decode list page")
}
