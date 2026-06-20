package github_test

// Satellite-level regression tests for the content-addressable blob/tree primitive
// (GitBlobStore) the artifactAccess C-AA-R rework builds on. They drive a REAL
// throwaway on-disk git repo (testinfra.LocalGitRepo) over go-git's file
// transport — no mock. This is the satellite's own coverage of the
// store/read/walk + content-addressable dedup the contract's §6 mapping requires.

import (
	"context"
	"testing"

	fwgithub "github.com/davidmarne/archistrator-platform/framework-go-infrastructure-github"
	gh "github.com/davidmarne/archistrator-platform/framework-go-infrastructure-github/testinfra"
	fwra "github.com/davidmarne/archistrator-platform/framework-go/resourceaccess"
)

func localBlobStore(t *testing.T) (*fwgithub.GitBlobStore, fwgithub.GitAuth, context.Context) {
	t.Helper()
	repo := gh.StartLocalGitRepo(t, "main")
	store, err := fwgithub.NewGitBlobStore(repo.URL)
	if err != nil {
		t.Fatalf("NewGitBlobStore: %v", err)
	}
	return store, fwgithub.GitAuth{Local: true}, context.Background()
}

// TestGitBlobStore_StoreReadRoundTrip — store a file set on a content branch, read
// one back at the returned commit hash.
func TestGitBlobStore_StoreReadRoundTrip(t *testing.T) {
	store, auth, ctx := localBlobStore(t)

	files := []fwgithub.GitObjectFile{
		{Path: "output.txt", Bytes: []byte("hello")},
		{Path: "meta.json", Bytes: []byte(`{"mimeType":"text/plain"}`)},
	}
	hash, err := store.StoreOutput(ctx, "aiarch/output/abc", files, "aiarch: k1", auth)
	if err != nil {
		t.Fatalf("StoreOutput: %v", err)
	}
	if hash == "" {
		t.Fatal("expected non-empty commit hash")
	}

	got, err := store.ReadFileAtCommit(ctx, hash, "output.txt", auth)
	if err != nil {
		t.Fatalf("ReadFileAtCommit: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("round-trip mismatch: %q", got)
	}
}

// TestGitBlobStore_DeterministicDedup — storing byte-identical content on the same
// content branch yields the SAME commit hash with no new object (the content-
// addressable invariant: fixed author/time => hash is a pure function of the tree).
func TestGitBlobStore_DeterministicDedup(t *testing.T) {
	store, auth, ctx := localBlobStore(t)

	files := []fwgithub.GitObjectFile{{Path: "output.bin", Bytes: []byte("same bytes")}}

	h1, err := store.StoreOutput(ctx, "aiarch/output/dedup", files, "aiarch: k1", auth)
	if err != nil {
		t.Fatalf("StoreOutput #1: %v", err)
	}
	// Re-store identical content (different commit message must NOT matter: the
	// dedup short-circuits on the empty-commit-against-tip path).
	h2, err := store.StoreOutput(ctx, "aiarch/output/dedup", files, "aiarch: k2", auth)
	if err != nil {
		t.Fatalf("StoreOutput #2: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("expected identical commit hash for identical content, got %q vs %q", h1, h2)
	}
}

// TestGitBlobStore_ProbeFileAtBranchTip — the dedup probe hits on stored content
// and misses on an absent branch.
func TestGitBlobStore_ProbeFileAtBranchTip(t *testing.T) {
	store, auth, ctx := localBlobStore(t)

	// Miss on absent branch.
	if _, _, found, err := store.ProbeFileAtBranchTip(ctx, "aiarch/output/none", "output.txt", auth); err != nil || found {
		t.Fatalf("probe absent branch: found=%v err=%v, want found=false err=nil", found, err)
	}

	files := []fwgithub.GitObjectFile{{Path: "output.txt", Bytes: []byte("probe me")}}
	hash, err := store.StoreOutput(ctx, "aiarch/output/probe", files, "aiarch: k1", auth)
	if err != nil {
		t.Fatalf("StoreOutput: %v", err)
	}

	data, tip, found, err := store.ProbeFileAtBranchTip(ctx, "aiarch/output/probe", "output.txt", auth)
	if err != nil {
		t.Fatalf("ProbeFileAtBranchTip: %v", err)
	}
	if !found || string(data) != "probe me" || tip != hash {
		t.Fatalf("probe hit mismatch: found=%v data=%q tip=%q (want hash %q)", found, data, tip, hash)
	}
}

// TestGitBlobStore_WalkTreeFiles — flatten the file paths of a commit's tree.
func TestGitBlobStore_WalkTreeFiles(t *testing.T) {
	store, auth, ctx := localBlobStore(t)

	files := []fwgithub.GitObjectFile{
		{Path: "output.go", Bytes: []byte("package main")},
		{Path: "meta.json", Bytes: []byte("{}")},
	}
	hash, err := store.StoreOutput(ctx, "aiarch/output/tree", files, "aiarch: k1", auth)
	if err != nil {
		t.Fatalf("StoreOutput: %v", err)
	}

	paths, err := store.WalkTreeFiles(ctx, hash, auth)
	if err != nil {
		t.Fatalf("WalkTreeFiles: %v", err)
	}
	var sawOutput, sawMeta bool
	for _, p := range paths {
		switch p {
		case "output.go":
			sawOutput = true
		case "meta.json":
			sawMeta = true
		}
	}
	if !sawOutput || !sawMeta {
		t.Fatalf("WalkTreeFiles missing entries: %v", paths)
	}
}

// TestGitBlobStore_NotFound — an unknown commit hash surfaces fwra.NotFound on both
// read and walk.
func TestGitBlobStore_NotFound(t *testing.T) {
	store, auth, ctx := localBlobStore(t)
	const unknown = "0123456789abcdef0123456789abcdef01234567"

	if _, err := store.ReadFileAtCommit(ctx, unknown, "output.txt", auth); kindOf(err) != fwra.NotFound {
		t.Fatalf("ReadFileAtCommit unknown: kind=%v, want NotFound", kindOf(err))
	}
	if _, err := store.WalkTreeFiles(ctx, unknown, auth); kindOf(err) != fwra.NotFound {
		t.Fatalf("WalkTreeFiles unknown: kind=%v, want NotFound", kindOf(err))
	}
}
