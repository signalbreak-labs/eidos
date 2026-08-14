package provider

import (
	"context"
	"testing"
)
import tfframeworkresource "github.com/hashicorp/terraform-plugin-framework/resource"

// TestNetworkResourceSchemaValidation verifies that the generated resource schema is valid.
func TestNetworkResourceSchemaValidation(t *testing.T) {
	r := &NetworkResource{}
	var resp tfframeworkresource.SchemaResponse
	r.Schema(context.Background(), tfframeworkresource.SchemaRequest{}, &resp)
	diags := resp.Schema.ValidateImplementation(context.Background())
	if diags.HasError() {
		t.Fatalf("schema validation failed: %s", diags)
	}
}

// TestNetworkResourceMetadata verifies that the generated resource reports the expected type name.
func TestNetworkResourceMetadata(t *testing.T) {
	r := &NetworkResource{}
	var resp tfframeworkresource.MetadataResponse
	r.Metadata(context.Background(), tfframeworkresource.MetadataRequest{}, &resp)
	if resp.TypeName != "mycloud_network" {
		t.Fatalf("TypeName = %q, want %q", resp.TypeName, "mycloud_network")
	}
}
