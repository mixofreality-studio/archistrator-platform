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
// CROSS-PRODUCTED (each branch's own alternative set computed exactly once, not
// once per already-accumulated combination — see the 2026-07-30 fix-round-1 note on
// walkFork below) and concatenated, in declared order, into the SAME path
// (fork-without-join is legal: nothing requires the branches to reconverge at a
// join). An end node always terminates its walk, regardless of any outgoing edge it
// might carry. A path also terminates whenever no eligible outgoing edge remains.
//
// Loops (back-edges) are traversed AT MOST ONCE per path: the walk tracks a
// per-path visited-EDGE set (edges are identified by their index into
// ActivityDiagram.Edges, not by node id — a node MAY be revisited via a different
// edge even after its first visit). This bounds every path to a finite length
// without needing a separate cycle-detection pass over node ids.
//
// Total enumerated paths are capped at maxActivityPaths per diagram (across ALL
// entries): activityPaths computes the FULL, uncapped result set first, then
// truncates it to the first maxActivityPaths entries in deterministic (entry,
// then declared-edge-order-within-entry) order. The cap is applied EXACTLY ONCE,
// at this single top-level point — nothing inside walkActivity/branchOverEdges/
// walkFork is budget-aware. This is a deliberate fix (2026-07-30 fix-round-1) for a
// defect in an earlier version that threaded a shared decrement-per-terminal
// "budget" through the recursion: a fork branch containing internal decision/
// switch branching was RECOMPUTED once per already-accumulated sibling
// combination, over-charging the budget relative to the actual number of final
// combinations produced, and — worse — silently dropping legitimate combinations
// when the (over-charged) budget ran out mid-fork, before every combination had
// been produced. Computing fully and truncating once, at the very end, cannot
// under- or over-charge and cannot drop an asymmetric subset: the returned set is
// always an exact, order-preserving prefix of the true, complete result.

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
// across its cross-producted branches, and so sibling decision/switch branches
// each start from an independent copy of the pre-branch state.
type activityWalk struct {
	seq     []string
	visited map[int]bool
}

// activityPaths enumerates every entry→end node-id path of a: entries are start
// nodes plus event nodes (any event node is a root, wherever it sits); decision/
// switch branches enumerate; loops (back-edges) are traversed at most once; fork
// branches cross-product and concatenate in declared edge order (fork-without-join
// supported); paths terminate at end nodes or when no outgoing edge remains. The
// full result is computed uncapped, then truncated to maxActivityPaths (see the
// file-level comment above for why the cap is applied this way, exactly once).
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

	var out []struct {
		Entry pathEntry
		Nodes []string
	}
	for _, entry := range entries {
		for _, w := range walkActivity(entry.NodeID, a.Edges, kindByID, edgesByFrom, map[int]bool{}) {
			out = append(out, struct {
				Entry pathEntry
				Nodes []string
			}{Entry: entry, Nodes: w.seq})
		}
	}
	if len(out) > maxActivityPaths {
		out = out[:maxActivityPaths]
	}
	return out
}

