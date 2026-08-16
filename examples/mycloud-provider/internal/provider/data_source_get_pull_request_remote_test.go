package provider

import (
	"context"
	"testing"
)
import "github.com/hashicorp/terraform-plugin-framework/datasource"

// TestGetPullRequestDataSource_Read_Happy exercises GetPullRequestDataSource.readRemote against an httptest mock: happy path returns the success status with no errors.
func TestGetPullRequestDataSource_Read_Happy(t *testing.T) {
	r := &GetPullRequestDataSource{client: newMockClientStatus(t, 200, "{}")}
	m := GetPullRequestDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestGetPullRequestDataSource_Read_NilClient exercises GetPullRequestDataSource.readRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestGetPullRequestDataSource_Read_NilClient(t *testing.T) {
	r := &GetPullRequestDataSource{}
	m := GetPullRequestDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestGetPullRequestDataSource_Read_BuildError exercises GetPullRequestDataSource.readRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestGetPullRequestDataSource_Read_BuildError(t *testing.T) {
	r := &GetPullRequestDataSource{client: newMalformedBaseURLClient(t)}
	m := GetPullRequestDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestGetPullRequestDataSource_Read_SendError exercises GetPullRequestDataSource.readRemote against an httptest mock: transport error surfaces Could not send request.
func TestGetPullRequestDataSource_Read_SendError(t *testing.T) {
	r := &GetPullRequestDataSource{client: newTransportErrorClient(t)}
	m := GetPullRequestDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestGetPullRequestDataSource_Read_NotFound exercises GetPullRequestDataSource.readRemote against an httptest mock: 404 surfaces the requested-resource-not-found error.
func TestGetPullRequestDataSource_Read_NotFound(t *testing.T) {
	r := &GetPullRequestDataSource{client: newMockClientStatus(t, 404, "")}
	m := GetPullRequestDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "The requested resource was not found.")
}

// TestGetPullRequestDataSource_Read_APIError exercises GetPullRequestDataSource.readRemote against an httptest mock: non-success status surfaces the API error summary.
func TestGetPullRequestDataSource_Read_APIError(t *testing.T) {
	r := &GetPullRequestDataSource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := GetPullRequestDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error reading mycloud_get_pull_request")
}

// TestGetPullRequestDataSource_Read_APIErrorReadBody exercises GetPullRequestDataSource.readRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestGetPullRequestDataSource_Read_APIErrorReadBody(t *testing.T) {
	r := &GetPullRequestDataSource{client: newMockClientReadErrorBody(t, 500)}
	m := GetPullRequestDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}

// TestGetPullRequestDataSource_Read_InvalidJSON exercises GetPullRequestDataSource.readRemote against an httptest mock: success status with a malformed body surfaces Could not decode response body.
func TestGetPullRequestDataSource_Read_InvalidJSON(t *testing.T) {
	r := &GetPullRequestDataSource{client: newMockClientStatus(t, 200, "{{")}
	m := GetPullRequestDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode response body")
}
