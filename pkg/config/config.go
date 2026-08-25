package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"
)

const (
	// DefaultProviderVersion is the provider version used when no explicit
	// version is supplied. It is shared by starter-config generation, IR
	// preview, and the config-to-IR conversion so that the default does not
	// drift across packages.
	DefaultProviderVersion = "0.1.0"
	// DefaultProtocolVersion is the Terraform protocol version used when no
	// explicit value is supplied.
	DefaultProtocolVersion = 6

	namingTransformSnakeCase           = "snake_case"
	polymorphismStrategySplitResources = "split_resources"
)

// Config is the top-level generator.yaml configuration.
type Config struct {
	Provider               ProviderConfig         `yaml:"provider" json:"provider"`
	Servers                []ServerConfig         `yaml:"servers,omitempty" json:"servers,omitempty"`
	ResourceOverrides      []ResourceOverride     `yaml:"resource_overrides,omitempty" json:"resource_overrides,omitempty"`
	DatasourceOverrides    []DatasourceOverride   `yaml:"datasource_overrides,omitempty" json:"datasource_overrides,omitempty"`
	ActionOverrides        []ActionOverride       `yaml:"action_overrides,omitempty" json:"action_overrides,omitempty"`
	EphemeralOverrides     []EphemeralOverride    `yaml:"ephemeral_resource_overrides,omitempty" json:"ephemeral_resource_overrides,omitempty"`
	ListResourceOverrides  []ListResourceOverride `yaml:"list_resource_overrides,omitempty" json:"list_resource_overrides,omitempty"`
	FunctionOverrides      []FunctionOverride     `yaml:"function_overrides,omitempty" json:"function_overrides,omitempty"`
	Logging                *LoggingConfig         `yaml:"logging,omitempty" json:"logging,omitempty"`
	Auth                   []AuthConfig           `yaml:"auth,omitempty" json:"auth,omitempty"`
	Security               *SecurityConfig        `yaml:"security,omitempty" json:"security,omitempty"`
	Naming                 *NamingConfig          `yaml:"naming,omitempty" json:"naming,omitempty"`
	SkipOperations         []string               `yaml:"skip_operations,omitempty" json:"skip_operations,omitempty"`
	IncludeOperations      []string               `yaml:"include_operations,omitempty" json:"include_operations,omitempty"`
	GlobalTimeouts         *TimeoutConfig         `yaml:"global_timeouts,omitempty" json:"global_timeouts,omitempty"`
	Pagination             *PaginationConfig      `yaml:"pagination,omitempty" json:"pagination,omitempty"`
	Polymorphism           *PolymorphismConfig    `yaml:"polymorphism,omitempty" json:"polymorphism,omitempty"`
	GenerateTerraformTests *bool                  `yaml:"generate_terraform_tests,omitempty" json:"generate_terraform_tests,omitempty"`
	// UsePutAsCreate is the kill-switch for PUT-as-create inference. When nil
	// (the field is absent, the auto-generator's natural state) or true, a CRUD
	// group with no collection POST but an instance PUT+GET+DELETE uses the PUT
	// as the Create (upsert) — default-on. When false, PUT-as-create is disabled
	// globally and those groups revert to the legacy scaffold behavior. It is a
	// *bool so the absent field is distinguishable from an explicit false (the
	// zero value of a plain bool), which is what makes "unset = on" legible. The
	// per-resource escape hatch is skip: true, not generate_resource: false
	// (GenerateResource is opt-in only and silently ignores false).
	UsePutAsCreate *bool `yaml:"use_put_as_create,omitempty" json:"use_put_as_create,omitempty"`
	// SignRelease is the opt-out for GPG signing of generated release
	// artifacts. Signed checksums are default-on: when nil (the field is absent,
	// the auto-generator's natural state) or true, the generated .goreleaser.yml
	// signs the checksums file and the release workflows import a GPG key. When
	// false, unsigned releases are produced and no GPG secrets are required. It
	// is a *bool so the absent field is distinguishable from an explicit false
	// (the zero value of a plain bool), which is what makes "unset = on" legible.
	// Operators enabling signed releases must configure GPG_PRIVATE_KEY and
	// GPG_PASSPHRASE repository secrets.
	SignRelease *bool            `yaml:"sign_release,omitempty" json:"sign_release,omitempty"`
	Generation  GenerationConfig `yaml:"generation,omitempty" json:"generation,omitempty"`
	Spec        SpecConfig       `yaml:"spec,omitempty" json:"spec,omitempty"`

	// Warnings holds non-fatal validation messages produced by Validate. Warnings
	// are not serialized back to generator.yaml; they are runtime metadata for
	// callers that want to surface guidance without failing validation.
	Warnings []string `yaml:"-" json:"-"`
}

// SpecConfig captures the source OpenAPI spec referenced by a starter
// generator.yaml. It is documentary today but is part of Config so that the
// spec section round-trips through LoadBytes instead of being silently dropped.
type SpecConfig struct {
	Path   string `yaml:"path,omitempty" json:"path,omitempty"`
	Format string `yaml:"format,omitempty" json:"format,omitempty"`
	// Auth is optional authentication for fetching a remote (http/https) spec
	// URL. Credentials are referenced only by environment variable name — never
	// by inline value — and are resolved at fetch time, mirroring AuthConfig's
	// EnvVar pattern (PROJECT_DESIGN §23). CLI flags override it when both are set.
	Auth *SpecAuthConfig `yaml:"auth,omitempty" json:"auth,omitempty"`
}

// SpecAuthConfig describes how a remote spec fetch authenticates. Every
// credential field names the environment variable that holds the secret; the
// loader reads them immediately before the request and never stores them on
// long-lived structs.
type SpecAuthConfig struct {
	// Scheme is one of bearer, basic, apiKey, or oauth2-client-credentials.
	Scheme string `yaml:"scheme,omitempty" json:"scheme,omitempty"`
	// HeaderName is the header the apiKey scheme sends the key in.
	HeaderName string `yaml:"header_name,omitempty" json:"header_name,omitempty"`
	// TokenEnv names the env var holding a bearer token (bearer scheme).
	TokenEnv string `yaml:"token_env,omitempty" json:"token_env,omitempty"`
	// UsernameEnv and PasswordEnv name the env vars for basic auth.
	UsernameEnv string `yaml:"username_env,omitempty" json:"username_env,omitempty"`
	PasswordEnv string `yaml:"password_env,omitempty" json:"password_env,omitempty"`
	// KeyEnv names the env var holding an apiKey value.
	KeyEnv string `yaml:"key_env,omitempty" json:"key_env,omitempty"`
	// TokenURL is the OAuth2 token endpoint for oauth2-client-credentials.
	TokenURL string `yaml:"token_url,omitempty" json:"token_url,omitempty"`
	// ClientIDEnv and ClientSecretEnv name the env vars holding the OAuth2
	// client credentials.
	ClientIDEnv     string `yaml:"client_id_env,omitempty" json:"client_id_env,omitempty"`
	ClientSecretEnv string `yaml:"client_secret_env,omitempty" json:"client_secret_env,omitempty"`
}

