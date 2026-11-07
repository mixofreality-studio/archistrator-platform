package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/testgen"
)

// errSurvivors is the -discriminate verdict when a scenario still passed with its stimulus nulled;
// main maps it to exit 1.
var errSurvivors = errors.New("testgen: scenarios survived the null subject (TP-DISCRIMINATE): they would pass without the behavior they claim to test")

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

// verdict is what one null run says about its scenario.
type verdict int

const (
	killed       verdict = iota // the scenario failed: it depends on the nulled operation
	survived                    // the scenario passed: it does not
	inconclusive                // the scenario did not run (build failure, skip, no such test)
)

// judge reads one nulled run. Only a run that executed the scenario counts: a package that does not
// build, a test that skipped (for instance under -short, or with no substrate) and a -run that
// matched nothing prove nothing either way.
func judge(output string, err error) (verdict, string) {
	switch {
	case strings.Contains(output, "[build failed]"), strings.Contains(output, "[setup failed]"):
		return inconclusive, "the package does not build"
	case strings.Contains(output, "no tests to run"):
		return inconclusive, "no test matched"
	case strings.Contains(output, "--- SKIP"):
		return inconclusive, "the scenario skipped itself"
	case err == nil:
		return survived, ""
	case strings.Contains(output, "FAIL"):
		return killed, ""
	}
	return inconclusive, fmt.Sprintf("go test did not run: %v", err)
}

// nullRun executes one pair and reports whether the scenario survived; a run that did not execute
// the scenario is an error.
func nullRun(o options, d testgen.Discrimination, run goTestRunner) (bool, error) {
	output, runErr := run(o.root, []string{"GOWORK=off", "AIARCH_NULL_OPS=" + d.NullOp},
		"test", "-count=1", "-v", "-run", "^"+d.Test+"$", "./"+d.Package+"/")
	switch v, why := judge(output, runErr); v {
	case inconclusive:
		return false, fmt.Errorf("testgen: discriminate %s %s with %s nulled: %s\n%s", d.Component, d.Scenario, d.NullOp, why, output)
	case survived:
		return true, nil
	case killed:
	}
	return false, nil
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

// discriminate runs every Go (scenario, stimulus operation) pair of gen with that operation nulled
// (AIARCH_NULL_OPS) and prints `TP-DISCRIMINATE <component> <scenario> <op>` for each that still
// passes. Scenarios the run cannot execute yet (web and machine clients, which need the booted server)
// are announced, not silently dropped.
func discriminate(o options, gen testgen.Output, out io.Writer, run goTestRunner) error {
	if err := announceSkipped(out, gen.Skipped); err != nil {
		return err
	}
	survivors := 0
	for _, d := range gen.Discriminate {
		survived, err := nullRun(o, d, run)
		if err != nil {
			return err
		}
		if !survived {
			continue
		}
		survivors++
		if err := say(out, fmt.Sprintf("TP-DISCRIMINATE %s %s %s", d.Component, d.Scenario, d.Op)); err != nil {
			return err
		}
	}
	if err := say(out, fmt.Sprintf("discriminate: %d run(s), %d survivor(s)", len(gen.Discriminate), survivors)); err != nil {
		return err
	}
	if survivors > 0 {
		return errSurvivors
	}
	return nil
}
