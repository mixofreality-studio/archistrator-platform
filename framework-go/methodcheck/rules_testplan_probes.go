package methodcheck

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"unicode"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
)

// rules_testplan_probes.go is the TP rules of FPC plan 2, Task 2.13c: the ways a probe or an
// expectation can look like evidence and be none. A probe that re-asks the question the step just
// answered agrees with itself (TP-PROBE-SELF); a probe on a long-poll verb hangs the test that
// generated it (TP-PROBE-POLLING); an agent with no delegation is refused at run time
// (TP-PRINCIPAL-DELEGATION, in principalFindings); an expectation an operation that did nothing
// already satisfies cannot tell the operation from its absence (TP-EXPECT-ZERO), and a plan whose
// every observation a null operation satisfies as well proves nothing of it (TP-NULL-SATISFIED).

const (
	// ruleTPProbeSelf: a probe may not call the step's own stimulus operation on the same component
	// with the same inputs. Error.
	ruleTPProbeSelf RuleID = "TP-PROBE-SELF"
	// ruleTPProbePolling: a probe on a Utility long-poll verb (Subscribe, Await, Wait*) blocks. Warning.
	ruleTPProbePolling RuleID = "TP-PROBE-POLLING"
	// ruleTPPrincipalDelegation: an agent actor's principal must declare onBehalfOf. Error.
	ruleTPPrincipalDelegation RuleID = "TP-PRINCIPAL-DELEGATION"
	// ruleTPExpectZero: an expectation equal to the operation's zero result is almost certainly
	// vacuous. Warning. A zero step is legitimate only beside evidence TP-NULL-SATISFIED accepts.
	ruleTPExpectZero RuleID = "TP-EXPECT-ZERO"
	// ruleTPNullSatisfied: a stimulus operation whose every step expects its zero result and whose
	// effect the plan never observes is indistinguishable from a null subject. Error.
	ruleTPNullSatisfied RuleID = "TP-NULL-SATISFIED"
)

// tpWarning builds a Warning finding of the family.
func tpWarning(id RuleID, l *Location, format string, args ...any) Finding {
	return Finding{RuleID: id, Severity: SeverityWarning, Message: fmt.Sprintf(format, args...), Location: l}
}

// ---- TP-PRINCIPAL-DELEGATION (classification half; the check is in principalFindings) ----

// agentWords are the words of an actor's role that classify it as an automated agent.
var agentWords = map[string]bool{"agent": true, "agents": true, "assistant": true, "assistants": true}

// isAgentActor reports an actor the use case classifies as an agent: kind "agent", or a role
// with the word agent or assistant in it.
func isAgentActor(a Actor) bool {
	if strings.EqualFold(a.Kind, "agent") {
		return true
	}
	words := strings.FieldsFunc(strings.ToLower(a.Role), func(r rune) bool { return !unicode.IsLetter(r) })
	for _, w := range words {
		if agentWords[w] {
			return true
		}
	}
	return false
}

// ---- TP-PROBE-SELF ----

// probeSelfFindings is TP-PROBE-SELF: a probe that calls the step's own operation on the step's own
// component with the same inputs reads the very thing the step returned, so it is true whether or not
// the step did anything.
func (sc tpScope) probeSelfFindings(st StepBinding, section string) []Finding {
	var out []Finding
	for i, pr := range st.Probes {
		if pr.Operation != st.Operation || !sc.isOwnComponent(pr.Component) || !sameInputs(pr.Inputs, st.Inputs) {
			continue
		}
		out = append(out, tpFinding(ruleTPProbeSelf, sc.l,
			"%s: probe %d calls %s.%s with the step's own inputs, so it re-reads the step's result and cannot disagree with it; probe a different read (another operation or another component) that shows the step's effect", section, i, pr.Component, pr.Operation))
	}
	return out
}

// isOwnComponent reports a component name that is the plan's own (its key, or the component a facet
// contract belongs to).
func (sc tpScope) isOwnComponent(name string) bool {
	callee := scenario.Normalize(name)
	return callee == scenario.Normalize(sc.comp) || (sc.contract.Component != "" && callee == scenario.Normalize(sc.contract.Component))
}

// sameInputs reports two input lists that supply the same values to the same parameters.
func sameInputs(a, b []TestArg) bool {
	if len(a) != len(b) {
		return false
	}
	byName := make(map[string]TestArgValue, len(b))
	for _, x := range b {
		byName[x.Name] = x.Value
	}
	for _, x := range a {
		other, ok := byName[x.Name]
		if !ok || !sameValue(x.Value, other) {
			return false
		}
	}
	return true
}

