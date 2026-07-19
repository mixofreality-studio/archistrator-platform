package strategyengine

// StrategyEngine is the generated Engine port (contract surface).
type StrategyEngine interface {
	StepOne(x int) error
	StepTwo(y string) error
}

// NewStrategyEngine constructs the engine (generated constructor).
func NewStrategyEngine() StrategyEngine { return &strategyEngine{} }
