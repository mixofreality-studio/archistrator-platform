package methodassets

// construct_injection_test.go pins the construct workflow's SCRIPT-INJECTION posture
// (archistrator plan-B1-B2 amendment §C.1 / §C.4). The construct workflow now carries
// free operator text (`operator_note`), so the whole class is closed structurally:
//
//   - no `${{ … }}` expression is ever expanded inside a `run:` body — every input
//     and github.* value reaches a script through the step's `env:`, where the
//     runner assigns it as data and no shell ever parses it;
//   - the ids that still reach `prompt:` and a step output are validated by the
//     FIRST step, before anything uses them;
//   - the MCP config is built by `jq -n --arg …` from env, never a heredoc of JSON.
//
// The behavioural tests run the rendered scripts under bash with hostile values: a
// note full of quotes, backticks, `${{`, `$(…)`, newlines, a lone backslash, a
// heredoc terminator and JSON metacharacters must reach the config byte-exact and
// must never execute.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// renderedConstructWorkflow renders the construct template exactly as the scaffold
// seats it.
func renderedConstructWorkflow(t *testing.T) string {
	t.Helper()
	files, err := ScaffoldFiles(ScaffoldData{
		ModulePath:            "github.com/acme/shop",
		AppSlug:               "aiarch-app",
		ProjectID:             "shop",
		Owner:                 "acme",
		Name:                  "shop",
		StateMcpModulePath:    "github.com/mixofreality-studio/archistrator/server/cmd/aiarch-state-mcp",
		StateMcpModuleVersion: "0123456789abcdef0123456789abcdef01234567",
	})
	if err != nil {
		t.Fatalf("render scaffold: %v", err)
	}
	b, ok := files[".github/workflows/aiarch-construct.yml"]
	if !ok {
		t.Fatal("the scaffold renders no .github/workflows/aiarch-construct.yml")
	}
	return string(b)
}

// workflowStep is one `- name:` step of the construct job, as written.
type workflowStep struct {
	name string
	// env maps each `env:` key to its raw value text.
	env map[string]string
	// run is the step's `run:` body, dedented; empty when the step has none.
	run string
}

var (
	stepNameLine = regexp.MustCompile(`^(\s*)- name: (.+)$`)
	keyLine      = regexp.MustCompile(`^(\s*)([A-Za-z_][A-Za-z0-9_-]*):(.*)$`)
)

// parseSteps reads the steps of the rendered workflow. It is deliberately a small
// line reader for this file's own style (block-scalar `run: |` bodies, flat `env:`
// maps) rather than a YAML library: method-assets has no YAML dependency, and a
// step shape this reader does not understand fails the test rather than slipping by.
func parseSteps(t *testing.T, doc string) []workflowStep {
	t.Helper()
	lines := strings.Split(doc, "\n")
	var steps []workflowStep
	for i := 0; i < len(lines); i++ {
		m := stepNameLine.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		stepIndent := len(m[1])
		st := workflowStep{name: strings.TrimSpace(m[2]), env: map[string]string{}}
		fieldIndent := stepIndent + 2
		j := i + 1
		for ; j < len(lines); j++ {
			l := lines[j]
			if strings.TrimSpace(l) == "" || strings.HasPrefix(strings.TrimSpace(l), "#") {
				continue
			}
			ind := len(l) - len(strings.TrimLeft(l, " "))
			if ind <= stepIndent {
				break
			}
			km := keyLine.FindStringSubmatch(l)
			if km == nil || len(km[1]) != fieldIndent {
				continue
			}
			switch km[2] {
			case "env":
				for k := j + 1; k < len(lines); k++ {
					el := lines[k]
					if strings.TrimSpace(el) == "" || strings.HasPrefix(strings.TrimSpace(el), "#") {
						continue
					}
					em := keyLine.FindStringSubmatch(el)
					if em == nil || len(em[1]) <= fieldIndent {
						break
					}
					st.env[em[2]] = strings.TrimSpace(em[3])
				}
			case "run":
				val := strings.TrimSpace(km[3])
				if val != "|" {
					st.run = val
					continue
				}
				var body []string
				bodyIndent := -1
				for k := j + 1; k < len(lines); k++ {
					bl := lines[k]
					if strings.TrimSpace(bl) == "" {
						body = append(body, "")
						continue
					}
					bi := len(bl) - len(strings.TrimLeft(bl, " "))
					if bi <= fieldIndent {
						break
					}
					if bodyIndent < 0 {
						bodyIndent = bi
					}
					if bi < bodyIndent {
						t.Fatalf("step %q: run body dedents below its first line (line %d)", st.name, k+1)
					}
					body = append(body, bl[bodyIndent:])
				}
				st.run = strings.TrimRight(strings.Join(body, "\n"), "\n") + "\n"
			}
		}
		steps = append(steps, st)
		i = j - 1
	}
	if len(steps) == 0 {
		t.Fatal("parsed no steps from the rendered construct workflow")
	}
	return steps
}

