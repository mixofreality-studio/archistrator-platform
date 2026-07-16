package github

// gitdata.go carries the GIT-DATA wire plumbing the project-state ResourceAccess
// (projectStateAccess, C-PA-R) needs to persist head-state as JSON files in the
// per-project git repo under git ref COMPARE-AND-SWAP. It is the satellite home
// for the read-blob / write-tree / commit / update-ref-with-expected-base
// operations the contract's REWORK.0–REWORK.3 require — kept HERE (the github
// satellite), out of the product RA, exactly as the App-JWT / installation-token
// / PR-rail wire code is, so the RA stays provider-opaque.
//
// PROVIDER-NEUTRAL GIT PLUMBING: although this module is named for GitHub, the
// git-data path is pure git (go-git): clone a remote, mutate a worktree subtree,
// commit, and push a single branch. It works against ANY git remote the URL
// addresses — github.com over HTTPS in production, a local bare repo over the
// `file://` transport in the C-PA-R regression harness (TestRefCasVsConcurrentWriter),
// or a Gitea host. The ref compare-and-swap is git's OWN non-fast-forward push
// rejection: the new commit's parent IS the observed base, so a racing winner
// that advanced the branch makes the loser's PLAIN (never --force) push a
// non-fast-forward, which git rejects. That rejection IS the CAS — it holds for
// every actor, protection-bypass or not, precisely because the push is unforced
// (C-PA-R invariant ii / review §R1.2-3,4).
//
// PROVIDER-OPACITY: the value types this file exposes (GitStore, GitCommitResult,
// GitAuth, the sentinel ErrRefCASLost) carry NO GitHub lexeme — no SHA on the
// contract-facing return (the SHA is internal, surfaced only as an opaque base
// token the RA re-meanings into projectStateAccess.Version), no owner/repo, no
// installation token format. GitHub Git-Data vocabulary lives only in this
// satellite's internals. The consuming RA threads a provider-neutral credential.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// ErrRefCASLost is the sentinel a CAS push returns when the target ref was
// advanced by a concurrent writer between the clone (the observed base) and the
// push — i.e. the push was rejected non-fast-forward. The consuming RA maps it
// to fwra.Conflict so the Manager re-reads HEAD and re-applies (the optimistic-
// concurrency loser path; contract REWORK.1). It is the git analog of the
// Postgres version-guard conflict.
var ErrRefCASLost = errors.New("git ref compare-and-swap lost: non-fast-forward")

// gitStoreAuthorName / gitStoreAuthorEmail stamp the state commits. The author
// is the aiarch App identity; the value is opaque to the RA above.
const (
	gitStoreAuthorName  = "aiarch"
	gitStoreAuthorEmail = "aiarch@projectstateaccess.local"
)

// GitAuth is the provider-neutral authentication the caller threads into a CAS
// operation. Exactly ONE shape is meaningful per call:
//
//   - Token: a bearer credential (the installation token the Manager minted via
//     getInstallationToken and threaded down as RepoCredential.Bytes). Presented
//     as git-HTTP BasicAuth (username "x-access-token", password = token), the
//     GitHub-App convention. NEVER logged or parsed here.
//   - Local: when true, the remote is a local `file://` / on-disk repo (the LOCAL
//     deployment profile and the C-PA-R regression harness); no HTTP credential is
//     attached. This is the "trivially-valid local credential" the contract's
//     LOCAL profile threads (REWORK.4).
//
// A zero GitAuth with Local=false and an empty Token authenticates nothing — used
// only against an open/local remote.
type GitAuth struct {
	Token string
	Local bool
}

// authMethod folds GitAuth into a go-git transport.AuthMethod. A local/empty
// credential yields nil (no auth header), which is correct for a file:// remote.
func (a GitAuth) authMethod() transport.AuthMethod {
	if a.Local || strings.TrimSpace(a.Token) == "" {
		return nil
	}
	// GitHub App installation tokens authenticate over git-HTTP as
	// BasicAuth{username: "x-access-token", password: <token>}.
	return &githttp.BasicAuth{Username: "x-access-token", Password: a.Token}
}

