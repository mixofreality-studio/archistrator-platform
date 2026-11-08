package methodcheck

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
)

// rules_testplan_probes_test.go: the TP rules of FPC plan 2, Task 2.13c - probes that prove nothing
// (TP-PROBE-SELF), probes that hang (TP-PROBE-POLLING), agents with no delegation
// (TP-PRINCIPAL-DELEGATION) and expectations an operation that did nothing satisfies (TP-EXPECT-ZERO).
// Each has a Fires test that mutates testdata/tp2_clean.json and a passing counterpart.

// A probe that calls the step's own stimulus operation on the same component with the same inputs reads
// what the step returned: it can only agree with itself.
func TestTP_ProbeSelf_Fires(t *testing.T) {
	p := loadProject(t, "tp2_clean.json")
	b := binding(t, p, "billingManager", "track-order-P1")
	wantRules(t, runTP(t, p), ruleTPProbeSelf, 0)
	same := []TestArg{{Name: "invoiceId", Value: lit(`"inv-o-2"`)}}
	b.Steps[0].Probes = []Probe{{
		For: "n1", Component: "billingManager", Operation: "GetInvoice", Inputs: same,
		Expect: TestExpect{Result: `{"state":"paid"}`},
	}}
	fs := runTP(t, p)
	wantRules(t, fs, ruleTPProbeSelf, 1)
	for _, f := range fs {
		if f.RuleID == ruleTPProbeSelf && (f.Severity != SeverityError || !strings.Contains(f.Message, "GetInvoice")) {
			t.Errorf("TP-PROBE-SELF is an Error naming the operation: %+v", f)
		}
	}
	// the same operation on other inputs reads something else
	b.Steps[0].Probes[0].Inputs = []TestArg{{Name: "invoiceId", Value: lit(`"inv-o-3"`)}}
	wantRules(t, runTP(t, p), ruleTPProbeSelf, 0)
	// another component's operation is not the step's own
	b.Steps[0].Probes[0].Inputs = same
	b.Steps[0].Probes[0].Component = "orderManager"
	wantRules(t, runTP(t, p), ruleTPProbeSelf, 0)
}

// A probe that blocks on a long-poll verb hangs a generated test; the author is told to read a
// non-blocking operation.
func TestTP_ProbePolling_Warns(t *testing.T) {
	p := loadProject(t, "tp2_clean.json")
	wantRules(t, runTP(t, p), ruleTPProbePolling, 0)
	b := binding(t, p, "billingManager", "track-order-P1")
	for _, name := range []string{"Subscribe", "SubscribeInvoice", "AwaitPaid", "WaitForInvoice", "Wait"} {
		c := p.ServiceContracts["billingManager"]
		c.Interface.Operations = append(append([]ContractOperation(nil), c.Interface.Operations...), ContractOperation{
			Name: name, ReadOnly: true, Error: true,
			Params: []ContractParam{{Name: "invoiceId", Schema: json.RawMessage(`{"type":"string"}`)}},
			Result: json.RawMessage(`{"type":"string"}`),
		})
		p.ServiceContracts["billingManager"] = c
		b.Steps[0].Probes = []Probe{{
			For: "n1", Component: "billingManager", Operation: name,
			Inputs: []TestArg{{Name: "invoiceId", Value: lit(`"inv-o-3"`)}}, Expect: TestExpect{Result: "x"},
		}}
		fs := runTP(t, p)
		wantRules(t, fs, ruleTPProbePolling, 1)
		for _, f := range fs {
			if f.RuleID == ruleTPProbePolling && f.Severity != SeverityWarning {
				t.Errorf("TP-PROBE-POLLING is a Warning: %+v", f)
			}
		}
	}
	// a name that merely contains the verb is not a long poll
	for _, name := range []string{"GetAwaitedInvoice", "Subscriptions", "Waiter", "Awaiting"} {
		if pollingOperation(name) {
			t.Errorf("%s is not a long-poll verb", name)
		}
	}
}