func stepNamed(t *testing.T, steps []workflowStep, name string) workflowStep {
	t.Helper()
	for _, s := range steps {
		if s.name == name {
			return s
		}
	}
	t.Fatalf("the construct workflow has no step named %q", name)
	return workflowStep{}
}

// TestConstructWorkflow_NoExpressionInsideAnyRunBody is the structural half: the whole
// script-injection class needs a `${{` inside a script, so no script may contain one.
func TestConstructWorkflow_NoExpressionInsideAnyRunBody(t *testing.T) {
	steps := parseSteps(t, renderedConstructWorkflow(t))
	runs := 0
	for _, s := range steps {
		if s.run == "" {
			continue
		}
		runs++
		if strings.Contains(s.run, "${{") {
			t.Errorf("step %q expands a GitHub expression inside its run: body; move the value to the step's env:\n%s", s.name, s.run)
		}
	}
	if runs < 4 {
		t.Fatalf("found %d run: bodies; the reader missed the workflow's scripts", runs)
	}
}

// TestConstructWorkflow_DeclaresOptionalOperatorNote pins the new dispatch input: an
// optional string, described as reaching the agent through the MCP server only.
func TestConstructWorkflow_DeclaresOptionalOperatorNote(t *testing.T) {
	doc := renderedConstructWorkflow(t)
	re := regexp.MustCompile(`(?m)^      operator_note:\n        description: "([^"\n]*)"\n        required: false\n        type: string$`)
	m := re.FindStringSubmatch(doc)
	if m == nil {
		t.Fatal("workflow_dispatch must declare `operator_note` (required: false, type: string)")
	}
	if !strings.Contains(m[1], "MCP") || !strings.Contains(m[1], "never the prompt") {
		t.Errorf("operator_note's description must say it reaches the agent through the MCP server, never the prompt; got %q", m[1])
	}
}

// TestConstructWorkflow_ValidatesIdsBeforeAnyUse pins that the validation step is the
// job's first step, so no id reaches a checkout, a script, an output or the prompt
// before it has been proven to be an id.
func TestConstructWorkflow_ValidatesIdsBeforeAnyUse(t *testing.T) {
	steps := parseSteps(t, renderedConstructWorkflow(t))
	if steps[0].name != "Validate dispatch inputs" {
		t.Fatalf("the first step is %q; it must be \"Validate dispatch inputs\"", steps[0].name)
	}
	for _, in := range []string{"activity_id", "component_id", "command"} {
		found := false
		for k, v := range steps[0].env {
			if v == "${{ inputs."+in+" }}" {
				found = true
				_ = k
			}
		}
		if !found {
			t.Errorf("the validation step does not read inputs.%s from env", in)
		}
	}
	for _, v := range steps[0].env {
		if strings.Contains(v, "operator_note") {
			t.Error("the validation step must not read operator_note: the note is free text, never validated as an id and never used by a script")
		}
	}
}

