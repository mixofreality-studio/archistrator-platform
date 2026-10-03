package methodcheck

import (
	"encoding/json"
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

// projectWithUseCases commits c as slot 4 of an otherwise empty project.
func projectWithUseCases(t *testing.T, c CoreUseCases) Project {
	t.Helper()
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	return Project{Slots: map[string]Slot{"4": {Status: reviewCommitted, Kind: kindCoreUseCases, Model: raw}}}
}

// UC-START-REDUNDANT (Amendment A3): a diagram with both a start node and an event
// entry whose start family carries no stimulus of its own is reported once, as a
// Warning located on the use case's diagram; the derivation drops that family.
func TestUCStartRedundant_FiresOncePerUseCase(t *testing.T) {
	c := ioUseCase([]ActivityNode{
		{ID: "s", Kind: nodeStart},
		{ID: "tick", Kind: kindTimeEvent, Label: "Pump due"},
		{ID: "a", Kind: nodeAction, Label: "Work"},
		{ID: "d", Kind: nodeDecision, Label: "Done?"},
		{ID: "b", Kind: nodeAction, Label: "More"},
		{ID: "e", Kind: nodeEnd},
	}, []ActivityEdge{
		{From: "s", To: "a", Kind: edgeControlFlow},
		{From: "tick", To: "a", Kind: edgeControlFlow},
		{From: "a", To: "d", Kind: edgeControlFlow},
		{From: "d", To: "e", Kind: edgeGuardedFlow, Guard: "yes"},
		{From: "d", To: "b", Kind: edgeGuardedFlow, Guard: "no"},
		{From: "b", To: "e", Kind: edgeControlFlow},
	})
	fs := runStartRedundant(t, projectWithUseCases(t, c))
	wantRules(t, fs, ruleUCStartRedundant, 1)
	f := fs[0]
	if f.Severity != SeverityWarning || !strings.Contains(f.Message, "2 path") ||
		f.Location == nil || f.Location.Section != "useCases[uc-io].activity" {
		t.Fatalf("UC-START-REDUNDANT shape: %+v %+v", f, f.Location)
	}
}

// A diagram with only a start entry, or a start family with a stimulus of its
// own, is silent.
func TestUCStartRedundant_SilentWithoutRedundancy(t *testing.T) {
	startOnly := ioUseCase([]ActivityNode{
		{ID: "s", Kind: nodeStart},
		{ID: "a", Kind: nodeAction, Label: "Work"},
		{ID: "e", Kind: nodeEnd},
	}, ioLinear("s", "a", "e"))
	ownStimulus := ioUseCase([]ActivityNode{
		{ID: "s", Kind: nodeStart},
		{ID: "in", Kind: kindAcceptEvent, Label: "Order received", LinkedActorID: "customer"},
		{ID: "tick", Kind: kindTimeEvent, Label: "Pump due"},
		{ID: "e", Kind: nodeEnd},
	}, []ActivityEdge{
		{From: "s", To: "in", Kind: edgeControlFlow},
		{From: "in", To: "e", Kind: edgeControlFlow},
		{From: "tick", To: "e", Kind: edgeControlFlow},
	})
	for name, c := range map[string]CoreUseCases{"start only": startOnly, "own stimulus": ownStimulus} {
		t.Run(name, func(t *testing.T) {
			wantRules(t, runStartRedundant(t, projectWithUseCases(t, c)), ruleUCStartRedundant, 0)
		})
	}
}

// runStartRedundant derives p's scenarios and runs UC-START-REDUNDANT over what
// the derivation dropped, as scenarioFindings does.
func runStartRedundant(t *testing.T, p Project) []Finding {
	t.Helper()
	r, err := deriveReport(p)
	if err != nil {
		t.Fatalf("deriveReport: %v", err)
	}
	fs, err := startRedundantFindings(p, r.StartRedundant)
	if err != nil {
		t.Fatalf("startRedundantFindings: %v", err)
	}
	return fs
}

// The rule is wired into ValidateProject and registered as an emitted rule.
func TestUCStartRedundant_Wired(t *testing.T) {
	c := ioUseCase([]ActivityNode{
		{ID: "s", Kind: nodeStart},
		{ID: "tick", Kind: kindTimeEvent, Label: "Pump due"},
		{ID: "a", Kind: nodeAction, Label: "Work"},
		{ID: "e", Kind: nodeEnd},
	}, []ActivityEdge{
		{From: "s", To: "a", Kind: edgeControlFlow},
		{From: "tick", To: "a", Kind: edgeControlFlow},
		{From: "a", To: "e", Kind: edgeControlFlow},
	})
	fs, err := ValidateProject(projectWithUseCases(t, c))
	if err != nil {
		t.Fatal(err)
	}
	if !hasRuleFindings(fs, ruleUCStartRedundant) {
		t.Fatalf("ValidateProject must surface UC-START-REDUNDANT:\n%s", dumpFindings(fs))
	}
	if !emittedRuleIDs()[ruleUCStartRedundant] {
		t.Fatal("UC-START-REDUNDANT missing from the emitted-rule registry")
	}
}
