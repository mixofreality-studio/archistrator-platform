package methodcheck

import (
	"errors"
	"fmt"
	"sort"
	"sync/atomic"
	"testing"
)

// rules_test.go PORTS the aiarch artifactValidationEngine predicate tests, adapted
// to the structural structs in project.go. Each test proves rule equivalence to the
// original: same rule IDs, same severities, same pass/fail verdicts. The original
// test names are preserved (sans the t prefix change) so the port is traceable.

// ---- helpers (mirror the original test helpers) ----

var nidCounter atomic.Uint64

func nid() string { return fmt.Sprintf("n%d", nidCounter.Add(1)) }

func hasRule(res ValidationResult, id RuleID) bool { return hasRuleFindings(res.Findings, id) }

func hasRuleFindings(findings []Finding, id RuleID) bool {
	for _, f := range findings {
		if f.RuleID == id {
			return true
		}
	}
	return false
}

func severityOf(res ValidationResult, id RuleID) (Severity, bool) {
	return findingSeverity(res.Findings, id)
}

func findingSeverity(findings []Finding, id RuleID) (Severity, bool) {
	for _, f := range findings {
		if f.RuleID == id {
			return f.Severity, true
		}
	}
	return 0, false
}

func assertContractMisuse(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected a ContractMisuse error, got nil")
	}
	var e *ContractMisuseError
	if !errors.As(err, &e) {
		t.Fatalf("expected *ContractMisuseError, got %T: %v", err, err)
	}
}

// comp builds a Component with the canonical layer for its kind (mirrors the
// original test's comp helper).
func comp(t *testing.T, name, kind string) Component {
	t.Helper()
	var layer string
	switch kind {
	case kindClient:
		layer = layerClient
	case kindManager:
		layer = layerManager
	case kindEngine:
		layer = layerEngine
	case kindResourceAccess:
		layer = layerResourceAccess
	case kindResource:
		layer = layerResource
	case kindUtility:
		layer = layerUtility
	default:
		t.Fatalf("comp: unhandled kind %v", kind)
	}
	return Component{ID: Slug(name), Name: name, Kind: kind, Layer: layer}
}

// ---- ValidateVolatilities ----

