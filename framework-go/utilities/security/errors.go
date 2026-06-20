package security

// SecurityError is the typed error returned across the security surface.
// Retryable is explicit per kind — never inferred by the caller. For [Validator],
// [Security.Authorize] and [Security.VerifyWebhookSignature] the FAIL-CLOSED rule
// applies: a SecurityError MUST be treated by the caller as deny/reject, never as
// permit/accept, even when Retryable is true (a transient outage must not open
// the gate).
type SecurityError struct {
	Kind      ErrorKind
	Retryable bool
	Cause     error // wrapped, optional
}

// ErrorKind classifies a [SecurityError]. The names are infrastructure-opaque:
// they name the security OUTCOME, never the mechanism.
type ErrorKind int

// Security error kinds. The trailing comment records the fixed Retryable value
// [NewError] seeds for each kind.
const (
	ErrUnknown               ErrorKind = iota
	ErrUnauthenticated                 // Validator: token absent/invalid/expired             — Retryable=false
	ErrPrincipalUnknown                // Validator: verified token maps to no principal       — Retryable=false
	ErrPolicyUnavailable               // Authorize: decision engine unreachable (FAIL CLOSED) — Retryable=true
	ErrSignatureInvalid                // VerifyWebhookSignature: bad signature (reject)        — Retryable=false
	ErrSigningKeyUnavailable           // VerifyWebhookSignature: secret fetch failed           — Retryable=true
	ErrIdentityUnavailable             // ObtainServiceIdentity: identity source unreachable    — Retryable=true
)

var errorKindNames = map[ErrorKind]string{
	ErrUnknown:               "Unknown",
	ErrUnauthenticated:       "Unauthenticated",
	ErrPrincipalUnknown:      "PrincipalUnknown",
	ErrPolicyUnavailable:     "PolicyUnavailable",
	ErrSignatureInvalid:      "SignatureInvalid",
	ErrSigningKeyUnavailable: "SigningKeyUnavailable",
	ErrIdentityUnavailable:   "IdentityUnavailable",
}

// String returns the stable, non-leaking name of the kind.
func (k ErrorKind) String() string {
	if n, ok := errorKindNames[k]; ok {
		return n
	}
	return "Unknown"
}

// defaultRetryable is the fixed Retryable value each kind carries. Transient
// infrastructure outages are retryable; identity/signature terminal outcomes are
// not.
func (k ErrorKind) defaultRetryable() bool {
	switch k {
	case ErrPolicyUnavailable, ErrSigningKeyUnavailable, ErrIdentityUnavailable:
		return true
	default:
		return false
	}
}

// NewError builds a *[SecurityError] with Retryable seeded from the kind. It is
// exported so a satellite-supplied [Validator] / [PolicyDecisionPoint] /
// [WebhookVerifier] / [ServiceIdentitySource] mints surface errors whose
// kind/Retryable pairing stays consistent with the contract.
func NewError(kind ErrorKind) *SecurityError {
	return &SecurityError{Kind: kind, Retryable: kind.defaultRetryable()}
}

// WrapError is [NewError] plus a wrapped cause (surfaced through Unwrap /
// errors.Is). The cause is internal context for logs only — it never reaches a
// caller-facing [Decision] reason.
func WrapError(kind ErrorKind, cause error) *SecurityError {
	e := NewError(kind)
	e.Cause = cause
	return e
}

func (e *SecurityError) Error() string {
	msg := "security: " + e.Kind.String()
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

func (e *SecurityError) Unwrap() error { return e.Cause }
