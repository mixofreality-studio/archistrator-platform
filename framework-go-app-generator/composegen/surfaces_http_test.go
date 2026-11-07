package composegen_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/composegen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/configgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator/httpgen"
)

// TestComposedServerServesMCPAndSurfacesOverHTTP builds the composition root generated for a
// fresh System (an MCP machine client and a browser surface fronting one Manager) into a real
// binary, boots it, and drives it over its real transports (FIX-050, FIX-051): an MCP tool
// call through the real MCP SDK's streamable-HTTP transport at /mcp, behind the generated auth
// middleware and the real security Utility, and the browser surface's page and assets on the
// same origin. Only Temporal is stubbed; the HTTP edge and telemetry are real.
func TestComposedServerServesMCPAndSurfacesOverHTTP(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and boots a sandbox binary")
	}
	m := freshSurfacesModel(t, freshSurfacesProject("", ""))
	generated, err := composegen.Generate(m, freshSurfacesCfg)
	if err != nil {
		t.Fatal(err)
	}
	cfgFile, err := configgen.Generate(m, configgen.Config{ContainerKey: "server", EnvPrefix: "APP", PackageName: "main"})
	if err != nil {
		t.Fatal(err)
	}
	seed, err := composegen.HooksSeed(m, freshSurfacesCfg)
	if err != nil {
		t.Fatal(err)
	}
	wiring, err := httpgen.GenerateWiring(httpgen.WiringOptions{Package: "web"})
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	files := freshHTTPSandbox(t)
	for name, src := range generated {
		files[name] = string(src)
	}
	files["config.gen.go"] = string(cfgFile["config.gen.go"])
	files["hooks.go"] = ownScopeHooks(t, string(seed))
	files["internal/client/web/middleware.gen.go"] = string(wiring.MiddlewareGo)
	files["internal/client/web/server.gen.go"] = string(wiring.ServerGo)
	writeTree(t, dir, files)
	if out, err := sandboxGo(dir, "build", "-o", "server", "."); err != nil {
		t.Fatalf("the composed server does not build: %v\n%s", err, out)
	}

	dist := filepath.Join(dir, "dist")
	writeTree(t, dist, map[string]string{
		"todo-web-app.html": "<!doctype html><title>todo</title>",
		"assets/app.js":     "console.log('todo')",
	})
	base := bootSandbox(t, dir, "APP_DEV_PRINCIPAL=dev-user", "APP_WEB_DIST_DIR="+dist)

	t.Run("browser surface", func(t *testing.T) {
		for path, want := range map[string]string{"/": "<title>todo</title>", "/active": "<title>todo</title>", "/assets/app.js": "console.log"} {
			if code, body := get(t, base+path); code != http.StatusOK || !strings.Contains(body, want) {
				t.Errorf("GET %s = %d %q, want 200 with %q", path, code, body, want)
			}
		}
		if code, _ := get(t, base+"/assets/missing.js"); code != http.StatusNotFound {
			t.Errorf("GET a missing asset = %d, want 404", code)
		}
		if code, _ := get(t, base+"/readyz"); code != http.StatusOK {
			t.Errorf("GET /readyz = %d, want the generated probe's 200", code)
		}
	})
	t.Run("MCP tool over streamable HTTP", func(t *testing.T) {
		session := mcpCall(t, base, "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"scenario","version":"1"}}}`)
		mcpCall(t, base, session, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
		got := mcpCall(t, base, session, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"listGreet","arguments":{}}}`)
		if !strings.Contains(got, "hello dev-user") {
			t.Fatalf("tools/call listGreet = %s, want the Manager's answer for the dev principal", got)
		}
	})
	// The null-subject run boots the same binary with AIARCH_NULL_OPS: the named operation answers
	// with its zero result over the real transport and the Manager is never reached.
	t.Run("null operation answers the zero result", func(t *testing.T) {
		nulled := bootSandbox(t, dir, "APP_DEV_PRINCIPAL=dev-user", "APP_WEB_DIST_DIR="+dist, "AIARCH_NULL_OPS=ListManager.Greet")
		session := mcpCall(t, nulled, "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"scenario","version":"1"}}}`)
		mcpCall(t, nulled, session, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
		got := mcpCall(t, nulled, session, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"listGreet","arguments":{}}}`)
		if strings.Contains(got, "hello") || !strings.Contains(got, `"result"`) {
			t.Fatalf("tools/call listGreet with the op nulled = %s, want the zero result", got)
		}
	})
}

// ownScopeHooks is the seeded hooks.go with the one decision only the app can make filled in:
// a policy admitting every verb on the principal's own scope.
func ownScopeHooks(t *testing.T, seed string) string {
	t.Helper()
	const fill = `panic("FILL: PolicyDecisionPoint")`
	if !strings.Contains(seed, fill) {
		t.Fatalf("the seed no longer asks for the policy:\n%s", seed)
	}
	return strings.Replace(seed, fill, "return ownScope{}", 1) + `
type ownScope struct{}

func (ownScope) Decide(_ context.Context, p security.Principal, _ security.Action, r security.ResourceRef) (bool, error) {
	return p.Subject != "" && r.ID == p.Subject, nil
}
`
}

// freshHTTPSandbox is the sandbox module: the real framework-go, OTel satellite and MCP SDK, a
// stub Temporal SDK, and the Manager, its REST handler and its MCP tool table in the shape
// temporalgen and clientgen give them.
func freshHTTPSandbox(t *testing.T) map[string]string {
	t.Helper()
	fw, err := filepath.Abs("../../framework-go")
	if err != nil {
		t.Fatal(err)
	}
	otelInfra, err := filepath.Abs("../../framework-go-infrastructure-otel")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	addTemporalStub(files)
	addFrameworkStubs(files)
	for name := range files {
		if strings.HasPrefix(name, "_stubs/framework-go/") || strings.HasPrefix(name, "_stubs/otel/") || strings.HasPrefix(name, "_stubs/pgxpool/") || strings.HasPrefix(name, "_stubs/postgres/") {
			delete(files, name)
		}
	}
	files["go.mod"] = `module ` + freshModule + `

go 1.25

require (
	github.com/mixofreality-studio/archistrator-platform/framework-go v0.0.0
	github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-otel v0.0.0
	github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-temporal v0.0.0
	github.com/modelcontextprotocol/go-sdk v1.6.1
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.0.0
	go.temporal.io/sdk v0.0.0
)

replace github.com/mixofreality-studio/archistrator-platform/framework-go => ` + fw + `
replace github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-otel => ` + otelInfra + `
replace github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-temporal => ./_stubs/temporalinfra
replace go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp => ./_stubs/otelhttp
replace go.temporal.io/sdk => ./_stubs/temporal
`
	files["internal/manager/list/list.go"] = `package list

import (
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	fwm "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
)

const TaskQueue = "list"

type Greeting string

type ListManager interface {
	Greet(ctx fwm.Context) (Greeting, error)
}

type impl struct{}

func (impl) Greet(ctx fwm.Context) (Greeting, error) {
	p, _ := security.PrincipalFrom(ctx)
	return Greeting("hello " + p.Subject), nil
}

func NewListManager(tc client.Client) ListManager { return impl{} }

func RegisterManagerWorker(w worker.Worker, m ListManager) {}
`
	files["internal/client/web/list/list.go"] = `package list

import (
	"net/http"

	mgr "` + freshModule + `/internal/manager/list"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
)

type Handler struct {
	Manager  mgr.ListManager
	Security security.Security
}

func (h *Handler) Register(mux *http.ServeMux) {}
`
	files["internal/client/assistantmcpserver/list/list_tools.go"] = `package list

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	mgr "` + freshModule + `/internal/manager/list"
	fwm "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
)

type Handler struct {
	Manager  mgr.ListManager
	Security security.Security
}

type greetInput struct{}

type greetOutput struct {
	Result string ` + "`json:\"result\"`" + `
}

func (h *Handler) Register(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{Name: "listGreet", Description: "Greet on the ListManager."}, h.handleGreet)
}

func (h *Handler) handleGreet(ctx context.Context, _ *mcp.CallToolRequest, _ greetInput) (*mcp.CallToolResult, greetOutput, error) {
	p, ok := security.PrincipalFrom(ctx)
	if !ok {
		return nil, greetOutput{}, errors.New("unauthenticated")
	}
	d, err := h.Security.Authorize(ctx, p, security.Action{Verb: "greet"}, security.ResourceRef{Kind: "listCatalog", ID: p.Subject})
	if err != nil || !d.Permit {
		return nil, greetOutput{}, errors.New("forbidden")
	}
	got, err := h.Manager.Greet(fwm.Context{Context: ctx})
	return nil, greetOutput{Result: string(got)}, err
}
`
	return files
}

// bootSandbox starts the built server with env, waits for its readiness probe and returns its
// base URL; the server is stopped when the test ends.
func bootSandbox(t *testing.T, dir string, env ...string) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	var logs bytes.Buffer
	cmd := exec.Command(filepath.Join(dir, "server"))
	cmd.Env = append(os.Environ(), append(env, "APP_LISTEN_ADDR="+addr)...)
	cmd.Stdout, cmd.Stderr = &logs, &logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	base := "http://" + addr
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if code, _ := tryGet(base + "/readyz"); code == http.StatusOK {
			return base
		}
	}
	t.Fatalf("the composed server never answered /readyz:\n%s", logs.String())
	return ""
}

func tryGet(url string) (int, string) {
	resp, err := http.Get(url) // #nosec G107 -- the test's own sandbox server
	if err != nil {
		return 0, err.Error()
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	code, body := tryGet(url)
	if code == 0 {
		t.Fatalf("GET %s: %s", url, body)
	}
	return code, body
}

// mcpCall posts one JSON-RPC message to /mcp and returns the session id for an initialize, or
// the JSON-RPC response (the last SSE data line) otherwise.
func mcpCall(t *testing.T, base, session, msg string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, base+"/mcp", strings.NewReader(msg))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
		req.Header.Set("Mcp-Protocol-Version", "2025-06-18")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /mcp %s = %d %s", msg, resp.StatusCode, body)
	}
	if session == "" {
		id := resp.Header.Get("Mcp-Session-Id")
		if id == "" {
			t.Fatalf("initialize returned no session id")
		}
		return id
	}
	var last string
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
			last = data
		} else if strings.HasPrefix(sc.Text(), "{") {
			last = sc.Text()
		}
	}
	if last != "" && !json.Valid([]byte(last)) {
		t.Fatalf("not a JSON-RPC response: %s", last)
	}
	return fmt.Sprint(last)
}
