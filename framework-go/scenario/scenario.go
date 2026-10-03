package scenario

// Scenario is one elementary path through a use case's activity diagram,
// expressed as the ordered input stimuli along it and the outcome observed
// after each one (the single-stimulus principle).
type Scenario struct {
	ID            string     `json:"id"`
	UseCase       string     `json:"useCase"`
	Title         string     `json:"title"`
	Path          []string   `json:"path"`
	Guards        []Guard    `json:"guards"`
	Preconditions Outcome    `json:"preconditions"` // outputs/states/hints seen before the first input
	Stimuli       []Stimulus `json:"stimuli"`
}

// Guard records the guard taken at a decision node on the path.
type Guard struct {
	At    string `json:"at"`
	Guard string `json:"guard"`
}

// Stimulus is one input on the path together with everything observed
// between it and the next input.
type Stimulus struct {
	Seq   int     `json:"seq"`
	Node  string  `json:"node"`
	Input InputEv `json:"input"`
	Outcome
}

// InputEv is the input event of a stimulus. Every dynamic-view call into a
// component on a path node is a stimulus of that component (Amendment A1): its
// callers are its actors, whether they are actors or other components.
type InputEv struct {
	Kind  string   `json:"kind"`            // "call" when From is a component; otherwise the node kind (action | acceptEvent | timeEvent | decision | switch | …)
	From  string   `json:"from"`            // the caller as written (actor or component id); "" for an un-called timeEvent
	To    string   `json:"to"`              // component id (normalized)
	Op    string   `json:"op"`              // resolved contract op name, or "" when unresolved
	Call  string   `json:"call"`            // "<useCase>/<nodeId>/<callIndex>" provenance ("…/<callIndex>.<k>" when one label names several ops), "" when none
	Guard string   `json:"guard,omitempty"` // the guard taken at a decision/switch node, on every stimulus of that node
	Via   []string `json:"via,omitempty"`   // the alternative callers when an alt group's calls into To collapse to this one stimulus (A4)
}

// Outcome is what is observed after a stimulus (or, for Preconditions,
// before the first one).
type Outcome struct {
	ExpectedOutputs []Output `json:"expectedOutputs"`
	ExpectedStates  []State  `json:"expectedStates"`
	Hints           []string `json:"hints"`
}

// Output is an expected output event.
type Output struct {
	Node  string `json:"node"`
	Kind  string `json:"kind"` // sendSignal | inferred
	To    string `json:"to"`
	Label string `json:"label"`
}

// State is an expected observable object state.
type State struct {
	Node    string `json:"node"`
	Object  string `json:"object"`
	InState string `json:"inState"`
}

// Report is a derivation together with what the derivation decided about the
// design while producing it.
type Report struct {
	Scenarios []Scenario
	// StartRedundant names, per use case and sorted by use case id, the start
	// families dropped because the diagram also has event entries and those
	// paths carry no stimulus of their own (Amendment A3, UC-START-REDUNDANT).
	StartRedundant []StartRedundancy
}

// StartRedundancy is one use case whose start-family paths were dropped.
type StartRedundancy struct {
	UseCase string
	Dropped int // number of start-family paths dropped
}
