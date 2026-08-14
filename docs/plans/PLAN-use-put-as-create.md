# Optional `use_put_as_create` toggle

Status: **plan — not implemented.**

## Context

Eidos infers a managed resource's Create mapping **only** from a `POST` on the collection
path (`pkg/transformer/crud.go:243`). Many APIs expose *upsert* resources — `PUT` on an
item path (`PUT /alarms/{alarmId}`) with no collection `POST` — so those resources are
permanently scaffolded. In the Gigamon bundle, 3 item paths are complete `PUT+GET+DELETE`
triples with no collection POST; the toggle rescues those.

We add an **optional** config toggle, `use_put_as_create`, that makes CRUD inference fall
back to the instance-path `PUT` as the Create mapping when no POST create exists. It flows
into both the automatic detection (`eidos generate`, via generator.yaml) and the config
generation (`eidos generate-config`, via a `--use-put-as-create` flag). **Off by default**
so current behavior and golden output are unchanged.

## Semantics

When enabled and a CRUD group has no collection `POST` but its instance path has `PUT`
(plus `GET`/`DELETE`), the `PUT` becomes the resource's Create mapping (upsert). The same
`PUT` remains the Update mapping — Create and Update both issue the upsert, which is
correct. Collection `POST` still wins when present. Groups missing `GET` or `DELETE` stay
scaffolded as today.

**Critical correctness requirement:** a PUT-as-create needs the practitioner to supply the
ID in the request URI, so the identifier attribute must be **Required** (user-settable).
`ManagedResourceSchema` currently forces `id` to Computed when the create body doesn't
declare it (`resource_schema.go:195-199`) — the common case for PUT-as-create — and the
generated Create body substitutes the plan's ID into the path placeholder
(`requestPathStmts`, `resource_crud.go:690-706`), so a Computed id would emit a
`PUT /pets/` with a null value (a dishonest wired body). Forcing the identifier Required
is what makes the wired body honest.

## Changes

### 1. Config — `pkg/config/config.go`
Add a top-level field to `Config` (pattern: existing bool toggles, e.g.
`GenerationConfig.SkipTests bool` at config.go:451):
```go
UsePutAsCreate bool `yaml:"use_put_as_create,omitempty"`
```
No validation needed (independent toggle). `omitempty` means it only appears in
`generate-config` output when set.

### 2. Transformer — `pkg/transformer/crud.go`
Thread a bool into `InferResourceCRUD(pathOps, usePutAsCreate bool)`. In
`buildResourceCRUD`, after the existing POST-create assignment (crud.go:243), add the
fallback in the `if instancePath != ""` block (after line 253):
```go
if resource.Create == nil && usePutAsCreate {
    if ops, ok := pathOps[instancePath]; ok {
        if put := cloneOp(ops, MethodPut); put != nil {
            resource.Create = put // PUT /alarms/{alarmId} is the upsert create
        }
    }
}
```
Keep `resource.Update` from `chooseUpdateOps` unchanged (PUT stays Update). `cloneOp`
already exists (crud.go:359).

**Call sites to update (6 total, per exploration):**
- Production: `pkg/api/handler.go:666` (list-resource promotion) and `:1426`
  (`buildGroupedResources`).
- Tests: `crud_test.go:233,309,342` and `datasource_test.go:574`.
All pass `false` except the two production sites, which get the config value.

### 3. Schema — `pkg/transformer/resource_schema.go`
In `ManagedResourceSchema`, before the final sort (line 249), when
`c.Create != nil && c.Create.Method == MethodPut`, force the identifier attribute to
`Required`, `Computed=false`, `Optional=false`. The identifier is the `id` attr when the
state shape carries one (`hasID`), else the path-parameter-named attr (`idAttribute`) or
its synthetic (resource_schema.go:240-245). This runs after the id-Computed logic, so it
overrides it; no change to the existing block needed. Existing `POST`-create tests
(including `TestManagedResourceSchema_PractitionerSetID`) are unaffected.

### 4. API — `pkg/api/handler.go`
- `buildGroupedResources(spec, providerName, pathOps)` gains a `usePutAsCreate bool` param;
  call `transformer.InferResourceCRUD(pathOps, usePutAsCreate)`.
- **Fix line 1449** — the Create mapping is hard-coded POST on the collection path:
  ```go
  res.CRUDMapping.Create = operationMapping("POST", g.CollectionPath, parserOp(spec, g.CollectionPath, "POST"))
  ```
  becomes method/path-generic from the resolved Create op (safe for POST creates too —
  `g.Create.Method` is POST, `g.Create.Path` is the collection path — so existing specs
  are byte-identical):
  ```go
  m, p := string(g.Create.Method), g.Create.Path
  res.CRUDMapping.Create = operationMapping(m, p, parserOp(spec, p, m))
  res.CRUDMapping.Create.MediaType = mediaTypeOf(g.Create)
  ```
