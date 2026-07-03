package methodcheck

import (
	"fmt"
	"testing"
)

// ---- interaction don'ts (§3.6) ----

func TestAppCDont_ClientCallsMultipleManagers_Fails(t *testing.T) {
	cli := comp(t, "WebClient", kindClient)
	m1 := comp(t, "DesignManager", kindManager)
	m2 := comp(t, "BuildManager", kindManager)
	dv := DynamicView{
		UseCaseID:    "uc1",
		Participants: []string{cli.ID, m1.ID, m2.ID},
		Edges: []Relationship{
			{From: cli.ID, To: m1.ID, Mode: modeSync},
			{From: cli.ID, To: m2.ID, Mode: modeSync},
		},
	}
	s := System{Components: []Component{cli, m1, m2}, DynamicViews: []DynamicView{dv}}
	findings := appCInteractionDonts(s)
	if !hasRuleFindings(findings, ruleAppcDontClientMultiMgr) {
		t.Fatalf("expected APPC-INT-CLIENT-MULTI-MGR, got %+v", findings)
	}
	sev, _ := findingSeverity(findings, ruleAppcDontClientMultiMgr)
	if sev != SeverityError {
		t.Fatalf("directive APPC-INT-CLIENT-MULTI-MGR must be SeverityError, got %v", sev)
	}
}

func TestAppCDont_EngineReceivesQueuedCall_Fails(t *testing.T) {
	m := comp(t, "DesignManager", kindManager)
	e := comp(t, "ValidatingEngine", kindEngine)
	s := System{
		Components:    []Component{m, e},
		Relationships: []Relationship{{From: m.ID, To: e.ID, Mode: modeQueued}},
	}
	findings := appCInteractionDonts(s)
	if !hasRuleFindings(findings, ruleAppcDontEngineQueue) {
		t.Fatalf("expected APPC-INT-ENGINE-NO-QUEUE, got %+v", findings)
	}
	sev, _ := findingSeverity(findings, ruleAppcDontEngineQueue)
	if sev != SeverityError {
		t.Fatalf("must be SeverityError, got %v", sev)
	}
}

func TestAppCDont_RAPublishesEvent_Fails(t *testing.T) {
	ra := comp(t, "StateAccess", kindResourceAccess)
	m := comp(t, "DesignManager", kindManager)
	s := System{
		Components:    []Component{ra, m},
		Relationships: []Relationship{{From: ra.ID, To: m.ID, Mode: modeEventPubSub}},
	}
	findings := appCInteractionDonts(s)
	if !hasRuleFindings(findings, ruleAppcDontRAPub) {
		t.Fatalf("expected APPC-INT-RA-NO-PUB, got %+v", findings)
	}
}

func TestAppCDont_ResourcePublishesEvent_Fails(t *testing.T) {
	res := comp(t, "StateDB", kindResource)
	m := comp(t, "DesignManager", kindManager)
	s := System{
		Components:    []Component{res, m},
		Relationships: []Relationship{{From: res.ID, To: m.ID, Mode: modeEventPubSub}},
	}
	findings := appCInteractionDonts(s)
	if !hasRuleFindings(findings, ruleAppcDontResourcePub) {
		t.Fatalf("expected APPC-INT-RESOURCE-NO-PUB, got %+v", findings)
	}
}

func TestAppCDont_EngineSubscribes_Fails(t *testing.T) {
	m := comp(t, "DesignManager", kindManager)
	e := comp(t, "ValidatingEngine", kindEngine)
	s := System{
		Components:    []Component{m, e},
		Relationships: []Relationship{{From: m.ID, To: e.ID, Mode: modeEventPubSub}},
	}
	findings := appCInteractionDonts(s)
	if !hasRuleFindings(findings, ruleAppcDontNonMgrSub) {
		t.Fatalf("expected APPC-INT-NONMGR-NO-SUB, got %+v", findings)
	}
}