// ProviderConfig holds provider metadata.
type ProviderConfig struct {
	Name            string `yaml:"name" json:"name"`
	DisplayName     string `yaml:"display_name,omitempty" json:"display_name,omitempty"`
	Version         string `yaml:"version" json:"version"`
	Description     string `yaml:"description,omitempty" json:"description,omitempty"`
	Author          string `yaml:"author,omitempty" json:"author,omitempty"`
	ContactEmail    string `yaml:"contact_email,omitempty" json:"contact_email,omitempty"`
	License         string `yaml:"license,omitempty" json:"license,omitempty"`
	Repository      string `yaml:"repository,omitempty" json:"repository,omitempty"`
	ProtocolVersion int    `yaml:"protocol_version,omitempty" json:"protocol_version,omitempty"`
}

// ServerConfig describes an API server and its template variables.
type ServerConfig struct {
	URL         string                          `yaml:"url" json:"url"`
	Description string                          `yaml:"description,omitempty" json:"description,omitempty"`
	Variables   map[string]ServerVariableConfig `yaml:"variables,omitempty" json:"variables,omitempty"`
}

// ServerVariableConfig describes a single server template variable.
type ServerVariableConfig struct {
	Default     string   `yaml:"default,omitempty" json:"default,omitempty"`
	Enum        []string `yaml:"enum,omitempty" json:"enum,omitempty"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
}

// ResourceOverride customizes resource generation for a schema or operation.
//
// Matching priority: if Operation is non-empty, it is used exclusively and
// Schema is ignored. If Operation is empty, Schema is matched against the
// resource name, type name, and full name (case-insensitive, ignoring
// underscores and surrounding whitespace).
//
// Operation may be an OpenAPI operationId or a "METHOD /path" form (e.g.
// "PUT /snmp/throttle") matched against the resource's create method and path.
// The method+path form disambiguates operations that share an operationId.
//
// Providing ResourceName overwrites the resource's Name, TypeName, and
// FullName with toHumanName(ResourceName).
type ResourceOverride struct {
	Schema              string               `yaml:"schema,omitempty" json:"schema,omitempty"`
	Operation           string               `yaml:"operation,omitempty" json:"operation,omitempty"`
	ResourceName        string               `yaml:"resource_name,omitempty" json:"resource_name,omitempty"`
	DatasourceName      string               `yaml:"datasource_name,omitempty" json:"datasource_name,omitempty"`
	IDAttribute         string               `yaml:"id_attribute,omitempty" json:"id_attribute,omitempty"`
	ImportFormat        string               `yaml:"import_format,omitempty" json:"import_format,omitempty"`
	Timeouts            *TimeoutConfig       `yaml:"timeouts,omitempty" json:"timeouts,omitempty"`
	ForceNew            []string             `yaml:"force_new,omitempty" json:"force_new,omitempty"`
	ComputedAttributes  []string             `yaml:"computed_attributes,omitempty" json:"computed_attributes,omitempty"`
	SensitiveAttributes []string             `yaml:"sensitive_attributes,omitempty" json:"sensitive_attributes,omitempty"`
	WriteOnlyAttributes []WriteOnlyAttribute `yaml:"write_only_attributes,omitempty" json:"write_only_attributes,omitempty"`
	Skip                *bool                `yaml:"skip,omitempty" json:"skip,omitempty"`
	GenerateDatasource  *bool                `yaml:"generate_datasource,omitempty" json:"generate_datasource,omitempty"`
	GenerateResource    *bool                `yaml:"generate_resource,omitempty" json:"generate_resource,omitempty"`
	// CreateOperation/ReadOperation/UpdateOperation/DeleteOperation let an
	// override wire a resource whose create path differs from its read/delete
	// path (e.g. MyCloud dashboards: POST /dashboards/db vs
	// GET|DELETE /dashboards/uid/{uid}). When set, the override builds a
	// managed resource from these operations instead of relying on inference
	// (G8). Operation is the seed/match operation.
	CreateOperation string               `yaml:"create_operation,omitempty" json:"create_operation,omitempty"`
	ReadOperation   string               `yaml:"read_operation,omitempty" json:"read_operation,omitempty"`
	UpdateOperation string               `yaml:"update_operation,omitempty" json:"update_operation,omitempty"`
	DeleteOperation string               `yaml:"delete_operation,omitempty" json:"delete_operation,omitempty"`
	SchemaVersion   int                  `yaml:"schema_version,omitempty" json:"schema_version,omitempty"`
	StateUpgrades   []StateUpgradeConfig `yaml:"state_upgrades,omitempty" json:"state_upgrades,omitempty"`
	Description     string               `yaml:"description,omitempty" json:"description,omitempty"`
}

// StateUpgradeConfig describes a single state migration from a prior schema
// version to the next version.
//
// Renames maps old attribute names to their current names; BlockRenames maps old
// block names to their current block names.
//
// AddedAttributes/AddedBlocks name current attributes/blocks that did not exist
// in the prior schema (new fields); they are null-initialized during upgrade.
//
// RemovedAttributes/RemovedBlocks name prior attributes/blocks absent from the
// current schema (dropped fields); they are kept in the prior schema so
// historical state decodes, then dropped during upgrade.
type StateUpgradeConfig struct {
	From              int               `yaml:"from" json:"from"`
	Renames           map[string]string `yaml:"renames,omitempty" json:"renames,omitempty"`
	BlockRenames      map[string]string `yaml:"block_renames,omitempty" json:"block_renames,omitempty"`
	AddedAttributes   []string          `yaml:"added_attributes,omitempty" json:"added_attributes,omitempty"`
	AddedBlocks       []string          `yaml:"added_blocks,omitempty" json:"added_blocks,omitempty"`
	RemovedAttributes []string          `yaml:"removed_attributes,omitempty" json:"removed_attributes,omitempty"`
	RemovedBlocks     []string          `yaml:"removed_blocks,omitempty" json:"removed_blocks,omitempty"`
}

// WriteOnlyAttribute declares a write-only argument on a managed resource.
//
// Name is the attribute's leaf name (snake_cased by the consumer). Path is the
// dotted location of the attribute within the resource schema (e.g.
// "owner.password" for a nested attribute); for a top-level attribute Path
// equals Name. Path is recorded so that two attributes sharing a leaf name at
// different nesting levels do not collapse into a single config entry (L-30).
type WriteOnlyAttribute struct {
	Name        string `yaml:"name" json:"name"`
	Path        string `yaml:"path,omitempty" json:"path,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Sensitive   bool   `yaml:"sensitive,omitempty" json:"sensitive,omitempty"`
}

