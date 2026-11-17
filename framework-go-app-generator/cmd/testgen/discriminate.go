package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/devstack"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/testgen"
)

// errSurvivors is the -discriminate verdict when a scenario still passed with its stimulus nulled;
// main maps it to exit 1.
var errSurvivors = errors.New("testgen: scenarios survived the null subject (TP-DISCRIMINATE): they would pass without the behavior they claim to test")

// errBroken is the -discriminate verdict when a scenario does not pass against its real subject: the
// null runs of it would prove nothing (a test that fails everywhere "fails under the null subject"
// too), so it is reported (TP-BROKEN) and not nulled; main maps it to exit 1.
var errBroken = errors.New("testgen: scenarios fail against their real subject (TP-BROKEN): they cannot be discriminated until they pass")

// goTestRunner runs `go <args>` in dir with env added to the process environment and returns its
// combined output. A non-nil error with output is a failing run; it is a function so the verdict
// logic is tested without compiling a module.
type goTestRunner func(dir string, env []string, args ...string) (string, error)

// execGoTest is the real runner.
func execGoTest(dir string, env []string, args ...string) (string, error) {
	cmd := exec.Command("go", args...) // #nosec G204 -- the go tool with arguments built from the plan
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	raw, err := cmd.CombinedOutput()
	return string(raw), err
}

// runOutcome is what one `go test` run of a scenario says about it.
//
//sumtype:decl
type runOutcome interface{ isRunOutcome() }

// ranPassed: the scenario's test executed and passed.
type ranPassed struct{}

// ranFailed: the scenario's test executed and reported its own failure (`--- FAIL: <Test>`).
type ranFailed struct{}

// ranInconclusive: the scenario did not run to a verdict (build failure, skip, no such test, or a
// test binary that died before the test reported: a TestMain exit, a crash).
type ranInconclusive struct{ why string }

func (ranPassed) isRunOutcome()       {}
func (ranFailed) isRunOutcome()       {}
func (ranInconclusive) isRunOutcome() {}

// classify reads one run of test. Only a run that executed the scenario counts: a package that does
// not build, a test that skipped (for instance under -short, or with no substrate), a package with
// no test files (`[no test files]`, exit 0), a -run that matched nothing and a test binary that exited before reporting the test prove nothing either way.
// In particular a bare `FAIL <package>` is not a failed scenario: it is what a TestMain that exits
// non-zero prints before any test ran, so a failure needs the test's own `--- FAIL:` line.
func classify(output string, err error, test string) runOutcome {
	switch {
	case strings.Contains(output, "[build failed]"), strings.Contains(output, "[setup failed]"):
		return ranInconclusive{"the package does not build"}
	case strings.Contains(output, "no tests to run"):
		return ranInconclusive{"no test matched"}
	case strings.Contains(output, "[no test files]"):
		return ranInconclusive{"the package has no test files (go test exits 0 without running anything)"}
	case strings.Contains(output, "--- SKIP"):
		return ranInconclusive{"the scenario skipped itself"}
	case err == nil:
		return ranPassed{}
	case testFailed(output, test):
		return ranFailed{}
	}
	return ranInconclusive{fmt.Sprintf("go test ended before %s reported a result (a TestMain exit or a crash): %v", test, err)}
}

// testFailed reports whether output holds test's own top-level failure line.
func testFailed(output, test string) bool {
	return regexp.MustCompile(`(?m)^--- FAIL: ` + regexp.QuoteMeta(test) + `( |$)`).MatchString(output)
}

// Playwright's list reporter ends a run with one summary line per outcome ("  1 passed (2.1s)").
var (
	pwPassed = regexp.MustCompile(`(?m)^\s+\d+ passed\b`)
	pwFailed = regexp.MustCompile(`(?m)^\s+\d+ (failed|flaky|timed out|interrupted)\b`)
)

// webLaunchFailure is the line `go tool webscenarios` fails with when the scenario host or the app it
// launches does not come up.
var webLaunchFailure = regexp.MustCompile(`(?m)^webscenarios: launch:`)

