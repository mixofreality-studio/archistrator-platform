package methodassets

import (
	"strings"
	"testing"
)

func TestWorkflowTemplates(t *testing.T) {
	for _, name := range []string{"aiarch-design.yml.tmpl", "aiarch-construct.yml.tmpl"} {
		body, err := assetsFS.ReadFile("assets/workflows/" + name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		s := string(body)
		if strings.Contains(s, "design_prompt") {
			t.Errorf("%s: raw design_prompt input must be gone (thin dispatch)", name)
		}
		if !strings.Contains(s, "/${{ inputs.command }}") {
			t.Errorf("%s: prompt must be the thin slash-command invocation", name)
		}
		if !strings.Contains(s, "[[.AppSlug]]") {
			t.Errorf("%s: allowed_bots must be templated on [[.AppSlug]]", name)
		}
	}
	// Construct must NOT build the MCP from source (app repos have no server source).
	c, _ := assetsFS.ReadFile("assets/workflows/aiarch-construct.yml.tmpl")
	if strings.Contains(string(c), "go build ./cmd/aiarch-state-mcp") {
		t.Error("construct template still builds MCP from checked-out source; must go install [[.StateMcpModulePath]]@[[.StateMcpModuleVersion]]")
	}
}

// TestWorkflowTemplatesScopeThePromptSurface pins the CLOUD rail's half of the
// per-step scoping. The local executor is covered by its own tests; without
// this, the two rails could silently diverge and the same step would carry a
// different context in CI than it does locally — the exact defect that makes a
// benchmark result untransferable to production.
func TestWorkflowTemplatesScopeThePromptSurface(t *testing.T) {
	for _, name := range []string{"aiarch-design.yml.tmpl", "aiarch-construct.yml.tmpl"} {
		body, err := assetsFS.ReadFile("assets/workflows/" + name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		s := string(body)

		// The seat step must scope to this step, not render all 59 commands, and the
		// MCP server must know the step, or it registers the full tool catalog. Both
		// workflows map every value through env: and build the config with jq
		// (construct_injection_test.go, workflow_injection_test.go).
		seat, stamp := `seat-assets --dest . --command "${AIARCH_COMMAND}"`, `AIARCH_COMMAND: $command`
		if !strings.Contains(s, seat) {
			t.Errorf("%s: seat step must scope the surface with --command", name)
		}
		if !strings.Contains(s, stamp) {
			t.Errorf("%s: MCP config must stamp AIARCH_COMMAND (the step manifest key)", name)
		}
		// Prompt-surface isolation: without this the runner's ambient ~/.claude
		// (plugins, skills, agents) loads into the dispatched agent.
		if !strings.Contains(s, "--setting-sources project") {
			t.Errorf("%s: claude_args must pass --setting-sources project", name)
		}
		if !strings.Contains(s, "--strict-mcp-config") {
			t.Errorf("%s: claude_args must keep --strict-mcp-config", name)
		}
		// The built-in surface is the task spec's allowlist (--tools), resolved by
		// the binary from the same code the server hashed. The old deny path
		// (step-tools --disallowed) is gone: an allowlist cannot silently admit a
		// tool a future CLI adds.
		if !strings.Contains(s, "--tools '${{ steps.spec.outputs.builtin }}'") {
			t.Errorf("%s: claude_args must restrict the built-ins to the spec's allowlist", name)
		}
		if !strings.Contains(s, "--model ${{ steps.spec.outputs.model }}") {
			t.Errorf("%s: claude_args must run the spec's model", name)
		}
	}
}

// TestWorkflowTemplatesRunTheSpec pins the job half of the agent task spec
// (spec §5.2): the job recomputes the spec and refuses a drifted hash, runs the
// agent on the spec's model and built-in allowlist, then gates the edit scope,
// runs the full check set, uploads the logs, records the CheckRun and fails red.
func TestWorkflowTemplatesRunTheSpec(t *testing.T) {
	for _, name := range []string{"aiarch-construct.yml.tmpl", "aiarch-design.yml.tmpl"} {
		raw, err := assetsFS.ReadFile("assets/workflows/" + name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		body := string(raw)
		for _, want := range []string{
			`task-spec --activity "${AIARCH_ACTIVITY_ID}" --task "${AIARCH_TASK_ID}"`,
			`if [ "${got}" != "${AIARCH_SPEC_HASH}" ]; then`,
			`steps.spec.outputs.model`,
			`--tools '${{ steps.spec.outputs.builtin }}'`,
			`edit-scope --spec`,
			`run-checks --gate --spec`,
			`record-check-run --spec`,
			`name: check-results-${{ inputs.activity_id }}-${{ github.run_id }}-${{ github.run_attempt }}`,
			// One run id and artifact per attempt; no empty commit on a re-record.
			`run_id="gh-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}"`,
			`--artifact "check-results-${AIARCH_ACTIVITY_ID}-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}"`,
			`git diff --cached --quiet || git -c`,
			// A missing spec field fails rather than reading as "null".
			`jq -er .model.id`, `jq -er '.tools.builtin | join(",")'`,
			// The edit-scope CLI takes commit SHAs only: main is pinned before the
			// agent, from full history, and passed through env; --no-main when absent.
			"fetch-depth: 0",
			`git fetch --no-tags --unshallow origin`,
			`main_sha="$(git rev-parse --verify "refs/remotes/origin/main^{commit}")"`,
			`echo "main_sha=${main_sha}" >> "$GITHUB_OUTPUT"`,
			"AIARCH_MAIN_SHA: ${{ steps.spec.outputs.main_sha }}",
			`--base "${AIARCH_BASE}" --head "$(git rev-parse FETCH_HEAD)" --main "${AIARCH_MAIN_SHA}"`,
			`--base "${AIARCH_BASE}" --head "$(git rev-parse FETCH_HEAD)" --no-main`,
			// Ruling R12: runs on one branch queue, never cancel; bounded.
			"cancel-in-progress: false", "timeout-minutes: 90", "timeout-minutes: 25", "timeout-minutes: 45",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: missing %q", name, want)
			}
		}
		gones := []string{"Fail on a red scenario run"}
		if name == "aiarch-construct.yml.tmpl" {
			// The design workflow keeps the deny path for ANSWER jobs only (R30,
			// pinned by workflow_answer_test.go).
			gones = append(gones, "--disallowedTools", "step-tools --command", "steps.steptools")
		}
		for _, gone := range gones {
			if strings.Contains(body, gone) {
				t.Errorf("%s: still has %q", name, gone)
			}
		}
		if !strings.Contains(body, "group: aiarch-construct-${{ inputs.activity_id }}") &&
			!strings.Contains(body, "group: aiarch-design-${{ inputs.target_branch }}") {
			t.Errorf("%s: the concurrency group must be keyed on the branch the runs push to", name)
		}
		if strings.Contains(body, "group: aiarch-cp-") {
			t.Errorf("%s: the concurrency group must not be keyed on the per-run dedup token", name)
		}
		// Order: the spec is resolved before the agent; every gate follows it.
		agent := strings.Index(body, "uses: anthropics/claude-code-action@v1")
		if at := strings.Index(body, "name: Resolve the task spec"); at < 0 || at > agent {
			t.Errorf("%s: the task spec must be resolved before the agent step", name)
		}
		if at := strings.Index(body, `echo "main_sha=${main_sha}"`); at < 0 || at > agent {
			t.Errorf("%s: main must be pinned before the agent step can move any ref", name)
		}
		if at := strings.Index(body, "name: Keep check output out of commits"); at < 0 || at > agent {
			t.Errorf("%s: check output must be excluded from commits before the agent step (R28)", name)
		}
		at := agent
		for _, step := range []string{
			"name: Gate the edit scope", "name: Run the checks", "name: Upload check results",
			"name: Record the check run", "name: Fail on a red check",
		} {
			i := strings.Index(body, step)
			if i < at {
				t.Errorf("%s: %q is missing or out of order (agent, edit scope, checks, upload, record, fail)", name, step)
				continue
			}
			at = i
		}
	}
}
