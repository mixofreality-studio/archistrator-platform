package methodcheck

import (
	"fmt"
	"runtime"
	"testing"
)

// activitypaths_test.go covers the six behaviors activityPaths must exhibit,
// one behavior per Test func, with inline ActivityDiagram literal fixtures (the
// idiom rules_test.go / rules_dynamic_test.go already use for activity diagrams).

func TestPaths_LinearStartActionEnd(t *testing.T) {
	a := ActivityDiagram{
		Nodes: []ActivityNode{
			{ID: "s", Kind: nodeStart}, {ID: "a", Kind: nodeAction}, {ID: "e", Kind: nodeEnd},
		},
		Edges: []ActivityEdge{{From: "s", To: "a"}, {From: "a", To: "e"}},
	}
	got := activityPaths(a)
	if len(got) != 1 {
		t.Fatalf("want 1 path, got %d: %+v", len(got), got)
	}
	want := []string{"s", "a", "e"}
	if !equalStrings(got[0].Nodes, want) {
		t.Fatalf("want nodes %v, got %v", want, got[0].Nodes)
	}
	if got[0].Entry.NodeID != "s" || got[0].Entry.Kind != nodeStart {
		t.Fatalf("want entry {s start}, got %+v", got[0].Entry)
	}
}

func TestPaths_DecisionBranches(t *testing.T) {
	a := ActivityDiagram{
		Nodes: []ActivityNode{{ID: "s", Kind: "start"}, {ID: "d", Kind: "decision"},
			{ID: "x", Kind: "action"}, {ID: "y", Kind: "action"}, {ID: "e", Kind: "end"}},
		Edges: []ActivityEdge{{From: "s", To: "d"}, {From: "d", To: "x", Guard: "[yes]"},
			{From: "d", To: "y", Guard: "[no]"}, {From: "x", To: "e"}, {From: "y", To: "e"}},
	}
	got := activityPaths(a)
	if len(got) != 2 {
		t.Fatalf("want 2 paths, got %d: %v", len(got), got)
	}
}

// TestPaths_LoopBackEdgeTraversedAtMostOnce builds a two-decision loop —
// d1 -> a -> d2, with d2's back-edge returning into "a" a SECOND time (via a
// different edge than the one that reached "a" the first time) — and asserts the
// walk is finite and the loop body ("a") never appears more than twice in any one
// path (it is revisited once via the back-edge, then its only outgoing edge is
// already consumed, so the second visit is itself a dead end).
func TestPaths_LoopBackEdgeTraversedAtMostOnce(t *testing.T) {
	a := ActivityDiagram{
		Nodes: []ActivityNode{
			{ID: "s", Kind: nodeStart}, {ID: "d1", Kind: nodeDecision}, {ID: "a", Kind: nodeAction},
			{ID: "d2", Kind: nodeDecision}, {ID: "e", Kind: nodeEnd},
		},
		Edges: []ActivityEdge{
			{From: "s", To: "d1"},
			{From: "d1", To: "a", Guard: "[enter]"},
			{From: "d1", To: "e", Guard: "[skip]"},
			{From: "a", To: "d2"},
			{From: "d2", To: "a", Guard: "[again]"}, // back-edge into the loop body
			{From: "d2", To: "e", Guard: "[done]"},
		},
	}
	got := activityPaths(a)
	if len(got) == 0 {
		t.Fatalf("want at least one path, got 0")
	}
	sawTwice := false
	for _, p := range got {
		count := 0
		for _, id := range p.Nodes {
			if id == "a" {
				count++
			}
		}
		if count > 2 {
			t.Fatalf("loop body must appear at most twice in any path, got %d in %v", count, p.Nodes)
		}
		if count == 2 {
			sawTwice = true
		}
	}
	if !sawTwice {
		t.Fatalf("expected at least one path where the back-edge is actually taken (loop body appears twice), got %+v", got)
	}
}

// TestPaths_ForkWithoutJoinConcatenatesBranches: a fork with two branches and no
// join must yield ONE path with both branches' walks concatenated in declared
// edge order — the fork does not branch into separate paths.
func TestPaths_ForkWithoutJoinConcatenatesBranches(t *testing.T) {
	a := ActivityDiagram{
		Nodes: []ActivityNode{
			{ID: "s", Kind: nodeStart}, {ID: "f", Kind: nodeFork},
			{ID: "x", Kind: nodeAction}, {ID: "y", Kind: nodeAction},
		},
		Edges: []ActivityEdge{
			{From: "s", To: "f"}, {From: "f", To: "x"}, {From: "f", To: "y"},
		},
	}
	got := activityPaths(a)
	if len(got) != 1 {
		t.Fatalf("want 1 path, got %d: %+v", len(got), got)
	}
	want := []string{"s", "f", "x", "y"}
	if !equalStrings(got[0].Nodes, want) {
		t.Fatalf("want nodes %v (branches concatenated in declared order), got %v", want, got[0].Nodes)
	}
}

