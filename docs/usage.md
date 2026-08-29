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
  api             Start the Eidos HTTP API server
  completion      Generate the autocompletion script for the specified shell
  generate        Generate a Terraform provider from an OpenAPI spec
  generate-config Generate a starter generator.yaml from an OpenAPI spec
  help            Help about any command
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
| `--spec` | — | no* | Path to the OpenAPI spec file (JSON or YAML), or an http(s) URL to fetch. Optional when `--config` supplies `spec.path`. |
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
| `--no-use-put-as-create` | `false` | no | With `--generate-config`, record `use_put_as_create: false` (the kill-switch) in the starter config. By default the starter config records `use_put_as_create: true` (PUT-as-create inference on). |
| `--skip-build` | `false` | no | Omit the build/CI/release files (`GNUmakefile`, `.goreleaser.yml`, `.github/workflows/release.yml`, `terraform-registry-manifest.json`) from the output. Mirrors `generation.skip_build`; the flag wins when both are set. |
| `--only-build` | `false` | no | Emit only the build/CI/release files and nothing else (not even `go.mod`, `main.go`, or the provider/client packages). Mutually exclusive with `--skip-build`. Requires `--output` unless `--dry-run`. |
| `--dynamic-release` | `false` | no | Also generate `.github/workflows/regenerate-and-release.yml`: a manually-dispatched workflow that regenerates the provider from its spec and publishes a release using the eidos CI image. Mirrors `generation.dynamic_release.enabled`; the flag wins when both are set. |

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
| `--no-use-put-as-create` | `false` | no | Record `use_put_as_create: false` (the kill-switch) in the starter config. By default the starter config records `use_put_as_create: true` so an instance-path PUT with no collection POST is used as the Create (upsert). |
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
| `detected` | Counts of paths, schemas, operations, resources, data sources, actions, ephemeral resources, list resources, functions, security schemes, schemas using `oneOf`/`allOf`/`anyOf`, write/read-only/nullable attributes, pagination style, importable resources, and state upgraders, plus `generate_terraform_tests`, `logging_enabled`, and `polymorphism_strategy`. |
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

The server advertises seven tools that let an MCP host (or an LLM without
codebase access) drive the whole workflow: inspect what a spec yields, run the
generator, check generated schemas for framework validity, preview the effect of
`generator.yaml` overrides, look up operations and schemas, and propose
non-inferred CRUD groupings as ready-to-paste `resource_overrides`.

### `eidos/generate-config`

Generate a starter `generator.yaml` from a spec.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `spec` | `string` or `object` | **yes** | OpenAPI spec as a JSON/YAML string or parsed object. |
| `format` | `string` | no | `yaml` (default) or `json`. |
| `include_comments` | `boolean` | no | Add a leading comment to the generated YAML. |
| `skip_operations` | `string[]` | no | Operation IDs or name patterns to omit from generated resources and data sources. |
| `include_operations` | `string[]` | no | Operation IDs or name patterns that must be present for a resource or data source to be generated; when empty, all operations are candidates. |

Returns `valid` (`false` when the spec produced error-level diagnostics, in
which case `config` is empty), `config` (the generated `generator.yaml`
contents), and `diagnostics` (parse and generation messages).

### `eidos/inspect`

Parse a spec and report what eidos would generate, before generating anything.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `spec` | `string` | **yes** | OpenAPI spec as a JSON/YAML string. |
| `config` | `string` | no | Optional `generator.yaml` contents. |

Returns `valid`, `diagnostics`, per-entity summaries: `resources` (each with
its `create`/`read`/`update`/`delete` operation mapping and `wired` status),
`data_sources`, `actions`, `ephemeral_resources`, `list_resources`, and
`functions`, and an explicit `counts` block (`resources`/`data_sources`/
`actions`/`ephemeral_resources`/`list_resources`/`functions` plus a
`wired_resources`/`scaffolded_resources` split) so counts are surfaced reliably
rather than inferred from array lengths. Use this to decide what is
provisionable — and which operations need an override — before authoring a
config.

### `eidos/generate`

