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

// ---- TP-NULL-SATISFIED ----

// todosContract is a todosAccess-shaped contract: Record mints TodoID; Recall returns a list that carries
// its owner, which it also takes, so OwnerID is not minted.
func todosContract() ServiceContract {
	raw := func(s string) json.RawMessage { return json.RawMessage(s) }
	ref := func(n string) string { return `{"$ref":"#/$defs/` + n + `"}` }
	param := func(n, schema string) ContractParam { return ContractParam{Name: n, Schema: raw(schema)} }
	return ServiceContract{
		Component: "todosAccess",
		Defs: map[string]json.RawMessage{
			"OwnerID":        raw(`{"type":"string"}`),
			"TodoID":         raw(`{"type":"string"}`),
			"Todo":           raw(`{"type":"object","properties":{"id":` + ref("TodoID") + `,"wording":{"type":"string"},"done":{"type":"boolean"}}}`),
			"TodoList":       raw(`{"type":"object","properties":{"owner":` + ref("OwnerID") + `,"todos":{"type":"array","items":` + ref("Todo") + `}}}`),
			"DiscardOutcome": raw(`{"type":"object","properties":{"discarded":{"type":"boolean"}}}`),
			"BulkOutcome":    raw(`{"type":"object","properties":{"affected":{"type":"integer"}}}`),
		},
		Interface: ContractInterface{Operations: []ContractOperation{
			{Name: "Record", Params: []ContractParam{param("owner", ref("OwnerID")), param("wording", `{"type":"string"}`)}, Result: raw(ref("Todo"))},
			{Name: "Discard", Params: []ContractParam{param("owner", ref("OwnerID")), param("todoId", ref("TodoID"))}, Result: raw(ref("DiscardOutcome"))},
			{Name: "DiscardAll", Params: []ContractParam{param("owner", ref("OwnerID")), param("todoIds", `{"type":"array","items":`+ref("TodoID")+`}`)}, Result: raw(ref("BulkOutcome"))},
			{Name: "SetAllDoneState", Params: []ContractParam{param("owner", ref("OwnerID")), param("done", `{"type":"boolean"}`)}, Result: raw(ref("BulkOutcome"))},
			{Name: "Recall", ReadOnly: true, Params: []ContractParam{param("owner", ref("OwnerID"))}, Result: raw(ref("TodoList"))},
		}},
	}
}

func todosScope(c ServiceContract) tpScope {
	look := func(name string) (ContractOperation, bool) {
		for _, op := range c.Interface.Operations {
			if op.Name == name {
				return op, true
			}
		}
		return ContractOperation{}, false
	}
	return tpScope{comp: "todosAccess", scenario: "s-P1", contract: c, op: look, l: loc(0, "x")}
}

var ownerArg = TestArg{Name: "owner", Value: lit(`"o"`)}

func recall(seq int, want string, probes ...Probe) StepBinding {
	return StepBinding{Seq: seq, Operation: "Recall", Inputs: []TestArg{ownerArg}, Expect: TestExpect{Result: want}, Probes: probes}
}

func recallProbe(want string) Probe {
	return Probe{For: "n", Component: "todosAccess", Operation: "Recall", Inputs: []TestArg{ownerArg}, Expect: TestExpect{Result: want}}
}

func todosStep(seq int, op, result string, inputs ...TestArg) StepBinding {
	return StepBinding{Seq: seq, Operation: op, Inputs: append([]TestArg{ownerArg}, inputs...), Expect: TestExpect{Result: result}}
}

func recordArrange() []ArrangeCall {
	return []ArrangeCall{ContractCall{Component: "todosAccess", Operation: "Record",
		Inputs: []TestArg{ownerArg, {Name: "wording", Value: lit(`"alpha"`)}}, Expect: TestExpect{Result: `{"wording":"alpha"}`}}}
}

const alphaList = `{"todos":[{"wording":"alpha"}]}`

