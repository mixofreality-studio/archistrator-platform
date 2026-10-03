package scenario

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Derive enumerates every scenario of every diagram in in, sorted by ID.
// Errors only on malformed input (unknown node kind, edge to unknown node, duplicate node id).
func Derive(in Input) ([]Scenario, error) {
	r, err := DeriveReport(in)
	return r.Scenarios, err
}

// DeriveReport is Derive plus the design facts the derivation established: the
// use cases whose start family it dropped (Amendment A3). Ids number the KEPT
// paths of a use case in canonical order, so a dropped path leaves no gap.
func DeriveReport(in Input) (Report, error) {
	var r Report
	diagrams := append([]Diagram{}, in.Diagrams...)
	sort.SliceStable(diagrams, func(i, j int) bool { return diagrams[i].UseCaseID < diagrams[j].UseCaseID })
	for _, d := range diagrams {
		kept, dropped, err := deriveDiagram(in, d)
		if err != nil {
			return Report{}, err
		}
		if dropped > 0 {
			r.StartRedundant = append(r.StartRedundant, StartRedundancy{UseCase: d.UseCaseID, Dropped: dropped})
		}
		r.Scenarios = append(r.Scenarios, kept...)
	}
	sort.SliceStable(r.Scenarios, func(i, j int) bool { return r.Scenarios[i].ID < r.Scenarios[j].ID })
	return r, nil
}

// deriveDiagram enumerates one diagram's paths from every entry, derives each
// path's stimuli, drops a redundant start family (A3) and numbers what is kept.
func deriveDiagram(in Input, d Diagram) (kept []Scenario, dropped int, err error) {
	g, err := buildGraph(d)
	if err != nil {
		return nil, 0, err
	}
	view := viewFor(in, d.UseCaseID)
	var cands []Scenario
	for _, entry := range g.entries() {
		ps, backs := g.elementary(entry)
		for _, rp := range append(ps, unrollOnce(ps, backs)...) {
			s := Scenario{UseCase: d.UseCaseID, Path: rp.nodes, Guards: rp.guards, Title: title(d.Title, rp.guards)}
			s.Preconditions, s.Stimuli = stimuli(in, g, d, view, rp)
			cands = append(cands, s)
		}
	}
	kept, dropped = dropRedundantStart(g, cands)
	for i := range kept {
		kept[i].ID = fmt.Sprintf("%s-P%d", d.UseCaseID, i+1)
	}
	return kept, dropped, nil
}

// dropRedundantStart is Amendment A3. When a diagram has both a start node and
// an event entry, a start-family path is dropped when it carries no stimulus of
// its own: it has none at all, or its walk after start is the tail of an
// event-entry path, so every stimulus it carries that event path carries too
// (with the entry's stimulus besides). A start-family path that reaches a
// stimulus no event path reaches is kept.
func dropRedundantStart(g graph, cands []Scenario) (kept []Scenario, dropped int) {
	var events [][]string
	hasStart := false
	for _, s := range cands {
		switch g.nodes[s.Path[0]].Kind {
		case "start":
			hasStart = true
		case "timeEvent", "acceptEvent":
			events = append(events, s.Path)
		}
	}
	if !hasStart || len(events) == 0 {
		return cands, 0
	}
	for _, s := range cands {
		if g.nodes[s.Path[0]].Kind == "start" && (len(s.Stimuli) == 0 || tailOfAny(s.Path[1:], events)) {
			dropped++
			continue
		}
		kept = append(kept, s)
	}
	return kept, dropped
}

