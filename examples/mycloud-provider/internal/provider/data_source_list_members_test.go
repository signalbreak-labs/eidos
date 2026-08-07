package provider

import (
	"context"
	"testing"
)
import tfframeworkdatasource "github.com/hashicorp/terraform-plugin-framework/datasource"
// TestListMembersDataSourceSchemaValidation verifies that the generated data source schema is valid.
func TestListMembersDataSourceSchemaValidation(t *testing.T) {
	d := NewListMembersDataSource()
	var resp tfframeworkdatasource.SchemaResponse
	d.Schema(context.Background(), tfframeworkdatasource.SchemaRequest{}, &resp)
	diags := resp.Schema.ValidateImplementation(context.Background())
	if diags.HasError() {
		t.Fatalf("schema validation failed: %s", diags)
	}
}
// TestListMembersDataSourceMetadata verifies that the generated data source reports the expected type name.
func TestListMembersDataSourceMetadata(t *testing.T) {
	d := NewListMembersDataSource()
	var resp tfframeworkdatasource.MetadataResponse
	d.Metadata(context.Background(), tfframeworkdatasource.MetadataRequest{}, &resp)
	if resp.TypeName != "mycloud_list_members" {
		t.Fatalf("TypeName = %q, want %q", resp.TypeName, "mycloud_list_members")
	}
}
