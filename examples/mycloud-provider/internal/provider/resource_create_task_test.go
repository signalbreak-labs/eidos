package provider

import (
	"context"
	"testing"
)
import tfframeworkresource "github.com/hashicorp/terraform-plugin-framework/resource"
// TestCreateTaskResourceSchemaValidation verifies that the generated resource schema is valid.
func TestCreateTaskResourceSchemaValidation(t *testing.T) {
	r := &CreateTaskResource{}
	var resp tfframeworkresource.SchemaResponse
	r.Schema(context.Background(), tfframeworkresource.SchemaRequest{}, &resp)
	diags := resp.Schema.ValidateImplementation(context.Background())
	if diags.HasError() {
		t.Fatalf("schema validation failed: %s", diags)
	}
}
// TestCreateTaskResourceMetadata verifies that the generated resource reports the expected type name.
func TestCreateTaskResourceMetadata(t *testing.T) {
	r := &CreateTaskResource{}
	var resp tfframeworkresource.MetadataResponse
	r.Metadata(context.Background(), tfframeworkresource.MetadataRequest{}, &resp)
	if resp.TypeName != "mycloud_create_task" {
		t.Fatalf("TypeName = %q, want %q", resp.TypeName, "mycloud_create_task")
	}
}
