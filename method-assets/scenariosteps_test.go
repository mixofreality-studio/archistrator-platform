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
		for _, forbidden := range []string{"make test-scenarios", "test:scenarios", "record-test-run", "Fill every", "make gen-tests"} {
			if strings.Contains(body, forbidden) {
				t.Errorf("%s still carries the scenario-test step %q: construction builds the component only", slug, forbidden)
			}
		}
		if !strings.Contains(body, "**No tests here.**") || !strings.Contains(body, "Do NOT generate, and do NOT write or edit") {
			t.Errorf("%s must tell the agent it writes and generates no tests", slug)
		}
	}
}

func TestIntegrationCommandsLeaveRunAndRecordToTheJob(t *testing.T) {
	files, err := ClaudeFiles()
	if err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"service-integration", "frontend-integration"} {
		body := commandBody(t, files, slug)
		for _, s := range []string{
			"[[the-method-testing]]",
			// The plans are keyed by CONTRACT name; the activity names its
			// component kebab (DCT plan 5 r2), so the commands address contracts.
			".phaseArtifacts.testPlan[\"<contract>\"]",
			"The tests are already generated: the job generated them before you started.",
			"`FILL`", "public constructor",
			"**Verify.** Call `run_checks()`.",
		} {
			if !strings.Contains(body, s) {
				t.Errorf("%s missing %q", slug, s)
			}
		}
		// The job generates the tests in the working tree but never commits them:
		// the agent's commit is the only path that puts them on the branch.
		if !strings.Contains(body, "**Commit** the wiring, the generated tests and the hooks file onto `activity/<activity_id>`.") {
			t.Errorf("%s: the commit step must include the generated tests", slug)
		}
		// Ordered: generated → fill → purge → commit → publish. The job runs the
		// scenarios and records the run.
		at := -1
		for _, s := range []string{"**Generated tests.**", "**Fill the hooks.**", "**Purge.**", "**Commit**", "**Publish.** `publishDraft`"} {
			i := strings.Index(body, s)
			if i < 0 || i < at {
				t.Errorf("%s: %q is missing or out of order (generated, fill, purge, commit, publish)", slug, s)
				continue
			}
			at = i
		}
	}
}
