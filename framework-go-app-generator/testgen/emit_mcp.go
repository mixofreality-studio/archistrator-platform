package testgen

import (
	"bytes"
	"fmt"
	"go/format"
	"path"
	"slices"
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

// emitMCP emits one machine client package's generated scenarios file. One
// package serves one tools surface, so it hosts exactly one machine client
// contract. Its subject is the app itself, launched by the test (LaunchDevstack) with every
// principal of the plan allowlisted, and the client a session on that server as the principal the
// plan records for each step's actor: MCP tool calls or REST requests over the wire, authorized by
// the composition root's own policy. A run of the null-subject discrimination nulls the Manager
// operation a step reaches in the launched process.
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
	// A machine client's file never imports its own package (it reaches the app over the wire), so its
	// name is free for a component package of the same name: a tool table is named after the Manager it
	// fronts, and a probe on that Manager imports it.
	delete(g.aliases, g.pkg)
	if err := g.claimPlan(plan); err != nil {
		return err
	}
	rest, err := isRESTPlan(plan)
	if err != nil {
		return err
	}
	for _, bs := range plan.Scenarios {
		if bs.Binding.Skip == nil {
			g.subject = true
		}
		g.emitToolScenario(plan, bs, rest)
	}
	if g.err != nil {
		return g.err
	}
	src, err := g.toolsFile(plan, rest, routes)
	if err != nil {
		return err
	}
	out.Generated[path.Join(goPkg, stereotype+"_scenarios.gen_test.go")] = src
	g.clientDiscriminations(out, plan, routes)
	return nil
}

// clientDiscriminations lists the null-subject runs of a machine client's scenarios: per scenario,
// one per distinct operation its steps call, nulling the Manager operation that call reaches
// ("<Iface>.<Op>", the name the composed server's applyNullOps matches) in the launched process,
// which inherits AIARCH_NULL_OPS from the test. A call that reaches no generated Manager operation
// names nothing to null, so it is a recorded skip and never a silent one.
func (g *goEmit) clientDiscriminations(out *Output, plan componentPlan, routes map[string]restRoute) {
	for _, bs := range plan.Scenarios {
		if bs.runnable() {
			g.clientScenarioRuns(out, plan, bs, routes)
		}
	}
}

// clientScenarioRuns is one runnable machine client scenario's runs: one per distinct operation its
// steps call, in step order.
func (g *goEmit) clientScenarioRuns(out *Output, plan componentPlan, bs boundScenario, routes map[string]restRoute) {
	var seen []string
	for _, st := range bs.Steps {
		if slices.Contains(seen, st.Op.Name) {
			continue
		}
		seen = append(seen, st.Op.Name)
		route, ok := routes[st.Op.Name]
		if !ok {
			out.Skipped = append(out.Skipped, DiscriminationSkip{Component: plan.Key, Scenario: bs.Scenario.ID, Reason: reasonClientOp + ": " + st.Op.Name})
			continue
		}
		out.Discriminate = append(out.Discriminate, Discrimination{
			Component: plan.Key, Scenario: bs.Scenario.ID, Op: st.Op.Name,
			Package: g.goPkg, Test: g.testName(plan, bs.Scenario.ID),
			NullOp: route.Manager + "." + route.Op,
		})
	}
}

// sessionVar names the session a principal's steps call through.
func sessionVar(principalName string) string {
	if principalName == "" {
		return "subject"
	}
	return "subjectAs" + exportName(ident(principalName))
}

// anonymousPrincipal is the identity a step runs as when the plan records none for its actor: the
// one the launched app serves a request as when the launch names no principal.
const anonymousPrincipal = "scenariohost.Principal{Subject: \"scenario\"}"

