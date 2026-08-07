package ir

// PlanModifierIR captures metadata about a Terraform-plugin-framework plan
// modifier that should be attached to an attribute. It models two roles:
//
//   - lifecycle modifiers such as RequiresReplace (force-new attributes);
//   - typed static-default modifiers such as stringdefault.StaticString /
//     int64default.StaticInt64, used to carry attribute default values.
//
// The Type field names the plan modifier constructor (e.g.
// "stringplanmodifier.UseStateForUnknown" or "stringdefault.StaticString").
//
// Args holds literal argument values for the constructor. It is populated by
// the transformer for default-value modifiers (e.g. the default string/int64)
// and round-trips through JSON, but the generator does not currently emit Args
// into the generated provider — PlanModifierIR metadata is not yet mapped to
// typed plan-modifier expressions in the generator (L-61 corrects the prior
// doc, which described a code-generation path no generator implements). The
// field is retained so the IR stays forward-compatible with that mapping.
type PlanModifierIR struct {
	Type string   `json:"type,omitempty"`
	Args []string `json:"args,omitempty"`
}

// PlanModifierType* constants are the canonical fully-qualified plugin-framework
// constructor strings stored in PlanModifierIR.Type. Both the transformer
// (producer) and the generator (consumer) reference these constants so a
// spelling drift between the two packages becomes a compile error rather than
// a silent contract bug where inferred plan modifiers never match their
// consumer (H-15).
const (
	// PlanModifierTypeRequiresReplace is the generic RequiresReplace plan
	// modifier constructor, emitted for forceNew / x-terraform-force-new
	// attributes and consumed by the generator to derive force_new entries.
	PlanModifierTypeRequiresReplace = "planmodifier.RequiresReplace"
)
