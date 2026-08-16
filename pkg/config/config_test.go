package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestLoad_ValidConfig(t *testing.T) {
	yamlInput := `
provider:
  name: mycloud
  display_name: MyCloud Provider
  version: "0.1.0"
  description: A Terraform provider for MyCloud
  author: MyCloud Team
  contact_email: terraform@mycloud.io
  license: MPL-2.0
  repository: https://github.com/mycloud/terraform-provider-mycloud
  protocol_version: 6

servers:
  - url: "https://api.mycloud.io/v1"
    description: Production
    variables:
      region:
        default: us-east-1
        enum:
          - us-east-1
          - us-west-2
        description: API region

resource_overrides:
  - schema: Pet
    resource_name: pet
    id_attribute: petId
    import_format: petId
    timeouts:
      create: 30m
      read: 10m
      update: 30m
      delete: 10m
    force_new:
      - name
      - species
    computed_attributes:
      - createdAt
    sensitive_attributes:
      - ownerSecret
    write_only_attributes:
      - name: password
        description: The pet's secret password
        sensitive: true

  - operation: listPets
    generate_datasource: true
    datasource_name: pets
    generate_resource: false

datasource_overrides:
  - operation: getPetById
    datasource_name: pet

action_overrides:
  - operation: rebootServer
    name: reboot_server
    description: Reboots the specified server
    progress_messages: true
    modify_plan: true

ephemeral_resource_overrides:
  - operation: generateTemporaryCredentials
    name: temporary_credential
    open_mapping: POST /credentials/temporary
    close_mapping: DELETE /credentials/temporary/{credentialId}
    renew_mapping: POST /credentials/temporary/{credentialId}/renew
    result_fields:
      - name: access_key_id
        type: string
        sensitive: true

list_resource_overrides:
  - resource: pet
    operation: listPets
    config_schema:
      - name: status
        type: string
        optional: true
        description: Filter pets by status
    pagination:
      style: offset
      page_param: page
      per_page_param: limit

function_overrides:
  - operation: ipLookup
    name: lookup_ip
    arguments:
      - name: ip
        type: string
    return_type: string

logging:
  enabled: true
  file_path: ./provider.log
  capture_request_headers: true
  capture_request_body: true
  capture_response_headers: true
  capture_response_body: true
  max_body_bytes: 4096
  redact_headers:
    - Authorization
    - X-API-Key

auth:
  - scheme: apiKey
    header_name: X-API-Key
    env_var: MYCLOUD_API_KEY
  - scheme: oauth2
    flow: client_credentials
    client_id_env: MYCLOUD_CLIENT_ID
    client_secret_env: MYCLOUD_CLIENT_SECRET
    token_url: https://auth.mycloud.io/oauth2/token

naming:
  resource_prefix: ""
  datasource_prefix: ""
  resource_suffix: ""
  transform: snake_case

skip_operations:
  - DELETE /admin/pets
  - OPTIONS *

global_timeouts:
  create: 20m
  read: 10m
  update: 20m
  delete: 10m

pagination:
  style: offset
  page_param: page
  per_page_param: per_page
  total_count_header: X-Total-Count
  next_link_header: Link

polymorphism:
  strategy: split_resources
  oneOf:
    - schema: Pet
      variants:
        - schema: Cat
          resource_name: cat
          datasource_name: cat
        - schema: Dog
          resource_name: dog
          datasource_name: dog

generate_terraform_tests: true
use_put_as_create: false
`
	cfg, err := LoadBytes([]byte(yamlInput))
	if err != nil {
		t.Fatalf("LoadBytes failed: %v", err)
	}

	if cfg.Provider.Name != "mycloud" {
		t.Errorf("provider.name = %q, want mycloud", cfg.Provider.Name)
	}
	if cfg.Provider.Version != "0.1.0" {
		t.Errorf("provider.version = %q, want 0.1.0", cfg.Provider.Version)
	}
	if cfg.Provider.ProtocolVersion != 6 {
		t.Errorf("provider.protocol_version = %d, want 6", cfg.Provider.ProtocolVersion)
	}
	if len(cfg.Servers) != 1 {
		t.Fatalf("servers count = %d, want 1", len(cfg.Servers))
	}
	if cfg.Servers[0].URL != "https://api.mycloud.io/v1" {
		t.Errorf("server url = %q", cfg.Servers[0].URL)
	}
	if len(cfg.Servers[0].Variables) != 1 {
		t.Fatalf("server variables count = %d, want 1", len(cfg.Servers[0].Variables))
	}
	regionVar, ok := cfg.Servers[0].Variables["region"]
	if !ok {
		t.Fatal("missing region server variable")
	}
	if regionVar.Default != "us-east-1" || len(regionVar.Enum) != 2 {
		t.Errorf("unexpected region variable: %+v", regionVar)
	}

	if len(cfg.ResourceOverrides) != 2 {
		t.Fatalf("resource_overrides count = %d, want 2", len(cfg.ResourceOverrides))
	}
	if cfg.ResourceOverrides[0].Schema != "Pet" {
		t.Errorf("resource_overrides[0].schema = %q", cfg.ResourceOverrides[0].Schema)
	}
	if cfg.ResourceOverrides[0].Timeouts.Create.Duration() != 30*time.Minute {
		t.Errorf("create timeout = %v", cfg.ResourceOverrides[0].Timeouts.Create.Duration())
	}
	if cfg.ResourceOverrides[1].Operation != "listPets" {
		t.Errorf("resource_overrides[1].operation = %q", cfg.ResourceOverrides[1].Operation)
	}
	if cfg.ResourceOverrides[1].GenerateDatasource == nil || !*cfg.ResourceOverrides[1].GenerateDatasource {
		t.Error("expected generate_datasource = true")
	}
	if cfg.ResourceOverrides[1].GenerateResource == nil || *cfg.ResourceOverrides[1].GenerateResource {
		t.Error("expected generate_resource = false")
	}

	if len(cfg.DatasourceOverrides) != 1 || cfg.DatasourceOverrides[0].Operation != "getPetById" {
		t.Errorf("unexpected datasource_overrides: %+v", cfg.DatasourceOverrides)
	}
	if len(cfg.ActionOverrides) != 1 || !cfg.ActionOverrides[0].ProgressMessages {
		t.Errorf("unexpected action_overrides: %+v", cfg.ActionOverrides)
	}
	if len(cfg.EphemeralOverrides) != 1 || cfg.EphemeralOverrides[0].Name != "temporary_credential" {
		t.Errorf("unexpected ephemeral_overrides: %+v", cfg.EphemeralOverrides)
	}
	if len(cfg.ListResourceOverrides) != 1 || cfg.ListResourceOverrides[0].Resource != "pet" {
		t.Errorf("unexpected list_resource_overrides: %+v", cfg.ListResourceOverrides)
	}
	if len(cfg.FunctionOverrides) != 1 || cfg.FunctionOverrides[0].ReturnType != "string" {
		t.Errorf("unexpected function_overrides: %+v", cfg.FunctionOverrides)
	}

	if cfg.Logging == nil || !cfg.Logging.Enabled || cfg.Logging.FilePath != "./provider.log" {
		t.Errorf("unexpected logging: %+v", cfg.Logging)
	}
	if len(cfg.Auth) != 2 {
		t.Fatalf("auth count = %d, want 2", len(cfg.Auth))
	}
	if cfg.Auth[0].Scheme != "apiKey" || cfg.Auth[1].Flow != "client_credentials" {
		t.Errorf("unexpected auth: %+v", cfg.Auth)
	}
	if cfg.Naming == nil || cfg.Naming.Transform != "snake_case" {
		t.Errorf("unexpected naming: %+v", cfg.Naming)
	}
	if len(cfg.SkipOperations) != 2 {
		t.Errorf("unexpected skip_operations: %+v", cfg.SkipOperations)
	}
	if cfg.GlobalTimeouts == nil || cfg.GlobalTimeouts.Read.Duration() != 10*time.Minute {
		t.Errorf("unexpected global_timeouts: %+v", cfg.GlobalTimeouts)
	}
	if cfg.Pagination == nil || cfg.Pagination.Style != "offset" {
		t.Errorf("unexpected pagination: %+v", cfg.Pagination)
	}
	if cfg.Polymorphism == nil || cfg.Polymorphism.Strategy != "split_resources" || len(cfg.Polymorphism.OneOf) != 1 {
		t.Errorf("unexpected polymorphism: %+v", cfg.Polymorphism)
	}
	if cfg.GenerateTerraformTests == nil || !*cfg.GenerateTerraformTests {
		t.Error("generate_terraform_tests should be true")
	}
	if cfg.UsePutAsCreate == nil || *cfg.UsePutAsCreate {
		t.Error("use_put_as_create should parse to a non-nil false (kill-switch)")
	}
}

