package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeStack is a launched app the run stub hands back.
type fakeStack struct {
	base, principal string
	stopped         int
	stopErr         error
}

func (f *fakeStack) BaseURL() string   { return f.base }
func (f *fakeStack) Principal() string { return f.principal }
func (f *fakeStack) Stop() error       { f.stopped++; return f.stopErr }

func envValue(env []string, name string) (string, bool) {
	for i := len(env) - 1; i >= 0; i-- {
		if v, ok := strings.CutPrefix(env[i], name+"="); ok {
			return v, true
		}
	}
	return "", false
}

// The run's flags select what Playwright runs, and what the run passes it: the base URL of the
// launched app, and the caller's own AIARCH_* environment (the null operations of a discrimination
// run, the scenario timeout) untouched.
func TestWebscenarios_ParsesFlagsAndPassesEnv(t *testing.T) {
	st := &fakeStack{base: "http://127.0.0.1:4321", principal: "alice"}
	var got command
	var started options
	d := deps{
		start: func(_ context.Context, o options) (stack, error) { started = o; return st, nil },
		exec: func(_ context.Context, c command, _, _ io.Writer) (int, error) {
			got = c
			return 0, nil
		},
		inventory: func(context.Context) ([]string, error) { return []string{"container:pg"}, nil },
	}
	environ := []string{"PATH=/bin", "AIARCH_NULL_OPS=ListManager.AddTodo", "AIARCH_SCENARIO_TIMEOUT_MS=90000", "PLAYWRIGHT_TEST_BASE_URL=http://stale"}
	var out, errOut bytes.Buffer
	code := run(t.Context(), []string{"-C", "uitests", "-config", "generated/todoWebApp/playwright.config.gen.ts", "-grep", "^add-a-todo-P1$", "-module", "srv", "--", "--workers=1"}, environ, d, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d, want 0\n%s%s", code, out.String(), errOut.String())
	}
	if started.config != "generated/todoWebApp/playwright.config.gen.ts" || started.module != "srv" {
		t.Errorf("start was given %+v", started)
	}
	if got.dir != "uitests" {
		t.Errorf("playwright runs in %q, want the -C directory uitests", got.dir)
	}
	want := []string{"npx", "playwright", "test", "--config", "generated/todoWebApp/playwright.config.gen.ts", "--grep", "^add-a-todo-P1$", "--workers=1"}
	if !slices.Equal(got.argv, want) {
		t.Errorf("argv = %v, want %v", got.argv, want)
	}
	for name, wantVal := range map[string]string{
		"PLAYWRIGHT_TEST_BASE_URL":   "http://127.0.0.1:4321",
		"AIARCH_NULL_OPS":            "ListManager.AddTodo",
		"AIARCH_SCENARIO_TIMEOUT_MS": "90000",
		"AIARCH_DEV_PRINCIPAL":       "alice",
	} {
		if v, ok := envValue(got.env, name); !ok || v != wantVal {
			t.Errorf("env %s = %q (set %v), want %q", name, v, ok, wantVal)
		}
	}
	if st.stopped != 1 {
		t.Errorf("the launched app was stopped %d times, want once", st.stopped)
	}
}

// -project selects a Playwright project (the surface check).
func TestWebscenarios_ProjectFlag(t *testing.T) {
	st := &fakeStack{base: "http://127.0.0.1:1"}
	var got command
	d := deps{
		start:     func(context.Context, options) (stack, error) { return st, nil },
		exec:      func(_ context.Context, c command, _, _ io.Writer) (int, error) { got = c; return 0, nil },
		inventory: func(context.Context) ([]string, error) { return nil, nil },
	}
	var out bytes.Buffer
	if code := run(t.Context(), []string{"-config", "c.ts", "-project", "surface"}, nil, d, &out, &out); code != 0 {
		t.Fatalf("exit %d\n%s", code, out.String())
	}
	if !slices.Contains(got.argv, "--project") || !slices.Contains(got.argv, "surface") {
		t.Errorf("argv = %v, want --project surface", got.argv)
	}
	if _, ok := envValue(got.env, "AIARCH_DEV_PRINCIPAL"); ok {
		t.Errorf("a plan with no principals must not set AIARCH_DEV_PRINCIPAL")
	}
}

