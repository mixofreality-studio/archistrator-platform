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

// A web scenario whose operation reaches no single generated Manager operation has nothing to null:
// it is announced as skipped with that reason, never dropped silently.
func TestDiscriminate_WebOpWithNoManagerRouteIsSkippedWithNotice(t *testing.T) {
	args := seatWeb(t, discriminateArgs(t))
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
	if !strings.Contains(out.String(), "shopClient") || !strings.Contains(out.String(), "skipped") || !strings.Contains(out.String(), "nothing to null") {
		t.Errorf("the skipped web scenarios are not announced: %q", out.String())
	}
	for _, c := range r.calls {
		if strings.Contains(c, "webscenarios") {
			t.Errorf("a web scenario with nothing to null was run: %s", c)
		}
	}
}

// webArgs discriminates the testgen fixture with its web client's operation renamed to the Manager
// operation it drives (GetOrder), so the web scenario has an operation to null.
func webArgs(t *testing.T) []string { return seatWeb(t, unseatedWebArgs(t)) }

// unseatedWebArgs is webArgs in a module whose web project is not seated.
func unseatedWebArgs(t *testing.T) []string {
	t.Helper()
	args := discriminateArgs(t)
	raw, err := os.ReadFile(args[1])
	if err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(t.TempDir(), "project.json")
	if err := os.WriteFile(project, []byte(strings.ReplaceAll(string(raw), "OpenOrders", "GetOrder")), 0o600); err != nil {
		t.Fatal(err)
	}
	args[1] = project
	return args
}

