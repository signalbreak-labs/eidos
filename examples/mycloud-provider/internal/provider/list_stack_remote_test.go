package provider

import (
	"context"
	"testing"
)

// TestStackListResource_List_Happy exercises StackListResource.listRemote against an httptest mock: happy path returns the success status with a JSON array body and no errors.
func TestStackListResource_List_Happy(t *testing.T) {
	r := &StackListResource{client: newMockClientStatus(t, 200, "[]")}
	m := StackListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	requireNoErrors(t, diags)
}

// TestStackListResource_List_NilClient exercises StackListResource.listRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestStackListResource_List_NilClient(t *testing.T) {
	r := &StackListResource{}
	m := StackListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Client Not Configured")
}

// TestStackListResource_List_BuildError exercises StackListResource.listRemote against an httptest mock: malformed base URL surfaces Could not read list response.
func TestStackListResource_List_BuildError(t *testing.T) {
	r := &StackListResource{client: newMalformedBaseURLClient(t)}
	m := StackListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestStackListResource_List_SendError exercises StackListResource.listRemote against an httptest mock: transport error surfaces Could not read list response.
func TestStackListResource_List_SendError(t *testing.T) {
	r := &StackListResource{client: newTransportErrorClient(t)}
	m := StackListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestStackListResource_List_InvalidJSON exercises StackListResource.listRemote against an httptest mock: success status with a non-array body surfaces Could not decode list page.
func TestStackListResource_List_InvalidJSON(t *testing.T) {
	r := &StackListResource{client: newMockClientStatus(t, 200, "{{")}
	m := StackListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not decode list page")
}