// TestPaths_TwoEndNodesBothTerminate: a decision branching to two DISTINCT end
// nodes must produce two paths, each terminating at its own end node.
func TestPaths_TwoEndNodesBothTerminate(t *testing.T) {
	a := ActivityDiagram{
		Nodes: []ActivityNode{
			{ID: "s", Kind: nodeStart}, {ID: "d", Kind: nodeDecision},
			{ID: "e1", Kind: nodeEnd}, {ID: "e2", Kind: nodeEnd},
		},
		Edges: []ActivityEdge{
			{From: "s", To: "d"},
			{From: "d", To: "e1", Guard: "[yes]"},
			{From: "d", To: "e2", Guard: "[no]"},
		},
	}
	got := activityPaths(a)
	if len(got) != 2 {
		t.Fatalf("want 2 paths, got %d: %+v", len(got), got)
	}
	ends := map[string]bool{}
	for _, p := range got {
		ends[p.Nodes[len(p.Nodes)-1]] = true
	}
	if !ends["e1"] || !ends["e2"] {
		t.Fatalf("want paths terminating at both e1 and e2, got %+v", got)
	}
}

// TestPaths_EventNodeIsItsOwnEntry: an edge-less timeEvent node is its own entry —
// a diagram with a start-rooted path AND a timeEvent-rooted path must yield both,
// with Entry.Kind distinguishing them.
func TestPaths_EventNodeIsItsOwnEntry(t *testing.T) {
	a := ActivityDiagram{
		Nodes: []ActivityNode{
			{ID: "s", Kind: nodeStart}, {ID: "act", Kind: nodeAction}, {ID: "e", Kind: nodeEnd},
			{ID: "tick", Kind: kindTimeEvent},
		},
		Edges: []ActivityEdge{{From: "s", To: "act"}, {From: "act", To: "e"}},
	}
	got := activityPaths(a)
	if len(got) != 2 {
		t.Fatalf("want 2 paths (one per entry), got %d: %+v", len(got), got)
	}
	var sawStart, sawEvent bool
	for _, p := range got {
		switch p.Entry.Kind {
		case nodeStart:
			sawStart = true
			if !equalStrings(p.Nodes, []string{"s", "act", "e"}) {
				t.Fatalf("start-rooted path wrong, got %v", p.Nodes)
			}
		case kindTimeEvent:
			sawEvent = true
			if !equalStrings(p.Nodes, []string{"tick"}) {
				t.Fatalf("event-rooted path must be just the event node itself, got %v", p.Nodes)
			}
		}
	}
	if !sawStart || !sawEvent {
		t.Fatalf("want both a start-rooted and an event-rooted path, got %+v", got)
	}
}

// TestPaths_ForkBranchWithInternalDecisionCrossProducts is the fix-round-1
// regression test for the confirmed double-charge/silent-drop defect: fork branch
// 1 leads into its OWN internal 2-way decision (d), fork branch 2 is a plain
// terminal action (y) with no branching of its own. The two must CROSS-PRODUCT —
// branch 1's 2 alternatives x branch 2's 1 alternative — into exactly 2 final
// combinations, enumerated in declared edge order (d's guard order first, since
// branch 1 is declared before branch 2 at the fork).
func TestPaths_ForkBranchWithInternalDecisionCrossProducts(t *testing.T) {
	a := ActivityDiagram{
		Nodes: []ActivityNode{
			{ID: "s", Kind: nodeStart}, {ID: "f", Kind: nodeFork},
			{ID: "d", Kind: nodeDecision}, {ID: "x1", Kind: nodeAction}, {ID: "x2", Kind: nodeAction},
			{ID: "y", Kind: nodeAction},
		},
		Edges: []ActivityEdge{
			{From: "s", To: "f"},
			{From: "f", To: "d"}, // branch 1: has its OWN internal decision
			{From: "f", To: "y"}, // branch 2: plain terminal, no branching
			{From: "d", To: "x1", Guard: "[p]"},
			{From: "d", To: "x2", Guard: "[q]"},
		},
	}
	got := activityPaths(a)
	if len(got) != 2 {
		t.Fatalf("want 2 cross-producted combinations (branch1's 2 alternatives x branch2's 1), got %d: %+v", len(got), got)
	}
	want0 := []string{"s", "f", "d", "x1", "y"}
	want1 := []string{"s", "f", "d", "x2", "y"}
	if !equalStrings(got[0].Nodes, want0) {
		t.Fatalf("combination 0: want %v (declared order), got %v", want0, got[0].Nodes)
	}
	if !equalStrings(got[1].Nodes, want1) {
		t.Fatalf("combination 1: want %v (declared order), got %v", want1, got[1].Nodes)
	}
}