Run the full generation pipeline and return a manifest.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `spec` | `string` | **yes** | OpenAPI spec as a JSON/YAML string. |
| `config` | `string` | no | Optional `generator.yaml` contents. |
| `output` | `string` | no | Optional directory to write the generated provider to. |
| `dry_run` | `boolean` | no | Collect the planned file list without writing (forces record mode even when `output` is set). |
| `verify` | `boolean` | no | After writing, run `go mod tidy` + `go build ./...` in `output` and report success/failure. Requires `output`; needs the Go toolchain on PATH and network access to resolve provider dependencies. |

Returns `valid`, `diagnostics`, the generated `resources`/`data_sources`/
`actions` summaries, `file_count` (`output_dir` when `output` was set), and:

- `files` — one `FileSummary` per planned file (`path`, `reason`, `would_overwrite`). Populated in both dry-run and write modes; `would_overwrite` is only meaningful in dry-run (after a forced write the files already exist).
- `stale_files` — existing files in `output` not produced by this generation (sorted, forward-slash relative paths; `.git` and dotfiles skipped), so a caller can see what a write would leave behind. Empty when `output` is not set.
- `verify_ok` / `verify_output` — present when `verify` is set; `verify_ok` is false with the build output in `diagnostics` on failure.

When `output` is supplied (and `dry_run` is not), the provider files are written
to that directory (the server runs locally over stdio, so local writes are
allowed).

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

### `eidos/lookup`

Look up an OpenAPI operation by `operationId` (forward) and/or a schema by name
(reverse) over the raw spec. Overrides do not change operations or schemas, so
`config` is not accepted.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `spec` | `string` | **yes** | OpenAPI spec as a JSON/YAML string. |
| `operation_id` | `string` | no* | Operation ID to look up (forward direction). |
| `schema` | `string` | no* | Schema name (the `$ref` final segment) to look up (reverse direction). |

\* At least one of `operation_id` or `schema` is required; both may be set to
answer the two directions in one call.

Returns `valid`, `diagnostics`, and:

- `operation` — the forward answer for the looked-up operation: `path`, `method`, `path_params` (name/required/type), `request_body_schema`, `request_media_type`, `response_schema`, `response_envelope`. `null` (with a warning diagnostic) when the operationId is not found.
- `schema_usage` — every operation that accepts the schema as a request body (`role: "request"`) or returns it as a response (`role: "response"`), sorted deterministically by path then method. Empty (non-nil) when the schema is not used.

### `eidos/suggest-resources`

Propose CRUD groupings that resource inference dropped — a collection POST +
instance GET with no DELETE-method delete on the instance — as ready-to-paste
`resource_overrides` entries. It scans for a near-miss delete: a non-DELETE verb
operation on a sub-path of the instance (e.g. `POST /my/ships/{id}/scrap`,
operationId `scrap-ship`) or any operation whose operationId leads with a delete
verb (`scrap`, `retire`, `cancel`, …), and wires it as `delete_operation` with
`delete_via_action: true`. A `config` declaring the resource (inferred or via
`resource_overrides`) suppresses its suggestion.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `spec` | `string` | **yes** | OpenAPI spec as a JSON/YAML string. |
| `config` | `string` | no | Optional `generator.yaml` contents. Resources already declared here are excluded from suggestions, and `use_put_as_create` is honored. |

