# Eidos — OpenAPI to Terraform Provider Generator

## Table of Contents

1. [Project Overview](#1-project-overview)
   - 1.1 [Implementation Status](#11-implementation-status)
2. [Problem Statement & Motivation](#2-problem-statement--motivation)
3. [Design Principles](#3-design-principles)
4. [High-Level Architecture](#4-high-level-architecture)
5. [OpenAPI Specification Coverage](#5-openapi-specification-coverage)
   - 5.1 [Swagger / OpenAPI 2.0](#51-swagger--openapi-20)
   - 5.2 [OpenAPI 3.0.x](#52-openapi-30x)
   - 5.3 [OpenAPI 3.1.x](#53-openapi-31x)
   - 5.4 [Feature Mapping Matrix](#54-feature-mapping-matrix)
6. [Intermediate Representation (IR)](#6-intermediate-representation-ir)
   - 6.1 [IR Type System](#61-ir-type-system)
   - 6.2 [Provider IR](#62-provider-ir)
   - 6.3 [Resource IR](#63-resource-ir)
   - 6.4 [Data Source IR](#64-data-source-ir)
   - 6.5 [Action IR](#65-action-ir)
   - 6.6 [Ephemeral Resource IR](#66-ephemeral-resource-ir)
   - 6.7 [List Resource IR](#67-list-resource-ir)
   - 6.8 [Schema IR](#68-schema-ir)
   - 6.9 [Security IR](#69-security-ir)
   - 6.10 [Operation IR](#610-operation-ir)
   - 6.11 [Why an IR?](#611-why-an-ir)
7. [Component Design](#7-component-design)
   - 7.1 [CLI (`cmd/eidos/`)](#71-cli)
     - 7.1.1 [Dry-Run Mode](#711-dry-run-mode)
     - 7.1.2 [Feature Validation Endpoint](#712-feature-validation-endpoint)
     - 7.1.3 [MCP Tool for Configuration Generation](#713-mcp-tool-for-configuration-generation)
     - 7.1.4 [Config File Generation from Detected Spec](#714-config-file-generation-from-detected-spec)
   - 7.2 [Parser (`pkg/parser/`)](#72-parser)
   - 7.3 [Normalizer (`pkg/normalizer/`)](#73-normalizer)
   - 7.4 [Transformer (`pkg/transform/`)](#74-transformer)
   - 7.5 [Code Generator (`pkg/generator/`)](#75-code-generator)
   - 7.6 [Protocol Layer Generator (`pkg/generator/protocol/`)](#76-protocol-layer-generator)
   - 7.7 [Documentation Generator (`pkg/generator/docs/`)](#77-documentation-generator)
   - 7.8 [HTTP Client Generator (`pkg/generator/client/`)](#78-http-client-generator)
   - 7.9 [Test Generator (`pkg/generator/test/` — Go package `testgen`)](#79-test-generator)
   - 7.10 [Support Packages](#710-support-packages)
8. [Terraform Plugin Framework Integration](#8-terraform-plugin-framework-integration)
   - 8.1 [Schema Generation](#81-schema-generation)
   - 8.2 [CRUD Mapping](#82-crud-mapping)
   - 8.3 [Plan Modifiers & Validators](#83-plan-modifiers--validators)
   - 8.4 [State Upgraders](#84-state-upgraders)
   - 8.5 [Import Support](#85-import-support)
   - 8.6 [Provider-Defined Functions](#86-provider-defined-functions)
   - 8.7 [Actions (Invoke Actions)](#87-actions-invoke-actions)
   - 8.8 [Ephemeral Resources](#88-ephemeral-resources)
   - 8.9 [List Resources (tfquery)](#89-list-resources-tfquery)
   - 8.10 [Write-Only Arguments on Managed Resources](#810-write-only-arguments-on-managed-resources)
9. [Terraform Plugin Protocol Details](#9-terraform-plugin-protocol-details)
   - 9.1 [Protocol Version 6 (Primary)](#91-protocol-version-6-primary)
   - 9.2 [Protocol Version 5 (Fallback)](#92-protocol-version-5-fallback)
   - 9.3 [gRPC Server Lifecycle](#93-grpc-server-lifecycle)
   - 9.4 [DynamicValue Serialization](#94-dynamicvalue-serialization)
10. [OpenAPI-to-Terraform Type Mapping](#10-openapi-to-terraform-type-mapping)
11. [Security Scheme Handling](#11-security-scheme-handling)
12. [Polymorphism & Complex Schemas](#12-polymorphism--complex-schemas)
13. [Error Handling & Diagnostics](#13-error-handling--diagnostics)
14. [Configuration & Overrides System](#14-configuration--overrides-system)
15. [Output Directory Structure](#15-output-directory-structure)
16. [Technology Stack](#16-technology-stack)
17. [Provider Development Workflow](#17-provider-development-workflow)
   - 17.1 [Build & Install](#171-build--install)
   - 17.2 [Version Injection](#172-version-injection)
   - 17.3 [Local Development Overrides](#173-local-development-overrides)
   - 17.4 [Provider Logging](#174-provider-logging)
   - 17.5 [Debugging with Delve](#175-debugging-with-delve)
   - 17.6 [go generate Integration](#176-go-generate-integration)
   - 17.7 [Multi-Platform Builds](#177-multi-platform-builds)
   - 17.8 [Provider Source Address](#178-provider-source-address)
   - 17.9 [Terraform Lifecycle & Meta-Arguments](#179-terraform-lifecycle--meta-arguments)
18. [Testing Strategy](#18-testing-strategy)
   - 18.1 [Unit Tests](#181-unit-tests)
   - 18.2 [Acceptance Tests](#182-acceptance-tests)
   - 18.3 [Protocol Compliance Tests](#183-protocol-compliance-tests)
   - 18.4 [End-to-End Integration Tests](#184-end-to-end-integration-tests)
   - 18.5 [Test Infrastructure](#185-test-infrastructure)
   - 18.6 [terraform test Framework](#186-terraform-test-framework)
19. [Terraform Registry Publishing](#19-terraform-registry-publishing)
20. [Roadmap](#20-roadmap)
21. [Risks & Mitigations](#21-risks--mitigations)
22. [Glossary](#22-glossary)
23. [Remaining Gaps & Accepted Limitations](#23-remaining-gaps--accepted-limitations)

---

## 1. Project Overview

**Eidos** is a code-generation toolchain that ingests **OpenAPI 2.0 (Swagger)**, **OpenAPI 3.0.x**, and **OpenAPI 3.1.x** specification files and emits a complete, production-ready **Terraform provider** — including all Go source code, the Terraform Plugin Protocol layer, an HTTP client, documentation, tests, and release tooling.

### Goals

- **Exhaustive spec coverage**: support every valid OpenAPI construct across all three major spec versions — paths, operations, schemas, security schemes, callbacks, links, webhooks, parameters, request bodies, responses, headers, examples, extensions, and more.
- **Self-contained output**: the generated provider includes all Go source code, the gRPC-based protocol layer (Protocol v6 / v5), a generated HTTP API client with auth middleware, registry-ready Markdown documentation, acceptance tests, CI/CD tooling, and support for the **full Terraform Plugin Framework surface** — Resources, Data Sources, Actions (invoke actions), Ephemeral Resources, List Resources (tfquery), and Provider-Defined Functions.
- **Minimal manual intervention**: users run a single CLI command and receive a provider ready for `go build`, `go test`, `goreleaser`, and the Terraform Registry.
- **Extensibility**: every generated decision can be overridden via a declarative configuration file; generated code is idiomatic Go that humans can read, extend, and maintain.
- **Idempotent generation**: running the generator twice with the same spec and config produces byte-identical output.

### Non-Goals

- Generating Terraform Cloud/Enterprise integration code.
- Generating provider SDKs in languages other than Go.
- Replacing hand-written providers where complex lifecycle management (e.g., create-or-update patterns, drift correction heuristics) is required beyond what the spec describes.
- Providing a runtime server that proxies Terraform calls to an API.

### 1.1 Implementation Status

Eidos is under active development. The architecture, intermediate representation, parser, transformer, and generator scaffolds are in place and the test suite is green. The following table distinguishes features that are currently usable from features that are defined in the design but not yet fully wired or functional.

| Capability | Status | Notes |
|------------|--------|-------|
| OpenAPI 2.0 / 3.0.x / 3.1 parsing | Implemented | All three versions are parsed. Scalar type-mismatch diagnostics are emitted for OpenAPI 3.0.x/3.1.x and for Swagger 2.0 scalar fields at every depth (response `$ref`/description, `collectionFormat`, `externalDocs` description/URL, `additionalProperties` boolean, and the schema string/bool keywords). Any-value fields (`default`/`example`/`const`/`exclusiveMaximum`/`exclusiveMinimum`) are preserved via `nodeToNative` without warning, matching the 3.x converter and avoiding false positives on legitimate array/object values. Unquoted `openapi`/`swagger` version values are preserved as strings by the lexer. |
| `$ref` resolution (local) | Implemented | Only local (same-document) JSON Pointer `$ref`s resolve. File and remote URL refs are rejected with a fail-loud error diagnostic rather than fetched (the remote-fetch `RefResolver` was removed from `pkg/parser/ref_external.go`). |
| IR normalization and transformation | Mostly implemented | Type mapping, CRUD inference, overrides, security mapping, and validator inference are functional. |
| `eidos generate --dry-run` | Implemented | Produces a file list and summary from the real parsed `ProviderIR`. |
| `eidos generate` (write mode) | Implemented | Writes generated files to `--output`, with overwrite guards controlled by `--force`. |
| `eidos generate-config` | Implemented | Emits a starter `generator.yaml` from a spec. |
| `eidos api` / `eidos mcp` | Implemented | Validation API and MCP `generate-config` tool are functional; the API server uses `MaxHeaderBytes`, access logging, and panic recovery. |
| Generated provider file list | Implemented | The generator records and writes the full set of files a provider needs. |
| Generated resource CRUD bodies | Partial | `Create`/`Read`/`Update`/`Delete` are wired to the generated API client when the resource has a complete create/read/delete operation mapping (update optional); the provider `Configure` method constructs the client and the optional `endpoint` provider attribute overrides the API base URL. Resources with incomplete mappings keep honest runtime diagnostics. Request/response mapping is generic JSON↔model conversion; query/header/cookie params are wired. Request bodies are encoded per the selected media type — JSON (default), `application/x-www-form-urlencoded` (primitive `formData`), `multipart/form-data` (binary `formData`, read from the model field's path), and `application/xml` (`mapToXML`, best-effort element-per-field) — via `transformer.RequestBodyKind`; unsupported media types stay honestly scaffolded. |
| Generated action / ephemeral / list / function bodies | Mostly wired | Ephemeral `Open` is wired, and `Renew`/`Close` wire when their mappings resolve (parameters are passed via ephemeral private state). Action `Invoke` is wired, and `ModifyPlan`/`ValidateConfig` wire when an explicit `modify_plan_operation`/`validate_config_operation` mapping is declared in `generator.yaml`. List resources wire when the list mapping resolves (identity from the paired instance path parameters or the item `id`), streaming results via the generated client with pagination. Provider-defined functions stay honestly scaffolded by design (no remote endpoint). Unresolvable mappings keep honest runtime diagnostics. |
| Provider-defined functions | Partially scaffolded | IR and config shapes exist; generated bodies emit runtime diagnostics instead of unconditional "not implemented" errors. |
| Construct include/exclude filtering | Implemented | `generator.yaml` `generation.*.include`/`exclude` patterns are applied to the provider IR before file generation. |
| Split-resource polymorphism | Implemented | Top-level `oneOf`/`anyOf` reach the IR as unions. `dynamic_union` renders a discriminated union as a `SingleNestedAttribute` merging variant fields plus the discriminator, with a `DiscriminatorValidator` (shared identical fields deduped). `split_resources` replaces a top-level polymorphic managed resource with one resource per variant (explicit config or the named-object-variants heuristic). Nested unions render as Dynamic attributes with a fail-loud warning; discriminated request/response payload switching stays out of scope. |
| `uniqueItems` → Set attributes | Implemented | `uniqueItems: true` is honored with `Set*` attributes/blocks for managed resources, data sources (array responses), ephemerals, and actions. The single remaining limitation is list resources: the experimental `list/schema` package has no Set types, so a list endpoint whose response array declares `uniqueItems: true` is downgraded to List and surfaced with a fail-loud `diagnostics.Warning` at transform time (the downgrade is not silent). |
| State upgrader generation | Implemented | State upgrades are generated for attribute renames and block-level changes (block renames, added/removed attributes and blocks). Removed fields are synthesized as Dynamic-typed prior attributes so historical state decodes safely, then dropped during upgrade; added fields are null-initialized. |
| HTTP trace logging in generated provider | Implemented | The generated provider schema carries `log_*` attributes (`log_file`, `log_capture_request_headers`, `log_capture_request_body`, `log_capture_response_headers`, `log_capture_response_body`, `log_max_body_bytes`); `Configure` builds a `client.LoggingConfig` from them — seeded with `generator.yaml` `logging` defaults baked via `ClientIR.Logging` — and appends `client.WithLogging` when a log file is configured, attaching the generated client's trace round-tripper. |
| Terraform Registry publishing artifacts | Implemented (scaffold) | Registry manifest, GoReleaser config, and release workflow are emitted. This project does **not** publish to the real Terraform Registry. |
| Live-API end-to-end validation | Validated | Generated providers from the reference Mycloud spec build, load their schemas, and pass a full connectivity/CRUD lifecycle against a local deterministic mock server (`testfixtures/live`, `TF_ACC=1`); no external system is involved. See §18.4. |

Features marked **Not implemented** or **Not functional** are explicitly accepted as current limitations and are summarized in `docs/usage.md` §Current limitations.

---

## 2. Problem Statement & Motivation

Building a Terraform provider from scratch is a significant engineering effort. A typical provider requires:

1. **Schema definition** — mapping API types to Terraform attribute types.
2. **CRUD implementation** — Create, Read, Update, Delete, and Import for every resource.
3. **HTTP client** — authentication, retries, error handling, request/response marshalling.
4. **Protocol compliance** — satisfying `tfprotov5` or `tfprotov6` interfaces, handling `DynamicValue` serialization, state upgraders, and plan modifiers.
5. **Documentation** — index page, per-resource and per-data-source Markdown files with examples.
6. **Testing** — unit tests, acceptance tests against mock HTTP servers.
7. **Release** — `goreleaser` config, registry manifest, GPG signing.

For APIs that already publish OpenAPI specs, most of this work is mechanical and repetitive. Eidos automates it: the OpenAPI spec becomes the single source of truth, and the provider is generated from it deterministically.

### Comparison with Existing Tools

| Tool | Scope | OpenAPI Versions | Output Completeness | Protocol Layer | Docs Generation |
|------|-------|-----------------|---------------------|----------------|-----------------|
| **OpenAPI Terraform Provider** (hashicorp/terraform-provider-openapi) | Runtime provider that reads OpenAPI at `terraform init` | 2.0, 3.0 | Partial (no generated code, runtime interpretation) | SDKv2 only | Minimal |
| **Terraform Provider Code Generator** (hashicorp/terraform-plugin-code-generation) | Partial scaffolding, not end-to-end | N/A (operates on Terraform schemas, not OpenAPI) | Partial | None | None |
| **OpenAPI Generator** (openapi-generator) | Multi-language SDK generator | 2.0, 3.0, 3.1 | No Terraform provider target | None | None |
| **Eidos (this project)** | End-to-end Terraform provider generation | 2.0, 3.0, 3.1 | Full (code + protocol + client + docs + tests + release) | Protocol v6 (v5 fallback) | Full registry-ready Markdown |

---

## 3. Design Principles

1. **Spec completeness over convenience**: every OpenAPI feature has a deterministic mapping to Terraform, even if the mapping is verbose or requires generated validators. This includes non-CRUD operations mapped to Actions, credential endpoints mapped to Ephemeral Resources, and collection endpoints mapped to List Resources.
2. **Layered generation**: separate parsing, normalization, IR transformation, and emission phases so that each layer can be tested independently.
3. **Protocol abstraction**: generate explicit gRPC protocol layer code (not just framework delegation) so the provider is fully self-contained and auditable.
4. **Idempotent builds**: identical inputs (spec + config) produce identical outputs; no timestamps, random IDs, or nondeterministic ordering in generated code.
5. **Extensible mappings**: users can override naming, type coercion, computed fields, and CRUD behavior via a declarative `generator.yaml`. Users can also explicitly promote operations to Actions, Ephemeral Resources, or List Resources.
6. **Spec-version parity**: OpenAPI 2.0, 3.0, and 3.1 are normalized into a single IR; the generator never needs to know which version the original spec was.
7. **Human-readable output**: generated Go code follows the conventions of `terraform-plugin-framework`, uses idiomatic naming, and includes comments derived from spec descriptions.
8. **Fail loud, never silently**: unsupported or ambiguous OpenAPI constructs produce warnings or errors — never silently dropped.
9. **Full Terraform surface**: Eidos targets every Terraform Plugin Framework construct — Resources, Data Sources, Actions, Ephemeral Resources, List Resources, Functions, and State Stores — not just CRUD resources.

---

## 4. High-Level Architecture

```
                              ┌──────────────────────┐
                              │   CLI (cmd/eidos/)    │
                              │  flags, config,       │
                              │  orchestration        │
                              └──────────┬───────────┘
                                         │
                              ┌──────────▼───────────┐
                               │     Parser            │
                               │  dedicated in-house   │
                               │  (2.0 / 3.0 / 3.1)   │
                              └──────────┬───────────┘
                                         │
                              ┌──────────▼───────────┐
                              │     Normalizer         │
                              │  $ref dereference,     │
                              │  allOf flatten,        │
                              │  polymorphism resolve  │
                              └──────────┬───────────┘
                                         │
                              ┌──────────▼───────────┐
│  Intermediate          │
│  Representation (IR)  │
│  ProviderIR,           │
│  ResourceIR,           │
│  DataSourceIR,         │
│  ActionIR,             │
│  EphemeralResourceIR,  │
│  ListResourceIR,       │
│  SchemaIR, etc.        │
                              └──────────┬───────────┘
                                         │
                              ┌──────────▼───────────┐
                              │     Transformer        │
                              │  OpenAPI → Terraform   │
                              │  type mapping, CRUD    │
                              │  inference, override    │
                              │  application            │
                              └──────────┬───────────┘
                                         │
          ┌───────────────────────────────┼──────────────────────────────────┐
          │                               │                                  │
 ┌────────▼─────────┐  ┌─────────────────▼──────────────┐  ┌────────────────▼─────────────┐
 │  Code Generator   │  │   Documentation Generator       │  │   Test & Release Generator    │
 │  ├─ Provider      │  │   ├─ index.md                   │  │   ├─ acceptance_test.go        │
 │  ├─ Resources     │  │   ├─ resources/*.md             │  │   ├─ unit_test.go              │
 │  ├─ Data Sources  │  │   ├─ data-sources/*.md          │  │   ├─ .tftest.hcl               │
 │  ├─ Actions       │  │   ├─ actions/*.md               │  │   ├─ GNUmakefile               │
 │  ├─ Ephemeral     │  │   ├─ ephemeral-resources/*.md   │  │   ├─ .goreleaser.yml           │
 │  ├─ List Resources│  │   ├─ functions/*.md             │  │   └─ .github/workflows/        │
 │  ├─ Functions     │  │   └─ examples/                 │  │                                │
 │  ├─ Client        │  │                                 │  │                                │
 │  ├─ Protocol      │  │                                 │  │                                │
 │  └─ Models        │  │                                 │  │                                │
 └───────────────────┘  └─────────────────────────────────┘  └───────────────────────────────┘
```

---

## 5. OpenAPI Specification Coverage

### 5.1 Swagger / OpenAPI 2.0

OpenAPI 2.0 (Swagger) introduces the foundational concepts that Eidos must handle:

| Feature | Description | Terraform Mapping |
|---------|-------------|-------------------|
| `swagger` | Version identifier (`"2.0"`) | Version detection in parser |
| `host`, `basePath`, `schemes` | Server URL composition | Provider-level `host`, `base_url`, `scheme` config attributes |
| `paths` + Operations | HTTP endpoints | Resource/Data Source CRUD methods |
| `definitions` | Reusable schemas | `SchemaIR` → nested attributes/blocks |
| `parameters` (path, query, header, body, formData) | Input parameters | Resource arguments, data source arguments |
| `responses` | Output schemas | Resource attributes (computed) |
| `securityDefinitions` | Auth schemes | Provider auth config attributes |
| `security` | Per-operation security | Applied to generated client requests |
| `tags` | Grouping metadata | Documentation sections |
| `externalDocs` | External URLs | Provider/resource documentation links |
| `produces` / `consumes` | Content types | Client `Accept` / `Content-Type` headers |
| `x-*` extensions | Vendor extensions | Override hints, custom metadata |
| Collection formats (`csv`, `ssv`, `tsv`, `pipes`, `multi`) | Array parameter serialization | Query parameter encoding in client |
| `$ref` (JSON Pointer) | Schema references | Dereferenced and inlined during normalization |

### 5.2 OpenAPI 3.0.x

OpenAPI 3.0 introduced significant structural changes over 2.0:

| Feature | Description | Terraform Mapping |
|---------|-------------|-------------------|
| `openapi: "3.0.x"` | Version identifier | Version detection in parser |
| `servers` / `serverVariables` | Replaces `host` + `basePath` + `schemes` | Provider config attributes with variable substitution |
| `components` | Replaces top-level `definitions`, `parameters`, `responses`, `securitySchemes` | Unified component resolution |
| `paths` + `operationId` | Uniquely identifies operations | Resource/data source name inference |
| `requestBody` + `content` + `schema` | Replaces `body` / `formData` parameters | Resource Create/Update argument schemas |
| `callbacks` | Out-of-band requests the API may make | **Action** (invoke action) or Ephemeral resource |
| `links` | Response-linked operations | Import hints, data source relations, or Action |
| `components/securitySchemes` | Replaces `securityDefinitions` | Provider auth config; adds `http` (Basic/Bearer), `oauth2` flows, `openIdConnect` |
| `nullable: true` | Explicit nullability | Computed + Optional attributes |
| `readOnly: true` / `writeOnly: true` | Field directionality | `readOnly` → `Computed`; `writeOnly` → `WriteOnly: true` + `Sensitive` (Terraform 1.10+) |
| `deprecated: true` | Deprecation markers | Attribute deprecation messages |
| `oneOf` / `anyOf` / `allOf` | Schema composition | See [Section 12](#12-polymorphism--complex-schemas) |
| `discriminator` | Polymorphic type switching | See [Section 12](#12-polymorphism--complex-schemas) |
| `not` / `const` / `if`-`then`-`else` | JSON Schema 2020-12 keywords (3.1) | See [Section 5.4 Feature Mapping Matrix](#54-feature-mapping-matrix) |
| `dependentRequired` / `dependentSchemas` | Conditional constraints (3.1) | See [Section 5.4 Feature Mapping Matrix](#54-feature-mapping-matrix) |
| `patternProperties` / `propertyNames` | Pattern-based property constraints (3.1) | See [Section 5.4 Feature Mapping Matrix](#54-feature-mapping-matrix) |
| `minProperties` / `maxProperties` | Object property count limits (3.1) | See [Section 5.4 Feature Mapping Matrix](#54-feature-mapping-matrix) |
| `exclusiveMinimum` / `exclusiveMaximum` | Strict numeric bounds (3.1) | See [Section 5.4 Feature Mapping Matrix](#54-feature-mapping-matrix) |
| `multipleOf` | Numeric multiple constraint (3.1) | See [Section 5.4 Feature Mapping Matrix](#54-feature-mapping-matrix) |
| `unevaluatedProperties` | Controls unevaluated properties (3.1) | See [Section 5.4 Feature Mapping Matrix](#54-feature-mapping-matrix) |
| `webhooks` (3.1 only) | Event-driven API descriptions | Provider-defined functions or ephemeral resources |

### 5.3 OpenAPI 3.1.x

OpenAPI 3.1 aligns with JSON Schema Draft 2020-12 and introduces:

| Feature | Description | Terraform Mapping |
|---------|-------------|-------------------|
| `jsonSchemaDialect` | Default `$schema` for Schema Objects | Validated but not directly mapped |
| `type` arrays (e.g., `["string", "null"]`) | Union types at the schema level | `Optional` + `Computed` with nullable handling |
| `prefixItems` | Ordered tuple validation | `ListNestedAttribute` with positional constraints |
| `contentMediaType` / `contentEncoding` | Binary data description | `StringAttribute` with format validators (base64, binary) |
| `$id` and `$ref` in Schema Objects | JSON Schema Draft 2020-12 reference resolution | Dereferenced during normalization with proper base URI resolution |
| `webhooks` | Incoming webhook descriptions | Provider-level config or data sources |
| `pathItems` in components | Reusable path items | Operation template reuse |
| `not` / `const` / `if`-`then`-`else` | JSON Schema 2020-12 keywords | See [Section 5.4 Feature Mapping Matrix](#54-feature-mapping-matrix) |
| `dependentRequired` / `dependentSchemas` | Conditional constraints | See [Section 5.4 Feature Mapping Matrix](#54-feature-mapping-matrix) |
| `patternProperties` / `propertyNames` | Pattern-based property constraints | See [Section 5.4 Feature Mapping Matrix](#54-feature-mapping-matrix) |
| `minProperties` / `maxProperties` | Object property count limits | See [Section 5.4 Feature Mapping Matrix](#54-feature-mapping-matrix) |
| `exclusiveMinimum` / `exclusiveMaximum` | Strict numeric bounds (changed from boolean to number in 3.1) | See [Section 5.4 Feature Mapping Matrix](#54-feature-mapping-matrix) |
| `multipleOf` | Numeric multiple constraint | See [Section 5.4 Feature Mapping Matrix](#54-feature-mapping-matrix) |
| `unevaluatedProperties` | Controls unevaluated properties | See [Section 5.4 Feature Mapping Matrix](#54-feature-mapping-matrix) |

### 5.4 Feature Mapping Matrix

The following matrix maps every major OpenAPI construct to its Terraform provider equivalent:

| OpenAPI Construct | Terraform Plugin Framework Mapping | Notes |
|-------------------|-----------------------------------|-------|
| `object` (with `properties`) | `SingleNestedAttribute` or `ListNestedAttribute` | Depends on cardinality |
| `object` (with `additionalProperties`) | `MapAttribute` or `MapNestedAttribute` | `MapAttribute` for string→primitive, `MapNestedAttribute` for string→object |
| `array` (of primitives) | `ListAttribute` or `SetAttribute` | `SetAttribute` when `uniqueItems: true` |
| `array` (of objects) | `ListNestedAttribute` or `SetNestedAttribute` | `SetNestedAttribute` when `uniqueItems: true` |
| `string` | `StringAttribute` | With format validators: `date-time`, `date`, `email`, `uuid`, `uri`, `password`, `byte`, `binary` |
| `number` | `Float64Attribute` | |
| `integer` | `Int64Attribute` | |
| `boolean` | `BoolAttribute` | |
| `enum` | `StringAttribute` + `stringvalidator.OneOf()` | Or `Int64Attribute` + `int64validator.OneOf()` for integer enums |
| `oneOf` | Dynamic union, discriminated nested blocks, or split resource types | Configurable per schema; see Section 12 |
| `anyOf` | Union type with validators | See Section 12 |
| `allOf` | Flattened merged object | All properties merged into one `SingleNestedAttribute` |
| `discriminator` | Type-switched nested attribute | See Section 12 |
| `$ref` | Dereferenced and inlined | Circular refs produce `Computed` opaque blocks |
| `readOnly: true` | `Computed: true` + `Optional: true` | Read-only fields are computed on Read |
| `writeOnly: true` | `WriteOnly: true` + `Sensitive: true` (Terraform 1.10+) | Not stored in state; requires `_wo_version` companion attribute |
| `nullable` / `type: ["string", "null"]` | `Optional` + `Computed` | Null is a valid state |
| `default` | `Default: <value>` plan modifier | |
| `minLength`, `maxLength` | `stringvalidator.LengthBetween()` | |
| `minimum`, `maximum` | `int64validator.Between()` / `float64validator.Between()` | |
| `pattern` | `stringvalidator.RegexMatches()` | |
| `minItems`, `maxItems` | `listvalidator.SizeBetween()` | |
| `uniqueItems` | Use `SetAttribute` / `SetNestedAttribute` | |
| `format: "date-time"` | `stringvalidator.IsDateTime()` | Custom validator if not available |
| `format: "email"` | `stringvalidator.IsEmailAddress()` | |
| `format: "uuid"` | `stringvalidator.IsUUID()` | |
| `format: "uri"` | `stringvalidator.IsURLWithScheme()` | |
| `format: "password"` | `Sensitive: true` | |
| `format: "byte"` | `StringAttribute` (base64) | |
| `format: "binary"` | `StringAttribute` or custom type | Terraform has no native binary type; use base64 |
| `format: "int32"` | `Int64Attribute` | Terraform only has Int64 |
| `format: "int64"` | `Int64Attribute` | |
| `format: "float"` | `Float64Attribute` | |
| `format: "double"` | `Float64Attribute` | |
| `deprecated: true` | `DeprecationMessage: "..."` plan modifier | |
| `example` / `x-examples` | Documentation defaults, acceptance test fixtures | |
| `description` | `MarkdownDescription: "..."` | CommonMark support in Framework |
| `externalDocs` | Documentation links in Markdown | |
| `tags` | Resource categorization, doc sections | |
| `not` | `stringvalidator.NoneOf(...)` or custom `NotValidator` | Simple enum negation uses `NoneOf`; complex negation generates custom validator |
| `const` | `stringvalidator.OneOf(value)` or `Default: stringdefault.StaticString(value)` | Single-value enum; if server-controlled, use `Computed` + `Default` |
| `if`/`then`/`else` | Resource-level `ConfigValidators()` with custom `ConditionalConfigValidator` | Generated validator inspects `if` condition and enforces `then`/`else` constraints |
| `dependentRequired` | `stringvalidator.AlsoRequires(path.MatchRoot("..."))` | Per-trigger-attribute `AlsoRequires` validators |
| `dependentSchemas` | Resource-level `ConfigValidators()` with custom `DependentSchemaValidator` | Generated validator runs sub-schema checks when trigger attribute is present |
| `patternProperties` | `MapAttribute` (uniform type) or custom `PatternPropertiesValidator` | Multi-pattern heterogeneous types → custom validator |
| `minProperties`/`maxProperties` | `mapvalidator.SizeBetween(min, max)` | Only for map types; fixed-schema objects skip |
| `exclusiveMinimum`/`exclusiveMaximum` | `int64validator.AtLeast(n+1)` (int) or custom `ExclusiveBoundValidator` (float) | 3.0 boolean form normalized to 3.1 number form |
| `multipleOf` | Custom `Int64MultipleOfValidator` / `Float64MultipleOfValidator` | No built-in validator; generated custom validator |
| `unevaluatedProperties` | No-op (closed schema) or overflow `MapAttribute` | After normalization, all properties are explicit; `false` → drop |
| `propertyNames` | `mapvalidator.KeysAre(stringvalidator.RegexMatches(...))` | Only for map types; validates property name patterns |
| `securitySchemes` (apiKey) | Provider attribute `api_key` (Sensitive) | Header, query, or cookie placement |
| `securitySchemes` (http Basic/Bearer) | Provider attributes `username`/`password` or `bearer_token` | |
| `securitySchemes` (oauth2) | Provider attributes for each flow | `client_id`, `client_secret`, `token_url`, etc. |
| `securitySchemes` (openIdConnect) | Provider attribute `oidc_token_url` | |
| `servers` / `serverVariables` | Provider attributes `host`, `base_url` | Variable substitution in URL templates |
| `callbacks` | **Action** (invoke action) — the API calls back with events | Trigger-style actions with progress messages |
| `links` | Import hints, data source relations, or Action | Identify relationships between operations |
| `webhooks` (3.1) | **Ephemeral resource** or **Action** | Short-lived credentials/tokens from webhook flows |
| Non-CRUD operations (e.g., `POST /servers/{id}/reboot`) | **Action** (invoke action) | See Section 8.7 |
| Collection `GET` endpoints (e.g., `GET /pets`) | **List Resource** (tfquery) | See Section 8.9 |
| Token/credential endpoints (e.g., `POST /credentials/temporary`) | **Ephemeral resource** | See Section 8.8 |

---

## 6. Intermediate Representation (IR)

The IR is a normalized, language-agnostic model that decouples OpenAPI semantics from Terraform Plugin Framework specifics. It is the single data structure that all downstream generators consume.

### 6.1 IR Type System

```go
type PrimitiveType string

const (
    TypeString  PrimitiveType = "string"
    TypeInt     PrimitiveType = "integer"
    TypeFloat   PrimitiveType = "number"
    TypeBool    PrimitiveType = "boolean"
    TypeNull    PrimitiveType = "null"
    TypeDynamic PrimitiveType = "dynamic"
)

type CollectionType struct {
    Kind      CollectionKind // List, Set, Map
    ElementType SchemaIR
}

type CollectionKind string

const (
    List CollectionKind = "list"
    Set  CollectionKind = "set"
    Map  CollectionKind = "map"
)

type UnionType struct {
    Variants    []SchemaIR
    Discriminator *DiscriminatorIR
}

type DiscriminatorIR struct {
    PropertyName string
    Mapping      map[string]string // discriminator value → schema name
}

type SchemaIR struct {
    Name             string
    Description      string
    Type             PrimitiveType
    Collection       *CollectionType
    Union            *UnionType
    Attributes       []AttributeIR
    Blocks           []BlockIR
    Required         bool
    Optional         bool
    Computed         bool
    Sensitive        bool
    WriteOnly        bool           // writeOnly: true → not stored in state (Terraform 1.10+)
    Deprecated       bool
    DeprecationMessage string
    Default          *any
    Validators       []ValidatorIR
    PlanModifiers    []PlanModifierIR
    Format           string   // OpenAPI format: date-time, email, uuid, etc.
    Pattern          string
    EnumValues       []any
    Const            *any            // JSON Schema const: exact value match
    MinLength        *int
    MaxLength        *int
    Minimum          *float64
    Maximum          *float64
    ExclusiveMinimum *float64        // JSON Schema 2020-12: strict > bound
    ExclusiveMaximum *float64        // JSON Schema 2020-12: strict < bound
    MultipleOf       *float64        // JSON Schema: value must be divisible by this
    MinItems         *int
    MaxItems         *int
    MinProperties    *int            // JSON Schema: min object property count
    MaxProperties    *int            // JSON Schema: max object property count
    Not              *SchemaIR       // JSON Schema: negation
    IfSchema         *SchemaIR       // JSON Schema: conditional if
    ThenSchema       *SchemaIR       // JSON Schema: conditional then
    ElseSchema       *SchemaIR       // JSON Schema: conditional else
    DependentRequired map[string][]string // JSON Schema: conditional required fields
    DependentSchemas map[string]*SchemaIR // JSON Schema: conditional schema application
    PatternProperties map[string]*SchemaIR // JSON Schema: regex-matched property schemas
    PropertyNames    *SchemaIR       // JSON Schema: validates property names
    UnevaluatedProperties *SchemaIR  // JSON Schema 2020-12: controls unevaluated properties
    OriginalRef      string   // Original $ref path for traceability
    SourceLocation   string   // File:line for diagnostics
}
```

### 6.2 Provider IR

```go
type ProviderIR struct {
    Name          string
    FullName      string           // e.g., "MyCloud"
    TypeName      string           // e.g., "mycloud"
    Version       string
    Description   string
    SourceSpec    string           // Path/URL of the original OpenAPI spec
    SourceSpecVersion string       // "2.0", "3.0.x", "3.1.x"
    ConfigSchema  ObjectSchemaIR  // Provider-level auth/endpoint config
    Resources     []ResourceIR
    DataSources   []DataSourceIR
    Actions       []ActionIR      // Invoke actions (Terraform 1.13+)
    EphemeralResources []EphemeralResourceIR // Ephemeral resources (Terraform 1.10+)
    ListResources []ListResourceIR // List resources for tfquery (Terraform 1.14+)
    Functions     []FunctionIR
    ClientIR      ClientIR
    SecurityIR    SecurityIR
    Servers       []ServerIR
}
```

### 6.3 Resource IR

```go
type ResourceIR struct {
    Name              string           // e.g., "pet"
    FullName          string           // e.g., "MyCloud Pet"
    TypeName          string           // e.g., "mycloud_pet"
    Description       string
    Schema            ObjectSchemaIR
    CRUDMapping       CRUDMappingIR
    IDAttribute       string
    ImportIDFormat    string           // e.g., "/pets/{petId}" → "petId"
    Importable        bool
    SensitiveAttrs    []string
    Timeouts          *TimeoutConfigIR
    Tags              []string
    DeprecationMessage string
    SourceOperation   string           // Primary operation ID for traceability
}

type CRUDMappingIR struct {
    Create OperationMappingIR
    Read   OperationMappingIR
    Update *OperationMappingIR
    Delete OperationMappingIR
    Import *OperationMappingIR
}

type OperationMappingIR struct {
    Method          string   // GET, POST, PUT, PATCH, DELETE
    PathTemplate    string   // e.g., "/pets/{petId}"
    PathParams      []ParamIR
    QueryParams     []ParamIR
    HeaderParams    []ParamIR
    BodySchema      *SchemaIR
    ResponseSchema  *SchemaIR
    SuccessCodes    []int
    ErrorMappings   map[int]ErrorMappingIR
}
```

### 6.4 Data Source IR

```go
type DataSourceIR struct {
    Name          string
    FullName      string
    TypeName      string
    Description   string
    Schema        ObjectSchemaIR
    ReadMapping   OperationMappingIR
    Tags          []string
    DeprecationMessage string
}
```

### 6.5 Action IR

Actions are a first-class Terraform Plugin Framework abstraction (introduced in Terraform 1.13+) that represent side-effects — operations that interact with remote systems but do not manage CRUD lifecycle state. This is a natural mapping for OpenAPI operations that are not idempotent lifecycle operations (POST to trigger a job, PUT to reboot a server, etc.).

```go
type ActionIR struct {
    Name              string           // e.g., "reboot_server"
    FullName          string           // e.g., "Reboot Server"
    TypeName          string           // e.g., "mycloud_reboot_server"
    Description       string
    ConfigSchema      ObjectSchemaIR  // Input parameters for the action
    InvokeMapping     OperationMappingIR // The HTTP call to make when invoked
    ModifyPlan        bool             // Whether to generate a ModifyPlan method (for API-accessible validation)
    ProgressMessages  bool             // Whether the action is long-running and should send progress messages
    Tags              []string
    SourceOperation   string           // Original OpenAPI operationId for traceability
}
```

**OpenAPI mapping**: Any operation that does not fit the CRUD pattern can be mapped to an Action. Specifically:
- Operations with HTTP methods like `POST /servers/{id}/reboot` that trigger side-effects
- Operations annotated with `x-terraform-action: true` extension
- Operations that don't return a persistent resource state (e.g., "send email", "rotate keys", "run backup")

### 6.6 Ephemeral Resource IR

Ephemeral resources (Terraform 1.10+) represent temporary data that Terraform guarantees will NOT be persisted in state or plan files. Data produced by ephemeral resources can only be referenced in specific ephemeral contexts (e.g., write-only arguments).

```go
type EphemeralResourceIR struct {
    Name              string           // e.g., "temporary_credential"
    FullName          string           // e.g., "Temporary Credential"
    TypeName          string           // e.g., "mycloud_temporary_credential"
    Description       string
    ConfigSchema      ObjectSchemaIR  // Input parameters (what you request)
    ResultSchema      ObjectSchemaIR  // Output data (what you get back, NOT stored in state)
    OpenMapping       OperationMappingIR // HTTP call for the Open lifecycle phase
    RenewMapping      *OperationMappingIR // HTTP call for Renew (if the credential can be renewed)
    CloseMapping      *OperationMappingIR // HTTP call for Close (cleanup/revocation)
    HasRenew          bool             // Whether renew is supported
    HasClose          bool             // Whether close/cleanup is supported
    Tags              []string
    SourceOperation   string
}
```

**OpenAPI mapping**:
- OAuth2 token endpoints → `ephemeral` resource that fetches a short-lived token
- `POST /credentials/temporary` → ephemeral resource that returns time-limited credentials
- `POST /sessions` with `writeOnly` response fields → ephemeral resource
- Any operation where the response contains `writeOnly: true` fields that should never be stored in state
- OpenAPI 3.1 `webhooks` that produce short-lived tokens or credentials

### 6.7 List Resource IR

List resources (Terraform 1.14+, via `terraform query`) enable searching for resources within a scope. They stream results back to Terraform and can optionally include full resource data.

```go
type ListResourceIR struct {
    Name              string           // e.g., "things"
    FullName          string           // e.g., "Things"
    TypeName          string           // e.g., "mycloud_thing" (must match the resource type name)
    Description       string
    ConfigSchema      ObjectSchemaIR  // Filter/search parameters (e.g., resource_group_name)
    ListMapping       OperationMappingIR // The HTTP GET call that returns a list of items
    IdentitySchema    ObjectSchemaIR  // The identity fields that uniquely identify each result
    ResourceSchema    *ObjectSchemaIR // Optional: full resource schema (when include_resource = true)
    PaginationStyle   string          // "offset", "cursor", "link_header", "none"
    Tags              []string
    SourceOperation   string
}
```

**OpenAPI mapping**:
- `GET /pets` (collection/list endpoints) → List resource
- `GET /pets?filter=...` (parameterized list) → List resource with config schema for filters
- Any `GET` operation on a collection path that returns an array of items

### 6.8 Schema IR

```go
type ObjectSchemaIR struct {
    Attributes []AttributeIR
    Blocks     []BlockIR
}

type AttributeIR struct {
    Name             string
    Schema           SchemaIR
    Description      string
    MarkdownDescription string
    Required         bool
    Optional         bool
    Computed         bool
    Sensitive        bool
    WriteOnly        bool           // writeOnly: true → not stored in state (Terraform 1.10+)
    Deprecated       bool
    DeprecationMessage string
    Default          *any
    PlanModifiers   []PlanModifierIR
    Validators       []ValidatorIR
}

type BlockIR struct {
    Name         string
    Schema       ObjectSchemaIR
    NestingMode BlockNestingMode // Single, List, Set
    MinItems     *int
    MaxItems     *int
    Description  string
    Deprecated   bool
    DeprecationMessage string
}

type BlockNestingMode string

const (
    NestingSingle BlockNestingMode = "single"
    NestingList   BlockNestingMode = "list"
    NestingSet    BlockNestingMode = "set"
)

type ValidatorIR struct {
    Type  string   // e.g., "stringvalidator.OneOf", "int64validator.Between"
    Args  []string
}

type PlanModifierIR struct {
    Type  string   // e.g., "stringplanmodifier.UseStateForUnknown", "RequiresReplace"
    Args  []string
}
```

### 6.9 Security IR

```go
type SecurityIR struct {
    Schemes []SecuritySchemeIR
    DefaultRequirements []map[string][]string // e.g., [{"api_key": []}]
}

type SecuritySchemeIR struct {
    Name        string
    Type        SecuritySchemeType // ApiKey, HTTP, OAuth2, OpenIDConnect
    Description string
    In          string   // header, query, cookie (apiKey)
    NameField   string   // header/query/cookie name (apiKey)
    Scheme      string   // "basic", "bearer" (http)
    BearerFormat string
    Flows       *OAuthFlowsIR
    OpenIDConnectURL string
}

type OAuthFlowsIR struct {
    Implicit          *OAuthFlowIR
    Password          *OAuthFlowIR
    ClientCredentials *OAuthFlowIR
    AuthorizationCode *OAuthFlowIR
}

type OAuthFlowIR struct {
    AuthorizationURL string
    TokenURL         string
    RefreshURL       string
    Scopes           map[string]string
}

type ServerIR struct {
    URL         string
    Description string
    Variables   map[string]ServerVariableIR
}

type ServerVariableIR struct {
    Default     string
    Enum        []string
    Description string
}
```

### 6.10 Operation IR

```go
type ParamIR struct {
    Name        string
    In          string   // path, query, header, cookie
    Description string
    Required    bool
    Schema      SchemaIR
    Deprecated  bool
}

type ErrorMappingIR struct {
    StatusCode  int
    Description string
    Schema      *SchemaIR
}

type TimeoutConfigIR struct {
    Create *time.Duration
    Read   *time.Duration
    Update *time.Duration
    Delete *time.Duration
}

type FunctionIR struct {
    Name        string
    FullName    string
    TypeName    string
    Description string
    Arguments   []AttributeIR
    ReturnType  SchemaIR
    Variadic    bool
    Tags        []string
    SourceOperation string
}

type ClientIR struct {
    BaseURLTemplate string
    UserAgent       string
    RetryMax        int
    RetryWaitMin    time.Duration
    RetryWaitMax    time.Duration
    Timeout         time.Duration
    AuthMiddleware  []string  // ordered list of auth handler names
    Pagination      *PaginationIR
}

type PaginationIR struct {
    Style            string  // "offset", "cursor", "link_header", "none"
    PageParam        string
    PerPageParam     string
    TotalCountHeader string
    NextLinkHeader   string
    CursorField      string
}
```

### 6.11 Why an IR?

1. **Multi-version normalization**: OpenAPI 2.0, 3.0, and 3.1 funnel into one IR — downstream generators never need to know the original spec version.
2. **Testability**: unit tests assert on IR nodes rather than generated Go strings, making the transformation pipeline deterministic and easy to validate.
3. **Extensibility**: future output targets (Pulumi provider, CDK constructs, REST client SDK) can reuse the same IR.
4. **Override injection**: user-supplied `generator.yaml` overrides are applied to the IR before emission, not during parsing.
5. **Traceability**: every IR node carries `SourceLocation` (file:line) back to the original spec, enabling precise error messages and warnings.

---

## 7. Component Design

### 7.1 CLI

**Package**: `cmd/eidos/`
**Library**: `spf13/cobra`

```
eidos generate \
  --spec ./api.yaml \
  --output ./terraform-provider-mycloud \
  --config ./generator.yaml \
  --provider-name mycloud \
  --provider-version 0.1.0 \
  --protocol-version 6 \
  --verbose
```

| Flag | Description | Default |
|------|-------------|---------|
| `--spec` | Path to OpenAPI spec file (JSON or YAML) | Required |
| `--output` | Output directory for generated provider | `./<provider-name>` |
| `--config` | Path to `generator.yaml` overrides file | None |
| `--provider-name` | Terraform provider type name (e.g., `mycloud`) | Derived from `info.title` |
| `--provider-version` | Semantic version for the generated provider | `0.1.0` |
| `--protocol-version` | Terraform protocol version: `6` or `5` | `6` |
| `--verbose` | Enable verbose logging | `false` |
| `--format` | Run `gofmt` on output | `true` |
| `--skip-tests` | Skip test generation | `false` |
| `--skip-docs` | Skip documentation generation | `false` |
| `--generate-terraform-tests` | Generate native `.tftest.hcl` files in the output `tests/` directory | `false` |
| `--generate-config` | Emit a starter `generator.yaml` in the output directory | `false` |
| `--dry-run` | Run the full pipeline but do not write files; print a summary of what would be generated | `false` |
| `--dry-run-output` | Write the dry-run summary to a file (JSON or text) | stdout |

The CLI orchestrates the pipeline:
1. **Parse** → `Parser.Parse(specBytes)` → raw OpenAPI document model
2. **Normalize** → `Normalizer.Normalize(rawModel)` → dereferenced, flattened model
3. **Transform** → `Transformer.Transform(normalizedModel, config)` → `ProviderIR`
4. **Generate** → `Generator.Generate(providerIR, outputDir)` → file tree

### 7.1.1 Dry-Run Mode

The `--dry-run` flag runs the full generation pipeline without writing any files to disk. It is useful for reviewing what Eidos would generate before committing to an output directory.

When `--dry-run` is set, the CLI:

1. Parses the OpenAPI spec.
2. Runs normalization and transformation to produce the `ProviderIR`.
3. Executes the generator logic in **record mode** so it can report the list of files it would create and the reasons for each.
4. Prints a structured summary to stdout (and optionally to a file via `--dry-run-output`).
5. Exits with code `0` on success or a non-zero code if the spec cannot be processed.

**Summary output**:

```text
Eidos dry-run summary for provider "mycloud"
Spec: ./api.yaml (OpenAPI 3.0.3)

Generated constructs:
  Resources:        3
  Data sources:     2
  Actions:          1
  Ephemeral resources: 0
  List resources:   1
  Functions:        0
  Security schemes: 1 (apiKey)
  Write-only attributes: 2

Files that would be written (16):
  internal/provider/provider.go
  internal/provider/resource_pet.go
  internal/provider/resource_server.go
  internal/provider/resource_database.go
  internal/provider/data_source_pet.go
  internal/provider/data_source_pets.go
  internal/provider/action_reboot_server.go
  internal/provider/list_pet.go
  internal/client/client.go
  internal/client/auth.go
  internal/client/models.go
  docs/index.md
  docs/resources/pet.md
  examples/resources/pet/resource.tf
  tests/pet.tftest.hcl   (requires --generate-terraform-tests)
  generator.yaml         (requires --generate-config)

Diagnostics:
  [info] Generated test files disabled; pass --generate-terraform-tests to include them
```

**Flags**:

| Flag | Description | Default |
|------|-------------|---------|
| `--dry-run` | Run pipeline without writing files | `false` |
| `--dry-run-output` | Path to write the dry-run summary (JSON or text, inferred from extension) | stdout |

**JSON dry-run output**:

```json
{
  "provider_name": "mycloud",
  "spec_version": "3.0.3",
  "counts": {
    "resources": 3,
    "data_sources": 2,
    "actions": 1,
    "ephemeral_resources": 0,
    "list_resources": 1,
    "functions": 0,
    "security_schemes": 1,
    "write_only_attributes": 2
  },
  "files": [
    { "path": "internal/provider/provider.go", "reason": "provider schema and registration" },
    { "path": "internal/provider/resource_pet.go", "reason": "resource Pet" }
  ],
  "diagnostics": []
}
```

Dry-run mode shares the same code path as normal generation; only the final file-writer is replaced by a recorder, so the summary is always an accurate preview.

### 7.1.2 Feature Validation Endpoint

Eidos includes a small built-in HTTP API (`cmd/eidos/api/`) that exposes a single endpoint designed to exercise and verify every generation feature supported by the project. This endpoint is used by the test suite and can be used by spec authors to confirm that Eidos will handle their constructs correctly.

**Route**: `POST /api/v1/validate`

**Purpose**: Accept an OpenAPI document (or fragment) plus an optional `generator.yaml`, run the parse and normalize pipeline, and return a structured report of what Eidos detected and how it would map it to Terraform.

**Request body**:

```json
{
  "openapi": "3.0.3",
  "info": { "title": "Feature Test", "version": "1.0.0" },
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
                  "oneOf": [
                    { "$ref": "#/components/schemas/Cat" },
                    { "$ref": "#/components/schemas/Dog" }
                  ],
                  "discriminator": {
                    "propertyName": "petType"
                  }
                }
              }
            }
          }
        }
      }
    }
  },
  "components": {
    "schemas": {
      "Cat": { "type": "object", "properties": { "name": { "type": "string" } } },
      "Dog": { "type": "object", "properties": { "breed": { "type": "string" } } }
    }
  },
  "config": "provider:\n  name: featuretest\n"
}
```

**Response**:

```json
{
  "valid": true,
  "diagnostics": [],
  "detected": {
    "resources": 2,
    "data_sources": 2,
    "actions": 0,
    "ephemeral_resources": 0,
    "list_resources": 0,
    "functions": 0,
    "security_schemes": 0,
    "schemas_with_oneOf": 1,
    "schemas_with_allOf": 0,
    "write_only_attributes": 0,
    "polymorphism_strategy": "split_resources"
  },
  "ir_preview": {
    "resources": [
      {
        "type_name": "featuretest_cat",
        "schema": "Cat",
        "crud": { "read": "GET /pets/{id}" }
      },
      {
        "type_name": "featuretest_dog",
        "schema": "Dog",
        "crud": { "read": "GET /pets/{id}" }
      }
    ]
  },
  "suggested_config": "provider:\n  name: featuretest\n  version: 0.1.0\n..."
}
```

**Included feature coverage**:

The validation endpoint is intentionally designed to exercise every feature defined in this document:

| Feature area | How it is exercised |
|--------------|---------------------|
| OpenAPI 2.0 / 3.0 / 3.1 | Accepts specs in all three versions and reports the detected version |
| Primitives, arrays, objects, maps | Returns IR preview of each schema |
| `allOf` / `oneOf` / `anyOf` / `discriminator` | Reports polymorphism strategy and variant mapping |
| `writeOnly` / `readOnly` / `nullable` | Flags detected attributes in `ir_preview` |
| Security schemes | Lists detected schemes and mapped provider config attributes |
| Resources, data sources, actions, ephemeral resources, list resources, functions | Counts and names each generated construct |
| Pagination | Reports pagination style detected from `x-pagination` or response headers |
| Import support | Reports importable resources and import formats |
| State upgraders | Reports version history if configured |
| Provider-defined functions | Lists detected/declared functions |
| Validation & plan modifiers | Lists inferred validators and plan modifiers per attribute |
| Logging options | Confirms provider-level logging schema attributes |
| `terraform test` files | Indicates whether `.tftest.hcl` generation would be enabled |
| Config file generation | Includes a suggested `generator.yaml` in the response |

The endpoint is implemented as a normal `net/http` handler in `cmd/eidos/api/validate.go` and can be started with `eidos api --port 8080`. It uses the same parser, normalizer, and transform package as the CLI so results are representative of real generation.

### 7.1.3 MCP Server

Eidos exposes a **Model Context Protocol (MCP) server** over stdio that lets an
MCP-compatible agent or IDE drive the whole workflow without codebase access
(live in `pkg/mcp`, wired by `cmd/eidos/mcp.go`). It advertises five tools:

| Tool | Purpose |
|------|---------|
| `eidos/generate-config` | Scaffold a `generator.yaml` from a supplied OpenAPI spec (or spec fragment) without invoking the full CLI pipeline. |
| `eidos/inspect` | Report what eidos would generate — every resource with its CRUD mapping completeness and wired-vs-scaffolded status, plus data sources, actions, ephemeral resources, list resources, and functions. |
| `eidos/generate` | Run parse → transform → generate and return a manifest; optionally writes the provider files to a caller-supplied output directory. |
| `eidos/validate-schemas` | Report which generated schemas terraform-plugin-framework would reject (dynamic-element collections, a nested `DynamicAttribute`, invalid attribute names, `Computed`+`Required`, reserved root names). |
| `eidos/override-preview` | Return the IR preview *after* `generator.yaml` overrides plus a per-entry report of which `resource_overrides` matched (surfacing silent no-ops). |

The full reference (parameters and return shapes) is in
`docs/usage.md` §`eidos mcp`. The config-scaffolding tool works as follows:

**Tool name**: `eidos/generate-config`

**Input schema**:

```json
{
  "openapi": "3.0.3",
  "info": { "title": "MyCloud", "version": "1.0.0" },
  "paths": { ... },
  "components": { ... }
}
```

Parameters:

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `spec` | `string` or `object` | Yes | The OpenAPI spec as a JSON/YAML string or parsed object |
| `format` | `string` | No | Output format: `yaml` (default) or `json` |
| `include_comments` | `boolean` | No | Include explanatory comments in the generated YAML |

**Output schema**:

```json
{
  "config": "provider:\n  name: mycloud\n  version: 0.1.0\n...",
  "diagnostics": []
}
```

The generated config contains:

- Detected provider name, version, and description.
- Detected servers.
- Detected security schemes with environment variable hints.
- Inferred resources and data sources with any ambiguous mappings flagged.
- Inferred actions, ephemeral resources, list resources, and functions.
- Detected polymorphism strategies.
- Suggested overrides for naming, timeouts, and pagination.

**Implementation**: `eidos/generate-config` is a thin wrapper around the config-generation helper described in [Section 14](#14-configuration--overrides-system). It lives in `pkg/mcp` and is registered on the server by `cmd/eidos/mcp.go` for any MCP host. When invoked, it runs the parser and transformer in **discovery mode** (no file output) and serializes the resulting config template. The other four tools reuse the same `pkg/api` validation/IR-preview path, so the CLI, HTTP API, and MCP server all stay consistent.

`eidos/generate-config` shares the config-generation path with the `eidos generate-config` CLI command and the `/api/v1/validate` endpoint, so all three entrypoints stay consistent.

### 7.1.4 Config File Generation from Detected Spec

In addition to accepting a user-authored `generator.yaml`, Eidos can **generate a starter config file** from the detected contents of the OpenAPI spec. This lets users take the generated config as a base and then add custom overrides, rename resources, fix ambiguous mappings, etc.

**CLI command**:

```
eidos generate-config \
  --spec ./api.yaml \
  --output ./generator.yaml \
  --include-comments
```

**Behavior**:

1. Parse the spec using the dedicated parser.
2. Run the transformer in **discovery mode** (no file generation).
3. Emit a `generator.yaml` containing:
   - Detected `provider.name`, `provider.version`, and `provider.description`.
   - Detected `servers`.
   - Detected security schemes with environment variable hints.
   - Inferred resources, data sources, actions, ephemeral resources, list resources, and functions.
   - Detected polymorphism strategies (`dynamic_union` or `split_resources`) and variant names.
   - Suggested `naming`, `timeouts`, and `pagination` blocks.
   - Comments (when `--include-comments` is set) explaining why each inferred mapping was chosen and which fields may need manual adjustment.

**Example generated config**:

```yaml
provider:
  name: mycloud
  version: 0.1.0
  description: "Auto-generated from api.yaml (OpenAPI 3.0.3)"

servers:
  - url: "https://api.mycloud.io/v1"
    description: "Production"

security:
  - scheme: apiKey
    header_name: X-API-Key
    env_var: MYCLOUD_API_KEY

# Inferred resources. Review each mapping; ambiguous entries are flagged with comments.
resources:
  - operation: createPet
    name: pet
    id_attribute: id
  - operation: createServer
    name: server
    id_attribute: server_id

# Inferred data sources.
data_sources:
  - operation: getPet
    name: pet
  - operation: listPets
    name: pets

# Inferred actions.
actions:
  - operation: rebootServer
    name: reboot_server

# Inferred list resources.
list_resources:
  - resource: pet
    operation: listPets

# Polymorphism detected in the spec.
# Strategy 'split_resources' chosen for Pet because it is a top-level oneOf of named variants.
polymorphism:
  strategy: split_resources
  oneOf:
    - schema: Pet
      variants:
        - schema: Cat
          resource_name: cat
        - schema: Dog
          resource_name: dog

# Suggested timeouts.
global_timeouts:
  create: 20m
  read: 10m
  update: 20m
  delete: 10m

# Suggested pagination style detected from x-pagination extension.
pagination:
  style: offset
  page_param: page
  per_page_param: per_page
```

**Integration with `eidos generate`**:

- Passing `--generate-config` to `eidos generate` writes a `generator.yaml` into the output directory alongside the generated provider.
- The generated provider is then based on that config, making the output self-documenting and reproducible.
- When combined with `--dry-run`, the summary includes the path of the config that would be emitted.

**MCP and API integration**:

The same config-generation logic is exposed through:

- The `eidos generate-config` CLI command.
- The `/api/v1/validate` endpoint (returns the config in `suggested_config`).
- The `eidos/generate-config` MCP tool.

All three use a single helper in `pkg/config/generator.go` so behavior never diverges.

### 7.2 Parser

**Package**: `pkg/parser/`
**Type**: Dedicated in-house parser (no external OpenAPI library dependency)

**Responsibilities**:
- Detect spec version (`swagger: "2.0"`, `openapi: "3.0.x"`, `openapi: "3.1.x"`).
- Parse JSON or YAML input into a thin, version-specific AST, then convert to a generic internal model.
- Validate structural correctness (missing required fields, invalid `$ref` targets) with source-location diagnostics.
- Resolve all `$ref` values, including external files and remote URLs, with cycle detection.
- Return a version-agnostic intermediate model consumed by the normalizer.

**Version-specific handling**:

| Version | Key Differences | Parser Handling |
|---------|----------------|-----------------|
| 2.0 | `host` + `basePath` + `schemes` instead of `servers` | Convert to `ServerIR` with template |
| 2.0 | `definitions` instead of `components/schemas` | Map to `components.schemas` internally |
| 2.0 | `parameters` in body/formData instead of `requestBody` | Convert to `RequestBodyIR` |
| 2.0 | `securityDefinitions` instead of `components/securitySchemes` | Map to `SecuritySchemeIR` |
| 2.0 | `produces`/`consumes` at top-level and operation-level | Map to `OperationMappingIR` content negotiation |
| 3.0 | `servers` with variables | Direct mapping to `ServerIR` |
| 3.0 | `components/*` | Direct mapping |
| 3.0 | `requestBody` with `content` | Direct mapping |
| 3.0 | `nullable: true` | Set `Optional: true, Computed: true` in IR |
| 3.1 | `type` arrays (`["string", "null"]`) | Map to nullable schema |
| 3.1 | `webhooks` | Map to provider-level config or data sources |
| 3.1 | `prefixItems` | Map to ordered tuple validation |
| 3.1 | `contentMediaType`/`contentEncoding` | Map to format validators or custom types |

**No external OpenAI parser dependency**: Eidos deliberately avoids `libopenapi`, `kin-openapi`, or any other third-party OpenAPI parser. All parsing, validation, and resolution logic lives in `pkg/parser/` and its version-specific subpackages.

### 7.3 Normalizer

**Package**: `pkg/normalizer/`

**Responsibilities**:
1. **`$ref` resolution**: Recursively dereference all JSON Pointer `$ref` entries. Circular references are detected and produce a warning; the referencing attribute becomes `Computed` with an opaque type.
2. **`allOf` flattening**: Merge all `allOf` schemas into a single flat object, resolving property conflicts (duplicate required fields with same type → merge; conflicting types → error).
3. **Polymorphism normalization**: Convert `oneOf` + `discriminator` combinations into `UnionType` with `DiscriminatorIR`; `anyOf` becomes `UnionType` without a discriminator.
4. **Parameter resolution**: Merge path-level parameters into operation-level parameters; resolve `parameters` references.
5. **Security resolution**: Merge global `security` with operation-level overrides.
6. **Server composition**: Compose `servers` hierarchy (global → path-item → operation) into final `ServerIR` list per operation.
7. **Naming normalization**: Derive `operationId` from method + path if not present; sanitize names to be Go/Terraform-compatible (lowercase_snake_case for HCL, PascalCase for Go types).

### 7.4 Transformer

**Package**: `pkg/transform/`

**Responsibilities**:
1. **Type mapping**: Convert OpenAPI types to Terraform Plugin Framework types (see [Section 10](#10-openapi-to-terraform-type-mapping)).
2. **CRUD inference**: Analyze path patterns and HTTP methods to infer Create/Read/Update/Delete operations:
   - `POST /pets` → Create
   - `GET /pets/{petId}` → Read
   - `GET /pets` → List (data source) or List Resource (tfquery)
   - `PUT /pets/{petId}` → Full Update
   - `PATCH /pets/{petId}` → Partial Update
   - `DELETE /pets/{petId}` → Delete
3. **Action inference**: Detect non-CRUD operations:
   - `POST /servers/{id}/<action>` patterns → Action
   - Operations returning non-resource responses (job IDs, status messages) → Action
   - Operations with `x-terraform-action: true` → Action
4. **Ephemeral resource inference**: Detect temporary/credential operations:
   - Operations returning `writeOnly: true` response properties → Ephemeral resource
   - OAuth2 token endpoints → Ephemeral resource
   - Operations with `x-terraform-ephemeral: true` → Ephemeral resource
5. **List resource inference**: Detect collection endpoints:
   - `GET /pets` where `GET /pets/{petId}` resource exists → List resource
   - Operations with `x-terraform-list: true` → List resource
6. **ID attribute detection**: Identify the primary identifier attribute from path parameters or response schema (e.g., `id`, `petId`).
7. **Computed/Required/Optional/WriteOnly inference**: Apply rules:
   - `readOnly: true` → `Computed: true`
   - `required: true` AND NOT `readOnly` → `Required: true`
   - `required: false` AND NOT `readOnly` → `Optional: true`
   - `writeOnly: true` → `WriteOnly: true` + `Sensitive: true`
   - Default values present → `Optional: true` + `Default` plan modifier
8. **Security mapping**: Convert security schemes to provider config attributes.
9. **Override application**: Apply `generator.yaml` overrides (see [Section 14](#14-configuration--overrides-system)).
10. **Naming conflicts**: Resolve collisions (e.g., two operations producing the same resource name) by appending HTTP method or path segments.

### 7.5 Code Generator

**Package**: `pkg/generator/`
**Template engine**: `text/template` with helper functions. Programmatic Go source files are generated with the in-tree `pkg/generator/astgen` standard-library package (`go/ast` + `go/token` + `go/format`).

**Responsibilities** — emit the following Go source files:

| File | Purpose |
|------|---------|
| `main.go` | Provider server entrypoint using `providerserver.NewServe()` |
| `internal/provider/provider.go` | Provider struct, `Metadata`, `Schema`, `Configure`, `Resources`, `DataSources`, `Actions`, `EphemeralResources`, `ListResources`, `Functions` |
| `internal/provider/provider_test.go` | Provider-level unit tests |
| `internal/provider/resource_<name>.go` | Resource implementation: `Create`, `Read`, `Update`, `Delete`, `ImportState`, `Schema` |
| `internal/provider/data_source_<name>.go` | Data source implementation: `Read`, `Schema` |
| `internal/provider/action_<name>.go` | Action implementation: `Invoke`, `Schema`, `ModifyPlan`, `ValidateConfig` |
| `internal/provider/ephemeral_<name>.go` | Ephemeral resource: `Open`, `Renew`, `Close`, `Schema` |
| `internal/provider/list_<name>.go` | List resource: `List`, `ListResourceConfigSchema` |
| `internal/provider/function_<name>.go` | Provider-defined function (optional) |
| `internal/provider/model_<name>.go` | Go struct types matching Terraform schemas (for JSON marshalling) |
| `internal/provider/validators.go` | Custom validators (discriminator, conditional, exclusive bounds, multipleOf, etc.) |
| `internal/client/client.go` | Generated HTTP client |
| `internal/client/auth.go` | Authentication middleware |
| `internal/client/models.go` | Request/response structs |
| `internal/client/retry.go` | Retry logic with exponential backoff |
| `internal/protocol/value_mappers.go` | `tftypes.Value` ↔ Go struct converters |

### 7.6 Protocol Layer Generator

**Package**: `pkg/generator/protocol/`

The generated provider uses `terraform-plugin-framework` which abstracts the gRPC protocol. However, Eidos also generates explicit value mapper code that bridges between `tftypes.Value` (the protocol representation) and generated Go models, ensuring:

1. **Schema descriptors** are correctly defined via `schema.Schema` for every resource, data source, and the provider.
2. **State conversion** between `tftypes.Value` and Go structs is handled by generated `PlanToModel()` and `ModelToState()` helper functions.
3. **Diagnostics accumulation** is handled by generated error-to-diagnostic converters that map HTTP error responses to `diag.Diagnostics`.
4. **State upgraders** are generated when schema versioning is detected (via `generator.yaml`).
5. **Import state** handlers parse composite IDs from `ImportStateRequest.ID`.

The provider binary is served via `providerserver.NewServe()` (Protocol v6) or `tf5server.NewServe()` (Protocol v5), both from `terraform-plugin-go`.

### 7.7 Documentation Generator

**Package**: `pkg/generator/docs/`

Generates Markdown files compatible with `terraform-plugin-docs` and the Terraform Registry:

| File | Content |
|------|---------|
| `docs/index.md` | Provider overview, authentication guide, example HCL |
| `docs/resources/<name>.md` | Argument reference, attribute reference, import syntax, timeout info, example HCL |
| `docs/data-sources/<name>.md` | Argument reference, attribute reference, example HCL |
| `docs/actions/<name>.md` | Action argument reference, example HCL invocation |
| `docs/ephemeral-resources/<name>.md` | Configuration reference, result attributes, ephemeral context restrictions, example HCL |
| `docs/functions/<name>.md` | Arguments, return type, example HCL |
| `examples/resources/<name>/resource.tf` | Minimal HCL example for `terraform apply` |
| `examples/data-sources/<name>/data-source.tf` | Minimal HCL example for data source |

Content is derived from:
- `info.title` and `info.description` → `index.md`
- `operation.summary` and `operation.description` → resource/data-source descriptions
- `schema.description` → attribute descriptions (rendered as Markdown)
- `schema.example` / `x-examples` → example HCL
- `externalDocs.url` → "See Also" sections
- `tags` → documentation grouping

Frontmatter for `terraform-plugin-docs`:

```yaml
---
subcategory: "Pets"
description: |-
  Manages a Pet resource.
---
```

### 7.8 HTTP Client Generator

**Package**: `pkg/generator/client/`

Generates a Go HTTP client using `net/http`:

**Features**:
- **Request construction**: Path parameter substitution via `strings.ReplaceAll`, query parameter encoding via `url.Values`, header injection.
- **Response parsing**: JSON unmarshalling into generated model structs; error response parsing into typed error structs.
- **Authentication middleware**: Pluggable auth handlers for each `SecuritySchemeIR` type:
  - `apiKey` → inject header/query/cookie
  - `http/basic` → `Authorization: Basic <encoded>`
  - `http/bearer` → `Authorization: Bearer <token>`
  - `oauth2/client_credentials` → token exchange with `client_id`, `client_secret`, `token_url`
  - `oauth2/authorization_code` → redirect-based flow (documented, not auto-implemented)
  - `openIdConnect` → OIDC discovery + token acquisition (documented, not auto-implemented)
- **Retry logic**: Exponential backoff with jitter for `5xx` and `429` responses; configurable max retries.
- **Pagination**: Detect `x-pagination` extension or `Link` header patterns; generate `ListAllPages()` helper.
- **Timeouts**: Per-request context timeouts from Terraform resource timeouts.
- **User-Agent**: `User-Agent: Terraform/<version> <provider-name>/<version>`.

### 7.9 Test Generator

**Package**: `pkg/generator/test/` (Go package `testgen`)

Generates:

1. **Unit tests** (`internal/provider/resource_<name>_test.go`): Test schema definitions, value mappers, validators.
2. **Acceptance tests** (`internal/provider/resource_<name>_acc_test.go`): Full `terraform-plugin-testing` acceptance tests using `httptest.NewServer()` as a mock API server. Tests cover:
   - Create + Read
   - Update + Read
   - Delete (resource disappears)
   - Import (by ID)
   - Import (by composite ID, if applicable)
3. **Mock HTTP server**: Generated `httptest` handlers that return fixture data derived from OpenAPI `example` fields and `x-examples` extensions.

### 7.10 Support Packages

The following support packages are shared by multiple stages of the generation pipeline and do not map one-to-one to the CLI or generator subpackages above:

**Package**: `pkg/ir/`

Defines the intermediate representation (`ProviderIR`, `ResourceIR`, `DataSourceIR`, etc.) consumed by the normalizer, transform, and generator packages. See [Section 6](#6-intermediate-representation-ir) for the IR type system.

**Package**: `pkg/config/`

Loads, validates, and emits the `generator.yaml` configuration file. Used by the CLI `generate`, `generate-config`, and `--dry-run` paths, as well as the `/api/v1/validate` endpoint and the MCP `eidos/generate-config` tool.

**Package**: `pkg/diagnostics/`

Collects, formats, and reports errors and warnings with source locations for the CLI, HTTP API, and MCP tool.

---

## 8. Terraform Plugin Framework Integration

### 8.1 Schema Generation

The Terraform Plugin Framework (v1.x) is the primary target. Each `SchemaIR` node maps to a Framework schema attribute:

| IR Construct | Framework Code |
|-------------|---------------|
| `AttributeIR` (primitive) | `schema.StringAttribute{}`, `schema.Int64Attribute{}`, etc. |
| `AttributeIR` (collection) | `schema.ListAttribute{}`, `schema.SetAttribute{}`, `schema.MapAttribute{}` |
| `BlockIR` (single) | `schema.SingleNestedAttribute{}` or `schema.SingleNestedBlock{}` |
| `BlockIR` (list) | `schema.ListNestedAttribute{}` or `schema.ListNestedBlock{}` |
| `BlockIR` (set) | `schema.SetNestedAttribute{}` or `schema.SetNestedBlock{}` |
| `Required` | Not `Optional`, not `Computed` |
| `Optional` | `schema.Attribute{Optional: true}` |
| `Computed` | `schema.Attribute{Computed: true}` |
| `Sensitive` | `schema.Attribute{Sensitive: true}` |
| `WriteOnly` | `schema.Attribute{WriteOnly: true}` (Terraform 1.10+) |
| `Deprecated` | `schema.Attribute{DeprecationMessage: "..."}` |
| `Validators` | `schema.Attribute{Validators: []validator.String{...}}` |
| `PlanModifiers` | `schema.Attribute{PlanModifiers: []planmodifier.String{...}}` |
| `Default` | `schema.Attribute{Default: stringdefault.StaticString("...")}` |

### 8.2 CRUD Mapping

| Terraform Operation | OpenAPI Pattern | HTTP Method |
|--------------------|----------------|-------------|
| `Create` | `POST /collection` or `PUT /collection/{id}` | POST or PUT |
| `Read` | `GET /collection/{id}` | GET |
| `Update` (full) | `PUT /collection/{id}` | PUT |
| `Update` (partial) | `PATCH /collection/{id}` | PATCH |
| `Delete` | `DELETE /collection/{id}` | DELETE |
| `Import` | Same as Read | GET |

**Create flow**:
1. Terraform calls `Create(ctx, req, resp)`.
2. Provider constructs request body from `req.Plan` using `PlanToModel()`.
3. Client sends HTTP request to API.
4. On success (201/200), client parses response body and/or Location header.
5. Provider stores response in state via `ModelToState()`.
6. On error, provider returns `diag.Diagnostics` with error detail.

**Update flow**:
- If `PUT` is available → full replacement of all modifiable fields.
- If only `PATCH` is available → compute diff, send only changed fields.
- If both are available → prefer `PUT` for full updates, `PATCH` for partial (configurable via `generator.yaml`).

**Delete flow**:
1. Terraform calls `Delete(ctx, req, resp)`.
2. Provider extracts ID from `req.State`.
3. Client sends `DELETE /collection/{id}`.
4. On success (204/200), provider removes resource from state.
5. On 404, provider removes resource from state (already deleted).
6. On other errors, provider returns diagnostics.

### 8.3 Plan Modifiers & Validators

**Automatically inferred plan modifiers**:

| OpenAPI Indicator | Generated Plan Modifier |
|-------------------|------------------------|
| `readOnly: true` | `stringplanmodifier.UseStateForUnknown()` (value set on Read) |
| `writeOnly: true` | `stringplanmodifier.UseStateForUnknown()` + `WriteOnly: true` (not stored in state) |
| `forceNew` / `x-terraform-force-new: true` | `planmodifier.RequiresReplace()` |
| Default value present | `stringdefault.StaticString(value)` |

**Automatically inferred validators**:

| OpenAPI Indicator | Generated Validator |
|-------------------|-------------------|
| `enum: [...]` | `stringvalidator.OneOf(values...)` |
| `minLength` / `maxLength` | `stringvalidator.LengthBetween(min, max)` |
| `minimum` / `maximum` | `int64validator.Between(min, max)` or `float64validator.Between(min, max)` |
| `pattern` | `stringvalidator.RegexMatches(regexp.MustCompile(pattern), msg)` |
| `format: "email"` | `stringvalidator.IsEmailAddress()` |
| `format: "uuid"` | `stringvalidator.IsUUID()` |
| `format: "uri"` | `stringvalidator.IsURLWithScheme("https")` |
| `format: "date-time"` | Custom RFC3339 validator |
| `not: {enum: [...]}` | `stringvalidator.NoneOf(values...)` |
| `not: {complex schema}` | Custom `NotValidator` |
| `const` | `stringvalidator.OneOf(value)` or `Default: stringdefault.StaticString(value)` |
| `if`/`then`/`else` | Resource-level `ConfigValidators()` with custom `ConditionalConfigValidator` |
| `dependentRequired` | `stringvalidator.AlsoRequires(path.MatchRoot("..."))` per trigger attribute |
| `dependentSchemas` | Resource-level `ConfigValidators()` with custom `DependentSchemaValidator` |
| `patternProperties` (uniform type) | `MapAttribute` with element type |
| `patternProperties` (heterogeneous) | Custom `PatternPropertiesValidator` |
| `minProperties`/`maxProperties` | `mapvalidator.SizeBetween(min, max)` (map types only) |
| `exclusiveMinimum`/`exclusiveMaximum` (int) | `int64validator.AtLeast(n+1)` / `int64validator.AtMost(n-1)` |
| `exclusiveMinimum`/`exclusiveMaximum` (float) | Custom `ExclusiveBoundValidator` |
| `multipleOf` | Custom `Int64MultipleOfValidator` / `Float64MultipleOfValidator` |
| `propertyNames` | `mapvalidator.KeysAre(stringvalidator.RegexMatches(...))` (map types only) |

### 8.4 State Upgraders

When a provider's schema evolves, Terraform requires state upgraders to migrate existing state. Eidos generates:

1. A `SchemaVersion` constant starting at `0`, incremented via `generator.yaml` configuration.
2. State upgrader functions for each version transition, converting old attribute and block names/types to new ones.

Each `StateUpgrade` entry in `generator.yaml` may declare, for a single `from_version`:

- `renames`: old attribute name → current attribute name.
- `block_renames`: old block name → current block name. Block renames assume shape invariance (the block's nested schema is unchanged); the generated upgrader copies `prior.<oldField>` into `upgraded.<currentField>` and emits a warning reminding the author to verify the nested shape.
- `added_attributes` / `added_blocks`: current fields absent from the prior schema. They are omitted from the prior schema and null-initialized during upgrade.
- `removed_attributes` / `removed_blocks`: prior fields absent from the current schema. They are synthesized into the prior schema as `Dynamic`-typed, `Optional`+`Computed` attributes so historical state of any shape decodes safely, then dropped during upgrade (the upgrader simply does not copy them into the upgraded model).

Validation enforces that every rename/added/removed name resolves to a real current or prior schema member, that prior names are unique within an upgrade (required for unambiguous state decode), and that no field is both added and removed. State upgrades are also propagated from `generator.yaml` overrides through the transformer into the IR (previously the override path was test-only).

This is opt-in: the `generator.yaml` must declare schema version changes.

### 8.5 Import Support

Eidos generates `ImportState` handlers for resources where:
- The resource has a `Read` operation.
- The `Read` path template contains exactly one path parameter (simple ID).
- Or the `generator.yaml` explicitly declares an import format. Composite IDs must use brace-enclosed attributes, for example `{project_id}:{resource_id}`.

For composite import IDs, the handler splits the import string on the delimiter between brace-enclosed attributes and maps each segment to the corresponding attribute. Unbraced composite formats such as `project_id:resource_id` are rejected during generation; existing `generator.yaml` entries must be updated to use braces.

### 8.6 Provider-Defined Functions

OpenAPI specs can define utility endpoints that don't map naturally to resources or data sources. Eidos can generate provider-defined functions for:

- `GET /utils/validate` → function `validate_<name>()`
- `GET /utils/lookup` → function `lookup_<name>()`

This is opt-in via `generator.yaml`:

```yaml
functions:
  - operation: "ipLookup"
    type: "lookup"
    arguments:
      - name: "ip"
        type: "string"
    return_type: "string"
```

### 8.7 Actions (Invoke Actions)

**Actions** are a first-class Terraform Plugin Framework abstraction introduced in Terraform 1.13. They represent side-effects that interact with remote systems but do not manage CRUD lifecycle state. Actions are invoked directly via `terraform apply` or the CLI, and they do not produce output data that can be referenced by other parts of a Terraform configuration.

Actions are a **critical** mapping target for OpenAPI because many API operations are inherently action-oriented rather than resource-oriented — e.g., "reboot server", "rotate credentials", "send notification", "run backup".

#### How Actions Map from OpenAPI

Not every HTTP operation maps to a resource CRUD method. Eidos uses the following heuristics and explicit configuration to identify actions:

**Automatic detection**:
- `POST /resources/{id}/<action>` patterns (e.g., `POST /servers/{id}/reboot`) → Action
- Operations that return non-resource responses (e.g., job IDs, status messages) → Action
- Operations annotated with `x-terraform-action: true` → Action

**Explicit configuration** (`generator.yaml`):
```yaml
actions:
  - operation: "rebootServer"
    name: "reboot_server"
    description: "Reboots the specified server"
    progress_messages: true   # Generate streaming progress updates
    modify_plan: true          # Generate ModifyPlan for API-accessible validation
  - operation: "rotateDatabaseCredentials"
    name: "rotate_database_credentials"
    description: "Rotates database credentials immediately"
```

#### Generated Code Structure

```go
// internal/provider/action_reboot_server.go

type RebootServerAction struct {
    client *http.Client
}

func (a *RebootServerAction) Metadata(ctx context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
    resp.TypeName = req.ProviderTypeName + "_reboot_server"
}

func (a *RebootServerAction) Schema(ctx context.Context, req action.SchemaRequest, resp *action.SchemaResponse) {
    resp.Schema = schema.Schema{
        Attributes: map[string]schema.Attribute{
            "server_id": schema.StringAttribute{
                Required:    true,
                Description:  "The ID of the server to reboot.",
            },
            "force": schema.BoolAttribute{
                Optional:    true,
                Description:  "Force a hard reboot.",
            },
        },
    }
}

func (a *RebootServerAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
    var data RebootServerActionModel
    resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
    if resp.Diagnostics.HasError() {
        return
    }
    // Generated HTTP call to POST /servers/{serverId}/reboot
    httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost,
        a.client.BaseURL + "/servers/" + data.ServerId.ValueString() + "/reboot", nil)
    httpResp, err := a.client.Do(httpReq)
    // ... error handling and progress messages ...
}
```

#### Action Lifecycle Methods

| Method | Purpose | OpenAPI Mapping |
|--------|---------|----------------|
| `Metadata` | Define action type name | Derived from `operationId` or path |
| `Schema` | Define input parameters | Derived from operation parameters + request body |
| `Configure` | Receive provider client | Same as resources/data sources |
| `Invoke` | Execute the action | The mapped HTTP operation |
| `ValidateConfig` | Validate configuration offline | Derived from parameter constraints |
| `ModifyPlan` | Validate with API access | Optional; for API-accessible validation |
| `ConfigValidators` | Cross-attribute validation | Derived from `allOf`/`oneOf` constraints |

#### Streaming Progress Messages

For long-running actions (e.g., `POST /servers/{id}/migrate`), Eidos generates code that uses `resp.SendProgress()` to stream progress messages back to the practitioner during invocation. This is enabled via `progress_messages: true` in the override config or auto-detected from operations that return `202 Accepted` responses.

### 8.8 Ephemeral Resources

**Ephemeral resources** (Terraform 1.10+) are resources whose data is guaranteed NOT to be persisted in state or plan files. They follow an Open → (Renew) → Close lifecycle and can only be referenced in ephemeral contexts (e.g., write-only arguments on managed resources).

This is the natural Terraform mapping for OpenAPI operations that produce short-lived, sensitive, or temporary data — credentials, tokens, signed URLs, temporary access grants.

#### How Ephemeral Resources Map from OpenAPI

**Automatic detection**:
- Operations that return `writeOnly: true` response properties → Ephemeral resource
- Operations with `x-terraform-ephemeral: true` annotation → Ephemeral resource
- OAuth2 token endpoints (detected from `securitySchemes` with `oauth2` flows) → Ephemeral resource
- Operations where the response schema has `format: "password"` or sensitive headers → Candidate ephemeral resource

**Explicit configuration** (`generator.yaml`):
```yaml
ephemeral_resources:
  - operation: "generateTemporaryCredentials"
    name: "temporary_credential"
    description: "Generates short-lived credentials"
    open_mapping: "POST /credentials/temporary"
    close_mapping: "DELETE /credentials/temporary/{credentialId}"
    renew_mapping: "POST /credentials/temporary/{credentialId}/renew"
    result_fields:
      - name: "access_key_id"
        type: "string"
        sensitive: true
      - name: "secret_access_key"
        type: "string"
        sensitive: true
      - name: "session_token"
        type: "string"
        sensitive: true
      - name: "expiration"
        type: "string"
```

#### Ephemeral Resource Lifecycle

| Lifecycle Phase | Method | OpenAPI Mapping |
|----------------|--------|----------------|
| **Open** | `Open(ctx, req, resp)` | Mapped to a `POST` or `GET` operation that produces temporary data |
| **Renew** | `Renew(ctx, req, resp)` | Mapped to a `POST /.../renew` or token-refresh operation (optional) |
| **Close** | `Close(ctx, req, resp)` | Mapped to a `DELETE` or revocation operation (optional) |

#### Generated Code Structure

```go
// internal/provider/ephemeral_temporary_credential.go

type TemporaryCredentialEphemeralResource struct {
    client *http.Client
}

func (e *TemporaryCredentialEphemeralResource) Metadata(ctx context.Context, req ephemeral.MetadataRequest, resp *ephemeral.MetadataResponse) {
    resp.TypeName = req.ProviderTypeName + "_temporary_credential"
}

func (e *TemporaryCredentialEphemeralResource) Schema(ctx context.Context, req ephemeral.SchemaRequest, resp *ephemeral.SchemaResponse) {
    resp.Schema = schema.Schema{
        Attributes: map[string]schema.Attribute{
            "role": schema.StringAttribute{
                Required:    true,
                Description:  "The IAM role to assume.",
            },
            "ttl": schema.StringAttribute{
                Optional:    true,
                Description:  "Time-to-live for the credential (e.g., '3600s').",
            },
            // Result attributes (not stored in state)
            "access_key_id": schema.StringAttribute{
                Computed:    true,
                Sensitive:   true,
                Description:  "The access key ID.",
            },
            "secret_access_key": schema.StringAttribute{
                Computed:    true,
                Sensitive:   true,
                Description:  "The secret access key.",
            },
            "session_token": schema.StringAttribute{
                Computed:    true,
                Sensitive:   true,
                Description:  "The session token.",
            },
            "expiration": schema.StringAttribute{
                Computed:    true,
                Description:  "The expiration time of the credential.",
            },
        },
    }
}

func (e *TemporaryCredentialEphemeralResource) Open(ctx context.Context, req ephemeral.OpenRequest, resp *ephemeral.OpenResponse) {
    var data TemporaryCredentialModel
    resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
    // POST /credentials/temporary with role and ttl
    // Populate result with access_key_id, secret_access_key, session_token, expiration
}

func (e *TemporaryCredentialEphemeralResource) Close(ctx context.Context, req ephemeral.CloseRequest, resp *ephemeral.CloseResponse) {
    // DELETE /credentials/temporary/{credentialId}
    // Best-effort cleanup
}
```

#### Ephemeral Context Restrictions

Ephemeral resource results can only be used in:
- Write-only arguments on managed resources (e.g., `password_wo`, `secret_string_wo`)
- Other ephemeral blocks
- Provider configuration (with `ephemeral` blocks)

They **cannot** be stored in state, passed to modules, or referenced in non-ephemeral contexts. Eidos documents these restrictions in the generated documentation.

### 8.9 List Resources (tfquery)

**List resources** (Terraform 1.14+) enable `terraform query` operations that search for resources within a scope. They stream results back to Terraform and can optionally include full resource data. List resources require a corresponding managed resource implementation (they share the same identity schema).

This is the natural Terraform mapping for OpenAPI `GET` collection endpoints — the same endpoints that also power data sources, but with a focus on listing/scanning rather than reading a single known instance.

#### How List Resources Map from OpenAPI

**Automatic detection**:
- `GET /pets` (collection endpoint) where the corresponding `GET /pets/{petId}` resource exists → List resource
- `GET /pets?filter=...` (parameterized collection) → List resource with config schema
- Operations annotated with `x-terraform-list: true` → List resource

**Explicit configuration** (`generator.yaml`):
```yaml
list_resources:
  - resource: "pet"                    # Must match a managed resource type name
    operation: "listPets"              # The GET collection operation
    config_schema:
      - name: "status"
        type: "string"
        optional: true
        description: "Filter pets by status"
      - name: "tags"
        type: "list(string)"
        optional: true
        description: "Filter pets by tags"
    pagination:
      style: "offset"
      page_param: "page"
      per_page_param: "limit"
```

#### Generated Code Structure

```go
// internal/provider/list_pet.go

type PetListResource struct {
    client *http.Client
}

func (l *PetListResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
    resp.TypeName = req.ProviderTypeName + "_pet"  // Must match the resource type name
}

func (l *PetListResource) ListResourceConfigSchema(ctx context.Context, req list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
    resp.Schema = listschema.Schema{
        Attributes: map[string]listschema.Attribute{
            "status": listschema.StringAttribute{
                Optional:    true,
                Description: "Filter pets by status.",
            },
            "tags": listschema.ListAttribute{
                ElementType: types.StringType,
                Optional:    true,
                Description: "Filter pets by tags.",
            },
        },
    }
}

func (l *PetListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
    var data PetListConfigModel
    diags := req.Config.Get(ctx, &data)
    // ... build query parameters from config ...
    // GET /pets?status={status}&page={page}&limit={limit}

    stream.Results = func(push func(list.ListResult) bool) {
        for _, pet := range pets {
            result := req.NewListResult(ctx)
            result.DisplayName = pet.Name
            result.Diagnostics.Append(result.Identity.Set(ctx, PetIdentityModel{
                PetId: types.StringValue(pet.ID),
            }))
            if req.IncludeResource {
                result.Diagnostics.Append(result.Resource.Set(ctx, PetResourceModel{
                    ID:     types.StringValue(pet.ID),
                    Name:   types.StringValue(pet.Name),
                    Status: types.StringValue(pet.Status),
                }))
            }
            if !push(result) {
                return
            }
        }
    }
}
```

### 8.10 Write-Only Arguments on Managed Resources

**Write-only arguments** (Terraform 1.10+) are a feature of managed resources that allows specific attributes to be passed during Create/Update but never stored in state or plan files. They are distinct from ephemeral resources — write-only arguments live on standard `resource` blocks, not `ephemeral` blocks.

Write-only arguments are the natural Terraform mapping for OpenAPI fields marked `writeOnly: true` on request bodies — fields that the API accepts but never returns in responses.

#### How Write-Only Arguments Map from OpenAPI

**Automatic detection**:
- Any request body property with `writeOnly: true` → Write-only argument on the resource
- Any parameter with `writeOnly: true` → Write-only argument
- Operations annotated with `x-terraform-write-only: true` on specific fields → Write-only argument

**Explicit configuration** (`generator.yaml`):
```yaml
resource_overrides:
  - schema: "DatabaseInstance"
    write_only_attributes:
      - name: "password"
        description: "The database master password. Not stored in state."
        sensitive: true
      - name: "initial_snapshot_id"
        description: "Snapshot to restore from. Only used on create."
```

#### Write-Only Argument Mechanics

Write-only arguments follow a versioned pattern:
1. The attribute is declared with `WriteOnly: true` in the Framework schema.
2. A companion `_wo_version` attribute (Int64) tracks the version of the write-only value.
3. When the write-only value changes, the user increments `_wo_version` to signal Terraform to send the new value.
4. Terraform sends the write-only value during Create/Update but **never** stores it in state.
5. On Read, the write-only value is not returned — the provider must not set it in the response state.

#### Generated Code Structure

```go
// In resource schema:
"password_wo": schema.StringAttribute{
    WriteOnly:   true,
    Optional:    true,
    Sensitive:   true,
    Description: "The database master password. Not stored in state.",
    PlanModifiers: []planmodifier.String{
        stringplanmodifier.UseStateForUnknown(),
    },
},
"password_wo_version": schema.Int64Attribute{
    Optional:    true,
    Description: "Increment this when the password changes.",
},

// In Create:
password := data.PasswordWO.ValueString()
// Send password in API request body
// Do NOT set password in state response

// In Read:
// Do NOT set password_wo in state (it's write-only)
// The API doesn't return it, and Terraform doesn't store it
```

#### Write-Only vs Ephemeral Resources

| Feature | Write-Only Arguments | Ephemeral Resources |
|---------|---------------------|---------------------|
| Lives on | `resource` block | `ephemeral` block |
| Stored in state? | No | No (entire resource) |
| Lifecycle | Create + Update | Open → Renew → Close |
| Referenced by | Only the owning resource | Write-only args, other ephemeral blocks, provider config |
| OpenAPI mapping | `writeOnly: true` on request body fields | Credential/token endpoints, `writeOnly` response fields |
| Terraform version | 1.10+ | 1.10+ |

---

## 9. Terraform Plugin Protocol Details

### 9.1 Protocol Version 6 (Primary)

Protocol v6 is the default and recommended target. It is compatible with Terraform CLI >= 1.0.

**Key capabilities**:
- **Nested Attributes**: Define `SchemaAttribute` with `NestedType` field, enabling argument syntax instead of block syntax.
- **Provider-Defined Functions**: Custom functions callable from HCL.
- **Ephemeral Resources**: Short-lived resources with write-only data that are never persisted in state (Terraform 1.10+).
- **Actions**: Side-effect operations invoked during `terraform apply` that don't produce persistent state (Terraform 1.13+).
- **List Resources**: `terraform query` search operations that stream results and can include resource data (Terraform 1.14+).
- **State Stores**: Pluggable state persistence via providers (experimental, Terraform 1.15+).

**Generated server setup**:

```go
func main() {
    opts := providerserver.ServeOpts{
        Address: "registry.terraform.io/hashicorp/mycloud",
        Debug:   os.Getenv("TF_DEBUG") != "",
    }
    err := providerserver.New(ctx, New("0.1.0"), opts)
    if err != nil {
        log.Fatal(err)
    }
}
```

### 9.2 Protocol Version 5 (Fallback)

Protocol v5 is compatible with Terraform CLI >= 0.12. It lacks nested attributes and provider-defined functions.

Eidos generates Protocol v5 code only when `--protocol-version=5` is specified. The generated code uses `tf5server.Serve()` and follows SDKv2 conventions (mapped through `terraform-plugin-mux` if needed).

---

### 9.3 gRPC Server Lifecycle

The generated provider binary:
1. Starts a gRPC server listening on a Unix domain socket (or named pipe on Windows).
2. Terraform CLI discovers and starts the binary via `terraform init`.
3. Terraform sends `ConfigureProvider`, `ValidateProviderConfig`, and then CRUD RPCs.
4. The provider processes each RPC, calls the generated HTTP client, and returns results.
5. On `SIGINT`, the provider gracefully shuts down.

The generated `main.go` handles debug mode (`TF_REATTACH_PROVIDERS`) for development.

### 9.4 DynamicValue Serialization

Terraform transmits resource state between CLI and provider as `DynamicValue` — MessagePack or JSON encoded `tftypes.Value`. The generated value mappers handle:

- **Plan → Model**: `req.Plan.Get(ctx, &model)` — Framework handles this natively.
- **Model → State**: `resp.State.Set(ctx, &model)` — Framework handles this natively.
- **Custom types**: For OpenAPI types that don't have native Terraform equivalents (e.g., `format: binary`), custom `basetypes.StringTypable` types are generated.

---

## 9.5 Dedicated OpenAPI Parser

Eidos does **not** depend on third-party OpenAPI parsing libraries such as `libopenapi` or `kin-openapi`. Instead, it ships a purpose-built parser inside `pkg/parser/` that is tailored to the needs of Terraform provider generation.

### Why a dedicated parser?

| Concern | Dedicated parser approach |
|---------|---------------------------|
| Spec-version parity | Single codebase normalizes 2.0, 3.0.x, and 3.1.x into the same internal model |
| Dependency surface | No external OpenAPI parser in `go.mod`; fewer breaking changes from upstream |
| Error context | Every IR node carries `SourceLocation` (file:line) for precise diagnostics |
| Feature control | Add support for new JSON Schema / OpenAPI keywords on Eidos's own schedule |
| Performance | Streaming YAML/JSON decode with optional memory budget and recursion limits |

### Parser structure

```
pkg/parser/
├── lexer.go            # Minimal YAML/JSON tokenization
├── spec.go             # Generic OpenAPI document model (version-agnostic nodes)
├── resolve.go          # $ref resolution, including external files and URLs
├── validate.go         # Structural validation and semantic warnings
├── v2/
│   └── convert.go      # Swagger 2.0 → generic spec model
├── v3_0/
│   └── convert.go      # OpenAPI 3.0.x → generic spec model
├── v3_1/
│   └── convert.go      # OpenAPI 3.1.x → generic spec model
└── parser_test.go      # Unit + fixture tests
```

### Parsing pipeline

1. **Detect version** from `swagger` (2.0) or `openapi` (3.0.x / 3.1.x).
2. **Decode raw bytes** into a thin, typed YAML/JSON AST.
3. **Version-specific conversion** maps raw AST nodes to the generic `Spec` model.
4. **Generic resolution** dereferences all `$ref` values, merges `allOf`, resolves polymorphism, and validates structural correctness.
5. **Return normalized model** to the transformer.

### Supported inputs

| Format | Detection |
|--------|-----------|
| YAML | File extension `.yaml`/`.yml` or leading `---` |
| JSON | File extension `.json` or leading `{`/`[` |

### `$ref` resolution

- Local JSON Pointers (`#/components/schemas/Pet`) are resolved against the in-memory spec.
- External file references (`./models.yaml#/Pet`) are loaded recursively with a cache.
- Remote references (`https://example.com/spec.yaml#/Pet`) are fetched with a bounded timeout and cache.
- Circular references are detected and reported as warnings; the referencing field is marked `Computed` with an opaque type.

### Validation

The parser reports diagnostics at the source location rather than failing silently:

| Issue | Diagnostic |
|-------|------------|
| Missing required field | Error with file:line |
| Invalid `$ref` target | Error with ref path and location |
| Type mismatch | Warning with suggested coercion |
| Unsupported keyword | Warning describing limitation |
| Circular `$ref` | Warning; field marked opaque |

### Technology stack update

Because the parser is dedicated, the following items are removed from the technology stack and architecture diagram:

- `github.com/pb33f/libopenapi`
- `github.com/getkin/kin-openapi`

These are replaced by the in-house parser packages. All parser tests use fixture specs stored in `test/specs/` and assert on the resulting normalized model and IR.

---

## 10. OpenAPI-to-Terraform Type Mapping

| OpenAPI `type` | OpenAPI `format` | Terraform Framework Type | Go Model Type |
|----------------|-------------------|------------------------|----------------|
| `string` | (none) | `schema.StringAttribute` | `types.String` |
| `string` | `date-time` | `schema.StringAttribute` | `types.String` (with RFC3339 validator) |
| `string` | `date` | `schema.StringAttribute` | `types.String` (with date validator) |
| `string` | `email` | `schema.StringAttribute` | `types.String` (with email validator) |
| `string` | `uuid` | `schema.StringAttribute` | `types.String` (with UUID validator) |
| `string` | `uri` | `schema.StringAttribute` | `types.String` (with URL validator) |
| `string` | `password` | `schema.StringAttribute` | `types.String` (`Sensitive: true`) |
| `string` | `byte` | `schema.StringAttribute` | `types.String` (base64) |
| `string` | `binary` | `schema.StringAttribute` | `types.String` (base64 encoded) |
| `integer` | (none) | `schema.Int64Attribute` | `types.Int64` |
| `integer` | `int32` | `schema.Int64Attribute` | `types.Int64` |
| `integer` | `int64` | `schema.Int64Attribute` | `types.Int64` |
| `number` | (none) | `schema.Float64Attribute` | `types.Float64` |
| `number` | `float` | `schema.Float64Attribute` | `types.Float64` |
| `number` | `double` | `schema.Float64Attribute` | `types.Float64` |
| `boolean` | (none) | `schema.BoolAttribute` | `types.Bool` |
| `array` | (of primitives) | `schema.ListAttribute` / `schema.SetAttribute` | `types.List` / `types.Set` |
| `array` | (of objects) | `schema.ListNestedAttribute` / `schema.SetNestedAttribute` | Nested model struct |
| `object` | (with properties) | `schema.SingleNestedAttribute` or `schema.SingleNestedBlock` | Nested model struct |
| `object` | (with additionalProperties) | `schema.MapAttribute` or `schema.MapNestedAttribute` | `types.Map` or nested map |
| `null` (in type array) | — | `Optional: true, Computed: true` | `types.String` (nullable) |
| `oneOf` | — | Dynamic or discriminated blocks | See Section 12 |
| `anyOf` | — | Dynamic or union blocks | See Section 12 |
| `allOf` | — | Flattened nested attribute | Merged model struct |
| `$ref` | — | Resolved inline | Dereferenced schema |
| (absent type) | — | `schema.DynamicAttribute` | `types.Dynamic` |

---

## 11. Security Scheme Handling

Each OpenAPI security scheme maps to provider-level configuration attributes and client auth middleware:

| Security Scheme | Provider Config Attributes | Client Auth Behavior |
|----------------|---------------------------|---------------------|
| `apiKey` (header) | `api_key` (Sensitive, String) | Inject `X-API-Key: <value>` header |
| `apiKey` (query) | `api_key` (Sensitive, String) | Append `?api_key=<value>` to URL |
| `apiKey` (cookie) | `api_key` (Sensitive, String) | Inject `Cookie: api_key=<value>` header |
| `http/basic` | `username` (String), `password` (Sensitive, String) | `Authorization: Basic <base64(user:pass)>` |
| `http/bearer` | `bearer_token` (Sensitive, String) | `Authorization: Bearer <token>` |
| `oauth2/client_credentials` | `client_id`, `client_secret` (Sensitive), `token_url`, `scopes` | Token exchange at `token_url`, cache token, inject `Authorization: Bearer <token>` |
| `oauth2/authorization_code` | `client_id`, `client_secret` (Sensitive), `auth_url`, `token_url`, `refresh_token` (Sensitive), `scopes` | Refresh-only: the initial code exchange is interactive and happens out-of-band; the provider refreshes the supplied `refresh_token` at `token_url` (handling rotation) and injects `Authorization: Bearer <token>` |
| `oauth2/implicit` | `auth_url`, `scopes` | No interceptor (interactive redirect; deprecated in OAuth 2.1); fail-loud runtime warning |
| `oauth2/password` | `username`, `password` (Sensitive), `client_id`, `client_secret` (Sensitive), `token_url`, `scopes` | Password-grant token exchange at `token_url`, cache token, inject Bearer |
| `openIdConnect` | `oidc_token_url`, `client_id`, `client_secret` (Sensitive) | Token endpoint from `oidc_token_url` override, else OIDC discovery (cached); client-credentials token fetch, inject Bearer |

Multiple security schemes on a single operation are resolved with **AND semantics**, not OR: an operation declaring exactly one security requirement authenticates with exactly that requirement's schemes via `client.WithSchemes(...)` (per-operation AND). An operation declaring no `security` inherits the global default (every configured scheme interceptor applies); an operation declaring a single empty requirement (`security: [{}]`) is unauthenticated. An operation (or global `security`) declaring **more than one requirement** — OR, where any one suffices — is ambiguous for a non-interactive provider: eidos applies every declared scheme (AND of all, which is stricter than OR) and emits a fail-loud Warning diagnostic (`warnPerOpORSecurity` for per-operation OR; the global case warns via `buildSecurityIR`). This is fail-loud, not silent — a non-interactive Terraform provider cannot reliably try/fallback across OR alternatives.

### 11.1 Provider-Level HTTP Trace Logging

In addition to the standard `terraform-plugin-log` logging, the generated provider supports **optional request/response trace logging to a file**. This is configured at the provider level in the generated `provider.go` schema and in `generator.yaml`:

```go
"log_file": schema.StringAttribute{
    Optional:    true,
    Description: "Path to a file where HTTP request/response traces are appended.",
},
"log_capture_request_headers": schema.BoolAttribute{
    Optional:    true,
    Description: "Capture request headers in the log file (sensitive headers redacted).",
},
"log_capture_request_body": schema.BoolAttribute{
    Optional:    true,
    Description: "Capture request bodies in the log file.",
},
"log_capture_response_headers": schema.BoolAttribute{
    Optional:    true,
    Description: "Capture response headers in the log file (sensitive headers redacted).",
},
"log_capture_response_body": schema.BoolAttribute{
    Optional:    true,
    Description: "Capture response bodies in the log file.",
},
"log_max_body_bytes": schema.Int64Attribute{
    Optional:    true,
    Description: "Maximum number of body bytes to write per log entry.",
},
```

All six attributes are emitted for every provider (all `Optional`, so existing practitioner configs are unaffected). In the wired-client branch of `Configure`, the provider builds a `client.LoggingConfig` from these attributes — seeded with any `generator.yaml` `logging` defaults carried on `ClientIR.Logging` — and appends `client.WithLogging(...)` when a log file is configured, so the generated client's `New` attaches the trace round-tripper. Capture flags default to *not* capturing bodies, so sensitive payloads are never written to disk unless explicitly enabled.

When `log_file` is set, the generated client wraps the underlying `http.Transport` with a logging round-tripper that:

1. Records the timestamp, method, URL, and sanitized headers.
2. Optionally records request/response body snippets up to `log_max_body_bytes`.
3. Redacts known sensitive headers (`Authorization`, `Cookie`, `X-API-Key`, etc.).
4. Appends one structured line per request/response to the configured file.
5. Is safe for concurrent use across resources, data sources, and actions.

This logging is **not** a replacement for `terraform-plugin-log`; it is an additional trace facility intended for debugging what the provider actually sends to the API. It is off by default and must be explicitly enabled by the practitioner.

---

## 12. Polymorphism & Complex Schemas

### `allOf` (Composition)

All schemas in `allOf` are merged into a single flat object. Property conflicts produce an error. Required fields from any subschema are combined.

```yaml
allOf:
  - $ref: '#/components/schemas/BasePet'
  - type: object
    properties:
      status:
        type: string
```

→ Single `SingleNestedAttribute` with all properties from `BasePet` plus `status`.

### `oneOf` (Exclusive Union)

`oneOf` is the most polymorphic OpenAPI construct and has no single Terraform equivalent. Eidos supports **two generation strategies** that the user can control globally or per-schema via `generator.yaml`.

#### Strategy A: Preserve as a dynamic union (default)

Without a `discriminator`: mapped to a `DynamicAttribute` (Terraform can hold any shape). The generated `Read` method populates the dynamic value based on the actual API response shape.

With a `discriminator`: mapped to type-switched nested blocks:

```yaml
oneOf:
  - $ref: '#/components/schemas/Cat'
  - $ref: '#/components/schemas/Dog'
discriminator:
  propertyName: petType
  mapping:
    cat: '#/components/schemas/Cat'
    dog: '#/components/schemas/Dog'
```

→ Generated as a `SingleNestedAttribute` containing all variant fields, with a `pet_type` `StringAttribute` that determines which validators apply. A custom `DiscriminatorValidator` ensures only fields for the active variant are set.

#### Strategy B: Split into separate Terraform resource types

For APIs where each `oneOf` variant models a logically distinct entity, Eidos can generate **one resource per variant** instead of a single union attribute. Each variant becomes a first-class Terraform resource with its own schema, CRUD methods, and documentation. The shared base schema (if any) is embedded in each generated resource.

Example:

```yaml
oneOf:
  - $ref: '#/components/schemas/Cat'
  - $ref: '#/components/schemas/Dog'
```

with generator configuration:

```yaml
polymorphism:
  strategy: "split_resources"
  oneOf:
    - schema: "Pet"
      variants:
        - schema: "Cat"
          resource_name: "cat"
          datasource_name: "cat"
        - schema: "Dog"
          resource_name: "dog"
          datasource_name: "dog"
```

→ Generates:
- `mycloud_cat` resource / data source from `Cat`
- `mycloud_dog` resource / data source from `Dog`
- The discriminator field (`pet_type`) is omitted because the Terraform resource type itself encodes the variant.

#### Strategy selection rules

| Heuristic | Default strategy | Rationale |
|-----------|------------------|-----------|
| `oneOf` appears inside a request/response body with a discriminator | Dynamic union with discriminator | Terraform can switch on the discriminator attribute |
| `oneOf` appears inside a request/response body without discriminator | Dynamic union | No reliable way to choose variant at plan time |
| Top-level resource schema is a `oneOf` of named variants | Split resources | Each variant is effectively a different resource kind |
| `x-terraform-polymorphism: split` extension present | Split resources | Explicit opt-in from spec author |
| `x-terraform-polymorphism: union` extension present | Dynamic union | Explicit opt-in from spec author |

When `strategy: split_resources` is chosen, Eidos also generates:
- A shared `Model` struct and helper functions if a base schema exists.
- The discriminator attribute is omitted from each variant's schema because the Terraform resource type itself encodes the variant (no per-variant "reject other variants" validator is emitted — the resource type is the switch).
- Documentation that clearly states which resource type corresponds to which API variant.

The default remains the existing dynamic-union behavior, so existing specs and configs continue to work unchanged.

### `anyOf` (Inclusive Union)

A top-level `anyOf` reaches the IR as a union (kind `AnyOf`) and renders with the same `dynamic_union` strategy as `oneOf`: a discriminated `anyOf` renders as a `SingleNestedAttribute` with a `DiscriminatorValidator`, and an undiscriminated one renders as a `DynamicAttribute`. Nested `anyOf` (inside properties, collection elements) is fail-loud: it emits a `warnCompositionNotModeled` Warning and falls back to `DynamicAttribute` because the flat Terraform attribute model cannot represent alternatives. No `AnyOfValidator` is emitted — the `DiscriminatorValidator` (allowed-keys check) is the only validator produced for a discriminated union.

### Circular References

When `$ref` forms a cycle (e.g., `Person` → `children: Person[]`), the referencing attribute is marked `Computed` with `MarkdownDescription: "Recursive type - see API documentation"`. The client serializes/deserializes using `json.RawMessage` for the recursive field.

---

## 13. Error Handling & Diagnostics

All errors from the API client are translated into Terraform `diag.Diagnostics`:

| API Error | Diagnostic |
|-----------|------------|
| 400 Bad Request | `diag.Errorf("API returned 400: %s", body)` with `Summary: "Bad Request"` |
| 401 Unauthorized | `diag.Errorf("Unauthorized: check provider credentials")` |
| 403 Forbidden | `diag.Errorf("Forbidden: insufficient permissions")` |
| 404 Not Found (on Read) | Mark resource as removed from state |
| 404 Not Found (on Delete) | Success (already deleted) |
| 409 Conflict | `diag.Errorf("Conflict: resource already exists")` |
| 422 Unprocessable Entity | Parse validation errors from response body |
| 429 Too Many Requests | Retry with exponential backoff |
| 500 Internal Server Error | `diag.Errorf("Internal server error")` |
| 502/503/504 | Retry with exponential backoff |
| Network error | `diag.Errorf("API request failed: %v", err)` |

Custom error response schemas (from `4xx`/`5xx` response definitions in the spec) are parsed into structured diagnostics.

---

## 14. Configuration & Overrides System

Users can place a `generator.yaml` beside the OpenAPI spec to customize generation:

```yaml
provider:
  name: "mycloud"
  display_name: "MyCloud Provider"
  version: "0.1.0"
  description: "A Terraform provider for MyCloud"
  author: "MyCloud Team"
  contact_email: "terraform@mycloud.io"
  license: "MPL-2.0"
  repository: "https://github.com/mycloud/terraform-provider-mycloud"
  protocol_version: 6

servers:
  - url: "https://api.mycloud.io/v1"
    description: "Production"
  - url: "https://staging-api.mycloud.io/v1"
    description: "Staging"

resource_overrides:
  - schema: "Pet"
    resource_name: "pet"
    id_attribute: "petId"
    import_format: "petId"
    timeouts:
      create: 30m
      read: 10m
      update: 30m
      delete: 10m
    force_new:
      - "name"
      - "species"
    computed_attributes:
      - "createdAt"
      - "updatedAt"
    sensitive_attributes:
      - "ownerSecret"
    write_only_attributes:
      - name: "password"
        description: "The pet's secret password. Not stored in state."
        sensitive: true
      - name: "initial_config"
        description: "One-time configuration applied on create."
    skip: false

  - operation: "listPets"
    generate_datasource: true
    datasource_name: "pets"
    generate_resource: false

  # Promote an operation inference classified as an action into a managed
  # resource, wiring each CRUD method to an explicit operation. This is the
  # escape hatch for entities whose create path differs from their read/delete
  # path (e.g. MyCloud dashboards: create on POST /dashboards/db, read/delete
  # on /dashboards/uid/{uid}).
  - operation: "postDashboard"
    resource_name: "dashboard"
    id_attribute: "uid"
    generate_resource: true
    create_operation: "postDashboard"
    read_operation: "getDashboardByUID"
    update_operation: "postDashboard"     # MyCloud updates via POST + overwrite
    delete_operation: "deleteDashboardByUID"

datasource_overrides:
  - operation: "getPetById"
    datasource_name: "pet"

action_overrides:
  - operation: "rebootServer"
    name: "reboot_server"
    description: "Reboots the specified server"
    progress_messages: true
    # Optional preflight endpoints (never auto-inferred — the spec encodes no
    # preflight semantics). Each is a "METHOD /path" string. When set, the
    # generated ModifyPlan/ValidateConfig body calls the endpoint and surfaces
    # a non-success status as an error diagnostic; on success ModifyPlan leaves
    # the plan unchanged (the spec encodes no plan mutations) and
    # ValidateConfig simply returns.
    modify_plan_operation: "POST /servers/{server_id}/reboot/preview"
    validate_config_operation: "POST /servers/{server_id}/reboot/validate"
  - operation: "rotateDatabaseCredentials"
    name: "rotate_database_credentials"
    description: "Rotates database credentials immediately"

ephemeral_resource_overrides:
  - operation: "generateTemporaryCredentials"
    name: "temporary_credential"
    description: "Generates short-lived credentials"
    open_mapping: "POST /credentials/temporary"
    close_mapping: "DELETE /credentials/temporary/{credentialId}"
    renew_mapping: "POST /credentials/temporary/{credentialId}/renew"
    result_fields:
      - name: "access_key_id"
        type: "string"
        sensitive: true
      - name: "secret_access_key"
        type: "string"
        sensitive: true
      - name: "session_token"
        type: "string"
        sensitive: true
      - name: "expiration"
        type: "string"

list_resource_overrides:
  - resource: "pet"
    operation: "listPets"
    config_schema:
      - name: "status"
        type: "string"
        optional: true
        description: "Filter pets by status"
      - name: "tags"
        type: "list(string)"
        optional: true
        description: "Filter pets by tags"
    pagination:
      style: "offset"
      page_param: "page"
      per_page_param: "limit"

function_overrides:
  - operation: "ipLookup"
    name: "lookup_ip"
    arguments:
      - name: "ip"
        type: "string"
    return_type: "string"

logging:
  enabled: true
  # Optional file path; when set, every HTTP request/response is appended to this file.
  # Uses the generated provider's own trace format (timestamp, method, URL, headers sanitized,
  # request body preview, response status, response body preview).
  # If omitted, logs go only to the standard Terraform provider log (terraform-plugin-log).
  file_path: "./provider.log"
  # What to capture per request/response.
  capture_request_headers: true
  capture_request_body: true
  capture_response_headers: true
  capture_response_body: true
  # Maximum body bytes to log (avoid writing huge payloads).
  max_body_bytes: 4096
  # Redact sensitive headers (case-insensitive).
  redact_headers:
    - "Authorization"
    - "X-API-Key"
    - "Cookie"

auth:
  - scheme: "apiKey"
    header_name: "X-API-Key"
    env_var: "MYCLOUD_API_KEY"
  - scheme: "oauth2"
    flow: "client_credentials"
    client_id_env: "MYCLOUD_CLIENT_ID"
    client_secret_env: "MYCLOUD_CLIENT_SECRET"
    token_url: "https://auth.mycloud.io/oauth2/token"

naming:
  resource_prefix: ""              # e.g., "mycloud_" → mycloud_pet
  datasource_prefix: ""            # e.g., "mycloud_" → data.mycloud_pet
  resource_suffix: ""              # Appended to all resource names
  transform: "snake_case"          # snake_case, camelCase, PascalCase

skip_operations:
  - "DELETE /admin/pets"           # Don't generate a resource for this
  - "OPTIONS *"

global_timeouts:
  create: 20m
  read: 10m
  update: 20m
  delete: 10m

pagination:
  style: "offset"                  # offset, cursor, link_header
  page_param: "page"
  per_page_param: "per_page"
  total_count_header: "X-Total-Count"
  next_link_header: "Link"
```

---

## 15. Output Directory Structure

```
terraform-provider-mycloud/
├── main.go                                    # Provider server entrypoint
├── go.mod                                     # Module definition
├── go.sum                                     # Dependency checksums
├── GNUmakefile                                # Build, test, lint targets
├── Makefile                                   # Symlink to GNUmakefile
├── .goreleaser.yml                            # Release automation
├── .golangci.yml                              # Linter config
├── .github/
│   └── workflows/
│       ├── ci.yml                             # CI: lint, test
│       └── release.yml                        # CD: goreleaser + registry publish
├── terraform-registry-manifest.json           # Registry manifest
├── README.md                                  # Auto-generated overview
├── internal/
│   ├── provider/
│   │   ├── provider.go                        # Provider struct, Schema, Configure, Resources, DataSources, Actions, EphemeralResources, ListResources, Functions
│   │   ├── provider_test.go                   # Unit tests
│   │   ├── resource_pet.go                    # Pet resource: Create, Read, Update, Delete, ImportState
│   │   ├── resource_pet_test.go               # Unit tests
│   │   ├── data_source_pet.go                 # Pet data source: Read
│   │   ├── data_source_pet_test.go            # Unit tests
│   │   ├── data_source_pets.go                # Pets list data source
│   │   ├── action_reboot_server.go            # Reboot server action: Invoke, ModifyPlan, progress messages
│   │   ├── action_reboot_server_test.go       # Action unit tests
│   │   ├── ephemeral_temporary_credential.go  # Ephemeral resource: Open, Renew, Close
│   │   ├── ephemeral_temporary_credential_test.go # Ephemeral resource tests
│   │   ├── list_pet.go                        # Pet list resource: List (for tfquery)
│   │   ├── list_pet_test.go                   # List resource tests
│   │   ├── model_pet.go                       # Pet Go struct models
│   │   └── validators.go                      # Custom validators (discriminator, etc.)
│   ├── client/
│   │   ├── client.go                          # HTTP client: request construction, retry
│   │   ├── auth.go                            # Auth middleware: apiKey, Bearer, OAuth2, Basic
│   │   ├── models.go                          # Request/response Go structs
│   │   ├── errors.go                          # Typed error structs
│   │   ├── retry.go                           # Exponential backoff + jitter
│   │   └── pagination.go                      # Pagination helpers
│   └── protocol/
│       └── value_mappers.go                   # tftypes.Value ↔ Go struct converters
├── docs/
│   ├── index.md                               # Provider overview + auth guide
│   ├── resources/
│   │   └── pet.md                             # Pet resource documentation
│   ├── data-sources/
│   │   ├── pet.md                             # Pet data source documentation
│   │   └── pets.md                            # Pets list data source documentation
│   ├── actions/
│   │   └── reboot_server.md                  # Action documentation
│   ├── ephemeral-resources/
│   │   └── temporary_credential.md           # Ephemeral resource documentation
│   └── functions/
│       └── lookup_ip.md                       # Provider-defined function docs
├── examples/
│   ├── resources/
│   │   └── pet/
│   │       └── resource.tf                    # Minimal HCL example
│   ├── data-sources/
│   │   ├── pet/
│   │   │   └── data-source.tf
│   │   └── pets/
│   │       └── data-source.tf
│   ├── actions/
│   │   └── reboot_server/
│   │       └── action.tf                     # Action invocation example
│   ├── ephemeral/
│   │   └── temporary_credential/
│   │       └── ephemeral.tf                   # Ephemeral resource example
│   └── provider/
│       └── provider.tf                       # Provider config example
├── templates/                                 # (optional) Custom Go templates for overrides
├── tests/                                      # terraform test framework files
│   └── pet.tftest.hcl                          # Native Terraform test for pet resource
├── tools/
│   └── tools.go                                # go generate tool dependencies
└── generator.yaml                             # Configuration used to generate this provider
```

---

## 16. Technology Stack

| Layer | Technology | Purpose |
|-------|-----------|---------|
| Language | Go 1.26+ | Provider code generation |
| OpenAPI Parsing | In-house `pkg/parser/` | Dedicated 2.0/3.0/3.1 parser with source-location diagnostics |
| YAML/JSON Parsing | `gopkg.in/yaml.v3`, `encoding/json` | Decode raw spec bytes into AST |
| Fallback Parsing | None | No external fallback parser; all edge cases handled in dedicated parser |
| Terraform SDK | [`terraform-plugin-framework`](https://github.com/hashicorp/terraform-plugin-framework) v1.x | Schema, CRUD, validators, plan modifiers |
| Protocol | [`terraform-plugin-go`](https://github.com/hashicorp/terraform-plugin-go) v0.31+ | Protocol v6/v5 server |
| Testing | [`terraform-plugin-testing`](https://github.com/hashicorp/terraform-plugin-testing) | Acceptance tests |
| Documentation | [`terraform-plugin-docs`](https://github.com/hashicorp/terraform-plugin-docs) | Registry-compatible Markdown generation |
| CLI | [`spf13/cobra`](https://github.com/spf13/cobra) v1.x | Command-line interface |
| Templates | [`text/template`](https://pkg.go.dev/text/template) for text files; `pkg/generator/astgen` (`go/ast` + `go/format`) for Go source files | Go code generation |
| HTTP Client | [`net/http`](https://pkg.go.dev/net/http) | Generated API client |
| Retry | [`github.com/hashicorp/go-retryablehttp`](https://github.com/hashicorp/go-retryablehttp) | Exponential backoff |
| Release | [`goreleaser/goreleaser`](https://goreleaser.com) | Multi-platform binary builds, signing, GitHub release |
| Linting | [`golangci-lint`](https://golangci-lint.run) | Code quality |
| YAML/JSON | [`gopkg.in/yaml.v3`](https://gopkg.in/yaml.v3), [`encoding/json`](https://pkg.go.dev/encoding/json) | Spec and config parsing |

---

## 17. Provider Development Workflow

Eidos generates a complete development workflow for the generated provider, including build tooling, debugging support, and CI/CD.

### 17.1 Build & Install

**Generated `GNUmakefile` targets**:

```makefile
default: fmt lint install generate

build:
    go build -v ./...

install: build
    go install -v ./...

lint:
    golangci-lint run

generate:
    cd tools; go generate ./...

fmt:
    gofmt -s -w -e .

test:
    go test -v -cover -timeout=120s -parallel=10 ./...

testacc:
    TF_ACC=1 go test -v -cover -timeout 120m ./...

.PHONY: fmt lint test testacc build install generate
```

### 17.2 Version Injection

The generated `main.go` includes a package-level `version` variable that is injected at build time via `ldflags`:

```go
// main.go
var version string = "dev"  // overridden at release build time
```

**GoReleaser ldflags**:
```yaml
ldflags:
  - '-s -w -X main.version={{.Version}} -X main.commit={{.Commit}}'
```

| Flag | Purpose |
|------|---------|
| `-s` | Omit symbol table (reduces binary size) |
| `-w` | Omit DWARF debug info |
| `-X main.version={{.Version}}` | Inject release tag (e.g., `1.2.3`) |
| `-X main.commit={{.Commit}}` | Inject short commit SHA |

### 17.3 Local Development Overrides

For local provider testing without publishing, users configure `dev_overrides` in `~/.terraformrc`:

```hcl
provider_installation {
  dev_overrides {
    "mycloud/mycloud" = "/home/dev/go/bin"
  }
  direct {}
}
```

- The path is a **directory** containing the provider binary.
- Skips version constraints and checksum verification.
- `terraform init` is not required (and will warn about the override).
- Requires Terraform >= 0.14.

### 17.4 Provider Logging

The generated provider uses `terraform-plugin-log` for structured logging. Key environment variables:

| Variable | Scope |
|----------|-------|
| `TF_LOG` | All loggers (core + SDKs + providers) |
| `TF_LOG_PATH` | Redirect log output to a file |
| `TF_LOG_PROVIDER` | All providers during the run |
| `TF_LOG_PROVIDER_{NAME}` | Specific named provider (e.g., `TF_LOG_PROVIDER_MYCLOUD=DEBUG`) |
| `TF_LOG_SDK_FRAMEWORK` | `terraform-plugin-framework` only |
| `TF_LOG_SDK_PROTO` | Protocol (gRPC) layer only |
| `TF_LOG_SDK_PROTO_DATA_DIR` | Write raw gRPC payload files to disk for wire-level debugging |

**Log levels** (least → most verbose): `OFF`, `ERROR`, `WARN`, `INFO`, `DEBUG`, `TRACE`.

### 17.5 Debugging with Delve

The generated `main.go` supports debug mode via a `-debug` flag:

```go
func main() {
    var debug bool
    flag.BoolVar(&debug, "debug", false, "enable debugger support")
    flag.Parse()
    providerserver.Serve(context.Background(), provider.New(), providerserver.ServeOpts{
        Address: "registry.terraform.io/mycloud/mycloud",
        Debug:   debug,
    })
}
```

**Debug workflow**:
1. Build without optimization: `go build -gcflags="all=-N -l" -o ./terraform-provider-mycloud .`
2. Start via Delve: `dlv exec --accept-multiclient --continue --headless ./terraform-provider-mycloud -- -debug`
3. Provider prints a `TF_REATTACH_PROVIDERS` JSON string to stdout.
4. Run Terraform with that env var: `TF_REATTACH_PROVIDERS='...' terraform apply`
5. Terraform reattaches to the already-running provider process (breakpoints work).

### 17.6 `go generate` Integration

The generated provider includes a `tools/` subdirectory for dev-only tool dependencies:

**`tools/tools.go`** (build-tagged to never compile into the provider binary):
```go
//go:build generate

package tools

import (
    _ "github.com/hashicorp/copywrite"
    _ "github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs"
)

//go:generate go run github.com/hashicorp/copywrite headers -d .. --config ../.copywrite.hcl
//go:generate terraform fmt -recursive ../examples/
//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-dir .. -provider-name mycloud
```

This produces:
- Copyright headers on all Go source files.
- Formatted example `.tf` files in `examples/`.
- Registry-ready documentation Markdown in `docs/`.

### 17.7 Multi-Platform Builds

The generated `.goreleaser.yml` targets these GOOS/GOARCH combinations:

| Tier | GOOS | GOARCH |
|------|------|--------|
| Required (HCP Terraform) | linux | amd64 |
| Primary | darwin | amd64 |
| Primary | darwin | arm64 |
| Primary | linux | amd64 |
| Primary | linux | arm64 |
| Primary | linux | arm (ARMv6) |
| Primary | windows | amd64 |
| Secondary | linux, windows, freebsd | 386 |
| Secondary | freebsd | amd64 |

`CGO_ENABLED=0` is set for all targets. `darwin/386` and `windows/arm` are explicitly excluded.

### 17.8 Provider Source Address

The provider's full source address follows the format `registry.terraform.io/<namespace>/<type>`:
- **Namespace** = GitHub organization or username that owns the provider repository.
- **Type** = provider name (e.g., `mycloud`).
- The `registry.terraform.io/` prefix can be omitted in HCL as shorthand (e.g., `mycloud/mycloud`).
- The GitHub repo must be named `terraform-provider-{NAME}` and be public.

### 17.9 Terraform Lifecycle & Meta-Arguments

The generated provider is compatible with all standard Terraform meta-arguments:

| Meta-Argument | Provider Impact |
|---------------|----------------|
| `lifecycle.prevent_destroy` | Terraform Core enforces; no provider code needed |
| `lifecycle.ignore_changes` | Terraform Core enforces; provider receives the ignored values as prior state |
| `lifecycle.replace_triggered_by` | Terraform Core triggers destroy+create; provider handles normally |
| `lifecycle.create_before_destroy` | Terraform Core manages ordering; provider must handle two simultaneous instances |
| `for_each` / `count` | Terraform Core manages multiple instances; provider receives one instance per CRUD call |
| `depends_on` | Terraform Core manages ordering; no provider code needed |
| `provider` alias | Multiple provider instances with different configs; provider handles normally |
| `required_providers` | User declares provider source and version constraint in HCL |
| `.terraform.lock.hcl` | Terraform Core manages; provider must be signed for checksum verification |
| `terraform_data` | Can trigger actions via `replace_triggered_by` |
| `terraform import` | Provider's `ImportState` method handles ID parsing |
| `terraform state mv`/`rm`/`list`/`show` | Terraform Core manages; no provider code needed |
| `terraform console` | Provider's Read/DataSource methods are called for evaluation |
| `terraform graph` | Terraform Core manages; provider resources appear as nodes |
| `terraform providers schema` | Terraform Core extracts schema from provider; works automatically |
| `terraform providers lock`/`mirror` | Terraform Core manages; provider must be published with checksums |

---

## 18. Testing Strategy

### 18.1 Unit Tests

- **IR assertions**: Every parser/normalizer/transformer function has unit tests that verify IR output against expected fixtures.
- **Golden file tests**: Run the generator against reference OpenAPI specs (Mycloud, Mycloud Pets, Mycloud Data, and the structural corpus) and compare emitted Go files to checked-in snapshots.
- **Type mapping tests**: Verify OpenAPI → IR → Terraform type conversions.

### 18.2 Acceptance Tests

- **Generated acceptance tests**: Each resource gets `TestAcc<ResourceName>_Create`, `_Update`, `_Delete`, `_Import` tests.
- **Mock HTTP server**: `httptest.NewServer()` handlers return fixture data derived from OpenAPI examples.
- **Terraform operations tested**: `terraform apply`, `terraform plan`, `terraform import`, `terraform destroy`.
- **Error scenarios**: 4xx, 5xx, network timeout, retry exhaustion.

### 18.3 Protocol Compliance Tests

- **Schema validation**: Use `terraform-plugin-framework` internal schema validation to ensure generated schemas are valid.
- **State round-trip**: Verify that `Plan → Model → State → Model` round-trips preserve data.
- **Import tests**: Verify that composite import IDs are parsed correctly.

### 18.4 End-to-End Integration Tests

- **Real API tests** (opt-in): Against a live API with a test account, using `TF_ACC=1`.
- **Multi-spec regression**: Run the generator against 10+ real-world OpenAPI specs and verify the generated code compiles and passes `go vet`.
- **Reference live e2e (Mycloud)**: the strongest validation case run so far. Eidos generates a provider from the reference Mycloud spec (`test/specs/mycloud.yaml`), injects a live connectivity test, and runs the generated acceptance suite with `TF_ACC=1` against a local deterministic mock server that validates the generated client's auth plumbing (`testfixtures/live`). No external system is involved.

### 18.5 Test Infrastructure

```
test/
├── specs/                    # Reference OpenAPI specs
│   ├── mycloud.yaml
│   ├── mycloud-pets.yaml
│   ├── mycloud-data.yaml
│   └── complex-polymorphism.yaml
├── fixtures/                 # Expected output
│   ├── mycloud/
│   │   ├── internal/
│   │   │   └── provider/
│   │   │       ├── provider.go
│   │   │       ├── resource_pet.go
│   │   │       └── ...
│   │   └── ...
│   └── ...
└── generator_test.go         # Integration tests
```

### 18.6 `terraform test` Framework

Terraform 1.6+ introduced a native testing framework using `.tftest.hcl` files. Eidos generates `.tftest.hcl` test files alongside the provider for integration testing without Go code:

```hcl
# tests/pet.tftest.hcl
provider "mycloud" {
  host     = "localhost"
  port     = 8080
  api_key  = "test-key"
}

run "create_pet" {
  command = apply

  variables {
    name   = "Fluffy"
    status = "available"
  }

  assert {
    condition     = mycloud_pet.test.name == "Fluffy"
    error_message = "Pet name should be Fluffy"
  }

  assert {
    condition     = mycloud_pet.test.status == "available"
    error_message = "Pet status should be available"
  }
}

run "update_pet" {
  command = apply

  variables {
    name   = "Fluffy Updated"
    status = "sold"
  }

  assert {
    condition     = mycloud_pet.test.name == "Fluffy Updated"
    error_message = "Pet name should be updated"
  }
}

run "delete_pet" {
  command = destroy

  assert {
    condition     = length(mycloud_pet.test) == 0
    error_message = "Pet should be destroyed"
  }
}
```

**Generation trigger**: `.tftest.hcl` files are generated only when `eidos generate` is invoked with `--generate-terraform-tests` or when `generator.yaml` contains:

```yaml
generate_terraform_tests: true
```

When disabled (the default), no `tests/` directory is emitted, keeping the output minimal.

**Generated test coverage**:
- `run` blocks for Create, Update, Import, and Destroy operations.
- `assert` blocks derived from OpenAPI response schemas and examples.
- `variables` blocks populated from OpenAPI `example` and `x-examples` fields.
- Mock HTTP server configuration via provider attributes (`host`, `port`).
- Optional provider-level `log_file` assertions for trace-output verification.

---

## 19. Terraform Registry Publishing

Eidos generates all artifacts required for Terraform Registry publishing:

| Artifact | Purpose |
|----------|---------|
| `terraform-registry-manifest.json` | Registry metadata (`protocol_versions: ["6.0"]` or `["5.0"]`) |
| `.goreleaser.yml` | Multi-OS/arch binary builds, SHA256SUMS, GPG signing |
| `.github/workflows/release.yml` | CI/CD: tag → build → sign → publish |
| `docs/` | Registry-rendered documentation |
| `examples/` | Registry-rendered examples |
| `README.md` | Provider overview with installation instructions |

**Registry naming requirement**: The GitHub repository must be named `terraform-provider-<NAME>`, where `<NAME>` matches the provider type name.

**Signing**: GPG key pairs must be generated and the public key uploaded to the Registry. `goreleaser` signs the SHA256SUMS file automatically.

---

## 20. Roadmap

Eidos is developed incrementally against the design in this document. The
authoritative, feature-level view of what is currently usable versus scaffolded
is kept in [§1.1 Implementation Status](#11-implementation-status); the
limitations listed there are the active work items. In broad strokes the focus
is:

- **Wire generated bodies to real remote APIs** — resource CRUD, data-source
  Read, action Invoke, ephemeral Open/Renew/Close, and list-resource List bodies
  are all wired to the generated client when the IR is resolvable (the generated
  acceptance tests exercise them against a stateful in-process mock server);
  only provider-defined functions remain honestly scaffolded by design (no
  remote endpoint). The reference corpus has been enriched so the checked-in
  specs exercise wired bodies at scale (the mycloud spec wires 7 resources,
  17 data sources, and 12 list resources; see
  [§23](#23-remaining-gaps--accepted-limitations) for the resolution record).
- **Heuristic auto-detection** — extend OpenAPI operation heuristics so that
  ephemeral resources and functions are inferred from specs that declare
  lifecycle subpaths (`/renew`, `/close`, `/revoke`) or function-classified
  operations; actions, list resources, and data sources are already inferred
  without explicit `generator.yaml` overrides.
- **Schema completeness** — the remaining JSON Schema 2020-12 keywords
  (`uniqueItems` → Set is now implemented for resources, data sources, actions,
  and ephemerals; list resources fall back to List with a fail-loud warning
  because the upstream `list/schema` package has no Set types; block-level state
  upgrades are now modeled; see §8.4).
- **End-to-end validation** — compile-checking generated providers against a
  growing corpus of real-world specs, and live API tests under `TF_ACC=1`.

The historical phase plan that originally scoped this work has been superseded
by the implementation-status matrix and is no longer maintained as a checklist.

---

## 21. Risks & Mitigations

| # | Risk | Likelihood | Impact | Mitigation |
|---|------|-----------|--------|------------|
| 1 | OpenAPI feature explosion (`oneOf` with deeply nested `$ref`) | High | High | Strict IR normalization; emit warnings with file:line for unsupported constructs rather than fail silently. Recursive depth limit with clear error. |
| 2 | Terraform type system mismatch (OpenAPI `any`, `oneOf` without discriminator) | High | Medium | Map to `DynamicAttribute` with generated validators; document limitations in generated docs. |
| 3 | Large spec → huge generated provider (1000+ resources) | Medium | Medium | Support resource filtering via `generator.yaml` allow-list; code splitting per resource; lazy schema registration. |
| 4 | Breaking changes in `terraform-plugin-framework` | Low | High | Pin framework version in generated `go.mod`; generate compatibility shims; test against multiple Terraform CLI versions. |
| 5 | Security scheme complexity (OAuth2 authorization code, OIDC) | Medium | Medium | Generate provider config blocks for tokens; leave full interactive flows to user-supplied middleware; document manual setup steps. |
| 6 | Circular `$ref` in specs | Medium | Low | Detect cycles during normalization; mark circular fields as `Computed` with opaque type; emit warning. |
| 7 | Ambiguous CRUD inference (multiple POST endpoints for same resource) | Medium | Medium | Require explicit `generator.yaml` override for ambiguous operations; emit error listing candidates. |
| 8 | Non-RESTful APIs (GraphQL, gRPC, WebSocket) | Low | Low | Document that Eidos targets REST/HTTP APIs only; non-REST APIs produce warnings. |
| 9 | Spec version incompatibilities (2.0 vs 3.0 vs 3.1 edge cases) | Medium | Medium | Dedicated in-house parser with a comprehensive version-specific test suite; no reliance on third-party parser behavior. |
| 10 | Terraform state drift for PATCH-only APIs | Medium | Medium | Generate drift-correcting Read after Update; configurable via `generator.yaml`. |
| 11 | Generated code readability and maintainability | Medium | High | Use `pkg/generator/astgen` (`go/ast` + `go/format`) for programmatic Go generation; follow HashiCorp's provider conventions; add comments with spec source references. |
| 12 | Multi-file specs (`$ref` to external files/URLs`) | Medium | Medium | Support recursive resolution of local and remote `$ref` targets with caching and cycle detection. |
| 13 | State Stores (experimental Terraform 1.15+) | Low | Low | State Stores are currently experimental and offered without compatibility promises. Eidos tracks the feature but does not generate State Store implementations until GA. Document as a future milestone. |
| 14 | Actions, Ephemeral Resources, List Resources require Terraform CLI >= 1.10–1.14 | Low | Medium | Generated providers set minimum Terraform version constraints appropriately. Actions require >= 1.13, Ephemeral require >= 1.10, List Resources require >= 1.14. Providers targeting older Terraform versions will not include these constructs. |

---

## 22. Glossary

| Term | Definition |
|------|-----------|
| **Eidos** | The name of this project; from Greek ἰδέα (form, pattern) |
| **OpenAPI** | A specification for describing RESTful APIs (versions 2.0, 3.0, 3.1) |
| **Swagger** | The original name for OpenAPI 2.0 |
| **IR** | Intermediate Representation — the normalized model between OpenAPI and Terraform |
| **Provider** | A Terraform plugin that manages a specific API/service |
| **Action** | A Terraform Plugin Framework abstraction for side-effect operations that don't manage CRUD lifecycle state. Invoked directly via CLI or during `terraform apply`. Maps to non-CRUD OpenAPI operations. |
| **Ephemeral Resource** | A Terraform resource type (1.10+) whose data is guaranteed not to persist in state or plan files. Follows an Open → Renew → Close lifecycle. Maps to credential/token/temporary-data API operations. |
| **List Resource** | A Terraform resource type (1.14+) that enables `terraform query` search operations. Streams results and can optionally include full resource data. Maps to OpenAPI collection GET endpoints. |
| **State Store** | A Terraform concept (experimental, 1.15+) for pluggable state persistence via providers. Not generated by Eidos currently. |
| **Write-Only Argument** | A resource argument that Terraform does not store in state or plan. Used with ephemeral resources to pass sensitive data without persistence. Maps to OpenAPI `writeOnly: true` fields. |
| **WriteOnly** | A Terraform Plugin Framework attribute flag (1.10+) that marks an attribute as never persisted in state. Requires a companion `_wo_version` attribute. |
| **JSON Schema 2020-12** | The JSON Schema specification version that OpenAPI 3.1 aligns with. Introduces `not`, `const`, `if`/`then`/`else`, `dependentRequired`, `dependentSchemas`, `patternProperties`, `propertyNames`, `minProperties`/`maxProperties`, `exclusiveMinimum`/`exclusiveMaximum` (as numbers), `multipleOf`, and `unevaluatedProperties`. |
| **tfquery** | The `terraform query` CLI command (1.14+) that uses List Resources to search for resources within a scope. |
| **`.tftest.hcl`** | Terraform's native testing framework (1.6+) using HCL-based test files with `run` and `assert` blocks. |
| **`dev_overrides`** | A Terraform CLI configuration in `~/.terraformrc` that redirects provider resolution to a local binary directory for development. |
| **`TF_REATTACH_PROVIDERS`** | An environment variable used for debugging providers with Delve; contains a JSON string with the provider's gRPC connection details. |
| **`go generate`** | A Go toolchain command that runs directives embedded in Go source files; used by Eidos to generate documentation and format examples. |
| **`ldflags`** | Go linker flags used to inject version and commit information at build time via `-X main.version=...`. |
| **Resource** | A Terraform construct representing an infrastructure object that can be created, read, updated, and deleted |
| **Data Source** | A Terraform construct representing a read-only reference to external data |
| **CRUD** | Create, Read, Update, Delete — the four primary resource operations |
| **Plan Modifier** | A Terraform Plugin Framework concept that modifies the planned state of a resource |
| **Validator** | A Terraform Plugin Framework concept that validates attribute values at plan time |
| **DynamicValue** | The serialized representation of Terraform state, transmitted via gRPC |
| **tftypes** | The Go package in `terraform-plugin-go` that defines Terraform's type system |
| **tfprotov6** | Protocol version 6 — the gRPC protocol between Terraform CLI and providers |
| **tfprotov5** | Protocol version 5 — legacy protocol (SDKv2 compatible) |
| **Framework** | `terraform-plugin-framework` — the recommended SDK for building providers |
| **SDKv2** | `terraform-plugin-sdk/v2` — the legacy SDK for building providers |
| **$ref** | JSON Pointer reference in OpenAPI specs, used for schema reuse |
| **discriminator** | An OpenAPI 3.0 construct for polymorphic type switching |
| **allOf/oneOf/anyOf** | OpenAPI composition keywords for merging, selecting, or combining schemas |
| **GoReleaser** | A tool for automating Go binary releases across multiple platforms |

---

## 23. Remaining Gaps & Accepted Limitations

This section is the canonical register of work that is genuinely open plus the
accepted, documented limitations of the generator. It supersedes the earlier
standalone `docs/FINAL_GAPS.md` register, which has been folded in here and
removed. The detailed per-construct limitation text lives in
[`docs/usage.md`](usage.md#current-limitations) §Current limitations; the
history of what was closed is in `CHANGELOG.md` [Unreleased].

### 23.1 Resolved (closed work)

The following items were tracked as open in the FINAL_GAPS audit and are now
implemented; `CHANGELOG.md` [Unreleased] records the detail:

| Item | Resolution |
|------|-----------|
| Reference corpus barely wired | the mycloud reference spec (merging the two former external-shape reference specs) carries real response schemas exposing path params; list resources populate their config schema from the collection path's parameters (`transformer.ListResourceConfigSchema`). The mycloud spec wires 7 resources, 17 data sources, and 12 list resources; the corpus-wide wired share went 1→7 resources, 7→25 data sources, 1→12 list resources. `assertCorpusWiring` in the golden test guards per-spec wiring floors. |
| Swagger 2.0 formData end-to-end | `paramsFromOperation` and `transformer.createFormDataParams` decompose the v2 request-body form schema back into per-field parameters; primitive fields wire as `application/x-www-form-urlencoded` and binary uploads as `multipart/form-data` (`swagger-formdata` reference spec). |
| Ephemeral/function inference | `ephemeralFromOperation` populates config/result schemas and consumes lifecycle subpaths; `ephemeral-resources` and `provider-functions` reference specs exercise the inference end to end. |
| Remote `--spec` URL fetching | `eidos generate`/`generate-config --spec` accept an http(s) URL with scheme allowlist, SSRF guard, size/timeout caps, and env-var-only auth. |
| Release-please migration | Versioning and changelog generation moved to Google's release-please; GoReleaser runs in the release-please workflow. |
| Stale documentation | `CHANGELOG.md` §Known limitations and this roadmap corrected. |
| Live-API e2e validation | Generated providers from the reference Mycloud spec build, load their schemas, and pass a full connectivity/CRUD lifecycle against a local deterministic mock server (`testfixtures/live`, `TF_ACC=1`); no external system is involved. The G1–G21 gap register is closed (rows below). |
| Framework-valid generated schemas (G2/G3/G4/G11/G12/G15) | Unrepresentable nested response shapes render as a top-level `DynamicAttribute` (dropped when nested inside a collection, where the framework rejects Dynamic); model files with `[]tftypes.Value` fields import `tftypes` and the mapper copies raw values instead of decoding to a primitive; the framework-rejected classes — dynamic-element collections, `DynamicAttribute` inside a `NestedAttributeObject`, invalid attribute names, and `Computed`+`Required` — are all fixed. |
| Wire fidelity: camelCase keys + uid path substitution (G14/G18/G19/G20) | `ir.AttributeIR` carries `WireName` (the original property/param name) and model fields emit a `json:"<wireName>"` tag so snake_cased attribute names round-trip against specs that use camelCase property names; `applyJSONToModel` null-defaults attributes the response did not carry and echoed request inputs become Optional+Computed; `resolvePathSubstitution` prefers the resource's `uid` attribute for UID-shaped path placeholders (`uid`, `*_uid`); every wired Update calls `preserveStateIntoPlan` to carry known state values (e.g. optimistic-concurrency `version`) into the plan. |
| `resource_overrides` per-CRUD promotion (G8/G9) | `generate_resource` plus explicit `create_operation`/`read_operation`/`update_operation`/`delete_operation` fields promote an action to a managed resource wired to the specified operations (MyCloud dashboards: create on `POST /dashboards/db`, read/delete on `/dashboards/uid/{uid}`); `ManagedResourceSchema` appends request-body inputs the response does not echo as Optional attributes. |
| Operational/framework notes closed as-is (G10, G21) | G10 — a merged reference spec can declare Enterprise/Cloud-only RBAC endpoints the target server does not serve (validate the spec against the server edition before generating); G21 — the recursive mute-timing `time_intervals` shape cannot be represented in terraform-plugin-framework (dynamic-in-collection), an accepted framework limitation. |

### 23.2 Accepted limitations (by design)

These are genuine, accepted limitations. They are documented here and in
`docs/usage.md` §Current limitations; none require work unless the stated
upstream constraint changes or a product decision is made.

- **Discriminated request/response payload switching is not implemented.** The
  OpenAPI `discriminator` is only a validator (an allowed-keys check via
  `DiscriminatorValidator`); create/update bodies use generic
  `applyJSONToModel` / `modelToJSONMap` JSON conversion that does not switch on
  the discriminator property when encoding/decoding a variant. A discriminated
  union round-trip is generic JSON, not variant-aware.
- **`EnumValues` is not rendered as a `stringvalidator`.** The allowed-keys
  check is covered by `DiscriminatorValidator` instead.
- **Nested `oneOf`/`anyOf`** (inside properties, collection elements) render as
  Dynamic attributes with a fail-loud `warnCompositionNotModeled` warning. The
  flat Terraform attribute model cannot represent alternatives, so the fallback
  is lossy but honest. Top-level `oneOf`/`anyOf` do reach the IR as unions.
- **OR security semantics are resolved as AND.** When an operation declares
  more than one security requirement (any one suffices), eidos applies AND of
  all declared schemes and emits a warning; a non-interactive Terraform provider
  cannot reliably try/fallback across OR alternatives. The warn-and-AND choice
  is stricter than OR and fail-loud.
- **OAuth2 `implicit` flow has no interceptor.** The flow is interactive
  (browser redirect) and deprecated in OAuth 2.1; `Configure` emits an
  `AddWarning` for every scheme that exposes config attributes but has no
  generated interceptor.
- **List-resource `uniqueItems: true` falls back to List.** The experimental
  `list/schema` package has no Set types, so a list endpoint whose response
  array declares `uniqueItems: true` is downgraded to List with a fail-loud
  `diagnostics.Warning` at transform time. Managed resources, data sources,
  ephemerals, and actions honor Set.
- **Terraform State Stores (1.15+) are not generated.** State Stores are
  experimental and offered without compatibility promises; eidos tracks the
  feature but waits for GA.
- **Mock-server and live-test breadth is intentionally scoped.** The generated
  mock server is a deterministic, single-resource lifecycle prover (hardcoded
  `example-id`, first-segment routes); the live acceptance tests
  (`testfixtures/live`, `TF_ACC=1`) run against a local deterministic mock
  server and never run in CI by design.
- **nested `metadata` wrapper flattening is not implemented.** The reference
  mycloud spec exposes the path parameters (`name`, `workspace`) as
  top-level properties instead. A future enhancement could extend
  `ManagedResourceSchema` to flatten a single level of nested `metadata` into
  top-level path-param attributes (and would exercise the list body's nested-
  `metadata` identity probing).