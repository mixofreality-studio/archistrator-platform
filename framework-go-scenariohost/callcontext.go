package scenariohost

import (
	"context"
	"testing"
	"time"

	fwe "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwm "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
)

// Principal is who a scenario step acts as: the test plan's principal for the
// step's actor. A generated test calls the subject with a context carrying it
// (PrincipalContext), so the component sees the identity a real request would
// have carried - never an anonymous call.
//
// OnBehalfOf is the subject of the human an agent acts for (the plan's
// onBehalfOf): the delegation claim. Intents is what that human lets the agent
// do, in the app's own vocabulary (the plan's intents); it is read only when
// OnBehalfOf is set.
type Principal struct {
	Subject    string
	Roles      []string
	OnBehalfOf string
	Intents    []string
}

// ClaimDelegations is the key of the delegation claim in security.Principal.Claims. security.Principal
// has no delegation field of its own (Claims is its escape hatch), so a delegated principal is a
// PrincipalApplication identity whose Claims[ClaimDelegations] is a list of grants, each
// {"grantor": <the human's subject>, "intents": [<what the human allows>]}. The shape is the one
// the apps' policies read from rc.Principal.Claims; a grant with no intents has no reach.
const ClaimDelegations = "delegations"

// Delegation is one grant in the delegation claim: the human who delegated and what they allowed.
type Delegation struct {
	Grantor string   `json:"grantor"`
	Intents []string `json:"intents"`
}

// DelegationsOf decodes the delegation claim of p: the one reader of the shape securityPrincipal
// writes. It accepts the generic JSON form ([]any of map[string]any, with intents as []any or
// []string) a principal rebuilt from a token carries, and []Delegation. A principal with no
// delegation claim, or a malformed one, has no delegations.
func DelegationsOf(p security.Principal) []Delegation {
	switch raw := p.Claims[ClaimDelegations].(type) {
	case []Delegation:
		return append([]Delegation(nil), raw...)
	case []any:
		var out []Delegation
		for _, item := range raw {
			switch g := item.(type) {
			case Delegation:
				out = append(out, g)
			case map[string]any:
				grantor, _ := g["grantor"].(string)
				out = append(out, Delegation{Grantor: grantor, Intents: stringsOf(g["intents"])})
			}
		}
		return out
	}
	return nil
}

// stringsOf is v as a string list: []string as is, []any keeping its string elements.
func stringsOf(v any) []string {
	switch s := v.(type) {
	case []string:
		return append([]string(nil), s...)
	case []any:
		var out []string
		for _, e := range s {
			if str, ok := e.(string); ok {
				out = append(out, str)
			}
		}
		return out
	}
	return nil
}

// securityPrincipal is p as the framework's principal. A Principal with no Subject fails the test: a
// call with no identity is not a call made as someone.
func securityPrincipal(t *testing.T, p Principal) security.Principal {
	t.Helper()
	if p.Subject == "" {
		t.Fatalf("scenariohost: Principal has no Subject")
	}
	sp := security.Principal{
		Kind:    security.PrincipalUser,
		Subject: p.Subject,
		Roles:   append([]string(nil), p.Roles...),
	}
	if p.OnBehalfOf == "" && len(p.Intents) > 0 {
		t.Fatalf("scenariohost: Principal %q declares intents but no OnBehalfOf", p.Subject)
	}
	if p.OnBehalfOf != "" {
		intents := make([]any, len(p.Intents))
		for i, in := range p.Intents {
			intents[i] = in
		}
		sp.Kind = security.PrincipalApplication
		sp.Claims = map[string]any{ClaimDelegations: []any{map[string]any{"grantor": p.OnBehalfOf, "intents": intents}}}
	}
	return sp
}

// principalIn is ctx carrying p as the authenticated principal.
func principalIn(t *testing.T, ctx context.Context, p Principal) context.Context {
	t.Helper()
	return security.WithPrincipal(ctx, securityPrincipal(t, p))
}

// PrincipalContext is the test's context carrying p as the authenticated
// principal (security.PrincipalFrom reads it back). A Principal with no Subject
// fails the test: a call with no identity is not a call made as someone.
func (h *Host) PrincipalContext(t *testing.T, p Principal) context.Context {
	t.Helper()
	return principalIn(t, h.Context(t), p)
}

// ResourceAccessContextAs is ResourceAccessContext acting as p: the same
// idempotency-key derivation, with p on the context.
func (h *Host) ResourceAccessContextAs(t *testing.T, p Principal, key string) fwra.Context {
	t.Helper()
	return fwra.Context{Context: h.PrincipalContext(t, p), Principal: securityPrincipal(t, p), IdempotencyKey: fwra.IdempotencyKey(t.Name() + "/" + key)}
}

