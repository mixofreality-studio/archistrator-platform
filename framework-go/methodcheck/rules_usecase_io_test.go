package methodcheck

import (
	"strings"
	"testing"
)

// rules_usecase_io_test.go covers UC-IO-KINDS (the three I/O node kinds are well
// formed) and UC-NO-IO (a use case whose diagram yields no external stimulus) over
// inline fixtures, and their wiring into validateArchitecture.

// ioUseCase builds a one-use-case set with the customer actor and the given diagram.
func ioUseCase(nodes []ActivityNode, edges []ActivityEdge) CoreUseCases {
	return CoreUseCases{Decisions: []UseCaseDecision{{UseCase: UseCase{
		ID: "uc-io", Name: "IO flow", Classification: classCore, Trigger: triggerBusMessage,
		Actors:   []Actor{{ID: "customer", Role: "Customer"}},
		Activity: &ActivityDiagram{Nodes: nodes, Edges: edges},
	}}}}
}

func ioLinear(ids ...string) []ActivityEdge {
	var edges []ActivityEdge
	for i := 1; i < len(ids); i++ {
		edges = append(edges, ActivityEdge{From: ids[i-1], To: ids[i], Kind: edgeControlFlow})
	}
	return edges
}

func TestUCIO_CleanDiagramIsSilent(t *testing.T) {
	c := ioUseCase([]ActivityNode{
		{ID: "s", Kind: nodeStart},
		{ID: "in", Kind: kindAcceptEvent, Label: "Order received", LinkedActorID: "customer"},
		{ID: "out", Kind: kindSendSignal, Label: "Send invoice", LinkedActorID: "customer"},
		{ID: "obj", Kind: kindObjectNode, Label: "Invoice", InState: "[requested]"},
		{ID: "n", Kind: kindNote, Label: "minor units", Anchor: "in"},
		{ID: "free", Kind: kindNote, Label: "unanchored note is fine"},
		{ID: "e", Kind: nodeEnd},
	}, ioLinear("s", "in", "out", "obj", "e"))
	if fs := useCaseIORules(c, allActorIDs(c)); len(fs) != 0 {
		t.Fatalf("clean diagram produced findings:\n%s", dumpFindings(fs))
	}
}

func TestUCIO_SendSignalMustNameActor(t *testing.T) {
	for _, actor := range []string{"", "order-manager"} {
		c := ioUseCase([]ActivityNode{
			{ID: "s", Kind: nodeStart},
			{ID: "in", Kind: kindAcceptEvent, LinkedActorID: "customer"},
			{ID: "out", Kind: kindSendSignal, Label: "Send invoice", LinkedActorID: actor},
			{ID: "e", Kind: nodeEnd},
		}, ioLinear("s", "in", "out", "e"))
		fs := useCaseIORules(c, allActorIDs(c))
		wantRules(t, fs, ruleUCIOKinds, 1)
		if fs[0].Severity != SeverityError || !strings.Contains(fs[0].Message, `sendSignal "out"`) {
			t.Fatalf("linkedActorId=%q: %+v", actor, fs[0])
		}
		if fs[0].Location == nil || fs[0].Location.Section != "useCases[uc-io].activity" {
			t.Fatalf("location: %+v", fs[0].Location)
		}
	}
}

func TestUCIO_ObjectNodeMustCarryInState(t *testing.T) {
	c := ioUseCase([]ActivityNode{
		{ID: "s", Kind: nodeStart},
		{ID: "in", Kind: kindAcceptEvent, LinkedActorID: "customer"},
		{ID: "obj", Kind: kindObjectNode, Label: "Invoice"},
		{ID: "e", Kind: nodeEnd},
	}, ioLinear("s", "in", "obj", "e"))
	fs := useCaseIORules(c, allActorIDs(c))
	wantRules(t, fs, ruleUCIOKinds, 1)
	if !strings.Contains(fs[0].Message, `objectNode "obj"`) {
		t.Fatalf("%+v", fs[0])
	}
}

