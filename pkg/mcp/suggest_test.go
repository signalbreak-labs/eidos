package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// shipStoreSpec mirrors the SpaceTraders motivating case: a collection POST
// (purchase-ship) + instance GET (get-my-ship) with no DELETE on the instance,
// but a verb operation on a sub-path (scrap-ship, POST) that can stand in for
// the delete. CRUD inference drops the group, so it is a suggest candidate.
const shipStoreSpec = `openapi: 3.0.0
info:
  title: Ship Store
  version: 1.0.0
paths:
  /my/ships:
    post:
      operationId: purchase-ship
      responses:
        "201":
          description: created
  /my/ships/{shipSymbol}:
    get:
      operationId: get-my-ship
      responses:
        "200":
          description: ok
  /my/ships/{shipSymbol}/scrap:
    post:
      operationId: scrap-ship
      responses:
        "200":
          description: scrapped
`

// completeSpec has a DELETE on the instance, so CRUD inference keeps the group
// as a complete resource. suggest must NOT propose it.
const completeSpec = `openapi: 3.0.0
info:
  title: Complete
  version: 1.0.0
paths:
  /widgets:
    post:
      operationId: createWidget
      responses:
        "201":
          description: created
  /widgets/{id}:
    get:
      operationId: getWidget
      responses:
        "200":
          description: ok
    delete:
      operationId: deleteWidget
      responses:
        "204":
          description: deleted
`

// noNearMissSpec has create+read but no DELETE on the instance and no verb-like
// sub-path operation, so the suggestion has a scaffolded delete (delete_via_action
// false, no delete_operation).
const noNearMissSpec = `openapi: 3.0.0
info:
  title: Nodelete
  version: 1.0.0
paths:
  /gadgets:
    post:
      operationId: createGadget
      responses:
        "201":
          description: created
  /gadgets/{id}:
    get:
      operationId: getGadget
      responses:
        "200":
          description: ok
`

// findSuggestion returns the suggestion whose create_operation matches id, or nil.
func findSuggestion(s []Suggestion, id string) *Suggestion {
	for i := range s {
		if s[i].CreateOperation == id {
			return &s[i]
		}
	}
	return nil
}

// TestHandleSuggestResources_NearMissDelete asserts the SpaceTraders-style group
// is proposed with the verb sub-path operation wired as delete_operation and
// delete_via_action=true, and a ready-to-paste override.
func TestHandleSuggestResources_NearMissDelete(t *testing.T) {
	_, out, err := HandleSuggestResources(context.Background(), nil, SuggestResourcesArgs{Spec: shipStoreSpec})
	if err != nil {
		t.Fatalf("HandleSuggestResources error: %v", err)
	}
	if !out.Valid {
		t.Fatalf("expected valid result, got diagnostics: %+v", out.Diagnostics)
	}
	sug := findSuggestion(out.Suggestions, "purchase-ship")
	if sug == nil {
		t.Fatalf("expected a suggestion for purchase-ship, got %+v", out.Suggestions)
	}
	if sug.ResourceName != "ship" {
		t.Errorf("resource_name = %q, want ship", sug.ResourceName)
	}
	if sug.CollectionPath != "/my/ships" || sug.InstancePath != "/my/ships/{shipSymbol}" {
		t.Errorf("paths = %q / %q, want /my/ships / /my/ships/{shipSymbol}", sug.CollectionPath, sug.InstancePath)
	}
	if sug.ReadOperation != "get-my-ship" {
		t.Errorf("read_operation = %q, want get-my-ship", sug.ReadOperation)
	}
	if sug.DeleteOperation != "scrap-ship" {
		t.Errorf("delete_operation = %q, want scrap-ship", sug.DeleteOperation)
	}
	if !sug.DeleteViaAction {
		t.Errorf("delete_via_action = false, want true (scrap-ship is POST)")
	}
	if sug.Completeness != "create+read+delete" {
		t.Errorf("completeness = %q, want create+read+delete", sug.Completeness)
	}
	if !strings.Contains(sug.Reason, "scrap-ship") || !strings.Contains(sug.Reason, "no DELETE-method delete") {
		t.Errorf("reason = %q, want it to mention scrap-ship and the missing DELETE-method delete", sug.Reason)
	}
	// The override must be a usable resource_overrides entry.
	for _, want := range []string{
		"resource_overrides:",
		"operation: purchase-ship",
		"generate_resource: true",
		"resource_name: ship",
		"read_operation: get-my-ship",
		"delete_operation: scrap-ship",
	} {
		if !strings.Contains(sug.OverrideYAML, want) {
			t.Errorf("override_yaml missing %q:\n%s", want, sug.OverrideYAML)
		}
	}
}