// classifyWeb reads one `go tool webscenarios` run of a web scenario. As for Go, only a run that
// executed the scenario counts: a launch that failed is reported as a failure so the host-boot retry
// sees it (and a host that never boots is inconclusive); a -grep that matched nothing, or a scenario
// that skipped itself, proves nothing either way.
func classifyWeb(output string, err error) runOutcome {
	switch {
	case webLaunchFailure.MatchString(output):
		return ranFailed{}
	case strings.Contains(output, "No tests found"):
		return ranInconclusive{"no test matched"}
	case err == nil && pwPassed.MatchString(output):
		return ranPassed{}
	case err == nil:
		return ranInconclusive{"the scenario skipped itself or reported nothing"}
	case pwFailed.MatchString(output):
		return ranFailed{}
	}
	return ranInconclusive{fmt.Sprintf("webscenarios ended before the scenario reported a result: %v", err)}
}

// hostBootAttempts is how many times a scenario run is made when the scenario host (the Temporal dev
// server, the database, the repository, the launched devstack) does not come up.
const hostBootAttempts = 3

// hostBootFailure matches the line a generated scenario fails with when the host it runs against did
// not boot (`scenariohost: temporal: ...`, `scenariohost: LaunchDevstack: ...`): the dev server losing
// a port or a SQLite lock to another local process, the launched app missing its readiness window.
// That says nothing about the scenario, and counting it as the scenario's failure makes a clean
// scenario TP-BROKEN, or a stimulus whose null run never executed look killed.
var hostBootFailure = regexp.MustCompile(`(?m)^\s*\S+\.go:\d+: scenariohost: (temporal|postgres|git|mkdir|LaunchDevstack):|^webscenarios: launch:`)

// scenarioArgs is the `go` command that runs d's scenario: its package's test, or, for a web scenario,
// the Playwright project through the scenario host.
func scenarioArgs(d testgen.Discrimination) []string {
	if d.Web() {
		return []string{"tool", "webscenarios", "-C", d.UIDir, "-config", d.UIConfig, "-grep", d.UIGrep}
	}
	return []string{"test", "-count=1", "-v", "-run", "^" + d.Test + "$", "./" + d.Package + "/"}
}

// classifyRun reads one run of d.
func classifyRun(d testgen.Discrimination, output string, err error) runOutcome {
	if d.Web() {
		return classifyWeb(output, err)
	}
	return classify(output, err, d.Test)
}

// goTest runs the scenario of d (a Go test, or a web scenario through the scenario host), with the
// operations nullOps nulled ("" runs the real subject: the variable is set empty so an inherited value
// cannot leak into the run, nor into the server a web run launches), and classifies it. A run whose
// host did not boot is made again, up to hostBootAttempts times in all; a host that never boots is
// inconclusive, never a verdict on the scenario.
func goTest(o options, d testgen.Discrimination, nullOps string, run goTestRunner) (runOutcome, string) {
	var output string
	for range hostBootAttempts {
		var runErr error
		output, runErr = run(o.root, []string{"GOWORK=off", "AIARCH_NULL_OPS=" + nullOps}, scenarioArgs(d)...)
		out := classifyRun(d, output, runErr)
		if _, failed := out.(ranFailed); !failed || !hostBootFailure.MatchString(output) {
			return out, output
		}
	}
	return ranInconclusive{fmt.Sprintf("the scenario host did not boot in %d attempts", hostBootAttempts)}, output
}

// inconclusive is the tool's error for a run that did not execute the scenario.
func inconclusive(d testgen.Discrimination, nullOps, why, output string) error {
	subject := "the real subject"
	if nullOps != "" {
		subject = nullOps + " nulled"
	}
	return fmt.Errorf("testgen: discriminate %s %s with %s: %s\n%s", d.Component, d.Scenario, subject, why, output)
}

