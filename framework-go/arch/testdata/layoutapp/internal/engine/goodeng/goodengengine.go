// Package goodeng is a CLEAN Engine-layer fixture: the impl file plus a
// correctly named test file, no workflow funcs. The checker must produce zero
// violations here.
package goodeng

type goodEngine struct{}

func (g *goodEngine) Compute(n int) (int, error) { return n + 1, nil }
