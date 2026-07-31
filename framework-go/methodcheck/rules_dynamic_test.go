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
		Steps: []CallStep{{Calls: []Relationship{
			{From: client.ID, To: mgr.ID, Mode: modeSync},
			{From: mgr.ID, To: eng.ID, Mode: modeSync},
			{From: mgr.ID, To: ra.ID, Mode: modeSync},
			{From: ra.ID, To: store.ID, Mode: modeSync},
		}}},
	}
	return System{Components: []Component{client, mgr, eng, ra, store}, Relationships: rels, DynamicViews: []DynamicView{dv}}
}

func TestDynamicViewConsistency_ValidBaseHasNoFindings(t *testing.T) {
	if f := dynamicViewConsistency(dynamicBaseSystem(t)); len(f) != 0 {
		t.Fatalf("a fully legal dynamic view must produce zero findings, got %+v", f)
	}
}

// TestDynamicViewConsistency_PartExist: under the step-keyed model, participant
// identity is derived purely from call endpoints (participantIDs) — there is no
// longer an independent Participants list to append a bogus id to. The equivalent
// construction is a call edge that NAMES an id the System does not declare as a
// Component; participantIDs surfaces it exactly the same way DV-PART-EXIST expects.
func TestDynamicViewConsistency_PartExist(t *testing.T) {
	s := dynamicBaseSystem(t)
	clientID := s.Components[0].ID
	s.DynamicViews[0].Steps = append(s.DynamicViews[0].Steps, CallStep{
		Calls: []Relationship{{From: clientID, To: nid(), Mode: modeSync}},
	})
	if !hasRuleFindings(dynamicViewConsistency(s), ruleDVPartExist) {
		t.Fatalf("expected DV-PART-EXIST")
	}
}

// TestDynamicViewConsistency_EdgeEnds_NotAParticipant: the original scenario (an edge
// whose endpoint IS a real Component, deliberately dropped from a separately-declared
// Participants list) can no longer be constructed — Participants no longer exists
// independently of the calls, so "declared participant" and "edge endpoint" are now
// the SAME derived set by construction. DV-EDGE-ENDS' "not a real Component" half
// remains reachable via an edge naming an undeclared id (this is now the same
// construction as DV-PART-EXIST above; both rules legitimately fire together on it).
func TestDynamicViewConsistency_EdgeEnds_NotAParticipant(t *testing.T) {
	s := dynamicBaseSystem(t)
	clientID := s.Components[0].ID
	s.DynamicViews[0].Steps = append(s.DynamicViews[0].Steps, CallStep{
		Calls: []Relationship{{From: clientID, To: nid(), Mode: modeSync}},
	})
	if !hasRuleFindings(dynamicViewConsistency(s), ruleDVEdgeEnds) {
		t.Fatalf("expected DV-EDGE-ENDS")
	}
}

func TestDynamicViewConsistency_EdgeInModel(t *testing.T) {
	s := dynamicBaseSystem(t)
	clientID := s.Components[0].ID
	engID := s.Components[2].ID
	s.DynamicViews[0].Steps[0].Calls = append(s.DynamicViews[0].Steps[0].Calls, Relationship{From: clientID, To: engID, Mode: modeSync})
	if !hasRuleFindings(dynamicViewConsistency(s), ruleDVEdgeInModel) {
		t.Fatalf("expected DV-EDGE-IN-MODEL")
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
			UseCaseID: nid(), Key: "uc-up", Steps: []CallStep{{Calls: []Relationship{rel}}},
		}},
	}
	if !hasRuleFindings(dynamicViewConsistency(s), ruleSysNoUp) {
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
			UseCaseID: nid(), Key: "uc-two-mgrs", Steps: []CallStep{{Calls: []Relationship{r1, r2}}},
		}},
	}
	if !hasRuleFindings(dynamicViewConsistency(s), ruleDVSingleMgr) {
		t.Fatalf("expected DV-SINGLE-MGR")
	}
}

func TestDynamicViewConsistency_Mode(t *testing.T) {
	s := dynamicBaseSystem(t)
	s.DynamicViews[0].Steps[0].Calls[0].Mode = modeEventPubSub
	if !hasRuleFindings(dynamicViewConsistency(s), ruleDVMode) {
		t.Fatalf("expected DV-MODE")
	}
}

func TestDynamicViewConsistency_KeyUnique(t *testing.T) {
	s := dynamicBaseSystem(t)
	dup := s.DynamicViews[0]
	dup.UseCaseID = nid()
	s.DynamicViews = append(s.DynamicViews, dup)
	if !hasRuleFindings(dynamicViewConsistency(s), ruleDVKeyUnique) {
		t.Fatalf("expected DV-KEY-UNIQUE")
	}
}