// GitStore is the satellite's provider-neutral git-data handle for one repo. It
// holds only the remote URL; every call clones fresh (stateless, so it is safe
// for concurrent callers and replays cleanly under Temporal retry). It performs
// NO IO at construction.
type GitStore struct {
	repoURL string
	branch  string // the CAS target branch (typically "main")
}

// NewGitStore builds a git-data handle over the repo reachable at repoURL,
// targeting `branch` for compare-and-swap writes (empty == "main"). No IO.
func NewGitStore(repoURL, branch string) (*GitStore, error) {
	if strings.TrimSpace(repoURL) == "" {
		return nil, fwra.New(fwra.ContractMisuse, "github.NewGitStore: empty repoURL")
	}
	if strings.TrimSpace(branch) == "" {
		branch = "main"
	}
	return &GitStore{repoURL: repoURL, branch: branch}, nil
}

// GitCommitResult is the provider-neutral outcome of a successful CAS write. Base
// is an OPAQUE token (the committed branch-tip identity) the RA carries forward
// as its concurrency token; the RA never parses it. Files is the post-commit
// view of the managed subtree (so the RA can read-your-writes without a second
// clone). NO GitHub lexeme is named on this type's contract role — Base is
// opaque text.
type GitCommitResult struct {
	// Base is the opaque tip token after the commit landed — the value the next
	// CAS write passes back as expectedBase. (Internally a commit SHA; the RA
	// treats it as opaque.)
	Base string
}

// GitSnapshot is the read view of the managed subtree at the current branch tip.
type GitSnapshot struct {
	// Base is the opaque tip token observed at read time (the CAS base for a
	// subsequent write; "" when the branch/subtree does not exist yet).
	Base string
	// Files maps each managed file's repo-relative path (under the caller's
	// pathPrefix, prefix-stripped) to its bytes. Empty when nothing is stored yet.
	Files map[string][]byte
	// Exists reports whether the branch already exists on the remote (false on a
	// brand-new repo whose default branch has no commits the clone can see).
	Exists bool
	// CommitTime is the author timestamp of the branch tip commit. Zero when the
	// branch does not exist yet or the commit cannot be resolved (non-fatal; callers
	// treat zero as "no timestamp available"). This is additive and opt-in — existing
	// callers that do not read CommitTime are unaffected.
	CommitTime time.Time
}

// ReadSubtree clones the remote and returns every file under pathPrefix at the
// target branch tip, plus the opaque base token for a follow-up CAS write. A
// remote with no such branch/subtree yields an empty, non-nil snapshot with
// Exists=false and Base="" — the caller then opens the aggregate (the first
// write seeds it).
func (s *GitStore) ReadSubtree(ctx context.Context, pathPrefix string, auth GitAuth) (GitSnapshot, error) {
	repo, err := s.clone(ctx, auth)
	if err != nil {
		if errors.Is(err, transport.ErrEmptyRemoteRepository) {
			return GitSnapshot{Files: map[string][]byte{}}, nil
		}
		return GitSnapshot{}, err
	}
	tip, exists := s.remoteTip(repo)
	if !exists {
		return GitSnapshot{Files: map[string][]byte{}}, nil
	}
	files, err := readSubtreeAt(repo, tip, pathPrefix)
	if err != nil {
		return GitSnapshot{}, err
	}
	// Resolve the tip commit's author timestamp for catalog ordering (additive;
	// zero on failure — non-fatal, callers treat zero as "not available").
	var commitTime time.Time
	if commit, cerr := repo.CommitObject(tip); cerr == nil {
		commitTime = commit.Author.When.UTC()
	}
	return GitSnapshot{Base: tip.String(), Files: files, Exists: true, CommitTime: commitTime}, nil
}