// TestConstructWorkflow_OperatorNoteReachesOnlyTheMCPConfig pins where the note may go:
// the MCP-config step's env, and nowhere else — never the prompt, an output, or
// another step.
func TestConstructWorkflow_OperatorNoteReachesOnlyTheMCPConfig(t *testing.T) {
	doc := renderedConstructWorkflow(t)
	if got := strings.Count(doc, "inputs.operator_note"); got != 1 {
		t.Fatalf("inputs.operator_note is referenced %d times; exactly one reference (the MCP-config step's env) is allowed", got)
	}
	cfg := stepNamed(t, parseSteps(t, doc), "Write the aiarch-state MCP config")
	if cfg.env["AIARCH_OPERATOR_NOTE"] != "${{ inputs.operator_note }}" {
		t.Errorf("the MCP-config step must take the note from env AIARCH_OPERATOR_NOTE; env = %v", cfg.env)
	}
	if !strings.Contains(cfg.run, "jq -n") || strings.Contains(cfg.run, "<<") {
		t.Errorf("the MCP config must be built with jq -n from env, never a heredoc:\n%s", cfg.run)
	}
}

// hostileNotes are the values an operator note must survive verbatim.
func hostileNotes() map[string]string {
	big := strings.Repeat("0123456789abcdef", 1024) // 16 KiB
	return map[string]string{
		"quotes":          `he said "stop" and 'go'`,
		"json-close":      `"}}, "mcpServers": {"evil": {"command": "sh"}}`,
		"backticks":       "run `touch PWNED_BACKTICK` now",
		"command-subst":   "$(touch PWNED_SUBST) and ${HOME} and $PATH",
		"expression":      "${{ secrets.CLAUDE_CODE_OAUTH_TOKEN }} ${{ github.token }}",
		"newlines":        "line one\nline two\r\nline three\n",
		"lone-backslash":  `C:\ and a lone \`,
		"escapes-literal": `\n \t \" \u0000 \\`,
		"heredoc-eof":     "before\nEOF\ncat /etc/passwd\nEOF\nafter",
		"json-meta":       `{"a":[1,2,{"b":null}],"c":true}`,
		"workflow-cmd":    "::set-output name=x::y\n::add-mask::z\n%0A%25",
		"non-bmp":         "clef 𝄞 grin 😀 han 漢字",
		"sixteen-kib":     big,
	}
}

func requireJQ(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		if os.Getenv("CI") == "true" {
			t.Fatal("jq is required for the construct-workflow injection tests (it is preinstalled on ubuntu-latest)")
		}
		t.Skip("jq not on PATH; this test runs in CI, where it is required")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Fatal("bash is required for the construct-workflow injection tests")
	}
}

// runStepScript runs one step's run: body under bash, as the runner does (bash -e),
// with env, in a scratch working directory. It returns the combined output and the
// exit error.
func runStepScript(t *testing.T, script, dir string, env map[string]string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", "--noprofile", "--norc", "-eo", "pipefail", "-c", script)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// assertNothingExecuted fails if any hostile payload's side effect landed.
func assertNothingExecuted(t *testing.T, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		matches, _ := filepath.Glob(filepath.Join(d, "PWNED*"))
		if len(matches) > 0 {
			t.Fatalf("a hostile value EXECUTED: %v", matches)
		}
	}
}

