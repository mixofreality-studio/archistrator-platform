package testgen

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/internal/testfixtures"
)

// The runtime every generated Playwright spec runs on typechecks under strict compiler options, and
// exposes what an acceptance spec calls: a contract call and an MCP tool call, each as a named
// actor. The usage file is what Task 4.4's generated specs write; this test pins the runtime's side
// of that contract. It needs npm (the generated TypeScript cannot be proven to typecheck without it).
func TestRuntime_TypeChecks(t *testing.T) {
	if _, err := exec.LookPath("npm"); err != nil {
		t.Fatal("npm is required: the runtime cannot be proven to typecheck without it")
	}
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("runtime.ts", tsRuntimeSource)
	write("usage.ts", `import { expect } from '@playwright/test';
import type { TestInfo } from '@playwright/test';
import { contractCall, mcpCall, probe, type ContractCall, type ToolCall } from './runtime';

export async function usage(testInfo: TestInfo): Promise<unknown> {
  const as: ContractCall = { verb: 'GET', path: '/api/v1/list/openList', args: {}, expect: { errorExpected: false }, as: 'agent-1' };
  await probe(testInfo, as);
  await contractCall(testInfo, as);
  const tool: ToolCall = { tool: 'listOpenList', args: {}, expect: { errorExpected: false }, as: 'agent-1' };
  expect(tool.as).toBe('agent-1');
  return mcpCall(testInfo, tool);
}
`)
	write("package.json", `{"name":"runtime-typecheck","private":true,"type":"module"}`)
	write("tsconfig.json", `{"compilerOptions":{"target":"ES2022","module":"ESNext","moduleResolution":"bundler","lib":["ES2023","DOM"],"types":["node"],"strict":true,"noUncheckedIndexedAccess":true,"noEmit":true,"skipLibCheck":true},"files":["runtime.ts","usage.ts"]}`)

	// The uitests bootstrap's own ranges (webgen/templates/bootstrap/uitests.package.json.tmpl).
	install := exec.Command("npm", "install", "--no-audit", "--no-fund", "--prefer-offline", "--ignore-scripts",
		"@playwright/test@^1.50.0", "@types/node@^25.0.3", "typescript@~5.9.3")
	install.Dir = dir
	if out, err := install.CombinedOutput(); err != nil {
		t.Fatalf("npm install: %v\n%s", err, out)
	}
	tsc := exec.Command(filepath.Join(dir, "node_modules", ".bin", "tsc"), "-p", "tsconfig.json")
	tsc.Dir = dir
	if out, err := tsc.CombinedOutput(); err != nil {
		t.Fatalf("tsc: %v\n%s", err, out)
	}
}

// ---- the acceptance emitters (FPC plan 4, Task 4.4) ----
//
// The fixture is the todomvc conformance project with an acceptance plan added in the test: a
// web-bearing variant (a REST step from the client plan by planRef, a probe of what the member
// sees in the browser) and a machine-only one (a REST step, a probe of the Manager as the member).

const (
	accWebVariant  = "work-the-list-P1@todoApi"
	accGoVariant   = "resume-the-list-P1@todoApi"
	accWebSpec     = "uitests/generated/acceptance/" + accWebVariant + ".spec.gen.ts"
	accGoFile      = "internal/acceptance/acceptance_scenarios.gen_test.go"
	accModulePath  = "example.com/app/server"
	accBrowserUser = "team-member"
)

func accLit(v any) map[string]any {
	return map[string]any{"kind": "literal", "model": map[string]any{"json": v}}
}

func accPlanRef(component, scenarioID string, seq int, input string) map[string]any {
	return map[string]any{"kind": "planRef", "model": map[string]any{"component": component, "scenario": scenarioID, "seq": seq, "input": input}}
}

