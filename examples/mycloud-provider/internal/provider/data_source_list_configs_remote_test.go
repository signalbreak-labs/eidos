package provider

import (
	"context"
	"testing"
)
import "github.com/hashicorp/terraform-plugin-framework/datasource"

// TestListConfigsDataSource_Read_Happy exercises ListConfigsDataSource.readListRemote against an httptest mock: happy path returns the success status with a JSON array body and no errors.
func TestListConfigsDataSource_Read_Happy(t *testing.T) {
	r := &ListConfigsDataSource{client: newMockClientStatus(t, 200, "[]")}
	m := ListConfigsDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestListConfigsDataSource_Read_NilClient exercises ListConfigsDataSource.readListRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestListConfigsDataSource_Read_NilClient(t *testing.T) {
	r := &ListConfigsDataSource{}
	m := ListConfigsDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestListConfigsDataSource_Read_BuildError exercises ListConfigsDataSource.readListRemote against an httptest mock: malformed base URL surfaces Could not read list response.
func TestListConfigsDataSource_Read_BuildError(t *testing.T) {
	r := &ListConfigsDataSource{client: newMalformedBaseURLClient(t)}
	m := ListConfigsDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read list response")
}

// TestListConfigsDataSource_Read_SendError exercises ListConfigsDataSource.readListRemote against an httptest mock: transport error surfaces Could not read list response.
func TestListConfigsDataSource_Read_SendError(t *testing.T) {
	r := &ListConfigsDataSource{client: newTransportErrorClient(t)}
	m := ListConfigsDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read list response")
}

// TestListConfigsDataSource_Read_InvalidJSON exercises ListConfigsDataSource.readListRemote against an httptest mock: success status with a non-array body surfaces Could not decode list page.
func TestListConfigsDataSource_Read_InvalidJSON(t *testing.T) {
	r := &ListConfigsDataSource{client: newMockClientStatus(t, 200, "{{")}
	m := ListConfigsDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode list page")
}
