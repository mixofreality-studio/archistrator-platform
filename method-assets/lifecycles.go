package methodassets

// lifecycles.go — THE PER-ACTIVITY-TYPE TASK DAG, as platform-fixed data.
//
// An activity advances through its own little life cycle (Righting Software,
// Appendix A): a DAG of TASKS, each either an agentic DISPATCH that produces an
// artifact or a REVIEW that judges one, grouped into LIFECYCLE PHASES that each
// carry an earned-value weight and a binary exit criterion — their gate task.
//
// lifecycles.json is the source of truth and is AUTHORED, not derived, so —
// unlike stepmanifest.gen.go — there is no generator: the file is embedded and
// parsed once at package init. lifecycles_test.go makes a release with a
// malformed or structurally invalid file impossible, which is what makes the
// init-time panic below unreachable in a tagged build.
//
// Type keys are the consuming app's own wire names: its ActivityType name, and
// "testing:<TestingVariant name>" for the testing variants.

import (
	"bytes"
	_ "embed" // go:embed lifecycles.json
	"encoding/json"
	"fmt"
	"slices"
)

//go:embed lifecycles.json
var lifecyclesJSON []byte

// The two kinds of task. A dispatch produces an artifact; a review judges one.
const (
	LifecycleTaskDispatch = "dispatch"
	LifecycleTaskReview   = "review"
)

// LifecyclePhase is a Figure A-2 grouping of tasks: the earned-value unit.
type LifecyclePhase struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Weight is the phase's Table A-1 share, in percent; a lifecycle's sum to 100.
	Weight int `json:"weight"`
	// Gate is the id of the review task whose success IS this phase's exit.
	Gate string `json:"gate"`
	// ExitCriterion states that exit as one sentence, in this lifecycle's words.
	ExitCriterion string `json:"exitCriterion"`
}

// LifecycleTask is one node of a lifecycle's DAG.
type LifecycleTask struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	// Phase is the LifecyclePhase.ID this task belongs to.
	Phase string `json:"phase"`
	// DependsOn names the tasks this one waits for. Each must appear EARLIER in
	// Lifecycle.Tasks — see ValidateLifecycle.
	DependsOn []string `json:"dependsOn"`
	// Reviews names the dispatch task a review judges; that pair is what a
	// send-back re-opens. Empty on a dispatch task.
	Reviews string `json:"reviews,omitempty"`
	// Command is the slash-command slug an agent runs for this task, if any.
	Command string `json:"command,omitempty"`
	// WorkerClass is the agent charter Command adopts (ManifestFor(Command).Agent).
	WorkerClass string `json:"workerClass,omitempty"`
	// ArtifactKind names what a dispatch produces (and what a review with no
	// Reviews target judges).
	ArtifactKind string `json:"artifactKind,omitempty"`
	// SendsBackTo names the review gate this dispatch may report a defect in the
	// reviewed work to: a run that names only what it found wrong re-opens that gate's
	// judged pair, and this task holds until the gate passes again. The route names the
	// gate rather than the task behind it, so nothing skips the review.
	SendsBackTo string `json:"sendsBackTo,omitempty"`
	// SendsUpstreamTo names the review gates of OTHER activity types this dispatch may report
	// a defect in: a draft that cannot honestly be made against what an upstream artifact
	// says reports the defect, and the upstream gate's judged pair re-opens, so the amendment
	// goes through that gate's critique and human commit again. Like SendsBackTo it names the
	// gate rather than the task behind it, so nothing skips review. ValidateLifecycles holds
	// each entry to a lifecycle type and a review gate that exist.
	SendsUpstreamTo []UpstreamRoute `json:"sendsUpstreamTo,omitempty"`
}

// UpstreamRoute is one gate of another activity type a dispatch may report a defect in.
type UpstreamRoute struct {
	// ActivityType is the upstream lifecycle's type key.
	ActivityType string `json:"activityType"`
	// Gate is the review task in that lifecycle whose judged pair the report re-opens.
	Gate string `json:"gate"`
}

// Lifecycle is the task DAG every activity of one type walks.
type Lifecycle struct {
	Type   string           `json:"type"`
	Phases []LifecyclePhase `json:"phases"`
	Tasks  []LifecycleTask  `json:"tasks"`
}

type lifecyclesFile struct {
	Lifecycles []Lifecycle `json:"lifecycles"`
}

