package provider

import (
	"context"
	"testing"
)
import tfframeworkdatasource "github.com/hashicorp/terraform-plugin-framework/datasource"
// TestListProjectsForOrganizationDataSourceSchemaValidation verifies that the generated data source schema is valid.
func TestListProjectsForOrganizationDataSourceSchemaValidation(t *testing.T) {
	d := NewListProjectsForOrganizationDataSource()
	var resp tfframeworkdatasource.SchemaResponse
	d.Schema(context.Background(), tfframeworkdatasource.SchemaRequest{}, &resp)
	diags := resp.Schema.ValidateImplementation(context.Background())
	if diags.HasError() {
		t.Fatalf("schema validation failed: %s", diags)
	}
}
// TestListProjectsForOrganizationDataSourceMetadata verifies that the generated data source reports the expected type name.
func TestListProjectsForOrganizationDataSourceMetadata(t *testing.T) {
	d := NewListProjectsForOrganizationDataSource()
	var resp tfframeworkdatasource.MetadataResponse
	d.Metadata(context.Background(), tfframeworkdatasource.MetadataRequest{}, &resp)
	if resp.TypeName != "mycloud_list_projects_for_organization" {
		t.Fatalf("TypeName = %q, want %q", resp.TypeName, "mycloud_list_projects_for_organization")
	}
}