Returns `valid`, `diagnostics`, and `suggestions` — one per candidate group, each
with `resource_name`, `collection_path`, `instance_path`, `create_operation`/
`read_operation`/`update_operation`/`delete_operation`, `delete_via_action`,
`completeness` (`create+read`, `create+read+update`, `create+read+delete`, …),
`reason`, and `override_yaml` (a ready-to-paste `resource_overrides:` snippet with
`generate_resource: true` and the CRUD operation ids). Output is deterministic
(sorted by resource name).

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
client:                 ClientConfig
auth:                   []AuthConfig
security:               SecurityConfig
naming:                 NamingConfig
skip_operations:        []string
include_operations:    []string
global_timeouts:        TimeoutConfig
pagination:             PaginationConfig
polymorphism:           PolymorphismConfig
generate_terraform_tests: bool
use_put_as_create:      bool
generation:             GenerationConfig
spec:                   SpecConfig
```

`generate_terraform_tests` is honored via the `eidos generate
--generate-terraform-tests` flag. The config key is surfaced in the API
`detected` summary but does not yet control generation output.

`use_put_as_create` is **on by default** for auto-generated providers (see
[PUT-as-create (upsert) inference](#put-as-create-upsert-inference)). It is a
tri-state key: absent (or `true`) enables the inference, and `false` is the
global kill-switch that restores the legacy scaffold behavior. A freshly
generated starter config records `use_put_as_create: true` so the default is
self-documenting and round-trips through `eidos generate --config`.

### `provider`

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | **Required.** Short provider name, e.g. `mycloud`. |
| `display_name` | string | Human-readable provider name. |
| `version` | string | **Required.** Provider version, e.g. `0.1.0`. |
| `description` | string | Provider description. Overrides the spec's `info.description` when set. |
| `author` | string | Author or organization name. |
| `contact_email` | string | Contact email. |
| `license` | string | License identifier. |
| `repository` | string | Provider repository URL. |
| `protocol_version` | int | `5` or `6`. Validated when set and re-emitted into starter configs; the generated provider targets protocol 6 (changeable at runtime via the generated binary's `--protocol-version` flag), so this key does not currently change generated output. |

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
        path: password
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
| `import_format` | string | Import ID format. Brace-enclosed attributes with a delimiter for composite IDs, e.g. `{alias}` or `{project_id}:{resource_id}`. Required read query parameters that map to a user-settable attribute and are not already in the format are appended automatically (e.g. `{alias}` → `{alias}/{cluster_id}` when the read requires `clusterId`), with an Info diagnostic. |
| `timeouts` | TimeoutConfig | Per-resource CRUD timeouts. |
| `force_new` | []string | Attributes that trigger replacement on change. |
| `computed_attributes` | []string | Attributes forced to `Computed`. |
| `sensitive_attributes` | []string | Attributes forced to `Sensitive`. |
| `write_only_attributes` | []WriteOnlyAttribute | Write-only arguments. |
| `skip` | bool | Skip this resource entirely (the per-resource opt-out — drops the resource from generation). |
| `generate_datasource` | bool | Emit a matching data source. |
| `generate_resource` | bool | **Opt-in only.** `true` (with the per-CRUD operation fields) promotes an operation that inference classified as an action into a wired managed resource. `false` is silently ignored — it is **not** an opt-out; use `skip: true` to drop a resource. |
| `create_operation` | string | OpenAPI operationId for Create. |
| `read_operation` | string | OpenAPI operationId for Read. |
| `update_operation` | string | OpenAPI operationId for Update (optional). |
| `delete_operation` | string | OpenAPI operationId for Delete. |
| `schema_version` | int | Schema version for state upgrades. |
| `state_upgrades` | []StateUpgradeConfig | State migrations. |
| `description` | string | Override the resource's description (replaces the spec-derived description). |

Either `schema` or `operation` is required. The per-CRUD operation fields
(`create_operation`/`read_operation`/`update_operation`/`delete_operation`) let
an override manage an entity whose create path does not match its read/delete
path — for example MyCloud dashboards (create on `POST /dashboards/db`, read/
delete on `/dashboards/uid/{uid}`). Without them, a promoted resource would have
its create wired but no read/delete (staying scaffolded).

Each `write_only_attributes` entry:

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | The attribute's leaf name (snake_cased by the consumer). |
| `path` | string | Dotted location within the resource schema (e.g. `owner.password` for a nested attribute); for a top-level attribute `path` equals `name`. |
| `sensitive` | bool | Mark the attribute sensitive in the generated schema. |
| `description` | string | Override the attribute's description. |

Each `state_upgrades` entry supports, in addition to `from` and `renames`:

| Field | Type | Description |
|-------|------|-------------|
| `block_renames` | map | Prior → current block name renames. |
| `added_attributes` | []string | Attributes added in the current schema (null-initialized during upgrade). |
| `added_blocks` | []string | Blocks added in the current schema. |
| `removed_attributes` | []string | Prior attributes dropped from the current schema (kept in the prior schema so historical state decodes, then dropped during upgrade). |
| `removed_blocks` | []string | Prior blocks dropped from the current schema. |

### `datasource_overrides`

```yaml
datasource_overrides:
  - operation: getPet
    datasource_name: pet
