package provider

import (
	"context"
	"testing"
)
import tfframeworkdatasource "github.com/hashicorp/terraform-plugin-framework/datasource"

// TestListBranchesDataSourceSchemaValidation verifies that the generated data source schema is valid.
func TestListBranchesDataSourceSchemaValidation(t *testing.T) {
	d := NewListBranchesDataSource()
	var resp tfframeworkdatasource.SchemaResponse
	d.Schema(context.Background(), tfframeworkdatasource.SchemaRequest{}, &resp)
	diags := resp.Schema.ValidateImplementation(context.Background())
	if diags.HasError() {
		t.Fatalf("schema validation failed: %s", diags)
	}
}

// TestListBranchesDataSourceMetadata verifies that the generated data source reports the expected type name.
func TestListBranchesDataSourceMetadata(t *testing.T) {
	d := NewListBranchesDataSource()
	var resp tfframeworkdatasource.MetadataResponse
	d.Metadata(context.Background(), tfframeworkdatasource.MetadataRequest{}, &resp)
	if resp.TypeName != "mycloud_list_branches" {
		t.Fatalf("TypeName = %q, want %q", resp.TypeName, "mycloud_list_branches")
	}
}
