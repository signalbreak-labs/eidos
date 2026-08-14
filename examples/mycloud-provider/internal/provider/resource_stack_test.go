package provider

import (
	"context"
	"testing"
)
import tfframeworkresource "github.com/hashicorp/terraform-plugin-framework/resource"

// TestStackResourceSchemaValidation verifies that the generated resource schema is valid.
func TestStackResourceSchemaValidation(t *testing.T) {
	r := &StackResource{}
	var resp tfframeworkresource.SchemaResponse
	r.Schema(context.Background(), tfframeworkresource.SchemaRequest{}, &resp)
	diags := resp.Schema.ValidateImplementation(context.Background())
	if diags.HasError() {
		t.Fatalf("schema validation failed: %s", diags)
	}
}

// TestStackResourceMetadata verifies that the generated resource reports the expected type name.
func TestStackResourceMetadata(t *testing.T) {
	r := &StackResource{}
	var resp tfframeworkresource.MetadataResponse
	r.Metadata(context.Background(), tfframeworkresource.MetadataRequest{}, &resp)
	if resp.TypeName != "mycloud_stack" {
		t.Fatalf("TypeName = %q, want %q", resp.TypeName, "mycloud_stack")
	}
}
