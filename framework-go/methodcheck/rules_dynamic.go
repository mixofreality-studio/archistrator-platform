package methodcheck

import (
	"fmt"
	"sort"
)

// rules_dynamic.go holds the dynamic-view consistency suite owned by
// ValidateArchitecture, ported from predicates_dynamic.go and RETARGETED onto the
// step-keyed DynamicView (2026-07-30 callchain-realization Task 5). DV-LAYER reuses
// edgeLegality, so it emits the SYS-* ids against a dynamic-view Location exactly as
// the original.
//
// The suite reasons over the UNION of a view's per-step calls (stepCalls) — these are
// whole-view invariants (is this call in the static model? does the Client enter one
// Manager?) that hold regardless of which step a call sits in. The rules that DO key
// off CallStep.ActivityNodeID are the CC-* family in rules_callchain.go.
//
// RETIREMENTS (Task 5): DV-PART-EXIST and DV-PART-USED are GONE. Under the step-keyed
// model participant identity is DERIVED from the calls (participantIDs), which made
// DV-PART-USED ("a declared participant no call touches") mathematically dead, and made
// DV-PART-EXIST a duplicate of DV-EDGE-ENDS' surviving branch. CC-ENDPOINT-RESOLVES now
// owns endpoint resolution and additionally resolves ACTORS, which a call chain may now
// name as endpoints — which is also why the rules below take the CoreUseCases: an actor
// endpoint is legal, and legality is relative to the view's OWNING use case.
func stepCalls(dv DynamicView) []Relationship {
	var out []Relationship
	for _, s := range dv.Steps {
		for _, c := range s.Calls {
			out = append(out, c.relationship())
		}
	}
	return out
}

// participantIDs derives the distinct endpoint ids in first-appearance order.
func participantIDs(dv DynamicView) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range dv.Steps {
		for _, c := range s.Calls {
			for _, id := range []string{c.From, c.To} {
				if !seen[id] {
					seen[id] = true
					out = append(out, id)
				}
			}
		}
	}
	return out
}

const (
	ruleDVEdgeEnds       RuleID = "DV-EDGE-ENDS"
	ruleDVEdgeInModel    RuleID = "DV-EDGE-IN-MODEL"
	ruleDVSingleMgr      RuleID = "DV-SINGLE-MGR"
	ruleDVMode           RuleID = "DV-MODE"
	ruleDVKeyUnique      RuleID = "DV-KEY-UNIQUE"
	ruleDVStaticCoverage RuleID = "DV-STATIC-COVERAGE"
	ruleDVRelCoverage    RuleID = "DV-REL-COVERAGE"
	// ruleDVPlannedSkipped (Info) lists the planned components DV-STATIC-COVERAGE
	// deliberately skipped — a planned component cannot yet appear in a call chain, so
	// it is exempt, but the exemption is surfaced (not silent) so it stays visible.
	ruleDVPlannedSkipped RuleID = "DV-PLANNED-SKIPPED"
	// ruleDVRelUtilityExempt (Info) lists the utility-targeted static relationships
	// DV-REL-COVERAGE deliberately skipped (Task 12 / 7b §E4 pre-flip companion) — see
	// checkRelationshipCoverage's utility-target exemption.
	ruleDVRelUtilityExempt RuleID = "DV-REL-UTILITY-EXEMPT"
)

type relPairKey struct {
	from string
	to   string
}

// relCallKey identifies a CALL: the (from,to) pair AND the mode it is made under.
// DV-EDGE-IN-MODEL matches on this triple, so realizing a declared sync relationship
// as a queued call is caught as the static/dynamic drift it is.
type relCallKey struct {
	from string
	to   string
	mode string
}

func dynamicViewConsistency(s System, c CoreUseCases) []Finding {
	idx := componentIndex(s)
	staticCalls := buildStaticCalls(s)
	ucByID := useCaseIndex(c)
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
		out = append(out, checkRelationships(dv, idx, actorIDs(ucByID[dv.UseCaseID]), staticCalls, section, ordinal)...)
	}
	out = append(out, checkStaticParticipationCoverage(s)...)
	out = append(out, checkRelationshipCoverage(s, idx)...)
	return out
}

