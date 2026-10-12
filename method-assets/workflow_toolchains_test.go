package methodassets

// workflow_toolchains_test.go pins ruling R51: the construct job provisions the
// toolchains its task spec's checks exec, decided from spec.json, before the
// agent runs. The planning step runs under bash against sample specs; the
// install steps are pinned by shape.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// toolchainProject is a project.json with one Go contract and one web contract.
const toolchainProject = `{"serviceContracts":{"ordersManager":{"goPackage":"internal/manager/orders"},"webClient":{}}}`

type toolchainFixture struct {
	spec    string
	files   map[string]string // repo-relative path -> content ("" for a dir)
	project string            // "" = no .aiarch/state/project.json
}

func runToolchainPlan(t *testing.T, fx toolchainFixture) (map[string]string, string, error) {
	t.Helper()
	plan := stepNamed(t, parseSteps(t, renderedConstructWorkflow(t)), "Plan the check toolchains")
	work, tmp := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "spec.json"), []byte(fx.spec), 0o644); err != nil {
		t.Fatal(err)
	}
	if fx.project != "" {
		fx.files[".aiarch/state/project.json"] = fx.project
	}
	for p, body := range fx.files {
		full := filepath.Join(work, p)
		if strings.HasSuffix(p, "/") {
			if err := os.MkdirAll(full, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	outFile := filepath.Join(tmp, "output")
	log, err := runStepScript(t, plan.run, work, map[string]string{"RUNNER_TEMP": tmp, "GITHUB_OUTPUT": outFile})
	assertNothingExecuted(t, work, tmp)
	return readOutputs(t, outFile), log, err
}

// Check sources as TaskSpecFor builds them (projectstateaccess.go).
const (
	goChecks = `{"id":"command:build","kind":"command","source":{"argv":["go","build","./internal/manager/orders/..."]}},
		{"id":"command:gofmt","kind":"command","source":{"argv":["sh","-c","test -z \"$(gofmt -l x)\""]}},
		{"id":"command:gen-tests-check","kind":"command","source":{"argv":["make","gen-tests-check"]}},
		{"id":"rule:project","kind":"rule","source":{}}`
	lintCheck   = `{"id":"command:lint","kind":"command","source":{"argv":["golangci-lint","run","./internal/manager/orders/..."]}}`
	goScenario  = `{"id":"scenario:ordersManager:s1","kind":"scenario","source":{"scenario":"s1","contract":"ordersManager"}}`
	webScenario = `{"id":"scenario:webClient:s1","kind":"scenario","source":{"scenario":"s1","contract":"webClient"}}`
	npmChecks   = `{"id":"command:typecheck","kind":"command","source":{"argv":["npm","run","typecheck"],"dir":"webApp"}},
		{"id":"command:lint","kind":"command","source":{"argv":["npx","eslint","src"],"dir":"webApp"}}`
)

func specWith(checks ...string) string {
	return `{"model":{"id":"opus"},"tools":{"builtin":[]},"checks":[` + strings.Join(checks, ",") + `],"hash":"` + gateHash + `"}`
}

func TestToolchainPlan_ProvisionsWhatTheChecksExec(t *testing.T) {
	requireJQ(t)
	webApp := map[string]string{"webApp/package.json": `{"name":"web"}`}
	for name, c := range map[string]struct {
		fx   toolchainFixture
		want map[string]string
	}{
		"go only provisions nothing extra": {
			toolchainFixture{specWith(goChecks, goScenario), map[string]string{}, toolchainProject},
			map[string]string{"node": "false", "npm_dirs": "", "golangci": "false", "playwright": "false"},
		},
		"no checks provisions nothing": {
			toolchainFixture{`{"checks":[]}`, map[string]string{}, ""},
			map[string]string{"node": "false", "npm_dirs": "", "golangci": "false", "playwright": "false"},
		},
		"golangci-lint": {
			toolchainFixture{specWith(goChecks, lintCheck), map[string]string{}, toolchainProject},
			map[string]string{"node": "false", "npm_dirs": "", "golangci": "true", "playwright": "false"},
		},
		"npm in the check dir, node from .nvmrc": {
			toolchainFixture{specWith(npmChecks), map[string]string{"webApp/package.json": `{"engines":{"node":">=18"}}`, "webApp/.nvmrc": "20.11.1\n"}, ""},
			map[string]string{"node": "true", "npm_dirs": "webApp", "node_version": "20.11.1", "golangci": "false", "playwright": "false"},
		},
		"npm, node from engines": {
			toolchainFixture{specWith(npmChecks), map[string]string{"webApp/package.json": `{"engines":{"node":">=22"}}`}, ""},
			map[string]string{"node": "true", "npm_dirs": "webApp", "node_version": ">=22"},
		},
		"npm, node defaults to LTS": {
			toolchainFixture{specWith(npmChecks), webApp, ""},
			map[string]string{"node": "true", "npm_dirs": "webApp", "node_version": "lts/*"},
		},
		"npx alone": {
			toolchainFixture{specWith(`{"id":"command:lint","kind":"command","source":{"argv":["npx","eslint","src"],"dir":"webApp"}}`), webApp, ""},
			map[string]string{"node": "true", "npm_dirs": "webApp"},
		},
		"npm at the repo root": {
			toolchainFixture{specWith(`{"id":"command:t","kind":"command","source":{"argv":["npm","test"]}}`), map[string]string{"package.json": `{}`}, ""},
			map[string]string{"node": "true", "npm_dirs": ".", "node_version": "lts/*"},
		},
		"web scenario adds uitests and playwright": {
			toolchainFixture{specWith(npmChecks, goScenario, webScenario), map[string]string{"webApp/package.json": `{}`, "uitests/package.json": `{}`}, toolchainProject},
			map[string]string{"node": "true", "npm_dirs": "uitests webApp", "golangci": "false", "playwright": "true"},
		},
		"web scenario on a contract the project does not record": {
			toolchainFixture{specWith(`{"id":"scenario:spa:s1","kind":"scenario","source":{"scenario":"s1","contract":"spa"}}`), map[string]string{"uitests/": ""}, `{}`},
			map[string]string{"node": "true", "npm_dirs": "uitests", "playwright": "true"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			outs, log, err := runToolchainPlan(t, c.fx)
			if err != nil {
				t.Fatalf("the plan step failed: %v\n%s", err, log)
			}
			for k, v := range c.want {
				if got, ok := outs[k]; !ok || got != v {
					t.Errorf("%s = %q (present %v), want %q; outputs %v", k, got, ok, v, outs)
				}
			}
		})
	}
}

func TestToolchainPlan_RefusesDirsAndVersionsThatAreNotPlain(t *testing.T) {
	requireJQ(t)
	npmIn := func(dir string) string {
		return specWith(`{"id":"command:t","kind":"command","source":{"argv":["npm","test"],"dir":"` + dir + `"}}`)
	}
	for name, fx := range map[string]toolchainFixture{
		"parent dir":              {npmIn("../elsewhere"), map[string]string{}, ""},
		"dot-dot (exists)":        {npmIn(".."), map[string]string{}, ""},
		"trailing dot-dot":        {npmIn("webApp/.."), map[string]string{"webApp/": ""}, ""},
		"inner parent":            {npmIn("webApp/../../x"), map[string]string{"webApp/": ""}, ""},
		"absolute dir":            {npmIn("/etc"), map[string]string{}, ""},
		"missing dir":             {npmIn("webApp"), map[string]string{}, ""},
		"spaced dir":              {npmIn("web App"), map[string]string{"web App/": ""}, ""},
		"substitution dir":        {npmIn("$(touch PWNED)"), map[string]string{}, ""},
		"glob dir":                {npmIn("*"), map[string]string{"webApp/": ""}, ""},
		"hostile .nvmrc":          {npmIn("webApp"), map[string]string{"webApp/.nvmrc": "$(touch PWNED)\n"}, ""},
		"hostile engines":         {npmIn("webApp"), map[string]string{"webApp/package.json": `{"engines":{"node":"20;touch PWNED"}}`}, ""},
		"scenario no project":     {specWith(webScenario), map[string]string{"uitests/": ""}, ""},
		"web scenario no uitests": {specWith(webScenario), map[string]string{}, toolchainProject},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			outs, log, err := runToolchainPlan(t, fx)
			if err == nil {
				t.Fatalf("the plan step accepted a %s:\n%s", name, log)
			}
			if !strings.Contains(log, "::error::") {
				t.Errorf("the refusal must be a clear ::error:: (not a script fault):\n%s", log)
			}
			if _, ok := outs["npm_dirs"]; ok {
				t.Errorf("a refused plan must write no outputs: %v", outs)
			}
		})
	}
}