func TestTP_NullSatisfied_Fires(t *testing.T) {
	sc := todosScope(todosContract())
	cases := map[string]struct {
		b    ScenarioBinding
		op   string
		want string // the criterion named
	}{
		"clear-completed: unknown ids, reads repeat (c1)": {ScenarioBinding{Arrange: recordArrange(), Steps: []StepBinding{
			recall(1, alphaList),
			todosStep(2, "DiscardAll", `{"affected":0}`, TestArg{Name: "todoIds", Value: lit(`["todo-already-gone"]`)}),
			recall(3, alphaList),
		}}, "DiscardAll", "c1"},
		"complete-all-and-undo: empty list stays empty (c1)": {ScenarioBinding{Steps: []StepBinding{
			recall(1, `{"todos":[]}`),
			todosStep(2, "SetAllDoneState", `{"affected":0}`, TestArg{Name: "done", Value: lit(`false`)}),
			recall(3, `{"todos":[]}`),
		}}, "SetAllDoneState", "c1"},
		"sweep-the-list: probes repeat the earlier read (c1)": {ScenarioBinding{Arrange: recordArrange(), Steps: []StepBinding{
			recall(1, alphaList),
			{Seq: 2, Operation: "SetAllDoneState", Inputs: []TestArg{ownerArg, {Name: "done", Value: lit(`false`)}},
				Expect: TestExpect{Result: `{"affected":0}`}, Probes: []Probe{recallProbe(alphaList), recallProbe(alphaList)}},
			recall(3, alphaList),
		}}, "SetAllDoneState", "c1"},
		"remove-a-todo: a literal minted id (c2)": {ScenarioBinding{Arrange: recordArrange(), Steps: []StepBinding{
			todosStep(1, "Discard", `{"discarded":false}`, TestArg{Name: "todoId", Value: lit(`"todo-not-in-list"`)}),
			recall(2, alphaList),
		}}, "Discard", "c2"},
		"unknown ids and no read (vacuous c1, c2)": {ScenarioBinding{Steps: []StepBinding{
			todosStep(1, "DiscardAll", `{"affected":0}`, TestArg{Name: "todoIds", Value: lit(`["todo-already-gone"]`)}),
		}}, "DiscardAll", "c1"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			fs := sc.nullSatisfiedFindings(c.b)
			if len(fs) != 1 {
				t.Fatalf("want one finding, got %+v", fs)
			}
			f := fs[0]
			if f.RuleID != ruleTPNullSatisfied || f.Severity != SeverityError ||
				!strings.Contains(f.Message, c.op) || !strings.Contains(f.Message, c.want+":") || !strings.Contains(f.Message, "s-P1") {
				t.Errorf("TP-NULL-SATISFIED is an Error naming scenario, operation and criterion %s: %+v", c.want, f)
			}
		})
	}
	// c1 and c2 both hold on the vacuous literal case: the message names both
	fs := sc.nullSatisfiedFindings(cases["unknown ids and no read (vacuous c1, c2)"].b)
	if !strings.Contains(fs[0].Message, "c1:") || !strings.Contains(fs[0].Message, "c2:") {
		t.Errorf("both criteria held: %s", fs[0].Message)
	}
}