// CommitSubtree atomically replaces the managed subtree (every path under
// pathPrefix) with `files` and pushes the new commit to the target branch under
// ref compare-and-swap against expectedBase:
//
//   - expectedBase == "" opens the aggregate: the commit is the first on a fresh
//     branch (or fast-forwards an empty repo). A concurrent first-writer that
//     beat us makes the push non-fast-forward → ErrRefCASLost.
//   - expectedBase != "" must equal the branch tip observed at read time; the new
//     commit's parent IS that tip, so the push is a fast-forward iff no racer
//     advanced the branch. A racer's advance → non-fast-forward → ErrRefCASLost.
//
// The whole subtree is rewritten from `files` (a delete is expressed by omitting
// a path). The push is ALWAYS plain (never --force) — git's own non-fast-forward
// rejection IS the CAS (C-PA-R invariant ii). `message` is the commit message
// (the RA embeds the idempotency key); `files` keys are repo-relative paths under
// pathPrefix (prefix-relative, joined here).
func (s *GitStore) CommitSubtree(
	ctx context.Context,
	pathPrefix string,
	files map[string][]byte,
	expectedBase string,
	message string,
	auth GitAuth,
) (GitCommitResult, error) {
	repo, err := s.clone(ctx, auth)
	if err != nil && !errors.Is(err, transport.ErrEmptyRemoteRepository) {
		return GitCommitResult{}, err
	}

	branchRef := plumbing.NewBranchReferenceName(s.branch)
	tip, exists := plumbing.ZeroHash, false
	if repo != nil {
		tip, exists = s.remoteTip(repo)
	}

	// CAS pre-check: fails fast on obviously stale base without a wasted push.
	if err := checkCASPrecondition(expectedBase, exists, tip); err != nil {
		return GitCommitResult{}, err
	}

	// Build the worktree for the commit. On a fresh repo we init in memory; on an
	// existing branch we base the commit on the observed tip so the push is a
	// fast-forward when uncontended.
	wt, repo, err := s.worktreeFor(ctx, repo, branchRef, tip, exists, auth)
	if err != nil {
		return GitCommitResult{}, err
	}

	if err := replaceSubtree(wt, pathPrefix, files); err != nil {
		return GitCommitResult{}, err
	}

	commitHash, err := commitWorktree(wt, message)
	if err != nil {
		return GitCommitResult{}, err
	}

	// Plain (non-force) push. Non-fast-forward rejection = CAS loss.
	if err := handleSubtreePushError(repo.PushContext(ctx, &gogit.PushOptions{
		Auth:     auth.authMethod(),
		RefSpecs: []config.RefSpec{config.RefSpec(fmt.Sprintf("%s:%s", branchRef, branchRef))},
	})); err != nil {
		return GitCommitResult{}, err
	}
	return GitCommitResult{Base: commitHash.String()}, nil
}

func checkCASPrecondition(expectedBase string, exists bool, tip plumbing.Hash) error {
	if expectedBase == "" {
		if exists {
			return fwra.Wrap(fwra.Conflict, ErrRefCASLost,
				"github.CommitSubtree: open-aggregate write but branch already exists")
		}
		return nil
	}
	if !exists || tip.String() != expectedBase {
		return fwra.Wrap(fwra.Conflict, ErrRefCASLost,
			"github.CommitSubtree: stale base (branch advanced since read)")
	}
	return nil
}

func handleSubtreePushError(err error) error {
	if err == nil || errors.Is(err, gogit.NoErrAlreadyUpToDate) {
		return nil
	}
	if isNonFastForward(err) {
		return fwra.Wrap(fwra.Conflict, ErrRefCASLost,
			"github.CommitSubtree: concurrent ref advance (non-fast-forward)")
	}
	return ClassifyGitError(err, "github.CommitSubtree: push")
}

// clone fetches the full repo into in-memory storage (no on-disk worktree). A
// full clone lets the caller see the target branch tip and read the subtree in
// one round-trip.
func (s *GitStore) clone(ctx context.Context, auth GitAuth) (*gogit.Repository, error) {
	repo, err := gogit.CloneContext(ctx, memory.NewStorage(), memfs.New(), &gogit.CloneOptions{
		URL:  s.repoURL,
		Auth: auth.authMethod(),
	})
	if err != nil {
		return nil, ClassifyGitError(err, "github.GitStore.clone")
	}
	return repo, nil
}

