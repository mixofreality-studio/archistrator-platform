package testgen

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// emitPlaywright emits one client component's Playwright tests under
// <UITestsDir>/<component>/: a spec per binding, the generated config and
// results reporter, and the once-emitted hooks.ts. The emitter is a function
// over the projected scenario + binding JSON only; nothing of the Go emitter
// is needed here.
func emitPlaywright(out *Output, cfg Config, plan componentPlan) error {
	dir := path.Join(cfg.UITestsDir, plan.Key)
	for _, bs := range plan.Scenarios {
		spec, err := playwrightSpec(plan, bs)
		if err != nil {
			return fmt.Errorf("testgen: %s %s: %w", plan.Key, bs.Scenario.ID, err)
		}
		out.Generated[path.Join(dir, bs.Scenario.ID+".spec.gen.ts")] = []byte(spec)
	}
	out.Generated[path.Join(dir, "playwright.config.gen.ts")] = []byte(playwrightConfig(playwrightResultsDir(plan.Key)))
	out.Generated[path.Join(dir, "reporter.gen.ts")] = []byte(playwrightReporter)
	out.HooksOnce[path.Join(dir, "hooks.ts")] = []byte(playwrightHooks(plan))
	return nil
}

// playwrightSpec is one scenario's spec: the hooks' before, then per step the
// bound actions, the step hook when hooked, and the expected observables.
//
// Both arms carry the 'scenario' annotation, because reporter.gen.ts keys
// results.json by it and would otherwise fall back to the display title: the
// live test pushes it first thing in the body (so a failing step keeps it),
// the skipped test declares it statically through test.skip's details
// argument (Playwright ≥1.42 TestDetails), since a skipped body never runs.
func playwrightSpec(plan componentPlan, bs boundScenario) (string, error) {
	id := bs.Scenario.ID
	var b strings.Builder
	b.WriteString(genHeader + "\n")
	fmt.Fprintf(&b, "// Source: project.json .phaseArtifacts.testPlan.%s × scenario.ForComponent(%s)\n", plan.Key, tsStr(plan.Key))
	b.WriteString("import { test, expect } from '@playwright/test';\nimport { hooks } from './hooks';\n\n")
	title := tsStr(id + " — " + bs.Scenario.Title)
	annotation := fmt.Sprintf("{ type: 'scenario', description: %s }", tsStr(id))
	if sk := bs.Binding.Skip; sk != nil {
		fmt.Fprintf(&b, "// skipped: %s (until %s)\ntest.skip(%s, { annotation: %s }, async () => {});\n", sk.Reason, sk.Until, title, annotation)
		return b.String(), nil
	}
	fmt.Fprintf(&b, "test(%s, async ({ page }, testInfo) => {\n", title)
	fmt.Fprintf(&b, "  testInfo.annotations.push(%s);\n", annotation)
	fmt.Fprintf(&b, "  await hooks.before(%s, page);\n", tsStr(id))
	for _, st := range bs.Steps {
		if err := writePlaywrightStep(&b, st); err != nil {
			return "", fmt.Errorf("step %d: %w", st.Bind.Seq, err)
		}
	}
	b.WriteString("});\n")
	return b.String(), nil
}

func writePlaywrightStep(b *strings.Builder, st boundStep) error {
	seq := st.Bind.Seq
	fmt.Fprintf(b, "  // step %d: %s (node %s)\n", seq, st.Op.Name, st.Stim.Node)
	for _, in := range st.Bind.Inputs {
		line, err := playwrightAction(in)
		if err != nil {
			return err
		}
		b.WriteString(line)
	}
	expect, err := playwrightExpect(st.Bind.Expect)
	if err != nil {
		return err
	}
	b.WriteString(expect)
	for _, pr := range st.Bind.Probes {
		fmt.Fprintf(b, "  // probe for %s: %s.%s %s\n", pr.For, pr.Component, pr.Operation, compactJSON(pr))
	}
	return nil
}

// playwrightActionLine spells one UIAction as a page action. check/uncheck use
// Playwright's check()/uncheck(), which fail unless the control flips on the click
// itself. The switch is exhaustive over the sealed UIAction (gochecksumtype).
//
// Until the element-inventory locator of the Playwright emitter v2 lands, the design
// element id is the locator's test id.
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

// playwrightStateMatchers is the matcher of each observable that asserts one state of an
// element and carries no operand.
var playwrightStateMatchers = map[methodcheck.UIObservableKind]string{
	methodcheck.UIObservableVisible:   ".toBeVisible()",
	methodcheck.UIObservableHidden:    ".toBeHidden()",
	methodcheck.UIObservableAbsent:    ".toHaveCount(0)",
	methodcheck.UIObservableChecked:   ".toBeChecked()",
	methodcheck.UIObservableUnchecked: ".not.toBeChecked()",
	methodcheck.UIObservableEnabled:   ".toBeEnabled()",
	methodcheck.UIObservableDisabled:  ".toBeDisabled()",
	methodcheck.UIObservableFocused:   ".toBeFocused()",
}

