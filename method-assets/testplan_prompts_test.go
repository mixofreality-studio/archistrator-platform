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
		serviceTestPlan      = ".claude/commands/service-test-plan.md"
		frontendTestPlan     = ".claude/commands/frontend-test-plan.md"
		frontendConstruction = ".claude/commands/frontend-construction.md"
		testEngineer         = ".claude/agents/test-engineer.md"
		reviewRouting        = ".claude/skills/the-method-review-routing/SKILL.md"
		testingSkill         = ".claude/skills/the-method-testing/SKILL.md"
		contractSkill        = ".claude/skills/the-method-service-contract/SKILL.md"
		detailedDesign       = ".claude/commands/service-detailed-design.md"
	)
	for _, tc := range []struct{ path, want string }{
		// The key: FIX-018.
		{serviceTestPlan, "mapKey = the contract key"},
		{frontendTestPlan, "mapKey = the contract key"},
		{testEngineer, "testPlan[<contractKey>]"},
		{testingSkill, "testPlan[<contractKey>]"},
		{reviewRouting, "testPlan[<contractKey>]"},
		// The values: FIX-021, restated for the declarative record (FPC plan 2).
		{serviceTestPlan, "never leave a blank placeholder"},
		{frontendTestPlan, "never leave a blank placeholder"},
		{testEngineer, "never a blank placeholder"},
		{serviceTestPlan, "errorExpected"},
		{testEngineer, "errorExpected"},
		{reviewRouting, "errorExpected"},
		// The browser behaviour the design owes the user: FIX-062.
		{frontendConstruction, "reflects the user's change in the same interaction (optimistic)"},
		{frontendConstruction, "roll it back and show the user a notice"},
		{frontendConstruction, "A call in flight does not block unrelated input"},
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

// The binding is a typed record the platform turns into tests (FPC plan 2): an agent never
// writes a hook, a fill-in, or test code. The words that described the retired escape hatches
// must be in no prompt, skill, agent or doc the platform ships, because a prompt that still
// names `hook: true` invites the record the schema now refuses (TP-HOOK).
func TestPrompts_NoHookOrUnobservableMentions(t *testing.T) {
	files, err := ClaudeFiles()
	if err != nil {
		t.Fatal(err)
	}
	for path, body := range files {
		for _, banned := range []string{"hook: true", "unobservable", "hooks.ts", "_hooks_test.go", "Fill the hooks", "fills their hooks"} {
			if strings.Contains(strings.ToLower(string(body)), strings.ToLower(banned)) {
				t.Errorf("%s still mentions %q: the binding is a declarative record, there are no hooks", path, banned)
			}
		}
	}
}

// Every sentence that states a rule of the declarative test model names the rule id that
// enforces it, so a prompt can never promise what no gate holds and a rule can be found from
// the prompt that explains it. The ids are the ones framework-go/methodcheck emits
// (emitters.go), framework-go/scenario/problems.go and framework-go/arch (ARCH-TEST-COMPOSE).
func TestPrompts_CiteTheRulesThatEnforceThem(t *testing.T) {
	files, err := ClaudeFiles()
	if err != nil {
		t.Fatal(err)
	}
	cmd := func(slug string) string { return ".claude/commands/" + slug + ".md" }
	for path, wants := range map[string][]string{
		cmd("service-test-plan"): {
			"TP-HOOK", "TP-EXPECT-REQUIRED", "TP-ARG-REF", "TP-ARRANGE", "TP-PROBE", "TP-STATE",
			"TP-ACTOR-OBSERVED", "TP-PRINCIPAL", "vacuous",
			"TP-PROBE-SELF", "TP-PROBE-POLLING", "TP-PRINCIPAL-DELEGATION", "TP-EXPECT-ZERO", "onBehalfOf", "read-only",
		},
		cmd("frontend-test-plan"): {
			"TP-HOOK", "TP-EXPECT-REQUIRED", "TP-UI-TARGET", "TP-UI-ACTION", "UID-ROUTE", "TP-ARG-REF",
			"TP-ARRANGE", "TP-ACTOR-OBSERVED", "TP-PRINCIPAL",
			"TP-PRINCIPAL-DELEGATION", "TP-EXPECT-ZERO", "onBehalfOf",
		},
		cmd("frontend-detailed-design"): {"UID-ROLE", "UID-NAME", "UID-ID-UNIQUE", "UID-WITHIN", "UID-ROUTE", "surface check", "latency"},
		cmd("service-integration"):      {"ARCH-TEST-COMPOSE", "TP-DISCRIMINATE"},
		cmd("frontend-integration"):     {"ARCH-TEST-COMPOSE", "TP-DISCRIMINATE", "surface-check"},
		cmd("service-construction"):     {"ARCH-TEST-COMPOSE"},
		cmd("frontend-construction"):    {"ARCH-TEST-COMPOSE", "UID-NAME", "surface check"},
		cmd("core-use-cases-critique"): {
			"UC-START-NOT-ACTION", "UC-PATH-OUTCOME", "UC-OUTCOME-AT-ASKING-SURFACE", "CC-DECIDED-BY-ACTOR-STEP",
			"SCN-UNADDRESSED", "SCN-ACTOR-PATH-CLIENT", "TP-CLIENT-CONTRACTED",
		},
		".claude/skills/the-method-testing/SKILL.md":        {"writes no test code", "ARCH-TEST-COMPOSE", "TP-DISCRIMINATE", "TP-PROBE-SELF", "TP-PROBE-POLLING", "TP-PRINCIPAL-DELEGATION", "TP-EXPECT-ZERO", "TP-STATE"},
		".claude/skills/the-method-review-routing/SKILL.md": {"TP-HOOK", "vacuous"},
		".claude/agents/test-engineer.md":                   {"TP-HOOK", "TP-EXPECT-REQUIRED", "TP-PROBE-SELF", "TP-PROBE-POLLING", "TP-PRINCIPAL-DELEGATION", "TP-EXPECT-ZERO"},
	} {
		body := string(files[path])
		for _, want := range wants {
			if !strings.Contains(body, want) {
				t.Errorf("%s does not cite %q", path, want)
			}
		}
	}
}
