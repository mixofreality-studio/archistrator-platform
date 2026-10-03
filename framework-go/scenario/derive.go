package scenario

import (
	"fmt"
	"sort"
	"strings"
)

// Derive enumerates every scenario of every diagram in in, sorted by ID.
// Errors only on malformed input (unknown node kind, edge to unknown node, duplicate node id).
func Derive(in Input) ([]Scenario, error) {
	var all []Scenario
	diagrams := append([]Diagram{}, in.Diagrams...)
	sort.SliceStable(diagrams, func(i, j int) bool { return diagrams[i].UseCaseID < diagrams[j].UseCaseID })
	for _, d := range diagrams {
		g, err := buildGraph(d)
		if err != nil {
			return nil, err
		}
		view := viewFor(in, d.UseCaseID)
		var raws []rawPath
		for _, entry := range g.entries() {
			ps, backs := g.elementary(entry)
			raws = append(raws, ps...)
			raws = append(raws, unrollOnce(ps, backs)...)
		}
		for i, rp := range raws {
			s := Scenario{
				ID:      fmt.Sprintf("%s-P%d", d.UseCaseID, i+1),
				UseCase: d.UseCaseID,
				Path:    rp.nodes,
				Guards:  rp.guards,
			}
			s.Title = title(d.Title, rp.guards)
			s.Preconditions, s.Stimuli = stimuli(in, g, d, view, rp)
			all = append(all, s)
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	return all, nil
}

func viewFor(in Input, uc string) *View {
	for i := range in.Views {
		if in.Views[i].UseCaseID == uc {
			return &in.Views[i]
		}
	}
	return nil
}

func title(base string, guards []Guard) string {
	var gs []string
	for _, g := range guards {
		if g.Guard != "" {
			gs = append(gs, g.Guard)
		}
	}
	if len(gs) == 0 {
		return base
	}
	return base + " — " + strings.Join(gs, " · ")
}

// stimuli walks the path, opening a new Stimulus at each input and attaching
// outputs/states/hints to the open one (or to preconditions before the first).
func stimuli(in Input, g graph, d Diagram, view *View, rp rawPath) (Outcome, []Stimulus) {
	var pre Outcome
	var out []Stimulus
	cur := func() *Outcome {
		if len(out) == 0 {
			return &pre
		}
		return &out[len(out)-1].Outcome
	}
	hints := hintsByAnchor(d)
	guardAt := map[string]string{}
	for _, gd := range rp.guards {
		guardAt[gd.At] = gd.Guard
	}
	for _, id := range rp.nodes {
		n := g.nodes[id]
		switch classify(n, in, view) {
		case classInput:
			ev := inputEvent(in, d, view, n, guardAt[id])
			out = append(out, Stimulus{Seq: len(out) + 1, Node: id, Input: ev})
		case classOutput:
			c := cur()
			c.ExpectedOutputs = append(c.ExpectedOutputs, outputOf(n))
		case classState:
			c := cur()
			c.ExpectedStates = append(c.ExpectedStates, State{Node: id, Object: n.Label, InState: n.InState})
		case classInternal, classHint, classStructural:
		}
		if hs, ok := hints[id]; ok {
			c := cur()
			c.Hints = append(c.Hints, hs...)
		}
	}
	return pre, out
}

// hintsByAnchor collects anchored note labels per anchor node, in declaration order.
func hintsByAnchor(d Diagram) map[string][]string {
	hints := map[string][]string{}
	for _, n := range d.Nodes {
		if n.Kind == "note" && n.Anchor != "" {
			hints[n.Anchor] = append(hints[n.Anchor], n.Label)
		}
	}
	return hints
}

// inputEvent builds the input of a stimulus opened at n. The first dynamic-view
// call into a component supplies the target, the resolved op and provenance;
// with no such call, the §2.2 rule-5 component-lane fallback supplies the target.
func inputEvent(in Input, d Diagram, view *View, n Node, guard string) InputEv {
	ev := InputEv{Kind: n.Kind}
	if n.Kind == "decision" || n.Kind == "switch" {
		ev.From = n.DecidedBy
		ev.Guard = guard
	} else {
		ev.From = n.LinkedActorID
	}
	if c, idx, ok := firstCallInto(in, view, n.ID); ok {
		ev.To = Normalize(c.To)
		ev.Op = resolveOp(in, c.To, c.Label)
		ev.Call = fmt.Sprintf("%s/%s/%d", d.UseCaseID, n.ID, idx)
		if ev.From == "" {
			ev.From = c.From
		}
	} else if lane := componentLane(in, n); lane != "" {
		ev.To = lane
	}
	return ev
}

// outputOf is the expected output an output-classified node contributes: a
// sendSignal as declared, any other node as an inference from its dynamic view.
func outputOf(n Node) Output {
	o := Output{Node: n.ID, Kind: "inferred", To: n.LinkedActorID, Label: n.Label}
	if n.Kind == "sendSignal" {
		o.Kind = "sendSignal"
	}
	return o
}

// componentLane is the §2.2 rule-5 fallback: a node in a component's lane with no
// dynamic-view call targets that component.
func componentLane(in Input, n Node) string {
	if n.RoleName == "" {
		return ""
	}
	if isComponent(in, n.RoleName) {
		return Normalize(n.RoleName)
	}
	return ""
}

// ForComponent keeps scenarios with ≥1 stimulus whose Input.To == Normalize(component),
// with Stimuli filtered to those (Seq renumbered from 1, original seq kept in Stimulus.Node provenance).
func ForComponent(all []Scenario, component string) []Scenario {
	key := Normalize(component)
	var out []Scenario
	for _, s := range all {
		var kept []Stimulus
		for _, st := range s.Stimuli {
			if st.Input.To == key {
				st.Seq = len(kept) + 1
				kept = append(kept, st)
			}
		}
		if len(kept) == 0 {
			continue
		}
		cp := s
		cp.Stimuli = kept
		out = append(out, cp)
	}
	return out
}
