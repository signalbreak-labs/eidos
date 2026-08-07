package provider

import (
	"context"
	"testing"
)
import tfframeworkresource "github.com/hashicorp/terraform-plugin-framework/resource"
// TestUpdatePullRequestResourceSchemaValidation verifies that the generated resource schema is valid.
func TestUpdatePullRequestResourceSchemaValidation(t *testing.T) {
	r := &UpdatePullRequestResource{}
	var resp tfframeworkresource.SchemaResponse
	r.Schema(context.Background(), tfframeworkresource.SchemaRequest{}, &resp)
	diags := resp.Schema.ValidateImplementation(context.Background())
	if diags.HasError() {
		t.Fatalf("schema validation failed: %s", diags)
	}
}
// TestUpdatePullRequestResourceMetadata verifies that the generated resource reports the expected type name.
func TestUpdatePullRequestResourceMetadata(t *testing.T) {
	r := &UpdatePullRequestResource{}
	var resp tfframeworkresource.MetadataResponse
	r.Metadata(context.Background(), tfframeworkresource.MetadataRequest{}, &resp)
	if resp.TypeName != "mycloud_update_pull_request" {
		t.Fatalf("TypeName = %q, want %q", resp.TypeName, "mycloud_update_pull_request")
	}
}
