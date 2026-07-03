package methodcheck

import (
	"fmt"
	"sort"
)

// rules_dynamic.go ports the dynamic-view consistency suite owned by
// ValidateArchitecture, FAITHFULLY from predicates_dynamic.go. Rule IDs /
// severities / messages are byte-identical. DV-LAYER reuses edgeLegality, so it
// emits the SYS-* ids against a dynamic-view Location exactly as the original.

const (
	ruleDVPartExist      RuleID = "DV-PART-EXIST"
	ruleDVEdgeEnds       RuleID = "DV-EDGE-ENDS"
	ruleDVEdgeInModel    RuleID = "DV-EDGE-IN-MODEL"
	ruleDVSingleMgr      RuleID = "DV-SINGLE-MGR"
	ruleDVMode           RuleID = "DV-MODE"
	ruleDVKeyUnique      RuleID = "DV-KEY-UNIQUE"
	ruleDVStaticCoverage RuleID = "DV-STATIC-COVERAGE"
	ruleDVRelCoverage    RuleID = "DV-REL-COVERAGE"
	ruleDVPartUsed       RuleID = "DV-PART-USED"
	// ruleDVPlannedSkipped (Info) lists the planned components DV-STATIC-COVERAGE
	// deliberately skipped — a planned component cannot yet appear in a call chain, so
	// it is exempt, but the exemption is surfaced (not silent) so it stays visible.
	ruleDVPlannedSkipped RuleID = "DV-PLANNED-SKIPPED"
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
		out = append(out, checkParticipantsUsed(dv, section, ordinal)...)
	}
	out = append(out, checkStaticParticipationCoverage(s)...)
	out = append(out, checkRelationshipCoverage(s, idx)...)
	return out
}

// checkParticipantsUsed emits DV-PART-USED (Error) for every declared participant
// of a view that no edge of that view touches — a participant that takes part in
// no call is dead weight in the call chain.
func checkParticipantsUsed(dv DynamicView, section string, ordinal int) []Finding {
	used := make(map[string]bool, len(dv.Edges)*2)
	for _, e := range dv.Edges {
		used[e.From] = true
		used[e.To] = true
	}
	var out []Finding
	for _, pid := range dv.Participants {
		if !used[pid] {
			out = append(out, Finding{
				RuleID:   ruleDVPartUsed,
				Severity: SeverityError,
				Message:  fmt.Sprintf("%s: participant %s appears in no edge of the view; every declared participant must take part in ≥1 call", section, pid),
				Location: loc(ordinal, section),
			})
		}
	}
	return out
}

// checkStaticParticipationCoverage emits DV-STATIC-COVERAGE (Error) for every
// core (Client/Manager/Engine/ResourceAccess) component that participates in no
// dynamic view. This is the founder's bidirectional static↔dynamic requirement in
// the static→dynamic direction: a component that exists in the static architecture
// but appears in no call chain is unexplained. Resources and Utilities are exempt
// (isCoreComponentKind — the shared exemption). Gated on ≥1 dynamic view existing:
// before any call chains are drawn there is nothing to be covered by, and
// ARCH-CHAINCOV separately requires a view per core use case.
func checkStaticParticipationCoverage(s System) []Finding {
	if len(s.DynamicViews) == 0 {
		return nil
	}
	participating := make(map[string]bool)
	for _, dv := range s.DynamicViews {
		for _, pid := range dv.Participants {
			participating[pid] = true
		}
	}
	var out []Finding
	var planned []string
	for i, c := range s.Components {
		if !isCoreComponentKind(c.Kind) || participating[c.ID] {
			continue
		}
		// A planned component cannot yet be in a call chain — exempt it (surfaced below).
		if c.BuildStatus == buildStatusPlanned {
			planned = append(planned, c.Name)
			continue
		}
		section := fmt.Sprintf("component %d (%s)", i+1, c.Name)
		out = append(out, Finding{
			RuleID:   ruleDVStaticCoverage,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s is a %s in the static architecture but participates in no dynamic view; every Client/Manager/Engine/ResourceAccess component must appear in ≥1 call chain (static/dynamic drift)", section, c.Kind),
			Location: loc(i+1, section),
		})
	}
	out = append(out, plannedSkippedInfo(ruleDVPlannedSkipped, "dynamic-view participation coverage", planned)...)
	return out
}

// plannedSkippedInfo returns a single Info finding listing the planned components a
// coverage rule skipped, or nil when none were skipped — the shared visibility emitter
// for the planned exemptions in DV-STATIC-COVERAGE and DEP-COVERAGE.
func plannedSkippedInfo(id RuleID, what string, planned []string) []Finding {
	if len(planned) == 0 {
		return nil
	}
	sort.Strings(planned)
	return []Finding{{
		RuleID:   id,
		Severity: SeverityInfo,
		Message:  fmt.Sprintf("%s skipped %d planned component(s) not yet expected in the architecture: %v", what, len(planned), planned),
		Location: loc(0, what),
	}}
}

// checkRelationshipCoverage emits DV-REL-COVERAGE (Warning) for every static
// relationship carrying a call mode (sync/queued) that appears in no dynamic-view
// edge — a declared call the dynamic views never exercise. Pub/sub relationships
// are exempt (they are not call-chain edges). Gated on ≥1 dynamic view existing.
func checkRelationshipCoverage(s System, idx map[string]Component) []Finding {
	if len(s.DynamicViews) == 0 {
		return nil
	}
	dynEdges := make(map[relPairKey]bool)
	for _, dv := range s.DynamicViews {
		for _, e := range dv.Edges {
			dynEdges[relPairKey{from: e.From, to: e.To}] = true
		}
	}
	var out []Finding
	for i, rel := range s.Relationships {
		if rel.Mode != modeSync && rel.Mode != modeQueued {
			continue
		}
		if dynEdges[relPairKey{from: rel.From, to: rel.To}] {
			continue
		}
		section := relationshipSection(rel, idx)
		out = append(out, Finding{
			RuleID:   ruleDVRelCoverage,
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("%s: static %s relationship appears in no dynamic-view edge; a declared call the call chains never exercise (static/dynamic drift)", section, rel.Mode),
			Location: loc(i+1, section),
		})
	}
	return out
}

// relationshipSection renders a human-readable locus for a relationship, using
// component names when both endpoints resolve.
func relationshipSection(rel Relationship, idx map[string]Component) string {
	from, fromOK := idx[rel.From]
	to, toOK := idx[rel.To]
	if fromOK && toOK {
		return fmt.Sprintf("Relationship %s→%s", from.Name, to.Name)
	}
	return fmt.Sprintf("Relationship %s→%s", rel.From, rel.To)
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
