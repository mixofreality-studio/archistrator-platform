package appgen

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/internal/testfixtures"
)

// mutateContracts edits the serviceContracts of a fixture's project.json through fn and returns the bytes.
func mutateContracts(t *testing.T, raw []byte, fn func(contracts map[string]map[string]any)) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	contracts := map[string]map[string]any{}
	for k, v := range doc["serviceContracts"].(map[string]any) {
		contracts[k] = v.(map[string]any)
	}
	fn(contracts)
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// collideWithInterface adds a $def named like the contract's own interface: modelgen then declares
// the name twice in contract.gen.go, and every sibling that names the interface cannot type.
func collideWithInterface(contracts map[string]map[string]any, key, iface string) {
	defs := contracts[key]["$defs"].(map[string]any)
	defs[iface] = map[string]any{"type": "object", "properties": map[string]any{"A": map[string]any{"type": "string"}}, "required": []any{"A"}}
}

func problemsOf(ps []Problem, consumer string) []Problem {
	var out []Problem
	for _, p := range ps {
		if p.Consumer == consumer {
			out = append(out, p)
		}
	}
	return out
}

func hasLocal(ps []Problem) bool {
	return slices.ContainsFunc(ps, func(p Problem) bool { _, ok := p.Severity.(Local); return ok })
}

// A def named like the contract's interface is a contract the recorder accepts and the generators
// cannot type against one another: the write is refused before it can reach a paid run.
func TestAdmitTier1_RefusesContractGeneratorsCannotType(t *testing.T) {
	raw := testfixtures.Fixture(t, "greenfield")
	in := Input{Project: mutateContracts(t, raw, func(c map[string]map[string]any) { collideWithInterface(c, "orderManager", "OrderManager") }),
		ModulePath: "example.com/app", ModuleDir: "server", Config: DefaultConfig()}
	ps := AdmitTier1(in, KindServiceContract)
	if !hasLocal(ps) {
		t.Fatalf("a def named like the interface must be a Local problem, got %v", ps)
	}
	var sawContract bool
	for _, p := range problemsOf(ps, "modelgen") {
		if p.Path == "server/internal/manager/order/contract.gen.go" && strings.Contains(p.Message, "redeclared") {
			sawContract = true
		}
	}
	if !sawContract {
		t.Errorf("modelgen's contract file must carry the redeclaration, got %v", ps)
	}
	// The siblings that name the interface fail too: the invoker side (appgen temporal) is one.
	var sawTemporal bool
	for _, p := range problemsOf(ps, "appgen temporal") {
		if p.Path == "server/internal/manager/order/worker.gen.go" {
			sawTemporal = true
		}
	}
	if !sawTemporal {
		t.Errorf("appgen temporal's worker for the same contract must fail against it, got %v", ps)
	}
	for _, p := range ps {
		if p.Path != "" && !strings.HasPrefix(p.Path, "server/") {
			t.Errorf("problem path %q is not repo-relative under the module dir", p.Path)
		}
	}
}

// A Manager whose dependency names a component the System has not recorded yet is the design in
// progress, not a defect: the write is accepted with a warning and Tier 2 enforces it.
func TestAdmitTier1_IncompleteIsWarning(t *testing.T) {
	raw := testfixtures.Fixture(t, "greenfield")
	in := Input{Project: []byte(strings.ReplaceAll(string(raw), `"component": "pricingEngine"`, `"component": "ghostEngine"`)),
		ModulePath: "example.com/app", ModuleDir: "server", Config: DefaultConfig()}
	ps := AdmitTier1(in, KindServiceContract)
	if len(ps) == 0 {
		t.Fatal("a dependency on an unrecorded component must be reported")
	}
	if hasLocal(ps) {
		t.Fatalf("an absent component is Incomplete, never Local: %v", ps)
	}
	for _, p := range ps {
		if !strings.Contains(p.Message, "ghostEngine") {
			t.Errorf("the problem must name the absent id: %q", p.Message)
		}
		if strings.Contains(p.Message, "\n") {
			t.Errorf("an emit failure is one line: %q", p.Message)
		}
	}
}

