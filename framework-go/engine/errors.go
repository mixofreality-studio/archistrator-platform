// Package engine holds the Engine-layer framework concerns for Method systems
// built by aiarch. Its first tenant is the standard error model. Engines are
// pure deterministic computation with no IO, so every engine error is
// non-retryable and the error channel is reserved for programmer/contract
// misuse — a failing computation is a domain result, not an error. Imports no
// Temporal ([[the-method-layers]]).
package engine

// Kind is the fixed Engine error classification. All engine errors are
// non-retryable. Component-specific nuance lives in Error.Detail.
type Kind int

const (
	Unknown           Kind = iota
	ContractMisuse         // bad call shape / arguments
	InvalidInput           // input fails the engine's domain rules
	InternalInvariant      // a broken internal invariant (a bug)
)

var kindNames = map[Kind]string{
	Unknown: "Unknown", ContractMisuse: "ContractMisuse",
	InvalidInput: "InvalidInput", InternalInvariant: "InternalInvariant",
}

// String returns the stable name used in Temporal Type() strings.
func (k Kind) String() string {
	if n, ok := kindNames[k]; ok {
		return n
	}
	return "Unknown"
}

// Kinds returns every Kind, for exhaustive iteration.
func Kinds() []Kind { return []Kind{Unknown, ContractMisuse, InvalidInput, InternalInvariant} }

// Error is the uniform Engine error. Retryable is always false (engines do no
// IO); the field exists for shape-parity with the other layers' Error types.
type Error struct {
	Kind      Kind
	Retryable bool // always false
	Detail    string
	Cause     error
}

// New builds a non-retryable Engine error.
func New(kind Kind, detail string) *Error {
	return &Error{Kind: kind, Retryable: false, Detail: detail}
}

// Wrap is New plus a wrapped cause.
func Wrap(kind Kind, cause error, detail string) *Error {
	e := New(kind, detail)
	e.Cause = cause
	return e
}

func (e *Error) Error() string {
	msg := "engine: " + e.Detail
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

func (e *Error) Unwrap() error { return e.Cause }
