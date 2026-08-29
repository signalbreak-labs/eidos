package generator

import (
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/signalbreak-labs/eidos/pkg/config"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

func irDurationPtr(d time.Duration) *time.Duration {
	return &d
}

func TestGenerateConfig_Minimal(t *testing.T) {
	providerIR := ir.ProviderIR{Name: "mycloud"}
	cfg, err := GenerateConfig(providerIR)
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}
	if cfg.Provider.Name != "mycloud" {
		t.Errorf("provider.name = %q, want mycloud", cfg.Provider.Name)
	}
	if cfg.Provider.Version != "0.1.0" {
		t.Errorf("provider.version = %q, want default 0.1.0", cfg.Provider.Version)
	}
	if cfg.Provider.ProtocolVersion != 6 {
		t.Errorf("provider.protocol_version = %d, want 6", cfg.Provider.ProtocolVersion)
	}
}

// TestGenerateConfig_GenerateDatasourceRoundTrip verifies M-17: a data source
// emitted by the resource_overrides.generate_datasource opt-in (marked with the
// resource's SourceOperation) is re-emitted as generate_datasource +
// datasource_name on the resource override and excluded from datasource_overrides,
// so a normalized generator.yaml reproduces the data source instead of silently
// dropping it (G8).
func TestGenerateConfig_GenerateDatasourceRoundTrip(t *testing.T) {
	providerIR := ir.ProviderIR{
		Name: "pet",
		Resources: []ir.ResourceIR{
			{
				Name:            "pet",
				SourceOperation: "createPet",
				CRUDMapping: ir.CRUDMappingIR{
					Create: ir.OperationMappingIR{Method: "POST", PathTemplate: "/pets", OperationID: "createPet"},
					Read:   ir.OperationMappingIR{Method: "GET", PathTemplate: "/pets/{id}", OperationID: "getPet"},
					Delete: ir.OperationMappingIR{Method: "DELETE", PathTemplate: "/pets/{id}", OperationID: "deletePet"},
				},
			},
		},
		// The data source emitted by generate_datasource carries the resource's
		// SourceOperation (createPet) as its marker, distinct from the read op id.
		DataSources: []ir.DataSourceIR{
			{Name: "pet", SourceOperation: "createPet", ReadMapping: ir.OperationMappingIR{Method: "GET", PathTemplate: "/pets/{id}", OperationID: "getPet"}},
			{Name: "pet_list", SourceOperation: "listPets", ReadMapping: ir.OperationMappingIR{Method: "GET", PathTemplate: "/pets", OperationID: "listPets"}},
		},
	}

	cfg, err := GenerateConfig(providerIR)
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}

	// The resource override carries generate_datasource + datasource_name.
	found := false
	for _, ro := range cfg.ResourceOverrides {
		if ro.Operation != "createPet" {
			continue
		}
		found = true
		if ro.GenerateDatasource == nil || !*ro.GenerateDatasource {
			t.Errorf("resource override createPet must re-emit generate_datasource: true")
		}
		if ro.DatasourceName != "pet" {
			t.Errorf("resource override createPet datasource_name = %q, want pet", ro.DatasourceName)
		}
	}
	if !found {
		t.Fatalf("no resource override for createPet in %+v", cfg.ResourceOverrides)
	}

	// The resource-derived data source must NOT appear in datasource_overrides
	// (it is represented by the resource's generate_datasource), while the
	// unrelated inferred data source still does.
	for _, do := range cfg.DatasourceOverrides {
		if do.Operation == "createPet" {
			t.Errorf("resource-derived data source leaked into datasource_overrides: %+v", do)
		}
	}
	keptList := false
	for _, do := range cfg.DatasourceOverrides {
		if do.Operation == "listPets" {
			keptList = true
		}
	}
	if !keptList {
		t.Errorf("inferred data source pet_list must remain in datasource_overrides; got %+v", cfg.DatasourceOverrides)
	}
}

