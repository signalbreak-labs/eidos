package provider

import (
	"context"
	"testing"
)

// TestListConfigsListResource_List_Happy exercises ListConfigsListResource.listRemote against an httptest mock: happy path returns the success status with a JSON array body and no errors.
func TestListConfigsListResource_List_Happy(t *testing.T) {
	r := &ListConfigsListResource{client: newMockClientStatus(t, 200, "[]")}
	m := ListConfigsListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	requireNoErrors(t, diags)
}

// TestListConfigsListResource_List_NilClient exercises ListConfigsListResource.listRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestListConfigsListResource_List_NilClient(t *testing.T) {
	r := &ListConfigsListResource{}
	m := ListConfigsListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Client Not Configured")
}

// TestListConfigsListResource_List_BuildError exercises ListConfigsListResource.listRemote against an httptest mock: malformed base URL surfaces Could not read list response.
func TestListConfigsListResource_List_BuildError(t *testing.T) {
	r := &ListConfigsListResource{client: newMalformedBaseURLClient(t)}
	m := ListConfigsListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestListConfigsListResource_List_SendError exercises ListConfigsListResource.listRemote against an httptest mock: transport error surfaces Could not read list response.
func TestListConfigsListResource_List_SendError(t *testing.T) {
	r := &ListConfigsListResource{client: newTransportErrorClient(t)}
	m := ListConfigsListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestListConfigsListResource_List_InvalidJSON exercises ListConfigsListResource.listRemote against an httptest mock: success status with a non-array body surfaces Could not decode list page.
func TestListConfigsListResource_List_InvalidJSON(t *testing.T) {
	r := &ListConfigsListResource{client: newMockClientStatus(t, 200, "{{")}
	m := ListConfigsListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not decode list page")
}
