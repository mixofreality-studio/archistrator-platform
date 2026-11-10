package scenariohost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	fwpg "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-postgres"
)

// The arrange primitives (first-pass-correctness design W2, ruling 10): the
// three typed setup actions a declarative test binding may use besides a
// contract call - move the clock, make the next call of an operation fail,
// load a fixture into a resource. An agent never writes this code; a generated
// test calls these with the values of a schema-complete ArrangeCall record.

// arrangeState is the mutable part of the host the primitives share: the test
// clock's offset and the registered one-shot faults.
type arrangeState struct {
	mu          sync.Mutex
	clockOffset time.Duration
	faults      []fault
	nextFaultID int
}

// fault is one registered one-shot failure, owned by the test that registered it.
type fault struct {
	id                       int
	component, op, errorCode string
}

// Clock is the time source generated components read in a scenario: real time
// plus whatever the scenario advanced it by (AdvanceClock). It satisfies
// framework-go/utilities/security.Clock and any other Now() time source.
type Clock interface{ Now() time.Time }

// Now is the host clock's reading: real time shifted by every AdvanceClock the
// running scenario made.
func (h *Host) Now() time.Time {
	h.arrange.mu.Lock()
	defer h.arrange.mu.Unlock()
	return time.Now().Add(h.arrange.clockOffset)
}

// Clock is the host clock as a time source a generated component takes.
func (h *Host) Clock() Clock { return h }

// AdvanceClock moves the host clock forward by d for the rest of t. The shift
// is undone when t ends, so the next scenario of the process starts at real
// time. A negative d fails t: a scenario moves time forward, never back.
//
// Only the host clock moves. The Temporal dev server the host boots runs in
// real time (the Go SDK's dev-server wrapper has no time-skipping mode), so a
// workflow timer is not fired by this call; what a scenario can age is whatever
// reads Host.Now - the RA contexts the generated constructors build.
func (h *Host) AdvanceClock(t *testing.T, d time.Duration) {
	t.Helper()
	if d < 0 {
		t.Fatalf("scenariohost: AdvanceClock: negative duration %v", d)
	}
	h.arrange.mu.Lock()
	h.arrange.clockOffset += d
	h.arrange.mu.Unlock()
	t.Cleanup(func() {
		h.arrange.mu.Lock()
		h.arrange.clockOffset -= d
		h.arrange.mu.Unlock()
	})
}

// FailNext registers a one-shot fault: the next call of component's op fails
// with errorCode. Faults on one (component, op) fire in registration order, one
// per call; whatever t registered and nothing consumed is dropped when t ends.
// The generated constructors wrap every component with an interceptor that asks
// TakeFault before each call.
func (h *Host) FailNext(t *testing.T, component, op, errorCode string) {
	t.Helper()
	if component == "" || op == "" || errorCode == "" {
		t.Fatalf("scenariohost: FailNext needs a component, an operation and an error code (got %q, %q, %q)", component, op, errorCode)
	}
	h.arrange.mu.Lock()
	h.arrange.nextFaultID++
	id := h.arrange.nextFaultID
	h.arrange.faults = append(h.arrange.faults, fault{id: id, component: component, op: op, errorCode: errorCode})
	h.arrange.mu.Unlock()
	t.Cleanup(func() {
		h.arrange.mu.Lock()
		defer h.arrange.mu.Unlock()
		h.arrange.faults = slices.DeleteFunc(h.arrange.faults, func(f fault) bool { return f.id == id })
	})
}

// TakeFault consumes the oldest fault registered for (component, op) and
// returns its error code; ok is false when none is pending. It is the fault
// interceptor's half of FailNext.
func (h *Host) TakeFault(component, op string) (errorCode string, ok bool) {
	h.arrange.mu.Lock()
	defer h.arrange.mu.Unlock()
	i := slices.IndexFunc(h.arrange.faults, func(f fault) bool { return f.component == component && f.op == op })
	if i < 0 {
		return "", false
	}
	errorCode = h.arrange.faults[i].errorCode
	h.arrange.faults = slices.Delete(h.arrange.faults, i, i+1)
	return errorCode, true
}

// The resource kinds SeedResource dispatches on.
const (
	resourcePostgres = "postgres"
	resourceGitHub   = "github"
	resourceGitRepo  = "gitRepo"
)

// SeedResource loads fixture into the resource of the given kind:
//
//	postgres  {"sql": "<statements>"}  executed on the host's database
//	github    {"owner","name","description","topics","private","files":{path:content}}
//	          a repo (and its files) in the GitHub double's catalog
//	gitRepo   {"branch","message","files":{path:content}}
//	          one commit adding the files on top of the local repo's branch
//
// An unknown kind, a fixture that does not decode strictly, or a resource the
// host did not boot fails t: a fixture that silently seeded nothing would let a
// scenario assert against an empty world.
func (h *Host) SeedResource(t *testing.T, resource string, fixture json.RawMessage) {
	t.Helper()
	if err := h.seedResource(t.Context(), resource, fixture); err != nil {
		t.Fatalf("scenariohost: SeedResource %q: %v", resource, err)
	}
}

