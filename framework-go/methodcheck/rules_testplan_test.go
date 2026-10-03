package methodcheck

import (
	"encoding/json"
	"strings"
	"testing"
)

// rules_testplan_test.go exercises the TP-* per-component test-plan family over
// testdata/tp_clean.json: a two-use-case project (process-order with an accepted /
// rejected branch and a payment; track-order with two reads) whose two managers each
// carry one correct binding per projected scenario. Every rule test mutates that
// baseline in place and counts the findings it expects.

// wantRules asserts exactly n findings carry rule id.
func wantRules(t *testing.T, fs []Finding, id RuleID, n int) {
	t.Helper()
	got := 0
	for _, f := range fs {
		if f.RuleID == id {
			got++
		}
	}
	if got != n {
		t.Fatalf("%s: got %d findings, want %d\n%s", id, got, n, dumpFindings(fs))
	}
}

func dumpFindings(fs []Finding) string {
	var b strings.Builder
	for _, f := range fs {
		b.WriteString(string(f.RuleID))
		b.WriteString(" ")
		b.WriteString(severityLabel(f.Severity))
		b.WriteString(": ")
		b.WriteString(f.Message)
		b.WriteString("\n")
	}
	return b.String()
}

func runTP(t *testing.T, p Project) []Finding {
	t.Helper()
	fs, err := testPlanFindings(p)
	if err != nil {
		t.Fatalf("testPlanFindings: %v", err)
	}
	return fs
}

// binding returns a pointer into the fixture's binding list so a test can mutate it.
func binding(t *testing.T, p Project, comp, scenario string) *ScenarioBinding {
	t.Helper()
	rec := p.PhaseArtifacts.TestPlan[comp]
	for i := range rec.Bindings {
		if rec.Bindings[i].Scenario == scenario {
			return &rec.Bindings[i]
		}
	}
	t.Fatalf("fixture has no binding %s/%s", comp, scenario)
	return nil
}

func TestTP_CleanFixtureIsClean(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	if fs := runTP(t, p); len(fs) != 0 {
		t.Fatalf("clean fixture produced findings:\n%s", dumpFindings(fs))
	}
}

// A project with no phase artifacts yet is not a defect: only the design-level
// TP-OP-REACHED can speak, and the clean design reaches every op.
func TestTP_NoPhaseArtifactsIsSilent(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	p.PhaseArtifacts = nil
	if fs := runTP(t, p); len(fs) != 0 {
		t.Fatalf("no-plan project produced findings:\n%s", dumpFindings(fs))
	}
}

func TestTP_Bound_MissingAndUnknown(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	rec := p.PhaseArtifacts.TestPlan["billingManager"]
	rec.Bindings = append(rec.Bindings[1:], ScenarioBinding{Scenario: "nope-P9"})
	p.PhaseArtifacts.TestPlan["billingManager"] = rec
	fs := runTP(t, p)
	wantRules(t, fs, ruleTPBound, 2) // one missing, one unknown
	for _, f := range fs {
		if f.Severity != SeverityError {
			t.Fatalf("TP-BOUND must be an Error: %+v", f)
		}
		if f.Location == nil || f.Location.Section != "phaseArtifacts.testPlan.billingManager" {
			t.Fatalf("TP-BOUND must locate the component's plan: %+v", f.Location)
		}
	}
}

func TestTP_Bound_Duplicate(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	rec := p.PhaseArtifacts.TestPlan["billingManager"]
	rec.Bindings = append(rec.Bindings, rec.Bindings[0])
	p.PhaseArtifacts.TestPlan["billingManager"] = rec
	wantRules(t, runTP(t, p), ruleTPBound, 1)
}

func TestTP_Step_CountMismatch(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	b := binding(t, p, "billingManager", "process-order-P2")
	b.Steps = append(b.Steps, b.Steps[0])
	fs := runTP(t, p)
	wantRules(t, fs, ruleTPStep, 1)
	wantRules(t, fs, ruleTPStepOp, 0) // a miscounted binding is reported once, not per step
}

func TestTP_Step_SeqMismatch(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	b := binding(t, p, "billingManager", "process-order-P2")
	b.Steps[0].Seq = 7
	wantRules(t, runTP(t, p), ruleTPStep, 1)
}

