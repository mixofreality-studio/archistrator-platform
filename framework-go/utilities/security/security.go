package security

import (
	"context"
	"errors"
)

// Security is the in-process authorization-and-service-infrastructure facet —
// the half of the security Utility that runs synchronously inside a request,
// after a [Validator] (via [Middleware]) has already established the principal.
// Three ops, each behind a mechanism-neutral seam:
//
//   - Authorize             — the policy-decision point.
//   - VerifyWebhookSignature — verify a signed inbound over its EXACT raw bytes.
//   - ObtainServiceIdentity — mint the platform's own short-lived credential for
//     an outbound call.
type Security interface {
	// Authorize answers "may this principal take this action on this resource?".
	// It returns a Permit/Deny [Decision] with an opaque, non-leaking reason.
	//
	// FAIL-CLOSED: on *[Error]{[ErrPolicyUnavailable]} (the decision
	// engine is unreachable, Retryable=true) the caller MUST treat the outcome as
	// DENY, never permit.
	Authorize(ctx context.Context, principal Principal, action Action, resource ResourceRef) (Decision, error)

	// VerifyWebhookSignature returns nil iff the presented signature is valid for
	// the exact raw body on the given channel; otherwise a typed error and the
	// caller drops the request. rawBody must be the EXACT unparsed bytes the
	// signature was computed over.
	//
	// FAIL-CLOSED: any *[Error] ([ErrSignatureInvalid] — terminal;
	// [ErrSigningKeyUnavailable] — transient) MUST be treated as REJECT.
	VerifyWebhookSignature(ctx context.Context, channel WebhookChannel, rawBody []byte, presentedSignature SignatureMaterial) error

	// ObtainServiceIdentity mints/returns the platform's own short-lived service
	// credential for the named downstream audience, plus the service principal.
	//
	// Errors: *[Error]{[ErrIdentityUnavailable]} when the identity source
	// is unreachable (Retryable=true).
	ObtainServiceIdentity(ctx context.Context, audience ServiceAudience) (ServiceCredential, error)
}

// service is the concrete [Security] facet. It holds the swappable seams; every
// op is a thin, infrastructure-opaque mapping from a seam result to the contract
// surface.
type service struct {
	pdp      PolicyDecisionPoint
	verifier WebhookVerifier
	identity ServiceIdentitySource
	audit    AuditSink
}

var _ Security = (*service)(nil)

// Option configures the [Security] facet at construction. Each option injects a
// satellite- or product-supplied seam implementation in place of a shipped
// default — this is how a heavy/alternate mechanism (a policy-language PDP, a
// workload-identity source) enters without a surface change.
type Option func(*service)

// WithPolicyDecisionPoint injects the authorization decision engine.
func WithPolicyDecisionPoint(p PolicyDecisionPoint) Option {
	return func(s *service) { s.pdp = p }
}

// WithWebhookVerifier injects the webhook signature verifier.
func WithWebhookVerifier(v WebhookVerifier) Option {
	return func(s *service) { s.verifier = v }
}

// WithServiceIdentitySource injects the service-identity source.
func WithServiceIdentitySource(i ServiceIdentitySource) Option {
	return func(s *service) { s.identity = i }
}

// WithAuditSink injects the audit sink for authorization decisions.
func WithAuditSink(a AuditSink) Option {
	return func(s *service) { s.audit = a }
}

// New builds the [Security] facet with working, infrastructure-opaque defaults:
// a deny-by-default decision point, an HMAC-SHA256 webhook verifier with NO
// seeded secrets, an in-process service-identity source, and a no-op audit sink.
// Supply Options to replace any seam with a satellite- or product-provided
// implementation.
func New(opts ...Option) Security {
	s := &service{
		pdp:      defaultPolicyDecisionPoint{},
		verifier: NewHMACWebhookVerifier(nil),
		identity: NewInProcessIdentitySource(nil, 0),
		audit:    noopAuditSink{},
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

func (s *service) Authorize(ctx context.Context, principal Principal, action Action, resource ResourceRef) (Decision, error) {
	permit, err := s.pdp.Decide(ctx, principal, action, resource)
	if err != nil {
		// The decision engine was unreachable. Do NOT emit a permit. The contract
		// mandates the caller treat this as DENY (fail-closed); return the typed
		// transient error and a Deny value so a caller that ignores the error still
		// does not get a permit.
		s.audit.RecordDecision(ctx, principal, action, resource, false, "policy engine unavailable: "+err.Error())
		return Decision{Permit: false, Reason: ReasonNotPermitted}, WrapError(ErrPolicyUnavailable, err)
	}
	reason := ReasonNotPermitted
	if permit {
		reason = ReasonPermitted
	}
	s.audit.RecordDecision(ctx, principal, action, resource, permit, "decision="+reason.String())
	return Decision{Permit: permit, Reason: reason}, nil
}

func (s *service) VerifyWebhookSignature(ctx context.Context, channel WebhookChannel, rawBody []byte, presentedSignature SignatureMaterial) error {
	err := s.verifier.Verify(ctx, channel, rawBody, presentedSignature)
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrSignatureMismatch) {
		return NewError(ErrSignatureInvalid) // secret available, signature bad → terminal reject
	}
	return WrapError(ErrSigningKeyUnavailable, err) // secret unavailable → transient, still fail-closed
}

func (s *service) ObtainServiceIdentity(ctx context.Context, audience ServiceAudience) (ServiceCredential, error) {
	cred, err := s.identity.Mint(ctx, audience)
	if err != nil {
		return ServiceCredential{}, WrapError(ErrIdentityUnavailable, err)
	}
	return cred, nil
}