// remoteTip resolves the target branch tip via its remote-tracking ref
// (refs/remotes/origin/<branch>); a full clone exposes branches only as
// remote-tracking refs. exists=false means the branch is absent (fresh repo).
func (s *GitStore) remoteTip(repo *gogit.Repository) (plumbing.Hash, bool) {
	remoteRef := plumbing.NewRemoteReferenceName("origin", s.branch)
	ref, err := repo.Reference(remoteRef, true)
	if err != nil {
		// Fall back to HEAD (a freshly-init'd remote may expose only HEAD).
		head, herr := repo.Head()
		if herr != nil {
			return plumbing.ZeroHash, false
		}
		return head.Hash(), true
	}
	return ref.Hash(), true
}

// worktreeFor prepares a worktree checked out to the target branch, basing it on
// the observed tip when the branch exists (so the push fast-forwards) or on a
// fresh in-memory repo when it does not. Returns the (possibly newly-created)
// repo alongside the worktree.
func (s *GitStore) worktreeFor(
	ctx context.Context,
	repo *gogit.Repository,
	branchRef plumbing.ReferenceName,
	tip plumbing.Hash,
	exists bool,
	auth GitAuth,
) (*gogit.Worktree, *gogit.Repository, error) {
	if repo == nil {
		return s.initFreshRepoWorktree(branchRef)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return nil, nil, fwra.Wrap(fwra.Infrastructure, err, "github.GitStore: worktree")
	}
	// The target branch may already exist as a LOCAL ref (clone's default branch)
	// or only as a remote-tracking ref. Resolve both: reset the local branch to the
	// observed tip and check it out, creating the local branch only when absent.
	_, localErr := repo.Reference(branchRef, false)
	co := &gogit.CheckoutOptions{Branch: branchRef, Force: true}
	if localErr != nil {
		// No local branch yet — create it (pointing at the observed tip).
		co.Create = true
		if exists {
			co.Hash = tip
		}
	}
	if err := wt.Checkout(co); err != nil {
		return nil, nil, fwra.Wrap(fwra.Infrastructure, err, "github.GitStore: checkout branch")
	}
	if exists && localErr == nil {
		// Local branch already pointed somewhere (the clone's tip); hard-reset it to
		// the observed remote tip so the new commit's parent IS the CAS base.
		if err := wt.Reset(&gogit.ResetOptions{Commit: tip, Mode: gogit.HardReset}); err != nil {
			return nil, nil, fwra.Wrap(fwra.Infrastructure, err, "github.GitStore: reset to base")
		}
	}
	return wt, repo, nil
}

func (s *GitStore) initFreshRepoWorktree(branchRef plumbing.ReferenceName) (*gogit.Worktree, *gogit.Repository, error) {
	fresh, err := gogit.Init(memory.NewStorage(), memfs.New())
	if err != nil {
		return nil, nil, fwra.Wrap(fwra.Infrastructure, err, "github.GitStore: init fresh repo")
	}
	if _, err := fresh.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{s.repoURL}}); err != nil {
		return nil, nil, fwra.Wrap(fwra.Infrastructure, err, "github.GitStore: wire remote")
	}
	wt, err := fresh.Worktree()
	if err != nil {
		return nil, nil, fwra.Wrap(fwra.Infrastructure, err, "github.GitStore: fresh worktree")
	}
	if err := wt.Checkout(&gogit.CheckoutOptions{Branch: branchRef, Create: true}); err != nil {
		return nil, nil, fwra.Wrap(fwra.Infrastructure, err, "github.GitStore: checkout fresh branch")
	}
	return wt, fresh, nil
}

