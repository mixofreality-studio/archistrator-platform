package badmgr

import "testing"

// manager_test.go was the name the retired test-file-name rule mandated; a
// hand-written, in-package test is exactly what scenario-tests-only forbids,
// so it is flagged scenario-tests-only (the name is outside the allowed pair).
func TestHelperAgain(t *testing.T) {
	if helper() != 1 {
		t.Fatal("unexpected")
	}
}
