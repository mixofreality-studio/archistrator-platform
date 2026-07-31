package methodcheck

// activitypaths.go is PURE, package-private plumbing (no Finding/RuleID of its own)
// that enumerates every entry→end node-id path through an ActivityDiagram. It is
// the shared traversal Task 5's CC-PATH-CONNECTED rule (2026-07-30
// callchain-realization) consumes to check that every activity-diagram path is
// realized by a DynamicView call chain.
//
// Entries are every "start" node PLUS every UML event node (kindTimeEvent /
// kindAcceptEvent) — an event node is a root wherever it sits in the diagram, edge
// or no edge, per the Task-3 relaxation (see kindTimeEvent's doc comment in
// project.go). Decision/switch nodes (and, in fact, any node with more than one
// remaining eligible outgoing edge) branch: one walk per outgoing edge. A fork node
// is the one exception — it does NOT branch; its outgoing edges' walks are
// concatenated, in declared order, into the SAME path (fork-without-join is legal:
// nothing requires the branches to reconverge at a join). An end node always
// terminates its walk, regardless of any outgoing edge it might carry. A path also
// terminates whenever no eligible outgoing edge remains.
//
// Loops (back-edges) are traversed AT MOST ONCE per path: the walk tracks a
// per-path visited-EDGE set (edges are identified by their index into
// ActivityDiagram.Edges, not by node id — a node MAY be revisited via a different
// edge even after its first visit). This bounds every path to a finite length
// without needing a separate cycle-detection pass over node ids.
//
// Total enumerated paths are capped at maxActivityPaths per diagram (across ALL
// entries) as a defensive bound on pathological fork/decision combinatorics; the
// function returns whatever it enumerated before hitting the cap rather than
// erroring — Task 5's rule is responsible for noting the cap in its message if the
// count lands exactly on it.

// maxActivityPaths caps the total number of enumerated paths per diagram.
const maxActivityPaths = 512

// pathEntry describes one enumeration root of an activity diagram.
type pathEntry struct {
	NodeID string
	Kind   string // "start", "timeEvent", "acceptEvent"
}

// activityWalk is one in-progress (or completed) DFS walk: the node-id sequence
// produced so far, plus the set of edge indices (into the diagram's Edges slice)
// already consumed along it — carried so a fork can thread consumption forward
// across its sequentially concatenated branches, and so sibling decision/switch
// branches each start from an independent copy of the pre-branch state.
type activityWalk struct {
	seq     []string
	visited map[int]bool
}

// activityPaths enumerates every entry→end node-id path of a: entries are start
// nodes plus event nodes (any event node is a root, wherever it sits); decision/
// switch branches enumerate; loops (back-edges) are traversed at most once; fork
// branches are concatenated in declared edge order (fork-without-join supported);
// paths terminate at end nodes or when no outgoing edge remains.
func activityPaths(a ActivityDiagram) []struct {
	Entry pathEntry
	Nodes []string // node ids in walk order, Entry.NodeID first
} {
	kindByID := make(map[string]string, len(a.Nodes))
	for _, n := range a.Nodes {
		kindByID[n.ID] = n.Kind
	}

	// edgesByFrom groups edge INDICES (not copies) by their From node, preserving
	// the diagram's declared Edges order — that order is what decides both branch
	// order (decision/switch) and concatenation order (fork).
	edgesByFrom := make(map[string][]int, len(a.Nodes))
	for i, e := range a.Edges {
		edgesByFrom[e.From] = append(edgesByFrom[e.From], i)
	}

	var entries []pathEntry
	for _, n := range a.Nodes {
		switch n.Kind {
		case nodeStart, kindTimeEvent, kindAcceptEvent:
			entries = append(entries, pathEntry{NodeID: n.ID, Kind: n.Kind})
		}
	}

	budget := maxActivityPaths
	var out []struct {
		Entry pathEntry
		Nodes []string
	}
	for _, entry := range entries {
		if budget <= 0 {
			break
		}
		for _, w := range walkActivity(entry.NodeID, a.Edges, kindByID, edgesByFrom, map[int]bool{}, &budget) {
			out = append(out, struct {
				Entry pathEntry
				Nodes []string
			}{Entry: entry, Nodes: w.seq})
		}
	}
	return out
}

