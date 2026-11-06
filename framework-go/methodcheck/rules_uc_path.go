package methodcheck

import (
	"fmt"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
)

// rules_uc_path.go is the use-case PATH family (first-pass-correctness W3): the rules
// that hold an activity diagram to stating what the ACTOR sees, because a scenario
// whose path ends in no observable outcome yields a test that asserts nothing the
// actor can check (RUN-021: a reword whose abandon path was a note).
//
//	UC-PATH-OUTCOME             every path with an actor-classified node carries a
//	                            sendSignal or objectNode after the first of them
//	UC-OUTCOME-AT-ASKING-SURFACE the Client the asking actor used shows the outcome:
//	                            a sendSignal to that actor drawn on that surface, or an
//	                            objectNode the system returned to that Client (a call
//	                            INTO it on the path after the asking call) or drew in
//	                            that Client's or the actor's lane
//	UC-START-NOT-ACTION         an actor's act is an action node, not a labelled start
//	SCN-UNADDRESSED /
//	SCN-ACTOR-PATH-CLIENT       the scenario derivation's own Problems, as findings
//
// The plan states the objectNode clause as "an objectNode whose step has a call
// To = that Client". CC-COVERAGE forbids steps on objectNodes, so it is read as a call
// INTO that Client on the path between the asking call and the objectNode: the system
// returns the state to the surface that asked. The asking call itself (the stimulus
// node's own actor->Client step) is excluded, otherwise every objectNode after the
// first actor input would satisfy the rule and the RUN-021 defect (change sent to
// other surfaces, never back to the asker) could not fire.
//
// The path rules read scenario.DeriveReport so the ids they name (UseCase-P<n>) are
// the ids a test plan binds. validateArchitecture has no serviceContracts, so the
// derivation input is built from the System alone: every component stands in as a
// contracted component (Client when its kind is client). Stimuli exist per dynamic-
// view call into a component, which is all these rules read; where a design leaves a
// component without a contract, TP-CLIENT-CONTRACTED and SCN-UNADDRESSED report it.

const (
	ruleUCPathOutcome            RuleID = "UC-PATH-OUTCOME"
	ruleUCOutcomeAtAskingSurface RuleID = "UC-OUTCOME-AT-ASKING-SURFACE"
	ruleUCStartNotAction         RuleID = "UC-START-NOT-ACTION"
	ruleSCNUnaddressed           RuleID = "SCN-UNADDRESSED"
	ruleSCNActorPathClient       RuleID = "SCN-ACTOR-PATH-CLIENT"
)

// ucPathFindings runs UC-PATH-OUTCOME and UC-OUTCOME-AT-ASKING-SURFACE over every
// derived scenario of every use case that carries an activity diagram.
func ucPathFindings(c CoreUseCases, s System) []Finding {
	rep, err := scenario.DeriveReport(pathScenarioInput(c, s))
	if err != nil {
		// Not every derivation error is a UC-ACTDIAG finding, so name it here rather
		// than let the path rules go silent for every use case.
		return []Finding{{
			RuleID: ruleUCPathOutcome, Severity: SeverityError, Location: loc(0, "core use cases"),
			Message: fmt.Sprintf("the scenario derivation failed, so no use-case path could be checked for an outcome: %v", err),
		}}
	}
	ucs := useCaseIndex(c)
	clients := clientIDs(s)
	var out []Finding
	for _, sc := range rep.Scenarios {
		uc, ok := ucs[sc.UseCase]
		if !ok || uc.Activity == nil {
			continue
		}
		p := newPathWalk(uc, s, sc, clients)
		out = append(out, p.pathOutcome()...)
		out = append(out, p.outcomeAtAskingSurface()...)
	}
	return out
}

// pathScenarioInput is the derivation input for c and s with every component
// standing in as a contracted component.
func pathScenarioInput(c CoreUseCases, s System) scenario.Input {
	in := scenario.Input{Actors: allActorIDs(c), Contracts: map[string]scenario.Contract{}}
	addUseCases(&in, c)
	for _, v := range s.DynamicViews {
		in.Views = append(in.Views, viewOf(v))
	}
	for _, comp := range s.Components {
		in.Contracts[comp.ID] = scenario.Contract{Component: comp.ID, Client: comp.Kind == kindClient}
	}
	return in
}

// clientIDs is the set of normalized ids of the System's Client components.
func clientIDs(s System) map[string]bool {
	ids := map[string]bool{}
	for _, comp := range s.Components {
		if comp.Kind == kindClient {
			ids[scenario.Normalize(comp.ID)] = true
		}
	}
	return ids
}

// pathWalk is one derived scenario read against its use case.
type pathWalk struct {
	uc      UseCase
	sc      scenario.Scenario
	actors  map[string]bool
	clients map[string]bool
	comps   map[string]bool // normalized ids of every component
	nodes   map[string]ActivityNode
	steps   map[string]CallStep
	at      *Location
}

