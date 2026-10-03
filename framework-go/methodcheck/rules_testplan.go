package methodcheck

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
)

// rules_testplan.go is the TP-* per-component test-plan family (deterministic-
// component-testing design §5.2), replacing the retired STP-* system-test-plan rules.
// The plan under test is `.phaseArtifacts.testPlan[<component>]`: one ScenarioBinding
// per scenario the derivation projects onto that component (DeriveScenarios +
// scenario.ForComponent). Every rule here is a check of a binding against the
// projected scenario and the committed contract surface, except TP-OP-REACHED, which
// is a check of the DESIGN (a contract op no scenario ever drives) and therefore a
// Warning: it must not block recordPhaseArtifact; design-health renders it and the
// workflow reports it for a founder ruling.

// Rule ids of the TP family.
const (
	ruleTPBound     RuleID = "TP-BOUND"
	ruleTPStep      RuleID = "TP-STEP"
	ruleTPStepOp    RuleID = "TP-STEP-OP"
	ruleTPArgName   RuleID = "TP-ARG-NAME"
	ruleTPArgType   RuleID = "TP-ARG-TYPE"
	ruleTPExpect    RuleID = "TP-EXPECT"
	ruleTPProbe     RuleID = "TP-PROBE"
	ruleTPState     RuleID = "TP-STATE"
	ruleTPOpReached RuleID = "TP-OP-REACHED"
	ruleTPSkip      RuleID = "TP-SKIP"
)

// opLookup resolves an operation of one contract by its exact (case-sensitive) name.
type opLookup func(name string) (ContractOperation, bool)

// testPlanFindings runs the family. TP-OP-REACHED runs over the whole derived set
// regardless of any committed plan; the binding rules run per committed component
// plan, in key order so the findings are deterministic. all is the project's
// derived scenario set (DeriveScenarios).
func testPlanFindings(p Project, all []scenario.Scenario) []Finding {
	out := opReachedFindings(p, all)
	if p.PhaseArtifacts == nil {
		return out
	}
	comps := make([]string, 0, len(p.PhaseArtifacts.TestPlan))
	for comp := range p.PhaseArtifacts.TestPlan {
		comps = append(comps, comp)
	}
	sort.Strings(comps)
	for _, comp := range comps {
		rec := p.PhaseArtifacts.TestPlan[comp]
		out = append(out, componentPlanFindings(p, comp, rec, scenario.ForComponent(all, comp))...)
	}
	return out
}

// componentPlanFindings is TP-BOUND over one component's plan (every projected
// scenario bound exactly once, no unknown ids) plus the per-binding rules.
func componentPlanFindings(p Project, comp string, rec TestPlanRecord, projected []scenario.Scenario) []Finding {
	l := loc(0, "phaseArtifacts.testPlan."+comp)
	byID := make(map[string]scenario.Scenario, len(projected))
	for _, s := range projected {
		byID[s.ID] = s
	}
	bound := map[string]bool{}
	var out []Finding
	for _, b := range rec.Bindings {
		s, ok := byID[b.Scenario]
		if !ok {
			out = append(out, tpFinding(ruleTPBound, l, "%s binds unknown scenario %q (stale after a use-case edit?)", comp, b.Scenario))
			continue
		}
		if bound[b.Scenario] {
			out = append(out, tpFinding(ruleTPBound, l, "%s binds %q twice", comp, b.Scenario))
			continue
		}
		bound[b.Scenario] = true
		out = append(out, bindingFindings(p, comp, s, b, l)...)
	}
	for _, s := range projected {
		if !bound[s.ID] {
			out = append(out, tpFinding(ruleTPBound, l, "%s has no binding for scenario %s", comp, s.ID))
		}
	}
	return out
}

