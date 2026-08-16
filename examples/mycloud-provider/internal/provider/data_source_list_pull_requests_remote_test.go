package provider

import (
	"context"
	"testing"
)
import "github.com/hashicorp/terraform-plugin-framework/datasource"

// TestListPullRequestsDataSource_Read_Happy exercises ListPullRequestsDataSource.readListRemote against an httptest mock: happy path returns the success status with a JSON array body and no errors.
func TestListPullRequestsDataSource_Read_Happy(t *testing.T) {
	r := &ListPullRequestsDataSource{client: newMockClientStatus(t, 200, "[]")}
	m := ListPullRequestsDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestListPullRequestsDataSource_Read_NilClient exercises ListPullRequestsDataSource.readListRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestListPullRequestsDataSource_Read_NilClient(t *testing.T) {
	r := &ListPullRequestsDataSource{}
	m := ListPullRequestsDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestListPullRequestsDataSource_Read_BuildError exercises ListPullRequestsDataSource.readListRemote against an httptest mock: malformed base URL surfaces Could not read list response.
func TestListPullRequestsDataSource_Read_BuildError(t *testing.T) {
	r := &ListPullRequestsDataSource{client: newMalformedBaseURLClient(t)}
	m := ListPullRequestsDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read list response")
}

// TestListPullRequestsDataSource_Read_SendError exercises ListPullRequestsDataSource.readListRemote against an httptest mock: transport error surfaces Could not read list response.
func TestListPullRequestsDataSource_Read_SendError(t *testing.T) {
	r := &ListPullRequestsDataSource{client: newTransportErrorClient(t)}
	m := ListPullRequestsDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read list response")
}

// TestListPullRequestsDataSource_Read_InvalidJSON exercises ListPullRequestsDataSource.readListRemote against an httptest mock: success status with a non-array body surfaces Could not decode list page.
func TestListPullRequestsDataSource_Read_InvalidJSON(t *testing.T) {
	r := &ListPullRequestsDataSource{client: newMockClientStatus(t, 200, "{{")}
	m := ListPullRequestsDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode list page")
}
