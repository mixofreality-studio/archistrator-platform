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
	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
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
)

func (d dialect) String() string {
	if d == dialectMCP {
		return "mcp"
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

// emitMCP emits one MCP client package's generated scenarios file and, when
// any binding is runnable, its once-emitted hooks file. One package serves one
// tools surface, so it hosts exactly one MCP client contract.
func emitMCP(out *Output, cfg Config, goPkg, stereotype string, plans []componentPlan) error {
	if len(plans) != 1 {
		return fmt.Errorf("testgen: %s hosts several MCP client contracts (%s); a package serves one tools surface", goPkg, hooksSubject(plans))
	}
	plan := plans[0]
	g := &goEmit{cfg: cfg, goPkg: goPkg, pkg: path.Base(goPkg), imports: map[string]bool{}, owners: map[string]string{}}
	if err := g.claimPlan(plan); err != nil {
		return err
	}
	for _, bs := range plan.Scenarios {
		if bs.Binding.Skip == nil {
			g.subject = true
		}
		g.emitToolScenario(plan, bs)
	}
	src, err := g.toolsFile(plan)
	if err != nil {
		return err
	}
	out.Generated[path.Join(goPkg, stereotype+"_scenarios.gen_test.go")] = src
	if g.subject {
		hooks, err := g.toolsHooksFile(plan)
		if err != nil {
			return err
		}
		out.HooksOnce[path.Join(goPkg, stereotype+"_hooks_test.go")] = hooks
	}
	return nil
}

// emitToolScenario emits one binding as a test: serve the hooks' tools
// in-process, then per step call the tool (or its hook) and check the
// expectation and probes.
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
		fmt.Fprintf(&g.body, "\t\tsubject := serveTools(t, h, %s(t, h))\n", g.subjectName(plan))
		if len(bs.Steps) == 0 {
			g.body.WriteString("\t\t_ = subject // the binding carries no steps\n")
		}
		for _, st := range bs.Steps {
			g.emitToolStep(plan, bs, st)
		}
	}
	g.body.WriteString("\t})\n}\n\n")
}

// emitToolStep emits one bound stimulus: the tool call with the binding's
// inputs as its argument object (or the step hook with them), the
// expectation, then the probes.
func (g *goEmit) emitToolStep(plan componentPlan, bs boundScenario, st boundStep) {
	id, seq := bs.Scenario.ID, st.Bind.Seq
	args := rawString(inputsJSON(plan.Contract, st.Op, st.Bind.Inputs))
	fmt.Fprintf(&g.body, "\t\t// step %d: %s → tool %s (node %s%s)\n", seq, st.Op.Name, toolName(st.Op), st.Stim.Node, guardNote(bs.Scenario))
	if st.Bind.Hook {
		fmt.Fprintf(&g.body, "\t\tout%d, err%d := %s(t, h, subject, %s)\n", seq, seq, g.stepName(plan, id, seq), args)
	} else {
		fmt.Fprintf(&g.body, "\t\tout%d, err%d := subject.Call(h.Context(t), %q, %s)\n", seq, seq, toolName(st.Op), args)
	}
	fmt.Fprintf(&g.body, "\t\texpect(t, %q, %d, out%d, err%d, %s)\n", id, seq, seq, seq, rawString(expectJSON(st.Bind.Expect)))
	if len(st.Bind.Unobservable) > 0 {
		fmt.Fprintf(&g.body, "\t\t// unobservable: %s\n", strings.Join(st.Bind.Unobservable, ", "))
	}
	for i, pr := range st.Bind.Probes {
		g.emitToolProbe(plan, bs, st, i+1, pr)
	}
}

