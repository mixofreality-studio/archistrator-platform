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
	all, err := DeriveScenarios(p)
	if err != nil {
		t.Fatalf("DeriveScenarios: %v", err)
	}
	return testPlanFindings(p, all)
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

// withOp appends a parameterless operation to one committed contract of p.
func withOp(p Project, contract, op string) {
	sc := p.ServiceContracts[contract]
	sc.Interface.Operations = append(sc.Interface.Operations, ContractOperation{Name: op, Params: []ContractParam{}})
	p.ServiceContracts[contract] = sc
}

// TP-OP-REACHED is the A5 ruling (every API has a use case) and is an Error: an op no
// dynamic view calls blocks the project. The message names the contract, the op and
// the fix.
func TestTP_OpReached_DeadOpIsError(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	withOp(p, "billingManager", "Unreached")
	fs := runTP(t, p)
	wantRules(t, fs, ruleTPOpReached, 1)
	f := fs[0]
	if f.Severity != SeverityError {
		t.Fatalf("TP-OP-REACHED must be an Error (every API has a use case), got %s", severityLabel(f.Severity))
	}
	for _, want := range []string{"billingManager", "Unreached", "add the op's call to a use case's dynamic view, or delete the op"} {
		if !strings.Contains(f.Message, want) {
			t.Fatalf("TP-OP-REACHED message must contain %q: %s", want, f.Message)
		}
	}
	if f.Location == nil || f.Location.Section != "serviceContracts.billingManager" {
		t.Fatalf("TP-OP-REACHED must locate the contract: %+v", f.Location)
	}
}

// The same op, once a use case's dynamic view calls it, is reached: no finding.
func TestTP_OpReached_CalledFromDynamicViewIsClean(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	p.PhaseArtifacts = nil // the plan is not under test; only the design is
	withOp(p, "orderManager", "CancelOrder")
	wantRules(t, runTP(t, p), ruleTPOpReached, 1)

	setSlotModel(t, p, kindSystem, func(m map[string]any) {
		views := m["dynamicViews"].([]any)
		uc2 := views[1].(map[string]any)
		steps := uc2["steps"].([]any)
		ask := steps[0].(map[string]any)
		ask["calls"] = append(ask["calls"].([]any), map[string]any{
			"from": "customer", "to": "order-manager", "mode": "sync", "label": "CancelOrder", "alt": nil,
		})
	})
	if fs := runTP(t, p); hasRuleFindings(fs, ruleTPOpReached) {
		t.Fatalf("an op a dynamic view calls must be reached:\n%s", dumpFindings(fs))
	}
}

// TP-OP-REACHED is design-level: it fires with no phase artifacts at all.
func TestTP_OpReached_WithoutPlan(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	p.PhaseArtifacts = nil
	withOp(p, "orderManager", "CancelOrder")
	wantRules(t, runTP(t, p), ruleTPOpReached, 1)
}

// Ordering edge: before slot 4 (core use cases) AND slot 5 (system) are both
// committed there are no scenarios at all, so every contract would read as unreached.
// A contract written before the architecture exists is not a defect: the rule is
// silent until both slots are committed.
func TestTP_OpReached_SilentBeforeArchitecture(t *testing.T) {
	for _, kind := range []int{kindCoreUseCases, kindSystem} {
		p := loadProject(t, "tp_clean.json")
		p.PhaseArtifacts = nil
		withOp(p, "orderManager", "CancelOrder")
		uncommit(t, p, kind)
		if fs := runTP(t, p); hasRuleFindings(fs, ruleTPOpReached) {
			t.Fatalf("slot %d uncommitted: TP-OP-REACHED must not fire before the architecture exists:\n%s", kind, dumpFindings(fs))
		}
	}
}

// uncommit drops the committed slot of kind from p.
func uncommit(t *testing.T, p Project, kind int) {
	t.Helper()
	for k, s := range p.Slots {
		if s.Kind == kind {
			s.Status = reviewCommitted - 1 // awaiting review
			p.Slots[k] = s
			return
		}
	}
	t.Fatalf("fixture has no slot of kind %d", kind)
}

