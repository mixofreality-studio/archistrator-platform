package scenariohost

import (
	"testing"

	fwe "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwm "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// The layer call contexts (deterministic-component-testing design §12 B2): every
// Manager, Engine and ResourceAccess op takes its layer's framework Context as
// its first parameter, so a hooks file that calls a REAL collaborator's public op
// — a probe on another component, an arrangement through an actor's own verb —
// needs one. The host serves them, as it serves the Temporal client: the hooks
// file passes the value straight to the op and never imports the framework's
// layer packages, so the hooks import rule (the component's own package, the
// host, testinfra, the app's component and contract packages, stdlib) stays as
// it is.

// ResourceAccessContext is the call context for a ResourceAccess op made on
// t's behalf. Its idempotency key is t.Name() + "/" + key — the same derivation
// a generated scenarios file uses for its own steps — so a hooks file that names
// a distinct key per call (e.g. "probe-2.1", "arrange-register") never collides
// with a generated step, and a re-run of the same scenario replays the same key.
func (h *Host) ResourceAccessContext(t *testing.T, key string) fwra.Context {
	t.Helper()
	return fwra.Context{Context: h.Context(t), IdempotencyKey: fwra.IdempotencyKey(t.Name() + "/" + key)}
}

// EngineContext is the call context for an Engine op made on t's behalf.
func (h *Host) EngineContext(t *testing.T) fwe.Context {
	t.Helper()
	return fwe.Context{Context: h.Context(t)}
}

// ManagerContext is the call context for a Manager op made on t's behalf.
func (h *Host) ManagerContext(t *testing.T) fwm.Context {
	t.Helper()
	return fwm.Context{Context: h.Context(t)}
}