// bindingFindings checks one binding: a skip (TP-SKIP) or the step list (TP-STEP
// cardinality, then every step).
func bindingFindings(p Project, comp string, s scenario.Scenario, b ScenarioBinding, l *Location) []Finding {
	if b.Skip != nil {
		return skipFindings(p, comp, b, l)
	}
	if len(b.Steps) != len(s.Stimuli) {
		return []Finding{tpFinding(ruleTPStep, l, "%s %s: %d steps bound, %d stimuli projected", comp, s.ID, len(b.Steps), len(s.Stimuli))}
	}
	contract, op := findContract(p, comp)
	sc := tpScope{p: p, comp: comp, scenario: s.ID, contract: contract, op: op, l: l}
	var out []Finding
	for i, st := range b.Steps {
		out = append(out, sc.stepFindings(s.Stimuli[i], st)...)
	}
	return out
}

// tpScope is one binding's evaluation context: the component, its scenario, its
// resolved contract, and the plan's location.
type tpScope struct {
	p        Project
	comp     string
	scenario string
	contract ServiceContract
	op       opLookup
	l        *Location
}

// section labels one step for messages.
func (sc tpScope) section(seq int) string {
	return fmt.Sprintf("%s %s step %d", sc.comp, sc.scenario, seq)
}

// stepFindings is TP-STEP (seq), TP-STEP-OP (the op exists and agrees with the call
// chain), then the arg/expect/probe/state rules over the resolved op.
func (sc tpScope) stepFindings(stim scenario.Stimulus, st StepBinding) []Finding {
	var out []Finding
	if st.Seq != stim.Seq {
		out = append(out, tpFinding(ruleTPStep, sc.l, "%s %s: step bound as seq %d where stimulus %d is projected", sc.comp, sc.scenario, st.Seq, stim.Seq))
	}
	section := sc.section(stim.Seq)
	cop, ok := sc.op(st.Operation)
	if !ok {
		return append(out, tpFinding(ruleTPStepOp, sc.l, "%s: %q is not an operation of %s", section, st.Operation, sc.comp))
	}
	if stim.Input.Op != "" && st.Operation != stim.Input.Op {
		return append(out, tpFinding(ruleTPStepOp, sc.l, "%s: operation %q but the call chain resolves %q", section, st.Operation, stim.Input.Op))
	}
	out = append(out, argFindings(st.Inputs, cop, sc.contract, ruleTPArgName, ruleTPArgType, section, sc.l)...)
	out = append(out, expectFindings(st.Expect, cop, sc.contract, ruleTPExpect, section, sc.l)...)
	proved, pf := sc.probeFindings(section, st.Probes)
	out = append(out, pf...)
	return append(out, sc.stateFindings(section, stim, st, proved)...)
}

// probeFindings is TP-PROBE: every probe names a designed operation and supplies its
// params; the result is the set of scenario nodes the probes claim to prove.
//
// EARMARK (spec §5.2 drift): the spec says probes name real READ-ONLY ops. The mirror's
// ContractOperation carries no read-only/mutating marker, so only existence is
// checked here; the wave that adds that flag to the contract surface owns the other
// half of the predicate.
func (sc tpScope) probeFindings(section string, probes []Probe) (map[string]bool, []Finding) {
	proved := map[string]bool{}
	var out []Finding
	for _, pr := range probes {
		pc, pop := findContract(sc.p, pr.Component)
		cpop, ok := pop(pr.Operation)
		if !ok {
			out = append(out, tpFinding(ruleTPProbe, sc.l, "%s: probe %s.%s is not a designed operation", section, pr.Component, pr.Operation))
			continue
		}
		psec := fmt.Sprintf("%s probe %s.%s", section, pr.Component, pr.Operation)
		out = append(out, argFindings(pr.Inputs, cpop, pc, ruleTPProbe, ruleTPProbe, psec, sc.l)...)
		out = append(out, expectFindings(pr.Expect, cpop, pc, ruleTPProbe, psec, sc.l)...)
		if pr.For != "" {
			proved[pr.For] = true
		}
	}
	return proved, out
}

