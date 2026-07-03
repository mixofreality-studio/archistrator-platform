package methodcheck

import (
	"encoding/json"
	"fmt"
	"strings"
)

// rules_testplan.go is the STP-* System-Test-Plan validation family: it cross-checks
// the committed `.testingState.systemTestPlan` against the designed service contracts
// (`.serviceContracts`), the committed System architecture (slot 5) and the core use
// cases (slot 4). A black-box system-test plan is authored at MANAGER-OPERATION
// granularity ({component, operation} steps); these rules prove each step actually
// resolves to a real designed operation, that its arguments and expected outcomes are
// contract-consistent, that its walk is a legal traversal of the use case's dynamic
// view, and that every scenario traces to a real core use case with adversarial cover.
//
// The family runs ONLY when the plan is non-empty AND its prerequisites (service
// contracts + slot 5 + slot 4) are committed; a plan authored without them is a
// ContractMisuse coherence fault (surfaced by validateSystemTestPlan's error return,
// NOT a finding). The family is NOT wired into ValidateProject yet — it is exercised
// behind validateSystemTestPlan pending app-side wiring.
//
// Component naming bridge: STP steps name a contract KEY (camelCase, e.g.
// settlementManager); dynamic-view participants are kebab-case slot-5 ids
// (settlement-manager). Both reduce to the same key under the shared normalizer, so
// the walk check matches across the two vocabularies.

const (
	ruleSTPOpExists      RuleID = "STP-OP-EXISTS"
	ruleSTPStaleContract RuleID = "STP-STALE-CONTRACT"
	ruleSTPArgName       RuleID = "STP-ARG-NAME"
	ruleSTPArgType       RuleID = "STP-ARG-TYPE"
	ruleSTPExpectShape   RuleID = "STP-EXPECT-SHAPE"
	ruleSTPWalkLegal     RuleID = "STP-WALK-LEGAL" //nolint:gosec // G101 false positive: a rule identifier, not a credential
	ruleSTPWalkMode      RuleID = "STP-WALK-MODE"  //nolint:gosec // G101 false positive: a rule identifier, not a credential
	ruleSTPUCTrace       RuleID = "STP-UC-TRACE"
	ruleSTPCaseKind      RuleID = "STP-CASE-KIND"
)

// stpContext carries the resolved, indexed inputs one validateSystemTestPlan run
// shares across every scenario/case/step — built once so the per-step predicates
// stay allocation-light.
type stpContext struct {
	normalize     func(string) string
	contractByKey map[string]ServiceContract // normalize(component key) → contract
	coreUCIDs     map[string]bool            // normalize(core use-case id) → true
	dvByUC        map[string]DynamicView     // normalize(dynamic-view UseCaseID) → view
	compIDByKey   map[string]string          // normalize(component id or name) → canonical id
	seenStale     map[string]bool            // stale-contract keys already reported (dedup)
}

// validateSystemTestPlan is the STP-* family orchestration. It returns findings for a
// committed System Test Plan, or a *ContractMisuseError when the plan is present but a
// prerequisite artifact (service contracts, System, core use cases) is not committed —
// the same coherence-fault posture the design verbs use. When the plan is absent or
// empty the whole family is a no-op (nil, nil). normalize may be nil (defaults to
// defaultNormalizer); pass a spec's NameNormalizer to honor a module's match keys.
func validateSystemTestPlan(p Project, normalize func(string) string) ([]Finding, error) {
	stp := p.systemTestPlan()
	if stp == nil || len(stp.Scenarios) == 0 {
		return nil, nil // the family is a no-op when the plan is absent/empty
	}
	if normalize == nil {
		normalize = defaultNormalizer
	}
	sys, cuc, err := stpPrerequisites(p)
	if err != nil {
		return nil, err
	}

	ctx := buildSTPContext(p, sys, cuc, normalize)
	var out []Finding
	for si, scn := range stp.Scenarios {
		out = append(out, stpUCTrace(scn, ctx, si)...)
		out = append(out, stpCaseKind(scn, si)...)
		dv, hasDV := ctx.dvByUC[normalize(scn.UseCase)]
		for _, cs := range scn.Cases {
			out = append(out, stpValidateCase(scn, cs, dv, hasDV, ctx, si)...)
		}
	}
	sortFindings(out)
	return out, nil
}

