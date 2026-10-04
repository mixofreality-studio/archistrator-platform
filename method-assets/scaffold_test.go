package methodassets

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

var testData = ScaffoldData{
	ModulePath: "github.com/acme/widgets", AppSlug: "aiarch-app",
	ProjectID: "widgets", Owner: "acme", Name: "widgets",
	StateMcpModulePath:    "github.com/mixofreality-studio/archistrator/server/cmd/aiarch-state-mcp",
	StateMcpModuleVersion: "v0.0.0-test",
}

func TestScaffoldFiles(t *testing.T) {
	files, err := ScaffoldFiles(testData)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		".github/workflows/aiarch-design.yml", ".github/workflows/aiarch-construct.yml",
		".github/workflows/go-checks.yml", ".golangci.yml",
		"go.mod", "aiarch_method_test.go", "internal/.gitkeep", "Makefile.scenarios.mk",
		".claude/agents/system-architect.md",
	} {
		if _, ok := files[want]; !ok {
			t.Errorf("missing %s", want)
		}
	}
	// server's CreateProject owns that path.
	if _, ok := files[".aiarch/state/project.json"]; ok {
		t.Errorf("ScaffoldFiles must not seed .aiarch/state/project.json: server's CreateProject owns that path")
	}
	for p, b := range files {
		// .claude/** is ClaudeFiles()'s untouched passthrough (never fed through
		// renderAsset), and its markdown legitimately uses "[[skill-name]]"
		// wiki-link cross-references between skills — not a Go template field.
		// Scoping the unrendered-field scan to the files this task actually
		// renders (renderedPaths' destinations) avoids flagging that pre-existing,
		// unrelated content as a bug. See task-9-report.md for detail.
		if strings.HasPrefix(p, ".claude/") {
			continue
		}
		if strings.Contains(string(b), "[[") {
			t.Errorf("%s: unrendered [[ ]] template field", p)
		}
	}
	gomod := string(files["go.mod"])
	for _, want := range []string{
		"module github.com/acme/widgets",
		"go 1.26.0",
		"require github.com/mixofreality-studio/archistrator-platform/framework-go " + FrameworkGoVersion,
		"tool github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator/cmd/httpgen",
		"tool github.com/mixofreality-studio/archistrator-platform/framework-go-mcp-generator/cmd/mcpgen",
	} {
		if !strings.Contains(gomod, want) {
			t.Errorf("go.mod missing %q", want)
		}
	}
	// framework-go-app-generator ships no cmd/ main package (library-only); a
	// tool directive against it breaks `go mod tidy` in generated apps.
	if strings.Contains(gomod, "framework-go-app-generator") {
		t.Errorf("go.mod must not reference framework-go-app-generator: it ships no cmd/ main package")
	}
}

// testgenModuleRootArgs is the testgen invocation every scaffolded caller
// must use on the module-root layout. The pinned CLI defaults -uitests to
// ../uitests/generated RELATIVE TO -root (the server/ layout), so a caller that
// passes only `-root .` writes the Playwright specs, config and hooks.ts
// OUTSIDE the checkout and its -check is permanently red for any project with
// a UI client; -uitests must be spelled explicitly.
const testgenModuleRootArgs = "-project .aiarch/state/project.json -root . -uitests uitests/generated"

