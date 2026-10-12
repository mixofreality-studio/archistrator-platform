package methodassets

// workflow_answer_test.go pins two task-spec rulings on the seated workflows:
// R30 (design answer jobs are not lifecycle tasks: no spec, no spec gates, ids
// optional) and R31 (construct works on activity/<id> before it hashes the spec).

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// stepText is the raw text of the step named name, from its "- name:" line to the
// next step (or the end of the job's steps).
func stepText(t *testing.T, doc, name string) string {
	t.Helper()
	start := strings.Index(doc, "      - name: "+name+"\n")
	if start < 0 {
		t.Fatalf("no step named %q", name)
	}
	rest := doc[start+1:]
	end := regexp.MustCompile(`(?m)^(      - name: |  [a-z]+:$)`).FindStringIndex(rest[len("      - name: "):])
	if end == nil {
		return rest
	}
	return rest[:len("      - name: ")+end[0]]
}

// TestDesignWorkflow_AnswerModeSkipsTheTaskSpec (R30): activity_id, task_id and
// spec_hash are optional dispatch inputs; the spec step, the draft agent and every
// gate that reads the spec are skipped for job_mode=answer, which runs its own
// agent step without --model/--tools.
func TestDesignWorkflow_AnswerModeSkipsTheTaskSpec(t *testing.T) {
	doc := renderedDesignWorkflow(t)
	for _, in := range []string{"activity_id", "task_id", "spec_hash"} {
		re := regexp.MustCompile(`(?m)^      ` + in + `:\n        description: "[^"\n]*"\n        required: false\n        type: string$`)
		if !re.MatchString(doc) {
			t.Errorf("workflow_dispatch input %s must be `required: false`: an answer job has no task spec", in)
		}
	}
	notAnswer := "if: ${{ inputs.job_mode != 'answer' }}"
	for _, name := range []string{"Resolve the task spec", "Draft the typed Method artifact (claude-code-action)"} {
		if !strings.Contains(stepText(t, doc, name), notAnswer) {
			t.Errorf("step %q must be skipped for answer jobs (`%s`)", name, notAnswer)
		}
	}
	// Every spec-reading gate keys on the spec step (skipped, so not 'success') or
	// on the checks step that does.
	for _, name := range []string{"Restore the job's git credential", "Gate the edit scope", "Run the checks"} {
		if !strings.Contains(stepText(t, doc, name), "steps.spec.outcome == 'success'") {
			t.Errorf("gate %q must require the spec step to have succeeded", name)
		}
	}
	for _, name := range []string{"Upload check results", "Record the check run"} {
		if !strings.Contains(stepText(t, doc, name), "steps.checks.outcome == 'success'") {
			t.Errorf("gate %q must require the checks step to have succeeded", name)
		}
	}
	if !strings.Contains(stepText(t, doc, "Fail on a red check"), "inputs.job_mode != 'answer'") {
		t.Error("`Fail on a red check` must not fire for answer jobs: no checks ran, so rc is unset")
	}
	if v := stepText(t, doc, "Verify claude pushed a commit to the target branch"); !strings.Contains(v, "inputs.job_mode == 'answer'") {
		t.Error("the push guard must still run for answer jobs, which skip the spec step it otherwise requires")
	}
	// The answer agent: runs for answer only, as before the wave (no spec outputs).
	ans := stepText(t, doc, "Answer the review comments (claude-code-action)")
	if !strings.Contains(ans, "if: ${{ inputs.job_mode == 'answer' }}") {
		t.Error("the answer agent step must run only for answer jobs")
	}
	args := regexp.MustCompile(`(?m)^ *claude_args: .*$`).FindString(ans)
	if args == "" || strings.Contains(ans, "steps.spec.") || strings.Contains(args, "--model") || strings.Contains(args, "--tools") {
		t.Errorf("the answer agent must not take --model/--tools from the task spec:\n%s", ans)
	}
	if !strings.Contains(ans, "--disallowedTools ${{ steps.steptools.outputs.disallowed }}") {
		t.Error("the answer agent keeps its pre-wave built-in scope (step-tools --disallowed)")
	}
	if !strings.Contains(stepText(t, doc, "Resolve the answer step's built-in tool scope"), "if: ${{ inputs.job_mode == 'answer' }}") {
		t.Error("the step-tools resolution must run only for answer jobs")
	}
}

