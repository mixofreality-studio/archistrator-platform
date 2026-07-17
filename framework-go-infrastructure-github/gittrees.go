package github

// gittrees.go carries the GIT-DATA (trees) REST wire plumbing the
// sourceControlAccess managed-scaffold converge needs to compare-and-write the
// seated scaffold EFFICIENTLY: ONE recursive tree read to diff the whole managed
// file set against locally computed blob SHAs (GitBlobSHA — no per-file Contents
// GET), and ONE atomic multi-file commit chain (blobs → tree → commit → ref
// fast-forward) to land every drifted file in a single commit (no
// one-commit-per-file Contents PUT loop). It is the satellite home for the
// trees-API primitives — kept HERE (the github satellite), out of the product RA,
// exactly as the App-JWT / installation-token / PR-rail / Contents wire code is,
// so the RA stays provider-opaque.
//
// This is the REST (AppClient/installation-token) counterpart of the go-git
// plumbing in gitdata.go/gitblob.go: those speak the git protocol against a
// remote URL; this file speaks the GitHub git-data REST endpoints under the same
// AppClient conventions (do(), ClassifyStatus, fwra.* error taxonomy).
//
// PROVIDER-OPACITY: the value types this file exposes (RepoTree, TreeEntry,
// TreeWriteEntry, CommitSignature) carry GitHub git-data vocabulary BY DESIGN —
// they are the satellite's provider-specific surface. The consuming RA wraps the
// results in its provider-neutral value types and never lets a GitHub lexeme
// cross its own contract.