func TestDynamicViewConsistency_EmptyUseCaseID(t *testing.T) {
	s := dynamicBaseSystem(t)
	s.DynamicViews[0].UseCaseID = ""
	if !hasRuleFindings(dynamicViewConsistency(s), ruleDVKeyUnique) {
		t.Fatalf("expected DV-KEY-UNIQUE for empty UseCaseID")
	}
}

func TestDynamicViewConsistency_StaticCoverage_UnparticipatingCoreComponentFails(t *testing.T) {
	s := dynamicBaseSystem(t)
	// Add a core Engine that appears in no dynamic view.
	orphan := comp(t, "OrphanEngine", kindEngine)
	s.Components = append(s.Components, orphan)
	sev, ok := findingSeverity(dynamicViewConsistency(s), ruleDVStaticCoverage)
	if !ok {
		t.Fatalf("expected DV-STATIC-COVERAGE for a core component in no dynamic view")
	}
	if sev != SeverityError {
		t.Fatalf("DV-STATIC-COVERAGE must be Error (founder requirement), got %v", sev)
	}
}

func TestDynamicViewConsistency_StaticCoverage_ResourceAndUtilityExempt(t *testing.T) {
	s := dynamicBaseSystem(t)
	// A Resource and a Utility that appear in no view must NOT trip DV-STATIC-COVERAGE.
	s.Components = append(s.Components,
		comp(t, "OrphanStore", kindResource),
		comp(t, "OrphanUtility", kindUtility))
	if hasRuleFindings(dynamicViewConsistency(s), ruleDVStaticCoverage) {
		t.Fatalf("Resources and Utilities are exempt from DV-STATIC-COVERAGE")
	}
}

func TestDynamicViewConsistency_StaticCoverage_NoViewsIsNoOp(t *testing.T) {
	s := dynamicBaseSystem(t)
	s.DynamicViews = nil
	if hasRuleFindings(dynamicViewConsistency(s), ruleDVStaticCoverage) {
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
	sev, ok := findingSeverity(dynamicViewConsistency(s), ruleDVRelCoverage)
	if !ok {
		t.Fatalf("expected DV-REL-COVERAGE for a sync relationship in no view edge")
	}
	if sev != SeverityWarning {
		t.Fatalf("DV-REL-COVERAGE must be Warning, got %v", sev)
	}
}

// TestDynamicViewConsistency_PartUsed_NoLongerConstructible documents a KNOWN,
// DELIBERATE regression introduced by this task's minimal step-keyed compile shim
// (2026-07-30 callchain-realization Task 3): DV-PART-USED (checkParticipantsUsed)
// used to fire for a participant DECLARED on the view's (independent) Participants
// list that no edge touched. The step-keyed DynamicView carries no independent
// participant list — participantIDs derives participation FROM the calls — so a
// "participant no call touches" can no longer exist: whatever participantIDs
// returns is, by construction, already the "used" set checkParticipantsUsed computes
// from the same calls. DV-PART-USED is consequently unreachable (dead) until Task 5's
// real per-rule retargeting gives participant identity an independent source again
// (e.g. via CallStep.ActivityNodeID / the activity diagram). This test is INVERTED
// on purpose — it pins the current (temporary) no-finding behavior so a future
// accidental revival of the check is visible in the diff — rather than deleting
// coverage silently.
func TestDynamicViewConsistency_PartUsed_NoLongerConstructible(t *testing.T) {
	s := dynamicBaseSystem(t)
	extra := comp(t, "LonelyEngine", kindEngine)
	s.Components = append(s.Components, extra)
	// Even a self-loop touching extra makes it "used" — there is no way, via the calls
	// alone, to declare extra as a participant while leaving it untouched.
	s.DynamicViews[0].Steps = append(s.DynamicViews[0].Steps, CallStep{
		Calls: []Relationship{{From: extra.ID, To: extra.ID, Mode: modeSync}},
	})
	if hasRuleFindings(dynamicViewConsistency(s), ruleDVPartUsed) {
		t.Fatalf("DV-PART-USED is expected to be unreachable under the Task-3 derived model; got a finding — see Task 5 retargeting")
	}
}

func TestEdgeLegality_MatchesSysLegalityForCallingUp(t *testing.T) {
	mgr := comp(t, "DesignManager", kindManager)
	ra := comp(t, "StateAccess", kindResourceAccess)
	if !hasRuleFindings(edgeLegality(ra, mgr, modeSync, loc(1, "Relationship RA→Mgr")), ruleSysNoUp) {
		t.Fatalf("edgeLegality must emit SYS-NOUP for a calling-up edge")
	}
}
