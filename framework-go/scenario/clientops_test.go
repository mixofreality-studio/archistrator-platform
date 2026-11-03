package scenario

import (
	"slices"
	"strings"
	"testing"
)

func TestLeadingOpNames(t *testing.T) {
	for _, tc := range []struct {
		label   string
		ops     []string
		invalid int
	}{
		{"AddTodo | RewordTodo — performs one list action", []string{"AddTodo", "RewordTodo"}, 0},
		{"OpenSdpReview(action) — opens the review", []string{"OpenSdpReview"}, 0},
		{"AddTodo", []string{"AddTodo"}, 0},
		{"Add(a | b) | Drop → x | y", []string{"Add", "Drop"}, 0},
		{"requests one list action", nil, 1},
		{"addTodo", nil, 1},
		{"AddTodo now — prose first", nil, 1},
		{"AddTodo | not an op", []string{"AddTodo"}, 1},
	} {
		ops, invalid := LeadingOpNames(tc.label)
		if !slices.Equal(ops, tc.ops) || len(invalid) != tc.invalid {
			t.Errorf("%q: got %v, %v", tc.label, ops, invalid)
		}
	}
}

func TestClientOps(t *testing.T) {
	got, err := ClientOps([]string{"AddTodo | RewordTodo — one", "AddTodo(x) — again", "Discard → done"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"AddTodo", "RewordTodo", "Discard"}) {
		t.Errorf("got %v", got)
	}
	if _, err := ClientOps([]string{"AddTodo", "Addtodo"}); err == nil || !strings.Contains(err.Error(), "two ways") {
		t.Errorf("two spellings: %v", err)
	}
	if _, err := ClientOps([]string{"performs one list action"}); err == nil {
		t.Error("prose names no op")
	}
}

// "|" separates only in the op clause: RUN-009's own labels list outcomes after the arrow.
func TestPipeSplitsOnlyTheOpClause(t *testing.T) {
	in := Input{Contracts: map[string]Contract{"web": {Ops: []Op{{Name: "AddTodo"}, {Name: "Refused"}, {Name: "Drop"}}}}}
	names := func(label string) []string {
		var out []string
		for _, ot := range resolveOps(in, "web", label) {
			out = append(out, ot.op)
		}
		return out
	}
	if got := names("AddTodo | Drop(x) — one"); !slices.Equal(got, []string{"AddTodo", "Drop"}) {
		t.Errorf("alternatives: %v", got)
	}
	if got := names("AddTodo(proof) → member | Refused"); !slices.Equal(got, []string{"AddTodo"}) {
		t.Errorf("an outcome after the arrow is not an op: %v", got)
	}
	if got := UnresolvedAlternatives(in, "web", "AddTodo | Nothing — x"); !slices.Equal(got, []string{"Nothing"}) {
		t.Errorf("unresolved: %v", got)
	}
	if got := UnresolvedAlternatives(in, "web", "overrideActivity — retry | skip"); got != nil {
		t.Errorf("after the dash is prose: %v", got)
	}
	if got := UnresolvedAlternatives(in, "nothing", "A | B"); got != nil {
		t.Errorf("a callee with no contract is not judged: %v", got)
	}
}

// A machine client's prose label resolves through its own call into the Manager in that step.
func TestProseCallResolvesThroughTheManager(t *testing.T) {
	in := Input{
		Actors: map[string]bool{"agent": true},
		Contracts: map[string]Contract{
			"listManager": {Interface: "ListManager", Ops: []Op{{Name: "AddTodo"}, {Name: "Drop"}}},
			"mcpServer":   {Client: true, Interface: "McpServer", Ops: []Op{{Name: "ListAddTodo"}, {Name: "ListDrop"}}},
			"store":       {Interface: "Store", Ops: []Op{{Name: "Put"}}},
		},
	}
	step := []Call{
		{From: "agent", To: "mcp-server", Label: "requests one action"},
		{From: "mcp-server", To: "list-manager", Label: "addTodo | drop(x)"},
	}
	var got []string
	for _, ot := range throughManagerOps(in, "mcp-server", step) {
		got = append(got, ot.op)
	}
	if !slices.Equal(got, []string{"ListAddTodo", "ListDrop"}) {
		t.Errorf("got %v", got)
	}
	if ops := throughManagerOps(in, "store", step); ops != nil {
		t.Errorf("only a Client resolves through its callee: %v", ops)
	}
	if ops := throughManagerOps(in, "absent", step); ops != nil {
		t.Errorf("no contract, no op: %v", ops)
	}
}

// A prose request into a machine client resolves through what the client does in the same step, and
// only there; a call of another client in that step is not its operation.
func TestProseRequestResolvesThroughTheManagerCall(t *testing.T) {
	in := Input{Contracts: map[string]Contract{
		"webApi":      {Component: "webApi", Client: true, Interface: "WebApiList", Ops: []Op{{Name: "ListAddTodo"}, {Name: "ListDrop"}}},
		"mcpServer":   {Component: "mcpServer", Client: true, Interface: "McpServerList", Ops: []Op{{Name: "ListDrop"}}},
		"listManager": {Component: "listManager", Interface: "ListManager", Ops: []Op{{Name: "AddTodo"}, {Name: "Drop"}}},
	}}
	step := []Call{
		{From: "agent", To: "web-api", Label: "submits a title"},
		{From: "agent", To: "mcp-server", Label: "drops one"},
		{From: "web-api", To: "list-manager", Label: "addTodo(proof) → todos"},
		{From: "mcp-server", To: "list-manager", Label: "drop(proof)"},
	}
	got := groupOpsOrThrough(in, step[:1], "web-api", step)
	if len(got) != 1 || got[0].op != "ListAddTodo" || got[0].to != "webapi" {
		t.Errorf("through the Manager: %+v", got)
	}
	if got := groupOpsOrThrough(in, step[:1], "web-api", step[:1]); len(got) != 0 {
		t.Errorf("no call beside it derives nothing: %+v", got)
	}
	if got := groupOpsOrThrough(in, []Call{{From: "agent", To: "list-manager", Label: "prose"}}, "list-manager", step); len(got) != 0 {
		t.Errorf("only a Client resolves through its own calls: %+v", got)
	}
}