func TestLoad_SpecSection(t *testing.T) {
	yamlInput := `
provider:
  name: generated
  version: 0.1.0
spec:
  path: /tmp/api.yaml
  format: openapi3
`
	cfg, err := LoadBytes([]byte(yamlInput))
	if err != nil {
		t.Fatalf("LoadBytes failed: %v", err)
	}
	if cfg.Spec.Path != "/tmp/api.yaml" {
		t.Errorf("spec.path = %q, want /tmp/api.yaml", cfg.Spec.Path)
	}
	if cfg.Spec.Format != "openapi3" {
		t.Errorf("spec.format = %q, want openapi3", cfg.Spec.Format)
	}
}

func TestLoad_CursorPagination(t *testing.T) {
	yamlInput := `
provider:
  name: mycloud
  version: "0.1.0"
pagination:
  style: cursor
  cursor_field: next_cursor
`
	cfg, err := LoadBytes([]byte(yamlInput))
	if err != nil {
		t.Fatalf("LoadBytes failed: %v", err)
	}
	if cfg.Pagination == nil || cfg.Pagination.Style != "cursor" || cfg.Pagination.CursorField != "next_cursor" {
		t.Errorf("unexpected pagination: %+v", cfg.Pagination)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	got, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("round-trip LoadBytes failed: %v", err)
	}
	if got.Pagination == nil || got.Pagination.CursorField != "next_cursor" {
		t.Errorf("cursor_field round-trip mismatch: %+v", got.Pagination)
	}
}

func TestLoad_UnknownKeyRejected(t *testing.T) {
	// Strict decoding rejects unknown keys that would otherwise silently drop a
	// user's overrides (M-7). The key below is deliberately not a recognized
	// config field.
	yamlInput := `
provider:
  name: mycloud
  version: "0.1.0"
bogus_overrides:
  - schema: Pet
`
	if _, err := LoadBytes([]byte(yamlInput)); err == nil {
		t.Fatal("expected error for unknown top-level key, got nil")
	}
}

func TestLoad_File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "generator.yaml")
	content := []byte("provider:\n  name: test\n  version: 1.0.0\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Provider.Name != "test" {
		t.Errorf("provider.name = %q, want test", cfg.Provider.Name)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.yaml")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got: %v", err)
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	_, err := LoadBytes([]byte("provider: [\n"))
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestValidate_Nil(t *testing.T) {
	if err := Validate(nil); err == nil {
		t.Fatal("expected validation error for nil config")
	}
}

func TestValidate_MissingProviderName(t *testing.T) {
	cfg := Config{Provider: ProviderConfig{Version: "1.0.0"}}
	if err := Validate(&cfg); err == nil {
		t.Fatal("expected validation error for missing provider.name")
	}
}

func TestValidate_MissingProviderVersion(t *testing.T) {
	cfg := Config{Provider: ProviderConfig{Name: "test"}}
	if err := Validate(&cfg); err == nil {
		t.Fatal("expected validation error for missing provider.version")
	}
}

func TestValidate_InvalidProtocolVersion(t *testing.T) {
	cfg := Config{Provider: ProviderConfig{Name: "test", Version: "1.0.0", ProtocolVersion: 7}}
	if err := Validate(&cfg); err == nil {
		t.Fatal("expected validation error for invalid protocol version")
	}
}

func TestValidate_InvalidNamingTransform(t *testing.T) {
	cfg := Config{
		Provider: ProviderConfig{Name: "test", Version: "1.0.0"},
		Naming:   &NamingConfig{Transform: "kebab-case"},
	}
	if err := Validate(&cfg); err == nil {
		t.Fatal("expected validation error for invalid naming transform")
	}
}

func TestApplyDefaults_NamingTransform(t *testing.T) {
	cases := []struct {
		name      string
		naming    *NamingConfig
		wantValue string
	}{
		{
			name:      "empty transform defaults to snake_case",
			naming:    &NamingConfig{ResourcePrefix: "mycloud_"},
			wantValue: "snake_case",
		},
		{
			name:      "nil naming does not panic",
			naming:    nil,
			wantValue: "",
		},
		{
			name:      "pre-set transform is not overwritten",
			naming:    &NamingConfig{Transform: "camelCase"},
			wantValue: "camelCase",
		},
		{
			name:      "whitespace-only transform defaults to snake_case",
			naming:    &NamingConfig{Transform: "  "},
			wantValue: "snake_case",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{
				Provider: ProviderConfig{Name: "test", Version: "1.0.0"},
				Naming:   tc.naming,
			}
			ApplyDefaults(&cfg)
			if tc.naming == nil {
				if cfg.Naming != nil {
					t.Errorf("naming was unexpectedly created: %+v", cfg.Naming)
				}
				return
			}
			if cfg.Naming == nil {
				t.Fatalf("naming was unexpectedly nil")
			}
			if cfg.Naming.Transform != tc.wantValue {
				t.Errorf("naming.transform = %q, want %q", cfg.Naming.Transform, tc.wantValue)
			}
		})
	}
}

