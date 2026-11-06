package methodcheck

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
)

// rules_testplan_v2.go is the TP-* rules of binding model v2 (FPC plan 2, Task 2.4).
// They close the ways a plan record could describe a vacuous test: a step that asserts
// nothing, an argument that points at nothing, a setup that performs the stimulus itself,
// a probe that can change state, an actor-visible outcome no step observes, and an
// actor with no identity to run as. Every finding is an Error (tpFinding).

// Rule ids of the v2 TP family.
const (
	// ruleTPHook: the plan hands the test a hook. The removed `hook` / `unobservable`
	// fields are refused when the record decodes (bindings.go); the arrange form of the same
	// defect, a setup call that performs the scenario's own stimulus, is reported here.
	ruleTPHook RuleID = "TP-HOOK"
	// ruleTPExpectRequired: a step (or its probes) must assert something.
	ruleTPExpectRequired RuleID = "TP-EXPECT-REQUIRED"
	// ruleTPArgRef: a ref argument names an earlier result that exists at that path.
	ruleTPArgRef RuleID = "TP-ARG-REF"
	// ruleTPArrange: each arrange call is well-formed against the committed design.
	ruleTPArrange RuleID = "TP-ARRANGE"
	// ruleTPActorObserved: what the actor sees after a Client stimulus is observed.
	ruleTPActorObserved RuleID = "TP-ACTOR-OBSERVED"
	// ruleTPPrincipal: every actor that originates a stimulus has an identity to run as.
	ruleTPPrincipal RuleID = "TP-PRINCIPAL"
)

// tpEnv is what the v2 rules read from the design beyond the contracts: the actor ids
// and the committed System. Both are empty before the architecture is committed.
type tpEnv struct {
	actors map[string]bool
	sys    System
	sysOK  bool
}

// newTPEnv reads the design context once per validation. A malformed slot leaves the
// matching half empty: the slot's own rules report it.
func newTPEnv(p Project) tpEnv {
	env := tpEnv{actors: map[string]bool{}}
	if in, err := ScenarioInput(p); err == nil {
		env.actors = in.Actors
	}
	if sys, ok, err := p.system(); err == nil && ok {
		env.sys, env.sysOK = sys, true
	}
	return env
}

// component resolves a plan's (or contract's) component to its System component by
// scenario.Normalize equality of id or name.
func (e tpEnv) component(id string) (Component, bool) {
	want := scenario.Normalize(id)
	for _, c := range e.sys.Components {
		if scenario.Normalize(c.ID) == want || scenario.Normalize(c.Name) == want {
			return c, true
		}
	}
	return Component{}, false
}

// isWeb reports whether the plan's component (the facet's parent when the contract is a
// facet) is a browser surface.
func (e tpEnv) isWeb(comp string, sc ServiceContract) bool {
	target := comp
	if sc.Component != "" {
		target = sc.Component
	}
	c, ok := e.component(target)
	return ok && c.isBrowserSurface()
}

// ---- TP-EXPECT-REQUIRED ----

// emptyResult reports an expected result that asserts nothing: absent, whitespace, or
// the JSON values {}, [], null and "".
func emptyResult(result string) bool {
	trimmed := strings.TrimSpace(result)
	if trimmed == "" {
		return true
	}
	var v any
	if err := json.Unmarshal([]byte(trimmed), &v); err != nil {
		return false // bare text is a value
	}
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	default:
		return false
	}
}

// expectRequiredFindings is TP-EXPECT-REQUIRED: a step that does not expect an error
// asserts a result itself or through at least one probe, and a probe that does not
// expect an error asserts a result.
func expectRequiredFindings(st StepBinding, section string, l *Location) []Finding {
	var out []Finding
	if !st.Expect.ErrorExpected && emptyResult(st.Expect.Result) && !slices.ContainsFunc(st.Probes, probeAsserts) {
		out = append(out, tpFinding(ruleTPExpectRequired, l,
			"%s: the step asserts nothing: expect a non-empty result, an error, or give it a probe that asserts the effect (an empty result, {} or [] passes whether or not the action worked)", section))
	}
	for i, pr := range st.Probes {
		if !probeAsserts(pr) {
			out = append(out, tpFinding(ruleTPExpectRequired, l,
				"%s: probe %d (%s.%s) asserts nothing: a probe states the result it expects to read", section, i, pr.Component, pr.Operation))
		}
	}
	return out
}