// TestHandleSuggestResources_OverrideRoundTripsConsumes asserts that feeding the
// suggested override_yaml back as a config suppresses the suggestion (the
// resource is now declared and its create op is consumed).
func TestHandleSuggestResources_OverrideRoundTripsConsumes(t *testing.T) {
	_, out, err := HandleSuggestResources(context.Background(), nil, SuggestResourcesArgs{Spec: shipStoreSpec})
	if err != nil {
		t.Fatalf("HandleSuggestResources error: %v", err)
	}
	sug := findSuggestion(out.Suggestions, "purchase-ship")
	if sug == nil {
		t.Fatalf("expected a suggestion, got %+v", out.Suggestions)
	}
	cfg := "provider:\n  name: shipstore\n  version: \"1.0.0\"\n" + sug.OverrideYAML
	_, out2, err := HandleSuggestResources(context.Background(), nil, SuggestResourcesArgs{Spec: shipStoreSpec, Config: cfg})
	if err != nil {
		t.Fatalf("HandleSuggestResources error: %v", err)
	}
	if findSuggestion(out2.Suggestions, "purchase-ship") != nil {
		t.Errorf("expected purchase-ship suppressed after applying its override, got %+v", out2.Suggestions)
	}
}

// TestHandleSuggestResources_CompleteGroupNotSuggested asserts a group with a
// DELETE on the instance (inferred as a complete resource) is not proposed.
func TestHandleSuggestResources_CompleteGroupNotSuggested(t *testing.T) {
	_, out, err := HandleSuggestResources(context.Background(), nil, SuggestResourcesArgs{Spec: completeSpec})
	if err != nil {
		t.Fatalf("HandleSuggestResources error: %v", err)
	}
	if findSuggestion(out.Suggestions, "createWidget") != nil {
		t.Errorf("complete CRUD group should not be suggested, got %+v", out.Suggestions)
	}
}

// TestHandleSuggestResources_NoNearMiss asserts a create+read group with no
// delete and no verb sub-path is still proposed, but with a scaffolded delete
// (delete_via_action false, no delete_operation, completeness create+read).
func TestHandleSuggestResources_NoNearMiss(t *testing.T) {
	_, out, err := HandleSuggestResources(context.Background(), nil, SuggestResourcesArgs{Spec: noNearMissSpec})
	if err != nil {
		t.Fatalf("HandleSuggestResources error: %v", err)
	}
	if !out.Valid {
		t.Fatalf("expected valid result, got diagnostics: %+v", out.Diagnostics)
	}
	sug := findSuggestion(out.Suggestions, "createGadget")
	if sug == nil {
		t.Fatalf("expected a suggestion for createGadget, got %+v", out.Suggestions)
	}
	if sug.DeleteOperation != "" {
		t.Errorf("delete_operation = %q, want empty (no near-miss)", sug.DeleteOperation)
	}
	if sug.DeleteViaAction {
		t.Errorf("delete_via_action = true, want false (no near-miss)")
	}
	if sug.Completeness != "create+read" {
		t.Errorf("completeness = %q, want create+read", sug.Completeness)
	}
	if !strings.Contains(sug.OverrideYAML, "operation: createGadget") || strings.Contains(sug.OverrideYAML, "delete_operation") {
		t.Errorf("override_yaml should omit delete_operation:\n%s", sug.OverrideYAML)
	}
}

