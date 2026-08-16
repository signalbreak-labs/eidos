package provider

import (
	"context"
	"testing"
)
import "github.com/hashicorp/terraform-plugin-framework/datasource"

// TestGetMemberDataSource_Read_Happy exercises GetMemberDataSource.readRemote against an httptest mock: happy path returns the success status with no errors.
func TestGetMemberDataSource_Read_Happy(t *testing.T) {
	r := &GetMemberDataSource{client: newMockClientStatus(t, 200, "{}")}
	m := GetMemberDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestGetMemberDataSource_Read_NilClient exercises GetMemberDataSource.readRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestGetMemberDataSource_Read_NilClient(t *testing.T) {
	r := &GetMemberDataSource{}
	m := GetMemberDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestGetMemberDataSource_Read_BuildError exercises GetMemberDataSource.readRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestGetMemberDataSource_Read_BuildError(t *testing.T) {
	r := &GetMemberDataSource{client: newMalformedBaseURLClient(t)}
	m := GetMemberDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestGetMemberDataSource_Read_SendError exercises GetMemberDataSource.readRemote against an httptest mock: transport error surfaces Could not send request.
func TestGetMemberDataSource_Read_SendError(t *testing.T) {
	r := &GetMemberDataSource{client: newTransportErrorClient(t)}
	m := GetMemberDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestGetMemberDataSource_Read_NotFound exercises GetMemberDataSource.readRemote against an httptest mock: 404 surfaces the requested-resource-not-found error.
func TestGetMemberDataSource_Read_NotFound(t *testing.T) {
	r := &GetMemberDataSource{client: newMockClientStatus(t, 404, "")}
	m := GetMemberDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "The requested resource was not found.")
}

// TestGetMemberDataSource_Read_APIError exercises GetMemberDataSource.readRemote against an httptest mock: non-success status surfaces the API error summary.
func TestGetMemberDataSource_Read_APIError(t *testing.T) {
	r := &GetMemberDataSource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := GetMemberDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error reading mycloud_get_member")
}

// TestGetMemberDataSource_Read_APIErrorReadBody exercises GetMemberDataSource.readRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestGetMemberDataSource_Read_APIErrorReadBody(t *testing.T) {
	r := &GetMemberDataSource{client: newMockClientReadErrorBody(t, 500)}
	m := GetMemberDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}

// TestGetMemberDataSource_Read_InvalidJSON exercises GetMemberDataSource.readRemote against an httptest mock: success status with a malformed body surfaces Could not decode response body.
func TestGetMemberDataSource_Read_InvalidJSON(t *testing.T) {
	r := &GetMemberDataSource{client: newMockClientStatus(t, 200, "{{")}
	m := GetMemberDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode response body")
}
