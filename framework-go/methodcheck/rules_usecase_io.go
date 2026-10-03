package methodcheck

import "fmt"

// rules_usecase_io.go is the use-case I/O family (deterministic-component-testing
// design §2.2, §5.2): the three node kinds added so authors state I/O intent
// directly must be well formed (UC-IO-KINDS), and a use case whose diagram yields
// no external input stimulus can never have a scenario bound (UC-NO-IO — a Warning,
// since it describes the design rather than a binding being written).

// Rule ids of the use-case I/O family.
const (
	ruleUCIOKinds RuleID = "UC-IO-KINDS"
	ruleUCNoIO    RuleID = "UC-NO-IO"
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
