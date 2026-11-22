package methodcheck

import "testing"

// ISSUE-PLAT-UC-EACH-ACTOR-DOOR: UC-ACTOR-DOOR passed a use case whose declared actors were
// not all served, as long as any one of them was. UC-ACTOR-UNSERVED asks it of each.

func servedByTeamMember(t *testing.T) (*UseCase, CoreUseCases) {
	t.Helper()
	c := run024CoreUseCases(t)
	uc := useCaseByID(t, &c, "remove-a-todo")
	linkActorNodes(uc, "team-member", "Team member", "a1")
	return uc, c
}

func TestUCActorUnserved_FiresForADeclaredActorWithNoDoor(t *testing.T) {
	_, c := servedByTeamMember(t)
	m := messagesFor(ucSlotPathFindings(c), ruleUCActorUnserved)
	if !anyContains(m, "remove-a-todo declares actor team-agent but no node is linked to it") {
		t.Fatalf("team-agent has no door: %v", m)
	}
	if anyContains(m, "declares actor team-member") {
		t.Fatalf("team-member acts and must not be named a no-door actor: %v", m)
	}
}

func TestUCActorUnserved_FiresForAnActorWithADoorButNoOutcome(t *testing.T) {
	uc, c := servedByTeamMember(t)
	linkActorNodes(uc, "team-agent", "Team agent", "a2")
	m := messagesFor(ucSlotPathFindings(c), ruleUCActorUnserved)
	if !anyContains(m, "actor team-agent acts but no outcome is shown to it") {
		t.Fatalf("team-agent acts and is told nothing: %v", m)
	}
}

func TestUCActorUnserved_PassesWhenEveryActorHasDoorAndOutcome(t *testing.T) {
	uc, c := servedByTeamMember(t)
	linkActorNodes(uc, "team-agent", "Team agent", "a2")
	uc.Activity.Nodes = append(uc.Activity.Nodes,
		ActivityNode{ID: "sigm", Kind: kindSendSignal, Label: "member told", LinkedActorID: "team-member"},
		ActivityNode{ID: "siga", Kind: kindSendSignal, Label: "agent told", LinkedActorID: "team-agent"})
	if m := messagesFor(ucSlotPathFindings(c), ruleUCActorUnserved); len(m) != 0 {
		t.Fatalf("both actors have a door and an outcome: %v", m)
	}
}

func TestUCActorUnserved_ObjectNodeInTheActorsLaneIsAnOutcome(t *testing.T) {
	uc, c := servedByTeamMember(t)
	linkActorNodes(uc, "team-agent", "Team agent", "a2")
	uc.Activity.Nodes = append(uc.Activity.Nodes,
		ActivityNode{ID: "sigm", Kind: kindSendSignal, LinkedActorID: "team-member"},
		ActivityNode{ID: "obja", Kind: kindObjectNode, RoleName: "Team agent", InState: "[removed]"})
	if m := messagesFor(ucSlotPathFindings(c), ruleUCActorUnserved); len(m) != 0 {
		t.Fatalf("an objectNode in the actor's lane is its outcome: %v", m)
	}
}

func TestUCActorUnserved_SkipsUseCasesNobodyInitiates(t *testing.T) {
	uc, c := servedByTeamMember(t)
	uc.Trigger = triggerTimer
	if m := messagesFor(ucSlotPathFindings(c), ruleUCActorUnserved); anyContains(m, "remove-a-todo") {
		t.Fatalf("a timer use case is exempt: %v", m)
	}
}

func TestUCActorUnserved_LeavesAnActorlessDiagramToActorDoor(t *testing.T) {
	c := run024CoreUseCases(t)
	if m := messagesFor(ucSlotPathFindings(c), ruleUCActorUnserved); anyContains(m, "remove-a-todo") {
		t.Fatalf("UC-ACTOR-DOOR owns the no-actor-node case: %v", m)
	}
}
