package methodcheck

import (
	"encoding/json"
	"os"
	"path/filepath"
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

// A machine client's contract is derived from what it calls in the step an actor calls it in, so a
// prose request with no such call would reach construction with nothing to test. A label that names
// its own exported operation resolves without one.
func TestSysClientMachineCall(t *testing.T) {
	sys, _, _ := clientDesign(t)
	step := func(calls ...TraceCall) {
		sys.DynamicViews[0].Steps[0].Calls = calls
	}
	step(TraceCall{From: "user", To: "web-api", Mode: "sync", Label: "requests one list action"})
	if msg := ruleMessages(clientMachineCalls(sys), ruleSysClientMachineCall); !strings.Contains(msg, "calls no Manager in that step") {
		t.Errorf("a prose request with no call beside it must be refused: %q", msg)
	}
	step(TraceCall{From: "user", To: "web-api", Mode: "sync", Label: "requests one list action"},
		TraceCall{From: "web-api", To: "list-manager", Mode: "sync", Label: "keep(owner)"})
	if msg := ruleMessages(clientMachineCalls(sys), ruleSysClientMachineCall); msg != "" {
		t.Errorf("the call in the same step derives the contract: %q", msg)
	}
	step(TraceCall{From: "user", To: "web-api", Mode: "sync", Label: "ListKeep(owner) — opens the list"})
	if msg := ruleMessages(clientMachineCalls(sys), ruleSysClientMachineCall); msg != "" {
		t.Errorf("a label naming its operation resolves on its own: %q", msg)
	}
	step(TraceCall{From: "user", To: "web-app", Mode: "sync", Label: "requests one list action"})
	if msg := ruleMessages(clientMachineCalls(sys), ruleSysClientMachineCall); msg != "" {
		t.Errorf("a browser surface is another rule's: %q", msg)
	}
}

// The rule reaches the design gate, not only its own test: putDraftModel runs validateArchitecture.
func TestSysClientMachineCallIsInTheDesignGate(t *testing.T) {
	sys, cu, _ := clientDesign(t)
	sys.DynamicViews[0].Steps[0].Calls = []TraceCall{{From: "user", To: "web-api", Mode: "sync", Label: "requests one list action"}}
	result, err := validateArchitecture(sys, cu)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ruleMessages(result.Findings, ruleSysClientMachineCall), "calls no Manager") {
		t.Errorf("validateArchitecture does not run the rule: %+v", result.Findings)
	}
}

// An app has one shared generated REST server, so a second REST client has nothing to be.
func TestSysClientProtocolRefusesASecondREST(t *testing.T) {
	sys, _, _ := clientDesign(t)
	sys.Components[2].MachineProtocol = machineProtocolREST
	if msg := ruleMessages(clientProtocolStated(sys), ruleSysClientProtocol); !strings.Contains(msg, "second REST client") {
		t.Errorf("got %q", msg)
	}
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
	surface := false
	s := System{Components: []Component{
		comp(t, "OrderManager", kindManager),
		{ID: "web-api", Name: "WebApi", Kind: kindClient, UISurface: &surface, MachineProtocol: machineProtocolREST},
		{ID: "mcp-server", Name: "McpServer", Kind: kindClient, UISurface: &surface, MachineProtocol: machineProtocolMCP},
	}}
	pkgs := []classifiedPackage{cpkg("ex/manager/order", "order", "Manager")}
	var missing []string
	for _, f := range alignSystemToCode(s, pkgs, StereotypeSuffixNormalizer, nil, nil, nil) {
		missing = append(missing, f.Message)
	}
	got := strings.Join(missing, "\n")
	if strings.Contains(got, "WebApi") {
		t.Errorf("the REST client owes no package: %s", got)
	}
	if !strings.Contains(got, "McpServer") {
		t.Errorf("an MCP client owes its own package: %q", got)
	}
}

