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
// TWO BOUNDS, ONE FOR EACH FAILURE MODE:
//
//   - The OUTPUT cap (maxActivityPaths) truncates the returned set. It is applied
//     EXACTLY ONCE, as a final truncation of whatever was enumerated, and nothing
//     inside the walk is cap-aware. This is a deliberate fix (2026-07-30
//     fix-round-1) for a defect in an earlier version that threaded a shared
//     decrement-per-TERMINAL "budget" through the recursion: a fork branch
//     containing internal decision/switch branching was RECOMPUTED once per
//     already-accumulated sibling combination, over-charging relative to the number
//     of final combinations produced, and — worse — silently dropping legitimate
//     combinations mid-fork. Charging per terminal is what made that scheme
//     asymmetric; capping the output once, at the end, cannot.
//   - The WORK budget (maxWalkWork) bounds the RECURSION itself (rollout rulings
//     2026-07-31). Truncating the output alone still requires materializing the
//     complete result first, which is exponential in nested fork×decision depth —
//     and designhealth runs this render-on-read over committed state, so one
//     pathological diagram is a CPU/memory sink. The budget is charged per WALK-STEP
//     (one node id materialized into a sequence — see spend/carry), never per
//     final path, so it cannot repeat the fix-round-1 asymmetry: it measures work
//     actually done rather than results produced.
//
// On exhaustion the walk stops EXPLORING; the walks it had already COMPLETED are
// still carried up to the caller, capped at maxActivityPaths per level (carry), so a
// blowup degrades to a smaller — often still cap-sized — answer instead of an empty
// or fabricated one. The degradation is deterministic (entries in declared order,
// branches in declared edge order) and only ever UNDER-approximates: every returned
// path is a real, complete path of the diagram, so CC-PATH-CONNECTED can lose a
// finding to a pathological diagram but can never gain a false one. For the shape
// that actually dominates real data — decisions, not forks — the returned set is in
// fact the EXACT prefix the output cap would have returned had the walk been
// unbounded (pinned by TestPaths_BudgetTruncationMatchesTheCapPrefix).

// maxActivityPaths caps the total number of enumerated paths per diagram.
const maxActivityPaths = 512

// maxWalkWork caps the enumeration's WALK-STEPS per diagram. One step is one node id
// MATERIALIZED into a walk sequence — see spend and carry, which between them
// charge every sequence the walk builds, both where one is created and where one is
// COPIED a level up. Charging the copies is what makes the budget a memory bound and
// not merely a CPU one: the charged phase materializes maxWalkWork node ids, full
// stop, rather than maxWalkWork walks each carrying a whole path.
//
// SIZING (measured 2026-07-31, fix round 1 — the numbers, not a rule of thumb):
//
//   - Committed data is the anchor: the most expensive of the 16 committed activity
//     diagrams (drive-system-design, 25 nodes, 22 paths) costs 2,605 steps and every
//     other one is under 1,900. The budget is ~380x the worst real diagram. Node count
//     alone bounds nothing — the adversarial fixtures below are 50-70 nodes — which is
//     why the honest claim is stated in terms of the OUTPUT CAP:
//   - any diagram whose COMPLETE enumeration would fit the cap (<=512 paths, <=40
//     nodes deep, widest admitted fan) costs at most ~550k steps, so it is never
//     budget-truncated: its result is bit-identical to an unbounded walk's.
//   - Blowups measured at this budget: a 22-decision reconverging chain (4.2M true
//     paths) returns a full 512 paths having allocated 63MB (peak heap 4MB); an
//     8-branch fork of 5-way decisions (5^8 = 390,625 combinations) trips the budget
//     having allocated 92MB (peak heap 73MB). Both in well under a second. Before the
//     copies were charged, the SAME decision-shaped chain spiked ~280MB peak heap and
//     >1GB allocated inside a 250k budget — the budget bounded walk COUNT while each
//     walk carried a whole path, and decision-shaped diagrams are the realistic case
//     (37 decision nodes vs 1 fork across the committed data).
//
// Raising the budget further is not free: the fork shape allocates ~70 bytes per step
// (each combination unions two visited-edge SETS) against the decision shape's ~4, so
// a budget generous enough to fully enumerate, say, a 4,096-path chain (3.0M steps)
// would put the fork-shaped worst case back over 250MB of peak heap — the very sink
// this bound exists to prevent. That trade costs nothing real: such a diagram is
// truncated to 512 paths anyway, and the 512 it returns are the same 512.
const maxWalkWork = 1_000_000