// TestPaths_CapBoundaryTruncatesDeterministically builds a diagram whose FULL,
// uncapped path count is comfortably over maxActivityPaths — via BREADTH (one
// start node with N > maxActivityPaths direct, single-hop branches to N distinct
// end nodes), not depth, so it's cheap to compute regardless of the cap. It
// asserts activityPaths returns EXACTLY maxActivityPaths paths and that they are
// precisely the first maxActivityPaths in declared edge order — a symmetric,
// deterministic prefix, never an ad hoc/asymmetric subset.
func TestPaths_CapBoundaryTruncatesDeterministically(t *testing.T) {
	n := maxActivityPaths + 8
	nodes := []ActivityNode{{ID: "s", Kind: nodeStart}}
	edges := make([]ActivityEdge, 0, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("e%d", i)
		nodes = append(nodes, ActivityNode{ID: id, Kind: nodeEnd})
		edges = append(edges, ActivityEdge{From: "s", To: id})
	}
	a := ActivityDiagram{Nodes: nodes, Edges: edges}

	got := activityPaths(a)
	if len(got) != maxActivityPaths {
		t.Fatalf("want exactly the cap (%d) paths, got %d", maxActivityPaths, len(got))
	}
	for i, p := range got {
		want := []string{"s", fmt.Sprintf("e%d", i)}
		if !equalStrings(p.Nodes, want) {
			t.Fatalf("path %d: want %v (contiguous declared-order prefix), got %v", i, want, p.Nodes)
		}
	}
}

// TestPaths_AcceptEventIsItsOwnEntry mirrors TestPaths_EventNodeIsItsOwnEntry for
// the OTHER UML event kind, kindAcceptEvent, so both event kinds are pinned as
// legal edge-less entries (not just kindTimeEvent).
func TestPaths_AcceptEventIsItsOwnEntry(t *testing.T) {
	a := ActivityDiagram{
		Nodes: []ActivityNode{{ID: "signal", Kind: kindAcceptEvent}},
	}
	got := activityPaths(a)
	if len(got) != 1 {
		t.Fatalf("want 1 path, got %d: %+v", len(got), got)
	}
	if got[0].Entry.NodeID != "signal" || got[0].Entry.Kind != kindAcceptEvent {
		t.Fatalf("want entry {signal acceptEvent}, got %+v", got[0].Entry)
	}
	if !equalStrings(got[0].Nodes, []string{"signal"}) {
		t.Fatalf("want nodes [signal], got %v", got[0].Nodes)
	}
}

