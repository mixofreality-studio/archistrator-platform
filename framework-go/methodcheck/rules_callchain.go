package methodcheck

import "fmt"

// rules_callchain.go is the CC-* CALL-CHAIN CORRESPONDENCE family (2026-07-30
// callchain-realization): the machine check that every use case's step-keyed
// DynamicView realization CORRESPONDS to that use case's activity diagram.
//
// Before this family, the two halves of a use case's design were only loosely
// coupled: the activity diagram (Grammar B — what the use case DOES, step by step)
// and the dynamic view (Grammar A's call chain — which components make which calls).
// Nothing checked that the second realized the first. CallStep.ActivityNodeID is the
// join key that makes the correspondence checkable, and these nine rules are what
// check it:
//
//	CC-STEP-NODE       every step keys a node the diagram declares
//	CC-STEP-UNIQUE     at most one step per activity node
//	CC-COVERAGE        every step-REQUIRING node is realized, and every step keys a
//	                   step-ELIGIBLE node
//	CC-STEP-NONEMPTY   a realized step makes at least one call
//	CC-ENDPOINT-RESOLVES  every call endpoint resolves to exactly one of {Component, Actor}
//	CC-ACTOR-EDGE      an actor may only interact, synchronously, with a Client
//	CC-ACTOR-LANE      a lane-linked node's step must touch that actor
//	CC-TRIGGER-EVENT   the use-case trigger and the diagram's entry nodes agree
//	CC-PATH-CONNECTED  every activity-diagram PATH is realized as a connected chain
//
// SUBSUMPTION: this family retires three rules whose job it now does strictly better —
// DV-PART-EXIST (subsumed by CC-ENDPOINT-RESOLVES, which additionally resolves actors),
// DV-PART-USED (mathematically dead once participants are derived from calls), and
// DV-CHAIN-CONNECTED (subsumed by CC-PATH-CONNECTED, which walks the diagram's paths in
// order rather than flattening a view's calls into one reachability question).
//
// ACTORS: an endpoint id is resolved against the component index UNION the OWNING use
// case's Actors. Actors are per-use-case, so the same id may name an actor in one use
// case and nothing in another — resolution is always relative to the view's use case.

const (
	ruleCCViewUseCase   RuleID = "CC-VIEW-USECASE"
	ruleCCStepNode      RuleID = "CC-STEP-NODE"
	ruleCCStepUnique    RuleID = "CC-STEP-UNIQUE"
	ruleCCCoverage      RuleID = "CC-COVERAGE"
	ruleCCStepNonempty  RuleID = "CC-STEP-NONEMPTY"
	ruleCCEndpoint      RuleID = "CC-ENDPOINT-RESOLVES"
	ruleCCActorEdge     RuleID = "CC-ACTOR-EDGE"
	ruleCCActorLane     RuleID = "CC-ACTOR-LANE"
	ruleCCTriggerEvent  RuleID = "CC-TRIGGER-EVENT"
	ruleCCPathConnected RuleID = "CC-PATH-CONNECTED"
)

// ccGateSeverity is the PoC-advisory severity for the correspondence family and
// the two coverage retargets. The post-QA rollout flips it to SeverityError.
const ccGateSeverity = SeverityWarning

// ccMustHaveStep is the set of activity-node kinds that MUST carry a realizing step:
// they are the nodes that DO something, so a call chain has to say what calls they make.
var ccMustHaveStep = map[string]bool{
	nodeAction:      true,
	kindTimeEvent:   true,
	kindAcceptEvent: true,
}

// ccMayHaveStep is the set of node kinds a step is ALLOWED but not required to key: a
// decision/switch may itself make a call (asking an Engine for the verdict it branches
// on) or may branch purely on already-held state. Every other kind (start/end/merge/
// join/fork/swimLane/note/loop/goto/interruptEdge) is pure control flow and must NOT
// carry a step.
var ccMayHaveStep = map[string]bool{
	nodeDecision: true,
	nodeSwitch:   true,
}