// probeAsserts reports a probe that states a non-empty result or expects an error.
func probeAsserts(pr Probe) bool {
	return pr.Expect.ErrorExpected || !emptyResult(pr.Expect.Result)
}

// ---- TP-ARG-REF ----

// refFindings is TP-ARG-REF over one input list. seq is the step the inputs belong to
// (0 for arrange, which runs before every step); probeIdx is the probe's index within
// its step, or -1 for a step's own inputs. A step input may name an earlier step; a
// probe may also name its own step and an earlier probe of it.
func (sc tpScope) refFindings(inputs []TestArg, seq, probeIdx int, section string) []Finding {
	var out []Finding
	for _, in := range inputs {
		switch v := in.Value.(type) {
		case LiteralValue:
		case RefValue:
			if msg := sc.refProblem(v, seq, probeIdx); msg != "" {
				out = append(out, tpFinding(ruleTPArgRef, sc.l, "%s: input %q: %s", section, in.Name, msg))
			}
		}
	}
	return out
}

// refProblem is the reason a ref does not resolve, or "" when it does (or when the
// operation it points at is unresolved, which its own rule reports).
func (sc tpScope) refProblem(v RefValue, seq, probeIdx int) string {
	inProbe := probeIdx >= 0
	var target *StepBinding
	for i := range sc.steps {
		if sc.steps[i].Seq == v.Step {
			target = &sc.steps[i]
		}
	}
	if target == nil {
		return fmt.Sprintf("refers to step %d, which this scenario does not have", v.Step)
	}
	if v.Step >= seq && (!inProbe || v.Step != seq) {
		return fmt.Sprintf("refers to step %d, which does not run before it", v.Step)
	}
	op, contract, ok := sc.refOp(v, *target, seq, probeIdx)
	if !ok {
		return sc.refShapeProblem(v, *target, seq, probeIdx)
	}
	if len(op.Result) == 0 {
		return fmt.Sprintf("refers to the result of %s, which declares none", op.Name)
	}
	if !schemaHasPath(op.Result, contract.Defs, v.Path) {
		return fmt.Sprintf("path %q does not resolve in the result of %s", v.Path, op.Name)
	}
	return ""
}

// refShapeProblem reports an out-of-range or not-yet-run probe index; "" when the ref's
// operation is merely unresolved (another rule's finding).
func (sc tpScope) refShapeProblem(v RefValue, target StepBinding, seq, probeIdx int) string {
	if v.Probe == nil {
		return ""
	}
	if *v.Probe < 0 || *v.Probe >= len(target.Probes) {
		return fmt.Sprintf("refers to probe %d of step %d, which has %d", *v.Probe, v.Step, len(target.Probes))
	}
	if probeIdx >= 0 && v.Step == seq && *v.Probe >= probeIdx {
		return fmt.Sprintf("refers to probe %d of step %d, which does not run before it", *v.Probe, v.Step)
	}
	return ""
}

// refOp resolves the operation whose result a ref reads: the target step's, or the
// target probe's. ok is false when the ref is out of range, not yet run, or its
// operation is not designed.
func (sc tpScope) refOp(v RefValue, target StepBinding, seq, probeIdx int) (ContractOperation, ServiceContract, bool) {
	if v.Probe == nil {
		op, ok := sc.op(target.Operation)
		return op, sc.contract, ok
	}
	if *v.Probe < 0 || *v.Probe >= len(target.Probes) || (probeIdx >= 0 && v.Step == seq && *v.Probe >= probeIdx) {
		return ContractOperation{}, ServiceContract{}, false
	}
	pr := target.Probes[*v.Probe]
	pc, pop := findContract(sc.p, pr.Component)
	op, ok := pop(pr.Operation)
	return op, pc, ok
}

