// Package transformer maps normalized OpenAPI schemas to Terraform Plugin
// Framework representations used by the Eidos provider generator.
//
// # Pipeline role
//
// The transformer is the second stage of the generation pipeline
// (cmd/eidos → parser → transformer → ir → generator). It consumes the
// parser's version-agnostic model, normalizes it (allOf flattening,
// polymorphism, parameter/security/server composition, naming), infers
// resources, data sources, list resources, actions, ephemeral resources, and
// functions, maps OpenAPI types to Terraform Plugin Framework types, applies
// generator.yaml overrides, and produces the ProviderIR consumed by the
// generator.
//
// # Live exported API (production callers)
//
// The production generation path (pkg/api/handler.go, pkg/generator) calls
// these exported symbols:
//
//   - OperationsFromSpecWithDiagnostics — operation extraction from a spec.
//   - InferResourceCRUD / InferListResources — CRUD and list-resource inference.
//   - ManagedResourceSchema / DataSourceSchema / ListResourceConfigSchema —
//     schema construction for inferred constructs.
//   - ObjectSchemaFromSpec / ObjectSchemaFromOperation / ResultSchemaFromResponse
//     / SchemaSpec — schema conversion helpers.
//   - ApplyOverrides — applies generator.yaml overrides to the IR.
//   - ApplyWriteOnlyAttributesWithDiagnostics — write-only attribute handling.
//   - MapSecuritySchemeToProviderConfig — security scheme → provider config.
//   - ApplyDynamicUnion — discriminated-union rendering for the generator.
//   - ToSnakeCase / NormalizeOperationID / SanitizeAttributeName — naming.
//   - RequestBodyKind — request media-type selection for wired CRUD bodies.
//   - FilterSpecOperations — include/exclude operation filtering.
//   - IsLifecycleSubpath / IsCRUDCreatePath / IDComposite — classification
//     helpers shared with the api package's operation classification.
//
// # Test-only exported API
//
// The following exported symbols have no non-test, non-transformer callers
// today; only tests exercise them. They are preserved so the heuristics can be
// switched on without re-deriving the API, but every bug in this surface is
// latent until that wiring lands:
//
//   - OperationsFromSpec (the non-diagnostic variant)
//   - InferActions / InferDataSources / InferEphemeralResources / InferFunctions
//     — the api package classifies these constructs inline in addPathOperations,
//     deliberately unified with the transformer's inference rules (see the
//     comments in pkg/api/handler.go), so these entry points are not called
//     directly.
//   - FilterResources / ShouldInclude
//   - NormalizeOneOf / NormalizeAnyOf
//   - ToPascalCase
//   - SelectStrategy / SplitResources
//   - InferPlanModifiers / ApplyPlanModifiers
//   - InferValidators / ApplyValidators
//   - NormalizeOperationSecurity / NormalizeOperationServers
//
// validator_inference.go additionally duplicates the live
// keywords_advanced.go validator pipeline with a divergent output format.
// The generator renders validators from schema constraints via
// int64ValidatorExprs/float64ValidatorExprs, NOT from attr.Validators or
// TypeMapping.Validator, so the inferred validators are currently dead
// metadata on the IR.
//
// Before relying on any of the test-only symbols for runtime behavior, confirm
// the wiring has landed (grep for non-test, non-transformer callers). Deleting
// or rewiring this surface is a product decision; it is intentionally retained
// until that decision is made.
package transformer
