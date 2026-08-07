package provider

import (
	"context"
	"testing"
)
import tfframeworkdatasource "github.com/hashicorp/terraform-plugin-framework/datasource"
// TestGetBranchDataSourceSchemaValidation verifies that the generated data source schema is valid.
func TestGetBranchDataSourceSchemaValidation(t *testing.T) {
	d := NewGetBranchDataSource()
	var resp tfframeworkdatasource.SchemaResponse
	d.Schema(context.Background(), tfframeworkdatasource.SchemaRequest{}, &resp)
	diags := resp.Schema.ValidateImplementation(context.Background())
	if diags.HasError() {
		t.Fatalf("schema validation failed: %s", diags)
	}
}
// TestGetBranchDataSourceMetadata verifies that the generated data source reports the expected type name.
func TestGetBranchDataSourceMetadata(t *testing.T) {
	d := NewGetBranchDataSource()
	var resp tfframeworkdatasource.MetadataResponse
	d.Metadata(context.Background(), tfframeworkdatasource.MetadataRequest{}, &resp)
	if resp.TypeName != "mycloud_get_branch" {
		t.Fatalf("TypeName = %q, want %q", resp.TypeName, "mycloud_get_branch")
	}
}
