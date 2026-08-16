package provider

import (
	"context"
	"testing"
)
import "github.com/hashicorp/terraform-plugin-framework/datasource"

// TestListMembersDataSource_Read_Happy exercises ListMembersDataSource.readListRemote against an httptest mock: happy path returns the success status with a JSON array body and no errors.
func TestListMembersDataSource_Read_Happy(t *testing.T) {
	r := &ListMembersDataSource{client: newMockClientStatus(t, 200, "[]")}
	m := ListMembersDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestListMembersDataSource_Read_NilClient exercises ListMembersDataSource.readListRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestListMembersDataSource_Read_NilClient(t *testing.T) {
	r := &ListMembersDataSource{}
	m := ListMembersDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestListMembersDataSource_Read_BuildError exercises ListMembersDataSource.readListRemote against an httptest mock: malformed base URL surfaces Could not read list response.
func TestListMembersDataSource_Read_BuildError(t *testing.T) {
	r := &ListMembersDataSource{client: newMalformedBaseURLClient(t)}
	m := ListMembersDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read list response")
}

// TestListMembersDataSource_Read_SendError exercises ListMembersDataSource.readListRemote against an httptest mock: transport error surfaces Could not read list response.
func TestListMembersDataSource_Read_SendError(t *testing.T) {
	r := &ListMembersDataSource{client: newTransportErrorClient(t)}
	m := ListMembersDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read list response")
}

// TestListMembersDataSource_Read_InvalidJSON exercises ListMembersDataSource.readListRemote against an httptest mock: success status with a non-array body surfaces Could not decode list page.
func TestListMembersDataSource_Read_InvalidJSON(t *testing.T) {
	r := &ListMembersDataSource{client: newMockClientStatus(t, 200, "{{")}
	m := ListMembersDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode list page")
}