func TestAppCDont_ManagerQueuesMultipleManagers_Fails(t *testing.T) {
	m0 := comp(t, "OrchestratorManager", kindManager)
	m1 := comp(t, "BillingManager", kindManager)
	m2 := comp(t, "OpsManager", kindManager)
	dv := DynamicView{
		UseCaseID:    "uc1",
		Participants: []string{m0.ID, m1.ID, m2.ID},
		Edges: []Relationship{
			{From: m0.ID, To: m1.ID, Mode: modeQueued},
			{From: m0.ID, To: m2.ID, Mode: modeQueued},
		},
	}
	s := System{Components: []Component{m0, m1, m2}, DynamicViews: []DynamicView{dv}}
	findings := appCInteractionDonts(s)
	if !hasRuleFindings(findings, ruleAppcDontMgrMultiQueue) {
		t.Fatalf("expected APPC-INT-MGR-MULTI-QUEUE, got %+v", findings)
	}
	sev, _ := findingSeverity(findings, ruleAppcDontMgrMultiQueue)
	if sev != SeverityError {
		t.Fatalf("directive APPC-INT-MGR-MULTI-QUEUE must be SeverityError, got %v", sev)
	}
}

func TestAppCDont_ManagerQueuesSingleManager_OK(t *testing.T) {
	m0 := comp(t, "OrchestratorManager", kindManager)
	m1 := comp(t, "BillingManager", kindManager)
	dv := DynamicView{
		UseCaseID:    "uc1",
		Participants: []string{m0.ID, m1.ID},
		Edges:        []Relationship{{From: m0.ID, To: m1.ID, Mode: modeQueued}},
	}
	s := System{Components: []Component{m0, m1}, DynamicViews: []DynamicView{dv}}
	if hasRuleFindings(appCInteractionDonts(s), ruleAppcDontMgrMultiQueue) {
		t.Fatalf("a single queued Manager→Manager target is legal; APPC-INT-MGR-MULTI-QUEUE must not fire")
	}
}

// ---- open/semi-open arch (§3.4) ----

func TestAppCClosedArch_OpenArch_Warned(t *testing.T) {
	ra1 := comp(t, "StateAccess", kindResourceAccess)
	ra2 := comp(t, "ArtifactAccess", kindResourceAccess)
	s := System{
		Components:    []Component{ra1, ra2},
		Relationships: []Relationship{{From: ra1.ID, To: ra2.ID, Mode: modeSync}},
	}
	findings := appCClosedArch(s)
	if !hasRuleFindings(findings, ruleAppcArchSemiOpen) {
		t.Fatalf("expected APPC-ARCH-SEMI-OPEN for same-rank RA→RA, got %+v", findings)
	}
	sev, _ := findingSeverity(findings, ruleAppcArchSemiOpen)
	if sev != SeverityWarning {
		t.Fatalf("APPC-ARCH-SEMI-OPEN is a guideline; must be Warning, got %v", sev)
	}
}

// ---- waiver downgrade ----

func TestApplyWaivers_GuidelineWaivedBecomesInfo(t *testing.T) {
	f := Finding{
		RuleID:   ruleAppcArchOpen,
		Severity: SeverityWarning,
		Message:  "open architecture detected",
	}
	sc := StandardCheck{Items: []CheckItem{
		{Section: "SYS-4a", Guideline: "avoid open architecture", Status: checkWaived, Justification: "deliberate: RA-to-RA is the validated design for this subsystem"},
	}}
	got := applyWaivers([]Finding{f}, sc)
	if len(got) != 1 {
		t.Fatalf("expected 1 finding after waiver, got %d", len(got))
	}
	if got[0].Severity != SeverityInfo {
		t.Fatalf("waived guideline must become SeverityInfo, got %v", got[0].Severity)
	}
}

func TestApplyWaivers_DirectiveNotDowngraded(t *testing.T) {
	f := Finding{
		RuleID:   ruleAppcSvcReject20,
		Severity: SeverityError,
		Message:  "contract has ≥20 ops",
	}
	sc := StandardCheck{Items: []CheckItem{
		{Section: "SVC-2d", Guideline: "reject ≥20 ops", Status: checkWaived, Justification: "we know best"},
	}}
	got := applyWaivers([]Finding{f}, sc)
	if got[0].Severity != SeverityError {
		t.Fatalf("directive must NOT be downgraded by waiver; got %v", got[0].Severity)
	}
}

// ---- §6 service contract op-count ----

