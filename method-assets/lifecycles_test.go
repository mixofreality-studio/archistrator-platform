package methodassets

import (
	"slices"
	"strings"
	"testing"
)

// wantLifecycleTypes is the closed, ordered set of activity-type keys. The keys
// are the app's own wire names (ActivityType.String(), and "testing:" +
// TestingVariant.String() for testing), so neither side needs a mapping table.
var wantLifecycleTypes = []string{
	"requirements", "architecture", "projectDesign",
	"service", "frontend",
	"testing:plan", "testing:harness", "testing:perf", "testing:systemTest", "testing:qaProcess",
	"deployment", "documentation", "uiDesign", "integration",
}

// figureA1TaskIDs are the ten non-conditional Figure A-1 task keys the app's
// append-only attempt ledger already uses (projectstate.MethodTask). The
// construction-family lifecycles may use no other id; someConstruction and
// testClient are conditional sub-attempts and are never nodes.
var figureA1TaskIDs = []string{
	"srs", "srsReview", "stp", "stpReview", "detailedDesign", "designReview",
	"construction", "codeReview", "integration", "testing",
}

var designActivityTypes = []string{"requirements", "architecture", "projectDesign"}

func TestLifecycles_ClosedOrderedTypeSet(t *testing.T) {
	var got []string
	for _, l := range Lifecycles() {
		got = append(got, l.Type)
	}
	if !slices.Equal(got, wantLifecycleTypes) {
		t.Fatalf("lifecycle types = %v, want %v", got, wantLifecycleTypes)
	}
	for _, key := range wantLifecycleTypes {
		if _, ok := LifecycleFor(key); !ok {
			t.Errorf("LifecycleFor(%q) not found", key)
		}
	}
	if _, ok := LifecycleFor("nope"); ok {
		t.Error("LifecycleFor must report false for an unknown type key")
	}
}

func TestLifecycles_EveryShippedLifecycleIsValid(t *testing.T) {
	for _, l := range Lifecycles() {
		for _, problem := range ValidateLifecycle(l) {
			t.Error(problem)
		}
	}
}

// An independent cycle check: ValidateLifecycle proves acyclicity through its
// authored-order rule; this DFS proves it without relying on that rule.
func TestLifecycles_Acyclic(t *testing.T) {
	const visiting, done = 1, 2
	for _, l := range Lifecycles() {
		deps := map[string][]string{}
		for _, task := range l.Tasks {
			deps[task.ID] = task.DependsOn
		}
		state := map[string]int{}
		var visit func(id string) bool
		visit = func(id string) bool {
			if state[id] == visiting {
				return false
			}
			if state[id] == done {
				return true
			}
			state[id] = visiting
			for _, d := range deps[id] {
				if !visit(d) {
					return false
				}
			}
			state[id] = done
			return true
		}
		for id := range deps {
			if !visit(id) {
				t.Errorf("%s: dependency cycle through %q", l.Type, id)
			}
		}
	}
}

func TestLifecycles_ServiceAndFrontendForkPerFigureA1(t *testing.T) {
	want := map[string][]string{
		"srs":            {},
		"srsReview":      {"srs"},
		"detailedDesign": {"srsReview"},
		"designReview":   {"detailedDesign"},
		"construction":   {"designReview"},
		"codeReview":     {"construction"},
		"integration":    {"codeReview"},
		"stp":            {"srsReview"},
		"stpReview":      {"stp"},
		"testing":        {"integration", "stpReview"},
	}
	for _, key := range []string{"service", "frontend"} {
		l, _ := LifecycleFor(key)
		if len(l.Tasks) != len(want) {
			t.Fatalf("%s: %d tasks, want %d", key, len(l.Tasks), len(want))
		}
		for _, task := range l.Tasks {
			deps, ok := want[task.ID]
			if !ok {
				t.Errorf("%s: unexpected task %q (someConstruction and testClient are conditional sub-attempts, never nodes)", key, task.ID)
				continue
			}
			if !slices.Equal(task.DependsOn, deps) {
				t.Errorf("%s: %s dependsOn = %v, want %v", key, task.ID, task.DependsOn, deps)
			}
		}
		// The trunk is authored first: the layout keeps the first-authored chain on lane 0.
		if l.Tasks[2].ID != "detailedDesign" || l.Tasks[7].ID != "stp" {
			t.Errorf("%s: the Detailed Design branch must be authored before the STP branch", key)
		}
	}
}

