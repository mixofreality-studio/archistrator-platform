package scenariohost

import (
	"testing"
)

func TestLayerCallContextsAreBoundToTheTest(t *testing.T) {
	h := Start(t)

	ra := h.ResourceAccessContext(t, "probe-2.1")
	if ra.Context != t.Context() {
		t.Error("ResourceAccessContext does not carry the test's context")
	}
	if got, want := string(ra.IdempotencyKey), t.Name()+"/probe-2.1"; got != want {
		t.Errorf("IdempotencyKey = %q, want %q (the generated callContext's derivation)", got, want)
	}
	if again := h.ResourceAccessContext(t, "probe-2.1"); again.IdempotencyKey != ra.IdempotencyKey {
		t.Errorf("the same key must replay the same idempotency key: %q vs %q", again.IdempotencyKey, ra.IdempotencyKey)
	}
	if other := h.ResourceAccessContext(t, "arrange-register"); other.IdempotencyKey == ra.IdempotencyKey {
		t.Errorf("distinct keys must not collide: both %q", ra.IdempotencyKey)
	}

	if e := h.EngineContext(t); e.Context != t.Context() {
		t.Error("EngineContext does not carry the test's context")
	}
	if m := h.ManagerContext(t); m.Context != t.Context() {
		t.Error("ManagerContext does not carry the test's context")
	}
}
