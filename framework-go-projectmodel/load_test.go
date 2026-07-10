// load_test.go
package projectmodel

import (
	"os"
	"strings"
	"testing"
)

func TestLoadArchistratorFixture(t *testing.T) {
	m, err := LoadFile("testdata/archistrator.project.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Contracts) != 28 {
		t.Fatalf("contracts: %d", len(m.Contracts))
	}
	if m.System == nil || len(m.System.Relationships) == 0 {
		t.Fatal("system not parsed")
	}
}

func TestLoadRejectsDanglingDep(t *testing.T) {
	raw, _ := os.ReadFile("testdata/broken.dangling-dep.json")
	_, err := Load(raw)
	if err == nil || !strings.Contains(err.Error(), "unknown component") {
		t.Fatalf("want dangling-dep error, got %v", err)
	}
}

func TestLoadRejectsLayerViolation(t *testing.T) {
	raw, _ := os.ReadFile("testdata/broken.layer-violation.json")
	_, err := Load(raw)
	if err == nil || !strings.Contains(err.Error(), "layer rule") {
		t.Fatalf("want layer-rule error, got %v", err)
	}
}

func TestLoadRejectsUnknownLayer(t *testing.T) {
	raw, _ := os.ReadFile("testdata/broken.unknown-layer.json")
	_, err := Load(raw)
	if err == nil || !strings.Contains(err.Error(), "unknown layer") {
		t.Fatalf("want unknown-layer error, got %v", err)
	}
}

// TestLoadToleratesEmptySlotsMap pins the other half of the "slot kind entirely
// absent" tolerance: a document whose slots map is present but empty (real
// shape seen in the gtdapp project.json — no slot 5/6 written yet). Load must
// succeed with both System and Deployment nil, not error on a missing kind==5
// or kind==6 slot.
func TestLoadToleratesEmptySlotsMap(t *testing.T) {
	raw := []byte(`{
		"serviceContracts": {
			"fooEngine": {
				"component": "fooEngine",
				"layer": "Engine",
				"goPackage": "internal/engine/foo",
				"title": "foo contract",
				"$defs": {},
				"interface": {"name": "FooEngine", "layer": "Engine", "operations": []}
			}
		},
		"slots": {}
	}`)
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(m.Contracts) != 1 {
		t.Fatalf("contracts: got %d, want 1", len(m.Contracts))
	}
	if m.Deployment != nil {
		t.Fatalf("Deployment: got %+v, want nil", m.Deployment)
	}
	if m.System != nil {
		t.Fatalf("System: got %+v, want nil", m.System)
	}
}
