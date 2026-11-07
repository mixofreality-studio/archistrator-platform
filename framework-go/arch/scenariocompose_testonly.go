package arch

import (
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// ruleScenarioComposeTestOnly is the rule id of the test-substrate gate: the generated
// scenariocompose package builds every component over the doubles the scenario host boots, so
// neither it nor the host may be reachable from the binary. Only _test packages import them.
const ruleScenarioComposeTestOnly = "ARCH-SCENARIOCOMPOSE-TEST-ONLY"

// scenarioHostModule is the import path of the scenario host module; it and everything beneath
// it is a test substrate.
const scenarioHostModule = "github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"

// scenarioComposeLoadMode is what the rule needs from a load: names and the direct imports.
const scenarioComposeLoadMode = packages.NeedName | packages.NeedImports | packages.NeedFiles | packages.NeedCompiledGoFiles

// CheckScenarioComposeTestOnly reports, via t.Errorf, every PRODUCTION package of the module that
// imports <module>/internal/scenariocompose or the scenario host module
// (ARCH-SCENARIOCOMPOSE-TEST-ONLY). The load covers the whole module ("./...", cmd/ included) with
// Tests:false, so a _test package, the only legitimate importer, is never seen. The generated
// scenariocompose package itself is exempt while every compiled file is generated (it is the
// package that builds on the host); one hand file and the exemption is gone. No other exemption
// exists.
func CheckScenarioComposeTestOnly(t *testing.T, spec Spec) {
	t.Helper()
	spec.requireSpec(t)
	cfg := &packages.Config{Mode: scenarioComposeLoadMode, Dir: spec.ModuleRoot, Tests: false}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		t.Fatalf("arch: packages.Load: %v", err)
	}
	if n := packages.PrintErrors(pkgs); n > 0 {
		t.Fatalf("arch: %d package load error(s); fix the build before checking the test substrates", n)
	}
	if len(pkgs) == 0 {
		t.Fatalf("arch: ./... matched no packages; the test-substrate check would pass vacuously")
	}
	for _, v := range scenarioComposeTestOnlyViolations(pkgs, spec) {
		t.Errorf("%s: %s: %s", v.Pkg, v.Rule, v.Detail)
	}
}

// scenarioComposeTestOnlyViolations is the pure core: pkgs must come from a Tests:false load.
func scenarioComposeTestOnlyViolations(pkgs []*packages.Package, spec Spec) []fileLayoutViolation {
	var out []fileLayoutViolation
	for _, p := range pkgs {
		if isGeneratedScenarioCompose(p, spec) {
			continue
		}
		for ip := range p.Imports {
			if isTestSubstrate(ip, spec) {
				out = append(out, fileLayoutViolation{p.PkgPath, "", ruleScenarioComposeTestOnly,
					"production code imports " + ip + "; only _test packages may import the scenariocompose package or the scenario host"})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Pkg != out[j].Pkg {
			return out[i].Pkg < out[j].Pkg
		}
		return out[i].Detail < out[j].Detail
	})
	return out
}

// isTestSubstrate reports whether importPath is the module's scenariocompose package or the
// scenario host module (or a package beneath it).
func isTestSubstrate(importPath string, spec Spec) bool {
	if spec.ModulePrefix != "" && importPath == spec.ModulePrefix+ScenarioComposeDir {
		return true
	}
	return importPath == scenarioHostModule || strings.HasPrefix(importPath, scenarioHostModule+"/")
}
