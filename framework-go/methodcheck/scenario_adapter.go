package methodcheck

import (
	"sort"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
)

// scenario_adapter.go projects the methodcheck structural mirror of project.json onto
// the adapter-neutral scenario.Input the derivation reads (deterministic-component-
// testing design §2). It is the ONLY place methodcheck touches framework-go/scenario:
// the TP-* rule family and the platform's testgen both consume DeriveScenarios so the
// scenario ids a test plan binds and the ids the emitter generates against are the
// same canonical set.

// ScenarioInput builds the derivation input from the committed core use cases (slot
// 4: one Diagram per use case that carries an activity diagram, plus every actor), the
// committed System's dynamic views (slot 5), and the top-level service contracts. A
// project without committed use cases yields an empty input, not an error; the only
// error is a malformed slot payload.
func ScenarioInput(p Project) (scenario.Input, error) {
	in := scenario.Input{Actors: map[string]bool{}, Contracts: map[string]scenario.Contract{}}
	cu, ok, err := p.coreUseCases()
	if err != nil {
		return in, err
	}
	if !ok {
		return in, nil
	}
	addUseCases(&in, cu)
	sys, ok, err := p.system()
	if err != nil {
		return in, err
	}
	if ok {
		for _, v := range sys.DynamicViews {
			in.Views = append(in.Views, viewOf(v))
		}
	}
	for key, sc := range p.ServiceContracts {
		in.Contracts[key] = contractOf(key, sc)
	}
	return in, nil
}

// addUseCases records every use case's actors and one Diagram per use case that
// carries an activity diagram, sorted by use-case id.
func addUseCases(in *scenario.Input, cu CoreUseCases) {
	for _, d := range cu.Decisions {
		uc := d.UseCase
		for _, a := range uc.Actors {
			in.Actors[a.ID] = true
		}
		if uc.Activity == nil {
			continue
		}
		in.Diagrams = append(in.Diagrams, diagramOf(uc))
	}
	sort.SliceStable(in.Diagrams, func(i, j int) bool { return in.Diagrams[i].UseCaseID < in.Diagrams[j].UseCaseID })
}

// DeriveScenarios is ScenarioInput followed by scenario.Derive: the canonical
// scenario set of a project.
func DeriveScenarios(p Project) ([]scenario.Scenario, error) {
	r, err := deriveReport(p)
	return r.Scenarios, err
}

// deriveReport is ScenarioInput followed by scenario.DeriveReport: the canonical
// scenario set plus the start families the derivation dropped (Amendment A3).
func deriveReport(p Project) (scenario.Report, error) {
	in, err := ScenarioInput(p)
	if err != nil {
		return scenario.Report{}, err
	}
	return scenario.DeriveReport(in)
}

// diagramOf hands a use case's activity diagram through with its wire node kinds
// verbatim; the mirror's zero-value strings for absent nullable fields are already
// the "" the derivation reads as absent.
func diagramOf(uc UseCase) scenario.Diagram {
	d := scenario.Diagram{UseCaseID: uc.ID, Title: uc.Name}
	for _, n := range uc.Activity.Nodes {
		d.Nodes = append(d.Nodes, scenario.Node{
			ID: n.ID, Kind: n.Kind, Label: n.Label, RoleName: n.RoleName,
			LinkedActorID: n.LinkedActorID, DecidedBy: n.DecidedBy,
			InState: n.InState, Anchor: n.Anchor,
		})
	}
	for _, e := range uc.Activity.Edges {
		d.Edges = append(d.Edges, scenario.Edge{From: e.From, To: e.To, Guard: e.Guard})
	}
	return d
}

// viewOf projects one dynamic view step by step, keeping call order.
func viewOf(v DynamicView) scenario.View {
	view := scenario.View{UseCaseID: v.UseCaseID}
	for _, st := range v.Steps {
		s := scenario.Step{NodeID: st.ActivityNodeID}
		for _, c := range st.Calls {
			s.Calls = append(s.Calls, scenario.Call{From: c.From, To: c.To, Mode: c.Mode, Label: c.Label, Alt: c.Alt})
		}
		view.Steps = append(view.Steps, s)
	}
	return view
}

// contractOf keeps a contract's operation surface (op names + param names) under its
// serviceContracts key, which is what the derivation resolves call labels against.
func contractOf(key string, sc ServiceContract) scenario.Contract {
	ct := scenario.Contract{Component: key}
	for _, op := range sc.Interface.Operations {
		o := scenario.Op{Name: op.Name}
		for _, prm := range op.Params {
			o.Params = append(o.Params, prm.Name)
		}
		ct.Ops = append(ct.Ops, o)
	}
	return ct
}
