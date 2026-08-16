package provider

import (
	"context"
	"testing"
)
import "github.com/hashicorp/terraform-plugin-framework/datasource"

// TestListSecretsDataSource_Read_Happy exercises ListSecretsDataSource.readListRemote against an httptest mock: happy path returns the success status with a JSON array body and no errors.
func TestListSecretsDataSource_Read_Happy(t *testing.T) {
	r := &ListSecretsDataSource{client: newMockClientStatus(t, 200, "[]")}
	m := ListSecretsDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestListSecretsDataSource_Read_NilClient exercises ListSecretsDataSource.readListRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestListSecretsDataSource_Read_NilClient(t *testing.T) {
	r := &ListSecretsDataSource{}
	m := ListSecretsDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestListSecretsDataSource_Read_BuildError exercises ListSecretsDataSource.readListRemote against an httptest mock: malformed base URL surfaces Could not read list response.
func TestListSecretsDataSource_Read_BuildError(t *testing.T) {
	r := &ListSecretsDataSource{client: newMalformedBaseURLClient(t)}
	m := ListSecretsDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read list response")
}

// TestListSecretsDataSource_Read_SendError exercises ListSecretsDataSource.readListRemote against an httptest mock: transport error surfaces Could not read list response.
func TestListSecretsDataSource_Read_SendError(t *testing.T) {
	r := &ListSecretsDataSource{client: newTransportErrorClient(t)}
	m := ListSecretsDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read list response")
}

// TestListSecretsDataSource_Read_InvalidJSON exercises ListSecretsDataSource.readListRemote against an httptest mock: success status with a non-array body surfaces Could not decode list page.
func TestListSecretsDataSource_Read_InvalidJSON(t *testing.T) {
	r := &ListSecretsDataSource{client: newMockClientStatus(t, 200, "{{")}
	m := ListSecretsDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode list page")
}
