package provider

import (
	"context"
	"testing"
)

// TestListSecretsListResource_List_Happy exercises ListSecretsListResource.listRemote against an httptest mock: happy path returns the success status with a JSON array body and no errors.
func TestListSecretsListResource_List_Happy(t *testing.T) {
	r := &ListSecretsListResource{client: newMockClientStatus(t, 200, "[]")}
	m := ListSecretsListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	requireNoErrors(t, diags)
}

// TestListSecretsListResource_List_NilClient exercises ListSecretsListResource.listRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestListSecretsListResource_List_NilClient(t *testing.T) {
	r := &ListSecretsListResource{}
	m := ListSecretsListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Client Not Configured")
}

// TestListSecretsListResource_List_BuildError exercises ListSecretsListResource.listRemote against an httptest mock: malformed base URL surfaces Could not read list response.
func TestListSecretsListResource_List_BuildError(t *testing.T) {
	r := &ListSecretsListResource{client: newMalformedBaseURLClient(t)}
	m := ListSecretsListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestListSecretsListResource_List_SendError exercises ListSecretsListResource.listRemote against an httptest mock: transport error surfaces Could not read list response.
func TestListSecretsListResource_List_SendError(t *testing.T) {
	r := &ListSecretsListResource{client: newTransportErrorClient(t)}
	m := ListSecretsListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestListSecretsListResource_List_InvalidJSON exercises ListSecretsListResource.listRemote against an httptest mock: success status with a non-array body surfaces Could not decode list page.
func TestListSecretsListResource_List_InvalidJSON(t *testing.T) {
	r := &ListSecretsListResource{client: newMockClientStatus(t, 200, "{{")}
	m := ListSecretsListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not decode list page")
}