- Thread the toggle from `cfg` (available in `buildIRPreview`, handler.go ~651/666):
  `usePutAsCreate := cfg != nil && cfg.UsePutAsCreate`.

### 5. `generate-config` — `cmd/eidos/generate_config.go` + `pkg/api/handler.go`
`eidos generate-config` currently builds IR with `cfg=nil`
(`generateStarterConfig`, handler.go:2721-2748). Add a `--use-put-as-create` flag to
`generate_config.go` that:
1. passes the toggle into `GenerateStarterConfigWithName` / `buildProviderIR` so the IR
   reflects PUT-as-create resources (emitted overrides stay consistent), and
2. sets `UsePutAsCreate: true` on the returned `*config.Config` so `yaml.Marshal` emits
   `use_put_as_create: true` in the output generator.yaml (self-describing round-trip:
   feeding it back to `eidos generate --config` honors the toggle via inference).

`generator.GenerateConfig` (`pkg/generator/config_generator.go:26`) needs no new keys — the
field round-trips through the existing `MarshalConfig` path.

### 6. Acceptance-test generator — `pkg/generator/acceptance_tests.go`
The mock server has real POST assumptions for a create (`mockRoutes` classifies
`POST → create`, `PUT/PATCH → update` at :732-740; the create dispatch hard-codes
`http.MethodPost` at :1181). For a PUT-as-create the create step only passes because the
update case happens to catch `MethodPut`. Make the mock honest:
- Make `addRoute` role-aware (pass a role: create/read/update/delete instead of
  classifying by method), so a PUT create sets `route.create`/`createStatus`.
- Store the create method on `mockRoute` and dispatch the create case with it instead of
  hard-coded `http.MethodPost`.

### 7. Generator resource body — no changes expected
`planResourceWiring`/`planOperation` are method-agnostic: `methodHasBody("PUT")` is true,
`httpMethodExpr("PUT")` → `http.MethodPut`, and success codes come from the mapping
(`defaultSuccessCodes("PUT")` → `[200]`, and `firstSuccessCode` returns `codes[0]`, so the
mock status matches the generated body's accepted codes). The Create body
(`wiredCreateBody`, resource_crud.go:1229-1256) reads `plan.create` and already emits a
valid PUT upsert once the identifier is user-settable (change #3). Verify with a test, but
expect no source change.

### 8. Docs — `docs/usage.md`
Document `use_put_as_create` in the generator.yaml reference: what it does (falls back to
the instance-path PUT as Create when no POST create exists), that it requires the
practitioner to supply the ID, and that it's off by default.

## Tests
- **Transformer** (`crud_test.go`): a `/pets/{petId}` pathOps map with `GET/PUT/DELETE`
  and no collection POST → with `usePutAsCreate=false`, `Create == nil` (existing
  behavior); with `true`, `Create.Method == PUT` and `Create.Path == "/pets/{petId}"`.
  Keep all existing cases passing with `false`.
- **Schema** (`resource_schema_test.go`): PUT-as-create → the identifier attribute is
  `Required`, `Computed=false`.
- **Handler** (`handler_test.go`): `BuildProviderIRWithName` on a small spec with a
  PUT+GET+DELETE resource + config `{UsePutAsCreate: true}` → the resource's
  `CRUDMapping.Create.Method == "PUT"`, `PathTemplate` is the instance path, and it
  survives the wiring gates (wired, not scaffolded).
- **Generator** (`resource_crud_test.go`): render a resource with
  `CRUDMapping.Create = {Method: "PUT", PathTemplate: "/pets/{petId}"}` and a Required id
  attribute → assert the Create body emits `http.MethodPut` to the filled path.
- **Acceptance tests** (`acceptance_tests_test.go` or the existing mock tests): a PUT
  create route sets `route.create` and dispatches on `MethodPut`.
- **Config** (`config_test.go`): `use_put_as_create` parses and round-trips through
  `generate-config`.
- **Golden**: no churn expected (toggle defaults off; golden runs with no config).

## Verification
1. `go build ./... && go vet ./... && go test ./...` — all pass with the toggle off.
2. Regenerate the Gigamon bundle with `use_put_as_create: true` in the generator config:
   the 3 candidate resources (`/portConfig/gigastreams/advHash/{slotId}`,
   `/notification/event/notifMetaConfig/{notifType}/{taskId}`,
   `/config/insights/nodes/{nodeId}`) generate wired Create bodies (`http.MethodPut`),
   the generated provider compiles + `go vet` cleanly, and the wiring count rises above 52.
3. `eidos generate-config --spec bundled.yaml --use-put-as-create` → output carries
   `use_put_as_create: true` and reflects the PUT-as-create resources.
