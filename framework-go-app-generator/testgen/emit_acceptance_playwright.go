package testgen

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// emit_acceptance_playwright.go emits the browser-bearing acceptance variants: one Playwright spec
// per variant under <UITestsDir>/acceptance/, run by the generated config beside it.
//
// The browser is one identity per launch (the app runs as the process principal, the plan's
// browser actor): a browser step or probe runs in the page. Every other step is a request the
// spec makes itself as the actor it names: an mcpCall for an MCP client's tool, a contractCall for a
// REST client's route or a Manager reached directly; a probe of a route is `probe`. The `as` of each
// is the actor's subject, spelled from the plan's principals.

// emitAcceptancePlaywright emits the specs of variants and the runtime, config and reporter they
// run under. The element inventory a spec locates through is the surface's own (elements.gen.ts of
// its client plan), imported from beside it.
func emitAcceptancePlaywright(out *Output, cfg Config, web webEnv, routes map[string]restRoute, p methodcheck.Project, rec methodcheck.AcceptancePlanRecord, variants []accBound) error {
	dir := path.Join(cfg.UITestsDir, acceptanceKey)
	plan := componentPlan{Key: acceptanceKey, Principals: rec.Principals}
	for _, ab := range variants {
		spec, warnings, err := acceptanceSpec(p, web, routes, plan, ab)
		if err != nil {
			return fmt.Errorf("testgen: acceptance %s: %w", ab.Scenario.ID, err)
		}
		out.Warnings = append(out.Warnings, warnings...)
		out.Generated[path.Join(dir, ab.Scenario.ID+".spec.gen.ts")] = []byte(spec)
	}
	out.Generated[path.Join(dir, "runtime.gen.ts")] = []byte(genHeader + "\n" + tsRuntimeSource)
	out.Generated[path.Join(dir, "playwright.config.gen.ts")] = []byte(acceptancePlaywrightConfig(playwrightResultsDir(acceptanceKey)))
	out.Generated[path.Join(dir, "reporter.gen.ts")] = []byte(playwrightReporter)
	return nil
}

// acceptancePlaywrightConfig is the generated config of the acceptance directory: the same worker,
// timeouts, trace and reporters as a surface's, with the one project the variants run in (there is
// no surface check here; each surface's own directory holds it).
func acceptancePlaywrightConfig(results string) string {
	return playwrightConfigWith(results, `
    { name: 'scenarios', testMatch: /\.spec\.gen\.ts$/ },`)
}

// accSpec is one variant's spec under construction.
type accSpec struct {
	*specWriter
	ab     accBound
	routes map[string]restRoute
	rec    methodcheck.AcceptancePlanRecord
	// principalsUsed: some call is made as a named actor, so the spec declares the subjects.
	principalsUsed bool
}

// acceptanceSpec is one variant's spec: the latency, the typed arrange, then per step its action
// or call, the observables a browser step expects and the probes.
func acceptanceSpec(p methodcheck.Project, web webEnv, routes map[string]restRoute, plan componentPlan, ab accBound) (string, []string, error) {
	design, ok := designOf(p, ab.surface, ab.surfaceSC)
	if !ok {
		return "", nil, fmt.Errorf("no uiDesign is recorded for surface %s, so no element can be located: record its UI design (screens and elements) first", ab.surface)
	}
	if err := refuseAdvanceClock(acceptanceKey, ab.boundScenario); err != nil {
		return "", nil, err
	}
	w := &specWriter{plan: plan, design: design, web: web, uses: map[string]bool{}, byID: map[string]methodcheck.UIElement{}}
	for _, e := range designElements(design) {
		w.byID[e.ID] = e
	}
	a := &accSpec{specWriter: w, ab: ab, routes: routes, rec: *p.PhaseArtifacts.AcceptancePlan}
	bs := ab.boundScenario
	id := bs.Scenario.ID
	annotation := fmt.Sprintf("{ type: 'scenario', description: %s }", tsStr(id))
	fmt.Fprintf(&w.b, "test(%s, async ({ page }, testInfo) => {\n", tsStr(id+" — "+bs.Scenario.Title))
	w.line("testInfo.annotations.push(%s);", annotation)
	w.use("installLatency")
	w.line("await installLatency(page, Number(process.env.AIARCH_UI_LATENCY_MS ?? 1500));")
	if err := w.arrange(bs); err != nil {
		return "", nil, err
	}
	for _, st := range bs.Steps {
		if err := a.step(st); err != nil {
			return "", nil, fmt.Errorf("step %d: %w", st.Bind.Seq, err)
		}
	}
	w.b.WriteString("});\n")
	return a.file(), w.warnings, nil
}

