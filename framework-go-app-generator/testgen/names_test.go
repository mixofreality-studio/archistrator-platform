package testgen

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestGoName pins the folding against revive's var-naming rule: underscores
// between words go, a lower-case word after the first is capitalized, a word
// that is a common initialism is upper-cased (lower-cased when it leads), and
// the fold is repeated until revive would suggest nothing — "id2" folds to
// "Id2" on one pass, which revive then splits at the lower→digit boundary and
// wants as "ID2".
func TestGoName(t *testing.T) {
	for _, tc := range []struct {
		parts []string
		want  string
	}{
		{[]string{"step", "UC3_P2", "S1"}, "stepUC3P2S1"},
		{[]string{"probe", "UC3_P2", "S1", "N2"}, "probeUC3P2S1N2"},
		{[]string{"step", "bill_the_user_for_usage_P1", "S1"}, "stepBillTheUserForUsageP1S1"},
		{[]string{"step", "BillingManager", "process_order_P2", "S1"}, "stepBillingManagerProcessOrderP2S1"},
		{[]string{"newSubject"}, "newSubject"},
		{[]string{"newSubject", "BillingManager"}, "newSubjectBillingManager"},
		{[]string{"step", "rotate_api_key_P1", "S2"}, "stepRotateAPIKeyP1S2"},
		{[]string{"step", "view_by_id_P1", "S1"}, "stepViewByIDP1S1"},
		{[]string{"step", "list_ids_P1", "S1"}, "stepListIDsP1S1"},
		{[]string{"step", "id2_P1", "S1"}, "stepID2P1S1"},
		{[]string{"input", "read_json_P3", "S4"}, "inputReadJSONP3S4"},
		{[]string{"step", "v1_2_P1", "S1"}, "stepV1_2P1S1"}, // revive keeps one underscore between digits
		{[]string{"step", "a__b", "S1"}, "stepABS1"},
	} {
		if got := goName(tc.parts...); got != tc.want {
			t.Errorf("goName(%q) = %q, want %q", tc.parts, got, tc.want)
		}
		if got := lintName(tc.want); got != tc.want {
			t.Errorf("lintName(%q) = %q: the fold is not a fixed point", tc.want, got)
		}
	}
}

// TestIsHookName pins the classifier the drift check uses against the names
// the emitter spells: every emitted hook is a hook, and the generated file's
// locals and an agent's own helpers are not.
func TestIsHookName(t *testing.T) {
	for _, name := range []string{
		"newSubject", "newSubjectBillingManager",
		"stepUC3P2S1", "stepBillingManagerProcessOrderP2S12", "stepV1_2P1S1",
		"probeUC3P2S1N2", "probeOrderManagerProcessOrderP2S1N10",
		"replayWorkflows",
	} {
		if !isHookName(name) {
			t.Errorf("isHookName(%q) = false, want true", name)
		}
	}
	for _, name := range []string{
		"probe1_1", "pout1_1", "inputUC3P2S1", "stepHelper", "stepper", "probeUC3P2S1",
		"arrange", "TestScenario_UC3_P2", "callContext", "replayWorkflowsBilling",
	} {
		if isHookName(name) {
			t.Errorf("isHookName(%q) = true, want false", name)
		}
	}
}

// TestGenerate_HooksFileIsLintClean parses every emitted Go hooks file and
// requires every top-level declaration, and every parameter of one, to be a
// name revive's var-naming accepts and to carry no underscore — the hooks file
// is agent-owned and linted, and B3 forbids both //nolint and a lint-config
// exemption.
func TestGenerate_HooksFileIsLintClean(t *testing.T) {
	for name, out := range map[string]Output{"single": generate(t), "shared package": generateShared(t)} {
		for _, p := range keys(out.HooksOnce) {
			if !strings.HasSuffix(p, ".go") {
				continue
			}
			f, err := parser.ParseFile(token.NewFileSet(), p, out.HooksOnce[p], parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("%s: %s: %v", name, p, err)
			}
			for _, d := range f.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok {
					continue
				}
				idents := []*ast.Ident{fn.Name}
				for _, fl := range fn.Type.Params.List {
					idents = append(idents, fl.Names...)
				}
				for _, id := range idents {
					if strings.Contains(id.Name, "_") || lintName(id.Name) != id.Name {
						t.Errorf("%s: %s declares %q, which revive var-naming rejects", name, p, id.Name)
					}
				}
				if !isHookName(fn.Name.Name) {
					t.Errorf("%s: %s declares %q, which the drift check does not see as a hook", name, p, fn.Name.Name)
				}
			}
		}
	}
}