// EngineContextAs is EngineContext acting as p. The principal is on the context's own field as well as
// its context value: a component reads rc.Principal, which the server's middleware fills.
func (h *Host) EngineContextAs(t *testing.T, p Principal) fwe.Context {
	t.Helper()
	return fwe.Context{Context: h.PrincipalContext(t, p), Principal: securityPrincipal(t, p)}
}

// ManagerContextAs is ManagerContext acting as p (its principal on rc.Principal, as EngineContextAs).
func (h *Host) ManagerContextAs(t *testing.T, p Principal) fwm.Context {
	t.Helper()
	return fwm.Context{Context: h.PrincipalContext(t, p), Principal: securityPrincipal(t, p)}
}

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

// Probe deadline (FPC plan 2, Task 2.13c). A generated probe reads the world before and after a
// step; a read that never returns (a long-polling Subscribe) must not hang the scenario until its
// watchdog kills the process. Every probe call is made with a context bounded by ProbeDeadline.

// maxProbeDeadline caps a probe's deadline however long the scenario may run.
const maxProbeDeadline = 10 * time.Second

// ProbeDeadline is how long one probe call may take: a tenth of the scenario timeout, at most ten
// seconds.
func ProbeDeadline() time.Duration {
	return min(ScenarioTimeout()/10, maxProbeDeadline)
}

// ProbeCalls builds the call contexts of one probe call: the same contexts the Host serves, every one
// bounded by the probe deadline, which starts when ProbeContext was called.
type ProbeCalls struct {
	ctx context.Context
}

// ProbeContext starts a probe call: the returned ProbeCalls hands out the call contexts of that
// call, all of them expiring ProbeDeadline() from now (or when the test ends). A generated probe
// closure calls it once per invocation, so the pre-state and the post-state read each get a full
// deadline.
func (h *Host) ProbeContext(t *testing.T) ProbeCalls {
	t.Helper()
	ctx, cancel := context.WithTimeout(h.Context(t), ProbeDeadline())
	t.Cleanup(cancel)
	return ProbeCalls{ctx: ctx}
}

// Context is the bounded context a probe call is made with.
func (p ProbeCalls) Context(t *testing.T) context.Context {
	t.Helper()
	return p.ctx
}

// PrincipalContext is Context carrying pr as the authenticated principal.
func (p ProbeCalls) PrincipalContext(t *testing.T, pr Principal) context.Context {
	t.Helper()
	return principalIn(t, p.ctx, pr)
}

// ResourceAccessContext is Host.ResourceAccessContext bounded by the probe deadline.
func (p ProbeCalls) ResourceAccessContext(t *testing.T, key string) fwra.Context {
	t.Helper()
	return fwra.Context{Context: p.ctx, IdempotencyKey: fwra.IdempotencyKey(t.Name() + "/" + key)}
}

// ResourceAccessContextAs is Host.ResourceAccessContextAs bounded by the probe deadline.
func (p ProbeCalls) ResourceAccessContextAs(t *testing.T, pr Principal, key string) fwra.Context {
	t.Helper()
	return fwra.Context{Context: p.PrincipalContext(t, pr), Principal: securityPrincipal(t, pr), IdempotencyKey: fwra.IdempotencyKey(t.Name() + "/" + key)}
}

// EngineContext is Host.EngineContext bounded by the probe deadline.
func (p ProbeCalls) EngineContext(t *testing.T) fwe.Context {
	t.Helper()
	return fwe.Context{Context: p.ctx}
}

// EngineContextAs is Host.EngineContextAs bounded by the probe deadline.
func (p ProbeCalls) EngineContextAs(t *testing.T, pr Principal) fwe.Context {
	t.Helper()
	return fwe.Context{Context: p.PrincipalContext(t, pr), Principal: securityPrincipal(t, pr)}
}

// ManagerContext is Host.ManagerContext bounded by the probe deadline.
func (p ProbeCalls) ManagerContext(t *testing.T) fwm.Context {
	t.Helper()
	return fwm.Context{Context: p.ctx}
}

// ManagerContextAs is Host.ManagerContextAs bounded by the probe deadline.
func (p ProbeCalls) ManagerContextAs(t *testing.T, pr Principal) fwm.Context {
	t.Helper()
	return fwm.Context{Context: p.PrincipalContext(t, pr), Principal: securityPrincipal(t, pr)}
}
