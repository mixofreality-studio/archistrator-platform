package methodcheck

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/arch"
)

// check_test.go exercises the Check entry point end-to-end: it materializes a repo
// root carrying .aiarch/state/project.json and runs Check with a real arch.Spec for
// the alignment walk, asserting the clean fixture passes (no t.Errorf) and the
// missing/empty paths behave. The deliberate-FAILURE alignment path is asserted
// against the underlying functions (alignSystemToCode + loadClassifiedPackages) so a
// real *testing.T failure is observed without sabotaging the suite.

// writeRepoState materializes repoRoot/.aiarch/state/project.json from a testdata
// fixture and returns repoRoot.
func writeRepoState(t *testing.T, fixture string) string {
	t.Helper()
	repoRoot := t.TempDir()
	dir := filepath.Join(repoRoot, ".aiarch", "state")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "project.json"), readFixture(t, fixture), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	return repoRoot
}

func TestCheck_CleanFixtureNoAlignmentPasses(t *testing.T) {
	repoRoot := writeRepoState(t, "project_clean.json")
	// No Arch patterns → alignment skipped; only the design rules run, all clean.
	Check(t, ProjectSpec{RepoRoot: repoRoot})
}

func TestCheck_CleanFixtureWithMatchingCodePasses(t *testing.T) {
	// GOWORK=off: the testdata alignment module is nested under testdata/ and not in
	// the repo go.work; a real consuming module loads its own in-workspace packages.
	t.Setenv("GOWORK", "off")
	repoRoot := writeRepoState(t, "project_clean.json")
	// Alignment against the testdata module whose packages match the design components.
	Check(t, ProjectSpec{RepoRoot: repoRoot, Arch: alignAppArchSpec()})
}

// TestCheck_DesignOnlyNoCodeSkipsLayerAndAlignment proves the pure-design-phase
// posture: a committed clean design + an Arch spec whose load patterns match NO Go
// packages must NOT invoke the arch layer rules nor the alignment pass — the empty
// package set IS the design phase, and arch.Check (which t.Fatalf's on zero packages)
// must never be reached. The design rules alone run and the clean fixture passes.
func TestCheck_DesignOnlyNoCodeSkipsLayerAndAlignment(t *testing.T) {
	t.Setenv("GOWORK", "off")
	repoRoot := writeRepoState(t, "project_clean.json")
	// The designphase fixture module has a go.mod + an internal/ tree but NO Go code,
	// so the load yields zero classified packages WITHOUT error → design phase → layer
	// + alignment skipped, no Fatalf. If Check wrongly called arch.Check on the empty
	// set it would Fatalf here (arch.Check t.Fatalf's on zero packages) and fail.
	noCodeSpec := arch.Spec{
		ModuleRoot:   "testdata/designphase",
		ModulePrefix: "example.com/designphase/internal/",
		Patterns:     []string{"./internal/..."},
		Layers: []arch.Layer{
			{Name: "Manager", DirPrefix: "manager"},
		},
	}
	Check(t, ProjectSpec{RepoRoot: repoRoot, Arch: noCodeSpec})
}

// TestCheck_WithCodeRunsAllThree proves that when the module HAS Go code, ONE Check
// call drives all three passes — design rules + arch LAYER rules + alignment — and
// the clean fixture passes them all. The arch layer pass is genuinely exercised here
// (arch.Check re-loads and runs the structural suite over the alignapp module): if
// arch.Check were not wired in, a spec that DOES match packages but is otherwise
// clean would still pass, so this is paired with TestCheck_DesignOnlyNoCodeSkips...
// (no-code → arch.Check skipped, no Fatalf) to pin both branches. The arch RULES
// firing on a violating module is covered exhaustively by the arch package's own
// tests; here we assert the wiring (invoked-and-clean with code, skipped without).
func TestCheck_WithCodeRunsAllThree(t *testing.T) {
	t.Setenv("GOWORK", "off")
	repoRoot := writeRepoState(t, "project_clean.json")
	// The full, correct alignapp spec: design rules + arch.Check + alignment all run
	// and all pass. (Same module arch's own TestZZ-style check passes against.)
	Check(t, ProjectSpec{RepoRoot: repoRoot, Arch: alignAppArchSpec()})
}

func TestCheck_MissingStateIsCleanPass(t *testing.T) {
	// A repo root with no .aiarch tree must NOT fail.
	repoRoot := t.TempDir()
	Check(t, ProjectSpec{RepoRoot: repoRoot})
}

func TestCheck_EmptyStateIsCleanPass(t *testing.T) {
	repoRoot := t.TempDir()
	dir := filepath.Join(repoRoot, ".aiarch", "state")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "project.json"), nil, 0o644); err != nil {
		t.Fatalf("write empty state: %v", err)
	}
	Check(t, ProjectSpec{RepoRoot: repoRoot})
}

// TestCheck_AlignmentSurfacesMissingPackage proves the alignment pass Check runs WILL
// produce a merge-blocking finding when a design component has no matching package.
// We assert against the same functions Check invokes, with the arch spec Check would
// use, so the wiring (decode System → loadClassifiedPackages → alignSystemToCode) is
// the same one Check drives — but the assertion observes the finding instead of a
// suite-failing t.Errorf.
func TestCheck_AlignmentSurfacesMissingPackage(t *testing.T) {
	t.Setenv("GOWORK", "off") // nested testdata module — see loadAlignPkgs.
	p, _, err := DecodeProject(readFixture(t, "project_clean.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	sys, ok, err := p.system()
	if err != nil || !ok {
		t.Fatalf("system: ok=%v err=%v", ok, err)
	}

	// An arch spec that OMITS the manager dir → designmanager classifies into no
	// layer and is dropped, so the design's DesignManager component has no package.
	badSpec := arch.Spec{
		ModuleRoot:   "testdata/alignapp",
		ModulePrefix: "example.com/alignapp/internal/",
		Patterns:     []string{"./internal/..."},
		Layers: []arch.Layer{
			{Name: "Client", DirPrefix: "client"},
			{Name: "Engine", DirPrefix: "engine"},
			{Name: "ResourceAccess", DirPrefix: "resourceaccess"},
		},
	}
	pkgs, err := loadClassifiedPackages(badSpec)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	findings := alignSystemToCode(sys, pkgs, nil)
	if !hasRuleFindings(findings, ruleAlignMissingPkg) {
		t.Fatalf("expected ALIGN-MISSING-PKG when the manager package is excluded, got %+v", findings)
	}
}