// emitToolProbe emits one probe: a tool call inline when it names one of the
// contract's own tools, a probe hook when it reads another component.
func (g *goEmit) emitToolProbe(plan componentPlan, bs boundScenario, st boundStep, n int, pr methodcheck.Probe) {
	id, seq := bs.Scenario.ID, st.Bind.Seq
	v := fmt.Sprintf("%d_%d", seq, n)
	fmt.Fprintf(&g.body, "\t\t// probe %d.%d: %s.%s for %s\n", seq, n, pr.Component, pr.Operation, pr.For)
	if op, inline := g.inlineProbe(plan, pr); inline {
		fmt.Fprintf(&g.body, "\t\tpout%s, perr%s := subject.Call(h.Context(t), %q, %s)\n", v, v, toolName(op), rawString(inputsJSON(plan.Contract, op, pr.Inputs)))
	} else {
		fmt.Fprintf(&g.body, "\t\tpout%s, perr%s := %s(t, h, subject)\n", v, v, g.probeName(plan, id, seq, n))
	}
	fmt.Fprintf(&g.body, "\t\texpect(t, %q, %d, pout%s, perr%s, %s)\n", fmt.Sprintf("%s probe %d.%d", id, seq, n), seq, v, v, rawString(expectJSON(pr.Expect)))
}

// toolsFile assembles the MCP client's generated file: header, package
// clause, imports, TestMain (plain: a client runs no workflow of its own), the
// scenario tests and, when any runs, the tools server and assertion helpers.
func (g *goEmit) toolsFile(plan componentPlan) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(genHeader + "\n")
	fmt.Fprintf(&b, "// Source: project.json .phaseArtifacts.testPlan.%s × scenario.ForComponent(%q)\n", plan.Key, plan.Key)
	fmt.Fprintf(&b, "\npackage %s_test\n\n", g.pkg)
	imps := map[string]string{"testing": "", scenarioHostImport: ""}
	if g.subject {
		for _, p := range []string{"context", "encoding/json", "errors", "reflect", "strings", mcpSDKImport} {
			imps[p] = ""
		}
	}
	g.writeImports(&b, imps)
	fmt.Fprintf(&b, "func TestMain(m *testing.M) { scenariohost.Main(m, %s) }\n\n", strconv.Quote(plan.Key))
	b.Write(g.body.Bytes())
	if g.subject {
		b.WriteString(toolsHelpers)
		fmt.Fprintf(&b, expectHelpers, "`")
		b.WriteString(toolsErrorHasCode)
		b.WriteString(matchHelpers)
	}
	src, err := format.Source(b.Bytes())
	if err != nil {
		return nil, fmt.Errorf("testgen: %s: generated file does not format: %w\n%s", g.goPkg, err, b.String())
	}
	return src, nil
}

// toolsHooksFile emits the once-generated, agent-owned hooks file of an MCP
// client package: the subject constructor (the tools server), a step stub per
// Hook:true step and a probe stub per probe on another component. It imports
// nothing the hooks of any component would not: the MCP SDK stays in the
// generated file, behind toolServer / toolSession.
func (g *goEmit) toolsHooksFile(plan componentPlan) ([]byte, error) {
	var b bytes.Buffer
	fmt.Fprintf(&b, "// Hooks for %s scenario tests. Generated ONCE by testgen; owned by the construction agent.\n", plan.Key)
	b.WriteString("//\n// Every func here is a hook the generated scenarios file calls; the drift check\n// (testgen -check) requires exactly this symbol set, so add or remove hooks by\n// changing the test plan, not this file. Fill the bodies.\n\n")
	fmt.Fprintf(&b, "package %s_test\n\n", g.pkg)
	g.writeImports(&b, map[string]string{"testing": "", scenarioHostImport: ""})
	subject := g.subjectName(plan)
	fmt.Fprintf(&b, `// %s builds the in-process MCP server the scenarios call: the Handler
// of every generated MCP tools package serving the tools below, bound to real
// managers on the host's stack, and Context when the tools need an identity
// (the principal they read from it).
// Tools: %s.
// FILL: construct the handlers.
func %s(t *testing.T, h *scenariohost.Host) toolServer {
	t.Helper()
	t.Fatal("FILL %s")
	return toolServer{}
}

`, subject, strings.Join(boundTools(plan), ", "), subject, subject)
	for _, bs := range plan.Scenarios {
		if bs.Binding.Skip != nil {
			continue
		}
		for _, st := range bs.Steps {
			g.writeToolStepHooks(&b, plan, bs, st)
		}
	}
	src, err := format.Source(b.Bytes())
	if err != nil {
		return nil, fmt.Errorf("testgen: %s: hooks file does not format: %w\n%s", g.goPkg, err, b.String())
	}
	return src, nil
}

