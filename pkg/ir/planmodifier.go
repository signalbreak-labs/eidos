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
// The generator maps Type entries to typed plan-modifier expressions:
// PlanModifierTypeRequiresReplace resolves to the attribute kind's typed
// RequiresReplace() constructor, and "<typedpkg>.UseStateForUnknown" emits the
// named constructor (H-15's silent contract gap). Args holds literal argument
// values for constructors that take them (the static-default modifiers) and
// round-trips through JSON, but the generator has no emission path for
// argument-bearing modifiers and fails loud (a render error) rather than
// emitting a modifier whose arguments were dropped. The field is retained so
// the IR stays forward-compatible with that mapping.
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