func TestTP_NullSatisfied_Silent(t *testing.T) {
	c := todosContract()
	for _, n := range []string{"Complete", "Reopen"} {
		c.Interface.Operations = append(c.Interface.Operations, ContractOperation{Name: n,
			Params: []ContractParam{{Name: "owner", Schema: json.RawMessage(`{"$ref":"#/$defs/OwnerID"}`)}, {Name: "todoId", Schema: json.RawMessage(`{"$ref":"#/$defs/TodoID"}`)}},
			Result: json.RawMessage(`{"$ref":"#/$defs/Todo"}`)})
	}
	sc := todosScope(c)
	recordStep := StepBinding{Seq: 1, Operation: "Record", Inputs: []TestArg{ownerArg, {Name: "wording", Value: lit(`"alpha"`)}}, Expect: TestExpect{Result: `{"wording":"alpha"}`}}
	discardRef := todosStep(2, "Discard", `{"discarded":true}`, TestArg{Name: "todoId", Value: RefValue{Step: 1, Path: "id"}})
	cases := map[string]ScenarioBinding{
		"the target is created by an earlier step and discarded": {Steps: []StepBinding{recordStep, discardRef, recall(3, `{"todos":[]}`)}},
		"a non-zero result": {Arrange: recordArrange(), Steps: []StepBinding{
			recall(1, alphaList),
			todosStep(2, "SetAllDoneState", `{"affected":1}`, TestArg{Name: "done", Value: lit(`true`)}),
			recall(3, `{"todos":[{"wording":"alpha","done":true}]}`),
		}},
		"an arrange also calls the operation": {Arrange: append(recordArrange(), ContractCall{Component: "todosAccess", Operation: "Discard",
			Inputs: []TestArg{ownerArg, {Name: "todoId", Value: lit(`"x"`)}}, Expect: TestExpect{Result: `{"discarded":true}`}}),
			Steps: []StepBinding{todosStep(1, "Discard", `{"discarded":false}`, TestArg{Name: "todoId", Value: lit(`"todo-not-in-list"`)}), recall(2, alphaList)}},
		"a later read with no earlier twin differs": {Steps: []StepBinding{
			todosStep(1, "SetAllDoneState", `{"affected":0}`, TestArg{Name: "done", Value: lit(`false`)}),
			recall(2, alphaList),
		}},
		"a literal minted id whose step expects an error": {Arrange: recordArrange(), Steps: []StepBinding{
			{Seq: 1, Operation: "Discard", Inputs: []TestArg{ownerArg, {Name: "todoId", Value: lit(`"nope"`)}}, Expect: TestExpect{ErrorExpected: true, ErrorCode: "NotFound"}},
			recall(2, alphaList),
		}},
		"an empty id list is not inert and a later read differs": {Steps: []StepBinding{
			todosStep(1, "DiscardAll", `{"affected":0}`, TestArg{Name: "todoIds", Value: lit(`[]`)}),
			recall(2, alphaList),
		}},
		"an undo whose state differs from the state after the last change, though it matches an older read": {Steps: []StepBinding{
			recordStep,
			recall(2, `{"todos":[{"wording":"alpha","done":false}]}`),
			todosStep(3, "Complete", `{"done":true}`, TestArg{Name: "todoId", Value: RefValue{Step: 1, Path: "id"}}),
			recall(4, `{"todos":[{"wording":"alpha","done":true}]}`),
			todosStep(5, "Reopen", `{"done":false}`, TestArg{Name: "todoId", Value: RefValue{Step: 1, Path: "id"}}),
			recall(6, `{"todos":[{"wording":"alpha","done":false}]}`),
		}},
		"a literal minted id that a seeded fixture hands out": {
			Arrange: []ArrangeCall{SeedResource{Resource: "todos", Fixture: json.RawMessage(`{"kind":"postgres","model":{"sql":"INSERT INTO todos (id, wording, done) VALUES ('t1', 'alpha', true)"}}`)}},
			Steps: []StepBinding{
				recall(1, `{"todos":[{"wording":"alpha","done":true}]}`),
				todosStep(2, "Reopen", `{"done":false}`, TestArg{Name: "todoId", Value: lit(`"t1"`)}),
				recall(3, `{"todos":[{"wording":"alpha","done":false}]}`),
			}},
		"a literal id on a step whose target is a ref, beside an owner literal": {Steps: []StepBinding{
			recordStep,
			todosStep(2, "Complete", `{"done":true}`, TestArg{Name: "todoId", Value: RefValue{Step: 1, Path: "id"}}),
			recall(3, `{"todos":[{"wording":"alpha","done":true}]}`),
			todosStep(4, "Reopen", `{"done":false}`, TestArg{Name: "todoId", Value: RefValue{Step: 1, Path: "id"}}),
			recall(5, `{"todos":[{"wording":"alpha","done":false}]}`),
		}},
		"a literal id the plan handed out in an earlier step": {Steps: []StepBinding{
			todosStep(1, "Discard", `{"discarded":true}`, TestArg{Name: "todoId", Value: lit(`"t9"`)}),
			todosStep(2, "Reopen", `{"done":false}`, TestArg{Name: "todoId", Value: lit(`"t9"`)}),
			recall(3, `{"todos":[]}`),
		}},
		"an owner literal is not a minted id": {Steps: []StepBinding{
			recall(1, `{"todos":[]}`),
			todosStep(2, "SetAllDoneState", `{"affected":0}`, TestArg{Name: "done", Value: lit(`false`)}),
			recall(3, alphaList),
		}},
	}
	for name, b := range cases {
		t.Run(name, func(t *testing.T) {
			if fs := sc.nullSatisfiedFindings(b); len(fs) != 0 {
				t.Errorf("must not fire: %+v", fs)
			}
		})
	}
}

