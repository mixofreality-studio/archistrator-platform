package methodcheck

import (
	"encoding/json"
	"fmt"
	"slices"
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
// is a check of the DESIGN: a contract op no use case's dynamic view ever calls. It is
// an Error (ruling A5, every API has a use case): Check fails the app repo on it, and
// every aiarch-state write that validates with methodcheck rejects a write that leaves
// an op unreached. It speaks only once slot 4 (core use cases) and slot 5 (system) are
// both committed — before that there are no scenarios, and a contract designed ahead
// of the architecture is not a defect.

// Rule ids of the TP family.
const (
	ruleTPKey       RuleID = "TP-KEY"
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
	// ruleTPClientOps holds a browser surface's contract family to the ops its labels derive.
	ruleTPClientOps RuleID = "TP-CLIENT-OPS"
	// ruleTPLabelOp refuses a "|" alternative that names no op of a contracted callee.
	ruleTPLabelOp RuleID = "TP-LABEL-OP"
	// ruleClientMachineMirror holds a machine client's contract to the Manager behind it.
	ruleClientMachineMirror RuleID = "CLIENT-MACHINE-MIRROR"
)

// opLookup resolves an operation of one contract by its exact (case-sensitive) name.
type opLookup func(name string) (ContractOperation, bool)

// testPlanFindings runs the family. TP-OP-REACHED runs over the whole derived set
// regardless of any committed plan (once the architecture is committed); the binding rules run per committed component
// plan, in key order so the findings are deterministic. all is the project's
// derived scenario set (DeriveScenarios).
func testPlanFindings(p Project, all []scenario.Scenario) []Finding {
	out := opReachedFindings(p, all)
	out = append(out, clientContractFindings(p)...)
	out = append(out, uiDesignRecordFindings(p)...)
	if p.PhaseArtifacts == nil {
		return out
	}
	env := newTPEnv(p)
	comps := make([]string, 0, len(p.PhaseArtifacts.TestPlan))
	for comp := range p.PhaseArtifacts.TestPlan {
		comps = append(comps, comp)
	}
	sort.Strings(comps)
	for _, comp := range comps {
		sc, ok := p.ServiceContracts[comp]
		if !ok {
			out = append(out, planKeyFinding(p, comp))
			continue
		}
		rec := p.PhaseArtifacts.TestPlan[comp]
		out = append(out, componentPlanFindings(p, env, comp, rec, scenario.ForComponentAs(all, comp, contractOf(comp, sc).Client))...)
	}
	return out
}

// planKeyFinding is TP-KEY: a plan's key must be exactly a serviceContracts key, because
// that key names the generated test, its results directory and the checks that read them.
// A plan under any other spelling (a component's kebab slot-5 id) generates tests whose
// verdicts no check ever finds, and one key could not hold one plan per facet anyway.
func planKeyFinding(p Project, comp string) Finding {
	l := loc(0, "phaseArtifacts.testPlan."+comp)
	want := scenario.Normalize(comp)
	var near []string
	for key, sc := range p.ServiceContracts {
		if scenario.Normalize(key) == want || scenario.Normalize(sc.Component) == want {
			near = append(near, key)
		}
	}
	sort.Strings(near)
	if len(near) == 0 {
		return tpFinding(ruleTPKey, l, "test plan key %q is not a serviceContracts key: no contract is recorded for it", comp)
	}
	return tpFinding(ruleTPKey, l, "test plan key %q is not a serviceContracts key: re-key it to %s", comp, strings.Join(near, " or "))
}

// componentPlanFindings is TP-BOUND over one component's plan (every projected
// scenario bound exactly once, no unknown ids) plus the per-binding rules.
func componentPlanFindings(p Project, env tpEnv, comp string, rec TestPlanRecord, projected []scenario.Scenario) []Finding {
	l := loc(0, "phaseArtifacts.testPlan."+comp)
	byID := make(map[string]scenario.Scenario, len(projected))
	for _, s := range projected {
		byID[s.ID] = s
	}
	bound := map[string]bool{}
	var live []scenario.Scenario // bound and not skipped: the scenarios that will run
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
		if b.Skip == nil {
			live = append(live, s)
		}
		out = append(out, bindingFindings(p, env, comp, s, b, l)...)
	}
	out = append(out, unboundFindings(comp, projected, bound, l)...)
	return append(out, principalFindings(env, comp, rec, live, l)...)
}

