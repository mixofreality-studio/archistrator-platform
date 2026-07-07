package temporalgen_test

import (
	"flag"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/temporalgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

var update = flag.Bool("update", false, "rewrite golden files")

func TestActivityName(t *testing.T) {
	if got := temporalgen.ActivityName("orderStateAccess", "ReadOrder"); got != "orderStateAccess.readOrder" {
		t.Fatal(got)
	}
}

func TestTaskQueueNameReproducesExisting(t *testing.T) {
	cases := map[string]string{
		"SystemDesignManager":  "system-design",
		"ProjectDesignManager": "project-design",
		"ConstructionManager":  "construction",
		"OperationsManager":    "operations",
		"BillingManager":       "billing",
	}
	for iface, want := range cases {
		if got := temporalgen.TaskQueueName(iface); got != want {
			t.Fatalf("%s: got %s want %s", iface, got, want)
		}
	}
}

// TestGenerateGreenfieldGolden runs Generate against the synthetic greenfield
// fixture (one manager, one ResourceAccess dep, one Engine dep, one plain
// dep) and byte-compares the three emitted files against their committed
// goldens. The goldens are near-empty at this step (Task 4 scaffold only);
// Tasks 5-7 grow the emitters and the goldens grow with them.
func TestGenerateGreenfieldGolden(t *testing.T) {
	m, err := projectmodel.LoadFile("../testdata/greenfield.project.json")
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}

	got, err := temporalgen.Generate(m, temporalgen.Config{
		ModulePath: "github.com/mixofreality-studio/archistrator/server",
		ManagerKey: "orderManager",
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	files := []string{"activities.gen.go", "invokers.gen.go", "worker.gen.go"}
	for _, name := range files {
		src, ok := got[name]
		if !ok {
			t.Fatalf("Generate did not return %q", name)
		}
		if _, err := parser.ParseFile(token.NewFileSet(), name, src, parser.AllErrors); err != nil {
			t.Fatalf("emitted %s does not parse: %v", name, err)
		}
		checkGolden(t, filepath.Join("../testdata", "greenfield."+name+".golden"), src)
	}

	if len(got) != len(files) {
		t.Fatalf("Generate returned %d files, want %d (got %v)", len(got), len(files), got)
	}
}

func checkGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update)", path, err)
	}
	if string(got) != string(want) {
		t.Errorf("output mismatch for %s (run with -update to refresh)", path)
	}
}
