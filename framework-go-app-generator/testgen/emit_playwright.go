package testgen

import (
	"bytes"
	_ "embed" // tsRuntimeSource
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// tsRuntimeSource is the runtime every generated spec of a surface imports (installLatency, immediate,
// arrange, probe, assertNever): one source, type-checked on its own, copied unchanged beside the specs.
//
//go:embed tsruntime/runtime.ts
var tsRuntimeSource string

// emitPlaywright emits one web client's Playwright tests under <UITestsDir>/<component>/. Nothing is
// agent-owned and nothing in a spec is chosen by an agent but the typed record:
//
//   - elements.gen.ts: the UI design's inventory and `locate`, the role/name locator of every element;
//   - runtime.gen.ts: the shared runtime (latency, immediacy, arrange, probe);
//   - surface.spec.gen.ts: every element the design names is on its screen, checked before any scenario;
//   - <scenario>.spec.gen.ts: one spec per binding;
//   - the generated config and results reporter.
func emitPlaywright(out *Output, cfg Config, web webEnv, plan componentPlan) error {
	dir := path.Join(cfg.UITestsDir, plan.Key)
	design := methodcheck.UIDesignRecord{Surface: plan.Key}
	if plan.Design != nil {
		design = *plan.Design
	}
	if err := checkWithin(design); err != nil {
		return fmt.Errorf("testgen: %s: %w", plan.Key, err)
	}
	if err := refuseElementlessScreens(plan.Key, design); err != nil {
		return err
	}
	for _, bs := range plan.Scenarios {
		if err := emitScenarioSpec(out, dir, web, plan, design, bs); err != nil {
			return err
		}
	}
	out.Generated[path.Join(dir, "elements.gen.ts")] = []byte(elementsTS(plan.Key, design))
	out.Generated[path.Join(dir, "runtime.gen.ts")] = []byte(genHeader + "\n" + tsRuntimeSource)
	out.Generated[path.Join(dir, "surface.spec.gen.ts")] = []byte(surfaceSpecTS(plan.Key, design))
	out.Generated[path.Join(dir, "playwright.config.gen.ts")] = []byte(playwrightConfig(playwrightResultsDir(plan.Key)))
	out.Generated[path.Join(dir, "reporter.gen.ts")] = []byte(playwrightReporter)
	return nil
}

// emitScenarioSpec emits one binding's spec under dir.
func emitScenarioSpec(out *Output, dir string, web webEnv, plan componentPlan, design methodcheck.UIDesignRecord, bs boundScenario) error {
	if plan.Design == nil && bs.Binding.Skip == nil && len(bs.Steps) > 0 {
		return fmt.Errorf("testgen: %s %s: no uiDesign is recorded for the surface, so no element can be located: record its UI design (screens and elements) first", plan.Key, bs.Scenario.ID)
	}
	if err := refuseAdvanceClock(plan.Key, bs); err != nil {
		return err
	}
	spec, warnings, err := playwrightSpec(plan, bs, design, web)
	if err != nil {
		return fmt.Errorf("testgen: %s %s: %w", plan.Key, bs.Scenario.ID, err)
	}
	out.Warnings = append(out.Warnings, warnings...)
	out.Generated[path.Join(dir, bs.Scenario.ID+".spec.gen.ts")] = []byte(spec)
	return nil
}

// refuseAdvanceClock refuses a web binding that arranges advanceClock. The clock moves only on the
// scenario host; the booted server's components read real time, so the arrange would do nothing to
// the app under test and the scenario would assert against a world it did not set up. Never skipped,
// never emitted: earmark E17 is a model-declared clock for the composition root.
func refuseAdvanceClock(key string, bs boundScenario) error {
	for _, a := range bs.Binding.Arrange {
		if _, ok := a.(methodcheck.AdvanceClock); ok {
			return fmt.Errorf("testgen: %s scenario %q arranges advanceClock, which a booted server cannot honour yet (E17); arrange state through a contract call or seedResource instead", key, bs.Scenario.ID)
		}
	}
	return nil
}

// refuseElementlessScreens: the surface check opens each screen's route and asserts its elements
// are there; a screen with none would open a route and assert nothing, a test that cannot fail.
func refuseElementlessScreens(key string, design methodcheck.UIDesignRecord) error {
	for _, s := range design.Screens {
		if len(s.Elements) == 0 {
			return fmt.Errorf("testgen: %s: screen %q has no elements, so its surface test would assert nothing: give it the elements a user sees on %s, or drop the screen", key, s.ID, s.Route)
		}
	}
	return nil
}

// specWriter builds one scenario's spec: the body, and which runtime names the body used so the
// import line names exactly those.
type specWriter struct {
	b      strings.Builder
	plan   componentPlan
	design methodcheck.UIDesignRecord
	byID   map[string]methodcheck.UIElement
	web    webEnv
	uses   map[string]bool // names the body used: runtime names, plus "expect" and "locate"
	// warnings are conditions the emitter tolerates but names (see immediateWarnings).
	warnings []string
}

func (w *specWriter) use(name string) { w.uses[name] = true }

func (w *specWriter) line(format string, args ...any) {
	fmt.Fprintf(&w.b, "  "+format+"\n", args...)
}

// playwrightSpec is one scenario's spec: the latency, the typed arrange, then per step the bound
// actions each followed by what a user sees the instant it lands (Layer A, asserted immediately),
// the expected observables, and the probes. The warnings it returns name what it tolerated.
//
// Both arms carry the 'scenario' annotation, because reporter.gen.ts keys
// results.json by it and would otherwise fall back to the display title: the
// live test pushes it first thing in the body (so a failing step keeps it),
// the skipped test declares it statically through test.skip's details
// argument (Playwright ≥1.42 TestDetails), since a skipped body never runs.
func playwrightSpec(plan componentPlan, bs boundScenario, design methodcheck.UIDesignRecord, web webEnv) (string, []string, error) {
	id := bs.Scenario.ID
	title := tsStr(id + " — " + bs.Scenario.Title)
	annotation := fmt.Sprintf("{ type: 'scenario', description: %s }", tsStr(id))
	if sk := bs.Binding.Skip; sk != nil {
		return genHeader + "\n" + fmt.Sprintf("// Source: project.json .phaseArtifacts.testPlan.%s × scenario.ForComponentAs(%s, client)\n", plan.Key, tsStr(plan.Key)) +
			"import { test } from '@playwright/test';\n\n" +
			fmt.Sprintf("// skipped: %s (until %s)\ntest.skip(%s, { annotation: %s }, async () => {});\n", sk.Reason, sk.Until, title, annotation), nil, nil
	}
	w := &specWriter{plan: plan, design: design, web: web, uses: map[string]bool{}, byID: map[string]methodcheck.UIElement{}}
	for _, e := range designElements(design) {
		w.byID[e.ID] = e
	}
	fmt.Fprintf(&w.b, "test(%s, async ({ page }, testInfo) => {\n", title)
	w.line("testInfo.annotations.push(%s);", annotation)
	w.use("installLatency")
	w.line("await installLatency(page, Number(process.env.AIARCH_UI_LATENCY_MS ?? 1500));")
	if err := w.arrange(bs); err != nil {
		return "", nil, err
	}
	for _, st := range bs.Steps {
		if err := w.step(st); err != nil {
			return "", nil, fmt.Errorf("step %d: %w", st.Bind.Seq, err)
		}
		w.warnings = append(w.warnings, immediateWarnings(plan.Key, bs.Scenario.ID, st)...)
	}
	w.b.WriteString("});\n")
	return w.file(), w.warnings, nil
}

// file is the spec: header, imports (only what the body used), body.
func (w *specWriter) file() string {
	var b strings.Builder
	b.WriteString(genHeader + "\n")
	fmt.Fprintf(&b, "// Source: project.json .phaseArtifacts.testPlan.%s × scenario.ForComponentAs(%s, client)\n", w.plan.Key, tsStr(w.plan.Key))
	playwright := []string{"test"}
	if w.uses["expect"] {
		playwright = append(playwright, "expect")
	}
	fmt.Fprintf(&b, "import { %s } from '@playwright/test';\n", strings.Join(playwright, ", "))
	if w.uses["locate"] {
		b.WriteString("import { locate } from './elements.gen';\n")
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
	b.WriteString("\n" + w.b.String())
	return b.String()
}

// arrange emits the binding's typed setup, in the plan's order, ahead of the first action. The switch is
// exhaustive over the sealed ArrangeCall and has no default, so gochecksumtype fails the build of a
// fifth kind nobody emits. A contract call goes to the component's generated REST route; failNext and
// seedResource are the scenario host's, performed through the local profile's /__scenariohost endpoint
// (tsruntime's arrange). advanceClock never reaches here: refuseAdvanceClock refused it.
func (w *specWriter) arrange(bs boundScenario) error {
	for i, a := range bs.Binding.Arrange {
		w.line("// arrange %d: %s", i+1, a.Kind())
		w.use("arrange")
		literal, err := w.arrangeLiteral(a)
		if err != nil {
			return fmt.Errorf("arrange %d: %w", i+1, err)
		}
		w.line("await arrange(testInfo, %s);", literal)
	}
	return nil
}

// arrangeLiteral is one ArrangeCall as the runtime's ArrangeCall literal.
func (w *specWriter) arrangeLiteral(a methodcheck.ArrangeCall) (string, error) {
	switch v := a.(type) {
	case methodcheck.ContractCall:
		call, err := w.contractCall(v.Component, v.Operation, v.Inputs, v.Expect)
		if err != nil {
			return "", err
		}
		return "{ kind: 'contractCall', " + call + " }", nil
	case methodcheck.AdvanceClock:
		return "", fmt.Errorf("advanceClock (%s) is refused for a web scenario (E17)", v.Duration)
	case methodcheck.FailNext:
		return "{ kind: 'failNext', component: " + tsStr(v.Component) + ", operation: " + tsStr(v.Operation) + ", errorCode: " + tsStr(v.ErrorCode) + " }", nil
	case methodcheck.SeedResource:
		var fixture bytes.Buffer
		if err := json.Compact(&fixture, v.Fixture); err != nil {
			return "", fmt.Errorf("seedResource %q fixture is not JSON: %w", v.Resource, err)
		}
		return "{ kind: 'seedResource', resource: " + tsStr(v.Resource) + ", fixture: " + fixture.String() + " }", nil
	}
	panic("unreachable: sealed ArrangeCall")
}

// contractCall spells a call on a Manager operation's generated route: `verb, path, args, expect`.
// Arguments are the literal inputs the route reads (a ref reads an earlier step's result, which a
// call made before or beside the step cannot see).
func (w *specWriter) contractCall(component, operation string, inputs []methodcheck.TestArg, want methodcheck.TestExpect) (string, error) {
	key, sc, ok := findContractIn(w.web.contracts, component)
	if !ok {
		return "", fmt.Errorf("component %q has no service contract to call", component)
	}
	op, ok := operationNamed(sc, operation)
	if !ok {
		return "", fmt.Errorf("operation %q is not on %s", operation, key)
	}
	route, ok := w.web.routes[key][op.Name]
	if !ok {
		return "", fmt.Errorf("%s.%s is no Manager's generated route, so a browser test cannot call it", component, operation)
	}
	args, refs, err := inputsJSON(sc, op, inputs)
	if err != nil {
		return "", err
	}
	if len(refs) > 0 {
		return "", fmt.Errorf("argument %q of %s.%s reads the result of step %d: a browser call is made without an earlier result", refs[0].param, component, operation, refs[0].ref.Step)
	}
	return fmt.Sprintf("verb: %s, path: %s, args: %s, expect: %s", tsStr(route.Verb), tsStr(route.Path), args, tsExpect(want)), nil
}

// tsExpect is a call's expectation as the runtime's Expectation literal.
func tsExpect(e methodcheck.TestExpect) string {
	parts := []string{}
	if e.Result != "" {
		parts = append(parts, "result: "+tsStr(e.Result))
	}
	parts = append(parts, fmt.Sprintf("errorExpected: %t", e.ErrorExpected))
	if e.ErrorCode != "" {
		parts = append(parts, "errorCode: "+tsStr(e.ErrorCode))
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func (w *specWriter) step(st boundStep) error {
	w.line("// step %d: %s (node %s)", st.Bind.Seq, st.Op.Name, st.Stim.Node)
	for _, in := range st.Bind.Inputs {
		if err := w.action(in); err != nil {
			return err
		}
	}
	if err := w.observe(st); err != nil {
		return err
	}
	for _, pr := range st.Bind.Probes {
		call, err := w.contractCall(pr.Component, pr.Operation, pr.Inputs, pr.Expect)
		if err != nil {
			return fmt.Errorf("probe for %s: %w", pr.For, err)
		}
		w.use("probe")
		w.line("// probe for %s: %s.%s", pr.For, pr.Component, pr.Operation)
		w.line("await probe(testInfo, { %s });", call)
	}
	return nil
}

// action emits one bound input as a page action followed by Layer A: what the user sees the instant
// the action lands, asserted under the immediate timeout, with the server's answers held back by the
// latency the spec installed. An input that is not the web operation's action parameter stays visible
// as a comment; an action outside the dialect is a generation error, never a silent comment.
func (w *specWriter) action(in methodcheck.TestArg) error {
	lit, err := literalInput(in)
	if err != nil {
		return err
	}
	if in.Name != webActionParam {
		w.line("// input %s: %s", in.Name, lit)
		return nil
	}
	a, err := methodcheck.ParseUIAction(lit)
	if err != nil {
		return fmt.Errorf("input %s %s: %w", in.Name, lit, err)
	}
	if t, ok := methodcheck.ActionTarget(a); ok {
		if err := w.known(t); err != nil {
			return err
		}
	}
	if _, ok := methodcheck.ActionTarget(a); ok {
		w.use("locate")
	}
	w.line("await %s;", playwrightActionLine(a))
	for _, o := range methodcheck.LayerAObservables(a) {
		w.use("immediate")
		w.useObservable(o)
		w.line("await %s;", observableLine(o, true))
	}
	return nil
}

// observe emits the expectation's observables as assertions: one object, or an array of them; an
// expected error is a comment (a UI step observes, it does not return). A step that asserts nothing is
// a generation error: a test with no assertion proves nothing. Every observable is asserted under the
// default timeout: the only immediate assertions are Layer A's, which the action itself determines.
func (w *specWriter) observe(st boundStep) error {
	e := st.Bind.Expect
	if e.ErrorExpected {
		w.line("// expected error: %s", e.ErrorCode)
		return nil
	}
	if e.Result == "" {
		return fmt.Errorf("the step asserts nothing: a browser step expects at least one thing the user sees")
	}
	many, err := methodcheck.ParseUIObservation(e.Result)
	if err != nil {
		return fmt.Errorf("expected result %s: %w", e.Result, err)
	}
	for _, o := range many {
		if err := w.observable(o); err != nil {
			return err
		}
	}
	return nil
}

// observable emits one assertion under the default timeout.
func (w *specWriter) observable(o methodcheck.UIObservable) error {
	if t, ok := methodcheck.ObservableTarget(o); ok {
		if err := w.known(t); err != nil {
			return err
		}
	}
	w.useObservable(o)
	w.line("await %s;", observableLine(o, false))
	return nil
}

// useObservable records the names an assertion on o spells: expect, and locate when it has a target.
func (w *specWriter) useObservable(o methodcheck.UIObservable) {
	w.use("expect")
	if _, ok := methodcheck.ObservableTarget(o); ok {
		w.use("locate")
	}
}

// known refuses a target the surface's design does not have: the locator would be built from nothing.
func (w *specWriter) known(t methodcheck.UITarget) error {
	if _, ok := w.byID[t.Element]; !ok {
		return fmt.Errorf("element %q is not in the uiDesign of surface %s", t.Element, w.plan.Key)
	}
	return nil
}

// immediateWarnings names every output of the step's actor outcome the derivation marks Immediate (the
// actor sees it without waiting on a component). Nothing asserts one immediately: tying an observable to
// an output needs a typed join (an explicit field of the binding's observable that a methodcheck rule
// validates against the derived outcome), and matching the output's label, which is narrative, against
// element names is not one. Until that join exists the only immediate assertions are Layer A's, which
// the action determines, and each Immediate output is reported here instead of guessed at.
func immediateWarnings(surface, scenarioID string, st boundStep) []string {
	var out []string
	for _, o := range st.Stim.ActorOutcome.ExpectedOutputs {
		if o.Immediate {
			out = append(out, fmt.Sprintf("testgen: %s %s step %d: output %s %q is marked Immediate but no typed observable is joined to it, so nothing asserts it immediately",
				surface, scenarioID, st.Bind.Seq, o.Node, o.Label))
		}
	}
	return out
}

// playwrightActionLine spells one UIAction as a page action on the located element. check/uncheck use
// Playwright's check()/uncheck(), which fail unless the control flips on the click itself. The switch is
// exhaustive over the sealed UIAction (gochecksumtype).
func playwrightActionLine(a methodcheck.UIAction) string {
	switch v := a.(type) {
	case methodcheck.UINavigate:
		return "page.goto(" + tsStr(v.Route) + ")"
	case methodcheck.UIClick:
		return byElement(v.Target) + ".click()"
	case methodcheck.UIDblclick:
		return byElement(v.Target) + ".dblclick()"
	case methodcheck.UIFill:
		return byElement(v.Target) + ".fill(" + tsStr(v.Value) + ")"
	case methodcheck.UIPress:
		return byElement(v.Target) + ".press(" + tsStr(v.Key) + ")"
	case methodcheck.UICheck:
		return byElement(v.Target) + ".check()"
	case methodcheck.UIUncheck:
		return byElement(v.Target) + ".uncheck()"
	case methodcheck.UIHover:
		return byElement(v.Target) + ".hover()"
	case methodcheck.UISelect:
		return byElement(v.Target) + ".selectOption(" + tsStr(v.Option) + ")"
	}
	panic("unreachable: sealed UIAction")
}

// playwrightMatcher is the matcher call of an observable that asserts one state of an element and
// carries no operand besides the optional timeout.
type playwrightMatcher struct {
	call string   // ".toBeChecked", ".not.toBeChecked"
	args []string // operands, before the timeout
}

var playwrightStateMatchers = map[methodcheck.UIObservableKind]playwrightMatcher{
	methodcheck.UIObservableVisible:   {call: ".toBeVisible"},
	methodcheck.UIObservableHidden:    {call: ".toBeHidden"},
	methodcheck.UIObservableAbsent:    {call: ".toHaveCount", args: []string{"0"}},
	methodcheck.UIObservableChecked:   {call: ".toBeChecked"},
	methodcheck.UIObservableUnchecked: {call: ".not.toBeChecked"},
	methodcheck.UIObservableEnabled:   {call: ".toBeEnabled"},
	methodcheck.UIObservableDisabled:  {call: ".toBeDisabled"},
	methodcheck.UIObservableFocused:   {call: ".toBeFocused"},
}

// matcherCall spells call(args..., timeout?): the immediate timeout is the last argument.
func matcherCall(call string, immediate bool, args ...string) string {
	if immediate {
		args = append(args, "immediate")
	}
	return call + "(" + strings.Join(args, ", ") + ")"
}

// playwrightObservableLine spells one UIObservable as an assertion under the default timeout.
func playwrightObservableLine(o methodcheck.UIObservable) string { return observableLine(o, false) }

// observableLine spells one UIObservable as an assertion; immediate asserts it under the short
// timeout of what the user sees the instant an action lands. The switch is exhaustive over the sealed
// UIObservable.
func observableLine(o methodcheck.UIObservable, immediate bool) string {
	switch v := o.(type) {
	case methodcheck.UIText:
		return "expect(" + byElement(v.Target) + ")" + matcherCall(".toHaveText", immediate, tsStr(v.Text))
	case methodcheck.UIValue:
		return "expect(" + byElement(v.Target) + ")" + matcherCall(".toHaveValue", immediate, tsStr(v.Value))
	case methodcheck.UICount:
		return "expect(" + byElement(v.Target) + ")" + matcherCall(".toHaveCount", immediate, fmt.Sprint(v.N))
	case methodcheck.UIURL:
		return "expect(page)" + matcherCall(".toHaveURL", immediate, urlMatcher(v.Route))
	case methodcheck.UIVisible, methodcheck.UIHidden, methodcheck.UIAbsent, methodcheck.UIChecked,
		methodcheck.UIUnchecked, methodcheck.UIEnabled, methodcheck.UIDisabled, methodcheck.UIFocused:
		t, _ := methodcheck.ObservableTarget(o)
		m := playwrightStateMatchers[o.Kind()]
		return "expect(" + byElement(t) + ")" + matcherCall(m.call, immediate, m.args...)
	}
	panic("unreachable: sealed UIObservable")
}

// UIActionKinds is the set of browser action kinds testgen emits: the one dialect the
// contract schema enumerates (playwrightActionLine is exhaustive over it).
func UIActionKinds() []string { return methodcheck.UIActionKinds() }

// UIObservableKinds is the set of browser observable kinds testgen asserts.
func UIObservableKinds() []string { return methodcheck.UIObservableKinds() }

// byElement is the locator of a target: the element found by role and name through the surface's
// generated inventory, one instance of it when the target picks one.
func byElement(t methodcheck.UITarget) string {
	loc := "locate(page, " + tsStr(t.Element)
	if t.Nth != nil {
		loc += fmt.Sprintf(", %d", *t.Nth)
	}
	return loc + ")"
}

// literalInput is a bound input's literal JSON text. A ref input names an
// earlier step's result; this emitter does not resolve one, so it is a
// generation error, never a stub.
func literalInput(in methodcheck.TestArg) (string, error) {
	switch v := in.Value.(type) {
	case methodcheck.LiteralValue:
		return string(v.JSON), nil
	case methodcheck.RefValue:
		return "", fmt.Errorf("input %s is a ref to step %d (%s): ref arguments are not resolved by this emitter", in.Name, v.Step, v.Path)
	case methodcheck.PlanRef:
		return "", planRefError(in)
	}
	panic("unreachable: sealed TestArgValue")
}

// planRefError is the generation error for a planRef input reaching a client-plan emitter. A
// planRef is resolved (methodcheck.ResolvePlanRef) only by the acceptance emitter; a client plan
// never carries one (ATP-REF), so one arriving here is a plan the gate should have refused.
func planRefError(in methodcheck.TestArg) error {
	return fmt.Errorf("input %s is a planRef: only the acceptance plan carries one and a client plan never does (ATP-REF)", in.Name)
}

// urlMatcher is the toHaveURL argument for a `url` observable. The observable
// names a ROUTE: a root-relative path ("/project/p/activity/a") is matched
// against the page URL's path exactly, whatever origin served it and whatever
// query or fragment the SPA keeps its selection state in (?task=&rev=) — state
// the use case does not name. An observable that itself carries a query or
// fragment, or names an absolute URL, names exactly that and is matched as the
// whole URL.
func urlMatcher(url string) string {
	if !strings.HasPrefix(url, "/") || strings.ContainsAny(url, "?#") {
		return tsStr(url)
	}
	return "new RegExp(" + tsStr("^[a-z][a-z0-9+.-]*://[^/]+"+regexp.QuoteMeta(url)+"(?:[?#].*)?$") + ")"
}

// componentDirDepth is how many levels a component's Playwright directory
// sits below the uitests module root: <module>/generated/<component>/. The
// results tree lives at the module root (<module>/test-results/<component>,
// the analogue of scenariohost's moduleRoot()/test-results/<component> for
// Go), so the config's up-path is this constant — never a function of how
// Config.UITestsDir was spelled, whose `..` segments are not levels.
const componentDirDepth = 2

// playwrightResultsDir is the results tree for one component, relative to
// that component's Playwright directory: ../../test-results/<component>.
func playwrightResultsDir(component string) string {
	return strings.Repeat("../", componentDirDepth) + "test-results/" + component
}

// playwrightConfig is the generated config: video and trace always on, the
// JSON report and results.json under test-results/<component>, and ONE worker.
// Every scenario of a client runs against the one real stack its venue boots —
// one server, one state repository, one workflow namespace — and each arranges
// that shared state before it runs, so two scenarios running at once would
// arrange over each other: the scenarios of a component run one at a time, in
// file order, exactly as a Go package's TestScenario_* functions do.
//
// Two projects: the surface check runs first, and every scenario depends on
// it, so a built surface that does not match its design fails once, naming the
// element, before any scenario spends its time failing at its first locator.
func playwrightConfig(results string) string {
	return genHeader + `
import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: '.',
  workers: 1,
  fullyParallel: false,
  timeout: Number(process.env.AIARCH_SCENARIO_TIMEOUT_MS ?? 300000),
  outputDir: ` + tsStr(results) + `,
  use: { video: 'on', trace: 'on' },
  reporter: [
    ['json', { outputFile: ` + tsStr(results+"/playwright.json") + ` }],
    ['list'],
    ['./reporter.gen.ts'],
  ],
  projects: [
    { name: 'surface', testMatch: /surface\.spec\.gen\.ts$/ },
    { name: 'scenarios', testMatch: /\.spec\.gen\.ts$/, testIgnore: /surface\.spec\.gen\.ts$/, dependencies: ['surface'] },
  ],
});
`
}

// playwrightReporter maps Playwright's test results to results.json in the
// TestRun shape (design §4.2): scenario from the annotation, status folded to
// pass|fail|skip|timeout, videoRef/traceRef from the attachments. The surface
// check is not a scenario: its verdict is the run's exit status, not a row.
const playwrightReporter = genHeader + `
import * as fs from 'node:fs';
import * as path from 'node:path';
import type { FullConfig, Reporter, TestCase, TestResult } from '@playwright/test/reporter';

type ScenarioResult = {
  scenario: string;
  status: 'pass' | 'fail' | 'skip' | 'timeout';
  durationMs: number;
  stdout?: string;
  stderr?: string;
  videoRef?: string;
  traceRef?: string;
};

class ScenarioReporter implements Reporter {
  private outputDir = 'test-results';
  private readonly results = new Map<string, ScenarioResult>();

  onBegin(config: FullConfig): void {
    this.outputDir = config.projects[0]?.outputDir ?? this.outputDir;
  }

  onTestEnd(test: TestCase, result: TestResult): void {
    if (test.annotations.some((a) => a.type === 'surface')) return;
    const scenario = test.annotations.find((a) => a.type === 'scenario')?.description ?? test.title;
    const status = result.status === 'passed' ? 'pass' : result.status === 'skipped' ? 'skip' : result.status === 'timedOut' ? 'timeout' : 'fail';
    const r: ScenarioResult = { scenario, status, durationMs: result.duration };
    const stdout = result.stdout.map(String).join('');
    if (stdout) r.stdout = stdout;
    const stderr = result.stderr.map(String).join('');
    if (stderr) r.stderr = stderr;
    const video = result.attachments.find((a) => a.name === 'video')?.path;
    if (video) r.videoRef = path.relative(this.outputDir, video);
    const trace = result.attachments.find((a) => a.name === 'trace')?.path;
    if (trace) r.traceRef = path.relative(this.outputDir, trace);
    this.results.set(scenario, r);
  }

  onEnd(): void {
    fs.mkdirSync(this.outputDir, { recursive: true });
    const file = path.join(this.outputDir, 'results.json');
    // A subset run (run_checks failedOnly) reports only its own scenarios: overlay
    // them on the existing index so every other scenario's verdict survives.
    const prior: ScenarioResult[] = fs.existsSync(file) ? JSON.parse(fs.readFileSync(file, 'utf8')) : [];
    const merged = new Map(prior.map((r) => [r.scenario, r]));
    for (const r of this.results.values()) merged.set(r.scenario, r);
    const out = [...merged.values()].sort((a, b) => (a.scenario < b.scenario ? -1 : a.scenario > b.scenario ? 1 : 0));
    fs.writeFileSync(file, JSON.stringify(out, null, 2) + '\n');
  }
}

export default ScenarioReporter;
`

// tsStr spells s as a single-quoted TypeScript string literal.
func tsStr(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\n", `\n`, "\r", `\r`)
	return "'" + r.Replace(s) + "'"
}
