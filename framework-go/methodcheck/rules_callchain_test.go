package methodcheck

import "testing"

// rules_callchain_test.go covers the CC-* call-chain correspondence family — one
// behavior per Test func, with inline fixtures (the idiom rules_test.go /
// rules_dynamic_test.go / activitypaths_test.go already use).

// ccUseCaseID is the id the shared fixtures join a DynamicView to its owning
// UseCase on (DynamicView.UseCaseID → UseCase.ID).
const ccUseCaseID = "uc-cc"

// sysWith builds the shared PERSON-FREE component roster the CC fixtures call
// through — web-client (Client) → mgr (Manager) → ra (ResourceAccess) → res
// (Resource) — plus the matching static sync relationships, so a realized call
// chain over it trips none of the DV-* static/dynamic rules.
func sysWith(dvs ...DynamicView) System {
	return System{
		Components: []Component{
			{ID: "web-client", Name: "WebClient", Kind: kindClient, Layer: layerClient, Encapsulates: "the transport entry point"},
			{ID: "mgr", Name: "FlowManager", Kind: kindManager, Layer: layerManager, Encapsulates: "the flow volatility"},
			{ID: "ra", Name: "StateAccess", Kind: kindResourceAccess, Layer: layerResourceAccess, Encapsulates: "the state store"},
			{ID: "res", Name: "StateDB", Kind: kindResource, Layer: layerResource, Encapsulates: "the database"},
		},
		Relationships: []Relationship{
			{From: "web-client", To: "mgr", Mode: modeSync},
			{From: "mgr", To: "ra", Mode: modeSync},
			{From: "ra", To: "res", Mode: modeSync},
		},
		DynamicViews: dvs,
	}
}

// ucWith builds the single-use-case CoreUseCases set the CC fixtures validate
// against: the owning use case of every view sysWith carries.
func ucWith(trigger string, nodes []ActivityNode, edges []ActivityEdge, actors ...Actor) CoreUseCases {
	return CoreUseCases{Decisions: []UseCaseDecision{{UseCase: UseCase{
		ID:             ccUseCaseID,
		Name:           "CC flow",
		Classification: classCore,
		Trigger:        trigger,
		Actors:         actors,
		Activity:       &ActivityDiagram{Nodes: nodes, Edges: edges},
	}}}}
}

// ccView wraps steps into the dynamic view sysWith's fixtures carry.
func ccView(steps ...CallStep) DynamicView {
	return DynamicView{UseCaseID: ccUseCaseID, Key: "cc", Title: "CC flow", Steps: steps}
}

// ccLinearNodes/ccLinearEdges are the smallest well-formed diagram: start → act → end.
func ccLinearNodes() []ActivityNode {
	return []ActivityNode{
		{ID: "s", Kind: nodeStart},
		{ID: "act", Kind: nodeAction, Label: "do the thing"},
		{ID: "e", Kind: nodeEnd},
	}
}

func ccLinearEdges() []ActivityEdge {
	return []ActivityEdge{{From: "s", To: "act"}, {From: "act", To: "e"}}
}

// ccActorEntry is the canonical legal chain root: the user actor enters the Client.
func ccActorEntry() Relationship {
	return Relationship{From: "user", To: "web-client", Mode: modeSync}
}

func ccUser() Actor { return Actor{ID: "user", Role: "User"} }

// ---- CC-STEP-NODE / CC-STEP-UNIQUE ----

func TestCC_StepNodeDanglingFires(t *testing.T) {
	s := sysWith(ccView(CallStep{ActivityNodeID: "ghost-node", Calls: []Relationship{
		ccActorEntry(), {From: "web-client", To: "mgr", Mode: modeSync},
	}}))
	c := ucWith(triggerClientAction, ccLinearNodes(), ccLinearEdges(), ccUser())
	if !hasRuleFindings(callChainRules(s, c), ruleCCStepNode) {
		t.Fatalf("a step keyed on a node the activity diagram does not declare must fire CC-STEP-NODE")
	}
}

