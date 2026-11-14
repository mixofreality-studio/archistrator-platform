package testgen

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/internal/testfixtures"
)

// ---- an mcpApp surface's specs run inside a harness MCP host (FPC plan 5, Task 5.4) ----
//
// The fixture is the shopClient plan of testdata/project_tp.json with its component's surface set
// to mcpApp: the same record as the web plan, so what differs in the output is the surface alone.

// shopSurfaceDoc is the shop fixture with the shop-client component stating surface, and its one
// step doing actions and observing observation.
func shopSurfaceDoc(t *testing.T, surface string, actions []string, observation string) map[string]any {
	t.Helper()
	doc := shopDoc(t)
	design := doc["slots"].(map[string]any)["5"].(map[string]any)["model"].(map[string]any)
	for _, c := range design["components"].([]any) {
		if comp := c.(map[string]any); comp["id"] == "shop-client" {
			comp["surface"] = surface
		}
	}
	step := shopStepOf(doc)
	inputs := make([]any, 0, len(actions))
	for _, a := range actions {
		inputs = append(inputs, map[string]any{"name": "action", "value": lit(json.RawMessage(a))})
	}
	step["inputs"] = inputs
	step["expect"] = map[string]any{"result": observation, "errorExpected": false}
	return doc
}

var mcpAppActions = []string{
	act("navigate", `{"route":"/orders"}`),
	act("check", tgt("order-done")),
}

func mcpAppObservation() string {
	return `[` + act("checked", tgt("order-done")) + `,` + act("text", `{"target":{"element":"order-count"},"text":"1 item left"}`) + `]`
}

func TestPlaywright_McpAppRunsInHost(t *testing.T) {
	mcp := mustGenerate(t, shopSurfaceDoc(t, "mcpApp", mcpAppActions, mcpAppObservation()))
	spec := string(mcp.Generated[shopSpec])
	requireContains(t, "mcpApp spec", spec,
		"appFrame",
		"openApp",
		"} from './runtime.gen';",
		"await openApp(page, '/orders');",
		"locate(appFrame(page), 'order-done')",
		"locate(appFrame(page), 'order-count')",
	)
	requireAbsent(t, "mcpApp spec", spec, "page.goto(", "locate(page,")
	requireContains(t, "elements.gen.ts", string(mcp.Generated[shopElements]),
		"locate(root: Page | FrameLocator", "FrameLocator")
	surface := string(mcp.Generated[shopSurface])
	requireContains(t, "surface.spec.gen.ts", surface, "openApp(page, ", "locate(appFrame(page), ")
	requireAbsent(t, "surface.spec.gen.ts", surface, "page.goto(", "locate(page,")
	requireContains(t, "runtime.gen.ts", string(mcp.Generated[shopRuntime]),
		"export const APP_FRAME = 'iframe#mcp-app';", "export function appFrame(", "export async function openApp(")

	// The same plan as a web surface emits none of these.
	web := mustGenerate(t, shopSurfaceDoc(t, "web", mcpAppActions, mcpAppObservation()))
	webSpec := string(web.Generated[shopSpec])
	requireContains(t, "web spec", webSpec, "page.goto('/orders')", "locate(page, 'order-done')")
	requireAbsent(t, "web spec", webSpec, "appFrame", "openApp", "FrameLocator")
	requireAbsent(t, "web elements.gen.ts", string(web.Generated[shopElements]), "FrameLocator")
	requireAbsent(t, "web surface.spec.gen.ts", string(web.Generated[shopSurface]), "appFrame", "openApp")
}

func TestPlaywright_McpAppRefusesURLObservable(t *testing.T) {
	doc := shopSurfaceDoc(t, "mcpApp", mcpAppActions, `[`+act("url", `{"route":"/orders"}`)+`]`)
	if _, err := generateDoc(t, doc); err == nil || !strings.Contains(err.Error(), "has no address bar") {
		t.Fatalf("err = %v, want one containing \"has no address bar\"", err)
	}
	// A web surface may observe its URL.
	if _, err := generateDoc(t, shopSurfaceDoc(t, "web", mcpAppActions, `[`+act("url", `{"route":"/orders"}`)+`]`)); err != nil {
		t.Fatalf("a web surface observes its URL: %v", err)
	}
}

// mcpAppAcceptanceDoc is the acceptance fixture with the web client an mcpApp.
func mcpAppAcceptanceDoc(t *testing.T, skip bool) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(acceptanceDoc(t, nil), &doc); err != nil {
		t.Fatal(err)
	}
	design := doc["slots"].(map[string]any)["5"].(map[string]any)["model"].(map[string]any)
	for _, c := range design["components"].([]any) {
		if comp := c.(map[string]any); comp["id"] == "todo-web-app" {
			comp["surface"] = "mcpApp"
		}
	}
	// The component's own plan observes URLs, which an mcpApp refuses; skip it so the acceptance
	// refusal is what the test reaches.
	clientPlan := doc["phaseArtifacts"].(map[string]any)["testPlan"].(map[string]any)["todoWebApp"].(map[string]any)
	for _, b := range clientPlan["bindings"].([]any) {
		b.(map[string]any)["skip"] = map[string]any{"reason": "the client plan is not under test here", "until": "2026-12-31"}
	}
	if skip {
		plan := doc["phaseArtifacts"].(map[string]any)["acceptancePlan"].(map[string]any)
		for _, b := range plan["bindings"].([]any) {
			if binding := b.(map[string]any); binding["scenario"] == accWebVariant {
				binding["skip"] = map[string]any{"reason": "acceptance through an MCP Apps host (E28)", "until": "2026-12-31"}
				binding["steps"] = []any{}
			}
		}
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestAcceptance_McpAppTargetRefusedUntilHosted(t *testing.T) {
	if _, err := Generate(mcpAppAcceptanceDoc(t, false), Config{ModulePath: accModulePath}); err == nil || !strings.Contains(err.Error(), "E28") {
		t.Fatalf("an unskipped variant driving an mcpApp: err = %v, want one containing \"E28\"", err)
	}
	out, err := Generate(mcpAppAcceptanceDoc(t, true), Config{ModulePath: accModulePath})
	if err != nil {
		t.Fatalf("a skipped variant is recorded, not refused: %v", err)
	}
	if _, ok := out.Generated[accWebSpec]; ok {
		t.Errorf("a skipped variant must not emit a live Playwright spec")
	}
	goFile := string(out.Generated[accGoFile])
	if !strings.Contains(goFile, "t.Skip(") || !strings.Contains(goFile, "E28") {
		t.Errorf("the skipped variant is not recorded as a skip naming its reason:\n%s", goFile)
	}
	// An unrelated plan is untouched.
	if _, err := Generate(testfixtures.Fixture(t, "todomvc"), Config{ModulePath: accModulePath}); err != nil {
		t.Fatalf("the todomvc fixture: %v", err)
	}
}
