package methodassets

// workflow_gates_test.go runs the task-spec steps of both seated workflows under
// bash, the way the runner does, against a fake aiarch-state-mcp and real git
// repositories: the spec step refuses a drifted hash and any model or tool list
// that is not a plain value, the checks step fails clearly when the agent never
// pushed, and the record step survives a branch that moved under it.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gateWorkflows are the two workflows that run the task spec, with the branch
// each one publishes to and the env that names it.
func gateWorkflows(t *testing.T) map[string]struct {
	steps  []workflowStep
	branch string
	env    map[string]string
} {
	t.Helper()
	return map[string]struct {
		steps  []workflowStep
		branch string
		env    map[string]string
	}{
		"construct": {parseSteps(t, renderedConstructWorkflow(t)), "activity/C-orders", map[string]string{}},
		"design":    {parseSteps(t, renderedDesignWorkflow(t)), "aiarch-design/shop/0", map[string]string{"TARGET_BRANCH": "aiarch-design/shop/0"}},
	}
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/bash\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "init.defaultBranch=main"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HOME="+dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func readOutputs(t *testing.T, path string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}
	}
	out := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			out[k] = v
		}
	}
	return out
}

const gateHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestTaskSpecStep_RefusesDriftAndUnplainValues(t *testing.T) {
	requireJQ(t)
	for name, wf := range gateWorkflows(t) {
		spec := stepNamed(t, wf.steps, "Resolve the task spec")
		run := func(t *testing.T, specJSON, printedHash, task string) (map[string]string, string, error) {
			t.Helper()
			work, tmp := t.TempDir(), t.TempDir()
			git(t, work, "init", "-q")
			git(t, work, "commit", "-q", "--allow-empty", "-m", "base")
			fixture := filepath.Join(tmp, "fixture.json")
			if err := os.WriteFile(fixture, []byte(specJSON), 0o644); err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(tmp, "aiarch-state-mcp")
			writeExecutable(t, bin, `while [ $# -gt 0 ]; do [ "$1" = --out ] && out="$2"; shift; done
cp "$SPEC_FIXTURE" "$out"
printf '%s\n' "$PRINTED_HASH"
`)
			outFile := filepath.Join(tmp, "output")
			env := map[string]string{
				"MCP_BIN": bin, "RUNNER_TEMP": tmp, "GITHUB_OUTPUT": outFile, "SPEC_FIXTURE": fixture,
				"PRINTED_HASH": printedHash, "AIARCH_ACTIVITY_ID": "C-orders", "AIARCH_TASK_ID": task,
				"AIARCH_SPEC_HASH": gateHash,
			}
			out, err := runStepScript(t, spec.run, work, env)
			assertNothingExecuted(t, work, tmp)
			return readOutputs(t, outFile), out, err
		}
		t.Run(name+"/plain spec passes", func(t *testing.T) {
			t.Parallel()
			outs, log, err := run(t, `{"model":{"id":"opus"},"tools":{"builtin":["Bash","Read","Skill"]}}`, gateHash, "integration")
			if err != nil {
				t.Fatalf("a matching, plain spec was refused: %v\n%s", err, log)
			}
			if outs["model"] != "opus" || outs["builtin"] != "Bash,Read,Skill" {
				t.Errorf("outputs = %v", outs)
			}
			if name == "construct" && (outs["integration"] != "true" || outs["base"] == "") {
				t.Errorf("the construct spec step must output the base and the integration flag: %v", outs)
			}
		})
		t.Run(name+"/empty allowlist passes", func(t *testing.T) {
			t.Parallel()
			outs, log, err := run(t, `{"model":{"id":"sonnet"},"tools":{"builtin":[]}}`, gateHash, "construction")
			if err != nil {
				t.Fatalf("an empty built-in allowlist means none and must pass: %v\n%s", err, log)
			}
			if b, ok := outs["builtin"]; !ok || b != "" {
				t.Errorf("builtin output = %q (present %v), want empty", b, ok)
			}
			if name == "construct" && outs["integration"] != "false" {
				t.Errorf("a non-integration task must not seed scenario tests: %v", outs)
			}
		})
		for label, c := range map[string]struct{ spec, hash string }{
			"drifted hash":         {`{"model":{"id":"opus"},"tools":{"builtin":["Read"]}}`, strings.Repeat("f", 64)},
			"missing model":        {`{"tools":{"builtin":["Read"]}}`, gateHash},
			"json null model":      {`{"model":{"id":null},"tools":{"builtin":["Read"]}}`, gateHash},
			"literal null model":   {`{"model":{"id":"null"},"tools":{"builtin":["Read"]}}`, gateHash},
			"newline model":        {`{"model":{"id":"opus\n--evil"},"tools":{"builtin":["Read"]}}`, gateHash},
			"spaced model":         {`{"model":{"id":"opus --x"},"tools":{"builtin":["Read"]}}`, gateHash},
			"missing builtin":      {`{"model":{"id":"opus"},"tools":{}}`, gateHash},
			"newline builtin":      {`{"model":{"id":"opus"},"tools":{"builtin":["Read\nx=1"]}}`, gateHash},
			"quoted builtin":       {`{"model":{"id":"opus"},"tools":{"builtin":["Read' --x '"]}}`, gateHash},
			"literal null tools":   {`{"model":{"id":"opus"},"tools":{"builtin":["null"]}}`, gateHash},
			"substitution builtin": {`{"model":{"id":"opus"},"tools":{"builtin":["$(touch PWNED)"]}}`, gateHash},
		} {
			t.Run(name+"/"+label, func(t *testing.T) {
				t.Parallel()
				outs, log, err := run(t, c.spec, c.hash, "integration")
				if err == nil {
					t.Fatalf("the spec step accepted a %s:\n%s", label, log)
				}
				if _, ok := outs["model"]; ok {
					t.Errorf("a refused spec must write no model output: %v", outs)
				}
			})
		}
	}
}

