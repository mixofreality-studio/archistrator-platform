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
