package provider

import (
	"context"
	"testing"
)
import "github.com/hashicorp/terraform-plugin-framework/action"

// TestUpdatePullRequestAction_Invoke_Happy exercises UpdatePullRequestAction.invokeRemote against an httptest mock: happy path returns the success status with no errors; the response body is not decoded.
func TestUpdatePullRequestAction_Invoke_Happy(t *testing.T) {
	r := &UpdatePullRequestAction{client: newMockClientStatus(t, 200, "{}")}
	m := UpdatePullRequestActionModel{}
	resp := &action.InvokeResponse{}
	r.invokeRemote(context.Background(), &m, resp)
	requireNoErrors(t, resp.Diagnostics)
}

// TestUpdatePullRequestAction_Invoke_NilClient exercises UpdatePullRequestAction.invokeRemote against an httptest mock: nil client surfaces the Client Not Configured diagnostic.
func TestUpdatePullRequestAction_Invoke_NilClient(t *testing.T) {
	r := &UpdatePullRequestAction{}
	m := UpdatePullRequestActionModel{}
	resp := &action.InvokeResponse{}
	r.invokeRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Client Not Configured")
}

// TestUpdatePullRequestAction_Invoke_BuildError exercises UpdatePullRequestAction.invokeRemote against an httptest mock: malformed base URL surfaces Could not build request.
func TestUpdatePullRequestAction_Invoke_BuildError(t *testing.T) {
	r := &UpdatePullRequestAction{client: newMalformedBaseURLClient(t)}
	m := UpdatePullRequestActionModel{}
	resp := &action.InvokeResponse{}
	r.invokeRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not build request")
}

// TestUpdatePullRequestAction_Invoke_SendError exercises UpdatePullRequestAction.invokeRemote against an httptest mock: transport error surfaces Could not send request.
func TestUpdatePullRequestAction_Invoke_SendError(t *testing.T) {
	r := &UpdatePullRequestAction{client: newTransportErrorClient(t)}
	m := UpdatePullRequestActionModel{}
	resp := &action.InvokeResponse{}
	r.invokeRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not send request")
}

// TestUpdatePullRequestAction_Invoke_APIError exercises UpdatePullRequestAction.invokeRemote against an httptest mock: non-success status surfaces the API error summary.
func TestUpdatePullRequestAction_Invoke_APIError(t *testing.T) {
	r := &UpdatePullRequestAction{client: newMockClientStatus(t, 500, "{\"message\":\"boom\"}")}
	m := UpdatePullRequestActionModel{}
	resp := &action.InvokeResponse{}
	r.invokeRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Error invoking mycloud_update_pull_request")
}

// TestUpdatePullRequestAction_Invoke_APIErrorReadBody exercises UpdatePullRequestAction.invokeRemote against an httptest mock: non-success status whose error body cannot be read surfaces Could not read error response.
func TestUpdatePullRequestAction_Invoke_APIErrorReadBody(t *testing.T) {
	r := &UpdatePullRequestAction{client: newMockClientReadErrorBody(t, 500)}
	m := UpdatePullRequestActionModel{}
	resp := &action.InvokeResponse{}
	r.invokeRemote(context.Background(), &m, resp)
	hasErrorContaining(t, resp.Diagnostics, "Could not read error response")
}
