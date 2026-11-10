package composegen

import (
	"path"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// clientRootPrefix is where clientgen writes every generated handler package: the REST handlers
// under internal/client/web/<pkg>, a machine client's MCP tool table under
// internal/client/<leaf>/<pkg>.
const clientRootPrefix = "internal/client/"

// restClientPrefix marks a generated REST handler package (clientgen's shared web transport).
const restClientPrefix = clientRootPrefix + "web/"

// scenarioClient is one client contract whose generated handler fronts a built Manager. A machine
// client's scenario runs over the wire against the launched server (Plan 3 Task 3.3), so nothing
// here builds a handler: the classification only says which Manager each client's requests reach,
// which is what the authorization rows of the client's requests are derived from (clientAuthzRows).
type scenarioClient struct {
	key     string
	manager scenarioComp
}

// classifyClients finds, per client contract that names a goPackage, the built Manager that its
// generated handler package fronts. A client contract with no goPackage (a browser surface) or
// whose package holds no generated Handler (the Manager is not exposed to it) is not a client of
// any Manager here.
func (g *scenarioGen) classifyClients() []scenarioClient {
	clientIDs := clientComponentIDs(g.m.System)
	var clients []scenarioClient
	for _, key := range sortedKeys(g.m.Contracts) {
		c := g.m.Contracts[key]
		if c == nil || c.Doc == nil || c.GoPackage == "" || !strings.EqualFold(c.Layer, "client") || !strings.HasPrefix(c.GoPackage, clientRootPrefix) {
			continue
		}
		mgr, ok := g.handlerManager(key, c, clientIDs)
		if !ok {
			continue
		}
		clients = append(clients, scenarioClient{key: key, manager: mgr})
	}
	return clients
}

// handlerManager is the built Manager whose generated transport lives in client contract c's
// package: the package's last segment is the Manager package's (clientgen names a tool table
// or a handler set after the Manager it fronts), and the Manager must be exposed to that
// transport - the same rule resolveManager and resolveMCP apply, so the Handler exists.
func (g *scenarioGen) handlerManager(key string, c *projectmodel.Contract, clientIDs map[string]bool) (scenarioComp, bool) {
	for _, comp := range g.comps {
		if comp.kind != scenarioManager || webPkgBase(comp.contract.GoPackage) != path.Base(c.GoPackage) {
			continue
		}
		if !g.r.managerIsWebExposed(g.m.System, comp.key, clientIDs) {
			continue
		}
		if !strings.HasPrefix(c.GoPackage, restClientPrefix) && !g.mcpClientFronts(key, comp.key) {
			continue
		}
		return comp, true
	}
	return scenarioComp{}, false
}

// mcpClientFronts reports that the MCP machine client behind contract key fronts the Manager,
// as clientgen decides it (resolved.mcpFronts).
func (g *scenarioGen) mcpClientFronts(key, managerKey string) bool {
	if g.m.System == nil {
		return false
	}
	c, ok := g.m.System.ComponentByContractKey(key)
	if !ok {
		// A facet contract (mcpClientBilling) is keyed apart from its System component (mcp-client,
		// contractKey mcpClient) and names that component in its own component field.
		if facet := g.m.Contracts[key]; facet != nil && facet.Component != "" && facet.Component != key {
			c, ok = g.m.System.ComponentByContractKey(facet.Component)
		}
	}
	return ok && g.r.mcpFronts(g.m.System, *c, managerKey)
}
