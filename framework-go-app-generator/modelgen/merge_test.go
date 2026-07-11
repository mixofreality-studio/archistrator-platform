package modelgen_test

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/modelgen"
)

// twoComponentsOnePackageFixture declares two RA-layer `.serviceContracts`
// entries — "primaryAccess" and "secondaryAccess" — that share one goPackage
// (internal/resourceaccess/shared), the multi-component-per-package shape a
// secondary RA takes when RA→RA imports force it onto an existing package's
// directory. Both entries declare the same "SharedID" $def, byte-identical,
// to prove shared/identical defs are deduped rather than duplicated or
// rejected as a collision.
const twoComponentsOnePackageFixture = `{
  "serviceContracts": {
    "primaryAccess": {
      "component": "primaryAccess",
      "layer": "ResourceAccess",
      "goPackage": "internal/resourceaccess/shared",
      "title": "primaryAccess contract",
      "$defs": {
        "SharedID": { "type": "string" },
        "Widget": {
          "type": "object",
          "properties": { "ID": { "$ref": "#/$defs/SharedID" } },
          "required": ["ID"],
          "additionalProperties": false
        }
      },
      "interface": {
        "name": "PrimaryAccess",
        "layer": "resourceaccess",
        "operations": [
          {
            "name": "ReadWidget",
            "params": [ { "name": "id", "schema": { "$ref": "#/$defs/SharedID" } } ],
            "result": { "$ref": "#/$defs/Widget" },
            "error": true
          }
        ]
      }
    },
    "secondaryAccess": {
      "component": "secondaryAccess",
      "layer": "ResourceAccess",
      "goPackage": "internal/resourceaccess/shared",
      "title": "secondaryAccess contract",
      "$defs": {
        "SharedID": { "type": "string" },
        "Gadget": {
          "type": "object",
          "properties": { "ID": { "$ref": "#/$defs/SharedID" } },
          "required": ["ID"],
          "additionalProperties": false
        }
      },
      "interface": {
        "name": "SecondaryAccess",
        "layer": "resourceaccess",
        "operations": [
          {
            "name": "ReadGadget",
            "params": [ { "name": "id", "schema": { "$ref": "#/$defs/SharedID" } } ],
            "result": { "$ref": "#/$defs/Gadget" },
            "error": true
          }
        ]
      }
    }
  }
}`

// TestGenerateMergesSharedGoPackage proves the core B1 requirement: two
// `.serviceContracts` entries with the same goPackage produce ONE
// contract.gen.go containing both interfaces, with the shared "SharedID" $def
// emitted exactly once (no duplicate type declaration).
func TestGenerateMergesSharedGoPackage(t *testing.T) {
	got, err := modelgen.Generate([]byte(twoComponentsOnePackageFixture), modelgen.Config{
		ModulePath: "example.com/merge",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Generate returned %d files, want 1 (both entries share one goPackage), keys=%v", len(got), keysOf(got))
	}
	src, ok := got["internal/resourceaccess/shared"]
	if !ok {
		t.Fatalf("Generate did not return internal/resourceaccess/shared, keys=%v", keysOf(got))
	}

	if _, err := parser.ParseFile(token.NewFileSet(), "shared", src, parser.AllErrors); err != nil {
		t.Fatalf("emitted file does not parse: %v\n%s", err, src)
	}

	s := string(src)
	for _, want := range []string{
		"type PrimaryAccess interface {",
		"type SecondaryAccess interface {",
		"type Widget struct {",
		"type Gadget struct {",
		"type SharedID string",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("emitted file missing %q:\n%s", want, s)
		}
	}

	if n := strings.Count(s, "type SharedID string"); n != 1 {
		t.Errorf("SharedID declared %d times, want exactly 1 (deduped shared $def):\n%s", n, s)
	}
	if n := strings.Count(s, "package shared"); n != 1 {
		t.Errorf("expected exactly one package clause, got %d:\n%s", n, s)
	}
}

// TestGenerateCollidingSharedDefFailsLoudly proves the other B1 requirement:
// two entries sharing a goPackage that declare the SAME $def name with
// DIFFERENT shapes must fail the merge loudly, never silently pick one side's
// version.
func TestGenerateCollidingSharedDefFailsLoudly(t *testing.T) {
	// secondaryAccess's SharedID becomes an object, not a string — same name
	// as primaryAccess's SharedID, incompatible shape. Target only
	// secondaryAccess's block explicitly so the intent is unambiguous.
	colliding := strings.Replace(twoComponentsOnePackageFixture,
		`"secondaryAccess": {
      "component": "secondaryAccess",
      "layer": "ResourceAccess",
      "goPackage": "internal/resourceaccess/shared",
      "title": "secondaryAccess contract",
      "$defs": {
        "SharedID": { "type": "string" },`,
		`"secondaryAccess": {
      "component": "secondaryAccess",
      "layer": "ResourceAccess",
      "goPackage": "internal/resourceaccess/shared",
      "title": "secondaryAccess contract",
      "$defs": {
        "SharedID": { "type": "object", "properties": { "V": { "type": "string" } }, "required": ["V"], "additionalProperties": false },`,
		1)
	if colliding == twoComponentsOnePackageFixture {
		t.Fatal("test fixture setup: secondaryAccess replacement target not found")
	}

	_, err := modelgen.Generate([]byte(colliding), modelgen.Config{
		ModulePath: "example.com/merge",
	})
	if err == nil {
		t.Fatal("Generate should fail loudly on a colliding same-name-different-shape $def, got nil error")
	}
	for _, want := range []string{"SharedID", "primaryAccess", "secondaryAccess"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("collision error should name %q, got: %v", want, err)
		}
	}
}
