package conformance

import (
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/appgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// TestFixtures_LoadUnderProjectmodelAndMethodcheck proves every embedded
// conformance fixture is admissible: it loads under projectmodel (Tier 0),
// decodes under methodcheck, and carries zero Error-severity findings.
func TestFixtures_LoadUnderProjectmodelAndMethodcheck(t *testing.T) {
	names := FixtureNames()
	if len(names) == 0 {
		t.Fatal("no conformance fixtures embedded")
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			raw, err := Fixture(name)
			if err != nil {
				t.Fatalf("Fixture(%q): %v", name, err)
			}
			if _, err := projectmodel.Load(raw); err != nil {
				t.Fatalf("projectmodel.Load: %v", err)
			}
			if _, ok, err := methodcheck.DecodeProject(raw); err != nil || !ok {
				t.Fatalf("methodcheck.DecodeProject: ok=%v err=%v", ok, err)
			}
			findings, err := methodcheck.ValidateProjectJSON(raw, methodcheck.ValidateOptions{SupportedSurfaces: appgen.SupportedSurfaces()})
			if err != nil {
				t.Fatalf("methodcheck.ValidateProjectJSON: %v", err)
			}
			for _, f := range findings {
				if f.Severity == methodcheck.SeverityError {
					t.Errorf("error finding %s: %s", f.RuleID, f.Message)
				}
			}
		})
	}
}

// Every surface the generators claim to build is proven by a fixture: some embedded fixture has a
// built Client with that surface. A surface joins appgen.SupportedSurfaces only with its fixture.
func TestSupportedSurfaces_EachHasAConformanceFixture(t *testing.T) {
	have := map[string]bool{}
	for _, name := range FixtureNames() {
		raw, err := Fixture(name)
		if err != nil {
			t.Fatalf("Fixture(%q): %v", name, err)
		}
		m, err := projectmodel.Load(raw)
		if err != nil {
			t.Fatalf("projectmodel.Load(%q): %v", name, err)
		}
		for _, c := range m.System.Components {
			if c.Kind == "client" {
				have[string(c.Surface)] = true
			}
		}
	}
	for _, s := range appgen.SupportedSurfaces() {
		if !have[string(s)] {
			t.Errorf("surface %s is supported by the generators but no conformance fixture has a Client with it", s)
		}
	}
}
