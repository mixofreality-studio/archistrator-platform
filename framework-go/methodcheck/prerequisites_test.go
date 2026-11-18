package methodcheck

import (
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
	for _, f := range errorFindings(fs) {
		t.Errorf("%s: %s", f.RuleID, f.Message)
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
