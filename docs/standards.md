# Eidos Engineering Standards

This document is the canonical reference for the standards this project follows:
toolchain and build, lint and formatting, testing, and the generation principles
that govern generated output. It consolidates rules that are also enforced
mechanically by `.golangci.yml`, `go.mod`, CI workflows, and the test suite.

For project architecture, see [`PROJECT_DESIGN.md`](PROJECT_DESIGN.md).  
For using the CLI, see [`docs/usage.md`](usage.md).

## Toolchain and build

- `go.mod` declares `go 1.26.0` and `toolchain go1.26.5`. Do not manually change
  the `toolchain` line unless you intend to change the CI toolchain.
- CI reads the Go version from `go.mod` (`go-version-file: go.mod`), so build/test
  jobs resolve to Go 1.26.5 via the toolchain directive.
- `golangci-lint` is intentionally pinned to Go 1.26 by `.golangci.yml`
  (`run.go: "1.26"`), separate from the module file.
- With the default `GOTOOLCHAIN=auto`, `go mod tidy` respects the toolchain
  directive and does not change it. Review any automatic `toolchain` change
  before committing if you override `GOTOOLCHAIN` (e.g. `=local`).

## Lint

- Config: `.golangci.yml`, version `"2"`. Requires golangci-lint v2.x; an older
  binary will fail to parse the config. CI uses `golangci-lint-action@v7` with
  `version: v2.12.2` (built with Go 1.26; versions built with Go < the `go`
  directive in `go.mod`, e.g. v2.2.1, cannot load the config).
- Run: `golangci-lint run ./...`. Note `golangci-lint` is not assumed to be
  installed in local environments, so the lint suite is effectively enforced
  in CI.
- Enabled linters include `revive`, `gocritic`, `gosec`, `errorlint`,
  `gocognit`, `nolintlint`, `errcheck` (with `check-type-assertions: true` and
  `check-blank: true`), `predeclared`, `unparam`, `usestdlibvars`, `copyloopvar`,
  `makezero`, `prealloc`, `bodyclose`, `noctx`, `misspell` (locale US), and
  `whitespace`.
- `//nolint` directives are governed by `nolintlint`: `allow-unused: false`,
  `require-explanation: true`, `require-specific: true`. Every `//nolint` must
  name the specific linter and carry a reason comment. Review the per-path
  exclusions in `.golangci.yml` before adding a new `//nolint`.
- `errcheck` is **not** blanket-excluded for `*_test.go`; only unchecked type
  assertions are excluded (a wrong type fails loudly). Test code that
  intentionally ignores a function error must use `//nolint:errcheck // <reason>`.
- `gocognit` is enabled for production code. It is excluded only for `*_test.go`
  and for `pkg/config/config.go` `Validate`, which is pending dedicated
  refactoring.

## Formatting

- `gofmt -s` and `goimports` are enforced via the `formatters` section of
  `.golangci.yml`.
- `goimports` local prefix is `github.com/signalbreak-labs/eidos`. Group imports so
  that local-prefixed packages come after third-party packages.
- Run `gofmt -w .` and `go vet ./...` locally before pushing.

## Testing

- Unit tests live next to the code they exercise (`*_test.go` alongside source).
- `go test ./...` is expected to pass locally. Match CI with:
  `go test -v -race -coverprofile=coverage.out ./...`.
- Golden regression tests live in `pkg/generator/golden_test.go`; specs are in
  `test/specs/` and checked-in snapshots in `testfixtures/golden/`. After
  intentionally changing generation output, update snapshots with:
  `EIDOS_UPDATE_GOLDEN=1 go test -run TestGoldenFiles ./pkg/generator`.
- Live end-to-end tests are in `testfixtures/live/live_test.go`; they require
  `TF_ACC=1`. GoReleaser snapshot tests require the `goreleaser` binary.
- Golden tests enforce two generation invariants that must be preserved:
  - Every generated CRUD/action/ephemeral/list/data-source/function body carries
    an honest "is not wired to a remote API endpoint" scaffold marker. A
    regression that drops or silently changes this marker is a test failure.
  - No stale `TODO` or `panic` markers remain in generated output.

## Generation principles

These principles (from `PROJECT_DESIGN.md` §3) are binding on the generator:

1. **Idempotent builds** — identical inputs (spec + config) produce identical
   output. No timestamps, random IDs, or nondeterministic ordering in generated
   code. The `Harness` writes files deterministically (lexicographic order,
   duplicate-path detection, all-or-nothing render phase).
2. **Fail loud, never silently** — unsupported or ambiguous OpenAPI constructs
   produce warnings or errors via `pkg/diagnostics`, never silent drops.
3. **Spec-version parity** — OpenAPI 2.0, 3.0, and 3.1 normalize into a single IR;
   the generator never branches on spec version.
4. **Layered generation** — parsing, normalization, IR transformation, and
   emission are separate so each layer can be tested independently.
5. **Human-readable output** — generated Go follows `terraform-plugin-framework`
   conventions, idiomatic naming, and includes comments derived from spec
   descriptions.

## Code generation standard

- All generated `.go` files are emitted via `pkg/generator/astgen`, the
  standard-library Go source builder (`go/ast`, `go/token`, `go/format`). The
  external `github.com/dave/jennifer` dependency and the vendored jennifer fork
  have been removed; `astgen` is the sole Go-source emission path.
- `text/template` is used only for text files: Markdown docs, README, examples,
  and Terraform test files.
- Generated bodies that are not yet wired to a real API must emit a clear runtime
  diagnostic (`resp.Diagnostics.AddError`/`AddWarning`) — never an unconditional
  `TODO` stub, `panic`, or `NewFuncError("not implemented")`.
- Generator panics on unexpected IR shapes are recovered by `renderFileSafely`
  (`pkg/generator/harness.go`) and returned as render errors rather than crashing
  the run.

## Release

- Release artifacts are produced by GoReleaser (`.goreleaser.yml`). Versioning
  and changelog generation are handled by the `release-please` GitHub Actions
  workflow (Google's release-please), which runs as the `signalbreak-release-bot`
  GitHub App so its PRs trigger CI and its tag pushes re-trigger `release.yml`.
  `release.yml` handles all `v*` tag pushes (human or bot) and, via GoReleaser's
  `homebrew_casks:` block, publishes the Homebrew cask to `signalbreak-labs/tap`.
- Generated providers ship with a registry manifest, GoReleaser config, and
  release workflow as scaffolding. This project does **not** publish to the real
  Terraform Registry.