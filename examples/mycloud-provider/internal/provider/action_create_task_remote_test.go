package provider

import (
	"context"
	"testing"
)
import "github.com/hashicorp/terraform-plugin-framework/action"

// TestCreateTaskAction_Invoke_Happy exercises CreateTaskAction.invokeRemote against an httptest mock: happy path returns the success status with no errors; the response body is not decoded.
func TestCreateTaskAction_Invoke_Happy(t *testing.T) {
	r := &CreateTaskAction{client: newMockClientStatus(t, 201, "{}")}
	m := CreateTaskActionModel{}
	resp := &action.InvokeResponse{}
	r.invokeRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestCreateTaskAction_Invoke_NilClient exercises CreateTaskAction.invokeRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestCreateTaskAction_Invoke_NilClient(t *testing.T) {
	r := &CreateTaskAction{}
	m := CreateTaskActionModel{}
	resp := &action.InvokeResponse{}
	r.invokeRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestCreateTaskAction_Invoke_BuildError exercises CreateTaskAction.invokeRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestCreateTaskAction_Invoke_BuildError(t *testing.T) {
	r := &CreateTaskAction{client: newMalformedBaseURLClient(t)}
	m := CreateTaskActionModel{}
	resp := &action.InvokeResponse{}
	r.invokeRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestCreateTaskAction_Invoke_SendError exercises CreateTaskAction.invokeRemote against an httptest mock: transport error surfaces Could not send request.
func TestCreateTaskAction_Invoke_SendError(t *testing.T) {
	r := &CreateTaskAction{client: newTransportErrorClient(t)}
	m := CreateTaskActionModel{}
	resp := &action.InvokeResponse{}
	r.invokeRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestCreateTaskAction_Invoke_APIError exercises CreateTaskAction.invokeRemote against an httptest mock: non-success status surfaces the API error summary.
func TestCreateTaskAction_Invoke_APIError(t *testing.T) {
	r := &CreateTaskAction{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := CreateTaskActionModel{}
	resp := &action.InvokeResponse{}
	r.invokeRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error invoking mycloud_create_task")
}

// TestCreateTaskAction_Invoke_APIErrorReadBody exercises CreateTaskAction.invokeRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestCreateTaskAction_Invoke_APIErrorReadBody(t *testing.T) {
	r := &CreateTaskAction{client: newMockClientReadErrorBody(t, 500)}
	m := CreateTaskActionModel{}
	resp := &action.InvokeResponse{}
	r.invokeRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}