// stateFindings is TP-STATE: every expected state on the stimulus has a probe For its
// node; every expected output has a probe or an Unobservable declaration.
func (sc tpScope) stateFindings(section string, stim scenario.Stimulus, st StepBinding, proved map[string]bool) []Finding {
	unobs := make(map[string]bool, len(st.Unobservable))
	for _, u := range st.Unobservable {
		unobs[u] = true
	}
	var out []Finding
	for _, es := range stim.ExpectedStates {
		if !proved[es.Node] {
			out = append(out, tpFinding(ruleTPState, sc.l, "%s: expected state %s %s (node %s) has no probe", section, es.Object, es.InState, es.Node))
		}
	}
	for _, eo := range stim.ExpectedOutputs {
		if !proved[eo.Node] && !unobs[eo.Node] {
			out = append(out, tpFinding(ruleTPState, sc.l, "%s: expected output %q (node %s) has neither a probe nor an unobservable declaration", section, eo.Label, eo.Node))
		}
	}
	return out
}

// opReachedFindings is TP-OP-REACHED (Warning): every operation of every contract is
// the input of at least one derived scenario. Contracts are visited in key order.
func opReachedFindings(p Project, all []scenario.Scenario) []Finding {
	reached := reachedOps(all)
	keys := make([]string, 0, len(p.ServiceContracts))
	for key := range p.ServiceContracts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out []Finding
	for _, key := range keys {
		for _, op := range p.ServiceContracts[key].Interface.Operations {
			if reached[scenario.Normalize(key)+"."+op.Name] {
				continue
			}
			out = append(out, Finding{
				RuleID:   ruleTPOpReached,
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("%s.%s is reached by no scenario: dead operation or missing use case", key, op.Name),
				Location: loc(0, "serviceContracts."+key),
			})
		}
	}
	return out
}

// reachedOps is the set of "<normalized component>.<op>" some stimulus drives.
func reachedOps(all []scenario.Scenario) map[string]bool {
	reached := map[string]bool{}
	for _, s := range all {
		for _, st := range s.Stimuli {
			if st.Input.Op != "" {
				reached[st.Input.To+"."+st.Input.Op] = true
			}
		}
	}
	return reached
}

// skipFindings is TP-SKIP: a skip carries a reason and names an activity that is
// genuinely not yet integrated (§3.1 — there is no other skip).
func skipFindings(p Project, comp string, b ScenarioBinding, l *Location) []Finding {
	if b.Skip.Until == "" || b.Skip.Reason == "" {
		return []Finding{tpFinding(ruleTPSkip, l, "%s %s: skip needs reason and until", comp, b.Scenario)}
	}
	if integrated(p, b.Skip.Until) {
		return []Finding{tpFinding(ruleTPSkip, l, "%s %s: skip waits on %s, which is already integrated", comp, b.Scenario, b.Skip.Until)}
	}
	return nil
}

// integrated reports whether activityID's execution row derives to Done — the server's
// CoarsePhaseFor read over the row the server's decoder would hand it. An activity with
// no row has not integrated. The current member is read when the document carries it
// and the legacy `.activityConstruction` member otherwise (activityExecutionOrLegacy).
//
// The precedence is CoarsePhaseFor's:
//   - a recorded FailureReason is sticky: Failed, whatever else the row holds. A legacy
//     row whose stored phase is Failed with no reason recorded is carried forward as
//     PipelineFailed (toActivityExecution), so the stored ordinal fails it too.
//   - a CompletedAt with a TailFailureDetail is CompletedNotLanded — it completed its
//     work and FAILED TO LAND IT; its dependents stay blocked, so a skip waiting on it
//     is legitimate. Read BEFORE Done, and never falls through to the ledger.
//   - a CompletedAt is the binary exit: Done. A legacy row whose stored phase is Done
//     has its exit minted by the carry-forward, so the ordinal counts as one.
//   - otherwise the attempt ledger decides (ledgerIntegrated). A legacy row whose stored
//     buildStatus is Integrated has a passed gate attempt minted for every phase of its
//     profile, so the ordinal reads as a ledger that passed.
func integrated(p Project, activityID string) bool {
	rows := p.ActivityExecution
	if rows == nil {
		rows = p.LegacyActivityConstruction
	}
	row, ok := rows[activityID]
	return ok && row.done()
}

