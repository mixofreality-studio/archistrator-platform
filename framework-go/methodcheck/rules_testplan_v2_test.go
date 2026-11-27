package methodcheck

import (
	"encoding/json"
	"strings"
	"testing"
)

// rules_testplan_v2_test.go: the TP rules of binding model v2 (FPC plan 2, Task 2.4) over
// testdata/tp2_clean.json, tp_clean plus a machine-client plan (shopClient) for the rules
// that only a Client's stimuli reach. Every rule has a Fires test that mutates the clean
// baseline and a Passes counterpart; testdata/tp2_vacuous.json is the RUN-021 shape (a plan
// whose steps assert nothing).

func intPtr(i int) *int { return &i }

func ref(step int, path string) TestArgValue { return RefValue{Step: step, Path: path} }

func TestTP2_CleanFixtureIsClean(t *testing.T) {
	p := loadProject(t, "tp2_clean.json")
	if fs := runTP(t, p); len(fs) != 0 {
		t.Fatalf("clean fixture produced findings:\n%s", dumpFindings(fs))
	}
}

func TestTP2_VacuousFixture(t *testing.T) {
	fs := runTP(t, loadProject(t, "tp2_vacuous.json"))
	// orderManager P2: brace result + 2 empty probes; billingManager P2: nothing asserted.
	wantRules(t, fs, ruleTPExpectRequired, 4)
	wantRules(t, fs, ruleTPPrincipal, 1)
	for _, f := range fs {
		if f.Severity != SeverityError {
			t.Fatalf("every TP v2 finding is an Error: %+v", f)
		}
	}
}

func TestTP_ExpectRequired_FiresOnEmptyAndBraces(t *testing.T) {
	p := loadProject(t, "tp2_clean.json")
	b := binding(t, p, "billingManager", "track-order-P1")
	for _, empty := range []string{"", "{}", "[]", "null", " { } ", `""`} {
		b.Steps[0].Expect.Result = empty
		fs := runTP(t, p)
		wantRules(t, fs, ruleTPExpectRequired, 1)
		for _, f := range fs {
			if f.RuleID == ruleTPExpectRequired && !strings.Contains(f.Message, "track-order-P1") {
				t.Errorf("TP-EXPECT-REQUIRED must locate the step: %s", f.Message)
			}
		}
	}
	// an error-expected step asserts the error, not a result
	b.Steps[0].Expect = TestExpect{ErrorExpected: true, ErrorCode: "NotFound"}
	wantRules(t, runTP(t, p), ruleTPExpectRequired, 0)
}

func TestTP_ExpectRequired_ProbeResultSatisfiesAVoidStep(t *testing.T) {
	p := loadProject(t, "tp2_clean.json")
	b := binding(t, p, "billingManager", "process-order-P2")
	if b.Steps[0].Expect.Result != "" {
		t.Fatal("fixture: RecordPayment is void and asserts through its probe")
	}
	wantRules(t, runTP(t, p), ruleTPExpectRequired, 0)
	b.Steps[0].Probes[0].Expect.Result = ""
	fs := runTP(t, p)
	// the probe asserts nothing, and with it the step asserts nothing: one finding each
	wantRules(t, fs, ruleTPExpectRequired, 2)
	b.Steps[0].Probes = nil
	wantRules(t, runTP(t, p), ruleTPExpectRequired, 1)
}

func TestTP_ArgRef_Passes(t *testing.T) {
	p := loadProject(t, "tp2_clean.json")
	b := binding(t, p, "shopClient", "process-order-P2")
	b.Steps[1].Inputs[0].Value = ref(1, "")
	wantRules(t, runTP(t, p), ruleTPArgRef, 0)
	b.Steps[1].Inputs[0].Value = RefValue{Step: 1, Probe: intPtr(0), Path: "id"}
	wantRules(t, runTP(t, p), ruleTPArgRef, 0)
}