// bareWithBranch makes a bare remote holding branch with one state commit, and a
// working clone of it on that branch.
func bareWithBranch(t *testing.T, branch string) (remote, work string) {
	t.Helper()
	root := t.TempDir()
	remote, seed, work := filepath.Join(root, "remote.git"), filepath.Join(root, "seed"), filepath.Join(root, "work")
	git(t, root, "init", "-q", "--bare", remote)
	git(t, root, "clone", "-q", remote, seed)
	if err := os.MkdirAll(filepath.Join(seed, ".aiarch", "state"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seed, ".aiarch", "state", "project.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, seed, "add", ".")
	git(t, seed, "commit", "-q", "-m", "seed")
	git(t, seed, "push", "-q", "origin", "HEAD:refs/heads/"+branch)
	git(t, root, "clone", "-q", "--branch", branch, remote, work)
	return remote, work
}

func TestChecksStep_FailsClearlyWhenTheAgentNeverPushed(t *testing.T) {
	for name, wf := range gateWorkflows(t) {
		t.Run(name, func(t *testing.T) {
			checks := stepNamed(t, wf.steps, "Run the checks")
			_, work := bareWithBranch(t, "main-only")
			tmp := t.TempDir()
			bin := filepath.Join(tmp, "aiarch-state-mcp")
			writeExecutable(t, bin, "touch \"$RUNNER_TEMP/RAN\"\n")
			outFile := filepath.Join(tmp, "output")
			env := map[string]string{"MCP_BIN": bin, "RUNNER_TEMP": tmp, "GITHUB_OUTPUT": outFile, "AIARCH_ACTIVITY_ID": "C-orders"}
			for k, v := range wf.env {
				env[k] = v
			}
			log, err := runStepScript(t, checks.run, work, env)
			if err == nil {
				t.Fatalf("the checks step passed on a branch nobody pushed:\n%s", log)
			}
			if outs := readOutputs(t, outFile); outs["rc"] != "1" {
				t.Errorf("rc = %q, want 1 (the fail-on-red step reads it)", outs["rc"])
			}
			if !strings.Contains(log, "::error::"+wf.branch+" was never pushed") {
				t.Errorf("the failure must say the branch was never pushed:\n%s", log)
			}
			if _, err := os.Stat(filepath.Join(tmp, "RAN")); err == nil {
				t.Error("no check may run on a branch nobody pushed")
			}
		})
	}
}

func TestRecordStep_RetriesOnceOnAMovedBranch(t *testing.T) {
	for name, wf := range gateWorkflows(t) {
		record := stepNamed(t, wf.steps, "Record the check run")
		env := func(tmp, bin string) map[string]string {
			e := map[string]string{
				"MCP_BIN": bin, "RUNNER_TEMP": tmp, "GITHUB_RUN_ID": "7", "GITHUB_RUN_ATTEMPT": "2",
				"AIARCH_ACTIVITY_ID": "C-orders", "AIARCH_REVISION": "C-orders:integration:1",
			}
			for k, v := range wf.env {
				e[k] = v
			}
			return e
		}
		t.Run(name+"/moved branch", func(t *testing.T) {
			remote, work := bareWithBranch(t, wf.branch)
			// Another writer advances the branch after this job checked it out.
			other := filepath.Join(t.TempDir(), "other")
			git(t, filepath.Dir(other), "clone", "-q", "--branch", wf.branch, remote, other)
			if err := os.WriteFile(filepath.Join(other, "elsewhere.txt"), []byte("x\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			git(t, other, "add", ".")
			git(t, other, "commit", "-q", "-m", "moved")
			git(t, other, "push", "-q", "origin", "HEAD:"+wf.branch)

			tmp := t.TempDir()
			bin := filepath.Join(tmp, "aiarch-state-mcp")
			writeExecutable(t, bin, "echo \"$*\" >> .aiarch/state/project.json\n")
			log, err := runStepScript(t, record.run, work, env(tmp, bin))
			if err != nil {
				t.Fatalf("the record step did not survive a moved branch: %v\n%s", err, log)
			}
			if !strings.Contains(log, "::warning::the record push was rejected") {
				t.Errorf("the retry must be announced:\n%s", log)
			}
			subjects := git(t, remote, "log", "--format=%s", wf.branch)
			if !strings.HasPrefix(subjects, "checks: record run gh-7-2\nmoved\n") {
				t.Errorf("the record must land once, on the moved tip; branch log:\n%s", subjects)
			}
			state := git(t, remote, "show", wf.branch+":.aiarch/state/project.json")
			for _, want := range []string{"--run-id gh-7-2", "--artifact check-results-C-orders-7-2", "--revision C-orders:integration:1"} {
				if !strings.Contains(state, want) {
					t.Errorf("record-check-run was not called with %q:\n%s", want, state)
				}
			}
		})
		t.Run(name+"/nothing to record", func(t *testing.T) {
			remote, work := bareWithBranch(t, wf.branch)
			tmp := t.TempDir()
			bin := filepath.Join(tmp, "aiarch-state-mcp")
			writeExecutable(t, bin, "exit 0\n")
			before := git(t, remote, "rev-parse", wf.branch)
			if log, err := runStepScript(t, record.run, work, env(tmp, bin)); err != nil {
				t.Fatalf("an unchanged state must not fail the record (no empty commit): %v\n%s", err, log)
			}
			if after := git(t, remote, "rev-parse", wf.branch); after != before {
				t.Errorf("an unchanged state must push no commit: %s -> %s", before, after)
			}
		})
	}
}
