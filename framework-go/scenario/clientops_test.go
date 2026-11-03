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
	if got := unresolvedAlts(in, "web", "AddTodo | Nothing — x"); !slices.Equal(got, []string{"Nothing"}) {
		t.Errorf("unresolved: %v", got)
	}
	if got := unresolvedAlts(in, "web", "overrideActivity — retry | skip"); got != nil {
		t.Errorf("after the dash is prose: %v", got)
	}
	if got := unresolvedAlts(in, "nothing", "A | B"); got != nil {
		t.Errorf("a callee with no contract is not judged: %v", got)
	}
}

func unresolvedAlts(in Input, component, label string) []string {
	var out []string
	for _, u := range UnresolvedOps(in, component, label) {
		out = append(out, u.Alt)
	}
	return out
}

// RUN-010's own labels: authored in phase 1 in the Manager's vocabulary, into two machine clients
// whose derived operations carry the Manager's name.
const (
	run010Change = "AddTodo | RewordTodo | DiscardTodo | SetTodoDone | ToggleAll | ClearCompleted — performs one list change"
	run010Call   = "addTodo | rewordTodo | discardTodo | setTodoDone | toggleAll | clearCompleted"
)

var run010ManagerOps = []string{"AddTodo", "RewordTodo", "DiscardTodo", "SetTodoDone", "ToggleAll", "ClearCompleted", "ReviewList"}

func run010Input() Input {
	mgr := Contract{Component: "listManager", Interface: "ListManager"}
	api := Contract{Component: "listApi", Client: true, Interface: "ListApiList"}
	mcp := Contract{Component: "listMcpServer", Client: true, Interface: "ListMcpServerList"}
	for _, op := range run010ManagerOps {
		mgr.Ops = append(mgr.Ops, Op{Name: op})
		api.Ops = append(api.Ops, Op{Name: ClientOpName("ListManager", op)})
		mcp.Ops = append(mcp.Ops, Op{Name: ClientOpName("ListManager", op)})
	}
	return Input{
		Actors:    map[string]bool{"agent-tooling": true},
		Contracts: map[string]Contract{"listManager": mgr, "listApi": api, "listMcpServer": mcp},
		Views: []View{
			{UseCaseID: "work-a-list", Steps: []Step{{NodeID: "a", Calls: []Call{
				{From: "agent-tooling", To: "list-api", Label: run010Change},
				{From: "agent-tooling", To: "list-mcp-server", Label: run010Change},
				{From: "list-api", To: "list-manager", Label: run010Call},
				{From: "list-mcp-server", To: "list-manager", Label: run010Call},
			}}}},
			{UseCaseID: "review-a-list", Steps: []Step{{NodeID: "b", Calls: []Call{
				{From: "agent-tooling", To: "list-api", Label: "ReviewList — opens the list in a chosen view"},
				{From: "list-api", To: "list-manager", Label: "reviewList"},
			}}}},
		},
	}
}

// A label into a machine client names the Manager's operation, and resolves through the Manager the
// client fronts onto the operation the client derives for it: once, and aimed at the derived name.
func TestLabelResolvesThroughTheFrontedManager(t *testing.T) {
	in := run010Input()
	for _, client := range []string{"list-api", "list-mcp-server"} {
		if got := UnresolvedOps(in, client, run010Change); got != nil {
			t.Errorf("%s: %+v", client, got)
		}
		var got []string
		for _, ot := range resolveOps(in, client, run010Change) {
			got = append(got, ot.to+"."+ot.op)
		}
		want := []string{}
		for _, op := range run010ManagerOps[:6] {
			want = append(want, Normalize(client)+".List"+op)
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s resolves %v, want %v", client, got, want)
		}
	}
	review := in.Views[1].Steps[0].Calls
	if got := UnresolvedOps(in, "list-api", review[0].Label); got != nil {
		t.Errorf("a single-op label: %+v", got)
	}
	got := groupOpsOrThrough(in, review[:1], "list-api", review)
	if len(got) != 1 || got[0].op != "ListReviewList" {
		t.Errorf("one stimulus, at the derived op: %+v", got)
	}
	if got := unresolvedAlts(in, "list-api", "ListAddTodo | ListReviewList — the prefixed form"); got != nil {
		t.Errorf("the client's own spelling resolves too: %v", got)
	}
}

// A name that resolves nowhere is reported, whether it is one of several alternatives or a label's
// only operation; prose is not.
func TestUnresolvedSingleOpLabel(t *testing.T) {
	in := run010Input()
	if got := unresolvedAlts(in, "list-api", "ArchiveList — puts it away"); !slices.Equal(got, []string{"ArchiveList"}) {
		t.Errorf("a single op naming nothing: %v", got)
	}
	if got := unresolvedAlts(in, "list-manager", "Archive(proof) → done | refused"); !slices.Equal(got, []string{"Archive(proof)"}) {
		t.Errorf("into a Manager too: %v", got)
	}
	for _, prose := range []string{"performs one list change", "reviewList", "Opens the list"} {
		if got := UnresolvedOps(in, "list-api", prose); got != nil {
			t.Errorf("%q is prose, or an unexported call: %+v", prose, got)
		}
	}
	in.Contracts["listApi"] = Contract{Component: "listApi", Client: true, Interface: "ListApiList", Ops: []Op{{Name: "ListAddTodo"}}}
	if got := unresolvedAlts(in, "list-api", "AddTodo | ReviewList — x"); !slices.Equal(got, []string{"ReviewList"}) {
		t.Errorf("a Manager op the client does not expose resolves nowhere: %v", got)
	}
}

// A bare name that is an operation of two Managers one client fronts names neither; the prefixed
// form names one.
func TestAmbiguousBareOpAcrossManagers(t *testing.T) {
	in := run010Input()
	in.Contracts["tagManager"] = Contract{Component: "tagManager", Interface: "TagManager", Ops: []Op{{Name: "AddTodo"}, {Name: "Tag"}}}
	api := in.Contracts["listApi"]
	api.Ops = append(api.Ops, Op{Name: "TagAddTodo"}, Op{Name: "TagTag"})
	in.Contracts["listApi"] = api
	in.Views = append(in.Views, View{UseCaseID: "tag", Steps: []Step{{NodeID: "c", Calls: []Call{
		{From: "agent-tooling", To: "list-api", Label: "Tag — tags one"},
		{From: "list-api", To: "tag-manager", Label: "tag | addTodo"},
	}}}})
	got := UnresolvedOps(in, "list-api", "AddTodo | Tag | TagAddTodo — x")
	if len(got) != 1 || got[0].Alt != "AddTodo" || !slices.Equal(got[0].Candidates, []string{"ListAddTodo", "TagAddTodo"}) {
		t.Errorf("ambiguity: %+v", got)
	}
	if ops := resolveOps(in, "list-api", "AddTodo — x"); ops != nil {
		t.Errorf("an ambiguous name derives no stimulus: %+v", ops)
	}
	if got := UnresolvedOps(in, "list-mcp-server", "AddTodo — x"); got != nil {
		t.Errorf("a client fronting one of them is not ambiguous: %+v", got)
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
