package methodcheck

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
)

// rules_acceptance.go is the ATP-* family (FPC plan 4, Amendment C): the residual use-case
// plan, `.phaseArtifacts.acceptancePlan`. The scenarios it binds are the acceptance variants
// (scenario.ForAcceptance) no single client plan covers (scenario.AcceptanceResidual); each is
// bound exactly once, with TP-* v2 held over every step against the contract of the component
// the step's stimulus addresses. Every finding is an Error.

// Rule ids of the ATP family.
const (
	// ruleATPBound: every residual variant has exactly one binding; a binding names a residual variant.
	ruleATPBound RuleID = "ATP-BOUND"
	// ruleATPRef: every planRef resolves to a literal of a client plan and names a parameter of the
	// same schema as the input that carries it; a client plan carries none.
	ruleATPRef RuleID = "ATP-REF"
	// ruleATPBrowser: a web step or a probe on a browser surface runs as the plan's BrowserActor.
	ruleATPBrowser RuleID = "ATP-BROWSER"
	// ruleATPProbeAs: Probe.As names one of the plan's principals.
	ruleATPProbeAs RuleID = "ATP-PROBE-AS"
)

// acceptanceLabel names the acceptance plan in messages, where a client plan names its component.
const acceptanceLabel = "acceptance"

// acceptanceRuleIDs is this family's entry in the emitted-rule registry (emitters.go).
var acceptanceRuleIDs = []RuleID{ruleATPBound, ruleATPRef, ruleATPBrowser, ruleATPProbeAs}

// acceptanceScope is what the family reads: the design's acceptance variants, which of them are
// residual and why, and the recorded plan.
type acceptanceScope struct {
	p        Project
	env      tpEnv
	variants map[string]scenario.Scenario
	residual map[string]string // variant id -> why it is residual
	rec      AcceptancePlanRecord
	recorded bool // an acceptancePlan record exists
	l        *Location
}

// acceptancePlanFindings runs the family: the planRef refusal over the client plans, then, once
// the architecture is committed, ATP-BOUND over the residual and the TP-* v2 and ATP-* rules over
// every bound variant.
func acceptancePlanFindings(p Project, env tpEnv, all []scenario.Scenario) []Finding {
	out := planRefInClientPlanFindings(p)
	sc, ok := newAcceptanceScope(p, env, all)
	if !ok {
		return out
	}
	bound := map[string]bool{}
	var live []scenario.Scenario // bound, not skipped: the variants that will run
	for _, b := range sc.rec.Bindings {
		v, refused := sc.admit(b, bound)
		if refused != nil {
			out = append(out, *refused)
			continue
		}
		out = append(out, sc.bindingFindings(v, b)...)
		if b.Skip == nil {
			live = append(live, v)
		}
	}
	// The unbound half runs only over a recorded plan, as TP-BOUND runs only over recorded client
	// plans: N-ATP runs beside construction, so a plan not yet written must not refuse the whole-
	// document writes construction makes. That the plan exists is N-ATP's exit gate (Task 4.4's job gate).
	for _, id := range sortedResidual(sc.residual) {
		if sc.recorded && !bound[id] {
			out = append(out, tpFinding(ruleATPBound, sc.l, "%s has no binding for residual variant %s (%s)", acceptanceLabel, id, sc.residual[id]))
		}
	}
	out = append(out, browserFindings(sc.p, sc.env, sc.rec, live, sc.l)...)
	return append(out, principalFindings(sc.env, acceptanceLabel, TestPlanRecord{Principals: sc.rec.Principals}, live, sc.l)...)
}

// newAcceptanceScope reads the design's variants and the recorded plan; ok is false before the
// architecture is committed, when there is no derived scenario set to hold a plan against.
func newAcceptanceScope(p Project, env tpEnv, all []scenario.Scenario) (acceptanceScope, bool) {
	if !architectureCommitted(p) {
		return acceptanceScope{}, false
	}
	in, err := ScenarioInput(p)
	if err != nil {
		return acceptanceScope{}, false
	}
	clients := clientContractKeys(in)
	sc := acceptanceScope{p: p, env: env, variants: map[string]scenario.Scenario{}, l: loc(0, "phaseArtifacts.acceptancePlan"),
		residual: residualReasons(scenario.AcceptanceResidual(all, clients))}
	for _, v := range scenario.ForAcceptance(all, clients) {
		sc.variants[v.ID] = v
	}
	if p.PhaseArtifacts != nil && p.PhaseArtifacts.AcceptancePlan != nil {
		sc.rec = *p.PhaseArtifacts.AcceptancePlan
		sc.recorded = true
	}
	return sc, true
}

