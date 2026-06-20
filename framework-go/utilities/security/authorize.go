package security

import "context"

// Action is the verb of an authorization request. It is an aiarch value type,
// never a policy-engine entity.
type Action struct {
	Verb string // e.g. "drive-phase", "approve-artifact", "query-cost"
}

// ResourceRef names the resource an [Action] targets. It is an aiarch value
// type, never a policy-engine entity. Organization scopes the resource to a
// tenancy; an empty Organization means the resource is not organization-scoped.
type ResourceRef struct {
	Kind         string // e.g. "project", "artifact", "operated-system"
	ID           string // opaque resource id
	Organization string // tenancy/organization id or name the resource belongs to
}

// Decision is the allow/deny outcome of [Security.Authorize] with a NON-LEAKING
// reason code (no policy internals — safe to surface toward the edge).
type Decision struct {
	Permit bool
	Reason DecisionReason
}

// DecisionReason is an opaque enum safe to return to the caller; it leaks no
// policy detail.
type DecisionReason int

// Decision reasons. ReasonNotPermitted is a single generic deny that
// deliberately does NOT say WHY (no information leakage); the rich "why" lives
// only in the audit record.
const (
	ReasonPermitted DecisionReason = iota
	ReasonNotPermitted
)

// String renders a DecisionReason for audit detail only (never returned to the
// caller as text).
func (r DecisionReason) String() string {
	switch r {
	case ReasonPermitted:
		return "Permitted"
	default:
		return "NotPermitted"
	}
}

// PolicyDecisionPoint is the authorization seam: the hidden engine that answers
// "may this principal take this action on this resource?". The default
// implementation shipped here is a deny-by-default rules evaluator; a heavy or
// richer engine (e.g. a policy-language PDP) is supplied from a satellite via
// [WithPolicyDecisionPoint] without changing [Security.Authorize]'s surface.
//
// A non-nil error means the engine was UNREACHABLE (transient) —
// [Security.Authorize] surfaces [ErrPolicyUnavailable] and the caller fails
// closed. A reachable engine that denies returns (false, nil).
type PolicyDecisionPoint interface {
	Decide(ctx context.Context, principal SecurityPrincipal, action Action, resource ResourceRef) (permit bool, err error)
}

// defaultPolicyDecisionPoint is a deny-by-default rules evaluator — a REAL,
// deterministic decision function (not a permit-everything stub) so fail-closed
// and allow/deny behaviour is exercised end-to-end. Rules:
//   - a malformed request (no verb or no resource kind) is denied;
//   - a resource scoped to an organization the principal is not a member of is
//     denied outright (tenancy isolation);
//   - an application principal may take its actions;
//   - otherwise the principal must hold a role naming the action verb.
//
// Production replaces this whole seam with a richer PDP via
// [WithPolicyDecisionPoint].
type defaultPolicyDecisionPoint struct{}

func (defaultPolicyDecisionPoint) Decide(_ context.Context, principal SecurityPrincipal, action Action, resource ResourceRef) (bool, error) {
	if action.Verb == "" || resource.Kind == "" {
		return false, nil // malformed → deny (not an engine outage)
	}
	// Organization-scoped resource: the principal must be a member.
	if resource.Organization != "" && !principal.IsMemberOf(resource.Organization) {
		return false, nil
	}
	// An application principal may take its actions.
	if principal.Kind == PrincipalApplication {
		return true, nil
	}
	// Otherwise the principal must hold a role naming the action verb.
	if principal.HasRole(action.Verb) {
		return true, nil
	}
	return false, nil
}

// AuditSink records an authorization decision for audit. The default is a no-op;
// production wires a sibling logging/diagnostics Utility via [WithAuditSink] so
// the rich "why" reaches the audit log. It is fire-and-forget on the caller path
// and must not block. The rich "why" goes HERE, never into the caller-facing
// [DecisionReason].
type AuditSink interface {
	RecordDecision(ctx context.Context, principal SecurityPrincipal, action Action, resource ResourceRef, permit bool, detail string)
}

type noopAuditSink struct{}

func (noopAuditSink) RecordDecision(context.Context, SecurityPrincipal, Action, ResourceRef, bool, string) {
}