// scenarioRuns is one scenario's test and its null runs, one per stimulus operation.
type scenarioRuns struct {
	test testgen.Discrimination // the first run: names the component, scenario, package and test
	runs []testgen.Discrimination
}

// groupByScenario splits the run list (sorted by package, test, operation) into its scenarios.
func groupByScenario(runs []testgen.Discrimination) []scenarioRuns {
	var out []scenarioRuns
	for _, d := range runs {
		if n := len(out); n > 0 && out[n-1].test.Package == d.Package && out[n-1].test.UIConfig == d.UIConfig && out[n-1].test.Test == d.Test {
			out[n-1].runs = append(out[n-1].runs, d)
			continue
		}
		out = append(out, scenarioRuns{test: d, runs: []testgen.Discrimination{d}})
	}
	return out
}

// batchEntry is one line of the run list a package's generated TestDiscriminate reads from the file
// AIARCH_DISCRIMINATE names; the test's subtest of the same ID carries the entry's verdict.
type batchEntry struct {
	ID      string `json:"id"`
	Test    string `json:"test"`
	NullOps string `json:"nullOps"`
}

// entryID is the subtest name of d's run with nullOps nulled ("" is the real subject).
func entryID(d testgen.Discrimination, nullOps string) string { return d.Test + "#" + nullOps }

// testEvent is the part of a `go test -json` event the batch reads.
type testEvent struct {
	Action string
	Test   string
	Output string
}

// batchTimeout is the `go test -timeout` of a batch of n entries: the default ten minutes plus a
// minute an entry, since the scenario host's own watchdog ends a scenario that hangs.
func batchTimeout(n int) string { return fmt.Sprintf("%dm", 10+n) }

// entryRun is what one process says about one entry: the outcome and the entry's own output.
type entryRun struct {
	outcome runOutcome
	output  string
}

// parseBatch reads a batch's `go test -json` stream. entries maps a subtest name (its entry id under
// TestDiscriminate) to its events; text is every line of output, the stream's own and any non-JSON
// line the go tool printed.
func parseBatch(raw string) (events map[string][]testEvent, text string) {
	events = map[string][]testEvent{}
	var b strings.Builder
	for line := range strings.SplitSeq(raw, "\n") {
		var ev testEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			b.WriteString(line + "\n")
			continue
		}
		b.WriteString(ev.Output)
		if id, ok := strings.CutPrefix(ev.Test, testgen.DiscriminateTest+"/"); ok {
			id, _, _ = strings.Cut(id, "/") // a scenario's own subtests belong to its entry
			events[id] = append(events[id], ev)
		}
	}
	return events, b.String()
}

// classifyEntry reads one entry's events. As for a single run, only an entry that executed the scenario
// counts: a skip (the scenario's own, or an unknown test) and an entry that never reported (the process
// ended first: a build failure, a TestMain exit, a crash) prove nothing either way.
func classifyEntry(id string, evs []testEvent, text string, err error) entryRun {
	verdict, output := entryVerdict(id, evs)
	switch verdict {
	case "skip":
		return entryRun{ranInconclusive{"the scenario skipped itself (or no such test in the package)"}, output}
	case "pass":
		return entryRun{ranPassed{}, output}
	case "fail":
		return entryRun{ranFailed{}, output}
	}
	if out, ok := classify(text, err, testgen.DiscriminateTest).(ranInconclusive); ok {
		return entryRun{out, text}
	}
	return entryRun{ranInconclusive{fmt.Sprintf("go test ended before %s reported a result (a TestMain exit or a crash): %v", id, err)}, text}
}

// entryVerdict is the terminal action of the entry's own subtest ("pass", "fail", "skip", or "" when it
// never reported) and the entry's output. A skip of any subtest the scenario ran inside the entry is a
// skip of the entry.
func entryVerdict(id string, evs []testEvent) (verdict, output string) {
	var b strings.Builder
	for _, ev := range evs {
		b.WriteString(ev.Output)
		switch {
		case ev.Test != testgen.DiscriminateTest+"/"+id:
			if ev.Action == "skip" {
				verdict = "skip"
			}
		case ev.Action == "pass" || ev.Action == "fail" || ev.Action == "skip":
			if verdict != "skip" {
				verdict = ev.Action
			}
		}
	}
	return verdict, b.String()
}

