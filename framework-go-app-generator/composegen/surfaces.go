package composegen

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/clientgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// The composed server mounts every Client surface whose Managers live in the container, in
// generated code, or generation fails (FIX-050, FIX-051): the REST handlers (web.NewServer),
// the MCP tool tables of every MCP machine client on one mcp.Server at /mcp, and the built
// browser surfaces on the API's own origin. A surface nothing composes is a contract the design
// promises and the delivered binary does not serve, which no gate downstream notices.

// pathMCP is the MCP SDK the generated tool tables register on.
const pathMCP = "github.com/modelcontextprotocol/go-sdk/mcp"

// SurfacesFile is the generated static handler that serves the built browser surfaces.
const SurfacesFile = "surfaces.gen.go"

// mcpMount is one Manager's generated MCP tool table mounted at /mcp.
type mcpMount struct {
	iface      string // the Manager interface, the WebManagers field
	alias      string // the tool table package's import alias
	importPath string
}

// resolveSurfaces records the MCP tool tables /mcp mounts and whether the browser surfaces are
// served, failing on a surface the composed server would leave unmounted.
func (r *resolved) resolveSurfaces(m *projectmodel.Model) error {
	if err := r.resolveMCP(m); err != nil {
		return err
	}
	return r.resolveBrowserSurfaces(m)
}

// resolveMCP mounts the tool table of every constructed Manager an MCP machine client fronts:
// the same (client, Manager) pairs clientgen writes tool tables for. Tool names are
// Manager-namespaced, so a Manager two MCP clients front is registered once.
func (r *resolved) resolveMCP(m *projectmodel.Model) error {
	seen := map[string]bool{}
	for _, c := range mcpClients(m.System) {
		for _, mc := range r.managers {
			if seen[mc.key] || !r.mcpFronts(m.System, c, mc.key) {
				continue
			}
			if !mc.webExposed {
				return fmt.Errorf("composegen: MCP client %q fronts manager %q, which the container does not expose to its transports: its tool table would be served by nothing", c.Name, mc.key)
			}
			seen[mc.key] = true
			r.mcpMounts = append(r.mcpMounts, mcpMount{
				iface:      mc.iface,
				alias:      pkgAlias(mc.importPath) + "mcp",
				importPath: r.cfg.ModulePath + "/" + clientgen.ClientPackage(c.Name) + "/" + pkgAlias(mc.importPath),
			})
		}
	}
	return nil
}

// mcpFronts mirrors clientgen's machineClients: with ExposedOps set every exposed Manager gets a
// tool table on every MCP client, else the client's relationships decide.
func (r *resolved) mcpFronts(s *projectmodel.System, c projectmodel.SystemComponent, key string) bool {
	if r.cfg.ExposedOps != nil {
		_, ok := r.cfg.ExposedOps[key]
		return ok
	}
	comp, ok := s.ComponentByContractKey(key)
	if !ok {
		return false
	}
	return slices.ContainsFunc(s.Relationships, func(rel projectmodel.Relationship) bool {
		return rel.From == c.ID && rel.To == comp.ID
	})
}

// mcpClients is the System's built MCP machine clients, the ones clientgen writes tool tables for.
func mcpClients(s *projectmodel.System) []projectmodel.SystemComponent {
	if s == nil {
		return nil
	}
	var out []projectmodel.SystemComponent
	for _, c := range s.Components {
		if strings.EqualFold(c.Kind, "client") && c.IsBuilt() && !c.IsBrowserSurface() && c.IsMCPClient() {
			out = append(out, c)
		}
	}
	return out
}

// resolveBrowserSurfaces serves the built browser surfaces when the deployment declares the
// webDistDir setting, and fails when the System has a browser surface and no setting to serve it
// by: its relative /api calls would have no origin to reach.
func (r *resolved) resolveBrowserSurfaces(m *projectmodel.Model) error {
	_, declared := hasSetting(m.Deployment, projectmodel.WebDistDirSetting)
	surfaces := m.System.BuiltBrowserSurfaces()
	if len(surfaces) > 0 && !declared {
		names := make([]string, 0, len(surfaces))
		for _, c := range surfaces {
			names = append(names, c.Name)
		}
		return fmt.Errorf("composegen: the System has browser surfaces (%s) but the deployment declares no %q setting: the composed server must serve them on the origin of their /api calls", strings.Join(names, ", "), projectmodel.WebDistDirSetting)
	}
	r.serveWeb = declared && len(r.webMgrs) > 0
	return nil
}

