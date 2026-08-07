package provider

import (
	"context"
	"testing"
)
import tfframeworkresource "github.com/hashicorp/terraform-plugin-framework/resource"
// TestConfigResourceSchemaValidation verifies that the generated resource schema is valid.
func TestConfigResourceSchemaValidation(t *testing.T) {
	r := &ConfigResource{}
	var resp tfframeworkresource.SchemaResponse
	r.Schema(context.Background(), tfframeworkresource.SchemaRequest{}, &resp)
	diags := resp.Schema.ValidateImplementation(context.Background())
	if diags.HasError() {
		t.Fatalf("schema validation failed: %s", diags)
	}
}
// TestConfigResourceMetadata verifies that the generated resource reports the expected type name.
func TestConfigResourceMetadata(t *testing.T) {
	r := &ConfigResource{}
	var resp tfframeworkresource.MetadataResponse
	r.Metadata(context.Background(), tfframeworkresource.MetadataRequest{}, &resp)
	if resp.TypeName != "mycloud_config" {
		t.Fatalf("TypeName = %q, want %q", resp.TypeName, "mycloud_config")
	}
}