// callChainRules validates every use case's realization against its activity
// diagram: coverage, endpoint resolution, actor legality, trigger alignment,
// and per-path chain connectivity (spec §4).
func callChainRules(s System, c CoreUseCases) []Finding {
	idx := componentIndex(s)
	ucByID := useCaseIndex(c)
	var out []Finding
	for i, dv := range s.DynamicViews {
		uc, ok := ucByID[dv.UseCaseID]
		if !ok {
			// CC-VIEW-USECASE. A view whose UseCaseID resolves to NOTHING silently
			// disables all nine correspondence rules for it, and no other rule notices:
			// USECASE-DYNAMIC-MISSING only checks the use-case→view direction, and
			// DV-KEY-UNIQUE only catches an EMPTY UseCaseID (on which it co-fires with
			// this rule, from its own angle). Report the dangling join key rather than
			// no-op'ing the whole family.
			out = append(out, ccFinding(ruleCCViewUseCase, loc(i+1, "dynamicView "+viewLabel(dv)),
				"dynamic view %q references useCaseId %q, which resolves to no use case in the committed set; the call chain cannot be checked against any activity diagram until the join key is fixed",
				viewLabel(dv), dv.UseCaseID))
			continue
		}
		// A use case with no activity diagram has nothing to correspond TO — that gap is
		// UC-ACT-PRESENT's to report, not this family's to re-report.
		if uc.Activity == nil {
			continue
		}
		out = append(out, newCCContext(dv, uc, idx, i+1).findings()...)
	}
	return out
}

// useCaseIndex maps use-case id → UseCase across the whole committed decision set
// (core AND nonCore variations — every one of them owns a dynamic view).
func useCaseIndex(c CoreUseCases) map[string]UseCase {
	idx := make(map[string]UseCase, len(c.Decisions))
	for _, d := range c.Decisions {
		idx[d.UseCase.ID] = d.UseCase
	}
	return idx
}

// actorIDs returns the id set of a use case's declared actors.
func actorIDs(uc UseCase) map[string]bool {
	ids := make(map[string]bool, len(uc.Actors))
	for _, a := range uc.Actors {
		ids[a.ID] = true
	}
	return ids
}

// ccContext is one dynamic view's evaluation context: the view, its owning use case,
// and the indices every CC rule joins on. Bundling them keeps each rule a method with
// no parameter train.
type ccContext struct {
	dv       DynamicView
	uc       UseCase
	idx      map[string]Component    // component id → Component
	actors   map[string]bool         // actor id (of THIS use case) → true
	nodes    map[string]ActivityNode // activity node id → node
	steps    map[string]CallStep     // activity node id → the step realizing it (first wins)
	incoming map[string]int          // activity node id → count of incoming edges
	ordinal  int                     // the dynamic view's 1-based position (finding order key)
}

func newCCContext(dv DynamicView, uc UseCase, idx map[string]Component, ordinal int) ccContext {
	nodes := make(map[string]ActivityNode, len(uc.Activity.Nodes))
	for _, n := range uc.Activity.Nodes {
		nodes[n.ID] = n
	}
	steps := make(map[string]CallStep, len(dv.Steps))
	for _, st := range dv.Steps {
		if _, dup := steps[st.ActivityNodeID]; !dup {
			steps[st.ActivityNodeID] = st
		}
	}
	incoming := make(map[string]int, len(uc.Activity.Nodes))
	for _, e := range uc.Activity.Edges {
		incoming[e.To]++
	}
	return ccContext{
		dv: dv, uc: uc, idx: idx, actors: actorIDs(uc),
		nodes: nodes, steps: steps, incoming: incoming, ordinal: ordinal,
	}
}

// findings runs the whole family over one view, in stable rule order.
func (cc ccContext) findings() []Finding {
	var out []Finding
	out = append(out, cc.stepIdentity()...)
	out = append(out, cc.coverage()...)
	out = append(out, cc.stepNonempty()...)
	out = append(out, cc.endpointResolves()...)
	out = append(out, cc.actorEdges()...)
	out = append(out, cc.actorLane()...)
	out = append(out, cc.triggerEvent()...)
	out = append(out, cc.pathConnected()...)
	return out
}

// stepLoc is the STEP-SCOPED location grammar. The app UI joins its per-step finding
// badges on this exact Section string — do not reshape it without the app side.
func (cc ccContext) stepLoc(nodeID string) *Location {
	return loc(cc.ordinal, "dynamicView "+viewLabel(cc.dv)+" step "+nodeID)
}

// ucLoc is the USE-CASE-SCOPED location grammar (coverage + trigger findings, which
// are statements about the use case as a whole, not about any one step).
func (cc ccContext) ucLoc() *Location {
	return loc(cc.ordinal, "useCase "+cc.uc.ID)
}