// file is the spec: header, imports (only what the body used), the principals' subjects, body.
func (a *accSpec) file() string {
	var b strings.Builder
	b.WriteString(genHeader + "\n")
	b.WriteString("// Source: project.json .phaseArtifacts.acceptancePlan × scenario.ForAcceptance(every client)\n")
	b.WriteString(a.imports())
	if a.principalsUsed {
		b.WriteString(a.principalsDecl())
	}
	b.WriteString("\n" + a.b.String())
	return b.String()
}

// imports is the import lines of exactly the names the body used.
func (a *accSpec) imports() string {
	w := a.specWriter
	var b strings.Builder
	playwright := []string{"test"}
	if w.uses["expect"] {
		playwright = append(playwright, "expect")
	}
	fmt.Fprintf(&b, "import { %s } from '@playwright/test';\n", strings.Join(playwright, ", "))
	if w.uses["locate"] {
		fmt.Fprintf(&b, "import { locate } from '../%s/elements.gen';\n", a.ab.surface)
	}
	var names []string
	for n := range w.uses {
		if n != "expect" && n != "locate" {
			names = append(names, n)
		}
	}
	slices.Sort(names)
	if len(names) > 0 {
		fmt.Fprintf(&b, "import { %s } from './runtime.gen';\n", strings.Join(names, ", "))
	}
	return b.String()
}

// principalsDecl declares the subject each actor of the plan is, as the launch allowlisted it.
func (a *accSpec) principalsDecl() string {
	entries := make([]string, 0, len(a.rec.Principals))
	for _, name := range sortedKeys(a.rec.Principals) {
		entries = append(entries, tsStr(name)+": "+tsStr(a.rec.Principals[name].Subject))
	}
	return "\n// The subject each actor of the plan is, as the launch allowlisted it.\nconst principals: Record<string, string> = { " + strings.Join(entries, ", ") + " };\n"
}

// as is the `as` property of a call made as actor ("" is the process principal: no property).
func (a *accSpec) as(actor string) string {
	if actor == "" {
		return ""
	}
	a.principalsUsed = true
	return ", as: principals[" + tsStr(actor) + "]"
}

// step emits one step: a browser action with what the user sees, or a request made as the actor.
func (a *accSpec) step(st boundStep) error {
	w := a.specWriter
	t := a.ab.targets[st.Bind.Seq]
	actor := stepPrincipal(w.plan, st)
	w.line("// step %d: %s (node %s) as %s", st.Bind.Seq, st.Op.Name, st.Stim.Node, orProcess(actor))
	var err error
	if t.isWeb() {
		err = a.browserStep(st)
	} else {
		err = a.call(t, st.Op, st.Bind.Inputs, st.Bind.Expect, actor, "step")
	}
	if err != nil {
		return err
	}
	for i, pr := range st.Bind.Probes {
		if err := a.probe(st, i+1, pr); err != nil {
			return fmt.Errorf("probe for %s: %w", pr.For, err)
		}
	}
	return nil
}

// browserStep is a step that is a browser action: each bound action, then what the user sees.
func (a *accSpec) browserStep(st boundStep) error {
	w := a.specWriter
	for _, in := range st.Bind.Inputs {
		if err := w.action(in); err != nil {
			return err
		}
	}
	if err := w.observe(st); err != nil {
		return err
	}
	w.warnings = append(w.warnings, immediateWarnings(acceptanceKey, a.ab.Scenario.ID, st)...)
	return nil
}

