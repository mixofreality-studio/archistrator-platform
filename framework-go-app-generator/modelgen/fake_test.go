package modelgen_test

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/modelgen"
)

// widgetFixture is a small synthetic project.json exercising the shapes
// TestGenerateFakesGolden asserts on: two goPackages (one ResourceAccess, one
// Engine), a multi-op interface, a (T,error) op, an error-only op, and a
// pointer param.
const widgetFixture = `{
  "serviceContracts": {
    "widgetAccess": {
      "component": "widgetAccess",
      "layer": "ResourceAccess",
      "goPackage": "internal/resourceaccess/widget",
      "title": "widgetAccess contract",
      "$defs": {
        "WidgetID": { "type": "string" },
        "Widget": {
          "type": "object",
          "properties": { "ID": { "$ref": "#/$defs/WidgetID" } },
          "required": ["ID"],
          "additionalProperties": false
        },
        "WidgetFilter": {
          "type": "object",
          "properties": { "Prefix": { "type": "string" } },
          "required": ["Prefix"],
          "additionalProperties": false
        }
      },
      "interface": {
        "name": "WidgetAccess",
        "layer": "resourceaccess",
        "operations": [
          {
            "name": "GetWidget",
            "params": [ { "name": "id", "schema": { "$ref": "#/$defs/WidgetID" } } ],
            "result": { "$ref": "#/$defs/Widget" },
            "error": true
          },
          {
            "name": "DeleteWidget",
            "params": [ { "name": "id", "schema": { "$ref": "#/$defs/WidgetID" } } ],
            "error": true
          },
          {
            "name": "FindWidgets",
            "params": [ { "name": "filter", "pointer": true, "schema": { "$ref": "#/$defs/WidgetFilter" } } ],
            "result": { "type": "array", "items": { "$ref": "#/$defs/WidgetID" } },
            "error": true
          }
        ]
      }
    },
    "classifierEngine": {
      "component": "classifierEngine",
      "layer": "Engine",
      "goPackage": "internal/engine/classifier",
      "title": "classifierEngine contract",
      "$defs": {
        "WidgetID": { "type": "string" },
        "Classification": { "type": "string" }
      },
      "interface": {
        "name": "ClassifierEngine",
        "layer": "engine",
        "operations": [
          {
            "name": "Classify",
            "params": [ { "name": "id", "schema": { "$ref": "#/$defs/WidgetID" } } ],
            "result": { "$ref": "#/$defs/Classification" },
            "error": true
          }
        ]
      }
    }
  }
}`

// leakyFixture declares a ResourceAccess whose sole op returns an unexported
// $def ("secretToken", lowercase) — the sibling <base>fake package cannot name
// it, so GenerateFakes must error rather than emit uncompilable code.
const leakyFixture = `{
  "serviceContracts": {
    "leakyAccess": {
      "component": "leakyAccess",
      "layer": "ResourceAccess",
      "goPackage": "internal/resourceaccess/leaky",
      "title": "leakyAccess contract",
      "$defs": {
        "secretToken": { "type": "string" }
      },
      "interface": {
        "name": "LeakyAccess",
        "layer": "resourceaccess",
        "operations": [
          {
            "name": "PeekToken",
            "params": [],
            "result": { "$ref": "#/$defs/secretToken" },
            "error": true
          }
        ]
      }
    }
  }
}`

