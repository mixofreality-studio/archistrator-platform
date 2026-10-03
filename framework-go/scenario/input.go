// Package scenario derives deterministic test scenarios from a project's use-case
// activity diagrams, dynamic views and service contracts. It is a pure function
// from an adapter-neutral Input to a canonical []Scenario: no clock, no
// randomness, no map-iteration-order dependence and no file I/O.
package scenario

// Input is the adapter-neutral view of a project that derivation reads.
// Adapters (methodcheck.ScenarioInput, archistrator's projectstate adapter)
// build it; this package never decodes project.json itself.
type Input struct {
	Diagrams  []Diagram           // one per use case, sorted by UseCaseID by the adapter
	Views     []View              // dynamic views
	Actors    map[string]bool     // actor ids (LinkedActorID / DecidedBy / TraceCall endpoints)
	Contracts map[string]Contract // by component id (kebab or camel — see Normalize)
}

// Diagram is one use case's activity diagram.
type Diagram struct {
	UseCaseID string
	Title     string
	Nodes     []Node
	Edges     []Edge
}

// Node is one activity-diagram node.
type Node struct {
	ID            string
	Kind          string // wire names: start action decision merge fork join end swimLane note loop switch goto interruptEdge timeEvent acceptEvent sendSignal objectNode
	Label         string
	RoleName      string
	LinkedActorID string
	DecidedBy     string
	InState       string // objectNode only
	Anchor        string // note only
}

// Edge is a control-flow edge; Guard is "" for an unguarded flow.
type Edge struct{ From, To, Guard string }

// View is a dynamic view: the call steps for one use case.
type View struct {
	UseCaseID string
	Steps     []Step
}

// Step is the set of calls a dynamic view attaches to one activity node.
type Step struct {
	NodeID string
	Calls  []Call
}

// Call is one trace call within a step.
type Call struct{ From, To, Mode, Label, Alt string }

// Contract is a component's service contract as derivation sees it.
type Contract struct {
	Component string
	Ops       []Op
}

// Op is one contract operation.
type Op struct {
	Name   string
	Params []string // param names
}