// emitToolScenario emits one binding as a test: launch the app with every principal the plan
// declares allowlisted, open a session on it once per principal the steps act as, run the arrange
// calls, then per step call the tool or route over the wire and check the expectation and probes.
func (g *goEmit) emitToolScenario(plan componentPlan, bs boundScenario, rest bool) {
	name := g.testName(plan, bs.Scenario.ID)
	fmt.Fprintf(&g.body, "// %s — %s (use case %s; path %s).\n", name, bs.Scenario.Title, bs.Scenario.UseCase, strings.Join(bs.Scenario.Path, " → "))
	fmt.Fprintf(&g.body, "func %s(t *testing.T) {\n", name)
	g.body.WriteString("\th := scenariohost.Start(t)\n")
	fmt.Fprintf(&g.body, "\th.RunScenario(t, %q, %q, func(t *testing.T) {\n", plan.Key, bs.Scenario.ID)
	switch {
	case bs.Binding.Skip != nil:
		fmt.Fprintf(&g.body, "\t\tt.Skip(%q)\n", bs.Binding.Skip.Reason+" (until "+bs.Binding.Skip.Until+")")
	default:
		fmt.Fprintf(&g.body, "\t\tapp := h.LaunchDevstack(t, \"\"%s)\n", g.launchArgs(plan, bs))
		if len(bs.Steps) == 0 {
			g.body.WriteString("\t\t_ = app // the binding carries no steps\n")
		}
		g.openSessions(plan, bs, rest)
		g.emitArrange(plan, bs)
		d := toolDialect{g: g}
		for _, st := range bs.Steps {
			g.emitStep(d, plan, bs, st)
		}
	}
	g.body.WriteString("\t})\n}\n\n")
}

// launchArgs is the principals the app is launched with, as extra LaunchDevstack arguments: every
// principal the plan declares, in name order (the first is the process principal), and the
// anonymous identity when some step's actor has none. The launch allowlists them all, so a session
// can be opened as any.
func (g *goEmit) launchArgs(plan componentPlan, bs boundScenario) string {
	var args []string
	for _, name := range sortedKeys(plan.Principals) {
		g.usesAs = true
		args = append(args, "principal("+strconv.Quote(name)+")")
	}
	for _, st := range bs.Steps {
		if stepPrincipal(plan, st) == "" {
			args = append(args, anonymousPrincipal)
			break
		}
	}
	if len(args) == 0 {
		return ""
	}
	return ", " + strings.Join(args, ", ")
}

// openSessions opens a session on the launched app once per principal the scenario's steps act as,
// in step order: MCP tool calls, or the routes of the REST client.
func (g *goEmit) openSessions(plan componentPlan, bs boundScenario, rest bool) {
	seen := map[string]bool{}
	for _, st := range bs.Steps {
		p := stepPrincipal(plan, st)
		if seen[p] {
			continue
		}
		seen[p] = true
		who := anonymousPrincipal
		if p != "" {
			g.usesAs = true
			who = "principal(" + strconv.Quote(p) + ")"
		}
		if rest {
			fmt.Fprintf(&g.body, "\t\t%s := scenariohost.RESTClient(t, app, %s, clientRoutes)\n", sessionVar(p), who)
			continue
		}
		fmt.Fprintf(&g.body, "\t\t%s := scenariohost.MCPSession(t, app, %s)\n", sessionVar(p), who)
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
		lines = append([]string{probeOpen}, lines...)
		return append(lines, fmt.Sprintf("return %s.Call(%s.Context(t), %q, args)", sessionVar(stepPrincipal(plan, st)), probeRecv, toolName(op)))
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
// scenario tests and, when any runs, the principals, the route table and assertion helpers.
func (g *goEmit) toolsFile(plan componentPlan, rest bool, routes map[string]restRoute) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(genHeader + "\n")
	fmt.Fprintf(&b, "// Source: project.json .phaseArtifacts.testPlan.%s × scenario.ForComponent(%q)\n", plan.Key, plan.Key)
	fmt.Fprintf(&b, "\npackage %s_test\n\n", g.pkg)
	g.writeImports(&b, g.toolsImports())
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

// toolsImports is the machine client file's import set: the test essentials always, the assertion
// helpers' and the called components' (the compose package among them) when any binding runs. The
// transport is the scenario host's, so no MCP SDK or HTTP package is imported.
func (g *goEmit) toolsImports() map[string]string {
	imps := map[string]string{"testing": "", scenarioHostImport: ""}
	if !g.subject {
		return imps
	}
	for _, p := range []string{"context", "encoding/json", "errors", "fmt", "reflect", "strings"} {
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

// writeToolHelpers writes the principals, the route table of a REST client and the assertion helpers.
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

// toolsErrorHasCode is the MCP dialect's code match: a tool error reaches the
// client as text, so the code is matched in it.
const toolsErrorHasCode = `// errorHasCode matches the code as a substring of the error text: a tool
// error reaches the client as text, never as a typed layer error.
func errorHasCode(err error, code string) bool {
	return strings.Contains(err.Error(), code)
}

`
