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

// The hooks file is the app's after the first generation: seeded once, never overwritten, and
// -check neither writes nor requires it.
func TestComposeSeedsHooksOnce(t *testing.T) {
	root, base := appFixture(t, "internal/resourceaccess/todo", "internal/manager/list")
	hooks := filepath.Join(root, "cmd/server/hooks.go")
	if err := run(append([]string{"compose"}, base...), io.Discard); err != nil {
		t.Fatal(err)
	}
	seed, err := os.ReadFile(hooks)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"func main()", "RunGenerated(cfg, appHooks{}, logger)"} {
		if !strings.Contains(string(seed), want) {
			t.Errorf("the seed has no %s:\n%s", want, seed)
		}
	}
	if err := os.WriteFile(hooks, []byte("package main\n// mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(append([]string{"compose"}, base...), io.Discard); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(hooks); !strings.Contains(string(got), "// mine") {
		t.Fatalf("compose overwrote the app's hooks:\n%s", got)
	}
}

func TestClientsGeneratesFromTheSystem(t *testing.T) {
	root, base := appFixture(t, "internal/manager/list")
	doc := strings.TrimSuffix(strings.TrimSpace(projectDoc), "}") + `,"slots":{"5":{"kind":5,"model":{
"components":[{"id":"api","name":"MachineApi","kind":"client","uiSurface":false},{"id":"list-manager","name":"ListManager","kind":"manager","contractKey":"listManager"}],
"relationships":[{"from":"api","to":"list-manager"}]}}}}`
	if err := os.WriteFile(filepath.Join(root, ".aiarch/state/project.json"), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(append([]string{"clients"}, base...), io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"internal/client/web/server.gen.go", "internal/client/web/list/list_handlers.gen.go", "internal/client/machineapi/list/list_tools.gen.go", "api/openapi.yaml"} {
		if _, err := os.Stat(filepath.Join(root, want)); err != nil {
			t.Errorf("no %s", want)
		}
	}
	if err := run(append([]string{"clients", "-check"}, base...), io.Discard); err != nil {
		t.Errorf("clients -check after clients: %v", err)
	}
}

// A Manager's Temporal layer exists from its first construction task, because the hand-written
// Manager is built against it, and generated files never count as the Manager being built: the
// composition root must not wire a package with no hand-written code yet.
func TestTemporalPrecedesTheManagersCode(t *testing.T) {
	root, base := appFixture(t, "internal/resourceaccess/todo")
	contract := filepath.Join(root, "internal/manager/list/contract.gen.go")
	if err := os.MkdirAll(filepath.Dir(contract), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(contract, []byte("package list\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(append([]string{"temporal"}, base...), io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "internal/manager/list/worker.gen.go")); err != nil {
		t.Fatalf("no Temporal layer before the Manager's code: %v", err)
	}
	if err := run(append([]string{"compose"}, base...), io.Discard); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(root, "cmd/server/main.gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), "NewListManager") {
		t.Fatal("compose wired a Manager with only generated files")
	}
}
