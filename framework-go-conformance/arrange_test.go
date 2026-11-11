//go:build conformance

package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The arrange endpoint of web scenarios (Plan 3 Task 3.5), proved on the built fixture: a browser
// scenario that arranges failNext reaches the booted server through /__scenariohost, and the Manager
// the server composed fails the next call of the named operation.

const (
	// arrangeScenario is the scenario of the web plan the proof arranges a fault before.
	arrangeScenario = "add-a-todo-P1"
	// webSurface is the todomvc fixture's browser surface (its serviceContracts key).
	webSurface = "todoWebApp"
)

// uitestsPackage seats the Playwright project the generated specs run in: the app's own `uitests`
// directory, which the generators write specs into and an app (not the scaffold) installs Playwright in.
const uitestsPackage = `{
  "name": "conformance-uitests",
  "private": true,
  "version": "0.0.0",
  "type": "module",
  "devDependencies": {
    "@playwright/test": "1.60.0"
  }
}
`

// run runs a command in dir under ctx and returns its combined output.
func run(t *testing.T, ctx context.Context, dir string, name string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- fixed argv of the proof
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.String(), err
}

// seatUITests installs Playwright and its browser in the built app's uitests directory.
func seatUITests(t *testing.T, ctx context.Context, dir string) {
	t.Helper()
	ui := filepath.Join(dir, "uitests")
	if err := os.MkdirAll(ui, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ui, "package.json"), []byte(uitestsPackage), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, argv := range [][]string{
		{"npm", "install", "--no-audit", "--no-fund"},
		{"npx", "playwright", "install", "chromium"},
	} {
		if out, err := run(t, ctx, ui, argv[0], argv[1:]...); err != nil {
			t.Fatalf("%s: %v\n%s", strings.Join(argv, " "), err, out)
		}
	}
}

// webscenarios runs the built app's generated scenario under the scenario host: the exit status and
// the combined output of `go tool webscenarios -grep <the scenario's title>`. The fixture's SPA is the
// generated scaffold (the RUN-021 harvest's hand-written bodies are the server's), which cannot satisfy
// the surface check the scenarios project depends on, so the run is made without its dependencies
// (--no-deps): the round trip under proof is the arrange endpoint's, not the surface's.
func webscenarios(t *testing.T, ctx context.Context, dir, scenario string) (int, string) {
	t.Helper()
	grep := `(?:^|\s)` + scenario + ` — `
	out, err := run(t, ctx, dir, "go", "tool", "webscenarios", "-C", "uitests",
		"-config", "generated/"+webSurface+"/playwright.config.gen.ts", "-grep", grep, "--", "--no-deps")
	if err == nil {
		return 0, out
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), out
	}
	t.Fatalf("go tool webscenarios: %v\n%s", err, out)
	return -1, out
}

// literal is a literal TestArg of the model: {"name", "value": {"kind":"literal","model":{"json": v}}}.
func literal(name string, v any) map[string]any {
	return map[string]any{"name": name, "schemaRef": "", "value": map[string]any{"kind": "literal", "model": map[string]any{"json": v}}}
}

// addTodoCall is the arrange contract call of listManager.AddTodo, expecting want.
func addTodoCall(want map[string]any) map[string]any {
	return map[string]any{"kind": "contractCall", "model": map[string]any{
		"component": "listManager", "operation": "AddTodo",
		"inputs": []any{literal("owner", "alice"), literal("wording", "Buy milk"), literal("requestId", "arrange-1")},
		"expect": want,
	}}
}

// addedExpect is the AddTodo outcome of a call the Manager served; refusedExpect that of a call it
// refused as Infrastructure.
var (
	addedExpect = map[string]any{
		"result": `{"outcome":"added","todo":{"title":"Buy milk","completed":false}}`, "errorExpected": false, "errorCode": "",
	}
	refusedExpect = map[string]any{"result": "", "errorExpected": true, "errorCode": "Infrastructure"}
)

// withArrange is the fixture's project.json with the arrange of arrangeScenario replaced, and its
// steps replaced by one the SPA cannot fail: open the list's route and see the URL. The scenario then
// passes or fails by its arrange alone, whatever the generated SPA shows.
func withArrange(t *testing.T, raw []byte, arrange []any) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	plan := dig(t, doc, "phaseArtifacts", "testPlan", webSurface)
	bindings, _ := plan["bindings"].([]any)
	for _, b := range bindings {
		bm, _ := b.(map[string]any)
		if bm["scenario"] != arrangeScenario {
			continue
		}
		bm["arrange"] = arrange
		bm["steps"] = []any{map[string]any{
			"seq": 1, "operation": "OpenList", "probes": []any{},
			"inputs": []any{literal("action", map[string]any{"kind": "navigate", "model": map[string]any{"route": "#/"}})},
			"expect": map[string]any{"result": `[{"kind":"url","model":{"route":"#/"}}]`, "errorExpected": false, "errorCode": ""},
		}}
		out, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	t.Fatalf("the fixture's %s plan has no %s binding", webSurface, arrangeScenario)
	return nil
}

