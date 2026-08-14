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
     - 7.1.3 [MCP Server](#713-mcp-server)
     - 7.1.4 [Config File Generation from Detected Spec](#714-config-file-generation-from-detected-spec)
   - 7.2 [Parser (`pkg/parser/`)](#72-parser)
   - 7.3 [Normalizer (`pkg/transformer/` — `normalizer_*.go`)](#73-normalizer)
   - 7.4 [Transformer (`pkg/transformer/`)](#74-transformer)
   - 7.5 [Code Generator (`pkg/generator/`)](#75-code-generator)
   - 7.6 [Protocol Layer Generator (`pkg/generator/`)](#76-protocol-layer-generator)
   - 7.7 [Documentation Generator (`pkg/generator/`)](#77-documentation-generator)
   - 7.8 [HTTP Client Generator (`pkg/generator/`)](#78-http-client-generator)
   - 7.9 [Test Generator (`pkg/generator/`)](#79-test-generator)
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
   - 9.5 [Dedicated OpenAPI Parser](#95-dedicated-openapi-parser)
10. [OpenAPI-to-Terraform Type Mapping](#10-openapi-to-terraform-type-mapping)
11. [Security Scheme Handling](#11-security-scheme-handling)
12. [Polymorphism & Complex Schemas](#12-polymorphism--complex-schemas)
   - 12.1 [allOf (Composition)](#121-allof-composition)
   - 12.2 [oneOf (Exclusive Union)](#122-oneof-exclusive-union)
   - 12.3 [anyOf (Inclusive Union)](#123-anyof-inclusive-union)
   - 12.4 [Circular References](#124-circular-references)
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
| OpenAPI 2.0 / 3.0.x / 3.1 parsing | Implemented | All three versions are parsed. Scalar type-mismatch diagnostics are emitted for OpenAPI 3.0.x/3.1.x and for Swagger 2.0 scalar fields at every depth (response `$ref`/description, `collectionFormat`, `externalDocs` description/URL, `additionalProperties` boolean, and the schema string/bool keywords). Any-value fields (`default`/`example`/`const`/`exclusiveMaximum`/`exclusiveMinimum`) are preserved via `nodeToNative` without warning, avoiding false positives on legitimate array/object values. Unquoted `openapi`/`swagger` version values are preserved as strings by the lexer. |
| `$ref` resolution (local) | Implemented | Only local (same-document) JSON Pointer `$ref`s resolve. File and remote URL refs are rejected with a fail-loud error diagnostic rather than fetched (the remote-fetch `RefResolver` was removed; fetching a remote *spec* is handled separately by `cmd/eidos/remote_spec.go`, which never resolves `$ref`s inside it). |
| IR normalization and transformation | Mostly implemented | Type mapping, CRUD inference, overrides, security mapping, and validator inference are functional. |
| `eidos generate --dry-run` | Implemented | Produces a file list and summary from the real parsed `ProviderIR`. |
| `eidos generate` (write mode) | Implemented | Writes generated files to `--output`, with overwrite guards controlled by `--force`. |
| `eidos generate-config` | Implemented | Emits a starter `generator.yaml` from a spec. |
| `eidos api` / `eidos mcp` | Implemented | Validation API and MCP server are functional; the MCP server advertises five tools (`eidos/generate-config`, `eidos/inspect`, `eidos/generate`, `eidos/validate-schemas`, `eidos/override-preview`); the API server uses `MaxHeaderBytes`, access logging, and panic recovery. |
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

Features marked **Partial**, **Mostly wired**, or **Partially scaffolded** are explicitly accepted as current limitations and are summarized in `docs/usage.md` §Current limitations.

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
9. **Full Terraform surface**: Eidos targets every Terraform Plugin Framework construct it generates — Resources, Data Sources, Actions, Ephemeral Resources, List Resources, and Provider-Defined Functions — not just CRUD resources. Experimental constructs it does not generate (e.g. Terraform State Stores, §23.2) are tracked as limitations rather than targets.

---

## 4. High-Level Architecture

```text
                             ┌────────────────────────┐
                             │  CLI (cmd/eidos/)      │
                             │  flags, config,        │
                             │  orchestration         │
                             └───────────┬────────────┘
                                         │
                             ┌───────────▼────────────┐
                             │  Parser                │
                             │  dedicated in-house    │
                             │  (2.0 / 3.0 / 3.1)     │
                             └───────────┬────────────┘
                                         │
                             ┌───────────▼────────────┐
                             │  Normalizer            │
                             │  $ref dereference,     │
                             │  allOf flatten,        │
                             │  polymorphism resolve  │
                             └───────────┬────────────┘
                                         │
                             ┌───────────▼────────────┐
                             │  Intermediate          │
                             │  Representation (IR)   │
                             │  ProviderIR,           │
                             │  ResourceIR,           │
                             │  DataSourceIR,         │
                             │  ActionIR,             │
                             │  EphemeralResourceIR,  │
                             │  ListResourceIR,       │
                             │  SchemaIR, etc.        │
                             └───────────┬────────────┘
                                         │
                             ┌───────────▼────────────┐
                             │  Transformer           │
                             │  OpenAPI → Terraform   │
                             │  type mapping, CRUD    │
                             │  inference, override   │
                             │  application           │
                             └───────────┬────────────┘
          ┌──────────────────────────────┼──────────────────────────────────┐
          │                              │                                  │
 ┌────────▼──────────┐ ┌─────────────────▼──────────────┐  ┌────────────────▼─────────────┐
 │  Code Generator   │ │   Documentation Generator      │  │   Test & Release Generator   │
 │  ├─ Provider      │ │   ├─ index.md                  │  │   ├─ acceptance_test.go      │
 │  ├─ Resources     │ │   ├─ resources/*.md            │  │   ├─ unit_test.go            │
 │  ├─ Data Sources  │ │   ├─ data-sources/*.md         │  │   ├─ .tftest.hcl             │
 │  ├─ Actions       │ │   ├─ actions/*.md              │  │   ├─ GNUmakefile             │
 │  ├─ Ephemeral     │ │   ├─ ephemeral-resources/*.md  │  │   ├─ .goreleaser.yml         │
 │  ├─ List Resources│ │   ├─ functions/*.md            │  │   └─ .github/workflows/      │
 │  ├─ Functions     │ │   └─ examples/                 │  │                              │
 │  ├─ Client        │ │                                │  │                              │
 │  ├─ Protocol      │ │                                │  │                              │
 │  └─ Models        │ │                                │  │                              │
 └───────────────────┘ └────────────────────────────────┘  └──────────────────────────────┘
```

---

## 5. OpenAPI Specification Coverage

### 5.1 Swagger / OpenAPI 2.0

OpenAPI 2.0 (Swagger) introduces the foundational concepts that Eidos must handle:

| Feature | Description | Terraform Mapping |
|---------|-------------|-------------------|
| `swagger` | Version identifier (`"2.0"`) | Version detection in parser |
| `host`, `basePath`, `schemes` | Server URL composition | Default API base URL, overridable via the `endpoint` provider attribute |
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
| `servers` / `serverVariables` | Replaces `host` + `basePath` + `schemes` | Default API base URL with variable substitution, overridable via the `endpoint` provider attribute |
| `components` | Replaces top-level `definitions`, `parameters`, `responses`, `securitySchemes` | Unified component resolution |
| `paths` + `operationId` | Uniquely identifies operations | Resource/data source name inference |
| `requestBody` + `content` + `schema` | Replaces `body` / `formData` parameters | Resource Create/Update argument schemas |
| `callbacks` | Out-of-band requests the API may make | Parsed but not mapped to a Terraform construct |
| `links` | Response-linked operations | Import hints, data source relations, or Action |
| `components/securitySchemes` | Replaces `securityDefinitions` | Provider auth config; adds `http` (Basic/Bearer), `oauth2` flows, `openIdConnect` |
| `nullable: true` | Explicit nullability | Computed + Optional attributes |
| `readOnly: true` / `writeOnly: true` | Field directionality | `readOnly` is parsed but not mapped (Computed-ness derives from request/response membership); `writeOnly` → `WriteOnly: true` + `Sensitive` (Terraform 1.10+) |
| `deprecated: true` | Deprecation markers | Attribute deprecation messages |
| `oneOf` / `anyOf` / `allOf` | Schema composition | See [Section 12](#12-polymorphism--complex-schemas) |
| `discriminator` | Polymorphic type switching | See [Section 12](#12-polymorphism--complex-schemas) |

### 5.3 OpenAPI 3.1.x

OpenAPI 3.1 aligns with JSON Schema Draft 2020-12 and introduces:

| Feature | Description | Terraform Mapping |
|---------|-------------|-------------------|
| `jsonSchemaDialect` | Default `$schema` for Schema Objects | Validated but not directly mapped |
| `type` arrays (e.g., `["string", "null"]`) | Union types at the schema level | `Optional` + `Computed` with nullable handling |
| `prefixItems` | Ordered tuple validation | Parsed but not mapped to a Terraform construct |
| `contentMediaType` / `contentEncoding` | Binary data description | Parsed but not mapped; no format validators are emitted |
| `$id` and `$ref` in Schema Objects | JSON Schema Draft 2020-12 reference resolution | `$ref` is dereferenced; `$id` is not used for base-URI resolution |
| `webhooks` | Incoming webhook descriptions | Parsed but not mapped to a Terraform construct |
| `pathItems` in components | Reusable path items | Parsed but not mapped to a Terraform construct |
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
| `string` | `StringAttribute` | `format` is preserved as metadata; no format validators are emitted |
| `number` | `Float64Attribute` | |
| `integer` | `Int64Attribute` | |
| `boolean` | `BoolAttribute` | |
| `enum` | `StringAttribute` / `Int64Attribute` | Enum values are carried in the IR but no enum validator is emitted |
| `oneOf` | Dynamic union, discriminated nested attribute, or split resource types | Configurable per schema; see Section 12 |
| `anyOf` | Dynamic union (same rendering as `oneOf`) | See Section 12 |
| `allOf` | Flattened merged object | All properties merged into one `SingleNestedAttribute` |
| `discriminator` | `SingleNestedAttribute` merging variant fields + `DiscriminatorValidator` (allowed-keys check) | See Section 12 |
| `$ref` | Dereferenced and inlined | Circular refs are marked `Opaque` on the ref holder and expanded up to `maxCyclicDepth` levels, then cut to an opaque boundary (first-entry properties preserved, deeper re-entry scalar-only) |
| `readOnly: true` | Parsed but not mapped | Computed-ness is derived from request/response membership, not `readOnly` |
| `writeOnly: true` | `WriteOnly: true` + `Sensitive: true` (Terraform 1.10+) | Renamed to `<name>_wo` with a companion `<name>_wo_version` Int64 attribute |
| `nullable` / `type: ["string", "null"]` | `Optional` + `Computed` | Null is a valid state |
| `default` | Carried in the IR; no `Default` schema field emitted | |
| `minLength`, `maxLength` | Carried in the IR; no validator emitted | |
| `minimum`, `maximum` | Carried in the IR; no validator emitted | Only the exclusive forms emit validators (below) |
| `pattern` | Carried in the IR; no validator emitted | |
| `minItems`, `maxItems` | Carried in the IR; no validator emitted | |
| `uniqueItems` | Use `SetAttribute` / `SetNestedAttribute` | |
| `format: "date-time"` | `StringAttribute` | No validator emitted |
| `format: "email"` | `StringAttribute` | No validator emitted |
| `format: "uuid"` | `StringAttribute` | No validator emitted |
| `format: "uri"` | `StringAttribute` | No validator emitted |
| `format: "password"` | `StringAttribute` | `Sensitive: true` is set for ephemeral-resource password-format properties and for write-only attributes, not for regular resource attributes |
| `format: "byte"` | `StringAttribute` | No base64 encoding is applied |
| `format: "binary"` | `StringAttribute` | Terraform has no native binary type; use base64 |
| `format: "int32"` | `Int64Attribute` | Terraform only has Int64 |
| `format: "int64"` | `Int64Attribute` | |
| `format: "float"` | `Float64Attribute` | |
| `format: "double"` | `Float64Attribute` | |
| `deprecated: true` | `DeprecationMessage: "..."` schema field | Not a plan modifier |
| `example` / `x-examples` | Documentation defaults, acceptance test fixtures | |
| `description` | `MarkdownDescription: "..."` | CommonMark support in Framework |
| `externalDocs` | Documentation links in Markdown | |
| `tags` | Resource categorization, doc sections | |
| `not` | Carried in the IR; no validator emitted | |
| `const` | Carried in the IR; no validator emitted | |
| `if`/`then`/`else` | Carried in the IR; no validator emitted | |
| `dependentRequired` | Custom `ConditionalValidator` | One per trigger attribute |
| `dependentSchemas` | Carried in the IR; no validator emitted | |
| `patternProperties` | Custom `PatternPropertiesValidator` | |
| `minProperties`/`maxProperties` | Carried in the IR; no validator emitted | |
| `exclusiveMinimum`/`exclusiveMaximum` | Custom `Int64ExclusiveMinimumValidator` / `Int64ExclusiveMaximumValidator` / `Float64ExclusiveMinimumValidator` / `Float64ExclusiveMaximumValidator` | 3.0 boolean form normalized to 3.1 number form |
| `multipleOf` | Custom `Int64MultipleOfValidator` / `Float64MultipleOfValidator` | No built-in validator; generated custom validator |
| `unevaluatedProperties` | Carried in the IR; no emission | |
| `propertyNames` | Carried in the IR; no validator emitted | |
| `securitySchemes` (apiKey) | Provider attribute `api_key` (Sensitive) | Header, query, or cookie placement |
| `securitySchemes` (http Basic/Bearer) | Provider attributes `username`/`password` or `bearer_token` | |
| `securitySchemes` (oauth2) | Provider attributes for each flow | `client_id`, `client_secret`, `token_url`, etc. |
| `securitySchemes` (openIdConnect) | Provider attribute `oidc_token_url` | |
| `servers` / `serverVariables` | Default API base URL, overridable via the `endpoint` provider attribute | Variable substitution in URL templates |
| `callbacks` | Parsed; the declaring operation is classified by the standard heuristics | The callback object itself maps to no construct |
| `links` | Parsed; the linked operations are classified by the standard heuristics | The link object itself maps to no construct |
| `webhooks` (3.1) | Parsed but not mapped to a Terraform construct | Webhook operations are not processed by the transformer |
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

type UnionKind string

const (
    OneOf UnionKind = "oneOf"
    AnyOf UnionKind = "anyOf"
)

type UnionType struct {
    Kind          UnionKind
    Variants      []SchemaIR
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
    ForceNew         bool           // x-terraform-force-new / forceNew marker
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
    SourceLocation   *SourceLocation // Source position in the original spec
}

type SourceLocation struct {
    File string
    Line int
    Col  int
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
    GenerateTerraformTests bool   // Emit native Terraform .tftest.hcl files
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
    SchemaVersion     int              // State schema version (for state upgrades)
    StateUpgrades     []StateUpgradeIR // Migrations from prior schema versions
}

type StateUpgradeIR struct {
    FromVersion       int
    Renames           map[string]string // old attribute name → current name
    BlockRenames      map[string]string // old block name → current block name
    AddedAttributes   []string
    AddedBlocks       []string
    RemovedAttributes []string
    RemovedBlocks     []string
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
    CookieParams    []ParamIR
    FormDataParams  []ParamIR
    MediaType       string   // Request body media type (e.g. "application/json")
    BodySchema      *SchemaIR
    ResponseSchema  *SchemaIR
    SuccessCodes    []int
    ErrorMappings   map[int]ErrorMappingIR
    SecurityRequirements []map[string][]string // Per-operation security (OR across alternatives)
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
    SourceOperation string
    IsList        bool             // Read response is a top-level JSON array
    Pagination    *PaginationIR    // Pagination strategy for a list data source
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
    MarkdownDescription string
    ConfigSchema      ObjectSchemaIR  // Input parameters for the action
    InvokeMapping     OperationMappingIR // The HTTP call to make when invoked
    ModifyPlan        bool             // Whether to generate a ModifyPlan method (for API-accessible validation)
    ModifyPlanMapping *OperationMappingIR // Explicit preflight endpoint (generator.yaml only)
    ValidateConfigMapping *OperationMappingIR // Explicit server-side validation endpoint (generator.yaml only)
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
    DependentRequired map[string][]string // JSON Schema conditional required fields
}

type AttributeIR struct {
    Name             string
    WireName         string   // Original OpenAPI property/parameter name when it differs from Name
    Schema           SchemaIR
    Description      string
    MarkdownDescription string
    Required         bool
    Optional         bool
    Computed         bool
    Sensitive        bool
    WriteOnly        bool           // writeOnly: true → not stored in state (Terraform 1.10+)
    ForceNew         bool           // x-terraform-force-new / forceNew marker
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
    MinItems     *int64
    MaxItems     *int64
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
    MarkdownDescription string
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
    Logging         *LoggingIR
}

type LoggingIR struct {
    LogFile                string
    CaptureRequestHeaders  bool
    CaptureRequestBody     bool
    CaptureResponseHeaders bool
    CaptureResponseBody    bool
    MaxBodyBytes           int
    RedactHeaders          []string
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

```bash
eidos generate \
  --spec ./api.yaml \
  --output ./terraform-provider-mycloud \
  --config ./generator.yaml \
  --dry-run
```

| Flag | Description | Default |
|------|-------------|---------|
| `--spec` | Path to OpenAPI spec file (JSON or YAML), or an http(s) URL to fetch | Required |
| `--output` | Output directory for generated provider | Required for full generation |
| `--config` | Path to `generator.yaml` overrides file | None |
| `--dry-run` | Run the full pipeline without writing files; print a summary | `false` |
| `--dry-run-output` | Path to write the dry-run summary (JSON or text) | stdout |
| `--generate-config` | Write a starter `generator.yaml` into the output directory | `false` |
| `--force` | Overwrite an existing `generator.yaml` (with `--generate-config`) or existing generated provider files (write mode) | `false` |
| `--provider-name` | Provider name for the starter config when used with `--generate-config` | Spec title |
| `--generate-terraform-tests` | Generate native Terraform `.tftest.hcl` files | `false` |
| `--spec-allow-http` | Permit `http://` spec URLs (https is the default for remote specs) | `false` |
| `--spec-auth-scheme` | Authenticate a remote spec fetch: `bearer`, `basic`, `apiKey`, or `oauth2-client-credentials` | None |
| `--spec-token-env` | Env var holding the bearer token for `--spec-auth-scheme bearer` | None |
| `--spec-username-env` | Env var holding the username for `--spec-auth-scheme basic` | None |
| `--spec-password-env` | Env var holding the password for `--spec-auth-scheme basic` | None |
| `--spec-key-env` | Env var holding the API key for `--spec-auth-scheme apiKey` | None |
| `--spec-header-name` | Header name the apiKey scheme sends the key in | None |
| `--spec-token-url` | OAuth2 token endpoint for `--spec-auth-scheme oauth2-client-credentials` | None |
| `--spec-client-id-env` | Env var holding the OAuth2 client ID | None |
| `--spec-client-secret-env` | Env var holding the OAuth2 client secret | None |

The CLI orchestrates the pipeline:
1. **Detect** → `parser.DetectVersion(root)` → OpenAPI version (2.0 / 3.0.x / 3.1.x)
2. **Parse** → `convertForVersion(root, version)` → raw OpenAPI document model
3. **Transform** → `buildIRPreview(sp, version, cfg)` → `ProviderIR`
4. **Generate** → `generator.Run(provider, opts)` → file tree (record mode for `--dry-run`, write mode otherwise)

### 7.1.1 Dry-Run Mode

The `--dry-run` flag runs the full generation pipeline without writing any files to disk. It is useful for reviewing what Eidos would generate before committing to an output directory.

When `--dry-run` is set, the CLI:

1. Parses the OpenAPI spec.
2. Runs normalization and transformation to produce the `ProviderIR`.
3. Executes the generator logic in **record mode** so it can report the list of files it would create and the reasons for each.
4. Prints a structured summary to stdout (and optionally to a file via `--dry-run-output`).
5. Exits with code `0` on success or a non-zero code if the spec cannot be processed.

**Summary output** (from the reference `test/specs/mycloud.yaml`; the file list is truncated here for brevity — the real output prints every path):

```text
Eidos dry-run summary for provider "mycloud"
Spec: test/specs/mycloud.yaml (OpenAPI 3.0)

Generated constructs:
  Resources:            11
  Data sources:         17
  Actions:              0
  Ephemeral resources:  0
  List resources:       12
  Functions:            0
  Security schemes:     1
  Write-only attributes:0

Files that would be written (180):
  .github/workflows/release.yml
  .goreleaser.yml
  GNUmakefile
  README.md
  docs/data-sources/get_branch.md
  docs/index.md
  docs/list-resources/list_branches.md
  docs/resources/config.md
  examples/resources/config/resource.tf
  internal/client/auth.go
  internal/client/client.go
  internal/client/errors.go
  internal/client/logging.go
  internal/client/models.go
  internal/client/pagination.go
  internal/client/retry.go
  internal/protocol/value_mappers.go
  internal/protocol/value_mappers_test.go
  internal/provider/data_source_get_branch.go
  internal/provider/json_convert.go
  internal/provider/model_config.go
  internal/provider/provider.go
  internal/provider/provider_test.go
  internal/provider/resource_config.go
  internal/provider/resource_config_acceptance_test.go
  internal/provider/resource_config_test.go
  internal/provider/validators.go
  main.go
  terraform-registry-manifest.json
  …

Diagnostics:
  none
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
    { "path": "internal/provider/provider.go", "reason": "provider schema and registration" },
    { "path": "internal/provider/resource_config.go", "reason": "resource config" }
  ],
  "written": false,
  "diagnostics": []
}
```

`config_path` is present only when a `generator.yaml` overrides file was supplied; `written` is `false` for a dry-run and `true` after a full generation run.

Dry-run mode shares the same code path as normal generation; only the final file-writer is replaced by a recorder, so the summary is always an accurate preview.

### 7.1.2 Feature Validation Endpoint

Eidos includes a small built-in HTTP API (`cmd/eidos/api.go`) that exposes a single endpoint designed to exercise and verify every generation feature supported by the project. This endpoint is used by the test suite and can be used by spec authors to confirm that Eidos will handle their constructs correctly.

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
  "config": "provider:\n  name: featuretest\n  version: 0.1.0\n"
}
```

**Response** (abridged — `ir_preview` is the full `ProviderIR`):

```json
{
  "valid": true,
  "diagnostics": [],
  "detected": {
    "version": "3.0.3",
    "title": "Feature Test",
    "info_version": "1.0.0",
    "paths": 1,
    "schemas": 2,
    "operations": 1,
    "resources": 0,
    "data_sources": 1,
    "actions": 0,
    "ephemeral_resources": 0,
    "list_resources": 0,
    "functions": 0,
    "security_schemes": 0,
    "schemas_with_oneOf": 0,
    "schemas_with_allOf": 0,
    "schemas_with_anyOf": 0,
    "write_only_attributes": 0,
    "read_only_attributes": 0,
    "nullable_attributes": 0,
    "importable_resources": 0,
    "state_upgraders": 0,
    "generate_terraform_tests": false,
    "logging_enabled": false
  },
  "ir_preview": {
    "name": "featuretest",
    "type_name": "featuretest",
    "version": "0.1.0",
    "source_spec_version": "3.0",
    "data_sources": [
      {
        "name": "get_pet",
        "type_name": "featuretest_get_pet",
        "schema": {
          "attributes": [
            {
              "name": "value",
              "schema": {
                "union": {
                  "kind": "oneOf",
                  "variants": [
                    { "name": "Cat", "attributes": [ { "name": "name", "schema": { "type": "string" }, "computed": true } ] },
                    { "name": "Dog", "attributes": [ { "name": "breed", "schema": { "type": "string" }, "computed": true } ] }
                  ],
                  "discriminator": { "property_name": "petType", "mapping": null }
                },
                "computed": true
              },
              "computed": true
            }
          ]
        },
        "read_mapping": { "method": "GET", "path_template": "/pets/{id}", "success_codes": [200] }
      }
    ],
    "client": {},
    "security": {}
  },
  "suggested_config": "provider:\n    name: featuretest\n    version: 0.1.0\n"
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
| Pagination | Reports the pagination style from `generator.yaml` (`pagination.style`) |
| Import support | Reports importable resources and import formats |
| State upgraders | Reports version history if configured |
| Provider-defined functions | Lists detected/declared functions |
| Validation & plan modifiers | Lists inferred validators and plan modifiers per attribute |
| Logging options | Confirms provider-level logging schema attributes |
| `terraform test` files | Indicates whether `.tftest.hcl` generation would be enabled |
| Config file generation | Includes a suggested `generator.yaml` in the response |

The endpoint is implemented as a normal `net/http` handler in `cmd/eidos/api.go` and can be started with `eidos api --port 8080`. It uses the same parser, normalizer, and transform package as the CLI so results are representative of real generation.

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

```bash
eidos generate-config \
  --spec ./api.yaml \
  --output ./generator.yaml
```

**Behavior**:

1. Parse the spec using the dedicated parser.
2. Run the transformer in **discovery mode** (no file generation).
3. Emit a `generator.yaml` containing:
   - Detected `provider.name`, `provider.version`, and `provider.description`.
   - Detected `servers`.
   - Detected security schemes as `auth` entries with environment variable hints.
   - Inferred `resource_overrides`, `datasource_overrides`, `action_overrides`, `ephemeral_resource_overrides`, `list_resource_overrides`, and `function_overrides`.
   - Detected polymorphism strategies (`dynamic_union` or `split_resources`) and variant names.
   - Suggested `global_timeouts`, `pagination`, and `logging` blocks.
   - A `spec` block recording the source spec path and detected format (`openapi2`, `openapi3`, or `openapi31`).

The CLI does not emit explanatory comments; the MCP `eidos/generate-config` tool accepts an `include_comments` parameter for that.

**Example generated config** (trimmed from the reference `test/specs/mycloud.yaml`; the `provider.name`/`display_name` and `auth.env_var` values assume `--provider-name mycloud` — the default for `eidos generate-config` is `generated`, while `eidos generate --generate-config` defaults to the spec title):

```yaml
provider:
    name: mycloud
    display_name: terraform-provider-mycloud
    version: 0.1.0
    description: Trimmed MyCloud API reference spec for golden file regression tests.
    protocol_version: 6
servers:
    - url: https://api.mycloud.example/v1
resource_overrides:
    - schema: config
      operation: createConfig
      resource_name: config
      id_attribute: id
      import_format: '{workspace}:{name}'
      computed_attributes:
        - api_version
        - data
        - id
        - kind
    - schema: workspace
      operation: createWorkspace
      resource_name: workspace
      id_attribute: name
      import_format: '{name}'
      computed_attributes:
        - api_version
        - kind
        - labels
        - status
        - phase
datasource_overrides:
    - operation: get_branch
      datasource_name: get_branch
    - operation: list_branches
      datasource_name: list_branches
list_resource_overrides:
    - resource: list_branches
      operation: listBranches
      config_schema:
        - name: organization
          type: string
        - name: project
          type: string
logging:
    max_body_bytes: 4096
    redact_headers:
        - Authorization
        - X-API-Key
        - Cookie
auth:
    - scheme: bearer
      env_var: MYCLOUD_BEARERAUTH
global_timeouts:
    create: 20m
    read: 10m
    update: 20m
    delete: 10m
generate_terraform_tests: false
spec:
    path: /path/to/api.yaml
    format: openapi3
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

All three build the config from the IR via `generator.GenerateConfig` in `pkg/generator/config_generator.go` (the CLI and MCP tool both call it through `pkg/api`), so behavior never diverges.

### 7.2 Parser

**Package**: `pkg/parser/`
**Type**: Dedicated in-house parser (no external OpenAPI library dependency)

**Responsibilities**:
- Detect spec version (`swagger: "2.0"`, `openapi: "3.0.x"`, `openapi: "3.1.x"`).
- Parse JSON or YAML input into a thin, version-specific AST, then convert to a generic internal model.
- Validate structural correctness (missing required fields, invalid `$ref` targets) with source-location diagnostics.
- Resolve local (same-document) JSON Pointer `$ref` values with cycle detection; external file and remote URL refs are rejected with a fail-loud error diagnostic rather than fetched.
- Return a version-agnostic intermediate model consumed by the transformer.

**Version-specific handling**:

| Version | Key Differences | Parser Handling |
|---------|----------------|-----------------|
| 2.0 | `host` + `basePath` + `schemes` instead of `servers` | Convert to a server URL template (`ServerIR`) |
| 2.0 | `definitions` instead of `components/schemas` | Map to `components.schemas` internally |
| 2.0 | `parameters` in body/formData instead of `requestBody` | Convert to `FormDataParams` on `OperationMappingIR` |
| 2.0 | `securityDefinitions` instead of `components/securitySchemes` | Map to `SecuritySchemeIR` |
| 2.0 | `produces`/`consumes` at top-level and operation-level | Map to `OperationMappingIR` content negotiation |
| 3.0 | `servers` with variables | Direct mapping to `ServerIR` |
| 3.0 | `components/*` | Direct mapping |
| 3.0 | `requestBody` with `content` | Direct mapping |
| 3.0 | `nullable: true` | Carried in the IR; Optional/Computed derives from request/response membership |
| 3.1 | `type` arrays (`["string", "null"]`) | Map to nullable schema |
| 3.1 | `webhooks` | Parsed but not mapped to a Terraform construct |
| 3.1 | `prefixItems` | Parsed but not mapped to a Terraform construct |
| 3.1 | `contentMediaType`/`contentEncoding` | Parsed but not mapped; no format validators are emitted |

**No external OpenAPI parser dependency**: Eidos deliberately avoids `libopenapi`, `kin-openapi`, or any other third-party OpenAPI parser. All parsing, validation, and resolution logic lives in `pkg/parser/` and its version-specific files.

### 7.3 Normalizer

**Package**: `pkg/transformer/` (normalization phase; the `normalizer_*.go` files)

**Responsibilities**:
1. **`$ref` resolution**: Recursively dereference all JSON Pointer `$ref` entries. Circular references are detected by the parser and produce a warning; the parser marks the cyclic ref holders `Opaque`, and the transformer expands a cyclic ref up to `maxCyclicDepth` levels (preserving first-entry properties) before cutting deeper re-entry to an opaque boundary (see §12.4).
2. **`allOf` flattening**: Merge all `allOf` schemas into a single flat object, resolving property conflicts (duplicate required fields with same type → merge; conflicting types → error).
3. **Polymorphism normalization**: Convert `oneOf` + `discriminator` combinations into `UnionType` with `DiscriminatorIR`; `anyOf` becomes `UnionType` without a discriminator.
4. **Parameter resolution**: Merge path-level parameters into operation-level parameters; resolve `parameters` references.
5. **Security resolution**: Merge global `security` with operation-level overrides.
6. **Server composition**: Compose `servers` hierarchy (global → path-item → operation) into final `ServerIR` list per operation.
7. **Naming normalization**: Derive `operationId` from method + path if not present; sanitize names to be Go/Terraform-compatible (lowercase_snake_case for HCL, PascalCase for Go types).

### 7.4 Transformer

**Package**: `pkg/transformer/`

**Responsibilities**:
1. **Type mapping**: Convert OpenAPI types to Terraform Plugin Framework types (see [Section 10](#10-openapi-to-terraform-type-mapping)).
2. **CRUD inference**: Analyze path patterns and HTTP methods to infer Create/Read/Update/Delete operations:
   - `POST /pets` → Create
   - `GET /pets/{petId}` → Read
   - `GET /pets` → List (data source) or List Resource (tfquery)
   - `PUT /pets/{petId}` → Full Update
   - `PATCH /pets/{petId}` → Partial Update
   - `DELETE /pets/{petId}` → Delete
3. **Action inference**: Detect non-CRUD operations (unified with `transformer.InferActions`):
   - `POST /servers/{id}/<action>` patterns → Action
   - POST whose `operationId` leading verb is a recognized action verb → Action
   - POST that is not a CRUD Create path → Action
   - Operations with `x-terraform-action: true` → Action
4. **Ephemeral resource inference**: Detect temporary/credential operations:
   - POST on a path containing ephemeral keywords (`credentials`, `token`, `session`, `lease`, `ticket`) → Ephemeral resource
   - Operations with `x-terraform-ephemeral: true` → Ephemeral resource
   - The transformer's `InferEphemeralResources` additionally treats `writeOnly` response properties and `password`-format responses as ephemeral cues
5. **List resource inference**: Detect collection endpoints:
   - A collection `GET` paired with an instance Read is promoted additively to a List resource (the data source is kept)
   - Operations with `x-terraform-list: true` → List resource
6. **ID attribute detection**: Identify the primary identifier attribute from path parameters or response schema (e.g., `id`, `petId`).
7. **Computed/Required/Optional/WriteOnly inference**: Apply rules:
   - Response-only attributes → `Computed: true`
   - Attributes present in both request and response → `Optional: true` + `Computed: true`
   - `required: true` request attributes → `Required: true`
   - `writeOnly: true` → `WriteOnly: true` + `Sensitive: true` (renamed to `<name>_wo` with a companion `<name>_wo_version` attribute)
   - Default values are carried in the IR; no `Default` schema field is emitted
8. **Security mapping**: Convert security schemes to provider config attributes.
9. **Override application**: Apply `generator.yaml` overrides (see [Section 14](#14-configuration--overrides-system)).
10. **Naming conflicts**: Resolve collisions (e.g., two operations producing the same resource name) by appending HTTP method or path segments.

### 7.5 Code Generator

**Package**: `pkg/generator/`
**Template engine**: `text/template` with helper functions. Programmatic Go source files are generated with the in-tree `pkg/generator/astgen` standard-library package (`go/ast` + `go/token` + `go/format`).

**Responsibilities** — emit the following Go source files:

| File | Purpose |
|------|---------|
| `main.go` | Provider server entrypoint serving via `providerserver.Serve` (Protocol v6) with a `tf5server.Serve` fallback for `--protocol-version 5` |
| `internal/provider/provider.go` | Provider struct, `Metadata`, `Schema`, `Configure`, `Resources`, `DataSources`, `Actions`, `EphemeralResources`, `ListResources`, `Functions` |
| `internal/provider/provider_test.go` | Provider-level unit tests |
| `internal/provider/resource_<name>.go` | Resource implementation: `Create`, `Read`, `Update`, `Delete`, `ImportState`, `Schema` |
| `internal/provider/resource_<name>_test.go` | Resource unit tests |
| `internal/provider/resource_<name>_acceptance_test.go` | Resource acceptance tests |
| `internal/provider/data_source_<name>.go` | Data source implementation: `Read`, `Schema` |
| `internal/provider/data_source_<name>_test.go` | Data source unit tests |
| `internal/provider/action_<name>.go` | Action implementation: `Invoke`, `Schema`, `ModifyPlan`, `ValidateConfig` |
| `internal/provider/ephemeral_<name>.go` | Ephemeral resource: `Open`, `Renew`, `Close`, `Schema` |
| `internal/provider/list_<name>.go` | List resource: `List`, `ListResourceConfigSchema` |
| `internal/provider/function_<name>.go` | Provider-defined function (optional) |
| `internal/provider/model_<name>.go` | Go struct types matching Terraform schemas (for JSON marshalling) |
| `internal/provider/validators.go` | Custom validators (discriminator, conditional, exclusive bounds, multipleOf, etc.) |
| `internal/provider/json_convert.go` | JSON/model conversion helpers for wired CRUD bodies (emitted when any resource, data source, or ephemeral resource is wired) |
| `internal/client/client.go` | Generated HTTP client |
| `internal/client/auth.go` | Authentication middleware (emitted when the spec declares security schemes) |
| `internal/client/models.go` | Request/response structs |
| `internal/client/errors.go` | Typed API error helpers |
| `internal/client/retry.go` | Retry logic with exponential backoff |
| `internal/client/pagination.go` | Pagination helpers |
| `internal/client/logging.go` | Request/response trace logging |
| `internal/protocol/value_mappers.go` | `tftypes.Value` ↔ Go struct converters |
| `internal/protocol/value_mappers_test.go` | Value mapper round-trip tests |

### 7.6 Protocol Layer Generator

**Package**: `pkg/generator/`

The generated provider uses `terraform-plugin-framework` which abstracts the gRPC protocol. However, Eidos also generates explicit value mapper code that bridges between `tftypes.Value` (the protocol representation) and generated Go models, ensuring:

1. **Schema descriptors** are correctly defined via `schema.Schema` for every resource, data source, and the provider.
2. **State conversion** between `tftypes.Value` and Go structs is handled by generated `XxxModelFromValue()` / `XxxModelToValue()` value mappers in `internal/protocol/value_mappers.go`; wired CRUD bodies additionally use `modelToJSONMap()` / `applyJSONToModel()` helpers to build request bodies and map responses back into the model.
3. **Diagnostics accumulation** is handled by generated error-to-diagnostic converters that map HTTP error responses to `diag.Diagnostics`.
4. **State upgraders** are generated when schema versioning is detected (via `generator.yaml`).
5. **Import state** handlers parse composite IDs from `ImportStateRequest.ID`.

The provider binary is served via `providerserver.Serve` (Protocol v6) or `tf5server.Serve` (Protocol v5), both from `terraform-plugin-go`.

### 7.7 Documentation Generator

**Package**: `pkg/generator/`

Generates Markdown files compatible with `terraform-plugin-docs` and the Terraform Registry:

| File | Content |
|------|---------|
| `docs/index.md` | Provider overview, authentication guide, example HCL |
| `docs/resources/<name>.md` | Argument reference, attribute reference, import syntax, timeout info, example HCL |
| `docs/data-sources/<name>.md` | Argument reference, attribute reference, example HCL |
| `docs/actions/<name>.md` | Action argument reference, example HCL invocation |
| `docs/ephemeral-resources/<name>.md` | Configuration reference, result attributes, ephemeral context restrictions, example HCL |
| `docs/list-resources/<name>.md` | List resource configuration, identity, and result attributes, example HCL |
| `docs/functions/<name>.md` | Arguments, return type, example HCL |
| `examples/resources/<name>/resource.tf` | Minimal HCL example for `terraform apply` |
| `examples/data-sources/<name>/data-source.tf` | Minimal HCL example for data source |
| `examples/actions/<name>/action.tf` | Minimal HCL example for action invocation |
| `examples/ephemeral-resources/<name>/ephemeral-resource.tf` | Minimal HCL example for ephemeral resource |

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
page_title: "mycloud_pet Resource - mycloud"
subcategory: ""
description: |-
  Manages a Pet resource.
---
```

### 7.8 HTTP Client Generator

**Package**: `pkg/generator/`

Generates a Go HTTP client using `net/http`:

**Features**:
- **Request construction**: Path parameter substitution via `strings.ReplaceAll`, query parameter encoding via `url.Values`, header injection.
- **Response parsing**: JSON unmarshalling into generated model structs; error responses are captured by `NewAPIError` into an `APIError` (status code, headers, and body capped at 1 MiB) rather than decoded into typed error structs — the `ErrorResponse` model is emitted but not wired.
- **Authentication middleware**: Pluggable auth handlers for each `SecuritySchemeIR` type:
  - `apiKey` → inject header/query/cookie
  - `http/basic` → `Authorization: Basic <encoded>`
  - `http/bearer` → `Authorization: Bearer <token>`
  - `oauth2/client_credentials` → token exchange with `client_id`, `client_secret`, `token_url`
  - `oauth2/authorization_code` → `OAuth2AuthorizationCodeRefresh` exercises the non-interactive refresh path against the token endpoint; the initial authorization-code exchange requires an interactive browser redirect and must happen out-of-band
  - `openIdConnect` → `OpenIDConnect` performs OIDC discovery (token endpoint resolved from the discovery document, cached on first use) and acquires tokens via the client-credentials flow
- **Retry logic**: Exponential backoff with additive jitter for `5xx` and `429` responses (and network errors); configurable max retries.
- **Pagination**: `DetectPaginationStyle` in the transformer examines the `x-pagination` extension, response `Link` headers, and query parameter names (offset/page vs cursor); the client emits `ListAllPages()` plus `ExtractLinkHeader` for RFC 5988 `Link` header following.
- **Timeouts**: The client uses a fixed 30-second default HTTP client timeout; there is no per-request timeout derived from Terraform resource timeouts.
- **User-Agent**: The client sets `User-Agent: eidos-generated-client` by default and exposes a `WithUserAgent` option to override it.

### 7.9 Test Generator

**Package**: `pkg/generator/`

Generates:

1. **Unit tests** (`internal/provider/resource_<name>_test.go`): Schema validation via `ValidateImplementation` and metadata/type-name checks.
2. **Acceptance tests** (`internal/provider/resource_<name>_acceptance_test.go`): Full `terraform-plugin-testing` acceptance tests using `httptest.NewServer()` as a mock API server. Tests cover:
   - Create + Read
   - Update + Read
   - Delete (resource disappears)
   - Import (by ID)
   - Import (by composite ID, if applicable)
3. **Mock HTTP server**: Generated `httptest` handlers that echo request bodies back (so create/update responses reflect the values sent by the test) and enforce the expected auth header (e.g. `Authorization: Bearer example`).

### 7.10 Support Packages

The following support packages are shared by multiple stages of the generation pipeline and do not map one-to-one to the CLI or generator subpackages above:

**Package**: `pkg/ir/`

Defines the intermediate representation (`ProviderIR`, `ResourceIR`, `DataSourceIR`, etc.) consumed by the transformer and generator packages. See [Section 6](#6-intermediate-representation-ir) for the IR type system.

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
| `Validators` | `schema.Attribute{Validators: []validator.Float64{...}}` — custom validators only (see §8.3) |
| `PlanModifiers` | Not emitted (see §8.3) |
| `Default` | Not emitted (see §8.3) |

### 8.2 CRUD Mapping

| Terraform Operation | OpenAPI Pattern | HTTP Method |
|--------------------|----------------|-------------|
| `Create` | `POST /collection` | POST |
| `Read` | `GET /collection/{id}` | GET |
| `Update` (full) | `PUT /collection/{id}` | PUT |
| `Update` (partial) | `PATCH /collection/{id}` | PATCH |
| `Delete` | `DELETE /collection/{id}` | DELETE |
| `Import` | Same as Read | GET |

**Create flow**:
1. Terraform calls `Create(ctx, req, resp)`.
2. Provider decodes `req.Plan` into the model via `req.Plan.Get(ctx, &plan)`.
3. Provider builds the request body from the model via `modelToJSONMap(&plan)` and `json.Marshal`.
4. Client sends HTTP request to API.
5. On success (201/200), client parses the response body and/or `Location` header.
6. Provider maps the response JSON back into the model via `applyJSONToModel(&plan, data)`.
7. Provider stores the model in state via `resp.State.Set(ctx, &plan)`.
8. On error, provider returns `resp.Diagnostics` with error detail.

**Update flow**:
- If `PUT` is available → full replacement of all modifiable fields.
- If only `PATCH` is available → the generated `Update` sends the full model body via PATCH (no diff is computed).
- If both are available → `PUT` wins over `PATCH` (hardcoded in the transformer's `chooseUpdateOps`; not configurable via `generator.yaml`).

**Delete flow**:
1. Terraform calls `Delete(ctx, req, resp)`.
2. Provider extracts ID from `req.State`.
3. Client sends `DELETE /collection/{id}`.
4. On success (204/200), provider removes resource from state.
5. On 404, provider removes resource from state (already deleted).
6. On other errors, provider returns diagnostics.

### 8.3 Plan Modifiers & Validators

**Plan modifiers**: The generator does not currently emit typed plan-modifier
constructors (`planmodifier.RequiresReplace`, `stringplanmodifier.UseStateForUnknown`,
`stringdefault.StaticString`, etc.) into generated schemas. Force-new attributes
(`forceNew` / `x-terraform-force-new: true`) are tracked in the IR
(`PlanModifierIR` with `planmodifier.RequiresReplace`) and surfaced in the
generated `generator.yaml` as `force_new` entries, but the schema itself does not
attach a `RequiresReplace` modifier. Attribute default values are carried on the
IR but are not rendered as `Default` schema fields.

**Validators emitted by the generator**: The generator renders validators
directly from schema constraints — not from the IR's generic `ValidatorIR`
metadata, which is currently dead metadata on the IR. The emitted set is:

| OpenAPI Indicator | Generated Validator |
|-------------------|-------------------|
| `exclusiveMinimum` / `exclusiveMaximum` (int) | Custom `Int64ExclusiveMinimumValidator` / `Int64ExclusiveMaximumValidator` |
| `exclusiveMinimum` / `exclusiveMaximum` (float) | Custom `Float64ExclusiveMinimumValidator` / `Float64ExclusiveMaximumValidator` |
| `multipleOf` | Custom `Int64MultipleOfValidator` / `Float64MultipleOfValidator` |
| `discriminator` | Custom `DiscriminatorValidator` |
| `dependentRequired` | Custom `ConditionalValidator` per trigger attribute |
| `patternProperties` | Custom `PatternPropertiesValidator` |

These custom validators are emitted into `internal/provider/validators.go` and
attached to the matching attributes. Standard framework validators for `enum`,
`minLength`/`maxLength`, `minimum`/`maximum`, `pattern`, `format`, `not`,
`const`, `if`/`then`/`else`, `dependentSchemas`, `minProperties`/`maxProperties`,
and `propertyNames` are not currently emitted.

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

OpenAPI specs can define utility endpoints that don't map naturally to resources or data sources. Eidos can generate provider-defined functions for read-only compute/query endpoints:

- `GET /utils/lookup` → function `lookup_<name>()`
- `GET /utils/search` → function `search_<name>()`

**Automatic detection**: GET operations whose path contains a function keyword (`search`, `compute`, `calculate`, `convert`, `lookup`, `query`) or that are annotated with `x-terraform-function: true` are inferred as functions. The function signature is derived from the operation's path/query/header/cookie parameters (arguments) and the resolved response schema (return type).

**Explicit configuration** (`generator.yaml`): `function_overrides` entries create or rename functions:

```yaml
function_overrides:
  - operation: "ipLookup"
    name: "lookup_ip"
    arguments:
      - name: "ip"
        type: "string"
```

### 8.7 Actions (Invoke Actions)

**Actions** are a first-class Terraform Plugin Framework abstraction introduced in Terraform 1.13. They represent side-effects that interact with remote systems but do not manage CRUD lifecycle state. Actions are invoked directly via `terraform apply` or the CLI, and they do not produce output data that can be referenced by other parts of a Terraform configuration.

Actions are a **critical** mapping target for OpenAPI because many API operations are inherently action-oriented rather than resource-oriented — e.g., "reboot server", "rotate credentials", "send notification", "run backup".

#### How Actions Map from OpenAPI

Not every HTTP operation maps to a resource CRUD method. Eidos uses the following heuristics and explicit configuration to identify actions:

**Automatic detection**:
- `POST /resources/{id}/<action>` patterns (e.g., `POST /servers/{id}/reboot`) → Action
- POST operations whose `operationId` leading verb is a recognized action verb → Action
- POST operations that are not a CRUD Create path → Action
- PUT/PATCH or DELETE on a collection path (bulk update/clear) → Action
- Operations annotated with `x-terraform-action: true` → Action

**Explicit configuration** (`generator.yaml`):
```yaml
action_overrides:
  - operation: "rebootServer"
    name: "reboot_server"
    description: "Reboots the specified server"
    progress_messages: true   # Stream progress updates during invocation
    modify_plan: true          # Generate ModifyPlan for API-accessible validation
  - operation: "rotateDatabaseCredentials"
    name: "rotate_database_credentials"
    description: "Rotates database credentials immediately"
```

#### Generated Code Structure

```go
// internal/provider/action_reboot_server.go

type RebootServerAction struct {
    client *client.Client
}

func (r *RebootServerAction) Metadata(ctx context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
    resp.TypeName = req.ProviderTypeName + "_reboot_server"
}

func (r *RebootServerAction) Schema(ctx context.Context, req action.SchemaRequest, resp *action.SchemaResponse) {
    resp.Schema = schema.Schema{
        Attributes: map[string]schema.Attribute{
            "server_id": schema.StringAttribute{
                Required:    true,
                Description: "The ID of the server to reboot.",
            },
            "force": schema.BoolAttribute{
                Optional:    true,
                Description: "Force a hard reboot.",
            },
        },
    }
}

func (r *RebootServerAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
    var config RebootServerActionModel
    resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
    if resp.Diagnostics.HasError() {
        return
    }
    // Generated HTTP call to POST /servers/{serverId}/reboot
    reqPath := "/servers/{serverId}/reboot"
    reqPath = strings.ReplaceAll(reqPath, "{serverId}", config.ServerId.ValueString())
    httpReq, err := r.client.NewRequest(ctx, http.MethodPost, reqPath, nil)
    if err != nil {
        resp.Diagnostics.AddError("Error invoking ...", fmt.Sprintf("Could not build request: %s", err))
        return
    }
    httpResp, err := r.client.Do(httpReq)
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
| `ValidateConfig` | Validate configuration offline | Scaffolded unless an explicit `validate_config_operation` mapping is declared |
| `ModifyPlan` | Validate with API access | Optional; generated when `modify_plan: true`, wired only with an explicit `modify_plan_operation` mapping |

#### Streaming Progress Messages

For long-running actions (e.g., `POST /servers/{id}/migrate`), Eidos generates code that calls `resp.SendProgress(action.InvokeProgressEvent{Message: "Invoking <action>"})` before issuing the request, streaming a progress update back to the practitioner during invocation. This is enabled via `progress_messages: true` in the `action_overrides` config.

### 8.8 Ephemeral Resources

**Ephemeral resources** (Terraform 1.10+) are resources whose data is guaranteed NOT to be persisted in state or plan files. They follow an Open → (Renew) → Close lifecycle and can only be referenced in ephemeral contexts (e.g., write-only arguments on managed resources).

This is the natural Terraform mapping for OpenAPI operations that produce short-lived, sensitive, or temporary data — credentials, tokens, signed URLs, temporary access grants.

#### How Ephemeral Resources Map from OpenAPI

**Automatic detection**:
- POST operations whose path contains an ephemeral keyword (`credentials`, `token`, `session`, `lease`, `ticket`) and is not a lifecycle subpath → Ephemeral resource
- Operations annotated with `x-terraform-ephemeral: true` → Ephemeral resource
- The sibling lifecycle operations (`/renew`, `/close`, `/revoke`, `/refresh`, `/rotate`) of an ephemeral open are consumed as its Renew/Close mappings rather than emitted as separate constructs

**Explicit configuration** (`generator.yaml`):
```yaml
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
    client *client.Client
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
list_resource_overrides:
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
    client *client.Client
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
- Explicit `write_only_attributes` entries in `resource_overrides` → Write-only argument

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

**Generated server setup** (from the reference `test/specs/mycloud.yaml`):

```go
func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "Set to true to run the provider with support for debuggers like delve")
	var protocolVersion int
	flag.IntVar(&protocolVersion, "protocol-version", 6, "Terraform plugin protocol version to serve (5 or 6)")
	var printVersion bool
	flag.BoolVar(&printVersion, "version", false, "Print version information and exit")
	flag.Parse()
	if printVersion {
		fmt.Printf("version=%s\ncommit=%s\ndate=%s\n", version, commit, date)
		return
	}
	address := "registry.terraform.io/mycloud/mycloud"
	if protocolVersion == 5 {
		err := tf5server.Serve(address, providerserver.NewProtocol5(provider.New()))
		if err != nil {
			log.Fatal(err)
		}
		return
	}
	if protocolVersion != 6 {
		log.Printf("warning: unsupported --protocol-version %d; defaulting to protocol version 6", protocolVersion)
	}
	opts := providerserver.ServeOpts{Address: address, Debug: debug}
	err := providerserver.Serve(context.Background(), provider.New, opts)
	if err != nil {
		log.Fatal(err)
	}
}
```

### 9.2 Protocol Version 5 (Fallback)

Protocol v5 is compatible with Terraform CLI >= 0.12. It lacks nested attributes and provider-defined functions.

The generated `main.go` always serves Protocol v6 by default and includes a `tf5server.Serve` fallback path selected by the generated binary's runtime `-protocol-version` flag (5 or 6). The eidos CLI itself has no `--protocol-version` flag; the choice is made at provider runtime.

---

### 9.3 gRPC Server Lifecycle

The generated provider binary:
1. Starts a gRPC server listening on a Unix domain socket (or named pipe on Windows).
2. Terraform CLI discovers and starts the binary via `terraform init`.
3. Terraform sends `ConfigureProvider`, `ValidateProviderConfig`, and then CRUD RPCs.
4. The provider processes each RPC, calls the generated HTTP client, and returns results.
5. On `SIGINT`, the provider gracefully shuts down.

The generated `main.go` handles debug mode via a `-debug` flag passed to `providerserver.ServeOpts{Debug: ...}` for development.

### 9.4 DynamicValue Serialization

Terraform transmits resource state between CLI and provider as `DynamicValue` — MessagePack or JSON encoded `tftypes.Value`. The generated value mappers handle:

- **Plan → Model**: `req.Plan.Get(ctx, &model)` — Framework handles this natively.
- **Model → State**: `resp.State.Set(ctx, &model)` — Framework handles this natively.
- **Value mappers**: `internal/protocol/value_mappers.go` provides `XxxModelFromValue()` / `XxxModelToValue()` converters between `tftypes.Value` and the generated Go models. String-typed attributes (including `format: byte` / `format: binary`) map to `tftypes.String`; no base64 encoding is applied.

---

### 9.5 Dedicated OpenAPI Parser

§7.2 introduces the parser's role in the pipeline; this section details the
design. Eidos does **not** depend on third-party OpenAPI parsing libraries such
as `libopenapi` or `kin-openapi`. Instead, it ships a purpose-built parser
inside `pkg/parser/` that is tailored to the needs of Terraform provider
generation.

#### Why a dedicated parser?

| Concern | Dedicated parser approach |
|---------|---------------------------|
| Spec-version parity | Single codebase normalizes 2.0, 3.0.x, and 3.1.x into the same internal model |
| Dependency surface | No external OpenAPI parser in `go.mod`; fewer breaking changes from upstream |
| Error context | Every IR node carries `SourceLocation` (file:line) for precise diagnostics |
| Feature control | Add support for new JSON Schema / OpenAPI keywords on Eidos's own schedule |
| Performance | Streaming YAML/JSON decode with optional memory budget and recursion limits |

#### Parser structure

```text
pkg/parser/
├── doc.go              # Package documentation
├── lexer.go            # YAML/JSON tokenization into a generic AST with source locations
├── spec.go             # Generic OpenAPI document model (version-agnostic Spec)
├── version.go          # Version detection (2.0 / 3.0.x / 3.1.x) and diagnostics types
├── ref_local.go        # Local JSON Pointer $ref resolution (non-local refs rejected)
├── circular.go         # Circular schema $ref cycle detection; marks participants Opaque
├── validate.go         # Structural validation and semantic checks
├── limits.go           # Resource usage guardrails (recursion/memory limits)
├── helpers.go          # Generic AST node helpers
├── v2.go               # Swagger 2.0 → generic spec model
├── v30.go              # OpenAPI 3.0.x → generic spec model (shared converter state)
└── v31.go              # OpenAPI 3.1.x → generic spec model
```

#### Parsing pipeline

1. **Detect version** from `swagger` (2.0) or `openapi` (3.0.x / 3.1.x).
2. **Decode raw bytes** into a thin, typed YAML/JSON AST.
3. **Version-specific conversion** maps raw AST nodes to the generic `Spec` model.
4. **Generic resolution** dereferences local `$ref` values, detects circular schema refs (marking participants `Opaque`), and validates structural correctness. `allOf` merging and polymorphism resolution happen later in the transformer, not in the parser.
5. **Return normalized model** to the transformer.

#### Supported inputs

| Format | Detection |
|--------|-----------|
| YAML | File extension `.yaml`/`.yml` or leading `---` |
| JSON | File extension `.json` or leading `{`/`[` |

#### `$ref` resolution

- Local JSON Pointers (`#/components/schemas/Pet`) are resolved against the in-memory spec.
- External file references (`./models.yaml#/Pet`) are rejected with a fail-loud error diagnostic; only same-document refs resolve.
- Remote references (`https://example.com/spec.yaml#/Pet`) are rejected with a fail-loud error diagnostic rather than fetched.
- Circular references are detected and reported as warnings; the parser marks the cyclic ref holders `Opaque` so the transformer bounds their expansion (up to `maxCyclicDepth` levels, then an opaque boundary) instead of expanding them unboundedly.

#### Validation

The parser reports diagnostics at the source location rather than failing silently:

| Issue | Diagnostic |
|-------|------------|
| Missing required field | Error with file:line |
| Invalid `$ref` target | Error with ref path and location |
| Type mismatch | Warning with suggested coercion |
| Unsupported keyword | Warning describing limitation |
| Circular `$ref` | Warning; ref holder marked Opaque and expansion bounded to `maxCyclicDepth` levels |

#### Technology stack update

Because the parser is dedicated, Eidos has no third-party OpenAPI parsing
dependency: `github.com/pb33f/libopenapi` and `github.com/getkin/kin-openapi`
are not part of the technology stack (see §16). They are replaced by the
in-house parser packages. Parser unit tests use embedded fixtures in
`pkg/parser/testdata/`; the reference specs in `test/specs/` drive the
end-to-end golden tests in `pkg/generator/golden_test.go`, which assert on the
resulting IR and generated file list.

---

## 10. OpenAPI-to-Terraform Type Mapping

| OpenAPI `type` | OpenAPI `format` | Terraform Framework Type | Go Model Type |
|----------------|-------------------|------------------------|----------------|
| `string` | (none) | `schema.StringAttribute` | `types.String` |
| `string` | `date-time` | `schema.StringAttribute` | `types.String` |
| `string` | `date` | `schema.StringAttribute` | `types.String` |
| `string` | `email` | `schema.StringAttribute` | `types.String` |
| `string` | `uuid` | `schema.StringAttribute` | `types.String` |
| `string` | `uri` | `schema.StringAttribute` | `types.String` |
| `string` | `password` | `schema.StringAttribute` | `types.String` (`Sensitive: true` for ephemeral-resource password-format properties and write-only attributes) |
| `string` | `byte` | `schema.StringAttribute` | `types.String` |
| `string` | `binary` | `schema.StringAttribute` | `types.String` |
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
| `null` (in type array) | — | The `"null"` entry is dropped and the remaining type maps normally; a lone `"null"` or a multi-type array falls back to `schema.DynamicAttribute` | `types.String` / `types.Dynamic` |
| `oneOf` | — | `schema.SingleNestedAttribute` (discriminated union) or `schema.DynamicAttribute` | See Section 12 |
| `anyOf` | — | `schema.SingleNestedAttribute` (discriminated union) or `schema.DynamicAttribute` | See Section 12 |
| `allOf` | — | Flattened into a single flat object schema | Merged model struct |
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
| `http/bearer` | `bearer_token` (Sensitive, String) — qualified to `<scheme>_token` when the spec declares several bearer schemes | `Authorization: Bearer <token>` |
| `oauth2/client_credentials` | `client_id`, `client_secret` (Sensitive), `token_url`, `scopes` | Token exchange at `token_url`, cache token, inject `Authorization: Bearer <token>` |
| `oauth2/authorization_code` | `client_id`, `client_secret` (Sensitive), `auth_url`, `token_url`, `refresh_token` (Sensitive), `scopes` | Refresh-only: the initial code exchange is interactive and happens out-of-band; the provider refreshes the supplied `refresh_token` at `token_url` (handling rotation) and injects `Authorization: Bearer <token>` |
| `oauth2/implicit` | `auth_url`, `scopes` | No interceptor (interactive redirect; deprecated in OAuth 2.1); fail-loud runtime warning |
| `oauth2/password` | `username`, `password` (Sensitive), `client_id`, `client_secret` (Sensitive), `token_url`, `scopes` | Password-grant token exchange at `token_url`, cache token, inject Bearer |
| `openIdConnect` | `oidc_token_url`, `client_id`, `client_secret` (Sensitive) | Token endpoint from `oidc_token_url` override, else OIDC discovery (cached); client-credentials token fetch, inject Bearer |

Multiple security schemes on a single operation are resolved with **AND semantics**, not OR: an operation declaring exactly one security requirement authenticates with exactly that requirement's schemes via `client.WithSchemes(...)` (per-operation AND). An operation declaring no `security` inherits the global default (every configured scheme interceptor applies); an operation declaring a single empty requirement (`security: [{}]`) is unauthenticated. An operation (or global `security`) declaring **more than one requirement** — OR, where any one suffices — is ambiguous for a non-interactive provider: eidos applies every declared scheme (AND of all, which is stricter than OR) and emits a fail-loud Warning diagnostic (`warnPerOpORSecurity` for per-operation OR; the global case warns via `buildSecurityIR`). This is fail-loud, not silent — a non-interactive Terraform provider cannot reliably try/fallback across OR alternatives.

A spec declaring **more than one bearer scheme** qualifies each scheme's provider attribute with the scheme name (`account_token`, `agent_token`, …) via `transformer.BearerTokenAttributeName`, so each interceptor reads its own token and per-operation `WithSchemes(...)` selection is meaningful. A single bearer scheme keeps the canonical `bearer_token`. Both the config-schema mapping (`applySecurityConfigAttributes`) and the generated Configure wiring (`pkg/generator/provider_auth.go`) resolve the same helper, so the attribute a practitioner sets is the attribute the interceptor reads.

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

OpenAPI models polymorphic types with `allOf` (composition), `oneOf`
(exclusive union), and `anyOf` (inclusive union), plus `discriminator` for
variant switching. This section covers how each maps into the IR and generated
Terraform attributes; the `generator.yaml` `polymorphism` settings that control
these mappings are documented in `docs/usage.md` §`polymorphism`.

### 12.1 `allOf` (Composition)

All schemas in `allOf` are merged into a single flat object. Property conflicts produce an error. Required fields from any subschema are combined.

```yaml
allOf:
  - $ref: '#/components/schemas/BasePet'
  - type: object
    properties:
      status:
        type: string
```

→ A single flat object schema with all properties from `BasePet` plus `status`.

### 12.2 `oneOf` (Exclusive Union)

`oneOf` is the most polymorphic OpenAPI construct and has no single Terraform equivalent. Eidos supports **two generation strategies** that the user can control globally or per-schema via `generator.yaml`.

#### Strategy A: Preserve as a dynamic union (default)

Without a `discriminator`: mapped to a `DynamicAttribute` (Terraform can hold any shape). The generated `Read` method populates the dynamic value based on the actual API response shape.

With a `discriminator`: mapped to a type-switched `SingleNestedAttribute`:

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
| Top-level resource schema is a `oneOf` of named, object-like variants | Split resources | Each variant is effectively a different resource kind |

Strategy selection precedence is: a per-schema `polymorphism.oneOf[i].strategy` override, then the global `polymorphism.strategy` default, then the heuristics above. There is no `x-terraform-polymorphism` spec extension — split/union is chosen via `generator.yaml` or the heuristics.

When `strategy: split_resources` is chosen, Eidos also generates:
- A shared `Model` struct and helper functions if a base schema exists.
- The discriminator attribute is omitted from each variant's schema because the Terraform resource type itself encodes the variant (no per-variant "reject other variants" validator is emitted — the resource type is the switch).
- Documentation that clearly states which resource type corresponds to which API variant.

The default remains the existing dynamic-union behavior, so existing specs and configs continue to work unchanged.

### 12.3 `anyOf` (Inclusive Union)

A top-level `anyOf` reaches the IR as a union (kind `AnyOf`) and renders with the same `dynamic_union` strategy as `oneOf`: a discriminated `anyOf` renders as a `SingleNestedAttribute` with a `DiscriminatorValidator`, and an undiscriminated one renders as a `DynamicAttribute`. Nested `anyOf` (inside properties, collection elements) is fail-loud: it emits a `warnCompositionNotModeled` Warning and falls back to `DynamicAttribute` because the flat Terraform attribute model cannot represent alternatives. No `AnyOfValidator` is emitted — the `DiscriminatorValidator` (allowed-keys check) is the only validator produced for a discriminated union.

### 12.4 Circular References

When `$ref` forms a cycle (e.g., `Person` → `children: Person[]`), the parser's
`DetectCircularSchemaRefs` detects it and emits a `Warning` diagnostic
("Circular schema reference" — the ref "resolves back to itself or an ancestor
schema, forming a cycle"). Every schema whose `$ref` participates in a cycle is
then marked `Opaque` (`markCircularSchemaRefs`); the parser marks the *ref
holder* (the property/inline schema carrying the cyclic `$ref`), not the
referenced component.

The transformer (`schemaSpecFromParserDepth` in `pkg/transformer/from_parser.go`)
expands a cyclic ref a bounded number of levels before cutting it, rather than
treating the first entry as an opaque boundary. `cycleDepth` counts how many
cyclic (Opaque) `$ref` edges have been descended on the current path; a cyclic
ref is expanded up to `maxCyclicDepth` (2) levels — preserving the first-entry
properties of the circular schema so the generated attribute is a concrete
object, not `DynamicAttribute` — and deeper re-entry is cut to an opaque
(scalar-only) boundary. This keeps the IR finite and shallow: a dense cyclic
component graph is expanded a fixed number of levels per operation instead of
re-expanding the whole graph, so generation terminates on specs that previously
hung. Because cyclic refs are not added to the path-local `visited` set,
`cycleDepth` is the only path-varying dimension for an Opaque ref, so the
conversion of a schema at a given `cycleDepth` is path-independent and memoizing
on `(schema, cycleDepth)` is sound (the previous schema-pointer-only memo could
conflate the expanded and cut forms). The `visited` set remains as a backstop
for cycles the parser did not mark Opaque (a synthetic or malformed spec).

---

## 13. Error Handling & Diagnostics

The generated client translates HTTP failures into a single `client.APIError`
value (`NewAPIError`), and every resource/data-source CRUD method surfaces it as
a Terraform `diag.Diagnostics` error. There is no per-status-code diagnostic
wording; the status code and (truncated) response body are carried verbatim:

| API Error | Behavior |
|-----------|----------|
| Any non-success status | `NewAPIError` captures the status code, headers, and body (capped at 1 MiB stored, 1 KiB displayed) and the CRUD method emits `AddError("Error <verb>ing <resource>", apiErr.Error())` where `Error()` renders `API error status=<code> body=<body>` |
| 404 Not Found (on Read) | `resp.State.RemoveResource(ctx)` — resource dropped from state |
| 404 Not Found (on Delete) | Success (already deleted) |
| 429 Too Many Requests | Retried by `DefaultRetryPolicy` (see below) |
| 5xx (500/502/503/504) | Retried by `DefaultRetryPolicy` (see below) |
| Network / transport error | `AddError("Error <verb>ing <resource>", "Could not send request: <err>")` |
| Response body decode error | `AddError("Error <verb>ing <resource>", "Could not decode response body: <err>")` |

Retries are applied inside `client.Do` via `DoWithRetry`: `DefaultRetryPolicy`
retries on network errors, HTTP 5xx, and 429 (never on context cancellation or
deadline exceeded), and `DefaultBackoff` returns exponential backoff with
additive jitter, clamped to the provider's configured `RetryWaitMin`/`RetryWaitMax`
windows (default 1s–30s). The retry count and wait windows come from the
`global_timeouts`/retry configuration in `generator.yaml`.

The generated client also defines an `ErrorResponse` struct (`message`/`code`
fields) for APIs that wrap errors in a predictable envelope, but it is not
wired into diagnostics — error bodies are surfaced raw via `APIError`, not
parsed into structured fields.

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
  - "deleteAdminPet"               # Don't generate a resource for this
  - "OPTIONS*"

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

```text
terraform-provider-mycloud/
├── main.go                                    # Provider server entrypoint
├── go.mod                                     # Module definition
├── GNUmakefile                                # Build, test, lint targets
├── .goreleaser.yml                            # Release automation
├── .github/
│   └── workflows/
│       └── release.yml                        # CD: goreleaser + registry publish
├── terraform-registry-manifest.json           # Registry manifest
├── README.md                                  # Auto-generated overview
├── generator.yaml                             # Configuration used to generate this provider
├── internal/
│   ├── provider/
│   │   ├── provider.go                        # Provider struct, Schema, Configure, Resources, DataSources, Actions, EphemeralResources, ListResources, Functions
│   │   ├── provider_test.go                   # Unit tests
│   │   ├── resource_<name>.go                 # Resource: Create, Read, Update, Delete, ImportState
│   │   ├── resource_<name>_test.go            # Unit tests
│   │   ├── resource_<name>_acceptance_test.go # Acceptance tests
│   │   ├── data_source_<name>.go              # Data source: Read
│   │   ├── data_source_<name>_test.go         # Unit tests
│   │   ├── action_<name>.go                   # Action: Invoke, ModifyPlan, progress messages
│   │   ├── ephemeral_<name>.go                # Ephemeral resource: Open, Renew, Close
│   │   ├── list_<name>.go                     # List resource: List (for tfquery)
│   │   ├── function_<name>.go                 # Provider-defined function
│   │   ├── model_<name>.go                    # Go struct models
│   │   ├── json_convert.go                    # JSON/model conversion helpers (emitted when any resource, data source, or ephemeral resource is wired)
│   │   └── validators.go                      # Custom validators (discriminator, etc.)
│   ├── client/
│   │   ├── client.go                          # HTTP client: request construction, retry
│   │   ├── auth.go                            # Auth middleware (emitted when the spec declares security schemes)
│   │   ├── models.go                          # Request/response Go structs
│   │   ├── errors.go                          # Typed error structs
│   │   ├── retry.go                           # Exponential backoff + jitter
│   │   ├── pagination.go                      # Pagination helpers
│   │   └── logging.go                         # Request/response trace logging
│   └── protocol/
│       ├── value_mappers.go                   # tftypes.Value ↔ Go struct converters
│       └── value_mappers_test.go              # Value mapper round-trip tests
├── docs/
│   ├── index.md                               # Provider overview + auth guide
│   ├── resources/
│   │   └── <name>.md                          # Resource documentation
│   ├── data-sources/
│   │   └── <name>.md                          # Data source documentation
│   ├── actions/
│   │   └── <name>.md                          # Action documentation
│   ├── ephemeral-resources/
│   │   └── <name>.md                          # Ephemeral resource documentation
│   ├── list-resources/
│   │   └── <name>.md                          # List resource documentation
│   └── functions/
│       └── <name>.md                          # Provider-defined function docs
├── examples/
│   ├── resources/
│   │   └── <name>/
│   │       └── resource.tf                    # Minimal HCL example
│   ├── data-sources/
│   │   └── <name>/
│   │       └── data-source.tf
│   ├── actions/
│   │   └── <name>/
│   │       └── action.tf                      # Action invocation example
│   └── ephemeral-resources/
│       └── <name>/
│           └── ephemeral-resource.tf          # Ephemeral resource example
└── tests/                                     # Only with --generate-terraform-tests
    ├── <name>.tftest.hcl                      # Native Terraform test
    └── modules/
        └── <name>/
            └── main.tf                        # Terraform test module
```

`go.sum`, `.golangci.yml`, a `Makefile` symlink, `templates/`, `tools/`, and `examples/provider/` are **not** generated; they are created by the operator (e.g. `go mod tidy` produces `go.sum`).

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
| Logging | [`terraform-plugin-log`](https://github.com/hashicorp/terraform-plugin-log) | Provider trace logging |
| Testing | [`terraform-plugin-testing`](https://github.com/hashicorp/terraform-plugin-testing) | Acceptance tests |
| Documentation | In-house Markdown generation (`pkg/generator/docs.go`) | Registry-compatible Markdown, no `terraform-plugin-docs` dependency |
| CLI | [`spf13/cobra`](https://github.com/spf13/cobra) v1.x | Command-line interface |
| Templates | [`text/template`](https://pkg.go.dev/text/template) for text files; `pkg/generator/astgen` (`go/ast` + `go/format`) for Go source files | Go code generation |
| HTTP Client | [`net/http`](https://pkg.go.dev/net/http) | Generated API client |
| Retry | In-house retry logic in the generated `internal/client/retry.go` | Exponential backoff with jitter (no `go-retryablehttp` dependency) |
| Release | [`goreleaser/goreleaser`](https://goreleaser.com) | Multi-platform binary builds, signing, GitHub release |
| Linting | [`golangci-lint`](https://golangci-lint.run) | Code quality |

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
    @if [ -d tools ]; then cd tools && go generate ./...; fi

fmt:
    gofmt -s -w -e .

test:
    go test -v -cover -timeout=120s -parallel=10 ./...

testacc:
    TF_ACC=1 go test -v -cover -timeout 120m ./...

.PHONY: fmt lint test testacc build install generate
```

### 17.2 Version Injection

The generated `main.go` includes package-level `version`, `commit`, and `date`
variables that are injected at build time via `ldflags`:

```go
// main.go
var (
    version string = "dev"
    commit  string = "none"
    date    string = "unknown"
)
```

**GoReleaser ldflags**:
```yaml
ldflags:
  - -s -w -X main.version={{ .Version }} -X main.commit={{ .Commit }} -X main.date={{ .CommitDate }}
```

| Flag | Purpose |
|------|---------|
| `-s` | Omit symbol table (reduces binary size) |
| `-w` | Omit DWARF debug info |
| `-X main.version={{ .Version }}` | Inject release tag (e.g., `1.2.3`) |
| `-X main.commit={{ .Commit }}` | Inject short commit SHA |
| `-X main.date={{ .CommitDate }}` | Inject build date |

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

The generated `main.go` supports debug mode via a `-debug` flag (see §9.1 for
the full generated `main.go`):

```go
var debug bool
flag.BoolVar(&debug, "debug", false, "Set to true to run the provider with support for debuggers like delve")
// ...
opts := providerserver.ServeOpts{Address: address, Debug: debug}
err := providerserver.Serve(context.Background(), provider.New, opts)
```

**Debug workflow**:
1. Build without optimization: `go build -gcflags="all=-N -l" -o ./terraform-provider-mycloud .`
2. Start via Delve: `dlv exec --accept-multiclient --continue --headless ./terraform-provider-mycloud -- -debug`
3. Provider prints a `TF_REATTACH_PROVIDERS` JSON string to stdout.
4. Run Terraform with that env var: `TF_REATTACH_PROVIDERS='...' terraform apply`
5. Terraform reattaches to the already-running provider process (breakpoints work).

### 17.6 `go generate` Integration

Eidos does **not** emit a `tools/` directory. The generated `GNUmakefile` `generate` target is a conditional no-op that runs `go generate` only when the operator has added a `tools/` directory:

```make
generate:
	@if [ -d tools ]; then cd tools && go generate ./...; fi
```

Operators who want `go generate` integration (e.g. `copywrite` headers, `tfplugindocs` documentation) add their own `tools/tools.go` with the appropriate `//go:generate` directives; the generated `GNUmakefile` picks it up automatically.

### 17.7 Multi-Platform Builds

The generated `.goreleaser.yml` builds for every GOOS/GOARCH combination in the
matrix below (the `ignore` list removes the unsupported pairs):

| GOOS | GOARCH |
|------|--------|
| linux | amd64, arm, arm64, 386 |
| darwin | amd64, arm64 |
| windows | amd64, arm, 386 |
| freebsd | amd64, arm, arm64, 386 |
| openbsd | amd64, 386 |
| solaris | amd64 |

`CGO_ENABLED=0` is set for all targets. The explicitly excluded pairs are
`darwin/386`, `darwin/arm`, `openbsd/arm`, `openbsd/arm64`, `solaris/386`,
`solaris/arm`, `solaris/arm64`, and `windows/arm64`. The binary is named
`terraform-provider-<name>_v{{ .Version }}` and archives use the
`{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}` template (zip on
Windows, tar.gz elsewhere).

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

- **Acceptance tests** (opt-in): generated acceptance suites run under `TF_ACC=1` against a local deterministic mock server (`testfixtures/live`) — no external API is involved, and `TF_ACC=1` gates them out of the default `go test ./...` run.
- **Multi-spec regression**: Run the generator against the reference corpus in `test/specs/` (14 specs) and verify the generated code compiles and passes `go vet`.
- **Reference live e2e (Mycloud)**: the strongest validation case run so far. Eidos generates a provider from the reference Mycloud spec (`test/specs/mycloud.yaml`), injects a live connectivity test, and runs the generated acceptance suite with `TF_ACC=1` against a local deterministic mock server that validates the generated client's auth plumbing (`testfixtures/live`). No external system is involved.

### 18.5 Test Infrastructure

```text
test/
├── specs/                    # Reference OpenAPI specs (14)
│   ├── mycloud.yaml
│   ├── mycloud-pets.yaml
│   ├── mycloud-data.yaml
│   ├── complex-polymorphism.yaml
│   ├── circular-references.yaml
│   └── ... (14 total)
└── fixtures/                 # Empty (only .gitkeep)

testfixtures/
├── golden/                   # Golden snapshots, one per reference spec
│   ├── mycloud.golden.json
│   ├── mycloud-pets.golden.json
│   └── ... (14 total)
└── live/                     # Live e2e tests (TF_ACC=1, local mock server)
    └── live_test.go

pkg/generator/golden_test.go  # Parse→transform→generate integration tests
```

### 18.6 `terraform test` Framework

Terraform 1.6+ introduced a native testing framework using `.tftest.hcl` files. Eidos generates `.tftest.hcl` test files alongside the provider for integration testing without Go code. For each managed resource it emits an orchestration file plus a supporting module:

```hcl
# tests/pet.tftest.hcl
run "create_pet" {
  command = apply

  variables {
    name = "example"
  }

  module {
    source = "./tests/modules/pet"
  }

  assert {
    condition     = output.id == "generated"
    error_message = "unexpected id"
  }
}
```

```hcl
# tests/modules/pet/main.tf
terraform {
  required_providers {
    mycloud = {
      source = "mycloud/mycloud"
    }
  }
}

provider "mycloud" {
  api_key = "example"
}

variable "name" {
  type = string
}

resource "mycloud_pet" "example" {
  name = var.name
}

output "id" {
  value = mycloud_pet.example.id
}
```

**Generation trigger**: `.tftest.hcl` files are generated only when `eidos generate` is invoked with `--generate-terraform-tests` or when `generator.yaml` contains:

```yaml
generate_terraform_tests: true
```

When disabled (the default), no `tests/` directory is emitted, keeping the output minimal.

**Generated test coverage**:
- One `run` block per resource (`create_<name>`) with `command = apply`; there are no Update, Import, or Destroy run blocks.
- A `variables` block populated from the resource's required top-level primitive attributes, using placeholder values (not OpenAPI `example` fields).
- A `module` block referencing the supporting `tests/modules/<name>` module.
- An `assert` block checking that the generated resource id matches the placeholder value produced by the generated provider's Create implementation.
- The supporting module declares `required_providers`, configures the provider with dummy placeholder values for its required attributes, declares variables for the resource's required primitives, and emits the resource plus an `output` for its identifier.

---

## 19. Terraform Registry Publishing

Eidos generates all artifacts required for Terraform Registry publishing:

| Artifact | Purpose |
|----------|---------|
| `terraform-registry-manifest.json` | Registry metadata (`protocol_versions: ["6.0"]`; the `ProtocolVersions` build field can override it) |
| `.goreleaser.yml` | Multi-OS/arch binary builds and SHA256SUMS; GPG signing is emitted only when `SignRelease` is enabled |
| `.github/workflows/release.yml` | CI/CD: tag → build → publish; GPG key import/signing steps only when `SignRelease` is enabled |
| `docs/` | Registry-rendered documentation |
| `examples/` | Registry-rendered examples |
| `README.md` | Provider overview with installation instructions |

**Registry naming requirement**: The GitHub repository must be named `terraform-provider-<NAME>`, where `<NAME>` matches the provider type name.

**Signing**: GPG signing is not automatic. The generator's `.goreleaser.yml` and
release-workflow templates include a `signs:` block and GPG key-import step only
when the `SignRelease` build option is enabled (which requires configuring
`GPG_PRIVATE_KEY` and `GPG_PASSPHRASE` repository secrets and uploading the
public key to the Registry). `SignRelease` is currently a `BuildConfig` field
that is not wired to any `generator.yaml` key or CLI flag, so generated releases
are unsigned by default.

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
  specs exercise wired bodies at scale (the mycloud spec wires 7 resources —
  config, instance, network, project, secret, stack, workspace — to the
  generated client, alongside 17 data sources and 12 list resources; see
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
| 6 | Circular `$ref` in specs | Medium | Low | Detect cycles during parsing; mark cyclic ref holders `Opaque` and emit a warning; the transformer expands a cyclic ref up to `maxCyclicDepth` levels (preserving first-entry properties) then cuts deeper re-entry to an opaque boundary, so the IR stays finite and generation terminates on dense cyclic specs that previously hung. |
| 7 | Ambiguous CRUD inference (multiple POST endpoints for same resource) | Medium | Medium | Require explicit `generator.yaml` override for ambiguous operations; emit error listing candidates. |
| 8 | Non-RESTful APIs (GraphQL, gRPC, WebSocket) | Low | Low | Document that Eidos targets REST/HTTP APIs only; non-REST APIs produce warnings. |
| 9 | Spec version incompatibilities (2.0 vs 3.0 vs 3.1 edge cases) | Medium | Medium | Dedicated in-house parser with a comprehensive version-specific test suite; no reliance on third-party parser behavior. |
| 10 | Terraform state drift for PATCH-only APIs | Medium | Medium | Wired Update bodies call `preserveStateIntoPlan` to carry known state values (e.g. optimistic-concurrency versions) into the plan so the request body is complete (G20); the framework's post-Update Read refreshes state. |
| 11 | Generated code readability and maintainability | Medium | High | Use `pkg/generator/astgen` (`go/ast` + `go/format`) for programmatic Go generation; follow HashiCorp's provider conventions; add comments with spec source references. |
| 12 | Multi-file specs (`$ref` to external files/URLs) | Medium | Medium | Local `$ref` resolution with cycle detection; external/remote `$ref` values are rejected with an error diagnostic rather than fetched. |
| 13 | State Stores (experimental Terraform 1.15+) | Low | Low | State Stores are currently experimental and offered without compatibility promises. Eidos tracks the feature but does not generate State Store implementations until GA. Document as a future milestone. |
| 14 | Actions, Ephemeral Resources, List Resources require Terraform CLI >= 1.10–1.14 | Low | Medium | The generated provider does not set a minimum Terraform version constraint; these constructs are emitted whenever the spec/overrides infer them. Practitioners on older Terraform CLIs must pin a compatible provider version themselves. |

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
| **`go generate`** | A Go toolchain command that runs directives embedded in Go source files. Eidos does not emit `//go:generate` directives; the generated `GNUmakefile` `generate` target runs `go generate` only when the operator adds a `tools/` directory (see §17.6). |
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
| Reference corpus barely wired | the mycloud reference spec (merging the two former external-shape reference specs) carries real response schemas exposing path params; list resources populate their config schema from the collection path's parameters (`transformer.ListResourceConfigSchema`). The mycloud spec wires 7 resources, 17 data sources, and 12 list resources; the corpus-wide wired share went 1→7 resources, 7→17 data sources, 1→12 list resources. `assertCorpusWiring` in the golden test guards per-spec wiring floors. |
| Swagger 2.0 formData end-to-end | `paramsFromOperation` and `transformer.createFormDataParams` decompose the v2 request-body form schema back into per-field parameters; primitive fields wire as `application/x-www-form-urlencoded` and binary uploads as `multipart/form-data` (`swagger-formdata` reference spec). |
| Ephemeral/function inference | `ephemeralFromOperation` populates config/result schemas and consumes lifecycle subpaths; `ephemeral-resources` and `provider-functions` reference specs exercise the inference end to end. |
| Remote `--spec` URL fetching | `eidos generate`/`generate-config --spec` accept an http(s) URL with scheme allowlist, SSRF guard, size/timeout caps, and env-var-only auth. |
| Release-please migration | Versioning and changelog generation moved to Google's release-please; GoReleaser runs in the release-please workflow. |
| Stale documentation | The former `CHANGELOG.md` §Known limitations and this roadmap were corrected (the changelog no longer carries a limitations section; limitations now live in §23 and `docs/usage.md`). |
| Live-API e2e validation | Generated providers from the reference Mycloud spec build, load their schemas, and pass a full connectivity/CRUD lifecycle against a local deterministic mock server (`testfixtures/live`, `TF_ACC=1`); no external system is involved. The G1–G21 gap register is closed; the rows below itemize the closures by category, with G-numbers in parentheses identifying the register items each row closes (G5, G6, G7, G13, G16, and G17 are folded into these categories rather than itemized individually). |
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
- **Actions have no result surface (upstream framework limit).**
  terraform-plugin-framework v1.19.0's `action.InvokeResponse` exposes only
  `Diagnostics` and `SendProgress` — there is no `Result` field, and every
  `action/schema` attribute rejects `Computed` — so a generated action that
  returns a value (e.g. SpaceTraders `register`'s token) cannot surface it. No
  broken code is emitted: the action reports success/failure and the response
  body is intentionally not decoded. Revisit when the framework adds an action
  output API.
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

### 23.3 Residual risk & investigation areas

These are not confirmed gaps and not accepted limitations; they are areas flagged
for verification that have not yet been broadly exercised against real specs.

- **Construct-name quality on specs that omit `operationId`.** Resource,
  data-source, and action inference relies on `operationId` for naming; when an
  operation omits it, eidos falls back to a `METHOD /path`-derived name. Name
  quality and collision behavior on a spec that omits `operationId` extensively
  has not been broadly verified. (`generator.yaml` overrides can always pin a
  name via the `METHOD /path` form.)
- **Golden snapshot coverage is structural, not content.** The golden test
  corpus records only `{Path, Reason}` per generated file (a structural
  fingerprint), so the full generated output is only visible by regenerating
  with `EIDOS_UPDATE_GOLDEN=1`. A change that preserves the file list and
  reason strings but alters body content would pass the golden test silently;
  richer snapshot content is a future hardening consideration.