// checkStaticParticipationCoverage emits DV-STATIC-COVERAGE for every core
// (Client/Manager/Engine/ResourceAccess) component that participates in no
// dynamic view. This is the founder's bidirectional static↔dynamic requirement in
// the static→dynamic direction: a component that exists in the static architecture
// but appears in no call chain is unexplained. Resources and Utilities are exempt
// (isCoreComponentKind — the shared exemption). Gated on ≥1 dynamic view existing:
// before any call chains are drawn there is nothing to be covered by, and
// ARCH-CHAINCOV separately requires a view per core use case.
//
// SEVERITY (2026-07-30 Task 5): this rule and DV-REL-COVERAGE are the two
// static↔dynamic COVERAGE rules, so they ride ccGateSeverity with the CC-* family —
// coverage becomes enforceable exactly when the correspondence family it depends on
// does. The post-QA rollout flips ccGateSeverity to SeverityError for all of them.
func checkStaticParticipationCoverage(s System) []Finding {
	if len(s.DynamicViews) == 0 {
		return nil
	}
	participating := make(map[string]bool)
	for _, dv := range s.DynamicViews {
		for _, pid := range participantIDs(dv) {
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
			Severity: ccGateSeverity,
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

// checkRelationshipCoverage emits DV-REL-COVERAGE for every static relationship
// carrying a call mode (sync/queued) that appears in no dynamic-view call — a declared
// call the dynamic views never exercise. Pub/sub relationships are exempt (they are not
// call-chain edges). Gated on ≥1 dynamic view existing. Carries ccGateSeverity (see
// checkStaticParticipationCoverage — the two coverage rules move together).
//
// Deliberately matched on (from,to) and NOT on mode, unlike DV-EDGE-IN-MODEL: the
// question here is "is this declared call exercised AT ALL", and a mode mismatch on an
// otherwise-exercised pair is already reported, precisely, by DV-EDGE-IN-MODEL.
//
// UTILITY-TARGET EXEMPTION (Task 12 / 7b §E4, the binding pre-flip companion, book-audit
// R3-confirmed): a relationship whose TARGET resolves to a Utility component is exempt
// from the every-relationship-exercised requirement. Utility calls (Logging, Diagnostics,
// Security, the message-bus) are drawn in a realization only where the verb IS the
// business work being narrated (the ratified §C doctrine — e.g. onboard's
// registerSchedule); the ambient dependency itself is not. Some of these edges (every
// Manager→Logging/Diagnostics edge, every Client→Security edge, and the two startup-only
// message-bus verbs om/cm→message-bus) can NEVER be honestly exercised by a use-case flow
// — mirrors DV-STATIC-COVERAGE's isCoreComponentKind exemption (the same reason Utilities
// are exempt from PARTICIPATION coverage exempts them from RELATIONSHIP coverage here).
// Exercising a utility edge stays fully legal (DV-EDGE-IN-MODEL still matches it); only
// the coverage OBLIGATION lifts. Surfaced via DV-REL-UTILITY-EXEMPT (Info), not silent —
// the no-silent-caps idiom shared with DV-PLANNED-SKIPPED.
func checkRelationshipCoverage(s System, idx map[string]Component) []Finding {
	if len(s.DynamicViews) == 0 {
		return nil
	}
	dynEdges := allDynEdges(s)
	var out []Finding
	var utilityExempt []string
	for i, rel := range s.Relationships {
		if !isUncoveredCallRel(rel, dynEdges) {
			continue
		}
		f, exemptSection := relCoverageOutcome(rel, i, idx)
		if exemptSection != "" {
			utilityExempt = append(utilityExempt, exemptSection)
			continue
		}
		out = append(out, f)
	}
	out = append(out, utilityTargetExemptInfo(utilityExempt)...)
	return out
}

// allDynEdges indexes every (from,to) pair exercised by ANY step of ANY dynamic view.
func allDynEdges(s System) map[relPairKey]bool {
	dynEdges := make(map[relPairKey]bool)
	for _, dv := range s.DynamicViews {
		for _, e := range stepCalls(dv) {
			dynEdges[relPairKey{from: e.From, to: e.To}] = true
		}
	}
	return dynEdges
}

// isUncoveredCallRel reports whether rel is a call-mode (sync/queued) relationship
// that no dynamic-view edge exercises — the candidate set DV-REL-COVERAGE reasons over.
func isUncoveredCallRel(rel Relationship, dynEdges map[relPairKey]bool) bool {
	if rel.Mode != modeSync && rel.Mode != modeQueued {
		return false
	}
	return !dynEdges[relPairKey{from: rel.From, to: rel.To}]
}

// relCoverageOutcome classifies one unexercised call-mode relationship: when its
// target resolves to a Utility component, exemptSection names the section to list
// under DV-REL-UTILITY-EXEMPT and f is the zero value; otherwise f is the
// DV-REL-COVERAGE finding to report and exemptSection is empty.
func relCoverageOutcome(rel Relationship, i int, idx map[string]Component) (f Finding, exemptSection string) {
	section := relationshipSection(rel, idx)
	if to, ok := idx[rel.To]; ok && to.Kind == kindUtility {
		return Finding{}, section
	}
	return Finding{
		RuleID:   ruleDVRelCoverage,
		Severity: ccGateSeverity,
		Message:  fmt.Sprintf("%s: static %s relationship appears in no dynamic-view edge; a declared call the call chains never exercise (static/dynamic drift)", section, rel.Mode),
		Location: loc(i+1, section),
	}, ""
}

// utilityTargetExemptInfo returns a single Info finding listing the static
// relationships checkRelationshipCoverage exempted because their target is a Utility
// component, or nil when none were exempted — the no-silent-caps visibility emitter
// for the utility-target exemption, matching plannedSkippedInfo's idiom.
func utilityTargetExemptInfo(exempted []string) []Finding {
	if len(exempted) == 0 {
		return nil
	}
	sort.Strings(exempted)
	return []Finding{{
		RuleID:   ruleDVRelUtilityExempt,
		Severity: SeverityInfo,
		Message:  fmt.Sprintf("relationship coverage exempted %d utility-targeted relationship(s) from the every-relationship-exercised requirement (ambient dependency, never exercised by design): %v", len(exempted), exempted),
		Location: loc(0, "relationship coverage"),
	}}
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

// buildStaticPairs indexes the declared relationships by (from,to) alone — the
// mode-agnostic key the code↔model conformance pass matches against (a Go import edge
// carries no call mode).
func buildStaticPairs(s System) map[relPairKey]bool {
	pairs := make(map[relPairKey]bool, len(s.Relationships))
	for _, rel := range s.Relationships {
		pairs[relPairKey{from: rel.From, to: rel.To}] = true
	}
	return pairs
}

// buildStaticCalls indexes the declared relationships by (from,to,mode) — the key
// DV-EDGE-IN-MODEL matches a realized call against.
func buildStaticCalls(s System) map[relCallKey]bool {
	calls := make(map[relCallKey]bool, len(s.Relationships))
	for _, rel := range s.Relationships {
		calls[relCallKey{from: rel.From, to: rel.To, mode: rel.Mode}] = true
	}
	return calls
}

// checkRelationships runs the per-call DV rules over the UNION of a view's step calls,
// plus the whole-view DV-SINGLE-MGR. actors is the OWNING use case's actor-id set (empty
// when the view's use case is unknown) — actor endpoints are legal call ends, so they
// resolve for DV-EDGE-ENDS and are exempt from DV-EDGE-IN-MODEL (an actor interaction
// is not a component-to-component relationship and is never declared as one).
func checkRelationships(dv DynamicView, idx map[string]Component, actors map[string]bool, staticCalls map[relCallKey]bool, section string, ordinal int) []Finding {
	var out []Finding
	enteredManagers := make(map[string]bool)
	for _, e := range stepCalls(dv) {
		eFindings, enteredMgr := checkSingleDynamicEdge(e, idx, actors, staticCalls, section, ordinal)
		out = append(out, eFindings...)
		if enteredMgr != "" {
			enteredManagers[enteredMgr] = true
		}
	}
	// Counting DISTINCT Managers entered (not distinct entering Clients) is what keeps
	// "several Clients entering ONE Manager" legal while "one use case entering two
	// Managers" is not.
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

func checkSingleDynamicEdge(e Relationship, idx map[string]Component, actors map[string]bool, staticCalls map[relCallKey]bool, section string, ordinal int) ([]Finding, string) {
	from, fromOK := idx[e.From]
	to, toOK := idx[e.To]
	out := dvEdgeEndFindings(e, fromOK, toOK, actors, section, ordinal)
	out = append(out, dvEdgeShapeFindings(e, actors, staticCalls, section, ordinal)...)
	legalFindings, enteredMgr := checkEdgeLegalityAndEntry(e, from, to, fromOK && toOK, section, ordinal)
	return append(out, legalFindings...), enteredMgr
}

// dvEdgeEndFindings is DV-EDGE-ENDS: each end of a call must resolve to a System
// Component OR to an actor of the view's owning use case.
func dvEdgeEndFindings(e Relationship, fromOK, toOK bool, actors map[string]bool, section string, ordinal int) []Finding {
	var out []Finding
	if !fromOK && !actors[e.From] {
		out = append(out, Finding{
			RuleID:   ruleDVEdgeEnds,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s: call source %s is neither a System Component nor an actor of the view's use case", section, e.From),
			Location: loc(ordinal, section),
		})
	}
	if !toOK && !actors[e.To] {
		out = append(out, Finding{
			RuleID:   ruleDVEdgeEnds,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s: call target %s is neither a System Component nor an actor of the view's use case", section, e.To),
			Location: loc(ordinal, section),
		})
	}
	return out
}

// dvEdgeShapeFindings is DV-EDGE-IN-MODEL + DV-MODE: a component-to-component call must
// be declared statically under the SAME mode, and every call must use a call mode.
// Calls with an actor end are exempt from the static match — an actor interaction is not
// a System.Relationships edge (CC-ACTOR-EDGE governs its legality instead).
func dvEdgeShapeFindings(e Relationship, actors map[string]bool, staticCalls map[relCallKey]bool, section string, ordinal int) []Finding {
	var out []Finding
	if !actors[e.From] && !actors[e.To] && !staticCalls[relCallKey{from: e.From, to: e.To, mode: e.Mode}] {
		out = append(out, Finding{
			RuleID:   ruleDVEdgeInModel,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s: dynamic call %s→%s (%s) has no matching static System.Relationships entry (static/dynamic drift)", section, e.From, e.To, e.Mode),
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
	return out
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
