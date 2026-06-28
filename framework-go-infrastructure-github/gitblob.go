package github

// gitblob.go carries the CONTENT-ADDRESSABLE blob/tree wire plumbing the
// artifact ResourceAccess (artifactAccess, C-AA-R) needs to persist PHASE-3
// CONSTRUCTION OUTPUTS into the per-project git repo. It is the satellite home
// for the store-blob / read-blob / read-tree operations the contract's §6
// content-addressable mapping requires — kept HERE (the github satellite), out
// of the product RA, exactly as the App-JWT / installation-token / git-data
// CAS code is, so the RA stays provider-opaque.
//
// PROVIDER-NEUTRAL GIT PLUMBING: although this module is named for GitHub, the
// path is pure git (go-git): clone a remote, write a small file set onto a
// CONTENT-DERIVED branch, commit with a FIXED author/committer/time so the
// commit object hash is a pure function of (content, message, tree), and push
// the single branch plain (never --force). It works against ANY git remote the
// URL addresses — github.com over HTTPS in production (auth via GitAuth.Token),
// a local on-disk repo over the `file://` transport in the C-AA-R regression
// harness and the LOCAL deployment profile (GitAuth.Local), or a Gitea host.
//
// CONTENT-ADDRESSABILITY + DEDUP: git is natively content-addressable. Distinct
// content lands on a distinct content-derived branch (never contends); identical
// content collapses to the SAME branch tip and the SAME commit hash (the fixed
// author/time keeps the hash a pure function of the tree), so a re-store of
// byte-identical content produces NO new object and returns the existing commit
// hash. The dedup probe reads the content-derived branch tip and short-circuits
// when the stored bytes already match.
//
// PROVIDER-OPACITY: the value types this file exposes (GitBlobStore, GitObjectFile)
// carry NO GitHub lexeme on their contract role — the returned commit hash is an
// opaque string the consuming RA folds into its own content-address scheme; no
// owner/repo, no installation-token format, no "blob"/"tree" leaks onto the RA's
// surface. The consuming RA threads a provider-neutral GitAuth credential.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sort"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// gitBlobAuthorName / gitBlobAuthorEmail stamp construction-output commits. They
// are FIXED (and the commit time is fixed below) so that the SAME content yields
// the SAME commit hash across retries — the content-addressable invariant depends
// on a deterministic author/committer/time.
const (
	gitBlobAuthorName  = "aiarch"
	gitBlobAuthorEmail = "aiarch@artifactaccess.local"
)

// gitBlobCommitTime is the deterministic commit timestamp. Using a fixed time
// (rather than time.Now) keeps the commit hash a pure function of (content,
// message, tree, author) so identical re-stores collapse to one object — a
// wall-clock time would make every commit unique and defeat the invariant.
var gitBlobCommitTime = time.Unix(0, 0).UTC()

// GitBlobStore is the satellite's provider-neutral content-addressable handle for
// one repo. It holds only the remote URL; every call clones fresh (stateless, so
// it is safe for concurrent callers and replays cleanly under Temporal retry). It
// performs NO IO at construction.
type GitBlobStore struct {
	repoURL string
}

// NewGitBlobStore builds a content-addressable git handle over the repo reachable
// at repoURL. No IO.
func NewGitBlobStore(repoURL string) (*GitBlobStore, error) {
	if strings.TrimSpace(repoURL) == "" {
		return nil, fwra.New(fwra.ContractMisuse, "github.NewGitBlobStore: empty repoURL")
	}
	return &GitBlobStore{repoURL: repoURL}, nil
}

// GitObjectFile is one file in a content-output commit: a slash-separated repo
// path and its bytes. The consuming RA decides what each file means (a content
// payload, a sidecar metadata file, …); the satellite only writes/reads bytes.
type GitObjectFile struct {
	Path  string
	Bytes []byte
}