func TestLifecycles_LinearTypesAreOneDispatchAndOneGatePerPhase(t *testing.T) {
	for _, l := range Lifecycles() {
		if l.Type == "service" || l.Type == "frontend" || l.Type == "projectDesign" {
			continue
		}
		if len(l.Tasks) != 2*len(l.Phases) {
			t.Errorf("%s: %d tasks for %d phases, want exactly a dispatch and a review gate per phase", l.Type, len(l.Tasks), len(l.Phases))
			continue
		}
		for i, task := range l.Tasks {
			wantKind, wantDeps := LifecycleTaskDispatch, []string{}
			if i%2 == 1 {
				wantKind = LifecycleTaskReview
			}
			if i > 0 {
				wantDeps = []string{l.Tasks[i-1].ID}
			}
			if task.Kind != wantKind || task.Phase != l.Phases[i/2].ID || !slices.Equal(task.DependsOn, wantDeps) {
				t.Errorf("%s: task %d = %+v, want kind %s in phase %s depending on %v", l.Type, i, task, wantKind, l.Phases[i/2].ID, wantDeps)
			}
		}
	}
}

func TestLifecycles_ConstructionFamilyReusesTheLedgerTaskIDs(t *testing.T) {
	for _, l := range Lifecycles() {
		if slices.Contains(designActivityTypes, l.Type) {
			continue
		}
		for _, task := range l.Tasks {
			if !slices.Contains(figureA1TaskIDs, task.ID) {
				t.Errorf("%s: task id %q is not a Figure A-1 ledger key", l.Type, task.ID)
			}
		}
	}
}

func TestLifecycles_DesignActivities(t *testing.T) {
	req, _ := LifecycleFor("requirements")
	var ids []string
	var weights []int
	for _, p := range req.Phases {
		ids = append(ids, p.ID)
		weights = append(weights, p.Weight)
	}
	if !slices.Equal(ids, []string{"mission", "glossary", "volatilities", "coreUseCases"}) {
		t.Errorf("requirements phases = %v", ids)
	}
	if !slices.Equal(weights, []int{15, 20, 35, 30}) {
		t.Errorf("requirements weights = %v, want 15/20/35/30", weights)
	}

	arch, _ := LifecycleFor("architecture")
	if len(arch.Phases) != 1 || arch.Phases[0].Weight != 100 || len(arch.Tasks) != 2 {
		t.Errorf("architecture must be one draft/review pair weighing 100, got %+v", arch)
	}

	pd, _ := LifecycleFor("projectDesign")
	if len(pd.Tasks) != 1 {
		t.Fatalf("projectDesign must be ONE task, got %d", len(pd.Tasks))
	}
	gate := pd.Tasks[0]
	if gate.Kind != LifecycleTaskReview || gate.Command != "" || gate.Reviews != "" || len(gate.DependsOn) != 0 || gate.ArtifactKind != "SdpReview" {
		t.Errorf("projectDesign's only task must be a command-less review of the computed SdpReview, got %+v", gate)
	}
}

// A review that names no dispatch task is legal only where there is none to
// name — and projectDesign is the only such lifecycle.
func TestLifecycles_OnlyProjectDesignReviewsNothing(t *testing.T) {
	for _, l := range Lifecycles() {
		for _, task := range l.Tasks {
			if task.Kind == LifecycleTaskReview && task.Reviews == "" && l.Type != "projectDesign" {
				t.Errorf("%s: review task %q names no dispatch task", l.Type, task.ID)
			}
		}
	}
}

// workerClass is not free text: it is the charter the task's command adopts.
func TestLifecycles_CommandsAreDispatchableAndNameTheirAgent(t *testing.T) {
	for _, l := range Lifecycles() {
		for _, task := range l.Tasks {
			if task.Command == "" {
				if task.WorkerClass != "" {
					t.Errorf("%s: task %q has a workerClass but no command", l.Type, task.ID)
				}
				continue
			}
			m, ok := ManifestFor(task.Command)
			if !ok {
				t.Errorf("%s: task %q names command %q, which has no step manifest", l.Type, task.ID, task.Command)
				continue
			}
			if m.Agent != task.WorkerClass {
				t.Errorf("%s: task %q workerClass = %q, but /%s adopts %q", l.Type, task.ID, task.WorkerClass, task.Command, m.Agent)
			}
		}
	}
}

func TestLifecycles_ResultsAreCopies(t *testing.T) {
	a, _ := LifecycleFor("service")
	a.Phases[0].Weight = 99
	a.Tasks[1].DependsOn[0] = "mutated"
	b, _ := LifecycleFor("service")
	if b.Phases[0].Weight != 15 || b.Tasks[1].DependsOn[0] != "srs" {
		t.Errorf("a caller's mutation leaked into the package data: %+v", b)
	}
}

func TestParseLifecycles_RejectsUnknownFields(t *testing.T) {
	if _, err := parseLifecycles([]byte(`{"lifecycles":[{"type":"x","phases":[],"tasks":[],"wieght":1}]}`)); err == nil {
		t.Error("an unknown field must be a parse error, so a typo cannot ship as a silently dropped value")
	}
}