// TestConstructWorkflow_MCPConfigCarriesHostileNotesVerbatim is the behavioural half of
// §C.4: the rendered config script, run under bash with each hostile note, writes valid
// JSON whose note round-trips byte-exact, and nothing in the note executes.
func TestConstructWorkflow_MCPConfigCarriesHostileNotesVerbatim(t *testing.T) {
	requireJQ(t)
	cfg := stepNamed(t, parseSteps(t, renderedConstructWorkflow(t)), "Write the aiarch-state MCP config")
	for name, note := range hostileNotes() {
		t.Run(name, func(t *testing.T) {
			work, tmp := t.TempDir(), t.TempDir()
			env := map[string]string{
				"RUNNER_TEMP":          tmp,
				"MCP_BIN":              "/opt/aiarch-bin/aiarch-state-mcp",
				"AIARCH_PROJECT_ID":    "shop",
				"AIARCH_COMPONENT_ID":  "orders-manager",
				"AIARCH_ACTIVITY_ID":   "C-orders-manager",
				"AIARCH_STATE_ROOT":    "/home/runner/work/shop/shop",
				"AIARCH_COMMAND":       "service-construction",
				"AIARCH_OPERATOR_NOTE": note,
			}
			if out, err := runStepScript(t, cfg.run, work, env); err != nil {
				t.Fatalf("the MCP-config script failed on a hostile note: %v\n%s", err, out)
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
			if len(doc.MCPServers) != 1 {
				t.Fatalf("the config declares %d MCP servers, want exactly aiarch-state: %v", len(doc.MCPServers), doc.MCPServers)
			}
			srv, ok := doc.MCPServers["aiarch-state"]
			if !ok {
				t.Fatalf("no aiarch-state server in %s", raw)
			}
			if got := srv.Env["AIARCH_OPERATOR_NOTE"]; got != note {
				t.Fatalf("the note did not round-trip byte-exact:\n got %q\nwant %q", got, note)
			}
			want := map[string]string{
				"AIARCH_PROJECT_ID":    "shop",
				"AIARCH_JOB_MODE":      "construct",
				"AIARCH_COMPONENT_ID":  "orders-manager",
				"AIARCH_ACTIVITY_ID":   "C-orders-manager",
				"AIARCH_TARGET_BRANCH": "activity/C-orders-manager",
				"AIARCH_STATE_ROOT":    "/home/runner/work/shop/shop",
				"AIARCH_COMMAND":       "service-construction",
			}
			for k, v := range want {
				if srv.Env[k] != v {
					t.Errorf("env %s = %q, want %q", k, srv.Env[k], v)
				}
			}
			if len(srv.Env) != len(want)+1 {
				t.Errorf("env carries %d keys, want %d (the rig envelope plus the note): %v", len(srv.Env), len(want)+1, srv.Env)
			}
			if srv.Command != env["MCP_BIN"] {
				t.Errorf("command = %q, want %q", srv.Command, env["MCP_BIN"])
			}
		})
	}
}

// TestConstructWorkflow_MCPConfigOmitsAnEmptyNote pins the no-note dispatch: no
// AIARCH_OPERATOR_NOTE key at all, so the rig reads "no operator notes" and a run
// dispatched without a note is exactly the pre-note envelope.
func TestConstructWorkflow_MCPConfigOmitsAnEmptyNote(t *testing.T) {
	requireJQ(t)
	cfg := stepNamed(t, parseSteps(t, renderedConstructWorkflow(t)), "Write the aiarch-state MCP config")
	tmp := t.TempDir()
	env := map[string]string{
		"RUNNER_TEMP": tmp, "MCP_BIN": "/bin/mcp", "AIARCH_PROJECT_ID": "shop",
		"AIARCH_COMPONENT_ID": "c", "AIARCH_ACTIVITY_ID": "C-c", "AIARCH_STATE_ROOT": "/w",
		"AIARCH_COMMAND": "service-construction", "AIARCH_OPERATOR_NOTE": "",
	}
	if out, err := runStepScript(t, cfg.run, t.TempDir(), env); err != nil {
		t.Fatalf("the MCP-config script failed with no note: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(filepath.Join(tmp, "aiarch-mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "AIARCH_OPERATOR_NOTE") {
		t.Fatalf("an empty note must not stamp AIARCH_OPERATOR_NOTE:\n%s", raw)
	}
}

// TestConstructWorkflow_ValidationRejectsHostileIds runs the validation step: a
// hostile id fails the job and nothing in it executes; the real id shapes pass.
func TestConstructWorkflow_ValidationRejectsHostileIds(t *testing.T) {
	requireJQ(t)
	steps := parseSteps(t, renderedConstructWorkflow(t))
	validate := steps[0]
	good := map[string]string{"ACTIVITY_ID": "U-SPA-web-client", "COMPONENT_ID": "web-client", "COMMAND": "frontend-construction"}
	envFor := func(activity, component, command string) map[string]string {
		env := map[string]string{}
		for k, v := range validate.env {
			switch v {
			case "${{ inputs.activity_id }}":
				env[k] = activity
			case "${{ inputs.component_id }}":
				env[k] = component
			case "${{ inputs.command }}":
				env[k] = command
			}
		}
		return env
	}
	work := t.TempDir()
	for _, ok := range []struct{ a, c, cmd string }{
		{good["ACTIVITY_ID"], good["COMPONENT_ID"], good["COMMAND"]},
		{"C-PE", "project-state-access", "service-construction"},
		{"N-STP", "system-test-plan", "testing-plan-requirements"},
		{"C-billing.v2:x/y", "a_b", "service-detailed-design"},
	} {
		if out, err := runStepScript(t, validate.run, work, envFor(ok.a, ok.c, ok.cmd)); err != nil {
			t.Errorf("valid inputs (%q, %q, %q) were rejected: %v\n%s", ok.a, ok.c, ok.cmd, err, out)
		}
	}
	hostile := []string{
		"", "C-1; touch PWNED_SEMI", "$(touch PWNED_ID)", "`touch PWNED_TICK`", "a b",
		"-rf", "C-1\ntouch PWNED_NL", "${{ secrets.X }}", `C"1`, strings.Repeat("a", 129),
	}
	for _, h := range hostile {
		for _, which := range []string{"activity", "component", "command"} {
			a, c, cmd := good["ACTIVITY_ID"], good["COMPONENT_ID"], good["COMMAND"]
			switch which {
			case "activity":
				a = h
			case "component":
				c = h
			case "command":
				cmd = h
			}
			if _, err := runStepScript(t, validate.run, work, envFor(a, c, cmd)); err == nil {
				t.Errorf("a hostile %s %q passed validation", which, h)
			}
		}
	}
	for _, bad := range []string{"Service-Construction", "service_construction", "-x"} {
		if _, err := runStepScript(t, validate.run, work, envFor(good["ACTIVITY_ID"], good["COMPONENT_ID"], bad)); err == nil {
			t.Errorf("command %q is not a slug and must be rejected", bad)
		}
	}
	assertNothingExecuted(t, work)
}

// TestDumpRenderedWorkflowsForLint writes the rendered workflows to
// $METHODASSETS_RENDER_DIR so CI can run actionlint (and shellcheck through it) over
// exactly what the scaffold seats. It does nothing without the env var.
func TestDumpRenderedWorkflowsForLint(t *testing.T) {
	dir := os.Getenv("METHODASSETS_RENDER_DIR")
	if dir == "" {
		t.Skip("set METHODASSETS_RENDER_DIR to dump the rendered workflows for actionlint")
	}
	files, err := ScaffoldFiles(ScaffoldData{
		ModulePath: "github.com/acme/shop", AppSlug: "aiarch-app", ProjectID: "shop", Owner: "acme", Name: "shop",
		StateMcpModulePath:    "github.com/mixofreality-studio/archistrator/server/cmd/aiarch-state-mcp",
		StateMcpModuleVersion: "0123456789abcdef0123456789abcdef01234567",
	})
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, ".github", "workflows")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for p, b := range files {
		if strings.HasPrefix(p, ".github/workflows/") {
			if err := os.WriteFile(filepath.Join(dir, p), b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}
