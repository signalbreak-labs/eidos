package provider

import (
	"context"
	"testing"
)
import tfframeworkresource "github.com/hashicorp/terraform-plugin-framework/resource"
// TestInstanceResourceSchemaValidation verifies that the generated resource schema is valid.
func TestInstanceResourceSchemaValidation(t *testing.T) {
	r := &InstanceResource{}
	var resp tfframeworkresource.SchemaResponse
	r.Schema(context.Background(), tfframeworkresource.SchemaRequest{}, &resp)
	diags := resp.Schema.ValidateImplementation(context.Background())
	if diags.HasError() {
		t.Fatalf("schema validation failed: %s", diags)
	}
}
// TestInstanceResourceMetadata verifies that the generated resource reports the expected type name.
func TestInstanceResourceMetadata(t *testing.T) {
	r := &InstanceResource{}
	var resp tfframeworkresource.MetadataResponse
	r.Metadata(context.Background(), tfframeworkresource.MetadataRequest{}, &resp)
	if resp.TypeName != "mycloud_instance" {
		t.Fatalf("TypeName = %q, want %q", resp.TypeName, "mycloud_instance")
	}
}
