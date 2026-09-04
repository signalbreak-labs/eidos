package provider

import (
	json "encoding/json"
	"fmt"
	"io"
	http "net/http"
	httptest "net/http/httptest"
	"strings"
	"sync"
	"testing"
)
import (
	providerserver "github.com/hashicorp/terraform-plugin-framework/providerserver"
	tfprotov6 "github.com/hashicorp/terraform-plugin-go/tfprotov6"
	resource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func testAccWorkspaceResourceConfig(serverURL string, name string) string {
	return fmt.Sprintf("provider \"mycloud\" {\n  endpoint = \"%s\"\n  bearer_token = \"example\"\n}\nresource \"mycloud_workspace\" \"example\" {\n  api_version = \"%s\"\n  kind = \"example\"\n  labels = {\n    \"key\" = \"example\"\n  }\n  name = \"example\"\n  status = {\n    phase = \"example\"\n  }\n}\n", serverURL, name)
}

// newWorkspaceResourceMockServer returns an httptest server that stubs the WorkspaceResource CRUD endpoints.
// The server echoes request bodies so that create/update responses reflect the values sent by the test.
func newWorkspaceResourceMockServer() *httptest.Server {
	mux := http.NewServeMux()
	state0 := make(map[string]map[string]interface{})
	var mu0 sync.Mutex
	lastKey0 := ""
	handler0 := func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer example" {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		mu0.Lock()
		defer mu0.Unlock()
		id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/workspaces"), "/")
		if id == "" {
			id = "example-id"
		}
		switch r.Method {
		case http.MethodPost:
			body := make(map[string]interface{})
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err != io.EOF {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if _, ok := body["name"]; !ok {
				body["name"] = "example-id"
			}
			id = fmt.Sprintf("%v", body["name"])
			state0[id] = body
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(body)
			lastKey0 = id
			return
		case http.MethodGet:
			body, ok := state0[id]
			if !ok && lastKey0 != "" {
				body = state0[lastKey0]
			}
			if body == nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			_ = json.NewEncoder(w).Encode(body)
			return
		case http.MethodPut, http.MethodPatch:
			body := make(map[string]interface{})
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err != io.EOF {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if _, ok := body["name"]; !ok {
				body["name"] = "example-id"
			}
			id = fmt.Sprintf("%v", body["name"])
			state0[id] = body
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			_ = json.NewEncoder(w).Encode(body)
			lastKey0 = id
			return
		case http.MethodDelete:
			delete(state0, id)
			w.WriteHeader(200)
			return
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
	mux.HandleFunc("/workspaces", handler0)
	mux.HandleFunc("/workspaces/", handler0)
	return httptest.NewServer(mux)
}

// TestAccWorkspaceResourceLifecycle verifies create, update, delete, and import flows against a mock API.
func TestAccWorkspaceResourceLifecycle(t *testing.T) {
	t.Setenv("TF_ACC", "1")
	server := newWorkspaceResourceMockServer()
	defer server.Close()
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"mycloud": providerserver.NewProtocol6WithError(New())}, Steps: []resource.TestStep{resource.TestStep{Config: testAccWorkspaceResourceConfig(server.URL, "example"), Check: resource.ComposeAggregateTestCheckFunc(resource.TestCheckResourceAttrSet("mycloud_workspace.example", "name"), resource.TestCheckResourceAttr("mycloud_workspace.example", "api_version", "example"))}, resource.TestStep{Config: testAccWorkspaceResourceConfig(server.URL, "updated"), Check: resource.ComposeAggregateTestCheckFunc(resource.TestCheckResourceAttrSet("mycloud_workspace.example", "name"), resource.TestCheckResourceAttr("mycloud_workspace.example", "api_version", "updated"))}, resource.TestStep{ResourceName: "mycloud_workspace.example", ImportState: true, ImportStateId: "imported-name"}}})
}