// DatasourceOverride customizes a data source mapping.
//
// Matching priority: if Operation is non-empty, it is used exclusively and
// Name is ignored. If Operation is empty, Name is matched against the data
// source name, type name, and full name (case-insensitive, ignoring underscores
// and surrounding whitespace).
//
// Operation may be an OpenAPI operationId or a "METHOD /path" form (e.g.
// "GET /snmp/throttle") matched against the data source's read method and path.
// The method+path form disambiguates operations that share an operationId.
//
// Providing DatasourceName overwrites the data source's Name, TypeName, and
// FullName with toHumanName(DatasourceName).
type DatasourceOverride struct {
	Operation      string `yaml:"operation,omitempty" json:"operation,omitempty"`
	Name           string `yaml:"name,omitempty" json:"name,omitempty"`
	DatasourceName string `yaml:"datasource_name,omitempty" json:"datasource_name,omitempty"`
}

// ActionOverride customizes an action mapping.
//
// Operation is required for matching. It is compared against the action's
// source operation (case-insensitive, ignoring underscores and surrounding
// whitespace) or its "METHOD /path" invoke mapping (e.g. "PUT /snmp/throttle").
// The method+path form disambiguates operations that share an operationId. If
// the action has no source operation, the override Operation is matched against
// the action's name and type name instead.
//
// Providing Name overwrites the action's Name, TypeName, and FullName with
// toHumanName(Name).
type ActionOverride struct {
	Operation        string `yaml:"operation" json:"operation"`
	Name             string `yaml:"name,omitempty" json:"name,omitempty"`
	Description      string `yaml:"description,omitempty" json:"description,omitempty"`
	ProgressMessages bool   `yaml:"progress_messages,omitempty" json:"progress_messages,omitempty"`
	ModifyPlan       bool   `yaml:"modify_plan,omitempty" json:"modify_plan,omitempty"`
	// ModifyPlanOperation and ValidateConfigOperation declare explicit
	// preflight / server-side validation endpoints as "METHOD /path" strings
	// (parsed by operationMappingFromString). When set, the generated action
	// wires ModifyPlan / ValidateConfig to call them; when only ModifyPlan is
	// true with no operation, the method stays an honest scaffold (F3).
	ModifyPlanOperation     string `yaml:"modify_plan_operation,omitempty" json:"modify_plan_operation,omitempty"`
	ValidateConfigOperation string `yaml:"validate_config_operation,omitempty" json:"validate_config_operation,omitempty"`
}

// EphemeralOverride customizes an ephemeral resource mapping.
//
// Operation is required for matching. It is compared against the ephemeral
// resource's source operation (case-insensitive, ignoring underscores and
// surrounding whitespace). If the ephemeral resource has no source operation,
// the override Operation is matched against its name and type name instead.
//
// Providing Name overwrites the ephemeral resource's Name, TypeName, and
// FullName with toHumanName(Name).
//
// OpenMapping, RenewMapping, and CloseMapping are preview-only heuristics
// parsed as "METHOD /path" strings by the validation endpoint; the full
// generation pipeline may resolve these mappings differently.
type EphemeralOverride struct {
	Operation    string        `yaml:"operation" json:"operation"`
	Name         string        `yaml:"name,omitempty" json:"name,omitempty"`
	Description  string        `yaml:"description,omitempty" json:"description,omitempty"`
	OpenMapping  string        `yaml:"open_mapping,omitempty" json:"open_mapping,omitempty"`
	CloseMapping string        `yaml:"close_mapping,omitempty" json:"close_mapping,omitempty"`
	RenewMapping string        `yaml:"renew_mapping,omitempty" json:"renew_mapping,omitempty"`
	ResultFields []ResultField `yaml:"result_fields,omitempty" json:"result_fields,omitempty"`
}

// ResultField describes a field returned by an ephemeral resource.
type ResultField struct {
	Name      string `yaml:"name" json:"name"`
	Type      string `yaml:"type,omitempty" json:"type,omitempty"`
	Sensitive bool   `yaml:"sensitive,omitempty" json:"sensitive,omitempty"`
}

// ListResourceOverride customizes a list resource mapping.
type ListResourceOverride struct {
	Resource     string             `yaml:"resource" json:"resource"`
	Operation    string             `yaml:"operation,omitempty" json:"operation,omitempty"`
	ConfigSchema []ListConfigSchema `yaml:"config_schema,omitempty" json:"config_schema,omitempty"`
	Pagination   *PaginationConfig  `yaml:"pagination,omitempty" json:"pagination,omitempty"`
}

// ListConfigSchema describes a list-resource filter/search argument.
type ListConfigSchema struct {
	Name        string `yaml:"name" json:"name"`
	Type        string `yaml:"type,omitempty" json:"type,omitempty"`
	Optional    bool   `yaml:"optional,omitempty" json:"optional,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// FunctionOverride customizes a provider-defined function.
//
// Operation is required for matching. It is compared against the function's
// source operation (case-insensitive, ignoring underscores and surrounding
// whitespace). If the function has no source operation, the override Operation
// is matched against the function's name and type name instead.
//
// Providing Name overwrites the function's Name, TypeName, and FullName with
// toHumanName(Name).
type FunctionOverride struct {
	Operation  string             `yaml:"operation" json:"operation"`
	Name       string             `yaml:"name,omitempty" json:"name,omitempty"`
	Type       string             `yaml:"type,omitempty" json:"type,omitempty"`
	Arguments  []FunctionArgument `yaml:"arguments,omitempty" json:"arguments,omitempty"`
	ReturnType string             `yaml:"return_type,omitempty" json:"return_type,omitempty"`
}

// FunctionArgument describes a function argument.
type FunctionArgument struct {
	Name string `yaml:"name" json:"name"`
	Type string `yaml:"type,omitempty" json:"type,omitempty"`
}

// LoggingConfig configures provider HTTP trace logging.
type LoggingConfig struct {
	Enabled                bool     `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	FilePath               string   `yaml:"file_path,omitempty" json:"file_path,omitempty"`
	CaptureRequestHeaders  bool     `yaml:"capture_request_headers,omitempty" json:"capture_request_headers,omitempty"`
	CaptureRequestBody     bool     `yaml:"capture_request_body,omitempty" json:"capture_request_body,omitempty"`
	CaptureResponseHeaders bool     `yaml:"capture_response_headers,omitempty" json:"capture_response_headers,omitempty"`
	CaptureResponseBody    bool     `yaml:"capture_response_body,omitempty" json:"capture_response_body,omitempty"`
	MaxBodyBytes           int      `yaml:"max_body_bytes,omitempty" json:"max_body_bytes,omitempty"`
	RedactHeaders          []string `yaml:"redact_headers,omitempty" json:"redact_headers,omitempty"`
}