func TestCC_StepUniqueDuplicateFires(t *testing.T) {
	s := sysWith(ccView(
		CallStep{ActivityNodeID: "act", Calls: []Relationship{ccActorEntry()}},
		CallStep{ActivityNodeID: "act", Calls: []Relationship{{From: "web-client", To: "mgr", Mode: modeSync}}},
	))
	c := ucWith(triggerClientAction, ccLinearNodes(), ccLinearEdges(), ccUser())
	if !hasRuleFindings(callChainRules(s, c), ruleCCStepUnique) {
		t.Fatalf("two steps realizing the same activity node must fire CC-STEP-UNIQUE")
	}
}

// ---- CC-COVERAGE ----

func TestCC_CoverageActionWithoutStepFires(t *testing.T) {
	nodes := []ActivityNode{
		{ID: "s", Kind: nodeStart},
		{ID: "a1", Kind: nodeAction, Label: "first"},
		{ID: "a2", Kind: nodeAction, Label: "second"},
		{ID: "e", Kind: nodeEnd},
	}
	edges := []ActivityEdge{{From: "s", To: "a1"}, {From: "a1", To: "a2"}, {From: "a2", To: "e"}}
	s := sysWith(ccView(CallStep{ActivityNodeID: "a1", Calls: []Relationship{ccActorEntry()}}))
	c := ucWith(triggerClientAction, nodes, edges, ccUser())
	if !hasRuleFindings(callChainRules(s, c), ruleCCCoverage) {
		t.Fatalf("an action node realized by no step must fire CC-COVERAGE")
	}
}

func TestCC_CoverageStepOnMergeFires(t *testing.T) {
	nodes := []ActivityNode{
		{ID: "s", Kind: nodeStart},
		{ID: "d", Kind: nodeDecision, Label: "valid?"},
		{ID: "a1", Kind: nodeAction, Label: "accept"},
		{ID: "a2", Kind: nodeAction, Label: "reject"},
		{ID: "m", Kind: nodeMerge},
		{ID: "e", Kind: nodeEnd},
	}
	edges := []ActivityEdge{
		{From: "s", To: "d"},
		{From: "d", To: "a1", Kind: edgeGuardedFlow, Guard: "[yes]"},
		{From: "d", To: "a2", Kind: edgeGuardedFlow, Guard: "[no]"},
		{From: "a1", To: "m"}, {From: "a2", To: "m"}, {From: "m", To: "e"},
	}
	s := sysWith(ccView(
		CallStep{ActivityNodeID: "a1", Calls: []Relationship{ccActorEntry()}},
		CallStep{ActivityNodeID: "a2", Calls: []Relationship{ccActorEntry()}},
		CallStep{ActivityNodeID: "m", Calls: []Relationship{{From: "web-client", To: "mgr", Mode: modeSync}}},
	))
	c := ucWith(triggerClientAction, nodes, edges, ccUser())
	if !hasRuleFindings(callChainRules(s, c), ruleCCCoverage) {
		t.Fatalf("a step keyed on a merge node (not step-eligible) must fire CC-COVERAGE")
	}
}

