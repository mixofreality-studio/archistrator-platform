package composegen_test

import (
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/composegen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// freshModule is the fresh app's module path in the surface tests.
const freshModule = "example.com/fresh"

// freshSurfacesProject is a fresh design with every Client kind and no authored deployment: a
// browser surface, a REST machine client and an MCP machine client, all fronting one Manager
// (RUN-017's shape). systemExtra is spliced into the component list, relExtra into the edges.
func freshSurfacesProject(systemExtra, relExtra string) string {
	return `{"serviceContracts":{
	"listManager":{"component":"listManager","layer":"Manager","goPackage":"internal/manager/list",
	  "deps":[{"name":"client","goType":"client.Client","goImport":"go.temporal.io/sdk/client"}],
	  "$defs":{"Greeting":{"type":"string"}},
	  "interface":{"name":"ListManager","layer":"manager","operations":[{"name":"Greet","params":[],"result":{"$ref":"#/$defs/Greeting"},"error":true}]}}},
	"slots":{"5":{"kind":5,"model":{"components":[
	  {"id":"todo-web-app","name":"TodoWebApp","kind":"client","layer":"client","uiSurface":true,"contractKey":"todoWebApp"},
	  {"id":"todo-api","name":"TodoApi","kind":"client","layer":"client","uiSurface":false,"machineProtocol":"rest"},
	  {"id":"assistant-mcp-server","name":"AssistantMcpServer","kind":"client","layer":"client","uiSurface":false,"machineProtocol":"mcp"},
	  {"id":"list-manager","name":"ListManager","kind":"manager","layer":"manager","contractKey":"listManager"}` + systemExtra + `],
	  "relationships":[
	  {"from":"todo-web-app","to":"list-manager"},{"from":"todo-api","to":"list-manager"},{"from":"assistant-mcp-server","to":"list-manager"}` + relExtra + `]}}}}`
}

func freshSurfacesModel(t *testing.T, project string) *projectmodel.Model {
	t.Helper()
	m, err := projectmodel.Load([]byte(project))
	if err != nil {
		t.Fatal(err)
	}
	m.Deployment = m.EffectiveDeployment("server")
	return m
}

var freshSurfacesCfg = composegen.Config{ContainerKey: "server", ModulePath: freshModule, PackageName: "main", EnvPrefix: "APP"}

// A fresh System's MCP machine client is mounted by the generated composition root (FIX-050):
// its Manager's generated tool table on one mcp.Server at /mcp, behind the same auth boundary
// and the same security Utility as /api. RUN-017's binary exposed no MCP surface at all.
func TestComposeMountsTheMCPClientsToolTables(t *testing.T) {
	got, err := composegen.Generate(freshSurfacesModel(t, freshSurfacesProject("", "")), freshSurfacesCfg)
	if err != nil {
		t.Fatal(err)
	}
	main := string(got[composegen.MainFile])
	for _, want := range []string{
		`listmcp "` + freshModule + `/internal/client/assistantmcpserver/list"`,
		`"github.com/modelcontextprotocol/go-sdk/mcp"`,
		`mcpServer := mcp.NewServer(&mcp.Implementation{Name: serviceName, Version: "1.0.0"}, nil)`,
		`(&listmcp.Handler{Manager: webManagers.ListManager, Security: sec}).Register(mcpServer)`,
		`root.Handle("/mcp", hooks.MCPMount(mcpServer, dev, web.AuthMiddleware(dev, validator)(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return mcpServer }, nil))))`,
		"MCPMount(server *mcp.Server, dev web.DevConfig, authed http.Handler) http.Handler",
	} {
		if !strings.Contains(main, want) {
			t.Errorf("main.gen.go lacks %q:\n%s", want, main)
		}
	}
	if strings.Index(main, `root.Handle("/mcp"`) > strings.Index(main, "hooks.ExtraMounts(") {
		t.Error("/mcp must be mounted before the hand routes")
	}
}

// The built browser surfaces are served by the composed server on the origin of their relative
// /api calls (FIX-051): a generated static handler, switched on the declared webDistDir setting,
// with /api and the probes on more specific patterns.
func TestComposeServesTheBrowserSurfacesOnTheAPIOrigin(t *testing.T) {
	got, err := composegen.Generate(freshSurfacesModel(t, freshSurfacesProject("", "")), freshSurfacesCfg)
	if err != nil {
		t.Fatal(err)
	}
	main := string(got[composegen.MainFile])
	for _, want := range []string{
		"if cfg.WebDistDir != \"\" {\n\t\tsurfaces, err := newSurfaceHandler(cfg.WebDistDir)",
		`root.Handle("/api/", genServer)`,
		`root.Handle("/readyz", genServer)`,
		`root.Handle("/", surfaces)`,
		"} else {\n\t\troot.Handle(\"/\", genServer)\n\t}",
	} {
		if !strings.Contains(main, want) {
			t.Errorf("main.gen.go lacks %q:\n%s", want, main)
		}
	}
	if _, ok := got[composegen.SurfacesFile]; !ok {
		t.Fatalf("no %s was generated", composegen.SurfacesFile)
	}
}

// Every Client surface is mounted by generated code, or generation fails naming it.
func TestComposeRefusesAnUnmountedSurface(t *testing.T) {
	t.Run("browser surface with no webDistDir", func(t *testing.T) {
		m := freshSurfacesModel(t, freshSurfacesProject("", ""))
		var kept []projectmodel.Setting
		for _, s := range m.Deployment.Settings {
			if s.Name != projectmodel.WebDistDirSetting {
				kept = append(kept, s)
			}
		}
		m.Deployment.Settings = kept
		_, err := composegen.Generate(m, freshSurfacesCfg)
		if err == nil || !strings.Contains(err.Error(), "TodoWebApp") || !strings.Contains(err.Error(), "webDistDir") {
			t.Fatalf("want a refusal naming the surface and the setting, got %v", err)
		}
	})
	t.Run("MCP client fronting an unexposed manager", func(t *testing.T) {
		cfg := freshSurfacesCfg
		cfg.WebExposedManagers = []string{}
		_, err := composegen.Generate(freshSurfacesModel(t, freshSurfacesProject("", "")), cfg)
		if err == nil || !strings.Contains(err.Error(), "AssistantMcpServer") || !strings.Contains(err.Error(), "listManager") {
			t.Fatalf("want a refusal naming the client and the manager, got %v", err)
		}
	})
}

// A design with no MCP client and no browser surface composes exactly as before: no /mcp, no
// static handler.
func TestComposeMountsNoSurfaceTheSystemLacks(t *testing.T) {
	src := composeMain(t, composeProject("", "", ""))
	for _, absent := range []string{"/mcp", "mcp.NewServer", "newSurfaceHandler"} {
		if strings.Contains(src, absent) {
			t.Errorf("main.gen.go has %q for a System without that surface:\n%s", absent, src)
		}
	}
}
