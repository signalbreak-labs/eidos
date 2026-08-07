package provider

import (
	"context"
	"testing"
)
import tfframeworkdatasource "github.com/hashicorp/terraform-plugin-framework/datasource"
// TestGetCommitDataSourceSchemaValidation verifies that the generated data source schema is valid.
func TestGetCommitDataSourceSchemaValidation(t *testing.T) {
	d := NewGetCommitDataSource()
	var resp tfframeworkdatasource.SchemaResponse
	d.Schema(context.Background(), tfframeworkdatasource.SchemaRequest{}, &resp)
	diags := resp.Schema.ValidateImplementation(context.Background())
	if diags.HasError() {
		t.Fatalf("schema validation failed: %s", diags)
	}
}
// TestGetCommitDataSourceMetadata verifies that the generated data source reports the expected type name.
func TestGetCommitDataSourceMetadata(t *testing.T) {
	d := NewGetCommitDataSource()
	var resp tfframeworkdatasource.MetadataResponse
	d.Metadata(context.Background(), tfframeworkdatasource.MetadataRequest{}, &resp)
	if resp.TypeName != "mycloud_get_commit" {
		t.Fatalf("TypeName = %q, want %q", resp.TypeName, "mycloud_get_commit")
	}
}