// Review Focus 3: the call chain resolved RecordPayment for the pay stimulus; a
// binding that names another op of the same component contradicts the design.
func TestTP_StepOp_MismatchDerived(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	b := binding(t, p, "billingManager", "process-order-P2")
	b.Steps[0].Operation = "GetInvoice"
	fs := runTP(t, p)
	wantRules(t, fs, ruleTPStepOp, 1)
	wantRules(t, fs, ruleTPArgName, 0) // the wrong op is the defect; its args are not re-judged
}

func TestTP_StepOp_UnknownOp(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	b := binding(t, p, "billingManager", "process-order-P2")
	b.Steps[0].Operation = "Nope"
	wantRules(t, runTP(t, p), ruleTPStepOp, 1)
}

// Review Focus 3, other half: when the call chain resolves NO op (label matches no
// contract op) the binding's Operation is authoritative and only has to exist.
func TestTP_StepOp_UnresolvedDerivedAcceptsBinding(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	sys, _, err := p.system()
	if err != nil {
		t.Fatal(err)
	}
	for i := range sys.DynamicViews {
		for j := range sys.DynamicViews[i].Steps {
			if sys.DynamicViews[i].Steps[j].ActivityNodeID == "pay" {
				sys.DynamicViews[i].Steps[j].Calls[0].Label = "settle the invoice"
			}
		}
	}
	p = withSystem(t, p, sys)
	fs := runTP(t, p)
	wantRules(t, fs, ruleTPStepOp, 0)
	b := binding(t, p, "billingManager", "process-order-P2")
	b.Steps[0].Operation = "Nope"
	wantRules(t, runTP(t, p), ruleTPStepOp, 1)
}

func TestTP_ArgName_UnknownAndMissing(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	b := binding(t, p, "billingManager", "process-order-P2")
	b.Steps[0].Inputs = []TestArg{{Name: "invoiceId", Value: "inv-1"}, {Name: "amt", Value: "1"}}
	wantRules(t, runTP(t, p), ruleTPArgName, 2) // amt unknown, amount missing
}

func TestTP_ArgName_PointerParamOptional(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	sc := p.ServiceContracts["billingManager"]
	sc.Interface.Operations[0].Params[1].Pointer = true
	p.ServiceContracts["billingManager"] = sc
	b := binding(t, p, "billingManager", "process-order-P2")
	b.Steps[0].Inputs = b.Steps[0].Inputs[:1]
	wantRules(t, runTP(t, p), ruleTPArgName, 0)
}

func TestTP_ArgType_StringForInteger(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	b := binding(t, p, "billingManager", "process-order-P2")
	b.Steps[0].Inputs[1].Value = "twelve hundred"
	wantRules(t, runTP(t, p), ruleTPArgType, 1)
}

func TestTP_ArgType_SchemaRefMismatch(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	b := binding(t, p, "orderManager", "process-order-P2")
	b.Steps[0].Inputs[0].SchemaRef = "#/$defs/OrderID"
	wantRules(t, runTP(t, p), ruleTPArgType, 1)
}

func TestTP_ArgType_ObjectForScalar(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	b := binding(t, p, "billingManager", "process-order-P2")
	b.Steps[0].Inputs[0].Value = `{"id":"inv-1"}`
	wantRules(t, runTP(t, p), ruleTPArgType, 1)
}

// A bare numeric value against a string param is ambiguous (the plan stores
// unquoted scalars) and must not fire; a quoted JSON string against an integer is
// not ambiguous.
func TestTP_ArgType_AmbiguousScalarsAreSilent(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	b := binding(t, p, "billingManager", "process-order-P2")
	b.Steps[0].Inputs[0].Value = "42"
	wantRules(t, runTP(t, p), ruleTPArgType, 0)
	b.Steps[0].Inputs[1].Value = `"42"`
	wantRules(t, runTP(t, p), ruleTPArgType, 1)
}

func TestTP_Expect_ErrorNotDeclared(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	sc := p.ServiceContracts["orderManager"]
	sc.Interface.Operations[0].Error = false
	p.ServiceContracts["orderManager"] = sc
	wantRules(t, runTP(t, p), ruleTPExpect, 1) // P1 expects OrderRejected
}

