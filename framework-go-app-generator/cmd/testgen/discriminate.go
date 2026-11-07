package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"

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

// goTest runs the test of d once, with the operations nullOps nulled ("" runs the real subject: the
// variable is set empty so an inherited value cannot leak into the run), and classifies it.
func goTest(o options, d testgen.Discrimination, nullOps string, run goTestRunner) (runOutcome, string) {
	output, runErr := run(o.root, []string{"GOWORK=off", "AIARCH_NULL_OPS=" + nullOps},
		"test", "-count=1", "-v", "-run", "^"+d.Test+"$", "./"+d.Package+"/")
	return classify(output, runErr, d.Test), output
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
		if n := len(out); n > 0 && out[n-1].test.Package == d.Package && out[n-1].test.Test == d.Test {
			out[n-1].runs = append(out[n-1].runs, d)
			continue
		}
		out = append(out, scenarioRuns{test: d, runs: []testgen.Discrimination{d}})
	}
	return out
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

// discriminate runs every Go scenario once against its real subject and, when that passes, once per
// stimulus operation with that operation nulled (AIARCH_NULL_OPS). It prints
// `TP-DISCRIMINATE <component> <scenario> <op>` for each pair that still passes, and
// `TP-BROKEN <component> <scenario>` for each scenario that does not pass against the real subject
// (its null runs would prove nothing). Scenarios the run cannot execute yet (web and machine
// clients, which need the booted server) are announced, not silently dropped.
func discriminate(o options, gen testgen.Output, out io.Writer, run goTestRunner) error {
	if err := announceSkipped(out, gen.Skipped); err != nil {
		return err
	}
	var t tally
	for _, sc := range groupByScenario(gen.Discriminate) {
		if err := discriminateScenario(o, sc, out, run, &t); err != nil {
			return err
		}
	}
	if err := say(out, fmt.Sprintf("discriminate: %d scenario(s), %d null run(s), %d survivor(s), %d broken", t.scenarios, t.runs, t.survivors, t.broken)); err != nil {
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