// done is CoarsePhaseFor's precedence over one row (see integrated).
func (row ActivityRow) done() bool {
	if row.FailureReason != 0 || row.Phase == legacyPhaseFailed {
		return false
	}
	completed := len(row.CompletedAt) > 0 && string(row.CompletedAt) != "null"
	if completed && row.TailFailureDetail != "" {
		return false
	}
	if completed || row.Phase == legacyPhaseDone {
		return true
	}
	return row.BuildStatus == legacyBuildIntegrated || ledgerIntegrated(row.Attempts)
}

// ledgerIntegrated is the mirror of the server's ledger arm — ResolvePhaseCompletions
// then CoarsePhase: Done when EVERY phase of the activity's profile has its gate task's
// LATEST attempt passed. This is the shape of every row on the live dogfood document
// (no head facts, a backfilled ledger), so without it TP-SKIP's "already integrated"
// arm could never fire on the one document the rule exists to gate.
//
// THE MIRROR IS DELIBERATELY STRICTER THAN THE SERVER, NEVER LOOSER. The server's
// profile comes from classifying the activity (ClassifyType over the committed activity
// list's id/workerClass/coding) and looking its lifecycle up in method-assets'
// lifecycles.json. Neither the classification rules nor the lifecycle data belong to
// this package (framework-go carries no method-assets dependency), so the mirror reads
// what the LEDGER says about itself instead:
//   - the Integration phase's gate ("testing") — the final gate of every construction
//     lifecycle that carries an Integration phase — has a latest attempt and it passed.
//     A ledger that never reached it (the live C-billing-manager shape: four phases
//     passed, Integration undecided) is not Done on the server either.
//   - every task the ledger mentions has its latest attempt passed: a gate re-attempted
//     and rejected after an earlier pass, or a task still pending after a requeue
//     re-opened the walk, is not Done on the server and is not Done here.
//
// What the mirror cannot see is a profile phase the ledger never mentions at all. A
// walk reaches "testing" only after the gates before it, so an observed ledger has
// mentioned them; a backfilled one is the residual window, and it errs toward the
// server's Done (a false "already integrated" would BLOCK a legitimate skip's write;
// a missed one accepts a stale skip). EARMARK: the two lifecycles whose final gate is
// not "testing" (uiDesign ends at designReview, testing:qaProcess at codeReview) and
// the three design lifecycles never read integrated by ledger here — a false negative
// in the safe direction, closed only by giving this package the profile.
func ledgerIntegrated(attempts []TaskAttempt) bool {
	if len(attempts) == 0 {
		return false
	}
	latest := make(map[string]TaskAttempt, len(attempts))
	for _, a := range attempts {
		if cur, ok := latest[a.Task]; !ok || a.Attempt > cur.Attempt {
			latest[a.Task] = a
		}
	}
	gate, ok := latest[taskIntegrationGate]
	if !ok || gate.Outcome != attemptPassed {
		return false
	}
	for _, a := range latest {
		if a.Outcome != attemptPassed {
			return false
		}
	}
	return true
}

// findContract resolves comp's contract by scenario.Normalize key equality (the
// contract key is camelCase, slot-5 ids are kebab-case; both fold to one key). When
// several keys fold together the lowest wins, so the result never depends on map
// order. The lookup always fails when no contract is committed for comp.
func findContract(p Project, comp string) (ServiceContract, opLookup) {
	want := scenario.Normalize(comp)
	best, found := "", false
	for key := range p.ServiceContracts {
		if scenario.Normalize(key) == want && (!found || key < best) {
			best, found = key, true
		}
	}
	if !found {
		return ServiceContract{}, func(string) (ContractOperation, bool) { return ContractOperation{}, false }
	}
	c := p.ServiceContracts[best]
	return c, func(name string) (ContractOperation, bool) { return findOperation(c, name) }
}

// findOperation returns the exact (case-sensitive) operation on a contract.
func findOperation(c ServiceContract, opName string) (ContractOperation, bool) {
	for _, op := range c.Interface.Operations {
		if op.Name == opName {
			return op, true
		}
	}
	return ContractOperation{}, false
}

