package scenariohost

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// readIndex reads component's results.json under root.
func readIndex(t *testing.T, root, component string) []ScenarioResult {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, component, resultsFile))
	if err != nil {
		t.Fatal(err)
	}
	var got []ScenarioResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("%s/results.json is not a ScenarioResult array: %v\n%s", component, err, raw)
	}
	return got
}

func TestResultsJSONIsSortedAndComplete(t *testing.T) {
	h := &Host{ResultsRoot: t.TempDir()}
	h.Record("billingManager", ScenarioResult{Scenario: "uc-P2", Status: "pass", DurationMs: 3})
	h.Record("billingManager", ScenarioResult{Scenario: "uc-P1", Status: "fail", Stderr: "boom"})
	if err := h.Flush(); err != nil {
		t.Fatal(err)
	}
	got := readIndex(t, h.ResultsRoot, "billingManager")
	if len(got) != 2 || got[0].Scenario != "uc-P1" || got[1].Status != "pass" {
		t.Fatalf("%+v", got)
	}
	if got[0].Stderr != "boom" {
		t.Fatalf("stderr not carried: %+v", got[0])
	}
}

// TestFacetsInOnePackageDoNotOverwriteEachOther is the projectstate false
// green (DCT Plan 4 report): several facets share one Go package, so one test
// process records the SAME scenario id for different components. Each verdict
// lands in its own component's index; neither overwrites the other.
func TestFacetsInOnePackageDoNotOverwriteEachOther(t *testing.T) {
	h := &Host{ResultsRoot: t.TempDir()}
	h.Record("activityExecutionAccess", ScenarioResult{Scenario: "uc3-P1", Status: StatusFail})
	h.Record("gitActivityStatusAccess", ScenarioResult{Scenario: "uc3-P1", Status: StatusPass})
	h.Record("activityExecutionAccess", ScenarioResult{Scenario: "uc3-P2", Status: StatusPass})
	if err := h.Flush(); err != nil {
		t.Fatal(err)
	}
	aea := readIndex(t, h.ResultsRoot, "activityExecutionAccess")
	if len(aea) != 2 || aea[0].Scenario != "uc3-P1" || aea[0].Status != StatusFail || aea[1].Scenario != "uc3-P2" {
		t.Fatalf("activityExecutionAccess index: %+v", aea)
	}
	git := readIndex(t, h.ResultsRoot, "gitActivityStatusAccess")
	if len(git) != 1 || git[0].Scenario != "uc3-P1" || git[0].Status != StatusPass {
		t.Fatalf("gitActivityStatusAccess index: %+v", git)
	}
}

func TestFlushLeavesTheIndexOfADeclaredComponentThatRanNothing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nested", "dir")
	prior := filepath.Join(root, "designSessionAccess", resultsFile)
	if err := os.MkdirAll(filepath.Dir(prior), 0o750); err != nil {
		t.Fatal(err)
	}
	const recorded = `[{"scenario":"uc3-P1","status":"pass","durationMs":1}]`
	if err := os.WriteFile(prior, []byte(recorded), 0o600); err != nil {
		t.Fatal(err)
	}
	h := &Host{ResultsRoot: root}
	h.declare("designSessionAccess", "activityExecutionAccess")
	h.Record("activityExecutionAccess", ScenarioResult{Scenario: "uc3-P1", Status: StatusPass})
	if err := h.Flush(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(prior)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != recorded {
		t.Fatalf("a run that ran none of a component's scenarios must leave its index as recorded, got %q", raw)
	}
	if got := readIndex(t, root, "activityExecutionAccess"); len(got) != 1 {
		t.Fatalf("a recorded component is written: %+v", got)
	}
}

func TestFlushWritesNoIndexForADeclaredComponentThatNeverRan(t *testing.T) {
	root := filepath.Join(t.TempDir(), "results")
	h := &Host{ResultsRoot: root}
	h.declare("designSessionAccess")
	if err := h.Flush(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "designSessionAccess", resultsFile)); !os.IsNotExist(err) {
		t.Fatalf("no verdict, no index: stat = %v", err)
	}
}

