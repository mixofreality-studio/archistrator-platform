package methodcheck

import (
	"encoding/json"
	"testing"
)

// rules_testplan_test.go exercises the STP-* System-Test-Plan family: per-rule
// violating + passing coverage over an in-code baseline, the historical billing-drift
// regression fixture, and the whole-family no-op when the plan is absent.

func raw(s string) json.RawMessage { return json.RawMessage(s) }

// stpParts returns a fully consistent baseline: a settlementManager contract, a System
// with a bill-user dynamic view, the matching core use case, and a plan with one clean
// happy + one clean boundary case. Every part is mutated in place by the per-rule tests.
func stpParts(t *testing.T) (map[string]ServiceContract, System, CoreUseCases, *SystemTestPlan) {
	t.Helper()
	contracts := map[string]ServiceContract{
		"settlementManager": {
			Component: "settlementManager",
			Layer:     "Manager",
			Title:     "settlement contract",
			Defs: map[string]json.RawMessage{
				"CustomerID": raw(`{"type":"string"}`),
				"CycleID":    raw(`{"type":"string"}`),
			},
			Interface: ContractInterface{
				Name:  "SettlementManager",
				Layer: "Manager",
				Operations: []ContractOperation{
					{
						Name: "CloseSettlementCycle",
						Params: []ContractParam{
							{Name: "customerID", Schema: raw(`{"$ref":"#/$defs/CustomerID"}`)},
							{Name: "cycleID", Schema: raw(`{"$ref":"#/$defs/CycleID"}`)},
						},
						Error: true,
					},
					{
						Name:   "RunShortfallSweep",
						Params: []ContractParam{{Name: "tickID", Schema: raw(`{"type":"string"}`)}},
						Error:  true,
					},
				},
			},
		},
	}
	sys := System{
		Components: []Component{
			{ID: "scheduler-client", Name: "SchedulerClient", Kind: kindClient, Layer: layerClient},
			{ID: "settlement-manager", Name: "SettlementManager", Kind: kindManager, Layer: layerManager},
		},
		DynamicViews: []DynamicView{{
			UseCaseID:    "bill-the-user-for-usage",
			Key:          "uc-bill",
			Participants: []string{"scheduler-client", "settlement-manager"},
			Edges: []Relationship{
				{From: "scheduler-client", To: "settlement-manager", Mode: modeSync, Label: "closeSettlementCycle(customerId, cycleId)"},
			},
		}},
	}
	cuc := CoreUseCases{Decisions: []UseCaseDecision{
		{UseCase: UseCase{ID: "bill-the-user-for-usage", Name: "Bill the User for Usage", Classification: classCore}},
	}}
	inputs := []TestArg{
		{Name: "customerID", Value: "cust-1", SchemaRef: "#/$defs/CustomerID"},
		{Name: "cycleID", Value: "cyc-1", SchemaRef: "#/$defs/CycleID"},
	}
	plan := &SystemTestPlan{Scenarios: []TestScenario{{
		ID:      "STP-BILL",
		UseCase: "bill-the-user-for-usage",
		Title:   "Bill the user",
		Cases: []TestCase{
			{ID: "H1", Kind: "happy", Steps: []TestStep{
				{Seq: 1, Component: "settlementManager", Operation: "CloseSettlementCycle", Inputs: inputs, Expect: TestExpect{}},
			}},
			{ID: "B1", Kind: "boundary", Steps: []TestStep{
				{Seq: 1, Component: "settlementManager", Operation: "CloseSettlementCycle", Inputs: inputs, Expect: TestExpect{ErrorExpected: true, ErrorCode: "GatewayDeclined"}},
			}},
		},
	}}}
	return contracts, sys, cuc, plan
}

