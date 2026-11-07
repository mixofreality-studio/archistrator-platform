package projectmodel

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func problemFixture(t *testing.T, name string) json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile("testdata/problems/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func findProblem(t *testing.T, probs Problems, code string) Problem {
	t.Helper()
	for _, p := range probs {
		if p.Code == code {
			return p
		}
	}
	t.Fatalf("no %s problem in:\n%v", code, probs)
	return Problem{}
}

func TestValidateContract_UnexportedOpName(t *testing.T) {
	probs := ValidateContract("messageBus", problemFixture(t, "unexported-op.json"))
	p := findProblem(t, probs, CodeOpUnexported)
	if want := "serviceContracts.messageBus.interface.operations[0].name"; p.Path != want {
		t.Fatalf("path: got %q, want %q", p.Path, want)
	}
}

// A built joinable-layer contract is Go-emitting by definition, so a missing goPackage must not hide
// its unexported operation names: both problems surface in one pass.
func TestValidateContract_MissingGoPackageStillReportsUnexportedOps(t *testing.T) {
	raw := json.RawMessage(`{"component":"artifactRenderingAccess","layer":"Engine","title":"t","$defs":{},
		"interface":{"name":"ArtifactRenderingAccess","layer":"engine","operations":[{"name":"renderSystem","params":null,"error":true}]}}`)
	probs := ValidateContract("artifactRenderingAccess", raw)
	if !probs.Has(CodeGoPackageMissing) || !probs.Has(CodeOpUnexported) {
		t.Fatalf("want both %s and %s in one pass, got %v", CodeGoPackageMissing, CodeOpUnexported, probs)
	}
}

// A client or utility contract is not emitted as Go, so its operation names are not Go identifiers.
func TestValidateContract_LowercaseOpNameIsFineWhereNoGoIsEmitted(t *testing.T) {
	raw := json.RawMessage(`{"component":"webClient","layer":"Client","title":"t","$defs":{},
		"interface":{"name":"WebClient","layer":"Client","operations":[{"name":"driveDesign","params":null,"error":false}]}}`)
	if probs := ValidateContract("webClient", raw); len(probs) != 0 {
		t.Fatalf("want no problems, got %v", probs)
	}
}

func TestValidateContract_ZeroOperations(t *testing.T) {
	probs := ValidateContract("messageBus", problemFixture(t, "zero-ops.json"))
	p := findProblem(t, probs, CodeOpsEmpty)
	if want := "serviceContracts.messageBus.interface.operations"; p.Path != want {
		t.Fatalf("path: got %q, want %q", p.Path, want)
	}
	for name, ops := range map[string]string{"null": `null`, "absent": ``} {
		field := ""
		if ops != "" {
			field = `,"operations":` + ops
		}
		raw := json.RawMessage(`{"component":"m","layer":"Engine","goPackage":"internal/engine/m","title":"t","$defs":{},"interface":{"name":"M","layer":"Engine"` + field + `}}`)
		if got := ValidateContract("m", raw); !got.Has(CodeOpsEmpty) {
			t.Errorf("%s operations: want %s, got %v", name, CodeOpsEmpty, got)
		}
	}
}

func TestValidateContract_MissingGoPackage(t *testing.T) {
	probs := ValidateContract("messageBus", problemFixture(t, "missing-gopackage.json"))
	p := findProblem(t, probs, CodeGoPackageMissing)
	if want := "serviceContracts.messageBus.goPackage"; p.Path != want {
		t.Fatalf("path: got %q, want %q", p.Path, want)
	}
	stub := json.RawMessage(`{"component":"m","layer":"Engine","stub":true,"title":"t","$defs":{},
		"interface":{"name":"M","layer":"Engine","operations":[{"name":"Do","params":null,"error":true}]}}`)
	if got := ValidateContract("m", stub); got.Has(CodeGoPackageMissing) {
		t.Fatalf("a stub contract has no Go emission target: %v", got)
	}
}

