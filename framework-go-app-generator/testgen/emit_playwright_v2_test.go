package testgen

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
)

const (
	shopDir      = "uitests/generated/shopClient/"
	shopElements = shopDir + "elements.gen.ts"
	shopRuntime  = shopDir + "runtime.gen.ts"
	shopSurface  = shopDir + "surface.spec.gen.ts"
)

// shopDoc is the fixture decoded for editing.
func shopDoc(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("testdata/project_tp.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

// shopBinding is the shopClient binding the tests edit.
func shopBinding(doc map[string]any) map[string]any {
	plan := doc["phaseArtifacts"].(map[string]any)["testPlan"].(map[string]any)["shopClient"].(map[string]any)
	return plan["bindings"].([]any)[0].(map[string]any)
}

func shopStepOf(doc map[string]any) map[string]any {
	return shopBinding(doc)["steps"].([]any)[0].(map[string]any)
}

func mustGenerate(t *testing.T, doc map[string]any) Output {
	t.Helper()
	out, err := generateDoc(t, doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func requireContains(t *testing.T, what, src string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(src, want) {
			t.Errorf("%s lacks %q:\n%s", what, want, src)
		}
	}
}

func requireAbsent(t *testing.T, what, src string, bads ...string) {
	t.Helper()
	for _, bad := range bads {
		if strings.Contains(src, bad) {
			t.Errorf("%s must not carry %q:\n%s", what, bad, src)
		}
	}
}

func TestPlaywright_LocatesByRoleFromDesign(t *testing.T) {
	out, err := generateShopStep(t, []string{act("check", tgt("order-done"))}, `[`+act("checked", tgt("order-done"))+`]`)
	if err != nil {
		t.Fatal(err)
	}
	spec := string(out.Generated[shopSpec])
	requireContains(t, "spec", spec, "locate(page, 'order-done')", "import { locate } from './elements.gen';")
	requireAbsent(t, "spec", spec, "getByTestId")
	els := string(out.Generated[shopElements])
	requireContains(t, "elements.gen.ts", els,
		"'order-done': { role: 'checkbox', name: 'Done', within: 'order-row', repeats: true },",
		"'orders-tab': { role: 'button', name: 'Orders', repeats: false },",
		"export function locate(page: Page, id: ElementId, nth?: number): Locator {",
		".getByRole(",
		"exact: true",
		"locate(page, spec.within)",
	)
	// a role located by structure carries no name option
	requireContains(t, "elements.gen.ts", els, "'orders-list': { role: 'list', name: '', repeats: false },")
}

func TestPlaywright_NthTargetIndexesTheLocator(t *testing.T) {
	nth := `{"target":{"element":"order-row","nth":2}}`
	out, err := generateShopStep(t, []string{act("hover", nth)}, `[`+act("visible", nth)+`]`)
	if err != nil {
		t.Fatal(err)
	}
	requireContains(t, "spec", string(out.Generated[shopSpec]), "locate(page, 'order-row', 2).hover()")
}

func TestPlaywright_LayerA_ImmediateUnderLatency(t *testing.T) {
	out, err := generateShopStep(t, []string{
		act("navigate", `{"route":"/orders"}`),
		act("check", tgt("order-done")),
		act("fill", `{"target":{"element":"order-edit"},"value":"rename"}`),
		act("click", tgt("orders-tab")),
	}, `[`+act("checked", tgt("order-done"))+`,`+act("text", `{"target":{"element":"order-count"},"text":"1 item left"}`)+`]`)
	if err != nil {
		t.Fatal(err)
	}
	spec := string(out.Generated[shopSpec])
	requireContains(t, "spec", spec,
		"await installLatency(page, Number(process.env.AIARCH_UI_LATENCY_MS ?? 1500));",
		"import { immediate, installLatency } from './runtime.gen';", // exactly what the body uses
		// Layer A: what the action itself changes in place, asserted at once
		"await locate(page, 'order-done').check();\n  await expect(locate(page, 'order-done')).toBeChecked(immediate);",
		"await locate(page, 'order-edit').fill('rename');\n  await expect(locate(page, 'order-edit')).toHaveValue('rename', immediate);",
		// what needs the server keeps the default timeout
		"await expect(locate(page, 'order-count')).toHaveText('1 item left');",
	)
	// the latency is installed before the first navigation
	if lat, nav := strings.Index(spec, "installLatency(page"), strings.Index(spec, "page.goto("); lat < 0 || nav < 0 || lat > nav {
		t.Errorf("installLatency must precede goto (%d, %d):\n%s", lat, nav, spec)
	}
	// a click changes nothing in place: no Layer A line after it
	if strings.Contains(spec, "click();\n  await expect(locate(page, 'orders-tab'))") {
		t.Errorf("a click has no Layer A observable:\n%s", spec)
	}
	rt := string(out.Generated[shopRuntime])
	requireContains(t, "runtime.gen.ts", rt,
		"export const immediate = { timeout: Number(process.env.AIARCH_UI_IMMEDIATE_MS ?? 250) };",
		"export async function installLatency(",
		"page.route('**/*'",
		"export function assertNever(",
		"export async function arrange(",
		"/__scenariohost/",
		"X-Dev-Principal",
		"default:\n      return assertNever(",
	)
}

func TestPlaywright_UncheckAndSelectAreLayerA(t *testing.T) {
	out, err := generateShopStep(t, []string{act("uncheck", tgt("order-flag"))}, `[`+act("unchecked", tgt("order-flag"))+`]`)
	if err != nil {
		t.Fatal(err)
	}
	requireContains(t, "spec", string(out.Generated[shopSpec]), "await expect(locate(page, 'order-flag')).not.toBeChecked(immediate);")
}

func TestPlaywright_SurfaceSpecCoversEveryElement(t *testing.T) {
	out := generate(t)
	surf := string(out.Generated[shopSurface])
	requireContains(t, "surface spec", surf,
		"test('surface — orders'",
		"await page.goto('/orders');",
		"await expect(locate(page, 'orders-tab')).toBeAttached();",
		"await expect(locate(page, 'orders-list')).toBeAttached();",
		"await expect(locate(page, 'order-count')).toBeAttached();",
	)
	// one item per row is not on an empty page, nor is anything inside it
	requireAbsent(t, "surface spec", surf, "'order-row'", "'order-done'", "'order-edit'")
	// the design is checked before any scenario runs
	cfg := string(out.Generated[shopConfig])
	requireContains(t, "config", cfg, "name: 'surface'", "dependencies: ['surface']", "video: 'on'", "trace: 'on'", "workers: 1,")
	// the surface check is not a scenario verdict
	requireContains(t, "reporter", string(out.Generated[shopReporter]), "'surface'")
}

func TestPlaywright_ActionSwitchExhaustive(t *testing.T) {
	// Go-side exhaustiveness is compile-enforced (gochecksumtype over the sealed UIAction); this pins
	// that every kind of the dialect is emitted through the design's locator.
	models := map[string]string{
		"navigate": `{"route":"/orders"}`, "press": `{"target":{"element":"order-edit"},"key":"Enter"}`,
		"select": `{"target":{"element":"order-edit"},"option":"o"}`, "fill": `{"target":{"element":"order-edit"},"value":"v"}`,
	}
	for _, k := range methodcheck.UIActionKinds() {
		model, ok := models[k]
		if !ok {
			model = tgt("order-row")
		}
		a, err := methodcheck.ParseUIAction(act(k, model))
		if err != nil {
			t.Fatalf("action %s: %v", k, err)
		}
		line := playwrightActionLine(a)
		if line == "" {
			t.Errorf("action %s has no emitter", k)
		}
		if k != "navigate" && !strings.Contains(line, "locate(page, ") {
			t.Errorf("action %s does not locate through the design: %s", k, line)
		}
	}
}

func TestPlaywright_NoHooksEmitted(t *testing.T) {
	out := generate(t)
	for p, src := range out.Generated {
		if strings.HasSuffix(p, "hooks.ts") {
			t.Errorf("a hooks file was generated: %s", p)
		}
		if strings.HasSuffix(p, ".ts") && strings.Contains(string(src), "hooks") {
			t.Errorf("%s still mentions hooks:\n%s", p, src)
		}
	}
	if _, has := reflect.TypeFor[Output]().FieldByName("HooksOnce"); has {
		t.Error("Output.HooksOnce must be deleted: no generator emits an agent-owned file")
	}
}

func TestPlaywright_EmptyObservationRefusedAtGeneration(t *testing.T) {
	for name, observation := range map[string]string{"empty array": `[]`, "no observation": ``} {
		_, err := generateShopStep(t, []string{act("click", tgt("orders-tab"))}, observation)
		if err == nil || !strings.Contains(err.Error(), "asserts nothing") || !strings.Contains(err.Error(), "shopClient browse-orders-P1") {
			t.Errorf("%s: err %v, want a refusal naming the scenario and saying the step asserts nothing", name, err)
		}
	}
}

func TestPlaywright_UnknownElementRefused(t *testing.T) {
	_, err := generateShopStep(t, []string{act("click", tgt("no-such-button"))}, `[`+act("visible", tgt("orders-list"))+`]`)
	if err == nil || !strings.Contains(err.Error(), `"no-such-button"`) || !strings.Contains(err.Error(), "uiDesign") {
		t.Errorf("err %v, want a refusal naming the element the design lacks", err)
	}
	_, err = generateShopStep(t, []string{act("click", tgt("orders-tab"))}, `[`+act("visible", tgt("ghost"))+`]`)
	if err == nil || !strings.Contains(err.Error(), `"ghost"`) {
		t.Errorf("err %v, want a refusal naming the observed element the design lacks", err)
	}
}

func TestPlaywright_NoDesignRefusedWhenAStepNeedsOne(t *testing.T) {
	doc := shopDoc(t)
	delete(doc["phaseArtifacts"].(map[string]any), "uiDesign")
	_, err := generateDoc(t, doc)
	if err == nil || !strings.Contains(err.Error(), "shopClient") || !strings.Contains(err.Error(), "uiDesign") {
		t.Errorf("err %v, want a refusal naming the surface with no uiDesign", err)
	}
}

func TestPlaywright_ArrangeOverRESTAndProbes(t *testing.T) {
	doc := shopDoc(t)
	b := shopBinding(doc)
	b["arrange"] = []any{
		map[string]any{"kind": "contractCall", "model": map[string]any{
			"component": "orderManager", "operation": "GetOrder",
			"inputs": []any{map[string]any{"name": "orderId", "value": lit(json.RawMessage(`"o-1"`))}},
			"expect": map[string]any{"result": `{"id":"o-1"}`, "errorExpected": false},
		}},
		map[string]any{"kind": "advanceClock", "model": map[string]any{"duration": "36h"}},
		map[string]any{"kind": "failNext", "model": map[string]any{"component": "billingManager", "operation": "GetInvoice", "errorCode": "NotFound"}},
		map[string]any{"kind": "seedResource", "model": map[string]any{"resource": "ordersDB", "fixture": map[string]any{"rows": 2}}},
	}
	shopStepOf(doc)["probes"] = []any{map[string]any{
		"for": "inv-req", "component": "billingManager", "operation": "GetInvoice",
		"inputs": []any{map[string]any{"name": "invoiceId", "value": lit(json.RawMessage(`"inv-o-1"`))}},
		"expect": map[string]any{"result": `{"id":"inv-o-1","state":"requested"}`, "errorExpected": false},
	}}
	out := mustGenerate(t, doc)
	spec := string(out.Generated[shopSpec])
	requireContains(t, "spec", spec,
		"await arrange(testInfo, { kind: 'contractCall',",
		"await arrange(testInfo, { kind: 'advanceClock', duration: '36h' });",
		"await arrange(testInfo, { kind: 'failNext', component: 'billingManager', operation: 'GetInvoice', errorCode: 'NotFound' });",
		"await arrange(testInfo, { kind: 'seedResource', resource: 'ordersDB', fixture: {\"rows\":2} });",
		"await probe(testInfo, { verb: ",
		"inv-o-1",
	)
	// arrange runs before the first action, probes after the step's observation
	if a, n, p := strings.Index(spec, "await arrange("), strings.Index(spec, "page.goto("), strings.Index(spec, "await probe("); a < 0 || a > n || p < n {
		t.Errorf("order wrong (arrange %d, goto %d, probe %d):\n%s", a, n, p, spec)
	}
}

func TestPlaywright_ArrangeRefusesWhatHasNoRoute(t *testing.T) {
	doc := shopDoc(t)
	shopBinding(doc)["arrange"] = []any{map[string]any{"kind": "contractCall", "model": map[string]any{
		"component": "noSuchManager", "operation": "Do", "inputs": []any{}, "expect": map[string]any{"result": "{}", "errorExpected": false},
	}}}
	if _, err := generateDoc(t, doc); err == nil || !strings.Contains(err.Error(), "noSuchManager") {
		t.Errorf("err %v, want a refusal naming the component with no route", err)
	}
}

// A binding observable is asserted immediately when the actor sees it without waiting on a component:
// the output the derivation marks Immediate and names the element.
func TestPlaywright_ImmediateElementsFromActorOutcome(t *testing.T) {
	design := methodcheck.UIDesignRecord{Screens: []methodcheck.UIScreen{{ID: "s", Route: "/orders", Elements: []methodcheck.UIElement{
		{ID: "order-done", Role: methodcheck.UIRoleCheckbox, Name: "Done", Repeats: true},
		{ID: "order-count", Role: methodcheck.UIRoleStatus, Name: "Items left"},
	}}}}
	oc := scenario.Outcome{ExpectedOutputs: []scenario.Output{
		{Node: "n1", Kind: "sendSignal", Label: "Order done is shown", Immediate: true},
		{Node: "n2", Kind: "sendSignal", Label: "Items left updated", Immediate: false},
	}}
	got := immediateElements(oc, design)
	if !got["order-done"] || got["order-count"] {
		t.Errorf("immediate elements = %v, want only order-done", got)
	}
}

func TestPlaywright_RuntimeIsTheEmbeddedSource(t *testing.T) {
	out := generate(t)
	if got := string(out.Generated[shopRuntime]); got != genHeader+"\n"+tsRuntimeSource {
		t.Errorf("runtime.gen.ts must be the embedded tsruntime/runtime.ts under the generated header:\n%s", got)
	}
}
