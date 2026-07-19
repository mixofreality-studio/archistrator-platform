package manager_test

import (
	"reflect"
	"testing"
	"time"

	eng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	mgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	ra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

func TestActivityPresetOptionsTimeout(t *testing.T) {
	opts := mgr.ActivityPreset{Timeout: 30 * time.Second}.Options()
	if opts.StartToCloseTimeout != 30*time.Second {
		t.Errorf("StartToCloseTimeout = %v, want 30s", opts.StartToCloseTimeout)
	}
}

func TestActivityPresetOptionsMaxAttemptsUnsetLeavesZeroValue(t *testing.T) {
	opts := mgr.ActivityPreset{Timeout: 10 * time.Second}.Options()
	if opts.RetryPolicy == nil {
		t.Fatalf("RetryPolicy is nil, want non-nil (even with no MaxAttempts set)")
	}
	if opts.RetryPolicy.MaximumAttempts != 0 {
		t.Errorf("MaximumAttempts = %d, want 0 (unset ⇒ Temporal default of unlimited)", opts.RetryPolicy.MaximumAttempts)
	}
}

func TestActivityPresetOptionsMaxAttemptsSet(t *testing.T) {
	opts := mgr.ActivityPreset{Timeout: 10 * time.Second, MaxAttempts: 5}.Options()
	if opts.RetryPolicy.MaximumAttempts != 5 {
		t.Errorf("MaximumAttempts = %d, want 5", opts.RetryPolicy.MaximumAttempts)
	}
}

func TestActivityPresetOptionsTerminalSetEmptyIsNil(t *testing.T) {
	opts := mgr.ActivityPreset{Timeout: 10 * time.Second}.Options()
	if opts.RetryPolicy == nil {
		t.Fatalf("RetryPolicy is nil, want a valid (empty-terminal-set) RetryPolicy")
	}
	if len(opts.RetryPolicy.NonRetryableErrorTypes) != 0 {
		t.Errorf("NonRetryableErrorTypes = %v, want empty", opts.RetryPolicy.NonRetryableErrorTypes)
	}
}

func TestActivityPresetOptionsTerminalRA(t *testing.T) {
	opts := mgr.ActivityPreset{
		Timeout:    10 * time.Second,
		TerminalRA: []ra.Kind{ra.NotFound, ra.ContractMisuse},
	}.Options()
	want := []string{mgr.RAErrType(ra.NotFound), mgr.RAErrType(ra.ContractMisuse)}
	if !reflect.DeepEqual(opts.RetryPolicy.NonRetryableErrorTypes, want) {
		t.Errorf("NonRetryableErrorTypes = %v, want %v", opts.RetryPolicy.NonRetryableErrorTypes, want)
	}
}

func TestActivityPresetOptionsTerminalRAIncludesConflict(t *testing.T) {
	// Conflict must be includable — some presets treat it as terminal.
	opts := mgr.ActivityPreset{
		Timeout:    10 * time.Second,
		TerminalRA: []ra.Kind{ra.Conflict},
	}.Options()
	want := []string{mgr.RAErrType(ra.Conflict)}
	if !reflect.DeepEqual(opts.RetryPolicy.NonRetryableErrorTypes, want) {
		t.Errorf("NonRetryableErrorTypes = %v, want %v", opts.RetryPolicy.NonRetryableErrorTypes, want)
	}
}

func TestActivityPresetOptionsTerminalRAExcludesConflictWhenOmitted(t *testing.T) {
	// Same activity family, Conflict deliberately left out (retryable) — the
	// builder must never inject NonRetryableErrorTypes() defaults.
	opts := mgr.ActivityPreset{
		Timeout:    10 * time.Second,
		TerminalRA: []ra.Kind{ra.NotFound},
	}.Options()
	for _, s := range opts.RetryPolicy.NonRetryableErrorTypes {
		if s == mgr.RAErrType(ra.Conflict) {
			t.Errorf("NonRetryableErrorTypes = %v, must not include Conflict when not specified", opts.RetryPolicy.NonRetryableErrorTypes)
		}
	}
}

func TestActivityPresetOptionsTerminalEngine(t *testing.T) {
	opts := mgr.ActivityPreset{
		Timeout:        10 * time.Second,
		TerminalEngine: []eng.Kind{eng.InvalidInput},
	}.Options()
	want := []string{mgr.EngineErrType(eng.InvalidInput)}
	if !reflect.DeepEqual(opts.RetryPolicy.NonRetryableErrorTypes, want) {
		t.Errorf("NonRetryableErrorTypes = %v, want %v", opts.RetryPolicy.NonRetryableErrorTypes, want)
	}
}

func TestActivityPresetOptionsTerminalManager(t *testing.T) {
	opts := mgr.ActivityPreset{
		Timeout:         10 * time.Second,
		TerminalManager: []mgr.Kind{mgr.FailedPrecondition},
	}.Options()
	want := []string{mgr.ErrType(mgr.FailedPrecondition)}
	if !reflect.DeepEqual(opts.RetryPolicy.NonRetryableErrorTypes, want) {
		t.Errorf("NonRetryableErrorTypes = %v, want %v", opts.RetryPolicy.NonRetryableErrorTypes, want)
	}
}

func TestActivityPresetOptionsTerminalSetsCombineInOrder(t *testing.T) {
	// RA, then Engine, then Manager — order must be deterministic so callers
	// (and tests) can assert on it directly.
	opts := mgr.ActivityPreset{
		Timeout:         10 * time.Second,
		TerminalRA:      []ra.Kind{ra.NotFound, ra.Auth},
		TerminalEngine:  []eng.Kind{eng.InvalidInput},
		TerminalManager: []mgr.Kind{mgr.Unauthorized},
	}.Options()
	want := []string{
		mgr.RAErrType(ra.NotFound), mgr.RAErrType(ra.Auth),
		mgr.EngineErrType(eng.InvalidInput),
		mgr.ErrType(mgr.Unauthorized),
	}
	if !reflect.DeepEqual(opts.RetryPolicy.NonRetryableErrorTypes, want) {
		t.Errorf("NonRetryableErrorTypes = %v, want %v", opts.RetryPolicy.NonRetryableErrorTypes, want)
	}
}