// An actor the use case classifies as an agent acts on someone's behalf: its principal must name them.
func TestTP_PrincipalDelegation_Fires(t *testing.T) {
	edits := map[string]string{
		"role names an agent":     `"role": "Order Agent"`,
		"role names an assistant": `"role": "Shopping Assistant"`,
		"kind is agent":           `"role": "Customer", "kind": "agent"`,
	}
	for name, replacement := range edits {
		t.Run(name, func(t *testing.T) {
			p := agentProject(t, replacement)
			fs := runTP(t, p)
			// orderManager, billingManager and shopClient each plan the actor
			wantRules(t, fs, ruleTPPrincipalDelegation, 3)
			for _, f := range fs {
				if f.RuleID == ruleTPPrincipalDelegation && (f.Severity != SeverityError || !strings.Contains(f.Message, "customer") || !strings.Contains(f.Message, "onBehalfOf")) {
					t.Errorf("TP-PRINCIPAL-DELEGATION is an Error naming the actor and the field: %+v", f)
				}
			}
			for _, comp := range []string{"orderManager", "billingManager", "shopClient"} {
				rec := p.PhaseArtifacts.TestPlan[comp]
				pr := rec.Principals["customer"]
				pr.OnBehalfOf = "alice"
				rec.Principals["customer"] = pr
				p.PhaseArtifacts.TestPlan[comp] = rec
			}
			wantRules(t, runTP(t, p), ruleTPPrincipalDelegation, 0)
		})
	}
}

// agentProject is tp2_clean with its actor's role line replaced.
func agentProject(t *testing.T, replacement string) Project {
	t.Helper()
	raw := strings.ReplaceAll(string(readFixture(t, "tp2_clean.json")), `"role": "Customer"`, replacement)
	p, ok, err := DecodeProject([]byte(raw))
	if err != nil || !ok {
		t.Fatalf("decode: ok=%v err=%v", ok, err)
	}
	return p
}

// A human actor needs no delegation, and a missing principal is TP-PRINCIPAL's finding, not this rule's.
func TestTP_PrincipalDelegation_HumanAndMissingAreNotThisRule(t *testing.T) {
	p := loadProject(t, "tp2_clean.json")
	wantRules(t, runTP(t, p), ruleTPPrincipalDelegation, 0)

	p = agentProject(t, `"role": "Order Agent"`)
	rec := p.PhaseArtifacts.TestPlan["orderManager"]
	rec.Principals = map[string]TestPrincipal{}
	p.PhaseArtifacts.TestPlan["orderManager"] = rec
	fs := runTP(t, p)
	wantRules(t, fs, ruleTPPrincipal, 1)
	wantRules(t, fs, ruleTPPrincipalDelegation, 2) // billingManager and shopClient still declare none
}

// An expectation equal to the operation's zero result is satisfied by an operation that did nothing.
func TestTP_ExpectZero_Warns(t *testing.T) {
	p := loadProject(t, "tp2_clean.json")
	wantRules(t, runTP(t, p), ruleTPExpectZero, 0)
	b := binding(t, p, "orderManager", "track-order-P1")
	for _, zero := range []string{`{"id":""}`, `{"total":0}`, `{"id":"","status":"","total":0}`} {
		b.Steps[0].Expect = TestExpect{Result: zero}
		fs := runTP(t, p)
		wantRules(t, fs, ruleTPExpectZero, 1)
		for _, f := range fs {
			if f.RuleID == ruleTPExpectZero && (f.Severity != SeverityWarning || !strings.Contains(f.Message, "GetOrder")) {
				t.Errorf("TP-EXPECT-ZERO is a Warning naming the operation: %+v", f)
			}
		}
	}
	// one field a nothing-doing operation cannot produce makes the expectation discriminating
	b.Steps[0].Expect = TestExpect{Result: `{"id":"o-2","total":0}`}
	wantRules(t, runTP(t, p), ruleTPExpectZero, 0)
	// an expected error is not a result
	b.Steps[0].Expect = TestExpect{ErrorExpected: true, ErrorCode: "NotFound"}
	wantRules(t, runTP(t, p), ruleTPExpectZero, 0)
	// a probe's expectation is judged the same way
	pb := binding(t, p, "billingManager", "process-order-P2")
	pb.Steps[0].Probes[0].Expect = TestExpect{Result: `{"state":""}`}
	wantRules(t, runTP(t, p), ruleTPExpectZero, 1)
}

