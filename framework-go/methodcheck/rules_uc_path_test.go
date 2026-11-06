package methodcheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
)

// rules_uc_path_test.go covers the UC-PATH-OUTCOME / UC-OUTCOME-AT-ASKING-SURFACE /
// UC-START-NOT-ACTION family and the SCN-* mirror, over the RUN-021 reword-a-todo
// design (scenario/testdata/reword_abandon.json) projected onto the methodcheck
// mirror types.

// ucPathFixture loads the reword_abandon scenario input and projects it onto a
// System + CoreUseCases: one component per view endpoint that is not an actor, a
// Client when its contract says so.
func ucPathFixture(t *testing.T) (System, CoreUseCases) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "scenario", "testdata", "reword_abandon.json"))
	if err != nil {
		t.Fatal(err)
	}
	var in scenario.Input
	if err := json.Unmarshal(raw, &in); err != nil {
		t.Fatal(err)
	}
	var c CoreUseCases
	var s System
	for _, d := range in.Diagrams {
		uc := UseCase{ID: d.UseCaseID, Name: d.Title, Classification: classCore, Trigger: triggerClientAction, Activity: &ActivityDiagram{}}
		uc.Actors = append(uc.Actors, Actor{ID: "team-member", Role: "Team Member"})
		for _, n := range d.Nodes {
			uc.Activity.Nodes = append(uc.Activity.Nodes, ActivityNode{ID: n.ID, Kind: n.Kind, Label: n.Label, RoleName: n.RoleName, LinkedActorID: n.LinkedActorID, DecidedBy: n.DecidedBy, InState: n.InState, Anchor: n.Anchor})
		}
		for _, e := range d.Edges {
			uc.Activity.Edges = append(uc.Activity.Edges, ActivityEdge{From: e.From, To: e.To, Guard: e.Guard})
		}
		c.Decisions = append(c.Decisions, UseCaseDecision{UseCase: uc})
	}
	seen := map[string]bool{}
	for _, v := range in.Views {
		dv := DynamicView{UseCaseID: v.UseCaseID, Key: v.UseCaseID}
		for _, st := range v.Steps {
			cs := CallStep{ActivityNodeID: st.NodeID}
			for _, call := range st.Calls {
				cs.Calls = append(cs.Calls, TraceCall{From: call.From, To: call.To, Mode: call.Mode, Label: call.Label, Alt: call.Alt})
				for _, id := range []string{call.From, call.To} {
					if in.Actors[id] || seen[id] {
						continue
					}
					seen[id] = true
					kind := kindManager
					if ct, ok := in.Contracts[camel(id)]; ok && ct.Client {
						kind = kindClient
					}
					s.Components = append(s.Components, Component{ID: id, Name: id, Kind: kind})
				}
			}
			dv.Steps = append(dv.Steps, cs)
		}
		s.DynamicViews = append(s.DynamicViews, dv)
	}
	return s, c
}

// camel turns a kebab id into the camelCase contract key it is filed under.
func camel(id string) string {
	parts := strings.Split(id, "-")
	for i := 1; i < len(parts); i++ {
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, "")
}

func messagesFor(fs []Finding, id RuleID) []string {
	var out []string
	for _, f := range fs {
		if f.RuleID == id {
			out = append(out, f.Message+" @ "+f.Location.Section)
		}
	}
	return out
}

func anyContains(msgs []string, sub string) bool {
	for _, m := range msgs {
		if strings.Contains(m, sub) {
			return true
		}
	}
	return false
}

// endWith inserts nodes, chained in order, between n9 and n10 of the reword fixture.
func endWith(c *CoreUseCases, nodes ...ActivityNode) {
	uc := &c.Decisions[0].UseCase
	prev := "n9"
	for _, node := range nodes {
		uc.Activity.Nodes = append(uc.Activity.Nodes, node)
		if prev == "n9" {
			for i, e := range uc.Activity.Edges {
				if e.From == "n9" && e.To == "n10" {
					uc.Activity.Edges[i] = ActivityEdge{From: "n9", To: node.ID}
				}
			}
		} else {
			uc.Activity.Edges = append(uc.Activity.Edges, ActivityEdge{From: prev, To: node.ID})
		}
		prev = node.ID
	}
	uc.Activity.Edges = append(uc.Activity.Edges, ActivityEdge{From: prev, To: "n10"})
}