func TestTP_ArgRef_FiresOnForwardRef(t *testing.T) {
	p := loadProject(t, "tp2_clean.json")
	b := binding(t, p, "shopClient", "process-order-P2")
	b.Steps[0].Inputs[0].Value = ref(2, "")
	wantRules(t, runTP(t, p), ruleTPArgRef, 1)
	b.Steps[0].Inputs[0].Value = ref(1, "") // a step cannot read its own result
	wantRules(t, runTP(t, p), ruleTPArgRef, 1)
	b.Steps[0].Inputs[0].Value = ref(9, "") // no such step
	wantRules(t, runTP(t, p), ruleTPArgRef, 1)
}

func TestTP_ArgRef_FiresOnBadPath(t *testing.T) {
	p := loadProject(t, "tp2_clean.json")
	b := binding(t, p, "shopClient", "process-order-P2")
	b.Steps[1].Inputs[0].Value = RefValue{Step: 1, Probe: intPtr(0), Path: "owner"} // Invoice has id, state
	wantRules(t, runTP(t, p), ruleTPArgRef, 1)
	b.Steps[1].Inputs[0].Value = RefValue{Step: 1, Path: "id"} // step 1 returns an OrderID string
	wantRules(t, runTP(t, p), ruleTPArgRef, 1)
	b.Steps[1].Inputs[0].Value = RefValue{Step: 1, Probe: intPtr(7), Path: "id"} // no such probe
	wantRules(t, runTP(t, p), ruleTPArgRef, 1)
}

func TestTP_ArgRef_ProbeMayReadItsOwnStepNotALaterProbe(t *testing.T) {
	p := loadProject(t, "tp2_clean.json")
	b := binding(t, p, "shopClient", "process-order-P2")
	b.Steps[0].Probes[0].Inputs[0].Value = ref(1, "") // the probe observes what its step returned
	wantRules(t, runTP(t, p), ruleTPArgRef, 0)
	b.Steps[0].Probes[0].Inputs[0].Value = RefValue{Step: 1, Probe: intPtr(1), Path: "id"} // probe 1 runs after probe 0
	wantRules(t, runTP(t, p), ruleTPArgRef, 1)
}

func TestTP_Arrange_FiresOnSelfStimulus(t *testing.T) {
	p := loadProject(t, "tp2_clean.json")
	b := binding(t, p, "billingManager", "process-order-P2")
	b.Arrange = []ArrangeCall{ContractCall{
		Component: "billingManager", Operation: "RecordPayment",
		Inputs: []TestArg{{Name: "invoiceId", Value: lit("inv-o-2")}, {Name: "amount", Value: lit("1200")}},
	}}
	fs := runTP(t, p)
	wantRules(t, fs, ruleTPHook, 1)
	for _, f := range fs {
		if f.RuleID == ruleTPHook && !strings.Contains(f.Message, "RecordPayment") {
			t.Errorf("TP-HOOK must name the op: %s", f.Message)
		}
	}
	// arranging through a different operation of the same component is fine
	b.Arrange = []ArrangeCall{ContractCall{
		Component: "billingManager", Operation: "GetInvoice", As: "customer",
		Inputs: []TestArg{{Name: "invoiceId", Value: lit("inv-o-2")}},
	}}
	fs = runTP(t, p)
	wantRules(t, fs, ruleTPHook, 0)
	wantRules(t, fs, ruleTPArrange, 0)
}

func TestTP_Hook_LegacyFieldDecodeNamesTheRule(t *testing.T) {
	raw := string(readFixture(t, "tp2_clean.json"))
	for _, field := range []string{"hook", "unobservable"} {
		mutated := strings.Replace(raw, `"probes": []`, `"probes": [], "`+field+`": true`, 1)
		if mutated == raw {
			t.Fatal("fixture has no empty probes list to mutate")
		}
		_, _, err := DecodeProject([]byte(mutated))
		if err == nil || !strings.Contains(err.Error(), "TP-HOOK") || !strings.Contains(err.Error(), field) {
			t.Fatalf("a step carrying %q must be refused naming TP-HOOK and the field, got %v", field, err)
		}
	}
}