// playwrightObservableLine spells one UIObservable as an assertion; the switch is
// exhaustive over the sealed UIObservable.
func playwrightObservableLine(o methodcheck.UIObservable) string {
	switch v := o.(type) {
	case methodcheck.UIText:
		return "expect(" + byElement(v.Target) + ").toHaveText(" + tsStr(v.Text) + ")"
	case methodcheck.UIValue:
		return "expect(" + byElement(v.Target) + ").toHaveValue(" + tsStr(v.Value) + ")"
	case methodcheck.UICount:
		return "expect(" + byElement(v.Target) + ").toHaveCount(" + fmt.Sprint(v.N) + ")"
	case methodcheck.UIURL:
		return "expect(page).toHaveURL(" + urlMatcher(v.Route) + ")"
	case methodcheck.UIVisible, methodcheck.UIHidden, methodcheck.UIAbsent, methodcheck.UIChecked,
		methodcheck.UIUnchecked, methodcheck.UIEnabled, methodcheck.UIDisabled, methodcheck.UIFocused:
		t, _ := methodcheck.ObservableTarget(o)
		return "expect(" + byElement(t) + ")" + playwrightStateMatchers[o.Kind()]
	}
	panic("unreachable: sealed UIObservable")
}

// UIActionKinds is the set of browser action kinds testgen emits: the one dialect the
// contract schema enumerates (playwrightActionLine is exhaustive over it).
func UIActionKinds() []string { return methodcheck.UIActionKinds() }

// UIObservableKinds is the set of browser observable kinds testgen asserts.
func UIObservableKinds() []string { return methodcheck.UIObservableKinds() }

func byElement(t methodcheck.UITarget) string {
	loc := "page.getByTestId(" + tsStr(t.Element) + ")"
	if t.Nth != nil {
		loc += fmt.Sprintf(".nth(%d)", *t.Nth)
	}
	return loc
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
	}
	panic("unreachable: sealed TestArgValue")
}

// playwrightAction spells one bound input as a page action. An input that is not the
// web operation's action parameter stays visible as a comment; an action outside the
// dialect is a generation error, never a silent comment.
func playwrightAction(in methodcheck.TestArg) (string, error) {
	lit, err := literalInput(in)
	if err != nil {
		return "", err
	}
	if in.Name != webActionParam {
		return fmt.Sprintf("  // input %s: %s\n", in.Name, lit), nil
	}
	a, err := methodcheck.ParseUIAction(lit)
	if err != nil {
		return "", fmt.Errorf("input %s %s: %w", in.Name, lit, err)
	}
	return "  await " + playwrightActionLine(a) + ";\n", nil
}

// playwrightExpect spells the expectation's observables as assertions: one
// object, or an array of them; an expected error is a comment (a UI step
// observes, it does not return). An observation outside the dialect is a
// generation error.
func playwrightExpect(e methodcheck.TestExpect) (string, error) {
	if e.ErrorExpected {
		return fmt.Sprintf("  // expected error: %s\n", e.ErrorCode), nil
	}
	if e.Result == "" {
		return "", nil
	}
	many, err := methodcheck.ParseUIObservation(e.Result)
	if err != nil {
		return "", fmt.Errorf("expected result %s: %w", e.Result, err)
	}
	var b strings.Builder
	for _, o := range many {
		b.WriteString("  await " + playwrightObservableLine(o) + ";\n")
	}
	return b.String(), nil
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
// one server, one state repository, one workflow namespace — and its hooks
// arrange that shared state before each scenario, so two scenarios running at
// once would arrange over each other: the scenarios of a component run one at a
// time, in file order, exactly as a Go package's TestScenario_* functions do.
func playwrightConfig(results string) string {
	return genHeader + `
import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: '.',
  testMatch: /\.spec\.gen\.ts$/,
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
});
`
}

// playwrightReporter maps Playwright's test results to results.json in the
// TestRun shape (design §4.2): scenario from the annotation, status folded to
// pass|fail|skip|timeout, videoRef/traceRef from the attachments.
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

// playwrightHooks is the once-emitted hooks.ts: before/step stubs that throw
// FILL, with each hooked step's binding inline for the agent.
func playwrightHooks(plan componentPlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "// Hooks for %s scenario tests. Generated ONCE by testgen; owned by the construction agent.\n", plan.Key)
	b.WriteString(`import type { Page } from '@playwright/test';

export const hooks = {
  // FILL: sign in and seed whatever state scenario id starts from.
  async before(id: string, page: Page): Promise<void> {
    void page;
    throw new Error('FILL hooks.before ' + id);
  },
};
`)
	return b.String()
}

// tsStr spells s as a single-quoted TypeScript string literal.
func tsStr(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\n", `\n`, "\r", `\r`)
	return "'" + r.Replace(s) + "'"
}
