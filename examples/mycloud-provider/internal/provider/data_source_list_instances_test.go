package provider

import (
	"context"
	"testing"
)
import tfframeworkdatasource "github.com/hashicorp/terraform-plugin-framework/datasource"

// TestListInstancesDataSourceSchemaValidation verifies that the generated data source schema is valid.
func TestListInstancesDataSourceSchemaValidation(t *testing.T) {
	d := NewListInstancesDataSource()
	var resp tfframeworkdatasource.SchemaResponse
	d.Schema(context.Background(), tfframeworkdatasource.SchemaRequest{}, &resp)
	diags := resp.Schema.ValidateImplementation(context.Background())
	if diags.HasError() {
		t.Fatalf("schema validation failed: %s", diags)
	}
}

// TestListInstancesDataSourceMetadata verifies that the generated data source reports the expected type name.
func TestListInstancesDataSourceMetadata(t *testing.T) {
	d := NewListInstancesDataSource()
	var resp tfframeworkdatasource.MetadataResponse
	d.Metadata(context.Background(), tfframeworkdatasource.MetadataRequest{}, &resp)
	if resp.TypeName != "mycloud_list_instances" {
		t.Fatalf("TypeName = %q, want %q", resp.TypeName, "mycloud_list_instances")
	}
}
