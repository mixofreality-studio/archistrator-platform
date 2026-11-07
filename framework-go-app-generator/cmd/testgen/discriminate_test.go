package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// discriminateArgs returns the args that discriminate the testgen fixture in a temp module.
func discriminateArgs(t *testing.T) []string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app/server\n\ngo 1.25\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join("..", "..", "testgen", "testdata", "project_tp.json")
	return []string{"-project", project, "-root", root, "-uitests", "uitests/generated", "-discriminate"}
}

// The three things a scenario's `go test -v` run can say, as the go tool prints them.
func passedRun(test string) (string, error) {
	return "=== RUN   " + test + "\n--- PASS: " + test + " (0.01s)\nPASS\nok  \texample.com/app/server/internal/manager/billing\t0.1s\n", nil
}

func failedRun(test string) (string, error) {
	return "=== RUN   " + test + "\n--- FAIL: " + test + " (0.01s)\nFAIL\nFAIL\texample.com/app/server/internal/manager/billing\t0.1s\n", errors.New("exit status 1")
}

// exitedRun is what a package whose TestMain exits before any test ran prints: a bare FAIL line and
// no `--- FAIL:` for any test.
func exitedRun() (string, error) {
	return "scenariohost: Workflows.Register registered no workflow\nFAIL\texample.com/app/server/internal/manager/billing\t0.4s\nFAIL\n", errors.New("exit status 1")
}

// recorder is a go-test runner that answers by the AIARCH_NULL_OPS it is given ("" is the real
// subject) and the test it is asked to run.
type recorder struct {
	calls  []string
	envs   [][]string
	answer func(nullOps, test string) (string, error)
}

func (r *recorder) run(dir string, env []string, args ...string) (string, error) {
	nullOps, test := "", ""
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "AIARCH_NULL_OPS="); ok {
			nullOps = v
		}
	}
	for i, a := range args {
		if a == "-run" && i+1 < len(args) {
			test = strings.TrimSuffix(strings.TrimPrefix(args[i+1], "^"), "$")
		}
	}
	r.calls = append(r.calls, nullOps+" "+strings.Join(args, " "))
	r.envs = append(r.envs, env)
	return r.answer(nullOps, test)
}

// nulled counts the recorded runs that null an operation.
func (r *recorder) nulled() int {
	n := 0
	for _, c := range r.calls {
		if !strings.HasPrefix(c, " ") {
			n++
		}
	}
	return n
}

// A scenario that passes against its real subject and still passes with its stimulus nulled is a
// survivor: it is named on stdout and the run exits non-zero.
func TestDiscriminate_SurvivorIsAnError(t *testing.T) {
	args := discriminateArgs(t)
	r := &recorder{answer: func(nullOps, test string) (string, error) {
		if nullOps == "" || nullOps == "BillingManager.RecordPayment" {
			return passedRun(test) // the real subject passes; RecordPayment nulled survives
		}
		return failedRun(test)
	}}
	var out, errOut bytes.Buffer
	err := runWith(args, &out, &errOut, r.run)
	if !errors.Is(err, errSurvivors) {
		t.Fatalf("err = %v, want errSurvivors\nout: %s\nerr: %s", err, out.String(), errOut.String())
	}
	if errors.Is(err, errBroken) {
		t.Errorf("every scenario passes against its real subject; none is broken: %v", err)
	}
	if want := "TP-DISCRIMINATE billingManager process-order-P2 RecordPayment"; !strings.Contains(out.String(), want) {
		t.Errorf("missing %q in %q", want, out.String())
	}
	if strings.Count(out.String(), "TP-DISCRIMINATE") != 1 {
		t.Errorf("only the surviving pair is named: %q", out.String())
	}
	if r.nulled() < 5 {
		t.Errorf("every (scenario, op) pair runs once nulled; got %d: %v", r.nulled(), r.calls)
	}
	for _, c := range r.calls {
		if !strings.Contains(c, "-run ^TestScenario_") || !strings.Contains(c, "./internal/") {
			t.Errorf("a run is not one scenario in one package: %q", c)
		}
	}
}

