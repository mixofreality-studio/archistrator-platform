package methodcheck

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// errorFindings is the Error-severity subset of fs.
func errorFindings(fs []Finding) []Finding {
	var out []Finding
	for _, f := range fs {
		if f.Severity == SeverityError {
			out = append(out, f)
		}
	}
	return out
}

// RUN-025: the todomvc design recorded through the coreUseCases slot and nothing after it (no
// System, no contracts), every actor's act linked to its actor as UC-ACTOR-DOOR requires. The
// scenario-derivation rules held each actor's input to reaching a contracted Client, which cannot
// exist yet, so no draft of the slot could be recorded. Recorded up to its own slot, the slot
// validates with no Error.
func TestValidateProject_RUN025_CoreUseCasesSlotBeforeArchitecture(t *testing.T) {
	fs, err := ValidateProjectJSON(readFixture(t, "run025_todomvc_through_core_use_cases.json"), ValidateOptions{})
	if err != nil {
		t.Fatalf("ValidateProjectJSON: %v", err)
	}
	// The recording predates UC-PATH-OUTCOME-DISTINCT: its guarded branches share one
	// outcome sendSignal after the merge, so that rule fires and nothing else does.
	distinct := 0
	for _, f := range errorFindings(fs) {
		if f.RuleID == ruleUCPathOutcomeDistinct {
			distinct++
			continue
		}
		t.Errorf("%s: %s", f.RuleID, f.Message)
	}
	if distinct == 0 {
		t.Errorf("the RUN-025 recording has guarded branches ending on one shared outcome; UC-PATH-OUTCOME-DISTINCT must fire")
	}
}

// The scenario-derivation rules are not weakened: once the contracts they read are recorded they
// fire on their violating fixtures exactly as before. tp_clean.json's abandon path carries an
// actor's input that reaches no Client (SCN-ACTOR-PATH-CLIENT); removing one contract of a design
// that has others leaves the calls into that component unaddressed (SCN-UNADDRESSED). With no
// contract recorded at all, neither runs.
func TestScenarioRules_RunOnceContractsAreRecorded(t *testing.T) {
	p := loadProject(t, "tp_clean.json")
	fs, err := ValidateProject(p, ValidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if countRule(fs, ruleSCNActorPathClient) == 0 {
		t.Errorf("SCN-ACTOR-PATH-CLIENT must fire with the contracts recorded: %s", dumpFindings(fs))
	}

	partial := loadProject(t, "tp_clean.json")
	delete(partial.ServiceContracts, "orderManager")
	fs, err = ValidateProject(partial, ValidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if countRule(fs, ruleSCNUnaddressed) == 0 {
		t.Errorf("SCN-UNADDRESSED must fire on a call into an uncontracted component once contracts are recorded: %s", dumpFindings(fs))
	}

	none := loadProject(t, "tp_clean.json")
	none.ServiceContracts = nil
	none.PhaseArtifacts = nil
	fs, err = ValidateProject(none, ValidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []RuleID{ruleSCNActorPathClient, ruleSCNUnaddressed, ruleTPClientContracted} {
		if n := countRule(fs, id); n != 0 {
			t.Errorf("%s needs the contracts, none of which is recorded, yet fired %d time(s)", id, n)
		}
	}
}

// ruleIDConstants is every RuleID constant the package's non-test source declares, by value.
func ruleIDConstants(t *testing.T) map[RuleID]string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	out := map[RuleID]string{}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				if typ, ok := vs.Type.(*ast.Ident); !ok || typ.Name != "RuleID" {
					continue
				}
				for i, v := range vs.Values {
					lit, ok := v.(*ast.BasicLit)
					if !ok {
						t.Fatalf("%s: RuleID constant %s is not a string literal", name, vs.Names[i].Name)
					}
					id, err := strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatal(err)
					}
					out[RuleID(id)] = name
				}
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("no RuleID constant found: the scan is vacuous")
	}
	return out
}

// Every rule declares the artifacts it needs recorded before it can judge anything, even when
// that is none: a RuleID constant of the package, or an id in the emitted-rule registry, with no
// prerequisites entry fails here, so a new rule cannot reach a slot whose upstream does not exist
// yet without its author saying what it reads. The registry names no rule that is not declared.
func TestRulePrerequisites_EveryRuleDeclaresThem(t *testing.T) {
	consts := ruleIDConstants(t)
	for _, id := range slices.Sorted(maps.Keys(consts)) {
		if _, ok := rulePrerequisites[id]; !ok {
			t.Errorf("rule %s (%s) declares no prerequisites: add it to rulePrerequisites (prerequisites.go)", id, consts[id])
		}
		if !emittedRuleIDs()[id] {
			t.Errorf("rule %s (%s) is not in the emitted-rule registry (emitters.go)", id, consts[id])
		}
	}
	for id := range emittedRuleIDs() {
		if _, ok := rulePrerequisites[id]; !ok {
			t.Errorf("emitted rule %s declares no prerequisites", id)
		}
	}
	for id := range rulePrerequisites {
		if _, ok := consts[id]; !ok {
			t.Errorf("rulePrerequisites declares %s, which no RuleID constant names", id)
		}
	}
}

