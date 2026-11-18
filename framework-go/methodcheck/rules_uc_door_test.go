package methodcheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rules_uc_door_test.go covers UC-ACTOR-DOOR and UC-LANE-RESOLVES, and that the
// actor-classified path rules run when the coreUseCases slot is validated. The
// violating fixture is an excerpt of the core use cases RUN-024 committed
// (testdata/run024_core_use_cases.json): every node's linkedActorId is null, the
// lanes are "List owner" / "Todo list" / "Other entry points" while the declared
// actors are team-member and team-agent, so every actor-classified rule judged
// nothing and the diagram passed.

func run024CoreUseCases(t *testing.T) CoreUseCases {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "run024_core_use_cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c CoreUseCases
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func useCaseByID(t *testing.T, c *CoreUseCases, id string) *UseCase {
	t.Helper()
	for i := range c.Decisions {
		if c.Decisions[i].UseCase.ID == id {
			return &c.Decisions[i].UseCase
		}
	}
	t.Fatalf("no use case %q", id)
	return nil
}

// linkActorNodes links the named nodes to actor and puts them in the actor's lane.
func linkActorNodes(uc *UseCase, actor, lane string, ids ...string) {
	for i := range uc.Activity.Nodes {
		for _, id := range ids {
			if uc.Activity.Nodes[i].ID == id {
				uc.Activity.Nodes[i].LinkedActorID = actor
				uc.Activity.Nodes[i].RoleName = lane
			}
		}
	}
}

func TestRun024_ValidateCoreUseCases_FiresActorDoorForEveryUseCase(t *testing.T) {
	c := run024CoreUseCases(t)
	res, err := validateCoreUseCases(c)
	if err != nil {
		t.Fatal(err)
	}
	msgs := messagesFor(res.Findings, ruleUCActorDoor)
	for _, uc := range []string{"remove-a-todo", "reword-a-todo"} {
		if !anyContains(msgs, uc) {
			t.Fatalf("UC-ACTOR-DOOR must name %s at the coreUseCases slot; got %v", uc, msgs)
		}
	}
	// the fix is stated in schema vocabulary, with the declared actor ids.
	if !anyContains(msgs, "set linkedActorId to one of: team-member, team-agent") {
		t.Fatalf("the message must name the fix in schema vocabulary: %v", msgs)
	}
	for _, f := range res.Findings {
		if f.RuleID == ruleUCActorDoor && f.Severity != SeverityError {
			t.Fatalf("UC-ACTOR-DOOR must be an Error: %+v", f)
		}
	}
}

func TestUCActorDoor_PassesWhenEveryPathHasAnActorNode(t *testing.T) {
	c := run024CoreUseCases(t)
	uc := useCaseByID(t, &c, "remove-a-todo")
	linkActorNodes(uc, "team-member", "Team member", "a1")
	if m := messagesFor(ucSlotPathFindings(c), ruleUCActorDoor); anyContains(m, "remove-a-todo") {
		t.Fatalf("a1 is linked to a declared actor: %v", m)
	}
	// reword still has none.
	if m := messagesFor(ucSlotPathFindings(c), ruleUCActorDoor); !anyContains(m, "reword-a-todo") {
		t.Fatalf("reword-a-todo is still unlinked: %v", m)
	}
}

func TestUCActorDoor_LinkToUndeclaredActorDoesNotCount(t *testing.T) {
	c := run024CoreUseCases(t)
	uc := useCaseByID(t, &c, "remove-a-todo")
	linkActorNodes(uc, "someone-else", "Team member", "a1")
	if m := messagesFor(ucSlotPathFindings(c), ruleUCActorDoor); !anyContains(m, "remove-a-todo") {
		t.Fatalf("an id that is no declared actor is not a door: %v", m)
	}
}

func TestUCActorDoor_DecidedByActorCounts(t *testing.T) {
	c := run024CoreUseCases(t)
	uc := useCaseByID(t, &c, "reword-a-todo")
	for i := range uc.Activity.Nodes {
		if uc.Activity.Nodes[i].ID == "d" {
			uc.Activity.Nodes[i].DecidedBy = "team-member"
		}
	}
	// the decision is on every path (the branches rejoin) so every path has an actor node.
	if m := messagesFor(ucSlotPathFindings(c), ruleUCActorDoor); anyContains(m, "reword-a-todo") {
		t.Fatalf("a decision an actor decides is an actor node: %v", m)
	}
}

func TestUCActorDoor_FiresOnAnActorlessPathOfALinkedDiagram(t *testing.T) {
	c := run024CoreUseCases(t)
	uc := useCaseByID(t, &c, "reword-a-todo")
	// only the acceptable branch carries an actor act; the [else] path has none.
	linkActorNodes(uc, "team-member", "Team member", "k")
	m := messagesFor(ucSlotPathFindings(c), ruleUCActorDoor)
	if !anyContains(m, "reword-a-todo-P") {
		t.Fatalf("the path through b carries no actor node and must be named by its scenario id; got %v", m)
	}
}

func TestUCActorDoor_OutcomeAndStructuralKindsAreNotAnActorsAct(t *testing.T) {
	c := run024CoreUseCases(t)
	uc := useCaseByID(t, &c, "remove-a-todo")
	// a sendSignal TO the actor is what the actor sees, not what the actor does.
	uc.Activity.Nodes = append(uc.Activity.Nodes, ActivityNode{ID: "sig", Kind: kindSendSignal, Label: "told", LinkedActorID: "team-member"})
	uc.Activity.Edges = append(uc.Activity.Edges, ActivityEdge{From: "a4", To: "sig"})
	for i, e := range uc.Activity.Edges {
		if e.From == "a4" && e.To == "e" {
			uc.Activity.Edges[i] = ActivityEdge{From: "sig", To: "e"}
		}
	}
	if m := messagesFor(ucSlotPathFindings(c), ruleUCActorDoor); !anyContains(m, "remove-a-todo") {
		t.Fatalf("a signal to the actor is not an actor act: %v", m)
	}
}

func TestUCActorDoor_SkipsUseCasesNobodyInitiates(t *testing.T) {
	c := run024CoreUseCases(t)
	uc := useCaseByID(t, &c, "remove-a-todo")
	uc.Trigger = triggerTimer
	uc.Actors = nil
	if m := messagesFor(ucSlotPathFindings(c), ruleUCActorDoor); anyContains(m, "remove-a-todo") {
		t.Fatalf("a timer-triggered use case is started by the clock and legitimately has no actor act: %v", m)
	}
}

func TestUCSlot_PathOutcomeRunsAtTheSlotOnceActorsAreLinked(t *testing.T) {
	c := run024CoreUseCases(t)
	uc := useCaseByID(t, &c, "remove-a-todo")
	linkActorNodes(uc, "team-member", "Team member", "a1")
	res, err := validateCoreUseCases(c)
	if err != nil {
		t.Fatal(err)
	}
	if m := messagesFor(res.Findings, ruleUCPathOutcome); !anyContains(m, "remove-a-todo-P1") {
		t.Fatalf("the path ends in an action, not an outcome; UC-PATH-OUTCOME must fire at the slot: %v", m)
	}
}

func TestUCSlot_StartNotActionRunsAtTheSlot(t *testing.T) {
	c := run024CoreUseCases(t)
	uc := useCaseByID(t, &c, "remove-a-todo")
	uc.Activity.Nodes[0].Label = "Activate"
	uc.Activity.Nodes[0].RoleName = "Team member"
	res, err := validateCoreUseCases(c)
	if err != nil {
		t.Fatal(err)
	}
	if m := messagesFor(res.Findings, ruleUCStartNotAction); len(m) != 1 {
		t.Fatalf("UC-START-NOT-ACTION must run when core use cases are recorded: %v", m)
	}
}

func TestUCSlot_AskingSurfaceStaysAtArchitecture(t *testing.T) {
	c := run024CoreUseCases(t)
	uc := useCaseByID(t, &c, "remove-a-todo")
	linkActorNodes(uc, "team-member", "Team member", "a1")
	res, err := validateCoreUseCases(c)
	if err != nil {
		t.Fatal(err)
	}
	if m := messagesFor(res.Findings, ruleUCOutcomeAtAskingSurface); len(m) != 0 {
		t.Fatalf("UC-OUTCOME-AT-ASKING-SURFACE needs the System's dynamic views: %v", m)
	}
}

func TestUCSlot_ReportsDerivationError(t *testing.T) {
	c := run024CoreUseCases(t)
	uc := useCaseByID(t, &c, "remove-a-todo")
	uc.Activity.Nodes = append(uc.Activity.Nodes, uc.Activity.Nodes[0])
	m := messagesFor(ucSlotPathFindings(c), ruleUCPathOutcome)
	if !anyContains(m, "derivation failed") {
		t.Fatalf("a derivation error must be a finding at the slot too: %v", m)
	}
}

// run024System is a System whose components are the lanes the RUN-024 diagrams may
// legitimately name: a manager called "Todo list".
func run024System() System {
	return System{Components: []Component{
		{ID: "todo-list", Name: "Todo list", Kind: kindManager},
		{ID: "todo-web-app", Name: "Todo web app", Kind: kindClient},
	}}
}

func TestUCLaneResolves_FiresOnLanesNamingNeitherActorNorComponent(t *testing.T) {
	c := run024CoreUseCases(t)
	fs := ucLaneFindings(c, run024System())
	msgs := messagesFor(fs, ruleUCLaneResolves)
	if !anyContains(msgs, `"List owner"`) || !anyContains(msgs, `"Other entry points"`) {
		t.Fatalf("both lanes resolve to nothing: %v", msgs)
	}
	if anyContains(msgs, `"Todo list"`) {
		t.Fatalf("Todo list is a System component and resolves: %v", msgs)
	}
	if !anyContains(msgs, "team-member, team-agent") {
		t.Fatalf("the message must name the declared actors: %v", msgs)
	}
	// one finding per use case and lane, not one per node.
	n := 0
	for _, m := range msgs {
		if strings.Contains(m, `"List owner"`) && strings.Contains(m, "remove-a-todo") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("remove-a-todo draws List owner on two nodes but must be reported once, got %d: %v", n, msgs)
	}
	for _, f := range fs {
		if f.Severity != SeverityError {
			t.Fatalf("UC-LANE-RESOLVES must be an Error: %+v", f)
		}
	}
}

func TestUCLaneResolves_PassesForActorRoleActorIDAndComponentName(t *testing.T) {
	c := run024CoreUseCases(t)
	for _, uc := range []string{"remove-a-todo", "reword-a-todo"} {
		u := useCaseByID(t, &c, uc)
		for i := range u.Activity.Nodes {
			switch u.Activity.Nodes[i].RoleName {
			case "List owner":
				u.Activity.Nodes[i].RoleName = "Team member"
			case "Other entry points":
				u.Activity.Nodes[i].RoleName = "todo-web-app"
			}
		}
	}
	if m := messagesFor(ucLaneFindings(c, run024System()), ruleUCLaneResolves); len(m) != 0 {
		t.Fatalf("actor role and component id resolve: %v", m)
	}
	u := useCaseByID(t, &c, "remove-a-todo")
	u.Activity.Nodes[0].RoleName = "team-agent" // an actor id also resolves
	if m := messagesFor(ucLaneFindings(c, run024System()), ruleUCLaneResolves); len(m) != 0 {
		t.Fatalf("actor id resolves: %v", m)
	}
}

func TestUCLaneResolves_RunsAtArchitecture(t *testing.T) {
	c := run024CoreUseCases(t)
	res, err := validateArchitecture(run024System(), c)
	if err != nil {
		t.Fatal(err)
	}
	if m := messagesFor(res.Findings, ruleUCLaneResolves); len(m) == 0 {
		t.Fatalf("validateArchitecture must report unresolved lanes")
	}
}

func TestUCActorDoor_RegisteredAsEmitted(t *testing.T) {
	ids := emittedRuleIDs()
	for _, id := range []RuleID{ruleUCActorDoor, ruleUCLaneResolves} {
		if !ids[id] {
			t.Fatalf("%s must be registered in emittedRuleIDs", id)
		}
	}
}
