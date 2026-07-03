package arch

import (
	"testing"

	"golang.org/x/tools/go/packages"
)

// gensurface_test.go exercises the encapsulation gate against the testdata
// gensurfaceapp module: a clean generated-contract package passes, a package
// leaking rogue exported symbols is flagged, the allowlist silences it, a package
// with no *.gen.go file is not targeted, and a hand-written type reachable from the
// generated surface is kept off the violation list.
//
// The pure core (generatedSurfaceViolations) is tested directly so violations are
// OBSERVED rather than routed to a failing t.Errorf. The public CheckGeneratedSurface
// wiring is exercised on the passing (allowlisted) path.

const gensurfacePrefix = "example.com/gensurfaceapp/internal/"

func gensurfaceSpec() Spec {
	return Spec{
		ModuleRoot:   "testdata/gensurfaceapp",
		ModulePrefix: gensurfacePrefix,
		Patterns:     []string{"./internal/..."},
	}
}

// loadGensurfacePkgs loads the testdata module (a nested module → GOWORK=off) with
// the rich mode CheckGeneratedSurface uses.
func loadGensurfacePkgs(t *testing.T) []*packages.Package {
	t.Helper()
	t.Setenv("GOWORK", "off")
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedDeps | packages.NeedImports,
		Dir:   "testdata/gensurfaceapp",
		Tests: false,
	}
	pkgs, err := packages.Load(cfg, "./internal/...")
	if err != nil {
		t.Fatalf("load gensurfaceapp: %v", err)
	}
	if n := packages.PrintErrors(pkgs); n > 0 {
		t.Fatalf("%d load error(s) in gensurfaceapp", n)
	}
	if len(pkgs) == 0 {
		t.Fatal("gensurfaceapp loaded zero packages; fixture missing")
	}
	return pkgs
}

func hasViolation(vios []surfaceViolation, rel, sym string) bool {
	for _, v := range vios {
		if v.rel == rel && v.sym == sym {
			return true
		}
	}
	return false
}

func TestGeneratedSurface_LeakyExportsFlagged(t *testing.T) {
	pkgs := loadGensurfacePkgs(t)
	vios := generatedSurfaceViolations(pkgs, gensurfacePrefix, nil)
	if !hasViolation(vios, "engine/leakyengine", "LeakyExtra") {
		t.Errorf("expected LeakyExtra flagged in engine/leakyengine, got %+v", vios)
	}
	if !hasViolation(vios, "engine/leakyengine", "LeakyFunc") {
		t.Errorf("expected LeakyFunc flagged in engine/leakyengine, got %+v", vios)
	}
}

func TestGeneratedSurface_CleanPackagePasses(t *testing.T) {
	pkgs := loadGensurfacePkgs(t)
	vios := generatedSurfaceViolations(pkgs, gensurfacePrefix, nil)
	for _, v := range vios {
		if v.rel == "engine/cleanengine" {
			t.Errorf("clean engine must produce no violations, got %+v", v)
		}
	}
}

func TestGeneratedSurface_ClosureTypeNotFlagged(t *testing.T) {
	pkgs := loadGensurfacePkgs(t)
	vios := generatedSurfaceViolations(pkgs, gensurfacePrefix, nil)
	// Detail is hand-written but reachable from the generated Result type.
	if hasViolation(vios, "engine/cleanengine", "Detail") {
		t.Errorf("a hand-written type reachable from the generated surface must not be flagged")
	}
}

func TestGeneratedSurface_NonGenPackageNotTargeted(t *testing.T) {
	pkgs := loadGensurfacePkgs(t)
	vios := generatedSurfaceViolations(pkgs, gensurfacePrefix, nil)
	// plainclient has no *.gen.go file → not a target, so its exports are ignored.
	for _, v := range vios {
		if v.rel == "client/plainclient" {
			t.Errorf("a package with no generated file must not be targeted, got %+v", v)
		}
	}
}

func TestGeneratedSurface_AllowlistSilencesLeak(t *testing.T) {
	pkgs := loadGensurfacePkgs(t)
	allow := map[string]map[string]bool{
		"engine/leakyengine": {"LeakyExtra": true, "LeakyFunc": true},
	}
	vios := generatedSurfaceViolations(pkgs, gensurfacePrefix, allow)
	if len(vios) != 0 {
		t.Errorf("allowlist must silence the rogue exports, got %+v", vios)
	}
}

// TestCheckGeneratedSurface_WiringPassesWithAllowlist drives the public entry point
// on the passing path: with the rogue symbols allowlisted, CheckGeneratedSurface must
// report nothing (no t.Errorf on this test).
func TestCheckGeneratedSurface_WiringPassesWithAllowlist(t *testing.T) {
	t.Setenv("GOWORK", "off")
	CheckGeneratedSurface(t, gensurfaceSpec(), map[string][]string{
		"engine/leakyengine": {"LeakyExtra", "LeakyFunc"},
	})
}
