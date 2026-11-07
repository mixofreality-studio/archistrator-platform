package arch

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// filelayout_test.go exercises the file-layout gate against the testdata
// layoutapp module: a clean Manager package and a clean Engine package produce
// zero violations, while badmgr/badeng between them exercise the violation
// rules - file-not-allowed, workflow-file-multiple-funcs, workflow-file-name,
// scenario-tests-only, test-package-not-external, ARCH-TEST-COMPOSE (a hooks
// file), hand-activity-registration, workflow-func-outside-manager. The
// composeapp module exercises ARCH-TEST-COMPOSE's in-file-fake half.
//
// The pure core (fileLayoutViolations) is tested directly so violations are
// OBSERVED rather than routed to a failing t.Errorf, matching the
// gensurface_test.go pattern.

const layoutPrefix = "example.com/layoutapp/internal/"

func layoutSpec() Spec {
	return MethodSpec("testdata/layoutapp")
}

// loadLayoutPkgs loads the testdata module (a nested module → GOWORK=off) with
// the mode fileLayoutViolations needs.
func loadLayoutPkgs(t *testing.T) []*packages.Package {
	t.Helper()
	t.Setenv("GOWORK", "off")
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax,
		Dir:   "testdata/layoutapp",
		Tests: false,
	}
	pkgs, err := packages.Load(cfg, "./internal/...")
	if err != nil {
		t.Fatalf("load layoutapp: %v", err)
	}
	if n := packages.PrintErrors(pkgs); n > 0 {
		t.Fatalf("%d load error(s) in layoutapp", n)
	}
	if len(pkgs) == 0 {
		t.Fatal("layoutapp loaded zero packages; fixture missing")
	}
	return pkgs
}

func hasLayoutViolation(vs []fileLayoutViolation, file, rule string) bool {
	for _, v := range vs {
		if v.File == file && v.Rule == rule {
			return true
		}
	}
	return false
}

// TestCheckFileLayoutPassesClean drives the public entry point on the passing
// path: restricting Patterns to only the clean fixture packages (goodmgr,
// goodeng) must produce zero t.Errorf calls.
func TestCheckFileLayoutPassesClean(t *testing.T) {
	t.Setenv("GOWORK", "off")
	spec := layoutSpec()
	spec.Patterns = []string{"./internal/manager/goodmgr/...", "./internal/engine/goodeng/..."}
	CheckFileLayout(t, spec)
}

func TestFileLayoutViolations(t *testing.T) {
	pkgs := loadLayoutPkgs(t)
	spec := layoutSpec()
	vs := fileLayoutViolations(pkgs, spec)
	for _, want := range []struct{ file, rule string }{
		{"helpers.go", "file-not-allowed"},
		{"workflow.go", "workflow-file-multiple-funcs"},
		{"workflow.go", "workflow-file-name"},
		{"badmgr_test.go", "scenario-tests-only"},
		{"manager_test.go", "scenario-tests-only"},
		{"manager_hooks_test.go", "ARCH-TEST-COMPOSE"},
		{"engine_hooks_test.go", "ARCH-TEST-COMPOSE"},
		{"regcall.go", "hand-activity-registration"},
		{"pure.go", "workflow-func-outside-manager"},
		{"orphanhelper.go", "file-not-allowed"},
	} {
		if !hasLayoutViolation(vs, want.file, want.rule) {
			t.Errorf("missing violation %s in %s: got %+v", want.rule, want.file, vs)
		}
	}
	for _, v := range vs {
		if strings.Contains(v.Pkg, "goodmgr") || strings.Contains(v.Pkg, "goodeng") || strings.Contains(v.Pkg, "collabeng") {
			t.Errorf("clean package flagged: %+v", v)
		}
	}
}

// TestFileLayoutScenarioTestsOnlyPositive pins the positive path of the
// scenario-tests-only rule in isolation: goodmgr carries exactly the generated
// scenarios file, black-box, and yields ZERO violations of any rule.
func TestFileLayoutScenarioTestsOnlyPositive(t *testing.T) {
	var good []*packages.Package
	for _, p := range loadLayoutPkgs(t) {
		if strings.HasSuffix(p.PkgPath, "/manager/goodmgr") {
			good = append(good, p)
		}
	}
	if len(good) != 1 {
		t.Fatalf("expected exactly one goodmgr package, got %d", len(good))
	}
	if vs := fileLayoutViolations(good, layoutSpec()); len(vs) != 0 {
		t.Errorf("goodmgr must be clean, got %+v", vs)
	}
}