import (
	"context"
	"crypto/sha1" // #nosec G505 -- git's object model mandates SHA-1 blob ids; not used for security.
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// GitBlobSHA computes git's blob object id for `content` — SHA-1 over
// "blob {len}\x00{content}" — as lowercase hex. It is a PURE helper: a caller
// holding the desired bytes can diff them against a GetRepoTree listing WITHOUT
// fetching any file content (the tree entries carry exactly this id).
func GitBlobSHA(content []byte) string {
	h := sha1.New() // #nosec G401 -- git blob ids are protocol-mandated SHA-1, not a security control.
	_, _ = fmt.Fprintf(h, "blob %d\x00", len(content))
	_, _ = h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}

// TreeEntry is one entry of a read tree (GetRepoTree): its repo-relative path,
// its git object id (for a blob: exactly GitBlobSHA of the content), and its
// type ("blob" | "tree" | "commit" for a submodule).
type TreeEntry struct {
	Path string
	SHA  string
	Type string
}

// RepoTree is the read view GetRepoTree returns: the tree's own object id, its
// entries, and GitHub's truncation flag (a recursive listing over ~100k entries
// is truncated — a truncated listing is NOT a sound diff base, so callers must
// treat every path as potentially drifted).
type RepoTree struct {
	SHA       string
	Entries   []TreeEntry
	Truncated bool
}

// TreeWriteEntry is one file to place in a created tree (CreateTree): the
// repo-relative path and the blob object id a prior CreateBlob returned. Every
// entry is written as a regular file (mode 100644, type blob) — the managed
// scaffold seats no executables/symlinks/submodules.
type TreeWriteEntry struct {
	Path string
	SHA  string
}

// CommitSignature optionally names the commit author/committer. The ZERO value
// omits both, letting GitHub attribute the commit to the authenticated identity
// (for an installation token: the App's bot identity) — the same attribution the
// Contents API produced.
type CommitSignature struct {
	Name  string
	Email string
}

func (s CommitSignature) isZero() bool { return s.Name == "" && s.Email == "" }

// treeDTO is the wire view of GET/POST .../git/trees responses.
type treeDTO struct {
	SHA  string `json:"sha"`
	Tree []struct {
		Path string `json:"path"`
		SHA  string `json:"sha"`
		Type string `json:"type"`
	} `json:"tree"`
	Truncated bool `json:"truncated"`
}

// shaDTO is the minimal {sha} envelope the blob/commit creates return.
type shaDTO struct {
	SHA string `json:"sha"`
}

// gitCommitDTO is the bit of GET .../git/commits/{sha} the chain reads: the
// commit's tree id (the base_tree of the next CreateTree).
type gitCommitDTO struct {
	SHA  string `json:"sha"`
	Tree struct {
		SHA string `json:"sha"`
	} `json:"tree"`
}

// GetRepoTree lists the tree at `ref` (a branch name, commit SHA, or tree SHA)
// in `fullName` via GET .../git/trees/{ref}, recursively when `recursive`. The
// entries carry each object's git id, so a caller can diff desired bytes against
// the listing purely locally via GitBlobSHA — ONE request replaces a per-file
// Contents-GET loop. A missing ref (unknown branch / unborn repo) maps to
// fwra.NotFound via ClassifyStatus; callers converging a fresh repo treat that
// as "no tree yet".
func (c *AppClient) GetRepoTree(ctx context.Context, fullName, ref string, recursive bool, instToken string) (RepoTree, error) {
	if strings.TrimSpace(fullName) == "" {
		return RepoTree{}, fwra.New(fwra.ContractMisuse, "GetRepoTree: empty repo")
	}
	if strings.TrimSpace(ref) == "" {
		return RepoTree{}, fwra.New(fwra.ContractMisuse, "GetRepoTree: empty ref")
	}
	url := fmt.Sprintf("%s/repos/%s/git/trees/%s", c.baseURL, fullName, ref)
	if recursive {
		url += "?recursive=1"
	}
	status, body, dErr := c.do(ctx, http.MethodGet, url, nil, "", instToken)
	if dErr != nil {
		return RepoTree{}, dErr
	}
	if status < 200 || status >= 300 {
		return RepoTree{}, ClassifyStatus(status, "GetRepoTree")
	}
	var dto treeDTO
	if uerr := json.Unmarshal(body, &dto); uerr != nil {
		return RepoTree{}, fwra.Wrap(fwra.Infrastructure, uerr, "GetRepoTree: decode")
	}
	out := RepoTree{SHA: dto.SHA, Truncated: dto.Truncated, Entries: make([]TreeEntry, 0, len(dto.Tree))}
	for _, e := range dto.Tree {
		out.Entries = append(out.Entries, TreeEntry{Path: e.Path, SHA: e.SHA, Type: e.Type})
	}
	return out, nil
}

// CreateBlob stores `content` as a git blob in `fullName` via POST
// .../git/blobs (base64-encoded, so binary content is safe) and returns the
// blob's object id — which equals GitBlobSHA(content).
func (c *AppClient) CreateBlob(ctx context.Context, fullName string, content []byte, instToken string) (string, error) {
	if strings.TrimSpace(fullName) == "" {
		return "", fwra.New(fwra.ContractMisuse, "CreateBlob: empty repo")
	}
	payload := map[string]string{
		"content":  base64.StdEncoding.EncodeToString(content),
		"encoding": "base64",
	}
	body, mErr := json.Marshal(payload)
	if mErr != nil {
		return "", fwra.Wrap(fwra.ContractMisuse, mErr, "CreateBlob: marshal")
	}
	url := fmt.Sprintf("%s/repos/%s/git/blobs", c.baseURL, fullName)
	status, respBody, dErr := c.do(ctx, http.MethodPost, url, body, "", instToken)
	if dErr != nil {
		return "", dErr
	}
	if status < 200 || status >= 300 {
		return "", ClassifyStatus(status, "CreateBlob")
	}
	var dto shaDTO
	if uerr := json.Unmarshal(respBody, &dto); uerr != nil {
		return "", fwra.Wrap(fwra.Infrastructure, uerr, "CreateBlob: decode")
	}
	return dto.SHA, nil
}

// CreateTree creates a tree in `fullName` via POST .../git/trees, layering
// `entries` (each a regular-file blob) over `baseTree` (the head commit's tree
// id; empty on an unborn branch — the new tree then carries ONLY the entries).
// Returns the new tree's object id.
func (c *AppClient) CreateTree(ctx context.Context, fullName, baseTree string, entries []TreeWriteEntry, instToken string) (string, error) {
	if strings.TrimSpace(fullName) == "" {
		return "", fwra.New(fwra.ContractMisuse, "CreateTree: empty repo")
	}
	if len(entries) == 0 {
		return "", fwra.New(fwra.ContractMisuse, "CreateTree: empty entry set")
	}
	wire := make([]map[string]string, 0, len(entries))
	for _, e := range entries {
		if strings.TrimSpace(e.Path) == "" || strings.TrimSpace(e.SHA) == "" {
			return "", fwra.New(fwra.ContractMisuse, "CreateTree: entry with empty path/sha")
		}
		wire = append(wire, map[string]string{
			"path": e.Path, "mode": "100644", "type": "blob", "sha": e.SHA,
		})
	}
	payload := map[string]any{"tree": wire}
	if baseTree != "" {
		payload["base_tree"] = baseTree
	}
	body, mErr := json.Marshal(payload)
	if mErr != nil {
		return "", fwra.Wrap(fwra.ContractMisuse, mErr, "CreateTree: marshal")
	}
	url := fmt.Sprintf("%s/repos/%s/git/trees", c.baseURL, fullName)
	status, respBody, dErr := c.do(ctx, http.MethodPost, url, body, "", instToken)
	if dErr != nil {
		return "", dErr
	}
	if status < 200 || status >= 300 {
		return "", ClassifyStatus(status, "CreateTree")
	}
	var dto shaDTO
	if uerr := json.Unmarshal(respBody, &dto); uerr != nil {
		return "", fwra.Wrap(fwra.Infrastructure, uerr, "CreateTree: decode")
	}
	return dto.SHA, nil
}

// CreateCommit creates a commit object in `fullName` via POST .../git/commits
// pointing at `treeSHA` with `parents` (empty on an unborn branch's root
// commit). A zero `committer` lets GitHub attribute the commit to the
// authenticated identity (the App bot for an installation token). Returns the
// new commit's object id.
func (c *AppClient) CreateCommit(ctx context.Context, fullName, message, treeSHA string, parents []string, committer CommitSignature, instToken string) (string, error) {
	if strings.TrimSpace(fullName) == "" {
		return "", fwra.New(fwra.ContractMisuse, "CreateCommit: empty repo")
	}
	if strings.TrimSpace(treeSHA) == "" {
		return "", fwra.New(fwra.ContractMisuse, "CreateCommit: empty tree sha")
	}
	if parents == nil {
		parents = []string{}
	}
	payload := map[string]any{"message": message, "tree": treeSHA, "parents": parents}
	if !committer.isZero() {
		sig := map[string]string{"name": committer.Name, "email": committer.Email}
		payload["author"] = sig
		payload["committer"] = sig
	}
	body, mErr := json.Marshal(payload)
	if mErr != nil {
		return "", fwra.Wrap(fwra.ContractMisuse, mErr, "CreateCommit: marshal")
	}
	url := fmt.Sprintf("%s/repos/%s/git/commits", c.baseURL, fullName)
	status, respBody, dErr := c.do(ctx, http.MethodPost, url, body, "", instToken)
	if dErr != nil {
		return "", dErr
	}
	if status < 200 || status >= 300 {
		return "", ClassifyStatus(status, "CreateCommit")
	}
	var dto shaDTO
	if uerr := json.Unmarshal(respBody, &dto); uerr != nil {
		return "", fwra.Wrap(fwra.Infrastructure, uerr, "CreateCommit: decode")
	}
	return dto.SHA, nil
}

// UpdateRef fast-forwards refs/heads/{branch} in `fullName` to `sha` via PATCH
// .../git/refs/heads/{branch}. The update is ALWAYS unforced ({"force": false})
// — GitHub's non-fast-forward rejection IS the compare-and-swap: a concurrent
// writer that advanced the branch between the caller's head read and this PATCH
// makes the update non-fast-forward, surfaced as fwra.Conflict (the same
// taxonomy slot as gitdata.go's ErrRefCASLost push rejection). Other failures
// map via ClassifyStatus.
func (c *AppClient) UpdateRef(ctx context.Context, fullName, branch, sha, instToken string) error {
	if strings.TrimSpace(fullName) == "" {
		return fwra.New(fwra.ContractMisuse, "UpdateRef: empty repo")
	}
	if strings.TrimSpace(branch) == "" || strings.TrimSpace(sha) == "" {
		return fwra.New(fwra.ContractMisuse, "UpdateRef: empty branch/sha")
	}
	payload := map[string]any{"sha": sha, "force": false}
	body, mErr := json.Marshal(payload)
	if mErr != nil {
		return fwra.Wrap(fwra.ContractMisuse, mErr, "UpdateRef: marshal")
	}
	url := fmt.Sprintf("%s/repos/%s/git/refs/heads/%s", c.baseURL, fullName, branch)
	status, respBody, dErr := c.do(ctx, http.MethodPatch, url, body, "", instToken)
	if dErr != nil {
		return dErr
	}
	if status >= 200 && status < 300 {
		return nil
	}
	if isNonFastForwardRefStatus(status, respBody) {
		return fwra.New(fwra.Conflict, "UpdateRef: non-fast-forward — branch advanced by a concurrent writer")
	}
	return ClassifyStatus(status, "UpdateRef")
}

// isNonFastForwardRefStatus recognises the ref-update rejection that signals a
// lost compare-and-swap. GitHub reports it as a 422 whose body names the
// fast-forward failure ("Update is not a fast forward"); a 409 on the ref
// endpoint is the same contention class.
func isNonFastForwardRefStatus(status int, body []byte) bool {
	if status == http.StatusConflict {
		return true
	}
	if status != http.StatusUnprocessableEntity {
		return false
	}
	return strings.Contains(strings.ToLower(string(body)), "fast forward")
}

// CommitFilesAtomic lands `files` (repo-relative path → bytes) on `branch` of
// `fullName` as ONE commit, via the standard git-data chain:
//
//	get ref head → create blobs → create tree (base = head commit's tree)
//	→ create commit (parent = head) → update ref (unforced fast-forward)
//
// ATOMICITY: nothing is reachable until the final ref update — a failure at any
// earlier step leaves the branch (and every file on it) EXACTLY as it was, so a
// caller's retry re-runs the whole converge against a clean base. The unforced
// ref update is the compare-and-swap: a concurrent writer that advanced the
// branch after the head read makes the update non-fast-forward → fwra.Conflict
// (retry-by-re-read).
//
// An ABSENT branch (unborn ref / fresh repo) is handled by creating the root
// commit (no parent, no base tree) and CREATING the ref instead of patching it;
// a first-writer race on the create surfaces as fwra.Conflict too.
//
// A zero `committer` attributes the commit to the authenticated identity (the
// App bot). Returns the new commit's object id.
func (c *AppClient) CommitFilesAtomic(ctx context.Context, fullName, branch, message string, files map[string][]byte, committer CommitSignature, instToken string) (string, error) {
	if strings.TrimSpace(fullName) == "" {
		return "", fwra.New(fwra.ContractMisuse, "CommitFilesAtomic: empty repo")
	}
	if strings.TrimSpace(branch) == "" {
		return "", fwra.New(fwra.ContractMisuse, "CommitFilesAtomic: empty branch")
	}
	if len(files) == 0 {
		return "", fwra.New(fwra.ContractMisuse, "CommitFilesAtomic: empty fileset")
	}

	headSHA, baseTree, exists, err := c.branchHeadAndTree(ctx, fullName, branch, instToken)
	if err != nil {
		return "", err
	}

	entries, err := c.createFileBlobs(ctx, fullName, files, instToken)
	if err != nil {
		return "", err
	}

	treeSHA, err := c.CreateTree(ctx, fullName, baseTree, entries, instToken)
	if err != nil {
		return "", err
	}

	var parents []string
	if exists {
		parents = []string{headSHA}
	}
	commitSHA, err := c.CreateCommit(ctx, fullName, message, treeSHA, parents, committer, instToken)
	if err != nil {
		return "", err
	}

	if exists {
		if err := c.UpdateRef(ctx, fullName, branch, commitSHA, instToken); err != nil {
			return "", err
		}
		return commitSHA, nil
	}
	if err := c.createBranchRef(ctx, fullName, branch, commitSHA, instToken); err != nil {
		return "", err
	}
	return commitSHA, nil
}

// branchHeadAndTree resolves the branch head commit and its tree id. An absent
// ref (404 — unborn branch / fresh repo) is NOT an error: exists=false and the
// chain roots a parentless commit.
func (c *AppClient) branchHeadAndTree(ctx context.Context, fullName, branch, instToken string) (headSHA, treeSHA string, exists bool, err error) {
	url := fmt.Sprintf("%s/repos/%s/git/ref/heads/%s", c.baseURL, fullName, branch)
	status, body, dErr := c.do(ctx, http.MethodGet, url, nil, "", instToken)
	if dErr != nil {
		return "", "", false, dErr
	}
	if status == http.StatusNotFound {
		return "", "", false, nil
	}
	if status < 200 || status >= 300 {
		return "", "", false, ClassifyStatus(status, "CommitFilesAtomic: get ref")
	}
	var r refDTO
	if uerr := json.Unmarshal(body, &r); uerr != nil {
		return "", "", false, fwra.Wrap(fwra.Infrastructure, uerr, "CommitFilesAtomic: decode ref")
	}
	headSHA = r.Object.SHA

	commitURL := fmt.Sprintf("%s/repos/%s/git/commits/%s", c.baseURL, fullName, headSHA)
	status, body, dErr = c.do(ctx, http.MethodGet, commitURL, nil, "", instToken)
	if dErr != nil {
		return "", "", false, dErr
	}
	if status < 200 || status >= 300 {
		return "", "", false, ClassifyStatus(status, "CommitFilesAtomic: get head commit")
	}
	var commit gitCommitDTO
	if uerr := json.Unmarshal(body, &commit); uerr != nil {
		return "", "", false, fwra.Wrap(fwra.Infrastructure, uerr, "CommitFilesAtomic: decode head commit")
	}
	return headSHA, commit.Tree.SHA, true, nil
}

// createFileBlobs uploads every file as a blob (deterministic path order) and
// returns the tree entries pointing at them.
func (c *AppClient) createFileBlobs(ctx context.Context, fullName string, files map[string][]byte, instToken string) ([]TreeWriteEntry, error) {
	paths := make([]string, 0, len(files))
	for p := range files {
		if strings.TrimSpace(p) == "" {
			return nil, fwra.New(fwra.ContractMisuse, "CommitFilesAtomic: empty path in fileset")
		}
		paths = append(paths, p)
	}
	sort.Strings(paths)
	entries := make([]TreeWriteEntry, 0, len(paths))
	for _, p := range paths {
		sha, err := c.CreateBlob(ctx, fullName, files[p], instToken)
		if err != nil {
			return nil, err
		}
		entries = append(entries, TreeWriteEntry{Path: p, SHA: sha})
	}
	return entries, nil
}

// createBranchRef creates refs/heads/{branch} at `sha` (the unborn-branch tail
// of the atomic chain). An already-exists rejection means a first writer landed
// between the head probe and this create — the same CAS loss as a
// non-fast-forward PATCH, surfaced as fwra.Conflict.
func (c *AppClient) createBranchRef(ctx context.Context, fullName, branch, sha, instToken string) error {
	payload := map[string]string{"ref": "refs/heads/" + branch, "sha": sha}
	body, mErr := json.Marshal(payload)
	if mErr != nil {
		return fwra.Wrap(fwra.ContractMisuse, mErr, "CommitFilesAtomic: marshal ref create")
	}
	url := fmt.Sprintf("%s/repos/%s/git/refs", c.baseURL, fullName)
	status, _, dErr := c.do(ctx, http.MethodPost, url, body, "", instToken)
	if dErr != nil {
		return dErr
	}
	if status == http.StatusUnprocessableEntity || status == http.StatusConflict {
		return fwra.New(fwra.Conflict,
			"CommitFilesAtomic: branch "+strconv.Quote(branch)+" created by a concurrent first writer")
	}
	if status < 200 || status >= 300 {
		return ClassifyStatus(status, "CommitFilesAtomic: create ref")
	}
	return nil
}
