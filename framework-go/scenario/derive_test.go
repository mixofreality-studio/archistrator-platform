package scenario

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

func load(t *testing.T, name string) Input {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var in Input
	if err := json.Unmarshal(raw, &in); err != nil {
		t.Fatal(err)
	}
	return in
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	p := filepath.Join("testdata", name+".golden.json")
	if *update {
		if err := os.WriteFile(p, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != string(got) {
		t.Fatalf("golden mismatch for %s\n--- want\n%s\n--- got\n%s", name, want, got)
	}
}

func TestDerive_OrderingGolden(t *testing.T) {
	s, err := Derive(load(t, "ordering"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := CanonicalJSON(s)
	golden(t, "ordering", b)
}

// Amendment A3: the start family (start -> a -> end) carries no stimulus while
// the diagram also has an event entry (tick), so it is dropped and reported; the
// tick family (elementary + loop unrolled once) keeps ids P1, P2.
func TestDerive_TwoEntries(t *testing.T) {
	r, err := DeriveReport(load(t, "two_entries"))
	if err != nil {
		t.Fatal(err)
	}
	s := r.Scenarios
	if len(s) != 2 {
		t.Fatalf("want 2 scenarios (tick family, tick loop; start family dropped), got %d", len(s))
	}
	for _, sc := range s {
		if sc.Path[0] != "tick" {
			t.Fatalf("start-family path survived: %+v", sc)
		}
	}
	if len(r.StartRedundant) != 1 || r.StartRedundant[0].UseCase != "x" || r.StartRedundant[0].Dropped != 1 {
		t.Fatalf("want one start redundancy on x dropping 1 path, got %+v", r.StartRedundant)
	}
	b, _ := CanonicalJSON(s)
	golden(t, "two_entries", b)
}

// A3 applies only when a diagram has BOTH a start node and an event entry: a
// start-only diagram keeps its zero-stimulus paths (UC-NO-IO reports those).
func TestDerive_StartOnlyKeepsZeroStimulusPaths(t *testing.T) {
	in := load(t, "two_entries")
	d := &in.Diagrams[0]
	var nodes []Node
	for _, n := range d.Nodes {
		if n.ID != "tick" {
			nodes = append(nodes, n)
		}
	}
	d.Nodes = nodes
	var edges []Edge
	for _, e := range d.Edges {
		if e.From != "tick" {
			edges = append(edges, e)
		}
	}
	d.Edges = edges
	r, err := DeriveReport(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Scenarios) != 1 || r.Scenarios[0].Path[0] != "start" || len(r.StartRedundant) != 0 {
		t.Fatalf("start-only diagram must keep its path and report nothing: %+v", r)
	}
}

// A3: a start-family path that reaches a region no event entry reaches carries
// stimuli of its own and is kept.
func TestDerive_StartFamilyWithOwnStimuliIsKept(t *testing.T) {
	in := load(t, "two_entries")
	in.Diagrams[0].Nodes[1].RoleName = "scheduler-manager" // a: a component-lane action...
	in.Diagrams[0].Nodes[1].Kind = "acceptEvent"           // ...that accepts an event: an input
	r, err := DeriveReport(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Scenarios) != 3 || r.Scenarios[0].Path[0] != "start" || len(r.StartRedundant) != 0 {
		t.Fatalf("start path with its own stimulus must be kept: %+v", r)
	}
}

// Out-edges sort ascending by (To, Guard) (spec §2.3), so "reject" precedes
// "send-invoice" and the reject branch is order-P1 = s[0].
func TestDerive_UnguardedBranch(t *testing.T) {
	in := load(t, "ordering")
	in.Diagrams[0].Edges[3].Guard = "" // accepted -> reject loses its guard
	s, err := Derive(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 2 || s[0].ID != "order-P1" || len(s[0].Guards) != 1 || s[0].Guards[0].Guard != "" {
		t.Fatalf("unguarded branch must still enumerate with empty guard: %+v", s)
	}
}

func TestDerive_UnresolvedOpIsEmptyNotError(t *testing.T) {
	in := load(t, "ordering")
	in.Views[0].Steps[0].Calls[0].Label = "Place the order please"
	s, err := Derive(in)
	if err != nil {
		t.Fatal(err)
	}
	if s[0].Stimuli[0].Input.Op != "" {
		t.Fatalf("want unresolved op to be empty, got %q", s[0].Stimuli[0].Input.Op)
	}
}

func TestDerive_IsDeterministic(t *testing.T) {
	in := load(t, "ordering")
	f1, _ := Fingerprint(must(Derive(in)))
	// shuffle node order; ids must not change
	in.Diagrams[0].Nodes[0], in.Diagrams[0].Nodes[5] = in.Diagrams[0].Nodes[5], in.Diagrams[0].Nodes[0]
	f2, _ := Fingerprint(must(Derive(in)))
	if f1 != f2 {
		t.Fatal("node declaration order changed the fingerprint")
	}
}

// The accepted branch (where "pay" lives) is order-P2 under the (To, Guard) ordering.
func TestForComponent(t *testing.T) {
	s := must(Derive(load(t, "ordering")))
	b := ForComponent(s, "billing-manager")
	if len(b) != 1 || b[0].ID != "order-P2" || len(b[0].Stimuli) != 1 || b[0].Stimuli[0].Seq != 1 || b[0].Stimuli[0].Node != "pay" {
		t.Fatalf("projection wrong: %+v", b)
	}
}

func must(s []Scenario, err error) []Scenario {
	if err != nil {
		panic(err)
	}
	return s
}

// resolveOps (A2) over the label shapes the execute-a-project-activity view uses.
func TestResolveOps_LabelShapes(t *testing.T) {
	in := load(t, "execute_activity")
	for _, tc := range []struct {
		component, label string
		want             []string
	}{
		// op(args) — prose
		{"delivery-manager", "executeNextActivity(projectId, tickId) — the pump starts a child for every eligible activity", []string{"ExecuteNextActivity"}},
		// op → result prose
		{"project-state-access", "readProject → the next ready tasks of the activity's lifecycle DAG", []string{"ReadProject"}},
		// op(args) → Result; op(args) → Result
		{"source-control-access", "getInstallationToken(repo) → RepoCredential; openBranch(repo, activityBranch, cred) → BranchRef", []string{"GetInstallationToken", "OpenBranch"}},
		// op / op — prose
		{"artifact-access", "storeConstructionOutput / retrieveConstructionOutput — the produced artifact, staged against the task", []string{"StoreConstructionOutput", "RetrieveConstructionOutput"}},
		// op(args) · op(args) — prose
		{"source-control-access", "postReview(pr, verdict, cred) · mergePullRequest(pr, cred) — the approval is relayed and the branch merges", []string{"PostReview", "MergePullRequest"}},
		// op(args) prose; op → prose
		{"agentic-job-access", "submitAgenticJob(review command, reviewer role) per agent seat; observeAgenticJob → each seat's verdict", []string{"SubmitAgenticJob", "ObserveAgenticJob"}},
		// pure prose: unresolved
		{"review-engine", "the ReviewSet's own seats decide it: a non-overridable floor row always holds for a human", nil},
		// ops that live on another contract do not resolve here
		{"project-state-access", "commitArtifactWithProvenance / commitActivityArtifacts — the staged artifact becomes committed state", nil},
		// a separator inside an argument list does not split the label
		{"delivery-manager", "overrideActivity(projectId, activityId / taskId, override) — retry | skip | takeover", []string{"OverrideActivity"}},
	} {
		got := resolveOps(in, tc.component, tc.label)
		if !equalStrings(got, tc.want) {
			t.Errorf("resolveOps(%s, %q) = %v, want %v", tc.component, tc.label, got, tc.want)
		}
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

// stimuliAt returns the stimuli opened at node on the first scenario whose path visits it.
func stimuliAt(t *testing.T, all []Scenario, node string) []Stimulus {
	t.Helper()
	for _, s := range all {
		var out []Stimulus
		for _, st := range s.Stimuli {
			if st.Node == node {
				out = append(out, st)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	t.Fatalf("no scenario has stimuli at %s", node)
	return nil
}

func target(st Stimulus) string { return st.Input.To + "." + st.Input.Op }

// The real execute-a-project-activity diagram + dynamic view + all 28 contracts'
// op names, copied from the deterministic-component-testing branch's project.json
// (Plan 4 Task 1): Amendment A1–A4 measured end to end.
func TestDerive_ExecuteActivityGolden(t *testing.T) {
	r, err := DeriveReport(load(t, "execute_activity"))
	if err != nil {
		t.Fatal(err)
	}
	all := r.Scenarios
	b, _ := CanonicalJSON(all)
	golden(t, "execute_activity", b)

	// A1: Engines and RA receive stimuli directly.
	for _, comp := range []string{"reviewEngine", "interventionEngine", "sourceControlAccess", "agenticJobAccess"} {
		if len(ForComponent(all, comp)) < 1 {
			t.Errorf("%s receives no scenario", comp)
		}
	}
	for _, s := range all {
		if len(s.Stimuli) == 0 {
			t.Errorf("%s has zero stimuli", s.ID)
		}
	}
	readProject := false
	for _, s := range ForComponent(all, "projectStateAccess") {
		for _, st := range s.Stimuli {
			if st.Input.Op == "ReadProject" {
				readProject = true
			}
		}
	}
	if !readProject {
		t.Error("projectStateAccess.ReadProject is not resolved")
	}

	// A1 + A2: three calls (manager→RA, manager→RA, RA→resource) yield the two
	// component calls' stimuli in call order; the first call's label names two ops.
	got := stimuliAt(t, all, "dispatch-task")
	want := []string{"sourcecontrolaccess.GetInstallationToken", "sourcecontrolaccess.OpenBranch", "agenticjobaccess.SubmitAgenticJob"}
	if len(got) != len(want) {
		t.Fatalf("dispatch-task stimuli: %+v", got)
	}
	for i := range want {
		if target(got[i]) != want[i] || got[i].Input.Kind != "call" || got[i].Input.From != "delivery-manager" {
			t.Errorf("dispatch-task stimulus %d = %s %+v, want %s from delivery-manager kind call", i, target(got[i]), got[i].Input, want[i])
		}
	}
	if got[0].Input.Call == got[2].Input.Call || got[0].Input.Call == got[1].Input.Call {
		t.Errorf("provenance must distinguish the two ops of one call and the second call: %q %q %q", got[0].Input.Call, got[1].Input.Call, got[2].Input.Call)
	}

	// A2: "storeConstructionOutput / retrieveConstructionOutput — …" → two stimuli.
	var art []string
	for _, st := range stimuliAt(t, all, "observe-validate") {
		if st.Input.To == "artifactaccess" {
			art = append(art, st.Input.Op)
		}
	}
	if !equalStrings(art, []string{"StoreConstructionOutput", "RetrieveConstructionOutput"}) {
		t.Errorf("observe-validate artifactAccess ops = %v", art)
	}

	// A4: web vs MCP alternatives collapse onto deliveryManager with via, while
	// each client keeps its own stimulus.
	hd := stimuliAt(t, all, "human-decision")
	var dm []Stimulus
	clients := map[string]int{}
	for _, st := range hd {
		switch st.Input.To {
		case "deliverymanager":
			dm = append(dm, st)
		case "webclient", "mcpclient":
			clients[st.Input.To]++
			if st.Input.From != "architect-user" || st.Input.Kind != "action" {
				t.Errorf("client stimulus must come from the actor with the node kind: %+v", st.Input)
			}
		}
	}
	if len(dm) != 1 || dm[0].Input.Op != "SubmitReviewDecision" || !equalStrings(dm[0].Input.Via, []string{"web-client", "mcp-client"}) || dm[0].Input.Kind != "call" {
		t.Errorf("human-decision deliveryManager stimuli: %+v", dm)
	}
	if clients["webclient"] != 1 || clients["mcpclient"] != 1 {
		t.Errorf("each client keeps one stimulus: %v", clients)
	}

	// A1: a decision node's guard rides on its stimuli.
	for _, s := range ForComponent(all, "interventionEngine") {
		for _, st := range s.Stimuli {
			if st.Node == "retry-remain" && st.Input.Guard == "" {
				t.Errorf("%s: retry-remain stimulus lost its guard: %+v", s.ID, st.Input)
			}
		}
	}

	// A3: the start family is redundant with the pump-fires family: dropped once.
	for _, s := range all {
		if s.Path[0] == "start" {
			t.Errorf("start-family scenario survived: %s", s.ID)
		}
	}
	if len(r.StartRedundant) != 1 || r.StartRedundant[0].UseCase != "execute-a-project-activity" {
		t.Errorf("want one start redundancy on execute-a-project-activity, got %+v", r.StartRedundant)
	}
	if want := "execute-a-project-activity-P1"; all[0].ID != want {
		t.Errorf("ids renumber over kept paths: first is %s, want %s", all[0].ID, want)
	}
}

// Derive is DeriveReport's scenarios.
func TestDerive_IsDeriveReportScenarios(t *testing.T) {
	in := load(t, "execute_activity")
	r := must2(DeriveReport(in))
	f1, _ := Fingerprint(r.Scenarios)
	f2, _ := Fingerprint(must(Derive(in)))
	if f1 != f2 {
		t.Fatal("Derive and DeriveReport disagree")
	}
}

func must2(r Report, err error) Report {
	if err != nil {
		panic(err)
	}
	return r
}
