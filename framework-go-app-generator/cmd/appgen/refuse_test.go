package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// defectiveProject has three independent loadability problems: a zero-operation contract, a
// lowercase operation name, and a built engine without a goPackage.
const defectiveProject = `{"serviceContracts":{
"zeroOps":{"component":"zeroOps","layer":"ResourceAccess","goPackage":"internal/resourceaccess/zero","$defs":{},"interface":{"name":"ZeroAccess","layer":"ResourceAccess","operations":[]}},
"lowerOp":{"component":"lowerOp","layer":"ResourceAccess","goPackage":"internal/resourceaccess/lower","$defs":{},"interface":{"name":"LowerAccess","layer":"ResourceAccess","operations":[{"name":"publish","params":null,"error":true}]}},
"noPkg":{"component":"noPkg","layer":"Engine","$defs":{},"interface":{"name":"NoPkgEngine","layer":"Engine","operations":[{"name":"Run","params":null,"error":true}]}}
}}`

// A project that does not load makes the binary exit 1 before any emission, and its stderr lists
// every problem (not only the first).
func TestAppgen_RefusesWithAllProblems(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/fresh\n\ngo 1.26.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(root, "project.json")
	if err := os.WriteFile(project, []byte(defectiveProject), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", ".", "compose", "-project", project, "-root", root)
	cmd.Env = append(os.Environ(), "GOWORK=off")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("want exit code 1, got %v\n%s", err, stderr.String())
	}
	for _, code := range []string{"PM-OPS-EMPTY", "PM-OP-UNEXPORTED", "PM-GOPACKAGE-MISSING"} {
		if !strings.Contains(stderr.String(), code) {
			t.Errorf("stderr lacks %s:\n%s", code, stderr.String())
		}
	}
	if _, err := os.Stat(filepath.Join(root, "cmd")); err == nil {
		t.Error("nothing may be emitted when the project does not load")
	}
}