func (h *Host) seedResource(ctx context.Context, resource string, fixture json.RawMessage) error {
	switch resource {
	case resourcePostgres:
		return h.seedPostgres(ctx, fixture)
	case resourceGitHub:
		return h.seedGitHub(fixture)
	case resourceGitRepo:
		return h.seedGitRepo(fixture)
	default:
		return fmt.Errorf("unknown resource kind %q (want %s, %s or %s)", resource, resourcePostgres, resourceGitHub, resourceGitRepo)
	}
}

// decodeFixture decodes fixture into v, refusing unknown fields and trailing data.
func decodeFixture(fixture json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(fixture))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("fixture: %w", err)
	}
	if dec.More() {
		return fmt.Errorf("fixture: trailing data after the JSON value")
	}
	return nil
}

func (h *Host) seedPostgres(ctx context.Context, fixture json.RawMessage) error {
	var f struct {
		SQL string `json:"sql"`
	}
	if err := decodeFixture(fixture, &f); err != nil {
		return err
	}
	if f.SQL == "" {
		return fmt.Errorf("fixture: sql is empty")
	}
	if h.PostgresURL == "" {
		return fmt.Errorf("the host has no Postgres (booted with -short)")
	}
	pool, err := fwpg.NewPool(ctx, h.PostgresDSN())
	if err != nil {
		return fmt.Errorf("open pool: %w", err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, f.SQL); err != nil {
		return fmt.Errorf("exec fixture sql: %w", err)
	}
	return nil
}

func (h *Host) seedGitHub(fixture json.RawMessage) error {
	var f struct {
		Owner       string            `json:"owner"`
		Name        string            `json:"name"`
		Description string            `json:"description"`
		Topics      []string          `json:"topics"`
		Private     bool              `json:"private"`
		Files       map[string]string `json:"files"`
	}
	if err := decodeFixture(fixture, &f); err != nil {
		return err
	}
	if f.Owner == "" || f.Name == "" {
		return fmt.Errorf("fixture: owner and name are required")
	}
	if h.GitHub == nil {
		return fmt.Errorf("the host has no GitHub double")
	}
	h.GitHub.SeedRepo(f.Owner, f.Name, f.Description, f.Topics, f.Private)
	for _, p := range sortedKeys(f.Files) {
		h.GitHub.SeedRepoFile(f.Owner, f.Name, p, []byte(f.Files[p]))
	}
	return nil
}

type gitRepoFixture struct {
	Branch  string            `json:"branch"`
	Message string            `json:"message"`
	Files   map[string]string `json:"files"`
}

func decodeGitRepoFixture(fixture json.RawMessage) (gitRepoFixture, error) {
	var f gitRepoFixture
	if err := decodeFixture(fixture, &f); err != nil {
		return f, err
	}
	if len(f.Files) == 0 {
		return f, fmt.Errorf("fixture: files is empty")
	}
	if f.Branch == "" {
		f.Branch = "main"
	}
	if f.Message == "" {
		f.Message = "arrange"
	}
	return f, nil
}

func (h *Host) seedGitRepo(fixture json.RawMessage) error {
	f, err := decodeGitRepoFixture(fixture)
	if err != nil {
		return err
	}
	if h.Repo.URL == "" {
		return fmt.Errorf("the host has no local repo")
	}
	work, err := os.MkdirTemp("", "scenariohost-seed-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(work) }()

	branch := plumbing.NewBranchReferenceName(f.Branch)
	repo, err := git.PlainClone(work, false, &git.CloneOptions{URL: h.Repo.URL, ReferenceName: branch, SingleBranch: true})
	if err != nil {
		return fmt.Errorf("clone %s: %w", h.Repo.URL, err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		return err
	}
	if err := writeAndStageFiles(work, wt, f.Files); err != nil {
		return err
	}
	return commitAndPush(repo, wt, f.Message, f.Branch)
}

func writeAndStageFiles(work string, wt *git.Worktree, files map[string]string) error {
	for _, p := range sortedKeys(files) {
		if !filepath.IsLocal(filepath.FromSlash(p)) {
			return fmt.Errorf("fixture: file path %q escapes the repo", p)
		}
		abs := filepath.Join(work, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(abs, []byte(files[p]), 0o600); err != nil {
			return err
		}
		if _, err := wt.Add(filepath.ToSlash(p)); err != nil {
			return fmt.Errorf("stage %s: %w", p, err)
		}
	}
	return nil
}

func commitAndPush(repo *git.Repository, wt *git.Worktree, message, branch string) error {
	sig := &object.Signature{Name: "arrange", Email: "arrange@aiarch.local", When: time.Now()}
	if _, err := wt.Commit(message, &git.CommitOptions{Author: sig, Committer: sig}); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	if err := repo.Push(&git.PushOptions{RemoteName: "origin"}); err != nil {
		return fmt.Errorf("push %s: %w", branch, err)
	}
	return nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
