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
		"X-Dev-Principal",
		"default:\n      return assertNever(",
	)
	// the scenario host's endpoint is never called: the composed server answers every unknown POST
	// with 405, so a runtime that probed for it would fail a scenario instead of skipping it
	requireAbsent(t, "runtime.gen.ts", rt, "callHost", "/__scenariohost/")
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

// A screen with no elements would generate a surface test that opens its route and asserts nothing
// (GC-1: no test that asserts nothing is representable), so generation refuses it.
func TestPlaywright_ScreenWithNoElementsRefusedAtGeneration(t *testing.T) {
	doc := shopDoc(t)
	design := doc["phaseArtifacts"].(map[string]any)["uiDesign"].(map[string]any)["shopClient"].(map[string]any)
	design["screens"] = append(design["screens"].([]any), map[string]any{"id": "archive", "route": "/archive", "elements": []any{}})
	_, err := generateDoc(t, doc)
	if err == nil || !strings.Contains(err.Error(), "shopClient") || !strings.Contains(err.Error(), `"archive"`) || !strings.Contains(err.Error(), "no elements") {
		t.Errorf("err %v, want a refusal naming the surface and the screen with no elements", err)
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
		"await probe(testInfo, { verb: ",
		"inv-o-1",
		// the scenario host serves no endpoint yet (the composed server answers an unknown POST with
		// 405): a scenario that arranges through it is skipped, by name, before it does anything
		"test.skip(true, 'arrange advanceClock, failNext, seedResource needs the local-profile /__scenariohost endpoint, which no generated server serves yet');",
	)
	requireAbsent(t, "spec", spec, "kind: 'advanceClock'", "kind: 'failNext'", "kind: 'seedResource'")
	// the skip precedes every action and every arrange call; arrange precedes the first action, probes follow it
	if sk, a, n, p := strings.Index(spec, "test.skip(true"), strings.Index(spec, "await arrange("), strings.Index(spec, "page.goto("), strings.Index(spec, "await probe("); sk < 0 || sk > a || a > n || p < n {
		t.Errorf("order wrong (skip %d, arrange %d, goto %d, probe %d):\n%s", sk, a, n, p, spec)
	}
}

// A scenario arranging only through contract calls is not skipped.
func TestPlaywright_ContractArrangeAloneIsNotSkipped(t *testing.T) {
	doc := shopDoc(t)
	shopBinding(doc)["arrange"] = []any{map[string]any{"kind": "contractCall", "model": map[string]any{
		"component": "orderManager", "operation": "GetOrder",
		"inputs": []any{map[string]any{"name": "orderId", "value": lit(json.RawMessage(`"o-1"`))}},
		"expect": map[string]any{"result": `{"id":"o-1"}`, "errorExpected": false},
	}}}
	spec := string(mustGenerate(t, doc).Generated[shopSpec])
	requireAbsent(t, "spec", spec, "test.skip(true")
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

// An output the derivation marks Immediate is reported, never matched against element names: the label
// is narrative, so a label that merely contains an element's name ("Order done is shown" contains the
// name of order-row, "Order") must not make that element immediate.
func TestPlaywright_ImmediateOutputIsWarnedNotGuessed(t *testing.T) {
	st := boundStep{Stim: scenario.Stimulus{ActorOutcome: scenario.Outcome{ExpectedOutputs: []scenario.Output{
		{Node: "n1", Kind: "sendSignal", Label: "Order done is shown", Immediate: true},
		{Node: "n2", Kind: "sendSignal", Label: "Items left updated", Immediate: false},
	}}}}
	st.Bind.Seq = 1
	got := immediateWarnings("shopClient", "browse-orders-P1", st)
	if len(got) != 1 || !strings.Contains(got[0], `"Order done is shown"`) || !strings.Contains(got[0], "shopClient browse-orders-P1 step 1") {
		t.Errorf("warnings = %q, want one naming the Immediate output only", got)
	}
}

func TestPlaywright_ElementNamedLikeAnImmediateLabelIsNotImmediate(t *testing.T) {
	// order-row is named "Order"; whatever the labels of the derived outputs say, an observable on it
	// is asserted under the default timeout, because only the action determines immediacy.
	out, err := generateShopStep(t, []string{act("click", tgt("orders-tab"))}, `[`+act("visible", tgt("order-row"))+`]`)
	if err != nil {
		t.Fatal(err)
	}
	spec := string(out.Generated[shopSpec])
	requireContains(t, "spec", spec, "await expect(locate(page, 'order-row')).toBeVisible();")
	requireAbsent(t, "spec", spec, "toBeVisible(immediate)", "immediate }")
}

// The imports name what the body used, not what its text happens to contain.
func TestPlaywright_ImportsFollowUseNotText(t *testing.T) {
	out, err := generateShopStep(t, []string{act("click", tgt("orders-tab"))}, `[`+act("text", `{"target":{"element":"order-count"},"text":"expect( locate( "}`)+`]`)
	if err != nil {
		t.Fatal(err)
	}
	requireContains(t, "spec", string(out.Generated[shopSpec]), "import { test, expect } from '@playwright/test';", "import { locate } from './elements.gen';")

	// a skipped-by-arrange-only spec that asserts through navigate/url uses neither locate nor a locator-free expect import
	out, err = generateShopStep(t, []string{act("navigate", `{"route":"/orders"}`)}, `[`+act("url", `{"route":"/orders"}`)+`]`)
	if err != nil {
		t.Fatal(err)
	}
	spec := string(out.Generated[shopSpec])
	requireContains(t, "spec", spec, "import { test, expect } from '@playwright/test';")
	requireAbsent(t, "spec", spec, "elements.gen")
}

func TestPlaywright_ContainerLoopRefused(t *testing.T) {
	doc := shopDoc(t)
	els := doc["phaseArtifacts"].(map[string]any)["uiDesign"].(map[string]any)["shopClient"].(map[string]any)["screens"].([]any)[0].(map[string]any)["elements"].([]any)
	for _, e := range els {
		m := e.(map[string]any)
		if m["id"] == "orders-list" {
			m["within"] = "order-row" // order-row is within orders-list
		}
	}
	_, err := generateDoc(t, doc)
	if err == nil || !strings.Contains(err.Error(), "loop") || !strings.Contains(err.Error(), "orders-list within order-row within orders-list") {
		t.Errorf("err %v, want a refusal naming the container loop", err)
	}
}

func TestPlaywright_RuntimeIsTheEmbeddedSource(t *testing.T) {
	out := generate(t)
	if got := string(out.Generated[shopRuntime]); got != genHeader+"\n"+tsRuntimeSource {
		t.Errorf("runtime.gen.ts must be the embedded tsruntime/runtime.ts under the generated header:\n%s", got)
	}
}
