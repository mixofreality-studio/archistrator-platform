package methodassets

import (
	"strings"
	"testing"
)

// The deterministic scenario tests ride the INTEGRATION task, not construction
// (spec §12 B12): the test plan is bound in parallel with construction, so
// construction builds the component only, and the integration step — where the
// two branches meet — generates the tests from the reviewed plan, fills their
// hooks, runs them against the integrated component and records the run before
// it publishes. These pins keep the four commands on that split, and keep the
// integration commands reading the exact env names the construct workflow
// exports (TestConstructTemplateHasScenarioSteps).

func commandBody(t *testing.T, files map[string][]byte, slug string) string {
	t.Helper()
	raw, ok := files[".claude/commands/"+slug+".md"]
	if !ok {
		t.Fatalf("no command %s", slug)
	}
	return string(raw)
}

func TestConstructionCommandsWriteNoTests(t *testing.T) {
	files, err := ClaudeFiles()
	if err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"service-construction", "frontend-construction"} {
		body := commandBody(t, files, slug)
		for _, forbidden := range []string{"make test-scenarios", "test:scenarios", "record-test-run", "Fill every", "make gen-tests-check"} {
			if strings.Contains(body, forbidden) {
				t.Errorf("%s still carries the scenario-test step %q: construction builds the component only", slug, forbidden)
			}
		}
		if !strings.Contains(body, "**No tests here.**") || !strings.Contains(body, "Do NOT run `make gen-tests`") {
			t.Errorf("%s must tell the agent it writes and generates no tests", slug)
		}
	}
}

func TestIntegrationCommandsRunAndRecordTheScenarios(t *testing.T) {
	files, err := ClaudeFiles()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		slug string
		run  []string // the run step's commands, in order
	}{
		{"service-integration", []string{"make test-scenarios"}},
		{"frontend-integration", []string{"npx playwright test", `cp -R "uitests/test-results/${c}" test-results/`, "make test-scenarios"}},
	} {
		body := commandBody(t, files, c.slug)
		if strings.Contains(body, "you do not run them here") {
			t.Errorf("%s still hands the scenario run to the venue", c.slug)
		}
		for _, s := range []string{
			"[[the-method-testing]]",
			// The plans and results are keyed by CONTRACT name; the activity names its
			// component kebab (DCT plan 5 r2), so the commands address contracts.
			".phaseArtifacts.testPlan[\"<contract>\"]",
			"`make gen-tests`", "`FILL`", "public constructor", "`make gen-tests-check` must be clean",
			"test-results/<contract>/results.json",
			`run="${AIARCH_TEST_RUN_ID:-local-`, `artifact="${AIARCH_TEST_ARTIFACT:-.aiarch/test-results/${run}}"`,
			"aiarch-state-mcp record-test-run --activity <activity_id> --component <component_id>",
			`--run-id "${run}" --artifact "${artifact}" --results test-results`,
			// The revision is the attempt the dispatch stamps, never a commit SHA.
			`--revision "${AIARCH_REVISION}"`,
		} {
			if !strings.Contains(body, s) {
				t.Errorf("%s missing %q", c.slug, s)
			}
		}
		// Ordered: generate → fill → purge → run → commit → record → publish, so
		// the recorded revision is the committed code and the run rides the publish.
		at := -1
		for _, s := range append(append([]string{"**Generate the tests**", "**Fill the hooks.**", "**Purge.**"}, c.run...),
			"**Commit**", "**Record the run**", "aiarch-state-mcp record-test-run", "**Publish.** `publishDraft`") {
			i := strings.Index(body, s)
			if i < 0 || i < at {
				t.Errorf("%s: %q is missing or out of order (generate, fill, purge, run, commit, record, publish)", c.slug, s)
				continue
			}
			at = i
		}
	}
}