func TestTP_Arrange_Rules(t *testing.T) {
	p := loadProject(t, "tp2_clean.json")
	sys, _, err := p.system()
	if err != nil {
		t.Fatal(err)
	}
	sys.Components = append(sys.Components, Component{ID: "order-db", Name: "OrderDB", Kind: kindResource, Layer: layerResource})
	p = withSystem(t, p, sys)
	b := binding(t, p, "billingManager", "process-order-P2")
	good := []ArrangeCall{
		ContractCall{Component: "orderManager", Operation: "GetOrder", As: "customer", Inputs: []TestArg{{Name: "orderId", Value: lit("o-2")}}},
		AdvanceClock{Duration: "90s"},
		FailNext{Component: "orderManager", Operation: "PlaceOrder", ErrorCode: "Boom"},
		SeedResource{Resource: "OrderDB", Fixture: json.RawMessage(`{"kind":"postgres","model":{"sql":"INSERT INTO orders (id) VALUES ('o-2')"}}`)},
		SeedResource{Resource: "OrderDB", Fixture: json.RawMessage(`{"kind":"github","model":{"owner":"acme","name":"shop","files":{"README.md":"hi"}}}`)},
		SeedResource{Resource: "OrderDB", Fixture: json.RawMessage(`{"kind":"gitRepo","model":{"files":{"a.txt":"x"}}}`)},
	}
	b.Arrange = good
	wantRules(t, runTP(t, p), ruleTPArrange, 0)

	cases := map[string]ArrangeCall{
		"unknown op":         ContractCall{Component: "orderManager", Operation: "Nope"},
		"unknown component":  ContractCall{Component: "ledgerManager", Operation: "GetOrder"},
		"bad arg name":       ContractCall{Component: "orderManager", Operation: "GetOrder", As: "customer", Inputs: []TestArg{{Name: "id", Value: lit("o-2")}}},
		"bad arg type":       ContractCall{Component: "billingManager", Operation: "GetInvoice", As: "customer", Inputs: []TestArg{{Name: "invoiceId", Value: lit(`{"a":1}`)}}},
		"bad duration":       AdvanceClock{Duration: "soon"},
		"failNext no op":     FailNext{Component: "orderManager", Operation: "Nope", ErrorCode: "Boom"},
		"seed no resource":   SeedResource{Resource: "ghost", Fixture: json.RawMessage(goodSeed)},
		"seed a manager":     SeedResource{Resource: "orderManager", Fixture: json.RawMessage(goodSeed)},
		"seed domain json":   SeedResource{Resource: "OrderDB", Fixture: json.RawMessage(`{"owner":"o-1","orders":[{"id":"o-2"}]}`)},
		"seed unknown kind":  SeedResource{Resource: "OrderDB", Fixture: json.RawMessage(`{"kind":"redis","model":{"sql":"x"}}`)},
		"seed empty sql":     SeedResource{Resource: "OrderDB", Fixture: json.RawMessage(`{"kind":"postgres","model":{"sql":""}}`)},
		"seed unknown field": SeedResource{Resource: "OrderDB", Fixture: json.RawMessage(`{"kind":"postgres","model":{"sql":"x","rows":2}}`)},
	}
	for name, c := range cases {
		b.Arrange = []ArrangeCall{c}
		if n := countRule(runTP(t, p), ruleTPArrange); n < 1 {
			t.Errorf("%s: TP-ARRANGE did not fire", name)
		}
	}
}

const goodSeed = `{"kind":"postgres","model":{"sql":"SELECT 1"}}`

// A refused fixture's finding names the resource, the decode error and the three seed shapes.
func TestTP2_ArrangeSeedFindingNamesShapes(t *testing.T) {
	p := loadProject(t, "tp2_clean.json")
	sys, _, err := p.system()
	if err != nil {
		t.Fatal(err)
	}
	sys.Components = append(sys.Components, Component{ID: "order-db", Name: "OrderDB", Kind: kindResource, Layer: layerResource})
	p = withSystem(t, p, sys)
	b := binding(t, p, "billingManager", "process-order-P2")
	b.Arrange = []ArrangeCall{SeedResource{Resource: "OrderDB", Fixture: json.RawMessage(`{"rows":2}`)}}
	var got string
	for _, f := range runTP(t, p) {
		if f.RuleID == ruleTPArrange {
			got = f.Message
		}
	}
	for _, want := range []string{`"OrderDB"`, "resource seed", `"kind":"postgres"`, `"kind":"github"`, `"kind":"gitRepo"`} {
		if !strings.Contains(got, want) {
			t.Errorf("finding %q lacks %q", got, want)
		}
	}
}