// outputTail is how much of a process's output an inconclusive entry keeps for the error message.
const outputTail = 16 << 10

// runBatchOnce runs entries of pkg in one `go test` process over the generated TestDiscriminate.
func runBatchOnce(o options, pkg string, entries []testgen.Discrimination, run goTestRunner) (map[string]entryRun, string, error) {
	list := make([]batchEntry, 0, len(entries))
	for _, d := range entries {
		list = append(list, batchEntry{ID: entryID(d, d.NullOp), Test: d.Test, NullOps: d.NullOp})
	}
	raw, err := json.Marshal(list)
	if err != nil {
		return nil, "", err
	}
	f, err := os.CreateTemp("", "testgen-discriminate-*.json")
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.Write(raw); err != nil {
		_ = f.Close()
		return nil, "", err
	}
	if err := f.Close(); err != nil {
		return nil, "", err
	}
	// AIARCH_NULL_OPS is set empty so a value inherited from the environment cannot leak into the
	// process: each subtest sets its own.
	out, runErr := run(o.root, []string{"GOWORK=off", "AIARCH_NULL_OPS=", testgen.DiscriminateEnv + "=" + f.Name()},
		"test", "-count=1", "-json", "-timeout", batchTimeout(len(entries)), "-run", "^"+testgen.DiscriminateTest+"$", "./"+pkg+"/")
	events, text := parseBatch(out)
	res := map[string]entryRun{}
	for _, d := range entries {
		id := entryID(d, d.NullOp)
		res[id] = classifyEntry(id, events[id], text, runErr)
	}
	return res, text, nil
}

// runBatch runs entries (every one a Go run of pkg; NullOp "" is the real subject) as subtests of the
// package's generated TestDiscriminate in one `go test -count=1 -json -run '^TestDiscriminate$'
// ./<pkg>/` process, and classifies each from its own subtest's events (pass: ranPassed; its own
// failure: ranFailed; a skip, or no event: ranInconclusive). Entries whose output says the scenario host
// did not boot are run again, only those, in a smaller batch, up to hostBootAttempts processes in all.
// The map is keyed by entry id (entryID); the string is the tail of the last process's output.
func runBatch(o options, pkg string, entries []testgen.Discrimination, run goTestRunner) (map[string]runOutcome, string) {
	outcomes := map[string]runOutcome{}
	pending := entries
	tail := ""
	for attempt := 0; attempt < hostBootAttempts && len(pending) > 0; attempt++ {
		res, text, err := runBatchOnce(o, pkg, pending, run)
		if err != nil {
			res = map[string]entryRun{}
			for _, d := range pending {
				res[entryID(d, d.NullOp)] = entryRun{outcome: ranInconclusive{err.Error()}}
			}
		}
		tail = text[max(0, len(text)-outputTail):]
		pending = settle(pending, res, outcomes)
	}
	for _, d := range pending {
		outcomes[entryID(d, d.NullOp)] = ranInconclusive{fmt.Sprintf("the scenario host did not boot in %d attempts", hostBootAttempts)}
	}
	return outcomes, tail
}

// settle records into outcomes the entries of pending that ran and returns those whose host did not boot.
func settle(pending []testgen.Discrimination, res map[string]entryRun, outcomes map[string]runOutcome) []testgen.Discrimination {
	var again []testgen.Discrimination
	for _, d := range pending {
		id := entryID(d, d.NullOp)
		r := res[id]
		if _, failed := r.outcome.(ranFailed); failed && hostBootFailure.MatchString(r.output) {
			again = append(again, d)
			continue
		}
		outcomes[id] = r.outcome
	}
	return again
}

