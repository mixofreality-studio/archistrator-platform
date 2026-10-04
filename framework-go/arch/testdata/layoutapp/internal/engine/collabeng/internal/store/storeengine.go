// Package store is collabeng's internal sub-package: Go's internal rule
// already hides it from other components, and the hooks import rule hides it
// from every hooks file (DCT §12 B2: public API only).
package store

// Lookup is an implementation detail of collabeng.
func Lookup(n int) int { return n * 2 }