// tailOfAny reports whether tail is a suffix of some path in paths.
func tailOfAny(tail []string, paths [][]string) bool {
	for _, p := range paths {
		if len(p) >= len(tail) && slices.Equal(p[len(p)-len(tail):], tail) {
			return true
		}
	}
	return false
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

// stimuli walks the path. At each node it opens one stimulus per dynamic-view
// call into a component (A1, callStimuli) — or, for an input-classified node
// with no such call, the single §2.2 input stimulus — then attaches the node's
// outputs/states/hints to the last stimulus open (preconditions before the first).
// A node visited more than once (a loop unrolled) takes, at each visit, the
// guard taken at that visit.
func stimuli(in Input, g graph, d Diagram, view *View, rp rawPath) (Outcome, []Stimulus) {
	w := stimulusWalk{hints: hintsByAnchor(d)}
	guards := visitGuards(rp)
	for _, id := range rp.nodes {
		n := g.nodes[id]
		cls := classify(n, in, view)
		guard := guards.next(id)
		evs := callStimuli(in, d, view, n, guard)
		if len(evs) == 0 && cls == classInput {
			evs = []InputEv{inputEvent(in, n, guard)}
		}
		for _, ev := range evs {
			w.out = append(w.out, Stimulus{Seq: len(w.out) + 1, Node: id, Input: ev})
		}
		w.observe(n, cls)
	}
	return w.pre, w.out
}

// guardCursor hands out, per node, the guards taken there in path order: the
// i-th call of next(node) is the guard of the node's i-th visit ("" once the
// node's guards run out, e.g. a path that ends on it).
type guardCursor map[string][]string

func visitGuards(rp rawPath) guardCursor {
	c := guardCursor{}
	for _, gd := range rp.guards {
		c[gd.At] = append(c[gd.At], gd.Guard)
	}
	return c
}

func (c guardCursor) next(node string) string {
	gs := c[node]
	if len(gs) == 0 {
		return ""
	}
	c[node] = gs[1:]
	return gs[0]
}

// stimulusWalk is the state of one stimuli walk.
type stimulusWalk struct {
	hints map[string][]string
	pre   Outcome
	out   []Stimulus
}

// cur is the outcome observations attach to: the last stimulus, or preconditions.
func (w *stimulusWalk) cur() *Outcome {
	if len(w.out) == 0 {
		return &w.pre
	}
	return &w.out[len(w.out)-1].Outcome
}

// observe attaches what node n contributes as an observation: its output or
// state by classification, then any notes anchored to it.
func (w *stimulusWalk) observe(n Node, cls class) {
	switch cls {
	case classOutput:
		c := w.cur()
		c.ExpectedOutputs = append(c.ExpectedOutputs, outputOf(n))
	case classState:
		c := w.cur()
		c.ExpectedStates = append(c.ExpectedStates, State{Node: n.ID, Object: n.Label, InState: n.InState})
	case classInput, classInternal, classHint, classStructural:
	}
	if hs, ok := w.hints[n.ID]; ok {
		c := w.cur()
		c.Hints = append(c.Hints, hs...)
	}
}

// callGroup is the calls of one node that land as one stimulus: a single call,
// or the calls of one alt group into the same component (A4).
type callGroup struct {
	idx   int // index of the first call, for provenance
	calls []Call
}

// callStimuli is Amendment A1/A2/A4 for node n. Every call into a component
// yields a stimulus into it, in call order. Calls sharing an Alt tag into the
// same component are alternatives and collapse to one stimulus whose Via lists
// the alternative callers; alternatives into different components (e.g. the
// web and MCP clients) each keep their own. Each op the call's label names (A2)
// is its own stimulus, with the call's provenance and an index suffix; a label
// naming none leaves Op "". Kind is "call" when the caller is a component and
// the node's kind otherwise; a decision/switch node's guard rides on each.
func callStimuli(in Input, d Diagram, view *View, n Node, guard string) []InputEv {
	var groups []*callGroup
	byAlt := map[string]*callGroup{}
	for i, c := range stepCalls(view, n.ID) {
		if !isComponent(in, c.To) {
			continue
		}
		if c.Alt != "" {
			key := c.Alt + "\x00" + Normalize(c.To)
			if g, ok := byAlt[key]; ok {
				g.calls = append(g.calls, c)
				continue
			}
			g := &callGroup{idx: i, calls: []Call{c}}
			byAlt[key] = g
			groups = append(groups, g)
			continue
		}
		groups = append(groups, &callGroup{idx: i, calls: []Call{c}})
	}
	var out []InputEv
	for _, g := range groups {
		out = append(out, groupStimuli(in, d, n, guard, g)...)
	}
	return out
}

// groupStimuli expands one call group into one stimulus per resolved op.
func groupStimuli(in Input, d Diagram, n Node, guard string, g *callGroup) []InputEv {
	first := g.calls[0]
	base := InputEv{Kind: n.Kind, From: first.From, To: Normalize(first.To)}
	if isComponent(in, first.From) {
		base.Kind = "call"
	}
	if n.Kind == "decision" || n.Kind == "switch" {
		base.Guard = guard
	}
	if len(g.calls) > 1 {
		for _, c := range g.calls {
			base.Via = append(base.Via, c.From)
		}
	}
	call := fmt.Sprintf("%s/%s/%d", d.UseCaseID, n.ID, g.idx)
	ops := groupOps(in, g.calls)
	if len(ops) == 0 {
		base.Call = call
		return []InputEv{base}
	}
	out := make([]InputEv, 0, len(ops))
	for k, op := range ops {
		ev := base
		ev.Via = append([]string(nil), base.Via...)
		ev.Op = op
		ev.Call = call
		if len(ops) > 1 {
			ev.Call = fmt.Sprintf("%s.%d", call, k)
		}
		out = append(out, ev)
	}
	return out
}

// groupOps is the union, in call then label order and without repeats, of the
// ops every call of a group resolves (A2).
func groupOps(in Input, calls []Call) []string {
	var ops []string
	seen := map[string]bool{}
	for _, c := range calls {
		for _, op := range resolveOps(in, c.To, c.Label) {
			if !seen[op] {
				seen[op] = true
				ops = append(ops, op)
			}
		}
	}
	return ops
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

// inputEvent is the §2.2 input of an input-classified node that makes no call
// into a component: the actor (or deciding actor) is the source, and the
// rule-5 component-lane fallback supplies the target.
func inputEvent(in Input, n Node, guard string) InputEv {
	ev := InputEv{Kind: n.Kind, To: componentLane(in, n)}
	if n.Kind == "decision" || n.Kind == "switch" {
		ev.From = n.DecidedBy
		ev.Guard = guard
	} else {
		ev.From = n.LinkedActorID
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