// AuthConfig describes provider-level authentication configuration.
type AuthConfig struct {
	Scheme          string `yaml:"scheme" json:"scheme"`
	HeaderName      string `yaml:"header_name,omitempty" json:"header_name,omitempty"`
	EnvVar          string `yaml:"env_var,omitempty" json:"env_var,omitempty"`
	Flow            string `yaml:"flow,omitempty" json:"flow,omitempty"`
	ClientIDEnv     string `yaml:"client_id_env,omitempty" json:"client_id_env,omitempty"`
	ClientSecretEnv string `yaml:"client_secret_env,omitempty" json:"client_secret_env,omitempty"`
	TokenURL        string `yaml:"token_url,omitempty" json:"token_url,omitempty"`
	DiscoveryURL    string `yaml:"discovery_url,omitempty" json:"discovery_url,omitempty"`
}

// SecurityConfig selects which security scheme the generated provider uses when
// the spec declares multiple global security requirements (OpenAPI OR: any one
// suffices). When unset, eidos applies every declared scheme (AND) and warns
// (G6). Setting Scheme to a declared scheme name restricts the provider to that
// scheme, so practitioners authenticate with one alternative.
type SecurityConfig struct {
	Scheme string `yaml:"scheme,omitempty" json:"scheme,omitempty"`
}

// ValidateAuth reports the validation errors for a single AuthConfig entry. It
// is exported so callers that build auth configuration programmatically (e.g.
// the API suggestAuth path) can ensure their output satisfies eidos's own
// validation before returning it to users. Returns nil when the entry is valid.
func ValidateAuth(ac AuthConfig) []error {
	var errs []error
	if strings.TrimSpace(ac.Scheme) == "" {
		return []error{fmt.Errorf("scheme is required")}
	}
	validAuthSchemes := map[string]bool{"apiKey": true, "oauth2": true, "basic": true, "bearer": true}
	if !validAuthSchemes[ac.Scheme] {
		return []error{fmt.Errorf("scheme must be one of: apiKey, oauth2, basic, bearer")}
	}
	switch ac.Scheme {
	case "apiKey":
		if strings.TrimSpace(ac.HeaderName) == "" && strings.TrimSpace(ac.EnvVar) == "" {
			errs = append(errs, fmt.Errorf("apiKey requires header_name or env_var"))
		}
	case "oauth2":
		if strings.TrimSpace(ac.Flow) == "" {
			errs = append(errs, fmt.Errorf("oauth2 requires flow"))
		}
		if strings.TrimSpace(ac.TokenURL) == "" {
			errs = append(errs, fmt.Errorf("oauth2 requires token_url"))
		}
		if ac.Flow == "client_credentials" || ac.Flow == "authorization_code" {
			if strings.TrimSpace(ac.ClientIDEnv) == "" {
				errs = append(errs, fmt.Errorf("oauth2 flow %q requires client_id_env", ac.Flow))
			}
			if strings.TrimSpace(ac.ClientSecretEnv) == "" {
				errs = append(errs, fmt.Errorf("oauth2 flow %q requires client_secret_env", ac.Flow))
			}
		}
	}
	return errs
}

// NamingConfig controls generated Terraform naming conventions.
type NamingConfig struct {
	ResourcePrefix   string `yaml:"resource_prefix,omitempty" json:"resource_prefix,omitempty"`
	DatasourcePrefix string `yaml:"datasource_prefix,omitempty" json:"datasource_prefix,omitempty"`
	ResourceSuffix   string `yaml:"resource_suffix,omitempty" json:"resource_suffix,omitempty"`
	Transform        string `yaml:"transform,omitempty" json:"transform,omitempty"`
}

// TimeoutConfig holds per-operation timeouts.
type TimeoutConfig struct {
	Create *Duration `yaml:"create,omitempty" json:"create,omitempty"`
	Read   *Duration `yaml:"read,omitempty" json:"read,omitempty"`
	Update *Duration `yaml:"update,omitempty" json:"update,omitempty"`
	Delete *Duration `yaml:"delete,omitempty" json:"delete,omitempty"`
}

// PaginationConfig describes collection pagination behavior.
type PaginationConfig struct {
	Style            string `yaml:"style,omitempty" json:"style,omitempty"`
	PageParam        string `yaml:"page_param,omitempty" json:"page_param,omitempty"`
	PerPageParam     string `yaml:"per_page_param,omitempty" json:"per_page_param,omitempty"`
	TotalCountHeader string `yaml:"total_count_header,omitempty" json:"total_count_header,omitempty"`
	NextLinkHeader   string `yaml:"next_link_header,omitempty" json:"next_link_header,omitempty"`
	CursorField      string `yaml:"cursor_field,omitempty" json:"cursor_field,omitempty"`
}

// PolymorphismConfig controls oneOf/anyOf generation strategy.
type PolymorphismConfig struct {
	Strategy string          `yaml:"strategy,omitempty" json:"strategy,omitempty"`
	OneOf    []OneOfOverride `yaml:"oneOf,omitempty" json:"oneOf,omitempty"`
}

// DiscriminatorConfig captures OpenAPI/JSON Schema discriminator metadata for a
// polymorphic union.
type DiscriminatorConfig struct {
	PropertyName string            `yaml:"property_name,omitempty" json:"property_name,omitempty"`
	Mapping      map[string]string `yaml:"mapping,omitempty" json:"mapping,omitempty"`
}

// OneOfOverride maps a schema's oneOf to a set of variants.
type OneOfOverride struct {
	Schema        string               `yaml:"schema" json:"schema"`
	Variants      []Variant            `yaml:"variants,omitempty" json:"variants,omitempty"`
	Discriminator *DiscriminatorConfig `yaml:"discriminator,omitempty" json:"discriminator,omitempty"`
}

// Variant is a single oneOf variant mapping.
type Variant struct {
	Schema         string               `yaml:"schema" json:"schema"`
	ResourceName   string               `yaml:"resource_name,omitempty" json:"resource_name,omitempty"`
	DatasourceName string               `yaml:"datasource_name,omitempty" json:"datasource_name,omitempty"`
	Discriminator  *DiscriminatorConfig `yaml:"discriminator,omitempty" json:"discriminator,omitempty"`
}

