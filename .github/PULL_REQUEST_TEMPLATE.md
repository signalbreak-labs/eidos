## Summary

<!-- What does this change do, and why? -->

## Test plan

<!-- How did you verify this change? -->

- [ ] `go test ./...` passes
- [ ] `golangci-lint run ./...` passes
- [ ] `gofmt -w .` and `go vet ./...` are clean
- [ ] Golden snapshots and the sample provider are regenerated if generation
      output changed (`EIDOS_UPDATE_GOLDEN=1 go test -run TestGoldenFiles
      ./pkg/generator` and `make examples`)

## Checklist

- [ ] Conventional Commits message (this repo uses release-please)
- [ ] No external-system references in examples/docs (everything uses the
      fictional `mycloud` provider)
- [ ] Generation stays deterministic (identical input → byte-identical output)
- [ ] Fail-loud diagnostics for unsupported/ambiguous OpenAPI constructs