// TestPaths_EventNodeWithIncomingEdgeIsStillARoot proves an event node is a root
// "wherever it sits" — even with an incoming edge from elsewhere in the diagram (a
// node "p" that itself is not an entry), the event node must STILL be enumerated
// as its own entry, in addition to whatever reaches it via that incoming edge.
func TestPaths_EventNodeWithIncomingEdgeIsStillARoot(t *testing.T) {
	a := ActivityDiagram{
		Nodes: []ActivityNode{
			{ID: "p", Kind: nodeAction}, {ID: "evt", Kind: kindAcceptEvent},
			{ID: "a", Kind: nodeAction}, {ID: "e", Kind: nodeEnd},
		},
		Edges: []ActivityEdge{
			{From: "p", To: "evt"}, // incoming edge into the event node
			{From: "evt", To: "a"}, {From: "a", To: "e"},
		},
	}
	got := activityPaths(a)
	if len(got) != 1 {
		t.Fatalf("want 1 path (evt is the only entry; p is not start/event so it roots nothing), got %d: %+v", len(got), got)
	}
	if got[0].Entry.NodeID != "evt" || got[0].Entry.Kind != kindAcceptEvent {
		t.Fatalf("want entry {evt acceptEvent}, got %+v", got[0].Entry)
	}
	if !equalStrings(got[0].Nodes, []string{"evt", "a", "e"}) {
		t.Fatalf("want nodes [evt a e], got %v", got[0].Nodes)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestPaths_BudgetBoundsNestedForkDecision is the compute-bound regression: a
// nested fork×decision diagram — ONE fork of 8 branches, each branch its own 5-way
// decision — whose complete cross-product is 5^8 = 390,625 combinations, ~760x the
// output cap. Truncating the FULL result at the end (the pre-bound behavior) means
// materializing all 390,625 first, which is the CPU/memory sink designhealth would
// hit render-on-read on a pathological committed diagram. The walker must instead
// stop WORKING once the budget is spent: the assertion is that the budget actually
// bound (exhausted), that the output still honors the cap, and that the paths
// completed BEFORE the blowup — here the start-rooted entry, declared first — are
// returned rather than lost.
func TestPaths_BudgetBoundsNestedForkDecision(t *testing.T) {
	nodes := []ActivityNode{
		{ID: "s", Kind: nodeStart}, {ID: "sa", Kind: nodeAction}, {ID: "se", Kind: nodeEnd},
		{ID: "tick", Kind: kindTimeEvent}, {ID: "f", Kind: nodeFork},
	}
	edges := []ActivityEdge{{From: "s", To: "sa"}, {From: "sa", To: "se"}, {From: "tick", To: "f"}}
	for b := 0; b < 8; b++ {
		d := fmt.Sprintf("d%d", b)
		nodes = append(nodes, ActivityNode{ID: d, Kind: nodeDecision, Label: "branch?"})
		edges = append(edges, ActivityEdge{From: "f", To: d})
		for i := 0; i < 5; i++ {
			leaf := fmt.Sprintf("a%d-%d", b, i)
			nodes = append(nodes, ActivityNode{ID: leaf, Kind: nodeAction, Label: leaf})
			edges = append(edges, ActivityEdge{From: d, To: leaf, Kind: edgeGuardedFlow, Guard: "[g]"})
		}
	}

	got, exhausted := boundedActivityPaths(ActivityDiagram{Nodes: nodes, Edges: edges})
	if !exhausted {
		t.Fatalf("a 5^8 cross-product must exhaust the %d-step budget; it enumerated fully instead", maxWalkWork)
	}
	if len(got) > maxActivityPaths {
		t.Fatalf("want at most the cap (%d) paths, got %d", maxActivityPaths, len(got))
	}
	if len(got) == 0 || !equalStrings(got[0].Nodes, []string{"s", "sa", "se"}) {
		t.Fatalf("the entry completed BEFORE the blowup must survive the budget; got %+v", got)
	}
}

// decisionChainDiagram builds the DECISION-shaped blowup: stages reconverging 2-way
// decisions in a row (d → {a|b} → m → next d), so the true path count is 2^stages and
// every path runs the full length of the chain. This is the shape that dominates real
// data — 37 decision nodes against 1 fork across the committed diagrams — and the one
// a fork-only adversarial test misses entirely.
func decisionChainDiagram(stages int) ActivityDiagram {
	nodes := []ActivityNode{{ID: "s", Kind: nodeStart}}
	var edges []ActivityEdge
	prev := "s"
	for i := 0; i < stages; i++ {
		d, a, b, m := fmt.Sprintf("d%d", i), fmt.Sprintf("a%d", i), fmt.Sprintf("b%d", i), fmt.Sprintf("m%d", i)
		nodes = append(nodes,
			ActivityNode{ID: d, Kind: nodeDecision, Label: "branch?"},
			ActivityNode{ID: a, Kind: nodeAction, Label: a},
			ActivityNode{ID: b, Kind: nodeAction, Label: b},
			ActivityNode{ID: m, Kind: nodeMerge})
		edges = append(edges,
			ActivityEdge{From: prev, To: d},
			ActivityEdge{From: d, To: a, Kind: edgeGuardedFlow, Guard: "[y]"},
			ActivityEdge{From: d, To: b, Kind: edgeGuardedFlow, Guard: "[n]"},
			ActivityEdge{From: a, To: m}, ActivityEdge{From: b, To: m})
		prev = m
	}
	nodes = append(nodes, ActivityNode{ID: "e", Kind: nodeEnd})
	edges = append(edges, ActivityEdge{From: prev, To: "e"})
	return ActivityDiagram{Nodes: nodes, Edges: edges}
}

// allocatedBytes reports how many bytes fn allocated. Coarse by design — the numbers
// it guards differ by an order of magnitude, not by a few percent.
func allocatedBytes(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// TestPaths_BudgetBoundsDecisionChain is the DECISION-shaped counterpart to the fork
// test above, and the regression for fix-round-1 finding I1. The budget used to charge
// ONE step per sub-walk carried up a level, while the copy that step paid for was a
// whole path long — so this diagram (22 reconverging decisions, 4.2M true paths, no
// fork anywhere) spiked ~280MB of peak heap and allocated >1GB inside a 250k budget,
// with CPU never looking alarming. Charging the copies BY LENGTH makes materialization
// scale with the budget instead of with budget x path-length x depth: measured after
// the fix, 63MB allocated and 4MB peak heap, and the walk still returns a full
// cap-sized answer. The ceiling asserted here is deliberately loose — an order of
// magnitude under the pre-fix >1GB, several times over the measured 63MB — so it
// catches the regression CLASS without pinning an allocator profile.
func TestPaths_BudgetBoundsDecisionChain(t *testing.T) {
	a := decisionChainDiagram(22)
	var got []activityPath
	var exhausted bool
	alloc := allocatedBytes(func() { got, exhausted = boundedActivityPaths(a) })

	if !exhausted {
		t.Fatalf("2^22 paths must exhaust the %d-step budget; it enumerated fully instead", maxWalkWork)
	}
	if len(got) > maxActivityPaths {
		t.Fatalf("want at most the cap (%d) paths, got %d", maxActivityPaths, len(got))
	}
	if len(got) == 0 {
		t.Fatalf("a decision blowup must still return the paths it completed, got none")
	}
	const ceiling = 256 << 20
	if alloc > ceiling {
		t.Fatalf("a decision-shaped blowup allocated %dMB, over the %dMB ceiling; the budget is bounding walk COUNT again rather than materialization",
			alloc>>20, uint64(ceiling)>>20)
	}
}

// TestPaths_BudgetTruncationMatchesTheCapPrefix pins what a budget-truncated answer
// costs the caller on that same realistic shape: nothing. A 12-decision chain has
// 4,096 true paths — 8x the output cap — and enumerating it fully costs 3.0M steps, so
// the budget truncates it. The paths it returns are nevertheless bit-identical to the
// first maxActivityPaths of the unbounded enumeration: same answer, less work. Only
// the `exhausted` verdict distinguishes the two.
func TestPaths_BudgetTruncationMatchesTheCapPrefix(t *testing.T) {
	a := decisionChainDiagram(12)

	unbounded := newWalker(a)
	unbounded.remaining = 1 << 40 // effectively unbudgeted
	var full []activityPath
	for _, entry := range diagramEntries(a) {
		for _, walk := range unbounded.walkFrom(entry.NodeID, map[int]bool{}) {
			full = append(full, activityPath{Entry: entry, Nodes: walk.seq})
		}
	}
	if len(full) != 4096 {
		t.Fatalf("fixture: want 4096 true paths, got %d", len(full))
	}

	got, exhausted := boundedActivityPaths(a)
	if !exhausted {
		t.Fatalf("fixture: a 3.0M-step enumeration must trip the %d-step budget", maxWalkWork)
	}
	if len(got) != maxActivityPaths {
		t.Fatalf("a budget-truncated decision chain must still fill the cap, got %d paths", len(got))
	}
	for i := range got {
		if !equalStrings(got[i].Nodes, full[i].Nodes) {
			t.Fatalf("path %d differs from the unbounded prefix:\n got %v\nwant %v", i, got[i].Nodes, full[i].Nodes)
		}
	}
}

// TestPaths_BudgetDoesNotBindOnOrdinaryDiagram is the other half of the bound: a
// diagram of the shape real authoring produces — a 3-way fork whose branches each
// carry their own 3-way decision, 27 complete combinations — must enumerate FULLY,
// with the budget untouched. The bound exists to stop pathological blowups, not to
// silently truncate ordinary designs.
func TestPaths_BudgetDoesNotBindOnOrdinaryDiagram(t *testing.T) {
	nodes := []ActivityNode{{ID: "s", Kind: nodeStart}, {ID: "f", Kind: nodeFork}}
	edges := []ActivityEdge{{From: "s", To: "f"}}
	for b := 0; b < 3; b++ {
		d := fmt.Sprintf("d%d", b)
		nodes = append(nodes, ActivityNode{ID: d, Kind: nodeDecision, Label: "branch?"})
		edges = append(edges, ActivityEdge{From: "f", To: d})
		for i := 0; i < 3; i++ {
			leaf := fmt.Sprintf("a%d-%d", b, i)
			nodes = append(nodes, ActivityNode{ID: leaf, Kind: nodeAction, Label: leaf})
			edges = append(edges, ActivityEdge{From: d, To: leaf, Kind: edgeGuardedFlow, Guard: "[g]"})
		}
	}

	got, exhausted := boundedActivityPaths(ActivityDiagram{Nodes: nodes, Edges: edges})
	if exhausted {
		t.Fatalf("an ordinary 27-combination diagram must not exhaust the budget")
	}
	if len(got) != 27 {
		t.Fatalf("want all 27 cross-producted combinations, got %d", len(got))
	}
}
