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

// StartLocalCheckedOutRepo creates the topology `archistrator init` produces for
// the LOCAL venue: a NON-bare repo with `branch` (default "main") CHECKED OUT and
// receive.denyCurrentBranch=updateInstead, seeded with one empty commit. A push
// into it runs git's push-to-checkout (read-tree -u -m) BEFORE the ref
// transaction checks the old value, so a racing loser can leave staged changes
// behind and wedge every later push (FIX-002). The bare StartLocalGitRepo cannot
// reproduce that: a lost push there changes nothing.
//
// Dir is the working tree (the .git dir lives under it); URL is its file:// form.
// It skips the test if `git` is not on PATH.
func StartLocalCheckedOutRepo(t *testing.T, branch string) LocalGitRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH; skipping real-git checked-out-repo test")
	}
	if branch == "" {
		branch = "main"
	}
	dir := filepath.Join(t.TempDir(), "state")
	run(t, filepath.Dir(dir), "git", "init", "--initial-branch="+branch, dir)
	run(t, dir, "git", "config", "user.email", "seed@aiarch.local")
	run(t, dir, "git", "config", "user.name", "seed")
	run(t, dir, "git", "config", "receive.denyCurrentBranch", "updateInstead")
	run(t, dir, "git", "commit", "--allow-empty", "-m", "seed")
	return LocalGitRepo{Dir: dir, URL: "file://" + dir}
}

// Git runs `git args...` in the repo's directory and returns its combined
// output, failing the test on error. Lets a test inspect or perturb the target
// (git status, stage a change, check out a branch) without its own exec plumbing.
func (r LocalGitRepo) Git(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}
