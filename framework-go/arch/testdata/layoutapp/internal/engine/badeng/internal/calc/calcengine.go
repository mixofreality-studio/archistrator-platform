// Package calc is badeng's internal sub-package. badeng's own hooks file
// imports it, which Go permits (same subtree) and the hooks import rule does
// not (DCT §12 B2: public API only).
package calc

// Double is an implementation detail of badeng.
func Double(n int) int { return n * 2 }
