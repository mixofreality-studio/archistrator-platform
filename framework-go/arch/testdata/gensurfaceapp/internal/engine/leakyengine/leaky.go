package leakyengine

type leakyEngine struct{}

func (l *leakyEngine) Run(in Payload) (Outcome, error) { return Outcome{Y: in.X}, nil }

// LeakyExtra is a ROGUE exported type — not part of the generated contract surface
// and not reachable from it. The gate must flag it (unless allowlisted).
type LeakyExtra struct{ Z int }

// LeakyFunc is a ROGUE exported top-level function — likewise flagged.
func LeakyFunc() {}
