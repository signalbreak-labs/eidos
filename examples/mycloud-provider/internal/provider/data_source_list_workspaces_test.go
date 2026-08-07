package provider

import (
	"context"
	"testing"
)
import tfframeworkdatasource "github.com/hashicorp/terraform-plugin-framework/datasource"
// TestListWorkspacesDataSourceSchemaValidation verifies that the generated data source schema is valid.
func TestListWorkspacesDataSourceSchemaValidation(t *testing.T) {
	d := NewListWorkspacesDataSource()
	var resp tfframeworkdatasource.SchemaResponse
	d.Schema(context.Background(), tfframeworkdatasource.SchemaRequest{}, &resp)
	diags := resp.Schema.ValidateImplementation(context.Background())
	if diags.HasError() {
		t.Fatalf("schema validation failed: %s", diags)
	}
}
// TestListWorkspacesDataSourceMetadata verifies that the generated data source reports the expected type name.
func TestListWorkspacesDataSourceMetadata(t *testing.T) {
	d := NewListWorkspacesDataSource()
	var resp tfframeworkdatasource.MetadataResponse
	d.Metadata(context.Background(), tfframeworkdatasource.MetadataRequest{}, &resp)
	if resp.TypeName != "mycloud_list_workspaces" {
		t.Fatalf("TypeName = %q, want %q", resp.TypeName, "mycloud_list_workspaces")
	}
}
