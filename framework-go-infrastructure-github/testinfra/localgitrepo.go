package testinfra

// localgitrepo.go provides a REAL throwaway on-disk git repo for the git-data /
// ref-CAS regression harness (the github satellite's GitStore and the
// projectStateAccess C-PA-R TestRefCasVsConcurrentWriter gate). Unlike FakeGitHub
// (a REST wire fake that cannot serve git smart-protocol), this is an ACTUAL git
// store: a bare repo on disk addressed by a `file://` URL, which go-git's file
// transport drives by shelling out to the local `git`. It therefore exercises the
// genuine non-fast-forward push rejection that IS the compare-and-swap — no mock,
// per the test-authoring constitution's real-store discipline.
//
// TEST-ONLY: nothing here is imported by production code.

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// LocalGitRepo is a throwaway bare git repo on disk plus its file:// clone URL.
type LocalGitRepo struct {
	// Dir is the bare repo's directory on disk (auto-removed by t.Cleanup).
	Dir string
	// URL is the file:// clone URL go-git uses to clone/push.
	URL string
}

// StartLocalGitRepo creates a bare git repo in a temp dir with an initial empty
// commit on `branch` (default "main" when empty), so a clone sees a real branch
// tip to compare-and-swap against. It skips the test if `git` is not on PATH.
// The repo is removed by t.TempDir's cleanup.
func StartLocalGitRepo(t *testing.T, branch string) LocalGitRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH; skipping real-git ref-CAS test")
	}
	if branch == "" {
		branch = "main"
	}

	root := t.TempDir()
	bare := filepath.Join(root, "remote.git")

	// Init a bare repo with the desired default branch.
	run(t, root, "git", "init", "--bare", "--initial-branch="+branch, bare)

	// Seed an initial commit so `branch` exists as a real ref (a CAS base). We do
	// this via a throwaway working clone, then push the seed commit back.
	work := filepath.Join(root, "seed")
	run(t, root, "git", "clone", bare, work)
	run(t, work, "git", "-c", "init.defaultBranch="+branch, "checkout", "-B", branch)
	run(t, work, "git", "config", "user.email", "seed@aiarch.local")
	run(t, work, "git", "config", "user.name", "seed")
	run(t, work, "git", "commit", "--allow-empty", "-m", "seed")
	run(t, work, "git", "push", "origin", branch)

	return LocalGitRepo{Dir: bare, URL: "file://" + bare}
}

// run executes a git command in dir, failing the test on error.
func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}