// TestCC_CoverageDecisionStepOptional pins the mayHaveStep band: a decision node
// is legal WITH a step and legal WITHOUT one — neither direction is a finding.
func TestCC_CoverageDecisionStepOptional(t *testing.T) {
	nodes := []ActivityNode{
		{ID: "s", Kind: nodeStart},
		{ID: "d", Kind: nodeDecision, Label: "valid?"},
		{ID: "a1", Kind: nodeAction, Label: "accept"},
		{ID: "a2", Kind: nodeAction, Label: "reject"},
		{ID: "e", Kind: nodeEnd},
	}
	edges := []ActivityEdge{
		{From: "s", To: "d"},
		{From: "d", To: "a1", Kind: edgeGuardedFlow, Guard: "[yes]"},
		{From: "d", To: "a2", Kind: edgeGuardedFlow, Guard: "[no]"},
		{From: "a1", To: "e"}, {From: "a2", To: "e"},
	}
	c := ucWith(triggerClientAction, nodes, edges, ccUser())

	without := sysWith(ccView(
		CallStep{ActivityNodeID: "a1", Calls: []Relationship{ccActorEntry()}},
		CallStep{ActivityNodeID: "a2", Calls: []Relationship{ccActorEntry()}},
	))
	if hasRuleFindings(callChainRules(without, c), ruleCCCoverage) {
		t.Fatalf("a decision node WITHOUT a step must not fire CC-COVERAGE (mayHaveStep)")
	}

	with := sysWith(ccView(
		CallStep{ActivityNodeID: "d", Calls: []Relationship{ccActorEntry()}},
		CallStep{ActivityNodeID: "a1", Calls: []Relationship{{From: "web-client", To: "mgr", Mode: modeSync}}},
		CallStep{ActivityNodeID: "a2", Calls: []Relationship{{From: "web-client", To: "mgr", Mode: modeSync}}},
	))
	if hasRuleFindings(callChainRules(with, c), ruleCCCoverage) {
		t.Fatalf("a decision node WITH a step must not fire CC-COVERAGE (mayHaveStep)")
	}
}

// ---- CC-STEP-NONEMPTY ----

func TestCC_StepNonemptyFires(t *testing.T) {
	s := sysWith(ccView(CallStep{ActivityNodeID: "act", Calls: []Relationship{}}))
	c := ucWith(triggerClientAction, ccLinearNodes(), ccLinearEdges(), ccUser())
	if !hasRuleFindings(callChainRules(s, c), ruleCCStepNonempty) {
		t.Fatalf("a realized step that makes no call must fire CC-STEP-NONEMPTY")
	}
}

// ---- CC-ENDPOINT-RESOLVES ----

func TestCC_EndpointResolvesUnknownFires(t *testing.T) {
	s := sysWith(ccView(CallStep{ActivityNodeID: "act", Calls: []Relationship{
		ccActorEntry(), {From: "web-client", To: "ghost-component", Mode: modeSync},
	}}))
	c := ucWith(triggerClientAction, ccLinearNodes(), ccLinearEdges(), ccUser())
	if !hasRuleFindings(callChainRules(s, c), ruleCCEndpoint) {
		t.Fatalf("a call endpoint resolving to neither a Component nor an Actor must fire CC-ENDPOINT-RESOLVES")
	}
}

// TestCC_EndpointActorResolves: an actor id drawn from the OWNING use case's
// Actors list is a legal call endpoint — it must not be reported as unresolved.
func TestCC_EndpointActorResolves(t *testing.T) {
	s := sysWith(ccView(CallStep{ActivityNodeID: "act", Calls: []Relationship{
		ccActorEntry(), {From: "web-client", To: "mgr", Mode: modeSync},
	}}))
	c := ucWith(triggerClientAction, ccLinearNodes(), ccLinearEdges(), ccUser())
	if hasRuleFindings(callChainRules(s, c), ruleCCEndpoint) {
		t.Fatalf("an actor id from the owning use case must resolve; got CC-ENDPOINT-RESOLVES")
	}
}

// ---- CC-ACTOR-EDGE ----

func TestCC_ActorEdgeNonClientFires(t *testing.T) {
	s := sysWith(ccView(CallStep{ActivityNodeID: "act", Calls: []Relationship{
		{From: "user", To: "mgr", Mode: modeSync},
	}}))
	c := ucWith(triggerClientAction, ccLinearNodes(), ccLinearEdges(), ccUser())
	if !hasRuleFindings(callChainRules(s, c), ruleCCActorEdge) {
		t.Fatalf("an actor calling a non-Client component must fire CC-ACTOR-EDGE")
	}
}

