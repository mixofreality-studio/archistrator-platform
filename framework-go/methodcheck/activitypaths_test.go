package methodcheck

import "testing"

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
