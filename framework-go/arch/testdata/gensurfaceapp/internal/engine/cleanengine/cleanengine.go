package cleanengine

// Detail is hand-written but part of the contract surface — it is reachable from
// the generated Result type, so the closure keeps it off the violation list.
type Detail struct{ Note string }

type cleanEngine struct{}

func (c *cleanEngine) Compute(in Input) (Result, error) {
	return Result{Sum: in.N + helper(), Detail: Detail{Note: "ok"}}, nil
}

func helper() int { return 1 }