// The design is recorded one contract at a time: an mcpApp client whose Manager is not recorded yet
// is the design in progress. Each contract of the todomvc fixture, recorded in layer order with no
// plan yet, admits with no Local problem, and the first ones carry the warning.
func TestAdmitTier1_ContractsRecordedOneAtATimeAreNeverLocal(t *testing.T) {
	raw := testfixtures.Fixture(t, "todomvc")
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	delete(doc, "phaseArtifacts")
	all := doc["serviceContracts"].(map[string]any)
	rank := map[string]int{"utility": 0, "resourceaccess": 1, "engine": 2, "manager": 3}
	var keys []string
	for k, v := range all {
		if _, ok := rank[strings.ToLower(v.(map[string]any)["layer"].(string))]; ok {
			keys = append(keys, k)
		}
	}
	slices.SortFunc(keys, func(a, b string) int {
		la, lb := strings.ToLower(all[a].(map[string]any)["layer"].(string)), strings.ToLower(all[b].(map[string]any)["layer"].(string))
		if rank[la] != rank[lb] {
			return rank[la] - rank[lb]
		}
		return strings.Compare(a, b)
	})
	recorded := map[string]any{}
	var warned bool
	for _, k := range keys {
		recorded[k] = all[k]
		doc["serviceContracts"] = recorded
		b, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		in := Input{Project: b, ModulePath: "example.com/app", ModuleDir: "server", Config: DefaultConfig()}
		for _, kind := range []ArtifactKind{KindServiceContract, KindSystem} {
			ps := AdmitTier1(in, kind)
			if hasLocal(ps) {
				t.Fatalf("after recording %s, %s admits a Local problem: %v", k, kind, ps)
			}
			warned = warned || len(ps) > 0
		}
	}
	if !warned {
		t.Error("an mcpApp client whose Manager is unrecorded must warn at least once: the proof is vacuous")
	}
}

// An unknown layer is a defect in the contract itself, not a part of the design that is yet to come.
func TestAdmitTier1_UnknownLayerIsLocal(t *testing.T) {
	raw := testfixtures.Fixture(t, "greenfield")
	in := Input{Project: []byte(strings.Replace(string(raw), `"layer": "Manager"`, `"layer": "Wizard"`, 1)), ModulePath: "example.com/app", ModuleDir: "server", Config: DefaultConfig()}
	if ps := AdmitTier1(in, KindServiceContract); !hasLocal(ps) {
		t.Fatalf("an unknown layer must be Local, got %v", ps)
	}
}

// Two defects in two contracts are two sets of problems from one call: no early return.
func TestAdmit_AllFindingsAtOnce(t *testing.T) {
	raw := testfixtures.Fixture(t, "greenfield")
	in := Input{Project: mutateContracts(t, raw, func(c map[string]map[string]any) {
		collideWithInterface(c, "orderManager", "OrderManager")
		collideWithInterface(c, "fulfillmentManager", "FulfillmentManager")
	}), ModulePath: "example.com/app", ModuleDir: "server", Config: DefaultConfig()}
	ps := AdmitTier1(in, KindServiceContract)
	paths := map[string]bool{}
	for _, p := range ps {
		paths[p.Path] = true
	}
	for _, want := range []string{"server/internal/manager/order/contract.gen.go", "server/internal/manager/fulfillment/contract.gen.go"} {
		if !paths[want] {
			t.Errorf("no problem for %s: %v", want, ps)
		}
	}
	sorted := slices.IsSortedFunc(ps, func(a, b Problem) int {
		if c := strings.Compare(a.Consumer, b.Consumer); c != 0 {
			return c
		}
		return strings.Compare(a.Path, b.Path)
	})
	if !sorted {
		t.Errorf("problems must be sorted by consumer then path: %v", ps)
	}
}

