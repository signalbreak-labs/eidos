package transformer

import (
	"fmt"
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// ETag-based conditional requests (RFC 9110 §13.1). A managed resource whose
// update or delete operation declares an If-Match request header gets
// optimistic concurrency: the generated provider stores the ETag the API
// returns from Create/Read/Update in private state and sends it back as
// If-Match, so a write against an object changed outside Terraform fails with
// 412 Precondition Failed instead of silently overwriting it. A create
// operation declaring If-None-Match is sent "If-None-Match: *" so it cannot
// overwrite an existing object.
//
// The conditional headers are managed by the provider, never by the
// practitioner, so they are kept out of the operation's HeaderParams and out
// of the resource schema (they used to surface as an `if_match` attribute the
// practitioner had to fill with an ETag they could not know).

// HeaderIfMatch and HeaderIfNoneMatch are the canonical conditional request
// header names. Matching is case-insensitive (HTTP field names are).
const (
	HeaderIfMatch     = "If-Match"
	HeaderIfNoneMatch = "If-None-Match"
)

// IsConditionalHeader reports whether a header parameter name is one of the
// ETag conditional request headers the generated provider manages itself.
func IsConditionalHeader(name string) bool {
	n := strings.TrimSpace(name)
	return strings.EqualFold(n, HeaderIfMatch) || strings.EqualFold(n, HeaderIfNoneMatch)
}

// isConditionalHeaderParam reports whether a parameter is an in: header
// If-Match / If-None-Match parameter.
func isConditionalHeaderParam(in, name string) bool {
	return strings.EqualFold(strings.TrimSpace(in), "header") && IsConditionalHeader(name)
}

// ConditionalRequestPlan is the resolved conditional-request behavior of one
// managed resource. The generator emits code from it and the transformer
// reports diagnostics from it, so the two cannot disagree.
type ConditionalRequestPlan struct {
	// TrackETag is true when the provider stores the API's ETag in private
	// state: Create, Read, and Update capture it so Update and/or Delete can
	// send it back as If-Match.
	TrackETag bool
	// UpdateIfMatch is true when the wired Update sends If-Match.
	// UpdateIfMatchRequired is true when the spec requires it; with no stored
	// ETag (state written by an older provider version, an API that returned
	// none, or tracking disabled) a required header is sent as "*" and an
	// optional one is omitted.
	UpdateIfMatch         bool
	UpdateIfMatchRequired bool
	// DeleteIfMatch is true when the wired Delete sends If-Match: always when
	// the spec requires it, and for an optional header only when the
	// conditional_delete: always override asks for it.
	DeleteIfMatch         bool
	DeleteIfMatchRequired bool
	// CreateIfNoneMatch is true when the wired Create sends
	// "If-None-Match: *" (the create operation declares the header), so the
	// API rejects the create with 412 instead of overwriting an existing
	// object.
	CreateIfNoneMatch bool
}

// Any reports whether the resource sends any conditional request header.
func (p ConditionalRequestPlan) Any() bool {
	return p.TrackETag || p.UpdateIfMatch || p.DeleteIfMatch || p.CreateIfNoneMatch
}

// PlanConditionalRequests resolves a resource's conditional-request behavior
// from its CRUD mappings and overrides.
func PlanConditionalRequests(r ir.ResourceIR) ConditionalRequestPlan {
	var p ConditionalRequestPlan
	updDeclared := r.CRUDMapping.Update != nil && r.CRUDMapping.Update.IfMatch != ""
	updRequired := r.CRUDMapping.Update != nil && r.CRUDMapping.Update.IfMatch == ir.ConditionalHeaderRequired
	delRequired := r.CRUDMapping.Delete.IfMatch == ir.ConditionalHeaderRequired
	delWanted := delRequired || (r.CRUDMapping.Delete.IfMatch == ir.ConditionalHeaderOptional && r.ConditionalDelete == ir.ConditionalDeleteAlways)

	p.TrackETag = !r.ETagTrackingDisabled && (updDeclared || delWanted)
	// Without tracking there is no ETag to send, so only a required header
	// (sent as "*") is emitted.
	p.UpdateIfMatch = updDeclared && (p.TrackETag || updRequired)
	p.UpdateIfMatchRequired = updRequired
	p.DeleteIfMatch = delWanted && (p.TrackETag || delRequired)
	p.DeleteIfMatchRequired = delRequired
	p.CreateIfNoneMatch = r.CRUDMapping.Create.IfNoneMatch != ""
	return p
}

// ConditionalRequestDiagnostics reports, for every managed resource, how the
// spec's ETag conditional request headers are honored. A resource that tracks
// ETags gets an Info diagnostic naming what it sends; a declared conditional
// header the provider does not send is an Info (optional) or a Warning
// (required: the API may reject the request), never silently dropped.
func ConditionalRequestDiagnostics(provider *ir.ProviderIR, diags *diagnostics.Diagnostics) {
	if provider == nil || diags == nil {
		return
	}
	for _, r := range provider.Resources {
		conditionalRequestDiagnostics(r, diags)
	}
}

func conditionalRequestDiagnostics(r ir.ResourceIR, diags *diagnostics.Diagnostics) {
	p := PlanConditionalRequests(r)
	name := r.TypeName
	if name == "" {
		name = r.Name
	}

	if p.Any() {
		var sends []string
		if p.CreateIfNoneMatch {
			sends = append(sends, `Create sends "If-None-Match: *"`)
		}
		if p.UpdateIfMatch {
			sends = append(sends, "Update sends If-Match")
		}
		if p.DeleteIfMatch {
			sends = append(sends, "Delete sends If-Match")
		}
		detail := fmt.Sprintf("Resource %q uses ETag conditional requests: %s.", name, strings.Join(sends, "; "))
		if p.TrackETag {
			detail += " The ETag returned by Create, Read, and Update is stored in the resource's private state and sent back, so a write against an object changed outside Terraform fails with 412 Precondition Failed instead of overwriting it."
		} else if r.ETagTrackingDisabled {
			detail += ` ETag tracking is disabled by the etag_tracking: false override, so a required If-Match header is sent as "*".`
		}
		*diags = append(*diags, diagnostics.Diagnostic{
			Severity: diagnostics.Info,
			Summary:  "ETag conditional requests enabled",
			Detail:   detail,
		})
	}

	type slot struct {
		op     string
		header string
		value  string
		sent   bool
		hint   string
	}
	var slots []slot
	slots = append(slots,
		slot{"read", HeaderIfMatch, r.CRUDMapping.Read.IfMatch, false, "Read requests are never conditional."},
		slot{"read", HeaderIfNoneMatch, r.CRUDMapping.Read.IfNoneMatch, false, "Read requests are never conditional; a 304 Not Modified response carries no body to refresh state from."},
		slot{"create", HeaderIfMatch, r.CRUDMapping.Create.IfMatch, false, "A resource being created has no ETag to match."},
		slot{"create", HeaderIfNoneMatch, r.CRUDMapping.Create.IfNoneMatch, p.CreateIfNoneMatch, ""},
		slot{"delete", HeaderIfNoneMatch, r.CRUDMapping.Delete.IfNoneMatch, false, "If-None-Match has no meaning for a delete of an existing object."},
		slot{"delete", HeaderIfMatch, r.CRUDMapping.Delete.IfMatch, p.DeleteIfMatch, deleteIfMatchHint(r)},
	)
	if upd := r.CRUDMapping.Update; upd != nil {
		slots = append(slots,
			slot{"update", HeaderIfMatch, upd.IfMatch, p.UpdateIfMatch, "ETag tracking is disabled by the etag_tracking: false override, so there is no ETag to send."},
			slot{"update", HeaderIfNoneMatch, upd.IfNoneMatch, false, "If-None-Match has no meaning for an update of an existing object."},
		)
	}
	for _, s := range slots {
		if s.value == "" || s.sent {
			continue
		}
		sev := diagnostics.Info
		if s.value == ir.ConditionalHeaderRequired {
			sev = diagnostics.Warning
		}
		*diags = append(*diags, diagnostics.Diagnostic{
			Severity: sev,
			Summary:  "conditional request header not sent",
			Detail: fmt.Sprintf(
				"The %s operation of resource %q declares the %s %s request header, which the generated provider does not send. %s",
				s.op, name, s.value, s.header, s.hint,
			),
		})
	}
}

// deleteIfMatchHint explains why a declared DELETE If-Match header is not sent.
func deleteIfMatchHint(r ir.ResourceIR) string {
	if r.CRUDMapping.Delete.IfMatch == ir.ConditionalHeaderOptional {
		if r.ConditionalDelete == ir.ConditionalDeleteAlways && r.ETagTrackingDisabled {
			return "ETag tracking is disabled by the etag_tracking: false override, so there is no ETag to send."
		}
		return "Optional conditional deletes are off by default so a stale ETag cannot block terraform destroy; set conditional_delete: always on the resource override to send it."
	}
	return ""
}
