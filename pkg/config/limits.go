package config

// LimitsConfig caps generated output sizes against Terraform platform limits.
// Generation of a provider whose estimated serialized schema exceeds the
// schema cap fails loud; docs files over the docs cap fail at write time (the
// Terraform Registry otherwise silently truncates them); descriptions over
// the description cap are truncated with an ellipsis. Zero values mean "use
// the built-in default"; a negative value disables that check entirely.
//
// The limits exist because the platform bounds are hard and silent: the
// Terraform CLI refuses GetProviderSchema responses over 64 MiB, and the
// Registry truncates any docs file over 500KB (G39). Without these checks a
// large spec generates a provider that fails only at `terraform init`/publish
// time, far from the cause.
type LimitsConfig struct {
	// MaxSchemaBytes is the hard cap on the estimated serialized provider
	// schema size (bytes). At or above it, generation fails with an error
	// naming the estimate and the largest constructs, so the operator can cut
	// keys or resources (e.g. via skip_operations / generation.skip_*, or by
	// trimming descriptions). A warning fires earlier at 80% of the cap.
	// Default 60 MiB (headroom under the 64 MiB client cap).
	MaxSchemaBytes int `yaml:"max_schema_size_bytes,omitempty" json:"max_schema_size_bytes,omitempty"`
	// WarnSchemaBytes is the warning threshold for the estimated serialized
	// provider schema size (bytes). Default 80% of the error cap.
	WarnSchemaBytes int `yaml:"warn_schema_size_bytes,omitempty" json:"warn_schema_size_bytes,omitempty"`
	// MaxDescriptionBytes truncates every generated attribute/block
	// description to at most this many bytes (UTF-8 boundary respected, an
	// ellipsis appended). Long descriptions are the dominant driver of both
	// schema and docs size, so this is the primary lever for fitting a large
	// spec under the caps. Negative disables truncation; zero uses no default
	// (truncation is opt-in — descriptions carry real spec content).
	MaxDescriptionBytes int `yaml:"max_description_bytes,omitempty" json:"max_description_bytes,omitempty"`
	// MaxDocsFileBytes caps each generated docs/ markdown file (bytes). At or
	// above it, write mode fails naming the file and size, because the
	// Registry truncates oversize docs with only a note, silently losing the
	// tail of the schema documentation. Default 500000 (the Registry's limit).
	MaxDocsFileBytes int `yaml:"max_docs_file_bytes,omitempty" json:"max_docs_file_bytes,omitempty"`
}
