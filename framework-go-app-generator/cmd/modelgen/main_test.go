package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

const doc = `{"serviceContracts":{
"todoAccess":{"component":"todoAccess","layer":"ResourceAccess","goPackage":"internal/resourceaccess/todo","infra":["Postgres"],"$defs":{"T":{"type":"object"}},"interface":{"name":"TodoAccess","layer":"resourceaccess","operations":[{"name":"Ping","params":[],"error":false}]}},
"sibling":{"component":"sibling","layer":"Engine","goPackage":"internal/engine/sibling","$defs":{"S":{"type":"object"}},"interface":{"name":"SiblingEngine","layer":"engine","operations":[{"name":"Ping","params":[],"error":false}]}},
"broken":{"component":"broken","layer":"ResourceAccess","goPackage":"internal/resourceaccess/broken","infra":["IdentityProvider"],"$defs":{"B":{"type":"object"}},"interface":{"name":"BrokenAccess","layer":"resourceaccess","operations":[{"name":"Ping","params":[],"error":false}]}},
"web":{"component":"web","layer":"Client","goPackage":"internal/client/web","$defs":{"W":{"type":"object"}},"interface":{"name":"Web","layer":"client","operations":[{"name":"Ping","params":[],"error":false}]}}
}}`

func fixture(t *testing.T) (root, project string) {
	t.Helper()
	root = t.TempDir()
	project = filepath.Join(root, "project.json")
	put(t, project, doc)
	put(t, filepath.Join(root, "go.mod"), "module example.com/fresh\n\ngo 1.26.0\n")
	return root, project
}

func put(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func exists(root, rel string) bool {
	_, err := os.Stat(filepath.Join(root, rel))
	return err == nil
}

func TestATaskGeneratesOnlyItsOwnContract(t *testing.T) {
	root, project := fixture(t)
	if err := run([]string{"-project", project, "-root", root, "-contract", "todoAccess"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"internal/resourceaccess/todo/contract.gen.go", "internal/resourceaccess/todo/fake/fake.gen.go"} {
		if !exists(root, p) {
			t.Errorf("%s was not written", p)
		}
	}
	for _, p := range []string{"internal/engine/sibling/contract.gen.go", "internal/engine/sibling/fake/fake.gen.go"} {
		if exists(root, p) {
			t.Errorf("%s must not land in this task's tree: a sibling's generated code imports a package that does not exist yet", p)
		}
	}
	if err := run([]string{"-project", project, "-root", root, "-check", "-contract", "todoAccess"}, io.Discard); err != nil {
		t.Errorf("the generated file must be current: %v", err)
	}
}

func TestCheckRequiresTheTasksOwnContract(t *testing.T) {
	root, project := fixture(t)
	err := run([]string{"-project", project, "-root", root, "-check", "-contract", "todoAccess"}, io.Discard)
	if !errors.Is(err, errDrift) {
		t.Fatalf("a named contract with no generated file must be drift, got %v", err)
	}
}

func TestCheckHoldsEveryContractedPackageWithCode(t *testing.T) {
	root, project := fixture(t)
	put(t, filepath.Join(root, "internal/engine/sibling/siblingengine.go"), "package sibling\n")
	// A test file alone is not a package yet.
	put(t, filepath.Join(root, "internal/resourceaccess/todo/access_hooks_test.go"), "package todo_test\n")
	err := run([]string{"-project", project, "-root", root, "-check"}, io.Discard)
	if !errors.Is(err, errDrift) {
		t.Fatalf("a contracted package with no contract.gen.go must be drift, got %v", err)
	}
	if err := run([]string{"-project", project, "-root", root}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !exists(root, "internal/engine/sibling/contract.gen.go") || exists(root, "internal/resourceaccess/todo/contract.gen.go") {
		t.Error("an unnamed run generates exactly the packages that hold code")
	}
	if err := run([]string{"-project", project, "-root", root, "-check"}, io.Discard); err != nil {
		t.Errorf("after generation the check is green: %v", err)
	}
	put(t, filepath.Join(root, "internal/engine/sibling/contract.gen.go"), "package sibling\n")
	if err := run([]string{"-project", project, "-root", root, "-check"}, io.Discard); !errors.Is(err, errDrift) {
		t.Errorf("a hand-edited generated file must be drift, got %v", err)
	}
}

func TestNoBuiltContractIsNotAnError(t *testing.T) {
	root, project := fixture(t)
	if err := run([]string{"-project", project, "-root", root, "-check"}, io.Discard); err != nil {
		t.Errorf("a fresh tree has nothing to drift from: %v", err)
	}
}
