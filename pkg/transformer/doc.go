// Package transformer maps normalized OpenAPI schemas to Terraform Plugin
// Framework representations used by the Eidos provider generator.
//
// # Pipeline status — live vs. unwired API (REVIEW M-42, M-51)
//
// The transformer exposes a large exported surface, but only a small subset is
// reached by the production generation path (cmd/eidos → parser → transformer →
// ir → generator). The remainder is exercised solely by tests. This is
// intentional scaffolding for auto-detection heuristics that are not yet wired
// into the generator (see PROJECT_DESIGN.md §8.7–§8.10 and the "Important
// current limitations" section of CLAUDE.md). It is preserved so the heuristics
// can be switched on without re-deriving the API, but every bug in the unwired
// surface is latent until that wiring lands.
//
// # Live exported API (production callers)
//
// Only these exported symbols have non-test, non-transformer callers today:
//
//   - ToSnakeCase                 — naming normalization used by the generator.
//   - NormalizeOperationID        — operation-id cleanup.
//   - ApplyOverrides              — applies generator.yaml overrides to the IR.
//   - ApplyWriteOnlyAttributes    — write-only attribute handling.
//
// # Unwired inference pipeline (M-42)
//
// The following have zero production callers — only tests exercise them:
//
//   - OperationsFromSpec
//   - InferResourceCRUD
//   - InferActions
//   - InferDataSources
//   - InferListResources
//   - InferEphemeralResources
//   - FilterResources / ShouldInclude
//   - NormalizeOneOf / NormalizeAnyOf
//   - ToPascalCase
//
// ~2,300 lines of exported API sit behind these entry points. Bugs fixed in
// this code (e.g. M-49's real stringvalidator.RegexMatches constructors) are
// latent guarantees: they are correct-if-wired, not active behavior.
//
// # Unwired second-half API (M-51)
//
// The following also have zero non-test callers (verified by grep):
//
//   - MapSecuritySchemeToProviderConfig
//   - SelectStrategy / ApplyDynamicUnion / SplitResources
//   - InferPlanModifiers / ApplyPlanModifiers
//   - InferValidators / ApplyValidators
//   - NormalizeOperationSecurity
//   - NormalizeOperationServers
//
// validator_inference.go additionally duplicates the live
// keywords_advanced.go validator pipeline with a divergent output format.
// The generator renders validators from schema constraints via
// int64ValidatorExprs/float64ValidatorExprs, NOT from attr.Validators or
// TypeMapping.Validator, so the inferred validators are currently dead
// metadata on the IR.
//
// Before relying on any of the above for runtime behavior, confirm the wiring
// has landed (grep for non-test, non-transformer callers). Deleting or rewiring
// this surface is a product decision; it is intentionally retained until that
// decision is made.
package transformer
