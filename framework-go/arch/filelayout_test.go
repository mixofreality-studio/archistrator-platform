package arch

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// filelayout_test.go exercises the file-layout gate against the testdata
// layoutapp module: a clean Manager package and a clean Engine package produce
// zero violations, while badmgr/badeng between them exercise all six violation
// rules — file-not-allowed, workflow-file-multiple-funcs, workflow-file-name,
// test-file-name, hand-activity-registration, workflow-func-outside-manager.
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
		{"badmgr_test.go", "test-file-name"},
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