// GenerationConfig controls which constructs are generated and how generated
// code is organized into sub-packages. It is the top-level generator.yaml
// section that drives allow-list/deny-list filtering and code splitting.
type GenerationConfig struct {
	Resources          ResourceGenerationConfig `yaml:"resources,omitempty" json:"resources,omitempty"`
	DataSources        ResourceGenerationConfig `yaml:"datasources,omitempty" json:"datasources,omitempty"`
	Actions            ResourceGenerationConfig `yaml:"actions,omitempty" json:"actions,omitempty"`
	EphemeralResources ResourceGenerationConfig `yaml:"ephemeral_resources,omitempty" json:"ephemeral_resources,omitempty"`
	ListResources      ResourceGenerationConfig `yaml:"list_resources,omitempty" json:"list_resources,omitempty"`
	Functions          ResourceGenerationConfig `yaml:"functions,omitempty" json:"functions,omitempty"`
	SkipTests          bool                     `yaml:"skip_tests,omitempty" json:"skip_tests,omitempty"`
	SkipDocs           bool                     `yaml:"skip_docs,omitempty" json:"skip_docs,omitempty"`
	SkipBuild          bool                     `yaml:"skip_build,omitempty" json:"skip_build,omitempty"`
	DynamicRelease     *DynamicReleaseConfig    `yaml:"dynamic_release,omitempty" json:"dynamic_release,omitempty"`
}

// DynamicReleaseConfig opts into generating a second GitHub Actions workflow
// (`.github/workflows/regenerate-and-release.yml`) that regenerates the
// provider from its OpenAPI spec and publishes a release in one
// manually-dispatched run, using the eidos CI image. It complements the static
// generated release.yml (which builds committed code on a v* tag push); the two
// coexist with non-overlapping triggers (manual dispatch vs. tag push). The
// regenerated code is committed to a release-specific branch so the default
// branch keeps only the spec and the committed build scaffolding.
type DynamicReleaseConfig struct {
	// Enabled turns on generation of the regenerate-and-release workflow.
	Enabled bool `yaml:"enabled" json:"enabled"`
	// Image is the eidos CI image reference the workflow runs in. Defaults to
	// ghcr.io/signalbreak-labs/eidos:latest when empty.
	Image string `yaml:"image,omitempty" json:"image,omitempty"`
	// SpecPath is the path to the OpenAPI spec, relative to the provider repo
	// root, that the workflow regenerates from. Defaults to spec.yaml when
	// empty.
	SpecPath string `yaml:"spec_path,omitempty" json:"spec_path,omitempty"`
}

// ResourceGenerationConfig configures filtering and package splitting for a
// single construct family (resources, data sources, actions, etc.).
//
// Include is the allow-list; when non-empty, only constructs whose name
// matches at least one pattern are retained. Exclude is the deny-list;
// constructs matching any exclude pattern are dropped regardless of the
// allow-list.
//
// Package is the default sub-package for included constructs. PackageRules
// override the default package for constructs that match their patterns. An
// empty Package (and no matching rule) keeps the construct in the root
// internal/provider package.
type ResourceGenerationConfig struct {
	Include  []string            `yaml:"include,omitempty" json:"include,omitempty"`
	Exclude  []string            `yaml:"exclude,omitempty" json:"exclude,omitempty"`
	Package  string              `yaml:"package,omitempty" json:"package,omitempty"`
	Packages []PackageRuleConfig `yaml:"packages,omitempty" json:"packages,omitempty"`
}

// PackageRuleConfig assigns constructs matching its include/exclude patterns to
// a named sub-package under internal/provider.
type PackageRuleConfig struct {
	Name    string   `yaml:"name" json:"name"`
	Include []string `yaml:"include,omitempty" json:"include,omitempty"`
	Exclude []string `yaml:"exclude,omitempty" json:"exclude,omitempty"`
}

// Duration wraps time.Duration to parse strings like "30m" in YAML.
type Duration time.Duration

// UnmarshalYAML parses a duration string from a YAML scalar.
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var s string
	if err := node.Decode(&s); err != nil {
		return err
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(dur)
	return nil
}

// MarshalYAML serializes the duration as a human-readable string.
func (d Duration) MarshalYAML() (interface{}, error) {
	return formatDuration(time.Duration(d)), nil
}

// MarshalJSON serializes the duration as a human-readable string so JSON
// output mirrors the YAML representation.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(formatDuration(time.Duration(d)))
}

// UnmarshalJSON parses a duration string from JSON into a Duration.
func (d *Duration) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(dur)
	return nil
}

// Duration returns the underlying time.Duration value.
func (d *Duration) Duration() time.Duration {
	if d == nil {
		return 0
	}
	return time.Duration(*d)
}

func formatDuration(d time.Duration) string {
	if d == 0 {
		return "0s"
	}
	sign := ""
	// The magnitude is computed in uint64: -d overflows int64 when
	// d == math.MinInt64 (|MinInt64| = 2^63 does not fit), and the previous
	// workaround time.Duration(uint64(d)) merely wrapped back to MinInt64 — a
	// no-op that produced a malformed "-" output. -(d+1)+1 equals -d for every
	// negative d while never overflowing (d+1 is representable) (N-43).
	var mag uint64
	if d < 0 {
		sign = "-"
		// gosec: the conversion is provably non-overflowing — for d < 0 the
		// magnitude |d| fits in uint64 (max is 2^63, exactly uint64's span
		// beyond int64), and -(d+1) is ≤ 2^63-1 which fits int64.
		mag = uint64(-(d + 1)) + 1 //nolint:gosec // magnitude fits uint64 by construction
	} else {
		mag = uint64(d)
	}
	var parts []string
	if h := mag / uint64(time.Hour); h > 0 {
		parts = append(parts, fmt.Sprintf("%dh", h))
		mag -= h * uint64(time.Hour)
	}
	if m := mag / uint64(time.Minute); m > 0 {
		parts = append(parts, fmt.Sprintf("%dm", m))
		mag -= m * uint64(time.Minute)
	}
	if s := mag / uint64(time.Second); s > 0 {
		parts = append(parts, fmt.Sprintf("%ds", s))
		mag -= s * uint64(time.Second)
	}
	if ms := mag / uint64(time.Millisecond); ms > 0 {
		parts = append(parts, fmt.Sprintf("%dms", ms))
		mag -= ms * uint64(time.Millisecond)
	}
	if us := mag / uint64(time.Microsecond); us > 0 {
		parts = append(parts, fmt.Sprintf("%dus", us))
		mag -= us * uint64(time.Microsecond)
	}
	if ns := mag; ns > 0 {
		parts = append(parts, fmt.Sprintf("%dns", ns))
	}
	return sign + strings.Join(parts, "")
}

// Load reads and parses a generator.yaml file from the given path.
func Load(path string) (*Config, error) {
	//nolint:gosec // Load is an intentional file-read API; callers supply the config path.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %q: %w", path, err)
	}
	return LoadBytes(data)
}

