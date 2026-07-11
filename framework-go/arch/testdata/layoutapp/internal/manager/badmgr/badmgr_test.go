package badmgr

import "testing"

// badmgr_test.go should have been named manager_test.go — the wrong name
// triggers the test-file-name violation.
func TestHelper(t *testing.T) {
	if helper() != 1 {
		t.Fatal("unexpected")
	}
}
