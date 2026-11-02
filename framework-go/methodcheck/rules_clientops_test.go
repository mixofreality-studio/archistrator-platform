package methodcheck

import (
	"encoding/json"
	"strings"
	"testing"
)

// clientDesign is a fresh design: a browser surface and two machine clients over one Manager.
func clientDesign(t *testing.T) (System, CoreUseCases, Project) {
	t.Helper()
	const raw = `{"id":"p","version":1,"slots":{
	"4":{"kind":4,"status":2,"model":{"decisions":[{"useCase":{"id":"keep","name":"Keep","actors":[{"id":"user","name":"User"}],
	  "activity":{"nodes":[{"id":"a","kind":"action","label":"act","linkedActorId":"user"}],"edges":[]}}}]}},
	"5":{"kind":5,"status":2,"model":{
	  "components":[
	    {"id":"web-app","name":"WebApp","kind":"client","layer":"client","uiSurface":true},
	    {"id":"web-api","name":"WebApi","kind":"client","layer":"client","uiSurface":false,"machineProtocol":"rest"},
	    {"id":"mcp-server","name":"McpServer","kind":"client","layer":"client","uiSurface":false,"machineProtocol":"mcp"},
	    {"id":"list-manager","name":"ListManager","kind":"manager","layer":"manager"}],
	  "relationships":[{"from":"web-app","to":"list-manager","mode":"sync"}],
	  "dynamicViews":[{"useCaseId":"keep","key":"keep","steps":[{"activityNodeId":"a","calls":[
	    {"from":"user","to":"web-app","mode":"sync","label":"AddTodo | RewordTodo(text) — performs one list action"},
	    {"from":"web-app","to":"list-manager","mode":"sync","label":"Keep(owner) → done | refused"}]}]}]}}},
	"serviceContracts":{
	  "listManager":{"component":"listManager","layer":"Manager","interface":{"name":"ListManager","layer":"manager","operations":[{"name":"Keep"}]}}}}`
	var p Project
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	sys, ok, err := p.system()
	if err != nil || !ok {
		t.Fatalf("no system: %v", err)
	}
	cu, ok, err := p.coreUseCases()
	if err != nil || !ok {
		t.Fatalf("no use cases: %v", err)
	}
	return sys, cu, p
}

func ruleMessages(out []Finding, id RuleID) string {
	var msgs []string
	for _, f := range out {
		if f.RuleID == id {
			msgs = append(msgs, f.Message)
		}
	}
	return strings.Join(msgs, "\n")
}

func TestSysClientOpLabel(t *testing.T) {
	sys, cu, _ := clientDesign(t)
	if got := clientOpLabels(sys, allActorIDs(cu)); len(got) != 0 {
		t.Fatalf("a label leading with exported ops must pass: %+v", got)
	}
	for _, bad := range []string{"requests one list action", "addTodo", "AddTodo now", "AddTodo | rewordTodo — x", "Add-Todo"} {
		sys.DynamicViews[0].Steps[0].Calls[0].Label = bad
		if msg := ruleMessages(clientOpLabels(sys, allActorIDs(cu)), ruleSysClientOpLabel); !strings.Contains(msg, "must lead with the exported operations") {
			t.Errorf("%q must be refused, got %q", bad, msg)
		}
	}
	// A component's call into another component and a call to a machine client are untouched.
	sys, cu, _ = clientDesign(t)
	sys.DynamicViews[0].Steps[0].Calls = append(sys.DynamicViews[0].Steps[0].Calls,
		TraceCall{From: "user", To: "web-api", Mode: modeSync, Label: "requests one list action"})
	if got := clientOpLabels(sys, allActorIDs(cu)); len(got) != 0 {
		t.Errorf("only a call into a browser surface is held to this: %+v", got)
	}
}

func TestSysClientOpLabelRefusesTwoSpellings(t *testing.T) {
	sys, cu, _ := clientDesign(t)
	sys.DynamicViews[0].Steps[0].Calls[0].Label = "AddTodo | Addtodo — two spellings"
	if msg := ruleMessages(clientOpLabels(sys, allActorIDs(cu)), ruleSysClientOpLabel); !strings.Contains(msg, "two ways") {
		t.Errorf("two spellings of one op must be refused, got %q", msg)
	}
}

func TestSysClientProtocol(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Component)
		want string
	}{
		{"a built machine client must state it", func(c *Component) { c.MachineProtocol = "" }, "does not state machineProtocol"},
		{"only rest or mcp", func(c *Component) { c.MachineProtocol = "grpc" }, "must be"},
		{"illegal on a browser surface", func(c *Component) { c.UISurface = boolPtr(true) }, "only a machine client"},
		{"illegal on a Manager", func(c *Component) { c.Kind = kindManager }, "only a machine client"},
		{"a planned client is exempt", func(c *Component) { c.MachineProtocol = ""; c.BuildStatus = buildStatusPlanned }, ""},
		{"an external client is exempt", func(c *Component) { c.MachineProtocol = ""; c.BuildStatus = buildStatusExternal }, ""},
		{"a stated one passes", func(*Component) {}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sys, _, _ := clientDesign(t)
			tc.edit(&sys.Components[1])
			msg := ruleMessages(clientProtocolStated(sys), ruleSysClientProtocol)
			if (tc.want == "") != (msg == "") || !strings.Contains(msg, tc.want) {
				t.Errorf("got %q, want %q", msg, tc.want)
			}
		})
	}
}

func withContracts(t *testing.T, p Project, contracts string) Project {
	t.Helper()
	var add map[string]ServiceContract
	if err := json.Unmarshal([]byte(contracts), &add); err != nil {
		t.Fatal(err)
	}
	for k, c := range add {
		p.ServiceContracts[k] = c
	}
	return p
}

