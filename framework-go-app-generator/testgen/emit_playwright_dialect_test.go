package testgen

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// act builds a {"kind","model"} dialect envelope.
func act(kind, model string) string { return `{"kind":"` + kind + `","model":` + model + `}` }

func tgt(el string) string { return `{"target":{"element":"` + el + `"}}` }

// The emitter covers exactly the dialect methodcheck holds (and the derived browser
// contract's schema enumerates): one list, gated.
func TestPlaywrightDialect_KindsMatchMethodcheck(t *testing.T) {
	if got, want := UIActionKinds(), methodcheck.UIActionKinds(); !slices.Equal(got, want) {
		t.Errorf("testgen emits actions %v, the dialect is %v", got, want)
	}
	if got, want := UIObservableKinds(), methodcheck.UIObservableKinds(); !slices.Equal(got, want) {
		t.Errorf("testgen asserts observables %v, the dialect is %v", got, want)
	}
}

// Every kind of the dialect has an emitted spelling: the line is non-empty for a decoded
// instance of each kind.
func TestPlaywrightDialect_EveryKindIsSpelled(t *testing.T) {
	models := map[string]string{
		"navigate": `{"route":"/orders"}`, "press": `{"target":{"element":"e"},"key":"Enter"}`,
		"select": `{"target":{"element":"e"},"option":"o"}`, "fill": `{"target":{"element":"e"},"value":"v"}`,
		"text": `{"target":{"element":"e"},"text":"t"}`, "value": `{"target":{"element":"e"},"value":"v"}`,
		"count": `{"target":{"element":"e"},"n":2}`, "url": `{"route":"/orders"}`,
	}
	model := func(kind string) string {
		if m, ok := models[kind]; ok {
			return m
		}
		return tgt("e")
	}
	for _, k := range methodcheck.UIActionKinds() {
		a, err := methodcheck.ParseUIAction(act(k, model(k)))
		if err != nil {
			t.Fatalf("action %s: %v", k, err)
		}
		if playwrightActionLine(a) == "" {
			t.Errorf("action %s has no spelling", k)
		}
	}
	for _, k := range methodcheck.UIObservableKinds() {
		o, err := methodcheck.UnmarshalUIObservable([]byte(act(k, model(k))))
		if err != nil {
			t.Fatalf("observable %s: %v", k, err)
		}
		if playwrightObservableLine(o) == "" {
			t.Errorf("observable %s has no spelling", k)
		}
	}
}

// generateShopStep binds the fresh (non-archistrator) fixture's shopClient step to the
// given actions and observation and generates.
func generateShopStep(t *testing.T, actions []string, observation string) (Output, error) {
	t.Helper()
	raw, err := os.ReadFile("testdata/project_tp.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	plan := doc["phaseArtifacts"].(map[string]any)["testPlan"].(map[string]any)["shopClient"].(map[string]any)
	step := plan["bindings"].([]any)[0].(map[string]any)["steps"].([]any)[0].(map[string]any)
	inputs := make([]any, 0, len(actions))
	for _, a := range actions {
		inputs = append(inputs, map[string]any{"name": "action", "value": lit(json.RawMessage(a))})
	}
	step["inputs"] = inputs
	step["expect"] = map[string]any{"result": observation, "errorExpected": false}
	edited, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return Generate(edited, Config{ModulePath: "example.com/app/server"})
}

// A step states what a user does and sees: a checkbox step emits check() (which fails
// unless the box flips on the click itself) and toBeChecked().
func TestPlaywrightDialect_CheckboxStepGolden(t *testing.T) {
	out, err := generateShopStep(t, []string{
		act("navigate", `{"route":"/orders"}`),
		act("check", tgt("order-done")),
		act("uncheck", tgt("order-flag")),
		act("dblclick", tgt("order-title")),
		act("fill", `{"target":{"element":"order-edit"},"value":"rename"}`),
		act("press", `{"target":{"element":"order-edit"},"key":"Enter"}`),
		act("hover", tgt("order-row")),
	}, `[`+act("checked", tgt("order-done"))+`,`+act("unchecked", tgt("order-flag"))+`,`+
		act("text", `{"target":{"element":"order-count"},"text":"1 item left"}`)+`,`+
		act("text", `{"target":{"element":"order-edit"},"text":""}`)+`]`)
	if err != nil {
		t.Fatal(err)
	}
	spec := string(out.Generated[shopSpec])
	want := `  // step 1: OpenOrders (node ` // the node id follows
	if !strings.Contains(spec, want) {
		t.Fatalf("missing step header in\n%s", spec)
	}
	// each action is followed by what it changes in place (Layer A, asserted at once); the step's own
	// observables come after the last action, under the default timeout
	golden := `  await page.goto('/orders');
  await locate(page, 'order-done').check();
  await expect(locate(page, 'order-done')).toBeChecked(immediate);
  await locate(page, 'order-flag').uncheck();
  await expect(locate(page, 'order-flag')).not.toBeChecked(immediate);
  await locate(page, 'order-title').dblclick();
  await locate(page, 'order-edit').fill('rename');
  await expect(locate(page, 'order-edit')).toHaveValue('rename', immediate);
  await locate(page, 'order-edit').press('Enter');
  await locate(page, 'order-row').hover();
  await expect(locate(page, 'order-done')).toBeChecked();
  await expect(locate(page, 'order-flag')).not.toBeChecked();
  await expect(locate(page, 'order-count')).toHaveText('1 item left');
  await expect(locate(page, 'order-edit')).toHaveText('');
`
	if !strings.Contains(spec, golden) {
		t.Errorf("spec does not carry the golden step:\n%s\nwant:\n%s", spec, golden)
	}
}

// An action or observation outside the dialect fails generation instead of becoming a
// silent comment: the backstop for state written before the dialect was closed.
func TestPlaywrightDialect_UnknownKindFailsGeneration(t *testing.T) {
	for name, tc := range map[string]struct {
		action, observation, want string
	}{
		"unknown action":       {act("tap", tgt("order-done")), `[` + act("visible", tgt("orders-list")) + `]`, `unknown UI action kind "tap"`},
		"press without key":    {act("press", tgt("order-edit")), `[` + act("visible", tgt("orders-list")) + `]`, "needs a key"},
		"empty observation":    {act("click", tgt("orders-tab")), `[]`, "asserts nothing"},
		"unknown observable":   {act("click", tgt("orders-tab")), `[` + act("glow", tgt("orders-list")) + `]`, `unknown UI observable kind "glow"`},
		"pre-envelope action":  {`{"kind":"click","target":"orders-tab"}`, `[` + act("visible", tgt("orders-list")) + `]`, "not a UI action"},
		"pre-envelope observe": {act("click", tgt("orders-tab")), `{"visible":"orders-list"}`, "not a UI observation"},
	} {
		_, err := generateShopStep(t, []string{tc.action}, tc.observation)
		if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "shopClient browse-orders-P1") {
			t.Errorf("%s: err %v, want one naming shopClient browse-orders-P1 and %q", name, err, tc.want)
		}
	}
}