// StoreOutput writes `files` onto the content-derived branch `branch`, commits
// with `message` (the RA embeds its idempotency key), and pushes plain (never
// --force). It returns the commit hash as an OPAQUE string the RA folds into its
// content-address scheme.
//
// DEDUP: the probe path is the RA's responsibility via ReadFileAtCommit against
// the branch tip; StoreOutput additionally treats an empty commit (go-git
// ErrEmptyCommit — the new tree is identical to the branch tip) as a benign
// content-address HIT by returning the existing tip hash, so a same-content store
// never produces a duplicate even if the RA's probe raced.
//
// A non-fast-forward push rejection means a concurrent writer advanced the SAME
// content branch between clone and push — surfaced as fwra.Conflict (retryable;
// the calling Manager re-runs the Activity, which re-clones and dedups).
func (s *GitBlobStore) StoreOutput(
	ctx context.Context,
	branch string,
	files []GitObjectFile,
	message string,
	auth GitAuth,
) (commitHash string, err error) {
	repo, err := s.clone(ctx, auth)
	if err != nil {
		return "", err
	}

	branchRef := plumbing.NewBranchReferenceName(branch)
	remoteRef := plumbing.NewRemoteReferenceName("origin", branch)
	priorTip, branchExists := resolveRef(repo, remoteRef)

	wt, err := repo.Worktree()
	if err != nil {
		return "", fwra.Wrap(fwra.Infrastructure, err, "github.GitBlobStore.StoreOutput: worktree")
	}

	// Base the write on the content-derived branch tip when it exists (so the push
	// is a fast-forward and identical content collapses to the existing commit);
	// otherwise base on the default branch HEAD the clone provides.
	co := &gogit.CheckoutOptions{Branch: branchRef, Create: true, Force: true}
	if branchExists {
		co.Hash = priorTip
	}
	if coErr := wt.Checkout(co); coErr != nil {
		return "", fwra.Wrap(fwra.Infrastructure, coErr, "github.GitBlobStore.StoreOutput: checkout branch")
	}

	if err := writeAndStageFiles(wt, files); err != nil {
		return "", err
	}

	return commitAndPushBlobContent(ctx, repo, wt, message, branchRef, priorTip, branchExists, auth)
}

func commitAndPushBlobContent(ctx context.Context, repo *gogit.Repository, wt *gogit.Worktree, message string, branchRef plumbing.ReferenceName, priorTip plumbing.Hash, branchExists bool, auth GitAuth) (string, error) {
	sig := &object.Signature{Name: gitBlobAuthorName, Email: gitBlobAuthorEmail, When: gitBlobCommitTime}
	hash, cerr := wt.Commit(message, &gogit.CommitOptions{Author: sig, Committer: sig})
	if cerr != nil {
		// ErrEmptyCommit: the new tree equals the branch tip — same content already
		// stored. The existing tip IS the content address.
		if errors.Is(cerr, gogit.ErrEmptyCommit) && branchExists {
			return priorTip.String(), nil
		}
		return "", fwra.Wrap(fwra.Infrastructure, cerr, "github.GitBlobStore.StoreOutput: commit")
	}
	pushErr := repo.PushContext(ctx, &gogit.PushOptions{
		Auth:     auth.authMethod(),
		RefSpecs: []config.RefSpec{config.RefSpec(string(branchRef) + ":" + string(branchRef))},
	})
	if pushErr != nil && !errors.Is(pushErr, gogit.NoErrAlreadyUpToDate) {
		if isNonFastForward(pushErr) {
			return "", fwra.Wrap(fwra.Conflict, pushErr,
				"github.GitBlobStore.StoreOutput: concurrent content-branch advance (non-fast-forward)")
		}
		return "", ClassifyGitError(pushErr, "github.GitBlobStore.StoreOutput: push")
	}
	return hash.String(), nil
}

func writeAndStageFiles(wt *gogit.Worktree, files []GitObjectFile) error {
	billyFS := wt.Filesystem
	// Deterministic write order keeps the tree (and thus the commit hash) stable.
	ordered := append([]GitObjectFile(nil), files...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })
	for _, f := range ordered {
		if werr := writeBillyFile(billyFS, f.Path, f.Bytes); werr != nil {
			return fwra.Wrap(fwra.Infrastructure, werr, "github.GitBlobStore.StoreOutput: write "+f.Path)
		}
		if _, aerr := wt.Add(f.Path); aerr != nil {
			return fwra.Wrap(fwra.Infrastructure, aerr, "github.GitBlobStore.StoreOutput: stage "+f.Path)
		}
	}
	return nil
}

// ReadFileAtCommit reads one file's bytes from the tree of the commit at
// `commitHash`. An unknown commit, or a missing path within it, surfaces as
// fwra.NotFound (the most common terminal error per the contract). Commit hashes
// are immutable, so the read is deterministic regardless of later writes.
func (s *GitBlobStore) ReadFileAtCommit(ctx context.Context, commitHash, path string, auth GitAuth) ([]byte, error) {
	repo, err := s.clone(ctx, auth)
	if err != nil {
		return nil, err
	}
	commit, err := s.resolveCommit(repo, commitHash, "github.GitBlobStore.ReadFileAtCommit")
	if err != nil {
		return nil, err
	}
	return readFileFromCommitObj(commit, path)
}