// allPrerequisites is every Prerequisite variant.
func allPrerequisites() []Prerequisite {
	return []Prerequisite{
		RequiresMission{}, RequiresGlossary{}, RequiresScrubbedRequirements{}, RequiresVolatilities{},
		RequiresCoreUseCases{}, RequiresSystem{}, RequiresOperationalConcepts{}, RequiresStandardCheck{},
		RequiresContracts{}, RequiresTestPlan{}, RequiresUIDesign{},
	}
}

// Every rule's prerequisites are listed for the record (go test -v -run RulePrerequisites).
func TestRulePrerequisites_Listing(t *testing.T) {
	for _, id := range slices.Sorted(maps.Keys(rulePrerequisites)) {
		prs, _ := RulePrerequisites(id)
		names := make([]string, len(prs))
		for i, pr := range prs {
			names[i] = PrerequisiteName(pr)
		}
		if len(names) == 0 {
			t.Errorf("%s declares an empty prerequisite list", id)
		}
		t.Logf("%s: %s", id, strings.Join(names, ", "))
	}
}

// Each prerequisite is recorded exactly when its artifact is: a committed slot of its kind, or a
// non-empty top-level record. A slot that is only drafted is not recorded.
func TestPrerequisiteRecorded(t *testing.T) {
	committed := func(kind int) Slot { return Slot{Status: reviewCommitted, Kind: kind, Model: []byte(`{}`)} }
	full := Project{
		Slots: map[string]Slot{
			"0": committed(kindMission), "1": committed(kindGlossary), "2": committed(kindScrubbedRequirements),
			"3": committed(kindVolatilities), "4": committed(kindCoreUseCases), "5": committed(kindSystem),
			"6": committed(kindOperationalConcepts), "7": committed(kindStandardCheck),
		},
		ServiceContracts: map[string]ServiceContract{"a": {}},
		PhaseArtifacts: &PhaseArtifacts{
			TestPlan: map[string]TestPlanRecord{"a": {}},
			UIDesign: map[string]UIDesignRecord{"web": {}},
		},
	}
	drafted := Project{Slots: map[string]Slot{}}
	for k, s := range full.Slots {
		s.Status = reviewCommitted + 1
		drafted.Slots[k] = s
	}
	for _, pr := range allPrerequisites() {
		if !recorded(full, pr) {
			t.Errorf("%s: not recorded in a project that holds every artifact", PrerequisiteName(pr))
		}
		if recorded(drafted, pr) {
			t.Errorf("%s: recorded in a project whose slots are only drafted and that holds no record", PrerequisiteName(pr))
		}
	}
}

// A finding under a rule the registry does not know (a scenario Problem code methodcheck has no
// rule for) is never silenced by the prerequisite gate.
func TestPrerequisiteGate_KeepsUnknownRules(t *testing.T) {
	fs := []Finding{{RuleID: "SCN-FUTURE", Severity: SeverityError}, {RuleID: ruleSCNUnaddressed, Severity: SeverityError}}
	got := withPrerequisitesRecorded(Project{}, fs)
	if len(got) != 1 || got[0].RuleID != "SCN-FUTURE" {
		t.Fatalf("want only the unknown rule kept over an empty project, got %s", dumpFindings(got))
	}
}

// systemProject is a project whose committed System holds the given components (id, kind,
// buildStatus) and whose contracts are filed under the given keys.
func systemProject(t *testing.T, comps [][3]string, contractKeys ...string) Project {
	t.Helper()
	var cs []Component
	for _, c := range comps {
		cs = append(cs, Component{ID: c[0], Kind: c[1], BuildStatus: c[2]})
	}
	model, err := json.Marshal(System{Components: cs})
	if err != nil {
		t.Fatal(err)
	}
	p := Project{
		Slots:            map[string]Slot{"5": {Status: reviewCommitted, Kind: kindSystem, Model: model}},
		ServiceContracts: map[string]ServiceContract{},
	}
	for _, k := range contractKeys {
		p.ServiceContracts[k] = ServiceContract{}
	}
	return p
}

