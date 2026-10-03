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

// TestConstructTemplateHasScenarioSteps pins the venue half of deterministic
// component testing (spec §6.2): on the integration phase the construct
// workflow runs the scenario tests, uploads test-results/** as a GitHub
// artifact and records the run through the aiarch-state MCP binary; go-checks
// runs the testgen drift check on every PR (spec §5.3).
func TestConstructTemplateHasScenarioSteps(t *testing.T) {
	out, err := ScaffoldFiles(testData)
	if err != nil {
		t.Fatal(err)
	}
	y := string(out[".github/workflows/aiarch-construct.yml"])
	for _, s := range []string{
		"name: Run scenario tests", "name: Upload test results", "actions/upload-artifact@v4",
		"name: Record test run", "record-test-run",
		AppGeneratorModulePath + "/cmd/testgen@" + AppGeneratorVersion,
	} {
		if !strings.Contains(y, s) {
			t.Errorf("aiarch-construct.yml missing %q", s)
		}
	}
	g := string(out[".github/workflows/go-checks.yml"])
	if !strings.Contains(g, "testgen@") || !strings.Contains(g, "-check") {
		t.Error("go-checks must run the testgen drift check")
	}
	if !strings.Contains(g, AppGeneratorModulePath+"/cmd/testgen@"+AppGeneratorVersion) {
		t.Error("go-checks must pin testgen to the scaffold's app-generator version")
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
	for _, s := range []string{
		"gen-tests:", "gen-tests-check:", "test-scenarios:",
		AppGeneratorModulePath + "/cmd/testgen@" + AppGeneratorVersion,
		"-project .aiarch/state/project.json", "-root .",
		"-run 'TestScenario_'", "test-results/go-test.jsonl",
	} {
		if !strings.Contains(m, s) {
			t.Errorf("Makefile.scenarios.mk missing %q", s)
		}
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