// bindingFindings is the TP-* v2 rules over one admitted binding and, unless it is skipped, its
// ATP-REF and ATP-PROBE-AS findings.
func (sc acceptanceScope) bindingFindings(v scenario.Scenario, b ScenarioBinding) []Finding {
	out := planBindingFindings(sc.p, sc.env, acceptanceLabel, v, b, sc.l, true)
	if b.Skip != nil {
		return out
	}
	out = append(out, bindingRefFindings(sc.p, v, b, sc.l)...)
	return append(out, bindingProbeAsFindings(sc.rec, b, sc.l)...)
}

// admit is ATP-BOUND over one binding: the residual variant it binds (recorded in bound), or the
// finding that refuses it.
func (sc acceptanceScope) admit(b ScenarioBinding, bound map[string]bool) (scenario.Scenario, *Finding) {
	v, known := sc.variants[b.Scenario]
	var msg string
	switch {
	case !known:
		msg = fmt.Sprintf("%s binds %q, which is no acceptance variant of this design (stale after a use-case edit?)", acceptanceLabel, b.Scenario)
	case sc.residual[b.Scenario] == "":
		msg = fmt.Sprintf("%s binds %q: not residual: covered by the %s projection, whose plan already binds it", acceptanceLabel, b.Scenario, clientOfVariant(b.Scenario))
	case bound[b.Scenario]:
		msg = fmt.Sprintf("%s binds %q twice", acceptanceLabel, b.Scenario)
	default:
		bound[b.Scenario] = true
		return v, nil
	}
	f := tpFinding(ruleATPBound, sc.l, "%s", msg)
	return v, &f
}

