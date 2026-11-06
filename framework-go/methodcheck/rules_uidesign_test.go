package methodcheck

import (
	"encoding/json"
	"testing"
)

func TestUID_Role(t *testing.T) {
	d := todoDesign()
	wantRules(t, uiDesignFindings("billingManager", d), ruleUIDRole, 0)
	d.Screens[0].Elements[3].Role = "bttn"
	wantRules(t, uiDesignFindings("billingManager", d), ruleUIDRole, 1)
}

func TestUID_Name(t *testing.T) {
	d := todoDesign()
	wantRules(t, uiDesignFindings("billingManager", d), ruleUIDName, 0) // list has no name: allowed
	d.Screens[0].Elements[3].Name = ""                                  // a button must be named
	wantRules(t, uiDesignFindings("billingManager", d), ruleUIDName, 1)
}

func TestUID_IDUnique(t *testing.T) {
	d := todoDesign()
	wantRules(t, uiDesignFindings("billingManager", d), ruleUIDIDUnique, 0)
	d.Screens[1].Elements[0].ID = "todo-add" // duplicates across screens
	wantRules(t, uiDesignFindings("billingManager", d), ruleUIDIDUnique, 1)
}

func TestUID_Within(t *testing.T) {
	d := todoDesign()
	wantRules(t, uiDesignFindings("billingManager", d), ruleUIDWithin, 0)
	d.Screens[0].Elements[2].Within = "nowhere"
	wantRules(t, uiDesignFindings("billingManager", d), ruleUIDWithin, 1)
	d = todoDesign()
	d.Screens[0].Elements[2].Within = "active-title" // an element of another screen
	wantRules(t, uiDesignFindings("billingManager", d), ruleUIDWithin, 1)
}

func TestUID_Route(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	asBrowserStep(t, p, act("navigate", `{"route":"/active"}`), `[`+act("url", `{"route":"/active"}`)+`]`)
	wantRules(t, runTP(t, p), ruleUIDRoute, 0)

	p = loadProject(t, "tp_clean.json")
	asBrowserStep(t, p, act("navigate", `{"route":"/nope"}`), `[`+act("url", `{"route":"/active"}`)+`]`)
	wantRules(t, runTP(t, p), ruleUIDRoute, 1)

	p = loadProject(t, "tp_clean.json")
	asBrowserStep(t, p, act("navigate", `{"route":"/active"}`), `[`+act("url", `{"route":"/nope"}`)+`]`)
	wantRules(t, runTP(t, p), ruleUIDRoute, 1)
}

// A `check` bound to an element whose design role is button fires (RUN-021).
func TestTP_UITarget_RoleAgreement(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	asBrowserStep(t, p, act("check", tgt("todo-toggle")), `[`+act("checked", tgt("todo-toggle"))+`]`)
	wantRules(t, runTP(t, p), ruleTPUITarget, 0)

	p = loadProject(t, "tp_clean.json")
	asBrowserStep(t, p, act("check", tgt("todo-add")), `[`+act("visible", tgt("todo-add"))+`]`)
	wantRules(t, runTP(t, p), ruleTPUITarget, 1)

	p = loadProject(t, "tp_clean.json")
	asBrowserStep(t, p, act("fill", `{"target":{"element":"todo-add"},"value":"x"}`), `[`+act("visible", tgt("todo-add"))+`]`)
	wantRules(t, runTP(t, p), ruleTPUITarget, 1)
}

func TestTP_UITarget_ElementMustResolve(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	asBrowserStep(t, p, act("click", tgt("ghost")), `[`+act("visible", tgt("todo-list"))+`]`)
	wantRules(t, runTP(t, p), ruleTPUITarget, 1)

	p = loadProject(t, "tp_clean.json")
	asBrowserStep(t, p, act("click", tgt("todo-add")), `[`+act("visible", tgt("ghost"))+`]`)
	wantRules(t, runTP(t, p), ruleTPUITarget, 1)
}

func TestTP_UITarget_NoDesignRecorded(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	asBrowserStep(t, p, act("click", tgt("todo-add")), `[`+act("visible", tgt("todo-list"))+`]`)
	delete(p.PhaseArtifacts.UIDesign, "billingManager")
	wantRules(t, runTP(t, p), ruleTPUITarget, 1)
}