func TestUC_PathOutcome_FiresOnNoteOnlyPath(t *testing.T) {
	s, c := ucPathFixture(t)
	fs := ucPathFindings(c, s)
	msgs := messagesFor(fs, ruleUCPathOutcome)
	if !anyContains(msgs, "reword-a-todo-P1") {
		t.Fatalf("the note-only abandon path must be named; got %v", msgs)
	}
	for _, f := range fs {
		if f.RuleID == ruleUCPathOutcome && f.Severity != SeverityError {
			t.Fatalf("UC-PATH-OUTCOME must be an Error: %+v", f)
		}
	}
}

func TestUC_PathOutcome_PassesWhenPathEndsInASignal(t *testing.T) {
	s, c := ucPathFixture(t)
	endWith(&c, ActivityNode{ID: "sig", Kind: kindSendSignal, Label: "title shown", LinkedActorID: "team-member"})
	fs := ucPathFindings(c, s)
	if m := messagesFor(fs, ruleUCPathOutcome); len(m) != 0 {
		t.Fatalf("every path now ends in a sendSignal: %v", m)
	}
	if m := messagesFor(fs, ruleUCOutcomeAtAskingSurface); len(m) != 0 {
		t.Fatalf("the signal goes to the asking actor: %v", m)
	}
}

func TestUC_OutcomeAtAskingSurface_FiresWhenOnlyOtherSurfaces(t *testing.T) {
	s, c := ucPathFixture(t)
	uc := &c.Decisions[0].UseCase
	uc.Actors = append(uc.Actors, Actor{ID: "observer", Role: "Observer"})
	// the signal reaches ANOTHER actor; the asker's Client shows nothing.
	endWith(&c, ActivityNode{ID: "sig", Kind: kindSendSignal, Label: "told someone else", LinkedActorID: "observer"})
	fs := ucPathFindings(c, s)
	if m := messagesFor(fs, ruleUCPathOutcome); len(m) != 0 {
		t.Fatalf("a signal is on every path, so UC-PATH-OUTCOME is satisfied: %v", m)
	}
	msgs := messagesFor(fs, ruleUCOutcomeAtAskingSurface)
	if !anyContains(msgs, "reword-a-todo-P2") || !anyContains(msgs, "todowebapp") {
		t.Fatalf("P2 must be reported against the asking Client todowebapp (normalized id); got %v", msgs)
	}
}

// returnToClient adds, to the step of each named node, a call from the list manager
// back into the todo web app: the system returning state to the asking surface.
func returnToClient(s *System, nodes ...string) {
	for i := range s.DynamicViews {
		for j := range s.DynamicViews[i].Steps {
			st := &s.DynamicViews[i].Steps[j]
			for _, n := range nodes {
				if st.ActivityNodeID == n {
					st.Calls = append(st.Calls, TraceCall{From: "list-manager", To: "todo-web-app", Mode: "sync", Label: "state returned to the asking surface"})
				}
			}
		}
	}
}

func TestUC_OutcomeAtAskingSurface_ObjectNodeAfterClientCall(t *testing.T) {
	s, c := ucPathFixture(t)
	onlyWebApp(&s)
	// the system calls back into the asking Client on both branches before the state.
	returnToClient(&s, "n6", "n7")
	endWith(&c, ActivityNode{ID: "obj", Kind: kindObjectNode, Label: "Todo", InState: "[reworded]"})
	fs := ucPathFindings(c, s)
	if m := messagesFor(fs, ruleUCOutcomeAtAskingSurface); len(m) != 0 {
		t.Fatalf("the state is returned to the asking Client by a call into it: %v", m)
	}
}

