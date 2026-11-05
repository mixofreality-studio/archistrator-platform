package testgen

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

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
		inputs = append(inputs, map[string]any{"name": "action", "value": a})
	}
	step["inputs"] = inputs
	step["expect"] = map[string]any{"result": observation, "errorExpected": false}
	step["hook"] = false
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
		`{"kind":"navigate","value":"/orders"}`,
		`{"kind":"check","target":"order-done"}`,
		`{"kind":"uncheck","target":"order-flag"}`,
		`{"kind":"dblclick","target":"order-title"}`,
		`{"kind":"fill","target":"order-edit","value":"rename"}`,
		`{"kind":"press","target":"order-edit","value":"Enter"}`,
		`{"kind":"hover","target":"order-row"}`,
	}, `[{"checked":"order-done"},{"unchecked":"order-flag"},{"text":"1 item left","target":"order-count"},{"text":"","target":"order-edit"}]`)
	if err != nil {
		t.Fatal(err)
	}
	spec := string(out.Generated[shopSpec])
	want := `  // step 1: OpenOrders (node ` // the node id follows
	if !strings.Contains(spec, want) {
		t.Fatalf("missing step header in\n%s", spec)
	}
	golden := `  await page.goto('/orders');
  await page.getByTestId('order-done').check();
  await page.getByTestId('order-flag').uncheck();
  await page.getByTestId('order-title').dblclick();
  await page.getByTestId('order-edit').fill('rename');
  await page.getByTestId('order-edit').press('Enter');
  await page.getByTestId('order-row').hover();
  await expect(page.getByTestId('order-done')).toBeChecked();
  await expect(page.getByTestId('order-flag')).not.toBeChecked();
  await expect(page.getByTestId('order-count')).toHaveText('1 item left');
  await expect(page.getByTestId('order-edit')).toHaveText('');
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
		"unknown action":       {`{"kind":"tap","target":"order-done"}`, `{"visible":"orders-list"}`, `unknown UI action kind "tap"`},
		"press without target": {`{"kind":"press","value":"Enter"}`, `{"visible":"orders-list"}`, "needs target"},
		"two-thing observable": {`{"kind":"click","target":"orders-tab"}`, `{"visible":"orders-list","checked":"x"}`, "exactly one"},
	} {
		_, err := generateShopStep(t, []string{tc.action}, tc.observation)
		if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "shopClient browse-orders-P1") {
			t.Errorf("%s: err %v, want one naming shopClient browse-orders-P1 and %q", name, err, tc.want)
		}
	}
}
