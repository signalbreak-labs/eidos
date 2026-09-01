package generator

import (
	"fmt"
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// Doc-page admonitions. The honest-body invariant (AGENTS.md) requires unwired
// constructs to fail loud at runtime; these notes extend it to the generated
// documentation so an unwired construct never looks fully functional on paper
// (the gigavuecore audit's grep for "not wired" under docs/ returned zero hits
// while 87 scaffolded bodies shipped, §3.4).

// docsNotWired is the admonition for a construct whose generated body is an
// honest scaffold: it fails with the "not wired to a remote API endpoint"
// diagnostic instead of calling the API.
func docsNotWired(kind string) string {
	return fmt.Sprintf("~> **Note:** This %s is not yet wired to a remote API endpoint. Invoking it fails with an explicit \"not wired\" diagnostic instead of calling the API. The OpenAPI operations it was inferred from could not be resolved into a complete mapping; consult the eidos generation warnings for the exact cause.", kind)
}

// docsUpdateNotWiredNote is the admonition for a resource whose CRUD bodies are
// wired but whose Update is not (the API exposes no usable update mapping). The
// generated Update keeps its honest scaffold, but every config-settable
// attribute carries a RequiresReplace plan modifier, so any configuration
// change proposes a replacement (which the wired Create/Delete pair can
// execute) instead of an in-place update that would fail at apply.
const docsUpdateNotWiredNote = "~> **Note:** The update operation for this resource is not wired to a remote API endpoint: the API spec exposes no usable update mapping. Changing any configuration attribute triggers a resource replacement (all config-settable attributes carry a RequiresReplace plan modifier); create, read, and delete remain functional."

// docsActionTFVersionNote is the admonition for actions, which require
// Terraform 1.14 or later and are invoked via -invoke or a lifecycle trigger,
// not a plain apply.
const docsActionTFVersionNote = "-> **Note:** This action requires Terraform 1.14 or later. Standalone actions are invoked with `terraform apply -invoke=action.<type>.<name>` (or attached to a resource lifecycle `action_trigger`); a plain `terraform apply` does not invoke a standalone action block."

// docsListResourceTFVersionNote is the admonition for list resources, which
// are queried through the `terraform query` command introduced in Terraform
// 1.14.
const docsListResourceTFVersionNote = "-> **Note:** This list resource requires Terraform 1.14 or later and is used through the `terraform query` command, not in configuration files."

// resourceDocsNotes returns the admonitions to render on a managed resource's
// doc page: the not-wired note for a fully scaffolded resource, or the
// update-not-wired note for a wired resource whose Update keeps its scaffold.
// The notes are computed with the same wiring predicate the resource bodies use
// (planResourceWiring), so docs and code cannot disagree.
func resourceDocsNotes(r ir.ResourceIR) []string {
	plan := planResourceWiring(r)
	if !plan.wired {
		return []string{docsNotWired("resource")}
	}
	if !plan.update {
		return []string{docsUpdateNotWiredNote}
	}
	return nil
}

// dataSourceDocsNotes returns the admonitions to render on a data source's doc
// page: the not-wired note when its Read keeps the honest scaffold.
func dataSourceDocsNotes(ds ir.DataSourceIR) []string {
	if !planDataSourceWiring(ds).wired {
		return []string{docsNotWired("data source")}
	}
	return nil
}

// actionDocsNotes returns the admonitions to render on an action's doc page:
// the Terraform 1.14 requirement note (actions do not exist in older CLI
// versions), the not-wired note when its Invoke keeps the honest scaffold, and
// the unmarkable-secret note when a secret-named attribute cannot be marked
// Sensitive in the action schema.
func actionDocsNotes(a ir.ActionIR) []string {
	notes := []string{docsActionTFVersionNote}
	if !planActionWiring(a).wired {
		notes = append(notes, docsNotWired("action"))
	}
	if secret := docsUnmarkableSecretNote("action", a.UnmarkableSensitiveAttrs); secret != "" {
		notes = append(notes, secret)
	}
	return notes
}

// ephemeralResourceDocsNotes returns the admonitions to render on an ephemeral
// resource's doc page: the not-wired note when its Open keeps the honest
// scaffold.
func ephemeralResourceDocsNotes(er ir.EphemeralResourceIR) []string {
	if !planEphemeralWiring(er).wired {
		return []string{docsNotWired("ephemeral resource")}
	}
	return nil
}

// listResourceDocsNotes returns the admonitions to render on a list resource's
// doc page: the Terraform 1.14 requirement note (`terraform query` does not
// exist in older CLI versions), plus the unmarkable-secret note when a
// secret-named attribute cannot be marked Sensitive in the list schema.
func listResourceDocsNotes(lr ir.ListResourceIR) []string {
	notes := []string{docsListResourceTFVersionNote}
	if secret := docsUnmarkableSecretNote("list resource", lr.UnmarkableSensitiveAttrs); secret != "" {
		notes = append(notes, secret)
	}
	return notes
}

// docsUnmarkableSecretNote renders the admonition for secret-named attributes
// that the schema kind cannot mark Sensitive: the plugin-framework
// action/schema and experimental list/schema packages have no Sensitive
// support, so the values surface in plan, state, and `terraform query` output.
// attrs holds the wire names recorded at transform time; an empty list renders
// no note.
func docsUnmarkableSecretNote(kind string, attrs []string) string {
	if len(attrs) == 0 {
		return ""
	}
	return fmt.Sprintf("~> **Warning:** This %s accepts attributes whose names indicate secrets (%s), but %s schemas cannot mark attributes Sensitive, so their values are stored and displayed in plain text. Avoid passing real secrets through these attributes.", kind, strings.Join(attrs, ", "), kind)
}

// functionDocsNotes returns the admonitions to render on a function's doc page.
// Provider-defined functions are never wired to remote API endpoints by eidos
// (F4), so every function page carries the not-wired note.
func functionDocsNotes() []string {
	return []string{docsNotWired("function")}
}
