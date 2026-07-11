// Package badeng is a DIRTY Engine-layer fixture: its impl file is clean, but
// pure.go declares a workflow func — forbidden outside the Manager layer.
package badeng

type badEngine struct{}

func (b *badEngine) Compute(n int) (int, error) { return n + 1, nil }