func TestTP_Expect_ResultShape(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	b := binding(t, p, "orderManager", "process-order-P2")
	b.Steps[0].Expect.Result = `{"orderId":"o-2"}` // PlaceOrder returns an OrderID string
	wantRules(t, runTP(t, p), ruleTPExpect, 1)
}

func TestTP_Expect_ResultOnVoid(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	b := binding(t, p, "billingManager", "process-order-P2")
	b.Steps[0].Expect.Result = "ok" // RecordPayment is void
	wantRules(t, runTP(t, p), ruleTPExpect, 1)
}

func TestTP_Probe_UnknownOp(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	b := binding(t, p, "billingManager", "process-order-P2")
	b.Steps[0].Probes[0].Operation = "PeekInvoice"
	fs := runTP(t, p)
	wantRules(t, fs, ruleTPProbe, 1)
	wantRules(t, fs, ruleTPState, 1) // an unresolvable probe proves nothing
}

func TestTP_Probe_UnknownComponent(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	b := binding(t, p, "billingManager", "process-order-P2")
	b.Steps[0].Probes[0].Component = "ledgerManager"
	wantRules(t, runTP(t, p), ruleTPProbe, 1)
}

func TestTP_Probe_ArgsChecked(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	b := binding(t, p, "billingManager", "process-order-P2")
	b.Steps[0].Probes[0].Inputs = []TestArg{{Name: "invoice", Value: "inv-1"}}
	wantRules(t, runTP(t, p), ruleTPProbe, 2) // invoice unknown, invoiceId missing
}

func TestTP_State_UnprovedExpectedState(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	b := binding(t, p, "billingManager", "process-order-P2")
	b.Steps[0].Probes = nil
	fs := runTP(t, p)
	wantRules(t, fs, ruleTPState, 1)
	if !strings.Contains(fs[0].Message, "pay-paid") || !strings.Contains(fs[0].Message, "[paid]") {
		t.Fatalf("TP-STATE must name the node and state: %s", fs[0].Message)
	}
}

func TestTP_State_OutputNeedsProbeOrUnobservable(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	b := binding(t, p, "orderManager", "process-order-P2")
	b.Steps[0].Unobservable = nil
	fs := runTP(t, p)
	wantRules(t, fs, ruleTPState, 1)
	b.Steps[0].Probes = append(b.Steps[0].Probes, Probe{
		For: "send-invoice", Component: "billingManager", Operation: "GetInvoice",
		Inputs: []TestArg{{Name: "invoiceId", Value: "inv-o-2"}},
	})
	wantRules(t, runTP(t, p), ruleTPState, 0)
}

func TestTP_OpReached_DeadOp(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	sc := p.ServiceContracts["billingManager"]
	sc.Interface.Operations = append(sc.Interface.Operations, ContractOperation{Name: "Unreached", Params: []ContractParam{}})
	p.ServiceContracts["billingManager"] = sc
	fs := runTP(t, p)
	wantRules(t, fs, ruleTPOpReached, 1)
	f := fs[0]
	if f.Severity != SeverityWarning {
		t.Fatalf("TP-OP-REACHED describes the design, not the binding: must be a Warning, got %s", severityLabel(f.Severity))
	}
	if !strings.Contains(f.Message, "billingManager.Unreached") || !strings.Contains(f.Message, "dead operation or missing use case") {
		t.Fatalf("TP-OP-REACHED message: %s", f.Message)
	}
	if f.Location == nil || f.Location.Section != "serviceContracts.billingManager" {
		t.Fatalf("TP-OP-REACHED must locate the contract: %+v", f.Location)
	}
}

// TP-OP-REACHED is design-level: it fires with no phase artifacts at all.
func TestTP_OpReached_WithoutPlan(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	p.PhaseArtifacts = nil
	sc := p.ServiceContracts["orderManager"]
	sc.Interface.Operations = append(sc.Interface.Operations, ContractOperation{Name: "CancelOrder", Params: []ContractParam{}})
	p.ServiceContracts["orderManager"] = sc
	wantRules(t, runTP(t, p), ruleTPOpReached, 1)
}

