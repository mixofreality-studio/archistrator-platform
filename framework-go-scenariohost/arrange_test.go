package scenariohost

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// The clock is the host's, not the process's: a scenario that advances it
// moves every generated RA context that reads Host.Now, and the advance is
// undone when the scenario ends so the next scenario starts at real time.
func TestAdvanceClock_ShiftsRAClock(t *testing.T) {
	h := &Host{}
	const d = 36 * time.Hour

	t.Run("scenario", func(t *testing.T) {
		before := time.Now()
		h.AdvanceClock(t, d)
		got := h.Clock().Now()
		if shift := got.Sub(before); shift < d || shift > d+time.Minute {
			t.Fatalf("clock shifted by %v, want about %v", shift, d)
		}
		h.AdvanceClock(t, time.Hour)
		if shift := h.Now().Sub(before); shift < d+time.Hour {
			t.Fatalf("a second advance must accumulate, shifted by %v", shift)
		}
	})

	if shift := time.Until(h.Now()); shift > time.Minute || shift < -time.Minute {
		t.Errorf("the advance outlived its scenario: clock is %v from real time", shift)
	}
}

func TestAdvanceClock_RefusesNegative(t *testing.T) {
	if os.Getenv("AIARCH_SUBPROCESS") == "advance-negative" {
		(&Host{}).AdvanceClock(t, -time.Second)
		return
	}
	out := runSubprocess(t, "TestAdvanceClock_RefusesNegative", "advance-negative")
	if !strings.Contains(out, "negative") {
		t.Errorf("a negative advance must fail the test naming it, got:\n%s", out)
	}
}

func TestFailNext_OneShot(t *testing.T) {
	h := &Host{}

	t.Run("scenario", func(t *testing.T) {
		h.FailNext(t, "todoManager", "AddTodo", "unavailable")
		if code, ok := h.TakeFault("todoManager", "AddTodo"); !ok || code != "unavailable" {
			t.Fatalf("first call: TakeFault = %q, %v; want unavailable, true", code, ok)
		}
		if code, ok := h.TakeFault("todoManager", "AddTodo"); ok {
			t.Fatalf("a fault is one-shot, but the second call also got %q", code)
		}
	})

	t.Run("only the named operation", func(t *testing.T) {
		h.FailNext(t, "todoManager", "AddTodo", "unavailable")
		if _, ok := h.TakeFault("todoManager", "ListTodos"); ok {
			t.Error("a fault registered for AddTodo fired on ListTodos")
		}
		if _, ok := h.TakeFault("otherManager", "AddTodo"); ok {
			t.Error("a fault registered for todoManager fired on otherManager")
		}
	})

	t.Run("queued faults fire in order", func(t *testing.T) {
		h.FailNext(t, "c", "o", "first")
		h.FailNext(t, "c", "o", "second")
		for _, want := range []string{"first", "second"} {
			if code, ok := h.TakeFault("c", "o"); !ok || code != want {
				t.Fatalf("TakeFault = %q, %v; want %q", code, ok, want)
			}
		}
	})

	// An unconsumed fault must not leak into the next scenario.
	if code, ok := h.TakeFault("todoManager", "AddTodo"); ok {
		t.Errorf("an unconsumed fault outlived its scenario: %q", code)
	}
}

func TestFailNext_RefusesIncomplete(t *testing.T) {
	if os.Getenv("AIARCH_SUBPROCESS") == "failnext-empty" {
		(&Host{}).FailNext(t, "c", "", "boom")
		return
	}
	out := runSubprocess(t, "TestFailNext_RefusesIncomplete", "failnext-empty")
	if !strings.Contains(out, "FailNext") {
		t.Errorf("an empty operation must fail the test, got:\n%s", out)
	}
}

func TestSeedResource_UnknownKindFatal(t *testing.T) {
	if os.Getenv("AIARCH_SUBPROCESS") == "seed-unknown" {
		(&Host{}).SeedResource(t, "redis", json.RawMessage(`{}`))
		return
	}
	out := runSubprocess(t, "TestSeedResource_UnknownKindFatal", "seed-unknown")
	if !strings.Contains(out, `unknown resource kind "redis"`) {
		t.Errorf("an unknown kind must fail the test naming it, got:\n%s", out)
	}
}

