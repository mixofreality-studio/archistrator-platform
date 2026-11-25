package composegen_test

import (
	"os"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/composegen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// mcpAppComponent is an mcpApp Client spliced into freshSurfacesProject's component list.
const mcpAppComponent = `,
	  {"id":"todo-mcp-app","name":"TodoMcpApp","kind":"client","layer":"client","surface":"mcpApp"}`

const mcpAppRelation = `,{"from":"todo-mcp-app","to":"list-manager"}`

var mcpAppCfg = func() composegen.Config {
	cfg := freshSurfacesCfg
	cfg.App = "fresh"
	return cfg
}()

// A System with no mcpApp Client gets no resources: no generated file, no call.
func TestMCPApp_NoResourceWithoutMcpAppClient(t *testing.T) {
	got, err := composegen.Generate(freshSurfacesModel(t, freshSurfacesProject("", "")), mcpAppCfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got[composegen.MCPAppsFile]; ok {
		t.Errorf("%s generated for a System with no mcpApp Client", composegen.MCPAppsFile)
	}
	if strings.Contains(string(got[composegen.MainFile]), "registerMCPAppResources") {
		t.Error("main.gen.go mentions registerMCPAppResources for a System with no mcpApp Client")
	}
}

// The mcpApp's resources are registered on the one /mcp server after the tool tables and before the
// route is mounted; an mcpApp whose Managers are not mounted is refused, naming it.
func TestMCPApp_ComposeRegistersResources(t *testing.T) {
	got, err := composegen.Generate(freshSurfacesModel(t, freshSurfacesProject(mcpAppComponent, mcpAppRelation)), mcpAppCfg)
	if err != nil {
		t.Fatal(err)
	}
	res := string(got[composegen.MCPAppsFile])
	if res == "" {
		t.Fatalf("no %s was generated", composegen.MCPAppsFile)
	}
	for _, want := range []string{"ui://fresh/todo-mcp-app/shell.html", "package main"} {
		if !strings.Contains(res, want) {
			t.Errorf("%s lacks %q:\n%s", composegen.MCPAppsFile, want, res)
		}
	}
	main := string(got[composegen.MainFile])
	call := `registerMCPAppResources(mcpServer, cfg.WebDistDir)`
	reg := strings.Index(main, "Register(mcpServer)")
	at := strings.Index(main, call)
	mount := strings.Index(main, `root.Handle("/mcp"`)
	if reg < 0 || at <= reg || mount <= at {
		t.Errorf("want the tool tables (%d), then %s (%d), then the /mcp mount (%d):\n%s", reg, call, at, mount, main)
	}

	_, err = composegen.Generate(freshSurfacesModel(t, freshSurfacesProject(mcpAppComponent, "")), mcpAppCfg)
	if err == nil || !strings.Contains(err.Error(), "fronts no Manager") || !strings.Contains(err.Error(), "TodoMcpApp") {
		t.Fatalf("an mcpApp that fronts no Manager: want a refusal naming it, got %v", err)
	}
}

// An mcpApp whose only related Manager has no contract yet is left unbuilt with it, not refused:
// contracts are recorded one detailedDesign at a time and every commit composes the model.
func TestMCPApp_LeftUnbuiltWhileItsManagerHasNoContract(t *testing.T) {
	comps := `,
	  {"id":"todo-mcp-app","name":"TodoMcpApp","kind":"client","layer":"client","surface":"mcpApp"},
	  {"id":"later-manager","name":"LaterManager","kind":"manager","layer":"manager","contractKey":"laterManager"}`
	rels := `,{"from":"todo-mcp-app","to":"later-manager"}`
	got, err := composegen.Generate(freshSurfacesModel(t, freshSurfacesProject(comps, rels)), mcpAppCfg)
	if err != nil {
		t.Fatalf("an mcpApp awaiting its Manager's contract: want no error, got %v", err)
	}
	if _, ok := got[composegen.MCPAppsFile]; ok {
		t.Errorf("%s generated for an mcpApp whose Manager is not built", composegen.MCPAppsFile)
	}
	if strings.Contains(string(got[composegen.MainFile]), "registerMCPAppResources") {
		t.Error("main.gen.go mentions registerMCPAppResources for an unbuilt mcpApp")
	}
}

// The real todomvc project at C-authorizing-engine: only authorizingEngine is recorded, and
// mcp-app-client (mcpApp) relates to todo-list-manager, which has no contract. Composed as
// cmd/nullgen does, it must not fail and must offer no mcpApp resource.
func TestMCPApp_TodomvcAtAuthorizingEngineComposes(t *testing.T) {
	raw, err := os.ReadFile("testdata/todomvc_authorizing_engine_project.json")
	if err != nil {
		t.Fatal(err)
	}
	m, err := projectmodel.Load(raw)
	if err != nil {
		t.Fatal(err)
	}
	container := "server"
	if m.Deployment != nil && len(m.Deployment.Containers) > 0 {
		container = m.Deployment.Containers[0].Key
	}
	effective := *m
	effective.Deployment = m.EffectiveDeployment(container)
	cfg := composegen.Config{ContainerKey: container, ModulePath: "example.com/x", PackageName: "main", EnvPrefix: "APP"}
	src, err := composegen.HooksSeedWith(&effective, cfg, composegen.SeedOptions{})
	if err != nil {
		t.Fatalf("composing todomvc at C-authorizing-engine: %v", err)
	}
	if strings.Contains(string(src), "registerMCPAppResources") {
		t.Error("hooks seed mentions an mcpApp resource")
	}
}
