package methodcheck

import (
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
)

// scenario_adapter_test.go pins the methodcheck → scenario.Input projection over a
// committed project.json in the exact envelope shape the server writes
// (testdata/scenario_project.json: slot 4 use cases with activity diagrams, slot 5
// dynamic views, top-level serviceContracts).

func loadProject(t *testing.T, name string) Project {
	t.Helper()
	p, ok, err := DecodeProject(readFixture(t, name))
	if err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	if !ok {
		t.Fatalf("decode %s: empty document", name)
	}
	return p
}

func TestScenarioInputFromProject(t *testing.T) {
	p := loadProject(t, "scenario_project.json")
	in, err := ScenarioInput(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Diagrams) == 0 || len(in.Contracts) == 0 || len(in.Actors) == 0 {
		t.Fatalf("empty adapter output: %+v", in)
	}

	// One Diagram per use case in slot 4 that carries an activity diagram, sorted by
	// use-case id, with nodes carrying the WIRE kinds verbatim.
	cu, _, err := p.coreUseCases()
	if err != nil {
		t.Fatal(err)
	}
	wantDiagrams := 0
	for _, d := range cu.Decisions {
		uc := d.UseCase
		for _, a := range uc.Actors {
			if !in.Actors[a.ID] {
				t.Errorf("actor %q of use case %q missing from Actors", a.ID, uc.ID)
			}
		}
		if uc.Activity == nil {
			continue
		}
		wantDiagrams++
		var dg *scenario.Diagram
		for i := range in.Diagrams {
			if in.Diagrams[i].UseCaseID == uc.ID {
				dg = &in.Diagrams[i]
			}
		}
		if dg == nil {
			t.Fatalf("no diagram for use case %q", uc.ID)
		}
		if dg.Title != uc.Name || len(dg.Nodes) != len(uc.Activity.Nodes) || len(dg.Edges) != len(uc.Activity.Edges) {
			t.Fatalf("diagram %q shape: %+v", uc.ID, *dg)
		}
		for i, n := range uc.Activity.Nodes {
			got := dg.Nodes[i]
			if got.ID != n.ID || got.Kind != n.Kind || got.Label != n.Label || got.RoleName != n.RoleName ||
				got.LinkedActorID != n.LinkedActorID || got.DecidedBy != n.DecidedBy ||
				got.InState != n.InState || got.Anchor != n.Anchor {
				t.Errorf("node %q projected wrong: got %+v want %+v", n.ID, got, n)
			}
		}
	}
	if len(in.Diagrams) != wantDiagrams {
		t.Fatalf("diagrams: got %d want %d", len(in.Diagrams), wantDiagrams)
	}
	for i := 1; i < len(in.Diagrams); i++ {
		if in.Diagrams[i-1].UseCaseID > in.Diagrams[i].UseCaseID {
			t.Fatalf("diagrams not sorted by use-case id: %q before %q", in.Diagrams[i-1].UseCaseID, in.Diagrams[i].UseCaseID)
		}
	}

	// The wire kinds the new mirror consts name must survive the projection.
	kinds := map[string]bool{}
	for _, d := range in.Diagrams {
		for _, n := range d.Nodes {
			kinds[n.Kind] = true
		}
	}
	for _, k := range []string{nodeStart, kindAcceptEvent, kindSendSignal, kindObjectNode, kindNote, kindSwimLane, nodeEnd} {
		if !kinds[k] {
			t.Errorf("fixture/adapter lost wire kind %q", k)
		}
	}

	// One Contract per ServiceContracts key, with op names and param names.
	if len(in.Contracts) != len(p.ServiceContracts) {
		t.Fatalf("contracts: got %d want %d", len(in.Contracts), len(p.ServiceContracts))
	}
	for key, sc := range p.ServiceContracts {
		ct, ok := in.Contracts[key]
		if !ok {
			t.Fatalf("contract %q missing", key)
		}
		if ct.Component != key || len(ct.Ops) != len(sc.Interface.Operations) {
			t.Fatalf("contract %q shape: %+v", key, ct)
		}
		for i, op := range sc.Interface.Operations {
			if ct.Ops[i].Name != op.Name || len(ct.Ops[i].Params) != len(op.Params) {
				t.Errorf("contract %q op %d: got %+v want %+v", key, i, ct.Ops[i], op)
			}
		}
	}

	// Dynamic views project step-by-step with their calls.
	sys, _, err := p.system()
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Views) != len(sys.DynamicViews) {
		t.Fatalf("views: got %d want %d", len(in.Views), len(sys.DynamicViews))
	}
	for i, v := range sys.DynamicViews {
		if in.Views[i].UseCaseID != v.UseCaseID || len(in.Views[i].Steps) != len(v.Steps) {
			t.Fatalf("view %d shape: %+v", i, in.Views[i])
		}
	}

	s1, err := DeriveScenarios(p)
	if err != nil {
		t.Fatal(err)
	}
	s2, _ := DeriveScenarios(p)
	f1, _ := scenario.Fingerprint(s1)
	f2, _ := scenario.Fingerprint(s2)
	if f1 != f2 || len(s1) == 0 {
		t.Fatal("unstable or empty derivation")
	}

	// The derivation resolves the dynamic-view call into a contract op: the
	// acceptEvent stimulus targets the manager (normalized id) and names its
	// operation (the slot-5 kebab id and the camelCase contract key fold together).
	var resolved bool
	for _, s := range s1 {
		for _, st := range s.Stimuli {
			if st.Input.To == scenario.Normalize("order-manager") && st.Input.Op == "PlaceOrder" {
				resolved = true
			}
		}
	}
	if !resolved {
		t.Fatalf("no stimulus resolved to order-manager.PlaceOrder: %+v", s1)
	}
}

// The adapter must tolerate the real captured fixture (no contracts, a use case with
// a nil activity + nil actors, a diagram with no entry node): no error, one diagram
// for the use case that has an activity, and a derivation that is simply empty.
func TestScenarioInputFromProject_RealFixtureTolerated(t *testing.T) {
	p := loadProject(t, "project.json")
	in, err := ScenarioInput(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Diagrams) != 1 || in.Diagrams[0].UseCaseID != "decide" {
		t.Fatalf("diagrams: %+v", in.Diagrams)
	}
	if !in.Actors["architect"] || len(in.Contracts) != 0 {
		t.Fatalf("actors/contracts: %+v / %+v", in.Actors, in.Contracts)
	}
	s, err := DeriveScenarios(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 0 {
		t.Fatalf("a diagram with no entry node must derive no scenarios: %+v", s)
	}
}

// A project with no committed core use cases yields an empty (but non-nil-mapped)
// input rather than an error.
func TestScenarioInput_NoUseCases(t *testing.T) {
	in, err := ScenarioInput(Project{})
	if err != nil {
		t.Fatal(err)
	}
	if in.Actors == nil || in.Contracts == nil || len(in.Diagrams) != 0 {
		t.Fatalf("unexpected: %+v", in)
	}
}
