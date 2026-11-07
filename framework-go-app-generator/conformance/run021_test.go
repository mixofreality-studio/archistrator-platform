//go:build conformance

package conformance

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/testgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
)

// run021Fixture is the todomvc fixture: the RUN-021 harvest migrated to binding model v2.
const run021Fixture = "todomvc"

// TestConformance_RUN021Proof replays RUN-021 through the new chain (FPC plan 2, Task 2.13).
//
// The harvest passed every gate and built an app whose reword-an-abandoned-todo path had no
// test, whose delete test asserted what was already true before the click, and whose checkbox
// test waited for the server. Each subtest pins one of those defects shut:
//
//	derive        reword-a-todo-P1 reaches todo-web-app and the derivation reports no Problem
//	methodcheck   the fixture, with no waiver, has zero Error findings
//	testgen       the generated mark-a-todo spec asserts the checked box immediately
//	chain         the fixture scaffolds, generates and builds (needs the released platform pins)
//	discriminate  list-manager has zero null-subject survivors (needs the chain)
//	vacuous       the old delete binding, left in place, is a survivor (needs the chain)
func TestConformance_RUN021Proof(t *testing.T) {
	raw, err := Fixture(run021Fixture)
	if err != nil {
		t.Fatalf("Fixture: %v", err)
	}

	t.Run("derive", func(t *testing.T) { run021Derive(t, raw) })
	t.Run("methodcheck", func(t *testing.T) { run021Methodcheck(t, raw) })
	t.Run("testgen", func(t *testing.T) { run021Testgen(t, raw) })

	var built string
	// The built tree belongs to the parent test: the subtests after the chain read it, and a
	// subtest's own TempDir is gone when that subtest returns.
	chainDir := t.TempDir()
	t.Run("chain", func(t *testing.T) {
		dir := chainDir
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
		defer cancel()
		if _, err := RunChainOn(ctx, run021Fixture, raw, dir); err != nil {
			t.Fatalf("RunChain: %v", err)
		}
		built = dir
	})
	if built == "" {
		return
	}
	t.Run("vet", func(t *testing.T) { run021Vet(t, built) })
	t.Run("discriminate", func(t *testing.T) {
		for _, s := range discriminate(t, built) {
			if s.component == "listManager" {
				t.Errorf("list-manager scenario %s op %s survives the null subject", s.scenario, s.op)
			}
		}
	})
	t.Run("vacuous", func(t *testing.T) {
		dir := t.TempDir()
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
		defer cancel()
		if _, err := RunChainOn(ctx, run021Fixture, vacuousDiscard(t, raw), dir); err != nil {
			t.Fatalf("RunChain (vacuous delete): %v", err)
		}
		for _, s := range discriminate(t, dir) {
			if s.component == "listManager" && s.scenario == "discard-a-todo-P1" {
				return
			}
		}
		t.Error("the old, vacuous discard-a-todo-P1 binding passes a null subject yet was not listed as a survivor")
	})
}

func run021Derive(t *testing.T, raw []byte) {
	p, ok, err := methodcheck.DecodeProject(raw)
	if err != nil || !ok {
		t.Fatalf("DecodeProject: ok=%v err=%v", ok, err)
	}
	in, err := methodcheck.ScenarioInput(p)
	if err != nil {
		t.Fatalf("ScenarioInput: %v", err)
	}
	rep, err := scenario.DeriveReport(in)
	if err != nil {
		t.Fatalf("DeriveReport: %v", err)
	}
	for _, pr := range rep.Problems {
		t.Errorf("derivation problem %s (%s): %s", pr.Code, pr.Scenario, pr.Message)
	}
	want := scenario.Normalize("todo-web-app")
	for _, sc := range rep.Scenarios {
		if sc.ID != "reword-a-todo-P1" {
			continue
		}
		for _, st := range sc.Stimuli {
			if st.Input.To == want && st.Input.Kind != "call" {
				return
			}
		}
		t.Fatalf("reword-a-todo-P1 has no actor stimulus into todo-web-app: %+v", sc.Stimuli)
	}
	t.Fatal("reword-a-todo-P1 is not derived: the abandon path is dropped again")
}

func run021Methodcheck(t *testing.T, raw []byte) {
	findings, err := methodcheck.ValidateProjectJSON(raw)
	if err != nil {
		t.Fatalf("ValidateProjectJSON: %v", err)
	}
	for _, f := range findings {
		if f.Severity == methodcheck.SeverityError {
			t.Errorf("error finding %s: %s", f.RuleID, f.Message)
		}
	}
}

