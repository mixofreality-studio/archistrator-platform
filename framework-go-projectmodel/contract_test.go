package projectmodel

import (
	"encoding/json"
	"os"
	"testing"
)

func loadFixtureContracts(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile("testdata/archistrator.project.json")
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	var sc map[string]json.RawMessage
	if err := json.Unmarshal(top["serviceContracts"], &sc); err != nil {
		t.Fatal(err)
	}
	return sc
}

func TestParseContractManager(t *testing.T) {
	sc := loadFixtureContracts(t)
	c, err := ParseContract("billingManager", sc["billingManager"])
	if err != nil {
		t.Fatal(err)
	}
	if c.Key != "billingManager" || c.Layer != "Manager" || c.GoPackage != "internal/manager/billing" {
		t.Fatalf("metadata: %+v", c)
	}
	if c.Doc == nil || c.Doc.Interface.Name == "" {
		t.Fatal("contract doc not parsed")
	}
	// deps: 1 plain (client) + 6 component deps, order preserved
	if len(c.Deps) != 7 {
		t.Fatalf("deps: got %d", len(c.Deps))
	}
	if c.Deps[0].Name != "client" || c.Deps[0].GoType != "client.Client" || c.Deps[0].GoImport != "go.temporal.io/sdk/client" {
		t.Fatalf("plain dep: %+v", c.Deps[0])
	}
	if c.Deps[1].Name != "billingState" || c.Deps[1].Component != "billingStateAccess" {
		t.Fatalf("component dep: %+v", c.Deps[1])
	}
}

func TestParseContractEngineHasNoDeps(t *testing.T) {
	sc := loadFixtureContracts(t)
	c, err := ParseContract("estimationEngine", sc["estimationEngine"])
	if err != nil {
		t.Fatal(err)
	}
	if c.Layer != "Engine" || len(c.Deps) != 0 {
		t.Fatalf("engine: %+v", c)
	}
}

func TestParseContractTolerantUnknownFields(t *testing.T) {
	raw := json.RawMessage(`{"component":"x","layer":"Engine","goPackage":"internal/engine/x",
		"someFutureField":{"a":1},
		"title":"x contract","$defs":{},"interface":{"name":"XEngine","layer":"Engine","operations":[]}}`)
	if _, err := ParseContract("x", raw); err != nil {
		t.Fatalf("unknown fields must be ignored: %v", err)
	}
}
