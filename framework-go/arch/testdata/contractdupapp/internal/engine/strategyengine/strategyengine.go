package strategyengine

type strategyEngine struct {
	strategy internalStrategy
}

func (s *strategyEngine) StepOne(x int) error    { s.strategy.stepOne(x); return nil }
func (s *strategyEngine) StepTwo(y string) error { s.strategy.stepTwo(y); return nil }

// internalStrategy is a LEGIT unexported internal polymorphism axis — same
// method COUNT (2) as the engine's own generated contract, but different
// (lowercase, non-error-returning) method names and signatures. It must not
// trip rule c: it is unexported (exportedInterfaces never even surfaces it as
// a candidate), and its signatures do not match StrategyEngine's regardless.
type internalStrategy interface {
	stepOne(n int) bool
	stepTwo(s string) bool
}
