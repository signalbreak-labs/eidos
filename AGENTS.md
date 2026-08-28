# AGENTS.md

Instructions for any AI coding agent working in this repository. This is the
canonical instruction file; vendor-specific files (e.g. `CLAUDE.md`) import it.

For full detail, see the linked docs in [Further reading](#further-reading).

## Project overview

Eidos is a Go CLI that generates Terraform providers from OpenAPI 2.0 / 3.0.x /
3.1.x specs. It parses a spec, normalizes and transforms it into an intermediate
representation (IR), and emits Terraform Plugin Framework code, documentation,
tests, and release tooling. Under active development; `eidos generate --dry-run`
and `eidos generate --output <dir>` are both usable (write mode refuses to
overwrite existing files unless `--force` is supplied).

## Hard rules

- **Never run `go mod tidy` without reviewing the `toolchain` line.** `go.mod`
  pins `go 1.26.0` + `toolchain go1.26.6`; CI resolves the toolchain from it.
  Do not change the `toolchain` line unless you intend to change the CI toolchain.
- **Generation must stay deterministic.** Identical spec + config must produce
  byte-identical output: no timestamps, random IDs, or nondeterministic
  ordering in generated code.
- **Fail loud, never silently.** Unsupported or ambiguous OpenAPI constructs must
  emit a diagnostic via `pkg/diagnostics`, never be dropped silently.
- **Generated bodies must be honest.** Resource CRUD bodies with a complete
  create/read/delete mapping are wired to the generated API client and make
  real HTTP calls. Bodies not yet wired (data sources, actions, ephemeral
  resources, list resources, functions, and resources with incomplete
  mappings) emit a clear runtime diagnostic
  (`resp.Diagnostics.AddError`/`AddWarning`) and carry the "is not wired to a
  remote API endpoint" scaffold marker — never an unconditional `TODO` stub,
  `panic`, or `NewFuncError("not implemented")`.
- **Do not reintroduce jennifer.** Go source is emitted only via
  `pkg/generator/astgen` (`go/ast`, `go/token`, `go/format`). `text/template` is
  for text files only (docs, README, examples, `.tftest.hcl`).
- **Every `//nolint` must name the specific linter and carry a reason.** Review
  `.golangci.yml` per-path exclusions before adding one.
- **Golden snapshots are not edited by hand.** Regenerate with
  `EIDOS_UPDATE_GOLDEN=1 go test -run TestGoldenFiles ./pkg/generator` only when
  intentionally changing generation output.

## Common commands

Build the CLI:

```bash
go build -o eidos ./cmd/eidos
```

Run all tests:

```bash
go test ./...
```

Run tests with race detection and coverage, matching CI:

```bash
go test -v -race -coverprofile=coverage.out ./...
```

Run a single package or test:

```bash
go test ./pkg/generator
go test -run TestGoldenFiles ./pkg/generator
go test -run 'TestGoldenFiles/mycloud-pets' ./pkg/generator
```

Update checked-in golden snapshots after changing generation output:

```bash
EIDOS_UPDATE_GOLDEN=1 go test -run TestGoldenFiles ./pkg/generator
```

Lint (requires golangci-lint v2.x; config is `.golangci.yml` version `"2"`):

```bash
golangci-lint run ./...
```

Format and vet:

```bash
gofmt -w .
go vet ./...
```

Run the CLI locally after building:

```bash
./eidos generate --spec test/specs/mycloud.yaml --dry-run
./eidos generate-config --spec test/specs/mycloud.yaml --output mycloud-generator.yaml
./eidos api --port 8080
./eidos mcp
```

`go test ./...` is expected to pass locally; skipped tests require optional
tooling (`goreleaser`) or `TF_ACC=1`.

## Repo layout

The pipeline is layered so each stage is testable independently:

```
CLI (cmd/eidos/) → Parser (pkg/parser/) → Transformer (pkg/transformer/)
  → IR (pkg/ir/) → Generator (pkg/generator/) → files on disk / dry-run summary
```

- `cmd/eidos/` — Cobra CLI (`root.go`): `generate`, `generate-config`, `api`
  (HTTP `POST /api/v1/validate`), `mcp` (MCP server over stdio). Tests build
  synthetic commands via `helpers_test.go` to avoid mutating global `os.Args`.
- `pkg/parser/` — in-house parsers for 2.0 (`v2.go`), 3.0.x (`v30.go`), 3.1.x
  (`v31.go`); version detection, local `$ref` resolution (external/remote refs
  are rejected with a diagnostic, not fetched), validation, circular-ref
  handling.
- `pkg/transformer/` — parser output → IR: `allOf` flattening, polymorphism,
  security, servers, params, naming normalization; CRUD/data-source/list/action/
  ephemeral/plan-modifier/validator inference; type mapping + overrides.
- `pkg/ir/` — pure data structs decoupling parser/transformer from generation
  (`ProviderIR`, `ResourceIR`, `DataSourceIR`, `ActionIR`, `EphemeralResourceIR`,
  `ListResourceIR`, `FunctionIR`, …). `server.go` defines `ServerIR`/
  `ServerVariableIR` for the OpenAPI `servers` URL template.
- `pkg/generator/` — emits the provider via `pkg/generator/astgen`.
  `Harness` (`harness.go`) writes deterministically (lexicographic order,
  duplicate-path detection, all-or-nothing render). `recorder.go` does record
  (`--dry-run`) and write modes. `golden_test.go` runs parse→transform→generate
  against `test/specs/` and compares to `testfixtures/golden/*.golden.json`, and
  enforces the scaffold-marker / no-stale-TODO invariants.
- `pkg/config/` — parses `generator.yaml` into a `BuildConfig`; `generator.go`
  backs `eidos generate-config`.
- `pkg/diagnostics/` — typed `Error`/`Warning`/`Info` with source locations.
- `test/specs/` — reference OpenAPI specs; `testfixtures/golden/` — snapshots;
  `testfixtures/live/live_test.go` — live e2e tests.

See `docs/PROJECT_DESIGN.md` for the full architecture and IR design.

## Conventions

- Write Go that reads like the surrounding code: match comment density, naming,
  and idiom. `goimports` local prefix is `github.com/signalbreak-labs/eidos`
  (local-prefixed imports group after third-party).
- Generated Go follows `terraform-plugin-framework` conventions with comments
  derived from spec descriptions.
- Generator panics on unexpected IR shapes must be returned as render errors via
  `renderFileSafely` (`pkg/generator/harness.go`), not crashed.
- Unit tests live next to the code they exercise (`*_test.go` alongside source).
  `errcheck` stays active for unchecked function calls in tests; intentionally
  ignored errors need `//nolint:errcheck // <reason>`.

## Toolchain & lint

- `go.mod`: `go 1.26.0`, `toolchain go1.26.6`. CI uses `go-version-file: go.mod`.
- `.golangci.yml` is a v2 config, lint Go version pinned to `1.26`. CI uses
  `golangci-lint-action@v9` (`v2.12.2`). An older binary will fail to parse it
  (and versions built with Go < the `go` directive in `go.mod`, e.g. v2.2.1,
  cannot load the config).
- Enabled linters include `revive`, `gocritic`, `gosec`, `errorlint`, `gocognit`,
  `nolintlint`, `errcheck` (type-assertions + blank assignments), `predeclared`,
  `unparam`, `copyloopvar`, `makezero`, `prealloc`, `bodyclose`, `noctx`,
  `misspell` (US).

See `docs/standards.md` for the full standards reference.

## Current limitations

- `uniqueItems: true` is honored with Set attributes for managed resources, data
  sources (array responses), ephemerals, and actions. The one exception is list
  resources: the experimental `list/schema` package has no Set types, so a list
  endpoint whose response array declares `uniqueItems: true` is downgraded to
  List and surfaced with a fail-loud `diagnostics.Warning` at transform time.
- OpenAPI operation inference for actions, ephemeral resources, list resources,
  and functions is unified with the transformer's inference layer (non-Create
  POSTs → actions; ephemeral Renew/Close from declared lifecycle paths;
  collection GETs with a paired instance Read promoted additively to list
  resources; function signatures from parameters + response schema).
  `generator.yaml` overrides remain the authoritative escape hatch.

See `docs/PROJECT_DESIGN.md` §1.1 (Implementation Status), §23 (Remaining Gaps &
Accepted Limitations), and `docs/usage.md` §Current limitations for the detailed
register.

## Further reading

- `docs/PROJECT_DESIGN.md` — architecture, IR design, OpenAPI coverage, component
  design, plugin-framework/protocol integration, implementation status, and the
  canonical register of remaining gaps and accepted limitations (§23, which
  supersedes the removed `docs/FINAL_GAPS.md`).
- `docs/standards.md` — consolidated engineering standards (toolchain, lint,
  formatting, testing, generation principles, release).
- `docs/usage.md` — CLI usage, `generator.yaml` reference, generated layout,
  troubleshooting.
- `CHANGELOG.md` — notable changes.