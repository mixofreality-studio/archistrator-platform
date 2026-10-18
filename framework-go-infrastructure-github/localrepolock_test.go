package github_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	fwgithub "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
)

// TestLockLocalRepo_CanonicalKeyAndCancel — the file:// form, the plain form and
// a symlink to the same repo are ONE lock; a waiter whose ctx is cancelled
// returns ctx.Err() holding nothing; a different repo is never blocked.
func TestLockLocalRepo_CanonicalKeyAndCancel(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	other := filepath.Join(root, "other")
	for _, d := range []string{repo, other} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(repo, link); err != nil {
		t.Fatal(err)
	}

	unlock, err := fwgithub.LockLocalRepo(context.Background(), repo)
	if err != nil {
		t.Fatalf("LockLocalRepo: %v", err)
	}
	for _, alias := range []string{"file://" + repo, link, repo + "/./", "file://" + link} {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		u, lerr := fwgithub.LockLocalRepo(ctx, alias)
		cancel()
		if !errors.Is(lerr, context.DeadlineExceeded) || u != nil {
			t.Fatalf("alias %q must share the held lock and give up with ctx.Err(); got unlock=%v err=%v", alias, u != nil, lerr)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, cerr := fwgithub.LockLocalRepo(ctx, repo); !errors.Is(cerr, context.Canceled) {
		t.Fatalf("cancelled waiter: want context.Canceled, got %v", cerr)
	}

	uo, err := fwgithub.LockLocalRepo(context.Background(), other)
	if err != nil {
		t.Fatalf("a different repo must not be blocked: %v", err)
	}
	uo()

	unlock()
	unlock() // idempotent
	u2, err := fwgithub.LockLocalRepo(context.Background(), "file://"+link)
	if err != nil {
		t.Fatalf("relock after unlock: %v", err)
	}
	u2()
}

// TestPushLocal_HoldsLockAcrossRun — the lock is held for the whole callback and
// released after it, and run's error is returned unchanged.
func TestPushLocal_HoldsLockAcrossRun(t *testing.T) {
	repo := t.TempDir()
	sentinel := errors.New("run failed")
	err := fwgithub.PushLocal(context.Background(), repo, func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		if _, lerr := fwgithub.LockLocalRepo(ctx, "file://"+repo); !errors.Is(lerr, context.DeadlineExceeded) {
			t.Errorf("lock must be held inside run; got %v", lerr)
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("PushLocal must return run's error; got %v", err)
	}
	if err := fwgithub.PushLocal(context.Background(), repo, func() error { return nil }); err != nil {
		t.Fatalf("lock must be released after run: %v", err)
	}
}
