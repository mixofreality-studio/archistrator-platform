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
//	                            a sendSignal to that actor, or an objectNode reached
//	                            after that Client was engaged
//	UC-START-NOT-ACTION         an actor's act is an action node, not a labelled start
//	SCN-UNADDRESSED /
//	SCN-ACTOR-PATH-CLIENT       the scenario derivation's own Problems, as findings
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
		return nil // a malformed diagram is UC-ACTDIAG's to report
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
	nodes   map[string]ActivityNode
	steps   map[string]CallStep
	at      *Location
}

func newPathWalk(uc UseCase, s System, sc scenario.Scenario, clients map[string]bool) pathWalk {
	p := pathWalk{
		uc: uc, sc: sc, actors: actorIDs(uc), clients: clients,
		nodes: map[string]ActivityNode{}, steps: map[string]CallStep{},
		at: loc(0, "useCase "+uc.ID+" scenario "+sc.ID),
	}
	for _, n := range uc.Activity.Nodes {
		p.nodes[n.ID] = n
	}
	for _, dv := range s.DynamicViews {
		if dv.UseCaseID != uc.ID {
			continue
		}
		for _, st := range dv.Steps {
			if _, dup := p.steps[st.ActivityNodeID]; !dup {
				p.steps[st.ActivityNodeID] = st
			}
		}
	}
	return p
}

// isActorNode: the node is performed or decided by an actor of the use case. The
// outcome kinds and notes are never an actor's act, even when lane-linked.
func (p pathWalk) isActorNode(n ActivityNode) bool {
	switch n.Kind {
	case kindSendSignal, kindObjectNode, kindNote:
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

// outcomeShownTo reports whether the path, after node, carries a sendSignal to the
// actor, or an objectNode reached after a step from node onward touched the client.
func (p pathWalk) outcomeShownTo(actor, client, node string) bool {
	engaged := false
	started := false
	for _, id := range p.sc.Path {
		if id == node {
			started = true
		}
		if !started {
			continue
		}
		n := p.nodes[id]
		if st, ok := p.steps[id]; ok && stepTouchesNormalized(st, client) {
			engaged = true
		}
		switch {
		case n.Kind == kindSendSignal && n.LinkedActorID == actor:
			return true
		case n.Kind == kindObjectNode && engaged:
			return true
		}
	}
	return false
}

// stepTouchesNormalized reports whether any call of st names id at either end under
// scenario.Normalize.
func stepTouchesNormalized(st CallStep, id string) bool {
	want := scenario.Normalize(id)
	for _, call := range st.Calls {
		if scenario.Normalize(call.From) == want || scenario.Normalize(call.To) == want {
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
		actors := actorIDs(uc)
		roles := map[string]bool{}
		for _, a := range uc.Actors {
			roles[a.Role] = true
		}
		for _, n := range uc.Activity.Nodes {
			if n.Kind != nodeStart || n.Label == "" {
				continue
			}
			if (n.LinkedActorID != "" && actors[n.LinkedActorID]) || (n.LinkedActorID == "" && n.RoleName != "" && roles[n.RoleName]) {
				out = append(out, Finding{
					RuleID: ruleUCStartNotAction, Severity: SeverityError,
					Location: loc(i, "useCases["+uc.ID+"].activity"),
					Message: fmt.Sprintf("start node %q of use case %s is in an actor's lane and carries the label %q; "+
						"an actor's act is an action node (leave the start unlabelled and give the act its own action)", n.ID, uc.ID, n.Label),
				})
			}
		}
	}
	return out
}

// scenarioProblemFindings mirrors the derivation's own Problems as findings, so a
// stimulus or path the derivation could not address fails the gate instead of being
// dropped.
func scenarioProblemFindings(r scenario.Report) []Finding {
	out := make([]Finding, 0, len(r.Problems))
	for _, pr := range r.Problems {
		id := ruleSCNUnaddressed
		if pr.Code == scenario.ProblemActorPathClient {
			id = ruleSCNActorPathClient
		}
		out = append(out, Finding{
			RuleID: id, Severity: SeverityError,
			Location: loc(0, "useCase "+pr.UseCase+" scenario "+pr.Scenario),
			Message:  fmt.Sprintf("scenario %s: %s", pr.Scenario, pr.Message),
		})
	}
	return out
}