// withActivities commits an activity list covering the named components (activity C-<id>), with
// the given completion state: a component in completed has a finished activity, the rest are
// still to be constructed. It models the project's own progress, which is the window's closing signal.
func withActivities(t *testing.T, p Project, ids []string, completed ...string) Project {
	t.Helper()
	type activity struct {
		Name        string `json:"name"`
		ComponentID string `json:"componentId"`
	}
	acts := make([]activity, 0, len(ids))
	for _, id := range ids {
		acts = append(acts, activity{Name: "C-" + id, ComponentID: id})
	}
	model, err := json.Marshal(map[string]any{"activities": acts})
	if err != nil {
		t.Fatal(err)
	}
	slots := map[string]Slot{}
	for k, s := range p.Slots {
		slots[k] = s
	}
	slots["9"] = Slot{Status: reviewCommitted, Kind: slotKindActivityList, Model: model}
	p.Slots = slots
	p.ActivityExecution = map[string]ActivityRow{}
	for _, id := range completed {
		p.ActivityExecution["C-"+id] = ActivityRow{CompletedAt: done}
	}
	return p
}

// A contract is owed by every component except a Resource and one nobody builds here; a Client owes
// one. UncontractedComponents lists the ones still owing, in System order, and is nil with no
// committed System.
func TestUncontractedComponents(t *testing.T) {
	comps := [][3]string{
		{"list-manager", "manager", ""},
		{"todo-web", "client", ""},
		{"todo-store", "resource", ""},
		{"later-engine", "engine", "planned"},
		{"vendor-api", "utility", "external"},
		{"todo-list-access", "resourceAccess", ""},
	}
	all := []string{"list-manager", "todo-web", "todo-list-access"}
	tests := []struct {
		name      string
		p         Project
		want      []string
		recorded  bool
		contracts []string
	}{
		{"nothing recorded", systemProject(t, comps), []string{"list-manager", "todo-web", "todo-list-access"}, false, nil},
		{"a Client still owes", systemProject(t, comps, "listManager", "todoListAccess"), []string{"todo-web"}, false, nil},
		{"complete", systemProject(t, comps, "listManager", "todoWeb", "todoListAccess"), nil, true, nil},
		// The window's closing signal: the Client's activity completed and it still has no contract.
		{"a Client built without a contract", withActivities(t, systemProject(t, comps, "listManager", "todoListAccess"), all, "todo-web"), []string{"todo-web"}, true, nil},
		{"no activity covers the Client", withActivities(t, systemProject(t, comps, "listManager", "todoListAccess"), []string{"list-manager", "todo-list-access"}), []string{"todo-web"}, true, nil},
		{"no activity list at all", systemProject(t, comps, "listManager", "todoListAccess"), []string{"todo-web"}, true, nil},
		{"no System", Project{ServiceContracts: map[string]ServiceContract{"a": {}}}, nil, true, nil},
	}
	for i := range tests {
		if _, covered := tests[i].p.Slots["9"]; !covered && tests[i].name != "no System" && tests[i].name != "no activity list at all" {
			tests[i].p = withActivities(t, tests[i].p, all)
		}
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := UncontractedComponents(tc.p); !slices.Equal(got, tc.want) {
				t.Errorf("UncontractedComponents = %v, want %v", got, tc.want)
			}
			if got := (RequiresContractsSettled{}).recordedIn(tc.p); got != tc.recorded {
				t.Errorf("RequiresContractsSettled recorded = %v, want %v", got, tc.recorded)
			}
		})
	}
}

