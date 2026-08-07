package provider

import (
	"context"
	"testing"
)
import tfframeworkresource "github.com/hashicorp/terraform-plugin-framework/resource"
// TestProjectResourceSchemaValidation verifies that the generated resource schema is valid.
func TestProjectResourceSchemaValidation(t *testing.T) {
	r := &ProjectResource{}
	var resp tfframeworkresource.SchemaResponse
	r.Schema(context.Background(), tfframeworkresource.SchemaRequest{}, &resp)
	diags := resp.Schema.ValidateImplementation(context.Background())
	if diags.HasError() {
		t.Fatalf("schema validation failed: %s", diags)
	}
}
// TestProjectResourceMetadata verifies that the generated resource reports the expected type name.
func TestProjectResourceMetadata(t *testing.T) {
	r := &ProjectResource{}
	var resp tfframeworkresource.MetadataResponse
	r.Metadata(context.Background(), tfframeworkresource.MetadataRequest{}, &resp)
	if resp.TypeName != "mycloud_project" {
		t.Fatalf("TypeName = %q, want %q", resp.TypeName, "mycloud_project")
	}
}