// When the real subject passes every scenario and every nulled run fails, the scenarios
// discriminate: green.
func TestDiscriminate_AllFailIsGreen(t *testing.T) {
	args := discriminateArgs(t)
	r := &recorder{answer: func(nullOps, test string) (string, error) {
		if nullOps == "" {
			return passedRun(test)
		}
		return failedRun(test)
	}}
	var out, errOut bytes.Buffer
	if err := runWith(args, &out, &errOut, r.run); err != nil {
		t.Fatalf("the real subject passes and every nulled run fails, so the scenarios discriminate: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "TP-DISCRIMINATE") || strings.Contains(out.String(), "TP-BROKEN") {
		t.Errorf("no survivors and no broken scenarios expected: %q", out.String())
	}
}

// The null run of a scenario means something only when the scenario passes against its real
// subject. A scenario that fails there fails under the null subject too, and counting that as a
// kill is what let a vacuous binding through: it is TP-BROKEN, its null runs are never made, and
// it is not green.
func TestDiscriminate_RequiresRealPassBeforeNull(t *testing.T) {
	args := discriminateArgs(t)
	r := &recorder{answer: func(nullOps, test string) (string, error) {
		if nullOps != "" {
			return passedRun(test) // would be a survivor, if it were ever asked
		}
		if test == "TestScenario_process_order_P2" {
			return failedRun(test) // broken against the real subject
		}
		return passedRun(test)
	}}
	var out, errOut bytes.Buffer
	err := runWith(args, &out, &errOut, r.run)
	if !errors.Is(err, errBroken) {
		t.Fatalf("err = %v, want errBroken\nout: %s", err, out.String())
	}
	if want := "TP-BROKEN billingManager process-order-P2"; !strings.Contains(out.String(), want) {
		t.Errorf("missing %q in %q", want, out.String())
	}
	if strings.Contains(out.String(), "TP-DISCRIMINATE billingManager process-order-P2") {
		t.Errorf("a scenario that fails against its real subject was nulled: %q", out.String())
	}
	for _, c := range r.calls {
		if !strings.HasPrefix(c, " ") && strings.Contains(c, "-run ^TestScenario_process_order_P2$") {
			t.Errorf("a broken scenario was run nulled: %q", c)
		}
	}
}

// The real-subject run must not inherit a null list from the environment: it sets the variable empty.
func TestDiscriminate_RealRunClearsNullOps(t *testing.T) {
	t.Setenv("AIARCH_NULL_OPS", "BillingManager.RecordPayment")
	args := discriminateArgs(t)
	r := &recorder{answer: func(nullOps, test string) (string, error) { return passedRun(test) }}
	var out, errOut bytes.Buffer
	_ = runWith(args, &out, &errOut, r.run)
	real := 0
	for i, c := range r.calls {
		if !strings.HasPrefix(c, " ") {
			continue
		}
		real++
		if !slices.Contains(r.envs[i], "AIARCH_NULL_OPS=") {
			t.Errorf("the real-subject run does not clear AIARCH_NULL_OPS: %q", r.envs[i])
		}
	}
	if real == 0 {
		t.Fatal("no real-subject run was made")
	}
}

// A run that did not execute the scenario proves nothing either way: a build failure, a skip, or a
// test binary that exited before the test reported (a TestMain exit prints a bare FAIL line) is
// reported as the tool's error, never counted as a kill or a survivor. It is checked in the
// real-subject run and in a nulled run.
func TestDiscriminate_InconclusiveRunIsAnError(t *testing.T) {
	for name, tc := range map[string]struct {
		out string
		err error
	}{
		"build failure": {"FAIL\texample.com/app/server/internal/manager/billing [build failed]\n", errors.New("exit status 1")},
		"skip":          {"=== RUN   TestScenario_x\n--- SKIP: TestScenario_x (0.00s)\nPASS\n", nil},
		"no tests":      {"testing: warning: no tests to run\nPASS\n", nil},
		"testmain exit": {"scenariohost: Workflows.Register registered no workflow\nFAIL\texample.com/app/server/internal/manager/billing\t0.4s\nFAIL\n", errors.New("exit status 1")},
	} {
		for _, when := range []string{"real", "nulled"} {
			t.Run(name+"/"+when, func(t *testing.T) {
				args := discriminateArgs(t)
				r := &recorder{answer: func(nullOps, test string) (string, error) {
					if (nullOps == "") == (when == "real") {
						return tc.out, tc.err
					}
					return passedRun(test)
				}}
				var out, errOut bytes.Buffer
				err := runWith(args, &out, &errOut, r.run)
				if err == nil || errors.Is(err, errSurvivors) || errors.Is(err, errBroken) {
					t.Fatalf("err = %v, want an inconclusive-run error", err)
				}
				if when == "real" && strings.Contains(out.String(), "TP-DISCRIMINATE") {
					t.Errorf("an inconclusive run named a survivor: %q", out.String())
				}
			})
		}
	}
}

// A TestMain that exits non-zero for every run (the package cannot start) must not look like every
// scenario being killed: the run is an error, not green.
func TestDiscriminate_PackageThatCannotStartIsNotAKill(t *testing.T) {
	args := discriminateArgs(t)
	r := &recorder{answer: func(string, string) (string, error) { return exitedRun() }}
	var out, errOut bytes.Buffer
	if err := runWith(args, &out, &errOut, r.run); err == nil {
		t.Fatalf("every run exited before reporting a test, yet the discrimination is green:\n%s", out.String())
	}
}

// `go test` prints `? pkg [no test files]` and exits 0 for a package that holds no test file: the
// scenario was not run. That is a run that proved nothing, not a pass (a pass would let a package
// whose generated tests were never emitted look like a scenario the real subject satisfies, and a
// nulled run would then count it a survivor for no reason).
func TestDiscriminate_NoTestFilesIsInconclusive(t *testing.T) {
	const noTests = "?   \texample.com/app/server/internal/manager/billing\t[no test files]\n"
	if _, ok := classify(noTests, nil, "TestScenario_x").(ranInconclusive); !ok {
		t.Fatalf("classify(no test files, exit 0) = %T, want ranInconclusive", classify(noTests, nil, "TestScenario_x"))
	}
	for _, when := range []string{"real", "nulled"} {
		t.Run(when, func(t *testing.T) {
			args := discriminateArgs(t)
			r := &recorder{answer: func(nullOps, test string) (string, error) {
				if (nullOps == "") == (when == "real") {
					return noTests, nil
				}
				return passedRun(test)
			}}
			var out, errOut bytes.Buffer
			err := runWith(args, &out, &errOut, r.run)
			if err == nil || errors.Is(err, errSurvivors) || errors.Is(err, errBroken) {
				t.Fatalf("err = %v, want an inconclusive-run error", err)
			}
			if !strings.Contains(err.Error(), "no test files") {
				t.Errorf("the error does not say the package has no test files: %v", err)
			}
		})
	}
}

// The web half is skipped with a printed notice until Plan 3 boots the server.
func TestDiscriminate_WebHalfIsSkippedWithNotice(t *testing.T) {
	args := discriminateArgs(t)
	r := &recorder{answer: func(nullOps, test string) (string, error) {
		if nullOps == "" {
			return passedRun(test)
		}
		return failedRun(test)
	}}
	var out, errOut bytes.Buffer
	if err := runWith(args, &out, &errOut, r.run); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "shopClient") || !strings.Contains(out.String(), "skipped") {
		t.Errorf("the skipped web scenarios are not announced: %q", out.String())
	}
}
