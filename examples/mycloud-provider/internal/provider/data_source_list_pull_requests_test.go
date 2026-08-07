package provider

import (
	"context"
	"testing"
)
import tfframeworkdatasource "github.com/hashicorp/terraform-plugin-framework/datasource"
// TestListPullRequestsDataSourceSchemaValidation verifies that the generated data source schema is valid.
func TestListPullRequestsDataSourceSchemaValidation(t *testing.T) {
	d := NewListPullRequestsDataSource()
	var resp tfframeworkdatasource.SchemaResponse
	d.Schema(context.Background(), tfframeworkdatasource.SchemaRequest{}, &resp)
	diags := resp.Schema.ValidateImplementation(context.Background())
	if diags.HasError() {
		t.Fatalf("schema validation failed: %s", diags)
	}
}
// TestListPullRequestsDataSourceMetadata verifies that the generated data source reports the expected type name.
func TestListPullRequestsDataSourceMetadata(t *testing.T) {
	d := NewListPullRequestsDataSource()
	var resp tfframeworkdatasource.MetadataResponse
	d.Metadata(context.Background(), tfframeworkdatasource.MetadataRequest{}, &resp)
	if resp.TypeName != "mycloud_list_pull_requests" {
		t.Fatalf("TypeName = %q, want %q", resp.TypeName, "mycloud_list_pull_requests")
	}
}
