package composegen

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// clientRootPrefix is where clientgen writes every generated handler package: the REST handlers
// under internal/client/web/<pkg>, a machine client's MCP tool table under
// internal/client/<leaf>/<pkg>.
const clientRootPrefix = "internal/client/"

// restClientPrefix marks a generated REST handler package (clientgen's shared web transport).
const restClientPrefix = clientRootPrefix + "web/"

// scenarioClient is one client contract whose generated handler a test serves: the package
// clientgen wrote its Handler into, and the Manager the Handler fronts.
type scenarioClient struct {
	key, iface, alias, importPath string
	manager                       scenarioComp
}

// classifyClients finds, per client contract that names a goPackage, the generated handler
// package it is served from and the built Manager that handler fronts. A client contract
// with no goPackage (a browser surface) or whose package holds no generated Handler (the
// Manager is not exposed to it) gets no constructor: it has no generated subject to build.
func (g *scenarioGen) classifyClients() error {
	clientIDs := clientComponentIDs(g.m.System)
	seenIface := map[string]string{}
	for _, comp := range g.comps {
		seenIface[comp.iface] = comp.key
	}
	for _, key := range sortedKeys(g.m.Contracts) {
		c := g.m.Contracts[key]
		if c == nil || c.Doc == nil || c.GoPackage == "" || !strings.EqualFold(c.Layer, "client") || !strings.HasPrefix(c.GoPackage, clientRootPrefix) {
			continue
		}
		mgr, ok := g.handlerManager(key, c, clientIDs)
		if !ok {
			continue
		}
		iface := c.Doc.Interface.Name
		if other, dup := seenIface[iface]; dup {
			return fmt.Errorf("composegen: scenariocompose: components %q and %q both define interface %q, so New%s would be declared twice", other, key, iface, iface)
		}
		seenIface[iface] = key
		sc := scenarioClient{
			key: key, iface: iface, manager: mgr,
			alias:      clientAlias(c.GoPackage),
			importPath: g.r.cfg.ModulePath + "/" + c.GoPackage,
		}
		g.clients = append(g.clients, sc)
		g.imp.add(sc.importPath, sc.alias)
	}
	return nil
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

// clientAlias is the import alias of a generated handler package: its whole path under
// internal/client, so two clients' handler packages that share a last segment (web/list and
// todomcpserver/list) never collide with each other or with a component package.
func clientAlias(goPackage string) string {
	return strings.ReplaceAll(strings.TrimPrefix(goPackage, clientRootPrefix), "/", "") + "client"
}

// emitClients renders, per client contract, its exported New<Iface>: the generated Handler
// (the value with Register) over the real Manager and the generated edge policy, plus the
// policy itself once.
func (g *scenarioGen) emitClients() error {
	if len(g.clients) == 0 {
		return nil
	}
	rows, err := g.clientAuthzRows()
	if err != nil {
		return err
	}
	g.imp.add(pathSecurity, "")
	g.imp.add("context", "")
	g.body.WriteString(edgePolicySource(rows))
	for _, cl := range g.clients {
		fmt.Fprintf(&g.body, "// New%s builds the generated handlers of the %s client over the real %s and the\n", cl.iface, cl.key, cl.manager.iface)
		g.body.WriteString("// generated authorization policy: the value a test serves (Register) and calls through.\n")
		fmt.Fprintf(&g.body, "func New%s(t *testing.T, h *scenariohost.Host) *%s.Handler {\n", cl.iface, cl.alias)
		fmt.Fprintf(&g.body, "\tt.Helper()\n\treturn &%s.Handler{Manager: %s(subjectFor(t, h)), Security: edgeSecurity()}\n}\n\n", cl.alias, buildFuncName(cl.manager.key))
	}
	return nil
}

// clientAuthzRows is the authorization rows of every Manager a client constructor fronts,
// de-duplicated: the same derivation (httpgen.PlanOps) authz.gen.go reads.
func (g *scenarioGen) clientAuthzRows() ([]authzRow, error) {
	seen := map[string]bool{}
	var rows []authzRow
	for _, cl := range g.clients {
		if seen[cl.manager.key] {
			continue
		}
		seen[cl.manager.key] = true
		mgrRows, err := managerAuthzRows(managerComp{key: cl.manager.key, iface: cl.manager.iface, doc: cl.manager.contract.Doc}, g.r.cfg.ExposedOps)
		if err != nil {
			return nil, err
		}
		rows = append(rows, mgrRows...)
	}
	return rows, nil
}

// edgePolicySource is the policy the generated handlers authorize against in a test: it
// permits exactly the (verb, scope) requests the generated transports send, each on the
// principal's own scope, and refuses everything else. It is built from the same rows as
// authz.gen.go, so a handler that asks for a decision the table does not hold is refused.
func edgePolicySource(rows []authzRow) string {
	pairs := map[string]bool{}
	for _, r := range rows {
		pairs[fmt.Sprintf("\t{verb: %q, scope: %q}: true,\n", r.verb, r.scope)] = true
	}
	lines := make([]string, 0, len(pairs))
	for l := range pairs {
		lines = append(lines, l)
	}
	sort.Strings(lines)
	var b strings.Builder
	b.WriteString("// authzKey is one decision a generated transport asks: a verb on a scope kind.\n")
	b.WriteString("type authzKey struct{ verb, scope string }\n\n")
	b.WriteString("// generatedAuthorizations is every (verb, scope) the generated REST handlers and MCP tools send the\n")
	b.WriteString("// policy, read from the same derivation as the composition root's authz.gen.go.\n")
	b.WriteString("var generatedAuthorizations = map[authzKey]bool{\n")
	for _, l := range lines {
		b.WriteString(l)
	}
	b.WriteString("}\n\n")
	b.WriteString("// generatedPolicy permits an authenticated principal exactly the generated requests on its own\n")
	b.WriteString("// scope. It denies everything else: a test's handlers are never served by a permit-all policy.\n")
	b.WriteString("type generatedPolicy struct{}\n\n")
	b.WriteString("func (generatedPolicy) Decide(_ context.Context, p security.Principal, a security.Action, r security.ResourceRef) (bool, error) {\n")
	b.WriteString("\treturn p.Subject != \"\" && r.ID == p.Subject && generatedAuthorizations[authzKey{verb: a.Verb, scope: r.Kind}], nil\n}\n\n")
	b.WriteString("// edgeSecurity is the security Utility every generated handler a test serves authorizes against.\n")
	b.WriteString("func edgeSecurity() security.Security {\n")
	b.WriteString("\treturn security.New(security.WithPolicyDecisionPoint(generatedPolicy{}))\n}\n\n")
	return b.String()
}
