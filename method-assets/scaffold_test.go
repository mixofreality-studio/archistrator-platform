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
// component testing under the agent task spec (spec §5.2, R2): the deterministic
// half of integration lives in the JOB, not the prompt. On the integration phase
// only, the job (1) generates the scenario tests BEFORE the agent (make gen-tests
// seeds the hooks file; the agent fills the hooks and commits the generated tests
// with its wiring). After the agent, on every phase, the job (2) runs the spec's
// full check set with `run-checks --gate` from a clean test-results/ (no stale
// pass from the agent's own runs leaks into the gate; testgen drift is the
// integration spec's gen-tests-check), (3) uploads the check logs and the
// scenario results tree under one artifact name, (4) records the CheckRun with
// that same name, and (5) fails the job on a red check. Neither the agent nor
// the job records a TestRun any more (CheckRun replaces it). go-checks still
// runs the testgen drift check on every PR, spelling the module-root -uitests dir.
//
// Artifact layout: actions/upload-artifact@v4 roots the zip at the least common
// ancestor of its SEARCH PATHS. With check-results/** and test-results/** that is
// the checkout, so entries are `check-results/results.json`,
// `check-results/checks/<checkId>.log` and `test-results/<component>/results.json`
// — the layout the CheckRun reader opens.
func TestConstructTemplateHasScenarioSteps(t *testing.T) {
	out, err := ScaffoldFiles(testData)
	if err != nil {
		t.Fatal(err)
	}
	testgen := AppGeneratorModulePath + "/cmd/testgen@" + AppGeneratorVersion
	y := string(out[".github/workflows/aiarch-construct.yml"])
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
	agent := strings.Index(y, "name: Run construction step (claude-code-action)")
	if agent < 0 {
		t.Fatal("aiarch-construct.yml has no agent step")
	}

	gen := step("Generate the scenario tests")
	if !strings.Contains(gen, "inputs.phase == 'integration'") || !strings.Contains(gen, "make gen-tests") ||
		!strings.Contains(gen, `GOWORK: "off"`) {
		t.Errorf("the integration phase must generate the scenario tests (make gen-tests, GOWORK off):\n%s", gen)
	}
	if strings.Index(y, "name: Generate the scenario tests") > agent {
		t.Error("the scenario tests must be generated BEFORE the agent step, which fills their hooks")
	}

	// The venue no longer hands the agent a recorder, re-runs the drift check
	// itself, or reads a scenario index: the check gate owns all three.
	for _, gone := range []string{
		"Expose the test-run recorder", "Check the generated scenario tests", "name: Upload test results",
		"Fail on a red scenario run", "AIARCH_TEST_ARTIFACT", "AIARCH_TEST_RUN_ID",
	} {
		if strings.Contains(y, gone) {
			t.Errorf("aiarch-construct.yml still carries %q", gone)
		}
	}
	for line := range strings.SplitSeq(y, "\n") {
		if l := strings.TrimSpace(line); !strings.HasPrefix(l, "#") && strings.Contains(l, "record-test-run") {
			t.Errorf("the construct workflow must not record a TestRun (CheckRun replaces it): %q", l)
		}
	}

	checks := step("Run the checks")
	if !strings.Contains(checks, "rm -rf test-results") ||
		strings.Index(checks, "rm -rf test-results") > strings.Index(checks, "run-checks --gate") {
		t.Errorf("the gate must start from a clean test-results/:\n%s", checks)
	}
	upload := step("Upload check results")
	for _, s := range []string{
		"actions/upload-artifact@v4", "name: check-results-${{ inputs.activity_id }}-${{ github.run_id }}",
		"check-results/**", "test-results/**", "always()",
	} {
		if !strings.Contains(upload, s) {
			t.Errorf("Upload check results must carry %q:\n%s", s, upload)
		}
	}
	// The recorded run and the uploaded tree must agree on one artifact name.
	record := step("Record the check run")
	if !strings.Contains(record, `--artifact "check-results-${AIARCH_ACTIVITY_ID}-${GITHUB_RUN_ID}"`) ||
		!strings.Contains(record, `--revision "${AIARCH_REVISION}"`) {
		t.Errorf("the record must name the uploaded artifact and the dispatched attempt:\n%s", record)
	}
	// The revision input is an id: validated by the first step before it reaches $GITHUB_ENV.
	validate := step("Validate dispatch inputs")
	for _, s := range []string{
		`REVISION: ${{ inputs.revision }}`, `valid "${REVISION}" "${id_re}"`,
		`printf 'AIARCH_REVISION=%s\n' "${REVISION}"`, `} >> "$GITHUB_ENV"`,
	} {
		if !strings.Contains(validate, s) {
			t.Errorf("the validate step must check and export the revision input (%q):\n%s", s, validate)
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
