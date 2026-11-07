package arch

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// scenarioComposeTestOnlyFixture is the testdata module for ARCH-SCENARIOCOMPOSE-TEST-ONLY:
// badmgr imports the scenario host and cmd/server imports internal/scenariocompose from
// production code (both violations); the all-generated scenariocompose package itself, okmgr
// and cmd/clean are clean.
const scenarioComposeTestOnlyFixture = "testdata/scenariocomposeapp"

func loadScenarioComposeFixture(t *testing.T, patterns ...string) []*packages.Package {
	t.Helper()
	t.Setenv("GOWORK", "off")
	cfg := &packages.Config{Mode: scenarioComposeLoadMode, Dir: scenarioComposeTestOnlyFixture, Tests: false}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		t.Fatalf("load scenariocomposeapp: %v", err)
	}
	if n := packages.PrintErrors(pkgs); n > 0 {
		t.Fatalf("%d load error(s) in scenariocomposeapp", n)
	}
	if len(pkgs) == 0 {
		t.Fatal("scenariocomposeapp loaded zero packages; fixture missing")
	}
	return pkgs
}

// TestArchScenarioComposeTestOnly_Fires: a production package (cmd/ included) that imports
// the scenariocompose package or the scenario host is a violation, and each violation names
// the importing package and the test substrate it pulled in.
func TestArchScenarioComposeTestOnly_Fires(t *testing.T) {
	spec := MethodSpec(scenarioComposeTestOnlyFixture)
	vs := scenarioComposeTestOnlyViolations(loadScenarioComposeFixture(t, "./..."), spec)
	got := map[string]string{}
	for _, v := range vs {
		if v.Rule != ruleScenarioComposeTestOnly {
			t.Errorf("unexpected rule %q in %+v", v.Rule, v)
		}
		got[v.Pkg] = v.Detail
	}
	if len(vs) != 2 {
		t.Fatalf("want exactly two violations (badmgr, cmd/server), got %+v", vs)
	}
	if d := got["example.com/scenariocomposeapp/internal/manager/badmgr"]; !strings.Contains(d, "framework-go-scenariohost") {
		t.Errorf("badmgr must be reported for the scenario host import, got %q", d)
	}
	if d := got["example.com/scenariocomposeapp/cmd/server"]; !strings.Contains(d, "internal/scenariocompose") {
		t.Errorf("cmd/server must be reported for the scenariocompose import, got %q", d)
	}
}

// TestArchScenarioComposeTestOnly_PassingFixture: the all-generated scenariocompose package
// (which imports the host by design), a package that imports neither, and a cmd/ that
// imports neither are clean.
func TestArchScenarioComposeTestOnly_PassingFixture(t *testing.T) {
	spec := MethodSpec(scenarioComposeTestOnlyFixture)
	pkgs := loadScenarioComposeFixture(t, "./internal/scenariocompose", "./internal/manager/okmgr", "./cmd/clean")
	if vs := scenarioComposeTestOnlyViolations(pkgs, spec); len(vs) != 0 {
		t.Fatalf("want no violations, got %+v", vs)
	}
}

// TestArchScenarioComposeTestOnly_HandFileLosesTheExemption: scenariocompose is exempt only
// while every compiled file is generated; one hand file and its host import is a violation.
func TestArchScenarioComposeTestOnly_HandFileLosesTheExemption(t *testing.T) {
	spec := Spec{ModulePrefix: "example.com/app/internal/"}
	host := &packages.Package{PkgPath: scenarioHostModule}
	for _, c := range []struct {
		name  string
		files []string
		want  int
	}{
		{"all generated", []string{"/x/compose.gen.go"}, 0},
		{"one hand file", []string{"/x/compose.gen.go", "/x/helper.go"}, 1},
	} {
		pkg := &packages.Package{
			PkgPath:         "example.com/app/internal/scenariocompose",
			CompiledGoFiles: c.files,
			Imports:         map[string]*packages.Package{scenarioHostModule: host},
		}
		if got := scenarioComposeTestOnlyViolations([]*packages.Package{pkg}, spec); len(got) != c.want {
			t.Errorf("%s: want %d violation(s), got %+v", c.name, c.want, got)
		}
	}
}