func run021Testgen(t *testing.T, raw []byte) {
	out, err := testgen.Generate(raw, testgen.Config{ModulePath: "example.com/conformance/" + run021Fixture})
	if err != nil {
		t.Fatalf("testgen.Generate: %v", err)
	}
	for _, w := range out.Warnings {
		t.Logf("testgen warning: %s", w)
	}
	// The generated tree is written to a temp dir so the file the proof reads is the file
	// an app would hold.
	dir := t.TempDir()
	var toggle string
	for path, content := range out.Generated {
		dest := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, content, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(path, "/mark-a-todo-completed-or-active-P1.spec.gen.ts") {
			toggle = dest
		}
	}
	if toggle == "" {
		t.Fatalf("no generated mark-a-todo-completed-or-active-P1 spec among %d files", len(out.Generated))
	}
	spec, err := os.ReadFile(toggle) // #nosec G304 -- written above
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(spec), "toBeChecked(immediate)") {
		t.Errorf("%s does not assert the checkbox immediately (toBeChecked(immediate)):\n%s", toggle, spec)
	}
}

// run021Vet vets the built app, test files included, in the scaffolded module whose platform
// requirements are replaced by this checkout. The generated client_scenarios.gen_test.go calls
// scenariocompose.NewTodoApiList / NewTodoMcpServerList: a compose.gen.go that lacks either
// constructor (the generated-only client packages dropped as unbuilt) fails here.
func run021Vet(t *testing.T, dir string) {
	t.Helper()
	compose, err := os.ReadFile(filepath.Join(dir, "internal", "scenariocompose", "compose.gen.go")) // #nosec G304 -- the chain's own output
	if err != nil {
		t.Fatalf("no scenario subject: %v", err)
	}
	for _, ctor := range []string{"func NewTodoApiList(", "func NewTodoMcpServerList("} {
		if !strings.Contains(string(compose), ctor) {
			t.Errorf("compose.gen.go lacks %s", ctor)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "vet", "./...") // #nosec G204 -- fixed argv
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go vet ./... over the generated tests: %v\n%s", err, out)
	}
}

// survivor is one TP-DISCRIMINATE line.
type survivor struct{ component, scenario, op string }

// discriminate runs `testgen -discriminate` in the built app dir and returns its survivors. Exit 1
// with TP-DISCRIMINATE lines is the run's answer, not a failure of the runner.
func discriminate(t *testing.T, dir string) []survivor {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "tool", "testgen", "-project", ".aiarch/state/project.json", // #nosec G204 -- fixed argv
		"-root", ".", "-uitests", "uitests/generated", "-discriminate")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.CombinedOutput()
	var survivors []survivor
	for _, line := range strings.Split(string(out), "\n") {
		if f := strings.Fields(line); len(f) == 4 && f[0] == "TP-DISCRIMINATE" {
			survivors = append(survivors, survivor{f[1], f[2], f[3]})
		}
	}
	if err != nil && len(survivors) == 0 {
		t.Fatalf("testgen -discriminate: %v\n%s", err, out)
	}
	return survivors
}

// vacuousDiscard is raw with listManager's discard-a-todo-P1 binding put back to what RUN-021's
// delete asserted: nothing the click changed. No arrange builds a list to discard from, no probe
// reads the list, and the only expectation is a count that is zero on an empty list and on a null
// subject alike. The real subject passes it, and so does the null one: a survivor.
func vacuousDiscard(t *testing.T, raw []byte) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	plan := dig(t, doc, "phaseArtifacts", "testPlan", "listManager")
	bindings, _ := plan["bindings"].([]any)
	for i, b := range bindings {
		bm, _ := b.(map[string]any)
		if bm["scenario"] != "discard-a-todo-P1" {
			continue
		}
		bindings[i] = map[string]any{
			"scenario": "discard-a-todo-P1",
			"arrange":  []any{},
			"steps": []any{map[string]any{
				"seq": 1, "operation": "DiscardTodo",
				"inputs": []any{
					literalArg("owner", "alice"),
					literalArg("todoId", "t1"),
				},
				"expect": map[string]any{"result": `{"itemsLeft":0}`, "errorExpected": false, "errorCode": ""},
				"probes": []any{},
			}},
		}
		out, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	t.Fatal("listManager has no discard-a-todo-P1 binding to make vacuous")
	return nil
}

func literalArg(name string, v any) map[string]any {
	return map[string]any{
		"name":      name,
		"schemaRef": "",
		"value":     map[string]any{"kind": "literal", "model": map[string]any{"json": v}},
	}
}

func dig(t *testing.T, m map[string]any, path ...string) map[string]any {
	t.Helper()
	for _, k := range path {
		next, ok := m[k].(map[string]any)
		if !ok {
			t.Fatalf("fixture has no %s", strings.Join(path, "."))
		}
		m = next
	}
	return m
}