// pathEntry describes one enumeration root of an activity diagram.
type pathEntry struct {
	NodeID string
	Kind   string // "start", "timeEvent", "acceptEvent"
}

// activityPath is one enumerated entry→end path: its root and the node ids it
// visits, in walk order.
type activityPath struct {
	Entry pathEntry
	Nodes []string // node ids in walk order, Entry.NodeID first
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

// walker carries the diagram-wide, walk-invariant enumeration state — the edge
// index, the node kinds, and the remaining work budget — so each step of the
// recursion is a method with no parameter train (the ccContext idiom).
type walker struct {
	edges       []ActivityEdge
	kindByID    map[string]string
	edgesByFrom map[string][]int // edge INDICES by From node, in declared order
	remaining   int              // walk-steps left in this diagram's budget
	exhausted   bool             // sticky: set the first time a charge is refused
}

// activityPaths enumerates every entry→end node-id path of a: entries are start
// nodes plus event nodes (any event node is a root, wherever it sits); decision/
// switch branches enumerate; loops (back-edges) are traversed at most once; fork
// branches cross-product and concatenate in declared edge order (fork-without-join
// supported); paths terminate at end nodes or when no outgoing edge remains. The
// enumeration is bounded by maxWalkWork and the result truncated to
// maxActivityPaths (see the file-level comment above for both bounds).
func activityPaths(a ActivityDiagram) []activityPath {
	paths, _ := boundedActivityPaths(a)
	return paths
}

// boundedActivityPaths is activityPaths plus the budget verdict: exhausted reports
// whether the walk stopped early because maxWalkWork ran out, which is the
// difference between "this diagram has 3 paths" and "this diagram has more paths
// than anyone can enumerate". Rules consume activityPaths (a truncated set reads
// the same either way); the walker's own tests assert on the verdict.
func boundedActivityPaths(a ActivityDiagram) (paths []activityPath, exhausted bool) {
	w := newWalker(a)
	var out []activityPath
	for _, entry := range diagramEntries(a) {
		for _, walk := range w.walkFrom(entry.NodeID, map[int]bool{}) {
			out = append(out, activityPath{Entry: entry, Nodes: walk.seq})
		}
	}
	if len(out) > maxActivityPaths {
		out = out[:maxActivityPaths]
	}
	return out, w.exhausted
}

func newWalker(a ActivityDiagram) *walker {
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
	return &walker{edges: a.Edges, kindByID: kindByID, edgesByFrom: edgesByFrom, remaining: maxWalkWork}
}

// diagramEntries lists the diagram's enumeration roots in declared node order.
func diagramEntries(a ActivityDiagram) []pathEntry {
	var entries []pathEntry
	for _, n := range a.Nodes {
		switch n.Kind {
		case nodeStart, kindTimeEvent, kindAcceptEvent:
			entries = append(entries, pathEntry{NodeID: n.ID, Kind: n.Kind})
		}
	}
	return entries
}

// spend charges n walk-steps of MATERIALIZATION — a terminal walk, a fork
// combination, or (through carry) a completed sub-walk copied a level up. Every
// sequence the walk builds goes through here, which is what makes the budget a memory
// bound and not merely a CPU one. Refusal is sticky — once the walk is out of budget
// it stays out, so the result cannot depend on the order in which the remainder of
// the recursion happened to ask.
func (w *walker) spend(n int) bool {
	if w.exhausted || w.remaining < n {
		w.exhausted = true
		return false
	}
	w.remaining -= n
	return true
}

// carry charges n walk-steps of ASSEMBLY — copying an ALREADY-COMPLETED sub-walk up
// one level — where assembled is how many walks this level has carried so far.
//
// While the budget holds, assembly is charged exactly like exploration: the copy is
// real materialization, and NOT charging it was the fix-round-1 defect (the budget
// bounded walk COUNT while each walk carried a whole path, so a decision-shaped
// blowup spiked ~280MB of heap inside a 250k budget).
//
// Once exploration is exhausted, assembly continues UNBUDGETED but capped at
// maxActivityPaths per level. That is the graceful-degradation half: the walks
// already completed must be able to reach the caller instead of being stranded one
// frame below it, and carrying more than the output cap is pure waste because the
// caller truncates to it anyway. The escape hatch is structurally bounded — at most
// cap x depth node ids per level, over at most depth levels — so it cannot reopen
// the memory hole the charging closed.
func (w *walker) carry(assembled, n int) bool {
	if !w.exhausted && w.spend(n) {
		return true
	}
	return assembled < maxActivityPaths
}

// walkFrom performs the recursive DFS described on activityPaths, returning every
// completed walk starting at nodeID given the edges already visited on the path so
// far. An EMPTY return means the budget ran out before ANYTHING below this node
// completed (an unexhausted walk always yields at least one walk — worst case, the
// terminal one — and an exhausted one still carries up whatever did complete).
// walkFork relies on that invariant to tell "this branch contributed nothing" from
// "this branch contributed fewer alternatives than it would have".
func (w *walker) walkFrom(nodeID string, visited map[int]bool) []activityWalk {
	if w.exhausted {
		return nil
	}
	eligible := eligibleEdges(nodeID, w.edgesByFrom, visited)

	// An end node always terminates, even if it happens to carry an outgoing edge;
	// otherwise, a node with no eligible (unvisited) outgoing edge left terminates
	// too — this is what bounds a loop to being traversed at most once.
	if w.kindByID[nodeID] == nodeEnd || len(eligible) == 0 {
		if !w.spend(1) {
			return nil
		}
		return []activityWalk{{seq: []string{nodeID}, visited: visited}}
	}

	if w.kindByID[nodeID] == nodeFork {
		return w.walkFork(nodeID, eligible, visited)
	}

	// Default: decision/switch (and, degenerately, any single-outgoing-edge node
	// such as action/merge/join/note/swimLane/loop/goto/interruptEdge) — one walk
	// per eligible outgoing edge. With exactly one eligible edge this is a plain
	// straight-line continuation; with >=2 it is the "branches enumerate" case.
	return w.branchOverEdges(nodeID, eligible, visited)
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
//
// A branch that comes back empty is one the budget cut short; its already-completed
// siblings are kept and returned. That is the graceful half of exhaustion: a
// decision blowup returns the alternatives enumerated before the budget ran out, in
// declared order, truncated to what carry still allows.
func (w *walker) branchOverEdges(nodeID string, eligible []int, visited map[int]bool) []activityWalk {
	var out []activityWalk
	for _, idx := range eligible {
		v := cloneVisited(visited)
		v[idx] = true
		for _, sub := range w.walkFrom(w.edges[idx].To, v) {
			if !w.carry(len(out), 1+len(sub.seq)) {
				return out
			}
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
// branches so far — an earlier version instead recomputed a later branch's walk
// once per already-accumulated combination, which double-charged (and, under the
// old budget scheme, could silently drop) the cost of an asymmetric branch shape
// like "branch 1 has 2 internal alternatives, branch 2 has 1". Computing once and
// cross-producting makes the work depend only on the true number of alternatives
// per branch, not on evaluation order.
//
// Exhaustion is ALL-OR-NOTHING at the BRANCH level, unlike a decision's graceful
// truncation: a fork path is only a real path of the diagram once EVERY branch is
// folded into it, so a fork whose branch (or whose fold) came back with NOTHING
// contributes no path at all, rather than a fabricated one missing a parallel branch
// (which could make a perfectly connected call chain look disconnected to
// CC-PATH-CONNECTED). A branch or fold that came back TRUNCATED but non-empty is a
// different matter: every walk in it is a complete walk of that branch, so folding
// them yields complete — merely fewer — combinations.
func (w *walker) walkFork(nodeID string, eligible []int, visited map[int]bool) []activityWalk {
	partials := []activityWalk{{seq: nil, visited: visited}}
	for _, idx := range eligible {
		v := cloneVisited(visited)
		v[idx] = true
		branch := w.walkFrom(w.edges[idx].To, v)
		if len(branch) == 0 {
			return nil // the budget cut this branch short — see the doc comment
		}
		partials = w.crossProduct(partials, branch)
		if len(partials) == 0 {
			return nil
		}
	}

	out := make([]activityWalk, 0, len(partials))
	for _, p := range partials {
		if !w.carry(len(out), 1+len(p.seq)) {
			break
		}
		out = append(out, activityWalk{seq: append([]string{nodeID}, p.seq...), visited: p.visited})
	}
	return out
}

// crossProduct combines every in-progress partial with every alternative of the
// next branch, in order (partials outer, branch inner — so branch N's
// alternatives vary fastest), concatenating sequences and UNIONING visited-edge
// sets (each side is already a superset of the pre-fork visited set, so the union
// correctly reflects everything consumed by every branch folded in so far). Each
// combination is charged the nodes it materializes; when the budget refuses one,
// the fold stops and walkFork abandons the fork.
func (w *walker) crossProduct(partials, branch []activityWalk) []activityWalk {
	var next []activityWalk
	for _, p := range partials {
		for _, b := range branch {
			if !w.spend(len(p.seq) + len(b.seq)) {
				return next
			}
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