```

| Field | Type | Description |
|-------|------|-------------|
| `operation` | string | OpenAPI operationId, or a `"METHOD /path"` form (e.g. `GET /snmp/throttle`) matched against the data source's read method and path. The method+path form disambiguates operations that share an operationId. |
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
| `operation` | string | **Required.** OpenAPI operationId, or a `"METHOD /path"` form (e.g. `PUT /snmp/throttle`) matched against the action's invoke method and path. The method+path form disambiguates operations that share an operationId. |
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

Each `config_schema` entry:

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | **Required.** Filter/search argument name. |
| `type` | string | Argument type (`string`, `integer`, `boolean`, …). |
| `optional` | bool | Whether the argument is optional. When omitted, an existing inferred argument keeps its spec-derived optionality; a brand-new argument defaults to Required. |
| `description` | string | Argument description. When omitted, an existing inferred argument keeps the spec's description. |

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

### `client`

```yaml
client:
  base_url_template: https://api.mycloud.example/v2
  user_agent: mycloud-terraform-provider/1.0.0
  timeout: 45s
  retry_max: 4
  retry_wait_min: 500ms
  retry_wait_max: 20s
```

These settings are baked into the generated HTTP client. Every field is
optional; an unset field falls back to the generator's default (the spec's
first server URL, `eidos-generated-client`, a 30s timeout, 3 retries with 1s/30s
backoff). Duration fields accept Go duration strings (`"30s"`, `"1m30s"`).

| Field | Type | Description |
|-------|------|-------------|
| `base_url_template` | string | Base URL for all generated requests. Defaults to the spec's first server URL. |
| `user_agent` | string | `User-Agent` header sent on every request. |
| `timeout` | duration | Per-request HTTP timeout. |
| `retry_max` | int | Maximum retry count for retryable requests. |
| `retry_wait_min` | duration | Minimum backoff between retries. |
| `retry_wait_max` | duration | Maximum backoff between retries. |

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

`transform` accepts only `snake_case` (the default). `camelCase` and `PascalCase` are rejected at validation time: inferred Terraform names are always normalized to snake_case, so no other transform is implemented (N-46).

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

Each `oneOf` entry may declare a `discriminator` with `property_name` (the
discriminator property in the payload) and `mapping` (discriminator value →
variant schema name). Each `variant` may carry its own `discriminator` plus
`resource_name`/`datasource_name` overrides.

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
  skip_build: false
```

| Field | Type | Description |
|-------|------|-------------|
| `resources` | ResourceGenerationConfig | Include/exclude patterns and package splitting for managed resources. |
| `datasources` | ResourceGenerationConfig | Same for data sources. |
| `actions` | ResourceGenerationConfig | Same for actions. |
| `ephemeral_resources` | ResourceGenerationConfig | Same for ephemeral resources. |
| `list_resources` | ResourceGenerationConfig | Same for list resources. |
| `functions` | ResourceGenerationConfig | Same for functions. |
| `skip_tests` | bool | Omit generated `*_test.go` and coverage test files from the output. |
| `skip_docs` | bool | Omit generated `docs/` Markdown from the output. |
| `skip_build` | bool | Omit the build/CI/release files (`GNUmakefile`, `.goreleaser.yml`, `.github/workflows/release.yml`, `terraform-registry-manifest.json`) from the output. The `--skip-build` flag is the CLI equivalent and wins when both are set. |
| `dynamic_release` | DynamicReleaseConfig | Opt into generating `.github/workflows/regenerate-and-release.yml`, a manually-dispatched workflow that regenerates the provider from its spec and publishes a release using the eidos CI image. Off when absent. The `--dynamic-release` flag is the CLI equivalent and wins when both are set. |

`DynamicReleaseConfig`:

| Field | Type | Description |
|-------|------|-------------|
| `enabled` | bool | Turn on generation of the regenerate-and-release workflow. |
| `image` | string | eidos CI image reference the workflow runs in (defaults to `ghcr.io/signalbreak-labs/eidos:latest` when empty). |
| `spec_path` | string | Path to the OpenAPI spec, relative to the provider repo root, that the workflow regenerates from (defaults to `spec.yaml` when empty). |

Each `ResourceGenerationConfig`:

| Field | Type | Description |
|-------|------|-------------|
| `include` | []string | Allow-list of construct name patterns; when non-empty, only matching constructs are retained. |
| `exclude` | []string | Deny-list of construct name patterns; matching constructs are dropped regardless of the allow-list. |
| `package` | string | Default sub-package for included constructs. *(Currently has no effect on generation output.)* |
| `packages` | []PackageRuleConfig | Per-pattern package overrides. *(Currently has no effect on generation output.)* |

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
| `format` | string | Detected OpenAPI version (`openapi2`, `openapi3`, or `openapi31`) recorded in starter configs; not consumed by generation. |
| `auth` | SpecAuthConfig | Authentication for a remote spec fetch (same fields as the `--spec-auth-*` CLI flags). |

