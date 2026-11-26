package methodcheck

import (
	"encoding/json"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
)

// SYS-RESOURCE-UNREAD and TP-OP-OBSERVABLE: a Resource that is written and never read, and
// the ResourceAccess op whose scenario expects outputs nothing can read back (RUN-032).

// unreadSystem is the RUN-032 shape: a Manager announces through a ResourceAccess into a
// Resource nothing reads.
func unreadSystem(t *testing.T, label string) (System, Component, Component, Component) {
	t.Helper()
	mgr := comp(t, "ListManager", kindManager)
	ra := comp(t, "ViewNoticeAccess", kindResourceAccess)
	res := comp(t, "ViewNoticeChannel", kindResource)
	s := System{
		Components: []Component{mgr, ra, res},
		Relationships: []Relationship{
			{From: mgr.ID, To: ra.ID, Mode: modeSync, Label: "announceListChanged(owner)"},
			{From: ra.ID, To: res.ID, Mode: modeSync},
		},
		DynamicViews: []DynamicView{{UseCaseID: "uc", Key: "uc1", Steps: []CallStep{{Calls: []TraceCall{
			{From: mgr.ID, To: ra.ID, Mode: modeSync, Label: label},
			{From: ra.ID, To: res.ID, Mode: modeSync},
		}}}}},
	}
	return s, mgr, ra, res
}

func TestSysResourceUnread(t *testing.T) {
	cases := []struct {
		name  string
		label string
		edit  func(s *System, mgr, ra, res Component)
		want  int
	}{
		{"RUN-032 shape fires", "announceListChanged(owner)", nil, 1},
		{"bare trailing arrow does not count", "announceListChanged(owner) →", nil, 1},
		{"arrow inside brackets does not count", "announceListChanged(owner → x)", nil, 1},
		{"store read label is clean", "recallList(owner) → todos", nil, 0},
		{"second call reads", "announceListChanged(owner)", func(s *System, mgr, ra, res Component) {
			s.DynamicViews[0].Steps[0].Calls = append(s.DynamicViews[0].Steps[0].Calls,
				TraceCall{From: mgr.ID, To: ra.ID, Mode: modeSync, Label: "pendingNotices(owner) → notices"})
		}, 0},
		{"relationship out of the resource is not a read", "announceListChanged(owner)", func(s *System, mgr, ra, res Component) {
			web := comp(t, "WebApp", kindClient)
			s.Components = append(s.Components, web)
			s.Relationships = append(s.Relationships, Relationship{From: res.ID, To: web.ID, Mode: modeEventPubSub})
		}, 1},
		{"client subscription into the resource is not a read", "announceListChanged(owner)", func(s *System, mgr, ra, res Component) {
			web := comp(t, "WebApp", kindClient)
			s.Components = append(s.Components, web)
			s.Relationships = append(s.Relationships, Relationship{From: web.ID, To: res.ID, Mode: modeEventPubSub})
		}, 1},
		{"encapsulates note does not discharge", "announceListChanged(owner)", func(s *System, mgr, ra, res Component) {
			s.Components[2].Encapsulates = "read later by the web app"
		}, 1},
		{"a read through a second RA over a different Resource does not discharge", "announceListChanged(owner)", func(s *System, mgr, ra, res Component) {
			// RUN-032: list-manager recalls through list-access into todos while
			// view-notice-channel is still never read.
			la := comp(t, "ListAccess", kindResourceAccess)
			todos := comp(t, "Todos", kindResource)
			s.Components = append(s.Components, la, todos)
			s.Relationships = append(s.Relationships, Relationship{From: mgr.ID, To: la.ID, Mode: modeSync, Label: "recallList(owner)"}, Relationship{From: la.ID, To: todos.ID, Mode: modeSync})
			s.DynamicViews[0].Steps[0].Calls = append(s.DynamicViews[0].Steps[0].Calls,
				TraceCall{From: mgr.ID, To: la.ID, Mode: modeSync, Label: "recallList(owner) → todos"},
				TraceCall{From: la.ID, To: todos.ID, Mode: modeSync})
		}, 1},
		{"no dynamic views is silent", "announceListChanged(owner)", func(s *System, mgr, ra, res Component) {
			s.DynamicViews = nil
		}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, mgr, ra, res := unreadSystem(t, tc.label)
			if tc.edit != nil {
				tc.edit(&s, mgr, ra, res)
			}
			fs := resourceUnread(s)
			wantRules(t, fs, ruleSysResourceUnread, tc.want)
			for _, f := range fs {
				if f.Severity != SeverityError {
					t.Errorf("severity = %v, want error", f.Severity)
				}
			}
		})
	}
}

