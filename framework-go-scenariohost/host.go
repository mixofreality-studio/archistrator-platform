// Package scenariohost boots the real downstream stack a package of generated
// scenario tests runs against — a Temporal dev server, a Postgres
// testcontainer (outside -short), the GitHub REST and Actions fakes and a real
// bare git repo — ONCE per test process, and collects every scenario's verdict
// into test-results/<component>/results.json, the TestRun contract the testing
// task renders. Verdicts are keyed by (component, scenario): several
// components (facets) may share one Go package and so one test process, and
// each still gets its own index.
//
// It is its own module, not a framework-go package, because every infra module
// it boots imports framework-go. TEST-ONLY: nothing here is imported by
// production code.
//
// Lifetime: the doubles belong to the PROCESS, not to the first test that
// happened to call Start — a sub-test's cleanups run when that sub-test ends,
// and a stack bound to them would be dead for every later test. Main (the
// generated TestMain) flushes results and tears the stack down after m.Run.
package scenariohost

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	ghtestinfra "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github/testinfra"
	fwpg "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-postgres"
	temporalinfra "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-temporal/testinfra"
)

// postgresImage pins the Postgres image, matching
// framework-go-infrastructure-postgres/testinfra so a scenario sees the same
// database a component's own integration tests see.
const postgresImage = "postgres:16-alpine"

// resultsRoot is the directory, under the consuming module root, every
// component's results.json lands in; the venue uploads test-results/** whole.
const resultsRoot = "test-results"

// Host is the shared downstream stack a package of scenario tests runs
// against, plus the results collector for every component the package tests.
type Host struct {
	TemporalHostPort  string
	TemporalNamespace string
	PostgresURL       string // "" when -short
	GitHub            *ghtestinfra.FakeGitHub
	Actions           *ghtestinfra.FakeActions
	Repo              ghtestinfra.LocalGitRepo
	ResultsRoot       string // <module root>/test-results; ResultsDir(c) is ResultsRoot/<c>

	Results

	// stops tears the doubles down, in boot order; Main runs them reversed.
	stops []func()
}

var (
	once     sync.Once
	shared   *Host
	startErr error
)

// Start boots every downstream double once per package (sync.Once) and
// registers t.Cleanup so the verdicts recorded so far are flushed when t ends.
// Every test in the package receives the same *Host — whichever component it
// tests; the component travels with each verdict (RunScenario). A boot
// failure fails every caller, not just the first.
func Start(t *testing.T) *Host {
	t.Helper()
	once.Do(func() { shared, startErr = boot(context.Background()) })
	if startErr != nil {
		t.Fatalf("scenariohost: %v", startErr)
	}
	t.Cleanup(func() {
		if err := shared.Flush(); err != nil {
			t.Errorf("scenariohost: %v", err)
		}
	})
	return shared
}

