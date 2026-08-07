package provider

import (
	"context"
	"testing"
)
import tfframeworkdatasource "github.com/hashicorp/terraform-plugin-framework/datasource"
// TestGetTaskDataSourceSchemaValidation verifies that the generated data source schema is valid.
func TestGetTaskDataSourceSchemaValidation(t *testing.T) {
	d := NewGetTaskDataSource()
	var resp tfframeworkdatasource.SchemaResponse
	d.Schema(context.Background(), tfframeworkdatasource.SchemaRequest{}, &resp)
	diags := resp.Schema.ValidateImplementation(context.Background())
	if diags.HasError() {
		t.Fatalf("schema validation failed: %s", diags)
	}
}
// TestGetTaskDataSourceMetadata verifies that the generated data source reports the expected type name.
func TestGetTaskDataSourceMetadata(t *testing.T) {
	d := NewGetTaskDataSource()
	var resp tfframeworkdatasource.MetadataResponse
	d.Metadata(context.Background(), tfframeworkdatasource.MetadataRequest{}, &resp)
	if resp.TypeName != "mycloud_get_task" {
		t.Fatalf("TypeName = %q, want %q", resp.TypeName, "mycloud_get_task")
	}
}