// unboundFindings is the TP-BOUND half that names every projected scenario with no binding.
func unboundFindings(comp string, projected []scenario.Scenario, bound map[string]bool, l *Location) []Finding {
	var out []Finding
	for _, s := range projected {
		if !bound[s.ID] {
			out = append(out, tpFinding(ruleTPBound, l, "%s has no binding for scenario %s", comp, s.ID))
		}
	}
	return out
}

// bindingFindings checks one binding: a skip (TP-SKIP) or the step list (TP-STEP
// cardinality, then every step).
func bindingFindings(p Project, env tpEnv, comp string, s scenario.Scenario, b ScenarioBinding, l *Location) []Finding {
	if b.Skip != nil {
		return skipFindings(p, comp, b, l)
	}
	if len(b.Steps) != len(s.Stimuli) {
		return []Finding{tpFinding(ruleTPStep, l, "%s %s: %d steps bound, %d stimuli projected", comp, s.ID, len(b.Steps), len(s.Stimuli))}
	}
	contract, op := findContract(p, comp)
	sc := tpScope{p: p, env: env, comp: comp, scenario: s.ID, scn: s, steps: b.Steps, contract: contract, op: op, web: env.isWeb(comp, contract), l: l}
	out := sc.arrangeFindings(b.Arrange)
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

	env   tpEnv
	scn   scenario.Scenario // the projected scenario under test
	steps []StepBinding     // the binding's steps, for refs
	web   bool              // the component is a browser surface
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
	out = append(out, sc.uiDialectFindings(st, cop, section)...)
	out = append(out, expectRequiredFindings(st, section, sc.l)...)
	out = append(out, sc.refFindings(st.Inputs, st.Seq, -1, section)...)
	proved, pf := sc.probeFindings(section, st.Seq, st.Probes)
	out = append(out, pf...)
	out = append(out, sc.actorObservedFindings(stim, st, cop, section)...)
	return append(out, sc.stateFindings(section, stim, proved)...)
}

// probeFindings is TP-PROBE: every probe names a designed READ-ONLY operation, supplies its
// params, and is For a node on the scenario's path; the result is the set of scenario nodes
// the probes claim to prove. A probe that could change state would make the observation
// part of the experiment. An empty probe result is TP-EXPECT-REQUIRED's.
func (sc tpScope) probeFindings(section string, seq int, probes []Probe) (map[string]bool, []Finding) {
	proved := map[string]bool{}
	var out []Finding
	for i, pr := range probes {
		pc, pop := findContract(sc.p, pr.Component)
		cpop, ok := pop(pr.Operation)
		if !ok {
			out = append(out, tpFinding(ruleTPProbe, sc.l, "%s: probe %s.%s is not a designed operation", section, pr.Component, pr.Operation))
			continue
		}
		psec := fmt.Sprintf("%s probe %s.%s", section, pr.Component, pr.Operation)
		if !cpop.ReadOnly {
			out = append(out, tpFinding(ruleTPProbe, sc.l, "%s: the operation is not read-only; a probe observes without changing state, so mark %s readOnly in its contract or probe through a read", psec, pr.Operation))
		}
		if !slices.Contains(sc.scn.Path, pr.For) {
			out = append(out, tpFinding(ruleTPProbe, sc.l, "%s: probe is For %q, which is not a node on the path of %s", psec, pr.For, sc.scenario))
		}
		out = append(out, argFindings(pr.Inputs, cpop, pc, ruleTPProbe, ruleTPProbe, psec, sc.l)...)
		out = append(out, sc.refFindings(pr.Inputs, seq, i, psec)...)
		out = append(out, expectFindings(pr.Expect, cpop, pc, ruleTPProbe, psec, sc.l)...)
		if pr.For != "" {
			proved[pr.For] = true
		}
	}
	return proved, out
}