var lifecycles = mustParseLifecycles(lifecyclesJSON)

func mustParseLifecycles(raw []byte) []Lifecycle {
	out, err := parseLifecycles(raw)
	if err != nil {
		panic("method-assets: embedded lifecycles.json: " + err.Error())
	}
	return out
}

// parseLifecycles decodes the data file strictly: an unknown field is an error,
// so a typo cannot ship as a silently dropped value.
func parseLifecycles(raw []byte) ([]Lifecycle, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var file lifecyclesFile
	if err := dec.Decode(&file); err != nil {
		return nil, err
	}
	return file.Lifecycles, nil
}

// LifecycleFor returns the lifecycle for an activity-type key. The result is a
// deep copy; callers may mutate it.
func LifecycleFor(typeKey string) (Lifecycle, bool) {
	for _, l := range lifecycles {
		if l.Type == typeKey {
			return l.clone(), true
		}
	}
	return Lifecycle{}, false
}

// Lifecycles returns every lifecycle in lifecycles.json order. The result is a
// deep copy; callers may mutate it.
func Lifecycles() []Lifecycle {
	out := make([]Lifecycle, len(lifecycles))
	for i, l := range lifecycles {
		out[i] = l.clone()
	}
	return out
}

func (l Lifecycle) clone() Lifecycle {
	c := Lifecycle{Type: l.Type, Phases: slices.Clone(l.Phases), Tasks: make([]LifecycleTask, len(l.Tasks))}
	for i, t := range l.Tasks {
		t.DependsOn = slices.Clone(t.DependsOn)
		t.SendsUpstreamTo = slices.Clone(t.SendsUpstreamTo)
		c.Tasks[i] = t
	}
	return c
}

// ValidateLifecycle returns every structural defect of one lifecycle as a
// sentence prefixed with its type key; an empty result means it is valid.
//
// The rules:
//   - task ids are non-empty and unique; kinds are dispatch or review; every
//     task names a declared phase; a dispatch carries command, workerClass and
//     artifactKind;
//   - a task depends only on tasks authored EARLIER. A cycle cannot be written
//     with every edge pointing backwards, so this one rule is acyclicity, and
//     it rejects unknown ids and self-references on the way;
//   - there is exactly one root task;
//   - phase ids are unique, weights are positive and sum to 100, and every
//     phase's gate is a review task IN that phase which every other task of
//     the phase is upstream of — so "gate passed" means "phase complete";
//   - a dispatch's SendsBackTo is safe to re-open (checkSendBackRoutes).
//   - only a dispatch carries SendsUpstreamTo, and each entry names another type and a
//     gate (what those name is checked against the whole catalog: ValidateLifecycles).
//   - a review names (Reviews) a dispatch task it transitively depends on. Only
//     a lifecycle with no dispatch task at all may have a review that names
//     none, and that review then carries the artifactKind itself.
func ValidateLifecycle(l Lifecycle) []string {
	v := newLifecycleValidator(l)
	v.checkTasks()
	v.checkRoots()
	v.checkPhases()
	v.checkReviews()
	v.checkSendBackRoutes()
	v.checkUpstreamRouteShapes()
	return v.problems
}

// ValidateLifecycles validates a whole catalog: every lifecycle on its own
// (ValidateLifecycle), its type keys unique, and every SendsUpstreamTo entry naming a
// lifecycle type of the catalog and, in it, a review gate with a reviews target — the
// judged pair the report re-opens. An empty result means the catalog is valid.
func ValidateLifecycles(ls []Lifecycle) []string {
	var problems []string
	byType := map[string]Lifecycle{}
	for _, l := range ls {
		problems = append(problems, ValidateLifecycle(l)...)
		if _, dup := byType[l.Type]; dup {
			problems = append(problems, fmt.Sprintf("lifecycle type %q is not unique", l.Type))
		}
		byType[l.Type] = l
	}
	for _, l := range ls {
		for _, t := range l.Tasks {
			for _, r := range t.SendsUpstreamTo {
				problems = append(problems, upstreamRouteProblem(l, t, r, byType)...)
			}
		}
	}
	return problems
}

