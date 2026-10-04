package scenariohost

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	fwpg "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-postgres"
)

// probeComponent and probeFacet are two components sharing this one test
// package, as facets of one Go package do in a generated scenario test file.
const (
	probeComponent = "scenariohost-probe"
	probeFacet     = "scenariohost-probe-facet"
)

// TestMain runs the package through Main so the shared stack is flushed and
// torn down once per process, exactly as a generated scenario test package
// does.
func TestMain(m *testing.M) { Main(m, probeComponent, probeFacet) }

func TestStartIsSharedAndOutlivesItsFirstCaller(t *testing.T) {
	var first *Host
	t.Run("first-caller", func(t *testing.T) {
		first = Start(t)
		if first == nil {
			t.Fatal("Start returned nil")
		}
	})
	// The first caller's cleanups have run; the stack must still be up.
	second := Start(t)
	if second != first {
		t.Fatalf("Start must hand back ONE shared host per package: %p vs %p", first, second)
	}

	if first.TemporalHostPort == "" {
		t.Error("TemporalHostPort is empty")
	}
	if first.TemporalNamespace != "aiarch-test" {
		t.Errorf("TemporalNamespace = %q", first.TemporalNamespace)
	}
	if first.GitHub == nil || first.GitHub.BaseURL() == "" {
		t.Error("FakeGitHub not started")
	}
	if first.Actions == nil || first.Actions.BaseURL() == "" {
		t.Error("FakeActions not started")
	}
	if first.Repo.URL == "" || !strings.HasPrefix(first.Repo.URL, "file://") {
		t.Errorf("Repo.URL = %q", first.Repo.URL)
	}
	if _, err := os.Stat(filepath.Join(first.Repo.Dir, "HEAD")); err != nil {
		t.Errorf("the bare repo died with its first caller: %v", err)
	}
	assertRealGitRepoWithTip(t, first.Repo.URL, "main")
	if filepath.Base(first.ResultsRoot) != "test-results" || !filepath.IsAbs(first.ResultsRoot) {
		t.Errorf("ResultsRoot = %q, want an absolute path ending in test-results", first.ResultsRoot)
	}
	if got, want := first.ResultsDir(probeComponent), filepath.Join(first.ResultsRoot, probeComponent); got != want {
		t.Errorf("ResultsDir(%q) = %q, want %q", probeComponent, got, want)
	}
	if testing.Short() {
		if first.PostgresURL != "" {
			t.Errorf("PostgresURL must be empty under -short, got %q", first.PostgresURL)
		}
		return
	}
	if first.PostgresURL == "" {
		t.Fatal("PostgresURL is empty outside -short")
	}
	ctx, cancel := context.WithTimeout(first.Context(t), 30*time.Second)
	defer cancel()
	pool, err := fwpg.NewPool(ctx, first.PostgresURL)
	if err != nil {
		t.Fatalf("postgres died with its first caller: %v", err)
	}
	pool.Close()
}

// assertRealGitRepoWithTip drives the system `git` against the seeded repo:
// the consumer's go-git file transport shells out to it, so the bare repo
// must be one it lists a real branch tip for (a seeded commit, not just refs).
func assertRealGitRepoWithTip(t *testing.T, url, branch string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	out, err := exec.Command("git", "ls-remote", "--heads", url).CombinedOutput()
	if err != nil {
		t.Fatalf("git ls-remote %s: %v\n%s", url, err, out)
	}
	if !strings.Contains(string(out), "\trefs/heads/"+branch+"\n") {
		t.Fatalf("no %s tip in the seeded repo:\n%s", branch, out)
	}
	clone := t.TempDir()
	if out, err := exec.Command("git", "clone", "--quiet", url, clone).CombinedOutput(); err != nil {
		t.Fatalf("git clone %s: %v\n%s", url, err, out)
	}
	log, err := exec.Command("git", "-C", clone, "log", "--oneline").CombinedOutput()
	if err != nil || !strings.Contains(string(log), "seed") {
		t.Fatalf("clone has no seed commit: %v\n%s", err, log)
	}
}

func TestStartFlushesResultsOnCleanup(t *testing.T) {
	t.Run("scenario-holder", func(t *testing.T) {
		h := Start(t)
		h.RunScenario(t, probeComponent, "probe-P1", func(t *testing.T) {})
		h.RunScenario(t, probeFacet, "probe-P1", func(t *testing.T) {})
	})
	// The holder's t.Cleanup has flushed: each component's results.json
	// carries its own verdict for the scenario id both share.
	for _, component := range []string{probeComponent, probeFacet} {
		raw, err := os.ReadFile(filepath.Join(shared.ResultsDir(component), resultsFile))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), `"scenario": "probe-P1"`) || !strings.Contains(string(raw), `"status": "pass"`) {
			t.Fatalf("%s: %s", component, raw)
		}
	}
}