func countRule(fs []Finding, id RuleID) int {
	n := 0
	for _, f := range fs {
		if f.RuleID == id {
			n++
		}
	}
	return n
}

func TestTP_Probe_RequiresReadOnly(t *testing.T) {
	p := loadProject(t, "tp2_clean.json")
	probes := 0
	for _, rec := range p.PhaseArtifacts.TestPlan {
		for _, b := range rec.Bindings {
			for _, st := range b.Steps {
				probes += len(st.Probes)
			}
		}
	}
	sc := p.ServiceContracts["billingManager"]
	for i := range sc.Interface.Operations {
		if sc.Interface.Operations[i].Name == "GetInvoice" {
			sc.Interface.Operations[i].ReadOnly = false
		}
	}
	p.ServiceContracts["billingManager"] = sc
	fs := runTP(t, p)
	wantRules(t, fs, ruleTPProbe, probes) // every probe in the fixture observes through GetInvoice
	for _, f := range fs {
		if f.RuleID == ruleTPProbe && !strings.Contains(f.Message, "read-only") {
			t.Errorf("TP-PROBE must say read-only: %s", f.Message)
		}
	}
}

func TestTP_Probe_ForMustBeOnThePath(t *testing.T) {
	p := loadProject(t, "tp2_clean.json")
	b := binding(t, p, "billingManager", "process-order-P2")
	b.Steps[0].Probes[0].For = "reject" // a node of the other branch
	fs := runTP(t, p)
	wantRules(t, fs, ruleTPProbe, 1)
	wantRules(t, fs, ruleTPState, 1) // and pay-paid is now unproved
}

func TestTP_State_NoUnobservableEscape(t *testing.T) {
	p := loadProject(t, "tp2_clean.json")
	b := binding(t, p, "billingManager", "process-order-P2")
	b.Steps[0].Probes = nil
	wantRules(t, runTP(t, p), ruleTPState, 1)
	// the removed `unobservable` field cannot excuse the missing probe: it does not decode
	raw := strings.Replace(string(readFixture(t, "tp2_clean.json")), `"probes": []`, `"probes": [], "unobservable": ["pay-paid"]`, 1)
	if _, _, err := DecodeProject([]byte(raw)); err == nil {
		t.Fatal("a step carrying unobservable must not decode")
	}
}

func TestTP_ActorObserved_Fires(t *testing.T) {
	p := loadProject(t, "tp2_clean.json")
	b := binding(t, p, "shopClient", "process-order-P2")
	wantRules(t, runTP(t, p), ruleTPActorObserved, 0)
	// step 1 shows the customer an invoice signal and an [requested] invoice
	b.Steps[0].Probes = b.Steps[0].Probes[:1] // keep only send-invoice
	fs := runTP(t, p)
	wantRules(t, fs, ruleTPActorObserved, 1)
	for _, f := range fs {
		if f.RuleID == ruleTPActorObserved && !strings.Contains(f.Message, "inv-req") {
			t.Errorf("TP-ACTOR-OBSERVED must name the unobserved node: %s", f.Message)
		}
	}
	b.Steps[0].Probes = nil
	wantRules(t, runTP(t, p), ruleTPActorObserved, 2)
}

