package composegen_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/composegen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/configgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator/httpgen"
)

// TestMCPApp_ResourceMime builds the composition root generated for a System with an mcpApp Client
// into a real binary and reads the UI resource over the real MCP SDK's streamable-HTTP transport:
// listed with the CSP, read with the MCP Apps MIME type and the bundle's bytes, and refused once the
// bundle is gone (Review Focus 5).
func TestMCPApp_ResourceMime(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and boots a sandbox binary")
	}
	m := freshSurfacesModel(t, freshSurfacesProject(mcpAppComponent, mcpAppRelation))
	generated, err := composegen.Generate(m, mcpAppCfg)
	if err != nil {
		t.Fatal(err)
	}
	cfgFile, err := configgen.Generate(m, configgen.Config{ContainerKey: "server", EnvPrefix: "APP", PackageName: "main"})
	if err != nil {
		t.Fatal(err)
	}
	seed, err := composegen.HooksSeed(m, mcpAppCfg)
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
	files["hooks.go"] = string(seed)
	files["internal/client/web/middleware.gen.go"] = string(wiring.MiddlewareGo)
	files["internal/client/web/server.gen.go"] = string(wiring.ServerGo)
	writeTree(t, dir, files)
	if out, err := sandboxGo(dir, "build", "-o", "server", "."); err != nil {
		t.Fatalf("the composed server does not build: %v\n%s", err, out)
	}

	const bundle = `<!doctype html><div id="root"></div>`
	dist := filepath.Join(dir, "dist")
	writeTree(t, dist, map[string]string{
		"todo-web-app.html":       "<!doctype html><title>todo</title>",
		"todo-mcp-app/shell.html": bundle,
	})
	base := bootSandbox(t, dir, "APP_DEV_PRINCIPAL=dev-user", "APP_WEB_DIST_DIR="+dist)
	session := mcpCall(t, base, "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"scenario","version":"1"}}}`)
	mcpCall(t, base, session, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	const uri = "ui://fresh/todo-mcp-app/shell.html"

	listed := mcpCall(t, base, session, `{"jsonrpc":"2.0","id":2,"method":"resources/list"}`)
	var list struct {
		Result struct {
			Resources []struct {
				URI  string `json:"uri"`
				Meta struct {
					UI struct {
						CSP map[string][]string `json:"csp"`
					} `json:"ui"`
				} `json:"_meta"`
			} `json:"resources"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(listed), &list); err != nil {
		t.Fatalf("resources/list = %s: %v", listed, err)
	}
	found := false
	for _, r := range list.Result.Resources {
		if r.URI != uri {
			continue
		}
		found = true
		if csp := r.Meta.UI.CSP; csp == nil || len(csp["connectDomains"]) != 0 || len(csp["resourceDomains"]) != 0 {
			t.Errorf("_meta.ui.csp = %v, want both domain lists present and empty (%s)", csp, listed)
		}
	}
	if !found {
		t.Fatalf("resources/list lacks %s: %s", uri, listed)
	}

	read := mcpCall(t, base, session, `{"jsonrpc":"2.0","id":3,"method":"resources/read","params":{"uri":"`+uri+`"}}`)
	var got struct {
		Result struct {
			Contents []struct {
				URI      string `json:"uri"`
				MIMEType string `json:"mimeType"`
				Text     string `json:"text"`
			} `json:"contents"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(read), &got); err != nil {
		t.Fatalf("resources/read = %s: %v", read, err)
	}
	if len(got.Result.Contents) != 1 || got.Result.Contents[0].MIMEType != "text/html;profile=mcp-app" || got.Result.Contents[0].Text != bundle || got.Result.Contents[0].URI != uri {
		t.Fatalf("resources/read = %s, want exactly one content of the bundle with mimeType text/html;profile=mcp-app", read)
	}

	if err := os.Remove(filepath.Join(dist, "todo-mcp-app", "shell.html")); err != nil {
		t.Fatal(err)
	}
	gone := mcpCall(t, base, session, `{"jsonrpc":"2.0","id":4,"method":"resources/read","params":{"uri":"`+uri+`"}}`)
	if !strings.Contains(gone, `"error"`) {
		t.Fatalf("resources/read of a missing bundle = %s, want an error", gone)
	}
}