// boundTools is every tool a runnable binding's steps or same-contract probes
// call, sorted and distinct.
func boundTools(plan componentPlan) []string {
	seen := map[string]bool{}
	for _, bs := range plan.Scenarios {
		if bs.Binding.Skip != nil {
			continue
		}
		for _, st := range bs.Steps {
			for _, tool := range stepTools(plan, st) {
				seen[tool] = true
			}
		}
	}
	return sortedKeys(seen)
}

// stepTools is the tool one step calls and the tools its same-contract
// probes call.
func stepTools(plan componentPlan, st boundStep) []string {
	tools := []string{toolName(st.Op)}
	for _, pr := range st.Bind.Probes {
		if scenario.Normalize(pr.Component) != scenario.Normalize(plan.Key) {
			continue
		}
		if op, ok := operationNamed(plan.Contract, pr.Operation); ok {
			tools = append(tools, toolName(op))
		}
	}
	return tools
}

// writeToolStepHooks writes the step stub of a hooked tool step and the probe
// stub of each of its cross-component probes.
func (g *goEmit) writeToolStepHooks(b *bytes.Buffer, plan componentPlan, bs boundScenario, st boundStep) {
	id, seq := bs.Scenario.ID, st.Bind.Seq
	if st.Bind.Hook {
		name := g.stepName(plan, id, seq)
		fmt.Fprintf(b, `// %s — %s, step %d: %s (tool %s)
// Projected stimulus (JSON): %s
// Binding (JSON): %s
// FILL: arrange state so that guards %s hold, call the tool —
// subject.Call(h.Context(t), %q, args) — and return its result.
func %s(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	t.Fatal("FILL %s")
	return nil, nil
}

`, name, bs.Scenario.Title, seq, st.Op.Name, toolName(st.Op), compactJSON(st.Stim), compactJSON(st.Bind), guardList(bs), toolName(st.Op), name, name)
	}
	for i, pr := range st.Bind.Probes {
		if _, inline := g.inlineProbe(plan, pr); inline {
			continue
		}
		name := g.probeName(plan, id, seq, i+1)
		fmt.Fprintf(b, `// %s — %s, step %d, probe %d: %s.%s proves %s.
// Probe (JSON): %s
// FILL: call %s.%s with the probe's inputs on the host's stack and return its result.
func %s(t *testing.T, h *scenariohost.Host, subject *toolSession) (any, error) {
	t.Helper()
	t.Fatal("FILL %s")
	return nil, nil
}

`, name, bs.Scenario.Title, seq, i+1, pr.Component, pr.Operation, pr.For, compactJSON(pr), pr.Component, pr.Operation, name, name)
	}
}

// toolsHelpers is the in-process tools server the generated MCP file runs:
// the hooks hand it the generated tools packages' handlers, it serves them on
// a fresh server and connects a client over an in-memory transport.
const toolsHelpers = `// toolRegistrar is the Handler of one generated MCP tools package: it adds
// that package's tools to a server.
type toolRegistrar interface {
	Register(srv *mcp.Server)
}

// toolServer is what newSubject builds: the handlers whose tools the
// scenarios call, and the context the in-process server serves them under
// (nil: the host's) — the one place a principal the tools read is set.
type toolServer struct {
	Handlers []toolRegistrar
	Context  context.Context
}

// toolSession is every scenario's subject: an MCP client session on the
// in-process server.
type toolSession struct {
	cs *mcp.ClientSession
}

// serveTools registers every handler on a fresh server, connects a client to
// it over an in-memory transport, and closes both when the test ends.
func serveTools(t *testing.T, h *scenariohost.Host, srv toolServer) *toolSession {
	t.Helper()
	ctx := srv.Context
	if ctx == nil {
		ctx = h.Context(t)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "scenario-tools", Version: "testgen"}, nil)
	for _, r := range srv.Handlers {
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
