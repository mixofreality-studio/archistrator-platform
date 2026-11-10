//go:build conformance

package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	scenariohost "github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
)

// callTool drives one streamable-HTTP MCP tool call as the process principal: initialize, the
// initialized notification, then tools/call. It returns the raw result.
func callTool(ctx context.Context, url, tool string, args map[string]any) (json.RawMessage, error) {
	_, session, err := rpc(ctx, url, "", map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": mcpProtocol, "capabilities": map[string]any{},
			"clientInfo": map[string]any{"name": "devstack-test", "version": "0"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("initialize: %w", err)
	}
	if _, _, err := rpc(ctx, url, session, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); err != nil {
		return nil, fmt.Errorf("initialized: %w", err)
	}
	reply, _, err := rpc(ctx, url, session, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": tool, "arguments": args},
	})
	if err != nil {
		return nil, fmt.Errorf("tools/call %s: %w", tool, err)
	}
	if reply.Error != nil {
		return nil, fmt.Errorf("tools/call %s: %s", tool, reply.Error.Message)
	}
	var res struct {
		IsError bool `json:"isError"`
	}
	if json.Unmarshal(reply.Result, &res) == nil && res.IsError {
		return nil, fmt.Errorf("tools/call %s failed: %s", tool, reply.Result)
	}
	return reply.Result, nil
}

// launchBuilt builds the todomvc fixture, with its harvested component bodies in place of the
// null implementations, into a fresh directory and returns it with the host: the composed app
// the launch tests drive does real work, so isolation between two launches is observable.
func launchBuilt(t *testing.T) (*scenariohost.Host, string) {
	t.Helper()
	impl, err := FixtureImpl("todomvc")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	if _, err := RunChain(ctx, "todomvc", dir, WithImpl(impl)); err != nil {
		t.Fatalf("RunChain: %v", err)
	}
	return scenariohost.Start(t), dir
}

// The host owns the substrates: the launcher is handed the host's Temporal, Postgres and git
// variables and provisions none of them, and the composed app answers the same probe a booted
// one does.
func TestDevstackLaunch_UsesHostSubstrates(t *testing.T) {
	h, dir := launchBuilt(t)
	ctx := context.Background()
	before, err := DockerInventory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	app := h.LaunchDevstack(t, dir)
	for _, own := range []string{"devstack: postgres on", "devstack: temporal dev server on"} {
		if strings.Contains(app.Log(), own) {
			t.Errorf("the launcher provisioned its own substrate (%q) instead of using the host's:\n%s", own, app.Log())
		}
	}
	during, err := DockerInventory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if left := DockerLeftovers(ctx, before, during); len(left) != 0 {
		t.Errorf("the launch created docker objects of its own: %v", left)
	}
	pctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := Probe(pctx, Booted{BaseURL: app.BaseURL, Dir: dir}); err != nil {
		t.Fatalf("Probe: %v\n%s", err, app.Log())
	}
}

// Two launches of the same built app collide on nothing: each has its own port, database and
// Temporal namespace, so a todo added through one is not visible through the other.
func TestDevstackLaunch_ParallelIsolated(t *testing.T) {
	h, dir := launchBuilt(t)
	a := h.LaunchDevstack(t, dir)
	b := h.LaunchDevstack(t, dir)
	if a.BaseURL == b.BaseURL {
		t.Fatalf("both launches serve %s", a.BaseURL)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	const wording = "only-in-app-a"
	added, err := callTool(ctx, a.MCPURL, "listAddTodo", map[string]any{"owner": "scenario", "wording": wording, "requestId": "r1"})
	if err != nil {
		t.Fatalf("add through A: %v\n%s", err, a.Log())
	}
	t.Logf("add through A: %s", added)
	inA, err := callTool(ctx, a.MCPURL, "listOpenList", map[string]any{"owner": "scenario", "view": "all"})
	if err != nil {
		t.Fatalf("open through A: %v", err)
	}
	if !bytes.Contains(inA, []byte(wording)) {
		t.Fatalf("app A does not show its own todo: %s", inA)
	}
	inB, err := callTool(ctx, b.MCPURL, "listOpenList", map[string]any{"owner": "scenario", "view": "all"})
	if err != nil {
		t.Fatalf("open through B: %v\n%s", err, b.Log())
	}
	if bytes.Contains(inB, []byte(wording)) {
		t.Errorf("app B shows app A's todo: %s", inB)
	}
	databases := h.LaunchDatabases()
	if len(databases) < 2 || databases[len(databases)-1] == databases[len(databases)-2] {
		t.Fatalf("the host created %v, want two distinct launch databases", databases)
	}
}

// Stop reaches the whole process group the launcher started and its docker objects: nothing
// outlives it.
func TestDevstackLaunch_StopKillsProcessGroup(t *testing.T) {
	h, dir := launchBuilt(t)
	ctx := context.Background()
	before, err := DockerInventory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	app := h.LaunchDevstack(t, dir)
	if out, _ := exec.Command("pgrep", "-g", fmt.Sprint(app.PGID)).Output(); len(bytes.TrimSpace(out)) == 0 { // #nosec G204 -- fixed tool, integer argument
		t.Fatalf("no process in the launcher's group %d while the app runs", app.PGID)
	}
	if err := app.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if out, _ := exec.Command("pgrep", "-g", fmt.Sprint(app.PGID)).Output(); len(bytes.TrimSpace(out)) != 0 { // #nosec G204 -- fixed tool, integer argument
		t.Errorf("processes outlived Stop in group %d: %s", app.PGID, out)
	}
	if resp, err := http.Get(app.BaseURL + "/readyz"); err == nil { // #nosec G107 -- the app this test launched
		_ = resp.Body.Close()
		t.Errorf("%s/readyz still answers after Stop", app.BaseURL)
	}
	after, err := DockerInventory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if left := DockerLeftovers(ctx, before, after); len(left) != 0 {
		t.Errorf("docker objects outlived Stop: %v", left)
	}
}