// LoadBytes parses generator.yaml bytes into a Config, applies defaults, and validates it.
func LoadBytes(data []byte) (*Config, error) {
	// Empty, whitespace-only, or comment-only configs fail yaml's Decode with a
	// bare "EOF" — accurate but useless (N-41). Reject them with an actionable
	// message before decoding.
	if !hasYAMLContent(data) {
		return nil, fmt.Errorf("generator.yaml is empty or contains only whitespace/comments; provide a config with at least a provider.name")
	}

	var cfg Config
	// Strict decoding: unknown or misspelled keys (e.g. "overrides_extra",
	// "time_outs") are rejected instead of being silently dropped, so a user's
	// overrides do not vanish without an error.
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal generator.yaml: %w", err)
	}
	// A concatenated config (or an accidental "---") silently drops everything
	// after the first document (N-40). Detect a second document and reject
	// fail-loud instead of generating a provider with half the overrides. A
	// trailing empty document (a stray "---" at EOF with nothing after) decodes
	// to a nil value and is tolerated; only a real second document is an error.
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, fmt.Errorf("failed to decode generator.yaml document separator: %w", err)
		}
		if extra != nil {
			return nil, fmt.Errorf("generator.yaml contains multiple YAML documents; the config must be a single document (remove the extra %q separator)", "---")
		}
	}
	ApplyDefaults(&cfg)
	if err := Validate(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ApplyDefaults fills in default values for a Config in place.
// It is called by Load/LoadBytes before validation and may be called
// independently when constructing a Config programmatically.
func ApplyDefaults(cfg *Config) {
	if cfg == nil {
		return
	}
	if cfg.Naming != nil {
		if strings.TrimSpace(cfg.Naming.Transform) == "" {
			cfg.Naming.Transform = namingTransformSnakeCase
		}
	}
}

// Validate checks that a Config satisfies structural and value constraints.
// It appends non-fatal warnings to cfg.Warnings (for example, when both
// operation and name are set on a datasource override) but does not otherwise
// modify configuration values. Callers that want default values applied should
// call ApplyDefaults first, or use Load/LoadBytes, which calls ApplyDefaults
// before Validate.
func Validate(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	var errs []error

	if strings.TrimSpace(cfg.Provider.Name) == "" {
		errs = append(errs, fmt.Errorf("provider.name is required"))
	}
	if strings.TrimSpace(cfg.Provider.Version) == "" {
		errs = append(errs, fmt.Errorf("provider.version is required"))
	}
	if cfg.Provider.ProtocolVersion != 0 && cfg.Provider.ProtocolVersion != 5 && cfg.Provider.ProtocolVersion != 6 {
		errs = append(errs, fmt.Errorf("provider.protocol_version must be 5 or 6, got %d", cfg.Provider.ProtocolVersion))
	}

	if cfg.Naming != nil {
		if strings.TrimSpace(cfg.Naming.Transform) != "" {
			// N-46: camelCase and PascalCase were validated and documented but
			// never implemented — applyNamingOverrides treats transform as a no-op
			// because inferred names are already snake_case. Reject them fail-loud
			// instead of advertising a config surface that does nothing.
			if cfg.Naming.Transform != namingTransformSnakeCase {
				errs = append(errs, fmt.Errorf("naming.transform %q is not implemented; only %s is supported (inferred names are always normalized to snake_case)", cfg.Naming.Transform, namingTransformSnakeCase))
			}
		}
	}

	// Empty skip/include patterns are rejected fail-loud (M-15): a pasted
	// trailing "- " (or an empty string threaded through an LLM) would otherwise
	// silently exclude every operation — MatchName("", opID) is false for every
	// non-empty operationId — and generate a provider with zero resources. This
	// mirrors the generation.<kind>.include/exclude empty-entry checks below.
	for i, p := range cfg.SkipOperations {
		if strings.TrimSpace(p) == "" {
			errs = append(errs, fmt.Errorf("skip_operations[%d] is empty", i))
		}
	}
	for i, p := range cfg.IncludeOperations {
		if strings.TrimSpace(p) == "" {
			errs = append(errs, fmt.Errorf("include_operations[%d] is empty", i))
		}
	}

	if err := validatePaginationConfig(cfg.Pagination, "pagination"); err != nil {
		errs = append(errs, err)
	}

	if err := validateTimeoutConfig(cfg.GlobalTimeouts, "global_timeouts"); err != nil {
		errs = append(errs, err)
	}

	if cfg.Polymorphism != nil && cfg.Polymorphism.Strategy != "" {
		valid := map[string]bool{"dynamic_union": true, "split_resources": true}
		if !valid[cfg.Polymorphism.Strategy] {
			errs = append(errs, fmt.Errorf("polymorphism.strategy must be one of: dynamic_union, split_resources"))
		}
	}

	for i := range cfg.ResourceOverrides {
		ro := &cfg.ResourceOverrides[i]
		if strings.TrimSpace(ro.Schema) == "" && strings.TrimSpace(ro.Operation) == "" {
			errs = append(errs, fmt.Errorf("resource_overrides[%d]: either schema or operation is required", i))
		}
		if strings.TrimSpace(ro.Schema) != "" && strings.TrimSpace(ro.Operation) != "" {
			cfg.Warnings = append(cfg.Warnings, fmt.Sprintf("resource_overrides[%d]: both schema %q and operation %q are set; operation takes precedence and schema is ignored", i, ro.Schema, ro.Operation))
		}
		// N-45: generate_resource: true builds a resource from an operation (or
		// create_operation). Without a seed operation applyResourceCreationOverrides
		// silently continues, so the opt-in would pass Validate and then do
		// nothing. Require the seed here fail-loud instead.
		if ro.GenerateResource != nil && *ro.GenerateResource &&
			strings.TrimSpace(ro.Operation) == "" && strings.TrimSpace(ro.CreateOperation) == "" {
			errs = append(errs, fmt.Errorf("resource_overrides[%d]: generate_resource: true requires an operation (or create_operation) to build the resource from", i))
		}
		// N-45: generate_resource: false is silently ignored by design (the escape
		// hatch is skip: true). Surface a warning so a practitioner who wrote
		// generate_resource: false expecting it to skip gets pointed at the
		// supported knob instead of a silent no-op.
		if ro.GenerateResource != nil && !*ro.GenerateResource {
			cfg.Warnings = append(cfg.Warnings, fmt.Sprintf("resource_overrides[%d]: generate_resource: false is a no-op; use skip: true to exclude this resource", i))
		}
		for j, woa := range ro.WriteOnlyAttributes {
			if strings.TrimSpace(woa.Name) == "" {
				errs = append(errs, fmt.Errorf("resource_overrides[%d].write_only_attributes[%d].name is required", i, j))
			}
		}
		if err := validateStateUpgrades(ro.SchemaVersion, ro.StateUpgrades, i); err != nil {
			errs = append(errs, err)
		}
		if err := validateTimeoutConfig(ro.Timeouts, fmt.Sprintf("resource_overrides[%d].timeouts", i)); err != nil {
			errs = append(errs, err)
		}
	}

	for i, do := range cfg.DatasourceOverrides {
		if strings.TrimSpace(do.Operation) == "" && strings.TrimSpace(do.Name) == "" {
			errs = append(errs, fmt.Errorf("datasource_overrides[%d]: either operation or name is required", i))
		}
		if strings.TrimSpace(do.Operation) != "" && strings.TrimSpace(do.Name) != "" {
			cfg.Warnings = append(cfg.Warnings, fmt.Sprintf("datasource_overrides[%d]: both operation %q and name %q are set; operation takes precedence and name is ignored", i, do.Operation, do.Name))
		}
	}

	for i, ao := range cfg.ActionOverrides {
		if strings.TrimSpace(ao.Operation) == "" {
			errs = append(errs, fmt.Errorf("action_overrides[%d]: operation is required", i))
		}
	}

	for i, eo := range cfg.EphemeralOverrides {
		if strings.TrimSpace(eo.Operation) == "" {
			errs = append(errs, fmt.Errorf("ephemeral_resource_overrides[%d]: operation is required", i))
		}
	}

	for i, lo := range cfg.ListResourceOverrides {
		if strings.TrimSpace(lo.Operation) == "" && strings.TrimSpace(lo.Resource) == "" {
			errs = append(errs, fmt.Errorf("list_resource_overrides[%d]: either operation or resource is required", i))
		}
		if strings.TrimSpace(lo.Operation) != "" && strings.TrimSpace(lo.Resource) != "" {
			cfg.Warnings = append(cfg.Warnings, fmt.Sprintf("list_resource_overrides[%d]: both operation and resource are set; operation takes precedence and resource is ignored", i))
		}
		for j, cs := range lo.ConfigSchema {
			if strings.TrimSpace(cs.Name) == "" {
				errs = append(errs, fmt.Errorf("list_resource_overrides[%d].config_schema[%d].name is required", i, j))
			}
		}
		if err := validatePaginationConfig(lo.Pagination, fmt.Sprintf("list_resource_overrides[%d].pagination", i)); err != nil {
			errs = append(errs, err)
		}
	}

	for i, fo := range cfg.FunctionOverrides {
		if strings.TrimSpace(fo.Operation) == "" {
			errs = append(errs, fmt.Errorf("function_overrides[%d]: operation is required", i))
		}
		for j, arg := range fo.Arguments {
			if strings.TrimSpace(arg.Name) == "" {
				errs = append(errs, fmt.Errorf("function_overrides[%d].arguments[%d].name is required", i, j))
			}
		}
	}

	for i, ac := range cfg.Auth {
		for _, e := range ValidateAuth(ac) {
			errs = append(errs, fmt.Errorf("auth[%d]: %w", i, e))
		}
	}

	if cfg.Polymorphism != nil {
		for i, oo := range cfg.Polymorphism.OneOf {
			if strings.TrimSpace(oo.Schema) == "" {
				errs = append(errs, fmt.Errorf("polymorphism.oneOf[%d].schema is required", i))
			}
			seen := make(map[string]struct{})
			for j, v := range oo.Variants {
				if strings.TrimSpace(v.Schema) == "" {
					errs = append(errs, fmt.Errorf("polymorphism.oneOf[%d].variants[%d].schema is required", i, j))
					continue
				}
				schemaKey := strings.TrimSpace(v.Schema)
				if _, dup := seen[schemaKey]; dup {
					errs = append(errs, fmt.Errorf("polymorphism.oneOf[%d].variants[%d]: duplicate schema %q (whitespace-insensitive match with %q)", i, j, v.Schema, schemaKey))
				}
				seen[schemaKey] = struct{}{}
				if cfg.Polymorphism.Strategy == polymorphismStrategySplitResources &&
					strings.TrimSpace(v.ResourceName) == "" && strings.TrimSpace(v.DatasourceName) == "" {
					errs = append(errs, fmt.Errorf("polymorphism.oneOf[%d].variants[%d]: resource_name or datasource_name is required when strategy is %s", i, j, polymorphismStrategySplitResources))
				}
			}
		}
	}

	errs = append(errs, validateGenerationConfig(&cfg.Generation)...)

	return errors.Join(errs...)
}

// validatePaginationConfig checks that a pagination style, if set, is one of the
// supported values. A nil config or empty style is valid.
func validatePaginationConfig(p *PaginationConfig, path string) error {
	if p == nil || strings.TrimSpace(p.Style) == "" {
		return nil
	}
	valid := map[string]bool{"offset": true, "cursor": true, "link_header": true, "none": true}
	if !valid[p.Style] {
		return fmt.Errorf("%s.style must be one of: offset, cursor, link_header, none", path)
	}
	return nil
}

// validateTimeoutConfig checks that every configured timeout duration is
// positive. A nil config is valid.
func validateTimeoutConfig(t *TimeoutConfig, path string) error {
	if t == nil {
		return nil
	}
	var errs []error
	check := func(d *Duration, name string) {
		if d == nil {
			return
		}
		if time.Duration(*d) <= 0 {
			errs = append(errs, fmt.Errorf("%s.%s must be a positive duration", path, name))
		}
	}
	check(t.Create, "create")
	check(t.Read, "read")
	check(t.Update, "update")
	check(t.Delete, "delete")
	return errors.Join(errs...)
}

// validateGenerationConfig checks that generation filters and package rules are
// structurally valid. It returns a slice of validation errors.
// validateGenerationConfig checks the generation.* include/exclude/override
// surface. The caller always passes &cfg.Generation (a value field), so cfg is
// never nil; the guard is deliberately omitted (N-44).
func validateGenerationConfig(cfg *GenerationConfig) []error {
	errs := make([]error, 0, 6)
	errs = append(errs, validateResourceGenerationConfig("resources", &cfg.Resources)...)
	errs = append(errs, validateResourceGenerationConfig("datasources", &cfg.DataSources)...)
	errs = append(errs, validateResourceGenerationConfig("actions", &cfg.Actions)...)
	errs = append(errs, validateResourceGenerationConfig("ephemeral_resources", &cfg.EphemeralResources)...)
	errs = append(errs, validateResourceGenerationConfig("list_resources", &cfg.ListResources)...)
	errs = append(errs, validateResourceGenerationConfig("functions", &cfg.Functions)...)

	return errs
}

func validateResourceGenerationConfig(field string, cfg *ResourceGenerationConfig) []error {
	if cfg == nil {
		return nil
	}

	var errs []error
	for i, p := range cfg.Include {
		if strings.TrimSpace(p) == "" {
			errs = append(errs, fmt.Errorf("generation.%s.include[%d] is empty", field, i))
		}
	}
	for i, p := range cfg.Exclude {
		if strings.TrimSpace(p) == "" {
			errs = append(errs, fmt.Errorf("generation.%s.exclude[%d] is empty", field, i))
		}
	}
	if pkg := strings.TrimSpace(cfg.Package); pkg != "" && !isValidPackageName(pkg) {
		errs = append(errs, fmt.Errorf("generation.%s.package %q is not a valid package name", field, pkg))
	}
	for i, rule := range cfg.Packages {
		if strings.TrimSpace(rule.Name) == "" {
			errs = append(errs, fmt.Errorf("generation.%s.packages[%d].name is required", field, i))
			continue
		}
		if !isValidPackageName(strings.TrimSpace(rule.Name)) {
			errs = append(errs, fmt.Errorf("generation.%s.packages[%d].name %q is not a valid package name", field, i, rule.Name))
		}
		for j, p := range rule.Include {
			if strings.TrimSpace(p) == "" {
				errs = append(errs, fmt.Errorf("generation.%s.packages[%d].include[%d] is empty", field, i, j))
			}
		}
		for j, p := range rule.Exclude {
			if strings.TrimSpace(p) == "" {
				errs = append(errs, fmt.Errorf("generation.%s.packages[%d].exclude[%d] is empty", field, i, j))
			}
		}
	}

	return errs
}

// isValidPackageName reports whether s is a safe Go package identifier segment.
// It allows letters, digits, and underscores, but requires the first character
// to be a letter or underscore so the segment cannot be mistaken for a path
// traversal sequence.
func isValidPackageName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 && (!unicode.IsLetter(r) && r != '_') {
			return false
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
	}
	return true
}