// acceptanceDoc is the todomvc fixture plus the two bound variants above; mutate edits the plan.
func acceptanceDoc(t *testing.T, mutate func(plan map[string]any)) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(testfixtures.Fixture(t, "todomvc"), &doc); err != nil {
		t.Fatal(err)
	}
	webProbe := map[string]any{
		"for": "out-team-member", "component": "todoWebApp", "operation": "OpenList", "as": accBrowserUser,
		"inputs": []any{map[string]any{"name": "action", "value": accLit(map[string]any{"kind": "navigate", "model": map[string]any{"route": "#/"}})}},
		"expect": map[string]any{"result": `[{"kind":"text","model":{"target":{"element":"items-left"},"text":"1 item left"}}]`, "errorExpected": false},
	}
	managerProbe := map[string]any{
		"for": "out-team-member", "component": "listManager", "operation": "OpenList", "as": accBrowserUser,
		"inputs": []any{
			map[string]any{"name": "owner", "value": accLit("bob")},
			map[string]any{"name": "view", "value": accLit("all")},
		},
		"expect": map[string]any{"errorExpected": true, "errorCode": "Refused"},
	}
	plan := map[string]any{
		"principals": map[string]any{
			"team-member": map[string]any{"subject": "alice", "roles": []any{"member"}},
			"team-agent": map[string]any{"subject": "agent-1", "roles": []any{"agent"}, "onBehalfOf": "alice",
				"intents": []any{"read", "add"}},
		},
		"browserActor": accBrowserUser,
		"bindings": []any{
			map[string]any{"scenario": accWebVariant, "arrange": []any{}, "steps": []any{map[string]any{
				"seq": 1, "operation": "ListAddTodo",
				"inputs": []any{
					map[string]any{"name": "owner", "value": accPlanRef("todoApi", "work-the-list-P1", 1, "owner")},
					map[string]any{"name": "wording", "value": accPlanRef("todoApi", "work-the-list-P1", 1, "wording")},
					map[string]any{"name": "requestId", "value": accPlanRef("todoApi", "work-the-list-P1", 1, "requestId")},
				},
				"expect": map[string]any{"result": `{"outcome":"added"}`, "errorExpected": false},
				"probes": []any{webProbe},
			}}},
			map[string]any{"scenario": accGoVariant, "arrange": []any{}, "steps": []any{map[string]any{
				"seq": 1, "operation": "ListOpenList",
				"inputs": []any{
					map[string]any{"name": "owner", "value": accPlanRef("todoApi", "resume-the-list-P1", 1, "owner")},
					map[string]any{"name": "view", "value": accPlanRef("todoApi", "resume-the-list-P1", 1, "view")},
				},
				"expect": map[string]any{"errorExpected": true, "errorCode": "Refused"},
				"probes": []any{managerProbe},
			}}},
		},
	}
	if mutate != nil {
		mutate(plan)
	}
	doc["phaseArtifacts"].(map[string]any)["acceptancePlan"] = plan
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func acceptanceOutput(t *testing.T) Output {
	t.Helper()
	out, err := Generate(acceptanceDoc(t, nil), Config{ModulePath: accModulePath})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return out
}

// A variant with a browser step or a browser probe is a Playwright spec: the non-browser step is an
// mcpCall or contractCall as its actor, the browser probe is a navigation and the probe's observables
// as the browser actor. It is no Go test.
func TestAcceptance_WebBearingVariantIsAPlaywrightSpec(t *testing.T) {
	out := acceptanceOutput(t)
	spec, ok := out.Generated[accWebSpec]
	if !ok {
		t.Fatalf("missing %s; have %v", accWebSpec, keys(out.Generated))
	}
	for _, want := range []string{
		"await contractCall(testInfo, {",
		"as: principals['team-agent']",
		"page.goto('#/')",
		".toHaveText('1 item left')",
		"import { locate } from '../todoWebApp/elements.gen';",
		"scenario', description: 'work-the-list-P1@todoApi'",
	} {
		if !strings.Contains(string(spec), want) {
			t.Errorf("missing %q in\n%s", want, spec)
		}
	}
	for _, f := range []string{"runtime.gen.ts", "playwright.config.gen.ts", "reporter.gen.ts"} {
		if _, ok := out.Generated["uitests/generated/acceptance/"+f]; !ok {
			t.Errorf("missing uitests/generated/acceptance/%s", f)
		}
	}
	if goFile, ok := out.Generated[accGoFile]; ok && strings.Contains(string(goFile), "work_the_list_P1_todoApi") {
		t.Errorf("a web-bearing variant must not also be a Go test:\n%s", goFile)
	}
}