func validFixture() Lifecycle {
	return Lifecycle{
		Type: "fixture",
		Phases: []LifecyclePhase{
			{ID: "a", Label: "A", Weight: 60, Gate: "aReview", ExitCriterion: "A passes review"},
			{ID: "b", Label: "B", Weight: 40, Gate: "bReview", ExitCriterion: "B passes review"},
		},
		Tasks: []LifecycleTask{
			{ID: "aDraft", Kind: LifecycleTaskDispatch, Title: "Draft A", Phase: "a", DependsOn: []string{}, Command: "mission-draft", WorkerClass: "system-architect", ArtifactKind: "A"},
			{ID: "aReview", Kind: LifecycleTaskReview, Title: "Review A", Phase: "a", DependsOn: []string{"aDraft"}, Reviews: "aDraft"},
			{ID: "bDraft", Kind: LifecycleTaskDispatch, Title: "Draft B", Phase: "b", DependsOn: []string{"aReview"}, Command: "glossary-draft", WorkerClass: "system-architect", ArtifactKind: "B"},
			{ID: "bReview", Kind: LifecycleTaskReview, Title: "Review B", Phase: "b", DependsOn: []string{"bDraft"}, Reviews: "bDraft"},
		},
	}
}

func TestValidateLifecycle_AcceptsTheFixtureAndAGateOnlyLifecycle(t *testing.T) {
	if problems := ValidateLifecycle(validFixture()); len(problems) != 0 {
		t.Fatalf("the valid fixture was rejected: %v", problems)
	}
	gateOnly := Lifecycle{
		Type:   "gateOnly",
		Phases: []LifecyclePhase{{ID: "g", Label: "G", Weight: 100, Gate: "gReview", ExitCriterion: "G is approved"}},
		Tasks:  []LifecycleTask{{ID: "gReview", Kind: LifecycleTaskReview, Title: "Review G", Phase: "g", DependsOn: []string{}, ArtifactKind: "G"}},
	}
	if problems := ValidateLifecycle(gateOnly); len(problems) != 0 {
		t.Fatalf("a lifecycle with no dispatch task may have a review that names none: %v", problems)
	}
}

func TestValidateLifecycle_RejectsEachDefect(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(l *Lifecycle)
		want   string
	}{
		{"cycle", func(l *Lifecycle) { l.Tasks[0].DependsOn = []string{"bReview"} }, "not an earlier task"},
		{"self dependency", func(l *Lifecycle) { l.Tasks[1].DependsOn = []string{"aReview"} }, "not an earlier task"},
		{"unknown dependency", func(l *Lifecycle) { l.Tasks[1].DependsOn = []string{"ghost"} }, "not an earlier task"},
		{"duplicate task id", func(l *Lifecycle) { l.Tasks[2].ID = "aDraft" }, "empty or not unique"},
		{"unknown kind", func(l *Lifecycle) { l.Tasks[0].Kind = "spike" }, "unknown kind"},
		{"unknown phase", func(l *Lifecycle) { l.Tasks[0].Phase = "zzz" }, "unknown phase"},
		{"dispatch without a command", func(l *Lifecycle) { l.Tasks[0].Command = "" }, "needs a command"},
		{"two roots", func(l *Lifecycle) { l.Tasks[2].DependsOn = []string{} }, "root tasks"},
		{"weights do not sum to 100", func(l *Lifecycle) { l.Phases[0].Weight = 50 }, "sum to 90"},
		{"duplicate phase id", func(l *Lifecycle) { l.Phases[1].ID = "a" }, "phase id"},
		{"gate is a dispatch task", func(l *Lifecycle) { l.Phases[0].Gate = "aDraft" }, "must be a review task in that phase"},
		{"gate lives in another phase", func(l *Lifecycle) { l.Phases[0].Gate = "bReview" }, "must be a review task in that phase"},
		{"no gate", func(l *Lifecycle) { l.Phases[0].Gate = "" }, "must be a review task in that phase"},
		{"a phase task its gate does not wait for", func(l *Lifecycle) { l.Tasks[2].Phase = "a" }, "not upstream of its phase gate"},
		{"reviews a non-ancestor", func(l *Lifecycle) { l.Tasks[1].Reviews = "bDraft" }, "not an ancestor dispatch task"},
		{"reviews a review", func(l *Lifecycle) { l.Tasks[3].Reviews = "aReview" }, "not an ancestor dispatch task"},
		{"review names nothing", func(l *Lifecycle) { l.Tasks[1].Reviews = "" }, "must name the dispatch task"},
		{"dispatch claims to review", func(l *Lifecycle) { l.Tasks[2].Reviews = "aDraft" }, "only a review task"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := validFixture()
			c.mutate(&l)
			problems := ValidateLifecycle(l)
			if !slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, c.want) }) {
				t.Fatalf("problems = %v, want one containing %q", problems, c.want)
			}
		})
	}
}
