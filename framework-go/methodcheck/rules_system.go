package methodcheck

import "fmt"

// rules_system.go ports the ValidateArchitecture predicate suite (whole-graph
// layering legality, cardinality, call-chain coverage) + the shared edgeLegality
// helper, FAITHFULLY from predicates_system.go. Rule IDs / severities / messages
// are byte-identical.

const (
	ruleSysNoUp      RuleID = "SYS-NOUP"
	ruleSysNoSide    RuleID = "SYS-NOSIDE"
	ruleSysNoSkip    RuleID = "SYS-NOSKIP"
	ruleSysPubOrig   RuleID = "SYS-PUBORIG"
	ruleSysPubDest   RuleID = "SYS-PUBDEST"
	ruleSysDontMtoM  RuleID = "SYS-DONT-MGR-SYNC-MGR"
	ruleSysDontCli   RuleID = "SYS-DONT-CLIENT-SKIP"
	ruleSysCardMgr   RuleID = "SYS-CARD-MGR"
	ruleSysCardRatio RuleID = "SYS-CARD-RATIO"
	ruleSysCardTotal RuleID = "SYS-CARD-TOTAL"
	ruleArchChainCov RuleID = "ARCH-CHAINCOV"
	ruleSysNameUniq  RuleID = "SYS-NAME-UNIQUE"
)

// layerRank collapses Manager+Engine onto the Business-Logic rank so an M→E edge
// is downward, and returns -1 for Utility (rank-less). Ported from
// projectstate.Layer.Rank() over the wire-name layer string.
func layerRank(layer string) int {
	switch layer {
	case layerClient:
		return 0
	case layerManager, layerEngine:
		return 1
	case layerResourceAccess:
		return 2
	case layerResource:
		return 3
	default:
		return -1 // Utility: rank-less, excluded from up/down legality
	}
}

func componentNameUnique(s System) []Finding {
	var out []Finding
	seen := make(map[string]bool, len(s.Components))
	for i, c := range s.Components {
		section := fmt.Sprintf("component %d (%s)", i+1, c.Name)
		key := Slug(c.Name)
		if key == "" {
			out = append(out, Finding{
				RuleID:   ruleSysNameUniq,
				Severity: SeverityError,
				Message:  fmt.Sprintf("%s: component name is empty or yields no identity slug; every component needs a unique human-readable name", section),
				Location: loc(i+1, section),
			})
			continue
		}
		if seen[key] {
			out = append(out, Finding{
				RuleID:   ruleSysNameUniq,
				Severity: SeverityError,
				Message:  fmt.Sprintf("%s: component name %q is not unique (its identity slug %q collides with an earlier component)", section, c.Name, key),
				Location: loc(i+1, section),
			})
		}
		seen[key] = true
	}
	return out
}

// componentIndex maps component id → Component for endpoint lookup.
func componentIndex(s System) map[string]Component {
	idx := make(map[string]Component, len(s.Components))
	for _, c := range s.Components {
		idx[c.ID] = c
	}
	return idx
}

func sysLegality(s System) []Finding {
	idx := componentIndex(s)
	var out []Finding
	for i, rel := range s.Relationships {
		from, fromOK := idx[rel.From]
		to, toOK := idx[rel.To]
		if !fromOK || !toOK {
			continue
		}
		section := fmt.Sprintf("Relationship %s→%s", from.Name, to.Name)
		out = append(out, edgeLegality(from, to, rel.Mode, loc(i+1, section))...)
	}
	return out
}

// edgeLegality runs the layering + pub/sub + Design-Don't predicates for ONE
// directed edge. Ported verbatim, shared by sysLegality + dynamicViewConsistency.
func edgeLegality(from, to Component, mode string, location *Location) []Finding {
	section := ""
	if location != nil {
		section = location.Section
	}
	fromRank := layerRank(from.Layer)
	toRank := layerRank(to.Layer)
	utilityInvolved := from.Kind == kindUtility || to.Kind == kindUtility

	var out []Finding
	for _, f := range []*Finding{
		checkNoUp(utilityInvolved, fromRank, toRank, section, location),
		checkNoSide(from, to, utilityInvolved, mode, section, fromRank, toRank, location),
		checkNoSkip(utilityInvolved, fromRank, toRank, section, location),
		checkPubOrig(from, mode, section, location),
		checkPubDest(to, mode, section, location),
		checkNoMgrSyncMgr(from, to, mode, section, location),
		checkNoClientSkip(from, to, section, location),
	} {
		if f != nil {
			out = append(out, *f)
		}
	}
	return out
}

func checkNoUp(utilityInvolved bool, fromRank, toRank int, section string, location *Location) *Finding {
	if !utilityInvolved && fromRank > toRank {
		return &Finding{
			RuleID:   ruleSysNoUp,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s calls UP the layer stack (rank %d → rank %d); calls must flow down or M→E equal-rank", section, fromRank, toRank),
			Location: location,
		}
	}
	return nil
}

