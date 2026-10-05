package testgen

import (
	"encoding/json"
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
func emitPlaywright(out *Output, cfg Config, plan componentPlan) {
	dir := path.Join(cfg.UITestsDir, plan.Key)
	for _, bs := range plan.Scenarios {
		out.Generated[path.Join(dir, bs.Scenario.ID+".spec.gen.ts")] = []byte(playwrightSpec(plan, bs))
	}
	out.Generated[path.Join(dir, "playwright.config.gen.ts")] = []byte(playwrightConfig(playwrightResultsDir(plan.Key)))
	out.Generated[path.Join(dir, "reporter.gen.ts")] = []byte(playwrightReporter)
	out.HooksOnce[path.Join(dir, "hooks.ts")] = []byte(playwrightHooks(plan))
}

// playwrightSpec is one scenario's spec: the hooks' before, then per step the
// bound actions, the step hook when hooked, and the expected observables.
//
// Both arms carry the 'scenario' annotation, because reporter.gen.ts keys
// results.json by it and would otherwise fall back to the display title: the
// live test pushes it first thing in the body (so a failing step keeps it),
// the skipped test declares it statically through test.skip's details
// argument (Playwright ≥1.42 TestDetails), since a skipped body never runs.
func playwrightSpec(plan componentPlan, bs boundScenario) string {
	id := bs.Scenario.ID
	var b strings.Builder
	b.WriteString(genHeader + "\n")
	fmt.Fprintf(&b, "// Source: project.json .phaseArtifacts.testPlan.%s × scenario.ForComponent(%s)\n", plan.Key, tsStr(plan.Key))
	b.WriteString("import { test, expect } from '@playwright/test';\nimport { hooks } from './hooks';\n\n")
	title := tsStr(id + " — " + bs.Scenario.Title)
	annotation := fmt.Sprintf("{ type: 'scenario', description: %s }", tsStr(id))
	if sk := bs.Binding.Skip; sk != nil {
		fmt.Fprintf(&b, "// skipped: %s (until %s)\ntest.skip(%s, { annotation: %s }, async () => {});\n", sk.Reason, sk.Until, title, annotation)
		return b.String()
	}
	fmt.Fprintf(&b, "test(%s, async ({ page }, testInfo) => {\n", title)
	fmt.Fprintf(&b, "  testInfo.annotations.push(%s);\n", annotation)
	fmt.Fprintf(&b, "  await hooks.before(%s, page);\n", tsStr(id))
	for _, st := range bs.Steps {
		writePlaywrightStep(&b, id, st)
	}
	b.WriteString("});\n")
	return b.String()
}

func writePlaywrightStep(b *strings.Builder, id string, st boundStep) {
	seq := st.Bind.Seq
	fmt.Fprintf(b, "  // step %d: %s (node %s)\n", seq, st.Op.Name, st.Stim.Node)
	for _, in := range st.Bind.Inputs {
		b.WriteString(playwrightAction(in))
	}
	if st.Bind.Hook {
		fmt.Fprintf(b, "  await hooks.step(%s, %d, page);\n", tsStr(id), seq)
	}
	b.WriteString(playwrightExpect(st.Bind.Expect))
	for _, pr := range st.Bind.Probes {
		fmt.Fprintf(b, "  // probe for %s: %s.%s %s\n", pr.For, pr.Component, pr.Operation, compactJSON(pr))
	}
	if len(st.Bind.Unobservable) > 0 {
		fmt.Fprintf(b, "  // unobservable: %s\n", strings.Join(st.Bind.Unobservable, ", "))
	}
}

// uiAction is a UI binding input: {Name:"action", Value: {kind, target, value}}.
type uiAction struct {
	Kind   string `json:"kind"`
	Target string `json:"target"`
	Value  string `json:"value"`
}

// playwrightAction spells one bound input as a page action by data-testid;
// an input that is not a recognised action stays visible as a comment.
func playwrightAction(in methodcheck.TestArg) string {
	var a uiAction
	if in.Name != webActionParam || json.Unmarshal([]byte(in.Value), &a) != nil {
		return fmt.Sprintf("  // input %s: %s\n", in.Name, in.Value)
	}
	switch a.Kind {
	case "click":
		return fmt.Sprintf("  await page.getByTestId(%s).click();\n", tsStr(a.Target))
	case "fill":
		return fmt.Sprintf("  await page.getByTestId(%s).fill(%s);\n", tsStr(a.Target), tsStr(a.Value))
	case "navigate":
		return fmt.Sprintf("  await page.goto(%s);\n", tsStr(a.Value))
	default:
		return fmt.Sprintf("  // unknown action kind %q: %s\n", a.Kind, in.Value)
	}
}

// uiObservable is a UI binding expectation: one of visible / text (+target) / url.
type uiObservable struct {
	Visible string `json:"visible"`
	Text    string `json:"text"`
	Target  string `json:"target"`
	URL     string `json:"url"`
}

// playwrightExpect spells the expectation's observables as assertions: one
// object, or an array of them; an expected error is a comment (a UI step
// observes, it does not return).
func playwrightExpect(e methodcheck.TestExpect) string {
	if e.ErrorExpected {
		return fmt.Sprintf("  // expected error: %s\n", e.ErrorCode)
	}
	if e.Result == "" {
		return ""
	}
	var many []uiObservable
	if err := json.Unmarshal([]byte(e.Result), &many); err != nil {
		var one uiObservable
		if json.Unmarshal([]byte(e.Result), &one) != nil {
			return fmt.Sprintf("  // expected result: %s\n", e.Result)
		}
		many = []uiObservable{one}
	}
	var b strings.Builder
	for _, o := range many {
		b.WriteString(playwrightAssertion(o, e.Result))
	}
	return b.String()
}

func playwrightAssertion(o uiObservable, raw string) string {
	switch {
	case o.Visible != "":
		return fmt.Sprintf("  await expect(page.getByTestId(%s)).toBeVisible();\n", tsStr(o.Visible))
	case o.Text != "" && o.Target != "":
		return fmt.Sprintf("  await expect(page.getByTestId(%s)).toHaveText(%s);\n", tsStr(o.Target), tsStr(o.Text))
	case o.URL != "":
		return fmt.Sprintf("  await expect(page).toHaveURL(%s);\n", urlMatcher(o.URL))
	default:
		return fmt.Sprintf("  // expected result: %s\n", raw)
	}
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
// JSON report and results.json under test-results/<component>.
func playwrightConfig(results string) string {
	return genHeader + `
import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: '.',
  testMatch: /\.spec\.gen\.ts$/,
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
// pass|fail|skip, videoRef/traceRef from the attachments.
const playwrightReporter = genHeader + `
import * as fs from 'node:fs';
import * as path from 'node:path';
import type { FullConfig, Reporter, TestCase, TestResult } from '@playwright/test/reporter';

type ScenarioResult = {
  scenario: string;
  status: 'pass' | 'fail' | 'skip';
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
    const status = result.status === 'passed' ? 'pass' : result.status === 'skipped' ? 'skip' : 'fail';
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
    const out = [...this.results.values()].sort((a, b) => (a.scenario < b.scenario ? -1 : a.scenario > b.scenario ? 1 : 0));
    fs.mkdirSync(this.outputDir, { recursive: true });
    fs.writeFileSync(path.join(this.outputDir, 'results.json'), JSON.stringify(out, null, 2) + '\n');
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

// Hooked steps (scenario / seq → binding JSON):
`)
	for _, bs := range plan.Scenarios {
		for _, st := range bs.Steps {
			if st.Bind.Hook {
				fmt.Fprintf(&b, "//   %s / %d: %s\n", bs.Scenario.ID, st.Bind.Seq, compactJSON(st.Bind))
			}
		}
	}
	b.WriteString(`
export const hooks = {
  // FILL: sign in and seed whatever state scenario id starts from.
  async before(id: string, page: Page): Promise<void> {
    void page;
    throw new Error('FILL hooks.before ' + id);
  },
  // FILL: perform step seq of scenario id (its binding is listed above) and assert its observables.
  async step(id: string, seq: number, page: Page): Promise<void> {
    void page;
    throw new Error('FILL hooks.step ' + id + '/' + seq);
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