// The exit status is Playwright's; a bad invocation is 2 and never starts the app.
func TestWebscenarios_ExitStatus(t *testing.T) {
	st := &fakeStack{base: "http://x"}
	started := 0
	d := deps{
		start: func(context.Context, options) (stack, error) { started++; return st, nil },
		exec: func(context.Context, command, io.Writer, io.Writer) (int, error) {
			return 7, nil
		},
		inventory: func(context.Context) ([]string, error) { return nil, nil },
	}
	var out bytes.Buffer
	if code := run(t.Context(), []string{"-config", "c.ts"}, nil, d, &out, &out); code != 7 {
		t.Errorf("a failing playwright run exits %d, want its own status 7", code)
	}
	if st.stopped != 1 {
		t.Errorf("a failing run must still stop the app, stopped %d", st.stopped)
	}
	started = 0
	if code := run(t.Context(), []string{"-grep", "x"}, nil, d, &out, &out); code != 2 || started != 0 {
		t.Errorf("no -config: exit %d, started %d; want 2 and no start", code, started)
	}
	if code := run(t.Context(), []string{"-nonesuch"}, nil, d, &out, &out); code != 2 {
		t.Errorf("an unknown flag exits %d, want 2", code)
	}
}

// A container or volume the run leaves behind is exit 3, naming it, even when every test passed.
func TestWebscenarios_LeftoverIsExit3(t *testing.T) {
	st := &fakeStack{base: "http://x"}
	calls := 0
	d := deps{
		start: func(context.Context, options) (stack, error) { return st, nil },
		exec: func(context.Context, command, io.Writer, io.Writer) (int, error) {
			return 0, nil
		},
		inventory: func(context.Context) ([]string, error) {
			calls++
			if calls == 1 {
				return []string{"container:keep"}, nil
			}
			return []string{"container:keep", "container:orphan-pg", "volume:orphan-vol"}, nil
		},
	}
	var out, errOut bytes.Buffer
	code := run(t.Context(), []string{"-config", "c.ts"}, nil, d, &out, &errOut)
	if code != 3 {
		t.Errorf("exit %d, want 3", code)
	}
	if text := errOut.String(); !strings.Contains(text, "container:orphan-pg") || !strings.Contains(text, "volume:orphan-vol") || strings.Contains(text, "container:keep") {
		t.Errorf("the leftovers must be named (and only them):\n%s", text)
	}
}

// A launch that fails is reported, and a stop that fails fails a passing run.
func TestWebscenarios_LaunchAndStopFailures(t *testing.T) {
	var out, errOut bytes.Buffer
	inv := func(context.Context) ([]string, error) { return nil, nil }
	pass := func(context.Context, command, io.Writer, io.Writer) (int, error) {
		return 0, nil
	}
	d := deps{start: func(context.Context, options) (stack, error) {
		return nil, errors.New("temporal: dev server did not start")
	}, exec: pass, inventory: inv}
	if code := run(t.Context(), []string{"-config", "c.ts"}, nil, d, &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "dev server did not start") {
		t.Errorf("failed launch: exit %d, stderr %q; want 1 naming the cause", code, errOut.String())
	}
	st := &fakeStack{base: "http://x", stopErr: errors.New("devstack outlived its grace")}
	d.start = func(context.Context, options) (stack, error) { return st, nil }
	errOut.Reset()
	if code := run(t.Context(), []string{"-config", "c.ts"}, nil, d, &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "outlived its grace") {
		t.Errorf("failed stop: exit %d, stderr %q; want 1 naming the cause", code, errOut.String())
	}
}

// The plan's principals of a surface are read from the model: every one allowlisted, in name order,
// the first the process principal.
func TestWebscenarios_SurfacePrincipals(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, ".aiarch", "state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	model := `{"phaseArtifacts":{"testPlan":{"todoWebApp":{"principals":{
		"team-member":{"subject":"alice","roles":["member"]},
		"team-agent":{"subject":"agent-1","roles":["agent"],"onBehalfOf":"alice","intents":["read"]}}}}}}`
	if err := os.WriteFile(filepath.Join(state, "project.json"), []byte(model), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := surfacePrincipals(dir, "generated/todoWebApp/playwright.config.gen.ts")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Subject != "agent-1" || got[0].OnBehalfOf != "alice" || got[0].Intents[0] != "read" || got[1].Subject != "alice" {
		t.Errorf("principals = %+v, want agent-1 (team-agent) then alice (team-member)", got)
	}
	none, err := surfacePrincipals(dir, "generated/other/playwright.config.gen.ts")
	if err != nil || len(none) != 0 {
		t.Errorf("a surface with no plan has no principals, got %+v, %v", none, err)
	}
}