// upstreamRouteProblem is what is wrong with one route against the catalog, if anything.
func upstreamRouteProblem(l Lifecycle, t LifecycleTask, r UpstreamRoute, byType map[string]Lifecycle) []string {
	target, ok := byType[r.ActivityType]
	if !ok {
		return []string{fmt.Sprintf("%s: task %q sends upstream to type %q, which no lifecycle has", l.Type, t.ID, r.ActivityType)}
	}
	for _, g := range target.Tasks {
		if g.ID == r.Gate && g.Kind == LifecycleTaskReview && g.Reviews != "" {
			return nil
		}
	}
	return []string{fmt.Sprintf("%s: task %q sends upstream to %s.%s, which is not a review with a reviews target", l.Type, t.ID, r.ActivityType, r.Gate)}
}

// checkUpstreamRouteShapes holds what one lifecycle alone can say about its upstream routes:
// they sit on a dispatch, name another type and a gate, and do not repeat.
func (v *lifecycleValidator) checkUpstreamRouteShapes() {
	for _, t := range v.l.Tasks {
		switch {
		case len(t.SendsUpstreamTo) == 0:
		case t.Kind != LifecycleTaskDispatch:
			v.addf("task %q sets sendsUpstreamTo, but only a dispatch task may", t.ID)
		default:
			v.checkUpstreamRoutesOf(t)
		}
	}
}

// checkUpstreamRoutesOf holds one dispatch's routes to naming another type and a gate, once each.
func (v *lifecycleValidator) checkUpstreamRoutesOf(t LifecycleTask) {
	seen := map[UpstreamRoute]bool{}
	for _, r := range t.SendsUpstreamTo {
		switch {
		case r.ActivityType == "" || r.Gate == "":
			v.addf("task %q has an upstream route with no activityType or gate", t.ID)
		case r.ActivityType == v.l.Type:
			v.addf("task %q sends upstream to its own type %q; a route inside one lifecycle is sendsBackTo", t.ID, r.ActivityType)
		case seen[r]:
			v.addf("task %q names the upstream route %s.%s twice", t.ID, r.ActivityType, r.Gate)
		}
		seen[r] = true
	}
}

type lifecycleValidator struct {
	l        Lifecycle
	phaseIDs map[string]bool
	tasks    map[string]LifecycleTask
	// ancestors[id] is every task id transitively depends on. An entry exists
	// only for a task already accepted, which is what "earlier" is checked by.
	ancestors map[string]map[string]bool
	problems  []string
}

func newLifecycleValidator(l Lifecycle) *lifecycleValidator {
	v := &lifecycleValidator{
		l:         l,
		phaseIDs:  map[string]bool{},
		tasks:     map[string]LifecycleTask{},
		ancestors: map[string]map[string]bool{},
	}
	for _, p := range l.Phases {
		v.phaseIDs[p.ID] = true
	}
	return v
}

func (v *lifecycleValidator) addf(format string, args ...any) {
	v.problems = append(v.problems, v.l.Type+": "+fmt.Sprintf(format, args...))
}

func (v *lifecycleValidator) checkTasks() {
	for _, t := range v.l.Tasks {
		if _, dup := v.tasks[t.ID]; dup || t.ID == "" {
			v.addf("task id %q is empty or not unique", t.ID)
			continue
		}
		v.checkTaskFields(t)
		v.ancestors[t.ID] = v.ancestorsOf(t)
		v.tasks[t.ID] = t
	}
}

func (v *lifecycleValidator) checkTaskFields(t LifecycleTask) {
	if t.Kind != LifecycleTaskDispatch && t.Kind != LifecycleTaskReview {
		v.addf("task %q has unknown kind %q", t.ID, t.Kind)
	}
	if !v.phaseIDs[t.Phase] {
		v.addf("task %q names unknown phase %q", t.ID, t.Phase)
	}
	if t.Title == "" {
		v.addf("task %q has no title", t.ID)
	}
	if t.Kind == LifecycleTaskDispatch && !dispatchIsComplete(t) {
		v.addf("dispatch task %q needs a command, a workerClass and an artifactKind", t.ID)
	}
}

func dispatchIsComplete(t LifecycleTask) bool {
	return t.Command != "" && t.WorkerClass != "" && t.ArtifactKind != ""
}

func (v *lifecycleValidator) ancestorsOf(t LifecycleTask) map[string]bool {
	out := map[string]bool{}
	for _, dep := range t.DependsOn {
		upstream, earlier := v.ancestors[dep]
		if !earlier {
			v.addf("task %q depends on %q, which is not an earlier task (an unknown id, a self or forward reference, or a cycle)", t.ID, dep)
			continue
		}
		out[dep] = true
		for a := range upstream {
			out[a] = true
		}
	}
	return out
}

