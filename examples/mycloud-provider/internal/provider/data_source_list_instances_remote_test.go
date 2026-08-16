package provider

import (
	"context"
	"testing"
)
import "github.com/hashicorp/terraform-plugin-framework/datasource"

// TestListInstancesDataSource_Read_Happy exercises ListInstancesDataSource.readListRemote against an httptest mock: happy path returns the success status with a JSON array body and no errors.
func TestListInstancesDataSource_Read_Happy(t *testing.T) {
	r := &ListInstancesDataSource{client: newMockClientStatus(t, 200, "[]")}
	m := ListInstancesDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestListInstancesDataSource_Read_NilClient exercises ListInstancesDataSource.readListRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestListInstancesDataSource_Read_NilClient(t *testing.T) {
	r := &ListInstancesDataSource{}
	m := ListInstancesDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestListInstancesDataSource_Read_BuildError exercises ListInstancesDataSource.readListRemote against an httptest mock: malformed base URL surfaces Could not read list response.
func TestListInstancesDataSource_Read_BuildError(t *testing.T) {
	r := &ListInstancesDataSource{client: newMalformedBaseURLClient(t)}
	m := ListInstancesDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read list response")
}

// TestListInstancesDataSource_Read_SendError exercises ListInstancesDataSource.readListRemote against an httptest mock: transport error surfaces Could not read list response.
func TestListInstancesDataSource_Read_SendError(t *testing.T) {
	r := &ListInstancesDataSource{client: newTransportErrorClient(t)}
	m := ListInstancesDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read list response")
}

// TestListInstancesDataSource_Read_InvalidJSON exercises ListInstancesDataSource.readListRemote against an httptest mock: success status with a non-array body surfaces Could not decode list page.
func TestListInstancesDataSource_Read_InvalidJSON(t *testing.T) {
	r := &ListInstancesDataSource{client: newMockClientStatus(t, 200, "{{")}
	m := ListInstancesDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode list page")
}
