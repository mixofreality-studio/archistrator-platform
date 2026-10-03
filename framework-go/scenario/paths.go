package scenario

import (
	"fmt"
	"sort"
)

// graph is one diagram indexed for the walk. out-edges are sorted by
// (To, Guard): that order is the canonical ordering behind scenario ids (§2.3).
type graph struct {
	nodes map[string]Node
	out   map[string][]Edge
	in    map[string]int
}

func buildGraph(d Diagram) (graph, error) {
	g := graph{nodes: map[string]Node{}, out: map[string][]Edge{}, in: map[string]int{}}
	for _, n := range d.Nodes {
		if _, dup := g.nodes[n.ID]; dup {
			return g, fmt.Errorf("use case %s: duplicate node id %q", d.UseCaseID, n.ID)
		}
		if !knownKind(n.Kind) {
			return g, fmt.Errorf("use case %s: node %q has unknown kind %q", d.UseCaseID, n.ID, n.Kind)
		}
		g.nodes[n.ID] = n
	}
	if err := g.addEdges(d); err != nil {
		return g, err
	}
	for k := range g.out {
		sortEdges(g.out[k])
	}
	return g, nil
}

func (g graph) addEdges(d Diagram) error {
	for _, e := range d.Edges {
		if _, ok := g.nodes[e.From]; !ok {
			return fmt.Errorf("use case %s: edge from unknown node %q", d.UseCaseID, e.From)
		}
		if _, ok := g.nodes[e.To]; !ok {
			return fmt.Errorf("use case %s: edge to unknown node %q", d.UseCaseID, e.To)
		}
		g.out[e.From] = append(g.out[e.From], e)
		g.in[e.To]++
	}
	return nil
}

func sortEdges(es []Edge) {
	sort.SliceStable(es, func(i, j int) bool {
		if es[i].To != es[j].To {
			return es[i].To < es[j].To
		}
		return es[i].Guard < es[j].Guard
	})
}

func knownKind(k string) bool {
	switch k {
	case "start", "action", "decision", "merge", "fork", "join", "end", "swimLane", "note", "loop",
		"switch", "goto", "interruptEdge", "timeEvent", "acceptEvent", "sendSignal", "objectNode":
		return true
	}
	return false
}

// entries: start nodes, plus timeEvent/acceptEvent nodes with no in-edge. Sorted:
// start first, then by id.
func (g graph) entries() []string {
	var starts, events []string
	for id, n := range g.nodes {
		switch {
		case n.Kind == "start":
			starts = append(starts, id)
		case (n.Kind == "timeEvent" || n.Kind == "acceptEvent") && g.in[id] == 0:
			events = append(events, id)
		}
	}
	sort.Strings(starts)
	sort.Strings(events)
	return append(starts, events...)
}

// rawPath is a node sequence with the guards taken along it.
type rawPath struct {
	nodes  []string
	guards []Guard
}

// extend returns a copy of p with id appended.
func (p rawPath) extend(id string) rawPath {
	return rawPath{nodes: append(append([]string{}, p.nodes...), id), guards: p.guards}
}

// withGuard returns a copy of p's guards with the guard of edge e (out of a
// node of kind fromKind) recorded when the edge is guarded or leaves a branch
// node — so an unguarded decision branch is still recorded, with guard "".
func (p rawPath) withGuard(fromKind string, e Edge) rawPath {
	if e.Guard == "" && !isBranch(fromKind) {
		return p
	}
	return rawPath{nodes: p.nodes, guards: append(append([]Guard{}, p.guards...), Guard{At: e.From, Guard: e.Guard})}
}

// backEdge is a loop edge u->v met during the elementary walk, with the walk's
// prefix (entry..v..u) at the moment it was met.
type backEdge struct {
	edge   Edge
	prefix rawPath
}

// elementary enumerates simple paths (no repeated node) from entry to any end
// node, in canonical DFS order. It also records back-edges encountered.
func (g graph) elementary(entry string) (paths []rawPath, backEdges []backEdge) {
	w := &walker{g: g, onPath: map[string]bool{}}
	w.walk(entry, rawPath{})
	return w.paths, w.backs
}

type walker struct {
	g      graph
	onPath map[string]bool
	paths  []rawPath
	backs  []backEdge
}

func (w *walker) walk(cur string, p rawPath) {
	p = p.extend(cur)
	w.onPath[cur] = true
	defer delete(w.onPath, cur)
	if w.g.nodes[cur].Kind == "end" || len(w.g.out[cur]) == 0 {
		w.paths = append(w.paths, p)
		return
	}
	for _, e := range w.g.out[cur] {
		if w.onPath[e.To] {
			w.backs = append(w.backs, backEdge{edge: e, prefix: p})
			continue
		}
		w.walk(e.To, p.withGuard(w.g.nodes[cur].Kind, e))
	}
}

func isBranch(kind string) bool {
	return kind == "decision" || kind == "switch" || kind == "interruptEdge" || kind == "loop"
}

// unrollOnce: for each back-edge u->v, emit the walk prefix (entry..v..u),
// then v again, then the continuation after v of the lowest-ordered elementary
// path through v. When u itself lies on that path this is
// prefix[..u] + body[v..u] + suffix(after u); when the body dead-ends into
// the back-edge (v..u never reaches an end) it is still the loop taken once.
func unrollOnce(paths []rawPath, backEdges []backEdge) []rawPath {
	var out []rawPath
	seen := map[string]bool{}
	for _, b := range backEdges {
		key := b.edge.From + "->" + b.edge.To
		if seen[key] {
			continue
		}
		seen[key] = true
		if rp, ok := spliceLoop(paths, b); ok {
			out = append(out, rp)
		}
	}
	return out
}

func spliceLoop(paths []rawPath, b backEdge) (rawPath, bool) {
	for _, p := range paths {
		vi := index(p.nodes, b.edge.To)
		if vi < 0 {
			continue
		}
		nodes := append([]string{}, b.prefix.nodes...)
		nodes = append(nodes, p.nodes[vi:]...)
		guards := append([]Guard{}, b.prefix.guards...)
		guards = append(guards, Guard{At: b.edge.From, Guard: b.edge.Guard})
		for _, gd := range p.guards {
			if index(p.nodes, gd.At) >= vi {
				guards = append(guards, gd)
			}
		}
		return rawPath{nodes: nodes, guards: guards}, true
	}
	return rawPath{}, false
}

func index(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}
