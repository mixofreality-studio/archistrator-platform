package methodassets

import (
	"strings"
	"testing"
)

// A test plan's key and its values are decided by what these prompts say: no rule can tell a
// literal "id from step 2" from a real value (FIX-021 measured it), so the prompt is the only
// tool, and a rewrite that drops a clause would pass every other test. A contract's layer rules
// are enforced at the record verb, and the prompt is what saves the agent the refused round.
func TestTestPlanAndContractPromptsCarryTheirRules(t *testing.T) {
	files, err := ClaudeFiles()
	if err != nil {
		t.Fatal(err)
	}
	const (
		serviceTestPlan  = ".claude/commands/service-test-plan.md"
		frontendTestPlan = ".claude/commands/frontend-test-plan.md"
		testEngineer     = ".claude/agents/test-engineer.md"
		reviewRouting    = ".claude/skills/the-method-review-routing/SKILL.md"
		testingSkill     = ".claude/skills/the-method-testing/SKILL.md"
		contractSkill    = ".claude/skills/the-method-service-contract/SKILL.md"
		detailedDesign   = ".claude/commands/service-detailed-design.md"
	)
	for _, tc := range []struct{ path, want string }{
		// The key: FIX-018.
		{serviceTestPlan, "mapKey = the contract key"},
		{frontendTestPlan, "mapKey = the contract key"},
		{testEngineer, "testPlan[<contractKey>]"},
		{testingSkill, "testPlan[<contractKey>]"},
		{reviewRouting, "testPlan[<contractKey>]"},
		// The values: FIX-021.
		{serviceTestPlan, "There is no step-output reference"},
		{frontendTestPlan, "There is no step-output reference"},
		{testEngineer, "there is no step-output reference"},
		{serviceTestPlan, "never leave a blank placeholder"},
		{frontendTestPlan, "never leave a blank placeholder"},
		{testEngineer, "never a blank placeholder"},
		{serviceTestPlan, "errorExpected"},
		{testEngineer, "errorExpected"},
		{reviewRouting, "errorExpected"},
		// The contract's layer rules: FIX-019.
		{contractSkill, `never reason "total, so no error"`},
		{detailedDesign, "declares `error: true`"},
	} {
		if !strings.Contains(strings.ToLower(string(files[tc.path])), strings.ToLower(tc.want)) {
			t.Errorf("%s no longer says %q", tc.path, tc.want)
		}
	}
	// No prompt may file a plan under anything but its contract key.
	for path, body := range files {
		for _, stale := range []string{"mapKey = component id", "testPlan[<component>]", "testPlan[component]"} {
			if strings.Contains(string(body), stale) {
				t.Errorf("%s still says %q", path, stale)
			}
		}
	}
}
