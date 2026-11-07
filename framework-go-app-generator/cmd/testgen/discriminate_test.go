package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
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

// recorder is a go-test runner that answers by the AIARCH_NULL_OPS it is given.
type recorder struct {
	calls  []string
	answer func(nullOps string) (string, error)
}

func (r *recorder) run(dir string, env []string, args ...string) (string, error) {
	nullOps := ""
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "AIARCH_NULL_OPS="); ok {
			nullOps = v
		}
	}
	r.calls = append(r.calls, nullOps+" "+strings.Join(args, " "))
	return r.answer(nullOps)
}

// A scenario that still passes with its stimulus nulled is a survivor: it is named on stdout and the
// run exits non-zero.
func TestDiscriminate_SurvivorIsAnError(t *testing.T) {
	args := discriminateArgs(t)
	r := &recorder{answer: func(nullOps string) (string, error) {
		if nullOps == "BillingManager.RecordPayment" {
			return "ok  \texample.com/app/server/internal/manager/billing\t0.1s\n", nil // survives
		}
		return "--- FAIL: TestScenario\nFAIL\n", errors.New("exit status 1")
	}}
	var out, errOut bytes.Buffer
	err := runWith(args, &out, &errOut, r.run)
	if !errors.Is(err, errSurvivors) {
		t.Fatalf("err = %v, want errSurvivors\nout: %s\nerr: %s", err, out.String(), errOut.String())
	}
	if want := "TP-DISCRIMINATE billingManager process-order-P2 RecordPayment"; !strings.Contains(out.String(), want) {
		t.Errorf("missing %q in %q", want, out.String())
	}
	if strings.Count(out.String(), "TP-DISCRIMINATE") != 1 {
		t.Errorf("only the surviving pair is named: %q", out.String())
	}
	if len(r.calls) < 5 {
		t.Errorf("every (scenario, op) pair runs once; got %d: %v", len(r.calls), r.calls)
	}
	for _, c := range r.calls {
		if !strings.Contains(c, "-run ^TestScenario_") || !strings.Contains(c, "./internal/") {
			t.Errorf("a run is not one scenario in one package: %q", c)
		}
	}
}

// When every nulled run fails, the scenarios discriminate: green.
func TestDiscriminate_AllFailIsGreen(t *testing.T) {
	args := discriminateArgs(t)
	r := &recorder{answer: func(string) (string, error) {
		return "--- FAIL: TestScenario_x\nFAIL\nFAIL\texample.com/app/server/internal/manager/billing\t0.1s\n", errors.New("exit status 1")
	}}
	var out, errOut bytes.Buffer
	if err := runWith(args, &out, &errOut, r.run); err != nil {
		t.Fatalf("all runs failed, so the scenarios discriminate: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "TP-DISCRIMINATE") {
		t.Errorf("no survivors expected: %q", out.String())
	}
}

// A run that did not execute the scenario proves nothing either way: a build failure or a skip is
// reported as the tool's error, never counted as a kill or a survivor.
func TestDiscriminate_InconclusiveRunIsAnError(t *testing.T) {
	for name, tc := range map[string]struct {
		out string
		err error
	}{
		"build failure": {"FAIL\texample.com/app/server/internal/manager/billing [build failed]\n", errors.New("exit status 1")},
		"skip":          {"=== RUN   TestScenario_x\n--- SKIP: TestScenario_x (0.00s)\nPASS\n", nil},
		"no tests":      {"testing: warning: no tests to run\nPASS\n", nil},
	} {
		t.Run(name, func(t *testing.T) {
			args := discriminateArgs(t)
			r := &recorder{answer: func(string) (string, error) { return tc.out, tc.err }}
			var out, errOut bytes.Buffer
			err := runWith(args, &out, &errOut, r.run)
			if err == nil || errors.Is(err, errSurvivors) {
				t.Fatalf("err = %v, want an inconclusive-run error", err)
			}
			if strings.Contains(out.String(), "TP-DISCRIMINATE") {
				t.Errorf("an inconclusive run named a survivor: %q", out.String())
			}
		})
	}
}

// The web half is skipped with a printed notice until Plan 3 boots the server.
func TestDiscriminate_WebHalfIsSkippedWithNotice(t *testing.T) {
	args := discriminateArgs(t)
	r := &recorder{answer: func(string) (string, error) { return "FAIL\n", errors.New("exit status 1") }}
	var out, errOut bytes.Buffer
	if err := runWith(args, &out, &errOut, r.run); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "shopClient") || !strings.Contains(out.String(), "skipped") {
		t.Errorf("the skipped web scenarios are not announced: %q", out.String())
	}
}