// tpFinding builds an Error finding of the family.
func tpFinding(id RuleID, l *Location, format string, args ...any) Finding {
	return Finding{RuleID: id, Severity: SeverityError, Message: fmt.Sprintf(format, args...), Location: l}
}

// ---- argument rules (restored from the STP-ARG-* predicates) ----

// argFindings is TP-ARG-NAME (every input names a param; every non-pointer param is
// supplied) and TP-ARG-TYPE (each matched input's type agrees with its param),
// labelled with the caller's rule ids so probes report under TP-PROBE.
func argFindings(inputs []TestArg, op ContractOperation, contract ServiceContract, nameRule, typeRule RuleID, section string, l *Location) []Finding {
	paramByName := make(map[string]ContractParam, len(op.Params))
	for _, p := range op.Params {
		paramByName[p.Name] = p
	}
	supplied := make(map[string]bool, len(inputs))
	var out []Finding
	for _, in := range inputs {
		supplied[in.Name] = true
		p, ok := paramByName[in.Name]
		if !ok {
			out = append(out, tpFinding(nameRule, l, "%s: input %q matches no parameter of operation %q; every input must name a contract parameter", section, in.Name, op.Name))
			continue
		}
		if f := argTypeFinding(in, p, contract, op, typeRule, section, l); f != nil {
			out = append(out, *f)
		}
	}
	for _, p := range op.Params {
		if !p.Pointer && !supplied[p.Name] {
			out = append(out, tpFinding(nameRule, l, "%s: required parameter %q of operation %q is not supplied by any input", section, p.Name, op.Name))
		}
	}
	return out
}

// argTypeFinding computes the type finding for one matched (input, param), or nil
// when consistent / indeterminate (best-effort — never fires on ambiguity). A set
// schemaRef must resolve to the SAME $defs entry as the param schema (or the param
// must be a named type at all); an empty schemaRef falls back to a value-kind check.
func argTypeFinding(in TestArg, p ContractParam, contract ServiceContract, op ContractOperation, rule RuleID, section string, l *Location) *Finding {
	paramRef, paramHasRef := schemaRefTail(p.Schema)
	if in.SchemaRef != "" {
		argRef := refTail(in.SchemaRef)
		switch {
		case paramHasRef && argRef != paramRef:
			f := tpFinding(rule, l, "%s: input %q declares schemaRef %q but parameter %q of operation %q is typed %q; the argument type contradicts the contract", section, in.Name, argRef, p.Name, op.Name, paramRef)
			return &f
		case !paramHasRef:
			f := tpFinding(rule, l, "%s: input %q declares schemaRef %q but parameter %q of operation %q is a primitive (%s); named-type argument contradicts a primitive parameter", section, in.Name, argRef, p.Name, op.Name, schemaPrimitiveKind(p.Schema))
			return &f
		}
		return nil
	}
	paramKind := schemaKind(p.Schema, contract.Defs)
	valueKind := jsonValueKind(in.Value)
	if !kindsConflict(paramKind, valueKind) {
		return nil
	}
	f := tpFinding(rule, l, "%s: input %q value is a %s but parameter %q of operation %q expects a %s", section, in.Name, valueKind, p.Name, op.Name, paramKind)
	return &f
}

// ---- expectation rule (restored from STP-EXPECT-SHAPE) ----

// expectFindings is TP-EXPECT: an error-expected step requires the operation to
// declare error:true; a non-error step's declared result value must agree with the
// operation's result kind (coarse class — not full schema validation).
func expectFindings(expect TestExpect, op ContractOperation, contract ServiceContract, rule RuleID, section string, l *Location) []Finding {
	if expect.ErrorExpected {
		if !op.Error {
			return []Finding{tpFinding(rule, l, "%s: the step expects an error but operation %q does not declare error:true; it cannot fail the way the binding asserts", section, op.Name)}
		}
		return nil // an error-expected step asserts no result value
	}
	if expect.Result == "" {
		return nil // no result asserted → nothing to shape-check
	}
	if len(op.Result) == 0 {
		return []Finding{tpFinding(rule, l, "%s: the step asserts a result value but operation %q declares no result (void)", section, op.Name)}
	}
	resultKind := schemaKind(op.Result, contract.Defs)
	valueKind := jsonValueKind(expect.Result)
	if !kindsConflict(resultKind, valueKind) {
		return nil
	}
	return []Finding{tpFinding(rule, l, "%s: the expected result is a %s but operation %q returns a %s", section, valueKind, op.Name, resultKind)}
}