func TestValidate_PureNoDefaultMutation(t *testing.T) {
	cfg := Config{
		Provider: ProviderConfig{Name: "test", Version: "1.0.0"},
		Naming:   &NamingConfig{ResourcePrefix: "mycloud_"},
	}
	if err := Validate(&cfg); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if cfg.Naming.Transform != "" {
		t.Errorf("Validate mutated naming.transform to %q; want no mutation", cfg.Naming.Transform)
	}
}

func TestValidate_InvalidPaginationStyle(t *testing.T) {
	cfg := Config{
		Provider:   ProviderConfig{Name: "test", Version: "1.0.0"},
		Pagination: &PaginationConfig{Style: "page_number"},
	}
	if err := Validate(&cfg); err == nil {
		t.Fatal("expected validation error for invalid pagination style")
	}
}

func TestValidate_InvalidPolymorphismStrategy(t *testing.T) {
	cfg := Config{
		Provider:     ProviderConfig{Name: "test", Version: "1.0.0"},
		Polymorphism: &PolymorphismConfig{Strategy: "merge"},
	}
	if err := Validate(&cfg); err == nil {
		t.Fatal("expected validation error for invalid polymorphism strategy")
	}
}

func TestValidate_ResourceOverrideRequiresSchemaOrOperation(t *testing.T) {
	base := Config{Provider: ProviderConfig{Name: "test", Version: "1.0.0"}}

	cases := []struct {
		name      string
		override  ResourceOverride
		wantError bool
	}{
		{
			name:      "both empty",
			override:  ResourceOverride{ResourceName: "orphan"},
			wantError: true,
		},
		{
			name:      "schema whitespace only",
			override:  ResourceOverride{Schema: "   ", Operation: ""},
			wantError: true,
		},
		{
			name:      "operation whitespace only",
			override:  ResourceOverride{Schema: "", Operation: "   "},
			wantError: true,
		},
		{
			name:      "both set",
			override:  ResourceOverride{Schema: "Pet", Operation: "createPet"},
			wantError: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			cfg.ResourceOverrides = []ResourceOverride{tc.override}
			err := Validate(&cfg)
			if tc.wantError && err == nil {
				t.Fatal("expected validation error")
			}
			if !tc.wantError && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}

func TestValidate_ResourceOverrideBothSchemaAndOperationWarning(t *testing.T) {
	base := Config{Provider: ProviderConfig{Name: "test", Version: "1.0.0"}}
	cfg := base
	cfg.ResourceOverrides = []ResourceOverride{{
		ResourceName: "pet",
		Schema:       "Pet",
		Operation:    "createPet",
	}}

	if err := Validate(&cfg); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}

	if len(cfg.Warnings) == 0 {
		t.Fatal("expected a warning when both schema and operation are set")
	}
	want := "operation takes precedence and schema is ignored"
	if !strings.Contains(cfg.Warnings[0], want) {
		t.Fatalf("warning %q does not state that %q", cfg.Warnings[0], want)
	}
}

func TestValidate_DatasourceOverrideRequiresOperationOrName(t *testing.T) {
	base := Config{Provider: ProviderConfig{Name: "test", Version: "1.0.0"}}

	cases := []struct {
		name      string
		override  DatasourceOverride
		wantError bool
	}{
		{
			name:      "both empty",
			override:  DatasourceOverride{DatasourceName: "pet"},
			wantError: true,
		},
		{
			name:      "operation only",
			override:  DatasourceOverride{Operation: "getPetById"},
			wantError: false,
		},
		{
			name:      "name only",
			override:  DatasourceOverride{Name: "getPetById"},
			wantError: false,
		},
		{
			name:      "both set warns",
			override:  DatasourceOverride{Operation: "getPetById", Name: "getPetById"},
			wantError: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			cfg.DatasourceOverrides = []DatasourceOverride{tc.override}
			err := Validate(&cfg)
			if tc.wantError && err == nil {
				t.Fatal("expected validation error")
			}
			if !tc.wantError && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}

func TestValidate_DatasourceOverrideBothOperationAndNameWarning(t *testing.T) {
	cfg := Config{
		Provider: ProviderConfig{Name: "test", Version: "1.0.0"},
		DatasourceOverrides: []DatasourceOverride{{
			Operation:      "getPetById",
			Name:           "getPetById",
			DatasourceName: "pet",
		}},
	}

	if err := Validate(&cfg); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}

	if len(cfg.Warnings) != 1 {
		t.Fatalf("warnings count = %d, want 1", len(cfg.Warnings))
	}
	if !strings.Contains(cfg.Warnings[0], "operation takes precedence and name is ignored") {
		t.Errorf("warning = %q, want it to contain 'operation takes precedence and name is ignored'", cfg.Warnings[0])
	}
}

func TestValidate_MultipleDatasourceOverrideBothOperationAndNameWarnings(t *testing.T) {
	cases := []struct {
		operation      string
		name           string
		datasourceName string
	}{
		{operation: "getPetById", name: "getPetById", datasourceName: "pet"},
		{operation: "listPets", name: "listPets", datasourceName: "pets"},
	}

	overrides := make([]DatasourceOverride, 0, len(cases))
	for _, c := range cases {
		overrides = append(overrides, DatasourceOverride{
			Operation:      c.operation,
			Name:           c.name,
			DatasourceName: c.datasourceName,
		})
	}

	cfg := Config{
		Provider:            ProviderConfig{Name: "test", Version: "1.0.0"},
		DatasourceOverrides: overrides,
	}

	if err := Validate(&cfg); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}

	if len(cfg.Warnings) != len(overrides) {
		t.Fatalf("warnings count = %d, want %d", len(cfg.Warnings), len(overrides))
	}

	for i, warning := range cfg.Warnings {
		c := cases[i]
		if !strings.Contains(warning, "operation takes precedence and name is ignored") {
			t.Errorf("warning[%d] = %q, want it to contain 'operation takes precedence and name is ignored'", i, warning)
		}
		want := fmt.Sprintf("datasource_overrides[%d]:", i)
		if !strings.Contains(warning, want) {
			t.Errorf("warning[%d] = %q, want it to contain %q", i, warning, want)
		}
		if !strings.Contains(warning, c.operation) {
			t.Errorf("warning[%d] = %q, want it to contain operation %q", i, warning, c.operation)
		}
		if !strings.Contains(warning, c.name) {
			t.Errorf("warning[%d] = %q, want it to contain name %q", i, warning, c.name)
		}
	}
}

func TestValidate_ActionOverrideRequiresOperation(t *testing.T) {
	cfg := Config{
		Provider:        ProviderConfig{Name: "test", Version: "1.0.0"},
		ActionOverrides: []ActionOverride{{Name: "reboot"}},
	}
	if err := Validate(&cfg); err == nil {
		t.Fatal("expected validation error for action override without operation")
	}
}

func TestValidate_EphemeralOverrideRequiresOperation(t *testing.T) {
	cfg := Config{
		Provider:           ProviderConfig{Name: "test", Version: "1.0.0"},
		EphemeralOverrides: []EphemeralOverride{{Name: "temp"}},
	}
	if err := Validate(&cfg); err == nil {
		t.Fatal("expected validation error for ephemeral override without operation")
	}
}

func TestValidate_ListResourceOverrideRequiresOperationOrResource(t *testing.T) {
	base := Config{Provider: ProviderConfig{Name: "test", Version: "1.0.0"}}

	cases := []struct {
		name      string
		override  ListResourceOverride
		wantError bool
		wantWarn  bool
	}{
		{
			name:      "both empty",
			override:  ListResourceOverride{ConfigSchema: []ListConfigSchema{{Name: "status"}}},
			wantError: true,
		},
		{
			name:      "operation only",
			override:  ListResourceOverride{Operation: "listPets"},
			wantError: false,
		},
		{
			name:      "resource only",
			override:  ListResourceOverride{Resource: "pets"},
			wantError: false,
		},
		{
			name:      "both set warns",
			override:  ListResourceOverride{Operation: "listPets", Resource: "pets"},
			wantError: false,
			wantWarn:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			cfg.ListResourceOverrides = []ListResourceOverride{tc.override}
			err := Validate(&cfg)
			if tc.wantError && err == nil {
				t.Fatal("expected validation error")
			}
			if !tc.wantError && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
			if tc.wantWarn && len(cfg.Warnings) != 1 {
				t.Fatalf("warnings count = %d, want 1", len(cfg.Warnings))
			}
			if !tc.wantWarn && len(cfg.Warnings) != 0 {
				t.Fatalf("warnings count = %d, want 0", len(cfg.Warnings))
			}
			if tc.wantWarn && !strings.Contains(cfg.Warnings[0], "both operation and resource are set") {
				t.Errorf("warning = %q, want it to contain 'both operation and resource are set'", cfg.Warnings[0])
			}
		})
	}
}