// replaceSubtree makes the worktree's pathPrefix subtree exactly equal `files`:
// it removes every existing file under pathPrefix, then writes the supplied set.
// Expressing the whole subtree as a value (rather than a diff) keeps the write
// atomic and idempotent — the same `files` always yields the same tree.
func replaceSubtree(wt *gogit.Worktree, pathPrefix string, files map[string][]byte) error {
	billyFS := wt.Filesystem
	prefix := strings.TrimSuffix(pathPrefix, "/")

	// Remove the existing subtree (best-effort: absent prefix is fine).
	if err := removeDirAll(billyFS, prefix); err != nil {
		return fwra.Wrap(fwra.Infrastructure, err, "github.GitStore: clear subtree")
	}

	// Write the new set in deterministic path order (stable trees across runs).
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, rel := range paths {
		full := joinRepoPath(prefix, rel)
		if err := writeBillyFile(billyFS, full, files[rel]); err != nil {
			return fwra.Wrap(fwra.Infrastructure, err, "github.GitStore: write "+full)
		}
		if _, err := wt.Add(full); err != nil {
			return fwra.Wrap(fwra.Infrastructure, err, "github.GitStore: stage "+full)
		}
	}

	// Stage removals too (go-git's Add of a deleted path records the deletion).
	status, err := wt.Status()
	if err != nil {
		return fwra.Wrap(fwra.Infrastructure, err, "github.GitStore: status")
	}
	return stageDeletions(wt, status)
}

func stageDeletions(wt *gogit.Worktree, status gogit.Status) error {
	for path, st := range status {
		if st.Worktree == gogit.Deleted || st.Staging == gogit.Deleted {
			if _, err := wt.Add(path); err != nil {
				return fwra.Wrap(fwra.Infrastructure, err, "github.GitStore: stage deletion "+path)
			}
		}
	}
	return nil
}

// commitWorktree commits the staged worktree with a deterministic author and the
// supplied message. An empty commit (no change vs the base) is reported by go-git
// as ErrEmptyCommit — the caller's idempotency probe should have short-circuited
// before this, but if it slips through we surface it as a benign no-op signal via
// the zero hash + a sentinel so callers can resolve the existing tip.
//
// The author/committer TIMESTAMP is the real wall clock. Unlike gitblob.go's
// content-addressable path (where a FIXED epoch time is load-bearing — the
// commit hash must be a pure function of the tree so identical re-stores
// collapse), the CAS state path needs NO commit-hash determinism: retry
// idempotency is the committed applied_mutations ledger the RA probes BEFORE
// committing, the CAS itself keys off the parent hash, and the committed
// project.json already carries a wall-clock updatedAt. A fixed epoch here
// (the pre-2026-07-16 behavior) stamped every state commit 1970-01-01, which
// broke recency ordering (GitSnapshot.CommitTime, catalog UpdatedAt fallback,
// repo pushed_at heuristics) and rendered as "56 years ago" on the host UI.
func commitWorktree(wt *gogit.Worktree, message string) (plumbing.Hash, error) {
	sig := &object.Signature{Name: gitStoreAuthorName, Email: gitStoreAuthorEmail, When: time.Now().UTC()}
	hash, err := wt.Commit(message, &gogit.CommitOptions{Author: sig, Committer: sig})
	if err != nil {
		if errors.Is(err, gogit.ErrEmptyCommit) {
			return plumbing.ZeroHash, fwra.Wrap(fwra.Infrastructure, err, "github.GitStore: empty commit")
		}
		return plumbing.ZeroHash, fwra.Wrap(fwra.Infrastructure, err, "github.GitStore: commit")
	}
	return hash, nil
}

// readSubtreeAt reads every file under pathPrefix from the tree of the commit at
// `tip`, keyed by prefix-relative path.
func readSubtreeAt(repo *gogit.Repository, tip plumbing.Hash, pathPrefix string) (map[string][]byte, error) {
	commit, err := repo.CommitObject(tip)
	if err != nil {
		return nil, fwra.Wrap(fwra.Infrastructure, err, "github.GitStore: resolve commit")
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, fwra.Wrap(fwra.Infrastructure, err, "github.GitStore: resolve tree")
	}
	prefix := strings.TrimSuffix(pathPrefix, "/") + "/"
	out := map[string][]byte{}
	err = tree.Files().ForEach(func(f *object.File) error {
		if !strings.HasPrefix(f.Name, prefix) {
			return nil
		}
		contents, cerr := f.Contents()
		if cerr != nil {
			return cerr
		}
		out[strings.TrimPrefix(f.Name, prefix)] = []byte(contents)
		return nil
	})
	if err != nil {
		return nil, fwra.Wrap(fwra.Infrastructure, err, "github.GitStore: walk subtree")
	}
	return out, nil
}

// --- small filesystem + classification helpers ---------------------------------