func TestTP_UITarget_CountNeedsRepeats(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	asBrowserStep(t, p, act("click", tgt("todo-add")), `[`+act("count", `{"target":{"element":"todo-row"},"n":2}`)+`]`)
	wantRules(t, runTP(t, p), ruleTPUITarget, 0)

	p = loadProject(t, "tp_clean.json")
	asBrowserStep(t, p, act("click", tgt("todo-add")), `[`+act("count", `{"target":{"element":"todo-add"},"n":2}`)+`]`)
	wantRules(t, runTP(t, p), ruleTPUITarget, 1)
}

// A browser step needs exactly one action input: a step with none does nothing and
// asserts a state that already holds.
func TestTP_UIAction_ExactlyOne(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	asBrowserStep(t, p, act("click", tgt("todo-add")), `[`+act("visible", tgt("todo-list"))+`]`)
	wantRules(t, runTP(t, p), ruleTPUIAction, 0)

	p = loadProject(t, "tp_clean.json")
	asBrowserStep(t, p, act("click", tgt("todo-add")), `[`+act("visible", tgt("todo-list"))+`]`)
	binding(t, p, "billingManager", "process-order-P2").Steps[0].Inputs = nil
	wantRules(t, runTP(t, p), ruleTPUIAction, 1)

	p = loadProject(t, "tp_clean.json")
	asBrowserStep(t, p, act("click", tgt("todo-add")), `[`+act("visible", tgt("todo-list"))+`]`)
	st := &binding(t, p, "billingManager", "process-order-P2").Steps[0]
	st.Inputs = append(st.Inputs, st.Inputs[0])
	wantRules(t, runTP(t, p), ruleTPUIAction, 1)
}

// uidProject is tp_clean with billingManager as a browser surface and its design
// recorded; mutate edits that design before the project is checked.
func uidProject(t *testing.T, mutate func(d *UIDesignRecord)) Project {
	t.Helper()
	p := loadProject(t, "tp_clean.json")
	asBrowserStep(t, p, act("click", tgt("todo-add")), `[`+act("visible", tgt("todo-list"))+`]`)
	d := p.PhaseArtifacts.UIDesign["billingManager"]
	if mutate != nil {
		mutate(&d)
	}
	p.PhaseArtifacts.UIDesign["billingManager"] = d
	return p
}

// The UID record rules fire through the project check pipeline (testPlanFindings),
// not only when uiDesignFindings is called directly.
func TestUID_RecordRulesRunInProjectCheck(t *testing.T) {
	cases := []struct {
		name   string
		rule   RuleID
		mutate func(d *UIDesignRecord)
	}{
		{"UID-ROLE", ruleUIDRole, func(d *UIDesignRecord) { d.Screens[0].Elements[3].Role = "bttn" }},
		{"UID-NAME", ruleUIDName, func(d *UIDesignRecord) { d.Screens[0].Elements[3].Name = "" }},
		{"UID-ID-UNIQUE", ruleUIDIDUnique, func(d *UIDesignRecord) { d.Screens[1].Elements[0].ID = "todo-add" }},
		{"UID-WITHIN", ruleUIDWithin, func(d *UIDesignRecord) { d.Screens[0].Elements[2].Within = "nowhere" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wantRules(t, runTP(t, uidProject(t, nil)), c.rule, 0)
			wantRules(t, runTP(t, uidProject(t, c.mutate)), c.rule, 1)
		})
	}
}

// An input named "action" is a UI action only on a browser surface: a service
// operation's ordinary string parameter of that name is not held to the dialect.
func TestTP_UIAction_NamedActionOnServiceOpIsOrdinary(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	sc := p.ServiceContracts["billingManager"]
	sc.Interface.Operations[0].Params = append(sc.Interface.Operations[0].Params,
		ContractParam{Name: "action", Schema: json.RawMessage(`{"type":"string"}`)})
	p.ServiceContracts["billingManager"] = sc
	st := &binding(t, p, "billingManager", "process-order-P2").Steps[0]
	st.Inputs = append(st.Inputs, TestArg{Name: "action", Value: lit(`"pay"`)})
	fs := runTP(t, p)
	wantRules(t, fs, ruleTPArgType, 0)
	wantRules(t, fs, ruleTPUIAction, 0)
}