// ProbeFileAtBranchTip reads one file from the tip of the content-derived branch
// (the dedup probe). found=false (with a nil error) means the branch does not
// exist or holds no such file yet — not a hit, not an error. On a HIT it returns
// the bytes and the tip commit hash so the RA can build the existing content
// address without a second clone.
func (s *GitBlobStore) ProbeFileAtBranchTip(ctx context.Context, branch, path string, auth GitAuth) (data []byte, tipHash string, found bool, err error) {
	repo, err := s.clone(ctx, auth)
	if err != nil {
		return nil, "", false, err
	}
	remoteRef := plumbing.NewRemoteReferenceName("origin", branch)
	tip, exists := resolveRef(repo, remoteRef)
	if !exists {
		return nil, "", false, nil
	}
	commit, cerr := repo.CommitObject(tip)
	if cerr != nil {
		return nil, "", false, fwra.Wrap(fwra.Infrastructure, cerr, "github.GitBlobStore.ProbeFileAtBranchTip: resolve commit")
	}
	b, rerr := readFileFromCommitObj(commit, path)
	if rerr != nil {
		var fe *fwra.Error
		if errors.As(rerr, &fe) && fe.Kind == fwra.NotFound {
			return nil, "", false, nil
		}
		return nil, "", false, rerr
	}
	return b, tip.String(), true, nil
}

// WalkTreeFiles flattens the tree of the commit at `commitHash` into the sorted
// list of its file paths (slash-separated). An unknown commit surfaces as
// fwra.NotFound. The RA maps each path to a per-entry content address.
func (s *GitBlobStore) WalkTreeFiles(ctx context.Context, commitHash string, auth GitAuth) ([]string, error) {
	repo, err := s.clone(ctx, auth)
	if err != nil {
		return nil, err
	}
	commit, err := s.resolveCommit(repo, commitHash, "github.GitBlobStore.WalkTreeFiles")
	if err != nil {
		return nil, err
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, fwra.Wrap(fwra.Infrastructure, err, "github.GitBlobStore.WalkTreeFiles: resolve tree")
	}
	var paths []string
	walker := object.NewTreeWalker(tree, true, nil)
	defer walker.Close()
	for {
		name, entry, werr := walker.Next()
		if errors.Is(werr, io.EOF) {
			break
		}
		if werr != nil {
			return nil, fwra.Wrap(fwra.Infrastructure, werr, "github.GitBlobStore.WalkTreeFiles: walk tree")
		}
		if entry.Mode.IsFile() {
			paths = append(paths, name)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

// clone fetches the full repo into in-memory storage (no on-disk worktree). A
// full clone lets the store see every content-derived branch (for the dedup probe)
// and resolve an arbitrary commit on read, in one round-trip.
func (s *GitBlobStore) clone(ctx context.Context, auth GitAuth) (*gogit.Repository, error) {
	repo, err := gogit.CloneContext(ctx, memory.NewStorage(), memfs.New(), &gogit.CloneOptions{
		URL:  s.repoURL,
		Auth: auth.authMethod(),
	})
	if err != nil {
		return nil, ClassifyGitError(err, "github.GitBlobStore.clone")
	}
	return repo, nil
}

// resolveCommit resolves a hex commit hash to its commit object, mapping an
// unknown object to fwra.NotFound.
func (s *GitBlobStore) resolveCommit(repo *gogit.Repository, hash, op string) (*object.Commit, error) {
	commit, err := repo.CommitObject(plumbing.NewHash(hash))
	if err != nil {
		if errors.Is(err, plumbing.ErrObjectNotFound) {
			return nil, fwra.Wrap(fwra.NotFound, err, op+": commit not found")
		}
		return nil, fwra.Wrap(fwra.Infrastructure, err, op+": resolve commit")
	}
	return commit, nil
}

// resolveRef resolves refName to its commit hash. ok=false means the ref does not
// exist (e.g. a content-derived branch never written).
func resolveRef(repo *gogit.Repository, refName plumbing.ReferenceName) (plumbing.Hash, bool) {
	ref, err := repo.Reference(refName, true)
	if err != nil {
		return plumbing.ZeroHash, false
	}
	return ref.Hash(), true
}

// readFileFromCommitObj reads one tree entry's bytes from a commit. A missing path
// surfaces as fwra.NotFound.
func readFileFromCommitObj(commit *object.Commit, p string) ([]byte, error) {
	tree, err := commit.Tree()
	if err != nil {
		return nil, fwra.Wrap(fwra.Infrastructure, err, "github.GitBlobStore: resolve tree")
	}
	f, err := tree.File(p)
	if err != nil {
		if errors.Is(err, object.ErrFileNotFound) || errors.Is(err, object.ErrEntryNotFound) {
			return nil, fwra.Wrap(fwra.NotFound, err, "github.GitBlobStore: file not found at "+p)
		}
		return nil, fwra.Wrap(fwra.Infrastructure, err, "github.GitBlobStore: resolve file")
	}
	reader, err := f.Reader()
	if err != nil {
		return nil, fwra.Wrap(fwra.Infrastructure, err, "github.GitBlobStore: open file")
	}
	defer func() { _ = reader.Close() }()
	contents, err := io.ReadAll(reader)
	if err != nil {
		return nil, fwra.Wrap(fwra.Infrastructure, err, "github.GitBlobStore: read contents")
	}
	return bytes.Clone(contents), nil
}