// TestDesignWorkflow_AnswerValidationNeedsNoSpecIds runs the first step: an answer
// dispatch with no activity, task or spec hash passes; a draft one with them
// missing still fails; an answer one that supplies a malformed value still fails.
func TestDesignWorkflow_AnswerValidationNeedsNoSpecIds(t *testing.T) {
	requireJQ(t)
	validate := parseSteps(t, renderedDesignWorkflow(t))[0]
	base := map[string]string{
		"command": "design-answer", "artifact_kind": "Mission", "job_mode": "answer",
		"target_branch": "aiarch-design/shop/0", "prior_state_ref": "",
		"activity_id": "", "task_id": "", "spec_hash": "", "revision": "",
	}
	run := func(vals map[string]string) error {
		env := map[string]string{
			"GITHUB_ENV":    filepath.Join(t.TempDir(), "env"),
			"GITHUB_OUTPUT": filepath.Join(t.TempDir(), "out"),
		}
		for k, v := range validate.env {
			for _, in := range designInputs {
				if v == "${{ inputs."+in+" }}" {
					env[k] = vals[in]
				}
			}
		}
		_, err := runStepScript(t, validate.run, t.TempDir(), env)
		return err
	}
	if err := run(base); err != nil {
		t.Errorf("an answer dispatch without activity_id/task_id/spec_hash was refused: %v", err)
	}
	draft := maps.Clone(base)
	draft["job_mode"] = "draft"
	if err := run(draft); err == nil {
		t.Error("a draft dispatch without the task-spec ids must still be refused")
	}
	bad := maps.Clone(base)
	bad["spec_hash"] = "not-a-hash"
	if err := run(bad); err == nil {
		t.Error("an answer dispatch that supplies a malformed spec_hash must still be refused")
	}
}

// TestConstructWorkflow_ChecksOutTheActivityBranchBeforeTheSpec (R31): the job
// hashes the state the server hashed, which lives on activity/<id>.
func TestConstructWorkflow_ChecksOutTheActivityBranchBeforeTheSpec(t *testing.T) {
	doc := renderedConstructWorkflow(t)
	co := strings.Index(doc, "      - name: Check out the activity branch\n")
	spec := strings.Index(doc, "      - name: Resolve the task spec\n")
	if co < 0 || spec < 0 || co > spec {
		t.Fatalf("the activity branch must be checked out before the spec step hashes (checkout at %d, spec at %d)", co, spec)
	}
	// ...and before anything else reads the working tree's state.
	for _, later := range []string{"Obtain the aiarch-state MCP server", "Materialize the .claude prompt surface"} {
		if at := strings.Index(doc, "- name: "+later); at < co {
			t.Errorf("%q must come after the activity-branch checkout", later)
		}
	}
	body := stepText(t, doc, "Check out the activity branch")
	for _, want := range []string{
		`activity/${AIARCH_ACTIVITY_ID}`,
		`git ls-remote --exit-code --heads origin`,
		`git checkout -f -B "${branch}"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("activity-branch step is missing %q", want)
		}
	}
}

// TestConstructWorkflow_ActivityBranchStepBehaves runs the checkout step against a
// real repository: with the branch on origin the work tree lands on its tip (and
// the spec step's base is that tip); without it the job stays on the dispatch ref;
// an unreadable origin fails.
func TestConstructWorkflow_ActivityBranchStepBehaves(t *testing.T) {
	steps := parseSteps(t, renderedConstructWorkflow(t))
	co := stepNamed(t, steps, "Check out the activity branch")
	env := map[string]string{"AIARCH_ACTIVITY_ID": "C-orders"}

	remote, work := bareWithBranch(t, "main")
	tip := commitOn(t, remote, "activity/C-orders", "main", "task1.txt")
	if out, err := runStepScript(t, co.run, work, env); err != nil {
		t.Fatalf("checkout with the branch present: %v\n%s", err, out)
	}
	if got := git(t, work, "rev-parse", "HEAD"); got != tip {
		t.Errorf("HEAD = %s, want the activity branch tip %s", got, tip)
	}
	if _, err := os.Stat(filepath.Join(work, "task1.txt")); err != nil {
		t.Errorf("the earlier task's file is not in the work tree: %v", err)
	}

	_, work2 := bareWithBranch(t, "main")
	before := git(t, work2, "rev-parse", "HEAD")
	if out, err := runStepScript(t, co.run, work2, env); err != nil {
		t.Fatalf("checkout with no activity branch yet must pass: %v\n%s", err, out)
	}
	if got := git(t, work2, "rev-parse", "HEAD"); got != before {
		t.Errorf("with no activity branch HEAD moved to %s, want the dispatch ref %s", got, before)
	}

	_, work3 := bareWithBranch(t, "main")
	git(t, work3, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))
	if out, err := runStepScript(t, co.run, work3, env); err == nil {
		t.Errorf("an unreadable origin must fail, not read as 'no activity branch':\n%s", out)
	}
}
