package methodassets

import (
	"strings"
	"testing"
)

const checksLine = "**Checks.** Call `run_checks()` until it reports every check passing, and finish with one full `run_checks()` call. Do not run the checks yourself."

// Every dispatched step runs its checks through the run_checks tool, and no
// command tells the agent to generate, run or record tests: the workflow owns it.
func TestEveryStepCommandRunsItsChecks(t *testing.T) {
	files, err := ClaudeFiles()
	if err != nil {
		t.Fatal(err)
	}
	for slug := range Manifests() {
		body := string(files[".claude/commands/"+slug+".md"])
		if strings.Count(body, checksLine) != 1 {
			t.Errorf("%s: want exactly one run_checks line", slug)
		}
		if i, j := strings.Index(body, checksLine), strings.Index(body, "\n## Steps"); i < 0 || j < 0 || i > j {
			t.Errorf("%s: the run_checks line must sit above ## Steps", slug)
		}
		for _, banned := range []string{"record-test-run", "make test-scenarios", "make gen-tests"} {
			if strings.Contains(body, banned) {
				t.Errorf("%s: still tells the agent to %q (the workflow owns it)", slug, banned)
			}
		}
	}
}