func (v *lifecycleValidator) checkRoots() {
	roots := 0
	for _, t := range v.l.Tasks {
		if len(t.DependsOn) == 0 {
			roots++
		}
	}
	if roots != 1 {
		v.addf("%d root tasks, want exactly 1", roots)
	}
}

func (v *lifecycleValidator) checkPhases() {
	total := 0
	seen := map[string]bool{}
	for _, p := range v.l.Phases {
		if p.ID == "" || seen[p.ID] {
			v.addf("phase id %q is empty or not unique", p.ID)
		}
		if p.Weight <= 0 {
			v.addf("phase %q has weight %d, want a positive share", p.ID, p.Weight)
		}
		seen[p.ID] = true
		total += p.Weight
		v.checkGate(p)
	}
	if total != 100 {
		v.addf("phase weights sum to %d, want 100", total)
	}
}

func (v *lifecycleValidator) checkGate(p LifecyclePhase) {
	gate, ok := v.tasks[p.Gate]
	if !ok || gate.Kind != LifecycleTaskReview || gate.Phase != p.ID {
		v.addf("phase %q gate %q must be a review task in that phase", p.ID, p.Gate)
		return
	}
	for _, t := range v.l.Tasks {
		if t.Phase == p.ID && t.ID != gate.ID && !v.ancestors[gate.ID][t.ID] {
			v.addf("task %q is not upstream of its phase gate %q, so the gate could pass with it unfinished", t.ID, gate.ID)
		}
	}
}

func (v *lifecycleValidator) checkReviews() {
	dispatches := 0
	for _, t := range v.l.Tasks {
		if t.Kind == LifecycleTaskDispatch {
			dispatches++
		}
	}
	for _, t := range v.l.Tasks {
		if t.Kind == LifecycleTaskReview {
			v.checkReview(t, dispatches)
			continue
		}
		if t.Reviews != "" {
			v.addf("task %q sets reviews, but only a review task may", t.ID)
		}
	}
}

// checkSendBackRoutes holds every SendsBackTo to what makes a re-open safe: it sits on a
// dispatch task and names a review with a Reviews target, that review does not wait on the
// source (the source could then never run), and everything downstream of the review is
// downstream of the source too, so a re-open cannot leave a stale pass standing behind it.
func (v *lifecycleValidator) checkSendBackRoutes() {
	for _, t := range v.l.Tasks {
		if t.SendsBackTo != "" {
			v.checkSendBackRoute(t)
		}
	}
}

func (v *lifecycleValidator) checkSendBackRoute(t LifecycleTask) {
	if t.Kind != LifecycleTaskDispatch {
		v.addf("task %q sets sendsBackTo, but only a dispatch task may", t.ID)
		return
	}
	target, ok := v.tasks[t.SendsBackTo]
	if !ok || target.Kind != LifecycleTaskReview || target.Reviews == "" {
		v.addf("task %q sends back to %q, which is not a review with a reviews target", t.ID, t.SendsBackTo)
		return
	}
	if v.ancestors[t.SendsBackTo][t.ID] {
		v.addf("task %q sends back to %q, which waits on it", t.ID, t.SendsBackTo)
	}
	for _, other := range v.l.Tasks {
		if other.ID != t.ID && v.ancestors[other.ID][t.SendsBackTo] && !v.ancestors[other.ID][t.ID] {
			v.addf("task %q sends back to %q, but %q passes behind that review without waiting on %q", t.ID, t.SendsBackTo, other.ID, t.ID)
		}
	}
}

func (v *lifecycleValidator) checkReview(t LifecycleTask, dispatches int) {
	if t.Reviews == "" {
		if dispatches > 0 || t.ArtifactKind == "" {
			v.addf("review task %q must name the dispatch task it reviews (only a lifecycle with no dispatch task may omit it, and that review then carries the artifactKind)", t.ID)
		}
		return
	}
	target, ok := v.tasks[t.Reviews]
	if !ok || target.Kind != LifecycleTaskDispatch || !v.ancestors[t.ID][t.Reviews] {
		v.addf("review task %q reviews %q, which is not an ancestor dispatch task", t.ID, t.Reviews)
	}
}
