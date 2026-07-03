package leakyengine

// LeakyEngine is the generated Engine port (contract surface).
type LeakyEngine interface {
	Run(in Payload) (Outcome, error)
}

// Payload is a generated contract value type.
type Payload struct{ X string }

// Outcome is a generated contract value type.
type Outcome struct{ Y string }

// NewLeakyEngine constructs the engine (generated constructor).
func NewLeakyEngine() LeakyEngine { return &leakyEngine{} }