func TestSeedResource_KindsAndFixtureErrors(t *testing.T) {
	h := &Host{}
	cases := []struct {
		name, resource, fixture, want string
	}{
		{"unknown kind", "redis", `{}`, `unknown resource kind "redis"`},
		{"empty kind", "", `{}`, `unknown resource kind ""`},
		{"postgres without a database", "postgres", `{"sql":"select 1"}`, "no Postgres"},
		{"postgres without sql", "postgres", `{}`, "sql"},
		{"postgres unknown field", "postgres", `{"sql":"select 1","extra":1}`, "extra"},
		{"github without the double", "github", `{"owner":"acme","name":"widget"}`, "no GitHub"},
		{"gitRepo without the repo", "gitRepo", `{"files":{"a.txt":"x"}}`, "no local repo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := h.seedResource(context.Background(), tc.resource, json.RawMessage(tc.fixture))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("seedResource(%q, %s) = %v, want an error containing %q", tc.resource, tc.fixture, err, tc.want)
			}
		})
	}
}

func TestSeedResource_PostgresExecsFixtureSQL(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the Postgres container")
	}
	h := Start(t)
	h.SeedResource(t, "postgres", json.RawMessage(`{"sql":"create table if not exists arrange_seed (id int primary key, v text); insert into arrange_seed values (1, 'one') on conflict do nothing"}`))

	conn, err := pgx.Connect(t.Context(), h.PostgresURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(context.Background()) }()
	var v string
	if err := conn.QueryRow(t.Context(), "select v from arrange_seed where id = 1").Scan(&v); err != nil || v != "one" {
		t.Fatalf("seeded row = %q, %v; want one", v, err)
	}
}

func TestSeedResource_GitHubSeedsTheFake(t *testing.T) {
	h := Start(t)
	h.SeedResource(t, "github", json.RawMessage(`{"owner":"acme","name":"widget","description":"d","topics":["t"],"private":true,"files":{"README.md":"# hi"}}`))

	b, ok := h.GitHub.RepoFile("acme", "widget", "README.md")
	if !ok || string(b) != "# hi" {
		t.Fatalf("RepoFile = %q, %v; want \"# hi\", true", b, ok)
	}
	resp, err := http.Get(h.GitHub.BaseURL() + "/repos/acme/widget")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /repos/acme/widget = %d, want 200", resp.StatusCode)
	}
	var repo struct {
		Description string   `json:"description"`
		Topics      []string `json:"topics"`
		Private     bool     `json:"private"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&repo); err != nil {
		t.Fatal(err)
	}
	if repo.Description != "d" || len(repo.Topics) != 1 || repo.Topics[0] != "t" || !repo.Private {
		t.Fatalf("repo = %+v; want description d, topics [t], private true", repo)
	}
}

func TestSeedResource_GitRepoCommitsFiles(t *testing.T) {
	h := Start(t)
	h.SeedResource(t, "gitRepo", json.RawMessage(`{"branch":"main","message":"arrange","files":{"docs/a.txt":"hello"}}`))

	out, err := exec.Command("git", "-C", h.Repo.Dir, "show", "main:docs/a.txt").CombinedOutput()
	if err != nil || string(out) != "hello" {
		t.Fatalf("show main:docs/a.txt = %q, %v; want hello", out, err)
	}
}

// runSubprocess re-runs one test of this binary with AIARCH_SUBPROCESS=mode, to
// observe a t.Fatalf that would otherwise fail the observing test.
func runSubprocess(t *testing.T, test, mode string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^"+test+"$", "-test.v", "-test.short")
	cmd.Env = append(os.Environ(), "AIARCH_SUBPROCESS="+mode)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("subprocess %s passed, want a failure:\n%s", mode, out)
	}
	return string(out)
}
