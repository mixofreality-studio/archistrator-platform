package appgen

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/composegen"
	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// A package holding only nullgen's null.gen.go is built: the conformance pipeline wires it. Any
// other generated file alone is not.
func TestHoldsCodeCountsNullImplementations(t *testing.T) {
	for _, tc := range []struct {
		file string
		want bool
	}{
		{"null.gen.go", true},
		{"contract.gen.go", false},
		{"thing_test.go", false},
		{"thing.go", true},
	} {
		tree := fstest.MapFS{"p/" + tc.file: &fstest.MapFile{Data: []byte("package p\n")}}
		if got := holdsCode(tree, "p"); got != tc.want {
			t.Errorf("holdsCode with only %s = %v, want %v", tc.file, got, tc.want)
		}
	}
}

// A machine client's package is generated end to end: `appgen clients` writes it and a person
// never adds a line. It is still built, so the model handed to compose keeps its contract and
// the scenario subject gets its constructor. A package with no generated file stays dropped.
func TestWireOnlyBuilt_KeepsGeneratedOnlyClients(t *testing.T) {
	tree := fstest.MapFS{}
	for _, rel := range []string{
		"internal/client/web/list/list_handlers.gen.go",
		"internal/manager/list/contract.gen.go",
		"internal/resourceaccess/todo/todo_test.go",
	} {
		tree[rel] = &fstest.MapFile{Data: []byte("package x\n")}
	}
	contract := func(layer, pkg string) *projectmodel.Contract {
		return &projectmodel.Contract{Layer: layer, GoPackage: pkg}
	}
	m := &projectmodel.Model{Contracts: map[string]*projectmodel.Contract{
		"todoApi":     contract("Client", "internal/client/web/list"),
		"ungenerated": contract("Client", "internal/client/web/absent"),
		"listManager": contract("Manager", "internal/manager/list"),
		"todoAccess":  contract("ResourceAccess", "internal/resourceaccess/todo"),
		"todoWebApp":  contract("Client", ""),
	}}
	wireOnlyBuilt(m, tree, false)
	if _, ok := m.Contracts["todoApi"]; !ok {
		t.Error("a Client whose package holds only generated files is built: its contract must be kept")
	}
	for _, dropped := range []string{"ungenerated", "listManager", "todoAccess"} {
		if _, ok := m.Contracts[dropped]; ok {
			t.Errorf("%s holds no hand-written code and no generated client package: it must be dropped", dropped)
		}
	}
}

// A nil tree is a fully built module: nothing is dropped, so Plan names every path.
func TestWireOnlyBuilt_NilTreeKeepsEverything(t *testing.T) {
	m := &projectmodel.Model{Contracts: map[string]*projectmodel.Contract{
		"listManager": {Layer: "Manager", GoPackage: "internal/manager/list"},
		"todoAccess":  {Layer: "ResourceAccess", GoPackage: "internal/resourceaccess/todo"},
	}}
	wireOnlyBuilt(m, nil, false)
	if len(m.Contracts) != 2 {
		t.Fatalf("a nil tree dropped contracts: %v", m.Contracts)
	}
}

// A hand-written file in cmd/server is read from the tree, like the generated Config's env check.
func TestHandServerSources_ReadsFromTheTree(t *testing.T) {
	tree := fstest.MapFS{
		"cmd/server/hooks.go":       &fstest.MapFile{Data: []byte("package main\n")},
		"cmd/server/main.gen.go":    &fstest.MapFile{Data: []byte("package main\n")},
		"cmd/server/hooks_test.go":  &fstest.MapFile{Data: []byte("package main\n")},
		"cmd/server/notes.markdown": &fstest.MapFile{Data: []byte("x")},
	}
	srcs, err := handServerSources(tree)
	if err != nil {
		t.Fatal(err)
	}
	if len(srcs) != 1 || srcs["cmd/server/hooks.go"] == nil {
		t.Fatalf("handServerSources = %v, want only cmd/server/hooks.go", srcs)
	}
	if srcs, err := handServerSources(nil); err != nil || srcs != nil {
		t.Fatalf("a nil tree has no hand sources, got %v, %v", srcs, err)
	}
}

// A Manager the generated subject leaves unbuilt is reported on the operator's stream, one line
// per refusal, never dropped.
func TestReportRefusals_NamesEveryUnbuiltManager(t *testing.T) {
	var b strings.Builder
	reportRefusals(&b, []composegen.Refusal{{Manager: "deliveryManager", Dependency: "repo", Reason: "policy"}, {Manager: "deliveryManager", Dependency: "repoBase", Reason: "policy"}})
	for _, want := range []string{"deliveryManager/repo: policy", "deliveryManager/repoBase: policy"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("report lacks %q:\n%s", want, b.String())
		}
	}
}
