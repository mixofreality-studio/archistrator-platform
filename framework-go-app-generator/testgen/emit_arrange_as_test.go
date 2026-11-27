package testgen

import (
	"encoding/json"
	"strings"
	"testing"
)

// emit_arrange_as_test.go pins who an arrange contract call runs as (TP-ARRANGE): a call into a
// Manager carries the plan principal it is made as, a call into a layer that reads no principal
// carries none, and every dialect spells that principal the way its steps and probes do.

// arrangeCall is a contractCall arrange of orderManager.GetOrder, as `as` when it is not empty.
func arrangeCall(component, operation, as string, inputs []any) map[string]any {
	model := map[string]any{
		"component": component, "operation": operation, "inputs": inputs,
		"expect": map[string]any{"errorExpected": false},
	}
	if as != "" {
		model["as"] = as
	}
	return map[string]any{"kind": "contractCall", "model": model}
}

func getOrderInputs() []any {
	return []any{map[string]any{"name": "orderId", "value": lit("o-2")}}
}

func TestArrange_ManagerCallRunsAsItsPrincipal(t *testing.T) {
	out := generateMutated(t, func(doc map[string]any) {
		planBindingNoT(doc, "billingManager", "process-order-P2")["arrange"] = []any{arrangeCall("orderManager", "GetOrder", "customer", getOrderInputs())}
	})
	src := string(out.Generated[billingGen])
	requireContains(t, "billing scenarios", src,
		`scenariocompose.NewOrderManager(t, h).GetOrder(h.ManagerContextAs(t, principal("customer")), in.OrderId)`,
		"func principal(", // usesAs: the helper and the table are emitted
	)
}

func TestArrange_ManagerCallWithoutAsRunsAnonymously(t *testing.T) {
	out := generateMutated(t, func(doc map[string]any) {
		planBindingNoT(doc, "billingManager", "process-order-P2")["arrange"] = []any{arrangeCall("orderManager", "GetOrder", "", getOrderInputs())}
	})
	src := string(out.Generated[billingGen])
	if !strings.Contains(src, "NewOrderManager(t, h).GetOrder(h.ManagerContext(t), in.OrderId)") {
		t.Errorf("a call that names no principal is anonymous:\n%s", src)
	}
}

// A ResourceAccess reads no principal: its arrange call is made on the anonymous call context.
func TestArrange_ResourceAccessCallCarriesNoPrincipal(t *testing.T) {
	out := generateMutated(t, func(doc map[string]any) {
		design := doc["slots"].(map[string]any)["5"].(map[string]any)["model"].(map[string]any)
		design["components"] = append(design["components"].([]any), map[string]any{
			"id": "ledger-access", "name": "LedgerAccess", "kind": "resourceAccess", "layer": "resourceAccess", "encapsulates": "", "atomicBusinessVerbs": nil,
		})
		doc["serviceContracts"].(map[string]any)["ledgerAccess"] = map[string]any{
			"component": "ledgerAccess", "layer": "ResourceAccess", "title": "ledger", "goPackage": "internal/resourceaccess/ledger",
			"$defs": map[string]any{},
			"interface": map[string]any{"name": "LedgerAccess", "layer": "resourceAccess", "operations": []any{
				map[string]any{"name": "Entries", "params": []any{}, "result": map[string]any{"type": "string"}, "readOnly": true, "error": true},
			}},
		}
		planBindingNoT(doc, "billingManager", "process-order-P2")["arrange"] = []any{arrangeCall("ledgerAccess", "Entries", "", []any{})}
	})
	src := string(out.Generated[billingGen])
	if !strings.Contains(src, `h.ResourceAccessContext(t, "arrange-1")`) || strings.Contains(src, "ResourceAccessContextAs") {
		t.Errorf("a ResourceAccess arrange runs on ResourceAccessContext with no principal:\n%s", src)
	}
}

// A web plan's arrange call is made as the principal's subject, the X-Dev-Principal the launch allowlists.
func TestArrange_PlaywrightLiteralCarriesAs(t *testing.T) {
	doc := shopDoc(t)
	shopBinding(doc)["arrange"] = []any{arrangeCall("orderManager", "GetOrder", "customer", getOrderInputs())}
	out := mustGenerate(t, doc)
	spec := string(out.Generated[shopSpec])
	subject := doc["phaseArtifacts"].(map[string]any)["testPlan"].(map[string]any)["shopClient"].(map[string]any)["principals"].(map[string]any)["customer"].(map[string]any)["subject"]
	want, _ := json.Marshal(subject)
	requireContains(t, "spec", spec, "await arrange(testInfo, { kind: 'contractCall',", "as: '"+strings.Trim(string(want), `"`)+"' }")
}

// A machine client's arrange runs as the principal it names, on a launch that allowlists it.
func TestArrange_MachineClientRunsAsThatPrincipal(t *testing.T) {
	doc := fixtureDoc(t)
	withShopTools(t, doc)
	plan := doc["phaseArtifacts"].(map[string]any)["testPlan"].(map[string]any)["shopTools"].(map[string]any)
	plan["bindings"].([]any)[0].(map[string]any)["arrange"] = []any{arrangeCall("orderManager", "GetOrder", "customer", getOrderInputs())}
	out, err := generateDoc(t, doc)
	if err != nil {
		t.Fatal(err)
	}
	requireContains(t, "tools scenarios", string(out.Generated[toolsGen]),
		`app := h.LaunchDevstack(t, "", principal("customer"))`,
		`scenariocompose.NewOrderManager(t, h).GetOrder(h.ManagerContextAs(t, principal("customer")), in.OrderId)`,
	)
}

// An acceptance spec resolves an arrange call's principal as it resolves a step's or a probe's: the
// subject the spec's principals table holds for the actor.
func TestArrange_AcceptanceSpecResolvesAsFromThePrincipals(t *testing.T) {
	raw := acceptanceDoc(t, func(plan map[string]any) {
		for _, b := range plan["bindings"].([]any) {
			bm := b.(map[string]any)
			if bm["scenario"] == accWebVariant {
				bm["arrange"] = []any{arrangeCall("listManager", "OpenList", accBrowserUser, []any{
					map[string]any{"name": "owner", "value": accLit("alice")},
					map[string]any{"name": "view", "value": accLit("all")},
				})}
			}
		}
	})
	out, err := Generate(raw, Config{ModulePath: accModulePath})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	requireContains(t, "acceptance spec", string(out.Generated[accWebSpec]),
		"await arrange(testInfo, { kind: 'contractCall',",
		"as: principals['"+accBrowserUser+"'] }",
		"const principals: Record<string, string>",
	)
}
