package cleanengine

// CleanEngine is the generated Engine port (contract surface).
type CleanEngine interface {
	Compute(in Input) (Result, error)
}

// Input is a generated contract value type.
type Input struct{ N int }

// Result is a generated contract value type. Its Detail field pulls a hand-written
// type into the contract surface via the transitive closure.
type Result struct {
	Sum    int
	Detail Detail
}

// NewCleanEngine constructs the engine (generated constructor).
func NewCleanEngine() CleanEngine { return &cleanEngine{} }