// TestSysResourceUnreadReachesArchitectureGate pins the wiring: the architecture gate
// (validateArchitecture, which putDraftModel and the CI validate run) refuses the RUN-032
// shape with exactly one SYS-RESOURCE-UNREAD Error, and admits the same design once
// something reads the Resource.
func TestSysResourceUnreadReachesArchitectureGate(t *testing.T) {
	s, mgr, ra, _ := unreadSystem(t, "announceListChanged(owner)")
	res, err := validateArchitecture(s, CoreUseCases{})
	if err != nil {
		t.Fatal(err)
	}
	wantRules(t, res.Findings, ruleSysResourceUnread, 1)
	for _, f := range res.Findings {
		if f.RuleID == ruleSysResourceUnread && f.Severity != SeverityError {
			t.Errorf("severity = %v, want error", f.Severity)
		}
	}

	s.DynamicViews[0].Steps[0].Calls = append(s.DynamicViews[0].Steps[0].Calls,
		TraceCall{From: mgr.ID, To: ra.ID, Mode: modeSync, Label: "pendingNotices(owner) → notices"})
	res2, err := validateArchitecture(s, CoreUseCases{})
	if err != nil {
		t.Fatal(err)
	}
	wantRules(t, res2.Findings, ruleSysResourceUnread, 0)
}

// observableProject adds to tp_clean a ResourceAccess "orderAccess" over a Resource, called
// by orderManager in uc1's first step, so the stimulus into it carries the expected output.
func observableProject(t *testing.T, ops []ContractOperation, other bool, otherContract bool) Project {
	t.Helper()
	p := loadProject(t, "tp_clean.json")
	setSlotModel(t, p, kindSystem, func(m map[string]any) {
		comps := m["components"].([]any)
		add := func(id, name, kind, layer string) {
			comps = append(comps, map[string]any{"id": id, "name": name, "kind": kind, "layer": layer, "encapsulates": "x"})
		}
		add("order-access", "OrderAccess", kindResourceAccess, layerResourceAccess)
		add("order-store", "OrderStore", kindResource, "resource")
		rels := m["relationships"].([]any)
		rel := func(from, to string) {
			rels = append(rels, map[string]any{"from": from, "to": to, "mode": modeSync, "label": ""})
		}
		rel("order-manager", "order-access")
		rel("order-access", "order-store")
		if other {
			add("audit-access", "AuditAccess", kindResourceAccess, layerResourceAccess)
			rel("audit-access", "order-store")
		}
		m["components"], m["relationships"] = comps, rels
		for _, v := range m["dynamicViews"].([]any) {
			view := v.(map[string]any)
			if view["key"] != "uc1" {
				continue
			}
			step := view["steps"].([]any)[0].(map[string]any)
			step["calls"] = append(step["calls"].([]any), map[string]any{"from": "order-manager", "to": "order-access", "mode": modeSync, "label": "AnnounceOrder(owner)"})
		}
	})
	p.ServiceContracts["orderAccess"] = ServiceContract{
		Component: "orderAccess", Layer: "ResourceAccess",
		Interface: ContractInterface{Name: "OrderAccess", Layer: "ResourceAccess", Operations: ops},
	}
	if other && otherContract {
		p.ServiceContracts["auditAccess"] = ServiceContract{
			Component: "auditAccess", Layer: "ResourceAccess",
			Interface: ContractInterface{Name: "AuditAccess", Layer: "ResourceAccess", Operations: []ContractOperation{
				{Name: "Recall", Params: []ContractParam{}, Result: json.RawMessage(`{"type":"string"}`)},
			}},
		}
	}
	return p
}

var (
	voidAnnounce   = ContractOperation{Name: "AnnounceOrder", Params: []ContractParam{{Name: "owner", Schema: json.RawMessage(`{"type":"string"}`)}}}
	pendingNotices = ContractOperation{Name: "PendingNotices", Params: []ContractParam{}, Result: json.RawMessage(`{"type":"string"}`)}
)

