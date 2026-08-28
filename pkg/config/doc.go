// Package config loads, validates, and emits generator.yaml configuration.
//
// Use Load or LoadBytes to parse a generator.yaml file, ApplyDefaults to fill
// in optional defaults, and Validate to check structural and value constraints.
// Use WriteStarterGeneratorConfigBytes to write serialized generator.yaml bytes
// atomically; the starter configuration content itself is produced by the
// discovery pipeline (pkg/api.GenerateStarterConfigWithName), not by this
// package.
package config
