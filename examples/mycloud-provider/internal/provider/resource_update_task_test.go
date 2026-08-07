package provider

import (
	"context"
	"testing"
)
import tfframeworkresource "github.com/hashicorp/terraform-plugin-framework/resource"
// TestUpdateTaskResourceSchemaValidation verifies that the generated resource schema is valid.
func TestUpdateTaskResourceSchemaValidation(t *testing.T) {
	r := &UpdateTaskResource{}
	var resp tfframeworkresource.SchemaResponse
	r.Schema(context.Background(), tfframeworkresource.SchemaRequest{}, &resp)
	diags := resp.Schema.ValidateImplementation(context.Background())
	if diags.HasError() {
		t.Fatalf("schema validation failed: %s", diags)
	}
}
// TestUpdateTaskResourceMetadata verifies that the generated resource reports the expected type name.
func TestUpdateTaskResourceMetadata(t *testing.T) {
	r := &UpdateTaskResource{}
	var resp tfframeworkresource.MetadataResponse
	r.Metadata(context.Background(), tfframeworkresource.MetadataRequest{}, &resp)
	if resp.TypeName != "mycloud_update_task" {
		t.Fatalf("TypeName = %q, want %q", resp.TypeName, "mycloud_update_task")
	}
}
