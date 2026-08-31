package provider

import (
	"context"
	"testing"
)

// TestSecretListResource_List_Happy exercises SecretListResource.listRemote against an httptest mock: happy path returns the success status with a JSON array body and no errors.
func TestSecretListResource_List_Happy(t *testing.T) {
	r := &SecretListResource{client: newMockClientStatus(t, 200, "[]")}
	m := SecretListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	requireNoErrors(t, diags)
}

// TestSecretListResource_List_NilClient exercises SecretListResource.listRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestSecretListResource_List_NilClient(t *testing.T) {
	r := &SecretListResource{}
	m := SecretListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Client Not Configured")
}

// TestSecretListResource_List_BuildError exercises SecretListResource.listRemote against an httptest mock: malformed base URL surfaces Could not read list response.
func TestSecretListResource_List_BuildError(t *testing.T) {
	r := &SecretListResource{client: newMalformedBaseURLClient(t)}
	m := SecretListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestSecretListResource_List_SendError exercises SecretListResource.listRemote against an httptest mock: transport error surfaces Could not read list response.
func TestSecretListResource_List_SendError(t *testing.T) {
	r := &SecretListResource{client: newTransportErrorClient(t)}
	m := SecretListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestSecretListResource_List_InvalidJSON exercises SecretListResource.listRemote against an httptest mock: success status with a non-array body surfaces Could not decode list page.
func TestSecretListResource_List_InvalidJSON(t *testing.T) {
	r := &SecretListResource{client: newMockClientStatus(t, 200, "{{")}
	m := SecretListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not decode list page")
}