// The three full-severity rules keep Error only once every contract-bearing component has a
// contract: until then an Error is a Warning that names the ids still owing one. Other rules and
// non-Error findings pass through.
func TestDowngradeUntilFullyRecorded(t *testing.T) {
	comps := [][3]string{
		{"list-manager", "manager", ""}, {"todo-web", "client", ""},
		{"todo-store", "resource", ""}, {"later-engine", "engine", "planned"}, {"vendor-api", "utility", "external"},
	}
	act := []string{"list-manager", "todo-web"}
	fs := func() []Finding {
		return []Finding{
			{RuleID: ruleSCNUnaddressed, Severity: SeverityError, Message: "a"},
			{RuleID: ruleSCNActorPathClient, Severity: SeverityError, Message: "b"},
			{RuleID: ruleTPClientContracted, Severity: SeverityError, Message: "c"},
			{RuleID: ruleTPClientContracted, Severity: SeverityInfo, Message: "d"},
			{RuleID: ruleSysNoUp, Severity: SeverityError, Message: "e"},
		}
	}
	tests := []struct {
		name      string
		p         Project
		advisory  bool
		ids       string
		wantFirst []Severity
	}{
		{"Client uncontracted", withActivities(t, systemProject(t, comps, "listManager"), act), true, "todo-web", nil},
		{"Manager uncontracted", withActivities(t, systemProject(t, comps, "todoWeb"), act), true, "list-manager", nil},
		{"complete", withActivities(t, systemProject(t, comps, "listManager", "todoWeb"), act), false, "", nil},
		// Closing signal: the Client's construction completed and no contract was recorded for it.
		{"Client built, never contracted", withActivities(t, systemProject(t, comps, "listManager"), act, "todo-web"), false, "", nil},
		{"no construction progress recorded", systemProject(t, comps, "listManager"), false, "", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := downgradeUntilFullyRecorded(tc.p, fs())
			for i, want := range fs() {
				f := got[i]
				tiered := i < 3
				switch {
				case tc.advisory && tiered:
					wantNote := " [advisory: contracts are still being recorded (none yet for " + tc.ids + "); this rule regains Error severity once the construction of each has completed or its contract is recorded]"
					if f.Severity != SeverityWarning || f.Message != want.Message+wantNote {
						t.Errorf("%s: got %s %q, want Warning with the pending note", f.RuleID, severityLabel(f.Severity), f.Message)
					}
				default:
					if f.Severity != want.Severity || f.Message != want.Message {
						t.Errorf("%s %q: changed to %s %q", want.RuleID, want.Message, severityLabel(f.Severity), f.Message)
					}
				}
			}
		})
	}
}

// ValidateProject applies the window end to end: tp_clean.json's SCN-ACTOR-PATH-CLIENT finding is
// an Error with every contract of its System recorded and a Warning once one is withdrawn.
func TestValidateProject_ContractWindowSeverity(t *testing.T) {
	sev := func(p Project) []Severity {
		fs, err := ValidateProject(p, ValidateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		var out []Severity
		for _, f := range fs {
			if f.RuleID == ruleSCNActorPathClient {
				out = append(out, f.Severity)
			}
		}
		return out
	}
	ids := []string{"shop-client", "order-manager", "billing-manager"}
	full := loadProject(t, "tp_clean.json")
	if pending := UncontractedComponents(full); len(pending) != 0 {
		t.Skipf("tp_clean.json leaves %v uncontracted; the complete case needs a complete fixture", pending)
	}
	got := sev(withActivities(t, full, ids))
	if len(got) == 0 || slices.Contains(got, SeverityWarning) {
		t.Errorf("complete contracts: severities %v, want only Error", got)
	}
	// A contract withdrawn while its component's construction is still to come: advisory.
	partial := loadProject(t, "tp_clean.json")
	delete(partial.ServiceContracts, "orderManager")
	got = sev(withActivities(t, partial, ids))
	if len(got) == 0 || slices.Contains(got, SeverityError) {
		t.Errorf("a contract withdrawn, construction to come: severities %v, want only Warning", got)
	}
	// The same withdrawal once that component's construction completed: the window is closed, so a
	// finished project with a built component that never got its contract is held at Error.
	got = sev(withActivities(t, partial, ids, "order-manager"))
	if len(got) == 0 || slices.Contains(got, SeverityWarning) {
		t.Errorf("a contract missing after construction completed: severities %v, want only Error", got)
	}
}

// Every full-severity entry names a known rule that also runs on a prerequisite tier of its own,
// and the tier is spent on contracts alone.
func TestRuleFullSeverity_EveryEntryIsAKnownRuleWithARunTier(t *testing.T) {
	if len(ruleFullSeverity) == 0 {
		t.Fatal("ruleFullSeverity is empty")
	}
	consts := ruleIDConstants(t)
	for id, prs := range ruleFullSeverity {
		if _, ok := consts[id]; !ok {
			t.Errorf("ruleFullSeverity names %s, which no RuleID constant names", id)
		}
		if run, ok := rulePrerequisites[id]; !ok || len(run) == 0 {
			t.Errorf("%s has a full-severity tier but no run-tier entry in rulePrerequisites", id)
		}
		if len(prs) == 0 {
			t.Errorf("%s has an empty full-severity tier", id)
		}
		if got, ok := RuleFullSeverityPrerequisites(id); !ok || len(got) != len(prs) {
			t.Errorf("RuleFullSeverityPrerequisites(%s) = %v, %v", id, got, ok)
		}
	}
	if _, ok := RuleFullSeverityPrerequisites(ruleSysNoUp); ok {
		t.Errorf("%s has no full-severity tier", ruleSysNoUp)
	}
}