// ---- schema-kind helpers (restored) ----

// kindClass is a coarse JSON value class for best-effort type checks.
type kindClass string

const (
	kindClassUnknown kindClass = ""
	kindClassObject  kindClass = "object"
	kindClassArray   kindClass = "array"
	kindClassString  kindClass = "string"
	kindClassNumber  kindClass = "number"
	kindClassBoolean kindClass = "boolean"
)

// kindsConflict reports whether a value of class value definitely cannot satisfy a
// schema of class want. Unknown on either side is indeterminate. A number or boolean
// value against a string schema is NOT a conflict: plan values are stored as text, so
// an unquoted "42" may well be the string "42".
func kindsConflict(want, value kindClass) bool {
	if want == kindClassUnknown || value == kindClassUnknown || want == value {
		return false
	}
	if want == kindClassString && (value == kindClassNumber || value == kindClassBoolean) {
		return false
	}
	return true
}

// refTail returns the trailing $def name of a "#/$defs/Name" ref (or the input when it
// carries no path separator).
func refTail(ref string) string {
	if i := strings.LastIndexByte(ref, '/'); i >= 0 {
		return ref[i+1:]
	}
	return ref
}

// schemaRefTail returns the $def name a schema node references via "$ref", and whether
// the node is a $ref at all.
func schemaRefTail(schema json.RawMessage) (string, bool) {
	if len(schema) == 0 {
		return "", false
	}
	var node struct {
		Ref string `json:"$ref"`
	}
	if err := json.Unmarshal(schema, &node); err != nil || node.Ref == "" {
		return "", false
	}
	return refTail(node.Ref), true
}

// schemaPrimitiveKind renders a schema node's inline "type" for a diagnostic (or
// "unknown").
func schemaPrimitiveKind(schema json.RawMessage) string {
	var node struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(schema, &node); err != nil || node.Type == "" {
		return "unknown"
	}
	return node.Type
}

// schemaKind reduces a schema node to a coarse kindClass, resolving one level of $ref
// into the contract's $defs when needed. Unknown when it cannot be determined.
func schemaKind(schema json.RawMessage, defs map[string]json.RawMessage) kindClass {
	if len(schema) == 0 {
		return kindClassUnknown
	}
	if tail, ok := schemaRefTail(schema); ok {
		def, defOK := defs[tail]
		if !defOK {
			return kindClassUnknown
		}
		return schemaKindFromType(def)
	}
	return schemaKindFromType(schema)
}

// schemaKindFromType maps a schema node's inline JSON-Schema "type" to a kindClass.
func schemaKindFromType(schema json.RawMessage) kindClass {
	var node struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(schema, &node); err != nil {
		return kindClassUnknown
	}
	switch node.Type {
	case "object":
		return kindClassObject
	case "array":
		return kindClassArray
	case "string":
		return kindClassString
	case "integer", "number":
		return kindClassNumber
	case "boolean":
		return kindClassBoolean
	default:
		return kindClassUnknown
	}
}

// jsonValueKind classifies a concrete plan value (JSON or bare text) into a kindClass.
// A value that does not parse as JSON is a bare string (the plan stores unquoted
// scalars like "proj-aiarch-01"); a JSON null is indeterminate.
func jsonValueKind(value string) kindClass {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return kindClassUnknown
	}
	var v any
	if err := json.Unmarshal([]byte(trimmed), &v); err != nil {
		return kindClassString
	}
	switch v.(type) {
	case map[string]any:
		return kindClassObject
	case []any:
		return kindClassArray
	case string:
		return kindClassString
	case float64:
		return kindClassNumber
	case bool:
		return kindClassBoolean
	default:
		return kindClassUnknown
	}
}