// setSlotModel rewrites the committed model of kind through a generic JSON edit.
func setSlotModel(t *testing.T, p Project, kind int, edit func(map[string]any)) {
	t.Helper()
	for k, s := range p.Slots {
		if s.Kind != kind {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(s.Model, &m); err != nil {
			t.Fatalf("decode slot %d: %v", kind, err)
		}
		edit(m)
		raw, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("encode slot %d: %v", kind, err)
		}
		s.Model = raw
		p.Slots[k] = s
		return
	}
	t.Fatalf("fixture has no slot of kind %d", kind)
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

	// Completed its work and FAILED TO LAND IT: the server's CoarsePhaseFor reads
	// tailFailureDetail BEFORE completedAt (CompletedNotLanded, dependents stay
	// blocked), so a skip may legitimately wait on it. Decoded from the fixture so
	// the json tag is pinned, not only the field.
	b.Skip.Until = "C-ledger-engine"
	wantRules(t, runTP(t, p), ruleTPSkip, 0)
	p.ActivityExecution["C-ledger-engine"] = ActivityRow{CompletedAt: json.RawMessage(`"2026-10-02T10:00:00Z"`), TailFailureDetail: "merge tail failed"}
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

// The ledger arm. The server's CoarsePhaseFor falls through to the attempt ledger when a
// row carries no head facts (ResolvePhaseCompletions: Done when every profile phase's
// gate attempt passed), and that is the shape of EVERY row on the live dogfood document:
// no startedAt, no completedAt, hundreds of passed gate attempts. All three rows are
// decoded from the fixture so the attempts' json tags are pinned.
func TestTP_Skip_LedgerDecidesWhenHeadFactsAreAbsent(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	b := binding(t, p, "billingManager", "process-order-P2")
	b.Steps = nil

	// every gate passed, no head facts at all: Done by the ledger, so the skip is stale
	b.Skip = &SkipReason{Reason: "waiting", Until: "C-inventory-engine"}
	wantRules(t, runTP(t, p), ruleTPSkip, 1)

	// the live C-billing-manager shape: four phases passed, the integration gate never
	// attempted — the server's profile walk is undecided on it, so not Done
	b.Skip.Until = "C-pricing-engine"
	wantRules(t, runTP(t, p), ruleTPSkip, 0)

	// the integration gate's LATEST attempt was rejected after an earlier pass: latest wins
	b.Skip.Until = "C-catalog-access"
	wantRules(t, runTP(t, p), ruleTPSkip, 0)

	// a completion whose merge tail failed does NOT fall through to a passing ledger:
	// CoarsePhaseFor reads the tail failure before the ledger (CompletedNotLanded)
	row := p.ActivityExecution["C-inventory-engine"]
	row.CompletedAt = json.RawMessage(`"2026-10-02T10:00:00Z"`)
	row.TailFailureDetail = "merge tail failed"
	p.ActivityExecution["C-inventory-engine"] = row
	b.Skip.Until = "C-inventory-engine"
	wantRules(t, runTP(t, p), ruleTPSkip, 0)

	// a sticky failure short-circuits a passing ledger
	row = p.ActivityExecution["C-inventory-engine"]
	row.CompletedAt, row.TailFailureDetail, row.FailureReason = nil, "", 4
	p.ActivityExecution["C-inventory-engine"] = row
	wantRules(t, runTP(t, p), ruleTPSkip, 0)
}

// A legacy document (pre-rename `.activityConstruction` rows) still decodes and still
// answers TP-SKIP. tp_legacy.json carries the rows in the server's WIRE shape — the
// integer ordinals of ActivityBuildStatus (Integrated == 2) and the stored coarse phase
// (Done == 2, Failed == 3), empty ledgers — exactly as archistrator@aa536491 stored its
// 69 rows. An in-memory string BuildStatus does not exist on the wire and proved
// nothing: the real document crashed DecodeProject while that test passed.
func TestTP_Skip_LegacyActivityRows(t *testing.T) {
	p := loadProject(t, "tp_legacy.json")
	if p.ActivityExecution != nil {
		t.Fatalf("fixture must carry only the legacy member")
	}
	b := binding(t, p, "billingManager", "process-order-P2")
	b.Steps = nil

	// phase=Done, buildStatus=Integrated, no head facts: the carry-forward mints the exit
	b.Skip = &SkipReason{Reason: "waiting", Until: "C-billing-manager"}
	wantRules(t, runTP(t, p), ruleTPSkip, 1)

	// Running, in construction
	b.Skip.Until = "C-order-manager"
	wantRules(t, runTP(t, p), ruleTPSkip, 0)

	// stored Failed roll-up with no failureReason: the carry-forward mints PipelineFailed
	b.Skip.Until = "C-shop-client"
	wantRules(t, runTP(t, p), ruleTPSkip, 0)

	// Running, but every gate in the ledger passed: the ledger decides Done
	b.Skip.Until = "C-ledger-engine"
	wantRules(t, runTP(t, p), ruleTPSkip, 1)

	// Integrated roll-up contradicted by a recorded failure: the failure is sticky
	b.Skip.Until = "C-catalog-access"
	wantRules(t, runTP(t, p), ruleTPSkip, 0)
}

// The integer wire form must decode on its own, before any rule runs: ValidateProjectJSON
// runs on every MCP state write, so a decode error here fails every write of a pre-rename
// project.
func TestDecodeProject_LegacyBuildStatusOrdinal(t *testing.T) {
	p, ok, err := DecodeProject([]byte(`{"id":"x","slots":{},"activityConstruction":{"C-AA":{"activityID":"C-AA","phase":2,"buildStatus":2,"attempts":[],"phases":[]}}}`))
	if err != nil || !ok {
		t.Fatalf("decode: ok=%v err=%v", ok, err)
	}
	row := p.LegacyActivityConstruction["C-AA"]
	if row.BuildStatus != legacyBuildIntegrated || row.Phase != legacyPhaseDone {
		t.Fatalf("legacy row decoded as %+v", row)
	}
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
