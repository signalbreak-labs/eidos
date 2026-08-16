package provider

import (
	"context"
	"testing"
)
import "github.com/hashicorp/terraform-plugin-framework/datasource"

// TestListNetworksDataSource_Read_Happy exercises ListNetworksDataSource.readListRemote against an httptest mock: happy path returns the success status with a JSON array body and no errors.
func TestListNetworksDataSource_Read_Happy(t *testing.T) {
	r := &ListNetworksDataSource{client: newMockClientStatus(t, 200, "[]")}
	m := ListNetworksDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestListNetworksDataSource_Read_NilClient exercises ListNetworksDataSource.readListRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestListNetworksDataSource_Read_NilClient(t *testing.T) {
	r := &ListNetworksDataSource{}
	m := ListNetworksDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestListNetworksDataSource_Read_BuildError exercises ListNetworksDataSource.readListRemote against an httptest mock: malformed base URL surfaces Could not read list response.
func TestListNetworksDataSource_Read_BuildError(t *testing.T) {
	r := &ListNetworksDataSource{client: newMalformedBaseURLClient(t)}
	m := ListNetworksDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read list response")
}

// TestListNetworksDataSource_Read_SendError exercises ListNetworksDataSource.readListRemote against an httptest mock: transport error surfaces Could not read list response.
func TestListNetworksDataSource_Read_SendError(t *testing.T) {
	r := &ListNetworksDataSource{client: newTransportErrorClient(t)}
	m := ListNetworksDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read list response")
}

// TestListNetworksDataSource_Read_InvalidJSON exercises ListNetworksDataSource.readListRemote against an httptest mock: success status with a non-array body surfaces Could not decode list page.
func TestListNetworksDataSource_Read_InvalidJSON(t *testing.T) {
	r := &ListNetworksDataSource{client: newMockClientStatus(t, 200, "{{")}
	m := ListNetworksDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode list page")
}