// generateShared emits the fixture with orderManager moved into billing's Go
// package, so two contracts share one package and every name carries the
// contract's interface.
func generateShared(t *testing.T) Output {
	t.Helper()
	raw, err := os.ReadFile("testdata/project_tp.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	doc["serviceContracts"].(map[string]any)["orderManager"].(map[string]any)["goPackage"] = "internal/manager/billing"
	shared, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Generate(shared, Config{ModulePath: "example.com/app/server"})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestGenerate_SharedPackageInfixesTheInterface(t *testing.T) {
	out := generateShared(t)
	hooks := string(out.HooksOnce[billingHooks])
	for _, want := range []string{
		"func newSubjectBillingManager(t *testing.T, h *scenariohost.Host) billing.BillingManager",
		"func newSubjectOrderManager(t *testing.T, h *scenariohost.Host) billing.OrderManager",
		"func stepBillingManagerProcessOrderP2S1(t *testing.T, h *scenariohost.Host, subject billing.BillingManager, in inputBillingManagerProcessOrderP2S1) (any, error)",
		"func stepOrderManagerProcessOrderP2S1(t *testing.T, h *scenariohost.Host, subject billing.OrderManager, in inputOrderManagerProcessOrderP2S1) (any, error)",
		"func probeOrderManagerProcessOrderP2S1N1(t *testing.T, h *scenariohost.Host, subject billing.OrderManager) (any, error)",
	} {
		if !strings.Contains(hooks, want) {
			t.Errorf("shared hooks file lacks %q:\n%s", want, hooks)
		}
	}
	root := t.TempDir()
	writeAll(t, root, out.Generated)
	writeAll(t, root, out.HooksOnce)
	msgs, err := Drift(root, out)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("fresh shared package drifted: %v", msgs)
	}
}

// TestGenerate_SharedPackageKeysResultsByComponent: facets sharing one Go
// package run in one test process, so the generated file declares every one
// of them to scenariohost.Main and records each verdict under its own
// component — the same scenario id bound by two contracts lands in two
// results.json files instead of one overwriting the other.
func TestGenerate_SharedPackageKeysResultsByComponent(t *testing.T) {
	src := string(generateShared(t).Generated[billingGen])
	for _, want := range []string{
		"func TestMain(m *testing.M) {\n\tscenariohost.MainWithWorkflows(m, replayWorkflows(), \"billingManager\", \"orderManager\")\n}\n",
		"h.RunScenario(t, \"billingManager\", \"process-order-P2\", func(t *testing.T) {",
		"h.RunScenario(t, \"orderManager\", \"process-order-P2\", func(t *testing.T) {",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("shared generated file lacks %q:\n%s", want, src)
		}
	}
	// One package, one worker registry: the shared hooks file declares the
	// replay hook once, not once per contract.
	if n := strings.Count(string(generateShared(t).HooksOnce[billingHooks]), "func replayWorkflows("); n != 1 {
		t.Errorf("shared hooks file declares replayWorkflows %d times, want 1", n)
	}
}

// TestDrift_AgentHelperIsNotAHook: a helper the agent adds to its hooks file
// is not spelled like a hook, so the drift check leaves it alone.
func TestDrift_AgentHelperIsNotAHook(t *testing.T) {
	out := generate(t)
	root := t.TempDir()
	writeAll(t, root, out.Generated)
	writeAll(t, root, out.HooksOnce)
	hooks := string(out.HooksOnce[billingHooks]) + "\nfunc stepHelper(t *testing.T) {}\n\nfunc arrangeInvoice(t *testing.T) {}\n"
	writeAll(t, root, map[string][]byte{billingHooks: []byte(hooks)})
	msgs, err := Drift(root, out)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("agent helpers reported as drift: %v", msgs)
	}
}

// TestClaim_FoldCollisionIsAnError: two scenarios whose ids fold to the same
// Go name would declare one symbol twice and break the package's build; the
// emitter refuses instead.
func TestClaim_FoldCollisionIsAnError(t *testing.T) {
	g := &goEmit{goPkg: "internal/manager/billing", owners: map[string]string{}}
	if err := g.claim("stepProcessOrderP2S1", "billingManager process-order-P2 step 1"); err != nil {
		t.Fatal(err)
	}
	if err := g.claim("stepProcessOrderP2S1", "billingManager process-order-P2 step 1"); err != nil {
		t.Fatalf("re-claim by the same owner: %v", err)
	}
	err := g.claim("stepProcessOrderP2S1", "billingManager process_order-P2 step 1")
	if err == nil {
		t.Fatal("collision not reported")
	}
	for _, want := range []string{"process-order-P2", "process_order-P2", "stepProcessOrderP2S1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}
