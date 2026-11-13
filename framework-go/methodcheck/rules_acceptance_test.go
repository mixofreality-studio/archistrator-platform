package methodcheck

import (
	"encoding/json"
	"strings"
	"testing"
)

// rules_acceptance_test.go: the ATP-* family (FPC plan 4, Task 4.2) over testdata/atp_clean.json,
// tp2_clean plus an acceptance plan binding its one residual variant, process-order-P2@shopClient
// (the customer's invoice is a later outcome of a stimulus no single client projection keeps).
// atp_unbound.json is the same design with a recorded acceptance plan that binds nothing;atp_badref.json has two planRefs
// that do not resolve.

func runATP(t *testing.T, p Project) []Finding {
	t.Helper()
	fs, err := scenarioFindings(p)
	if err != nil {
		t.Fatalf("scenarioFindings: %v", err)
	}
	return fs
}

// webProject is atp_clean with its shopClient made a browser surface.
func webProject(t *testing.T) Project {
	t.Helper()
	raw := strings.Replace(string(readFixture(t, "atp_clean.json")), `"uiSurface": false`, `"uiSurface": true`, 1)
	p, ok, err := DecodeProject([]byte(raw))
	if err != nil || !ok {
		t.Fatalf("decode web project: ok=%v err=%v", ok, err)
	}
	return p
}

func acceptanceBinding(t *testing.T, p Project, id string) *ScenarioBinding {
	t.Helper()
	rec := p.PhaseArtifacts.AcceptancePlan
	if rec == nil {
		t.Fatal("fixture has no acceptancePlan")
	}
	for i := range rec.Bindings {
		if rec.Bindings[i].Scenario == id {
			return &rec.Bindings[i]
		}
	}
	t.Fatalf("fixture has no acceptance binding %s", id)
	return nil
}

func TestATP_CleanFixtureIsClean(t *testing.T) {
	p := loadProject(t, "atp_clean.json")
	for _, f := range runATP(t, p) {
		if strings.HasPrefix(string(f.RuleID), "ATP-") || strings.HasPrefix(string(f.RuleID), "TP-") {
			t.Fatalf("clean fixture produced findings:\n%s", dumpFindings(runATP(t, p)))
		}
	}
}

func TestATP_Bound_FiresOnUnboundResidual(t *testing.T) {
	fs := runATP(t, loadProject(t, "atp_unbound.json"))
	wantRules(t, fs, ruleATPBound, 1)
	for _, f := range fs {
		if f.RuleID != ruleATPBound {
			continue
		}
		for _, want := range []string{"process-order-P2@shopClient", "later outcome send-invoice after receive"} {
			if !strings.Contains(f.Message, want) {
				t.Errorf("ATP-BOUND must name %q: %s", want, f.Message)
			}
		}
		if f.Severity != SeverityError {
			t.Errorf("ATP-BOUND is an Error: %+v", f)
		}
	}
}

// No recorded plan is not an ATP-BOUND finding: N-ATP runs beside construction, so every
// whole-document write gate (ValidateProject) must stay open until the plan is written.
func TestATP_Bound_SilentWhenNoPlanIsRecorded(t *testing.T) {
	p := loadProject(t, "atp_unbound.json")
	wantRules(t, runATP(t, p), ruleATPBound, 1)
	p.PhaseArtifacts.AcceptancePlan = nil
	wantRules(t, runATP(t, p), ruleATPBound, 0)
}

