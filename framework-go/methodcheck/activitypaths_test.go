package methodcheck

import (
	"fmt"
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
