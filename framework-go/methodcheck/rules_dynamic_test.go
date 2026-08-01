package methodcheck

import "testing"

// rules_dynamic_test.go PORTS predicates_dynamic_test.go to the structural structs.

func dynamicBaseSystem(t *testing.T) System {
	t.Helper()
	client := comp(t, "AppClient", kindClient)
	mgr := comp(t, "DesignManager", kindManager)
	eng := comp(t, "ValidatingEngine", kindEngine)
	ra := comp(t, "StateAccess", kindResourceAccess)
	store := comp(t, "StateDB", kindResource)
	rels := []Relationship{
		{From: client.ID, To: mgr.ID, Mode: modeSync},
		{From: mgr.ID, To: eng.ID, Mode: modeSync},
		{From: mgr.ID, To: ra.ID, Mode: modeSync},
		{From: ra.ID, To: store.ID, Mode: modeSync},
	}
	dv := DynamicView{
		UseCaseID: nid(),
		Key:       "uc1-core-flow",
		Title:     "Core flow",
		Steps: []CallStep{{Calls: []TraceCall{
			{From: client.ID, To: mgr.ID, Mode: modeSync},
			{From: mgr.ID, To: eng.ID, Mode: modeSync},
			{From: mgr.ID, To: ra.ID, Mode: modeSync},
			{From: ra.ID, To: store.ID, Mode: modeSync},
		}}},
	}
	return System{Components: []Component{client, mgr, eng, ra, store}, Relationships: rels, DynamicViews: []DynamicView{dv}}
}

func TestDynamicViewConsistency_ValidBaseHasNoFindings(t *testing.T) {
	if f := dynamicViewConsistency(dynamicBaseSystem(t), CoreUseCases{}); len(f) != 0 {
		t.Fatalf("a fully legal dynamic view must produce zero findings, got %+v", f)
	}
}

// TestDynamicViewConsistency_EdgeEnds_UnresolvableEndpoint: DV-PART-EXIST was RETIRED
// (2026-07-30 callchain-realization Task 5) — CC-ENDPOINT-RESOLVES subsumes it and
// additionally resolves actor endpoints. DV-EDGE-ENDS keeps the surviving live branch:
// a call endpoint that is neither a System Component nor an actor of the view's use case.
func TestDynamicViewConsistency_EdgeEnds_UnresolvableEndpoint(t *testing.T) {
	s := dynamicBaseSystem(t)
	clientID := s.Components[0].ID
	s.DynamicViews[0].Steps = append(s.DynamicViews[0].Steps, CallStep{
		Calls: []TraceCall{{From: clientID, To: nid(), Mode: modeSync}},
	})
	if !hasRuleFindings(dynamicViewConsistency(s, CoreUseCases{}), ruleDVEdgeEnds) {
		t.Fatalf("expected DV-EDGE-ENDS")
	}
}

// TestDynamicViewConsistency_EdgeEnds_ActorEndpointResolves: an actor of the view's
// OWNING use case is a legal call endpoint under the retargeted rule — it must not be
// reported as a bogus endpoint.
func TestDynamicViewConsistency_EdgeEnds_ActorEndpointResolves(t *testing.T) {
	s := dynamicBaseSystem(t)
	clientID := s.Components[0].ID
	ucID := s.DynamicViews[0].UseCaseID
	s.DynamicViews[0].Steps[0].Calls = append([]TraceCall{{From: "user", To: clientID, Mode: modeSync}},
		s.DynamicViews[0].Steps[0].Calls...)
	c := CoreUseCases{Decisions: []UseCaseDecision{{UseCase: UseCase{
		ID: ucID, Name: "Core flow", Classification: classCore,
		Actors: []Actor{{ID: "user", Role: "User"}},
	}}}}
	if hasRuleFindings(dynamicViewConsistency(s, c), ruleDVEdgeEnds) {
		t.Fatalf("an actor endpoint of the owning use case must resolve; got DV-EDGE-ENDS")
	}
}

func TestDynamicViewConsistency_EdgeInModel(t *testing.T) {
	s := dynamicBaseSystem(t)
	clientID := s.Components[0].ID
	engID := s.Components[2].ID
	s.DynamicViews[0].Steps[0].Calls = append(s.DynamicViews[0].Steps[0].Calls, TraceCall{From: clientID, To: engID, Mode: modeSync})
	if !hasRuleFindings(dynamicViewConsistency(s, CoreUseCases{}), ruleDVEdgeInModel) {
		t.Fatalf("expected DV-EDGE-IN-MODEL")
	}
}

