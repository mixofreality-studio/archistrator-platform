package scenario

import "sort"

// Problem codes: defects of the design that derivation found and reports
// instead of dropping the scenario or stimulus they concern.
const (
	// ProblemUnaddressed: an input stimulus has no target component, or a target
	// that has no contract, so no test of any component would ever receive it.
	ProblemUnaddressed = "SCN-UNADDRESSED"
	// ProblemActorPathClient: a path carries an actor's input but no stimulus
	// reaches a contracted Client, so the actor's side of the path is untested.
	ProblemActorPathClient = "SCN-ACTOR-PATH-CLIENT"
)

// Problem is one defect found while deriving a scenario.
type Problem struct {
	UseCase  string
	Scenario string // scenario id (UseCase-P<n>)
	Node     string // activity node the defect is at
	Code     string
	Message  string
}

// scenarioProblems reports every defect of the kept scenario s over graph g:
// each input stimulus that no contracted component receives, then, when an
// actor-decided or actor-lane node lies on the path and no stimulus reaches a
// Client, the first such node.
func scenarioProblems(in Input, g graph, view *View, s Scenario) []Problem {
	out := unaddressedProblems(in, s)
	if reachesClient(in, s) {
		return out
	}
	if id, ok := firstActorNode(in, g, view, s.Path); ok {
		out = append(out, Problem{
			UseCase: s.UseCase, Scenario: s.ID, Node: id, Code: ProblemActorPathClient,
			Message: "path carries an input of an actor at node " + id + " but no stimulus reaches a contracted Client",
		})
	}
	return out
}

// addressed reports whether a stimulus targets a contracted component.
func addressed(in Input, st Stimulus) bool {
	return st.Input.To != "" && isComponent(in, st.Input.To)
}

// unaddressedProblems is SCN-UNADDRESSED for every stimulus of s that no
// contracted component receives.
func unaddressedProblems(in Input, s Scenario) []Problem {
	var out []Problem
	for _, st := range s.Stimuli {
		if addressed(in, st) {
			continue
		}
		out = append(out, Problem{
			UseCase: s.UseCase, Scenario: s.ID, Node: st.Node, Code: ProblemUnaddressed,
			Message: "input stimulus at node " + st.Node + " names no contracted component; no component test receives it",
		})
	}
	return out
}

// reachesClient reports whether some stimulus of s lands on a contracted Client.
func reachesClient(in Input, s Scenario) bool {
	for _, st := range s.Stimuli {
		if addressed(in, st) && isClientComponent(in, st.Input.To) {
			return true
		}
	}
	return false
}

// firstActorNode is the first node on path that is an input of an actor: an
// input-classified node in an actor's lane or decided by an actor.
func firstActorNode(in Input, g graph, view *View, path []string) (string, bool) {
	for _, id := range path {
		n := g.nodes[id]
		if classify(n, in, view) == classInput && (in.Actors[n.LinkedActorID] || in.Actors[n.DecidedBy]) {
			return id, true
		}
	}
	return "", false
}

// isClientComponent reports whether id names a component whose contract family
// includes a Client's contract.
func isClientComponent(in Input, id string) bool {
	for _, kc := range family(in, id) {
		if kc.contract.Client {
			return true
		}
	}
	return false
}

// sortProblems orders problems by scenario id, node, code, so a Report is
// canonical (scenario ids sort the same way Report.Scenarios does).
func sortProblems(ps []Problem) {
	sort.SliceStable(ps, func(i, j int) bool {
		a, b := ps[i], ps[j]
		if a.Scenario != b.Scenario {
			return a.Scenario < b.Scenario
		}
		if a.Node != b.Node {
			return a.Node < b.Node
		}
		return a.Code < b.Code
	})
}