// ccFinding builds a finding at the family's shared PoC severity.
func ccFinding(id RuleID, l *Location, format string, args ...any) Finding {
	return Finding{RuleID: id, Severity: ccGateSeverity, Message: fmt.Sprintf(format, args...), Location: l}
}

// ---- CC-STEP-NODE / CC-STEP-UNIQUE ----

// stepIdentity checks that each step names a REAL activity node (CC-STEP-NODE) and
// that no node is realized twice (CC-STEP-UNIQUE). A step with an empty
// ActivityNodeID is a dangling step: the join key is what makes a step meaningful.
func (cc ccContext) stepIdentity() []Finding {
	var out []Finding
	seen := make(map[string]bool, len(cc.dv.Steps))
	for _, st := range cc.dv.Steps {
		if _, ok := cc.nodes[st.ActivityNodeID]; !ok {
			out = append(out, ccFinding(ruleCCStepNode, cc.stepLoc(st.ActivityNodeID),
				"dynamic view %q has a step keyed on %q, which is not a node of use case %s's activity diagram; every step must realize a declared activity node",
				viewLabel(cc.dv), st.ActivityNodeID, cc.uc.ID))
		}
		if seen[st.ActivityNodeID] {
			out = append(out, ccFinding(ruleCCStepUnique, cc.stepLoc(st.ActivityNodeID),
				"dynamic view %q realizes activity node %q with more than one step; a node's calls belong to exactly one step",
				viewLabel(cc.dv), st.ActivityNodeID))
		}
		seen[st.ActivityNodeID] = true
	}
	return out
}

// ---- CC-COVERAGE ----

// coverage is the BIDIRECTIONAL step/node correspondence: every node that must be
// realized IS (diagram → view), and every step keys a node that may legally carry one
// (view → diagram). Dangling steps are CC-STEP-NODE's business and are skipped here.
func (cc ccContext) coverage() []Finding {
	var out []Finding
	for _, n := range cc.uc.Activity.Nodes {
		if !ccMustHaveStep[n.Kind] {
			continue
		}
		if _, ok := cc.steps[n.ID]; ok {
			continue
		}
		out = append(out, ccFinding(ruleCCCoverage, cc.ucLoc(),
			"activity node %q (%s) of use case %s is realized by no step of dynamic view %q; every action/timeEvent/acceptEvent node must say which calls it makes",
			n.ID, n.Kind, cc.uc.ID, viewLabel(cc.dv)))
	}
	for _, st := range cc.dv.Steps {
		n, ok := cc.nodes[st.ActivityNodeID]
		if !ok || ccMustHaveStep[n.Kind] || ccMayHaveStep[n.Kind] {
			continue
		}
		out = append(out, ccFinding(ruleCCCoverage, cc.ucLoc(),
			"dynamic view %q attaches a step to %s node %q; only action/timeEvent/acceptEvent nodes carry calls (decision/switch may), every other node is pure control flow",
			viewLabel(cc.dv), n.Kind, n.ID))
	}
	return out
}

// ---- CC-STEP-NONEMPTY ----

// stepNonempty: a realized step that makes no call says nothing — either the node
// makes calls (and they belong here) or it should carry no step at all.
func (cc ccContext) stepNonempty() []Finding {
	var out []Finding
	for _, st := range cc.dv.Steps {
		if len(st.Calls) > 0 {
			continue
		}
		out = append(out, ccFinding(ruleCCStepNonempty, cc.stepLoc(st.ActivityNodeID),
			"dynamic view %q step %q makes no call; a realized step must carry at least one call (drop the step if the node makes none)",
			viewLabel(cc.dv), st.ActivityNodeID))
	}
	return out
}

// ---- CC-ENDPOINT-RESOLVES ----

// endpointResolves checks that every call endpoint resolves to EXACTLY ONE of the two
// namespaces a call chain draws from: the System's components, or the owning use
// case's actors. Resolving to neither is a dangling endpoint; resolving to BOTH is
// ambiguous (the reader cannot tell whether the id denotes the person or the code).
// Reported once per distinct id per view, at the first step that names it.
func (cc ccContext) endpointResolves() []Finding {
	var out []Finding
	reported := map[string]bool{}
	for _, st := range cc.dv.Steps {
		for _, call := range st.Calls {
			out = append(out, cc.callEndpointFindings(call, st.ActivityNodeID, reported)...)
		}
	}
	return out
}

