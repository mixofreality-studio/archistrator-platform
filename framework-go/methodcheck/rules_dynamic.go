package methodcheck

import "fmt"

// rules_dynamic.go ports the dynamic-view consistency suite owned by
// ValidateArchitecture, FAITHFULLY from predicates_dynamic.go. Rule IDs /
// severities / messages are byte-identical. DV-LAYER reuses edgeLegality, so it
// emits the SYS-* ids against a dynamic-view Location exactly as the original.

const (
	ruleDVPartExist   RuleID = "DV-PART-EXIST"
	ruleDVEdgeEnds    RuleID = "DV-EDGE-ENDS"
	ruleDVEdgeInModel RuleID = "DV-EDGE-IN-MODEL"
	ruleDVSingleMgr   RuleID = "DV-SINGLE-MGR"
	ruleDVMode        RuleID = "DV-MODE"
	ruleDVKeyUnique   RuleID = "DV-KEY-UNIQUE"
)

type relPairKey struct {
	from string
	to   string
}

func dynamicViewConsistency(s System) []Finding {
	idx := componentIndex(s)
	staticPairs := buildStaticPairs(s)
	var out []Finding
	seenKeys := make(map[string]bool, len(s.DynamicViews))
	for i, dv := range s.DynamicViews {
		ordinal := i + 1
		section := fmt.Sprintf("dynamic view %q", dv.Key)
		if seenKeys[dv.Key] {
			out = append(out, Finding{
				RuleID:   ruleDVKeyUnique,
				Severity: SeverityError,
				Message:  fmt.Sprintf("%s: dynamic-view Key %q is not unique across the System", section, dv.Key),
				Location: loc(ordinal, section),
			})
		}
		seenKeys[dv.Key] = true
		if dv.UseCaseID == "" {
			out = append(out, Finding{
				RuleID:   ruleDVKeyUnique,
				Severity: SeverityError,
				Message:  fmt.Sprintf("%s: dynamic view has an empty UseCaseID; every view must reference a use case", section),
				Location: loc(ordinal, section),
			})
		}
		participantSet, pFindings := checkDynamicViewParticipants(dv, idx, section, ordinal)
		out = append(out, pFindings...)
		out = append(out, checkRelationships(dv, idx, participantSet, staticPairs, section, ordinal)...)
	}
	return out
}

func buildStaticPairs(s System) map[relPairKey]bool {
	pairs := make(map[relPairKey]bool, len(s.Relationships))
	for _, rel := range s.Relationships {
		pairs[relPairKey{from: rel.From, to: rel.To}] = true
	}
	return pairs
}

func checkDynamicViewParticipants(dv DynamicView, idx map[string]Component, section string, ordinal int) (map[string]bool, []Finding) {
	participantSet := make(map[string]bool, len(dv.Participants))
	var out []Finding
	for _, pid := range dv.Participants {
		if _, ok := idx[pid]; !ok {
			out = append(out, Finding{
				RuleID:   ruleDVPartExist,
				Severity: SeverityError,
				Message:  fmt.Sprintf("%s: participant %s is not a System Component", section, pid),
				Location: loc(ordinal, section),
			})
		}
		participantSet[pid] = true
	}
	return participantSet, out
}

func checkRelationships(dv DynamicView, idx map[string]Component, participantSet map[string]bool, staticPairs map[relPairKey]bool, section string, ordinal int) []Finding {
	var out []Finding
	enteredManagers := make(map[string]bool)
	for _, e := range dv.Edges {
		from, fromOK := idx[e.From]
		to, toOK := idx[e.To]
		eFindings, enteredMgr := checkSingleDynamicEdge(e, from, fromOK, to, toOK, participantSet, staticPairs, section, ordinal)
		out = append(out, eFindings...)
		if enteredMgr != "" {
			enteredManagers[enteredMgr] = true
		}
	}
	if len(enteredManagers) > 1 {
		out = append(out, Finding{
			RuleID:   ruleDVSingleMgr,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s: the Client enters %d distinct Managers; a use-case call chain must enter exactly one Manager (Don't 6a)", section, len(enteredManagers)),
			Location: loc(ordinal, section),
		})
	}
	return out
}

func checkSingleDynamicEdge(e Relationship, from Component, fromOK bool, to Component, toOK bool, participantSet map[string]bool, staticPairs map[relPairKey]bool, section string, ordinal int) ([]Finding, string) {
	var out []Finding
	if !fromOK || !participantSet[e.From] {
		out = append(out, Finding{
			RuleID:   ruleDVEdgeEnds,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s: edge source %s is not a real Component declared as a participant", section, e.From),
			Location: loc(ordinal, section),
		})
	}
	if !toOK || !participantSet[e.To] {
		out = append(out, Finding{
			RuleID:   ruleDVEdgeEnds,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s: edge target %s is not a real Component declared as a participant", section, e.To),
			Location: loc(ordinal, section),
		})
	}
	if !staticPairs[relPairKey{from: e.From, to: e.To}] {
		out = append(out, Finding{
			RuleID:   ruleDVEdgeInModel,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s: dynamic edge %s→%s has no matching static System.Relationships pair (static/dynamic drift)", section, e.From, e.To),
			Location: loc(ordinal, section),
		})
	}
	if e.Mode != modeSync && e.Mode != modeQueued {
		out = append(out, Finding{
			RuleID:   ruleDVMode,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s: dynamic edge %s→%s uses a non-call mode; a call chain may only use sync or queued edges", section, e.From, e.To),
			Location: loc(ordinal, section),
		})
	}
	legalFindings, enteredMgr := checkEdgeLegalityAndEntry(e, from, to, fromOK && toOK, section, ordinal)
	return append(out, legalFindings...), enteredMgr
}

func checkEdgeLegalityAndEntry(e Relationship, from, to Component, bothOK bool, section string, ordinal int) ([]Finding, string) {
	if !bothOK {
		return nil, ""
	}
	out := edgeLegality(from, to, e.Mode, loc(ordinal, section))
	if from.Kind == kindClient && to.Kind == kindManager {
		return out, e.To
	}
	return out, ""
}