### PUT-as-create (upsert) inference

Many APIs expose *upsert* resources — a `PUT` on an item path
(`PUT /alarms/{alarmId}`) with no collection `POST`. Eidos infers a managed
resource's Create mapping from a collection `POST` by default, so such resources
would otherwise stay permanently scaffolded. When `use_put_as_create` is on
(the default), Eidos instead uses the instance-path `PUT` as the resource's
Create mapping (an upsert) whenever a CRUD group has no collection `POST` but
its instance path has `PUT` (plus `GET`/`DELETE`). The same `PUT` remains the
Update mapping — Create and Update both issue the upsert, which is correct.
A collection `POST` still wins when present; groups missing `GET` or `DELETE`
stay scaffolded as before.

Because the practitioner must supply the ID in the request URI for a PUT create,
the inference forces the resource's identifier attribute to **Required**
(user-settable) so the wired Create body fills the path placeholder with a real
value rather than a null, Computed id.

When the inference fires, Eidos emits an `Info` diagnostic naming the resource
and the PUT path, e.g. `using PUT /alarms/{alarmId} as Create (upsert)` — an
inferred Create is a load-bearing assumption the user should see (fail loud,
never silently).

**Escape hatches:**

- `use_put_as_create: false` — global kill-switch that restores the legacy
  scaffold behavior for every resource at once.
- `skip: true` on the resource (`resource_overrides`) — per-resource opt-out
  that drops the resource entirely:

  ```yaml
  resource_overrides:
    - operation: "PUT /alarms/{alarmId}"
      skip: true
  ```

  `generate_resource: false` is **not** an opt-out — `generate_resource` is
  opt-in only, so `false` is silently ignored. Use `skip: true` to drop a
  resource.

`eidos generate-config` records `use_put_as_create: true` in the starter
config by default (so the default is self-documenting and round-trips through
`eidos generate --config`); pass `--no-use-put-as-create` to record the
kill-switch (`use_put_as_create: false`) instead.

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
├── generator.yaml                  # written in write mode
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

## Build/release files

A full generation run emits four build/CI/release scaffolding files alongside
the provider code: `GNUmakefile`, `.goreleaser.yml`,
`.github/workflows/release.yml`, and `terraform-registry-manifest.json`. They
are templated from the provider name and release settings (not the spec's
constructs), so they are independent of the generated provider code.

### Omitting them — `--skip-build` / `skip_build`

To regenerate a provider without touching hand-managed release scaffolding
(for example, in a CI job that regenerates the provider into a fresh directory),
drop the four files with `--skip-build` or `generation.skip_build: true`:

```bash
eidos generate --spec ./api.yaml --output ./provider --skip-build
```

```yaml
generation:
  skip_build: true
```

### Generating only them — `--only-build`

`--only-build` inverts the selection: it emits exactly the four scaffolding files
and nothing else — not even `go.mod`, `main.go`, or the provider/client packages.
This supports a workflow where the provider code is regenerated dynamically in
CI and never stored in git, while the release scaffolding is checked in once and
managed separately:

```bash
# Once: generate and commit just the release scaffolding.
eidos generate --spec ./api.yaml --only-build --output . --force

# In CI: regenerate the full provider (no build files) into a throwaway dir.
eidos generate --spec ./api.yaml --skip-build --output ./provider
```

`--only-build` and `--skip-build` are mutually exclusive. Like the normal write
path, `--only-build` requires `--output` unless `--dry-run` is set.

## CI image

Each eidos release publishes a container image to GHCR that bundles eidos, the
Go toolchain, and GoReleaser:

```text
ghcr.io/signalbreak-labs/eidos:<tag>     # e.g. ghcr.io/signalbreak-labs/eidos:v0.4.0
ghcr.io/signalbreak-labs/eidos:latest
```

It is a "generate-and-build" image: a CI job can run one container to regenerate
a provider from a spec and publish it, without installing Go, GoReleaser, or
eidos on the runner. The image is for the eidos *generator* (the tool). Generated
providers are still normal Go modules that GoReleaser cross-compiles into
Terraform plugin binaries via their own generated `.goreleaser.yml` — the image
supplies the tools to drive that pipeline.