// walkActivity performs the recursive DFS described on activityPaths, returning
// every completed walk starting at nodeID given the edges already visited on the
// path so far. It is NOT budget-aware — see activityPaths, which applies
// maxActivityPaths exactly once, as a final truncation of the complete result.
func walkActivity(nodeID string, edges []ActivityEdge, kindByID map[string]string, edgesByFrom map[string][]int, visited map[int]bool) []activityWalk {
	eligible := eligibleEdges(nodeID, edgesByFrom, visited)

	// An end node always terminates, even if it happens to carry an outgoing edge;
	// otherwise, a node with no eligible (unvisited) outgoing edge left terminates
	// too — this is what bounds a loop to being traversed at most once.
	if kindByID[nodeID] == nodeEnd || len(eligible) == 0 {
		return []activityWalk{{seq: []string{nodeID}, visited: visited}}
	}

	if kindByID[nodeID] == nodeFork {
		return walkFork(nodeID, eligible, edges, kindByID, edgesByFrom, visited)
	}

	// Default: decision/switch (and, degenerately, any single-outgoing-edge node
	// such as action/merge/join/note/swimLane/loop/goto/interruptEdge) — one walk
	// per eligible outgoing edge. With exactly one eligible edge this is a plain
	// straight-line continuation; with >=2 it is the "branches enumerate" case.
	return branchOverEdges(nodeID, eligible, edges, kindByID, edgesByFrom, visited)
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
func branchOverEdges(nodeID string, eligible []int, edges []ActivityEdge, kindByID map[string]string, edgesByFrom map[string][]int, visited map[int]bool) []activityWalk {
	var out []activityWalk
	for _, idx := range eligible {
		v := cloneVisited(visited)
		v[idx] = true
		for _, sub := range walkActivity(edges[idx].To, edges, kindByID, edgesByFrom, v) {
			out = append(out, activityWalk{seq: append([]string{nodeID}, sub.seq...), visited: sub.visited})
		}
	}
	return out
}

// walkFork implements the fork's semantics: unlike a decision/switch, a fork's
// outgoing edges do NOT branch into ALTERNATIVE paths — every branch is taken, so
// their walks are combined into the SAME path. But a branch itself may contain
// internal decision/switch branching, in which case it contributes MULTIPLE
// alternative walks of its own — those must be CROSS-PRODUCTED against whatever
// the other branches contribute, not treated as more alternatives at the fork
// itself.
//
// 2026-07-30 fix-round-1: each branch's own alternative set is computed EXACTLY
// ONCE, independent of how many combinations have been accumulated from earlier
// branches so far (branches() below) — an earlier version instead recomputed a
// later branch's walkActivity once per already-accumulated combination, which
// double-charged (and, under the old budget scheme, could silently drop) the cost
// of an asymmetric branch shape like "branch 1 has 2 internal alternatives, branch
// 2 has 1". Computing once and cross-producting (crossProduct below) makes the
// work — and, now that capping is a single final truncation, the correctness —
// depend only on the true number of alternatives per branch, not on evaluation
// order.
func walkFork(nodeID string, eligible []int, edges []ActivityEdge, kindByID map[string]string, edgesByFrom map[string][]int, visited map[int]bool) []activityWalk {
	branches := make([][]activityWalk, len(eligible))
	for i, idx := range eligible {
		v := cloneVisited(visited)
		v[idx] = true
		branches[i] = walkActivity(edges[idx].To, edges, kindByID, edgesByFrom, v)
	}

	partials := []activityWalk{{seq: nil, visited: visited}}
	for _, branch := range branches {
		partials = crossProduct(partials, branch)
	}

	out := make([]activityWalk, 0, len(partials))
	for _, p := range partials {
		out = append(out, activityWalk{seq: append([]string{nodeID}, p.seq...), visited: p.visited})
	}
	return out
}

// crossProduct combines every in-progress partial with every alternative of the
// next branch, in order (partials outer, branch inner — so branch N's
// alternatives vary fastest), concatenating sequences and UNIONING visited-edge
// sets (each side is already a superset of the pre-fork visited set, so the union
// correctly reflects everything consumed by every branch folded in so far).
func crossProduct(partials, branch []activityWalk) []activityWalk {
	var next []activityWalk
	for _, p := range partials {
		for _, b := range branch {
			next = append(next, activityWalk{
				seq:     append(append([]string{}, p.seq...), b.seq...),
				visited: unionVisited(p.visited, b.visited),
			})
		}
	}
	return next
}

// cloneVisited copies a visited-edge set so a branch point (decision/switch
// alternatives, or a fork's own branches) can extend it independently without
// mutating the state its siblings (or its own caller) still hold.
func cloneVisited(v map[int]bool) map[int]bool {
	out := make(map[int]bool, len(v)+1)
	for k := range v {
		out[k] = true
	}
	return out
}

// unionVisited merges two visited-edge sets (see crossProduct).
func unionVisited(a, b map[int]bool) map[int]bool {
	out := make(map[int]bool, len(a)+len(b))
	for k := range a {
		out[k] = true
	}
	for k := range b {
		out[k] = true
	}
	return out
}