// callEndpointFindings resolves one call's two ends, skipping ids already reported for
// this view.
func (cc ccContext) callEndpointFindings(call Relationship, nodeID string, reported map[string]bool) []Finding {
	var out []Finding
	for _, id := range []string{call.From, call.To} {
		if reported[id] {
			continue
		}
		if f, bad := cc.endpointFinding(id, nodeID); bad {
			reported[id] = true
			out = append(out, f)
		}
	}
	return out
}

func (cc ccContext) endpointFinding(id, nodeID string) (Finding, bool) {
	_, isComponent := cc.idx[id]
	isActor := cc.actors[id]
	switch {
	case isComponent && isActor:
		return ccFinding(ruleCCEndpoint, cc.stepLoc(nodeID),
			"dynamic view %q names endpoint %q, which resolves to BOTH a System Component and an actor of use case %s; an endpoint id must denote exactly one of them",
			viewLabel(cc.dv), id, cc.uc.ID), true
	case !isComponent && !isActor:
		return ccFinding(ruleCCEndpoint, cc.stepLoc(nodeID),
			"dynamic view %q names endpoint %q, which is neither a System Component nor an actor of use case %s",
			viewLabel(cc.dv), id, cc.uc.ID), true
	}
	return Finding{}, false
}

// ---- CC-ACTOR-EDGE ----

// actorEdges enforces the actor-interaction grammar: a person touches the system only
// through a Client, and only synchronously. Two actors talking to each other is not a
// system call at all.
func (cc ccContext) actorEdges() []Finding {
	var out []Finding
	for _, st := range cc.dv.Steps {
		for _, call := range st.Calls {
			out = append(out, cc.actorEdgeFindings(call, st.ActivityNodeID)...)
		}
	}
	return out
}

func (cc ccContext) actorEdgeFindings(call Relationship, nodeID string) []Finding {
	fromActor, toActor := cc.actors[call.From], cc.actors[call.To]
	if !fromActor && !toActor {
		return nil
	}
	l := cc.stepLoc(nodeID)
	if fromActor && toActor {
		return []Finding{ccFinding(ruleCCActorEdge, l,
			"dynamic view %q step %q draws actor %s → actor %s; an actor edge models a person entering the system through a Client, not two people interacting",
			viewLabel(cc.dv), nodeID, call.From, call.To)}
	}
	component := call.To
	if toActor {
		component = call.From
	}
	var out []Finding
	if c, ok := cc.idx[component]; !ok || c.Kind != kindClient {
		out = append(out, ccFinding(ruleCCActorEdge, l,
			"dynamic view %q step %q draws actor edge %s→%s, whose non-actor end %s is not a Client component; an actor enters the system only through a Client",
			viewLabel(cc.dv), nodeID, call.From, call.To, component))
	}
	if call.Mode != modeSync {
		out = append(out, ccFinding(ruleCCActorEdge, l,
			"dynamic view %q step %q draws actor edge %s→%s with mode %q; an actor interaction is always synchronous",
			viewLabel(cc.dv), nodeID, call.From, call.To, call.Mode))
	}
	return out
}

// ---- CC-ACTOR-LANE ----

// actorLane ties the diagram's swim-lane assignment to the realization: a node placed
// in an actor's lane claims that actor performs it, so the step realizing that node
// must actually touch the actor. A lane that no call honors is decoration.
func (cc ccContext) actorLane() []Finding {
	var out []Finding
	for _, n := range cc.uc.Activity.Nodes {
		if n.LinkedActorID == "" {
			continue
		}
		st, ok := cc.steps[n.ID]
		if !ok || stepTouches(st, n.LinkedActorID) {
			continue
		}
		out = append(out, ccFinding(ruleCCActorLane, cc.stepLoc(n.ID),
			"activity node %q is laned to actor %s, but dynamic view %q's step for it touches that actor in no call; realize the actor's participation or drop the lane link",
			n.ID, n.LinkedActorID, viewLabel(cc.dv)))
	}
	return out
}

// stepTouches reports whether any of a step's calls names id at either end.
func stepTouches(st CallStep, id string) bool {
	for _, call := range st.Calls {
		if call.From == id || call.To == id {
			return true
		}
	}
	return false
}

// ---- CC-TRIGGER-EVENT ----

