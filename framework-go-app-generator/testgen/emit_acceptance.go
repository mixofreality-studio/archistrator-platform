package testgen

import (
	"bytes"
	"fmt"
	"go/format"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
)

// emit_acceptance.go emits the use-case acceptance tests (FPC plan 4, Amendment C) from
// `.phaseArtifacts.acceptancePlan`. The plan binds the acceptance variants no single client
// projection covers (scenario.AcceptanceResidual); each variant is one use-case path walked through
// one client, with the stimulus of every step addressed to the contract of the component it lands
// on. A step's operation is therefore resolved per step, not per plan.
//
// Two targets, chosen by the variant:
//
//   - a variant with a browser step or a browser probe is one Playwright spec,
//     <UITestsDir>/acceptance/<variant>.spec.gen.ts (emit_acceptance_playwright.go): a step that is
//     not a browser action is an mcpCall or contractCall as its actor, the browser as the plan's
//     browser actor;
//   - every other variant is a Go test in internal/acceptance (this file): the app is launched once
//     with every principal of the plan allowlisted, and each (client, actor) the variant uses is a
//     session on it, over MCP (emit_acceptance_mcpwire.go) or REST (emit_acceptance_http.go).
//
// Nothing an agent writes is code: a planRef input is inlined here, from the client plan it names,
// as the literal it resolves to. Acceptance variants are not null-discriminated (earmark E22).

// acceptanceKey names the acceptance plan wherever a component's key would appear: the scenario
// host's verdict key, the results directory, the Playwright directory and the Go package's TestMain.
const acceptanceKey = "acceptance"

const (
	acceptanceGoPackage = "internal/acceptance"
	acceptanceGoFile    = "acceptance_scenarios.gen_test.go"
)

// accTarget is the component one step's stimulus lands on: the contract its operation resolves on,
// and, for a client, the dialect its steps are driven in.
type accTarget struct {
	key     string
	sc      methodcheck.ServiceContract
	client  bool
	dialect dialect // meaningful when client
}

// accBound is one bound acceptance variant: the pairing of its stimuli with the plan's steps, the
// target of each step, and whether it needs a browser.
type accBound struct {
	boundScenario
	targets map[int]accTarget // by step seq
	// surface is the contract key of the one browser surface the variant drives, "" when it needs no
	// browser; surfaceSC is its contract.
	surface   string
	surfaceSC methodcheck.ServiceContract
}

func (ab accBound) web() bool { return ab.surface != "" }

// isWeb: the target is a browser surface, whose steps are page actions.
func (t accTarget) isWeb() bool { return t.client && t.dialect == dialectWeb }

// emitAcceptance emits the acceptance tests of the recorded plan: the Go file of the machine-only
// variants and one Playwright spec per browser-bearing variant.
func emitAcceptance(out *Output, cfg Config, web webEnv, routes map[string]restRoute, p methodcheck.Project, all []scenario.Scenario) error {
	rec := *p.PhaseArtifacts.AcceptancePlan
	bound, err := acceptanceBound(out, p, rec, all)
	if err != nil {
		return err
	}
	var machine, browser []accBound
	for _, ab := range bound {
		if ab.web() {
			browser = append(browser, ab)
		} else {
			machine = append(machine, ab)
		}
	}
	if len(machine) > 0 {
		if err := emitAcceptanceGo(out, cfg, p, rec, machine, routes); err != nil {
			return err
		}
	}
	if len(browser) > 0 {
		return emitAcceptancePlaywright(out, cfg, web, routes, p, rec, browser)
	}
	return nil
}

