package provider

import (
	"context"
	"testing"
)
import tfframeworkresource "github.com/hashicorp/terraform-plugin-framework/resource"
// TestWorkspaceResourceSchemaValidation verifies that the generated resource schema is valid.
func TestWorkspaceResourceSchemaValidation(t *testing.T) {
	r := &WorkspaceResource{}
	var resp tfframeworkresource.SchemaResponse
	r.Schema(context.Background(), tfframeworkresource.SchemaRequest{}, &resp)
	diags := resp.Schema.ValidateImplementation(context.Background())
	if diags.HasError() {
		t.Fatalf("schema validation failed: %s", diags)
	}
}
// TestWorkspaceResourceMetadata verifies that the generated resource reports the expected type name.
func TestWorkspaceResourceMetadata(t *testing.T) {
	r := &WorkspaceResource{}
	var resp tfframeworkresource.MetadataResponse
	r.Metadata(context.Background(), tfframeworkresource.MetadataRequest{}, &resp)
	if resp.TypeName != "mycloud_workspace" {
		t.Fatalf("TypeName = %q, want %q", resp.TypeName, "mycloud_workspace")
	}
}