func TestATP_Bound_FiresOnNonResidualBinding(t *testing.T) {
	p := loadProject(t, "atp_clean.json")
	wantRules(t, runATP(t, p), ruleATPBound, 0)
	rec := p.PhaseArtifacts.AcceptancePlan
	rec.Bindings = append(rec.Bindings, ScenarioBinding{Scenario: "process-order-P1@shopClient", Skip: &SkipReason{Reason: "covered", Until: "2099-01-01"}})
	fs := runATP(t, p)
	wantRules(t, fs, ruleATPBound, 1)
	if !strings.Contains(dumpFindings(fs), "not residual: covered by the shopClient projection") {
		t.Errorf("ATP-BOUND must say why:\n%s", dumpFindings(fs))
	}
	rec.Bindings[len(rec.Bindings)-1].Scenario = "process-order-P9@shopClient"
	wantRules(t, runATP(t, p), ruleATPBound, 1)
	rec.Bindings[len(rec.Bindings)-1] = rec.Bindings[0]
	wantRules(t, runATP(t, p), ruleATPBound, 1) // bound twice
}

func TestATP_Ref_FiresOnDanglingAndOnNonLiteral(t *testing.T) {
	fs := runATP(t, loadProject(t, "atp_badref.json"))
	wantRules(t, fs, ruleATPRef, 2)
	text := dumpFindings(fs)
	for _, want := range []string{"has no step 9", "must name a literal"} {
		if !strings.Contains(text, want) {
			t.Errorf("ATP-REF must say %q:\n%s", want, text)
		}
	}
}

func TestATP_Ref_ParamMustAgree(t *testing.T) {
	p := loadProject(t, "atp_clean.json")
	// step 3 (shopClient BillingRecordPayment) takes invoiceId (string) and amount (integer):
	// pointing amount at the string input is a different parameter schema.
	b := acceptanceBinding(t, p, "process-order-P2@shopClient")
	for i := range b.Steps[2].Inputs {
		if b.Steps[2].Inputs[i].Name == "amount" {
			b.Steps[2].Inputs[i].Value = PlanRef{Component: "shopClient", Scenario: "process-order-P2", Seq: 2, Input: "invoiceId"}
		}
	}
	wantRules(t, runATP(t, p), ruleATPRef, 1)
}

func TestATP_Ref_RefusedInsideAClientPlan(t *testing.T) {
	p := loadProject(t, "atp_clean.json")
	b := binding(t, p, "orderManager", "track-order-P1")
	b.Steps[0].Inputs[0].Value = PlanRef{Component: "shopClient", Scenario: "process-order-P2", Seq: 1, Input: "order"}
	wantRules(t, runATP(t, p), ruleATPRef, 1)
}

func TestATP_Browser_FiresWhenWebProbeActorDiffers(t *testing.T) {
	p := webProject(t)
	rec := p.PhaseArtifacts.AcceptancePlan
	rec.BrowserActor = "customer"
	wantRules(t, runATP(t, p), ruleATPBrowser, 0)

	rec.Principals["auditor"] = TestPrincipal{Subject: "aud-1", Roles: []string{"auditor"}}
	b := acceptanceBinding(t, p, "process-order-P2@shopClient")
	b.Steps[0].Probes = append(b.Steps[0].Probes, Probe{For: "send-invoice", Component: "shopClient", Operation: "OrderPlaceOrder", As: "auditor"})
	wantRules(t, runATP(t, p), ruleATPBrowser, 1)

	b.Steps[0].Probes[len(b.Steps[0].Probes)-1].As = "customer"
	wantRules(t, runATP(t, p), ruleATPBrowser, 0)

	rec.BrowserActor = ""
	wantRules(t, runATP(t, p), ruleATPBrowser, 1)

	rec.BrowserActor = "nobody"
	wantRules(t, runATP(t, p), ruleATPBrowser, 1)
}

func TestATP_Browser_NotRequiredWithoutWebSteps(t *testing.T) {
	wantRules(t, runATP(t, loadProject(t, "atp_clean.json")), ruleATPBrowser, 0)
}

func TestATP_ProbeAs_NeedsAPrincipal(t *testing.T) {
	p := loadProject(t, "atp_clean.json")
	b := acceptanceBinding(t, p, "process-order-P2@shopClient")
	b.Steps[1].Probes[0].As = "nobody"
	wantRules(t, runATP(t, p), ruleATPProbeAs, 1)
	b.Steps[1].Probes[0].As = "customer"
	wantRules(t, runATP(t, p), ruleATPProbeAs, 0)
}

