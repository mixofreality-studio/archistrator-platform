package methodcheck

import (
	"encoding/json"
	"testing"
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
		{"relationship out of the resource", "announceListChanged(owner)", func(s *System, mgr, ra, res Component) {
			web := comp(t, "WebApp", kindClient)
			s.Components = append(s.Components, web)
			s.Relationships = append(s.Relationships, Relationship{From: res.ID, To: web.ID, Mode: modeEventPubSub})
		}, 0},
		{"client subscription into the resource", "announceListChanged(owner)", func(s *System, mgr, ra, res Component) {
			web := comp(t, "WebApp", kindClient)
			s.Components = append(s.Components, web)
			s.Relationships = append(s.Relationships, Relationship{From: web.ID, To: res.ID, Mode: modeEventPubSub})
		}, 0},
		{"encapsulates note does not discharge", "announceListChanged(owner)", func(s *System, mgr, ra, res Component) {
			s.Components[2].Encapsulates = "read later by the web app"
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
