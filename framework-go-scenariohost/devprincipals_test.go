package scenariohost

import (
	"encoding/json"
	"testing"

	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
)

// devPrincipalJSON is one entry of the <PREFIX>_DEV_PRINCIPALS list as the launched app reads it
// (configgen's LocalDevPrincipals).
type devPrincipalJSON struct {
	Subject string         `json:"subject"`
	Kind    string         `json:"kind"`
	Roles   []string       `json:"roles"`
	Claims  map[string]any `json:"claims"`
}

// LaunchDevstack allowlists every principal it is given (Plan 3 Task 3.3): the launched app may
// serve a request as any of them, each as the framework principal a call in-process would carry,
// the delegation claim included, so a client acting as an agent reaches the Manager as one over the
// wire. The first stays the process principal.
func TestLaunchDevstack_AllowlistsEveryPrincipal(t *testing.T) {
	h := Start(t)
	app := h.LaunchDevstack(t, fakeApp(t),
		Principal{Subject: "alice", Roles: []string{"member"}},
		Principal{Subject: "bot", OnBehalfOf: "alice", Intents: []string{"read", "add"}},
	)
	raw := envOf(app.Log(), "FAKE_DEV_PRINCIPALS")
	if raw == "" {
		t.Fatalf("FAKE_DEV_PRINCIPALS is not set:\n%s", app.Log())
	}
	var got []devPrincipalJSON
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("FAKE_DEV_PRINCIPALS is not the list the Config reads: %v\n%s", err, raw)
	}
	if len(got) != 2 || got[0].Subject != "alice" || got[1].Subject != "bot" {
		t.Fatalf("principals = %+v, want alice then bot", got)
	}
	if got[0].Kind != string(security.PrincipalUser) || len(got[0].Roles) != 1 || got[0].Roles[0] != "member" {
		t.Errorf("alice = %+v, want a user with the member role", got[0])
	}
	if got[1].Kind != string(security.PrincipalApplication) {
		t.Errorf("bot kind = %q, want application (a delegated agent)", got[1].Kind)
	}
	sp := security.Principal{Claims: got[1].Claims}
	ds := DelegationsOf(sp)
	if len(ds) != 1 || ds[0].Grantor != "alice" || len(ds[0].Intents) != 2 || ds[0].Intents[1] != "add" {
		t.Errorf("bot's delegation claim = %+v, want alice's grant of read and add", ds)
	}
	if v := envOf(app.Log(), "FAKE_DEV_PRINCIPAL"); v != "alice" {
		t.Errorf("FAKE_DEV_PRINCIPAL = %q, want the first principal", v)
	}
}

// With no principal there is no allowlist: the process principal alone serves every request.
func TestLaunchDevstack_NoPrincipalsNoAllowlist(t *testing.T) {
	h := Start(t)
	app := h.LaunchDevstack(t, fakeApp(t))
	if v := envOf(app.Log(), "FAKE_DEV_PRINCIPALS"); v != "" {
		t.Errorf("FAKE_DEV_PRINCIPALS = %q, want it unset", v)
	}
}

// Components a scenario builds in-process after the launch (an arrange call, a probe on another
// component) read the state the launched app wrote, so the host's answer for the Postgres URL is the
// launch's database from then on.
func TestLaunchDevstack_InProcessInputsFollowTheLaunch(t *testing.T) {
	h := Start(t)
	if h.PostgresURL == "" {
		t.Skip("postgres is not booted under -short")
	}
	if got := h.Input(t, "pg", projectmodel.SubstratePostgres, "URL"); got != h.PostgresURL {
		t.Fatalf("before a launch the host answers its own URL, got %q", got)
	}
	app := h.LaunchDevstack(t, fakeApp(t))
	launched := envOf(app.Log(), "FAKE_POSTGRES_URL")
	if launched == "" {
		t.Fatalf("no FAKE_POSTGRES_URL:\n%s", app.Log())
	}
	if got := h.Input(t, "pg", projectmodel.SubstratePostgres, "URL"); got != launched {
		t.Errorf("after a launch the host answers the launch's database %q, got %q", launched, got)
	}
}