// stpPrerequisites resolves the committed inputs the family depends on, returning a
// *ContractMisuseError when the plan is present but a prerequisite (service contracts,
// System, core use cases) is not committed — the same coherence-fault posture as the
// design verbs.
func stpPrerequisites(p Project) (System, CoreUseCases, error) {
	if len(p.ServiceContracts) == 0 {
		return System{}, CoreUseCases{}, &ContractMisuseError{Msg: "validateSystemTestPlan: systemTestPlan present but no service contracts are committed (cannot resolve any step operation)"}
	}
	sys, sysOK, err := p.system()
	if err != nil {
		return System{}, CoreUseCases{}, err
	}
	if !sysOK {
		return System{}, CoreUseCases{}, &ContractMisuseError{Msg: "validateSystemTestPlan: systemTestPlan present but the System architecture (slot 5) is not committed"}
	}
	cuc, cucOK, err := p.coreUseCases()
	if err != nil {
		return System{}, CoreUseCases{}, err
	}
	if !cucOK {
		return System{}, CoreUseCases{}, &ContractMisuseError{Msg: "validateSystemTestPlan: systemTestPlan present but the core use cases (slot 4) are not committed"}
	}
	return sys, cuc, nil
}

// buildSTPContext indexes the committed inputs for one plan validation run.
func buildSTPContext(p Project, sys System, cuc CoreUseCases, normalize func(string) string) stpContext {
	ctx := stpContext{
		normalize:     normalize,
		contractByKey: make(map[string]ServiceContract, len(p.ServiceContracts)),
		coreUCIDs:     make(map[string]bool),
		dvByUC:        make(map[string]DynamicView, len(sys.DynamicViews)),
		compIDByKey:   make(map[string]string, len(sys.Components)*2),
		seenStale:     make(map[string]bool),
	}
	for key, c := range p.ServiceContracts {
		ctx.contractByKey[normalize(key)] = c
	}
	indexCoreUseCases(&ctx, cuc)
	indexDynamicViews(&ctx, sys)
	indexComponents(&ctx, sys)
	return ctx
}

// indexCoreUseCases records the normalized ids of the CORE use cases for STP-UC-TRACE.
func indexCoreUseCases(ctx *stpContext, cuc CoreUseCases) {
	for _, d := range cuc.Decisions {
		if d.UseCase.Classification == classCore && d.UseCase.ID != "" {
			ctx.coreUCIDs[ctx.normalize(d.UseCase.ID)] = true
		}
	}
}

// indexDynamicViews maps each dynamic view by its normalized UseCaseID for the walk.
func indexDynamicViews(ctx *stpContext, sys System) {
	for _, dv := range sys.DynamicViews {
		if dv.UseCaseID != "" {
			ctx.dvByUC[ctx.normalize(dv.UseCaseID)] = dv
		}
	}
}

// indexComponents maps each component's normalized id AND name to its canonical id, so
// a step keyed on either form resolves. Both reduce to the same key under the shared
// normalizer; the id wins when both are present.
func indexComponents(ctx *stpContext, sys System) {
	for _, c := range sys.Components {
		if c.ID != "" {
			ctx.compIDByKey[ctx.normalize(c.ID)] = c.ID
		}
		if c.Name == "" {
			continue
		}
		if _, ok := ctx.compIDByKey[ctx.normalize(c.Name)]; !ok {
			ctx.compIDByKey[ctx.normalize(c.Name)] = c.ID
		}
	}
}

// ---- STP-UC-TRACE ----

// stpUCTrace emits STP-UC-TRACE (Error) when a scenario's useCase does not resolve to
// a real CORE use-case id in slot 4.
func stpUCTrace(scn TestScenario, ctx stpContext, si int) []Finding {
	if ctx.coreUCIDs[ctx.normalize(scn.UseCase)] {
		return nil
	}
	section := fmt.Sprintf("scenario %s", scn.ID)
	return []Finding{{
		RuleID:   ruleSTPUCTrace,
		Severity: SeverityError,
		Message:  fmt.Sprintf("%s: useCase %q does not resolve to a core use case in slot 4; every scenario must trace to a real core use case", section, scn.UseCase),
		Location: loc(si+1, section),
	}}
}

