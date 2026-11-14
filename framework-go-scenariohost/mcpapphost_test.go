package scenariohost

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// stubApp is a launched app's origin: it records what reaches /mcp.
func stubApp(t *testing.T) (*App, *http.Request, *string) {
	t.Helper()
	var seen http.Request
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" {
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		seen = *r.Clone(r.Context())
		w.Header().Set("Mcp-Session-Id", "session-from-app")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	t.Cleanup(srv.Close)
	return &App{BaseURL: srv.URL, MCPURL: srv.URL + "/mcp", Stop: func() error { return nil }}, &seen, &body
}

func serveHost(t *testing.T, app *App, surface string) *AppHost {
	t.Helper()
	h, err := ServeMCPAppHost(t.Context(), app, surface)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return h
}

func get(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Get(url) // #nosec G107 G704 -- the harness host's own listener
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(b)
}

func TestMCPAppHost_ServesPageAndBundle(t *testing.T) {
	app, _, _ := stubApp(t)
	h := serveHost(t, app, "todo-mcp-app")
	resp, page := get(t, h.URL+"/")
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("GET / = %d %q, want 200 text/html", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	for _, want := range []string{`<script src="/host.js">`, `data-surface="todo-mcp-app"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the host page lacks %s:\n%s", want, page)
		}
	}
	resp, bundle := get(t, h.URL+"/host.js")
	if resp.StatusCode != http.StatusOK || bundle != string(hostBundle) {
		t.Errorf("GET /host.js = %d with %d bytes, want 200 with the embedded bundle (%d bytes)", resp.StatusCode, len(bundle), len(hostBundle))
	}
	if err := h.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Errorf("a second Close must be safe: %v", err)
	}
}

func TestMCPAppHost_ProxiesMCPToTheApp(t *testing.T) {
	app, seen, body := stubApp(t)
	h := serveHost(t, app, "todo-mcp-app")
	const rpc = `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, h.URL+"/mcp", strings.NewReader(rpc))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Mcp-Session-Id", "session-from-host")
	resp, err := http.DefaultClient.Do(req) // #nosec G704 -- the harness host's own listener
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	got, _ := io.ReadAll(resp.Body)
	if *body != rpc || seen.Header.Get("Mcp-Session-Id") != "session-from-host" {
		t.Errorf("the app saw body %q and session %q, want the host's request intact", *body, seen.Header.Get("Mcp-Session-Id"))
	}
	if resp.Header.Get("Mcp-Session-Id") != "session-from-app" || !strings.Contains(string(got), `"jsonrpc":"2.0"`) {
		t.Errorf("the app's response did not come back: session %q body %q", resp.Header.Get("Mcp-Session-Id"), got)
	}
}

// A spec's arrange, probes and contract calls use the host's origin as their base URL, so the app's
// REST and arrange routes are reachable through it too.
func TestMCPAppHost_ProxiesTheAppsOtherRoutes(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	h := serveHost(t, &App{BaseURL: srv.URL}, "todo-mcp-app")
	for _, route := range []string{"/api/v1/list/openList", "/__scenariohost/failNext"} {
		resp, _ := get(t, h.URL+route)
		if resp.StatusCode != http.StatusNoContent {
			t.Errorf("GET %s = %d, want the app's 204", route, resp.StatusCode)
		}
	}
	if len(paths) != 2 || paths[0] != "GET /api/v1/list/openList" || paths[1] != "GET /__scenariohost/failNext" {
		t.Errorf("the app saw %v", paths)
	}
}

// The committed bundle is what its sources build: its first line records the sha256 of the sources
// (src/host.ts, index.html, package-lock.json), which this re-checks without node.
func TestMCPAppHost_BundleMatchesSource(t *testing.T) {
	f, err := os.Open("mcpapphost/dist/host.js")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	first, err := bufio.NewReader(f).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "// source-sha256: "
	if !strings.HasPrefix(first, prefix) {
		t.Fatalf("dist/host.js's first line is %q, want %q<hex>", first, prefix)
	}
	h := sha256.New()
	for _, name := range []string{"src/host.ts", "index.html", "package-lock.json"} {
		b, err := os.ReadFile("mcpapphost/" + name)
		if err != nil {
			t.Fatal(err)
		}
		h.Write(b)
	}
	if got, want := strings.TrimSpace(strings.TrimPrefix(first, prefix)), hex.EncodeToString(h.Sum(nil)); got != want {
		t.Fatalf("dist/host.js was built from other sources (recorded %s, sources hash to %s): cd mcpapphost && npm ci && node build.mjs", got, want)
	}
}
