package badeng_test

import (
	"testing"

	"example.com/layoutapp/internal/engine/badeng/internal/calc"
)

// engine_hooks_test.go is black-box and correctly named, but it reaches into
// its OWN component's internal/ sub-package, so it is flagged
// hooks-import-not-allowed: a hooks file drives the public API only.
func stepDoubleUC1P1S1(t *testing.T) int {
	t.Helper()
	return calc.Double(1)
}