// run010Project is RUN-010's real design (testdata/fresh/run010.design.json: its core use cases,
// its System and the contracts it had recorded) with its Manager's contract recorded, and the two
// machine clients' contracts as the recorder derives them from it.
func run010Project(t *testing.T, managerOps ...string) Project {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "fresh", "run010.design.json"))
	if err != nil {
		t.Fatal(err)
	}
	var p Project
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	if _, ok := p.ServiceContracts["listAccess"]; !ok {
		t.Fatal("the fixture lost RUN-010's own contract")
	}
	op := func(names []string, prefix string) string {
		parts := make([]string, len(names))
		for i, n := range names {
			parts[i] = `{"name":"` + prefix + n + `"}`
		}
		return strings.Join(parts, ",")
	}
	return withContracts(t, p, `{
	"listManager":{"component":"listManager","layer":"Manager","interface":{"name":"ListManager","layer":"manager","operations":[`+op(managerOps, "")+`]}},
	"listApi":{"component":"listApi","layer":"Client","interface":{"name":"ListApiList","layer":"client","operations":[`+op(managerOps, "List")+`]}},
	"listMcpServer":{"component":"listMcpServer","layer":"Client","interface":{"name":"ListMcpServerList","layer":"client","operations":[`+op(managerOps, "List")+`]}}}`)
}

// RUN-010's "|" labels into list-api and list-mcp-server name the Manager's operations; once its
// contract is recorded they resolve through it onto the clients' derived ones, so TP-LABEL-OP has
// nothing to say. Before, it raised 24 errors and every later task's arch gate went red.
func TestTPLabelOpRun010(t *testing.T) {
	ops := []string{"AddTodo", "RewordTodo", "DiscardTodo", "SetTodoDone", "ToggleAll", "ClearCompleted", "ReviewList"}
	p := run010Project(t, ops...)
	if msg := ruleMessages(clientContractFindings(p), ruleTPLabelOp); msg != "" {
		t.Errorf("RUN-010's labels resolve through the Manager the clients front:\n%s", msg)
	}
	p = run010Project(t, ops[:6]...)
	msg := ruleMessages(clientContractFindings(p), ruleTPLabelOp)
	for _, client := range []string{"list-api", "list-mcp-server"} {
		if !strings.Contains(msg, `agent-tooling→`+client) {
			t.Errorf("a single-op label naming no op into %s is refused, got:\n%s", client, msg)
		}
	}
	if !strings.Contains(msg, `"ReviewList"`) {
		t.Errorf("the unresolved name is named: %s", msg)
	}
}

// A bare operation of two Managers one client fronts is refused as ambiguous, naming the prefixed
// forms.
func TestTPLabelOpAmbiguous(t *testing.T) {
	_, _, p := clientDesign(t)
	p = withContracts(t, p, `{
	"tagManager":{"component":"tagManager","layer":"Manager","interface":{"name":"TagManager","layer":"manager","operations":[{"name":"Keep"}]}},
	"mcpServer":{"component":"mcpServer","layer":"Client","interface":{"name":"McpServerList","layer":"client","operations":[{"name":"ListKeep"},{"name":"TagKeep"}]}}}`)
	sys, _, _ := p.system()
	sys.Components = append(sys.Components, Component{ID: "tag-manager", Name: "TagManager", Kind: kindManager})
	sys.DynamicViews[0].Steps[0].Calls = append(sys.DynamicViews[0].Steps[0].Calls,
		TraceCall{From: "user", To: "mcp-server", Mode: "sync", Label: "Keep — keeps one"},
		TraceCall{From: "mcp-server", To: "list-manager", Mode: "sync", Label: "keep(owner)"},
		TraceCall{From: "mcp-server", To: "tag-manager", Mode: "sync", Label: "keep(owner)"})
	raw, err := json.Marshal(sys)
	if err != nil {
		t.Fatal(err)
	}
	slot := p.Slots["5"]
	slot.Model = raw
	p.Slots["5"] = slot
	msg := ruleMessages(clientContractFindings(p), ruleTPLabelOp)
	if !strings.Contains(msg, "more than one Manager") || !strings.Contains(msg, "[ListKeep TagKeep]") {
		t.Errorf("ambiguous bare op, got %q", msg)
	}
}