func TestTP_ActorObserved_WebClientNeedsUIObservable(t *testing.T) {
	p := loadProject(t, "tp2_clean.json")
	sys, _, err := p.system()
	if err != nil {
		t.Fatal(err)
	}
	for i := range sys.Components {
		if sys.Components[i].ID == "shop-client" {
			sys.Components[i].Surface = SurfaceWeb
		}
	}
	p = withSystem(t, p, sys)
	sc := p.ServiceContracts["shopClient"]
	sc.Defs = map[string]json.RawMessage{
		"UIAction":      json.RawMessage(`{"type":"object"}`),
		"UIObservation": json.RawMessage(`{"type":"array"}`),
	}
	for i := range sc.Interface.Operations {
		sc.Interface.Operations[i].Params = []ContractParam{{Name: "action", Schema: json.RawMessage(`{"$ref":"#/$defs/UIAction"}`)}}
		sc.Interface.Operations[i].Result = json.RawMessage(`{"$ref":"#/$defs/UIObservation"}`)
	}
	p.ServiceContracts["shopClient"] = sc
	p.PhaseArtifacts.UIDesign = map[string]UIDesignRecord{"shopClient": {Surface: "shopClient", Screens: []UIScreen{{
		ID: "orders", Route: "/", Elements: []UIElement{
			{ID: "invoice-sent", Role: UIRoleStatus, Name: "Invoice sent"},
			{ID: "paid", Role: UIRoleCheckbox, Name: "Paid"},
			{ID: "pay", Role: UIRoleButton, Name: "Pay"},
		}}}}}
	b := binding(t, p, "shopClient", "process-order-P2")
	for i := range b.Steps {
		b.Steps[i].Inputs = []TestArg{{Name: "action", Value: lit(act("click", tgt("pay")))}}
	}
	b.Steps[0].Expect = TestExpect{Result: `[` + act("visible", tgt("invoice-sent")) + `]`}
	b.Steps[1].Expect = TestExpect{Result: `[` + act("checked", tgt("paid")) + `]`}
	wantRules(t, runTP(t, p), ruleTPActorObserved, 0)
	b.Steps[0].Expect = TestExpect{Result: `{"unknown":"x"}`}
	b.Steps[1].Expect = TestExpect{ErrorExpected: true, ErrorCode: "Nope"}
	wantRules(t, runTP(t, p), ruleTPActorObserved, 2) // neither step asserts what the customer sees
}

func TestTP_Principal_Fires(t *testing.T) {
	p := loadProject(t, "tp2_clean.json")
	wantRules(t, runTP(t, p), ruleTPPrincipal, 0)
	rec := p.PhaseArtifacts.TestPlan["orderManager"]
	rec.Principals = map[string]TestPrincipal{}
	p.PhaseArtifacts.TestPlan["orderManager"] = rec
	fs := runTP(t, p)
	wantRules(t, fs, ruleTPPrincipal, 1) // once for the actor, not once per stimulus
	for _, f := range fs {
		if f.RuleID == ruleTPPrincipal && !strings.Contains(f.Message, "customer") {
			t.Errorf("TP-PRINCIPAL must name the actor: %s", f.Message)
		}
	}
	rec.Principals = map[string]TestPrincipal{"customer": {Subject: ""}}
	p.PhaseArtifacts.TestPlan["orderManager"] = rec
	wantRules(t, runTP(t, p), ruleTPPrincipal, 1)
}

func TestTP_Principal_SkippedScenarioNeedsNone(t *testing.T) {
	p := loadProject(t, "tp2_clean.json")
	rec := p.PhaseArtifacts.TestPlan["orderManager"]
	rec.Principals = map[string]TestPrincipal{}
	for i := range rec.Bindings {
		rec.Bindings[i].Arrange = nil
		rec.Bindings[i].Steps = nil
		rec.Bindings[i].Skip = &SkipReason{Reason: "waits on the client", Until: "act-later"}
	}
	p.PhaseArtifacts.TestPlan["orderManager"] = rec
	wantRules(t, runTP(t, p), ruleTPPrincipal, 0)
}

func TestTP_V2RulesAreRegistered(t *testing.T) {
	ids := emittedRuleIDs()
	for _, id := range append([]RuleID{
		ruleTPHook, ruleTPExpectRequired, ruleTPArgRef, ruleTPArrange, ruleTPActorObserved, ruleTPPrincipal,
		ruleTPClientOps, ruleTPLabelOp, ruleClientMachineMirror,
	}, uiDesignRuleIDs...) {
		if !ids[id] {
			t.Errorf("%s is emitted but not in emittedRuleIDs", id)
		}
	}
}

