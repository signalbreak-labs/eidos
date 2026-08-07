package provider

import (
	"context"
	"testing"
)
import tfframeworkdatasource "github.com/hashicorp/terraform-plugin-framework/datasource"
// TestGetMemberDataSourceSchemaValidation verifies that the generated data source schema is valid.
func TestGetMemberDataSourceSchemaValidation(t *testing.T) {
	d := NewGetMemberDataSource()
	var resp tfframeworkdatasource.SchemaResponse
	d.Schema(context.Background(), tfframeworkdatasource.SchemaRequest{}, &resp)
	diags := resp.Schema.ValidateImplementation(context.Background())
	if diags.HasError() {
		t.Fatalf("schema validation failed: %s", diags)
	}
}
// TestGetMemberDataSourceMetadata verifies that the generated data source reports the expected type name.
func TestGetMemberDataSourceMetadata(t *testing.T) {
	d := NewGetMemberDataSource()
	var resp tfframeworkdatasource.MetadataResponse
	d.Metadata(context.Background(), tfframeworkdatasource.MetadataRequest{}, &resp)
	if resp.TypeName != "mycloud_get_member" {
		t.Fatalf("TypeName = %q, want %q", resp.TypeName, "mycloud_get_member")
	}
}