func TestUCIO_NoteAnchorMustResolve(t *testing.T) {
	c := ioUseCase([]ActivityNode{
		{ID: "s", Kind: nodeStart},
		{ID: "in", Kind: kindAcceptEvent, LinkedActorID: "customer"},
		{ID: "n", Kind: kindNote, Label: "hint", Anchor: "ghost"},
		{ID: "e", Kind: nodeEnd},
	}, ioLinear("s", "in", "e"))
	fs := useCaseIORules(c, allActorIDs(c))
	wantRules(t, fs, ruleUCIOKinds, 1)
	if !strings.Contains(fs[0].Message, `note "n"`) || !strings.Contains(fs[0].Message, `"ghost"`) {
		t.Fatalf("%+v", fs[0])
	}
}

// Every input-yielding shape of §2.2 rule 1 counts: acceptEvent, timeEvent, an action
// in an actor's lane, and a decision/switch decided by an actor. A diagram with none
// of them is UC-NO-IO — a Warning, since it describes the design, not a binding.
func TestUCIO_NoInputIsWarning(t *testing.T) {
	c := ioUseCase([]ActivityNode{
		{ID: "s", Kind: nodeStart},
		{ID: "act", Kind: nodeAction, Label: "internal work", RoleName: "order-manager"},
		{ID: "d", Kind: nodeDecision, DecidedBy: "order-manager"},
		{ID: "e", Kind: nodeEnd},
	}, ioLinear("s", "act", "d", "e"))
	fs := useCaseIORules(c, allActorIDs(c))
	wantRules(t, fs, ruleUCNoIO, 1)
	if fs[0].Severity != SeverityWarning {
		t.Fatalf("UC-NO-IO must be a Warning: %+v", fs[0])
	}
}

func TestUCIO_EachInputKindSatisfies(t *testing.T) {
	for name, n := range map[string]ActivityNode{
		"acceptEvent":    {ID: "in", Kind: kindAcceptEvent, LinkedActorID: "customer"},
		"timeEvent":      {ID: "in", Kind: kindTimeEvent, Label: "nightly"},
		"actorAction":    {ID: "in", Kind: nodeAction, LinkedActorID: "customer"},
		"actorDecision":  {ID: "in", Kind: nodeDecision, DecidedBy: "customer"},
		"actorSwitch":    {ID: "in", Kind: nodeSwitch, DecidedBy: "customer"},
		"componentOwned": {ID: "in", Kind: nodeAction, LinkedActorID: "order-manager"},
	} {
		c := ioUseCase([]ActivityNode{{ID: "s", Kind: nodeStart}, n, {ID: "e", Kind: nodeEnd}}, ioLinear("s", "in", "e"))
		fs := useCaseIORules(c, allActorIDs(c))
		want := 0
		if name == "componentOwned" {
			want = 1
		}
		if got := len(fs); got != want {
			t.Errorf("%s: got %d findings, want %d\n%s", name, got, want, dumpFindings(fs))
		}
	}
}

func TestUCIO_NoActivityIsSkipped(t *testing.T) {
	c := ioUseCase(nil, nil)
	c.Decisions[0].UseCase.Activity = nil
	if fs := useCaseIORules(c, allActorIDs(c)); len(fs) != 0 {
		t.Fatalf("a use case without a diagram is UC-ACT-PRESENT's concern:\n%s", dumpFindings(fs))
	}
}

// The family runs inside validateArchitecture beside the CC-* rules.
func TestUCIO_WiredIntoValidateArchitecture(t *testing.T) {
	c := ioUseCase([]ActivityNode{
		{ID: "s", Kind: nodeStart},
		{ID: "in", Kind: kindAcceptEvent, LinkedActorID: "customer"},
		{ID: "obj", Kind: kindObjectNode, Label: "Invoice"},
		{ID: "e", Kind: nodeEnd},
	}, ioLinear("s", "in", "obj", "e"))
	res, err := validateArchitecture(sysWith(), c)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRuleFindings(res.Findings, ruleUCIOKinds) {
		t.Fatalf("UC-IO-KINDS not reachable through validateArchitecture:\n%s", dumpFindings(res.Findings))
	}
}
