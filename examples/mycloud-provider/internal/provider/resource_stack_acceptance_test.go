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

func testAccStackResourceConfig(serverURL string, name string) string {
	return fmt.Sprintf("provider \"mycloud\" {\n  endpoint = \"%s\"\n  bearer_token = \"example\"\n}\nresource \"mycloud_stack\" \"example\" {\n  api_version = \"%s\"\n  kind = \"example\"\n  name = \"example\"\n  spec = {\n  }\n  status = {\n  }\n  workspace = \"example\"\n}\n", serverURL, name)
}
// newStackResourceMockServer returns an httptest server that stubs the StackResource CRUD endpoints.
// The server echoes request bodies so that create/update responses reflect the values sent by the test.
func newStackResourceMockServer() *httptest.Server {
	mux := http.NewServeMux()
	state0 := make(map[string]map[string]interface{})
	var mu0 sync.Mutex
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
			body["id"] = "example-id"
			state0[id] = body
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(body)
			return
		case http.MethodGet:
			body, ok := state0[id]
			if !ok && len(state0) == 1 {
				for _, v := range state0 {
					body = v
					break
				}
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
			body["id"] = id
			state0[id] = body
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			_ = json.NewEncoder(w).Encode(body)
			return
		case http.MethodDelete:
			delete(state0, id)
			w.WriteHeader(204)
			return
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
	mux.HandleFunc("/workspaces", handler0)
	mux.HandleFunc("/workspaces/", handler0)
	return httptest.NewServer(mux)
}
// TestAccStackResourceLifecycle verifies create, update, delete, and import flows against a mock API.
func TestAccStackResourceLifecycle(t *testing.T) {
	t.Setenv("TF_ACC", "1")
	server := newStackResourceMockServer()
	defer server.Close()
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"mycloud": providerserver.NewProtocol6WithError(New())}, Steps: []resource.TestStep{resource.TestStep{Config: testAccStackResourceConfig(server.URL, "example"), Check: resource.ComposeAggregateTestCheckFunc(resource.TestCheckResourceAttrSet("mycloud_stack.example", "id"), resource.TestCheckResourceAttr("mycloud_stack.example", "api_version", "example"))}, resource.TestStep{Config: testAccStackResourceConfig(server.URL, "updated"), Check: resource.ComposeAggregateTestCheckFunc(resource.TestCheckResourceAttrSet("mycloud_stack.example", "id"), resource.TestCheckResourceAttr("mycloud_stack.example", "api_version", "updated"))}, resource.TestStep{ResourceName: "mycloud_stack.example", ImportState: true, ImportStateId: "imported-workspace:imported-name"}}})
}