// triggerEvent aligns the use-case trigger with the diagram's ENTRY nodes: a timer
// trigger must enter on a timeEvent, a bus-message trigger on an acceptEvent, and a
// client-action trigger on neither (a person clicking is not a UML event node). An
// "entry" here is an event node with no incoming edge — nothing leads into a trigger.
func (cc ccContext) triggerEvent() []Finding {
	hasTimeEntry, hasAcceptEntry := cc.eventEntries()
	switch cc.uc.Trigger {
	case triggerTimer:
		if !hasTimeEntry {
			return []Finding{ccFinding(ruleCCTriggerEvent, cc.ucLoc(),
				"use case %s is timer-triggered but its activity diagram declares no timeEvent entry node; a scheduled use case enters on the timer that fires it",
				cc.uc.ID)}
		}
	case triggerBusMessage:
		if !hasAcceptEntry {
			return []Finding{ccFinding(ruleCCTriggerEvent, cc.ucLoc(),
				"use case %s is busMessage-triggered but its activity diagram declares no acceptEvent entry node; a message-driven use case enters on the signal it accepts",
				cc.uc.ID)}
		}
	case triggerClientAction:
		if hasTimeEntry || hasAcceptEntry {
			return []Finding{ccFinding(ruleCCTriggerEvent, cc.ucLoc(),
				"use case %s is clientAction-triggered but its activity diagram enters on a UML event node; a client-initiated use case enters at a start node — reclassify the trigger as timer/busMessage or remove the event entry",
				cc.uc.ID)}
		}
	}
	return nil
}

// eventEntries reports whether the diagram carries a timeEvent (resp. acceptEvent)
// node with no incoming edge — the shape that makes an event node the diagram's entry.
// An event node WITH an incoming edge is a mid-flow event (a wait/receive inside the
// flow), not a trigger, and does not count.
func (cc ccContext) eventEntries() (timeEntry, acceptEntry bool) {
	for _, n := range cc.uc.Activity.Nodes {
		if cc.incoming[n.ID] > 0 {
			continue
		}
		switch n.Kind {
		case kindTimeEvent:
			timeEntry = true
		case kindAcceptEvent:
			acceptEntry = true
		}
	}
	return timeEntry, acceptEntry
}

// ---- CC-PATH-CONNECTED ----

// pathConnected is the heart of the correspondence: for EVERY entry→end path the
// activity diagram admits (activitypaths.go enumerates them), the steps realized along
// that path must compose into a CONNECTED call chain. Walking per PATH rather than
// over the view's flattened calls is what makes this strictly stronger than the retired
// DV-CHAIN-CONNECTED: a fragment that is only reachable on some OTHER branch does not
// silently justify a disconnect on this one.
//
// A call is connected when any of these holds:
//   - it is an actor→Client call — a person entering the system always re-seeds the
//     chain, at any point (mid-chain re-entry by a second actor is legal);
//   - it is the path's FIRST call and matches the entry kind's root shape (timeEvent →
//     Client→Manager, i.e. the scheduler client drives it; acceptEvent → a queued call
//     into a Manager; a start/clientAction entry → the actor→Client case above);
//   - its From is already in the reached set.
//
// Findings are deduplicated across paths by (node, from, to): one disconnect reported
// once, not once per path that happens to traverse it.
//
// EVENT-ROOTED SUFFIX PATHS ARE SKIPPED (2026-07-30 fix-round-1, narrowed in
// fix-round-2 — see ccIsSuffixPath for the precise predicate and why it must NOT be
// applied to start-rooted paths).
func (cc ccContext) pathConnected() []Finding {
	var out []Finding
	reported := map[string]bool{}
	for _, p := range activityPaths(*cc.uc.Activity) {
		if cc.ccIsSuffixPath(p.Entry) {
			continue
		}
		out = append(out, cc.walkRealizedPath(p.Entry, p.Nodes, reported)...)
	}
	return out
}

