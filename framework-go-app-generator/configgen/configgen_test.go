package configgen_test

import (
	"flag"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/configgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

var update = flag.Bool("update", false, "rewrite golden files")

// greenfieldCfg is the configgen invocation the golden + compile-proof share.
var greenfieldCfg = configgen.Config{
	ContainerKey: "order-app",
	EnvPrefix:    "ORDERAPP",
	PackageName:  "config",
}

// TestGreenfieldGolden emits config.gen.go for the greenfield deployment
// (2 profiles cloud+local; temporal required across BOTH profiles with a
// HOSTPORT env override; postgres required for cloud only — the per-profile /
// MissingFor case; github-app optional-dormant — the DormantWarnings case; all
// four setting types) and byte-compares it against the committed golden.
func TestGreenfieldGolden(t *testing.T) {
	m := loadGreenfield(t)
	got, err := configgen.Generate(m, greenfieldCfg)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	src, ok := got["config.gen.go"]
	if !ok {
		t.Fatal("Generate did not return config.gen.go")
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "config.gen.go", src, parser.AllErrors); err != nil {
		t.Fatalf("emitted config.gen.go does not parse: %v\n%s", err, src)
	}
	if !strings.Contains(string(src), "package config") {
		t.Errorf("missing 'package config' clause")
	}
	checkGolden(t, "../testdata/greenfield.configgen.config.gen.go.golden", src)
}

// TestDeterminism asserts two Generate runs are byte-identical.
func TestDeterminism(t *testing.T) {
	m := loadGreenfield(t)
	a, err := configgen.Generate(m, greenfieldCfg)
	if err != nil {
		t.Fatal(err)
	}
	b, err := configgen.Generate(m, greenfieldCfg)
	if err != nil {
		t.Fatal(err)
	}
	if string(a["config.gen.go"]) != string(b["config.gen.go"]) {
		t.Fatal("Generate is not deterministic")
	}
}

// TestCompileSandbox writes the emitted file into a throwaway module and proves
// it builds and vets under GOWORK=off — the emitter's expressibility proof.
func TestCompileSandbox(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping compile sandbox in -short")
	}
	m := loadGreenfield(t)
	out, err := configgen.Generate(m, greenfieldCfg)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module configsandbox\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.gen.go"), out["config.gen.go"], 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"build", "./..."}, {"vet", "./..."}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
		if o, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %v in sandbox failed: %v\n%s", args, err, o)
		}
	}
}

// TestConfigErrors asserts the required-parameter and resolution guards.
func TestConfigErrors(t *testing.T) {
	m := loadGreenfield(t)
	cases := []struct {
		name string
		cfg  configgen.Config
		want string
	}{
		{"no-package", configgen.Config{EnvPrefix: "X"}, "PackageName"},
		{"no-prefix", configgen.Config{PackageName: "config"}, "EnvPrefix"},
		{"bad-container", configgen.Config{PackageName: "config", EnvPrefix: "X", ContainerKey: "ghost"}, "container"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := configgen.Generate(m, c.cfg); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want %q error, got %v", c.want, err)
			}
		})
	}

	if _, err := configgen.Generate(&projectmodel.Model{}, configgen.Config{PackageName: "config", EnvPrefix: "X"}); err == nil || !strings.Contains(err.Error(), "deployment") {
		t.Fatalf("want no-deployment error, got %v", err)
	}
}

func loadGreenfield(t *testing.T) *projectmodel.Model {
	t.Helper()
	m, err := projectmodel.LoadFile("../testdata/greenfield.project.json")
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	return m
}

func checkGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update)", path, err)
	}
	if string(got) != string(want) {
		t.Errorf("output mismatch for %s (run with -update to refresh)", path)
	}
}
