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
	return withDeploymentSkipping(t, bindings, nil)
}

// withDeploymentSkipping is withDeployment with a dated skip recorded on every binding of each
// named component's test plan.
func withDeploymentSkipping(t *testing.T, bindings string, skipped []string) []byte {
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
	artifacts, _ := doc["phaseArtifacts"].(map[string]any)
	plans, _ := artifacts["testPlan"].(map[string]any)
	for _, comp := range skipped {
		plan, _ := plans[comp].(map[string]any)
		bs, _ := plan["bindings"].([]any)
		for _, b := range bs {
			b.(map[string]any)["skip"] = map[string]any{"reason": "needs a local arm", "until": "2026-12-31"}
		}
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestGenerate_NoLocalArmRefusesUnskippedBindings: a component whose optional binding names no
// local-profile arm has no constructor in scenariocompose. Recorded bindings that nothing could run
// are a refusal naming the component, not a silent drop; with every binding on a dated skip the
// component is named in the warnings and no test file is emitted. A required binding, and a binding
// with a local arm, are untouched.
func TestGenerate_NoLocalArmRefusesUnskippedBindings(t *testing.T) {
	noArm := `[{"component":"billingManager","presence":"optional-dormant","perProfile":{"cloud":{"variant":"Cloud"}}}]`

	_, err := Generate(withDeployment(t, noArm), Config{ModulePath: "example.com/app/server"})
	if err == nil || !strings.Contains(err.Error(), "billingManager") || !strings.Contains(err.Error(), "dated skip") {
		t.Fatalf("recorded bindings with no subject must be refused, naming the component and the skip: %v", err)
	}

	out, err := Generate(withDeploymentSkipping(t, noArm, []string{"billingManager"}), Config{ModulePath: "example.com/app/server"})
	if err != nil {
		t.Fatalf("every binding skipped: %v", err)
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