func TestCC_ActorEdgeQueuedFires(t *testing.T) {
	s := sysWith(ccView(CallStep{ActivityNodeID: "act", Calls: []Relationship{
		{From: "user", To: "web-client", Mode: modeQueued},
	}}))
	c := ucWith(triggerClientAction, ccLinearNodes(), ccLinearEdges(), ccUser())
	if !hasRuleFindings(callChainRules(s, c), ruleCCActorEdge) {
		t.Fatalf("a queued actor edge must fire CC-ACTOR-EDGE (an actor interaction is synchronous)")
	}
}

func TestCC_ActorToActorFires(t *testing.T) {
	s := sysWith(ccView(CallStep{ActivityNodeID: "act", Calls: []Relationship{
		{From: "user", To: "approver", Mode: modeSync},
	}}))
	c := ucWith(triggerClientAction, ccLinearNodes(), ccLinearEdges(),
		ccUser(), Actor{ID: "approver", Role: "Approver"})
	if !hasRuleFindings(callChainRules(s, c), ruleCCActorEdge) {
		t.Fatalf("an actor→actor call must fire CC-ACTOR-EDGE")
	}
}

// ---- CC-ACTOR-LANE ----

func TestCC_ActorLaneMismatchFires(t *testing.T) {
	nodes := []ActivityNode{
		{ID: "s", Kind: nodeStart},
		{ID: "act", Kind: nodeAction, Label: "do the thing", RoleName: "User", LinkedActorID: "user"},
		{ID: "e", Kind: nodeEnd},
	}
	// The step realizing the user-laned node never touches the user actor.
	s := sysWith(ccView(CallStep{ActivityNodeID: "act", Calls: []Relationship{
		{From: "web-client", To: "mgr", Mode: modeSync},
	}}))
	c := ucWith(triggerClientAction, nodes, ccLinearEdges(), ccUser())
	if !hasRuleFindings(callChainRules(s, c), ruleCCActorLane) {
		t.Fatalf("a lane-linked node whose step never touches that actor must fire CC-ACTOR-LANE")
	}
}

// ---- CC-TRIGGER-EVENT ----

func TestCC_TriggerEventTimerWithoutTimeEventFires(t *testing.T) {
	s := sysWith(ccView(CallStep{ActivityNodeID: "act", Calls: []Relationship{
		{From: "web-client", To: "mgr", Mode: modeSync},
	}}))
	c := ucWith(triggerTimer, ccLinearNodes(), ccLinearEdges())
	if !hasRuleFindings(callChainRules(s, c), ruleCCTriggerEvent) {
		t.Fatalf("a timer-triggered use case whose diagram carries no timeEvent entry must fire CC-TRIGGER-EVENT")
	}
}

func TestCC_TriggerEventClientActionWithEventEntryFires(t *testing.T) {
	nodes := []ActivityNode{
		{ID: "tick", Kind: kindTimeEvent, Label: "period elapses"},
		{ID: "act", Kind: nodeAction, Label: "sweep"},
		{ID: "e", Kind: nodeEnd},
	}
	edges := []ActivityEdge{{From: "tick", To: "act"}, {From: "act", To: "e"}}
	s := sysWith(ccView(
		CallStep{ActivityNodeID: "tick", Calls: []Relationship{{From: "web-client", To: "mgr", Mode: modeSync}}},
		CallStep{ActivityNodeID: "act", Calls: []Relationship{{From: "mgr", To: "ra", Mode: modeSync}}},
	))
	c := ucWith(triggerClientAction, nodes, edges)
	if !hasRuleFindings(callChainRules(s, c), ruleCCTriggerEvent) {
		t.Fatalf("a clientAction use case whose diagram carries an event entry must fire CC-TRIGGER-EVENT")
	}
}

// ---- CC-PATH-CONNECTED ----

