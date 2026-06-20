package resourceaccess_test

import (
	"errors"
	"testing"

	ra "github.com/davidmarne/archistrator-platform/framework-go/resourceaccess"
)

func TestDefaultRetryable(t *testing.T) {
	retryable := map[ra.Kind]bool{
		ra.Transient: true, ra.RateLimited: true, ra.Infrastructure: true,
		ra.Unknown: false, ra.Auth: false, ra.NotFound: false, ra.Conflict: false,
		ra.QuotaExhausted: false, ra.ContentPolicy: false, ra.ContractMisuse: false,
	}
	for k, want := range retryable {
		if got := k.DefaultRetryable(); got != want {
			t.Errorf("%v.DefaultRetryable() = %v, want %v", k, got, want)
		}
	}
}

func TestNewSeedsRetryableFromKind(t *testing.T) {
	if e := ra.New(ra.Transient, "blip"); !e.Retryable {
		t.Errorf("New(Transient) Retryable = false, want true")
	}
	if e := ra.New(ra.NotFound, "gone"); e.Retryable {
		t.Errorf("New(NotFound) Retryable = true, want false")
	}
}

func TestWrapUnwrapAndMessage(t *testing.T) {
	cause := errors.New("root cause")
	e := ra.Wrap(ra.Conflict, cause, "version mismatch")
	if !errors.Is(e, cause) {
		t.Errorf("errors.Is(e, cause) = false, want true")
	}
	if e.Error() != "resourceaccess: version mismatch: root cause" {
		t.Errorf("Error() = %q", e.Error())
	}
}

func TestKindsCoversEnum(t *testing.T) {
	if len(ra.Kinds()) != 10 {
		t.Errorf("len(Kinds()) = %d, want 10", len(ra.Kinds()))
	}
}
