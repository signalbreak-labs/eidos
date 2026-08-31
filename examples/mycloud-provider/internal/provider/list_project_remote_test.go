package provider

import (
	"context"
	"testing"
)

// TestProjectListResource_List_Happy exercises ProjectListResource.listRemote against an httptest mock: happy path returns the success status with a JSON array body and no errors.
func TestProjectListResource_List_Happy(t *testing.T) {
	r := &ProjectListResource{client: newMockClientStatus(t, 200, "[]")}
	m := ProjectListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	requireNoErrors(t, diags)
}

// TestProjectListResource_List_NilClient exercises ProjectListResource.listRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestProjectListResource_List_NilClient(t *testing.T) {
	r := &ProjectListResource{}
	m := ProjectListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Client Not Configured")
}

// TestProjectListResource_List_BuildError exercises ProjectListResource.listRemote against an httptest mock: malformed base URL surfaces Could not read list response.
func TestProjectListResource_List_BuildError(t *testing.T) {
	r := &ProjectListResource{client: newMalformedBaseURLClient(t)}
	m := ProjectListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestProjectListResource_List_SendError exercises ProjectListResource.listRemote against an httptest mock: transport error surfaces Could not read list response.
func TestProjectListResource_List_SendError(t *testing.T) {
	r := &ProjectListResource{client: newTransportErrorClient(t)}
	m := ProjectListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not read list response")
}

// TestProjectListResource_List_InvalidJSON exercises ProjectListResource.listRemote against an httptest mock: success status with a non-array body surfaces Could not decode list page.
func TestProjectListResource_List_InvalidJSON(t *testing.T) {
	r := &ProjectListResource{client: newMockClientStatus(t, 200, "{{")}
	m := ProjectListResourceModel{}
	_, diags := r.listRemote(context.Background(), &m)
	hasErrorContaining(t, diags, "Could not decode list page")
}