// ---- STP-CASE-KIND ----

// stpCaseKind emits STP-CASE-KIND. A scenario with zero cases is an Error (nothing is
// proven). Otherwise: no happy case → Warning; no negative/boundary case → Warning
// (the plan's value is its adversarial cover — Righting Software ch.14).
func stpCaseKind(scn TestScenario, si int) []Finding {
	section := fmt.Sprintf("scenario %s", scn.ID)
	if len(scn.Cases) == 0 {
		return []Finding{{
			RuleID:   ruleSTPCaseKind,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s: scenario has zero test cases; a scenario must carry at least one case (and ≥1 happy + ≥1 adversarial)", section),
			Location: loc(si+1, section),
		}}
	}
	var happy, adversarial int
	for _, c := range scn.Cases {
		switch strings.ToLower(c.Kind) {
		case "happy":
			happy++
		case "negative", "boundary":
			adversarial++
		}
	}
	var out []Finding
	if happy == 0 {
		out = append(out, Finding{
			RuleID:   ruleSTPCaseKind,
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("%s: scenario has no happy case; a scenario should prove the use case holds on at least one happy path", section),
			Location: loc(si+1, section),
		})
	}
	if adversarial == 0 {
		out = append(out, Finding{
			RuleID:   ruleSTPCaseKind,
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("%s: scenario has no negative/boundary case; the plan's value is its adversarial cover — add ≥1 failure or boundary case", section),
			Location: loc(si+1, section),
		})
	}
	return out
}

// ---- per-case step validation ----

// stpValidateCase runs the per-step contract + walk predicates for one case, then the
// walk mode/order predicates that need the whole step sequence.
func stpValidateCase(scn TestScenario, cs TestCase, dv DynamicView, hasDV bool, ctx stpContext, si int) []Finding {
	var out []Finding
	lastEdge := -1 // highest matched dynamic-view edge index (walk order monotonicity)
	for _, step := range cs.Steps {
		section := fmt.Sprintf("scenario %s case %s step %d (%s.%s)", scn.ID, cs.ID, step.Seq, step.Component, step.Operation)
		l := loc(si+1, section)

		contract, cOK := ctx.contractByKey[ctx.normalize(step.Component)]
		if !cOK {
			out = append(out, opNotFoundFinding(step, section, l, "no service contract is committed for that component"))
			out = append(out, stpWalk(step, dv, hasDV, ctx, section, l, &lastEdge)...)
			continue
		}
		op, opOK := findOperation(contract, step.Operation)
		if !opOK {
			out = append(out, opNotFoundFinding(step, section, l, fmt.Sprintf("its contract %q declares no operation with that exact (case-sensitive) name", contract.Component)))
		}
		// A never-detailed-designed stub (ALL ops params:null) gets a dedicated
		// diagnostic and SUPPRESSES the STP-ARG-* checks for that component (they would
		// only restate the stub's incompleteness). STP-OP-EXISTS still runs above.
		if contractAllParamsNull(contract) {
			out = append(out, staleFinding(contract, section, l, ctx)...)
		} else if opOK {
			out = append(out, stpArgName(step, op, section, l)...)
			out = append(out, stpArgType(step, op, contract, section, l)...)
			out = append(out, stpExpectShape(step, op, contract, section, l)...)
		}
		out = append(out, stpWalk(step, dv, hasDV, ctx, section, l, &lastEdge)...)
		out = append(out, stpWalkMode(step, cs, dv, hasDV, ctx, section, l)...)
	}
	return out
}