// sameValue compares two argument values: literals as JSON documents, refs structurally.
func sameValue(a, b TestArgValue) bool {
	switch x := a.(type) {
	case LiteralValue:
		y, ok := b.(LiteralValue)
		return ok && sameJSON(x.JSON, y.JSON)
	case RefValue:
		y, ok := b.(RefValue)
		return ok && reflect.DeepEqual(x, y)
	case PlanRef:
		y, ok := b.(PlanRef)
		return ok && x == y
	}
	return false
}

// sameJSON compares two JSON texts as documents (whitespace and key order do not matter).
func sameJSON(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return string(a) == string(b)
	}
	return reflect.DeepEqual(x, y)
}

// ---- TP-PROBE-POLLING ----

// pollingVerbs are the Utility long-poll verbs: an operation named with one of them as its leading
// word waits for something to happen.
var pollingVerbs = []string{"Subscribe", "Await", "Wait"}

// pollingOperation reports an operation name whose leading word is a long-poll verb: the verb itself,
// or the verb followed by a capitalised word (SubscribeInvoice, WaitForInvoice).
func pollingOperation(name string) bool {
	for _, v := range pollingVerbs {
		rest, ok := strings.CutPrefix(name, v)
		if !ok {
			continue
		}
		if rest == "" || unicode.IsUpper([]rune(rest)[0]) {
			return true
		}
	}
	return false
}

// pollingFindings is TP-PROBE-POLLING for one probe's operation.
func pollingFindings(op ContractOperation, section string, l *Location) []Finding {
	if !pollingOperation(op.Name) {
		return nil
	}
	return []Finding{tpWarning(ruleTPProbePolling, l,
		"%s: %s is a long-poll verb: a probe that waits for an event can hang the test (the generated harness bounds it and fails the step as timed out); probe a non-blocking read of the same state instead", section, op.Name)}
}

// ---- TP-EXPECT-ZERO ----

// expectZeroFindings is TP-EXPECT-ZERO: an expected result that the operation's zero result (an empty
// string, zero, false, every member of an object unset) already satisfies. A null subject returns
// exactly that, so the assertion cannot tell the operation from its absence. An empty result, which
// asserts nothing at all, is TP-EXPECT-REQUIRED's. A deliberately-zero outcome is legitimate, hence
// a Warning: a zero step is legitimate only beside evidence TP-NULL-SATISFIED accepts, and that rule
// is the Error when the plan holds no such evidence.
func expectZeroFindings(expect TestExpect, op ContractOperation, contract ServiceContract, section string, l *Location) []Finding {
	if !expectsZero(expect, op, contract) {
		return nil
	}
	return []Finding{tpWarning(ruleTPExpectZero, l,
		"%s: the expected result %s is what %s returns when it does nothing (its zero result), so a null subject satisfies it; assert a value the operation has to produce, or pair the step with evidence TP-NULL-SATISFIED accepts (a read that differs from the state before it, a call that targets something an earlier step created)", section, strings.TrimSpace(expect.Result), op.Name)}
}

// expectsZero reports a non-error, non-empty expected result that the operation's zero result satisfies.
// An uncomputable zero result predicts nothing.
func expectsZero(expect TestExpect, op ContractOperation, contract ServiceContract) bool {
	if expect.ErrorExpected || emptyResult(expect.Result) || len(op.Result) == 0 {
		return false
	}
	zero, ok := zeroResult(op.Result, contract.Defs, 0)
	if !ok {
		return false
	}
	var want any
	if json.Unmarshal([]byte(expect.Result), &want) != nil {
		want = expect.Result
	}
	return zeroSatisfies(want, zero)
}

// zeroResult is the JSON form of the zero value of schema: "" for a string, 0 for a number, false for
// a boolean, an empty array, and for an object a map of its properties' zeros. ok is false for a
// schema that does not state its shape.
func zeroResult(schema json.RawMessage, defs map[string]json.RawMessage, depth int) (any, bool) {
	if depth > 8 {
		return nil, false
	}
	n, ok := openSchemaNode(schema, defs)
	if !ok {
		return nil, false
	}
	switch n.Type {
	case "string":
		return "", true
	case "integer", "number":
		return float64(0), true
	case "boolean":
		return false, true
	case "array":
		return []any{}, true
	case "object", "":
		return zeroObject(n.Properties, defs, depth)
	}
	return nil, false
}