// clientContractKeys is the key of every Client contract (a facet is a contract of its own).
func clientContractKeys(in scenario.Input) []string {
	var keys []string
	for key, c := range in.Contracts {
		if c.Client {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// residualReasons is each residual variant's reasons as one line of prose, keyed by variant id.
func residualReasons(rs []scenario.Residual) map[string]string {
	by := map[string][]string{}
	for _, r := range rs {
		by[r.Scenario] = append(by[r.Scenario], residualText(r.Reason))
	}
	out := make(map[string]string, len(by))
	for id, reasons := range by {
		out[id] = strings.Join(reasons, "; ")
	}
	return out
}

func sortedResidual(residual map[string]string) []string {
	ids := make([]string, 0, len(residual))
	for id := range residual {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// residualText is a residual reason as prose for a finding.
func residualText(r scenario.ResidualReason) string {
	switch v := r.(type) {
	case scenario.CrossClient:
		return "cross-client path through " + strings.Join(v.Clients, ", ")
	case scenario.EventEntered:
		return "entered by an event at " + v.Node
	case scenario.LaterOutcome:
		return "later outcome " + v.OutcomeNode + " after " + v.AfterStimulus
	}
	return ""
}

// clientOfVariant is the cover client of a variant id "<scenario>@<client>".
func clientOfVariant(id string) string {
	if i := strings.LastIndexByte(id, '@'); i >= 0 {
		return id[i+1:]
	}
	return id
}

// ---- ATP-REF ----

// planRefInClientPlanFindings refuses a planRef anywhere in a client plan: the reference runs from
// the acceptance plan to a client plan, never the other way, and a client plan stands alone.
func planRefInClientPlanFindings(p Project) []Finding {
	if p.PhaseArtifacts == nil {
		return nil
	}
	keys := make([]string, 0, len(p.PhaseArtifacts.TestPlan))
	for key := range p.PhaseArtifacts.TestPlan {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out []Finding
	for _, key := range keys {
		l := loc(0, "phaseArtifacts.testPlan."+key)
		for _, b := range p.PhaseArtifacts.TestPlan[key].Bindings {
			for _, site := range planRefSites(b) {
				out = append(out, tpFinding(ruleATPRef, l, "%s %s %s: input %q is a planRef, which only the acceptance plan may carry", key, b.Scenario, site.list.where, site.input))
			}
		}
	}
	return out
}

// inputList is one list of call inputs in a binding, with where it sits and, for a list that names
// its callee (an arrange call, a probe), the callee.
type inputList struct {
	where     string
	seq       int // the step the list belongs to; 0 for arrange
	component string
	operation string
	inputs    []TestArg
}

// inputLists is every input list of a binding: its arrange calls, its steps and their probes.
func inputLists(b ScenarioBinding) []inputList {
	var out []inputList
	for i, a := range b.Arrange {
		if c, ok := a.(ContractCall); ok {
			out = append(out, inputList{where: fmt.Sprintf("arrange %d", i), component: c.Component, operation: c.Operation, inputs: c.Inputs})
		}
	}
	for _, st := range b.Steps {
		out = append(out, inputList{where: fmt.Sprintf("step %d", st.Seq), seq: st.Seq, operation: st.Operation, inputs: st.Inputs})
		for i, pr := range st.Probes {
			out = append(out, inputList{where: fmt.Sprintf("step %d probe %d", st.Seq, i), seq: st.Seq, component: pr.Component, operation: pr.Operation, inputs: pr.Inputs})
		}
	}
	return out
}

// planRefSite is one input of a binding whose value is a planRef.
type planRefSite struct {
	list  inputList
	input string
	ref   PlanRef
}

// planRefSites is every planRef input of a binding, in input-list order.
func planRefSites(b ScenarioBinding) []planRefSite {
	var out []planRefSite
	for _, il := range inputLists(b) {
		for _, in := range il.inputs {
			if ref, ok := in.Value.(PlanRef); ok {
				out = append(out, planRefSite{list: il, input: in.Name, ref: ref})
			}
		}
	}
	return out
}

// bindingRefFindings is ATP-REF over one acceptance binding: every planRef resolves to a literal,
// and the parameter it feeds has the schema of the parameter the literal was bound to.
func bindingRefFindings(p Project, v scenario.Scenario, b ScenarioBinding, l *Location) []Finding {
	var out []Finding
	for _, site := range planRefSites(b) {
		comp := site.list.component
		if comp == "" {
			comp = stepComponent(v, site.list.seq)
		}
		section := fmt.Sprintf("%s %s %s input %q", acceptanceLabel, b.Scenario, site.list.where, site.input)
		if _, problem := resolvePlanRef(p, site.ref); problem != "" {
			out = append(out, tpFinding(ruleATPRef, l, "%s: the planRef to %s %s step %d does not resolve: %s", section, site.ref.Component, site.ref.Scenario, site.ref.Seq, problem))
		} else if msg := planRefParamProblem(p, site.ref, comp, site.list.operation, site.input); msg != "" {
			out = append(out, tpFinding(ruleATPRef, l, "%s: %s", section, msg))
		}
	}
	return out
}

// stepComponent is the component the stimulus numbered seq addresses.
func stepComponent(v scenario.Scenario, seq int) string {
	for _, st := range v.Stimuli {
		if st.Seq == seq {
			return st.Input.To
		}
	}
	return ""
}

// planRefParamProblem is the reason the parameter a planRef feeds (param of operation op of comp)
// differs from the parameter its literal was bound to; "" when they agree or either is not
// resolvable (another rule's finding).
func planRefParamProblem(p Project, ref PlanRef, comp, op, param string) string {
	have, ok := paramSchema(p, comp, op, param)
	if !ok {
		return ""
	}
	refOp := planRefOperation(p, ref)
	want, ok := paramSchema(p, ref.Component, refOp, ref.Input)
	if !ok || schemasAgree(have, want) {
		return ""
	}
	return fmt.Sprintf("parameter %q of %s.%s is %s, but the planRef names input %q of %s.%s, which is %s: a shared input is the same parameter", param, comp, op, have, ref.Input, ref.Component, refOp, want)
}

// planRefOperation is the operation of the client plan step a planRef names ("" when it names none).
func planRefOperation(p Project, ref PlanRef) string {
	rec, ok := planRecord(p, ref.Component)
	if !ok {
		return ""
	}
	bi := slices.IndexFunc(rec.Bindings, func(b ScenarioBinding) bool { return b.Scenario == ref.Scenario })
	if bi < 0 {
		return ""
	}
	steps := rec.Bindings[bi].Steps
	si := slices.IndexFunc(steps, func(st StepBinding) bool { return st.Seq == ref.Seq })
	if si < 0 {
		return ""
	}
	return steps[si].Operation
}

// paramSchema is the schema of one parameter of one operation of comp's contract.
func paramSchema(p Project, comp, op, param string) (json.RawMessage, bool) {
	_, look := findContract(p, comp)
	cop, ok := look(op)
	if !ok {
		return nil, false
	}
	for _, prm := range cop.Params {
		if prm.Name == param {
			return prm.Schema, true
		}
	}
	return nil, false
}

// schemasAgree compares two parameter schemas: two $refs by their $def name, anything else as JSON.
func schemasAgree(a, b json.RawMessage) bool {
	ta, aRef := schemaRefTail(a)
	tb, bRef := schemaRefTail(b)
	if aRef || bRef {
		return aRef && bRef && ta == tb
	}
	return sameJSON(a, b)
}

// ---- ATP-PROBE-AS ----

// bindingProbeAsFindings is ATP-PROBE-AS over one binding: a probe run as another actor names one
// of the plan's principals, or the test has no identity to run it as.
func bindingProbeAsFindings(rec AcceptancePlanRecord, b ScenarioBinding, l *Location) []Finding {
	var out []Finding
	for _, st := range b.Steps {
		for i, pr := range st.Probes {
			if pr.As == "" {
				continue
			}
			if _, ok := rec.Principals[pr.As]; !ok {
				out = append(out, tpFinding(ruleATPProbeAs, l, "%s %s step %d probe %d (%s.%s): runs as %q, which is none of the plan's principals; record who the observation runs as", acceptanceLabel, b.Scenario, st.Seq, i, pr.Component, pr.Operation, pr.As))
			}
		}
	}
	return out
}

// ---- ATP-BROWSER ----

// browserFindings is ATP-BROWSER: the app serves one browser per launch, so every web step and every
// web probe of the plan runs as one actor, BrowserActor, who is one of the plan's principals.
func browserFindings(p Project, env tpEnv, rec AcceptancePlanRecord, live []scenario.Scenario, l *Location) []Finding {
	uses := browserUses(p, env, live, rec.Bindings)
	if len(uses) == 0 {
		return nil
	}
	switch _, isPrincipal := rec.Principals[rec.BrowserActor]; {
	case rec.BrowserActor == "":
		return []Finding{tpFinding(ruleATPBrowser, l, "%s: %s is a web step or probe, but the plan names no browserActor: the app serves one browser per launch, so name the actor whose browser every web step and probe runs in", acceptanceLabel, uses[0].where)}
	case !isPrincipal:
		return []Finding{tpFinding(ruleATPBrowser, l, "%s: browserActor %q is none of the plan's principals", acceptanceLabel, rec.BrowserActor)}
	}
	var out []Finding
	for _, u := range uses {
		if !slices.Contains(u.actors, rec.BrowserActor) {
			out = append(out, tpFinding(ruleATPBrowser, l, "%s: %s runs as %s, but every web step and probe runs in the browser of %q", acceptanceLabel, u.where, strings.Join(u.actors, " or "), rec.BrowserActor))
		}
	}
	return out
}

// browserUse is one web step or web probe of a bound variant and the actors that may run it.
type browserUse struct {
	where  string
	actors []string
}

// browserUses lists the web steps and web probes of the live bindings, in binding then step order.
func browserUses(p Project, env tpEnv, live []scenario.Scenario, bindings []ScenarioBinding) []browserUse {
	byID := map[string]scenario.Scenario{}
	for _, v := range live {
		byID[v.ID] = v
	}
	var out []browserUse
	for _, b := range bindings {
		if v, ok := byID[b.Scenario]; ok && b.Skip == nil && len(b.Steps) == len(v.Stimuli) {
			out = append(out, bindingBrowserUses(p, env, v, b)...)
		}
	}
	return out
}

// bindingBrowserUses is the web steps and web probes of one bound variant.
func bindingBrowserUses(p Project, env tpEnv, v scenario.Scenario, b ScenarioBinding) []browserUse {
	var out []browserUse
	for i, st := range b.Steps {
		stim := v.Stimuli[i]
		if contract, _ := findContract(p, stim.Input.To); env.isWeb(stim.Input.To, contract) {
			out = append(out, browserUse{where: fmt.Sprintf("%s step %d (%s)", b.Scenario, st.Seq, stim.Input.To), actors: stimulusCallers(stim)})
		}
		out = append(out, probeBrowserUses(p, env, b.Scenario, st, stimulusCallers(stim))...)
	}
	return out
}

// probeBrowserUses is the web probes of one step; a probe runs as the step's callers unless it says as.
func probeBrowserUses(p Project, env tpEnv, scenarioID string, st StepBinding, callers []string) []browserUse {
	var out []browserUse
	for j, pr := range st.Probes {
		pc, _ := findContract(p, pr.Component)
		if !env.isWeb(pr.Component, pc) {
			continue
		}
		actors := callers
		if pr.As != "" {
			actors = []string{pr.As}
		}
		out = append(out, browserUse{where: fmt.Sprintf("%s step %d probe %d (%s.%s)", scenarioID, st.Seq, j, pr.Component, pr.Operation), actors: actors})
	}
	return out
}
