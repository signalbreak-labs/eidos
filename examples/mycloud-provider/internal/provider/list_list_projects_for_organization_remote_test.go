package provider

import (
	"context"
	"testing"
)

// TestListProjectsForOrganizationListResource_List_Happy exercises ListProjectsForOrganizationListResource.listRemote against an httptest mock: happy path returns the success status with a JSON array body and no errors.
func TestListProjectsForOrganizationListResource_List_Happy(t *testing.T) {
	r := &ListProjectsForOrganizationListResource{client: newMockClientStatus(t, 200, "[]")}
	m := ListProjectsForOrganizationListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	requireNoErrors(t, diags)
}

// TestListProjectsForOrganizationListResource_List_NilClient exercises ListProjectsForOrganizationListResource.listRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestListProjectsForOrganizationListResource_List_NilClient(t *testing.T) {
	r := &ListProjectsForOrganizationListResource{}
	m := ListProjectsForOrganizationListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Client Not Configured")
}

// TestListProjectsForOrganizationListResource_List_BuildError exercises ListProjectsForOrganizationListResource.listRemote against an httptest mock: malformed base URL surfaces Could not read list response.
func TestListProjectsForOrganizationListResource_List_BuildError(t *testing.T) {
	r := &ListProjectsForOrganizationListResource{client: newMalformedBaseURLClient(t)}
	m := ListProjectsForOrganizationListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestListProjectsForOrganizationListResource_List_SendError exercises ListProjectsForOrganizationListResource.listRemote against an httptest mock: transport error surfaces Could not read list response.
func TestListProjectsForOrganizationListResource_List_SendError(t *testing.T) {
	r := &ListProjectsForOrganizationListResource{client: newTransportErrorClient(t)}
	m := ListProjectsForOrganizationListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestListProjectsForOrganizationListResource_List_InvalidJSON exercises ListProjectsForOrganizationListResource.listRemote against an httptest mock: success status with a non-array body surfaces Could not decode list page.
func TestListProjectsForOrganizationListResource_List_InvalidJSON(t *testing.T) {
	r := &ListProjectsForOrganizationListResource{client: newMockClientStatus(t, 200, "{{")}
	m := ListProjectsForOrganizationListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not decode list page")
}
