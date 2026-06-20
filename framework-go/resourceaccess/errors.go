// Package resourceaccess holds the ResourceAccess-layer framework concerns for
// Method systems built by aiarch. Its first tenant is the standard error model:
// one fixed Kind enum every RA component shares, plus a uniform Error type whose
// Retryable flag is seeded from the kind. It imports no Temporal and does no IO,
// so RA components can depend on it without violating the layer model
// ([[the-method-layers]]).
package resourceaccess

// Kind is the fixed ResourceAccess error classification shared by every RA
// component. Component-specific nuance lives in Error.Detail.
type Kind int

const (
	Unknown Kind = iota
	Transient      // retryable: a transient infra blip
	RateLimited    // retryable: an external resource throttled the caller
	Infrastructure      // retryable: the backing store/infrastructure is temporarily down
	Auth           // terminal: credential/permission failure
	NotFound       // terminal: the addressed resource does not exist
	Conflict       // terminal: uniqueness or optimistic-concurrency (version) conflict
	QuotaExhausted // terminal: a hard quota was hit
	ContentPolicy  // terminal: content rejected by policy
	ContractMisuse // terminal: programmer error (bad arguments)
)

var kindNames = map[Kind]string{
	Unknown: "Unknown", Transient: "Transient", RateLimited: "RateLimited",
	Infrastructure: "Infrastructure", Auth: "Auth", NotFound: "NotFound", Conflict: "Conflict",
	QuotaExhausted: "QuotaExhausted", ContentPolicy: "ContentPolicy",
	ContractMisuse: "ContractMisuse",
}

// String returns the stable name used in Temporal Type() strings.
func (k Kind) String() string {
	if n, ok := kindNames[k]; ok {
		return n
	}
	return "Unknown"
}

// DefaultRetryable reports whether this kind is retryable unless overridden.
func (k Kind) DefaultRetryable() bool {
	switch k {
	case Transient, RateLimited, Infrastructure:
		return true
	default:
		return false
	}
}

// Kinds returns every Kind, for exhaustive iteration (e.g. building the
// non-retryable Temporal type list).
func Kinds() []Kind {
	return []Kind{Unknown, Transient, RateLimited, Infrastructure, Auth, NotFound,
		Conflict, QuotaExhausted, ContentPolicy, ContractMisuse}
}

// Error is the uniform ResourceAccess error. Retryable is seeded from the kind's
// default by New/Wrap and may be overridden on the returned value for the rare
// exception.
type Error struct {
	Kind      Kind
	Retryable bool
	Detail    string
	Cause     error
}

// New builds an Error with Retryable seeded from kind.DefaultRetryable().
func New(kind Kind, detail string) *Error {
	return &Error{Kind: kind, Retryable: kind.DefaultRetryable(), Detail: detail}
}

// Wrap is New plus a wrapped cause (surfaced through Unwrap / errors.Is).
func Wrap(kind Kind, cause error, detail string) *Error {
	e := New(kind, detail)
	e.Cause = cause
	return e
}

func (e *Error) Error() string {
	msg := "resourceaccess: " + e.Detail
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

func (e *Error) Unwrap() error { return e.Cause }
