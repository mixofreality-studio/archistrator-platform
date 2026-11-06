package methodassets

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
		"go.mod", "go.sum", ArchGatePath, "Makefile", "Makefile.scenarios.mk", ".gitattributes", ".gitignore",
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
		"github.com/mixofreality-studio/archistrator-platform/framework-go " + FrameworkGoVersion,
	} {
		if !strings.Contains(gomod, want) {
			t.Errorf("go.mod missing %q", want)
		}
	}
	for _, tool := range ScaffoldTools() {
		if !strings.Contains(gomod, tool) {
			t.Errorf("go.mod does not make %q runnable as `go tool`", tool)
		}
	}
}

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
	if !strings.Contains(gen, "steps.spec.outputs.integration == 'true'") || !strings.Contains(gen, "make gen-tests") ||
		!strings.Contains(gen, `GOWORK: "off"`) {
		t.Errorf("the integration task (from the spec step, not the optional phase input) must generate the scenario tests (make gen-tests, GOWORK off):\n%s", gen)
	}
	if strings.Index(y, "name: Generate the scenario tests") > agent {
		t.Error("the scenario tests must be generated BEFORE the agent step, which fills their hooks")
	}

	generators := step("Run the task's generators")
	if !strings.Contains(generators, ".generate") || !strings.Contains(generators, `GOWORK: "off"`) {
		t.Errorf("the job must run the generators the task spec names:\n%s", generators)
	}
	if strings.Index(y, "name: Run the task's generators") > agent {
		t.Error("the task's generators must run BEFORE the agent step, which commits their output")
	}
	if !strings.Contains(step("Keep check output out of commits"), ".temporal/") {
		t.Error("the scenario host's database must be kept out of commits")
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
		"actions/upload-artifact@v4", "name: check-results-${{ inputs.activity_id }}-${{ github.run_id }}-${{ github.run_attempt }}",
		"check-results/**", "test-results/**", "always()",
	} {
		if !strings.Contains(upload, s) {
			t.Errorf("Upload check results must carry %q:\n%s", s, upload)
		}
	}
	// The recorded run and the uploaded tree must agree on one artifact name.
	record := step("Record the check run")
	if !strings.Contains(record, `--artifact "check-results-${AIARCH_ACTIVITY_ID}-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}"`) ||
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
	for _, want := range []string{"make gen-tests-check", "make gen-models-check", "go mod tidy -diff"} {
		if !strings.Contains(g, want) {
			t.Errorf("go-checks must run %q:\n%s", want, g)
		}
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
	for _, want := range []string{
		"gen-models:", "gen-models-check:", "gen-null:", "gen-tests:", "gen-tests-check:", "test-scenarios:",
		"go tool modelgen", "go tool nullgen -project .aiarch/state/project.json -root .", "go tool testgen",
		// testgen's default resolves one level above -root; every call must spell it.
		"TESTGEN_ARGS = -project .aiarch/state/project.json -root . -uitests uitests/generated",
		"-run 'TestScenario_'", "test-results/go-test.jsonl",
		// the local run contract is an alias of the generated launcher, never logic of its own (FIX-049)
		"run-local:\n\tGOWORK=off go run ./cmd/devstack\n",
	} {
		if !strings.Contains(m, want) {
			t.Errorf("Makefile.scenarios.mk missing %q", want)
		}
	}
	if strings.Contains(m, "../") || strings.Contains(m, "server/") {
		t.Errorf("Makefile.scenarios.mk must be module-root-relative:\n%s", m)
	}
	if !strings.Contains(string(out["Makefile"]), "include Makefile.scenarios.mk") {
		t.Errorf("the root Makefile must include the fragment:\n%s", out["Makefile"])
	}
}

// TestScaffoldNeverRunsAGeneratorByVersion: `go run pkg@version` is refused for a module
// with replace directives, which the published generators have. A generator reached that
// way is dead on arrival in every project.
func TestScaffoldNeverRunsAGeneratorByVersion(t *testing.T) {
	out, err := ScaffoldFiles(testData)
	if err != nil {
		t.Fatal(err)
	}
	banned := regexp.MustCompile(`go run [^\s]*@|go install [^\s]*(testgen|modelgen|webgen|appgen)`)
	for p, b := range out {
		if strings.HasPrefix(p, ".claude/") {
			continue
		}
		if m := banned.Find(b); m != nil {
			t.Errorf("%s runs a generator by version: %q", p, m)
		}
	}
}

// TestScaffoldGateDerivesItsPrefix: the seated gate must not name a prefix. Handing
// MethodSpec one is how every check silently stopped applying to built apps.
func TestScaffoldGateDerivesItsPrefix(t *testing.T) {
	out, err := ScaffoldFiles(testData)
	if err != nil {
		t.Fatal(err)
	}
	gate := string(out[ArchGatePath])
	for _, want := range []string{"package internal_test", "func TestFileLayout", "func TestGeneratedOnlyPublic", "func TestMethodLayering", "func TestMethod(", "arch.MethodSpec(moduleRoot)", "arch.CheckModuleFile"} {
		if !strings.Contains(gate, want) {
			t.Errorf("the seated gate is missing %q", want)
		}
	}
	if got := strings.Count(gate, "arch.MethodSpec(moduleRoot)"); got != 4 {
		t.Errorf("every one of the gate's checks must judge the module's derived spec, %d do:\n%s", got, gate)
	}
	if _, stale := out["aiarch_method_test.go"]; stale {
		t.Error("the root method test is retired: the gate lives under internal/")
	}
}

// TestScaffoldModuleIsTidy holds the release to the invariant every project is born under:
// main passes `go mod tidy -diff`. It renders the seated module and asks the go command, so
// a pin bumped without running cmd/gen-scaffold-mod fails here. It resolves the pins
// through GOPROXY.
func TestScaffoldModuleIsTidy(t *testing.T) {
	out, err := ScaffoldFiles(testData)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, p := range []string{"go.mod", "go.sum", ArchGatePath, DepsPinPath} {
		dest := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, out[p], 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "mod", "tidy", "-diff")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the seated module is not tidy; run go run ./cmd/gen-scaffold-mod: %v\n%s", err, b)
	}
	for _, line := range strings.Split(string(out["go.mod"]), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "replace") || strings.HasPrefix(line, "toolchain") {
			t.Errorf("the seated go.mod must not carry %q", line)
		}
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
		if p == manifestPath || strings.HasPrefix(p, ".claude/") {
			continue
		}
		if _, ok := renderedPaths[p]; !ok {
			t.Errorf("unexpected key in ScaffoldFiles output: %q", p)
		}
	}
}
