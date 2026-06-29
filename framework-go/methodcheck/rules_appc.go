package methodcheck

import "fmt"

// rules_appc.go encodes the automated subset of Appendix C that the System model
// carries enough information to check. Part C of the conformance gate.
//
// Directives → SeverityError. Guidelines → SeverityWarning (waivable via
// applyWaivers). A waived guideline finding → SeverityInfo.

const (
	// §3.6 Interaction don'ts (directives → SeverityError)
	ruleAppcDontClientMultiMgr RuleID = "APPC-INT-CLIENT-MULTI-MGR" // SYS-6a
	ruleAppcDontMgrMultiQueue  RuleID = "APPC-INT-MGR-MULTI-QUEUE"  // SYS-6b
	ruleAppcDontEngineQueue    RuleID = "APPC-INT-ENGINE-NO-QUEUE"  // SYS-6c
	ruleAppcDontRAQueue        RuleID = "APPC-INT-RA-NO-QUEUE"      // SYS-6d
	ruleAppcDontClientPub      RuleID = "APPC-INT-CLIENT-NO-PUB"    // SYS-6e
	ruleAppcDontEnginePub      RuleID = "APPC-INT-ENGINE-NO-PUB"    // SYS-6f
	ruleAppcDontRAPub          RuleID = "APPC-INT-RA-NO-PUB"        // SYS-6g
	ruleAppcDontResourcePub    RuleID = "APPC-INT-RESOURCE-NO-PUB"  // SYS-6h
	ruleAppcDontNonMgrSub      RuleID = "APPC-INT-NONMGR-NO-SUB"    // SYS-6i

	// §3.4 Closed architecture (guidelines → SeverityWarning)
	ruleAppcArchOpen     RuleID = "APPC-ARCH-OPEN"      // SYS-4a
	ruleAppcArchSemiOpen RuleID = "APPC-ARCH-SEMI-OPEN" // SYS-4b

	// §3.2 Per-subsystem cardinality (guideline → SeverityWarning)
	ruleAppcCardSubMgr RuleID = "APPC-CARD-SUB-MGR" // SYS-2c: >3 Managers per subsystem

	// §3.5 Interaction rules (guidelines → SeverityWarning, for completeness in coverage)
	ruleAppcIntUtility  RuleID = "APPC-INT-UTILITY"    // SYS-5a
	ruleAppcIntMgrEngRA RuleID = "APPC-INT-MGR-ENG-RA" // SYS-5b
	ruleAppcIntMgrEng   RuleID = "APPC-INT-MGR-ENG"    // SYS-5c

	// §6 Service contract metrics
	ruleAppcSvcSingle   RuleID = "APPC-SVC-SINGLE"    // SYS-SVC-2a: avoid 1-op (Warning)
	ruleAppcSvcStrive   RuleID = "APPC-SVC-STRIVE"    // SYS-SVC-2b: strive 3–5 (Info)
	ruleAppcSvcAvoid12  RuleID = "APPC-SVC-AVOID-12"  // SYS-SVC-2c: avoid >12 (Warning)
	ruleAppcSvcReject20 RuleID = "APPC-SVC-REJECT-20" // SYS-SVC-2d: ≥20 reject (Error, directive)
)

// appCInteractionDonts checks §3.6 — the nine "don't" directives.
// These overlap with existing rules_system.go checks (SYS-PUBORIG/PUBDEST,
// SYS-DONT-MGR-SYNC-MGR, SYS-DONT-CLIENT-SKIP) but carry distinct APPC-*
// rule IDs for traceability to the coverage matrix.
func appCInteractionDonts(s System) []Finding {
	idx := componentIndex(s)
	var out []Finding
	out = append(out, clientMultiMgrDonts(s, idx)...)
	out = append(out, relationshipDonts(s, idx)...)
	return out
}

// clientMultiMgrDonts detects SYS-6a — a Client calling more than one Manager
// within a single dynamic-view (use case).
func clientMultiMgrDonts(s System, idx map[string]Component) []Finding {
	var out []Finding
	for i, dv := range s.DynamicViews {
		clientID, mgrsCalled := clientManagerCalls(dv, idx)
		if clientID != "" && len(mgrsCalled) > 1 {
			section := fmt.Sprintf("dynamic view %d (%s)", i+1, dv.Key)
			out = append(out, Finding{
				RuleID:   ruleAppcDontClientMultiMgr,
				Severity: SeverityError,
				Message:  fmt.Sprintf("%s: a Client calls %d Managers in one use case; clients may not call multiple Managers in the same use case (App-C §3.6a)", section, len(mgrsCalled)),
				Location: loc(i+1, section),
			})
		}
	}
	return out
}

// clientManagerCalls returns the calling Client's ID (if any) and the set of
// Managers it calls within one dynamic-view.
func clientManagerCalls(dv DynamicView, idx map[string]Component) (string, map[string]bool) {
	mgrsCalled := make(map[string]bool)
	var clientID string
	for _, e := range dv.Edges {
		from, fromOK := idx[e.From]
		to, toOK := idx[e.To]
		if !fromOK || !toOK {
			continue
		}
		if from.Kind == kindClient && to.Kind == kindManager {
			clientID = e.From
			mgrsCalled[e.To] = true
		}
	}
	return clientID, mgrsCalled
}

