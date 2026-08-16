package provider

import (
	"context"
	"testing"
)
import "github.com/hashicorp/terraform-plugin-framework/datasource"

// TestGetCommitDataSource_Read_Happy exercises GetCommitDataSource.readRemote against an httptest mock: happy path returns the success status with no errors.
func TestGetCommitDataSource_Read_Happy(t *testing.T) {
	r := &GetCommitDataSource{client: newMockClientStatus(t, 200, "{}")}
	m := GetCommitDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestGetCommitDataSource_Read_NilClient exercises GetCommitDataSource.readRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestGetCommitDataSource_Read_NilClient(t *testing.T) {
	r := &GetCommitDataSource{}
	m := GetCommitDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestGetCommitDataSource_Read_BuildError exercises GetCommitDataSource.readRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestGetCommitDataSource_Read_BuildError(t *testing.T) {
	r := &GetCommitDataSource{client: newMalformedBaseURLClient(t)}
	m := GetCommitDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestGetCommitDataSource_Read_SendError exercises GetCommitDataSource.readRemote against an httptest mock: transport error surfaces Could not send request.
func TestGetCommitDataSource_Read_SendError(t *testing.T) {
	r := &GetCommitDataSource{client: newTransportErrorClient(t)}
	m := GetCommitDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestGetCommitDataSource_Read_NotFound exercises GetCommitDataSource.readRemote against an httptest mock: 404 surfaces the requested-resource-not-found error.
func TestGetCommitDataSource_Read_NotFound(t *testing.T) {
	r := &GetCommitDataSource{client: newMockClientStatus(t, 404, "")}
	m := GetCommitDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "The requested resource was not found.")
}

// TestGetCommitDataSource_Read_APIError exercises GetCommitDataSource.readRemote against an httptest mock: non-success status surfaces the API error summary.
func TestGetCommitDataSource_Read_APIError(t *testing.T) {
	r := &GetCommitDataSource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := GetCommitDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error reading mycloud_get_commit")
}

// TestGetCommitDataSource_Read_APIErrorReadBody exercises GetCommitDataSource.readRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestGetCommitDataSource_Read_APIErrorReadBody(t *testing.T) {
	r := &GetCommitDataSource{client: newMockClientReadErrorBody(t, 500)}
	m := GetCommitDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}

// TestGetCommitDataSource_Read_InvalidJSON exercises GetCommitDataSource.readRemote against an httptest mock: success status with a malformed body surfaces Could not decode response body.
func TestGetCommitDataSource_Read_InvalidJSON(t *testing.T) {
	r := &GetCommitDataSource{client: newMockClientStatus(t, 200, "{{")}
	m := GetCommitDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode response body")
}