// TestGenerateFakesGolden runs GenerateFakes over widgetFixture and asserts
// each emitted fake.gen.go: parses as Go, carries the Fake<Iface> struct with
// one <Op>Fn field per op (multi-op, a (T,error) op, an error-only op, and a
// pointer param all present), the panic-if-unset delegator, and the
// `var _ <pkg>.<Iface> = (*Fake<Iface>)(nil)` assertion.
func TestGenerateFakesGolden(t *testing.T) {
	got, err := modelgen.GenerateFakes([]byte(widgetFixture), modelgen.Config{ModulePath: "example.com/widgets"})
	if err != nil {
		t.Fatalf("GenerateFakes: %v", err)
	}

	wantKeys := []string{"internal/resourceaccess/widget/fake", "internal/engine/classifier/fake"}
	if len(got) != len(wantKeys) {
		t.Fatalf("GenerateFakes returned %d files, want %d (keys %v)", len(got), len(wantKeys), keysOf(got))
	}
	for _, k := range wantKeys {
		if _, ok := got[k]; !ok {
			t.Fatalf("GenerateFakes did not return %q (keys %v)", k, keysOf(got))
		}
	}

	widget := string(got["internal/resourceaccess/widget/fake"])
	if _, err := parser.ParseFile(token.NewFileSet(), "fake.gen.go", widget, parser.AllErrors); err != nil {
		t.Fatalf("widget fake does not parse: %v\n%s", err, widget)
	}
	widgetNorm := squeezeSpace(widget)
	if !strings.Contains(widget, "package widgetfake") {
		t.Errorf("widget fake: want package widgetfake, got:\n%s", widget)
	}
	if !strings.Contains(widget, "type FakeWidgetAccess struct {") {
		t.Errorf("widget fake: missing FakeWidgetAccess struct:\n%s", widget)
	}
	for _, want := range []string{
		// multi-op: one Fn field per operation. squeezeSpace tolerates gofmt's
		// struct-field column-alignment padding between the field name and "func".
		"GetWidgetFn func(rc fwra.Context, id widget.WidgetID) (widget.Widget, error)",
		// (T,error) op reused above; error-only op:
		"DeleteWidgetFn func(rc fwra.Context, id widget.WidgetID) error",
		// pointer param, rendered via paramType (not goType) as *widget.WidgetFilter:
		"FindWidgetsFn func(rc fwra.Context, filter *widget.WidgetFilter) ([]widget.WidgetID, error)",
		// panic-if-unset delegator (one example; all three ops follow the same shape):
		`panic("FakeWidgetAccess.GetWidgetFn not set")`,
		"return f.GetWidgetFn(rc, id)",
		// error-only shape must still `return` (no result, but Error:true):
		"return f.DeleteWidgetFn(rc, id)",
		// compile-time assertion against the contract package selector:
		"var _ widget.WidgetAccess = (*FakeWidgetAccess)(nil)",
		// contract package imported under its own (unaliased) name:
		`"example.com/widgets/internal/resourceaccess/widget"`,
	} {
		if !strings.Contains(widgetNorm, want) {
			t.Errorf("widget fake missing %q, got:\n%s", want, widget)
		}
	}

	classifier := string(got["internal/engine/classifier/fake"])
	if _, err := parser.ParseFile(token.NewFileSet(), "fake.gen.go", classifier, parser.AllErrors); err != nil {
		t.Fatalf("classifier fake does not parse: %v\n%s", err, classifier)
	}
	classifierNorm := squeezeSpace(classifier)
	for _, want := range []string{
		"package classifierfake",
		"type FakeClassifierEngine struct {",
		"ClassifyFn func(rc fweng.Context, id classifier.WidgetID) (classifier.Classification, error)",
		"var _ classifier.ClassifierEngine = (*FakeClassifierEngine)(nil)",
	} {
		if !strings.Contains(classifierNorm, want) {
			t.Errorf("classifier fake missing %q, got:\n%s", want, classifier)
		}
	}
}

// squeezeSpace collapses runs of spaces/tabs (never newlines) to a single
// space, so a substring assertion doesn't need to account for gofmt's
// struct-field column-alignment padding.
func squeezeSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// TestGenerateFakesDeterministic asserts GenerateFakes run twice over the same
// input is byte-identical, per key — the non-negotiable determinism
// requirement (sorted goPackage keys, ops in iface.Operations order, imports
// via the shared sorted pendingImports path).
func TestGenerateFakesDeterministic(t *testing.T) {
	cfg := modelgen.Config{ModulePath: "example.com/widgets"}

	first, err := modelgen.GenerateFakes([]byte(widgetFixture), cfg)
	if err != nil {
		t.Fatalf("GenerateFakes (1st): %v", err)
	}
	second, err := modelgen.GenerateFakes([]byte(widgetFixture), cfg)
	if err != nil {
		t.Fatalf("GenerateFakes (2nd): %v", err)
	}

	if len(first) != len(second) {
		t.Fatalf("run count mismatch: %d vs %d", len(first), len(second))
	}
	for k, want := range first {
		got, ok := second[k]
		if !ok {
			t.Fatalf("2nd run missing key %q", k)
		}
		if !bytes.Equal(want, got) {
			t.Errorf("GenerateFakes(%q) not deterministic:\n--- run 1 ---\n%s\n--- run 2 ---\n%s", k, want, got)
		}
	}
}