func TestGenerateConfig_Full(t *testing.T) {
	providerIR := ir.ProviderIR{
		Name:        "mycloud",
		FullName:    "MyCloud",
		Version:     "1.0.0",
		Description: "Generated provider for MyCloud",
		Servers: []ir.ServerIR{
			{
				URL:         "https://api.mycloud.io/v1",
				Description: "Production",
				Variables: map[string]ir.ServerVariableIR{
					"region": {Default: "us-east-1", Enum: []string{"us-east-1", "us-west-2"}, Description: "Region"},
				},
			},
		},
		SecurityIR: ir.SecurityIR{
			Schemes: []ir.SecuritySchemeIR{
				{
					Name:      "api_key",
					Type:      ir.SecuritySchemeAPIKey,
					In:        "header",
					NameField: "X-API-Key",
				},
				{
					Name:  "oauth2",
					Type:  ir.SecuritySchemeOAuth2,
					Flows: &ir.OAuthFlowsIR{ClientCredentials: &ir.OAuthFlowIR{TokenURL: "https://auth.mycloud.io/token"}},
				},
			},
		},
		Resources: []ir.ResourceIR{
			{
				Name:            "pet",
				TypeName:        "mycloud_pet",
				Description:     "A pet resource",
				IDAttribute:     "id",
				ImportIDFormat:  "{id}",
				SourceOperation: "createPet",
				Timeouts: &ir.TimeoutConfigIR{
					Create: irDurationPtr(30 * time.Second),
					Read:   irDurationPtr(10 * time.Second),
				},
				Schema: ir.ObjectSchemaIR{
					// N-49: the AttributeIR carries the framework flags; the embedded
					// SchemaIR carries only the shape.
					Attributes: []ir.AttributeIR{
						{Name: "id", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						{Name: "name", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						// RequestInput + Computed (the GigaVUE-FM clusterId shape):
						// must NOT be re-emitted as a computed_attribute, or a
						// regenerated config would demote a spec-settable request
						// field to read-only (G39).
						{Name: "cluster_id", Optional: true, Computed: true, RequestInput: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						{Name: "secret", Sensitive: true, Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						{Name: "password", WriteOnly: true, Sensitive: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						{Name: "immutable_tag", Schema: ir.SchemaIR{Type: ir.TypeString}, PlanModifiers: []ir.PlanModifierIR{{Type: ir.PlanModifierTypeRequiresReplace}}},
					},
				},
			},
		},
		DataSources: []ir.DataSourceIR{
			{Name: "pet", TypeName: "mycloud_pet"},
			{Name: "pets", TypeName: "mycloud_pets"},
		},
		Actions: []ir.ActionIR{
			{
				Name:             "reboot_server",
				TypeName:         "mycloud_reboot_server",
				Description:      "Reboots the specified server",
				SourceOperation:  "rebootServer",
				ProgressMessages: true,
				ModifyPlan:       true,
			},
		},
		EphemeralResources: []ir.EphemeralResourceIR{
			{
				Name:            "temporary_credential",
				TypeName:        "mycloud_temporary_credential",
				Description:     "Generates short-lived credentials",
				SourceOperation: "generateTemporaryCredentials",
				OpenMapping:     ir.OperationMappingIR{Method: "POST", PathTemplate: "/credentials/temporary"},
				CloseMapping:    &ir.OperationMappingIR{Method: "DELETE", PathTemplate: "/credentials/temporary/{id}"},
				RenewMapping:    &ir.OperationMappingIR{Method: "PATCH", PathTemplate: "/credentials/temporary/{id}"},
				ResultSchema: ir.ObjectSchemaIR{
					Attributes: []ir.AttributeIR{
						// N-49: AttributeIR carries Sensitive; SchemaIR carries only the shape.
						{Name: "access_key_id", Sensitive: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
					},
				},
			},
		},
		ListResources: []ir.ListResourceIR{
			{
				Name:            "things",
				TypeName:        "mycloud_thing",
				SourceOperation: "listThings",
				PaginationStyle: "offset",
				ConfigSchema: ir.ObjectSchemaIR{
					Attributes: []ir.AttributeIR{
						{Name: "status", Schema: ir.SchemaIR{Type: ir.TypeString}, Optional: true, Description: "Filter by status"},
					},
				},
			},
		},
		Functions: []ir.FunctionIR{
			{
				Name:            "lookup_ip",
				TypeName:        "lookup_ip",
				SourceOperation: "ipLookup",
				Arguments: []ir.FunctionParamIR{
					{Name: "ip", Schema: ir.SchemaIR{Type: ir.TypeString}},
				},
				ReturnType: ir.SchemaIR{Type: ir.TypeString},
			},
		},
		ClientIR: ir.ClientIR{
			Pagination: &ir.PaginationIR{
				Style:        "offset",
				PageParam:    "page",
				PerPageParam: "per_page",
			},
			// N-68: GenerateConfig emits a logging: section only when the IR
			// carries LoggingIR (exact inverse of the transformer's
			// buildLoggingIR). Setting it here preserves the assertion below
			// that the canonical config documents the redaction defaults.
			Logging: &ir.LoggingIR{
				LogFile:       "trace.log",
				MaxBodyBytes:  8192,
				RedactHeaders: []string{"Authorization", "X-API-Key", "Cookie"},
			},
		},
	}

	cfg, err := GenerateConfig(providerIR)
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}

	if cfg.Provider.Name != "mycloud" || cfg.Provider.Version != "1.0.0" {
		t.Errorf("unexpected provider config: %+v", cfg.Provider)
	}
	if cfg.Provider.Description != "Generated provider for MyCloud" {
		t.Errorf("provider description = %q, want %q", cfg.Provider.Description, "Generated provider for MyCloud")
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].URL != "https://api.mycloud.io/v1" {
		t.Errorf("unexpected servers: %+v", cfg.Servers)
	}
	if cfg.Servers[0].Description != "Production" {
		t.Errorf("server description = %q, want %q", cfg.Servers[0].Description, "Production")
	}
	if len(cfg.Auth) != 2 {
		t.Fatalf("auth count = %d, want 2", len(cfg.Auth))
	}
	if cfg.Auth[0].Scheme != "apiKey" || cfg.Auth[0].HeaderName != "X-API-Key" {
		t.Errorf("unexpected apiKey auth: %+v", cfg.Auth[0])
	}
	if cfg.Auth[1].Scheme != "oauth2" || cfg.Auth[1].Flow != "client_credentials" {
		t.Errorf("unexpected oauth2 auth: %+v", cfg.Auth[1])
	}

	if len(cfg.ResourceOverrides) != 1 {
		t.Fatalf("resource_overrides count = %d, want 1", len(cfg.ResourceOverrides))
	}
	ro := cfg.ResourceOverrides[0]
	if ro.Schema != "pet" {
		t.Errorf("resource schema = %q, want pet", ro.Schema)
	}
	if ro.Operation != "createPet" {
		t.Errorf("resource operation = %q, want createPet", ro.Operation)
	}
	if ro.IDAttribute != "id" {
		t.Errorf("resource id_attribute = %q, want id", ro.IDAttribute)
	}
	if ro.Timeouts == nil || ro.Timeouts.Create.Duration() != 30*time.Second {
		t.Errorf("resource create timeout = %v", ro.Timeouts)
	}
	if len(ro.WriteOnlyAttributes) != 1 || ro.WriteOnlyAttributes[0].Name != "password" {
		t.Errorf("unexpected write_only_attributes: %+v", ro.WriteOnlyAttributes)
	}
	if len(ro.ComputedAttributes) != 2 {
		t.Errorf("expected 2 computed attributes (id, secret), got %d: %v", len(ro.ComputedAttributes), ro.ComputedAttributes)
	}
	if len(ro.SensitiveAttributes) != 2 {
		t.Errorf("expected 2 sensitive attributes (secret, password), got %d: %v", len(ro.SensitiveAttributes), ro.SensitiveAttributes)
	}
	if len(ro.ForceNew) != 1 || ro.ForceNew[0] != "immutable_tag" {
		t.Errorf("unexpected force_new: %+v", ro.ForceNew)
	}

	if len(cfg.DatasourceOverrides) != 2 {
		t.Errorf("unexpected datasource_overrides: %+v", cfg.DatasourceOverrides)
	}
	if len(cfg.ActionOverrides) != 1 || cfg.ActionOverrides[0].Name != "reboot_server" {
		t.Errorf("unexpected action_overrides: %+v", cfg.ActionOverrides)
	}
	if cfg.ActionOverrides[0].Description != "Reboots the specified server" {
		t.Errorf("action description = %q, want %q", cfg.ActionOverrides[0].Description, "Reboots the specified server")
	}
	if len(cfg.EphemeralOverrides) != 1 || cfg.EphemeralOverrides[0].OpenMapping != "POST /credentials/temporary" {
		t.Errorf("unexpected ephemeral_overrides: %+v", cfg.EphemeralOverrides)
	}
	if cfg.EphemeralOverrides[0].Description != "Generates short-lived credentials" {
		t.Errorf("ephemeral description = %q, want %q", cfg.EphemeralOverrides[0].Description, "Generates short-lived credentials")
	}
	if cfg.EphemeralOverrides[0].CloseMapping != "DELETE /credentials/temporary/{id}" {
		t.Errorf("ephemeral close_mapping = %q, want %q", cfg.EphemeralOverrides[0].CloseMapping, "DELETE /credentials/temporary/{id}")
	}
	if cfg.EphemeralOverrides[0].RenewMapping != "PATCH /credentials/temporary/{id}" {
		t.Errorf("ephemeral renew_mapping = %q, want %q", cfg.EphemeralOverrides[0].RenewMapping, "PATCH /credentials/temporary/{id}")
	}
	if len(cfg.ListResourceOverrides) != 1 || cfg.ListResourceOverrides[0].Pagination.Style != "offset" {
		t.Errorf("unexpected list_resource_overrides: %+v", cfg.ListResourceOverrides)
	}
	if len(cfg.FunctionOverrides) != 1 || cfg.FunctionOverrides[0].ReturnType != "string" {
		t.Errorf("unexpected function_overrides: %+v", cfg.FunctionOverrides)
	}
	if cfg.FunctionOverrides[0].Type != "lookup_ip" {
		t.Errorf("function type = %q, want %q", cfg.FunctionOverrides[0].Type, "lookup_ip")
	}
	if cfg.Pagination == nil || cfg.Pagination.Style != "offset" {
		t.Errorf("unexpected pagination: %+v", cfg.Pagination)
	}
	if cfg.GlobalTimeouts == nil || cfg.GlobalTimeouts.Read.Duration() != 10*time.Minute {
		t.Errorf("unexpected global_timeouts: %+v", cfg.GlobalTimeouts)
	}
	if cfg.GlobalTimeouts.Create.Duration() != 20*time.Minute {
		t.Errorf("global create timeout = %v, want 20m", cfg.GlobalTimeouts.Create.Duration())
	}
	if cfg.GlobalTimeouts.Update.Duration() != 20*time.Minute {
		t.Errorf("global update timeout = %v, want 20m", cfg.GlobalTimeouts.Update.Duration())
	}
	if cfg.GlobalTimeouts.Delete.Duration() != 10*time.Minute {
		t.Errorf("global delete timeout = %v, want 10m", cfg.GlobalTimeouts.Delete.Duration())
	}
	if cfg.Logging == nil {
		t.Error("expected logging config")
	}
	if !sliceContains(cfg.Logging.RedactHeaders, "Authorization") {
		t.Errorf("expected logging.redact_headers to contain Authorization, got %v", cfg.Logging.RedactHeaders)
	}
	if cfg.GenerateTerraformTests == nil || *cfg.GenerateTerraformTests {
		t.Errorf("expected generate_terraform_tests = false")
	}
}

func TestGenerateConfig_Polymorphism(t *testing.T) {
	providerIR := ir.ProviderIR{
		Name: "mycloud",
		Resources: []ir.ResourceIR{
			{
				Name:     "pet",
				TypeName: "mycloud_pet",
				Schema: ir.ObjectSchemaIR{
					Attributes: []ir.AttributeIR{
						{
							Name: "pet",
							Schema: ir.SchemaIR{
								Name: "Pet",
								Union: &ir.UnionType{
									Variants: []ir.SchemaIR{
										{Name: "Cat"},
										{Name: "Dog"},
									},
									Discriminator: &ir.DiscriminatorIR{PropertyName: "petType"},
								},
							},
						},
					},
				},
			},
		},
	}

	cfg, err := GenerateConfig(providerIR)
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}
	if cfg.Polymorphism == nil {
		t.Fatal("expected polymorphism config")
	}
	if cfg.Polymorphism.Strategy != "dynamic_union" {
		t.Errorf("polymorphism strategy = %q, want dynamic_union", cfg.Polymorphism.Strategy)
	}
	if len(cfg.Polymorphism.OneOf) != 1 {
		t.Fatalf("polymorphism oneOf count = %d, want 1", len(cfg.Polymorphism.OneOf))
	}
	if cfg.Polymorphism.OneOf[0].Schema != "Pet" {
		t.Errorf("polymorphism schema = %q, want Pet", cfg.Polymorphism.OneOf[0].Schema)
	}
	if len(cfg.Polymorphism.OneOf[0].Variants) != 2 {
		t.Errorf("polymorphism variants = %+v", cfg.Polymorphism.OneOf[0].Variants)
	}
	if cfg.Polymorphism.OneOf[0].Discriminator == nil {
		t.Fatal("expected polymorphism discriminator")
	}
	if cfg.Polymorphism.OneOf[0].Discriminator.PropertyName != "petType" {
		t.Errorf("discriminator property_name = %q, want petType", cfg.Polymorphism.OneOf[0].Discriminator.PropertyName)
	}
}

func TestMarshalConfig(t *testing.T) {
	providerIR := ir.ProviderIR{
		Name:    "mycloud",
		Version: "1.0.0",
		Servers: []ir.ServerIR{
			{URL: "https://api.mycloud.io/v1", Description: "Production"},
		},
		SecurityIR: ir.SecurityIR{
			Schemes: []ir.SecuritySchemeIR{
				{Name: "api_key", Type: ir.SecuritySchemeAPIKey, In: "header", NameField: "X-API-Key"},
			},
		},
		Resources: []ir.ResourceIR{
			{
				Name:            "pet",
				TypeName:        "mycloud_pet",
				SourceOperation: "createPet",
				Schema: ir.ObjectSchemaIR{
					Attributes: []ir.AttributeIR{
						{Name: "id", Schema: ir.SchemaIR{Type: ir.TypeString, Computed: true}},
						{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString, Required: true}},
					},
				},
			},
		},
	}

	data, err := MarshalConfig(providerIR)
	if err != nil {
		t.Fatalf("MarshalConfig failed: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		"provider:",
		"servers:",
		"auth:",
		"resource_overrides:",
		"global_timeouts:",
		// No logging section: the IR carries no LoggingIR, so the canonical
		// config must not declare one (N-68 — it would not round-trip).
		"generate_terraform_tests:",
		"name: mycloud",
		"version: 1.0.0",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("generated YAML missing %q:\n%s", want, content)
		}
	}

	var roundTrip config.Config
	if err := yaml.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("round-trip unmarshal failed: %v", err)
	}
	if roundTrip.Provider.Name != "mycloud" {
		t.Errorf("round-trip provider.name = %q", roundTrip.Provider.Name)
	}
}

func TestGenerateConfig_EmptyProviderName(t *testing.T) {
	providerIR := ir.ProviderIR{Name: ""}
	_, err := GenerateConfig(providerIR)
	if err == nil {
		t.Fatal("expected error for empty provider name")
	}
	if !strings.Contains(err.Error(), "generated config is invalid") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestConvertSecurityScheme_HTTPBasic(t *testing.T) {
	ac := convertSecurityScheme("mycloud", ir.SecuritySchemeIR{
		Name:   "basic",
		Type:   ir.SecuritySchemeHTTP,
		Scheme: "basic",
	})
	if ac.Scheme != "basic" {
		t.Errorf("scheme = %q, want basic", ac.Scheme)
	}
	if ac.EnvVar != "" {
		t.Errorf("env_var = %q, want empty", ac.EnvVar)
	}
}

func TestConvertSecurityScheme_Bearer(t *testing.T) {
	ac := convertSecurityScheme("mycloud", ir.SecuritySchemeIR{
		Name:   "bearer",
		Type:   ir.SecuritySchemeHTTP,
		Scheme: "bearer",
	})
	if ac.Scheme != "bearer" {
		t.Errorf("scheme = %q, want bearer", ac.Scheme)
	}
	if ac.EnvVar != "MYCLOUD_BEARER" {
		t.Errorf("env_var = %q, want MYCLOUD_BEARER", ac.EnvVar)
	}
}

func TestConvertSecurityScheme_UnknownHTTP(t *testing.T) {
	ac := convertSecurityScheme("mycloud", ir.SecuritySchemeIR{
		Name:   "digest",
		Type:   ir.SecuritySchemeHTTP,
		Scheme: "digest",
	})
	if ac.Scheme != "bearer" {
		t.Errorf("scheme = %q, want bearer fallback", ac.Scheme)
	}
	if ac.EnvVar != "MYCLOUD_DIGEST" {
		t.Errorf("env_var = %q, want MYCLOUD_DIGEST", ac.EnvVar)
	}
}

func TestConvertSecurityScheme_OpenIDConnect(t *testing.T) {
	ac := convertSecurityScheme("mycloud", ir.SecuritySchemeIR{
		Name:             "oidc",
		Type:             ir.SecuritySchemeOpenIDConnect,
		OpenIDConnectURL: "https://auth.mycloud.io/.well-known/openid-configuration",
	})
	if ac.Scheme != "oauth2" {
		t.Errorf("scheme = %q, want oauth2", ac.Scheme)
	}
	if ac.Flow != "openIdConnect" {
		t.Errorf("flow = %q, want openIdConnect", ac.Flow)
	}
	// OIDC discovery URLs are not token endpoints; they are routed to DiscoveryURL.
	if ac.TokenURL != "" {
		t.Errorf("token_url = %q, want empty", ac.TokenURL)
	}
	if ac.DiscoveryURL != "https://auth.mycloud.io/.well-known/openid-configuration" {
		t.Errorf("discovery_url = %q, want openid connect url", ac.DiscoveryURL)
	}
	if ac.ClientIDEnv != "MYCLOUD_CLIENT_ID" {
		t.Errorf("client_id_env = %q, want MYCLOUD_CLIENT_ID", ac.ClientIDEnv)
	}
}

func TestConvertSecurityScheme_UnknownFallback(t *testing.T) {
	ac := convertSecurityScheme("mycloud", ir.SecuritySchemeIR{
		Name: "custom",
		Type: ir.SecuritySchemeType("unknown"),
	})
	if ac.Scheme != "apiKey" {
		t.Errorf("scheme = %q, want apiKey fallback", ac.Scheme)
	}
	if ac.EnvVar != "MYCLOUD_CUSTOM" {
		t.Errorf("env_var = %q, want MYCLOUD_CUSTOM", ac.EnvVar)
	}
}

// TestConvertSecurityScheme_PreservesAuthOverrides locks in the M-5 round-trip
// fix: a user-customized env var (or flow selection) carried on the scheme by
// transformer.ApplyAuthOverrides is emitted back verbatim instead of being
// recomputed from the provider prefix, so regeneration does not revert edits.
func TestConvertSecurityScheme_PreservesAuthOverrides(t *testing.T) {
	t.Run("apiKey env_var preserved", func(t *testing.T) {
		ac := convertSecurityScheme("mycloud", ir.SecuritySchemeIR{
			Name:      "api_key",
			Type:      ir.SecuritySchemeAPIKey,
			In:        "header",
			NameField: "X-API-Key",
			EnvVar:    "MY_CUSTOM_KEY",
		})
		if ac.EnvVar != "MY_CUSTOM_KEY" {
			t.Errorf("env_var = %q, want preserved MY_CUSTOM_KEY (M-5)", ac.EnvVar)
		}
		if ac.HeaderName != "X-API-Key" {
			t.Errorf("header_name = %q, want X-API-Key", ac.HeaderName)
		}
	})

	t.Run("bearer env_var preserved", func(t *testing.T) {
		ac := convertSecurityScheme("mycloud", ir.SecuritySchemeIR{
			Name:   "bearer",
			Type:   ir.SecuritySchemeHTTP,
			Scheme: "bearer",
			EnvVar: "MY_CUSTOM_TOKEN",
		})
		if ac.EnvVar != "MY_CUSTOM_TOKEN" {
			t.Errorf("env_var = %q, want preserved MY_CUSTOM_TOKEN (M-5)", ac.EnvVar)
		}
	})

	t.Run("oauth2 flow and env hints preserved", func(t *testing.T) {
		ac := convertSecurityScheme("mycloud", ir.SecuritySchemeIR{
			Name: "oauth2",
			Type: ir.SecuritySchemeOAuth2,
			Flows: &ir.OAuthFlowsIR{
				ClientCredentials: &ir.OAuthFlowIR{TokenURL: "https://spec.example/token"},
				Password:          &ir.OAuthFlowIR{TokenURL: "https://override.example/password"},
			},
			SelectedFlow:    "password",
			ClientIDEnv:     "MY_CLIENT_ID",
			ClientSecretEnv: "MY_CLIENT_SECRET",
		})
		if ac.Flow != "password" {
			t.Errorf("flow = %q, want preserved password (M-5)", ac.Flow)
		}
		if ac.TokenURL != "https://override.example/password" {
			t.Errorf("token_url = %q, want the selected flow's token URL (M-5)", ac.TokenURL)
		}
		if ac.ClientIDEnv != "MY_CLIENT_ID" {
			t.Errorf("client_id_env = %q, want preserved MY_CLIENT_ID (M-5)", ac.ClientIDEnv)
		}
		if ac.ClientSecretEnv != "MY_CLIENT_SECRET" {
			t.Errorf("client_secret_env = %q, want preserved MY_CLIENT_SECRET (M-5)", ac.ClientSecretEnv)
		}
	})

	t.Run("oidc client_id_env preserved", func(t *testing.T) {
		ac := convertSecurityScheme("mycloud", ir.SecuritySchemeIR{
			Name:             "oidc",
			Type:             ir.SecuritySchemeOpenIDConnect,
			OpenIDConnectURL: "https://auth.mycloud.io/.well-known/openid-configuration",
			ClientIDEnv:      "MY_OIDC_CLIENT_ID",
		})
		if ac.ClientIDEnv != "MY_OIDC_CLIENT_ID" {
			t.Errorf("client_id_env = %q, want preserved MY_OIDC_CLIENT_ID (M-5)", ac.ClientIDEnv)
		}
	})
}

// TestConvertLogging covers the N-68 reverse mapping: nil IR -> nil config (a
// run without logging declares no logging section), and a LoggingIR with a log
// file inverts back to Enabled+FilePath so the emitted section round-trips.
func TestConvertLogging(t *testing.T) {
	if got := convertLogging(nil); got != nil {
		t.Fatalf("convertLogging(nil) = %+v, want nil", got)
	}

	l := &ir.LoggingIR{
		LogFile:               "trace.log",
		CaptureRequestHeaders: true,
		CaptureResponseBody:   true,
		MaxBodyBytes:          8192,
		RedactHeaders:         []string{"Authorization", "X-Api-Key"},
	}
	cfg := convertLogging(l)
	if cfg == nil {
		t.Fatal("convertLogging(non-nil) returned nil")
	}
	if !cfg.Enabled {
		t.Error("Enabled = false, want true (derived from non-empty LogFile)")
	}
	if cfg.FilePath != "trace.log" {
		t.Errorf("FilePath = %q, want trace.log", cfg.FilePath)
	}
	if !cfg.CaptureRequestHeaders || !cfg.CaptureResponseBody {
		t.Errorf("capture flags not copied: %+v", cfg)
	}
	if cfg.MaxBodyBytes != 8192 {
		t.Errorf("MaxBodyBytes = %d, want 8192", cfg.MaxBodyBytes)
	}
	if len(cfg.RedactHeaders) != 2 || cfg.RedactHeaders[0] != "Authorization" {
		t.Errorf("RedactHeaders = %v", cfg.RedactHeaders)
	}
}

// TestGenerateConfig_RoundTripLogging asserts the canonical config and the IR
// agree on logging: an IR without logging emits no logging section, and one
// with logging emits a section that round-trips back to the same LoggingIR.
func TestGenerateConfig_RoundTripLogging(t *testing.T) {
	// No logging configured -> no logging section.
	noLog := ir.ProviderIR{
		Name: "mycloud",
		ClientIR: ir.ClientIR{
			BaseURLTemplate: "https://api.mycloud.io/v1",
		},
	}
	cfg, err := GenerateConfig(noLog)
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}
	if cfg.Logging != nil {
		t.Errorf("Logging = %+v, want nil for an IR without logging", cfg.Logging)
	}

	// Logging configured -> section emits and round-trips.
	withLog := noLog
	withLog.ClientIR.Logging = &ir.LoggingIR{
		LogFile:       "trace.log",
		MaxBodyBytes:  8192,
		RedactHeaders: []string{"Cookie"},
	}
	cfg, err = GenerateConfig(withLog)
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}
	if cfg.Logging == nil {
		t.Fatal("Logging = nil, want the IR's logging section")
	}
	if cfg.Logging.FilePath != "trace.log" || !cfg.Logging.Enabled {
		t.Errorf("round-tripped logging = %+v, want FilePath trace.log + Enabled", cfg.Logging)
	}
	if cfg.Logging.MaxBodyBytes != 8192 {
		t.Errorf("MaxBodyBytes = %d, want 8192", cfg.Logging.MaxBodyBytes)
	}
}

func TestSchemaTypeString(t *testing.T) {
	cases := []struct {
		name string
		in   ir.SchemaIR
		want string
	}{
		{"string", ir.SchemaIR{Type: ir.TypeString}, "string"},
		{"number", ir.SchemaIR{Type: ir.TypeFloat}, "number"},
		{"list", ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Type: ir.TypeString}}}, "list(string)"},
		{"set", ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.Set, ElementType: ir.SchemaIR{Type: ir.TypeInt}}}, "set(integer)"},
		{"union", ir.SchemaIR{Union: &ir.UnionType{}}, "dynamic"},
		{"object", ir.SchemaIR{Attributes: []ir.AttributeIR{{Name: "id"}}}, "object"},
		{"empty_fallback", ir.SchemaIR{}, "string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := schemaTypeString(tc.in); got != tc.want {
				t.Errorf("schemaTypeString() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestConvertTimeoutConfigIR_NilFields(t *testing.T) {
	timeouts := convertTimeoutConfigIR(&ir.TimeoutConfigIR{
		Create: irDurationPtr(30 * time.Second),
		Read:   nil,
		Update: nil,
		Delete: nil,
	})
	if timeouts == nil {
		t.Fatal("expected non-nil timeout config when at least one field is set")
	}
	if timeouts.Create == nil || timeouts.Create.Duration() != 30*time.Second {
		t.Errorf("create = %v, want 30s", timeouts.Create)
	}
	if timeouts.Read != nil {
		t.Errorf("read = %v, want nil", timeouts.Read)
	}
	if timeouts.Update != nil {
		t.Errorf("update = %v, want nil", timeouts.Update)
	}
	if timeouts.Delete != nil {
		t.Errorf("delete = %v, want nil", timeouts.Delete)
	}

	// All-nil should return nil so omitempty is preserved.
	allNil := convertTimeoutConfigIR(&ir.TimeoutConfigIR{})
	if allNil != nil {
		t.Errorf("all-nil timeout config should return nil, got %+v", allNil)
	}

	// Nil IR input should return nil.
	if convertTimeoutConfigIR(nil) != nil {
		t.Error("nil IR input should return nil")
	}
}

func TestMarshalConfig_ZeroTimeoutsOmitted(t *testing.T) {
	providerIR := ir.ProviderIR{
		Name:    "mycloud",
		Version: "1.0.0",
		Resources: []ir.ResourceIR{
			{
				Name:            "pet",
				TypeName:        "mycloud_pet",
				SourceOperation: "createPet",
				Timeouts: &ir.TimeoutConfigIR{
					Create: irDurationPtr(30 * time.Second),
				},
				Schema: ir.ObjectSchemaIR{
					Attributes: []ir.AttributeIR{
						{Name: "id", Schema: ir.SchemaIR{Type: ir.TypeString, Computed: true}},
					},
				},
			},
		},
	}

	data, err := MarshalConfig(providerIR)
	if err != nil {
		t.Fatalf("MarshalConfig failed: %v", err)
	}
	content := string(data)
	for _, unwanted := range []string{
		"update: 0s",
		"delete: 0s",
		"read: 0s",
	} {
		if strings.Contains(content, unwanted) {
			t.Errorf("generated YAML contains zero timeout %q:\n%s", unwanted, content)
		}
	}
}

func TestGenerateConfig_InvalidPaginationStyle(t *testing.T) {
	providerIR := ir.ProviderIR{
		Name: "mycloud",
		ClientIR: ir.ClientIR{
			Pagination: &ir.PaginationIR{Style: "cursor_link"},
		},
	}
	_, err := GenerateConfig(providerIR)
	if err == nil {
		t.Fatal("expected error for invalid pagination style")
	}
	if !strings.Contains(err.Error(), "generated config is invalid") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGenerateConfig_InvalidPolymorphismStrategy(t *testing.T) {
	providerIR := ir.ProviderIR{
		Name: "mycloud",
		Resources: []ir.ResourceIR{
			{
				Name:     "pet",
				TypeName: "mycloud_pet",
				Schema: ir.ObjectSchemaIR{
					Attributes: []ir.AttributeIR{
						{
							Name: "pet",
							Schema: ir.SchemaIR{
								Name: "Pet",
								Union: &ir.UnionType{
									Variants: []ir.SchemaIR{
										{Name: "Cat"},
										{Name: "Dog"},
									},
									Discriminator: &ir.DiscriminatorIR{PropertyName: "petType"},
								},
							},
						},
					},
				},
			},
		},
	}

	cfg, err := GenerateConfig(providerIR)
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}
	if cfg.Polymorphism == nil {
		t.Fatal("expected polymorphism config to test invalid strategy validation")
	}
	// GenerateConfig always produces a valid strategy; exercise the validation
	// path by mutating the generated config to an unsupported value.
	cfg.Polymorphism.Strategy = "invalid_strategy"
	if err := config.Validate(cfg); err == nil {
		t.Fatal("expected config.Validate to reject invalid polymorphism strategy")
	}
}

func TestGenerateConfig_PolymorphismSkipsEmptyVariantName(t *testing.T) {
	providerIR := ir.ProviderIR{
		Name: "mycloud",
		Resources: []ir.ResourceIR{
			{
				Name:     "pet",
				TypeName: "mycloud_pet",
				Schema: ir.ObjectSchemaIR{
					Attributes: []ir.AttributeIR{
						{
							Name: "pet",
							Schema: ir.SchemaIR{
								Name: "Pet",
								Union: &ir.UnionType{
									Variants: []ir.SchemaIR{
										{Name: "Cat"},
										{Name: ""},
										{Name: "Dog"},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	cfg, err := GenerateConfig(providerIR)
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}
	if cfg.Polymorphism == nil {
		t.Fatal("expected polymorphism config")
	}
	if len(cfg.Polymorphism.OneOf[0].Variants) != 2 {
		t.Errorf("variants = %+v, want 2", cfg.Polymorphism.OneOf[0].Variants)
	}
}

func TestConvertSecurityScheme_ApiKeyQuery(t *testing.T) {
	ac := convertSecurityScheme("mycloud", ir.SecuritySchemeIR{
		Name:      "api_key",
		Type:      ir.SecuritySchemeAPIKey,
		In:        "query",
		NameField: "api_key",
	})
	if ac.Scheme != "apiKey" {
		t.Errorf("scheme = %q, want apiKey", ac.Scheme)
	}
	if ac.HeaderName != "" {
		t.Errorf("header_name = %q, want empty for query placement", ac.HeaderName)
	}
	// A query-param apiKey still sources its token from an env var; the
	// generated APIKeyAuth interceptor injects it as a query parameter per the
	// spec's `in`. EnvVar must be set so the starter config validates and the
	// practitioner knows where to supply the key.
	if ac.EnvVar != "MYCLOUD_API_KEY" {
		t.Errorf("env_var = %q, want MYCLOUD_API_KEY for query scheme", ac.EnvVar)
	}
}

func TestConvertSecurityScheme_ApiKeyCookie(t *testing.T) {
	ac := convertSecurityScheme("mycloud", ir.SecuritySchemeIR{
		Name:      "session",
		Type:      ir.SecuritySchemeAPIKey,
		In:        "cookie",
		NameField: "session_id",
	})
	if ac.Scheme != "apiKey" {
		t.Errorf("scheme = %q, want apiKey", ac.Scheme)
	}
	if ac.HeaderName != "" {
		t.Errorf("header_name = %q, want empty for cookie placement", ac.HeaderName)
	}
	// A cookie apiKey sources its token from an env var; APIKeyAuth injects it
	// as a cookie per the spec's `in`. EnvVar must be set so the starter config
	// validates (an apiKey with neither header_name nor env_var is rejected).
	if ac.EnvVar != "MYCLOUD_SESSION" {
		t.Errorf("env_var = %q, want MYCLOUD_SESSION for cookie scheme", ac.EnvVar)
	}
}

func TestConvertSecurityScheme_ApiKeyWithUnsafeName(t *testing.T) {
	ac := convertSecurityScheme("my-cloud", ir.SecuritySchemeIR{
		Name:      "api key",
		Type:      ir.SecuritySchemeAPIKey,
		In:        "header",
		NameField: "X-API-Key",
	})
	if ac.EnvVar != "MY_CLOUD_API_KEY" {
		t.Errorf("env_var = %q, want MY_CLOUD_API_KEY", ac.EnvVar)
	}
}

func TestEnvPrefixSanitizesUnsafeCharacters(t *testing.T) {
	if got := envPrefix("my.cloud-provider"); got != "MY_CLOUD_PROVIDER" {
		t.Errorf("envPrefix = %q, want MY_CLOUD_PROVIDER", got)
	}
	if got := envSuffix("auth.scheme-v2"); got != "AUTH_SCHEME_V2" {
		t.Errorf("envSuffix = %q, want AUTH_SCHEME_V2", got)
	}
	if got := envSuffix("   "); got != "UNKNOWN" {
		t.Errorf("envSuffix = %q, want UNKNOWN", got)
	}
	long := strings.Repeat("a", 100)
	want := strings.ToUpper(long[:64])
	if got := envSuffix(long); got != want {
		t.Errorf("envSuffix long = %q, want %q", got, want)
	}
}

func TestHasRequiresReplace_ExactMatch(t *testing.T) {
	cases := []struct {
		name string
		pms  []ir.PlanModifierIR
		want bool
	}{
		{
			name: "exact",
			pms:  []ir.PlanModifierIR{{Type: ir.PlanModifierTypeRequiresReplace}},
			want: true,
		},
		{
			name: "prefix",
			pms:  []ir.PlanModifierIR{{Type: "planmodifier.RequiresReplaceImmediately"}},
			want: false,
		},
		{
			name: "suffix",
			pms:  []ir.PlanModifierIR{{Type: "planmodifier.RequiresReplaceIfSet"}},
			want: false,
		},
		{
			name: "negated",
			pms:  []ir.PlanModifierIR{{Type: "planmodifier.NoRequiresReplace"}},
			want: false,
		},
		{
			// The bare spelling the transformer never emits must not match, so
			// the pre-H-15 contract bug (transformer emitting
			// "planmodifier.RequiresReplace" while the consumer matched bare
			// "RequiresReplace") cannot regress.
			name: "bare-spelling-does-not-match",
			pms:  []ir.PlanModifierIR{{Type: "RequiresReplace"}},
			want: false,
		},
		{
			name: "empty",
			pms:  nil,
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasRequiresReplace(tc.pms); got != tc.want {
				t.Errorf("hasRequiresReplace() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestConvertDatasources_Empty(t *testing.T) {
	got := convertDatasources(ir.ProviderIR{})
	if got == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
	// Verify callers can iterate over the returned slice without panic.
	for range got {
		t.Error("expected no iterations over empty slice")
	}
}

// TestConvertDatasources_UsesSourceOperation verifies that convertDatasources
// emits Operation from SourceOperation (with a Name fallback), matching the
// transformer's override resolution. Using Name directly produced generator.yaml
// files that failed to match overrides whenever Name != SourceOperation (M-12).
func TestConvertDatasources_UsesSourceOperation(t *testing.T) {
	ds := []ir.DataSourceIR{
		{Name: "pet", SourceOperation: "listPets"},
		{Name: "order", SourceOperation: ""},
	}
	got := convertDatasources(ir.ProviderIR{DataSources: ds})
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Operation != "listPets" {
		t.Errorf("got[0].Operation = %q, want %q (SourceOperation)", got[0].Operation, "listPets")
	}
	if got[1].Operation != "order" {
		t.Errorf("got[1].Operation = %q, want %q (Name fallback)", got[1].Operation, "order")
	}
}

func TestOAuth2Flow_AuthorizationCode(t *testing.T) {
	got := oauth2Flow(&ir.OAuthFlowsIR{AuthorizationCode: &ir.OAuthFlowIR{TokenURL: "https://auth.example.com/token"}})
	if got != "authorization_code" {
		t.Errorf("flow = %q, want authorization_code", got)
	}
}

func TestOAuth2Flow_Password(t *testing.T) {
	got := oauth2Flow(&ir.OAuthFlowsIR{Password: &ir.OAuthFlowIR{TokenURL: "https://auth.example.com/token"}})
	if got != "password" {
		t.Errorf("flow = %q, want password", got)
	}
}

func TestOAuth2Flow_Implicit(t *testing.T) {
	got := oauth2Flow(&ir.OAuthFlowsIR{Implicit: &ir.OAuthFlowIR{AuthorizationURL: "https://auth.example.com/auth"}})
	if got != "implicit" {
		t.Errorf("flow = %q, want implicit", got)
	}
}

func TestGenerateConfig_GenerateTerraformTestsFlag(t *testing.T) {
	providerIR := ir.ProviderIR{
		Name:                   "mycloud",
		GenerateTerraformTests: true,
	}
	cfg, err := GenerateConfig(providerIR)
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}
	if cfg.GenerateTerraformTests == nil || !*cfg.GenerateTerraformTests {
		t.Errorf("expected generate_terraform_tests = true")
	}
}

func TestGenerateConfig_PolymorphismNestedUnion(t *testing.T) {
	providerIR := ir.ProviderIR{
		Name: "mycloud",
		Resources: []ir.ResourceIR{
			{
				Name:     "pet",
				TypeName: "mycloud_pet",
				Schema: ir.ObjectSchemaIR{
					Attributes: []ir.AttributeIR{
						{
							Name: "pet",
							Schema: ir.SchemaIR{
								Name: "Pet",
								Union: &ir.UnionType{
									Variants: []ir.SchemaIR{
										{
											Name: "Mammal",
											Union: &ir.UnionType{
												Variants: []ir.SchemaIR{
													{Name: "Cat"},
													{Name: "Dog"},
												},
											},
										},
										{Name: "Fish"},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	cfg, err := GenerateConfig(providerIR)
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}
	if cfg.Polymorphism == nil {
		t.Fatal("expected polymorphism config")
	}

	wantSchemas := map[string]struct{}{
		"Pet":    {},
		"Mammal": {},
	}
	gotSchemas := make(map[string]struct{}, len(cfg.Polymorphism.OneOf))
	for _, oo := range cfg.Polymorphism.OneOf {
		gotSchemas[oo.Schema] = struct{}{}
	}
	for schema := range wantSchemas {
		if _, ok := gotSchemas[schema]; !ok {
			t.Errorf("missing expected oneOf schema %q, got %+v", schema, cfg.Polymorphism.OneOf)
		}
	}

	// Verify the nested Mammal union is detected as its own oneOf override and
	// carries the complete variant list (Cat, Dog), while the outer Pet union
	// also preserves its direct variant Fish.
	wantOneOf := map[string]map[string]struct{}{
		"Pet":    {"Mammal": {}, "Fish": {}},
		"Mammal": {"Cat": {}, "Dog": {}},
	}
	for _, oo := range cfg.Polymorphism.OneOf {
		want, ok := wantOneOf[oo.Schema]
		if !ok {
			continue
		}
		got := make(map[string]struct{}, len(oo.Variants))
		for _, v := range oo.Variants {
			got[v.Schema] = struct{}{}
		}
		for v := range want {
			if _, ok := got[v]; !ok {
				t.Errorf("oneOf %q missing expected variant %q, got variants %+v", oo.Schema, v, got)
			}
		}
		for v := range got {
			if _, ok := want[v]; !ok {
				t.Errorf("oneOf %q unexpected variant %q", oo.Schema, v)
			}
		}
	}
}

func walkSchema(schema ir.SchemaIR, fn func(ir.AttributeIR)) {
	walkSchemaNode(schema, nil, fn)
}

func TestWalkSchema_VisitsAllAttributes(t *testing.T) {
	schema := ir.SchemaIR{
		Attributes: []ir.AttributeIR{
			{Name: "id", Schema: ir.SchemaIR{Type: ir.TypeString}},
			{
				Name: "nested",
				Schema: ir.SchemaIR{
					Attributes: []ir.AttributeIR{
						{Name: "email", Schema: ir.SchemaIR{Type: ir.TypeString}},
					},
				},
			},
			{
				Name: "tags",
				Schema: ir.SchemaIR{
					Collection: &ir.CollectionType{
						Kind:        ir.List,
						ElementType: ir.SchemaIR{Type: ir.TypeString},
					},
				},
			},
		},
	}

	var names []string
	walkSchema(schema, func(attr ir.AttributeIR) {
		names = append(names, attr.Name)
	})

	want := []string{"id", "nested", "email", "tags"}
	if len(names) != len(want) {
		t.Fatalf("got %v, want %v", names, want)
	}
	for i, w := range want {
		if names[i] != w {
			t.Errorf("names[%d] = %q, want %q", i, names[i], w)
		}
	}
}

func TestWalkSchema_VisitsUnionVariants(t *testing.T) {
	schema := ir.SchemaIR{
		Attributes: []ir.AttributeIR{
			{
				Name: "animal",
				Schema: ir.SchemaIR{
					Union: &ir.UnionType{
						Variants: []ir.SchemaIR{
							{Attributes: []ir.AttributeIR{{Name: "meow", Schema: ir.SchemaIR{Type: ir.TypeBool}}}},
							{Attributes: []ir.AttributeIR{{Name: "woof", Schema: ir.SchemaIR{Type: ir.TypeBool}}}},
						},
					},
				},
			},
		},
	}

	var names []string
	walkSchema(schema, func(attr ir.AttributeIR) {
		names = append(names, attr.Name)
	})

	want := []string{"animal", "meow", "woof"}
	if len(names) != len(want) {
		t.Fatalf("got %v, want %v", names, want)
	}
	for i, w := range want {
		if names[i] != w {
			t.Errorf("names[%d] = %q, want %q", i, names[i], w)
		}
	}
}

// TestOAuth2TokenURL covers every branch of oauth2TokenURL: nil flows, each
// SelectedFlow, the priority-order fallback, and the empty return.
func TestOAuth2TokenURL(t *testing.T) {
	flow := func(url string) *ir.OAuthFlowIR { return &ir.OAuthFlowIR{TokenURL: url} }
	all := &ir.OAuthFlowsIR{
		Implicit:          flow("https://implicit"),
		Password:          flow("https://password"),
		ClientCredentials: flow("https://client"),
		AuthorizationCode: flow("https://authcode"),
	}

	cases := []struct {
		name   string
		scheme ir.SecuritySchemeIR
		want   string
	}{
		{"nil-flows", ir.SecuritySchemeIR{}, ""},
		{"selected-client-credentials", ir.SecuritySchemeIR{Flows: all, SelectedFlow: "client_credentials"}, "https://client"},
		{"selected-password", ir.SecuritySchemeIR{Flows: all, SelectedFlow: "password"}, "https://password"},
		{"selected-authorization-code", ir.SecuritySchemeIR{Flows: all, SelectedFlow: "authorization_code"}, "https://authcode"},
		{"selected-implicit", ir.SecuritySchemeIR{Flows: all, SelectedFlow: "implicit"}, "https://implicit"},
		{"selected-unknown-falls-back", ir.SecuritySchemeIR{Flows: all, SelectedFlow: "nope"}, "https://client"},
		{"fallback-password-only", ir.SecuritySchemeIR{Flows: &ir.OAuthFlowsIR{Password: flow("https://pw")}}, "https://pw"},
		{"fallback-authcode-only", ir.SecuritySchemeIR{Flows: &ir.OAuthFlowsIR{AuthorizationCode: flow("https://ac")}}, "https://ac"},
		{"no-flows-empty", ir.SecuritySchemeIR{Flows: &ir.OAuthFlowsIR{}}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := oauth2TokenURL(tc.scheme); got != tc.want {
				t.Errorf("oauth2TokenURL() = %q, want %q", got, tc.want)
			}
		})
	}
}
