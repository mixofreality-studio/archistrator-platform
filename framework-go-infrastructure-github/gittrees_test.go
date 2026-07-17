package github_test

// Satellite-level regression tests for the git-data (trees API) surface —
// GetRepoTree + the atomic multi-file commit chain (CreateBlob / CreateTree /
// CreateCommit / UpdateRef, composed by CommitFilesAtomic) and the pure
// GitBlobSHA helper. They exercise the satellite IN ISOLATION against the
// testinfra FakeGitHub's stateful git-data endpoints (which stage blobs/trees/
// commits invisibly and materialise them ONLY on the ref update, and reject an
// unforced non-fast-forward update 422 — the compare-and-swap), so the satellite
// carries its own coverage independent of any consuming ResourceAccess.

import (
	"context"
	"strings"
	"testing"

	fwgithub "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
	gh "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github/testinfra"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// TestGitBlobSHA_MatchesGitVectors pins the helper to git's own blob ids
// (`git hash-object` vectors) — the whole diff-without-fetching scheme depends
// on this exact function.
func TestGitBlobSHA_MatchesGitVectors(t *testing.T) {
	vectors := map[string]string{
		"":        "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391", // the empty blob
		"hello\n": "ce013625030ba8dba906f756967f9e9ca394464a",
	}
	for content, want := range vectors {
		if got := fwgithub.GitBlobSHA([]byte(content)); got != want {
			t.Fatalf("GitBlobSHA(%q) = %s, want %s", content, got, want)
		}
	}
}

func gitTreesFixture(t *testing.T) (*gh.FakeGitHub, *fwgithub.AppClient) {
	t.Helper()
	fake := gh.Start()
	t.Cleanup(fake.Close)
	fake.EnableRepoCatalog()
	fake.SeedRepo("acme", "widget", "", nil, true)
	return fake, newClient(t, fake.BaseURL())
}

// TestGetRepoTree_ListsBlobsWithRealGitSHAs — one recursive tree read returns
// every file with its REAL git blob id, so a caller can diff desired bytes
// locally via GitBlobSHA without any per-file content fetch.
func TestGetRepoTree_ListsBlobsWithRealGitSHAs(t *testing.T) {
	fake, c := gitTreesFixture(t)
	fake.SeedRepoFile("acme", "widget", ".claude/commands/a.md", []byte("prompt a"))
	fake.SeedRepoFile("acme", "widget", ".github/workflows/w.yml", []byte("name: w"))

	tree, err := c.GetRepoTree(context.Background(), "acme/widget", "main", true, "tok")
	if err != nil {
		t.Fatalf("GetRepoTree: %v", err)
	}
	if tree.Truncated {
		t.Fatal("fake tree must not be truncated")
	}
	got := map[string]string{}
	for _, e := range tree.Entries {
		if e.Type != "blob" {
			continue
		}
		got[e.Path] = e.SHA
	}
	for path, content := range map[string]string{
		".claude/commands/a.md":   "prompt a",
		".github/workflows/w.yml": "name: w",
	} {
		want := fwgithub.GitBlobSHA([]byte(content))
		if got[path] != want {
			t.Fatalf("tree sha for %s = %q, want the git blob sha %q", path, got[path], want)
		}
	}
}

// TestGetRepoTree_UnknownRefIsNotFound — an unborn/unknown ref maps to
// fwra.NotFound (callers converging a fresh repo treat it as "no tree yet").
func TestGetRepoTree_UnknownRefIsNotFound(t *testing.T) {
	fake := gh.Start()
	t.Cleanup(fake.Close)
	fake.EnableRepoCatalog()
	fake.SeedEmptyRepo("acme", "unborn", true)
	c := newClient(t, fake.BaseURL())

	_, err := c.GetRepoTree(context.Background(), "acme/unborn", "main", true, "tok")
	if kindOf(err) != fwra.NotFound {
		t.Fatalf("GetRepoTree on an unborn branch: kind = %v, want NotFound (err: %v)", kindOf(err), err)
	}
}

// TestCommitFilesAtomic_HappyChain — the full chain lands two files in ONE
// commit: get ref → blobs → tree (base = head tree) → commit (parent = head)
// → unforced ref PATCH; the files are readable afterwards and pre-existing
// content survives (base_tree layering, not tree replacement).
func TestCommitFilesAtomic_HappyChain(t *testing.T) {
	fake, c := gitTreesFixture(t)
	fake.SeedRepoFile("acme", "widget", "README.md", []byte("# keep me"))

	sha, err := c.CommitFilesAtomic(context.Background(), "acme/widget", "main", "aiarch: converge",
		map[string][]byte{
			".claude/commands/a.md":   []byte("prompt a v2"),
			".github/workflows/w.yml": []byte("name: w2"),
		}, fwgithub.CommitSignature{}, "tok")
	if err != nil {
		t.Fatalf("CommitFilesAtomic: %v", err)
	}
	if sha == "" {
		t.Fatal("expected a non-empty commit sha")
	}
	for path, want := range map[string]string{
		".claude/commands/a.md":   "prompt a v2",
		".github/workflows/w.yml": "name: w2",
		"README.md":               "# keep me", // untouched — base_tree layering
	} {
		got, ok := fake.RepoFile("acme", "widget", path)
		if !ok || string(got) != want {
			t.Fatalf("after atomic commit, %s = %q (found=%v), want %q", path, got, ok, want)
		}
	}
	// Exactly ONE commit object was created; the update was an unforced PATCH.
	commits, patches := 0, 0
	for _, r := range fake.Requests() {
		if r.Method == "POST" && strings.HasSuffix(r.Path, "/git/commits") {
			commits++
		}
		if r.Method == "PATCH" && strings.HasSuffix(r.Path, "/git/refs/heads/main") {
			patches++
			if strings.Contains(r.Body, `"force":true`) {
				t.Fatalf("ref update must be unforced, got body %q", r.Body)
			}
		}
	}
	if commits != 1 || patches != 1 {
		t.Fatalf("expected exactly 1 commit POST and 1 ref PATCH, got %d / %d", commits, patches)
	}
}

// TestCommitFilesAtomic_NonFastForwardIsConflict — a concurrent writer that
// advanced the branch between the head read and the ref update makes the
// unforced PATCH non-fast-forward; the satellite surfaces fwra.Conflict (the
// retry-by-re-read CAS-loss signal) and the branch content is untouched.
func TestCommitFilesAtomic_NonFastForwardIsConflict(t *testing.T) {
	fake, c := gitTreesFixture(t)
	fake.SeedRepoFile("acme", "widget", "f.txt", []byte("old"))
	fake.On("PATCH", "/repos/acme/widget/git/refs/heads/main",
		gh.Response{Status: 422, Body: `{"message":"Update is not a fast forward"}`})

	_, err := c.CommitFilesAtomic(context.Background(), "acme/widget", "main", "aiarch: converge",
		map[string][]byte{"f.txt": []byte("new")}, fwgithub.CommitSignature{}, "tok")
	if kindOf(err) != fwra.Conflict {
		t.Fatalf("non-fast-forward ref update: kind = %v, want Conflict (err: %v)", kindOf(err), err)
	}
	// Atomicity: the failed chain left the old content fully intact.
	got, ok := fake.RepoFile("acme", "widget", "f.txt")
	if !ok || string(got) != "old" {
		t.Fatalf("a failed atomic commit must leave the branch untouched, got %q", got)
	}
}

// TestCommitFilesAtomic_FailedChainLeavesBranchIntact — a mid-chain fault (the
// commit create 500s) leaves the branch EXACTLY as it was: nothing became
// reachable, so a retry re-runs the whole converge.
func TestCommitFilesAtomic_FailedChainLeavesBranchIntact(t *testing.T) {
	fake, c := gitTreesFixture(t)
	fake.SeedRepoFile("acme", "widget", "f.txt", []byte("old"))
	fake.On("POST", "/repos/acme/widget/git/commits",
		gh.Response{Status: 500, Body: `{"message":"boom"}`})

	_, err := c.CommitFilesAtomic(context.Background(), "acme/widget", "main", "aiarch: converge",
		map[string][]byte{"f.txt": []byte("new")}, fwgithub.CommitSignature{}, "tok")
	if kindOf(err) != fwra.Transient {
		t.Fatalf("mid-chain 500: kind = %v, want Transient (err: %v)", kindOf(err), err)
	}
	got, ok := fake.RepoFile("acme", "widget", "f.txt")
	if !ok || string(got) != "old" {
		t.Fatalf("a failed atomic commit must leave the branch untouched, got %q", got)
	}
	// The failure clears → the retry lands the whole converge.
	fake.ClearRoute("POST", "/repos/acme/widget/git/commits")
	if _, err := c.CommitFilesAtomic(context.Background(), "acme/widget", "main", "aiarch: converge",
		map[string][]byte{"f.txt": []byte("new")}, fwgithub.CommitSignature{}, "tok"); err != nil {
		t.Fatalf("CommitFilesAtomic (retry): %v", err)
	}
	if got, _ := fake.RepoFile("acme", "widget", "f.txt"); string(got) != "new" {
		t.Fatalf("retry must land the converge, got %q", got)
	}
}

// TestCommitFilesAtomic_UnbornBranchCreatesRef — a fresh repo (no branch yet)
// takes the root-commit tail: no parent, no base tree, and the ref is CREATED
// (POST git/refs) instead of patched.
func TestCommitFilesAtomic_UnbornBranchCreatesRef(t *testing.T) {
	fake := gh.Start()
	t.Cleanup(fake.Close)
	fake.EnableRepoCatalog()
	fake.SeedEmptyRepo("acme", "unborn", true)
	c := newClient(t, fake.BaseURL())

	sha, err := c.CommitFilesAtomic(context.Background(), "acme/unborn", "main", "aiarch: seat",
		map[string][]byte{"go.mod": []byte("module x\n")}, fwgithub.CommitSignature{}, "tok")
	if err != nil {
		t.Fatalf("CommitFilesAtomic (unborn): %v", err)
	}
	if sha == "" {
		t.Fatal("expected a non-empty root commit sha")
	}
	got, ok := fake.RepoFile("acme", "unborn", "go.mod")
	if !ok || string(got) != "module x\n" {
		t.Fatalf("root commit content = %q (found=%v)", got, ok)
	}
	created := false
	for _, r := range fake.Requests() {
		if r.Method == "POST" && strings.HasSuffix(r.Path, "/git/refs") {
			created = true
		}
		if r.Method == "PATCH" && strings.Contains(r.Path, "/git/refs/heads/") {
			t.Fatal("an unborn branch must be CREATED, not patched")
		}
	}
	if !created {
		t.Fatal("expected a POST git/refs ref create")
	}
}

// TestCommitFilesAtomic_Guards — bad input rejects before any wire call.
func TestCommitFilesAtomic_Guards(t *testing.T) {
	fake, c := gitTreesFixture(t)
	pre := len(fake.Requests())
	cases := []func() error{
		func() error {
			_, err := c.CommitFilesAtomic(context.Background(), "", "main", "m", map[string][]byte{"a": []byte("x")}, fwgithub.CommitSignature{}, "tok")
			return err
		},
		func() error {
			_, err := c.CommitFilesAtomic(context.Background(), "acme/widget", "", "m", map[string][]byte{"a": []byte("x")}, fwgithub.CommitSignature{}, "tok")
			return err
		},
		func() error {
			_, err := c.CommitFilesAtomic(context.Background(), "acme/widget", "main", "m", nil, fwgithub.CommitSignature{}, "tok")
			return err
		},
	}
	for i, run := range cases {
		if kindOf(run()) != fwra.ContractMisuse {
			t.Fatalf("guard case %d: want ContractMisuse", i)
		}
	}
	if len(fake.Requests()) != pre {
		t.Fatalf("guards must fire before any wire call; requests went %d → %d", pre, len(fake.Requests()))
	}
}

// TestUpdateRef_NonFastForwardIsConflict — the standalone primitive maps the
// 422 fast-forward rejection to fwra.Conflict; a genuine 422 (bad sha) stays
// ContractMisuse via ClassifyStatus.
func TestUpdateRef_NonFastForwardIsConflict(t *testing.T) {
	fake := gh.Start()
	t.Cleanup(fake.Close)
	fake.On("PATCH", "/repos/acme/widget/git/refs/heads/main",
		gh.Response{Status: 422, Body: `{"message":"Update is not a fast forward"}`})
	c := newClient(t, fake.BaseURL())

	err := c.UpdateRef(context.Background(), "acme/widget", "main", "abc123", "tok")
	if kindOf(err) != fwra.Conflict {
		t.Fatalf("non-fast-forward: kind = %v, want Conflict", kindOf(err))
	}

	fake.On("PATCH", "/repos/acme/widget/git/refs/heads/main",
		gh.Response{Status: 422, Body: `{"message":"Object does not exist"}`})
	err = c.UpdateRef(context.Background(), "acme/widget", "main", "abc123", "tok")
	if kindOf(err) != fwra.ContractMisuse {
		t.Fatalf("non-FF-unrelated 422: kind = %v, want ContractMisuse", kindOf(err))
	}
}