func TestCC_PathConnected_HappyChainPasses(t *testing.T) {
	nodes := []ActivityNode{
		{ID: "s", Kind: nodeStart},
		{ID: "a1", Kind: nodeAction, Label: "submit"},
		{ID: "a2", Kind: nodeAction, Label: "persist"},
		{ID: "e", Kind: nodeEnd},
	}
	edges := []ActivityEdge{{From: "s", To: "a1"}, {From: "a1", To: "a2"}, {From: "a2", To: "e"}}
	s := sysWith(ccView(
		CallStep{ActivityNodeID: "a1", Calls: []Relationship{
			ccActorEntry(), {From: "web-client", To: "mgr", Mode: modeSync},
		}},
		CallStep{ActivityNodeID: "a2", Calls: []Relationship{{From: "mgr", To: "ra", Mode: modeSync}}},
	))
	c := ucWith(triggerClientAction, nodes, edges, ccUser())
	if hasRuleFindings(callChainRules(s, c), ruleCCPathConnected) {
		t.Fatalf("an actor-rooted chain whose later step continues from a reached component must not fire CC-PATH-CONNECTED")
	}
}

func TestCC_PathConnected_DisconnectedFromFires(t *testing.T) {
	nodes := []ActivityNode{
		{ID: "s", Kind: nodeStart},
		{ID: "a1", Kind: nodeAction, Label: "submit"},
		{ID: "a2", Kind: nodeAction, Label: "persist"},
		{ID: "e", Kind: nodeEnd},
	}
	edges := []ActivityEdge{{From: "s", To: "a1"}, {From: "a1", To: "a2"}, {From: "a2", To: "e"}}
	s := sysWith(ccView(
		CallStep{ActivityNodeID: "a1", Calls: []Relationship{
			ccActorEntry(), {From: "web-client", To: "mgr", Mode: modeSync},
		}},
		// ra was never reached by the first step's fragment.
		CallStep{ActivityNodeID: "a2", Calls: []Relationship{{From: "ra", To: "res", Mode: modeSync}}},
	))
	c := ucWith(triggerClientAction, nodes, edges, ccUser())
	if !hasRuleFindings(callChainRules(s, c), ruleCCPathConnected) {
		t.Fatalf("a later step calling FROM a component the chain never reached must fire CC-PATH-CONNECTED")
	}
}

// TestCC_PathConnected_MidChainActorReentryPasses: a mid-chain fragment rooted at a
// SECOND actor (never reached by any earlier call) is legal — an actor→Client call
// always re-seeds the chain.
func TestCC_PathConnected_MidChainActorReentryPasses(t *testing.T) {
	nodes := []ActivityNode{
		{ID: "s", Kind: nodeStart},
		{ID: "a1", Kind: nodeAction, Label: "submit"},
		{ID: "a2", Kind: nodeAction, Label: "approve"},
		{ID: "e", Kind: nodeEnd},
	}
	edges := []ActivityEdge{{From: "s", To: "a1"}, {From: "a1", To: "a2"}, {From: "a2", To: "e"}}
	s := sysWith(ccView(
		CallStep{ActivityNodeID: "a1", Calls: []Relationship{
			ccActorEntry(), {From: "web-client", To: "mgr", Mode: modeSync},
		}},
		CallStep{ActivityNodeID: "a2", Calls: []Relationship{
			{From: "approver", To: "web-client", Mode: modeSync},
			{From: "web-client", To: "mgr", Mode: modeSync},
		}},
	))
	c := ucWith(triggerClientAction, nodes, edges, ccUser(), Actor{ID: "approver", Role: "Approver"})
	if hasRuleFindings(callChainRules(s, c), ruleCCPathConnected) {
		t.Fatalf("a mid-chain actor→Client re-entry must not fire CC-PATH-CONNECTED")
	}
}