// writeRootMounts binds the generated surfaces onto the root mux: the REST server (and the
// browser surfaces beside it when served), then /mcp.
func writeRootMounts(b *strings.Builder, r *resolved) {
	b.WriteString("\troot := http.NewServeMux()\n")
	if r.serveWeb {
		field := settingFieldName(projectmodel.WebDistDirSetting)
		b.WriteString("\t// The built browser surfaces share the API's origin, so their relative /api calls\n")
		b.WriteString("\t// reach it; /api, the probes and /mcp are more specific patterns and win.\n")
		b.WriteString("\tif cfg." + field + " != \"\" {\n")
		b.WriteString("\t\tsurfaces, err := newSurfaceHandler(cfg." + field + ")\n")
		b.WriteString("\t\tif err != nil {\n\t\t\treturn err\n\t\t}\n")
		b.WriteString("\t\troot.Handle(\"/api/\", genServer)\n")
		b.WriteString("\t\troot.Handle(\"/healthz\", genServer)\n")
		b.WriteString("\t\troot.Handle(\"/readyz\", genServer)\n")
		b.WriteString("\t\troot.Handle(\"/\", surfaces)\n")
		b.WriteString("\t\tlogger.Info(\"browser surfaces served\", \"dir\", cfg." + field + ")\n")
		b.WriteString("\t} else {\n")
		b.WriteString("\t\troot.Handle(\"/\", genServer)\n")
		b.WriteString("\t}\n")
	} else {
		b.WriteString("\troot.Handle(\"/\", genServer)\n")
	}
	// Plan 3 Task 3.5: the arrange endpoint of web scenarios, mounted only when the scenario host's
	// address is configured (local profile alone), forwarding to the host's arrange server.
	b.WriteString("\tif scenariohostURL != \"\" {\n")
	b.WriteString("\t\troot.Handle(\"/__scenariohost/\", arrangeForwarder(scenariohostURL))\n")
	b.WriteString("\t}\n")
	if len(r.mcpMounts) == 0 {
		return
	}
	b.WriteString("\t// MCP machine clients: every generated tool table on one server, behind the same\n")
	b.WriteString("\t// auth boundary as /api (a bearer token, or the local dev principal) and authorized\n")
	b.WriteString("\t// by the same security Utility.\n")
	b.WriteString("\tmcpServer := mcp.NewServer(&mcp.Implementation{Name: serviceName, Version: \"1.0.0\"}, nil)\n")
	for _, mm := range r.mcpMounts {
		b.WriteString("\t(&" + mm.alias + ".Handler{Manager: webManagers." + mm.iface + ", Security: sec}).Register(mcpServer)\n")
	}
	b.WriteString("\troot.Handle(\"/mcp\", hooks.MCPMount(mcpServer, dev, web.AuthMiddleware(dev, validator)(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return mcpServer }, nil))))\n")
}

// addSurfaceImports adds the MCP SDK and the mounted tool tables.
func (r *resolved) addSurfaceImports(s *importSet) {
	if len(r.mcpMounts) == 0 {
		return
	}
	s.add(pathMCP, "")
	for _, mm := range r.mcpMounts {
		s.add(mm.importPath, mm.alias)
	}
}

// emitSurfaces renders surfaces.gen.go, the static handler of the built browser surfaces.
func emitSurfaces(pkg string) []byte {
	return []byte(genHeader + "\npackage " + pkg + surfacesBody)
}

const surfacesBody = `

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

// surfaceHandler serves the built browser surfaces in a directory on the API's origin. Every
// top-level <surface>.html is a surface: a lone surface is served at "/", several each at
// "/<surface>/". A path naming a file serves that file; any other extensionless path under a
// surface serves its page, so a client-side route survives a reload.
type surfaceHandler struct {
	files fs.FS
	pages []string
}

// newSurfaceHandler serves dir, which must hold at least one built <surface>.html.
func newSurfaceHandler(dir string) (http.Handler, error) {
	files := os.DirFS(dir)
	pages, err := fs.Glob(files, "*.html")
	if err != nil {
		return nil, err
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("browser surfaces: %s holds no <surface>.html; build the web project first", dir)
	}
	return surfaceHandler{files: files, pages: pages}, nil
}

func (h surfaceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name != "" && fs.ValidPath(name) {
		if info, err := fs.Stat(h.files, name); err == nil && !info.IsDir() {
			http.ServeFileFS(w, r, h.files, name)
			return
		}
	}
	page, ok := h.pageFor(name)
	if !ok || path.Ext(name) != "" {
		http.NotFound(w, r)
		return
	}
	http.ServeFileFS(w, r, h.files, page)
}

// pageFor is the page of the surface a path belongs to.
func (h surfaceHandler) pageFor(name string) (string, bool) {
	if len(h.pages) == 1 {
		return h.pages[0], true
	}
	first, _, _ := strings.Cut(name, "/")
	for _, p := range h.pages {
		if strings.TrimSuffix(p, ".html") == first {
			return p, true
		}
	}
	return "", false
}
`