### Dynamic regenerate-and-publish

The intended workflow pairs `--only-build` (commit the release scaffolding once)
with the image (regenerate and publish on every CI run, no provider code in git):

```yaml
# .github/workflows/release.yml in the provider repo
jobs:
  release:
    runs-on: ubuntu-latest
    permissions:
      contents: write
    container:
      image: ghcr.io/signalbreak-labs/eidos:v0.4.0
    steps:
      - uses: actions/checkout@v4
      # Regenerate the provider from the spec; --skip-build keeps the committed
      # scaffolding (GNUmakefile, .goreleaser.yml, release workflow, manifest).
      - run: eidos generate --spec ./api.yaml --skip-build --output .
      - run: go test ./...
      # Publish via the generated GoReleaser config.
      - run: goreleaser release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

Run `eidos generate --spec ./api.yaml --only-build --output . --force` once and
commit the four scaffolding files; the CI job above regenerates everything else
on each release. The image carries Go, GoReleaser, `make`, and `eidos` on `PATH`.

### Generated regenerate-and-release workflow

Instead of hand-writing the job above, ask eidos to generate it into the provider
with `--dynamic-release` (or `generation.dynamic_release.enabled: true`):

```bash
eidos generate --spec ./api.yaml --output . --force --dynamic-release
```

This emits `.github/workflows/regenerate-and-release.yml` alongside the static
`release.yml`. The two coexist with non-overlapping triggers:

- `release.yml` fires on a `v*` tag push and builds *committed* code.
- `regenerate-and-release.yml` is manually dispatched (`workflow_dispatch` with a
  `version` input): it regenerates the provider from the spec with
  `eidos generate --skip-build`, builds, tests, commits to a `release/<version>`
  branch, tags, and runs `goreleaser release --clean` inside the CI image.

Because the tag is created with the default `GITHUB_TOKEN`, it does not re-trigger
`release.yml`, so the workflows never double-fire. The regenerated code lands on a
release-specific branch, keeping the default branch to just the spec and the
committed build scaffolding. Configure the image and spec path:

```yaml
generation:
  dynamic_release:
    enabled: true
    image: ghcr.io/signalbreak-labs/eidos:latest
    spec_path: spec.yaml
