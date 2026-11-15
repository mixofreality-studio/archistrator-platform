//go:build conformance

package conformance

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/devstack"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
)

// The MCP Apps surface of the todomvc fixture, run for real (FPC plan 5, Task 5.5): the fixture lists
// its four delivery surfaces and has a todo-mcp-app Client; the built app offers the generated
// ui:// resource through its /mcp server and names it on exactly the tools the Client reaches.

const (
	mcpAppSurfaceKey = "todoMcpApp"
	mcpAppResource   = "ui://todomvc/todo-mcp-app/shell.html"
	mcpAppMIME       = "text/html;profile=mcp-app"
	// managerToolPrefix is the tool-name prefix of ListManager's generated MCP tool table.
	managerToolPrefix = "list"
)

// mcpAppReachedTools is the tool names of the Manager operations the fixture's todo-mcp-app Client
// reaches, read from the model (ManagerOpsThrough), never from the build.
func mcpAppReachedTools(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := Fixture("todomvc")
	if err != nil {
		t.Fatal(err)
	}
	p, ok, err := methodcheck.DecodeProject(raw)
	if err != nil || !ok {
		t.Fatalf("DecodeProject: ok=%v err=%v", ok, err)
	}
	in, err := methodcheck.ScenarioInput(p)
	if err != nil {
		t.Fatalf("ScenarioInput: %v", err)
	}
	want := map[string]bool{}
	for contract, ops := range scenario.ManagerOpsThrough(in, "todo-mcp-app") {
		if contract != "listManager" {
			continue
		}
		for _, op := range ops {
			want[managerToolPrefix+op.Op] = true
		}
	}
	if len(want) == 0 {
		t.Fatal("the fixture's todo-mcp-app reaches no ListManager operation")
	}
	return want
}

// mcpSession initializes an MCP session against url and returns its id.
func mcpSession(ctx context.Context, t *testing.T, url string) string {
	t.Helper()
	_, session, err := devstack.RPC(ctx, url, "", map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": devstack.MCPProtocol, "capabilities": map[string]any{},
			"clientInfo": map[string]any{"name": "mcpapp-probe", "version": "0"},
		},
	})
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if _, _, err := devstack.RPC(ctx, url, session, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); err != nil {
		t.Fatalf("initialized: %v", err)
	}
	return session
}

// rpcResult posts one request in the session and decodes its result into out.
func rpcResult(ctx context.Context, t *testing.T, url, session, method string, params map[string]any, out any) {
	t.Helper()
	reply, _, err := devstack.RPC(ctx, url, session, map[string]any{"jsonrpc": "2.0", "id": 2, "method": method, "params": params})
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	if reply.Error != nil {
		t.Fatalf("%s: %s", method, reply.Error.Message)
	}
	if err := json.Unmarshal(reply.Result, out); err != nil {
		t.Fatalf("%s: decoding %s: %v", method, reply.Result, err)
	}
}

// mcpAppProbe is the bench's frontend-mcpapps-surface probe (archistrator-bench
// benchmarks/todomvc/acceptance/harness.ts:63-66) over the launched app: initialize, a tool that
// carries _meta.ui.resourceUri, and resources/read returning the resource with the MCP Apps mime.
func mcpAppProbe(ctx context.Context, t *testing.T, mcpURL string) {
	t.Helper()
	session := mcpSession(ctx, t, mcpURL)

	var tools struct {
		Tools []struct {
			Name string         `json:"name"`
			Meta map[string]any `json:"_meta"`
		} `json:"tools"`
	}
	rpcResult(ctx, t, mcpURL, session, "tools/list", map[string]any{}, &tools)
	reached := mcpAppReachedTools(t)
	carrying := map[string]bool{}
	for _, tool := range tools.Tools {
		ui, _ := tool.Meta["ui"].(map[string]any)
		uri, has := ui["resourceUri"]
		if !has {
			continue
		}
		carrying[tool.Name] = true
		if uri != mcpAppResource {
			t.Errorf("tool %s names resource %v, want %s", tool.Name, uri, mcpAppResource)
		}
		if !reached[tool.Name] {
			t.Errorf("tool %s carries the MCP App resource but todo-mcp-app does not reach it", tool.Name)
		}
	}
	for name := range reached {
		if !carrying[name] {
			t.Errorf("tool %s is reached through todo-mcp-app but names no ui:// resource", name)
		}
	}

	var resources struct {
		Resources []struct {
			URI      string `json:"uri"`
			MIMEType string `json:"mimeType"`
		} `json:"resources"`
	}
	rpcResult(ctx, t, mcpURL, session, "resources/list", map[string]any{}, &resources)
	listed := false
	for _, r := range resources.Resources {
		if r.URI == mcpAppResource {
			listed = true
			if r.MIMEType != mcpAppMIME {
				t.Errorf("resources/list gives %s mime %q, want %q", r.URI, r.MIMEType, mcpAppMIME)
			}
		}
	}
	if !listed {
		t.Fatalf("resources/list does not hold %s: %+v", mcpAppResource, resources.Resources)
	}

	var read struct {
		Contents []struct {
			URI      string `json:"uri"`
			MIMEType string `json:"mimeType"`
			Text     string `json:"text"`
		} `json:"contents"`
	}
	rpcResult(ctx, t, mcpURL, session, "resources/read", map[string]any{"uri": mcpAppResource}, &read)
	if len(read.Contents) != 1 {
		t.Fatalf("resources/read returned %d contents, want 1", len(read.Contents))
	}
	c := read.Contents[0]
	if c.MIMEType != mcpAppMIME {
		t.Errorf("resources/read mime %q, want %q", c.MIMEType, mcpAppMIME)
	}
	if !strings.Contains(c.Text, `id="root"`) {
		t.Errorf("the shell has no root element:\n%.400s", c.Text)
	}
	if strings.Contains(c.Text, "<script src=") {
		t.Errorf("the shell references an external script; it must be one self-contained file:\n%.400s", c.Text)
	}
}