func TestValidateVolatilities_Pass(t *testing.T) {
	g := Glossary{Items: []GlossaryItem{
		{Term: "Scheduling", Definition: "the scheduling concept"},
		{Term: "Pricing", Definition: "the pricing concept"},
	}}
	sr := ScrubbedRequirements{Items: []Requirement{
		{ID: "R1", Statement: "scheduling must adapt over time"},
		{ID: "R2", Statement: "pricing must vary across customers"},
	}}
	v := Volatilities{Items: []Volatility{
		{Name: "Scheduling policy", Rationale: "scheduling rules change over time", Axis: axisSameCustomerOverTime},
		{Name: "Pricing model", Rationale: "pricing differs across customers", Axis: axisAllCustomersAtOneTime},
	}}
	res, err := validateVolatilities(v, g, sr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Verdict != VerdictPass {
		t.Fatalf("expected VerdictPass, got %v (%+v)", res.Verdict, res.Findings)
	}
}

func TestValidateVolatilities_UntracedFails(t *testing.T) {
	g := Glossary{Items: []GlossaryItem{{Term: "Scheduling", Definition: "x"}}}
	sr := ScrubbedRequirements{Items: []Requirement{{ID: "R1", Statement: "scheduling adapts"}}}
	v := Volatilities{Items: []Volatility{
		{Name: "Scheduling policy", Rationale: "scheduling changes", Axis: axisSameCustomerOverTime},
		{Name: "Telemetry exporter", Rationale: "exporter wire format churns", Axis: axisAllCustomersAtOneTime},
	}}
	res, err := validateVolatilities(v, g, sr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Verdict != VerdictFail {
		t.Fatalf("expected VerdictFail, got %v", res.Verdict)
	}
	if !hasRule(res, ruleVolTrace) {
		t.Fatalf("expected VOL-TRACE finding, got %+v", res.Findings)
	}
}

func TestValidateVolatilities_SingleAxisFails(t *testing.T) {
	g := Glossary{Items: []GlossaryItem{{Term: "Scheduling", Definition: "x"}, {Term: "Pricing", Definition: "y"}}}
	sr := ScrubbedRequirements{Items: []Requirement{{ID: "R1", Statement: "scheduling pricing"}}}
	v := Volatilities{Items: []Volatility{
		{Name: "Scheduling policy", Rationale: "scheduling", Axis: axisSameCustomerOverTime},
		{Name: "Pricing model", Rationale: "pricing", Axis: axisSameCustomerOverTime},
	}}
	res, _ := validateVolatilities(v, g, sr)
	if !hasRule(res, ruleVolAxis) {
		t.Fatalf("expected VOL-AXIS finding, got %+v", res.Findings)
	}
	if res.Verdict != VerdictFail {
		t.Fatalf("expected VerdictFail, got %v", res.Verdict)
	}
}

func TestValidateVolatilities_NatureOfBusinessIsWarningNotFail(t *testing.T) {
	g := Glossary{Items: []GlossaryItem{{Term: "Scheduling", Definition: "x"}, {Term: "Pricing", Definition: "y"}}}
	sr := ScrubbedRequirements{Items: []Requirement{{ID: "R1", Statement: "scheduling pricing future"}}}
	v := Volatilities{Items: []Volatility{
		{Name: "Scheduling policy", Rationale: "scheduling rules change", Axis: axisSameCustomerOverTime},
		{Name: "Pricing model", Rationale: "pricing might change in the future", Axis: axisAllCustomersAtOneTime},
	}}
	res, _ := validateVolatilities(v, g, sr)
	sev, ok := severityOf(res, ruleVolNOB)
	if !ok {
		t.Fatalf("expected VOL-NOB finding, got %+v", res.Findings)
	}
	if sev != SeverityWarning {
		t.Fatalf("VOL-NOB must be Warning, got %v", sev)
	}
	if res.Verdict != VerdictPass {
		t.Fatalf("a Warning must not fail the verdict; got %v", res.Verdict)
	}
}

func TestValidateVolatilities_WiringBugIsContractMisuse(t *testing.T) {
	v := Volatilities{Items: []Volatility{{Name: "X", Rationale: "y", Axis: axisSameCustomerOverTime}}}
	_, err := validateVolatilities(v, Glossary{}, ScrubbedRequirements{})
	assertContractMisuse(t, err)
}

func TestValidateVolatilities_GlossaryMissFails(t *testing.T) {
	g := Glossary{Items: []GlossaryItem{{Term: "Authentication", Definition: "verifying identity"}}}
	sr := ScrubbedRequirements{Items: []Requirement{{ID: "R1", Statement: "authentication must support SSO"}}}
	v := Volatilities{Items: []Volatility{
		{Name: "Rendering pipeline", Rationale: "authentication rendering differs across environments", Axis: axisSameCustomerOverTime},
		{Name: "Authentication provider", Rationale: "authentication mechanism changes over time", Axis: axisAllCustomersAtOneTime},
	}}
	res, err := validateVolatilities(v, g, sr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sev, ok := severityOf(res, ruleVolGloss)
	if !ok {
		t.Fatalf("expected VOL-GLOSS finding, got %+v", res.Findings)
	}
	if sev != SeverityError {
		t.Fatalf("VOL-GLOSS must be SeverityError, got %v", sev)
	}
	if res.Verdict != VerdictFail {
		t.Fatalf("expected VerdictFail, got %v", res.Verdict)
	}
}

// ---- ValidateCoreUseCases ----

func coreUC(name string) UseCaseDecision {
	return UseCaseDecision{UseCase: UseCase{ID: Slug(name), Name: name, Classification: classCore}}
}

func TestValidateCoreUseCases_Pass(t *testing.T) {
	c := CoreUseCases{Decisions: []UseCaseDecision{
		coreUC("Co-author artifact"), coreUC("Validate artifact"), coreUC("Render artifact"),
	}}
	res, err := validateCoreUseCases(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Verdict != VerdictPass {
		t.Fatalf("expected VerdictPass, got %v (%+v)", res.Verdict, res.Findings)
	}
}

func TestValidateCoreUseCases_SevenCoreTripsCardinality(t *testing.T) {
	var ds []UseCaseDecision
	for i := 0; i < 7; i++ {
		ds = append(ds, coreUC(fmt.Sprintf("uc%d", i)))
	}
	res, _ := validateCoreUseCases(CoreUseCases{Decisions: ds})
	if !hasRule(res, ruleCucCard) {
		t.Fatalf("expected CUC-CARD finding, got %+v", res.Findings)
	}
	if sev, _ := severityOf(res, ruleCucCard); sev != SeverityError {
		t.Fatalf("CUC-CARD must be Error")
	}
	if res.Verdict != VerdictFail {
		t.Fatalf("expected VerdictFail, got %v", res.Verdict)
	}
}

func TestValidateCoreUseCases_OneCoreTripsCardinality(t *testing.T) {
	res, _ := validateCoreUseCases(CoreUseCases{Decisions: []UseCaseDecision{coreUC("only one")}})
	if !hasRule(res, ruleCucCard) {
		t.Fatalf("expected CUC-CARD finding for <2 core, got %+v", res.Findings)
	}
}

func TestValidateCoreUseCases_IncompleteDecisionDiagram(t *testing.T) {
	decisionID := nid()
	uc := coreUC("Decide")
	uc.UseCase.Activity = &ActivityDiagram{
		Nodes: []ActivityNode{
			{ID: nid(), Kind: "start"},
			{ID: decisionID, Kind: nodeDecision, Label: "is valid?"},
		},
	}
	c := CoreUseCases{Decisions: []UseCaseDecision{uc, coreUC("Other")}}
	res, _ := validateCoreUseCases(c)
	if !hasRule(res, ruleUcActDiagram) {
		t.Fatalf("expected UC-ACTDIAG finding, got %+v", res.Findings)
	}
	if res.Verdict != VerdictFail {
		t.Fatalf("expected VerdictFail, got %v", res.Verdict)
	}
}

func TestValidateCoreUseCases_WellFormedIfElsePasses(t *testing.T) {
	dID, a1, a2, m := nid(), nid(), nid(), nid()
	uc := coreUC("Decide")
	uc.UseCase.Activity = &ActivityDiagram{
		Nodes: []ActivityNode{
			{ID: dID, Kind: nodeDecision, Label: "actionable?"},
			{ID: a1, Kind: "action", Label: "create next step"},
			{ID: a2, Kind: "action", Label: "file or incubate"},
			{ID: m, Kind: nodeMerge},
		},
		Edges: []ActivityEdge{
			{From: dID, To: a1, Kind: edgeGuardedFlow, Guard: "[actionable]"},
			{From: dID, To: a2, Kind: edgeGuardedFlow, Guard: "[else]"},
			{From: a1, To: m, Kind: edgeControlFlow},
			{From: a2, To: m, Kind: edgeControlFlow},
		},
	}
	c := CoreUseCases{Decisions: []UseCaseDecision{uc, coreUC("Other")}}
	res, _ := validateCoreUseCases(c)
	if hasRule(res, ruleUcActDiagram) {
		t.Fatalf("a well-formed if/else must NOT trip UC-ACTDIAG, got %+v", res.Findings)
	}
}

func TestValidateCoreUseCases_OneArmedDecisionFails(t *testing.T) {
	dID, a := nid(), nid()
	uc := coreUC("Decide")
	uc.UseCase.Activity = &ActivityDiagram{
		Nodes: []ActivityNode{
			{ID: dID, Kind: nodeDecision, Label: "actionable?"},
			{ID: a, Kind: "action", Label: "create next step"},
		},
		Edges: []ActivityEdge{{From: dID, To: a, Kind: edgeGuardedFlow, Guard: "[actionable]"}},
	}
	c := CoreUseCases{Decisions: []UseCaseDecision{uc, coreUC("Other")}}
	res, _ := validateCoreUseCases(c)
	if !hasRule(res, ruleUcActDiagram) {
		t.Fatalf("a one-armed decision must trip UC-ACTDIAG, got %+v", res.Findings)
	}
}

func TestValidateCoreUseCases_GuardedEdgeFromNonDecisionFails(t *testing.T) {
	a1, a2 := nid(), nid()
	uc := coreUC("Flow")
	uc.UseCase.Activity = &ActivityDiagram{
		Nodes: []ActivityNode{
			{ID: a1, Kind: "action", Label: "step one"},
			{ID: a2, Kind: "action", Label: "step two"},
		},
		Edges: []ActivityEdge{{From: a1, To: a2, Kind: edgeGuardedFlow, Guard: "[whatever]"}},
	}
	c := CoreUseCases{Decisions: []UseCaseDecision{uc, coreUC("Other")}}
	res, _ := validateCoreUseCases(c)
	if !hasRule(res, ruleUcActDiagram) {
		t.Fatalf("a guarded edge from a non-decision node must trip UC-ACTDIAG, got %+v", res.Findings)
	}
}

func TestValidateCoreUseCases_DuplicateUseCaseNameFails(t *testing.T) {
	c := CoreUseCases{Decisions: []UseCaseDecision{
		coreUC("Co-author artifact"), coreUC("Co-author artifact"), coreUC("Render artifact"),
	}}
	res, _ := validateCoreUseCases(c)
	if !hasRule(res, ruleCucNameUniq) {
		t.Fatalf("expected CUC-NAME-UNIQUE, got %+v", res.Findings)
	}
	if res.Verdict != VerdictFail {
		t.Fatalf("duplicate use-case name must fail the verdict")
	}
}

func TestValidateCoreUseCases_DuplicateActorRoleWithinUseCaseFails(t *testing.T) {
	uc := coreUC("Co-author artifact")
	uc.UseCase.Actors = []Actor{
		{ID: Slug("Architect"), Role: "Architect"},
		{ID: Slug("Architect"), Role: "Architect"},
	}
	c := CoreUseCases{Decisions: []UseCaseDecision{uc, coreUC("Other")}}
	res, _ := validateCoreUseCases(c)
	if !hasRule(res, ruleCucActorUniq) {
		t.Fatalf("expected CUC-ACTOR-UNIQUE, got %+v", res.Findings)
	}
}

func TestValidateCoreUseCases_DuplicateActivityNodeIDFails(t *testing.T) {
	uc := coreUC("Co-author artifact")
	uc.UseCase.Activity = &ActivityDiagram{Nodes: []ActivityNode{
		{ID: "n1", Kind: "start"},
		{ID: "n1", Kind: "action", Label: "dup"},
	}}
	c := CoreUseCases{Decisions: []UseCaseDecision{uc, coreUC("Other")}}
	res, _ := validateCoreUseCases(c)
	if !hasRule(res, ruleUcNodeIDUniq) {
		t.Fatalf("expected UC-NODE-UNIQUE, got %+v", res.Findings)
	}
}

func TestValidateCoreUseCases_UniqueNamesAndNodesPass(t *testing.T) {
	uc := coreUC("Co-author artifact")
	uc.UseCase.Actors = []Actor{{ID: Slug("Architect"), Role: "Architect"}}
	c := CoreUseCases{Decisions: []UseCaseDecision{uc, coreUC("Render artifact")}}
	res, _ := validateCoreUseCases(c)
	if hasRule(res, ruleCucNameUniq) || hasRule(res, ruleCucActorUniq) || hasRule(res, ruleUcNodeIDUniq) {
		t.Fatalf("unique names/actors/nodes must not trip uniqueness rules, got %+v", res.Findings)
	}
}

// ---- ValidateArchitecture ----

func passingSystem(t *testing.T, ucID string) System {
	t.Helper()
	client := comp(t, "AppClient", kindClient)
	mgr := comp(t, "DesignManager", kindManager)
	eng := comp(t, "ValidatingEngine", kindEngine)
	ra := comp(t, "StateAccess", kindResourceAccess)
	res := comp(t, "StateDB", kindResource)
	rels := []Relationship{
		{From: client.ID, To: mgr.ID, Mode: modeSync},
		{From: mgr.ID, To: eng.ID, Mode: modeSync},
		{From: mgr.ID, To: ra.ID, Mode: modeSync},
		{From: ra.ID, To: res.ID, Mode: modeSync},
	}
	// The primary view exercises the full chain so every core component participates
	// (DV-STATIC-COVERAGE) and every sync relationship is covered (DV-REL-COVERAGE).
	dvs := []DynamicView{{
		UseCaseID:    ucID,
		Key:          "uc1",
		Title:        "Core flow",
		Participants: []string{client.ID, mgr.ID, eng.ID, ra.ID, res.ID},
		Edges: []Relationship{
			{From: client.ID, To: mgr.ID, Mode: modeSync},
			{From: mgr.ID, To: eng.ID, Mode: modeSync},
			{From: mgr.ID, To: ra.ID, Mode: modeSync},
			{From: ra.ID, To: res.ID, Mode: modeSync},
		},
	}}
	return System{Components: []Component{client, mgr, eng, ra, res}, Relationships: rels, DynamicViews: dvs}
}

func TestValidateArchitecture_Pass(t *testing.T) {
	ucID := nid()
	s := passingSystem(t, ucID)
	c := CoreUseCases{Decisions: []UseCaseDecision{
		{UseCase: UseCase{ID: ucID, Name: "Core flow", Classification: classCore}},
		coreUC("Second"),
	}}
	s.DynamicViews = append(s.DynamicViews, DynamicView{UseCaseID: c.Decisions[1].UseCase.ID, Key: "uc2"})
	res, err := validateArchitecture(s, c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Verdict != VerdictPass {
		t.Fatalf("expected VerdictPass, got %v (%+v)", res.Verdict, res.Findings)
	}
}

func TestValidateArchitecture_CallingUpFails(t *testing.T) {
	mgr := comp(t, "DesignManager", kindManager)
	ra := comp(t, "StateAccess", kindResourceAccess)
	s := System{Components: []Component{mgr, ra}, Relationships: []Relationship{{From: ra.ID, To: mgr.ID, Mode: modeSync}}}
	res, _ := validateArchitecture(s, CoreUseCases{})
	if !hasRule(res, ruleSysNoUp) {
		t.Fatalf("expected SYS-NOUP, got %+v", res.Findings)
	}
	if res.Verdict != VerdictFail {
		t.Fatalf("expected VerdictFail")
	}
}

func TestValidateArchitecture_SyncManagerToManagerFails(t *testing.T) {
	m1 := comp(t, "AManager", kindManager)
	m2 := comp(t, "BManager", kindManager)
	s := System{Components: []Component{m1, m2}, Relationships: []Relationship{{From: m1.ID, To: m2.ID, Mode: modeSync}}}
	res, _ := validateArchitecture(s, CoreUseCases{})
	if !hasRule(res, ruleSysNoSide) {
		t.Fatalf("expected SYS-NOSIDE, got %+v", res.Findings)
	}
	if !hasRule(res, ruleSysDontMtoM) {
		t.Fatalf("expected SYS-DONT-MGR-SYNC-MGR, got %+v", res.Findings)
	}
}

func TestValidateArchitecture_QueuedManagerToManagerLegal(t *testing.T) {
	m1 := comp(t, "AManager", kindManager)
	m2 := comp(t, "BManager", kindManager)
	// A ResourceAccess keeps the system non-degenerate (SYSTEM-LAYER-DEGENERATE), so this
	// test isolates the queued-M→M legality it is about.
	ra := comp(t, "StateAccess", kindResourceAccess)
	s := System{Components: []Component{m1, m2, ra}, Relationships: []Relationship{{From: m1.ID, To: m2.ID, Mode: modeQueued}}}
	res, _ := validateArchitecture(s, CoreUseCases{})
	if hasRule(res, ruleSysNoSide) {
		t.Fatalf("queued M→M is legal; should not trip SYS-NOSIDE: %+v", res.Findings)
	}
	if res.Verdict != VerdictPass {
		t.Fatalf("queued M→M should pass, got %v (%+v)", res.Verdict, res.Findings)
	}
}

func TestValidateArchitecture_LayerSkipFails(t *testing.T) {
	mgr := comp(t, "DesignManager", kindManager)
	res := comp(t, "DB", kindResource)
	s := System{Components: []Component{mgr, res}, Relationships: []Relationship{{From: mgr.ID, To: res.ID, Mode: modeSync}}}
	out, _ := validateArchitecture(s, CoreUseCases{})
	if !hasRule(out, ruleSysNoSkip) {
		t.Fatalf("expected SYS-NOSKIP, got %+v", out.Findings)
	}
}

func TestValidateArchitecture_PubSubOriginAndDestFail(t *testing.T) {
	eng := comp(t, "PublishingEngine", kindEngine)
	ra := comp(t, "StoreAccess", kindResourceAccess)
	s := System{Components: []Component{eng, ra}, Relationships: []Relationship{{From: eng.ID, To: ra.ID, Mode: modeEventPubSub}}}
	out, _ := validateArchitecture(s, CoreUseCases{})
	if !hasRule(out, ruleSysPubOrig) {
		t.Fatalf("expected SYS-PUBORIG, got %+v", out.Findings)
	}
	if !hasRule(out, ruleSysPubDest) {
		t.Fatalf("expected SYS-PUBDEST, got %+v", out.Findings)
	}
}

func TestValidateArchitecture_ClientSkipFails(t *testing.T) {
	client := comp(t, "AppClient", kindClient)
	eng := comp(t, "SomeEngine", kindEngine)
	s := System{Components: []Component{client, eng}, Relationships: []Relationship{{From: client.ID, To: eng.ID, Mode: modeSync}}}
	out, _ := validateArchitecture(s, CoreUseCases{})
	if !hasRule(out, ruleSysDontCli) {
		t.Fatalf("expected SYS-DONT-CLIENT-SKIP, got %+v", out.Findings)
	}
}

func TestValidateArchitecture_TooManyManagersFails(t *testing.T) {
	var comps []Component
	for i := 0; i < 6; i++ {
		comps = append(comps, comp(t, fmt.Sprintf("Mgr%d", i), kindManager))
	}
	out, _ := validateArchitecture(System{Components: comps}, CoreUseCases{})
	if !hasRule(out, ruleSysCardMgr) {
		t.Fatalf("expected SYS-CARD-MGR, got %+v", out.Findings)
	}
	if sev, _ := severityOf(out, ruleSysCardMgr); sev != SeverityError {
		t.Fatalf("SYS-CARD-MGR must be Error")
	}
	if out.Verdict != VerdictFail {
		t.Fatalf("expected VerdictFail")
	}
}

func TestValidateArchitecture_GoldenRatioIsWarningNotFail(t *testing.T) {
	comps := []Component{
		comp(t, "OnlyManager", kindManager),
		comp(t, "EngineA", kindEngine),
		comp(t, "EngineB", kindEngine),
		comp(t, "EngineC", kindEngine),
		// A ResourceAccess keeps the system non-degenerate so the golden-ratio Warning is
		// the only rule under test.
		comp(t, "StateAccess", kindResourceAccess),
	}
	out, _ := validateArchitecture(System{Components: comps}, CoreUseCases{})
	sev, ok := severityOf(out, ruleSysCardRatio)
	if !ok {
		t.Fatalf("expected SYS-CARD-RATIO finding, got %+v", out.Findings)
	}
	if sev != SeverityWarning {
		t.Fatalf("SYS-CARD-RATIO must be Warning, got %v", sev)
	}
	if out.Verdict != VerdictPass {
		t.Fatalf("golden-ratio Warning must NOT fail the verdict; got %v", out.Verdict)
	}
}

func TestValidateArchitecture_TotalComponentCountIsWarning(t *testing.T) {
	var comps []Component
	comps = append(comps, comp(t, "OnlyManager", kindManager))
	for i := 0; i < 4; i++ {
		comps = append(comps, comp(t, fmt.Sprintf("Engine%d", i), kindEngine))
	}
	for i := 0; i < 8; i++ {
		comps = append(comps, comp(t, fmt.Sprintf("StateAccess%d", i), kindResourceAccess))
	}
	for i := 0; i < 8; i++ {
		comps = append(comps, comp(t, fmt.Sprintf("StateDB%d", i), kindResource))
	}
	if len(comps) != 21 {
		t.Fatalf("test setup: expected 21 components, got %d", len(comps))
	}
	out, _ := validateArchitecture(System{Components: comps}, CoreUseCases{})
	sev, ok := severityOf(out, ruleSysCardTotal)
	if !ok {
		t.Fatalf("expected SYS-CARD-TOTAL finding, got %+v", out.Findings)
	}
	if sev != SeverityWarning {
		t.Fatalf("SYS-CARD-TOTAL must be SeverityWarning, got %v", sev)
	}
	if out.Verdict != VerdictPass {
		t.Fatalf("SYS-CARD-TOTAL Warning must NOT fail the verdict; got %v", out.Verdict)
	}
}

func TestValidateArchitecture_UtilityEdgesExemptFromLayerRules(t *testing.T) {
	util := comp(t, "LoggingUtility", kindUtility)
	resource := comp(t, "StateDB", kindResource)
	eng := comp(t, "ProcessEngine", kindEngine)
	client := comp(t, "WebClient", kindClient)
	// A Manager + ResourceAccess keep the system non-degenerate (SYSTEM-LAYER-DEGENERATE),
	// so this test isolates the Utility-edge layer exemption it is about.
	mgr := comp(t, "CoreManager", kindManager)
	ra := comp(t, "StateAccess", kindResourceAccess)
	s := System{
		Components: []Component{util, resource, eng, client, mgr, ra},
		Relationships: []Relationship{
			{From: resource.ID, To: util.ID, Mode: modeSync},
			{From: eng.ID, To: util.ID, Mode: modeSync},
			{From: client.ID, To: util.ID, Mode: modeSync},
		},
	}
	out, _ := validateArchitecture(s, CoreUseCases{})
	if hasRule(out, ruleSysNoUp) || hasRule(out, ruleSysNoSide) || hasRule(out, ruleSysNoSkip) {
		t.Fatalf("Utility edges must be exempt from layer rules: %+v", out.Findings)
	}
	if out.Verdict != VerdictPass {
		t.Fatalf("Utility edges must not fail the verdict; got %v", out.Verdict)
	}
}

func TestValidateArchitecture_ChainCoverageFails(t *testing.T) {
	ucID := nid()
	s := passingSystem(t, ucID)
	c := CoreUseCases{Decisions: []UseCaseDecision{
		{UseCase: UseCase{ID: nid(), Name: "Uncovered", Classification: classCore}},
		coreUC("Another"),
	}}
	out, _ := validateArchitecture(s, c)
	if !hasRule(out, ruleArchChainCov) {
		t.Fatalf("expected ARCH-CHAINCOV, got %+v", out.Findings)
	}
	if out.Verdict != VerdictFail {
		t.Fatalf("expected VerdictFail")
	}
}

// TestValidateArchitecture_UseCaseDynamicMissing_NonCoreVariation is the founder
// extension (2026-07-05): a nonCore use-case variation without its own dynamic view
// trips USECASE-DYNAMIC-MISSING even though ARCH-CHAINCOV (core-only) stays silent.
func TestValidateArchitecture_UseCaseDynamicMissing_NonCoreVariation(t *testing.T) {
	ucID := nid()
	s := passingSystem(t, ucID) // covers ucID (the core UC) only
	variationOf := ucID
	c := CoreUseCases{Decisions: []UseCaseDecision{
		{UseCase: UseCase{ID: ucID, Name: "Core flow", Classification: classCore}},
		{UseCase: UseCase{ID: nid(), Name: "Edge variation", Classification: "nonCore", VariationOf: &variationOf}},
	}}
	out, _ := validateArchitecture(s, c)
	if !hasRule(out, ruleUseCaseDynamicMissing) {
		t.Fatalf("expected USECASE-DYNAMIC-MISSING for the uncovered nonCore variation, got %+v", out.Findings)
	}
	if hasRule(out, ruleArchChainCov) {
		t.Fatalf("ARCH-CHAINCOV must stay silent for a covered core UC; got %+v", out.Findings)
	}
	if out.Verdict != VerdictFail {
		t.Fatalf("expected VerdictFail")
	}
}

// TestValidateArchitecture_UseCaseDynamicMissing_CoreTripsBoth: a missing CORE view
// legitimately trips BOTH ARCH-CHAINCOV (Löwy) and USECASE-DYNAMIC-MISSING (founder).
func TestValidateArchitecture_UseCaseDynamicMissing_CoreTripsBoth(t *testing.T) {
	ucID := nid()
	s := passingSystem(t, ucID)
	c := CoreUseCases{Decisions: []UseCaseDecision{
		{UseCase: UseCase{ID: ucID, Name: "Core flow", Classification: classCore}},
		{UseCase: UseCase{ID: nid(), Name: "Uncovered core", Classification: classCore}},
	}}
	out, _ := validateArchitecture(s, c)
	if !hasRule(out, ruleArchChainCov) || !hasRule(out, ruleUseCaseDynamicMissing) {
		t.Fatalf("expected BOTH ARCH-CHAINCOV and USECASE-DYNAMIC-MISSING for the uncovered core UC, got %+v", out.Findings)
	}
}

// TestValidateArchitecture_UseCaseDynamicMissing_AllCoveredPasses: every use case
// (core + nonCore) covered by a view → no USECASE-DYNAMIC-MISSING.
func TestValidateArchitecture_UseCaseDynamicMissing_AllCoveredPasses(t *testing.T) {
	ucID := nid()
	s := passingSystem(t, ucID)
	variationID := nid()
	// Add a second view for the nonCore variation, reusing the same participants/edges.
	primary := s.DynamicViews[0]
	s.DynamicViews = append(s.DynamicViews, DynamicView{
		UseCaseID:    variationID,
		Key:          "uc2",
		Title:        "Variation flow",
		Participants: primary.Participants,
		Edges:        primary.Edges,
	})
	variationOf := ucID
	c := CoreUseCases{Decisions: []UseCaseDecision{
		{UseCase: UseCase{ID: ucID, Name: "Core flow", Classification: classCore}},
		{UseCase: UseCase{ID: variationID, Name: "Edge variation", Classification: "nonCore", VariationOf: &variationOf}},
	}}
	out, _ := validateArchitecture(s, c)
	if hasRule(out, ruleUseCaseDynamicMissing) {
		t.Fatalf("USECASE-DYNAMIC-MISSING must stay silent when every use case has a view; got %+v", out.Findings)
	}
}

func TestValidateArchitecture_DuplicateComponentNameFails(t *testing.T) {
	a := comp(t, "Design Manager", kindManager)
	b := comp(t, "design manager", kindManager)
	s := System{Components: []Component{a, b}}
	out, _ := validateArchitecture(s, CoreUseCases{})
	if !hasRule(out, ruleSysNameUniq) {
		t.Fatalf("expected SYS-NAME-UNIQUE, got %+v", out.Findings)
	}
	if out.Verdict != VerdictFail {
		t.Fatalf("duplicate component name must fail the verdict")
	}
}

func TestValidateArchitecture_UniqueComponentNamesPass(t *testing.T) {
	a := comp(t, "DesignManager", kindManager)
	b := comp(t, "RenderManager", kindManager)
	out, _ := validateArchitecture(System{Components: []Component{a, b}}, CoreUseCases{})
	if hasRule(out, ruleSysNameUniq) {
		t.Fatalf("unique component names must NOT trip SYS-NAME-UNIQUE, got %+v", out.Findings)
	}
}

// ---- ValidateOperationalConcepts ----

func TestValidateOperationalConcepts_Pass(t *testing.T) {
	m := MissionStatement{Vision: "v", Objectives: []Objective{{Number: 1, Statement: "fast"}, {Number: 2, Statement: "cheap"}}}
	o := OperationalConcepts{Decisions: []OperationalDecision{
		{Topic: "sync vs queued", Decision: "queued", JustifyingObjective: 1},
		{Topic: "pub/sub", Decision: "none", JustifyingObjective: 2},
	}}
	res, err := validateOperationalConcepts(o, m, System{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Verdict != VerdictPass {
		t.Fatalf("expected VerdictPass, got %v (%+v)", res.Verdict, res.Findings)
	}
}

func TestValidateOperationalConcepts_DanglingObjectiveFails(t *testing.T) {
	m := MissionStatement{Vision: "v", Objectives: []Objective{{Number: 1, Statement: "fast"}}}
	o := OperationalConcepts{Decisions: []OperationalDecision{{Topic: "sync vs queued", Decision: "queued", JustifyingObjective: 9}}}
	res, _ := validateOperationalConcepts(o, m, System{})
	if !hasRule(res, ruleOpcObjRef) {
		t.Fatalf("expected OPC-OBJREF, got %+v", res.Findings)
	}
	if res.Verdict != VerdictFail {
		t.Fatalf("expected VerdictFail")
	}
}

func TestValidateOperationalConcepts_WiringBugIsContractMisuse(t *testing.T) {
	o := OperationalConcepts{Decisions: []OperationalDecision{{Topic: "x", JustifyingObjective: 1}}}
	_, err := validateOperationalConcepts(o, MissionStatement{}, System{})
	assertContractMisuse(t, err)
}

// ---- ValidateStandardCheck ----

func TestValidateStandardCheck_Pass(t *testing.T) {
	sc := StandardCheck{Items: []CheckItem{
		{Section: "§3.1", Guideline: "x", Status: "pass"},
		{Section: "§3.2", Guideline: "y", Status: checkWaived, Justification: "deliberate, documented"},
	}}
	res, err := validateStandardCheck(sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Verdict != VerdictPass {
		t.Fatalf("expected VerdictPass, got %v (%+v)", res.Verdict, res.Findings)
	}
}

func TestValidateStandardCheck_UnjustifiedWaiverFails(t *testing.T) {
	sc := StandardCheck{Items: []CheckItem{{Section: "§3.2", Guideline: "y", Status: checkWaived, Justification: "   "}}}
	res, _ := validateStandardCheck(sc)
	if !hasRule(res, ruleStdWaive) {
		t.Fatalf("expected STD-WAIVE, got %+v", res.Findings)
	}
	if res.Verdict != VerdictFail {
		t.Fatalf("expected VerdictFail")
	}
}

// ---- deterministic ordering ----

func TestFindingsOrderedByOrdinalThenRuleID(t *testing.T) {
	mgr := comp(t, "AManager", kindManager)
	mgr2 := comp(t, "BManager", kindManager)
	ra := comp(t, "Store", kindResourceAccess)
	s := System{
		Components: []Component{mgr, mgr2, ra},
		Relationships: []Relationship{
			{From: ra.ID, To: mgr.ID, Mode: modeSync},
			{From: mgr.ID, To: mgr2.ID, Mode: modeSync},
		},
	}
	out, _ := validateArchitecture(s, CoreUseCases{})
	if len(out.Findings) < 2 {
		t.Fatalf("expected multiple findings, got %+v", out.Findings)
	}
	sortedCopy := make([]Finding, len(out.Findings))
	copy(sortedCopy, out.Findings)
	sort.SliceStable(sortedCopy, func(i, j int) bool {
		oi, oj := ordinalOf(sortedCopy[i]), ordinalOf(sortedCopy[j])
		if oi != oj {
			return oi < oj
		}
		return sortedCopy[i].RuleID < sortedCopy[j].RuleID
	})
	for i := range out.Findings {
		if out.Findings[i].RuleID != sortedCopy[i].RuleID || ordinalOf(out.Findings[i]) != ordinalOf(sortedCopy[i]) {
			t.Fatalf("findings not deterministically ordered: %+v", out.Findings)
		}
	}
}