// clientKeys is the key of every Client contract, sorted: the cover clients of the acceptance variants.
func clientKeys(contracts map[string]methodcheck.ServiceContract) []string {
	var keys []string
	for key, sc := range contracts {
		if isClient(sc) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// acceptanceBound pairs every binding of the plan with its acceptance variant and resolves each step.
// A binding naming no variant of this design is an error (a stale plan); a residual variant with no
// binding is a warning, ATP-BOUND's domain.
func acceptanceBound(out *Output, p methodcheck.Project, rec methodcheck.AcceptancePlanRecord, all []scenario.Scenario) ([]accBound, error) {
	clients := clientKeys(p.ServiceContracts)
	variants := map[string]scenario.Scenario{}
	for _, v := range scenario.ForAcceptance(all, clients) {
		variants[v.ID] = v
	}
	var bound []accBound
	seen := map[string]bool{}
	for _, b := range rec.Bindings {
		v, ok := variants[b.Scenario]
		if !ok {
			return nil, fmt.Errorf("testgen: acceptance binds %q, which is no acceptance variant of this design (ATP-BOUND)", b.Scenario)
		}
		seen[b.Scenario] = true
		ab, err := bindAcceptance(out, p, rec, v, b)
		if err != nil {
			return nil, err
		}
		bound = append(bound, ab)
	}
	for _, r := range scenario.AcceptanceResidual(all, clients) {
		if !seen[r.Scenario] {
			seen[r.Scenario] = true // a variant with several reasons is named once
			out.Warnings = append(out.Warnings, fmt.Sprintf("testgen: acceptance has no binding for residual variant %q (ATP-BOUND)", r.Scenario))
		}
	}
	sort.SliceStable(bound, func(i, j int) bool { return bound[i].Scenario.ID < bound[j].Scenario.ID })
	return bound, nil
}

// bindAcceptance resolves one binding: every step on the contract its stimulus lands on, every
// planRef input to the literal it names, and the browser surface the variant needs.
func bindAcceptance(out *Output, p methodcheck.Project, rec methodcheck.AcceptancePlanRecord, v scenario.Scenario, b methodcheck.ScenarioBinding) (accBound, error) {
	ab := accBound{boundScenario: boundScenario{Scenario: v, Binding: b}, targets: map[int]accTarget{}}
	if b.Skip != nil {
		return ab, nil
	}
	arrange, err := resolveArrange(p, b.Arrange)
	if err != nil {
		return ab, fmt.Errorf("testgen: acceptance %s: %w", v.ID, err)
	}
	ab.Binding.Arrange = arrange
	plan := componentPlan{Key: acceptanceKey, Principals: rec.Principals}
	for _, step := range b.Steps {
		bs, ok, err := bindAcceptanceStep(out, p, v, step)
		if err != nil {
			return ab, err
		}
		if !ok {
			continue
		}
		target := bs.target
		ab.Steps = append(ab.Steps, bs.boundStep)
		ab.targets[step.Seq] = target
		if err := ab.noteBrowser(p, plan, rec, bs.boundStep, target); err != nil {
			return ab, err
		}
	}
	sort.SliceStable(ab.Steps, func(i, j int) bool { return ab.Steps[i].Bind.Seq < ab.Steps[j].Bind.Seq })
	return ab, nil
}

type accStep struct {
	boundStep
	target accTarget
}

// bindAcceptanceStep resolves one step on the contract of the component its stimulus lands on. A seq
// the variant's projection lacks is dropped with a warning (TP-STEP's domain, as in a client plan).
func bindAcceptanceStep(out *Output, p methodcheck.Project, v scenario.Scenario, step methodcheck.StepBinding) (accStep, bool, error) {
	stim, ok := stimulusAt(v, step.Seq)
	if !ok {
		out.Warnings = append(out.Warnings, fmt.Sprintf("testgen: acceptance variant %q binds seq %d, which the projection lacks", v.ID, step.Seq))
		return accStep{}, false, nil
	}
	key, sc, ok := findContractIn(p.ServiceContracts, stim.Input.To)
	if !ok {
		return accStep{}, false, fmt.Errorf("testgen: acceptance %s step %d: the stimulus lands on %q, which has no service contract", v.ID, step.Seq, stim.Input.To)
	}
	op, ok := operationNamed(sc, step.Operation)
	if !ok {
		return accStep{}, false, fmt.Errorf("testgen: acceptance %s step %d: operation %q is not on %s", v.ID, step.Seq, step.Operation, key)
	}
	t := accTarget{key: key, sc: sc, client: isClient(sc)}
	if t.client {
		d, err := clientDialect(key, sc)
		if err != nil {
			return accStep{}, false, err
		}
		t.dialect = d
	}
	var err error
	if step.Inputs, err = resolvePlanRefs(p, step.Inputs); err != nil {
		return accStep{}, false, fmt.Errorf("testgen: acceptance %s step %d: %w", v.ID, step.Seq, err)
	}
	probes := make([]methodcheck.Probe, len(step.Probes))
	for i, pr := range step.Probes {
		if pr.Inputs, err = resolvePlanRefs(p, pr.Inputs); err != nil {
			return accStep{}, false, fmt.Errorf("testgen: acceptance %s step %d probe %d: %w", v.ID, step.Seq, i+1, err)
		}
		probes[i] = pr
	}
	step.Probes = probes
	return accStep{boundStep: boundStep{Stim: stim, Bind: step, Op: op}, target: t}, true, nil
}

// noteBrowser records that the variant needs a browser and holds every browser step and probe to the
// plan's one browser actor (ATP-BROWSER) and to one surface.
func (ab *accBound) noteBrowser(p methodcheck.Project, plan componentPlan, rec methodcheck.AcceptancePlanRecord, st boundStep, t accTarget) error {
	if t.isWeb() {
		if err := ab.useBrowser(rec, stepPrincipal(plan, st), t, fmt.Sprintf("step %d", st.Bind.Seq)); err != nil {
			return err
		}
	}
	return ab.noteBrowserProbes(p, plan, rec, st)
}

// noteBrowserProbes is noteBrowser over a step's probes of a browser surface.
func (ab *accBound) noteBrowserProbes(p methodcheck.Project, plan componentPlan, rec methodcheck.AcceptancePlanRecord, st boundStep) error {
	for i, pr := range st.Bind.Probes {
		pt, ok, err := probeTarget(p.ServiceContracts, pr.Component)
		if err != nil {
			return err
		}
		if !ok || !pt.isWeb() {
			continue
		}
		if err := ab.useBrowser(rec, probeActor(plan, st, pr), pt, fmt.Sprintf("step %d probe %d", st.Bind.Seq, i+1)); err != nil {
			return err
		}
	}
	return nil
}

// probeTarget is the client a probe's component names, with its dialect; ok is false for a component
// that is no client (or has no contract).
func probeTarget(contracts map[string]methodcheck.ServiceContract, component string) (accTarget, bool, error) {
	key, sc, ok := findContractIn(contracts, component)
	if !ok || !isClient(sc) {
		return accTarget{}, false, nil
	}
	d, err := clientDialect(key, sc)
	return accTarget{key: key, sc: sc, client: true, dialect: d}, err == nil, err
}

// useBrowser records the browser surface t, refusing an actor other than the browser actor and a
// second surface.
func (ab *accBound) useBrowser(rec methodcheck.AcceptancePlanRecord, actor string, t accTarget, what string) error {
	if rec.BrowserActor == "" || actor != rec.BrowserActor {
		return fmt.Errorf("testgen: acceptance %s %s: a browser action runs as the plan's browser actor %q, not %q (ATP-BROWSER)", ab.Scenario.ID, what, rec.BrowserActor, actor)
	}
	if ab.surface != "" && ab.surface != t.key {
		return fmt.Errorf("testgen: acceptance %s %s: the variant drives two browser surfaces, %s and %s: one browser identity per launch (ATP-BROWSER)", ab.Scenario.ID, what, ab.surface, t.key)
	}
	ab.surface, ab.surfaceSC = t.key, t.sc
	return nil
}

// resolvePlanRefs is inputs with every planRef replaced by the literal it names in its client plan.
func resolvePlanRefs(p methodcheck.Project, inputs []methodcheck.TestArg) ([]methodcheck.TestArg, error) {
	out := make([]methodcheck.TestArg, len(inputs))
	for i, in := range inputs {
		if ref, ok := in.Value.(methodcheck.PlanRef); ok {
			lit, ok := methodcheck.ResolvePlanRef(p, ref)
			if !ok {
				return nil, fmt.Errorf("input %s: planRef %s/%s step %d input %q names no literal of a client plan (ATP-REF)", in.Name, ref.Component, ref.Scenario, ref.Seq, ref.Input)
			}
			in.Value = lit
		}
		out[i] = in
	}
	return out, nil
}

// resolveArrange is the arrange list with the planRef inputs of its contract calls resolved.
func resolveArrange(p methodcheck.Project, arrange []methodcheck.ArrangeCall) ([]methodcheck.ArrangeCall, error) {
	out := make([]methodcheck.ArrangeCall, len(arrange))
	for i, a := range arrange {
		if cc, ok := a.(methodcheck.ContractCall); ok {
			inputs, err := resolvePlanRefs(p, cc.Inputs)
			if err != nil {
				return nil, fmt.Errorf("arrange %d: %w", i+1, err)
			}
			cc.Inputs = inputs
			a = cc
		}
		out[i] = a
	}
	return out, nil
}

// ---- the Go half ----

// accGoEmit is the emission of the machine-only variants: a machine client's file shape, whose
// subject is the launched app, with the steps addressed per client.
type accGoEmit struct {
	*goEmit
	plan componentPlan
	rest map[string]bool // the REST clients the file calls, for their route tables
	mcp  map[string]bool
}

// emitAcceptanceGo emits internal/acceptance/acceptance_scenarios.gen_test.go: one test per variant.
func emitAcceptanceGo(out *Output, cfg Config, p methodcheck.Project, rec methodcheck.AcceptancePlanRecord, variants []accBound, routes map[string]restRoute) error {
	plan := componentPlan{Key: acceptanceKey, Principals: rec.Principals}
	for _, ab := range variants {
		plan.Scenarios = append(plan.Scenarios, ab.boundScenario)
	}
	g, err := newGoEmit(cfg, acceptanceGoPackage, []componentPlan{plan}, p.ServiceContracts)
	if err != nil {
		return err
	}
	g.tools = true
	// The file reaches the app over the wire and never imports its own package.
	delete(g.aliases, g.pkg)
	if err := g.claimPlan(plan); err != nil {
		return err
	}
	a := &accGoEmit{goEmit: g, plan: plan, rest: map[string]bool{}, mcp: map[string]bool{}}
	for _, ab := range variants {
		if ab.Binding.Skip == nil {
			g.subject = true
		}
		a.emitScenario(ab)
	}
	if g.err != nil {
		return g.err
	}
	src, err := a.file(routes)
	if err != nil {
		return err
	}
	out.Generated[path.Join(acceptanceGoPackage, acceptanceGoFile)] = src
	return nil
}

// emitScenario emits one variant as a test: launch the app with every principal of the plan
// allowlisted, open one session per (client, actor) the variant uses, run the arrange calls, then
// each step and its probes.
func (a *accGoEmit) emitScenario(ab accBound) {
	g, bs := a.goEmit, ab.boundScenario
	name := g.testName(a.plan, bs.Scenario.ID)
	fmt.Fprintf(&g.body, "// %s — %s (use case %s; path %s).\n", name, bs.Scenario.Title, bs.Scenario.UseCase, strings.Join(bs.Scenario.Path, " → "))
	fmt.Fprintf(&g.body, "func %s(t *testing.T) {\n", name)
	g.body.WriteString("\th := scenariohost.Start(t)\n")
	fmt.Fprintf(&g.body, "\th.RunScenario(t, %q, %q, func(t *testing.T) {\n", acceptanceKey, bs.Scenario.ID)
	if bs.Binding.Skip != nil {
		fmt.Fprintf(&g.body, "\t\tt.Skip(%q)\n", bs.Binding.Skip.Reason+" (until "+bs.Binding.Skip.Until+")")
		g.body.WriteString("\t})\n}\n\n")
		return
	}
	fmt.Fprintf(&g.body, "\t\tapp := h.LaunchDevstack(t, \"\"%s)\n", g.launchArgs(a.plan, bs))
	sessions := a.sessionsOf(ab)
	if len(sessions) == 0 {
		g.body.WriteString("\t\t_ = app // no step or probe goes through a client\n")
	}
	for _, s := range sessions {
		a.openSession(s)
	}
	g.emitArrange(a.plan, bs)
	d := accDialect{a: a, ab: ab}
	for _, st := range bs.Steps {
		g.emitStep(d, a.plan, bs, st)
	}
	g.body.WriteString("\t})\n}\n\n")
}

// accSession is one session on the launched app: a client reached as an actor ("" is the anonymous
// identity).
type accSession struct {
	client  string
	sc      methodcheck.ServiceContract
	dialect dialect
	actor   string
}

func (s accSession) variable() string {
	v := "session" + exportName(ident(s.client))
	if s.actor != "" {
		v += "As" + exportName(ident(s.actor))
	}
	return v
}

// sessionsOf is every session the variant's steps and probes call through, in first-use order.
func (a *accGoEmit) sessionsOf(ab accBound) []accSession {
	var out []accSession
	seen := map[string]bool{}
	add := func(s accSession) {
		if !seen[s.variable()] {
			seen[s.variable()] = true
			out = append(out, s)
		}
	}
	for _, st := range ab.Steps {
		if t := ab.targets[st.Bind.Seq]; t.client {
			add(accSession{client: t.key, sc: t.sc, dialect: t.dialect, actor: stepPrincipal(a.plan, st)})
		}
		for _, pr := range st.Bind.Probes {
			if s, ok := a.probeSession(st, pr); ok {
				add(s)
			}
		}
	}
	return out
}

// probeSession is the session a probe of a client calls through, as the actor it names.
func (a *accGoEmit) probeSession(st boundStep, pr methodcheck.Probe) (accSession, bool) {
	t, ok, err := probeTarget(a.contracts, pr.Component)
	if err != nil {
		a.fail(err)
	}
	if !ok {
		return accSession{}, false
	}
	return accSession{client: t.key, sc: t.sc, dialect: t.dialect, actor: probeActor(a.plan, st, pr)}, true
}

// probeActor is the actor a probe runs as: the one it names, else the step's own.
func probeActor(plan componentPlan, st boundStep, pr methodcheck.Probe) string {
	if pr.As != "" {
		return pr.As
	}
	return stepPrincipal(plan, st)
}

// who is the Principal expression a session is opened as.
func (a *accGoEmit) who(actor string) string {
	if actor == "" {
		return anonymousPrincipal
	}
	a.usesAs = true
	return "principal(" + strconv.Quote(actor) + ")"
}

// openSession emits the session's declaration: the client's own dialect, over the launched app.
func (a *accGoEmit) openSession(s accSession) {
	switch s.dialect {
	case dialectREST:
		a.rest[s.client] = true
		fmt.Fprintf(&a.body, "\t\t%s\n", restSessionLine(s, a.who(s.actor)))
	case dialectMCP:
		a.mcp[s.client] = true
		fmt.Fprintf(&a.body, "\t\t%s\n", mcpSessionLine(s, a.who(s.actor)))
	case dialectWeb:
		a.fail(fmt.Errorf("testgen: acceptance: %s is a browser surface: a variant that drives it is a Playwright spec, never a Go test", s.client))
	}
}

// accDialect drives each step through the client its stimulus lands on, or in process when it lands
// on a component below the clients (an event entering the system).
type accDialect struct {
	a  *accGoEmit
	ab accBound
}

func (d accDialect) stepComment(st boundStep, bs boundScenario) string {
	t := d.ab.targets[st.Bind.Seq]
	how := "in process on " + t.key
	switch {
	case t.client && t.dialect == dialectMCP:
		how = "tool " + toolName(st.Op) + " of " + t.key
	case t.client:
		how = "route " + toolName(st.Op) + " of " + t.key
	}
	return fmt.Sprintf("step %d: %s → %s (node %s%s)", st.Bind.Seq, st.Op.Name, how, st.Stim.Node, guardNote(bs.Scenario))
}

func (d accDialect) stepCall(plan componentPlan, bs boundScenario, st boundStep, out, errVar string) []string {
	g := d.a.goEmit
	seq := st.Bind.Seq
	t := d.ab.targets[seq]
	if t.client {
		args := fmt.Sprintf("args%d", seq)
		lines, err := g.toolArgs(t.sc, st.Op, st.Bind.Inputs, args, bs.Scenario.ID, seq)
		if err != nil {
			g.fail(err)
			return nil
		}
		session := accSession{client: t.key, actor: stepPrincipal(plan, st)}.variable()
		return append(lines, fmt.Sprintf("%s, %s = %s.Call(h.Context(t), %q, %s)", out, errVar, session, toolName(st.Op), args))
	}
	tgt, err := g.targetFor(t.key)
	if err != nil {
		g.fail(err)
		return nil
	}
	call := directCall{
		recv: tgt.construct(), alias: tgt.alias, sc: tgt.sc, op: st.Op, inputs: st.Bind.Inputs,
		ctx:   g.ctxExpr(hostRecv, tgt.layer, stepPrincipal(plan, st), strconv.Itoa(seq)),
		inVar: fmt.Sprintf("in%d", seq), scenario: bs.Scenario.ID, seq: seq,
	}
	return g.directLines(call, func(c string) string { return assignStmt(st.Op, c, out, errVar) })
}

// probeBody is the closure of a probe: a call through the probed client's session as the probe's actor,
// or a direct call on the component as that actor.
func (d accDialect) probeBody(plan componentPlan, bs boundScenario, st boundStep, n int, pr methodcheck.Probe) []string {
	g := d.a.goEmit
	seq := st.Bind.Seq
	key, sc, ok := findContractIn(g.contracts, pr.Component)
	if !ok {
		g.fail(fmt.Errorf("testgen: acceptance %s step %d probe %d: component %q has no service contract to call", bs.Scenario.ID, seq, n, pr.Component))
		return nil
	}
	op, ok := operationNamed(sc, pr.Operation)
	if !ok {
		g.fail(fmt.Errorf("testgen: acceptance %s step %d probe %d: operation %q is not on %s", bs.Scenario.ID, seq, n, pr.Operation, key))
		return nil
	}
	actor := probeActor(plan, st, pr)
	if isClient(sc) {
		lines, err := g.toolArgs(sc, op, pr.Inputs, "args", bs.Scenario.ID, seq)
		if err != nil {
			g.fail(err)
			return nil
		}
		session := accSession{client: key, actor: actor}.variable()
		lines = append([]string{probeOpen}, lines...)
		return append(lines, fmt.Sprintf("return %s.Call(%s.Context(t), %q, args)", session, probeRecv, toolName(op)))
	}
	tgt, err := g.targetFor(pr.Component)
	if err != nil {
		g.fail(err)
		return nil
	}
	call := directCall{
		recv: tgt.construct(), alias: tgt.alias, sc: tgt.sc, op: op, inputs: pr.Inputs,
		ctx:   g.ctxExpr(probeRecv, tgt.layer, actor, fmt.Sprintf("%d.%d", seq, n)),
		inVar: "in", scenario: bs.Scenario.ID, seq: seq,
	}
	return append([]string{probeOpen}, g.directLines(call, func(c string) string { return returnStmt(op, c) })...)
}

// file assembles the generated file: header, package clause, imports, TestMain, the tests, and, when
// any variant runs, the principals, one route table per REST client and the assertion helpers.
func (a *accGoEmit) file(routes map[string]restRoute) ([]byte, error) {
	g := a.goEmit
	var b bytes.Buffer
	b.WriteString(genHeader + "\n")
	b.WriteString("// Source: project.json .phaseArtifacts.acceptancePlan × scenario.ForAcceptance(every client)\n")
	fmt.Fprintf(&b, "\npackage %s_test\n\n", g.pkg)
	g.writeImports(&b, g.toolsImports())
	fmt.Fprintf(&b, "func TestMain(m *testing.M) { scenariohost.Main(m, %s) }\n\n", strconv.Quote(acceptanceKey))
	b.Write(g.body.Bytes())
	if g.subject {
		if err := a.helpers(&b, routes); err != nil {
			return nil, err
		}
	}
	src, err := format.Source(b.Bytes())
	if err != nil {
		return nil, fmt.Errorf("testgen: acceptance: generated file does not format: %w\n%s", err, b.String())
	}
	return src, nil
}

// helpers writes the principals, the route table of each REST client the file calls, and the
// assertion helpers. The code match is the REST one when any REST client is called: it accepts the
// error text an MCP tool carries as well.
func (a *accGoEmit) helpers(b *bytes.Buffer, routes map[string]restRoute) error {
	g := a.goEmit
	if g.usesAs {
		writePrincipalTable(b, g.principals)
	}
	for _, key := range sortedKeys(a.rest) {
		table, err := acceptanceRouteTable(key, g.contracts[key], routes)
		if err != nil {
			return err
		}
		b.WriteString(table)
	}
	fmt.Fprintf(b, expectHelpers, "`")
	b.WriteString(refHelpers)
	b.WriteString(errorHasCodeHelper(len(a.rest) > 0))
	b.WriteString(matchHelpers)
	return nil
}
