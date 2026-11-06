package methodcheck

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func act(kind, model string) string { return `{"kind":"` + kind + `","model":` + model + `}` }

func tgt(el string) string { return `{"target":{"element":"` + el + `"}}` }

func TestUIDialect_Kinds(t *testing.T) {
	if got, want := UIActionKinds(), []string{"check", "click", "dblclick", "fill", "hover", "navigate", "press", "select", "uncheck"}; !slices.Equal(got, want) {
		t.Errorf("UIActionKinds = %v, want %v", got, want)
	}
	if got, want := UIObservableKinds(), []string{"absent", "checked", "count", "disabled", "enabled", "focused", "hidden", "text", "unchecked", "url", "value", "visible"}; !slices.Equal(got, want) {
		t.Errorf("UIObservableKinds = %v, want %v", got, want)
	}
	if got := len(UIRoles()); got != 21 {
		t.Errorf("UIRoles has %d roles, want 21", got)
	}
}

func TestUIDialect_UnmarshalUIActionEveryVariant(t *testing.T) {
	for kind, model := range map[string]string{
		"navigate": `{"route":"/active"}`,
		"click":    tgt("todo-add"),
		"dblclick": tgt("todo-title"),
		"fill":     `{"target":{"element":"todo-new","nth":1},"value":"milk"}`,
		"press":    `{"target":{"element":"todo-edit"},"key":"Enter"}`,
		"check":    tgt("todo-toggle"),
		"uncheck":  tgt("todo-toggle"),
		"hover":    tgt("todo-row"),
		"select":   `{"target":{"element":"todo-sort"},"option":"Newest"}`,
	} {
		raw := act(kind, model)
		a, err := UnmarshalUIAction([]byte(raw))
		if err != nil {
			t.Errorf("%s: %v", raw, err)
			continue
		}
		if string(a.Kind()) != kind {
			t.Errorf("%s decoded as %s", raw, a.Kind())
		}
		out, err := json.Marshal(a)
		if err != nil {
			t.Fatal(err)
		}
		again, err := UnmarshalUIAction(out)
		if err != nil || again.Kind() != a.Kind() {
			t.Errorf("%s does not round-trip: %s (%v)", raw, out, err)
		}
	}
	// Fill with "" clears a field and is a valid action.
	if _, err := ParseUIAction(act("fill", `{"target":{"element":"todo-edit"},"value":""}`)); err != nil {
		t.Errorf("fill with an empty value: %v", err)
	}
}

func TestUIDialect_UnmarshalUIActionRefusals(t *testing.T) {
	for raw, want := range map[string]string{
		act("tap", tgt("x")):                                        "unknown UI action kind",
		act("press", tgt("todo-edit")):                              "needs a key",
		act("check", `{"target":{}}`):                               "needs an element",
		act("navigate", `{}`):                                       "needs a route",
		act("select", tgt("x")):                                     "needs an option",
		act("click", `{"target":{"element":"x"},"value":"y"}`):      "unknown field",
		act("click", `{"target":{"element":"x","nth":-1}}`):         "nth must not be negative",
		`{"kind":"click"}`:                                          "no model",
		`{"kind":"click","model":{"target":{"element":"x"}},"x":1}`: "envelope",
		`not json`: "not a UI action",
	} {
		if _, err := ParseUIAction(raw); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err %v, want %q", raw, err, want)
		}
	}
}

func TestUIDialect_ParseUIObservation(t *testing.T) {
	ok := `[` + act("checked", tgt("todo-toggle")) + `,` +
		act("text", `{"target":{"element":"todo-count"},"text":"0 items left"}`) + `,` +
		act("text", `{"target":{"element":"todo-new"},"text":""}`) + `,` +
		act("count", `{"target":{"element":"todo-row"},"n":3}`) + `,` +
		act("url", `{"route":"/active"}`) + `]`
	got, err := ParseUIObservation(ok)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 || got[4].Kind() != UIObservableURL {
		t.Errorf("decoded %v", got)
	}
	for raw, want := range map[string]string{
		`[]`: "asserts nothing",
		`{"kind":"visible","model":{"target":{"element":"a"}}}`:            "not a UI observation",
		`[` + act("enabled", tgt("a")) + `,` + act("glow", tgt("a")) + `]`: "observable 1",
		`[` + act("count", `{"target":{"element":"a"},"n":-1}`) + `]`:      "n must not be negative",
		`[` + act("visible", `{}`) + `]`:                                   "needs an element",
		`[` + act("url", `{}`) + `]`:                                       "needs a route",
		`[{"visible":"a"}]`:                                                "observable 0",
	} {
		if _, err := ParseUIObservation(raw); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err %v, want %q", raw, err, want)
		}
	}
}

