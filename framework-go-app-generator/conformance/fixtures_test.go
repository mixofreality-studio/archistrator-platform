package conformance

import (
	"testing"

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
			findings, err := methodcheck.ValidateProjectJSON(raw)
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