// A variant with no browser step or probe is a Go test over the wire in internal/acceptance: one
// session per (client, actor), and a probe of a Manager as another actor is a call made as that actor.
func TestAcceptance_MachineOnlyVariantIsGo(t *testing.T) {
	out := acceptanceOutput(t)
	src, ok := out.Generated[accGoFile]
	if !ok {
		t.Fatalf("missing %s; have %v", accGoFile, keys(out.Generated))
	}
	for _, want := range []string{
		"package acceptance_test",
		"func TestMain(m *testing.M) { scenariohost.Main(m, \"acceptance\") }",
		"func TestScenario_resume_the_list_P1_todoApi(t *testing.T) {",
		`h.RunScenario(t, "acceptance", "resume-the-list-P1@todoApi"`,
		`app := h.LaunchDevstack(t, "", principal("team-agent"), principal("team-member"))`,
		`scenariohost.RESTClient(t, app, principal("team-agent"), clientRoutesTodoApi)`,
		`.Call(h.Context(t), "listOpenList"`,
		`pc.ManagerContextAs(t, principal("team-member"))`,
		"var clientRoutesTodoApi = map[string]scenariohost.Route{",
	} {
		if !strings.Contains(string(src), want) {
			t.Errorf("missing %q in\n%s", want, src)
		}
	}
	if strings.Contains(string(src), "work_the_list_P1") {
		t.Errorf("the web-bearing variant is not a Go test:\n%s", src)
	}
	if _, ok := out.Generated["uitests/generated/acceptance/"+accGoVariant+".spec.gen.ts"]; ok {
		t.Error("a machine-only variant is not a Playwright spec")
	}
}

// A planRef is resolved at generation to the literal it names in the client plan: nothing generated
// carries a reference.
func TestAcceptance_PlanRefInlinesTheLiteral(t *testing.T) {
	out := acceptanceOutput(t)
	spec := string(out.Generated[accWebSpec])
	if !strings.Contains(spec, `"wording":"Buy milk"`) || !strings.Contains(spec, `"requestId":"work-the-list-P1-req-1"`) {
		t.Errorf("the REST step's args must carry the client plan's literals:\n%s", spec)
	}
	for path, src := range out.Generated {
		if strings.Contains(path, "acceptance") && strings.Contains(strings.ToLower(string(src)), "planref") {
			t.Errorf("%s carries a planRef", path)
		}
	}
}

// The browser is one identity per launch: a browser probe as another actor is a generation error that
// names the rule and the variant.
func TestAcceptance_WebProbeAsOtherActorRefused(t *testing.T) {
	doc := acceptanceDoc(t, func(plan map[string]any) {
		step := plan["bindings"].([]any)[0].(map[string]any)["steps"].([]any)[0].(map[string]any)
		step["probes"].([]any)[0].(map[string]any)["as"] = "team-agent"
	})
	_, err := Generate(doc, Config{ModulePath: accModulePath})
	if err == nil || !strings.Contains(err.Error(), "ATP-BROWSER") || !strings.Contains(err.Error(), accWebVariant) {
		t.Fatalf("want a generation error naming ATP-BROWSER and %s, got %v", accWebVariant, err)
	}
}

// A residual variant with no binding is named, never silently dropped.
func TestAcceptance_UnboundResidualIsAWarning(t *testing.T) {
	doc := acceptanceDoc(t, func(plan map[string]any) { plan["bindings"] = plan["bindings"].([]any)[:1] })
	out, err := Generate(doc, Config{ModulePath: accModulePath})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(out.Warnings, func(w string) bool {
		return strings.Contains(w, "acceptance") && strings.Contains(w, "no binding") && strings.Contains(w, accGoVariant)
	}) {
		t.Errorf("an unbound residual variant must be warned about: %v", out.Warnings)
	}
}

// A project with no acceptance plan emits no acceptance file.
func TestAcceptance_NoPlanEmitsNothing(t *testing.T) {
	out := todomvcOutput(t)
	for path := range out.Generated {
		if strings.Contains(path, "acceptance") {
			t.Errorf("no acceptancePlan is recorded, yet %s was emitted", path)
		}
	}
}

const stubAcceptanceList = `package list

import fwm "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"

type View string
type ListView struct{}

type ListManager interface {
	OpenList(ctx fwm.Context, owner string, view View) (ListView, error)
}
`

const stubAcceptanceCompose = `package scenariocompose

import (
	"testing"

	"example.com/app/server/internal/manager/list"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
)

func NewListManager(t *testing.T, h *scenariohost.Host) list.ListManager { return nil }
`

// The generated acceptance Go type-checks against the real scenario host (sessions, the launch and
// the probe contexts are the host's), beside the components it calls in-process.
func TestAcceptance_GeneratedGoCompiles(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles the real scenario host")
	}
	out := acceptanceOutput(t)
	root := writeModule(t, map[string]string{
		"internal/manager/list/list.go":       stubAcceptanceList,
		"internal/scenariocompose/compose.go": stubAcceptanceCompose,
		accGoFile:                             string(out.Generated[accGoFile]),
	}, moduleOpts{realHost: true})
	if msg, err := goIn(root, nil, "vet", "./internal/..."); err != nil {
		t.Fatalf("the generated acceptance file does not type-check: %v\n%s", err, msg)
	}
}
