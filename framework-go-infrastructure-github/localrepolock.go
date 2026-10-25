package github

// localrepolock.go is the process-wide, per-repo serialisation point for every
// writer that pushes into a LOCAL (on-disk / file://) repo from this process.
//
// INTERIM (FIX-002, founder ruling option A, 2026-10-18). The lock is a
// COMPENSATION for a topology, not a root-cause fix. `archistrator init` makes
// the user's working repo the local state store: non-bare, main checked out,
// receive.denyCurrentBranch=updateInstead. With updateInstead git runs
// push-to-checkout (read-tree -u -m) BEFORE the ref transaction checks the old
// value, so a racing loser writes its tree into the target's index and worktree
// and then loses the ref CAS — leaving staged changes that make git refuse every
// later push to that branch, permanently. Against a bare repo, or a ref nobody
// checks out, concurrent pushes are an ordinary CAS and need no lock at all
// (TestGitStore_RefCASLoserConflict proves it).
//
// REMOVAL CONDITION: when the r2 topology ruling lands (local state no longer
// lives on a checked-out branch — a bare repo or refs no one checks out), delete
// the lock inside PushLocal (keep PushLocal as a pass-through or inline it) and
// keep the checkout-refusal classifier (pushclassify.go), which is the
// root-cause half and stands on its own. Earmark:
// archistrator docs/bugs FIX-002 note.
//
// SCOPE: the lock only serialises writers INSIDE this process. An agent's own
// `git push`, the user's push, or a dirty user worktree can still refuse a write;
// the classifier then reports that refusal once, non-retryably, with its real
// cause instead of a CAS loss.

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
)

// localRepoLocks maps a canonical repo path to a 1-slot channel semaphore. A
// channel (not a sync.Mutex) so a waiter can give up when its ctx is cancelled.
// Entries are never removed: the key set is the handful of local repos one
// server process writes to.
var localRepoLocks = struct {
	mu    sync.Mutex
	slots map[string]chan struct{}
}{slots: map[string]chan struct{}{}}

// CanonicalLocalRepoPath is the lock key for a local repo: the `file://` scheme
// stripped, then filepath.Abs, EvalSymlinks (when the path resolves) and Clean, so
// the file:// form, the plain form and a symlink to the same repo are ONE key.
func CanonicalLocalRepoPath(repoPath string) (string, error) {
	p := strings.TrimPrefix(strings.TrimSpace(repoPath), "file://")
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if resolved, rerr := filepath.EvalSymlinks(abs); rerr == nil {
		abs = resolved
	}
	return filepath.Clean(abs), nil
}

// LockLocalRepo takes the process-wide lock for the local repo at repoPath (a
// path or a file:// URL; see CanonicalLocalRepoPath). It blocks until the lock is
// free or ctx is done; on ctx cancellation it returns ctx.Err() and holds
// nothing. The returned unlock is idempotent. Prefer PushLocal, which cannot
// forget the unlock and is what the push arch gate (pushgate) recognises.
func LockLocalRepo(ctx context.Context, repoPath string) (unlock func(), err error) {
	key, err := CanonicalLocalRepoPath(repoPath)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	localRepoLocks.mu.Lock()
	slot, ok := localRepoLocks.slots[key]
	if !ok {
		slot = make(chan struct{}, 1)
		localRepoLocks.slots[key] = slot
	}
	localRepoLocks.mu.Unlock()

	select {
	case slot <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	var once sync.Once
	return func() { once.Do(func() { <-slot }) }, nil
}

// PushLocal runs `run` while holding the process-wide lock for the local repo at
// repoPath, so a whole clone → commit → push sequence into that repo is
// serialised against every other in-process writer. Every receive-pack into a
// local repo (branch pushes and deletions included, not only pushes to main)
// must run inside a PushLocal callback; the pushgate arch check enforces that by
// type. It returns ctx.Err() if the lock wait is cancelled, else run's error.
//
// INTERIM: see the file header for why the lock exists and when it is removed.
func PushLocal(ctx context.Context, repoPath string, run func() error) error {
	unlock, err := LockLocalRepo(ctx, repoPath)
	if err != nil {
		return err
	}
	defer unlock()
	return run()
}

// localRepoPath reports whether repoURL addresses a LOCAL repo (a file:// URL or
// a bare filesystem path, as opposed to an https/ssh/scp-style remote) and, if so,
// returns it in the form PushLocal accepts.
func localRepoPath(repoURL string) (string, bool) {
	u := strings.TrimSpace(repoURL)
	if strings.HasPrefix(u, "file://") {
		return u, true
	}
	if strings.Contains(u, "://") {
		return "", false
	}
	// scp-like remote: user@host:path (a ':' before the first '/').
	if i := strings.Index(u, ":"); i > 0 && !strings.Contains(u[:i], "/") && !filepath.IsAbs(u) {
		return "", false
	}
	return u, true
}

// withLocalRepoLock runs op under PushLocal when repoURL is a local repo, and
// plainly otherwise: a remote host (GitHub) has no checked-out branch to wedge,
// and its ref CAS needs no in-process serialisation.
func withLocalRepoLock(ctx context.Context, repoURL string, op func() error) error {
	if p, ok := localRepoPath(repoURL); ok {
		return PushLocal(ctx, p, op)
	}
	return op()
}
