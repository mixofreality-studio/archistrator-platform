// Package testinfra provides an in-process Temporal dev-server bootstrap and
// workflow-history dumper for the integration tests of any aiarch-built system.
// It is TEST-ONLY: nothing here is imported by production code (the production
// Temporal bridge lives in framework-go/manager). StartDevServer is meant to be
// called from TestMain and is skipped in the fast path by the caller's own
// -short guard.
//
// Temporal is the FIXED durable-execution substrate for aiarch Managers (see the
// WorkflowRuntime volatility in the Method design and the dependency allowlist
// enforced by framework-go/arch).
package testinfra

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/testsuite"
	"google.golang.org/protobuf/encoding/protojson"

	enumspb "go.temporal.io/api/enums/v1"
)

// TemporalNamespace is the FIXED namespace every integration workflow runs in. A
// fixed namespace (not the ephemeral default) is what makes the persisted SQLite
// DB browsable in the UI after the run: every test execution lands in the same
// namespace so a UI re-serve can list them all.
const TemporalNamespace = "aiarch-test"

// TemporalDBFile is the FIXED, PERSISTENT SQLite DB the dev server writes to,
// relative to the consuming module root. It survives the test run (NOT the
// default ephemeral in-memory DB) so the full event history of every test
// workflow can be browsed in the Temporal UI afterwards.
const TemporalDBFile = ".temporal/aiarch-test.db"

// TemporalArtifactDir is where per-test workflow event-history JSON artifacts are
// written (one file per integration test, "like playwright"). Loadable /
// replayable: the shape matches `temporal workflow show -o json`.
const TemporalArtifactDir = "test-artifacts/temporal"

// DevServer wraps a shared testsuite dev server plus the resolved module root so
// helpers can write artifacts and the persistent DB at stable, repo-relative
// paths regardless of the per-package test working directory.
type DevServer struct {
	srv        *testsuite.DevServer
	moduleRoot string
}

// StartDevServer boots ONE Temporal CLI dev server in-process for the whole test
// package (call it from TestMain, share the handle). It is configured for the
// "run the tests, then SEE them in a UI" requirement:
//
//   - PERSISTENT SQLite at <moduleRoot>/.temporal/aiarch-test.db (DBFilename),
//     so every workflow execution + full event history survives the run.
//   - FIXED namespace aiarch-test, so all executions are listable in one place.
//   - headless here (EnableUI:false) to keep the test process light; the SAME DB
//     is re-served WITH the UI by a separate script.
//
// The dev server binary is the Temporal CLI, downloaded once from
// temporal.download and cached; the UI script reuses that exact cached binary.
//
// The caller is responsible for Stop() in TestMain teardown.
func StartDevServer(ctx context.Context) (*DevServer, error) {
	moduleRoot, err := findModuleRoot()
	if err != nil {
		return nil, err
	}

	dbPath := filepath.Join(moduleRoot, TemporalDBFile)
	if mkErr := os.MkdirAll(filepath.Dir(dbPath), 0o755); mkErr != nil {
		return nil, fmt.Errorf("testinfra.StartDevServer: mkdir .temporal: %w", mkErr)
	}

	opts := testsuite.DevServerOptions{
		ClientOptions: &client.Options{Namespace: TemporalNamespace},
		DBFilename:    dbPath,
		// Headless in-test; the UI is launched separately against the same DB.
		EnableUI: false,
		LogLevel: "error",
	}
	// Use a locally-installed Temporal CLI when available (env override, then PATH)
	// instead of the SDK's first-use download from temporal.download. This keeps
	// the embedded dev server working in environments where that download is
	// blocked, and reuses the SAME binary the UI script launches with. When no
	// local binary is found, fall back to the SDK's cached download.
	if bin := resolveTemporalCLI(); bin != "" {
		opts.ExistingPath = bin
	}

	srv, err := testsuite.StartDevServer(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("testinfra.StartDevServer: %w", err)
	}
	return &DevServer{srv: srv, moduleRoot: moduleRoot}, nil
}

// resolveTemporalCLI returns a path to a local Temporal CLI binary, or "" if none
// is found (the SDK then downloads/caches one). Order: TEMPORAL_CLI_PATH env, then
// a `temporal` on PATH.
func resolveTemporalCLI() string {
	if p := strings.TrimSpace(os.Getenv("TEMPORAL_CLI_PATH")); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("temporal"); err == nil {
		return p
	}
	return ""
}

// Client returns the dev server's Temporal client (already bound to the fixed
// namespace).
func (d *DevServer) Client() client.Client { return d.srv.Client() }

// FrontendHostPort returns the host:port of the dev server frontend.
func (d *DevServer) FrontendHostPort() string { return d.srv.FrontendHostPort() }

// Stop terminates the dev server. The persistent DB file is intentionally left in
// place so the UI can browse it after the run.
func (d *DevServer) Stop() error { return d.srv.Stop() }

// DumpHistory writes the full event history of one workflow execution to
// <moduleRoot>/test-artifacts/temporal/<sanitized-test-name>.json. The JSON shape
// is a History envelope ({"events":[...]}) matching `temporal workflow show -o
// json`, so the artifact is loadable/replayable. Call this at the end of each
// integration test (e.g. via t.Cleanup or directly) so a failed or passing run
// leaves an inspectable trace, "like playwright".
func (d *DevServer) DumpHistory(ctx context.Context, t *testing.T, workflowID, runID string) {
	t.Helper()

	hist := &historypb.History{}
	iter := d.Client().GetWorkflowHistory(ctx, workflowID, runID, false,
		enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	for iter.HasNext() {
		ev, err := iter.Next()
		if err != nil {
			t.Logf("testinfra.DumpHistory: iterate history for %s: %v", workflowID, err)
			return
		}
		hist.Events = append(hist.Events, ev)
	}

	out, err := protojson.MarshalOptions{Indent: "  "}.Marshal(hist)
	if err != nil {
		t.Logf("testinfra.DumpHistory: marshal history for %s: %v", workflowID, err)
		return
	}

	dir := filepath.Join(d.moduleRoot, TemporalArtifactDir)
	if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
		t.Logf("testinfra.DumpHistory: mkdir %s: %v", dir, mkErr)
		return
	}
	path := filepath.Join(dir, sanitizeName(t.Name())+".json")
	if wErr := os.WriteFile(path, out, 0o644); wErr != nil {
		t.Logf("testinfra.DumpHistory: write %s: %v", path, wErr)
		return
	}
	t.Logf("testinfra.DumpHistory: wrote %d events to %s", len(hist.Events), path)
}

// sanitizeName turns a test name (which may contain '/', spaces, etc.) into a
// filesystem-safe artifact filename.
func sanitizeName(name string) string {
	repl := func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}
	return strings.Map(repl, name)
}

// findModuleRoot walks up from the current working directory to the directory
// holding go.mod (the consuming module root), so artifacts and the persistent DB
// are written at stable repo-relative paths regardless of which package's tests
// are running.
func findModuleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("testinfra.findModuleRoot: getwd: %w", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("testinfra.findModuleRoot: go.mod not found above %s", dir)
		}
		dir = parent
	}
}
