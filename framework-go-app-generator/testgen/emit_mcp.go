package testgen

import (
	"bytes"
	"fmt"
	"go/format"
	"path"
	"strconv"
	"strings"

	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// Client dialects (deterministic-component-testing design §12 B1). A client
// surface is a contract with ops, and how a scenario drives it depends on
// what the surface is:
//
//   - web: every op is one user action and takes exactly one parameter,
//     `action` = {kind, target data-testid, value?}. testgen emits Playwright
//     (emit_playwright.go).
//   - MCP: every op is one generated MCP tool — op name = the tool's Go method
//     name (ManagerBase+Op, as transportgen's MCPClient spells it), tool name =
//     projectmodel.LowerFirst(op name), params = the tool's input properties,
//     result = its structured output. testgen emits a Go scenarios file in the
//     contract's goPackage that serves the generated tools in-process and calls
//     them over an in-memory transport (this file).
//
// The `action` parameter is the web dialect's defining shape, so a contract is
// classified by its ops, never by whether it names a goPackage (a web client's
// REST transport can live in one). A contract mixing the two shapes is an
// error. A contract with no ops yet is MCP when it names a goPackage — the
// in-process dialect's home — and web otherwise.
type dialect int

const (
	dialectWeb dialect = iota
	dialectMCP
	dialectREST
)

// restClientPackage is where a REST machine client's handlers live: the shared generated
// transport. It is what tells a REST client's contract from an MCP one, whose ops look the same.
const restClientPackage = "internal/client/web/"

func (d dialect) String() string {
	switch d {
	case dialectMCP:
		return "mcp"
	case dialectREST:
		return "rest"
	}
	return "web"
}

// webActionParam is the one parameter a web client op takes (§12 B1).
const webActionParam = "action"

// clientDialect classifies client contract comp by its ops.
func clientDialect(comp string, sc methodcheck.ServiceContract) (dialect, error) {
	var web, tools []string
	for _, op := range sc.Interface.Operations {
		if isWebAction(op) {
			web = append(web, op.Name)
		} else {
			tools = append(tools, op.Name)
		}
	}
	switch {
	case len(web) > 0 && len(tools) > 0:
		return dialectWeb, fmt.Errorf("testgen: %s mixes web actions (%s) and MCP tools (%s): a client surface is a browser or an MCP server, not both", comp, strings.Join(web, ", "), strings.Join(tools, ", "))
	case len(web) > 0:
		return dialectWeb, nil
	case strings.HasPrefix(sc.GoPackage, restClientPackage):
		return dialectREST, nil
	case len(tools) > 0 || sc.GoPackage != "":
		return dialectMCP, nil
	default:
		return dialectWeb, nil
	}
}

func isWebAction(op methodcheck.ContractOperation) bool {
	return len(op.Params) == 1 && op.Params[0].Name == webActionParam
}

// toolName is the MCP tool an MCP client op calls.
func toolName(op methodcheck.ContractOperation) string { return projectmodel.LowerFirst(op.Name) }

// mcpSDKImport is the MCP SDK the generated tools server and client run on —
// the one the generated tools packages register with.
const mcpSDKImport = "github.com/modelcontextprotocol/go-sdk/mcp"

// emitMCP emits one machine client package's generated scenarios file. One
// package serves one tools surface, so it hosts exactly one machine client
// contract. Its subject is the client's generated handlers, built by the
// scenariocompose package with the app's real authorization (never a policy that
// permits everything), served in-process and called as the principal the plan
// records for each step's actor.
func emitMCP(out *Output, cfg Config, goPkg, stereotype string, plans []componentPlan, routes map[string]restRoute, contracts map[string]methodcheck.ServiceContract) error {
	if len(plans) != 1 {
		return fmt.Errorf("testgen: %s hosts several MCP client contracts (%s); a package serves one tools surface", goPkg, plansLabel(plans))
	}
	plan := plans[0]
	g, err := newGoEmit(cfg, goPkg, plans, contracts)
	if err != nil {
		return err
	}
	g.tools = true
	if err := g.claimPlan(plan); err != nil {
		return err
	}
	for _, bs := range plan.Scenarios {
		if bs.Binding.Skip == nil {
			g.subject = true
		}
		g.emitToolScenario(plan, bs)
	}
	if g.err != nil {
		return g.err
	}
	src, err := g.toolsFile(plan, routes)
	if err != nil {
		return err
	}
	out.Generated[path.Join(goPkg, stereotype+"_scenarios.gen_test.go")] = src
	return nil
}

// sessionVar names the in-process session a principal's steps call through.
func sessionVar(principalName string) string {
	if principalName == "" {
		return "subject"
	}
	return "subjectAs" + exportName(ident(principalName))
}

// emitToolScenario emits one binding as a test: build the client's handlers,
// serve them once per principal the steps act as, run the arrange calls, then
// per step call the tool and check the expectation and probes.
func (g *goEmit) emitToolScenario(plan componentPlan, bs boundScenario) {
	name := g.testName(plan, bs.Scenario.ID)
	fmt.Fprintf(&g.body, "// %s — %s (use case %s; path %s).\n", name, bs.Scenario.Title, bs.Scenario.UseCase, strings.Join(bs.Scenario.Path, " → "))
	fmt.Fprintf(&g.body, "func %s(t *testing.T) {\n", name)
	g.body.WriteString("\th := scenariohost.Start(t)\n")
	fmt.Fprintf(&g.body, "\th.RunScenario(t, %q, %q, func(t *testing.T) {\n", plan.Key, bs.Scenario.ID)
	switch {
	case bs.Binding.Skip != nil:
		fmt.Fprintf(&g.body, "\t\tt.Skip(%q)\n", bs.Binding.Skip.Reason+" (until "+bs.Binding.Skip.Until+")")
	default:
		g.imports[g.cfg.ModulePath+"/"+scenarioComposeDir] = true
		fmt.Fprintf(&g.body, "\t\thandlers := %s\n", g.ownTarget(plan).construct())
		if len(bs.Steps) == 0 {
			g.body.WriteString("\t\t_ = handlers // the binding carries no steps\n")
		}
		g.serveSessions(plan, bs)
		g.emitArrange(plan, bs)
		d := toolDialect{g: g}
		for _, st := range bs.Steps {
			g.emitStep(d, plan, bs, st)
		}
	}
	g.body.WriteString("\t})\n}\n\n")
}

// serveSessions serves the handlers once per principal the scenario's steps act as, in step order.
func (g *goEmit) serveSessions(plan componentPlan, bs boundScenario) {
	seen := map[string]bool{}
	for _, st := range bs.Steps {
		p := stepPrincipal(plan, st)
		if seen[p] {
			continue
		}
		seen[p] = true
		ctx := "h.Context(t)"
		if p != "" {
			g.usesAs = true
			ctx = "h.PrincipalContext(t, principal(" + strconv.Quote(p) + "))"
		}
		fmt.Fprintf(&g.body, "\t\t%s := serveTools(%s, t, h, handlers)\n", sessionVar(p), ctx)
	}
}

// toolDialect drives the subject by calling the tools or routes of its generated handlers.
type toolDialect struct{ g *goEmit }

func (toolDialect) stepComment(st boundStep, bs boundScenario) string {
	return fmt.Sprintf("step %d: %s → tool %s (node %s%s)", st.Bind.Seq, st.Op.Name, toolName(st.Op), st.Stim.Node, guardNote(bs.Scenario))
}

// stepCall calls the tool with the binding's inputs as its argument object, refs threaded in from earlier results.
func (d toolDialect) stepCall(plan componentPlan, bs boundScenario, st boundStep, out, errVar string) []string {
	seq := st.Bind.Seq
	args := fmt.Sprintf("args%d", seq)
	lines, err := d.g.toolArgs(plan.Contract, st.Op, st.Bind.Inputs, args, bs.Scenario.ID, seq)
	if err != nil {
		d.g.fail(err)
		return nil
	}
	return append(lines, fmt.Sprintf("%s, %s = %s.Call(h.Context(t), %q, %s)", out, errVar, sessionVar(stepPrincipal(plan, st)), toolName(st.Op), args))
}

// probeBody is the closure body of a probe: a tool call inline when the probe names one of the contract's
// own tools, a direct call on the probe's component otherwise, as the step's principal.
func (d toolDialect) probeBody(plan componentPlan, bs boundScenario, st boundStep, n int, pr methodcheck.Probe) []string {
	g := d.g
	if sameComponent(plan, pr.Component) {
		op, ok := operationNamed(plan.Contract, pr.Operation)
		if !ok {
			g.fail(fmt.Errorf("testgen: %s scenario %q step %d probe %d: operation %q is not on the contract", plan.Key, bs.Scenario.ID, st.Bind.Seq, n, pr.Operation))
			return nil
		}
		lines, err := g.toolArgs(plan.Contract, op, pr.Inputs, "args", bs.Scenario.ID, st.Bind.Seq)
		if err != nil {
			g.fail(err)
			return nil
		}
		return append(lines, fmt.Sprintf("return %s.Call(h.Context(t), %q, args)", sessionVar(stepPrincipal(plan, st)), toolName(op)))
	}
	return goDialect{g: g}.probeBody(plan, bs, st, n, pr)
}

// toolArgs declares argsVar as the JSON object of a tool call's literal inputs and threads each ref in.
func (g *goEmit) toolArgs(sc methodcheck.ServiceContract, op methodcheck.ContractOperation, inputs []methodcheck.TestArg, argsVar, scenarioID string, seq int) ([]string, error) {
	args, refs, err := inputsJSON(sc, op, inputs)
	if err != nil {
		return nil, err
	}
	lines := []string{fmt.Sprintf("%s := %s", argsVar, rawString(args))}
	for _, r := range refs {
		lines = append(lines, fmt.Sprintf("%s = threadRef(t, %q, %d, %s, %q, %s, %q)", argsVar, scenarioID, seq, argsVar, r.param, refSource(r.ref), r.ref.Path))
	}
	return lines, nil
}

// toolsFile assembles the machine client's generated file: header, package
// clause, imports, TestMain (plain: a client runs no workflow of its own), the
// scenario tests and, when any runs, the tools server and assertion helpers.
func (g *goEmit) toolsFile(plan componentPlan, routes map[string]restRoute) ([]byte, error) {
	rest, err := isRESTPlan(plan)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteString(genHeader + "\n")
	fmt.Fprintf(&b, "// Source: project.json .phaseArtifacts.testPlan.%s × scenario.ForComponent(%q)\n", plan.Key, plan.Key)
	fmt.Fprintf(&b, "\npackage %s_test\n\n", g.pkg)
	g.writeImports(&b, g.toolsImports(rest))
	fmt.Fprintf(&b, "func TestMain(m *testing.M) { scenariohost.Main(m, %s) }\n\n", strconv.Quote(plan.Key))
	b.Write(g.body.Bytes())
	if g.subject {
		if err := g.writeToolHelpers(&b, plan, rest, routes); err != nil {
			return nil, err
		}
	}
	src, err := format.Source(b.Bytes())
	if err != nil {
		return nil, fmt.Errorf("testgen: %s: generated file does not format: %w\n%s", g.goPkg, err, b.String())
	}
	return src, nil
}

// toolsImports is the machine client file's import set: the test essentials always, the transport's,
// the compose package's and the called components' when any binding runs.
func (g *goEmit) toolsImports(rest bool) map[string]string {
	imps := map[string]string{"testing": "", scenarioHostImport: ""}
	if !g.subject {
		return imps
	}
	for _, p := range append(transportImports(rest), "fmt") {
		imps[p] = ""
	}
	if g.usesTime {
		imps["time"] = ""
	}
	for p := range g.imports {
		imps[p] = ""
	}
	return imps
}

// writeToolHelpers writes the principals, the in-process server and the assertion helpers.
func (g *goEmit) writeToolHelpers(b *bytes.Buffer, plan componentPlan, rest bool, routes map[string]restRoute) error {
	transport, err := transportHelpers(plan, rest, routes)
	if err != nil {
		return err
	}
	if g.usesAs {
		writePrincipalTable(b, g.principals)
	}
	b.WriteString(transport)
	fmt.Fprintf(b, expectHelpers, "`")
	b.WriteString(refHelpers)
	b.WriteString(errorHasCodeHelper(rest))
	b.WriteString(matchHelpers)
	return nil
}

// toolsHelpers is the in-process tools server the generated MCP file runs:
// scenariocompose hands it the generated tools packages' handlers (built over
// the app's real authorization), it serves them on a fresh server and connects
// a client over an in-memory transport.
const toolsHelpers = `// toolRegistrar is the Handler of one generated MCP tools package: it adds
// that package's tools to a server.
type toolRegistrar interface {
	Register(srv *mcp.Server)
}

// toolSession is every scenario's subject: an MCP client session on the
// in-process server.
type toolSession struct {
	cs *mcp.ClientSession
}

// serveTools registers every handler on a fresh server, connects a client to
// it over an in-memory transport, and closes both when the test ends. The
// server serves its calls under ctx, the principal the session acts as; a ctx
// carrying none is served as one scenario principal, as the server's auth
// middleware authenticates every call.
func serveTools(ctx context.Context, t *testing.T, h *scenariohost.Host, handlers ...toolRegistrar) *toolSession {
	t.Helper()
	if _, ok := security.PrincipalFrom(ctx); !ok {
		ctx = security.WithPrincipal(ctx, security.Principal{Subject: "scenario"})
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "scenario-tools", Version: "testgen"}, nil)
	for _, r := range handlers {
		r.Register(server)
	}
	clientEnd, serverEnd := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, serverEnd, nil)
	if err != nil {
		t.Fatalf("testgen: serve tools: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "scenario", Version: "testgen"}, nil).Connect(h.Context(t), clientEnd, nil)
	if err != nil {
		t.Fatalf("testgen: connect to tools: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return &toolSession{cs: cs}
}

// Call calls tool name with args (a JSON object) and returns its structured
// output. A protocol error (an unknown tool, arguments the tool's input schema
// rejects) and a result flagged isError are both the call's error, carrying
// the tool's text.
func (s *toolSession) Call(ctx context.Context, name, args string) (any, error) {
	res, err := s.cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: json.RawMessage(args)})
	if err != nil {
		return nil, err
	}
	if res.IsError {
		var text []string
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				text = append(text, tc.Text)
			}
		}
		return nil, errors.New(name + ": " + strings.Join(text, "\n"))
	}
	return res.StructuredContent, nil
}

`

// toolsErrorHasCode is the MCP dialect's code match: a tool error reaches the
// client as text, so the code is matched in it.
const toolsErrorHasCode = `// errorHasCode matches the code as a substring of the error text: a tool
// error reaches the client as text, never as a typed layer error.
func errorHasCode(err error, code string) bool {
	return strings.Contains(err.Error(), code)
}

`