func checkNoSide(from, to Component, utilityInvolved bool, mode, section string, fromRank, toRank int, location *Location) *Finding {
	if !utilityInvolved && fromRank == toRank && fromRank >= 0 {
		equalKind := from.Kind == to.Kind
		legalQueuedMtoM := from.Kind == kindManager && to.Kind == kindManager && mode == modeQueued
		if equalKind && !legalQueuedMtoM {
			return &Finding{
				RuleID:   ruleSysNoSide,
				Severity: SeverityError,
				Message:  fmt.Sprintf("%s is a sideways call between equal-rank peers; only queued Manager→Manager is permitted", section),
				Location: location,
			}
		}
	}
	return nil
}

func checkNoSkip(utilityInvolved bool, fromRank, toRank int, section string, location *Location) *Finding {
	if !utilityInvolved && toRank > fromRank && (toRank-fromRank) > 1 {
		return &Finding{
			RuleID:   ruleSysNoSkip,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s skips a layer (rank %d → rank %d); calls must be to the adjacent layer", section, fromRank, toRank),
			Location: location,
		}
	}
	return nil
}

func checkPubOrig(from Component, mode, section string, location *Location) *Finding {
	if mode == modeEventPubSub && from.Kind != kindClient && from.Kind != kindManager {
		return &Finding{
			RuleID:   ruleSysPubOrig,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s publishes an event but only Clients and Managers may publish", section),
			Location: location,
		}
	}
	return nil
}

func checkPubDest(to Component, mode, section string, location *Location) *Finding {
	if mode == modeEventPubSub && to.Kind != kindClient && to.Kind != kindManager {
		return &Finding{
			RuleID:   ruleSysPubDest,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s delivers an event to a non-subscriber; only Clients and Managers may subscribe", section),
			Location: location,
		}
	}
	return nil
}

func checkNoMgrSyncMgr(from, to Component, mode, section string, location *Location) *Finding {
	if from.Kind == kindManager && to.Kind == kindManager && mode == modeSync {
		return &Finding{
			RuleID:   ruleSysDontMtoM,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s: a Manager must not call another Manager synchronously (use a queued call)", section),
			Location: location,
		}
	}
	return nil
}

func checkNoClientSkip(from, to Component, section string, location *Location) *Finding {
	if from.Kind == kindClient && to.Kind != kindManager && to.Kind != kindUtility {
		return &Finding{
			RuleID:   ruleSysDontCli,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s: a Client must enter through a Manager (or a Utility), never an Engine/ResourceAccess/Resource directly", section),
			Location: location,
		}
	}
	return nil
}

func sysCardinality(s System) []Finding {
	var managers, engines int
	for _, c := range s.Components {
		switch c.Kind {
		case kindManager:
			managers++
		case kindEngine:
			engines++
		}
	}
	total := len(s.Components)

	var out []Finding

	const maxManagers = 5
	if managers > maxManagers {
		out = append(out, Finding{
			RuleID:   ruleSysCardMgr,
			Severity: SeverityError,
			Message:  fmt.Sprintf("system has %d Managers; The Method limits a system to %d Managers without introducing subsystems", managers, maxManagers),
			Location: loc(0, "system cardinality"),
		})
	}

	if engines > managers {
		out = append(out, Finding{
			RuleID:   ruleSysCardRatio,
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("system has %d Engines but only %d Managers; more Engines than Managers is a smell (golden ratio favours fewer Engines)", engines, managers),
			Location: loc(0, "system cardinality"),
		})
	}

	const totalSanity = 20
	if total > totalSanity {
		out = append(out, Finding{
			RuleID:   ruleSysCardTotal,
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("system has %d components; a well-factored Method system is order-of-a-dozen — consider subsystems", total),
			Location: loc(0, "system cardinality"),
		})
	}

	return out
}

func archChainCoverage(s System, c CoreUseCases) []Finding {
	covered := make(map[string]bool, len(s.DynamicViews))
	for _, dv := range s.DynamicViews {
		covered[dv.UseCaseID] = true
	}
	var out []Finding
	for i, d := range c.Decisions {
		if d.UseCase.Classification != classCore {
			continue
		}
		if !covered[d.UseCase.ID] {
			section := fmt.Sprintf("core use case %d (%s)", i+1, d.UseCase.Name)
			out = append(out, Finding{
				RuleID:   ruleArchChainCov,
				Severity: SeverityError,
				Message:  fmt.Sprintf("%s has no supporting dynamic view in the System; every core use case needs one call-chain", section),
				Location: loc(i+1, section),
			})
		}
	}
	return out
}