func TestValidateContract_UnknownField(t *testing.T) {
	probs := ValidateContract("messageBus", problemFixture(t, "unknown-field.json"))
	var paths []string
	for _, p := range probs {
		if p.Code == CodeFieldUnknown {
			paths = append(paths, p.Path)
		}
	}
	want := []string{"serviceContracts.messageBus.goPackge", "serviceContracts.messageBus.interface.operations[0].returns"}
	if len(paths) != len(want) {
		t.Fatalf("unknown-field paths: got %v, want %v", paths, want)
	}
	for _, w := range want {
		found := false
		for _, p := range paths {
			found = found || p == w
		}
		if !found {
			t.Fatalf("missing %s in %v", w, paths)
		}
	}
}

func TestValidateContract_UnknownLayer(t *testing.T) {
	probs := ValidateContract("messageBus", problemFixture(t, "bad-layer.json"))
	findProblem(t, probs, CodeLayerUnknown)
}

func TestValidateContract_CleanContractHasNoProblems(t *testing.T) {
	sc := loadFixtureContracts(t)
	for key, raw := range sc {
		if probs := ValidateContract(key, raw); len(probs) != 0 {
			t.Errorf("%s: %v", key, probs)
		}
	}
}

func TestLoad_ReportsAllProblemsAtOnce(t *testing.T) {
	doc := `{"serviceContracts":{
		"aEngine":` + string(problemFixture(t, "unexported-op.json")) + `,
		"bEngine":` + string(problemFixture(t, "zero-ops.json")) + `,
		"cEngine":` + string(problemFixture(t, "missing-gopackage.json")) + `
	}}`
	_, err := Load([]byte(doc))
	var probs Problems
	if !errors.As(err, &probs) {
		t.Fatalf("want Problems, got %T %v", err, err)
	}
	if len(probs) < 3 {
		t.Fatalf("want every problem at once (>= 3), got %d:\n%v", len(probs), probs)
	}
	for _, code := range []string{CodeOpUnexported, CodeOpsEmpty, CodeGoPackageMissing} {
		if !probs.Has(code) {
			t.Errorf("missing %s in:\n%v", code, probs)
		}
	}
}

func TestProblemsError_SortedByPathThenCode(t *testing.T) {
	p := Problems{
		{Path: "b", Code: "PM-A", Message: "m"},
		{Path: "a", Code: "PM-B", Message: "m"},
		{Path: "a", Code: "PM-A", Message: "m"},
	}
	lines := strings.Split(p.Error(), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], "PM-A a") || !strings.Contains(lines[1], "PM-B a") || !strings.Contains(lines[2], "PM-A b") {
		t.Fatalf("unsorted: %q", lines)
	}
	if p[0].Path != "b" {
		t.Fatal("Error must not reorder the receiver")
	}
}

func TestLoad_CleanDocumentIsUntypedNilError(t *testing.T) {
	_, err := Load([]byte(`{"serviceContracts":{},"slots":{}}`))
	if err != nil {
		t.Fatalf("want a nil error, got %#v", err)
	}
}

func TestValidateSystem_UnresolvedEndpoint(t *testing.T) {
	probs := ValidateSystem(json.RawMessage(`{"components":[{"id":"a"}],"relationships":[{"from":"a","to":"ghost"}]}`))
	p := findProblem(t, probs, CodeRelEnds)
	if p.Path != "systemDesign.relationships[0]" {
		t.Fatalf("path: %q", p.Path)
	}
}

func TestValidateDeployment_UndeclaredProfile(t *testing.T) {
	raw := json.RawMessage(`{"environments":[{"profile":"local"}],
		"infrastructure":[{"key":"pg","substrate":"postgres","profiles":["cloud"]}]}`)
	findProblem(t, ValidateDeployment(raw, nil), CodeDeployRef)
}

// An operation's readOnly flag is part of the contract document: methodcheck reads it to hold a probe
// to an observing operation, and the loader every generator runs first must not call it a misspelling.
func TestValidateContract_ReadOnlyIsAKnownOperationField(t *testing.T) {
	raw := json.RawMessage(`{"component":"messageBus","layer":"Utility","goPackage":"internal/utility/messagebus","title":"t","$defs":{},
		"interface":{"name":"MessageBus","layer":"utility","operations":[{"name":"GetCursor","params":[],"error":true,"readOnly":true}]}}`)
	if probs := ValidateContract("messageBus", raw); len(probs) != 0 {
		t.Fatalf("readOnly refused: %v", probs)
	}
	c, err := ParseContract("messageBus", raw)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Doc.Interface.Operations[0].ReadOnly {
		t.Error("readOnly did not decode")
	}
}