// validateStateUpgrades checks that the configured state upgrades are valid:
//   - from versions are non-negative
//   - from versions are unique
//   - from versions form a contiguous prefix starting at 0
//   - when schema_version is explicit (>0), it equals len(state_upgrades)
//   - when schema_version is 0 (unspecified) with non-empty upgrades, the final
//     schema version is implicitly derived as len(state_upgrades); callers must
//     ensure the generated resource code uses the same derivation
//   - rename keys/values are non-empty
func validateStateUpgrades(schemaVersion int, upgrades []StateUpgradeConfig, idx int) error {
	if len(upgrades) == 0 {
		return nil
	}

	seen := make(map[int]struct{}, len(upgrades))
	fromVersions := make([]int, 0, len(upgrades))
	for j, su := range upgrades {
		if su.From < 0 {
			return fmt.Errorf("resource_overrides[%d].state_upgrades[%d].from must be non-negative", idx, j)
		}
		if _, dup := seen[su.From]; dup {
			return fmt.Errorf("resource_overrides[%d].state_upgrades[%d].from %d is duplicated", idx, j, su.From)
		}
		seen[su.From] = struct{}{}
		fromVersions = append(fromVersions, su.From)
		// Iterate renames in sorted key order so the reported error (when several
		// entries are invalid) is deterministic across runs; a raw map range over a
		// Go map is non-deterministic (N-42).
		for _, oldName := range sortedKeys(su.Renames) {
			if strings.TrimSpace(oldName) == "" {
				return fmt.Errorf("resource_overrides[%d].state_upgrades[%d].renames contains empty old attribute name", idx, j)
			}
			if strings.TrimSpace(su.Renames[oldName]) == "" {
				return fmt.Errorf("resource_overrides[%d].state_upgrades[%d].renames[%q] maps to empty new attribute name", idx, j, oldName)
			}
		}
		for _, oldName := range sortedKeys(su.BlockRenames) {
			if strings.TrimSpace(oldName) == "" {
				return fmt.Errorf("resource_overrides[%d].state_upgrades[%d].block_renames contains empty old block name", idx, j)
			}
			if strings.TrimSpace(su.BlockRenames[oldName]) == "" {
				return fmt.Errorf("resource_overrides[%d].state_upgrades[%d].block_renames[%q] maps to empty new block name", idx, j, oldName)
			}
		}
		for _, name := range su.AddedAttributes {
			if strings.TrimSpace(name) == "" {
				return fmt.Errorf("resource_overrides[%d].state_upgrades[%d].added_attributes contains empty name", idx, j)
			}
		}
		for _, name := range su.AddedBlocks {
			if strings.TrimSpace(name) == "" {
				return fmt.Errorf("resource_overrides[%d].state_upgrades[%d].added_blocks contains empty name", idx, j)
			}
		}
		for _, name := range su.RemovedAttributes {
			if strings.TrimSpace(name) == "" {
				return fmt.Errorf("resource_overrides[%d].state_upgrades[%d].removed_attributes contains empty name", idx, j)
			}
		}
		for _, name := range su.RemovedBlocks {
			if strings.TrimSpace(name) == "" {
				return fmt.Errorf("resource_overrides[%d].state_upgrades[%d].removed_blocks contains empty name", idx, j)
			}
		}
		// A field cannot be both added and removed in the same upgrade step.
		if err := validateNoAddRemoveOverlap(su, idx, j); err != nil {
			return err
		}
	}

	sort.Ints(fromVersions)
	for i, v := range fromVersions {
		if v != i {
			return fmt.Errorf("resource_overrides[%d].state_upgrades has gap or unexpected from version %d at position %d (expected %d); state upgrades must cover contiguous versions 0..N", idx, v, i, i)
		}
	}

	if schemaVersion > 0 && schemaVersion != len(upgrades) {
		return fmt.Errorf("resource_overrides[%d].schema_version (%d) must equal len(state_upgrades) (%d) when state upgrades are configured", idx, schemaVersion, len(upgrades))
	}

	return nil
}