// pathStatus is how one schema node answers "does it carry this member".
type pathStatus int

const (
	pathFound  pathStatus = iota // the member exists and is the returned node
	pathAbsent                   // the node is closed and has no such member
	pathOpen                     // the node does not state its shape: indeterminate, never a finding
)

// schemaHasPath reports whether path (dot-separated member names, a trailing "[]" per
// array level) resolves in schema, following $refs into defs. A node that does not state
// its shape (no properties, a oneOf, an unresolvable $ref) is indeterminate and counts
// as resolving: the rule never fires on ambiguity.
func schemaHasPath(schema json.RawMessage, defs map[string]json.RawMessage, path string) bool {
	if path == "" {
		return true
	}
	cur := schema
	for seg := range strings.SplitSeq(path, ".") {
		next, st := walkSegment(cur, defs, seg)
		switch st {
		case pathAbsent:
			return false
		case pathOpen:
			return true
		case pathFound:
			cur = next
		}
	}
	return true
}

// walkSegment follows one path segment: a member name, each trailing "[]" an array level.
// An empty segment is absent.
func walkSegment(cur json.RawMessage, defs map[string]json.RawMessage, seg string) (json.RawMessage, pathStatus) {
	if seg == "" {
		return nil, pathAbsent
	}
	name, arrays := seg, 0
	for strings.HasSuffix(name, "[]") {
		name = strings.TrimSuffix(name, "[]")
		arrays++
	}
	st := pathFound
	if name != "" {
		cur, st = schemaMember(cur, defs, name)
	}
	for ; arrays > 0 && st == pathFound; arrays-- {
		cur, st = schemaItems(cur, defs)
	}
	return cur, st
}

// schemaNode is the slice of a JSON Schema node the path walk reads.
type schemaNode struct {
	Type       string                     `json:"type"`
	Properties map[string]json.RawMessage `json:"properties"`
	Items      json.RawMessage            `json:"items"`
	AddlProps  json.RawMessage            `json:"additionalProperties"`
	OneOf      []json.RawMessage          `json:"oneOf"`
	AnyOf      []json.RawMessage          `json:"anyOf"`
	AllOf      []json.RawMessage          `json:"allOf"`
}

// openSchemaNode decodes a node after following its $ref chain; ok is false when the
// shape cannot be determined.
func openSchemaNode(node json.RawMessage, defs map[string]json.RawMessage) (schemaNode, bool) {
	for range 8 {
		tail, isRef := schemaRefTail(node)
		if !isRef {
			break
		}
		def, ok := defs[tail]
		if !ok {
			return schemaNode{}, false
		}
		node = def
	}
	var n schemaNode
	if len(node) == 0 || json.Unmarshal(node, &n) != nil {
		return schemaNode{}, false
	}
	if _, isRef := schemaRefTail(node); isRef {
		return schemaNode{}, false
	}
	if len(n.OneOf)+len(n.AnyOf)+len(n.AllOf) > 0 {
		return schemaNode{}, false
	}
	return n, true
}

// schemaMember is the named member of an object schema.
func schemaMember(node json.RawMessage, defs map[string]json.RawMessage, name string) (json.RawMessage, pathStatus) {
	n, ok := openSchemaNode(node, defs)
	if !ok {
		return nil, pathOpen
	}
	if sub, has := n.Properties[name]; has {
		return sub, pathFound
	}
	switch n.Type {
	case "string", "integer", "number", "boolean", "array":
		return nil, pathAbsent
	}
	if len(n.Properties) == 0 || (len(n.AddlProps) > 0 && string(n.AddlProps) != "false") {
		return nil, pathOpen
	}
	return nil, pathAbsent
}

