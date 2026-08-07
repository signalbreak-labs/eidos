// Package config loads, validates, and emits generator.yaml configuration.
//
// Use Load or LoadBytes to parse a generator.yaml file, ApplyDefaults to fill
// in optional defaults, and Validate to check structural and value constraints.
// Use WriteStarterGeneratorConfig or WriteStarterGeneratorConfigBytes to emit
// a starter configuration derived from an OpenAPI spec.
package config
