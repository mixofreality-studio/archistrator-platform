package badmgr

import "testing"

// badmgr_test.go is neither manager_scenarios.gen_test.go nor
// manager_hooks_test.go — the only two test files a component package may
// carry — so it triggers the scenario-tests-only violation.
func TestHelper(t *testing.T) {
	if helper() != 1 {
		t.Fatal("unexpected")
	}
}