// TestCC_PathConnected_TimeEventRootClientToManagerPasses: on a timeEvent-entry path
// the first call is legally rooted at a Client→Manager call (the scheduler client),
// with no actor involved.
func TestCC_PathConnected_TimeEventRootClientToManagerPasses(t *testing.T) {
	nodes := []ActivityNode{
		{ID: "tick", Kind: kindTimeEvent, Label: "period elapses"},
		{ID: "act", Kind: nodeAction, Label: "sweep"},
		{ID: "e", Kind: nodeEnd},
	}
	edges := []ActivityEdge{{From: "tick", To: "act"}, {From: "act", To: "e"}}
	s := sysWith(ccView(
		CallStep{ActivityNodeID: "tick", Calls: []Relationship{{From: "sched-client", To: "mgr", Mode: modeSync}}},
		CallStep{ActivityNodeID: "act", Calls: []Relationship{{From: "mgr", To: "ra", Mode: modeSync}}},
	))
	s.Components = append(s.Components, Component{
		ID: "sched-client", Name: "SchedulerClient", Kind: kindClient, Layer: layerClient,
		Encapsulates: "the scheduled entry point",
	})
	s.Relationships = append(s.Relationships, Relationship{From: "sched-client", To: "mgr", Mode: modeSync})
	c := ucWith(triggerTimer, nodes, edges)
	if hasRuleFindings(callChainRules(s, c), ruleCCPathConnected) {
		t.Fatalf("a timeEvent-entry path rooted at a Client→Manager call must not fire CC-PATH-CONNECTED")
	}
}

// TestCC_PathConnected_MidFlowEventNodeDoesNotFire is fix-round-1's regression guard.
// activityPaths makes EVERY event node an enumeration root, so a MID-FLOW acceptEvent
// (start → a1 → ev → a2 → end) also yields the bare suffix [ev, a2, end]. Walking that
// suffix would restart with an empty reached set and judge ev's step — connected on the
// full path — against the acceptEvent ROOT shape, a false positive the (node,from,to)
// dedup cannot absorb (the full path never reported the call). An EVENT-kind entry with
// an incoming edge is now skipped as the suffix it is (see the start-rooted companion
// below for why the predicate is not incoming-count alone).
func TestCC_PathConnected_MidFlowEventNodeDoesNotFire(t *testing.T) {
	nodes := []ActivityNode{
		{ID: "s", Kind: nodeStart},
		{ID: "a1", Kind: nodeAction, Label: "submit"},
		{ID: "ev", Kind: kindAcceptEvent, Label: "await approval"},
		{ID: "a2", Kind: nodeAction, Label: "persist"},
		{ID: "e", Kind: nodeEnd},
	}
	edges := []ActivityEdge{
		{From: "s", To: "a1"}, {From: "a1", To: "ev"}, {From: "ev", To: "a2"}, {From: "a2", To: "e"},
	}
	s := sysWith(ccView(
		CallStep{ActivityNodeID: "a1", Calls: []Relationship{
			ccActorEntry(), {From: "web-client", To: "mgr", Mode: modeSync},
		}},
		// Perfectly connected on the full path: mgr was reached by a1's fragment.
		CallStep{ActivityNodeID: "ev", Calls: []Relationship{{From: "mgr", To: "ra", Mode: modeSync}}},
		CallStep{ActivityNodeID: "a2", Calls: []Relationship{{From: "ra", To: "res", Mode: modeSync}}},
	))
	c := ucWith(triggerClientAction, nodes, edges, ccUser())
	if hasRuleFindings(callChainRules(s, c), ruleCCPathConnected) {
		t.Fatalf("a mid-flow event node on a connected chain must not fire CC-PATH-CONNECTED (its suffix path is not an ingress)")
	}
}

