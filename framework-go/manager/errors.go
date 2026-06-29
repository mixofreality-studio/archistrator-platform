// Package manager holds the Manager-layer framework concerns for Method systems
// built by aiarch: the standard façade error model AND the error→Temporal bridge
// (temporal.go). This is the ONLY framework-go package that imports
// go.temporal.io, mirroring the rule that Temporal lives only in the Manager
// layer ([[the-method-layers]]). It imports the lower layers' error packages
// (resourceaccess, engine) — legal downward dependencies — so MapError can
// classify their errors.
package manager

// Kind is the fixed Manager façade error classification returned to clients.
// Component-specific nuance lives in Error.Detail.
type Kind int

const (
	Unknown            Kind = iota
	ContractMisuse          // malformed request at the façade boundary
	NotFound                // the addressed entity/session does not exist
	Unauthorized            // caller failed authorization
	FailedPrecondition      // a workflow/state gate was not satisfied
	Infrastructure          // retryable: durable-execution infrastructure unavailable
)

var kindNames = map[Kind]string{
	Unknown: "Unknown", ContractMisuse: "ContractMisuse", NotFound: "NotFound",
	Unauthorized: "Unauthorized", FailedPrecondition: "FailedPrecondition",
	Infrastructure: "Infrastructure",
}

// String returns the stable name used in Temporal Type() strings.
func (k Kind) String() string {
	if n, ok := kindNames[k]; ok {
		return n
	}
	return "Unknown"
}

// DefaultRetryable reports whether this kind is retryable unless overridden.
// Only Infrastructure (transient infra) is retryable by default.
func (k Kind) DefaultRetryable() bool { return k == Infrastructure }

// Kinds returns every Kind, for exhaustive iteration.
func Kinds() []Kind {
	return []Kind{Unknown, ContractMisuse, NotFound, Unauthorized, FailedPrecondition, Infrastructure}
}

// Error is the uniform Manager façade error.
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

// Wrap is New plus a wrapped cause.
func Wrap(kind Kind, cause error, detail string) *Error {
	e := New(kind, detail)
	e.Cause = cause
	return e
}

func (e *Error) Error() string {
	msg := "manager: " + e.Detail
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

func (e *Error) Unwrap() error { return e.Cause }
