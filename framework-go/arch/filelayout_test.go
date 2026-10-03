package arch

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// filelayout_test.go exercises the file-layout gate against the testdata
// layoutapp module: a clean Manager package and a clean Engine package produce
// zero violations, while badmgr/badeng between them exercise all eight
// violation rules — file-not-allowed, workflow-file-multiple-funcs,
// workflow-file-name, scenario-tests-only, test-package-not-external,
// hooks-import-not-allowed, hand-activity-registration,
// workflow-func-outside-manager.
//
// The pure core (fileLayoutViolations) is tested directly so violations are
// OBSERVED rather than routed to a failing t.Errorf, matching the
// gensurface_test.go pattern.

const layoutPrefix = "example.com/layoutapp/internal/"

func layoutSpec() Spec {
	return MethodSpec("testdata/layoutapp", layoutPrefix)
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
		{"manager_hooks_test.go", "test-package-not-external"},
		{"manager_hooks_test.go", "hooks-import-not-allowed"},
		{"regcall.go", "hand-activity-registration"},
		{"pure.go", "workflow-func-outside-manager"},
		{"orphanhelper.go", "file-not-allowed"},
	} {
		if !hasLayoutViolation(vs, want.file, want.rule) {
			t.Errorf("missing violation %s in %s: got %+v", want.rule, want.file, vs)
		}
	}
	for _, v := range vs {
		if strings.Contains(v.Pkg, "goodmgr") || strings.Contains(v.Pkg, "goodeng") {
			t.Errorf("clean package flagged: %+v", v)
		}
	}
}

// TestFileLayoutScenarioTestsOnlyPositive pins the positive path of the
// scenario-tests-only rule in isolation: goodmgr carries exactly the allowed
// pair (manager_scenarios.gen_test.go + manager_hooks_test.go), both
// black-box, the hooks file importing only its own package and stdlib — and
// yields ZERO violations of any rule.
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

// TestHooksImportAllowed pins the hooks-file import allowlist: own package,
// stdlib, any path ending in /testinfra or containing /scenariohost, and any
// Spec.HooksImportAllowlist prefix are allowed; everything else is not.
func TestHooksImportAllowed(t *testing.T) {
	own := "example.com/app/internal/manager/goodmgr"
	spec := Spec{HooksImportAllowlist: []string{"example.com/app/internal/contracts/"}}
	for _, tc := range []struct {
		path string
		want bool
	}{
		{own, true},
		{"testing", true},
		{"encoding/json", true},
		{"example.com/platform/framework-go/testinfra", true},
		{"example.com/platform/framework-go-scenariohost", true},
		{"example.com/platform/framework-go-scenariohost/sub", true},
		{"example.com/app/internal/contracts/gen", true},
		{"example.com/app/internal/contracts", false},
		{"example.com/app/internal/manager/othermgr", false},
		{"example.com/other/notallowed", false},
		{"github.com/stretchr/testify/require", false},
	} {
		if got := hooksImportAllowed(tc.path, own, spec); got != tc.want {
			t.Errorf("hooksImportAllowed(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}
