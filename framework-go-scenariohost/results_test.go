package scenariohost

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestResultsJSONIsSortedAndComplete(t *testing.T) {
	h := &Host{ResultsDir: t.TempDir()}
	h.Record(ScenarioResult{Scenario: "uc-P2", Status: "pass", DurationMs: 3})
	h.Record(ScenarioResult{Scenario: "uc-P1", Status: "fail", Stderr: "boom"})
	if err := h.Flush(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(h.ResultsDir, "results.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got []ScenarioResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("results.json is not a ScenarioResult array: %v\n%s", err, raw)
	}
	if len(got) != 2 || got[0].Scenario != "uc-P1" || got[1].Status != "pass" {
		t.Fatalf("%s", raw)
	}
	if got[0].Stderr != "boom" {
		t.Fatalf("stderr not carried: %+v", got[0])
	}
}

func TestResultsJSONIsAnEmptyArrayWhenNothingRan(t *testing.T) {
	h := &Host{ResultsDir: filepath.Join(t.TempDir(), "nested", "dir")}
	if err := h.Flush(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(h.ResultsDir, "results.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "[]" {
		t.Fatalf("want an empty JSON array, got %q", raw)
	}
}

func TestRecordReplacesAnEarlierVerdictForTheSameScenario(t *testing.T) {
	h := &Host{ResultsDir: t.TempDir()}
	h.Record(ScenarioResult{Scenario: "uc-P1", Status: "fail"})
	h.Record(ScenarioResult{Scenario: "uc-P1", Status: "pass"})
	if len(h.entries) != 1 || h.entries["uc-P1"].Status != "pass" {
		t.Fatalf("%+v", h.entries)
	}
}

func TestRunScenarioRecordsFailure(t *testing.T) {
	h := &Host{ResultsDir: t.TempDir()}
	t.Run("probe", func(t *testing.T) {
		h.RunScenario(t, "x-P1", func(t *testing.T) { t.Log("hello"); t.Skip("upstream") })
	})
	if h.entries["x-P1"].Status != "skip" {
		t.Fatalf("%+v", h.entries)
	}
}

func TestRunScenarioRecordsPassWithDuration(t *testing.T) {
	h := &Host{ResultsDir: t.TempDir()}
	t.Run("probe", func(t *testing.T) {
		h.RunScenario(t, "x-P2", func(t *testing.T) {})
	})
	r, ok := h.entries["x-P2"]
	if !ok || r.Status != "pass" || r.Scenario != "x-P2" || r.DurationMs < 0 {
		t.Fatalf("%+v", h.entries)
	}
}

// helperEnv marks the re-executed test binary that hosts the deliberately
// failing scenario: a failed sub-test fails its whole ancestry, so the fail
// verdict is observed from a child process through the results.json it writes.
const helperEnv = "SCENARIOHOST_FAIL_HELPER_DIR"

func TestRunScenarioRecordsFail(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperFailingScenario$")
	cmd.Env = append(os.Environ(), helperEnv+"="+dir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("the helper process must exit non-zero (its scenario fails):\n%s", out)
	}
	raw, readErr := os.ReadFile(filepath.Join(dir, "results.json"))
	if readErr != nil {
		t.Fatalf("helper wrote no results.json: %v\n%s", readErr, out)
	}
	var got []ScenarioResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Scenario != "x-F1" || got[0].Status != "fail" {
		t.Fatalf("%s", raw)
	}
}

func TestHelperFailingScenario(t *testing.T) {
	dir := os.Getenv(helperEnv)
	if dir == "" {
		t.Skip("helper for TestRunScenarioRecordsFail; runs only in its child process")
	}
	h := &Host{ResultsDir: dir}
	h.RunScenario(t, "x-F1", func(t *testing.T) { t.Error("boom") })
	if err := h.Flush(); err != nil {
		t.Fatal(err)
	}
}
