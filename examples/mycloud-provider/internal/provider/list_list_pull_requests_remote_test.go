package provider

import (
	"context"
	"testing"
)

// TestListPullRequestsListResource_List_Happy exercises ListPullRequestsListResource.listRemote against an httptest mock: happy path returns the success status with a JSON array body and no errors.
func TestListPullRequestsListResource_List_Happy(t *testing.T) {
	r := &ListPullRequestsListResource{client: newMockClientStatus(t, 200, "[]")}
	m := ListPullRequestsListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	requireNoErrors(t, diags)
}

// TestListPullRequestsListResource_List_NilClient exercises ListPullRequestsListResource.listRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestListPullRequestsListResource_List_NilClient(t *testing.T) {
	r := &ListPullRequestsListResource{}
	m := ListPullRequestsListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Client Not Configured")
}

// TestListPullRequestsListResource_List_BuildError exercises ListPullRequestsListResource.listRemote against an httptest mock: malformed base URL surfaces Could not read list response.
func TestListPullRequestsListResource_List_BuildError(t *testing.T) {
	r := &ListPullRequestsListResource{client: newMalformedBaseURLClient(t)}
	m := ListPullRequestsListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestListPullRequestsListResource_List_SendError exercises ListPullRequestsListResource.listRemote against an httptest mock: transport error surfaces Could not read list response.
func TestListPullRequestsListResource_List_SendError(t *testing.T) {
	r := &ListPullRequestsListResource{client: newTransportErrorClient(t)}
	m := ListPullRequestsListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestListPullRequestsListResource_List_InvalidJSON exercises ListPullRequestsListResource.listRemote against an httptest mock: success status with a non-array body surfaces Could not decode list page.
func TestListPullRequestsListResource_List_InvalidJSON(t *testing.T) {
	r := &ListPullRequestsListResource{client: newMockClientStatus(t, 200, "{{")}
	m := ListPullRequestsListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not decode list page")
}
