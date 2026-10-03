// Package goodeng is a CLEAN Engine-layer fixture: the impl file plus a
// correctly named, black-box (package goodeng_test) generated scenarios test
// file, no workflow funcs. The checker must produce zero violations here.
package goodeng

// Engine is exported so the black-box (goodeng_test) scenario tests can
// construct it.
type Engine struct{}

func (g *Engine) Compute(n int) (int, error) { return n + 1, nil }
