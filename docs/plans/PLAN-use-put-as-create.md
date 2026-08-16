# PUT-as-create inference (default-on)

Status: **plan — revised: default-on.** Supersedes the earlier "optional toggle,
off by default" draft. This version makes PUT-as-create automatic for generated
providers, surfaces it with a diagnostic, and keeps a kill-switch + per-resource
opt-out.

## Context

Eidos infers a managed resource's Create mapping **only** from a `POST` on the
collection path (`pkg/transformer/crud.go:243`). Many APIs expose *upsert*
resources — `PUT` on an item path (`PUT /alarms/{alarmId}`) with no collection
`POST` — so those resources are permanently scaffolded. In the Gigamon bundle, 3
item paths are complete `PUT+GET+DELETE` triples with no collection POST.

The earlier draft proposed an **opt-in** `use_put_as_create` toggle, off by
default. That under-serves the auto-generated case: a user running
`eidos generate` with no config gets a permanently-scaffolded resource for an API
that is perfectly serviceable as a Terraform upsert. Since eidos is an
auto-generator, the better default is to **use PUT-as-create automatically** when
the CRUD group has no collection POST but has instance `PUT+GET+DELETE`, surface
it with a diagnostic so the user knows what happened, and offer a kill-switch +
per-resource opt-out for the cases where the upsert semantics are wrong.

## Recommendation (summary)

1. **On by default.** PUT-as-create fires automatically when a CRUD group has no
   collection `POST` but its instance path has `PUT` (plus `GET`/`DELETE`).