// relationshipDonts checks the relationship-level §3.6 don'ts (SYS-6c..6i).
func relationshipDonts(s System, idx map[string]Component) []Finding {
	var out []Finding
	for i, rel := range s.Relationships {
		from, fromOK := idx[rel.From]
		to, toOK := idx[rel.To]
		if !fromOK || !toOK {
			continue
		}
		section := fmt.Sprintf("Relationship %s→%s", from.Name, to.Name)
		out = append(out, relationshipDontFindings(from, to, rel, section, loc(i+1, section))...)
	}
	return out
}

// relDontRule is one table-driven §3.6 relationship don't: a predicate plus the
// rule ID and message text (the message is prefixed with the relationship locus).
type relDontRule struct {
	ruleID RuleID
	text   string
	match  func(from, to Component, rel Relationship) bool
}

// relDontRules enumerates SYS-6c..6i in their original evaluation order.
var relDontRules = []relDontRule{
	{ // SYS-6c: Engines do not receive queued calls
		ruleAppcDontEngineQueue, "an Engine must not receive a queued call (App-C §3.6c)",
		func(from, to Component, rel Relationship) bool {
			return to.Kind == kindEngine && rel.Mode == modeQueued
		},
	},
	{ // SYS-6d: ResourceAccess do not receive queued calls
		ruleAppcDontRAQueue, "a ResourceAccess must not receive a queued call (App-C §3.6d)",
		func(from, to Component, rel Relationship) bool {
			return to.Kind == kindResourceAccess && rel.Mode == modeQueued
		},
	},
	{ // SYS-6e: Clients do not publish events
		ruleAppcDontClientPub, "a Client must not publish events (App-C §3.6e)",
		func(from, to Component, rel Relationship) bool {
			return from.Kind == kindClient && rel.Mode == modeEventPubSub
		},
	},
	{ // SYS-6f: Engines do not publish events
		ruleAppcDontEnginePub, "an Engine must not publish events (App-C §3.6f)",
		func(from, to Component, rel Relationship) bool {
			return from.Kind == kindEngine && rel.Mode == modeEventPubSub
		},
	},
	{ // SYS-6g: ResourceAccess do not publish events
		ruleAppcDontRAPub, "a ResourceAccess must not publish events (App-C §3.6g)",
		func(from, to Component, rel Relationship) bool {
			return from.Kind == kindResourceAccess && rel.Mode == modeEventPubSub
		},
	},
	{ // SYS-6h: Resources do not publish events
		ruleAppcDontResourcePub, "a Resource must not publish events (App-C §3.6h)",
		func(from, to Component, rel Relationship) bool {
			return from.Kind == kindResource && rel.Mode == modeEventPubSub
		},
	},
	{ // SYS-6i: Engines, ResourceAccess, Resources do not subscribe to events
		ruleAppcDontNonMgrSub, "Engines, ResourceAccess, and Resources must not subscribe to events (App-C §3.6i)",
		func(from, to Component, rel Relationship) bool {
			subscriberKinds := map[string]bool{kindEngine: true, kindResourceAccess: true, kindResource: true}
			return rel.Mode == modeEventPubSub && subscriberKinds[to.Kind]
		},
	},
}

// relationshipDontFindings evaluates every §3.6 relationship rule against one edge.
func relationshipDontFindings(from, to Component, rel Relationship, section string, l *Location) []Finding {
	var out []Finding
	for _, r := range relDontRules {
		if r.match(from, to, rel) {
			out = append(out, Finding{
				RuleID:   r.ruleID,
				Severity: SeverityError,
				Message:  fmt.Sprintf("%s: %s", section, r.text),
				Location: l,
			})
		}
	}
	return out
}

// appCClosedArch checks §3.4 — prefer closed architecture.
// Open arch = RA→RA edges or any skip-adjacency outside Utility paths.
// These are SYS-4a/4b guidelines → SeverityWarning.
func appCClosedArch(s System) []Finding {
	idx := componentIndex(s)
	var out []Finding
	for i, rel := range s.Relationships {
		from, fromOK := idx[rel.From]
		to, toOK := idx[rel.To]
		if !fromOK || !toOK {
			continue
		}
		out = append(out, closedArchFindings(from, to, i)...)
	}
	return out
}