// call emits a call that is not a browser action, made as actor: an mcpCall for an MCP client's tool,
// a contractCall for a step (probe for a probe) of a REST client's route or a Manager reached directly.
func (a *accSpec) call(t accTarget, op methodcheck.ContractOperation, inputs []methodcheck.TestArg, want methodcheck.TestExpect, actor, role string) error {
	w := a.specWriter
	if t.client && t.dialect == dialectMCP {
		call, err := a.toolCall(t, op, inputs, want)
		if err != nil {
			return err
		}
		w.use("mcpCall")
		w.line("await mcpCall(testInfo, { %s%s });", call, a.as(actor))
		return nil
	}
	call, err := a.routeCall(t, op, inputs, want)
	if err != nil {
		return err
	}
	fn := "contractCall"
	if role == "probe" {
		fn = "probe"
	}
	w.use(fn)
	w.line("await %s(testInfo, { %s%s });", fn, call, a.as(actor))
	return nil
}

func orProcess(actor string) string {
	if actor == "" {
		return "the process principal"
	}
	return actor
}

// targetOf is the component a probe addresses, with the dialect of a client.
func (a *accSpec) targetOf(component string) (accTarget, error) {
	key, sc, ok := findContractIn(a.web.contracts, component)
	if !ok {
		return accTarget{}, fmt.Errorf("component %q has no service contract to call", component)
	}
	t := accTarget{key: key, sc: sc, client: isClient(sc)}
	if t.client {
		d, err := clientDialect(key, sc)
		if err != nil {
			return accTarget{}, err
		}
		t.dialect = d
	}
	return t, nil
}

// probe emits one probe as the actor it names: what the browser shows, a tool call, or a request.
func (a *accSpec) probe(st boundStep, n int, pr methodcheck.Probe) error {
	w := a.specWriter
	t, err := a.targetOf(pr.Component)
	if err != nil {
		return err
	}
	op, ok := operationNamed(t.sc, pr.Operation)
	if !ok {
		return fmt.Errorf("operation %q is not on %s", pr.Operation, t.key)
	}
	actor := probeActor(w.plan, st, pr)
	w.line("// probe %d.%d for %s: %s.%s as %s", st.Bind.Seq, n, pr.For, pr.Component, pr.Operation, orProcess(actor))
	if !t.isWeb() {
		return a.call(t, op, pr.Inputs, pr.Expect, actor, "probe")
	}
	for _, in := range pr.Inputs {
		if err := w.action(in); err != nil {
			return err
		}
	}
	return w.observe(boundStep{Bind: methodcheck.StepBinding{Seq: st.Bind.Seq, Expect: pr.Expect}})
}

// toolCall spells an MCP client's operation as the runtime's ToolCall: `tool, args, expect`.
func (a *accSpec) toolCall(t accTarget, op methodcheck.ContractOperation, inputs []methodcheck.TestArg, want methodcheck.TestExpect) (string, error) {
	args, err := a.literalArgs(t, op, inputs)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("tool: %s, args: %s, expect: %s", tsStr(toolName(op)), args, tsExpect(want)), nil
}

// routeCall spells an operation reached by its generated route as the runtime's ContractCall: a REST
// client's route (planned under the client's op name) or the route of a Manager reached directly.
func (a *accSpec) routeCall(t accTarget, op methodcheck.ContractOperation, inputs []methodcheck.TestArg, want methodcheck.TestExpect) (string, error) {
	var route restRoute
	var err error
	if t.client {
		route, err = restRouteOf(a.routes, t.key, op)
	} else {
		var ok bool
		if route, ok = a.web.routes[t.key][op.Name]; !ok {
			err = fmt.Errorf("%s.%s is no Manager's generated route, so a browser test cannot call it", t.key, op.Name)
		}
	}
	if err != nil {
		return "", err
	}
	args, err := a.literalArgs(t, op, inputs)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("verb: %s, path: %s, args: %s, expect: %s", tsStr(route.Verb), tsStr(route.Path), args, tsExpect(want)), nil
}

// literalArgs is the JSON object of a call's literal inputs. A ref reads an earlier step's result,
// which a spec that makes its calls beside the browser cannot see.
func (a *accSpec) literalArgs(t accTarget, op methodcheck.ContractOperation, inputs []methodcheck.TestArg) (string, error) {
	args, refs, err := inputsJSON(t.sc, op, inputs)
	if err != nil {
		return "", err
	}
	if len(refs) > 0 {
		return "", fmt.Errorf("argument %q of %s.%s reads the result of step %d: a call a browser spec makes is made without an earlier result", refs[0].param, t.key, op.Name, refs[0].ref.Step)
	}
	return args, nil
}
