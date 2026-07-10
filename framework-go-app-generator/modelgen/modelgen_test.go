package modelgen_test

import (
	"flag"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/modelgen"
)

var update = flag.Bool("update", false, "rewrite golden files")

// archistratorAllowlist is the 8-key engine-impl allowlist copied verbatim from
// the source emitter's engineImplAllowlist.
var archistratorAllowlist = []string{
	"reviewEngine",
	"handOffEngine",
	"interventionEngine",
	"settlementEngine",
	"billingEngine",
	"operationEstimationEngine",
	"autoscalerEngine",
	"estimationEngine",
}

// TestGenerateGreenfieldGolden runs Generate over the synthetic greenfield
// fixture (one manager with a plain + two component deps, one Postgres-backed
// RA, one allowlisted engine) and byte-compares each emitted file against a
// committed golden keyed by the goPackage's last segment.
func TestGenerateGreenfieldGolden(t *testing.T) {
	raw, err := os.ReadFile("../testdata/greenfield.project.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	got, err := modelgen.Generate(raw, modelgen.Config{
		ModulePath:          "example.com/greenfield",
		EngineImplAllowlist: []string{"pricingEngine"},
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	wantPkgs := map[string]string{
		"internal/manager/order":             "order",
		"internal/resourceaccess/orderstate": "orderstate",
		"internal/engine/pricing":            "pricing",
		"internal/manager/fulfillment":       "fulfillment",
	}
	if len(got) != len(wantPkgs) {
		t.Fatalf("Generate returned %d files, want %d (keys %v)", len(got), len(wantPkgs), keysOf(got))
	}
	for goPkg, short := range wantPkgs {
		src, ok := got[goPkg]
		if !ok {
			t.Fatalf("Generate did not return %q", goPkg)
		}
		if _, err := parser.ParseFile(token.NewFileSet(), short, src, parser.AllErrors); err != nil {
			t.Errorf("emitted %s does not parse: %v", goPkg, err)
		}
		checkGolden(t, filepath.Join("../testdata", "greenfield.modelgen."+short+".gen.go.golden"), src)
	}
}

// TestGenerateArchistratorFidelity runs Generate over the real archistrator
// project.json fixture with archistrator's ModulePath + 8-key allowlist, asserts
// every emitted file go/parser-parses, and byte-matches the systemdesign manager
// + projectstate RA outputs against the REAL committed contract.gen.go files
// snapshotted into testdata. A mismatch means the PORT has a bug (never edit the
// goldens — they are the ground truth from archistrator's committed tree).
func TestGenerateArchistratorFidelity(t *testing.T) {
	raw, err := os.ReadFile("../testdata/archistrator.project.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	got, err := modelgen.Generate(raw, modelgen.Config{
		ModulePath:          "github.com/mixofreality-studio/archistrator/server",
		EngineImplAllowlist: archistratorAllowlist,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if len(got) != 22 {
		t.Errorf("Generate returned %d files, want 22 (entries with goPackage)", len(got))
	}
	for goPkg, src := range got {
		if _, err := parser.ParseFile(token.NewFileSet(), goPkg, src, parser.AllErrors); err != nil {
			t.Errorf("emitted %s does not parse: %v", goPkg, err)
		}
	}

	snapshots := map[string]string{
		"internal/manager/systemdesign":        "archistrator.modelgen.systemdesign.gen.go.golden",
		"internal/resourceaccess/projectstate": "archistrator.modelgen.projectstate.gen.go.golden",
	}
	for goPkg, golden := range snapshots {
		src, ok := got[goPkg]
		if !ok {
			t.Fatalf("Generate did not return %q", goPkg)
		}
		want, err := os.ReadFile(filepath.Join("../testdata", golden))
		if err != nil {
			t.Fatalf("read snapshot %s: %v", golden, err)
		}
		if string(src) != string(want) {
			t.Errorf("BYTE MISMATCH for %s vs committed %s — the port emits differently than archistrator. Fix the PORT, never the golden.", goPkg, golden)
		}
	}
}

// TestGenerateErrorEmptyModulePath asserts Generate rejects an empty ModulePath
// naming the field.
func TestGenerateErrorEmptyModulePath(t *testing.T) {
	_, err := modelgen.Generate([]byte(`{"serviceContracts":{}}`), modelgen.Config{})
	if err == nil {
		t.Fatal("Generate should error on empty ModulePath")
	}
	if !contains(err.Error(), "ModulePath") {
		t.Fatalf("error should name ModulePath, got: %v", err)
	}
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
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