// stateFindings is TP-STATE: every expected state and every expected output on the
// stimulus has a probe For its node. There is no unobservable escape (binding
// model v2): an output no probe can observe is a design defect.
func (sc tpScope) stateFindings(section string, stim scenario.Stimulus, proved map[string]bool) []Finding {
	var out []Finding
	for _, es := range stim.ExpectedStates {
		if !proved[es.Node] {
			out = append(out, tpFinding(ruleTPState, sc.l, "%s: expected state %s %s (node %s) has no probe", section, es.Object, es.InState, es.Node))
		}
	}
	for _, eo := range stim.ExpectedOutputs {
		if !proved[eo.Node] {
			out = append(out, tpFinding(ruleTPState, sc.l, "%s: expected output %q (node %s) has no probe", section, eo.Label, eo.Node))
		}
	}
	return out
}

// opReachedFindings is TP-OP-REACHED (Error): every operation of every contract is
// the input of at least one derived scenario, i.e. some use case's dynamic view calls
// it. Silent until both slot 4 and slot 5 are committed (architectureCommitted).
// Contracts are visited in key order.
func opReachedFindings(p Project, all []scenario.Scenario) []Finding {
	if !architectureCommitted(p) {
		return nil
	}
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
			out = append(out, tpFinding(ruleTPOpReached, loc(0, "serviceContracts."+key),
				"%s.%s is called by no use case's dynamic view (every API has a use case): add the op's call to a use case's dynamic view, or delete the op", key, op.Name))
		}
	}
	return out
}

// clientContractFindings are the rules that read a Client's contract against the design. Each
// holds one way of keeping contracts from drifting from what is generated or derived; none
// fires before the architecture is committed.
func clientContractFindings(p Project) []Finding {
	sys, ok, err := p.system()
	if err != nil || !ok || !architectureCommitted(p) {
		return nil
	}
	in, err := ScenarioInput(p)
	if err != nil {
		return nil
	}
	out := labelOpFindings(sys, in)
	out = append(out, clientContractedFindings(p, sys)...)
	tools := generatedToolNames(p)
	for _, c := range sys.Components {
		switch {
		case c.isBrowserSurface():
			out = append(out, browserOpsFindings(sys, in, c)...)
		case c.Kind == kindClient:
			out = append(out, machineMirrorFindings(in, c, tools)...)
		}
	}
	return out
}

// ruleTPClientContracted: every Client component has a serviceContracts entry.
const ruleTPClientContracted RuleID = "TP-CLIENT-CONTRACTED"

// clientContractedFindings is TP-CLIENT-CONTRACTED. A Client with no contract has no
// operations for a scenario to address, so every stimulus into it is unaddressed and
// its tests cannot be generated. A contract names a component either by its key or by
// its `component` field (a facet), compared under scenario.Normalize.
func clientContractedFindings(p Project, sys System) []Finding {
	contracted := map[string]bool{}
	for key, sc := range p.ServiceContracts {
		contracted[scenario.Normalize(key)] = true
		contracted[scenario.Normalize(sc.Component)] = true
	}
	var out []Finding
	for _, c := range sys.Components {
		if c.Kind != kindClient || contracted[scenario.Normalize(c.ID)] {
			continue
		}
		out = append(out, tpFinding(ruleTPClientContracted, loc(0, "component "+c.ID),
			"client component %q has no serviceContracts entry; every Client's operations are tested through its contract, so record one", c.ID))
	}
	return out
}

// labelOpFindings is TP-LABEL-OP: an op name in a label (a "|" alternative, or a label's one
// leading operation) that resolves to no op would silently lose its stimulus, and with it the proof
// of that op. A bare name that is an operation of several Managers one machine client fronts is
// ambiguous, and must be named as the client exposes it.
func labelOpFindings(sys System, in scenario.Input) []Finding {
	var out []Finding
	for i, dv := range sys.DynamicViews {
		for _, call := range stepCalls(dv) {
			for _, u := range scenario.UnresolvedOps(in, call.To, call.Label) {
				out = append(out, labelOpFinding(loc(i+1, "dynamicView "+viewLabel(dv)), call, u))
			}
		}
	}
	return out
}