// TestDynamicViewConsistency_EdgeInModel_ModeDrift pins the retargeted match key: a
// realized call whose (from,to) pair IS declared statically but under a DIFFERENT mode
// is static/dynamic drift — the match is now (from,to,mode), not (from,to).
func TestDynamicViewConsistency_EdgeInModel_ModeDrift(t *testing.T) {
	s := dynamicBaseSystem(t)
	// client→mgr is declared sync; realize it queued.
	s.DynamicViews[0].Steps[0].Calls[0].Mode = modeQueued
	if !hasRuleFindings(dynamicViewConsistency(s, CoreUseCases{}), ruleDVEdgeInModel) {
		t.Fatalf("expected DV-EDGE-IN-MODEL for a mode-drifted call")
	}
}

func TestDynamicViewConsistency_Layer(t *testing.T) {
	mgr := comp(t, "DesignManager", kindManager)
	ra := comp(t, "StateAccess", kindResourceAccess)
	rel := Relationship{From: ra.ID, To: mgr.ID, Mode: modeSync}
	s := System{
		Components:    []Component{mgr, ra},
		Relationships: []Relationship{rel},
		DynamicViews: []DynamicView{{
			UseCaseID: nid(), Key: "uc-up", Steps: []CallStep{{Calls: []TraceCall{traceOf(rel)}}},
		}},
	}
	if !hasRuleFindings(dynamicViewConsistency(s, CoreUseCases{}), ruleSysNoUp) {
		t.Fatalf("expected SYS-NOUP (DV-LAYER reuses edgeLegality)")
	}
}

func TestDynamicViewConsistency_SingleMgr(t *testing.T) {
	client := comp(t, "AppClient", kindClient)
	m1 := comp(t, "AManager", kindManager)
	m2 := comp(t, "BManager", kindManager)
	r1 := Relationship{From: client.ID, To: m1.ID, Mode: modeSync}
	r2 := Relationship{From: client.ID, To: m2.ID, Mode: modeSync}
	s := System{
		Components:    []Component{client, m1, m2},
		Relationships: []Relationship{r1, r2},
		DynamicViews: []DynamicView{{
			UseCaseID: nid(), Key: "uc-two-mgrs", Steps: []CallStep{{Calls: []TraceCall{traceOf(r1), traceOf(r2)}}},
		}},
	}
	if !hasRuleFindings(dynamicViewConsistency(s, CoreUseCases{}), ruleDVSingleMgr) {
		t.Fatalf("expected DV-SINGLE-MGR")
	}
}

// TestDynamicViewConsistency_AltGroupToDifferentManagersFires pins the one thing an
// alternative group does NOT get to do (rollout rulings 2026-07-31). Alt is inert for
// the CC-* verdicts and for per-EDGE legality, but the per-VIEW cardinality rules read
// a view's calls as concurrent: two alternatives entering two different Managers is
// one Client driving two Managers, and DV-SINGLE-MGR says so. Alternatives are two
// doors into ONE chain — same Manager — not two chains.
func TestDynamicViewConsistency_AltGroupToDifferentManagersFires(t *testing.T) {
	client := comp(t, "AppClient", kindClient)
	m1 := comp(t, "AManager", kindManager)
	m2 := comp(t, "BManager", kindManager)
	r1 := Relationship{From: client.ID, To: m1.ID, Mode: modeSync}
	r2 := Relationship{From: client.ID, To: m2.ID, Mode: modeSync}
	alt1, alt2 := traceOf(r1), traceOf(r2)
	alt1.Alt, alt2.Alt = "entry", "entry" // ONE alternative group
	s := System{
		Components:    []Component{client, m1, m2},
		Relationships: []Relationship{r1, r2},
		DynamicViews: []DynamicView{{
			UseCaseID: nid(), Key: "uc-alt-two-mgrs", Steps: []CallStep{{Calls: []TraceCall{alt1, alt2}}},
		}},
	}
	if !hasRuleFindings(dynamicViewConsistency(s, CoreUseCases{}), ruleDVSingleMgr) {
		t.Fatalf("an alt group entering two different Managers must still fire DV-SINGLE-MGR")
	}
}

func TestDynamicViewConsistency_Mode(t *testing.T) {
	s := dynamicBaseSystem(t)
	s.DynamicViews[0].Steps[0].Calls[0].Mode = modeEventPubSub
	if !hasRuleFindings(dynamicViewConsistency(s, CoreUseCases{}), ruleDVMode) {
		t.Fatalf("expected DV-MODE")
	}
}

func TestDynamicViewConsistency_KeyUnique(t *testing.T) {
	s := dynamicBaseSystem(t)
	dup := s.DynamicViews[0]
	dup.UseCaseID = nid()
	s.DynamicViews = append(s.DynamicViews, dup)
	if !hasRuleFindings(dynamicViewConsistency(s, CoreUseCases{}), ruleDVKeyUnique) {
		t.Fatalf("expected DV-KEY-UNIQUE")
	}
}

