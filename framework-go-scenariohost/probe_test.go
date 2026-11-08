package scenariohost

import (
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
)

func TestProbeDeadline_IsATenthOfTheScenarioTimeoutCappedAtTenSeconds(t *testing.T) {
	for _, tc := range []struct {
		timeout string
		want    time.Duration
	}{
		{"", 10 * time.Second},     // default 15m: a tenth is 90s, capped
		{"1h", 10 * time.Second},   // capped
		{"100s", 10 * time.Second}, // exactly the cap
		{"50s", 5 * time.Second},   // a tenth
		{"3s", 300 * time.Millisecond},
	} {
		t.Setenv("AIARCH_SCENARIO_TIMEOUT", tc.timeout)
		if got := ProbeDeadline(); got != tc.want {
			t.Errorf("AIARCH_SCENARIO_TIMEOUT=%q: ProbeDeadline = %s, want %s", tc.timeout, got, tc.want)
		}
	}
}

// Every layer context a probe is called with carries the deadline: a probe on a long-polling read
// returns when it passes instead of hanging the scenario.
func TestProbeContext_EveryLayerContextIsBounded(t *testing.T) {
	t.Setenv("AIARCH_SCENARIO_TIMEOUT", "50s")
	h := &Host{}
	pc := h.ProbeContext(t)
	p := Principal{Subject: "alice"}
	before := time.Now()
	for name, ctx := range map[string]interface{ Deadline() (time.Time, bool) }{
		"Context":                 pc.Context(t),
		"PrincipalContext":        pc.PrincipalContext(t, p),
		"ManagerContext":          pc.ManagerContext(t),
		"ManagerContextAs":        pc.ManagerContextAs(t, p),
		"EngineContext":           pc.EngineContext(t),
		"EngineContextAs":         pc.EngineContextAs(t, p),
		"ResourceAccessContext":   pc.ResourceAccessContext(t, "probe-1.1"),
		"ResourceAccessContextAs": pc.ResourceAccessContextAs(t, p, "probe-1.1"),
	} {
		dl, ok := ctx.Deadline()
		if !ok {
			t.Errorf("%s carries no deadline", name)
			continue
		}
		if d := dl.Sub(before); d <= 0 || d > 5*time.Second+time.Second {
			t.Errorf("%s deadline is %s away, want about 5s", name, d)
		}
	}
	if _, ok := h.Context(t).Deadline(); ok {
		t.Error("the plain test context must stay unbounded: only probes are bounded")
	}
}

func TestProbeContext_KeepsKeyDerivationAndPrincipal(t *testing.T) {
	h := &Host{}
	pc := h.ProbeContext(t)
	if got, want := pc.ResourceAccessContext(t, "probe-2.1").IdempotencyKey, h.ResourceAccessContext(t, "probe-2.1").IdempotencyKey; got != want {
		t.Errorf("probe key = %q, want the host's derivation %q", got, want)
	}
	m := pc.ManagerContextAs(t, Principal{Subject: "owner", Roles: []string{"member"}})
	got, ok := security.PrincipalFrom(m.Context)
	if !ok || got.Subject != "owner" || len(got.Roles) != 1 {
		t.Errorf("probe principal = %+v, %v", got, ok)
	}
}

func TestProbeContext_ExpiresWithinTheDeadline(t *testing.T) {
	t.Setenv("AIARCH_SCENARIO_TIMEOUT", "1s") // a tenth: 100ms
	pc := (&Host{}).ProbeContext(t)
	select {
	case <-pc.Context(t).Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the probe context never expired")
	}
}

// The Manager, Engine and ResourceAccess contexts carry the principal in their own field, which is what a
// component reads (rc.Principal): the context value alone leaves the component's view of the caller
// anonymous.
func TestLayerContextsAs_SetThePrincipalField(t *testing.T) {
	h := &Host{}
	p := Principal{Subject: "agent-1", Roles: []string{"agent"}, OnBehalfOf: "alice"}
	pc := h.ProbeContext(t)
	for name, got := range map[string]security.Principal{
		"ManagerContextAs":              h.ManagerContextAs(t, p).Principal,
		"EngineContextAs":               h.EngineContextAs(t, p).Principal,
		"ResourceAccessContextAs":       h.ResourceAccessContextAs(t, p, "k").Principal,
		"probe ManagerContextAs":        pc.ManagerContextAs(t, p).Principal,
		"probe EngineContextAs":         pc.EngineContextAs(t, p).Principal,
		"probe ResourceAccessContextAs": pc.ResourceAccessContextAs(t, p, "k").Principal,
	} {
		if got.Subject != "agent-1" || got.Kind != security.PrincipalApplication || got.Claims[ClaimOnBehalfOf] != "alice" || len(got.Roles) != 1 {
			t.Errorf("%s: Principal = %+v, want agent-1 acting for alice", name, got)
		}
	}
	for name, got := range map[string]security.Principal{
		"ManagerContext":        h.ManagerContext(t).Principal,
		"EngineContext":         h.EngineContext(t).Principal,
		"ResourceAccessContext": h.ResourceAccessContext(t, "k").Principal,
	} {
		if got.Subject != "" {
			t.Errorf("%s: the plain context is anonymous, got %+v", name, got)
		}
	}
}

// A delegated principal is a non-interactive identity carrying whose behalf it acts on.
func TestPrincipalContext_OnBehalfOfIsTheDelegationClaim(t *testing.T) {
	h := &Host{}
	agent, ok := security.PrincipalFrom(h.PrincipalContext(t, Principal{Subject: "agent-1", Roles: []string{"agent"}, OnBehalfOf: "alice"}))
	if !ok {
		t.Fatal("no principal")
	}
	if agent.Kind != security.PrincipalApplication {
		t.Errorf("a delegated principal is an application identity, got %q", agent.Kind)
	}
	if got := agent.Claims[ClaimOnBehalfOf]; got != "alice" {
		t.Errorf("Claims[%q] = %v, want alice", ClaimOnBehalfOf, got)
	}
	human, _ := security.PrincipalFrom(h.PrincipalContext(t, Principal{Subject: "alice"}))
	if human.Kind != security.PrincipalUser {
		t.Errorf("a principal with no delegation is a user, got %q", human.Kind)
	}
	if _, has := human.Claims[ClaimOnBehalfOf]; has {
		t.Error("a principal with no delegation carries no delegation claim")
	}
	// the probe variant carries it too
	pa, _ := security.PrincipalFrom((&Host{}).ProbeContext(t).PrincipalContext(t, Principal{Subject: "agent-1", OnBehalfOf: "alice"}))
	if pa.Claims[ClaimOnBehalfOf] != "alice" {
		t.Errorf("probe principal lost the delegation: %+v", pa)
	}
}
