package methodcheck

import (
	"slices"
	"strings"
	"testing"
)

func TestIsCoreUseCaseRule(t *testing.T) {
	for _, id := range []RuleID{
		"UC-PATH-OUTCOME", "UC-PATH-OUTCOME-DISTINCT", "UC-OUTCOME-AT-ASKING-SURFACE", "UC-OUTCOME-TO-ASKER",
		"UC-START-NOT-ACTION", "UC-ACTOR-DOOR", "UC-ACTOR-UNSERVED", "UC-LANE-RESOLVES",
	} {
		if !IsCoreUseCaseRule(id) {
			t.Errorf("%s must be a core-use-case rule", id)
		}
	}
	for _, id := range []RuleID{"SCN-UNADDRESSED", "SCN-ACTOR-PATH-CLIENT", "CC-COVERAGE", "NOPE", ""} {
		if IsCoreUseCaseRule(id) {
			t.Errorf("%s must not be a core-use-case rule", id)
		}
	}
}

func TestCoreUseCaseRules_ReturnsACopy(t *testing.T) {
	got := CoreUseCaseRules()
	if len(got) != 8 {
		t.Fatalf("want 8 rules, got %v", got)
	}
	got[0] = "MUTATED"
	if slices.Contains(CoreUseCaseRules(), "MUTATED") || !IsCoreUseCaseRule("UC-PATH-OUTCOME") {
		t.Fatal("CoreUseCaseRules must return a clone")
	}
}

func TestUCLoc_KeepsOrdinalAndSection(t *testing.T) {
	l := ucLoc(3, "useCase a scenario b", "a", "b")
	if l.Ordinal != 3 || l.Section != "useCase a scenario b" || l.UseCase != "a" || l.Scenario != "b" {
		t.Fatalf("ucLoc: %+v", l)
	}
}

func TestUCOutcomeToAsker_LocationNamesUseCaseAndScenarioOnRUN030(t *testing.T) {
	fs, err := ValidateProjectJSON(readFixture(t, "run030_todomvc_through_core_use_cases.json"), ValidateOptions{})
	if err != nil {
		t.Fatalf("ValidateProjectJSON: %v", err)
	}
	n, shape := 0, false
	for _, f := range fs {
		if f.RuleID != ruleUCOutcomeToAsker || f.Location == nil {
			continue
		}
		n++
		want := "useCase " + f.Location.UseCase + " scenario " + f.Location.Scenario
		if f.Location.UseCase == "" || f.Location.Scenario == "" || f.Location.Section != want {
			t.Fatalf("location must match its section: %+v", f.Location)
		}
		shape = shape || f.Location.UseCase == "shape-the-list"
	}
	if n == 0 || !shape {
		t.Fatalf("want UC-OUTCOME-TO-ASKER findings incl. shape-the-list, got %d (shape=%v)", n, shape)
	}
}

func TestUCLaneResolves_LocationNamesUseCaseOnly(t *testing.T) {
	fs := ucLaneFindings(run024CoreUseCases(t), run024System())
	n := 0
	for _, f := range fs {
		if f.RuleID != ruleUCLaneResolves {
			continue
		}
		n++
		if f.Location.UseCase == "" || f.Location.Scenario != "" || !strings.Contains(f.Location.Section, f.Location.UseCase) {
			t.Fatalf("lane finding location: %+v", f.Location)
		}
	}
	if n == 0 {
		t.Fatal("expected a UC-LANE-RESOLVES finding")
	}
}