// ---- TP-ARRANGE: who a contract call runs as ----

// ledgerAccessProject is tp2_clean plus a ResourceAccess, ledgerAccess, with one operation Get: the
// callee of an arrange that reads no principal.
func ledgerAccessProject(t *testing.T) Project {
	t.Helper()
	p := loadProject(t, "tp2_clean.json")
	sys, _, err := p.system()
	if err != nil {
		t.Fatal(err)
	}
	sys.Components = append(sys.Components, Component{ID: "ledger-access", Name: "LedgerAccess", Kind: kindResourceAccess, Layer: layerResourceAccess})
	p = withSystem(t, p, sys)
	p.ServiceContracts["ledgerAccess"] = ServiceContract{
		Component: "ledgerAccess", Layer: layerResourceAccess,
		Interface: ContractInterface{Name: "LedgerAccess", Layer: layerResourceAccess, Operations: []ContractOperation{{Name: "Get", ReadOnly: true}}},
	}
	return p
}

func arrangeFindingsOf(t *testing.T, p Project, c ContractCall) []Finding {
	t.Helper()
	binding(t, p, "billingManager", "process-order-P2").Arrange = []ArrangeCall{c}
	var out []Finding
	for _, f := range runTP(t, p) {
		if f.RuleID == ruleTPArrange {
			out = append(out, f)
		}
	}
	return out
}

func TestTPArrange_ManagerCallWithoutAsIsRefused(t *testing.T) {
	p := loadProject(t, "tp2_clean.json")
	fs := arrangeFindingsOf(t, p, ContractCall{Component: "orderManager", Operation: "GetOrder", Inputs: []TestArg{{Name: "orderId", Value: lit("o-2")}}})
	if len(fs) != 1 || fs[0].Severity != SeverityError {
		t.Fatalf("a Manager call with no as is one TP-ARRANGE Error: %v", fs)
	}
	for _, want := range []string{"billingManager process-order-P2 arrange 0", "orderManager.GetOrder", "customer", "seedResource"} {
		if !strings.Contains(fs[0].Message, want) {
			t.Errorf("the message names %q: %s", want, fs[0].Message)
		}
	}
}

func TestTPArrange_UnknownAsIsRefused(t *testing.T) {
	p := loadProject(t, "tp2_clean.json")
	fs := arrangeFindingsOf(t, p, ContractCall{Component: "orderManager", Operation: "GetOrder", As: "stranger", Inputs: []TestArg{{Name: "orderId", Value: lit("o-2")}}})
	if len(fs) != 1 || !strings.Contains(fs[0].Message, `"stranger"`) || !strings.Contains(fs[0].Message, "customer") {
		t.Fatalf("an as that is none of the plan's principals is refused, naming it and the principals: %v", fs)
	}
}

func TestTPArrange_PlanPrincipalIsAdmitted(t *testing.T) {
	p := loadProject(t, "tp2_clean.json")
	if fs := arrangeFindingsOf(t, p, ContractCall{Component: "orderManager", Operation: "GetOrder", As: "customer", Inputs: []TestArg{{Name: "orderId", Value: lit("o-2")}}}); len(fs) != 0 {
		t.Fatalf("a plan principal is admitted: %v", fs)
	}
}

func TestTPArrange_ClientCallNeedsAs(t *testing.T) {
	p := loadProject(t, "tp2_clean.json")
	ops := p.ServiceContracts["shopClient"].Interface.Operations
	if len(ops) == 0 {
		t.Skip("fixture client has no operation")
	}
	fs := arrangeFindingsOf(t, p, ContractCall{Component: "shopClient", Operation: ops[0].Name})
	if countRule(fs, ruleTPArrange) < 1 || !strings.Contains(fs[0].Message, "no `as`") {
		t.Fatalf("a Client call with no as is refused: %v", fs)
	}
}