// schemaItems is the element schema of an array schema.
func schemaItems(node json.RawMessage, defs map[string]json.RawMessage) (json.RawMessage, pathStatus) {
	n, ok := openSchemaNode(node, defs)
	if !ok {
		return nil, pathOpen
	}
	if n.Type != "array" {
		return nil, pathAbsent
	}
	if len(n.Items) == 0 {
		return nil, pathOpen
	}
	return n.Items, pathFound
}

// ---- TP-ARRANGE and TP-HOOK (arrange form) ----

// arrangeFindings is TP-ARRANGE over a binding's setup: a contract call names a designed
// operation with well-typed arguments and refs, a failNext names a designed operation, a
// clock advance is a Go duration, a seed names a Resource component. A contract call that
// performs one of the scenario's own stimuli on the plan's component is TP-HOOK: the test
// would set up by doing the very thing it is meant to observe.
func (sc tpScope) arrangeFindings(arrange []ArrangeCall) []Finding {
	var out []Finding
	for i, a := range arrange {
		out = append(out, sc.arrangeOne(a, fmt.Sprintf("%s %s arrange %d", sc.comp, sc.scenario, i))...)
	}
	return out
}

// arrangeOne checks one arrange call.
func (sc tpScope) arrangeOne(a ArrangeCall, section string) []Finding {
	switch v := a.(type) {
	case ContractCall:
		return sc.contractCallFindings(v, section)
	case AdvanceClock:
		if _, err := time.ParseDuration(v.Duration); err != nil {
			return []Finding{tpFinding(ruleTPArrange, sc.l, "%s: advanceClock %q is not a Go duration (for example 90s, 5m, 36h)", section, v.Duration)}
		}
	case FailNext:
		if _, op := findContract(sc.p, v.Component); !opExists(op, v.Operation) {
			return []Finding{tpFinding(ruleTPArrange, sc.l, "%s: failNext %s.%s is not a designed operation", section, v.Component, v.Operation)}
		}
	case SeedResource:
		if !sc.isResource(v.Resource) {
			return []Finding{tpFinding(ruleTPArrange, sc.l, "%s: seedResource %q names no Resource component of the system", section, v.Resource)}
		}
	}
	return nil
}

// opExists reports whether look resolves name.
func opExists(look opLookup, name string) bool {
	_, ok := look(name)
	return ok
}

// contractCallFindings checks one arrange contract call.
func (sc tpScope) contractCallFindings(v ContractCall, section string) []Finding {
	contract, look := findContract(sc.p, v.Component)
	cop, ok := look(v.Operation)
	if !ok {
		return []Finding{tpFinding(ruleTPArrange, sc.l, "%s: contractCall %s.%s is not a designed operation", section, v.Component, v.Operation)}
	}
	var out []Finding
	if sc.performsOwnStimulus(v) {
		out = append(out, tpFinding(ruleTPHook, sc.l,
			"%s: the setup calls %s.%s, which is a stimulus of this scenario; arranging by performing the action under test proves nothing about it", section, v.Component, v.Operation))
	}
	out = append(out, argFindings(v.Inputs, cop, contract, ruleTPArrange, ruleTPArrange, section, sc.l)...)
	out = append(out, expectFindings(v.Expect, cop, contract, ruleTPArrange, section, sc.l)...)
	return append(out, sc.refFindings(v.Inputs, 0, -1, section)...)
}

// performsOwnStimulus reports an arrange call to the plan's own component and one of the
// scenario's stimulus operations.
func (sc tpScope) performsOwnStimulus(v ContractCall) bool {
	callee := scenario.Normalize(v.Component)
	if callee != scenario.Normalize(sc.comp) && callee != scenario.Normalize(sc.contract.Component) {
		return false
	}
	for _, stim := range sc.scn.Stimuli {
		if stim.Input.Op == v.Operation {
			return true
		}
	}
	return false
}

// isResource reports a Resource component named by id or name.
func (sc tpScope) isResource(name string) bool {
	c, ok := sc.env.component(name)
	return ok && c.Kind == kindResource
}