// opNotFoundFinding builds the STP-OP-EXISTS finding with a caller-supplied reason.
func opNotFoundFinding(step TestStep, section string, l *Location, reason string) Finding {
	return Finding{
		RuleID:   ruleSTPOpExists,
		Severity: SeverityError,
		Message:  fmt.Sprintf("%s: step operation does not resolve — %s; every step must name a real designed {component, operation}", section, reason),
		Location: l,
	}
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

// contractAllParamsNull reports whether a contract has ≥1 op and EVERY op carries a
// nil (JSON `null`) Params slice — the never-detailed-designed stub shape.
func contractAllParamsNull(c ServiceContract) bool {
	if len(c.Interface.Operations) == 0 {
		return false
	}
	for _, op := range c.Interface.Operations {
		if op.Params != nil {
			return false
		}
	}
	return true
}

// staleFinding emits STP-STALE-CONTRACT once per stale component per plan (deduped).
func staleFinding(c ServiceContract, section string, l *Location, ctx stpContext) []Finding {
	key := ctx.normalize(c.Component)
	if ctx.seenStale[key] {
		return nil
	}
	ctx.seenStale[key] = true
	return []Finding{{
		RuleID:   ruleSTPStaleContract,
		Severity: SeverityError,
		Message:  fmt.Sprintf("%s: contract %q is a never-detailed-designed stub (every operation has params:null); a scenario cannot exercise it until it is detailed-designed — STP-ARG-* checks are suppressed for it", section, c.Component),
		Location: l,
	}}
}

// ---- STP-ARG-NAME ----

// stpArgName emits STP-ARG-NAME (Error) for unknown args (an input naming no param)
// and for missing required (non-pointer) params.
func stpArgName(step TestStep, op ContractOperation, section string, l *Location) []Finding {
	paramByName := make(map[string]ContractParam, len(op.Params))
	for _, p := range op.Params {
		paramByName[p.Name] = p
	}
	suppliedByName := make(map[string]bool, len(step.Inputs))
	var out []Finding
	for _, in := range step.Inputs {
		suppliedByName[in.Name] = true
		if _, ok := paramByName[in.Name]; !ok {
			out = append(out, Finding{
				RuleID:   ruleSTPArgName,
				Severity: SeverityError,
				Message:  fmt.Sprintf("%s: input %q matches no parameter of operation %q; every input must name a contract parameter", section, in.Name, op.Name),
				Location: l,
			})
		}
	}
	for _, p := range op.Params {
		if p.Pointer {
			continue // pointer params are optional — absence is legal
		}
		if !suppliedByName[p.Name] {
			out = append(out, Finding{
				RuleID:   ruleSTPArgName,
				Severity: SeverityError,
				Message:  fmt.Sprintf("%s: required parameter %q of operation %q is not supplied by any input", section, p.Name, op.Name),
				Location: l,
			})
		}
	}
	return out
}

// ---- STP-ARG-TYPE ----

// stpArgType emits STP-ARG-TYPE (Error). For an input that matches a param: a set
// schemaRef must resolve to the SAME $defs entry (or primitive) as the param schema;
// an empty schemaRef falls back to a best-effort JSON-kind check of the value against
// the param's kind. Inputs that match no param are STP-ARG-NAME's concern, not this.
func stpArgType(step TestStep, op ContractOperation, contract ServiceContract, section string, l *Location) []Finding {
	paramByName := make(map[string]ContractParam, len(op.Params))
	for _, p := range op.Params {
		paramByName[p.Name] = p
	}
	var out []Finding
	for _, in := range step.Inputs {
		p, ok := paramByName[in.Name]
		if !ok {
			continue
		}
		if f := argTypeFinding(in, p, contract, op, section, l); f != nil {
			out = append(out, *f)
		}
	}
	return out
}

// argTypeFinding computes the STP-ARG-TYPE finding for one matched (input, param), or
// nil when consistent / indeterminate (best-effort — never fires on ambiguity).
func argTypeFinding(in TestArg, p ContractParam, contract ServiceContract, op ContractOperation, section string, l *Location) *Finding {
	paramRef, paramHasRef := schemaRefTail(p.Schema)
	if in.SchemaRef != "" {
		argRef := refTail(in.SchemaRef)
		if paramHasRef {
			if argRef != paramRef {
				return &Finding{
					RuleID:   ruleSTPArgType,
					Severity: SeverityError,
					Message:  fmt.Sprintf("%s: input %q declares schemaRef %q but parameter %q of operation %q is typed %q; the argument type contradicts the contract", section, in.Name, argRef, p.Name, op.Name, paramRef),
					Location: l,
				}
			}
			return nil
		}
		// Param is a primitive but the arg claims a named $def — a contradiction.
		return &Finding{
			RuleID:   ruleSTPArgType,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s: input %q declares schemaRef %q but parameter %q of operation %q is a primitive (%s); named-type argument contradicts a primitive parameter", section, in.Name, argRef, p.Name, op.Name, schemaPrimitiveKind(p.Schema)),
			Location: l,
		}
	}
	// Empty schemaRef → best-effort value-kind vs param-kind check.
	paramKind := schemaKind(p.Schema, contract.Defs)
	valueKind := jsonValueKind(in.Value)
	if paramKind == kindClassUnknown || valueKind == kindClassUnknown {
		return nil
	}
	if paramKind != valueKind {
		return &Finding{
			RuleID:   ruleSTPArgType,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s: input %q value is a %s but parameter %q of operation %q expects a %s", section, in.Name, valueKind, p.Name, op.Name, paramKind),
			Location: l,
		}
	}
	return nil
}

// ---- STP-EXPECT-SHAPE ----

// stpExpectShape emits STP-EXPECT-SHAPE (Error): an error-expected step requires the
// operation to declare error:true; a non-error step's declared result value must match
// the operation's result kind (object/array/scalar class — not full validation).
func stpExpectShape(step TestStep, op ContractOperation, contract ServiceContract, section string, l *Location) []Finding {
	var out []Finding
	if step.Expect.ErrorExpected {
		if !op.Error {
			out = append(out, Finding{
				RuleID:   ruleSTPExpectShape,
				Severity: SeverityError,
				Message:  fmt.Sprintf("%s: the step expects an error but operation %q does not declare error:true; it cannot fail the way the case asserts", section, op.Name),
				Location: l,
			})
		}
		return out // an error-expected step asserts no result value
	}
	if step.Expect.Result == "" {
		return out // no result asserted → nothing to shape-check
	}
	if len(op.Result) == 0 {
		out = append(out, Finding{
			RuleID:   ruleSTPExpectShape,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s: the step asserts a result value but operation %q declares no result (void)", section, op.Name),
			Location: l,
		})
		return out
	}
	resultKind := schemaKind(op.Result, contract.Defs)
	valueKind := jsonValueKind(step.Expect.Result)
	if resultKind == kindClassUnknown || valueKind == kindClassUnknown {
		return out // best-effort — do not fire on an indeterminate shape
	}
	if resultKind != valueKind {
		out = append(out, Finding{
			RuleID:   ruleSTPExpectShape,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s: the expected result is a %s but operation %q returns a %s", section, valueKind, op.Name, resultKind),
			Location: l,
		})
	}
	return out
}

// ---- STP-WALK-LEGAL ----

// stpWalk emits STP-WALK-LEGAL (Error) when a step is not a legal step of the
// scenario's use-case dynamic view: no view edge whose target IS the step's component
// AND whose label NAMES the step's operation, or such an edge exists only earlier than
// the last matched edge (the walk must respect the view's edge ordering). When the
// use case has no dynamic view the walk cannot be checked and the step is skipped.
func stpWalk(step TestStep, dv DynamicView, hasDV bool, ctx stpContext, section string, l *Location, lastEdge *int) []Finding {
	if !hasDV {
		return nil
	}
	firstMatch, orderedMatch := scanWalkEdges(step, dv, ctx, *lastEdge)
	if firstMatch == -1 {
		return []Finding{{
			RuleID:   ruleSTPWalkLegal,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s: no edge of the use-case dynamic view targets this component with a label naming the operation; the step is not a legal walk of the call chain", section),
			Location: l,
		}}
	}
	if orderedMatch == -1 {
		// A match exists, but only before an already-consumed edge — out of order.
		return []Finding{{
			RuleID:   ruleSTPWalkLegal,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s: the matching dynamic-view edge precedes an earlier step's edge; the case does not walk the call chain in view-edge order", section),
			Location: l,
		}}
	}
	*lastEdge = orderedMatch
	return nil
}

// scanWalkEdges finds, among the view's edges that target the step's component and
// name its operation, the index of the FIRST such edge and the first such edge at or
// after lastEdge (the order-respecting match). Either is -1 when none qualifies.
func scanWalkEdges(step TestStep, dv DynamicView, ctx stpContext, lastEdge int) (firstMatch, orderedMatch int) {
	compKeyN := ctx.normalize(step.Component)
	opN := ctx.normalize(step.Operation)
	firstMatch, orderedMatch = -1, -1
	for ei, e := range dv.Edges {
		if ctx.normalize(e.To) != compKeyN || !labelNamesOp(e.Label, opN, ctx.normalize) {
			continue
		}
		if firstMatch == -1 {
			firstMatch = ei
		}
		if ei >= lastEdge && orderedMatch == -1 {
			orderedMatch = ei
		}
	}
	return firstMatch, orderedMatch
}

// labelNamesOp reports whether a dynamic-view edge label names the operation: the
// normalized operation is a substring of the normalized label (edge labels carry the
// op plus its argument gloss, e.g. "startSystemDesign(projectId)").
func labelNamesOp(label, opN string, normalize func(string) string) bool {
	if opN == "" {
		return false
	}
	return strings.Contains(normalize(label), opN)
}

// ---- STP-WALK-MODE ----

// stpWalkMode emits STP-WALK-MODE (Error) when a step maps onto a QUEUED dynamic-view
// edge but is encoded as a synchronous expect-then-assert (it asserts a result or an
// error inline) without any later poll/observe step in the case — mirroring ruleDVMode
// granularity: queued work is fire-and-forget, so its outcome must be observed, not
// asserted synchronously. All-sync plans never trip this.
func stpWalkMode(step TestStep, cs TestCase, dv DynamicView, hasDV bool, ctx stpContext, section string, l *Location) []Finding {
	if !hasDV {
		return nil
	}
	if !stepMapsToQueuedEdge(step, dv, ctx) {
		return nil
	}
	// A synchronous assertion = the step asserts a concrete result or an inline error.
	synchronous := step.Expect.Result != "" || step.Expect.ErrorExpected
	if !synchronous {
		return nil
	}
	if caseHasLaterObserveStep(cs, step.Seq) {
		return nil
	}
	return []Finding{{
		RuleID:   ruleSTPWalkMode,
		Severity: SeverityError,
		Message:  fmt.Sprintf("%s: the step maps to a queued dynamic-view edge but asserts its outcome synchronously with no later poll/observe step; queued work must be observed, not asserted inline", section),
		Location: l,
	}}
}

// stepMapsToQueuedEdge reports whether some queued dynamic-view edge targets the step's
// component with a label naming its operation.
func stepMapsToQueuedEdge(step TestStep, dv DynamicView, ctx stpContext) bool {
	compKeyN := ctx.normalize(step.Component)
	opN := ctx.normalize(step.Operation)
	for _, e := range dv.Edges {
		if e.Mode != modeQueued {
			continue
		}
		if ctx.normalize(e.To) == compKeyN && labelNamesOp(e.Label, opN, ctx.normalize) {
			return true
		}
	}
	return false
}

// observeVerbs are the operation-name fragments that mark a poll/observe step — a
// synchronous read that observes the outcome of prior queued work.
var observeVerbs = []string{"poll", "observe", "get", "query", "read", "reconcile", "await", "wait"}

// caseHasLaterObserveStep reports whether a case has a step after afterSeq whose
// operation reads/observes (a poll for the queued outcome).
func caseHasLaterObserveStep(cs TestCase, afterSeq int) bool {
	for _, s := range cs.Steps {
		if s.Seq <= afterSeq {
			continue
		}
		op := strings.ToLower(s.Operation)
		for _, v := range observeVerbs {
			if strings.Contains(op, v) {
				return true
			}
		}
	}
	return false
}

// ---- schema-kind helpers ----

// kindClass is a coarse JSON value class for best-effort type checks.
type kindClass string

const (
	kindClassUnknown kindClass = ""
	kindClassObject  kindClass = "object"
	kindClassArray   kindClass = "array"
	kindClassScalar  kindClass = "scalar"
)

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
	case "string", "integer", "number", "boolean":
		return kindClassScalar
	default:
		return kindClassUnknown
	}
}

// jsonValueKind classifies a concrete step value (JSON or bare text) into a kindClass.
// A value that does not parse as JSON is treated as a bare string scalar (the plan
// stores unquoted scalars like "proj-aiarch-01").
func jsonValueKind(value string) kindClass {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return kindClassUnknown
	}
	var v any
	if err := json.Unmarshal([]byte(trimmed), &v); err != nil {
		return kindClassScalar // bare, unquoted text → a string scalar
	}
	switch v.(type) {
	case map[string]any:
		return kindClassObject
	case []any:
		return kindClassArray
	default:
		return kindClassScalar
	}
}
