package generator

import (
	"fmt"

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
// generated Update keeps its honest scaffold, so in-place changes fail and the
// resource must be replaced instead.
const docsUpdateNotWiredNote = "~> **Note:** The update operation for this resource is not wired to a remote API endpoint: the API spec exposes no usable update mapping. Changing any configuration fails at apply time with an explicit \"not wired\" diagnostic. To change this resource, replace it (for example `terraform apply -replace=...`); create, read, and delete remain functional."

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
// versions), plus the not-wired note when its Invoke keeps the honest scaffold.
func actionDocsNotes(a ir.ActionIR) []string {
	notes := []string{docsActionTFVersionNote}
	if !planActionWiring(a).wired {
		notes = append(notes, docsNotWired("action"))
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
// exist in older CLI versions).
func listResourceDocsNotes() []string {
	return []string{docsListResourceTFVersionNote}
}

// functionDocsNotes returns the admonitions to render on a function's doc page.
// Provider-defined functions are never wired to remote API endpoints by eidos
// (F4), so every function page carries the not-wired note.
func functionDocsNotes() []string {
	return []string{docsNotWired("function")}
}