// TestCC_PathConnected_StartWithBackEdgeStillWalked is fix-round-2's regression guard,
// encoding the re-reviewer's empirical probe. A start node with a guarded back-edge into
// it ("retry from the top") is an ordinary authored shape, and a start node is ALWAYS a
// primary entry — never a suffix of anything. The fix-round-1 predicate skipped on
// incoming-count ALONE, which made CC-PATH-CONNECTED silently VACUOUS for such a
// diagram: a genuinely disconnected call produced zero findings from this rule,
// UC-ACTDIAG and UC-ACT-PRESENT alike. The skip is now restricted to EVENT-kind entries.
func TestCC_PathConnected_StartWithBackEdgeStillWalked(t *testing.T) {
	nodes := []ActivityNode{
		{ID: "s", Kind: nodeStart},
		{ID: "a1", Kind: nodeAction, Label: "attempt"},
		{ID: "d", Kind: nodeDecision, Label: "succeeded?"},
		{ID: "e", Kind: nodeEnd},
	}
	edges := []ActivityEdge{
		{From: "s", To: "a1"},
		{From: "a1", To: "d"},
		{From: "d", To: "s", Kind: edgeGuardedFlow, Guard: "[retry]"}, // back-edge INTO start
		{From: "d", To: "e", Kind: edgeGuardedFlow, Guard: "[done]"},
	}
	// ra is a real component the chain never reaches — a genuine disconnect.
	s := sysWith(ccView(
		CallStep{ActivityNodeID: "a1", Calls: []Relationship{{From: "ra", To: "res", Mode: modeSync}}},
	))
	c := ucWith(triggerClientAction, nodes, edges, ccUser())
	if !hasRuleFindings(callChainRules(s, c), ruleCCPathConnected) {
		t.Fatalf("a start node with a back-edge is still a primary entry; a disconnected call on its path must fire CC-PATH-CONNECTED")
	}
}

// TestCC_PathConnected_AcceptEventEntryQueuedRootPasses is the companion to the
// mid-flow guard: an EDGE-LESS acceptEvent entry IS a real ingress, is still walked, and
// its root shape (a queued call into a Manager) is still enforced — here, satisfied.
func TestCC_PathConnected_AcceptEventEntryQueuedRootPasses(t *testing.T) {
	s, c := ccAcceptEventFixture(modeQueued)
	out := callChainRules(s, c)
	if hasRuleFindings(out, ruleCCPathConnected) {
		t.Fatalf("an acceptEvent entry whose first call is queued into a Manager must not fire CC-PATH-CONNECTED")
	}
	if hasRuleFindings(out, ruleCCTriggerEvent) {
		t.Fatalf("a busMessage use case WITH an acceptEvent entry must not fire CC-TRIGGER-EVENT")
	}
}

// TestCC_PathConnected_AcceptEventEntrySyncRootFires: the same edge-less entry with a
// SYNC first call violates the acceptEvent root shape — proving the suffix skip removed
// only suffix paths, not root enforcement.
func TestCC_PathConnected_AcceptEventEntrySyncRootFires(t *testing.T) {
	s, c := ccAcceptEventFixture(modeSync)
	if !hasRuleFindings(callChainRules(s, c), ruleCCPathConnected) {
		t.Fatalf("an acceptEvent entry rooted at a SYNC call must fire CC-PATH-CONNECTED (the root must be queued into a Manager)")
	}
}

// ccAcceptEventFixture builds an edge-less-acceptEvent-entry diagram whose first call
// into the Manager is made under mode.
func ccAcceptEventFixture(mode string) (System, CoreUseCases) {
	nodes := []ActivityNode{
		{ID: "msg", Kind: kindAcceptEvent, Label: "order placed"},
		{ID: "act", Kind: nodeAction, Label: "record"},
		{ID: "e", Kind: nodeEnd},
	}
	edges := []ActivityEdge{{From: "msg", To: "act"}, {From: "act", To: "e"}}
	s := sysWith(ccView(
		CallStep{ActivityNodeID: "msg", Calls: []Relationship{{From: "web-client", To: "mgr", Mode: mode}}},
		CallStep{ActivityNodeID: "act", Calls: []Relationship{{From: "mgr", To: "ra", Mode: modeSync}}},
	))
	return s, ucWith(triggerBusMessage, nodes, edges)
}

// ---- CC-VIEW-USECASE ----

// TestCC_ViewUseCaseUnresolvableFires: a view whose useCaseId names no use case used to
// silently disable all nine rules with ZERO findings from anywhere — USECASE-DYNAMIC-
// MISSING only checks the use-case→view direction, and DV-KEY-UNIQUE only catches an
// EMPTY id. The dangling join key is now reported.
func TestCC_ViewUseCaseUnresolvableFires(t *testing.T) {
	dv := ccView(CallStep{ActivityNodeID: "act", Calls: []Relationship{ccActorEntry()}})
	dv.UseCaseID = "uc-ccc" // typo'd — no such use case
	s := sysWith(dv)
	c := ucWith(triggerClientAction, ccLinearNodes(), ccLinearEdges(), ccUser())
	if !hasRuleFindings(callChainRules(s, c), ruleCCViewUseCase) {
		t.Fatalf("a view referencing an unresolvable useCaseId must fire CC-VIEW-USECASE")
	}
}

