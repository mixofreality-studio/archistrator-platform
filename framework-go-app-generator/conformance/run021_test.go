//go:build conformance

package conformance

import (
	"context"
	"encoding/json"
	"fmt"
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
//	discriminate  on the null build no scenario survives, and exactly the scenarios that assert
//	              behavior are TP-BROKEN (the null build cannot satisfy them; they are never nulled)
//	vacuous       the old delete binding, left in place, is a survivor (needs the chain)
//	discriminate_impl
//	              over the RUN-021 harvest's hand-written bodies (testdata/todomvc/impl, overlaid
//	              in place of null.gen.go and the nullgen hooks.go) every generated Go scenario
//	              passes against its real subject (broken == 0) and none survives its stimulus
//	              being nulled (survivors == 0): the proof that the generated tests discriminate
//	              a real implementation, which the null build alone cannot give
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
		got := discriminate(t, built)
		for _, s := range got.list {
			t.Errorf("%s scenario %s op %s survives the null subject", s.component, s.scenario, s.op)
		}
		if got.survivors != 0 {
			t.Errorf("null build: %d survivor(s), want 0", got.survivors)
		}
		if want := goScenarios(t, built); got.scenarios != want {
			t.Errorf("null build: %d scenario(s) discriminated, but %d TestScenario_ functions were generated for Go components", got.scenarios, want)
		}
		// The null build is all-null: its "real subject" returns zero results, so every scenario
		// that asserts behavior fails there (TP-BROKEN) and is not nulled. Only the scenarios whose
		// assertions the zero results satisfy reach the null runs; pinning the count makes a change
		// in what the generators emit (or in the fixture) show up here.
		if got.broken != nullBuildBroken {
			t.Errorf("null build: %d broken scenario(s), want %d", got.broken, nullBuildBroken)
		}
	})
	t.Run("vacuous", func(t *testing.T) {
		dir := t.TempDir()
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
		defer cancel()
		if _, err := RunChainOn(ctx, run021Fixture, vacuousDiscard(t, raw), dir); err != nil {
			t.Fatalf("RunChain (vacuous delete): %v", err)
		}
		for _, s := range discriminate(t, dir).list {
			if s.component == "listManager" && s.scenario == "discard-a-todo-P1" {
				return
			}
		}
		t.Error("the old, vacuous discard-a-todo-P1 binding passes a null subject yet was not listed as a survivor")
	})
	t.Run("discriminate_impl", func(t *testing.T) { run021DiscriminateImpl(t, raw) })
}

// nullBuildBroken is how many Go scenarios of the todomvc fixture fail against the null build's own
// (all-null) real subject.
const nullBuildBroken = 78

// run021DiscriminateImpl builds the fixture with the harvest's hand-written component bodies in
// place of the null implementations and requires the discrimination to be clean over them: every
// generated Go scenario passes against the real subject, and nulling any stimulus operation fails it.
func run021DiscriminateImpl(t *testing.T, raw []byte) {
	impl, err := FixtureImpl(run021Fixture)
	if err != nil {
		t.Fatalf("FixtureImpl: %v", err)
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	if _, err := RunChainOn(ctx, run021Fixture, raw, dir, WithImpl(impl)); err != nil {
		t.Fatalf("RunChain (harvested bodies): %v", err)
	}
	got := discriminate(t, dir)
	for _, s := range got.list {
		t.Errorf("%s scenario %s op %s survives the null subject over the real bodies", s.component, s.scenario, s.op)
	}
	if got.broken != 0 {
		t.Errorf("%d scenario(s) fail against the harvested bodies (TP-BROKEN), want 0", got.broken)
	}
	if got.survivors != 0 {
		t.Errorf("%d survivor(s), want 0", got.survivors)
	}
	if want := goScenarios(t, dir); got.scenarios != want || want == 0 {
		t.Errorf("%d scenario(s) discriminated, but %d TestScenario_ functions were generated for Go components", got.scenarios, want)
	}
	if got.scenarios != implScenarios {
		t.Errorf("%d scenario(s) discriminated over the harvested bodies, want %d: a scenario was dropped or added", got.scenarios, implScenarios)
	}
}

// implScenarios is how many Go scenarios the todomvc fixture generates: every one passes against the
// harvested bodies and none survives its stimulus being nulled.
const implScenarios = 78

// goScenarios counts the TestScenario_ functions testgen generated for Go components under dir
// (client_scenarios.gen_test.go belongs to the machine clients, which -discriminate announces as
// skipped).
func goScenarios(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(filepath.Join(dir, "internal"), func(path string, d os.DirEntry, err error) error {
		base := filepath.Base(path)
		if err != nil || d.IsDir() || !strings.HasSuffix(base, "_scenarios.gen_test.go") || base == "client_scenarios.gen_test.go" {
			return err
		}
		src, err := os.ReadFile(path) // #nosec G304 -- the chain's own output
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(src), "\n") {
			if strings.HasPrefix(line, "func TestScenario_") {
				n++
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("counting generated scenarios: %v", err)
	}
	return n
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

// discriminateRun is what one `testgen -discriminate` run reported: the counts of its summary line
// `discriminate: N scenario(s), M null run(s), S survivor(s), B broken`, and the survivors it named.
type discriminateRun struct {
	scenarios, nullRuns, survivors, broken int
	list                                   []survivor
}

// discriminate runs `testgen -discriminate` in the built app dir and returns what it reported. Exit 1
// with TP-DISCRIMINATE or TP-BROKEN lines is the run's answer, not a failure of the runner; a run with
// no summary line did not complete (an inconclusive scenario run is the tool's error) and fails the
// test. On a null build a scenario that asserts behavior is TP-BROKEN (it fails against the real
// subject, which is the null one) and is not nulled; only a scenario the null subject satisfies, such
// as the vacuous binding, is a survivor.
func discriminate(t *testing.T, dir string) discriminateRun {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "tool", "testgen", "-project", ".aiarch/state/project.json", // #nosec G204 -- fixed argv
		"-root", ".", "-uitests", "uitests/generated", "-discriminate")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.CombinedOutput()
	var got discriminateRun
	summarized := false
	named, brokenNamed := 0, 0
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		switch {
		case len(f) == 4 && f[0] == "TP-DISCRIMINATE":
			got.list = append(got.list, survivor{f[1], f[2], f[3]})
			named++
		case len(f) == 3 && f[0] == "TP-BROKEN":
			brokenNamed++
		default:
			var n [4]int
			if c, _ := fmt.Sscanf(line, "discriminate: %d scenario(s), %d null run(s), %d survivor(s), %d broken", &n[0], &n[1], &n[2], &n[3]); c == 4 {
				got.scenarios, got.nullRuns, got.survivors, got.broken = n[0], n[1], n[2], n[3]
				summarized = true
			}
		}
	}
	if !summarized {
		t.Fatalf("testgen -discriminate did not complete (%v); no summary line in:\n%s", err, out)
	}
	if named != got.survivors || brokenNamed != got.broken {
		t.Fatalf("testgen -discriminate named %d survivor(s) and %d broken but summarized %d and %d:\n%s", named, brokenNamed, got.survivors, got.broken, out)
	}
	return got
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
