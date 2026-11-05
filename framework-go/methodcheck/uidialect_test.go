package methodcheck

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestUIDialect_Kinds(t *testing.T) {
	if got, want := UIActionKinds(), []string{"check", "click", "dblclick", "fill", "hover", "navigate", "press", "uncheck"}; !slices.Equal(got, want) {
		t.Errorf("UIActionKinds = %v, want %v", got, want)
	}
	if got, want := UIObservableKinds(), []string{"checked", "text", "unchecked", "url", "visible"}; !slices.Equal(got, want) {
		t.Errorf("UIObservableKinds = %v, want %v", got, want)
	}
}

func TestUIDialect_ParseUIAction(t *testing.T) {
	for _, ok := range []string{
		`{"kind":"click","target":"todo-add"}`,
		`{"kind":"check","target":"todo-toggle"}`,
		`{"kind":"uncheck","target":"todo-toggle"}`,
		`{"kind":"dblclick","target":"todo-title"}`,
		`{"kind":"hover","target":"todo-row"}`,
		`{"kind":"press","target":"todo-edit","value":"Enter"}`,
		`{"kind":"fill","target":"todo-new","value":"milk"}`,
		`{"kind":"fill","target":"todo-edit"}`,
		`{"kind":"navigate","value":"/active"}`,
	} {
		if _, err := ParseUIAction(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for raw, want := range map[string]string{
		`{"kind":"tap","target":"x"}`:                  "unknown UI action kind",
		`{"kind":"press","value":"Enter"}`:             "needs target",
		`{"kind":"press","target":"todo-edit"}`:        "needs value",
		`{"kind":"check"}`:                             "needs target",
		`{"kind":"navigate"}`:                          "needs value",
		`{"kind":"navigate","target":"x","value":"/"}`: "takes no target",
		`{"kind":"click","target":"x","value":"y"}`:    "takes no value",
		`{"kind":"click","target":"x","key":"y"}`:      "not a UI action",
		`not json`: "not a UI action",
	} {
		if _, err := ParseUIAction(raw); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err %v, want %q", raw, err, want)
		}
	}
}

func TestUIDialect_ParseUIObservation(t *testing.T) {
	for _, ok := range []string{
		`{"visible":"todo-list"}`,
		`{"checked":"todo-toggle"}`,
		`{"unchecked":"todo-toggle"}`,
		`{"text":"1 item left","target":"todo-count"}`,
		`{"url":"/active"}`,
		`[{"checked":"todo-toggle"},{"text":"0 items left","target":"todo-count"}]`,
		// The field holds no text: presence states the kind (RUN-021's fresh todomvc plan wrote it).
		`[{"visible":"new-todo-input"},{"target":"new-todo-input","text":""}]`,
	} {
		if _, err := ParseUIObservation(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for raw, want := range map[string]string{
		`{}`:                            "exactly one",
		`{"visible":"a","checked":"a"}`: "exactly one",
		`[{"visible":"a"},{"url":"/","checked":"b"}]`: "observable 1",
		`{"visible":"a","target":"b"}`:                "target goes with text",
		`{"text":"x"}`:                                "target goes with text",
		`{"text":""}`:                                 "target goes with text",
		`{"visible":"","target":"a"}`:                 "exactly one",
		`{"enabled":"a"}`:                             "not a UI observation",
	} {
		if _, err := ParseUIObservation(raw); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err %v, want %q", raw, err, want)
		}
	}
}

// asBrowserStep turns billingManager's RecordPayment into a browser operation of the
// web dialect and binds process-order-P2 step 1 to action and expected observation.
func asBrowserStep(t *testing.T, p Project, action, observation string) {
	t.Helper()
	sc := p.ServiceContracts["billingManager"]
	sc.Defs["UIAction"] = json.RawMessage(`{"type":"object"}`)
	sc.Defs["UIObservation"] = json.RawMessage(`{"oneOf":[{"type":"object"},{"type":"array"}]}`)
	sc.Interface.Operations[0].Params = []ContractParam{{Name: "action", Schema: json.RawMessage(`{"$ref":"#/$defs/UIAction"}`)}}
	sc.Interface.Operations[0].Result = json.RawMessage(`{"$ref":"#/$defs/UIObservation"}`)
	p.ServiceContracts["billingManager"] = sc
	b := binding(t, p, "billingManager", "process-order-P2")
	b.Steps[0].Inputs = []TestArg{{Name: "action", Value: action}}
	b.Steps[0].Expect = TestExpect{Result: observation}
}

// The TP rules hold a browser step to the dialect when the plan is written: a known
// kind with its fields (TP-ARG-TYPE), one thing per observable (TP-EXPECT).
func TestTP_UIDialect(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	asBrowserStep(t, p, `{"kind":"check","target":"todo-toggle"}`, `[{"checked":"todo-toggle"},{"text":"0 items left","target":"todo-count"}]`)
	fs := runTP(t, p)
	wantRules(t, fs, ruleTPArgType, 0)
	wantRules(t, fs, ruleTPExpect, 0)

	p = loadProject(t, "tp_clean.json")
	asBrowserStep(t, p, `{"kind":"tap","target":"todo-toggle"}`, `{"visible":"a","checked":"a"}`)
	fs = runTP(t, p)
	wantRules(t, fs, ruleTPArgType, 1)
	wantRules(t, fs, ruleTPExpect, 1)

	p = loadProject(t, "tp_clean.json")
	asBrowserStep(t, p, `{"kind":"press","value":"Enter"}`, `{"visible":"todo-list"}`)
	wantRules(t, runTP(t, p), ruleTPArgType, 1)
}