func TestATP_UsesTheStepsOwnContract(t *testing.T) {
	// step 2 is an orderManager call inside a shopClient variant: an operation of the wrong
	// component is TP-STEP-OP against the component the stimulus addresses.
	p := loadProject(t, "atp_clean.json")
	b := acceptanceBinding(t, p, "process-order-P2@shopClient")
	b.Steps[1].Operation = "RecordPayment"
	wantRules(t, runATP(t, p), ruleTPStepOp, 1)
}

func TestATP_PrincipalIsRequired(t *testing.T) {
	p := loadProject(t, "atp_clean.json")
	delete(p.PhaseArtifacts.AcceptancePlan.Principals, "customer")
	wantRules(t, runATP(t, p), ruleTPPrincipal, 1)
}

func TestBindings_PlanRefDecode(t *testing.T) {
	raw := `{"kind":"planRef","model":{"component":"shopClient","scenario":"process-order-P2","seq":1,"input":"order"}}`
	v, err := UnmarshalTestArgValue([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	want := PlanRef{Component: "shopClient", Scenario: "process-order-P2", Seq: 1, Input: "order"}
	if v != want {
		t.Fatalf("decoded %+v, want %+v", v, want)
	}
	back, err := json.Marshal(v)
	if err != nil || !strings.Contains(string(back), `"kind":"planRef"`) {
		t.Fatalf("marshal: %s %v", back, err)
	}
	if _, err := UnmarshalTestArgValue([]byte(`{"kind":"planRef","model":{"component":"a","hook":1}}`)); err == nil {
		t.Fatal("an undeclared planRef field must be refused")
	}

	p := loadProject(t, "atp_clean.json")
	lit, ok := ResolvePlanRef(p, want)
	if !ok || !sameJSON(lit.JSON, json.RawMessage(`{"id":"o-2","total":1200}`)) {
		t.Fatalf("ResolvePlanRef = %s, %v", lit.JSON, ok)
	}
	for _, bad := range []PlanRef{
		{Component: "nobody", Scenario: "process-order-P2", Seq: 1, Input: "order"},
		{Component: "shopClient", Scenario: "nope", Seq: 1, Input: "order"},
		{Component: "shopClient", Scenario: "process-order-P2", Seq: 9, Input: "order"},
		{Component: "shopClient", Scenario: "process-order-P2", Seq: 1, Input: "nope"},
	} {
		if _, ok := ResolvePlanRef(p, bad); ok {
			t.Errorf("ResolvePlanRef(%+v) must fail", bad)
		}
	}
}

func TestAcceptancePlan_DecodesStrictly(t *testing.T) {
	raw := strings.Replace(string(readFixture(t, "atp_clean.json")), `"acceptancePlan": {`, `"acceptancePlan": {"hook": "x",`, 1)
	_, _, err := DecodeProject([]byte(raw))
	if err == nil || !strings.Contains(err.Error(), string(ruleTPHook)) {
		t.Fatalf("an acceptancePlan carrying hook must be refused naming TP-HOOK, got %v", err)
	}
	raw = strings.Replace(string(readFixture(t, "atp_clean.json")), `"acceptancePlan": {`, `"acceptancePlan": {"stray": 1,`, 1)
	if _, _, err := DecodeProject([]byte(raw)); err == nil {
		t.Fatal("an undeclared acceptancePlan field must be refused")
	}
}

func TestATP_RulesAreRegistered(t *testing.T) {
	for _, id := range []RuleID{ruleATPBound, ruleATPRef, ruleATPBrowser, ruleATPProbeAs} {
		if !emittedRuleIDs()[id] {
			t.Errorf("%s is not in emittedRuleIDs", id)
		}
	}
}