func TestTPClientOps(t *testing.T) {
	ops := func(names ...string) string {
		parts := make([]string, len(names))
		for i, n := range names {
			parts[i] = `{"name":"` + n + `"}`
		}
		return strings.Join(parts, ",")
	}
	contract := func(iface, component, names string) string {
		return `{"component":"` + component + `","layer":"Client","interface":{"name":"` + iface + `","layer":"client","operations":[` + names + `]}}`
	}
	for _, tc := range []struct{ name, contracts, want string }{
		{"no contract yet is silent", `{}`, ""},
		{"exactly the derived set", `{"webApp":` + contract("WebApp", "webApp", ops("RewordTodo", "AddTodo")) + `}`, ""},
		{"split across facets", `{"webApp":` + contract("WebApp", "webApp", ops("AddTodo")) + `,"webAppB":` + contract("WebAppB", "webApp", ops("RewordTodo")) + `}`, ""},
		{"one missing", `{"webApp":` + contract("WebApp", "webApp", ops("AddTodo")) + `}`, "derive [AddTodo RewordTodo]"},
		{"one extra", `{"webApp":` + contract("WebApp", "webApp", ops("AddTodo", "RewordTodo", "Other")) + `}`, "Other"},
		{"an empty contract", `{"webApp":` + contract("WebApp", "webApp", "") + `}`, "derive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, p := clientDesign(t)
			p = withContracts(t, p, tc.contracts)
			msg := ruleMessages(clientContractFindings(p), ruleTPClientOps)
			if (tc.want == "") != (msg == "") || !strings.Contains(msg, tc.want) {
				t.Errorf("got %q, want %q", msg, tc.want)
			}
		})
	}
}

func TestTPLabelOp(t *testing.T) {
	_, _, p := clientDesign(t)
	if msg := ruleMessages(clientContractFindings(p), ruleTPLabelOp); msg != "" {
		t.Fatalf("a | after the arrow is an outcome, not an alternative: %s", msg)
	}
	sys, _, _ := p.system()
	sys.DynamicViews[0].Steps[0].Calls[1].Label = "Keep | Discard(owner) — does one"
	raw, err := json.Marshal(sys)
	if err != nil {
		t.Fatal(err)
	}
	slot := p.Slots["5"]
	slot.Model = raw
	p.Slots["5"] = slot
	if msg := ruleMessages(clientContractFindings(p), ruleTPLabelOp); !strings.Contains(msg, `"Discard(owner)"`) {
		t.Errorf("an alternative naming no op of a contracted callee must be refused, got %q", msg)
	}
}

func TestClientMachineMirror(t *testing.T) {
	mirror := func(key, component, ops string) string {
		return `{"` + key + `":{"component":"` + component + `","layer":"Client","interface":{"name":"M","layer":"client","operations":[` + ops + `]}}}`
	}
	for _, tc := range []struct{ name, contracts, want string }{
		{"not yet derived", `{}`, ""},
		{"an MCP client's op is its Manager's", mirror("mcpServer", "mcpServer", `{"name":"ListKeep"}`), ""},
		{"a REST client's too", mirror("webApi", "webApi", `{"name":"ListKeep"}`), ""},
		{"an op that is no Manager's", mirror("mcpServer", "mcpServer", `{"name":"Other"}`), "none of its Manager's"},
		{"an empty one", mirror("mcpServer", "mcpServer", ""), "no operations"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, p := clientDesign(t)
			p = withContracts(t, p, tc.contracts)
			msg := ruleMessages(clientContractFindings(p), ruleClientMachineMirror)
			if (tc.want == "") != (msg == "") || !strings.Contains(msg, tc.want) {
				t.Errorf("got %q, want %q", msg, tc.want)
			}
		})
	}
}

func TestCCEdgeRealized(t *testing.T) {
	logging := Component{ID: "logging", Name: "Logging", Kind: kindUtility}
	bus := Component{ID: "pub-sub", Name: "PubSub", Kind: kindUtility}
	for _, tc := range []struct {
		name string
		rel  Relationship
		want string
	}{
		{"an uncalled edge to a Utility is refused", Relationship{From: "list-manager", To: "pub-sub", Mode: modeSync}, "no dynamic view calls"},
		{"ambient lets it stand", Relationship{From: "list-manager", To: "logging", Mode: modeSync, Ambient: true}, ""},
		{"a pub/sub edge no view calls is refused", Relationship{From: "list-manager", To: "web-api", Mode: "pubsub"}, "no dynamic view calls"},
		{"ambient is illegal on anything but a Utility", Relationship{From: "web-app", To: "list-manager", Mode: modeSync, Ambient: true}, "legal only"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sys, _, _ := clientDesign(t)
			sys.Components = append(sys.Components, logging, bus)
			if tc.rel.From == "web-app" {
				sys.Relationships[0] = tc.rel
			} else {
				sys.Relationships = append(sys.Relationships, tc.rel)
			}
			msg := ruleMessages(checkRelationshipCoverage(sys, componentIndex(sys)), ruleCCEdgeRealized)
			if (tc.want == "") != (msg == "") || !strings.Contains(msg, tc.want) {
				t.Errorf("got %q, want %q", msg, tc.want)
			}
		})
	}
}

// A REST client is the shared transport, so finishing it owes no package; an MCP one owes its own.
func TestRESTClientOwesNoPackage(t *testing.T) {
	rest := Component{Kind: kindClient, MachineProtocol: machineProtocolREST}
	mcp := Component{Kind: kindClient, MachineProtocol: machineProtocolMCP}
	if !rest.isRESTClient() || mcp.isRESTClient() {
		t.Error("only machineProtocol rest is the shared transport")
	}
}