func TestTP_NullSatisfied_ClientChosenKey(t *testing.T) {
	raw := func(s string) json.RawMessage { return json.RawMessage(s) }
	key := `{"$ref":"#/$defs/Key"}`
	c := ServiceContract{
		Component: "todosAccess",
		Defs: map[string]json.RawMessage{
			"Key":   raw(`{"type":"string"}`),
			"Entry": raw(`{"type":"object","properties":{"key":` + key + `,"done":{"type":"boolean"}}}`),
			"Page":  raw(`{"type":"object","properties":{"entries":{"type":"array","items":{"$ref":"#/$defs/Entry"}}}}`),
		},
		Interface: ContractInterface{Operations: []ContractOperation{
			{Name: "Put", Params: []ContractParam{{Name: "key", Schema: raw(key)}, {Name: "done", Schema: raw(`{"type":"boolean"}`)}}, Result: raw(`{"$ref":"#/$defs/Entry"}`)},
			{Name: "Reopen", Params: []ContractParam{{Name: "key", Schema: raw(key)}}, Result: raw(`{"$ref":"#/$defs/Entry"}`)},
			{Name: "List", ReadOnly: true, Result: raw(`{"$ref":"#/$defs/Page"}`)},
		}},
	}
	if got := mintedDefs(c); got["Key"] {
		t.Errorf("a client-chosen key is not minted by a read that echoes it: %v", got)
	}
	k1 := TestArg{Name: "key", Value: lit(`"k1"`)}
	list := func(seq int, want string) StepBinding {
		return StepBinding{Seq: seq, Operation: "List", Expect: TestExpect{Result: want}}
	}
	b := ScenarioBinding{
		Arrange: []ArrangeCall{ContractCall{Component: "todosAccess", Operation: "Put",
			Inputs: []TestArg{k1, {Name: "done", Value: lit(`true`)}}, Expect: TestExpect{Result: `{"done":true}`}}},
		Steps: []StepBinding{
			list(1, `{"entries":[{"key":"k1","done":true}]}`),
			{Seq: 2, Operation: "Reopen", Inputs: []TestArg{k1}, Expect: TestExpect{Result: `{"done":false}`}},
			list(3, `{"entries":[{"key":"k1","done":false}]}`),
		},
	}
	if fs := todosScope(c).nullSatisfiedFindings(b); len(fs) != 0 {
		t.Errorf("a plan whose final read differs from the null run must not fire: %+v", fs)
	}
}

// The client's key arrives nested in an object parameter: Put(entry{key, done}). The key is chosen by
// the client, not minted, and the arrange literal hands it out; a later read that differs shows the
// effect, so the plan is a discriminating one.
func TestTP_NullSatisfied_ClientChosenKeyNestedInObject(t *testing.T) {
	raw := func(s string) json.RawMessage { return json.RawMessage(s) }
	key := `{"$ref":"#/$defs/Key"}`
	c := ServiceContract{
		Component: "todosAccess",
		Defs: map[string]json.RawMessage{
			"Key":   raw(`{"type":"string"}`),
			"Entry": raw(`{"type":"object","properties":{"key":` + key + `,"done":{"type":"boolean"}}}`),
			"Page":  raw(`{"type":"object","properties":{"entries":{"type":"array","items":{"$ref":"#/$defs/Entry"}}}}`),
		},
		Interface: ContractInterface{Operations: []ContractOperation{
			{Name: "Put", Params: []ContractParam{{Name: "entry", Schema: raw(`{"$ref":"#/$defs/Entry"}`)}}, Result: raw(`{"$ref":"#/$defs/Entry"}`)},
			{Name: "Reopen", Params: []ContractParam{{Name: "key", Schema: raw(key)}}, Result: raw(`{"$ref":"#/$defs/Entry"}`)},
			{Name: "List", ReadOnly: true, Result: raw(`{"$ref":"#/$defs/Page"}`)},
		}},
	}
	if got := mintedDefs(c); got["Key"] {
		t.Errorf("a def reachable through a parameter's schema is taken, not minted: %v", got)
	}
	list := func(seq int, want string) StepBinding {
		return StepBinding{Seq: seq, Operation: "List", Expect: TestExpect{Result: want}}
	}
	arrange := []ArrangeCall{ContractCall{Component: "todosAccess", Operation: "Put",
		Inputs: []TestArg{{Name: "entry", Value: lit(`{"key":"k1","done":true}`)}}, Expect: TestExpect{Result: `{"done":true}`}}}
	reopen := StepBinding{Seq: 2, Operation: "Reopen", Inputs: []TestArg{{Name: "key", Value: lit(`"k1"`)}}, Expect: TestExpect{Result: `{"done":false}`}}
	differs := ScenarioBinding{Arrange: arrange, Steps: []StepBinding{
		list(1, `{"entries":[{"key":"k1","done":true}]}`), reopen, list(3, `{"entries":[{"key":"k1","done":false}]}`)}}
	if fs := todosScope(c).nullSatisfiedFindings(differs); len(fs) != 0 {
		t.Errorf("a discriminating plan must not fire: %+v", fs)
	}
	if got := handedOut(arrange, nil, "Reopen"); !got[`"k1"`] {
		t.Errorf("a key inside an object literal is handed out: %v", got)
	}
}

