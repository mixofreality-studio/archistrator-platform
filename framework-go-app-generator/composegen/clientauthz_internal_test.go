package composegen

import (
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/internal/testfixtures"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// The authorization rows of the requests a machine client's generated handlers send are the
// composition root's own (Plan 3 Task 3.4 generates the policy from them): every row the clients
// reach is a row authz.gen.go carries, and the todomvc fixture's REST and MCP clients, which front
// one Manager, reach all of its operations once.
func TestClientAuthzRows_AreTheCompositionRootsRows(t *testing.T) {
	cfg := Config{ContainerKey: "server", ModulePath: "example.com/todomvc"}
	m, err := projectmodel.Load(testfixtures.Fixture(t, "todomvc"))
	if err != nil {
		t.Fatal(err)
	}
	effective := *m
	effective.Deployment = m.EffectiveDeployment(cfg.ContainerKey)

	r := newResolved(&effective, cfg)
	r.assignAliases(scenarioPackages(&effective))
	if err := r.resolveRAs(&effective); err != nil {
		t.Fatal(err)
	}
	g := &scenarioGen{m: &effective, r: r, imp: newImportSet(), byKey: map[string]scenarioComp{}, refused: map[string]Refusal{}}
	if err := g.classify(); err != nil {
		t.Fatal(err)
	}
	clients := g.classifyClients()
	if len(clients) < 2 {
		t.Fatalf("the fixture's REST and MCP clients are not classified: %+v", clients)
	}
	rows, err := g.clientAuthzRows()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("the clients' requests have no authorization rows")
	}
	root := map[authzRow]bool{}
	all, err := managerAuthzRows(managerComp{key: "listManager", iface: "ListManager", doc: m.Contracts["listManager"].Doc}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range all {
		root[row] = true
	}
	seen := map[authzRow]bool{}
	for _, row := range rows {
		if !root[row] {
			t.Errorf("client row %+v is not a row of the Manager's own derivation", row)
		}
		if seen[row] {
			t.Errorf("client row %+v is listed twice: two clients of one Manager share its rows", row)
		}
		seen[row] = true
	}
}
