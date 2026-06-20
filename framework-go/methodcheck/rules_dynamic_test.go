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
		UseCaseID:    nid(),
		Key:          "uc1-core-flow",
		Title:        "Core flow",
		Participants: []string{client.ID, mgr.ID, eng.ID, ra.ID, store.ID},
		Edges: []Relationship{
			{From: client.ID, To: mgr.ID, Mode: modeSync},
			{From: mgr.ID, To: eng.ID, Mode: modeSync},
			{From: mgr.ID, To: ra.ID, Mode: modeSync},
			{From: ra.ID, To: store.ID, Mode: modeSync},
		},
	}
	return System{Components: []Component{client, mgr, eng, ra, store}, Relationships: rels, DynamicViews: []DynamicView{dv}}
}

func TestDynamicViewConsistency_ValidBaseHasNoFindings(t *testing.T) {
	if f := dynamicViewConsistency(dynamicBaseSystem(t)); len(f) != 0 {
		t.Fatalf("a fully legal dynamic view must produce zero findings, got %+v", f)
	}
}

func TestDynamicViewConsistency_PartExist(t *testing.T) {
	s := dynamicBaseSystem(t)
	s.DynamicViews[0].Participants = append(s.DynamicViews[0].Participants, nid())
	if !hasRuleFindings(dynamicViewConsistency(s), ruleDVPartExist) {
		t.Fatalf("expected DV-PART-EXIST")
	}
}

func TestDynamicViewConsistency_EdgeEnds_NotAParticipant(t *testing.T) {
	s := dynamicBaseSystem(t)
	mgrID := s.Components[1].ID
	var trimmed []string
	for _, p := range s.DynamicViews[0].Participants {
		if p != mgrID {
			trimmed = append(trimmed, p)
		}
	}
	s.DynamicViews[0].Participants = trimmed
	if !hasRuleFindings(dynamicViewConsistency(s), ruleDVEdgeEnds) {
		t.Fatalf("expected DV-EDGE-ENDS")
	}
}

func TestDynamicViewConsistency_EdgeInModel(t *testing.T) {
	s := dynamicBaseSystem(t)
	clientID := s.Components[0].ID
	engID := s.Components[2].ID
	s.DynamicViews[0].Edges = append(s.DynamicViews[0].Edges, Relationship{From: clientID, To: engID, Mode: modeSync})
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
			UseCaseID: nid(), Key: "uc-up", Participants: []string{mgr.ID, ra.ID}, Edges: []Relationship{rel},
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
			UseCaseID: nid(), Key: "uc-two-mgrs", Participants: []string{client.ID, m1.ID, m2.ID}, Edges: []Relationship{r1, r2},
		}},
	}
	if !hasRuleFindings(dynamicViewConsistency(s), ruleDVSingleMgr) {
		t.Fatalf("expected DV-SINGLE-MGR")
	}
}

func TestDynamicViewConsistency_Mode(t *testing.T) {
	s := dynamicBaseSystem(t)
	s.DynamicViews[0].Edges[0].Mode = modeEventPubSub
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

func TestEdgeLegality_MatchesSysLegalityForCallingUp(t *testing.T) {
	mgr := comp(t, "DesignManager", kindManager)
	ra := comp(t, "StateAccess", kindResourceAccess)
	if !hasRuleFindings(edgeLegality(ra, mgr, modeSync, loc(1, "Relationship RA→Mgr")), ruleSysNoUp) {
		t.Fatalf("edgeLegality must emit SYS-NOUP for a calling-up edge")
	}
}