// mcpAppVerdicts reads the scenario host's results for the mcpApp surface: scenario -> status.
func mcpAppVerdicts(t *testing.T, dir string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "uitests", "test-results", mcpAppSurfaceKey, "results.json")) // #nosec G304 -- the scenario host's own index
	if err != nil {
		t.Fatalf("no verdicts recorded for %s: %v", mcpAppSurfaceKey, err)
	}
	var verdicts []struct{ Scenario, Status string }
	if err := json.Unmarshal(raw, &verdicts); err != nil {
		t.Fatalf("results.json: %v", err)
	}
	out := map[string]string{}
	for _, v := range verdicts {
		out[v.Scenario] = v.Status
	}
	return out
}

// TestConformance_McpAppSurface is the bench's frontend-mcpapps-surface probe over the built fixture,
// then its scenarios in the harness MCP host.
func TestConformance_McpAppSurface(t *testing.T) {
	h, dir := launchBuilt(t)
	ctx := context.Background()
	before, err := DockerInventory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	app := h.LaunchDevstack(t, dir)
	pctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	mcpAppProbe(pctx, t, app.MCPURL)

	t.Run("scenarios", func(t *testing.T) {
		sctx, cancel := context.WithTimeout(ctx, 40*time.Minute)
		defer cancel()
		seatUITests(t, sctx, dir)
		// A scenario the harvested SPA cannot satisfy waits out the Playwright test timeout; bound it
		// as the discriminate runs do.
		t.Setenv("AIARCH_SCENARIO_TIMEOUT_MS", "30000")
		// Each scenario arranges the state it needs over the one app a run launches, so each runs on its
		// own stack (a scenario host launch per scenario), as the discriminate runs do. The harvested SPA
		// satisfies only the scenarios pinned in webPasses, so the run's exit status is not the verdict:
		// each binding's recorded verdict is.
		got := map[string]string{}
		for _, id := range mcpAppScenarioIDs(t, dir) {
			out, _ := run(t, sctx, dir, "go", "tool", "webscenarios", "-C", "uitests",
				"-config", "generated/"+mcpAppSurfaceKey+"/playwright.config.gen.ts", "-grep", `(?:^|\s)`+id+` — `, "--", "--no-deps")
			t.Logf("%s:\n%s", id, tail(out))
			// Playwright clears its output directory at the start of each run, so each run's verdict is
			// read before the next run replaces it.
			maps.Copy(got, mcpAppVerdicts(t, dir))
		}
		var ids []string
		for id := range got {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		if len(ids) == 0 {
			t.Fatal("the host recorded no verdict for the MCP App's scenarios")
		}
		if want := mcpAppScenarioIDs(t, dir); len(ids) != len(want) {
			t.Errorf("%d verdict(s) recorded, want one per generated scenario (%d)", len(ids), len(want))
		}
		for _, id := range ids {
			if (got[id] == "pass") != webPasses[id] {
				t.Errorf("MCP App scenario %s: verdict %q, pinned as passing = %v (the web surface's pins apply: the MCP App reuses the harvested SPA)", id, got[id], webPasses[id])
			}
		}
	})

	during, err := DockerInventory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if left := DockerLeftovers(ctx, before, during); len(left) != 0 {
		t.Errorf("the run created docker objects of its own: %v", left)
	}
}

// mcpAppScenarioIDs lists the MCP App's generated scenarios by id (the surface check is not a scenario).
func mcpAppScenarioIDs(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "uitests", "generated", mcpAppSurfaceKey, "*.spec.gen.ts"))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, f := range files {
		base := filepath.Base(f)
		if base == "surface.spec.gen.ts" {
			continue
		}
		ids = append(ids, strings.TrimSuffix(base, ".spec.gen.ts"))
	}
	sort.Strings(ids)
	return ids
}
