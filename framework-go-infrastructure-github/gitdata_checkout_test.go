package github_test

// FIX-002 regression tests: the LOCAL venue's state repo is the topology
// `archistrator init` produces — NON-bare, main CHECKED OUT, and
// receive.denyCurrentBranch=updateInstead. Every other GitStore test runs against
// the bare testinfra.LocalGitRepo, where a lost push changes nothing and can only
// be a genuine non-fast-forward; here a racing loser's push-to-checkout writes its
// tree into the target's index before losing the ref CAS, which wedged the repo
// permanently in run-20261017T211949Z-30a6612c (1159 "concurrent ref advance"
// retries of a refusal that could never clear).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	fwgithub "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
	gh "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github/testinfra"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

func assertCleanCheckout(t *testing.T, repo gh.LocalGitRepo, when string) {
	t.Helper()
	if st := strings.TrimSpace(repo.Git(t, "status", "--porcelain", "--untracked-files=no")); st != "" {
		t.Fatalf("%s: target checkout is dirty (wedged):\n%s", when, st)
	}
}

// TestGitStore_CheckedOutUpdateInsteadConcurrentWriters — 8 in-process writers
// CommitSubtree from ONE shared base into the checked-out updateInstead repo,
// 50 rounds. Exactly one wins each round, every loser is a genuine CAS Conflict,
// and the target's checkout stays clean (no wedge); a fresh-base write lands
// afterwards.
func TestGitStore_CheckedOutUpdateInsteadConcurrentWriters(t *testing.T) {
	repo := gh.StartLocalCheckedOutRepo(t, "main")
	store, err := fwgithub.NewGitStore(repo.URL, "main")
	if err != nil {
		t.Fatalf("NewGitStore: %v", err)
	}
	auth, ctx := fwgithub.GitAuth{Local: true}, context.Background()

	const rounds, writers = 50, 8
	for round := range rounds {
		base := mustReadBase(t, store, auth, ctx)
		errs := make([]error, writers)
		var wg sync.WaitGroup
		for w := range writers {
			wg.Go(func() {
				files := map[string][]byte{"project.json": fmt.Appendf(nil, `{"round":%d,"writer":%d}`, round, w)}
				_, errs[w] = store.CommitSubtree(ctx, statePrefix, files, base, fmt.Sprintf("r%d w%d", round, w), auth)
			})
		}
		wg.Wait()

		wins := 0
		for w, werr := range errs {
			if werr == nil {
				wins++
				continue
			}
			var fe *fwra.Error
			if !errors.As(werr, &fe) || fe.Kind != fwra.Conflict || !errors.Is(werr, fwgithub.ErrRefCASLost) {
				t.Fatalf("round %d writer %d: want a CAS Conflict loser, got %v", round, w, werr)
			}
		}
		if wins != 1 {
			t.Fatalf("round %d: want exactly 1 winner, got %d (errs=%v)", round, wins, errs)
		}
		assertCleanCheckout(t, repo, fmt.Sprintf("after round %d", round))
	}

	base := mustReadBase(t, store, auth, ctx)
	if _, err := store.CommitSubtree(ctx, statePrefix, map[string][]byte{"project.json": []byte(`{"after":true}`)}, base, "after", auth); err != nil {
		t.Fatalf("fresh-base write after the race: %v", err)
	}
	assertCleanCheckout(t, repo, "after the fresh-base write")
}

// assertCheckoutRefusal pins the FIX-002 classification of a push the target's
// checked-out working tree refused: NOT a CAS loss (not Conflict, not
// ErrRefCASLost), an Infrastructure fault with Retryable explicitly FALSE (the
// refusal is permanent until a human cleans the checkout), naming the dirty
// checkout.
func assertCheckoutRefusal(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("want the push refused by the dirty checked-out target, got success")
	}
	if errors.Is(err, fwgithub.ErrRefCASLost) {
		t.Fatalf("a checkout refusal must not be a CAS loss: %v", err)
	}
	var fe *fwra.Error
	if !errors.As(err, &fe) {
		t.Fatalf("want *fwra.Error, got %T: %v", err, err)
	}
	if fe.Kind == fwra.Conflict {
		t.Fatalf("a checkout refusal must not be Conflict: %v", err)
	}
	if fe.Kind != fwra.Infrastructure {
		t.Fatalf("want Infrastructure, got %v: %v", fe.Kind, err)
	}
	if fe.Retryable {
		t.Fatalf("a checkout refusal is permanent; want Retryable=false: %v", err)
	}
	if !strings.Contains(err.Error(), "dirty checkout") {
		t.Fatalf("error must name the dirty checkout of the target: %v", err)
	}
}

// stageStrayChange leaves a staged change in the target's index — the state the
// wedge leaves behind (and the state a user's own `git add` can leave).
func stageStrayChange(t *testing.T, repo gh.LocalGitRepo) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo.Dir, "stray.txt"), []byte("stray"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo.Git(t, "add", "stray.txt")
}

// TestGitStore_DirtyCheckedOutTargetIsNotACASLoss — a write from the CORRECT base
// into a checked-out target with staged changes is refused by push-to-checkout.
// That refusal ignores the base, so it must not be reported as a CAS loss (which
// the app retries until its bound burns out).
func TestGitStore_DirtyCheckedOutTargetIsNotACASLoss(t *testing.T) {
	repo := gh.StartLocalCheckedOutRepo(t, "main")
	store, err := fwgithub.NewGitStore(repo.URL, "main")
	if err != nil {
		t.Fatalf("NewGitStore: %v", err)
	}
	auth, ctx := fwgithub.GitAuth{Local: true}, context.Background()
	base := mustReadBase(t, store, auth, ctx)
	stageStrayChange(t, repo)

	_, err = store.CommitSubtree(ctx, statePrefix, map[string][]byte{"project.json": []byte(`{"v":1}`)}, base, "write", auth)
	assertCheckoutRefusal(t, err)
}

// TestGitBlobStore_DirtyCheckedOutContentBranchIsNotACASLoss — the same
// classification on the content-branch path: a user who checks out a content
// branch and leaves it dirty must not turn StoreOutput into a CAS-loss retry loop.
func TestGitBlobStore_DirtyCheckedOutContentBranchIsNotACASLoss(t *testing.T) {
	repo := gh.StartLocalCheckedOutRepo(t, "main")
	store, err := fwgithub.NewGitBlobStore(repo.URL)
	if err != nil {
		t.Fatalf("NewGitBlobStore: %v", err)
	}
	auth, ctx := fwgithub.GitAuth{Local: true}, context.Background()
	const branch = "aiarch/out/abc"
	if _, err := store.StoreOutput(ctx, branch, []fwgithub.GitObjectFile{{Path: "out.txt", Bytes: []byte("v1")}}, "v1", auth); err != nil {
		t.Fatalf("seed StoreOutput: %v", err)
	}
	repo.Git(t, "checkout", branch)
	stageStrayChange(t, repo)

	_, err = store.StoreOutput(ctx, branch, []fwgithub.GitObjectFile{{Path: "out.txt", Bytes: []byte("v2")}}, "v2", auth)
	assertCheckoutRefusal(t, err)
}
