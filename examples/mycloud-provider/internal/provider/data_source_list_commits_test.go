package provider

import (
	"context"
	"testing"
)
import tfframeworkdatasource "github.com/hashicorp/terraform-plugin-framework/datasource"
// TestListCommitsDataSourceSchemaValidation verifies that the generated data source schema is valid.
func TestListCommitsDataSourceSchemaValidation(t *testing.T) {
	d := NewListCommitsDataSource()
	var resp tfframeworkdatasource.SchemaResponse
	d.Schema(context.Background(), tfframeworkdatasource.SchemaRequest{}, &resp)
	diags := resp.Schema.ValidateImplementation(context.Background())
	if diags.HasError() {
		t.Fatalf("schema validation failed: %s", diags)
	}
}
// TestListCommitsDataSourceMetadata verifies that the generated data source reports the expected type name.
func TestListCommitsDataSourceMetadata(t *testing.T) {
	d := NewListCommitsDataSource()
	var resp tfframeworkdatasource.MetadataResponse
	d.Metadata(context.Background(), tfframeworkdatasource.MetadataRequest{}, &resp)
	if resp.TypeName != "mycloud_list_commits" {
		t.Fatalf("TypeName = %q, want %q", resp.TypeName, "mycloud_list_commits")
	}
}