// announceSkipped names every runnable scenario the run cannot execute yet.
func announceSkipped(out io.Writer, skipped []testgen.DiscriminationSkip) error {
	for _, s := range skipped {
		if err := say(out, fmt.Sprintf("discriminate: skipped %s %s: %s", s.Component, s.Scenario, s.Reason)); err != nil {
			return err
		}
	}
	return nil
}

// tally counts what a -discriminate run found.
type tally struct{ scenarios, runs, survivors, broken int }

// baseline runs sc against its real subject and reports whether it passes there. A scenario that
// fails is TP-BROKEN; a run that proved nothing is the tool's error.
func baseline(o options, sc scenarioRuns, out io.Writer, run goTestRunner) (passes bool, err error) {
	outcome, output := goTest(o, sc.test, "", run)
	switch v := outcome.(type) {
	case ranPassed:
		return true, nil
	case ranFailed:
		return false, say(out, fmt.Sprintf("TP-BROKEN %s %s", sc.test.Component, sc.test.Scenario))
	case ranInconclusive:
		return false, inconclusive(sc.test, "", v.why, output)
	}
	return false, nil
}

// survives runs d with its operation nulled and reports whether the scenario still passes. A run
// that did not execute the scenario is an error.
func survives(o options, d testgen.Discrimination, run goTestRunner) (bool, error) {
	outcome, output := goTest(o, d, d.NullOp, run)
	switch v := outcome.(type) {
	case ranPassed:
		return true, nil
	case ranFailed:
		return false, nil
	case ranInconclusive:
		return false, inconclusive(d, d.NullOp, v.why, output)
	}
	return false, nil
}

// discriminateScenario judges one scenario: the real subject must pass it, then each stimulus
// operation is nulled in turn and a pass is a survivor.
func discriminateScenario(o options, sc scenarioRuns, out io.Writer, run goTestRunner, t *tally) error {
	t.scenarios++
	passes, err := baseline(o, sc, out, run)
	if err != nil {
		return err
	}
	if !passes {
		t.broken++
		return nil
	}
	for _, d := range sc.runs {
		t.runs++
		survived, err := survives(o, d, run)
		if err != nil {
			return err
		}
		if !survived {
			continue
		}
		t.survivors++
		if err := say(out, fmt.Sprintf("TP-DISCRIMINATE %s %s %s", d.Component, d.Scenario, d.Op)); err != nil {
			return err
		}
	}
	return nil
}

// packageRuns is one Go package's runs.
type packageRuns struct {
	name string
	runs []testgen.Discrimination
}

// batchesOf splits the Go runs (sorted by package) into one batch per package.
func batchesOf(runs []testgen.Discrimination) []packageRuns {
	var out []packageRuns
	for _, d := range runs {
		if n := len(out); n > 0 && out[n-1].name == d.Batch {
			out[n-1].runs = append(out[n-1].runs, d)
			continue
		}
		out = append(out, packageRuns{name: d.Batch, runs: []testgen.Discrimination{d}})
	}
	return out
}

// discriminatePackage judges one package's scenarios in two processes: the baselines of all of them,
// then the nulled runs of those that passed (D13). A scenario that fails against its real subject is
// TP-BROKEN and none of its null runs is made; a run that proved nothing is the tool's error.
func discriminatePackage(o options, pkg string, scenarios []scenarioRuns, out io.Writer, run goTestRunner, t *tally) error {
	baselines := make([]testgen.Discrimination, 0, len(scenarios))
	for _, sc := range scenarios {
		d := sc.test
		d.NullOp = ""
		baselines = append(baselines, d)
	}
	base, output := runBatch(o, pkg, baselines, run)
	var mutants []testgen.Discrimination
	for _, sc := range scenarios {
		t.scenarios++
		passes, err := baselineVerdict(sc, base[entryID(sc.test, "")], output, out, t)
		if err != nil {
			return err
		}
		if passes {
			mutants = append(mutants, sc.runs...)
		}
	}
	if len(mutants) == 0 {
		return nil
	}
	nulled, output := runBatch(o, pkg, mutants, run)
	for _, d := range mutants {
		t.runs++
		if err := mutantVerdict(d, nulled[entryID(d, d.NullOp)], output, out, t); err != nil {
			return err
		}
	}
	return nil
}

