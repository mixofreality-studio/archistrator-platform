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

// InputEv is the input event of a stimulus.
type InputEv struct {
	Kind  string `json:"kind"`            // action | acceptEvent | timeEvent | decision
	From  string `json:"from"`            // actor id, or "" for timeEvent
	To    string `json:"to"`              // component id (normalized)
	Op    string `json:"op"`              // resolved contract op name, or "" when unresolved
	Call  string `json:"call"`            // "<useCase>/<nodeId>/<callIndex>" provenance, "" when none
	Guard string `json:"guard,omitempty"` // decision inputs only
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