func labelOpFinding(at *Location, call Relationship, u scenario.Unresolved) Finding {
	if len(u.Candidates) > 0 {
		return tpFinding(ruleTPLabelOp, at,
			"the call %s→%s is labelled %q, and %q is an operation of more than one Manager %s fronts; name the one meant as %s exposes it, one of %v", call.From, call.To, call.Label, u.Alt, call.To, call.To, u.Candidates)
	}
	return tpFinding(ruleTPLabelOp, at,
		"the call %s→%s is labelled %q, and %q names no operation of %s's contract; name an operation, or move outcomes after the \"—\"", call.From, call.To, call.Label, u.Alt, call.To)
}

// browserOpsFindings is TP-CLIENT-OPS: a browser surface's contract is derived from its labels,
// so its family's ops must equal that set exactly. How they split across facets is free.
func browserOpsFindings(sys System, in scenario.Input, c Component) []Finding {
	have, ok := scenario.FamilyOps(in, c.ID)
	if !ok {
		return nil // written when the surface's UI design is recorded
	}
	want, err := scenario.ClientOps(browserLabels(sys, in.Actors, c.ID))
	if err != nil {
		return nil // SYS-CLIENT-OP-LABEL's to report
	}
	sort.Strings(want)
	if slices.Equal(have, want) {
		return nil
	}
	return []Finding{tpFinding(ruleTPClientOps, loc(0, "component "+c.ID),
		"browser surface %q's contract has operations %v, but the labels of the actor calls into it derive %v; a browser contract is derived, so correct the design's labels and re-record the UI design", c.ID, have, want)}
}

// browserLabels is the labels of the actor calls into a browser surface, in view order: the
// input of scenario.ClientOps. Only design's own words reach the derivation.
func browserLabels(sys System, actors map[string]bool, componentID string) []string {
	var out []string
	for _, dv := range sys.DynamicViews {
		for _, call := range stepCalls(dv) {
			if call.To == componentID && actors[call.From] {
				out = append(out, call.Label)
			}
		}
	}
	return out
}

// generatedToolNames is every operation name a machine client can have: a Manager's
// interface name without its stereotype, then the operation.
func generatedToolNames(p Project) map[string]bool {
	out := map[string]bool{}
	for _, sc := range p.ServiceContracts {
		if !strings.EqualFold(sc.Layer, kindManager) && !strings.EqualFold(sc.Interface.Layer, kindManager) {
			continue
		}
		base := strings.TrimSuffix(sc.Interface.Name, "Manager")
		for _, op := range sc.Interface.Operations {
			out[base+op.Name] = true
		}
	}
	return out
}

// machineMirrorFindings is CLIENT-MACHINE-MIRROR. A machine client's contract is derived from the
// Manager behind it when that Manager's contract is recorded, so it has operations and each is
// one of that Manager's, as the generators name it, over REST and MCP alike.
func machineMirrorFindings(in scenario.Input, c Component, tools map[string]bool) []Finding {
	if !c.isBuilt() {
		return nil
	}
	have, ok := scenario.FamilyOps(in, c.ID)
	if !ok {
		return nil
	}
	section := "component " + c.ID
	if len(have) == 0 {
		return []Finding{tpFinding(ruleClientMachineMirror, loc(0, section), "machine client %q has a contract with no operations; its contract is derived from the Manager operations its views call", c.ID)}
	}
	var out []Finding
	for _, op := range have {
		if !tools[op] {
			out = append(out, tpFinding(ruleClientMachineMirror, loc(0, section), "machine client %q has contract operation %q, which is none of its Manager's: a machine contract mirrors the Manager's operations (the Manager's name without its stereotype, then the operation)", c.ID, op))
		}
	}
	return out
}