func TestUIDialect_ActionRolesAndLayerA(t *testing.T) {
	if got := ActionRoles(UIActionCheck); !slices.Equal(got, []UIRole{UIRoleCheckbox, UIRoleSwitch}) {
		t.Errorf("check roles = %v", got)
	}
	if got := ActionRoles(UIActionUncheck); !slices.Equal(got, []UIRole{UIRoleCheckbox, UIRoleSwitch}) {
		t.Errorf("uncheck roles = %v", got)
	}
	if got := ActionRoles(UIActionFill); !slices.Equal(got, []UIRole{UIRoleTextbox, UIRoleCombobox}) {
		t.Errorf("fill roles = %v", got)
	}
	if got := ActionRoles(UIActionSelect); !slices.Equal(got, []UIRole{UIRoleCombobox}) {
		t.Errorf("select roles = %v", got)
	}
	for _, k := range []UIActionKind{UIActionPress, UIActionClick, UIActionDblclick, UIActionHover} {
		if ActionRoles(k) != nil {
			t.Errorf("%s admits any role", k)
		}
	}
	tg := UITarget{Element: "todo-toggle"}
	for _, tc := range []struct {
		a    UIAction
		want []UIObservable
	}{
		{UICheck{Target: tg}, []UIObservable{UIChecked{Target: tg}}},
		{UIUncheck{Target: tg}, []UIObservable{UIUnchecked{Target: tg}}},
		{UIFill{Target: tg, Value: "milk"}, []UIObservable{UIValue{Target: tg, Value: "milk"}}},
		{UISelect{Target: tg, Option: "Newest"}, []UIObservable{UIValue{Target: tg, Value: "Newest"}}},
		{UIClick{Target: tg}, nil},
		{UINavigate{Route: "/"}, nil},
		{UIPress{Target: tg, Key: "Enter"}, nil},
	} {
		got := LayerAObservables(tc.a)
		if len(got) != len(tc.want) || (len(got) == 1 && got[0] != tc.want[0]) {
			t.Errorf("LayerAObservables(%s) = %v, want %v", tc.a.Kind(), got, tc.want)
		}
	}
}

// todoDesign is a small surface design the rule tests target.
func todoDesign() UIDesignRecord {
	return UIDesignRecord{Surface: "billingManager", Screens: []UIScreen{{
		ID: "all", Route: "/", Elements: []UIElement{
			{ID: "todo-list", Role: UIRoleList},
			{ID: "todo-row", Role: UIRoleListitem, Name: "todo", Within: "todo-list", Repeats: true},
			{ID: "todo-toggle", Role: UIRoleCheckbox, Name: "Mark done", Within: "todo-row"},
			{ID: "todo-add", Role: UIRoleButton, Name: "Add"},
			{ID: "todo-new", Role: UIRoleTextbox, Name: "What needs to be done?"},
			{ID: "todo-count", Role: UIRoleStatus, Name: "items left"},
		}}, {
		ID: "active", Route: "/active", Elements: []UIElement{{ID: "active-title", Role: UIRoleHeading, Name: "Active"}},
	}}}
}

// asBrowserStep turns billingManager's RecordPayment into a browser operation of the
// web dialect, records the surface's design, and binds process-order-P2 step 1 to the
// action and expected observation.
func asBrowserStep(t *testing.T, p Project, action, observation string) {
	t.Helper()
	sc := p.ServiceContracts["billingManager"]
	sc.Defs["UIAction"] = json.RawMessage(`{"type":"object"}`)
	sc.Defs["UIObservation"] = json.RawMessage(`{"type":"array"}`)
	sc.Interface.Operations[0].Params = []ContractParam{{Name: "action", Schema: json.RawMessage(`{"$ref":"#/$defs/UIAction"}`)}}
	sc.Interface.Operations[0].Result = json.RawMessage(`{"$ref":"#/$defs/UIObservation"}`)
	p.ServiceContracts["billingManager"] = sc
	if p.PhaseArtifacts.UIDesign == nil {
		p.PhaseArtifacts.UIDesign = map[string]UIDesignRecord{}
	}
	p.PhaseArtifacts.UIDesign["billingManager"] = todoDesign()
	b := binding(t, p, "billingManager", "process-order-P2")
	b.Steps[0].Inputs = []TestArg{{Name: "action", Value: lit(action)}}
	b.Steps[0].Expect = TestExpect{Result: observation}
}

// The TP rules hold a browser step to the dialect when the plan is written: a known
// kind with its fields (TP-ARG-TYPE), a non-empty array of observables (TP-EXPECT).
func TestTP_UIDialect(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	asBrowserStep(t, p, act("check", tgt("todo-toggle")),
		`[`+act("checked", tgt("todo-toggle"))+`,`+act("text", `{"target":{"element":"todo-count"},"text":"0 items left"}`)+`]`)
	fs := runTP(t, p)
	wantRules(t, fs, ruleTPArgType, 0)
	wantRules(t, fs, ruleTPExpect, 0)
	wantRules(t, fs, ruleTPUITarget, 0)
	wantRules(t, fs, ruleTPUIAction, 0)

	p = loadProject(t, "tp_clean.json")
	asBrowserStep(t, p, act("tap", tgt("todo-toggle")), `[]`)
	fs = runTP(t, p)
	wantRules(t, fs, ruleTPArgType, 1)
	wantRules(t, fs, ruleTPExpect, 1)

	p = loadProject(t, "tp_clean.json")
	asBrowserStep(t, p, act("press", tgt("todo-new")), `[`+act("visible", tgt("todo-list"))+`]`)
	wantRules(t, runTP(t, p), ruleTPArgType, 1)
}