// TestConstructTemplateHasScenarioSteps pins the venue half of deterministic
// component testing as spec §12 B12 shapes it: the integration TASK generates the
// tests from the reviewed plan, fills their hooks, runs them and records the run
// with `record-test-run` before publishDraft (so the run rides the agent's
// publish onto the activity branch). The construct workflow, on the integration
// phase only, therefore (1) hands the agent the recorder on PATH, the run id and
// the artifact name BEFORE the agent step, (2) re-checks testgen drift after it,
// (3) uploads ONE results tree under that same artifact name, and (4) fails the
// job on a red run. It never records a run itself — a record written after the
// agent published would never leave the runner — and go-checks runs the testgen
// drift check on every PR (spec §5.3). Both testgen invocations spell the
// module-root -uitests dir.
//
// Artifact layout (pinned here because two consumers were written against it):
// actions/upload-artifact@v4 roots the zip at the least common ancestor of its
// SEARCH PATHS, so the single `test-results/**` yields entries `go-test.jsonl`
// and `<component>/results.json` — the layout the local venue (spec §6.3,
// .aiarch/test-results/<run>/<component>/results.json) and Plan 2's cloud
// TestRun reader (`OpenTestRunFile(ctx, ref, "<component>/results.json")`,
// TestRunPanel `fileUrl('go-test.jsonl')`) expect. A second upload path would
// re-root the zip at the checkout for Go-only components too; the
// frontend-integration command mirrors uitests/test-results/<component> into
// test-results/ instead (TestIntegrationCommandsRunAndRecordTheScenarios).
func TestConstructTemplateHasScenarioSteps(t *testing.T) {
	out, err := ScaffoldFiles(testData)
	if err != nil {
		t.Fatal(err)
	}
	testgen := AppGeneratorModulePath + "/cmd/testgen@" + AppGeneratorVersion
	y := string(out[".github/workflows/aiarch-construct.yml"])
	for _, s := range []string{
		"name: Expose the test-run recorder to the integration step",
		"name: Check the generated scenario tests",
		"name: Upload test results", "actions/upload-artifact@v4",
		"name: Fail on a red scenario run",
		testgen + " " + testgenModuleRootArgs + " -check",
	} {
		if !strings.Contains(y, s) {
			t.Errorf("aiarch-construct.yml missing %q", s)
		}
	}
	step := func(name string) string {
		i := strings.Index(y, "name: "+name)
		if i < 0 {
			t.Fatalf("aiarch-construct.yml has no step %q", name)
		}
		rest := y[i:]
		if j := strings.Index(rest[1:], "\n      - name: "); j >= 0 {
			return rest[:j+1]
		}
		return rest
	}
	for _, name := range []string{
		"Expose the test-run recorder to the integration step", "Check the generated scenario tests",
		"Upload test results", "Fail on a red scenario run",
	} {
		if !strings.Contains(step(name), "inputs.phase == 'integration'") {
			t.Errorf("step %q must run on the integration phase only:\n%s", name, step(name))
		}
	}
	agent := strings.Index(y, "name: Run construction step (claude-code-action)")
	if expose := strings.Index(y, "name: Expose the test-run recorder"); expose < 0 || expose > agent {
		t.Error("the recorder must be exposed BEFORE the agent step, which records the run")
	}
	for _, name := range []string{"Check the generated scenario tests", "Upload test results", "Fail on a red scenario run"} {
		if strings.Index(y, "name: "+name) < agent {
			t.Errorf("step %q must follow the agent step", name)
		}
	}
	expose := step("Expose the test-run recorder to the integration step")
	for _, s := range []string{
		`dirname "${MCP_BIN}" >> "$GITHUB_PATH"`,
		`AIARCH_TEST_RUN_ID=%s\n' "${AIARCH_RUN_ID}" >> "$GITHUB_ENV"`,
		`AIARCH_TEST_ARTIFACT=test-results-%s-%s\n' "${AIARCH_ACTIVITY_ID}" "${AIARCH_RUN_ID}" >> "$GITHUB_ENV"`,
	} {
		if !strings.Contains(expose, s) {
			t.Errorf("the expose step must write %q:\n%s", s, expose)
		}
	}
	// The recorded run and the uploaded tree must agree on one artifact name.
	upload := step("Upload test results")
	if !strings.Contains(upload, "name: test-results-${{ inputs.activity_id }}-${{ github.run_id }}") {
		t.Errorf("Upload test results must use the artifact name the expose step hands the agent:\n%s", upload)
	}
	if !strings.Contains(upload, "path: test-results/**") || !strings.Contains(upload, "if-no-files-found: error") {
		t.Errorf("Upload test results must upload the single test-results/** tree and fail on an empty one:\n%s", upload)
	}
	if strings.Contains(upload, "uitests/test-results") {
		t.Errorf("Upload test results must not add a second search path (it re-roots the zip at the checkout):\n%s", upload)
	}
	if !strings.Contains(upload, "always()") {
		t.Errorf("Upload test results must upload a run's evidence after a failed agent step too:\n%s", upload)
	}
	if red := step("Fail on a red scenario run"); !strings.Contains(red, `all(.[]; .status == "pass")`) ||
		!strings.Contains(red, `"test-results/${AIARCH_COMPONENT_ID}/results.json"`) {
		t.Errorf("a red (or skipped) scenario must fail the job:\n%s", red)
	}
	for line := range strings.SplitSeq(y, "\n") {
		if l := strings.TrimSpace(line); !strings.HasPrefix(l, "#") && strings.Contains(l, "record-test-run") {
			t.Errorf("the construct workflow must not record a run itself (the integration agent records it before publishDraft): %q", l)
		}
	}
	g := string(out[".github/workflows/go-checks.yml"])
	if !strings.Contains(g, testgen+" "+testgenModuleRootArgs+" -check") {
		t.Errorf("go-checks must run the pinned testgen drift check with module-root paths:\n%s", g)
	}
}

