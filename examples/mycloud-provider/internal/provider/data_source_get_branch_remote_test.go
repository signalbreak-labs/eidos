package provider

import (
	"context"
	"testing"
)
import "github.com/hashicorp/terraform-plugin-framework/datasource"

// TestGetBranchDataSource_Read_Happy exercises GetBranchDataSource.readRemote against an httptest mock: happy path returns the success status with no errors.
func TestGetBranchDataSource_Read_Happy(t *testing.T) {
	r := &GetBranchDataSource{client: newMockClientStatus(t, 200, "{}")}
	m := GetBranchDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestGetBranchDataSource_Read_NilClient exercises GetBranchDataSource.readRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestGetBranchDataSource_Read_NilClient(t *testing.T) {
	r := &GetBranchDataSource{}
	m := GetBranchDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestGetBranchDataSource_Read_BuildError exercises GetBranchDataSource.readRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestGetBranchDataSource_Read_BuildError(t *testing.T) {
	r := &GetBranchDataSource{client: newMalformedBaseURLClient(t)}
	m := GetBranchDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestGetBranchDataSource_Read_SendError exercises GetBranchDataSource.readRemote against an httptest mock: transport error surfaces Could not send request.
func TestGetBranchDataSource_Read_SendError(t *testing.T) {
	r := &GetBranchDataSource{client: newTransportErrorClient(t)}
	m := GetBranchDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestGetBranchDataSource_Read_NotFound exercises GetBranchDataSource.readRemote against an httptest mock: 404 surfaces the requested-resource-not-found error.
func TestGetBranchDataSource_Read_NotFound(t *testing.T) {
	r := &GetBranchDataSource{client: newMockClientStatus(t, 404, "")}
	m := GetBranchDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "The requested resource was not found.")
}

// TestGetBranchDataSource_Read_APIError exercises GetBranchDataSource.readRemote against an httptest mock: non-success status surfaces the API error summary.
func TestGetBranchDataSource_Read_APIError(t *testing.T) {
	r := &GetBranchDataSource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := GetBranchDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error reading mycloud_get_branch")
}

// TestGetBranchDataSource_Read_APIErrorReadBody exercises GetBranchDataSource.readRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestGetBranchDataSource_Read_APIErrorReadBody(t *testing.T) {
	r := &GetBranchDataSource{client: newMockClientReadErrorBody(t, 500)}
	m := GetBranchDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}

// TestGetBranchDataSource_Read_InvalidJSON exercises GetBranchDataSource.readRemote against an httptest mock: success status with a malformed body surfaces Could not decode response body.
func TestGetBranchDataSource_Read_InvalidJSON(t *testing.T) {
	r := &GetBranchDataSource{client: newMockClientStatus(t, 200, "{{")}
	m := GetBranchDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode response body")
}