2. **Fail loud.** Emit a `diagnostics.Info` when it fires, naming the resource
   and the PUT path (e.g. "using PUT /alarms/{alarmId} as Create (upsert) because
   no collection POST exists"). This keeps with the repo's "fail loud, never
   silently" rule — an inferred Create is a load-bearing assumption the user
   should see.
3. **Per-resource opt-out is the existing `skip: true` override**
   (`pkg/config/config.go:150`, consumed at `pkg/transformer/override.go:114`),
   **not** `generate_resource: false`. `GenerateResource *bool` (config.go:152)
   is **opt-in only** — `pkg/api/handler.go:1729` reads
   `gen := ro.GenerateResource != nil && *ro.GenerateResource`, so
   `generate_resource: false` is silently ignored. The honest per-resource
   escape hatch is `skip: true`, which drops the resource entirely.
4. **`use_put_as_create` becomes a kill-switch defaulting ON.** Setting
   `use_put_as_create: false` in `generator.yaml` restores the legacy scaffold
   behavior for every resource at once. It is *not* the opt-in from the earlier
   draft — the semantics are inverted: the field's presence is a global opt-**out**.
5. **Schema change (#3) is mandatory and ships with the inference.** Without it
   the wired Create body emits a dishonest `PUT /items/` with a null id (see
   Semantics). The inference and the schema fix are one atomic change.
6. **Golden churn is expected and one-time.** Default-on changes output for
   specs with PUT+GET+DELETE-no-POST triples (3 in the Gigamon bundle). Regenerate
   with `EIDOS_UPDATE_GOLDEN=1` once, intentionally.

## Semantics

When a CRUD group has no collection `POST` but its instance path has `PUT` (plus
`GET`/`DELETE`), the `PUT` becomes the resource's Create mapping (upsert). The
same `PUT` remains the Update mapping — Create and Update both issue the upsert,
which is correct. Collection `POST` still wins when present. Groups missing
`GET` or `DELETE` stay scaffolded as today.

**Critical correctness requirement (mandatory):** a PUT-as-create needs the
practitioner to supply the ID in the request URI, so the identifier attribute
must be **Required** (user-settable). `ManagedResourceSchema` currently forces
`id` to Computed when the create body doesn't declare it
(`resource_schema.go:195-199`) — the common case for PUT-as-create — and the
generated Create body substitutes the plan's ID into the path placeholder
(`requestPathStmts`, `resource_crud.go:690-706`), so a Computed id would emit a
`PUT /pets/` with a null value (a dishonest wired body). Forcing the identifier
Required is what makes the wired body honest. This **must** land with the
inference change; it is not a follow-up.

## Changes

### 1. Config — `pkg/config/config.go`
Add a top-level field to `Config` (pattern: existing bool toggles, e.g.
`GenerationConfig.SkipTests bool` at config.go:451):
```go
UsePutAsCreate bool `yaml:"use_put_as_create,omitempty"`
```
**Semantics inverted from the earlier draft:** the field defaults to effective-ON
because the transformer applies PUT-as-create whenever the field is unset/true
*and the config doesn't opt out*. To make the default legible, the transformer
treats the absence of the field as "on" (the auto-generator's natural behavior);
setting `use_put_as_create: false` is the global kill-switch. `omitempty` means
the field only appears in `generate-config` output when the user explicitly
disabled it — so a freshly-generated config carries no `use_put_as_create` line,
and the provider behaves as default-on. (If we want the generated config to be
self-documenting about the default, `generate-config` may instead emit
`use_put_as_create: true` explicitly; see §5.)

### 2. Transformer — `pkg/transformer/crud.go`
Thread the toggle into `InferResourceCRUD`. Because the transformer layer does
not currently carry a `*diagnostics.Diagnostics`, thread it as a bool
`usePutAsCreate` and emit the surfacing diagnostic at the **handler** layer
(§4), where `previewDiags` is already available (~handler.go:666). In
`buildResourceCRUD`, after the existing POST-create assignment (crud.go:243),
add the fallback in the `if instancePath != ""` block (after line 253):
```go
if resource.Create == nil && usePutAsCreate {
    if ops, ok := pathOps[instancePath]; ok {
        if put := cloneOp(ops, MethodPut); put != nil {
            resource.Create = put // PUT /alarms/{alarmId} is the upsert create
        }
    }
}
```
Keep `resource.Update` from `chooseUpdateOps` unchanged (PUT stays Update).
`cloneOp` already exists (crud.go:359).

**Default wiring:** the production call sites pass `true` (the new default) unless
the config sets `use_put_as_create: false`. Concretely, the handler resolves
`usePutAsCreate := cfg == nil || cfg.UsePutAsCreate` — `cfg == nil` (no config,
pure auto-generation) is default-on, matching the auto-generator stance.

**Call sites to update (6 total, per exploration):**
- Production: `pkg/api/handler.go:666` (list-resource promotion) and `:1426`
  (`buildGroupedResources`).
- Tests: `crud_test.go:233,309,342` and `datasource_test.go:574`.
The production sites pass the resolved `usePutAsCreate` (default true); the test
sites pass `false` to preserve existing assertion shapes, plus a new `true` case
(see Tests).

### 3. Schema — `pkg/transformer/resource_schema.go` (mandatory, atomic with §2)
In `ManagedResourceSchema`, before the final sort (line 249), when
`c.Create != nil && c.Create.Method == MethodPut`, force the identifier attribute
to `Required`, `Computed=false`, `Optional=false`. The identifier is the `id` attr
when the state shape carries one (`hasID`), else the path-parameter-named attr
(`idAttribute`) or its synthetic (resource_schema.go:240-245). This runs after the
id-Computed logic, so it overrides it; no change to the existing block needed.
Existing `POST`-create tests (including
`TestManagedResourceSchema_PractitionerSetID`) are unaffected. **This change is
gated on `Create.Method == MethodPut`, so it only affects PUT-as-create resources;
POST-create resources are byte-identical.**

### 4. API — `pkg/api/handler.go`
- `buildGroupedResources(spec, providerName, pathOps)` gains a `usePutAsCreate
  bool` param; call `transformer.InferResourceCRUD(pathOps, usePutAsCreate)`.
- **Emit the surfacing diagnostic here**, where `previewDiags` is available
  (~handler.go:666): when a group's Create was resolved from a PUT (no collection
  POST existed), append
  `diagnostics.Info{Summary: "using PUT <path> as Create (upsert)", Detail:
  "resource <name>: no collection POST exists; the instance-path PUT is used as
  the Create (upsert) mapping. Set use_put_as_create: false to disable, or skip:
  true on this resource to drop it."}`. Use `diagnostics.Info` (severity exists at
  `pkg/diagnostics/diagnostics.go:28`) — this is an informative inference, not a
  warning or error. The diagnostic must be emitted for both the preview
  (`buildIRPreview`) and the full `BuildProviderIR` paths so `eidos generate` and
  `eidos generate-config` both surface it.
- **Fix line 1449** — the Create mapping is hard-coded POST on the collection
  path:
  ```go
  res.CRUDMapping.Create = operationMapping("POST", g.CollectionPath, parserOp(spec, g.CollectionPath, "POST"))
  ```
  becomes method/path-generic from the resolved Create op (safe for POST creates
  too — `g.Create.Method` is POST, `g.Create.Path` is the collection path — so
  existing specs are byte-identical):
  ```go
  m, p := string(g.Create.Method), g.Create.Path
  res.CRUDMapping.Create = operationMapping(m, p, parserOp(spec, p, m))
  res.CRUDMapping.Create.MediaType = mediaTypeOf(g.Create)
  ```
- Thread the toggle from `cfg`: `usePutAsCreate := cfg == nil || cfg.UsePutAsCreate`
  (default-on when no config is supplied).

### 5. `generate-config` — `cmd/eidos/generate_config.go` + `pkg/api/handler.go`
`eidos generate-config` currently builds IR with `cfg=nil`
(`generateStarterConfig`, handler.go:2721-2748). Add a `--use-put-as-create` flag
to `generate_config.go` that:
1. passes the toggle into `GenerateStarterConfigWithName` / `buildProviderIR` so
   the IR reflects PUT-as-create resources (emitted overrides stay consistent),
   and
2. sets `UsePutAsCreate: true` on the returned `*config.Config` so `yaml.Marshal`
   emits `use_put_as_create: true` in the output generator.yaml.

Add a `--no-use-put-as-create` companion (or let the flag be a tri-state) so a
user can generate a config that explicitly records the kill-switch
(`use_put_as_create: false`). Default `generate-config` (no flag) emits
`use_put_as_create: true` so the generated config is self-documenting about the
default-on behavior and round-trips: feeding it back to `eidos generate --config`
honors the toggle via inference.

`generator.GenerateConfig` (`pkg/generator/config_generator.go:26`) needs no new
keys — the field round-trips through the existing `MarshalConfig` path.

### 6. Acceptance-test generator — `pkg/generator/acceptance_tests.go`
The mock server has real POST assumptions for a create (`mockRoutes` classifies
`POST → create`, `PUT/PATCH → update` at :732-740; the create dispatch hard-codes
`http.MethodPost` at :1181). For a PUT-as-create the create step only passes
because the update case happens to catch `MethodPut`. Make the mock honest:
- Make `addRoute` role-aware (pass a role: create/read/update/delete instead of
  classifying by method), so a PUT create sets `route.create`/`createStatus`.
- Store the create method on `mockRoute` and dispatch the create case with it
  instead of hard-coded `http.MethodPost`.

### 7. Generator resource body — no changes expected
`planResourceWiring`/`planOperation` are method-agnostic: `methodHasBody("PUT")`
is true, `httpMethodExpr("PUT")` → `http.MethodPut`, and success codes come from
the mapping (`defaultSuccessCodes("PUT")` → `[200]`, and `firstSuccessCode`
returns `codes[0]`, so the mock status matches the generated body's accepted
codes). The Create body (`wiredCreateBody`, resource_crud.go:1229-1256) reads
`plan.create` and already emits a valid PUT upsert once the identifier is
user-settable (change #3). Verify with a test, but expect no source change.

### 8. Docs — `docs/usage.md`
Document `use_put_as_create` in the generator.yaml reference: what it does
(falls back to the instance-path PUT as Create when no POST create exists), that
it is **on by default** for auto-generated providers, that it requires the
practitioner to supply the ID, that an `Info` diagnostic is emitted when it
fires, and the two escape hatches: `use_put_as_create: false` (global
kill-switch) and `skip: true` on the resource (per-resource opt-out). Correct the
common misconception that `generate_resource: false` opts out — it does not
(opt-in only); use `skip: true`.

## Per-resource opt-out (correction)

The per-resource escape hatch is the **existing `skip: true`** override
(`pkg/config/config.go:150`, applied at `pkg/transformer/override.go:114`):
```yaml
resources:
  - operation: PUT /alarms/{alarmId}
    skip: true
```
This drops the resource entirely. `generate_resource: false` is **not** an
opt-out: `GenerateResource *bool` (config.go:152) is opt-in only
(handler.go:1729 acts only when the pointer is non-nil and true), so
`generate_resource: false` is silently ignored. Any plan text that suggested
`generate_resource: false` as the opt-out was wrong; this is the corrected
guidance.

## Tests
- **Transformer** (`crud_test.go`): a `/pets/{petId}` pathOps map with
  `GET/PUT/DELETE` and no collection POST → with `usePutAsCreate=false`,
  `Create == nil` (existing behavior); with `true`, `Create.Method == PUT` and
  `Create.Path == "/pets/{petId}"`. Keep all existing cases passing with `false`.
- **Schema** (`resource_schema_test.go`): PUT-as-create → the identifier attribute
  is `Required`, `Computed=false`.
- **Handler** (`handler_test.go`): `BuildProviderIRWithName` on a small spec with
  a PUT+GET+DELETE resource + no config (default-on) → the resource's
  `CRUDMapping.Create.Method == "PUT"`, `PathTemplate` is the instance path, it
  survives the wiring gates (wired, not scaffolded), and an `Info` diagnostic
  naming the PUT path is present. With `cfg.UsePutAsCreate = false` → Create is
  nil and the resource is scaffolded (legacy behavior), no Info diagnostic.
- **Generator** (`resource_crud_test.go`): render a resource with
  `CRUDMapping.Create = {Method: "PUT", PathTemplate: "/pets/{petId}"}` and a
  Required id attribute → assert the Create body emits `http.MethodPut` to the
  filled path.
- **Acceptance tests** (`acceptance_tests_test.go` or the existing mock tests): a
  PUT create route sets `route.create` and dispatches on `MethodPut`.
- **Config** (`config_test.go`): `use_put_as_create` parses and round-trips
  through `generate-config`; `generate_resource: false` is confirmed to be a
  no-op (regression guard for the opt-out correction).
- **Golden**: **one-time churn expected.** Default-on changes output for specs
  with PUT+GET+DELETE-no-POST triples. Regenerate with
  `EIDOS_UPDATE_GOLDEN=1 go test -run TestGoldenFiles ./pkg/generator` once,
  intentionally. Manifest diff: the affected resources move from scaffolded to
  wired (Create method PUT, Required id). No new files; existing golden JSON
  bodies change for those resources only.

## Verification
1. `go build ./... && go vet ./... && go test ./...` — all pass with the default
   (on). Existing POST-create specs are byte-identical (the §3 schema change is
   gated on `Create.Method == MethodPut`).
2. Regenerate the Gigamon bundle (no config, default-on): the 3 candidate
   resources (`/portConfig/gigastreams/advHash/{slotId}`,
   `/notification/event/notifMetaConfig/{notifType}/{taskId}`,
   `/config/insights/nodes/{nodeId}`) generate wired Create bodies
   (`http.MethodPut`) with Required identifiers, the generated provider compiles
   + `go vet` cleanly, the wiring count rises above 52, and each emits an `Info`
   diagnostic. `eidos generate --dry-run` on the bundle prints the 3 Info
   diagnostics.
3. Set `use_put_as_create: false` in a generator config and regenerate the
   bundle: the 3 resources revert to scaffolded (legacy), no Info diagnostics.
4. Set `skip: true` on one of the 3 resources (config otherwise default-on): that
   resource is dropped; the other 2 are still wired. Confirm
   `generate_resource: false` on the same resource does **not** drop it
   (regression guard).
5. `eidos generate-config --spec bundled.yaml` → output carries
   `use_put_as_create: true` and reflects the PUT-as-create resources;
   `--no-use-put-as-create` → output carries `use_put_as_create: false`.