// zeroObject is the zero result of an object schema: a map of its properties' zeros. ok is false
// when the object states no properties.
func zeroObject(props map[string]json.RawMessage, defs map[string]json.RawMessage, depth int) (any, bool) {
	if len(props) == 0 {
		return nil, false
	}
	obj := make(map[string]any, len(props))
	for name, sub := range props {
		if z, ok := zeroResult(sub, defs, depth+1); ok {
			obj[name] = z
		}
	}
	return obj, true
}

// zeroSatisfies reports whether the zero result is a JSON superset of want, with the generated
// assertion's semantics: every expected member present and matching, arrays element-wise, scalars equal.
func zeroSatisfies(want, zero any) bool {
	switch w := want.(type) {
	case map[string]any:
		return zeroSatisfiesObject(w, zero)
	case []any:
		return zeroSatisfiesArray(w, zero)
	default:
		return reflect.DeepEqual(want, zero)
	}
}

// zeroSatisfiesObject is zeroSatisfies for an expected object: every expected member present and matching.
func zeroSatisfiesObject(w map[string]any, zero any) bool {
	z, ok := zero.(map[string]any)
	if !ok {
		return false
	}
	for k, wv := range w {
		zv, ok := z[k]
		if !ok || !zeroSatisfies(wv, zv) {
			return false
		}
	}
	return true
}

// zeroSatisfiesArray is zeroSatisfies for an expected array: same length, element-wise.
func zeroSatisfiesArray(w []any, zero any) bool {
	z, ok := zero.([]any)
	if !ok || len(z) != len(w) {
		return false
	}
	for i := range w {
		if !zeroSatisfies(w[i], z[i]) {
			return false
		}
	}
	return true
}

// ---- TP-NULL-SATISFIED ----

// nullSatisfiedFindings is TP-NULL-SATISFIED. The null-subject run nulls one stimulus operation across
// the whole scenario (arrange included): it returns its zero result and has no effect, while the other
// operations are real. An operation O on the plan's own component is indistinguishable from that run
// when (a) no arrange call calls O, (b) every step calling O expects a non-error result O's zero result
// satisfies, and (c) either (c1) everything the plan observes at or after the first O step repeats an
// observation made since the last state-changing step before it, or (c2) every O step is inert: it
// addresses, by a literal, an id the contract mints, so the real operation addresses an id never handed
// out and does nothing; a binding that seeds a resource may hand that id out in its fixture, so c2 does
// not apply to it. A
// prediction the rule cannot compute (no zero result, an unknown operation) is no finding, and a
// read-only operation has no effect for the null run to remove: a read is judged by TP-EXPECT-ZERO.
func (sc tpScope) nullSatisfiedFindings(b ScenarioBinding) []Finding {
	var out []Finding
	done := map[string]bool{}
	for _, st := range b.Steps {
		if done[st.Operation] {
			continue
		}
		done[st.Operation] = true
		if f, ok := sc.nullSatisfiedOp(b, st.Operation); ok {
			out = append(out, f)
		}
	}
	return out
}

// nullSatisfiedOp judges one stimulus operation of the binding.
func (sc tpScope) nullSatisfiedOp(b ScenarioBinding, name string) (Finding, bool) {
	op, ok := sc.op(name)
	if !ok || op.ReadOnly || sc.arrangeCalls(b.Arrange, name) {
		return Finding{}, false
	}
	first, seqs, zero := sc.zeroCalls(b.Steps, name, op)
	if !zero {
		return Finding{}, false
	}
	var held []string
	if onlyRepeatsEarlier(sc, b.Steps, first, name) {
		held = append(held, "c1: everything the plan observes at or after the first call repeats an observation made before it")
	}
	if !seedsAResource(b.Arrange) && sc.allInert(b.Arrange, b.Steps, name, op) {
		held = append(held, "c2: every call addresses by a literal an id the contract mints, which no step handed out")
	}
	if len(held) == 0 {
		return Finding{}, false
	}
	return tpFinding(ruleTPNullSatisfied, sc.l,
		"%s %s: %s (steps %v) is satisfied by a null subject: it expects only its zero result and the plan observes no effect (%s); give %s an effect the plan observes: create the target in an earlier step and ref its result, expect a non-zero result, or add a later read that differs from the state before it",
		sc.comp, sc.scenario, name, seqs, strings.Join(held, "; "), name), true
}

