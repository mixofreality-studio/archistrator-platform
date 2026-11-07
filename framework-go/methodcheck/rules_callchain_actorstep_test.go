package methodcheck

import "testing"

// CC-DECIDED-BY-ACTOR-STEP: an actor-decided decision/switch is a human choice, so
// the view must say which Client the actor makes it on.

func ccDecisionDiagram() ([]ActivityNode, []ActivityEdge) {
	nodes := []ActivityNode{
		{ID: "s", Kind: nodeStart},
		{ID: "d", Kind: nodeDecision, Label: "commit or abandon?", DecidedBy: "user"},
		{ID: "act", Kind: nodeAction, Label: "do the thing"},
		{ID: "e", Kind: nodeEnd},
	}
	edges := []ActivityEdge{
		{From: "s", To: "d"}, {From: "d", To: "act", Guard: "[commit]"}, {From: "d", To: "e", Guard: "[abandon]"}, {From: "act", To: "e"},
	}
	return nodes, edges
}

func TestCC_DecidedByActorStep_Fires(t *testing.T) {
	nodes, edges := ccDecisionDiagram()
	// the actor decides, but no step of the view is keyed on the decision.
	s := sysWith(ccView(CallStep{ActivityNodeID: "act", Calls: []TraceCall{
		ccActorEntry(), {From: "web-client", To: "mgr", Mode: modeSync},
	}}))
	c := ucWith(triggerClientAction, nodes, edges, ccUser())
	fs := callChainRules(s, c)
	if !hasRuleFindings(fs, ruleCCDecidedByActorStep) {
		t.Fatalf("an actor-decided decision with no step must fire CC-DECIDED-BY-ACTOR-STEP:\n%s", dumpFindings(fs))
	}
	// a decision step with no actor→Client call still fires.
	s = sysWith(ccView(
		CallStep{ActivityNodeID: "d", Calls: []TraceCall{{From: "web-client", To: "mgr", Mode: modeSync}}},
		CallStep{ActivityNodeID: "act", Calls: []TraceCall{ccActorEntry(), {From: "web-client", To: "mgr", Mode: modeSync}}},
	))
	if !hasRuleFindings(callChainRules(s, c), ruleCCDecidedByActorStep) {
		t.Fatalf("a step without an actor→Client call must fire")
	}
}

func TestCC_DecidedByActorStep_Passes(t *testing.T) {
	nodes, edges := ccDecisionDiagram()
	s := sysWith(ccView(
		CallStep{ActivityNodeID: "d", Calls: []TraceCall{ccActorEntry()}},
		CallStep{ActivityNodeID: "act", Calls: []TraceCall{{From: "web-client", To: "mgr", Mode: modeSync}}},
	))
	c := ucWith(triggerClientAction, nodes, edges, ccUser())
	fs := callChainRules(s, c)
	if hasRuleFindings(fs, ruleCCDecidedByActorStep) {
		t.Fatalf("the actor's decision has an actor→Client step:\n%s", dumpFindings(fs))
	}
}

func TestCC_DecidedByActorStep_ComponentDeciderIsNotChecked(t *testing.T) {
	nodes, edges := ccDecisionDiagram()
	nodes[1].DecidedBy = "mgr"
	s := sysWith(ccView(CallStep{ActivityNodeID: "act", Calls: []TraceCall{ccActorEntry(), {From: "web-client", To: "mgr", Mode: modeSync}}}))
	c := ucWith(triggerClientAction, nodes, edges, ccUser())
	if hasRuleFindings(callChainRules(s, c), ruleCCDecidedByActorStep) {
		t.Fatal("a component decider needs no actor step")
	}
}