// c2 yields to observation: a literal minted id the plan never handed out, but a later read differs
// from the read before the step, so the plan shows an effect.
func TestTP_NullSatisfied_InertLiteralYieldsToDifferingRead(t *testing.T) {
	obs := ScenarioBinding{Arrange: recordArrange(), Steps: []StepBinding{
		recall(1, alphaList),
		todosStep(2, "Discard", `{"discarded":false}`, TestArg{Name: "todoId", Value: lit(`"todo-not-in-list"`)}),
		recall(3, `{"todos":[]}`),
	}}
	if fs := todosScope(todosContract()).nullSatisfiedFindings(obs); len(fs) != 0 {
		t.Errorf("a later read that differs from the earlier one is evidence: %+v", fs)
	}
}

func TestTP_NullSatisfied_MintedDefs(t *testing.T) {
	got := mintedDefs(todosContract())
	if !got["TodoID"] || got["OwnerID"] {
		t.Errorf("TodoID is minted by Record and OwnerID is not: %v", got)
	}
}

func TestTP_NullSatisfied_IsRegistered(t *testing.T) {
	if !emittedRuleIDs()[ruleTPNullSatisfied] {
		t.Errorf("%s is emitted but not in emittedRuleIDs", ruleTPNullSatisfied)
	}
}

