package provider

import (
	"context"
	"testing"
)
import "github.com/hashicorp/terraform-plugin-framework/datasource"

// TestListTasksDataSource_Read_Happy exercises ListTasksDataSource.readListRemote against an httptest mock: happy path returns the success status with a JSON array body and no errors.
func TestListTasksDataSource_Read_Happy(t *testing.T) {
	r := &ListTasksDataSource{client: newMockClientStatus(t, 200, "[]")}
	m := ListTasksDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestListTasksDataSource_Read_NilClient exercises ListTasksDataSource.readListRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestListTasksDataSource_Read_NilClient(t *testing.T) {
	r := &ListTasksDataSource{}
	m := ListTasksDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestListTasksDataSource_Read_BuildError exercises ListTasksDataSource.readListRemote against an httptest mock: malformed base URL surfaces Could not read list response.
func TestListTasksDataSource_Read_BuildError(t *testing.T) {
	r := &ListTasksDataSource{client: newMalformedBaseURLClient(t)}
	m := ListTasksDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read list response")
}

// TestListTasksDataSource_Read_SendError exercises ListTasksDataSource.readListRemote against an httptest mock: transport error surfaces Could not read list response.
func TestListTasksDataSource_Read_SendError(t *testing.T) {
	r := &ListTasksDataSource{client: newTransportErrorClient(t)}
	m := ListTasksDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read list response")
}

// TestListTasksDataSource_Read_InvalidJSON exercises ListTasksDataSource.readListRemote against an httptest mock: success status with a non-array body surfaces Could not decode list page.
func TestListTasksDataSource_Read_InvalidJSON(t *testing.T) {
	r := &ListTasksDataSource{client: newMockClientStatus(t, 200, "{{")}
	m := ListTasksDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode list page")
}
