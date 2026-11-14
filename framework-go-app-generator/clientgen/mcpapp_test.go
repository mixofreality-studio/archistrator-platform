package clientgen

import (
	"encoding/json"
	"strings"
	"testing"

	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// greetContract is a Manager with two operations, so a test can tell the operation an mcpApp
// reaches from the one it does not. uiOnGreet, when set, annotates Greet with the hand ui.view.
func greetContract(uiOnGreet string) string {
	ui := ""
	if uiOnGreet != "" {
		ui = `,"ui":{"view":"` + uiOnGreet + `"}`
	}
	return `{"component":"listManager","layer":"Manager","goPackage":"internal/manager/list","$defs":{"Greeting":{"type":"string"}},
"interface":{"name":"ListManager","layer":"manager","operations":[
{"name":"Greet","params":[],"result":{"$ref":"#/$defs/Greeting"},"error":true` + ui + `},
{"name":"Wave","params":[],"result":{"$ref":"#/$defs/Greeting"},"error":true}]}}`
}

// mcpAppProject is the System of appProject plus an mcpApp Client fronting the same Manager.
func mcpAppProject(contract string, apps ...MCPApp) Project {
	p := appProject()
	p.Contracts = map[string]json.RawMessage{"listManager": json.RawMessage(contract)}
	p.System.Components = append(p.System.Components,
		projectmodel.SystemComponent{ID: "todo-mcp-app", Name: "TodoMcpApp", Kind: "client", Surface: projectmodel.SurfaceMCPApp})
	p.System.Relationships = append(p.System.Relationships, projectmodel.Relationship{From: "todo-mcp-app", To: "list-manager"})
	p.MCPApps = apps
	return p
}

// The operations an mcpApp reaches carry the derived ui:// resource in their tool's _meta, in the
// table of every MCP-served client; no other op carries it, and without an mcpApp no table does.
func TestMCPApp_OpUIDerivedFromModel(t *testing.T) {
	app := MCPApp{Component: "todo-mcp-app", Ops: map[string][]string{"listManager": {"Greet"}}}
	files, err := Generate(mcpAppProject(greetContract(""), app), Config{ModulePath: "example.com/todomvc"})
	if err != nil {
		t.Fatal(err)
	}
	uri := `"ui://todomvc/todo-mcp-app/shell.html"`
	for _, path := range []string{
		"internal/client/machineapi/list/list_tools.gen.go",
		"internal/client/todomcpapp/list/list_tools.gen.go",
	} {
		src := string(files[path])
		if src == "" {
			t.Fatalf("no %s", path)
		}
		greet := lineWith(src, `Name: "listGreet"`)
		if !strings.Contains(greet, `Meta: mcp.Meta{"ui": map[string]any{"resourceUri": `+uri+`}}`) {
			t.Errorf("%s: listGreet lacks the derived resource: %s", path, greet)
		}
		if wave := lineWith(src, `Name: "listWave"`); strings.Contains(wave, "resourceUri") {
			t.Errorf("%s: listWave must carry no resource: %s", path, wave)
		}
		if strings.Contains(src, "shellResourceURI") {
			t.Errorf("%s: the hand shell const belongs to an annotated op only", path)
		}
	}

	none, err := Generate(mcpAppProject(greetContract("")), Config{ModulePath: "example.com/todomvc"})
	if err != nil {
		t.Fatal(err)
	}
	for path, src := range none {
		if strings.Contains(string(src), "resourceUri") {
			t.Errorf("%s: no mcpApp reaches an op, yet a resource is named", path)
		}
	}
}

func lineWith(src, sub string) string {
	for _, l := range strings.Split(src, "\n") {
		if strings.Contains(l, sub) {
			return l
		}
	}
	return ""
}

// One operation has one resource: two mcpApps reaching it, or an mcpApp reaching an op that also
// carries the hand ui.view annotation, is a generation error.
func TestMCPApp_OpReachedTwiceIsAnError(t *testing.T) {
	a := MCPApp{Component: "todo-mcp-app", Ops: map[string][]string{"listManager": {"Greet"}}}
	b := MCPApp{Component: "other-mcp-app", Ops: map[string][]string{"listManager": {"Greet"}}}
	_, err := Generate(mcpAppProject(greetContract(""), a, b), Config{ModulePath: "example.com/todomvc"})
	if err == nil || !strings.Contains(err.Error(), "todo-mcp-app") || !strings.Contains(err.Error(), "other-mcp-app") {
		t.Fatalf("two mcpApps on one op: want an error naming both, got %v", err)
	}
	_, err = Generate(mcpAppProject(greetContract("home"), a), Config{ModulePath: "example.com/todomvc"})
	if err == nil || !strings.Contains(err.Error(), "ui annotation") {
		t.Fatalf("an annotated op: want a ui annotation error, got %v", err)
	}
}