// TestHandleSuggestResources_EmptyAndErrorValidateAgainstOutputSchema asserts
// the empty, invalid, and error paths all produce output that validates against
// the suggest OutputSchema with non-nil required arrays.
func TestHandleSuggestResources_EmptyAndErrorValidateAgainstOutputSchema(t *testing.T) {
	cases := []struct {
		name string
		args SuggestResourcesArgs
	}{
		{"empty valid spec", SuggestResourcesArgs{Spec: emptySpec}},
		{"invalid spec", SuggestResourcesArgs{Spec: invalidSpec}},
		{"file read error", SuggestResourcesArgs{Spec: "/definitely/not/a/real/spec.yaml"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, out, err := HandleSuggestResources(context.Background(), nil, tc.args)
			if err != nil {
				t.Fatalf("HandleSuggestResources error: %v", err)
			}
			body := toolBody(t, res)
			assertOutputValidates(t, SuggestResourcesTool(), body)
			assertArrayFieldsNotNull(t, body, []string{"diagnostics", "suggestions"})
			assertStructuredOutputValidates(t, SuggestResourcesTool(), out)
		})
	}
}

// TestSuggestResourcesErrorResult asserts the error-result constructor produces
// output that validates against the suggest OutputSchema with non-nil arrays.
func TestSuggestResourcesErrorResult(t *testing.T) {
	out := suggestResourcesErrorResult(errors.New("boom"))
	if out.Valid {
		t.Errorf("expected valid=false from error result")
	}
	res, err := marshalToolResult(out)
	if err != nil {
		t.Fatalf("marshalToolResult: %v", err)
	}
	body := toolBody(t, res)
	assertOutputValidates(t, SuggestResourcesTool(), body)
	assertArrayFieldsNotNull(t, body, []string{"diagnostics", "suggestions"})
	if !strings.Contains(string(body), "boom") {
		t.Errorf("expected error detail in body, got %s", body)
	}
}

// TestRecoverHandler_SuggestPanicPath asserts recoverHandler turns a suggest
// handler panic into schema-conformant output (the suggest-specific instance of
// the generic guard).
func TestRecoverHandler_SuggestPanicPath(t *testing.T) {
	var (
		res *sdkmcp.CallToolResult
		out SuggestResourcesResult
	)
	func() {
		defer recoverHandler("eidos/suggest-resources", suggestResourcesErrorResult, &res, &out)
		panic("kaboom")
	}()
	if res == nil {
		t.Fatal("expected recoverHandler to set a non-nil result")
	}
	if out.Valid {
		t.Errorf("expected Valid=false after panic, got %+v", out)
	}
	body := toolBody(t, res)
	assertOutputValidates(t, SuggestResourcesTool(), body)
	if !strings.Contains(string(body), "panic in eidos/suggest-resources handler") {
		t.Errorf("expected panic summary in body, got %s", body)
	}
}

// TestLeadingVerb asserts the operationId-to-leading-verb extraction handles
// camelCase, snake/kebab, and dotted operation ids.
func TestLeadingVerb(t *testing.T) {
	cases := map[string]string{
		"scrapShip":     "scrap",
		"scrap-ship":    "scrap",
		"scrap_ship":    "scrap",
		"deletePet":     "delete",
		"getMyShip":     "get",
		"purchase-ship": "purchase",
		"archive.order": "archive",
	}
	for in, want := range cases {
		if got := leadingVerb(in); got != want {
			t.Errorf("leadingVerb(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestIsDeleteVerb asserts the delete-verb allowlist.
func TestIsDeleteVerb(t *testing.T) {
	for _, v := range []string{"delete", "scrap", "remove", "destroy", "purge", "cancel", "terminate", "retire", "archive"} {
		if !isDeleteVerb(v) {
			t.Errorf("isDeleteVerb(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"get", "post", "create", "update", "list", "purchase", "read"} {
		if isDeleteVerb(v) {
			t.Errorf("isDeleteVerb(%q) = true, want false", v)
		}
	}
}