// TestCC_ViewUseCaseNilActivityStaysDelegated pins the OTHER half of the old combined
// guard: a RESOLVABLE use case that simply carries no activity diagram is
// UC-ACT-PRESENT's finding to make, not this family's — CC stays silent on it.
func TestCC_ViewUseCaseNilActivityStaysDelegated(t *testing.T) {
	s := sysWith(ccView(CallStep{ActivityNodeID: "act", Calls: []Relationship{ccActorEntry()}}))
	c := ucWith(triggerClientAction, nil, nil, ccUser())
	c.Decisions[0].UseCase.Activity = nil
	if out := callChainRules(s, c); len(out) != 0 {
		t.Fatalf("a use case with no activity diagram is UC-ACT-PRESENT's to report; CC must stay silent, got %+v", out)
	}
}

// ---- CC-ENDPOINT-RESOLVES: the ambiguity branch ----

// TestCC_EndpointResolvesAmbiguousFires: an id naming BOTH a Component and an actor of
// the owning use case is ambiguous. Asserted as EXACTLY ONE finding even though two
// calls name the id — resolution is reported once per distinct id per view.
func TestCC_EndpointResolvesAmbiguousFires(t *testing.T) {
	s := sysWith(ccView(CallStep{ActivityNodeID: "act", Calls: []Relationship{
		{From: "web-client", To: "mgr", Mode: modeSync},
		{From: "mgr", To: "web-client", Mode: modeSync},
	}}))
	// The actor id deliberately collides with the Client component's id.
	c := ucWith(triggerClientAction, ccLinearNodes(), ccLinearEdges(), Actor{ID: "web-client", Role: "Web User"})
	n := 0
	for _, f := range callChainRules(s, c) {
		if f.RuleID == ruleCCEndpoint {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("an id resolving to both a Component and an actor must produce exactly one CC-ENDPOINT-RESOLVES finding, got %d", n)
	}
}

// ---- CC-TRIGGER-EVENT: the busMessage branch ----

func TestCC_TriggerEventBusMessageWithoutAcceptEventFires(t *testing.T) {
	s := sysWith(ccView(CallStep{ActivityNodeID: "act", Calls: []Relationship{
		{From: "web-client", To: "mgr", Mode: modeSync},
	}}))
	c := ucWith(triggerBusMessage, ccLinearNodes(), ccLinearEdges())
	if !hasRuleFindings(callChainRules(s, c), ruleCCTriggerEvent) {
		t.Fatalf("a busMessage-triggered use case whose diagram carries no acceptEvent entry must fire CC-TRIGGER-EVENT")
	}
}

// ---- PoC severity ----

// TestCC_AllRulesAreWarningSeverityInPoC pins ccGateSeverity: the whole family is
// ADVISORY for the PoC (the post-QA rollout flips it to SeverityError), so a firing
// CC rule must never fail the verdict.
func TestCC_AllRulesAreWarningSeverityInPoC(t *testing.T) {
	s := sysWith(ccView(CallStep{ActivityNodeID: "ghost-node", Calls: []Relationship{ccActorEntry()}}))
	c := ucWith(triggerClientAction, ccLinearNodes(), ccLinearEdges(), ccUser())
	sev, ok := findingSeverity(callChainRules(s, c), ruleCCStepNode)
	if !ok {
		t.Fatalf("expected a CC-STEP-NODE finding to assert its severity against")
	}
	if sev != ccGateSeverity || ccGateSeverity != SeverityWarning {
		t.Fatalf("every CC-* rule must be advisory (SeverityWarning) in the PoC, got %v", sev)
	}
}