func newPathWalk(uc UseCase, s System, sc scenario.Scenario, clients map[string]bool) pathWalk {
	p := pathWalk{
		uc: uc, sc: sc, actors: actorIDs(uc), clients: clients, comps: map[string]bool{},
		nodes: map[string]ActivityNode{}, steps: map[string]CallStep{},
		at: loc(0, "useCase "+uc.ID+" scenario "+sc.ID),
	}
	for _, comp := range s.Components {
		p.comps[scenario.Normalize(comp.ID)] = true
	}
	for _, n := range uc.Activity.Nodes {
		p.nodes[n.ID] = n
	}
	// The derivation reads the FIRST dynamic view of a use case (scenario.viewFor);
	// the outcome is judged against that same view's steps, never another view's.
	for _, dv := range s.DynamicViews {
		if dv.UseCaseID != uc.ID {
			continue
		}
		for _, st := range dv.Steps {
			p.steps[st.ActivityNodeID] = st
		}
		break
	}
	return p
}

// isActorNode: the node is performed or decided by an actor of the use case. The
// outcome kinds, notes and the structural kinds are never an actor's act, even when
// lane-linked; scenario.classify treats them the same way (a start is structural).
func (p pathWalk) isActorNode(n ActivityNode) bool {
	switch n.Kind {
	case kindSendSignal, kindObjectNode, kindNote, nodeStart, nodeEnd, nodeMerge, nodeFork, nodeJoin:
		return false
	}
	return (n.LinkedActorID != "" && p.actors[n.LinkedActorID]) || (n.DecidedBy != "" && p.actors[n.DecidedBy])
}

// firstActor is the index in the path of the first actor-classified node.
func (p pathWalk) firstActor() (int, bool) {
	for i, id := range p.sc.Path {
		if p.isActorNode(p.nodes[id]) {
			return i, true
		}
	}
	return 0, false
}

// pathOutcome is UC-PATH-OUTCOME.
func (p pathWalk) pathOutcome() []Finding {
	first, ok := p.firstActor()
	if !ok {
		return nil
	}
	for _, id := range p.sc.Path[first+1:] {
		if k := p.nodes[id].Kind; k == kindSendSignal || k == kindObjectNode {
			return nil
		}
	}
	return []Finding{{
		RuleID: ruleUCPathOutcome, Severity: SeverityError, Location: p.at,
		Message: fmt.Sprintf("scenario %s of use case %s has an actor's input at node %q but no sendSignal or objectNode after it; "+
			"state what the actor sees when this path ends (a sendSignal to the actor, or an objectNode in its state), not only a note",
			p.sc.ID, p.uc.ID, p.sc.Path[first]),
	}}
}

// outcomeAtAskingSurface is UC-OUTCOME-AT-ASKING-SURFACE: for each Client the first
// actor input of the path enters, the outcome must show on that Client.
func (p pathWalk) outcomeAtAskingSurface() []Finding {
	var node string
	var out []Finding
	for _, st := range p.sc.Stimuli {
		if st.Input.Kind == "call" || !p.actors[st.Input.From] || !p.clients[scenario.Normalize(st.Input.To)] {
			continue
		}
		if node == "" {
			node = st.Node
		}
		if st.Node != node {
			break
		}
		if p.outcomeShownTo(st.Input.From, st.Input.To, st.Node) {
			continue
		}
		out = append(out, Finding{
			RuleID: ruleUCOutcomeAtAskingSurface, Severity: SeverityError, Location: p.at,
			Message: fmt.Sprintf("scenario %s of use case %s: actor %s acts on client %s at node %q but no sendSignal to that actor, "+
				"and no objectNode after the call into that client, shows the outcome there; the surface the actor used must show what happened",
				p.sc.ID, p.uc.ID, st.Input.From, st.Input.To, st.Node),
		})
	}
	return out
}

// outcomeShownTo reports whether the path, after the asking node, carries a sendSignal
// to the actor on the asking surface, or an objectNode the system returned to the
// asking client (see outcomeAt). The asking node's own step is the actor->client call
// and never counts as the system returning anything to the client.
func (p pathWalk) outcomeShownTo(actor, client, node string) bool {
	started, engaged := false, false
	for _, id := range p.sc.Path {
		if !started {
			started = id == node
			continue
		}
		if st, ok := p.steps[id]; ok && stepCallsInto(st, client) {
			engaged = true
		}
		if p.outcomeAt(p.nodes[id], engaged, actor, client) {
			return true
		}
	}
	return false
}

// outcomeAt reports whether n shows the outcome to the asking actor on the asking
// Client: a sendSignal to the actor that is not drawn in another component's lane
// (cross-surface propagation is additional, never a substitute), or an objectNode
// that is either reached once a call into the Client followed the asking call
// (engaged: the state was returned to that surface) or drawn in the Client's or the
// actor's own lane (the state is drawn on that surface).
func (p pathWalk) outcomeAt(n ActivityNode, engaged bool, actor, client string) bool {
	switch n.Kind {
	case kindSendSignal:
		return n.LinkedActorID == actor && p.laneIsAskingSurface(n.RoleName, actor, client)
	case kindObjectNode:
		return engaged || p.laneIsSurface(n.RoleName, actor, client)
	}
	return false
}

