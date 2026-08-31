package provider

import (
	"context"
	"testing"
)

// TestConfigListResource_List_Happy exercises ConfigListResource.listRemote against an httptest mock: happy path returns the success status with a JSON array body and no errors.
func TestConfigListResource_List_Happy(t *testing.T) {
	r := &ConfigListResource{client: newMockClientStatus(t, 200, "[]")}
	m := ConfigListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	requireNoErrors(t, diags)
}

// TestConfigListResource_List_NilClient exercises ConfigListResource.listRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestConfigListResource_List_NilClient(t *testing.T) {
	r := &ConfigListResource{}
	m := ConfigListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Client Not Configured")
}

// TestConfigListResource_List_BuildError exercises ConfigListResource.listRemote against an httptest mock: malformed base URL surfaces Could not read list response.
func TestConfigListResource_List_BuildError(t *testing.T) {
	r := &ConfigListResource{client: newMalformedBaseURLClient(t)}
	m := ConfigListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestConfigListResource_List_SendError exercises ConfigListResource.listRemote against an httptest mock: transport error surfaces Could not read list response.
func TestConfigListResource_List_SendError(t *testing.T) {
	r := &ConfigListResource{client: newTransportErrorClient(t)}
	m := ConfigListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestConfigListResource_List_InvalidJSON exercises ConfigListResource.listRemote against an httptest mock: success status with a non-array body surfaces Could not decode list page.
func TestConfigListResource_List_InvalidJSON(t *testing.T) {
	r := &ConfigListResource{client: newMockClientStatus(t, 200, "{{")}
	m := ConfigListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not decode list page")
}
