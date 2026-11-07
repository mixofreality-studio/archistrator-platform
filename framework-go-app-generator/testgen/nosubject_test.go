package testgen

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// withDeployment is the project_tp fixture with a deployment slot holding the given bindings.
func withDeployment(t *testing.T, bindings string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/project_tp.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	slots, _ := doc["slots"].(map[string]any)
	var deployment any
	if err := json.Unmarshal([]byte(`{"bindings": `+bindings+`}`), &deployment); err != nil {
		t.Fatal(err)
	}
	slots["6"] = map[string]any{"status": 2, "kind": 6, "model": map[string]any{"deployment": deployment}}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestGenerate_NoLocalArmEmitsNoScenarios: a component whose optional binding names no local-profile
// arm has no constructor in scenariocompose, so testgen names it and emits no test file that would
// call one. A required binding, and a binding with a local arm, are untouched.
func TestGenerate_NoLocalArmEmitsNoScenarios(t *testing.T) {
	raw := withDeployment(t, `[{"component":"billingManager","presence":"optional-dormant","perProfile":{"cloud":{"variant":"Cloud"}}}]`)
	out, err := Generate(raw, Config{ModulePath: "example.com/app/server"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out.Generated[billingGen]; ok {
		t.Errorf("billingManager has no local arm, but %s was generated", billingGen)
	}
	var named bool
	for _, w := range out.Warnings {
		if strings.Contains(w, "billingManager has no local-profile arm") {
			named = true
		}
	}
	if !named {
		t.Errorf("the skipped component is not named in the warnings: %v", out.Warnings)
	}

	for _, keep := range []string{
		`[{"component":"billingManager","presence":"optional-dormant","perProfile":{"local":{"variant":"Local"},"cloud":{"variant":"Cloud"}}}]`,
		`[{"component":"billingManager","presence":"required","perProfile":{"cloud":{"variant":"Cloud"}}}]`,
	} {
		out, err := Generate(withDeployment(t, keep), Config{ModulePath: "example.com/app/server"})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := out.Generated[billingGen]; !ok {
			t.Errorf("binding %s must still generate %s", keep, billingGen)
		}
	}
}