// zeroCalls is b of the rule: the index of the first step calling name, the seqs of all of them, and
// whether every one expects a non-error result the operation's zero result satisfies (false when none).
func (sc tpScope) zeroCalls(steps []StepBinding, name string, op ContractOperation) (first int, seqs []int, zero bool) {
	first = -1
	for i, st := range steps {
		if st.Operation != name {
			continue
		}
		if !expectsZero(st.Expect, op, sc.contract) {
			return 0, nil, false
		}
		if first < 0 {
			first = i
		}
		seqs = append(seqs, st.Seq)
	}
	return first, seqs, first >= 0
}

// allInert is c2: every step calling name is inert.
func (sc tpScope) allInert(arrange []ArrangeCall, steps []StepBinding, name string, op ContractOperation) bool {
	minted := mintedDefs(sc.contract)
	for i, st := range steps {
		if st.Operation == name && !literalMintedInput(st, op, minted, handedOut(arrange, steps[:i], name)) {
			return false
		}
	}
	return true
}

// handedOut are the literal values the plan itself passes to the component before a step: the inputs of
// every arrange contract call and of every earlier step that does not call name. A value the plan hands
// out is a key or an owner the plan chose, so a later call addressing it may find something.
func handedOut(arrange []ArrangeCall, earlier []StepBinding, name string) map[string]bool {
	out := map[string]bool{}
	for _, a := range arrange {
		if c, ok := a.(ContractCall); ok {
			addLiterals(out, c.Inputs)
		}
	}
	for _, st := range earlier {
		if st.Operation != name {
			addLiterals(out, st.Inputs)
		}
	}
	return out
}

// addLiterals records the elements of every literal input into set.
func addLiterals(set map[string]bool, ins []TestArg) {
	for _, in := range ins {
		if lit, ok := in.Value.(LiteralValue); ok {
			for _, v := range literalElements(lit.JSON) {
				set[v] = true
			}
		}
	}
}

// literalElements is a literal's canonical JSON, or each element's when it is an array.
func literalElements(raw json.RawMessage) []string {
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) == nil {
		var out []string
		for _, e := range arr {
			out = append(out, literalElements(e)...)
		}
		return out
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return []string{string(b)}
}

// arrangeCalls reports an arrange call to the plan's own component operation name.
func (sc tpScope) arrangeCalls(arrange []ArrangeCall, name string) bool {
	for _, a := range arrange {
		if c, ok := a.(ContractCall); ok && c.Operation == name && sc.isOwnComponent(c.Component) {
			return true
		}
	}
	return false
}

// observation is one read the plan makes: a step or a probe, with what it expects.
type observation struct {
	component, operation string
	inputs               []TestArg
	expect               TestExpect
}

// sameObservation reports two reads of the same operation on the same inputs expecting the same outcome.
func sameObservation(a, b observation) bool {
	return scenario.Normalize(a.component) == scenario.Normalize(b.component) &&
		a.operation == b.operation && sameInputs(a.inputs, b.inputs) &&
		a.expect.ErrorExpected == b.expect.ErrorExpected && a.expect.ErrorCode == b.expect.ErrorCode &&
		sameJSON(json.RawMessage(a.expect.Result), json.RawMessage(b.expect.Result))
}

// stepObservations is the step's own call followed by its probes.
func (sc tpScope) stepObservations(st StepBinding) []observation {
	obs := []observation{{sc.comp, st.Operation, st.Inputs, st.Expect}}
	for _, pr := range st.Probes {
		obs = append(obs, observation{pr.Component, pr.Operation, pr.Inputs, pr.Expect})
	}
	return obs
}

// onlyRepeatsEarlier is c1: every step at or after index first that does not call op, and every probe
// of any step at or after it, repeats an observation of the state the first call found. That state is
// what was observed since the last state-changing step before first (that step's own probes included),
// or everything observed before first when no such step exists: an observation older than a
// state-changing step describes a state that step replaced. Vacuously true when nothing else follows.
func onlyRepeatsEarlier(sc tpScope, steps []StepBinding, first int, op string) bool {
	var before []observation
	for _, st := range steps[lastChangeBefore(sc, steps, first):first] {
		before = append(before, sc.stepObservations(st)...)
	}
	for _, st := range steps[first:] {
		obs := sc.stepObservations(st)
		if st.Operation == op {
			obs = obs[1:]
		}
		for _, o := range obs {
			if !slices.ContainsFunc(before, func(e observation) bool { return sameObservation(o, e) }) {
				return false
			}
		}
	}
	return true
}

