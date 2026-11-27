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
		// Who an arrange contract call runs as: a Manager or a Client authorizes its caller, so the
		// call carries `as`; an Engine or a ResourceAccess reads no principal (ISS-033-3).
		{serviceTestPlan, "it carries `as`, one of the plan's principals"},
		{frontendTestPlan, "it carries `as`, one of the plan's principals"},
		{testEngineer, "it carries `as`, one of the plan's principals"},
		{serviceTestPlan, "into an Engine or a ResourceAccess it carries none"},
		{frontendTestPlan, "into an Engine or a ResourceAccess it carries none"},
		{testEngineer, "carries none `[TP-ARRANGE]`"},
		{serviceTestPlan, "build the state with `seedResource`"},
		{frontendTestPlan, "build the state with `seedResource`"},
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

// The Client's surface is one closed field (FPC W6): the two booleans it replaced must be in no
// prompt, skill or agent, because a prompt that still names them invites a record the schema now
// refuses.
func TestPrompts_NoRetiredSurfaceFields(t *testing.T) {
	files, err := ClaudeFiles()
	if err != nil {
		t.Fatal(err)
	}
	for path, body := range files {
		for _, banned := range []string{"uiSurface", "machineProtocol", "SYS-CLIENT-UISURFACE", "SYS-CLIENT-PROTOCOL"} {
			if strings.Contains(string(body), banned) {
				t.Errorf("%s still mentions %q: a Client states one `surface`", path, banned)
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
			"TP-PROBE-SELF", "TP-PROBE-POLLING", "TP-PRINCIPAL-DELEGATION", "TP-EXPECT-ZERO", "TP-NULL-SATISFIED", "TP-FALSIFIABLE", "onBehalfOf", "read-only",
			`{"kind":"postgres","model":{"sql":"..."}}`, `{"kind":"github","model":{"owner"`, `{"kind":"gitRepo","model":{"branch"`, "never the domain's own JSON",
		},
		cmd("frontend-test-plan"): {
			"TP-HOOK", "TP-EXPECT-REQUIRED", "TP-UI-TARGET", "TP-UI-ACTION", "UID-ROUTE", "TP-ARG-REF",
			"TP-ARRANGE", "TP-ACTOR-OBSERVED", "TP-PRINCIPAL",
			"TP-PRINCIPAL-DELEGATION", "TP-EXPECT-ZERO", "TP-NULL-SATISFIED", "TP-FALSIFIABLE", "onBehalfOf",
			`{"kind":"postgres","model":{"sql":"..."}}`, `{"kind":"github","model":{"owner"`, `{"kind":"gitRepo","model":{"branch"`, "never the domain's own JSON",
		},
		cmd("acceptance-test-plan"): {
			"ATP-BOUND", "ATP-REF", "ATP-PROBE-AS", "ATP-BROWSER", "TP-PRINCIPAL", "TP-EXPECT-REQUIRED", "TP-STATE",
			"TP-ARG-REF", "TP-SKIP", "onBehalfOf", "planRef",
		},
		cmd("frontend-detailed-design"):                       {"UID-ROLE", "UID-NAME", "UID-ID-UNIQUE", "UID-WITHIN", "UID-ROUTE", "surface check", "latency"},
		cmd("system-draft"):                                   {"surface", "SYS-CLIENT-SURFACE", "mcpApp", "SYS-SURFACE-REQUIRED", "SYS-SURFACE-ENTRY", "GEN-SURFACE-SUPPORTED", "UC-OUTCOME-AT-ASKING-SURFACE", "SYS-RESOURCE-UNREAD"},
		cmd("system-critique"):                                {"SYS-RESOURCE-UNREAD"},
		cmd("service-detailed-design"):                        {"TP-OP-REACHED", "TP-OP-OBSERVABLE", "SYS-RESOURCE-UNREAD"},
		".claude/skills/the-method-service-contract/SKILL.md": {"TP-OP-REACHED", "TP-OP-OBSERVABLE", "SYS-RESOURCE-UNREAD"},
		cmd("mission-draft"):                                  {"deliverySurfaces", "verbatim", "SYS-SURFACE-REQUIRED"},
		cmd("mission-critique"):                               {"deliverySurfaces", "audience"},
		cmd("service-integration"):                            {"ARCH-TEST-COMPOSE", "TP-DISCRIMINATE"},
		cmd("frontend-integration"):                           {"ARCH-TEST-COMPOSE", "TP-DISCRIMINATE", "surface-check"},
		cmd("service-construction"):                           {"ARCH-TEST-COMPOSE"},
		cmd("frontend-construction"):                          {"ARCH-TEST-COMPOSE", "UID-NAME", "surface check"},
		cmd("core-use-cases-critique"): {
			"UC-START-NOT-ACTION", "UC-PATH-OUTCOME", "UC-OUTCOME-AT-ASKING-SURFACE", "CC-DECIDED-BY-ACTOR-STEP",
			"SCN-UNADDRESSED", "SCN-ACTOR-PATH-CLIENT", "TP-CLIENT-CONTRACTED", "UC-ACTOR-DOOR", "UC-LANE-RESOLVES", "UC-OUTCOME-TO-ASKER",
		},
		cmd("core-use-cases-draft"):                         {"UC-OUTCOME-TO-ASKER"},
		".claude/skills/the-method-core-use-cases/SKILL.md": {"UC-OUTCOME-TO-ASKER"},
		".claude/skills/the-method-architecture/SKILL.md":   {"UC-LANE-RESOLVES", "UC-OUTCOME-AT-ASKING-SURFACE", "SYS-RESOURCE-UNREAD"},
		".claude/skills/the-method-testing/SKILL.md":        {"writes no test code", "ARCH-TEST-COMPOSE", "TP-DISCRIMINATE", "TP-PROBE-SELF", "TP-PROBE-POLLING", "TP-PRINCIPAL-DELEGATION", "TP-EXPECT-ZERO", "TP-NULL-SATISFIED", "TP-STATE"},
		".claude/skills/the-method-review-routing/SKILL.md": {"TP-HOOK", "vacuous"},
		".claude/agents/test-engineer.md":                   {"TP-HOOK", "TP-EXPECT-REQUIRED", "TP-PROBE-SELF", "TP-PROBE-POLLING", "TP-PRINCIPAL-DELEGATION", "TP-EXPECT-ZERO", "TP-NULL-SATISFIED", "TP-FALSIFIABLE"},
	} {
		body := string(files[path])
		for _, want := range wants {
			if !strings.Contains(body, want) {
				t.Errorf("%s does not cite %q", path, want)
			}
		}
	}
}

// The core-use-cases critique reads the coverage table (FPC Task 2.14): per derived path, the door
// the actor enters through, the outcome nodes the actor is shown and the derivation's defect codes. The
// table is a tool read (getUseCaseCoverage), derived on read from the draft and stored nowhere, so the
// prompt must name the tool, the columns the PM judges and the floor the PM's send-back is, and the PM's
// charter must grant the tool: an agent without it cannot be asked to read the table.
func TestCoreUseCasesCritique_ReadsTheCoverageTable(t *testing.T) {
	files, err := ClaudeFiles()
	if err != nil {
		t.Fatal(err)
	}
	const (
		critique = ".claude/commands/core-use-cases-critique.md"
		pm       = ".claude/agents/product-manager.md"
	)
	for _, tc := range []struct{ path, want string }{
		{critique, "getUseCaseCoverage"},
		{critique, "Door"},
		{critique, "Outcomes the actor sees"},
		{critique, "NONE"},
		{critique, "every review preset"},
		{critique, "override"},
		{pm, "mcp__aiarch-state__getUseCaseCoverage"},
		{pm, "every review preset"},
	} {
		if !strings.Contains(string(files[tc.path]), tc.want) {
			t.Errorf("%s no longer says %q", tc.path, tc.want)
		}
	}
}
