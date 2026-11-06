package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const defectiveDoc = `{"serviceContracts":{
"zeroOps":{"component":"zeroOps","layer":"ResourceAccess","goPackage":"internal/resourceaccess/zero","$defs":{},"interface":{"name":"ZeroAccess","layer":"ResourceAccess","operations":[]}},
"lowerOp":{"component":"lowerOp","layer":"ResourceAccess","goPackage":"internal/resourceaccess/lower","$defs":{},"interface":{"name":"LowerAccess","layer":"ResourceAccess","operations":[{"name":"publish","params":null,"error":true}]}},
"noPkg":{"component":"noPkg","layer":"Engine","$defs":{},"interface":{"name":"NoPkgEngine","layer":"Engine","operations":[{"name":"Run","params":null,"error":true}]}}
}}`

// A project that does not load is refused with every problem, before any generation.
func TestRun_RefusesAProjectThatDoesNotLoad(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app/server\n\ngo 1.25\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(root, "project.json")
	if err := os.WriteFile(project, []byte(defectiveDoc), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	err := run([]string{"-project", project, "-root", root}, &out, &errOut)
	for _, code := range []string{"PM-OPS-EMPTY", "PM-OP-UNEXPORTED", "PM-GOPACKAGE-MISSING"} {
		if err == nil || !strings.Contains(err.Error(), code) {
			t.Errorf("error lacks %s: %v", code, err)
		}
	}
}
