package clientgen

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"

	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// opNames is the interface.operations[].name list of a contract entry.
func opNames(t *testing.T, entry json.RawMessage) []string {
	t.Helper()
	var doc struct {
		Interface struct {
			Operations []struct {
				Name string `json:"name"`
			} `json:"operations"`
		} `json:"interface"`
	}
	if err := json.Unmarshal(entry, &doc); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, op := range doc.Interface.Operations {
		out = append(out, op.Name)
	}
	return out
}

// A manager exposed with an op list carries only those ops onto the client layer,
// in contract order; every other member of the entry is kept verbatim.
func TestExposedContract_KeepsOnlyListedOps(t *testing.T) {
	entry := json.RawMessage(`{"component":"billingManager","goPackage":"internal/manager/billing","$defs":{"X":{"type":"string"}},` +
		`"interface":{"name":"BillingManager","operations":[{"name":"CloseBillingCycle","params":[]},{"name":"GetBillingCycle","params":[{"name":"cycleID"}]},{"name":"RunShortfallSweep"}]}}`)
	got, err := ExposedContract(entry, []string{"GetBillingCycle"})
	if err != nil {
		t.Fatal(err)
	}
	if names := opNames(t, got); !slices.Equal(names, []string{"GetBillingCycle"}) {
		t.Fatalf("exposed ops = %v, want [GetBillingCycle]", names)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"component", "goPackage"} {
		if _, ok := doc[k]; !ok {
			t.Errorf("member %q dropped", k)
		}
	}
	if !strings.Contains(string(got), `"params":[{"name":"cycleID"}]`) {
		t.Errorf("the kept op is not verbatim: %s", got)
	}
}

// The $defs an exposed contract keeps are those its kept ops reach, transitively:
// a type only a hidden op uses never reaches the OpenAPI document.
func TestExposedContract_PrunesDefsToTheKeptOps(t *testing.T) {
	entry := json.RawMessage(`{"$defs":{` +
		`"View":{"type":"object","properties":{"r":{"$ref":"#/$defs/Routing"}}},` +
		`"Routing":{"type":"integer"},` +
		`"CloseResult":{"type":"object","properties":{"r":{"$ref":"#/$defs/Routing"}}},` +
		`"Id":{"type":"string"}},` +
		`"interface":{"name":"M","operations":[` +
		`{"name":"Close","result":{"$ref":"#/$defs/CloseResult"}},` +
		`{"name":"Get","params":[{"name":"id","schema":{"$ref":"#/$defs/Id"}}],"result":{"$ref":"#/$defs/View"}}]}}`)
	got, err := ExposedContract(entry, []string{"Get"})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for k := range doc.Defs {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"Id", "Routing", "View"}) {
		t.Fatalf("kept $defs = %v, want [Id Routing View]", keys)
	}
}

// nil exposes the whole interface, unchanged.
func TestExposedContract_NilKeepsEveryOp(t *testing.T) {
	entry := json.RawMessage(`{"interface":{"name":"M","operations":[{"name":"A"},{"name":"B"}]}}`)
	got, err := ExposedContract(entry, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(entry) {
		t.Fatalf("nil op list rewrote the entry: %s", got)
	}
}

// A listed op the contract does not declare is an error, not a silently smaller
// surface.
func TestExposedContract_UnknownOpFails(t *testing.T) {
	entry := json.RawMessage(`{"interface":{"name":"M","operations":[{"name":"A"}]}}`)
	if _, err := ExposedContract(entry, []string{"A", "Nope"}); err == nil || !strings.Contains(err.Error(), "Nope") {
		t.Fatalf("want an error naming Nope, got %v", err)
	}
}

const appContract = `{"component":"listManager","layer":"Manager","goPackage":"internal/manager/list","$defs":{"Name":{"type":"string"}},
"interface":{"name":"ListManager","layer":"manager","operations":[{"name":"RecallList","params":[{"name":"owner","schema":{"$ref":"#/$defs/Name"}}],"result":{"$ref":"#/$defs/Name"},"error":true}]}}`

func boolPtr(b bool) *bool { return &b }

func appProject() Project {
	return Project{
		Contracts: map[string]json.RawMessage{"listManager": json.RawMessage(appContract)},
		System: &projectmodel.System{
			Components: []projectmodel.SystemComponent{
				{ID: "web-app", Name: "WebApp", Kind: "client", UISurface: boolPtr(true)},
				{ID: "machine-api", Name: "MachineApi", Kind: "client", UISurface: boolPtr(false)},
				{ID: "later", Name: "LaterClient", Kind: "client", UISurface: boolPtr(false), BuildStatus: "planned"},
				{ID: "list-manager", Name: "ListManager", Kind: "manager", ContractKey: "listManager"},
			},
			Relationships: []projectmodel.Relationship{
				{From: "web-app", To: "list-manager"}, {From: "machine-api", To: "list-manager"}, {From: "later", To: "list-manager"},
			},
		},
	}
}

// A fresh app has no knobs: the System decides. Browser surfaces share the REST transport, a
// machine client gets its own package, and a planned client gets nothing.
func TestGenerateDerivesTheClientLayerFromTheSystem(t *testing.T) {
	files, err := Generate(appProject(), Config{ModulePath: "example.com/todomvc"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"internal/client/web/server.gen.go",
		"internal/client/web/middleware.gen.go",
		"internal/client/web/list/list_handlers.gen.go",
		"internal/client/machineapi/list/list_tools.gen.go",
		"api/openapi.yaml",
	} {
		if _, ok := files[want]; !ok {
			t.Errorf("no %s; got %v", want, slices.Sorted(maps.Keys(files)))
		}
	}
	for p := range files {
		if strings.Contains(p, "later") || strings.Contains(p, "webapp") {
			t.Errorf("%s must not be generated", p)
		}
	}
	if !strings.Contains(string(files["internal/client/machineapi/list/list_tools.gen.go"]), "Recall list on the ListManager.") {
		t.Error("a contract with no prose gets a description made from the operation's name")
	}
	if !strings.Contains(string(files["api/openapi.yaml"]), "title: todomvc API") {
		t.Errorf("title: %s", files["api/openapi.yaml"])
	}
}

// An unbuilt Manager is simply absent: nothing is generated that would not compile.
func TestGenerateNothingBeforeAManagerIsBuilt(t *testing.T) {
	p := appProject()
	p.Contracts = map[string]json.RawMessage{}
	files, err := Generate(p, Config{ModulePath: "example.com/todomvc"})
	if err != nil || len(files) != 0 {
		t.Fatalf("got %v, %v", files, err)
	}
}

// A Manager listed in the prose knob must document every operation.
func TestOpDocsKnobIsStrict(t *testing.T) {
	_, err := Generate(appProject(), Config{ModulePath: "example.com/todomvc", OpDocs: map[string]map[string]string{"ListManager": {}}})
	if err == nil || !strings.Contains(err.Error(), "RecallList") {
		t.Fatalf("got %v", err)
	}
}
