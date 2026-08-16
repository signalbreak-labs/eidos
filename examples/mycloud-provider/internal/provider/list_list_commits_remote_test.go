package provider

import (
	"context"
	"testing"
)

// TestListCommitsListResource_List_Happy exercises ListCommitsListResource.listRemote against an httptest mock: happy path returns the success status with a JSON array body and no errors.
func TestListCommitsListResource_List_Happy(t *testing.T) {
	r := &ListCommitsListResource{client: newMockClientStatus(t, 200, "[]")}
	m := ListCommitsListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	requireNoErrors(t, diags)
}

// TestListCommitsListResource_List_NilClient exercises ListCommitsListResource.listRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestListCommitsListResource_List_NilClient(t *testing.T) {
	r := &ListCommitsListResource{}
	m := ListCommitsListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Client Not Configured")
}

// TestListCommitsListResource_List_BuildError exercises ListCommitsListResource.listRemote against an httptest mock: malformed base URL surfaces Could not read list response.
func TestListCommitsListResource_List_BuildError(t *testing.T) {
	r := &ListCommitsListResource{client: newMalformedBaseURLClient(t)}
	m := ListCommitsListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestListCommitsListResource_List_SendError exercises ListCommitsListResource.listRemote against an httptest mock: transport error surfaces Could not read list response.
func TestListCommitsListResource_List_SendError(t *testing.T) {
	r := &ListCommitsListResource{client: newTransportErrorClient(t)}
	m := ListCommitsListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestListCommitsListResource_List_InvalidJSON exercises ListCommitsListResource.listRemote against an httptest mock: success status with a non-array body surfaces Could not decode list page.
func TestListCommitsListResource_List_InvalidJSON(t *testing.T) {
	r := &ListCommitsListResource{client: newMockClientStatus(t, 200, "{{")}
	m := ListCommitsListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not decode list page")
}
