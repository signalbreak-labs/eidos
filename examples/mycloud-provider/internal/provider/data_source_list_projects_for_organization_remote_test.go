package provider

import (
	"context"
	"testing"
)
import "github.com/hashicorp/terraform-plugin-framework/datasource"

// TestListProjectsForOrganizationDataSource_Read_Happy exercises ListProjectsForOrganizationDataSource.readListRemote against an httptest mock: happy path returns the success status with a JSON array body and no errors.
func TestListProjectsForOrganizationDataSource_Read_Happy(t *testing.T) {
	r := &ListProjectsForOrganizationDataSource{client: newMockClientStatus(t, 200, "[]")}
	m := ListProjectsForOrganizationDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestListProjectsForOrganizationDataSource_Read_NilClient exercises ListProjectsForOrganizationDataSource.readListRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestListProjectsForOrganizationDataSource_Read_NilClient(t *testing.T) {
	r := &ListProjectsForOrganizationDataSource{}
	m := ListProjectsForOrganizationDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestListProjectsForOrganizationDataSource_Read_APIError exercises ListProjectsForOrganizationDataSource.readListRemote against an httptest mock: non-success status surfaces Could not read list response carrying the API error status and body.
func TestListProjectsForOrganizationDataSource_Read_APIError(t *testing.T) {
	r := &ListProjectsForOrganizationDataSource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := ListProjectsForOrganizationDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read list response")
	hasErrorContaining(t, resp.Diagnostics, "{\"message\":\"boom\"}")
}

// TestListProjectsForOrganizationDataSource_Read_BuildError exercises ListProjectsForOrganizationDataSource.readListRemote against an httptest mock: malformed base URL surfaces Could not read list response.
func TestListProjectsForOrganizationDataSource_Read_BuildError(t *testing.T) {
	r := &ListProjectsForOrganizationDataSource{client: newMalformedBaseURLClient(t)}
	m := ListProjectsForOrganizationDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read list response")
}

// TestListProjectsForOrganizationDataSource_Read_SendError exercises ListProjectsForOrganizationDataSource.readListRemote against an httptest mock: transport error surfaces Could not read list response.
func TestListProjectsForOrganizationDataSource_Read_SendError(t *testing.T) {
	r := &ListProjectsForOrganizationDataSource{client: newTransportErrorClient(t)}
	m := ListProjectsForOrganizationDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read list response")
}

// TestListProjectsForOrganizationDataSource_Read_InvalidJSON exercises ListProjectsForOrganizationDataSource.readListRemote against an httptest mock: success status with a non-array body surfaces Could not decode list page.
func TestListProjectsForOrganizationDataSource_Read_InvalidJSON(t *testing.T) {
	r := &ListProjectsForOrganizationDataSource{client: newMockClientStatus(t, 200, "{{")}
	m := ListProjectsForOrganizationDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readListRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode list page")
}