// The clean fixtures admit with zero problems for every artifact kind although no standard
// library, framework-go or Temporal package is resolvable by the importer: an opaque package's
// errors are Tier 2's.
func TestAdmitTier1_OpaqueImportsAreNotFindings(t *testing.T) {
	for _, fx := range fixtures {
		for _, k := range ArtifactKinds() {
			t.Run(fx+"/"+string(k), func(t *testing.T) {
				in := fixtureInput(t, fx, "server", false)
				if ps := AdmitTier1(in, k); len(ps) != 0 {
					t.Fatalf("the clean %s fixture admits with no problems for %s, got %d: %v", fx, k, len(ps), ps)
				}
			})
		}
	}
}

// Code a person or an agent writes beside the generated files in a contract's package is the one
// thing the check cannot see: a name the generated files take from it is not a finding.
func TestAdmitTier1_HandWrittenNamesAreNotFindings(t *testing.T) {
	raw := testfixtures.Fixture(t, "greenfield")
	in := Input{Project: mutateContracts(t, raw, func(c map[string]map[string]any) {
		deps := c["orderManager"]["deps"].([]any)
		c["orderManager"]["deps"] = append(deps, map[string]any{"name": "extra", "goType": "pricing.NotEmitted", "goImport": "example.com/app/internal/engine/pricing"})
	}), ModulePath: "example.com/app", ModuleDir: "server", Config: DefaultConfig()}
	if ps := AdmitTier1(in, KindServiceContract); len(ps) != 0 {
		t.Fatalf("a dependency on a name of the pricing package's hand-written code is not a Tier 1 finding: %v", ps)
	}
}

// The check is pure and runs in a workflow under Temporal's one-second deadlock detector: the
// median of three runs over the todomvc fixture (the largest conformance project) and over
// archistrator's own project stays under 500 ms (D8).
func TestAdmitTier1_UnderWorkflowBudget(t *testing.T) {
	own, err := os.ReadFile("../testdata/archistrator.project.json")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]Input{
		"todomvc":      fixtureInput(t, "todomvc", "server", false),
		"archistrator": {Project: own, ModulePath: "github.com/mixofreality-studio/archistrator/server", ModuleDir: "server", Config: DefaultConfig()},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			var runs []time.Duration
			for range 3 {
				start := time.Now()
				AdmitTier1(in, KindServiceContract)
				runs = append(runs, time.Since(start))
			}
			slices.Sort(runs)
			if runs[1] > 500*time.Millisecond {
				t.Fatalf("median %v over the 500 ms budget (runs %v)", runs[1], runs)
			}
		})
	}
}

// Archistrator's own project is an app with hand-written packages and an appgen.json this check
// does not read: nothing in it is a Local problem. What its variant constructors' arguments depend
// on is Incomplete (Tier 2 builds with the real file).
func TestAdmitTier1_OwnProjectHasNoLocalProblem(t *testing.T) {
	own, err := os.ReadFile("../testdata/archistrator.project.json")
	if err != nil {
		t.Fatal(err)
	}
	in := Input{Project: own, ModulePath: "github.com/mixofreality-studio/archistrator/server", ModuleDir: "server", Config: DefaultConfig()}
	for _, k := range ArtifactKinds() {
		if ps := AdmitTier1(in, k); hasLocal(ps) {
			t.Errorf("%s: archistrator's own project must have no Local problem: %v", k, ps)
		}
	}
}

func TestAdmitTier1_UnknownKindAdmitsNothing(t *testing.T) {
	if ps := AdmitTier1(fixtureInput(t, "greenfield", "server", false), "nope"); ps != nil {
		t.Fatalf("an unknown kind has no consumers: %v", ps)
	}
}

func TestSeverityName(t *testing.T) {
	if SeverityName(Local{}) != "local" || SeverityName(Incomplete{}) != "incomplete" {
		t.Fatal("severity names")
	}
}
