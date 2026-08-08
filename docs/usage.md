# Eidos Usage Guide

This guide covers installing Eidos, running each CLI command, authoring
`generator.yaml`, and interpreting generator output.

For a short project overview, see [`README.md`](../README.md).  
For design and architecture details, see [`PROJECT_DESIGN.md`](PROJECT_DESIGN.md).

## Table of Contents

- [Installation](#installation)
- [Command overview](#command-overview)
- [`eidos generate`](#eidos-generate)
- [`eidos generate-config`](#eidos-generate-config)
- [`eidos api`](#eidos-api)
- [`eidos mcp`](#eidos-mcp)
- [`generator.yaml` reference](#generatoryaml-reference)
- [Generated provider layout](#generated-provider-layout)
- [Dry-run output](#dry-run-output)
- [Examples](#examples)
- [Current limitations](#current-limitations)
- [Troubleshooting](#troubleshooting)

## Installation

Install via Homebrew (recommended):

```bash
brew tap signalbreak-labs/tap
brew install eidos
```

Or build from source. From the repository root:

```bash
go build -o eidos ./cmd/eidos
```

Or install the latest release with:

```bash
go install github.com/signalbreak-labs/eidos/cmd/eidos@latest
```

Confirm the binary works:

```bash
eidos --version
```

## Command overview

```text
Usage:
  eidos [command]

Available Commands:
  generate        Generate a Terraform provider from an OpenAPI spec
  generate-config Generate a starter generator.yaml from an OpenAPI spec
  api             Start the Eidos HTTP API server
  mcp             Start the Eidos MCP server

Flags:
  -h, --help      help for eidos
  -v, --version   version for eidos

Use "eidos [command] --help" for more information about a command.
```

## `eidos generate`

Generate a Terraform provider from an OpenAPI specification.

```bash
eidos generate --spec ./api.yaml --config ./generator.yaml --output ./terraform-provider-mycloud
```

### Flags

| Flag | Default | Required | Description |
|------|---------|----------|-------------|
| `--spec` | — | **yes** | Path to the OpenAPI spec file (JSON or YAML), or an http(s) URL to fetch. |
| `--output` | — | **yes** (full generation) | Target directory for generated files. Required when not using `--dry-run`. |
| `--config` | *(none)* | no | Path to a `generator.yaml` overrides file. |
| `--dry-run` | `false` | no | Run the pipeline without writing files and print a summary. |
| `--spec-allow-http` | `false` | no | Permit `http://` spec URLs (https is the default for remote specs). |
| `--spec-auth-scheme` | *(none)* | no | Authenticate a remote spec fetch: `bearer`, `basic`, `apiKey`, or `oauth2-client-credentials`. Credential *values* come from environment variables (see below). |
| `--spec-token-env` | *(none)* | no | Environment variable holding the bearer token for `--spec-auth-scheme bearer`. |
| `--spec-username-env` | *(none)* | no | Environment variable holding the username for `--spec-auth-scheme basic`. |
| `--spec-password-env` | *(none)* | no | Environment variable holding the password for `--spec-auth-scheme basic`. |
| `--spec-key-env` | *(none)* | no | Environment variable holding the API key for `--spec-auth-scheme apiKey`. |
| `--spec-header-name` | *(none)* | no | Header name the `apiKey` scheme sends the key in. |
| `--spec-token-url` | *(none)* | no | OAuth2 token endpoint for `--spec-auth-scheme oauth2-client-credentials`. |
| `--spec-client-id-env` | *(none)* | no | Environment variable holding the OAuth2 client ID. |
| `--spec-client-secret-env` | *(none)* | no | Environment variable holding the OAuth2 client secret. |
| `--dry-run-output` | stdout | no | Write the dry-run summary to a file. JSON is used when the path ends in `.json`; otherwise plain text is used. The path must be relative to the current working directory. |
| `--generate-config` | `false` | no | Emit a starter `generator.yaml` into the output directory. Can be combined with `--dry-run`. |
| `--provider-name` | *(spec title)* | no | Provider name for the starter config when used with `--generate-config`. |
| `--force` | `false` | no | Overwrite an existing `generator.yaml` when used with `--generate-config`, or overwrite generated provider files in write mode. |
| `--generate-terraform-tests` | `false` | no | Emit native `.tftest.hcl` suites in the output `tests/` directory. |

### Behavior

When `--generate-config` is set, Eidos writes a starter `generator.yaml` and
returns. If `--dry-run` is also set, it additionally prints a dry-run summary
that includes the path of the emitted config.

When `--dry-run` is set without `--generate-config`, Eidos runs the generator
in record mode and prints the list of files that would be created. The pipeline
does not modify the filesystem.

Omitting `--dry-run` runs the generator in write mode and writes the provider
files to `--output` (which is required for full generation). Write mode refuses
to overwrite existing files unless `--force` is supplied. Use `--dry-run` to
preview the generated provider layout first.

## `eidos generate-config`

Create a starter `generator.yaml` from a spec so you can review and edit it
before running `eidos generate`.

```bash
eidos generate-config --spec ./api.yaml --output ./generator.yaml --provider-name mycloud
```

### Flags

| Flag | Default | Required | Description |
|------|---------|----------|-------------|
| `--spec` | — | **yes** | Path to the OpenAPI spec file (JSON or YAML), or an http(s) URL to fetch. |
| `--output` | `generator.yaml` | no | Path to write the starter config. |
| `--provider-name` | `generated` | no | Provider short name used in the config. |
| `--force` | `false` | no | Overwrite the output file if it already exists. |

The command fails with a clear message if the output file exists and `--force`
is not set.

## `eidos api`

Start a lightweight HTTP server with a single validation endpoint.

```bash
eidos api --host 127.0.0.1 --port 8080
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--host` | `127.0.0.1` | Interface to bind to. |
| `--port` | `8080` | TCP port; must be between `1` and `65535`. |

### `POST /api/v1/validate`

Accepts an OpenAPI document (JSON or YAML) with an optional top-level
`config` string containing `generator.yaml` contents. Returns a JSON report:

| Field | Meaning |
|-------|---------|
| `valid` | `true` if no error-level diagnostics were produced. |
| `diagnostics` | Parse, validation, and generation messages. |
| `detected` | Counts of paths, schemas, operations, resources, data sources, actions, ephemeral resources, list resources, functions, security schemes, write/read-only/nullable attributes, pagination style, importable resources, and state upgraders. |
| `ir_preview` | A preview of the `ProviderIR` that would drive generation. |
| `suggested_config` | A starter `generator.yaml` derived from the spec. |

Example request body:

```json
{
  "openapi": "3.0.3",
  "info": { "title": "MyCloud", "version": "1.0.0" },
  "paths": {
    "/pets/{id}": {
      "get": {
        "operationId": "getPet",
        "responses": {
          "200": {
            "description": "ok",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "id": { "type": "string" },
                    "name": { "type": "string" }
                  }
                }
              }
            }
          }
        }
      }
    }
  },
  "config": "provider:\n  name: mycloud\n"
}
```

## `eidos mcp`

Start the Model Context Protocol server over stdio.

```bash
eidos mcp
```

The server advertises five tools that let an MCP host (or an LLM without
codebase access) drive the whole workflow: inspect what a spec yields, run the
generator, check generated schemas for framework validity, and preview the
effect of `generator.yaml` overrides.

### `eidos/generate-config`

Generate a starter `generator.yaml` from a spec.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `spec` | `string` or `object` | **yes** | OpenAPI spec as a JSON/YAML string or parsed object. |
| `format` | `string` | no | `yaml` (default) or `json`. |
| `include_comments` | `boolean` | no | Add a leading comment to the generated YAML. |
| `skip_operations` | `string[]` | no | Operation IDs or name patterns to omit from generated resources and data sources. |
| `include_operations` | `string[]` | no | Operation IDs or name patterns that must be present for a resource or data source to be generated; when empty, all operations are candidates. |

Returns an object with `config` (the generated `generator.yaml` contents) and
`diagnostics` (parse and generation messages).

### `eidos/inspect`

Parse a spec and report what eidos would generate, before generating anything.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `spec` | `string` | **yes** | OpenAPI spec as a JSON/YAML string. |
| `config` | `string` | no | Optional `generator.yaml` contents. |

Returns `valid`, `diagnostics`, and per-entity summaries: `resources` (each with
its `create`/`read`/`update`/`delete` operation mapping and `wired` status),
`data_sources`, `actions`, `ephemeral_resources`, `list_resources`, and
`functions`. Use this to decide what is provisionable — and which operations
need an override — before authoring a config.

### `eidos/generate`

Run the full generation pipeline and return a manifest.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `spec` | `string` | **yes** | OpenAPI spec as a JSON/YAML string. |
| `config` | `string` | no | Optional `generator.yaml` contents. |
| `output` | `string` | no | Optional directory to write the generated provider to. |

Returns `valid`, `diagnostics`, the generated `resources`/`data_sources`/
`actions` summaries, and `file_count` (`output_dir` when `output` was set). When
`output` is supplied, the provider files are written to that directory (the
server runs locally over stdio, so local writes are allowed).

### `eidos/validate-schemas`

Report which generated schemas terraform-plugin-framework would reject, without
a Terraform round-trip.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `spec` | `string` | **yes** | OpenAPI spec as a JSON/YAML string. |
| `config` | `string` | no | Optional `generator.yaml` contents. |

Returns `valid`, `diagnostics`, and an `issues` list covering the classes the
framework rejects: dynamic-element collections, a `DynamicAttribute` nested
inside a `NestedAttributeObject`, invalid attribute names, `Computed`+`Required`
flags, and reserved root names.

### `eidos/override-preview`

Given a spec and a `generator.yaml`, return the IR preview *after* overrides
plus a per-entry report of which `resource_overrides` matched.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `spec` | `string` | **yes** | OpenAPI spec as a JSON/YAML string. |
| `config` | `string` | **yes** | `generator.yaml` contents. |

Returns `valid`, `diagnostics`, the resulting `resources`, and `overrides` — one
entry per override with `matched` and a `note` explaining any that had no effect
(so a silent no-op, e.g. an override whose operation is not present, is visible).

## `generator.yaml` reference

`generator.yaml` controls how Eidos maps an OpenAPI spec to a Terraform
provider.

### Top-level fields

```yaml
provider:                 ProviderConfig
servers:                []ServerConfig
resource_overrides:     []ResourceOverride
datasource_overrides:   []DatasourceOverride
action_overrides:       []ActionOverride
ephemeral_resource_overrides: []EphemeralOverride
list_resource_overrides: []ListResourceOverride
function_overrides:     []FunctionOverride
logging:                LoggingConfig
auth:                   []AuthConfig
security:               SecurityConfig
naming:                 NamingConfig
skip_operations:        []string
include_operations:    []string
global_timeouts:        TimeoutConfig
pagination:             PaginationConfig
polymorphism:           PolymorphismConfig
generate_terraform_tests: bool
generation:             GenerationConfig
spec:                   SpecConfig
```

### `provider`

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | **Required.** Short provider name, e.g. `mycloud`. |
| `display_name` | string | Human-readable provider name. |
| `version` | string | **Required.** Provider version, e.g. `0.1.0`. |
| `description` | string | Provider description. |
| `author` | string | Author or organization name. |
| `contact_email` | string | Contact email. |
| `license` | string | License identifier. |
| `repository` | string | Provider repository URL. |
| `protocol_version` | int | `5` or `6`. Defaults to `6` when generated. |

### `servers`

```yaml
servers:
  - url: "https://api.mycloud.example/v1"
    description: Production
    variables:
      region:
        default: us-east-1
        enum: [us-east-1, eu-west-1]
```

| Field | Type | Description |
|-------|------|-------------|
| `url` | string | Server URL template. |
| `description` | string | Human-readable description. |
| `variables` | map | Per-template-variable defaults and allowed values. |

### `resource_overrides`

```yaml
resource_overrides:
  - schema: Pet
    operation: createPet
    resource_name: pet
    datasource_name: pet
    id_attribute: id
    import_format: "{id}"
    force_new: [species]
    computed_attributes: [created_at]
    sensitive_attributes: [api_secret]
    write_only_attributes:
      - name: password
        sensitive: true
    skip: false
    generate_datasource: true
    generate_resource: true
    # Promote an action to a managed resource and wire it to explicit
    # operations whose create path differs from the read/delete path (e.g.
    # MyCloud dashboards: create via POST /dashboards/db, read/delete via
    # /dashboards/uid/{uid}).
    create_operation: postDashboard
    read_operation: getDashboardByUID
    update_operation: postDashboard
    delete_operation: deleteDashboardByUID
    schema_version: 1
    state_upgrades:
      - from: 0
        renames:
          old_name: new_name
```

| Field | Type | Description |
|-------|------|-------------|
| `schema` | string | Schema name to match. |
| `operation` | string | OpenAPI operationId to match. |
| `resource_name` | string | Generated resource name. |
| `datasource_name` | string | Generated data source name. |
| `id_attribute` | string | Name of the ID attribute. |
| `import_format` | string | Import string format. |
| `timeouts` | TimeoutConfig | Per-resource CRUD timeouts. |
| `force_new` | []string | Attributes that trigger replacement on change. |
| `computed_attributes` | []string | Attributes forced to `Computed`. |
| `sensitive_attributes` | []string | Attributes forced to `Sensitive`. |
| `write_only_attributes` | []WriteOnlyAttribute | Write-only arguments. |
| `skip` | bool | Skip this resource entirely. |
| `generate_datasource` | bool | Emit a matching data source. |
| `generate_resource` | bool | Emit the managed resource. When combined with the per-CRUD operation fields, promotes an operation that inference classified as an action into a wired managed resource. |
| `create_operation` | string | OpenAPI operationId for Create. |
| `read_operation` | string | OpenAPI operationId for Read. |
| `update_operation` | string | OpenAPI operationId for Update (optional). |
| `delete_operation` | string | OpenAPI operationId for Delete. |
| `schema_version` | int | Schema version for state upgrades. |
| `state_upgrades` | []StateUpgradeConfig | State migrations. |

Either `schema` or `operation` is required. The per-CRUD operation fields
(`create_operation`/`read_operation`/`update_operation`/`delete_operation`) let
an override manage an entity whose create path does not match its read/delete
path — for example MyCloud dashboards (create on `POST /dashboards/db`, read/
delete on `/dashboards/uid/{uid}`). Without them, a promoted resource would have
its create wired but no read/delete (staying scaffolded).

### `datasource_overrides`

```yaml
datasource_overrides:
  - operation: getPet
    datasource_name: pet
```

| Field | Type | Description |
|-------|------|-------------|
| `operation` | string | OpenAPI operationId to match. |
| `name` | string | Data source name/type name to match (alternative to `operation`). |
| `datasource_name` | string | Generated data source name. |

Either `operation` or `name` is required to match a data source.

### `action_overrides`

```yaml
action_overrides:
  - operation: rebootServer
    name: reboot_server
    description: Reboots a server.
    progress_messages: true
    modify_plan: false
    modify_plan_operation: POST /servers/{server_id}/reboot/preview
    validate_config_operation: POST /servers/{server_id}/reboot/validate
```

| Field | Type | Description |
|-------|------|-------------|
| `operation` | string | **Required.** OpenAPI operationId. |
| `name` | string | Generated action name. |
| `description` | string | Action description. |
| `progress_messages` | bool | Stream progress messages during invocation. |
| `modify_plan` | bool | Generate a `ModifyPlan` method. |
| `modify_plan_operation` | string | `"METHOD /path"` preflight endpoint. When set, `ModifyPlan` is generated and wired to call it (a non-success status becomes an error diagnostic; the plan is left unchanged on success — the spec encodes no plan mutations). |
| `validate_config_operation` | string | `"METHOD /path"` server-side validation endpoint. When set, `ValidateConfig` is wired to call it (a non-success status becomes an error diagnostic). |

### `ephemeral_resource_overrides`

```yaml
ephemeral_resource_overrides:
  - operation: generateTemporaryCredentials
    name: temporary_credential
    open_mapping: "POST /credentials/temporary"
    close_mapping: "DELETE /credentials/temporary/{credentialId}"
    renew_mapping: "POST /credentials/temporary/{credentialId}/renew"
    result_fields:
      - name: access_key_id
        type: string
        sensitive: true
```

| Field | Type | Description |
|-------|------|-------------|
| `operation` | string | **Required.** OpenAPI operationId. |
| `name` | string | Generated ephemeral resource name. |
| `description` | string | Description. |
| `open_mapping` | string | `METHOD /path` for the Open lifecycle call. |
| `close_mapping` | string | `METHOD /path` for the Close lifecycle call. |
| `renew_mapping` | string | `METHOD /path` for the Renew lifecycle call. |
| `result_fields` | []ResultField | Output fields exposed by the ephemeral resource. |

### `list_resource_overrides`

```yaml
list_resource_overrides:
  - resource: pet
    operation: listPets
    config_schema:
      - name: status
        type: string
        optional: true
    pagination:
      style: offset
      page_param: page
      per_page_param: limit
```

| Field | Type | Description |
|-------|------|-------------|
| `resource` | string | **Required.** Type name of the matching managed resource. |
| `operation` | string | OpenAPI operationId for the list call. |
| `config_schema` | []ListConfigSchema | Filter/search arguments. |
| `pagination` | PaginationConfig | Pagination behavior for this list resource. |

### `function_overrides`

```yaml
function_overrides:
  - operation: ipLookup
    name: ip_lookup
    type: lookup
    arguments:
      - name: ip
        type: string
    return_type: string
```

| Field | Type | Description |
|-------|------|-------------|
| `operation` | string | **Required.** OpenAPI operationId. |
| `name` | string | Generated function name. |
| `type` | string | Function category. |
| `arguments` | []FunctionArgument | Input arguments. |
| `return_type` | string | Return type. |

### `logging`

```yaml
logging:
  enabled: true
  file_path: /tmp/eidos-provider.log
  capture_request_headers: true
  capture_request_body: false
  capture_response_headers: true
  capture_response_body: false
  max_body_bytes: 4096
  redact_headers:
    - Authorization
    - X-API-Key
    - Cookie
```

These settings are baked into the generated provider as defaults for the
practitioner-facing `log_*` provider attributes (`log_file`,
`log_capture_request_headers`, `log_capture_request_body`,
`log_capture_response_headers`, `log_capture_response_body`,
`log_max_body_bytes`), which every generated provider schema exposes as
`Optional`. At `Configure` time the practitioner attributes override the baked
defaults; when a log file is configured (via `log_file` or a baked
`enabled` + `file_path`), the provider attaches the generated client's HTTP
trace round-tripper via `client.WithLogging`. `enabled` without `file_path`
has no effect — logging is active iff a log file path is set.

### `auth`

```yaml
auth:
  - scheme: apiKey
    header_name: X-API-Key
    env_var: MYCLOUD_API_KEY
  - scheme: oauth2
    flow: client_credentials
    token_url: https://api.mycloud.example/oauth/token
    client_id_env: MYCLOUD_CLIENT_ID
    client_secret_env: MYCLOUD_CLIENT_SECRET
```

| Field | Type | Description |
|-------|------|-------------|
| `scheme` | string | **Required.** One of `apiKey`, `oauth2`, `basic`, `bearer`. |
| `header_name` | string | Header name for `apiKey`. |
| `env_var` | string | Environment variable for credentials. |
| `flow` | string | OAuth2 flow name. |
| `client_id_env` | string | Client ID environment variable. |
| `client_secret_env` | string | Client secret environment variable. |
| `token_url` | string | Token endpoint URL. |
| `discovery_url` | string | OIDC discovery URL (overrides the spec's `openIdConnectUrl`). |

Generated providers wire interceptors for `apiKey`, HTTP `basic`/`bearer`,
OAuth2 `client_credentials` and `password` grants, OAuth2 `authorization_code`
(refresh-only: the practitioner supplies a `refresh_token` obtained out-of-band
and the provider refreshes it via `token_url`, handling rotation), and OpenID
Connect (discovery from the spec's `openIdConnectUrl`, or an `oidc_token_url`
override that skips discovery, then a client-credentials token fetch). The
OAuth2 `implicit` flow is intentionally not wired — it requires an interactive
browser redirect and is deprecated in OAuth 2.1 — and emits a runtime warning
when configured.

### `security`

```yaml
security:
  scheme: apiKey
```

| Field | Type | Description |
|-------|------|-------------|
| `scheme` | string | Name of a declared security scheme to use when the spec declares multiple global security requirements (OpenAPI OR: any one suffices). When unset, eidos applies every declared scheme (AND) and emits a warning. |

### `naming`

```yaml
naming:
  resource_prefix: mycloud_
  datasource_prefix: mycloud_
  resource_suffix: ""
  transform: snake_case
```

Allowed `transform` values: `snake_case` (default), `camelCase`, `PascalCase`.

### `skip_operations` / `include_operations`

```yaml
skip_operations:
  - deleteAdminPet
  - "OPTIONS*"
include_operations:
  - getPet
```

| Field | Type | Description |
|-------|------|-------------|
| `skip_operations` | []string | Operation IDs to exclude from generation. |
| `include_operations` | []string | Operation IDs to include; when non-empty, only matching operations are kept. |

Patterns use glob-style wildcards (`*` matches zero or more characters, `?`
matches exactly one) and are matched case-sensitively against the operation ID.

### `global_timeouts`

```yaml
global_timeouts:
  create: 20m
  read: 10m
  update: 20m
  delete: 10m
```

Durations are parsed by Go's `time.ParseDuration`, e.g. `30s`, `5m`, `1h`.

### `pagination`

```yaml
pagination:
  style: offset
  page_param: page
  per_page_param: per_page
  total_count_header: X-Total-Count
  next_link_header: Link
  cursor_field: next_cursor
```

Allowed `style` values: `offset`, `cursor`, `link_header`, `none`.

### `polymorphism`

```yaml
polymorphism:
  strategy: dynamic_union
  oneOf:
    - schema: Pet
      variants:
        - schema: Cat
        - schema: Dog
```

Allowed `strategy` values: `dynamic_union`, `split_resources`. When no strategy
is configured, a top-level `oneOf` whose variants are named object schemas
splits by default (the named-object-variants heuristic); everything else
defaults to `dynamic_union`. With `dynamic_union`, a discriminated union
renders as a `SingleNestedAttribute` merging all variant fields plus the
discriminator attribute (identical shared fields are deduped), guarded by a
`DiscriminatorValidator`; a union without a discriminator renders as a
`DynamicAttribute`. With `split_resources`, each variant becomes its own
resource sharing the original CRUD mapping; declare a `resource_name` or
`datasource_name` per variant to control the emitted names. Nested
`oneOf`/`anyOf` compositions (inside object properties or collection
elements) render as Dynamic attributes and emit a warning.

### `generation`

```yaml
generation:
  resources:
    include: [pet, server]
    exclude: ["admin_*"]
  datasources:
    include: []
  skip_tests: false
  skip_docs: false
```

| Field | Type | Description |
|-------|------|-------------|
| `resources` | ResourceGenerationConfig | Include/exclude patterns and package splitting for managed resources. |
| `datasources` | ResourceGenerationConfig | Same for data sources. |
| `actions` | ResourceGenerationConfig | Same for actions. |
| `ephemeral_resources` | ResourceGenerationConfig | Same for ephemeral resources. |
| `list_resources` | ResourceGenerationConfig | Same for list resources. |
| `functions` | ResourceGenerationConfig | Same for functions. |
| `skip_tests` | bool | Skip generating test files. |
| `skip_docs` | bool | Skip generating documentation. |

Each `ResourceGenerationConfig`:

| Field | Type | Description |
|-------|------|-------------|
| `include` | []string | Allow-list of construct name patterns; when non-empty, only matching constructs are retained. |
| `exclude` | []string | Deny-list of construct name patterns; matching constructs are dropped regardless of the allow-list. |
| `package` | string | Default sub-package for included constructs. |
| `packages` | []PackageRuleConfig | Per-pattern package overrides. |

### `spec`

```yaml
spec:
  path: https://vendor.example/api/openapi.json
  auth:
    scheme: bearer
    token_env: VENDOR_SPEC_TOKEN
```

| Field | Type | Description |
|-------|------|-------------|
| `path` | string | Path or http(s) URL of the OpenAPI spec. |
| `format` | string | Spec format hint (`yaml` or `json`). |
| `auth` | SpecAuthConfig | Authentication for a remote spec fetch (same fields as the `--spec-auth-*` CLI flags). |

## Generated provider layout

The generated provider is a normal Go module with a standard Terraform provider
structure.

```text
<output-dir>/
├── main.go                         # provider server entrypoint
├── go.mod
├── GNUmakefile
├── .goreleaser.yml
├── .github/workflows/release.yml
├── terraform-registry-manifest.json
├── README.md
├── generator.yaml                  # when config collection is enabled
├── internal/
│   ├── provider/
│   │   ├── provider.go             # provider schema and registration
│   │   ├── provider_test.go
│   │   ├── resource_<name>.go      # managed resources
│   │   ├── resource_<name>_test.go
│   │   ├── resource_<name>_acceptance_test.go
│   │   ├── data_source_<name>.go
│   │   ├── data_source_<name>_test.go
│   │   ├── action_<name>.go
│   │   ├── ephemeral_<name>.go
│   │   ├── list_<name>.go
│   │   ├── function_<name>.go
│   │   ├── model_<name>.go
│   │   ├── json_convert.go         # when any resource, data source, or ephemeral resource is wired
│   │   └── validators.go
│   ├── client/
│   │   ├── client.go
│   │   ├── auth.go                 # when the spec declares security schemes
│   │   ├── models.go
│   │   ├── errors.go
│   │   ├── retry.go
│   │   ├── pagination.go
│   │   └── logging.go
│   └── protocol/
│       ├── value_mappers.go
│       └── value_mappers_test.go
├── docs/
│   ├── index.md
│   ├── resources/<name>.md
│   ├── data-sources/<name>.md
│   ├── actions/<name>.md
│   ├── ephemeral-resources/<name>.md
│   ├── list-resources/<name>.md
│   └── functions/<name>.md
├── examples/
│   ├── resources/<name>/resource.tf
│   ├── data-sources/<name>/data-source.tf
│   ├── actions/<name>/action.tf
│   └── ephemeral-resources/<name>/ephemeral-resource.tf
└── tests/                          # only with --generate-terraform-tests
    ├── <name>.tftest.hcl
    └── modules/<name>/main.tf
```

Only constructs present in the spec and enabled by flags generate files. For
example, actions are only created when `action_overrides` are declared or when
the transformer detects action-oriented operations.

## Dry-run output

Use `--dry-run` to inspect what Eidos would generate before writing any files.

```bash
eidos generate --spec ./api.yaml --config ./generator.yaml --dry-run
```

The text summary includes:

- Detected provider name and OpenAPI version.
- Counts of resources, data sources, actions, ephemeral resources, list
  resources, functions, security schemes, and write-only attributes.
- A deterministic list of files that would be written.
- Diagnostics from the pipeline.

Writing the summary to a file:

```bash
# JSON
eidos generate --spec ./api.yaml --dry-run --dry-run-output summary.json

# Plain text
eidos generate --spec ./api.yaml --dry-run --dry-run-output summary.txt
```

The JSON shape is stable and can be consumed by other tooling:

```json
{
  "provider_name": "mycloud",
  "spec": "test/specs/mycloud.yaml",
  "spec_version": "3.0",
  "counts": {
    "resources": 11,
    "data_sources": 17,
    "actions": 0,
    "ephemeral_resources": 0,
    "list_resources": 12,
    "functions": 0,
    "security_schemes": 1,
    "write_only_attributes": 0
  },
  "files": [
    { "path": "internal/provider/provider.go", "reason": "provider schema and registration" }
  ],
  "written": false,
  "diagnostics": []
}
```

`config_path` is present only when a `generator.yaml` overrides file was
supplied; `written` is `false` for a dry-run and `true` after a full generation
run.

## Examples

### Generate a starter config

```bash
eidos generate-config --spec ./mycloud.yaml --provider-name mycloud --output mycloud-generator.yaml
```

### Preview generation

```bash
eidos generate --spec ./mycloud.yaml --config ./mycloud-generator.yaml --dry-run
```

### Include Terraform test files

```bash
eidos generate --spec ./mycloud.yaml --config ./mycloud-generator.yaml --generate-terraform-tests --dry-run
```

### Run the validation API

```bash
# Terminal 1
eidos api --port 8080

# Terminal 2
curl -s -X POST http://127.0.0.1:8080/api/v1/validate \
  -H 'Content-Type: application/json' \
  -d @mycloud.json | jq .
```

### Use the MCP server

```bash
eidos mcp
```

Connect with any MCP host and invoke `eidos/generate-config` with a `spec`
argument.

## Current limitations

- Generated resource CRUD methods are wired to the generated API client when the resource has a complete create/read/delete operation mapping (and an update mapping for `Update`); the provider `Configure` method constructs the client and the optional `endpoint` provider attribute overrides the API base URL. Data-source `Read` bodies and action `Invoke` bodies wire on the same terms, ephemeral `Open` wires when its mapping is bodiless and resolvable (with `Renew`/`Close` wiring via ephemeral private state), and list resources wire when the list mapping resolves (streaming via the generated client). Action `ModifyPlan`/`ValidateConfig` wire only when an explicit `modify_plan_operation`/`validate_config_operation` mapping is declared in `generator.yaml` (never auto-inferred). Resources with incomplete mappings and provider-defined functions emit clear runtime diagnostics instead of placeholder TODO comments, but they are not wired to real remote API calls.
- Parser type-mismatch diagnostics are emitted for OpenAPI 3.0.x/3.1.x and for Swagger 2.0 scalar fields at every depth; any-value fields (default/example/const and the exclusive bounds) are preserved without warning.
- `uniqueItems: true` is honored with Terraform Set attributes/blocks for managed resources, data sources (array responses), ephemerals, and actions. The one exception is list resources: the experimental Terraform `list/schema` package has no Set types, so a list endpoint whose response array declares `uniqueItems: true` is downgraded to a List and surfaced with a `diagnostics.Warning` at transform time (the downgrade is not silent).
- Auth interceptors are generated for API key, HTTP basic/bearer, OAuth2 `client_credentials` and `password` grants, OAuth2 `authorization_code` (refresh-only — the initial code exchange is interactive and must happen out-of-band; the practitioner supplies a `refresh_token` and the provider refreshes it, handling rotation), and OpenID Connect (discovery, or an `oidc_token_url` override, then a client-credentials token fetch). The OAuth2 `implicit` flow has no interceptor (interactive redirect; deprecated in OAuth 2.1) and warns at runtime when configured.
- Wired create/update request bodies are encoded per the operation's selected media type (`transformer.RequestBodyKind`): JSON is the default (including JSON dialects ending in `+json`); primitive `in: formData` parameters are sent as `application/x-www-form-urlencoded`; a binary `formData` parameter (`type: string, format: binary`, including Swagger 2.0 `type: file`) selects `multipart/form-data` and is uploaded from the model field's file path; and `application/xml`/`text/xml` bodies are encoded with a best-effort element-per-field `mapToXML` (custom `xml` keyword names/attributes are out of scope). A media type the generator cannot encode (e.g. `application/octet-stream`) is surfaced with a fail-loud `Warning` and kept scaffolded. Swagger 2.0 `formData` parameters wire end to end: the v2 parser's request-body form schema is decomposed back into per-field parameters (`swagger-formdata` reference spec exercises both form-urlencoded and multipart).
- Polymorphism: top-level `oneOf`/`anyOf` reach the IR as unions (the `dynamic_union` strategy renders a discriminated union as a `SingleNestedAttribute` with a `DiscriminatorValidator`; `split_resources` replaces a top-level polymorphic resource with one resource per variant). Nested `oneOf`/`anyOf` (inside properties or collection elements) render as Dynamic attributes with a fail-loud `warnCompositionNotModeled` warning — the flat Terraform attribute model cannot represent alternatives. The OpenAPI `discriminator` is only a validator: create/update bodies use generic JSON↔model conversion and do not switch on the discriminator property when encoding/decoding a variant, so a discriminated union round-trip is generic JSON, not variant-aware. `EnumValues` is not rendered as a `stringvalidator` (the allowed-keys check is covered by `DiscriminatorValidator`).
- Security: when an operation declares more than one security requirement (any one suffices), eidos applies AND of all declared schemes and emits a warning — a non-interactive Terraform provider cannot reliably try/fallback across OR alternatives, and the warn-and-AND choice is stricter than OR and fail-loud. The OAuth2 `implicit` flow has no interceptor (interactive browser redirect; deprecated in OAuth 2.1) and `Configure` warns at runtime when configured.
- Terraform State Stores (experimental in Terraform 1.15+) are not generated; eidos tracks the feature and waits for GA.
- The generated mock server is intentionally a deterministic, single-resource lifecycle prover (hardcoded `example-id`, first-segment routes, no nested routes); the live acceptance tests (`testfixtures/live`, `TF_ACC=1`) run against a local deterministic mock server and never run in CI by design.
- nested `metadata` wrapper flattening is not implemented: the reference mycloud spec exposes the path parameters (`name`, `workspace`) as top-level properties instead. A future enhancement could extend `ManagedResourceSchema` to flatten a single level of nested `metadata` into top-level path-param attributes.
- Entities whose create path differs from their read/delete path (e.g. MyCloud dashboards: create on `POST /dashboards/db`, read/delete on `/dashboards/uid/{uid}`) are not inferred as managed resources. They are reachable via an explicit `resource_overrides` entry with `generate_resource: true` plus `create_operation`/`read_operation`/`update_operation`/`delete_operation` (see [§`resource_overrides`](#resource_overrides)). The remaining caveat is a read response that nests the id under a wrapper object (e.g. `dashboard.uid`) rather than at the top level.

See [`PROJECT_DESIGN.md`](PROJECT_DESIGN.md#11-implementation-status) for a feature-level implementation matrix of what is currently usable versus scaffolded, and
[`PROJECT_DESIGN.md`](PROJECT_DESIGN.md#23-remaining-gaps--accepted-limitations) §23 for the canonical register of remaining gaps and accepted limitations.

## Troubleshooting

### Dry-run output path rejected

`--dry-run-output` must be a relative path inside the current working directory.
Absolute paths or paths pointing outside the working directory are rejected to
keep previews self-contained.

### generate-config refuses to overwrite

Pass `--force` to replace an existing `generator.yaml`.

### Spec path resolution

A local `--spec` path is resolved to an absolute path before use. If the spec
file does not exist, the command returns a clear file-read error.

A `--spec` value of the form `https://...` (or `http://...` with
`--spec-allow-http`) is fetched over HTTP with the same hardening eidos applies
to untrusted network input:

- **Scheme allowlist** — https is required; `http` is an explicit opt-in via
  `--spec-allow-http`. Any other scheme is rejected.
- **SSRF guard** — the host must not be a private, loopback, or link-local IP
  (nor resolve to one); every redirect target is re-checked against the same
  rules. Set `EIDOS_SPEC_ALLOW_PRIVATE=1` to permit an initial private host for
  local development (redirect targets stay guarded).
- **Size/timeout bounds** — a 30-second timeout and a 10 MiB response cap.
- **No URL-embedded credentials** — `https://user:pass@host/...` is rejected;
  use the `--spec-auth-*` flags naming environment variables instead.
- **Credentials are never logged** — fetch errors redact URL userinfo and never
  include header values.

Optional authentication reuses the provider's auth schemes, with credentials
read from environment variables at fetch time:

```bash
export VENDOR_SPEC_TOKEN=...            # bearer
export VENDOR_SPEC_USER=... VENDOR_SPEC_PASS=...   # basic
export VENDOR_SPEC_KEY=...              # apiKey + --spec-header-name
export VENDOR_SPEC_CLIENT_ID=... VENDOR_SPEC_CLIENT_SECRET=...   # oauth2

eidos generate --spec https://vendor.example/api/openapi.json \
  --spec-auth-scheme bearer --spec-token-env VENDOR_SPEC_TOKEN --dry-run
```

The same `spec.auth` section can be declared in `generator.yaml`; CLI flags
override it:

```yaml
spec:
  path: https://vendor.example/api/openapi.json
  auth:
    scheme: bearer
    token_env: VENDOR_SPEC_TOKEN
```

A URL-fetched spec is not pinned across runs: two invocations against the same
URL can produce different output if the remote document changes. For
reproducible generation, commit the downloaded copy or use a versioned spec URL.
