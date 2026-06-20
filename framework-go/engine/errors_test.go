package engine_test

import (
	"errors"
	"testing"

	eng "github.com/davidmarne/archistrator-platform/framework-go/engine"
)

func TestEngineErrorsAlwaysNonRetryable(t *testing.T) {
	for _, k := range eng.Kinds() {
		if e := eng.New(k, "x"); e.Retryable {
			t.Errorf("engine.New(%v).Retryable = true; engine errors are never retryable", k)
		}
	}
}

func TestEngineWrapUnwrap(t *testing.T) {
	cause := errors.New("bad input")
	e := eng.Wrap(eng.InvalidInput, cause, "unknown artifact kind")
	if !errors.Is(e, cause) {
		t.Errorf("errors.Is = false, want true")
	}
	if e.Error() != "engine: unknown artifact kind: bad input" {
		t.Errorf("Error() = %q", e.Error())
	}
}

func TestEngineKindsCoversEnum(t *testing.T) {
	if len(eng.Kinds()) != 4 {
		t.Errorf("len(Kinds()) = %d, want 4", len(eng.Kinds()))
	}
}
