package testgen

import (
	"encoding/json"
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
	src := string(out.Generated[billingGen])
	for _, want := range []string{
		"func TestScenario_BillingManager_process_order_P2(t *testing.T) {",
		"func TestScenario_OrderManager_process_order_P2(t *testing.T) {",
		"type inputBillingManagerProcessOrderP2S1 struct {",
		"type inputOrderManagerProcessOrderP2S1 struct {",
		"var subject billing.BillingManager = scenariocompose.NewBillingManager(t, h)",
		"var subject billing.OrderManager = scenariocompose.NewOrderManager(t, h)",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("shared generated file lacks %q:\n%s", want, src)
		}
	}
	if len(out.HooksOnce) != 1 { // the web client's hooks.ts only
		t.Errorf("a shared Go package gets no hooks file: %v", keys(out.HooksOnce))
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
	out := generateShared(t)
	src := string(out.Generated[billingGen]) + string(out.Generated[billingNull])
	for _, want := range []string{
		"\tscenariohost.MainWithWorkflows(m, scenariocompose.Workflows(), \"billingManager\", \"orderManager\")\n}\n",
		"h.RunScenario(t, \"billingManager\", \"process-order-P2\", func(t *testing.T) {",
		"h.RunScenario(t, \"orderManager\", \"process-order-P2\", func(t *testing.T) {",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("shared generated file lacks %q:\n%s", want, src)
		}
	}
	// One package, one worker registry: the replay registration comes from the compose package, once.
	if n := strings.Count(src, "scenariocompose.Workflows()"); n != 1 {
		t.Errorf("shared generated file registers workflows %d times, want 1", n)
	}
}

// TestClaim_FoldCollisionIsAnError: two scenarios whose ids fold to the same
// Go name would declare one symbol twice and break the package's build; the
// emitter refuses instead.
func TestClaim_FoldCollisionIsAnError(t *testing.T) {
	g := &goEmit{goPkg: "internal/manager/billing", owners: map[string]string{}}
	if err := g.claim("inputProcessOrderP2S1", "billingManager process-order-P2 step 1"); err != nil {
		t.Fatal(err)
	}
	if err := g.claim("inputProcessOrderP2S1", "billingManager process-order-P2 step 1"); err != nil {
		t.Fatalf("re-claim by the same owner: %v", err)
	}
	err := g.claim("inputProcessOrderP2S1", "billingManager process_order-P2 step 1")
	if err == nil {
		t.Fatal("collision not reported")
	}
	for _, want := range []string{"process-order-P2", "process_order-P2", "inputProcessOrderP2S1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}
