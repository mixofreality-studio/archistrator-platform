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

	scenariohost "github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
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
	got, browser, err := planPrincipals(dir, "generated/todoWebApp/playwright.config.gen.ts")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Subject != "agent-1" || got[0].OnBehalfOf != "alice" || got[0].Intents[0] != "read" || got[1].Subject != "alice" || browser != "agent-1" {
		t.Errorf("principals = %+v, browser %q; want agent-1 (team-agent) then alice (team-member), browser agent-1", got, browser)
	}
	none, _, err := planPrincipals(dir, "generated/other/playwright.config.gen.ts")
	if err != nil || len(none) != 0 {
		t.Errorf("a surface with no plan has no principals, got %+v, %v", none, err)
	}
}

// acceptanceModule is a module whose project.json holds an acceptance plan, and the path of the
// config an acceptance run is given (its directory holds the generated specs).
func acceptanceModule(t *testing.T, plan string, specs bool) (module, config string) {
	t.Helper()
	module = t.TempDir()
	state := filepath.Join(module, ".aiarch", "state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "project.json"), []byte(`{"phaseArtifacts":{"acceptancePlan":`+plan+`}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	gen := filepath.Join(module, "uitests", "generated", "acceptance")
	if err := os.MkdirAll(gen, 0o755); err != nil {
		t.Fatal(err)
	}
	if specs {
		if err := os.WriteFile(filepath.Join(gen, "work-the-list-P1@todoApi.spec.gen.ts"), []byte("// spec\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return module, filepath.Join(gen, "playwright.config.gen.ts")
}

const acceptancePlanJSON = `{"principals":{
	"team-agent":{"subject":"agent-1","roles":["agent"],"onBehalfOf":"alice","intents":["read"]},
	"team-member":{"subject":"alice","roles":["member"]}},
	"browserActor":"team-member","bindings":[]}`

// An acceptance run launches the app as the acceptance plan's principals with the browser actor
// first (the SPA's own fetches carry no header, so the browser identity is the process principal),
// and AIARCH_DEV_PRINCIPAL is that actor's subject.
func TestWebscenarios_AcceptancePrincipalsBrowserActorFirst(t *testing.T) {
	var launched []string
	var launchedBrowser string
	launch := func(_ context.Context, _ string, principals []scenariohost.Principal, browser string) (stack, error) {
		launched = nil
		for _, p := range principals {
			launched = append(launched, p.Subject)
		}
		launchedBrowser = browser
		return &fakeStack{base: "http://127.0.0.1:1", principal: browser}, nil
	}
	var got command
	d := deps{
		start:     func(ctx context.Context, o options) (stack, error) { return startWith(ctx, o, launch, noHost(t)) },
		exec:      func(_ context.Context, c command, _, _ io.Writer) (int, error) { got = c; return 0, nil },
		inventory: func(context.Context) ([]string, error) { return nil, nil },
	}
	var out, errOut bytes.Buffer

	module, config := acceptanceModule(t, acceptancePlanJSON, true)
	if code := run(t.Context(), []string{"-config", config, "-module", module}, nil, d, &out, &errOut); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, out.String(), errOut.String())
	}
	if !slices.Equal(launched, []string{"alice", "agent-1"}) || launchedBrowser != "alice" {
		t.Errorf("launch saw principals %v and browser %q, want [alice agent-1] and alice", launched, launchedBrowser)
	}
	if v, ok := envValue(got.env, "AIARCH_DEV_PRINCIPAL"); !ok || v != "alice" {
		t.Errorf("AIARCH_DEV_PRINCIPAL = %q (set %v), want alice", v, ok)
	}

	// Web specs and no browserActor: ATP-BROWSER.
	module, config = acceptanceModule(t, `{"principals":{"team-member":{"subject":"alice","roles":["member"]}},"bindings":[]}`, true)
	if code := run(t.Context(), []string{"-config", config, "-module", module}, nil, d, &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "ATP-BROWSER") {
		t.Errorf("web specs with no browserActor: exit %d, stderr %q; want 1 naming ATP-BROWSER", code, errOut.String())
	}
	// No web spec and no browserActor: nothing runs in a browser; the first by name is the process principal.
	module, config = acceptanceModule(t, `{"principals":{"b":{"subject":"bob"},"a":{"subject":"ann"}},"bindings":[]}`, false)
	if code := run(t.Context(), []string{"-config", config, "-module", module}, nil, d, &out, &errOut); code != 0 || !slices.Equal(launched, []string{"ann", "bob"}) || launchedBrowser != "ann" {
		t.Errorf("no web specs: exit %d, principals %v, browser %q; want 0, [ann bob], ann", code, launched, launchedBrowser)
	}
	// A browserActor the plan does not declare is refused by name.
	module, config = acceptanceModule(t, `{"principals":{"a":{"subject":"ann"}},"browserActor":"ghost","bindings":[]}`, true)
	errOut.Reset()
	if code := run(t.Context(), []string{"-config", config, "-module", module}, nil, d, &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "ghost") {
		t.Errorf("undeclared browserActor: exit %d, stderr %q; want 1 naming ghost", code, errOut.String())
	}
}

// noHost is the host server of a run that must not serve one.
func noHost(t *testing.T) serveHostFunc {
	t.Helper()
	return func(context.Context, *scenariohost.App, string) (*scenariohost.AppHost, error) {
		t.Error("a surface that is not an mcpApp must not be given a harness host")
		return nil, errors.New("unexpected host")
	}
}

// appFakeStack is a launched app that can be handed to a host server.
type appFakeStack struct {
	fakeStack
	app   *scenariohost.App
	order *[]string
}

func (h *appFakeStack) App() *scenariohost.App { return h.app }
func (h *appFakeStack) Stop() error {
	*h.order = append(*h.order, "app")
	return h.fakeStack.Stop()
}

// surfaceModule is a module whose System has one Client component per key=surface of clients, and
// the config path of the plan keyed contractKey.
func surfaceModule(t *testing.T, contractKey string, clients map[string]string) (module, config string) {
	t.Helper()
	module = t.TempDir()
	state := filepath.Join(module, ".aiarch", "state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	var comps []string
	for id, surface := range clients {
		comps = append(comps, `{"id":"`+id+`","name":"`+id+`","kind":"client","layer":"client","surface":"`+surface+`"}`)
	}
	project := `{"id":"p","version":1,"slots":{"5":{"kind":5,"status":2,"model":{"components":[` + strings.Join(comps, ",") + `]}}}}`
	if err := os.WriteFile(filepath.Join(state, "project.json"), []byte(project), 0o600); err != nil {
		t.Fatal(err)
	}
	return module, filepath.Join("generated", contractKey, "playwright.config.gen.ts")
}

// An mcpApp surface's specs run inside the harness MCP host: the stack's base URL is the host's, and
// Stop closes the host, then the app. A web surface's config gets no host.
func TestWebscenarios_McpAppConfigServesTheHost(t *testing.T) {
	var order []string
	var hostedFor string
	launch := func(context.Context, string, []scenariohost.Principal, string) (stack, error) {
		return &appFakeStack{fakeStack: fakeStack{base: "http://127.0.0.1:1111"}, app: &scenariohost.App{BaseURL: "http://127.0.0.1:1111"}, order: &order}, nil
	}
	serve := func(_ context.Context, app *scenariohost.App, surface string) (*scenariohost.AppHost, error) {
		hostedFor = surface + "@" + app.BaseURL
		return &scenariohost.AppHost{URL: "http://127.0.0.1:2222", Close: func() error { order = append(order, "host"); return nil }}, nil
	}
	module, config := surfaceModule(t, "todoMcpApp", map[string]string{"todo-mcp-app": "mcpApp", "todo-web-app": "web"})
	st, err := startWith(t.Context(), options{module: module, config: config}, launch, serve)
	if err != nil {
		t.Fatal(err)
	}
	if st.BaseURL() != "http://127.0.0.1:2222" {
		t.Errorf("BaseURL = %q, want the host's origin", st.BaseURL())
	}
	if hostedFor != "todo-mcp-app@http://127.0.0.1:1111" {
		t.Errorf("the host was served for %q, want the surface over the launched app", hostedFor)
	}
	if err := st.Stop(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(order, []string{"host", "app"}) {
		t.Errorf("stop order = %v, want the host closed first, then the app", order)
	}

	// A web surface's config: the app's own origin, no host.
	order = nil
	module, config = surfaceModule(t, "todoWebApp", map[string]string{"todo-mcp-app": "mcpApp", "todo-web-app": "web"})
	st, err = startWith(t.Context(), options{module: module, config: config}, launch, noHost(t))
	if err != nil {
		t.Fatal(err)
	}
	if st.BaseURL() != "http://127.0.0.1:1111" {
		t.Errorf("a web surface's BaseURL = %q, want the app's", st.BaseURL())
	}
}
