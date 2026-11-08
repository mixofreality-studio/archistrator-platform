package methodcheck

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"unicode"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
)

// rules_testplan_probes.go is the TP rules of FPC plan 2, Task 2.13c: the ways a probe or an
// expectation can look like evidence and be none. A probe that re-asks the question the step just
// answered agrees with itself (TP-PROBE-SELF); a probe on a long-poll verb hangs the test that
// generated it (TP-PROBE-POLLING); an agent with no delegation is refused at run time
// (TP-PRINCIPAL-DELEGATION, in principalFindings); an expectation an operation that did nothing
// already satisfies cannot tell the operation from its absence (TP-EXPECT-ZERO).

const (
	// ruleTPProbeSelf: a probe may not call the step's own stimulus operation on the same component
	// with the same inputs. Error.
	ruleTPProbeSelf RuleID = "TP-PROBE-SELF"
	// ruleTPProbePolling: a probe on a Utility long-poll verb (Subscribe, Await, Wait*) blocks. Warning.
	ruleTPProbePolling RuleID = "TP-PROBE-POLLING"
	// ruleTPPrincipalDelegation: an agent actor's principal must declare onBehalfOf. Error.
	ruleTPPrincipalDelegation RuleID = "TP-PRINCIPAL-DELEGATION"
	// ruleTPExpectZero: an expectation equal to the operation's zero result is almost certainly
	// vacuous. Warning.
	ruleTPExpectZero RuleID = "TP-EXPECT-ZERO"
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
// a Warning: pair it with a member the zero value cannot hold.
func expectZeroFindings(expect TestExpect, op ContractOperation, contract ServiceContract, section string, l *Location) []Finding {
	if expect.ErrorExpected || emptyResult(expect.Result) || len(op.Result) == 0 {
		return nil
	}
	zero, ok := zeroResult(op.Result, contract.Defs, 0)
	if !ok {
		return nil
	}
	var want any
	if json.Unmarshal([]byte(expect.Result), &want) != nil {
		want = expect.Result
	}
	if !zeroSatisfies(want, zero) {
		return nil
	}
	return []Finding{tpWarning(ruleTPExpectZero, l,
		"%s: the expected result %s is what %s returns when it does nothing (its zero result), so a null subject satisfies it; assert a value the operation has to produce", section, strings.TrimSpace(expect.Result), op.Name)}
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
		if len(n.Properties) == 0 {
			return nil, false
		}
		obj := make(map[string]any, len(n.Properties))
		for name, sub := range n.Properties {
			if z, ok := zeroResult(sub, defs, depth+1); ok {
				obj[name] = z
			}
		}
		return obj, true
	}
	return nil, false
}

// zeroSatisfies reports whether the zero result is a JSON superset of want, with the generated
// assertion's semantics: every expected member present and matching, arrays element-wise, scalars equal.
func zeroSatisfies(want, zero any) bool {
	switch w := want.(type) {
	case map[string]any:
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
	case []any:
		z, ok := zero.([]any)
		return ok && len(z) == len(w) && func() bool {
			for i := range w {
				if !zeroSatisfies(w[i], z[i]) {
					return false
				}
			}
			return true
		}()
	default:
		return reflect.DeepEqual(want, zero)
	}
}
