package provider

import (
	"context"
	"testing"
)

// TestInstanceListResource_List_Happy exercises InstanceListResource.listRemote against an httptest mock: happy path returns the success status with a JSON array body and no errors.
func TestInstanceListResource_List_Happy(t *testing.T) {
	r := &InstanceListResource{client: newMockClientStatus(t, 200, "[]")}
	m := InstanceListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	requireNoErrors(t, diags)
}

// TestInstanceListResource_List_NilClient exercises InstanceListResource.listRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestInstanceListResource_List_NilClient(t *testing.T) {
	r := &InstanceListResource{}
	m := InstanceListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Client Not Configured")
}

// TestInstanceListResource_List_BuildError exercises InstanceListResource.listRemote against an httptest mock: malformed base URL surfaces Could not read list response.
func TestInstanceListResource_List_BuildError(t *testing.T) {
	r := &InstanceListResource{client: newMalformedBaseURLClient(t)}
	m := InstanceListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestInstanceListResource_List_SendError exercises InstanceListResource.listRemote against an httptest mock: transport error surfaces Could not read list response.
func TestInstanceListResource_List_SendError(t *testing.T) {
	r := &InstanceListResource{client: newTransportErrorClient(t)}
	m := InstanceListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestInstanceListResource_List_InvalidJSON exercises InstanceListResource.listRemote against an httptest mock: success status with a non-array body surfaces Could not decode list page.
func TestInstanceListResource_List_InvalidJSON(t *testing.T) {
	r := &InstanceListResource{client: newMockClientStatus(t, 200, "{{")}
	m := InstanceListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not decode list page")
}