```

The workflow is opt-in and off by default, so generation output (and golden
snapshots) are unchanged unless you turn it on.

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

- Relative file `$ref` values are resolved only when the entry spec is a local CLI or MCP file input. Nested refs are relative to the file containing them and may cross JSON/YAML documents. Inline/API bodies and remotely fetched entry specs have no trusted local base, and HTTP(S) or absolute `$ref` targets are never fetched; these cases emit error diagnostics. Local resolution is capped at 100 documents, 50 MiB total, and 100 reference levels.
- Generated resource CRUD methods are wired to the generated API client when the resource has a complete create/read/delete operation mapping (and an update mapping for `Update`); the provider `Configure` method constructs the client and the optional `endpoint` provider attribute overrides the API base URL. Data-source `Read` bodies and action `Invoke` bodies wire on the same terms, ephemeral `Open` wires when its mapping is bodiless and resolvable (with `Renew`/`Close` wiring via ephemeral private state), and list resources wire when the list mapping resolves (streaming via the generated client). Action `ModifyPlan`/`ValidateConfig` wire only when an explicit `modify_plan_operation`/`validate_config_operation` mapping is declared in `generator.yaml` (never auto-inferred). Resources with incomplete mappings and provider-defined functions emit clear runtime diagnostics instead of placeholder TODO comments, but they are not wired to real remote API calls.
- Parser type-mismatch diagnostics are emitted for OpenAPI 3.0.x/3.1.x and for Swagger 2.0 scalar fields at every depth; any-value fields (default/example/const and the exclusive bounds) are preserved without warning.
- `uniqueItems: true` is honored with Terraform Set attributes/blocks for managed resources, data sources (array responses), ephemerals, and actions. The one exception is list resources: the experimental Terraform `list/schema` package has no Set types, so a list endpoint whose response array declares `uniqueItems: true` is downgraded to a List and surfaced with a `diagnostics.Warning` at transform time (the downgrade is not silent).
- JSON Schema constraints declared on a spec's schema attributes (enum, const, minLength/maxLength, pattern, minimum/maximum, minItems/maxItems, enum-constrained string collection elements) are carried end to end and emitted as standard `terraform-plugin-framework-validators` calls on managed-resource and provider-config attributes (e.g. `stringvalidator.OneOf`, `int64validator.Between`, `listvalidator.ValueStringsAre`). A declared bound of `0` is preserved (`minimum: 0` forbids negative values). Data sources, actions, ephemeral resources, list resources, and functions carry the constraints in the IR but do not yet emit validators; wiring them is follow-up work (see `PROJECT_DESIGN.md` §23.2).
- Auth interceptors are generated for API key, HTTP basic/bearer, OAuth2 `client_credentials` and `password` grants, OAuth2 `authorization_code` (refresh-only — the initial code exchange is interactive and must happen out-of-band; the practitioner supplies a `refresh_token` and the provider refreshes it, handling rotation), and OpenID Connect (discovery, or an `oidc_token_url` override, then a client-credentials token fetch). The OAuth2 `implicit` flow has no interceptor (interactive redirect; deprecated in OAuth 2.1) and warns at runtime when configured. A spec that declares more than one HTTP bearer scheme qualifies each scheme's provider attribute with the scheme name (`account_token`, `agent_token`, …) so distinct tokens can be set per scheme; a single bearer scheme keeps the canonical `bearer_token`.
- Wired create/update request bodies are encoded per the operation's selected media type (`transformer.RequestBodyKind`): JSON is the default (including JSON dialects ending in `+json`); primitive `in: formData` parameters are sent as `application/x-www-form-urlencoded`; a binary `formData` parameter (`type: string, format: binary`, including Swagger 2.0 `type: file`) selects `multipart/form-data` and is uploaded from the model field's file path; and `application/xml`/`text/xml` bodies are encoded with a best-effort element-per-field `mapToXML` (custom `xml` keyword names/attributes are out of scope). A media type the generator cannot encode (e.g. `application/octet-stream`) is surfaced with a fail-loud `Warning` and kept scaffolded. Swagger 2.0 `formData` parameters wire end to end: the v2 parser's request-body form schema is decomposed back into per-field parameters (`swagger-formdata` reference spec exercises both form-urlencoded and multipart).
- Polymorphism: top-level `oneOf`/`anyOf` reach the IR as unions (the `dynamic_union` strategy renders a discriminated union as a `SingleNestedAttribute` with a `DiscriminatorValidator`; `split_resources` replaces a top-level polymorphic resource with one resource per variant). Nested `oneOf`/`anyOf` (inside properties or collection elements) render as Dynamic attributes with a fail-loud `warnCompositionNotModeled` warning — the flat Terraform attribute model cannot represent alternatives. The OpenAPI `discriminator` is only a validator: create/update bodies use generic JSON↔model conversion and do not switch on the discriminator property when encoding/decoding a variant, so a discriminated union round-trip is generic JSON, not variant-aware. `EnumValues` is not rendered as a `stringvalidator` (the allowed-keys check is covered by `DiscriminatorValidator`).
- Security: when an operation declares more than one security requirement (any one suffices), eidos applies AND of all declared schemes and emits a warning — a non-interactive Terraform provider cannot reliably try/fallback across OR alternatives, and the warn-and-AND choice is stricter than OR and fail-loud. The OAuth2 `implicit` flow has no interceptor (interactive browser redirect; deprecated in OAuth 2.1) and `Configure` warns at runtime when configured.
- Actions have no result surface: terraform-plugin-framework v1.19.0's `action.InvokeResponse` exposes only `Diagnostics` and `SendProgress` (no `Result` field), and `action/schema` attributes cannot be `Computed`, so a generated action that returns a value (e.g. an auth `register` action's token) reports success/failure but does not decode the response body. This is an upstream framework limitation, not a generator gap; no broken code is emitted.
- `generator.yaml` must not claim one operation in both `resource_overrides` and a non-resource override (`action_overrides`, `ephemeral_resource_overrides`, `list_resource_overrides`, or `function_overrides`): a resource already owns the operation, so the duplicate override is skipped with a fail-loud `Warning` naming the operation and its method+path (`Action override references an operation already claimed by a resource`, `Ephemeral resource override references …`, `List resource override references …`, `Function override references …`).
- Terraform State Stores (experimental in Terraform 1.15+) are not generated; eidos tracks the feature and waits for GA.
- The generated mock server is intentionally a deterministic, single-resource lifecycle prover (hardcoded `example-id`, first-segment routes, no nested routes); the live acceptance tests (`testfixtures/live`, `TF_ACC=1`) run against a local deterministic mock server and never run in CI by design.
- nested `metadata` wrapper flattening is not implemented: the reference mycloud spec exposes the path parameters (`name`, `workspace`) as top-level properties instead. A future enhancement could extend `ManagedResourceSchema` to flatten a single level of nested `metadata` into top-level path-param attributes.
- Entities whose create path differs from their read/delete path (e.g. MyCloud dashboards: create on `POST /dashboards/db`, read/delete on `/dashboards/uid/{uid}`) are not inferred as managed resources. They are reachable via an explicit `resource_overrides` entry with `generate_resource: true` plus `create_operation`/`read_operation`/`update_operation`/`delete_operation` (see [§`resource_overrides`](#resource_overrides)). The remaining caveat is a read response that nests the id under a wrapper object (e.g. `dashboard.uid`) rather than at the top level.
- `eidos/suggest-resources` groups from the raw spec, while the "already claimed" set comes from the config-aware IR preview. `skip_operations`/`include_operations` are applied only in the config-aware pass, so a near-miss delete the user dropped via `skip_operations` may still be proposed; applying the override then yields a resource with a scaffolded delete (the op resolves to nil on the filtered pathOps). The suggestion is advisory — the user judges it — and the dropped op is the contrived case where a user disables the very operation they would use as a delete.
- `eidos/suggest-resources` parses and transforms the spec twice (raw pathOps for grouping, plus the config-aware pass for the consumed set) because the config-aware pipeline does not expose raw pathOps. This is a cost on very large specs (e.g. Kubernetes), not a correctness issue.
- `eidos/generate` `verify` runs `go mod tidy` + `go build ./...` in `output` with a 5-minute timeout. It needs the Go toolchain on `PATH` and network access to resolve provider dependencies (terraform-plugin-framework et al.); without them, `verify_ok` is false with the build error in `diagnostics`.

See [`PROJECT_DESIGN.md`](PROJECT_DESIGN.md#11-implementation-status) for a feature-level implementation matrix of what is currently usable versus scaffolded, and
[`PROJECT_DESIGN.md`](PROJECT_DESIGN.md#23-remaining-gaps--accepted-limitations) §23 for the canonical register of remaining gaps and accepted limitations.

## Troubleshooting

### Dry-run output path rejected

`--dry-run-output` must be a relative path inside the current working directory.
Absolute paths or paths pointing outside the working directory are rejected to
keep previews self-contained.

### generate-config refuses to overwrite

Pass `--force` to replace an existing `generator.yaml`.

### Warning: required readOnly property cannot be both input and output-only

The spec lists a property in `required` and also marks it `readOnly`. A
readOnly property is not a practitioner input, so eidos cannot honor the
required constraint on the request body. The generated schema treats that
body field as Computed.

If the same name is also a required query or header parameter on create,
read, update, or delete, the Terraform attribute is Required so the generated
request can send it. Otherwise, fix the spec: drop the property from
`required`, or drop `readOnly` if practitioners must supply it.

### Duplicate operationIds fail generation

Two operations that share an `operationId` (or whose `operationId`s normalize
to the same construct name) produce the same resource/action/data source name,
which would make the generator emit two files at one path. Eidos fails loud
with an error diagnostic naming both source operations instead of surfacing a
confusing "duplicate output path" error.

Resolve the collision by renaming one `operationId` in the spec, or by adding a
`generator.yaml` override that matches the colliding operation by its
`METHOD /path` and renames it:

```yaml
action_overrides:
  - operation: "PUT /system/snmp/throttle"
    name: redefine_system_snmp_throttle_config
datasource_overrides:
  - operation: "GET /licensing/module/all/flat"
    datasource_name: get_all_cluster_licenses_flat
```

The `operation` field accepts either an OpenAPI operationId or a `"METHOD /path"`
form; the method+path form disambiguates operations that share an operationId,
which the operationId form cannot (it would match every operation carrying that
operationId).

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
