package manager_test

import (
	"errors"
	"testing"

	mgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
)

func TestManagerDefaultRetryable(t *testing.T) {
	if !mgr.Infrastructure.DefaultRetryable() {
		t.Errorf("Infrastructure.DefaultRetryable() = false, want true")
	}
	for _, k := range []mgr.Kind{mgr.Unknown, mgr.ContractMisuse, mgr.NotFound, mgr.Unauthorized, mgr.FailedPrecondition} {
		if k.DefaultRetryable() {
			t.Errorf("%v.DefaultRetryable() = true, want false", k)
		}
	}
}

func TestManagerWrapUnwrap(t *testing.T) {
	cause := errors.New("infrastructure down")
	e := mgr.Wrap(mgr.Infrastructure, cause, "durable execution unavailable")
	if !errors.Is(e, cause) {
		t.Errorf("errors.Is = false, want true")
	}
	if !e.Retryable {
		t.Errorf("Infrastructure error Retryable = false, want true")
	}
	if e.Error() != "manager: durable execution unavailable: infrastructure down" {
		t.Errorf("Error() = %q", e.Error())
	}
}

func TestManagerKindsCoversEnum(t *testing.T) {
	if len(mgr.Kinds()) != 6 {
		t.Errorf("len(Kinds()) = %d, want 6", len(mgr.Kinds()))
	}
}