func TestFlushRefusesAVerdictForAComponentThePackageDoesNotTest(t *testing.T) {
	root := filepath.Join(t.TempDir(), "results")
	h := &Host{ResultsRoot: root}
	h.declare("designSessionAccess")
	h.Record("projectStateAccess", ScenarioResult{Scenario: "uc3-P1", Status: StatusPass})
	err := h.Flush()
	if err == nil || !strings.Contains(err.Error(), "projectStateAccess/uc3-P1") {
		t.Fatalf("want the undeclared verdict named, got %v", err)
	}
	if _, serr := os.Stat(root); !os.IsNotExist(serr) {
		t.Fatalf("nothing is written when a verdict belongs to no index: stat = %v", serr)
	}
}

func TestFlushWritesNothingWhenNothingWasDeclaredOrRecorded(t *testing.T) {
	root := filepath.Join(t.TempDir(), "results")
	h := &Host{ResultsRoot: root}
	if err := h.Flush(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("no component, no index: stat %s = %v", root, err)
	}
}

func TestFlushRejectsAVerdictWithNoComponent(t *testing.T) {
	h := &Host{ResultsRoot: t.TempDir()}
	h.Record("", ScenarioResult{Scenario: "uc-P1", Status: StatusPass})
	err := h.Flush()
	if err == nil || !strings.Contains(err.Error(), "uc-P1") {
		t.Fatalf("a verdict with no component must fail the flush, naming the scenario; got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(h.ResultsRoot, resultsFile)); !os.IsNotExist(statErr) {
		t.Fatalf("no results.json may land at the results root: %v", statErr)
	}
}

func TestRecordReplacesAnEarlierVerdictForTheSameComponentAndScenario(t *testing.T) {
	h := &Host{ResultsRoot: t.TempDir()}
	h.Record("billingManager", ScenarioResult{Scenario: "uc-P1", Status: "fail"})
	h.Record("billingManager", ScenarioResult{Scenario: "uc-P1", Status: "pass"})
	if err := h.Flush(); err != nil {
		t.Fatal(err)
	}
	got := readIndex(t, h.ResultsRoot, "billingManager")
	if len(got) != 1 || got[0].Status != "pass" {
		t.Fatalf("%+v", got)
	}
}

func TestRunScenarioRecordsSkip(t *testing.T) {
	h := &Host{ResultsRoot: t.TempDir()}
	t.Run("probe", func(t *testing.T) {
		h.RunScenario(t, "x", "x-P1", func(t *testing.T) { t.Log("hello"); t.Skip("upstream") })
	})
	if r := h.entries[resultKey{"x", "x-P1"}]; r.Status != "skip" {
		t.Fatalf("%+v", h.entries)
	}
}

func TestRunScenarioRecordsPassWithDuration(t *testing.T) {
	h := &Host{ResultsRoot: t.TempDir()}
	t.Run("probe", func(t *testing.T) {
		h.RunScenario(t, "x", "x-P2", func(t *testing.T) {})
	})
	r, ok := h.entries[resultKey{"x", "x-P2"}]
	if !ok || r.Status != "pass" || r.Scenario != "x-P2" || r.DurationMs < 0 {
		t.Fatalf("%+v", h.entries)
	}
}

// helperEnv marks the re-executed test binary that hosts the deliberately
// failing scenarios: a failed sub-test fails its whole ancestry, so the
// verdicts are observed from a child process through the results.json it
// writes.
const helperEnv = "SCENARIOHOST_FAIL_HELPER_DIR"

func runHelper(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^"+name+"$")
	cmd.Env = append(os.Environ(), helperEnv+"="+dir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("the helper process must exit non-zero (its scenario fails):\n%s", out)
	}
	return dir
}

func TestRunScenarioRecordsFail(t *testing.T) {
	dir := runHelper(t, "TestHelperFailingScenario")
	got := readIndex(t, dir, "x")
	if len(got) != 1 || got[0].Scenario != "x-F1" || got[0].Status != "fail" {
		t.Fatalf("%+v", got)
	}
}

func TestHelperFailingScenario(t *testing.T) {
	dir := os.Getenv(helperEnv)
	if dir == "" {
		t.Skip("helper for TestRunScenarioRecordsFail; runs only in its child process")
	}
	h := &Host{ResultsRoot: dir}
	h.RunScenario(t, "x", "x-F1", func(t *testing.T) { t.Error("boom") })
	if err := h.Flush(); err != nil {
		t.Fatal(err)
	}
}
