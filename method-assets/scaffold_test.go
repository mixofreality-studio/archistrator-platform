package methodassets

import (
	"encoding/json"
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
		"go.mod", "aiarch_method_test.go", "internal/.gitkeep",
		".aiarch/state/project.json", ".claude/agents/system-architect.md",
	} {
		if _, ok := files[want]; !ok {
			t.Errorf("missing %s", want)
		}
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
		"go 1.25.0",
		"require github.com/mixofreality-studio/archistrator-platform/framework-go " + FrameworkGoVersion,
		"tool github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator",
	} {
		if !strings.Contains(gomod, want) {
			t.Errorf("go.mod missing %q", want)
		}
	}
	var pj struct {
		ID    string         `json:"id"`
		Phase int            `json:"phase"`
		Slots map[string]any `json:"slots"`
	}
	if err := json.Unmarshal(files[".aiarch/state/project.json"], &pj); err != nil {
		t.Fatalf("project.json invalid: %v", err)
	}
	if pj.ID != "widgets" || pj.Phase != 0 || len(pj.Slots) != 0 {
		t.Errorf("project.json seed wrong: %+v", pj)
	}
}
