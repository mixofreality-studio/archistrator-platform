package methodcheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/arch"
)

// pendingProject is a project with one committed activity list covering the named
// components (activity id C-<id>), plus the given execution rows.
func pendingProject(t *testing.T, ids []string, rows map[string]ActivityRow) Project {
	t.Helper()
	type activity struct {
		Name        string `json:"name"`
		ComponentID string `json:"componentId"`
	}
	acts := make([]activity, 0, len(ids))
	for _, id := range ids {
		acts = append(acts, activity{Name: "C-" + id, ComponentID: id})
	}
	model, err := json.Marshal(map[string]any{"activities": acts})
	if err != nil {
		t.Fatal(err)
	}
	return Project{
		Slots:             map[string]Slot{"9": {Status: 2, Kind: slotKindActivityList, Model: model}},
		ActivityExecution: rows,
	}
}

var done = json.RawMessage(`"2026-10-31T17:34:48Z"`)

func TestComponentProgress(t *testing.T) {
	proj := pendingProject(t, []string{"none", "running", "failed", "ok", "skip"}, map[string]ActivityRow{
		"C-running": {},
		"C-failed":  {CompletedAt: done, FailureReason: 4},
		"C-ok":      {CompletedAt: done},
		"C-skip":    {CompletedAt: done, Attempts: []TaskAttempt{{Task: "testing", Attempt: 1, Outcome: "skipped"}}},
	})
	pending, skipped, err := proj.componentProgress()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"none", "running", "failed"} {
		if !pending[id] {
			t.Errorf("%s must be pending: its code is legitimately absent", id)
		}
	}
	if pending["ok"] || skipped["ok"] {
		t.Error("a completed activity must leave its component obligated")
	}
	if !skipped["skip"] || pending["skip"] {
		t.Error("a skipped activity must mark its component skipped")
	}
	if pending["uncovered"] || skipped["uncovered"] {
		t.Error("a component no activity covers stays obligated")
	}
}

func TestMissingPackageToleranceFollowsTheActivity(t *testing.T) {
	sys := System{Components: []Component{{ID: "todo-access", Name: "TodoAccess", Kind: kindResourceAccess, Layer: layerResourceAccess, ContractKey: "todoAccess"}}}
	other := []classifiedPackage{{pkgPath: "m/internal/manager/list", leaf: "list", layer: "Manager"}}
	sysWithOther := sys
	sysWithOther.Components = append(sysWithOther.Components, Component{ID: "list", Name: "List", Kind: kindManager, Layer: layerManager})
	count := func(findings []Finding, sev Severity) int {
		n := 0
		for _, f := range findings {
			if f.RuleID == ruleAlignMissingPkg && f.Severity == sev {
				n++
			}
		}
		return n
	}
	id := map[string]bool{"todo-access": true}
	if got := alignSystemToCode(sysWithOther, other, nil, nil, id, nil); count(got, SeverityError) != 0 {
		t.Errorf("a pending component's missing package must not be an error: %v", got)
	}
	if got := alignSystemToCode(sysWithOther, other, nil, nil, nil, id); count(got, SeverityError) != 0 || count(got, SeverityWarning) != 1 {
		t.Errorf("a skipped component's missing package must be a warning: %v", got)
	}
	if got := alignSystemToCode(sysWithOther, other, nil, nil, nil, nil); count(got, SeverityError) != 1 {
		t.Errorf("a component that is neither pending nor skipped stays obligated: %v", got)
	}
}

// A pending component's package in the WRONG layer is still a design disagreement.
func TestPendingDoesNotExcuseALayerMismatch(t *testing.T) {
	sys := System{Components: []Component{{ID: "todo", Name: "Todo", Kind: kindManager, Layer: layerManager}}}
	pkgs := []classifiedPackage{{pkgPath: "m/internal/engine/todo", leaf: "todo", layer: "Engine"}}
	got := alignSystemToCode(sys, pkgs, nil, nil, map[string]bool{"todo": true}, nil)
	if len(got) != 1 || got[0].RuleID != ruleAlignLayerMismate {
		t.Fatalf("want ALIGN-LAYER-MISMATCH, got %v", got)
	}
}

func TestLoadUnclassifiedCodeSeesCodeOutsideTheLayers(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/fresh\n\ngo 1.25\n")
	write("internal/gate_test.go", "package internal_test\n")
	spec := arch.MethodSpec(dir)
	if code, err := loadUnclassifiedCode(spec); err != nil || len(code) != 0 {
		t.Fatalf("a test-only tree is not code: %v, %v", code, err)
	}
	write("internal/domain/rogue/rogue.go", "package rogue\n")
	code, err := loadUnclassifiedCode(spec)
	if err != nil || len(code) != 1 || code[0] != "example.com/fresh/internal/domain/rogue" {
		t.Fatalf("got %v, %v", code, err)
	}
}

// A generated fake is test support, not a component the design failed to declare, even when
// its parent package was matched only through a contract join.
func TestLoadClassifiedPackagesDropsGeneratedFakes(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/fresh\n\ngo 1.25\n")
	write("internal/resourceaccess/todo/contract.gen.go", "package todo\n")
	write("internal/resourceaccess/todo/fake/fake.gen.go", "package todofake\n")
	write("internal/resourceaccess/handmade/fake/fake.go", "package fake\n")
	pkgs, err := loadClassifiedPackages(arch.MethodSpec(dir))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range pkgs {
		got = append(got, p.pkgPath)
	}
	want := []string{"example.com/fresh/internal/resourceaccess/handmade/fake", "example.com/fresh/internal/resourceaccess/todo"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %v, want %v: a hand-written package named fake is still a package", got, want)
	}
}