// stpBuild assembles a Project from the parts, committing slot 4 (core use cases) and
// slot 5 (System) so validateSystemTestPlan's prerequisites are satisfied.
func stpBuild(t *testing.T, contracts map[string]ServiceContract, sys System, cuc CoreUseCases, plan *SystemTestPlan) Project {
	t.Helper()
	return Project{
		Slots: map[string]Slot{
			"4": {Status: reviewCommitted, Kind: kindCoreUseCases, Model: mustJSON(t, cuc)},
			"5": {Status: reviewCommitted, Kind: kindSystem, Model: mustJSON(t, sys)},
		},
		ServiceContracts: contracts,
		TestingState:     &TestingState{SystemTestPlan: plan},
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func runSTP(t *testing.T, contracts map[string]ServiceContract, sys System, cuc CoreUseCases, plan *SystemTestPlan) []Finding {
	t.Helper()
	f, err := validateSystemTestPlan(stpBuild(t, contracts, sys, cuc, plan), nil)
	if err != nil {
		t.Fatalf("validateSystemTestPlan: unexpected error %v", err)
	}
	return f
}

func TestSTP_BaselineIsClean(t *testing.T) {
	c, s, u, p := stpParts(t)
	if f := runSTP(t, c, s, u, p); len(f) != 0 {
		t.Fatalf("a fully consistent plan must produce zero findings, got %+v", f)
	}
}

func TestSTP_OpExists_UnknownOperation(t *testing.T) {
	c, s, u, p := stpParts(t)
	p.Scenarios[0].Cases[0].Steps[0].Operation = "NoSuchOp"
	if !hasRuleFindings(runSTP(t, c, s, u, p), ruleSTPOpExists) {
		t.Fatalf("expected STP-OP-EXISTS for an unknown operation")
	}
}

func TestSTP_OpExists_IsCaseSensitive(t *testing.T) {
	c, s, u, p := stpParts(t)
	// The contract declares CloseSettlementCycle; lowercase-initial must NOT resolve.
	p.Scenarios[0].Cases[0].Steps[0].Operation = "closeSettlementCycle"
	if !hasRuleFindings(runSTP(t, c, s, u, p), ruleSTPOpExists) {
		t.Fatalf("STP-OP-EXISTS must be case-sensitive (closeSettlementCycle != CloseSettlementCycle)")
	}
}

func TestSTP_OpExists_UnknownComponent(t *testing.T) {
	c, s, u, p := stpParts(t)
	p.Scenarios[0].Cases[0].Steps[0].Component = "ghostManager"
	if !hasRuleFindings(runSTP(t, c, s, u, p), ruleSTPOpExists) {
		t.Fatalf("expected STP-OP-EXISTS for a component with no committed contract")
	}
}

func TestSTP_StaleContract_SuppressesArgChecks(t *testing.T) {
	c, s, u, p := stpParts(t)
	// Turn the contract into a never-detailed-designed stub (all ops params:null).
	sc := c["settlementManager"]
	for i := range sc.Interface.Operations {
		sc.Interface.Operations[i].Params = nil
	}
	c["settlementManager"] = sc
	// Also feed a bogus arg — ARG-NAME would fire were it not suppressed by the stub.
	p.Scenarios[0].Cases[0].Steps[0].Inputs = append(p.Scenarios[0].Cases[0].Steps[0].Inputs, TestArg{Name: "bogus", Value: "x"})
	f := runSTP(t, c, s, u, p)
	if !hasRuleFindings(f, ruleSTPStaleContract) {
		t.Fatalf("expected STP-STALE-CONTRACT for an all-params-null stub")
	}
	if hasRuleFindings(f, ruleSTPArgName) {
		t.Fatalf("STP-ARG-* must be suppressed for a stale contract")
	}
}

func TestSTP_ArgName_UnknownArgument(t *testing.T) {
	c, s, u, p := stpParts(t)
	p.Scenarios[0].Cases[0].Steps[0].Inputs = append(p.Scenarios[0].Cases[0].Steps[0].Inputs, TestArg{Name: "surprise", Value: "1"})
	if !hasRuleFindings(runSTP(t, c, s, u, p), ruleSTPArgName) {
		t.Fatalf("expected STP-ARG-NAME for an input naming no parameter")
	}
}

func TestSTP_ArgName_MissingRequiredParam(t *testing.T) {
	c, s, u, p := stpParts(t)
	// Drop the cycleID input — a required (non-pointer) param goes unsupplied.
	p.Scenarios[0].Cases[0].Steps[0].Inputs = p.Scenarios[0].Cases[0].Steps[0].Inputs[:1]
	if !hasRuleFindings(runSTP(t, c, s, u, p), ruleSTPArgName) {
		t.Fatalf("expected STP-ARG-NAME for a missing required parameter")
	}
}

func TestSTP_ArgName_PointerParamOptional(t *testing.T) {
	c, s, u, p := stpParts(t)
	// Make cycleID a pointer (optional) and drop its input — must NOT fire.
	sc := c["settlementManager"]
	sc.Interface.Operations[0].Params[1].Pointer = true
	c["settlementManager"] = sc
	p.Scenarios[0].Cases[0].Steps[0].Inputs = p.Scenarios[0].Cases[0].Steps[0].Inputs[:1]
	p.Scenarios[0].Cases[1].Steps[0].Inputs = p.Scenarios[0].Cases[1].Steps[0].Inputs[:1]
	if hasRuleFindings(runSTP(t, c, s, u, p), ruleSTPArgName) {
		t.Fatalf("a pointer (optional) parameter may be omitted without STP-ARG-NAME")
	}
}

func TestSTP_ArgType_SchemaRefMismatch(t *testing.T) {
	c, s, u, p := stpParts(t)
	// customerID input now claims the CycleID type — contradicts the param's CustomerID.
	p.Scenarios[0].Cases[0].Steps[0].Inputs[0].SchemaRef = "#/$defs/CycleID"
	if !hasRuleFindings(runSTP(t, c, s, u, p), ruleSTPArgType) {
		t.Fatalf("expected STP-ARG-TYPE for a schemaRef contradicting the contract param")
	}
}

func TestSTP_ArgType_ValueKindMismatch(t *testing.T) {
	c, s, u, p := stpParts(t)
	// Empty schemaRef → best-effort value-kind check; an object value for a string param.
	in := &p.Scenarios[0].Cases[0].Steps[0].Inputs[0]
	in.SchemaRef = ""
	in.Value = `{"nested":true}`
	if !hasRuleFindings(runSTP(t, c, s, u, p), ruleSTPArgType) {
		t.Fatalf("expected STP-ARG-TYPE for an object value where a scalar param is declared")
	}
}

func TestSTP_ExpectShape_ErrorOnNonErrorOp(t *testing.T) {
	c, s, u, p := stpParts(t)
	sc := c["settlementManager"]
	sc.Interface.Operations[0].Error = false // op cannot fail...
	c["settlementManager"] = sc
	// ...but the boundary case expects an error.
	if !hasRuleFindings(runSTP(t, c, s, u, p), ruleSTPExpectShape) {
		t.Fatalf("expected STP-EXPECT-SHAPE: error-expected step against a non-error operation")
	}
}

func TestSTP_ExpectShape_ResultKindMismatch(t *testing.T) {
	c, s, u, p := stpParts(t)
	// Give the op an object result, but assert a scalar result value.
	sc := c["settlementManager"]
	sc.Interface.Operations[0].Result = raw(`{"type":"object"}`)
	c["settlementManager"] = sc
	p.Scenarios[0].Cases[0].Steps[0].Expect.Result = "just-a-scalar"
	if !hasRuleFindings(runSTP(t, c, s, u, p), ruleSTPExpectShape) {
		t.Fatalf("expected STP-EXPECT-SHAPE for a scalar result against an object-returning op")
	}
}

func TestSTP_ExpectShape_VoidOpWithAssertedResult(t *testing.T) {
	c, s, u, p := stpParts(t)
	// CloseSettlementCycle declares no result (void); asserting a value is a shape error.
	p.Scenarios[0].Cases[0].Steps[0].Expect.Result = "unexpected"
	if !hasRuleFindings(runSTP(t, c, s, u, p), ruleSTPExpectShape) {
		t.Fatalf("expected STP-EXPECT-SHAPE for a result asserted against a void op")
	}
}

// ---- STP-CHAIN-COVER + the entry-edge walk model ----

// twoOpChainView rewires the baseline view to a two-entry-op chain: the scheduler
// client drives closeSettlementCycle THEN runShortfallSweep on the settlement manager.
func twoOpChainView(s *System) {
	s.DynamicViews[0].Edges = []Relationship{
		{From: "scheduler-client", To: "settlement-manager", Mode: modeSync, Label: "closeSettlementCycle()"},
		{From: "scheduler-client", To: "settlement-manager", Mode: modeSync, Label: "runShortfallSweep()"},
	}
}

func closeInputs() []TestArg {
	return []TestArg{{Name: "customerID", Value: "c"}, {Name: "cycleID", Value: "y"}}
}

func TestSTP_ChainCover_FullChainInOrderClean(t *testing.T) {
	c, s, u, p := stpParts(t)
	twoOpChainView(&s)
	p.Scenarios[0].Cases[0].Steps = []TestStep{
		{Seq: 1, Component: "settlementManager", Operation: "CloseSettlementCycle", Inputs: closeInputs()},
		{Seq: 2, Component: "settlementManager", Operation: "RunShortfallSweep", Inputs: []TestArg{{Name: "tickID", Value: "t"}}},
	}
	p.Scenarios[0].Cases[1].Steps = []TestStep{
		{Seq: 1, Component: "settlementManager", Operation: "CloseSettlementCycle", Inputs: closeInputs(), Expect: TestExpect{ErrorExpected: true}},
	}
	if f := runSTP(t, c, s, u, p); len(f) != 0 {
		t.Fatalf("a happy case walking the full entry chain in order must be clean, got %+v", f)
	}
}

func TestSTP_ChainCover_MissingEntryOp(t *testing.T) {
	c, s, u, p := stpParts(t)
	twoOpChainView(&s)
	// The happy case covers only the first entry op; runShortfallSweep is never exercised.
	p.Scenarios[0].Cases[0].Steps = []TestStep{
		{Seq: 1, Component: "settlementManager", Operation: "CloseSettlementCycle", Inputs: closeInputs()},
	}
	if !hasRuleFindings(runSTP(t, c, s, u, p), ruleSTPChainCover) {
		t.Fatalf("expected STP-CHAIN-COVER when a happy case omits an entry operation")
	}
}

func TestSTP_ChainCover_NoHappyCaseIsError(t *testing.T) {
	c, s, u, p := stpParts(t)
	p.Scenarios[0].Cases = p.Scenarios[0].Cases[1:] // boundary only — no happy case
	sev, ok := findingSeverity(runSTP(t, c, s, u, p), ruleSTPChainCover)
	if !ok || sev != SeverityError {
		t.Fatalf("a scenario with a view but no happy case must be a STP-CHAIN-COVER Error, got sev=%v ok=%v", sev, ok)
	}
}

// TestSTP_ChainCover_R4WebMcpDedupe proves web+mcp entry edges naming the SAME manager
// operation collapse to one entry op — a single close step then covers the whole chain.
func TestSTP_ChainCover_R4WebMcpDedupe(t *testing.T) {
	c, s, u, p := stpParts(t)
	s.Components = []Component{
		{ID: "web-client", Name: "WebClient", Kind: kindClient, Layer: layerClient},
		{ID: "mcp-client", Name: "McpClient", Kind: kindClient, Layer: layerClient},
		{ID: "settlement-manager", Name: "SettlementManager", Kind: kindManager, Layer: layerManager},
	}
	s.DynamicViews[0].Participants = []string{"web-client", "mcp-client", "settlement-manager"}
	s.DynamicViews[0].Edges = []Relationship{
		{From: "web-client", To: "settlement-manager", Mode: modeSync, Label: "closeSettlementCycle(customerId, cycleId)"},
		{From: "mcp-client", To: "settlement-manager", Mode: modeSync, Label: "closeSettlementCycle(customerId, cycleId)"},
	}
	if f := runSTP(t, c, s, u, p); hasRuleFindings(f, ruleSTPChainCover) {
		t.Fatalf("web+mcp entry edges naming the same op must dedupe to one; a single close step covers it, got %+v", f)
	}
}

// TestSTP_WalkLegal_NonEntryContractOpNoFinding is the regression for the 24 historical
// false positives: an interior contract op (GetSessionState) that no ENTRY edge names
// must raise NO walk finding — it is the contract family's jurisdiction, not the walk's.
func TestSTP_WalkLegal_NonEntryContractOpNoFinding(t *testing.T) {
	c, s, u, p := stpParts(t)
	sc := c["settlementManager"]
	sc.Interface.Operations = append(sc.Interface.Operations, ContractOperation{
		Name: "GetSessionState", Params: []ContractParam{}, Result: raw(`{"type":"object"}`),
	})
	c["settlementManager"] = sc
	// Drive the entry op, then assert against the interior op in the same happy case.
	p.Scenarios[0].Cases[0].Steps = []TestStep{
		{Seq: 1, Component: "settlementManager", Operation: "CloseSettlementCycle", Inputs: closeInputs()},
		{Seq: 2, Component: "settlementManager", Operation: "GetSessionState", Expect: TestExpect{Result: `{"ok":true}`}},
	}
	if f := runSTP(t, c, s, u, p); hasRuleFindings(f, ruleSTPWalkLegal) {
		t.Fatalf("an interior (non-entry) contract op must NOT raise STP-WALK-LEGAL, got %+v", f)
	}
}

// TestSTP_WalkParticipant_ForeignComponentWarns stages an off-footprint step (a
// component that is committed + contracted but not a view participant) and expects the
// Warning-severity drift signal.
func TestSTP_WalkParticipant_ForeignComponentWarns(t *testing.T) {
	c, s, u, p := stpParts(t)
	s.Components = append(s.Components, Component{ID: "audit-engine", Name: "AuditEngine", Kind: kindEngine, Layer: layerEngine})
	c["auditEngine"] = ServiceContract{
		Component: "auditEngine", Layer: "Engine",
		Interface: ContractInterface{Name: "AuditEngine", Layer: "Engine", Operations: []ContractOperation{{Name: "RecordAudit", Params: []ContractParam{}}}},
	}
	p.Scenarios[0].Cases[1].Steps = append(p.Scenarios[0].Cases[1].Steps, TestStep{
		Seq: 2, Component: "auditEngine", Operation: "RecordAudit",
	})
	sev, ok := findingSeverity(runSTP(t, c, s, u, p), ruleSTPWalkParticipant)
	if !ok {
		t.Fatalf("expected STP-WALK-PARTICIPANT for a step on a non-participant component")
	}
	if sev != SeverityWarning {
		t.Fatalf("STP-WALK-PARTICIPANT must be a Warning, got %v", sev)
	}
}

func TestSTP_WalkLegal_OutOfOrder(t *testing.T) {
	c, s, u, p := stpParts(t)
	// Two ops on two ordered edges; walk them in the reverse order.
	s.DynamicViews[0].Edges = []Relationship{
		{From: "scheduler-client", To: "settlement-manager", Mode: modeSync, Label: "closeSettlementCycle()"},
		{From: "scheduler-client", To: "settlement-manager", Mode: modeSync, Label: "runShortfallSweep()"},
	}
	p.Scenarios[0].Cases[0].Steps = []TestStep{
		{Seq: 1, Component: "settlementManager", Operation: "RunShortfallSweep", Inputs: []TestArg{{Name: "tickID", Value: "t1", SchemaRef: "#/$defs/CycleID"}}, Expect: TestExpect{}},
		{Seq: 2, Component: "settlementManager", Operation: "CloseSettlementCycle", Inputs: []TestArg{{Name: "customerID", Value: "c", SchemaRef: "#/$defs/CustomerID"}, {Name: "cycleID", Value: "y", SchemaRef: "#/$defs/CycleID"}}, Expect: TestExpect{}},
	}
	// tickID param is a primitive; the schemaRef would mismatch, so clear it to isolate WALK.
	p.Scenarios[0].Cases[0].Steps[0].Inputs[0].SchemaRef = ""
	if !hasRuleFindings(runSTP(t, c, s, u, p), ruleSTPWalkLegal) {
		t.Fatalf("expected STP-WALK-LEGAL for a walk that violates view-edge order")
	}
}

func TestSTP_WalkMode_QueuedAssertedSynchronously(t *testing.T) {
	c, s, u, p := stpParts(t)
	// The edge becomes queued; the boundary step asserts its error inline with no observe.
	s.DynamicViews[0].Edges[0].Mode = modeQueued
	if !hasRuleFindings(runSTP(t, c, s, u, p), ruleSTPWalkMode) {
		t.Fatalf("expected STP-WALK-MODE for a queued edge asserted synchronously")
	}
}

func TestSTP_WalkMode_QueuedWithObserveIsClean(t *testing.T) {
	c, s, u, p := stpParts(t)
	s.DynamicViews[0].Edges[0].Mode = modeQueued
	s.DynamicViews[0].Edges = append(s.DynamicViews[0].Edges, Relationship{
		From: "scheduler-client", To: "settlement-manager", Mode: modeSync, Label: "getSettlementStatus()",
	})
	sc := c["settlementManager"]
	sc.Interface.Operations = append(sc.Interface.Operations, ContractOperation{
		Name: "GetSettlementStatus", Params: []ContractParam{}, Result: raw(`{"type":"object"}`), Error: true,
	})
	c["settlementManager"] = sc
	// Boundary case: queued call THEN a poll/observe step — no WALK-MODE.
	p.Scenarios[0].Cases[1].Steps = []TestStep{
		{Seq: 1, Component: "settlementManager", Operation: "CloseSettlementCycle", Inputs: []TestArg{{Name: "customerID", Value: "c", SchemaRef: "#/$defs/CustomerID"}, {Name: "cycleID", Value: "y", SchemaRef: "#/$defs/CycleID"}}, Expect: TestExpect{ErrorExpected: true}},
		{Seq: 2, Component: "settlementManager", Operation: "GetSettlementStatus", Expect: TestExpect{Result: `{"done":true}`}},
	}
	if hasRuleFindings(runSTP(t, c, s, u, p), ruleSTPWalkMode) {
		t.Fatalf("a queued call followed by an observe step must NOT trip STP-WALK-MODE")
	}
}

func TestSTP_UCTrace_UnknownUseCase(t *testing.T) {
	c, s, u, p := stpParts(t)
	p.Scenarios[0].UseCase = "not-a-use-case"
	if !hasRuleFindings(runSTP(t, c, s, u, p), ruleSTPUCTrace) {
		t.Fatalf("expected STP-UC-TRACE for a useCase that resolves to no core use case")
	}
}

func TestSTP_UCTrace_NonCoreUseCaseFails(t *testing.T) {
	c, s, u, p := stpParts(t)
	u.Decisions[0].UseCase.Classification = "nonCore"
	if !hasRuleFindings(runSTP(t, c, s, u, p), ruleSTPUCTrace) {
		t.Fatalf("a scenario tracing to a non-core use case must fail STP-UC-TRACE")
	}
}

func TestSTP_CaseKind_NoAdversarialWarns(t *testing.T) {
	c, s, u, p := stpParts(t)
	p.Scenarios[0].Cases = p.Scenarios[0].Cases[:1] // happy only
	sev, ok := findingSeverity(runSTP(t, c, s, u, p), ruleSTPCaseKind)
	if !ok {
		t.Fatalf("expected STP-CASE-KIND for a scenario with no adversarial case")
	}
	if sev != SeverityWarning {
		t.Fatalf("STP-CASE-KIND for missing adversarial cover must be Warning, got %v", sev)
	}
}

func TestSTP_CaseKind_NoHappyWarns(t *testing.T) {
	c, s, u, p := stpParts(t)
	p.Scenarios[0].Cases = p.Scenarios[0].Cases[1:] // boundary only
	if !hasRuleFindings(runSTP(t, c, s, u, p), ruleSTPCaseKind) {
		t.Fatalf("expected STP-CASE-KIND for a scenario with no happy case")
	}
}

func TestSTP_CaseKind_ZeroCasesIsError(t *testing.T) {
	c, s, u, p := stpParts(t)
	p.Scenarios[0].Cases = nil
	sev, ok := findingSeverity(runSTP(t, c, s, u, p), ruleSTPCaseKind)
	if !ok || sev != SeverityError {
		t.Fatalf("a scenario with zero cases must be a STP-CASE-KIND Error, got sev=%v ok=%v", sev, ok)
	}
}

// ---- prerequisite / no-op posture ----

func TestSTP_NoOp_WhenPlanAbsent(t *testing.T) {
	c, s, u, _ := stpParts(t)
	p := stpBuild(t, c, s, u, nil)
	p.TestingState = nil
	f, err := validateSystemTestPlan(p, nil)
	if err != nil || len(f) != 0 {
		t.Fatalf("family must be a no-op when the plan is absent, got findings=%+v err=%v", f, err)
	}
}

func TestSTP_NoOp_WhenPlanHasNoScenarios(t *testing.T) {
	c, s, u, _ := stpParts(t)
	f, err := validateSystemTestPlan(stpBuild(t, c, s, u, &SystemTestPlan{}), nil)
	if err != nil || len(f) != 0 {
		t.Fatalf("family must be a no-op for a scenario-less plan, got findings=%+v err=%v", f, err)
	}
}

func TestSTP_ContractMisuse_WhenContractsMissing(t *testing.T) {
	_, s, u, p := stpParts(t)
	proj := stpBuild(t, map[string]ServiceContract{}, s, u, p)
	proj.ServiceContracts = nil
	_, err := validateSystemTestPlan(proj, nil)
	assertContractMisuse(t, err)
}

func TestSTP_ContractMisuse_WhenSystemMissing(t *testing.T) {
	c, _, u, p := stpParts(t)
	proj := Project{
		Slots:            map[string]Slot{"4": {Status: reviewCommitted, Kind: kindCoreUseCases, Model: mustJSON(t, u)}},
		ServiceContracts: c,
		TestingState:     &TestingState{SystemTestPlan: p},
	}
	_, err := validateSystemTestPlan(proj, nil)
	assertContractMisuse(t, err)
}

// ---- integration regression: the historical billing drift ----

// TestSTP_Regression_BillingDrift reproduces the pre-reconciliation shape: a scenario
// step references billingManager.closeBillingPeriod, but billingManager is a
// never-detailed-designed stub (all ops params:null) whose real settlement family was
// renamed. Both STP-STALE-CONTRACT and STP-OP-EXISTS must catch it — the regression the
// family exists for.
func TestSTP_Regression_BillingDrift(t *testing.T) {
	p, ok, err := DecodeProject(readFixture(t, "stp_billing_drift.json"))
	if err != nil || !ok {
		t.Fatalf("decode fixture: ok=%v err=%v", ok, err)
	}
	f, err := validateSystemTestPlan(p, nil)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !hasRuleFindings(f, ruleSTPStaleContract) {
		t.Fatalf("regression: expected STP-STALE-CONTRACT for the billingManager stub, got %+v", f)
	}
	if !hasRuleFindings(f, ruleSTPOpExists) {
		t.Fatalf("regression: expected STP-OP-EXISTS for the renamed operation, got %+v", f)
	}
}

// TestSTP_Regression_AbsentPlanFixtureIsNoOp proves the family stays silent on a
// committed document that carries no systemTestPlan at all.
func TestSTP_Regression_AbsentPlanFixtureIsNoOp(t *testing.T) {
	p, ok, err := DecodeProject(readFixture(t, "stp_absent_plan.json"))
	if err != nil || !ok {
		t.Fatalf("decode fixture: ok=%v err=%v", ok, err)
	}
	f, err := validateSystemTestPlan(p, nil)
	if err != nil || len(f) != 0 {
		t.Fatalf("family must be a no-op for a document with no systemTestPlan, got findings=%+v err=%v", f, err)
	}
}