// closedArchFindings evaluates the §3.4a/3.4b open-architecture signals for one
// edge (i is the relationship ordinal, used for the finding location).
func closedArchFindings(from, to Component, i int) []Finding {
	if from.Kind == kindUtility || to.Kind == kindUtility {
		return nil // utility edges are rank-exempt
	}
	fromRank := layerRank(from.Layer)
	toRank := layerRank(to.Layer)
	section := fmt.Sprintf("Relationship %s→%s", from.Name, to.Name)
	var out []Finding
	// Semi-open: same-rank non-Manager peers (sideways)
	if fromRank == toRank && fromRank >= 0 && from.Kind != kindManager && to.Kind != kindManager {
		out = append(out, Finding{
			RuleID:   ruleAppcArchSemiOpen,
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("%s: sideways call between same-rank non-Manager peers indicates semi-open architecture (App-C §3.4b); prefer closed", section),
			Location: loc(i+1, section),
		})
	}
	// Open: skip-layer (already an Error via SYS-NOSKIP, but also surface as arch signal)
	if toRank > fromRank && (toRank-fromRank) > 1 {
		out = append(out, Finding{
			RuleID:   ruleAppcArchOpen,
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("%s: layer-skipping call indicates open architecture (App-C §3.4a); prefer closed", section),
			Location: loc(i+1, section),
		})
	}
	return out
}

// appCCardinality checks §3.2c — avoid more than 3 Managers per subsystem.
// Without explicit subsystem modelling in the JSON, we count total managers as a
// proxy; anything >3 in a flat (non-subsystem) design is a guideline warning.
func appCCardinality(s System) []Finding {
	managers := 0
	for _, c := range s.Components {
		if c.Kind == kindManager {
			managers++
		}
	}
	const maxMgrPerSubsystem = 3
	if managers > maxMgrPerSubsystem {
		return []Finding{{
			RuleID:   ruleAppcCardSubMgr,
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("system has %d Managers; App-C §3.2c strives for ≤3 per subsystem — consider introducing subsystems", managers),
			Location: loc(0, "system cardinality"),
		}}
	}
	return nil
}

// appCServiceContract checks §6 service-contract op-count metrics against the
// System's component.AtomicBusinessVerbs (each entry = one operation).
// Applies only to Manager/Engine/ResourceAccess — Clients, Resources, Utilities are exempt.
func appCServiceContract(s System) []Finding {
	contractLayers := map[string]bool{kindManager: true, kindEngine: true, kindResourceAccess: true}
	var out []Finding
	for i, c := range s.Components {
		if !contractLayers[c.Kind] {
			continue
		}
		n := len(c.AtomicBusinessVerbs)
		section := fmt.Sprintf("component %d (%s)", i+1, c.Name)
		l := loc(i+1, section)
		switch {
		case n >= 20:
			out = append(out, Finding{
				RuleID:   ruleAppcSvcReject20,
				Severity: SeverityError,
				Message:  fmt.Sprintf("%s has %d ops (≥20); App-C §6.2d directs rejection of contracts with 20 or more operations", section, n),
				Location: l,
			})
		case n > 12:
			out = append(out, Finding{
				RuleID:   ruleAppcSvcAvoid12,
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("%s has %d ops (>12); App-C §6.2c advises avoiding contracts with more than 12 operations", section, n),
				Location: l,
			})
		case n == 1:
			out = append(out, Finding{
				RuleID:   ruleAppcSvcSingle,
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("%s has 1 op; App-C §6.2a advises avoiding contracts with a single operation", section),
				Location: l,
			})
		}
	}
	return out
}

// applyWaivers downgrades guideline-severity (Warning) findings whose rule ID
// is matched by a waived+justified StandardCheck entry to SeverityInfo.
// Directive findings (SeverityError with a directive rule ID) are never downgraded.
func applyWaivers(findings []Finding, sc StandardCheck) []Finding {
	waivedRules := waivedRuleSet(sc)
	if len(waivedRules) == 0 {
		return findings
	}
	out := make([]Finding, len(findings))
	copy(out, findings)
	for i, f := range out {
		if f.Severity == SeverityWarning && waivedRules[f.RuleID] {
			out[i].Severity = SeverityInfo
		}
	}
	return out
}

// waivedRuleSet builds the set of guideline rule IDs covered by a waived+justified
// StandardCheck entry. Directives are never included.
func waivedRuleSet(sc StandardCheck) map[RuleID]bool {
	waivedRules := make(map[RuleID]bool)
	for _, item := range sc.Items {
		// Only CheckItems with status==checkWaived and a non-empty justification waive.
		if item.Status != checkWaived || len(item.Justification) == 0 {
			continue
		}
		addWaivedRules(item, waivedRules)
	}
	return waivedRules
}

// addWaivedRules adds every guideline rule the waived item covers into waivedRules.
func addWaivedRules(item CheckItem, waivedRules map[RuleID]bool) {
	for _, cov := range DefaultCoverage() {
		if cov.Kind == AppCDirective {
			continue // directives never waived
		}
		if cov.RuleID != "" && waiverMatchesCoverage(item, cov) {
			waivedRules[cov.RuleID] = true
		}
	}
}

// waiverMatchesCoverage reports whether a StandardCheck item references the given
// coverage entry by its App-C ref or rule ID.
func waiverMatchesCoverage(item CheckItem, cov AppCItem) bool {
	return item.Section == string(cov.AppcRef) ||
		item.Section == string(cov.RuleID) ||
		item.Guideline == string(cov.RuleID)
}