// architectureCommitted reports whether slot 4 (core use cases) and slot 5 (system)
// are both committed: the point from which the derived scenario set is the design's
// whole call surface, so an op it does not reach is genuinely unreached.
func architectureCommitted(p Project) bool {
	_, ucs := p.slotByKind(kindCoreUseCases)
	_, sys := p.slotByKind(kindSystem)
	return ucs && sys
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
	switch v := in.Value.(type) {
	case LiteralValue:
		valueKind := rawJSONKind(v.JSON)
		if !kindsConflict(paramKind, valueKind) {
			return nil
		}
		f := tpFinding(rule, l, "%s: input %q value is a %s but parameter %q of operation %q expects a %s", section, in.Name, valueKind, p.Name, op.Name, paramKind)
		return &f
	case RefValue:
		return nil // a ref resolves at run time: indeterminate here
	}
	return nil
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

// rawJSONKind classifies a literal's JSON value (the v2 form: real JSON, never a
// bare scalar); a JSON null or unparsable input is indeterminate.
func rawJSONKind(raw json.RawMessage) kindClass {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return kindClassUnknown
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

// ---- browser dialect (uidialect.go) ----

// uiDialectFindings holds a browser operation's step to the dialect: each input bound
// to a UIAction parameter is a known action kind with the fields that kind requires
// (TP-ARG-TYPE), and an expected UIObservation states exactly one thing per
// observable (TP-EXPECT). An unknown kind is refused here, when the plan is written,
// so testgen never meets one. The decoded action and observables are then resolved
// against the surface's UI design (uiStepFindings).
func (sc tpScope) uiDialectFindings(st StepBinding, op ContractOperation, section string) []Finding {
	actions, out := sc.uiActionInputs(st, op, section)
	obs, obsFindings := sc.uiObservations(st, op, section)
	out = append(out, obsFindings...)
	return append(out, sc.uiStepFindings(actions, obs, section)...)
}

// uiActionInputs decodes the step's action inputs (TP-ARG-TYPE) and holds a browser step
// to exactly one (TP-UI-ACTION).
func (sc tpScope) uiActionInputs(st StepBinding, op ContractOperation, section string) ([]UIAction, []Finding) {
	actionParams := refParams(op, "UIAction")
	var out []Finding
	var actions []UIAction
	n := 0
	for _, in := range st.Inputs {
		if !actionParams[in.Name] && in.Name != webActionParam {
			continue
		}
		n++
		lv, ok := in.Value.(LiteralValue) // a ref resolves at run time
		if !ok {
			continue
		}
		a, err := ParseUIAction(string(lv.JSON))
		if err != nil {
			out = append(out, tpFinding(ruleTPArgType, sc.l, "%s: input %q: %v", section, in.Name, err))
			continue
		}
		actions = append(actions, a)
	}
	if len(actionParams) > 0 || sc.web {
		out = append(out, sc.exactlyOneAction(n, section)...)
	}
	return actions, out
}

// exactlyOneAction is TP-UI-ACTION over a browser step with n action inputs.
func (sc tpScope) exactlyOneAction(n int, section string) []Finding {
	if n == 1 {
		return nil
	}
	return []Finding{tpFinding(ruleTPUIAction, sc.l, "%s: a browser step carries exactly one action input (named %q), found %d: a step with no action does nothing and asserts a state that already holds", section, webActionParam, n)}
}

// uiObservations decodes the step's expected observation and its before-state (TP-EXPECT).
func (sc tpScope) uiObservations(st StepBinding, op ContractOperation, section string) ([]UIObservable, []Finding) {
	var out []Finding
	var obs []UIObservable
	for _, e := range []*TestExpect{&st.Expect, st.Before} {
		if e == nil || !observesUI(op, *e) {
			continue
		}
		parsed, err := ParseUIObservation(e.Result)
		if err != nil {
			out = append(out, tpFinding(ruleTPExpect, sc.l, "%s: the expected observation: %v", section, err))
			continue
		}
		obs = append(obs, parsed...)
	}
	return obs, out
}

// webActionParam is the name of a browser operation's action parameter.
const webActionParam = "action"

// refParams is the names of the operation's params typed by the $def named def.
func refParams(op ContractOperation, def string) map[string]bool {
	out := map[string]bool{}
	for _, p := range op.Params {
		if tail, ok := schemaRefTail(p.Schema); ok && tail == def {
			out[p.Name] = true
		}
	}
	return out
}

// observesUI reports whether the step asserts a UIObservation result.
func observesUI(op ContractOperation, e TestExpect) bool {
	tail, ok := schemaRefTail(op.Result)
	return ok && tail == "UIObservation" && !e.ErrorExpected && e.Result != ""
}
