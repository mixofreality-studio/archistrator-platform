package temporalgen_test

import (
	"flag"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/temporalgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

var update = flag.Bool("update", false, "rewrite golden files")

// sampleModulePath is the import root the compile-proof sample is generated
// against: it points INSIDE this module so the emitted RA import
// (sampleModulePath + "/internal/resourceaccess/orderstate") resolves to the
// hand-written stub package at internal/sample/internal/resourceaccess/orderstate.
const sampleModulePath = "github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/internal/sample"

// TestSampleInSync regenerates the greenfield trio against the in-module
// sampleModulePath, byte-compares each emitted file against the committed
// internal/sample/order/*.gen.go copies (rewrite with -update), then runs
// `go build ./internal/sample/...` (GOWORK=off) to prove the emitted trio
// compiles against the real Temporal SDK + framework-go — the httpgen
// TestSampleInSync pattern.
func TestSampleInSync(t *testing.T) {
	m, err := projectmodel.LoadFile("../testdata/greenfield.project.json")
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}

	got, err := temporalgen.Generate(m, temporalgen.Config{
		ModulePath:     sampleModulePath,
		ManagerKey:     "orderManager",
		CallerKeyedOps: map[string][]string{"orderState": {"ChargeOrder"}},
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	for _, name := range []string{"activities.gen.go", "invokers.gen.go", "worker.gen.go"} {
		src, ok := got[name]
		if !ok {
			t.Fatalf("Generate did not return %q", name)
		}
		checkGolden(t, filepath.Join("..", "internal", "sample", "order", name), src)
	}

	cmd := exec.Command("go", "build", "./internal/sample/...")
	cmd.Dir = ".."
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./internal/sample/... failed: %v\n%s", err, out)
	}
}

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
		ModulePath:     "github.com/mixofreality-studio/archistrator/server",
		ManagerKey:     "orderManager",
		CallerKeyedOps: map[string][]string{"orderState": {"ChargeOrder"}},
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
		if !strings.Contains(string(src), "package order") {
			t.Errorf("%s: does not contain 'package order', got:\n%s", name, string(src))
		}
		if strings.Contains(string(src), "package temporal") {
			t.Errorf("%s: emitted the generator's own package clause ('package temporal'):\n%s", name, string(src))
		}
		checkGolden(t, filepath.Join("../testdata", "greenfield."+name+".golden"), src)
	}

	if len(got) != len(files) {
		t.Fatalf("Generate returned %d files, want %d (got %v)", len(got), len(files), got)
	}
}

// TestGenerateErrorUnknownManager asserts that Generate returns an error
// when the ManagerKey is not found in the model.
func TestGenerateErrorUnknownManager(t *testing.T) {
	m, err := projectmodel.LoadFile("../testdata/greenfield.project.json")
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}

	_, err = temporalgen.Generate(m, temporalgen.Config{
		ModulePath: "github.com/mixofreality-studio/archistrator/server",
		ManagerKey: "nope",
	})
	if err == nil {
		t.Fatal("Generate should return an error for unknown ManagerKey")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Fatalf("error should contain 'nope', got: %v", err)
	}
}

// TestGenerateErrorEmptyGoPackage asserts that Generate returns an error
// when the Manager contract has an empty GoPackage.
func TestGenerateErrorEmptyGoPackage(t *testing.T) {
	m := &projectmodel.Model{
		Contracts: map[string]*projectmodel.Contract{
			"x": {
				Key:       "x",
				Layer:     "Manager",
				GoPackage: "",
				Doc:       nil,
			},
		},
	}

	_, err := temporalgen.Generate(m, temporalgen.Config{
		ModulePath: "github.com/mixofreality-studio/archistrator/server",
		ManagerKey: "x",
	})
	if err == nil {
		t.Fatal("Generate should return an error for empty goPackage")
	}
	if !strings.Contains(err.Error(), "goPackage") && !strings.Contains(err.Error(), "x") {
		t.Fatalf("error should mention goPackage or 'x', got: %v", err)
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