func TestTP_OpObservable(t *testing.T) {
	// The projection must give the void op a stimulus that expects something, or the
	// cases below prove nothing.
	base := observableProject(t, []ContractOperation{voidAnnounce}, false, false)
	if fs := runTP(t, base); !hasRuleFindings(fs, ruleTPOpObservable) {
		t.Fatalf("void RA op with expected outputs must fire TP-OP-OBSERVABLE:\n%s", dumpFindings(fs))
	}
	cases := []struct {
		name  string
		build func() Project
		want  int
	}{
		{"only a void op", func() Project { return base }, 1},
		{"same contract has a result op", func() Project {
			return observableProject(t, []ContractOperation{voidAnnounce, pendingNotices}, false, false)
		}, 0},
		{"same contract has a readOnly op", func() Project {
			ro := ContractOperation{Name: "Peek", Params: []ContractParam{}, ReadOnly: true}
			return observableProject(t, []ContractOperation{voidAnnounce, ro}, false, false)
		}, 0},
		{"another RA over the Resource has a reader", func() Project {
			return observableProject(t, []ContractOperation{voidAnnounce}, true, true)
		}, 0},
		{"another RA is uncontracted: no prediction", func() Project {
			return observableProject(t, []ContractOperation{voidAnnounce}, true, false)
		}, 0},
		{"the op declares a result", func() Project {
			op := voidAnnounce
			op.Result = json.RawMessage(`{"type":"string"}`)
			return observableProject(t, []ContractOperation{op}, false, false)
		}, 0},
		{"a Manager with only void ops is out of scope", func() Project {
			p := loadProject(t, "tp_clean.json")
			sc := p.ServiceContracts["orderManager"]
			for i := range sc.Interface.Operations {
				sc.Interface.Operations[i].Result, sc.Interface.Operations[i].ReadOnly = nil, false
			}
			p.ServiceContracts["orderManager"] = sc
			return p
		}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantRules(t, runTP(t, tc.build()), ruleTPOpObservable, tc.want)
		})
	}
	t.Run("silent before the architecture is committed", func(t *testing.T) {
		p := observableProject(t, []ContractOperation{voidAnnounce}, false, false)
		uncommit(t, p, kindSystem)
		wantRules(t, runTP(t, p), ruleTPOpObservable, 0)
	})
}

// TestSysResourceUnreadTaughtFixesStayLegal pins that the two relationship shapes that once
// counted as reads are refused by the architecture gate as a whole, with the layer-rule
// Errors, and that SYS-RESOURCE-UNREAD still fires: no design that draws them is admitted.
func TestSysResourceUnreadTaughtFixesStayLegal(t *testing.T) {
	for _, mode := range []string{modeSync, modeEventPubSub} {
		for _, intoResource := range []bool{true, false} {
			s, _, _, res := unreadSystem(t, "announceListChanged(owner)")
			web := comp(t, "WebApp", kindClient)
			s.Components = append(s.Components, web)
			rel := Relationship{From: res.ID, To: web.ID, Mode: mode}
			if intoResource {
				rel = Relationship{From: web.ID, To: res.ID, Mode: mode}
			}
			s.Relationships = append(s.Relationships, rel)
			out, err := validateArchitecture(s, CoreUseCases{})
			if err != nil {
				t.Fatal(err)
			}
			wantRules(t, out.Findings, ruleSysResourceUnread, 1)
			errs := 0
			for _, f := range out.Findings {
				if f.Severity == SeverityError && f.RuleID != ruleSysResourceUnread {
					errs++
				}
			}
			if errs == 0 {
				t.Errorf("mode=%s intoResource=%v: the architecture gate admitted a Client/Resource edge", mode, intoResource)
			}
		}
	}
}

// TestUnobservedOpsNeedsExpectation pins the precondition that limits TP-OP-OBSERVABLE to
// the design-time twin of TP-STATE: a stimulus into a void op that expects no state and no
// output proves nothing is owed, so the op is not flagged.
func TestUnobservedOpsNeedsExpectation(t *testing.T) {
	sc := ServiceContract{Interface: ContractInterface{Name: "OrderAccess", Operations: []ContractOperation{voidAnnounce}}}
	stim := func(o scenario.Outcome) []scenario.Scenario {
		return []scenario.Scenario{{ID: "S1", Stimuli: []scenario.Stimulus{{Seq: 1, Input: scenario.InputEv{Kind: "call", Op: "AnnounceOrder"}, Outcome: o}}}}
	}
	cases := []struct {
		name string
		o    scenario.Outcome
		want int
	}{
		{"nothing expected is silent", scenario.Outcome{}, 0},
		{"hints alone are silent", scenario.Outcome{Hints: []string{"h"}}, 0},
		{"expected output fires", scenario.Outcome{ExpectedOutputs: []scenario.Output{{Kind: "sendSignal", Label: "x"}}}, 1},
		{"expected state fires", scenario.Outcome{ExpectedStates: []scenario.State{{}}}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := len(unobservedOps(sc, stim(tc.o))); got != tc.want {
				t.Errorf("unobservedOps = %d ops, want %d", got, tc.want)
			}
		})
	}
}
