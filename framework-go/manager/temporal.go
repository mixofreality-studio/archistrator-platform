package manager

import (
	"errors"

	eng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	ra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"go.temporal.io/sdk/temporal"
)

// RAErrType is the canonical Temporal Type() string for a ResourceAccess kind,
// e.g. "ResourceAccess_NotFound". Manager Activity RetryPolicies use this to name
// non-retryable types.
func RAErrType(k ra.Kind) string { return "ResourceAccess_" + k.String() }

// EngineErrType is the canonical Temporal Type() string for an Engine kind.
func EngineErrType(k eng.Kind) string { return "Engine_" + k.String() }

// ManagerErrType is the canonical Temporal Type() string for a Manager kind.
func ManagerErrType(k Kind) string { return "Manager_" + k.String() }

// MapError converts any of the three layer errors into a Temporal
// ApplicationError tagged with the canonical stable Type() and the error's
// Retryable flag. Non-layer errors (including nil) pass through unchanged.
//
// Retryability composes with the Activity RetryPolicy: a retryable mapping can
// still be made terminal for a specific Activity by listing its Type() in that
// Activity's RetryPolicy.NonRetryableErrorTypes.
func MapError(err error) error {
	if err == nil {
		return nil
	}
	var rae *ra.Error
	if errors.As(err, &rae) {
		return tagError(err, RAErrType(rae.Kind), rae.Retryable)
	}
	var ene *eng.Error
	if errors.As(err, &ene) {
		return tagError(err, EngineErrType(ene.Kind), ene.Retryable)
	}
	var mge *Error
	if errors.As(err, &mge) {
		return tagError(err, ManagerErrType(mge.Kind), mge.Retryable)
	}
	return err
}

func tagError(cause error, errType string, retryable bool) error {
	if retryable {
		return temporal.NewApplicationErrorWithCause(cause.Error(), errType, cause)
	}
	return temporal.NewNonRetryableApplicationError(cause.Error(), errType, cause)
}

// NonRetryableErrorTypes is the canonical union of every non-retryable Type()
// across the three layer enums — a convenience default for an Activity
// RetryPolicy. Activities that need finer control build their own list from the
// *ErrType helpers instead.
func NonRetryableErrorTypes() []string {
	var out []string
	for _, k := range ra.Kinds() {
		if !k.DefaultRetryable() {
			out = append(out, RAErrType(k))
		}
	}
	for _, k := range eng.Kinds() {
		out = append(out, EngineErrType(k)) // engine errors are always non-retryable
	}
	for _, k := range Kinds() {
		if !k.DefaultRetryable() {
			out = append(out, ManagerErrType(k))
		}
	}
	return out
}
