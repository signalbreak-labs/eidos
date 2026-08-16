package provider

import (
	"context"
	"testing"
)
import "github.com/hashicorp/terraform-plugin-framework/action"

// TestUpdateTaskAction_Invoke_Happy exercises UpdateTaskAction.invokeRemote against an httptest mock: happy path returns the success status with no errors; the response body is not decoded.
func TestUpdateTaskAction_Invoke_Happy(t *testing.T) {
	r := &UpdateTaskAction{client: newMockClientStatus(t, 200, "{}")}
	m := UpdateTaskActionModel{}
	resp := &action.InvokeResponse{}
	r.invokeRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestUpdateTaskAction_Invoke_NilClient exercises UpdateTaskAction.invokeRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestUpdateTaskAction_Invoke_NilClient(t *testing.T) {
	r := &UpdateTaskAction{}
	m := UpdateTaskActionModel{}
	resp := &action.InvokeResponse{}
	r.invokeRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestUpdateTaskAction_Invoke_BuildError exercises UpdateTaskAction.invokeRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestUpdateTaskAction_Invoke_BuildError(t *testing.T) {
	r := &UpdateTaskAction{client: newMalformedBaseURLClient(t)}
	m := UpdateTaskActionModel{}
	resp := &action.InvokeResponse{}
	r.invokeRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestUpdateTaskAction_Invoke_SendError exercises UpdateTaskAction.invokeRemote against an httptest mock: transport error surfaces Could not send request.
func TestUpdateTaskAction_Invoke_SendError(t *testing.T) {
	r := &UpdateTaskAction{client: newTransportErrorClient(t)}
	m := UpdateTaskActionModel{}
	resp := &action.InvokeResponse{}
	r.invokeRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestUpdateTaskAction_Invoke_APIError exercises UpdateTaskAction.invokeRemote against an httptest mock: non-success status surfaces the API error summary.
func TestUpdateTaskAction_Invoke_APIError(t *testing.T) {
	r := &UpdateTaskAction{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := UpdateTaskActionModel{}
	resp := &action.InvokeResponse{}
	r.invokeRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error invoking mycloud_update_task")
}

// TestUpdateTaskAction_Invoke_APIErrorReadBody exercises UpdateTaskAction.invokeRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestUpdateTaskAction_Invoke_APIErrorReadBody(t *testing.T) {
	r := &UpdateTaskAction{client: newMockClientReadErrorBody(t, 500)}
	m := UpdateTaskActionModel{}
	resp := &action.InvokeResponse{}
	r.invokeRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}