// baselineVerdict reports whether sc passed against its real subject; one that failed is TP-BROKEN.
func baselineVerdict(sc scenarioRuns, outcome runOutcome, output string, out io.Writer, t *tally) (bool, error) {
	switch v := outcome.(type) {
	case ranPassed:
		return true, nil
	case ranFailed:
		t.broken++
		return false, say(out, fmt.Sprintf("TP-BROKEN %s %s", sc.test.Component, sc.test.Scenario))
	case ranInconclusive:
		return false, inconclusive(sc.test, "", v.why, output)
	}
	return false, nil
}

// mutantVerdict judges one nulled run: a pass is a survivor.
func mutantVerdict(d testgen.Discrimination, outcome runOutcome, output string, out io.Writer, t *tally) error {
	switch v := outcome.(type) {
	case ranPassed:
		t.survivors++
		return say(out, fmt.Sprintf("TP-DISCRIMINATE %s %s %s", d.Component, d.Scenario, d.Op))
	case ranFailed:
		return nil // killed
	case ranInconclusive:
		return inconclusive(d, d.NullOp, v.why, output)
	}
	return nil
}

// runAll judges every run: a web scenario's runs one process each through the scenario host, a Go
// package's runs batched (discriminatePackage).
func runAll(o options, runs []testgen.Discrimination, out io.Writer, run goTestRunner, t *tally) error {
	var goRuns []testgen.Discrimination
	for _, sc := range groupByScenario(runs) {
		if sc.test.Batch != "" {
			goRuns = append(goRuns, sc.runs...)
			continue
		}
		if err := discriminateScenario(o, sc, out, run, t); err != nil {
			return err
		}
	}
	for _, pkg := range batchesOf(goRuns) {
		if err := discriminatePackage(o, pkg.name, groupByScenario(pkg.runs), out, run, t); err != nil {
			return err
		}
	}
	return nil
}

// discriminate runs every scenario (Go, and web through the scenario host) once against its real
// subject and, when that passes, once per stimulus operation with that operation nulled
// (AIARCH_NULL_OPS). It prints
// `TP-DISCRIMINATE <component> <scenario> <op>` for each pair that still passes, and
// `TP-BROKEN <component> <scenario>` for each scenario that does not pass against the real subject
// (its null runs would prove nothing). Scenarios the run cannot execute, because a step reaches no
// generated Manager operation to null, are announced, not silently dropped. Web scenarios run only once
// the web project is seated (devstack.WebSeated); until then each is announced as skipped and the
// summary line counts them.
func discriminate(o options, gen testgen.Output, out io.Writer, run goTestRunner) error {
	// Web scenarios are discriminated only once the web project is seated: until then no browser can
	// load the app (the launcher serves no SPA), so each is a recorded skip, counted in the summary.
	webSkipped := 0
	if !devstack.WebSeated(o.root) {
		webSkipped = gen.DeferWeb()
	}
	if err := announceSkipped(out, gen.Skipped); err != nil {
		return err
	}
	var t tally
	if err := runAll(o, gen.Discriminate, out, run, &t); err != nil {
		return err
	}
	summary := fmt.Sprintf("discriminate: %d scenario(s), %d null run(s), %d survivor(s), %d broken", t.scenarios, t.runs, t.survivors, t.broken)
	if webSkipped > 0 {
		summary += fmt.Sprintf(", %d web skipped (%s)", webSkipped, testgen.ReasonWebUnseated)
	}
	if err := say(out, summary); err != nil {
		return err
	}
	var verdicts []error
	if t.survivors > 0 {
		verdicts = append(verdicts, errSurvivors)
	}
	if t.broken > 0 {
		verdicts = append(verdicts, errBroken)
	}
	return errors.Join(verdicts...)
}