func TestTP_2_13cRulesAreRegistered(t *testing.T) {
	ids := emittedRuleIDs()
	for _, id := range []RuleID{ruleTPProbeSelf, ruleTPProbePolling, ruleTPPrincipalDelegation, ruleTPExpectZero} {
		if !ids[id] {
			t.Errorf("%s is emitted but not in emittedRuleIDs", id)
		}
	}
}

// onBehalfOf is part of the record: it decodes and is not written when unset.
func TestTestPrincipal_OnBehalfOfRoundTrips(t *testing.T) {
	var pr TestPrincipal
	if err := json.Unmarshal([]byte(`{"subject":"agent-1","roles":["agent"],"onBehalfOf":"alice"}`), &pr); err != nil {
		t.Fatal(err)
	}
	if pr.OnBehalfOf != "alice" {
		t.Fatalf("onBehalfOf = %q", pr.OnBehalfOf)
	}
	raw, err := json.Marshal(TestPrincipal{Subject: "alice", Roles: []string{"member"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "onBehalfOf") {
		t.Errorf("an unset onBehalfOf must not be written: %s", raw)
	}
}

// A read-only operation observes through its own result: a step on one whose expectation asserts
// something needs no probe to discharge the outputs and states it reaches (a probe could only re-read the
// same thing, which TP-PROBE-SELF refuses). A step on an operation that can change state still needs one.
func TestTP_State_ReadOnlyStepObservesItself(t *testing.T) {
	p := loadProject(t, "tp2_clean.json")
	contract, look := findContract(p, "billingManager")
	read, _ := look("GetInvoice")
	write, _ := look("RecordPayment")
	if !read.ReadOnly || write.ReadOnly {
		t.Fatal("fixture: GetInvoice reads, RecordPayment writes")
	}
	sc := tpScope{p: p, comp: "billingManager", scenario: "s", contract: contract, l: loc(0, "x")}
	stim := scenario.Stimulus{Outcome: scenario.Outcome{ExpectedOutputs: []scenario.Output{{Node: "out-a", Label: "shown"}}}}

	if fs := sc.stateFindings("sec", stim, StepBinding{Expect: TestExpect{Result: `{"state":"paid"}`}}, read, nil); len(fs) != 0 {
		t.Errorf("a read step that asserts a result observes its own outputs: %v", fs)
	}
	if fs := sc.stateFindings("sec", stim, StepBinding{Expect: TestExpect{ErrorExpected: true, ErrorCode: "X"}}, read, nil); len(fs) != 0 {
		t.Errorf("a read step that asserts an error observes its own outputs: %v", fs)
	}
	if fs := sc.stateFindings("sec", stim, StepBinding{}, read, nil); len(fs) != 1 {
		t.Errorf("a read step that asserts nothing observes nothing: %v", fs)
	}
	if fs := sc.stateFindings("sec", stim, StepBinding{Expect: TestExpect{Result: `{"state":"paid"}`}}, write, nil); len(fs) != 1 {
		t.Errorf("a step that changes state needs a probe for each output: %v", fs)
	}
	if fs := sc.stateFindings("sec", stim, StepBinding{}, write, map[string]bool{"out-a": true}); len(fs) != 0 {
		t.Errorf("a probe discharges the output: %v", fs)
	}
}