// TestGenerateFakesUnexportedRefErrors asserts GenerateFakes errors (naming
// the offending type) rather than emitting code that references an unexported
// $def type from the sibling fake package.
func TestGenerateFakesUnexportedRefErrors(t *testing.T) {
	_, err := modelgen.GenerateFakes([]byte(leakyFixture), modelgen.Config{ModulePath: "example.com/leaky"})
	if err == nil {
		t.Fatal("GenerateFakes should error on an unexported $def referenced by an interface method")
	}
	if !contains(err.Error(), "secretToken") {
		t.Fatalf("error should name the offending type %q, got: %v", "secretToken", err)
	}
}

// TestGenerateFakesErrorEmptyModulePath mirrors
// TestGenerateErrorEmptyModulePath: GenerateFakes rejects an empty ModulePath,
// naming the field.
func TestGenerateFakesErrorEmptyModulePath(t *testing.T) {
	_, err := modelgen.GenerateFakes([]byte(`{"serviceContracts":{}}`), modelgen.Config{})
	if err == nil {
		t.Fatal("GenerateFakes should error on empty ModulePath")
	}
	if !contains(err.Error(), "ModulePath") {
		t.Fatalf("error should name ModulePath, got: %v", err)
	}
}

// fakeProofModulePath is the import root the compile-proof pair (contract.gen.go
// + fake.gen.go) is generated against: it points INSIDE this module, under
// internal/sample/fakeproof, a subtree dedicated to this proof (kept separate
// from temporalgen's own internal/sample/order + orderstate stub — that stub
// is hand-written against framework-go-projectmodel's "integer"->int
// convention, which genuinely differs from modelgen's own "integer"->int64
// (goTypeForType); reusing it here would fail to compile for a reason
// unrelated to GenerateFakes). Generating BOTH files with modelgen itself
// keeps them self-consistent by construction — whatever Go type Generate
// picks for a schema node, GenerateFakes picks the identical one, since both
// go through the same goType/paramType/returnClause — and needs no
// hand-written stub package at all.
const fakeProofModulePath = "github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/internal/sample/fakeproof"

// TestGenerateFakesCompileProof regenerates widgetFixture's contract.gen.go
// (via Generate) and fake.gen.go (via GenerateFakes) for both its goPackages
// (a ResourceAccess and an Engine — the multi-op interface, the (T,error) op,
// the error-only op, and the pointer param all present), byte-compares each
// against its committed internal/sample/fakeproof/... golden (rewrite with
// -update), then runs `go build ./internal/sample/...` (GOWORK=off) to prove
// the emitted fakes compile for REAL against framework-go/resourceaccess and
// framework-go/engine — the go-build compile-verification the brief asks for,
// not just format.Source — following temporalgen's TestSampleInSync pattern.
func TestGenerateFakesCompileProof(t *testing.T) {
	cfg := modelgen.Config{ModulePath: fakeProofModulePath}

	contracts, err := modelgen.Generate([]byte(widgetFixture), cfg)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	fakes, err := modelgen.GenerateFakes([]byte(widgetFixture), cfg)
	if err != nil {
		t.Fatalf("GenerateFakes: %v", err)
	}

	sampleRoot := filepath.Join("..", "internal", "sample", "fakeproof")
	for _, goPkg := range []string{"internal/resourceaccess/widget", "internal/engine/classifier"} {
		contractSrc, ok := contracts[goPkg]
		if !ok {
			t.Fatalf("Generate did not return %q (keys %v)", goPkg, keysOf(contracts))
		}
		checkGolden(t, filepath.Join(sampleRoot, goPkg, "contract.gen.go"), contractSrc)

		fakeSrc, ok := fakes[goPkg+"/fake"]
		if !ok {
			t.Fatalf("GenerateFakes did not return %q (keys %v)", goPkg+"/fake", keysOf(fakes))
		}
		checkGolden(t, filepath.Join(sampleRoot, goPkg, "fake", "fake.gen.go"), fakeSrc)
	}

	cmd := exec.Command("go", "build", "./internal/sample/...")
	cmd.Dir = ".."
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./internal/sample/... failed: %v\n%s", err, out)
	}
}