func TestUC_OutcomeAtAskingSurface_FiresWhenObjectNodeIsOnlyInResourceLane(t *testing.T) {
	s, c := ucPathFixture(t)
	onlyWebApp(&s)
	// RUN-021: the state exists, but only in a resource lane; nothing returns to the
	// asking surface, and the signal goes to the actor from ANOTHER surface.
	endWith(&c,
		ActivityNode{ID: "obj", Kind: kindObjectNode, Label: "Todo", InState: "[reworded]", RoleName: "List Keeping"},
		ActivityNode{ID: "sig", Kind: kindSendSignal, Label: "shown elsewhere", LinkedActorID: "team-member", RoleName: "todo-mcp-server"})
	msgs := messagesFor(ucPathFindings(c, s), ruleUCOutcomeAtAskingSurface)
	if !anyContains(msgs, "reword-a-todo-P2") || !anyContains(msgs, "todowebapp") {
		t.Fatalf("an objectNode no call returned to the asking Client must not satisfy it; got %v", msgs)
	}
}

func TestUC_OutcomeAtAskingSurface_ObjectNodeInAskingLane(t *testing.T) {
	for _, lane := range []string{"todo-web-app", "Team Member"} {
		s, c := ucPathFixture(t)
		onlyWebApp(&s)
		endWith(&c, ActivityNode{ID: "obj", Kind: kindObjectNode, Label: "Todo", InState: "[reworded]", RoleName: lane})
		if m := messagesFor(ucPathFindings(c, s), ruleUCOutcomeAtAskingSurface); len(m) != 0 {
			t.Fatalf("an objectNode drawn in lane %q is shown on the asking surface: %v", lane, m)
		}
	}
}

func TestUC_PathOutcome_NamesTheActorStimulusNotTheStart(t *testing.T) {
	s, c := ucPathFixture(t)
	msgs := messagesFor(ucPathFindings(c, s), ruleUCPathOutcome)
	if !anyContains(msgs, `input at node "n2"`) || anyContains(msgs, `input at node "n1"`) {
		t.Fatalf("n1 is a structural start; the first actor input is n2: %v", msgs)
	}
}

func TestUC_PathRules_ReportDerivationError(t *testing.T) {
	s, c := ucPathFixture(t)
	uc := &c.Decisions[0].UseCase
	uc.Activity.Nodes = append(uc.Activity.Nodes, uc.Activity.Nodes[0]) // duplicate node id
	msgs := messagesFor(ucPathFindings(c, s), ruleUCPathOutcome)
	if !anyContains(msgs, "derivation failed") || !anyContains(msgs, "duplicate node id") {
		t.Fatalf("a derivation error must be a finding, not silence: %v", msgs)
	}
}

func TestUC_StartNotAction_Fires(t *testing.T) {
	_, c := ucPathFixture(t)
	msgs := messagesFor(startNotActionFindings(c), ruleUCStartNotAction)
	if len(msgs) != 1 || !strings.Contains(msgs[0], `"n1"`) {
		t.Fatalf("n1 is a labelled start in the Team Member lane: %v", msgs)
	}
	// An unlabelled start, or one outside an actor lane, is fine.
	uc := &c.Decisions[0].UseCase
	uc.Activity.Nodes[0].Label = ""
	if m := messagesFor(startNotActionFindings(c), ruleUCStartNotAction); len(m) != 0 {
		t.Fatalf("unlabelled start: %v", m)
	}
	uc.Activity.Nodes[0].Label = "Activate"
	uc.Activity.Nodes[0].LinkedActorID = ""
	uc.Activity.Nodes[0].RoleName = "List Keeping"
	if m := messagesFor(startNotActionFindings(c), ruleUCStartNotAction); len(m) != 0 {
		t.Fatalf("start outside an actor lane: %v", m)
	}
}

func TestUC_StartNotAction_RoleResolvesToActor(t *testing.T) {
	_, c := ucPathFixture(t)
	uc := &c.Decisions[0].UseCase
	uc.Activity.Nodes[0].LinkedActorID = ""
	uc.Activity.Nodes[0].RoleName = "Team Member" // the actor's role
	if m := messagesFor(startNotActionFindings(c), ruleUCStartNotAction); len(m) != 1 {
		t.Fatalf("a lane whose role resolves to an actor is an actor lane: %v", m)
	}
}

