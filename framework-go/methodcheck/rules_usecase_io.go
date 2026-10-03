package methodcheck

import (
	"fmt"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
)

// rules_usecase_io.go is the use-case I/O family (deterministic-component-testing
// design §2.2, §5.2): the three node kinds added so authors state I/O intent
// directly must be well formed (UC-IO-KINDS), and a use case whose diagram yields
// no external input stimulus can never have a scenario bound (UC-NO-IO — a Warning,
// since it describes the design rather than a binding being written). UC-START-
// REDUNDANT (Amendment A3) reports a start family the scenario derivation dropped
// because the diagram's event entries already drive every stimulus it carries.

// Rule ids of the use-case I/O family.
const (
	ruleUCIOKinds        RuleID = "UC-IO-KINDS"
	ruleUCNoIO           RuleID = "UC-NO-IO"
	ruleUCStartRedundant RuleID = "UC-START-REDUNDANT"
)

// useCaseIORules runs the family over every use case that carries an activity
// diagram. actors is the union of actor ids across the set — the same set the
// scenario derivation reads, so "names an actor" here agrees with what the
// derivation classifies as an input or an output.
func useCaseIORules(c CoreUseCases, actors map[string]bool) []Finding {
	var out []Finding
	for i, d := range c.Decisions {
		if d.UseCase.Activity == nil {
			continue
		}
		out = append(out, useCaseIOFindings(d.UseCase, actors, loc(i, "useCases["+d.UseCase.ID+"].activity"))...)
	}
	return out
}

// useCaseIOFindings runs both rules over one diagram.
func useCaseIOFindings(uc UseCase, actors map[string]bool, l *Location) []Finding {
	ids := make(map[string]bool, len(uc.Activity.Nodes))
	for _, n := range uc.Activity.Nodes {
		ids[n.ID] = true
	}
	var out []Finding
	inputs := 0
	for _, n := range uc.Activity.Nodes {
		out = append(out, ioKindFindings(n, ids, actors, l)...)
		if yieldsInput(n, actors) {
			inputs++
		}
	}
	if inputs == 0 {
		out = append(out, Finding{RuleID: ruleUCNoIO, Severity: SeverityWarning, Message: "use case yields no external input stimulus; no scenario can be bound", Location: l})
	}
	return out
}

// ioKindFindings is UC-IO-KINDS for one node: a sendSignal names a receiving actor,
// an objectNode carries inState, an anchored note anchors a node of its own diagram.
//
// EARMARK (spec §2.2 drift): the spec also forbids an objectNode or note carrying a
// control-flow out-edge other than to the next node. That constraint is not checked
// here; it belongs to a follow-up over the diagram's edge list.
func ioKindFindings(n ActivityNode, ids, actors map[string]bool, l *Location) []Finding {
	var msg string
	switch n.Kind {
	case kindSendSignal:
		if n.LinkedActorID == "" || !actors[n.LinkedActorID] {
			msg = fmt.Sprintf("sendSignal %q must name a receiving actor", n.ID)
		}
	case kindObjectNode:
		if n.InState == "" {
			msg = fmt.Sprintf("objectNode %q must carry inState", n.ID)
		}
	case kindNote:
		if n.Anchor != "" && !ids[n.Anchor] {
			msg = fmt.Sprintf("note %q anchors unknown node %q", n.ID, n.Anchor)
		}
	}
	if msg == "" {
		return nil
	}
	return []Finding{{RuleID: ruleUCIOKinds, Severity: SeverityError, Message: msg, Location: l}}
}

// yieldsInput mirrors §2.2 rule 1: acceptEvent and timeEvent always; an action in an
// actor's lane; a decision/switch decided by an actor.
func yieldsInput(n ActivityNode, actors map[string]bool) bool {
	switch n.Kind {
	case kindAcceptEvent, kindTimeEvent:
		return true
	case nodeAction:
		return n.LinkedActorID != "" && actors[n.LinkedActorID]
	case nodeDecision, nodeSwitch:
		return n.DecidedBy != "" && actors[n.DecidedBy]
	}
	return false
}

// startRedundantFindings is UC-START-REDUNDANT: one Warning per use case whose
// start-family paths scenario.DeriveReport dropped (Amendment A3). It reads the
// same derivation TP-* and testgen read, so the paths it names as dropped are
// exactly the scenarios no plan can bind. A Warning: it describes the design
// (the start node duplicates an event entry), never a binding being written.
func startRedundantFindings(p Project, dropped []scenario.StartRedundancy) ([]Finding, error) {
	if len(dropped) == 0 {
		return nil, nil
	}
	cu, _, err := p.coreUseCases()
	if err != nil {
		return nil, err
	}
	ordinal := make(map[string]int, len(cu.Decisions))
	for i, d := range cu.Decisions {
		ordinal[d.UseCase.ID] = i
	}
	out := make([]Finding, 0, len(dropped))
	for _, sr := range dropped {
		out = append(out, Finding{
			RuleID:   ruleUCStartRedundant,
			Severity: SeverityWarning,
			Message: fmt.Sprintf("use case %s: %d path(s) from the start node carry no stimulus an event entry does not already drive; "+
				"they are dropped from the scenario set (remove the start node, or give it a stimulus of its own)", sr.UseCase, sr.Dropped),
			Location: loc(ordinal[sr.UseCase], "useCases["+sr.UseCase+"].activity"),
		})
	}
	return out, nil
}
