package mcpemit

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// Every built mcpApp Client is one ui:// resource, read from its own file of the web dist at read
// time, with the MCP Apps MIME type and the platform's fixed CSP.
func TestGenerateResources_OnePerSurface(t *testing.T) {
	src, err := GenerateResources(ResourceOptions{Package: "main", App: "todomvc", Surfaces: []string{"a", "b"}})
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	for _, want := range []string{
		ResourceURI("todomvc", "a"),
		ResourceURI("todomvc", "b"),
		`"text/html;profile=mcp-app"`,
		`"csp"`,
		`filepath.Join(webDistDir, "a", "shell.html")`,
		`filepath.Join(webDistDir, "b", "shell.html")`,
		"func registerMCPAppResources(srv *mcp.Server, webDistDir string)",
		"mcp.ResourceNotFoundError(",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("generated source lacks %q:\n%s", want, text)
		}
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "mcpapps.gen.go", src, parser.AllErrors); err != nil {
		t.Fatalf("does not parse: %v\n%s", err, text)
	}
	again, _ := GenerateResources(ResourceOptions{Package: "main", App: "todomvc", Surfaces: []string{"a", "b"}})
	if string(again) != text {
		t.Error("output is not deterministic")
	}
}

func TestResourceConstants(t *testing.T) {
	if ResourceURI("app", "s") != "ui://app/s/shell.html" {
		t.Errorf("uri: %s", ResourceURI("app", "s"))
	}
	if ResourceMIMEType != "text/html;profile=mcp-app" {
		t.Errorf("mime: %s", ResourceMIMEType)
	}
	csp := ResourceCSP()
	for _, k := range []string{"connectDomains", "resourceDomains"} {
		if v, ok := csp[k].([]string); !ok || len(v) != 0 {
			t.Errorf("csp[%s] = %#v, want an empty list", k, csp[k])
		}
	}
}