// ccIsSuffixPath reports whether an enumerated path is a mere SUFFIX of another path
// this walk already covers, and so carries no connectivity information of its own.
//
// activityPaths treats every EVENT node as an enumeration root, wherever it sits —
// deliberately, and it is pinned verbatim. For a MID-FLOW event
// (`start → a1 → ev(acceptEvent) → a2 → end`) that yields the full path AND the bare
// suffix `[ev, a2, end]`. Walking that suffix would restart with an empty reached set
// and first=true, so ev's step — perfectly connected on the full path — would be judged
// against the acceptEvent ROOT shape and fire a false positive the (node,from,to) dedup
// cannot absorb (the full path never reported the call). An event node with an incoming
// edge is exactly that case: something leads into it, so the full path already covers it.
//
// A START-rooted path is NEVER a suffix, incoming edges or not: a start node is always a
// PRIMARY entry, and an incoming edge into it is an ordinary authored shape (a guarded
// back-edge — "retry from the top"). Applying the suffix skip to start entries (the
// fix-round-1 predicate did, on incoming-count alone) made CC-PATH-CONNECTED silently
// VACUOUS for any diagram with a retry loop: a genuinely disconnected call produced zero
// findings from this rule, UC-ACTDIAG and UC-ACT-PRESENT alike. The predicate is
// therefore membership in the EVENT kinds AND a non-zero incoming count — both, never
// the count alone.
func (cc ccContext) ccIsSuffixPath(entry pathEntry) bool {
	switch entry.Kind {
	case kindTimeEvent, kindAcceptEvent:
		return cc.incoming[entry.NodeID] > 0
	default: // nodeStart — always a primary entry, always walked.
		return false
	}
}

// ccPathWalk is the mutable state carried ALONG one path: which endpoints the chain has
// reached so far, and whether the next call is still the path's first.
type ccPathWalk struct {
	reached map[string]bool
	first   bool
}

func (cc ccContext) walkRealizedPath(entry pathEntry, nodeIDs []string, reported map[string]bool) []Finding {
	w := &ccPathWalk{reached: map[string]bool{}, first: true}
	var out []Finding
	for _, nodeID := range nodeIDs {
		st, ok := cc.steps[nodeID]
		if !ok {
			continue
		}
		out = append(out, cc.walkStepCalls(st, nodeID, entry, w, reported)...)
	}
	return out
}

// walkStepCalls threads one realized step's call fragment through the walk state,
// reporting each call that is neither legally rooted nor continuing from a reached
// endpoint.
func (cc ccContext) walkStepCalls(st CallStep, nodeID string, entry pathEntry, w *ccPathWalk, reported map[string]bool) []Finding {
	var out []Finding
	for _, call := range st.Calls {
		if !cc.callConnects(call, entry, w.first, w.reached) {
			if f, fresh := cc.disconnectFinding(call, nodeID, entry, reported); fresh {
				out = append(out, f)
			}
		}
		w.reached[call.From] = true
		w.reached[call.To] = true
		w.first = false
	}
	return out
}

// disconnectFinding builds the CC-PATH-CONNECTED finding for one disconnected call,
// returning fresh=false when this (node, from, to) was already reported on an earlier
// path (the same disconnect is one finding, not one per traversing path).
func (cc ccContext) disconnectFinding(call Relationship, nodeID string, entry pathEntry, reported map[string]bool) (Finding, bool) {
	key := nodeID + "\x00" + call.From + "\x00" + call.To
	if reported[key] {
		return Finding{}, false
	}
	reported[key] = true
	return ccFinding(ruleCCPathConnected, cc.stepLoc(nodeID),
		"dynamic view %q step %q calls %s→%s, but %s is not reached by any earlier call on the activity path entered at %q, and the call is not a legal chain root (actor→Client, or the entry's own root shape)",
		viewLabel(cc.dv), nodeID, call.From, call.To, call.From, entry.NodeID), true
}

func (cc ccContext) callConnects(call Relationship, entry pathEntry, first bool, reached map[string]bool) bool {
	if cc.isActorToClient(call) {
		return true
	}
	if first {
		return cc.rootsEntry(call, entry)
	}
	return reached[call.From]
}

// isActorToClient reports the always-legal chain root: an actor of this use case
// entering a Client component.
func (cc ccContext) isActorToClient(call Relationship) bool {
	if !cc.actors[call.From] {
		return false
	}
	to, ok := cc.idx[call.To]
	return ok && to.Kind == kindClient
}

// rootsEntry reports whether a path's FIRST call is a legal root for that path's entry
// kind. A start entry (the clientAction shape) has only ONE legal root — the
// actor→Client call callConnects already accepted — so it is false here.
func (cc ccContext) rootsEntry(call Relationship, entry pathEntry) bool {
	from, fromOK := cc.idx[call.From]
	to, toOK := cc.idx[call.To]
	switch entry.Kind {
	case kindTimeEvent:
		return fromOK && toOK && from.Kind == kindClient && to.Kind == kindManager
	case kindAcceptEvent:
		return toOK && to.Kind == kindManager && call.Mode == modeQueued
	default:
		return false
	}
}
