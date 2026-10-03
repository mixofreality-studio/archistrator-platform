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
// component testing (spec §6.2): on the integration phase the construct
// workflow runs the scenario tests, uploads ONE results tree as a GitHub
// artifact and records the run through the aiarch-state MCP binary; go-checks
// runs the testgen drift check on every PR (spec §5.3). Both testgen
// invocations spell the module-root -uitests dir.
//
// Artifact layout (pinned here because two consumers were written against it):
// actions/upload-artifact@v4 roots the zip at the least common ancestor of its
// SEARCH PATHS, so the single `test-results/**` yields entries `go-test.jsonl`
// and `<component>/results.json` — the layout the local venue (spec §6.3,
// .aiarch/test-results/<run>/<component>/results.json) and Plan 2's cloud
// TestRun reader (`OpenTestRunFile(ctx, ref, "<component>/results.json")`,
// TestRunPanel `fileUrl('go-test.jsonl')`) expect. The generated Playwright
// config writes to uitests/test-results/<component> (emit_playwright.go,
// componentDirDepth=2), so the scenario step MIRRORS that directory into
// test-results/<component>/ after the run: a second upload path would re-root
// the zip at the checkout (test-results/go-test.jsonl, uitests/test-results/…)
// for Go-only components too, and `record-test-run --results test-results`
// would find no results.json for a UI client. videoRef/traceRef inside
// results.json are relative to the component dir, so the copy keeps them valid.
func TestConstructTemplateHasScenarioSteps(t *testing.T) {
	out, err := ScaffoldFiles(testData)
	if err != nil {
		t.Fatal(err)
	}
	testgen := AppGeneratorModulePath + "/cmd/testgen@" + AppGeneratorVersion
	y := string(out[".github/workflows/aiarch-construct.yml"])
	for _, s := range []string{
		"name: Run scenario tests", "name: Upload test results", "actions/upload-artifact@v4",
		"name: Record test run", "record-test-run",
		testgen + " " + testgenModuleRootArgs + " -check",
	} {
		if !strings.Contains(y, s) {
			t.Errorf("aiarch-construct.yml missing %q", s)
		}
	}
	scenarios := y[strings.Index(y, "name: Run scenario tests"):]
	scenarios = scenarios[:strings.Index(scenarios, "name: Upload test results")]
	// The Playwright run is followed by the mirror into the Go tree, so the
	// artifact and the run record see test-results/<component>/results.json.
	pw := strings.Index(scenarios, "npx playwright test")
	mirror := strings.Index(scenarios, `cp -R "uitests/test-results/${AIARCH_COMPONENT_ID}" test-results/`)
	if pw < 0 || mirror < 0 || mirror < pw {
		t.Errorf("Run scenario tests must mirror uitests/test-results/<component> into test-results/ after the Playwright run:\n%s", scenarios)
	}
	// The runner's default shell is `bash -e`: a red Go scenario or a red
	// Playwright run must be captured, not abort the script, or the mirror never
	// happens and the record step finds no results.json for a UI client.
	if got := strings.Count(scenarios, "|| status=$?"); got != 2 || !strings.Contains(scenarios, `exit "${status}"`) {
		t.Errorf("Run scenario tests must capture the go test and playwright statuses and exit with them (got %d captures):\n%s", got, scenarios)
	}
	upload := y[strings.Index(y, "name: Upload test results"):]
	upload = upload[:strings.Index(upload, "name: Record test run")]
	if !strings.Contains(upload, "path: test-results/**") {
		t.Errorf("Upload test results must upload the single test-results/** tree:\n%s", upload)
	}
	if strings.Contains(upload, "uitests/test-results") {
		t.Errorf("Upload test results must not add a second search path (it re-roots the zip at the checkout):\n%s", upload)
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