// TestScaffoldMakefileScenariosInclude pins the make targets the construction
// commands and the local venue call (spec §4.3): gen-tests, gen-tests-check and
// test-scenarios, each against the pinned testgen and module-root-relative
// paths (the seated app module lives at the repo root; TestLayoutNeutral).
func TestScaffoldMakefileScenariosInclude(t *testing.T) {
	out, err := ScaffoldFiles(testData)
	if err != nil {
		t.Fatal(err)
	}
	mk, ok := out["Makefile.scenarios.mk"]
	if !ok {
		t.Fatal("missing Makefile.scenarios.mk")
	}
	m := string(mk)
	testgen := AppGeneratorModulePath + "/cmd/testgen@" + AppGeneratorVersion
	for _, s := range []string{
		"gen-tests:", "gen-tests-check:", "test-scenarios:",
		testgen + " " + testgenModuleRootArgs + "\n",
		testgen + " " + testgenModuleRootArgs + " -check\n",
		"-run 'TestScenario_'", "test-results/go-test.jsonl",
	} {
		if !strings.Contains(m, s) {
			t.Errorf("Makefile.scenarios.mk missing %q", s)
		}
	}
	// Every testgen call (write and check) spells -uitests: a bare -root . would
	// default it to ../uitests/generated and write outside the checkout.
	if got := strings.Count(m, "cmd/testgen@"); got != strings.Count(m, "-uitests uitests/generated") {
		t.Errorf("every testgen invocation must pass -uitests uitests/generated (%d invocations):\n%s", got, m)
	}
	if strings.Contains(m, "../") || strings.Contains(m, "server/") {
		t.Errorf("Makefile.scenarios.mk must be module-root-relative:\n%s", m)
	}
}

// The server's SyncManagedScaffold fast-paths "already at version X" off a
// single manifest read instead of ~100 per-file compares (Task B4). Pin the
// manifest ScaffoldFiles emits: same {version, files[] sorted} shape the
// materializer writes, files scoped to the .claude/** keys the output
// carries (the manifest is not self-listed), and no other new key leaks in.
func TestScaffoldFilesIncludesManifest(t *testing.T) {
	files, err := ScaffoldFiles(testData)
	if err != nil {
		t.Fatal(err)
	}

	raw, ok := files[manifestPath]
	if !ok {
		t.Fatalf("missing %s", manifestPath)
	}
	var m manifest
	if uerr := json.Unmarshal(raw, &m); uerr != nil {
		t.Fatalf("manifest is not valid JSON: %v", uerr)
	}

	if m.Version != Version() {
		t.Errorf("manifest version = %q, want Version() = %q", m.Version, Version())
	}

	var wantFiles []string
	for p := range files {
		if p == manifestPath {
			continue
		}
		if strings.HasPrefix(p, ".claude/") {
			wantFiles = append(wantFiles, p)
		}
	}
	sort.Strings(wantFiles)
	got := append([]string(nil), m.Files...)
	sort.Strings(got)
	if len(got) != len(wantFiles) {
		t.Fatalf("manifest files = %v, want %v", got, wantFiles)
	}
	for i := range got {
		if got[i] != wantFiles[i] {
			t.Errorf("manifest files[%d] = %q, want %q", i, got[i], wantFiles[i])
		}
	}

	// No other new key appeared: every output key is either a known rendered
	// destination, the .claude/** passthrough, or the manifest itself.
	for p := range files {
		if p == manifestPath || p == "internal/.gitkeep" || strings.HasPrefix(p, ".claude/") {
			continue
		}
		if _, ok := renderedPaths[p]; !ok {
			t.Errorf("unexpected key in ScaffoldFiles output: %q", p)
		}
	}
}
