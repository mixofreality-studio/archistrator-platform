package methodcheck

import (
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
