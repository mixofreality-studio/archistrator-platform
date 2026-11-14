package composegen

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/clientgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/clientgen/mcpemit"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// MCPAppsFile is the generated file that registers the ui:// resources of the built mcpApp Clients
// on the composed /mcp server. It is emitted only for a System that has one.
const MCPAppsFile = "mcpapps.gen.go"

// resolveMCPApps records the built mcpApp Clients whose UI bundles the composed server serves as
// MCP resources. An mcpApp is an MCP-served Client: its resource is reachable only through the
// /mcp server its Managers' tool tables are mounted on, so one that fronts no mounted Manager
// would be a UI the delivered binary never offers.
func (r *resolved) resolveMCPApps(m *projectmodel.Model) error {
	for _, c := range mcpClients(m.System) {
		kind, err := projectmodel.ParseSurface(string(c.Surface))
		if err != nil {
			return fmt.Errorf("composegen: client %q: %w", c.Name, err)
		}
		switch kind.(type) {
		case projectmodel.MCPAppSurface:
		case projectmodel.WebSurface, projectmodel.RESTSurface, projectmodel.MCPToolsSurface:
			continue
		}
		if !r.mcpAppFrontsMounted(m.System, c) {
			return fmt.Errorf("composegen: mcpApp client %q fronts no Manager the composed /mcp server mounts: its ui:// resource would be offered by nothing", c.Name)
		}
		r.mcpApps = append(r.mcpApps, c.ID)
	}
	slices.Sort(r.mcpApps)
	return nil
}

// mcpAppFrontsMounted reports whether some Manager the client fronts has its tool table mounted.
func (r *resolved) mcpAppFrontsMounted(s *projectmodel.System, c projectmodel.SystemComponent) bool {
	return slices.ContainsFunc(r.managers, func(mc managerComp) bool {
		return mc.webExposed && r.mcpFronts(s, c, mc.key)
	})
}

// emitSurfaceFiles adds the files the served surfaces need: the static handler of the browser
// surfaces and the ui:// resources of the mcpApp Clients.
func emitSurfaceFiles(r *resolved, out map[string][]byte) (err error) {
	if r.serveWeb {
		out[SurfacesFile] = emitSurfaces(r.pkgName)
	}
	if len(r.mcpApps) > 0 {
		out[MCPAppsFile], err = emitMCPApps(r)
	}
	return err
}

// emitMCPApps renders mcpapps.gen.go.
func emitMCPApps(r *resolved) ([]byte, error) {
	app := clientgen.AppName(clientgen.Config{ModulePath: r.cfg.ModulePath, App: r.cfg.App})
	return mcpemit.GenerateResources(mcpemit.ResourceOptions{Package: r.pkgName, App: app, Surfaces: r.mcpApps})
}

// writeMCPAppResources registers the resources on the /mcp server, after the tool tables.
func writeMCPAppResources(b *strings.Builder, r *resolved) {
	if len(r.mcpApps) == 0 {
		return
	}
	field := settingFieldName(projectmodel.WebDistDirSetting)
	b.WriteString("\t// MCP Apps: each mcpApp Client's UI bundle is a ui:// resource, read from the web dist.\n")
	b.WriteString("\tregisterMCPAppResources(mcpServer, cfg." + field + ")\n")
}