func TestAppCSvcContract_SingleOpWarns(t *testing.T) {
	mgr := comp(t, "BuildManager", kindManager)
	mgr.AtomicBusinessVerbs = []string{"executeNextActivity"}
	s := System{Components: []Component{mgr}}
	findings := appCServiceContract(s)
	if !hasRuleFindings(findings, ruleAppcSvcSingle) {
		t.Fatalf("expected APPC-SVC-SINGLE for 1-op contract, got %+v", findings)
	}
	sev, _ := findingSeverity(findings, ruleAppcSvcSingle)
	if sev != SeverityWarning {
		t.Fatalf("APPC-SVC-SINGLE must be Warning, got %v", sev)
	}
}

func TestAppCSvcContract_TwelveOpsWarns(t *testing.T) {
	ra := comp(t, "StateAccess", kindResourceAccess)
	ra.AtomicBusinessVerbs = make([]string, 13)
	for i := range ra.AtomicBusinessVerbs {
		ra.AtomicBusinessVerbs[i] = fmt.Sprintf("op%d", i)
	}
	s := System{Components: []Component{ra}}
	findings := appCServiceContract(s)
	if !hasRuleFindings(findings, ruleAppcSvcAvoid12) {
		t.Fatalf("expected APPC-SVC-AVOID-12 for 13-op contract, got %+v", findings)
	}
	sev, _ := findingSeverity(findings, ruleAppcSvcAvoid12)
	if sev != SeverityWarning {
		t.Fatalf("APPC-SVC-AVOID-12 must be Warning, got %v", sev)
	}
}

func TestAppCSvcContract_TwentyOpsError(t *testing.T) {
	mgr := comp(t, "GodManager", kindManager)
	mgr.AtomicBusinessVerbs = make([]string, 20)
	for i := range mgr.AtomicBusinessVerbs {
		mgr.AtomicBusinessVerbs[i] = fmt.Sprintf("op%d", i)
	}
	s := System{Components: []Component{mgr}}
	findings := appCServiceContract(s)
	if !hasRuleFindings(findings, ruleAppcSvcReject20) {
		t.Fatalf("expected APPC-SVC-REJECT-20 for 20-op contract, got %+v", findings)
	}
	sev, _ := findingSeverity(findings, ruleAppcSvcReject20)
	if sev != SeverityError {
		t.Fatalf("APPC-SVC-REJECT-20 is a directive; must be Error, got %v", sev)
	}
}

func TestAppCSvcContract_FiveOpsNoFinding(t *testing.T) {
	mgr := comp(t, "DesignManager", kindManager)
	mgr.AtomicBusinessVerbs = []string{"op1", "op2", "op3", "op4", "op5"}
	s := System{Components: []Component{mgr}}
	findings := appCServiceContract(s)
	for _, f := range findings {
		if f.RuleID == ruleAppcSvcSingle || f.RuleID == ruleAppcSvcAvoid12 || f.RuleID == ruleAppcSvcReject20 {
			t.Fatalf("5-op contract must not trip §6 count rules, got %+v", findings)
		}
	}
}

func TestAppCSvcContract_StriveIsInfoDefaultArm(t *testing.T) {
	mgr := comp(t, "DesignManager", kindManager)
	mgr.AtomicBusinessVerbs = []string{"op1", "op2", "op3", "op4"} // 4 ops → default arm
	s := System{Components: []Component{mgr}}
	findings := appCServiceContract(s)
	sev, ok := findingSeverity(findings, ruleAppcSvcStrive)
	if !ok {
		t.Fatalf("expected APPC-SVC-STRIVE (default arm) for an in-range contract, got %+v", findings)
	}
	if sev != SeverityInfo {
		t.Fatalf("APPC-SVC-STRIVE must be Info, got %v", sev)
	}
}

func TestAppCSvcContract_UtilityExempt(t *testing.T) {
	util := comp(t, "LoggingUtility", kindUtility)
	util.AtomicBusinessVerbs = make([]string, 25)
	s := System{Components: []Component{util}}
	findings := appCServiceContract(s)
	for _, f := range findings {
		if f.RuleID == ruleAppcSvcReject20 {
			t.Fatalf("Utility must be exempt from §6 op count, got %+v", findings)
		}
	}
}
