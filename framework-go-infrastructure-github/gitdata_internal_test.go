package github

// gitdata_internal_test.go pins the BOUNDED-CLONE invariant on the git-data /
// ref-CAS path (GitStore): every operation clones ONLY the target-branch tip,
// never the repo's full history. The invariant is load-bearing for memory
// safety — the 2026-08-11 production outage was the server OOM-crash-looping
// because listProjects full-cloned a project repo whose per-mutation project.json
// rewrites had grown its history past the container's memory limit (a ~60MB pack
// ballooned to >800MB of runtime memory during the in-memory unpack). The CAS
// never needs history: reads walk the tip tree, and a CAS write's parent IS the
// observed tip.

import (
	"context"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/object"
	gh "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github/testinfra"
)

// TestGitStoreClone_FetchesOnlyBranchTip — after growing the remote's history to
// several commits (via the store's own CAS writes), a fresh clone must contain
// exactly ONE commit object: the branch tip. Fetching the full history is the
// memory-unbounded behavior this test forbids.
func TestGitStoreClone_FetchesOnlyBranchTip(t *testing.T) {
	repo := gh.StartLocalGitRepo(t, "main")
	store, err := NewGitStore(repo.URL, "main")
	if err != nil {
		t.Fatalf("NewGitStore: %v", err)
	}
	auth, ctx := GitAuth{Local: true}, context.Background()

	// Grow history: three sequential CAS writes on top of the seed commit.
	base := ""
	if snap, rerr := store.ReadSubtree(ctx, ".aiarch/state", auth); rerr == nil {
		base = snap.Base
	} else {
		t.Fatalf("ReadSubtree: %v", rerr)
	}
	for i, doc := range []string{`{"v":1}`, `{"v":2}`, `{"v":3}`} {
		res, cerr := store.CommitSubtree(ctx, ".aiarch/state",
			map[string][]byte{"project.json": []byte(doc)}, base, "aiarch: grow", auth)
		if cerr != nil {
			t.Fatalf("CommitSubtree %d: %v", i, cerr)
		}
		base = res.Base
	}

	cloned, err := store.clone(ctx, auth)
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	count := 0
	iter, err := cloned.CommitObjects()
	if err != nil {
		t.Fatalf("CommitObjects: %v", err)
	}
	if err := iter.ForEach(func(*object.Commit) error { count++; return nil }); err != nil {
		t.Fatalf("iterate commits: %v", err)
	}
	if count != 1 {
		t.Fatalf("clone fetched %d commits, want exactly 1 (the branch tip) — full-history clones are memory-unbounded", count)
	}
}
