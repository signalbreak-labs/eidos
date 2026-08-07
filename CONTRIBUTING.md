# Contributing to eidos

Thanks for considering a contribution! This guide covers how to set up the
project, run the checks, and get a change merged.

## Development setup

Requirements:

- Go 1.26+ (the module pins `go 1.26.0` with `toolchain go1.26.5`; CI reads the
  version from `go.mod` — do not change the `toolchain` line without reviewing
  the CI impact)
- `golangci-lint` v2.x (config is `.golangci.yml`, version `"2"`)

Build and run the CLI:

```bash
go build -o eidos ./cmd/eidos
./eidos generate --spec test/specs/mycloud.yaml --dry-run
```

## Common commands

```bash
go test ./...                 # full test suite
go test -run TestGoldenFiles ./pkg/generator   # golden snapshot tests
golangci-lint run ./...        # lint (must pass)
gofmt -w .                     # format
go vet ./...                   # vet
```

Regenerate checked-in golden snapshots after intentionally changing generation
output:

```bash
EIDOS_UPDATE_GOLDEN=1 go test -run TestGoldenFiles ./pkg/generator
```

Regenerate the sample generated provider after changing the reference spec or
the generator:

```bash
make examples
```

## Making changes

1. **Fork and branch.** Work on a feature branch off `main`.
2. **Follow the conventions.** Read [`AGENTS.md`](AGENTS.md) and
   [`docs/standards.md`](docs/standards.md) — they are the canonical engineering
   standards (generation determinism, fail-loud diagnostics, honest scaffolds,
   no jennifer, `//nolint` discipline).
3. **Keep generation deterministic.** Identical spec + config must produce
   byte-identical output. No timestamps, random IDs, or nondeterministic
   ordering in generated code.
4. **Update goldens and the sample provider** when you change generation output
   (see commands above). CI fails on any diff.
5. **Run the full suite** — `go test ./...`, `golangci-lint run ./...`,
   `gofmt -w .`, `go vet ./...` — and make sure everything is green.

## Commit messages

This repo uses [Conventional Commits](https://www.conventionalcommits.org/) and
[release-please](https://github.com/googleapis/release-please) to drive
versioning and the changelog. The commit type determines the release bump:

- `feat(...)` → minor
- `fix(...)` / `perf(...)` / `chore(...)` → patch
- `BREAKING CHANGE` footer or `<type>!` → major
- `docs` / `test` / `style` / `refactor` / `build` / `ci` / untyped → no release

## Submitting a pull request

- Open a PR against `main` with a clear title and description.
- Reference any related issue.
- Ensure CI (lint, build, test, determinism checks) is green.
- Keep the diff focused; split unrelated changes into separate PRs.

## Reporting issues

Use the issue templates for bug reports and feature requests. For security
vulnerabilities, see [`SECURITY.md`](SECURITY.md) — do not open a public issue.