// Main is the body of a generated TestMain: it runs the package's tests, writes
// one results.json per component the package tests (an empty index for a
// component none of whose scenarios ran) and tears the shared stack down, then
// exits with m.Run's code. components lists every contract whose generated
// scenarios live in the package — one, or several facets sharing it.
func Main(m *testing.M, components ...string) {
	if len(components) == 0 || slices.Contains(components, "") {
		fmt.Fprintf(os.Stderr, "scenariohost: Main needs the package's non-empty component names, got %q\n", components)
		os.Exit(2)
	}
	code := m.Run()
	h := shared
	if h == nil {
		h = newHost()
	}
	h.declare(components...)
	if err := h.Flush(); err != nil {
		fmt.Fprintf(os.Stderr, "scenariohost: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	for i := len(h.stops) - 1; i >= 0; i-- {
		h.stops[i]()
	}
	os.Exit(code)
}

// Context returns the context a scenario step calls the component with: the
// test's own, cancelled when the test ends.
func (h *Host) Context(t *testing.T) context.Context {
	t.Helper()
	return t.Context()
}

func newHost() *Host {
	return &Host{
		ResultsRoot:       filepath.Join(moduleRoot(), resultsRoot),
		TemporalNamespace: temporalinfra.TemporalNamespace,
	}
}

// boot starts the doubles in dependency-free order; on any failure the ones
// already up are stopped and the error names the one that did not come up.
func boot(ctx context.Context) (*Host, error) {
	h := newHost()
	fail := func(err error) (*Host, error) {
		for i := len(h.stops) - 1; i >= 0; i-- {
			h.stops[i]()
		}
		return nil, err
	}

	dev, err := temporalinfra.StartDevServer(ctx)
	if err != nil {
		return fail(fmt.Errorf("temporal: %w", err))
	}
	h.stops = append(h.stops, func() { _ = dev.Stop() })
	h.TemporalHostPort = dev.FrontendHostPort()

	if !testing.Short() {
		url, stop, pgErr := startPostgres(ctx)
		if pgErr != nil {
			return fail(fmt.Errorf("postgres: %w", pgErr))
		}
		h.stops = append(h.stops, stop)
		h.PostgresURL = url
	}

	h.GitHub = ghtestinfra.Start()
	h.stops = append(h.stops, h.GitHub.Close)
	h.Actions = ghtestinfra.StartActions()
	h.stops = append(h.stops, h.Actions.Close)

	repo, stop, repoErr := startLocalGitRepo("main")
	if repoErr != nil {
		return fail(fmt.Errorf("git: %w", repoErr))
	}
	h.stops = append(h.stops, stop)
	h.Repo = repo

	if mkErr := os.MkdirAll(h.ResultsRoot, 0o750); mkErr != nil {
		return fail(fmt.Errorf("mkdir %s: %w", h.ResultsRoot, mkErr))
	}
	return h, nil
}

// startPostgres spins the same throwaway container
// framework-go-infrastructure-postgres/testinfra.StartPostgres does, but with a
// process lifetime (that helper binds the container to one test's cleanup).
// The returned URL is verified reachable before it is handed out.
func startPostgres(ctx context.Context) (url string, stop func(), err error) {
	container, err := postgres.Run(ctx, postgresImage,
		postgres.WithDatabase("aiarch_test"),
		postgres.WithUsername("aiarch"),
		postgres.WithPassword("aiarch"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		return "", nil, fmt.Errorf("start container: %w", err)
	}
	stop = func() { _ = testcontainers.TerminateContainer(container) }

	// sslmode=disable: the throwaway container has no TLS.
	url, err = container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		stop()
		return "", nil, fmt.Errorf("connection string: %w", err)
	}
	pool, err := fwpg.NewPool(ctx, url)
	if err != nil {
		stop()
		return "", nil, fmt.Errorf("open pool: %w", err)
	}
	pool.Close()
	return url, stop, nil
}

// startLocalGitRepo creates a bare repo with an initial empty commit on
// branch, addressed by a file:// URL — what
// framework-go-infrastructure-github/testinfra.StartLocalGitRepo does, with a
// process lifetime instead of one test's t.TempDir. The repo is written with
// go-git (no git subprocess), so it is a plain on-disk repository the local
// `git` the consumer's file transport shells out to reads as its own.
func startLocalGitRepo(branch string) (ghtestinfra.LocalGitRepo, func(), error) {
	var none ghtestinfra.LocalGitRepo
	root, err := os.MkdirTemp("", "scenariohost-git-")
	if err != nil {
		return none, nil, err
	}
	stop := func() { _ = os.RemoveAll(root) }
	bare := filepath.Join(root, "remote.git")
	if err := seedBareRepo(bare, branch); err != nil {
		stop()
		return none, nil, err
	}
	return ghtestinfra.LocalGitRepo{Dir: bare, URL: "file://" + bare}, stop, nil
}

// seedBareRepo initialises a bare repo at path whose HEAD is branch, and
// points branch at one empty commit so a clone sees a real tip to
// compare-and-swap against.
func seedBareRepo(path, branch string) error {
	ref := plumbing.NewBranchReferenceName(branch)
	repo, err := git.PlainInitWithOptions(path, &git.PlainInitOptions{
		InitOptions: git.InitOptions{DefaultBranch: ref},
		Bare:        true,
	})
	if err != nil {
		return fmt.Errorf("init bare repo: %w", err)
	}
	treeHash, err := storeObject(repo.Storer, &object.Tree{})
	if err != nil {
		return fmt.Errorf("seed tree: %w", err)
	}
	seed := object.Signature{Name: "seed", Email: "seed@aiarch.local", When: time.Unix(0, 0).UTC()}
	commitHash, err := storeObject(repo.Storer, &object.Commit{
		Author: seed, Committer: seed, Message: "seed\n", TreeHash: treeHash,
	})
	if err != nil {
		return fmt.Errorf("seed commit: %w", err)
	}
	if err := repo.Storer.SetReference(plumbing.NewHashReference(ref, commitHash)); err != nil {
		return fmt.Errorf("set %s: %w", ref, err)
	}
	return nil
}

// encodable is what go-git's tree and commit objects share: they serialise
// themselves into a storer-allocated encoded object.
type encodable interface {
	Encode(plumbing.EncodedObject) error
}

func storeObject(s storer.EncodedObjectStorer, o encodable) (plumbing.Hash, error) {
	enc := s.NewEncodedObject()
	if err := o.Encode(enc); err != nil {
		return plumbing.ZeroHash, err
	}
	return s.SetEncodedObject(enc)
}

// moduleRoot walks up from the working directory to the directory holding
// go.mod (the consuming module root), so test-results/** is one tree at the
// module root whichever package's tests are running. Without a go.mod above,
// the working directory itself is the root.
func moduleRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for d := dir; ; {
		if _, statErr := os.Stat(filepath.Join(d, "go.mod")); statErr == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return dir
		}
		d = parent
	}
}
