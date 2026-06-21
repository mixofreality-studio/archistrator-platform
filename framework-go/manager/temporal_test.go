package manager_test

import (
	"errors"
	"testing"

	eng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	mgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	ra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"go.temporal.io/sdk/temporal"
)

func appErr(t *testing.T, err error) *temporal.ApplicationError {
	t.Helper()
	var ae *temporal.ApplicationError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *temporal.ApplicationError, got %T: %v", err, err)
	}
	return ae
}

func TestMapErrorResourceAccess(t *testing.T) {
	ae := appErr(t, mgr.MapError(ra.New(ra.NotFound, "gone")))
	if ae.Type() != "ResourceAccess_NotFound" {
		t.Errorf("Type() = %q, want ResourceAccess_NotFound", ae.Type())
	}
	if !ae.NonRetryable() {
		t.Errorf("NotFound mapped retryable, want non-retryable")
	}
}

func TestMapErrorRetryableStaysRetryable(t *testing.T) {
	ae := appErr(t, mgr.MapError(ra.New(ra.Transient, "blip")))
	if ae.NonRetryable() {
		t.Errorf("Transient mapped non-retryable, want retryable")
	}
}

func TestMapErrorEngine(t *testing.T) {
	ae := appErr(t, mgr.MapError(eng.New(eng.InvalidInput, "bad")))
	if ae.Type() != "Engine_InvalidInput" {
		t.Errorf("Type() = %q, want Engine_InvalidInput", ae.Type())
	}
	if !ae.NonRetryable() {
		t.Errorf("engine error mapped retryable, want non-retryable")
	}
}

func TestMapErrorManager(t *testing.T) {
	ae := appErr(t, mgr.MapError(mgr.New(mgr.FailedPrecondition, "gate")))
	if ae.Type() != "Manager_FailedPrecondition" {
		t.Errorf("Type() = %q, want Manager_FailedPrecondition", ae.Type())
	}
}

func TestMapErrorNilAndPassthrough(t *testing.T) {
	if mgr.MapError(nil) != nil {
		t.Errorf("MapError(nil) != nil")
	}
	plain := errors.New("not a layer error")
	if got := mgr.MapError(plain); got != plain {
		t.Errorf("MapError(plain) = %v, want passthrough", got)
	}
}

func TestNonRetryableErrorTypes(t *testing.T) {
	got := map[string]bool{}
	for _, s := range mgr.NonRetryableErrorTypes() {
		got[s] = true
	}
	for _, want := range []string{
		"ResourceAccess_NotFound", "ResourceAccess_ContractMisuse",
		"Engine_InvalidInput", "Manager_FailedPrecondition",
	} {
		if !got[want] {
			t.Errorf("NonRetryableErrorTypes missing %q", want)
		}
	}
	for _, notWant := range []string{"ResourceAccess_Transient", "Manager_Infrastructure"} {
		if got[notWant] {
			t.Errorf("NonRetryableErrorTypes wrongly includes %q", notWant)
		}
	}
}