// TestFileLayout_HooksFileIsViolation pins the removal of the agent-owned hooks
// file: a <stereotype>_hooks_test.go is no longer part of the allowed set, so
// each one is reported under ARCH-TEST-COMPOSE (an agent never writes test
// code), whatever it imports and whichever package it declares.
func TestFileLayout_HooksFileIsViolation(t *testing.T) {
	vs := fileLayoutViolations(loadLayoutPkgs(t), layoutSpec())
	for _, want := range []struct{ pkg, file string }{
		{layoutPrefix + "manager/badmgr", "manager_hooks_test.go"},
		{layoutPrefix + "engine/badeng", "engine_hooks_test.go"},
	} {
		found := false
		for _, v := range vs {
			if v.Pkg == want.pkg && v.File == want.file && v.Rule == ruleTestCompose {
				found = true
			}
			if v.File == want.file && v.Rule == "scenario-tests-only" {
				t.Errorf("a hooks file is reported as ARCH-TEST-COMPOSE, not as a stray test file: %+v", v)
			}
		}
		if !found {
			t.Errorf("missing %s for %s/%s: got %+v", ruleTestCompose, want.pkg, want.file, vs)
		}
	}
	for _, v := range vs {
		if v.Rule == "hooks-import-not-allowed" {
			t.Errorf("the hooks import allowlist is gone with the hooks file: %+v", v)
		}
	}
}

// TestFileLayout_NullFileIsAllowed pins the closed set of test files: the
// generated scenarios file and null.gen_test.go. okmgr carries exactly those
// two and is clean.
func TestFileLayout_NullFileIsAllowed(t *testing.T) {
	var prod []*packages.Package
	for _, p := range loadComposePkgs(t, "./internal/manager/okmgr") {
		if p.ID == p.PkgPath && !strings.HasSuffix(p.PkgPath, ".test") { // the production package, not its test variants
			prod = append(prod, p)
		}
	}
	if len(prod) != 1 {
		t.Fatalf("want exactly the okmgr production package, got %d", len(prod))
	}
	if vs := fileLayoutViolations(prod, composeSpec()); len(vs) != 0 {
		t.Errorf("okmgr carries exactly scenarios + null files and must be clean, got %+v", vs)
	}
}

func composeSpec() Spec { return MethodSpec("testdata/composeapp") }

// loadComposePkgs loads the composeapp fixture module's packages matching
// patterns, with test variants and the type information the in-file-fake rule
// needs.
func loadComposePkgs(t *testing.T, patterns ...string) []*packages.Package {
	t.Helper()
	t.Setenv("GOWORK", "off")
	cfg := &packages.Config{Mode: contractFakeLoadMode, Dir: "testdata/composeapp", Tests: true}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		t.Fatalf("load composeapp: %v", err)
	}
	if n := packages.PrintErrors(pkgs); n > 0 {
		t.Fatalf("%d load error(s) in composeapp", n)
	}
	if len(pkgs) == 0 {
		t.Fatal("composeapp loaded zero packages; fixture missing")
	}
	return pkgs
}

// TestArchTestCompose_InFileFakeIsViolation: a _test package that declares a type
// whose method set implements a generated contract interface is a hand-written
// subject. fakedmgr declares one; okmgr declares a type that merely lives beside
// the contract (no matching method set) and is clean.
func TestArchTestCompose_InFileFakeIsViolation(t *testing.T) {
	vs := contractFakeViolations(loadComposePkgs(t, "./internal/..."), composeSpec())
	if len(vs) != 1 {
		t.Fatalf("want exactly one in-file-fake violation (fakedmgr), got %+v", vs)
	}
	v := vs[0]
	if v.Pkg != "example.com/composeapp/internal/manager/fakedmgr" || v.File != "manager_scenarios.gen_test.go" || v.Rule != ruleTestCompose {
		t.Errorf("unexpected violation %+v", v)
	}
	if !strings.Contains(v.Detail, "inFileFake") || !strings.Contains(v.Detail, "FakedManager") {
		t.Errorf("detail must name the fake and the contract it implements, got %q", v.Detail)
	}
}