func TestTP_Skip_RequiresUnintegratedActivity(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	b := binding(t, p, "billingManager", "process-order-P2")
	b.Steps = nil
	b.Skip = &SkipReason{Reason: "payments land with billing", Until: "C-billing-manager"} // completed, no failure
	fs := runTP(t, p)
	wantRules(t, fs, ruleTPSkip, 1)
	wantRules(t, fs, ruleTPStep, 0) // a skipped binding has no steps to match

	b.Skip.Until = "C-order-manager" // opened, not completed
	wantRules(t, runTP(t, p), ruleTPSkip, 0)

	b.Skip.Until = "C-shop-client" // completed WITH a failure: not integrated
	wantRules(t, runTP(t, p), ruleTPSkip, 0)

	b.Skip.Until = "C-never-opened"
	wantRules(t, runTP(t, p), ruleTPSkip, 0)
}

func TestTP_Skip_NeedsReasonAndUntil(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	b := binding(t, p, "billingManager", "process-order-P2")
	b.Skip = &SkipReason{Until: "C-order-manager"}
	wantRules(t, runTP(t, p), ruleTPSkip, 1)
	b.Skip = &SkipReason{Reason: "waiting"}
	wantRules(t, runTP(t, p), ruleTPSkip, 1)
}

// A legacy document (pre-rename `.activityConstruction` rows carrying buildStatus)
// still answers TP-SKIP.
func TestTP_Skip_LegacyActivityRows(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	p.ActivityExecution = nil
	p.LegacyActivityConstruction = map[string]ActivityRow{
		"C-billing-manager": {BuildStatus: "Integrated"},
		"C-order-manager":   {BuildStatus: "in-construction"},
	}
	b := binding(t, p, "billingManager", "process-order-P2")
	b.Skip = &SkipReason{Reason: "waiting", Until: "C-billing-manager"}
	wantRules(t, runTP(t, p), ruleTPSkip, 1)
	b.Skip.Until = "C-order-manager"
	wantRules(t, runTP(t, p), ruleTPSkip, 0)
}

// tp_drift.json is tp_clean.json after a use-case edit the plan never followed: the
// rejected branch is gone, so the accepted path is now process-order-P1 and the
// committed bindings are stale — the former P2 is unknown and the P1 binding (written
// for the rejected path) proves none of the accepted path's states.
func TestTP_DriftFixture(t *testing.T) {
	p := loadProject(t, "tp_drift.json")
	fs := runTP(t, p)
	wantRules(t, fs, ruleTPBound, 3) // both managers bind the vanished P2; billingManager never bound the renumbered P1
	wantRules(t, fs, ruleTPState, 2) // orderManager P1: inv-req state + send-invoice output unproved
	var stale bool
	for _, f := range fs {
		if f.RuleID == ruleTPBound && strings.Contains(f.Message, "stale after a use-case edit") {
			stale = true
		}
	}
	if !stale {
		t.Fatalf("TP-BOUND must hint at the use-case edit:\n%s", dumpFindings(fs))
	}
}

// The family is wired into ValidateProject: a clean plan adds no TP/UC-IO finding
// and a stale one fails the project. (The fixture is committed-SHAPE, like
// testdata/scenario_project.json — it is not a Method-clean architecture, so the
// CC-*/DV-* families are out of scope here.)
func TestTP_WiredIntoValidateProject(t *testing.T) {
	fs, err := ValidateProjectJSON(readFixture(t, "tp_clean.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if strings.HasPrefix(string(f.RuleID), "TP-") || strings.HasPrefix(string(f.RuleID), "UC-IO") || f.RuleID == ruleUCNoIO {
			t.Fatalf("clean plan produced a finding: %s %s", f.RuleID, f.Message)
		}
	}
	fs, err = ValidateProjectJSON(readFixture(t, "tp_drift.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !hasRuleFindings(fs, ruleTPBound) {
		t.Fatalf("drift fixture must fail TP-BOUND through ValidateProject:\n%s", dumpFindings(fs))
	}
}

// withSystem re-encodes a mutated System into the project's slot 5.
func withSystem(t *testing.T, p Project, sys System) Project {
	t.Helper()
	for k, s := range p.Slots {
		if s.Kind != kindSystem {
			continue
		}
		raw, err := json.Marshal(sys)
		if err != nil {
			t.Fatal(err)
		}
		s.Model = raw
		p.Slots[k] = s
		return p
	}
	t.Fatal("fixture has no System slot")
	return p
}
