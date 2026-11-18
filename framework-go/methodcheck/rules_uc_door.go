package methodcheck

import (
	"fmt"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
)

// rules_uc_door.go closes the vacuous pass of the actor-classified path rules
// (RUN-024). UC-PATH-OUTCOME, UC-OUTCOME-AT-ASKING-SURFACE and UC-START-NOT-ACTION judge
// only nodes classified as an actor's (linkedActorId or decidedBy naming a declared
// actor), so a diagram that links NO node to a declared actor judged nothing and
// passed all three: RUN-024 committed 101 activity nodes with linkedActorId null,
// lanes "List owner" / "Todo list" / "Other entry points" against declared actors
// team-member and team-agent, and every flow ended in an announcement to "Other entry
// points".
//
//	UC-ACTOR-DOOR     a use case somebody initiates (trigger clientAction) has at least
//	                  one node an actor performs or decides, and every elementary path
//	                  (loops unrolled once, the derivation's paths) passes through one;
//	                  a path with no actor node has no door an actor's act enters by
//	UC-LANE-RESOLVES  a node's lane (roleName) names a declared actor of its use case
//	                  (by role or id) or a System component (by id or name); a lane
//	                  that names neither is a stranger the rules above cannot join to
//	                  anything
//
// Where they run. The path rules read the diagram alone except UC-OUTCOME-AT-ASKING-
// SURFACE, which reads the System's dynamic views. So UC-ACTOR-DOOR, UC-PATH-OUTCOME
// and UC-START-NOT-ACTION run when the coreUseCases slot is validated
// (ucSlotPathFindings), where the draft is still the architect's to fix. UC-LANE-
// RESOLVES resolves a lane against the System's components, which do not exist until
// architecture, so it runs there (ucLaneFindings); at the slot the lane is judged only
// through UC-ACTOR-DOOR, which is what makes a stranger lane unable to stand in for
// the link.
//
// Timer- and busMessage-triggered use cases are started by the clock or the bus and
// legitimately declare no actor (CUC-ACTOR-REQUIRED), so UC-ACTOR-DOOR leaves them
// alone, as it leaves a clientAction use case with no declared actor to that rule.

const (
	ruleUCActorDoor    RuleID = "UC-ACTOR-DOOR"
	ruleUCLaneResolves RuleID = "UC-LANE-RESOLVES"
)

// ucSlotPathFindings runs the path rules that need nothing but the committed core use
// cases: UC-ACTOR-DOOR and UC-PATH-OUTCOME. (UC-START-NOT-ACTION is startNotActionFindings,
// also run at the slot.) The derivation reads the diagrams alone here: no dynamic view
// exists yet, so the paths are the diagrams' own elementary paths and no stimulus is
// derived. A derivation failure is reported as a finding, once, for every path rule.
func ucSlotPathFindings(c CoreUseCases) []Finding {
	rep, err := scenario.DeriveReport(pathScenarioInput(c, System{}))
	if err != nil {
		// Not every derivation error is a UC-ACTDIAG finding, so name it here rather
		// than let the path rules go silent for every use case.
		return []Finding{{
			RuleID: ruleUCPathOutcome, Severity: SeverityError, Location: loc(0, "core use cases"),
			Message: fmt.Sprintf("the scenario derivation failed, so no use-case path could be checked for an outcome: %v", err),
		}}
	}
	var out []Finding
	for i, d := range c.Decisions {
		out = append(out, useCaseDoorFindings(i, d.UseCase)...)
	}
	ucs := useCaseIndex(c)
	for _, sc := range rep.Scenarios {
		uc, ok := ucs[sc.UseCase]
		if !ok || uc.Activity == nil {
			continue
		}
		p := newPathWalk(uc, System{}, sc, nil)
		out = append(out, p.pathOutcome()...)
		out = append(out, p.actorDoor()...)
	}
	return out
}

// needsActorDoor reports whether uc is one UC-ACTOR-DOOR judges: somebody initiates
// it (clientAction), it declares at least one actor to name in the fix, and it has a
// diagram.
func needsActorDoor(uc UseCase) bool {
	return uc.Activity != nil && uc.Trigger == triggerClientAction && len(declaredActorIDs(uc)) > 0
}

