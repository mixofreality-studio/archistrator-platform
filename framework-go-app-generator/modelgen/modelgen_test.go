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

// busFixture declares a UTILITY-layer contract with a Temporal infra binding —
// the shape a restricted messaging utility (Manager-only clientele) takes once a
// durable-execution ResourceAccess folds into it. Before the utility layer was
// mapped, this emitted context-less signatures and NO infra constructor (the
// impl silently vanished); the tests below pin both halves.
const busFixture = `{
  "serviceContracts": {
    "messageBus": {
      "component": "messageBus",
      "layer": "Utility",
      "goPackage": "internal/utility/messagebus",
      "title": "messagebus contract",
      "infra": ["Temporal"],
      "$defs": {
        "ExecutionKind": { "type": "string", "enum": ["billing", "operations"] },
        "ExecutionID": { "type": "string" },
        "SignalName": { "type": "string" },
        "ExecutionPayload": {
          "type": "object",
          "properties": { "Bytes": { "type": "string", "contentEncoding": "base64" } },
          "required": ["Bytes"],
          "additionalProperties": false
        }
      },
      "interface": {
        "name": "MessageBus",
        "layer": "utility",
        "operations": [
          {
            "name": "DeliverSignal",
            "params": [
              { "name": "targetExecutionID", "schema": { "$ref": "#/$defs/ExecutionID" } },
              { "name": "signalName", "schema": { "$ref": "#/$defs/SignalName" } },
              { "name": "payload", "schema": { "$ref": "#/$defs/ExecutionPayload" } }
            ],
            "error": true
          }
        ]
      }
    }
  }
}`

// TestGenerateUtilityLayerContext asserts a Utility-layer contract emits the
// ResourceAccess call Context on every op (layerContext's deliberate reuse: an
// I/O utility's verbs are retried mutating calls needing Principal +
// IdempotencyKey) together with the fwra import, and — because Utility now
// shares the RA impl branch — the delegating infra constructor for its declared
// infra. A regression here means an infra-backed utility's implementation
// silently disappears from the generated surface.
func TestGenerateUtilityLayerContext(t *testing.T) {
	got, err := modelgen.Generate([]byte(busFixture), modelgen.Config{ModulePath: "example.com/bus"})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	src, ok := got["internal/utility/messagebus"]
	if !ok {
		t.Fatalf("Generate did not return the utility package (keys %v)", keysOf(got))
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "messagebus", src, parser.AllErrors); err != nil {
		t.Fatalf("emitted utility package does not parse: %v", err)
	}

	out := string(src)
	if !contains(out, `fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"`) {
		t.Error("utility contract should import the fwra call-context package")
	}
	if !contains(out, "DeliverSignal(rc fwra.Context,") {
		t.Errorf("utility op should take the fwra call Context, got:\n%s", out)
	}
	if !contains(out, "func NewTemporalMessageBus(") {
		t.Errorf("utility with infra should emit the delegating infra constructor, got:\n%s", out)
	}
}

// TestGenerateFakesUtilityLayer asserts the utility layer reaches the fake
// emitter too — a Fake<Iface> whose methods carry the same fwra.Context, so it
// satisfies the real generated interface.
func TestGenerateFakesUtilityLayer(t *testing.T) {
	got, err := modelgen.GenerateFakes([]byte(busFixture), modelgen.Config{ModulePath: "example.com/bus"})
	if err != nil {
		t.Fatalf("GenerateFakes: %v", err)
	}
	src, ok := got["internal/utility/messagebus/fake"]
	if !ok {
		t.Fatalf("GenerateFakes did not return the utility fake package (keys %v)", keysOf(got))
	}
	if !contains(string(src), "rc fwra.Context") {
		t.Errorf("utility fake should carry the fwra call Context, got:\n%s", src)
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