// laneIsSurface reports whether the lane roleName is the asking Client's or the
// actor's own; an empty lane is neither.
func (p pathWalk) laneIsSurface(roleName, actor, client string) bool {
	if roleName == "" {
		return false
	}
	lane := scenario.Normalize(roleName)
	return lane == scenario.Normalize(client) || lane == scenario.Normalize(p.actorRole(actor))
}

// laneIsAskingSurface reports whether a signal drawn in the lane roleName reaches the
// asking surface: an unlaned signal, one in the asking Client's lane, one in the
// actor's own lane, or one in a lane that names no component at all. A lane that
// resolves under scenario.Normalize to a different component is another surface.
func (p pathWalk) laneIsAskingSurface(roleName, actor, client string) bool {
	if roleName == "" {
		return true
	}
	lane := scenario.Normalize(roleName)
	if lane == scenario.Normalize(client) || lane == scenario.Normalize(p.actorRole(actor)) {
		return true
	}
	return !p.comps[lane]
}

// actorRole is the role name of the use case's actor id, or "" when it has none.
func (p pathWalk) actorRole(actor string) string {
	for _, a := range p.uc.Actors {
		if a.ID == actor {
			return a.Role
		}
	}
	return ""
}

// stepCallsInto reports whether any call of st targets id under scenario.Normalize.
func stepCallsInto(st CallStep, id string) bool {
	want := scenario.Normalize(id)
	for _, call := range st.Calls {
		if scenario.Normalize(call.To) == want {
			return true
		}
	}
	return false
}

// startNotActionFindings is UC-START-NOT-ACTION: a start node is a control-flow
// entry, so a labelled one in an actor's lane is an actor action authored as a
// start, which the derivation then cannot see as a stimulus.
func startNotActionFindings(c CoreUseCases) []Finding {
	var out []Finding
	for i, d := range c.Decisions {
		uc := d.UseCase
		if uc.Activity == nil {
			continue
		}
		actors, roles := actorLane(uc)
		for _, n := range uc.Activity.Nodes {
			if !isLabelledActorStart(n, actors, roles) {
				continue
			}
			out = append(out, Finding{
				RuleID: ruleUCStartNotAction, Severity: SeverityError,
				Location: loc(i, "useCases["+uc.ID+"].activity"),
				Message: fmt.Sprintf("start node %q of use case %s is in an actor's lane and carries the label %q; "+
					"an actor's act is an action node (leave the start unlabelled and give the act its own action)", n.ID, uc.ID, n.Label),
			})
		}
	}
	return out
}

// actorLane is the actor ids and actor role names of uc: the two ways a node can sit
// in an actor's lane.
func actorLane(uc UseCase) (actors, roles map[string]bool) {
	actors = actorIDs(uc)
	roles = map[string]bool{}
	for _, a := range uc.Actors {
		roles[a.Role] = true
	}
	return actors, roles
}

// isLabelledActorStart reports whether n is a start node that carries a label and sits
// in an actor's lane, by linked actor id or, when unlinked, by role name.
func isLabelledActorStart(n ActivityNode, actors, roles map[string]bool) bool {
	if n.Kind != nodeStart || n.Label == "" {
		return false
	}
	if n.LinkedActorID != "" {
		return actors[n.LinkedActorID]
	}
	return n.RoleName != "" && roles[n.RoleName]
}

// scenarioProblemFindings mirrors the derivation's own Problems as findings, so a
// stimulus or path the derivation could not address fails the gate instead of being
// dropped.
func scenarioProblemFindings(r scenario.Report) []Finding {
	out := make([]Finding, 0, len(r.Problems))
	for _, pr := range r.Problems {
		out = append(out, Finding{
			RuleID: problemRule(pr.Code), Severity: SeverityError,
			Location: loc(0, "useCase "+pr.UseCase+" scenario "+pr.Scenario),
			Message:  problemMessage(pr),
		})
	}
	return out
}

// problemRule is the rule a derivation Problem code is reported under. A code this
// gate does not know is reported under its own code, never under another rule.
func problemRule(code string) RuleID {
	switch code {
	case scenario.ProblemUnaddressed:
		return ruleSCNUnaddressed
	case scenario.ProblemActorPathClient:
		return ruleSCNActorPathClient
	}
	return RuleID(code)
}

// problemMessage is the finding text of pr; an unrecognized code names itself so the
// gap is visible.
func problemMessage(pr scenario.Problem) string {
	switch pr.Code {
	case scenario.ProblemUnaddressed, scenario.ProblemActorPathClient:
		return fmt.Sprintf("scenario %s: %s", pr.Scenario, pr.Message)
	}
	return fmt.Sprintf("scenario %s: unrecognized scenario problem code %q (methodcheck has no rule for it): %s", pr.Scenario, pr.Code, pr.Message)
}
