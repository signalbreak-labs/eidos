package provider

import (
	"context"
	"testing"
)
import tfframeworkdatasource "github.com/hashicorp/terraform-plugin-framework/datasource"
// TestGetPullRequestDataSourceSchemaValidation verifies that the generated data source schema is valid.
func TestGetPullRequestDataSourceSchemaValidation(t *testing.T) {
	d := NewGetPullRequestDataSource()
	var resp tfframeworkdatasource.SchemaResponse
	d.Schema(context.Background(), tfframeworkdatasource.SchemaRequest{}, &resp)
	diags := resp.Schema.ValidateImplementation(context.Background())
	if diags.HasError() {
		t.Fatalf("schema validation failed: %s", diags)
	}
}
// TestGetPullRequestDataSourceMetadata verifies that the generated data source reports the expected type name.
func TestGetPullRequestDataSourceMetadata(t *testing.T) {
	d := NewGetPullRequestDataSource()
	var resp tfframeworkdatasource.MetadataResponse
	d.Metadata(context.Background(), tfframeworkdatasource.MetadataRequest{}, &resp)
	if resp.TypeName != "mycloud_get_pull_request" {
		t.Fatalf("TypeName = %q, want %q", resp.TypeName, "mycloud_get_pull_request")
	}
}