func TestDynamicViewConsistency_EmptyUseCaseID(t *testing.T) {
	s := dynamicBaseSystem(t)
	s.DynamicViews[0].UseCaseID = ""
	if !hasRuleFindings(dynamicViewConsistency(s, CoreUseCases{}), ruleDVKeyUnique) {
		t.Fatalf("expected DV-KEY-UNIQUE for empty UseCaseID")
	}
}

func TestDynamicViewConsistency_StaticCoverage_UnparticipatingCoreComponentFails(t *testing.T) {
	s := dynamicBaseSystem(t)
	// Add a core Engine that appears in no dynamic view.
	orphan := comp(t, "OrphanEngine", kindEngine)
	s.Components = append(s.Components, orphan)
	sev, ok := findingSeverity(dynamicViewConsistency(s, CoreUseCases{}), ruleDVStaticCoverage)
	if !ok {
		t.Fatalf("expected DV-STATIC-COVERAGE for a core component in no dynamic view")
	}
	// Retargeted 2026-07-30 (callchain-realization Task 5): the two coverage rules now
	// ride the CC gate's PoC-advisory severity — the post-QA rollout flips ccGateSeverity
	// back to SeverityError for the whole family at once.
	if sev != ccGateSeverity {
		t.Fatalf("DV-STATIC-COVERAGE must carry ccGateSeverity, got %v", sev)
	}
}

func TestDynamicViewConsistency_StaticCoverage_ResourceAndUtilityExempt(t *testing.T) {
	s := dynamicBaseSystem(t)
	// A Resource and a Utility that appear in no view must NOT trip DV-STATIC-COVERAGE.
	s.Components = append(s.Components,
		comp(t, "OrphanStore", kindResource),
		comp(t, "OrphanUtility", kindUtility))
	if hasRuleFindings(dynamicViewConsistency(s, CoreUseCases{}), ruleDVStaticCoverage) {
		t.Fatalf("Resources and Utilities are exempt from DV-STATIC-COVERAGE")
	}
}

func TestDynamicViewConsistency_StaticCoverage_NoViewsIsNoOp(t *testing.T) {
	s := dynamicBaseSystem(t)
	s.DynamicViews = nil
	if hasRuleFindings(dynamicViewConsistency(s, CoreUseCases{}), ruleDVStaticCoverage) {
		t.Fatalf("with zero dynamic views DV-STATIC-COVERAGE must be a no-op (pre-dynamic-view phase)")
	}
}

func TestDynamicViewConsistency_RelCoverage_UncoveredSyncRelWarns(t *testing.T) {
	s := dynamicBaseSystem(t)
	// Add a static sync relationship (Manager→Manager queued would be legal; use a
	// second RA reachable from the Manager) that appears in no view edge.
	extraRA := comp(t, "AuditAccess", kindResourceAccess)
	s.Components = append(s.Components, extraRA)
	mgrID := s.Components[1].ID
	s.Relationships = append(s.Relationships, Relationship{From: mgrID, To: extraRA.ID, Mode: modeSync})
	// extraRA must still participate in a view or DV-STATIC-COVERAGE would fire too;
	// add it as a participant+edge in the existing view so only DV-REL-COVERAGE is
	// isolated is not required for this assertion — we only assert the rule fires.
	sev, ok := findingSeverity(dynamicViewConsistency(s, CoreUseCases{}), ruleDVRelCoverage)
	if !ok {
		t.Fatalf("expected DV-REL-COVERAGE for a sync relationship in no view edge")
	}
	if sev != ccGateSeverity {
		t.Fatalf("DV-REL-COVERAGE must carry ccGateSeverity, got %v", sev)
	}
}

// DV-PART-USED was RETIRED (2026-07-30 callchain-realization Task 5) along with its
// inverted Task-3 placeholder test. Under the step-keyed DynamicView, participant
// identity is DERIVED from the calls (participantIDs), so "a declared participant no
// call touches" is not constructible — the rule was mathematically dead, not merely
// unexercised. Participation is now checked in the direction that IS meaningful:
// DV-STATIC-COVERAGE (a static component in no chain) and CC-ENDPOINT-RESOLVES (a call
// endpoint that resolves to nothing).

func TestEdgeLegality_MatchesSysLegalityForCallingUp(t *testing.T) {
	mgr := comp(t, "DesignManager", kindManager)
	ra := comp(t, "StateAccess", kindResourceAccess)
	if !hasRuleFindings(edgeLegality(ra, mgr, modeSync, loc(1, "Relationship RA→Mgr")), ruleSysNoUp) {
		t.Fatalf("edgeLegality must emit SYS-NOUP for a calling-up edge")
	}
}
