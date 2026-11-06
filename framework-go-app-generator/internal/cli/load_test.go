package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// defective is a project.json with three independent loadability problems: a zero-operation
// contract, a lowercase operation name, and a built engine without a goPackage.
const defective = `{"serviceContracts":{
"zeroOps":{"component":"zeroOps","layer":"ResourceAccess","goPackage":"internal/resourceaccess/zero","$defs":{},"interface":{"name":"ZeroAccess","layer":"ResourceAccess","operations":[]}},
"lowerOp":{"component":"lowerOp","layer":"ResourceAccess","goPackage":"internal/resourceaccess/lower","$defs":{},"interface":{"name":"LowerAccess","layer":"ResourceAccess","operations":[{"name":"publish","params":null,"error":true}]}},
"noPkg":{"component":"noPkg","layer":"Engine","$defs":{},"interface":{"name":"NoPkgEngine","layer":"Engine","operations":[{"name":"Run","params":null,"error":true}]}}
}}`

func TestAdmit_ReportsEveryProblem(t *testing.T) {
	err := Admit([]byte(defective))
	var probs projectmodel.Problems
	if !errors.As(err, &probs) {
		t.Fatalf("want projectmodel.Problems, got %T: %v", err, err)
	}
	for _, code := range []string{projectmodel.CodeOpsEmpty, projectmodel.CodeOpUnexported, projectmodel.CodeGoPackageMissing} {
		if !probs.Has(code) {
			t.Errorf("missing %s in:\n%v", code, err)
		}
	}
}

func TestAdmit_PassesALoadableProject(t *testing.T) {
	if err := Admit([]byte(`{"serviceContracts":{}}`)); err != nil {
		t.Fatalf("an empty project loads: %v", err)
	}
}

func TestLoad_ReadsAFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "project.json")
	if err := os.WriteFile(p, []byte(defective), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("a defective project must not load")
	}
	if _, err := Load(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Fatal("an absent file must not load")
	}
}
