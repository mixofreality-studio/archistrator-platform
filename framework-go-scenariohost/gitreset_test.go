package scenariohost

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// A scenario starts from the seed repository: what the one before it committed, on main or on
// another branch, is gone.
func TestRunScenario_ResetsGitReposToSeed(t *testing.T) {
	h := Start(t)
	h.RunScenario(t, probeComponent, "reset-dirty-P1", func(t *testing.T) {
		h.SeedResource(t, "Repo", json.RawMessage(`{"kind":"gitRepo","model":{"branch":"main","message":"dirty","files":{"leak.txt":"x"}}}`))
		if out, err := exec.Command("git", "-C", h.Repo.Dir, "branch", "side", "main").CombinedOutput(); err != nil {
			t.Fatalf("branch side: %v: %s", err, out)
		}
	})
	h.RunScenario(t, probeComponent, "reset-clean-P1", func(t *testing.T) {
		out, err := exec.Command("git", "-C", h.Repo.Dir, "ls-tree", "-r", "--name-only", "main").CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != "" {
			t.Fatalf("main tree after reset = %q, %v; want empty", out, err)
		}
		branches, err := exec.Command("git", "-C", h.Repo.Dir, "branch", "--format=%(refname:short)").CombinedOutput()
		if err != nil || strings.TrimSpace(string(branches)) != "main" {
			t.Fatalf("branches after reset = %q, %v; want only main", branches, err)
		}
	})
}
