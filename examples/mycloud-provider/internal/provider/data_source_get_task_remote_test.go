package provider

import (
	"context"
	"testing"
)
import "github.com/hashicorp/terraform-plugin-framework/datasource"

// TestGetTaskDataSource_Read_Happy exercises GetTaskDataSource.readRemote against an httptest mock: happy path returns the success status with no errors.
func TestGetTaskDataSource_Read_Happy(t *testing.T) {
	r := &GetTaskDataSource{client: newMockClientStatus(t, 200, "{}")}
	m := GetTaskDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestGetTaskDataSource_Read_NilClient exercises GetTaskDataSource.readRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestGetTaskDataSource_Read_NilClient(t *testing.T) {
	r := &GetTaskDataSource{}
	m := GetTaskDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestGetTaskDataSource_Read_BuildError exercises GetTaskDataSource.readRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestGetTaskDataSource_Read_BuildError(t *testing.T) {
	r := &GetTaskDataSource{client: newMalformedBaseURLClient(t)}
	m := GetTaskDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestGetTaskDataSource_Read_SendError exercises GetTaskDataSource.readRemote against an httptest mock: transport error surfaces Could not send request.
func TestGetTaskDataSource_Read_SendError(t *testing.T) {
	r := &GetTaskDataSource{client: newTransportErrorClient(t)}
	m := GetTaskDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestGetTaskDataSource_Read_NotFound exercises GetTaskDataSource.readRemote against an httptest mock: 404 surfaces the requested-resource-not-found error.
func TestGetTaskDataSource_Read_NotFound(t *testing.T) {
	r := &GetTaskDataSource{client: newMockClientStatus(t, 404, "")}
	m := GetTaskDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "The requested resource was not found.")
}

// TestGetTaskDataSource_Read_APIError exercises GetTaskDataSource.readRemote against an httptest mock: non-success status surfaces the API error summary.
func TestGetTaskDataSource_Read_APIError(t *testing.T) {
	r := &GetTaskDataSource{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := GetTaskDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error reading mycloud_get_task")
}

// TestGetTaskDataSource_Read_APIErrorReadBody exercises GetTaskDataSource.readRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestGetTaskDataSource_Read_APIErrorReadBody(t *testing.T) {
	r := &GetTaskDataSource{client: newMockClientReadErrorBody(t, 500)}
	m := GetTaskDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}

// TestGetTaskDataSource_Read_InvalidJSON exercises GetTaskDataSource.readRemote against an httptest mock: success status with a malformed body surfaces Could not decode response body.
func TestGetTaskDataSource_Read_InvalidJSON(t *testing.T) {
	r := &GetTaskDataSource{client: newMockClientStatus(t, 200, "{{")}
	m := GetTaskDataSourceModel{}
	resp := &datasource.ReadResponse{}
	r.readRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not decode response body")
}
