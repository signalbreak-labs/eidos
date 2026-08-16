package provider

import (
	"context"
	"testing"
)
import "github.com/hashicorp/terraform-plugin-framework/datasource"

// TestListStacksDataSource_Read_Happy exercises ListStacksDataSource.readListRemote against an httptest mock: happy path returns the success status with a JSON array body and no errors.
func TestListStacksDataSource_Read_Happy(t *testing.T) {
	r := &ListStacksDataSource{client: newMockClientStatus(t, 200, "[]")}
	m := ListStacksDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestListStacksDataSource_Read_NilClient exercises ListStacksDataSource.readListRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestListStacksDataSource_Read_NilClient(t *testing.T) {
	r := &ListStacksDataSource{}
	m := ListStacksDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestListStacksDataSource_Read_BuildError exercises ListStacksDataSource.readListRemote against an httptest mock: malformed base URL surfaces Could not read list response.
func TestListStacksDataSource_Read_BuildError(t *testing.T) {
	r := &ListStacksDataSource{client: newMalformedBaseURLClient(t)}
	m := ListStacksDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read list response")
}

// TestListStacksDataSource_Read_SendError exercises ListStacksDataSource.readListRemote against an httptest mock: transport error surfaces Could not read list response.
func TestListStacksDataSource_Read_SendError(t *testing.T) {
	r := &ListStacksDataSource{client: newTransportErrorClient(t)}
	m := ListStacksDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read list response")
}

// TestListStacksDataSource_Read_InvalidJSON exercises ListStacksDataSource.readListRemote against an httptest mock: success status with a non-array body surfaces Could not decode list page.
func TestListStacksDataSource_Read_InvalidJSON(t *testing.T) {
	r := &ListStacksDataSource{client: newMockClientStatus(t, 200, "{{")}
	m := ListStacksDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode list page")
}
