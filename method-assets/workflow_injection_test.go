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
	for i := 0; i < len(lines); i++ {
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
var designInputs = []string{"command", "artifact_kind", "job_mode", "target_branch", "prior_state_ref"}

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
	if !strings.Contains(m[1], "steps.inputs.outcome == 'success'") {
		t.Errorf("the verify step runs on always(); it must also require steps.inputs.outcome == 'success', got %q", m[1])
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
	for _, k := range []string{"AIARCH_ARTIFACT_KIND", "AIARCH_JOB_MODE", "AIARCH_TARGET_BRANCH", "AIARCH_COMMAND", "AIARCH_PROJECT_ID", "AIARCH_STATE_ROOT"} {
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
			if strings.Join(keys, ",") != "AIARCH_ARTIFACT_KIND,AIARCH_COMMAND,AIARCH_JOB_MODE,AIARCH_PROJECT_ID,AIARCH_STATE_ROOT,AIARCH_TARGET_BRANCH" {
				t.Fatalf("env keys = %v", keys)
			}
			for _, k := range []string{"AIARCH_ARTIFACT_KIND", "AIARCH_JOB_MODE", "AIARCH_TARGET_BRANCH", "AIARCH_COMMAND"} {
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
	}
	envFor := func(vals map[string]string) map[string]string {
		env := map[string]string{}
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
		for k, g := range good {
			vals[k] = g
		}
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
	}
	for in, vals := range hostile {
		for _, h := range vals {
			if out, err := runStepScript(t, validate.run, work, envFor(with(in, h))); err == nil {
				t.Errorf("a hostile %s %q passed validation\n%s", in, h, out)
			}
		}
	}
	assertNothingExecuted(t, work)
}
