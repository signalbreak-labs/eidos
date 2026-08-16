package provider

import (
	"context"
	"testing"
)
import "github.com/hashicorp/terraform-plugin-framework/datasource"

// TestListWorkspacesDataSource_Read_Happy exercises ListWorkspacesDataSource.readListRemote against an httptest mock: happy path returns the success status with a JSON array body and no errors.
func TestListWorkspacesDataSource_Read_Happy(t *testing.T) {
	r := &ListWorkspacesDataSource{client: newMockClientStatus(t, 200, "[]")}
	m := ListWorkspacesDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestListWorkspacesDataSource_Read_NilClient exercises ListWorkspacesDataSource.readListRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestListWorkspacesDataSource_Read_NilClient(t *testing.T) {
	r := &ListWorkspacesDataSource{}
	m := ListWorkspacesDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestListWorkspacesDataSource_Read_BuildError exercises ListWorkspacesDataSource.readListRemote against an httptest mock: malformed base URL surfaces Could not read list response.
func TestListWorkspacesDataSource_Read_BuildError(t *testing.T) {
	r := &ListWorkspacesDataSource{client: newMalformedBaseURLClient(t)}
	m := ListWorkspacesDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read list response")
}

// TestListWorkspacesDataSource_Read_SendError exercises ListWorkspacesDataSource.readListRemote against an httptest mock: transport error surfaces Could not read list response.
func TestListWorkspacesDataSource_Read_SendError(t *testing.T) {
	r := &ListWorkspacesDataSource{client: newTransportErrorClient(t)}
	m := ListWorkspacesDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read list response")
}

// TestListWorkspacesDataSource_Read_InvalidJSON exercises ListWorkspacesDataSource.readListRemote against an httptest mock: success status with a non-array body surfaces Could not decode list page.
func TestListWorkspacesDataSource_Read_InvalidJSON(t *testing.T) {
	r := &ListWorkspacesDataSource{client: newMockClientStatus(t, 200, "{{")}
	m := ListWorkspacesDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode list page")
}
