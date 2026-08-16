package provider

import (
	"context"
	"testing"
)
import "github.com/hashicorp/terraform-plugin-framework/action"

// TestCreatePullRequestAction_Invoke_Happy exercises CreatePullRequestAction.invokeRemote against an httptest mock: happy path returns the success status with no errors; the response body is not decoded.
func TestCreatePullRequestAction_Invoke_Happy(t *testing.T) {
	r := &CreatePullRequestAction{client: newMockClientStatus(t, 201, "{}")}
	m := CreatePullRequestActionModel{}
	resp := &action.InvokeResponse{}
	r.invokeRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestCreatePullRequestAction_Invoke_NilClient exercises CreatePullRequestAction.invokeRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestCreatePullRequestAction_Invoke_NilClient(t *testing.T) {
	r := &CreatePullRequestAction{}
	m := CreatePullRequestActionModel{}
	resp := &action.InvokeResponse{}
	r.invokeRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestCreatePullRequestAction_Invoke_BuildError exercises CreatePullRequestAction.invokeRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestCreatePullRequestAction_Invoke_BuildError(t *testing.T) {
	r := &CreatePullRequestAction{client: newMalformedBaseURLClient(t)}
	m := CreatePullRequestActionModel{}
	resp := &action.InvokeResponse{}
	r.invokeRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestCreatePullRequestAction_Invoke_SendError exercises CreatePullRequestAction.invokeRemote against an httptest mock: transport error surfaces Could not send request.
func TestCreatePullRequestAction_Invoke_SendError(t *testing.T) {
	r := &CreatePullRequestAction{client: newTransportErrorClient(t)}
	m := CreatePullRequestActionModel{}
	resp := &action.InvokeResponse{}
	r.invokeRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestCreatePullRequestAction_Invoke_APIError exercises CreatePullRequestAction.invokeRemote against an httptest mock: non-success status surfaces the API error summary.
func TestCreatePullRequestAction_Invoke_APIError(t *testing.T) {
	r := &CreatePullRequestAction{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := CreatePullRequestActionModel{}
	resp := &action.InvokeResponse{}
	r.invokeRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error invoking mycloud_create_pull_request")
}

// TestCreatePullRequestAction_Invoke_APIErrorReadBody exercises CreatePullRequestAction.invokeRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestCreatePullRequestAction_Invoke_APIErrorReadBody(t *testing.T) {
	r := &CreatePullRequestAction{client: newMockClientReadErrorBody(t, 500)}
	m := CreatePullRequestActionModel{}
	resp := &action.InvokeResponse{}
	r.invokeRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}