// declaredActorIDs is the non-empty actor ids of uc in declaration order (CUC-ACTOR-ID
// reports the empty ones).
func declaredActorIDs(uc UseCase) []string {
	var ids []string
	for _, a := range uc.Actors {
		if a.ID != "" {
			ids = append(ids, a.ID)
		}
	}
	return ids
}

// useCaseDoorFindings is the use-case half of UC-ACTOR-DOOR: at least one node the
// declared actors perform or decide.
func useCaseDoorFindings(ordinal int, uc UseCase) []Finding {
	if !needsActorDoor(uc) || hasActorNode(uc) {
		return nil
	}
	return []Finding{{
		RuleID: ruleUCActorDoor, Severity: SeverityError,
		Location: loc(ordinal+1, "useCases["+uc.ID+"].activity"),
		Message: fmt.Sprintf("use case %s has no node linked to a declared actor, so no rule can tell what the actor does or sees; "+
			"on the node where an actor acts, set linkedActorId to one of: %s (or set decidedBy on a decision the actor resolves), "+
			"and draw that node in the actor's lane",
			uc.ID, strings.Join(declaredActorIDs(uc), ", ")),
	}}
}

// hasActorNode reports whether any node of uc's diagram is an actor's act.
func hasActorNode(uc UseCase) bool {
	actors := actorIDs(uc)
	for _, n := range uc.Activity.Nodes {
		if isActorNodeOf(actors, n) {
			return true
		}
	}
	return false
}

// actorDoor is the path half of UC-ACTOR-DOOR: every elementary path of a use case
// that has an actor node somewhere carries one. A use case with none at all is
// reported once by useCaseDoorFindings, not once per path.
func (p pathWalk) actorDoor() []Finding {
	if !needsActorDoor(p.uc) || !hasActorNode(p.uc) {
		return nil
	}
	if _, ok := p.firstActor(); ok {
		return nil
	}
	return []Finding{{
		RuleID: ruleUCActorDoor, Severity: SeverityError, Location: p.at,
		Message: fmt.Sprintf("scenario %s of use case %s passes through no node linked to a declared actor, so no actor enters this path; "+
			"on the node where an actor acts, set linkedActorId to one of: %s (or set decidedBy on a decision the actor resolves)",
			p.sc.ID, p.uc.ID, strings.Join(declaredActorIDs(p.uc), ", ")),
	}}
}

// ucLaneFindings is UC-LANE-RESOLVES: one finding per use case and lane that names
// neither a declared actor of the use case nor a component of s.
func ucLaneFindings(c CoreUseCases, s System) []Finding {
	var out []Finding
	for i, d := range c.Decisions {
		uc := d.UseCase
		if uc.Activity == nil {
			continue
		}
		known := laneNames(uc, s)
		seen := map[string]bool{}
		for _, n := range uc.Activity.Nodes {
			lane := scenario.Normalize(n.RoleName)
			if n.RoleName == "" || known[lane] || seen[lane] {
				continue
			}
			seen[lane] = true
			out = append(out, Finding{
				RuleID: ruleUCLaneResolves, Severity: SeverityError,
				Location: loc(i+1, "useCases["+uc.ID+"].activity"),
				Message: fmt.Sprintf("use case %s draws node %q in lane %q, which names neither a declared actor (%s) nor a System component; "+
					"rename the lane to an actor's role or a component's name, and on the actor's nodes set linkedActorId to one of: %s",
					uc.ID, n.ID, n.RoleName, actorRoles(uc), strings.Join(declaredActorIDs(uc), ", ")),
			})
		}
	}
	return out
}

// laneNames is the set of normalized names a lane of uc may carry: each declared
// actor's id and role, each component's id and name.
func laneNames(uc UseCase, s System) map[string]bool {
	known := map[string]bool{}
	for _, a := range uc.Actors {
		known[scenario.Normalize(a.ID)] = true
		known[scenario.Normalize(a.Role)] = true
	}
	for _, comp := range s.Components {
		known[scenario.Normalize(comp.ID)] = true
		known[scenario.Normalize(comp.Name)] = true
	}
	delete(known, "") // an actor or component without a name resolves nothing
	return known
}

// actorRoles renders the declared actors of uc as "id (role)" for a message.
func actorRoles(uc UseCase) string {
	var parts []string
	for _, a := range uc.Actors {
		parts = append(parts, fmt.Sprintf("%s: %q", a.ID, a.Role))
	}
	if len(parts) == 0 {
		return "none declared"
	}
	return strings.Join(parts, ", ")
}