// The rule is wired into plan validation: a binding a null subject satisfies is refused through
// ValidateProjectJSON, as the RUN-031 remove-a-todo binding was admitted, and an acceptance plan's
// binding is not held to it.
func TestTP_NullSatisfied_WiredIntoValidation(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(readFixture(t, "tp_clean.json"), &doc); err != nil {
		t.Fatal(err)
	}
	contract := doc["serviceContracts"].(map[string]any)["orderManager"].(map[string]any)
	iface := contract["interface"].(map[string]any)
	iface["operations"] = append(iface["operations"].([]any), map[string]any{
		"name": "CancelOrder", "error": true,
		"result": map[string]any{"type": "object", "properties": map[string]any{"cancelled": map[string]any{"type": "boolean"}}},
		"params": []any{map[string]any{"name": "orderId", "schema": map[string]any{"$ref": "#/$defs/OrderID"}}},
	})
	plan := doc["phaseArtifacts"].(map[string]any)["testPlan"].(map[string]any)["orderManager"].(map[string]any)
	for _, b := range plan["bindings"].([]any) {
		bm := b.(map[string]any)
		if bm["scenario"] != "process-order-P2" {
			continue
		}
		st := bm["steps"].([]any)[0].(map[string]any)
		st["operation"] = "CancelOrder"
		st["expect"] = map[string]any{"result": `{"cancelled":false}`, "errorExpected": false}
		st["probes"] = []any{}
		st["inputs"] = []any{map[string]any{"name": "orderId", "value": map[string]any{"kind": "literal", "model": map[string]any{"json": "order-not-in-list"}}}}
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	fs, err := ValidateProjectJSON(raw, ValidateOptions{SupportedSurfaces: Surfaces()})
	if err != nil {
		t.Fatal(err)
	}
	var got *Finding
	for i := range fs {
		if fs[i].RuleID == ruleTPNullSatisfied {
			got = &fs[i]
		}
	}
	if got == nil || got.Severity != SeverityError || !strings.Contains(got.Message, "CancelOrder") {
		t.Fatalf("a binding a null subject satisfies must be refused with a TP-NULL-SATISFIED Error:\n%s", dumpFindings(fs))
	}

	// the same binding as an acceptance binding is not held to the rule
	p, ok, err := DecodeProject(raw)
	if err != nil || !ok {
		t.Fatalf("decode: %v", err)
	}
	all, err := DeriveScenarios(p)
	if err != nil {
		t.Fatal(err)
	}
	b := binding(t, p, "orderManager", "process-order-P2")
	env := newTPEnv(p)
	for _, s := range scenario.ForComponentAs(all, "orderManager", contractOf("orderManager", p.ServiceContracts["orderManager"]).Client) {
		if s.ID != "process-order-P2" {
			continue
		}
		if !hasRuleFindings(planBindingFindings(p, env, "orderManager", s, *b, nil, loc(0, "x"), false), ruleTPNullSatisfied) {
			t.Error("a component plan binding fires the rule")
		}
		if hasRuleFindings(planBindingFindings(p, env, "orderManager", s, *b, nil, loc(0, "x"), true), ruleTPNullSatisfied) {
			t.Error("an acceptance plan binding must not")
		}
	}
}

// ---- TP-FALSIFIABLE / the read-only discharge of TP-ACTOR-OBSERVED ----

func readWriteOps(t *testing.T) (tpScope, ContractOperation, ContractOperation) {
	t.Helper()
	p := loadProject(t, "tp2_clean.json")
	contract, look := findContract(p, "billingManager")
	read, _ := look("GetInvoice")
	write, _ := look("RecordPayment")
	if !read.ReadOnly || write.ReadOnly {
		t.Fatal("fixture: GetInvoice reads, RecordPayment writes")
	}
	return tpScope{p: p, comp: "billingManager", scenario: "s", contract: contract, l: loc(0, "x")}, read, write
}

func probeOf() Probe {
	return Probe{Component: "billingManager", Operation: "GetInvoice", For: "out-a", Expect: TestExpect{Result: `{"state":"paid"}`}}
}

func TestTPFalsifiable_ReadOnlyStepWithProbeIsRefused(t *testing.T) {
	sc, read, write := readWriteOps(t)
	asserts := TestExpect{Result: `{"state":"paid"}`}

	fs := sc.falsifiableFindings(StepBinding{Operation: "GetInvoice", Expect: asserts, Probes: []Probe{probeOf()}}, read, "sec")
	if len(fs) != 1 || fs[0].RuleID != ruleTPFalsifiable || fs[0].Severity != SeverityError {
		t.Fatalf("a read-only success step with a probe is an Error of TP-FALSIFIABLE: %v", fs)
	}
	if !strings.Contains(fs[0].Message, "sec") || !strings.Contains(fs[0].Message, "GetInvoice") {
		t.Errorf("the message names the section and the op: %s", fs[0].Message)
	}
	if fs := sc.falsifiableFindings(StepBinding{Expect: asserts}, read, "sec"); len(fs) != 0 {
		t.Errorf("a read-only step with no probe is admitted: %v", fs)
	}
	if fs := sc.falsifiableFindings(StepBinding{Expect: asserts, Probes: []Probe{probeOf()}}, write, "sec"); len(fs) != 0 {
		t.Errorf("a mutating step keeps its probes: %v", fs)
	}
	errExpect := TestExpect{ErrorExpected: true, ErrorCode: "NotFound"}
	if fs := sc.falsifiableFindings(StepBinding{Expect: errExpect, Probes: []Probe{probeOf()}}, read, "sec"); len(fs) != 0 {
		t.Errorf("an error-expected read step is untouched: %v", fs)
	}
}

func TestTPFalsifiable_IsRegistered(t *testing.T) {
	ids := emittedRuleIDs()
	if !ids[ruleTPFalsifiable] {
		t.Errorf("%s is emitted but not in emittedRuleIDs", ruleTPFalsifiable)
	}
}

func TestTPActorObserved_ReadOnlyResultDischargesMachineClient(t *testing.T) {
	sc, read, write := readWriteOps(t)
	stim := scenario.Stimulus{ActorOutcome: scenario.Outcome{
		ExpectedOutputs: []scenario.Output{{Node: "out-a", Label: "shown"}},
		ExpectedStates:  []scenario.State{{Node: "st-a", Object: "Invoice", InState: "paid"}},
	}}
	if fs := sc.actorObservedFindings(stim, StepBinding{Expect: TestExpect{Result: `{"state":"paid"}`}}, read, "sec"); len(fs) != 0 {
		t.Errorf("a read-only step asserting a result is admitted with no probe: %v", fs)
	}
	fs := sc.actorObservedFindings(stim, StepBinding{}, read, "sec")
	if len(fs) != 2 {
		t.Fatalf("a read-only step asserting nothing is refused for each outcome: %v", fs)
	}
	for _, f := range fs {
		if f.RuleID != ruleTPActorObserved || !strings.Contains(f.Message, "assert the step's result") {
			t.Errorf("TP-ACTOR-OBSERVED tells a read to assert its result: %+v", f)
		}
	}
	if fs := sc.actorObservedFindings(stim, StepBinding{Expect: TestExpect{Result: `{"state":"paid"}`}}, write, "sec"); len(fs) != 2 {
		t.Errorf("a mutating step still needs a probe per outcome: %v", fs)
	}
}

func TestTPState_ReadOnlyExemptionIsShared(t *testing.T) {
	_, read, write := readWriteOps(t)
	if !readOnlyObserves(read, StepBinding{Expect: TestExpect{Result: `{"a":1}`}}) || readOnlyObserves(read, StepBinding{}) || readOnlyObserves(write, StepBinding{Expect: TestExpect{Result: `{"a":1}`}}) {
		t.Error("readOnlyObserves is a read-only op whose expectation asserts")
	}
}

// falsifiableFinding returns the TP-FALSIFIABLE finding in fs, or nil.
func falsifiableFinding(fs []Finding) *Finding {
	for i := range fs {
		if fs[i].RuleID == ruleTPFalsifiable {
			return &fs[i]
		}
	}
	return nil
}

// A probe on a read-only success step is refused through the real validation path: the service
// plan's track-order-P1 step reads GetOrder, so a probe on it can never pass the generated
// differential.
func TestTPFalsifiable_WiredIntoValidation(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(readFixture(t, "tp_clean.json"), &doc); err != nil {
		t.Fatal(err)
	}
	plan := doc["phaseArtifacts"].(map[string]any)["testPlan"].(map[string]any)["orderManager"].(map[string]any)
	for _, b := range plan["bindings"].([]any) {
		bm := b.(map[string]any)
		if bm["scenario"] != "track-order-P1" {
			continue
		}
		st := bm["steps"].([]any)[0].(map[string]any)
		st["probes"] = []any{map[string]any{
			"for": "inv-req", "component": "billingManager", "operation": "GetInvoice",
			"inputs": []any{map[string]any{"name": "invoiceId", "value": map[string]any{"kind": "literal", "model": map[string]any{"json": "inv-o-2"}}}},
			"expect": map[string]any{"result": `{"id":"inv-o-2","state":"requested"}`, "errorExpected": false},
		}}
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	fs, err := ValidateProjectJSON(raw, ValidateOptions{SupportedSurfaces: Surfaces()})
	if err != nil {
		t.Fatal(err)
	}
	got := falsifiableFinding(fs)
	if got == nil || got.Severity != SeverityError || !strings.Contains(got.Message, "GetOrder") {
		t.Fatalf("a probe on a read-only success step must be refused with a TP-FALSIFIABLE Error:\n%s", dumpFindings(fs))
	}
}

// An acceptance plan is exempt: its steps address several components and its probes are the
// cross-actor observation, so the same shape does not draw TP-FALSIFIABLE there.
func TestTPFalsifiable_AcceptancePlanIsExempt(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(readFixture(t, "atp_clean.json"), &doc); err != nil {
		t.Fatal(err)
	}
	// orderManager.PlaceOrder is read-only here, so the acceptance step 2 that calls it (with the
	// fixture's own probes) is the shape the service-plan rule refuses.
	ops := doc["serviceContracts"].(map[string]any)["orderManager"].(map[string]any)["interface"].(map[string]any)["operations"].([]any)
	for _, o := range ops {
		if om := o.(map[string]any); om["name"] == "PlaceOrder" {
			om["readOnly"] = true
		}
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	p, ok, err := DecodeProject(raw)
	if err != nil || !ok {
		t.Fatalf("decode: ok=%v err=%v", ok, err)
	}
	b := acceptanceBinding(t, p, "process-order-P2@shopClient")
	st := &b.Steps[1]
	if st.Operation != "PlaceOrder" || len(st.Probes) == 0 {
		t.Fatalf("fixture step 2 is not a PlaceOrder step with probes: %s %d", st.Operation, len(st.Probes))
	}
	// The service plan's own read-only step (same fixture) is refused; only the acceptance
	// plan's findings (the @shopClient variant) are under test.
	for _, f := range runATP(t, p) {
		if f.RuleID == ruleTPFalsifiable && strings.Contains(f.Message, "process-order-P2@shopClient") {
			t.Fatalf("an acceptance plan must not draw TP-FALSIFIABLE: %s", f.Message)
		}
	}
}