// lastChangeBefore is the index of the last step before first that is not a read-only call (an
// unknown operation counts as a change), or 0 when every step before it only reads.
func lastChangeBefore(sc tpScope, steps []StepBinding, first int) int {
	for i := first - 1; i > 0; i-- {
		if o, ok := sc.op(steps[i].Operation); !ok || !o.ReadOnly {
			return i
		}
	}
	return 0
}

// seedsAResource reports a seedResource arrange call.
func seedsAResource(arrange []ArrangeCall) bool {
	return slices.ContainsFunc(arrange, func(a ArrangeCall) bool { _, ok := a.(SeedResource); return ok })
}

// literalMintedInput is c2 for one step: the step addresses, only through non-empty literals, an id the
// contract mints. Every input whose parameter is a minted def (or an array of it) is the one addressing
// the target: all of them must be non-empty literals the plan did not hand out, and there must be one.
// Other inputs (an owner, a flag) say nothing about the target.
func literalMintedInput(st StepBinding, op ContractOperation, minted map[string]bool, handed map[string]bool) bool {
	found := false
	for _, p := range op.Params {
		in, ok := inputNamed(st.Inputs, p.Name)
		if !ok || !minted[paramDef(p.Schema)] {
			continue
		}
		if !inertLiteral(in, handed) {
			return false
		}
		found = true
	}
	return found
}

// inertLiteral reports a non-empty literal none of whose elements the plan handed out.
func inertLiteral(in TestArg, handed map[string]bool) bool {
	lit, ok := in.Value.(LiteralValue)
	if !ok || !nonEmptyLiteral(lit.JSON) {
		return false
	}
	return !slices.ContainsFunc(literalElements(lit.JSON), func(v string) bool { return handed[v] })
}

func inputNamed(ins []TestArg, name string) (TestArg, bool) {
	for _, in := range ins {
		if in.Name == name {
			return in, true
		}
	}
	return TestArg{}, false
}

// nonEmptyLiteral reports a non-empty string or a non-empty array.
func nonEmptyLiteral(raw json.RawMessage) bool {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return false
	}
	switch x := v.(type) {
	case string:
		return x != ""
	case []any:
		return len(x) > 0
	}
	return false
}

// paramDef is the $def a parameter's schema names, directly or as the items of an array; "" when none.
func paramDef(schema json.RawMessage) string {
	if tail, ok := schemaRefTail(schema); ok {
		return tail
	}
	var n schemaNode
	if json.Unmarshal(schema, &n) != nil || n.Type != "array" {
		return ""
	}
	tail, _ := schemaRefTail(n.Items)
	return tail
}

// mintedDefs are the $defs the contract hands out: some state-changing operation returns D somewhere in
// its result and takes no parameter of D (or an array of it). A read only echoes what is stored, so it
// mints nothing. Recall of a list that carries its owner mints no
// OwnerID, since it takes one.
func mintedDefs(c ServiceContract) map[string]bool {
	minted := map[string]bool{}
	for _, op := range c.Interface.Operations {
		if op.ReadOnly {
			continue
		}
		taken := map[string]bool{}
		for _, p := range op.Params {
			taken[paramDef(p.Schema)] = true
		}
		returned := map[string]bool{}
		collectDefs(op.Result, c.Defs, 0, returned)
		for d := range returned {
			minted[d] = minted[d] || !taken[d]
		}
	}
	return minted
}

// collectDefs adds to into every $def a schema reaches through $ref, properties and items.
func collectDefs(node json.RawMessage, defs map[string]json.RawMessage, depth int, into map[string]bool) {
	if depth > 8 || len(node) == 0 {
		return
	}
	if tail, ok := schemaRefTail(node); ok {
		if into[tail] {
			return
		}
		into[tail] = true
		collectDefs(defs[tail], defs, depth+1, into)
		return
	}
	var n schemaNode
	if json.Unmarshal(node, &n) != nil {
		return
	}
	for _, sub := range n.Properties {
		collectDefs(sub, defs, depth+1, into)
	}
	collectDefs(n.Items, defs, depth+1, into)
}
