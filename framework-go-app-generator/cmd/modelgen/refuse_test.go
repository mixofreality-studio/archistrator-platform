package main

import (
	"io"
	"path/filepath"
	"strings"
	"testing"
)

const defectiveDoc = `{"serviceContracts":{
"zeroOps":{"component":"zeroOps","layer":"ResourceAccess","goPackage":"internal/resourceaccess/zero","$defs":{},"interface":{"name":"ZeroAccess","layer":"ResourceAccess","operations":[]}},
"lowerOp":{"component":"lowerOp","layer":"ResourceAccess","goPackage":"internal/resourceaccess/lower","$defs":{},"interface":{"name":"LowerAccess","layer":"ResourceAccess","operations":[{"name":"publish","params":null,"error":true}]}},
"noPkg":{"component":"noPkg","layer":"Engine","$defs":{},"interface":{"name":"NoPkgEngine","layer":"Engine","operations":[{"name":"Run","params":null,"error":true}]}}
}}`

// A project that does not load is refused with every problem, before any generation, even when
// the run is scoped to one contract.
func TestRefusesAProjectThatDoesNotLoad(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project.json")
	put(t, project, defectiveDoc)
	put(t, filepath.Join(root, "go.mod"), "module example.com/fresh\n\ngo 1.26.0\n")
	err := run([]string{"-project", project, "-root", root, "-contract", "zeroOps"}, io.Discard)
	for _, code := range []string{"PM-OPS-EMPTY", "PM-OP-UNEXPORTED", "PM-GOPACKAGE-MISSING"} {
		if err == nil || !strings.Contains(err.Error(), code) {
			t.Errorf("error lacks %s: %v", code, err)
		}
	}
	if exists(root, "internal/resourceaccess/zero/contract.gen.go") {
		t.Error("nothing may be emitted when the project does not load")
	}
}
