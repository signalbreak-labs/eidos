package provider

import (
	"context"
	"testing"
)
import tfframeworkprovider "github.com/hashicorp/terraform-plugin-framework/provider"
// TestProviderSchemaValidation verifies that the generated provider schema is valid.
func TestProviderSchemaValidation(t *testing.T) {
	p := New()
	var resp tfframeworkprovider.SchemaResponse
	p.Schema(context.Background(), tfframeworkprovider.SchemaRequest{}, &resp)
	diags := resp.Schema.ValidateImplementation(context.Background())
	if diags.HasError() {
		t.Fatalf("schema validation failed: %s", diags)
	}
}
// TestProviderMetadata verifies that the generated provider reports the expected type name.
func TestProviderMetadata(t *testing.T) {
	p := New()
	var resp tfframeworkprovider.MetadataResponse
	p.Metadata(context.Background(), tfframeworkprovider.MetadataRequest{}, &resp)
	if resp.TypeName != "mycloud" {
		t.Fatalf("TypeName = %q, want %q", resp.TypeName, "mycloud")
	}
}