func TestScenarioProblems_AreFindings(t *testing.T) {
	r := scenario.Report{Problems: []scenario.Problem{
		{UseCase: "uc", Scenario: "uc-P1", Node: "n2", Code: scenario.ProblemUnaddressed, Message: "m1"},
		{UseCase: "uc", Scenario: "uc-P2", Node: "n1", Code: scenario.ProblemActorPathClient, Message: "m2"},
	}}
	fs := scenarioProblemFindings(r)
	if len(fs) != 2 || fs[0].RuleID != ruleSCNUnaddressed || fs[1].RuleID != ruleSCNActorPathClient {
		t.Fatalf("%s", dumpFindings(fs))
	}
	for _, f := range fs {
		if f.Severity != SeverityError || !strings.Contains(f.Message, "uc-P") {
			t.Fatalf("%+v", f)
		}
	}
	if len(scenarioProblemFindings(scenario.Report{})) != 0 {
		t.Fatal("no problems, no findings")
	}
}

func TestUC_OutcomeAtAskingSurface_FiresWhenSignalIsOnAnotherClient(t *testing.T) {
	s, c := ucPathFixture(t)
	onlyWebApp(&s)
	// the signal reaches the asking actor, but drawn in ANOTHER surface's lane.
	endWith(&c, ActivityNode{ID: "sig", Kind: kindSendSignal, Label: "shown elsewhere", LinkedActorID: "team-member", RoleName: "todo-mcp-server"})
	fs := ucPathFindings(c, s)
	msgs := messagesFor(fs, ruleUCOutcomeAtAskingSurface)
	if !anyContains(msgs, "reword-a-todo-P2") || !anyContains(msgs, "todowebapp") {
		t.Fatalf("a signal in another component's lane must not satisfy the asking Client; got %v", msgs)
	}
	// the same signal in the asking Client's lane, or the actor's own, does.
	for _, lane := range []string{"todo-web-app", "Team Member"} {
		s2, c2 := ucPathFixture(t)
		onlyWebApp(&s2)
		endWith(&c2, ActivityNode{ID: "sig", Kind: kindSendSignal, Label: "shown here", LinkedActorID: "team-member", RoleName: lane})
		if m := messagesFor(ucPathFindings(c2, s2), ruleUCOutcomeAtAskingSurface); len(m) != 0 {
			t.Fatalf("lane %q is the asking surface: %v", lane, m)
		}
	}
}

// onlyWebApp restricts the actor's n4 input to the web app, so it is the one asking Client.
func onlyWebApp(s *System) {
	for i := range s.DynamicViews {
		for j := range s.DynamicViews[i].Steps {
			st := &s.DynamicViews[i].Steps[j]
			if st.ActivityNodeID != "n4" {
				continue
			}
			kept := st.Calls[:0:0]
			for _, call := range st.Calls {
				if call.To != "todo-mcp-server" {
					kept = append(kept, call)
				}
			}
			st.Calls = kept
		}
	}
}

func TestScenarioProblems_UnknownCodeIsNotMislabelled(t *testing.T) {
	r := scenario.Report{Problems: []scenario.Problem{
		{UseCase: "uc", Scenario: "uc-P1", Node: "n2", Code: "SCN-FUTURE", Message: "m"},
	}}
	fs := scenarioProblemFindings(r)
	if len(fs) != 1 || fs[0].RuleID == ruleSCNUnaddressed || fs[0].RuleID == ruleSCNActorPathClient {
		t.Fatalf("an unknown code must not be reported under a known rule: %s", dumpFindings(fs))
	}
	if !strings.Contains(fs[0].Message, "SCN-FUTURE") || fs[0].Severity != SeverityError {
		t.Fatalf("the unknown code must be named, as an Error: %+v", fs[0])
	}
}