// seatWeb seats the web project of the module the args run in (webgen bootstrap writes
// webApp/package.json), so its web scenarios are discriminated.
func seatWeb(t *testing.T, args []string) []string {
	t.Helper()
	root := args[slices.Index(args, "-root")+1]
	if err := os.MkdirAll(filepath.Join(root, "webApp"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "webApp", "package.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return args
}

// Until the web client's integration seats the web project, `go tool webscenarios` cannot load a
// browser (the launcher serves no SPA either), so web scenarios are not run: each is recorded as
// skipped with the reason, and the summary line names how many, so the skip is visible. The Go
// scenarios are unaffected, and the run is green.
func TestDiscriminate_WebSkippedUntilSeated(t *testing.T) {
	var webRuns int
	run := withWeb(func([]string) (string, error) {
		webRuns++
		return "  1 failed\n", errors.New("exit status 1")
	})
	var out, errOut bytes.Buffer
	if err := runWith(unseatedWebArgs(t), &out, &errOut, run); err != nil {
		t.Fatalf("an unseated web project must not fail the run: %v\n%s", err, out.String())
	}
	if webRuns != 0 {
		t.Errorf("%d web run(s) were made with no seated web project", webRuns)
	}
	for _, want := range []string{
		"discriminate: skipped shopClient browse-orders-P1: web project not yet seated",
		"1 web skipped (web project not yet seated)",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "TP-BROKEN") {
		t.Errorf("unexpected output:\n%s", out.String())
	}
}

// Once the web project is seated web discriminations are mandatory again: the web scenario runs
// through the scenario host and nothing is reported skipped for want of the project.
func TestDiscriminate_WebRunsWhenSeated(t *testing.T) {
	var webRuns int
	run := withWeb(func(env []string) (string, error) {
		webRuns++
		if nullOpsOf(env) == "" {
			return "  1 passed (1s)\n", nil
		}
		return "  1 failed\n", errors.New("exit status 1")
	})
	var out, errOut bytes.Buffer
	if err := runWith(seatWeb(t, webArgs(t)), &out, &errOut, run); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if webRuns < 2 {
		t.Errorf("a seated web project must be discriminated, got %d web run(s)", webRuns)
	}
	if strings.Contains(out.String(), "not yet seated") {
		t.Errorf("nothing is unseated, yet the run says so:\n%s", out.String())
	}
}

// withWeb is a runner that answers the Go scenarios as the real subject passes and every null run
// kills (the recorder's rule), and a `go tool webscenarios` run with web, given the run's environment.
func withWeb(web func(env []string) (string, error)) goTestRunner {
	goRuns := &recorder{answer: func(nullOps, test string) (string, error) {
		if nullOps == "" {
			return passedRun(test)
		}
		return failedRun(test)
	}}
	return func(dir string, env []string, args ...string) (string, error) {
		if len(args) >= 2 && args[0] == "tool" && args[1] == "webscenarios" {
			return web(env)
		}
		return goRuns.run(dir, env, args...)
	}
}

// nullOpsOf is the AIARCH_NULL_OPS a run's environment carries ("" when unset or empty).
func nullOpsOf(env []string) string {
	v := ""
	for _, e := range env {
		if s, ok := strings.CutPrefix(e, "AIARCH_NULL_OPS="); ok {
			v = s
		}
	}
	return v
}

// webRun is one run of the stub: the arguments and environment of a command.
type webRun struct {
	args []string
	env  []string
}

// Web scenarios run through the scenario host: once against the real subject, then once per stimulus
// operation with that Manager operation nulled in the launched server, each as
// `go tool webscenarios -C <uitests> -config generated/<contract>/playwright.config.gen.ts -grep <id>`
// with AIARCH_NULL_OPS in the environment the launched app inherits. A pass with the operation nulled
// is a survivor, as it is for Go.
func TestDiscriminate_WebRunsThroughWebscenarios(t *testing.T) {
	var runs []webRun
	run := withWeb(func(env []string) (string, error) {
		runs = append(runs, webRun{env: env})
		if nullOpsOf(env) == "" {
			return "  1 passed (2.1s)\n", nil
		}
		return "  1 failed\n", errors.New("exit status 1")
	})
	capture := func(dir string, env []string, args ...string) (string, error) {
		if len(args) >= 2 && args[0] == "tool" {
			out, err := run(dir, env, args...)
			runs[len(runs)-1].args = args
			return out, err
		}
		return run(dir, env, args...)
	}
	var out, errOut bytes.Buffer
	if err := runWith(webArgs(t), &out, &errOut, capture); err != nil {
		t.Fatalf("every null run failed, so nothing survived: %v\n%s", err, out.String())
	}
	if len(runs) < 2 {
		t.Fatalf("want a real-subject run and a null run of the web scenario, got %d runs", len(runs))
	}
	first := runs[0]
	joined := strings.Join(first.args, " ")
	for _, want := range []string{"tool webscenarios -C uitests -config generated/shopClient/playwright.config.gen.ts -grep ", "browse-orders-P1", " — "} {
		if !strings.Contains(joined, want) {
			t.Errorf("the run is %q, want it to contain %q", joined, want)
		}
	}
	var nulled bool
	for _, r := range runs[1:] {
		for _, e := range r.env {
			if e == "AIARCH_NULL_OPS=OrderManager.GetOrder" {
				nulled = true
			}
		}
	}
	if !nulled {
		t.Errorf("no run nulled OrderManager.GetOrder: %+v", runs)
	}
	if strings.Contains(out.String(), "TP-DISCRIMINATE shopClient") {
		t.Errorf("a failing null run is a kill, not a survivor:\n%s", out.String())
	}
}

// A web scenario that still passes with its operation nulled is a survivor, and one that fails against
// the real subject is broken, exactly as for Go; a run that did not execute the scenario is the tool's
// error.
func TestDiscriminate_WebVerdicts(t *testing.T) {
	cases := map[string]struct {
		real, null string
		realErr    error
		want       string
		wantErr    error
	}{
		"survivor": {real: "  1 passed (1s)\n", null: "  1 passed (1s)\n", want: "TP-DISCRIMINATE shopClient browse-orders-P1", wantErr: errSurvivors},
		"broken":   {real: "  1 failed\n", realErr: errors.New("exit status 1"), want: "TP-BROKEN shopClient browse-orders-P1", wantErr: errBroken},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			run := withWeb(func(env []string) (string, error) {
				if nullOpsOf(env) == "" {
					return c.real, c.realErr
				}
				return c.null, nil
			})
			var out, errOut bytes.Buffer
			err := runWith(webArgs(t), &out, &errOut, run)
			if !errors.Is(err, c.wantErr) || !strings.Contains(out.String(), c.want) {
				t.Errorf("err = %v, out = %q; want %v and %q", err, out.String(), c.wantErr, c.want)
			}
		})
	}
	for name, output := range map[string]string{
		"nothing matched": "Error: No tests found\n",
		"skipped":         "  1 skipped\n",
	} {
		t.Run(name, func(t *testing.T) {
			run := withWeb(func([]string) (string, error) { return output, nil })
			var out, errOut bytes.Buffer
			err := runWith(webArgs(t), &out, &errOut, run)
			if err == nil || errors.Is(err, errSurvivors) || errors.Is(err, errBroken) {
				t.Errorf("err = %v, want an inconclusive-run error", err)
			}
		})
	}
}

