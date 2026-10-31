package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const projectDoc = `{"serviceContracts":{
"todoAccess":{"component":"todoAccess","layer":"ResourceAccess","goPackage":"internal/resourceaccess/todo","infra":["Postgres"],"$defs":{"T":{"type":"object"}},"interface":{"name":"TodoAccess","layer":"resourceaccess","operations":[]}},
"callerAccess":{"component":"callerAccess","layer":"ResourceAccess","goPackage":"internal/resourceaccess/caller","infra":["ExternalHTTP"],"$defs":{"C":{"type":"object"}},"interface":{"name":"CallerAccess","layer":"resourceaccess","operations":[]}},
"listManager":{"component":"listManager","layer":"Manager","goPackage":"internal/manager/list","deps":[{"name":"todos","component":"todoAccess"}],"$defs":{"L":{"type":"object"}},"interface":{"name":"ListManager","layer":"manager","operations":[]}}
}}`

func appFixture(t *testing.T, built ...string) (root string, base []string) {
	t.Helper()
	root = t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/fresh\n\ngo 1.26.0\n")
	write(".aiarch/state/project.json", projectDoc)
	for _, pkg := range built {
		write(pkg+"/impl.go", "package x\n")
	}
	return root, []string{"-project", filepath.Join(root, ".aiarch/state/project.json"), "-root", root}
}

func TestComposeWiresOnlyBuiltPackages(t *testing.T) {
	root, base := appFixture(t, "internal/resourceaccess/todo", "internal/manager/list")
	if err := run(append([]string{"compose"}, base...), io.Discard); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(root, "cmd/server/main.gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	for _, want := range []string{"NewPostgresTodoAccess", "NewListManager", "example.com/fresh/internal/manager/list"} {
		if !strings.Contains(body, want) {
			t.Errorf("main.gen.go does not name %s", want)
		}
	}
	if strings.Contains(body, "CallerAccess") {
		t.Error("an unbuilt component must not be wired: its package does not exist yet")
	}
	if err := run(append([]string{"compose", "-check"}, base...), io.Discard); err != nil {
		t.Errorf("compose -check after compose: %v", err)
	}
}

func TestConfigDeclaresTheDerivedInfrastructure(t *testing.T) {
	root, base := appFixture(t, "internal/resourceaccess/todo", "internal/manager/list")
	if err := run(append([]string{"config"}, base...), io.Discard); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(root, "cmd/server/config.gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "APP_POSTGRES") {
		t.Errorf("config must read the default prefix's Postgres input:\n%s", src)
	}
}

func TestCheckReportsMissingOutput(t *testing.T) {
	_, base := appFixture(t, "internal/resourceaccess/todo")
	if err := run(append([]string{"compose", "-check"}, base...), io.Discard); !errors.Is(err, errDrift) {
		t.Fatalf("got %v, want drift", err)
	}
}

func TestKnobsFileRefusesUnknownFields(t *testing.T) {
	root, base := appFixture(t)
	if err := os.WriteFile(filepath.Join(root, "appgen.json"), []byte(`{"contianer":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(append([]string{"config"}, base...), io.Discard); err == nil {
		t.Fatal("a misspelled knob must not be ignored")
	}
}
