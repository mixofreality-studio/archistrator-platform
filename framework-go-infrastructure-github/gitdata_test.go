package github_test

// Satellite-level regression tests for the git-data / ref-CAS primitive
// (GitStore). They drive a REAL throwaway on-disk git repo (testinfra.LocalGitRepo)
// over go-git's file transport, so the non-fast-forward push rejection that IS the
// compare-and-swap is exercised genuinely — no mock. This is the satellite's own
// coverage of the primitive the projectStateAccess C-PA-R rework builds on.

import (
	"context"
	"errors"
	"testing"

	fwgithub "github.com/davidmarne/archistrator-platform/framework-go-infrastructure-github"
	gh "github.com/davidmarne/archistrator-platform/framework-go-infrastructure-github/testinfra"
	fwra "github.com/davidmarne/archistrator-platform/framework-go/resourceaccess"
)

const statePrefix = ".aiarch/state"

func localStore(t *testing.T) (*fwgithub.GitStore, fwgithub.GitAuth, context.Context) {
	t.Helper()
	repo := gh.StartLocalGitRepo(t, "main")
	store, err := fwgithub.NewGitStore(repo.URL, "main")
	if err != nil {
		t.Fatalf("NewGitStore: %v", err)
	}
	return store, fwgithub.GitAuth{Local: true}, context.Background()
}

// TestGitStore_RoundTrip — write a subtree, read it back at the new tip.
func TestGitStore_RoundTrip(t *testing.T) {
	store, auth, ctx := localStore(t)

	snap, err := store.ReadSubtree(ctx, statePrefix, auth)
	if err != nil {
		t.Fatalf("ReadSubtree (empty): %v", err)
	}
	files := map[string][]byte{"project.json": []byte(`{"v":1}`)}
	res, err := store.CommitSubtree(ctx, statePrefix, files, snap.Base, "aiarch: k1", auth)
	if err != nil {
		t.Fatalf("CommitSubtree: %v", err)
	}
	if res.Base == "" {
		t.Fatal("expected non-empty base token after commit")
	}

	got, err := store.ReadSubtree(ctx, statePrefix, auth)
	if err != nil {
		t.Fatalf("ReadSubtree (after): %v", err)
	}
	if string(got.Files["project.json"]) != `{"v":1}` {
		t.Fatalf("round-trip mismatch: %q", got.Files["project.json"])
	}
	if got.Base != res.Base {
		t.Fatalf("read base %q != commit base %q", got.Base, res.Base)
	}
}

// TestGitStore_RefCASLoserConflict — two writers from the SAME base: one wins, the
// loser's push is rejected non-fast-forward and surfaces fwra.Conflict; retrying
// the loser against the winner's new base lands cleanly (no lost update). This is
// the satellite-level proof of the CAS primitive C-PA-R depends on.
func TestGitStore_RefCASLoserConflict(t *testing.T) {
	store, auth, ctx := localStore(t)

	base := mustReadBase(t, store, auth, ctx)

	// Writer A wins.
	resA, err := store.CommitSubtree(ctx, statePrefix,
		map[string][]byte{"project.json": []byte(`{"w":"A"}`)}, base, "aiarch: A", auth)
	if err != nil {
		t.Fatalf("writer A commit: %v", err)
	}

	// Writer B races from the SAME (now stale) base — must lose the CAS.
	_, err = store.CommitSubtree(ctx, statePrefix,
		map[string][]byte{"project.json": []byte(`{"w":"B"}`)}, base, "aiarch: B", auth)
	if err == nil {
		t.Fatal("writer B expected ref-CAS conflict, got success (LOST UPDATE)")
	}
	if k := kindOf(err); k != fwra.Conflict {
		t.Fatalf("writer B error kind = %v, want Conflict", k)
	}
	if !errors.Is(err, fwgithub.ErrRefCASLost) {
		t.Fatalf("writer B error not ErrRefCASLost: %v", err)
	}

	// B retries against the WINNER's new base — now lands.
	resB, err := store.CommitSubtree(ctx, statePrefix,
		map[string][]byte{"project.json": []byte(`{"w":"B"}`)}, resA.Base, "aiarch: B-retry", auth)
	if err != nil {
		t.Fatalf("writer B retry: %v", err)
	}

	got := mustRead(t, store, auth, ctx)
	if string(got.Files["project.json"]) != `{"w":"B"}` {
		t.Fatalf("after retry, tip = %q, want B's write", got.Files["project.json"])
	}
	if got.Base != resB.Base {
		t.Fatalf("tip base %q != B-retry base %q", got.Base, resB.Base)
	}
}

func mustReadBase(t *testing.T, s *fwgithub.GitStore, auth fwgithub.GitAuth, ctx context.Context) string {
	t.Helper()
	return mustRead(t, s, auth, ctx).Base
}

func mustRead(t *testing.T, s *fwgithub.GitStore, auth fwgithub.GitAuth, ctx context.Context) fwgithub.GitSnapshot {
	t.Helper()
	snap, err := s.ReadSubtree(ctx, statePrefix, auth)
	if err != nil {
		t.Fatalf("ReadSubtree: %v", err)
	}
	return snap
}