// joinRepoPath joins a prefix and a relative path with a forward slash (git paths
// are always slash-separated regardless of host OS).
func joinRepoPath(prefix, rel string) string {
	rel = strings.TrimPrefix(rel, "/")
	if prefix == "" {
		return rel
	}
	return prefix + "/" + rel
}

// writeBillyFile (over-)writes a billy worktree file, creating parent dirs.
func writeBillyFile(fs billy.Filesystem, p string, data []byte) (retErr error) {
	if dir := pathDir(p); dir != "" {
		if err := fs.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := fs.Create(p)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && retErr == nil {
			retErr = cerr
		}
	}()
	_, retErr = io.Copy(f, bytes.NewReader(data))
	return
}

// ClassifyGitError maps a go-git transport/protocol fault onto the shared RA
// error model. A repository-not-found is terminal NotFound; an auth failure is
// terminal Auth (the credential was rejected/expired — routes to re-mint /
// intervention, contract REWORK.6 Q3); a recognised network blip is retryable
// Transient; anything else escalates as Infrastructure (also retryable, the
// conservative default). Bad caller input is caught as ContractMisuse before any
// git IO and never reaches here.
func ClassifyGitError(err error, op string) error {
	switch {
	case errors.Is(err, transport.ErrRepositoryNotFound):
		return fwra.Wrap(fwra.NotFound, err, op+": repository not found")
	case errors.Is(err, transport.ErrAuthenticationRequired),
		errors.Is(err, transport.ErrAuthorizationFailed):
		return fwra.Wrap(fwra.Auth, err, op+": auth failed")
	case isTransientGitError(err):
		return fwra.Wrap(fwra.Transient, err, op+": transient")
	default:
		return fwra.Wrap(fwra.Infrastructure, err, op)
	}
}

// isTransientGitError heuristically classifies network-level blips (connection
// refused/reset, timeout, EOF mid-stream) as retryable. go-git surfaces these
// without stable sentinels, so we match on the message; the unmatched default
// (Infrastructure) is itself retryable, so a miss never turns a retryable fault
// terminal.
func isTransientGitError(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"connection refused", "connection reset", "timeout", "i/o timeout",
		"no such host", "unexpected eof", "broken pipe", "network is unreachable",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// removeDirAll removes every file under dir in the billy worktree. A non-existent
// dir is not an error (nothing to clear).
func removeDirAll(fs billy.Filesystem, dir string) error {
	if dir == "" {
		return nil
	}
	entries, err := fs.ReadDir(dir)
	if err != nil {
		// Absent directory → nothing to clear.
		return nil
	}
	for _, e := range entries {
		if err := removeEntry(fs, dir, e.Name(), e.IsDir()); err != nil {
			return err
		}
	}
	return nil
}

func removeEntry(fs billy.Filesystem, dir, name string, isDir bool) error {
	full := dir + "/" + name
	if isDir {
		if err := removeDirAll(fs, full); err != nil {
			return err
		}
		_ = fs.Remove(full)
		return nil
	}
	return fs.Remove(full)
}

// pathDir returns the directory portion of a slash path ("" when none).
func pathDir(p string) string {
	i := strings.LastIndex(p, "/")
	if i < 0 {
		return ""
	}
	return p[:i]
}

// isNonFastForward reports whether a push error is the non-fast-forward rejection
// that signals a CAS loss (a concurrent writer advanced the ref).
func isNonFastForward(err error) bool {
	if errors.Is(err, gogit.ErrNonFastForwardUpdate) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "non-fast-forward") ||
		strings.Contains(msg, "fetch first") ||
		// Server-side receive-pack report-status for a rejected ref update. Over the
		// file/smart transports a losing CAS push surfaces as a per-command report
		// rather than the client-side ErrNonFastForwardUpdate sentinel; the ref-update
		// rejection IS the compare-and-swap loss.
		strings.Contains(msg, "failed to update ref") ||
		strings.Contains(msg, "command error on") ||
		strings.Contains(msg, "cannot lock ref") ||
		strings.Contains(msg, "stale info") ||
		strings.Contains(msg, "already exists") // first-writer race creating the branch
}