// validateNoAddRemoveOverlap rejects a state upgrade that declares the same
// attribute or block as both added and removed in a single step, since the two
// are contradictory (a field is either new or dropped, not both).
func validateNoAddRemoveOverlap(su StateUpgradeConfig, idx, j int) error {
	addedAttr := make(map[string]struct{}, len(su.AddedAttributes))
	for _, n := range su.AddedAttributes {
		addedAttr[n] = struct{}{}
	}
	for _, n := range su.RemovedAttributes {
		if _, ok := addedAttr[n]; ok {
			return fmt.Errorf("resource_overrides[%d].state_upgrades[%d]: %q appears in both added_attributes and removed_attributes", idx, j, n)
		}
	}
	addedBlock := make(map[string]struct{}, len(su.AddedBlocks))
	for _, n := range su.AddedBlocks {
		addedBlock[n] = struct{}{}
	}
	for _, n := range su.RemovedBlocks {
		if _, ok := addedBlock[n]; ok {
			return fmt.Errorf("resource_overrides[%d].state_upgrades[%d]: %q appears in both added_blocks and removed_blocks", idx, j, n)
		}
	}
	return nil
}

// sortedKeys returns the keys of m in sorted order. It exists so map iteration
// never leaks into error text or emitted output, keeping behavior deterministic.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// hasYAMLContent reports whether data contains any line that is neither blank
// nor a YAML comment (a '#' as the first non-whitespace character). A line like
// `name: "value # not a comment"` still counts as content because the '#' is not
// leading. Used to reject empty/whitespace/comment-only configs with an
// actionable error instead of yaml's bare "EOF" (N-41).
func hasYAMLContent(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		return true
	}
	return false
}