// ---- TP-ACTOR-OBSERVED ----

// actorObservedFindings is TP-ACTOR-OBSERVED: the outputs and states the actor sees after
// a Client stimulus (its ActorOutcome) must each be observed. A browser surface observes
// them through the step's UI observation; a machine client through a probe For the node.
func (sc tpScope) actorObservedFindings(stim scenario.Stimulus, st StepBinding, cop ContractOperation, section string) []Finding {
	oc := stim.ActorOutcome
	if len(oc.ExpectedOutputs) == 0 && len(oc.ExpectedStates) == 0 {
		return nil
	}
	if sc.web {
		return sc.webObservedFindings(oc, st, cop, section)
	}
	proved := map[string]bool{}
	for _, pr := range st.Probes {
		proved[pr.For] = true
	}
	var out []Finding
	for _, eo := range oc.ExpectedOutputs {
		if !proved[eo.Node] {
			out = append(out, tpFinding(ruleTPActorObserved, sc.l, "%s: the actor sees output %q (node %s) but no probe observes it", section, eo.Label, eo.Node))
		}
	}
	for _, es := range oc.ExpectedStates {
		if !proved[es.Node] {
			out = append(out, tpFinding(ruleTPActorObserved, sc.l, "%s: the actor sees state %s %s (node %s) but no probe observes it", section, es.Object, es.InState, es.Node))
		}
	}
	return out
}

// webObservedFindings: a browser surface's step must expect at least one UI observable.
func (sc tpScope) webObservedFindings(oc scenario.Outcome, st StepBinding, cop ContractOperation, section string) []Finding {
	if observesUI(cop, st.Expect) {
		if obs, err := ParseUIObservation(st.Expect.Result); err == nil && len(obs) > 0 {
			return nil
		}
	}
	return []Finding{tpFinding(ruleTPActorObserved, sc.l,
		"%s: the actor sees %s after this action, but the step expects no UI observable: assert what the user sees", section, describeActorOutcome(oc))}
}

// describeActorOutcome names the nodes an actor outcome reaches.
func describeActorOutcome(oc scenario.Outcome) string {
	var nodes []string
	for _, eo := range oc.ExpectedOutputs {
		nodes = append(nodes, eo.Node)
	}
	for _, es := range oc.ExpectedStates {
		nodes = append(nodes, es.Node)
	}
	return strings.Join(nodes, ", ")
}

// ---- TP-PRINCIPAL ----

// originatingActors is the sorted set of actors that originate a stimulus of the scenarios.
func originatingActors(env tpEnv, live []scenario.Scenario) []string {
	actors := map[string]bool{}
	for _, s := range live {
		for _, st := range s.Stimuli {
			for _, from := range stimulusCallers(st) {
				actors[from] = env.actors[from] || actors[from]
			}
		}
	}
	var names []string
	for a, isActor := range actors {
		if isActor {
			names = append(names, a)
		}
	}
	sort.Strings(names)
	return names
}

// stimulusCallers is the caller of a stimulus and the alternative callers it stands for.
func stimulusCallers(st scenario.Stimulus) []string {
	return append([]string{st.Input.From}, st.Input.Via...)
}

// principalFindings is TP-PRINCIPAL: every actor that originates a stimulus of a
// scenario the plan binds (not skips) has a principal with a subject to run as. One
// finding per missing actor, in actor order.
func principalFindings(env tpEnv, comp string, rec TestPlanRecord, live []scenario.Scenario, l *Location) []Finding {
	var out []Finding
	for _, a := range originatingActors(env, live) {
		pr, ok := rec.Principals[a]
		switch {
		case !ok:
			out = append(out, tpFinding(ruleTPPrincipal, l, "%s: actor %q originates stimuli but the plan has no principal for it; record who the test runs as", comp, a))
		case pr.Subject == "":
			out = append(out, tpFinding(ruleTPPrincipal, l, "%s: the principal for actor %q has an empty subject", comp, a))
		}
	}
	return out
}
