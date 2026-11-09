package scenariohost

import (
	"slices"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
)

func TestPrincipalContextCarriesThePrincipal(t *testing.T) {
	h := Start(t)
	p := Principal{Subject: "team-member", Roles: []string{"member", "editor"}}

	ctx := h.PrincipalContext(t, p)
	got, ok := security.PrincipalFrom(ctx)
	if !ok {
		t.Fatal("PrincipalContext carries no principal")
	}
	if got.Subject != "team-member" || !slices.Equal(got.Roles, p.Roles) {
		t.Errorf("principal = %+v, want subject %q roles %v", got, p.Subject, p.Roles)
	}
	if ctx.Err() != nil {
		t.Errorf("a fresh context must be live, got %v", ctx.Err())
	}
	if _, ok := security.PrincipalFrom(h.Context(t)); ok {
		t.Error("the plain test context must carry no principal")
	}
}

func TestLayerContextsAsCarryThePrincipal(t *testing.T) {
	h := Start(t)
	p := Principal{Subject: "owner"}

	ra := h.ResourceAccessContextAs(t, p, "probe-1")
	if got, ok := security.PrincipalFrom(ra.Context); !ok || got.Subject != "owner" {
		t.Errorf("ResourceAccessContextAs principal = %+v, %v", got, ok)
	}
	if want := h.ResourceAccessContext(t, "probe-1").IdempotencyKey; ra.IdempotencyKey != want {
		t.Errorf("the principal variant must keep the plain variant's key derivation: %q vs %q", ra.IdempotencyKey, want)
	}
	if e := h.EngineContextAs(t, p); e.Context == nil {
		t.Error("EngineContextAs has no context")
	} else if got, ok := security.PrincipalFrom(e.Context); !ok || got.Subject != "owner" {
		t.Errorf("EngineContextAs principal = %+v, %v", got, ok)
	}
	if m := h.ManagerContextAs(t, p); m.Context == nil {
		t.Error("ManagerContextAs has no context")
	} else if got, ok := security.PrincipalFrom(m.Context); !ok || got.Subject != "owner" {
		t.Errorf("ManagerContextAs principal = %+v, %v", got, ok)
	}
}

func TestPrincipalContext_DelegationClaimShape(t *testing.T) {
	h := Start(t)
	p := Principal{Subject: "agent-1", Roles: []string{"agent"}, OnBehalfOf: "alice", Intents: []string{"read", "add"}}
	got, _ := security.PrincipalFrom(h.PrincipalContext(t, p))
	if got.Kind != security.PrincipalApplication {
		t.Errorf("kind = %q, want application", got.Kind)
	}
	if _, stale := got.Claims["onBehalfOf"]; stale {
		t.Error(`Claims["onBehalfOf"] must no longer be written`)
	}
	raw, ok := got.Claims[ClaimDelegations].([]any)
	if !ok || len(raw) != 1 {
		t.Fatalf("Claims[%q] = %#v, want one grant", ClaimDelegations, got.Claims[ClaimDelegations])
	}
	grant := raw[0].(map[string]any)
	if grant["grantor"] != "alice" {
		t.Errorf("grantor = %v", grant["grantor"])
	}
	if intents, _ := grant["intents"].([]any); len(intents) != 2 || intents[0] != "read" || intents[1] != "add" {
		t.Errorf("intents = %v", grant["intents"])
	}
	ds := DelegationsOf(got)
	if len(ds) != 1 || ds[0].Grantor != "alice" || !slices.Equal(ds[0].Intents, []string{"read", "add"}) {
		t.Errorf("DelegationsOf = %+v", ds)
	}
	human, has := security.PrincipalFrom(h.PrincipalContext(t, Principal{Subject: "alice"}))
	if !has {
		t.Fatal("human principal lost")
	}
	if human.Claims != nil {
		t.Errorf("a human carries no claims, got %v", human.Claims)
	}
}