// walkActivity performs the recursive DFS described on activityPaths, returning
// every completed walk starting at nodeID given the edges already visited on the
// path so far. budget is a shared, package-wide countdown (decremented once per
// terminal walk produced, across every entry) that enforces maxActivityPaths.
func walkActivity(nodeID string, edges []ActivityEdge, kindByID map[string]string, edgesByFrom map[string][]int, visited map[int]bool, budget *int) []activityWalk {
	if *budget <= 0 {
		return nil
	}

	eligible := eligibleEdges(nodeID, edgesByFrom, visited)

	// An end node always terminates, even if it happens to carry an outgoing edge;
	// otherwise, a node with no eligible (unvisited) outgoing edge left terminates
	// too — this is what bounds a loop to being traversed at most once.
	if kindByID[nodeID] == nodeEnd || len(eligible) == 0 {
		*budget--
		return []activityWalk{{seq: []string{nodeID}, visited: visited}}
	}

	if kindByID[nodeID] == nodeFork {
		return walkFork(nodeID, eligible, edges, kindByID, edgesByFrom, visited, budget)
	}

	// Default: decision/switch (and, degenerately, any single-outgoing-edge node
	// such as action/merge/join/note/swimLane/loop/goto/interruptEdge) — one walk
	// per eligible outgoing edge. With exactly one eligible edge this is a plain
	// straight-line continuation; with >=2 it is the "branches enumerate" case.
	return branchOverEdges(nodeID, eligible, edges, kindByID, edgesByFrom, visited, budget)
}

// eligibleEdges lists nodeID's outgoing edge indices, in declared order, EXCLUDING
// any edge already consumed on this path (loop-once).
func eligibleEdges(nodeID string, edgesByFrom map[string][]int, visited map[int]bool) []int {
	var eligible []int
	for _, idx := range edgesByFrom[nodeID] {
		if !visited[idx] {
			eligible = append(eligible, idx)
		}
	}
	return eligible
}

// branchOverEdges implements the decision/switch (and single-eligible-edge
// default) case: one walk per eligible outgoing edge, each prefixed with nodeID.
// Alternatives are mutually exclusive, so each starts from an independent copy of
// the pre-branch visited set.
func branchOverEdges(nodeID string, eligible []int, edges []ActivityEdge, kindByID map[string]string, edgesByFrom map[string][]int, visited map[int]bool, budget *int) []activityWalk {
	var out []activityWalk
	for _, idx := range eligible {
		if *budget <= 0 {
			break
		}
		v := cloneVisited(visited)
		v[idx] = true
		for _, sub := range walkActivity(edges[idx].To, edges, kindByID, edgesByFrom, v, budget) {
			out = append(out, activityWalk{seq: append([]string{nodeID}, sub.seq...), visited: sub.visited})
		}
	}
	return out
}

// walkFork implements the fork's sequential-concatenation semantics: unlike a
// decision/switch, a fork's outgoing edges do NOT branch into alternative paths —
// each one's walk is appended, in declared order, into the SAME path.
func walkFork(nodeID string, eligible []int, edges []ActivityEdge, kindByID map[string]string, edgesByFrom map[string][]int, visited map[int]bool, budget *int) []activityWalk {
	partials := []activityWalk{{seq: nil, visited: visited}}
	for _, idx := range eligible {
		if *budget <= 0 {
			break
		}
		partials = forkAdvance(idx, partials, edges, kindByID, edgesByFrom, budget)
	}
	out := make([]activityWalk, 0, len(partials))
	for _, p := range partials {
		out = append(out, activityWalk{seq: append([]string{nodeID}, p.seq...), visited: p.visited})
	}
	return out
}

// forkAdvance concatenates one fork branch's walk (edge idx) onto every
// in-progress partial, in sequence — the cross product of "already concatenated
// so far" x "this branch's own alternatives" (present when the branch itself
// contains internal decision/switch branching).
func forkAdvance(idx int, partials []activityWalk, edges []ActivityEdge, kindByID map[string]string, edgesByFrom map[string][]int, budget *int) []activityWalk {
	var next []activityWalk
	for _, p := range partials {
		if *budget <= 0 {
			break
		}
		v := cloneVisited(p.visited)
		v[idx] = true
		for _, sub := range walkActivity(edges[idx].To, edges, kindByID, edgesByFrom, v, budget) {
			next = append(next, activityWalk{
				seq:     append(append([]string{}, p.seq...), sub.seq...),
				visited: sub.visited,
			})
		}
	}
	return next
}

// cloneVisited copies a visited-edge set so a branch point (decision/switch
// alternatives, or a fork's next sequential edge) can extend it independently
// without mutating the state its sibling branches (or its own caller) still hold.
func cloneVisited(v map[int]bool) map[int]bool {
	out := make(map[int]bool, len(v)+1)
	for k := range v {
		out[k] = true
	}
	return out
}