func TestValidate_ListResourceOverrideBothOperationAndResourceWarning(t *testing.T) {
	cfg := Config{
		Provider: ProviderConfig{Name: "test", Version: "1.0.0"},
		ListResourceOverrides: []ListResourceOverride{{
			Operation: "listPets",
			Resource:  "pets",
		}},
	}

	if err := Validate(&cfg); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}

	if len(cfg.Warnings) != 1 {
		t.Fatalf("warnings count = %d, want 1", len(cfg.Warnings))
	}
	if !strings.Contains(cfg.Warnings[0], "both operation and resource are set") {
		t.Errorf("warning = %q, want it to contain 'both operation and resource are set'", cfg.Warnings[0])
	}
}

func TestValidate_FunctionOverrideRequiresOperation(t *testing.T) {
	cfg := Config{
		Provider:          ProviderConfig{Name: "test", Version: "1.0.0"},
		FunctionOverrides: []FunctionOverride{{Name: "lookup"}},
	}
	if err := Validate(&cfg); err == nil {
		t.Fatal("expected validation error for function override without operation")
	}
}

func TestValidate_NestedRequiredFields(t *testing.T) {
	base := Config{Provider: ProviderConfig{Name: "test", Version: "1.0.0"}}

	cases := []struct {
		name string
		cfg  Config
	}{
		{
			name: "write_only_attribute missing name",
			cfg: Config{
				ResourceOverrides: []ResourceOverride{{
					Schema:              "Pet",
					WriteOnlyAttributes: []WriteOnlyAttribute{{Description: "missing name"}},
				}},
			},
		},
		{
			name: "list config_schema missing name",
			cfg: Config{
				ListResourceOverrides: []ListResourceOverride{{
					Resource:     "pet",
					ConfigSchema: []ListConfigSchema{{Type: "string"}},
				}},
			},
		},
		{
			name: "function argument missing name",
			cfg: Config{
				FunctionOverrides: []FunctionOverride{{
					Operation: "ipLookup",
					Arguments: []FunctionArgument{{Type: "string"}},
				}},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			cfg.ResourceOverrides = tc.cfg.ResourceOverrides
			cfg.ListResourceOverrides = tc.cfg.ListResourceOverrides
			cfg.FunctionOverrides = tc.cfg.FunctionOverrides
			if err := Validate(&cfg); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestValidate_StateUpgradeConfig(t *testing.T) {
	base := Config{Provider: ProviderConfig{Name: "test", Version: "1.0.0"}}

	cases := []struct {
		name      string
		override  ResourceOverride
		wantError bool
	}{
		{
			name: "valid state upgrade with renames",
			override: ResourceOverride{
				Schema:        "Pet",
				SchemaVersion: 1,
				StateUpgrades: []StateUpgradeConfig{{From: 0, Renames: map[string]string{"old_name": "name"}}},
			},
			wantError: false,
		},
		{
			name: "negative from version",
			override: ResourceOverride{
				Schema:        "Pet",
				SchemaVersion: 1,
				StateUpgrades: []StateUpgradeConfig{{From: -1}},
			},
			wantError: true,
		},
		{
			name: "empty rename key",
			override: ResourceOverride{
				Schema:        "Pet",
				SchemaVersion: 1,
				StateUpgrades: []StateUpgradeConfig{{From: 0, Renames: map[string]string{"": "name"}}},
			},
			wantError: true,
		},
		{
			name: "empty rename value",
			override: ResourceOverride{
				Schema:        "Pet",
				SchemaVersion: 1,
				StateUpgrades: []StateUpgradeConfig{{From: 0, Renames: map[string]string{"old_name": ""}}},
			},
			wantError: true,
		},
		{
			name: "valid multi-step chain",
			override: ResourceOverride{
				Schema:        "Pet",
				SchemaVersion: 2,
				StateUpgrades: []StateUpgradeConfig{
					{From: 0, Renames: map[string]string{"old_name": "name"}},
					{From: 1, Renames: map[string]string{"legacy_tag": "tag"}},
				},
			},
			wantError: false,
		},
		{
			name: "duplicate from version",
			override: ResourceOverride{
				Schema:        "Pet",
				SchemaVersion: 1,
				StateUpgrades: []StateUpgradeConfig{
					{From: 0, Renames: map[string]string{"old_name": "name"}},
					{From: 0, Renames: map[string]string{"legacy_id": "id"}},
				},
			},
			wantError: true,
		},
		{
			name: "gap in from versions",
			override: ResourceOverride{
				Schema:        "Pet",
				SchemaVersion: 2,
				StateUpgrades: []StateUpgradeConfig{
					{From: 0},
					{From: 2},
				},
			},
			wantError: true,
		},
		{
			name: "out of order but contiguous",
			override: ResourceOverride{
				Schema:        "Pet",
				SchemaVersion: 2,
				StateUpgrades: []StateUpgradeConfig{
					{From: 1},
					{From: 0},
				},
			},
			wantError: false,
		},
		{
			name: "schema version does not match upgrade count",
			override: ResourceOverride{
				Schema:        "Pet",
				SchemaVersion: 3,
				StateUpgrades: []StateUpgradeConfig{
					{From: 0},
					{From: 1},
				},
			},
			wantError: true,
		},
		{
			name: "schema version zero with upgrades derives final version",
			override: ResourceOverride{
				Schema:        "Pet",
				SchemaVersion: 0,
				StateUpgrades: []StateUpgradeConfig{
					{From: 0, Renames: map[string]string{"old_name": "name"}},
					{From: 1, Renames: map[string]string{"legacy_tag": "tag"}},
				},
			},
			wantError: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			cfg.ResourceOverrides = []ResourceOverride{tc.override}
			err := Validate(&cfg)
			if tc.wantError && err == nil {
				t.Fatal("expected validation error")
			}
			if !tc.wantError && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}

func TestValidate_AuthConfig(t *testing.T) {
	base := Config{Provider: ProviderConfig{Name: "test", Version: "1.0.0"}}

	cases := []struct {
		name      string
		auth      []AuthConfig
		wantError bool
	}{
		{
			name:      "apiKey missing header and env",
			auth:      []AuthConfig{{Scheme: "apiKey"}},
			wantError: true,
		},
		{
			name:      "apiKey with header only",
			auth:      []AuthConfig{{Scheme: "apiKey", HeaderName: "X-API-Key"}},
			wantError: false,
		},
		{
			name:      "oauth2 missing flow",
			auth:      []AuthConfig{{Scheme: "oauth2"}},
			wantError: true,
		},
		{
			name:      "oauth2 with flow",
			auth:      []AuthConfig{{Scheme: "oauth2", Flow: "client_credentials", TokenURL: "https://auth.example.com/token", ClientIDEnv: "CLIENT_ID", ClientSecretEnv: "CLIENT_SECRET"}},
			wantError: false,
		},
		{
			name:      "oauth2 missing token_url",
			auth:      []AuthConfig{{Scheme: "oauth2", Flow: "client_credentials", ClientIDEnv: "CLIENT_ID", ClientSecretEnv: "CLIENT_SECRET"}},
			wantError: true,
		},
		{
			name:      "oauth2 client_credentials missing client_id_env",
			auth:      []AuthConfig{{Scheme: "oauth2", Flow: "client_credentials", TokenURL: "https://auth.example.com/token", ClientSecretEnv: "CLIENT_SECRET"}},
			wantError: true,
		},
		{
			name:      "oauth2 authorization_code missing client_secret_env",
			auth:      []AuthConfig{{Scheme: "oauth2", Flow: "authorization_code", TokenURL: "https://auth.example.com/token", ClientIDEnv: "CLIENT_ID"}},
			wantError: true,
		},
		{
			name:      "oauth2 implicit requires token_url only",
			auth:      []AuthConfig{{Scheme: "oauth2", Flow: "implicit", TokenURL: "https://auth.example.com/token"}},
			wantError: false,
		},
		{
			name:      "empty scheme",
			auth:      []AuthConfig{{HeaderName: "X-API-Key"}},
			wantError: true,
		},
		{
			name:      "invalid scheme",
			auth:      []AuthConfig{{Scheme: "digest"}},
			wantError: true,
		},
		{
			name:      "basic valid without extras",
			auth:      []AuthConfig{{Scheme: "basic"}},
			wantError: false,
		},
		{
			name:      "bearer valid without extras",
			auth:      []AuthConfig{{Scheme: "bearer"}},
			wantError: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			cfg.Auth = tc.auth
			err := Validate(&cfg)
			if tc.wantError && err == nil {
				t.Fatal("expected validation error")
			}
			if !tc.wantError && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}

func TestValidate_TimeoutConfig(t *testing.T) {
	base := Config{Provider: ProviderConfig{Name: "test", Version: "1.0.0"}}
	pos := Duration(1 * time.Minute)
	neg := Duration(-1 * time.Minute)
	zero := Duration(0)

	cases := []struct {
		name      string
		global    *TimeoutConfig
		override  *TimeoutConfig
		wantError bool
	}{
		{
			name:      "all positive durations",
			global:    &TimeoutConfig{Read: &pos},
			override:  &TimeoutConfig{Create: &pos},
			wantError: false,
		},
		{
			name:      "negative global timeout",
			global:    &TimeoutConfig{Read: &neg},
			wantError: true,
		},
		{
			name:      "zero resource override timeout",
			override:  &TimeoutConfig{Create: &zero},
			wantError: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			cfg.GlobalTimeouts = tc.global
			if tc.override != nil {
				cfg.ResourceOverrides = []ResourceOverride{{Schema: "Pet", Operation: "createPet", Timeouts: tc.override}}
			}
			err := Validate(&cfg)
			if tc.wantError && err == nil {
				t.Fatal("expected validation error")
			}
			if !tc.wantError && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}

func TestValidate_PolymorphismOneOf(t *testing.T) {
	base := Config{Provider: ProviderConfig{Name: "test", Version: "1.0.0"}}

	cases := []struct {
		name      string
		poly      *PolymorphismConfig
		wantError bool
	}{
		{
			name: "missing oneOf schema",
			poly: &PolymorphismConfig{
				Strategy: "split_resources",
				OneOf:    []OneOfOverride{{Variants: []Variant{{Schema: "Cat", ResourceName: "cat"}}}},
			},
			wantError: true,
		},
		{
			name: "missing variant schema",
			poly: &PolymorphismConfig{
				Strategy: "split_resources",
				OneOf: []OneOfOverride{{
					Schema:   "Pet",
					Variants: []Variant{{ResourceName: "cat"}},
				}},
			},
			wantError: true,
		},
		{
			name: "duplicate variant schema",
			poly: &PolymorphismConfig{
				Strategy: "split_resources",
				OneOf: []OneOfOverride{{
					Schema: "Pet",
					Variants: []Variant{
						{Schema: "Cat", ResourceName: "cat"},
						{Schema: "Cat", ResourceName: "kitten"},
					},
				}},
			},
			wantError: true,
		},
		{
			name: "split_resources missing resource and datasource name",
			poly: &PolymorphismConfig{
				Strategy: "split_resources",
				OneOf: []OneOfOverride{{
					Schema:   "Pet",
					Variants: []Variant{{Schema: "Cat"}},
				}},
			},
			wantError: true,
		},
		{
			name: "dynamic_union does not require resource/datasource name",
			poly: &PolymorphismConfig{
				Strategy: "dynamic_union",
				OneOf: []OneOfOverride{{
					Schema:   "Pet",
					Variants: []Variant{{Schema: "Cat"}},
				}},
			},
			wantError: false,
		},
		{
			name: "valid split_resources",
			poly: &PolymorphismConfig{
				Strategy: "split_resources",
				OneOf: []OneOfOverride{{
					Schema: "Pet",
					Variants: []Variant{
						{Schema: "Cat", ResourceName: "cat", DatasourceName: "cat"},
						{Schema: "Dog", ResourceName: "dog"},
					},
				}},
			},
			wantError: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			cfg.Polymorphism = tc.poly
			err := Validate(&cfg)
			if tc.wantError && err == nil {
				t.Fatal("expected validation error")
			}
			if !tc.wantError && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}

func TestValidate_MultipleErrors(t *testing.T) {
	cfg := Config{
		Provider: ProviderConfig{ProtocolVersion: 7},
		Naming:   &NamingConfig{Transform: "kebab-case"},
	}
	err := Validate(&cfg)
	if err == nil {
		t.Fatal("expected validation errors")
	}
	msg := err.Error()
	if !strings.Contains(msg, "provider.name is required") {
		t.Errorf("expected provider.name error in %q", msg)
	}
	if !strings.Contains(msg, "provider.version is required") {
		t.Errorf("expected provider.version error in %q", msg)
	}
	if !strings.Contains(msg, "naming.transform") {
		t.Errorf("expected naming.transform error in %q", msg)
	}
}

func TestDuration_UnmarshalYAML(t *testing.T) {
	cases := map[string]time.Duration{
		"30m": 30 * time.Minute,
		"1h":  time.Hour,
		"90s": 90 * time.Second,
	}
	for input, want := range cases {
		var d Duration
		if err := yaml.Unmarshal([]byte(input), &d); err != nil {
			t.Fatalf("unmarshal %q failed: %v", input, err)
		}
		if time.Duration(d) != want {
			t.Errorf("duration %q = %v, want %v", input, time.Duration(d), want)
		}
	}
}

func TestDuration_UnmarshalYAML_Invalid(t *testing.T) {
	cases := []string{"notaduration", "abc"}
	for _, input := range cases {
		var d Duration
		if err := yaml.Unmarshal([]byte(input), &d); err == nil {
			t.Errorf("expected error for invalid duration %q", input)
		}
	}
}

func TestDuration_MarshalYAML(t *testing.T) {
	d := Duration(30 * time.Minute)
	out, err := yaml.Marshal(d)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if string(out) != "30m\n" {
		t.Errorf("marshal output = %q, want \"30m\\n\"", string(out))
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		name  string
		input time.Duration
		want  string
	}{
		{name: "zero", input: 0, want: "0s"},
		{name: "minutes", input: 30 * time.Minute, want: "30m"},
		{name: "hours-minutes", input: time.Hour + 30*time.Minute, want: "1h30m"},
		{name: "hours-minutes-seconds", input: time.Hour + 30*time.Minute + 45*time.Second, want: "1h30m45s"},
		{name: "seconds-milliseconds", input: time.Second + 500*time.Millisecond, want: "1s500ms"},
		{name: "microseconds", input: 100 * time.Microsecond, want: "100us"},
		{name: "nanoseconds", input: 100 * time.Nanosecond, want: "100ns"},
		{name: "negative", input: -(30 * time.Minute), want: "-30m"},
		{name: "mixed-precision", input: 2*time.Hour + 3*time.Minute + 4*time.Second + 5*time.Millisecond + 6*time.Microsecond + 7*time.Nanosecond, want: "2h3m4s5ms6us7ns"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatDuration(tc.input)
			if got != tc.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tc.input, got, tc.want)
			}

			d := Duration(tc.input)
			out, err := yaml.Marshal(d)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}
			if string(out) != tc.want+"\n" {
				t.Errorf("Duration.MarshalYAML(%v) = %q, want %q", tc.input, string(out), tc.want+"\n")
			}

			var roundTrip Duration
			if err := yaml.Unmarshal(out, &roundTrip); err != nil {
				t.Fatalf("Unmarshal of marshaled duration failed: %v", err)
			}
			if time.Duration(roundTrip) != tc.input {
				t.Errorf("Duration round-trip mismatch: got %v, want %v", time.Duration(roundTrip), tc.input)
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	cfg := &Config{
		Provider: ProviderConfig{
			Name:            "mycloud",
			Version:         "0.1.0",
			ProtocolVersion: 6,
		},
		Servers: []ServerConfig{
			{URL: "https://api.mycloud.io/v1", Description: "Production"},
		},
		ResourceOverrides: []ResourceOverride{
			{
				Schema:        "Pet",
				ResourceName:  "pet",
				SchemaVersion: 1,
				StateUpgrades: []StateUpgradeConfig{
					{From: 0, Renames: map[string]string{"old_name": "name"}},
				},
				Timeouts: &TimeoutConfig{
					Create: durationPtr(Duration(30 * time.Minute)),
					Read:   durationPtr(Duration(10 * time.Minute)),
				},
			},
		},
		GlobalTimeouts: &TimeoutConfig{
			Create: durationPtr(Duration(20 * time.Minute)),
			Read:   durationPtr(Duration(10 * time.Minute)),
		},
		Pagination: &PaginationConfig{Style: "offset", PageParam: "page", PerPageParam: "per_page"},
		Polymorphism: &PolymorphismConfig{
			Strategy: "split_resources",
			OneOf: []OneOfOverride{
				{
					Schema: "Pet",
					Variants: []Variant{
						{Schema: "Cat", ResourceName: "cat", DatasourceName: "cat"},
						{Schema: "Dog", ResourceName: "dog", DatasourceName: "dog"},
					},
				},
			},
		},
		Logging:                &LoggingConfig{Enabled: true, FilePath: "./provider.log"},
		GenerateTerraformTests: boolPtr(true),
		Auth: []AuthConfig{
			{Scheme: "apiKey", HeaderName: "X-API-Key", EnvVar: "MYCLOUD_API_KEY"},
		},
		Naming: &NamingConfig{Transform: "snake_case"},
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	got, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("LoadBytes of marshaled config failed: %v", err)
	}
	if got.Provider.Name != cfg.Provider.Name || got.Provider.Version != cfg.Provider.Version {
		t.Errorf("provider round-trip mismatch: got %+v, want %+v", got.Provider, cfg.Provider)
	}
	if len(got.Servers) != 1 || got.Servers[0].URL != cfg.Servers[0].URL {
		t.Errorf("servers round-trip mismatch: got %+v", got.Servers)
	}
	if got.ResourceOverrides[0].Timeouts.Create.Duration() != 30*time.Minute {
		t.Errorf("timeout round-trip mismatch: got %v", got.ResourceOverrides[0].Timeouts.Create.Duration())
	}
	if got.ResourceOverrides[0].SchemaVersion != 1 {
		t.Errorf("schema_version round-trip mismatch: got %d, want 1", got.ResourceOverrides[0].SchemaVersion)
	}
	if len(got.ResourceOverrides[0].StateUpgrades) != 1 || got.ResourceOverrides[0].StateUpgrades[0].From != 0 {
		t.Errorf("state_upgrades round-trip mismatch: got %+v", got.ResourceOverrides[0].StateUpgrades)
	}
	if got.ResourceOverrides[0].StateUpgrades[0].Renames["old_name"] != "name" {
		t.Errorf("renames round-trip mismatch: got %v", got.ResourceOverrides[0].StateUpgrades[0].Renames)
	}
	if got.Pagination.Style != "offset" || got.Polymorphism.Strategy != "split_resources" {
		t.Errorf("round-trip mismatch: pagination=%+v polymorphism=%+v", got.Pagination, got.Polymorphism)
	}
	if got.Logging == nil || !got.Logging.Enabled {
		t.Errorf("logging round-trip mismatch: got %+v", got.Logging)
	}
	if got.GenerateTerraformTests == nil || !*got.GenerateTerraformTests {
		t.Error("generate_terraform_tests round-trip lost value")
	}
	if got.Naming == nil || got.Naming.Transform != "snake_case" {
		t.Errorf("naming round-trip mismatch: got %+v", got.Naming)
	}
	if len(got.Auth) != 1 || got.Auth[0].EnvVar != "MYCLOUD_API_KEY" {
		t.Errorf("auth round-trip mismatch: got %+v", got.Auth)
	}
}

func TestLoad_GenerationConfig(t *testing.T) {
	yamlInput := `
provider:
  name: mycloud
  version: "0.1.0"
generation:
  resources:
    include:
      - pet
      - owner
    exclude:
      - admin_*
    package: core
    packages:
      - name: pets
        include:
          - pet*
      - name: people
        include:
          - owner*
  skip_tests: true
  skip_docs: true
`
	cfg, err := LoadBytes([]byte(yamlInput))
	if err != nil {
		t.Fatalf("LoadBytes failed: %v", err)
	}

	if cfg.Generation.Resources.Package != "core" {
		t.Errorf("generation.resources.package = %q, want core", cfg.Generation.Resources.Package)
	}
	if len(cfg.Generation.Resources.Include) != 2 || cfg.Generation.Resources.Include[0] != "pet" {
		t.Errorf("unexpected generation.resources.include: %v", cfg.Generation.Resources.Include)
	}
	if len(cfg.Generation.Resources.Exclude) != 1 || cfg.Generation.Resources.Exclude[0] != "admin_*" {
		t.Errorf("unexpected generation.resources.exclude: %v", cfg.Generation.Resources.Exclude)
	}
	if len(cfg.Generation.Resources.Packages) != 2 {
		t.Fatalf("expected 2 package rules, got %d", len(cfg.Generation.Resources.Packages))
	}
	if cfg.Generation.Resources.Packages[0].Name != "pets" {
		t.Errorf("unexpected first package rule name: %q", cfg.Generation.Resources.Packages[0].Name)
	}
	if !cfg.Generation.SkipTests || !cfg.Generation.SkipDocs {
		t.Errorf("expected skip_tests and skip_docs to be true")
	}
}

func TestValidate_GenerationConfig(t *testing.T) {
	base := Config{Provider: ProviderConfig{Name: "test", Version: "1.0.0"}}

	cases := []struct {
		name      string
		cfg       Config
		wantError bool
	}{
		{
			name: "valid generation config",
			cfg: Config{
				Provider: base.Provider,
				Generation: GenerationConfig{
					Resources: ResourceGenerationConfig{
						Include:  []string{"pet"},
						Exclude:  []string{"admin_*"},
						Package:  "core",
						Packages: []PackageRuleConfig{{Name: "pets", Include: []string{"pet*"}}},
					},
				},
			},
			wantError: false,
		},
		{
			name: "empty include pattern",
			cfg: Config{
				Provider:   base.Provider,
				Generation: GenerationConfig{Resources: ResourceGenerationConfig{Include: []string{""}}},
			},
			wantError: true,
		},
		{
			name: "empty exclude pattern",
			cfg: Config{
				Provider:   base.Provider,
				Generation: GenerationConfig{Resources: ResourceGenerationConfig{Exclude: []string{"  "}}},
			},
			wantError: true,
		},
		{
			name: "invalid package name with path separator",
			cfg: Config{
				Provider:   base.Provider,
				Generation: GenerationConfig{Resources: ResourceGenerationConfig{Package: "core/pkg"}},
			},
			wantError: true,
		},
		{
			name: "invalid package name starting with digit",
			cfg: Config{
				Provider:   base.Provider,
				Generation: GenerationConfig{Resources: ResourceGenerationConfig{Package: "1core"}},
			},
			wantError: true,
		},
		{
			name: "empty package rule name",
			cfg: Config{
				Provider: base.Provider,
				Generation: GenerationConfig{
					Resources: ResourceGenerationConfig{Packages: []PackageRuleConfig{{Name: ""}}},
				},
			},
			wantError: true,
		},
		{
			name: "invalid package rule name",
			cfg: Config{
				Provider: base.Provider,
				Generation: GenerationConfig{
					Resources: ResourceGenerationConfig{Packages: []PackageRuleConfig{{Name: "bad-name"}}},
				},
			},
			wantError: true,
		},
		{
			name: "empty package rule include pattern",
			cfg: Config{
				Provider: base.Provider,
				Generation: GenerationConfig{
					Resources: ResourceGenerationConfig{Packages: []PackageRuleConfig{{Name: "pets", Include: []string{""}}}},
				},
			},
			wantError: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(&tc.cfg)
			if tc.wantError && err == nil {
				t.Fatal("expected validation error")
			}
			if !tc.wantError && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}

func durationPtr(d Duration) *Duration {
	return &d
}

func boolPtr(b bool) *bool {
	return &b
}

func TestTimeoutConfigYAMLRoundTrip(t *testing.T) {
	original := TimeoutConfig{
		Create: durationPtr(Duration(30 * time.Second)),
		Read:   durationPtr(Duration(2 * time.Minute)),
		Update: durationPtr(Duration(1 * time.Hour)),
		Delete: durationPtr(Duration(0)),
	}

	data, err := yaml.Marshal(original)
	if err != nil {
		t.Fatalf("marshal timeout config: %v", err)
	}

	var decoded TimeoutConfig
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal timeout config: %v", err)
	}

	if decoded.Create == nil || decoded.Create.Duration() != 30*time.Second {
		t.Errorf("create duration mismatch: got %v", decoded.Create)
	}
	if decoded.Read == nil || decoded.Read.Duration() != 2*time.Minute {
		t.Errorf("read duration mismatch: got %v", decoded.Read)
	}
	if decoded.Update == nil || decoded.Update.Duration() != 1*time.Hour {
		t.Errorf("update duration mismatch: got %v", decoded.Update)
	}
	if decoded.Delete == nil || decoded.Delete.Duration() != 0 {
		t.Errorf("delete duration mismatch: got %v", decoded.Delete)
	}
}

func TestTimeoutConfigJSONRoundTrip(t *testing.T) {
	original := TimeoutConfig{
		Create: durationPtr(Duration(90 * time.Second)),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal timeout config: %v", err)
	}

	var decoded TimeoutConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal timeout config: %v", err)
	}

	if decoded.Create == nil || decoded.Create.Duration() != 90*time.Second {
		t.Errorf("create duration mismatch: got %v", decoded.Create)
	}
}

func TestDurationMarshalUnmarshal(t *testing.T) {
	d := Duration(5 * time.Minute)
	b, err := d.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal duration: %v", err)
	}
	if string(b) != `"5m"` {
		t.Errorf("json marshal got %s", b)
	}

	y, err := d.MarshalYAML()
	if err != nil {
		t.Fatalf("marshal duration to yaml: %v", err)
	}
	if y != "5m" {
		t.Errorf("yaml marshal got %v", y)
	}

	var parsed Duration
	if err := parsed.UnmarshalJSON([]byte(`"5m"`)); err != nil {
		t.Fatalf("unmarshal duration: %v", err)
	}
	if parsed.Duration() != 5*time.Minute {
		t.Errorf("unmarshaled duration got %v", parsed.Duration())
	}
}

// TestLoad_UsePutAsCreateTriState verifies the use_put_as_create field's tri-state
// semantics: absent (nil) means default-on, true means on, and false is the
// kill-switch. The nil state must round-trip as absent (omitempty keeps the
// emitted config minimal), while an explicit true/false survives a marshal +
// reload round-trip.
func TestLoad_UsePutAsCreateTriState(t *testing.T) {
	t.Run("absent defaults to nil", func(t *testing.T) {
		yamlInput := `
provider:
  name: mycloud
  version: "0.1.0"
`
		cfg, err := LoadBytes([]byte(yamlInput))
		if err != nil {
			t.Fatalf("LoadBytes failed: %v", err)
		}
		if cfg.UsePutAsCreate != nil {
			t.Errorf("use_put_as_create should be nil when absent, got %v", *cfg.UsePutAsCreate)
		}
		// omitempty: a nil *bool must not be emitted on marshal.
		data, err := yaml.Marshal(cfg)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		if strings.Contains(string(data), "use_put_as_create") {
			t.Errorf("nil use_put_as_create should be omitted on marshal, got:\n%s", data)
		}
	})

	t.Run("explicit true round-trips", func(t *testing.T) {
		yamlInput := `
provider:
  name: mycloud
  version: "0.1.0"
use_put_as_create: true
`
		cfg, err := LoadBytes([]byte(yamlInput))
		if err != nil {
			t.Fatalf("LoadBytes failed: %v", err)
		}
		if cfg.UsePutAsCreate == nil || !*cfg.UsePutAsCreate {
			t.Fatalf("use_put_as_create should parse to non-nil true, got %v", cfg.UsePutAsCreate)
		}
		data, err := yaml.Marshal(cfg)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		if !strings.Contains(string(data), "use_put_as_create: true") {
			t.Errorf("explicit true should round-trip, got:\n%s", data)
		}
		got, err := LoadBytes(data)
		if err != nil {
			t.Fatalf("round-trip LoadBytes failed: %v", err)
		}
		if got.UsePutAsCreate == nil || !*got.UsePutAsCreate {
			t.Errorf("round-trip use_put_as_create should stay true, got %v", got.UsePutAsCreate)
		}
	})

	t.Run("explicit false round-trips as kill-switch", func(t *testing.T) {
		yamlInput := `
provider:
  name: mycloud
  version: "0.1.0"
use_put_as_create: false
`
		cfg, err := LoadBytes([]byte(yamlInput))
		if err != nil {
			t.Fatalf("LoadBytes failed: %v", err)
		}
		if cfg.UsePutAsCreate == nil || *cfg.UsePutAsCreate {
			t.Fatalf("use_put_as_create should parse to non-nil false, got %v", cfg.UsePutAsCreate)
		}
		data, err := yaml.Marshal(cfg)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		if !strings.Contains(string(data), "use_put_as_create: false") {
			t.Errorf("explicit false should round-trip, got:\n%s", data)
		}
		got, err := LoadBytes(data)
		if err != nil {
			t.Fatalf("round-trip LoadBytes failed: %v", err)
		}
		if got.UsePutAsCreate == nil || *got.UsePutAsCreate {
			t.Errorf("round-trip use_put_as_create should stay false, got %v", got.UsePutAsCreate)
		}
	})
}