// TestConstructWorkflow_ProvisionsBeforeTheAgent pins the install steps: each
// is gated on the plan's output, runs before the agent (so run_checks finds the
// tools too), takes every value from env or the plan's outputs, and pins
// golangci-lint to v2.13.2 by a checksum held in the workflow.
func TestConstructWorkflow_ProvisionsBeforeTheAgent(t *testing.T) {
	doc := renderedConstructWorkflow(t)
	agent := strings.Index(doc, "uses: anthropics/claude-code-action@v1")
	at := strings.Index(doc, "name: Resolve the task spec")
	for _, step := range []string{
		"name: Plan the check toolchains",
		"name: Set up Node (for the spec's npm checks)",
		"name: Install the check dirs' npm dependencies",
		"name: Install golangci-lint (for the spec's lint check)",
		"name: Install the Playwright browser (for the spec's web scenario checks)",
	} {
		i := strings.Index(doc, step)
		if i < at || i > agent {
			t.Errorf("%q must sit after the spec step and before the agent", step)
			continue
		}
		at = i
	}
	steps := parseSteps(t, doc)
	for name, want := range map[string][]string{
		"Set up Node (for the spec's npm checks)": {
			"if: ${{ steps.tools.outputs.node == 'true' }}", "uses: actions/setup-node@v4",
			"node-version: ${{ steps.tools.outputs.node_version }}",
		},
		"Install the check dirs' npm dependencies": {
			"if: ${{ steps.tools.outputs.node == 'true' }}", "NPM_DIRS: ${{ steps.tools.outputs.npm_dirs }}", "npm ci",
		},
		"Install golangci-lint (for the spec's lint check)": {
			"if: ${{ steps.tools.outputs.golangci == 'true' }}", `GOLANGCI_LINT_VERSION: "2.13.2"`,
			`GOLANGCI_LINT_SHA256: "2277d43b98ec0054280f2ac26b53268bae97682444678a59a657dd565da021d6"`,
			`| sha256sum -c -`, `>> "$GITHUB_PATH"`,
		},
		"Install the Playwright browser (for the spec's web scenario checks)": {
			"if: ${{ steps.tools.outputs.playwright == 'true' }}", "working-directory: uitests",
			"run: npx playwright install --with-deps chromium",
		},
	} {
		stepNamed(t, steps, name)
		i := strings.Index(doc, "- name: "+name)
		end := strings.Index(doc[i+1:], "- name: ")
		body := doc[i : i+1+end]
		for _, w := range want {
			if !strings.Contains(body, w) {
				t.Errorf("%q lacks %q", name, w)
			}
		}
	}
}
