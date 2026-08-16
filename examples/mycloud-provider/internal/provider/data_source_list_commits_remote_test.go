package provider

import (
	"context"
	"testing"
)
import "github.com/hashicorp/terraform-plugin-framework/datasource"

// TestListCommitsDataSource_Read_Happy exercises ListCommitsDataSource.readListRemote against an httptest mock: happy path returns the success status with a JSON array body and no errors.
func TestListCommitsDataSource_Read_Happy(t *testing.T) {
	r := &ListCommitsDataSource{client: newMockClientStatus(t, 200, "[]")}
	m := ListCommitsDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestListCommitsDataSource_Read_NilClient exercises ListCommitsDataSource.readListRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestListCommitsDataSource_Read_NilClient(t *testing.T) {
	r := &ListCommitsDataSource{}
	m := ListCommitsDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestListCommitsDataSource_Read_BuildError exercises ListCommitsDataSource.readListRemote against an httptest mock: malformed base URL surfaces Could not read list response.
func TestListCommitsDataSource_Read_BuildError(t *testing.T) {
	r := &ListCommitsDataSource{client: newMalformedBaseURLClient(t)}
	m := ListCommitsDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read list response")
}

// TestListCommitsDataSource_Read_SendError exercises ListCommitsDataSource.readListRemote against an httptest mock: transport error surfaces Could not read list response.
func TestListCommitsDataSource_Read_SendError(t *testing.T) {
	r := &ListCommitsDataSource{client: newTransportErrorClient(t)}
	m := ListCommitsDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read list response")
}

// TestListCommitsDataSource_Read_InvalidJSON exercises ListCommitsDataSource.readListRemote against an httptest mock: success status with a non-array body surfaces Could not decode list page.
func TestListCommitsDataSource_Read_InvalidJSON(t *testing.T) {
	r := &ListCommitsDataSource{client: newMockClientStatus(t, 200, "{{")}
	m := ListCommitsDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode list page")
}