// A launch failure of the scenario host is retried like a Go scenario's, and is never the scenario's
// failure.
func TestDiscriminate_WebLaunchFlakeIsRetried(t *testing.T) {
	attempts := 0
	run := withWeb(func(env []string) (string, error) {
		attempts++
		if attempts == 1 {
			return "webscenarios: launch: temporal: dev server did not start\n", errors.New("exit status 1")
		}
		if nullOpsOf(env) == "" {
			return "  1 passed (1s)\n", nil
		}
		return "  1 failed\n", errors.New("exit status 1")
	})
	var out, errOut bytes.Buffer
	if err := runWith(webArgs(t), &out, &errOut, run); err != nil {
		t.Fatalf("a launch flake must be retried, got %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "TP-BROKEN") {
		t.Errorf("a launch failure is not the scenario's failure:\n%s", out.String())
	}
}

// bootFailedRun is what a scenario prints when the host it launches does not come up: the test fails
// on the host's own error, before any step ran.
func bootFailedRun(test string) (string, error) {
	return "=== RUN   " + test + "\n    manager_scenarios.gen_test.go:30: scenariohost: temporal: testinfra.StartDevServer: failed connecting after timeout\n--- FAIL: " + test + " (60.10s)\nFAIL\nFAIL\texample.com/app/server/internal/manager/billing\t60.5s\n", errors.New("exit status 1")
}

// A host that fails to boot once is not the scenario's failure: the run is made again, and the
// scenario is judged on the run that executed it.
func TestDiscriminate_HostBootFlakeIsRetried(t *testing.T) {
	args := discriminateArgs(t)
	flaked := map[string]bool{}
	r := &recorder{answer: func(nullOps, test string) (string, error) {
		key := nullOps + "/" + test
		if !flaked[key] {
			flaked[key] = true
			return bootFailedRun(test) // every distinct run flakes once
		}
		if nullOps == "" {
			return passedRun(test)
		}
		return failedRun(test)
	}}
	var out, errOut bytes.Buffer
	if err := runWith(args, &out, &errOut, r.run); err != nil {
		t.Fatalf("each run's first boot flaked and its retry judged the scenario: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "TP-BROKEN") || strings.Contains(out.String(), "TP-DISCRIMINATE") {
		t.Errorf("a boot flake is neither broken nor a survivor: %q", out.String())
	}
}

// A host that never boots is the tool's error, not a verdict: not TP-BROKEN, not a kill.
func TestDiscriminate_HostThatNeverBootsIsInconclusive(t *testing.T) {
	args := discriminateArgs(t)
	r := &recorder{answer: func(_, test string) (string, error) { return bootFailedRun(test) }}
	var out, errOut bytes.Buffer
	err := runWith(args, &out, &errOut, r.run)
	if err == nil || errors.Is(err, errBroken) || errors.Is(err, errSurvivors) || !strings.Contains(err.Error(), "did not boot") {
		t.Fatalf("err = %v, want the tool's own error naming the host that did not boot", err)
	}
	if strings.Contains(out.String(), "TP-BROKEN") {
		t.Errorf("a host that never boots is not a broken scenario: %q", out.String())
	}
	if len(r.calls) != hostBootAttempts {
		t.Errorf("%d runs, want %d attempts at the first scenario", len(r.calls), hostBootAttempts)
	}
}