// failNextAddTodo is the arrange that makes the next listManager.AddTodo fail as Infrastructure.
var failNextAddTodo = map[string]any{"kind": "failNext", "model": map[string]any{
	"component": "listManager", "operation": "AddTodo", "errorCode": "Infrastructure",
}}

// regenerate writes the one-off model over the built app's state and regenerates its tests.
func regenerate(t *testing.T, ctx context.Context, dir string, model []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".aiarch", "state", "project.json"), model, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := run(t, ctx, dir, "go", "tool", "testgen", "-project", ".aiarch/state/project.json",
		"-root", ".", "-uitests", "uitests/generated"); err != nil {
		t.Fatalf("testgen over the one-off binding: %v\n%s", err, out)
	}
}

// A browser scenario that arranges failNext{listManager, AddTodo, Infrastructure} reaches the booted
// server through /__scenariohost: the Manager the server composed refuses the next AddTodo, and
// refuses it as Infrastructure. The one-off binding (add-a-todo's slot, whose steps only open the
// list's route, so that the generated SPA cannot decide the verdict) is run three ways:
//
//   - control: AddTodo is arranged with no fault and is served: the scenario PASSES;
//   - fault, refusal expected: the same call after failNext is refused as Infrastructure, which the
//     arrange call asserts: the scenario PASSES only if the fault reached the server;
//   - fault, service expected: the same call after failNext, still expecting the service: the
//     scenario FAILS at the arrange, with the server's Infrastructure refusal in the output.
//
// Each run goes through the scenario host, which gives back everything it launched (exit 3 names a
// leftover container or volume).
func TestPlaywright_ArrangeFailNextRoundTrip(t *testing.T) {
	raw, err := Fixture("todomvc")
	if err != nil {
		t.Fatal(err)
	}
	impl, err := FixtureImpl("todomvc")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()
	if _, err := RunChainOn(ctx, "todomvc", raw, dir, WithImpl(impl)); err != nil {
		t.Fatalf("RunChain: %v", err)
	}
	seatUITests(t, ctx, dir)
	before, err := DockerInventory(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("control: the call is served", func(t *testing.T) {
		regenerate(t, ctx, dir, withArrange(t, raw, []any{addTodoCall(addedExpect)}))
		if code, out := webscenarios(t, ctx, dir, arrangeScenario); code != 0 {
			t.Fatalf("the control exited %d, want 0:\n%s", code, out)
		}
	})

	t.Run("fault: the call is refused as Infrastructure", func(t *testing.T) {
		regenerate(t, ctx, dir, withArrange(t, raw, []any{failNextAddTodo, addTodoCall(refusedExpect)}))
		spec, err := os.ReadFile(filepath.Join(dir, "uitests", "generated", webSurface, arrangeScenario+".spec.gen.ts")) // #nosec G304 -- the tree this test built
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(spec), "kind: 'failNext', component: 'listManager', operation: 'AddTodo', errorCode: 'Infrastructure'") {
			t.Fatalf("the regenerated spec does not arrange the fault:\n%s", spec)
		}
		if code, out := webscenarios(t, ctx, dir, arrangeScenario); code != 0 {
			t.Fatalf("the faulted call was not refused as Infrastructure (exit %d): the fault did not reach the server\n%s", code, out)
		}
	})

	t.Run("fault: the scenario fails where the call was expected to be served", func(t *testing.T) {
		regenerate(t, ctx, dir, withArrange(t, raw, []any{failNextAddTodo, addTodoCall(addedExpect)}))
		code, out := webscenarios(t, ctx, dir, arrangeScenario)
		switch {
		case code == 0:
			t.Fatalf("the scenario passed with its AddTodo refused: the fault did not reach the booted server\n%s", out)
		case code == 3:
			t.Fatalf("the scenario host left something behind:\n%s", out)
		case !strings.Contains(out, "arrange POST") || !strings.Contains(out, "infrastructure"):
			t.Errorf("the scenario failed (exit %d), but not at the arranged call with the Infrastructure refusal visible:\n%s", code, out)
		}
	})

	after, err := DockerInventory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if left := DockerLeftovers(ctx, before, after); len(left) > 0 {
		t.Errorf("the runs left docker resources behind: %v", left)
	}
}
