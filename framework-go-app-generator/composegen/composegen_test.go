package composegen_test

import (
	"flag"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/composegen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

var update = flag.Bool("update", false, "rewrite golden files")

// greenfieldCfg is the composegen invocation the golden + compile-proof share.
var greenfieldCfg = composegen.Config{
	ContainerKey: "order-app",
	ModulePath:   "example.com/greenfield/server",
	PackageName:  "main",
	EnvPrefix:    "ORDERAPP",
}

func TestGreenfieldGolden(t *testing.T) {
	m := loadGreenfield(t)
	got, err := composegen.Generate(m, greenfieldCfg)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	src, ok := got["main.gen.go"]
	if !ok {
		t.Fatal("Generate did not return main.gen.go")
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "main.gen.go", src, parser.AllErrors); err != nil {
		t.Fatalf("emitted main.gen.go does not parse: %v\n%s", err, src)
	}
	if !strings.Contains(string(src), "package main") {
		t.Errorf("missing 'package main' clause")
	}
	checkGolden(t, "../testdata/greenfield.composegen.main.gen.go.golden", src)
}

func TestDeterminism(t *testing.T) {
	m := loadGreenfield(t)
	a, err := composegen.Generate(m, greenfieldCfg)
	if err != nil {
		t.Fatal(err)
	}
	b, err := composegen.Generate(m, greenfieldCfg)
	if err != nil {
		t.Fatal(err)
	}
	if string(a["main.gen.go"]) != string(b["main.gen.go"]) {
		t.Fatal("Generate is not deterministic")
	}
}

func TestConfigErrors(t *testing.T) {
	m := loadGreenfield(t)
	cases := []struct {
		name string
		cfg  composegen.Config
		want string
	}{
		{"no-package", composegen.Config{ModulePath: "x"}, "PackageName"},
		{"no-module", composegen.Config{PackageName: "main"}, "ModulePath"},
		{"bad-container", composegen.Config{PackageName: "main", ModulePath: "x", ContainerKey: "ghost"}, "container"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := composegen.Generate(m, c.cfg); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want %q error, got %v", c.want, err)
			}
		})
	}
	if _, err := composegen.Generate(&projectmodel.Model{}, composegen.Config{PackageName: "main", ModulePath: "x"}); err == nil || !strings.Contains(err.Error(), "deployment") {
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