// A facet contract's key is no System component and its layer is capitalized: the callee's
// layer still resolves through the facet's parent component, so the call needs `as`.
func TestTPArrange_FacetContractCallNeedsAs(t *testing.T) {
	p := loadProject(t, "tp2_clean.json")
	base := p.ServiceContracts["orderManager"]
	p.ServiceContracts["orderManagerFacet"] = ServiceContract{Component: "orderManager", Layer: "Manager", Interface: base.Interface}
	in := []TestArg{{Name: "orderId", Value: lit("o-2")}}
	fs := arrangeFindingsOf(t, p, ContractCall{Component: "orderManagerFacet", Operation: "GetOrder", Inputs: in})
	if len(fs) != 1 || !strings.Contains(fs[0].Message, "no `as`") {
		t.Fatalf("a facet Manager call with no as is refused: %v", fs)
	}
	if fs := arrangeFindingsOf(t, p, ContractCall{Component: "orderManagerFacet", Operation: "GetOrder", As: "customer", Inputs: in}); len(fs) != 0 {
		t.Fatalf("a facet Manager call as a plan principal is admitted: %v", fs)
	}
}

// A contract whose component is no System component falls back to its own capitalized layer.
func TestTPArrange_OrphanFacetUsesItsOwnLayer(t *testing.T) {
	p := loadProject(t, "tp2_clean.json")
	base := p.ServiceContracts["orderManager"]
	p.ServiceContracts["orphanFacet"] = ServiceContract{Component: "noSuchParent", Layer: "Client", Interface: base.Interface}
	fs := arrangeFindingsOf(t, p, ContractCall{Component: "orphanFacet", Operation: "GetOrder", Inputs: []TestArg{{Name: "orderId", Value: lit("o-2")}}})
	if len(fs) != 1 || !strings.Contains(fs[0].Message, "no `as`") {
		t.Fatalf("a capitalized Client layer is read as a Client: %v", fs)
	}
}

func TestTPArrange_ResourceAccessCallTakesNoAs(t *testing.T) {
	p := ledgerAccessProject(t)
	fs := arrangeFindingsOf(t, p, ContractCall{Component: "ledgerAccess", Operation: "Get", As: "customer"})
	if len(fs) != 1 || !strings.Contains(fs[0].Message, "drop `as`") || !strings.Contains(fs[0].Message, "ledgerAccess.Get") {
		t.Fatalf("a ResourceAccess call with as is refused: %v", fs)
	}
	if fs := arrangeFindingsOf(t, p, ContractCall{Component: "ledgerAccess", Operation: "Get"}); len(fs) != 0 {
		t.Fatalf("a ResourceAccess call without as is admitted: %v", fs)
	}
}

// The rule is reached through the document validation a write goes through.
func TestTPArrange_ReachedThroughValidateProjectJSON(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(readFixture(t, "tp2_clean.json"), &doc); err != nil {
		t.Fatal(err)
	}
	plan := doc["phaseArtifacts"].(map[string]any)["testPlan"].(map[string]any)["billingManager"].(map[string]any)
	for _, b := range plan["bindings"].([]any) {
		bm := b.(map[string]any)
		if bm["scenario"] == "process-order-P2" {
			bm["arrange"] = []any{map[string]any{"kind": "contractCall", "model": map[string]any{
				"component": "orderManager", "operation": "GetOrder",
				"inputs": []any{map[string]any{"name": "orderId", "value": map[string]any{"kind": "literal", "model": map[string]any{"json": "o-2"}}}},
				"expect": map[string]any{"errorExpected": false},
			}}}
		}
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	fs, err := ValidateProjectJSON(raw, ValidateOptions{SupportedSurfaces: Surfaces()})
	if err != nil {
		t.Fatal(err)
	}
	if n := countRule(fs, ruleTPArrange); n != 1 {
		t.Fatalf("a Manager arrange with no as is one TP-ARRANGE finding through ValidateProjectJSON, got %d:\n%s", n, dumpFindings(fs))
	}
}
