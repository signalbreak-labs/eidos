package provider

import (
	"context"
	"testing"
)
import "github.com/hashicorp/terraform-plugin-framework/datasource"

// TestListBranchesDataSource_Read_Happy exercises ListBranchesDataSource.readListRemote against an httptest mock: happy path returns the success status with a JSON array body and no errors.
func TestListBranchesDataSource_Read_Happy(t *testing.T) {
	r := &ListBranchesDataSource{client: newMockClientStatus(t, 200, "[]")}
	m := ListBranchesDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestListBranchesDataSource_Read_NilClient exercises ListBranchesDataSource.readListRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestListBranchesDataSource_Read_NilClient(t *testing.T) {
	r := &ListBranchesDataSource{}
	m := ListBranchesDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestListBranchesDataSource_Read_BuildError exercises ListBranchesDataSource.readListRemote against an httptest mock: malformed base URL surfaces Could not read list response.
func TestListBranchesDataSource_Read_BuildError(t *testing.T) {
	r := &ListBranchesDataSource{client: newMalformedBaseURLClient(t)}
	m := ListBranchesDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read list response")
}

// TestListBranchesDataSource_Read_SendError exercises ListBranchesDataSource.readListRemote against an httptest mock: transport error surfaces Could not read list response.
func TestListBranchesDataSource_Read_SendError(t *testing.T) {
	r := &ListBranchesDataSource{client: newTransportErrorClient(t)}
	m := ListBranchesDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read list response")
}

// TestListBranchesDataSource_Read_InvalidJSON exercises ListBranchesDataSource.readListRemote against an httptest mock: success status with a non-array body surfaces Could not decode list page.
func TestListBranchesDataSource_Read_InvalidJSON(t *testing.T) {
	r := &ListBranchesDataSource{client: newMockClientStatus(t, 200, "{{")}
	m := ListBranchesDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode list page")
}
