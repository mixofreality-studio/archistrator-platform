package methodassets

// workflow_injection_test.go extends the construct workflow's SCRIPT-INJECTION posture
// (construct_injection_test.go) to EVERY workflow template method-assets seats
// (archistrator B1 fix round, I3). The design workflow runs with contents:write,
// id-token:write and the Claude token, and anyone holding only Actions:write can
// dispatch it with arbitrary input values, so it gets the same three guarantees:
//
//   - no `${{ … }}` expression inside any `run:` body, in any template;
//   - every dispatch input validated by the FIRST step, before any use;
//   - the MCP config built by `jq -n --arg …` from env, never a heredoc of JSON.

import (
	"encoding/json"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// runKeyLine matches a `run:` key, as a step field or as the step's first key.
var runKeyLine = regexp.MustCompile(`^(\s*)(- )?run:(.*)$`)

// runBodies returns every `run:` value in doc, block scalar or inline. It keys on the
// `run:` field itself rather than on named steps, so an unnamed step (`- run: …`) or a
// folded scalar (`run: >`) cannot slip past.
func runBodies(doc string) []string {
	lines := strings.Split(doc, "\n")
	var bodies []string
	for i := range lines {
		m := runKeyLine.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		keyIndent := len(m[1]) + len(m[2])
		val := strings.TrimSpace(m[3])
		if val != "" && val[0] != '|' && val[0] != '>' {
			bodies = append(bodies, val)
			continue
		}
		var body []string
		for k := i + 1; k < len(lines); k++ {
			l := lines[k]
			if strings.TrimSpace(l) == "" {
				body = append(body, "")
				continue
			}
			if len(l)-len(strings.TrimLeft(l, " ")) <= keyIndent {
				break
			}
			body = append(body, l)
		}
		bodies = append(bodies, strings.Join(body, "\n"))
	}
	return bodies
}

// workflowTemplateSources returns every workflow template's raw text, keyed by name.
// It globs the embedded directory, so a template added later is covered with no edit.
func workflowTemplateSources(t *testing.T) map[string]string {
	t.Helper()
	names, err := fs.Glob(assetsFS, "assets/workflows/*.tmpl")
	if err != nil || len(names) == 0 {
		t.Fatalf("no workflow templates found: %v", err)
	}
	out := map[string]string{}
	for _, n := range names {
		b, err := assetsFS.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		out[n] = string(b)
	}
	return out
}

// renderedWorkflows returns every rendered .github/workflows file the scaffold seats.
func renderedWorkflows(t *testing.T) map[string]string {
	t.Helper()
	files, err := ScaffoldFiles(testScaffoldData())
	if err != nil {
		t.Fatalf("render scaffold: %v", err)
	}
	out := map[string]string{}
	for p, b := range files {
		if strings.HasPrefix(p, ".github/workflows/") {
			out[p] = string(b)
		}
	}
	if len(out) == 0 {
		t.Fatal("the scaffold renders no workflows")
	}
	return out
}

func testScaffoldData() ScaffoldData {
	return ScaffoldData{
		ModulePath:            "github.com/acme/shop",
		AppSlug:               "aiarch-app",
		ProjectID:             "shop",
		Owner:                 "acme",
		Name:                  "shop",
		StateMcpModulePath:    "github.com/mixofreality-studio/archistrator/server/cmd/aiarch-state-mcp",
		StateMcpModuleVersion: "0123456789abcdef0123456789abcdef01234567",
	}
}

// TestEveryWorkflowTemplate_NoExpressionInsideAnyRunBody is the structural gate over
// ALL templates, raw and rendered: the script-injection class needs a `${{` inside a
// script, so no script in any seated workflow may contain one.
func TestEveryWorkflowTemplate_NoExpressionInsideAnyRunBody(t *testing.T) {
	check := func(label, doc string) int {
		n := 0
		for _, body := range runBodies(doc) {
			n++
			if strings.Contains(body, "${{") {
				t.Errorf("%s: a run: body expands a GitHub expression; move the value to the step's env:\n%s", label, body)
			}
		}
		return n
	}
	sources := workflowTemplateSources(t)
	for name, doc := range sources {
		if check(name, doc) == 0 {
			t.Errorf("%s: the reader found no run: bodies", name)
		}
	}
	rendered := renderedWorkflows(t)
	if len(rendered) != len(sources) {
		t.Errorf("the scaffold renders %d workflows from %d templates", len(rendered), len(sources))
	}
	for p, doc := range rendered {
		check(p, doc)
	}
}

// TestRunBodiesReaderSeesEveryShape keeps the gate honest: each shape a run: body can
// take, with an expression in it, is found.
func TestRunBodiesReaderSeesEveryShape(t *testing.T) {
	doc := strings.Join([]string{
		"jobs:",
		"  a:",
		"    steps:",
		"      - run: echo ${{ inputs.x }}",
		"      - name: literal",
		"        run: |",
		"          echo ok",
		"          echo ${{ inputs.y }}",
		"      - name: folded",
		"        run: >-",
		"          echo ${{ inputs.z }}",
		"      - name: quoted",
		`        run: "echo ${{ inputs.w }}"`,
	}, "\n")
	bodies := runBodies(doc)
	if len(bodies) != 4 {
		t.Fatalf("found %d run bodies, want 4: %q", len(bodies), bodies)
	}
	for _, b := range bodies {
		if !strings.Contains(b, "${{") {
			t.Errorf("the reader lost the expression in %q", b)
		}
	}
}

func renderedDesignWorkflow(t *testing.T) string {
	t.Helper()
	doc, ok := renderedWorkflows(t)[".github/workflows/aiarch-design.yml"]
	if !ok {
		t.Fatal("the scaffold renders no .github/workflows/aiarch-design.yml")
	}
	return doc
}

// designInputs are the design workflow's dispatch inputs the first step validates.
var designInputs = []string{
	"command", "artifact_kind", "job_mode", "target_branch", "prior_state_ref",
	"activity_id", "task_id", "spec_hash", "revision",
}

// TestDesignWorkflow_ValidatesInputsBeforeAnyUse pins that validation is the draft
// job's first step and reads every input from env.
func TestDesignWorkflow_ValidatesInputsBeforeAnyUse(t *testing.T) {
	steps := parseSteps(t, renderedDesignWorkflow(t))
	if steps[0].name != "Validate dispatch inputs" {
		t.Fatalf("the first step is %q; it must be \"Validate dispatch inputs\"", steps[0].name)
	}
	for _, in := range designInputs {
		found := false
		for _, v := range steps[0].env {
			if v == "${{ inputs."+in+" }}" {
				found = true
			}
		}
		if !found {
			t.Errorf("the validation step does not read inputs.%s from env", in)
		}
	}
}

// TestDesignWorkflow_VerifyNeverRunsOnUnvalidatedInputs pins the one always() step:
// it puts target_branch into an API path, so it must be gated on validation.
func TestDesignWorkflow_VerifyNeverRunsOnUnvalidatedInputs(t *testing.T) {
	doc := renderedDesignWorkflow(t)
	re := regexp.MustCompile(`(?m)^      - name: Verify claude pushed a commit to the target branch\n        if: (.*)$`)
	m := re.FindStringSubmatch(doc)
	if m == nil {
		t.Fatal("the verify step must declare its if: directly under its name")
	}
	if !strings.Contains(m[1], "steps.inputs.outcome == 'success'") || !strings.Contains(m[1], "steps.spec.outcome == 'success'") {
		t.Errorf("the verify step runs on always(); it must also require the inputs and the spec to have passed, got %q", m[1])
	}
	if !regexp.MustCompile(`(?m)^      - name: Validate dispatch inputs\n        id: inputs$`).MatchString(doc) {
		t.Error("the validation step must carry id: inputs, which the verify step's guard reads")
	}
}

// TestDesignWorkflow_MCPConfigIsJQFromEnv pins the config step's shape.
func TestDesignWorkflow_MCPConfigIsJQFromEnv(t *testing.T) {
	cfg := stepNamed(t, parseSteps(t, renderedDesignWorkflow(t)), "Write the aiarch-state MCP config")
	if !strings.Contains(cfg.run, "jq -n") || strings.Contains(cfg.run, "<<") {
		t.Errorf("the MCP config must be built with jq -n from env, never a heredoc:\n%s", cfg.run)
	}
	for _, k := range []string{
		"AIARCH_ARTIFACT_KIND", "AIARCH_JOB_MODE", "AIARCH_TARGET_BRANCH", "AIARCH_COMMAND", "AIARCH_PROJECT_ID", "AIARCH_STATE_ROOT",
		"AIARCH_ACTIVITY_ID", "AIARCH_TASK_ID", "AIARCH_SPEC_HASH",
	} {
		if cfg.env[k] == "" {
			t.Errorf("the MCP-config step must take %s from env; env = %v", k, cfg.env)
		}
	}
}

// TestDesignWorkflow_MCPConfigCarriesHostileValuesVerbatim runs the rendered config
// script under bash. Validation refuses these values first, so this is the second
// wall: even so, a hostile value lands in the JSON byte-exact and never executes.
func TestDesignWorkflow_MCPConfigCarriesHostileValuesVerbatim(t *testing.T) {
	requireJQ(t)
	cfg := stepNamed(t, parseSteps(t, renderedDesignWorkflow(t)), "Write the aiarch-state MCP config")
	for name, v := range hostileNotes() {
		t.Run(name, func(t *testing.T) {
			work, tmp := t.TempDir(), t.TempDir()
			env := map[string]string{
				"RUNNER_TEMP":          tmp,
				"MCP_BIN":              "/opt/aiarch-bin/aiarch-state-mcp",
				"AIARCH_PROJECT_ID":    "shop",
				"AIARCH_ARTIFACT_KIND": v,
				"AIARCH_JOB_MODE":      v,
				"AIARCH_TARGET_BRANCH": v,
				"AIARCH_STATE_ROOT":    "/home/runner/work/shop/shop",
				"AIARCH_COMMAND":       v,
				"AIARCH_ACTIVITY_ID":   v,
				"AIARCH_TASK_ID":       v,
				"AIARCH_SPEC_HASH":     v,
			}
			if out, err := runStepScript(t, cfg.run, work, env); err != nil {
				t.Fatalf("the MCP-config script failed on a hostile value: %v\n%s", err, out)
			}
			assertNothingExecuted(t, work, tmp)
			raw, err := os.ReadFile(filepath.Join(tmp, "aiarch-mcp.json"))
			if err != nil {
				t.Fatalf("no MCP config written: %v", err)
			}
			var doc struct {
				MCPServers map[string]struct {
					Command string            `json:"command"`
					Env     map[string]string `json:"env"`
				} `json:"mcpServers"`
			}
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatalf("the MCP config is not valid JSON: %v\n%s", err, raw)
			}
			srv, ok := doc.MCPServers["aiarch-state"]
			if !ok || len(doc.MCPServers) != 1 {
				t.Fatalf("the config must declare exactly aiarch-state: %s", raw)
			}
			var keys []string
			for k := range srv.Env {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			if strings.Join(keys, ",") != "AIARCH_ACTIVITY_ID,AIARCH_ARTIFACT_KIND,AIARCH_COMMAND,AIARCH_JOB_MODE,AIARCH_PROJECT_ID,AIARCH_SPEC_HASH,AIARCH_STATE_ROOT,AIARCH_TARGET_BRANCH,AIARCH_TASK_ID" {
				t.Fatalf("env keys = %v", keys)
			}
			for _, k := range []string{"AIARCH_ARTIFACT_KIND", "AIARCH_JOB_MODE", "AIARCH_TARGET_BRANCH", "AIARCH_COMMAND", "AIARCH_ACTIVITY_ID", "AIARCH_TASK_ID", "AIARCH_SPEC_HASH"} {
				if srv.Env[k] != v {
					t.Errorf("%s did not round-trip byte-exact:\n got %q\nwant %q", k, srv.Env[k], v)
				}
			}
		})
	}
}

// TestDesignWorkflow_ValidationRejectsHostileInputs runs the validation step: the
// shapes the server sends pass, and every hostile value fails the job without
// executing.
func TestDesignWorkflow_ValidationRejectsHostileInputs(t *testing.T) {
	requireJQ(t)
	validate := parseSteps(t, renderedDesignWorkflow(t))[0]
	good := map[string]string{
		"command": "mission-draft", "artifact_kind": "ScrubbedRequirements", "job_mode": "draft",
		"target_branch": "aiarch-design/gtd-qa2/3-amend-2", "prior_state_ref": "",
		"activity_id": "SD-mission", "task_id": "draft", "revision": "",
		"spec_hash": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	ghEnv := filepath.Join(t.TempDir(), "github_env")
	ghOut := filepath.Join(t.TempDir(), "github_output")
	envFor := func(vals map[string]string) map[string]string {
		env := map[string]string{"GITHUB_ENV": ghEnv, "GITHUB_OUTPUT": ghOut}
		for k, v := range validate.env {
			for _, in := range designInputs {
				if v == "${{ inputs."+in+" }}" {
					env[k] = vals[in]
				}
			}
		}
		return env
	}
	with := func(in, v string) map[string]string {
		vals := map[string]string{}
		maps.Copy(vals, good)
		vals[in] = v
		return vals
	}
	work := t.TempDir()
	valids := []map[string]string{
		good,
		with("job_mode", "critique"),
		with("job_mode", "answer"),
		with("artifact_kind", "Mission"),
		with("target_branch", "aiarch-design/shop/0"),
		with("prior_state_ref", "0123456789abcdef0123456789abcdef01234567"),
		with("prior_state_ref", "main"),
		with("prior_state_ref", "release/v1.2"),
		with("activity_id", "C-billing.v2:x/y"),
		with("task_id", "critique"),
		with("revision", "SD-mission:draft:2"),
	}
	for _, vals := range valids {
		if out, err := runStepScript(t, validate.run, work, envFor(vals)); err != nil {
			t.Errorf("valid inputs %v were rejected: %v\n%s", vals, err, out)
		}
	}
	common := []string{
		"a; touch PWNED_SEMI", "$(touch PWNED_SUBST)", "`touch PWNED_TICK`", "a b",
		"-rf", "a\ntouch PWNED_NL", "${{ secrets.CLAUDE_CODE_OAUTH_TOKEN }}", `a"b`,
		"a..b", "../x", "x/../y", "..", strings.Repeat("a", 256), "a'b", "a|b", "a&b",
	}
	hostile := map[string][]string{
		"command":         append([]string{"", "Mission-Draft", "mission_draft", "-x"}, common...),
		"artifact_kind":   append([]string{"", "9Mission", "mission-draft", "Mission.x"}, common...),
		"job_mode":        append([]string{"", "Draft", "draft ", "draftx", "construct", "draft|critique"}, common...),
		"target_branch":   append([]string{"", "/abs", "trail/", "a//b", "a/.hidden", "a.", "a/-x", "refs/heads/../main"}, common...),
		"prior_state_ref": append([]string{"/abs", "a//b", "HEAD~1", "@{-1}", "a.lock/.."}, common...),
		"activity_id":     append([]string{"", "/abs", "a//b", "a/./b", "C-PE.", ".hidden"}, common...),
		"task_id":         append([]string{"", "/abs", "a//b", "a/./b", "x.", ".x"}, common...),
		"revision":        append([]string{"/abs", "a//b", "a/./b", "x.", ".x"}, common...),
		"spec_hash": append([]string{
			"", strings.Repeat("0", 63), strings.Repeat("0", 65), strings.Repeat("A", 64), strings.Repeat("g", 64),
			strings.Repeat("0", 64) + "\ntouch PWNED_HASH",
		}, common...),
	}
	for in, vals := range hostile {
		for _, h := range vals {
			_ = os.Remove(ghOut)
			if out, err := runStepScript(t, validate.run, work, envFor(with(in, h))); err == nil {
				t.Errorf("a hostile %s %q passed validation\n%s", in, h, out)
			}
			if raw, _ := os.ReadFile(ghOut); strings.Contains(string(raw), "ok=true") {
				t.Errorf("a hostile %s %q set inputs_ok: the validate job would run on it", in, h)
			}
		}
	}
	assertNothingExecuted(t, work)

	// A valid dispatch exports the validated ids for every later step.
	if err := os.Remove(ghEnv); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if out, err := runStepScript(t, validate.run, work, envFor(good)); err != nil {
		t.Fatalf("valid inputs were rejected: %v\n%s", err, out)
	}
	exported, err := os.ReadFile(ghEnv)
	if err != nil {
		t.Fatalf("the validation step exported nothing: %v", err)
	}
	for _, line := range []string{"AIARCH_ACTIVITY_ID=SD-mission", "AIARCH_TASK_ID=draft", "AIARCH_SPEC_HASH=" + good["spec_hash"]} {
		if !strings.Contains("\n"+string(exported), "\n"+line+"\n") {
			t.Errorf("$GITHUB_ENV is missing %q:\n%s", line, exported)
		}
	}
	if raw, _ := os.ReadFile(ghOut); !strings.Contains(string(raw), "ok=true") {
		t.Errorf("valid inputs must set the step's ok output (the draft job's inputs_ok):\n%s", raw)
	}
}

// TestDesignWorkflow_ValidateRunsWhenDraftFails pins the required check against
// the skip-means-pass trap: draft fails exactly when its rule checks are red, so
// validate must run whenever draft's inputs validated, not only when draft passed.
func TestDesignWorkflow_ValidateRunsWhenDraftFails(t *testing.T) {
	doc := renderedDesignWorkflow(t)
	if !regexp.MustCompile(`(?m)^  validate:\n    runs-on: ubuntu-latest\n    needs: draft\n    if: \$\{\{ !cancelled\(\) && needs\.draft\.outputs\.inputs_ok == 'true' \}\}$`).MatchString(doc) {
		t.Error("the validate job must run on `!cancelled() && needs.draft.outputs.inputs_ok == 'true'`")
	}
	if !strings.Contains(doc, "    outputs:\n      inputs_ok: ${{ steps.inputs.outputs.ok }}\n") {
		t.Error("the draft job must expose inputs_ok from the validation step's ok output")
	}
}
