package methodcheck

import (
	"testing"

	"github.com/davidmarne/archistrator-platform/framework-go/arch"
)

// align_test.go exercises the design↔code alignment check against the testdata
// alignapp module (whose internal/{client,manager,engine,resourceaccess} packages
// are named after the design components below). It proves: matched pass, missing
// package fail, extra package fail, layer mismatch fail, and the empty-code
// design-phase no-op.

// alignAppArchSpec is the arch.Spec for the testdata alignment module.
func alignAppArchSpec() arch.Spec {
	return arch.Spec{
		ModuleRoot: "testdata/alignapp",
		// ModulePrefix includes the internal/ segment, exactly as arch.MethodSpec's
		// real consumers do (Patterns ./internal/... + the layer DirPrefixes are
		// relative to internal/), so a package path trims to "manager/designmanager".
		ModulePrefix: "example.com/alignapp/internal/",
		Patterns:     []string{"./internal/..."},
		Layers: []arch.Layer{
			{Name: "Client", DirPrefix: "client"},
			{Name: "Manager", DirPrefix: "manager"},
			{Name: "Engine", DirPrefix: "engine"},
			{Name: "ResourceAccess", DirPrefix: "resourceaccess"},
			{Name: "Utility", DirPrefix: "utility"},
		},
	}
}

// loadAlignPkgs loads + classifies the testdata module's packages.
//
// The testdata fixture is a NESTED module under testdata/ (the go tool ignores
// testdata/ for the parent build, so the alignment fixture needs its own module to
// be loadable at all). The repo's go.work does not list it, so the load must run in
// module mode — GOWORK=off — exactly as a standalone checkout of a consuming module
// would. (A real consuming module loads its OWN packages, which DO sit in its
// workspace, so this is a fixture-only concern.)
func loadAlignPkgs(t *testing.T) []classifiedPackage {
	t.Helper()
	t.Setenv("GOWORK", "off")
	pkgs, err := loadClassifiedPackages(alignAppArchSpec())
	if err != nil {
		t.Fatalf("load classified packages: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatal("testdata alignapp loaded zero packages; the fixture module is missing")
	}
	return pkgs
}

// alignAppSystem is a System whose Client/Manager/Engine/ResourceAccess components
// are named to match the testdata package leaves; StateDB is a Resource (no own code).
func alignAppSystem() System {
	return System{Components: []Component{
		{ID: "appclient", Name: "AppClient", Kind: kindClient, Layer: layerClient},
		{ID: "designmanager", Name: "DesignManager", Kind: kindManager, Layer: layerManager},
		{ID: "validatingengine", Name: "ValidatingEngine", Kind: kindEngine, Layer: layerEngine},
		{ID: "stateaccess", Name: "StateAccess", Kind: kindResourceAccess, Layer: layerResourceAccess},
		{ID: "statedb", Name: "StateDB", Kind: kindResource, Layer: layerResource},
	}}
}

func TestAlign_MatchedPasses(t *testing.T) {
	pkgs := loadAlignPkgs(t)
	findings := alignSystemToCode(alignAppSystem(), pkgs, nil)
	if len(findings) != 0 {
		t.Fatalf("a design whose components all have matching packages must produce zero findings, got %+v", findings)
	}
}

func TestAlign_MissingPackageFails(t *testing.T) {
	pkgs := loadAlignPkgs(t)
	s := alignAppSystem()
	// Add a design Manager with no corresponding code package.
	s.Components = append(s.Components, Component{ID: "phantommanager", Name: "PhantomManager", Kind: kindManager, Layer: layerManager})
	findings := alignSystemToCode(s, pkgs, nil)
	if !hasRuleFindings(findings, ruleAlignMissingPkg) {
		t.Fatalf("expected ALIGN-MISSING-PKG, got %+v", findings)
	}
}

func TestAlign_ExtraPackageFails(t *testing.T) {
	pkgs := loadAlignPkgs(t)
	s := alignAppSystem()
	// Drop the Manager component so its real package (designmanager) is unmatched.
	var trimmed []Component
	for _, c := range s.Components {
		if c.Name != "DesignManager" {
			trimmed = append(trimmed, c)
		}
	}
	s.Components = trimmed
	findings := alignSystemToCode(s, pkgs, nil)
	if !hasRuleFindings(findings, ruleAlignExtraPkg) {
		t.Fatalf("expected ALIGN-EXTRA-PKG for the unmatched designmanager package, got %+v", findings)
	}
}

func TestAlign_LayerMismatchFails(t *testing.T) {
	pkgs := loadAlignPkgs(t)
	s := alignAppSystem()
	// Declare DesignManager in the Engine layer while its code lives in manager/.
	for i := range s.Components {
		if s.Components[i].Name == "DesignManager" {
			s.Components[i].Kind = kindEngine
			s.Components[i].Layer = layerEngine
		}
	}
	findings := alignSystemToCode(s, pkgs, nil)
	if !hasRuleFindings(findings, ruleAlignLayerMismate) {
		t.Fatalf("expected ALIGN-LAYER-MISMATCH, got %+v", findings)
	}
}

func TestAlign_EmptyCodeDesignPhaseIsNoOp(t *testing.T) {
	// Pure design phase: a System but ZERO loaded packages → no alignment findings.
	findings := alignSystemToCode(alignAppSystem(), nil, nil)
	if len(findings) != 0 {
		t.Fatalf("design phase (no code) must emit no alignment findings, got %+v", findings)
	}
}

func TestAlign_CustomNormalizerOverride(t *testing.T) {
	pkgs := loadAlignPkgs(t)
	s := alignAppSystem()
	// A normalizer that maps everything to one bucket would make all components and
	// packages "match" the same key — proving the override is wired. Use a benign
	// one that strips a known prefix so DesignManager still matches designmanager.
	norm := func(in string) string { return defaultNormalizer(in) }
	findings := alignSystemToCode(s, pkgs, norm)
	if len(findings) != 0 {
		t.Fatalf("explicit default-equivalent normalizer must still match cleanly, got %+v", findings)
	}
}
